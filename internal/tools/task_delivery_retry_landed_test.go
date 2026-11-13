package tools

// THE DELIVERY REPAIR FACE RECORDS ITS OWN DELIVERIES (bug row 01a19d5f).
//
// THE DEFECT. This file's three success exits — "the peer already advertises the
// row's branch at the row's own tip", "the clone channel pushed and the peer's ref
// ADVANCED", and the channel's `BlockerKindAlreadyDelivered` gate — changed only
// the in-memory `DeliveryRetryReport`. None of them appended a
// `clone-delivery-landed` record, and the delivery proof's third arm reads those
// records and nothing else (`TaskLandedDeliveryTip`). So a row this path had just
// VERIFIED as delivered still answered "" to the proof, and the empty delivery
// gate fell back to "carries no commit to deliver" — a sentence that points a
// reader at a commit which is, in fact, on the peer.
//
// 7303f30e repaired the same defect on two other faces (cmd/agent's
// already-delivered arm, internal/agent's end hook) and its enumeration missed
// this one. This file pins the third face, ONE TEST PER EXIT.
//
// WHY THE ASSERTIONS ARE COUNTED, NOT JUST READ. "The proof now answers the right
// tip" is satisfiable by a record written EARLIER in the same ledger (the
// `BlockerKindAlreadyDelivered` arm's whole input is such a record), so a test
// asserting only the reader's answer would stay green with the new write deleted.
// Each arm therefore asserts (a) the number of `clone-delivery-landed` records on
// the row grew by exactly one, and (b) the NEWEST record's detail carries the ARM
// TOKEN — a value only the new call site can produce. Deleting a production
// `recordLanded(...)` line turns its arm red on both counts.
//
// THE BRANCH EACH ARM EXPECTS is the one its fixture declared, so the two
// addresses the reader admits (the row's recorded branch and the row's ledger
// address, TaskGroundBranchName) are covered by different arms rather than left
// to a single shape.
//
// The ledger is pinned through testutil.NewTestDBPath (Rule 32) — never a
// hand-rolled path — so nothing here can reach the operator's live ledger.

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nick/gopi/internal/devworkspace"
	"github.com/nick/gopi/internal/testutil"
)

// delLandedSeedRow creates the ledger row these tests judge, with its STATUS and
// its deliverable written directly.
//
// WHY NOT covLifeSeedTaskRow's shape (TaskNew → TaskUpdate(completed) →
// UPDATE deliverable). Measured in this package's own suite run: the completed
// WRITEBACK runs the delivery-owed refusal, which asks the delivery PROOF about
// a row whose deliverable column is still empty — the proof answers "records no
// deliverable branch", the refusal is recorded, and the row stays OPEN. Standalone
// the same order happens to survive (the refusal is a note, not a blocker), so the
// fixture would be green alone and red in the suite: a fixture that depends on a
// gate's own verdict about a half-seeded row is measuring the gate, not the
// subject. Writing status and deliverable in ONE UPDATE removes the intermediate
// state entirely — the row is born in the state under test, and no transition
// machinery is consulted.
func delLandedSeedRow(t *testing.T, taskID, status, deliverable string) {
	t.Helper()
	if _, err := TaskNew("delivery retry fixture "+taskID, taskID, "", ""); err != nil {
		t.Fatalf("TaskNew(%s): %v", taskID, err)
	}
	db, err := openTasksDB(t.Context(), memoryDBPath())
	if err != nil {
		t.Fatalf("open the pinned ledger: %v", err)
	}
	defer db.Close()
	if _, err := db.Exec(`UPDATE tasks SET status = ?, deliverable = ? WHERE task_id = ?`,
		status, deliverable, taskID); err != nil {
		t.Fatalf("seed the row %s: %v", taskID, err)
	}
}

// delLandedRecordDetails returns every `clone-delivery-landed` record on the
// row, in the order they were written. It is the COUNT the red proof turns on.
func delLandedRecordDetails(t *testing.T, taskID string) []string {
	t.Helper()
	db, err := openTasksDB(t.Context(), memoryDBPath())
	if err != nil {
		t.Fatalf("open the pinned ledger: %v", err)
	}
	defer db.Close()
	rows, err := db.QueryContext(t.Context(),
		`SELECT detail FROM task_events WHERE task_id = ? AND event = ? ORDER BY id ASC`,
		taskID, DeliveryLandedEventName)
	if err != nil {
		t.Fatalf("read the landed records of %s: %v", taskID, err)
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var d string
		if err := rows.Scan(&d); err != nil {
			t.Fatalf("scan a landed record of %s: %v", taskID, err)
		}
		out = append(out, d)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("the landed records of %s ended badly: %v", taskID, err)
	}
	return out
}

// delLandedAssertOneNewRecord is the assertion every arm shares: exactly ONE new
// landed record, whose detail names this arm, whose parsed branch/tip is what the
// fixture measured, and which the READER (TaskLandedDeliveryTip) actually serves.
func delLandedAssertOneNewRecord(t *testing.T, taskID, branch, wantTip, armToken string, before int) string {
	t.Helper()
	got := delLandedRecordDetails(t, taskID)
	if len(got) != before+1 {
		t.Fatalf("the row carries %d `%s` record(s), want %d — the %s exit did not record its delivery: %v",
			len(got), DeliveryLandedEventName, before+1, armToken, got)
	}
	detail := got[len(got)-1]
	if !strings.Contains(detail, armToken) {
		t.Fatalf("the newest landed record does not name the exit that wrote it (%q missing): %q", armToken, detail)
	}
	// THE WRITER'S OWN DEFAULT IS A CLAIM, and this face mostly does not push:
	// "pushed to origin" on an arm that only ever READ the peer would be the same
	// confusion the defect was filed on.
	if strings.Contains(detail, "pushed to origin") {
		t.Fatalf("the record claims a push this face did not make: %q", detail)
	}
	gotBranch, gotTip, ok := deliveryProofParseLandedRecord(detail)
	if !ok {
		t.Fatalf("the record is not in the writer's grammar (the reader cannot use it): %q", detail)
	}
	if gotBranch != branch || gotTip != wantTip {
		t.Fatalf("the record names (branch=%q tip=%q), want (%q, %q): %q", gotBranch, gotTip, branch, wantTip, detail)
	}
	// THE CONSUMER, asked last and asked of the LEDGER: this is the read that was
	// answering "" and sending the empty gate to "carries no commit to deliver".
	if served := TaskLandedDeliveryTip(taskID, branch); served != wantTip {
		t.Fatalf("TaskLandedDeliveryTip(%s, %s) = %q, want %q — the record exists but the reader does not reach it",
			taskID, branch, served, wantTip)
	}
	return detail
}

// TestDeliveryRetryLandedPeerAdvertisesRecordsTheRowsTip pins exit ④: the peer's
// own answer already IS this row's tip, so nothing is pushed — and the fact is
// nevertheless recorded, because it is exactly the fact the proof needs.
func TestDeliveryRetryLandedPeerAdvertisesRecordsTheRowsTip(t *testing.T) {
	covLifeGitEnv(t)
	testutil.NewTestDBPath(t)
	shim := delRetryPeerShim(t)

	root := t.TempDir()
	upstream := delCloneUpstream(t)
	taskID := "t-retry-landed-peer-advertises"
	ground, rowTip := delCloneGround(t, root, taskID, upstream)
	shim.seedPeerMaster(t, upstream)

	// The row's own ledger address, published BY the peer at the row's own tip —
	// the state a repair finds after an earlier pass delivered this branch.
	branch := TaskGroundBranchName(taskID)
	if branch == "" {
		t.Fatal("fixture: the row must have a ledger-address branch")
	}
	covLifeGit(t, ground, covLifeGitBinary, "push", "-q", shim.repo, rowTip+":"+delEmptyHeadsPrefix+branch)
	if got := shim.holds(t, branch); got != rowTip {
		t.Fatalf("fixture: the peer must advertise %s at %q, it holds %q", branch, rowTip, got)
	}

	delLandedSeedRow(t, taskID, TaskStatusCompleted,
		taskDeliverableRebuild(taskDeliverable{Repo: upstream, Branch: branch, Worktree: ground}))
	before := len(delLandedRecordDetails(t, taskID))

	rep, err := DeliveryRetryTaskID(taskID)
	if err != nil {
		t.Fatalf("delivery retry: %v", err)
	}
	if rep.Status != deliveryRetryStatusAlreadyDelivered {
		t.Fatalf("status=%q want %q — the peer holds this row's own tip (detail=%q peerTip=%q rowTip=%q)",
			rep.Status, deliveryRetryStatusAlreadyDelivered, rep.Detail, rep.PeerTip, rep.RowTip)
	}
	// THE ARM PUSHED NOTHING — the channel never ran, so the record's only honest
	// content is the measurement that made this exit a success.
	if rep.Clone != nil {
		t.Fatalf("the clone channel ran on an already-advertised branch: %+v", *rep.Clone)
	}
	// THE TIP IS THE PEER'S OWN, because this exit is the one that KNOWS the
	// commit exists: whichever of the two peer arms answered, the delivered
	// commit is what the peer advertises. `rowTip` is asserted to AGREE when it
	// could be read — a disagreement would be the resurrect defect, not this one.
	if rep.RowTip != "" && rep.RowTip != rowTip {
		t.Fatalf("the row's tip was re-read as %q, the fixture pushed %q", rep.RowTip, rowTip)
	}
	delLandedAssertOneNewRecord(t, taskID, branch, rep.PeerTip, deliveryRetryLandedArmPeerAdvertises, before)
}

// TestDeliveryRetryLandedCloneChannelRecordsTheAdvancedTip pins exit ⑦: the
// channel genuinely pushed and the peer's ref ADVANCED from the tip it held
// before. The record must name the tip the peer now carries — not the one it
// carried before, and not the row's pre-pull tip.
func TestDeliveryRetryLandedCloneChannelRecordsTheAdvancedTip(t *testing.T) {
	covLifeGitEnv(t)
	testutil.NewTestDBPath(t)
	shim := delRetryPeerShim(t)

	root := t.TempDir()
	upstream := delCloneUpstream(t)
	delCloneSettings(t, upstream, upstream, root)

	taskID := "t-retry-landed-clone-channel"
	ground, rowTip := delCloneGround(t, root, taskID, upstream)
	shim.seedPeerMaster(t, upstream)
	peerTipBefore := shim.holds(t, masterBranchName)
	if peerTipBefore == "" || peerTipBefore == rowTip {
		t.Fatalf("fixture: the peer must advertise %s at a tip that is not the row's (%q vs %q)",
			masterBranchName, peerTipBefore, rowTip)
	}

	delLandedSeedRow(t, taskID, TaskStatusCompleted,
		taskDeliverableRebuild(taskDeliverable{Repo: upstream, Branch: masterBranchName, Worktree: ground}))
	before := len(delLandedRecordDetails(t, taskID))

	rep, err := DeliveryRetryTaskID(taskID)
	if err != nil {
		t.Fatalf("delivery retry: %v", err)
	}
	if rep.Status != deliveryRetryStatusDelivered {
		t.Fatalf("status=%q want %q (blocked=%q detail=%q clone=%+v)",
			rep.Status, deliveryRetryStatusDelivered, rep.Blocked, rep.Detail, rep.Clone)
	}
	if rep.Clone == nil || !rep.Clone.Pushed {
		t.Fatalf("the channel did not report a push, so this is not the arm under test: %+v", rep.Clone)
	}
	// THE PEER REALLY ADVANCED — asked of the bare repo, never of the report.
	landed := shim.holds(t, masterBranchName)
	if landed == peerTipBefore {
		t.Fatalf("the peer's %s is still %q — the exit under test was not reached", masterBranchName, landed)
	}
	detail := delLandedAssertOneNewRecord(t, taskID, masterBranchName, landed, deliveryRetryLandedArmCloneChannel, before)
	if !strings.Contains(detail, peerTipBefore) || !strings.Contains(detail, landed) {
		t.Fatalf("the record must say which tip the peer advanced FROM (%q) and TO (%q): %q", peerTipBefore, landed, detail)
	}
}

// TestDeliveryRetryLandedGateRecordsTheTipTheGateSaw pins the third exit: the
// channel's own gate answered `BlockerKindAlreadyDelivered` — this row's earlier
// push already moved the integration base onto its tip, so the count is 0 because
// the delivery SUCCEEDED.
//
// THE RECORD IS COUNTED, NOT READ, and that is the point of this fixture: the
// gate's whole INPUT is a `clone-delivery-landed` record (cfg.DeliveredTip), so
// the reader already answers a tip before this run writes anything. A test that
// only asked the reader would stay green with the production call deleted — so
// the assertion is "one MORE record, naming THIS arm".
func TestDeliveryRetryLandedGateRecordsTheTipTheGateSaw(t *testing.T) {
	covLifeGitEnv(t)
	testutil.NewTestDBPath(t)
	shim := delRetryPeerShim(t)

	root := t.TempDir()
	upstream := delCloneUpstream(t)
	delCloneSettings(t, upstream, upstream, root)

	// A ground with nothing of its own: HEAD IS the base tip, which is the one
	// shape the gate cannot rank (the `empty` kind's condition) and the shape a
	// delivery that already landed leaves behind.
	taskID := "t-retry-landed-gate"
	ground := filepath.Join(root, taskID)
	covLifeGit(t, root, covLifeGitBinary, "clone", "-q", upstream, ground)
	covLifeGit(t, ground, covLifeGitBinary, "remote", "set-url", deliveryProofOriginRemote,
		delRetryShimAlias+":~/work/GoPi")
	baseTip := strings.TrimSpace(covLifeGit(t, ground, covLifeGitBinary, "rev-parse", "HEAD"))
	shim.seedPeerMaster(t, upstream)
	if shim.holds(t, masterBranchName) != baseTip {
		t.Fatalf("fixture: the peer's %s must stand on the ground's own tip %q", masterBranchName, baseTip)
	}
	delEmptyTrackingPointsAtRow(t, ground, baseTip)

	branch := TaskGroundBranchName(taskID)
	if branch == "" {
		t.Fatal("fixture: the row must have a ledger-address branch")
	}
	if got := shim.holds(t, branch); got != "" {
		t.Fatalf("fixture: the peer must NOT advertise %s (④ would answer first), it holds %q", branch, got)
	}
	delLandedSeedRow(t, taskID, TaskStatusCompleted,
		taskDeliverableRebuild(taskDeliverable{Repo: upstream, Branch: branch, Worktree: ground}))

	// THE GATE'S INPUT: the row already carries the record of the push that
	// landed. It is written with a seed detail so the newest-record assertion
	// below cannot be satisfied by it.
	TaskDeliveryRecordLanded(taskID, branch, ground, baseTip, "a seed record: the earlier push that landed")
	before := delLandedRecordDetails(t, taskID)
	if len(before) != 1 {
		t.Fatalf("fixture: the row must carry exactly the seed record, it carries %d: %v", len(before), before)
	}

	rep, err := DeliveryRetryTaskID(taskID)
	if err != nil {
		t.Fatalf("delivery retry: %v", err)
	}
	// THE ARM WAS REACHED: the clone result exists only when the channel ran, and
	// a pushed report means the gate let the push through instead.
	if rep.Clone == nil {
		t.Fatalf("the report carries no clone-channel result — the gate arm was never reached (status=%q detail=%q)",
			rep.Status, rep.Detail)
	}
	if rep.Clone.Pushed {
		t.Fatalf("this attempt pushed, so the gate did not fire: %+v", *rep.Clone)
	}
	if rep.Status != deliveryRetryStatusAlreadyDelivered {
		t.Fatalf("status=%q want %q — the base already reaches this row's tip %q (blocked=%q detail=%q)",
			rep.Status, deliveryRetryStatusAlreadyDelivered, baseTip, rep.Blocked, rep.Detail)
	}
	if !strings.Contains(rep.Detail, "already landed") {
		t.Errorf("the detail must be the gate's own already-delivered sentence: %q", rep.Detail)
	}
	delLandedAssertOneNewRecord(t, taskID, branch, baseTip, deliveryRetryLandedArmGate, len(before))
}

// A COMPILE-TIME TIE: the gate arm is keyed on the exported Kind, and the record's
// event name is the reader's own constant. If devworkspace ever renames either,
// the build fails here instead of the two faces silently disagreeing (Rules
// 1/13 — the identifier is REFERENCED, never retyped).
var _ = devworkspace.BlockerKindAlreadyDelivered
var _ = DeliveryLandedEventName

// TestDeliveryRetryLandedWithoutATipSaysSoInsteadOfWritingNothingAtAll pins the
// Rule 15 half of the same seam: when the exit has verified a delivery but cannot
// name the commit, the record must NOT be written with a made-up tip (a record
// naming no commit is unusable to the reader, which parses the sha out of it) and
// it must not be written silently either — the line names the ARM and the ground,
// which is what tells a later reader whether the missing record is a defect or a
// discard.
func TestDeliveryRetryLandedWithoutATipSaysSoInsteadOfWritingNothingAtAll(t *testing.T) {
	testutil.NewTestDBPath(t)
	taskID := "t-retry-landed-no-tip"
	if _, err := TaskNew("a row whose delivery cannot name its tip", taskID, "", ""); err != nil {
		t.Fatalf("TaskNew(%s): %v", taskID, err)
	}

	// THE LOG IS THE OBSERVABLE (this package's log face is stderr), so it is
	// captured for the duration rather than assumed.
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe for the log face: %v", err)
	}
	saved := os.Stderr
	os.Stderr = w
	recordLanded(&DeliveryRetryReport{TaskID: taskID, Branch: "task/" + taskID},
		"/some/ground", "", deliveryRetryLandedArmCloneChannel, "aaa", "bbb")
	os.Stderr = saved
	if err := w.Close(); err != nil {
		t.Fatalf("close the capture pipe: %v", err)
	}
	said, err := io.ReadAll(r)
	if err != nil {
		t.Fatalf("read the captured log: %v", err)
	}

	if got := delLandedRecordDetails(t, taskID); len(got) != 0 {
		t.Fatalf("a record was written without a tip: %v", got)
	}
	if !strings.Contains(string(said), deliveryRetryLandedArmCloneChannel) ||
		!strings.Contains(string(said), "/some/ground") {
		t.Fatalf("the missing record must be SAID OUT LOUD, naming the arm and the ground: %q", string(said))
	}
}
