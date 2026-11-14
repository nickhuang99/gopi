package tools

// THE SECOND-GROUND AXIS (epic 01a0dff1) — the producer-side cause of "a live
// ground with no owner".
//
// THE MEASURED DEFECT. Row 01a0ddc4-c410 (session 3592a5ef) ran, mid-flight:
//
//	git worktree add /home/user/workspace/gopi-evi-pushback -b fix/pushback-rule31-evidence
//
// while its own deliverable already named .../gopi-01a0ddc4… on
// task/01a0ddc4…. Three separate mechanisms then conspired to record NOTHING
// about the directory it created:
//
//	the row       — the primary worktree/branch are single-valued and the
//	                writer refuses to overwrite them (correctly: a detection
//	                seam must never REBIND a row's own ground);
//	the seam      — the creation detection's candidate loop RETURNED at its
//	                first resolvable directory, and candidate #1 IS the run's
//	                own ground by construction, so the created directory was
//	                never probed;
//	the anchor    — TaskGroundAnchorRecord is called from that seam, which had
//	                already returned (the sink held 154 anchors and none for
//	                gopi-evi-pushback).
//
// So a registered linked worktree on a live branch had no owner anywhere:
// the then-live branch→row lookup answered "no owner" on a branch this host
// holds no row for, and
// `gopi worker --resume --branch B` had no row. The tests below pin the repair
// on the two halves that produce the fact:
//
//	(1) the SEAM records a ground the run CREATED even when the row is already
//	    bound to its own ground — into the plural axis, never rebinding the
//	    row's own;
//	(2) the LOOKUPS (branch + worktree) answer with that row, naming the
//	    provenance.
//
// RED ON THE PRE-FIX CODE BY CONSTRUCTION: the blob has no plural axis at all
// before this change, so after a run creates its second ground the row's blob
// carries no "grounds" key and the lookup has no owner — exactly the incident.

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nickhuang99/gopi/internal/config"
	"github.com/nickhuang99/gopi/internal/removalaudit"
	"github.com/nickhuang99/gopi/internal/toolnames"
)

// groundsFixture is the incident's shape in miniature: a real repo whose MAIN
// checkout is on master, a LINKED worktree on the row's own branch (the ground
// the runner pre-created and the row already records), and a SECOND worktree the
// run creates mid-flight (the evidence ground).
type groundsFixture struct {
	t          *testing.T
	root       string
	repo       string
	ownWT      string
	ownBranch  string
	createdWT  string
	createdBr  string
	rowID      string
	scriptPath string
}

// groundsRowID is a PER-TEST row id. The suite pins ONE ledger for the whole
// run (testutil.PinSuiteForTestMain), so a fixed id would collide with the same
// fixture in the next test — and a collision here would hide the defect behind
// a fixture error instead of failing on the behaviour.
func groundsRowID(t *testing.T) string {
	t.Helper()
	return "grounds-" + groundsSlug(t)
}

// groundsSlug is the sanitised per-test name stem (a git ref and a path token
// cannot carry the characters a test name may).
func groundsSlug(t *testing.T) string {
	t.Helper()
	return strings.Map(func(r rune) rune {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			return r
		case r >= 'A' && r <= 'Z':
			return r + ('a' - 'A')
		}
		return '-'
	}, t.Name())
}

// groundsSeedRule31Script writes the command a run's own bash round would carry
// when it mints a second worktree — the incident's command, verbatim in shape.
// It is a FILE, not a literal here, so the case reads like the run it reproduces.
func groundsSeedRule31Script(t *testing.T, dir string) string {
	t.Helper()
	path := filepath.Join(dir, "rule31-creation.sh")
	body := strings.Join([]string{
		"#!/bin/bash",
		"set -e",
		"# The run's own ground is already recorded on its row; this round mints a",
		"# SECOND ground for its evidence (row 01a0ddc4-c410, session 3592a5ef).",
	}, "\n") + "\n"
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("write the creation script: %v", err)
	}
	return path
}

// newGroundsFixture builds the world: repo on master, the row's own linked
// worktree on its own branch, and the row seeded with the PRIMARY axes already
// stamped (the state the incident's row was in when the second ground appeared).
func newGroundsFixture(t *testing.T) *groundsFixture {
	t.Helper()
	root := t.TempDir()
	// The suite pins ONE ledger for the whole run, so the BRANCH names must be
	// per-test too: a shared name would let one test's row answer the next
	// test's lookup (measured while writing this file: the earlier fixture's row
	// won the ordering and the assertion read as a lookup bug).
	slug := groundsSlug(t)
	f := &groundsFixture{
		t:         t,
		root:      root,
		repo:      filepath.Join(root, "repo"),
		ownWT:     filepath.Join(root, "workspace", "gopi-own-ground"),
		ownBranch: "task/" + slug,
		createdWT: filepath.Join(root, "workspace", "gopi-evi-pushback"),
		createdBr: "fix/" + slug,
		rowID:     groundsRowID(t),
	}
	deliverableWTGit(t, root, "init", f.repo)
	deliverableWTGit(t, f.repo, "config", "user.email", "t@t")
	deliverableWTGit(t, f.repo, "config", "user.name", "t")
	deliverableWTGit(t, f.repo, "checkout", "-b", masterBranchName)
	if err := os.WriteFile(filepath.Join(f.repo, "base.txt"), []byte("base\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	deliverableWTGit(t, f.repo, "add", "base.txt")
	deliverableWTGit(t, f.repo, "commit", "-m", "base")
	// The row's OWN ground: a registered linked worktree on its own branch.
	deliverableWTGit(t, f.repo, "worktree", "add", "-b", f.ownBranch, f.ownWT, masterBranchName)
	f.scriptPath = groundsSeedRule31Script(t, root)
	return f
}

// groundsSeedBoundRow seeds the row with BOTH primary axes already stamped —
// the row's own worktree and branch — which is precisely why the created ground
// had nowhere to go.
func (f *groundsFixture) groundsSeedBoundRow() {
	f.t.Helper()
	deliverableWTSeedWrapperRow(f.t, os.Getenv(memoryDBEnv), f.rowID, f.repo)
	blob := taskDeliverableRebuild(taskDeliverable{Repo: f.repo, Branch: f.ownBranch, Worktree: f.ownWT})
	groundsSetRowBlob(f.t, f.rowID, blob)
}

// groundsSetRowBlob overwrites the row's raw deliverable (the fixture arranging
// the state the run is already in).
func groundsSetRowBlob(t *testing.T, taskID, blob string) {
	t.Helper()
	ctx := t.Context()
	db, err := openTasksDB(ctx, memoryDBPath())
	if err != nil {
		t.Fatalf("open ledger: %v", err)
	}
	defer db.Close()
	if _, err := db.ExecContext(ctx, `UPDATE tasks SET deliverable = ? WHERE task_id = ?`, blob, taskID); err != nil {
		t.Fatalf("seed deliverable for %s: %v", taskID, err)
	}
}

// groundsCreatedCommand is the ONE spelling of the incident's creation command:
// the run minting a SECOND worktree on its own branch, from the repo's ground.
func (f *groundsFixture) groundsCreatedCommand() string {
	return fmt.Sprintf("git worktree add %s -b %s %s", f.createdWT, f.createdBr, masterBranchName)
}

// THE PRODUCER HALF — case (1).
func TestSecondGroundSeamRecordsARunCreatedGroundWhenTheRowIsAlreadyBound(t *testing.T) {
	f := newGroundsFixture(t)
	f.groundsSeedBoundRow()

	// The repo's registered worktrees are git-truth: the created ground does not
	// exist until the run's round creates it.
	deliverableWTGit(t, f.repo, "worktree", "add", "-b", f.createdBr, f.createdWT, masterBranchName)

	// The run's own round, in the incident's shape: the command creates the
	// second ground while the run stands on its OWN ground.
	TaskDeliverableWorktreeBashForRow(f.rowID, f.ownWT, f.groundsCreatedCommand())

	blob := deliverableWTRowBlob(t, os.Getenv(memoryDBEnv), f.rowID)
	d := taskDeliverableParse(blob)
	// (1a) the row's OWN axes are untouched — a created ground never rebinds
	// the primary (the 2026-09-12 foreign-owner hijack stays closed).
	if d.Worktree != f.ownWT || d.Branch != f.ownBranch {
		t.Fatalf("the row's primary ground was REBOUND by a created-ground stamp: worktree=%q branch=%q, want %q / %q",
			d.Worktree, d.Branch, f.ownWT, f.ownBranch)
	}
	// (1b) the created ground is recorded — the whole defect.
	if !taskDeliverableGroundListed(d, f.createdWT, f.createdBr) {
		t.Fatalf("the run-created ground %s (%s) was NOT recorded on the row: blob=%s"+
			"\n  this is the incident: the row is already bound, so the created directory must land on the SECONDARY axis",
			f.createdWT, f.createdBr, blob)
	}
}

// THE CONSUMER HALF — cases (2) branch and (3) worktree.
func TestForBranchFindsTheRowThatOnlyCreatedTheBranch(t *testing.T) {
	f := newGroundsFixture(t)
	f.groundsSeedBoundRow()
	deliverableWTGit(t, f.repo, "worktree", "add", "-b", f.createdBr, f.createdWT, masterBranchName)
	TaskDeliverableWorktreeBashForRow(f.rowID, f.ownWT, f.groundsCreatedCommand())

	lookup, err := TaskForBranch(TaskForBranchQuery{Branch: f.createdBr}, os.Getenv(memoryDBEnv))
	if err != nil {
		t.Fatalf("TaskForBranch(%s): %v", f.createdBr, err)
	}
	if len(lookup.Owners) == 0 {
		t.Fatalf("no owner for %s — the live ground the run created is owner-less (the incident)\n  blob=%s",
			f.createdBr, deliverableWTRowBlob(t, os.Getenv(memoryDBEnv), f.rowID))
	}
	owner := lookup.Owners[0]
	if owner.TaskID != f.rowID {
		t.Fatalf("owner = %s, want the row that created the branch (%s)", owner.TaskID, f.rowID)
	}
	// The provenance is what makes the answer re-derivable: the row's own axes
	// name a DIFFERENT branch, so a reader checking `task show` must be told why.
	if !strings.Contains(owner.Provenance, f.createdBr) {
		t.Fatalf("the owner answer does not name the created branch as its provenance: %q", owner.Provenance)
	}
	if !strings.Contains(owner.Provenance, f.ownBranch) {
		t.Fatalf("the provenance does not name the row's OWN branch (the answer must be checkable): %q", owner.Provenance)
	}
}

func TestForWorktreeFindsTheRowThatOnlyCreatedTheWorktree(t *testing.T) {
	f := newGroundsFixture(t)
	f.groundsSeedBoundRow()
	deliverableWTGit(t, f.repo, "worktree", "add", "-b", f.createdBr, f.createdWT, masterBranchName)
	TaskDeliverableWorktreeBashForRow(f.rowID, f.ownWT, f.groundsCreatedCommand())

	lookup, err := TaskForWorktree(TaskForWorktreeQuery{Worktree: f.createdWT}, os.Getenv(memoryDBEnv))
	if err != nil {
		t.Fatalf("TaskForWorktree(%s): %v", f.createdWT, err)
	}
	if len(lookup.Owners) == 0 || lookup.Owners[0].TaskID != f.rowID {
		t.Fatalf("owners = %+v, want the row that created %s", lookup.Owners, f.createdWT)
	}
}

// The ANCHOR half (Rule 15b / the deterministic-lifecycle principle): the ground
// the run created is anchored at the moment it is observed, so the record
// survives the terminal transition and no reclaim arm has to guess its owner.
func TestSecondGroundIsAnchoredWhenObserved(t *testing.T) {
	sink := anchorSink(t)
	f := newGroundsFixture(t)
	f.groundsSeedBoundRow()
	deliverableWTGit(t, f.repo, "worktree", "add", "-b", f.createdBr, f.createdWT, masterBranchName)
	TaskDeliverableWorktreeBashForRow(f.rowID, f.ownWT, f.groundsCreatedCommand())

	found := false
	for _, a := range anchorsIn(t, sink) {
		if sameWorkingPath(a.Worktree, f.createdWT) {
			found = true
			if a.Branch != f.createdBr {
				t.Fatalf("anchor branch = %q, want %q", a.Branch, f.createdBr)
			}
			if !strings.Contains(a.TaskID, groundsRowID(t)) {
				t.Fatalf("anchor task = %q, want the creating row", a.TaskID)
			}
		}
	}
	if !found {
		t.Fatalf("no run-ground anchor for the created ground %s — the acquisition was not recorded (sink %s)",
			f.createdWT, sink)
	}
}

// The IDENTITY case: a row that has NOT recorded its own ground yet still gets
// its created ground on the primary axes... no — the plural axis, and the
// primary stays empty. One entry per ground, and re-running the round does not
// duplicate it.
func TestSecondGroundAxisIsIdempotentAndNeverDuplicatesThePrimary(t *testing.T) {
	f := newGroundsFixture(t)
	f.groundsSeedBoundRow()
	deliverableWTGit(t, f.repo, "worktree", "add", "-b", f.createdBr, f.createdWT, masterBranchName)
	for i := 0; i < 3; i++ { // the seam fires once per round
		TaskDeliverableWorktreeBashForRow(f.rowID, f.ownWT, f.groundsCreatedCommand())
	}
	blob := deliverableWTRowBlob(t, os.Getenv(memoryDBEnv), f.rowID)
	if n := strings.Count(blob, f.createdBr); n != 1 {
		t.Fatalf("the created ground appears %d times in the blob (want exactly 1): %s", n, blob)
	}
	// A round that "creates" the row's OWN ground is not a second ground.
	TaskDeliverableWorktreeBashForRow(f.rowID, f.ownWT,
		fmt.Sprintf("git worktree add %s -b %s %s", f.ownWT, f.ownBranch, masterBranchName))
	blob = deliverableWTRowBlob(t, os.Getenv(memoryDBEnv), f.rowID)
	if d := taskDeliverableParse(blob); len(d.Grounds) != 1 {
		t.Fatalf("the row's own ground was recorded as a SECOND ground: grounds=%+v blob=%s", d.Grounds, blob)
	}
}

// The writer's refusals, in one place: a repo-less row has nothing to pair
// against, and a row whose blob cannot be parsed is left alone — neither is an
// error a detection seam may escalate (it must never fail a run).
func TestSecondGroundWriterRefusesWithNothingToPairAgainst(t *testing.T) {
	f := newGroundsFixture(t)
	deliverableWTSeedWrapperRow(t, os.Getenv(memoryDBEnv), f.rowID, f.repo)
	groundsSetRowBlob(t, f.rowID, "not json at all")
	if err := taskBackfillDeliverableGrounds(f.rowID, f.createdWT, f.createdBr); err != nil {
		t.Fatalf("a non-JSON blob must be left alone, not error: %v", err)
	}
	if blob := deliverableWTRowBlob(t, os.Getenv(memoryDBEnv), f.rowID); blob != "not json at all" {
		t.Fatalf("a blob the parser cannot read was REWRITTEN: %q", blob)
	}
}

// taskGroundRecordedForCreatedDir is the resolver the seam's created group uses:
// it must answer for a directory that IS a ground of the row's repo and refuse
// for one that is not (a foreign directory proves nothing about this row).
func TestCreatedDirResolvesOnlyForTheRowsOwnRepo(t *testing.T) {
	f := newGroundsFixture(t)
	f.groundsSeedBoundRow()
	deliverableWTGit(t, f.repo, "worktree", "add", "-b", f.createdBr, f.createdWT, masterBranchName)
	if got, ok := taskGroundRecordedForCreatedDir(f.rowID, f.createdWT); !ok || got.Branch != f.createdBr {
		t.Fatalf("created dir did not resolve: %+v ok=%v", got, ok)
	}
	foreign := filepath.Join(t.TempDir(), "somebody-elses-dir")
	if err := os.MkdirAll(foreign, 0o755); err != nil {
		t.Fatal(err)
	}
	if _, ok := taskGroundRecordedForCreatedDir(f.rowID, foreign); ok {
		t.Fatalf("a directory outside the row's repo resolved as its ground: %s", foreign)
	}
}

// The JSON contract of the axis: one object per ground with repo/branch/worktree,
// carried through the ONE rebuild (and preserved across a PRIMARY stamp, the way
// the attachments manifest is).
func TestSecondGroundAxisSurvivesAPrimaryStamp(t *testing.T) {
	f := newGroundsFixture(t)
	f.groundsSeedBoundRow()
	deliverableWTGit(t, f.repo, "worktree", "add", "-b", f.createdBr, f.createdWT, masterBranchName)
	TaskDeliverableWorktreeBashForRow(f.rowID, f.ownWT, f.groundsCreatedCommand())

	// A LATER primary stamp (another row's claim/creation path would do this):
	// the plural axis must survive it byte for byte.
	before := taskDeliverableParse(deliverableWTRowBlob(t, os.Getenv(memoryDBEnv), f.rowID))
	if len(before.Grounds) != 1 {
		t.Fatalf("precondition: the created ground is not recorded yet: %+v", before)
	}
	if err := taskBackfillDeliverableWorktree(f.rowID, "", f.ownBranch); err != nil {
		t.Fatalf("primary stamp: %v", err)
	}
	after := taskDeliverableParse(deliverableWTRowBlob(t, os.Getenv(memoryDBEnv), f.rowID))
	if len(after.Grounds) != 1 || after.Grounds[0].Worktree != before.Grounds[0].Worktree {
		t.Fatalf("a primary stamp DROPPED the second-ground axis: before=%+v after=%+v", before.Grounds, after.Grounds)
	}
}

// The sink the anchor test reads must be the test's own, never the operator's —
// spelled here once so a copy in the anchor test cannot drift (Rule 14).
var _ = removalaudit.EnvSink

// The session env helper this file's fixture does NOT need is deliberately not
// named here: the seam's row argument comes from the run's own face
// (autoTaskRowID), which is what the production call site passes.

// The tool name the incident's round used — asserted so a future rename of the
// bash tool cannot silently make this file's evidence read as something else.
var _ = toolnames.ToolBash

// The profile pin guard: this file's fixtures read the row's repo from the
// deliverable (never from an ambient profile), so they must not depend on
// GOPI_HOME; the assertion keeps that property visible.
var _ = config.GopiDirEnv
