package testutil

// testbudget_freshness_gate_test.go — the STALE-TESTED-BINARY half of the budget
// runner's report: the shell readers that recognise a freshness refusal, and the
// real script's use of them.
//
// THE DEFECT THIS PINS (measured 2026-10-13, run-1791849386-333342, task
// 01a12c66). Twelve of that run's fifteen failing tests were the CLI e2e guards'
// freshness refusal — "…-tested-bin/gopi (mtime 07:56:32) is OLDER than
// …/internal/agent/clock.go (mtime 07:58:50) — this test drives the BUILT binary
// …". The runner printed them as bare `--- FAIL:` markers, so the run read as
// twelve red tests and a red package. What had actually happened: the same run's
// INLINE red proof rewrote internal/agent/clock.go 1.6 seconds AFTER the runner
// built the tested binary and restored it 2m16s later, so the failures were the
// runner's own one-build design meeting its own tree being written under it — and
// no test body ran for any of them.
//
// WHY IT IS TESTED THIS WAY. A real refusal can only be produced by a ~25-minute
// suite against a tree that moves mid-run; an arm nobody can exercise is an arm
// nobody checks. So the arms below are: the SYNTHETIC stream (the library's own
// readers, called the way the script calls them — Rule 14, no Go re-reading of
// the JSON), the REAL script end-to-end against a stub `go` (the pattern
// testbudget_failures_gate_test.go established: cheap, deterministic, no test
// bodies, no device work — Rule 27).
//
// The handed-over branch ALSO carried a third arm here, a DRIFT gate over the
// marker's two spellings. On current master that arm lives in the package that owns
// the Go half: cmd/agent/task_session_cwd_freshness_evidence_test.go's
// TestFreshnessMarkerSpellingsAgree. The refusal is composed by
// gopibin_identity_guard_test.go's mtime arm, so the two spellings are read THERE,
// in the package that holds them both — cmd/agent has no importable path into this
// package's fixtures, and a drift gate is about the FILES agreeing, not about a
// package boundary.

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

const (
	// freshnessFixturePkg is the package the synthetic stream's refusals come
	// from, in the FIXTURE module (the stream is retargeted to the repo's module
	// for the script run, so the run's own scope check stays silent).
	freshnessFixturePkg = budgetFailuresFixtureModule + "/cmd/agent"
	// freshnessRefusingName is the test that refuses (its output carries the
	// guard's sentence), freshnessQuotingName is a PASSING test that merely
	// PRINTS the same sentence — a gate quoting the guard's message, which this
	// repository really does — and freshnessOtherName is a test that fails for an
	// unrelated reason. The three together are what tells "the runner's own
	// refusals" from "a string a passing test was carrying".
	freshnessRefusingName = "TestRefusesAStaleBinary"
	freshnessQuotingName  = "TestQuotesTheRefusalText"
	freshnessOtherName    = "TestFailsForAnotherReason"
	// freshnessRefusalLine is the guard's sentence in miniature: the two mtimes
	// and the marker, in the order the guard composes them.
	freshnessRefusalLine = "    task_session_cwd_e2e_test.go:126: /tmp/x-tested-bin/gopi (mtime 2026-10-13T07:56:32+08:00) " +
		"is OLDER than /tmp/x/internal/agent/clock.go (mtime 2026-10-13T07:58:50+08:00) — this test drives the BUILT binary, so it would verify code that is not in that binary."
	// freshnessMovedVerdict and freshnessPredatesVerdict are the guard's two
	// VERDICT lines, carried on their own rows — the sentences the runner must
	// relay verbatim rather than re-derive.
	freshnessMovedVerdict    = "  VERDICT: the tree MOVED while the suite ran — every source newer than this binary is byte-for-byte what the tree's HEAD holds and HEAD was committed before the binary was built."
	freshnessPredatesVerdict = "  VERDICT: the binary PREDATES this source — the tree's own records show content newer than this binary."
	// freshnessHeadingMarker is the block's heading (the wording lives in the
	// script's freshness_heading and is pinned here).
	freshnessHeadingMarker = "STALE-TESTED-BINARY REFUSALS"
	// freshnessDesignMarker is the sentence that states the RUNNER'S OWN design —
	// the half of the answer the run cannot get from the guard, because the guard
	// does not know who built the binary.
	freshnessDesignMarker = "builds the tested binary ONCE"
)

// freshnessRefusalStream is the synthetic stream: one test that refuses (with its
// verdict row), one PASSING test that quotes the same sentence, and one test that
// fails for an unrelated reason.
func freshnessRefusalStream() string {
	row := func(action, test, output string) string {
		if output == "" {
			return `{"Action":"` + action + `","Package":"` + freshnessFixturePkg + `","Test":"` + test + `"}`
		}
		return `{"Action":"` + action + `","Package":"` + freshnessFixturePkg + `","Test":"` + test + `","Output":"` + output + `\n"}`
	}
	return strings.Join([]string{
		`{"Action":"start","Package":"` + freshnessFixturePkg + `"}`,
		row("run", freshnessRefusingName, ""),
		row("output", freshnessRefusingName, freshnessRefusalLine),
		row("output", freshnessRefusingName, freshnessMovedVerdict),
		row("output", freshnessRefusingName, "--- FAIL: "+freshnessRefusingName+" (0.01s)"),
		`{"Action":"fail","Package":"` + freshnessFixturePkg + `","Test":"` + freshnessRefusingName + `","Elapsed":0.01}`,
		row("run", freshnessQuotingName, ""),
		row("output", freshnessQuotingName, "    gate_test.go:9: the refusal text is "+freshnessRefusalLine),
		row("output", freshnessQuotingName, "--- PASS: "+freshnessQuotingName+" (0.02s)"),
		`{"Action":"pass","Package":"` + freshnessFixturePkg + `","Test":"` + freshnessQuotingName + `","Elapsed":0.02}`,
		row("run", freshnessOtherName, ""),
		row("output", freshnessOtherName, "    other_test.go:4: got 1, want 2"),
		row("output", freshnessOtherName, "--- FAIL: "+freshnessOtherName+" (0.02s)"),
		`{"Action":"fail","Package":"` + freshnessFixturePkg + `","Test":"` + freshnessOtherName + `","Elapsed":0.02}`,
		`{"Action":"fail","Package":"` + freshnessFixturePkg + `","Elapsed":0.05}`,
	}, "\n") + "\n"
}

// TestFreshnessPredicateFindsOnlyTheRefusingTest is the core predicate arm: the
// readers the runner uses must find the ONE refusing test, must not be fooled by
// a PASSING test quoting the same sentence, and must return exactly the guard's
// verdict LINE for it.
func TestFreshnessPredicateFindsOnlyTheRefusingTest(t *testing.T) {
	root, err := repoRootFromCwd()
	if err != nil {
		t.Fatalf("locate repo root: %v", err)
	}
	streamPath := filepath.Join(t.TempDir(), "stream.json")
	if err := os.WriteFile(streamPath, []byte(freshnessRefusalStream()), 0o644); err != nil {
		t.Fatalf("write stream: %v", err)
	}

	count, err := budgetFailuresRunLib(t, root, streamPath, "gopi_stream_freshness_refusal_count")
	if err != nil {
		t.Fatalf("freshness refusal count: %v\n%s", err, count)
	}
	if got := strings.TrimSpace(count); got != "1" {
		t.Errorf("gopi_stream_freshness_refusal_count = %s; want 1 — the PASSING test that merely quotes the refusal "+
			"text must not be counted as a refusal of this run (and the unrelated failure must not be one either)",
			got)
	}

	tests, err := budgetFailuresRunLib(t, root, streamPath, "gopi_stream_freshness_tests")
	if err != nil {
		t.Fatalf("freshness tests: %v\n%s", err, tests)
	}
	if !strings.Contains(tests, freshnessRefusingName) {
		t.Errorf("gopi_stream_freshness_tests does not name the refusing test:\n%s", tests)
	}
	if strings.Contains(tests, freshnessQuotingName) {
		t.Errorf("gopi_stream_freshness_tests names %s, a PASSING test — its own fixture text was harvested as this "+
			"run's refusal:\n%s", freshnessQuotingName, tests)
	}
	if strings.Contains(tests, freshnessOtherName) {
		t.Errorf("gopi_stream_freshness_tests names %s, which failed for an unrelated reason:\n%s", freshnessOtherName, tests)
	}

	verdicts, err := budgetFailuresRunLib(t, root, streamPath, "gopi_stream_freshness_verdicts")
	if err != nil {
		t.Fatalf("freshness verdicts: %v\n%s", err, verdicts)
	}
	if !strings.Contains(verdicts, "the tree MOVED while the suite ran") {
		t.Errorf("the verdict reader does not relay the guard's own VERDICT line — the runner must not re-derive a "+
			"cause the guard already decided (Rule 14):\n%s", verdicts)
	}
	if strings.Contains(verdicts, "PREDATES") {
		t.Errorf("the verdict reader reported a cause the stream does not carry:\n%s", verdicts)
	}
}

// TestBudgetScriptNamesTheFreshnessCause is the end-to-end arm: the REAL script,
// against a stub `go` that answers with the synthetic stream. The summary must
// name the condition, quote the guard's verdict, state the runner's own one-build
// design, and NOT leave the reader with a bare `--- FAIL:` marker.
func TestBudgetScriptNamesTheFreshnessCause(t *testing.T) {
	root, err := repoRootFromCwd()
	if err != nil {
		t.Fatalf("locate repo root: %v", err)
	}
	if _, err := exec.LookPath(budgetShellBin); err != nil {
		t.Skipf("%s not available: %v", budgetShellBin, err)
	}
	module := repoModulePath(t, root)
	stream := strings.ReplaceAll(freshnessRefusalStream(), budgetFailuresFixtureModule, module)
	stubDir := t.TempDir()
	scratchHome := t.TempDir()
	stub := "#!/bin/bash\n" +
		"# Stands in for `go`: it answers `go test … -json` with the fixture stream and\n" +
		"# succeeds for a build, so the script under test runs its real path.\n" +
		"for a in \"$@\"; do\n" +
		"    if [ \"$a\" = \"build\" ]; then\n" +
		"        out=\"\"\n" +
		"        while [ $# -gt 0 ]; do case \"$1\" in -o) out=\"$2\";; esac; shift; done\n" +
		"        [ -n \"$out\" ] && : >\"$out\"\n" +
		"        exit 0\n" +
		"    fi\n" +
		"done\n" +
		"cat <<'FIXTURE_STREAM'\n" + stream + "FIXTURE_STREAM\n" +
		"exit 1\n"
	if err := os.WriteFile(filepath.Join(stubDir, "go"), []byte(stub), 0o755); err != nil {
		t.Fatalf("write stub go: %v", err)
	}

	script := filepath.Join(root, filepath.FromSlash(budgetScriptPath))
	cmd := exec.Command(budgetShellBin, script, "./...")
	cmd.Dir = root
	cmd.Env = append(os.Environ(),
		ScratchHomeEnvName+"="+scratchHome,
		"PATH="+stubDir+string(os.PathListSeparator)+os.Getenv("PATH"),
	)
	out, _ := cmd.CombinedOutput() //stylegate:ignore the child's exit code is asserted through the report text, which is the signal under test
	report := string(out)
	for _, want := range []string{freshnessHeadingMarker, freshnessDesignMarker, "the tree MOVED while the suite ran"} {
		if !strings.Contains(report, want) {
			t.Errorf("the run's summary does not contain %q — the freshness refusals stay unnamed and the run reads as "+
				"a code verdict:\n%s", want, report)
		}
	}
	// The PASSING test that merely quotes the refusal text must not be reported as
	// a refusal — asserted over the BLOCK, not the whole report: the slowest-tests
	// list names passing tests by design, so a whole-report search would fail on a
	// legitimate line and tell us nothing about the freshness block.
	if at := strings.Index(report, freshnessHeadingMarker); at >= 0 {
		if block := report[at:]; strings.Contains(block, freshnessQuotingName) {
			t.Errorf("the freshness block names %s, a PASSING test — its own quoted fixture text was harvested as "+
				"this run's refusal:\n%s", freshnessQuotingName, block)
		}
	}
	// It must sit with the failures, not merely somewhere in the report: the
	// reader who counted the `--- FAIL:` rows is the reader this block corrects.
	if failuresAt, freshnessAt := strings.Index(report, "--- failures"), strings.Index(report, freshnessHeadingMarker); failuresAt < 0 || freshnessAt < failuresAt {
		t.Errorf("the freshness block does not follow the failures block (failures at %d, freshness at %d):\n%s",
			failuresAt, freshnessAt, report)
	}
}
