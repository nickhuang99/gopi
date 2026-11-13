// session_list_unreadable_index_test.go — the two claims `session list` used to
// make at once, and the exit code that made them worse.
//
// THE DEFECT (filed 2026-11-06 as a bug row, root-caused and fixed here). On a
// fresh empty GOPI_HOME:
//
//	$ GOPI_HOME=<empty dir> gopi session list --limit 1
//	0 session(s):
//	No sessions found.
//	[degraded] the chat index could not be read: unable to open database file: out of memory (14)
//	$ echo $?
//	0
//
// Three separate defects were nested in that one screenful:
//
//  1. THE PROFILE WAS NEVER CREATED. `openLedgerIndexDB`'s openability probe
//     called openDB directly, whose first statement creates the FILE and
//     therefore needs the DIRECTORY. Nothing made the directory, so the open
//     failed — while the sibling face `gopi task list` on an equally fresh home
//     succeeded, because it reaches initKnowledgeBase (which mkdirs). An empty
//     profile is not a broken index; it is a profile with nothing in it.
//  2. THE MESSAGE NAMED NO PATH AND REPORTED A FALSE CAUSE. The driver's
//     sentence for SQLITE_CANTOPEN is "unable to open database file: out of
//     memory (14)", which reads as an allocation failure and sends a reader to
//     `free -m` / cgroup limits / the OOM killer for a defect that is a PATH or
//     a PERMISSION.
//  3. "INDEX UNREADABLE" WAS INDISTINGUISHABLE FROM "NO SESSIONS". Both lines
//     printed together, and both exited 0 — a script could not tell a measured
//     empty view from a half-read one.
//
// The arms below are the three outcomes the command must keep distinct:
// measured-empty (0), partially-read (3), refused (1). Each would have failed
// on the pre-fix binary; each is driven through the REAL entry point
// (runSessionList), not a helper this test invented (Rule 31).
package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nick/gopi/internal/config"
)

// sessionListEmptyProfileEnv points GOPI_HOME at a directory that exists and
// holds NOTHING — a first run, not a broken install. It deliberately does NOT
// use testutil.NewTestDBPath: that scaffold CREATES a ledger, and the state
// under test is precisely the absence of one.
func sessionListEmptyProfileEnv(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("GOPI_HOME", home)
	// Mask the legacy spelling so the resolution cannot fall back to the
	// operator's own ledger (Rule 32: a test never reads the live profile).
	t.Setenv("GOPI_MEMORY_DB", "")
	t.Setenv(config.MemoryDBEnvLegacy, "")
	// Pin the settings document to a path inside the fixture that does NOT
	// exist. Without this the resolver's rank 3 reads the OPERATOR's
	// settings.json and a `memory_db` key there silently points the ledger out
	// of the fixture — measured while writing this test: the profile under
	// GOPI_HOME stayed empty because the ledger resolved somewhere else.
	t.Setenv(config.SettingsPathEnv, filepath.Join(home, "no-settings.json"))
	if _, err := os.Stat(filepath.Join(home, config.ProfileDirName, config.MemoryDBFileName)); err == nil {
		t.Fatalf("the fixture is not empty: %s already holds a ledger — the state this test needs was not produced",
			filepath.Join(home, config.ProfileDirName, config.MemoryDBFileName))
	}
	return home
}

// TestSessionListOnAFreshProfileIsAMeasuredEmptyView is arm ①: a first run on
// an empty profile prints "No sessions found." and exits 0, with NO [degraded]
// line. A brand-new profile has no index to be degraded about, and telling a
// first-time user their install is damaged is the report that sends them
// looking for a fault that does not exist.
func TestSessionListOnAFreshProfileIsAMeasuredEmptyView(t *testing.T) {
	home := sessionListEmptyProfileEnv(t)

	var code int
	out := captureStdoutAndStderr(t, func() int {
		code = runSessionList([]string{sessionLimitFlag, "1"})
		return code
	})

	if code != 0 {
		t.Errorf("a fresh profile listed with exit %d, want 0 — an empty profile is a measured empty view, "+
			"not a refused or half-read one\noutput:\n%s", code, out)
	}
	if strings.Contains(out, sessionFilteredPrefixDegraded()) {
		t.Errorf("a fresh profile reported a DEGRADED chat index — nothing was broken, the profile had simply "+
			"never been created. This is the defect the fix removes.\noutput:\n%s", out)
	}
	if !strings.Contains(out, sessionSourceEmptyResult) {
		t.Errorf("a fresh profile did not print %q — the caller cannot tell an empty view from a silent one\noutput:\n%s",
			sessionSourceEmptyResult, out)
	}
	// The ledger really is there afterwards: the fix creates the profile, it
	// does not merely suppress the message. THIS is the root-cause assertion —
	// the pre-fix binary left the directory uncreated and reported the failure
	// as a degraded index.
	ledger := filepath.Join(home, config.ProfileDirName, config.MemoryDBFileName)
	if _, err := os.Stat(ledger); err != nil {
		t.Errorf("no ledger at %s after the listing: %v — the profile directory was created but the index was "+
			"never opened", ledger, err)
	}
}

// sessionFilteredPrefixDegraded returns the [degraded] prefix the renderer uses.
func sessionFilteredPrefixDegraded() string {
	return strings.Fields(sessionDegradedFormat)[0]
}

// TestSessionListOnAnUnreadableIndexSaysSoAndFails is arm ②, the
// refuse-to-overreach half: a GENUINELY broken index (a file that is not a
// database — a truncated or corrupted ledger) must still be loud. The fix must
// not buy quiet by deleting the degraded path.
//
// It also pins the two distinctions the defect collapsed: the zero-row sentence
// must not claim the listing was measured, and the exit code must not claim
// success.
func TestSessionListOnAnUnreadableIndexSaysSoAndFails(t *testing.T) {
	home := t.TempDir()
	t.Setenv("GOPI_HOME", home)
	t.Setenv("GOPI_MEMORY_DB", "")

	ledger := filepath.Join(home, config.ProfileDirName, config.MemoryDBFileName)
	if err := os.MkdirAll(filepath.Dir(ledger), 0o755); err != nil {
		t.Fatalf("mkdir profile: %v", err)
	}
	// A file that EXISTS and is UNREADABLE BY THE DRIVER — not absent. This is
	// the state the fix must not silence: the reader asked about an index that
	// is there and could not be read.
	if err := os.WriteFile(ledger, []byte("this is not a sqlite database, it is text"), 0o644); err != nil {
		t.Fatalf("write corrupt ledger: %v", err)
	}

	var code int
	out := captureStdoutAndStderr(t, func() int {
		code = runSessionList([]string{sessionLimitFlag, "1"})
		return code
	})

	if !strings.Contains(out, sessionFilteredPrefixDegraded()) {
		t.Errorf("an unreadable chat index printed no %s disclosure — a half-read listing was reported as a "+
			"clean one\noutput:\n%s", sessionFilteredPrefixDegraded(), out)
	}
	if code != sessionDegradedExit {
		t.Errorf("an unreadable chat index exited %d, want %d — a half-read listing must not report success, "+
			"or a script cannot tell it from a measured empty view\noutput:\n%s", code, sessionDegradedExit, out)
	}
	if strings.Contains(out, sessionSourceEmptyResult+"\n") {
		t.Errorf("the listing claimed %q while its chat index could not be read — the two lines contradict "+
			"each other, which is the defect this arm removes\noutput:\n%s", sessionSourceEmptyResult, out)
	}
	if !strings.Contains(out, sessionUnreadableResult) {
		t.Errorf("the zero-row line did not distinguish a half-read listing from a measured empty one "+
			"(want %q)\noutput:\n%s", sessionUnreadableResult, out)
	}
}

// TestSessionListOnAnUnopenableIndexNamesThePathAndTheRealCause is arm ③: a
// ledger path that cannot be OPENED must be disclosed with gopi's own sentence
// naming the FILE, and the run must exit non-zero instead of claiming success.
//
// The state is produced the only honest way from outside: a ledger path whose
// parent is a REGULAR FILE, so the directory can never be created and the open
// can never succeed. That is not "absent" (absent is arm ①'s normal empty
// profile) — it is "present and unreachable", which is the family CANTOPEN
// belongs to. NOTE which sentence this arm gets: the directory cannot be made,
// so the guard fires first and names the mkdir target. That is the correct
// answer HERE, and it is why it is asserted instead of the CANTOPEN sentence —
// the CANTOPEN translation itself is pinned by
// internal/tools/ledger_open_cause_test.go, which can produce a real code-14
// error from the driver.
func TestSessionListOnAnUnopenableIndexNamesThePathAndTheRealCause(t *testing.T) {
	home := t.TempDir()
	t.Setenv("GOPI_HOME", home)
	t.Setenv(config.SettingsPathEnv, filepath.Join(home, "no-settings.json"))

	blocker := filepath.Join(home, "blocker")
	if err := os.WriteFile(blocker, []byte("a regular file, not a directory"), 0o644); err != nil {
		t.Fatalf("write blocker file: %v", err)
	}
	unopenable := filepath.Join(blocker, config.MemoryDBFileName)
	t.Setenv("GOPI_MEMORY_DB", unopenable)

	var code int
	out := captureStdoutAndStderr(t, func() int {
		code = runSessionList([]string{sessionLimitFlag, "1"})
		return code
	})

	if code != sessionDegradedExit {
		t.Errorf("an unopenable chat index exited %d, want %d\noutput:\n%s", code, sessionDegradedExit, out)
	}
	if !strings.Contains(out, unopenable) {
		t.Errorf("the disclosure does not name the ledger path %s — the driver's own sentence names neither the "+
			"file nor the probe\noutput:\n%s", unopenable, out)
	}
	if strings.Contains(out, "out of memory") {
		t.Errorf("the disclosure repeated the driver's false allocation diagnosis with nothing beside it — a reader "+
			"is left hunting memory pressure for a path defect\noutput:\n%s", out)
	}
	if strings.Contains(out, sessionSourceEmptyResult+"\n") {
		t.Errorf("the listing claimed %q while its chat index could not be opened\noutput:\n%s", sessionSourceEmptyResult, out)
	}
}
