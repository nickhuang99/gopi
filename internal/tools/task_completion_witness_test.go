package tools

// THE COMPLETION-WITNESS CRITERION'S REGRESSION TESTS (epic 01a182bf).
//
// The contract, in the operator's terms:
//
//   - a row claiming `completed` with NO artefact behind it (its branch ref
//     gone, no landed record naming a commit this repo still holds, no
//     reachable commit naming the row) gets ONE attributed record on its own
//     trail — and the claim STANDS, because this arm reads and appends and
//     deletes nothing (turning `completed` back into `failed` would re-open the
//     defect row 01a18244 fixed);
//   - the row that carries a commit TRAILER naming it is WITNESSED even after
//     its ref AND its ground are gone — the measured shape of row
//     01a18253-f7c4, whose only surviving artefact was
//     `20e8328 … task: 01a18253-f7c4-7cf7-8cb3-868647a25401`;
//   - a completion that rests on the channel's own MEASURED CONCLUSION (the
//     nothing-to-deliver exit) is the third verdict, NOT the defect: the whole
//     point is that the ledger can tell "concluded" from "unproven";
//   - a delivered row is SILENT — the trail is an exception log, not a
//     heartbeat;
//   - a row naming no repository is `unknown`, never `witnessless`: "this row
//     names no repo" is a different fact from "this row's repo holds nothing".
//
// THE RED PROOF: with the arm's witnessless branch removed (or the trailer arm
// disabled) the fixtures below come back `witnessed`/silent and these tests
// fail naming it — the criterion is what turns them green, not the fixture.

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nick/gopi/internal/removalaudit"
)

// cwFixture builds the incident's HOST state: a repo with a branch that has
// its own commit, a worktree checked out on it, and a submitted+started row
// recording both. It returns the repo, the worktree, the branch and the sha the
// branch held — so a test can decide WHICH artefact survives.
func cwFixture(t *testing.T, taskID string) (repo, wt, branch, tip string) {
	t.Helper()
	repo, wt, branch = deadHolderReclaimFixture(t)
	t.Setenv(removalaudit.EnvSink, filepath.Join(t.TempDir(), "removal-audit.log"))
	if _, err := TaskSubmit(TaskSubmitOptions{Summary: "s", Payload: "p", TaskID: taskID, Source: "human"}); err != nil {
		t.Fatal(err)
	}
	seedDeliverable(t, taskID, repo, branch, wt)
	if _, err := TaskStart(taskID, "worker-1"); err != nil {
		t.Fatal(err)
	}
	tip = strings.TrimSpace(covLifeGit(t, repo, covLifeGitBinary, "rev-parse", "refs/heads/"+branch))
	if tip == "" {
		t.Fatalf("fixture: refs/heads/%s did not resolve", branch)
	}
	return repo, wt, branch, tip
}

// cwRetire removes BOTH addresses the ledger names for the row — the worktree
// registration and the branch ref — which is exactly what the close-out chain
// does after a delivered run (worktree remove → branch delete).
func cwRetire(t *testing.T, repo, wt, branch string) {
	t.Helper()
	gitRun(t, repo, "worktree", "remove", wt)
	gitRun(t, repo, "update-ref", "-d", "refs/heads/"+branch)
}

// cwWitnesslessEventDetail returns the row's newest witnessless record ("" when
// none was filed) — the same trail a reviewer reads.
func cwWitnesslessEventDetail(t *testing.T, taskID string) string {
	t.Helper()
	return lastEventDetail(t, taskID, CompletionWitnesslessEvent)
}

// TestCompletionWitnessClaimWithoutArtefactIsRecorded is the DEFECT CLASS: the
// row's ref, ground and history all say nothing, and the claim is nevertheless
// `completed`. The arm must name it and leave the status alone.
func TestCompletionWitnessClaimWithoutArtefactIsRecorded(t *testing.T) {
	const taskID = "t-cw-gone"
	repo, wt, branch, tip := cwFixture(t, taskID)
	// The commit the fixture made is reachable from the branch ONLY; retiring
	// the branch strands it, so no ref reaches it and the trailer arm sees
	// nothing. This is the state the epic calls witnessless.
	cwRetire(t, repo, wt, branch)
	if got := strings.TrimSpace(covLifeGit(t, repo, covLifeGitBinary, "for-each-ref", "--contains", tip, "--format=%(refname)")); got != "" {
		t.Fatalf("fixture: no ref may contain %s, got %q", tip, got)
	}

	out, err := TaskCompletionWitnessOn(taskID, TaskStatusCompleted, "")
	if err != nil {
		t.Fatalf("judge: %v", err)
	}
	if out.Verdict != CompletionWitnessless {
		t.Fatalf("the row claims %s with NO artefact behind it (branch %s resolves to no ref, no reachable commit names it) — verdict %q (%s)",
			TaskStatusCompleted, branch, out.Verdict, out.Note)
	}
	for _, want := range []string{CompletionWitnesslessMarker, taskID, branch} {
		if !strings.Contains(out.Note, want) {
			t.Errorf("the sentence must name %q — got %q", want, out.Note)
		}
	}
	// THE RECOVERY ADDRESS (bug 01a193f8, rows 01a190bb / 01a190b4): the row's
	// recorded ground is where the unpublished commits would be, so the sentence
	// must carry it. The pre-fix wording named neither the ground nor its HEAD and
	// left an operator opening session logs by hand to find one address the arm
	// already held.
	if !strings.Contains(out.Note, wt) {
		t.Errorf("the sentence must name the row's recorded ground %q — got %q", wt, out.Note)
	}
	if !strings.Contains(out.Note, "no longer on disk") {
		t.Errorf("this fixture's ground was RETIRED, so the sentence must say it is gone rather than stay silent about it — got %q", out.Note)
	}

	// THE RECORD HALF, through the production door the close-out uses.
	if _, err := TaskCompletionWitnessAtWriteback(taskID, TaskStatusCompleted, ""); err != nil {
		t.Fatalf("writeback arm: %v", err)
	}
	detail := cwWitnesslessEventDetail(t, taskID)
	if detail == "" {
		t.Fatalf("SILENT CLAIM: the row reads %s while its own deliverable %s resolves to no ref, no reachable commit names it, and NOTHING on its trail says so", TaskStatusCompleted, branch)
	}
	if !strings.Contains(detail, CompletionWitnesslessMarker) {
		t.Errorf("the record must carry the criterion's phrase — got %q", detail)
	}
	// IDEMPOTENT: a second stamp does not drip a second identical sentence.
	if _, err := TaskCompletionWitnessAtWriteback(taskID, TaskStatusCompleted, ""); err != nil {
		t.Fatalf("second writeback arm: %v", err)
	}
	if n := cwEventCount(t, taskID, CompletionWitnesslessEvent); n != completionWitnessRecordLimit {
		t.Fatalf("the record must be filed once (brake %d), got %d", completionWitnessRecordLimit, n)
	}
}

// TestCompletionWitnessCommitTrailerWitnessesAGoneGround is THE MEASURED ROW
// (01a18253-f7c4): ref gone, ground gone, and one commit reachable from a ref
// carries `task: <id>`. That commit is the witness, and the row is NOT the
// defect — the arm must say so, and file nothing.
func TestCompletionWitnessCommitTrailerWitnessesAGoneGround(t *testing.T) {
	const taskID = "t-cw-trailer"
	repo, wt, branch, _ := cwFixture(t, taskID)
	cwRetire(t, repo, wt, branch)
	// A commit reachable from the repo's own branch that names the row in its
	// trailer — the artefact that survives both the ground and the address ref.
	gitRun(t, repo, "checkout", "master")
	if err := os.WriteFile(filepath.Join(repo, "landed.txt"), []byte("delivered\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitRun(t, repo, "add", "landed.txt")
	gitRun(t, repo, "commit", "-q", "-m", "fix: delivered work", "--trailer", TaskTrailerKey+": "+taskID)
	gitRun(t, repo, "checkout", "--detach")

	out, err := TaskCompletionWitnessOn(taskID, TaskStatusCompleted, "")
	if err != nil {
		t.Fatalf("judge: %v", err)
	}
	if out.Verdict != CompletionWitnessed {
		t.Fatalf("a commit reachable from a ref names this row (`%s: %s`) — the row IS witnessed; verdict %q (%s)",
			TaskTrailerKey, taskID, out.Verdict, out.Note)
	}
	if len(out.Witnesses) == 0 || !strings.HasPrefix(out.Witnesses[0], "commit:") {
		t.Fatalf("the witness must be the commit arm — got %v", out.Witnesses)
	}
	if _, err := TaskCompletionWitnessAtWriteback(taskID, TaskStatusCompleted, ""); err != nil {
		t.Fatalf("writeback arm: %v", err)
	}
	if detail := cwWitnesslessEventDetail(t, taskID); detail != "" {
		t.Fatalf("a WITNESSED row must stay silent on the trail — got %q", detail)
	}
}

// TestCompletionWitnessConcludedCompletionIsNotTheDefect is the distinction the
// epic exists for: the SAME witnessless state, but the completion rests on a
// conclusion the channel measured. It must come back `concluded`, and it must
// file NO witnessless record — an unproven claim and a concluded one are not the
// same row.
func TestCompletionWitnessConcludedCompletionIsNotTheDefect(t *testing.T) {
	const taskID = "t-cw-concluded"
	repo, wt, branch, _ := cwFixture(t, taskID)
	cwRetire(t, repo, wt, branch)

	const conclusion = "clone channel: nothing to deliver — the ground held nothing past the integration point (rev-list --count … → 0)"
	out, err := TaskCompletionWitnessOn(taskID, TaskStatusCompleted, conclusion)
	if err != nil {
		t.Fatalf("judge: %v", err)
	}
	if out.Verdict != CompletionConcluded {
		t.Fatalf("a completion resting on a MEASURED conclusion must read %q, got %q (%s)",
			CompletionConcluded, out.Verdict, out.Note)
	}
	if !strings.Contains(out.Note, conclusion) {
		t.Errorf("the sentence must quote the conclusion it rests on — got %q", out.Note)
	}
	if strings.Contains(out.Note, CompletionWitnesslessMarker) {
		t.Errorf("a concluded completion must NOT be described as unproven — got %q", out.Note)
	}
	if _, err := TaskCompletionWitnessAtWriteback(taskID, TaskStatusCompleted, conclusion); err != nil {
		t.Fatalf("writeback arm: %v", err)
	}
	if detail := cwWitnesslessEventDetail(t, taskID); detail != "" {
		t.Fatalf("the defect record must not be filed for a CONCLUDED completion — got %q", detail)
	}
}

// TestCompletionWitnessLiveRefIsSilent is the ordinary delivered close-out: the
// branch still resolves, so nothing is filed and nothing is said.
func TestCompletionWitnessLiveRefIsSilent(t *testing.T) {
	const taskID = "t-cw-live"
	repo, wt, branch, _ := cwFixture(t, taskID)
	_ = repo
	_ = wt
	out, err := TaskCompletionWitnessOn(taskID, TaskStatusCompleted, "")
	if err != nil {
		t.Fatalf("judge: %v", err)
	}
	if out.Verdict != CompletionWitnessed {
		t.Fatalf("refs/heads/%s still resolves — the row is witnessed; verdict %q (%s)", branch, out.Verdict, out.Note)
	}
	if _, err := TaskCompletionWitnessAtWriteback(taskID, TaskStatusCompleted, ""); err != nil {
		t.Fatalf("writeback arm: %v", err)
	}
	if detail := cwWitnesslessEventDetail(t, taskID); detail != "" {
		t.Fatalf("a delivered row must stay silent on the trail — got %q", detail)
	}
}

// TestCompletionWitnessRowWithoutRepoIsUnknown: a row that names no repository
// is not accused. `unknown` is the honest non-answer and it is REPORTED, never
// swallowed (Rule 15).
func TestCompletionWitnessRowWithoutRepoIsUnknown(t *testing.T) {
	const taskID = "t-cw-norepo"
	if _, err := TaskSubmit(TaskSubmitOptions{Summary: "s", Payload: "p", TaskID: taskID, Source: "human"}); err != nil {
		t.Fatal(err)
	}
	// TaskSubmit resolves THIS process's cwd as the default repo, so the row is
	// stripped back to "records no repository" explicitly — that is the fact
	// under test, and it must not be inherited from where the suite happens to run.
	seedDeliverable(t, taskID, "", "", "")
	out, err := TaskCompletionWitnessOn(taskID, TaskStatusCompleted, "")
	if err != nil {
		t.Fatalf("judge: %v", err)
	}
	if out.Verdict != completionWitnessUnkown {
		t.Fatalf("a row naming no repo must read %q, got %q (%s)", completionWitnessUnkown, out.Verdict, out.Note)
	}
	if out.Note == "" {
		t.Fatal("the non-answer must be REPORTED, not silent (Rule 15)")
	}
	if _, err := TaskCompletionWitnessAtWriteback(taskID, TaskStatusCompleted, ""); err != nil {
		t.Fatalf("writeback arm: %v", err)
	}
	if detail := cwWitnesslessEventDetail(t, taskID); detail != "" {
		t.Fatalf("an UNKNOWN verdict must never be recorded as the defect — got %q", detail)
	}
}

// TestCompletionWitnessFailedClaimIsNotJudged: the arm judges DELIVERY claims
// only. A `failed` row is not a claim that anything was produced, so it is not
// measured — and `taskStatusClaimsDelivery` is the ONE place that rule lives.
func TestCompletionWitnessFailedClaimIsNotJudged(t *testing.T) {
	const taskID = "t-cw-failed"
	repo, wt, branch, _ := cwFixture(t, taskID)
	cwRetire(t, repo, wt, branch)
	if _, err := TaskCompletionWitnessAtWriteback(taskID, TaskStatusFailed, ""); err != nil {
		t.Fatalf("writeback arm: %v", err)
	}
	if detail := cwWitnesslessEventDetail(t, taskID); detail != "" {
		t.Fatalf("a failed row claims no delivery and must not be judged — got %q", detail)
	}
}

// cwEventCount counts a row's events of one name.
func cwEventCount(t *testing.T, taskID, event string) int {
	t.Helper()
	ctx := context.Background()
	db := openTestTasksDB(t, memoryDBPath())
	defer db.Close()
	var n int
	if err := db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM task_events WHERE task_id = ? AND event = ?`, taskID, event).Scan(&n); err != nil {
		t.Fatalf("count %s: %v", event, err)
	}
	return n
}

// TestCompletionWitnesslessNamesTheGroundItStandsOn is the second half of bug
// 01a193f8: the witnessless sentence must be an ADDRESS, not a shrug. When the
// ground still exists the sentence names the commit it stands on — the exact
// thing an operator has to fetch — and it never edits, deletes or claims that
// work (its contract is RECORD, not repair).
func TestCompletionWitnesslessNamesTheGroundItStandsOn(t *testing.T) {
	const taskID = "t-cw-ground"
	repo, _, branch, _ := cwFixture(t, taskID)
	// The branch REF is retired; the GROUND stays and is still standing on a
	// readable commit. That is the post-close-out shape a clone-channel row
	// leaves behind when its push never landed, and the one where the arm can
	// still say WHERE the work is. (The fixture's ground is the row's own repo
	// checkout — a linked worktree whose branch was just deleted has no HEAD to
	// read, which is a DIFFERENT shape and the arm states it differently.)
	gitRun(t, repo, "update-ref", "-d", "refs/heads/"+branch)
	seedDeliverable(t, taskID, repo, branch, repo)

	out, err := TaskCompletionWitnessOn(taskID, TaskStatusCompleted, "")
	if err != nil {
		t.Fatalf("judge: %v", err)
	}
	if out.Verdict != CompletionWitnessless {
		t.Fatalf("verdict = %q, want %q (%s)", out.Verdict, CompletionWitnessless, out.Note)
	}
	// The commit the ground stands on, read the way the arm reads it (the arm
	// renders the SHORT form: an operator pastes it into `git show` from inside
	// that directory, and the full id is one `rev-parse` away).
	head := strings.TrimSpace(covLifeGit(t, repo, covLifeGitBinary, "rev-parse", "HEAD"))
	short := branchDeleteShortHash(head)
	for _, want := range []string{repo, short} {
		if !strings.Contains(out.Note, want) {
			t.Errorf("the sentence must name %q — the ground and the commit it stands on are what an operator recovers from; got %q", want, out.Note)
		}
	}
	// THE ARM MUST NOT REPAIR. If the branch ref came back, the witness would be
	// creating the artefact it was asked to look for. (Probed directly rather
	// than through covLifeGit, which fails the test on a non-zero exit — and a
	// non-zero exit is the PASSING answer here.)
	probe := exec.Command("git", "rev-parse", "--verify", "--quiet", "refs/heads/"+branch)
	probe.Dir = repo
	if outBytes, perr := probe.Output(); perr == nil {
		t.Fatalf("the witnessless arm re-created refs/heads/%s (%s) — it RECORDS an absence, it does not manufacture the artefact", branch, strings.TrimSpace(string(outBytes)))
	}
}
