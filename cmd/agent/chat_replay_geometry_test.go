package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nick/gopi/internal/config"
	"github.com/nick/gopi/internal/replaydrive"
	"github.com/nick/gopi/internal/sessionevent"
)

// geometryRecording writes a session JSONL whose session_start carries the
// geometry field (or none, when geometryJSON is empty) — the RECORDING side of
// this file's contract, produced the way internal/agent produces it.
func geometryRecording(t *testing.T, geometryJSON string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "session.jsonl")
	row := `{"type":"` + sessionevent.EventTypeSessionStart + `","sessionId":"s-geo","model":"m"`
	if geometryJSON != "" {
		row += `,"` + sessionevent.FieldTerminalGeometry + `":` + geometryJSON
	}
	body := row + "}\n" +
		`{"type":"user","message":{"role":"user","content":"hello"}}` + "\n"
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("write the recording: %v", err)
	}
	return path
}

// TestChatReplayDrawsAtTheRecordedSize is the epic's first deliverable, end to
// end at the seam that decides: a recording made at 90x20 makes THIS run's
// resolved ideal geometry 90x20, and the run says where the number came from.
func TestChatReplayDrawsAtTheRecordedSize(t *testing.T) {
	// The env channel is registered empty so this test cannot inherit (or leak)
	// a geometry an operator exported: an EMPTY value is "nobody asked" to the
	// resolver, which is what lets the recording decide.
	t.Setenv(config.ChatIdealGeometryEnv, "")
	rp := &chatReplay{rec: &replaydrive.Recording{Path: geometryRecording(t, `{"state":"measured","cols":90,"rows":20}`)}}

	var errOut bytes.Buffer
	chatReplayApplyRecordedGeometry(rp, &errOut)

	got := config.ChatIdealGeometry()
	if got.Cols != 90 || got.Rows != 20 {
		t.Fatalf("the replay resolved %dx%d, want the recording's 90x20\nstderr:\n%s", got.Cols, got.Rows, errOut.String())
	}
	if got.Source != config.ChatIdealGeometryEnv {
		t.Errorf("the recorded size is in force but the source says %q — a reader cannot tell it apart from a stated size", got.Source)
	}
	if !strings.Contains(errOut.String(), "90"+config.ChatIdealGeometryDimSep+"20") {
		t.Errorf("the notice does not name the recorded size:\n%s", errOut.String())
	}
	for _, want := range []string{"recording", "replaces the ideal"} {
		if !strings.Contains(errOut.String(), want) {
			t.Errorf("the notice never says %q, so the canvas source is not attributable:\n%s", want, errOut.String())
		}
	}
}

// TestChatReplayStatesTheFallbackWhenTheRecordingHasNone is the OLD-SESSION
// case, and the reason the epic demanded a VISIBLE fallback: a session recorded
// before the field existed still replays, and the run says out loud that the
// picture is not the size the session was made on.
func TestChatReplayStatesTheFallbackWhenTheRecordingHasNone(t *testing.T) {
	t.Setenv(config.ChatIdealGeometryEnv, "")
	before := config.ChatIdealGeometry()
	rp := &chatReplay{rec: &replaydrive.Recording{Path: geometryRecording(t, "")}}

	var errOut bytes.Buffer
	chatReplayApplyRecordedGeometry(rp, &errOut)

	after := config.ChatIdealGeometry()
	if after.Cols != before.Cols || after.Rows != before.Rows {
		t.Errorf("a recording with no geometry moved the ideal from %dx%d to %dx%d", before.Cols, before.Rows, after.Cols, after.Rows)
	}
	notice := errOut.String()
	if !strings.Contains(notice, "recorded before") {
		t.Errorf("the fallback does not say WHY (an older recording):\n%s", notice)
	}
	if !strings.Contains(notice, "not the size the session was made on") {
		t.Errorf("the fallback does not warn that the picture is not the recorded size:\n%s", notice)
	}
}

// TestChatReplayKeepsAnExplicitlyStatedSize: the recording's size is a
// DEFAULT, not an override — an operator who states a size on this command line
// gets it, and hears that the recording did not win.
func TestChatReplayKeepsAnExplicitlyStatedSize(t *testing.T) {
	const stated = "100" + "x" + "30"
	t.Setenv(config.ChatIdealGeometryEnv, stated)
	rp := &chatReplay{rec: &replaydrive.Recording{Path: geometryRecording(t, `{"state":"measured","cols":90,"rows":20}`)}}

	var errOut bytes.Buffer
	chatReplayApplyRecordedGeometry(rp, &errOut)

	got := config.ChatIdealGeometry()
	if got.Cols != 100 || got.Rows != 30 {
		t.Fatalf("a stated size was overridden by the recording: %dx%d", got.Cols, got.Rows)
	}
	if !strings.Contains(errOut.String(), "does not override it") {
		t.Errorf("the run does not say it kept the stated size:\n%s", errOut.String())
	}
}

// TestChatReplayReportsARecordedUnknownAsAReason: a session that RECORDED
// "unknown" answered the question, so the fallback sentence must carry the
// recording's own reason rather than claiming the field was missing.
func TestChatReplayReportsARecordedUnknownAsAReason(t *testing.T) {
	t.Setenv(config.ChatIdealGeometryEnv, "")
	rp := &chatReplay{rec: &replaydrive.Recording{Path: geometryRecording(t,
		`{"state":"unknown","reason":"`+sessionevent.GeometryReasonNoTerminal+`"}`)}}

	var errOut bytes.Buffer
	chatReplayApplyRecordedGeometry(rp, &errOut)

	if !strings.Contains(errOut.String(), sessionevent.GeometryReasonNoTerminal) {
		t.Errorf("the recorded reason was dropped:\n%s", errOut.String())
	}
	if strings.Contains(errOut.String(), "recorded before") {
		t.Errorf("an explicit unknown was reported as a MISSING field:\n%s", errOut.String())
	}
}

// TestChatReplayNeverForwardsAZeroGeometry: an undecodable field (a zero width
// marked "measured") is not a size the surface may draw at — the run falls back
// and says the field could not be read.
func TestChatReplayNeverForwardsAZeroGeometry(t *testing.T) {
	t.Setenv(config.ChatIdealGeometryEnv, "")
	before := config.ChatIdealGeometry()
	rp := &chatReplay{rec: &replaydrive.Recording{Path: geometryRecording(t, `{"state":"measured","cols":0,"rows":24}`)}}

	var errOut bytes.Buffer
	chatReplayApplyRecordedGeometry(rp, &errOut)

	after := config.ChatIdealGeometry()
	if after.Cols != before.Cols || after.Rows != before.Rows {
		t.Errorf("a zero-width geometry reached the surface: %dx%d", after.Cols, after.Rows)
	}
	if !strings.Contains(errOut.String(), "could not be read") {
		t.Errorf("the unusable field was not reported:\n%s", errOut.String())
	}
}

// TestChatFilmShootsAtTheRecordedSize is the FILM half: with no --cols/--rows
// on the command line, the frames are shot at the recording's terminal size.
func TestChatFilmShootsAtTheRecordedSize(t *testing.T) {
	t.Setenv(config.ChatIdealGeometryEnv, "")
	path := geometryRecording(t, `{"state":"measured","cols":100,"rows":30}`)

	var errOut bytes.Buffer
	cols, rows, err := chatFilmGeometry(chatBeatFlag{}, path, &errOut)
	if err != nil {
		t.Fatalf("a recording-sized film was refused: %v", err)
	}

	if cols != 100 || rows != 30 {
		t.Fatalf("the film would shoot at %dx%d, want the recording's 100x30\nstderr:\n%s", cols, rows, errOut.String())
	}
	if !strings.Contains(errOut.String(), "recording's own terminal size") {
		t.Errorf("the film does not say where its canvas came from:\n%s", errOut.String())
	}
}

// TestChatFilmKeepsAStatedSize: --cols/--rows is the operator speaking about
// THIS film, so it wins over the recording — and a half-stated pair keeps the
// default for the dimension that was not stated, out loud.
func TestChatFilmKeepsAStatedSize(t *testing.T) {
	t.Setenv(config.ChatIdealGeometryEnv, "")
	path := geometryRecording(t, `{"state":"measured","cols":100,"rows":30}`)

	var full bytes.Buffer
	if cols, rows, err := chatFilmGeometry(chatBeatFlag{cols: 40, rows: 12, colsSet: true, rowsSet: true}, path, &full); err != nil || cols != 40 || rows != 12 {
		t.Fatalf("a fully stated size = %dx%d (err %v), want 40x12", cols, rows, err)
	}
	if strings.Contains(full.String(), "recording's own terminal size") {
		t.Errorf("a stated size still reported the recording as the source:\n%s", full.String())
	}

	var half bytes.Buffer
	idealCols, idealRows := chatBeatIdealCols(), chatBeatIdealRows()
	if cols, rows, err := chatFilmGeometry(chatBeatFlag{cols: 40, rows: idealRows, colsSet: true}, path, &half); err != nil || cols != 40 || rows != idealRows {
		t.Fatalf("a half stated size = %dx%d (err %v), want 40x%d", cols, rows, err, idealRows)
	}
	if !strings.Contains(half.String(), chatFlagRows) {
		t.Errorf("the half-stated pair did not name the dimension it defaulted:\n%s", half.String())
	}
	if idealCols <= 0 || idealRows <= 0 {
		t.Fatalf("the ideal geometry resolved to %dx%d", idealCols, idealRows)
	}
}

// TestChatFilmFallsBackWithAReason: an unresolvable target still shoots — at
// the surface's ideal, with the reason printed. A film is never silently made at
// a canvas nobody chose.
func TestChatFilmFallsBackWithAReason(t *testing.T) {
	t.Setenv(config.ChatIdealGeometryEnv, "")

	var errOut bytes.Buffer
	cols, rows, err := chatFilmGeometry(chatBeatFlag{}, "definitely-not-a-real-session-"+t.Name(), &errOut)
	if err != nil {
		t.Fatalf("an unresolvable target was refused instead of falling back: %v", err)
	}
	if cols != chatBeatIdealCols() || rows != chatBeatIdealRows() {
		t.Fatalf("the fallback = %dx%d, want the ideal %dx%d", cols, rows, chatBeatIdealCols(), chatBeatIdealRows())
	}
	notice := errOut.String()
	if !strings.Contains(notice, "ideal") || !strings.Contains(notice, "not the size the session was made on") {
		t.Errorf("the fallback is not stated with its reason:\n%s", notice)
	}
}

// TestChatHelpLeadsToTheReplaySurface is the DISCOVERY deliverable: the page a
// user reaches from `gopi chat -h` (and from `gopi chat --replay <id> -h`) names
// every word the replay/film path is reached by, gives a worked command line for
// each step, and answers the pixel question rather than leaving it hanging.
func TestChatHelpLeadsToTheReplaySurface(t *testing.T) {
	// The DETAILED half lives on the SCOPED page (`gopi chat --replay -h`, the
	// family's own page — chat_replay_help_scope.go): the worked command lines, the
	// pixel arithmetic and the env/settings channels are what a reader sent there
	// needs, and keeping them there is what makes the session page readable.
	// The main page still names every WORD the family is reached by, through its
	// index block, plus the route to the scoped page — both are asserted, because
	// a reader who never learns the route has the same problem either way.
	for _, want := range []string{
		chatGeometryFlag, config.ChatIdealGeometryEnv, config.ChatIdealGeometrySettingsKey,
		chatCellSizeReference, chatPixelsFor(100, 30),
		config.ChatIdealGeometryDefault,
	} {
		if !strings.Contains(chatReplayHelp, want) {
			t.Errorf("the replay/film page never mentions %q — the word is unreachable from the page that documents it", want)
		}
	}
	for _, want := range append(chatReplayHelpFlags(), chatReplayHelpPointsHere) {
		if !strings.Contains(chatUsage, want) {
			t.Errorf("the chat page never mentions %q — the word is unreachable from the page a reader starts on", want)
		}
	}
	// Every flag the examples use must be one this verb actually accepts: a page
	// that spells a flag its parser refuses is worse than no page (Rule 13).
	for _, flag := range []string{chatFlagReplay, chatFlagFilm, chatFlagOut, chatTerminalFlag, chatGeometryFlag} {
		if !containsString(chatREPLFlags(), flag) {
			t.Errorf("the page advertises %s, which is not in this verb's vocabulary (%v)", flag, chatREPLFlags())
		}
	}
	// The pixel reference is derived, not retyped: two cells of the reference
	// pair are what a reader multiplies by, so the page must show the arithmetic
	// the constant performs.
	if got, want := chatPixelsFor(117, 26), "706x342px"; got != want {
		t.Errorf("chatPixelsFor(117, 26) = %q, want %q", got, want)
	}
}

// TestChatFilmRefusesAStatedCanvasBeyondTheRecording is the epic's regression
// guard, and it is the reason this file grew a third return value.
//
// THE DEFECT IT PINS (measured 2026-10-11, session 62a7079b, task 01a127c2):
// `gopi chat --replay <id> --film DIR --cols 113 --rows 25` against a session
// recorded at 112x25 started a display server, a terminal and a producer at
// 113x25; the producer's own surface found the terminal was the recording's
// 112x25, refused with "this terminal is 112x25 but the beat protocol was asked
// for 113x25", and exited before its first beat. The film then ended with 0
// frame(s), an EMPTY terminal.log, and a closing report that only POINTED at
// producer.log — the two numbers that explain it were never on the film
// driver's own stream.
//
// BOTH AXES ARE CHECKED SEPARATELY, because they are separately actionable: a
// width above the recording is not fixed by lowering --rows, and the refusal
// must say WHICH number is out of range.
func TestChatFilmRefusesAStatedCanvasBeyondTheRecording(t *testing.T) {
	t.Setenv(config.ChatIdealGeometryEnv, "")
	path := geometryRecording(t, `{"state":"measured","cols":112,"rows":25}`)

	for _, tc := range []struct {
		name string
		flag chatBeatFlag
		axis string
	}{
		{"cols one cell over", chatBeatFlag{cols: 113, rows: 25, colsSet: true, rowsSet: true}, chatFilmDimWidth},
		{"rows well over", chatBeatFlag{cols: 112, rows: 123, colsSet: true, rowsSet: true}, chatFilmDimHeight},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var errOut bytes.Buffer
			_, _, err := chatFilmGeometry(tc.flag, path, &errOut)
			if err == nil {
				t.Fatalf("a canvas above the recording was accepted — the run would end with 0 frames and no named reason\nstderr:\n%s", errOut.String())
			}
			// The message must name the BOUNDARY, not just fail: the flag the
			// caller typed, the value they asked for, the recording's value on
			// that axis, and the recording's geometry as a pair.
			for _, want := range []string{
				tc.axis,
				"112" + config.ChatIdealGeometryDimSep + "25",
				"recorded",
			} {
				if !strings.Contains(err.Error(), want) {
					t.Errorf("the refusal never says %q, so the boundary it refused on is not named:\n%s", want, err.Error())
				}
			}
			if !strings.Contains(err.Error(), chatFilmDimWidth) && !strings.Contains(err.Error(), chatFilmDimHeight) {
				t.Errorf("the refusal names neither axis:\n%s", err.Error())
			}
		})
	}
}

// TestChatFilmRefusesWithTheTwoNumbersAndTheFit is the CLARITY half of the same
// check: the sentence carries the asked value, the recorded value, the flag that
// fits, and the reason the two cannot be reconciled — a refusal an operator can
// act on without opening producer.log.
func TestChatFilmRefusesWithTheTwoNumbersAndTheFit(t *testing.T) {
	t.Setenv(config.ChatIdealGeometryEnv, "")
	path := geometryRecording(t, `{"state":"measured","cols":112,"rows":25}`)

	var errOut bytes.Buffer
	_, _, err := chatFilmGeometry(chatBeatFlag{cols: 113, rows: 25, colsSet: true, rowsSet: true}, path, &errOut)
	if err == nil {
		t.Fatalf("113 columns was accepted against a 112-column recording")
	}
	msg := err.Error()
	for _, want := range []string{
		chatFlagCols,          // what was typed
		"113",                 // what was asked for
		"112",                 // what the recording had
		chatFlagCols + " 112", // the value that fits
		"a replay draws its canvas at the size the session was RECORDED at", // why
	} {
		if !strings.Contains(msg, want) {
			t.Errorf("the refusal does not carry %q:\n%s", want, msg)
		}
	}
	if strings.Contains(errOut.String(), "no frame was taken") {
		t.Errorf("the refusal was reported through the zero-frame path instead of before anything started:\n%s", errOut.String())
	}
}

// TestChatFilmAcceptsAStatedCanvasAtOrBelowTheRecording is the OTHER direction,
// and it is the constraint that makes this change safe: within the legal range
// (cols/rows <= the recorded size) the resolution is EXACTLY what it was — same
// numbers, same notices, no new refusal.
func TestChatFilmAcceptsAStatedCanvasAtOrBelowTheRecording(t *testing.T) {
	t.Setenv(config.ChatIdealGeometryEnv, "")
	path := geometryRecording(t, `{"state":"measured","cols":112,"rows":25}`)

	for _, tc := range []struct {
		name  string
		flag  chatBeatFlag
		wantC int
		wantR int
	}{
		{"exactly at the recorded size", chatBeatFlag{cols: 112, rows: 25, colsSet: true, rowsSet: true}, 112, 25},
		{"below on both axes", chatBeatFlag{cols: 94, rows: 24, colsSet: true, rowsSet: true}, 94, 24},
		{"a half-stated pair keeps its default", chatBeatFlag{cols: 40, rows: chatBeatIdealRows(), colsSet: true}, 40, chatBeatIdealRows()},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var errOut bytes.Buffer
			cols, rows, err := chatFilmGeometry(tc.flag, path, &errOut)
			if err != nil {
				t.Fatalf("a canvas inside the recorded range was refused: %v", err)
			}
			if cols != tc.wantC || rows != tc.wantR {
				t.Fatalf("resolved %dx%d, want %dx%d", cols, rows, tc.wantC, tc.wantR)
			}
		})
	}
}

// TestChatFilmDoesNotRefuseWithoutARecordedSize: the check has to know the
// BOUNDARY before it can refuse against it. A recording with no geometry (an
// older session) or an unresolvable target has none, so the run falls back —
// exactly as before — and says why on its own stream.
func TestChatFilmDoesNotRefuseWithoutARecordedSize(t *testing.T) {
	t.Setenv(config.ChatIdealGeometryEnv, "")

	for _, tc := range []struct {
		name   string
		target string
	}{
		{"a recording with no geometry field", geometryRecording(t, "")},
		{"an unresolvable target", "definitely-not-a-real-session-" + t.Name()},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var errOut bytes.Buffer
			cols, rows, err := chatFilmGeometry(chatBeatFlag{cols: 999, rows: 999, colsSet: true, rowsSet: true}, tc.target, &errOut)
			if err != nil {
				t.Fatalf("a run with no recorded boundary was refused: %v", err)
			}
			if cols != 999 || rows != 999 {
				t.Fatalf("a stated canvas was not kept: %dx%d", cols, rows)
			}
		})
	}
}
