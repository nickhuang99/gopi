package main

// chat_replay_geometry.go — WHICH CANVAS a replay (and the film shot from it)
// is drawn at.
//
// THE DEFECT THIS FILE CLOSES. A session was recorded AT a terminal size and
// the recording did not say which; `--replay` therefore drew every re-run at the
// chat surface's built-in ideal size instead of the size the session actually
// happened on, and the operator had no way to tell the difference — the screen
// looked plausible and was not the recorded one. Two facts are fixed here:
//
//   - the recording now CARRIES the geometry (internal/agent writes
//     sessionevent.TerminalGeometry into session_start, and metadata.json);
//   - the replay USES it — as this run's ideal geometry for the surface, and as
//     the cell count the film is shot at — with the source of that number SAID
//     OUT LOUD on stderr, because a canvas chosen by a recording and a canvas
//     chosen by a fallback are two different facts about a picture (Rule 15).
//
// WHAT WINS, AND WHY THAT ORDER. An explicit `--chat-ideal-geometry` (or the
// same value in GOPI_CHAT_IDEAL_GEOMETRY) is the operator stating a size on this
// command line: it wins outright. Otherwise the RECORDING's own size wins — a
// replay reproduces a session, and the session's terminal is part of what it
// was — and the settings key's declaration is deliberately shadowed by it. That
// shadowing is not silent: the notice names the value it overrode and where that
// value came from.
//
// THE FALLBACK IS VISIBLE, ALWAYS. An older session (recorded before the field
// existed) or a session recorded with no terminal has no geometry; the run then
// uses the built-in ideal and says which of those two it was. Those sessions
// still replay — the fallback is a notice, never a refusal.

import (
	"fmt"
	"io"

	"github.com/nick/gopi/internal/config"
	"github.com/nick/gopi/internal/replaydrive"
)

// The notices. Each names the SOURCE of the number, because "this canvas is the
// recording's" and "this canvas is the built-in default because the recording
// had none" are the two facts a reader must be able to tell apart.
const (
	// chatReplayGeometryRecordedFmt fires when the recording's own geometry is
	// used. Args: cols, rows, the ideal it replaced (cols, rows), and that
	// ideal's source label.
	chatReplayGeometryRecordedFmt = chatReplayNotice +
		"the recording's terminal was %d" + config.ChatIdealGeometryDimSep + "%d — this run draws its canvas at that size " +
		"(it replaces the ideal %d" + config.ChatIdealGeometryDimSep + "%d from %s)\n"

	// chatReplayGeometryStatedFmt fires when the caller stated a size, so the
	// recording's is deliberately NOT used. Args: cols, rows.
	chatReplayGeometryStatedFmt = chatReplayNotice +
		"drawing at the size this command line states (%d" + config.ChatIdealGeometryDimSep + "%d); " +
		"the recording's own terminal size does not override it\n"

	// chatReplayGeometryAbsentFmt fires for a recording with NO geometry field
	// (a session recorded before the field existed). Args: the fallback size,
	// its source label.
	chatReplayGeometryAbsentFmt = chatReplayNotice +
		"the recording carries no terminal geometry (recorded before sessions recorded one) — " +
		"falling back to the chat surface's ideal %s (%s); the picture is not the size the session was made on\n"

	// chatReplayGeometryUnknownFmt fires for a recording that explicitly says
	// its terminal was unknown. Args: the recorded reason, the fallback size,
	// its source label.
	chatReplayGeometryUnknownFmt = chatReplayNotice +
		"the recording's terminal was UNKNOWN (%s) — falling back to the chat surface's ideal %s (%s)\n"

	// chatReplayGeometryUnreadableFmt fires when the geometry field exists but
	// cannot be read. Args: the error. The run continues on the fallback, and
	// the sentence says the recording's answer was not usable rather than
	// pretending the recording had none.
	chatReplayGeometryUnreadableFmt = chatReplayNotice +
		"the recording's terminal geometry could not be read (%v) — falling back to the chat surface's ideal\n"

	// chatFilmGeometryRecordedFmt is the FILM's half: the driver prints it
	// before it starts a display server, so the cell count the frames will be
	// shot at is attributable to the recording rather than to the default.
	// Args: cols, rows.
	chatFilmGeometryRecordedFmt = chatFilmNotice +
		"shooting at the recording's own terminal size %d" + config.ChatIdealGeometryDimSep + "%d " +
		"(no %s/%s was given on this command line)\n"

	// chatFilmGeometryFallbackFmt fires when neither the command line nor the
	// recording supplies a size. Args: the fallback size, the reason.
	chatFilmGeometryFallbackFmt = chatFilmNotice +
		"shooting at the chat surface's ideal %s — %s; the frames are not the size the session was made on\n"

	// chatFilmGeometryHalfStatedFmt fires when only ONE of the two dimensions
	// was stated, so the other keeps the ideal. Args: the stated word, the
	// other word, the ideal size.
	chatFilmGeometryHalfStatedFmt = chatFilmNotice +
		"only %s was given — %s keeps the chat surface's ideal %s\n"

	// chatFilmGeometryUnreadableFmt fires when the recording's geometry could
	// not be read at all (the file could not be resolved or its header could not
	// be decoded). Args: the error.
	chatFilmGeometryUnreadableFmt = chatFilmNotice +
		"the recording's terminal geometry could not be read (%v)\n"
)

// The reasons the film's fallback sentence can carry, spelled once each
// (Rule 1b) because the notice and the test name the same words.
const (
	chatFilmGeomReasonNoRecording = "the recording was not readable, so its terminal size is unknown"
	chatFilmGeomReasonNoField     = "the recording carries no terminal geometry"
	chatFilmGeomReasonUnknownTerm = "the recording's terminal was recorded as unknown"

	// chatFilmBeyondRecordingFmt is the PRE-FLIGHT REFUSAL for a --cols/--rows
	// that asks for more than the recording's own terminal had. It is the ONE
	// sentence that turns the silent end of a film into a named failure, and it
	// carries the three facts the operator needs: the value they ASKED for, the
	// value the RECORDING had, and the reason the two cannot be reconciled —
	// the replay canvas IS the recorded size, so the surface refuses the
	// terminal this driver starts and the run would end with no frame at all.
	// Args: the flag word, the asked value, the recorded value on that axis, the
	// recorded geometry's cols, the dimension separator, its rows, the dimension
	// word, the asked value again, the flag word, the recorded value again.
	chatFilmBeyondRecordingFmt = "%s asked for %d, but the recording's %s was %d (recorded geometry %d%s%d) — a replay draws its canvas at the size the session was RECORDED at, so the surface refuses the terminal this driver starts and the film would end with no frame at all. Use %s %d (or less), or re-record the session at that size"

	// chatFilmDimWidth / chatFilmDimHeight name the axis a refusal is about, so
	// the sentence reads as English rather than as a flag dump (Rule 1b: the
	// words are spelled once and both arms of the check use them).
	chatFilmDimWidth  = "width"
	chatFilmDimHeight = "height"
)

// chatReplayApplyRecordedGeometry makes the recording's terminal size this run's
// IDEAL GEOMETRY, through the knob's ONE representation (Rule 2/13), and reports
// on errOut which source decided.
//
// It deliberately does nothing when the operator stated a size: this function's
// job is to fill a silence, and a stated value is not one.
func chatReplayApplyRecordedGeometry(rp *chatReplay, errOut io.Writer) {
	if rp == nil {
		return
	}
	stated := config.ChatIdealGeometry()
	if stated.Source == config.ChatIdealGeometryEnv {
		fmt.Fprintf(errOut, chatReplayGeometryStatedFmt, stated.Cols, stated.Rows)
		return
	}
	geo, ok, err := replaydrive.LoadRecordedGeometry(rp.rec.Path)
	switch {
	case err != nil:
		fmt.Fprintf(errOut, chatReplayGeometryUnreadableFmt, err)
	case !ok:
		fmt.Fprintf(errOut, chatReplayGeometryAbsentFmt, config.ChatIdealGeometryDefault, stated.Source)
	case !geo.Known():
		fmt.Fprintf(errOut, chatReplayGeometryUnknownFmt, geo.Reason, config.ChatIdealGeometryDefault, stated.Source)
	default:
		config.PublishChatIdealGeometryFlag(chatGeometryValue(geo.Cols, geo.Rows))
		fmt.Fprintf(errOut, chatReplayGeometryRecordedFmt, geo.Cols, geo.Rows, stated.Cols, stated.Rows, stated.Source)
	}
}

// chatGeometryValue composes the knob's COLSxROWS spelling from two numbers —
// the SAME shape every channel parses (config.ChatIdealGeometryDimSep), so a
// published value can never be one the resolver refuses.
func chatGeometryValue(cols, rows int) string {
	return fmt.Sprintf("%d%s%d", cols, config.ChatIdealGeometryDimSep, rows)
}

// chatFilmGeometry decides the CELL COUNT the film is shot at:
//
//	--cols/--rows  >  the recording's own terminal size  >  the surface's ideal
//
// and reports which one decided on errOut. The film driver asks this BEFORE it
// starts a display server or spawns the producer, so the numbers it arrives at
// are the ones the producer's own `--cols/--rows` will carry and the ones the
// frames will be measured against.
//
// A refusal (a stated count ABOVE what the recording was made at — see
// chatFilmRefuseBeyondRecording) is RETURNED rather than resolved: the caller
// ends the run non-zero before it pays for a display server, a terminal or a
// producer, which is the whole point of asking here.
func chatFilmGeometry(beat chatBeatFlag, replayTarget string, errOut io.Writer) (cols, rows int, err error) {
	// The recording is READ ONCE, quietly: a stated run never consults it for
	// its canvas, so reading it must not print anything on that path — the
	// notices below belong to the runs that USE the answer (Rule 15: a notice
	// states a decision this run made).
	recorded, reason, rerr := chatFilmRecordedGeometry(replayTarget)
	if refErr := chatFilmRefuseBeyondRecording(beat, recorded, rerr == nil && reason == ""); refErr != nil {
		return 0, 0, refErr
	}
	if beat.colsSet || beat.rowsSet {
		// A half-stated pair is not a refusal (the operator sized the film and
		// the other dimension keeps the default), but it is not silent either.
		stated, other := chatFlagCols, chatFlagRows
		if !beat.colsSet {
			stated, other = chatFlagRows, chatFlagCols
		}
		if !(beat.colsSet && beat.rowsSet) {
			fmt.Fprintf(errOut, chatFilmGeometryHalfStatedFmt,
				stated, other, chatIdealGeometryLabel())
		}
		return beat.cols, beat.rows, nil
	}
	if rerr != nil {
		fmt.Fprintf(errOut, chatFilmGeometryUnreadableFmt, rerr)
	}
	if recorded.Known() {
		fmt.Fprintf(errOut, chatFilmGeometryRecordedFmt, recorded.cols, recorded.rows, chatFlagCols, chatFlagRows)
		return recorded.cols, recorded.rows, nil
	}
	fcols, frows := chatFallbackFilmGeometry(reason, errOut)
	return fcols, frows, nil
}

// chatFilmRefuseBeyondRecording is the PRE-FLIGHT CHECK the film driver owes the
// operator before it starts anything: `--cols/--rows` states the canvas THIS
// film will be shot at, while a replay draws its canvas at the size the SESSION
// was RECORDED at (chat_replay_geometry.go's header) — so a stated count ABOVE
// the recording's is not a bigger picture, it is a canvas the surface cannot
// draw at all.
//
// WHAT IT COSTS WHEN IT IS MISSING (measured 2026-10-11, before this function
// existed). The driver started a display server, a terminal and the producer at
// the stated 113x25; the producer's surface found its own terminal was the
// recording's 112x25 and refused with "this terminal is 112x25 but the beat
// protocol was asked for 113x25"; the producer exited 1 before its first beat,
// and the film ended with 0 frame(s) and an EMPTY terminal.log — the reason
// living only inside producer.log, which the run's own closing report merely
// pointed at. The refusal is now made HERE, where the recording has just been
// read, so the operator is told the two numbers and the flag that fits before a
// display server is paid for.
//
// Both axes are checked, and each refusal names its OWN axis, because the two
// are separately actionable: a width that exceeds the recording cannot be fixed
// by lowering --rows. A STATED value only — the recording's own size and the
// built-in ideal both come from the recording or from the designed default, and
// neither can exceed it.
func chatFilmRefuseBeyondRecording(beat chatBeatFlag, recorded terminalGeometryView, known bool) error {
	if !known {
		// No recorded size means there is no boundary to exceed: the run falls
		// back to the surface's ideal, which the run itself states out loud
		// (chatFilmGeometryFallbackFmt). Refusing here would invent a bound.
		return nil
	}
	if err := chatFilmBeyondAxis(beat.colsSet, beat.cols, recorded, chatFlagCols, chatFilmDimWidth); err != nil {
		return err
	}
	return chatFilmBeyondAxis(beat.rowsSet, beat.rows, recorded, chatFlagRows, chatFilmDimHeight)
}

// chatFilmBeyondAxis is ONE axis of the check above, so the two dimensions are
// compared by the same comparison and each refusal names the axis it is about
// (Rule 14: one comparison, two readers).
func chatFilmBeyondAxis(set bool, asked int, recorded terminalGeometryView, flag, dim string) error {
	if !set || asked <= recorded.axis(dim) {
		return nil
	}
	return fmt.Errorf(chatFilmBeyondRecordingFmt,
		flag, asked, dim, recorded.axis(dim),
		recorded.cols, config.ChatIdealGeometryDimSep, recorded.rows,
		flag, recorded.axis(dim))
}

// axis answers the cell count on the dimension the word names. It takes the
// dimension WORD rather than a second pair of arguments because the two callers
// read the same geometry and differ only in which of its two numbers they hold
// the stated value against.
func (v terminalGeometryView) axis(dim string) int {
	if dim == chatFilmDimHeight {
		return v.rows
	}
	return v.cols
}

// chatFilmRecordedGeometry reads the recording's own terminal size for the
// pre-flight check above, through the SAME reader chatRecordedGeometryOrFallback
// uses (Rule 14: one reader of the one field, so a check and a decision cannot
// disagree about what the recording said).
//
// It writes NOTHING: the check runs on every film, including a fully stated one
// that never uses the answer, and a notice about a fallback this run did not
// take would be a lie. The reason is RETURNED so the caller decides whether to
// speak (Rule 15: an unreported fact is the caller's, never a silent drop of
// the reader's).
func chatFilmRecordedGeometry(replayTarget string) (geo terminalGeometryView, reason string, err error) {
	path, err := resolveReplayRecordingPath(replayTarget)
	if err != nil {
		return terminalGeometryView{}, chatFilmGeomReasonNoRecording, err
	}
	measured, ok, gerr := replaydrive.LoadRecordedGeometry(path)
	switch {
	case gerr != nil:
		return terminalGeometryView{}, chatFilmGeomReasonNoRecording, gerr
	case !ok:
		return terminalGeometryView{}, chatFilmGeomReasonNoField, nil
	case !measured.Known():
		return terminalGeometryView{}, chatFilmGeomReasonUnknownTerm, nil
	}
	return terminalGeometryView{cols: measured.Cols, rows: measured.Rows}, "", nil
}

// chatFallbackFilmGeometry is the last rung of the film's resolution: the chat
// surface's own ideal size, with the reason it was needed printed.
func chatFallbackFilmGeometry(reason string, errOut io.Writer) (cols, rows int) {
	fmt.Fprintf(errOut, chatFilmGeometryFallbackFmt, chatIdealGeometryLabel(), reason)
	return chatBeatIdealCols(), chatBeatIdealRows()
}

// terminalGeometryView is the two numbers this file needs, kept local so the
// decision above is a pure function of "which size, if any, did the recording
// name" rather than of a package's whole type.
type terminalGeometryView struct {
	cols int
	rows int
}

// Known reports whether the view carries a real size (Rule 15a: 0 is not a
// size).
func (v terminalGeometryView) Known() bool { return v.cols > 0 && v.rows > 0 }

// chatIdealGeometryLabel is the human spelling of the built-in default,
// composed from the knob's own constant so the help, the notices and the
// CONFIG.md row cannot drift apart.
func chatIdealGeometryLabel() string { return config.ChatIdealGeometryDefault }
