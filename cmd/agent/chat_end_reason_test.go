package main

// chat_end_reason_test.go — the session_end record must say HOW the run ended
// (task 01a163f8).
//
// THE DEFECT THIS GUARDS, measured on master 5758f23 against the real binary
// (evidence: the task's session directory, probe-baseline.txt):
//
//	leg      action        returncode  session_end
//	quit     "/quit\n"     0           {"exit_code":0,…}      ← byte-identical
//	eof      closed stdin  0           {"exit_code":0,…}      ← to each other
//	sighup   killpg HUP    1           exit 1 + error.category=signal
//
// Three different endings produced records a reader could not tell apart. The
// epic's own words: "不知道为什么退出是最坏结果". Every leg DID reach its
// close-out (so this is not the "no record at all" bug memory #14109 measured),
// and that is exactly why the gap went unnoticed — the record existed and said
// nothing useful.
//
// The fix carries the ending as session_end's own `reason` (and `signal` /
// `eof_kind` where each refines it). The gates below pin all three layers that
// have to agree for that to be true:
//
//  1. the input layer's WORD for the event (lineedit.EOFKind*, chatEndKind*),
//  2. the mapping from that word to a session_end cause (chatEndCause), and
//  3. the precedence that lets a signal already recorded on the agent beat the
//     caller's word — because the watcher knows the signal and the close-out
//     does not.

import (
	"encoding/json"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/nickhuang99/gopi/internal/agent"
	"github.com/nickhuang99/gopi/internal/deepseek"
	"github.com/nickhuang99/gopi/internal/lineedit"
	"github.com/nickhuang99/gopi/internal/sessionevent"
	"github.com/nickhuang99/gopi/internal/sessionlayout"
)

// endReasonTestModel is the model name the harness's agent declares; it only has
// to be a value the mock provider accepts.
const endReasonTestModel = "test-model"

// TestChatEndCause_MapsEveryKind pins the mapping layer: each word the input
// layer can produce resolves to exactly one session_end reason, and no two legs
// share a (reason, eof_kind) pair — which is the property the defect violated,
// since /quit and a closed stdin both produced `exit_code:0` and nothing else.
func TestChatEndCause_MapsEveryKind(t *testing.T) {
	cases := []struct {
		kind       string
		wantReason string
		wantEOF    string
		// distinct marks the legs the record must be able to TELL APART. The
		// unknown bucket is deliberately shared (see below), so it is checked
		// for its value but not for uniqueness.
		distinct bool
	}{
		{lineedit.EOFKindKey, sessionevent.SessionEndReasonEof, sessionevent.SessionEndEOFKindKey, true},
		{lineedit.EOFKindSource, sessionevent.SessionEndReasonEof, sessionevent.SessionEndEOFKindSource, true},
		{chatEndKindInterrupt, sessionevent.SessionEndReasonInterrupt, "", true},
		// The cooked reader has no key stream to ask, so it says "unknown"
		// rather than borrowing Ctrl+D's word — and an unknown WORD from a future
		// input layer joins that same bucket on purpose. Both are the admitted
		// unknown, which is one fact, not two: a wrong specific answer would be
		// worse than the honest shared one (Rule 15).
		{chatEndKindCookedEnd, sessionevent.SessionEndReasonEof, sessionevent.SessionEndEOFKindUnknown, false},
		{"", sessionevent.SessionEndReasonEof, sessionevent.SessionEndEOFKindUnknown, false},
		{"something-new", sessionevent.SessionEndReasonEof, sessionevent.SessionEndEOFKindUnknown, false},
	}
	seen := map[string]string{}
	for _, c := range cases {
		got := chatEndCause(c.kind)
		if got.Reason != c.wantReason || got.EOFKind != c.wantEOF {
			t.Errorf("chatEndCause(%q) = {reason:%q eof_kind:%q}, want {%q %q}",
				c.kind, got.Reason, got.EOFKind, c.wantReason, c.wantEOF)
			continue
		}
		if !c.distinct {
			continue
		}
		key := got.Reason + "/" + got.EOFKind
		if prev, dup := seen[key]; dup {
			t.Errorf("kinds %q and %q map to the SAME record (%s) — the defect is the collapse",
				prev, c.kind, key)
		}
		seen[key] = c.kind
	}
}

// TestChatEnd_SessionEndRecordsTheCause drives the REAL REPL (runChatWithInput,
// the same function every face calls) to its close-out through each input event
// it can be given in-process, and reads the session_end the run itself wrote.
//
// WHY THROUGH THE REPL AND NOT A UNIT CALL: the bug was a MISSING PASS, not a
// wrong value — the close-out had a reason list it could have written and no
// caller supplied one. A test that called agent.CloseChatSession directly would
// have passed against the defect.
func TestChatEnd_SessionEndRecordsTheCause(t *testing.T) {
	cases := []struct {
		name       string
		input      string
		wantReason string
		wantEOF    string
	}{
		{"quit", "/quit\n", sessionevent.SessionEndReasonQuit, ""},
		{"exit", "/exit\n", sessionevent.SessionEndReasonQuit, ""},
		// The cooked reader has no key stream, so which byte ended the input is
		// genuinely unknowable there — it says so rather than borrowing Ctrl+D's
		// word, and that sentence is the assertion.
		{"eof", "", sessionevent.SessionEndReasonEof, sessionevent.SessionEndEOFKindUnknown},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			end := driveChatEnding(t, c.input)
			if end.Reason != c.wantReason {
				t.Fatalf("session_end reason = %q, want %q (the whole event: %v)",
					end.Reason, c.wantReason, end.raw)
			}
			if end.EOFKind != c.wantEOF {
				t.Errorf("session_end eof_kind = %q, want %q", end.EOFKind, c.wantEOF)
			}
			if end.Count != 1 {
				t.Errorf("session_end count = %d, want exactly 1", end.Count)
			}
		})
	}
}

// TestChatEnd_SignalBeatsTheCallersWord pins the PRECEDENCE: the watcher records
// the signal on the goroutine that decoded it (the only moment the fact exists),
// and the close-out caller states the leg it observed. A signal that actually
// arrived is the more specific fact and must win, or a SIGTERM would be filed as
// whatever reason the loop happened to be returning with.
func TestChatEnd_SignalBeatsTheCallersWord(t *testing.T) {
	end := driveChatEndingWithAgent(t, "/quit\n", func(ag *agent.Agent) {
		ag.CloseSignal = "SIGHUP"
	})
	if end.Reason != sessionevent.SessionEndReasonSignalled {
		t.Fatalf("session_end reason = %q, want %q (the whole event: %v)",
			end.Reason, sessionevent.SessionEndReasonSignalled, end.raw)
	}
	if end.Signal != "SIGHUP" {
		t.Errorf("session_end signal = %q, want SIGHUP — the reason says HOW, the signal says WHICH", end.Signal)
	}
}

// endRecord is the session_end line a run wrote, decoded far enough to judge it.
type endRecord struct {
	Count   int
	Reason  string
	Signal  string
	EOFKind string
	raw     map[string]interface{}
}

// driveChatEnding runs one REPL session that ends on `input` and returns its
// session_end. An empty input is a reader that is already at EOF — the leg a
// closed stdin takes.
func driveChatEnding(t *testing.T, input string) endRecord {
	t.Helper()
	return driveChatEndingWithAgent(t, input, nil)
}

// driveChatEndingWithAgent is driveChatEnding with a hook that may mutate the
// agent before the run starts (the seam the signal-precedence case needs).
func driveChatEndingWithAgent(t *testing.T, input string, mutate func(*agent.Agent)) endRecord {
	t.Helper()
	prevFactory := chatAgentFactory
	t.Cleanup(func() { chatAgentFactory = prevFactory })

	t.Setenv("GOPI_HOME", t.TempDir())
	t.Setenv("GOPI_MEMORY_DB", "")

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.ReadAll(r.Body) //stylegate:ignore bounded read of a test request body; nothing is asserted on it
		textSSE(w, "ok")
	}))
	t.Cleanup(srv.Close)

	sl, err := agent.NewSessionLogger(endReasonTestModel, "")
	if err != nil {
		t.Fatalf("NewSessionLogger: %v", err)
	}
	t.Cleanup(func() { sl.Close() }) //stylegate:ignore test teardown of a logger this test opened
	recordPath := sl.Path()

	// The same agent literal, field for field, that the tree's other REPL
	// harnesses build (chat_boundary_test.go): the production path minus the
	// terminal, so this test drives exactly what a real run drives.
	ag := &agent.Agent{
		Client:     &deepseek.Client{BaseURL: srv.URL, APIKey: "test-key", HTTPClient: srv.Client()},
		Ctx:        &agent.Context{},
		Model:      endReasonTestModel,
		Timeout:    0,
		SessionLog: sl,
		Logger:     log.New(io.Discard, "", 0),
	}
	ag.PersistChatSession = true
	if mutate != nil {
		mutate(ag)
	}
	chatAgentFactory = func(string) (*agent.Agent, error) { return ag, nil }

	in, closeIn := newChatInputCooked(strings.NewReader(input), io.Discard)
	_ = runChatWithInput([]string{"--no-editor"}, in)
	closeIn()

	return readEndRecord(t, recordPath)
}

// readEndRecord decodes the session_end line out of a session log this test's
// own run wrote. The record container is resolved through sessionlayout — the
// SAME derivation the writers use — so this reader cannot pass by looking in a
// place the run never wrote to.
func readEndRecord(t *testing.T, recordPath string) endRecord {
	t.Helper()
	if recordPath == "" {
		recordPath = sessionlayout.RecordPath(sessionlayout.RecordContainer(recordPath))
	}
	data, err := os.ReadFile(recordPath)
	if err != nil {
		t.Fatalf("read session log %q: %v", recordPath, err)
	}
	var out endRecord
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		var ev map[string]interface{}
		if json.Unmarshal([]byte(line), &ev) != nil {
			continue
		}
		if ev["type"] != sessionevent.EventTypeSessionEnd {
			continue
		}
		out.Count++
		out.raw = ev
		out.Reason, _ = ev[sessionevent.SessionEndFieldReason].(string)
		out.Signal, _ = ev[sessionevent.SessionEndFieldSignal].(string)
		out.EOFKind, _ = ev[sessionevent.SessionEndFieldEOFKind].(string)
	}
	if out.Count == 0 {
		t.Fatalf("the run wrote NO session_end — every close-out leg must leave one (log: %s)", recordPath)
	}
	return out
}
