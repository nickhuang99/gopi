// dashless_container_claim_test.go — the regression guard for the SECOND shape
// the container test could not see: a uuid's 32 hex characters with NO separator
// at all (the branch/worktree stem spelling).
//
// THE MEASURED STATE (2026-10-17, host host-a, task
// 01a144001e4b7086b97d257497a832ed; evidence:
// `gopi tool scratch-gc trees --dry-run` + a stat of the sessions root). The
// sessions root carried 25 root-level directories named exactly this way.
// Measured facts about them, one by one:
//
//   - ALL 25 had a CANONICAL twin beside them (25 of 25) — so the dash-less name
//     is not "the same container, differently spelled", it is a SECOND
//     directory a run minted for itself out of its own id;
//   - one twin was written 40 HOURS AFTER its dash-less copy (the dash-less
//     directory is not the older, superseded spelling);
//   - NONE of the 25 held a `metadata.json` or a `*.jsonl` (the ledger describes
//     the canonical twin, never this one);
//   - 1.93 GB between them, with two whole checkouts inside.
//
// THE DEFECT THIS FILE PINS. `ShapeOf` maps a bare 32-hex run onto `<id>` exactly
// as it maps a canonical uuid, so the shape test called each of them a session
// container — while the tree's ROOT-LEVEL DIRECTORY predicate
// (IsRootTaskContainerName) refused them, because it refuses anything the
// container test admitted. A directory that IS a container by one rule and
// belongs to NO family by the other is in the GAP between two predicates: no
// declared tree's namespace claimed it, so no tree could count it, and the
// registry had no family to report it under.
//
// THE CLAIM, and it is the whole of the fix: the dash-less stem spelling is
// REFUSED as a container name and CLAIMED by the family whose writers minted it
// (the task-container family, whose own doc already says "a 32-hex row id"), so
// the directory lands on a family — the legacy tree's namespace — instead of in
// the gap. The two halves are written in ONE place each
// (IsSessionContainerName / IsRootTaskContainerName), and the registry's Match
// functions and the declared tree's EntryMatch both reference those, so the
// classifier and the sweep cannot disagree.
package sessionlayout

import "testing"

// The measured dash-less names. They are the REAL directories the live sessions
// root carried (a sample of the 25), written once here so the red case and the
// green case cannot drift apart.
var dashlessContainerClaimMeasured = []string{
	"01a12161de4075a487774f0042b63d6a",
	"01a12dc6f44d73479b952382a4a7d352",
	"01a1361ba9ee7f198271a702059e958b",
	"01a145494fff700c9f2b3381e8242f0e",
}

// dashlessCanonicalTwin is the canonical spelling of one of them: the directory
// the ledger really describes, and the reason the dash-less one is a SECOND
// container rather than a differently-spelled first.
const dashlessCanonicalTwin = "01a12161-de40-75a4-8777-4f0042b63d6a"

// TestDashlessContainerIsRefusedAndClaimed is the claim itself, and it is the
// pair that matters: every measured name must be REJECTED by the container test
// and ACCEPTED by a family — not merely refused, because a refusal nobody follows
// is how the gap was opened in the first place.
func TestDashlessContainerIsRefusedAndClaimed(t *testing.T) {
	for _, name := range dashlessContainerClaimMeasured {
		if IsSessionContainerName(name) {
			t.Errorf("IsSessionContainerName(%q) = true — a uuid's 32 hex characters with NO separator is the branch/worktree stem spelling, not a session container name; ShapeOf erases exactly this difference, so the container test must read the name itself", name)
		}
		// THE OTHER HALF, and the one this fix adds: refused is not enough, it
		// must land on a family. IsRootTaskContainerName is that family's
		// membership test, and it is the SAME function the session-legacy tree's
		// root-level directory predicate references — one predicate, two readers
		// (Rules 13/14).
		if !IsRootTaskContainerName(name) {
			t.Errorf("IsRootTaskContainerName(%q) = false — the dash-less spelling is refused as a container AND claimed by no other family: a root-level directory in the gap between two predicates, counted by no declared tree", name)
		}
	}
}

// TestDashlessClaimDoesNotMoveTheCanonicalContainer is the REVERSE CONTROL, and
// it is the half that keeps the claim from becoming over-blocking: the row's OWN
// container, spelled canonically, keeps every verdict it had.
//
// A claim that swept up the canonical twin as well would be worse than the gap:
// it would move a container the ledger still resolves through into a legacy
// namespace, which is the namespace a tree REMOVES in.
func TestDashlessClaimDoesNotMoveTheCanonicalContainer(t *testing.T) {
	if !IsSessionContainerName(dashlessCanonicalTwin) {
		t.Errorf("IsSessionContainerName(%q) = false — the canonical spelling is a session container name, and this fix must not touch it", dashlessCanonicalTwin)
	}
	if IsRootTaskContainerName(dashlessCanonicalTwin) {
		t.Errorf("IsRootTaskContainerName(%q) = true — the canonical container moved into the legacy family; a container the ledger still resolves through must stay in the session-container family", dashlessCanonicalTwin)
	}
	// And the canonical name is not the same string as the dash-less one: the
	// two really are two directories, which is why the fix is a claim and not a
	// de-duplication.
	if dashlessCanonicalTwin == dashlessContainerClaimMeasured[0] {
		t.Fatalf("the fixture's canonical twin equals its dash-less name — this test's premise (two directories) is vacuous, re-derive it")
	}
}

// TestDashlessClaimLeavesEveryOtherNameAlone pins the third direction: the names
// this tree really carries that are NEITHER a canonical container NOR a dash-less
// stem keep the verdicts they had.
//
// The corpus is deliberate: it holds the aliases a length-or-hex test could
// accidentally swallow (an 8-hex prefix, a dashed non-uuid, a name that is not an
// id at all), the two legacy container prefixes, and the measured stray shapes.
// Over-blocking any one of them would move a directory into a removing tree's
// namespace on the strength of a length test.
func TestDashlessClaimLeavesEveryOtherNameAlone(t *testing.T) {
	cases := []struct {
		name          string
		wantContainer bool
		wantTask      bool
	}{
		// A canonical uuid: the container family, not this one.
		{dashlessCanonicalTwin, true, false},
		// The legacy container prefixes: this family, never the container's.
		{"t-1788897004", false, true},
		{"t-acc-live", false, true},
		{"process-12345", false, true},
		// Names that are NOT a uuid's 32 hex characters: a dashed id that is not
		// a uuid grouping, an 8-hex run, a zero-padded name, a plain name.
		{"01a11548-tracked", false, false},
		{"0000-emptydir", false, false},
		{"01a0a392", false, false},
		{"artifacts", false, false},
		// The measured stray shapes: neither family, and the stray predicate's
		// own business.
		{"zz-live-probe", false, false},
		{"evidence", false, false},
		// A name that CONTAINS a long hex run but does not START with one, and a
		// 12-hex run (below the 16-character id threshold ShapeOf needs): the
		// length test must not swallow either. (A name that merely CONTAINS 32
		// hex characters — `…<run>-extra`, `…<run>.raw.log` — shapes to `<id>…`
		// and was a container name BEFORE this change too; that verdict is the
		// pre-existing shape rule, not this test's subject.)
		{"x01a12161de4075a487774f0042b63d6a", false, false},
		{"01a0a3926b1c", false, false},
	}
	for _, tc := range cases {
		if got := IsSessionContainerName(tc.name); got != tc.wantContainer {
			t.Errorf("IsSessionContainerName(%q) = %v, want %v — the claim must narrow EXACTLY the two id spellings this tree does not use", tc.name, got, tc.wantContainer)
		}
		if got := IsRootTaskContainerName(tc.name); got != tc.wantTask {
			t.Errorf("IsRootTaskContainerName(%q) = %v, want %v", tc.name, got, tc.wantTask)
		}
	}
}

// TestEveryRefusedRootContainerLandsOnAWorkLegacyPredicate is the structural
// invariant behind the whole change, stated once over the registry's OWN
// predicates: a root-level DIRECTORY that the container test refuses must be
// claimed by the predicate the legacy tree actually uses.
//
// The tree's root-level directory row is
// `IsRootTaskContainerName(name) || IsStrayRootDirName(name)`. So the property
// is: for every measured dash-less name, that disjunction is TRUE. It is the
// sentence the epic's acceptance criterion is about — "these directories change
// from claimed by no declared tree to claimed by one" — asked in the vocabulary
// the tree itself asks it in, which is why this test cannot pass while the tree
// says otherwise.
func TestEveryRefusedRootContainerLandsOnAWorkLegacyPredicate(t *testing.T) {
	for _, name := range dashlessContainerClaimMeasured {
		if IsSessionContainerName(name) {
			continue // the container family claimed it; not this test's case
		}
		if !IsRootTaskContainerName(name) && !IsStrayRootDirName(name) {
			t.Errorf("root-level directory %q: refused by IsSessionContainerName and claimed by NEITHER half of the session-legacy tree's predicate (IsRootTaskContainerName ∨ IsStrayRootDirName) — it is in the gap, and no declared tree's namespace counts it", name)
		}
	}
}
