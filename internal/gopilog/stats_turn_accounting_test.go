// Regression gate for the accounting defect this task repaired (Rule 5:
// every fix ships a minimal repro).
//
// THE DEFECT, measured 2026-11-01 on session
// e1e22e9f-eee0-4c33-a238-0fca04534ad9 (`gopi session stats`):
//
//	turns        329
//	human turns   83
//
// Two numbers under two words, and NOTHING on the screen said where the other
// 246 went. A reader can only take that as a disagreement between two turn
// counts — which is exactly how the operator reported it ("314 is neither a turn
// nor a round"). The 246 is real and measurable: that log holds 247
// notify-opened seams and 83 person-opened ones (330 seams), and the ledger's
// 329 is the same count one behind because the turn in flight had not closed.
//
// So the block had two correct numbers and no ARITHMETIC: the total and its
// subset must ACCOUNT FOR EACH OTHER on screen, or the reader is left to subtract
// two words that do not announce they are related.
package gopilog

import (
	"strconv"
	"strings"
	"testing"

	"github.com/nickhuang99/gopi/internal/sessionevent"
)

// accountingStats is the smallest block that shows the defect: a measured total,
// a measured person-only subset, and a residual nobody printed.
func accountingStats() *SessionStats {
	return &SessionStats{
		SessionID:     "01a1948f-accounting-fixture",
		Source:        StatsSourceJSONL,
		Sessions:      1,
		Rounds:        42,
		TurnsMeasured: true,
		TurnsOpened:   329,
		TurnCount:     331,
		HumanTurns:    83,
		PromptTokens:  9030,
		Completion:    6321,
	}
}

// TestStatsAccountsForEveryTurnItPrints: the residual row must appear and must
// equal total − subset. A future change that drops it puts the reader back in
// front of two unrelated-looking numbers.
func TestStatsAccountsForEveryTurnItPrints(t *testing.T) {
	st := accountingStats()
	out := FormatStatsOverview(st)

	// PREMISE (Rule 10.4): the fixture can distinguish the two answers. A fixture
	// where the subset equals the total has no residual to print and would make
	// this test blind.
	// THE TOTAL THE RESIDUAL ACCOUNTS FOR IS THE ROW PRINTED ABOVE IT — the log's
	// OPENED count (epic 2026-11-04). The ledger's completed count sits on its own
	// row one line down and must never be subtracted from here.
	if st.TurnsOpened == st.HumanTurns {
		t.Fatalf("fixture is blind: total %d equals subset %d, so no residual exists", st.TurnsOpened, st.HumanTurns)
	}
	if st.TurnCount == st.TurnsOpened {
		t.Fatalf("fixture is blind: the two turn counts are equal, so a residual taken from the wrong one would pass")
	}
	want := st.TurnsOpened - st.HumanTurns

	var residualLine string
	for _, line := range strings.Split(out, "\n") {
		if strings.Contains(line, sessionevent.CounterUnitOtherTurn) {
			residualLine = line
			break
		}
	}
	if residualLine == "" {
		t.Fatalf("no %q row: a reader is left to subtract the total and the subset by hand, which is the defect:\n%s",
			sessionevent.CounterUnitOtherTurn, out)
	}
	if !strings.Contains(residualLine, strconv.Itoa(want)) {
		t.Fatalf("the residual row reads %q, want it to carry %d (total %d − subset %d)",
			strings.TrimSpace(residualLine), want, st.TurnsOpened, st.HumanTurns)
	}
	// The three numbers must ACCOUNT, not merely coexist: total = subset + residual.
	if st.HumanTurns+want != st.TurnsOpened {
		t.Fatalf("the printed numbers do not account: %d + %d != %d", st.HumanTurns, want, st.TurnsOpened)
	}
}

// TestStatsResidualAccountsAgainstTheMeasuredRowAboveIt: the residual is DERIVED,
// and since epic 2026-11-04 it is derived from the LOG'S OPENED count — the row
// it sits under — never from the ledger's completed count, which rides its own
// row and can sit behind OR ahead of the opened axis.
//
// THE EARLIER VERSION OF THIS ARM ASSERTED THE OPPOSITE ("an unmeasured total
// prints no residual"), and that premise is what changed: the ledger going
// unread no longer makes the total unknown, because the total is no longer the
// ledger's number. What must NOT appear is a completed row carrying a number
// nobody measured (Rule 15).
func TestStatsResidualAccountsAgainstTheMeasuredRowAboveIt(t *testing.T) {
	st := accountingStats()
	st.TurnsMeasured = false // the LEDGER row was not read
	st.TurnCountNote = "fixture: the ledger row was not read"

	out := FormatStatsOverview(st)
	var residual string
	for _, line := range strings.Split(out, "\n") {
		if strings.Contains(line, sessionevent.CounterUnitOtherTurn) {
			residual = line
		}
		if strings.HasPrefix(line, "\u2502 "+sessionevent.CounterUnitTurnCompleted+" ") &&
			!strings.Contains(line, TurnCountUnmeasuredPrefix) {
			t.Fatalf("an UNMEASURED ledger count printed a completed row carrying a NUMBER (%q) — a number "+
				"nobody measured:\n%s", strings.TrimSpace(line), out)
		}
	}
	if residual == "" {
		t.Fatalf("an unmeasured LEDGER removed the residual of the OPENED count — two facts, and losing the "+
			"measured one is not what \"do not fabricate\" asks for:\n%s", out)
	}
	if want := st.TurnsOpened - st.HumanTurns; !strings.Contains(residual, strconv.Itoa(want)) {
		t.Fatalf("the residual reads %q, want it to carry %d (opened %d − subset %d)",
			strings.TrimSpace(residual), want, st.TurnsOpened, st.HumanTurns)
	}
}
