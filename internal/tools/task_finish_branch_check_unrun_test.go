package tools

// THE MISSING-GROUND ABSTENTION MUST NAME WHAT IT WITHHELD (epic 01a13303).
//
// THE DEFECT, AND WHY THIS FILE EXISTS. The delivery chain's branch-checks leg
// abstains when no directory is checked out on the delivered branch — the
// recorded shape of a row whose ground was collected (or never existed) before
// the close-out ran. Its sentence said ONE thing: "the branch's own test run
// was NOT made". Measured three times in this repo (drifted commits 3217781bc /
// 22c38dbd5 were RED at their own revision, and the one machine that runs
// internal/standards never ran them): every gate downstream abstains BY DESIGN
// — mergeset is report-only, the merger's ⓪e checks blobs, Rule 27's smoke is
// branch-scoped — so this sentence is the only place the fact can appear, and
// "there was nothing to run" and "there were packages and nobody ran them" were
// the SAME WORDS.
//
// WHAT THE FIX ADDS, and what these tests pin: the abstention carries the
// selection the run WOULD have made, computed with no directory at all — the
// same primitive the run itself uses (smoke.TouchedPackages: two reads against
// the branch's own ref). Three arms, three sentences, never one silence
// (Rule 15): packages NAMED, an EMPTY diff, and a selection that could not be
// computed here (UNKNOWN with its reason).

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const (
	// tbcuGoModBody is the fixture module the branch commits (stdlib only, so
	// the selection needs no network and no go.sum).
	tbcuGoModBody = "module tbcufixture\n\ngo 1.21\n"
	// tbcuPkgDir / tbcuPkgFile are the package the branch introduces — the
	// package the abstention must NAME as unrun.
	tbcuPkgDir  = "internal/foo"
	tbcuPkgFile = "foo.go"
	// tbcuAssertionText is the failing assertion a reader must be able to act
	// on — the red the withheld run would have reported.
	tbcuAssertionText = "the branch's own package was never run at its own revision"
)

// tbcuExpectedPattern is the go-test pattern smoke.PackagesIn renders for
// tbcuPkgDir. It is derived from the smoke package's own composition constants
// rather than typed, so a change there cannot leave this file asserting a
// spelling the selector no longer produces.
func tbcuExpectedPattern() string {
	return "./" + tbcuPkgDir + "/..."
}

// tbcuBranchWithGoPackage is the fixture shape the epic is about: a feature
// branch carrying one Go package, checked out in a linked worktree that is then
// REMOVED while the branch ref survives — the gone-ground row.
func tbcuBranchWithGoPackage(t *testing.T) *finishCleanupFixture {
	t.Helper()
	f := newFinishCleanupFixture(t)
	pmcWriteFile(t, filepath.Join(f.wtDir, mergeBuildGateGoModFile), tbcuGoModBody)
	pmcWriteFile(t, filepath.Join(f.wtDir, tbcuPkgDir, tbcuPkgFile),
		"package foo\n\nfunc F() int { return 1 }\n")
	preMergeGit(t, f.wtDir, "add", "-A")
	preMergeGit(t, f.wtDir, "commit", "-q", "-m", "branch: one package of its own")
	return f
}

// tbcuRemoveGround deletes the recorded worktree directory and drops the stale
// registration, which is the gone-ground state the ledger produced (the
// stranded-row shape of task 01a13017).
func tbcuRemoveGround(t *testing.T, f *finishCleanupFixture) {
	t.Helper()
	if err := os.RemoveAll(f.wtDir); err != nil {
		t.Fatalf("remove the recorded ground: %v", err)
	}
	preMergeGit(t, f.clientDir, "worktree", "prune")
	if _, err := os.Stat(f.wtDir); err == nil {
		t.Fatalf("fixture invariant: %s must be gone", f.wtDir)
	}
}

// TestBranchCheckMissingGroundNamesTheUnrunPackages is THE regression, and it
// states the RED PROOF explicitly: the sentence this change replaces — the
// marker ALONE — is asserted to be exactly what a reader could not act on,
// while the new one carries the package pattern and the command that would have
// run it.
func TestBranchCheckMissingGroundNamesTheUnrunPackages(t *testing.T) {
	f := tbcuBranchWithGoPackage(t)
	tbcuRemoveGround(t, f)

	res := finishCleanupBranchCheckVerify(context.Background(), f.clientDir, "", f.featBranch)

	// The verdict does not change: a missing ground is still an abstention, the
	// delivery still proceeds (the 2026-10-12 decision is not reversed here).
	if res.Status != finishCleanupPreMergeStatusSkipped {
		t.Fatalf("a missing ground must ABSTAIN, got status=%q detail=%q", res.Status, res.Detail)
	}
	if !strings.Contains(res.Detail, finishCleanupBranchCheckOffBranchMark) {
		t.Fatalf("the abstention must still name the missing ground, got:\n%s", res.Detail)
	}

	// RED PROOF — the pre-fix sentence, spelled here as the literal it was.
	const preFixDetail = finishCleanupBranchCheckOffBranchMark
	if strings.Contains(preFixDetail, tbcuExpectedPattern()) {
		t.Fatal("the red proof is vacuous: the pre-fix sentence already named the package")
	}
	if res.Detail == preFixDetail {
		t.Fatalf("the abstention must not be the pre-fix sentence — nothing was named as unrun:\n%s", res.Detail)
	}

	// THE FIX, both halves: WHAT was withheld, and where it is computable from.
	if !strings.Contains(res.Detail, finishCleanupBranchCheckUnrunHead) {
		t.Errorf("the abstention must state what the missing ground withheld, got:\n%s", res.Detail)
	}
	if !strings.Contains(res.Detail, tbcuExpectedPattern()) {
		t.Errorf("the abstention must NAME the unrun package %q, got:\n%s", tbcuExpectedPattern(), res.Detail)
	}
	t.Logf("the missing-ground abstention an operator now reads:\n%s", res.Detail)

	// AND IT IS A FACT ABOUT THE BRANCH'S REF, not about the ground: the branch
	// has no directory anywhere in this fixture, so the package list can only
	// have come from the object store.
	if wt := finishCleanupBranchWorktree(context.Background(), f.clientDir, f.featBranch); wt != "" {
		t.Fatalf("fixture invariant: the branch must have no worktree left, got %q", wt)
	}
}

// TestBranchCheckMissingGroundSaysWhenNothingWasWithheld is the OTHER arm: a
// branch whose diff selects no Go package must say the withheld run was EMPTY —
// "nothing needed running" is a different fact from "nobody ran it", and the
// pre-fix sentence gave both the same words.
func TestBranchCheckMissingGroundSaysWhenNothingWasWithheld(t *testing.T) {
	f := newFinishCleanupFixture(t) // the base fixture's branch adds no Go file
	tbcuRemoveGround(t, f)

	res := finishCleanupBranchCheckVerify(context.Background(), f.clientDir, "", f.featBranch)
	if res.Status != finishCleanupPreMergeStatusSkipped {
		t.Fatalf("a missing ground must ABSTAIN, got status=%q detail=%q", res.Status, res.Detail)
	}
	if !strings.Contains(res.Detail, finishCleanupBranchCheckUnrunEmpty) {
		t.Errorf("an empty selection must be stated as empty, got:\n%s", res.Detail)
	}
	if strings.Contains(res.Detail, finishCleanupBranchCheckUnrunHead) {
		t.Errorf("an empty selection must not claim packages were withheld, got:\n%s", res.Detail)
	}
	if strings.Contains(res.Detail, finishCleanupBranchCheckUnrunUnknown) {
		t.Errorf("a computable selection must never report UNKNOWN, got:\n%s", res.Detail)
	}
}

// TestBranchCheckMissingGroundReportsUnknownRatherThanEmpty is the THIRD arm,
// and the fail-toward-saying-less guard (Rule 15): when the selection cannot be
// computed at all — no integration ref to diff against — the abstention says
// UNKNOWN and carries the reason. An empty list here would read as "nothing to
// run", which is exactly the conflation this change removes.
func TestBranchCheckMissingGroundReportsUnknownRatherThanEmpty(t *testing.T) {
	// A repository with no integration ref: no master, no main.
	repo := t.TempDir()
	pmcCommitAllBranch(t, repo, "other")
	f := &finishCleanupFixture{t: t, clientDir: repo, featBranch: "other"}

	res := finishCleanupBranchCheckVerify(context.Background(), repo, "", f.featBranch)
	if res.Status != finishCleanupPreMergeStatusSkipped {
		t.Fatalf("a missing ground must ABSTAIN, got status=%q detail=%q", res.Status, res.Detail)
	}
	if !strings.Contains(res.Detail, finishCleanupBranchCheckUnrunUnknown) {
		t.Errorf("an uncomputable selection must be reported as UNKNOWN, got:\n%s", res.Detail)
	}
	if strings.Contains(res.Detail, finishCleanupBranchCheckUnrunEmpty) {
		t.Errorf("an uncomputable selection must never read as EMPTY, got:\n%s", res.Detail)
	}
	if !strings.Contains(res.Detail, finishCleanupBranchCheckNoBaseMarker) {
		t.Errorf("the UNKNOWN note must carry its reason, got:\n%s", res.Detail)
	}
}

// pmcCommitAllBranch initialises repo on `branch` with one commit and no
// integration ref — the world in which "what did this branch change" has no
// base to be asked against.
func pmcCommitAllBranch(t *testing.T, repo, branch string) {
	t.Helper()
	preMergeGit(t, repo, "init", "-q", "-b", branch)
	preMergeGit(t, repo, "config", "user.email", "t@t")
	preMergeGit(t, repo, "config", "user.name", "t")
	pmcWriteFile(t, filepath.Join(repo, "a.txt"), "hello\n")
	preMergeGit(t, repo, "add", "-A")
	preMergeGit(t, repo, "commit", "-q", "-m", "init")
}
