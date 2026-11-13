package flaketest

// The class's knob and registry names are tiny, and that is exactly why they get
// a test: the mechanism's whole claim rests on the repetition count meaning what
// the script thinks it means. Two halves:
//
//   - the band (ClampReps / RepsInBand) is the boundary that keeps the mechanism
//     from degenerating into a device-load generator (PROJECT.md Rule 27) on the
//     low side and into a proof of nothing on the high side;
//   - Enabled() is the ONE reader of the knob, so a fixture can ask "am I inside a
//     catch run?" without re-spelling the variable (Rule 13).
//
// t.Setenv is the instrument (PROJECT.md Rule 17): the knob is process state, and a
// leak here would change a sibling test's behaviour by test ORDER — the very
// class this package documents.

import (
	"strconv"
	"testing"
)

// TestRepsInBandPinsTheRule27Boundary: below the floor a single pass proves
// nothing; above the ceiling the mechanism is load generation, which Rule 27
// forbids absolutely. Both edges are asserted, and so is the interior.
func TestRepsInBandPinsTheRule27Boundary(t *testing.T) {
	cases := []struct {
		raw  string
		want bool
		why  string
	}{
		{"", false, "unset is not a count"},
		{"  ", false, "blank is not a count"},
		{"1", false, "below the floor: one pass cannot tell a flake from a pass"},
		{strconv.Itoa(MinReps - 1), false, "below the floor"},
		{strconv.Itoa(MinReps), true, "the floor itself is usable"},
		{"10", true, "the default"},
		{strconv.Itoa(MaxReps), true, "the ceiling itself is usable"},
		{strconv.Itoa(MaxReps + 1), false, "above the ceiling: repetition becomes load"},
		{"abc", false, "not a number"},
		{"3.5", false, "not an integer"},
		{"-5", false, "a negative count"},
		{"1 0", false, "not a single integer token"},
	}
	for _, c := range cases {
		if got := RepsInBand(c.raw); got != c.want {
			t.Errorf("RepsInBand(%q) = %v, want %v (%s)", c.raw, got, c.want, c.why)
		}
	}
}

// TestTheBandEdgesAreUsable: both edges of [MinReps, MaxReps] must themselves be
// runnable counts — an off-by-one in the band would make the floor (the smallest
// run that can prove anything) unusable.
func TestTheBandEdgesAreUsable(t *testing.T) {
	if MinReps >= MaxReps {
		t.Fatalf("the repetition band is empty: MinReps=%d MaxReps=%d", MinReps, MaxReps)
	}
	t.Setenv(RepsEnv, strconv.Itoa(MinReps))
	if got := Reps(); got != MinReps {
		t.Errorf("Reps() at the floor = %d, want %d (the floor must be a usable count)", got, MinReps)
	}
	t.Setenv(RepsEnv, strconv.Itoa(MaxReps))
	if got := Reps(); got != MaxReps {
		t.Errorf("Reps() at the ceiling = %d, want %d", got, MaxReps)
	}
}

// TestRepsReadsTheKnobAndFallsBack: Reps() is the ONE reader of RepsEnv. An
// unusable value falls back to the default rather than being clamped silently —
// a caller that asked for 1 repetition must not be told it got the floor.
func TestRepsReadsTheKnobAndFallsBack(t *testing.T) {
	t.Setenv(RepsEnv, "")
	if got := Reps(); got != DefaultReps {
		t.Errorf("Reps() with the knob unset = %d, want the default %d", got, DefaultReps)
	}
	t.Setenv(RepsEnv, "20")
	if got := Reps(); got != 20 {
		t.Errorf("Reps() with %s=20 = %d, want 20", RepsEnv, got)
	}
	for _, raw := range []string{"1", "abc", strconv.Itoa(MaxReps + 1)} {
		t.Setenv(RepsEnv, raw)
		if got := Reps(); got != DefaultReps {
			t.Errorf("Reps() with %s=%q = %d, want the default %d (an out-of-band value is refused "+
				"by the script, never silently re-interpreted here)", RepsEnv, raw, got, DefaultReps)
		}
	}
}

// TestEnabledIsExactlyTheInBandKnob: the fixture-facing predicate must not drift
// from the count the script will run. Enabled() false also proves nothing skips —
// unlike heavytest.Require, this is a "am I in a catch run" question, never a
// reason to skip a test.
func TestEnabledIsExactlyTheInBandKnob(t *testing.T) {
	// t.Setenv (not os.Unsetenv) is the instrument PROJECT.md Rule 17 wants: the knob
	// is process state, and a leak here would decide a SIBLING test's behaviour by
	// test ORDER — this package's own subject. An EMPTY value and an ABSENT one are
	// the same code path (TrimSpace then the band check), so this pins the case that
	// matters: no repetitions requested means no catch run.
	t.Setenv(RepsEnv, "")
	if Enabled() {
		t.Errorf("Enabled() is true with %s empty — a catch run is a run WITH repetitions requested",
			RepsEnv)
	}
	t.Setenv(RepsEnv, "4")
	if !Enabled() {
		t.Errorf("Enabled() is false with %s=4 in band", RepsEnv)
	}
	t.Setenv(RepsEnv, "0")
	if Enabled() {
		t.Errorf("Enabled() is true with %s=0 (out of band)", RepsEnv)
	}
}

// TestMechanismOKIsTheDeclarationsShape pins the ONE predicate the registry gate
// applies to a declaration's ARGUMENT. It is a runtime-free check by design: the
// gate reads the literal at the call site, so the refusal cannot depend on the
// test being run, and no panic is needed here (PROJECT.md Rule 19).
//
// Both directions are asserted, so the predicate cannot be satisfied by refusing
// everything (a check nobody can pass is not a check) or by accepting everything
// (which would let a test join the flake class by asserting it is flaky).
func TestMechanismOKIsTheDeclarationsShape(t *testing.T) {
	cases := []struct {
		raw  string
		want bool
		why  string
	}{
		{"", false, "nothing at all"},
		{"   ", false, "blank"},
		{"flaky", false, "one word names no mechanism"},
		{CausePrefix + "flaky", false, "the prefix does not supply a mechanism"},
		{CausePrefix + "it is flaky", false, "three words that name nothing"},
		{"timing", false, "an excuse, not a mechanism"},
		{CausePrefix + "the fixture root is shared and walked from a Go map", true, "a real mechanism, prefixed"},
		{CausePrefix + "a wall-clock sleep stands in for the claim own signal", true, "a real mechanism"},
		{"the fixture root is shared and walked from a Go map", true, "the prefix is optional at the call site"},
	}
	for _, c := range cases {
		if got := MechanismOK(c.raw); got != c.want {
			t.Errorf("MechanismOK(%q) = %v, want %v (%s)", c.raw, got, c.want, c.why)
		}
	}
}

// TestDeclareIsAVerdictNeutralMarker — Declare must not change any verdict. The
// class's whole premise is that a flake is a DEFECT: a call that skipped, retried
// or suppressed would HIDE exactly what the registry documents. The observable
// form of that promise is that the call returns and does nothing, which is what
// this asserts — the absence is the contract.
func TestDeclareIsAVerdictNeutralMarker(t *testing.T) {
	Declare(CausePrefix + "a shared fixture root walked from a Go map decides the address")
	t.Setenv(RepsEnv, "")
	if Enabled() {
		t.Fatalf("Declare turned a catch run on — it must read no environment and change no state")
	}
}

// TestDeclareShapeNamesTheRealFunction — the gate matches call sites through
// DeclareShape, so a rename that did not move it would make the gate scan for a
// function nobody calls and report an empty (vacuously green) declared set.
func TestDeclareShapeNamesTheRealFunction(t *testing.T) {
	if want := "flaketest." + DeclareSelfName; DeclareShape != want {
		t.Fatalf("DeclareShape = %q, want %q — the gate matches this spelling at the call site",
			DeclareShape, want)
	}
	if DeclareSelfName != "Declare" {
		t.Fatalf("DeclareSelfName = %q, want %q — it is the pure-Go half of the shape the gate "+
			"and the AST scanner read", DeclareSelfName, "Declare")
	}
}
