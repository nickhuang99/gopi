package tools

// Dispatch-time staleness guard tests (epic merge-verify-rework-staleness,
// 2026-09-14): the measured case is reproduced here as a REGRESSION PIN — row
// 01a09e9b ("merge verify rework: fix/tasklist-inprogress-scan (was
// 01a09d2d)") named a branch ref git cannot resolve (the real ref was
// `fix/task-list-inprogress-scan`) while the fix had ALREADY been re-landed on
// master as fc9b06d4 "adoption: merge fix/task-list-inprogress-scan
// (0618a2cd)" — and nothing at claim time noticed.
//
// These tests drive the REAL dispatch seam (TaskNextFiltered — the single CAS
// claim funnel behind the worker round, serve dispatch and `task run next`)
// against a real temp repo and a real temp ledger, and assert BOTH directions:
// a stale premise is closed as a no-op with the evidence recorded (no worker
// spawned), and a genuinely unlanded rework is still dispatched (the guard must
// never block real work).

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// guardSeedMergedBranch creates branch, commits one file on it, merges it into
// master under the lane's own landing message and (when sweep is true) deletes
// the ref — exactly what the adoption lane does: `adoption: merge <branch>
// (<sha>)` then the merged-cleanup `git branch -d`.
func guardSeedMergedBranch(t *testing.T, repo, branch, file string, sweep bool) string {
	t.Helper()
	vgCommitFile(t, repo, branch, file, "the fix\n", "fix: "+branch)
	tip := vgGit(t, repo, "rev-parse", branch)
	vgGit(t, repo, "merge", "--no-ff", "-q", "-m", mergeVerifyAdoptionSubject(branch, tip), branch)
	if sweep {
		vgGit(t, repo, "branch", "-d", branch)
	}
	return tip
}

// guardSeedMergerLandedBranch is the ORDINARY merger's landing: `git merge
// <branch>` with git's own subject (mergeVerifyGitMergeSubject), then the
// merged-cleanup `git branch -d` — the shape that leaves master carrying a
// landing commit and NO `adoption: merge` subject anywhere (defect D8).
func guardSeedMergerLandedBranch(t *testing.T, repo, branch, file string, sweep bool) string {
	t.Helper()
	vgCommitFile(t, repo, branch, file, "the fix\n", "fix: "+branch)
	tip := vgGit(t, repo, "rev-parse", branch)
	vgGit(t, repo, "merge", "--no-ff", "-q", "-m", mergeVerifyGitMergeSubject(branch), branch)
	if sweep {
		vgGit(t, repo, "branch", "-d", branch)
	}
	return tip
}

// guardSubmitReworkRow submits the lane row the gate would have filed for a
// branch (tags merge-verify+branch-rework, deliverable {"repo","branch"}).
func guardSubmitReworkRow(t *testing.T, repo, branch, payload string) string {
	t.Helper()
	summary := mergeVerifyReworkSummaryPrefix + branch
	_, err := TaskSubmit(TaskSubmitOptions{
		Summary: summary,
		Payload: payload,
		Tags:    MergeVerifyEvent + "," + mergeVerifyReworkTag,
		Source:  "auto",
		Repo:    repo,
	})
	if err != nil {
		t.Fatalf("submit rework row: %v", err)
	}
	id := MergeVerifyRowIDForTest(summary)
	if id == "" {
		t.Fatalf("rework row %q not found after submit", summary)
	}
	if err := MergeVerifySetDeliverableBranchForTest(id, repo, branch); err != nil {
		t.Fatalf("stamp deliverable: %v", err)
	}
	return id
}

// guardReworkPayload is the forward-rework briefing the gate files (the text
// whose premise the guard judges).
func guardReworkPayload(repo, branch string) string {
	return "You are the rework agent for a REVERTED merge in repo " + repo + ".\n" +
		"TASK: 1. Check out branch " + branch + " and reproduce the failure."
}

// guardRowRefusalText reads the row's CLOSE EVENT detail — the surface that
// carries the guard's full refusal/no-op text (the `reason` column deliberately
// stays compact: marker + verdict + evidence, so `task list` stays scannable).
//
// Two traps this helper exists to avoid, both measured here:
//
//	· `task show` TRUNCATES the text it renders, so an ABSENCE assertion made
//	  against it passes VACUOUSLY (the clause sits past the cut and the check
//	  can never see it — green under a mutation that restores the retired
//	  sentence);
//	· the `reason` column is not the text either. Only the event detail is.
//
// Rule 10.4 — a check that cannot fail is not a check.
func guardRowRefusalText(t *testing.T, taskID string) string {
	t.Helper()
	ctx := context.Background()
	db, err := openTasksDB(ctx, memoryDBPath())
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var detail string
	if err := db.QueryRowContext(ctx,
		`SELECT COALESCE(detail,'') FROM task_events WHERE task_id = ? AND event = ?`,
		taskID, taskStatusCancelled).Scan(&detail); err != nil {
		t.Fatalf("close event detail of %s: %v", taskID, err)
	}
	return detail
}

// TestStaleReworkGuardTypoBranchAlreadyReLandedIsNoOp is THE measured case: the
// row's branch is a typo of the real ref, the real ref was swept after its
// re-landing, and the landing commit is on master. The claim must NOT hand the
// row to a worker: it is closed as a no-op pointing at the landing commit.
func TestStaleReworkGuardTypoBranchAlreadyReLandedIsNoOp(t *testing.T) {
	repo := vgRepo(t)
	vgLedger(t)
	const realBranch = "fix/task-list-inprogress-scan"
	const typoBranch = "fix/tasklist-inprogress-scan" // row 01a09e9b's spelling
	landed := guardSeedMergedBranch(t, repo, realBranch, "fix.txt", true)
	if realBranch == typoBranch {
		t.Fatal("fixture error: the typo must differ from the real ref")
	}

	row := guardSubmitReworkRow(t, repo, typoBranch, guardReworkPayload(repo, typoBranch))

	// ① the guard's own judgement, read-only.
	v, err := MergeVerifyStaleReworkJudge(row)
	if err != nil {
		t.Fatalf("judge: %v", err)
	}
	if !v.Stale || v.Verdict != MergeVerifyStaleVerdictLanded {
		t.Fatalf("verdict = %q stale=%v, want %q stale=true (evidence: %s)",
			v.Verdict, v.Stale, MergeVerifyStaleVerdictLanded, v.Evidence)
	}
	if !strings.Contains(v.Evidence, realBranch) {
		t.Fatalf("evidence must carry the measured landing commit's subject (the real ref %s): %s",
			realBranch, v.Evidence)
	}
	if !strings.Contains(v.LandedAt, landed) {
		t.Fatalf("evidence must name the landing sha %s: got %q", landed, v.LandedAt)
	}

	// ② the dispatch seam: the row is NOT claimable, and it is CLOSED.
	res, err := TaskNextFiltered("regression-pin", "", "")
	if err != nil {
		t.Fatalf("TaskNextFiltered: %v", err)
	}
	if res != nil && !res.Empty {
		t.Fatalf("the stale rework row was CLAIMED (%s) — no worker may be spawned for an already-landed fix",
			res.Task.TaskID)
	}
	if got := vgRowStatus(t, row); got != TaskStatusCompleted {
		t.Fatalf("stale row status = %q, want %q (closed as a LANDED row — the fix is on master)", got, TaskStatusCompleted)
	}
	show := vgShow(t, row)
	if !strings.Contains(show, MergeVerifyStaleNoOpMarker) {
		t.Fatalf("the closed row must carry the guard marker %q; show:\n%s", MergeVerifyStaleNoOpMarker, show)
	}
	if !strings.Contains(show, landed) {
		t.Fatalf("the closed row must POINT AT the landing commit %s; show:\n%s", landed, show)
	}
	// The briefing itself is untouched (append-only: the refusal is recorded,
	// the frozen text is not rewritten).
	if _, payload, err := TaskBriefing(row); err != nil || !strings.Contains(payload, typoBranch) {
		t.Fatalf("the row's payload must be preserved: err=%v payload=%q", err, payload)
	}
}

// TestStaleReworkGuardMergerLandedBranchAbsentRefIsLandedNotRefused is defect
// D8's arm (a), and it is the case the pre-fix guard got WRONG: the row's
// branch was landed by the ORDINARY merger (`git merge <branch>` → git's own
// "Merge branch '<b>'" subject), the merged-cleanup then deleted the ref, and
// master carries NO `adoption: merge` subject at all. The pre-fix guard looked
// only for the adoption form, found nothing, and CANCELLED the row as
// "branch-not-found" — recording a success as a failure. Rows 01a0a59a
// (feat/web-lan-access) and 01a0a9e0 (feat/merger-status-log) are the measured
// instances: both are demonstrably on master
// (`51edd8d9 Merge branch 'feat/web-lan-access'`,
// `15060a5c resolve merge feat/merger-status-log into master`).
//
// Post-fix: the landing is PROVED from master's own history, and the row is
// closed COMPLETED with the landed disposition — never cancelled.
func TestStaleReworkGuardMergerLandedBranchAbsentRefIsLandedNotRefused(t *testing.T) {
	repo := vgRepo(t)
	vgLedger(t)
	const branch = "feat/merger-landed-then-swept"
	guardSeedMergerLandedBranch(t, repo, branch, "merged.txt", true)

	row := guardSubmitReworkRow(t, repo, branch, guardReworkPayload(repo, branch))

	// ① the judgement: LANDED, proved by the merge subject on master.
	v, err := MergeVerifyStaleReworkJudge(row)
	if err != nil {
		t.Fatalf("judge: %v", err)
	}
	if v.Verdict != MergeVerifyStaleVerdictLanded {
		t.Fatalf("verdict = %q, want %q (evidence: %s)", v.Verdict, MergeVerifyStaleVerdictLanded, v.Evidence)
	}
	if !v.Stale {
		t.Fatalf("a merged-and-swept branch must be judged stale-closable; evidence: %s", v.Evidence)
	}
	if v.Landed == "" || !strings.Contains(v.LandedAt, branch) {
		t.Fatalf("the verdict must name the landing commit and its subject; got sha=%q subject=%q", v.Landed, v.LandedAt)
	}
	if v.LandedBranch != branch {
		t.Fatalf("landing token = %q, want the row's branch %q", v.LandedBranch, branch)
	}

	// ② the claim seam: no worker, and the row ends COMPLETED (not cancelled).
	res, err := TaskNextFiltered("regression-pin", "", "")
	if err != nil {
		t.Fatalf("TaskNextFiltered: %v", err)
	}
	if res != nil && !res.Empty {
		t.Fatalf("a landed row was CLAIMED (%s) — no worker may run work that already shipped", res.Task.TaskID)
	}
	if got := vgRowStatus(t, row); got != TaskStatusCompleted {
		t.Fatalf("row status = %q, want %q — a landed row must never be disposed as a failure", got, TaskStatusCompleted)
	}
	show := vgShow(t, row)
	if !strings.Contains(show, MergeVerifyStaleVerdictLanded) {
		t.Fatalf("the close must record verdict %q; show:\n%s", MergeVerifyStaleVerdictLanded, show)
	}
	if !strings.Contains(show, v.Landed) {
		t.Fatalf("the close must point at the landing commit %s; show:\n%s", v.Landed, show)
	}
	if !strings.Contains(show, MergeVerifyStaleLandedReasonPrefix) {
		t.Fatalf("the reason column must use the landed spelling %q; show:\n%s", MergeVerifyStaleLandedReasonPrefix, show)
	}
	if strings.Contains(show, taskStatusCancelled) {
		t.Fatalf("a landed row must not read as cancelled; show:\n%s", show)
	}
}

// TestStaleReworkGuardMergerConflictResolutionLandingIsLanded is the SECOND
// landing form D8 names: a branch merged by hand with `resolve merge <b> into
// master` (row 01a0a9e0's shape — `15060a5c resolve merge feat/merger-status-log
// into master`). The pre-fix guard did not know this form either.
func TestStaleReworkGuardMergerConflictResolutionLandingIsLanded(t *testing.T) {
	repo := vgRepo(t)
	vgLedger(t)
	const branch = "feat/resolved-by-hand"
	vgCommitFile(t, repo, branch, "hand.txt", "the fix\n", "fix: resolved by hand")
	vgGit(t, repo, "merge", "--no-ff", "-q", "-m", mergeVerifyResolveMergeSubject(branch), branch)
	vgGit(t, repo, "branch", "-d", branch)

	row := guardSubmitReworkRow(t, repo, branch, guardReworkPayload(repo, branch))
	v, err := MergeVerifyStaleReworkJudge(row)
	if err != nil {
		t.Fatalf("judge: %v", err)
	}
	if v.Verdict != MergeVerifyStaleVerdictLanded {
		t.Fatalf("verdict = %q, want %q (evidence: %s)", v.Verdict, MergeVerifyStaleVerdictLanded, v.Evidence)
	}
	if _, err := TaskNextFiltered("regression-pin", "", ""); err != nil {
		t.Fatalf("TaskNextFiltered: %v", err)
	}
	if got := vgRowStatus(t, row); got != TaskStatusCompleted {
		t.Fatalf("row status = %q, want %q", got, TaskStatusCompleted)
	}
}

// guardSeedMergeBacklogLandedBranch is the merge-backlog EXECUTOR's landing:
// the executor stages the merge without committing it, then commits with
// mergeExecMergeMsg (mergeExecGit's `commit --no-verify -m`), which names the
// target as the FULL ref (`refs/heads/<b>`) — the spelling the witness must
// reduce before comparing it with the ledger's branch name. The merged-cleanup
// then deletes the ref.
func guardSeedMergeBacklogLandedBranch(t *testing.T, repo, branch, file string, sweep bool) string {
	t.Helper()
	vgCommitFile(t, repo, branch, file, "the fix\n", "fix: "+branch)
	// The executor's own sequence — merge (staged), then the explicit commit.
	vgGit(t, repo, "merge", "--no-ff", "--no-commit", branch)
	vgGit(t, repo, "commit", "--no-verify", "-q",
		"-m", fmt.Sprintf(mergeExecMergeMsg, mergeVerifyLocalRefPrefix+branch))
	// The LANDING is the merge commit the executor wrote, not the branch tip
	// (the tip is the branch's own last commit, reachable THROUGH the merge).
	landed := vgGit(t, repo, "rev-parse", "HEAD")
	if sweep {
		vgGit(t, repo, "branch", "-d", branch)
	}
	return landed
}

// TestStaleReworkGuardMergeBacklogExecutorLandingIsLanded is the residual D8
// arm the master witness does NOT read: the merge-backlog executor
// (task_merge_executor.go) commits `merge-backlog: merge <b>` and its branch
// argument is a FULL ref. Neither the older parser (four forms, none of them
// this one) nor a bare equality on the raw token can attribute that landing, so
// a branch the executor merged and the cleanup swept was still reported as a
// branch no landing attests. The subject is built from the executor's OWN
// message constant and its branch from the SAME mergeVerifyLocalRefPrefix the
// witness strips, so a drift between the writer's spelling and the witness's
// reading fails here.
func TestStaleReworkGuardMergeBacklogExecutorLandingIsLanded(t *testing.T) {
	repo := vgRepo(t)
	vgLedger(t)
	const branch = "feat/merged-by-backlog-executor"
	landed := guardSeedMergeBacklogLandedBranch(t, repo, branch, "backlog.txt", true)
	// Push the landing OUT of a narrowed window, so the verdict can only come
	// from the full-history --grep arm — the arm whose PATTERN SET must carry
	// the executor's own form. Without the narrowing, a shallow fixture lets the
	// windowed positive pass prove the landing and the pattern is never
	// exercised (measured: a mutant that drops the executor pattern from
	// mergeVerifyLandingSubjectCandidates passed on a shallow repo).
	oldWindow := mergeVerifyLogWindowForTest
	t.Cleanup(func() { mergeVerifyLogWindowForTest = oldWindow })
	for i := 0; i < 5; i++ {
		guardCommitOnMaster(t, repo, "later.txt", fmt.Sprintf("later %d\n", i),
			fmt.Sprintf("chore: later commit %d", i))
	}
	mergeVerifyLogWindowForTest = mergeVerifyGitLogWindowTestNarrow

	row := guardSubmitReworkRow(t, repo, branch, guardReworkPayload(repo, branch))
	v, err := MergeVerifyStaleReworkJudge(row)
	if err != nil {
		t.Fatalf("judge: %v", err)
	}
	if v.Verdict != MergeVerifyStaleVerdictLanded || !v.Stale {
		t.Fatalf("verdict = %q stale=%v, want %q stale=true — the executor's landing is a real landing (evidence: %s)",
			v.Verdict, v.Stale, MergeVerifyStaleVerdictLanded, v.Evidence)
	}
	if v.Landed != landed {
		t.Fatalf("Landed = %q, want %s (evidence: %s)", v.Landed, landed, v.Evidence)
	}
	if v.LandedBranch != branch {
		t.Fatalf("landing token = %q, want the row's branch %q — the refs/heads/ spelling must be reduced, not reported",
			v.LandedBranch, branch)
	}
	// The dispatch seam: no worker, and the row is COMPLETED — never cancelled
	// as a branch that never existed.
	if _, err := TaskNextFiltered("regression-pin", "", ""); err != nil {
		t.Fatalf("TaskNextFiltered: %v", err)
	}
	if got := vgRowStatus(t, row); got != TaskStatusCompleted {
		t.Fatalf("row status = %q, want %q", got, TaskStatusCompleted)
	}
	show := vgShow(t, row)
	if !strings.Contains(show, landed) {
		t.Fatalf("the close must point at the landing commit %s; show:\n%s", landed, show)
	}
	if strings.Contains(show, taskStatusCancelled) {
		t.Fatalf("an executor-landed row must not read as cancelled; show:\n%s", show)
	}
}

// TestStaleReworkGuardMergeBacklogFormNeverCrossMatches pins the fifth form's
// edges: only the executor's own prefix is a witness, a bare `merge-backlog:`
// sentence is not, and the reported token is the BRANCH — the ref spelling is
// reduced at the comparison site, never leaked into the verdict.
func TestStaleReworkGuardMergeBacklogFormNeverCrossMatches(t *testing.T) {
	cases := []struct {
		label   string
		subject string
		want    string
		ok      bool
	}{
		{"full-ref", fmt.Sprintf(mergeExecMergeMsg, mergeVerifyLocalRefPrefix+"fix/a"), "refs/heads/fix/a", true},
		{"bare-name", fmt.Sprintf(mergeExecMergeMsg, "fix/a"), "fix/a", true},
		{"prefix-only", mergeVerifyMergeBacklogPrefix, "", false},
		{"prefixless", "merge-backlog: fix/a", "", false},
		{"blank-name", mergeVerifyMergeBacklogPrefix + " ", "", false},
		// A prose sentence that merely NAMES the subject is not a commit subject.
		{"body-mention", "docs: describe " + mergeVerifyMergeBacklogPrefix + "fix/a", "", false},
	}
	for _, tc := range cases {
		got, ok := mergeVerifyStaleLandingToken(tc.subject)
		if ok != tc.ok || got != tc.want {
			t.Fatalf("%s: landing token of %q = (%q, %v), want (%q, %v)",
				tc.label, tc.subject, got, ok, tc.want, tc.ok)
		}
	}
}

// TestStaleReworkGuardLandingTokenNormalizationIsAnEquality pins the
// normalization's SAFETY: it reduces a ref SPELLING and nothing more. A
// different branch whose name merely CONTAINS the row's branch must still be a
// different branch — the guard's whole value is that it closes a row only for
// the row's own landing.
func TestStaleReworkGuardLandingTokenNormalizationIsAnEquality(t *testing.T) {
	cases := []struct {
		label string
		token string
		want  string
	}{
		{"full-ref", mergeVerifyLocalRefPrefix + "fix/a", "fix/a"},
		{"bare", "fix/a", "fix/a"},
		{"trailing-sep", "fix/a/", "fix/a"},
		{"surrounding-space", "  fix/a  ", "fix/a"},
		{"empty", "", ""},
		{"prefix-only", mergeVerifyLocalRefPrefix, ""},
	}
	for _, tc := range cases {
		if got := mergeVerifyLandingTokenNormalize(tc.token); got != tc.want {
			t.Fatalf("%s: normalize(%q) = %q, want %q", tc.label, tc.token, got, tc.want)
		}
	}
	// The containment trap: a landing for fix/a/sub is NOT the row's fix/a.
	if mergeVerifyLandingTokenNormalize("fix/a/sub") == mergeVerifyLandingTokenNormalize("fix/a") {
		t.Fatal("normalization must not collapse a longer branch name into a shorter one")
	}
}

// guardCommitOnMaster adds one commit directly on master (used to push a seeded
// landing out of a narrowed window).
func guardCommitOnMaster(t *testing.T, repo, file, content, msg string) {
	t.Helper()
	vgGit(t, repo, "checkout", "-q", "master")
	if err := os.WriteFile(filepath.Join(repo, file), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	vgGit(t, repo, "add", file)
	vgGit(t, repo, "commit", "-q", "-m", msg)
}

// TestStaleReworkGuardLandingOlderThanWindowIsStillProved closes the gap the D8
// fix itself opened: the windowed positive scan (`-n mergeVerifyGitLogWindow`)
// cannot see a landing that sits deeper than the window, and the REAL repo is
// exactly that shape — master carries 3714 commits against a 2000-record window,
// so an absence read off the window alone would have been unsound in production
// AND the refusal arm would have been unreachable there (untested on the path
// that decides a row's fate). The full-history --grep search is what proves the
// landing anyway; this test narrows the window below a seeded landing and
// asserts the LANDED disposition still comes back.
func TestStaleReworkGuardLandingOlderThanWindowIsStillProved(t *testing.T) {
	repo := vgRepo(t)
	vgLedger(t)
	const branch = "feat/landed-deep-in-history"

	// Late-bound on purpose: restore the production window even if a Fatal
	// below unwinds the test (git facts seeded first, window narrowed after).
	oldWindow := mergeVerifyLogWindowForTest
	t.Cleanup(func() { mergeVerifyLogWindowForTest = oldWindow })

	// The landing, then enough later commits to push it out of a 2-record
	// window (the merge itself + these are what the windowed pass sees).
	guardSeedMergerLandedBranch(t, repo, branch, "deep.txt", true)
	for i := 0; i < 5; i++ {
		guardCommitOnMaster(t, repo, "later.txt", fmt.Sprintf("later %d\n", i),
			fmt.Sprintf("chore: later commit %d", i))
	}
	mergeVerifyLogWindowForTest = mergeVerifyGitLogWindowTestNarrow

	row := guardSubmitReworkRow(t, repo, branch, guardReworkPayload(repo, branch))
	v, err := MergeVerifyStaleReworkJudge(row)
	if err != nil {
		t.Fatalf("judge: %v", err)
	}
	if v.Verdict != MergeVerifyStaleVerdictLanded {
		t.Fatalf("verdict = %q, want %q — a landing older than the window must still be PROVED, not read as a loss (evidence: %s)",
			v.Verdict, MergeVerifyStaleVerdictLanded, v.Evidence)
	}
	if v.LandedBranch != branch {
		t.Fatalf("landing token = %q, want %q (evidence: %s)", v.LandedBranch, branch, v.Evidence)
	}
	if got := vgRowStatus(t, row); got == taskStatusCancelled {
		t.Fatalf("a landing older than the window was disposed as a failure (status %q)", got)
	}
}

// form yields its own branch, and a subject that only RESEMBLES a landing (a
// ledger summary, a `resolve merge conflict:` sentence, an unterminated quote)
// is not a witness — a false positive here would close a live rework row as
// landed.
func TestStaleReworkGuardLandingFormsNeverCrossMatch(t *testing.T) {
	cases := []struct {
		subject string
		want    string
		ok      bool
	}{
		{mergeVerifyGitMergeSubject("fix/a"), "fix/a", true},
		{mergeVerifyGitRemoteMergeSubject("origin/fix/a"), "origin/fix/a", true},
		{mergeVerifyResolveMergeSubject("fix/a"), "fix/a", true},
		{mergeVerifyAdoptionSubject("fix/a", "deadbeef"), "fix/a", true},
		{"resolve merge conflict: fix/a into master", "", false},
		// A real merge subject that carries an authored tail: the branch's
		// work is reachable from master, so the token IS a landing witness.
		{"Merge branch 'fix/a' (a0bc3e11) into fix/other", "fix/a", true},
		{"Merge branch fix/a", "", false},
		{"docs: mention Merge branch 'fix/a'", "", false},
		{"resolve merge  into master", "", false},
		{"adoption: merge fix/a", "", false},
	}
	for _, tc := range cases {
		got, ok := mergeVerifyStaleLandingToken(tc.subject)
		if ok != tc.ok || got != tc.want {
			t.Fatalf("landing token of %q = (%q, %v), want (%q, %v)", tc.subject, got, ok, tc.want, tc.ok)
		}
	}
}

// TestStaleReworkGuardUnresolvableBranchWithoutLandingIsHardNoOp: an
// unresolvable ref with NO landing witness is a hard claim-time refusal — the
// briefing orders a checkout of a ref that was never created.
func TestStaleReworkGuardUnresolvableBranchWithoutLandingIsHardNoOp(t *testing.T) {
	repo := vgRepo(t)
	vgLedger(t)
	const typo = "fix/never-existed-at-all"
	row := guardSubmitReworkRow(t, repo, typo, guardReworkPayload(repo, typo))

	res, err := TaskNextFiltered("regression-pin", "", "")
	if err != nil {
		t.Fatalf("TaskNextFiltered: %v", err)
	}
	if res != nil && !res.Empty {
		t.Fatalf("an unresolvable-branch row was claimed (%s)", res.Task.TaskID)
	}
	if got := vgRowStatus(t, row); got != TaskStatusCancelled {
		t.Fatalf("row status = %q, want %q", got, TaskStatusCancelled)
	}
	show := vgShow(t, row)
	if !strings.Contains(show, MergeVerifyStaleVerdictBranchGoneUnproved) {
		t.Fatalf("the refusal must record verdict %q; show:\n%s", MergeVerifyStaleVerdictBranchGoneUnproved, show)
	}
	if !strings.Contains(show, typo) {
		t.Fatalf("the refusal must name the unresolvable ref %q; show:\n%s", typo, show)
	}
	// The REWORDED genuine-loss clause (D8): the refusal states the measured
	// fact — the ref is gone AND no landing is provable — instead of asserting
	// a typo.
	//
	// The assertion is a PAIR and deliberately asymmetric, because each half
	// alone is defeatable:
	//   · asserting MergeVerifyStaleBranchGoneReason proves the writer and the
	//     reader agree on ONE spelling, but it is self-referential — a mutation
	//     that rewrote the constant would rewrite both sides and stay green;
	//   · asserting the RETIRED pre-fix claim is ABSENT is what pins the D8
	//     correction itself: restoring the old sentence turns this arm red.
	// The absence half reads the row's CLOSE EVENT detail (guardRowRefusalText)
	// — the only surface that carries the full text: `task show` truncates it
	// and the `reason` column stays compact, so either would make the check
	// pass vacuously.
	// The discredited word therefore lives here as a literal — the one place a
	// literal is the POINT (the claim being falsified), and it is a test, so no
	// production site carries it.
	refusal := guardRowRefusalText(t, row)
	if !strings.Contains(refusal, MergeVerifyStaleBranchGoneReason) {
		t.Fatalf("the refusal must state %q; refusal text:\n%s", MergeVerifyStaleBranchGoneReason, refusal)
	}
	if strings.Contains(refusal, guardRetiredTypoClaimWord) {
		t.Fatalf("the refusal still asserts the RETIRED pre-fix claim (the word %q) — D8 replaced it with the measured fact; refusal text:\n%s",
			guardRetiredTypoClaimWord, refusal)
	}
	if strings.Contains(MergeVerifyStaleNoOpText(MergeVerifyStaleVerdict{Stale: true, Branch: typo, Repo: repo, Verdict: MergeVerifyStaleVerdictBranchGoneUnproved}), guardRetiredTypoClaimWord) {
		t.Fatalf("the guard's own refusal TEXT still carries the retired claim (the word %q)", guardRetiredTypoClaimWord)
	}
}

// guardRetiredTypoClaimWord is the load-bearing WORD of the pre-fix refusal D8
// replaced — the claim that an unresolvable ref means the briefing was a typo
// ("The briefing names a ref that was never created (a typo of the real ref…)").
//
// The word, not the sentence, is the marker on purpose: the pre-fix sentence
// has more than one spelling ("a typo of the real ref" / "a briefing typo of
// the real ref" — a sentence-literal check silently missed the second, measured
// as mutation B staying GREEN). The word is what carries the discredited claim,
// and the reworded refusal states the MEASURED fact without it, so its absence
// is a real, mutation-checkable property. Named (Rule 1b), test-only: no
// production file carries this claim.
const guardRetiredTypoClaimWord = "typo"

// TestStaleReworkGuardAlreadyLandedRefStillPresentIsNoOp: the ref survived (a
// linked worktree keeps it alive) but its tip is already an ancestor of the
// integration branch — the fix is on master, the rework is owed by nobody.
func TestStaleReworkGuardAlreadyLandedRefStillPresentIsNoOp(t *testing.T) {
	repo := vgRepo(t)
	vgLedger(t)
	const branch = "fix/landed-ref-kept"
	guardSeedMergedBranch(t, repo, branch, "kept.txt", false) // ref NOT swept
	row := guardSubmitReworkRow(t, repo, branch, guardReworkPayload(repo, branch))

	v, err := MergeVerifyStaleReworkJudge(row)
	if err != nil {
		t.Fatalf("judge: %v", err)
	}
	if v.Verdict != MergeVerifyStaleVerdictAlreadyLanded || !v.Stale {
		t.Fatalf("verdict = %q stale=%v, want %q (evidence: %s)",
			v.Verdict, v.Stale, MergeVerifyStaleVerdictAlreadyLanded, v.Evidence)
	}
	res, err := TaskNextFiltered("regression-pin", "", "")
	if err != nil {
		t.Fatalf("TaskNextFiltered: %v", err)
	}
	if res != nil && !res.Empty {
		t.Fatalf("an already-landed row was claimed (%s)", res.Task.TaskID)
	}
	if got := vgRowStatus(t, row); got != TaskStatusCancelled {
		t.Fatalf("row status = %q, want %q", got, TaskStatusCancelled)
	}
}

// TestStaleReworkGuardUnlandedReworkStillDispatched is the NOT-THIS-ARM
// control: a branch that resolves and is NOT on the integration branch is
// legitimate rework — the guard must hand it to a worker (a guard that blocked
// it would silently kill the lane).
func TestStaleReworkGuardUnlandedReworkStillDispatched(t *testing.T) {
	repo := vgRepo(t)
	vgLedger(t)
	const branch = "fix/really-unlanded"
	// The branch exists with a commit master does NOT have — the rework is owed.
	vgCommitFile(t, repo, branch, "unlanded.txt", "work\n", "fix: really unlanded")
	row := guardSubmitReworkRow(t, repo, branch, guardReworkPayload(repo, branch))

	v, err := MergeVerifyStaleReworkJudge(row)
	if err != nil {
		t.Fatalf("judge: %v", err)
	}
	if v.Stale {
		t.Fatalf("a genuinely unlanded branch judged STALE (%q, evidence: %s)", v.Verdict, v.Evidence)
	}
	if v.Verdict != MergeVerifyStaleVerdictPremiseOpen {
		t.Fatalf("verdict = %q, want %q (evidence: %s)", v.Verdict, MergeVerifyStaleVerdictPremiseOpen, v.Evidence)
	}

	res, err := TaskNextFiltered("regression-pin", "", "")
	if err != nil {
		t.Fatalf("TaskNextFiltered: %v", err)
	}
	if res == nil || res.Empty || res.Task == nil {
		t.Fatal("the unlanded rework row was NOT claimable — the guard blocked real work")
	}
	if res.Task.TaskID != row {
		t.Fatalf("claimed %s, want the unlanded rework row %s", res.Task.TaskID, row)
	}
	if got := vgRowStatus(t, row); got != TaskStatusClaimed {
		t.Fatalf("row status = %q, want %q (dispatched)", got, TaskStatusClaimed)
	}
}

// TestStaleReworkGuardLeavesUnrelatedRowsAlone: the guard is lane-scoped — an
// ordinary pending row is still claimed, and a completed/unrelated row is never
// touched by the sweep.
func TestStaleReworkGuardLeavesUnrelatedRowsAlone(t *testing.T) {
	repo := vgRepo(t)
	vgLedger(t)
	if _, err := TaskSubmit(TaskSubmitOptions{
		Summary: "unrelated ordinary work",
		Payload: "do the ordinary thing",
		Tags:    "bug",
		Source:  "auto",
		Repo:    repo,
	}); err != nil {
		t.Fatalf("submit ordinary row: %v", err)
	}
	res, err := TaskNextFiltered("regression-pin", "", "")
	if err != nil {
		t.Fatalf("TaskNextFiltered: %v", err)
	}
	if res == nil || res.Empty || res.Task == nil {
		t.Fatal("an ordinary pending row must still be claimable")
	}
	if res.Task.Summary != "unrelated ordinary work" {
		t.Fatalf("claimed %q, want the ordinary row", res.Task.Summary)
	}
}

// TestStaleReworkGuardClaimNoteNamesTheMeasurement: the read-only entry an
// operator uses before a round must report the SAME judgement the claim path
// enforces (verdict + evidence), never a silent nothing.
func TestStaleReworkGuardClaimNoteNamesTheMeasurement(t *testing.T) {
	repo := vgRepo(t)
	vgLedger(t)
	const branch = "fix/note-probe"
	vgCommitFile(t, repo, branch, "n.txt", "x\n", "fix: note probe")
	row := guardSubmitReworkRow(t, repo, branch, guardReworkPayload(repo, branch))
	note := MergeVerifyStaleClaimNote(row)
	if !strings.Contains(note, MergeVerifyStaleNoOpMarker) || !strings.Contains(note, row) {
		t.Fatalf("claim note = %q, want the marker + row id", note)
	}
	if !strings.Contains(note, MergeVerifyStaleVerdictPremiseOpen) {
		t.Fatalf("claim note = %q, want the measured verdict %q", note, MergeVerifyStaleVerdictPremiseOpen)
	}
	// A non-lane row has no premise to report.
	if got := MergeVerifyStaleClaimNote("no-such-row"); got != "" {
		t.Fatalf("a missing row must yield no note, got %q", got)
	}
}

// TestVerifyGateReworkBriefingRendersNoVerdictAsNoVerdict is the wrong-premise
// fix pin: a rework briefing filed for a NO-VERDICT reason must NOT claim the
// verify child "judged the merged state ON THE REAL MASTER failing" — the
// gate's own reason string (MergeVerifyReasonChildDead/ChildTimeout) says the
// opposite, and the briefing contradicted its own parenthetical (row
// 01a09e9b). A FAILED verdict still renders as an observed failure.
func TestVerifyGateReworkBriefingRendersNoVerdictAsNoVerdict(t *testing.T) {
	const contradiction = "judged the merged state ON THE REAL MASTER failing"
	cases := []struct {
		reason      string
		wantContain string
		wantContra  bool
	}{
		{MergeVerifyReasonChildDead, MergeVerifyReasonChildDead, false},
		{MergeVerifyReasonChildTimeout, MergeVerifyReasonChildTimeout, false},
		{MergeVerifyReasonFail, MergeVerifyReasonFail, true},
	}
	for _, tc := range cases {
		t.Run(tc.reason, func(t *testing.T) {
			repo := vgRepo(t)
			vgLedger(t)
			vgCommitFile(t, repo, "fix/briefing", "b.txt", "x\n", "fix: briefing")
			old := vgSubmitRow(t, repo, "fix/briefing", "bug", "bug: briefing")
			vgFinishRow(t, old, TaskStatusCompleted)

			id, err := MergeVerifyFileRework(MergeVerifyReworkOptions{
				Repo: repo, Master: "master", Branch: "fix/briefing",
				OldTaskID: old, OldSummary: "bug: briefing",
				PreTip: "0f0f0f0f0f0f0f0f0f0f0f0f0f0f0f0f0f0f0f0f",
				Revert: "git reset --hard 0f0f0f",
				Reason: tc.reason,
			})
			if err != nil {
				t.Fatalf("file rework: %v", err)
			}
			_, payload, err := TaskBriefing(id)
			if err != nil {
				t.Fatalf("read payload: %v", err)
			}
			if !strings.Contains(payload, tc.wantContain) {
				t.Fatalf("the briefing must carry the reason %q; payload:\n%s", tc.wantContain, payload)
			}
			gotContra := strings.Contains(payload, contradiction)
			if gotContra != tc.wantContra {
				t.Fatalf("briefing claims a measured failure on master = %v, want %v (reason %q); payload:\n%s",
					gotContra, tc.wantContra, tc.reason, payload)
			}
			if !tc.wantContra && !strings.Contains(payload, "NO VERDICT") {
				t.Fatalf("a no-verdict briefing must SAY so; payload:\n%s", payload)
			}
		})
	}
}

// TestStaleReworkGuardJudgesOnTheDeliveryRefWhenLocalMasterTrails is task
// 01a12d30's defect, driven through the REAL dispatch seam: the branch tip IS
// landed — on the ref the delivery lane actually maintains — while the LOCAL
// integration branch is behind and says "not landed". Before the claim target
// was resolved, this guard asked the local ref and judged a LANDED branch as
// owed rework (the same false negative the bug row 01a12d1b's premise came
// from). The control arm (no mirror at all) proves the fix activates on the
// MEASURED ref relation and on nothing else: with no delivery ref to trust, the
// guard must answer exactly as it always did.
func TestStaleReworkGuardJudgesOnTheDeliveryRefWhenLocalMasterTrails(t *testing.T) {
	repo := vgRepo(t)
	vgLedger(t)
	seed := vgGit(t, repo, "rev-parse", IntegrationBranchName)

	const branch = "fix/landed-under-stale-local-master"
	tip := guardSeedMergedBranch(t, repo, branch, "landed.txt", false) // merged, ref kept
	merged := vgGit(t, repo, "rev-parse", IntegrationBranchName)

	// Rewind the LOCAL branch to the seed and let ONLY the delivery lane's mirror
	// carry the landing — the measured client shape (local master behind, mirror
	// ahead, branch tip landed).
	vgGit(t, repo, "update-ref", integrationClaimLocalRef, seed)
	vgGit(t, repo, "update-ref", gitPushbackCheckRef, merged)

	// Fixture sanity: the two refs really disagree about this very tip.
	if got := mergeVerifyStaleGitExit(t, repo, tip, integrationClaimLocalRef); got != mergeVerifyGitExitNo {
		t.Fatalf("fixture: tip vs %s → exit=%d, want %d", integrationClaimLocalRef, got, mergeVerifyGitExitNo)
	}
	if got := mergeVerifyStaleGitExit(t, repo, tip, gitPushbackCheckRef); got != 0 {
		t.Fatalf("fixture: tip vs %s → exit=%d, want 0", gitPushbackCheckRef, got)
	}

	row := guardSubmitReworkRow(t, repo, branch, guardReworkPayload(repo, branch))
	v, err := MergeVerifyStaleReworkJudge(row)
	if err != nil {
		t.Fatalf("judge: %v", err)
	}
	if v.Verdict != MergeVerifyStaleVerdictAlreadyLanded || !v.Stale {
		t.Fatalf("verdict = %q stale=%v, want %q — the guard judged a LANDED branch as owed rework (evidence: %s)",
			v.Verdict, v.Stale, MergeVerifyStaleVerdictAlreadyLanded, v.Evidence)
	}
	if v.Master != gitPushbackCheckRef {
		t.Fatalf("verdict.Master = %q, want the delivery lane's mirror %q", v.Master, gitPushbackCheckRef)
	}
	if !strings.Contains(v.Evidence, gitPushbackCheckRef) {
		t.Errorf("evidence must name the ref it measured against; got %q", v.Evidence)
	}
	res, err := TaskNextFiltered("regression-pin-stale-master", "", "")
	if err != nil {
		t.Fatalf("TaskNextFiltered: %v", err)
	}
	if res != nil && !res.Empty {
		t.Fatalf("a landed row was claimed for rework (%s)", res.Task.TaskID)
	}
	if got := vgRowStatus(t, row); got != TaskStatusCancelled {
		t.Fatalf("row status = %q, want %q", got, TaskStatusCancelled)
	}
}

// TestStaleReworkGuardWithoutADeliveryRefStillDispatches is the control for the
// test above: the SAME rewound local branch, with NO delivery mirror to trust,
// leaves the guard's answer unchanged (premise open, rework dispatched). Without
// this arm "always judge on the mirror" would pass the green leg.
func TestStaleReworkGuardWithoutADeliveryRefStillDispatches(t *testing.T) {
	repo := vgRepo(t)
	vgLedger(t)
	seed := vgGit(t, repo, "rev-parse", IntegrationBranchName)

	const branch = "fix/landed-but-no-delivery-ref"
	tip := guardSeedMergedBranch(t, repo, branch, "landed.txt", false)
	merged := vgGit(t, repo, "rev-parse", IntegrationBranchName)
	vgGit(t, repo, "update-ref", integrationClaimLocalRef, seed)
	if merged == seed {
		t.Fatal("fixture: the merge produced no commit")
	}
	if got := mergeVerifyStaleGitExit(t, repo, tip, integrationClaimLocalRef); got != mergeVerifyGitExitNo {
		t.Fatalf("fixture: tip vs %s → exit=%d, want %d", integrationClaimLocalRef, got, mergeVerifyGitExitNo)
	}

	row := guardSubmitReworkRow(t, repo, branch, guardReworkPayload(repo, branch))
	v, err := MergeVerifyStaleReworkJudge(row)
	if err != nil {
		t.Fatalf("judge: %v", err)
	}
	if v.Stale {
		t.Fatalf("with no delivery ref the guard must not claim a landing (verdict %q, evidence: %s)", v.Verdict, v.Evidence)
	}
	if v.Master != integrationClaimLocalRef {
		t.Fatalf("verdict.Master = %q, want the local ref %q", v.Master, integrationClaimLocalRef)
	}
	res, err := TaskNextFiltered("regression-pin-no-mirror", "", "")
	if err != nil {
		t.Fatalf("TaskNextFiltered: %v", err)
	}
	if res == nil || res.Empty || res.Task == nil || res.Task.TaskID != row {
		t.Fatalf("the rework row %s was not claimable", row)
	}
}

// mergeVerifyStaleGitExit returns one ancestry probe's EXIT STATUS in repo — the
// instrument the two tests above assert a fixture on (never git's text).
func mergeVerifyStaleGitExit(t *testing.T, repo, tip, ref string) int {
	t.Helper()
	_, exit, _ := mergeVerifyStaleGit(repo, "merge-base", "--is-ancestor", tip, ref)
	return exit
}
