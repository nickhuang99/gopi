package main

// RP-1 (end-user-audit slice 8) — the boundary regression test.
//
// WHAT IS PINNED. `gopi -h`'s Inputs row says `"<prompt>"  The task text`; the
// first Usage line says `Run one task from a prompt argument`. Before this fix
// the run path wrapped that text in the agent-harness frame — `[EPIC GOAL]` +
// the task-list directive + the reframe protocol, ~1000 chars of operating
// instructions — around a plain shell prompt (reproduced at the request
// boundary, 0 real tokens; devdoc/end-user-audit-slice8-runpath.md).
//
// WHERE THE ASSERTION LIVES. At the REQUEST BOUNDARY and nowhere else: the
// payload is what left the process. Reading the Agent's own flag would pass on
// a build whose caller never set it; stdout shows only the answer. So this file
// stands up a local Anthropic-shaped endpoint that RECORDS every request body
// and drives the REAL BINARY against it — the instrument the audit used, with
// no upstream host and no synthetic load (Rule 27).
//
// THE TWO ARMS ARE THE WHOLE POINT. Removing the frame everywhere would break
// the surfaces it belongs to (a dispatched row's briefing must arrive as an
// epic goal so task(action=reframe) and the task list are part of the run), so
// both are driven:
//
//	arm A — a plain prompt: the FIRST user message is the text, EXACTLY.
//	arm B — a `task run` child (tools.TaskWrapperEnv present, the wrapper's own
//	        spawn shape): the frame is STILL there.
//
// Arm A is red on the pre-fix tree. Arm B is red if the fix is "delete the
// framing". Neither arm alone is sufficient.

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/nick/gopi/internal/config"
)

const (
	// rp1PromptToken is the needle. A token the framing code cannot produce, so
	// "the text is present" and "nothing was added around it" stay separate
	// assertions.
	rp1PromptToken = "RP1-BOUNDARY-PLAIN-PROMPT-4f2a"
	// rp1ProbeTimeout bounds ONE child. A correctly-called run answers the mock
	// well under a second; the ceiling makes a wedged probe its own failure
	// instead of a parked package budget.
	rp1ProbeTimeout = 30 * time.Second
	// rp1PromptFlag is the prompt argument the probe passes. A `task run` child
	// really receives its briefing on STDIN (cmd/agent/cli_tasks.go binds
	// cmd.Stdin = strings.NewReader(briefing)); the flag is used here because
	// the argv spelling is irrelevant to the measurement — the subject is the
	// FIRST USER MESSAGE, and what makes the child a task child is the
	// environment the real wrapper sets, which this probe sets the same way.
	rp1PromptFlag = "--prompt"
	// rp1HeadChars caps an error message's quotation of a payload.
	rp1HeadChars = 240
)

// The frame's distinguishing phrases are the SAME constants the help page names
// (config.FrameMarker*): a test that typed them again could pass while the page
// and the framing drifted apart, which is the defect class this slice is in.
const (
	rp1EpicMarker    = config.FrameMarkerEpic
	rp1TaskListMark  = config.FrameMarkerTaskList
	rp1ReframeMarker = config.FrameMarkerReframe
)

// rp1Reading is one model request as it arrived at the boundary: the first user
// message, the message count, and the body size (a frame can ride in a later
// message as easily as in the first).
//
// All concatenates EVERY message's text, and the frame scan reads it rather than
// First alone — the frame is a payload property, not a position, so a wire that
// merely moved it off the front must not pass.
type rp1Reading struct {
	Messages int
	First    string
	All      string
	Bytes    int
}

// rp1Probe drives the real binary against a local recording endpoint in a
// throwaway profile and returns every request the child made.
//
// The environment is built from EMPTY plus an explicit list — `env -i`
// semantics: a probe inside a task run inherits ~20 GOPI_* vars, and a probe
// that inherited them would be measuring its parent rather than the run path.
// That distinction is not academic here: it is how the original defect had to
// be reproduced before it was believed.
func rp1Probe(t *testing.T, prompt string, extraEnv ...string) []rp1Reading {
	t.Helper()
	bin := gopiTestBinPath(t)
	home := t.TempDir()
	// The profile root gopi actually resolves: $GOPI_HOME/.gopi (Rule 34).
	profileDir := filepath.Join(home, ".gopi")
	if err := os.MkdirAll(profileDir, 0o755); err != nil {
		t.Fatalf("mkdir profile %s: %v", profileDir, err)
	}

	var (
		mu  sync.Mutex
		rec []rp1Reading
	)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			http.Error(w, "read body", http.StatusBadRequest)
			return
		}
		var req struct {
			Messages []struct {
				Content json.RawMessage `json:"content"`
			} `json:"messages"`
		}
		// A body that does not decode is recorded as an empty reading rather
		// than skipped: "the probe sent something unreadable" is a result.
		_ = json.Unmarshal(body, &req) //stylegate:ignore best-effort decode of an optional field; the zero value is the documented fallback
		reading := rp1Reading{Messages: len(req.Messages), Bytes: len(body)}
		for _, m := range req.Messages {
			reading.All += rp1Flatten(m.Content)
		}
		if len(req.Messages) > 0 {
			reading.First = rp1Flatten(req.Messages[0].Content)
		}
		mu.Lock()
		rec = append(rec, reading)
		mu.Unlock()

		w.Header().Set("Content-Type", "text/event-stream")
		for _, frame := range []struct{ event, data string }{
			{"message_start", `{"type":"message_start","message":{"id":"m1","type":"message","role":"assistant","model":"echo","content":[],"stop_reason":null,"usage":{"input_tokens":1,"output_tokens":1}}}`},
			{"content_block_start", `{"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}`},
			{"content_block_delta", `{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"RP1-ECHO-OK"}}`},
			{"content_block_stop", `{"type":"content_block_stop","index":0}`},
			{"message_delta", `{"type":"message_delta","delta":{"stop_reason":"end_turn"},"usage":{"output_tokens":3}}`},
			{"message_stop", `{"type":"message_stop"}`},
		} {
			fmt.Fprintf(w, "event: %s\ndata: %s\n\n", frame.event, frame.data)
		}
	}))
	defer srv.Close()

	args := []string{
		"--base-url", srv.URL,
		"--api-key", "rp1-probe-key",
		"--db", filepath.Join(profileDir, "memory.db"),
		rp1PromptFlag, prompt,
	}
	cmd := exec.Command(bin, args...)
	cmd.Env = append([]string{
		"PATH=" + os.Getenv("PATH"),
		"HOME=" + home,
		"GOPI_HOME=" + home,
		// The endpoint var is the belt to --base-url's braces: the probe must
		// never reach a real host. The brew/sync timers are off so the child
		// leaves nothing running behind it.
		"DEEPSEEK_BASE_URL=" + srv.URL,
		"GOPI_BREW_INTERVAL_SEC=0",
	}, extraEnv...)
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &out
	if err := cmd.Start(); err != nil {
		t.Fatalf("start probe %v: %v", args, err)
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	select {
	case <-done:
	case <-time.After(rp1ProbeTimeout):
		_ = cmd.Process.Kill() //stylegate:ignore kills the child process the test spawned and is tearing down
		<-done
		t.Fatalf("probe `gopi %s` did not finish within %s — a run that cannot answer a local mock is itself the finding:\n%s",
			strings.Join(args, " "), rp1ProbeTimeout, rp1Head(out.String()))
	}
	mu.Lock()
	defer mu.Unlock()
	return append([]rp1Reading(nil), rec...)
}

// rp1Flatten reads a message's content field: a plain string, or the
// content-block array the client really sends.
func rp1Flatten(raw json.RawMessage) string {
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return s
	}
	var blocks []struct {
		Text string `json:"text"`
	}
	if json.Unmarshal(raw, &blocks) == nil {
		var b strings.Builder
		for _, blk := range blocks {
			b.WriteString(blk.Text)
		}
		return b.String()
	}
	return ""
}

// rp1Head trims a body for an error message.
func rp1Head(s string) string {
	if len(s) <= rp1HeadChars {
		return s
	}
	return s[:rp1HeadChars] + "…"
}

// rp1Unframed fails when a reading carries the epic frame. It is one predicate
// because three arms of the fix can each put it back — the marker, the plan
// directive, or the reframe protocol — and a fix that removes only one of them
// is still framed.
//
// IT SCANS EVERY MESSAGE (r.All), not the first user message alone. The frame
// is a payload property, not a position: a fix that merely reordered the wire so
// the frame landed in message 2 would leave `gopi -h`'s "The task text" promise
// broken while an assertion scoped to one message went green.
func rp1Unframed(t *testing.T, idx int, r rp1Reading) {
	t.Helper()
	for _, mark := range []struct{ needle, what string }{
		{rp1EpicMarker, "the epic-goal marker"},
		{rp1TaskListMark, "the task-list directive"},
		{rp1ReframeMarker, "the reframe protocol"},
	} {
		if strings.Contains(r.All, mark.needle) {
			t.Errorf("request %d carries %s (%q) — a plain prompt must reach the model with nothing added around it:\n%s",
				idx, mark.what, mark.needle, rp1Head(r.All))
		}
	}
}

// TestRunPathPlainPromptIsVerbatim is ARM A — the fix RP-1 asks for.
//
// RED ON THE PRE-FIX TREE TWO WAYS, and the two reds are different defects:
//
//	(1) `[EPIC GOAL]\n<token>\n\nBased on this Epic, …[EPIC REFRAME] …` — the
//	    token did not start the message at all (the harness frame on a shell
//	    prompt).
//	(2) `<token>[system] [t+0s] — first request of this session. session
//	    budget: …` — the frame gone, the prompt at offset 0, and the per-request
//	    turn stamp SPLICED INTO it. requestMessages appends the stamp as a
//	    user-role message and the client merges consecutive user text
//	    (deepseek/claude_client.go BuildClaudeRequest keeps roles alternating),
//	    so on this surface "appended" means "inside the operator's own text".
//
// THE ASSERTION IS EQUALITY, and this is the shape the product earns:
// turnStampForRequest (internal/agent/clock.go) exempts a plain prompt's FIRST
// request while still minting the stamp, so the run keeps its time base and the
// model receives the operator's text and nothing else. Equality is what `gopi
// -h` promises ("`\"<prompt>\"`  The task text"): a prefix check would pass on
// `<prompt><anything>` — which is exactly defect (2), i.e. a prompt with
// operating text glued to it, and it is satisfied by a message that is not the
// prompt at all.
//
// KEPT, not weakened: a mid-history reading of this file asserted
// `strings.HasPrefix(got, rp1PromptToken)` while the product fix was not on the
// branch — the assertion and the product disagreed, and the test was green on
// the half that did not. rp1Unframed below carries the frame half of the same
// claim across EVERY message, so this arm stays two-sided.
func TestRunPathPlainPromptIsVerbatim(t *testing.T) {
	// GOPI_NO_ENFORCE_MEMORY=1 is the run's own knob for the harness memory
	// checkpoints (loop.go's NoEnforceMemory: GOPI_NO_ENFORCE_MEMORY /
	// --no-enforce-memory). It is set so this arm measures the PROMPT's fate
	// alone: without it the harness appends its TASK-START checkpoint to the
	// first turn, which is a separate mechanism the help page names and this
	// test is not about. With it, "the text is the model's first user message as
	// typed" is checkable byte for byte.
	//
	// THE THIRD ADDITION — the per-request turn stamp — was the last to be
	// removed, and it is the informative half of this arm's history: setting
	// GOPI_NO_ENFORCE_MEMORY killed the checkpoint but NOT the stamp, because the
	// stamp is appended by requestMessages as a USER message and the wire merges
	// consecutive user text into the preceding one (claude_client.go). So the
	// first user message read
	//   "<token>[system] [t+0s] — first request of this session. session budget: …"
	// (measured 2026-10-12, printed verbatim in the failure below). A harness
	// message is not ONE mechanism: each injector has to be told which surfaces it
	// may appear on, and only the product can decide that — see clock.go
	// (turnStampForRequest), which exempts a plain prompt's FIRST request and
	// nothing else, with the in-process repro in
	// internal/agent/plainprompt_firstmessage_test.go
	// (TestPlainPrompt_FirstRequestCarriesNoTurnStamp).
	readings := rp1Probe(t, rp1PromptToken, "GOPI_NO_ENFORCE_MEMORY=1")
	if len(readings) == 0 {
		t.Fatalf("the probe made no model request — the run never reached the endpoint, so the payload claim is unmeasured (an unmeasured claim is not a passing one)")
	}
	if got := readings[0].First; got != rp1PromptToken {
		t.Errorf("the FIRST user message is not the prompt verbatim:\n got (%d chars): %q\nwant (%d chars): %q\n"+
			"`gopi -h` promises \"The task text\", so anything in front of, after or around it is the RP-1 defect "+
			"(the turn stamp is exempted on this surface's first request by internal/agent/clock.go's turnStampForRequest).",
			len(got), rp1Head(got), len(rp1PromptToken), rp1PromptToken)
	}
	for i, r := range readings {
		rp1Unframed(t, i, r)
	}
}

// TestRunPathTaskChildKeepsTheFraming is ARM B — the surface the frame BELONGS to.
//
// RED IF THE FIX IS "DELETE THE FRAMING": a dispatched row's briefing must still
// arrive as an epic goal, because task(action=reframe) and the task list are
// that surface's contract, not decoration.
func TestRunPathTaskChildKeepsTheFraming(t *testing.T) {
	// The wrapper's own spawn shape: the identity env is what makes the child a
	// TASK child (cmd/agent/cli_tasks.go writes tools.TaskWrapperEnv). The probe
	// owns its ledger (--db above) and the auto-row registration is off so a
	// probe run leaves no row behind. (The child-side close-out kill switch that
	// used to sit here is gone with the seam it disabled — task 01a16f7c — and
	// with it the last reason to name that variable.)
	readings := rp1Probe(t, rp1PromptToken,
		"GOPI_TASK_WRAPPER_ID=t-rp1-probe",
		"GOPI_TASK_RUN_ID=t-rp1-probe",
		"GOPI_AUTO_TASK_REGISTER=off",
	)
	if len(readings) == 0 {
		t.Fatalf("the task-child probe made no model request — the framing claim is unmeasured")
	}
	first := readings[0].First
	if first == "" {
		t.Fatalf("the task-child probe sent no user message at all — the framing claim is unmeasured")
	}
	if !strings.Contains(first, rp1EpicMarker) {
		t.Errorf("a task child's first user message does not carry %q — the frame IS the task surface's contract, and removing it everywhere is the over-correction this arm exists to catch:\n%s",
			rp1EpicMarker, rp1Head(first))
	}
	if !strings.Contains(first, rp1PromptToken) {
		t.Errorf("the task child's briefing vanished from the first user message:\n%s", rp1Head(first))
	}
	if !strings.Contains(first, rp1TaskListMark) {
		t.Errorf("the task-list directive is gone from a task child's first message:\n%s", rp1Head(first))
	}
}

// TestHelpStatesWhereTheFramingApplies pins the OTHER half of the fix: the page
// must make the difference predictable — a prompt is read verbatim, a
// dispatched run is framed. Without that sentence the page promises one thing
// while one surface does another, which was the defect.
func TestHelpStatesWhereTheFramingApplies(t *testing.T) {
	bin := gopiTestBinPath(t)
	cmd := exec.Command(bin, "-h")
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &out
	if err := cmd.Run(); err != nil {
		t.Fatalf("`gopi -h` failed: %v\n%s", err, rp1Head(out.String()))
	}
	page := out.String()
	// The help WRAPS its rows at the terminal column, so a long sentence arrives
	// with newlines inside it: both sides are flattened to single spaces before
	// the comparison, which is what makes "the page carries this sentence" a
	// statement about the sentence rather than about the wrap width.
	flatPage := strings.Join(strings.Fields(page), " ")
	// The claim internal/config exports is the SAME sentence the run path's
	// difference is stated in; the page must carry it, and it must name every
	// marker the frame is built from.
	for _, want := range []string{
		config.FrameNoteText,
		rp1EpicMarker,
		rp1TaskListMark,
		rp1ReframeMarker,
	} {
		if !strings.Contains(flatPage, strings.Join(strings.Fields(want), " ")) {
			t.Errorf("`gopi -h` does not carry %q — the page and the framing disagree about what a prompt is:\n%s",
				rp1Head(want), rp1Head(page))
		}
	}
}
