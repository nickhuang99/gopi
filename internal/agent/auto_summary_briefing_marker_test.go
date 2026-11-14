package agent

// auto_summary_briefing_marker_test.go — THE STORED ROW AFTER THE MARKER (row
// 01a188da).
//
// THE PRICE THIS FILE MEASURES. A parent process now marks the briefing it hands
// a child on stdin (sessionevent.BriefingOnStdin, 16 runes). The ledger folds that
// same briefing's first line into AutoTaskSummaryBudget (200) runes, so every rune
// the marker spends comes out of what the STORED row can still say — and the
// budget's whole contract (row 01a18124) is that the DECISIVE line survives the
// fold. A marker that pushed it out would trade one silent failure for another.
//
// The assertions are on the FINAL STRING THE LEDGER STORES, never on the briefing:
// a briefing can look right while the summary it folds into does not.
//
// Measured at this revision: a real task briefing with the marker folds to 151
// runes and still carries "没有 harness 标记" (the decisive words); unmarked it is
// 135. The headroom is 49 runes of budget, which is why the marker's cost is
// pinned in internal/sessionevent as a number rather than left as prose.

import (
	"strings"
	"testing"

	"github.com/nickhuang99/gopi/internal/sessionevent"
)

// briefingMarkerDecisiveBody is a real briefing's head, taken from this row's own
// transcript: the section headers the producer emits, then the sentence that
// says what the row is about.
const briefingMarkerDecisiveBody = "[EPIC GOAL]\n[Task summary]\n" +
	"bug: 另两个 stdin-briefing 生产点（cli_tasks.go:3413 / tasks.go:10389）没有 harness 标记，框架简报照样占 chats.first_msg 标题\n" +
	"[Briefing payload]\n## 现象\n\n同一个缺陷在另外两个「把 briefing 喂进子 agent 的 stdin」的生产点仍然存在。"

// TestTheMarkerDoesNotEatTheStoredDecisiveLine is the budget arm: the marker is
// paid for, and the decisive sentence is STILL in the row the ledger keeps.
func TestTheMarkerDoesNotEatTheStoredDecisiveLine(t *testing.T) {
	const decisive = "没有 harness 标记"

	unmarked := AutoTaskSummaryLine(briefingMarkerDecisiveBody)
	marked := AutoTaskSummaryLine(sessionevent.BriefingOnStdin(briefingMarkerDecisiveBody))

	if !strings.Contains(unmarked, decisive) {
		t.Fatalf("the fixture's decisive line does not survive the fold WITHOUT the marker (%d runes) — the fixture, not the marker, is what this arm would measure:\n%q",
			len([]rune(unmarked)), unmarked)
	}
	if !strings.Contains(marked, decisive) {
		t.Fatalf("the marker pushed the decisive line out of the STORED summary (%d runes, budget %d):\nstored: %q\ndecisive: %q",
			len([]rune(marked)), AutoTaskSummaryBudget, marked, decisive)
	}
	// The marker must be at the HEAD of what the ledger stores, not somewhere in
	// the middle: a reader tests a LEADING marker, and a stored row that carries
	// it after the auto prefix would be recognised by nothing.
	if !strings.HasPrefix(marked, AutoTaskSummaryPrefix+sessionevent.HarnessBriefingOnStdinMarker) {
		t.Fatalf("the stored summary does not open with the auto prefix then the briefing marker:\n%q", marked)
	}
	// THE BUDGET ITSELF IS UNTOUCHED. Raising the cap to accommodate the marker
	// is the alternative this change deliberately did not take (row 01a18124's
	// ruling: make the decisive line fit, do not raise the cap).
	if AutoTaskSummaryBudget != autoTaskSummaryMaxChars {
		t.Fatalf("AutoTaskSummaryBudget is no longer the fold's own constant")
	}
	// BOTH ARE INSIDE THE BUDGET, which is the property that matters: the fold
	// cuts the BRIEFING at AutoTaskSummaryBudget runes, so what is bounded is the
	// summary minus the auto prefix — plus the "…" the cap appends. (A real
	// briefing this size DOES reach the cap, and the arm below measures the price
	// where it is visible — on a short row.)
	capRunes := len([]rune(AutoTaskSummaryPrefix)) + AutoTaskSummaryBudget + 1
	for _, s := range []string{marked, unmarked} {
		if n := len([]rune(s)); n > capRunes {
			t.Fatalf("a stored summary is %d runes, cap %d (prefix %d + budget %d + ellipsis) — the fold's cap moved:\n%q",
				n, capRunes, len([]rune(AutoTaskSummaryPrefix)), AutoTaskSummaryBudget, s)
		}
	}
}

// TestTheMarkerCostsItsOwnLengthOutOfAShortRow is the cost arm on a briefing the
// fold does NOT truncate — the only shape where the price is the marker's own
// length rather than a shifted window.
//
// Both shapes matter and they are different facts: on a SHORT row the marker is
// the only difference (16 runes, exactly); on a LONG row the cap swallows the
// difference in total length while pushing 16 runes of the briefing's TAIL out of
// view. Asserting only one of the two would let a reworded marker change the
// ledger's content silently in the other.
func TestTheMarkerCostsItsOwnLengthOutOfAShortRow(t *testing.T) {
	const shortBody = "bug: the stdin briefing carries no marker"
	unmarked := AutoTaskSummaryLine(shortBody)
	marked := AutoTaskSummaryLine(sessionevent.BriefingOnStdin(shortBody))
	if strings.HasSuffix(unmarked, "…") {
		t.Fatalf("the fixture reaches the cap, so it cannot measure a price:\n%q", unmarked)
	}
	if delta := len([]rune(marked)) - len([]rune(unmarked)); delta != len([]rune(sessionevent.HarnessBriefingOnStdinMarker)) {
		t.Fatalf("the marker cost %d runes of a short stored row, want %d (the marker's own length):\nmarked:   %q\nunmarked: %q",
			delta, len([]rune(sessionevent.HarnessBriefingOnStdinMarker)), marked, unmarked)
	}
}

// TestAMarkedBriefingStillFoldsToOneCleanLine pins the OTHER half of the
// fold contract that a prefix could plausibly break: the stored summary is one
// line, no newlines, no leading/trailing space. A marker carrying a newline (or a
// wrapper that added one) would put the ledger's first line at the marker and
// leave the decisive text on a second.
func TestAMarkedBriefingStillFoldsToOneCleanLine(t *testing.T) {
	marked := AutoTaskSummaryLine(sessionevent.BriefingOnStdin(briefingMarkerDecisiveBody))
	if strings.ContainsAny(marked, "\n\t\r") {
		t.Fatalf("the stored summary carries a control character (the fold collapsed nothing):\n%q", marked)
	}
	if strings.TrimSpace(marked) != marked {
		t.Fatalf("the stored summary carries edge whitespace:\n%q", marked)
	}
}
