package main

// cli_screen_replay_verb.go — `gopi screen-replay <SESSION-ID|FILE>`: the
// TOP-LEVEL face of the recorded screen's byte reader.
//
// WHY A SECOND FACE ON THE SAME VERB, AND NOT A SECOND IMPLEMENTATION. The
// sub-verb `gopi screen replay` (cli_screen_replay.go) already answers the
// question this page answers: what bytes did the renderer emit, and how did the
// recording end. It answers it one level down, under a group word, so an
// operator who wants the bytes of the session they are standing in has to know
// that `screen` is a family, that its first leaf is `replay`, and that the leaf
// is not the renderer of the same name under `session`. This file adds the
// ADDRESS, not the behaviour: it resolves the operand through the SAME
// resolver, writes the frames through the SAME writer, states the ending with
// the SAME three sentences, and compares two recordings through the SAME
// reader (uiclient.CompareSgrStreams) the sub-verb uses. Rule 14: one value,
// one implementation, two doors.
//
// WHAT DIFFERS FROM THE SUB-VERB, and why the difference is deliberate rather
// than drift:
//
//  1. `--verify` takes TWO operands here (`gopi screen-replay --verify A B`)
//     instead of comparing an implicit "this" with one OTHER. A top-level verb
//     is not standing on a session, so it has no "this" to be implicit about:
//     both sides of the comparison are named by the caller.
//  2. A DIVERGENCE EXITS NON-ZERO. The sub-verb reports a divergence as a
//     FINDING and exits 0 (the sentence carries it); a top-level reader that a
//     script pipes into a branch needs the status, because "the two recordings
//     differ" and "the comparison could not run" must not share an exit code.
//  3. The identical case prints ONE counts line — `identical: N frames, M
//     bytes` — so the answer is greppable without parsing a heading.
//
// THE ENDING SENTENCES ARE NOT DUPLICATED. A healthy recording says nothing,
// and the three non-healthy states print the three sentences
// cli_screen_replay.go already owns (screenReplaySayEnding), including its
// fourth: a scan that cannot run. A copy of those sentences here would be a
// second vocabulary for one file's condition, which is exactly the shape Rule
// 15's "two different failures, one sentence" is the other end of.

import (
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/nickhuang99/gopi/internal/cliverbs"
	"github.com/nickhuang99/gopi/internal/uiclient"
)

// screenReplayVerbVerifyFlag is `--verify A B`: the measurement. It is the SAME
// flag word the sub-verb takes (taken from its constant, so the two pages cannot
// disagree about what the flag is called), used with a different operand count.
const screenReplayVerbVerifyFlag = screenReplayVerifyFlag

// screenReplayVerbOperands is how many recordings `--verify` compares. Named
// because the refusal quotes it and a bare 2 there would be a magic number
// (Rule 1).
const screenReplayVerbOperands = 2

// screenReplayVerbExitDiverged is the status a MEASURED divergence returns. It
// is the ordinary failure status on purpose: the caller's question was "are
// these two recordings the same picture", "no" is a negative answer, and a
// non-zero status is how a shell reads one. The distinction that matters is
// kept separate — "could not compare" keeps the same non-zero status but says
// so in words, and never reports a divergence it did not measure.
const screenReplayVerbExitDiverged = chatExitFailed

// screenReplayVerbUsageHead is this verb's own page head — the fingerprint the
// contract suite pins, so a rewrite that drops it fails the live-binary smoke.
const screenReplayVerbUsageHead = "gopi " + CliVerbScreenReplay +
	" — write a recorded screen's render bytes back out, unchanged."

// screenReplayVerbSynopsis is how the page spells a full invocation, composed
// from the dispatch-side constant so a rename cannot leave the page teaching a
// command line the binary refuses (Rule 13).
const screenReplayVerbSynopsis = "gopi " + CliVerbScreenReplay + " <SESSION-ID|FILE>"

// screenReplayVerbErrorPrefix heads every refusal and every diagnostic this
// verb prints, so one grep finds all of them.
const screenReplayVerbErrorPrefix = "gopi " + CliVerbScreenReplay + ": "

// screenReplayVerbIdenticalFormat is the ONE line an identical comparison
// prints. Both counts come from the comparison's own MEASUREMENT (counted
// frames and counted payload bytes), never from a trailer's declaration.
const screenReplayVerbIdenticalFormat = "identical: %d frames, %d bytes\n"

// screenReplayVerbIdenticalLine is the same line with its counts kept as the
// format's own placeholders — the spelling the -h page quotes, so the page and
// the output name one sentence (Rule 13: derived, never re-typed).
const screenReplayVerbIdenticalLine = "identical: %d frames, %d bytes"

// screenReplayVerbFramesFormat / screenReplayVerbDivergenceFormat are the
// divergence answer: WHICH frame first differs, WHERE it differs in each file,
// and WHAT the bytes were. The frame number and the offset are the reading;
// the payloads are the context that makes the reading checkable by hand.
const (
	screenReplayVerbSideFormat        = "  %s: %s — frames=%d bytes=%d\n"
	screenReplayVerbDivergenceFormat  = "differ: frame %d — %s\n"
	screenReplayVerbOffsetFormat      = "  %s: frame %d at byte offset %d — %s\n"
	screenReplayVerbMissingSideFormat = "  %s: no frame at position %d — %s\n"
)

// screenReplayVerbOptions is one parsed command line.
type screenReplayVerbOptions struct {
	// operands are the recordings named by the caller, in argv order: ONE for
	// the write, TWO for `--verify`.
	operands []string
	verify   bool
	help     bool
}

// isScreenReplaySubcommand reports whether argv begins with this verb. It
// consumes the full argv and handles its own exit, exactly like every other
// subcommand predicate in this package.
func isScreenReplaySubcommand(args []string) bool {
	// The comparison names the constant DIRECTLY rather than through a helper,
	// for one reason worth stating: the census generator
	// (scripts/g7-verb-census.sh) reads the verb constant out of this predicate's
	// own body, and a body that delegates cannot be read by the shape master
	// ships. The comparison is on the WHOLE word, so `gopi screen` is never
	// mistaken for `gopi screen-replay`.
	if len(args) == 0 || args[0] != CliVerbScreenReplay {
		return false
	}
	os.Exit(runScreenReplayVerb(args[1:], os.Stdout, os.Stderr))
	return true
}

// runScreenReplayVerb is the verb with its streams as parameters, so a test can
// watch the bytes and the notices without a terminal (Rule 31: the surface is
// executed, not described).
func runScreenReplayVerb(args []string, out io.Writer, errw io.Writer) int {
	opts, err := parseScreenReplayVerbArgs(args)
	if err != nil {
		fmt.Fprintf(errw, "%s%v\n%s", screenReplayVerbErrorPrefix, err, screenReplayVerbUsage)
		return chatExitFailed
	}
	if opts.help {
		fmt.Fprint(out, screenReplayVerbUsage)
		return chatExitOK
	}
	if opts.verify {
		return screenReplayVerbDiff(opts.operands[0], opts.operands[1], out, errw)
	}
	return screenReplayVerbWrite(opts.operands[0], out, errw)
}

// parseScreenReplayVerbArgs reads this verb's flags. An unknown flag is refused
// BY NAME (Rule 15): a silently ignored flag would make a caller believe it had
// changed the output.
func parseScreenReplayVerbArgs(args []string) (screenReplayVerbOptions, error) {
	var opts screenReplayVerbOptions
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case a == HelpFlag || a == HelpFlagLong:
			opts.help = true
			return opts, nil
		case a == screenReplayVerbVerifyFlag:
			// The flag TAKES BOTH OPERANDS, and it takes them by position:
			// `--verify A B` reads two recordings, so the flag consumes
			// nothing itself and the operands are counted below. That is why
			// there is no `i++` here — `--verify` is a MODE, not a valued flag.
			opts.verify = true
		case strings.HasPrefix(a, screenReplayVerbVerifyFlag+"="):
			return opts, fmt.Errorf("%s takes no value — it takes two recordings (%s A B)",
				screenReplayVerbVerifyFlag, screenReplayVerbSynopsis)
		case strings.HasPrefix(a, "-"):
			return opts, fmt.Errorf("unknown flag %q (this page takes %s, %s and %s)",
				a, HelpFlag, HelpFlagLong, screenReplayVerbVerifyFlag)
		default:
			opts.operands = append(opts.operands, a)
		}
	}
	want := 1
	if opts.verify {
		want = screenReplayVerbOperands
	}
	if len(opts.operands) == 0 {
		return opts, fmt.Errorf("no recording was given (usage: %s)", screenReplayVerbSynopsis)
	}
	if len(opts.operands) != want {
		return opts, fmt.Errorf("%d recording(s) were given, want %d: %s",
			len(opts.operands), want, screenReplayVerbOperandShape(opts.verify))
	}
	return opts, nil
}

// screenReplayVerbOperandShape states what the caller's operand count should
// have been, in the words of the mode they asked for.
func screenReplayVerbOperandShape(verify bool) string {
	if verify {
		return screenReplayVerbSynopsis + " " + screenReplayVerbVerifyFlag + " A B (two recordings to compare)"
	}
	return screenReplayVerbSynopsis + " (one recording to write)"
}

// screenReplayVerbWrite is the write arm: resolve the operand, then hand the
// work to the ONE writer the sub-verb uses, so the bytes, the counts line and
// the three ending sentences cannot drift between the two doors.
//
// THE OPERAND IS RESOLVED BY THE SUB-VERB'S OWN RESOLVER. `screenReplayStreamPath`
// accepts a session id (through the directory resolver `gopi session log` uses),
// a new-suffix file, a legacy-suffix file and any existing path — which is
// exactly this verb's contract, and the reason there is no second resolver to
// keep in step (Rule 14).
func screenReplayVerbWrite(operand string, out io.Writer, errw io.Writer) int {
	path, err := screenReplayStreamPath(operand)
	if err != nil {
		fmt.Fprintf(errw, "%s%v\n", screenReplayVerbErrorPrefix, err)
		return chatExitFailed
	}
	return screenReplayWrite(operand, path, out, errw)
}

// screenReplayVerbDiff is the `--verify A B` arm: two named recordings, compared
// FRAME BY FRAME through uiclient.CompareSgrStreams (the same comparison the
// sub-verb reports through), printing the first divergence with its frame
// number, its byte offset on each side and the two payloads as context — or the
// one counts line when the frame sets agree.
//
// FRAMES, NOT FILES. The header names the build, the geometry and the start
// stamp, so two recordings of the same picture made by two binaries at two
// moments are never byte-identical as FILES and are often identical as
// PICTURES. "Byte offset" here therefore means the payload offset the frame
// begins at INSIDE its own file — the number a reader can seek to and compare.
func screenReplayVerbDiff(operandA, operandB string, out io.Writer, errw io.Writer) int {
	pathA, err := screenReplayStreamPath(operandA)
	if err != nil {
		fmt.Fprintf(errw, "%sA: %v\n", screenReplayVerbErrorPrefix, err)
		return chatExitFailed
	}
	pathB, err := screenReplayStreamPath(operandB)
	if err != nil {
		fmt.Fprintf(errw, "%sB: %v\n", screenReplayVerbErrorPrefix, err)
		return chatExitFailed
	}
	cmp, err := uiclient.CompareSgrStreams(pathA, pathB)
	if err != nil {
		fmt.Fprintf(errw, "%s%v\n", screenReplayVerbErrorPrefix, err)
		return chatExitFailed
	}
	fmt.Fprintf(out, "%s %s\n", screenReplayVerbSynopsis, screenReplayVerbVerifyFlag)
	fmt.Fprintf(out, screenReplayVerbSideFormat, "A", pathA, cmp.FramesA, cmp.BytesA)
	fmt.Fprintf(out, screenReplayVerbSideFormat, "B", pathB, cmp.FramesB, cmp.BytesB)
	if cmp.Identical() {
		// The identical answer states the counts ONCE, in the shape a script
		// can grep. It uses the MEASURED totals (a trailer's declaration is
		// compared by the scan, never believed here).
		fmt.Fprintf(out, screenReplayVerbIdenticalFormat, cmp.FramesA, cmp.BytesA)
		return chatExitOK
	}
	d := cmp.First
	fmt.Fprintf(out, screenReplayVerbDivergenceFormat, d.Frame, d.Reason)
	screenReplayVerbSaySide(out, "A", d.Frame, d.OffsetA, d.A, d.Reason)
	screenReplayVerbSaySide(out, "B", d.Frame, d.OffsetB, d.B, d.Reason)
	return screenReplayVerbExitDiverged
}

// screenReplayVerbSaySide prints one side of a divergence: the frame number,
// the byte offset inside THAT file, and the payload as context. A side with no
// frame at that position says so by name — printing an empty byte range instead
// would read as "the frame is empty", a different claim from "this file ends
// before the other" (the distinction uiclient.SgrFrameDivergence documents).
func screenReplayVerbSaySide(out io.Writer, label string, frame int, offset int64, payload []byte, reason string) {
	if offset < 0 {
		fmt.Fprintf(out, screenReplayVerbMissingSideFormat, label, frame, reason)
		return
	}
	fmt.Fprintf(out, screenReplayVerbOffsetFormat, label, frame, offset, screenReplayVerifyPayload(payload))
}

// screenReplayVerbUsage is this verb's own page.
var screenReplayVerbUsage = screenReplayVerbUsageHead + "\n" +
	"\n" +
	"Usage:\n" +
	"  " + screenReplayVerbSynopsis + "\n" +
	"        Write the recorded render bytes to stdout, unchanged.\n" +
	"  " + screenReplayVerbSynopsis + " " + screenReplayVerbVerifyFlag + " A B\n" +
	"        Compare two recordings frame by frame, and print where they first differ.\n" +
	"  " + screenReplayVerbSynopsis + " " + HelpFlag + "\n" +
	"        This page.\n" +
	"\n" +
	"The operand is a SESSION ID (resolved through the same directory as\n" +
	"`gopi session log`) or the PATH of a recording. Both file names are accepted:\n" +
	uiclient.ScreenStreamSuffix + " and the pre-rename " + uiclient.LegacyScreenStreamSuffix + "\n" +
	"(" + screenStreamSuffixSpellings() + ").\n" +
	"\n" +
	"WHAT GOES TO STDOUT, AND WHAT DOES NOT. Every frame's payload, in recorded\n" +
	"order, and nothing else: no canvas is built, no frame is paced, no colour is\n" +
	"rewritten and no line is re-wrapped, so the output is the recording's own\n" +
	"bytes and can be piped, hashed or captured. Every diagnostic — the counts and\n" +
	"how the recording ended — goes to STDERR, so a redirected stdout never carries\n" +
	"a word that was not in the recording.\n" +
	"\n" +
	"HOW THE RECORDING ENDED is stated in its own sentence, from the file's own\n" +
	"records: a recording whose close-out record is present and agrees with its\n" +
	"frames says nothing; one that is whole but was never closed says THAT; one\n" +
	"whose last frame runs past the end of the file says that, with the offset;\n" +
	"one whose own records contradict its own length says that. They are four\n" +
	"findings, never one boolean.\n" +
	"\n" +
	"Options:\n" +
	"  " + screenReplayVerbVerifyFlag + " A B      Replaces the write: compares recording A with recording B,\n" +
	"                    frame by frame, and prints the FIRST divergence — the\n" +
	"                    frame number, the byte offset of that frame in each\n" +
	"                    file, and both payloads as context. Prints\n" +
	"                    `" + screenReplayVerbIdenticalLine + "` when the frame sets are identical.\n" +
	"                    EXIT STATUS: 0 when identical, non-zero on a divergence\n" +
	"                    (and non-zero when the comparison could not run at all —\n" +
	"                    the words say which).\n" +
	"\n" +
	"`" + screenReplayVerbSynopsis + "` is the same reader as\n" +
	"`gopi " + CliVerbScreen + " " + screenSubcmdReplay + " " + screenReplayVerifyFlag + " OTHER`, which compares an implicit\n" +
	"session with one other recording and reports a divergence as a finding.\n" +
	"Use that one when a session is the subject; use this one when the recordings\n" +
	"are.\n" +
	"\n" +
	"Record one with " + cliverbs.ScreenStreamEnv + "=PATH (or =1).\n"
