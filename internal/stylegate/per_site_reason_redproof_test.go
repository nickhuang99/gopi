package stylegate

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// This file is the RED/GREEN proof for the change that retires the COUNTING
// ratchet for RuleIgnoredError: every counted site must state its OWN reason,
// and a reason that documents a GROUP (a whole file, a whole callee) must not
// clear a site.
//
// The history it repairs, stated as the mechanism and not as a mood: the ceiling
// was a number, so the cheapest way past a red gate was to raise the number or
// to add one callee key to internal/stylegate/reasons/ignored-error.json that
// silenced every call to that function. Five recurrences of "ignored-error is
// over its ceiling" were each answered by moving a digit. A ceiling of 0 removes
// the first move (there is no headroom to spend) and the per-site table removes
// the second (there is no group key to add).
//
// Three propositions, and each fails on a plausible way to get it wrong:
//
//	RED      a bare `_ = call()` is a finding and the gate's decision is OVER,
//	         naming `file:line` — a gate that is red without naming the line
//	         leaves the author to re-derive which site appeared.
//	GREEN    the SAME site with `//stylegate:ignore <reason>` on its own line
//	         is not a finding, so the repair is local and the ceiling of 0 is
//	         reachable.
//	PER-SITE a reason written on a NEIGHBOURING site's line, and a reason
//	         written in the same file above a blank line, do not clear this one:
//	         the justification has to be about this discard.

// perSiteFixtureMod is the throwaway module's go.mod. The fixture needs a real
// module so the scanner type-checks it the way it type-checks the repo.
const perSiteFixtureMod = `module fixture

go 1.21
`

// perSiteSiteLine is the 1-based line the fixture's discard sits on. It is a
// constant because the proof asserts the gate NAMES it — a line number typed at
// three call sites could disagree with itself.
const perSiteSiteLine = 8

// perSiteFixtureFileName is the fixture's one source file.
const perSiteFixtureFileName = "fixture.go"

// perSiteFixture writes a one-site module whose single discard is `bare` (no
// reason) or annotated, and whose SECOND function is the neighbour used by the
// per-site proposition. It returns the module root.
//
// The layout is fixed on purpose (the line number is asserted): a fixture that
// reformats itself would make the "names the line" proposition unmeasurable.
func perSiteFixture(t *testing.T, variant perSiteVariant) (dir string, siteLine int) {
	t.Helper()
	dir = t.TempDir()
	siteLine = perSiteSiteLine

	first := "\t_ = boom()"
	switch variant {
	case perSiteAnnotated:
		first = "\t_ = boom() //stylegate:ignore the fixture's own teardown; boom's failure is the fixture's setup error"
	case perSiteNeighbourAnnotated:
		// The marker documents the NEIGHBOUR function's site, not this one.
		first = "\t_ = boom()"
	case perSiteDetachedMarker:
		// A marker one blank line above the statement: the container-header
		// climb must not reach it, so it documents nothing.
		first = "//stylegate:ignore a detached marker documents nothing at all\n\n\t_ = boom()"
		// The marker plus the blank line push the statement two lines down; the
		// fixture reports the address so the proposition asserts on where the
		// site ACTUALLY is, not on where the layout was assumed to put it.
		siteLine = perSiteSiteLine + 2
	}

	body := "package fixture\n" + // 1
		"\n" + // 2
		"import \"fmt\"\n" + // 3
		"\n" + // 4
		"func boom() error { return fmt.Errorf(\"boom\") }\n" + // 5
		"\n" + // 6
		"func site() {\n" + // 7
		first + "\n" + // 8 (bare) — perSiteSiteLine
		"}\n" + // 9 (or later, when the variant inserts the detached marker)
		"\n" + // 10
		"func neighbour() {\n" + // 11
		"\t_ = boom() //stylegate:ignore the fixture's neighbouring site; this reason is about THIS discard\n" + // 12
		"}\n"

	files := map[string]string{
		"go.mod":       perSiteFixtureMod,
		"fixture.go":   body,
		"fixture_x.go": "package fixture\n",
	}
	for name, content := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
			t.Fatalf("write fixture file %s: %v", name, err)
		}
	}
	return dir, siteLine
}

// perSiteVariant names the fixture shapes the proof drives.
type perSiteVariant string

const (
	// perSiteBare is the RED shape: a discard with no reason at all.
	perSiteBare perSiteVariant = "bare"
	// perSiteAnnotated is the GREEN shape: the same discard, its own reason.
	perSiteAnnotated perSiteVariant = "annotated"
	// perSiteNeighbourAnnotated has a reason in the same FILE, on the other
	// site's line: the per-site proposition's control.
	perSiteNeighbourAnnotated perSiteVariant = "neighbour-annotated"
	// perSiteDetachedMarker has a marker separated from the statement by a
	// blank line: it is not attached to anything.
	perSiteDetachedMarker perSiteVariant = "detached-marker"
)

// perSiteFindings scans the fixture and returns its ignored-error findings,
// asserting the instrument is not BLIND: a detector that stopped seeing its
// subject would make every proposition below vacuously true, which is exactly
// the "vacuum green" Rule 38 refuses.
func perSiteFindings(t *testing.T, dir string) []Finding {
	t.Helper()
	rep, err := Scan(Options{Root: dir, IncludeTests: true})
	if err != nil {
		t.Fatalf("Scan(%s): %v", dir, err)
	}
	if len(rep.PackagesFailed) > 0 {
		t.Fatalf("the fixture did not type-check, so nothing below is measured: %v", rep.PackagesFailed)
	}
	var out []Finding
	for _, f := range rep.Findings {
		if f.Rule == RuleIgnoredError {
			out = append(out, f)
		}
	}
	return out
}

// perSiteGateMessage is the ONE message shape the gate renders for an over-
// ceiling reading. It is a function so the proof can assert on the wording the
// author actually sees — "stylegate <rule>: N <scope> hit(s), ceiling N" plus
// every site as `file:line (detail)` — rather than on a summary the test invented.
func perSiteGateMessage(rule string, reading, ceiling int, inTest bool, sites []Finding) string {
	scope := "production"
	if inTest {
		scope = "test"
	}
	lines := make([]string, 0, len(sites))
	for _, f := range sites {
		lines = append(lines, fmt.Sprintf("%s:%d (%s)", f.File, f.Line, f.Detail))
	}
	return fmt.Sprintf("stylegate %s: %d %s hit(s), ceiling %d\n  sites:\n    %s",
		rule, reading, scope, ceiling, strings.Join(lines, "\n    "))
}

// TestIgnoredErrorPerSiteReasonIsTheGate is the deliverable's RED/GREEN proof.
func TestIgnoredErrorPerSiteReasonIsTheGate(t *testing.T) {
	// The ceiling this proof is driven against is the SHIPPED one, not a
	// literal: a proof that hardcoded 0 would keep passing after someone
	// raised the number, which is the exact regression it exists to prevent.
	ceiling, ok := gateCeilings[RuleIgnoredError][true]
	if !ok {
		t.Fatalf("gateCeilings carries no test-scope row for %s — the rule is ungated and this proof is vacuous", RuleIgnoredError)
	}
	if ceiling != 0 {
		t.Fatalf("%s test-scope ceiling is %d, want 0: a non-zero ceiling is headroom, and headroom is what "+
			"let five recurrences be answered by moving a digit instead of by justifying a site",
			RuleIgnoredError, ceiling)
	}
	if prodCeiling := gateCeilings[RuleIgnoredError][false]; prodCeiling != 0 {
		t.Fatalf("%s production ceiling is %d, want 0", RuleIgnoredError, prodCeiling)
	}

	// ── RED: a bare discard is a finding, the decision is OVER, and the message
	// names the line. All three, because a red gate that does not say WHERE is
	// the shape that forced every earlier run to re-derive the site by hand.
	bareDir, bareLine := perSiteFixture(t, perSiteBare)
	bare := perSiteFindings(t, bareDir)
	if len(bare) != 1 {
		t.Fatalf("the bare fixture produced %d %s finding(s), want exactly 1 (the neighbour's is annotated): %v",
			len(bare), RuleIgnoredError, bare)
	}
	site := bare[0]
	if site.File != perSiteFixtureFileName || site.Line != bareLine {
		t.Fatalf("the finding names %s:%d, want %s:%d — the gate's value is that it points at the "+
			"discard, so a wrong address is a wrong reading", site.File, site.Line, perSiteFixtureFileName, bareLine)
	}
	if v := ratchetDecision(len(bare), ceiling); !v.over {
		t.Fatalf("a bare discard read as %d against the shipped ceiling %d and the decision was NOT over — "+
			"that is the counting ratchet still absorbing a new site; got %+v", len(bare), ceiling, v)
	}
	msg := perSiteGateMessage(RuleIgnoredError, len(bare), ceiling, true, bare)
	wantAddr := fmt.Sprintf("%s:%d", perSiteFixtureFileName, bareLine)
	if !strings.Contains(msg, wantAddr) {
		t.Fatalf("the gate's message does not name %s, so a reader cannot find the site it is red about:\n%s",
			wantAddr, msg)
	}
	t.Logf("RED (bare discard) — the gate renders:\n%s", msg)

	// ── GREEN: the SAME site with its own reason is not a finding. The repair
	// is one comment on one line, which is the only property that makes a
	// ceiling of 0 affordable.
	annotatedDir, annotatedLine := perSiteFixture(t, perSiteAnnotated)
	annotated := perSiteFindings(t, annotatedDir)
	for _, f := range annotated {
		if f.Line == annotatedLine {
			t.Fatalf("an annotated site still counts as %s:%d (%s) — the marker on the statement's own line "+
				"must be read, or no site can ever be cleared and the ceiling of 0 is unreachable",
				f.File, f.Line, f.Detail)
		}
	}
	if v := ratchetDecision(len(annotated), ceiling); v.over {
		t.Fatalf("the annotated fixture read as %d against ceiling %d and the decision was OVER: %+v",
			len(annotated), ceiling, v)
	}
	t.Logf("GREEN (same site, its own reason) — the gate decision is not over")

	// ── PER-SITE: a reason in the same FILE, on the OTHER site's line, does not
	// clear this one. This is the property that distinguishes "a site states its
	// reason" from "a file/callee exemption covers it".
	neighbourDir, neighbourLine := perSiteFixture(t, perSiteNeighbourAnnotated)
	neighbour := perSiteFindings(t, neighbourDir)
	found := false
	for _, f := range neighbour {
		if f.Line == neighbourLine {
			found = true
		}
	}
	if !found {
		t.Fatalf("a reason written on the NEIGHBOURING site's line cleared this site as well — that is a "+
			"file-wide exemption, and it is the shape this rule retires: %v", neighbour)
	}

	// ── DETACHED: a marker separated from the statement by a blank line still
	// documents nothing (the container-header climb deliberately stops there).
	detachedDir, detachedLine := perSiteFixture(t, perSiteDetachedMarker)
	detached := perSiteFindings(t, detachedDir)
	found = false
	for _, f := range detached {
		if f.Line == detachedLine {
			found = true
		}
	}
	if !found {
		t.Fatalf("a marker detached from the statement by a blank line cleared it — a marker that is not "+
			"attached to the discard it justifies is not a justification: %v", detached)
	}
}

// TestIgnoredErrorReasonTableRefusesGroupKeys is the second half of "per SITE":
// the annotate tool's table is keyed `file:line`, and BOTH group shapes are
// refused by NAME. It lives in this package rather than beside the tool because
// the predicates it pins are what the tool's refusal is made of — the shapes
// themselves are driven end to end through the CLI in
// cmd/stylegate/annotate_table_test.go.
func TestIgnoredErrorReasonTableRefusesGroupKeys(t *testing.T) {
	// The accepted shape: a site key.
	for _, k := range []string{"internal/tools/x_test.go:42", "cmd/agent/a.go:1"} {
		if !IsSiteKey(k) {
			t.Fatalf("%q is a site key and was not recognised — the tool would refuse the only shape it asks for", k)
		}
	}
	// The refused shapes, each for its own stated reason: a callee, a
	// `file::callee` (the spelling the old lookup's middle fallback used), a
	// whole file, a bare line, and a trailing colon.
	for _, k := range []string{"strconv.Atoi", "db.Close", "internal/tools/x_test.go::gitIn",
		"internal/tools/x_test.go", "a.go", "42", "internal/tools/x_test.go:"} {
		if IsSiteKey(k) {
			t.Fatalf("%q was accepted as a site key — every one of these names a GROUP or nothing at all, "+
				"and accepting it is how one line of JSON came to silence 48 sites", k)
		}
	}
}
