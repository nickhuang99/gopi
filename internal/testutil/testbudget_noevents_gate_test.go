package testutil

// THE NOT-MEASURED GATE: "go produced no structured event" is not a test verdict.
//
// Measured 2026-10-12 on this host: /home sat at 100% (2.9G free) and a budget
// run's stream came back 0 BYTES — go died writing its own output before the
// test framework started. The entry point's report said
//
//	executed: 0 test(s) ran      stamp: … exit=1 outcome=fail
//
// i.e. exactly what a broken/empty suite prints, and the real cause (a full
// filesystem, a toolchain that never started) appeared NOWHERE in a report whose
// whole job is to name it. The empty-stream NOTE the script printed said only
// that the COUNT could not be read, and it went to stderr.
//
// scripts/test-budget.sh is the declared entry point on the merge/verify path,
// so the discrimination lives there: a run whose stream carries NO structured
// event gets its own exit code (6), its own banner, go's OWN words quoted
// verbatim, and the disk figures for the artifact directory and `/`. This file
// is the gate that keeps it — and keeps the five EXISTING codes meaning what
// they meant (0 green / 1 failure / 2 usage / 3 budget trip / 4 scope / 5 filter
// matched nothing).
//
// HOW THE TWO SHAPES ARE PRODUCED WITHOUT FILLING A DISK (Rule 27): a stub `go`
// is placed first on PATH. It answers the script's tested-binary build with
// success (so the run's own preflight stays quiet) and then answers the test run
// with (a) nothing at all — the 0-byte shape, byte-for-byte what the exhausted
// partition produced — or (b) one line of unstructured text. No device load is
// generated; the mechanism under test is "no event arrived", which the stub
// produces exactly.

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

const (
	// budgetNoEventsExit is the script's dedicated code for "no structured
	// event": distinct from 1 (a failure), 3 (a budget trip) and 5 (a filter
	// that matched nothing).
	budgetNoEventsExit = 6
	// budgetNoEventsBanner is the headline the script must print for it.
	budgetNoEventsBanner = "NOT MEASURED, GO PRODUCED NO TEST EVENT"
	// budgetNoEventsOutcome is the stamp's outcome word, so a reader of the
	// stamp log sees the verdict without reading the body.
	budgetNoEventsOutcome = "not-measured"
	// budgetNoEventsExecutedLine is the summary line a reader mistakes for a
	// test result ("the suite ran and had nothing to do") when it stands alone.
	budgetNoEventsExecutedLine = "executed: 0 test(s) ran"
	// budgetNoEventsNoEventLine is the sentence that must ride BESIDE it, so the
	// summary cannot be read as a test result.
	budgetNoEventsNoEventLine = "go produced NO structured event at all"
	// budgetNoEventsShapeEmpty / …Text are the two shapes' own sentences: the
	// evidence, named rather than asserted.
	budgetNoEventsShapeEmpty = "the stream is 0 bytes"
	budgetNoEventsShapeText  = "ZERO structured events"
	// budgetNoEventsDiagnosisLabel heads go's own words inside the block.
	budgetNoEventsDiagnosisLabel = "go's OWN words from that stream"
	// budgetNoEventsDiskLabel heads the two filesystem figures.
	budgetNoEventsDiskLabel = "disk:"
	// budgetNoEventsFailureBlock heads the assertion/build failure block: a run
	// that measured nothing must NOT be presented through it.
	budgetNoEventsFailureBlock = "--- failures (from this run's own events"
	// budgetNoEventsPkg is the package the arms request. It does not matter
	// which one: the stub `go` replaces the run — the value only has to resolve
	// so the scope check has something to compare.
	budgetNoEventsPkg = "./internal/smokegate/"

	// --- the stub `go` ---
	// budgetStubModeNoOutput produces the 0-byte stream (the measured
	// disk-exhaustion shape).
	budgetStubModeNoOutput = "no-output"
	// budgetStubModeTextOnly produces a stream holding TEXT but not one JSON
	// event (go spat its own diagnostic and died).
	budgetStubModeTextOnly = "text-only"
	// budgetStubModeOneEvent is the POSITIVE CONTROL: a stream carrying one
	// structured failure event, so the run keeps the ordinary failure verdict
	// (exit 1) — proof that the new branch captures only the event-less shape.
	budgetStubModeOneEvent = "one-event"
	// budgetStubDiagnosis is the sentence the text-only arm prints: the stand-in
	// for go's own diagnostic, quoted back by the report.
	budgetStubDiagnosis = "go: writing test output: no space left on device (stub go, no disk was filled)"
	// budgetStubEvent is the one structured event the control arm emits.
	budgetStubEvent = `{"Action":"fail","Package":"github.com/nick/gopi/internal/smokegate","Test":"TestStubGoNeverRan"}`
	// budgetStubBuildCase is the stub's answer to the script's tested-binary
	// build: success, so the run's own "not evidence about this tree" preflight
	// stays out of the way and only the test run is under test.
	budgetStubBuildCase = "*build*"
	// budgetStubBinName is the file the stub is written as.
	budgetStubBinName = "go"
)

// TestBudgetNoStructuredEventIsNotAMeasurement is the acceptance gate: both
// event-less shapes are measured through the real entry point against the real
// repo, and each must produce the marked verdict — not `executed: 0` + fail.
func TestBudgetNoStructuredEventIsNotAMeasurement(t *testing.T) {
	root, err := repoRootFromCwd()
	if err != nil {
		t.Fatalf("locate repo root: %v", err)
	}

	for _, tc := range []struct {
		name  string
		mode  string
		shape string
		// wantDiagnosis is the text go's words must be quoted back from. The
		// 0-byte shape has none — its evidence IS the absence, which the block
		// states.
		wantDiagnosis string
	}{
		{name: "zero-byte stream", mode: budgetStubModeNoOutput, shape: budgetNoEventsShapeEmpty},
		{name: "text but no event", mode: budgetStubModeTextOnly, shape: budgetNoEventsShapeText,
			wantDiagnosis: budgetStubDiagnosis},
	} {
		t.Run(tc.name, func(t *testing.T) {
			budgetStubGoOnPath(t, tc.mode)
			out, code := budgetRunThroughEntryPoint(t, root, budgetNoEventsPkg)
			if code != budgetNoEventsExit {
				t.Errorf("a run whose stream carried no structured event exited %d, want %d — before this "+
					"verdict the report said `executed: 0 test(s) ran` + `outcome=fail`, which reads as a "+
					"broken suite and names the cause nowhere\n%s", code, budgetNoEventsExit, out)
			}
			for _, want := range []string{
				budgetNoEventsBanner,
				tc.shape,
				budgetNoEventsNoEventLine,
				budgetNoEventsDiagnosisLabel,
				budgetNoEventsDiskLabel,
				budgetNoEventsOutcome,
			} {
				if !strings.Contains(out, want) {
					t.Errorf("the run's output does not carry %q — an unmeasurable run must NAME itself "+
						"(Rule 15)\n%s", want, out)
				}
			}
			// The count line is still printed (a reader needs to see it) — but
			// never alone: the sentence beside it is what stops "0 tests ran"
			// from reading as a test result.
			if !strings.Contains(out, budgetNoEventsExecutedLine) {
				t.Errorf("the summary no longer prints %q at all — the count is part of the record\n%s",
					budgetNoEventsExecutedLine, out)
			}
			if !budgetLineAfter(out, budgetNoEventsExecutedLine, budgetNoEventsNoEventLine) {
				t.Errorf("the summary prints %q WITHOUT the no-structured-event sentence on the following "+
					"line — standing alone it reads as 'the suite ran and had nothing to do', which is the "+
					"misreading this verdict removes\n%s", budgetNoEventsExecutedLine, out)
			}
			if tc.wantDiagnosis != "" && !budgetQuotedInDiagnosisBlock(out, tc.wantDiagnosis) {
				t.Errorf("go's own diagnostic %q is not quoted inside the %q block — the cause must appear "+
					"there as EVIDENCE (the one-line echo in the summary is bounded and is not the block)\n%s",
					tc.wantDiagnosis, budgetNoEventsDiagnosisLabel, out)
			}
			if strings.Contains(out, budgetNoEventsFailureBlock) {
				t.Errorf("the run presented itself through the assertion/build failure block — 'nothing was "+
					"measured' and 'the test failed' are different findings\n%s", out)
			}
			if strings.Contains(out, "outcome=fail") {
				t.Errorf("the stamp still reads outcome=fail — go exiting non-zero because it could not "+
					"produce a report is not a failing test\n%s", out)
			}
		})
	}
}

// TestBudgetStructuredEventsKeepTheOrdinaryFailureVerdict is the positive
// control for the pair above: when the stream DOES carry a structured event, the
// new branch must not fire — the run keeps its ordinary failure verdict (exit 1)
// and its failures block. Without this arm the two event-less assertions would
// pass just as well if the script had stopped reporting failures altogether.
func TestBudgetStructuredEventsKeepTheOrdinaryFailureVerdict(t *testing.T) {
	root, err := repoRootFromCwd()
	if err != nil {
		t.Fatalf("locate repo root: %v", err)
	}
	budgetStubGoOnPath(t, budgetStubModeOneEvent)
	out, code := budgetRunThroughEntryPoint(t, root, budgetNoEventsPkg)
	if code != 1 {
		t.Errorf("a run whose stream carries a structured failure event exited %d, want 1 — the "+
			"event-less verdict must not capture a run that DID measure something\n%s", code, out)
	}
	if strings.Contains(out, budgetNoEventsBanner) {
		t.Errorf("a run with a structured event printed %q\n%s", budgetNoEventsBanner, out)
	}
	if !strings.Contains(out, budgetNoEventsFailureBlock) {
		t.Errorf("a failing run no longer reports its failures block\n%s", out)
	}
	if !strings.Contains(out, "outcome=fail") {
		t.Errorf("a failing run's stamp no longer reads outcome=fail\n%s", out)
	}
}

// TestBudgetExitCodeVocabularyIsUnchanged pins the contract the new verdict was
// added to: the five existing codes keep their meaning, the three values that
// are declared as constants are distinct, and every code in the header's table
// is really there. A verdict a reader cannot look up is a verdict that will be
// misread — the whole defect this epic removes.
func TestBudgetExitCodeVocabularyIsUnchanged(t *testing.T) {
	root, err := repoRootFromCwd()
	if err != nil {
		t.Fatalf("locate repo root: %v", err)
	}
	raw, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(budgetScriptPath)))
	if err != nil {
		t.Fatalf("read %s: %v", budgetScriptPath, err)
	}
	script := string(raw)

	// The declared codes: read from the script's own constants (Rule 1a/13:
	// never re-spelled in this file), then required to be the documented values.
	for _, want := range []struct {
		constName string
		value     string
	}{
		{"scope_check_failed_exit", "4"},
		{"no_test_executed_exit", "5"},
		{"not_measured_exit", "6"},
	} {
		re := regexp.MustCompile(`(?m)^` + want.constName + `=([0-9]+)$`)
		m := re.FindStringSubmatch(script)
		if m == nil {
			t.Errorf("%s declares no `%s=<n>` — every non-verdict exit status must live in ONE named "+
				"constant (Rule 1a)", budgetScriptPath, want.constName)
			continue
		}
		if m[1] != want.value {
			t.Errorf("%s declares %s=%s, want %s — the exit-code vocabulary is a contract with every "+
				"caller on the merge/verify path", budgetScriptPath, want.constName, m[1], want.value)
		}
	}
	// 0 green, 1 failure and 3 budget trip are spelled at their decision sites;
	// 2 is the usage/setup status of the script's own refusals. Each is pinned
	// so a future edit cannot silently renumber the contract.
	for _, want := range []string{
		"exit_code=0",
		"exit_code=1",
		"exit_code=3",
		"exit 2",
		budgetTimeoutNamer,
	} {
		if !strings.Contains(script, want) {
			t.Errorf("%s no longer contains %q — the existing exit-code vocabulary must not move",
				budgetScriptPath, want)
		}
	}
	// The header's table is part of the contract: a code with no entry is a code
	// a reader has to guess at.
	for _, want := range []string{
		"  0  green",
		"  1  a test or the build failed",
		"  2  usage/setup error",
		"  3  TIMEOUT — the per-package budget was exhausted",
		"  4  --scope-check only",
		"  5  NO TEST EXECUTED",
		"  6  NOT MEASURED",
	} {
		if !strings.Contains(script, want) {
			t.Errorf("%s's exit-code table no longer documents %q — every code, including the new one, "+
				"must be written down where the reader looks", budgetScriptPath, want)
		}
	}
}

// budgetQuotedInDiagnosisBlock reports whether the report QUOTED want inside the
// block headed by budgetNoEventsDiagnosisLabel — i.e. as the block's own
// evidence, on one of the lines that follow the label (up to the blank line that
// ends it). The summary carries a bounded ONE-LINE echo of the same text, so a
// whole-output substring test would be satisfied by the echo alone and would
// stay green with the block's quote removed (measured: exactly that mutation
// passed before this helper existed).
func budgetQuotedInDiagnosisBlock(out, want string) bool {
	lines := strings.Split(out, "\n")
	for i, line := range lines {
		if !strings.Contains(line, budgetNoEventsDiagnosisLabel) {
			continue
		}
		for _, body := range lines[i+1:] {
			if strings.TrimSpace(body) == "" {
				break
			}
			if strings.Contains(body, want) {
				return true
			}
		}
	}
	return false
}

// budgetLineAfter reports whether want appears on the FIRST line after the
// line that carries anchor — the shape the summary relies on: a reader who
// stops at the count must already meet the verdict beside it. Indentation is
// ignored, so the sentence may be re-indented without breaking the contract.
func budgetLineAfter(out, anchor, want string) bool {
	lines := strings.Split(out, "\n")
	for i, line := range lines {
		if !strings.Contains(line, anchor) || i+1 >= len(lines) {
			continue
		}
		if strings.Contains(lines[i+1], want) {
			return true
		}
	}
	return false
}

// budgetStubGoOnPath installs a stub `go` as the FIRST entry of PATH for this
// test, so the entry point's own two `go` invocations (its tested-binary build,
// then the test run) meet it. It never touches the ambient PATH of anything but
// this test (t.Setenv restores it), and it writes nothing outside t.TempDir().
func budgetStubGoOnPath(t *testing.T, mode string) {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "stubbin")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("create the stub bin dir: %v", err)
	}
	body := "#!/usr/bin/env bash\n" +
		"# test-budget gate fixture: a `go` that never measures anything." +
		" Generated in a test's TempDir, never committed.\n" +
		"case \"$*\" in\n" +
		"  " + budgetStubBuildCase + ") exit 0 ;;\n" +
		"esac\n"
	switch mode {
	case budgetStubModeNoOutput:
		body += "exit 1\n"
	case budgetStubModeTextOnly:
		body += "printf '%s\\n' '" + budgetStubDiagnosis + "' >&2\nexit 1\n"
	case budgetStubModeOneEvent:
		body += "printf '%s\\n' '" + budgetStubEvent + "'\nexit 1\n"
	default:
		t.Fatalf("unknown stub mode %q", mode)
	}
	if err := os.WriteFile(filepath.Join(dir, budgetStubBinName), []byte(body), 0o755); err != nil {
		t.Fatalf("write the stub go: %v", err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
}
