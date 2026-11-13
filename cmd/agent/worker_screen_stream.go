package main

// worker_screen_stream.go — the WORKER's half of the screen-byte recording.
//
// WHY A WORKER HAS ONE AT ALL (epic screen-stream, 2026-11-08). `gopi worker`
// paints no screen of its own, so for years the recording did not apply to it: the
// switch lived in the chat flag namespace (`GOPI_CHAT_SGR_STREAM`), and a worker
// process could not be asked on or off at all. The question an operator actually
// asks is not "which surface is this" but "what did this run put on the wire", and
// a round's own progress text is a stream a reader may want verbatim — the same
// class of evidence as a chat's paint bytes. So the switch is TERMINAL-AGNOSTIC
// (internal/config/screenstream.go is the one parser and the one precedence
// statement) and the worker arms the same `uiclient.SgrStream`.
//
// WHAT "EQUIVALENT OUTPUT" MEANS HERE, and what it deliberately does NOT mean. The
// stream records the bytes THIS RUN WROTE TO ITS OWN STREAM — `workerRoundAnnounce`
// (the round's record: the claim line, the workdir line, the close-out) and
// `worktreerelease.ReleaseReporting`. It does NOT record a terminal's answer, a
// repaint, or escape sequences the worker never emitted: a worker that is not on a
// tty has no terminal to be equivalent to, and claiming one would be the exact
// dishonesty this epic forbids. When stdout IS a tty the tee is invisible by
// construction — the same bytes reach the operator and the artifact.
//
// THE EMPTY RUN IS ANNOUNCED, NEVER FAKED (the epic's hard requirement). An armed
// round that emitted nothing — a claim refused before the first announce line, an
// empty queue — leaves a stream with a header, a trailer, `frames=0`, and NO
// payload. Writing that file and calling it "recorded" is the artifact a reader
// takes as evidence that a screen existed. So the close-out DELETES it and prints
// the one sentence, in the operator's own words:
//
//	本 run 无屏幕输出 (this run produced no screen output: N byte(s) emitted, 0 recorded frames) — the empty artifact <path> was removed, so no zero-payload stream survives to be mistaken for evidence
//
// The `ls` half of the evidence is the file's ABSENCE, which is why the removal is
// reported with the path it removed rather than performed quietly.
//
// THE LISTENER IS A PROCESS-WIDE LATCH, not a parameter threaded through a dozen
// round helpers: `workerRoundAnnounce` is the round's ONE stdout door
// (cli_tasks_worker.go), so redirecting that door covers the round's record
// completely, and a caller that forgets to pass a sink cannot silently record
// nothing.

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"

	"github.com/nick/gopi/internal/cliverbs"
	"github.com/nick/gopi/internal/config"
	"github.com/nick/gopi/internal/replayplay"
	"github.com/nick/gopi/internal/tools"
	"github.com/nick/gopi/internal/uiclient"
	"github.com/nick/gopi/internal/version"
)

// The worker's own vocabulary (Rule 1/13): the decision words, the report shapes
// and the sentence the epic requires verbatim.
const (
	// workerScreenStreamNoOutput is the required sentence. It is stated in the
	// operator's words because the requirement is about what a HUMAN reads in a
	// transcript, and a paraphrase would satisfy nobody reading it.
	workerScreenStreamNoOutput = "本 run 无屏幕输出"
	// workerScreenStreamReportFmt is the armed round's closing record: where the
	// artifact is, what it holds, and the sink's last error.
	workerScreenStreamReportFmt = "#gopi-worker-screen-stream-report path=%s bytes=%d frames=%d error=%q\n"
	// workerScreenStreamOffFmt is a DECLINED round's closing record. It is
	// printed even though nothing was recorded: the switch's off state is never
	// silent (Rule 15 — "no line" would be indistinguishable from "the run died
	// before it could say").
	workerScreenStreamOffFmt = "#gopi-worker-screen-stream-report decision=%s origin=%s source=%q\n"
	// workerScreenStreamEmptyFmt is the empty round's record, and it names the
	// file it removed so the absence is auditable rather than merely true.
	workerScreenStreamEmptyFmt = "#gopi-worker-screen-stream-report decision=empty path=%s emitted=%d — %s: 0 recorded frames, the empty artifact was removed, so no zero-payload stream survives to be mistaken for evidence\n"
	// workerScreenStreamGeometryFmt is the line a NON-TTY round prints about the
	// grid it is recording at. It is not decoration: the header carries
	// `grid=80x24`, which a replay reads as a MEASURED size
	// (replayplay.GeometryFromStream), and a stream whose grid was assumed rather
	// than discovered has to say which it was — otherwise the artifact claims a
	// terminal measurement this process never took (Rule 15).
	workerScreenStreamGeometryFmt = "worker: screen byte stream grid %dx%d — NOT measured: this process has no terminal, so the recording declares the program's documented default (replayplay.FallbackCols/FallbackRows) instead of inventing a size\n"
	// workerScreenStreamArmedFmt announces the destination BEFORE the round's
	// first line is written, so a file nobody can find is impossible.
	workerScreenStreamArmedFmt = "worker: screen byte stream → %s (%s)\n"
	// workerScreenStreamFailFmt reports an armed switch that could not produce
	// its artifact. It does NOT stop the round: an observation must never be able
	// to break what it observes (the same contract uiclient's Err keeps), but
	// silence is not an option either.
	workerScreenStreamFailFmt = "worker: screen byte stream could not be armed at %s (%v) — this round is NOT being recorded\n"
	// workerScreenStreamUnresolvedFmt is the round with an ARMED choice but no
	// session directory to put the artifact in. It is a distinct sentence from
	// the arming failure because the cause is different, and the epic's rule is
	// that a refusal names its own cause.
	workerScreenStreamUnresolvedFmt = "worker: screen byte stream is asked for, but the row's session directory is unknown (%v) — this round is NOT being recorded and no path was invented\n"
)

// workerScreenStreamSink is the ONE listener. It is a package-level latch for the
// reason the file header gives: workerRoundAnnounce is the round's single stdout
// door, and the round is single-threaded (the same reasoning as workerWriteback).
var workerScreenStreamSink *workerScreenStreamTee

// workerScreenStreamMu guards the latch against the write path: the tee is
// consulted from workerRoundAnnounce while the round's close-out swaps it back.
var workerScreenStreamMu sync.Mutex

// workerScreenStreamTee duplicates every announce write into the recording sink.
// A nil sink is the unarmed state, and it is not a special case worth a branch at
// the call site: the ONE door stays one door.
type workerScreenStreamTee struct {
	out  io.Writer
	sink *uiclient.SgrStream
}

// Write appends to the recording FIRST, then passes the bytes to the round's own
// stream. Order matters for a reader of the artifact: the frame is on disk before
// the operator's line is, so a stream is never missing the last line a transcript
// shows. The sink's error is recorded inside it (SgrStream.Err) and surfaces in
// the close-out report — never returned into the render/record path, which is the
// contract that keeps an observer from breaking what it observes.
func (t *workerScreenStreamTee) Write(p []byte) (int, error) {
	if t.sink != nil {
		_ = t.sink.Write(p) //stylegate:ignore the sink records this failure in SgrStream.Err and the close-out report prints it; returning it here would make an observer able to break the round
	}
	return t.out.Write(p)
}

// workerScreenStreamOpen resolves THIS round's choice and arms the sink.
//
// Precedence is not re-derived here (Rule 13): internal/config's
// ScreenStreamResolve states it once — argv > GOPI_SCREEN_STREAM > the SURFACE's
// default — and this function supplies only the two surface-specific inputs: no
// argv channel at all (a worker round has no command line of its own to carry the
// word; an operator who wants the bytes names them in the environment), and the
// worker's default, which is OFF (ScreenStreamDefaultWorker: a round's progress
// text is not a screen, so recording it is an opt-in answer).
func workerScreenStreamOpen(taskID string) *workerScreenStreamTee {
	workerScreenStreamOpened()
	choice := config.ScreenStreamResolve(config.ScreenStreamChoice{},
		config.ScreenStreamFromEnv(), config.ScreenStreamDefaultWorker)
	workerScreenStreamLegacyNotice(choice)
	if !choice.Armed() {
		fmt.Printf(workerScreenStreamOffFmt, choice.State, choice.Origin, choice.Source)
		// THE DECLINED ROUND IS ITS OWN REQUIREMENT'S CASE: nobody asked for an
		// artifact, so there is no empty file to remove and nothing was lost —
		// but the sentence still goes out, because "the run produced no screen
		// output" is a fact about the RUN, not about the switch.
		fmt.Println(workerScreenStreamNoOutput)
		return nil
	}
	path := choice.Path
	if path == "" {
		sessDir, err := tools.TaskSessionDir(taskID)
		if err != nil {
			fmt.Printf(workerScreenStreamUnresolvedFmt, err)
			fmt.Println(workerScreenStreamNoOutput)
			return nil
		}
		// The ROW's id names the artifact, which is also what
		// sessionlayout.SessionID derives for a container named after its row:
		// two runs that share a sessions root still produce two
		// distinguishable artifacts, and a reader holding only the task id can
		// compute the path without opening anything.
		path = filepath.Join(sessDir, taskID+uiclient.ScreenStreamSuffix)
	}
	if dir := filepath.Dir(path); dir != "" && dir != "." {
		if err := os.MkdirAll(dir, dumpDirPerm); err != nil {
			fmt.Printf(workerScreenStreamFailFmt, path, err)
			fmt.Println(workerScreenStreamNoOutput)
			return nil
		}
	}
	// THE GRID THE SINK IS BUILT AT — and a worker is usually NOT on a tty, so
	// this is where the honest answer differs from the chat's. `terminalGeometry`
	// returns 0,0 for a pipe (a redirected run, a launched-by-a-script run, a
	// `--daemon`), and a header of `grid=0x0` is a stream NO READER CAN PAINT:
	// measured on this host, `gopi session replay <id> --from stream` refuses it by
	// name — "declares grid=0x0, which cannot be painted: a screen needs both
	// dimensions". A recording that exists and cannot be replayed is not a
	// recording, so a non-tty falls back to the program's ONE documented default
	// (replayplay.FallbackCols/FallbackRows, the same 80x24 a replay assumes when
	// neither artifact states a size) and SAYS SO — a discovered size and a
	// default one are different facts (Rule 15).
	cols, rows := chatTerminalGeometry(os.Stdout)
	if cols <= 0 || rows <= 0 {
		cols, rows = replayplay.FallbackCols, replayplay.FallbackRows
		fmt.Printf(workerScreenStreamGeometryFmt, cols, rows)
	}
	sink, err := uiclient.OpenSgrStream(uiclient.SgrStreamConfig{
		Path:     path,
		Cols:     cols,
		Rows:     rows,
		Revision: version.Revision,
	})
	if err != nil {
		fmt.Printf(workerScreenStreamFailFmt, path, err)
		fmt.Println(workerScreenStreamNoOutput)
		return nil
	}
	tee := &workerScreenStreamTee{out: os.Stdout, sink: sink}
	fmt.Printf(workerScreenStreamArmedFmt, path, choice.Origin)
	workerScreenStreamMu.Lock()
	workerScreenStreamSink = tee
	workerScreenStreamMu.Unlock()
	return tee
}

// workerScreenStreamClose ends the round's recording and REPORTS what happened,
// which is three different outcomes and three different lines:
//
//   - frames > 0: the artifact is real, and its path/bytes/error go out;
//   - frames = 0: the requirement's case — the sentence, the removal, and the
//     absence that makes the removal checkable;
//   - nil tee: nothing was armed, and the OFF/FAILED line was already printed at
//     the front (this function says nothing twice).
//
// It RESTORES the door before it reports, so the report itself is not recorded
// into the artifact it describes (a stream whose last frame is its own obituary
// would be a frame the run never wrote to its stream).
func workerScreenStreamClose(tee *workerScreenStreamTee) {
	if tee == nil {
		return
	}
	workerScreenStreamMu.Lock()
	if workerScreenStreamSink == tee {
		workerScreenStreamSink = nil
	}
	workerScreenStreamMu.Unlock()
	sink := tee.sink
	path := sink.Path()
	frames := sink.Frames()
	payload := sink.Bytes()
	serr := sink.Err()
	if cerr := sink.Close(); cerr != nil && serr == "" {
		serr = cerr.Error()
	}
	if frames == 0 {
		// THE EMPTY ARTIFACT IS REMOVED, and the removal is reported with the
		// path: an armed run that wrote a header and a trailer and nothing else
		// has recorded NO screen, and a 0-frame file on disk is exactly the
		// "pretend we recorded" this epic forbids. `ls` showing nothing IS the
		// evidence, and this line is what makes it re-derivable.
		rmErr := os.Remove(path)
		if rmErr != nil {
			// Rule 15: the removal failing is its own finding, never folded into
			// the empty case. A 0-frame file that could NOT be removed must be
			// named, because it is now looking like evidence.
			fmt.Printf("#gopi-worker-screen-stream-report decision=empty path=%s emitted=%d — %s: 0 recorded frames, and the empty artifact COULD NOT be removed (%v): it is on disk and is NOT evidence of a screen\n",
				path, payload, workerScreenStreamNoOutput, rmErr)
			return
		}
		fmt.Printf(workerScreenStreamEmptyFmt, path, payload, workerScreenStreamNoOutput)
		return
	}
	fmt.Printf(workerScreenStreamReportFmt, path, payload, frames, serr)
}

// workerScreenStreamWriter is the door workerRoundAnnounce writes to. It is a
// FUNCTION (not a variable), so the latch cannot be reassigned from outside this
// file and the swap is one guarded read.
func workerScreenStreamWriter() io.Writer {
	workerScreenStreamMu.Lock()
	defer workerScreenStreamMu.Unlock()
	if workerScreenStreamSink == nil {
		return os.Stdout
	}
	return workerScreenStreamSink
}

// workerScreenStreamLegacyNotice reports the pre-rename spelling when it is the
// one that decided, so a shell profile written before the rename is visible
// rather than merely tolerated (Rule 15). The chat does the same
// (chatSgrEnvAliasWarn); this is its worker-side counterpart and it names the
// same pair of constants.
func workerScreenStreamLegacyNotice(choice config.ScreenStreamChoice) {
	if choice.Source != cliverbs.LegacyScreenStreamEnv {
		return
	}
	fmt.Printf("worker: the screen byte stream was switched by %s, the PRE-RENAME spelling of %s — export %s to stay current\n",
		cliverbs.LegacyScreenStreamEnv, cliverbs.ScreenStreamEnv, cliverbs.ScreenStreamEnv)
}

// workerScreenStreamRounds counts how many rounds this PROCESS opened a
// recorder for, armed or not. It exists for one question the loop boundary has to
// answer and no round can: "did anything speak about the recording at all?" A
// per-round report is the round's business; the loop only speaks when it had NO
// round to carry the report, which is the drained/idle case.
//
// It is a counter rather than a bool because the loop's own report is decided
// AFTER its rounds ran, and "at least one" is the only thing it needs — the
// count is free and it makes a test able to assert the ordering without a second
// instrument.
var workerScreenStreamRounds int

// workerScreenStreamOpen marks the round as having consulted the switch, so the
// loop boundary stays quiet about a run whose rounds already reported.
func workerScreenStreamOpened() { workerScreenStreamRounds++ }

// workerScreenStreamNoRound is the LOOP's boundary: a run that was asked for the
// recording and never ran a round (a drained or idle queue).
//
// WHY IT DOES NOT OPEN A STREAM. A stream with no round's lines would be exactly
// the empty artifact the epic forbids — a header, a trailer, `frames=0`, and a
// file on disk a reader would take as evidence that a screen existed. So this
// boundary opens NOTHING and only OWNS THE SENTENCE: report() prints it when (a)
// the switch was armed and no round armed a recorder, or (b) nobody ever spoke
// about the switch at all — the plain-off run, whose decision would otherwise be
// invisible (Rule 15: the off state is never silent, and an idle worker is a run
// too).
func watchWorkerScreenStreamNoRound() *workerScreenStreamNoRoundWatch {
	before := workerScreenStreamRounds
	choice := config.ScreenStreamResolve(config.ScreenStreamChoice{},
		config.ScreenStreamFromEnv(), config.ScreenStreamDefaultWorker)
	return &workerScreenStreamNoRoundWatch{choice: choice, before: before}
}

// workerScreenStreamNoRoundWatch is the loop boundary's capture: the resolved
// choice at the loop's start, and the round count it started from.
type workerScreenStreamNoRoundWatch struct {
	choice config.ScreenStreamChoice
	before int
}

// report is the loop's single line about the recording. It stays SILENT when a
// round already reported (workerScreenStreamRounds > before): two speakers for one
// run would make an operator count lines instead of reading one.
func (w *workerScreenStreamNoRoundWatch) report() {
	if w == nil || workerScreenStreamRounds > w.before {
		return
	}
	workerScreenStreamLegacyNotice(w.choice)
	if w.choice.Armed() {
		// ASKED AND NOTHING RAN: the epic's sentence, and no file — nothing was
		// opened, so there is nothing to remove and `ls` shows nothing to remove.
		fmt.Printf(workerScreenStreamEmptyFmt, "(none — no round ran)", 0, workerScreenStreamNoOutput)
		return
	}
	fmt.Printf(workerScreenStreamOffFmt, w.choice.State, w.choice.Origin, w.choice.Source)
}
