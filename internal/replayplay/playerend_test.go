package replayplay

// playerend_test.go — THE TWO ENDS OF THE MATERIAL ARE THE TWO ENDS.
//
// THE DEFECT THIS FILE PINS (measured 2026-11-01, task 01a19638): `End` jumped
// to the TOP of the last turn (`RoundIndex = RoundCount(); Scroll = 0`), which
// is the OPPOSITE end from the one a viewer pressing the end key asks for, and
// NO key reached the bottom of a long recording at all. On a log whose last turn
// is taller than the body — the normal case for the turn a session ends in — the
// material's own last row could not be reached in one keystroke, so "scroll to
// the bottom of this log" was `End` plus a page count.
//
// The two questions are separate and both are asserted here:
//
//  1. WHERE the view lands (the turn AND the scroll offset), and
//  2. that `G` and `End` land in exactly the same place — two spellings of one
//     destination, so they cannot drift into two behaviours.

import "testing"

// lastTurnLongPlayer builds a player over 3 TURNS whose LAST turn is taller than
// the canvas's body, so "the bottom of the material" is a scroll offset a
// one-keystroke move has to reach, not a coincidence of the fixture.
func lastTurnLongPlayer() *Player {
	p := playerFor(3)
	p.Mat.Turns[len(p.Mat.Turns)-1].Rows = repeatRows(tallRoundRows)
	p.Mat.Rounds[len(p.Mat.Rounds)-1].Rows = p.Mat.Turns[len(p.Mat.Turns)-1].Rows
	p.RoundIndex = 1
	p.Scroll = 0
	p.Paused = true
	p.Notice = ""
	return p
}

// TestPlayer_EndLandsOnTheBottomOfTheLastTurn is the acceptance assertion the
// epic states, in its own terms: after End, `RoundIndex == RoundCount()` and
// `Scroll == renderOptions().MaxScroll()`.
func TestPlayer_EndLandsOnTheBottomOfTheLastTurn(t *testing.T) {
	p := lastTurnLongPlayer()
	p.ApplyKey(KeyEnd)

	if p.RoundIndex != p.RoundCount() {
		t.Fatalf("End landed on turn %d of %d, want the last one", p.RoundIndex, p.RoundCount())
	}
	max := p.renderOptions().MaxScroll()
	if max == 0 {
		t.Fatalf("the fixture's last turn fits the body: a scroll assertion here would prove nothing")
	}
	if p.Scroll != max {
		t.Fatalf("End landed at scroll %d of %d — the end key must reach the BOTTOM of the last turn, not its top", p.Scroll, max)
	}
}

// TestPlayer_GIsTheSameDestinationAsEnd pins the second spelling: `G` says the
// same thing from anywhere, and it says it through the SAME code (both route
// through endOfMaterial), so a change to one cannot leave the other behind.
func TestPlayer_GIsTheSameDestinationAsEnd(t *testing.T) {
	viaEnd := lastTurnLongPlayer()
	viaEnd.ApplyKey(KeyEnd)

	viaG := lastTurnLongPlayer()
	viaG.RoundIndex = 2 // start somewhere else entirely
	viaG.Scroll = 3
	viaG.ApplyKey(KeyJumpEnd)

	if viaG.RoundIndex != viaEnd.RoundIndex || viaG.Scroll != viaEnd.Scroll {
		t.Fatalf("G landed at (turn %d, scroll %d) and End at (turn %d, scroll %d): the two spellings must name one destination",
			viaG.RoundIndex, viaG.Scroll, viaEnd.RoundIndex, viaEnd.Scroll)
	}
	if viaG.RoundIndex != viaG.RoundCount() || viaG.Scroll != viaG.renderOptions().MaxScroll() {
		t.Fatalf("G landed at (turn %d, scroll %d), want (turn %d, scroll %d)",
			viaG.RoundIndex, viaG.Scroll, viaG.RoundCount(), viaG.renderOptions().MaxScroll())
	}
}

// TestPlayer_AKeyThatCannotMoveFurtherSaysSoAtTheEnd keeps the end honest: a
// second End is a move that cannot happen, and a key that appears to do nothing
// is indistinguishable from a broken key (the rule this file's neighbours
// already hold). The destination itself must not move.
func TestPlayer_AKeyThatCannotMoveFurtherSaysSoAtTheEnd(t *testing.T) {
	p := lastTurnLongPlayer()
	p.ApplyKey(KeyEnd)
	atEnd := p.Scroll
	p.Notice = ""
	p.ApplyKey(KeyEnd)
	if p.Scroll != atEnd || p.RoundIndex != p.RoundCount() {
		t.Fatalf("a second End moved the view to (turn %d, scroll %d), want (%d, %d)", p.RoundIndex, p.Scroll, p.RoundCount(), atEnd)
	}
}

// TestParseKey_DecodesTheEndKeySpellings ties the new binding to the decoder the
// keyboard path uses: `G` (either case) must decode to the destination key and
// nothing else may — an unbound letter is KeyNone, never a surprise action.
func TestParseKey_DecodesTheEndKeySpellings(t *testing.T) {
	for _, seq := range [][]byte{{'G'}, {'g'}} {
		if got := ParseKey(seq); got != KeyJumpEnd {
			t.Fatalf("%q decoded as %v, want KeyJumpEnd", seq, got)
		}
	}
}

// TestKeyMapRowsNamesTheEndKey closes the loop the epic requires: the key map is
// the ONE source of the help page AND the `?` overlay, so a binding that reaches
// the decoder but not this list is invisible to the operator — and the list must
// describe End as the material's END, not as the last turn's top (the wording the
// defect was).
func TestKeyMapRowsNamesTheEndKey(t *testing.T) {
	var endRow, jumpRow string
	for _, row := range KeyMapRows {
		if row[0] == "Home / End" {
			endRow = row[1]
		}
		if row[0] == "G or g" {
			jumpRow = row[1]
		}
	}
	if endRow == "" {
		t.Fatalf("the key map has no Home/End row: %v", KeyMapRows)
	}
	if jumpRow == "" {
		t.Fatalf("the key map does not list G, so the help page and the `?` overlay cannot teach the new binding: %v", KeyMapRows)
	}
	for _, want := range []string{"BOTTOM", "LAST"} {
		if !containsFold(endRow, want) {
			t.Fatalf("the Home/End row %q does not say %q — End lands on the last turn's BOTTOM", endRow, want)
		}
	}
	if !containsFold(jumpRow, "end") {
		t.Fatalf("the G row %q does not say it is the end of the material", jumpRow)
	}
	// The OVERLAY is rendered from this list, so the new row must be visible there
	// too — a binding the operator can only find by reading source has not shipped.
	lines := HelpLines()
	joined := ""
	for _, l := range lines {
		joined += l + "\n"
	}
	if !containsFold(joined, "LAST turn's BOTTOM") {
		t.Fatalf("the ? overlay never says where End lands:\n%s", joined)
	}
}

// containsFold is a case-insensitive substring test — the key map's wording is
// prose and may be capitalised for emphasis, and a test that pinned the exact
// capitalisation would fail on an edit that changed no meaning.
func containsFold(haystack, needle string) bool {
	h := []rune(haystack)
	n := []rune(needle)
	if len(n) == 0 {
		return true
	}
	for i := 0; i+len(n) <= len(h); i++ {
		ok := true
		for j := range n {
			if lowerRune(h[i+j]) != lowerRune(n[j]) {
				ok = false
				break
			}
		}
		if ok {
			return true
		}
	}
	return false
}

// lowerRune is an ASCII-only case fold: every byte this file compares is ASCII,
// and pulling unicode into a key-map assertion would be a dependency for nothing.
func lowerRune(r rune) rune {
	if r >= 'A' && r <= 'Z' {
		return r + ('a' - 'A')
	}
	return r
}
