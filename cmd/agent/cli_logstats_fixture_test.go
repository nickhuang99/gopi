package main

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nick/gopi/internal/config"
)

// Shared fixture plumbing for the `gopi session stats` / `gopi session raw` tests. The input
// is a REAL session log (IMPLEMENT-SPEC §4 — no constructed mocks) copied into
// a GOPI_HOME sandbox so the CLI's own resolution path runs hermetically.

const (
	// statsFixtureRootRel is the fixture directory. go test runs with the
	// package directory as CWD, so this is stable for cmd/agent.
	statsFixtureRootRel = "../../tests/fixtures/log-stats-raw"
	// statsFixtureSessionID is the real session the fixture was copied from.
	statsFixtureSessionID = "fc426fed-e3df-4e30-b73a-3dfdb2a8ccd4"
	// statsFixtureRawLogName is the raw log paired with statsFixtureSessionID.
	statsFixtureRawLogName = "gopi-raw-20260904-204046.log"
)

// statsFixtureSandbox copies the fixture (session JSONL + paired raw log) into
// a fresh GOPI_HOME and points the process at it.
func statsFixtureSandbox(t *testing.T) string {
	t.Helper()
	// GOPI_HOME is the PARENT of the .gopi data dir: $GOPI_HOME/.gopi/sessions.
	home := t.TempDir()
	gopiDir := filepath.Join(home, ".gopi")
	sessions := filepath.Join(gopiDir, "sessions")
	raw := filepath.Join(gopiDir, "raw")
	for _, d := range []string{sessions, raw} {
		if err := os.MkdirAll(d, 0o700); err != nil {
			t.Fatalf("mkdir %s: %v", d, err)
		}
	}
	copyFixture(t, filepath.Join("sessions", statsFixtureSessionID+".jsonl"),
		filepath.Join(sessions, statsFixtureSessionID+".jsonl"))
	copyFixture(t, filepath.Join("raw", statsFixtureRawLogName),
		filepath.Join(raw, statsFixtureRawLogName))
	t.Setenv(config.GopiDirEnv, home)
	return statsFixtureSessionID
}

// statsFixtureSessionsOnly copies ONLY the session JSONL — the no-raw-log case.
func statsFixtureSessionsOnly(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	sessions := filepath.Join(home, ".gopi", "sessions")
	if err := os.MkdirAll(sessions, 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	copyFixture(t, filepath.Join("sessions", statsFixtureSessionID+".jsonl"),
		filepath.Join(sessions, statsFixtureSessionID+".jsonl"))
	t.Setenv(config.GopiDirEnv, home)
	return statsFixtureSessionID
}

// statsFixtureBareLayout copies the fixture session JSONL into the bare
// session-dir layout — sessions/<uuid>/session.jsonl — the migrated layout
// for ~872 non-task sessions. The fixed file name means the DISPLAY id must
// come from the session_start event, never the file basename.
func statsFixtureBareLayout(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	bareDir := filepath.Join(home, ".gopi", "sessions", statsFixtureSessionID)
	if err := os.MkdirAll(bareDir, 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	copyFixture(t, filepath.Join("sessions", statsFixtureSessionID+".jsonl"),
		filepath.Join(bareDir, "session.jsonl"))
	t.Setenv(config.GopiDirEnv, home)
	return statsFixtureSessionID
}

// statsFixtureRawPath is the fixture raw log's in-repo path.
func statsFixtureRawPath(t *testing.T) string {
	t.Helper()
	return filepath.Join(statsFixtureRootRel, "raw", statsFixtureRawLogName)
}

// statsFixtureRawLine returns raw-log line n (1-based) verbatim from the
// fixture raw log.
func statsFixtureRawLine(t *testing.T, n int) string {
	t.Helper()
	b, err := os.ReadFile(statsFixtureRawPath(t))
	if err != nil {
		t.Fatalf("read fixture raw log: %v", err)
	}
	lines := strings.Split(strings.TrimRight(string(b), "\n"), "\n")
	if n < 1 || n > len(lines) {
		t.Fatalf("fixture raw log has %d lines, asked for %d", len(lines), n)
	}
	return lines[n-1]
}

func copyFixture(t *testing.T, srcRel, dst string) {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(statsFixtureRootRel, srcRel))
	if err != nil {
		t.Fatalf("read fixture %s: %v", srcRel, err)
	}
	if err := os.WriteFile(dst, b, 0o600); err != nil {
		t.Fatalf("write sandbox %s: %v", dst, err)
	}
}

// captureStdoutStream is captureStdout for LARGE outputs: it drains the pipe
// while fn runs, so a view that writes more than the pipe buffer (64 KiB) —
// e.g. a whole raw log — cannot block on a full pipe and hang the test.
func captureStdoutStream(t *testing.T, fn func()) string {
	t.Helper()
	old := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	os.Stdout = w
	done := make(chan string, 1)
	go func() {
		b, _ := io.ReadAll(r) //stylegate:ignore bounded read of an error/diagnostic body; a short read is acceptable
		done <- string(b)
	}()
	fn()
	if err := w.Close(); err != nil {
		t.Fatalf("close writer: %v", err)
	}
	os.Stdout = old
	return <-done
}

// assertGolden compares got with the committed golden snapshot, failing with
// the first differing line. A missing golden is a failure here (the gopilog
// package owns recording; the CLI layer must match what is already committed).
func assertGolden(t *testing.T, name, got string) {
	t.Helper()
	path := filepath.Join(statsFixtureRootRel, "golden", name)
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read golden %s: %v", name, err)
	}
	if got != string(want) {
		wantLines := strings.Split(string(want), "\n")
		gotLines := strings.Split(got, "\n")
		hint := ""
		for i := 0; i < len(wantLines) && i < len(gotLines); i++ {
			if wantLines[i] != gotLines[i] {
				hint = fmt.Sprintf("first difference at line %d:\n  want: %q\n  got:  %q", i+1, wantLines[i], gotLines[i])
				break
			}
		}
		if hint == "" {
			hint = fmt.Sprintf("line count: want %d, got %d", len(wantLines), len(gotLines))
		}
		t.Errorf("golden %s drifted\n%s", name, hint)
	}
}
