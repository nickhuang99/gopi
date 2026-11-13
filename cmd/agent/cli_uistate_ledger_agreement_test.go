// cli_uistate_ledger_agreement_test.go — the audit ledger's counters and its own
// `lines` list must be the SAME facts read twice.
//
// THE REPORTED DEFECT (task 01a0e4f5, on the real dump of session
// 01a0e49e-6730-755a-a6ed-11dc0dfa5d96, `gopi ui_state --target chat --all
// --json`): the ledger printed
//
//	audited=7 matched=6 disagreed=1 rejoined=1 repainted=0 lost_text=1
//	lines: [index 1 status=mismatch … rejoined=true  -> "the text arrived" ]
//	       [index 4 status=mismatch damage=wrap_artifact_or_loss]
//
// Three contradictions in one ledger: TWO mismatch entries beside `disagreed: 1`;
// `rejoined: 1` naming no entry at all; and the entry that DID carry the rejoin was
// simultaneously a "mismatch" and a sentence saying the text arrived. Every counter
// was incremented at one point of the auditing loop while the entry describing the
// same line was written at another, so nothing asked the two to agree.
//
// WHAT IS ASSERTED HERE, and nothing weaker: the counters are RE-DERIVED from the
// entry list, by this file's own reading of the words it sees, and must equal the
// counters the verb printed. The reading is spelled in literals (see the damage
// words below) rather than read from the package, so a class that silently changed
// side fails this case instead of agreeing with the code by construction.
//
// THE SHAPE OF THE FIXTURE is the one the defect was measured on: a line WIDER than
// the grid, painted through the production band on a REAL pty (auditFixture), so the
// grid WRAPS it across rows — the arrival that no single row carries, which is the
// case whose entry used to be a "mismatch" while the count called it a rejoin.

package main

import (
	"encoding/json"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

// The damage words and verdict statuses this file classifies, spelled HERE rather
// than read from the package under test (the convention
// cli_uistate_contradiction_test.go established for its own inventory): the
// assertion is about the contract a reader of the output sees, so a word that moved
// to the other side must fail this case.
const (
	ledgerArrivalTextureWrapped = "text_wrapped"
	ledgerArrivalRepaintTail    = "repaint_tail"
	ledgerArrivalDamageToRow    = "damage_to_row"
	ledgerArrivalEmphasisPlain  = "emphasis_unpainted"

	ledgerLossPaintLoss          = "paint_loss"
	ledgerLossWrapArtifactOrLoss = "wrap_artifact_or_loss"
	ledgerLossCJKWrap            = "cjk_wrap"

	ledgerStatusMismatch = "mismatch"
	ledgerStatusRejoined = "rejoined"

	// ledgerRejoinNamesRowsFragment is the part of a rejoin sentence that NAMES the
	// rows the line was reassembled from — the fact that makes the ledger's
	// `rejoined` count checkable rather than a number to trust.
	ledgerRejoinNamesRowsFragment = "WRAPPED across rows ["
)

// ledgerArrivalDamage reports whether a damage word means "the text is on the
// grid". The empty word is an arrival: it is what a record with nothing to report
// carries.
func ledgerArrivalDamage(word string) bool {
	switch word {
	case "", ledgerArrivalTextureWrapped, ledgerArrivalRepaintTail,
		ledgerArrivalDamageToRow, ledgerArrivalEmphasisPlain:
		return true
	default:
		return false
	}
}

// ledgerLossDamage reports whether a damage word means a layer LOST the text. It is
// the complement of the predicate above over the closed vocabulary, decided here so
// the two can never overlap.
func ledgerLossDamage(word string) bool {
	switch word {
	case ledgerLossPaintLoss, ledgerLossWrapArtifactOrLoss, ledgerLossCJKWrap:
		return true
	default:
		return false
	}
}

// ledgerWrappedLine is wider than the fixture's grid (117 cells) and made of whole
// words, so the band wraps it the way a terminal wraps an answer line rather than by
// cutting a token in half.
const ledgerWrappedLine = "the answer continues past the width of the grid so the band wraps it across " +
	"consecutive rows exactly as a terminal does with a paragraph that is too wide for the window it was painted in"

// ledgerRunAuditJSON runs the REAL verb with --json and decodes the envelope, so the
// counters are read as DATA rather than scraped from prose.
func ledgerRunAuditJSON(t *testing.T, session string) (uiStateAuditRecord, string, int) {
	t.Helper()
	out, code := uiStateRun(t, auditArgs(session, uiStateFlagJSON))
	var rec uiStateAuditRecord
	if err := json.Unmarshal([]byte(out), &rec); err != nil {
		t.Fatalf("the audit's --json envelope does not decode: %v\noutput:\n%s", err, out)
	}
	return rec, out, code
}

// TestUIStateAudit_LedgerCountersAgreeWithTheEntryList is the regression case for the
// reported defect, driven through the real `gopi ui_state --all` on a canned dump.
func TestUIStateAudit_LedgerCountersAgreeWithTheEntryList(t *testing.T) {
	_, session := auditFixture(t, []string{ledgerWrappedLine}, []string{ledgerWrappedLine})
	rec, out, code := ledgerRunAuditJSON(t, session)
	led := rec.Ledger

	// The fixture must EXERCISE the case it exists for: a wrap the grid really drew.
	// A case that quietly stopped wrapping would pass every assertion below while
	// testing nothing — the arrival, whose entry and whose counter used to disagree,
	// would never appear.
	if led.Rejoined == 0 {
		t.Fatalf("the fixture did not produce a wrapped line: rejoined=%d, so the counters-vs-list case is not exercised\noutput:\n%s", led.Rejoined, out)
	}
	if led.Audited == 0 {
		t.Fatalf("the fixture audited nothing: audited=0\noutput:\n%s", out)
	}

	// ONE ENTRY PER LINE, read by this file's own rules.
	seen := map[int]bool{}
	var mismatches, rejoins, losses, unnamedRejoins int
	for _, e := range led.Lines {
		if seen[e.Index] {
			t.Fatalf("line %d appears twice in the ledger's list: one line, one entry\noutput:\n%s", e.Index, out)
		}
		seen[e.Index] = true
		if strings.TrimSpace(e.Verdict.Line) == "" {
			t.Fatalf("entry %d carries no line text, so the ledger does not say WHICH line it is about\noutput:\n%s", e.Index, out)
		}
		switch {
		case ledgerArrivalDamage(string(e.Damage)):
			// THE CONTRADICTION, stated as the report states it: an entry whose class
			// says the text is on the grid must not carry the loss status. The ONE
			// exception is the class that reports a damage to the ROW while the text
			// arrived (damage_to_row) — and it is an arrival for the counters, which
			// is what the reconciliation below charges it as.
			if e.Damage != "" && e.Damage != ledgerArrivalDamageToRow && e.Verdict.Status != ledgerStatusRejoined {
				t.Fatalf("entry %d is damage=%q — an arrival — and verdict.status=%q: one line, ONE verdict\noutput:\n%s",
					e.Index, e.Damage, e.Verdict.Status, out)
			}
		case ledgerLossDamage(string(e.Damage)):
			losses++
		default:
			t.Fatalf("entry %d carries damage=%q, which this file's inventory does not classify as an arrival or a loss — a new class needs its side decided here\noutput:\n%s",
				e.Index, e.Damage, out)
		}
		switch e.Verdict.Status {
		case ledgerStatusMismatch:
			mismatches++
		case ledgerStatusRejoined:
			rejoins++
			// A REJOIN IS NAMED: the count is only checkable if the entry says which
			// rows the line was reassembled from, and says that it arrived.
			if !strings.Contains(e.Verdict.Reason, ledgerRejoinNamesRowsFragment) {
				unnamedRejoins++
			}
			if !strings.Contains(e.Verdict.Reason, "the text arrived") {
				t.Fatalf("entry %d rejoined but its sentence does not say the text arrived: %q\noutput:\n%s",
					e.Index, e.Verdict.Reason, out)
			}
		}
	}
	if unnamedRejoins > 0 {
		t.Fatalf("%d rejoined entr(y/ies) do not name the rows they were reassembled from (want a sentence containing %q)\noutput:\n%s",
			unnamedRejoins, ledgerRejoinNamesRowsFragment, out)
	}
	if mismatches != 0 {
		t.Fatalf("%d entr(y/ies) still carry verdict.status=%q while their class says the text arrived: the status IS the counting rule\noutput:\n%s",
			mismatches, ledgerStatusMismatch, out)
	}

	// THE RECONCILIATION: every counter equals the count this file derived from the
	// list, and the arithmetic closes.
	if led.Rejoined != rejoins {
		t.Fatalf("rejoined=%d but %d entr(y/ies) in the list rejoined: the count does not name its entries\noutput:\n%s", led.Rejoined, rejoins, out)
	}
	if led.Disagreed != losses {
		t.Fatalf("disagreed=%d but the list holds %d loss class(es): a disagreement is a line whose class says a layer lost the text, and every such entry is counted once\noutput:\n%s",
			led.Disagreed, losses, out)
	}
	if led.LostText != losses {
		t.Fatalf("lost_text=%d but the list holds %d loss class(es)\noutput:\n%s", led.LostText, losses, out)
	}
	if led.Matched != led.Audited-led.Disagreed {
		t.Fatalf("matched=%d does not close the arithmetic: audited=%d disagreed=%d\noutput:\n%s",
			led.Matched, led.Audited, led.Disagreed, out)
	}
	// Every named line index is one of the audited lines: the property that makes the
	// counts above meaningful.
	for idx := range seen {
		if idx < 0 || idx >= led.Audited {
			t.Fatalf("the list names line %d, outside the %d audited lines\noutput:\n%s", idx, led.Audited, out)
		}
	}
	// The fixture's own shape: the wrapped line ARRIVED, so nothing was lost and the
	// run agrees with itself.
	if led.LostText != 0 {
		t.Fatalf("lost_text=%d for a line the grid carries WRAPPED: the wrap is not a loss\noutput:\n%s", led.LostText, out)
	}
	if code != 0 || rec.Outcome != uiStateOutcomeAgreed {
		t.Fatalf("exit=%d outcome=%q for an answer that arrived: a wrap is not a disagreement\noutput:\n%s",
			code, rec.Outcome, out)
	}
	// The named rows are a REAL row list, not an empty pair of brackets.
	if !regexp.MustCompile(`rows \[\d+`).MatchString(led.Lines[0].Verdict.Reason) {
		t.Fatalf("the rejoin sentence names no row number: %q", led.Lines[0].Verdict.Reason)
	}
}

// TestUIStateAudit_TextFacePrintsTheSameCountersAsTheList is the other face of the
// same run: the human rendering prints the numbers the JSON ledger carries, and one
// record per entry, so the two renderings cannot tell a reader two stories about one
// audit.
func TestUIStateAudit_TextFacePrintsTheSameCountersAsTheList(t *testing.T) {
	_, session := auditFixture(t, []string{ledgerWrappedLine}, []string{ledgerWrappedLine})
	rec, jsonOut, code := ledgerRunAuditJSON(t, session)
	textOut, textCode := uiStateRun(t, auditArgs(session))

	if code != textCode {
		t.Fatalf("--json exited %d and the text rendering exited %d for the same audit\noutput:\n%s", code, textCode, jsonOut)
	}
	led := rec.Ledger
	for _, want := range []string{
		"matched=" + strconv.Itoa(led.Matched),
		"disagreed=" + strconv.Itoa(led.Disagreed),
		"rejoined=" + strconv.Itoa(led.Rejoined),
		"audited=" + strconv.Itoa(led.Audited),
		"lost_text=" + strconv.Itoa(led.LostText),
	} {
		if !strings.Contains(textOut, want) {
			t.Fatalf("the text rendering does not carry %q, which the JSON ledger reports\noutput:\n%s", want, textOut)
		}
	}
	if got := strings.Count(textOut, "\n  line "); got != len(led.Lines) {
		t.Fatalf("the text rendering prints %d line records for %d ledger entries\noutput:\n%s", got, len(led.Lines), textOut)
	}
}
