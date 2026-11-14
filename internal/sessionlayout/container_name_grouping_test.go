// container_name_grouping_test.go — the regression guard for the ONE defect the
// container test could not see: a uuid's 32 hex characters in a grouping this
// tree does not use.
//
// THE MEASURED INCIDENT (host host-b, 2026-10-14, task
// 01a13645-66c3-79ec-962a-5f8776e1f216). A run transcribed its own row id with
// the tail group's leading dash moved one group left and ran
// `mkdir -p <that spelling>/artifacts`. The spelling is the branch/worktree form
// a stem that strips non-alphanumerics produces. `ShapeOf` maps BOTH spellings
// onto `<id>`, so `IsSessionContainerName` called the wrong one a container —
// and that predicate is the namespace rule of the rows that REMOVE root-level
// directories, so `gopi tool scratch-gc trees` reduced the empty, aged
// near-miss container (and the `artifacts/` inside it) while keeping the row's
// real container, which carries `metadata.json`.
//
// Every spelling in this test is written ONCE, as a constant, so the red case and
// the green case cannot drift apart.
package sessionlayout

import "testing"

const (
	// containerGroupingCaseRowID is the canonical spelling of the row the
	// incident's run was working on — a uuid's 16 bytes grouped 8-4-4-4-12.
	containerGroupingCaseRowID = "01a13645-66c3-79ec-962a-5f8776e1f216"
	// containerGroupingCaseMisTranscribed is the SAME 32 hex characters with the
	// tail group's leading dash moved one group left (5 groups → 4), which is
	// what that run typed into its own commands. It is the name that went to
	// disk as a second container.
	containerGroupingCaseMisTranscribed = "01a13645-66c3-79ec-962a5f8776e1f216"

	// containerGroupingCaseDashlessStem is the row id's 32 hex characters with
	// NO separators: the branch/worktree stem spelling `taskGroundIDStem`
	// produces, and the second shape the container test cannot see (measured
	// 2026-10-17, task 01a144001e4b7086b97d257497a832ed: 25 root-level
	// directories named this way, every one of them beside a canonical twin).
	containerGroupingCaseDashlessStem = "01a1364566c379ec962a5f8776e1f216"
)

// TestContainerNameRefusesANonCanonicalGrouping is the red case: the
// mis-transcribed spelling is NOT a container name, the row's real spelling IS.
//
// Before the fix both were `true` (both shape to `<id>`), which is why an empty
// near-miss container sat inside a removing row's namespace.
func TestContainerNameRefusesANonCanonicalGrouping(t *testing.T) {
	if IsSessionContainerName(containerGroupingCaseMisTranscribed) {
		t.Errorf("IsSessionContainerName(%q) = true — a uuid's 32 hex characters in a NON-canonical grouping is not a container name; ShapeOf erases exactly this difference, so the container test must read the name itself",
			containerGroupingCaseMisTranscribed)
	}
	if !IsSessionContainerName(containerGroupingCaseRowID) {
		t.Errorf("IsSessionContainerName(%q) = false — the row's own canonical spelling is a container name", containerGroupingCaseRowID)
	}
}

// TestContainerNameVerdictsMovedForExactlyTwoShapes pins the OTHER direction,
// and it is the half that keeps the refusal from becoming over-blocking. It
// compares the predicate against the PRE-FIX rule re-derived here
// (shape-based: `isSessionContainerShape(ShapeOf(name))`, which is what the
// function was before) over a corpus of names this tree really carries, and
// requires the verdicts to agree EVERYWHERE except the TWO id spellings this
// tree does not use — the non-canonical grouping and the dash-less stem.
//
// It is not a tautology in the direction that matters: if the new refusal ever
// widened past those two spellings, a name in this corpus would change its
// answer and the test would say which one.
func TestContainerNameVerdictsMovedForExactlyTwoShapes(t *testing.T) {
	corpus := []string{
		containerGroupingCaseRowID,
		containerGroupingCaseMisTranscribed,
		containerGroupingCaseDashlessStem,

		// Names this tree really carries that are NOT id-shaped containers.
		"t-1791802609",
		"01a11548-tracked",
		"0000-emptydir",
		"01a0a392",
		"artifacts",
		"01a13645-66c3-79ec-962a-5f8776e1f216.raw",
	}
	moved := 0
	for _, name := range corpus {
		legacy := isSessionContainerShape(ShapeOf(name)) // the PRE-fix rule
		got := IsSessionContainerName(name)
		if name == containerGroupingCaseMisTranscribed {
			if !legacy {
				t.Fatalf("%q was NOT admitted by the pre-fix rule — this test's red case is vacuous, re-derive it", name)
			}
			if got {
				t.Errorf("%q is STILL a container name — the refusal it must hit is not reached", name)
			}
			moved++
			continue
		}
		if name == containerGroupingCaseDashlessStem {
			// THE SECOND MOVED VERDICT, and the half that keeps the refusal from
			// opening a gap: the dash-less stem spelling was admitted by the
			// pre-fix rule and is refused now — AND it is claimed, by the ONE
			// family that owns a run's id-named directory. A refusal that no
			// family followed would leave a root-level directory that belongs to
			// nothing, which is the state this change exists to end.
			if !legacy {
				t.Fatalf("%q was NOT admitted by the pre-fix rule — this test's red case is vacuous, re-derive it", name)
			}
			if got {
				t.Errorf("%q is STILL a container name — the dash-less stem spelling is the second shape the container test cannot see", name)
			}
			if !IsRootTaskContainerName(name) {
				t.Errorf("%q is refused as a container AND claimed by no other family — a root-level directory in the gap between two predicates", name)
			}
			moved++
			continue
		}
		if got != legacy {
			t.Errorf("IsSessionContainerName(%q) = %v, pre-fix rule says %v — the refusal must narrow EXACTLY the two id spellings this tree does not use, never anything else", name, got, legacy)
		}
	}
	if moved != 2 {
		t.Fatalf("exactly two corpus names must change their verdict (the non-canonical grouping and the dash-less stem spelling); %d did", moved)
	}
}
