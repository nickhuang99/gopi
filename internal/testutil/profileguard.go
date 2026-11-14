package testutil

// The TEST-HERMETICITY contract against the OPERATOR'S LIVE PROFILE.
//
// A test that resolves a path through $HOME is only hermetic while its own
// `t.Setenv("HOME", t.TempDir())` is in force. Omit that line — or share one
// helper between tests that set it and tests that do not — and the test writes
// into the operator's real `~/.gopi`. Measured 2026-09-12: seven tests in
// internal/gopilog called `writeRawLog` (which resolves
// `filepath.Join(os.Getenv("HOME"), ".gopi", "raw")`) with NO HOME override, so
// every `go test ./internal/gopilog/` rewrote the operator's live
// `~/.gopi/raw/rounds-test.log` — md5 unchanged, mtime moved two minutes into
// the future of a "read-only" verification run. Earlier instance of the same
// class: commit f6694fc8 ("stop the web-signal test from logging into the real
// /home/user/.gopi").
//
// WHY the anchor below cannot simply be "the HOME this test set": the guard's
// whole job is to catch a MISSING override, and a missing override makes the
// test's HOME *be* the operator's HOME — the two are indistinguishable from
// the test's own environment. The one identity a test cannot fake is the OS
// user record, so that is what this package reads:
//
//	user.Current().HomeDir   ← passwd/directory-service based (verified to
//	                           ignore a HOME override in BOTH CGO_ENABLED=0
//	                           and CGO_ENABLED=1 builds)
//	os.UserHomeDir()         ← $HOME first (Go ≥1.12), so it FOLLOWS the
//	                           override and can never be the anchor
//
// The guard is deliberately a REFUSAL (t.Fatalf) rather than a silent
// re-point: a test that meant to write into a temp profile must be told its
// sandbox did not engage, not quietly handed a different directory (Rule 15).

import (
	"fmt"
	"os/user"
	"path/filepath"
	"testing"

	"github.com/nickhuang99/gopi/internal/scratch"
)

// GopiProfileDirName is the gopi data-root directory name under a HOME
// (`~/.gopi`). Exported here so a test elsewhere can build the operator-profile
// path without a fresh `".gopi"` literal (Rule 1b/13). NOTE: this is a SECOND
// definition site beside internal/config's own `".gopi"` joins — config cannot
// import testutil (testutil is a test-support package), so the consolidation
// belongs in a leaf constants package; recorded as a follow-up rather than
// bundled into this change (see devdoc/rawlog-test-home-leak.md).
const GopiProfileDirName = ".gopi"

// OperatorProfileDir returns the OPERATOR'S live gopi profile root
// (`<os-user-home>/.gopi`) — the directory no test may write into.
//
// This deliberately ignores $HOME/GOPI_HOME: those are exactly the knobs a
// test uses for its sandbox, so honouring them would make the guard agree with
// the leak it exists to catch.
func OperatorProfileDir() (string, error) {
	u, err := user.Current()
	if err != nil {
		return "", fmt.Errorf("resolve the operator's OS user (os/user): %w", err)
	}
	if u.HomeDir == "" {
		return "", fmt.Errorf("os/user reported an empty home directory for user %q", u.Username)
	}
	return filepath.Join(u.HomeDir, GopiProfileDirName), nil
}

// PathLiesInOperatorProfile reports whether path is the operator's live
// profile root or anything beneath it. Both arguments are compared after
// filepath.Clean, so a trailing slash or a `..` segment cannot slip past.
func PathLiesInOperatorProfile(path string) (bool, error) {
	profile, err := OperatorProfileDir()
	if err != nil {
		return false, err
	}
	return scratch.PathWithin(filepath.Clean(path), filepath.Clean(profile)), nil
}

// RefuseOperatorProfileWrite fails the test when dir resolves into the
// operator's live gopi profile, naming the seam (`what`) and the fix. Call it
// immediately before a test writes, with the directory it is about to write
// into.
//
// A failure to determine the operator's home is itself reported rather than
// skipped: an unverifiable guard is the silent permission this contract exists
// to remove (Rule 15b).
func RefuseOperatorProfileWrite(t *testing.T, what, dir string) {
	t.Helper()
	// The profile is resolved ONCE, so the refusal below reports the very
	// directory the comparison used — and the error has nowhere to be
	// discarded (the value the gate would have had to guess at is the one
	// already in hand; Rule 15b).
	profile, err := OperatorProfileDir()
	if err != nil {
		t.Fatalf("%s: cannot verify %q is outside the operator's live profile: %v — "+
			"an unverifiable guard must not pass (Rule 15)", what, dir, err)
	}
	if !scratch.PathWithin(filepath.Clean(dir), filepath.Clean(profile)) {
		return
	}
	t.Fatalf("%s: refusing to write into the OPERATOR'S LIVE gopi profile %q "+
		"(a test must never touch %s). FIX: sandbox the resolution in the test — "+
		"t.Setenv(\"HOME\", t.TempDir()) (or GOPI_HOME) — before calling %s; "+
		"a helper shared with tests that already sandbox must not resolve $HOME itself.",
		what, dir, profile, what)
}
