package uiclient

// blankunpainted_test.go — the RED-PROOF + regression guard for the measured
// chat defect: a grid that is ALL-BLANK while the sink reports frames>0.
//
// THE MEASUREMENT THIS PINS (a real 117x26 pty, `gopi chat --dump-screen`, no
// input at all, 2026-09-26): the record line reads
//
//	#gopi-chat-dump pid=… target=self:chat tools=off terminal=117x26
//	 model=117x26 grid=117x26 geometry=layout mode=ansi frames=3 dumps=1
//
// and does not change again — 17 samples over 10 s gave byte-identical headers
// and an unchanged mtime. The sink is not broken: the layout paints only when
// the screen CHANGES, so an idle surface has nothing more to write. The defect is
// the READER's: `frames=3 dumps=1` with an all-blank grid was reported as a
// readable live screen, and a --compare against it answered "not in the grid" —
// manufacturing a paint loss out of a screen that was never painted.
//
// RED PROOF: the first assertion below is the one that FAILS before the change
// (`frames` is not the authority — the grid is) and passes after it. The last
// test drives the real reader/compare surfaces end to end on the same fixture.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/nick/gopi/internal/lineedit"
)

// blankIdleDump builds the MEASURED artifact: a real dump whose sink interpreted
// frames (never a hand-typed record — the fixture uses the product's own
// writer), and whose grid carries nothing in any cell.
//
// The three writes are what a chat layout does on the way to an idle screen
// (install + chrome), and each one is pushed past the throttle by the sink's own
// clock, exactly as writeFixtureDump does — the test is about what a READER
// concludes, not about the refresh rate.
func blankIdleDump(t *testing.T, path string, frames int) string {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", filepath.Dir(path), err)
	}
	d, err := NewScreenDumper(DumpConfig{
		Path: path,
		Cols: 117,
		Rows: 26,
		Identity: DumpIdentity{
			PID:              os.Getpid(),
			Target:           "self:chat",
			Tools:            false,
			ColsRows:         [2]int{117, 26},
			TerminalColsRows: [2]int{117, 26},
			Started:          selfStamp(t),
		},
	})
	if err != nil {
		t.Fatalf("NewScreenDumper(%s): %v", path, err)
	}
	fake := int64(0)
	d.now = func() time.Time {
		fake++
		return time.Unix(1_800_000_000+fake*3600, 0)
	}
	// A layout installs, paints its chrome, then sits idle: the bytes an idle
	// chat really sends are cursor placement + style resets on an empty screen.
	// Every one of them counts as a FRAME (the sink counts writes), and none of
	// them puts a printable cell on the grid.
	for i := 0; i < frames; i++ {
		if _, err := d.Write([]byte("\x1b[2J\x1b[H\x1b[0m\x1b[1;1H\x1b[0m")); err != nil {
			t.Fatalf("write fixture dump (%d): %v", i, err)
		}
	}
	if err := d.Close(); err != nil {
		t.Fatalf("Close(%s): %v", path, err)
	}
	return path
}

// TestBlankGridWithFramesIsNotAScreen is the RED PROOF.
//
// BEFORE the change: a `frames=3`, all-blank grid passed every readability check
// the reader had (`Unpainted()` is `frames == 0`, and the geometry here IS the
// layout's), so the reader rendered it as a live screen and the compare answered
// against it. AFTER: the grid is asked directly, and a blank one is refused.
func TestBlankGridWithFramesIsNotAScreen(t *testing.T) {
	dir := t.TempDir()
	path := blankIdleDump(t, filepath.Join(dir, DumpFileName), 3)

	cand, err := ReadDump(path)
	if err != nil {
		t.Fatalf("ReadDump(%s): %v", path, err)
	}
	// The measured header, asserted as measured: `frames=3 dumps=1`, geometry
	// from the layout — i.e. this fixture is the artifact the bug was filed on,
	// not a convenient stand-in.
	if cand.Frames != 3 {
		t.Fatalf("frames = %d, want the measured 3 (this fixture must BE the filed artifact)", cand.Frames)
	}
	if cand.Unpainted() {
		t.Fatal("frames=3 must NOT read as unpainted — that is `598aa37a`'s frames=0 case, and conflating the two would erase the distinction this change adds")
	}
	if GeometryOf(cand) != GeometrySourceTerminal {
		t.Fatalf("geometry = %q, want %q: the filed chat artifact was written before the layout took the screen over (its install re-derives the grid and makes the dump write happen ahead of the banner), so the refusal must be about the GRID, not the geometry", GeometryOf(cand), GeometrySourceTerminal)
	}
	// THE ASSERTION THAT WAS RED: frames>0 is not the question. The grid is.
	if !cand.BlankUnpainted() {
		t.Fatal("an all-blank 117x26 grid with frames=3 must read as blank-unpainted — this is the measured chat artifact, and reading it as a screen is the filed defect")
	}
	// And the predicate must not be a check that always fires.
	painted, err := ReadDump(writeFixtureDump(t, filepath.Join(dir, "painted", DumpFileName), os.Getpid(), selfStamp(t),
		"self:chat", "gopi chat → painted row"))
	if err != nil {
		t.Fatalf("ReadDump(painted): %v", err)
	}
	if painted.BlankUnpainted() {
		t.Fatalf("a grid carrying %q must not read as blank", painted.Dump.Lines[0])
	}
	if refusal := CompareGridUnusable(painted); refusal != "" {
		t.Errorf("a painted dump at the layout's geometry must be comparable, got refusal %q", refusal)
	}
}

// TestCompareRefusesABlankGridInsteadOfAnswering is the second half of the RED
// proof, on the surface an agent actually calls: BEFORE, `ui_state --compare`
// answered "not in the grid" (a paint loss) for a line on a grid that was never
// painted; AFTER, the compare is REFUSED (exit 1) with the blank sentence as its
// reason, so "I could not check" is never dressed up as "the paint lost it".
func TestCompareRefusesABlankGridInsteadOfAnswering(t *testing.T) {
	dir := t.TempDir()
	path := blankIdleDump(t, filepath.Join(dir, DumpFileName), 3)
	cand, err := ReadDump(path)
	if err != nil {
		t.Fatalf("ReadDump(%s): %v", path, err)
	}
	line := "the answer line the agent believes it saw"
	v := CompareTranscriptLine(line, cand, []string{line})
	if v.Status != CompareRefused {
		t.Fatalf("status = %q, want %q — answering a blank grid reports the reader's own emptiness as a paint loss", v.Status, CompareRefused)
	}
	if v.Reason != BlankUnpaintedNotice {
		t.Fatalf("reason = %q, want the blank-grid notice %q", v.Reason, BlankUnpaintedNotice)
	}
	// The frames=0 sentence must survive untouched (`598aa37a`): its own
	// fixture still refuses with ITS reason, never with the new one.
	zeroDir := filepath.Join(dir, "zero")
	if err := os.MkdirAll(zeroDir, 0o755); err != nil {
		t.Fatal(err)
	}
	unpainted, err := NewScreenDumper(DumpConfig{
		Path: filepath.Join(zeroDir, DumpFileName), Cols: 20, Rows: 4,
		Identity: DumpIdentity{PID: os.Getpid(), ColsRows: [2]int{20, 4}, TerminalColsRows: [2]int{20, 4}, Started: selfStamp(t)},
	})
	if err != nil {
		t.Fatalf("NewScreenDumper(zero): %v", err)
	}
	zero, err := ReadDump(unpainted.Path())
	if err != nil {
		t.Fatalf("ReadDump(zero): %v", err)
	}
	if !zero.Unpainted() {
		t.Fatalf("a dump with no Write at all must read as unpainted, got frames=%d", zero.Frames)
	}
	if got := CompareGridUnusable(zero); got != UnpaintedNotice {
		t.Fatalf("frames=0 refusal = %q, want the frames=0 notice %q — the new case must not overwrite the old one", got, UnpaintedNotice)
	}
}

// TestBlankUnpaintedPredicateAsksTheGrid pins the predicate's own contract: it
// is a statement about CELLS, never about trimmed lines or the frame counter, and
// it must not read a row whose content sits at its right edge as blank.
func TestBlankUnpaintedPredicateAsksTheGrid(t *testing.T) {
	if strings.Contains(BlankUnpaintedNotice, UnpaintedNotice) {
		t.Error("the blank notice must be its own sentence, not the frames=0 one repeated")
	}
	if !strings.Contains(strings.ToLower(BlankUnpaintedNotice), "not evidence") {
		t.Errorf("the blank notice must DENY the empty-screen reading, got %q", BlankUnpaintedNotice)
	}
	// A grid of blanks: blank.
	blank := lineedit.NewScreen(8, 2).Dump()
	if !ScreenDumpAllBlank(blank) {
		t.Error("a fresh screen must read as all-blank")
	}
	// A grid with one printable cell in the LAST column of a row: NOT blank —
	// the trimmed Lines form reports that row as empty, which is exactly the
	// reading this predicate must not use.
	right := lineedit.NewScreen(8, 2)
	right.Feed([]byte("\x1b[1;8HX"))
	if ScreenDumpAllBlank(right.Dump()) {
		t.Error("a cell carrying content at the row's right edge must not read as blank")
	}
	if !ScreenDumpAllBlank(blank) {
		t.Error("the blank screen changed under a second question")
	}
	if !ScreenDumpAllBlank(lineedit.ScreenDump{}) {
		t.Error("a dump with no grid cells carries no content: reading it as non-blank would let an empty artifact speak as a screen (the assertion documents the choice)")
	}
}

// TestBlankGridIsNotReadableAsARealEmptyScreen is the READER-level regression
// guard: the same artifact, rendered by the reader's own text path, must carry
// the qualification ABOVE the grid it qualifies — so a report (or an agent) that
// quotes only the first lines of its output still learns that the grid is blank
// rather than believing the surface is empty.
//
// This is the assertion the field bug failed: the artifact WAS blank, the reader
// said nothing about it, and the agent believed the screen was empty.
func TestBlankGridIsNotReadableAsARealEmptyScreen(t *testing.T) {
	dir := t.TempDir()
	path := blankIdleDump(t, filepath.Join(dir, DumpFileName), 3)
	cand, err := ReadDump(path)
	if err != nil {
		t.Fatalf("ReadDump(%s): %v", path, err)
	}
	// The predicate the reader's text path branches on, asserted as the reader
	// asks it: frames OR the grid, never frames alone.
	blankWithFrames := !cand.Unpainted() && cand.BlankUnpainted()
	if !blankWithFrames {
		t.Fatalf("frames=%d blank=%t — the measured idle artifact must take the reader's blank-with-frames branch",
			cand.Frames, cand.BlankUnpainted())
	}
	// And the sentence that branch prints must deny the empty-screen reading by
	// name, exactly as the frames=0 sentence does (a reader greps for it).
	if !strings.Contains(strings.ToLower(BlankUnpaintedNotice), "all-blank") {
		t.Errorf("the notice must name the ALL-BLANK grid, got %q", BlankUnpaintedNotice)
	}
	// The dump body it qualifies must really be empty: 26 rows, none with
	// content. If that stops being true the fixture has drifted away from the
	// measured artifact and this test would pass vacuously.
	rowsWithContent := 0
	for _, row := range cand.Dump.Lines {
		if strings.TrimSpace(row) != "" {
			rowsWithContent++
		}
	}
	if rowsWithContent != 0 {
		t.Fatalf("the fixture grid carries content on %d rows; it must be the measured all-blank artifact", rowsWithContent)
	}
	if cand.Dump.Rows != 26 || cand.Dump.Cols != 117 {
		t.Fatalf("grid = %dx%d, want the measured 117x26", cand.Dump.Cols, cand.Dump.Rows)
	}
}
