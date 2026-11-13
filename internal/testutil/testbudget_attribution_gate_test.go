package testutil

// THE ATTRIBUTION GATE: the run must say WHERE THE WALL-CLOCK WENT, and the
// budget constant must carry the derivation it was set from.
//
// WHY (measured 2026-10-15, bug row on `cmd/agent` and the 25m cap). A trip
// printed `exit=3 outcome=timeout` plus a `slowest 15 tests` list and NOTHING
// that said which package absorbed the cap or which arms account for it — the
// bug row's own honest gap: "the heartbeat's last-started test names do not
// prove attribution". The numbers were already in the stream (every finished
// test carries `Elapsed`), so the run can attribute itself.
//
// WHAT IS GUARDED, and each arm is a different claim:
//
//  1. the block EXISTS in the shipped script and is printed by EVERY run (not
//     only a trip: the reading is what makes the next trip interpretable);
//  2. its heading and the LOWER BOUND label are PINNED, so the block cannot be
//     reworded away (Rule 15: a partial sum must never lose the word);
//  3. the reader is DRIVEN on synthetic streams whose answers are known — a
//     stream whose package was cut by the cap must print the LOWER BOUND and
//     the count of arms that never reported; a stream whose package finished
//     must NOT (or the label would mean nothing);
//  4. the RED PROOF: the reader is driven with the PRE-FIX evidence shape
//     (prose, no structured events) and must produce NO attribution — a
//     summary rendered from text cannot attribute anything, which is exactly
//     why the pre-fix script could not.
//
// The constant's own derivation is pinned too (arm 5): `test_budget` must be
// the value the comment above it derives, so a future edit cannot silently
// change the number while leaving the arithmetic describing the old one.

import (
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

const (
	// budgetAttributionHeadingConst is the script's declaration line for the
	// block heading; the gate reads the VALUE out of it (Rule 13: one spelling).
	budgetAttributionHeadingConst = `attribution_heading="`
	// budgetAttributionQualifierConst is the declaration line for the block's
	// second line, which carries the two limitations (finished arms only; not a
	// wall clock). Both halves are pinned, because the heading alone would let a
	// future edit drop the qualifications into an unread tail.
	budgetAttributionQualifierConst = `attribution_qualifier="`
	// budgetAttributionLowerLabelConst is the declaration of the LOWER BOUND
	// label — the one word that turns a partial sum into an honest figure.
	budgetAttributionLowerLabelConst = `attribution_label_lower="`
	// budgetAttributionLibRel is the shared reader the script sources.
	budgetAttributionLibRel = "scripts/lib/stream-attribution.sh"
	// budgetAttributionCallSite is the call the summary makes, so the block
	// cannot exist as dead code.
	budgetAttributionCallSite = "report_attribution"
	// budgetAttributionLowerMarker is the distinguishing half of the lower
	// label; the gate asserts it is present on a cut package and absent on a
	// completed one, using the script's OWN constant for both sides.
	budgetAttributionLowerMarker = "LOWER BOUND"
	// budgetAttributionPanicMarker mirrors the script's own trip marker (go's
	// panic text), which is how the reader decides a package was CUT.
	budgetAttributionPanicMarker = "panic: test timed out after"
	// budgetAttributionBudgetConstRe reads the ONE budget constant.
	budgetAttributionBudgetConstRe = `(?m)^test_budget="([^"]+)"`
	// budgetAttributionDerivationBudget is the value the derivation comment
	// above the constant must be talking about. It is asserted against the
	// constant itself below, so the pair cannot drift apart silently.
	budgetAttributionDerivationBudget = "45m"
	// budgetAttributionDerivationToken is the phrase the derivation must carry:
	// the criterion the number was set from, quoted from the script's own usage
	// block. A derivation that does not name its criterion is a number with a
	// story attached.
	budgetAttributionDerivationToken = "a wall-clock distance a loaded 4-core host cannot reach"
	// budgetAttributionDerivationReading is the measured demand the derivation
	// states, taken from THIS host's own stream (the 993s full pass of
	// ./cmd/agent). It is pinned so the arithmetic has a reading in it.
	budgetAttributionDerivationReading = "993s"
	// budgetAttributionDerivationStream is the stream the reading came from —
	// Rule 38: a figure names the run that produced it.
	budgetAttributionDerivationStream = "run-1792026228-2214329.json"
	// budgetAttributionRedProofProse is the PRE-FIX evidence shape: the prose a
	// summary used to be rendered from, with no structured event in it at all.
	// Driving the reader with this is the red proof (arm 4).
	budgetAttributionRedProofProse = "this suite takes ~189-291s quiet and >1123s at load 42-63"
	// budgetAttributionPureFormat is the reader's argument tail, supplied by the
	// gate exactly as the script supplies it (format strings, not sentences), so
	// the arm measures the READER and not a copy of its wording.
	budgetAttributionPurePkgFmt        = "  %s: %s = %ss over %d finished arm(s)"
	budgetAttributionPureUnfinishedFmt = "  ^ %d arm(s) started and never finished: %s"
	budgetAttributionPureArmHeader     = "  top arms:"
	budgetAttributionPureArmFmt        = "  %8.2f  %s"
	budgetAttributionPureNoneLine      = "  (no finished arm)"
	// budgetAttributionPureRunningMarker / budgetAttributionPureAgeFmt /
	// budgetAttributionPureUnknownAgeFmt are the AGE arm's arguments, supplied
	// exactly as the script supplies them. go's own dump prints
	// `name (Round(age, time.Second))` (runningList, testing/testing.go:2490),
	// so the newest starter reads `(2s)` beside a package that burned 600s and
	// a reader concludes it cannot be the culprit — the age is rounded AND it
	// is the age at the dump instant. Measured 2026-11-08 (row 01a1b971): the
	// arm the cap named had been in flight 1.8s of a 180s cap, i.e. 1% of it.
	budgetAttributionPureRunningMarker = "running tests:"
	budgetAttributionPureAgeFmt        = "    %s: in flight %.1fs when the cap expired (its run event %s, the cap line %s)"
	budgetAttributionPureUnknownAgeFmt = "    %s: in flight for an UNMEASURED time"
	// budgetAttributionUnknownAgeMarker is the distinguishing half of that
	// sentence, so the gate asserts on the WORD rather than on the format
	// string's own `%s` (which never appears in rendered output).
	budgetAttributionUnknownAgeMarker = "in flight for an UNMEASURED time"
	budgetAttributionPureArms         = "15"
)

// budgetAttributionRun drives the SHIPPED reader over a stream fixture. It runs
// bash with the lib sourced — the same file the entry point sources — so the
// arm exercises the shipped code rather than a paraphrase of it.
func budgetAttributionRun(t *testing.T, root, stream string) string {
	t.Helper()
	lib := filepath.Join(root, filepath.FromSlash(budgetAttributionLibRel))
	args := []string{"-c",
		`. "$1"; shift; gopi_stream_attribution "$@"`,
		"_", lib, stream, budgetAttributionPureArms,
		budgetAttributionPurePkgFmt, budgetAttributionPureUnfinishedFmt,
		budgetAttributionPureArmHeader, budgetAttributionPureArmFmt,
		budgetAttributionPureNoneLine, "ΣElapsed (LOWER BOUND)", "ΣElapsed",
		budgetAttributionPanicMarker, budgetAttributionPureRunningMarker,
		budgetAttributionPureAgeFmt, budgetAttributionPureUnknownAgeFmt}
	cmd := exec.Command(budgetShellBin, args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("driving %s failed: %v\n%s", budgetAttributionLibRel, err, out)
	}
	return string(out)
}

// budgetAttributionWriteStream writes a synthetic `go test -json` stream.
func budgetAttributionWriteStream(t *testing.T, name string, lines ...string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0o600); err != nil {
		t.Fatalf("write %s: %v", name, err)
	}
	return path
}

// TestBudgetRunAttributesTheWallClock is the acceptance gate for the block.
func TestBudgetRunAttributesTheWallClock(t *testing.T) {
	root, err := repoRootFromCwd()
	if err != nil {
		t.Fatalf("locate repo root: %v", err)
	}
	raw, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(budgetScriptPath)))
	if err != nil {
		t.Fatalf("read %s: %v", budgetScriptPath, err)
	}
	script := string(raw)

	// (1) the block exists, is printed by every run, and takes its wording from
	// named constants (Rule 1a/13).
	for _, decl := range []string{
		budgetAttributionHeadingConst, budgetAttributionQualifierConst,
		budgetAttributionLowerLabelConst,
	} {
		if !strings.Contains(script, decl) {
			t.Errorf("%s declares no %s… — the attribution block's wording must live in a named "+
				"constant, or it can be reworded away with nothing to notice", budgetScriptPath, decl)
		}
	}
	if !strings.Contains(script, budgetAttributionCallSite) {
		t.Errorf("%s never calls %s — a block that is not printed cannot attribute anything",
			budgetScriptPath, budgetAttributionCallSite)
	}
	if !strings.Contains(script, "$stream_attribution_lib") {
		t.Errorf("%s does not source the shared attribution reader (%s) — the reader must be ONE "+
			"definition, shared by the entry point and this gate", budgetScriptPath, budgetAttributionLibRel)
	}
	// The call site must sit in the ALWAYS-printed summary region, not inside the
	// timeout branch: the reading is what makes the next trip interpretable.
	summaryAt := strings.Index(script, "--- summary (")
	callAt := strings.Index(script, "\n"+budgetAttributionCallSite+"\n")
	tripAt := strings.Index(script, "TIMEOUT, NOT AN ASSERTION FAILURE")
	if summaryAt < 0 || callAt < 0 {
		t.Fatalf("%s has no summary region or no %s call to place", budgetScriptPath, budgetAttributionCallSite)
	}
	if callAt < summaryAt {
		t.Errorf("%s calls %s BEFORE the summary region — the reader would be printing the block for "+
			"run it has not finished reading", budgetScriptPath, budgetAttributionCallSite)
	}
	if tripAt > 0 && callAt < summaryAt {
		t.Errorf("%s places %s inside the timeout path only: an attribution a reader has never seen on a "+
			"working day is one nobody can calibrate against", budgetScriptPath, budgetAttributionCallSite)
	}

	// (2)+(3) the reader's two claims, driven on streams whose answers are known.
	completed := budgetAttributionWriteStream(t, "completed.json",
		`{"Action":"run","Package":"example.com/m/pkg","Test":"TestFast"}`,
		`{"Action":"pass","Package":"example.com/m/pkg","Test":"TestFast","Elapsed":0.5}`,
		`{"Action":"run","Package":"example.com/m/pkg","Test":"TestSlow"}`,
		`{"Action":"pass","Package":"example.com/m/pkg","Test":"TestSlow","Elapsed":2.5}`,
		`{"Action":"pass","Package":"example.com/m/pkg","Elapsed":3.0}`,
	)
	out := budgetAttributionRun(t, root, completed)
	for _, want := range []string{"2.5", "TestSlow", "2 finished arm(s)"} {
		if !strings.Contains(out, want) {
			t.Errorf("a COMPLETED package's attribution does not carry %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, budgetAttributionLowerMarker) {
		t.Errorf("a package whose own verdict arrived was labelled %q — the label must mean something:\n%s",
			budgetAttributionLowerMarker, out)
	}

	cut := budgetAttributionWriteStream(t, "cut.json",
		`{"Action":"run","Package":"example.com/m/pkg","Test":"TestDone"}`,
		`{"Action":"pass","Package":"example.com/m/pkg","Test":"TestDone","Elapsed":1.0}`,
		`{"Action":"run","Package":"example.com/m/pkg","Test":"TestNeverReported"}`,
		`{"Action":"output","Package":"example.com/m/pkg","Output":"`+budgetAttributionPanicMarker+`5s\n"}`,
		`{"Action":"fail","Package":"example.com/m/pkg","Elapsed":5.0}`,
	)
	out = budgetAttributionRun(t, root, cut)
	// The label is read from the script's OWN constant, so the gate cannot be
	// satisfied by a second spelling of the word (Rule 13).
	m := regexp.MustCompile(`(?m)^` + regexp.QuoteMeta(budgetAttributionLowerLabelConst[:len(budgetAttributionLowerLabelConst)-1]) + `"([^"]+)"`).FindStringSubmatch(script)
	if m == nil {
		t.Fatalf("%s declares no %s — the lower-bound label's one definition must exist",
			budgetScriptPath, budgetAttributionLowerLabelConst)
	}
	for _, want := range []string{m[1], "TestNeverReported", "1.0"} {
		if !strings.Contains(out, want) {
			t.Errorf("a package CUT BY THE CAP is not labelled %q in its attribution:\n%s", want, out)
		}
	}
	// The count that READS the lower bound: one arm began and never reported.
	if !strings.Contains(out, "1 arm(s) started and never finished") {
		t.Errorf("the attribution does not count the arms that started and never finished — that count is "+
			"what makes the LOWER BOUND readable:\n%s", out)
	}
	// A stream that carries NO `running tests:` line must say the age is
	// UNMEASURED rather than invent one (Rule 15: a refusal names its cause).
	if !strings.Contains(out, budgetAttributionUnknownAgeMarker) {
		t.Errorf("a cut package with no `running tests:` timestamp in its stream did not report the age as "+
			"UNMEASURED — a missing measurement must never be rendered as a number:\n%s", out)
	}

	// (5) THE AGE ARM: `running tests:` names an arm but cannot say HOW LONG it
	// had been in flight. go's own `(0.0s)` is truncated to whole seconds and
	// taken at the dump, so the newest starter reads `(0s)` beside a 600s
	// package and a reader concludes it cannot be the culprit. The two
	// timestamps that answer it are already in the stream — the arm's own `run`
	// event and the `running tests:` line itself — so the reader must subtract
	// them and print the result.
	aged := budgetAttributionWriteStream(t, "aged.json",
		`{"Action":"run","Package":"example.com/m/pkg","Test":"TestDone","Time":"2026-11-08T13:04:00.000000000+08:00"}`,
		`{"Action":"pass","Package":"example.com/m/pkg","Test":"TestDone","Elapsed":1.0,"Time":"2026-11-08T13:04:01.000000000+08:00"}`,
		`{"Action":"run","Package":"example.com/m/pkg","Test":"TestInFlight","Time":"2026-11-08T13:04:27.900000000+08:00"}`,
		`{"Action":"output","Package":"example.com/m/pkg","Output":"panic: test timed out after 180s\n","Time":"2026-11-08T13:04:29.700000000+08:00"}`,
		`{"Action":"output","Package":"example.com/m/pkg","Output":"\trunning tests:\n","Time":"2026-11-08T13:04:29.739000000+08:00"}`,
		`{"Action":"output","Package":"example.com/m/pkg","Output":"\t\tTestInFlight (2s)\n","Time":"2026-11-08T13:04:29.740000000+08:00"}`,
		`{"Action":"fail","Package":"example.com/m/pkg","Elapsed":29.7,"Time":"2026-11-08T13:04:30.000000000+08:00"}`,
	)
	out = budgetAttributionRun(t, root, aged)
	// 13:04:29.739 − 13:04:27.900 = 1.839s — the reading that turns "it was in
	// flight" into "it had been in flight 1.8s of a 180s cap", i.e. NOT the
	// culprit. Hard-coded on purpose: the arithmetic is the claim.
	for _, want := range []string{"TestInFlight", "in flight 1.8s", "13:04:27.900", "13:04:29.739"} {
		if !strings.Contains(out, want) {
			t.Errorf("the age arm does not report %q for an arm cut 1.839s into its body — without it the "+
				"report names a test and leaves its duration to the reader's guess:\n%s", want, out)
		}
	}
	if strings.Contains(out, "(2s)") && !strings.Contains(out, "in flight 1.8s") {
		t.Errorf("the age arm echoed go's ROUNDED `(2s)` instead of the stream's own difference:\n%s", out)
	}

	// (4) THE RED PROOF: the pre-fix evidence shape is prose. A reader driven
	// with it must produce NOTHING, which is precisely why the pre-fix script
	// could not attribute the wall it spent.
	prose := budgetAttributionWriteStream(t, "prefix-prose.txt", budgetAttributionRedProofProse)
	if out := budgetAttributionRun(t, root, prose); strings.TrimSpace(out) != "" {
		t.Errorf("the reader produced attribution from PROSE alone (%q) — the pre-fix summary shape must "+
			"yield nothing, or the red proof is not a proof:\n%s", budgetAttributionRedProofProse, out)
	}
}

// TestBudgetConstantCarriesItsDerivation pins the constant TO its derivation,
// so the arithmetic and the number cannot drift apart (Rule 38: a figure a
// reader acts on names the run it came from, and here it names the stream).
func TestBudgetConstantCarriesItsDerivation(t *testing.T) {
	root, err := repoRootFromCwd()
	if err != nil {
		t.Fatalf("locate repo root: %v", err)
	}
	raw, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(budgetScriptPath)))
	if err != nil {
		t.Fatalf("read %s: %v", budgetScriptPath, err)
	}
	script := string(raw)

	m := regexp.MustCompile(budgetAttributionBudgetConstRe).FindStringSubmatch(script)
	if m == nil {
		t.Fatalf("%s declares no `test_budget=\"…\"` — the budget must exist as ONE named constant "+
			"(Rule 1a/2)", budgetScriptPath)
	}
	if m[1] != budgetAttributionDerivationBudget {
		t.Errorf("the budget constant is %q but the derivation beside it is written for %q — a derivation "+
			"that describes another number is worse than none: it reads as evidence (Rule 15)",
			m[1], budgetAttributionDerivationBudget)
	}
	// The derivation must sit ABOVE the constant, carry the criterion verbatim
	// from the script's own usage block, and cite the measured stream.
	above := script[:strings.Index(script, "\ntest_budget=\"")]
	for _, want := range []string{
		budgetAttributionDerivationToken,
		budgetAttributionDerivationReading,
		budgetAttributionDerivationStream,
	} {
		if !strings.Contains(above, want) {
			t.Errorf("the derivation above test_budget does not carry %q — the value was set from a "+
				"measurement, and the block must name it (criterion, reading, stream)", want)
		}
	}
}
