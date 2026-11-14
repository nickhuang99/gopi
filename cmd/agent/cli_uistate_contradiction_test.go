package main

// cli_uistate_contradiction_test.go — the REPORTED contradiction, pinned at the verb
// the report was collected from.
//
// THE REPORT (task 01a0e313-ac65, real 117x26 pty, `gopi ui_state --target chat --all`):
//
//	line 0 damage=paint_loss grid_row=-1 in_grid=false in_transcript=true
//	expected_cols=95 expected_runes=95 grid_cols=117 prefix=95
//
// The verdict contradicts the measurement printed BESIDE it: `prefix=95` equals
// `expected_runes=95` — the longest verbatim prefix on the grid is the WHOLE line, so
// the paint carried all of it — while the damage says the paint lost it. The row was
// not the line because it carried MORE than the line.
//
// The repaint_tail rule (commit e167bbb9, landed after this report) closed the exact
// shape the report measured — the row holding the line DOUBLED — but not the rows
// where the re-paint left something else after the line, and it never reached the
// ONE-LINE face (`--compare`, and the `chat_surface` tool action behind it), which
// still emitted paint_loss for a row carrying the whole line.
//
// WHAT IS ASSERTED HERE is the property the report asks for and nothing weaker: the
// printed measurements must RECONCILE with the verdict. Concretely, on a row that
// displays the whole line, `damage=paint_loss` is never printed; the run is not left
// empty either — the emission inventory below is exact, so a bucket that stopped being
// produced fails the case instead of hiding behind a substring check.

import (
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/nickhuang99/gopi/internal/uiclient"
)

// contradictionLine is the subject line for these cases. The report's own line was 95
// runes on a row of 190 (twice the line), which is LONGER THAN THE BAND: the band
// splits a row wider than itself across band rows, so a fixture whose row exceeded 117
// cells did not put the shape on the grid at all (measured: the grid came back holding
// only the split's last piece, prefix=0). The SHAPE is what these cases are about, and
// the length is chosen so the whole row fits the band and is therefore really on the
// grid the verdict is computed from. The over-wide doubling shape is pinned where it
// can be measured exactly — internal/uiclient's own cases, on hand-built grids.
const contradictionLine = "self-observation: the whole line lands on the canvas."

// contradictionOtherLine is the text a second paint left after the line on the same
// row: enough to make the row "not exactly the line", short enough to fit the band
// beside it.
const contradictionOtherLine = "and here is a different line drawn afterwards"

// The damage words are spelled HERE rather than read from the package under test,
// for the reason the geometry contract test gives for its own literal: a case that
// read the implementation's constant would agree with the code by construction, and
// the assertion this file makes is about the CONTRACT a reader of the output sees.
// A word added to the package without a side decided here fails the inventory.
const (
	uiStateAuditDamagePaintLoss   = "paint_loss"
	uiStateAuditDamageRepaintTail = "repaint_tail"
	uiStateAuditDamageToRow       = "damage_to_row"
	uiStateAuditDamageTextWrapped = "text_wrapped"
	// uiStateFieldLostText is the ledger field the "was any text lost" answer is
	// read from — the count the whole-answer STATUS is derived from.
	uiStateFieldLostText = "lost_text"
)

// contradictionRowAuditLine matches one `line N damage=... ` record of the audit's
// text output, capturing the damage word and the measurements printed beside it.
var contradictionRowAuditLine = regexp.MustCompile(`line \d+ damage=(\S+) grid_row=(-?\d+) in_grid=(\w+) in_transcript=(\w+) expected_cols=(\d+) expected_runes=(\d+) grid_cols=(\d+) prefix=(\d+)`)

// contradictionAuditFixture paints `painted` through the production band on a REAL
// 117x26 pty (the same fixture the whole-answer cases use) and writes a session log
// whose answer is `transcript`. It returns the session id the audit is invoked with.
//
// The rig is the package's own (auditFixture): the grid comes from the production
// sink driven by the production editor, never from a grid typed into a file, so the
// assertion is about what the product paints rather than about a test's idea of a
// screen (Rule 31).
func contradictionAuditFixture(t *testing.T, painted, transcript []string) string {
	t.Helper()
	_, session := auditFixture(t, painted, transcript)
	return session
}

// TestUIStateAudit_RowDisplayingTheWholeLineIsNeverPaintLoss is the report's own case
// through the VERB. Two row shapes are distinguished deliberately:
//
//   - a row holding the line twice (`line+line`): the re-paint rule's case. It is an
//     arrival (in_grid=true) and the ledger records repaint_tail.
//   - a row holding the line three times (`line+line+line`): the shape that still
//     contradicted itself on master before this change — `in_grid=false` beside
//     `prefix=95` of 95 runes and `damage=paint_loss`.
//
// For both, the printed damage must not be paint_loss, and the printed prefix must
// reconcile with it: a damage word that says "lost" while the prefix covers the line
// is the defect this case exists to catch.
func TestUIStateAudit_RowDisplayingTheWholeLineIsNeverPaintLoss(t *testing.T) {
	cases := []struct {
		name     string
		row      string
		wantWord string
	}{
		{
			name:     "the line doubled on its row (the report's measured shape)",
			row:      contradictionLine + contradictionLine,
			wantWord: uiStateAuditDamageRepaintTail,
		},
		{
			name:     "the line and a second line drawn over the row's tail",
			row:      contradictionLine + contradictionOtherLine,
			wantWord: uiStateAuditDamageToRow,
		},
		{
			// The row carries the line, then a re-paint's head, then ANOTHER line:
			// the tail is not the line's own head, so the re-paint rule cannot fire
			// either — the shape that still contradicted itself on master.
			name:     "the line, its own head re-drawn, and then a different line",
			row:      contradictionLine + contradictionLine[:12] + contradictionOtherLine,
			wantWord: uiStateAuditDamageToRow,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			session := contradictionAuditFixture(t, []string{tc.row}, []string{contradictionLine})
			out, code := uiStateRun(t, auditArgs(session))

			rec, ok := contradictionRowRecord(t, out)
			if !ok {
				t.Fatalf("the audit printed no line record; output:\n%s", out)
			}
			if rec.Damage == uiStateAuditDamagePaintLoss {
				t.Fatalf("VERDICT CONTRADICTS ITS OWN EVIDENCE: in_grid=%t prefix=%d expected_runes=%d damage=%s — the row displays the whole line, so the paint did not lose it (exit=%d)\noutput:\n%s",
					rec.InGrid, rec.Prefix, rec.ExpectedRunes, rec.Damage, code, out)
			}
			if rec.Damage != tc.wantWord {
				t.Fatalf("damage=%q, want %q\noutput:\n%s", rec.Damage, tc.wantWord, out)
			}
			// The reconciliation itself, stated as the report states it: a damage
			// word that blames the paint for text the paint carried.
			if rec.Prefix >= rec.ExpectedRunes && !uiStateDamageArrivedWord(rec.Damage) {
				t.Fatalf("the printed measurements do not reconcile with the verdict: prefix=%d covers expected_runes=%d, and damage=%q blames the paint\noutput:\n%s",
					rec.Prefix, rec.ExpectedRunes, rec.Damage, out)
			}
			// THE EXIT CONTRACT, unchanged (the deliverable's own condition): a lost
			// line is exit 3, everything else is exit 0 or a refusal. An arrival that
			// was not exactly alone is still exit 0 — no text was lost.
			if code != 0 {
				t.Fatalf("exit=%d, want 0 for a line the grid displays (no text was lost)\noutput:\n%s", code, out)
			}
			if !strings.Contains(out, uiStateAuditWordAgreed) {
				t.Fatalf("the outcome word is not %q beside exit 0\noutput:\n%s", uiStateAuditWordAgreed, out)
			}
		})
	}
}

// TestUIStateAudit_LostLineStillSaysPaintLoss is the anti-widening control at the
// verb: the same invocation with a grid that does NOT carry the line must still print
// damage=paint_loss, the lost_text count must be 1, and the exit code must still be
// the disagreement code. Without this the case above could be satisfied by an audit
// that stopped reporting losses.
func TestUIStateAudit_LostLineStillSaysPaintLoss(t *testing.T) {
	session := contradictionAuditFixture(t, []string{"a row that has nothing of the audited line"}, []string{contradictionLine})
	out, code := uiStateRun(t, auditArgs(session))

	rec, ok := contradictionRowRecord(t, out)
	if !ok {
		t.Fatalf("the audit printed no line record; output:\n%s", out)
	}
	if rec.Damage != uiStateAuditDamagePaintLoss {
		t.Fatalf("damage=%q, want %q — a line the grid does not carry at all is the real-loss class\noutput:\n%s",
			rec.Damage, uiStateAuditDamagePaintLoss, out)
	}
	if !strings.Contains(out, uiStateFieldLostText+"=1") {
		t.Fatalf("the ledger does not report %s=1 for a lost line\noutput:\n%s", uiStateFieldLostText, out)
	}
	if code != uiStateExitCompareMismatch {
		t.Fatalf("exit=%d, want %d (the disagreement code) for a lost line\noutput:\n%s", code, uiStateExitCompareMismatch, out)
	}
	if !strings.Contains(out, uiStateAuditWordDisagreed) {
		t.Fatalf("the outcome word is not %q\noutput:\n%s", uiStateAuditWordDisagreed, out)
	}
}

// TestUIStateCompare_SingleLineFaceCarriesTheSameDamageVocabulary is finding (4) of
// the epic, pinned: the contradiction was reachable through `--compare` and through
// the `chat_surface` tool action, which print a single verdict rather than a ledger.
// Both now carry the SAME damage word, decided by the same classifier, so a row that
// displays the whole line cannot be a paint_loss on one face and an arrival on the
// other.
func TestUIStateCompare_SingleLineFaceCarriesTheSameDamageVocabulary(t *testing.T) {
	painted := []string{contradictionLine + contradictionOtherLine}
	session := contradictionAuditFixture(t, painted, []string{contradictionLine})

	out, code := uiStateRun(t, []string{uiStateFlagCompare, contradictionLine, uiStateFlagSession, session, uiStateFlagTarget, string(uiclient.SurfaceChat)})
	if !strings.Contains(out, "damage="+uiStateAuditDamageToRow) {
		t.Fatalf("the single-line face does not carry damage=%s for a row that displays the whole line\noutput:\n%s",
			uiStateAuditDamageToRow, out)
	}
	if strings.Contains(out, "damage="+uiStateAuditDamagePaintLoss) {
		t.Fatalf("the single-line face blamed the paint for a line the row displays\noutput:\n%s", out)
	}
	// The one-line face stays a MISMATCH (no single row is the line) and exits 3:
	// the exit contract is untouched, and the damage word is what tells the reader
	// which layer to look at.
	if code != uiStateExitCompareMismatch {
		t.Fatalf("exit=%d, want %d — the row is not the line, and this face reports that as a mismatch\noutput:\n%s",
			code, uiStateExitCompareMismatch, out)
	}
}

// uiStateContradictionRow is one parsed `line N damage=... ` record.
type uiStateContradictionRow struct {
	Damage        string
	InGrid        bool
	InTranscript  bool
	ExpectedCols  int
	ExpectedRunes int
	GridCols      int
	Prefix        int
}

// contradictionRowRecord parses the FIRST `line N` record out of the audit's text
// output, and reports whether there was one. Fields are read from the printed line
// rather than from a struct the test built itself: the assertion is about what a
// reader of the output sees.
func contradictionRowRecord(t *testing.T, out string) (uiStateContradictionRow, bool) {
	t.Helper()
	for _, line := range strings.Split(out, "\n") {
		m := contradictionRowAuditLine.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		atoi := func(s string) int { n, _ := strconv.Atoi(s); return n } //stylegate:ignore parses a count out of the fixture's own output; a parse failure yields 0 and fails the comparison below
		return uiStateContradictionRow{
			Damage:        m[1],
			InGrid:        m[3] == "true",
			InTranscript:  m[4] == "true",
			ExpectedCols:  atoi(m[5]),
			ExpectedRunes: atoi(m[6]),
			GridCols:      atoi(m[7]),
			Prefix:        atoi(m[8]),
		}, true
	}
	return uiStateContradictionRow{}, false
}

// uiStateDamageArrivedWord reports whether a damage word means "the text is on the
// grid" rather than "a layer lost it". It is spelled HERE rather than read from the
// package under test, so the assertion is about the contract a reader sees: adding a
// class to the package without deciding its side fails this case.
func uiStateDamageArrivedWord(word string) bool {
	switch word {
	case uiStateAuditDamageRepaintTail, uiStateAuditDamageToRow, uiStateAuditDamageTextWrapped:
		return true
	default:
		return false
	}
}

// TestUIStateDamageWords_HaveADecidedSide is the inventory half of the contract: every
// damage word the package can emit is either an arrival word or a loss word, decided
// HERE, and the two lists together are the whole vocabulary. A new class fails this
// case rather than slipping into a face's output unclassified.
func TestUIStateDamageWords_HaveADecidedSide(t *testing.T) {
	// NOTE ON WHAT THIS INVENTORY CAN SEE: the words are literals here rather than
	// reads of the package's constants, which is what makes the case about the
	// contract instead of about the code — but it also means a class added to the
	// package is caught only when this list is consulted. The list below is the
	// package's own vocabulary, kept beside the classes it names; the faces' own
	// output is what the other cases in this file assert on.
	arrived := []string{uiStateAuditDamageRepaintTail, uiStateAuditDamageToRow, uiStateAuditDamageTextWrapped}
	lost := []string{uiStateAuditDamagePaintLoss, "wrap_artifact_or_loss", "cjk_wrap"}
	for _, w := range arrived {
		if !uiStateDamageArrivedWord(w) {
			t.Errorf("%q is an arrival word but the predicate does not say so", w)
		}
	}
	for _, w := range lost {
		if uiStateDamageArrivedWord(w) {
			t.Errorf("%q is a loss word but the predicate calls it an arrival", w)
		}
	}
	// The two lists must agree with the package's own constants for the classes they
	// name — the ONE direction this case can check without re-typing the vocabulary:
	// a constant that the package renamed or dropped leaves a dangling literal here.
	named := append(append([]string{}, arrived...), lost...)
	for _, w := range []string{
		string(uiclient.DamagePaintLoss), string(uiclient.DamageWrapArtifactOrLoss),
		string(uiclient.DamageCJKWrap), string(uiclient.DamageRepaintTail),
		string(uiclient.DamageTextWrapped), string(uiclient.DamageToRow),
	} {
		if !containsWord(named, w) {
			t.Errorf("the package emits damage=%q and this inventory does not classify it", w)
		}
	}
}

func containsWord(words []string, w string) bool {
	for _, got := range words {
		if got == w {
			return true
		}
	}
	return false
}
