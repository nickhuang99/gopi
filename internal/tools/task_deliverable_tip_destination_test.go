package tools

// THE DELIVERY DESTINATION ARM (epic 01a19433, measured 2026-10-31).
//
// THE FALSE POSITIVE. The arm judged a row's work by its ADDRESS
// (`deliverable.branch` = `task/<id>`) plus whatever delivery point the
// removal-audit sink happened to record. On the CLONE channel the delivery
// destination is the INTEGRATION branch — `devworkspace.publishGround` pushes
// `HEAD:refs/heads/master` and never `refs/heads/task/<id>` — so a delivered
// run's address ref resolves NOWHERE while its work sits on `origin/master`.
// Measured twice:
//
//	① inside a real `gopi worker --resume` e2e whose own log carries
//	   `clone channel: delivery delivered — pushed master to origin after 1
//	   attempt(s)` AND `AFTER: source master = "858b4f5 …"`, while the row was
//	   reported "its deliverable's tip is reachable from no ref";
//	② two historical rows (01a18daa, 01a18d61) carrying the event whose commits
//	   are ancestors of master (`git for-each-ref --contains c64595a87` →
//	   refs/remotes/origin/master).
//
// The sibling delivery-proof arm answers UNCHECKED for the same fact, which is
// the two-arms-one-fact disagreement the fix removes.
//
// THE THREE ARMS BELOW, and the last two are what keep the repair from being
// "delete the check" (the arm's ONLY job is catching a run whose work exists in
// NO ref):
//
//	① clone-channel shape: address ref absent, destination ref carries the tip
//	   → reachable, NO `deliverable-tip-unreachable` (RED when the lookup is
//	   reverted — see the red proof in the report);
//	② the tip exists in no ref at all → the note is STILL emitted (this arm is
//	   green in BOTH states, and it is the arm that would catch a blanket
//	   disable);
//	③ the destination ref exists but does NOT carry the tip, and the tip's only
//	   company is a FOREIGN ref → not accepted (the destination is a ref, not a
//	   pass; an existing destination proves nothing).

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nickhuang99/gopi/internal/removalaudit"
	"github.com/nickhuang99/gopi/internal/testutil"
)

// tipDestinationGround builds the CLONE-CHANNEL shape this epic measured:
//
//	bare "server" (the delivery point)  ← the channel pushes HEAD:refs/heads/master
//	clone "ground"                      ← the run's ground, .git a DIRECTORY
//	clone "recorded"                    ← the repo the ROW records
//
// The ground makes ONE commit (the run's work) and does NOT push it unless
// landIt says so; the row's address branch (`task/<id>`) is never created
// anywhere, which is the channel's real behaviour.
func tipDestinationGround(t *testing.T, landIt bool, taskID string) (repo, ground, tip string) {
	t.Helper()
	root := t.TempDir()
	server := filepath.Join(root, "server")
	if err := os.MkdirAll(server, 0o755); err != nil {
		t.Fatal(err)
	}
	rebuildTestGit(t, server, "init", "-q", "--bare", "-b", IntegrationBranchName)

	seed := filepath.Join(root, "seed")
	if err := os.MkdirAll(seed, 0o755); err != nil {
		t.Fatal(err)
	}
	rebuildTestGit(t, seed, "init", "-q", "-b", IntegrationBranchName)
	rebuildTestGit(t, seed, "config", "user.email", "tipdest@test")
	rebuildTestGit(t, seed, "config", "user.name", "tipdest")
	if err := os.WriteFile(filepath.Join(seed, "f.txt"), []byte("base\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	rebuildTestGit(t, seed, "add", "f.txt")
	rebuildTestGit(t, seed, "commit", "-q", "-m", "base")
	rebuildTestGit(t, seed, "push", "-q", server, IntegrationBranchName)

	ground = filepath.Join(root, "ground")
	rebuildTestGit(t, root, "clone", "-q", server, ground)
	rebuildTestGit(t, ground, "config", "user.email", "tipdest@test")
	rebuildTestGit(t, ground, "config", "user.name", "tipdest")
	if err := os.WriteFile(filepath.Join(ground, "work.txt"), []byte("the run's own work\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	rebuildTestGit(t, ground, "add", "work.txt")
	rebuildTestGit(t, ground, "commit", "-q", "-m", "the work this run produced")
	tip = rebuildTestGit(t, ground, "rev-parse", "HEAD")

	// The CLONE discrimination, asserted so a fixture that silently became a
	// linked worktree cannot make this file pass for the wrong reason.
	if info, err := os.Stat(filepath.Join(ground, dpGroundGit)); err != nil || !info.IsDir() {
		t.Fatalf("fixture must be a CLONE ground (.git a directory): %v", err)
	}
	if landIt {
		// THE CHANNEL'S OWN REFSPEC, verbatim: the integration branch, never the
		// row's address. This is the fact the arm did not read.
		rebuildTestGit(t, ground, "push", "-q", deliveryProofOriginRemote, "HEAD:refs/heads/"+IntegrationBranchName)
	}
	repo = filepath.Join(root, "recorded")
	rebuildTestGit(t, root, "clone", "-q", server, repo)
	return repo, ground, tip
}

// tipDestinationRow seeds a row whose deliverable records the ADDRESS
// (`task/<id>`), the ground, and the repo — the row shape the channel produces.
func tipDestinationRow(t *testing.T, taskID, repo, ground string) string {
	t.Helper()
	dbPath := memoryDBPath()
	dpSeedRow(t, dbPath, taskID, "feat: the channel delivered this run's work", taskStatusInProgress)
	branch := TaskGroundBranchName(taskID)
	dpWriteDelivery(t, dbPath, taskID, repo, branch, ground)
	return branch
}

// TestDeliverableTip_DestinationCarriesACloneChannelsDelivery is ARM ①.
//
// RED WHEN THE DESTINATION LOOKUP IS REVERTED (task_deliverable_tip.go, step
// ④b): the address ref `task/<id>` resolves nowhere (the channel never creates
// it), no delivery point and no removal-audit record exists for the row, and
// the verdict falls to `unreachable` — the epic's measured sentence — even
// though `refs/remotes/origin/master` in the row's own ground holds the tip.
func TestDeliverableTip_DestinationCarriesACloneChannelsDelivery(t *testing.T) {
	db := testutil.NewTestDBPath(t)
	const taskID = "01a19433tipdestlnd"
	// The removal-audit sink is PRIVATE and EMPTY: the real incident's state —
	// nothing recorded a delivery point for the row, so the fix must not depend
	// on one being there.
	t.Setenv(removalaudit.EnvSink, filepath.Join(t.TempDir(), "removal-audit.log"))
	repo, ground, tip := tipDestinationGround(t, true, taskID)
	branch := tipDestinationRow(t, taskID, repo, ground)

	// The fixture's PRECONDITIONS, asserted rather than assumed: the address ref
	// exists nowhere, and the ground's own tracking ref holds the tip.
	if got := strings.TrimSpace(covLifeGit(t, ground, covLifeGitBinary, "for-each-ref", "--format=%(refname)", "refs/heads/"+branch)); got != "" {
		t.Fatalf("fixture: the ADDRESS ref %s must not exist (the channel never creates it), got %q", branch, got)
	}
	if got := strings.TrimSpace(covLifeGit(t, ground, covLifeGitBinary, "rev-parse", "--verify", "--quiet",
		refsRemotesPrefix+deliveryProofOriginRemote+"/"+IntegrationBranchName)); got != tip {
		t.Fatalf("fixture: the ground's origin/%s must be the pushed tip %s, got %q", IntegrationBranchName, tip, got)
	}
	if got := strings.TrimSpace(covLifeGit(t, ground, covLifeGitBinary, "for-each-ref", "--contains", tip, "--format=%(refname)")); !strings.Contains(got, refsRemotesPrefix) {
		t.Fatalf("fixture: the tip must be held by the delivery destination, got %q", got)
	}

	out, err := TaskDeliverableTipReachable(taskID)
	if err != nil {
		t.Fatalf("judge: %v", err)
	}
	if out.Verdict == deliverableTipVerdictUnreachable {
		t.Fatalf("FALSE POSITIVE: the channel delivered this run's work to %s (the ground's own "+
			"refs/remotes/origin/%s is %s), yet the row is judged unreachable — this is the epic's\n"+
			"measured sentence, produced for a delivered run:\n  %s", IntegrationBranchName, IntegrationBranchName, tip, out.Note)
	}
	if out.Verdict != deliverableTipVerdictReachable {
		t.Fatalf("verdict = %q, want %q (note=%s)", out.Verdict, deliverableTipVerdictReachable, out.Note)
	}
	if out.Destination == "" {
		t.Errorf("the reachable verdict must NAME the destination that carried the tip, got empty")
	}
	if strings.Contains(out.Note, deliverableTipUnreachableMarker) {
		t.Fatalf("the note must never wear the defect sentence on a delivered row: %q", out.Note)
	}

	// And the WRITEBACK files nothing: the whole point of the repair is that the
	// trail stays silent for a delivered row.
	if _, err := TaskRunWriteback(taskID, TaskStatusCompleted, "done — elapsed=1s"); err != nil {
		t.Fatalf("writeback: %v", err)
	}
	if detail := lastEventDetail(t, taskID, deliverableTipUnreachableEvent); detail != "" {
		t.Fatalf("a DELIVERED row must carry no %s record — got %q", deliverableTipUnreachableEvent, detail)
	}
	if got := dpEventCount(t, db, taskID, deliverableTipUnreachableEvent); got != 0 {
		t.Fatalf("event count for %s = %d, want 0", deliverableTipUnreachableEvent, got)
	}
}

// TestDeliverableTip_UnlandedTipAtACloneShapeIsStillReported is ARM ②.
//
// The REVERSE arm, and it is green in BOTH states on purpose: it is what would
// catch a repair that disabled the arm instead of narrowing it. Same clone
// shape, same address-only deliverable, same empty audit sink — but the channel
// NEVER PUSHED, and the run's ground is then reclaimed (the ground's own branch
// and tracking refs go with it), so the tip's only company is the object store:
// exactly the shape the arm was built for (row 01a13ae1).
func TestDeliverableTip_UnlandedTipAtACloneShapeIsStillReported(t *testing.T) {
	db := testutil.NewTestDBPath(t)
	const taskID = "01a19433tipdestunl"
	t.Setenv(removalaudit.EnvSink, filepath.Join(t.TempDir(), "removal-audit.log"))
	repo, ground, tip := tipDestinationGround(t, false, taskID)
	tipDestinationRow(t, taskID, repo, ground)

	// The push never happened, the run's own branch was consumed, and the ground
	// is left DETACHED with every branch and tracking ref removed — so the tip
	// is a live object that NO ref names, while its sha is still readable (the
	// ground's HEAD), which is the strongest form of this arm: a KNOWN tip with
	// no holder at all.
	rebuildTestGit(t, ground, "checkout", "-q", "--detach")
	rebuildTestGit(t, ground, "update-ref", "-d", "refs/heads/"+IntegrationBranchName)
	rebuildTestGit(t, ground, "update-ref", "-d", refsRemotesPrefix+deliveryProofOriginRemote+"/"+IntegrationBranchName)
	if got := strings.TrimSpace(covLifeGit(t, ground, covLifeGitBinary, "for-each-ref", "--contains", tip, "--format=%(refname)")); got != "" {
		t.Fatalf("fixture: the ground must hold no ref naming the tip, got %q", got)
	}
	if got := strings.TrimSpace(covLifeGit(t, ground, covLifeGitBinary, "rev-parse", "HEAD")); got != tip {
		t.Fatalf("fixture: the ground's HEAD must still be the tip %s, got %q", tip, got)
	}
	if got := strings.TrimSpace(covLifeGit(t, ground, covLifeGitBinary, "for-each-ref", "--contains", tip, "--format=%(refname)")); got != "" {
		t.Fatalf("fixture: no ref may contain the unreachable tip, got %q", got)
	}
	if got := strings.TrimSpace(covLifeGit(t, repo, covLifeGitBinary, "rev-parse", "--verify", "--quiet",
		refsRemotesPrefix+deliveryProofOriginRemote+"/"+IntegrationBranchName)); got == tip {
		t.Fatalf("fixture: the destination must NOT hold the tip")
	}

	out, err := TaskDeliverableTipReachable(taskID)
	if err != nil {
		t.Fatalf("judge: %v", err)
	}
	if out.Verdict != deliverableTipVerdictUnreachable {
		t.Fatalf("a tip in NO ref must still be judged %q (the arm's only job); got %q (%s)",
			deliverableTipVerdictUnreachable, out.Verdict, out.Note)
	}
	if !strings.Contains(out.Note, deliverableTipUnreachableMarker) {
		t.Fatalf("the note must carry the defect sentence, got %q", out.Note)
	}
	// THE ACT, exercised through this arm's OWN recorder. The writeback is NOT
	// used here on purpose, and the reason is measured: the sibling delivery-proof
	// arm refuses the same writeback first (it prints
	// `delivery proof: REFUSED … the work exists only in the run's own ground`),
	// so driving the writeback would assert on THAT arm's verdict instead of
	// this one's. Two arms, two facts — this test measures the one it is about.
	if note := deliverableTipRecord(taskID, out); note == "" {
		t.Fatalf("SILENT LOSS: the arm produced no sentence for a tip that is in no ref")
	}
	if detail := lastEventDetail(t, taskID, deliverableTipUnreachableEvent); detail == "" {
		t.Fatalf("SILENT LOSS: nothing on the row's trail says its work is unreachable")
	}
	if got := dpEventCount(t, db, taskID, deliverableTipUnreachableEvent); got == 0 {
		t.Fatalf("event count for %s = 0, want >= 1", deliverableTipUnreachableEvent)
	}
}

// TestDeliverableTip_ForeignRefIsNotADeliveryPoint is ARM ③.
//
// The tip exists, the destination ref exists — and the destination does NOT
// carry the tip; the commit's only company is a FOREIGN ref (an unrelated
// branch on the server). The destination must not be read as a pass, and the
// foreign ref must not be accepted as a delivery: this is the arm that refuses
// "a ref exists somewhere, therefore delivered".
func TestDeliverableTip_ForeignRefIsNotADeliveryPoint(t *testing.T) {
	const taskID = "01a19433tipdestfrn"
	t.Setenv(removalaudit.EnvSink, filepath.Join(t.TempDir(), "removal-audit.log"))
	repo, ground, tip := tipDestinationGround(t, false, taskID)
	tipDestinationRow(t, taskID, repo, ground)

	// The commit is pushed to a ref that is NOT the delivery destination, and
	// the local branch that held it is removed, so no destination carries it.
	rebuildTestGit(t, ground, "push", "-q", deliveryProofOriginRemote, "HEAD:refs/heads/"+taskForeignRefName)
	rebuildTestGit(t, ground, "checkout", "-q", "--detach")
	rebuildTestGit(t, ground, "update-ref", "-d", "refs/heads/"+IntegrationBranchName)
	rebuildTestGit(t, repo, "fetch", "-q", deliveryProofOriginRemote)
	if got := strings.TrimSpace(covLifeGit(t, repo, covLifeGitBinary, "rev-parse", "--verify", "--quiet",
		refsRemotesPrefix+deliveryProofOriginRemote+"/"+IntegrationBranchName)); got == tip {
		t.Fatalf("fixture: the DESTINATION must not carry the tip")
	}
	if got := strings.TrimSpace(covLifeGit(t, repo, covLifeGitBinary, "rev-parse", "--verify", "--quiet",
		refsRemotesPrefix+deliveryProofOriginRemote+"/"+taskForeignRefName)); got != tip {
		t.Fatalf("fixture: the foreign ref must hold the tip, got %q", got)
	}

	out, err := TaskDeliverableTipReachable(taskID)
	if err != nil {
		t.Fatalf("judge: %v", err)
	}
	if out.Verdict == deliverableTipVerdictReachable && strings.Contains(out.Destination, IntegrationBranchName) {
		t.Fatalf("the destination %s does NOT contain %s — a destination that exists is not a destination "+
			"that CARRIES the tip (note=%s)", out.Destination, tip, out.Note)
	}
	if out.Verdict == deliverableTipVerdictUnreachable && !strings.Contains(out.Note, deliverableTipUnreachableMarker) {
		t.Fatalf("the unreachable verdict must carry its own sentence, got %q", out.Note)
	}
	// The load-bearing assertion: whatever the verdict, the FOREIGN ref was never
	// named as the destination that carried the tip.
	if strings.Contains(out.Destination, taskForeignRefName) {
		t.Fatalf("a FOREIGN ref must never be read as this row's delivery destination — got %q", out.Destination)
	}
	if out.Verdict == deliverableTipVerdictReachable {
		// Reachable through the foreign ref's ancestry is the arm's own definition
		// of reachable (its job is a tip in NO ref) — but the foreign ref must
		// never be reported as a DELIVERY, which is what Destination means.
		if out.Destination != "" {
			t.Fatalf("the destination must stay empty when no delivery destination carries %s — got %q", tip, out.Destination)
		}
		if len(out.Holders) == 0 {
			t.Fatalf("a reachable verdict must name the ref that holds %s", tip)
		}
	}
}

// taskForeignRefName is the unrelated ref arm ③ parks the tip on. A name no
// channel of this repository produces (no `task/` prefix, no integration
// branch), so nothing in the arm may mistake it for a delivery destination.
const taskForeignRefName = "someone-elses-topic"
