// cwd_release_gate_leg2_test.go — the positive control for the cwd-release
// gate's leg ② (2026-11-06).
//
// THE DEFECT THIS FREEZES: a gate was changed to ACCEPT a call. Commit
// 0ee5745c had a real production caller (cmd/agent, the `task sync --pass`
// re-driver) turn leg ② red, and instead of moving the caller it redefined the
// obligation as "…and the calling file ALSO removes a directory". Leg ① of the
// same test asserts the arms' library file reaches no removal verb, and the
// caller removed nothing either, so the conjunction was false by construction:
// leg ② could never fire again, for any caller, ever. The leftover t.Logf made
// the pass read as "re-measured, no obligation" — a gate whose red had been
// quietly revoked, indistinguishable in CI from a gate that passed.
//
// Row 01a1ab49 repaired the CONSEQUENCE (1c85d569: move the call into
// internal/tools, which this gate does not scan, so the unconditional absence
// became true again). That repair would not have been needed if the narrowing
// had been caught when it was written, and nothing catches it today: the next
// landing can write the same conjunction back and the gate silently loses its
// ability to be red.
//
// SO THIS TEST IS THE CATCHER. It drives the one predicate leg ② consumes with
// a fixture shaped exactly like the caller 0ee5745c was written for — calls an
// arm, removes no directory — and REQUIRES a red. Two things therefore cannot
// happen quietly any more:
//
//	· narrowing the predicate into a conjunction (the fixture stops being
//	  flagged, and the failure names the fixture and the arm);
//	· narrowing the LEG inline while leaving the predicate honest (the
//	  structural arm below reads the leg's own source and refuses the removal
//	  verb, which is the conjunct that made the conjunction false).
//
// Rule 35: no hook, no scheduled job, no external checker — the guard is a Go
// test in the package that owns the gate, and it runs whenever the gate runs.
package standardgate

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const (
	// cwdReleaseLeg2FixtureCalling removes NOTHING and calls an arm: the exact
	// shape of the caller 0ee5745c narrowed the gate for. It must be a red.
	cwdReleaseLeg2FixtureCalling = `package agent

// deliveryOwed is the shape of the caller this gate must reject: it references
// a re-delivery arm and removes no directory at all.
func deliveryOwed(s *passState) {
	owed := tools.DeliveryRetryTaskID
	if lines := tools.DeliveryRetryLines(owed); lines == 0 {
		return
	}
	s.redrive(owed)
}
`

	// cwdReleaseLeg2FixtureRemoving is the NEGATIVE half of the control: it
	// removes a directory and calls NO arm, so it must be clean. Its job is to
	// prove the positive assertion above is not vacuous — a predicate that
	// flagged everything (or that keyed on the removal verb rather than on the
	// arm) would flag this too, and this assertion is what notices.
	cwdReleaseLeg2FixtureRemoving = `package agent

// teardown removes a worktree and references no re-delivery arm.
func teardown(path string) {
	if err := reclaimGitRemove(path); err != nil {
		return
	}
}
`

	// cwdReleaseLeg2GateFile is the gate whose leg ② the structural arm reads.
	cwdReleaseLeg2GateFile = "internal/standards/cwd_release_gate_test.go"

	// cwdReleaseLeg2RegionOpen / Close bracket leg ② inside that test's body.
	cwdReleaseLeg2RegionOpen  = "// ② no scanned-tree production caller."
	cwdReleaseLeg2RegionClose = "// ③ in neither list."

	// cwdReleaseLeg2Predicate is the ONE verdict leg ② must consume; and
	// cwdReleaseLeg2Conjunct is the piece that made the conjunction false.
	cwdReleaseLeg2Predicate = "cwdReleaseDeliveryRetryCallerViolations"
	cwdReleaseLeg2Conjunct  = "cwdReleaseRemovalVerb"
)

// TestGateCwdReleaseDeliveryRetryLeg2StillRejectsACaller is the positive
// control: the gate's leg ② must reject a caller that removes nothing.
func TestGateCwdReleaseDeliveryRetryLeg2StillRejectsACaller(t *testing.T) {
	arms := strings.Split(cwdReleaseDeliveryRetryArms, cwdReleaseTriggerSep)
	if len(arms) == 0 || arms[0] == "" {
		t.Fatalf("cwdReleaseDeliveryRetryArms is empty — this control would assert over no arm at all")
	}

	// The fixture must have the property the narrowing keyed on: it removes
	// NOTHING. If this ever stops holding, the fixture stopped being the
	// 0ee5745c shape and the positive control below would be testing a caller
	// the conjunction would have rejected anyway (a vacuous green).
	if cwdReleaseRemovalVerb.MatchString(cwdReleaseCode(cwdReleaseLeg2FixtureCalling)) {
		t.Fatalf("the positive-control fixture MATCHES %s — it must remove no directory, or it stops being the 0ee5745c-shaped caller this control exists for; fix the fixture, do not weaken the assertion", cwdReleaseRemovalVerb)
	}

	// THE POSITIVE ASSERTION. A caller that removes nothing is still a red,
	// because what it CALLS is the obligation.
	offending := cwdReleaseDeliveryRetryCallerViolations(cwdReleaseLeg2FixtureCalling, arms)
	if len(offending) == 0 {
		t.Errorf("fixture %q calls %v and removes NO directory, and leg ② accepted it — the verdict has been narrowed back into the 0ee5745c conjunction (\"the caller must ALSO remove a directory\"). That shape is false by construction: leg ① asserts %s reaches no removal verb, so no caller can ever satisfy it and this gate can never be red again. Restore the unconditional verdict: a production caller of these arms IS the obligation, and the repair is to move the CALL out of a scanned tree (into internal/tools), never to weaken this leg",
			"cwdReleaseLeg2FixtureCalling", arms, cwdReleaseDeliveryRetryLib)
		return
	}
	for _, arm := range arms {
		if strings.Contains(cwdReleaseCode(cwdReleaseLeg2FixtureCalling), arm) && !containsString(offending, arm) {
			t.Errorf("the fixture calls %s but the verdict did not name it (got %v) — a verdict that returns a red without naming the arm cannot tell a reader what to repair", arm, offending)
		}
	}

	// THE NEGATIVE HALF — the assertion above is only meaningful if the
	// verdict can also say "clean". A predicate that flagged every file would
	// pass the positive half and fail here.
	//
	// why: correct — the fixture at this site is cwdReleaseLeg2FixtureRemoving, and the set the claim quantifies over is "files the arm-calling verdict flags"; the assertion fires only when that set is NON-EMPTY for a fixture that calls no arm, i.e. it is the redirection check that keeps the predicate keyed on the ARM. The positive half immediately above pins the mirror case (an arm-calling fixture MUST be flagged and MUST be named), so a predicate that returned nothing would fail there instead — neither arm can pass by widening. No idempotent step is involved: this reads a pure function of two string fixtures and mutates nothing.
	if got := cwdReleaseDeliveryRetryCallerViolations(cwdReleaseLeg2FixtureRemoving, arms); len(got) != 0 {
		t.Errorf("fixture %q calls NO arm and removes a directory, yet the verdict flagged it (%v) — the verdict must key on the ARM, not on the removal verb; as written it is a different predicate than leg ② claims to measure", "cwdReleaseLeg2FixtureRemoving", got)
	}

	// ③ STRUCTURAL ARM — the leg itself must consume the predicate and must
	// not re-introduce the conjunct. The behavioural arms above test the
	// FUNCTION; this one tests that the FUNCTION is what the gate uses, which
	// is the half that 0ee5745c actually broke (it edited the loop, not a
	// helper).
	leg2 := cwdReleaseLeg2Body(t)
	if !strings.Contains(leg2, cwdReleaseLeg2Predicate) {
		t.Errorf("leg ② of TestGateCwdReleaseDeliveryRetryRetirement does not call %s — the gate has stopped consuming the verdict this control measures, so both of its probes above are measuring a function nothing runs", cwdReleaseLeg2Predicate)
	}
	if strings.Contains(leg2, cwdReleaseLeg2Conjunct) {
		t.Errorf("leg ② of TestGateCwdReleaseDeliveryRetryRetirement mentions %s — that is the conjunct 0ee5745c added (\"the caller must ALSO remove a directory\"), and leg ① guarantees it is false for every caller, so a leg that reads it cannot be red. The call ALONE is the obligation: delete the conjunct and report every caller unconditionally", cwdReleaseLeg2Conjunct)
	}
}

// cwdReleaseLeg2Body returns the source region of leg ② inside
// TestGateCwdReleaseDeliveryRetryRetirement, bounded by the ②/③ markers the
// test body already carries. It fails the test rather than returning "" when a
// boundary is missing, because a structural proof over an empty string is a
// green that measured nothing (Rule 15).
func cwdReleaseLeg2Body(t *testing.T) string {
	t.Helper()
	root := repoRoot(t)
	src, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(cwdReleaseLeg2GateFile)))
	if err != nil {
		t.Fatalf("read %s: %v — the structural arm needs the gate's own source", cwdReleaseLeg2GateFile, err)
	}
	text := string(src)
	start := strings.Index(text, cwdReleaseLeg2RegionOpen)
	if start < 0 {
		t.Fatalf("%s no longer carries the %q marker — leg ② was renamed or removed, so this structural arm cannot locate what it judges", cwdReleaseLeg2GateFile, cwdReleaseLeg2RegionOpen)
	}
	end := strings.Index(text[start:], cwdReleaseLeg2RegionClose)
	if end < 0 {
		t.Fatalf("%s no longer carries the %q marker after leg ② — the region cannot be bounded, and an unbounded read would judge the whole file", cwdReleaseLeg2GateFile, cwdReleaseLeg2RegionClose)
	}
	return text[start : start+end]
}

// containsString reports whether list holds want (small helper: the arm lists
// here are 3 long, so a map would be noise).
func containsString(list []string, want string) bool {
	for _, v := range list {
		if v == want {
			return true
		}
	}
	return false
}
