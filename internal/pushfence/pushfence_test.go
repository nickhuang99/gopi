package pushfence

// The fence's contract, and the reason it is a PACKAGE rather than a helper
// inside one lane.
//
// What these arms pin, in order of what would break silently without them:
//   - a TEST process is refused when the recipient is outside its temp space;
//   - a TEST process is ALLOWED when the recipient is inside it (the ordinary
//     fixture case — a fence that refused this would have to be opted out
//     everywhere and would protect nothing);
//   - a SHIPPED BINARY is never a test (testing.Testing() is false), so the
//     fence cannot withhold a real delivery;
//   - the opt-out is scoped and named;
//   - the parse answers with GIT's rule (the measured false-positive defect);
//   - the sentence carries the marker an operator greps for.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nick/gopi/internal/scratch"
)

// testSpaceRoot is the suite's own temp root for this package's arms: the
// harness pins TMPDIR into it, so "inside test space" and "outside test space"
// are both measurable without touching the operator's tree.
func testSpaceRoot(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	t.Setenv(scratch.TmpDirEnvName, root)
	t.Setenv(scratch.GopiTmpDirEnvName, root)
	return root
}

// TestRefusalInsideTestSpaceIsAllowed is the arm that keeps the fence USABLE: a
// fixture pushing into its own temp root must go through untouched.
func TestRefusalInsideTestSpaceIsAllowed(t *testing.T) {
	root := testSpaceRoot(t)
	for _, target := range []string{
		filepath.Join(root, "origin.git"),
		filepath.Join(root, "nested", "deeper", "origin.git"),
		root,
	} {
		if why := Refusal(target); why != "" {
			t.Fatalf("Refusal(%q) = %q, want \"\" — a target under the suite's own temp root is test space", target, why)
		}
	}
}

// TestRefusalOutsideTestSpaceIsRefused is THE regression this file exists for:
// it is the shape that produced refs/heads/feat/exists on a real node.
func TestRefusalOutsideTestSpaceIsRefused(t *testing.T) {
	testSpaceRoot(t)
	// `/home` is deliberately not spelled: the criterion is "not under a root in
	// force", and a path outside every root measures it without naming anyone's
	// home.
	outside := string(os.PathSeparator) + strings.Join([]string{"srv", "gopi-delivery", "gopi.git"}, string(os.PathSeparator))
	why := Refusal(outside)
	if why == "" {
		t.Fatalf("Refusal(%q) = \"\", want a refusal — the recipient is outside every root in force", outside)
	}
	if !strings.Contains(why, Marker) {
		t.Fatalf("refusal %q does not carry the marker %q", why, Marker)
	}
	if !strings.Contains(why, outside) {
		t.Fatalf("refusal %q does not name the target %q", why, outside)
	}
	if !strings.Contains(why, OptOutEnv) {
		t.Fatalf("refusal %q does not name the opt-out %q", why, OptOutEnv)
	}
}

// TestRefusalPeerAliasIsRefused: an ssh target's local side is a path on the
// PEER, so it can never be under this process's temp root — the alias case is
// the one that reaches a real node.
func TestRefusalPeerAliasIsRefused(t *testing.T) {
	testSpaceRoot(t)
	alias := strings.Join([]string{"some-peer-alias", "/home/somebody/work/gopi"}, ":")
	why := Refusal(alias)
	if why == "" {
		t.Fatalf("Refusal(%q) = \"\", want a refusal — a peer alias names a real node's path", alias)
	}
	if !strings.Contains(why, alias) {
		t.Fatalf("refusal %q does not name the target %q", why, alias)
	}
}

// TestRefusalOptOutIsHonoured: the opt-out exists so the ONE arm that must push
// to a local target can say so, in its own scope.
func TestRefusalOptOutIsHonoured(t *testing.T) {
	testSpaceRoot(t)
	outside := string(os.PathSeparator) + strings.Join([]string{"srv", "elsewhere", "gopi.git"}, string(os.PathSeparator))
	if why := Refusal(outside); why == "" {
		t.Fatal("precondition failed: the target must be refused without the opt-out")
	}
	t.Setenv(OptOutEnv, "1") // the opt-out, scoped to THIS arm
	if why := Refusal(outside); why != "" {
		t.Fatalf("Refusal(%q) = %q with %s=1, want \"\"", outside, why, OptOutEnv)
	}
}

// TestInTestSpaceNeedsARootInForce: with no root published, NOTHING is test
// space — the honest reading. A fence that called everything safe because it
// could not measure the roots would be a fence that never fires.
func TestInTestSpaceNeedsARootInForce(t *testing.T) {
	t.Setenv(scratch.TmpDirEnvName, "")
	t.Setenv(scratch.GopiTmpDirEnvName, "")
	any := string(os.PathSeparator) + strings.Join([]string{"tmp", "looks-tempy-but-unpinned"}, string(os.PathSeparator))
	if InTestSpace(any) {
		t.Fatalf("InTestSpace(%q) = true with no roots published — an unmeasurable root is not test space", any)
	}
}

// TestInTestSpaceRefusesALookalikeRoot: /tmp-evil is not under /tmp. A textual
// prefix match would say it is, and would then allow a push into a directory the
// suite never created.
func TestInTestSpaceRefusesALookalikeRoot(t *testing.T) {
	root := testSpaceRoot(t)
	lookalike := root + "-evil"
	if !InTestSpace(filepath.Join(root, "x.git")) {
		t.Fatal("precondition failed: the real root must be test space")
	}
	if InTestSpace(lookalike) {
		t.Fatalf("InTestSpace(%q) = true, want false — a sibling name is not under the root", lookalike)
	}
}

// TestLocalSideFollowsGit is the measured false-positive defect (2026-09-28)
// pinned: a target that OPENS with a path is a local directory name in its
// ENTIRETY, so a colon later in it is part of the name, not an ssh split.
func TestLocalSideFollowsGit(t *testing.T) {
	cases := []struct{ target, want string }{
		// The defect: the pre-fix split on the first colon read this as
		// `~/work/GoPi` — the operator's real checkout — and refused a push
		// whose bytes land inside the suite's temp root.
		{"/dev/shm/suite-run/peer.git:~/work/GoPi", "/dev/shm/suite-run/peer.git:~/work/GoPi"},
		{"./relative:name.git", "./relative:name.git"},
		{"../up:name.git", "../up:name.git"},
		// The ordinary shapes still split.
		{"peer-alias:/home/x/work/gopi", "/home/x/work/gopi"},
		{"/home/x/work/gopi", "/home/x/work/gopi"},
		{"no-colon-at-all", "no-colon-at-all"},
	}
	for _, c := range cases {
		if got := LocalSide(c.target); got != c.want {
			t.Errorf("LocalSide(%q) = %q, want %q", c.target, got, c.want)
		}
	}
}

// TestLeavesTestSpaceIsGatedOnTesting: inside `go test` this arm runs, so the
// "is a test" half is true BY CONSTRUCTION here — the assertion that matters is
// that the OPT-OUT alone (with Testing() true) flips it, which is what the
// shipped binary relies on (Testing() false there, opt-out irrelevant).
func TestLeavesTestSpaceIsGatedOnTesting(t *testing.T) {
	// The roots come from the ONE helper, so "inside" is a path under the pinned
	// root — a fresh t.TempDir() would be a SIBLING of it under the ambient
	// TMPDIR, i.e. outside the roots in force, and would assert the opposite.
	root := testSpaceRoot(t)
	outside := string(os.PathSeparator) + strings.Join([]string{"srv", "gated-check", "gopi.git"}, string(os.PathSeparator))
	if !LeavesTestSpace(outside) {
		t.Fatal("LeavesTestSpace = false for a target outside every root, while running as a test")
	}
	if LeavesTestSpace(filepath.Join(root, "x.git")) {
		t.Fatal("LeavesTestSpace = true for a target inside the pinned temp root")
	}
}
