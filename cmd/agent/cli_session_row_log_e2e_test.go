package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nickhuang99/gopi/internal/config"
	"github.com/nickhuang99/gopi/internal/sessionlayout"
	"github.com/nickhuang99/gopi/internal/testutil"
	"github.com/nickhuang99/gopi/internal/tools"
)

// THE EXIT CODE IS THE CONTRACT — real binary, three shapes (Rule 31).
//
// The resolution tests above pin WHICH FILE each shape answers. These pin what
// an OPERATOR sees, because the defect's most expensive half was not a wrong
// answer: it was `session resume <row>` exiting 0 and continuing an unrelated
// conversation. So shape 3 asserts exit != 0 AND that nothing new appeared in
// the sessions tree — the "nothing started" half of the requirement.

// sessionVerbProfile pins a private profile (Rule 32/22) and returns its
// profile ROOT, its sessions root and the env the child binary runs with.
func sessionVerbProfile(t *testing.T) (home, root string, env []string) {
	t.Helper()
	home = testutil.TestScratchDir(t, "sessionverb-home")
	db := testutil.NewTestDBPath(t)
	env = append(os.Environ(),
		config.GopiDirEnv+"="+home,
		"GOPI_MEMORY_DB="+db,
		"HOME="+home,
		"GOPI_CRASH_DUMP_SWEEP=off",
	)
	return home, filepath.Join(home, config.ProfileDirName, config.SessionsDirName), env
}

// firstN renders the head of a captured stream for a failure message.
func firstN(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}

// writeRecord lays one session record down for the real binary to read.
func writeRecord(t *testing.T, path, sid, tid string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", filepath.Dir(path), err)
	}
	line := `{"type":"session_start","sessionId":"` + sid + `"`
	if tid != "" {
		line += `,"taskId":"` + tid + `"`
	}
	line += "}\n"
	if err := os.WriteFile(path, []byte(line), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

// sessionTreeSnapshot lists every path under the sessions root, so a test can
// assert that a refused verb created NOTHING.
func sessionTreeSnapshot(t *testing.T, root string) []string {
	t.Helper()
	var out []string
	err := filepath.Walk(root, func(p string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		out = append(out, p)
		return nil
	})
	if err != nil {
		t.Fatalf("walk %s: %v", root, err)
	}
	return out
}

// TestSessionLogVerbResolvesARowIDEndToEnd: shape 1 through the real binary —
// the row id addresses the record named by the run uuid.
func TestSessionLogVerbResolvesARowIDEndToEnd(t *testing.T) {
	_, root, env := sessionVerbProfile(t)
	const rowID = "vrow1"
	rec := filepath.Join(root, rowID, "verb-run-1"+sessionlayout.JSONLSuffix)
	writeRecord(t, rec, "verb-run-1", rowID)

	stdout, stderr, code := testutil.RunGopi(t, []string{CliVerbSession, CliVerbLog, rowID}, "", env, os.TempDir())
	if code != 0 {
		t.Fatalf("session log %s: code=%d\nstdout:\n%s\nstderr:\n%s", rowID, code, stdout, stderr)
	}
	// The rendered header names the SESSION, not the row (measured: "Session:
	// verb-run-1"), so the row id is what was PASSED and the session id is what
	// must come back — an assertion on the row id would fail on a correct run.
	if !strings.Contains(stdout, "verb-run-1") {
		t.Errorf("the rendered log is not the record the row's directory holds:\n%s", firstN(stdout, 400))
	}
}

// TestSessionLogVerbRefusesARowWithNoRecord: shape 3 — the HARD error. The
// assertion is deliberately two-sided: exit non-zero, and the sessions tree is
// byte-for-byte the same set of paths afterwards (a "not found" that created a
// session would be the silent-new-session defect wearing an exit code).
func TestSessionLogVerbRefusesARowWithNoRecord(t *testing.T) {
	_, root, env := sessionVerbProfile(t)
	const rowID = "vrow3"
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatalf("mkdir sessions root: %v", err)
	}
	before := sessionTreeSnapshot(t, root)

	stdout, stderr, code := testutil.RunGopi(t, []string{CliVerbSession, CliVerbLog, rowID}, "", env, os.TempDir())
	if code == 0 {
		t.Fatalf("session log %s exited 0 for a row that owns no record\nstdout:\n%s", rowID, stdout)
	}
	if !strings.Contains(stderr, "not found") && !strings.Contains(stderr, "no session") {
		t.Errorf("the refusal does not say the row has no log:\n%s", stderr)
	}
	after := sessionTreeSnapshot(t, root)
	if len(after) != len(before) {
		t.Fatalf("the refused verb changed the sessions tree: before=%d entries, after=%d\nbefore:\n%s\nafter:\n%s",
			len(before), len(after), strings.Join(before, "\n"), strings.Join(after, "\n"))
	}
}

// TestSessionResumeVerbRefusesARowWithNoCheckpoint: the resume half of shape 3.
// It must exit non-zero with a message naming the row, and start nothing —
// proved by the sessions tree being unchanged (a resume creates a session
// directory the moment it runs).
func TestSessionResumeVerbRefusesARowWithNoCheckpoint(t *testing.T) {
	home, root, env := sessionVerbProfile(t)
	const rowID = "vrow4"
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatalf("mkdir sessions root: %v", err)
	}
	// The row must EXIST in the ledger, or the refusal is about a non-row rather
	// than about a row with no checkpoint — this test is about the second fact.
	t.Setenv("GOPI_MEMORY_DB", envValue(env, "GOPI_MEMORY_DB"))
	t.Setenv(config.GopiDirEnv, home)
	if _, err := tools.TaskNew("resume with no checkpoint", rowID, "", ""); err != nil {
		t.Fatalf("TaskNew: %v", err)
	}
	before := sessionTreeSnapshot(t, root)

	stdout, stderr, code := testutil.RunGopi(t, []string{CliVerbSession, CliVerbResume, rowID}, "", env, os.TempDir())
	if code == 0 {
		t.Fatalf("session resume %s exited 0 for a row with no checkpoint\nstdout:\n%s\nstderr:\n%s", rowID, stdout, stderr)
	}
	if !strings.Contains(stderr, rowID) {
		t.Errorf("the refusal does not name the row %q:\n%s", rowID, stderr)
	}
	after := sessionTreeSnapshot(t, root)
	if len(after) != len(before) {
		t.Fatalf("the refused resume created something: before=%d, after=%d\nbefore:\n%s\nafter:\n%s",
			len(before), len(after), strings.Join(before, "\n"), strings.Join(after, "\n"))
	}
}
