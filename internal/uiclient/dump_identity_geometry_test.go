package uiclient

// dump_identity_geometry_test.go — the record's `terminal=` / `model=` /
// `grid=` are ONE geometry after a resize, and the sink is what moves them.
//
// THE MEASURED DEFECT (2026-11-09, a live `gopi chat` on a real pty). The dump
// header reported three geometries and two of them were frozen:
//
//	#gopi-chat-dump … terminal=121x24 model=121x24 grid=63x24  geometry=layout …
//	#gopi-chat-dump … terminal=121x24 model=121x24 grid=101x22 geometry=layout …
//
// `grid=` followed the pty across two resizes; `terminal=` and `model=` stayed
// at the CONSTRUCTOR's numbers for the whole run — in a line whose stated
// purpose (cmd/agent/chat_dump.go's Identity literal) was that the two numbers
// "are the same number BY CONSTRUCTION, so there is no second, staler geometry
// to disagree with." The three were equal at t0 and never again, and an agent
// reading the artifact was told the surface's model was stale when the layout
// had in fact re-wrapped at the pty's size on every frame.
//
// THE MECHANISM. provenanceLine renders `terminal=` from d.identity.
// TerminalColsRows and `model=` from d.identity.ColsRows; SetGeometry moved the
// GRID (d.screen) and the RAW leg (sgr) and never touched the identity. So the
// record had a live field and two dead ones, and no test asserted they agreed —
// every fixture in this package happened to construct the identity with the same
// numbers the grid got, which is exactly the shape that cannot catch it.
//
// These are the minimum reproductions: one per path of SetGeometry, because the
// identity is stamped on all of them (a fix that only handled the re-seed path
// would leave the "same size, new source" and "no layout geometry" paths dead,
// and those are the two the chat surface actually hits on a fallback terminal).

import (
	"os"
	"strings"
	"testing"
)

// identityGeomLine returns the record's provenance line out of the artifact the
// sink just installed — the LINE, not a substring of the whole file, so an
// assertion can say which field disagreed.
func identityGeomLine(t *testing.T, path string) string {
	t.Helper()
	blob, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	for _, line := range strings.Split(string(blob), "\n") {
		if strings.HasPrefix(line, RecordPrefixFor(SurfaceChat)) {
			return line
		}
	}
	t.Fatalf("no %q record line in the artifact:\n%s", RecordPrefixFor(SurfaceChat), blob)
	return ""
}

// assertOneGeometry fails unless the record states the SAME size in all three
// geometry fields, and states the expected one.
func assertOneGeometry(t *testing.T, line string, want string) {
	t.Helper()
	for _, field := range []string{"terminal=", "model=", "grid="} {
		wantField := field + want
		if !strings.Contains(line, wantField) {
			t.Errorf("record is missing %q — the three geometry fields disagree with each other or with the grid:\n%s",
				wantField, line)
		}
	}
}

// newChatIdentityDumper builds the sink the way `gopi chat` arms it: a CHAT
// identity whose two geometry fields start at the caller's PRE-LAYOUT hint,
// which the layout's own observer then overwrites.
func newChatIdentityDumper(t *testing.T, hintCols, hintRows int) (*ScreenDumper, string) {
	t.Helper()
	d, path := newTestDumper(t, DumpModeANSI, hintCols, hintRows)
	d.identity.Surface = SurfaceChat
	d.identity.Target = "self"
	return d, path
}

// TestDumpRecordGeometryFollowsTheGridOnResize is the defect's minimum
// reproduction: construct at one geometry, resize to another, then read the
// artifact's own header back.
//
// It fails on the pre-fix code with `terminal=121x24` and `model=121x24` beside
// `grid=101x22` — the measured line, byte for byte.
func TestDumpRecordGeometryFollowsTheGridOnResize(t *testing.T) {
	const (
		hintCols, hintRows = 121, 24 // the constructor's hint (the measured run)
		ptyCols, ptyRows   = 63, 24  // the first pty this run was 'attached' to
		newCols, newRows   = 101, 22 // after the resize the operator reported
	)
	d, path := newChatIdentityDumper(t, hintCols, hintRows)

	// The layout installs at the pty's size, then the terminal is resized.
	d.SetGeometry(GeometrySourceLayout, ptyCols, ptyRows)
	assertOneGeometry(t, identityGeomLine(t, path), "63x24")

	d.SetGeometry(GeometrySourceLayout, newCols, newRows)
	assertOneGeometry(t, identityGeomLine(t, path), "101x22")

	// And the grid really is that size — the assertion above is about the
	// RECORD, this one is about what the record is describing, so a fix that
	// stamped the identity without moving the grid cannot pass both.
	sd := d.Snapshot()
	if sd.Cols != newCols || sd.Rows != newRows {
		t.Fatalf("grid = %dx%d, want %dx%d: the record and the grid must be the same geometry", sd.Cols, sd.Rows, newCols, newRows)
	}
}

// TestDumpRecordGeometryFollowsTheGridOnSourceChange pins the path that does NOT
// re-size the grid: the layout re-announces the SAME size with a new authority
// (fallback → layout), which takes the early return. The identity still has to
// be stamped there, because that is the branch a chat run takes on its first
// frame after the grid was built from a fallback hint.
func TestDumpRecordGeometryFollowsTheGridOnSourceChange(t *testing.T) {
	d, path := newChatIdentityDumper(t, 80, 24)

	// The grid was never resized, but the LAYOUT now says it is 100x30 and
	// re-seeds it first — then announces the same 100x30 again (a repaint).
	d.SetGeometry(GeometrySourceLayout, 100, 30)
	d.SetGeometry(GeometrySourceFallback, 0, 0) // "no layout geometry to state"
	d.SetGeometry(GeometrySourceLayout, 100, 30)

	assertOneGeometry(t, identityGeomLine(t, path), "100x30")
	if d.identity.ColsRows != [2]int{100, 30} || d.identity.TerminalColsRows != [2]int{100, 30} {
		t.Fatalf("identity geometry = model %v / terminal %v, want both [100 30]",
			d.identity.ColsRows, d.identity.TerminalColsRows)
	}
}

// TestDumpRecordGeometryOnNoLayoutKeepsTheGridSize pins the `cols<=0` branch: it
// does not resize anything, so the honest record states the grid's OWN size on
// all three fields and lets `geometry=fallback` carry the "nothing measured a
// layout" fact — the geometry fields are sizes, never a verdict.
//
// This branch does not re-install the artifact by itself (it is a provenance
// change, and its sibling branch is the one that flushes), so the frame below is
// what makes the file say what the sink already holds: the assertion is about
// what a READER of the artifact gets, which is the only version of this claim
// that matters.
func TestDumpRecordGeometryOnNoLayoutKeepsTheGridSize(t *testing.T) {
	d, path := newChatIdentityDumper(t, 121, 24)

	d.SetGeometry(GeometrySourceLayout, 90, 20)
	d.SetGeometry(GeometrySourceFallback, 0, 0) // the layout was taken down
	if _, err := d.Write([]byte("x")); err != nil {
		t.Fatalf("Write: %v", err)
	}

	line := identityGeomLine(t, path)
	assertOneGeometry(t, line, "90x20")
	if !strings.Contains(line, "geometry="+string(GeometrySourceFallback)) {
		t.Errorf("record does not report geometry=%s after the layout was taken down:\n%s", GeometrySourceFallback, line)
	}
}
