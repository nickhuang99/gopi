package main

// cli_screen_replay.go — `gopi screen replay <session-id>`: write a recorded
// screen's render bytes back out, BYTE FOR BYTE.
//
// WHAT IT IS, AND WHAT IT DELIBERATELY IS NOT. `gopi session replay` is a
// RENDERER: it builds a canvas at a chosen geometry, paces the frames at a human
// rate, and falls back to the reduced dump when a session recorded no stream.
// This sub-verb is a WRITER: it reads the artifact, hands every frame's payload
// to stdout in recorded order, and adds nothing — no canvas, no pacing, no
// colour change, no line re-wrapping, no dump fallback. The two answer different
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
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/nick/gopi/internal/cliverbs"
	"github.com/nick/gopi/internal/replayplay"
	"github.com/nick/gopi/internal/uiclient"
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
)

// screenReplayOptions is one parsed command line.
type screenReplayOptions struct {
	id     string
	verify string
	help   bool
}

// runScreenReplay is the sub-verb's entry point from the group dispatcher.
func runScreenReplay(args []string) int {
	return screenReplayRun(args, os.Stdout, os.Stderr)
}

// screenReplayRun is the verb with its streams as parameters, so a test can
// watch the bytes and the notices without a terminal (Rule 31: the surface is
// executed, not described).
func screenReplayRun(args []string, out io.Writer, errw io.Writer) int {
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
	return screenReplayWrite(opts.id, path, out, errw)
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
		case strings.HasPrefix(a, "-"):
			return opts, fmt.Errorf("unknown flag %q (this page takes %s and %s)",
				a, HelpFlag, screenReplayVerifyFlag)
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
func screenReplayWrite(id, path string, out io.Writer, errw io.Writer) int {
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
	var written int64
	for _, frame := range frames {
		n, werr := out.Write(frame)
		written += int64(n)
		if werr != nil {
			fmt.Fprintf(errw, screenReplayWriteFormat, screenReplayErrorPrefix,
				strings.TrimPrefix(uiclient.ScreenStreamSuffix, "."), werr)
			return chatExitFailed
		}
	}
	// THE RECORDED COUNTS ARE STATED ON STDERR, always — they are what makes
	// "byte for byte" checkable by the caller (it can compare them against its
	// own count) and they never touch stdout. The trailer's own claim is what
	// meta carries when the run closed out; otherwise these are the reader's
	// counted values, which meta also distinguishes.
	fmt.Fprintf(errw, screenReplayCountsFormat,
		screenReplayNoticePrefix, id, len(frames), written, meta.Grid[0], meta.Grid[1],
		orUndeclared(meta.Revision))
	screenReplaySayEnding(path, errw)
	return chatExitOK
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
	"  " + screenReplaySynopsis + " " + screenReplayVerifyFlag + " OTHER  Compare two recordings frame by frame\n" +
	"  " + screenReplaySynopsis + " -h              This page\n" +
	"\n" +
	"WHAT GOES TO STDOUT, AND WHAT DOES NOT. Every frame's payload, in recorded\n" +
	"order, and nothing else: no canvas is built, no frame is paced, no colour is\n" +
	"rewritten and no line is re-wrapped, so the output is the recording's own\n" +
	"bytes and can be piped, hashed or captured. Every diagnostic — the counts and\n" +
	"how the recording ended — goes to STDERR, so a redirected stdout never carries\n" +
	"a word that was not in the recording.\n" +
	"\n" +
	"Options:\n" +
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
