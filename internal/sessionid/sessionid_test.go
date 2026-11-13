package sessionid

import (
	"strings"
	"testing"
)

// TestIsAdmissibleIsTheShapeRule pins the rule itself, in one table, so the
// seam tests (internal/agent, internal/tools) test the SEAMS and this one tests
// the definitions they share. The cases are the spellings this tree really
// holds — including the near-miss that is NOT a task id (the branch/worktree
// spelling) and the many non-uuid ids that ARE, which a whitelist would have
// broken.
func TestIsAdmissibleIsTheShapeRule(t *testing.T) {
	for _, tc := range []struct {
		name string
		id   string
		want bool
	}{
		{"canonical uuid row id", "01a12dc6-f44d-7347-9b95-2382a4a7d352", true},
		{"legacy t-<epoch>", "t-1788146419", true},
		{"expanded t-<epoch>-<n>", "t-1788146419-2", true},
		{"auxiliary channel t-worker-…", "t-worker-stale", true},
		{"branch/worktree spelling (dashes dropped)", "01a12dc6f44d73479b952382a4a7d352", false},
		{"rendered-list prefix", "01a12dc6", true},
		// The shapes this tree really uses for task ids in tests and scratch
		// vocabulary: admitted, because refusing them would break the paths that
		// resolve a run's session directory in order to REPORT a failure.
		{"test id with a suffix", "01a11548-tracked", true},
		{"test id with a suffix (ignored)", "01a11548-ignored", true},
		{"test id, bare epoch-ish prefix", "01a11548", true},
		{"test id, non-uuid body", "01a09aaa-dead-beef-cafe-000000000001", true},
		{"test id, zero body", "0000-emptydir", true},
		{"test id, short", "01a0a392", true},
		// A uuid's 16 bytes in a NON-CANONICAL grouping is the SAME class as the
		// dashless spelling above: joining it mints a second container beside the
		// row's own. This row's pin REVERSED (was true) when the gate widened
		// from "no dash at all" to "any non-canonical grouping" — see the
		// commit message for the argument.
		{"32 hex with a dash (a uuid's bytes, non-canonical grouping)", "01a12c9f-5a1b2c3d4e5f60718293a4b5", false},
		{"a name, not an id", "capture-round-probe", true},
		{"uuid with the wrong grouping (5 dashes, 8-4-4-4-11-1)", "01a12dc6-f44d-7347-9b95-2382a4a7d35-2", false},
		// The spellings the LIVE defect was made of (measured on this host:
		// 22 containers under ~/.gopi/sessions, each with a canonical twin):
		// a uuid with one dash dropped, with two dropped, and with a grouping
		// that is not the canonical one at all.
		{"live near-miss: 4th dash dropped (len 35)", "01a12f96-c2bf-732f-8cd24b63a367d4af", false},
		{"live near-miss: last two dashes dropped", "01a127d7fdc3-7a07-a6c9-40589236fd8b", false},
		{"live near-miss: 12-4-4-12 grouping", "01a0dff1-21cb-722e9d2e8bfd9d3ac894", false},
		// THE EXTRA-DIGIT FAMILY (measured 2026-10-17, task 01a1452c): the same
		// uuids' bytes with ONE hex digit too many (33 under the dashes, 37
		// characters, 5 dashes). This was the shape the length-keyed blacklist
		// missed — it mints a second session directory exactly like the grouping
		// miss, and the run then reads its own evidence back under the canonical
		// name and reports files that "vanished from a live session directory".
		{"live near-miss: one hex digit too many (33 hex, 5 dashes)", "01a1452c-928d-7e0f9-9ef3-52c1482cfbc3", false},
		{"live near-miss: a lone dash-group (dashes moved, 32 hex)", "01a1452c-928d-7e0f-9-ef3-52c1482cfbc3", false},
		// The conservative cost, stated rather than hidden: an ANONYMOUS 33-hex
		// operand is refused too, because the near-miss above is
		// indistinguishable from it by shape — and the repair (which digit to
		// delete) is not derivable from the operand, so no "did you mean" is
		// printed for this arm.
		{"anonymous 33-hex token", "01a1452c928d7e0f9ef352c1482cfbc3a", false},
		{"uppercase uuid", "01A12DC6-F44D-7347-9B95-2382A4A7D352", true},
		{"empty", "", false},
	} {
		if got := IsAdmissible(tc.id); got != tc.want {
			t.Errorf("IsAdmissible(%q) [%s] = %v, want %v", tc.id, tc.name, got, tc.want)
		}
	}
}

// TestRefuseNamesTheRepairAndTheCanonicalIdIsReachable pins the hint: the
// dash-stripped spelling is named back as the row id it meant, and the id it
// names is itself admissible — a "did you mean" that would also be refused is
// not a repair. A refusal an operator cannot act on is the silence this package
// exists to end.
func TestRefuseNamesTheRepairAndTheCanonicalIdIsReachable(t *testing.T) {
	undashed := Refuse("01a12dc6f44d73479b952382a4a7d352")
	if !strings.Contains(undashed.Error(), "01a12dc6-f44d-7347-9b95-2382a4a7d352") {
		t.Errorf("Refuse(branch spelling) = %q, want the canonical row id named as the repair", undashed)
	}
	if !IsCanonical("01a12dc6-f44d-7347-9b95-2382a4a7d352") {
		t.Error("the id named by the refusal is itself inadmissible — the hint must be reachable")
	}

	// The widened half: a uuid's 16 bytes with the WRONG dashes (one dropped,
	// two dropped, re-grouped) is the same class, and its refusal names the same
	// canonical row id — the spelling the ledger, the branch and the container
	// of the row that already exists all use. These are the shapes measured on
	// this host (22 containers, each with a canonical twin).
	for _, tc := range []struct{ name, id, want string }{
		{"one dash dropped", "01a12f96-c2bf-732f-8cd24b63a367d4af", "01a12f96-c2bf-732f-8cd2-4b63a367d4af"},
		{"last two dashes dropped", "01a127d7fdc3-7a07-a6c9-40589236fd8b", "01a127d7-fdc3-7a07-a6c9-40589236fd8b"},
		{"re-grouped 12-4-4-12", "01a0dff1-21cb-722e9d2e8bfd9d3ac894", "01a0dff1-21cb-722e-9d2e-8bfd9d3ac894"},
		{"a uuid's bytes, malformed grouping", "01a12c9f-5a1b2c3d4e5f60718293a4b5", "01a12c9f-5a1b-2c3d-4e5f-60718293a4b5"},
	} {
		if IsAdmissible(tc.id) {
			t.Errorf("IsAdmissible(%q) [%s] = true, want false", tc.id, tc.name)
			continue
		}
		if msg := Refuse(tc.id).Error(); !strings.Contains(msg, tc.want) {
			t.Errorf("Refuse(%q) [%s] = %q, want the canonical row id %q named as the repair", tc.id, tc.name, msg, tc.want)
		}
	}

	// An id with no near-miss carries no invented hint: the sentence states the
	// rule and stops (a wrong "did you mean" is worse than none — Rule 15).
	for _, id := range []string{"zzzz", "01a12dc6f44d73479b952382a4a7d35"} {
		if msg := Refuse(id).Error(); strings.Contains(msg, "did you mean") {
			t.Errorf("Refuse(%q) = %q, want no near-miss invented for an id that has none", id, msg)
		}
	}
}
