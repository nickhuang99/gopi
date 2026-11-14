package tools

// THE RED PROOF for the REBASE half of the delivery-proof criterion
// (task_delivery_proof.go, row 01a17a67-f272-7cdf-adc1-2485d7a73a33).
//
// This file is written against the PRE-FIX API ONLY — no symbol introduced by
// the fix appears in it (the landed record is written through the pre-existing
// AppendTaskTrailEvent with the grammar typed out here) — so it can be copied
// into a detached checkout of the commit before the fix and RUN there, where it
// must FAIL and print the symptom:
//
//	the clone channel DELIVERED (its push landed), the row carries its own
//	record of that delivery, and the guard still answers `undelivered` because
//	the rebase that carried the work to the delivery point RE-DERIVED one
//	commit's content — so neither ancestry nor `git cherry` can see it.
//
// After the fix the same file PASSES. That A/B is the whole point.
//
// THE MEASURED SHAPE (the row's own two commits, verified before this file was
// written):
//
//	$ git -C <ground> cherry ecce621cd HEAD
//	- 50b57f896ecdd560ed4ed882157dfce8bf25d6bf      ← present (pure rebase)
//	+ 34f80394899406c2e3e00bf35f055b35cd48c092      ← ABSENT  (content re-derived)
//	$ git -C <ground> show 34f8039 | git patch-id --stable
//	cb843035253d6a3f453a6d12a5a1f72c76d7c7d2 34f803948994…
//	$ git -C <master> show ecce621cd | git patch-id --stable
//	4f1ab5bf8c7ccb089452d1922b8aa56c5994f586 ecce621cdfa1…   ← different
//
// The second commit is a REGENERATED TABLE measured against a moved base: the
// rebase replays it and the regeneration writes different bytes. No content
// comparison can ever match it, which is why the criterion needs the channel's
// own delivery record as a third witness.

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nickhuang99/gopi/internal/testutil"
)

const (
	zzrClock = 1792679579.0
	zzrEmail = "zzr@test"
	zzrUser  = "zzr"

	// zzrTablePath is the regenerated table the fixture commits.
	zzrTablePath = "table.tsv"
	// zzrGroundRow is the DATA row of the table as the GROUND measured it: a
	// base commit sha the ground's own measurement ran on. The delivery point
	// regenerates it against ITS base, so the reboot/land side writes a
	// different sha — the re-derived content this fixture models.
	zzrGroundRow = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	// zzrLandedRow is what the regeneration writes once the delivery point has
	// moved. It records a DIFFERENT base, so B's addition is not the byte
	// string the ground produced — the shape NO content comparison can match
	// and only the channel's own record can settle.
	zzrLandedRow = "cccccccccccccccccccccccccccccccccccccccc"
)

// zzrTableBody renders the table with ONE data row naming a base commit. The
// `measured-at:` header line is deliberately NOT the difference: it is
// normalised away by the content arm, so a fixture that differed only there
// would be answered by that arm's normaliser and never reach the record arm
// these tests exist to pin (measured: that is exactly what the pre-fix fixture
// did, and both red proofs turned green for the wrong reason).
func zzrTableBody(base string) string {
	return "# measured-at: " + base + "\n" + base + "\t1\n"
}

// zzrRebasedDelivery builds the row's own shape:
//
//	server        the delivery point (bare)
//	ground        a CLONE of it (`.git` a DIRECTORY) carrying commit A and
//	              commit B, neither of which the delivery point has as a commit
//	land          a second clone that moves master forward and then REBASES A
//	              and B onto it, RE-DERIVING B's content (the regenerated table
//	              against the new base) before pushing
//
// Returns (ground, repo, tip) where repo is the row's recorded repo (the seed)
// and tip is the commit the run produced.
func zzrRebasedDelivery(t *testing.T) (ground, repo, tip string) {
	t.Helper()
	root := t.TempDir()
	server := filepath.Join(root, "server")
	rebuildTestGit(t, root, "init", "-q", "--bare", "-b", masterBranchName, server)

	repo = filepath.Join(root, "seed")
	rebuildTestGit(t, root, "clone", "-q", server, repo)
	rebuildTestGit(t, repo, "config", "user.email", zzrEmail)
	rebuildTestGit(t, repo, "config", "user.name", zzrUser)
	if err := os.WriteFile(filepath.Join(repo, "f.txt"), []byte("base\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	rebuildTestGit(t, repo, "add", "f.txt")
	rebuildTestGit(t, repo, "commit", "-q", "-m", "base")
	rebuildTestGit(t, repo, "push", "-q", "origin", masterBranchName)

	ground = filepath.Join(root, "ground")
	rebuildTestGit(t, root, "clone", "-q", server, ground)
	rebuildTestGit(t, ground, "config", "user.email", zzrEmail)
	rebuildTestGit(t, ground, "config", "user.name", zzrUser)
	// A — a pure patch the rebase will replay identically.
	if err := os.WriteFile(filepath.Join(ground, "work.txt"), []byte("delivered by a rebase\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	rebuildTestGit(t, ground, "add", "work.txt")
	rebuildTestGit(t, ground, "commit", "-q", "-m", "feat: the run's work")
	// B — a table measured against the GROUND's base. Its DATA row is derived
	// from the base the measurement ran on, which is what a regeneration
	// re-derives: `zzrDerivedRow` names one commit, and the land below names
	// another.
	if err := os.WriteFile(filepath.Join(ground, zzrTablePath), []byte(zzrTableBody(zzrGroundRow)), 0o644); err != nil {
		t.Fatal(err)
	}
	rebuildTestGit(t, ground, "add", "table.tsv")
	rebuildTestGit(t, ground, "commit", "-q", "-m", "chore(census): regenerate the table on this tip")
	tip = rebuildTestGit(t, ground, "rev-parse", "HEAD")

	// The delivery point MOVES while the run works (somebody else's push).
	land := filepath.Join(root, "land")
	rebuildTestGit(t, root, "clone", "-q", server, land)
	rebuildTestGit(t, land, "config", "user.email", zzrEmail)
	rebuildTestGit(t, land, "config", "user.name", zzrUser)
	if err := os.WriteFile(filepath.Join(land, "other.txt"), []byte("another row\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	rebuildTestGit(t, land, "add", "other.txt")
	rebuildTestGit(t, land, "commit", "-q", "-m", "feat: another row landed first")
	rebuildTestGit(t, land, "push", "-q", "origin", masterBranchName)

	// THE DELIVERY: the channel's `pull --rebase` then `push`, replayed here.
	rebuildTestGit(t, land, "fetch", "-q", ground, refsHeadsPrefix+masterBranchName)
	rebuildTestGit(t, land, "checkout", "-q", "-B", "rebased", "FETCH_HEAD")
	rebuildTestGit(t, land, "rebase", "-q", masterBranchName)
	// THE REGENERATION: the table is re-measured against the NEW base, so this
	// commit's patch is not the one the ground produced.
	if err := os.WriteFile(filepath.Join(land, zzrTablePath),
		[]byte(zzrTableBody(zzrLandedRow)), 0o644); err != nil {
		t.Fatal(err)
	}
	rebuildTestGit(t, land, "add", zzrTablePath)
	rebuildTestGit(t, land, "commit", "-q", "--amend", "--no-edit")
	rebuildTestGit(t, land, "push", "-q", "origin", "HEAD:"+refsHeadsPrefix+masterBranchName)

	// The ground's own tracking ref follows the delivery (a clone channel
	// refreshes it on every round), so the destination IS resolvable — the
	// refusal below is about the CONTENT, not about a missing ref.
	rebuildTestGit(t, ground, "fetch", "-q", deliveryProofOriginRemote)

	// THE FIXTURE MUST REPRODUCE ITS OWN SHAPE, AND THAT IS ASSERTED HERE.
	//
	// The property these arms exist to pin is "the rebase leaves NOTHING a
	// content comparison can match" — so this asserts it, on the DATA, before
	// any arm reads the fixture. Measured 2026-10-28: the pre-fix fixture made
	// the `# measured-at:` HEADER the only difference between the ground's
	// table and the destination's, and the content arm's normaliser drops that
	// line BY DESIGN (a rebase must rewrite it) — so the content arm answered
	// `delivered`, correctly, and these two arms were green-for-the-wrong-reason
	// from the commit that introduced them until this guard existed. A fixture
	// that can rot back to that shape now fails loudly instead.
	//
	// Deliberately NO production symbol here (a `git show` of the path on each
	// side): this file must still COMPILE and RUN on the pre-fix tree, which is
	// the whole point of the red proof.
	destRef := refsRemotesPrefix + deliveryProofOriginRemote + "/" + masterBranchName
	atGround := rebuildTestGit(t, ground, "show", tip+":"+zzrTablePath)
	if !strings.Contains(atGround, zzrGroundRow) {
		t.Fatalf("fixture is wrong: the run's own table must carry the row it measured (%q), got:\n%s",
			zzrGroundRow, atGround)
	}
	atDest := rebuildTestGit(t, ground, "show", destRef+":"+zzrTablePath)
	if strings.Contains(atDest, zzrGroundRow) {
		t.Fatalf("fixture is wrong: the delivery point still carries the row the GROUND measured (%q), so a "+
			"content comparison WOULD match and neither arm below pins the shape it claims:\n%s",
			zzrGroundRow, atDest)
	}
	if !strings.Contains(atDest, zzrLandedRow) {
		t.Fatalf("fixture is wrong: the regeneration must have written the re-derived row (%q), got:\n%s",
			zzrLandedRow, atDest)
	}
	return ground, repo, tip
}

// zzrRequireTheReDerivedShape IS the assertion, and every arm asks it.
//
// WHY IT LIVES IN A HELPER. These arms each assert an EXPECTED ANSWER
// (`undelivered` in two of them, `delivered` in the third), and an expected
// `delivered` is satisfied by a fixture that never modelled the re-derivation at
// all. Measured 2026-10-28 (row 01a1824e): the fixture's only ground-vs-land
// difference was one `# measured-at:` line — the very line the content arm
// DELIBERATELY normalises away — so the shape was never present, the guard's
// `delivered` was CORRECT, and both arms were red on master for a fixture
// reason while all three were green on a baseline 33 commits behind. The check
// existed only inside the first arm, which is the arm a stale fixture turned
// red; the two arms that stayed green were the two nobody had pinned.
//
// TWO WITNESSES, both structural — never a string comparison these arms
// themselves rely on:
//   - the tip is NOT an ancestor of the delivery point (the rebase moved it);
//   - `git cherry` reports a patch it CANNOT match (`+`), i.e. no patch-id
//     comparison can answer this shape either.
func zzrRequireTheReDerivedShape(t *testing.T, ground, ref, tip string) {
	t.Helper()
	if _, err := branchDeleteGit(context.Background(), ground, "merge-base", "--is-ancestor", tip, ref); err == nil {
		t.Fatal("fixture is wrong: the tip must not be an ancestor of the delivery point")
	}
	cherry := rebuildTestGit(t, ground, "cherry", ref, tip)
	if !strings.Contains(cherry, "+ ") {
		t.Fatalf("fixture is wrong: the rebase must leave a patch git cherry cannot match\n%s", cherry)
	}
}

// TestZZRedProof_RebasedDeliveryIsUndeliveredWithoutTheChannelsRecord is the
// regression this epic closes: pre-fix the guard answers `undelivered` here.
func TestZZRedProof_RebasedDeliveryIsUndeliveredWithoutTheChannelsRecord(t *testing.T) {
	db := testutil.NewTestDBPath(t)
	ground, repo, tip := zzrRebasedDelivery(t)
	ref := refsRemotesPrefix + deliveryProofOriginRemote + "/" + masterBranchName

	zzrRequireTheReDerivedShape(t, ground, ref, tip)

	id := "zzr-rebased-without-record"
	dpSeedRow(t, db, id, "feat: the work a rebase delivered", taskStatusInProgress)
	dpWriteDelivery(t, db, id, repo, masterBranchName, ground)

	out, err := TaskDeliveryProof(id)
	if err != nil {
		t.Fatalf("probe: %v", err)
	}
	// NO RECORD WAS WRITTEN AND THE CONTENT DIFFERS: refusing is RIGHT.
	if out.State != deliveryProofUndelivered {
		t.Fatalf("state = %q, want %q — a rebase whose content was re-derived leaves NOTHING a content "+
			"comparison can match, and no delivery record exists, so this is exactly the shape the guard "+
			"must still refuse (note: %s)", out.State, deliveryProofUndelivered, out.Note)
	}
}

// TestZZRedProof_LandedRecordDeliversARederivedRebase is the GREEN half: the
// SAME fixture, plus the record the clone channel writes when its push lands.
func TestZZRedProof_LandedRecordDeliversARederivedRebase(t *testing.T) {
	db := testutil.NewTestDBPath(t)
	ground, repo, tip := zzrRebasedDelivery(t)

	// WITHOUT THIS the arm proves nothing: `delivered` is what a fixture that
	// never re-derived anything answers too (that is exactly how this arm went
	// green while its sibling stayed red — see zzrRequireTheReDerivedShape).
	zzrRequireTheReDerivedShape(t, ground, refsRemotesPrefix+deliveryProofOriginRemote+"/"+masterBranchName, tip)

	id := "zzr-rebased-with-record"
	dpSeedRow(t, db, id, "feat: the work a rebase delivered", taskStatusInProgress)
	dpWriteDelivery(t, db, id, repo, masterBranchName, ground)

	// THE CHANNEL'S OWN RECORD — written through the PRE-EXISTING trail door, in
	// the grammar this arm reads. Nothing from the fix is referenced here.
	AppendTaskTrailEvent(id, DeliveryLandedEventName, fmt.Sprintf(
		"clone channel: delivered %s from %s at %s — pushed %s to origin after 1 attempt(s)",
		masterBranchName, ground, tip, masterBranchName))

	out, err := TaskDeliveryProof(id)
	if err != nil {
		t.Fatalf("probe: %v", err)
	}
	if out.State != deliveryProofDelivered {
		t.Fatalf("state = %q, want %q: the clone channel's push LANDED and the row records it for this very "+
			"tip %s, so the delivery is proven even though the rebase re-derived the content (note: %s)",
			out.State, deliveryProofDelivered, branchDeleteShortHash(tip), out.Note)
	}
	if _, err := TaskUpdateWithResult(id, taskStatusCompleted, "work done"); err != nil {
		t.Fatalf("a delivery the channel recorded must not be refused at close-out (the row would stay OPEN "+
			"and be skipped by the reclaim), got: %v", err)
	}
	if got := dpRowStatus(t, db, id); got != taskStatusCompleted {
		t.Errorf("row status = %q, want %q", got, taskStatusCompleted)
	}
}

// TestZZRedProof_ARecordForAnotherTipDeliversNothing is the mirror that keeps
// the fix from being a loophole: ANY landed record is not enough — it has to
// name the commit being judged.
func TestZZRedProof_ARecordForAnotherTipDeliversNothing(t *testing.T) {
	db := testutil.NewTestDBPath(t)
	ground, repo, tip := zzrRebasedDelivery(t)

	// Its expected answer is `undelivered` too, but for the RECORD's reason —
	// so the fixture must still be the re-derived shape, or the refusal would
	// be right by accident and a widened content arm would go unnoticed.
	zzrRequireTheReDerivedShape(t, ground, refsRemotesPrefix+deliveryProofOriginRemote+"/"+masterBranchName, tip)

	id := "zzr-rebased-wrong-tip"
	dpSeedRow(t, db, id, "feat: the work a rebase delivered", taskStatusInProgress)
	dpWriteDelivery(t, db, id, repo, masterBranchName, ground)

	// A record about a DIFFERENT commit of the same branch: this row HAS been
	// delivered before (or something was), but not this tip.
	other := rebuildTestGit(t, ground, "rev-parse", tip+"^")
	if other == tip {
		t.Fatal("fixture is wrong: the tip must have a parent")
	}
	AppendTaskTrailEvent(id, DeliveryLandedEventName, fmt.Sprintf(
		"clone channel: delivered %s from %s at %s — pushed %s to origin after 1 attempt(s)",
		masterBranchName, ground, other, masterBranchName))

	out, err := TaskDeliveryProof(id)
	if err != nil {
		t.Fatalf("probe: %v", err)
	}
	if out.State != deliveryProofUndelivered {
		t.Fatalf("state = %q, want %q — the record names %s, NOT the tip being judged (%s); "+
			"\"this row was delivered once\" is not \"this commit was delivered\" (note: %s)",
			out.State, deliveryProofUndelivered, branchDeleteShortHash(other),
			branchDeleteShortHash(tip), out.Note)
	}
	if _, err := TaskUpdateWithResult(id, taskStatusCompleted, "work done"); err == nil {
		t.Fatal("a record naming another commit must not let an undelivered tip close")
	}
	if _, err := os.Stat(ground); err != nil {
		t.Fatalf("ground vanished: %v", err)
	}
}
