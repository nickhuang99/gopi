package agent

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestRunEngine_RejectsEmptyInput is the regression test for the
// "empty test-model sessions" Epic: an empty prompt with no images must
// never start a session. Root cause: the SessionLogger is created eagerly in
// NewAgent, so by the time Run() sees the empty input a session file already
// exists on disk — the guard must live at the loop entry.
//
// Isolation: newTestAgentEnv (not bare tempHome) — NewAgent resolves an API
// key at construction, so a keyless shell used to fail here with
// "ANTHROPIC_AUTH_TOKEN not set" before the guard was ever reached. The
// injected key is never used: Run() rejects empty input before any request,
// so the test stays green (and real-key-free) in a fresh environment.
func TestRunEngine_RejectsEmptyInput(t *testing.T) {
	newTestAgentEnv(t)

	a, err := NewAgent("test-model")
	if err != nil {
		t.Fatalf("NewAgent: %v", err)
	}
	defer a.SessionLog.Close()

	for name, input := range map[string]string{
		"empty string":  "",
		"whitespace":    "   \n\t  ",
		"whitespace+nl": "\n",
	} {
		_, runErr := a.Run(input)
		if runErr == nil {
			t.Errorf("%s: Run succeeded, want empty-input error", name)
		}
		if !strings.Contains(runErr.Error(), "empty task input") {
			t.Errorf("%s: error %q does not mention empty task input", name, runErr.Error())
		}
	}

	// No task may have started: NewAgent eagerly creates the session file
	// (accepted design — see devdoc), but the loop must never have logged a
	// user message (the [EPIC GOAL] framing) for an empty input. An empty
	// session file with only session_start is the fingerprint of a run that
	// never happened; the guard keeps it from ever containing a task.
	lines := readJSONLLines(t, a.SessionLog.Path())
	for _, l := range lines {
		if l["type"] == "user" {
			t.Errorf("empty-input run logged a user message: %v", l)
		}
	}
}

// TestRunEngine_AcceptsImageOnlyInput — the empty-input guard must not block
// native image input: an empty prompt WITH an image is a legitimate vision
// task ("describe this image" is implied by the epic framing).
func TestRunEngine_AcceptsImageOnlyInput(t *testing.T) {
	if os.Getenv("GOPI_E2E") == "" {
		t.Skip("requires a live API endpoint (GOPI_E2E=1)")
	}
	tempHome(t)

	a, err := NewAgent("test-model")
	if err != nil {
		t.Fatalf("NewAgent: %v", err)
	}
	defer a.SessionLog.Close()

	// A nil image still passes the guard (len(images) > 0) — we cannot make
	// a real API call in unit tests; this only proves the guard does not
	// reject image-only input. The actual loop would fail on the API call.
	if _, runErr := a.Run("", nil); runErr != nil && strings.Contains(runErr.Error(), "empty task input") {
		t.Fatalf("guard rejected image-only input: %v", runErr)
	}
}

// TestSessionLogger_LandsInGopiHome verifies the GOPI_HOME redirect end to
// end: with GOPI_HOME set, NewSessionLogger must create its session file
// under $GOPI_HOME/.gopi/sessions and NEVER touch the real user profile.
func TestSessionLogger_LandsInGopiHome(t *testing.T) {
	root := t.TempDir()
	t.Setenv("GOPI_HOME", root)
	// Deliberately do NOT set HOME to a usable dir: GOPI_HOME must suffice.
	t.Setenv("HOME", "")

	sl, err := NewSessionLogger("test-model", "")
	if err != nil {
		t.Fatalf("NewSessionLogger: %v", err)
	}
	defer sl.Close()

	wantDir := filepath.Join(root, ".gopi", "sessions")
	if filepath.Dir(sl.Path()) != filepath.Join(wantDir, sl.SessionID()) {
		t.Errorf("session file dir = %q, want %q (bare session dir)", filepath.Dir(sl.Path()), filepath.Join(wantDir, sl.SessionID()))
	}
	if _, err := os.Stat(sl.Path()); err != nil {
		t.Errorf("session file missing: %v", err)
	}
	// Bare sessions write session.jsonl inside their per-session directory.
	if filepath.Base(sl.Path()) != "session.jsonl" {
		t.Errorf("session file basename = %q, want session.jsonl", filepath.Base(sl.Path()))
	}
}

// TestTaskregisterSessionStaysOutOfTheRealProfile is a belt-and-braces check:
// the taskregister helper must point its SessionLogger at a SANDBOX (GOPI_HOME
// is set by the helper), never at the real user profile — the 8/26 regression
// where one throwaway session leaked into ~/.gopi/sessions per test run.
func TestTaskregisterSessionStaysOutOfTheRealProfile(t *testing.T) {
	a := newAutoTaskAgent(t)
	a.maybeRegisterSessionTask("probe: taskregister isolation")

	if !strings.HasPrefix(a.SessionLog.Path(), os.TempDir()) {
		t.Errorf("session log %q is not in a temp sandbox — the taskregister helper leaked into the real profile", a.SessionLog.Path())
	}
	if strings.HasPrefix(a.SessionLog.Path(), "/home/nick/.gopi") {
		t.Errorf("session log %q escaped into the real user profile", a.SessionLog.Path())
	}
}
