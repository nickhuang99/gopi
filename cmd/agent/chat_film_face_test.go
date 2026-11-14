package main

// chat_film_face_test.go — THE BOUNDARY THIS ARM EXISTS FOR: the delivery cap is
// a fact about the terminal the film is shot on, and the defect it replaces was
// the same cap derived on a face that terminal does not have.
//
// The red-then-green the task was filed for (01a10603, measured 2026-10-04): with
// the cap derived from xterm's reference face (6x13 + a 4 px border, i.e. 82
// columns = 496 px), a film shot on this host's DEFAULT terminal — xfce4-terminal,
// 10x19 px per cell with a 16x27 px border, where 82 columns are 836 px — was
// released at a canvas the composer then refused: the cap let through a frame the
// delivery contract rejects, so the parity half of that contract was never
// reached on a real session. The fix is not a new number; it is asking the
// terminal.

import (
	"bytes"
	"strconv"
	"strings"
	"testing"

	"github.com/nickhuang99/gopi/internal/gifgen"
	"github.com/nickhuang99/gopi/internal/shot"
	"github.com/nickhuang99/gopi/internal/testutil"
)

// the faces this host's two terminals published (measured 2026-10-04 by reading
// WM_NORMAL_HINTS off their own windows — the readings are the fixtures, and the
// e2e probe in internal/shot re-takes them from live windows when a display is
// offered).
func filmFaceXFCE4(t *testing.T) shot.TerminalFace {
	t.Helper()
	return shot.TerminalFace{CellW: 10, CellH: 19, BorderW: 16, BorderH: 27, Measured: true, Window: 0x200003, Display: ":99"}
}

func filmFaceXterm(t *testing.T) shot.TerminalFace {
	t.Helper()
	return shot.TerminalFace{CellW: 6, CellH: 13, BorderW: 4, BorderH: 4, Measured: true, Window: 0x40000c, Display: ":99"}
}

// TestFilmFaceGapIsTheDefectReproduced is the RED half: the reference face's
// answer (the cap the arm used to apply) is compared with the real face's, and
// the gap is asserted to be the measured one — 82 columns are playable on xterm
// and OVER THE LIMIT on xfce4-terminal. If this ever passes as "the same
// number", the faces have been conflated again and the rest of the file proves
// nothing.
func TestFilmFaceGapIsTheDefectReproduced(t *testing.T) {
	const measuredXFCE4Width82Cols = 836
	const measuredXtermWidth82Cols = 496
	xfce4 := filmFaceXFCE4(t)
	xterm := filmFaceXterm(t)

	if got := xterm.WidthForCols(82); got != measuredXtermWidth82Cols {
		t.Fatalf("xterm's face puts 82 columns at %d px, want the measured %d", got, measuredXtermWidth82Cols)
	}
	if got := xfce4.WidthForCols(82); got != measuredXFCE4Width82Cols {
		t.Fatalf("xfce4-terminal's face puts 82 columns at %d px, want the measured %d", got, measuredXFCE4Width82Cols)
	}
	// The arithmetic the defect is made of: the reference count FITS on xterm and
	// does NOT fit on the terminal the film is actually shot in.
	referenceCols := filmPlayableMaxColsForFace(xterm)
	if got := xterm.WidthForCols(referenceCols); got > playableGIFMaxWidth {
		t.Fatalf("the reference count %d is %d px on xterm, which is over the %d px limit — the reference face is not the face this cap was derived from", referenceCols, got, playableGIFMaxWidth)
	}
	if got := referenceCols; got != filmReferenceMaxCols() {
		t.Fatalf("the reference face yields %d columns, want filmReferenceMaxCols() = %d", got, filmReferenceMaxCols())
	}
	if got := xfce4.WidthForCols(referenceCols); got <= playableGIFMaxWidth {
		t.Fatalf("the reference count %d is %d px on xfce4-terminal — the two faces are not distinguishable, so this test cannot reproduce the defect", referenceCols, got)
	}
}

// TestFilmCapFollowsTheMeasuredFace is the GREEN half, and it is the one that
// matters: given the face a run MEASURED, the cap's frame is inside the limit ON
// THAT FACE — never on the reference face.
func TestFilmCapFollowsTheMeasuredFace(t *testing.T) {
	for _, tc := range []struct {
		name string
		face shot.TerminalFace
	}{
		{"xfce4-terminal (this host's default)", filmFaceXFCE4(t)},
		{"xterm (the reference)", filmFaceXterm(t)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			max := filmPlayableMaxColsForFace(tc.face)
			if max <= 0 {
				t.Fatalf("no cell count is playable on %s", tc.face)
			}
			w := tc.face.WidthForCols(max)
			if w > playableGIFMaxWidth {
				t.Errorf("the cap is %d columns = %d px on %s, over the %d px limit", max, w, tc.face, playableGIFMaxWidth)
			}
			// The cap is the LARGEST count that fits: one more cell must be over.
			if next := tc.face.WidthForCols(max + 1); next <= playableGIFMaxWidth {
				t.Errorf("the cap is %d but %d columns is still only %d px on %s — the cap is not the largest count that fits", max, max+1, next, tc.face)
			}
		})
	}
}

// TestFilmCapOnTheRealFaceIsFarBelowTheReferenceCount states the size of the
// defect in the numbers a report quotes: on this host's default terminal the
// playable count is 48 columns, not the 82 the reference face implies — and 48
// columns are 496 px there, exactly the limit's neighbourhood the reference
// figure was mistaken for.
func TestFilmCapOnTheRealFaceIsFarBelowTheReferenceCount(t *testing.T) {
	const wantXFCE4Cols = 48
	xfce4 := filmFaceXFCE4(t)
	if got := filmPlayableMaxColsForFace(xfce4); got != wantXFCE4Cols {
		t.Fatalf("xfce4-terminal's cap = %d columns, want %d ((500-16)/10)", got, wantXFCE4Cols)
	}
	if got := filmReferenceMaxCols(); got != 82 {
		t.Fatalf("the reference face's cap = %d columns, want 82 ((500-4)/6)", got)
	}
	if got := filmPlayableMaxColsForFace(xfce4); got >= filmReferenceMaxCols() {
		t.Fatalf("the measured face's cap (%d) is not below the reference face's (%d) — the film would be shot at a canvas only xterm could play", got, filmReferenceMaxCols())
	}
}

// TestFilmPlayableGeometryUsesTheFaceItIsGiven is the composer's own boundary:
// the same resolved canvas, two faces, two different answers — and the notice
// says WHICH face moved the number, so a reader comparing two runs can tell them
// apart (Rule 15: a size change is reported with its cause).
func TestFilmPlayableGeometryUsesTheFaceItIsGiven(t *testing.T) {
	xfce4, xterm := filmFaceXFCE4(t), filmFaceXterm(t)
	const cols, rows = 82, 26

	var onXterm bytes.Buffer
	if gotCols, gotRows := chatFilmPlayableGeometry(xterm, cols, rows, &onXterm); gotCols != cols || gotRows != rows {
		t.Errorf("on xterm the 82-column canvas was changed to %dx%d — it is already inside the limit there", gotCols, gotRows)
	}
	if onXterm.Len() != 0 {
		t.Errorf("a canvas inside the limit on its own face was reported as narrowed: %s", onXterm.String())
	}

	var onXFCE4 bytes.Buffer
	gotCols, gotRows := chatFilmPlayableGeometry(xfce4, cols, rows, &onXFCE4)
	want := filmPlayableMaxColsForFace(xfce4)
	if gotCols != want || gotRows != rows {
		t.Fatalf("on xfce4-terminal the 82-column canvas became %dx%d, want %dx%d", gotCols, gotRows, want, rows)
	}
	notice := onXFCE4.String()
	for _, must := range []string{
		strconv.Itoa(cols),
		strconv.Itoa(xfce4.WidthForCols(cols)), // 836 — the frame as RESOLVED
		strconv.Itoa(want),
		strconv.Itoa(xfce4.WidthForCols(want)), // 496 — the frame as SHOT
		strconv.Itoa(playableGIFMaxWidth),
		"10x19", // the face the two figures are on
	} {
		if !strings.Contains(notice, must) {
			t.Errorf("the narrowing notice never mentions %s — the size change is not attributable to the face:\n%s", must, notice)
		}
	}
	// A face that was not measured cannot narrow anything: it states no cell size,
	// so the canvas is shot as resolved and the fallback is the caller's to say
	// (filmPlayableFaceFallback). A cap derived from 0 px cells would be infinite.
	var noFace bytes.Buffer
	unknown := shot.TerminalFace{}
	if gotCols, gotRows := chatFilmPlayableGeometry(unknown, cols, rows, &noFace); gotCols != cols || gotRows != rows {
		t.Errorf("an unmeasured face narrowed the canvas to %dx%d — a cap cannot be derived from a face that states no cell size", gotCols, gotRows)
	}
	if !strings.Contains(noFace.String(), "no face was measured for this run") {
		t.Errorf("an unmeasured face's fallback was not stated: %q", noFace.String())
	}
}

// TestFilmRequirePlayableNamesTheFaceAndTheFix is the refusal's contract: an
// over-wide frame is refused with the limit, the face it was too wide ON, and the
// column count that fits on that face — the three things a caller needs to shoot
// it again without guessing. The generic "shoot it narrower" is what this
// replaces.
func TestFilmRequirePlayableNamesTheFaceAndTheFix(t *testing.T) {
	defer func(prev shot.TerminalFace) { filmDeliveryFace = prev }(filmDeliveryFace)
	face := filmFaceXFCE4(t)
	filmDeliveryFace = face

	over := &gifgen.Result{Out: "film.gif", Width: face.WidthForCols(82), Height: 521, LoopCount: gifgen.LoopForever, LoopReadBack: gifgen.LoopForever}
	err := chatFilmRequirePlayable(over, false)
	if err == nil {
		t.Fatalf("an %d px wide animation was accepted on a face whose cap is %d columns", over.Width, filmPlayableMaxColsForFace(face))
	}
	msg := err.Error()
	for _, must := range []string{
		strconv.Itoa(over.Width),
		strconv.Itoa(playableGIFMaxWidth),
		strconv.Itoa(filmPlayableMaxColsForFace(face)),
		"10x19", // the face
		chatFlagCols,
	} {
		if !strings.Contains(msg, must) {
			t.Errorf("the over-wide refusal never mentions %s:\n%s", must, msg)
		}
	}
	// The fix it names is the one that WORKS: the columns it tells the caller to
	// use must produce a frame inside the limit on that same face.
	if got := face.WidthForCols(filmPlayableMaxColsForFace(face)); got > playableGIFMaxWidth {
		t.Errorf("the refusal tells the caller to shoot at %d columns, which is %d px on the face it names — over the limit it cites", filmPlayableMaxColsForFace(face), got)
	}
	// And with NO face measured, the refusal does not invent a column count for a
	// terminal it never saw: it says so and points at the run that would measure it.
	filmDeliveryFace = shot.TerminalFace{Why: "no terminal face was read in this process"}
	err = chatFilmRequirePlayable(over, false)
	if err == nil {
		t.Fatal("an over-wide animation was accepted with no face measured")
	}
	if !strings.Contains(err.Error(), "no terminal face was measured") {
		t.Errorf("a refusal composed without a face claims a column count anyway:\n%s", err.Error())
	}
}

// TestFilmPlayableNoColsIsARefusalNotAWidthOfZero is the degenerate pair: a face
// on which not even one cell fits the limit. Capping to zero columns would hand
// the producer a canvas of no width (the producer refuses such a canvas anyway,
// and the operator would read a width of 0 as an internal error), so it is
// refused with both numbers and both levers (Rule 15).
func TestFilmPlayableNoColsIsARefusalNotAWidthOfZero(t *testing.T) {
	defer func(prev shot.TerminalFace) { filmDeliveryFace = prev }(filmDeliveryFace)
	huge := shot.TerminalFace{CellW: playableGIFMaxWidth, CellH: 19, BorderW: playableGIFMaxWidth, BorderH: 27, Measured: true, Window: 0x1, Display: ":0"}
	if got := filmPlayableMaxColsForFace(huge); got != 0 {
		t.Fatalf("a face with a %d px border yields %d playable columns, want 0", huge.BorderW, got)
	}
	filmDeliveryFace = huge
	// explicit=false: the canvas is the driver's own resolution, so the delivery
	// cap applies — and on this face there is no playable count at all.
	cols, _, err := filmDeliveryCanvas(20, 26, false, &bytes.Buffer{})
	if err == nil {
		t.Fatalf("a face with no playable cell count returned a canvas of %d columns, want a refusal", cols)
	}
	for _, must := range []string{strconv.Itoa(playableGIFMaxWidth), "px per cell", chatFlagCols, chatFlagRows} {
		if !strings.Contains(err.Error(), must) {
			t.Errorf("the no-playable-count refusal never mentions %s:\n%s", must, err.Error())
		}
	}
}

// TestFilmFaceFixturesAreOutsideTheOperatorProfile is the Rule-32 half: this file
// reads no database and writes no state, so its only way to touch the operator's
// append-only ledger would be a test that resolved one — asserted rather than
// assumed.
func TestFilmFaceFixturesAreOutsideTheOperatorProfile(t *testing.T) {
	dir := t.TempDir()
	inProfile, err := testutil.PathLiesInOperatorProfile(dir)
	if err != nil {
		// The predicate itself could not answer: reported, never treated as "no"
		// (Rule 15 — an unreadable check is not a pass).
		t.Fatalf("resolve whether %s lies in the operator's profile: %v", dir, err)
	}
	if inProfile {
		t.Fatalf("t.TempDir() = %s resolves inside the operator's profile — a fixture written here would be permanent", dir)
	}
}
