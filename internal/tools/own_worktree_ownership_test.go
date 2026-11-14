package tools

// own_worktree_ownership_test.go — THE 2026-10-27 DEFECT, reproduced.
//
// MEASURED (verbatim, from row 01a17dcd's close-out on this host):
//
//	task 01a17dcd-c59c-7344-a99a-83133d4416c7: own-worktree-release: own registered worktree kept: /home/user/workspace/cptest-verify — worktree is DIRTY (uncommitted work) — left for a human
//
// The registration was a chat's hand-made verification checkout that no ledger
// row had ever created; the seven "uncommitted" files belonged to a DIFFERENT
// task, whose work had already landed. `01a17dcd`'s own commit was complete. The
// pass had asked every question EXCEPT whose directory it was looking at.
//
// THE THREE PROPERTIES:
//
//	① UNOWNED STAYS NEUTRAL — a dirty registration no row names is still REPORTED
//	  (Rule 15: its dirt is a human's decision) but under the neutral lead, so a
//	  producer seam printing it cannot prefix it with a row id.
//	② OWN STAYS OWN, BY BOTH ROUTES — the recorded `deliverable.worktree`, and the
//	  row's own ground branch on a LINKED worktree.
//	③ THE SHAPE IS THE MEASURED ONE — GroundModelOf's answer decides the branch
//	  test (Rule 6c): a CLONE carrying the same branch name is NOT this row's own
//	  ground, because the clone channel owns its commit, not a worktree row.
//
// The RED proof is in ① and does not depend on a comment: the SAME fixture is
// judged twice — once through the resolver the production pass now asks
// (GroundOwnership) and once through the pre-fix ROW PATH
// (ownGroundLegacyRowPathForTest below, the path test exactly as it stood on
// master) — and the two verdicts must disagree. Reverting the production wiring
// makes the pass agree with the legacy path, which is what turns this file red.

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nick/gopi/internal/config"
	"github.com/nick/gopi/internal/devworkspace"
)

const (
	// ownGroundFixtureRowID is the row that is CLOSING OUT (the subject the
	// pre-fix pass wrongly used) — deliberately NOT the owner of the
	// registration under judgement.
	ownGroundFixtureRowID = "01a17dcd-c59c-7344-a99a-83133d4416c7"
	// ownGroundFixtureRecordedGround is the directory that row's deliverable
	// records: its real ground, a DIFFERENT directory from the registration the
	// unowned test builds (that difference is the defect).
	ownGroundFixtureRecordedGround = "gopi-01a17dcdc59c7344a99a83133d4416c7"
	// ownGroundFixtureUnowned is the registration no row names — the measured
	// `cptest-verify` shape, spelled with this test's own marker so a leftover
	// directory is attributable.
	ownGroundFixtureUnowned = "cptest-verify-unowned"
)

// ownGroundLedgerFixture builds: one committed repo, one registration NO row
// names (dirty), and one row THIS process claims whose deliverable records a
// DIFFERENT directory. Returns repo, the unowned registration's path, the row id.
func ownGroundLedgerFixture(t *testing.T) (repo, unowned, taskID string) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	t.Setenv(memoryDBEnv, filepath.Join(t.TempDir(), "memory.db"))
	settings := filepath.Join(t.TempDir(), "settings.json")
	if err := os.WriteFile(settings, []byte(`{"sync_peers":[]}`), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv(config.SettingsEnv, settings)
	t.Setenv(config.SyncPeersEnv, "")

	root := t.TempDir()
	repo = filepath.Join(root, "repo")
	gitRun(t, root, "init", repo)
	gitRun(t, repo, "config", "user.email", "t@t")
	gitRun(t, repo, "config", "user.name", "t")
	gitRun(t, repo, "checkout", "-b", "master")
	if err := os.WriteFile(filepath.Join(repo, "a.txt"), []byte("base"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitRun(t, repo, "add", "a.txt")
	gitRun(t, repo, "commit", "-m", "init")

	// The row's REAL ground (recorded in the ledger) — created first, so the
	// unowned registration below is provably a different directory.
	recorded := filepath.Join(root, ownGroundFixtureRecordedGround)
	gitRun(t, repo, "worktree", "add", "-b", ownGroundBranchForFixture(ownGroundFixtureRowID),
		recorded, "master")

	// The registration NO row names — the measured shape: created by hand, on a
	// detached HEAD, carrying uncommitted work nobody's row accounts for.
	unowned = filepath.Join(root, ownGroundFixtureUnowned)
	gitRun(t, repo, "worktree", "add", "--detach", unowned, "master")
	if err := os.WriteFile(filepath.Join(unowned, "stray.txt"), []byte("someone else's work"), 0o644); err != nil {
		t.Fatal(err)
	}

	taskID = ownGroundFixtureRowID
	if _, err := TaskSubmit(TaskSubmitOptions{Summary: "closeout subject", Payload: "p", TaskID: taskID, Source: "human"}); err != nil {
		t.Fatalf("submit: %v", err)
	}
	seedSelfHolderRow(t, taskID, repo, ownGroundBranchForFixture(taskID), recorded)
	return repo, unowned, taskID
}

// ownGroundBranchForFixture is the row's ground branch name, taken from the ONE
// producer of that spelling (TaskGroundBranchName) instead of typed here — a
// hand-written "task/..." would let this test pass while the production name
// moved.
func ownGroundBranchForFixture(taskID string) string {
	return TaskGroundBranchName(taskID)
}

// ownGroundLegacyRowPathForTest is the PRE-FIX path test, verbatim in effect:
// on master the walk had no ownership question at all, so "own" meant "it is a
// registration of a repo the ledger names" — i.e. every registration. Keeping
// the legacy predicate here as a NAMED function is what makes the red proof
// mechanical rather than rhetorical: the fixture is judged by both, and the
// legacy answer must be the wrong one.
//
// legacy-row-path-for-test is deliberate: this function exists so the red proof
// can name the pre-fix behaviour after it is gone from production.
func ownGroundLegacyRowPathForTest(rows []ownGroundRow) ownGroundVerdict {
	if len(rows) == 0 {
		return ownGroundVerdict{Evidence: "no rows"}
	}
	return ownGroundVerdict{Own: true, TaskID: rows[0].TaskID,
		Evidence: "pre-fix row path: any registration in a repo the ledger names"}
}

// TestCloseoutDoesNotAttributeAnUnownedDirtyWorktreeToAClosingRow is ①, the
// defect itself.
func TestCloseoutDoesNotAttributeAnUnownedDirtyWorktreeToAClosingRow(t *testing.T) {
	repo, unowned, taskID := ownGroundLedgerFixture(t)
	rows := reclaimSelfOwnVerdictRows()
	if len(rows) == 0 {
		t.Fatalf("the fixture row is not visible to the ownership reader — the test would prove nothing")
	}

	// The registration's branch is whatever `git worktree list` reports (a
	// detached one reports none) — read it the way the pass does.
	regs, err := worktreeSweepRegistrations(repo)
	if err != nil {
		t.Fatalf("registrations: %v", err)
	}
	var reg worktreeRegistration
	for _, r := range regs {
		if sameWorkingPath(r.Path, unowned) {
			reg = r
		}
	}
	if reg.Path == "" {
		t.Fatalf("the unowned registration %s is not in git's list: %+v", unowned, regs)
	}

	verdict := GroundOwnership(reg, rows)
	if verdict.Own {
		t.Fatalf("an unowned registration was claimed as the ground of row %s (%s) — this is the defect",
			verdict.TaskID, verdict.Evidence)
	}

	line := reclaimSelfOwnLine(verdict, reclaimSelfOwnKeptMarker+": "+unowned+" — worktree is DIRTY (uncommitted work)")
	if !strings.HasPrefix(line, ReclaimSelfOwnUnownedLead) {
		t.Fatalf("an unowned line must carry the neutral lead %q, got %q", ReclaimSelfOwnUnownedLead, line)
	}
	if ReclaimSelfOwnIsOwnLine(line) {
		t.Fatalf("the producer seams would prefix this line with a row id: %q", line)
	}
	// Rule 15: the FACT survives. Only the subject changed.
	if !strings.Contains(line, reclaimSelfOwnKeptMarker) || !strings.Contains(line, unowned) {
		t.Fatalf("the observation must be kept verbatim (marker + path), got %q", line)
	}

	// THE RED PROOF, mechanical: the same fixture judged by the PRE-FIX path
	// claims the row. Reverting the production wiring makes the pass agree with
	// this answer, and the assertions above then fail.
	if legacy := ownGroundLegacyRowPathForTest(rows); !legacy.Own || legacy.TaskID != taskID {
		t.Fatalf("the legacy path no longer reproduces the defect (Own=%v id=%q) — the red proof has lost its baseline",
			legacy.Own, legacy.TaskID)
	} else {
		t.Logf("RED baseline (pre-fix path) would report it under row %s; the resolver answers Own=false", legacy.TaskID)
	}

	// And the pass itself must behave: the unowned directory is still reported,
	// and its line is neutral.
	var passLines []string
	for _, l := range reclaimSelfOwnRepo(repo, rows) {
		if strings.Contains(l, unowned) {
			passLines = append(passLines, l)
		}
	}
	if len(passLines) == 0 {
		t.Fatalf("the pass said nothing about an aged-or-not dirty registration — Rule 15 forbids the silence")
	}
	for _, l := range passLines {
		if !strings.HasPrefix(l, ReclaimSelfOwnUnownedLead) {
			t.Fatalf("the pass reported an unowned registration under the own marker: %q", l)
		}
	}
}

// TestOwnGroundIsStillRecognisedByItsDerivedBranch is ② route B: the row's own
// ground branch on a LINKED worktree, with the deliverable recording ANOTHER
// directory (the launch-directory shape reclaim_own_ground.go documents).
func TestOwnGroundIsStillRecognisedByItsDerivedBranch(t *testing.T) {
	repo, _, taskID := ownGroundLedgerFixture(t)
	branch := ownGroundBranchForFixture(taskID)
	regs, err := worktreeSweepRegistrations(repo)
	if err != nil {
		t.Fatalf("registrations: %v", err)
	}
	var ground worktreeRegistration
	for _, r := range regs {
		if strings.TrimSpace(r.Branch) == branch {
			ground = r
		}
	}
	if ground.Path == "" {
		t.Fatalf("the row's own ground (branch %s) is not registered: %+v", branch, regs)
	}
	// The deliverable records a DIFFERENT directory, so only the branch test can
	// claim it.
	rows := []ownGroundRow{{TaskID: taskID, Worktree: "/somewhere/else", Branch: branch}}
	v := GroundOwnership(ground, rows)
	if !v.Own || v.TaskID != taskID {
		t.Fatalf("the row's own ground branch was not recognised as own: %+v", v)
	}
	if !strings.Contains(v.Evidence, string(devworkspace.GroundModelLinkedWorktree)) &&
		!strings.Contains(v.Evidence, "deliverable.worktree") {
		t.Fatalf("the verdict must say which test decided it, got %q", v.Evidence)
	}
	// An own line must SAY it is this close-out's row — the positive half of the
	// subject statement (the neutral lead is the negative half).
	ownLine := reclaimSelfOwnLine(v, "line")
	if !ReclaimSelfOwnIsOwnLine(ownLine) {
		t.Fatalf("an own line must be prefixable with the row id, got %q", ownLine)
	}
	if !strings.Contains(ownLine, "this close-out's row "+taskID) {
		t.Fatalf("an own line must name the row whose ground it is, got %q", ownLine)
	}
	if !strings.HasSuffix(ownLine, v.Evidence+")") {
		t.Fatalf("an own line must carry the evidence that decided it (%q), got %q", v.Evidence, ownLine)
	}

	// The ID-DERIVED spelling alone (no recorded branch) must also claim it: that
	// is the clone channel's row, whose deliverable names no branch.
	derivedOnly := []ownGroundRow{{TaskID: taskID, Worktree: "/somewhere/else"}}
	if v2 := GroundOwnership(ground, derivedOnly); !v2.Own || v2.TaskID != taskID {
		t.Fatalf("the branch DERIVED from the row id did not claim its own ground: %+v", v2)
	}
}

// TestOwnGroundIsStillRecognisedByItsRecordedWorktree is ② route A: the path the
// row's deliverable records, which holds for BOTH ground models.
func TestOwnGroundIsStillRecognisedByItsRecordedWorktree(t *testing.T) {
	repo, _, taskID := ownGroundLedgerFixture(t)
	recorded := filepath.Join(filepath.Dir(repo), ownGroundFixtureRecordedGround)
	regs, err := worktreeSweepRegistrations(repo)
	if err != nil {
		t.Fatalf("registrations: %v", err)
	}
	var reg worktreeRegistration
	for _, r := range regs {
		if sameWorkingPath(r.Path, recorded) {
			reg = r
		}
	}
	if reg.Path == "" {
		t.Fatalf("the recorded ground %s is not registered: %+v", recorded, regs)
	}
	// A row naming ONLY the directory (no branch at all) must still claim it.
	rows := []ownGroundRow{{TaskID: taskID, Worktree: recorded}}
	v := GroundOwnership(reg, rows)
	if !v.Own || v.TaskID != taskID {
		t.Fatalf("the recorded deliverable.worktree did not claim its own directory: %+v", v)
	}
	if !strings.Contains(v.Evidence, "deliverable.worktree") {
		t.Fatalf("the verdict must say the RECORDED DIRECTORY decided it, got %q", v.Evidence)
	}
}

// TestOwnershipBranchAxisIsNotACloneClaim is ③: the branch axis is a fact about
// a LINKED worktree, and the shape is answered by devworkspace.GroundModelOf —
// the ONE discriminator Rule 6c names. A CLONE standing on the row's branch (the
// clone channel's ordinary state) is not claimed by it: the clone's commit is
// owed by that channel, not by a worktree release.
func TestOwnershipBranchAxisIsNotACloneClaim(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	taskID := ownGroundFixtureRowID
	branch := ownGroundBranchForFixture(taskID)
	rows := []ownGroundRow{{TaskID: taskID, Branch: branch}}

	// A real source repo STANDING ON the row's branch, then a clone of it: `git
	// clone` checks out the source's HEAD branch, so the clone really does carry
	// `task/<id>` — the clone channel's ordinary state (see
	// TaskGroundBranchName's doc), which no worktree row owns.
	source := filepath.Join(t.TempDir(), "source")
	gitRun(t, filepath.Dir(source), "init", "-q", "-b", "master", source)
	gitRun(t, source, "config", "user.email", "t@t")
	gitRun(t, source, "config", "user.name", "t")
	if err := os.WriteFile(filepath.Join(source, "s.txt"), []byte("s"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitRun(t, source, "add", "s.txt")
	gitRun(t, source, "commit", "-q", "-m", "seed")
	gitRun(t, source, "checkout", "-q", "-b", branch)
	clone := filepath.Join(t.TempDir(), "clone-ground")
	gitRun(t, filepath.Dir(clone), "clone", "-q", source, clone)
	// A clone is a directory with a .git DIRECTORY — assert the discriminator
	// agrees, so the test cannot pass on a fixture that failed to clone.
	if got := devworkspace.GroundModelOf(clone); got != devworkspace.GroundModelClone {
		t.Fatalf("the fixture is not a clone (model=%s) — the clone pole cannot be staged", got)
	}
	v := GroundOwnership(worktreeRegistration{Path: clone, Branch: branch, Repo: clone}, rows)
	if v.Own {
		t.Fatalf("a CLONE carrying the row's branch was claimed as its own ground: %+v", v)
	}
	if v.Evidence == "" {
		t.Fatalf("a non-own verdict must carry its evidence sentence")
	}

	// A directory that is no ground at all: also not claimed (the branch test
	// declines rather than guessing).
	nowhere := t.TempDir()
	if got := devworkspace.GroundModelOf(nowhere); got != devworkspace.GroundModelMissing {
		t.Fatalf("the empty fixture is not MISSING (model=%s)", got)
	}
	if v := GroundOwnership(worktreeRegistration{Path: nowhere, Branch: branch}, rows); v.Own {
		t.Fatalf("a directory that is no ground at all was claimed: %+v", v)
	}
}

// TestUnownedLeadSurvivesTheRunCapLine keeps the neutral lead on the run-cap
// line too: a cap note produced while walking an unowned registration is about
// that directory, not about the row that happens to be closing out.
func TestUnownedLeadSurvivesTheRunCapLine(t *testing.T) {
	v := ownGroundVerdict{Evidence: ownGroundNoRowPhrase}
	line := reclaimSelfOwnLine(v, "own registered worktree kept: /x — run cap reached (8)")
	if !strings.HasPrefix(line, ReclaimSelfOwnUnownedLead) {
		t.Fatalf("the cap line lost its neutral subject: %q", line)
	}
	own := ownGroundVerdict{Own: true, TaskID: "01a17dcd-c59c-7344-a99a-83133d4416c7",
		Evidence: ownGroundNoRowPhrase}
	got := reclaimSelfOwnLine(own, "x")
	if !strings.HasPrefix(got, "x") {
		t.Fatalf("an own line must keep the observation first, got %q", got)
	}
	if !strings.Contains(got, "this close-out's row 01a17dcd-c59c-7344-a99a-83133d4416c7") {
		t.Fatalf("an own line must name its row, got %q", got)
	}
}

// TestOwnershipIsNotDecidedByAnUnrelatedLiveSessionShape pins the OTHER half of
// the measured defect: the pre-fix pass decided such a directory by asking about
// the ROW (age floor / holder / checklist), so any row closing out at that
// moment "owned" it. Here the row is a live, in-progress row of this process and
// the registration is still nobody's — ownership must not be inferred from the
// row's existence.
func TestOwnershipIsNotDecidedByAnUnrelatedLiveSessionShape(t *testing.T) {
	repo, unowned, taskID := ownGroundLedgerFixture(t)
	rows := reclaimSelfOwnVerdictRows()
	// The row is live (claimed by this process, in progress) — the pre-fix pass's
	// only evidence for calling anything "own".
	for _, r := range rows {
		if r.TaskID != taskID {
			continue
		}
		if v := GroundOwnership(worktreeRegistration{Path: unowned, Branch: "", Repo: repo}, rows); v.Own {
			t.Fatalf("a live row of this process claimed a directory it does not record: %+v", v)
		}
	}
	// Sanity on the fixture's own facts (so a future reader can see the shape).
	if _, err := os.Stat(filepath.Join(unowned, "stray.txt")); err != nil {
		t.Fatalf("the unowned registration lost its dirt: %v", err)
	}
	if !strings.Contains(gitOut(t, repo, finishCleanupWorktreeVerb, finishCleanupWorktreeListVerb), unowned) {
		t.Fatalf("git no longer lists the unowned registration")
	}
}

// TestPassReportsItsOwnGroundAsOwn is the PASS-LEVEL pole of both halves: driven
// through the production walk (reclaimSelfOwnRepo, which reads the subject set
// the same way the public pass does), the row's OWN recorded ground must be
// reported WITHOUT the neutral lead, and the unowned registration WITH it. This
// is the assertion that turns red if the ownership question is dropped from the
// walk — including the subtle form where the subject set comes back empty and
// every ground silently reads as unowned.
func TestPassReportsItsOwnGroundAsOwn(t *testing.T) {
	repo, unowned, taskID := ownGroundLedgerFixture(t)
	recorded := filepath.Join(filepath.Dir(repo), ownGroundFixtureRecordedGround)
	lines := reclaimSelfOwnRepo(repo, nil) // nil = the pass's own read of its subject set
	if len(lines) == 0 {
		t.Fatalf("the pass reported nothing at all")
	}

	var ownSeen, unownedSeen bool
	for _, l := range lines {
		switch {
		case strings.Contains(l, recorded):
			ownSeen = true
			if !ReclaimSelfOwnIsOwnLine(l) {
				t.Errorf("the row's OWN ground was reported as unowned: %q", l)
			}
			// The POSITIVE half: the line must say it is THIS row's ground, and
			// the producer seam prefixes the id — together they are the sentence
			// `task <id>: … — this close-out's row <id> (evidence)`.
			if !strings.Contains(l, taskID) {
				t.Errorf("the row's OWN ground did not name its row (%s): %q", taskID, l)
			}
			if prefixed := taskRunOwnReleaseLineForTest(taskID, l); !strings.Contains(prefixed, "task "+taskID+": ") {
				t.Errorf("an own line must be printable under its row id, got %q", prefixed)
			}
		case strings.Contains(l, unowned):
			unownedSeen = true
			if ReclaimSelfOwnIsOwnLine(l) {
				t.Errorf("an unowned registration was reported as the row's own (the 2026-10-27 defect): %q", l)
			}
			// The NEGATIVE half at the same granularity: no row id ANYWHERE in
			// the line, so no seam can leak one back in.
			if strings.Contains(l, taskID) {
				t.Errorf("an unowned line carries the closing row's id (%s) — the 2026-10-27 defect: %q", taskID, l)
			}
		}
	}
	if !ownSeen {
		t.Fatalf("the pass said nothing about the row's own ground %s — the subject set is empty or unread:\n%s",
			recorded, strings.Join(lines, "\n"))
	}
	if !unownedSeen {
		t.Fatalf("the pass said nothing about the unowned registration %s (Rule 15):\n%s",
			unowned, strings.Join(lines, "\n"))
	}
	t.Logf("row %s: own ground reported as own; %s reported as unowned", taskID, unowned)
}

// taskRunOwnReleaseLineForTest renders the producer seam's ONE line for a pass
// line, WITHOUT importing cmd/agent (which would be an import cycle). It is a
// deliberate mirror of taskRunPrintOwnReleaseLine's two shapes — the seam itself
// lives in cmd/agent and is asserted there
// (cmd/agent/task_run_own_release_subject_test.go); what this mirror buys is
// that the pass-level test can state BOTH granularities on ONE line: the pass's
// own classification, and the sentence an operator actually reads.
// seam-mirror-for-test: the production seam's format is `task %s:
// own-worktree-release: %s`; a change there must change this string too, and
// cmd/agent's own test fails first when it does.
func taskRunOwnReleaseLineForTest(taskID, line string) string {
	if !ReclaimSelfOwnIsOwnLine(line) {
		return "own-worktree-release: " + line
	}
	return "task " + taskID + ": own-worktree-release: " + line
}
