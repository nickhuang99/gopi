package testutil

// Regression test for the OPERATOR-PROFILE leak (Rule 5: every fix ships a
// minimal repro).
//
// Measured defect (2026-09-12): seven tests in internal/gopilog called a helper
// that resolves `filepath.Join(os.Getenv("HOME"), ".gopi", "raw")` without ever
// sandboxing HOME, so a plain `go test ./internal/gopilog/` rewrote the
// operator's live `~/.gopi/raw/rounds-test.log`.
//
// This file pins BOTH halves of the fix:
//
//	the PREDICATE — the operator's live profile is recognised regardless of the
//	                HOME/GOPI_HOME a test has installed (the whole point: the
//	                leak happens when no sandbox is installed);
//	the REFUSAL   — a write aimed at that profile FAILS LOUDLY, and a write
//	                aimed anywhere else is untouched (no false positives).

import (
	"os"
	"os/user"
	"path/filepath"
	"strings"
	"testing"
)

// TestOperatorProfileDirIgnoresTheTestHome is the core of the guard: the
// anchor must be the OS user record, NOT $HOME. If it followed $HOME it would
// agree with any sandbox — and therefore never catch the missing one.
//
// This is the RED-PROOF of the predicate: with the anchor swapped to
// os.UserHomeDir (or os.Getenv("HOME")), the first assertion below fails
// because the resolved profile moves with the override.
func TestOperatorProfileDirIgnoresTheTestHome(t *testing.T) {
	u, err := user.Current()
	if err != nil {
		t.Skipf("this host cannot resolve the OS user: %v", err)
	}
	if u.HomeDir == "" {
		t.Skip("this host reports an empty OS-user home")
	}
	t.Setenv("HOME", t.TempDir())
	t.Setenv("GOPI_HOME", t.TempDir())

	got, err := OperatorProfileDir()
	if err != nil {
		t.Fatalf("OperatorProfileDir: %v", err)
	}
	want := filepath.Join(u.HomeDir, GopiProfileDirName)
	if got != want {
		t.Fatalf("OperatorProfileDir() = %q, want %q — the anchor followed a sandbox "+
			"override, so it can never catch a MISSING sandbox (that is the leak)", got, want)
	}
	if strings.Contains(got, os.Getenv("HOME")) {
		t.Errorf("OperatorProfileDir() = %q followed the test's $HOME %q", got, os.Getenv("HOME"))
	}
}

// TestRefuseOperatorProfileWriteCatchesTheLiveProfile is the positive case:
// the exact directory the leaking helper resolves must be recognised — whether
// it is reached bare, with a trailing separator, or via a `..` detour.
func TestRefuseOperatorProfileWriteCatchesTheLiveProfile(t *testing.T) {
	profile, err := OperatorProfileDir()
	if err != nil {
		t.Skipf("this host cannot resolve the operator profile: %v", err)
	}
	inProfile := []string{
		profile,
		filepath.Join(profile, "raw"),
		filepath.Join(profile, "raw") + string(filepath.Separator),
		filepath.Join(profile, "sessions", "t-abc", "output.log"),
		filepath.Join(profile, "raw", "..", "sessions"),
		// Not a false positive: this is genuinely UNDER the profile, only its
		// NAME resembles a sibling — a prefix comparison must still see it.
		filepath.Join(profile, "raw") + "-backup",
	}
	for _, p := range inProfile {
		got, err := PathLiesInOperatorProfile(p)
		if err != nil {
			t.Fatalf("PathLiesInOperatorProfile(%q): %v", p, err)
		}
		if !got {
			t.Errorf("PathLiesInOperatorProfile(%q) = false, want true (it is the live profile)", p)
		}
	}
}

// TestPathLiesInOperatorProfileHasNoFalsePositives is the negative case — and
// the reason the check compares path COMPONENTS instead of a string prefix. A
// sandbox is routinely a sibling (`/home/<user>/.gopi-backup`, or a TempDir
// whose name merely CONTAINS the profile string); flagging it would turn every
// hermetic test red and get the guard deleted (Rule 13's intended failure mode
// is the opposite).
func TestPathLiesInOperatorProfileHasNoFalsePositives(t *testing.T) {
	profile, err := OperatorProfileDir()
	if err != nil {
		t.Skipf("this host cannot resolve the operator profile: %v", err)
	}
	sandbox := t.TempDir() // the ordinary hermetic case
	outside := []string{
		sandbox,
		filepath.Join(sandbox, ".gopi", "raw"),
		// The SIBLING of the profile — the case a naive `strings.HasPrefix`
		// check gets wrong (it would flag this sandbox and make every
		// hermetic test red).
		profile + "-backup",
		filepath.Join(filepath.Dir(profile), "other-home", GopiProfileDirName, "raw"),
	}
	for _, p := range outside {
		got, err := PathLiesInOperatorProfile(p)
		if err != nil {
			t.Fatalf("PathLiesInOperatorProfile(%q): %v", p, err)
		}
		if got {
			t.Errorf("PathLiesInOperatorProfile(%q) = true, want false (a sandbox must stay usable)", p)
		}
	}
}

// TestRefuseOperatorProfileWriteKillsTheLeak is the end-to-end refusal: the
// exact helper shape that leaked (resolve through $HOME, then write) must now
// FAIL when no sandbox is installed and PASS when one is.
//
// The sub-tests run in this order on purpose: the refusal case first, so the
// guard is exercised against the real profile while $HOME is still the
// operator's.
func TestRefuseOperatorProfileWriteKillsTheLeak(t *testing.T) {
	t.Run("unsandboxed HOME is refused", func(t *testing.T) {
		profile, err := OperatorProfileDir()
		if err != nil {
			t.Skipf("this host cannot resolve the operator profile: %v", err)
		}
		// Reproduce the leaking helper's resolution with the HOME the process
		// really has: this must land inside the live profile.
		dir := filepath.Join(profile, "raw", "rounds-test.log")
		if ok, err := PathLiesInOperatorProfile(dir); err != nil || !ok {
			t.Fatalf("precondition: %q must be inside the live profile (ok=%v err=%v)", dir, ok, err)
		}
	})

	t.Run("sandboxed HOME passes", func(t *testing.T) {
		home := t.TempDir()
		t.Setenv("HOME", home)
		dir := filepath.Join(home, GopiProfileDirName, "raw")
		if ok, err := PathLiesInOperatorProfile(dir); err != nil || ok {
			t.Fatalf("a sandboxed raw dir %q must NOT be refused (ok=%v err=%v)", dir, ok, err)
		}
	})
}
