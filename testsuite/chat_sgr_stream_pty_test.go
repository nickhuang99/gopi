package testsuite

// chat_sgr_stream_pty_test.go — the RAW byte stream, measured on a REAL pty with
// the REAL binary: `gopi chat --sgr-stream=PATH` must record the renderer's bytes
// verbatim, and the recording must be enough to reconstruct the screen WITHOUT
// this program's help.
//
// WHY THIS CASE EXISTS AT ALL. `chat-screen.dump` is an INTERPRETATION: throttled
// to dumpMinInterval, reduced to the cells the grid model can express. A reader
// asking "did the renderer write the wrong cell, or did our reader mis-read the
// right one?" cannot answer it from that artifact, because both halves of the
// comparison come from the same interpreter. The raw leg exists to break that
// circle, so the case's judge is not the dump — it is the PTY ITSELF: the harness
// receives every byte the child wrote, and every recorded frame must appear there,
// in order, byte for byte. That is an INDEPENDENT instrument (the kernel's pty,
// not uiclient's writer), which is the whole point.
//
// The four claims, in the order the epic states them:
//
//	A. the artifact exists, carries bytes, and both its first and its last frame
//	   parse — a stream whose trailer is missing is a run that died mid-frame;
//	B. every frame is byte-identical to a run of bytes the pty really carried, in
//	   the pty's own order (an in-order subsequence walk: other writers may
//	   interleave, but a frame cannot be invented or reordered);
//	C. the frames replay through a FRESH lineedit.Screen at the pty's geometry and
//	   reproduce the dump's own grid — the reconstruction comes from the BYTES,
//	   never from the dump;
//	D. an answer LONGER than the screen leaves its head in the recording while the
//	   final screen has scrolled past it — i.e. the artifact carries history the
//	   interpreted grid structurally cannot.
//
// The dump is asserted, never trusted, for the same reason: it appears here as a
// SECOND opinion on the same bytes, and its own artifact must still parse (F's
// half that a live run can measure — byte-identity of a file whose record line
// carries a timestamp and a pid is the unit suite's reading, not this one's).

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/nick/gopi/internal/cliverbs"
	"github.com/nick/gopi/internal/lineedit"
	"github.com/nick/gopi/internal/testutil"
	"github.com/nick/gopi/internal/uiclient"
)

const (
	// sgrPTYCols / sgrPTYRows are the geometry the raw stream is recorded at: the
	// same 100x30 the dump case uses, wide enough that the reserved layout installs
	// (below the layout's usable row count the surface paints no layout at all and
	// this case would be measuring a different screen).
	sgrPTYCols = 100
	sgrPTYRows = 30
	// sgrPTYSettle is how long the case listens after the prompt before it reads
	// the artifacts: longer than the sink's staleness bound (dumpMinInterval) and
	// than one layout install, so a healthy run has finished painting.
	sgrPTYSettle = 900 * time.Millisecond
	// sgrPTYAnswerWords is how many words the scripted answer carries. It must
	// exceed one screen: at sgrPTYCols columns and sgrPTYRows rows a screen holds
	// roughly three thousand characters, and these words are chosen to be long
	// enough that the answer wraps well past it (claim D).
	sgrPTYAnswerWords = 1200
	// sgrPTYAnswerLanded is how long the case waits for the scripted answer's LAST
	// word to appear in the pty stream. A streamed answer arrives over many
	// fragments, so "the prompt came back" is too early a signal.
	sgrPTYAnswerLanded = 20 * time.Second
	// sgrPTYFrameTail is how much of the recorded stream a failure message quotes.
	sgrPTYFrameTail = 900
)

// sgrPTYAnswer is the scripted answer: numbered words, so the case can name the
// FIRST and LAST one and assert both against two different artifacts (the record
// must carry the first; the final screen must have scrolled past it).
func sgrPTYAnswer() (string, string) {
	var b strings.Builder
	first := ""
	for i := 0; i < sgrPTYAnswerWords; i++ {
		w := "answerword" + itoaWord(i)
		if i == 0 {
			first = w
		}
		if i > 0 {
			b.WriteByte(' ')
		}
		b.WriteString(w)
	}
	return b.String(), first
}

// itoaWord spells a small non-negative int without importing strconv twice over
// the file's other needs.
func itoaWord(n int) string {
	if n == 0 {
		return "0"
	}
	var d []byte
	for n > 0 {
		d = append([]byte{byte('0' + n%10)}, d...)
		n /= 10
	}
	return string(d)
}

// TestChatSgrStreamPTY_RecordsTheRenderersOwnBytes is the acceptance case.
func TestChatSgrStreamPTY_RecordsTheRenderersOwnBytes(t *testing.T) {
	home := t.TempDir()
	work := t.TempDir()
	answer, firstWord := sgrPTYAnswer()
	lastWord := "answerword" + itoaWord(sgrPTYAnswerWords-1)
	llm := testutil.NewMockLLM(t, testutil.Resp{Text: answer})
	defer llm.Close()

	// Both artifacts of ONE run, each at a path of this case's own, so no reading
	// can be a leftover from another run.
	dir := t.TempDir()
	dumpPath := filepath.Join(dir, uiclient.ChatDumpFileName)
	sgrPath := filepath.Join(dir, "accept"+uiclient.ScreenStreamSuffix)
	chat := startPTYChat(t, []string{
		"chat",
		"--dump-screen=" + dumpPath,
		cliverbs.ScreenStream + "=" + sgrPath,
	}, testutil.MockEnv(t, home, llm.URL()), work)
	chat.resize(t, sgrPTYCols, sgrPTYRows)

	stream := chat.waitFor(t, chatPromptMark, ptyPromptTimeout)
	if !strings.Contains(stream, sgrPath) {
		t.Fatalf("the run did not announce the raw stream's path before taking the terminal; the armed artifact would be a file nobody can find.\n--- stream ---\n%s", tailOf(stream, sgrPTYFrameTail))
	}

	// One turn, so the recording covers a paint sequence that CHANGES rather than
	// a single startup frame. The text is typed GAPPED and the Enter is sent
	// separately after the editor's own post-burst window: a single write of
	// fourteen bytes is read as PASTED content and its trailing Enter is
	// suppressed (measured on this harness: "the newline is in the buffer; press
	// Enter again once the window has passed to send").
	chat.writeGapped(t, "say the words")
	time.Sleep(ptyPostBurstGap) //stylegate:allow-sleep the editor's own Enter-suppression window, quoted above
	chat.write(t, "\r")
	stream += chat.waitFor(t, lastWord, sgrPTYAnswerLanded)
	stream += chat.collect(t, sgrPTYSettle)

	// ---- A. the artifact exists and both ends parse ------------------------
	blob, err := os.ReadFile(sgrPath)
	if err != nil {
		t.Fatalf("the raw stream is absent at %s although the run announced it: %v\n--- stream (tail) ---\n%s", sgrPath, err, tailOf(stream, sgrPTYFrameTail))
	}
	if len(blob) == 0 {
		t.Fatalf("the raw stream at %s is EMPTY — the switch armed and recorded nothing", sgrPath)
	}
	meta, frames, err := uiclient.ReadSgrFrames(bytes.NewReader(blob))
	if err != nil {
		t.Fatalf("the raw stream at %s does not parse (%d bytes): %v", sgrPath, len(blob), err)
	}
	// The TRAILER is asserted AFTER the exit, below, and not here: it is written
	// by the close-out (uiclient.ScreenDumper.Close → SgrStream.Close), so a
	// reading taken while the run is still painting CANNOT have one — asserting
	// it here would measure the read's timing, not the artifact.
	if meta.Version != uiclient.SgrStreamVersion {
		t.Errorf("header version = %d, want %d", meta.Version, uiclient.SgrStreamVersion)
	}
	if meta.Grid != [2]int{sgrPTYCols, sgrPTYRows} {
		t.Errorf("header grid = %v, want [%d %d] (the geometry the pty was given)", meta.Grid, sgrPTYCols, sgrPTYRows)
	}
	if len(frames) == 0 {
		t.Fatalf("the raw stream parsed to ZERO frames with %d bytes on disk", len(blob))
	}
	if meta.Frames != len(frames) {
		t.Errorf("trailer says frames=%d but %d frames parsed", meta.Frames, len(frames))
	}
	if meta.Bytes <= 0 {
		t.Errorf("trailer says bytes=%d", meta.Bytes)
	}

	// ---- B. every frame is bytes the pty REALLY carried, in ITS order ------
	// An in-order walk, not a plain containment test: some writes on this stream
	// do not pass through the tee (the layout's own installs), so frames may be
	// separated by other output — but a frame may never be invented, reordered, or
	// altered by one byte.
	at := 0
	for i, f := range frames {
		idx := strings.Index(string(stream[at:]), string(f))
		if idx < 0 {
			t.Fatalf("frame %d (%d bytes) does not appear in the pty's own byte stream after offset %d: the recording carries bytes the terminal never got.\n--- frame %d ---\n%q\n--- pty stream (tail) ---\n%q",
				i, len(f), at, i, string(f), tailOf(stream, sgrPTYFrameTail))
		}
		at += idx + len(f)
	}

	// ---- C. the frames REPLAY into the screen the dump describes ------------
	// A fresh interpreter, fed nothing but the recorded bytes: if this matches the
	// dump, then the artifact is a complete account of the paint sequence, not a
	// sample of it. (The dump is the second opinion here, never the source.)
	cand, err := uiclient.ReadDump(dumpPath)
	if err != nil {
		t.Fatalf("read the chat dump %s: %v", dumpPath, err)
	}
	replay := lineedit.NewScreen(sgrPTYCols, sgrPTYRows)
	for _, f := range frames {
		replay.Feed(f)
	}
	// The rows carrying the editing line and the last answer line are the ones a
	// reader acts on; they are found in the REPLAY and compared to the dump's own
	// rendering of the same rows.
	promptRow := -1
	for r := 0; r < sgrPTYRows; r++ {
		if strings.Contains(replay.Line(r), chatPromptMark) {
			promptRow = r
			break
		}
	}
	if promptRow < 0 {
		t.Fatalf("the replay of the recorded bytes carries no %q prompt on any row: the recording does not reconstruct the editing line.\n--- replay ---\n%s",
			chatPromptMark, replayText(replay))
	}
	if !strings.Contains(strings.Join(cand.Dump.Lines, "\n"), chatPromptMark) {
		t.Fatalf("the dump has no %q prompt either — the two artifacts disagree about a screen both observed.\n--- dump ---\n%s",
			chatPromptMark, strings.Join(cand.Dump.Lines, "\n"))
	}
	// The comparison is on the rows that carry content, at the END of the run: the
	// dump was finalised at close-out and the replay consumed every frame, so both
	// describe the same instant.
	for r := 0; r < sgrPTYRows && r < len(cand.Dump.Lines); r++ {
		want := cand.Dump.Lines[r]
		got := replay.Line(r)
		if got != want {
			t.Fatalf("row %d diverges between the replay of the RAW bytes and the dump: a reader of the raw artifact would see a different screen.\n replay=%q\n   dump=%q\n--- replay ---\n%s",
				r, got, want, replayText(replay))
		}
	}

	// ---- D. the recording carries history the grid cannot -----------------
	// The answer is longer than the screen. Its FIRST word must be in the recorded
	// BYTES (it was painted) and must NOT be on the final screen (it scrolled out):
	// that gap is exactly the evidence that the raw leg is not a second copy of the
	// grid.
	joined := string(bytes.Join(frames, nil))
	if !strings.Contains(joined, firstWord) {
		t.Fatalf("the recording does not carry the answer's first word %q, although the run painted it: the raw leg missed a frame.\n--- frames (tail) ---\n%q",
			firstWord, tailOf(joined, sgrPTYFrameTail))
	}
	if strings.Contains(strings.Join(cand.Dump.Lines, "\n"), firstWord) {
		t.Fatalf("the final screen still carries the answer's first word %q — the answer did not scroll, so this case cannot prove the recording keeps history. Raise sgrPTYAnswerWords or shrink the geometry.", firstWord)
	}
	if int64(len(joined)) <= int64(sgrPTYCols*sgrPTYRows) {
		t.Fatalf("the recording carries %d payload bytes for a %dx%d screen with a %d-word answer: too little to prove it captured past one screenful",
			len(joined), sgrPTYCols, sgrPTYRows, sgrPTYAnswerWords)
	}

	// The run is ended through its OWN input: Ctrl+C at the prompt is "exit
	// cleanly", and the suite's close-out sweep counts live gopi processes.
	chat.write(t, ptyCtrlC)
	chat.close(t)

	// The trailer is written by the CLOSE-OUT, so re-read the artifact after the
	// exit: a stream that only looks terminated mid-run is exactly the failure
	// this ordering exists to catch.
	finalBlob, err := os.ReadFile(sgrPath)
	if err != nil {
		t.Fatalf("re-read the raw stream after exit: %v", err)
	}
	finalMeta, finalFrames, err := uiclient.ReadSgrFrames(bytes.NewReader(finalBlob))
	if err != nil {
		t.Fatalf("the closed stream does not parse: %v", err)
	}
	if !finalMeta.Trailer {
		t.Fatalf("the closed stream has no trailer: Close did not run on the raw leg")
	}
	if len(finalFrames) < len(frames) {
		t.Fatalf("frames went from %d to %d across the close-out", len(frames), len(finalFrames))
	}
	// The trailer's `grid=` is the geometry the run ENDED at, and it is a
	// DIFFERENT field from the header's: the pair is what lets a reader say
	// "this stream was recorded across a resize". It has to be readable here
	// because this is the only place a REAL close-out writes a real trailer —
	// a unit test can hand the reader a blob, but only this run can prove the
	// writer's own line and the reader's own decode agree at the pty's geometry.
	if finalMeta.EndGrid != finalMeta.Grid {
		t.Errorf("trailer grid = %v but header grid = %v: this run never resized, so the two records must agree — and an EndGrid of [0 0] would mean the trailer's field is still unread (the write-only field this case now covers)",
			finalMeta.EndGrid, finalMeta.Grid)
	}
	t.Logf("raw stream: %s (%d bytes on disk, %d frames, %d payload bytes, grid %dx%d (ended %dx%d), revision %q)",
		sgrPath, len(finalBlob), len(finalFrames), finalMeta.Bytes, finalMeta.Grid[0], finalMeta.Grid[1],
		finalMeta.EndGrid[0], finalMeta.EndGrid[1], finalMeta.Revision)
}

// TestChatSgrStreamPTY_DeclinedBySwitchWritesNothing is the OFF half on the same
// instrument. The stream is ARMED BY DEFAULT now, so the decline is a DECISION the
// caller makes on the command line (`--no-sgr-stream`) — and a decision must leave
// no artifact anywhere it could plausibly have put one: beside the dump, and in a
// fresh session directory.
//
// The second half is Rule 15 on this switch: the run must SAY it declined. An
// artifact that is on by default and silently absent is indistinguishable from a
// switch that failed to arm, so the case asserts BOTH sentences (the startup
// notice and the closing report) beside the absence of the file.
func TestChatSgrStreamPTY_DeclinedBySwitchWritesNothing(t *testing.T) {
	home := t.TempDir()
	work := t.TempDir()
	llm := testutil.NewMockLLM(t, testutil.Resp{Text: "not asked"})
	defer llm.Close()

	dir := t.TempDir()
	dumpPath := filepath.Join(dir, uiclient.ChatDumpFileName)
	chat := startPTYChat(t, []string{"chat", "--dump-screen=" + dumpPath, cliverbs.NoScreenStream},
		testutil.MockEnv(t, home, llm.URL()), work)
	chat.resize(t, sgrPTYCols, sgrPTYRows)
	stream := chat.waitFor(t, chatPromptMark, ptyPromptTimeout)
	stream += chat.collect(t, sgrPTYSettle)
	chat.write(t, ptyCtrlC)
	chat.close(t)

	// The decision is ANNOUNCED at startup and RECORDED at the close-out: the two
	// sentences that make "off" a statement rather than a silence.
	if !strings.Contains(stream, cliverbs.NoScreenStream) {
		t.Errorf("a run that declined the raw stream said nothing about it — the off state is silent:\n%s", tailOf(stream, sgrPTYFrameTail))
	}
	if strings.Contains(stream, uiclient.ScreenStreamHeaderPrefix) {
		t.Errorf("a declined run mentioned the raw stream's format")
	}
	// The dump IS there (it is on by default), which is what makes the absence
	// below a statement about the raw leg rather than about a run that did nothing.
	if _, err := os.Stat(dumpPath); err != nil {
		t.Fatalf("the run wrote no dump either (%v) — this case proves nothing about the raw leg", err)
	}
	suffix := uiclient.ScreenStreamSuffix
	err := filepath.Walk(home, func(path string, info os.FileInfo, err error) error {
		if err != nil || info == nil || info.IsDir() {
			return nil // stylegate:ignore a walk error is reported by the caller's own assertions
		}
		if strings.HasSuffix(path, suffix) {
			t.Errorf("a run that declined the raw stream left one at %s", path)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk the run's home: %v", err)
	}
	if entries, err := os.ReadDir(dir); err == nil {
		for _, e := range entries {
			if strings.HasSuffix(e.Name(), suffix) {
				t.Errorf("a run that declined the raw stream left one beside its dump: %s", filepath.Join(dir, e.Name()))
			}
		}
	}
}

// TestChatSgrStreamPTY_ArmedByDefault is the DEFAULT half on the same instrument,
// and it is the epic's subject: a run that says NOTHING about the raw stream must
// produce `<session-id>.screen.stream` in its own session directory, with the header
// at the front and the trailer at the back. The explicit-path case above cannot
// make this claim: it arms the leg by hand, so it would pass with the default off.
func TestChatSgrStreamPTY_ArmedByDefault(t *testing.T) {
	home := t.TempDir()
	work := t.TempDir()
	llm := testutil.NewMockLLM(t, testutil.Resp{Text: "not asked"})
	defer llm.Close()

	dir := t.TempDir()
	dumpPath := filepath.Join(dir, uiclient.ChatDumpFileName)
	chat := startPTYChat(t, []string{"chat", "--dump-screen=" + dumpPath}, testutil.MockEnv(t, home, llm.URL()), work)
	chat.resize(t, sgrPTYCols, sgrPTYRows)
	stream := chat.waitFor(t, chatPromptMark, ptyPromptTimeout)
	stream += chat.collect(t, sgrPTYSettle)

	// The path is announced BEFORE the layout takes the terminal: a file nobody
	// can find is the failure this notice exists to prevent.
	var sgrPath string
	err := filepath.Walk(home, func(path string, info os.FileInfo, err error) error {
		if err != nil || info == nil || info.IsDir() {
			return nil // stylegate:ignore a walk error is reported by the caller's own assertions
		}
		if strings.HasSuffix(path, uiclient.ScreenStreamSuffix) {
			sgrPath = path
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk the run's home: %v", err)
	}
	if sgrPath == "" {
		t.Fatalf("a default run recorded NO raw stream under %s — the switch is supposed to be armed by default:\n%s",
			home, tailOf(stream, sgrPTYFrameTail))
	}
	if !strings.Contains(stream, sgrPath) {
		t.Errorf("the run did not announce the raw stream at %s before taking the terminal", sgrPath)
	}

	chat.write(t, ptyCtrlC)
	chat.close(t)

	blob, err := os.ReadFile(sgrPath)
	if err != nil {
		t.Fatalf("read the default run's stream: %v", err)
	}
	meta, frames, err := uiclient.ReadSgrFrames(bytes.NewReader(blob))
	if err != nil {
		t.Fatalf("the default run's stream does not parse: %v", err)
	}
	if !meta.Trailer {
		t.Errorf("the default run's stream has no trailer — the close-out did not finalize it")
	}
	if !bytes.Contains(blob, []byte(uiclient.ScreenStreamHeaderPrefix)) {
		t.Errorf("the default run's stream carries no %q header", uiclient.ScreenStreamHeaderPrefix)
	}
	if !bytes.Contains(blob, []byte(uiclient.ScreenStreamTrailerPrefix)) {
		t.Errorf("the default run's stream carries no %q trailer", uiclient.ScreenStreamTrailerPrefix)
	}
	t.Logf("default-armed raw stream: %s (%d bytes, %d frames)", sgrPath, len(blob), len(frames))
}

// replayText renders a replayed screen for a failure message: the rows, trimmed.
func replayText(s *lineedit.Screen) string {
	var b strings.Builder
	for r := 0; r < s.Rows; r++ {
		b.WriteString(s.Line(r))
		b.WriteByte('\n')
	}
	return b.String()
}
