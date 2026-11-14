package tools

// task_show_prefix_refusal_test.go — the ledger SHOW seam must refuse a
// truncated task id BY SHAPE, naming the full id(s) it could have meant.
//
// THE ANOMALY (Rule 41, measured 2026-10-13 on this host):
//
//	$ gopi task list --status pending
//	#01a132d6-5ac1-7b41-aa26-ee1a0c9a178f  [pending]  SEEDED FIXTURE — …
//	$ gopi task show 01a132d6-5ac1          # the first dash group, copied
//	gopi task show error: task: no task with task_id="01a132d6-5ac1"
//
// The sentence was literally true and the operator could not act on it: it is
// the SAME sentence the seam prints for a typo, for a row that lives on a
// peer's ledger, and for a near-miss spelling — nothing said the operand was a
// PREFIX of a row this ledger holds. internal/fullid already owns that refusal
// (fullid.Refusal) and it was already reached by the session-resume seam and
// the row send/move leg (syncRowResolveTaskID), but NOT by the show seam.
//
// NOT a resolution path: a prefix is never resolved (epic full-id-no-prefix,
// Rule: internal/fullid). These tests pin the MESSAGE and pin that nothing
// resolves.

import (
	"strings"
	"testing"

	"github.com/nickhuang99/gopi/internal/testutil"
)

// TestTaskShowRefusesATruncatedIDByShape is the repro of the anomaly above.
func TestTaskShowRefusesATruncatedIDByShape(t *testing.T) {
	testutil.NewTestDBPath(t)

	const (
		full   = "01a132d6-5ac1-7b41-aa26-ee1a0c9a178f"
		prefix = "01a132d6-5ac1" // the first dash group — the intuitive copy
	)
	if _, err := TaskNew("the row whose prefix is refused", full, "", ""); err != nil {
		t.Fatalf("TaskNew: %v", err)
	}

	_, err := TaskShow(prefix)
	if err == nil {
		t.Fatalf("TaskShow(%q) succeeded — a prefix must NEVER resolve", prefix)
	}
	// ① the shape is named: the operand is not a full id.
	if !strings.Contains(err.Error(), "not a full id") {
		t.Errorf("TaskShow(%q) = %v, want a refusal whose text names the operand as not a full id", prefix, err)
	}
	// ② the full id it could have meant is named — the operator can act.
	if !strings.Contains(err.Error(), full) {
		t.Errorf("TaskShow(%q) = %v, want the full id %q named", prefix, err, full)
	}
	// ③ and the message still speaks about THIS operand, not a generic one.
	if !strings.Contains(err.Error(), prefix) {
		t.Errorf("TaskShow(%q) = %v, want the operand quoted", prefix, err)
	}
	// ④ NO row content leaked: nothing was resolved and rendered.
	if strings.Contains(err.Error(), "status:") {
		t.Errorf("TaskShow(%q) = %v, want a refusal — no row digest", prefix, err)
	}

	// The full id still resolves, unchanged (acceptance 2).
	shown, err := TaskShow(full)
	if err != nil {
		t.Fatalf("TaskShow(%q) [the full id]: %v", full, err)
	}
	if !strings.Contains(shown, full) {
		t.Errorf("TaskShow(%q) = %q, want the row's own digest", full, shown)
	}
}

// TestTaskShowNamesEveryCandidateForAnAmbiguousPrefix pins the multi-candidate
// arm: when several rows share the prefix, the refusal names them ALL rather
// than picking one (the old silent pick is the defect internal/fullid exists
// for).
func TestTaskShowNamesEveryCandidateForAnAmbiguousPrefix(t *testing.T) {
	testutil.NewTestDBPath(t)

	const (
		rowA = "01a132d6-5ac1-7b41-aa26-ee1a0c9a178f"
		rowB = "01a132d6-5ac1-7b42-aa26-ee1a0c9a178f"
		head = "01a132d6-5ac1"
	)
	for _, id := range []string{rowA, rowB} {
		if _, err := TaskNew("ambiguous prefix candidate", id, "", ""); err != nil {
			t.Fatalf("TaskNew(%q): %v", id, err)
		}
	}

	_, err := TaskShow(head)
	if err == nil {
		t.Fatalf("TaskShow(%q) succeeded — an ambiguous prefix must never resolve", head)
	}
	for _, want := range []string{rowA, rowB, "not a full id"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("TaskShow(%q) = %v, want %q named", head, err, want)
		}
	}
}

// TestTaskShowKeepsItsOrdinaryAnswerForANonPrefixOperand is the Rule 15 half:
// an operand that is NOT the uuid shape cut short keeps the seam's ordinary
// "no task with task_id" sentence with NO prefix clause invented for it. A
// wrong "did you mean" is worse than none (bug 01a130e4's precedent).
func TestTaskShowKeepsItsOrdinaryAnswerForANonPrefixOperand(t *testing.T) {
	testutil.NewTestDBPath(t)

	for _, operand := range []string{
		"t-ghost",            // a legacy id shape, not a uuid chunk
		"01a11548-tracked",   // a name, not the uuid pattern
		"zzzzzzzz",           // 8 chars (fullid.ShortPrefixLen) but not hex — not a uuid chunk
		"01a11548-6513-4651", // right shape, right length, but not a row here
	} {
		_, err := TaskShow(operand)
		if err == nil {
			t.Fatalf("TaskShow(%q) succeeded — want no such row", operand)
		}
		if !strings.Contains(err.Error(), "no task with task_id") {
			t.Errorf("TaskShow(%q) = %v, want the ordinary no-such-row answer", operand, err)
		}
		if strings.Contains(err.Error(), "did you mean") {
			t.Errorf("TaskShow(%q) = %v, want no invented spelling repair", operand, err)
		}
	}
}

// TestTaskShowStillNamesTheCanonicalSpellingForANearMiss pins that the new
// prefix clause did NOT displace the existing near-miss (dash-moved) clause at
// the same seam: order matters there, and the repair must keep firing with its
// ledger fact.
func TestTaskShowStillNamesTheCanonicalSpellingForANearMiss(t *testing.T) {
	testutil.NewTestDBPath(t)

	const (
		canonical = "01a12f96-c2bf-732f-8cd2-4b63a367d4af"
		nearMiss  = "01a12f96-c2bf-732f-8cd24b63a367d4af" // one dash dropped
	)
	if _, err := TaskNew("near-miss repair still fires", canonical, "", ""); err != nil {
		t.Fatalf("TaskNew: %v", err)
	}

	_, err := TaskShow(nearMiss)
	if err == nil {
		t.Fatalf("TaskShow(%q) succeeded — a non-canonical spelling must not resolve", nearMiss)
	}
	if !strings.Contains(err.Error(), "did you mean") {
		t.Errorf("TaskShow(%q) = %v, want the near-miss spelling repair clause", nearMiss, err)
	}
	if !strings.Contains(err.Error(), canonical) {
		t.Errorf("TaskShow(%q) = %v, want the canonical spelling %q named", nearMiss, err, canonical)
	}
	// The near-miss operand is NOT the truncated-uuid shape (its length is
	// uuid's, its dashes are wrong), so the prefix clause must NOT fire for it:
	// one operand, one refusal.
	if strings.Contains(err.Error(), "not a full id") {
		t.Errorf("TaskShow(%q) = %v, want ONE clause — the near-miss repair, never a prefix sentence too", nearMiss, err)
	}
}

// TestTaskShowCapsTheNamedCandidates pins Rule 1's named cap AND Rule 15's
// honesty: the refusal names at most a bounded number of ids, and when there
// are more it states the REAL total — a capped list is never read as the whole
// set. The bound is spelled as a literal here ON PURPOSE: this test is a guard
// for the pre-fix tree too (it must compile and FAIL against it), so it may not
// reference a constant the fix introduces.
func TestTaskShowCapsTheNamedCandidates(t *testing.T) {
	testutil.NewTestDBPath(t)

	// 20 rows share one 8-character head, above any plausible cap, so the cap is
	// exercised with real rows rather than a mocked list.
	const (
		head  = "01a1beef"
		total = 20
	)
	for i := 0; i < total; i++ {
		id := head + "-0000-0000-0000-" + strings.Repeat("0", 11) + string(rune('a'+i%26))
		if _, err := TaskNew("capped candidate", id, "", ""); err != nil {
			t.Fatalf("TaskNew(%q): %v", id, err)
		}
	}

	_, err := TaskShow(head)
	if err == nil {
		t.Fatalf("TaskShow(%q) succeeded — a prefix must never resolve", head)
	}
	// Rule 15: the capped refusal must not read as the whole set — the real
	// total is stated.
	if !strings.Contains(err.Error(), "are named above, in claim order") {
		t.Errorf("TaskShow(%q) = %v, want the REAL total stated when the named list is capped", head, err)
	}
	if !strings.Contains(err.Error(), "20") {
		t.Errorf("TaskShow(%q) = %v, want the real candidate total (20) named", head, err)
	}
}
