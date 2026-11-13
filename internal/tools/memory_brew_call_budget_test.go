package tools

// Regression tests for the two brew failure modes a real
// `gopi memory brew --dry-run --limit 4` run surfaced (2026-09-14, 260.1s run):
//
//	! session.jsonl: distill "判 merge 只判增量非既有红": brew: too few breadcrumbs (1 < 6) …
//	! session.jsonl: distill workflow "共享tmpfs满导致的伪失败排查流程": brew: llm call:
//	  deepseek: failed to read response body: context deadline exceeded
//
// ① the per-call budget was un-tunable and reported itself as a bare
//    "context deadline exceeded" (indistinguishable from a code bug);
// ② a thin breadcrumb list — a SAMPLING shortfall, not a broken reply —
//    discarded an otherwise good lesson with no re-ask.

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"
)

// budgetBrewClient scripts per-call errors AND replies (the plain
// stubBrewClient only scripts replies).
type budgetBrewClient struct {
	errs    []error
	replies []string
	calls   int
}

func (c *budgetBrewClient) Chat(ctx context.Context, system, user string) (string, error) {
	i := c.calls
	c.calls++
	if i < len(c.errs) && c.errs[i] != nil {
		return "", c.errs[i]
	}
	if i < len(c.replies) {
		return c.replies[i], nil
	}
	return "", fmt.Errorf("budget stub: unexpected call #%d", i)
}

// sixKeywordQA is a reply that clears brewMinKeywords.
const sixKeywordQA = `{"question":"q","answer":"a","keywords":["a","b","c","d","e","f"]}`

// thinQA is a reply with one breadcrumb — the exact shape the live run hit.
const thinQA = `{"question":"q","answer":"a","keywords":["判 merge 只判增量"]}`

// --- ① per-call budget: tunable + self-attributing -----------------------

// TestBrewTimeout_ConstantsAreTheDefaultEnvIsTheOverride pins Rule 2: the
// constant is the default, GOPI_BREW_TIMEOUT_SEC is the only override, and a
// malformed value never silently becomes a zero (or negative) budget.
func TestBrewTimeout_ConstantsAreTheDefaultEnvIsTheOverride(t *testing.T) {
	t.Setenv(brewTimeoutEnv, "")
	if got := brewTimeout(); got != brewClientTimeout {
		t.Fatalf("brewTimeout() = %s, want the constant %s", got, brewClientTimeout)
	}
	t.Setenv(brewTimeoutEnv, "42")
	if got := brewTimeout(); got != 42*time.Second {
		t.Fatalf("brewTimeout() = %s, want 42s", got)
	}
	for _, bad := range []string{"0", "-5", "banana", " "} {
		t.Setenv(brewTimeoutEnv, bad)
		if got := brewTimeout(); got != brewClientTimeout {
			t.Errorf("brewTimeout() with %q = %s, want the default %s", bad, got, brewClientTimeout)
		}
	}
}

// TestCallBrewJSON_DeadlineNamesTheBudget is the interpretability half: the
// error must say WHICH call ran out of time, on WHICH budget, and which knob
// raises it — never a bare "context deadline exceeded".
func TestCallBrewJSON_DeadlineNamesTheBudget(t *testing.T) {
	t.Setenv(brewTimeoutEnv, "")
	bc := &budgetBrewClient{errs: []error{context.DeadlineExceeded}}
	var out brewDistillReply
	err := callBrewJSON(context.Background(), bc, "sys", brewDistillPrompt+"主题: t", &out)
	if err == nil {
		t.Fatal("a deadline must still be an error")
	}
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("the deadline must stay in the chain (errors.Is): %v", err)
	}
	msg := err.Error()
	for _, want := range []string{
		"180s",         // the budget actually in force
		brewTimeoutEnv, // the knob that raises it
		"distill",      // which call failed
	} {
		if !strings.Contains(msg, want) {
			t.Errorf("deadline message %q is missing %q", msg, want)
		}
	}
}

// TestPromptTag_NamesEachStage: the tag drives the message, so it must map
// each prompt to its stage (and never mislabel one as another).
func TestPromptTag_NamesEachStage(t *testing.T) {
	cases := map[string]string{
		brewJudgePrompt + "chunk":    "judge",
		brewDistillPrompt + "主题: x":  "distill",
		brewWorkflowPrompt + "主题: x": "distill workflow",
		"something else entirely":    "unknown prompt",
	}
	for prompt, want := range cases {
		if got := promptTag(prompt); got != want {
			t.Errorf("promptTag(%.20s…) = %q, want %q", prompt, got, want)
		}
	}
}

// TestCallBrewJSON_ParentCancellationIsNotBlamedOnTheBudget: when the CALLER
// cancelled (session exit, Ctrl-C), the message must not tell the operator to
// raise the brew timeout.
func TestCallBrewJSON_ParentCancellationIsNotBlamedOnTheBudget(t *testing.T) {
	t.Setenv(brewTimeoutEnv, "")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	bc := &budgetBrewClient{errs: []error{context.DeadlineExceeded}}
	var out brewDistillReply
	err := callBrewJSON(ctx, bc, "sys", brewDistillPrompt+"主题: t", &out)
	if err == nil {
		t.Fatal("want an error")
	}
	if strings.Contains(err.Error(), brewTimeoutEnv) {
		t.Errorf("a caller-side cancellation must not point at %s: %v", brewTimeoutEnv, err)
	}
}

// --- ② thin breadcrumbs: one re-ask, never a silent discard --------------

// TestDistillLesson_ThinBreadcrumbsReasksAndRecovers: the first reply has 1
// keyword, the second 6 — the lesson is stored instead of thrown away.
func TestDistillLesson_ThinBreadcrumbsReasksAndRecovers(t *testing.T) {
	bc := &budgetBrewClient{replies: []string{thinQA, sixKeywordQA}}
	qa, err := distillLesson(context.Background(), bc, brewCandidate{Topic: "判 merge 只判增量非既有红"})
	if err != nil {
		t.Fatalf("a re-ask must recover the lesson: %v", err)
	}
	if len(qa.Keywords) < brewMinKeywords {
		t.Fatalf("recovered row has %d keywords, want >= %d", len(qa.Keywords), brewMinKeywords)
	}
	if bc.calls != brewDistillRepairTries+1 {
		t.Errorf("calls = %d, want exactly %d (one re-ask)", bc.calls, brewDistillRepairTries+1)
	}
}

// TestDistillLesson_ThinBreadcrumbsGivesUpAfterTheCap: two thin replies — the
// error still explains the rejection, names the re-ask, and the call count is
// bounded (no unbounded retry loop, cost budget ④).
func TestDistillLesson_ThinBreadcrumbsGivesUpAfterTheCap(t *testing.T) {
	bc := &budgetBrewClient{replies: []string{thinQA, thinQA, thinQA, thinQA}}
	_, err := distillLesson(context.Background(), bc, brewCandidate{Topic: "t"})
	if err == nil {
		t.Fatal("two thin replies must be a rejection")
	}
	if !errors.Is(err, errThinBreadcrumbs) {
		t.Errorf("the rejection must stay classifiable: %v", err)
	}
	msg := err.Error()
	if !strings.Contains(msg, "1 < 6") {
		t.Errorf("the message must still report the counts: %q", msg)
	}
	if !strings.Contains(msg, "re-asked") {
		t.Errorf("the message must say a re-ask happened: %q", msg)
	}
	if bc.calls != brewDistillRepairTries+1 {
		t.Errorf("calls = %d, want the capped %d", bc.calls, brewDistillRepairTries+1)
	}
}

// TestDistillLesson_MalformedReplyIsNotRetried: only a thin breadcrumb list
// earns a re-ask — a reply that fails to parse must fail on the first call
// (an extra LLM call per malformed reply is real money).
func TestDistillLesson_MalformedReplyIsNotRetried(t *testing.T) {
	bc := &budgetBrewClient{replies: []string{"totally not json", sixKeywordQA}}
	_, err := distillLesson(context.Background(), bc, brewCandidate{Topic: "t"})
	if err == nil {
		t.Fatal("a malformed reply must be an error")
	}
	if bc.calls != 1 {
		t.Errorf("calls = %d, want 1 (no re-ask for a malformed reply)", bc.calls)
	}
}

// TestDistillLesson_EmptyQAIsNotRetried — an empty QA is a prompt/answer
// problem, not sampling: it must not spend a second call either.
func TestDistillLesson_EmptyQAIsNotRetried(t *testing.T) {
	bc := &budgetBrewClient{replies: []string{`{"question":"","answer":"","keywords":[]}`, sixKeywordQA}}
	_, err := distillLesson(context.Background(), bc, brewCandidate{Topic: "t"})
	if err == nil {
		t.Fatal("an empty QA must be an error")
	}
	if !strings.Contains(err.Error(), "distilled QA empty") {
		t.Errorf("unexpected error: %v", err)
	}
	if bc.calls != 1 {
		t.Errorf("calls = %d, want 1 (no re-ask for an empty QA)", bc.calls)
	}
}
