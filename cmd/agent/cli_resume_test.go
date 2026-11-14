package main

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/nick/gopi/internal/config"
)

// TestResolveResumeLogPath_PathPassthrough verifies an existing path is used
// verbatim.
func TestResolveResumeLogPath_PathPassthrough(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "gopi-raw-test.log")
	if err := os.WriteFile(p, []byte("→ REQUEST\n{}\n"), 0644); err != nil {
		t.Fatal(err)
	}
	got, err := resolveResumeLogPath(p)
	if err != nil {
		t.Fatalf("resolveResumeLogPath: %v", err)
	}
	if got != p {
		t.Errorf("got %q, want %q", got, p)
	}
}

// TestResolveResumeLogPath_UnknownTarget verifies a non-path, non-session
// argument produces a helpful error.
func TestResolveResumeLogPath_UnknownTarget(t *testing.T) {
	if _, err := resolveResumeLogPath("not-a-real-thing"); err == nil {
		t.Fatal("want error, got nil")
	}
}

// TestResolveResumeLogPath_SessionIDResolvesRawLog wires a fake HOME with a
// session JSONL whose session_start timestamp matches a raw log filename, and
// verifies the resolver maps the session id to that raw log (the same pairing
// FindRawLogForSession uses for `gopi session log --request`).
func TestResolveResumeLogPath_SessionIDResolvesRawLog(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv(config.GopiDirEnv, home) // this fixture's profile WINS over the suite pin

	sessDir := filepath.Join(home, ".gopi", "sessions")
	rawDir := filepath.Join(home, ".gopi", "raw")
	if err := os.MkdirAll(sessDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(rawDir, 0755); err != nil {
		t.Fatal(err)
	}

	// session_start timestamp 2026-08-26 12:00:00.5 local; raw log named for
	// the same second (local wall clock). Sub-second offset avoids the exact
	// second-0 delta ambiguity in FindRawLogForSession (real session_start
	// timestamps carry nanoseconds, so this mirrors production).
	sessionID := "4c9a1f3e-8b2d-4a10-9e6f-2c5d7a0b1e33"
	sessionPath := filepath.Join(sessDir, sessionID+".jsonl")
	start := time.Date(2026, 8, 26, 12, 0, 0, 500_000_000, time.Local)
	sessContent := "{\"type\":\"session_start\",\"sessionId\":\"" + sessionID +
		"\",\"timestamp\":\"" + start.Format(time.RFC3339Nano) + "\"}\n" +
		"{\"type\":\"session_end\",\"exit_code\":1}\n"
	if err := os.WriteFile(sessionPath, []byte(sessContent), 0644); err != nil {
		t.Fatal(err)
	}

	rawName := "gopi-raw-20260826-120000.log"
	rawPath := filepath.Join(rawDir, rawName)
	if err := os.WriteFile(rawPath, []byte("→ REQUEST\n{}\n"), 0644); err != nil {
		t.Fatal(err)
	}

	got, err := resolveResumeLogPath(sessionID)
	if err != nil {
		t.Fatalf("resolveResumeLogPath: %v", err)
	}
	if got != rawPath {
		t.Errorf("got %q, want %q", got, rawPath)
	}
}

// TestResolveResumeLogPath_UsesRecordedRawLog verifies that when a session's
// session_start records a rawLogPath (custom GOPI_RAW_LOG_DIR), session-id
// resolution uses it verbatim — even when the recorded path is NOT inside
// /home/user/.gopi/raw (the timestamp-based pairing would fail there).
func TestResolveResumeLogPath_UsesRecordedRawLog(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv(config.GopiDirEnv, home) // this fixture's profile WINS over the suite pin

	sessDir := filepath.Join(home, ".gopi", "sessions")
	if err := os.MkdirAll(sessDir, 0755); err != nil {
		t.Fatal(err)
	}

	// Custom raw dir OUTSIDE /home/user/.gopi — recorded in session_start.
	customRawDir := filepath.Join(t.TempDir(), "custom-raw")
	if err := os.MkdirAll(customRawDir, 0755); err != nil {
		t.Fatal(err)
	}
	customRaw := filepath.Join(customRawDir, "gopi-raw-20260826-090000.log")
	if err := os.WriteFile(customRaw, []byte("→ REQUEST\n{}\n"), 0644); err != nil {
		t.Fatal(err)
	}

	sessionID := "abcd1234-1111-2222-3333-444455556666"
	sessionPath := filepath.Join(sessDir, sessionID+".jsonl")
	sessContent := "{\"type\":\"session_start\",\"sessionId\":\"" + sessionID +
		"\",\"rawLogPath\":\"" + customRaw + "\",\"timestamp\":\"2026-08-26T09:00:00.123456789+08:00\"}\n"
	if err := os.WriteFile(sessionPath, []byte(sessContent), 0644); err != nil {
		t.Fatal(err)
	}

	got, err := resolveResumeLogPath(sessionID)
	if err != nil {
		t.Fatalf("resolveResumeLogPath: %v", err)
	}
	if got != customRaw {
		t.Errorf("got %q, want recorded raw log %q", got, customRaw)
	}
}
