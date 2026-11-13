package tools

// Tests for the branch-attribution classifier (branch_attribution.go).
//
// RED-PROOF NOTE (stated because the deliverable is judged on it): the
// assertions below cannot pass before the change — the classifier does not
// exist on master. What they pin is the DEFECT, reproduced at the arm level in
// devdoc/branch-task-attribution.md §2 against the live ledger row 01a0a996:
// a row still in_progress whose claim pid is a dead local pid reads as UNOWNED
// by the age sweep (worktreeSweepRowHoldsGround requires !HolderProvenDead),
// and a row whose deliverable names only a branch is not even loaded by the
// resume arm's needle (deadHolderResumeRows). The tests below drive the
// classifier on exactly those fixture shapes and assert the answer the merger
// needs; the mutation red-proof for the tier ordering is recorded in the task
// report (inverting tierLedgerEnded/tierProcessGone reddens
// TestBranchAttributionOwnerEndedForLiveRowWithDeadRunner).
//
// Every test asserts BOTH directions where a direction exists: a live holder
// must keep the branch (the false-positive that deletes work), and a
// provably-gone owner must be named as such (the false-negative that parks
// work forever).

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/nick/gopi/internal/scratch"
)

// branchAttribFixture builds a real repo with a feat branch CHECKED OUT in a
// linked worktree — the shape every worker leaves behind — and returns the
// repo, the worktree directory and the branch name.
func branchAttribFixture(t *testing.T, branch string) (repo, worktree string) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	t.Setenv("GOPI_MEMORY_DB", filepath.Join(t.TempDir(), "memory.db"))
	root := t.TempDir()
	repo = filepath.Join(root, "repo")
	worktree = filepath.Join(root, "wt")
	gitRun(t, root, "init", repo)
	gitRun(t, repo, "config", "user.email", "t@t")
	gitRun(t, repo, "config", "user.name", "t")
	gitRun(t, repo, "checkout", "-b", masterBranchName)
	if err := os.WriteFile(filepath.Join(repo, "a.txt"), []byte("base"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitRun(t, repo, "add", "a.txt")
	gitRun(t, repo, "commit", "-m", "init")
	gitRun(t, repo, "checkout", "-b", branch)
	if err := os.WriteFile(filepath.Join(repo, "work.txt"), []byte("deliverable"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitRun(t, repo, "add", "work.txt")
	gitRun(t, repo, "commit", "-m", "work")
	gitRun(t, repo, "checkout", masterBranchName)
	gitRun(t, repo, "worktree", "add", worktree, branch)
	return repo, worktree
}

// branchAttribSeed files a row with the given status/claim stamp and
// deliverable blob, returning its task id.
func branchAttribSeed(t *testing.T, taskID, status, claimedBy, blob string) {
	t.Helper()
	if _, err := TaskSubmit(TaskSubmitOptions{Summary: "s", Payload: "p", TaskID: taskID, Source: "human"}); err != nil {
		t.Fatalf("submit: %v", err)
	}
	ctx := context.Background()
	db := openTestTasksDB(t, memoryDBPath())
	defer db.Close()
	if _, err := db.ExecContext(ctx,
		`UPDATE tasks SET status = ?, claimed_by = ?, deliverable = ? WHERE task_id = ?`,
		status, claimedBy, blob, taskID); err != nil {
		t.Fatalf("stamp %s: %v", taskID, err)
	}
}

// branchAttribRows loads the classifier's evidence set (the same loader
// production uses) so the tests judge the REAL rows, not hand-built structs.
func branchAttribRows(t *testing.T) []tasksRowForSweep {
	t.Helper()
	rows, err := branchAttributionRows(context.Background())
	if err != nil {
		t.Fatalf("load evidence rows: %v", err)
	}
	return rows
}

func attribBlob(repo, branch, worktree string) string {
	blob := `{"repo":"` + repo + `","branch":"` + branch + `"`
	if worktree != "" {
		blob += `,"worktree":"` + worktree + `"`
	}
	return blob + `}`
}

// TestBranchAttributionOwnerEndedForLiveRowWithDeadRunner is the live defect
// (row 01a0a996): status=in_progress, a DEAD claim pid, and a real checkout
// still holding the branch. The age sweep calls that row unowned; the merger
// must be able to see the unfinished, unowned-in-fact run and name its owner.
func TestBranchAttributionOwnerEndedForLiveRowWithDeadRunner(t *testing.T) {
	branch := "feat/attrib-dead-runner"
	repo, wt := branchAttribFixture(t, branch)
	dead := deadPID(t)
	branchAttribSeed(t, "t-attrib-dead-runner", taskStatusInProgress, hostPid(t, dead), attribBlob(repo, branch, wt))

	d := BranchAttribution(repo, branch, branchAttribRows(t), scratch.ScanHolders(), true)
	if d.Verdict != branchAttribOwnerEnded {
		t.Fatalf("verdict = %s, want %s (reason %q, evidence %v)", d.Verdict, branchAttribOwnerEnded, d.Reason, d.Evidence)
	}
	if d.Task != "t-attrib-dead-runner" {
		t.Fatalf("attributed task = %q, want t-attrib-dead-runner (the merger must name WHO owns it)", d.Task)
	}
	if d.Reclaimer != branchArmResume {
		t.Fatalf("reclaimer = %q, want %q — the resume arm is the remedy for an interrupted run", d.Reclaimer, branchArmResume)
	}
	if len(d.Evidence) == 0 || d.Reason == "" {
		t.Fatalf("the verdict carries no evidence: reason=%q evidence=%v", d.Reason, d.Evidence)
	}
}

// TestBranchAttributionLiveHolderKeepsTheBranch is the direction that must
// never regress (memory #2632): a row whose recorded RUNNER is still alive
// owns its branch, whatever its claim stamp says.
func TestBranchAttributionLiveHolderKeepsTheBranch(t *testing.T) {
	branch := "feat/attrib-live-runner"
	repo, wt := branchAttribFixture(t, branch)
	// This test process IS the live runner: record its own identity, the same
	// triple a claim stamps (pid + start ticks), and a CLAIM STAMP of a dead pid
	// — the exact combination that made the sweep call a live run unowned.
	ident, ok := taskCaptureProcIdentity(os.Getpid())
	if !ok {
		t.Fatalf("cannot capture this process's identity")
	}
	dead := deadPID(t)
	branchAttribSeed(t, "t-attrib-live-runner", taskStatusInProgress, hostPid(t, dead), attribBlob(repo, branch, wt))
	ctx := context.Background()
	db := openTestTasksDB(t, memoryDBPath())
	if _, err := db.ExecContext(ctx,
		`UPDATE tasks SET `+tasksProcPidColumnName+` = ?, `+tasksProcStartColumnName+` = ?, `+tasksProcCmdColumnName+` = ? WHERE task_id = ?`,
		ident.Pid, ident.Start, ident.Cmd, "t-attrib-live-runner"); err != nil {
		db.Close() //stylegate:ignore fatal path: the test ends here, the process reclaims the fixture handle
		t.Fatalf("stamp runner identity: %v", err)
	}
	// Closed (not deferred) on purpose: the classifier opens its OWN ledger
	// connection and must read the stamp committed above from disk, not from
	// this fixture's open handle.
	db.Close() //stylegate:ignore fixture handle released before the classifier opens its own ledger connection

	d := BranchAttribution(repo, branch, branchAttribRows(t), scratch.ScanHolders(), true)
	if d.Verdict != branchAttribLiveHolder {
		t.Fatalf("verdict = %s, want %s — a LIVE recorded runner must own its branch (reason %q)", d.Verdict, branchAttribLiveHolder, d.Reason)
	}
	if d.Task != "t-attrib-live-runner" {
		t.Fatalf("attributed task = %q, want t-attrib-live-runner", d.Task)
	}
}

// TestBranchAttributionBranchOnlyRowIsJudged pins the axis the resume arm's
// needle drops: a row whose deliverable names a BRANCH and no worktree is still
// evidence, and the classifier must judge it rather than skip it.
func TestBranchAttributionBranchOnlyRowIsJudged(t *testing.T) {
	branch := "feat/attrib-branch-only"
	repo, _ := branchAttribFixture(t, branch)
	dead := deadPID(t)
	// No worktree key at all — the submit/claim shape before any backfill.
	branchAttribSeed(t, "t-attrib-branch-only", taskStatusInProgress, hostPid(t, dead), attribBlob(repo, branch, ""))

	rows := branchAttribRows(t)
	found := false
	for _, r := range rows {
		if r.TaskID == "t-attrib-branch-only" {
			found = true
		}
	}
	if !found {
		t.Fatalf("the evidence loader skipped a branch-only row — the axis the resume arm's SQL truncates")
	}
	d := BranchAttribution(repo, branch, rows, scratch.ScanHolders(), true)
	if d.Task != "t-attrib-branch-only" {
		t.Fatalf("attributed task = %q, want t-attrib-branch-only (verdict %s, reason %q)", d.Task, d.Verdict, d.Reason)
	}
	if d.Verdict != branchAttribOwnerEnded && d.Verdict != branchAttribOwnerRecordOnly {
		t.Fatalf("verdict = %s, want an owner-* verdict for a row that is provably without a runner", d.Verdict)
	}
}

// TestBranchAttributionRecordOnlyWhenCheckoutIsGone: the row names the branch
// and its directory is gone (the 01a0a996 shape verbatim — the wrapper removed
// the worktree before dying). There is no interrupted checkout to resume, so
// the verdict must route to the registration repair instead — but it must still
// be an OWNER verdict, never "unowned".
func TestBranchAttributionRecordOnlyWhenCheckoutIsGone(t *testing.T) {
	branch := "feat/attrib-gone-dir"
	repo, wt := branchAttribFixture(t, branch)
	dead := deadPID(t)
	branchAttribSeed(t, "t-attrib-gone-dir", taskStatusInProgress, hostPid(t, dead), attribBlob(repo, branch, wt))
	gitRun(t, repo, "worktree", "remove", wt)

	d := BranchAttribution(repo, branch, branchAttribRows(t), scratch.ScanHolders(), true)
	if d.Verdict != branchAttribOwnerRecordOnly {
		t.Fatalf("verdict = %s, want %s (reason %q)", d.Verdict, branchAttribOwnerRecordOnly, d.Reason)
	}
	if d.Task != "t-attrib-gone-dir" {
		t.Fatalf("attributed task = %q, want t-attrib-gone-dir", d.Task)
	}
	if d.Reclaimer != branchArmReaper {
		t.Fatalf("reclaimer = %q, want %q — a registration with no directory is collected by the repair arm", d.Reclaimer, branchArmReaper)
	}
}

// TestBranchAttributionUnownedOnlyWhenTheLedgerSaysSo: the one verdict that
// licenses disposal, and the two ways to miss it must NOT produce it.
func TestBranchAttributionUnownedOnlyWhenTheLedgerSaysSo(t *testing.T) {
	branch := "feat/attrib-unowned"
	repo, _ := branchAttribFixture(t, branch)
	rows := branchAttribRows(t)

	d := BranchAttribution(repo, branch, rows, scratch.ScanHolders(), true)
	if d.Verdict != branchAttribUnowned {
		t.Fatalf("verdict = %s, want %s (reason %q)", d.Verdict, branchAttribUnowned, d.Reason)
	}
	if d.Task != "" {
		t.Fatalf("an unowned branch must name no task, got %q", d.Task)
	}

	// An UNREADABLE ledger is not evidence of absence: the same branch must
	// come back ungoverned, never unowned (Rule 15's conservative direction).
	d = BranchAttribution(repo, branch, nil, scratch.HolderScan{}, false)
	if d.Verdict != branchAttribUngoverned {
		t.Fatalf("verdict with an unreadable ledger = %s, want %s", d.Verdict, branchAttribUngoverned)
	}
	if d.Reason == "" {
		t.Fatalf("an ungoverned verdict must say why")
	}
}

// TestBranchAttributionIntegrationNamespaces: master/main, hp-unique-* and an
// archive-tagged branch are not anyone's deliverable.
func TestBranchAttributionIntegrationNamespaces(t *testing.T) {
	branch := "feat/attrib-tagged"
	repo, _ := branchAttribFixture(t, branch)
	gitRun(t, repo, "tag", archiveBranchPrefix+branch)

	for _, b := range []string{masterBranchName, "main", "hp-unique-x", branch} {
		d := BranchAttribution(repo, b, nil, scratch.HolderScan{}, true)
		if d.Verdict != branchAttribIntegration {
			t.Fatalf("%s: verdict = %s, want %s (reason %q)", b, d.Verdict, branchAttribIntegration, d.Reason)
		}
	}
}

// TestBranchAttributionReportOverTheRealRepo drives the public report on a real
// repo: the branch is attributed to its row and NOTHING is removed — the
// classifier reports, the arms act (memory #2324: never derive a promise from a
// verdict).
func TestBranchAttributionReportOverTheRealRepo(t *testing.T) {
	branch := "feat/attrib-report"
	repo, wt := branchAttribFixture(t, branch)
	dead := deadPID(t)
	branchAttribSeed(t, "t-attrib-report", taskStatusInProgress, hostPid(t, dead), attribBlob(repo, branch, wt))

	decisions, err := BranchAttributionReport(repo)
	if err != nil {
		t.Fatalf("report: %v", err)
	}
	var got *branchAttributionDecision
	for i := range decisions {
		if decisions[i].Branch == branch {
			got = &decisions[i]
		}
	}
	if got == nil {
		t.Fatalf("the report does not mention %s at all: %v", branch, decisions)
	}
	if got.Verdict != branchAttribOwnerEnded || got.Task != "t-attrib-report" {
		t.Fatalf("report verdict for %s = %s / %q, want %s / t-attrib-report", branch, got.Verdict, got.Task, branchAttribOwnerEnded)
	}
	// Report-only: the checkout and the branch are untouched.
	if _, err := os.Stat(wt); err != nil {
		t.Fatalf("the classifier removed the worktree directory: %v", err)
	}
	if _, err := repoSweepGit(repo, "rev-parse", "--verify", "refs/heads/"+branch); err != nil {
		t.Fatalf("the classifier removed the branch: %v", err)
	}
}

// TestBranchAttributionTerminalRowWithLiveWorkerPidIsNotALiveHolder is the
// red-proof of a MEASURED false positive found while building this classifier
// on the live ledger (2026-09-16): four TERMINAL rows (completed / failed)
// carried a claim stamp naming a pid that is still alive — the long-lived
// `gopi worker` process that wrote the stamp and outlived the row (memory
// #2161). Ranking a live claim pid above the ledger's own verdict made
// finished rows report as "somebody is working here" on `gopi merger --status`
// (observed: fix/standards-reds and three siblings listed as live-holder).
//
// The assertion below FAILS on the pre-fix ordering and passes on the fixed one
// (the ledger's terminal declaration outranks pid evidence for a row that is not
// in a live holder status).
func TestBranchAttributionTerminalRowWithLiveWorkerPidIsNotALiveHolder(t *testing.T) {
	branch := "feat/attrib-done-live-pid"
	repo, wt := branchAttribFixture(t, branch)
	// This test process stands in for the long-lived worker: its pid is alive.
	branchAttribSeed(t, "t-attrib-done-live-pid", taskStatusCompleted, hostPid(t, os.Getpid()), attribBlob(repo, branch, wt))

	d := BranchAttribution(repo, branch, branchAttribRows(t), scratch.ScanHolders(), true)
	if d.Verdict == branchAttribLiveHolder {
		t.Fatalf("a COMPLETED row with a live claim pid read as %s — the ledger's own verdict must outrank a pid that outlived the row (reason %q)", d.Verdict, d.Reason)
	}
	if d.Verdict != branchAttribOwnerEnded {
		t.Fatalf("verdict = %s, want %s (reason %q)", d.Verdict, branchAttribOwnerEnded, d.Reason)
	}
	if d.Reclaimer != branchArmReclaim {
		t.Fatalf("reclaimer = %q, want %q — a finished run's checkout is released, not resumed (the resume arm refuses terminal rows)", d.Reclaimer, branchArmReclaim)
	}
	if !d.Checkout {
		t.Fatalf("the decision must record that a checkout holds the branch (the merger is blocked by it)")
	}
}

// TestBranchAttributionUnownedRowWithLiveCheckoutIsNotUnowned is the
// conservative direction of the same axis: a live-status row whose runner is
// unprovable and whose recorded directory is absent, while a process stands in
// the branch's checkout. The classifier must NOT call the branch unowned — no
// arm may be told to recycle ground a live process is standing on.
func TestBranchAttributionUnownedRowWithLiveCheckoutIsNotUnowned(t *testing.T) {
	branch := "feat/attrib-held-checkout"
	repo, wt := branchAttribFixture(t, branch)
	branchAttribSeed(t, "t-attrib-held-checkout", taskStatusInProgress, "", attribBlob(repo, branch, ""))
	holdDir(t, wt)

	d := BranchAttribution(repo, branch, branchAttribRows(t), scratch.ScanHolders(), true)
	if d.Verdict == branchAttribUnowned {
		t.Fatalf("a branch whose checkout a live process holds read as UNOWNED — that verdict licenses recycling (reason %q)", d.Reason)
	}
	if d.Verdict != branchAttribLiveHolder {
		t.Fatalf("verdict = %s, want %s (reason %q)", d.Verdict, branchAttribLiveHolder, d.Reason)
	}
}
