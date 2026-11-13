package main

import (
	"database/sql"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nick/gopi/internal/tools"
	_ "modernc.org/sqlite"
)

// The version subcommand must short-circuit BEFORE agent/config startup: the
// old behavior let `gopi version` fall through to the prompt path and start a
// whole session (memory_search checkpoint, API call, session log). These tests
// run the built binary in a sandboxed GOPI_HOME and assert it prints the
// version and exits 0 without touching the network or the session store.
func TestVersionSubcommand(t *testing.T) {
	for _, arg := range []string{"version", "--version", "-v"} {
		t.Run(arg, func(t *testing.T) {
			out, home := runGopiVersionSandboxed(t, arg)
			if out == "" {
				t.Fatalf("empty version output for %q", arg)
			}
			// dev build from this test tree → VCS fallback or "dev"; never a
			// raw unknown-flag error, never an agent banner.
			if strings.Contains(out, "unknown flag") || strings.Contains(out, "[gopi] session:") {
				t.Fatalf("version leaked into the prompt path: %q", out)
			}
			assertNoSessionArtifact(t, arg, home)
		})
	}
}

// TestVersionSubcommandNoLedgerRow is the bug-030 regression guard: a
// pre-fix `gopi version` fell through to the prompt path, so the run opened a
// session JSONL (an empty shell — the process exits before session_end) AND
// registered a task-ledger row (taskregister.go's start hook runs whenever a
// SessionLog exists; the process exits before the end hook → the row is stuck
// in_progress forever, polluting the in_progress view and stale detection).
// Post-fix the version subcommand short-circuits before any agent/session
// exists, so a sandboxed GOPI_HOME must end the run with NO session JSONL and
// NO tasks ledger row (the ledger insert is gated on the session log, but the
// assertion pins both halves of the bug independently).
func TestVersionSubcommandNoLedgerRow(t *testing.T) {
	for _, arg := range []string{"version", "--version", "-v"} {
		t.Run(arg, func(t *testing.T) {
			_, home := runGopiVersionSandboxed(t, arg)
			assertNoSessionArtifact(t, arg, home)
			// No memory.db at all = the strongest pass: the CLI probe never
			// even initialized the knowledge base.
			dbPath := filepath.Join(home, "memory.db")
			if _, err := os.Stat(dbPath); os.IsNotExist(err) {
				return
			}
			db, err := sql.Open("sqlite", tools.LedgerReadOnlyDSN(dbPath)) //db-scaffold-ok: the file was created by the CHILD in its own sandboxed GOPI_HOME (assertNoSessionArtifact asserts the home); the test only READS it, and pins nothing because nothing it calls resolves a database — the scaffold's NewTestDBPath names a path THIS process will create, which is the opposite of what this probe needs
			if err != nil {
				t.Fatalf("open sandbox memory.db: %v", err)
			}
			defer db.Close()
			var rows int
			if err := db.QueryRow(`SELECT COUNT(*) FROM tasks`).Scan(&rows); err != nil {
				t.Fatalf("count ledger rows: %v", err)
			}
			if rows != 0 {
				t.Fatalf("gopi %s registered %d task-ledger row(s) — CLI probe leaked into the ledger", arg, rows)
			}
		})
	}
}

// assertNoSessionArtifact fails the test when the sandboxed GOPI_HOME gained
// a session JSONL (or a sessions dir) — the empty-shell signature of a run
// that opened a session it never closed.
func assertNoSessionArtifact(t *testing.T, arg, home string) {
	t.Helper()
	dir := filepath.Join(home, "sessions")
	entries, err := os.ReadDir(dir)
	if os.IsNotExist(err) {
		return // no session dir at all — the good case
	}
	if err != nil {
		t.Fatalf("read sandbox sessions dir: %v", err)
	}
	if len(entries) > 0 {
		names := make([]string, 0, len(entries))
		for _, e := range entries {
			names = append(names, e.Name())
		}
		t.Fatalf("gopi %s created session artifact(s) %v — CLI probe leaked into the session store", arg, names)
	}
}

func TestVersionHelpFlag(t *testing.T) {
	out, err := runGopiForVersion(t, "version", "-h")
	if err != nil {
		t.Fatalf("gopi version -h failed: %v\noutput:\n%s", err, out)
	}
	if !strings.Contains(out, "Usage: gopi version") {
		t.Fatalf("expected usage text, got: %q", out)
	}
}

func runGopiForVersion(t *testing.T, args ...string) (string, error) {
	out, _, err := runGopiForVersionHome(t, args...)
	return out, err
}

// runGopiVersionSandboxed runs the built binary and also returns the
// sandboxed GOPI_HOME so callers can assert nothing leaked into the session
// store / task ledger (bug-030 regression guards).
func runGopiVersionSandboxed(t *testing.T, args ...string) (string, string) {
	t.Helper()
	out, home, err := runGopiForVersionHome(t, args...)
	if err != nil {
		t.Fatalf("gopi %s failed: %v\noutput:\n%s", strings.Join(args, " "), err, out)
	}
	return out, home
}

func runGopiForVersionHome(t *testing.T, args ...string) (string, string, error) {
	t.Helper()
	bin := gopiTestBinPath(t)
	// Sandboxed home: even a regression into the prompt path cannot pollute
	// the real profile (memory.db / sessions) — GOPI_HOME redirects everything.
	home := t.TempDir()
	cmd := exec.Command(bin, args...)
	cmd.Env = append(os.Environ(), "GOPI_HOME="+home)
	out, err := cmd.CombinedOutput()
	return string(out), home, err
}
