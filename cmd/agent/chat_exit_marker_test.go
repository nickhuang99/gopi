package main

// chat_exit_marker_test.go — the death certificate's two properties, driven
// through the REAL REPL path (chat_end_reason_test.go's harness: the same agent
// literal a live run builds, the same runChatWithInput entry point).
//
// WHY THESE TWO AND NOT MORE. The instrument's whole value is one discrimination
// (session 8616b46c): a run that reached its closeout must say so, and a run that
// did not must be left saying `live`. Both halves are what a reader relies on, so
// both are measured here — a test of only the happy path would certify exactly the
// artefact that would have lied about the 2026-10-20 death.
//
// The third property — that the marker lands where the sweep looks for it — is
// pinned in internal/agent (TestOrphanDumpCarriesTheExitMarkerVerdict), because
// the reader lives there and package main cannot be imported by it.

import (
	"encoding/json"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nickhuang99/gopi/internal/agent"
	"github.com/nickhuang99/gopi/internal/deepseek"
	"github.com/nickhuang99/gopi/internal/sessionevent"
	"github.com/nickhuang99/gopi/internal/sessionlayout"
)

// markerOnDisk is one marker as this test reads it back — the WIRE shape, decoded
// from the file, never from the writer's own struct (a test that compared the
// struct to itself would pass with the file never written).
type markerOnDisk struct {
	Version    int    `json:"version"`
	Phase      string `json:"phase"`
	SessionID  string `json:"session_id"`
	PID        int    `json:"pid"`
	StartTicks uint64 `json:"start_ticks"`
	Started    string `json:"started"`
	Updated    string `json:"updated"`
	ExitCode   int    `json:"exit_code"`
	Reason     string `json:"reason"`
	Signal     string `json:"signal"`
	EOFKind    string `json:"eof_kind"`
}

// readMarkerAt reads the marker in dir and fails when it is not there: an absent
// marker is a FINDING (a death before arming), never something a test tolerates.
func readMarkerAt(t *testing.T, dir string) markerOnDisk {
	t.Helper()
	path := filepath.Join(dir, sessionlayout.ExitMarkerName)
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("no exit marker at %s: %v — a run that took this path must leave one", path, err)
	}
	var m markerOnDisk
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatalf("exit marker %s is not valid JSON: %v (bytes: %q)", path, err, raw)
	}
	return m
}

// driveChatMarked mirrors driveChatEndingWithAgent but RETURNS the session
// directory, which is what the marker is filed in.
func driveChatMarked(t *testing.T, input string) string {
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
	dir := sessionlayout.RecordContainer(sl.Path())

	ag := &agent.Agent{
		Client:     &deepseek.Client{BaseURL: srv.URL, APIKey: "test-key", HTTPClient: srv.Client()},
		Ctx:        &agent.Context{},
		Model:      endReasonTestModel,
		Timeout:    0,
		SessionLog: sl,
		Logger:     log.New(io.Discard, "", 0),
	}
	ag.PersistChatSession = true
	chatAgentFactory = func(string) (*agent.Agent, error) { return ag, nil }

	in, closeIn := newChatInputCooked(strings.NewReader(input), io.Discard)
	_ = runChatWithInput([]string{"--no-editor"}, in)
	closeIn()
	return dir
}

// TestChatExitMarkerRecordsTheCloseout is the GREEN half of the discrimination:
// a run that reaches finishChat leaves `phase=closing` with the closeout's own
// verdict — so a reader who finds session_end PRESENT and this marker says the
// ending, and never has to reconstruct it.
//
// RED WITHOUT THE FIX: chat_exit_marker.go's arm call did not exist, so nothing
// wrote this file at all and the test failed at readMarkerAt with "no exit
// marker at …" (observed — see the run's evidence file).
func TestChatExitMarkerRecordsTheCloseout(t *testing.T) {
	dir := driveChatMarked(t, "/quit\n")
	m := readMarkerAt(t, dir)

	if m.Phase != chatExitPhaseClosing {
		t.Fatalf("phase = %q, want %q — the closeout ran, and the marker must say so (marker: %+v)",
			m.Phase, chatExitPhaseClosing, m)
	}
	if m.SessionID == "" {
		t.Errorf("marker names no session (session_id = %q) — a marker that cannot say WHICH session it witnesses is not evidence", m.SessionID)
	}
	if m.PID != os.Getpid() {
		t.Errorf("marker pid = %d, want %d (this run's own process)", m.PID, os.Getpid())
	}
	if m.Started == "" || m.Updated == "" {
		t.Errorf("marker instants = %q / %q — both are needed to order the arm, the last write and the death", m.Started, m.Updated)
	}
	// /quit states its own reason; the marker carries the caller's word verbatim
	// (the precedence that lets a recorded signal win lives in agentEndCause, and
	// this file must not re-derive it).
	if m.Reason != sessionevent.SessionEndReasonQuit {
		t.Errorf("marker reason = %q, want %q — the closeout's stated cause", m.Reason, sessionevent.SessionEndReasonQuit)
	}
	if m.ExitCode != 0 {
		t.Errorf("marker exit_code = %d, want 0 for /quit", m.ExitCode)
	}
}

// TestChatExitMarkerStaysLiveWhenNoCloseoutRuns is the OTHER half, and the one
// that makes `phase=live` mean something: a run that never calls finishChat must
// leave the marker at its armed phase — or, if its own function returned, at the
// returning phase. Neither may claim a closeout that did not happen.
//
// The negative arm is driven by the production predicate rather than by
// hand-writing a file: an unadvanced handle IS what a killed process leaves
// behind, and asserting on the handle's own file keeps this test honest about
// the bytes (it reads the JSON off disk, not the struct).
func TestChatExitMarkerStaysLiveWhenNoCloseoutRuns(t *testing.T) {
	dir := t.TempDir()
	m, err := armChatExitMarker(dir, "session-under-test")
	if err != nil {
		t.Fatalf("armChatExitMarker: %v", err)
	}
	if m == nil {
		t.Fatal("armChatExitMarker returned nil for a real directory — the run would pay for an instrument it does not write")
	}
	armed := readMarkerAt(t, dir)
	if armed.Phase != chatExitPhaseLive {
		t.Fatalf("phase right after arming = %q, want %q", armed.Phase, chatExitPhaseLive)
	}
	if armed.SessionID != "session-under-test" {
		t.Errorf("marker session_id = %q, want the identity the caller handed it", armed.SessionID)
	}

	// A DEATH HERE is simulated by never calling closing(): the file must still
	// read `live`, which is the verdict the postmortem reads off a killed run.
	if after := readMarkerAt(t, dir); after.Phase != chatExitPhaseLive {
		t.Fatalf("phase without a closeout = %q, want %q — this is the ONE value that separates the measured death from an ordinary ending",
			after.Phase, chatExitPhaseLive)
	}

	// The RETURNING path must NOT be able to pass for a closeout either: it has
	// its own phase, so `closing` stays reserved for a real finishChat.
	if err := m.returnedWithoutCloseout(); err != nil {
		t.Fatalf("returnedWithoutCloseout: %v", err)
	}
	if got := readMarkerAt(t, dir).Phase; got != chatExitPhaseReturned {
		t.Fatalf("phase after a returning path = %q, want %q", got, chatExitPhaseReturned)
	}
	// …and a later closeout claim cannot overwrite it (one ending per run).
	if err := m.closing(0, agent.EndCause{Reason: sessionevent.SessionEndReasonQuit}); err != nil {
		t.Fatalf("closing after the returning path: %v", err)
	}
	if got := readMarkerAt(t, dir).Phase; got != chatExitPhaseReturned {
		t.Errorf("phase = %q, want %q — the FIRST verdict wins, so a run cannot report two endings", got, chatExitPhaseReturned)
	}
}

// TestChatExitMarkerNilSessionIsSilent pins the no-session case (`--replay` mints
// no record): every method must be a no-op rather than a panic, and the arm call
// must return nil so the caller's deferred release is skipped.
func TestChatExitMarkerNilSessionIsSilent(t *testing.T) {
	m, err := armChatExitMarker("", "")
	if err != nil || m != nil {
		t.Fatalf("armChatExitMarker with no session dir = (%v, %v), want (nil, nil)", m, err)
	}
	var nilMark *chatExitMark
	if err := nilMark.closing(0, agent.EndCause{}); err != nil {
		t.Errorf("nil closing: %v", err)
	}
	if err := nilMark.returnedWithoutCloseout(); err != nil {
		t.Errorf("nil returnedWithoutCloseout: %v", err)
	}
}
