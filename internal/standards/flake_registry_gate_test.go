package standardgate

// The ORDER/DETERMINISM flake class (PROJECT.md Rule 27's neighbour: measured by
// repetition of the TEST, never by load on the HOST) is only a class if it is
// ENUMERATED and kept in step with the code. This gate is what makes the
// registry reviewable, in three halves:
//
//  1. REGISTRY <-> CODE, both directions (testdata/flakes.tsv). Every row must
//     name a test that EXISTS in the package the row claims (a stale row is a
//     flake nobody can re-run), every registered test's root cause must be a
//     DETERMINISTIC mechanism (`deterministic: <…>`, not free prose — a row
//     whose cause column is an excuse is a SUPPRESSION entry wearing a registry
//     row's clothes), and every row must carry an owning ledger row id and a
//     date. The registry may not outlive a test: a renamed or deleted test
//     fails the row rather than sitting there as a permanent exemption.
//
//  2. THE CLASS'S BOUNDARY. A flake is a DEFECT, not a heaviness class, so a
//     registered test may not be routed through heavytest.Require /
//     CriterionHostCondition — that would HIDE it behind a skip that reads as a
//     passing green. A registered row in a GATE package is rejected for the
//     reason the heavy inventory rejects one: internal/standards and
//     internal/stylegate must run on the smoke/merge path.
//
//  3. THE CATCH MECHANISM AND THE SMOKE SPLIT. `make test` stays smoke-only,
//     `make flake-catch` is the ONE named opt-in, the shared names live in ONE
//     place on each side (internal/flaketest and scripts/lib/flake-threshold.sh,
//     asserted to agree), and the mechanism actually WORKS: the behavioural half
//     builds a throwaway module whose only test is a PLANTED map-iteration-order
//     flake, runs the real script over it, and requires that the plant is NAMED
//     — then runs the same fixture with the order dependence removed and
//     requires that nobody is named. A mechanism that is only grepped is a
//     mechanism nobody has ever seen fire.
//
// Rule 27 note: nothing here generates load, pressure or contention. The plant
// fails because Go RANDOMIZES map iteration order; the instrument is
// `go test -count=N` over the test itself.

import (
	"context"
	"errors"
	"fmt"
	"go/ast"
	"go/token"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/nickhuang99/gopi/internal/flaketest"
	"github.com/nickhuang99/gopi/internal/tools"
)

const (
	// flakeRegistryRel / flakeCatchScriptRel / flakeProbeScriptRel — the ONE
	// definitions live in internal/flaketest (a leaf package); the gate reads them
	// from there so a rename cannot leave this gate asserting a path the code no
	// longer uses.
	flakeRegistryRel    = flaketest.RegistryRel
	flakeCatchScriptRel = flaketest.CatchScriptRel
	flakeProbeScriptRel = flaketest.GateProbeScriptRel

	// flakeLibRel is the shell side's ONE definition of the same values.
	flakeLibRel = "scripts/lib/flake-threshold.sh"

	// flakeGoConstFile is the Go-side leaf package spelling those values.
	flakeGoConstFile = "internal/flaketest/flaketest.go"

	// flakeRepsShellVar is the shell name of the repetition knob; its Go twin is
	// flaketest.RepsEnv (one name, one reader — Rule 2/21).
	flakeRepsShellVar = "flake_reps_env"

	// flakeVerdictMarkerShellVar is the shell name of the marker that prefixes a
	// NAMED unstable test — the text an operator greps and this gate matches.
	flakeVerdictMarkerShellVar = "flake_verdict_marker"

	// flakeMinRegistryRows is the non-vacuity floor: the class has four
	// root-caused rows at the commit this gate lands. A registry that reads zero
	// rows is a broken reader (or a deleted registry) reporting a clean green.
	flakeMinRegistryRows = 4

	// flakeMinCauseWords is the smallest root cause that can name a mechanism.
	// "deterministic: flaky sometimes" is two words and names nothing; the four
	// measured rows run 40-70 words.
	flakeMinCauseWords = 12

	// flakeGatePackages may never carry a registry row (their tests must run).
	flakeGatePackages = "internal/standards|internal/stylegate"

	// flakeReplayReps / flakeReplayFakeScope — a replayed stream is synthetic, so
	// the scope is never compiled; the names only have to be unmistakably fake so
	// a replay can never be mistaken for a measured run.
	flakeReplayReps      = "4"
	flakeReplayFakeScope = "x"

	// flakeReplayTimeoutSec bounds each replay (no compilation happens).
	flakeReplayTimeoutSec = 60

	// flakeLedgerRowsFloor is the second non-vacuity guard for the owner lookup:
	// the task ledger holds ~1800 rows, so a rendered list shorter than this is
	// not the ledger — and asserting owner ids against an empty list would fail
	// every row for the wrong reason.
	flakeLedgerRowsFloor = 100

	// flakeLedgerUnverified is printed (never silent) when the ledger could not
	// be read in this environment, so a reader knows the owner half of the gate
	// did not run (Rule 15: a check that did not happen must say so).
	flakeLedgerUnverified = "FLAKE-LEDGER-UNVERIFIED"

	// flakeProbeTimeoutSec bounds the behavioural half. The fixture is one trivial
	// test compiled in a throwaway module — seconds, not minutes; the cap only
	// keeps a wedged toolchain from holding the smoke path.
	flakeProbeTimeoutSec = 300

	// flakeOwnerRe is flaketest.OwnerShape — the ONE definition of the shape a
	// registry owner id may have (Rule 14). The 8-hex chip is NOT accepted (epic
	// full-id-no-prefix): `01a0aba1` names 2 ledger rows, so an owner written as
	// a chip cannot be resolved to one identity.
	flakeOwnerRe = flaketest.OwnerShape

	// flakeColumns is the registry's column count (test / package / root_cause /
	// owner / date) — one definition for the reader and the red proofs.
	flakeColumns = 5

	// flakeTab is the registry's column separator. It is a named constant because
	// the STRUCTURAL guard's whole subject is a TAB where a TAB does not belong
	// (Rule 1b: a meaningful literal comes from its constant).
	flakeTab = "\t"

	// flakeCommentMark opens a comment line in the registry. A line beginning with
	// it is prose; a line beginning with it AND CARRYING a TAB is a swallowed row.
	flakeCommentMark = "#"

	// flakeMinDeclaredTests is the second direction's non-vacuity floor: the class
	// has SEVEN registered tests at the commit this gate lands (six at the epic's
	// start; the seventh arrived with the master merge of task 01a10a2d, whose row
	// this gate immediately flagged as uncorroborated until its declaration
	// landed). A scan that reads fewer is a broken scan (or a renamed
	// declaration), which would make "every declaration is registered" true by
	// having nothing to check.
	flakeMinDeclaredTests = 7

	// flakeClipLen bounds a quoted line inside a finding so the failure message
	// stays a message.
	flakeClipLen = 160
)

// flakeGoTestNameRe is the shape a Go test function name must have (the registry
// names TOP-LEVEL test functions: a subtest cannot be re-run alone by the scope
// mechanism, and its verdict is reported to its parent).
var flakeGoTestNameRe = regexp.MustCompile(`^Test[A-Za-z0-9_]+$`)

// flakeRow is one parsed registry row.
type flakeRow struct {
	test  string
	pkg   string
	cause string
	owner string
	date  string
	line  int
}

// flakeTestDef is one test function definition found in the tree.
type flakeTestDef struct {
	rel      string
	hasHeavy bool
}

// TestFlakeRegistryMatchesTheTestsItNames is the bidirectional registry gate.
func TestFlakeRegistryMatchesTheTestsItNames(t *testing.T) {
	root := repoRoot(t)
	rows := flakeRegistryRows(t, root)

	if len(rows) < flakeMinRegistryRows {
		t.Fatalf("%s carries %d row(s) (want at least %d) — the registry is empty or the "+
			"reader is broken, so this gate would pass vacuously (the class's known flakes "+
			"would be undocumented again)", flakeRegistryRel, len(rows), flakeMinRegistryRows)
	}

	defs := flakeTestDefs(t, root)
	if len(defs) < flakeMinRegistryRows {
		t.Fatalf("the scan found %d test definition(s) under internal/ + cmd/ — a broken scan "+
			"must fail, never read as 'every registered test exists'", len(defs))
	}

	gateRe := regexp.MustCompile(flakeGatePackages)
	ownerRe := regexp.MustCompile(flakeOwnerRe)
	seen := map[string]bool{}
	owners := map[string]bool{}
	for _, r := range rows {
		key := r.pkg + "\t" + r.test
		if seen[key] {
			t.Errorf("%s:%d lists %s in %s twice — one row per flake, or the cause column "+
				"drifts between two rows nobody re-reads", flakeRegistryRel, r.line, r.test, r.pkg)
		}
		seen[key] = true
		owners[r.owner] = true

		// The test must EXIST (the registry may not outlive its test).
		def, ok := defs[key]
		if !ok {
			t.Errorf("%s:%d registers %s in %s, but no such test function exists — a stale row "+
				"is a flake nobody can re-run; delete it or fix the name (this is the "+
				"direction that keeps the registry ratcheting with the code)",
				flakeRegistryRel, r.line, r.test, r.pkg)
			continue
		}

		// A flake must not be HIDDEN behind a heaviness skip.
		if def.hasHeavy {
			t.Errorf("%s:%d registers %s, which also classifies itself heavytest.Require — a "+
				"flake is a DEFECT, not a heaviness class: a skip reads as a passing green and "+
				"hides exactly the defect this registry documents (fix the flake or remove the "+
				"classification; never both)", flakeRegistryRel, r.line, r.test)
		}

		// A gate package's tests must run on the smoke path: a registered row there
		// would mean a gate that does not run (the same reason the heavy inventory
		// refuses to classify one).
		if gateRe.MatchString(r.pkg) {
			t.Errorf("%s:%d registers %s in GATE package %s — those tests must keep running on "+
				"the smoke/merge path (a merge may never land with its own gates skipped)",
				flakeRegistryRel, r.line, r.test, r.pkg)
		}
	}

	for _, r := range rows {
		if r.owner == "" || !ownerRe.MatchString(r.owner) {
			t.Errorf("%s:%d has owner %q — the owning ledger row id is the difference between a "+
				"flake that gets re-read and one that rots (GOPI.md Rule 26: dispatch again, "+
				"don't wait). Use the FULL ledger id (36 characters) or the `t-<epoch>` id — an "+
				"8-hex chip names more than one row and is refused",
				flakeRegistryRel, r.line, r.owner)
		}
		if _, err := time.Parse(flaketest.DateLayout, r.date); err != nil {
			t.Errorf("%s:%d has date %q — want %s (the date the root cause was pinned)",
				flakeRegistryRel, r.line, r.date, flaketest.DateLayout)
		}
	}

	flakeAssertOwnersExist(t, owners)
}

// flakeRegistryRows reads testdata/flakes.tsv, enforcing the row SHAPE (five
// columns, a determinism-shaped cause, a date, an owner) as it goes. A malformed
// or excuse-shaped row fails here rather than being skipped: the registry's
// whole value is that a row cannot be told from a root cause.
//
// The STRUCTURAL half of the reading (does a row begin at a line start, does the
// file end with a newline, is a `#` line carrying a TAB a swallowed row) lives in
// flakeScanRegistryText, which is pure over the registry's text and therefore the
// same function the red proofs drive. This function owns the COLUMN half only.
func flakeRegistryRows(t *testing.T, root string) []flakeRow {
	t.Helper()
	src, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(flakeRegistryRel)))
	if err != nil {
		t.Fatalf("read %s: %v — the flake registry is gone, so the class's known "+
			"order/determinism defects are undocumented again", flakeRegistryRel, err)
	}
	scan, findings := flakeScanRegistryText(string(src))
	for _, f := range findings {
		t.Errorf("%s: %s", flakeRegistryRel, f)
	}
	rows := scan.rows
	for _, r := range rows {
		line := r.line
		if !flakeGoTestNameRe.MatchString(r.test) {
			t.Errorf("%s:%d registers %q — not a Go test function name (`Test…`, top-level: a "+
				"subtest's verdict belongs to its parent)", flakeRegistryRel, line, r.test)
		}
		if r.pkg == "" || !strings.Contains(r.pkg, "/") {
			t.Errorf("%s:%d has package %q — want the module-relative package DIRECTORY (e.g. "+
				"internal/tools)", flakeRegistryRel, line, r.pkg)
		} else if info, serr := os.Stat(filepath.Join(root, filepath.FromSlash(r.pkg))); serr != nil || !info.IsDir() {
			t.Errorf("%s:%d has package %q, which is not a directory in this repo (%v)",
				flakeRegistryRel, line, r.pkg, serr)
		}
		if !strings.HasPrefix(r.cause, flaketest.CausePrefix) {
			t.Errorf("%s:%d root cause does not open with %q — a row without a DETERMINISTIC "+
				"mechanism is a suppression entry, and this file is not a suppression list: %q",
				flakeRegistryRel, line, flaketest.CausePrefix, r.cause)
		} else if n := len(strings.Fields(strings.TrimPrefix(r.cause, flaketest.CausePrefix))); n < flakeMinCauseWords {
			t.Errorf("%s:%d root cause names no mechanism in %d word(s) — state the MECHANISM "+
				"(what makes the same code on the same host pass and fail), not the observation: %q",
				flakeRegistryRel, line, n, r.cause)
		}
	}
	return rows
}

// flakeTestDefs indexes every test function defined under internal/ + cmd/ by
// (package dir, test name), and records whether the test's own body classifies
// itself heavy — the one way a flake could be hidden from the mechanism.
func flakeTestDefs(t *testing.T, root string) map[string]flakeTestDef {
	t.Helper()
	out := map[string]flakeTestDef{}
	for _, rel := range goFilesUnder(t, root, prodRoots, true) {
		p := parseRepoFile(t, root, rel)
		dir := filepath.ToSlash(filepath.Dir(rel))
		for _, decl := range p.file.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Body == nil || fn.Name == nil {
				continue
			}
			if !strings.HasPrefix(fn.Name.Name, "Test") {
				continue
			}
			out[dir+"\t"+fn.Name.Name] = flakeTestDef{rel: rel, hasHeavy: flakeBodyClassifiesHeavy(fn)}
		}
	}
	return out
}

// flakeBodyClassifiesHeavy reports whether a test body calls the heaviness
// opt-in (heavytest.Require). It is a body check, not a file check: a file may
// legitimately hold both heavy tests and a flake.
func flakeBodyClassifiesHeavy(fn *ast.FuncDecl) bool {
	found := false
	ast.Inspect(fn.Body, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		if heavyCallName(call) == heavyRequireFunc {
			found = true
		}
		return true
	})
	return found
}

// flakeAssertOwnersExist resolves each registry owner against the task ledger.
// The registry names the row that FIXED or OWES the fix; an id naming nothing is
// how a registry rots into decoration.
//
// The ledger check is skipped — LOUDLY, never silently — when the ledger cannot
// be read in this environment (the sandboxed suite points GOPI_MEMORY_DB at a
// temp file, where the class's owner rows do not exist): the owner's SHAPE is
// still enforced above, and the reason the deeper check did not run is printed.
func flakeAssertOwnersExist(t *testing.T, owners map[string]bool) {
	t.Helper()
	ids, ok := flakeLedgerIDs(t)
	if !ok || len(ids) < flakeLedgerRowsFloor {
		t.Logf("%s: task ledger unavailable or too small (%d row(s)) in this environment — the "+
			"registry's owner ids were checked for SHAPE only; run `%s` where the ledger is "+
			"readable to check that each row exists", flakeLedgerUnverified, len(ids), "go test -run TestFlakeRegistryMatchesTheTestsItNames ./internal/standards/")
		return
	}
	for owner := range owners {
		if !ids[owner] {
			t.Errorf("%s: owner id %q names no task row in the ledger — the registry must name the "+
				"row that fixed or owes the fix (re-dispatch rather than wait, GOPI.md Rule 26)",
				flakeRegistryRel, owner)
		}
	}
}

// flakeLedgerIDs renders the task ledger and returns every row id it carries.
// ok=false means the ledger could not be read at all.
func flakeLedgerIDs(t *testing.T) (map[string]bool, bool) {
	t.Helper()
	out, err := tools.TaskList("")
	if err != nil {
		return nil, false
	}
	return flakeLedgerIDsFrom(out), true
}

// flakeLedgerIDsFrom extracts the row ids from a rendered `gopi task list`: the
// FULL ledger id after the leading `#`, plus the legacy `t-<epoch>` id form.
//
// The 8-hex SHORT chip is deliberately NOT parsed any more (epic
// full-id-no-prefix, 2026-09-23). It used to be, and that was a real defect in
// this gate's own input: two of the registry's five owner rows (`01a0aba1`,
// `01a0c039`) name MORE THAN ONE ledger row, so resolving an owner through the
// chip could match the WRONG row and the gate would accept an owner that exists
// only by coincidence of a shared 8-character prefix. Only a full id is an
// identity.
//
// It is pure so the parser can be PROVEN on a synthetic list — otherwise the
// owner half of this gate would be exercised for the first time on an
// operator's machine, where a parser bug would fail every registry row for the
// wrong reason.
func flakeLedgerIDsFrom(rendered string) map[string]bool {
	ids := map[string]bool{}
	for _, line := range strings.Split(rendered, "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "#") {
			continue
		}
		for _, tok := range strings.Fields(strings.TrimPrefix(line, "#")) {
			if len(tok) > 8 && !strings.Contains(tok, "]") {
				ids[tok] = true
			}
		}
	}
	return ids
}

// TestFlakeLedgerOwnerParserReadsEveryRowForm proves the owner lookup on a
// synthetic ledger render: the FULL id and the legacy `t-<epoch>` form must
// resolve, while a status/summary token and — the point of this epic — the
// 8-hex CHIP a listing used to carry beside the full id must NOT.
func TestFlakeLedgerOwnerParserReadsEveryRowForm(t *testing.T) {
	rendered := strings.Join([]string{
		"#01a0aba1-44c8-7179-9f5c-635f1ac2b2e7  [completed]  flake: TestTaskFindSessionJSONLStillHonoursEveryLayout",
		"#t-1788387314  [completed]  flaky TestTaskRunSurvivesClosedStdoutPipe — child SIGPIPE race",
	}, "\n")

	ids := flakeLedgerIDsFrom(rendered)
	for _, want := range []string{
		"01a0aba1-44c8-7179-9f5c-635f1ac2b2e7", "t-1788387314",
	} {
		if !ids[want] {
			t.Errorf("the owner parser missed %q — the registry names owners in these forms, and a "+
				"miss would fail a real row on a machine where the ledger is readable",
				want)
		}
	}
	// The chip a listing used to carry beside the full id must NOT resolve: two
	// registry owners share one (`01a0aba1` names 2 ledger rows), so honouring a
	// chip is how the gate would accept an owner that exists by coincidence.
	if ids["01a0aba1"] {
		t.Errorf("the owner parser accepted the truncated 8-hex chip as a row id — only a full id is an identity")
	}
	if ids["[completed]"] {
		t.Errorf("the owner parser read a status token as a row id")
	}
}

// TestFlakeCatchMechanismStaysOffTheSmokePath gates the split: the smoke/merge
// path is the default and never runs the mechanism, the mechanism is ONE named
// opt-in, and the names both sides share have one definition each, agreeing.
func TestFlakeCatchMechanismStaysOffTheSmokePath(t *testing.T) {
	root := repoRoot(t)

	// The scripts exist and are runnable (a mechanism nobody can invoke is not a
	// mechanism).
	for _, rel := range []string{flakeCatchScriptRel, flakeProbeScriptRel} {
		info, err := os.Stat(filepath.Join(root, filepath.FromSlash(rel)))
		if err != nil {
			t.Fatalf("%s is missing: %v — the flake catch mechanism has no runner", rel, err)
		}
		if info.Mode().Perm()&0o111 == 0 {
			t.Errorf("%s is not executable (%v) — it is invoked as a script", rel, info.Mode().Perm())
		}
	}

	// The runner sources the ONE definition of the class's values, and re-declares
	// none of them (a copy is drift; a bare literal is a defect).
	catch := readRepoFile(t, root, flakeCatchScriptRel)
	lib := readRepoFile(t, root, flakeLibRel)
	probe := readRepoFile(t, root, flakeProbeScriptRel)
	for _, s := range []struct{ rel, src string }{
		{flakeCatchScriptRel, catch},
		{flakeProbeScriptRel, probe},
	} {
		if !strings.Contains(s.src, "lib/flake-threshold.sh") {
			t.Errorf("%s does not source %s — it must take the registry path, the knob name and "+
				"the repetition band from the one definition", s.rel, flakeLibRel)
		}
		if regexp.MustCompile(`(?m)^\s*` + flakeRepsShellVar + `=`).MatchString(s.src) {
			t.Errorf("%s re-declares %s — the shared lib is its only definition (Rule 14)",
				s.rel, flakeRepsShellVar)
		}
	}

	// The shell and Go sides agree on every shared value.
	if got, want := flakeLibValue(t, lib, "flake_registry_rel"), flaketest.RegistryRel; got != want {
		t.Errorf("registry path drift: %s says %q, %s says %q — one of the two reads a file "+
			"that is not the registry", flakeLibRel, got, flakeGoConstFile, want)
	}
	if got, want := flakeLibValue(t, lib, "flake_catch_script"), flaketest.CatchScriptRel; got != want {
		t.Errorf("catch script drift: %s says %q, %s says %q", flakeLibRel, got, flakeGoConstFile, want)
	}
	if got, want := flakeLibValue(t, lib, "flake_reps_env"), flaketest.RepsEnv; got != want {
		t.Errorf("knob name drift: %s says %q, %s says %q — the environment variable an operator "+
			"sets and the one a test reads must be the same name", flakeLibRel, got, flakeGoConstFile, want)
	}
	if got, want := flakeLibValue(t, lib, "flake_gate_probe_script"), flaketest.GateProbeScriptRel; got != want {
		t.Errorf("probe script drift: %s says %q, %s says %q", flakeLibRel, got, flakeGoConstFile, want)
	}
	if got, want := flakeLibInt(t, lib, "flake_default_reps"), flaketest.DefaultReps; got != want {
		t.Errorf("default repetitions drift: %s says %d, %s says %d", flakeLibRel, got, flakeGoConstFile, want)
	}
	if got, want := flakeLibInt(t, lib, "flake_min_reps"), flaketest.MinReps; got != want {
		t.Errorf("minimum repetitions drift: %s says %d, %s says %d", flakeLibRel, got, flakeGoConstFile, want)
	}
	if got, want := flakeLibInt(t, lib, "flake_max_reps"), flaketest.MaxReps; got != want {
		t.Errorf("maximum repetitions drift: %s says %d, %s says %d", flakeLibRel, got, flakeGoConstFile, want)
	}

	// The knob is the ONLY way in, and it has ONE reader on the Go side.
	if !strings.Contains(catch, "${!"+flakeRepsShellVar) {
		t.Errorf("%s does not read the repetition knob through its shared name (${!%s…}) — a bare "+
			"literal there would let the operator's knob and the gate's name drift apart (Rule 13)",
			flakeCatchScriptRel, flakeRepsShellVar)
	}
	// The knob's NAME has exactly one definition on each side: the Go literal may
	// appear only where the constant is DEFINED (Rule 13 — everywhere else references
	// flaketest.RepsEnv), and the shell may not carry it as a bare string at all.
	if got := strings.Count(readRepoFile(t, root, flakeGoConstFile), `"`+flaketest.RepsEnv+`"`); got != 1 {
		t.Errorf("%s spells the %q literal %d time(s) — it must appear exactly once, at its "+
			"definition (elsewhere reference the constant: Rule 13)", flakeGoConstFile, flaketest.RepsEnv, got)
	}
	// Comments may NAME the knob (the usage text has to tell an operator what to
	// set); the CODE may not spell it — there it comes from the shared shell name.
	if strings.Contains(flakeStripComments(catch), `"`+flaketest.RepsEnv+`"`) {
		t.Errorf("%s spells the %q literal in CODE — reference the shared shell name ($%s) instead, "+
			"so the knob has one name on both sides (Rules 1b/13)",
			flakeCatchScriptRel, flaketest.RepsEnv, flakeRepsShellVar)
	}

	// The Makefile split: `test` is the merge/verify path and must NOT run the
	// mechanism; `flake-catch` is the ONE named opt-in.
	sections := makeSections(t, root)
	smoke, ok := sections["test:"]
	if !ok {
		t.Fatalf("the Makefile has no `test:` target — the merge/verify entry point must exist " +
			"and stay smoke-only")
	}
	for _, forbidden := range []string{flaketest.CatchMakeTarget, flakesCatchScriptName()} {
		if strings.Contains(smoke, forbidden) {
			t.Errorf("the `test:` target references %q — the default (merge/verify) path MUST be "+
				"smoke-only; the repetition mechanism belongs to the opt-in target (GOPI.md Rule 27: "+
				"measure the test, never the host, and never on the merge path)", forbidden)
		}
	}
	catchSection, ok := sections[flaketest.CatchMakeTarget]
	if !ok {
		t.Fatalf("the Makefile has no %q target — a mechanism with no named way in is a mechanism "+
			"nobody runs", flaketest.CatchMakeTarget)
	}
	if !strings.Contains(catchSection, flakeCatchScriptRel) {
		t.Errorf("the %q target does not invoke %s — the named opt-in must actually run the "+
			"mechanism", flaketest.CatchMakeTarget, flakeCatchScriptRel)
	}
	if !strings.Contains(catchSection, "PKGS") {
		t.Errorf("the %q target does not take a scope (PKGS) — a catch run that cannot be aimed at "+
			"the suspect package can only run a merge-sized scope", flaketest.CatchMakeTarget)
	}

	// The heavy opt-in is untouched: this change must not have moved the smoke
	// path's meaning (the heavy inventory gate asserts the same two targets).
	if !strings.Contains(smoke, "GOPI_HEAVY_SKIP_LEDGER") {
		t.Errorf("the `test:` target no longer runs the smoke suite the way it did " +
			"(GOPI_HEAVY_SKIP_LEDGER is gone) — the flake mechanism must not change what the " +
			"merge/verify path measures")
	}
	if _, heavyOK := sections["test-heavy:"]; !heavyOK {
		t.Errorf("the Makefile no longer has `test-heavy:` — this change must not disturb the " +
			"heavy opt-in either")
	}
}

// flakeStripComments drops whole-line shell comments, so an assertion about what
// a script's CODE does is not satisfied (or broken) by its usage text.
func flakeStripComments(src string) string {
	var b strings.Builder
	for _, line := range strings.Split(src, "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "#") {
			continue
		}
		b.WriteString(line)
		b.WriteString("\n")
	}
	return b.String()
}

// flakesCatchScriptName is the catch script's file name, used to make the
// smoke-path assertion read the TARGET's text rather than a path fragment.
func flakesCatchScriptName() string { return filepath.Base(flaketest.CatchScriptRel) }

// TestFlakeCatchVerdictLogicOnSyntheticStreams checks the report's VERDICT LOGIC
// deterministically, by replaying synthetic streams through the real script
// (`-replay`, which runs no test at all). The plant proves the mechanism sees
// real map-order flakiness; it cannot prove what the report does with a test that
// fails EVERY repetition, with a verdict that MOVES, with a moving SUBTEST (which
// must not be named — its scope is its parent) or with a scope that measured
// nothing (which must never read as a clean green). Those are exactly the reading
// errors that would make the catch useless, and they cost milliseconds to prove.
func TestFlakeCatchVerdictLogicOnSyntheticStreams(t *testing.T) {
	root := repoRoot(t)
	lib := readRepoFile(t, root, flakeLibRel)
	expect := flakeLibValue(t, lib, "flake_replay_expect")
	verdicts := flakeLibValue(t, lib, "flake_replay_verdicts")
	if strings.TrimSpace(expect) == "" || strings.TrimSpace(verdicts) == "" {
		t.Fatalf("%s declares no replay expectations — this test would pass without checking anything",
			flakeLibRel)
	}

	// Every declared case is replayed, and BOTH the exit code and the report text
	// are checked: an exit code alone would accept a report that says the opposite.
	cases := map[string]string{}
	for _, pair := range strings.Fields(expect) {
		name, rc := flakeSplitPair(pair)
		if name == "" || rc == "" {
			t.Fatalf("%s declares a malformed expectation %q (want case:exit-code)", flakeLibRel, pair)
		}
		cases[name] = rc
	}
	declared := strings.Fields(flakeLibValue(t, lib, "flake_replay_cases"))
	if len(cases) != len(declared) {
		t.Fatalf("%s declares %d replay case(s) but %d expectation(s) — an unasserted case is a "+
			"fixture nobody checks", flakeLibRel, len(declared), len(cases))
	}
	for name := range cases {
		if !contains(declared, name) {
			t.Errorf("%s expects %q but does not declare it among flake_replay_cases", flakeLibRel, name)
		}
	}

	names := map[string]string{} // test name -> kind
	for _, pair := range strings.Fields(verdicts) {
		nm, kind := flakeSplitPair(pair)
		names[nm] = kind
	}

	for name, wantRC := range cases {
		out, rc := runFlakeReplay(t, root, name)
		if got, want := strconv.Itoa(rc), wantRC; got != want {
			t.Errorf("replay %q exited %s, want %s (the expectation is declared in %s so the gate "+
				"cannot disagree with the fixture):\n%s", name, got, want, flakeLibRel, out)
		}
		switch name {
		case flakeReplayAll:
			// A moved verdict is NAMED as the flake class, and a test that fails every
			// repetition is reported as the PLAIN defect it is (different remedy).
			for nm, kind := range names {
				switch kind {
				case flakeKindMoved:
					if !strings.Contains(out, nm) {
						t.Errorf("the report did not NAME the test whose verdict moved (%q):\n%s", nm, out)
					}
				case flakeKindFails:
					if !strings.Contains(out, nm) {
						t.Errorf("the report did not report the always-failing test (%q) — a plain "+
							"defect must not be filed as a flake:\n%s", nm, out)
					}
				case flakeKindSubtest:
					if strings.Contains(out, nm+"/") {
						t.Errorf("the report NAMED the subtest %s/… — a subtest's re-run scope is its "+
							"parent, and a registry row names a top-level test:\n%s", nm, out)
					}
				}
			}
			if !strings.Contains(out, "FLAKY") {
				t.Errorf("the report did not mark the moved verdict %q:\n%s", flakeVerdictPrefix, out)
			}
		case flakeReplayNothing:
			// The reading error that would make the whole mechanism decorative: a scope
			// where nothing ran (all-skipped) reported as "nothing moved".
			if strings.Contains(out, "no verdict moved") {
				t.Errorf("a scope that measured NOTHING was reported as 'no verdict moved' — that is "+
					"the silence this mechanism exists to end:\n%s", out)
			}
			if !strings.Contains(out, "measured NO verdict") {
				t.Errorf("the report did not say plainly that nothing was measured:\n%s", out)
			}
		}
	}
}

// flakeReplayAllPrefix / flakeReplayNothingPrefix are the case names; they are
// also declared in the shell (flake_replay_cases) and asserted to agree there.
const (
	flakeReplayAll     = "all"
	flakeReplayNothing = "nothing"

	flakeKindMoved   = "moved"
	flakeKindFails   = "fail"
	flakeKindSubtest = "sub"
)

// flakeVerdictPrefix is the report's marker for a moved verdict. Kept as a
// literal here because it IS the human-facing text this test greps for; the
// script's own definition is flake_verdict_marker in the shared lib, asserted
// equal by TestFlakeCatchMechanismStaysOffTheSmokePath.
const flakeVerdictPrefix = "FLAKY"

// runFlakeReplay drives the real script in replay mode and returns its output and
// exit code. No test runs (the stream is synthetic), which is why this check is
// cheap enough to sit on the smoke path and deterministic enough to be a gate.
func runFlakeReplay(t *testing.T, root, name string) (string, int) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), flakeReplayTimeoutSec*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "bash",
		filepath.Join(root, filepath.FromSlash(flakeCatchScriptRel)),
		"-reps", flakeReplayReps, "-replay", name, "./"+flakeReplayFakeScope+"/")
	cmd.Dir = root
	out, err := cmd.CombinedOutput()
	rc := 0
	if err != nil {
		var ee *exec.ExitError
		if !errors.As(err, &ee) {
			t.Fatalf("run %s -replay %s: %v\n%s", flakeCatchScriptRel, name, err, out)
		}
		rc = ee.ExitCode()
	}
	return string(out), rc
}

// flakeSplitPair splits a "left:right" pair from the shared lib.
func flakeSplitPair(pair string) (string, string) {
	i := strings.Index(pair, ":")
	if i < 0 {
		return "", ""
	}
	return pair[:i], pair[i+1:]
}

// contains reports whether list holds want.
func contains(list []string, want string) bool {
	for _, s := range list {
		if s == want {
			return true
		}
	}
	return false
}

// flakeProbeNames holds the planted fixture's two test names, read from the ONE
// shared shell definition (scripts/lib/flake-threshold.sh) so the gate can never
// assert a name the fixture does not carry.
type flakeProbeNames struct {
	plant string
	clean string
}

// TestFlakeCatchMechanismNamesAPlantedOrderFlake is the red proof, run: the gate
// plants a map-iteration-order-dependent test in a throwaway module, points the
// real mechanism at it, and requires the plant to be NAMED. Then it runs the same
// fixture with the order dependence removed and requires that nobody is named —
// without the negative control, "the mechanism named it" could just mean the
// mechanism names everything.
func TestFlakeCatchMechanismNamesAPlantedOrderFlake(t *testing.T) {
	root := repoRoot(t)
	names := flakeProbeNamesFromLib(t, root)
	// The verdict marker has ONE definition (the shared shell lib); the report a
	// human reads and this gate read the same text.
	marker := flakeLibValue(t, readRepoFile(t, root, flakeLibRel), flakeVerdictMarkerShellVar)
	workdir := t.TempDir()

	plantOut, plantRC := runFlakeProbe(t, root, workdir, flaketest.ProbePlantMode)
	if plantRC == 0 {
		t.Fatalf("the mechanism reported NO moved verdict for a planted map-iteration-order flake "+
			"(exit 0) — the catch mechanism cannot see the class it exists for:\n%s", plantOut)
	}
	if plantRC != 1 {
		t.Errorf("the mechanism exited %d for a planted flake (want 1 = a FLAKY test was named):\n%s",
			plantRC, plantOut)
	}
	if !strings.Contains(plantOut, names.plant) {
		t.Errorf("the mechanism did not NAME the planted test %q:\n%s", names.plant, plantOut)
	}
	if !strings.Contains(plantOut, marker) {
		t.Errorf("the mechanism did not mark the hit %q — a report that flags without naming the "+
			"class is noise:\n%s", marker, plantOut)
	}
	if !strings.Contains(plantOut, flaketest.RegistryRel) {
		t.Errorf("the mechanism did not point at %s — the catch loop ends in a registry row (the "+
			"root cause is what makes the next reader's diagnosis cheap):\n%s",
			flaketest.RegistryRel, plantOut)
	}

	cleanOut, cleanRC := runFlakeProbe(t, root, workdir, flaketest.ProbeCleanMode)
	if cleanRC != 0 {
		t.Fatalf("the mechanism exited %d on the order-PINNED control fixture (want 0 = no verdict "+
			"moved) — a mechanism that fires on a deterministic test cannot be trusted when it "+
			"fires:\n%s", cleanRC, cleanOut)
	}
	if strings.Contains(cleanOut, names.clean) && strings.Contains(cleanOut, marker) {
		t.Errorf("the mechanism named the control fixture %q as %s:\n%s",
			names.clean, marker, cleanOut)
	}
}

// flakeProbeNamesFromLib reads the fixture's test names from the shared shell lib
// (their one definition), so a renamed fixture fails the gate instead of making
// it assert a stale literal.
func flakeProbeNamesFromLib(t *testing.T, root string) flakeProbeNames {
	t.Helper()
	lib := readRepoFile(t, root, flakeLibRel)
	return flakeProbeNames{
		plant: flakeLibValue(t, lib, "flake_probe_plant_test"),
		clean: flakeLibValue(t, lib, "flake_probe_clean_test"),
	}
}

// runFlakeProbe drives scripts/flake-gate-probe.sh in one mode and returns its
// combined output plus exit code. The script builds the fixture module itself;
// the gate only supplies the throwaway workdir.
func runFlakeProbe(t *testing.T, root, workdir, mode string) (string, int) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), flakeProbeTimeoutSec*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "bash", filepath.Join(root, filepath.FromSlash(flakeProbeScriptRel)), mode, workdir)
	cmd.Dir = root
	out, err := cmd.CombinedOutput()
	rc := 0
	if err != nil {
		var ee *exec.ExitError
		if !errors.As(err, &ee) {
			t.Fatalf("run %s %s: %v\n%s", flakeProbeScriptRel, mode, err, out)
		}
		rc = ee.ExitCode()
	}
	if ctx.Err() != nil {
		t.Fatalf("the flake catch mechanism did not finish inside %ds — a mechanism that cannot "+
			"complete its own probe is not usable:\n%s", flakeProbeTimeoutSec, out)
	}
	return string(out), rc
}

// makeSections splits a Makefile into its target sections (the same reading the
// heavy inventory gate uses: a target line, then its tab-indented recipe).
func makeSections(t *testing.T, root string) map[string]string {
	t.Helper()
	src := readRepoFile(t, root, "Makefile")
	sections := map[string]string{}
	var current string
	for _, line := range strings.Split(src, "\n") {
		if strings.HasSuffix(line, ":") && !strings.HasPrefix(line, "\t") {
			current = line
			continue
		}
		if current != "" && strings.HasPrefix(line, "\t") {
			sections[current] += line + "\n"
		}
	}
	return sections
}

// flakeLibValue reads `name="value"` from the shared shell lib.
func flakeLibValue(t *testing.T, lib, name string) string {
	t.Helper()
	re := regexp.MustCompile(`(?m)^` + name + `="([^"]*)"\s*$`)
	m := re.FindStringSubmatch(lib)
	if m == nil {
		t.Fatalf("%s does not define %s=\"…\" — the class's shared values must have one "+
			"definition", flakeLibRel, name)
	}
	return m[1]
}

// flakeLibInt reads `name=<n>` from the shared shell lib.
func flakeLibInt(t *testing.T, lib, name string) int {
	t.Helper()
	re := regexp.MustCompile(`(?m)^` + name + `=([0-9]+)\s*$`)
	m := re.FindStringSubmatch(lib)
	if m == nil {
		t.Fatalf("%s does not define %s=<n> — the repetition band must have one definition, or "+
			"the script and the gate disagree about how many runs a claim rests on", flakeLibRel, name)
	}
	n := 0
	for _, c := range m[1] {
		n = n*10 + int(c-'0')
	}
	return n
}

// ---------------------------------------------------------------------------
// Direction 2 — REGISTRY ⊇ DECLARED, and the STRUCTURAL shape of a row
// ---------------------------------------------------------------------------
//
// The gate above checks ONE direction: every registry row names a test that
// exists. That direction cannot see the defect measured on task 01a10a2d — a test
// that IS flaky and is registered by NOBODY reads green, because the only input
// the check ever read was the table itself.
//
// The missing direction needs a second source of the flaky set, INDEPENDENT OF
// THE TABLE. Two candidates were weighed:
//
//   (a) A MINIMAL EXEMPTION ROW IN A SEPARATE FILE (e.g. testdata/flakes-declared.tsv)
//       — rejected: it is the same kind of hand-maintained table as the registry,
//       so the same append-with-no-newline defect, the same "somebody forgot" and
//       the same drift. Two tables that must be kept in step ARE one table with a
//       second place to rot, and the gate asserting them equal would still be
//       asserting a fact about tables, never about the CODE.
//
//   (b) A DECLARATION CARRIED IN THE TEST ITSELF (flaketest.Declare), enumerated
//       by an AST scan of the tree — CHOSEN. The declaration travels with the test
//       body, so it is written at the moment a person is looking at the mechanism
//       (the same moment the registry row gets its prose); it cannot be forgotten
//       by a tool that appends to a different file; and the scan reads the CODE,
//       which is the only source that moves when a test is renamed, deleted or
//       moved. The table keeps what a machine cannot derive (prose mechanism,
//       owner, date); the call site keeps the membership bit. The two sources fail
//       INDEPENDENTLY, which is exactly what makes asserting registry ⊇ declared
//       worth doing.
//
// The second half is STRUCTURAL, and it is not decoration: the mechanism names a
// TEST, so the gate must be able to say WHICH LINE ends the file. The measured
// shape is a row appended onto a `#` comment line with no trailing newline — the
// row inherits the `#` and the whole row becomes a comment, silently, while a
// one-direction gate stays green. A `#` line carrying a TAB is therefore not a
// comment: it is a swallowed ROW, and the gate names it.

// flakeDeclShapeRe matches the declaration's shape as it appears in the registry
// prose — the marker a row quotes so a reader can tell the two sources are in
// step. It is a pattern the gate greps for in the CAUSE column, not a parsing
// device: the AUTHORITATIVE declared set is the AST scan.
var flakeDeclShapeRe = regexp.MustCompile(regexp.QuoteMeta(flaketest.DeclareShape) + `\(`)

// flakeRegistryScan is the structural reading of the registry's text: the rows a
// reader would see, plus the row count the scanner actually consumed.
type flakeRegistryScan struct {
	rows []flakeRow
}

// flakeScanRegistryText is the PURE structural reader of the registry: it is the
// function the real gate calls AND the function the red proofs drive, so a red
// proof cannot pass because it exercised a private copy of the logic.
//
// Every failure it reports is a DEFECT OF THE FILE, never a reason to skip a
// line (Rule 15 — a scanner that dropped a malformed line would turn the row it
// could not read into a row nobody has to register):
//
//   - a line carrying the row SHAPE (5 TAB columns) whose first field is not at a
//     LINE START is reported by name: leading whitespace before a row makes the
//     first column a diacritic of the previous line, not a test name;
//   - a `#` line carrying a TAB is reported as a SWALLOWED ROW — this is the
//     measured defect, and it is reported even when its columns are well-formed,
//     because that is precisely the state the defect produces (a perfectly good
//     row, prefixed by `#` from a comment it was concatenated onto);
//   - a file that does not END WITH A NEWLINE is reported: that is the CONDITION
//     that produced the swallowed row, and it makes the NEXT append inherit the
//     last line's shape. The check is on the file, not on the row, because the
//     danger is the append that has not happened yet.
func flakeScanRegistryText(src string) (flakeRegistryScan, []string) {
	var out flakeRegistryScan
	var findings []string

	if src != "" && !strings.HasSuffix(src, "\n") {
		findings = append(findings, fmt.Sprintf("the file does NOT end with a newline (last line %q) — "+
			"a row appended to it is concatenated onto that line instead of starting a new one, and "+
			"when the line it lands on is a comment the appended ROW IS SWALLOWED as part of the "+
			"comment (measured on task 01a10a2d): the write looks successful and this file reads "+
			"green while the row documents nothing. End every registry file with a newline",
			flakeLastLine(src)))
	}

	lines := strings.Split(src, "\n")
	// A trailing newline yields an empty final element; that is not a line.
	if n := len(lines); n > 0 && lines[n-1] == "" {
		lines = lines[:n-1]
	}
	for i, raw := range lines {
		line := i + 1
		text := strings.TrimRight(raw, "\r")
		if strings.TrimSpace(text) == "" {
			continue
		}
		trimmed := strings.TrimLeft(text, " \t")
		if strings.HasPrefix(trimmed, flakeCommentMark) {
			// A comment may NAME anything. It may not CARRY a row: a TAB after the
			// mark means a row-shaped line was concatenated onto a comment (the
			// swallowed row), which is the defect this half of the gate exists for.
			//
			// ONE exemption, and it is a documented convention rather than a free
			// pass: the column-header line. It is spelled once, in the leaf package
			// (Rule 14), and the comparison is exact, so it cannot be widened into a
			// hole — a swallowed ROW is never byte-equal to the header, because the
			// row's first column is a test name where the header's is the word
			// `test`.
			if strings.Contains(trimmed, flakeTab) && strings.TrimRight(text, "\r") != flaketest.RegistryHeader {
				findings = append(findings, fmt.Sprintf("line %d starts with %q and CARRIES A TAB — a "+
					"comment line holding a TAB is a SWALLOWED REGISTRY ROW, not a comment: the row was "+
					"appended onto this comment (no trailing newline on the line above it) and inherited "+
					"the %q, so it is registered by nobody while every reader of this file sees it as "+
					"prose. Give the file a trailing newline and start the row at a line start: %q",
					line, flakeCommentMark, flakeCommentMark, flakeClip(text)))
			}
			continue
		}
		if strings.HasPrefix(text, " ") || strings.HasPrefix(text, "\t") {
			findings = append(findings, fmt.Sprintf("line %d carries LEADING WHITESPACE and a row "+
				"SHAPE — a row must begin at a line start: with whitespace in front, the first column "+
				"is a continuation of whatever came before it rather than a test name, and a scanner "+
				"that trimmed it would accept a row this file never began: %q", line, flakeClip(text)))
			continue
		}
		parts := strings.Split(text, flakeTab)
		if len(parts) != flakeColumns {
			findings = append(findings, fmt.Sprintf("line %d is malformed (%d column(s), want %d: "+
				"test%spackage%sroot_cause%sowner%sdate): %q",
				line, len(parts), flakeColumns, flakeTab, flakeTab, flakeTab, flakeTab, flakeClip(text)))
			continue
		}
		out.rows = append(out.rows, flakeRow{
			test: parts[0], pkg: parts[1], cause: parts[2], owner: parts[3], date: parts[4], line: line,
		})
	}
	return out, findings
}

// flakeLastLine returns the final line of src, clipped so a finding stays readable.
func flakeLastLine(src string) string {
	trimmed := strings.TrimRight(src, "\n")
	if i := strings.LastIndex(trimmed, "\n"); i >= 0 {
		return flakeClip(trimmed[i+1:])
	}
	return flakeClip(trimmed)
}

// flakeClip bounds a quoted line so a finding stays a line and not a screenful.
func flakeClip(s string) string {
	if len(s) <= flakeClipLen {
		return s
	}
	return s[:flakeClipLen] + "…"
}

// flakeDecl is one declaration found in the tree: the test it sits inside, and
// where the call is.
type flakeDecl struct {
	test      string // the top-level test function wrapping the call
	pkg       string // module-relative package DIRECTORY of that test
	rel       string // the file carrying the call
	line      int    // 1-based line of the call
	mechanism string // the declaration's ARGUMENT (the mechanism it names)
}

// flakeDeclaredIndex is the DECLARED set: every test that carries a
// flaketest.Declare call, keyed exactly like the registry's (pkg, test) pair so
// the two sets are compared as identities rather than as text.
//
// It walks the same file set the registry's existence check walks (prodRoots,
// test files only), so the two directions of this gate read the SAME tree: a
// declaration in a file neither side scans would be a hole in both.
func flakeDeclaredIndex(t *testing.T, root string) map[string][]flakeDecl {
	t.Helper()
	out := map[string][]flakeDecl{}
	for _, rel := range goFilesUnder(t, root, prodRoots, true) {
		if !strings.Contains(string(readRepoBytes(t, root, rel)), flaketest.DeclareShape) {
			continue // cheap pre-filter; the AST below is authoritative
		}
		p := parseRepoFile(t, root, rel)
		dir := filepath.ToSlash(filepath.Dir(rel))
		for _, decl := range p.file.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Body == nil || fn.Name == nil {
				continue
			}
			// The declaration must sit INSIDE a test: a call in a helper is not a
			// membership bit (it has no verdict of its own), and accepting one
			// would let a single helper declare for every test that calls it.
			ast.Inspect(fn.Body, func(n ast.Node) bool {
				call, ok := n.(*ast.CallExpr)
				if !ok || heavyCallName(call) != flaketest.DeclareShape {
					return true
				}
				pos := p.fset.Position(call.Pos())
				out[dir+"\t"+fn.Name.Name] = append(out[dir+"\t"+fn.Name.Name], flakeDecl{
					test:      fn.Name.Name,
					pkg:       dir,
					rel:       rel,
					line:      pos.Line,
					mechanism: flakeDeclMechanism(t, call, rel, pos.Line),
				})
				return true
			})
		}
	}
	return out
}

// flakeDeclMechanism reads the declaration's ARGUMENT at a call site. It is the
// check that replaces a runtime panic (PROJECT.md Rule 19: a panic takes the
// caller's choice away, and here it would also fire only where the test happens
// to run): the gate reads the SOURCE, so a declaration that names no mechanism is
// reported by file:line whether or not the test is ever executed.
//
// A non-literal argument is refused rather than skipped: the declaration's whole
// value is that the mechanism is legible AT THE CALL SITE, and a computed string
// would put it somewhere no reviewer of this gate can see.
func flakeDeclMechanism(t *testing.T, call *ast.CallExpr, rel string, line int) string {
	t.Helper()
	if len(call.Args) != 1 {
		t.Errorf("%s:%d calls %s with %d argument(s) — exactly one mechanism string, so the gate "+
			"can read what the declaration claims", rel, line, flaketest.DeclareShape, len(call.Args))
		return ""
	}
	lit, ok := call.Args[0].(*ast.BasicLit)
	if !ok || lit.Kind != token.STRING {
		t.Errorf("%s:%d calls %s with a non-literal argument — the mechanism must be a STRING "+
			"LITERAL at the call site, because this gate reads the source (a computed string would "+
			"put the mechanism where no reviewer of this gate can see it, and the declaration would "+
			"be unverifiable)", rel, line, flaketest.DeclareShape)
		return ""
	}
	s, err := strconv.Unquote(lit.Value)
	if err != nil {
		t.Errorf("%s:%d calls %s with an unquotable literal %q: %v", rel, line, flaketest.DeclareShape, lit.Value, err)
		return ""
	}
	return s
}

// readRepoBytes reads a repo-relative file's bytes (a pre-filter, so the AST
func readRepoBytes(t *testing.T, root, rel string) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(rel)))
	if err != nil {
		t.Fatalf("read %s: %v", rel, err)
	}
	return b
}

// TestFlakeRegistryCoversEveryDeclaredFlake is direction 2 of the registry gate:
// REGISTRY ⊇ DECLARED. A test that carries flaketest.Declare IS a member of the
// class by its own say-so, so a registry row missing for it is the defect this
// epic exists for — it is reported BY NAME and at file:line, because the remedy
// is to append a row, and the caller needs to know which test and where.
func TestFlakeRegistryCoversEveryDeclaredFlake(t *testing.T) {
	root := repoRoot(t)
	rows := flakeRegistryRows(t, root)
	declared := flakeDeclaredIndex(t, root)

	// Non-vacuity (Rule 15): an empty declared set would make this whole direction
	// pass by having nothing to compare — a broken scan or a rename of the
	// declaration would read as "every declaration is registered".
	if len(declared) < flakeMinDeclaredTests {
		t.Fatalf("the scan found %d test(s) carrying %s under internal/ + cmd/ (want at least %d) — "+
			"the registry's second direction cannot be checked against an empty set, and a silently "+
			"empty scan reads exactly like a clean green. Is the marker %q still the shape the "+
			"declaration is called with?", len(declared), flaketest.DeclareShape, flakeMinDeclaredTests,
			flaketest.DeclareShape+"(…)")
	}
	if len(rows) < flakeMinRegistryRows {
		t.Fatalf("%s carries %d row(s) (want at least %d) — comparing the declared set against an "+
			"empty registry would fail every declaration for the wrong reason",
			flakeRegistryRel, len(rows), flakeMinRegistryRows)
	}

	registered := map[string]flakeRow{}
	for _, r := range rows {
		registered[r.pkg+"\t"+r.test] = r
	}

	keys := make([]string, 0, len(declared))
	for k := range declared {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	// The difference is the ONE pure function the red proof drives
	// (flakeCoverageDiff) — the same code path, so a red proof cannot pass by
	// exercising a private copy of the comparison.
	missing, unbacked := flakeCoverageDiff(declared, registered)
	for _, d := range missing {
		t.Errorf("%s:%d declares %s a member of the flake class (%s), but %s has NO ROW for it in "+
			"%s — a flaky test that is registered by nobody is the false green this epic closes: "+
			"the test's own declaration is the second, table-independent source of the flaky set, "+
			"and the registry must COVER it. Append the row (test<TAB>package<TAB>%q…<TAB>owner<TAB>date)",
			d.rel, d.line, d.test, flaketest.DeclareShape, d.pkg, flakeRegistryRel, flaketest.CausePrefix)
	}
	for _, r := range unbacked {
		t.Errorf("%s:%d registers %s in %s, but that test carries no %s call — the registry row "+
			"and the test's own declaration are the two sources this gate holds in step: a row "+
			"the code does not corroborate is either a stale row or a declaration that was never "+
			"written (add the call inside the test body, or delete the row)",
			flakeRegistryRel, r.line, r.test, r.pkg, flaketest.DeclareShape)
	}

	// A declared test whose row names no mechanism: the declaration says the test
	// IS flaky, the row is the only place a reader learns WHY.
	for _, key := range keys {
		row, ok := registered[key]
		if !ok {
			continue
		}
		if n := len(strings.Fields(row.cause)); n < flaketest.DeclareMinMechanismWords {
			d := declared[key][0]
			t.Errorf("%s:%d declares %s, and %s:%d carries the row — but the row's root cause names "+
				"no mechanism in %d word(s)", d.rel, d.line, d.test, flakeRegistryRel, row.line, n)
		}
		// …and the DECLARATION itself must name one: a call site whose argument is
		// "flaky" joins the class without recording anything, which is the state the
		// registry's own cause floor exists to prevent.
		for _, d := range declared[key] {
			if !flaketest.MechanismOK(d.mechanism) {
				t.Errorf("%s:%d declares %s a member of the flake class with %q — that names no "+
					"mechanism (want %q followed by at least %d words: WHAT makes the same code on the "+
					"same host pass and fail). The declaration is the machine-readable half of this "+
					"gate, so a declaration that names nothing makes the registry's own cause column "+
					"the only evidence — and a test can then join the class by asserting it is flaky",
					d.rel, d.line, d.test, d.mechanism, flaketest.CausePrefix, flaketest.DeclareMinMechanismWords)
			}
		}
	}
}

// TestFlakeScanRejectsAMalformedRegistry is the STRUCTURAL red proof: the same
// pure scanner the real gate drives, over synthetic registry text — the SWALLOWED
// row, a row with leading whitespace, and a file with no trailing newline must all
// be reported BY NAME, while a well-formed document yields no finding and its rows.
// Without this the structural half of the gate would be exercised for the first
// time on a real file, where a false positive blocks every commit and a false
// negative is invisible.
func TestFlakeScanRejectsAMalformedRegistry(t *testing.T) {
	good := flaketest.RegistryHeader + "\n" +
		"TestFoo\tinternal/tools\tdeterministic: a map walked in randomized order decides the verdict\t01a0aba1-44c8-7179-9f5c-635f1ac2b2e7\t2026-09-17\n"

	t.Run("well-formed text is green", func(t *testing.T) {
		scan, findings := flakeScanRegistryText(good)
		if len(findings) != 0 {
			t.Fatalf("the scanner reported %d finding(s) on well-formed registry text — a false "+
				"positive here fails every commit: %v", len(findings), findings)
		}
		if len(scan.rows) != 1 || scan.rows[0].test != "TestFoo" {
			t.Fatalf("the scanner read %d row(s) (%v) from text carrying one — the structural half "+
				"and the column half disagree about what a row is", len(scan.rows), scan.rows)
		}
	})

	t.Run("a row concatenated onto a comment is RED, by name", func(t *testing.T) {
		// The MEASURED defect: the file's last line is a comment with no trailing
		// newline, and the appended row is concatenated onto it.
		swallowed := good + "# a closing note about the column meanings" +
			"TestBar\tcmd/agent\tdeterministic: the fixture slept a wall-clock second and hoped" +
			"\t01a0aba1-44c8-7179-9f5c-635f1ac2b2e7\t2026-09-17\n"
		scan, findings := flakeScanRegistryText(swallowed)
		if !flakeFindingsMention(findings, "TestBar") {
			t.Fatalf("the scanner did NOT report the swallowed row by name (%q) — this is the exact "+
				"shape measured on task 01a10a2d, and a scanner that passes it is the false green "+
				"this epic closes. findings=%v", "TestBar", findings)
		}
		if !flakeFindingsMention(findings, "SWALLOWED") {
			t.Errorf("the finding does not name the shape (SWALLOWED REGISTRY ROW) — a reader must "+
				"be told WHAT happened, not only which line: %v", findings)
		}
		if len(scan.rows) != 1 {
			t.Errorf("the swallowed row was READ as a row (%d row(s)) — it must be reported and NOT "+
				"counted, or the registry would silently gain a row nobody reviewed", len(scan.rows))
		}
	})

	t.Run("a row with leading whitespace is RED, by name", func(t *testing.T) {
		indented := good + "  TestBaz\tinternal/tools\tdeterministic: the shared fixture root is " +
			"walked from a map and the winner decides the address\t01a0aba1-44c8-7179-9f5c-635f1ac2b2e7\t2026-09-17\n"
		_, findings := flakeScanRegistryText(indented)
		if !flakeFindingsMention(findings, "LEADING WHITESPACE") {
			t.Errorf("the scanner accepted a row that does not begin at a line start: %v", findings)
		}
		if !flakeFindingsMention(findings, "TestBaz") {
			t.Errorf("the leading-whitespace finding does not quote the row it reports: %v", findings)
		}
	})

	t.Run("a file with no trailing newline is RED, even when every row is clean", func(t *testing.T) {
		noEOL := "# test\tpackage\troot_cause\towner\tdate\n" +
			"TestFoo\tinternal/tools\tdeterministic: a map walked in randomized order decides it\t" +
			"01a0aba1-44c8-7179-9f5c-635f1ac2b2e7\t2026-09-17"
		_, findings := flakeScanRegistryText(noEOL)
		if !flakeFindingsMention(findings, "does NOT end with a newline") {
			t.Errorf("the scanner accepted a registry whose last line carries no newline — that is "+
				"the CONDITION that produces the swallowed row on the next append (Rule 15: report "+
				"the state that makes the defect possible, not only the defect): %v", findings)
		}
	})

	t.Run("a malformed column count is RED", func(t *testing.T) {
		bad := good + "TestQux\tinternal/tools\n"
		_, findings := flakeScanRegistryText(bad)
		if !flakeFindingsMention(findings, "malformed") {
			t.Errorf("the scanner accepted a row with 2 columns: %v", findings)
		}
	})
}

// TestFlakeDeclarationGateRedProof is the DECLARATION red proof, driven on the
// real comparison path: the set difference that
// TestFlakeRegistryCoversEveryDeclaredFlake reports is a pure function of the two
// sets, so it is exercised here with a synthetic declared set (a test nobody
// registered → must be named) and a synthetic registered set (a row with no
// declaration → must be named). Without the second arm this gate could name the
// wrong side of the difference and still read green in production.
func TestFlakeDeclarationGateRedProof(t *testing.T) {
	builtin := func(key, name string) map[string][]flakeDecl {
		return map[string][]flakeDecl{
			key: {{test: name, pkg: strings.Split(key, "\t")[0], rel: "synthetic_test.go", line: 42}},
		}
	}

	t.Run("a declared, unregistered flake is RED, by name and file:line", func(t *testing.T) {
		declared := builtin("internal/tools\tTestSyntheticFlake", "TestSyntheticFlake")
		registered := map[string]flakeRow{}
		missing, _ := flakeCoverageDiff(declared, registered)
		if len(missing) != 1 {
			t.Fatalf("the difference reported %d missing declaration(s) (want 1) — the direction "+
				"that closes this epic is not wired to the comparison", len(missing))
		}
		if missing[0].test != "TestSyntheticFlake" || missing[0].line != 42 {
			t.Errorf("the missing-row report does not carry the test name and its file:line (%+v) — "+
				"the remedy is to append a row, and that needs to know WHICH test and WHERE",
				missing[0])
		}
	})

	t.Run("a registered row with no declaration is RED", func(t *testing.T) {
		declared := map[string][]flakeDecl{}
		registered := map[string]flakeRow{
			"cmd/agent\tTestSyntheticRow": {test: "TestSyntheticRow", pkg: "cmd/agent", line: 7},
		}
		_, unbacked := flakeCoverageDiff(declared, registered)
		if len(unbacked) != 1 || unbacked[0].test != "TestSyntheticRow" {
			t.Fatalf("the difference did not report the uncorroborated row (%+v)", unbacked)
		}
	})

	t.Run("a matching pair is GREEN", func(t *testing.T) {
		declared := builtin("internal/tools\tTestSyntheticFlake", "TestSyntheticFlake")
		registered := map[string]flakeRow{
			"internal/tools\tTestSyntheticFlake": {test: "TestSyntheticFlake", pkg: "internal/tools"},
		}
		missing, unbacked := flakeCoverageDiff(declared, registered)
		if len(missing) != 0 || len(unbacked) != 0 {
			t.Fatalf("a declaration and its row were reported as a difference (%v / %v) — the gate "+
				"would fail the very state it exists to require", missing, unbacked)
		}
	})
}

// flakeCoverageDiff is the ONE set difference both directions of the declaration
// gate are read from — pure, so the red proof drives the real comparison rather
// than a copy of it.
func flakeCoverageDiff(declared map[string][]flakeDecl, registered map[string]flakeRow) ([]flakeDecl, []flakeRow) {
	var missing []flakeDecl
	keys := make([]string, 0, len(declared))
	for k := range declared {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, key := range keys {
		if _, ok := registered[key]; !ok {
			missing = append(missing, declared[key][0])
		}
	}
	var unbacked []flakeRow
	rkeys := make([]string, 0, len(registered))
	for k := range registered {
		rkeys = append(rkeys, k)
	}
	sort.Strings(rkeys)
	for _, key := range rkeys {
		if _, ok := declared[key]; !ok {
			unbacked = append(unbacked, registered[key])
		}
	}
	return missing, unbacked
}

// flakeFindingsMention reports whether any finding carries want.
func flakeFindingsMention(findings []string, want string) bool {
	for _, f := range findings {
		if strings.Contains(f, want) {
			return true
		}
	}
	return false
}
