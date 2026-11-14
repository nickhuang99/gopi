package main

// web_session_budget_test.go — the WEB console's session-budget DEFAULT
// (cmd/agent/chat_session_budget.go's publishWebSessionBudgetDefault, called from
// runWeb).
//
// THE DEFECT THESE TESTS PIN (task 01a1b498, measured 2026-11-07): every `gopi
// web` turn goes through Agent.ChatOpsOnCarrier — the SAME task prompt assembly a
// dispatched worker gets — and `runWeb` never published the budget default that
// `runChatWithInput` publishes. So the console inherited internal/agent's
// 60-minute TASK ceiling (sessionBudgetMinutes) and taught it to the model: the
// operator's live PA console received
//
//	session budget: 55m28s elapsed of 1h0m0s (92% used, past the 75% mark) — …
//
// at the 55th minute of an unbounded human conversation, and was told to land the
// work or declare TOO_BIG. A web console is a CONVERSATION, not a row: it has no
// epic to split and no dispatcher waiting, exactly as chat_session_budget.go
// argues for `gopi chat`.
//
// THREE LAYERS, because they catch different mistakes:
//
//   - the SEAM (this process): unset ⇒ "0" (unlimited), an explicit value
//     untouched, and the notice names THIS surface ([gopi] web:), so an operator
//     reading a mixed log can tell which face spoke;
//   - the SURFACE (the real `gopi web`, over real HTTP): the sentence the model
//     actually reads, read back off the mock's captured request body — the defect
//     was a sentence, and only the wire proves it is gone (Rule 31);
//   - the NON-REGRESSION (the same real surface, with the operator's own export in
//     front of it): a decided value reaches the prompt UNCHANGED — a default, never
//     an enforcement.

import (
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/nickhuang99/gopi/internal/config"
	"github.com/nickhuang99/gopi/internal/lineedit"
	"github.com/nickhuang99/gopi/internal/testutil"
)

const (
	// webBudgetTurnPrompt is the user message; the scripted mock answers by
	// request INDEX, so its text only has to be a well-formed turn.
	webBudgetTurnPrompt = "hello"
	// webBudgetTurnReply is the mock provider's answer.
	webBudgetTurnReply = "OK"
	// webBudgetTurnTimeout bounds the one turn — a hang guard, not a
	// performance assertion.
	webBudgetTurnTimeout = 60 * time.Second
)

// TestWebBudgetPublishesUnlimitedWhenUnset is the headline seam case: a web
// launch that inherits NO budget export publishes unlimited, and the notice
// names the surface, the variable, the value and the reason (Rule 15 — a silent
// default would leave the operator's "why is my console unlimited?"
// unanswerable).
//
// The variable is genuinely ABSENT for the call, not merely empty: the
// publication's input is the environment's own "unset", which is the whole
// defect (a budget nobody had decided on).
func TestWebBudgetPublishesUnlimitedWhenUnset(t *testing.T) {
	// Rule 17: a PER-CALL flip uses os.Setenv behind testutil.SnapshotEnv
	// (t.Setenv's scope is the whole test, which would hide the "was it absent
	// before?" half this assertion is about).
	defer testutil.SnapshotEnv(t, chatSessionBudgetEnv)()
	if err := os.Unsetenv(chatSessionBudgetEnv); err != nil { // env-leak-ok: testutil.SnapshotEnv restores this on test return, ABSENCE included — the case's SUBJECT is the variable being absent, which t.Setenv cannot express (it only ever sets a present value)
		t.Fatalf("unset %s: %v", chatSessionBudgetEnv, err)
	}

	stderr := captureStderrStream(t, func() { publishWebSessionBudgetDefault(termToolStylesFor(lineedit.ColourOff)) })

	if got := os.Getenv(chatSessionBudgetEnv); got != chatSessionBudgetUnlimited {
		t.Fatalf("%s = %q after the web default, want %q (unlimited)",
			chatSessionBudgetEnv, got, chatSessionBudgetUnlimited)
	}
	// The SURFACE half: the sentence must name the face that applied it. The chat
	// notice's own prefix would be a lie on a web launch, and the two share one
	// format precisely so the only difference is this argument.
	for _, want := range []string{sessionBudgetSurfaceWeb, chatSessionBudgetEnv, chatSessionBudgetUnlimited, "unlimited"} {
		if !strings.Contains(stderr, want) {
			t.Errorf("the default was published without saying so: stderr %q does not name %q", stderr, want)
		}
	}
	if strings.Contains(stderr, sessionBudgetSurfaceChat) {
		t.Errorf("the web publication announced itself as the CHAT surface: stderr %q", stderr)
	}
}

// TestWebBudgetHonoursAnExplicitValue is the constraint the operator stated in
// as many words: a default, never an enforcement. An explicitly exported budget
// is a decision about the operator's OWN surface, so the web console honours it
// verbatim — including values that are NOT unlimited — and does not touch it.
//
// "0" is in the list on purpose: a caller that already declared unlimited must
// not be re-declared, and an implementation keyed on the VALUE rather than on
// PRESENCE would print its default notice over a decision somebody had made.
func TestWebBudgetHonoursAnExplicitValue(t *testing.T) {
	for _, value := range []string{"30", "0", "1"} {
		t.Run("explicit="+value, func(t *testing.T) {
			defer testutil.SnapshotEnv(t, chatSessionBudgetEnv)()
			if err := os.Setenv(chatSessionBudgetEnv, value); err != nil { // env-leak-ok: testutil.SnapshotEnv restores this on test return, absence included — the flip is PER-CALL (call N's value must be gone before call N+1), so it is torn down by the defer this file pairs with every set //stylegate:ignore SnapshotEnv IS the explicit restore: the pair is torn down when THIS closure returns, which is the per-call scope the case is about, not at test cleanup
				t.Fatalf("set %s: %v", chatSessionBudgetEnv, err)
			}

			stderr := captureStderrStream(t, func() { publishWebSessionBudgetDefault(termToolStylesFor(lineedit.ColourOff)) })

			if got := os.Getenv(chatSessionBudgetEnv); got != value {
				t.Fatalf("%s = %q after a web run, want the operator's %q untouched",
					chatSessionBudgetEnv, got, value)
			}
			if strings.Contains(stderr, "unset") {
				t.Errorf("a web run with an explicit budget announced a default: stderr %q", stderr)
			}
		})
	}
}

// TestWebTurnPromptTeachesNoFabricatedCeiling is the SURFACE half: the REAL
// binary, `gopi web --tools --addr=127.0.0.1:0`, driven over real HTTP, with the
// sentence read back off the request body the mock captured. It is the exact
// invocation that rendered `BUDGET: session budget 60min` before this change.
//
// testutil.MockEnv strips every ambient GOPI_* variable, so "the operator
// exported nothing" is the real precondition here rather than a claim.
func TestWebTurnPromptTeachesNoFabricatedCeiling(t *testing.T) {
	home := t.TempDir()
	llm := testutil.NewMockLLM(t, testutil.Resp{Text: webBudgetTurnReply})
	defer llm.Close()

	p := testutil.StartGopi(t, []string{"web", "--addr=127.0.0.1:0", webToolsFlag}, "",
		testutil.MockEnv(t, home, llm.URL()), "")
	defer p.Stop()
	base := p.WebBaseURL()

	// Readiness = the session artifact: the JSONL is created at agent
	// construction, before the listener accepts (the sibling live cases' signal).
	sessRoot := filepath.Join(home, ".gopi", config.SessionsDirName)
	p.WaitFor("the sandboxed session JSONL", func() bool { return testutil.SessionRecordPath(sessRoot) != "" })

	stream := webPostOneTurn(t, base)
	if !strings.Contains(stream, "event: done") {
		t.Fatalf("turn did not reach `event: done` — stream=%q\nstderr:\n%s", stream, p.Stderr())
	}

	body := llm.RequestBody(1)
	if body == "" {
		t.Fatalf("the mock captured no request — no turn ran, so the prompt was never observed\nstderr:\n%s", p.Stderr())
	}
	if strings.Contains(body, chatBudgetFabricatedCeiling) {
		t.Errorf("the web turn still teaches the fabricated ceiling: request body carries %q", chatBudgetFabricatedCeiling)
	}
	if strings.Contains(body, "session budget 60min") {
		t.Errorf("the web turn still renders a 60-minute budget anywhere in the request: %s", budgetRuleLine(body))
	}
	if !strings.Contains(body, chatBudgetUnlimitedRule) {
		t.Errorf("the web turn does not state the unlimited budget — the rule line is missing: %s", budgetRuleLine(body))
	}
	// The DEFAULT is announced on stderr, under the WEB surface's prefix, and the
	// announcement is what makes the run's own output self-explanatory (Rule 15).
	if !strings.Contains(p.Stderr(), sessionBudgetSurfaceWeb+" "+chatSessionBudgetEnv) &&
		!strings.Contains(p.Stderr(), sessionBudgetSurfaceWeb+chatSessionBudgetEnv) {
		t.Errorf("the run published the default without naming %s%s on stderr:\n%s",
			sessionBudgetSurfaceWeb, chatSessionBudgetEnv, p.Stderr())
	}
}

// TestWebTurnHonoursAnExplicitBudget is the same real surface with the
// operator's own export in front of it: the value reaches the prompt UNCHANGED,
// which is what "a default, never an enforcement" means end to end. It is the
// companion of the surface case above — that one proves the fabricated ceiling is
// gone, this one proves the surviving value is the operator's, not a silent
// "unlimited" painted over their decision.
func TestWebTurnHonoursAnExplicitBudget(t *testing.T) {
	const explicitMinutes = 30
	home := t.TempDir()
	llm := testutil.NewMockLLM(t, testutil.Resp{Text: webBudgetTurnReply})
	defer llm.Close()

	// APPENDED, not replaced: MockEnv strips every ambient GOPI_* var, so this is
	// the one channel that declares the budget for the child, and exec keeps the
	// LAST occurrence of a duplicated key.
	env := append(testutil.MockEnv(t, home, llm.URL()), chatSessionBudgetEnv+"="+strconv.Itoa(explicitMinutes))

	p := testutil.StartGopi(t, []string{"web", "--addr=127.0.0.1:0", webToolsFlag}, "", env, "")
	defer p.Stop()
	base := p.WebBaseURL()

	sessRoot := filepath.Join(home, ".gopi", config.SessionsDirName)
	p.WaitFor("the sandboxed session JSONL", func() bool { return testutil.SessionRecordPath(sessRoot) != "" })

	stream := webPostOneTurn(t, base)
	if !strings.Contains(stream, "event: done") {
		t.Fatalf("turn did not reach `event: done` — stream=%q\nstderr:\n%s", stream, p.Stderr())
	}

	body := llm.RequestBody(1)
	want := "BUDGET: session budget " + strconv.Itoa(explicitMinutes) + "min"
	if !strings.Contains(body, want) {
		t.Errorf("the web turn does not teach the operator's %d-minute budget (%q missing): %s",
			explicitMinutes, want, budgetRuleLine(body))
	}
	if strings.Contains(body, chatBudgetUnlimitedRule) {
		t.Errorf("the web console published unlimited OVER an explicit %s=%d: %s",
			chatSessionBudgetEnv, explicitMinutes, budgetRuleLine(body))
	}
	if strings.Contains(p.Stderr(), "unset") {
		t.Errorf("a web run with an explicit budget announced a default:\n%s", p.Stderr())
	}
}

// TestWebHelpChangesNoBudget is the ORDERING half, and it is checked in the
// SOURCE because no single command line can establish it: `gopi web -h` must
// return a page and change nothing in the environment, and the publication must
// sit BELOW the argv walk's returns — while still sitting ABOVE the agent
// construction, because the system prompt is rendered from the environment at
// the moment the agent exists. One window, both halves.
func TestWebHelpChangesNoBudget(t *testing.T) {
	src, err := os.ReadFile(filepath.Join(repoRootForChatBudgetTest(t), "cmd", "agent", "cli_web.go"))
	if err != nil {
		t.Fatalf("read cli_web.go: %v", err)
	}
	text := string(src)

	call := strings.Index(text, "publishWebSessionBudgetDefault(")
	if call < 0 {
		t.Fatal("cli_web.go no longer calls publishWebSessionBudgetDefault( — the web default is unreachable")
	}
	helpReturn := strings.Index(text, "fmt.Printf(webUsage,")
	if helpReturn < 0 {
		t.Fatal("no usage page found in cli_web.go — this gate is checking nothing")
	}
	if helpReturn > call {
		t.Error("the publication sits ABOVE the usage page: `gopi web -h` would change the environment")
	}
	// The agent construction is where the prompt is assembled from the
	// environment, so a publication below it would be too late by construction:
	// the budget would be read before the default existed.
	agentBuild := strings.Index(text, "ag, err := srv.newWebAgent()")
	if agentBuild < 0 {
		t.Fatal("cli_web.go no longer builds the agent with srv.newWebAgent() — this gate is checking nothing")
	}
	if call > agentBuild {
		t.Error("the publication sits BELOW the agent construction: the system prompt would be rendered from the un-defaulted environment")
	}
}

// webPostOneTurn sends ONE user message to the running console and returns the
// SSE stream it answered with. The two surface cases above differ only in the
// environment the process was started with, so the turn itself is shared (Rule
// 14: one question, one answer).
func webPostOneTurn(t *testing.T, base string) string {
	t.Helper()
	client := &http.Client{Timeout: webBudgetTurnTimeout}
	resp, err := client.Post(base+"/api/chat", "application/json",
		strings.NewReader(`{"message":"`+webBudgetTurnPrompt+`"}`))
	if err != nil {
		t.Fatalf("turn POST failed: %v", err)
	}
	body, readErr := io.ReadAll(resp.Body)
	resp.Body.Close() //stylegate:ignore the body was drained to EOF by the read above and its error is checked on the next line; the close error of a fully-read response carries no state
	if readErr != nil {
		t.Fatalf("reading the turn stream failed: %v", readErr)
	}
	return string(body)
}
