package tools

// THE PERMANENT REFUSAL MUST NAME THE WORK IT LEAVES STRANDED.
//
// THE DEFECT THESE TESTS PIN (epic: failed row, stranded commit), measured on
// this host 2026-11-01 against row 01a16043-2b43-7590-a050-3418abd608ef:
//
//	status      failed   (zombie disposal — heartbeat stalled, holder pid dead)
//	verdict     reclaim-unreachable: "…refusing to remove a directory this row
//	            does not own (row status and directory left untouched; this pass
//	            will not report this row again)"
//	its tip     f2215b227ac3b9f279e169f6a128f7b56c16c8b4
//	peer master 2dd1140c… (tip NOT contained; `git cherry` → "+ f2215b227…")
//	PROJECT.md on master still carries Rule 36 (grep -c → 2)
//
// The arm refused CORRECTLY (Rule 42 — it is not its directory to remove) and
// then promised to be silent for ever about it. The commit that was the row's
// whole purpose sat in that directory, and the surfaces a reviewer reads did
// not name it.
//
// CORRECTION, measured rather than reasoned (this host, 2026-11-01). The first
// version of this comment said the ledger held NOTHING about the stranded work.
// That is false: `delivery-proof-owed` (task_delivery_proof.go) had already
// written the tip on 65 of the 82 undelivered grounds — 804 characters on row
// 01a16043 naming `f2215b227ac3` and both delivery points. The real gap is that
// the event is NOT a member of verdictTrailEventNames, so `task show` renders
// none of it, and the row's `verdict:` line shows whatever verdict event came
// later. The record existed and could not be read; 8 rows had no record at all.
//
// So the contract below is the half that is genuinely open — the arm's own
// record, on the trail, and RENDERED — and it does not lean on the false
// "nothing was recorded" premise. The render gap for the pre-existing event is
// a separate decision (it would move the verdict line for 63 rows) and is filed
// as its own finding rather than smuggled in here.
//
// THE CONTRACT PINNED HERE (four halves):
//
//	① a CLONE ground that holds undelivered commits gets a trail record naming
//	  the ground, the branch, the count, the delivery points and the commit ids —
//	  written BESIDE the reclaim-unreachable marker, on the same append-only trail,
//	  so `task show` renders it without running anything;
//	② a MEASURED zero is recorded as its own named state (not omitted), so
//	  "measured, nothing stranded" can never be confused with "never asked";
//	③ an UNMEASURABLE probe is recorded as unmeasurable with its reason, and is
//	  never rendered as either verdict (Rule 15a);
//	④ the record moves NOTHING: the row's status is unchanged and neither
//	  directory is created, removed or written through (Rule 42).
//
// The measurement itself is NOT re-implemented here: the record asks
// worktreeUndeliveredCommits — the probe the undelivered-commits rung already
// calls — so these tests also catch the two drifting apart.

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nickhuang99/gopi/internal/devworkspace"
)

// reclaimStrandedEvents returns the `reclaim-stranded-commits` trail entries
// recorded for taskID — read back from the ledger, so each test measures what a
// READER sees rather than what the writer believes it wrote.
func reclaimStrandedEvents(t *testing.T, taskID string) []string {
	t.Helper()
	db := openTestTasksDB(t, memoryDBPath())
	defer db.Close()
	rows, err := db.QueryContext(context.Background(),
		`SELECT detail FROM task_events WHERE task_id = ? AND event = ? ORDER BY id`,
		taskID, reclaimStrandedCommitsEvent)
	if err != nil {
		t.Fatalf("read %s events for %s: %v", reclaimStrandedCommitsEvent, taskID, err)
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var detail string
		if err := rows.Scan(&detail); err != nil {
			t.Fatalf("scan %s event for %s: %v", reclaimStrandedCommitsEvent, taskID, err)
		}
		out = append(out, detail)
	}
	return out
}

// reclaimStrandedFixture builds the real shape at the smallest sample: a clone
// ground of a repository, with ONE commit the clone holds and the "delivery
// point" (the parent's `origin/master`) does not. Returns the parent repo, the
// clone ground, the branch and the stranded commit id.
//
// The clone is created by `git clone` and then committed into — the shape the
// copy channel produced on this host. The stranded commit is a REAL commit with
// a real diff, so the equivalence legs have something to decline, exactly as
// they do for a genuine stranded row.
func reclaimStrandedFixture(t *testing.T) (repo, clone, branch, stranded string) {
	t.Helper()
	root := t.TempDir()
	repo = filepath.Join(root, "repo")
	clone = filepath.Join(root, "clone-ground")
	branch = IntegrationBranchName
	gitRun(t, root, "init", repo)
	gitRun(t, repo, "config", "user.email", "t@t")
	gitRun(t, repo, "config", "user.name", "t")
	gitRun(t, repo, "checkout", "-b", branch)
	if err := os.WriteFile(filepath.Join(repo, "a.txt"), []byte("base\n"), 0o644); err != nil {
		t.Fatalf("write base: %v", err)
	}
	gitRun(t, repo, "add", "a.txt")
	gitRun(t, repo, "commit", "-m", "init")
	gitRun(t, root, "clone", repo, clone)

	// The stranded commit: written in the CLONE, never pushed. Its diff is a
	// deletion so a content-containment leg cannot mistake it for base content.
	if err := os.WriteFile(filepath.Join(clone, "stranded.txt"), []byte("work nobody has\n"), 0o644); err != nil {
		t.Fatalf("write stranded: %v", err)
	}
	gitRun(t, clone, "add", "stranded.txt")
	gitRun(t, clone, "commit", "-m", "the run's only deliverable")
	stranded = strings.TrimSpace(gitOut(t, clone, "rev-parse", "HEAD"))

	// PRECONDITIONS, measured rather than assumed — this is the shape the whole
	// defect rests on: the ground is a CLONE, and the parent repo does NOT
	// contain the commit while the clone does.
	if got := devworkspace.GroundModelOf(clone); got != devworkspace.GroundModelClone {
		t.Fatalf("%s must be a CLONE ground (.git is a directory), measured %q", clone, got)
	}
	if p := worktreeUndeliveredCommits(clone, branch); !p.Measured || p.Ahead != 1 {
		t.Fatalf("precondition: the ground must hold exactly ONE undelivered commit, measured %+v", p)
	}
	if _, err := os.Stat(filepath.Join(repo, "stranded.txt")); !os.IsNotExist(err) {
		t.Fatalf("precondition: the parent repo must NOT have the stranded file (err=%v)", err)
	}
	return repo, clone, branch, stranded
}

// TestReclaimStrandedCommitsAreNamedOnTheRow is ①: the row that refuses a clone
// ground holding work must carry that fact on its own trail — the count, the
// branch, the delivery points and the commit id — so a reader of the LEDGER
// learns it without running a sweep, and without the directory being the only
// witness.
func TestReclaimStrandedCommitsAreNamedOnTheRow(t *testing.T) {
	freshReclaimRefusalMemo(t)
	fossilLedgerEnv(t)
	repo, clone, branch, stranded := reclaimStrandedFixture(t)

	const id = "t-stranded-named"
	if _, err := TaskSubmit(TaskSubmitOptions{Summary: "stranded", Payload: "p", TaskID: id, Source: "human"}); err != nil {
		t.Fatalf("submit: %v", err)
	}
	// A LIVE-status row: the terminal-row shape leaves the report set before any
	// wording is chosen, so driving one would measure silence from the FIRST pass
	// rather than the record this test is about.
	seedDeadHolderRow(t, id, taskStatusInProgress, repo, branch, clone, hostPid(t, deadPID(t)))

	captureStderrDuring(t, func() { ReclaimDeadHolderWorktrees(true) })

	// The refusal itself is still recorded — this change ADDS a record, it does
	// not replace the marker whose convergence contract is already pinned.
	if n := len(reclaimMarkerEvents(t, id)); n != 1 {
		t.Fatalf("the reclaim-unreachable marker must still be exactly one row, got %d", n)
	}
	got := reclaimStrandedEvents(t, id)
	if len(got) != 1 {
		t.Fatalf("%s must carry exactly ONE %s record, got %d: %v",
			id, reclaimStrandedCommitsEvent, len(got), got)
	}
	body := got[0]
	for _, want := range []string{
		reclaimStrandedCommitsMarker, // the grep token an operator uses
		clone,                        // WHICH directory is holding the work
		branch,                       // which branch of it
		plural(1),                    // the measured count
		stranded,                     // the commit id itself
		refsRemotesPrefix + deliveryProofOriginRemote, // the delivery point it was measured against
		"the ground is KEPT",                          // the disposition: nothing will be removed
	} {
		if !strings.Contains(body, want) {
			t.Errorf("the record must name %q — a record a reader cannot act on is a tombstone:\n%s",
				want, body)
		}
	}
	// The refusal must not be rendered as delivery: the record says the commits
	// have NO destination, and says who will not push them.
	if strings.Contains(body, "clone-delivery-landed") {
		t.Errorf("a ground whose commits no delivery point has must never be recorded as landed:\n%s", body)
	}
	// The ground is still on disk — this arm records, it never removes (Rule 42).
	if _, err := os.Stat(clone); err != nil {
		t.Errorf("the clone ground must still exist: the record is a note, not a removal (%v)", err)
	}
	// And a SECOND pass writes nothing new: the marker's own gate
	// (reclaimUnreachableRecorded) is what makes this once-per-row.
	freshReclaimRefusalMemo(t)
	captureStderrDuring(t, func() { ReclaimDeadHolderWorktrees(true) })
	if n := len(reclaimStrandedEvents(t, id)); n != 1 {
		t.Fatalf("a later pass must not append a second record for an already-marked row, got %d", n)
	}
}

// TestReclaimStrandedCommitsMeasuredZeroIsNamed is ②: a clone ground with
// nothing pending gets a record too, worded as a MEASURED ZERO. Omitting it
// would make "measured, nothing stranded" indistinguishable from "the arm never
// asked" — the exact ambiguity that hid the defect this epic is about.
func TestReclaimStrandedCommitsMeasuredZeroIsNamed(t *testing.T) {
	freshReclaimRefusalMemo(t)
	fossilLedgerEnv(t)
	repo, clone, branch, stranded := reclaimStrandedFixture(t)

	// Remove the work from the ground's own branch: the ground remains a clone
	// with a clean branch, which is the state a successfully-delivered run
	// leaves behind.
	gitRun(t, clone, "reset", "--hard", "HEAD~1")
	if p := worktreeUndeliveredCommits(clone, branch); !p.Measured || p.Ahead != 0 {
		t.Fatalf("precondition: the ground must now measure ZERO undelivered commits, measured %+v", p)
	}
	if strings.Contains(strings.Join(worktreeUndeliveredCommits(clone, branch).Commits, ","), stranded) {
		t.Fatalf("precondition: the reset commit %s must no longer be pending", stranded)
	}

	const id = "t-stranded-zero"
	if _, err := TaskSubmit(TaskSubmitOptions{Summary: "zero", Payload: "p", TaskID: id, Source: "human"}); err != nil {
		t.Fatalf("submit: %v", err)
	}
	seedDeadHolderRow(t, id, taskStatusInProgress, repo, branch, clone, hostPid(t, deadPID(t)))

	captureStderrDuring(t, func() { ReclaimDeadHolderWorktrees(true) })

	got := reclaimStrandedEvents(t, id)
	if len(got) != 1 {
		t.Fatalf("a measured zero must still be RECORDED (so it is distinguishable from never-asked), got %d", len(got))
	}
	if !strings.Contains(got[0], reclaimStrandedNoneMarker) {
		t.Errorf("the zero record must spell %q, got %q", reclaimStrandedNoneMarker, got[0])
	}
	if strings.Contains(got[0], reclaimStrandedCommitsMarker) {
		t.Errorf("a zero measurement must never wear the stranding token %q — that is the "+
			"false-alarm direction of the same ambiguity:\n%s", reclaimStrandedCommitsMarker, got[0])
	}
	if !strings.Contains(got[0], "OWNERSHIP") {
		t.Errorf("the zero record must say the refusal was about ownership, not work:\n%s", got[0])
	}
}

// TestReclaimStrandedCommitsUnmeasurableIsNamed is ③: when the question cannot
// be answered the record SAYS SO, with the reason, and wears neither verdict.
// Rule 15a — a probe that failed is never "nothing is there", and never
// "commits are stranded".
//
// The shape driven is real and has its own known cause on this host: a clone
// whose remote-tracking refs were pruned has NO delivery point that resolves, so
// the count could not be taken. The probe reports that faithfully (Measured=true
// with an EMPTY Points list, which is why the record asks about Points and not
// only about Measured).
func TestReclaimStrandedCommitsUnmeasurableIsNamed(t *testing.T) {
	freshReclaimRefusalMemo(t)
	fossilLedgerEnv(t)
	repo, clone, branch, _ := reclaimStrandedFixture(t)

	gitRun(t, clone, "update-ref", "-d", refsRemotesPrefix+deliveryProofOriginRemote+"/"+branch)
	if p := worktreeUndeliveredCommits(clone, branch); len(p.Points) != 0 {
		t.Fatalf("precondition: with the remote ref pruned NO delivery point may resolve, got %v", p.Points)
	}

	const id = "t-stranded-unmeasurable"
	if _, err := TaskSubmit(TaskSubmitOptions{Summary: "unmeasurable", Payload: "p", TaskID: id, Source: "human"}); err != nil {
		t.Fatalf("submit: %v", err)
	}
	seedDeadHolderRow(t, id, taskStatusInProgress, repo, branch, clone, hostPid(t, deadPID(t)))

	captureStderrDuring(t, func() { ReclaimDeadHolderWorktrees(true) })

	got := reclaimStrandedEvents(t, id)
	if len(got) != 1 {
		t.Fatalf("an unmeasurable probe must be RECORDED (Rule 15 — a swallowed failure is the defect), got %d", len(got))
	}
	if !strings.Contains(got[0], reclaimStrandedUnmeasurableMarker) {
		t.Errorf("the record must spell %q, got %q", reclaimStrandedUnmeasurableMarker, got[0])
	}
	for _, banned := range []string{reclaimStrandedCommitsMarker, reclaimStrandedNoneMarker} {
		if strings.Contains(got[0], banned) {
			t.Errorf("an UNMEASURED question must never be rendered as %q — that is the "+
				"false-zero defect Rule 15a names:\n%s", banned, got[0])
		}
	}
	if !strings.Contains(got[0], "keeping the ground") {
		t.Errorf("an unmeasurable probe must say the ground is KEPT on that basis:\n%s", got[0])
	}
}

// TestReclaimStrandedCommitsNamesABranchTheGroundDoesNotHave pins the FOURTH
// state the fixtures exposed: the row names a branch the ground does not carry.
// It is neither stranding nor a clean zero, and the first implementation did
// render it as a clean zero — "ground holds no undelivered commit: branch
// no-such-branch of … holds nothing no delivery point has ()" — a measured-zero
// sentence with an EMPTY delivery-point list, i.e. a question never asked wearing
// the answer's clothes (Rule 15a). The states must stay distinguishable because
// the actions differ: an empty ground is collectable, a mismatched registration
// needs its deliverable block read.
func TestReclaimStrandedCommitsNamesABranchTheGroundDoesNotHave(t *testing.T) {
	freshReclaimRefusalMemo(t)
	fossilLedgerEnv(t)
	repo, clone, _, _ := reclaimStrandedFixture(t)
	const missingBranch = "no-such-branch"

	const id = "t-stranded-no-branch"
	if _, err := TaskSubmit(TaskSubmitOptions{Summary: "no-branch", Payload: "p", TaskID: id, Source: "human"}); err != nil {
		t.Fatalf("submit: %v", err)
	}
	seedDeadHolderRow(t, id, taskStatusInProgress, repo, missingBranch, clone, hostPid(t, deadPID(t)))

	captureStderrDuring(t, func() { ReclaimDeadHolderWorktrees(true) })

	got := reclaimStrandedEvents(t, id)
	if len(got) != 1 {
		t.Fatalf("a registration that names no existing branch must be RECORDED, got %d", len(got))
	}
	if !strings.Contains(got[0], reclaimStrandedBranchMissingMarker) {
		t.Errorf("the record must spell %q, got %q", reclaimStrandedBranchMissingMarker, got[0])
	}
	for _, banned := range []string{reclaimStrandedCommitsMarker, reclaimStrandedNoneMarker} {
		if strings.Contains(got[0], banned) {
			t.Errorf("a MISSING BRANCH must never be rendered as %q — the ground supports no claim "+
				"about work it does not carry:\n%s", banned, got[0])
		}
	}
	if !strings.Contains(got[0], missingBranch) {
		t.Errorf("the record must name the branch the row claims, so the mismatch is readable:\n%s", got[0])
	}
}

// TestReclaimStrandedCommitsSkipsALinkedWorktree is ④'s scope half: a LINKED
// worktree cannot strand work — `git worktree remove` drops a registration and
// the branch stays in the shared repo — so the record must not be written for
// that shape. Firing there would make the event's own name false.
//
// Measured through the ONE shape discriminator: the fixture asserts the shape it
// built before asserting the record's absence.
func TestReclaimStrandedCommitsSkipsALinkedWorktree(t *testing.T) {
	freshReclaimRefusalMemo(t)
	root := fossilLedgerEnv(t)

	repo := filepath.Join(root, "repo")
	linked := filepath.Join(root, "linked-wt")
	gitRun(t, root, "init", repo)
	gitRun(t, repo, "config", "user.email", "t@t")
	gitRun(t, repo, "config", "user.name", "t")
	gitRun(t, repo, "checkout", "-b", IntegrationBranchName)
	if err := os.WriteFile(filepath.Join(repo, "a.txt"), []byte("base\n"), 0o644); err != nil {
		t.Fatalf("write base: %v", err)
	}
	gitRun(t, repo, "add", "a.txt")
	gitRun(t, repo, "commit", "-m", "init")
	gitRun(t, repo, "worktree", "add", "-b", "feat/linked", linked)

	if got := devworkspace.GroundModelOf(linked); got != devworkspace.GroundModelLinkedWorktree {
		t.Fatalf("precondition: %s must be a LINKED worktree (.git is a file), measured %q", linked, got)
	}

	const id = "t-stranded-linked"
	if _, err := TaskSubmit(TaskSubmitOptions{Summary: "linked", Payload: "p", TaskID: id, Source: "human"}); err != nil {
		t.Fatalf("submit: %v", err)
	}
	seedDeadHolderRow(t, id, taskStatusInProgress, repo, "feat/linked", linked, hostPid(t, deadPID(t)))

	captureStderrDuring(t, func() { ReclaimDeadHolderWorktrees(true) })

	if got := reclaimStrandedEvents(t, id); len(got) != 0 {
		t.Fatalf("a LINKED worktree cannot strand commits (its branch lives in the shared repo), "+
			"so no %s record may be written — got %d: %v",
			reclaimStrandedCommitsEvent, len(got), got)
	}
}

// TestReclaimStrandedRecordIsRenderedByTaskShow is the half that makes ① worth
// anything: the event must be a member of verdictTrailEventNames, or the record
// is written into a table no surface renders and the operator is back to
// running a sweep to find it. Asserted against the ONE list, not a copy.
func TestReclaimStrandedRecordIsRenderedByTaskShow(t *testing.T) {
	found := false
	for _, name := range verdictTrailEventNames {
		if name == reclaimStrandedCommitsEvent {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("%q must be a member of verdictTrailEventNames (tasks.go) — a trail record no "+
			"surface renders is invisible to the reader this epic exists for", reclaimStrandedCommitsEvent)
	}
	// And it must not collide with a name that already means something else: the
	// whole point is a NEW observable fact, not a second spelling of an old one.
	for _, other := range []string{
		reclaimUnreachableEventName,
		DeliveryLandedEventName,
		DeliveryDidNotLandEventName,
		reclaimGroundOutcomeEvent,
	} {
		if other == reclaimStrandedCommitsEvent {
			t.Fatalf("%q must be its own event, not a reuse of %q", reclaimStrandedCommitsEvent, other)
		}
	}
}
