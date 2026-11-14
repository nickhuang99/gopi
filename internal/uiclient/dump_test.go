package uiclient

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/nickhuang99/gopi/internal/lineedit"
)

// newTestDumper builds a sink writing under t.TempDir() with a clock the test
// drives itself, so no assertion depends on wall time.
//
// The path is hand-rolled here on purpose: this is a RUNTIME file the surface
// writes for an observer, not a ledger, so the Rule 32 database scaffold does
// not apply — there is no sqlite open and no Gopi database in sight.
func newTestDumper(t *testing.T, mode DumpMode, cols, rows int) (*ScreenDumper, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "ui-screen.dump")
	d, err := NewScreenDumper(DumpConfig{
		Path: path,
		Mode: mode,
		Cols: cols,
		Rows: rows,
		Identity: DumpIdentity{
			PID:              4242,
			Target:           "http://127.0.0.1:8123",
			Tools:            true,
			ColsRows:         [2]int{cols, rows},
			TerminalColsRows: [2]int{cols, rows},
		},
	})
	if err != nil {
		t.Fatalf("NewScreenDumper: %v", err)
	}
	base := time.Unix(1_700_000_000, 0)
	// The helper's clock ADVANCES on every read, past the refresh interval
	// each time: a test asserting CONTENT does not want to also be asserting
	// the throttle, so the throttle is exercised by the one test that sets its
	// own fixed clock.
	ticks := 0
	d.now = func() time.Time {
		ticks++
		return base.Add(time.Duration(ticks) * (dumpMinInterval + time.Millisecond))
	}
	// The constructor's own first flush used the real clock; re-anchor so the
	// first Write is measured against this clock.
	d.last = base
	return d, path
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(b)
}

// TestScreenDumperInterpretsTheRenderStream pins the core claim: the sink is a
// real observer of the render stream — the bytes bubbletea paints become a cell
// grid whose rows, columns and cursor a test can assert as TEXT.
func TestScreenDumperInterpretsTheRenderStream(t *testing.T) {
	d, path := newTestDumper(t, DumpModeANSI, 32, 4)

	// Frames placed the way a full-screen TUI places them: absolute cursor
	// addresses, no reliance on the wipe edge (a real renderer diffs its own
	// previous frame, so it pads rather than clears).
	frames := []string{
		"\x1b[1;1Hgopi ui \u2192 http://127.0.0.1:8123",
		"\x1b[2;1Hyou> \x1b[7mhello\x1b[0m",
		"\x1b[4;1Hidle \u00b7 turns 1 \u00b7 tool events 0",
		"\x1b[2;1Hyou> \x1b[7mhello\x1b[0m",
	}
	for _, f := range frames {
		if _, err := d.Write([]byte(f)); err != nil {
			t.Fatalf("Write: %v", err)
		}
	}

	snap := d.Snapshot()
	cases := []struct {
		name string
		got  string
		want string
	}{
		{"header row", snap.Lines[0], "gopi ui \u2192 http://127.0.0.1:8123"},
		{"transcript row", snap.Lines[1], "you> hello"},
		{"empty row between them", snap.Lines[2], ""},
		{"status row", snap.Lines[3], "idle \u00b7 turns 1 \u00b7 tool events 0"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if tc.got != tc.want {
				t.Errorf("row = %q, want %q", tc.got, tc.want)
			}
		})
	}
	if snap.Cursor.Row != 1 || snap.Cursor.Col != 10 {
		t.Errorf("cursor = (%d,%d), want (1,10): the caret sits after the painted input", snap.Cursor.Row, snap.Cursor.Col)
	}
	if len(snap.Grid) != 4 || len([]rune(snap.Grid[0])) != 32 {
		t.Errorf("grid = %d rows, first row %d cells, want 4 rows x 32 cells",
			len(snap.Grid), len([]rune(snap.Grid[0])))
	}
	// The attribute export reaches the file too: "you> " is default and the
	// painted input is reverse video, which is a fact about a COLUMN RANGE.
	if len(snap.AttrRuns[1]) < 2 || snap.AttrRuns[1][1].Col != 5 || snap.AttrRuns[1][1].Style != "reverse" {
		t.Errorf("AttrRuns[1] = %+v, want a reverse-video run starting at column 5", snap.AttrRuns[1])
	}

	// The file on disk is the same observation, readable without the process.
	file := readFile(t, path)
	for _, want := range []string{
		"#gopi-ui-dump pid=4242",
		"target=http://127.0.0.1:8123",
		"tools=on",
		"grid=32x4",
		"mode=ansi",
		"#gopi-screen-dump v=1 cols=32 rows=4",
		"cursor row=1 col=10",
		"001|0:5=default 5:5=reverse 10:22=default",
	} {
		if !strings.Contains(file, want) {
			t.Errorf("dump file is missing %q\n--- file ---\n%s", want, file)
		}
	}
}

// TestScreenDumperPlainMode pins the alternative interpretation: with no escape
// handling a row is literally the characters between newlines, which is what a
// reader wants from a captured pipe — and the mode is reported, so a reader is
// never left guessing which rendering it has.
func TestScreenDumperPlainMode(t *testing.T) {
	d, path := newTestDumper(t, DumpModePlain, 24, 3)
	if _, err := d.Write([]byte("plain line one\nplain line two\n")); err != nil {
		t.Fatalf("Write: %v", err)
	}

	if got := d.Snapshot().Lines[0]; got != "plain line one" {
		t.Errorf("plain row = %q, want %q", got, "plain line one")
	}
	if got := readFile(t, path); !strings.Contains(got, "mode=plain") {
		t.Errorf("dump does not report the mode in force:\n%s", got)
	}
	if snap := d.Snapshot(); snap.Lines[1] != "plain line two" || snap.Lines[2] != "" {
		t.Errorf("plain rows = %q / %q, want the two lines on the first two rows",
			snap.Lines[1], snap.Lines[2])
	}
}

// TestScreenDumperThrottlesTheRenderPath pins the cost bound: the render path
// pays for a snapshot at most once per dumpMinInterval, an unchanged frame is
// never rewritten, and the in-memory grid still reflects every byte fed.
func TestScreenDumperThrottlesTheRenderPath(t *testing.T) {
	d, _ := newTestDumper(t, DumpModeANSI, 20, 2)

	base := time.Unix(1_700_000_000, 0)
	// The constructor installs the file immediately (an observer must be able
	// to tell "the surface has not painted yet" from "there is no dump").
	if got := d.Writes(); got != 1 { //screen-ok: measures the install COUNT after construction — no screen is being produced here, and no row is being awaited

		t.Fatalf("after construction dumps = %d, want the initial file", got)
	}

	// INSIDE the interval: the bytes reach the grid, the FILE does not change.
	d.now = func() time.Time { return base }
	if _, err := d.Write([]byte("\x1b[1;1Hfirst")); err != nil {
		t.Fatalf("Write: %v", err)
	}
	if got := d.Writes(); got != 1 { //screen-ok: measures that the throttle SUPPRESSED an install — an unchanged frame is byte-identical on disk, so the counter is the only observable
		t.Errorf("a write inside the interval installed a dump (dumps=%d, want 1)", got)
	}
	if got := d.Snapshot().Lines[0]; got != "first" {
		t.Errorf("a snapshot inside the interval missed its own bytes: row = %q, want %q", got, "first")
	}

	// PAST the interval with changed content: the file refreshes.
	d.now = func() time.Time { return base.Add(dumpMinInterval + time.Millisecond) }
	if _, err := d.Write([]byte("\x1b[1;1Hsecon")); err != nil {
		t.Fatalf("Write: %v", err)
	}
	if got := d.Writes(); got != 2 { //screen-ok: measures the install the interval EXPIRY earned — the assertion's subject is the install count, asserted synchronously after a Write returned
		t.Fatalf("a write past the interval did not install a dump (dumps=%d, want 2)", got)
	}
	if got := d.Snapshot().Lines[0]; got != "secon" {
		t.Errorf("snapshot row = %q, want %q", got, "secon")
	}

	// PAST the interval with an IDENTICAL rendering: no rewrite at all — the
	// steady state of an idle surface costs a comparison, not two renames.
	// (The write itself is harmless: it repaints the same cells.)
	d.now = func() time.Time { return base.Add(2 * (dumpMinInterval + time.Millisecond)) }
	if _, err := d.Write([]byte("\x1b[1;1Hsecon")); err != nil {
		t.Fatalf("Write: %v", err)
	}
	if got := d.Writes(); got != 2 { //screen-ok: measures the unchanged-rendering SUPPRESSION (an identical body is not rewritten) — the counter is the only observable, the file is byte-identical either way
		t.Errorf("an unchanged rendering was rewritten (dumps=%d, want 2)", got)
	}
}

// TestScreenDumperIdentityAndJSONTwin pins the two halves an agent needs to
// trust a file it found: WHICH surface wrote it, and a machine-parseable twin.
func TestScreenDumperIdentityAndJSONTwin(t *testing.T) {
	d, path := newTestDumper(t, DumpModeANSI, 12, 2)
	if _, err := d.Write([]byte("\x1b[1;1Hhi")); err != nil {
		t.Fatalf("Write: %v", err)
	}

	raw := readFile(t, d.JSONPath())
	var got struct {
		Identity DumpIdentity `json:"identity"`
		Observer jsonFields   `json:"observer"`
		Screen   struct {
			Cols  int                   `json:"cols"`
			Rows  int                   `json:"rows"`
			Lines []string              `json:"lines"`
			Blind []lineedit.ScreenDump `json:"-"`
		} `json:"screen"`
	}
	if err := json.Unmarshal([]byte(raw), &got); err != nil {
		t.Fatalf("the JSON twin does not parse: %v\n%s", err, raw)
	}
	if got.Identity.PID != 4242 || got.Identity.Target != "http://127.0.0.1:8123" {
		t.Errorf("identity = %+v, want pid 4242 against the target", got.Identity)
	}
	if !got.Identity.Tools {
		t.Error("identity lost the tools switch")
	}
	if got.Screen.Cols != 12 || got.Screen.Rows != 2 || got.Screen.Lines[0] != "hi" {
		t.Errorf("screen = %dx%d %q, want 12x2 starting with %q", got.Screen.Cols, got.Screen.Rows, got.Screen.Lines, "hi")
	}
	if got.Observer.Dumps < 1 {
		t.Errorf("observer.Dumps = %d, want at least 1", got.Observer.Dumps)
	}
	if got.Observer.Frames < 1 {
		t.Errorf("observer.Frames = %d, want at least 1", got.Observer.Frames)
	}
	if _, err := os.Stat(path); err != nil {
		t.Errorf("the text dump beside the JSON twin is missing: %v", err)
	}
}

// TestScreenDumperInstallIsAtomicAndLeavesNoLitter pins the two filesystem
// properties a reader depends on: a dump is never observable half-written, and
// the installer cleans up after itself (a stray .tmp in the operator's
// directory would be litter produced by the observer).
func TestScreenDumperInstallIsAtomicAndLeavesNoLitter(t *testing.T) {
	d, path := newTestDumper(t, DumpModeANSI, 16, 2)
	base := time.Unix(1_700_000_000, 0)
	for i, frame := range []string{"aaaa", "bbbb", "cccc"} {
		d.now = func() time.Time { return base.Add(time.Duration(i+1) * (dumpMinInterval + time.Millisecond)) }
		if _, err := d.Write([]byte("\x1b[1;1H" + frame)); err != nil {
			t.Fatalf("Write: %v", err)
		}
	}

	entries, err := os.ReadDir(filepath.Dir(path))
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	var names []string
	for _, e := range entries {
		names = append(names, e.Name())
		if strings.Contains(e.Name(), ".tmp") {
			t.Errorf("a temporary file survived the install: %s", e.Name())
		}
	}
	if len(names) != 2 {
		t.Errorf("directory holds %v, want exactly the dump and its JSON twin", names)
	}

	// The dump is complete, never truncated: the body is the full grid.
	body := readFile(t, path)
	if !strings.Contains(body, "grid_begin count=2\n") || !strings.HasSuffix(body, "blind_spots_end\n") {
		t.Errorf("the dump is not a complete rendering:\n%s", body)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("Stat: %v", err)
	}
	if perm := info.Mode().Perm(); perm != dumpFilePerm {
		t.Errorf("dump file mode = %o, want %o", perm, dumpFilePerm)
	}
}

// TestScreenDumperWithoutAPathIsSnapshotOnly pins the off-file mode: the sink
// still observes, and it does not invent a path to write to (a file nobody
// named is a file nobody can find).
func TestScreenDumperWithoutAPathIsSnapshotOnly(t *testing.T) {
	d, err := NewScreenDumper(DumpConfig{Cols: 10, Rows: 2})
	if err != nil {
		t.Fatalf("NewScreenDumper: %v", err)
	}
	if d.Path() != "" || d.JSONPath() != "" {
		t.Errorf("paths = %q / %q, want none", d.Path(), d.JSONPath())
	}
	if _, err := d.Write([]byte("\x1b[1;1Hx")); err != nil {
		t.Fatalf("Write: %v", err)
	}
	if got := d.Snapshot().Lines[0]; got != "x" {
		t.Errorf("snapshot row = %q, want %q", got, "x")
	}
	if got := d.Writes(); got != 0 { //screen-ok: measures that the off-file mode installs NOTHING — with no path there is no file to assert on, the counter IS the measurement
		t.Errorf("dumps = %d, want 0 with no path", got)
	}
}

// TestScreenDumperRejectsAnUnknownMode pins Rule 15 at the constructor: a mode
// nobody implements is refused by name rather than silently reinterpreted.
func TestScreenDumperRejectsAnUnknownMode(t *testing.T) {
	if _, err := NewScreenDumper(DumpConfig{Mode: DumpMode("jsonl")}); err == nil {
		t.Fatal("an unknown dump mode was accepted")
	}
}
