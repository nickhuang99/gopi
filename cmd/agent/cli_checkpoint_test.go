package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// writeCheckpointRawLog writes a minimal raw log with one → REQUEST (the
// resume checkpoint) into dir and returns its path.
func writeCheckpointRawLog(t *testing.T, dir string) string {
	t.Helper()
	path := filepath.Join(dir, "gopi-raw-20260826-120000.log")
	body := "→ REQUEST\n" +
		`{"model":"deepseek-v4-pro","max_tokens":16384,"messages":[{"role":"user","content":[{"type":"text","text":"[EPIC GOAL] interrupted"}]}],"stream":true}` + "\n"
	if err := os.WriteFile(path, []byte(body), 0644); err != nil {
		t.Fatalf("write raw log: %v", err)
	}
	return path
}

func TestResolveRawLog_FullPath(t *testing.T) {
	dir := t.TempDir()
	path := writeCheckpointRawLog(t, dir)
	if got := resolveRawLog(path, dir); got != path {
		t.Errorf("resolveRawLog(full path) = %q, want %q", got, path)
	}
}

func TestResolveRawLog_BareName(t *testing.T) {
	dir := t.TempDir()
	path := writeCheckpointRawLog(t, dir)
	if got := resolveRawLog("gopi-raw-20260826-120000.log", dir); got != path {
		t.Errorf("resolveRawLog(bare name) = %q, want %q", got, path)
	}
}

func TestResolveRawLog_Prefix(t *testing.T) {
	dir := t.TempDir()
	path := writeCheckpointRawLog(t, dir)
	if got := resolveRawLog("20260826-120000", dir); got != path {
		t.Errorf("resolveRawLog(prefix) = %q, want %q", got, path)
	}
}

func TestResolveRawLog_MissingExits(t *testing.T) {
	dir := t.TempDir()
	if os.Getenv("GOPI_TEST_EXIT") == "1" {
		resolveRawLog("no-such-log", dir)
		return
	}
	// resolveRawLog exits the process on missing logs — verify via a
	// subprocess so the test binary survives.
	cmd := testHelperCommand(t, "TestResolveRawLog_MissingExits")
	out, err := cmd.CombinedOutput()
	if err == nil {
		t.Fatalf("expected non-zero exit for missing log, got output: %s", out)
	}
	if !strings.Contains(string(out), "no raw log matching") {
		t.Errorf("unexpected stderr: %s", out)
	}
}

func TestTruncate(t *testing.T) {
	if got := truncate("short", 10); got != "short" {
		t.Errorf("truncate(short) = %q", got)
	}
	if got := truncate("a very long string indeed", 5); got != "a ver…" {
		t.Errorf("truncate(long) = %q", got)
	}
}

// testHelperCommand builds the current test binary as a subprocess helper
// (standard Go pattern for testing os.Exit paths).
func testHelperCommand(t *testing.T, name string) *exec.Cmd {
	t.Helper()
	exe, err := os.Executable()
	if err != nil {
		t.Fatalf("os.Executable: %v", err)
	}
	cmd := exec.Command(exe, "-test.run="+name)
	cmd.Env = append(os.Environ(), "GOPI_TEST_EXIT=1")
	return cmd
}
