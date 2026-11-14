package tools

// THE WRITER-IDENTITY DECISION, PINNED AS A TEST (epic 01a1651d).
//
// Row 01a163df-a105-71b8-a297-680c5d35d093 reached `completed` through its
// SURVIVING CHILD process, not through the wrapper pid its `claimed_by`
// column named. The question this file answers, with measurements rather than
// argument, is whether a PROCESS-OWNERSHIP predicate at the write choke point
// (taskUpdateStampedCore) would add anything the DELIVERY arm
// (task_delivery_proof.go) does not already do — or whether it would refuse a
// legitimate close-out.
//
// WHAT WAS MEASURED ON THE INCIDENT ROW (2026-10-23, live ledger):
//
//	$ sqlite3 (read-only) SELECT claimed_by, proc_pid, proc_start_ticks,
//	                     in_progress_at, session_id FROM tasks WHERE task_id LIKE '01a163df%'
//	  host-a:1190977 | NULL | NULL | NULL | ''
//	$ ls -d /proc/1190977
//	  No such file or directory
//	$ SELECT count(*) FROM tasks WHERE proc_start_ticks IS NOT NULL
//	  7        -- of 1805 rows (933 of them terminal and carrying a host:pid stamp)
//
// i.e. the row carried NO process INCARNATION to compare against: the
// documented design clears proc_pid/proc_start_ticks on every transition out
// of in_progress (taskproc.go), and on this ledger 1798 of 1805 rows were never
// stamped at all. The writer is identified only by the session log
// (session_start pid 1191807 = the child), and `claimed_by` names the dead
// wrapper 1190977.
//
// WHAT THIS FILE DRIVES FOR REAL, on scratch git repos and a scratch ledger,
// through the REAL write path:
//
//  1. the DELIVERY arm — reached through TaskUpdateWithResult →
//     taskUpdate → taskUpdateStamped → taskUpdateStampedCore, which is the
//     choke point EVERY terminal write passes, so it is where the incident's
//     write would have been caught — REFUSES the incident's own shape: a clone
//     ground whose HEAD is commits ahead of the delivery point. The row stays
//     OPEN and the trail explains itself.
//
//     (The door this test USED to drive was the child-side run close-out,
//     tools.TaskRunCloseoutStaleRun. task 01a16f7c deleted that seam: it could
//     not write while the wrapper lived, and on the healthy path the row was
//     already terminal. Measuring the arm through the surviving door is the
//     point — a proof aimed at a door nobody uses proves nothing.)
//
//  2. the NO-OWNERSHIP-PREDICATE decision, as a census over the OTHER legal
//     doors (`gopi task update`, the merge executor, the merge-verify staleness
//     guard): each re-claims a row under its OWN holder and then writes it, so
//     a rule of the form "the writer pid must be the claimed pid" would refuse
//     legitimate work. The incident's own case is the sharpest: the writer that
//     legitimately finishes the work is NOT the pid the row was claimed under.
//     A rule whose false-refusal set contains the repair it was meant to protect
//     is not a guard, it is a regression — so none ships, and a future attempt
//     to add one fails the named test below.

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/nick/gopi/internal/testutil"
)

// twSeedClaimedRow inserts an OPEN row carrying a claim stamp, the way the
// claim ladder leaves one: status in_progress, claimed_by = <host>:<pid>. Raw
// SQL for the CAS-managed columns (they are not settable through the public
// API, which is the point — this fixture reproduces a stamp the ladder wrote).
func twSeedClaimedRow(t *testing.T, dbPath, id, claimedBy string) {
	t.Helper()
	dpSeedRow(t, dbPath, id, "feat: the incident's own write shape", taskStatusInProgress)
	ctx := context.Background()
	db, err := openTasksDB(ctx, dbPath)
	if err != nil {
		t.Fatalf("open ledger: %v", err)
	}
	defer db.Close()
	if _, err := db.ExecContext(ctx,
		`UPDATE tasks SET claimed_by = ?, claimed_at = ?, updated_at = ? WHERE task_id = ?`,
		claimedBy, float64(time.Now().Unix()), float64(time.Now().Unix()), id); err != nil {
		t.Fatalf("stamp claim on %s: %v", id, err)
	}
}

// TestWriterIdentity_DeliveryArmRefusesTheIncidentsOwnTerminalWrite is the
// COVERAGE half of the decision: the incident's terminal write happened on the
// ordinary success door (a `completed` write for a row whose deliverable had
// not reached its destination), and the delivery arm stands in front of that
// choke point. Nothing about process ownership is needed to stop the
// incident's harm — the write is refused with the row left OPEN and a trail
// entry naming the undelivered deliverable.
//
// THE DOOR IT DRIVES IS THE PRODUCTION ONE. Until task 01a16f7c this test
// went through the child-side run close-out (tools.TaskRunCloseoutStaleRun);
// that seam is deleted, and substituting it here would have left the arm
// measured against a door nobody uses. TaskUpdateWithResult is the door the
// WRAPPER's own post-Wait writeback reaches, and the one `gopi task update`
// reaches — the arm has to stand there.
//
// RED WHEN THE DELIVERY ARM IS NEUTRALIZED: the write returns nil, the row
// lands `completed`, and the assertions fail on "" and on the status — i.e.
// this test does not merely re-assert the arm, it measures that the arm is the
// thing standing in the door.
func TestWriterIdentity_DeliveryArmRefusesTheIncidentsOwnTerminalWrite(t *testing.T) {
	db := testutil.NewTestDBPath(t)
	ground, _, masterTip, groundTip := dpCloneChannel(t)
	repo := filepath.Join(filepath.Dir(ground), "seed")

	// The incident's claim stamp: a holder on THIS host that is measurably
	// dead (pid_max+1 — derived from the kernel, never a magic number).
	dead := hostClaimStamp(t, deadClaimPid(t))

	id := "tw-incident"
	twSeedClaimedRow(t, db, id, dead)
	dpWriteDelivery(t, db, id, repo, masterBranchName, ground)

	_, err := TaskUpdateWithResult(id, taskStatusCompleted, "work done")
	if err == nil {
		t.Fatalf("the terminal write landed on %s while its ground HEAD %s is ahead of the delivery point %s "+
			"— the delivery-proof arm is not standing in THIS door, which is the door 01a163df's "+
			"terminal status came through",
			id, branchDeleteShortHash(groundTip), branchDeleteShortHash(masterTip))
	}
	if !contains(err.Error(), deliveryProofMarker) {
		t.Errorf("the refusal must name the undelivered deliverable, got: %v", err)
	}
	if got := dpRowStatus(t, db, id); got != taskStatusInProgress {
		t.Errorf("row status after the refused write = %q, want %q (the write must not land)", got, taskStatusInProgress)
	}
	if n := dpEventCount(t, db, id, deliveryProofOwedEvent); n == 0 {
		t.Errorf("the refusal must leave a %s trail entry on the still-open row (Rule 15)", deliveryProofOwedEvent)
	}
}

// TestWriterIdentity_OtherLegalDoorsAreInvisibleToAnOwnershipRule is the second
// half of the argument, and it CONTRADICTED this epic's first guess — measured,
// not assumed. The merge executor and the merge-verify staleness guard close
// rows they re-claim under their own NON-PID holder stamps
// ("merge-executor" / "merge-verify-staleness-guard", both named constants), and
// claimHolder answers ok=false for those:
//
//	claimHolder("merge-executor")                -> host="" pid=0 ok=false
//	claimHolder("merge-verify-staleness-guard")  -> host="" pid=0 ok=false
//	claimHolder("host-a:1190977")            -> host="host-a" pid=1190977 ok=true
//
// So an ownership predicate keyed on claimed_by's pid does not REFUSE those
// doors — it cannot see them, and "no pid to judge" becomes a silent PASS. That
// is the worse half for a guard: a rule that is unsafe where it fires and blind
// where it does not is not a guard.
func TestWriterIdentity_OtherLegalDoorsAreInvisibleToAnOwnershipRule(t *testing.T) {
	for _, c := range []struct{ name, holder string }{
		{"merge-executor", mergeExecClaimHolder},
		{"merge-verify-staleness-guard", mergeVerifyStaleGuardHolder},
	} {
		if c.holder == "" {
			t.Fatalf("%s: holder stamp is empty — the door could not be identified", c.name)
		}
		host, pid, ok := claimHolder(c.holder)
		if ok || pid != 0 || host != "" {
			t.Errorf("%s holder %q now parses as a pid claim (host=%q pid=%d ok=%v) — if that changed, "+
				"re-measure this decision: an ownership rule would start REFUSING this door's legitimate "+
				"close-out instead of being blind to it", c.name, c.holder, host, pid, ok)
		}
	}
}

// contains is the substring predicate used by the assertions above (a package
// test helper, not a production symbol).
func contains(s, sub string) bool {
	if sub == "" {
		return true
	}
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
