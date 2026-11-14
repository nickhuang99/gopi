package testsuite

// chat_pty_hangup_traced_test.go — a chat ended by a HANGUP leaves a TRACE, end
// to end on a real pty (Rule 31: a unit test is not a substitute).
//
// THE MEASURED REPORT (session ab9e0023, 2026-10-21). The operator's terminal
// session tore down, `gopi chat` printed `[gopi] hangup received — finalizing
// chat session…` and `bye.`, exited 1, and its own session directory held a
// console, a dump and a log — and NOTHING that named the signal. The dump said
// `category: "unknown"` with an empty error_body. So the two artifacts an
// operator reads afterwards both answer "something unrecognised happened",
// while the screen had said the opposite (an orderly close-out). Reproduced
// against this branch's own binary before the fix, with these exact observations:
//
//	[gopi] hangup received — finalizing chat session…   # orderly, on screen
//	exit status: 1
//	signal-provenance.jsonl: ABSENT
//	session.dump.json: {"category":"unknown","error_body":""}
//
// TWO INDEPENDENT HALVES ARE PINNED HERE, because they failed for two different
// reasons and neither is visible from the other:
//
//  1. THE RECORD LANDED. chat CLAIMS its own handlers (signalrec's
//     modesThatOwnTheirSignalHandlers), so the recorder writes the provenance
//     file on its own goroutine while the REPL unwinds — and main() then calls
//     os.Exit, which does not wait. Every other claiming door pays the
//     record-filed handshake on the way out; chat did not.
//  2. THE DUMP NAMES THE CAUSE. The close-out's collector has an arm only for a
//     RUN ERROR; a signal death has none, so the record kept the unknown default.
//
// The dump half also has a unit-level red proof
// (internal/agent/chat_close_signal_category_test.go); this file is the leg that
// proves the two halves meet in a REAL process.

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"

	"github.com/nickhuang99/gopi/internal/testutil"
)

// The harness's own bounds and spellings. Every value is named (Rule 1).
const (
	// hangupTraceSettle is how long the artifacts get to appear after the child
	// has EXITED. The process is gone, so this is a readiness window over the
	// filesystem rather than a wait the assertions race against — the writer ran
	// to completion before exit (that is the fix), and this only covers the
	// last rename/flush the kernel owes.
	hangupTraceSettle = 5 * time.Second
	// hangupTracePollStep is the step inside that window.
	hangupTracePollStep = 50 * time.Millisecond
	// hangupProvenanceFileName is the artifact the recorder writes beside the
	// session's own files. It is SPELLED here because this test asserts on a
	// FILE, and the producer's name is not exported to the testsuite — a rename
	// must fail this test rather than pass it silently.
	hangupProvenanceFileName = "signal-provenance.jsonl"
	// hangupDumpFileName is the session's crash dump.
	hangupDumpFileName = "session.dump.json"
	// hangupSignalField is the provenance record's signal key.
	hangupSignalField = "signal"
	// hangupSignalWant is the signal this test sends, and therefore the one the
	// record must name.
	hangupSignalWant = "SIGHUP"
)

// TestChatPTY_HangupIsTraced_NOTVanished drives a real `gopi chat` on a pty,
// sends the real signal a terminal teardown sends, and reads the session
// directory the run itself publishes. Both halves must hold: the provenance
// record exists and names the signal, and the dump's category is the signal's
// own rather than the unknown default.
func TestChatPTY_HangupIsTraced_NOTVanished(t *testing.T) {
	home := t.TempDir()
	work := t.TempDir()
	llm := startUsageLLM(t)

	env := testutil.MockEnv(t, home, llm.URL)
	chat := startPTYChat(t, []string{"chat"}, env, work)
	t.Cleanup(func() {
		if chat.cmd.Process != nil {
			_ = chat.cmd.Process.Kill() //stylegate:ignore best-effort teardown of a failed run
		}
		_ = chat.master.Close() //stylegate:ignore the master is the test's own descriptor
	})

	// The session is really up and parked at the prompt: the signal must arrive
	// in the steady state a teardown finds it in, not during start-up.
	chat.waitFor(t, chatPromptMark, ptyPromptTimeout)

	if err := chat.cmd.Process.Signal(syscall.SIGHUP); err != nil {
		t.Fatalf("send SIGHUP to the chat process: %v", err)
	}

	// The close-out is on screen — the same sentence the operator saw. It is the
	// run's OWN statement that it handled the signal, so the assertions below
	// are about the artifacts of a graceful unwind, not of a crash.
	chat.waitFor(t, hangupCloseoutFragment, ptyPromptTimeout)
	code := chat.close(t)
	if code == 0 {
		// Not a failure condition by itself in the product, but this run's shape
		// (a handled hangup) is the non-zero close-out the dump is collected for;
		// if that ever changes the dump assertions below would go vacuous.
		t.Logf("note: the hangup close-out exited %d (expected a non-zero code)", code)
	}

	dir := waitForChatSessionDir(t, home)
	assertHangupProvenance(t, dir)
	assertHangupDumpCategory(t, dir)
}

// hangupCloseoutFragment is the run's own on-screen statement that it handled
// the signal (cmd/agent's chatSignalCloseoutNotice renders it). It is spelled
// here as a FRAGMENT so the test reads the operator's experience rather than an
// implementation detail of the sentence's punctuation.
const hangupCloseoutFragment = "hangup received"

// waitForChatSessionDir returns the session directory the run published — the
// directory under <home>/.gopi/sessions that holds the dump. Unlike the unit
// path, nothing tells the test the id, so it is DISCOVERED from the artifact
// that the run writes unconditionally.
func waitForChatSessionDir(t *testing.T, home string) string {
	t.Helper()
	root := filepath.Join(home, ".gopi", "sessions")
	deadline := time.Now().Add(hangupTraceSettle)
	for {
		entries, err := os.ReadDir(root)
		if err == nil {
			for _, e := range entries {
				if !e.IsDir() {
					continue
				}
				dir := filepath.Join(root, e.Name())
				if _, statErr := os.Stat(filepath.Join(dir, hangupDumpFileName)); statErr == nil {
					return dir
				}
			}
		}
		if time.Now().After(deadline) {
			t.Fatalf("no session directory under %s carries %s — the run published no dump at all", root, hangupDumpFileName)
		}
		//stylegate:allow-sleep the wait is this loop's POLL CADENCE over a filesystem artifact (a dump the exited child already wrote), not an assumption about an interval: the loop is bounded by hangupTraceSettle and fails BY NAME at the deadline, so nothing is synchronized by sleeping
		time.Sleep(hangupTracePollStep)
	}
}

// assertHangupProvenance is HALF ONE: the recorder's record is on disk, and it
// names the signal that ended the run.
func assertHangupProvenance(t *testing.T, dir string) {
	t.Helper()
	path := filepath.Join(dir, hangupProvenanceFileName)
	raw, err := os.ReadFile(path)
	if err == nil {
		assertProvenanceNamesSignal(t, path, raw)
		return
	}
	// The write is on the recorder's goroutine and the child has exited, so a
	// last-moment appearance is possible; poll before calling it lost.
	deadline := time.Now().Add(hangupTraceSettle)
	for time.Now().Before(deadline) {
		raw, err = os.ReadFile(path)
		if err == nil {
			assertProvenanceNamesSignal(t, path, raw)
			return
		}
		//stylegate:allow-sleep the wait is this loop's POLL CADENCE over the recorder's file, which an already-exited child's goroutine may still be renaming into place; the loop is bounded by hangupTraceSettle and fails BY NAME at the deadline rather than racing it
		time.Sleep(hangupTracePollStep)
	}
	t.Fatalf("NO %s in %s — a chat session this process HANDLED a hangup for left no trace of it (the 'vanished without a trace' defect)",
		hangupProvenanceFileName, dir)
}

// assertProvenanceNamesSignal reads the record and requires the signal's name.
// An unparseable line is a failure, not a skip: a record nobody can read is the
// same evidence gap the file was supposed to close.
func assertProvenanceNamesSignal(t *testing.T, path string, raw []byte) {
	t.Helper()
	dec := json.NewDecoder(bytes.NewReader(raw))
	found := false
	for {
		var rec map[string]any
		if err := dec.Decode(&rec); err != nil {
			break
		}
		if rec[hangupSignalField] == hangupSignalWant {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("%s carries no record naming %q:\n%s", path, hangupSignalWant, string(raw))
	}
}

// assertHangupDumpCategory is HALF TWO: the dump's category is the signal's own,
// with a body that names the signal — never the unknown default beside an empty
// body, which is what made an orderly teardown read as an unexplained failure.
func assertHangupDumpCategory(t *testing.T, dir string) {
	t.Helper()
	path := filepath.Join(dir, hangupDumpFileName)
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	var dump struct {
		Category  string `json:"category"`
		ErrorBody string `json:"error_body"`
	}
	if err := json.Unmarshal(raw, &dump); err != nil {
		t.Fatalf("parse %s: %v\n%s", path, err, string(raw))
	}
	if dump.Category != signalDumpCategory {
		t.Fatalf("dump category = %q, want %q — a handled signal is a KNOWN cause, not an unrecognised one\n%s",
			dump.Category, signalDumpCategory, string(raw))
	}
	if dump.ErrorBody == "" {
		t.Fatalf("dump error_body is empty — the record does not say what ended the session\n%s", string(raw))
	}
}

// signalDumpCategory is the category deepseek.CategorySignal renders as. It is
// spelled here for the same reason the file names are: this test asserts on the
// ARTIFACT an operator reads.
const signalDumpCategory = "signal"
