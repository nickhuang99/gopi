package replayplay

// stream_test.go — the VERBATIM LEG's proof.
//
// The arms that matter:
//   - a recording is found under BOTH suffixes (the pre-rename one still reads),
//     and the CURRENT one wins when a directory holds both;
//   - a loaded stream yields steppable frames whose rows are the painted screen,
//     at the RECORDING's own grid — not the caller's, not the terminal's;
//   - the sampling policy keeps the first frame and the tail, and SAYS it sampled;
//   - a session with no recording is the ORDINARY path (no error, no invented
//     artifact), `--from stream` REFUSES instead of falling back, and a stream
//     that exists but cannot be painted is reported rather than swallowed.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nickhuang99/gopi/internal/uiclient"
)

// recordStream writes one real recording at cols×rows and returns its path. It
// uses the production writer (OpenSgrStream + dumper), so the fixture's grammar is
// the grammar a run leaves on disk.
func recordStream(t *testing.T, dir, name string, cols, rows int, paints [][]byte) string {
	t.Helper()
	path := filepath.Join(dir, name+uiclient.ScreenStreamSuffix)
	s, err := uiclient.OpenSgrStream(uiclient.SgrStreamConfig{Path: path, Cols: cols, Rows: rows})
	if err != nil {
		t.Fatalf("OpenSgrStream: %v", err)
	}
	d, err := uiclient.NewScreenDumper(uiclient.DumpConfig{
		Path: filepath.Join(dir, name+"-chat-screen.dump"),
		Cols: cols,
		Rows: rows,
		Sgr:  s,
		Identity: uiclient.DumpIdentity{
			Surface:          uiclient.SurfaceChat,
			PID:              11,
			ColsRows:         [2]int{cols, rows},
			TerminalColsRows: [2]int{cols, rows},
		},
	})
	if err != nil {
		t.Fatalf("NewScreenDumper: %v", err)
	}
	for _, p := range paints {
		if _, werr := d.Write(p); werr != nil {
			t.Fatalf("Write: %v", werr)
		}
	}
	if cerr := d.Close(); cerr != nil {
		t.Fatalf("Close: %v", cerr)
	}
	return path
}

// TestScreenStreamPathFindsBothGenerations: the resolver asks for every suffix in
// the ONE list, so a session recorded before the rename resolves too — and a
// directory holding BOTH resolves to the current spelling.
func TestScreenStreamPathFindsBothGenerations(t *testing.T) {
	dir := t.TempDir()
	legacy := filepath.Join(dir, "sid"+uiclient.LegacyScreenStreamSuffix)
	if err := os.WriteFile(legacy, []byte("x\n"), 0o600); err != nil {
		t.Fatalf("write the pre-rename fixture: %v", err)
	}
	got, ok := ScreenStreamPath(dir)
	if !ok || got != legacy {
		t.Fatalf("ScreenStreamPath = %q,%v; want the pre-rename file %q", got, ok, legacy)
	}
	current := filepath.Join(dir, "sid"+uiclient.ScreenStreamSuffix)
	if err := os.WriteFile(current, []byte("y\n"), 0o600); err != nil {
		t.Fatalf("write the current fixture: %v", err)
	}
	got, ok = ScreenStreamPath(dir)
	if !ok || got != current {
		t.Fatalf("with both present ScreenStreamPath = %q,%v; want the CURRENT file %q", got, ok, current)
	}
	if p, ok := ScreenStreamPath(t.TempDir()); ok {
		t.Fatalf("an empty directory resolved to %q", p)
	}
}

// TestLoadScreenStreamPaintsAtTheRecordedGrid: the frames are the painted screen
// and the geometry is the RECORDING's own — the property that makes a replay
// cell-exact instead of an approximation re-wrapped for the caller's terminal.
func TestLoadScreenStreamPaintsAtTheRecordedGrid(t *testing.T) {
	dir := t.TempDir()
	path := recordStream(t, dir, "sid", 24, 4, [][]byte{
		[]byte("\x1b[Hhello"),
		[]byte("\x1b[2;1Hworld"),
		[]byte("\x1b[3;1Hthird"),
	})
	st, err := LoadScreenStream(path)
	if err != nil {
		t.Fatalf("LoadScreenStream: %v", err)
	}
	if st.Total != 3 {
		t.Fatalf("Total = %d, want 3", st.Total)
	}
	if st.Meta.Grid[0] != 24 || st.Meta.Grid[1] != 4 {
		t.Fatalf("Meta.Grid = %v, want [24 4]", st.Meta.Grid)
	}
	if st.Sampled {
		t.Fatal("three frames were reported as sampled")
	}
	if len(st.Frames) != 3 {
		t.Fatalf("Frames = %d, want 3", len(st.Frames))
	}
	if st.PayloadBytes <= 0 {
		t.Fatalf("PayloadBytes = %d, want the counted payload", st.PayloadBytes)
	}
	last := st.Frames[len(st.Frames)-1]
	if len(last.Rows) != 4 {
		t.Fatalf("last frame has %d rows, want 4", len(last.Rows))
	}
	joined := ""
	for _, r := range last.Rows {
		joined += strings.TrimRight(string(r.Cells), " ") + "\n"
	}
	for _, want := range []string{"hello", "world", "third"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("last frame does not carry %q:\n%s", want, joined)
		}
	}
	if st.LegacyHeader {
		t.Fatal("a stream written by THIS build was reported as carrying the pre-rename header")
	}
}

// TestLoadScreenStreamSamplingKeepsTheFirstAndTheTail pins the sampling policy: a
// long recording keeps what a reader steps (the opening screen and the end), and
// Sampled says so rather than the count silently shrinking.
func TestLoadScreenStreamSamplingKeepsTheFirstAndTheTail(t *testing.T) {
	keep := screenStreamKeptIndexes(streamStepFrameCap + 10)
	if len(keep) != streamStepFrameCap {
		t.Fatalf("kept %d indexes, want the cap %d", len(keep), streamStepFrameCap)
	}
	if keep[0] != 0 {
		t.Fatalf("first kept index = %d, want 0 (the screen as it opened)", keep[0])
	}
	if keep[len(keep)-1] != streamStepFrameCap+9 {
		t.Fatalf("last kept index = %d, want %d (the end of the recording)", keep[len(keep)-1], streamStepFrameCap+9)
	}
	for i := 1; i < len(keep); i++ {
		if keep[i] <= keep[i-1] {
			t.Fatalf("kept indexes are not ascending at %d: %v", i, keep[i-1:i+1])
		}
	}
	all := screenStreamKeptIndexes(3)
	if len(all) != 3 || all[0] != 0 || all[2] != 2 {
		t.Fatalf("a short recording was sampled: %v", all)
	}
}

// TestLoadStreamLegSubstitutesTheGrid replaces the geometry only when the
// recording declares one, and records that it did — the provenance word a reader
// sees on the header line.
func TestLoadStreamLegSubstitutesTheGrid(t *testing.T) {
	dir := t.TempDir()
	recordStream(t, dir, "sid", 40, 9, [][]byte{[]byte("\x1b[Hon")})
	cols, rows := 80, 24
	geo := GeometryFromFallback
	st, note, err := loadStreamLeg(dir, StreamFromAuto, &cols, &rows, &geo)
	if err != nil {
		t.Fatalf("loadStreamLeg: %v", err)
	}
	if st == nil {
		t.Fatal("loadStreamLeg found no stream in a directory holding one")
	}
	if note != "" {
		t.Fatalf("note = %q, want empty for a usable recording", note)
	}
	if cols != 40 || rows != 9 {
		t.Fatalf("grid = %dx%d, want the recording's 40x9", cols, rows)
	}
	if geo != GeometryFromStream {
		t.Fatalf("geometry source = %q, want %q", geo, GeometryFromStream)
	}
}

// TestLoadStreamLegThreeOutcomes is the Rule-15 arm: nothing-recorded, forced-dump
// and recorded-but-unreadable must be three DIFFERENT answers.
func TestLoadStreamLegThreeOutcomes(t *testing.T) {
	t.Run("nothing recorded is not an error", func(t *testing.T) {
		cols, rows, geo := 80, 24, GeometryFromFallback
		st, note, err := loadStreamLeg(t.TempDir(), StreamFromAuto, &cols, &rows, &geo)
		if err != nil || st != nil || note != "" {
			t.Fatalf("empty dir: stream=%v note=%q err=%v; want all empty (a worker run paints no screen)", st, note, err)
		}
		if geo != GeometryFromFallback {
			t.Fatalf("geometry source became %q on a session with no recording", geo)
		}
	})
	t.Run("from dump never looks", func(t *testing.T) {
		dir := t.TempDir()
		recordStream(t, dir, "sid", 30, 6, [][]byte{[]byte("\x1b[Hon")})
		cols, rows, geo := 80, 24, GeometryFromFallback
		st, note, err := loadStreamLeg(dir, StreamFromDump, &cols, &rows, &geo)
		if err != nil || st != nil {
			t.Fatalf("--from dump still used the stream: stream=%v err=%v", st, err)
		}
		if !strings.Contains(note, StreamFromDump) {
			t.Fatalf("note = %q, want it to name the forced source", note)
		}
		if cols != 80 || geo != GeometryFromFallback {
			t.Fatalf("a forced dump changed the geometry: %dx%d %q", cols, rows, geo)
		}
	})
	t.Run("from stream refuses a session that recorded none", func(t *testing.T) {
		cols, rows, geo := 80, 24, GeometryFromFallback
		st, _, err := loadStreamLeg(t.TempDir(), StreamFromStream, &cols, &rows, &geo)
		if err == nil {
			t.Fatalf("--from stream fell back silently: stream=%v", st)
		}
		if !strings.Contains(err.Error(), StreamFromDump) {
			t.Fatalf("the refusal %q does not name the way to get the dump", err)
		}
	})
	t.Run("recorded but unusable is reported", func(t *testing.T) {
		dir := t.TempDir()
		if err := os.WriteFile(filepath.Join(dir, "sid"+uiclient.ScreenStreamSuffix), []byte("garbage\n"), 0o600); err != nil {
			t.Fatalf("write the corrupt fixture: %v", err)
		}
		cols, rows, geo := 80, 24, GeometryFromFallback
		st, note, err := loadStreamLeg(dir, StreamFromAuto, &cols, &rows, &geo)
		if err == nil {
			t.Fatal("a corrupt recording was accepted")
		}
		if st != nil || note == "" {
			t.Fatalf("stream=%v note=%q; want nil and a note naming the unreadable artifact", st, note)
		}
	})
}

// TestScreenStreamHeaderOfSeparatesAShortReadFromAFailedOne is the MINIMAL
// REPRODUCTION of the ignored-error site this arm exists to close: a `Read` that
// returned no byte was indistinguishable from one that read a CURRENT-spelling
// header, because the failure came back as "" and the caller turned "" into
// `SgrStreamHeaderIsLegacy` → false → "this recording was written by this build".
// The two facts must be separable, and the ordinary short read must NOT be
// reported as a failure — that is what would make the honest version useless.
func TestScreenStreamHeaderOfSeparatesAShortReadFromAFailedOne(t *testing.T) {
	t.Run("a read that yields no byte is an error", func(t *testing.T) {
		dir := t.TempDir()
		empty := filepath.Join(dir, "empty"+uiclient.ScreenStreamSuffix)
		if err := os.WriteFile(empty, nil, 0o600); err != nil {
			t.Fatalf("write the empty fixture: %v", err)
		}
		if line, err := screenStreamHeaderOf(empty); err == nil {
			t.Fatalf("an empty file read as the header %q; a read that returned no byte was reported as a reading", line)
		}
	})
	t.Run("a short file is a reading, not a failure", func(t *testing.T) {
		dir := t.TempDir()
		short := filepath.Join(dir, "short"+uiclient.ScreenStreamSuffix)
		const first = "garbage\nsecond line"
		if err := os.WriteFile(short, []byte(first), 0o600); err != nil {
			t.Fatalf("write the short fixture: %v", err)
		}
		line, err := screenStreamHeaderOf(short)
		if err != nil {
			t.Fatalf("a %d-byte file was refused: %v — a regular file shorter than the "+
				"probe buffer is the ordinary case, not a failed read", len(first), err)
		}
		if line != "garbage" {
			t.Fatalf("first line = %q, want %q", line, "garbage")
		}
	})
	t.Run("the reading flows to LegacyHeader and the degrade is named", func(t *testing.T) {
		dir := t.TempDir()
		legacy := filepath.Join(dir, "sid"+uiclient.LegacyScreenStreamSuffix)
		if err := os.WriteFile(legacy, []byte(uiclient.LegacyScreenStreamHeaderPrefix+" version=1 grid=12x3\n"), 0o600); err != nil {
			t.Fatalf("write the legacy fixture: %v", err)
		}
		line, err := screenStreamHeaderOf(legacy)
		if err != nil {
			t.Fatalf("screenStreamHeaderOf: %v", err)
		}
		if !uiclient.SgrStreamHeaderIsLegacy(line) {
			t.Fatalf("a pre-rename header %q was not reported as legacy", line)
		}
	})
}

// TestParseStreamFromNamesItsOperands: an unknown word is refused BY NAME. A flag
// that accepts `--from dum` and quietly replays the default teaches a caller
// something false about their own command line.
func TestParseStreamFromNamesItsOperands(t *testing.T) {
	for _, ok := range []string{"", StreamFromAuto, StreamFromStream, StreamFromDump} {
		if got, err := ParseStreamFrom(ok); err != nil || got == "" {
			t.Fatalf("ParseStreamFrom(%q) = %q,%v; want a source", ok, got, err)
		}
	}
	if _, err := ParseStreamFrom("dum"); err == nil {
		t.Fatal("ParseStreamFrom accepted a misspelling")
	}
}

// TestLoadScreenStreamRefusesAnEmptyRecording: a recording that wrote no paint
// bytes is not a picture. Handing the player an empty sequence would render a
// blank screen and call it a successful replay (Rule 15).
func TestLoadScreenStreamRefusesAnEmptyRecording(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "sid"+uiclient.ScreenStreamSuffix)
	// A header with no frame and no trailer: the shape a run killed at open leaves.
	s, err := uiclient.OpenSgrStream(uiclient.SgrStreamConfig{Path: path, Cols: 12, Rows: 3})
	if err != nil {
		t.Fatalf("OpenSgrStream: %v", err)
	}
	if cerr := s.Close(); cerr != nil {
		t.Fatalf("Close: %v", cerr)
	}
	if _, lerr := LoadScreenStream(path); lerr == nil {
		t.Fatal("an empty recording loaded as a replayable stream")
	}
}

// TestLoadSessionAtFromStreamIsARequirement is the MINIMAL REPRODUCTION of a
// defect measured on this tree (2026-11-08): `--from stream` fell through to the
// dump, so a caller who demanded the renderer's own bytes was silently handed the
// interpretation — the exact failure the flag exists to make impossible, and one
// that LOOKS correct (`source=dump(interpreted, throttled)` on a session that had
// nothing better). The arm goes through the MATERIAL entry point, not
// loadStreamLeg, because that is the seam the defect was in: the leg reported
// "nothing recorded" and the material carried on regardless.
func TestLoadSessionAtFromStreamIsARequirement(t *testing.T) {
	dir := t.TempDir()
	_, err := LoadSessionAtFrom(dir, "", StreamFromStream)
	if err == nil {
		t.Fatal("--from stream loaded a session that recorded no screen stream: it fell back to the dump instead of refusing")
	}
	if !strings.Contains(err.Error(), StreamFromStream) || !strings.Contains(err.Error(), StreamFromDump) {
		t.Fatalf("the refusal %q names neither the requirement nor the way to get the dump", err)
	}
	// --from auto keeps the FALLBACK (that is what "auto" means) — the same empty
	// directory must NOT be refused on that spelling, or the fix would have turned
	// a preference into a requirement.
	if _, aerr := LoadSessionAtFrom(dir, "", StreamFromAuto); aerr == nil {
		// An empty directory has nothing to replay at all, so an error here is
		// expected — what must NOT appear is the stream REQUIREMENT sentence.
		t.Log("auto on a truly empty directory refused it (no material at all), which is correct")
	} else if strings.Contains(aerr.Error(), "requires one") {
		t.Fatalf("--from auto refused the session with the stream REQUIREMENT text: %v", aerr)
	}
}
