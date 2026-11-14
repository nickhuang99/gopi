package agent

// RP-1 REGRESSION (the MINIMAL repro, Rule 5) — a plain prompt's first user
// message carries the prompt and NOTHING ELSE.
//
// WHY THIS FILE EXISTS AND WHAT IT REPLACES AS EVIDENCE. The failure was found
// at the REQUEST BOUNDARY by cmd/agent's out-of-process probe
// (TestRunPathPlainPromptIsVerbatim, cli_runpath_boundary_rp1_test.go:246: the
// FIRST user message is not the prompt verbatim). That probe spawns the real
// binary per case and is therefore the RIGHT acceptance instrument — but it is
// a slow one, so the diagnosis needs a case an agent can afford to run and to
// keep green in the package that OWNS the defect. This is that case: in
// process, no child, no profile, no ledger.
//
// THE DEFECT (root cause, named). internal/agent/loop.go builds the first turn
// as the caller's text and only wraps it when !plainPrompt — so on the
// RunPrompt path the EPIC frame is correctly absent. But startOfRunFor
// (hippocampus.go) then calls maybeInjectSelfRecordRule
// (selfrecord.go), which does AddUserMessage(selfRecordRule) — a ~800-char
// harness contract. TWO CONSECUTIVE USER MESSAGES are not what the model
// receives: internal/deepseek/claude_client.go coalesces consecutive user text
// messages ("Merge consecutive user text messages — the API requires
// alternating roles", claude_client.go:560), appending the rule's text
// block to the first message's block list. The prompt is therefore no longer
// the first user message "as typed"; it is the first 30 characters of an
// 883-character message. The assert then reads a prompt that is not verbatim,
// and the verdict does not depend on load at all (it reproduces at -count=1,
// and it is 100% deterministic).
//
// THE FIX'S CONTRACT, pinned twice (arm A / arm B), because the cheap way to
// make it pass is to stop injecting the rule at all — which would delete the
// self-recording feature:
//
//	arm A — plainPrompt (RunPrompt, the shell path): the rule is NOT injected,
//	        so the first user message is the prompt, byte for byte.
//	arm B — a `task run` child (tools.TaskWrapperEnv set): the rule IS still
//	        injected; the feature survives.
//
// The boundary between them is exactly the one RunPrompt already draws
// (a.taskChildRowID == "" ⇒ plainPrompt), so no new concept is introduced.
//
// Gated the same way as the other self-record cases (selfrecord_gate_test.go):
// GOPI_NO_ENFORCE_MEMORY is NOT set here — this file is ABOUT that second
// message, and an A/B isolation switch would remove the very thing under test.

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/nickhuang99/gopi/internal/deepseek"
	"github.com/nickhuang99/gopi/internal/tools"
)

// ppPromptToken is the needle: a token neither the system prompt nor any
// harness message can produce, so "the prompt is present" and "nothing else
// was added in front of/after it" stay separate assertions.
const ppPromptToken = "PP-FIRSTMESSAGE-PLAIN-9c31"

// ppCapture records the FIRST request's first user message as it arrived at the
// boundary: its text blocks, in wire order.
//
// ONLY THE FIRST REQUEST IS KEPT, deliberately: the loop may make further
// rounds (a nudge, a continuation) and every one of them carries the same first
// message plus whatever the loop appended. Keeping the first is what makes the
// claim "the prompt IS the first user message" a statement about the turn the
// prompt was sent in.
type ppCapture struct {
	mu       sync.Mutex
	blocks   [][]string
	captured bool
}

// ppServer answers with a one-block Claude SSE stream and records the FIRST
// request's first user message. The answer is deliberately trivial: the subject
// is the OUTBOUND payload, not the loop's behaviour after it. Only the first
// request carries text — the follow-up rounds answer with an empty block, which
// is how the loop reaches its normal end (the shape the package's other
// in-process cases use) instead of exhausting the iteration budget.
func ppServer(cap *ppCapture) *httptest.Server {
	var rounds int32
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := atomic.AddInt32(&rounds, 1)
		texts := ppRequestFirstUserTexts(r)
		cap.mu.Lock()
		if !cap.captured {
			cap.blocks = texts
			cap.captured = true
		}
		cap.mu.Unlock()
		answer := ""
		if n == 1 {
			answer = "ok"
		}
		writeClaudeSSE(w, "message_start", `{"type":"message_start","message":{"id":"m1","usage":{"input_tokens":10,"output_tokens":0}}}`)
		writeClaudeSSE(w, "content_block_start", `{"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}`)
		writeClaudeSSE(w, "content_block_delta", fmt.Sprintf(`{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":%q}}`, answer))
		writeClaudeSSE(w, "content_block_stop", `{"type":"content_block_stop","index":0}`)
		writeClaudeSSE(w, "message_delta", `{"type":"message_delta","delta":{"stop_reason":"end_turn","stop_sequence":null}}`)
		writeClaudeSSE(w, "message_stop", `{"type":"message_stop"}`)
	}))
}

// ppRequestFirstUserTexts flattens one request's FIRST user message to its text
// blocks, in wire order.
func ppRequestFirstUserTexts(r *http.Request) [][]string {
	body, err := io.ReadAll(r.Body)
	if err != nil {
		return nil
	}
	var req struct {
		Messages []struct {
			Role    string `json:"role"`
			Content []struct {
				Text string `json:"text"`
			} `json:"content"`
		} `json:"messages"`
	}
	if err := json.Unmarshal(body, &req); err != nil { //stylegate:ignore best-effort decode of an optional field; the zero value is the documented fallback
		return nil
	}
	var out [][]string
	for _, m := range req.Messages {
		if m.Role != "user" {
			continue
		}
		var texts []string
		for _, b := range m.Content {
			texts = append(texts, b.Text)
		}
		out = append(out, texts)
	}
	return out
}

// ppAgent builds the MINIMAL agent that reaches runEngine's non-resume branch:
// an httptest endpoint and an in-memory context.
//
// NO SessionLog and NO ledger. The two harness injectors this file is about run
// regardless of the logger (it only decides whether the injected text is ALSO
// archived to the session JSONL), while every ledger-touching path — the auto
// row registration, the heartbeat, the memory checkpoints — is either guarded
// by a nil logger or pinned to a throwaway home by newTestAgentEnv. That is
// Rule 32's requirement honoured by NOT reaching a database at all, rather than
// by reaching a hand-rolled one.
func ppAgent(t *testing.T, url string) *Agent {
	t.Helper()
	newTestAgentEnv(t)
	ag := &Agent{
		Client: &deepseek.Client{
			BaseURL:    url,
			APIKey:     "test-key",
			HTTPClient: &http.Client{Transport: &http.Transport{}},
		},
		Ctx:   &Context{},
		Model: "deepseek-v4-pro",
		// NoEnforceMemory is the same A/B isolation the boundary probe uses
		// (GOPI_NO_ENFORCE_MEMORY=1, cli_runpath_boundary_rp1_test.go's arm A):
		// the harness's TASK-START memory_search checkpoint ALSO appends to the
		// first turn — governed behaviour the help page names ("The run is still
		// governed by the agent's system prompt and its own checkpoints"), NOT
		// the defect this file reproduces. With it on, "the first user message
		// is the prompt, byte for byte" is checkable at all; the two subjects
		// stay separate, exactly as they do at the request boundary.
		NoEnforceMemory: true,
		MaxIterations:   1,
	}
	ag.Logger = log.New(io.Discard, "", 0)
	return ag
}

// ppRun drives the run and classifies its outcome: the harness's own
// iteration budget is the DETERMINISTIC end of a probe that answers with a
// non-substantive reply, and it is accepted — the first request is the subject,
// and the answer is filler. Any OTHER error is a real failure of the run path
// and is reported.
func ppRun(t *testing.T, ag *Agent, what string) {
	t.Helper()
	if _, err := ag.RunPrompt(ppPromptToken); err != nil &&
		!strings.Contains(err.Error(), "max iterations") {
		t.Fatalf("%s: %v", what, err)
	}
}

// ppFirstUserTexts returns the first request's first user message text blocks,
// failing when the probe never reached the endpoint (an unmeasured claim is not
// a passing one).
func ppFirstUserTexts(t *testing.T, cap *ppCapture) []string {
	t.Helper()
	cap.mu.Lock()
	defer cap.mu.Unlock()
	if !cap.captured {
		t.Fatalf("the agent made no model request — the first-message claim is unmeasured")
	}
	if len(cap.blocks) == 0 {
		t.Fatalf("the first request carried no user message at all")
	}
	return cap.blocks[0]
}

// TestPlainPrompt_FirstUserMessageIsThePromptAlone is ARM A — RED BEFORE THE FIX.
//
// Pre-fix observation (the failure this case exists to keep out):
//
//	first user message text = "PP-FIRSTMESSAGE-PLAIN-9c31[system] SELF-RECORDING: end EVERY response …"
//
// i.e. the prompt survived with the self-record rule COALESCED onto it, and the
// message is no longer the prompt "as typed".
func TestPlainPrompt_FirstUserMessageIsThePromptAlone(t *testing.T) {
	cap := &ppCapture{}
	srv := ppServer(cap)
	defer srv.Close()

	ag := ppAgent(t, srv.URL)
	ppRun(t, ag, "RunPrompt")

	blocks := ppFirstUserTexts(t, cap)
	if len(blocks) != 1 {
		t.Errorf("the first user message carries %d text blocks, want 1 — a plain prompt is ONE text and nothing else:\n%q",
			len(blocks), blocks)
	}
	if got := strings.Join(blocks, ""); got != ppPromptToken {
		t.Errorf("the first user message is not the prompt verbatim (the RP-1 defect):\n got (%d chars): %q\nwant (%d chars): %q",
			len(got), headChars(got), len(ppPromptToken), ppPromptToken)
	}
	// The named mechanism, asserted DIRECTLY (not inferred from the length):
	// the injected text is selfRecordRule, and its presence is a pointer at the
	// injector rather than at the frame builder — the frame builder is already
	// correct on this path, which is what made the defect look like nothing.
	for _, b := range blocks {
		if strings.Contains(b, selfRecordRule) {
			t.Errorf("the self-recording contract (selfRecordRule, injected by maybeInjectSelfRecordRule) reached a PLAIN PROMPT's first user message — startOfRunFor (hippocampus.go) calls it unconditionally, and the wire coalesces it into the prompt's own message:\n%q",
				headChars(b))
		}
	}
}

// TestPlainPrompt_TaskChildStillGetsTheSelfRecordContract is ARM B — the
// counterweight. A fix that simply stops injecting the contract would make arm
// A pass while deleting the feature, and this case is what refuses that: a
// `task run` child keeps it.
//
// The child's identity is the same environment the wrapper's spawn seam sets
// (tools.TaskWrapperEnv, read once in NewAgent) — so this case drives the SAME
// boundary NewAgent reads, not a private flag.
func TestPlainPrompt_TaskChildStillGetsTheSelfRecordContract(t *testing.T) {
	t.Setenv(tools.TaskWrapperEnv, "t-pp-child")
	cap := &ppCapture{}
	srv := ppServer(cap)
	defer srv.Close()

	ag := ppAgent(t, srv.URL)
	ag.taskChildRowID = strings.TrimSpace(os.Getenv(tools.TaskWrapperEnv))
	ppRun(t, ag, "RunPrompt (task child)")

	blocks := ppFirstUserTexts(t, cap)
	joined := strings.Join(blocks, "")
	if !strings.Contains(joined, selfRecordRule) {
		t.Errorf("a task child's first user message does not carry the self-recording contract — the feature was deleted rather than scoped, which is the over-correction this arm exists to refuse:\n%q",
			headChars(joined))
	}
	if !strings.Contains(joined, ppPromptToken) {
		t.Errorf("a task child's briefing vanished from its first user message:\n%q", headChars(joined))
	}
}

// headChars trims a payload for an error message.
func headChars(s string) string {
	const max = 240
	if len(s) <= max {
		return s
	}
	return s[:max] + fmt.Sprintf("…(+%d chars)", len(s)-max)
}

// TestPlainPrompt_LiveClockKeepsTheFirstMessageVerbatim is ARM D — the same
// claim as arm A, but with the SESSION CLOCK WIRED, which is the state the
// product runs in and the state arms A/B never entered.
//
// WHY IT IS A SEPARATE ARM (the measured reason). ppAgent leaves Clock nil, so
// the two arms above prove the FRAME and the SELF-RECORD rule are absent —
// they never exercise the third injector, requestMessages' turn stamp
// (clock.go), because a nil Clock renders no stamp at all. The end-to-end probe
// was therefore the ONLY place that defect could appear, and it did: measured
// at the request boundary on this ground, the first user message arrived as
//
//	"RP1-BOUNDARY-PLAIN-PROMPT-4f2a[system] [t+0s] @ 2026-11-07T05:52:46+08:00
//	 — +0s since your previous request. session budget: 0s elapsed of 1h0m0s (0% used)."
//
// — 158 characters where the prompt is 30. Note the DISCRIMINATING detail:
// that stamp is the GAP arm ("+0s since your previous request"), not the first
// arm ("first request of this session"), and that is what made the exemption
// look correct while the wire was wrong. turnStampForRequest's exemption is
// keyed on Clock.TurnStamp().First, and the loop assembled the round's request
// TWICE — once for the pairing-invariant proof and the debug print, then again
// inside the send-time gate — so the first assembly consumed the exemption and
// the assembly that actually went on the wire was already the session's
// SECOND request. The exemption was correct and unreachable.
//
// THE ASSERTION IS EQUALITY, for the same reason cli_runpath_boundary_rp1_test.go's
// is: `gopi -h` promises `"<prompt>"  The task text`, and a prefix check passes
// on exactly the defect (a prompt with operating text glued to it).
func TestPlainPrompt_LiveClockKeepsTheFirstMessageVerbatim(t *testing.T) {
	cap := &ppCapture{}
	srv := ppServer(cap)
	defer srv.Close()

	ag := ppAgent(t, srv.URL)
	// The ONLY difference from arm A: the clock a real run has.
	ag.Clock = NewSessionClock()
	ppRun(t, ag, "RunPrompt (live clock)")

	blocks := ppFirstUserTexts(t, cap)
	if len(blocks) != 1 {
		t.Errorf("the first user message carries %d text blocks, want 1 — a plain prompt is ONE text and nothing else:\n%q",
			len(blocks), blocks)
	}
	got := strings.Join(blocks, "")
	if got != ppPromptToken {
		t.Errorf("the first user message is not the prompt verbatim with a live clock:\n got (%d chars): %q\nwant (%d chars): %q\n"+
			"The turn stamp is exempted on a plain prompt's FIRST request (clock.go turnStampForRequest); a GAP stamp here means a build nobody sent already spent that exemption — one assembly per round is the invariant.",
			len(got), headChars(got), len(ppPromptToken), ppPromptToken)
	}
}
