package main

// cli_config_env_test.go — the contract tests for the environment sub-verb.
//
// WHY THESE EXIST (Rule 31: a command you did not run is not delivered). The
// verb's value is its OUTPUT and its WRITE-NOTHING default, so the tests drive
// the real surfaces an operator drives. Three facts are pinned:
//
//	1. REPORT writes nothing and its output carries the conflict policy — a
//	   conflict is printed, not resolved, and the file names the sources it
//	   read.
//	2. --write materialises the artifact at the requested path AND the read
//	   path then finds the SAME summary bytes (the writer and the reader agree —
//	   the property that keeps the prompt's summary from drifting away from the
//	   file).
//	3. A bare invocation never writes. A generation run is a measurement, and
//	   a measurement taken by accident is a timestamp nobody can trust.
//
// The fixture is a temp HOME carrying its own ssh config, so the tests measure
// a description the test composed — never this developer machine's inventory.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nick/gopi/internal/envctx"
)

// configEnvFixtureHome creates a temp HOME with the given ssh config and pins
// it for the test, so the verb measures the fixture.
func configEnvFixtureHome(t *testing.T, sshConfig string) string {
	t.Helper()
	home := t.TempDir()
	if err := os.MkdirAll(filepath.Join(home, ".ssh"), 0o755); err != nil {
		t.Fatalf("mkdir fixture ssh dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(home, ".ssh", "config"), []byte(sshConfig), 0o600); err != nil {
		t.Fatalf("write fixture ssh config: %v", err)
	}
	t.Setenv("HOME", home)
	t.Setenv("GOPI_HOME", "") // the fixture HOME is the one that must be measured
	return home
}

// configEnvRun drives the verb's real implementation and returns what it wrote
// to stdout. stderr is left alone: the verb's diagnostics are not the subject.
func configEnvRun(t *testing.T, args ...string) string {
	t.Helper()
	old := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	os.Stdout = w
	done := make(chan string, 1)
	go func() {
		var sb strings.Builder
		buf := make([]byte, 4096)
		for {
			n, rerr := r.Read(buf)
			sb.Write(buf[:n])
			if rerr != nil {
				break
			}
		}
		done <- sb.String()
	}()
	runConfigEnv(args)
	w.Close() //stylegate:ignore closes the write end of a pipe this test created, after the verb has returned
	os.Stdout = old
	return <-done
}

// TestConfigEnvWriteIsFoundByTheReader is the round-trip: what --write puts on
// disk is what the prompt loader reads back, summary for summary.
func TestConfigEnvWriteIsFoundByTheReader(t *testing.T) {
	configEnvFixtureHome(t, "Host peer 10.0.0.2\n        HostName 10.0.0.2\n")
	path := filepath.Join(t.TempDir(), envctx.FileName)
	configEnvRun(t, "--write", "--path", path)

	onDisk, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("--write produced no file at %s: %v", path, err)
	}
	if !strings.Contains(string(onDisk), envctx.FileName) {
		t.Errorf("the artifact does not name itself:\n%s", onDisk)
	}
	if !strings.Contains(string(onDisk), "10.0.0.2") {
		t.Errorf("the artifact does not carry the fixture node:\n%s", onDisk)
	}
	block, ok := envctx.Load(path)
	if !ok {
		t.Fatalf("the reader found nothing in the file the writer wrote (%s)", path)
	}
	if !strings.Contains(string(onDisk), block.Summary) {
		t.Error("the block the reader returns is not the file's own summary text")
	}
}

// TestConfigEnvRecordsTheConflictItMeasured is the acceptance criterion on a
// fixture with the real shape: two aliases on one address must both be printed,
// with the ambiguity named — never reduced to one answer.
func TestConfigEnvRecordsTheConflictItMeasured(t *testing.T) {
	configEnvFixtureHome(t, "Host one 10.1.1.5\n        HostName 10.1.1.5\n\nHost two 10.1.1.5\n        HostName 10.1.1.5\n")
	path := filepath.Join(t.TempDir(), envctx.FileName)
	configEnvRun(t, "--write", "--path", path)

	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read artifact: %v", err)
	}
	text := string(got)
	for _, want := range []string{"one", "two", "10.1.1.5", "claimed by 2 ssh aliases", "RECORDED, NOT RESOLVED"} {
		if !strings.Contains(text, want) {
			t.Errorf("the artifact does not carry %q — the shared address was resolved instead of recorded:\n%s", want, text)
		}
	}
}

// TestConfigEnvBareRunWritesNothing pins the write-nothing default.
func TestConfigEnvBareRunWritesNothing(t *testing.T) {
	home := configEnvFixtureHome(t, "Host peer 10.0.0.2\n        HostName 10.0.0.2\n")
	out := configEnvRun(t) // no --write

	if !strings.Contains(out, "10.0.0.2") {
		t.Errorf("the read arm did not print the description:\n%s", out)
	}
	// The fixture home holds only the .ssh directory the test created: a bare
	// run must not have added anything to it.
	entries, err := os.ReadDir(home)
	if err != nil {
		t.Fatalf("read fixture home: %v", err)
	}
	for _, e := range entries {
		if e.Name() != ".ssh" {
			t.Errorf("a bare `gopi config env` wrote %q into the home", e.Name())
		}
	}
}
