package main

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nick/gopi/internal/config"
	"github.com/nick/gopi/internal/sessionlayout"
)

// captureStderrStream captures stderr while fn runs. The raw view's contract
// is "stdout stays pure bytes", so the shared-raw-log diagnostic is asserted
// on stderr specifically.
func captureStderrStream(t *testing.T, fn func()) string {
	t.Helper()
	old := os.Stderr
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	os.Stderr = w
	done := make(chan string, 1)
	go func() {
		b, _ := io.ReadAll(r) //stylegate:ignore bounded read of an error/diagnostic body; a short read is acceptable
		done <- string(b)
	}()
	fn()
	if err := w.Close(); err != nil {
		t.Fatalf("close writer: %v", err)
	}
	os.Stderr = old
	return <-done
}

// rawSharedSandbox builds a $GOPI_HOME with a raw log recorded by every id in
// owners (one session JSONL each) and returns the first session's path plus
// the raw log path.
func rawSharedSandbox(t *testing.T, rawLogName string, owners ...string) (string, string) {
	t.Helper()
	home := t.TempDir()
	rawDir := filepath.Join(home, ".gopi", "raw")
	sessDir := filepath.Join(home, ".gopi", "sessions")
	for _, d := range []string{rawDir, sessDir} {
		if err := os.MkdirAll(d, 0o700); err != nil {
			t.Fatalf("mkdir %s: %v", d, err)
		}
	}
	rawLog := filepath.Join(rawDir, rawLogName)
	if err := os.WriteFile(rawLog, []byte("→ REQUEST\n{}\n"), 0o600); err != nil {
		t.Fatalf("write raw log: %v", err)
	}
	for _, id := range owners {
		start := "{\"type\":\"session_start\",\"sessionId\":\"" + id +
			"\",\"model\":\"m\",\"rawLogPath\":\"" + rawLog + "\",\"timestamp\":\"2026-08-03T19:06:11Z\"}\n"
		if err := os.WriteFile(filepath.Join(sessDir, id+".jsonl"), []byte(start), 0o600); err != nil {
			t.Fatalf("write session %s: %v", id, err)
		}
	}
	t.Setenv(config.GopiDirEnv, home)
	return filepath.Join(sessDir, owners[0]+".jsonl"), rawLog
}

// TestWarnSharedRawLogNamesTheOtherSessions: a raw log two sessions recorded
// is the interleaving case — the diagnostic must fire on stderr, name the
// OTHER session (the reader's own id is noise), and stay off stdout.
func TestWarnSharedRawLogNamesTheOtherSessions(t *testing.T) {
	self := "aaaa1111-2222-3333-4444-555566667777"
	other := "bbbb8888-2222-3333-4444-555566667777"
	sessionPath, rawLog := rawSharedSandbox(t, "gopi-raw-20260803-190611.log", self, other)

	got := captureStderrStream(t, func() { warnSharedRawLog(sessionPath, rawLog) })
	if !strings.Contains(got, "gopi-raw-20260803-190611.log") {
		t.Errorf("diagnostic must name the raw log, got:\n%s", got)
	}
	if !strings.Contains(got, "bbbb8888") {
		t.Errorf("diagnostic must name the other session, got:\n%s", got)
	}
	if !strings.Contains(got, "NOT session-scoped") {
		t.Errorf("diagnostic must state the round-numbering consequence, got:\n%s", got)
	}
	if n := strings.Count(got, "\n"); n != 1 {
		t.Errorf("diagnostic must be one line, got %d:\n%s", n, got)
	}

	out := captureStdoutStream(t, func() { warnSharedRawLog(sessionPath, rawLog) })
	if out != "" {
		t.Errorf("diagnostic leaked to stdout (%d bytes) — stdout must stay byte-pure", len(out))
	}
}

// TestWarnSharedRawLogSilentForSingleOwner: the normal one-session-per-log
// case must produce no diagnostic at all — stderr noise on every raw view
// would be its own trust problem.
func TestWarnSharedRawLogSilentForSingleOwner(t *testing.T) {
	self := "aaaa1111-2222-3333-4444-555566667777"
	sessionPath, rawLog := rawSharedSandbox(t, "gopi-raw-20260803-190611.log", self)

	got := captureStderrStream(t, func() { warnSharedRawLog(sessionPath, rawLog) })
	if got != "" {
		t.Errorf("single-owner raw log must be silent, got:\n%s", got)
	}
}

// TestWarnSharedRawLogToleratesUnreadableSessions: an unreadable sibling
// session file must not turn the diagnostic into an error path.
func TestWarnSharedRawLogToleratesUnreadableSessions(t *testing.T) {
	self := "aaaa1111-2222-3333-4444-555566667777"
	sessionPath, rawLog := rawSharedSandbox(t, "gopi-raw-20260803-190611.log", self)
	broken := filepath.Join(sessionlayout.RecordContainer(sessionPath), "cccc4444-2222-3333-4444-555566667777.jsonl")
	if err := os.WriteFile(broken, []byte("not json\n"), 0o600); err != nil {
		t.Fatalf("write broken session: %v", err)
	}

	got := captureStderrStream(t, func() { warnSharedRawLog(sessionPath, rawLog) })
	if got != "" {
		t.Errorf("unparseable sibling must not trigger the diagnostic, got:\n%s", got)
	}
}

// TestSharedRawLogWarningNamesFullSessionIDs — the diagnostic exists so the
// operator can go inspect the OTHER sessions, and `gopi session log` refuses a
// truncated id (epic full-id-no-prefix). A leading-block abbreviation printed
// an operand that could not be pasted back, so the warning must carry the full
// id of every co-recording session.
func TestSharedRawLogWarningNamesFullSessionIDs(t *testing.T) {
	const other = "c6985118-f087-4034-ad6f-cedb50524cbe"
	self := "aaaa1111-2222-3333-4444-555566667777"
	sessionPath, rawLog := rawSharedSandbox(t, "gopi-raw-20260803-190611.log", self, other)

	got := captureStderrStream(t, func() { warnSharedRawLog(sessionPath, rawLog) })
	if !strings.Contains(got, other) {
		t.Errorf("the shared-log warning must name the full co-recording session id %q, got:\n%s", other, got)
	}
}
