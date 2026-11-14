package lineedit

// turn_stamp_test.go — the band header STATES WHEN THE TURN OPENED, and states
// it from the record's value rather than from a clock.
//
// Three properties, each one a way the banner can lie:
//
//  1. a stamped header carries the turn's own moment, spelled by the SHARED
//     formatter (sessionevent.FormatTurnStamp) — not by a second format string
//     living here;
//  2. the moment rides the SAME line as the ordinal and the prompt, between
//     them, so a narrow band cuts the PROMPT's tail rather than the time;
//  3. a caller that sets no stamp gets byte-identical output to the pre-stamp
//     header — the zero value must not silently add eighteen cells to every
//     existing caller (that arm is what keeps this change honest: the feature is
//     opt-in on the RECORD, not a global restyle).

import (
	"strings"
	"testing"
	"time"

	"github.com/nickhuang99/gopi/internal/sessionevent"
)

// TestTurnHeaderCarriesTheTurnsOwnMoment pins 1 and 2.
func TestTurnHeaderCarriesTheTurnsOwnMoment(t *testing.T) {
	const cols = 120
	at := time.Date(2026, 11, 8, 12, 31, 20, 0, time.FixedZone("CST", 8*3600))
	lines := TurnHeaderLines(TurnInfo{
		Index:    7,
		Prompt:   "the operator's ask",
		OpenedAt: at,
		Stamp:    true,
	}, cols)
	if len(lines) == 0 {
		t.Fatalf("TurnHeaderLines returned no rows for a stamped turn")
	}
	row := stripSGR(lines[0])
	stamp := sessionevent.FormatTurnStamp(at)
	if !strings.Contains(row, stamp) {
		t.Fatalf("the header does not carry the turn's moment %q:\n%s", stamp, row)
	}
	if !strings.Contains(row, "7") || !strings.Contains(row, "the operator's ask") {
		t.Fatalf("the header lost its ordinal or its prompt:\n%s", row)
	}
	// ORDER: ordinal, then moment, then prompt — the web banner's order, and the
	// one that survives a cut with the least damage (a cut tail loses the ask,
	// which the answer below restates).
	iNum := strings.Index(row, "7")
	iStamp := strings.Index(row, stamp)
	iPrompt := strings.Index(row, "the operator's ask")
	if !(iNum < iStamp && iStamp < iPrompt) {
		t.Fatalf("the three fields are not in ordinal→moment→prompt order (num %d, stamp %d, prompt %d):\n%s",
			iNum, iStamp, iPrompt, row)
	}
	// The whole header must respect the band: one row, at most cols cells.
	if got := displayWidth(row); got > cols {
		t.Fatalf("the stamped header is %d cells wide at cols=%d:\n%s", got, cols, row)
	}
}

// TestTurnHeaderStatesAnAbsentMoment — a stamped turn whose row recorded no
// timestamp prints the SHARED placeholder. It is the same arm as the formatter's,
// one layer up: it proves this renderer routes through that function instead of
// falling back to `time.Now()` (which would be a header that always looked
// complete and sometimes stated the wrong day).
func TestTurnHeaderStatesAnAbsentMoment(t *testing.T) {
	lines := TurnHeaderLines(TurnInfo{Index: 3, Prompt: "asked at an unknown moment", Stamp: true}, 120)
	if len(lines) == 0 {
		t.Fatalf("TurnHeaderLines returned no rows")
	}
	row := stripSGR(lines[0])
	if !strings.Contains(row, sessionevent.TurnStampPlaceholder) {
		t.Fatalf("an absent moment did not render as %q:\n%s", sessionevent.TurnStampPlaceholder, row)
	}
	if strings.Contains(row, time.Now().Format(sessionevent.TurnStampLayout)) {
		t.Fatalf("the header reached for the clock — a replayed session would print today:\n%s", row)
	}
}

// TestTurnHeaderWithoutAStampIsUnchanged — the compatibility arm: stamping is
// carried by an explicit bit, so a caller that never heard of it gets the bytes
// it always got. Without this, every existing header gained 18 cells and every
// geometry assertion in the suite became a coin flip.
func TestTurnHeaderWithoutAStampIsUnchanged(t *testing.T) {
	at := time.Date(2026, 11, 8, 12, 31, 20, 0, time.FixedZone("CST", 8*3600))
	plain := TurnHeaderLines(TurnInfo{Index: 5, Prompt: "same prompt"}, 120)
	// OpenedAt SET but Stamp UNSET: the value alone must change nothing. A
	// renderer that keyed on the time instead of the bit would print a moment
	// for a caller that never asked for one.
	withValue := TurnHeaderLines(TurnInfo{Index: 5, Prompt: "same prompt", OpenedAt: at}, 120)
	if len(plain) != len(withValue) {
		t.Fatalf("a stamp-less header rendered %d row(s), an identical one with an unused moment rendered %d",
			len(plain), len(withValue))
	}
	for i := range plain {
		if plain[i] != withValue[i] {
			t.Fatalf("row %d differs although neither call asked for a stamp:\nplain: %q\nvalue: %q",
				i, stripSGR(plain[i]), stripSGR(withValue[i]))
		}
	}
	want := "turn 5" + StatusSep + "same prompt"
	if row := stripSGR(plain[0]); !strings.Contains(row, want) {
		t.Fatalf("the unstamped label is %q, want it to carry %q", row, want)
	}
	if strings.Contains(stripSGR(plain[0]), sessionevent.TurnStampPlaceholder) {
		t.Fatalf("an unstamped header printed the placeholder:\n%s", stripSGR(plain[0]))
	}
	// And the stamp IS an addition, not a replacement: it takes cells the
	// prompt used to occupy. Measured on a WIDTH where the unstamped header
	// carries its prompt whole — comparing total row widths would prove nothing,
	// because the closing rule fills every row out to cols either way.
	// And the stamp IS an addition, not a replacement: it occupies cells in the
	// LABEL itself (measured on turnLabel, which is the label before any band
	// arithmetic — comparing rendered row widths would prove nothing, because the
	// closing rule fills every row out to cols either way, and comparing cut
	// positions depends on a width nobody wants to re-derive).
	plainLabel := turnLabel(TurnInfo{Index: 5, Prompt: "same prompt"})
	stampedLabel := turnLabel(TurnInfo{Index: 5, Prompt: "same prompt", OpenedAt: at, Stamp: true})
	if w0, w1 := displayWidth(plainLabel), displayWidth(stampedLabel); w1 <= w0 {
		t.Fatalf("the stamped label is not wider than the unstamped one (%d vs %d): %q / %q",
			w1, w0, stampedLabel, plainLabel)
	}
	// The addition is exactly the separator plus the moment's 19 cells: a label
	// that grew by anything else has changed the ordinal or the prompt too.
	if grew, want := displayWidth(stampedLabel)-displayWidth(plainLabel),
		displayWidth(sessionevent.TurnStampSep)+displayWidth(sessionevent.FormatTurnStamp(at)); grew != want {
		t.Fatalf("the stamp added %d cells, want %d (separator + moment): %q", grew, want, stampedLabel)
	}
}

// TestTurnStampCutKeepsTheMomentOnANarrowBand — at a width that cannot hold the
// prompt, the cut must fall on the PROMPT: an ordinal and a moment with a
// truncated ask is a usable landmark, while a moment cut in half states a time
// that does not exist.
func TestTurnStampCutKeepsTheMomentOnANarrowBand(t *testing.T) {
	at := time.Date(2026, 11, 8, 12, 31, 20, 0, time.FixedZone("CST", 8*3600))
	stamp := sessionevent.FormatTurnStamp(at)
	// A width that holds "turn 9 · <stamp>" and almost none of the prompt.
	const cols = 34
	if w := displayWidth("turn 9" + sessionevent.TurnStampSep + stamp); w > cols {
		t.Fatalf("the fixture width %d is smaller than the ordinal and moment alone (%d)", cols, w)
	}
	rows := TurnHeaderLines(TurnInfo{
		Index:    9,
		Prompt:   strings.Repeat("long ask ", 12),
		OpenedAt: at,
		Stamp:    true,
	}, cols)
	var band strings.Builder
	for _, r := range rows {
		if w := displayWidth(stripSGR(r)); w > cols {
			t.Fatalf("a header row is %d cells wide at cols=%d: %s", w, cols, stripSGR(r))
		}
		band.WriteString(stripSGR(r))
	}
	flat := band.String()
	// THE MOMENT SURVIVES IN FULL — as CONTIGUOUS text when the row can hold it,
	// and otherwise carried across the header's continuation rows (the block's
	// own wrap, which cuts the label and re-emits the remainder indented). The
	// assertion is therefore "every character is present, in order", not
	// "contiguous": a banner that dropped "12:31:20" would state no moment at
	// all, while a banner that wraps it still states the whole one.
	if idx := strings.Index(flat, stamp); idx < 0 {
		if drop := firstMissingInOrder(flat, stamp); drop >= 0 {
			t.Fatalf("the moment is not preserved across the header rows at cols=%d (missing from %q at byte %d):\n%s",
				cols, string(stamp[drop]), drop, flat)
		}
	}
	// THE ORDINAL MAY YIELD, AND AT THIS WIDTH IT MUST. `turn 9 · <19-cell
	// clock>` needs 29 cells of label room and a 34-column band leaves 20: the
	// moment takes them whole and the number goes, because a mangled clock is a
	// FALSE time while a missing number is recoverable from the status row and
	// from the record. Pinned here so the trade is a decision, not an accident.
	if !strings.Contains(flat, stamp) {
		t.Fatalf("the moment is not drawn whole at cols=%d:\n%s", cols, flat)
	}
	// At a width that CAN hold both, both are drawn — the ordinal is not
	// sacrificed for room it does not need to give.
	roomy := strings.Join(TurnHeaderLines(TurnInfo{
		Index: 9, Prompt: "p", OpenedAt: at, Stamp: true,
	}, 80), "\n")
	if !strings.Contains(roomy, "turn 9") || !strings.Contains(roomy, stamp) {
		t.Fatalf("at cols=80 the header dropped a field:\n%s", roomy)
	}
}

// firstMissingInOrder re-walks want over got and reports the index of the first
// byte of want that never appears in order (or -1 when all of them do). It is
// the diagnostic half of the wrap-tolerant assertion above: "wrapped" and "cut"
// look identical to strings.Contains, and only one of them is acceptable.
func firstMissingInOrder(got, want string) int {
	i := 0
	for k := 0; k < len(want); k++ {
		j := strings.IndexByte(got[i:], want[k])
		if j < 0 {
			return k
		}
		i += j + 1
	}
	return -1
}
