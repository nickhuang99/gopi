package main

// cli_screen.go — the `gopi screen` GROUP verb: the family that holds the
// readers of a RECORDED SCREEN.
//
// WHY IT EXISTS, and why it is a sibling of `gopi session replay` rather than a
// flag on it. `session replay` paints a recording back as a PICTURE: it is a
// RENDERER, so it chooses a canvas, paces the frames and falls back to the
// reduced dump when a session recorded no stream — every one of those decisions
// is right for a human watching, and every one of them is a step away from the
// bytes that were actually written. A reader that wants the BYTES (a diff
// between two runs, a replay into a file, a capture nobody throttles) cannot get
// them from a renderer without subtracting the renderer's own decisions, and the
// subtraction is not something a caller can do: `--from stream` on
// `session replay` still walks through the same player, so what it emits is the
// player's picture.
//
// `gopi screen replay` writes the payload bytes and nothing else, and it REFUSES
// a session with no stream instead of substituting the dump — a substitution
// that would answer a different question in the same shape (Rule 15).
//
// IT IS A GROUP AND NOT A TOP-LEVEL WORD FOR THE PLAYBACK. The 2026-10-03
// abolition (devdoc/replay-abolished-verb-single-route.md) deleted the top-level
// `gopi replay` because the verb list must not grow for every reader of a
// session id; the same reasoning puts a reader of a RECORDED SCREEN under the
// noun that names the artifact, never back at the top level.
// cmd/agent/session_replay_abolition_test.go still pins the banned word, and
// this group adds no top-level word for it: `replay` lives one level down,
// twice, in two families that answer two different questions.

import (
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/nick/gopi/internal/cliverbs"
	"github.com/nick/gopi/internal/uiclient"
)

// screenSubcmdReplay is the family's first sub-verb word. It is a SUB-VERB
// WORD, never a top-level one (see this file's header).
const screenSubcmdReplay = "replay"

// screenSubcommandNames is the family's ONE vocabulary: the dispatcher below,
// the unknown-sub-verb error, the family page and the tests all read this
// slice, so a new sub-verb reaches every surface or none (Rule 13/14).
var screenSubcommandNames = []string{
	screenSubcmdReplay,
}

// screenStreamSuffixSpellings is the file-name vocabulary this page prints,
// taken from the package that owns the grammar rather than re-typed (Rule 14):
// a page that named a suffix no reader accepts would be teaching a dead end.
func screenStreamSuffixSpellings() string {
	return strings.Join(uiclient.ScreenStreamSuffixes[:], " / ")
}

// screenUsage is the family page (`gopi screen -h`). EVERY `Usage:` line is one
// command an operator can type — sessionUsage's shape, and for its reason: the
// sub-verb's detail is one hop away in its own page.
var screenUsage = `GoPi screen subcommands — read a RECORDED SCREEN as the bytes that painted it.

Usage:
  gopi ` + CliVerbScreen + ` ` + screenSubcmdReplay + ` <session-id>
        Write the recorded render bytes to stdout, unchanged.
  gopi ` + CliVerbScreen + ` ` + screenSubcmdReplay + ` <session-id> ` + screenReplayVerifyFlag + ` OTHER
        Compare two recordings frame by frame, and print where they differ.
  gopi ` + CliVerbScreen + ` -h
        This page.

TWO QUESTIONS LIVE ONE LEVEL DOWN FROM THE WORD replay, and they are not
interchangeable:

  ` + sessionReplaySynopsis + `
        PLAYS the recording back — a renderer. It picks a canvas, paces the
        frames, and falls back to the reduced dump when the session recorded no
        stream. The answer is "what was on the screen".

  gopi ` + CliVerbScreen + ` ` + screenSubcmdReplay + ` <session-id>
        WRITES the recording — the render bytes exactly as the run wrote them,
        with no canvas, no pacing, no colour change and no fallback. The answer
        is "what bytes did the renderer emit", so the output can be piped,
        diffed and captured without a terminal.

A session that recorded no screen stream has no bytes to write, and this family
REFUSES it by name, naming ` + sessionReplaySynopsis + ` — the reader that can
still show something. It never substitutes the dump: a dump is this program's
interpretation of the paint bytes, so printing one where a caller asked for the
bytes would answer a different question in the same shape.

The artifact is <session-id>` + uiclient.ScreenStreamSuffix + `, or the pre-rename
` + uiclient.LegacyScreenStreamSuffix + `. Accepted names: ` + screenStreamSuffixSpellings() + `.
Record one with ` + cliverbs.ScreenStreamEnv + `=PATH (or =1).
`

// isScreenSubcommand reports whether argv begins with the `screen` group verb.
// It consumes the full argv and handles its own exit, exactly like every other
// subcommand predicate in this package.
func isScreenSubcommand(args []string) bool {
	if len(args) == 0 || args[0] != CliVerbScreen {
		return false
	}
	os.Exit(runScreenGroup(args[1:], os.Stdout, os.Stderr))
	return true
}

// runScreenGroup dispatches one `gopi screen …` invocation and RETURNS the exit
// status — it does not exit the process itself. The distinction is not style: a
// dispatcher that calls os.Exit cannot be executed by a test, so its refusals
// (the bare group, an unknown sub-verb) would be pinned by a copy of their
// sentence instead of by running them. `isScreenSubcommand` above is the ONE
// place that turns this status into a process exit.
//
// THE BARE GROUP AND AN UNKNOWN SUB-VERB ARE USAGE ERRORS, never an agent run —
// the M4.13 lesson every group in this package already follows: a mistyped
// command that fell through to the loop would spend a whole LLM session on it.
func runScreenGroup(args []string, out io.Writer, errw io.Writer) int {
	if len(args) == 0 {
		fmt.Fprintf(errw, "gopi %s: missing subcommand (want %s)\n%s",
			CliVerbScreen, strings.Join(screenSubcommandNames, "|"), screenUsage)
		return chatExitFailed
	}
	// -h BEFORE the sub-verb is the GROUP page; after one it belongs to THAT
	// sub-verb (the split sessionUsage documents, and for the same reason).
	if args[0] == HelpFlag || args[0] == HelpFlagLong {
		fmt.Fprint(out, screenUsage)
		return chatExitOK
	}
	verb, rest := args[0], args[1:]
	if !containsVerb(screenSubcommandNames, verb) {
		fmt.Fprintf(errw, "gopi %s: unknown subcommand %q (want %s)\n%s",
			CliVerbScreen, verb, strings.Join(screenSubcommandNames, "|"), screenUsage)
		return chatExitFailed
	}
	switch verb {
	case screenSubcmdReplay:
		// The streams are FORWARDED, never re-bound to os.Stdout here: the ONE
		// place that binds the real process streams is isScreenSubcommand, so a
		// test can drive the whole group and watch what a caller would receive.
		// (Measured: routing through runScreenReplay instead made the injected
		// writer dead and every group-level assertion read an empty buffer.)
		return screenReplayRun(rest, out, errw)
	}
	// Unreachable while screenSubcommandNames and this switch agree; the return
	// keeps the contract explicit if a word is added to the list and not here.
	fmt.Fprintf(errw, "gopi %s: subcommand %q is in the vocabulary but has no route (a defect in this file)\n",
		CliVerbScreen, verb)
	return chatExitFailed
}
