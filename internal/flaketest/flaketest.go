// Package flaketest owns the repository's ORDER/DETERMINISM flake class: the
// checked-in registry of known non-deterministic tests, the ONE name of the
// repetition opt-in that re-runs the suspect scope, and the boundary that keeps
// that opt-in off the merge/verify path.
//
// WHY THIS IS NOT PART OF internal/heavytest
// ------------------------------------------
// A flake is a DEFECT, not a heaviness class. A heavy test's verdict is the
// host's (ambient load, wall clock, sudo availability) — skipping it on the
// smoke path is CORRECT, because there is nothing to fix in the code. A flaky
// test FAILS AND PASSES on the SAME host with the SAME code: the randomizer is
// map-iteration order, a shared fixture root, a pid slot, a wall-clock seeding
// race. Its verdict is wrong under every host state, so it cannot be routed
// through heavytest.Require / CriterionHostCondition: a skip reads as a passing
// green, which would HIDE the defect instead of fixing it — the exact
// `t.Skip`-without-a-reason failure this repo counts.
//
// The instrument for such a test is REPETITION — `go test -count=N` on the test
// itself (PROJECT.md Rule 27: measure the test, never the host; no device load, no
// pressure generator, EVER). The class therefore has: a registry
// (testdata/flakes.tsv, gated against the code in both directions by
// internal/standards), a runner (scripts/flake-catch.sh, a named opt-in that is
// NOT on the smoke/merge path), and this leaf package, which is the ONE
// definition of the names those two share.
//
// WHY A LEAF PACKAGE: the in-package tests of internal/scratch, internal/tools
// and cmd/agent must be able to import it, so it may import nothing that could
// close a cycle (the same reason internal/heavytest exists — see its header).
// Values are strings only.
package flaketest

import (
	"os"
	"strconv"
	"strings"
)

const (
	// DeclareShape is the CALL SHAPE of the in-test declaration: the
	// package-qualified callee the gate's AST scan matches. It has one definition
	// here (Rule 1b/13) so the gate cannot assert a spelling this package does not
	// export, and so renaming the function is a one-line change that the gate
	// follows rather than a silent hole.
	DeclareShape = "flaketest.Declare"

	// DeclareMinMechanismWords is the fewest words a Declare mechanism may carry
	// after CausePrefix. It is a CRUDE guard, and deliberately so: "it is flaky"
	// and "flaky sometimes" are three and two words and name nothing, while every
	// real mechanism in the registry runs ten words or more. The SUBSTANTIVE floor
	// is the registry row's own (the gate's flakeMinCauseWords = 12, applied to the
	// cause column): this one only keeps the degenerate declaration — a test
	// joining the class without saying anything — out of the declared set, so the
	// gate's second direction can never be satisfied by declaring a test flaky in
	// order to have something to register.
	DeclareMinMechanismWords = 6

	// DeclareSelfName — the function name inside DeclareShape, used by the scanner
	// that resolves a call site to the test function that wraps it.
	DeclareSelfName = "Declare"

	// RepsEnv is the ONE name of the repetition opt-in: it says HOW MANY times
	// the catch mechanism may re-run a suspect scope. Read by Reps only
	// (PROJECT.md Rule 2/21: one reader per knob); `make flake-catch` sets it.
	//
	// It is deliberately a COUNT, not an enable/disable switch, because
	// repetition IS the instrument: a mechanism that ran the scope once would
	// measure nothing, and a mechanism that ran it "some" number of times would
	// hide its own sensitivity behind an unstated default.
	RepsEnv = "GOPI_FLAKE_REPS"

	// RegistryRel is the checked-in registry (repo-relative): one row per known
	// order/determinism flake. Its single definition is here, and the shell side
	// (scripts/lib/flake-threshold.sh) is asserted to spell the same path — a
	// renamed registry would otherwise leave the gate reading an empty file and
	// reporting a clean green.
	RegistryRel = "testdata/flakes.tsv"

	// RegistryHeader heads the registry file. The columns are load-bearing:
	// test / package / deterministic root cause / owning task row / date. A row
	// whose cause column is prose ("flaky sometimes", "timing") is a SUPPRESSION
	// entry wearing a registry row's clothes, and the gate rejects it — the whole
	// point of a root-cause registry is that the next reader inherits the
	// DETERMINISM, not the observation.
	RegistryHeader = "# test\tpackage\troot_cause\towner\tdate"

	// CausePrefix marks the root-cause column's machine-checkable shape
	// (`deterministic: <mechanism>`). The prefix is required because a free-form
	// sentence cannot be told from an excuse by any predicate; a colon-delimited
	// mechanism can at least be required to NAME one.
	CausePrefix = "deterministic: "

	// DateLayout is the registry's date column format (sortable, unambiguous).
	DateLayout = "2006-01-02"

	// OwnerShape — kept in the shell/Go agreement test's vocabulary below; a
	// task row id is the FULL ledger form
	// (`01a0aa22-fa30-718e-9422-c0eef51f0510`) or a legacy `t-<epoch>` id. It
	// used to accept an 8-hex-digit short id (`01a0aa22`) as well; that is
	// retired because the chip is ambiguous (`01a0aba1` names 2 ledger rows),
	// and the gate additionally checks the id EXISTS as a row id in this
	// repo's history (see the gate).
	OwnerShape = `^([0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}|t-[0-9]{10})$`

	// CatchScriptRel is the ONE catch mechanism: a named opt-in that re-runs a
	// scope with go test -count=N and NAMES a test whose verdict moves across
	// repetitions.
	CatchScriptRel = "scripts/flake-catch.sh"

	// CatchMakeTarget is the Makefile target that runs it. Like `test-heavy` it
	// is deliberately NOT the default: `make test` stays smoke-only.
	CatchMakeTarget = "flake-catch:"

	// GateProbeScriptRel is the red-proof DRIVER: it builds a throwaway module
	// whose only test is a PLANTED map-iteration-order flake and points the real
	// catch mechanism at it. The standards gate's behavioural half runs this, so
	// the gate's claim ("the mechanism NAMES a new order-dependent flake") is
	// checked by running the mechanism rather than by grepping it.
	GateProbeScriptRel = "scripts/flake-gate-probe.sh"

	// ProbePlantMode / ProbeCleanMode are the driver's two modes: the planted
	// order-dependent fixture (must be NAMED) and the same fixture with the order
	// dependence removed (the negative control — must name nobody).
	ProbePlantMode = "plant"
	ProbeCleanMode = "clean"

	// DefaultReps is how many repetitions the catch mechanism uses when the
	// operator does not say. It is a starting point, not a rate estimate: a
	// 1-in-20 flake needs `-reps 20` and the script says so in its output.
	DefaultReps = 10

	// MinReps / MaxReps bound the count. Below the floor a run cannot tell a
	// flake from a pass (a single run proves nothing — the whole class's
	// signature), and above the ceiling the mechanism stops being a mechanism
	// and becomes a load generator, which PROJECT.md Rule 27 forbids absolutely.
	MinReps = 2
	MaxReps = 100
)

// Reps reports how many repetitions the catch mechanism may run: RepsEnv when it
// holds an integer inside [MinReps, MaxReps], DefaultReps otherwise. It is the
// ONE reader of RepsEnv (Rule 2/21).
//
// An out-of-band value is NEVER silently re-interpreted as a different count:
// the script refuses it loudly (Rule 15 — a knob that quietly does something
// else is worse than a knob that says no) and this accessor falls back to the
// default, so a Go consumer can be told the same thing the script does and
// never believes it got a count the band forbids.
func Reps() int {
	raw := strings.TrimSpace(os.Getenv(RepsEnv))
	if !RepsInBand(raw) {
		return DefaultReps
	}
	n, err := strconv.Atoi(raw)
	if err != nil { // unreachable: RepsInBand already parsed it, kept as a guard
		return DefaultReps
	}
	return n
}

// RepsInBand reports whether raw is a usable repetition count: an integer (no
// spaces, no sign noise) at or above MinReps and at or below MaxReps. The
// script refuses anything else instead of guessing — see Reps.
func RepsInBand(raw string) bool {
	s := strings.TrimSpace(raw)
	if s == "" || strings.ContainsAny(s, " \t") {
		return false
	}
	n, err := strconv.Atoi(s)
	if err != nil {
		return false
	}
	return n >= MinReps && n <= MaxReps
}

// Enabled reports whether THIS process is a catch run: RepsEnv is set to a value
// in band. A test that wants to be quiet during a repetition run asks this
// instead of re-spelling the variable (Rule 13), and a fixture that would
// otherwise spend a wall-clock second per repetition uses it to bound its
// startup cost — the mechanism's cost must scale with the verdict, not with
// incidental sleeping.
//
// It is deliberately NOT an on/off switch for the TEST itself: unlike
// heavytest.Require, a catch run never skips a test. A flake that skipped itself
// when someone looked for it would be invisible exactly when it matters.
func Enabled() bool {
	return RepsInBand(os.Getenv(RepsEnv))
}

// Declare marks the CALLING TEST as a member of the order/determinism flake
// class, from INSIDE the test, and is the second half of the registry's
// bidirectional gate.
//
// WHY A DECLARATION IN THE TEST, AND NOT THE TABLE ALONE
// -----------------------------------------------------
// testdata/flakes.tsv is a hand-maintained table. A gate that reads only the
// table can check "every row names a test that exists" and nothing else — so a
// test that IS flaky and was never registered reads green, and the registry
// quietly under-reports the class it exists to document (the false green
// measured on task 01a10a2d). The missing direction ("every declared flake is
// registered") needs a SECOND, INDEPENDENT source of the flaky set that the
// table cannot fake. This call is that source: the declaration travels WITH the
// test, so the gate can enumerate the declared set by an AST scan and assert
// registry ⊇ declared.
//
// The two sources fail in different ways on purpose. The table is the human
// artifact (prose mechanism, owner, date — none of which a machine can derive
// from a call site); this call is the machine-readable membership bit, written
// by the person fixing or documenting the flake at the moment they are looking
// at the code. A test whose flakiness was diagnosed but whose row was never
// appended — the measured failure — is exactly the case the two disagree on.
//
// WHAT IT DOES AT RUNTIME: NOTHING — deliberately, and the reason is that the
// alternative is worse. It does not skip, retry, suppress, reorder or read the
// environment: a flake is a DEFECT, and a call that changed a verdict would be
// exactly the hiding this class refuses (compare heavytest.Require, which may
// skip, and which the registry gate refuses on a registered test). It does not
// panic on a mechanism that names nothing either: panicking here would remove
// the caller's choice (PROJECT.md Rule 19) and would only fire where a test
// happens to run, while the ENFORCEMENT belongs where the evidence is — the
// gate reads the ARGUMENT LITERAL at the call site (an AST check on the source)
// and reports a declaration that names no mechanism by file:line, whether or
// not the test ever runs. MechanismOK is that check's one definition.
func Declare(mechanism string) {
	_ = mechanism // the argument is EVIDENCE, read from the AST by the registry gate
}

// MechanismOK reports whether mechanism names a mechanism: CausePrefix optional,
// then at least DeclareMinMechanismWords words. The gate applies it to the
// literal at each call site, and its own red proof drives it directly.
func MechanismOK(mechanism string) bool {
	trimmed := strings.TrimPrefix(strings.TrimSpace(mechanism), CausePrefix)
	return len(strings.Fields(trimmed)) >= DeclareMinMechanismWords
}
