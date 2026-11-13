package testutil

// The ROUTING-predicate regression tests (task 01a1137f, 2026-10-07), split
// from scripts_gate_test.go so the gate itself stays readable: the gate's rules
// live there, the evidence that the rules CATCH what they claim lives here.
//
// Every test drives the REAL gate (assertScratchHelpersAreUsed /
// scratchConstructionsOf / routesToScratchHelper) through a recordingT — never a
// copy of its predicate, because a test that re-implements the predicate agrees
// with whatever bug it is supposed to catch.

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const (
	// scratchGateFixtureDir holds the shell probes these tests drive: synthetic
	// fixtures plus the verbatim UNFIXED scripts/replay/chat_replay_xtest.sh,
	// taken from the commit before its own fix, so the documented red-to-fix
	// pair stays in the tree. internal/ is not an audited script root, so this
	// corpus is never scanned by the tree walk — it is input data, not a script.
	scratchGateFixtureDir = "testdata/scratch-gate"
	// scratchGateMinFixtures is the corpus floor: one probe per OFFENDER shape
	// (prose-only mention; routing added while the own mktemp is kept; the real
	// unfixed replay script). A corpus that shrank must fail loudly rather than
	// let these regression tests pass on an empty directory.
	scratchGateMinFixtures = 3
	// The fixture file names, each spelled once.
	scratchGateProseProbe    = "mentions-helper-in-prose.sh"
	scratchGateRoutedProbe   = "routed-but-kept-mktemp.sh"
	scratchGateUnfixedReplay = "chat_replay_xtest-unfixed.sh"
	// The 1-based lines each fixture keeps its own construction on. They are
	// pinned deliberately: the gate must name the SITE, not merely fail
	// somewhere in the file.
	scratchProseProbeMktempLine    = 11
	scratchRoutedProbeMktempLine   = 9
	scratchUnfixedReplayMktempLine = 66
)

// TestScratchRoutingRegressionFixturesFail pins the THREE offender shapes the
// tightened predicate must catch, each with the line:file the gate has to name:
//
//  1. the prose-only offender — the helper's name in a comment, an echo and a
//     `[ -r … ]` probe, no routing statement at all (the old predicate's pass);
//  2. the marginal probe — a REAL `. ` statement ADDED while the script's own
//     `mktemp -d` is KEPT (the brief's step-1..step-4 repro);
//  3. the same offender as it actually stood in the tree: the UNFIXED
//     scripts/replay/chat_replay_xtest.sh (203 lines, its own `mktemp -d -p`).
func TestScratchRoutingRegressionFixturesFail(t *testing.T) {
	entries, err := os.ReadDir(scratchGateFixtureDir)
	if err != nil {
		t.Fatalf("read the fixture corpus %s: %v", scratchGateFixtureDir, err)
	}
	if len(entries) < scratchGateMinFixtures {
		t.Fatalf("the fixture corpus holds %d files (want at least %d) — the probes are missing, so "+
			"these regression tests would pass on an empty tree", len(entries), scratchGateMinFixtures)
	}
	cases := []struct {
		name    string
		file    string
		routed  bool // does the fixture carry a real routing statement?
		command string
		line    int
	}{
		{scratchGateProseProbe, scratchGateProseProbe, false, scratchTempCommandShell, scratchProseProbeMktempLine},
		{scratchGateRoutedProbe, scratchGateRoutedProbe, true, scratchTempCommandShell, scratchRoutedProbeMktempLine},
		{scratchGateUnfixedReplay, scratchGateUnfixedReplay, false, scratchTempCommandShell, scratchUnfixedReplayMktempLine},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := readFixtureScript(t, tc.file)
			if routed, _ := routesToScratchHelper(f, scratchHelperShell); routed != tc.routed {
				t.Fatalf("%s: routesToScratchHelper = %v, want %v — the fixture, not the predicate, "+
					"is wrong (this file is frozen evidence of one shape)", tc.file, routed, tc.routed)
			}
			wantRedOnKeptConstruction(t, f, tc.command, tc.line)
		})
	}
}

// TestOldPredicateWouldPassTheOffenders is the RED PROOF, in process: the
// predicate this change REPLACED is spelled out verbatim and run over the same
// fixtures — it calls each offender compliant, which is exactly why the new
// predicate is not optional. Re-introduce the substring rule and this test is
// the record of what it costs.
func TestOldPredicateWouldPassTheOffenders(t *testing.T) {
	for _, name := range []string{scratchGateProseProbe, scratchGateRoutedProbe} {
		f := readFixtureScript(t, name)
		if !oldHelperMentioned(f, scratchHelperShell) {
			t.Fatalf("%s is NOT recognised by the old predicate — the fixture no longer reproduces the "+
				"false negative this gate was tightened for; fix or drop the fixture", name)
		}
		if got := scratchConstructionsOf(f); len(got) == 0 {
			t.Fatalf("%s keeps no own construction, so the old predicate's pass is not a false negative "+
				"for it — the fixture no longer reproduces the defect", name)
		}
	}
}

// TestCompliantScratchRoutingStaysGreen is the anti-over-fire half: the helper's
// own generator is not a construction, a $TMPDIR the script pointed INTO the
// helper's tree may be mkdir'd under, and the Python import form routes. A
// predicate that flagged these would be red on the day it landed —
// scripts/e2e_smoke.sh is the live form of the second one.
func TestCompliantScratchRoutingStaysGreen(t *testing.T) {
	compliant := []struct {
		name string
		src  string
	}{
		{"helper-generator-only", `
. "$HERE/lib/tmp-ban.sh"
WORK=$(gopi_scratch_mkdir probe) || exit 1
echo "$WORK" >"$WORK/out.txt"
`},
		{"tmpdir-pointed-into-helper-tree", `
. "$HERE/lib/tmp-ban.sh"
SCRATCH="$(gopi_scratch_mkdir smoke)" || exit 2
export TMPDIR="$SCRATCH/tmp"
mkdir -p "$TMPDIR"
`},
		{"python-helper-import", `
from lib.tmp_ban import scratch_dir
with scratch_dir("probe") as d:
    print(d)
`},
	}
	for _, tc := range compliant {
		t.Run(tc.name, func(t *testing.T) {
			f := fixtureScript(tc.name+".sh", tc.src, true)
			if got := scratchConstructionsOf(f); len(got) != 0 {
				t.Fatalf("the compliant form %q was flagged as a scratch construction at line %d (%s) — "+
					"a routed script may take its directory from the helper and point $%s into it",
					tc.name, got[0].line, got[0].command, scratchTempRootVar)
			}
		})
	}
}

// TestCompoundLineRoutingIsSeen pins shellStatements, the split found by
// RUNNING the brief's own repro rather than reading the code: that repro writes
// the routing statement on a COMPOUND line —
//
//	HERE="…"; . "$HERE/../lib/tmp-ban.sh"
//
// — and reading only the first verb of a line (an assignment) classified the
// very shape this change exists to catch as "does not route". Both directions
// are asserted: the statement IS seen at its own line, and the kept `mktemp` on
// a later line is the line the gate names.
func TestCompoundLineRoutingIsSeen(t *testing.T) {
	src := "#!/bin/bash\n" +
		"set -uo pipefail\n" +
		"HERE=\"$(dirname -- \"${BASH_SOURCE[0]}\")\"; . \"$HERE/../lib/tmp-ban.sh\" || exit 1\n" +
		"SCRATCH_PARENT=\"${TMPDIR:-$HOME/.cache}\"\n" +
		"SB=$(mktemp -d -p \"$SCRATCH_PARENT\" compound.XXXXXX) || exit 1\n"
	f := fixtureScript("compound-line.sh", src, true)
	const routeLine, constructionLine = 3, 5
	routed, gotLine := routesToScratchHelper(f, scratchHelperShell)
	if !routed || gotLine != routeLine {
		t.Fatalf("routesToScratchHelper = (%v, %d), want (true, %d) — a `. ` statement on a compound "+
			"line routes exactly like one on a line of its own", routed, gotLine, routeLine)
	}
	wantRedOnKeptConstruction(t, f, scratchTempCommandShell, constructionLine)
}

// oldHelperMentioned is the predicate this change REPLACED, spelled verbatim as
// the thing the regression tests are proved against: compliance by
// `strings.Contains(body, filepath.Base(helper))` over ALL lines, comments
// included, with no statement required.
func oldHelperMentioned(f scriptFile, helper string) bool {
	return strings.Contains(strings.Join(f.lines, "\n"), filepath.Base(helper))
}

// wantRedOnKeptConstruction asserts the gate reports the kept construction at
// its exact line, by running the REAL check through a recording T.
func wantRedOnKeptConstruction(t *testing.T, f scriptFile, command string, line int) {
	t.Helper()
	rec := &recordingT{}
	assertScratchHelpersAreUsed(rec, []scriptFile{f})
	if len(rec.errors) == 0 {
		t.Fatalf("%s passed the routing gate while keeping its own `%s` — the false negative is back "+
			"(the gate must require the helper to be the ONE construction)", f.rel, command)
	}
	want := fmt.Sprintf("%s:%d", f.rel, line)
	for _, e := range rec.errors {
		if strings.Contains(e, want) {
			return
		}
	}
	t.Fatalf("the gate did not name %q — it reported %v", want, rec.errors)
}

// readFixtureScript reads one fixture from the gate's own corpus. The path is
// relative to the PACKAGE directory (a test binary runs with its package as the
// working directory), so the corpus travels with the test that drives it; only
// the rel label — what the gate's message must name — is repo-anchored.
func readFixtureScript(t *testing.T, name string) scriptFile {
	t.Helper()
	path := filepath.Join(scratchGateFixtureDir, name)
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read fixture %s: %v", path, err)
	}
	return scriptFile{
		rel:   testutilPackageDir + "/" + scratchGateFixtureDir + "/" + name,
		lines: strings.Split(string(raw), "\n"),
		shell: strings.HasSuffix(name, ".sh"),
	}
}

// fixtureScript builds an in-memory scriptFile from source, for the minimal
// probes that need no committed file.
func fixtureScript(name, src string, shell bool) scriptFile {
	return scriptFile{rel: name, lines: strings.Split(src, "\n"), shell: shell}
}

// gateFailer is the slice of *testing.T the routing gate needs: it REPORTS
// failures. Narrowing it to an interface is what lets these tests drive the
// real gate through a recordingT and assert on what it said, instead of
// re-implementing its predicate.
type gateFailer interface {
	Errorf(format string, args ...any)
	Error(args ...any)
	Helper()
}

// recordingT is a *testing.T substitute that RECORDS the gate's failures
// instead of failing the run.
type recordingT struct {
	errors []string
}

// Errorf records one failure the gate produced.
func (r *recordingT) Errorf(format string, args ...any) {
	r.errors = append(r.errors, fmt.Sprintf(format, args...))
}

// Error records one failure the gate produced without formatting.
func (r *recordingT) Error(args ...any) {
	r.errors = append(r.errors, fmt.Sprint(args...))
}

// Helper is a no-op: there is no caller stack to trim.
func (r *recordingT) Helper() {}
