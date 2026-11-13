package tools

// ledger_seam_prefix_refusal_test.go — EVERY ledger seam that answers a
// missing row must answer a TRUNCATED canonical uuid the same way the
// `task show` seam already does: refuse it BY SHAPE and name the full id it
// could have meant.
//
// THE GAP (epic sibling of devdoc/show-prefix-refusal.md, commit ec2fa309b):
// that commit taught ONE seam (`taskShowWithDB`) to reach the shared refusal
// (taskIDPrefixRefusal / internal/fullid.Refusal). Its siblings kept printing
// their generic "no task with task_id=%q" sentence for the same operand — the
// first dash group, the intuitive thing to copy out of a listing. An operator
// therefore got "you gave me a fragment" and "this ledger holds no such row"
// rendered identically on five other seams.
//
// This file pins the seam set. It is a MESSAGE-ONLY contract: nothing here
// resolves a prefix, and each test also asserts the operand still fails.
//
// Every test seeds its ledger with testutil.NewTestDBPath (Rule 32) and uses a
// prefix spelled as a LITERAL, because this file must also compile and FAIL
// against the pre-fix tree — referencing a constant the fix introduces would
// make it a compile error there instead of a red test.

import (
	"strings"
	"testing"

	"github.com/nick/gopi/internal/testutil"
)

// assertShapeRefusal is the ONE assertion every seam shares: the operand is
// named as not a full id, the full id it could have meant is named, and the
// operand itself is still quoted.
func assertShapeRefusal(t *testing.T, seam string, err error, fullID, prefix string) {
	t.Helper()
	if err == nil {
		t.Fatalf("%s(%q) returned no error — a prefix must NEVER resolve", seam, prefix)
	}
	msg := err.Error()
	if !strings.Contains(msg, "not a full id") {
		t.Errorf("%s(%q) = %v, want the operand named as not a full id", seam, prefix, msg)
	}
	if !strings.Contains(msg, fullID) {
		t.Errorf("%s(%q) = %v, want the full id %q named so the operator can act", seam, prefix, msg, fullID)
	}
	if !strings.Contains(msg, prefix) {
		t.Errorf("%s(%q) = %v, want the operand quoted", seam, prefix, msg)
	}
}

// assertOrdinaryAnswer is the Rule 15 half, shared with the show seam: an
// operand that is NOT the canonical-uuid shape cut short keeps the seam's own
// sentence, with no prefix clause and no invented spelling advice.
func assertOrdinaryAnswer(t *testing.T, seam string, err error, operand string) {
	t.Helper()
	if err == nil {
		t.Fatalf("%s(%q) returned no error — want no such row", seam, operand)
	}
	msg := err.Error()
	if !strings.Contains(msg, "no task with task_id") {
		t.Errorf("%s(%q) = %v, want the seam's ordinary no-such-row answer", seam, operand, msg)
	}
	if strings.Contains(msg, "not a full id") {
		t.Errorf("%s(%q) = %v, want NO prefix clause for a non-prefix operand", seam, operand, msg)
	}
	if strings.Contains(msg, "did you mean") {
		t.Errorf("%s(%q) = %v, want no invented spelling repair", seam, operand, msg)
	}
}

// TestLedgerSeamsRefuseATruncatedTaskIDByShape walks every seam the epic names
// that can reach a ctx + *sql.DB at its refusal site, and asserts each one
// reaches the shared phrasing for the SAME operand.
func TestLedgerSeamsRefuseATruncatedTaskIDByShape(t *testing.T) {
	testutil.NewTestDBPath(t)

	const (
		full   = "01a1336d-5028-7459-8e9a-28ce87c2b0d2"
		prefix = "01a1336d-5028" // the first two dash groups — the intuitive copy
	)
	if _, err := TaskNew("a row whose prefix must be refused by every seam", full, "", ""); err != nil {
		t.Fatalf("TaskNew(%q): %v", full, err)
	}

	// ① the `task status` seam (TaskProcessStatusFor) — reached by
	// `gopi task status <id>` and by the resume gate's tri-factor verdict.
	if _, err := TaskProcessStatusFor(prefix); err != nil {
		assertShapeRefusal(t, "TaskProcessStatusFor", err, full, prefix)
	} else {
		t.Errorf("TaskProcessStatusFor(%q) returned no error — a prefix must never resolve", prefix)
	}

	// ② the run door's pre-spawn guard (TaskRunGuardFor) — reached by
	// `gopi worker <id>` and by `gopi task run <id>`.
	if _, _, err := TaskRunGuardFor(prefix); err != nil {
		assertShapeRefusal(t, "TaskRunGuardFor", err, full, prefix)
	} else {
		t.Errorf("TaskRunGuardFor(%q) returned no error — a prefix must never resolve", prefix)
	}

	// ③ the claim path's ghost-id refusal (taskRunGuardResolvable, reached
	// through TaskClaimExplicitForce's Shape ② branch) — the "refusing to run
	// a ghost id" sentence.
	err := TaskClaimExplicitForce(prefix, "test:seam", false)
	if err == nil {
		t.Errorf("TaskClaimExplicitForce(%q) returned no error — a prefix must never resolve", prefix)
	} else {
		assertShapeRefusal(t, "TaskClaimExplicitForce", err, full, prefix)
		if !strings.Contains(err.Error(), "refusing to run a ghost id") {
			t.Errorf("TaskClaimExplicitForce(%q) = %v, want the ghost-id sentence kept", prefix, err)
		}
	}

	// ④ the status reader (TaskStatusOf) — the ledger's existence verdict,
	// reached by `gopi worker --resume <id>` through TaskResumeGroundForTask.
	status, err := TaskStatusOf(prefix)
	if err == nil {
		t.Errorf("TaskStatusOf(%q) = %q, no error — a prefix must never resolve", prefix, status)
	} else {
		assertShapeRefusal(t, "TaskStatusOf", err, full, prefix)
	}

	// ⑤ the session-id reader (TaskSessionID) — reached by the worker's
	// session publication and by `gopi task run`'s target resolution.
	sid, err := TaskSessionID(prefix)
	if err == nil {
		t.Errorf("TaskSessionID(%q) = %q, no error — a prefix must never resolve", prefix, sid)
	} else {
		assertShapeRefusal(t, "TaskSessionID", err, full, prefix)
	}

	// ⑥ the liveness predicate (TaskProcessLiveness) — the exported
	// tri-factor verdict over a row's recorded process identity.
	if _, _, _, _, lerr := TaskProcessLiveness(prefix); lerr == nil {
		t.Errorf("TaskProcessLiveness(%q) returned no error — a prefix must never resolve", prefix)
	} else {
		assertShapeRefusal(t, "TaskProcessLiveness", lerr, full, prefix)
	}

	// The full id still resolves on every seam that resolves anything: the
	// refusal is about the OPERAND's shape, never about the row.
	if got, err := TaskStatusOf(full); err != nil || got != taskStatusInProgress {
		t.Errorf("TaskStatusOf(%q) [the full id] = %q, %v — want the row's own status", full, got, err)
	}
	if sid, err := TaskSessionID(full); err != nil {
		t.Errorf("TaskSessionID(%q) [the full id]: %v", full, err)
	} else if sid != "" {
		t.Logf("TaskSessionID(%q) = %q", full, sid)
	}
	if _, _, err := TaskRunGuardFor(full); err != nil {
		t.Errorf("TaskRunGuardFor(%q) [the full id]: %v — a pending row is 'free', not an error", full, err)
	}
}

// TestLedgerSeamsKeepTheirOrdinaryAnswerForANonPrefixOperand is the boundary
// half, on the seams that carry a status/reader sentence: an operand that is
// not the canonical uuid shape cut short keeps each seam's own answer.
func TestLedgerSeamsKeepTheirOrdinaryAnswerForANonPrefixOperand(t *testing.T) {
	testutil.NewTestDBPath(t)

	for _, operand := range []string{
		"t-ghost",          // a legacy id shape, not a uuid chunk
		"01a11548-tracked", // a name, not the uuid pattern
		"zzzzzzzz",         // 8 chars but not hex — not a uuid chunk
		// A truncated uuid chunk (`01a11548-6513-4651`) is deliberately NOT in
		// this list: it belongs to the OTHER arm (the prefix refusal) and is
		// covered by TestLedgerSeamsRefusesATruncatedTaskIDByShape. A
		// full-length uuid with no row is the third arm, tested below.
	} {
		if _, err := TaskProcessStatusFor(operand); err == nil {
			t.Errorf("TaskProcessStatusFor(%q) returned no error — want no such row", operand)
		} else {
			assertOrdinaryAnswer(t, "TaskProcessStatusFor", err, operand)
		}
		if _, err := TaskStatusOf(operand); err == nil {
			t.Errorf("TaskStatusOf(%q) returned no error — want no such row", operand)
		} else {
			assertOrdinaryAnswer(t, "TaskStatusOf", err, operand)
		}
		if _, err := TaskSessionID(operand); err == nil {
			t.Errorf("TaskSessionID(%q) returned no error — want no such row", operand)
		} else {
			assertOrdinaryAnswer(t, "TaskSessionID", err, operand)
		}
		if _, _, err := TaskRunGuardFor(operand); err == nil {
			t.Errorf("TaskRunGuardFor(%q) returned no error — want no such row", operand)
		} else {
			assertOrdinaryAnswer(t, "TaskRunGuardFor", err, operand)
		}
		if _, _, _, _, err := TaskProcessLiveness(operand); err == nil {
			t.Errorf("TaskProcessLiveness(%q) returned no error — want no such row", operand)
		} else {
			assertOrdinaryAnswer(t, "TaskProcessLiveness", err, operand)
		}
	}
}

// TestLedgerSeamsKeepTheirOrdinaryAnswerForAFullLengthUnknownID is the third
// arm, and the one that most looks like the defect: a FULL-length canonical
// uuid that names no row on this ledger (pruned, or a peer's row) is NOT a
// prefix — it keeps the ordinary sentence with no "not a full id" clause. The
// prefix refusal is about the OPERAND's shape, never about a missing row.
func TestLedgerSeamsKeepTheirOrdinaryAnswerForAFullLengthUnknownID(t *testing.T) {
	testutil.NewTestDBPath(t)

	const unknown = "01a11548-6513-4651-8e9a-28ce87c2b0d2" // full shape, no row here
	if _, err := TaskStatusOf(unknown); err == nil {
		t.Errorf("TaskStatusOf(%q) returned no error — want no such row", unknown)
	} else {
		assertOrdinaryAnswer(t, "TaskStatusOf", err, unknown)
	}
	if _, err := TaskSessionID(unknown); err == nil {
		t.Errorf("TaskSessionID(%q) returned no error — want no such row", unknown)
	} else {
		assertOrdinaryAnswer(t, "TaskSessionID", err, unknown)
	}
	if _, _, err := TaskRunGuardFor(unknown); err == nil {
		t.Errorf("TaskRunGuardFor(%q) returned no error — want no such row", unknown)
	} else {
		assertOrdinaryAnswer(t, "TaskRunGuardFor", err, unknown)
	}
}

// TestLedgerSeamsRefuseAnAmbiguousPrefix pins the multi-candidate arm on a
// non-show seam: when several rows share the prefix, EVERY candidate is named
// and none is picked. Two seams are exercised rather than one, because the
// candidate scan is shared and a second caller is what proves it.
func TestLedgerSeamsRefuseAnAmbiguousPrefix(t *testing.T) {
	testutil.NewTestDBPath(t)

	const (
		rowA = "01a1336d-5028-7459-8e9a-28ce87c2b0d1"
		rowB = "01a1336d-5028-7459-8e9a-28ce87c2b0d2"
		head = "01a1336d-5028"
	)
	for _, id := range []string{rowA, rowB} {
		if _, err := TaskNew("ambiguous prefix candidate", id, "", ""); err != nil {
			t.Fatalf("TaskNew(%q): %v", id, err)
		}
	}

	for _, seam := range []struct {
		name string
		call func() error
	}{
		{"TaskStatusOf", func() error { _, err := TaskStatusOf(head); return err }},
		{"TaskSessionID", func() error { _, err := TaskSessionID(head); return err }},
		{"TaskRunGuardFor", func() error { _, _, err := TaskRunGuardFor(head); return err }},
	} {
		err := seam.call()
		if err == nil {
			t.Errorf("%s(%q) returned no error — an ambiguous prefix must never resolve", seam.name, head)
			continue
		}
		for _, want := range []string{rowA, rowB, "not a full id"} {
			if !strings.Contains(err.Error(), want) {
				t.Errorf("%s(%q) = %v, want %q named", seam.name, head, err, want)
			}
		}
		// NO row content leaked: nothing was resolved and rendered. The seam's
		// own sentence is the only place the word "status" may appear (e.g.
		// "task status: no task with task_id=…"), so the digest markers are
		// what is looked for, never the bare word.
		for _, leak := range []string{"\nstatus:", "\nsummary:", "\npayload:"} {
			if strings.Contains(err.Error(), leak) {
				t.Errorf("%s(%q) = %v, want a refusal — no row digest", seam.name, head, err)
			}
		}
	}
}
