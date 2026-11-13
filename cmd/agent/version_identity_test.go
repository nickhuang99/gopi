package main

// The epic's regression guard: gopi's identity is INTERNAL and HARD-CODED, so
// `gopi version` answers the same thing wherever it is run from.
//
// WHY THIS IS A TEST AND NOT A PROSE RULE. gopi merges ANY git repo and is
// routinely run from directories that are not its own checkout — a scratch
// repo, a tarball extract, /tmp. The pre-change command derived its top line
// from the checkout the process stood in (a `version/*` tag the merger had
// minted, or the bare revision), so the SAME binary answered differently
// depending on cwd, and answered "dev" when the checkout could not name it.
// This test pins the opposite: one string, from the compiled-in constant, with
// the build metadata below it read out of the ARTIFACT.
//
// The two directories are deliberately the two shapes the epic names: a
// directory that is not a git repository at all, and a git repository with a
// single commit and NO `version/*` tag — the checkout that, under the old
// design, could lend no name and made the binary print a bare revision.

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nick/gopi/internal/testutil"
	gopiversion "github.com/nick/gopi/internal/version"
)

const (
	// versionRepoDirName is the scratch repository's directory name (Rule 1b:
	// it is used by the fixture, the fixture's own assertions and the tag
	// count below, so it is spelled once).
	versionRepoDirName = "tagless-repo"
	// versionTagGlob is the namespace the retired mint wrote under; the
	// fixture asserts it is EMPTY, which is what makes the scratch repo the
	// "no name to borrow" case the epic names.
	versionTagGlob = "version/*"
)

// versionFixtureRepo makes a real git repository with ONE commit and no tags —
// the "cannot lend a name" checkout. It is a fixture in the /tmp ban's sense:
// the files live under t.TempDir(), which the suite pins off the / partition.
func versionFixtureRepo(t *testing.T) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), versionRepoDirName)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", dir, err)
	}
	for _, args := range [][]string{
		{"init", "-q", "-b", "master"},
		{"config", "user.email", "version-test@localhost"},
		{"config", "user.name", "version-test"},
	} {
		versionFixtureGit(t, dir, args...)
	}
	if err := os.WriteFile(filepath.Join(dir, "f.txt"), []byte("one commit, no tags\n"), 0o644); err != nil {
		t.Fatalf("write fixture file: %v", err)
	}
	versionFixtureGit(t, dir, "add", "f.txt")
	versionFixtureGit(t, dir, "commit", "-qm", "c1")
	if tags := strings.TrimSpace(versionFixtureGit(t, dir, "tag", "-l", versionTagGlob)); tags != "" {
		t.Fatalf("fixture is not tagless: %s carries %q", dir, tags)
	}
	return dir
}

func versionFixtureGit(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return string(out)
}

// runVersionAt executes the built binary's `version` verb with dir as its
// working directory, and returns its output verbatim (stdout+stderr, so a
// diagnostic cannot hide in the half nobody reads).
func runVersionAt(t *testing.T, bin, dir string) string {
	t.Helper()
	cmd := exec.Command(bin, CliVerbVersion)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("%s %s (cwd %s): %v\n%s", bin, CliVerbVersion, dir, err, out)
	}
	return string(out)
}

// versionTopLine is the first line of a version answer — the fact the epic is
// about (the rest is metadata about the artifact, not the identity).
func versionTopLine(out string) string {
	return strings.SplitN(strings.TrimRight(out, "\n"), "\n", 2)[0]
}

// TestVersionTopLineIsTheInternalVersion pins the deliverable's first
// acceptance: the top line is internal/version.Version — a constant compiled
// into this artifact — run from a directory that is not a git repository at
// all.
//
// RED on the pre-change tree: the old buildVersion had no compiled-in constant
// to print. Built without a VCS stamp its answer was the DevName fallback —
// literally "dev" — so this assertion fails against the old binary, and it was
// verified by running that binary (see devdoc/version-identity.md).
func TestVersionTopLineIsTheInternalVersion(t *testing.T) {
	bin := testutil.GopiBin(t)
	outside := t.TempDir() // not a git repository, and not this checkout
	out := runVersionAt(t, bin, outside)
	top := versionTopLine(out)
	if top != gopiversion.Version {
		t.Fatalf("gopi version (cwd %s) top line = %q, want the internal version %q\n--- output ---\n%s",
			outside, top, gopiversion.Version, out)
	}
	// The identity must be a well-formed version, not a revision borrowed from
	// somewhere: the build's own metadata may not become the top line.
	if err := gopiversion.Validate(top); err != nil {
		t.Fatalf("top line %q is not a version: %v", top, err)
	}
}

// TestVersionIsIdenticalInAndOutOfACheckout is the epic's checkout-
// independence acceptance, in its strongest form: the WHOLE answer — identity
// and metadata alike — is byte-identical whether the binary is run from a
// non-repository directory or from a git checkout with no version name.
//
// The two directories differ in every way this command's identity could
// legitimately have depended on, so an equal answer is the proof that nothing
// about the checkout reaches it.
func TestVersionIsIdenticalInAndOutOfACheckout(t *testing.T) {
	bin := testutil.GopiBin(t)
	outside := t.TempDir()
	repo := versionFixtureRepo(t)

	fromOutside := runVersionAt(t, bin, outside)
	fromRepo := runVersionAt(t, bin, repo)

	if versionTopLine(fromOutside) != gopiversion.Version {
		t.Fatalf("from %s (no repo): top line %q, want %q", outside, versionTopLine(fromOutside), gopiversion.Version)
	}
	if versionTopLine(fromRepo) != gopiversion.Version {
		t.Fatalf("from %s (tagless repo): top line %q, want %q", repo, versionTopLine(fromRepo), gopiversion.Version)
	}
	if fromOutside != fromRepo {
		t.Fatalf("gopi version depends on the checkout:\n--- cwd %s (no repo) ---\n%s\n--- cwd %s (tagless repo) ---\n%s",
			outside, fromOutside, repo, fromRepo)
	}
}

// TestVersionDoesNotBorrowANameTheCheckoutCarries is the same statement
// against the exact temptation the old design gave in to: a checkout that DOES
// carry a `version/*` tag (the real repository carries 183 of them) must not
// change one byte of the answer either.
//
// This is the state the command used to report on: it borrowed the nearest
// reachable tag and printed it as the binary's identity. Now the tag is not
// consultable at all, because the command never asks git anything.
func TestVersionDoesNotBorrowANameTheCheckoutCarries(t *testing.T) {
	bin := testutil.GopiBin(t)
	repo := versionFixtureRepo(t)
	before := runVersionAt(t, bin, repo)

	versionFixtureGit(t, repo, "tag", gopiversion.Namespace+"v0.0.0-a-checkout-name")
	after := runVersionAt(t, bin, repo)

	if after != before {
		t.Fatalf("the answer changed when the checkout gained a %s tag:\n--- before ---\n%s\n--- after ---\n%s",
			versionTagGlob, before, after)
	}
	if versionTopLine(after) != gopiversion.Version {
		t.Fatalf("top line after tagging = %q, want the internal version %q", versionTopLine(after), gopiversion.Version)
	}
}
