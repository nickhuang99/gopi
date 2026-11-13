package lineedit

import (
	"strings"
	"testing"
)

// takeover_test.go — the reserved layout's TAKEOVER: when the layout installs,
// does the screen it owns start CLEAN, or does it share the page with whatever
// the process (or the shell) wrote before it?
//
// WHY THIS NEEDED A NEW GUARD. Every other assertion in this package reads the
// band's own rows, and the band's rows are exactly the rows the layout REPAINTS
// — so a screen whose TOP rows were never touched passes every one of them. The
// defect is about the rows the layout does NOT repaint, which is why the
// measurement that found it (evidence/fill.raw, task 01a0fae7) had to drive a
// real pty: 40 filler lines were printed, `gopi chat` started, and the settled
// grid still carried `FILLER-LINE-39/40` plus the run's own two `[gopi]` startup
// notices at rows 0..4, with the box beginning at row 5. The screen was SHARED.
//
// The instrument is the same Screen the close-out guard reads (closeout_test.go):
// a grid the recorded bytes are replayed into, so "the takeover reached row 3" is
// a statement about cells rather than about a byte pattern.

// takeoverFiller is a row a PREVIOUS owner of the screen left behind: text no part
// of this surface ever draws, so finding it after the layout installed is finding
// the previous owner. It is written through the same recording writer the editor
// writes to, i.e. as the bytes a shell would have emitted before `gopi chat` ran.
const takeoverFiller = "PREVIOUS-OWNER-LINE-"

// takeoverPlant writes n filler rows to the recorded stream — the state a screen
// is in when a shell handed it to gopi.
func takeoverPlant(t *testing.T, h *a5Harness, n int) {
	t.Helper()
	for i := range n {
		if _, err := h.out.Write([]byte(takeoverFiller + string(rune('0'+i)) + "\r\n")); err != nil {
			t.Fatalf("planting filler row %d: %v", i, err)
		}
	}
}

// TestRegionEnableTakesOverTheScreen is the takeover's red case: the rows above
// the layout's first write are EMPTY on the grid once the layout has installed.
//
// RED PROOF: drop the escClearScreen from region.Enable and the assertion fires
// naming every filler row that survived — measured on the pre-fix tree at rows
// 0..4 (evidence/fill.raw replayed through this same Screen).
func TestRegionEnableTakesOverTheScreen(t *testing.T) {
	h := newA5Harness(t, nil, nil)
	// The harness has ALREADY installed the layout (EnableRegion). Take it down
	// and back up, with the previous owner's rows written in between: that is the
	// order a real run has (shell output, then the layout).
	h.ed.DisableRegion()
	takeoverPlant(t, h, 4)
	h.replay()
	for i := range 4 {
		line := takeoverFiller + string(rune('0'+i))
		if row := screenRowOf(h.screen, line); row < 0 {
			t.Fatalf("setup: %q is not on the screen before the layout installs — the test would prove nothing\n--- screen ---\n%s",
				line, h.screen.Contents())
		}
	}
	if !h.region.Enable(h.screenRows, h.screenCols) {
		t.Fatalf("Enable(%d rows) = false, want true", h.screenRows)
	}
	h.replay()
	for i := range 4 {
		line := takeoverFiller + string(rune('0'+i))
		if row := screenRowOf(h.screen, line); row >= 0 {
			t.Fatalf("after the layout installed the screen still carries %q at row %d — the layout SHARED the page instead of taking it over\n--- screen ---\n%s",
				line, row, h.screen.Contents())
		}
	}
}

// TestRegionEnableHomesTheCursorBeforeErasing pins the ORDER, which is the whole
// correctness of the takeover: ED erases FROM the cursor, so an erase emitted
// before the cursor is homed would leave every row above the parked cursor
// painted — the same half-erased screen the defect produced, arriving from the
// other direction.
//
// It is asserted on the recorded BYTES rather than on the grid because the grid
// cannot see the difference when the cursor already happens to be at the top: the
// order is the half a caller cannot check by looking.
func TestRegionEnableHomesTheCursorBeforeErasing(t *testing.T) {
	h := newA5Harness(t, nil, nil)
	h.ed.DisableRegion()
	// Park the terminal's cursor low, so the two orders are distinguishable: with
	// the cursor below the rows above it, an erase-first takeover leaves those
	// rows lit.
	takeoverPlant(t, h, 3)
	h.replay()
	before := h.out.count()
	if !h.region.Enable(h.screenRows, h.screenCols) {
		t.Fatal("Enable = false, want true")
	}
	h.replay()
	var wrote strings.Builder
	for _, w := range h.out.snapshot()[before:] {
		wrote.WriteString(string(w))
	}
	seq := wrote.String()
	home := strings.Index(seq, "\x1b[1;1H")
	erase := strings.Index(seq, "\x1b[2J")
	if home < 0 || erase < 0 {
		t.Fatalf("the takeover's own bytes are not both present (home=%d erase=%d) in %q", home, erase, seq)
	}
	if home > erase {
		t.Fatalf("the erase is emitted BEFORE the cursor is homed (erase=%d home=%d) — ED erases from the cursor, so the rows above it would stay painted",
			erase, home)
	}
	for i := range 3 {
		line := takeoverFiller + string(rune('0'+i))
		if row := screenRowOf(h.screen, line); row >= 0 {
			t.Fatalf("the filler row survived the takeover at row %d\n--- screen ---\n%s", row, h.screen.Contents())
		}
	}
}

// TestRegionEnableClearsTheScreenNotTheHistory: the takeover is ED(2), never
// ED(3). The scrollback is not this surface's to discard — the startup screen
// BELONGS in scrollback (region.go's own reason for anchoring the band at row 0,
// and the reason the historic one-liner is still readable above the box), so an
// erase that reached the history would delete the record the geometry exists to
// keep. ED(3) is what the operator's own note called out as the thing not to do.
func TestRegionEnableClearsTheScreenNotTheHistory(t *testing.T) {
	h := newA5Harness(t, nil, nil)
	h.ed.DisableRegion()
	before := h.out.count()
	if !h.region.Enable(h.screenRows, h.screenCols) {
		t.Fatal("Enable = false, want true")
	}
	h.replay()
	var wrote strings.Builder
	for _, w := range h.out.snapshot()[before:] {
		wrote.WriteString(string(w))
	}
	if strings.Contains(wrote.String(), "\x1b[3J") {
		t.Fatalf("the takeover emits ED(3) — it would erase the scrollback, which is the half of the screen the startup screen is supposed to LEAVE behind: %q",
			wrote.String())
	}
}
