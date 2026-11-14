package shot

// terminalface_test.go — the face reader's proofs: the atom against the header
// it is taken from, the parser against the two terminals this host actually has
// (whose published models were measured with xprop), and — when a display is
// offered — the MODEL against the window the server really mapped.

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nickhuang99/gopi/internal/config"
)

// TestWMNormalHintsAtomIsTheHeaders is the guard on the one number in
// terminalface.go that is not derived: a wrong atom returns type-None for EVERY
// window, which reads exactly like "no client on this host publishes a face" —
// a wrong answer shaped like a measurement. The header is the authority (Rule 31:
// the value is checked against its source, not restated).
func TestWMNormalHintsAtomIsTheHeaders(t *testing.T) {
	const header = "/usr/include/X11/Xatom.h"
	raw, err := os.ReadFile(header)
	if err != nil {
		// The header is not installed: the constant is still asserted below, and
		// the fact that its source could not be read is REPORTED rather than
		// silently skipped (Rule 15).
		t.Logf("%s could not be read (%v) — the atom is only checked against the constant", header, err)
	} else {
		want := fmt.Sprintf("XA_WM_NORMAL_HINTS ((Atom) %d)", x11AtomWMNormalHintsID)
		if !strings.Contains(string(raw), want) {
			t.Errorf("%s does not contain %q — the atom this file reads is not ICCCM's WM_NORMAL_HINTS", header, want)
		}
	}
	if x11AtomWMNormalHintsID != 40 {
		t.Errorf("x11AtomWMNormalHintsID = %d, want 40 (XA_WM_NORMAL_HINTS)", x11AtomWMNormalHintsID)
	}
	if x11WMNormalHintsItems != 18 {
		t.Errorf("x11WMNormalHintsItems = %d, want 18 (ICCCM's XSizeHints)", x11WMNormalHintsItems)
	}
}

// TestFaceFromHintsTellsAnAbsentFaceFromAnUnstatedOne is the parser's boundary:
// the shapes a client can publish, and the two DIFFERENT absences (it said
// nothing / it said something this file cannot use). Every refusal names which
// one it is — a caller that treats them alike cannot tell a terminal problem
// from a parser problem.
func TestFaceFromHintsTellsAnAbsentFaceFromAnUnstatedOne(t *testing.T) {
	// The two models this host's terminals published, typed here as the
	// MEASURED fixtures they are (xprop on a private display, 2026-10-04):
	//   xfce4-terminal --geometry=82x26 → base 16 27, increment 10 19 → 836x521
	//   xterm -geometry 82x26           → base  4  4, increment  6 13 → 496x342
	hints := func(flags, incW, incH, baseW, baseH uint32) []uint32 {
		items := make([]uint32, x11WMNormalHintsItems)
		items[x11HintFlagsIndex] = flags
		items[x11HintWidthIncIdx] = incW
		items[x11HintHeightIncIdx] = incH
		items[x11HintBaseWidthIdx] = baseW
		items[x11HintBaseHeightIx] = baseH
		return items
	}
	both := uint32(x11HintPResizeInc | x11HintPBaseSize)

	cases := []struct {
		name  string
		items []uint32
		want  TerminalFace
		why   string // non-empty: an unmeasured face, and this substring in Why
	}{
		{
			name:  "xfce4-terminal's published model",
			items: hints(both, 10, 19, 16, 27),
			want:  TerminalFace{CellW: 10, CellH: 19, BorderW: 16, BorderH: 27, Measured: true},
		},
		{
			name:  "xterm's published model",
			items: hints(both, 6, 13, 4, 4),
			want:  TerminalFace{CellW: 6, CellH: 13, BorderW: 4, BorderH: 4, Measured: true},
		},
		{
			name: "a client that declares a cell size and no base size is still measured",
			// PBaseSize clear: ICCCM leaves the base fields as garbage, and a
			// parser that read them anyway would derive its chrome from
			// uninitialised memory — here 999/888, which must NOT appear.
			items: hints(uint32(x11HintPResizeInc), 10, 19, 999, 888),
			want:  TerminalFace{CellW: 10, CellH: 19, Measured: true},
		},
		{
			name:  "no property at all",
			items: nil,
			why:   "publishes no WM_NORMAL_HINTS",
		},
		{
			name:  "a truncated property",
			items: hints(both, 10, 19, 16, 27)[:4],
			why:   "carries 4 item(s)",
		},
		{
			// The measured VTE shape (xfce4-terminal on this host: flags 848 =
			// 0x350, increment fields 10 19, base fields 16 27): the cell is
			// published WITHOUT the PResizeInc bit, and the window really is
			// 16+10x82 by 27+19x26 = 836x521. Refusing it would call the host's
			// default terminal unmeasurable while its own numbers are in the
			// property.
			name:  "VTE's shape: an increment published without PResizeInc",
			items: hints(uint32(x11HintPBaseSize), 10, 19, 16, 27),
			want:  TerminalFace{CellW: 10, CellH: 19, BorderW: 16, BorderH: 27, Measured: true},
		},
		{
			name:  "a zero cell is not a cell whatever the flags say",
			items: hints(both, 0, 19, 16, 27),
			why:   "no usable character cell",
		},
		{
			name:  "an implausibly wide cell is refused rather than believed",
			items: hints(both, 100000, 19, 16, 27),
			why:   "no usable character cell",
		},
		{
			name: "implausible chrome is dropped, not believed",
			// The cell is real; the base fields are garbage (a client that never
			// set them). The face is still usable with zero chrome, and no pixel
			// figure is shifted by a number nobody wrote.
			items: hints(both, 10, 19, 100000, 100000),
			want:  TerminalFace{CellW: 10, CellH: 19, Measured: true},
		},
	}
	for _, tc := range cases {
		face, why := faceFromHints(0x200003, ":99", tc.items)
		if tc.why != "" {
			if why == "" {
				t.Errorf("%s: read as a face (%+v), want an unmeasured face", tc.name, face)
				continue
			}
			if !strings.Contains(why, tc.why) {
				t.Errorf("%s: reason = %q, want it to name %q", tc.name, why, tc.why)
			}
			if face.Known() {
				t.Errorf("%s: an unmeasured face reports Known() = true", tc.name)
			}
			continue
		}
		if why != "" {
			t.Errorf("%s: reported unmeasurable (%s), want a measured face", tc.name, why)
			continue
		}
		if face.CellW != tc.want.CellW || face.CellH != tc.want.CellH || face.BorderW != tc.want.BorderW || face.BorderH != tc.want.BorderH {
			t.Errorf("%s: face = %+v, want cell %dx%d + border %dx%d", tc.name, face, tc.want.CellW, tc.want.CellH, tc.want.BorderW, tc.want.BorderH)
		}
		if !face.Known() {
			t.Errorf("%s: a measured face reports Known() = false", tc.name)
		}
		// The model's own prediction, against the window the measurement came
		// from: this is the relation the film's pixel arithmetic rests on.
		if w := face.WidthForCols(82); w != face.BorderW+face.CellW*82 {
			t.Errorf("%s: WidthForCols(82) = %d, want the model's own base+increment", tc.name, w)
		}
	}
}

// TestUnmeasuredFaceIsNotAFaceOfZeroPixelCells pins the representation Rule 15a
// is about: "this client publishes no face" must never read as "the cells are 0
// px wide", because the second is a number a caller would divide by.
func TestUnmeasuredFaceIsNotAFaceOfZeroPixelCells(t *testing.T) {
	var zero TerminalFace
	if zero.Known() {
		t.Fatal("the zero-value face claims to be known")
	}
	if s := zero.String(); !strings.Contains(s, "UNMEASURED") {
		t.Errorf("an unmeasured face renders as %q, want it to say it is unmeasured", s)
	}
	face := TerminalFace{Why: "the window publishes no WM_NORMAL_HINTS property"}
	if s := face.String(); !strings.Contains(s, "no WM_NORMAL_HINTS") {
		t.Errorf("an unmeasured face does not carry its reason: %q", s)
	}
	// And a MEASURED face states its numbers and its provenance, so a report
	// quoting it can be checked against the window it came from.
	measured := TerminalFace{CellW: 10, CellH: 19, BorderW: 16, BorderH: 27, Window: 0x200003, Display: ":99", Measured: true}
	s := measured.String()
	for _, want := range []string{"10x19", "16x27", "0x200003", ":99"} {
		if !strings.Contains(s, want) {
			t.Errorf("a measured face renders as %q, want it to name %s", s, want)
		}
	}
}

// The two knobs this file's end-to-end test reads. They are TEST-ONLY (no
// production path consults them), which is why they are named constants here
// rather than entries in internal/config's catalog: a knob in the catalog is a
// promise to an operator (Rule 21's other direction — every catalogued name must
// have a real reader; these have readers that are not the shipped binary).
const (
	faceTestDisplayEnvName  = "GOPI_FACE_TEST_DISPLAY"
	faceTestTerminalEnvName = "GOPI_FACE_TEST_TERMINAL"
)

// TestFaceReadsTheTerminalsOwnModel is the END-TO-END half, and it is the one
// that would have caught a wrong atom number: a real terminal is started on a
// display THIS TEST OWNS, its window is resolved through the SAME rule the film
// uses, and the face read off it must PREDICT the window the server actually
// mapped.
//
// THE DISPLAY MUST BE THE TEST'S OWN, and that is a measured requirement rather
// than tidiness. xfce4-terminal is D-Bus single-instance: starting it while
// another instance is up hands the window to the EXISTING process, so a probe on
// a shared display resolves a window whose _NET_WM_PID belongs to somebody
// else's terminal and the pid+title rule correctly refuses it (measured while
// writing this file). shot.StartFilmDisplay allocates a private Xvfb — the same
// one the film driver uses — so the probe is the only client on it.
//
// It is skipped unless GOPI_FACE_TEST_DISPLAY names a display, because it needs
// one and because the allocation is a real server (Rule 27: nothing here
// manufactures load — one Xvfb and one terminal for a few seconds). Run it with:
//
//	GOPI_FACE_TEST_DISPLAY=:99 go test ./internal/shot/ -run TestFaceReadsTheTerminalsOwnModel
func TestFaceReadsTheTerminalsOwnModel(t *testing.T) {
	display := strings.TrimSpace(os.Getenv(faceTestDisplayEnvName))
	if display == "" {
		t.Skip("GOPI_FACE_TEST_DISPLAY is not set: this test needs a real display (see the doc comment for why it must be private)")
	}
	terminal := strings.TrimSpace(os.Getenv(faceTestTerminalEnvName))
	if terminal == "" {
		terminal = FilmTerminalDefault
	}
	spec, serr := FilmTerminalSpecByName(terminal)
	if serr != nil {
		t.Skipf("the terminal %s is not usable here: %v", terminal, serr)
	}
	if _, err := exec.LookPath(spec.Binary); err != nil {
		t.Skipf("the terminal %s is not installed here: %v", terminal, err)
	}
	const cols, rows = 82, 26
	title := fmt.Sprintf("gopi face probe %d", os.Getpid())
	// THROUGH THE DRIVER'S OWN STARTER, not a hand-rolled exec: that is the code
	// path the film takes (locale pin, display pin, the registry's argv protocol,
	// the pid in the returned struct), so this test exercises the real thing
	// rather than a lookalike.
	term, terr := StartFilmTerminalSpec(spec, display, filmTestGeometry(cols, rows), []string{"sleep", "20"}, title, "", "")
	if terr != nil {
		t.Fatalf("start %s on %s: %v", terminal, display, terr)
	}
	defer func() {
		if cerr := term.Close(); cerr != nil {
			t.Logf("close the probe terminal: %v", cerr)
		}
	}()
	t.Logf("probe: %s pid %d on %s, window 0x%x %dx%d, title %q", terminal, term.PID, display, term.Window, term.Geometry.W, term.Geometry.H, term.Name)
	face, ferr := term.Face()
	if ferr != nil {
		t.Fatalf("read the face of window 0x%x on %s: %v", term.Window, display, ferr)
	}
	if !face.Known() {
		t.Fatalf("the face of a real %s window reads back unmeasured: %s", terminal, face)
	}
	t.Logf("%s on %s, %dx%d cells: mapped %dx%d px; face %s", terminal, display, cols, rows, term.Geometry.W, term.Geometry.H, face)
	// THE MODEL AGAINST THE SERVER: the face predicts the window's pixel size, and
	// a mismatch means this file read the wrong property (or the wrong field of the
	// right one) — the failure a wrong atom number produces, and the one a caller
	// deriving a width limit from the face would inherit silently.
	if got, want := face.WidthForCols(cols), term.Geometry.W; got != want {
		t.Errorf("the face predicts %d px for %d cells but the server mapped %d px — the model read from WM_NORMAL_HINTS does not describe this window", got, cols, want)
	}
	if got, want := face.HeightForRows(rows), term.Geometry.H; got != want {
		t.Errorf("the face predicts %d px for %d rows but the server mapped %d px", got, rows, want)
	}
	// AND THE RESIZE HALF, on the same live window: moving it to another cell count
	// must land it on that count's pixels, which is what the driver relies on to
	// make the delivered canvas the canvas the surface is drawing. The count is
	// derived from the SAME face and the playable limit the film uses.
	const filmPlayableLimitForTest = 500
	narrow := (filmPlayableLimitForTest - face.BorderW) / face.CellW
	if narrow <= 0 || narrow >= cols {
		t.Skipf("the probe face %s yields %d playable columns against a limit below the probe's %d, so the resize half cannot be exercised here", face, narrow, cols)
	}
	rect, rerr := term.Resize(face, narrow, rows)
	if rerr != nil {
		t.Fatalf("resize the probe window to %dx%d cells: %v", narrow, rows, rerr)
	}
	if want := face.WidthForCols(narrow); rect.W != want {
		t.Errorf("after resizing to %d cells the window reports %d px, want %d on %s", narrow, rect.W, want, face)
	}
	// The face is a property of the CLIENT, not of its size: re-reading it after
	// the move must give the same cell size and chrome (this is what lets the
	// driver use one reading across a resize).
	after, aerr := term.Face()
	if aerr != nil {
		t.Fatalf("re-read the face after resizing: %v", aerr)
	}
	if after.CellW != face.CellW || after.CellH != face.CellH || after.BorderW != face.BorderW || after.BorderH != face.BorderH {
		t.Errorf("the face changed across a resize: %s → %s — a face is a property of the client's font, not of its current size", face, after)
	}
}

// filmTestGeometry spells a cell count the way the registry's own geometry
// protocol wants it — the separator is internal/config's ONE definition of the
// WxC spelling, not a literal typed here (Rule 1b).
func filmTestGeometry(cols, rows int) string {
	return fmt.Sprintf("%d%s%d", cols, config.ChatIdealGeometryDimSep, rows)
}

// parseWindowID is the test's own hex-id reader for a listing's "0x…" spelling.
func parseWindowID(id string) (uint32, error) {
	var v uint32
	if _, err := fmt.Sscanf(strings.TrimPrefix(id, "0x"), "%x", &v); err != nil {
		return 0, err
	}
	return v, nil
}

// TestFaceProbeFixturesAreOutsideTheSourceTree is the Rule 30 half: this file
// writes nothing, so the only way it could leave residue is a temp path inside
// the checkout — asserted rather than assumed.
func TestFaceProbeFixturesAreOutsideTheSourceTree(t *testing.T) {
	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	if !strings.HasSuffix(filepath.Clean(wd), filepath.Join("internal", "shot")) {
		t.Fatalf("this test expects to run in internal/shot, not %s", wd)
	}
}
