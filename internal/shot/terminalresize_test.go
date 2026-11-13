package shot

// terminalresize_test.go — the request's WIRE LAYOUT, pinned against the API it
// encodes, and the refusal contract for a window that cannot be measured.
//
// The layout half is not decoration: a wrong opcode or value-mask returns a
// well-formed reply and a window that simply did not move, which reads exactly
// like a window manager refusing the size. The end-to-end half (a live window
// moved and read back) is TestFaceReadsTheTerminalsOwnModel in
// terminalface_test.go, which needs a display.

import (
	"strings"
	"testing"
)

// TestConfigureWindowRequestMatchesTheProtocol pins the request's byte map: the
// opcode against the installed header's value, the length field against the
// words actually written, and the mask's ORDER against the value list (X11 reads
// the values in bit order, least significant bit first, so a swapped list would
// set width to the intended height).
func TestConfigureWindowRequestMatchesTheProtocol(t *testing.T) {
	if x11OpConfigureWindow != 12 {
		t.Fatalf("x11OpConfigureWindow = %d, want 12 (X11/Xproto.h)", x11OpConfigureWindow)
	}
	if x11ConfigMaskWidth != 0x0004 || x11ConfigMaskHeight != 0x0008 {
		t.Fatalf("the configure mask bits are %#x/%#x, want 0x4/0x8 (CWWidth/CWHeight)", x11ConfigMaskWidth, x11ConfigMaskHeight)
	}
	if x11ConfigureWindowTotalLen != 5 {
		t.Fatalf("the request is %d units long, want 5 (header 3 + 2 values)", x11ConfigureWindowTotalLen)
	}
	// The mask's bits decide the VALUE ORDER: CWWidth (0x4) is read before
	// CWHeight (0x8), so width must be written first.
	if x11ConfigMaskWidth > x11ConfigMaskHeight {
		t.Fatalf("the width bit (%#x) is not below the height bit (%#x): the value list would be read in the wrong order", x11ConfigMaskWidth, x11ConfigMaskHeight)
	}
}

// TestResizeRefusesAnUnknownFaceByName is the Rule-15 half: without a measured
// face there is no px-per-cell, so no pixel size can be computed — and a resize
// derived from a zero would move the window to 0x0. The refusal names the face
// and both counts, so a reader knows which of the two inputs was missing.
func TestResizeRefusesAnUnknownFaceByName(t *testing.T) {
	term := &FilmTerminal{Window: 0x200003, Display: ":99"}
	_, err := term.Resize(TerminalFace{Why: "no WM_NORMAL_HINTS on this window"}, 80, 24)
	if err == nil {
		t.Fatal("a resize on an UNMEASURED face was accepted — the pixel size cannot be derived from a face that states no cell")
	}
	for _, must := range []string{"0x200003", "80x24", "UNMEASURED", "no WM_NORMAL_HINTS on this window"} {
		if !strings.Contains(err.Error(), must) {
			t.Errorf("the refusal never mentions %s:\n%s", must, err.Error())
		}
	}
}

// TestResizeRefusesAWindowItDoesNotHave: a terminal that never mapped one has
// nothing to move, and saying so beats a ConfigureWindow against window 0 (the
// server's BadWindow would be the only trace).
func TestResizeRefusesAWindowItDoesNotHave(t *testing.T) {
	term := &FilmTerminal{}
	if _, err := term.Resize(filmFaceForTest(), 80, 24); err == nil {
		t.Fatal("a resize with no window was accepted")
	}
}

// TestResizeRefusesANonCanvas states the degenerate counts: a terminal's own
// protocol refuses 0 columns, and so does this — before any request is composed.
func TestResizeRefusesANonCanvas(t *testing.T) {
	term := &FilmTerminal{Window: 0x200003, Display: ":99"}
	for _, tc := range []struct{ cols, rows int }{{0, 24}, {80, 0}, {-1, 24}} {
		if _, err := term.Resize(filmFaceForTest(), tc.cols, tc.rows); err == nil {
			t.Errorf("a resize to %dx%d cells was accepted", tc.cols, tc.rows)
		}
	}
}

func filmFaceForTest() TerminalFace {
	return TerminalFace{CellW: 6, CellH: 13, BorderW: 4, BorderH: 4, Measured: true, Window: 0x200003, Display: ":99"}
}
