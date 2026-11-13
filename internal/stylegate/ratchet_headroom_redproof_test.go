package stylegate

import (
	"fmt"
	"sort"
	"strings"
	"testing"
)

// This file closes the ONE direction of the ratchet that nothing measured: a
// ceiling LEFT ABOVE its reading.
//
// TestStyleGate fails when a reading EXCEEDS its ceiling — drift in. Nothing
// failed when a reading fell BELOW its ceiling, and that is not a cosmetic
// asymmetry: the ceiling table's own prose states the opposite property in
// several places ("the ceiling now equals the reading, so the count may only
// move down from here"), so the table can claim a ratchet it is not holding.
//
// MEASURED, and the reason this arm exists (2026-10-28, on 96a853d): the
// ignored-error entry read `{false: 0, true: 77}` with the sentence "the ceiling
// now equals the reading" directly above it, while the live instrument reported
//
//	go run ./internal/stylegate/cmd/stylegate -root . -rule ignored-error
//	→ ignored-error  74 / TOTAL 74 (all in _test.go)
//
// Three sites of headroom — three silent discards that could arrive without
// moving a number anybody had decided on. The 77 was last true at 88f9b5f;
// 6111467 then repaired four of the sites its own reading named and correctly
// left the ceiling untouched, so the measurement moved and the threshold did not
// follow it. TestStyleGate stayed GREEN through all of that, which is exactly
// what a one-directional ratchet does.
//
// So the two directions are now measured by two arms with two messages: this one
// owns "the ceiling is a LICENCE" (reading < ceiling → tighten it in the same
// commit that removed the site), and TestStyleGate owns "the ceiling is a GATE"
// (reading > ceiling → a regression arrived).

// ratchetDriftTreeCheck is the ONE command a reader runs to tell the two
// measured causes of a drift failure apart: an excess on a DIRTY tree (the
// working-tree arm sees it, the HEAD-worktree arm does not) versus an excess
// that is really in HEAD (both arms must see it, so a disagreement there means
// root resolution moved). Named rather than typed into the message (Rule 1/13).
const ratchetDriftTreeCheck = "git status --porcelain"

// ratchetHeadroomGap is the ONE arithmetic the two arms share: the ceiling minus
// the reading. Positive = unused headroom (a licence), zero = the ratcheted
// state, negative = a reading past its threshold (drift). Kept as a named
// function because the arms assert the DECISION, and an inlined subtraction in a
// test message is a decision nobody can drive with a fixture.
func ratchetHeadroomGap(reading, ceiling int) int {
	return ceiling - reading
}

// ratchetHeadroomDirection names what a non-zero gap MEANS, so a failure message
// says which way to move the number instead of only which number is off (Rule 15:
// a report that only states the value leaves the reader to re-derive the fix).
func ratchetHeadroomDirection(reading, ceiling int) string {
	switch gap := ratchetHeadroomGap(reading, ceiling); {
	case gap > 0:
		return fmt.Sprintf("%d site(s) of UNUSED HEADROOM — the ceiling is %d, the measurement is %d, so %d "+
			"silent discard(s) may arrive without moving a number anyone decided on. Tighten the ceiling to "+
			"the reading in the same change that removed the site (Rule 1: 只能向下棘轮, and a ceiling above "+
			"its measurement is a licence, not history)", gap, ceiling, reading, gap)
	case gap < 0:
		return fmt.Sprintf("the measurement is %d past a ceiling of %d — this is the DRIFT direction and "+
			"TestStyleGate owns it. The two arms CAN disagree, and the difference is MEASURED, not "+
			"mysterious: TestStyleGate scans the WORKING TREE (repoRootFromGateFile walks up to go.mod from "+
			"the package's own working directory), while the merge leg's baseline arm "+
			"(internal/tools, TestStylegateRatchetSuiteIsGreenOnThisTree) runs this very suite inside a "+
			"DETACHED WORKTREE OF HEAD (mgToolchain). So a green there against a red here is the ordinary "+
			"shape of an excess that lives in content git does not have yet — confirm it with `%s`, which "+
			"prints every uncommitted and untracked entry. If that list is EMPTY, the two really are "+
			"reading different trees and root resolution is what to suspect", -gap, ceiling, ratchetDriftTreeCheck)
	default:
		return "the ceiling equals its measurement — the ratcheted state"
	}
}

// ratchetHeadroomMiss is one ceiling entry whose value differs from its reading.
type ratchetHeadroomMiss struct {
	Rule    string
	InTest  bool
	Reading int
	Ceiling int
	Gap     int
}

// ratchetHeadroomMisses is the predicate: every (rule, scope) entry whose
// ceiling is not exactly its reading, sorted so a failure list is stable and a
// reader can diff two runs.
//
// It is a pure function of the two tables (ceilings, counts) so the arms below
// can drive it with fixtures — a control that must stay silent and a red that
// must name the row. A predicate reachable only through the live scan could not
// be shown to fire on anything but the one tree it was written against.
func ratchetHeadroomMisses(ceilings map[string]map[bool]int, counts map[string]map[bool]int) []ratchetHeadroomMiss {
	var misses []ratchetHeadroomMiss
	for rule, byTest := range ceilings {
		for inTest, ceiling := range byTest {
			reading := counts[rule][inTest]
			if gap := ratchetHeadroomGap(reading, ceiling); gap != 0 {
				misses = append(misses, ratchetHeadroomMiss{
					Rule: rule, InTest: inTest, Reading: reading, Ceiling: ceiling, Gap: gap,
				})
			}
		}
	}
	sort.Slice(misses, func(i, j int) bool {
		if misses[i].Rule != misses[j].Rule {
			return misses[i].Rule < misses[j].Rule
		}
		return !misses[i].InTest && misses[j].InTest
	})
	return misses
}

// TestRatchetHeadroomPredicateIsDistinguishable is the arm's own CONTROL and
// RED, on the predicate the live arm uses.
//
// Three fixtures, and each one is a proposition the predicate must be able to
// tell apart:
//
//	equal    — the ratcheted state, reported by NOTHING (a predicate that
//	           always fires would satisfy the red half and fail here)
//	licence  — ceiling above reading; must be reported, with a positive gap,
//	           naming the rule (this is the shape nobody measured before)
//	drift    — reading above ceiling; must be reported with a negative gap, so
//	           the two directions cannot be confused in a failure message
func TestRatchetHeadroomPredicateIsDistinguishable(t *testing.T) {
	if got := ratchetHeadroomGap(74, 74); got != 0 {
		t.Fatalf("gap(reading 74, ceiling 74) = %d, want 0 — a ceiling sitting at its measurement is the "+
			"swept state, not a licence and not drift", got)
	}

	// CONTROL: every entry exactly at its reading → nothing to report.
	clean := map[string]map[bool]int{RuleIgnoredError: {false: 0, true: 74}}
	if misses := ratchetHeadroomMisses(clean, clean); len(misses) != 0 {
		t.Fatalf("a ceiling table equal to its counts produced %d miss(es) %+v — the predicate must be "+
			"silent on the ratcheted state, or this arm would fire on every green tree", len(misses), misses)
	}

	// THE RED: the exact shape measured on 96a853d — ceiling 77, reading 74.
	loose := map[string]map[bool]int{RuleIgnoredError: {false: 0, true: 77}}
	misses := ratchetHeadroomMisses(loose, clean)
	if len(misses) != 1 {
		t.Fatalf("ceiling 77 against reading 74 produced %d miss(es) %+v, want exactly 1 — this is the "+
			"measured defect (the table claimed \"the ceiling now equals the reading\" while carrying 3 "+
			"sites of headroom)", len(misses), misses)
	}
	if m := misses[0]; m.Rule != RuleIgnoredError || !m.InTest || m.Ceiling != 77 || m.Reading != 74 || m.Gap != 3 {
		t.Fatalf("the reported miss is %+v, want {Rule:%s InTest:true Reading:74 Ceiling:77 Gap:3} — the "+
			"message must name the row and the direction it has to move", m, RuleIgnoredError)
	}
	if d := ratchetHeadroomDirection(74, 77); d == "" || misses[0].Gap <= 0 {
		t.Fatalf("direction for 74/77 = %q (gap %d): a licence must read as positive headroom", d, misses[0].Gap)
	}

	// The OTHER direction, so the two are never conflated in a message.
	drift := map[string]map[bool]int{RuleIgnoredError: {false: 0, true: 73}}
	misses = ratchetHeadroomMisses(drift, clean)
	if len(misses) != 1 || misses[0].Gap != -1 {
		t.Fatalf("ceiling 73 against reading 74 produced %+v, want exactly one miss with gap -1 — the "+
			"drift direction belongs to TestStyleGate and this arm must classify it as negative, not as "+
			"more headroom", misses)
	}
}

// TestRatchetDriftMessageNamesTheMeasuredCause pins the sentence a DRIFT failure
// prints.
//
// The message used to end at "the two are reading different trees" — a
// HYPOTHESIS stated as the explanation. It is the wrong default: the two arms
// resolve the same root (both walk up to go.mod from this package's directory),
// and they differ in something else that was measured on 2026-11-07 on
// 38c536bf: TestStyleGate scans the WORKING TREE, while internal/tools' baseline
// arm (TestStylegateRatchetSuiteIsGreenOnThisTree) runs this suite inside a
// DETACHED WORKTREE OF HEAD. So a site that is uncommitted or untracked is
// visible to one arm and invisible to the other, and that — not a root
// disagreement — is the ordinary cause of a red here beside a green there.
//
// Measured for the RED half of this arm: with an untracked file planted under
// internal/stylegate/ carrying one `_ = f.Close()`, the CLI read
//
//	go run ./internal/stylegate/cmd/stylegate -root . -rule ignored-error
//	→ ignored-error 2   (0 with the file removed)
//
// while `go test ./internal/tools -run TestStylegateRatchetSuiteIsGreenOnThisTree`
// stayed `ok` — the same tree, two answers, which is exactly the asymmetry this
// message now names instead of guessing at.
func TestRatchetDriftMessageNamesTheMeasuredCause(t *testing.T) {
	d := ratchetHeadroomDirection(74, 73)
	for _, want := range []string{
		"WORKING TREE",              // which tree TestStyleGate reads
		"DETACHED WORKTREE OF HEAD", // which tree the merge leg's arm reads
		ratchetDriftTreeCheck,       // the command that tells the two causes apart
	} {
		if !strings.Contains(d, want) {
			t.Fatalf("the drift message does not name %q, so a reader cannot tell a dirty tree from a root "+
				"disagreement:\n%s", want, d)
		}
	}
	// The escape hatch must stay MENTIONED as the second cause, never dropped:
	// with an empty status the dirty-tree explanation is refuted and root
	// resolution is what remains. A message that only ever blames the worktree
	// would be as unhelpful as the one that only ever blamed "different trees".
	if !strings.Contains(d, "root resolution") {
		t.Fatalf("the drift message must name what remains when %s is EMPTY (root resolution), or an "+
			"honest refutation of the dirty-tree cause has nowhere to go:\n%s", ratchetDriftTreeCheck, d)
	}
	// The LICENCE direction must NOT borrow this text: the two directions carry
	// different remedies (tighten the ceiling vs fix/find the site).
	if l := ratchetHeadroomDirection(73, 74); strings.Contains(l, ratchetDriftTreeCheck) {
		t.Fatalf("the licence direction must not carry the drift-only tree check:\n%s", l)
	}
}

// TestEveryCeilingEqualsItsMeasurement is the LIVE half: the shipped ceiling
// table, scanned over this repository, must have no headroom in ANY entry.
//
// It is the guard the table's own prose has been asserting without a check, and
// it is deliberately the strict reading: a ceiling above its measurement is a
// decision to tolerate sites nobody has decided on. When a change removes a site,
// this arm fails until the ceiling follows it down — which is the same commit,
// by the same author, who is the only one who knows the site is gone.
func TestEveryCeilingEqualsItsMeasurement(t *testing.T) {
	root := repoRootFromGateFile(t)
	rep, err := Scan(Options{Root: root, IncludeTests: true})
	if err != nil {
		t.Fatalf("scan %s: %v", root, err)
	}
	if len(rep.PackagesFailed) > 0 {
		t.Fatalf("type information failed for %d package(s), so the reading would be incomplete and every "+
			"comparison below unsound: %v", len(rep.PackagesFailed), rep.PackagesFailed)
	}

	counts := map[string]map[bool]int{}
	for _, f := range rep.Findings {
		if counts[f.Rule] == nil {
			counts[f.Rule] = map[bool]int{}
		}
		counts[f.Rule][isTestPath(f.File)]++
	}

	misses := ratchetHeadroomMisses(gateCeilings, counts)
	if len(misses) == 0 {
		t.Logf("every ceiling equals its measurement (%d entries, undecidable %d)", len(gateCeilings), rep.Undecidable)
		return
	}
	for _, m := range misses {
		scope := "production"
		if m.InTest {
			scope = "test"
		}
		t.Errorf("stylegate %s: ceiling %d vs measured %d (%s scope): %s",
			m.Rule, m.Ceiling, m.Reading, scope, ratchetHeadroomDirection(m.Reading, m.Ceiling))
	}
}
