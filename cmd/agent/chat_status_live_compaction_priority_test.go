package main

import (
	"strings"
	"testing"

	"github.com/nickhuang99/gopi/internal/lineedit"
)

// TestStatusLiveCompactionCounterOutranksTheWidestStateWord is the regression guard
// for the operator's own defect report — "when idle, it shows `⧉30 · ready` … when
// request is sent, the `ready` becomes `responding`, it will hide the compaction
// number" — read as the priority it states: the compaction count outranks how long
// the state word is, so the block gives up the word's WIDTH (a short spelling) rather
// than the counter.
//
// THE FIXTURE IS THE DEFECT (and the test fails if it stops being one): the counter
// and the widest state word do NOT fit the reserved block together, measured through
// the production composer's own width unit — "⧉30 · responding" is 15 cells against
// the 13 statusLiveCol reserves. If that arithmetic ever changes, the assertion at
// the end of this test fails rather than the test passing for a reason it no longer
// exercises (Rule 31: the surface is executed, and a green test must be green about
// the thing it names).
func TestStatusLiveCompactionCounterOutranksTheWidestStateWord(t *testing.T) {
	const compactions = 30
	ops := statusOps{compressionsKnown: true, compressions: compactions}
	counter := statusCompactionLabel(compactions)

	// The fixture is the defect: the counter plus the FULL widest word does not fit.
	if got := statusFieldCells(counter + lineedit.StatusSep + statusLiveWordResponding); got <= statusLiveCol {
		t.Fatalf("the fixture no longer describes the defect: %q is %d cells, inside the reserved %d — "+
			"choose counters/words that still collide, or this guard proves nothing",
			counter+lineedit.StatusSep+statusLiveWordResponding, got, statusLiveCol)
	}

	// EVERY state a turn walks, at EVERY animation frame the keepalive advances: the
	// counter is on the block at all of them. The frames matter because a frame is a
	// real repaint (the animation rides behind the word), so a spelling that only fits
	// on frame zero would blink the counter away once per keepalive.
	states := []struct{ name, word string }{
		{"idle", statusLiveIdle},
		{"sent", statusLiveWordSent},
		{"thinking", statusLiveWordThinking},
		{"responding", statusLiveWordResponding},
	}
	for _, st := range states {
		for frame := 0; frame < liveHintFrameCount; frame++ {
			hint := st.word + strings.Repeat(statusLiveAnimationChar, frame)
			got := statusLiveField(hint, "", ops)
			if !strings.Contains(got, counter) {
				t.Errorf("the %s state at animation frame %d drops the compaction counter: %q — the "+
					"counter reports the event that changed what the model still remembers, and the "+
					"width of a state word must not be able to hide it", st.name, frame, got)
			}
			if !strings.Contains(got, st.name) && st.name != "idle" {
				// The word may be shown SHORT ("resp"), never absent: the two facts are
				// on the block together or the fix has traded one silent field for
				// another.
				if !strings.Contains(got, statusLiveWordRespondingShort) &&
					!strings.Contains(got, statusLiveWordThinkingShort) &&
					!strings.Contains(got, statusLiveWordSent) && !strings.Contains(got, statusLiveIdle) {
					t.Errorf("the %s state's word is absent from the block %q", st.name, got)
				}
			}
			if w := statusFieldCells(got); w > statusLiveCol {
				t.Errorf("the %s block is %d cells, over the %d the row reserves: %q", st.name, w, statusLiveCol, got)
			}
		}
	}

	// THE MECHANISM, not only the outcome: the widest state is shown in its SHORT
	// spelling, and that spelling's width is what makes the pair fit. A fix that
	// reached the assertion above by widening the column or by cutting the word would
	// pass the loop and fail here — a cut word is not a spelling.
	wide := statusLiveField(statusLiveWordResponding, "", ops)
	if !strings.Contains(wide, statusLiveWordRespondingShort) {
		t.Fatalf("the widest state is not shown in its short spelling on the block %q — the counter "+
			"survived some other way than the one this fix implements", wide)
	}
	if strings.Contains(wide, statusLiveResponding) {
		t.Errorf("the FULL word %q is still on the block %q beside the counter, so the block is not "+
			"inside its reservation", statusLiveResponding, wide)
	}
	if got, want := statusFieldCells(counter+lineedit.StatusSep+statusLiveWordRespondingShort), 11; got != want {
		t.Errorf("the short combination %q is %d cells, want %d — the fixture's arithmetic moved",
			counter+lineedit.StatusSep+statusLiveWordRespondingShort, got, want)
	}
}

// TestStatusLiveStateWordsAreNamedConstantsAndStatedAlternatives ties the short
// spellings to the table the composer actually walks (Rule 1/13/14): a state's
// alternatives are defined in ONE place, a state with no short form answers with
// itself alone (so the caller's loop has one shape and the old fallback — the counter
// is dropped — stays reachable), and the animation cells the hint carries are
// preserved across the substitution for the same reason they exist (a frame must be
// a new row, not a lost one).
func TestStatusLiveStateWordsAreNamedConstantsAndStatedAlternatives(t *testing.T) {
	// A state WITH a short spelling: full first, short second, and the short one is
	// the named constant rather than a hand-built string.
	got := statusLiveStateWords(statusLiveWordResponding)
	if len(got) != 2 || got[0] != statusLiveWordResponding || got[1] != statusLiveWordRespondingShort {
		t.Fatalf("statusLiveStateWords(responding) = %q, want [%q %q]",
			got, statusLiveWordResponding, statusLiveWordRespondingShort)
	}
	if got := statusLiveStateWords(statusLiveWordThinking); len(got) != 2 || got[1] != statusLiveWordThinkingShort {
		t.Errorf("statusLiveStateWords(thinking) = %q, want the thinking short form %q",
			got, statusLiveWordThinkingShort)
	}
	// A state with NO short spelling answers with itself alone — "sent" and "ready"
	// already fit beside a counter (9 and 11 cells), so a shorter spelling of them
	// would be a second name for one string.
	for _, full := range []string{statusLiveWordSent, statusLiveIdle} {
		if got := statusLiveStateWords(full); len(got) != 1 || got[0] != full {
			t.Errorf("statusLiveStateWords(%q) = %q, want the single spelling %q", full, got, full)
		}
	}
	// The animation rides BEHIND the word and is carried over unchanged by the
	// substitution: dropping it would make the keepalive's frame a repaint that draws
	// the same text, which the renderer deduplicates away.
	animated := statusLiveWordResponding + statusLiveAnimationChar
	alt := statusLiveStateWords(animated)
	if len(alt) != 2 || alt[1] != statusLiveWordRespondingShort+statusLiveAnimationChar {
		t.Errorf("statusLiveStateWords(%q) = %q, want the animated short form %q",
			animated, alt, statusLiveWordRespondingShort+statusLiveAnimationChar)
	}
}

// TestStatusLiveShortWordsFitTheReservedColumn is the geometry tie for the SHORT
// spellings, the twin of TestStatusLiveWordsFitTheirColumn: the block is a fixed
// reservation, so every word the composer can put in it — full or short — must fit,
// with its animation behind it. A short spelling wider than the reservation would
// move the row's tail and re-open the defect the reservation exists to close.
func TestStatusLiveShortWordsFitTheReservedColumn(t *testing.T) {
	words := map[string]string{
		"responding (short)": statusLiveWordRespondingShort,
		"thinking (short)":   statusLiveWordThinkingShort,
	}
	for name, word := range words {
		if w := displayCellsForTest(word); w > statusLiveCol {
			t.Errorf("the %s word %q is %d cells, over the reserved live column's %d", name, word, w, statusLiveCol)
		}
		widest := word + strings.Repeat(statusLiveAnimationChar, liveHintFrameCount-1)
		if w := displayCellsForTest(widest); w > statusLiveCol {
			t.Errorf("the %s word at its widest (word + %d animation cell(s)) is %d cells, over the reserved %d",
				name, liveHintFrameCount-1, w, statusLiveCol)
		}
	}
}
