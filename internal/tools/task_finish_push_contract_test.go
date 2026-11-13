package tools

// Client-mode PUSH CONTRACT tests (epic client-push-rebase, 2026-09-19).
//
// The epic asked three questions about the CLIENT arm of the close-out chain
// (task_finish_cleanup.go: taskFinishCleanupChain → runFinishCleanupChainTarget),
// and each has a test here that answers it with PHYSICAL truth — a remote ref, a
// commit ancestry, a directory on disk — never a report alone:
//
//	① CHECKOUT INDEPENDENCE — the push argv carries the DELIVERABLE branch and
//	   the config-resolved peer, so a worktree left standing on some temporary
//	   branch (or on a detached HEAD) still delivers the right ref, and nothing
//	   about the push depends on what is checked out.
//	② SKEW IS NOT THE CLIENT'S TO RESOLVE — a branch BEHIND the peer, and a
//	   branch that would CONFLICT with it, both still reach the peer. The client
//	   used to fetch the peer's master, merge it into the branch locally and
//	   withhold the push when that failed (the trial merge); measured
//	   2026-10-12, that withheld three finished branches in one batch while
//	   hand-pushing the same three took about a minute each. Merging, and the
//	   verdict on the composed tree, are the merger's own acts on the
//	   integration branch, so what is asserted here is the STATE AFTER THE
//	   CHAIN: the ref arrived, carrying the branch's own commits.
//	③ DIRTY / GONE GROUND STILL DELIVERS — a worktree carrying residue, and a
//	   worktree whose directory was already removed, both still hand the branch
//	   over; the removal is refused with its own reason and the directory (when
//	   it exists) survives. The push is deliberately reachable from both.
//
// Every fixture is a real two-node git world (the finishCleanupFixture: a bare
// "server" + a client repo + a linked worktree), for the same reason the
// close-out suites use one: every question here is about git's own semantics.
import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// pushContractGit runs one git command with a deterministic identity and no
// ambient configuration, at a directory the caller names.
func pushContractGit(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t", "GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t",
		"GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_SYSTEM=/dev/null")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v in %s: %v: %s", args, dir, err, out)
	}
	return strings.TrimSpace(string(out))
}

// pushContractGitErr is the failure-tolerant twin (a non-zero exit IS the
// observation when the question is "does this ref exist?").
func pushContractGitErr(dir string, args ...string) error {
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t", "GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t",
		"GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_SYSTEM=/dev/null")
	return cmd.Run()
}

// --- ① the push argv does not depend on the checked-out branch ---

// TestClientPushArgvIgnoresCheckedOutBranch is the epic's question ①, answered
// as a measurement: the delivered ref is the DELIVERABLE's branch, and moving
// the worktree's HEAD somewhere else (a temporary branch, the shape a
// development session leaves behind — "worker was testing in a tmp branch and
// forgot to check out the deliverable branch") changes nothing about where the
// commit lands.
//
// The negative half is the point: the SAME fixture with the worktree left on a
// tmp branch pushes feat/… and NOT the tmp branch, and the tmp branch stays
// local — so a reader can see the property is not merely "a push happened".
func TestClientPushArgvIgnoresCheckedOutBranch(t *testing.T) {
	f := newFinishCleanupFixture(t)
	tmpBranch := "tmp/experiment"
	pushContractGit(t, f.wtDir, "checkout", "-b", tmpBranch)
	deliverableTip := pushContractGit(t, f.wtDir, "rev-parse", "HEAD") // still the deliverable's tip

	rep := f.runChain()
	assertStep(t, rep, finishCleanupPushVerb, true)

	// The deliverable ref is on the peer, at exactly the tip the branch had.
	got := pushContractGit(t, f.serverDir, "rev-parse", "refs/heads/"+f.featBranch)
	if got != deliverableTip {
		t.Errorf("the peer's %s is at %s, want the deliverable tip %s", f.featBranch, got, deliverableTip)
	}
	// The branch the worktree happened to be on was NOT delivered.
	if pushContractGitErr(f.serverDir, "rev-parse", "--verify", "--quiet", "refs/heads/"+tmpBranch) == nil {
		t.Errorf("the worktree's temporary branch %s reached the peer — the push is following HEAD", tmpBranch)
	}
}

// TestClientPushArgvSurvivesDetachedHead is the same property at its extreme: a
// worktree on a DETACHED HEAD. The push still names the deliverable branch, so
// the delivery does not depend on there being a branch checked out at all.
func TestClientPushArgvSurvivesDetachedHead(t *testing.T) {
	f := newFinishCleanupFixture(t)
	tip := pushContractGit(t, f.wtDir, "rev-parse", "HEAD")
	pushContractGit(t, f.wtDir, "checkout", "--detach", tip)

	rep := f.runChain()
	assertStep(t, rep, finishCleanupPushVerb, true)
	if got := pushContractGit(t, f.serverDir, "rev-parse", "refs/heads/"+f.featBranch); got != tip {
		t.Errorf("a detached HEAD cost the delivery: peer has %s, want %s", got, tip)
	}
}

// TestClientPushUsesThePeerRemoteNotAPath is the other half of question ①: the
// remote is whatever the CONFIG resolves to (a peer alias is the production
// shape), never the local directory a test happens to know about. The fixture's
// chain is driven through the same injection seam the production resolver uses,
// and the assertion is that the delivered ref appeared at the resolved target
// and NOWHERE else — a second, unrelated bare repo stands in as the control.
func TestClientPushUsesThePeerRemoteNotAPath(t *testing.T) {
	f := newFinishCleanupFixture(t)
	control := filepath.Join(filepath.Dir(f.serverDir), "not-the-peer.git")
	pushContractGit(t, filepath.Dir(control), "init", "--bare", control)

	rep := f.runChain()
	assertStep(t, rep, finishCleanupPushVerb, true)
	if pushContractGitErr(f.serverDir, "rev-parse", "--verify", "--quiet", "refs/heads/"+f.featBranch) != nil {
		t.Errorf("the resolved target did not receive the branch")
	}
	if pushContractGitErr(control, "rev-parse", "--verify", "--quiet", "refs/heads/"+f.featBranch) == nil {
		t.Errorf("a repo that is NOT the resolved peer received the branch — the target is not the resolver's answer")
	}
}

// --- ② the branch is aligned with the peer BEFORE it is pushed ---

// TestClientPushDeliversABranchBehindThePeer is the epic's question ②, answered
// under the CURRENT contract: a branch that is BEHIND the peer reaches it
// anyway, carrying its own tip. The client no longer merges the peer's master
// into the branch before handing it over, so the assertion is NOT ancestry —
// asserting that would re-assert the very gate this change removed — but the
// physical fact the merger needs: the ref arrived, at the branch's own revision,
// and the local ground was released.
func TestClientPushDeliversABranchBehindThePeer(t *testing.T) {
	f := newFinishCleanupFixture(t)
	deliverableTip := pushContractGit(t, f.wtDir, "rev-parse", "HEAD")
	f.advanceServerMaster(t, "server-only.txt", "peer moved ahead\n", "peer: unrelated change")

	rep := f.runChain()
	assertStep(t, rep, finishCleanupPushVerb, true)

	got := pushContractGit(t, f.serverDir, "rev-parse", "refs/heads/"+f.featBranch)
	if got != deliverableTip {
		t.Errorf("the peer holds %s, want the branch's own tip %s — a behind branch must still be delivered", got, deliverableTip)
	}
	// The branch's own commit is what arrived: an artificial merge commit would
	// show up as a different tip (and would be the client performing the
	// server's merge again).
	if n := countCommits(t, f.serverDir, f.featBranch); n != countCommits(t, f.clientDir, deliverableTip) {
		t.Errorf("the delivered ref carries %d commits, the branch had %d — the client composed the history", n, countCommits(t, f.clientDir, deliverableTip))
	}
	// The peer's tip is NOT an ancestor: the skew is real and it is the merger's
	// to resolve. (This is the assertion that inverts the removed gate: it used
	// to be required here, and requiring it is exactly what withheld deliveries.)
	if err := pushContractGitErr(f.serverDir, "merge-base", "--is-ancestor", masterBranchName, f.featBranch); err == nil {
		t.Logf("note: the peer's master happens to be an ancestor already; the assertion below is the load-bearing one")
	}
	if pushContractGitErr(f.serverDir, "rev-parse", "--verify", "--quiet", "refs/heads/"+f.featBranch) != nil {
		t.Errorf("the delivered ref is not on the peer")
	}
}

// TestClientPushDeliversABranchThatConflictsWithThePeer is epic acceptance (a)
// driven through the WHOLE close-out chain: the peer's master changed the very
// file the branch's deliverable touches, so a trial merge would have collided —
// and the branch is delivered anyway.
//
// This is the measurement that removed the trial-merge lane (2026-10-12): three
// finished branches were withheld by it in one batch (01a1301b by an anchor
// outside its own diff; 01a13019 and 01a1300b after 834-838 s with no FAIL line
// anywhere), while hand-pushing the same three took about a minute each and all
// three merged cleanly. A finished branch that reaches nobody is the worse
// failure, and the conflict is the merger's to adjudicate with the author's
// commits in hand.
func TestClientPushDeliversABranchThatConflictsWithThePeer(t *testing.T) {
	f := newFinishCleanupFixture(t)
	deliverableTip := pushContractGit(t, f.wtDir, "rev-parse", "HEAD")
	// The peer changes the SAME file the branch's deliverable touches.
	f.advanceServerMaster(t, "b.txt", "peer's own version\n", "peer: collides with the deliverable")

	rep := f.runChain()
	assertStep(t, rep, finishCleanupPushVerb, true)

	got := pushContractGit(t, f.serverDir, "rev-parse", "refs/heads/"+f.featBranch)
	if got != deliverableTip {
		t.Fatalf("a conflicting branch was NOT delivered: peer holds %q, want %q", got, deliverableTip)
	}
	// The branch's own file — the one that collides — is intact in what arrived:
	// nothing merged into it, so no conflict marker can be in the delivered
	// revision.
	blob := pushContractGit(t, f.serverDir, "cat-file", "-p", f.featBranch+":b.txt")
	if !strings.Contains(blob, "deliverable") {
		t.Errorf("the delivered revision lost the branch's own content: %q", blob)
	}
	if strings.Contains(blob, "<<<<<<<") {
		t.Errorf("the delivered revision carries conflict markers — something merged in it: %q", blob)
	}
	// The chain ran to its end: the branch is at the peer and the local ref is
	// gone (step ④ ran). The delivery is asserted from the peer's own object
	// store, which is the only place a delivery is a fact (Rule 31).
	if pushContractGitErr(f.clientDir, "rev-parse", "--verify", "--quiet", "refs/heads/"+f.featBranch) == nil {
		t.Errorf("the local ref survived a completed delivery — the chain stopped before step ④")
	}
}

// --- ③ a dirty or already-gone worktree still delivers ---

// TestClientDeliverySurvivesGoneWorktree is the epic's question ③ in its
// hardest form, and the live shape it comes from: the row records a worktree
// whose directory is ALREADY GONE (removed by hand, or by an earlier pass),
// while the branch's commits exist nowhere but this host. Before the reordering
// the chain stopped at `git status` — "chdir …: no such file or directory" —
// and the branch was never handed over (devdoc/hp-push-diagnosis-20260916.md
// finding A′).
func TestClientDeliverySurvivesGoneWorktree(t *testing.T) {
	f := newFinishCleanupFixture(t)
	tip := pushContractGit(t, f.wtDir, "rev-parse", "HEAD")
	if err := os.RemoveAll(f.wtDir); err != nil {
		t.Fatalf("remove worktree: %v", err)
	}
	pushContractGit(t, f.clientDir, "worktree", "prune")

	rep, err := runFinishCleanupChainTarget(context.Background(),
		&FinishCleanupReport{TaskID: "test", Repo: f.clientDir},
		taskFinishDeliverable{Repo: f.clientDir, Branch: f.featBranch, Worktree: f.wtDir}, f.serverDir, false)
	if err != nil {
		t.Fatalf("chain: %v", err)
	}
	// The delivery happened, and the peer holds the tip the gone directory had.
	got := pushContractGit(t, f.serverDir, "rev-parse", "refs/heads/"+f.featBranch)
	if got != tip {
		t.Fatalf("a gone worktree cost the delivery: peer has %q, want %q", got, tip)
	}
	// The removal is refused by NAME (nothing to remove), and the operator
	// learns the directory was not collected.
	if !rep.Skipped || !strings.Contains(rep.SkipReason, finishCleanupGoneWorktreeSkipMarker) {
		t.Errorf("the refusal must name the missing directory: %q", rep.SkipReason)
	}
	if !finishCleanupReportSettled(rep) {
		t.Errorf("a delivered row whose directory is gone must read settled: %q", rep.SkipReason)
	}
}

// TestClientDeliverySurvivesDirtyWorktreeWithTrackedWork guards the reach
// change at its most delicate point: the worktree holds a MODIFIED TRACKED file
// — real, committable work — and it must keep that work AND still deliver the
// branch's commits.
//
// The second assertion is the one a naive implementation fails: the delivery
// gate runs a trial merge in that very worktree, and its unwind used to be a
// `reset --hard` (sound only while the chain gated the push on a clean tree).
// Here the local edit must be in the file afterwards, byte for byte.
func TestClientDeliverySurvivesDirtyWorktreeWithTrackedWork(t *testing.T) {
	f := newFinishCleanupFixture(t)
	const localWork = "the author was still editing this\n"
	if err := os.WriteFile(filepath.Join(f.wtDir, "b.txt"), []byte(localWork), 0o644); err != nil {
		t.Fatal(err)
	}
	tip := pushContractGit(t, f.wtDir, "rev-parse", "HEAD")

	rep := f.runChain()
	assertStep(t, rep, finishCleanupPushVerb, true)
	if got := pushContractGit(t, f.serverDir, "rev-parse", "refs/heads/"+f.featBranch); got != tip {
		t.Errorf("dirty ground cost the delivery: peer has %q, want %q", got, tip)
	}
	// The local edit survived — the chain discarded nothing.
	body, err := os.ReadFile(filepath.Join(f.wtDir, "b.txt"))
	if err != nil {
		t.Fatalf("read the edited file: %v", err)
	}
	if string(body) != localWork {
		t.Errorf("the worktree's uncommitted work was lost: %q", body)
	}
	if !strings.Contains(rep.SkipReason, finishCleanupDirtyTrackedSkipMarker) {
		t.Errorf("the refusal must name tracked changes: %q", rep.SkipReason)
	}
	if _, err := os.Stat(f.wtDir); err != nil {
		t.Errorf("the dirty directory must survive: %v", err)
	}
}

// TestClientDeliverySurvivesDirtyWorktreeAgainstAdvancedPeer runs BOTH halves
// at once — residue in the worktree AND a peer that moved ahead — because that
// is the ordinary HP shape (a finished run with scratch residue, handing over
// to a server that kept merging). The delivery carries the branch's own tip: the
// peer's advance is the merger's to absorb, not a reason to withhold.
func TestClientDeliverySurvivesDirtyWorktreeAgainstAdvancedPeer(t *testing.T) {
	f := newFinishCleanupFixture(t)
	if err := os.WriteFile(filepath.Join(f.wtDir, "residue.tmp"), []byte("scratch\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	deliverableTip := pushContractGit(t, f.wtDir, "rev-parse", "HEAD")
	f.advanceServerMaster(t, "server-2.txt", "peer moved on\n", "peer: advanced")

	rep := f.runChain()
	assertStep(t, rep, finishCleanupPushVerb, true)
	if got := pushContractGit(t, f.serverDir, "rev-parse", "refs/heads/"+f.featBranch); got != deliverableTip {
		t.Errorf("the peer holds %q, want the branch's own tip %q", got, deliverableTip)
	}
	if _, err := os.Stat(f.wtDir); err == nil {
		t.Errorf("the residue ground must be reclaimed once its residue is retrievable")
	}
	salvage := lastStepNamed(t, rep, finishCleanupSalvageStep)
	if !salvage.OK || !strings.Contains(salvage.Detail, finishCleanupSalvageRefPrefix) {
		t.Errorf("the residue must be rescued by name before the ground goes: %s", finishCleanupReportLine(rep))
	}
}

// TestClientDeliveryRefusalReachesTheRowTrail pins B2 of
// devdoc/requirements/merge-and-branches.md, closed by this epic: the refusal
// that leaves a worktree behind is recorded on the ROW's trail, not only on
// stderr — "the one condition that strands a delivery is the one that leaves no
// trace beyond a stderr line". The event name is the operator's grep, so the
// assertion is on the name that lands in task_events.
//
// NARROWED 2026-10-07 (epic 01a11548): the untracked shape no longer strands the
// ground — its residue is RESCUED and the ground reclaimed — so the trail this
// test guards is now the RESCUE's record. The property B2 states is unchanged
// and still asserted: whatever the close-out does with a dirty ground, it says
// so on the ROW's trail, not only on stderr, and the delivery happens anyway.
func TestClientDeliveryRefusalReachesTheRowTrail(t *testing.T) {
	f := newFinishCleanupFixture(t)
	if err := os.WriteFile(filepath.Join(f.wtDir, "residue.tmp"), []byte("scratch\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	rep := f.runChain()
	// The trail row the operator greps for is the RESCUE (the untracked shape's
	// own record) — it names the ref, so the residue is retrievable by anyone
	// reading the ledger alone.
	rows := readTaskEventDetails(t, rep.TaskID, finishCleanupSalvageEvent)
	if len(rows) == 0 {
		t.Fatalf("the close-out left NO %q row on the trail (B2 is open again)", finishCleanupSalvageEvent)
	}
	last := rows[len(rows)-1]
	if !strings.Contains(last, finishCleanupSalvageRefPrefix) || !strings.Contains(last, "residue.tmp") {
		t.Errorf("the recorded detail must name the retrievable ref AND the entry, got %q", last)
	}
	// The delivery still happened — the trail records what happened to the
	// GROUND, never a failed hand-off.
	if !serverHasRefRuns(t, f.serverDir, f.featBranch) {
		t.Errorf("the branch must be on the peer despite the recorded refusal")
	}
}

// TestClientDeliveryKeepsAGroundOfUncommittedTrackedWork pins the OTHER half of
// the asymmetry (epic 01a11548): a TRACKED uncommitted change is not residue to
// be collected — it is an undelivered obligation, so the directory SURVIVES and
// the refusal (with its own event) is what the trail records. A rescue that
// could carry it is not a licence to delete the tree it was written in.
func TestClientDeliveryKeepsAGroundOfUncommittedTrackedWork(t *testing.T) {
	f := newFinishCleanupFixture(t)
	// A TRACKED file, modified and not committed: the run has not delivered.
	if err := os.WriteFile(filepath.Join(f.wtDir, "tracked.txt"), []byte("wip\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	preMergeGit(t, f.wtDir, "add", "tracked.txt")
	preMergeGit(t, f.wtDir, "commit", "-q", "-m", "the tracked file")
	if err := os.WriteFile(filepath.Join(f.wtDir, "tracked.txt"), []byte("wip\nuncommitted edit\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	rep := f.runChain()
	if _, statErr := os.Stat(f.wtDir); statErr != nil {
		t.Fatalf("a ground holding uncommitted TRACKED work must survive the close-out (it is not residue): %v", statErr)
	}
	if !rep.Skipped {
		t.Fatalf("the removal must be refused for uncommitted tracked work, got %+v", rep)
	}
	if !strings.Contains(rep.SkipReason, finishCleanupDirtyTrackedPrefix) {
		t.Errorf("the refusal must name the TRACKED shape, got %q", rep.SkipReason)
	}
	// And it is recorded on the row's trail, not only stderr (B2).
	rows := readTaskEventDetails(t, rep.TaskID, finishCleanupRemovalRefusedEvent)
	if len(rows) == 0 {
		t.Fatalf("the tracked-work refusal left NO %q row on the trail", finishCleanupRemovalRefusedEvent)
	}
	// The delivery still happened — the ground was kept, the work was not lost.
	if !serverHasRefRuns(t, f.serverDir, f.featBranch) {
		t.Errorf("the branch must still reach the peer")
	}
}

// TestClientDeliveryRemovalNotReachedReachesTheRowTrail is the same pin on the
// FAILED-delivery path: rep.Skipped is not set there, so the removal reason
// would otherwise exist only inside the report object.
func TestClientDeliveryRemovalNotReachedReachesTheRowTrail(t *testing.T) {
	f := newFinishCleanupFixture(t)
	if err := os.WriteFile(filepath.Join(f.wtDir, "residue.tmp"), []byte("scratch\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	// A peer branch of the same name that this host's branch cannot
	// fast-forward onto → the push is rejected.
	clone := f.serverDir + "-diverge2"
	pushContractGit(t, filepath.Dir(f.serverDir), "clone", f.serverDir, clone)
	// A CONTENT CONFLICT: the peer's copy rewrites the very file our branch
	// adds. A change to a file only one side touched merges cleanly, and the
	// delivery loop (task_delivery_loop.go) then legitimately ALIGNS and
	// delivers — so such a fixture no longer pins "the push is refused", it
	// pins nothing.
	if err := os.WriteFile(filepath.Join(clone, "b.txt"), []byte("peer took b.txt differently    \n"), 0o644); err != nil {
		t.Fatal(err)
	}
	pushContractGit(t, clone, "add", "b.txt")
	pushContractGit(t, clone, "commit", "-m", "diverge")
	pushContractGit(t, clone, "push", "origin", "HEAD:refs/heads/"+f.featBranch)

	rep := f.runChain()
	if !strings.Contains(rep.SkipReason, finishCleanupRemovalNotReachedNote) {
		t.Fatalf("fixture: expected the removal-not-reached sentence, got %q", rep.SkipReason)
	}
	if rows := readTaskEventDetails(t, rep.TaskID, finishCleanupRemovalRefusedEvent); len(rows) == 0 {
		t.Errorf("a failed delivery left the surviving worktree unrecorded on the trail")
	}
}

// readTaskEventDetails reads the detail column of one event name for a task.
func readTaskEventDetails(t *testing.T, taskID, event string) []string {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), finishCleanupDBTimeout)
	defer cancel()
	db, err := openTasksDB(ctx, memoryDBPath())
	if err != nil {
		t.Fatalf("open ledger: %v", err)
	}
	defer db.Close()
	rows, err := db.QueryContext(ctx, `SELECT detail FROM task_events WHERE task_id = ? AND event = ? ORDER BY id`, taskIDNormalize(taskID), event)
	if err != nil {
		t.Fatalf("query task_events: %v", err)
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var d string
		if err := rows.Scan(&d); err != nil {
			t.Fatalf("scan detail: %v", err)
		}
		out = append(out, d)
	}
	return out
}

// --- the delivery's own reporting (Rule 15) ---

// TestClientDeliveryFailureNamesTheUncollectedWorktree pins the report contract
// on the path where the delivery FAILS: rep.Skipped is not set there (the
// report carries a push step, so a caller reads it as progress), and a removal
// reason discovered before the push would otherwise vanish — leaving an
// operator with a failed push and no word about the directory that is still
// there.
func TestClientDeliveryFailureNamesTheUncollectedWorktree(t *testing.T) {
	f := newFinishCleanupFixture(t)
	if err := os.WriteFile(filepath.Join(f.wtDir, "residue.tmp"), []byte("scratch\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	// Diverge the peer's same-named branch so the push is rejected.
	clone := f.serverDir + "-diverge"
	pushContractGit(t, filepath.Dir(f.serverDir), "clone", f.serverDir, clone)
	// A CONTENT CONFLICT: the peer's copy rewrites the very file our branch
	// adds. A change to a file only one side touched merges cleanly, and the
	// delivery loop (task_delivery_loop.go) then legitimately ALIGNS and
	// delivers — so such a fixture no longer pins "the push is refused", it
	// pins nothing.
	if err := os.WriteFile(filepath.Join(clone, "b.txt"), []byte("peer took b.txt differently    \n"), 0o644); err != nil {
		t.Fatal(err)
	}
	pushContractGit(t, clone, "add", "b.txt")
	pushContractGit(t, clone, "commit", "-m", "diverge")
	pushContractGit(t, clone, "push", "origin", "HEAD:refs/heads/"+f.featBranch)

	rep := f.runChain()
	for _, s := range rep.Steps {
		if s.Step == finishCleanupPushVerb && s.OK {
			t.Fatalf("fixture: the push was expected to fail: %+v", rep.Steps)
		}
	}
	if !strings.Contains(rep.SkipReason, finishCleanupRemovalNotReachedNote) {
		t.Errorf("a failed delivery must still say the worktree was not collected: %q", rep.SkipReason)
	}
}

// --- ④ the shape with NO worktree at all: the branch is still reaped ---

// TestClientDeliveryWithNoWorktreeStillDeletesTheBranch is the 2026-09-26 HP
// regression, and it is the SECOND leg of the ticket's question ("pushed but not
// deleted").
//
// MEASURED on the client host: `gopi task finish-cleanup --dry-run` reported
// `candidates=10 would-run=0 settled=0 refused=10` for days — the close-out
// never reached the delivery, so the branches stayed. That is leg A. Leg B is
// this one and it is independent of leg A: a WT-LESS row (the Rule-6a executor
// shape — the row records no deliverable.worktree, so `wt` resolves to "" and
// `rep.Worktree` is empty) reached ④ only through a return that treated "no
// worktree was collected" as "the pair must survive". With no worktree in the
// chain at all there is no pair, and the two rows it was measured on
// (task/01a0d9ab7f5a75f49be4f842fa1449cf, task/01a0d9ab911872e3ad166017e2104c2e)
// kept their refs after their branches had reached the peer.
//
// What ④ must NOT lose: standing. A chain that pushed nothing still refuses the
// force-delete (axis 2), and the orphan guard consults the delivery point — both
// are asserted here, because a fix that deleted the ref unconditionally would be
// the 2026-08/2026-09-17 incident again.
func TestClientDeliveryWithNoWorktreeStillDeletesTheBranch(t *testing.T) {
	f := newFinishCleanupFixture(t)
	tip := pushContractGit(t, f.wtDir, "rev-parse", "HEAD")
	// THE DIRECTORY IS ALREADY GONE — the state the two real rows were in when
	// the sweep looked at them (their grounds had been collected by an earlier
	// pass; only the refs survived).
	if err := os.RemoveAll(f.wtDir); err != nil {
		t.Fatalf("remove worktree: %v", err)
	}
	pushContractGit(t, f.clientDir, "worktree", "prune")
	// NO recorded worktree, and none resolvable: the repo argument is the MAIN
	// checkout, so finishCleanupOwningWorktree finds nothing to bind this row to
	// (the gone directory is no longer listed). The branch's own linked worktree
	// is therefore never consulted (that probe only runs for a row that DID
	// record a worktree).
	rep, err := runFinishCleanupChainTarget(context.Background(),
		&FinishCleanupReport{TaskID: "test-no-wt", Repo: f.clientDir},
		taskFinishDeliverable{Repo: f.clientDir, Branch: f.featBranch}, f.serverDir, false)
	if err != nil {
		t.Fatalf("chain: %v", err)
	}
	// ① THE DELIVERY — unchanged, and the first thing this chain does.
	if got := pushContractGit(t, f.serverDir, "rev-parse", "refs/heads/"+f.featBranch); got != tip {
		t.Fatalf("the peer must hold the tip: got %q, want %q", got, tip)
	}
	assertStep(t, rep, finishCleanupPushVerb, true)
	// ② THE DELETION — the leg this fix exists for. Without it the ref survives
	// on the client host forever (nothing re-runs a close-out).
	assertStep(t, rep, finishCleanupBranchVerb, true)
	if out := pushContractGit(t, f.clientDir, "branch", "--list", f.featBranch); strings.TrimSpace(out) != "" {
		t.Errorf("the local ref must be gone after a delivered close-out, got %q", out)
	}
	// ③ NO WORKTREE EXISTED, and that is RECORDED rather than left as a silent
	// gap in the step list (an operator must be able to tell "the sweep removed
	// the directory" from "this chain never had one").
	if rep.Skipped {
		t.Errorf("a fully delivered wt-less row is not a refusal, got %+v", rep)
	}
	var recorded bool
	for _, s := range rep.Steps {
		if s.Step == finishCleanupStepWorktree && strings.Contains(s.Detail, finishCleanupNoGroundStepDetail) {
			recorded = true
		}
	}
	if !recorded {
		t.Errorf("the absent worktree must be recorded as its own step, got %+v", rep.Steps)
	}
	// ④ NOTHING was removed by this chain: there was no directory in it, and the
	// git registration is gone (the chain does not prune on its own).
	if _, err := os.Stat(f.wtDir); !os.IsNotExist(err) {
		t.Errorf("a wt-less chain must remove nothing (and resurrect nothing): %v", err)
	}
}

// TestClientDeliveryKeepsABranchGitRefusesToDelete is the boundary of the fix
// above, stated so a later change cannot widen it: when the branch is STILL
// checked out somewhere, git refuses BOTH halves of ④ (`-d` and `-D`), the
// refusal is reported, and the ref SURVIVES. That is not a defect to route
// around — a checked-out branch is a directory's ground, which is exactly what
// the deletion guards exist to protect. The chain has no business deleting a
// directory it was not told about in order to reach the ref.
func TestClientDeliveryKeepsABranchGitRefusesToDelete(t *testing.T) {
	f := newFinishCleanupFixture(t)
	rep, err := runFinishCleanupChainTarget(context.Background(),
		&FinishCleanupReport{TaskID: "test-no-wt-held", Repo: f.clientDir},
		taskFinishDeliverable{Repo: f.clientDir, Branch: f.featBranch}, f.serverDir, false)
	if err != nil {
		t.Fatalf("chain: %v", err)
	}
	// The delivery still happened — the push is not gated by the removal side.
	assertStep(t, rep, finishCleanupPushVerb, true)
	// And the ref is kept, with the reason on the step (Rule 15: reported, never
	// a silent no-op).
	assertNoStepRan(t, rep, finishCleanupBranchVerb)
	if out := pushContractGit(t, f.clientDir, "branch", "--list", f.featBranch); strings.TrimSpace(out) == "" {
		t.Errorf("a branch still checked out in a worktree must survive")
	}
	if _, err := os.Stat(f.wtDir); err != nil {
		t.Errorf("the directory holding the branch must be untouched: %v", err)
	}
}

// TestClientDeliveryWithNoWorktreeKeepsAnUndeliveredBranch is the STANDING half:
// the same wt-less shape, but the delivery FAILS (the peer is refused). The
// branch must survive — the pair rule's protection is now carried by ④'s own two
// axes, and this is the test that proves they still hold the ref.
func TestClientDeliveryWithNoWorktreeKeepsAnUndeliveredBranch(t *testing.T) {
	f := newFinishCleanupFixture(t)
	// A server side that REFUSES the push: a non-bare repo with the branch
	// checked out refuses a push to that ref (the receive.denyCurrentBranch
	// refusal, the ordinary shape). Same construction the pushback tests use.
	blocked := filepath.Join(t.TempDir(), "blocked")
	pushContractGit(t, f.clientDir, "init", blocked)
	pushContractGit(t, blocked, "config", "user.email", "t@t")
	pushContractGit(t, blocked, "config", "user.name", "t")
	pushContractGit(t, f.clientDir, "-C", blocked, "checkout", "-b", f.featBranch)

	rep, err := runFinishCleanupChainTarget(context.Background(),
		&FinishCleanupReport{TaskID: "test-no-wt-fail", Repo: f.clientDir},
		taskFinishDeliverable{Repo: f.clientDir, Branch: f.featBranch}, blocked, false)
	if err != nil {
		t.Fatalf("chain: %v", err)
	}
	if finishCleanupStepSucceeded(rep, finishCleanupPushVerb) {
		t.Fatalf("fixture: the push was supposed to be refused, got %+v", rep.Steps)
	}
	if out := pushContractGit(t, f.clientDir, "branch", "--list", f.featBranch); strings.TrimSpace(out) == "" {
		t.Errorf("an UNDELIVERED branch must survive a wt-less close-out")
	}
}

// TestClientDeliveryWithGoneRecordedWorktreeStillDeletesTheBranch is the LIVE
// shape of the 2026-09-26 HP defect, in the form the real ledger held it: the
// row RECORDS a worktree, the directory is gone from disk (collected by an
// earlier pass), and the branch ref is still here.
//
// The chain's reply USED TO read "removal skipped (" + retired claim + ")" —
// the whole point of this epic: the wording is gone, and the sentence below
// quotes it only as history.
// and a return — before ④. That is why this host's task/<id> refs outlived the
// work they name: the host's ten candidates were ALL in this state
// (`settled=7 refused=3`, each "the recorded worktree directory is not there"),
// and the delivery is a single shot per row — nothing re-runs it (Rule 6d). The
// removal half is genuinely finished here (there is no directory to keep alive),
// so the ref is the only thing this chain can still hand back.
//
// The standing is unchanged and is asserted: the delivery must have happened
// FIRST (a gone directory must not cost the push), and ④'s own guards still hold
// the ref when the push did not land.
func TestClientDeliveryWithGoneRecordedWorktreeStillDeletesTheBranch(t *testing.T) {
	f := newFinishCleanupFixture(t)
	tip := pushContractGit(t, f.wtDir, "rev-parse", "HEAD")
	// The directory is gone; the LEDGER still records it (that is the whole
	// shape: the row's deliverable.worktree names a path that no longer exists).
	if err := os.RemoveAll(f.wtDir); err != nil {
		t.Fatalf("remove worktree: %v", err)
	}
	pushContractGit(t, f.clientDir, "worktree", "prune")

	rep, err := runFinishCleanupChainTarget(context.Background(),
		&FinishCleanupReport{TaskID: "test-gone-wt", Repo: f.clientDir},
		taskFinishDeliverable{Repo: f.clientDir, Branch: f.featBranch, Worktree: f.wtDir}, f.serverDir, false)
	if err != nil {
		t.Fatalf("chain: %v", err)
	}
	// ① THE DELIVERY (the epic's own guarantee, re-asserted beside the new leg).
	if got := pushContractGit(t, f.serverDir, "rev-parse", "refs/heads/"+f.featBranch); got != tip {
		t.Fatalf("a gone worktree cost the delivery: peer has %q, want %q", got, tip)
	}
	assertStep(t, rep, finishCleanupPushVerb, true)
	// ② THE DELETION — the leg the live defect needed. The removal refusal is
	// still RECORDED (Rule 15: the operator learns the directory was not
	// collected) ...
	if !rep.Skipped || !strings.Contains(rep.SkipReason, finishCleanupGoneWorktreeSkipMarker) {
		t.Errorf("the removal refusal must still be reported: %q", rep.SkipReason)
	}
	if !finishCleanupReportSettled(rep) {
		t.Errorf("a delivered row whose directory is gone must read settled: %q", rep.SkipReason)
	}
	// ... and it no longer costs the ref.
	assertStep(t, rep, finishCleanupBranchVerb, true)
	if out := pushContractGit(t, f.clientDir, "branch", "--list", f.featBranch); strings.TrimSpace(out) != "" {
		t.Errorf("the local ref must be gone after a delivered close-out, got %q", out)
	}
}
