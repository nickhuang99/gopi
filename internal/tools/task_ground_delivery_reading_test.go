package tools

// task_ground_delivery_reading_test.go — the DELIVERY axis of `gopi task show`
// (epic 01a17e7b, half B), driven against REAL repositories: a main checkout, a
// real CLONE ground whose `.git` is a DIRECTORY, and a real bare repo standing
// behind the resolved peer alias.
//
// THE DEFECT (measured 2026-10-28, row 01a17df2-…, killed by a signal). The run
// finished its work in a clone ground; the delivery leg only runs from the run's
// OWN end hook, so it never ran, and nothing was pushed. Every surface rendered
// that row exactly like a run that did nothing — and hours later the clone and
// the run's session directory were collected by the sweep, so the tip
// (3af81a93…) can now be read in NO repo on this machine:
//
//	$ git -C /home/user/work/gopi cat-file -t 3af81a93271e80356c99b95263a125a808a27e67
//	fatal: git cat-file: could not get object info
//
// WHAT THESE TESTS PIN (the three directions the reading must never confuse):
//
//	(a) the tip exists in the clone ground and NOWHERE else and the peer does not
//	    advertise it ⇒ UNDELIVERED, naming the clone, the tip sha and the repo
//	    that does not hold it (the measured shape);
//	(b) the peer advertises the row's own tip ⇒ DELIVERED — the same tip, so the
//	    reading cannot be satisfied by a peer holding the NAME at another commit;
//	(c) nothing is anywhere ⇒ UNCHECKED, naming the places asked — never
//	    "the row has no work", which no observation here supports (Rule 15);
//	(d) a non-terminal row has no delivery question yet, so the line is silent —
//	    and the advisory line it sits beside is unchanged (no writer).
//
// Each arm drives the REAL `TaskShow` digest: the assertions read the rendered
// line, not the reading struct, so a render that drops the evidence fails here.

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/nickhuang99/gopi/internal/devworkspace"
)

// deliveryReadingSeedRow inserts a row in the given status carrying the given
// deliverable blob. It writes through the ledger opener directly because these
// arms need a TERMINAL (`failed`) row — the status the interrupted run leaves —
// and `TaskUpdate` cannot be asked for `failed` without a run.
func deliveryReadingSeedRow(t *testing.T, taskID, status, deliverable string) {
	t.Helper()
	ctx := context.Background()
	db, err := openTasksDB(ctx, os.Getenv(memoryDBEnv))
	if err != nil {
		t.Fatalf("open ledger: %v", err)
	}
	defer db.Close()
	if _, err := db.ExecContext(ctx,
		`INSERT INTO tasks (task_id, summary, status, deliverable, source, created_at, updated_at)
		 VALUES (?, ?, ?, ?, 'auto', ?, ?)`,
		taskID, "delivery reading fixture", status, deliverable, nowStamp(), nowStamp()); err != nil {
		t.Fatalf("seed row: %v", err)
	}
}

// deliveryReadingVerdict returns the VERDICT TOKEN of a rendered delivery line —
// the word between the label and the em dash separating it from the why.
//
// It exists because "DELIVERED" is a SUBSTRING of "UNDELIVERED": a test asking
// `strings.Contains(line, TaskShowDeliveryDelivered)` answers YES on the
// undelivered arm, which is exactly the confusion this whole reading exists to
// end (measured while writing this test — the first arm failed on it).
func deliveryReadingVerdict(t *testing.T, line string) string {
	t.Helper()
	body := strings.TrimPrefix(line, TaskShowDeliveryLabel+" ")
	verdict, _, _ := strings.Cut(body, " ")
	return strings.TrimSpace(verdict)
}

// deliveryReadingLine extracts the delivery line from a show digest — the ONE
// place its spelling is known to these tests (Rule 1b).
func deliveryReadingLine(t *testing.T, show string) string {
	t.Helper()
	for _, line := range strings.Split(show, "\n") {
		if strings.HasPrefix(line, TaskShowDeliveryLabel) {
			return line
		}
	}
	return ""
}

// deliveryReadingFixture is the world the three arms share: an isolated profile,
// a main checkout, a CLONE ground on the row's branch, and a peer alias that
// resolves to a real bare repo (through the existing shim, so the peer is asked
// by the PRODUCTION channel — `ls-remote` over the resolved target — and never by
// a stubbed answer).
type deliveryReadingFixture struct {
	*clonedGroundFixture
	shim *delRetryShim
}

func newDeliveryReadingFixture(t *testing.T, branch string) *deliveryReadingFixture {
	t.Helper()
	covLifeGitEnv(t)
	shim := delRetryPeerShim(t)
	f := newClonedGroundFixture(t, branch, "sess-delivery-reading")
	f.branchInCloneOnly(t)
	return &deliveryReadingFixture{clonedGroundFixture: f, shim: shim}
}

// newDeliveryReadingFixtureNoWork is the same world with the clone ground
// standing ON the integration tip (no run-specific commit) — the shape whose
// work is already merged.
func newDeliveryReadingFixtureNoWork(t *testing.T, branch string) *deliveryReadingFixture {
	t.Helper()
	covLifeGitEnv(t)
	shim := delRetryPeerShim(t)
	f := newClonedGroundFixture(t, branch, "sess-delivery-merged")
	// The clone arrives checked out on `master`; the row's BRANCH is created in
	// the clone's own ref store at the SAME commit (git checkout -b).
	covLifeGit(t, f.clone, covLifeGitBinary, "checkout", "-q", "-b", branch)
	return &deliveryReadingFixture{clonedGroundFixture: f, shim: shim}
}

// cloneTip is the tip the row's work ended on, read from the ground itself.
func (f *deliveryReadingFixture) cloneTip(t *testing.T) string {
	t.Helper()
	return covLifeGit(t, f.clone, covLifeGitBinary, "rev-parse", "HEAD")
}

// TestTaskShowDeliveryNamesTheCloneAsTheOnlyPlaceTheWorkExists is (a). This is
// the row an interrupted run leaves: `failed`, its work committed in the clone
// ground, the recorded repo without the ref, and nothing ever pushed.
func TestTaskShowDeliveryNamesTheCloneAsTheOnlyPlaceTheWorkExists(t *testing.T) {
	f := newDeliveryReadingFixture(t, "task/01a17df2delivery")
	if got := devworkspace.GroundModelOf(f.clone); got != devworkspace.GroundModelClone {
		t.Fatalf("fixture precondition: %s must be a CLONE ground, got %q", f.clone, got)
	}
	// The asymmetry IS the defect: the clone has the ref, the recorded repo does
	// not, and the peer advertises nothing.
	if !taskBranchExists(f.clone, f.branch) || taskBranchExists(f.repo, f.branch) {
		t.Fatalf("fixture precondition: the branch must live ONLY in the clone (%s)", f.clone)
	}
	if tip := f.shim.holds(t, f.branch); tip != "" {
		t.Fatalf("fixture precondition: the peer must not hold %s, got %s", f.branch, tip)
	}
	tip := f.cloneTip(t)

	deliveryReadingSeedRow(t, "t-delivery-clone-only", taskStatusFailed, f.deliverableBlob())
	out, err := TaskShow("t-delivery-clone-only")
	if err != nil {
		t.Fatalf("TaskShow must never fail because of the delivery reading: %v", err)
	}
	line := deliveryReadingLine(t, out)
	if line == "" {
		t.Fatalf("a terminal row recording a branch must render the delivery line:\n%s", out)
	}
	if got := deliveryReadingVerdict(t, line); got != TaskShowDeliveryUndelivered {
		t.Fatalf("a tip that exists only in its clone ground and not on the peer must read %s, got %q (%q)",
			TaskShowDeliveryUndelivered, got, line)
	}
	// The three facts an operator needs to act: the commit, WHERE it is, and the
	// repo that does NOT hold it.
	for _, want := range []string{tip, f.clone, f.repo, f.branch, f.shim.alias} {
		if !strings.Contains(line, want) {
			t.Errorf("the UNDELIVERED why must carry %q, got %q", want, line)
		}
	}
	// The recorded deliverable is byte-identical across the show: this line is a
	// READER (the same contract the ground advisory carries).
	advisorySeedRowDigestUnchanged(t, "t-delivery-clone-only", f.deliverableBlob())
}

// TestTaskShowDeliveryReadsDeliveredWhenThePeerHoldsTheRowsOwnTip is (b). The
// peer advertises the branch at EXACTLY the tip the ground holds — the one
// observation that justifies the word.
func TestTaskShowDeliveryReadsDeliveredWhenThePeerHoldsTheRowsOwnTip(t *testing.T) {
	f := newDeliveryReadingFixture(t, "task/01a17df2landed")
	tip := f.cloneTip(t)
	// Deliver through the peer's real ref store: the same transfer a push makes.
	covLifeGit(t, f.clone, covLifeGitBinary, "push", "-q", f.shim.repo, f.branch+":"+f.branch)
	if got := f.shim.holds(t, f.branch); got != tip {
		t.Fatalf("fixture precondition: the peer must hold %s at %s, got %q", f.branch, tip, got)
	}

	deliveryReadingSeedRow(t, "t-delivery-landed", taskStatusFailed, f.deliverableBlob())
	out, err := TaskShow("t-delivery-landed")
	if err != nil {
		t.Fatalf("TaskShow: %v", err)
	}
	line := deliveryReadingLine(t, out)
	if got := deliveryReadingVerdict(t, line); got != TaskShowDeliveryDelivered {
		t.Fatalf("a peer holding this row's own tip must read %s, got %q (%q)", TaskShowDeliveryDelivered, got, line)
	}
	if !strings.Contains(line, tip) {
		t.Errorf("the DELIVERED why must name the tip it compared: %q", line)
	}
}

// TestTaskShowDeliveryIsUncheckedWhenNothingCanBeNamed is (c): the branch is in
// no directory the ground can hold it in and the peer advertises nothing. The
// reading must NOT claim a delivery verdict — it states the absence of a
// reading and names every place it asked (Rule 15).
func TestTaskShowDeliveryIsUncheckedWhenNothingCanBeNamed(t *testing.T) {
	f := newDeliveryReadingFixture(t, "task/01a17df2absent")
	// Undo the fixture's work: no branch in the clone, none in the repo.
	covLifeGit(t, f.clone, covLifeGitBinary, "checkout", "-q", masterBranchName)
	covLifeGit(t, f.clone, covLifeGitBinary, "branch", "-q", "-D", f.branch)
	if taskBranchExists(f.clone, f.branch) || taskBranchExists(f.repo, f.branch) {
		t.Fatalf("fixture precondition: the branch must exist nowhere")
	}

	deliveryReadingSeedRow(t, "t-delivery-absent", taskStatusFailed, f.deliverableBlob())
	out, err := TaskShow("t-delivery-absent")
	if err != nil {
		t.Fatalf("TaskShow: %v", err)
	}
	line := deliveryReadingLine(t, out)
	if got := deliveryReadingVerdict(t, line); got != TaskShowDeliveryUnchecked {
		t.Fatalf("a reading with no tip must be %s, never a verdict, got %q (%q)", TaskShowDeliveryUnchecked, got, line)
	}
	// The evidence: both directories asked, so "found nothing" is distinguishable
	// from "asked the wrong tree".
	for _, want := range []string{f.clone, f.repo} {
		if !strings.Contains(line, want) {
			t.Errorf("the UNCHECKED why must name %q among the places asked: %q", want, line)
		}
	}
	// The why is the renderer's own sentence, addressed through its constant so
	// the test cannot drift into pinning a hand-typed paraphrase (Rules 13/14).
	want := fmt.Sprintf(taskShowDeliveryNothingFmt, f.branch,
		taskGroundBranchRefIn(f.repo, f.clone, f.branch).siteText(), deliveryProofLandedWitnessText)
	if !strings.Contains(line, want) {
		t.Errorf("the UNCHECKED why must be the renderer's own absence sentence\nwant: %s\ngot:  %s", want, line)
	}
}

// TestTaskShowDeliveryReadsMergedLocallyWhenTheIntegrationBranchHasTheTip is (e).
//
// THE MEASUREMENT THAT FORCED THIS ARM: driven against this host's real ledger
// the same hour, two live failed rows (01a17e03-12f0, 01a17dfd-09ef) hold tips
// that ARE the integration branch's tip — their branch never reached the peer,
// the WORK already had. Reporting them UNDELIVERED, as this file's first draft
// did, is a false alarm; reporting them DELIVERED would be a lie. So the third
// verdict exists, and this arm pins it.
func TestTaskShowDeliveryReadsMergedLocallyWhenTheIntegrationBranchHasTheTip(t *testing.T) {
	// NO extra commit in the clone: its HEAD IS the recorded repo's integration
	// tip, which is the measured shape (the two live rows' clone grounds stood
	// on 161f48a, master's own tip). Adding a commit here instead would put the
	// tip in a repo the recorded repo cannot see, and the arm would be measuring
	// a different state.
	f := newDeliveryReadingFixtureNoWork(t, "task/01a17df2merged")
	tip := f.cloneTip(t)
	// The recorded repo's integration branch already CONTAINS the row's tip —
	// the measured shape: the clone ground stands ON the integration commit.
	if out := covLifeGit(t, f.repo, covLifeGitBinary, "merge-base", "--is-ancestor", tip, masterBranchName); out != "" {
		t.Fatalf("fixture precondition: %s must be contained in %s", tip, masterBranchName)
	}
	if got := f.shim.holds(t, f.branch); got != "" {
		t.Fatalf("fixture precondition: the peer must advertise nothing for %s, got %s", f.branch, got)
	}

	deliveryReadingSeedRow(t, "t-delivery-merged", taskStatusFailed, f.deliverableBlob())
	out, err := TaskShow("t-delivery-merged")
	if err != nil {
		t.Fatalf("TaskShow: %v", err)
	}
	line := deliveryReadingLine(t, out)
	if got := deliveryReadingVerdict(t, line); got != TaskShowDeliveryMergedLocally {
		t.Fatalf("a tip already contained in the integration branch must read %s, got %q (%q)",
			TaskShowDeliveryMergedLocally, got, line)
	}
	for _, want := range []string{tip, masterBranchName, f.shim.alias} {
		if !strings.Contains(line, want) {
			t.Errorf("the %s why must carry %q, got %q", TaskShowDeliveryMergedLocally, want, line)
		}
	}
	// It must NOT overclaim: the peer's own integration branch was never asked.
	if !strings.Contains(line, "not asked") {
		t.Errorf("the %s why must state that the PEER's integration branch was not asked: %q", TaskShowDeliveryMergedLocally, line)
	}
}

// TestTaskShowDeliveryStaysSilentForAnInFlightRow is (d): a row that has not
// finished has no delivery debt to report, and printing one on every live row is
// exactly the noise this epic removes.
func TestTaskShowDeliveryStaysSilentForAnInFlightRow(t *testing.T) {
	f := newDeliveryReadingFixture(t, "task/01a17df2inflight")
	deliveryReadingSeedRow(t, "t-delivery-inflight", taskStatusInProgress, f.deliverableBlob())
	out, err := TaskShow("t-delivery-inflight")
	if err != nil {
		t.Fatalf("TaskShow: %v", err)
	}
	if line := deliveryReadingLine(t, out); line != "" {
		t.Fatalf("an in-flight row must not render the delivery line, got %q", line)
	}
	// And the advisory GROUND line is still there — the two axes answer different
	// questions and both are rendered.
	if !strings.Contains(out, TaskShowGroundLabel) {
		t.Errorf("the ground advisory must still render:\n%s", out)
	}
}

// advisorySeedRowDigestUnchanged asserts the row's deliverable blob is still
// exactly what was written — the reading is a READER, and this is its contract.
func advisorySeedRowDigestUnchanged(t *testing.T, taskID, want string) {
	t.Helper()
	ctx := context.Background()
	db, err := openTasksDBReadOnly(ctx, memoryDBPath())
	if err != nil {
		t.Fatalf("open ledger read-only: %v", err)
	}
	defer db.Close()
	var got string
	if err := db.QueryRowContext(ctx, `SELECT COALESCE(deliverable,'') FROM tasks WHERE task_id = ?`, taskID).Scan(&got); err != nil {
		t.Fatalf("re-read deliverable: %v", err)
	}
	if got != want {
		t.Fatalf("the show CHANGED the recorded deliverable — it must never write.\nwant: %s\ngot:  %s", want, got)
	}
}
