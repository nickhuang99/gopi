package tools

// "THIS GROUND CARRIES NO COMMIT TO DELIVER" IS A CONCLUSION, NOT A FAILURE
// (bug 01a1827e, measured 2026-10-28).
//
// devworkspace's deliveryEmptyGate answers `rev-list --count HEAD --not
// origin/<base>` == 0 with a Blocker of its own Kind `empty`. In
// deliveryRetryCloneDelivery the switch's first arm was `case blk != nil`, so
// that blocker — whatever its Kind — became `blocked`, and the `already-merged`
// exit further down (the peer's master already CONTAINS this row's work) was
// unreachable FOR THIS KIND: a row whose work had landed was retried forever.
//
// THE TWO FACTS THE FIX SPLITS, and the reason each arm below exists:
//
//	empty + peer master CONTAINS rowTipBefore → already-merged (nothing owed)
//	empty + peer master LACKS   rowTipBefore → blocked (the work is not there)
//
// THE SECOND ARM IS NOT OPTIONAL. Splitting `empty` out must not become "an
// empty ground is always fine" — that would turn a genuinely undelivered row
// green. Every other Kind (push refused, build failed, dirty worktree, pull
// conflict) keeps the untouched `case blk != nil` arm, pinned by the existing
// TestDeliveryRetryCloneGroundRefusalIsTheChannelsNotTheExemptions.
//
// THE `already-merged` EXIT HAS TWO DOORS, and both are pinned because they are
// two different measurements (task 01a194f2, measured 2026-11-01):
//
//	inside the empty arm   the gate could NOT rank HEAD against the integration
//	                       tip (HEAD *IS* the base tip — a ground that produced
//	                       nothing of its own) and the peer's master contains it
//	                       → TestDeliveryRetryCloneGroundEmptyArmAdoptedIsAlreadyMerged
//	router ④, before the   the peer ADVERTISES the row's branch at another tip
//	channel runs           while its master already contains the row's work
//	                       → TestDeliveryRetryCloneGroundPeerAdoptingTheWorkIsAlreadyMerged
//
// WHY THAT HAD TO BE MEASURED RATHER THAN ASSUMED. When the forward arm below
// moved its assertion to `inherited` (its fixture IS the strict-ancestor shape),
// the empty arm's `already-merged` exit had no test reaching it at all — and a
// status an arm can produce but no test reaches is indistinguishable from dead
// code. The red proof for the new arm is recorded in its own comment: with that
// arm's containment question short-circuited (`if false && …`) the same fixture
// degrades to `blocked` with the channel's empty sentence — the exact pre-fix
// behaviour — and returns to `already-merged` when restored.
//
// WHY THE FIXTURE LOOKS THE WAY IT DOES. Production rows reach the empty state
// because the gate measures the GROUND'S OWN remote-tracking ref — a clone's
// STALE VIEW (`refs/remotes/origin/<base>`), which an earlier channel pull
// fast-forwarded while the peer's master already had the work. The fixture
// therefore (a) points that tracking ref at the row's tip — a write to a
// `refs/remotes/` ref only, which moves no branch and touches no peer — and
// (b) keeps the PEER from advertising the row's branch, so the router's earlier
// `already-delivered` exit cannot answer before the channel runs. That is the
// production shape: `deliverable.branch = master` (the channel pushes
// HEAD:master) is not a ref any peer advertises.

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/nickhuang99/gopi/internal/devworkspace"
)

const (
	// delEmptyCount is the count the empty gate stops on, spelled once (Rule 1b).
	delEmptyCount = "0"
	// delEmptyTrackingGlobPrefix is git's remote-tracking namespace for the
	// fixture's own measurement of the gate's question.
	delEmptyTrackingGlobPrefix = "refs/remotes/origin/"
	// delEmptyHeadsPrefix fully qualifies the destination of a fixture push: a
	// sha on the source side plus a bare name on the destination is REFUSED by
	// git ("The destination you provided is not a full refname"), so the
	// namespace is spelled once here rather than re-typed per push (Rule 1b).
	delEmptyHeadsPrefix = "refs/heads/"
	// delEmptyLaterEmail / delEmptyLaterName sign the fixture's "a later row
	// landed on top" commit.
	delEmptyLaterEmail = "later@example.invalid"
	delEmptyLaterName  = "later"
)

// A COMPILE-TIME TIE to the Kind this file's fix keys on: if devworkspace ever
// renames its exported constant the build fails here instead of the production
// switch silently comparing against a stale literal (Rules 1/13: the Kind is
// REFERENCED, never retyped).
var _ = devworkspace.BlockerKindEmpty

// delEmptyIntegration is the base the channel rebases onto for these rows: the
// row records `deliverable.branch = master` and no GroundBranch, so
// devworkspace.baseBranchOf answers exactly this.
const delEmptyIntegration = devworkspace.DefaultBranch

// delEmptyTrackingPointsAtRow is the shared precondition of both arms: the
// ground's remote-tracking integration ref is made to CONTAIN its HEAD — the
// measured meaning of `rev-list --count HEAD --not origin/<base>` answering 0.
// The assertion below asks the gate's own question directly, so an arm cannot
// pass while the gate never fired (a fixture that fails to reach the code under
// test is a vacuum, not a green).
func delEmptyTrackingPointsAtRow(t *testing.T, ground, tip string) {
	t.Helper()
	covLifeGit(t, ground, covLifeGitBinary, "update-ref",
		delEmptyTrackingGlobPrefix+delEmptyIntegration, tip)
	if n := strings.TrimSpace(covLifeGit(t, ground, covLifeGitBinary, "rev-list", "--count",
		"HEAD", "--not", delEmptyTrackingGlobPrefix+delEmptyIntegration)); n != delEmptyCount {
		t.Fatalf("fixture: the ground must carry %s commit(s) against origin/%s, it carries %q",
			delEmptyCount, delEmptyIntegration, n)
	}
}

// delEmptyPeerMasterContains is the fixture's own direct question to the BARE
// repo (never a report): does the peer's integration branch contain the row's
// work?
func delEmptyPeerMasterContains(t *testing.T, shim *delRetryShim, tip string) bool {
	t.Helper()
	_, err := covLifeGitErr(t, "", covLifeGitBinary, "--git-dir="+shim.repo,
		"merge-base", "--is-ancestor", tip, "refs/heads/"+delEmptyIntegration)
	return err == nil
}

// delEmptyPeerMasterAhead seeds the peer's integration branch with a tip that
// CONTAINS the row's work but is NOT it — the shape a landed row is in once a
// LATER row commits on top. The distinction is load-bearing: `peerTip ==
// rowTip` would be answered by the router's earlier `already-delivered` exit,
// and the empty-gate branch under test would never run.
func delEmptyPeerMasterAhead(t *testing.T, shim *delRetryShim, upstream, ground, rowTip string) string {
	t.Helper()
	later := t.TempDir()
	covLifeGit(t, filepath.Dir(later), covLifeGitBinary, "clone", "-q", upstream, later)
	covLifeGit(t, later, covLifeGitBinary, "fetch", "-q", ground, delEmptyIntegration)
	covLifeGit(t, later, covLifeGitBinary, "-c", "user.email="+delEmptyLaterEmail,
		"-c", "user.name="+delEmptyLaterName, "merge", "-q", "--no-ff",
		"-m", "a later row landed on top", "FETCH_HEAD")
	covLifeWriteFile(t, filepath.Join(later, "later.txt"), "landed after this row\n")
	covLifeGit(t, later, covLifeGitBinary, "add", "later.txt")
	covLifeGit(t, later, covLifeGitBinary, "-c", "user.email="+delEmptyLaterEmail,
		"-c", "user.name="+delEmptyLaterName, "commit", "-q", "-m", "a later row's deliverable")
	covLifeGit(t, later, covLifeGitBinary, "push", "-q", shim.repo, delEmptyIntegration+":"+delEmptyIntegration)
	peerTip := strings.TrimSpace(covLifeGit(t, later, covLifeGitBinary, "rev-parse", "HEAD"))
	if peerTip == rowTip {
		t.Fatalf("fixture: the peer's tip must differ from the row's %q", rowTip)
	}
	if !delEmptyPeerMasterContains(t, shim, rowTip) {
		t.Fatalf("fixture: the peer's %s must CONTAIN the row's work %q", delEmptyIntegration, rowTip)
	}
	return peerTip
}

// TestDeliveryRetryCloneGroundWithNothingLeftToDeliverIsAlreadyMerged — the
// forward arm of the delivery-retry split. THE NAME IS HISTORICAL: it dates from
// 4776af5b, before the gate had a third kind for `ahead == 0`, and back then this
// fixture's disposition really was `already-merged`. dfe20bde then taught the gate
// to rank HEAD against the peer's live tip and to name this shape
// `BlockerKindInheritedDelivery` (HEAD a STRICT ANCESTOR of the integration tip),
// and this arm was left asserting the old status — the red this task repairs
// (`status="inherited" want "already-merged"`).
//
// THE SEMANTIC RULING, and the reason the assertion moves instead of the code:
// this fixture IS the strict-ancestor shape (the ground's HEAD is the row's own
// commit, the peer's master has a LATER commit on top of it), so no amount of
// code could honestly answer anything but `inherited` — HEAD carries nothing the
// integration branch lacks, because the content this run was handed is already
// there. `already-merged` keeps its own, DIFFERENT shape (the peer advertises the
// row's branch at another tip while its master already contains the row's work —
// the "do not resurrect a ref the adoption chain removed" disposition, pinned
// reachable by TestDeliveryRetryCloneGroundPeerAdoptingTheWorkIsAlreadyMerged).
//
// NOTHING IS WEAKENED HERE. Every claim about the row's DEBT is still asserted,
// and the fact that makes "no debt" true rather than "we gave up" — the peer's
// master really CONTAINS this row's commit — is now asserted against the peer
// repo directly, so the arm cannot pass on a sentence alone.
func TestDeliveryRetryCloneGroundWithNothingLeftToDeliverIsAlreadyMerged(t *testing.T) {
	covLifeGitEnv(t)
	shim := delRetryPeerShim(t)

	root := t.TempDir()
	upstream := delCloneUpstream(t)
	delCloneSettings(t, upstream, upstream, root)

	taskID := "t-retry-clone-empty-merged"
	ground, rowTip := delCloneGround(t, root, taskID, upstream)
	// THE PEER REALLY HAS THE WORK (a later row landed on top, so the peer's ref
	// is NOT this row's tip — see delEmptyPeerMasterAhead).
	delEmptyPeerMasterAhead(t, shim, upstream, ground, rowTip)
	// AND THIS GROUND HAS NOTHING LEFT TO DELIVER, by the gate's own measurement.
	delEmptyTrackingPointsAtRow(t, ground, rowTip)

	// THE ROW'S BRANCH IS ITS OWN LEDGER ADDRESS and the peer does NOT advertise
	// it — the shape an adopted row is in after the server's chain removed the
	// ref on purpose. That is what lets the CHANNEL run at all: the router's ④
	// asks the peer for `deliverable.branch` and answers early whenever the peer
	// carries it.
	own := TaskGroundBranchName(taskID)
	if own == "" {
		t.Fatal("fixture: the row must have a ledger-address branch")
	}
	if got := shim.holds(t, own); got != "" {
		t.Fatalf("fixture: the peer must NOT advertise %s, it holds %q", own, got)
	}
	covLifeSeedTaskRow(t, taskID,
		taskDeliverableRebuild(taskDeliverable{Repo: upstream, Branch: own, Worktree: ground}))

	rep, err := DeliveryRetryTaskID(taskID)
	if err != nil {
		t.Fatalf("delivery retry on a clone-ground row: %v", err)
	}
	if rep.Clone == nil {
		t.Fatalf("the report carries no clone-channel result — the empty gate was never reached (status=%q detail=%q)",
			rep.Status, rep.Detail)
	}
	// THE DEFECT'S OWN FINGERPRINT: before the split this row answered `blocked`
	// with the channel's empty sentence — forever — because `case blk != nil`
	// claimed the kind first. It must now answer the kind's OWN disposition, and
	// the kind here is `inherited`: the gate ranked HEAD against the peer's live
	// tip and found it a STRICT ancestor, which is the content-already-there
	// shape. `already-merged` is a different measurement and has its own arm.
	if rep.Status != deliveryRetryStatusInherited {
		t.Fatalf("status=%q want %q — this ground's HEAD is a strict ancestor of the integration tip, so the "+
			"content it was handed is already there and there is nothing to publish "+
			"(blocked=%q detail=%q peerTip=%q)",
			rep.Status, deliveryRetryStatusInherited, rep.Blocked, rep.Detail, rep.PeerTip)
	}
	// AND THE FACT THAT MAKES THAT HONEST RATHER THAN A SILENT PASS: the peer's
	// master really does contain this row's work. Asked of the peer repo itself,
	// never of the report (Rule 31 — a sentence is not a measurement).
	if !delEmptyPeerMasterContains(t, shim, rowTip) {
		t.Fatalf("fixture/defect: %q is only justified when the peer's master CONTAINS the row's %q",
			deliveryRetryStatusInherited, rowTip)
	}
	if rep.Blocked != "" {
		t.Fatalf("a row with no debt must carry no blocked reason, got %q", rep.Blocked)
	}
	if rep.Owed() {
		t.Fatalf("%q must not be owed: Owed()=true (status=%q)", deliveryRetryStatusInherited, rep.Status)
	}
	// The detail is the gate's own MEASURED sentence for this kind (composed
	// once in devworkspace and carried through `blk.Report()`), so this asserts
	// the SENTENCE ARRIVED — not a local paraphrase of it. The marker is spelled
	// here exactly as the sibling inherited arm spells it: devworkspace keeps it
	// unexported on purpose (one composition site), and the two tests are the
	// readers that would notice it drifting.
	if !strings.Contains(rep.Detail, "already on the integration branch") {
		t.Errorf("the detail must carry the inherited sentence's own marker: %q", rep.Detail)
	}
	if strings.Contains(rep.Detail, "the peer's  ") || strings.Contains(rep.Detail, "  ") {
		t.Errorf("the detail carries an empty field (a doubled space): %q", rep.Detail)
	}
	// THE ROW'S OWN TIP IS STILL NAMED. The moved assertion used to check this
	// field too, and dropping it would have been the one real loss in this
	// change: `inherited` is only honest if the report still says WHICH tip this
	// row owed, so a reader can go and verify the containment itself. It is the
	// router's own reading (`deliveryRetryRowTip` on a clone ground = the
	// worktree's HEAD), not a value copied from the fixture's expectation.
	if rep.RowTip != rowTip {
		t.Errorf("the report must name THIS row's tip %q, it names %q", rowTip, rep.RowTip)
	}
	if strings.HasPrefix(rep.Detail, deliveryRetryOwedMarker) {
		t.Errorf("a row with no debt must not be worded as a debt: %q", rep.Detail)
	}
	// The trail says "delivered", not "blocked": the resident pass reads this to
	// decide what to retry, so a blocked event would keep the row in the loop.
	if events := delRetryTrailEvents(t, taskID); !hasString(events, deliveryRetryDeliveredEvent) {
		t.Errorf("the row's trail must carry %q, got %v", deliveryRetryDeliveredEvent, events)
	}
}

// TestDeliveryRetryCloneGroundWithNothingLeftToDeliverStaysBlockedWhenThePeerLacksTheWork
// is the REVERSE arm, and it is what keeps the split from becoming a relaxation:
// the SAME empty ground, the SAME fix, but the peer's master does NOT contain the
// row's work — so the row still owes its delivery and must stay blocked.
//
// The shape is a real one: a ground's tracking ref is a local VIEW that can say
// "0 to deliver" while the peer's master has never seen this tip — an
// `origin/master` is exactly as trustworthy as its last fetch.
func TestDeliveryRetryCloneGroundWithNothingLeftToDeliverStaysBlockedWhenThePeerLacksTheWork(t *testing.T) {
	covLifeGitEnv(t)
	shim := delRetryPeerShim(t)

	root := t.TempDir()
	upstream := delCloneUpstream(t)
	delCloneSettings(t, upstream, upstream, root)

	taskID := "t-retry-clone-empty-unmerged"
	ground, rowTip := delCloneGround(t, root, taskID, upstream)
	// THE PEER HAS ONLY THE BASE: its master is upstream's tip, which does NOT
	// contain the row's commit.
	shim.seedPeerMaster(t, upstream)
	if delEmptyPeerMasterContains(t, shim, rowTip) {
		t.Fatalf("fixture: the peer's %s must NOT contain the row's work %q", delEmptyIntegration, rowTip)
	}
	delEmptyTrackingPointsAtRow(t, ground, rowTip)

	own := TaskGroundBranchName(taskID)
	covLifeSeedTaskRow(t, taskID,
		taskDeliverableRebuild(taskDeliverable{Repo: upstream, Branch: own, Worktree: ground}))

	rep, err := DeliveryRetryTaskID(taskID)
	if err != nil {
		t.Fatalf("delivery retry on a clone-ground row: %v", err)
	}
	if rep.Clone == nil {
		t.Fatalf("the report carries no clone-channel result — the empty gate was never reached (status=%q)", rep.Status)
	}
	if rep.Status == deliveryRetryStatusAlreadyMerged {
		t.Fatalf("status=%q on a row the peer's master does NOT contain — the split relaxed a real debt "+
			"(detail=%q peerTip=%q)", rep.Status, rep.Detail, rep.PeerTip)
	}
	if rep.Status != deliveryRetryStatusBlocked {
		t.Fatalf("status=%q want %q: an empty ground whose work is not on the peer still owes it (detail=%q)",
			rep.Status, deliveryRetryStatusBlocked, rep.Detail)
	}
	if !rep.Owed() {
		t.Error("a blocked row must still OWE its delivery")
	}
	// The refusal is STILL the channel's own empty sentence: the fix must not
	// invent wording for a failure that was already worded correctly, and it must
	// not have become some other step's refusal.
	if !strings.Contains(rep.Blocked, cloneChannelFailurePrefix) {
		t.Errorf("the blocked reason must carry the channel's prefix %q: %q", cloneChannelFailurePrefix, rep.Blocked)
	}
	// EXACTLY ONCE: this arm's body IS `blk.Report()`, whose empty-kind sentence
	// opens with the prefix already — the doubling the composer removes.
	if n := strings.Count(rep.Blocked, cloneChannelFailurePrefix); n != 1 {
		t.Errorf("the channel prefix must occur exactly once in the owed reason, count=%d: %q", n, rep.Blocked)
	}
	if !strings.Contains(rep.Blocked, "carries no commit to deliver") {
		t.Errorf("the blocked reason must still be the empty gate's own sentence: %q", rep.Blocked)
	}
	// The commit the row still owns is untouched: this path repairs, never cleans.
	if tip := strings.TrimSpace(covLifeGit(t, ground, covLifeGitBinary, "rev-parse", "HEAD")); tip != rowTip {
		t.Errorf("the ground's HEAD moved to %q during a blocked retry, want %q", tip, rowTip)
	}
}

// TestDeliveryRetryCloneGroundWhoseContentThePeerAlreadyContainsIsInherited
// pins the THIRD meaning of the same zero on the REPAIR face (epic: inherited
// delivery): the ground's HEAD is a STRICT ancestor of the base it measures
// against — the base landed this row's content and moved on — so the count is 0
// because the content is THERE, not because the ground is empty and not because
// this attempt pushed anything.
//
// The pre-fix repair face had no case for it: `case blk != nil` claimed it and
// the row went back to `blocked` — "owed" forever, on work that had already
// landed (the epic measured 49 rows wearing 交付没有落地 for this shape).
func TestDeliveryRetryCloneGroundWhoseContentThePeerAlreadyContainsIsInherited(t *testing.T) {
	covLifeGitEnv(t)
	shim := delRetryPeerShim(t)

	root := t.TempDir()
	upstream := delCloneUpstream(t)
	delCloneSettings(t, upstream, upstream, root)

	taskID := "t-retry-clone-inherited"
	ground, rowTip := delCloneGround(t, root, taskID, upstream)
	// THE BASE CARRIES THE ROW'S CONTENT AND HAS MOVED PAST IT.
	peerTip := delEmptyPeerMasterAhead(t, shim, upstream, ground, rowTip)
	// THE GROUND'S OWN VIEW OF THE BASE IS THAT LATER TIP — the shape a channel
	// pull leaves behind: HEAD still points at the row's commit, while the ref it
	// measures against contains it. Written as a tracking ref only, so no branch
	// and no peer ref moves.
	if peerTip == rowTip {
		t.Fatalf("fixture: the peer's tip must be PAST the row's %q", rowTip)
	}
	// THE OBJECT HAS TO BE IN THE GROUND'S STORE, or `update-ref` writes a name
	// pointing at nothing (a real failure this fixture hit first: exit 128, "tried
	// to write ref with nonexistent object"). The fetch is a LOCAL read of the
	// peer repo the fixture built — no network, and it updates no branch, so the
	// ground's HEAD does not move.
	covLifeGit(t, ground, covLifeGitBinary, "fetch", "-q", shim.repo,
		"+refs/heads/"+delEmptyIntegration+":"+delEmptyTrackingGlobPrefix+delEmptyIntegration)
	delEmptyTrackingPointsAtRow(t, ground, peerTip)
	if peerTip == strings.TrimSpace(covLifeGit(t, ground, covLifeGitBinary, "rev-parse", "HEAD")) {
		t.Fatal("fixture: the ground's HEAD must NOT be the base tip, or this arm would be the EMPTY shape")
	}

	own := TaskGroundBranchName(taskID)
	covLifeSeedTaskRow(t, taskID,
		taskDeliverableRebuild(taskDeliverable{Repo: upstream, Branch: own, Worktree: ground}))

	rep, err := DeliveryRetryTaskID(taskID)
	if err != nil {
		t.Fatalf("delivery retry on a clone-ground row: %v", err)
	}
	if rep.Clone == nil {
		t.Fatalf("the report carries no clone-channel result — the gate was never reached (status=%q)", rep.Status)
	}
	if rep.Status != deliveryRetryStatusInherited {
		t.Fatalf("status=%q want %q — the base already CONTAINS this row's content (blocked=%q detail=%q)",
			rep.Status, deliveryRetryStatusInherited, rep.Blocked, rep.Detail)
	}
	if rep.Blocked != "" {
		t.Fatalf("a row whose content is on the base owes nothing, got blocked=%q", rep.Blocked)
	}
	if rep.Owed() {
		t.Error("an inherited delivery must not be owed")
	}
	if strings.HasPrefix(rep.Detail, deliveryRetryOwedMarker) ||
		strings.Contains(rep.Detail, "delivery did not land") {
		t.Errorf("nothing was refused — the debt wording must not appear: %q", rep.Detail)
	}
	if !strings.Contains(rep.Detail, "already on the integration branch") {
		t.Errorf("the detail must carry the inherited sentence it measured: %q", rep.Detail)
	}
	if events := delRetryTrailEvents(t, taskID); !hasString(events, deliveryRetryDeliveredEvent) {
		t.Errorf("the trail must carry %q (not a blocked event that keeps the row in the loop), got %v",
			deliveryRetryDeliveredEvent, events)
	}
}

// TestDeliveryRetryCloneGroundPeerAdoptingTheWorkIsAlreadyMerged pins the
// `already-merged` EXIT, and it exists because the arm above changed its
// assertion: a status that no reachable path can produce is dead code, and the
// Rule 43 duty is to prove which it is rather than count call sites.
//
// THE SHAPE IS THE ONE THAT ARM IS NOT, and the difference is a measurement, not
// a wording:
//
//	inherited        the peer does NOT advertise this row's branch at all, and
//	                 the integration tip is a STRICT ANCESTOR relationship to
//	                 HEAD — nothing here to publish.
//	already-merged   the peer DOES advertise this row's branch, at a tip that is
//	                 NOT this row's, while its integration branch already
//	                 CONTAINS this row's work. Pushing would resurrect a ref the
//	                 server's adoption chain removed on purpose.
//
// Both are reachable, and they are told apart by the peer's own answer read in
// the router's ④ — never by a re-test inside a kind's arm (the layering the
// sibling arm's comment records).
func TestDeliveryRetryCloneGroundPeerAdoptingTheWorkIsAlreadyMerged(t *testing.T) {
	covLifeGitEnv(t)
	shim := delRetryPeerShim(t)

	taskID := "t-retry-peer-adopted"
	repo, wt, branch := delRetryRepoWithGround(t, "task/retry-peer-adopted")
	rowTip := strings.TrimSpace(covLifeGit(t, repo, covLifeGitBinary, "rev-parse", branch))
	parentTip := strings.TrimSpace(covLifeGit(t, repo, covLifeGitBinary, "rev-parse", branch+"~1"))
	if rowTip == parentTip {
		t.Fatalf("fixture: the row's tip must differ from its parent, both are %q", rowTip)
	}

	// ① THE PEER'S INTEGRATION BRANCH CONTAINS THIS ROW'S WORK — the adoption
	// fact the disposition rests on, published straight at the bare repo so no
	// branch of the client moves.
	covLifeGit(t, repo, covLifeGitBinary, "push", "-q", shim.repo, "+"+rowTip+":"+delEmptyHeadsPrefix+delEmptyIntegration)
	// ② AND IT STILL ADVERTISES THIS ROW'S BRANCH, at an OLDER tip — the ref the
	// adoption chain would have removed, still there. That is what makes the
	// router's ④ run at all (an unadvertised branch skips it, which is the
	// `inherited` shape).
	covLifeGit(t, repo, covLifeGitBinary, "push", "-q", shim.repo, parentTip+":"+delEmptyHeadsPrefix+branch)
	if got := shim.holds(t, branch); got != parentTip {
		t.Fatalf("fixture: the peer must advertise %s at %q, it holds %q", branch, parentTip, got)
	}
	if !delEmptyPeerMasterContains(t, shim, rowTip) {
		t.Fatalf("fixture: the peer's %s must CONTAIN the row's work %q", delEmptyIntegration, rowTip)
	}

	covLifeSeedTaskRow(t, taskID,
		taskDeliverableRebuild(taskDeliverable{Repo: repo, Branch: branch, Worktree: wt}))

	rep, err := DeliveryRetryTaskID(taskID)
	if err != nil {
		t.Fatalf("delivery retry: %v", err)
	}
	if rep.Status != deliveryRetryStatusAlreadyMerged {
		t.Fatalf("status=%q want %q — the peer advertises %s at %q while its %s already contains this row's "+
			"work %q (blocked=%q detail=%q peerTip=%q)",
			rep.Status, deliveryRetryStatusAlreadyMerged, branch, parentTip, delEmptyIntegration,
			rowTip, rep.Blocked, rep.Detail, rep.PeerTip)
	}
	if rep.Owed() {
		t.Errorf("%q must not be owed — the work is on the peer (detail=%q)", deliveryRetryStatusAlreadyMerged, rep.Detail)
	}
	if rep.Blocked != "" {
		t.Errorf("an adopted row carries no debt, got blocked=%q", rep.Blocked)
	}
	// NOTHING WAS PUSHED: the peer's branch must still be where it was, so the
	// disposition is a genuine no-op rather than a delivery wearing its name.
	if got := shim.holds(t, branch); got != parentTip {
		t.Errorf("the peer's %s moved to %q during a no-op — re-pushing resurrects a removed ref", branch, got)
	}
}

// TestDeliveryRetryCloneGroundEmptyArmAdoptedIsAlreadyMerged pins the
// `already-merged` EXIT THAT LIVES INSIDE THE `Kind == empty` ARM — the one the
// sibling test of the same name stopped exercising when its assertion moved to
// `inherited` (task 01a194f2, 2026-11-01).
//
// WHY THIS TEST HAS TO EXIST SEPARATELY. The split has TWO readers of the same
// zero and the epic's own wording names both: "要么让 already-merged 出口在该
// Kind 上可达并绿". A status an arm can produce but no test reaches is
// indistinguishable from dead code — and the two ways to reach `already-merged`
// are DIFFERENT measurements, so pinning one says nothing about the other:
//
//	router ④ (before the channel runs)   the peer ADVERTISES the row's branch at
//	                                     another tip while its master already
//	                                     contains the row's work
//	                                     → TestDeliveryRetryCloneGroundPeerAdoptingTheWorkIsAlreadyMerged
//	empty arm (inside the channel)       the gate could NOT rank HEAD against the
//	                                     integration tip — HEAD *IS* the base tip,
//	                                     a ground that produced nothing of its
//	                                     own — and the peer's master contains it
//	                                     → THIS test
//
// THE SHAPE, measured rather than described. This ground is a plain clone of the
// base with NO commit of its own (the commonest clone-channel row: a ground was
// cut and delivered nothing), its tracking ref is made to contain its HEAD (the
// gate's own `rev-list --count` question, answered 0), and the peer's master is
// the base tip itself. The branch the row records is its LEDGER ADDRESS, which
// no peer advertises — so the router's ④ skips and the channel really runs. The
// disposition is the adoption one: the work at that tip is on the peer, the row
// owes nothing, and pushing would resurrect a ref the server removed.
//
// NOT A RELAXATION: the sibling arm
// (TestDeliveryRetryCloneGroundWithNothingLeftToDeliverStaysBlockedWhenThePeerLacksTheWork)
// is the same empty ground with a peer that LACKS the work, and it must stay
// blocked. The two together are the whole split.
func TestDeliveryRetryCloneGroundEmptyArmAdoptedIsAlreadyMerged(t *testing.T) {
	covLifeGitEnv(t)
	shim := delRetryPeerShim(t)

	root := t.TempDir()
	upstream := delCloneUpstream(t)
	delCloneSettings(t, upstream, upstream, root)

	// A GROUND WITH NOTHING OF ITS OWN: cloned, never committed to, its origin
	// re-pointed at the peer through the fixture's alias (the production shape —
	// the channel pushes to `origin`, and that is the host the retry asks about).
	taskID := "t-retry-clone-empty-arm-adopted"
	ground := filepath.Join(root, taskID)
	covLifeGit(t, root, covLifeGitBinary, "clone", "-q", upstream, ground)
	covLifeGit(t, ground, covLifeGitBinary, "remote", "set-url", deliveryProofOriginRemote,
		delRetryShimAlias+":~/work/GoPi")
	baseTip := strings.TrimSpace(covLifeGit(t, ground, covLifeGitBinary, "rev-parse", "HEAD"))
	// THE PEER'S MASTER IS THE BASE TIP — nothing landed on top of it, which is
	// what makes the gate UNABLE to rank HEAD against it (the `empty` kind's own
	// condition; a later commit would produce `inherited` instead).
	shim.seedPeerMaster(t, upstream)
	if !delEmptyPeerMasterContains(t, shim, baseTip) {
		t.Fatalf("fixture: the peer's %s must contain the ground's tip %q", delEmptyIntegration, baseTip)
	}
	delEmptyTrackingPointsAtRow(t, ground, baseTip)

	// AND THE PEER DOES NOT ADVERTISE THE ROW'S BRANCH — that is what lets the
	// channel run at all (the router's ④ answers early whenever the peer carries
	// `deliverable.branch`).
	own := TaskGroundBranchName(taskID)
	if own == "" {
		t.Fatal("fixture: the row must have a ledger-address branch")
	}
	if got := shim.holds(t, own); got != "" {
		t.Fatalf("fixture: the peer must NOT advertise %s, it holds %q", own, got)
	}
	covLifeSeedTaskRow(t, taskID,
		taskDeliverableRebuild(taskDeliverable{Repo: upstream, Branch: own, Worktree: ground}))

	rep, err := DeliveryRetryTaskID(taskID)
	if err != nil {
		t.Fatalf("delivery retry on a clone-ground row: %v", err)
	}
	// THE ARM WAS REACHED: the clone result is present only when the channel ran,
	// so a report without it would be a test passing on the router's earlier
	// exits instead of on the arm it names.
	if rep.Clone == nil {
		t.Fatalf("the report carries no clone-channel result — the empty arm was never reached (status=%q detail=%q)",
			rep.Status, rep.Detail)
	}
	if rep.Clone.Pushed {
		t.Errorf("this attempt must not push (the ground has nothing of its own), clone=%+v", *rep.Clone)
	}
	if rep.Status != deliveryRetryStatusAlreadyMerged {
		t.Fatalf("status=%q want %q — an empty ground whose tip the peer's %s already contains owes nothing "+
			"(blocked=%q detail=%q peerTip=%q)",
			rep.Status, deliveryRetryStatusAlreadyMerged, delEmptyIntegration, rep.Blocked, rep.Detail, rep.PeerTip)
	}
	if rep.Blocked != "" {
		t.Fatalf("a row with no debt must carry no blocked reason, got %q", rep.Blocked)
	}
	if rep.Owed() {
		t.Fatalf("%q must not be owed: Owed()=true (status=%q)", deliveryRetryStatusAlreadyMerged, rep.Status)
	}
	// THE DETAIL IS A MEASUREMENT, and it names the tip the containment was
	// measured about — the field a reader needs to re-take the reading.
	if rep.RowTip != baseTip {
		t.Errorf("the report must name the tip the peer's master contains (%q), it names %q", baseTip, rep.RowTip)
	}
	if !strings.Contains(rep.Detail, baseTip) || !strings.Contains(rep.Detail, "already contains") {
		t.Errorf("the detail must name the contained tip and the containment fact: %q", rep.Detail)
	}
	// THE SENTENCE HAS NO HOLE: the peer advertises no ref for this row's branch,
	// so a message pasting the empty tip reads "the peer's  already contains".
	if strings.Contains(rep.Detail, "  ") {
		t.Errorf("the detail carries an empty field (a doubled space): %q", rep.Detail)
	}
	if strings.HasPrefix(rep.Detail, deliveryRetryOwedMarker) {
		t.Errorf("a row with no debt must not be worded as a debt: %q", rep.Detail)
	}
	// The trail says "delivered", not "blocked", so the resident pass stops
	// retrying this row.
	if events := delRetryTrailEvents(t, taskID); !hasString(events, deliveryRetryDeliveredEvent) {
		t.Errorf("the row's trail must carry %q, got %v", deliveryRetryDeliveredEvent, events)
	}
	// AND NOTHING MOVED: the disposition is a genuine no-op, not a delivery
	// wearing its name.
	if got := shim.holds(t, own); got != "" {
		t.Errorf("the peer now advertises %s at %q — a no-op must not create a ref", own, got)
	}
	if got := strings.TrimSpace(covLifeGit(t, ground, covLifeGitBinary, "rev-parse", "HEAD")); got != baseTip {
		t.Errorf("the ground's HEAD moved to %q during a no-op, want %q", got, baseTip)
	}
}
