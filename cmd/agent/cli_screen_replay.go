package main

// cli_screen_replay.go — `gopi screen replay <session-id>`: write a recorded
// screen's render bytes back out, BYTE FOR BYTE.
//
// WHAT IT IS, AND WHAT IT DELIBERATELY IS NOT. `gopi session replay` is a
// RENDERER: it builds a canvas at a chosen geometry and falls back to the
// reduced dump when a session recorded no stream. This sub-verb is a WRITER: it
// reads the artifact, hands every frame's payload to stdout in recorded order,
// and changes nothing about them — no canvas, no colour change, no line
// re-wrapping, no dump fallback. Its `--pace`/`--step`/`--from` controls decide
// WHEN and FROM WHERE a frame goes out, never WHAT it contains, which is what
// keeps "byte for byte" true. The two answer different
// questions ("what was on the screen" vs "what bytes did the renderer emit"),
// and the second one is only answerable if the reader subtracts nothing.
//
// WHY A SEPARATE SURFACE RATHER THAN A FLAG ON THE RENDERER. `session replay
// --from stream` exists and still emits the PLAYER'S picture: the player is what
// the flag selects a SOURCE for, not a mode it can be talked out of. A caller
// that wants bytes it can hash, diff or hand to another process cannot recover
// them from a renderer, and the attempt to do so produced exactly the confusion
// this file exists to end.
//
// THREE STATES OF AN ENDING, and they are not one boolean (bug 01a1b823-5070):
// a stream whose close-out record is present is HEALTHY and this sub-verb says
// nothing; a stream that is whole but was never closed says THAT; a stream cut
// inside a frame says that, with the byte offset; and a stream whose own records
// contradict its own length says that. A scan that cannot run is a FIFTH
// sentence — never folded into "nothing is wrong" (Rule 15).

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/nickhuang99/gopi/internal/cliverbs"
	"github.com/nickhuang99/gopi/internal/replayplay"
	"github.com/nickhuang99/gopi/internal/sgr"
	"github.com/nickhuang99/gopi/internal/uiclient"
)

// screenReplayVerifyFlag is `--verify OTHER`: the measurement, not the picture.
// It compares THIS session's recording with another one frame by frame and
// prints where they first differ. It plays nothing.
const screenReplayVerifyFlag = "--verify"

// screenReplayUsageHead is the sub-verb's own page head — the fingerprint the
// contract suite pins, so a rewrite that drops it fails the live-binary smoke.
const screenReplayUsageHead = "gopi " + CliVerbScreen + " " + screenSubcmdReplay +
	" — write a recorded screen's render bytes back out, unchanged."

// screenReplaySynopsis is how the page spells a full invocation. It is composed
// from the dispatch-side constants, so a rename cannot leave the page teaching a
// command line the binary refuses (Rule 13).
const screenReplaySynopsis = "gopi " + CliVerbScreen + " " + screenSubcmdReplay + " <session-id>"

// screenReplayErrorPrefix heads every refusal and every diagnostic this family
// prints, so one grep finds all of them.
const screenReplayErrorPrefix = "gopi " + CliVerbScreen + " " + screenSubcmdReplay + ": "

// screenReplayNoticePrefix heads the ORDINARY diagnostics — the counts and the
// ending sentences. It is the SAME string as the refusal prefix, and that is
// deliberate rather than an oversight: what these lines share is the verb they
// belong to ("this came from gopi screen replay"), which is what a log reader
// greps for. The part that tells a refusal from a description is the exit
// status and the sentence itself, not the prefix — inventing a second spelling
// here would only give a reader two strings to search for the same family.
//
// (It is a named constant rather than a bare re-use so the two roles stay
// distinguishable at the call sites that print them: a future change that wants
// them apart edits ONE line here and nothing else.)
const screenReplayNoticePrefix = screenReplayErrorPrefix

// screenReplayCountsFormat is the ONE counts line: frames, payload bytes, the
// grid the run recorded, and the revision it named.
const screenReplayCountsFormat = "%s%s: frames=%d bytes=%d grid=%dx%d revision=%s\n"

// The THREE ending sentences, plus the scanner's own failure. They are separate
// constants because they are separate findings: a reader that flattened them
// into one line could not tell "healthy" from "unclosed" — the exact bug
// (01a1b823-5070) this family's predecessor was filed for.
const (
	screenReplayUnclosedWholeFormat = "%sthe recording is whole — every frame ends on a record boundary — but its recording run never wrote the close-out record, so nothing says the run finished. Nothing is truncated; the artifact is simply unclosed\n"
	screenReplayUnclosedCutFormat   = "%sthe stream ends inside a frame at byte %d — its recording run never reached close-out, so the bytes above are what reached that terminal and no more\n"
	screenReplayContradictionFormat = "%sthe recording contradicts itself (%d byte(s) on disk): %s. That is a fact about the FILE, not about the code that reads it\n"
	screenReplayEndUnreadableFormat = "%scannot tell how the recording at %s ends: %v\n"
	screenReplayWriteFormat         = "%swriting the %s bytes failed: %v\n"
	// The SELECTION line: a replay that started partway says so, so its short
	// counts are never read as a short recording.
	screenReplaySelectionFormat = "%sselection: started at frame %d, emitted %d of %d frame(s)\n"
	// The reset's own failure — a terminal left dirty because stdout closed is a
	// fact the caller acts on (it must still run `clear`), not a footnote.
	screenReplayResetFormat = "%scould not restore the terminal's attributes: %v — the colours above are still in force, run `clear`\n"
	// A start past the end is refused by name and with both numbers, because the
	// caller's next action depends on which side was wrong.
	screenReplayFromPastEndFormat = "%s--from %d is past the end: this recording has %d frame(s), indexed from 0\n"
	// A stdin that cannot be read in `--step` mode. Not folded into "continue":
	// replaying at full speed is a different command than the one asked for.
	screenReplayStepReadFormat = "%scannot read the stepping key: %v\n"
)

// screenReplaySgrReset is the SGR sequence that puts every attribute back to the
// terminal's default. A replay hands the terminal the RECORDED bytes, so the last
// frame's colours are still in force when the process exits and the caller has to
// run `clear` to get a readable prompt back. This is written on EVERY exit of the
// replay path — a normal end, a writer error, an unreadable stream — because the
// defect it repairs is "the terminal was left in a state nobody recovered from",
// and an early return is exactly the path where nobody notices until the next
// prompt is invisible.
// It is a VAR rather than a CONST because the spelling comes from internal/sgr,
// the project's one source of truth for the escape vocabulary (Rule 13/14): the
// literal "\x1b[0m" typed here is a second spelling of `sgr.ResetSeq()`, and the
// standards gate fails the build for exactly that.
var screenReplaySgrReset = sgr.ResetSeq()

// screenReplayTerminalRestore is the CLOSING TAIL: the last bytes this verb puts
// on stdout, on every exit including the error paths.
//
// It restores the TERMINAL STATE the recording may have changed, in the one
// order that leaves the shell usable, and each piece is here for a measured
// reason:
//
//  1. the DECSTBM scroll region reset FIRST — a recording that sets a scroll
//     region (`CSI 1;19r` appeared three times in the 369396-byte capture that
//     produced this fix) and never clears it freezes every row below the region
//     for the shell that follows, which is exactly what "I have to run `clear`"
//     looks like. It goes first so the sequences after it are not confined to a
//     region the terminal is about to forget;
//  2. the cursor made VISIBLE again (a recording may have hidden it);
//  3. the carriage return, so the shell's prompt starts at column 1 instead of
//     wherever the last frame parked the cursor (`CSI 20;6H` in that capture) —
//     a bare CR rather than a CUP keeps the LAST CSI a reset-class sequence;
//  4. the SGR reset LAST, so it is the final CSI on the wire — the old tail's
//     own promise, kept.
//
// Every sequence comes from internal/sgr (Rule 13/14): the literals are that
// package's, never a second spelling typed here.
var screenReplayTerminalRestore = sgr.ResetScrollRegionSeq() + sgr.CursorShowSeq() +
	screenReplayCarriageReturn + screenReplaySgrReset

// screenReplayCarriageReturn is CR: "return to column 1" without moving rows.
// It is the one piece of the tail that is not an escape sequence, which is why
// it can sit between the cursor restore and the SGR reset without becoming the
// last CSI — the cursor's column comes back while the last CSI stays a reset.
const screenReplayCarriageReturn = "\r"

// screenReplaySgrResetSpelled is how the PAGES name that sequence. A help page
// is read as text and printed to a terminal, so embedding the raw bytes would
// make the page itself repaint the reader's screen — the exact defect the reset
// exists to repair, committed by the documentation. The bytes and their spelling
// are two constants so neither can be edited alone (Rule 14).
const screenReplaySgrResetSpelled = "ESC[0m"

// screenReplayScrollResetSpelled and screenReplayCursorShowSpelled name the
// OTHER two pieces of the closing tail in the pages' own spelling, for the same
// reason: the page must not emit the bytes it documents. They stay separate
// constants beside the sequences they describe (Rule 14).
const (
	screenReplayScrollResetSpelled = "ESC[r"
	screenReplayCursorShowSpelled  = "ESC[?25h"
)

// screenReplayFromFlag selects the FIRST frame to emit: a replay that can only
// start at zero cannot be resumed, and a long recording is unviewable in one
// pass.
const screenReplayFromFlag = "--from"

// screenReplayPaceFlag is the per-frame delay. Without it a replay of a long
// session finishes in a fraction of a second — the bytes arrive faster than they
// can be read, which is the second half of the defect this verb repairs.
const screenReplayPaceFlag = "--pace"

// screenReplayStepFlag makes the replay advance ONE frame per keypress on stdin,
// so a viewer controls the pace instead of guessing it up front.
const screenReplayStepFlag = "--step"

// screenReplayDefaultPaceMS is the delay used when `--pace` is given with no
// argument, and the unit `--pace N` is read in. Chosen so a recorded screen is
// followable by eye without being slow: a frame is a whole redraw, not a byte.
const screenReplayDefaultPaceMS = 40

// screenReplayStepPrompt is written to stderr before each interactive wait. It
// goes to stderr with every other diagnostic, so stdout keeps carrying the
// recording's own bytes and nothing else.
const screenReplayStepPrompt = "press ENTER for the next frame, q then ENTER to stop"

// screenReplayStepQuit answers the step prompt with "stop here": a viewer that
// wants to leave must be able to, and the reset is still written.
const screenReplayStepQuit = "q"

// screenReplayOptions is one parsed command line.
type screenReplayOptions struct {
	id     string
	verify string
	help   bool
	// from is the zero-based frame index the replay STARTS at; 0 is the whole
	// recording, which is the historical behaviour and stays the default.
	from int
	// pace is the delay between frames; zero emits as fast as the writer
	// accepts, which is the historical behaviour and stays the default.
	pace time.Duration
	// step turns the replay into a one-frame-per-keypress viewer.
	step bool
}

// runScreenReplay is the sub-verb's entry point from the group dispatcher.
func runScreenReplay(args []string) int {
	return screenReplayRun(args, os.Stdout, os.Stderr)
}

// screenReplayRun is the verb with its streams as parameters, so a test can
// watch the bytes and the notices without a terminal (Rule 31: the surface is
// executed, not described).
func screenReplayRun(args []string, out io.Writer, errw io.Writer) int {
	return screenReplayRunWithStdin(args, os.Stdin, out, errw)
}

// screenReplayRunWithStdin is screenReplayRun with the STEPPING keys as a
// parameter too, so a test can drive `--step` without a terminal.
// screenReplayRunWithStdin is the verb's body with BOTH of its inputs as
// parameters: `--step` reads keys from `in`, so this is the form a test drives.
//
// WHO WRITES THE RESET, and who must not. screenReplayWrite owns it, because "the
// replay path" is exactly the set of exits that may have put colour on the
// caller's terminal — a normal end, a writer error, a stream that could not be
// opened or parsed, a start past the end. The exits ABOVE it (a usage error, an
// operand that names no recording) write NOTHING to stdout and keep writing
// nothing: a refusal must not emit control bytes at a caller whose invocation was
// wrong, and the contract that stdout carries only the recording is older than
// this reset. The reset is therefore not double-written — only this one path
// emits it (Rule 14).
func screenReplayRunWithStdin(args []string, in io.Reader, out io.Writer, errw io.Writer) int {
	opts, err := parseScreenReplayArgs(args)
	if err != nil {
		fmt.Fprintf(errw, "%s%v\n%s", screenReplayErrorPrefix, err, screenReplayUsage)
		return chatExitFailed
	}
	if opts.help {
		fmt.Fprint(out, screenReplayUsage)
		return chatExitOK
	}
	path, err := screenReplayStreamPath(opts.id)
	if err != nil {
		fmt.Fprintf(errw, "%s%v\n", screenReplayErrorPrefix, err)
		return chatExitFailed
	}
	if opts.verify != "" {
		return screenReplayVerify(opts.id, path, opts.verify, out, errw)
	}
	return screenReplayWrite(opts, path, in, out, errw)
}

// parseScreenReplayArgs reads the sub-verb's flags. A flag this page does not
// know is REFUSED by name (Rule 15): a silently ignored flag would make a caller
// believe it had changed the output.
func parseScreenReplayArgs(args []string) (screenReplayOptions, error) {
	var opts screenReplayOptions
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case a == HelpFlag || a == HelpFlagLong:
			opts.help = true
			return opts, nil
		case a == screenReplayVerifyFlag:
			if i+1 >= len(args) {
				return opts, fmt.Errorf("%s needs another recording (a session id or the path of a %s file)",
					screenReplayVerifyFlag, uiclient.ScreenStreamSuffix)
			}
			opts.verify = args[i+1]
			i++
		case strings.HasPrefix(a, screenReplayVerifyFlag+"="):
			opts.verify = strings.TrimSpace(strings.TrimPrefix(a, screenReplayVerifyFlag+"="))
			if opts.verify == "" {
				return opts, fmt.Errorf("%s= names no recording", screenReplayVerifyFlag)
			}
		case a == screenReplayStepFlag:
			opts.step = true
		case a == screenReplayFromFlag:
			if i+1 >= len(args) {
				return opts, fmt.Errorf("%s needs a frame index (0 is the first frame)", screenReplayFromFlag)
			}
			n, nerr := screenReplayIndex(screenReplayFromFlag, args[i+1])
			if nerr != nil {
				return opts, nerr
			}
			opts.from = n
			i++
		case strings.HasPrefix(a, screenReplayFromFlag+"="):
			n, nerr := screenReplayIndex(screenReplayFromFlag,
				strings.TrimPrefix(a, screenReplayFromFlag+"="))
			if nerr != nil {
				return opts, nerr
			}
			opts.from = n
		case a == screenReplayPaceFlag:
			// A BARE `--pace` takes the default, because "slow this down" is the
			// common request and demanding a number for it buys nothing.
			if i+1 < len(args) && !strings.HasPrefix(args[i+1], "-") {
				d, derr := screenReplayPace(screenReplayPaceFlag, args[i+1])
				if derr != nil {
					return opts, derr
				}
				opts.pace = d
				i++
			} else {
				opts.pace = screenReplayDefaultPaceMS * time.Millisecond
			}
		case strings.HasPrefix(a, screenReplayPaceFlag+"="):
			d, derr := screenReplayPace(screenReplayPaceFlag,
				strings.TrimPrefix(a, screenReplayPaceFlag+"="))
			if derr != nil {
				return opts, derr
			}
			opts.pace = d
		case strings.HasPrefix(a, "-"):
			return opts, fmt.Errorf("unknown flag %q (this page takes %s, %s, %s, %s and %s)",
				a, HelpFlag, screenReplayVerifyFlag, screenReplayFromFlag,
				screenReplayPaceFlag, screenReplayStepFlag)
		default:
			if opts.id != "" {
				return opts, fmt.Errorf("one session id, got %q and %q", opts.id, a)
			}
			opts.id = a
		}
	}
	if opts.id == "" {
		return opts, fmt.Errorf("no session id was given (usage: %s)", screenReplaySynopsis)
	}
	return opts, nil
}

// screenReplayIndex reads a NON-NEGATIVE frame index. A negative one is refused
// rather than clamped: `--from -1` reads to a caller as "the last frame", and a
// silent clamp would hand back the whole recording while the invocation said
// otherwise (Rule 15).
func screenReplayIndex(flag, raw string) (int, error) {
	v := strings.TrimSpace(raw)
	n, err := strconv.Atoi(v)
	if err != nil {
		return 0, fmt.Errorf("%s wants a frame index, got %q", flag, raw)
	}
	if n < 0 {
		return 0, fmt.Errorf("%s wants a frame index of 0 or more, got %d", flag, n)
	}
	return n, nil
}

// screenReplayPace reads a per-frame delay in MILLISECONDS and answers it as a
// duration. Milliseconds rather than a Go duration string because the operand is
// typed at a shell by someone watching a screen, and `--pace 80` is a thing
// nobody has to look up.
func screenReplayPace(flag, raw string) (time.Duration, error) {
	v := strings.TrimSpace(raw)
	ms, err := strconv.Atoi(v)
	if err != nil {
		return 0, fmt.Errorf("%s wants a delay in milliseconds, got %q", flag, raw)
	}
	if ms < 0 {
		return 0, fmt.Errorf("%s wants a delay of 0 or more milliseconds, got %d", flag, ms)
	}
	return time.Duration(ms) * time.Millisecond, nil
}

// screenReplayStreamPath resolves the operand to a screen-stream FILE. A path
// that exists is taken as itself (so a copy, or a file produced on another host,
// can be read); anything else is a session id resolved through the SAME
// directory resolver `gopi session log` uses, so the two readers cannot disagree
// about which directory an id names.
//
// A SESSION WITH NO STREAM IS A REFUSAL, never a dump: it names the reader that
// can still show something (the renderer), because a caller who asked for bytes
// and silently got a picture would draw a conclusion from the wrong artifact.
func screenReplayStreamPath(operand string) (string, error) {
	operand = strings.TrimSpace(operand)
	if operand == "" {
		return "", fmt.Errorf("no session id was given")
	}
	if st, err := os.Stat(operand); err == nil && !st.IsDir() {
		return operand, nil
	}
	dir, err := sessionReplayDir(operand)
	if err != nil {
		return "", err
	}
	path, ok := replayplay.ScreenStreamPath(dir)
	if !ok {
		return "", fmt.Errorf("session %s recorded no screen stream: %s holds no %s file, so there are no bytes to write (a worker run is the ordinary case). "+
			"For the picture a session without a stream can still show, use `%s` — this reader will not substitute the dump for the bytes",
			operand, dir, uiclient.ScreenStreamSuffix, sessionReplaySynopsis)
	}
	return path, nil
}

// screenReplayWrite is the verb proper: read the artifact, write every frame's
// payload in recorded order, and say how the recording ENDED.
//
// THE BYTES ARE THE OUTPUT and nothing else goes to `out`: every diagnostic
// belongs on `errw`, because a caller piping this into a file or another program
// must receive the recording and only the recording. The ending is reported even
// so, on the diagnostic stream.
func screenReplayWrite(opts screenReplayOptions, path string, in io.Reader, out io.Writer, errw io.Writer) int {
	code := screenReplayWriteFrames(opts, path, in, out, errw)
	// THE TRAILING SGR RESET IS WRITTEN HERE, on EVERY exit — the ordinary end,
	// a writer error, an unreadable stream, a bad frame index. It is the LAST
	// thing this verb puts on stdout by construction: screenReplayWriteFrames
	// never emits it, so nothing can follow it. A failed reset is reported
	// rather than swallowed (Rule 15) — the caller has to know the terminal it
	// is about to type into still carries the recording's colours.
	if rerr := screenReplayEmitReset(out); rerr != nil {
		fmt.Fprintf(errw, screenReplayResetFormat, screenReplayErrorPrefix, rerr)
		return chatExitFailed
	}
	return code
}

// screenReplayEmitReset restores the terminal's state: the scroll region, the
// cursor and the SGR attributes, in screenReplayTerminalRestore's one order.
// The name is kept because every caller and every page names this step after
// the SGR reset it used to do alone; the comment states the wider job.
func screenReplayEmitReset(out io.Writer) error {
	_, err := io.WriteString(out, screenReplayTerminalRestore)
	return err
}

// screenReplayWriteFrames reads the stream and emits the SELECTED frames. It
// never writes the reset — that belongs to its caller, so that it happens once
// on every path including this function's own early returns.
func screenReplayWriteFrames(opts screenReplayOptions, path string, in io.Reader, out io.Writer, errw io.Writer) int {
	f, oerr := os.Open(path)
	if oerr != nil {
		fmt.Fprintf(errw, "%scannot open %s: %v\n", screenReplayErrorPrefix, path, oerr)
		return chatExitFailed
	}
	meta, frames, perr := uiclient.ReadSgrFrames(f)
	// The close is checked and reported through the same sentence as an
	// unreadable open: a stream that could not be closed may have been read
	// through a failure, and bytes nobody vouched for must not be presented as
	// the recording.
	if cerr := f.Close(); cerr != nil {
		fmt.Fprintf(errw, "%scannot close %s: %v\n", screenReplayErrorPrefix, path, cerr)
		return chatExitFailed
	}
	if perr != nil {
		fmt.Fprintf(errw, "%s%s is not a readable screen stream: %v\n", screenReplayErrorPrefix, path, perr)
		return chatExitFailed
	}
	// A start past the end is REFUSED, not clamped to an empty replay: the
	// invocation asked for a frame this recording does not have, and an empty
	// exit 0 would read as "there was nothing to show" (Rule 15).
	if opts.from > len(frames) {
		fmt.Fprintf(errw, screenReplayFromPastEndFormat, screenReplayErrorPrefix,
			opts.from, len(frames))
		return chatExitFailed
	}
	steps := bufio.NewReader(in)
	written, emitted, code := screenReplayEmitFrames(opts, frames, steps, out, errw)
	if code != chatExitOK {
		return code
	}
	// THE RECORDED COUNTS ARE STATED ON STDERR, always — they are what makes
	// "byte for byte" checkable by the caller (it can compare them against its
	// own count) and they never touch stdout. The trailer's own claim is what
	// meta carries when the run closed out; otherwise these are the reader's
	// counted values, which meta also distinguishes. A SELECTIVE replay says
	// how many frames it left out, so a short count is never mistaken for a
	// short recording.
	fmt.Fprintf(errw, screenReplayCountsFormat,
		screenReplayNoticePrefix, opts.id, emitted, written, meta.Grid[0], meta.Grid[1],
		orUndeclared(meta.Revision))
	if emitted != len(frames) {
		fmt.Fprintf(errw, screenReplaySelectionFormat, screenReplayNoticePrefix,
			opts.from, emitted, len(frames))
	}
	screenReplaySayEnding(path, errw)
	return chatExitOK
}

// screenReplayEmitFrames writes frames[opts.from:] one at a time, honouring the
// pacing and stepping controls, and answers how many bytes went out and how many
// frames carried them. A stop asked for by the viewer is NOT a failure: it is
// the control working, so it returns chatExitOK.
func screenReplayEmitFrames(opts screenReplayOptions, frames [][]byte, steps *bufio.Reader,
	out io.Writer, errw io.Writer) (int64, int, int) {
	var written int64
	emitted := 0
	for _, frame := range frames[opts.from:] {
		// The wait happens BEFORE the frame it introduces, and never before the
		// first: a replay that paused before showing anything would look hung.
		if emitted > 0 {
			if opts.step {
				stop, serr := screenReplayAwaitStep(steps, errw)
				if serr != nil {
					fmt.Fprintf(errw, screenReplayStepReadFormat, screenReplayErrorPrefix, serr)
					return written, emitted, chatExitFailed
				}
				if stop {
					return written, emitted, chatExitOK
				}
			}
			if opts.pace > 0 {
				time.Sleep(opts.pace)
			}
		}
		n, werr := out.Write(frame)
		written += int64(n)
		emitted++
		if werr != nil {
			fmt.Fprintf(errw, screenReplayWriteFormat, screenReplayErrorPrefix,
				strings.TrimPrefix(uiclient.ScreenStreamSuffix, "."), werr)
			return written, emitted, chatExitFailed
		}
	}
	return written, emitted, chatExitOK
}

// screenReplayAwaitStep asks for one keypress and answers whether the viewer
// asked to stop. Both answers come from the SAME read so the prompt and the
// decision cannot disagree, and a read error is returned rather than treated as
// "continue": a stdin that cannot be read makes the stepping mode unusable, and
// silently replaying at full speed instead would be a different command than the
// one the caller asked for.
func screenReplayAwaitStep(steps *bufio.Reader, errw io.Writer) (bool, error) {
	fmt.Fprintf(errw, "%s%s\n", screenReplayNoticePrefix, screenReplayStepPrompt)
	line, err := steps.ReadString('\n')
	if err != nil && line == "" {
		return false, err
	}
	return strings.TrimSpace(line) == screenReplayStepQuit, nil
}

// orUndeclared names an empty revision as the absence it is, rather than
// printing nothing (a blank field reads as "no revision exists", a different
// claim from "this build was not told one").
func orUndeclared(s string) string {
	if strings.TrimSpace(s) == "" {
		return "undeclared"
	}
	return s
}

// screenReplaySayEnding states how the recording ended, in ONE of four
// sentences (healthy says nothing at all — a capture of a good recording must
// see an empty diagnostic stream).
func screenReplaySayEnding(path string, errw io.Writer) {
	end, size, err := uiclient.ScanSgrStreamEndPath(path)
	if err != nil {
		fmt.Fprintf(errw, screenReplayEndUnreadableFormat, screenReplayErrorPrefix, path, err)
		return
	}
	if end.Contradicts() {
		parts := make([]string, 0, len(end.Findings))
		for _, f := range end.Findings {
			parts = append(parts, f.String())
		}
		fmt.Fprintf(errw, screenReplayContradictionFormat, screenReplayErrorPrefix, size, strings.Join(parts, "; "))
		return
	}
	if end.Closed() {
		return
	}
	if end.BoundaryWhole() {
		fmt.Fprintf(errw, screenReplayUnclosedWholeFormat, screenReplayErrorPrefix)
		return
	}
	fmt.Fprintf(errw, screenReplayUnclosedCutFormat, screenReplayErrorPrefix, end.CutFrameAt)
}

// screenReplayVerify compares two recordings FRAME BY FRAME and prints the first
// divergence — the frame number, the byte offset on each side and both payloads
// — or `first divergence: none` when the frame sets are identical.
//
// IT COMPARES FRAMES, NOT FILES: the header names the build, the geometry and
// the start stamp, so two recordings of the same picture made by two binaries at
// two moments are never byte-identical as FILES and are frequently identical as
// PICTURES. That distinction is the measurement this command exists for, and it
// is why the same comparison backs `session replay --verify` — one implementation
// (uiclient.CompareSgrStreams), so the two surfaces cannot disagree.
func screenReplayVerify(id, selfPath, other string, out io.Writer, errw io.Writer) int {
	otherPath, err := screenReplayStreamPath(other)
	if err != nil {
		fmt.Fprintf(errw, "%s%s\n", screenReplayErrorPrefix, err)
		return chatExitFailed
	}
	cmp, err := uiclient.CompareSgrStreams(selfPath, otherPath)
	if err != nil {
		fmt.Fprintf(errw, "%s%v\n", screenReplayErrorPrefix, err)
		return chatExitFailed
	}
	fmt.Fprintf(out, "%s %s\n", screenReplaySynopsis, screenReplayVerifyFlag)
	fmt.Fprintf(out, "  A=%s\n  B=%s\n", selfPath, otherPath)
	fmt.Fprintf(out, "  A: frames=%d bytes=%d\n", cmp.FramesA, cmp.BytesA)
	fmt.Fprintf(out, "  B: frames=%d bytes=%d\n", cmp.FramesB, cmp.BytesB)
	if cmp.Identical() {
		fmt.Fprintf(out, "first divergence: none\n")
		return chatExitOK
	}
	d := cmp.First
	fmt.Fprintf(out, "first divergence: frame %d — %s\n", d.Frame, d.Reason)
	if d.OffsetA >= 0 {
		fmt.Fprintf(out, "  A: frame %d at byte offset %d — %s\n", d.Frame, d.OffsetA, screenReplayVerifyPayload(d.A))
	}
	if d.OffsetB >= 0 {
		fmt.Fprintf(out, "  B: frame %d at byte offset %d — %s\n", d.Frame, d.OffsetB, screenReplayVerifyPayload(d.B))
	}
	// A DIVERGENCE IS A FINDING, NOT A FAILURE: exit 0, because the command did
	// what it was asked (the same posture `diff` takes with its own exit codes
	// documented; here the caller reads the line). The exit code stays for
	// "could not compare".
	return chatExitOK
}

// screenReplayVerifyPayloadMax bounds the payload spelling in a --verify line,
// so one 200 KB frame cannot flood a terminal.
const screenReplayVerifyPayloadMax = sessionReplayVerifyPayloadMax

// screenReplayVerifyPayload renders one frame's bytes for a divergence line.
func screenReplayVerifyPayload(p []byte) string {
	return sessionReplayVerifyPayload(p)
}

// screenReplayUsage is the sub-verb's own page (`gopi screen replay -h`).
var screenReplayUsage = screenReplayUsageHead + "\n" +
	"\n" +
	"Usage:\n" +
	"  " + screenReplaySynopsis + "                 Write the recorded render bytes to stdout\n" +
	"  " + screenReplaySynopsis + " " + screenReplayFromFlag + " N " + screenReplayPaceFlag + " MS  Write them FROM frame N, MS apart\n" +
	"  " + screenReplaySynopsis + " " + screenReplayStepFlag + "            Write ONE frame per keypress\n" +
	"  " + screenReplaySynopsis + " " + screenReplayVerifyFlag + " OTHER  Compare two recordings frame by frame\n" +
	"  " + screenReplaySynopsis + " -h              This page\n" +
	"\n" +
	"WHAT GOES TO STDOUT, AND WHAT DOES NOT. Every frame's payload, in recorded\n" +
	"order, and nothing else: no canvas is built, no colour is rewritten and no\n" +
	"line is re-wrapped, so the output is the recording's own bytes and can be\n" +
	"piped, hashed or captured. Every diagnostic — the counts and how the\n" +
	"recording ended — goes to STDERR, so a redirected stdout never carries a\n" +
	"word that was not in the recording. The ONE exception is deliberate and it\n" +
	"is the last thing written on every exit, error paths included: the terminal\n" +
	"is handed back CLEAN. Four sequences, in one order — the DECSTBM scroll\n" +
	"region reset `" + screenReplayScrollResetSpelled + "` first (a recording that sets a scroll region and never\n" +
	"clears it freezes every row below it, which is what \"I have to run clear\"\n" +
	"actually is), then the cursor made visible again `" + screenReplayCursorShowSpelled + "`, then a carriage\n" +
	"return so the prompt starts at column 1, and the SGR reset `" + screenReplaySgrResetSpelled + "` LAST, so\n" +
	"the final sequence on the wire is a reset and the shell inherits a terminal\n" +
	"in its default attributes rather than the last frame's.\n" +
	"\n" +
	"Options:\n" +
	"  " + screenReplayFromFlag + " N         start at frame N (0-based). Omitted = frame 0, the whole\n" +
	"                    recording, which is the historical behaviour. A start\n" +
	"                    past the last frame is refused by name, never clamped\n" +
	"                    to an empty replay.\n" +
	"  " + screenReplayPaceFlag + " MS        wait MS milliseconds between frames. A bare " + screenReplayPaceFlag + "\n" +
	"                    takes " + strconv.Itoa(screenReplayDefaultPaceMS) + "ms. Omitted = emit as fast as the writer accepts,\n" +
	"                    which is the historical behaviour and is unusable for\n" +
	"                    watching a long session.\n" +
	"  " + screenReplayStepFlag + "           advance ONE frame per line read from stdin: press ENTER\n" +
	"                    for the next frame, type `" + screenReplayStepQuit + "` then ENTER to stop early. The\n" +
	"                    prompt goes to stderr, so stdout stays the recording.\n" +
	"                    Combine with " + screenReplayPaceFlag + " if you want both.\n" +
	"  " + screenReplayVerifyFlag + " OTHER     does not write the bytes: compares THIS session's recording with\n" +
	"                    OTHER (a session id, or the path of a screen-stream file),\n" +
	"                    frame by frame, and prints where they first differ — the\n" +
	"                    frame number, the byte offset on each side and both\n" +
	"                    payloads. Prints `first divergence: none` when the frame\n" +
	"                    sets are identical.\n" +
	"                    Frames, not files: the header names the build, the grid\n" +
	"                    and the start stamp, so two recordings of the same\n" +
	"                    picture are never byte-identical as FILES and are often\n" +
	"                    identical as PICTURES\n" +
	"\n" +
	"The operand is a session id (resolved through the same directory as\n" +
	"`gopi session log`) or the PATH of a stream file. A session that recorded no\n" +
	"screen stream is REFUSED by name, naming `" + sessionReplaySynopsis + "` — the\n" +
	"renderer, which can still show the reduced dump. This sub-verb never\n" +
	"substitutes one artifact for the other.\n" +
	"\n" +
	"What it reads: <session-id>" + uiclient.ScreenStreamSuffix + ", or the pre-rename\n" +
	uiclient.LegacyScreenStreamSuffix + " (accepted names: " + screenStreamSuffixSpellings() + ").\n" +
	"\n" +
	"Record one with " + cliverbs.ScreenStreamEnv + "=PATH (or =1).\n"
