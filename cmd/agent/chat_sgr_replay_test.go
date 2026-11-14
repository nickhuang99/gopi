package main

// chat_sgr_replay_test.go — `gopi chat --sgr-replay`'s behaviour, executed (Rule 31:
// the surface this run changed is INVOKED here with real arguments, not described).
//
// Every test that produces a picture builds its fixture with the SAME writer
// production uses (uiclient.OpenSgrStream), so a change to the format shows up as a
// failing test rather than as two hand-rolled streams that agree with nobody. The
// stream's bytes are written through the writer's own frame API, which is what makes
// the sha256 assertions meaningful: the expected bytes are what the RECORDING
// contains, not what the test hoped for.

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/nickhuang99/gopi/internal/config"
	"github.com/nickhuang99/gopi/internal/sessionlayout"
	"github.com/nickhuang99/gopi/internal/uiclient"
)

// sgrReplayParkedYields is how many scheduling opportunities the paused-replay
// assertion spends before it concludes the writer is parked. It is a count of
// Gosched turns rather than a wall-clock window ON PURPOSE (Rule 1: the number is
// named and it is not a timeout): the property under test is "a paused writer
// produces nothing", which is decided by ordering — play() parks in
// waitWhilePaused before its first Write — so the test's job is to give that
// ordering every chance to be violated, not to wait for a duration. A generous
// count costs microseconds, whereas the 200ms of sleeping it replaces cost every
// run of this package 200ms of real time.
const sgrReplayParkedYields = 5000

// sgrReplayFixture writes a stream with the production writer and returns its path
// and the exact concatenation of its frames (the bytes a replay must produce, in
// order, with nothing added). Trailer==false leaves the file without its close-out
// record, which is the "the recording run died mid-frame" shape.
func sgrReplayFixture(t *testing.T, frames []string, trailer bool) (path string, want []byte) {
	t.Helper()
	if strings.Contains(frames[0], "..") {
		t.Fatalf("fixture path traversal guard: %q", frames[0])
	}
	dir := t.TempDir()
	path = filepath.Join(dir, "fixture"+uiclient.ScreenStreamSuffix)
	s, err := uiclient.OpenSgrStream(uiclient.SgrStreamConfig{Path: path, Cols: 80, Rows: 24, Revision: "test"})
	if err != nil {
		t.Fatalf("open the fixture stream: %v", err)
	}
	var buf bytes.Buffer
	for _, f := range frames {
		if werr := s.Write([]byte(f)); werr != nil {
			t.Fatalf("write fixture frame %q: %v", f, werr)
		}
		buf.WriteString(f)
	}
	if trailer {
		if cerr := s.Close(); cerr != nil {
			t.Fatalf("close the fixture stream: %v", cerr)
		}
	}
	return path, buf.Bytes()
}

// replayCapture runs the verb's PLAYBACK leg with no terminal and returns what
// reached stdout and stderr and the status. It calls the same function the CLI
// dispatches to (chatSgrReplayRun), so the flag's resolsolution, the reader, the
// pause/quit layer and the notices are all exercised — only the tty-ness of stdin
// differs, and that is the environment, not the code under test.
func replayCapture(t *testing.T, target string) (out, errOut string, status int) {
	t.Helper()
	var o, e bytes.Buffer
	status = chatSgrReplayRun(target, &o, &e)
	return o.String(), e.String(), status
}

// TestChatSgrReplayWritesTheRecordingVerbatim is the verb's promise in one assertion:
// stdout IS the concatenation of the frames — same bytes, same order, nothing
// interleaved — and a complete stream says nothing at all on stderr.
func TestChatSgrReplayWritesTheRecordingVerbatim(t *testing.T) {
	frames := []string{"\x1b[2J\x1b[1;1Hhello", "\x1b[2;1Hworld", "\x1b[Hyou> "}
	path, want := sgrReplayFixture(t, frames, true)

	out, errOut, status := replayCapture(t, path)
	if status != chatExitOK {
		t.Fatalf("status = %d, want %d (chatExitOK); stderr = %q", status, chatExitOK, errOut)
	}
	if out != string(want) {
		t.Fatalf("stdout = %q, want the recording's frames verbatim %q", out, string(want))
	}
	if errOut != "" {
		t.Fatalf("stderr = %q, want nothing at all: a complete recording is played silently "+
			"(this surface may not paint over the artifact with narration)", errOut)
	}
}

// TestChatSgrReplaySha256IsStableAcrossRuns is the evidence the flag exists for, run
// as a measurement rather than a claim: the same file, played twice, has one digest.
// Two DIFFERENT builds is the human acceptance run (the report carries it); the
// property that makes that meaningful — the output is a function of the artifact and
// of nothing else — is what this pins.
func TestChatSgrReplaySha256IsStableAcrossRuns(t *testing.T) {
	frames := make([]string, 0, 32)
	for i := 0; i < 32; i++ {
		frames = append(frames, "\x1b["+string(rune('1'+i%9))+";1Hframe")
	}
	path, _ := sgrReplayFixture(t, frames, true)

	first, _, s1 := replayCapture(t, path)
	second, _, s2 := replayCapture(t, path)
	if s1 != chatExitOK || s2 != chatExitOK {
		t.Fatalf("statuses = %d/%d, want %d/%d", s1, s2, chatExitOK, chatExitOK)
	}
	d1 := sha256.Sum256([]byte(first))
	d2 := sha256.Sum256([]byte(second))
	if d1 != d2 {
		t.Fatalf("two replays of one file differ: %s vs %s",
			hex.EncodeToString(d1[:]), hex.EncodeToString(d2[:]))
	}
	if len(first) == 0 {
		t.Fatal("the replay produced no bytes at all — a digest of nothing is not evidence")
	}
}

// TestChatSgrReplayWholeButUnclosedSaysSoAndDoesNotSayTruncated pins SENTENCE 1 of
// the three, and it is the measured defect this change closes (bug 01a1b823-5070).
//
// The fixture is the shape a census of this host found on every stream that lacked a
// close-out record: NO trailer, and every frame ending on its own record boundary
// (`remaining=0`). The bytes are whole. The verb used to print
// `the stream ends mid-frame`, which is a statement about a TRUNCATION and was false
// about all 14 files measured — so the assertion here is not only "the unclosed
// sentence appears" but "the truncation sentence does not".
func TestChatSgrReplayWholeButUnclosedSaysSoAndDoesNotSayTruncated(t *testing.T) {
	path, want := sgrReplayFixture(t, []string{"\x1b[2Jfirst", "\x1b[2;1Hsecond"}, false)

	out, errOut, status := replayCapture(t, path)
	if status != chatExitOK {
		t.Fatalf("status = %d, want %d: a whole artifact is a playable one", status, chatExitOK)
	}
	if out != string(want) {
		t.Fatalf("stdout = %q, want %q", out, string(want))
	}
	if !strings.Contains(errOut, "the recording is whole") {
		t.Fatalf("stderr = %q, want the WHOLE-BUT-UNCLOSED sentence", errOut)
	}
	if strings.Contains(errOut, "ends inside a frame") {
		t.Fatalf("stderr = %q, want NO truncation claim: every frame in this fixture ends on its own boundary, "+
			"so saying the stream is cut is the misdiagnosis this test exists to end", errOut)
	}
	if strings.Contains(out, "recording is whole") {
		t.Fatal("the ending notice reached stdout — stdout belongs to the recording")
	}
}

// TestChatSgrReplayHalfWrittenFrameIsNotAnError covers the finer truncation: the
// writer announced a frame length and the file ends inside the frame. The reader
// keeps every complete frame before it, so the run plays a shorter (honest) picture
// instead of failing — and it still says the recording never closed out.
func TestChatSgrReplayHalfWrittenFrameIsNotAnError(t *testing.T) {
	frames := []string{"\x1b[2Jwhole", "\x1b[2;1Hwhole2"}
	path, all := sgrReplayFixture(t, frames, false)
	// ONLY THE FIRST FRAME IS COMPLETE once the file is cut inside the second: that
	// is the reader's rule (a announced-but-unfinished frame is dropped whole, not
	// delivered in part), so it is what the picture must contain.
	want := []byte(frames[0])
	_ = all

	// Cut the file inside the LAST frame: everything announced before it stays.
	raw, rerr := os.ReadFile(path)
	if rerr != nil {
		t.Fatalf("read the fixture: %v", rerr)
	}
	cropped := raw[:len(raw)-3]
	if werr := os.WriteFile(path, cropped, 0o600); werr != nil {
		t.Fatalf("truncate the fixture: %v", werr)
	}

	out, errOut, status := replayCapture(t, path)
	if status != chatExitOK {
		t.Fatalf("status = %d, want %d (a stream that ends inside a frame is a truncated recording, "+
			"and the reader keeps what completed); stderr = %q", status, chatExitOK, errOut)
	}
	if out != string(want) {
		t.Fatalf("stdout = %q, want every COMPLETE frame %q", out, string(want))
	}
	if !strings.Contains(errOut, "ends inside a frame") {
		t.Fatalf("stderr = %q, want SENTENCE 2 — the cut-frame clause naming where the frame begins", errOut)
	}
}

// TestChatSgrReplayClosedStreamIsSilent pins the contract a capture depends on: a
// stream that reached its close-out record produces the picture and NOTHING on
// stderr. It is the third of the three endings, and the one the other two must not
// leak into.
func TestChatSgrReplayClosedStreamIsSilent(t *testing.T) {
	path, want := sgrReplayFixture(t, []string{"\x1b[2Jfirst", "\x1b[2;1Hsecond"}, true)

	out, errOut, status := replayCapture(t, path)
	if status != chatExitOK {
		t.Fatalf("status = %d, want %d", status, chatExitOK)
	}
	if out != string(want) {
		t.Fatalf("stdout = %q, want %q", out, string(want))
	}
	if errOut != "" {
		t.Fatalf("stderr = %q, want EMPTY: a closed, self-consistent recording has nothing to report, "+
			"and a capture comparing two builds must not have to filter a notice out", errOut)
	}
}

// TestChatSgrReplayContradictoryStreamNamesTheFileNotTheReader pins SENTENCE 3: a
// file whose own records contradict its own length is refused as a fact about the
// FILE (the finding's own kind and offset), never as either of the two "it did not
// close" sentences — the distinction a single `meta.Trailer` boolean had erased.
//
// The fixture is built the way a second writer on one path builds it: a complete
// recording, then bytes appended after its close-out record.
func TestChatSgrReplayContradictoryStreamNamesTheFileNotTheReader(t *testing.T) {
	path, want := sgrReplayFixture(t, []string{"\x1b[2Jfirst"}, true)
	f, oerr := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o600)
	if oerr != nil {
		t.Fatalf("open the fixture for the second writer: %v", oerr)
	}
	if _, werr := f.WriteString("\x00\x00\x00\x00trailing bytes from a second writer"); werr != nil {
		t.Fatalf("append after the close-out record: %v", werr)
	}
	if cerr := f.Close(); cerr != nil {
		t.Fatalf("close the second writer: %v", cerr)
	}

	out, errOut, status := replayCapture(t, path)
	// THE PICTURE IS STILL PLAYED: the frames before the contradiction are real, and
	// withholding them would hide the evidence the reader is being told to distrust.
	if status != chatExitOK {
		t.Fatalf("status = %d, want %d; stderr = %q", status, chatExitOK, errOut)
	}
	if out != string(want) {
		t.Fatalf("stdout = %q, want the complete frames %q", out, string(want))
	}
	if !strings.Contains(errOut, "contradicts itself") {
		t.Fatalf("stderr = %q, want the contradiction sentence", errOut)
	}
	if !strings.Contains(errOut, uiclient.SgrStreamContradictionKindAfterTrailer) {
		t.Fatalf("stderr = %q, want the finding's own KIND (%s) so the reader knows which defect it holds",
			errOut, uiclient.SgrStreamContradictionKindAfterTrailer)
	}
	if strings.Contains(errOut, "the recording is whole") || strings.Contains(errOut, "ends inside a frame") {
		t.Fatalf("stderr = %q, want ONLY the contradiction sentence — a contradictory file is neither of the "+
			"two unclosed states", errOut)
	}
}

// TestChatSgrReplayRefusals names each way the operand can be wrong and asserts the
// sentence, the status and — the part that matters most — that NOTHING reached
// stdout. A refusal that also printed half a picture would be the worst outcome: the
// operator would have a partial artifact and a status to argue with.
func TestChatSgrReplayRefusals(t *testing.T) {
	dir := t.TempDir()
	missing := filepath.Join(dir, "absent"+uiclient.ScreenStreamSuffix)
	bogus := filepath.Join(dir, "not-a-stream"+uiclient.ScreenStreamSuffix)
	if err := os.WriteFile(bogus, []byte("this file is not what it claims to be\n"), 0o600); err != nil {
		t.Fatalf("write the bogus fixture: %v", err)
	}

	cases := []struct {
		name    string
		target  string
		wantSub string
	}{
		{"missing path", missing, "no screen.stream stream at"},
		{"not a stream", bogus, "not a screen stream"},
		{"missing value", "", chatSgrReplayFlag},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			out, errOut, status := replayCapture(t, c.target)
			if status == chatExitOK {
				t.Fatalf("status = %d, want non-zero: %q is not a playable recording", status, c.target)
			}
			if out != "" {
				t.Fatalf("stdout = %q, want nothing: a refusal must not paint a partial picture", out)
			}
			if !strings.Contains(errOut, c.wantSub) {
				t.Fatalf("stderr = %q, want it to contain %q", errOut, c.wantSub)
			}
		})
	}
}

// TestChatSgrReplayParserBothSpellings pins the two shapes the flag accepts and the
// missing-value refusal, at the PARSER rather than through the runner: the joined
// and separated spellings must land on the same target, and a bare flag must be a
// usage error that names the operand it is missing.
func TestChatSgrReplayParserBothSpellings(t *testing.T) {
	joined, consumed, err := parseChatSgrReplayArg(chatSgrReplayFlag+"=abc", "", false)
	if err != nil || !consumed || joined.target != "abc" || !joined.set {
		t.Fatalf("joined form: got %+v consumed=%v err=%v, want target abc", joined, consumed, err)
	}
	spaced, consumed, err := parseChatSgrReplayArg(chatSgrReplayFlag, "abc", true)
	if err != nil || !consumed || spaced.target != "abc" || !spaced.set {
		t.Fatalf("separated form: got %+v consumed=%v err=%v, want target abc", spaced, consumed, err)
	}
	if _, _, err := parseChatSgrReplayArg(chatSgrReplayFlag, "", false); err == nil {
		t.Fatal("a bare --sgr-replay was accepted with no operand — it must be a usage error")
	}
	if _, _, err := parseChatSgrReplayArg(chatSgrReplayFlag+"=", "", false); err == nil {
		t.Fatal("an empty joined value was accepted — it names no recording")
	}
	if _, consumed, err := parseChatSgrReplayArg("--replay", "abc", true); err != nil || consumed {
		t.Fatalf("--replay must not be claimed by this parser (consumed=%v err=%v)", consumed, err)
	}
}

// TestChatSgrReplayPauseStopsThePicture is the pause's only honest definition, run
// against the playback itself: after the control layer has been paused, a SECOND
// picture of the same frames writes nothing until it is resumed. It is measured on a
// byte count, not on a state word, because this surface prints none.
func TestChatSgrReplayPauseStopsThePicture(t *testing.T) {
	frames := [][]byte{[]byte("one"), []byte("two"), []byte("three")}

	ctl := newChatReplayControl(func(string, ...any) {})
	ctl.toggle() // pause, exactly as any key would
	if !ctl.pausedNow() {
		t.Fatal("toggle did not pause — the layer and the surface disagree about the state")
	}

	var out bytes.Buffer
	var errOut bytes.Buffer
	done := make(chan int, 1)
	go func() { done <- chatSgrReplayPlay(frames, &out, &errOut, 0, ctl) }()

	// THE MEASUREMENT: nothing is written while paused. A PAUSED DRIVER CANNOT
	// UNDO THAT — play() checks quitRequested and then parks in waitWhilePaused
	// BEFORE its first Write, and only this test can release that park — so the
	// window below is a count of SCHEDULING OPPORTUNITIES rather than of wall
	// time: the writer is handed the processor sgrReplayParkedYields times and
	// the count must still be zero. Waiting on the clock here measured the
	// machine's speed, not the surface's behaviour, and it was the only thing
	// standing between this assertion and `-race` noticing a writer that never
	// ran at all.
	for i := 0; i < sgrReplayParkedYields; i++ {
		runtime.Gosched()
		if out.Len() != 0 {
			t.Fatalf("a paused replay wrote %q — pause must stop the picture, not slow it", out.String())
		}
	}

	ctl.toggle() // resume
	select {
	case status := <-done:
		if status != chatExitOK {
			t.Fatalf("status = %d, want %d", status, chatExitOK)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the replay did not finish after resume — the resume never reached the writer")
	}
	if got, want := out.String(), "onetwothree"; got != want {
		t.Fatalf("after resume stdout = %q, want %q", got, want)
	}
}

// TestChatSgrReplayQuitEndsTheRun pins the departure: once the watcher leaves, the
// remaining frames are NOT written and the status is 0 — an exit the watcher asked
// for is the surface working, not a failure.
func TestChatSgrReplayQuitEndsTheRun(t *testing.T) {
	frames := [][]byte{[]byte("one"), []byte("two"), []byte("three")}
	ctl := newChatReplayControl(func(string, ...any) {})
	var out, errOut bytes.Buffer
	// Pause FIRST, so the writer is parked before any byte is written, then leave.
	// NOTHING IS WAITED FOR HERE, and that is the whole point: the pause is taken
	// BEFORE the goroutine exists, play() checks quitRequested before its first
	// Write, and ctl.leave() releases the park through the same mutex the driver
	// reads — so the departure is ordered by the lock, not by a settle delay.
	// (The leave-while-paused release itself is pinned by
	// TestChatReplayControlQuitReleasesThePauseAndBecomesVisible.)
	ctl.toggle()
	done := make(chan int, 1)
	go func() { done <- chatSgrReplayPlay(frames, &out, &errOut, 0, ctl) }()
	ctl.leave()

	select {
	case status := <-done:
		if status != chatExitOK {
			t.Fatalf("status = %d, want %d: a watcher's departure is the surface working", status, chatExitOK)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the replay kept running after the watcher left")
	}
	if out.Len() != 0 {
		t.Fatalf("stdout = %q, want nothing: the run was still parked when the watcher left", out.String())
	}
}

// TestChatSgrReplayWriteFailureIsReported pins Rule 15 on the one failure a playback
// can meet mid-picture: a writer that refuses. The status is non-zero and the reason
// is on stderr — never a quiet "0" over a picture that did not arrive.
func TestChatSgrReplayWriteFailureIsReported(t *testing.T) {
	ctl := newChatReplayControl(func(string, ...any) {})
	var errOut bytes.Buffer
	status := chatSgrReplayPlay([][]byte{[]byte("x")}, failingWriter{}, &errOut, 0, ctl)
	if status == chatExitOK {
		t.Fatal("a failed write reported success — the picture did not reach anyone")
	}
	if !strings.Contains(errOut.String(), "cannot write") {
		t.Fatalf("stderr = %q, want the write failure named", errOut.String())
	}
}

// failingWriter refuses every write, standing in for a closed pipe or a gone tty.
type failingWriter struct{}

func (failingWriter) Write(p []byte) (int, error) {
	return 0, errors.New("stdout is gone")
}

// TestChatSgrReplaySessionIDFindsThePreRenameStream is the REGRESSION for a defect
// this rename introduced and a real invocation caught: the session-id branch of
// chatResolveSgrReplayPath composed ONE file name — `uiclient.ScreenStreamSuffix` —
// and stat'd it, so a session recorded BEFORE the sgr→screen rename (whose bytes
// live in `.sgr.stream`) resolved to nothing and the verb answered "no screen.stream
// stream for <id>" about a recording that was right there. Measured on this host
// before the fix: 177 `.sgr.stream` files against 4 `.screen.stream`, i.e. virtually
// every recording was unreachable BY SESSION ID (the path form worked, because it
// takes the file the caller named).
//
// The assertion is on the RESOLVED PATH, not on a sentence: the writer's suffix and
// the legacy one are both planted under the SAME session, and the resolver must land
// on the legacy file when it is the only one. That is what makes the test fail for
// the real reason — a single-name composition — rather than for a message change.
func TestChatSgrReplaySessionIDFindsThePreRenameStream(t *testing.T) {
	const legacySessionID = "5f1a0c3e-0b7d-4f52-9c11-2ab7e0d4c801"
	home := t.TempDir()
	t.Setenv("GOPI_HOME", home)
	dir := filepath.Join(home, config.ProfileDirName, config.SessionsDirName, legacySessionID)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatalf("create the session container %s: %v", dir, err)
	}
	// The record: one session_start, which is what names the session (the id is
	// read back out of the RECORD, not out of the directory name).
	record := filepath.Join(dir, legacySessionID+sessionlayout.JSONLSuffix)
	if err := os.WriteFile(record,
		[]byte(`{"type":"session_start","sessionId":"`+legacySessionID+`"}`+"\n"), 0o600); err != nil {
		t.Fatalf("write the session record %s: %v", record, err)
	}
	// The RECORDING: a real stream, written by the production writer and then named
	// with the PRE-RENAME suffix — exactly the shape of an artifact this host already
	// holds (nothing new is ever written under the old name; only read).
	frames := []string{"\x1b[2J\x1b[1;1Hlegacy", "\x1b[2;1Hpaint"}
	encoded, want := sgrReplayFixture(t, frames, true)
	legacyPath := filepath.Join(dir, legacySessionID+uiclient.LegacyScreenStreamSuffix)
	if err := os.Rename(encoded, legacyPath); err != nil {
		t.Fatalf("give the fixture the pre-rename name %s: %v", legacyPath, err)
	}

	got, err := chatResolveSgrReplayPath(legacySessionID)
	if err != nil {
		t.Fatalf("a session id whose only recording carries the PRE-RENAME suffix must still resolve, got error: %v", err)
	}
	if got != legacyPath {
		t.Fatalf("resolved %q, want the pre-rename recording %q — the resolver composed one name instead of walking uiclient.ScreenStreamSuffixes", got, legacyPath)
	}

	// And the resolved path plays: the bytes a caller gets are the frames, verbatim.
	out, errOut, status := replayCapture(t, got)
	if status != chatExitOK {
		t.Fatalf("replay of the resolved pre-rename path exited %d; stderr = %q", status, errOut)
	}
	if out != string(want) {
		t.Fatalf("stdout = %q, want the recording's frames %q", out, string(want))
	}

	// The NEW suffix still wins when BOTH exist (newest first), so "walk the list"
	// never means "the old name shadows the current one".
	newPath := filepath.Join(dir, legacySessionID+uiclient.ScreenStreamSuffix)
	if err := os.WriteFile(newPath, screenRenameFixtureBytes(t, legacyPath), 0o600); err != nil {
		t.Fatalf("plant the current-name recording %s: %v", newPath, err)
	}
	got, err = chatResolveSgrReplayPath(legacySessionID)
	if err != nil {
		t.Fatalf("resolve with both suffixes present: %v", err)
	}
	if got != newPath {
		t.Fatalf("resolved %q, want the CURRENT-name recording %q — ScreenStreamSuffixes[0] is what a resolver prefers", got, newPath)
	}
}

// screenRenameFixtureBytes is the fixture reader for the arm above; a fixture that cannot be
// read is a broken test, never a skip.
func screenRenameFixtureBytes(t *testing.T, path string) []byte {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read fixture %s: %v", path, err)
	}
	return b
}
