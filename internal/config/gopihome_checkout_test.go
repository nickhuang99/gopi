package config

import (
	"os"
	"path/filepath"
	"testing"
)

// ---------------------------------------------------------------------------
// Red proof + keep arm for GopiHomeNamesCodeCheckout (epic: a child must never
// resolve a throwaway profile).
//
// The defect: GOPI_HOME is documented as a HOME and the profile root is
// `<GOPI_HOME>/<ProfileDirName>`, so a value naming a GIT WORK TREE makes every
// descendant resolve `<worktree>/.gopi` — a throwaway profile inside a code
// checkout that nothing sweeps, nothing dispatches from and nothing reads back.
//
// The predicate is the marker test, and BOTH arms are pinned here: a checkout is
// recognised (the strip's input) and every legitimate home is NOT (the keep arm
// the sandbox machinery depends on).
// ---------------------------------------------------------------------------

// markCheckout materialises the one thing that makes a directory a code
// checkout: the marker entry (a real directory here, the form a work tree and a
// `git init` share; a worktree's `.git` is a regular FILE and takes the same
// path through the predicate because the test is existence, not mode).
func markCheckout(t *testing.T, dir string) string {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(dir, CheckoutMarkerName), 0o755); err != nil {
		t.Fatalf("mark %s as a checkout: %v", dir, err)
	}
	return dir
}

// TestGopiHomeNamesCodeCheckout_RecognisesACheckout is the RED side: remove the
// predicate body (return false) and this arm fails, which is exactly the
// mutation the epic asks for.
func TestGopiHomeNamesCodeCheckout_RecognisesACheckout(t *testing.T) {
	wt := markCheckout(t, t.TempDir())
	if !GopiHomeNamesCodeCheckout(wt) {
		t.Fatalf("GopiHomeNamesCodeCheckout(%q) = false — a directory carrying %q IS a code checkout; "+
			"without this arm a run resolves %s/<profile> as its profile", wt, CheckoutMarkerName, wt)
	}

	// A worktree's marker is a FILE (a gitdir pointer), not a directory. It must
	// be refused the same way: the test is existence, never the mode.
	fileWT := t.TempDir()
	if err := os.WriteFile(filepath.Join(fileWT, CheckoutMarkerName), []byte("gitdir: /nowhere\n"), 0o644); err != nil {
		t.Fatalf("write worktree marker file: %v", err)
	}
	if !GopiHomeNamesCodeCheckout(fileWT) {
		t.Fatalf("GopiHomeNamesCodeCheckout(%q) = false for a WORKTREE-shaped checkout "+
			"(marker is a file) — the predicate must key on existence", fileWT)
	}
}

// TestGopiHomeNamesCodeCheckout_KeepsRealHomes is the KEEP ARM: a sandbox home,
// an ordinary home and every non-claim must come back false, or the guard would
// break the documented sandboxed-profile path it is meant to protect.
func TestGopiHomeNamesCodeCheckout_KeepsRealHomes(t *testing.T) {
	sandbox := t.TempDir() // what `gopi tool sandbox create` materialises: a real
	// directory holding a PROFILE — and no checkout marker.
	if err := os.MkdirAll(ProfileDirIn(sandbox), 0o755); err != nil {
		t.Fatalf("materialise a sandbox profile: %v", err)
	}
	ordinaryHome := t.TempDir()
	nearMiss := t.TempDir()
	// A directory whose name merely CONTAINS the marker word is not a checkout.
	if err := os.MkdirAll(filepath.Join(nearMiss, CheckoutMarkerName+"-backup"), 0o755); err != nil {
		t.Fatalf("materialise near-miss dir: %v", err)
	}

	cases := []struct {
		name string
		home string
	}{
		{"sandbox home (GOPI_HOME=$SB — the documented spelling)", sandbox},
		{"ordinary empty home", ordinaryHome},
		{"near miss: a sibling named .git-backup", nearMiss},
		{"empty value (nothing published)", ""},
		{"blank value", "   "},
		{"relative value is not a checkout claim", "build/checkout"},
	}
	for _, c := range cases {
		if GopiHomeNamesCodeCheckout(c.home) {
			t.Fatalf("GopiHomeNamesCodeCheckout(%q) = true for %s — the keep arm is what lets a "+
				"sandboxed profile inherit to children", c.home, c.name)
		}
	}
}
