package main

// chat_stdin_guard.go — THE INTERACTIVE CONSOLE REFUSES A STDIN THAT CARRIES
// NOTHING, BEFORE IT MINTS ANYTHING.
//
// THE MEASURED DEFECT. `gopi chat < /dev/null` — the shape a backgrounded
// launch, a heredoc-less script and every "just try it" probe produces — used
// to run the whole start-up: it built the agent, which mints the session
// directory, the .raw.log, the session.jsonl and the exit marker, then read
// EOF immediately and exited 0. Measured on this host with a scratch profile
// (`GOPI_HOME=/home/nick/workspace/_tmp_A.u5od`):
//
//	gopi chat </dev/null   →  EXIT=0, session dirs 1
//	                          (id 3b9ab81b-bd8d-41d1-a0c6-4d68dbfe5a96,
//	                           session.jsonl: 2 lines — one session_start,
//	                           one session_end, elapsed_sec 0.381980258,
//	                           terminalGeometry.reason "no standard
//	                           descriptor is a terminal")
//
// Nothing was typed, nothing was answered, and a directory appeared in the
// operator's sessions tree that no reader can distinguish from a real
// conversation somebody abandoned. The idle shape is the same defect with a
// different ending: `(sleep 25 | gopi chat)` blocked until the caller's own
// timeout killed it (EXIT=124) with the session minted all the same.
//
// WHAT IS REFUSED, AND WHAT IS NOT. The test is not "is stdin a terminal" —
// that would refuse the two scripted shapes that are SUPPOSED to work. It is
// "does stdin carry anything at all": a terminal, a pipe, a regular file and a
// socket all carry bytes and all keep working exactly as before
// (`printf 'hello\n/quit\n' | gopi chat` answers and exits 0 — measured, 595
// bytes on stdout). A character device that is NOT a terminal (/dev/null) is
// the one shape with nothing behind it, and it is refused by name.
//
// WHAT THE REFUSAL NAMES (Rule 15: a refusal that does not say what to do
// instead is the same dead end as a silent one). Every non-interactive carrier
// this verb already has: `--once`, `--replay`, and a pipe.
//
// WHY IT SITS HERE AND NOT IN THE REPL. The REPL is far too late: by the time
// it reads, the session is already on disk (that is the whole defect). This
// guard stands where the flags are all resolved and BEFORE the agent exists —
// i.e. before the two minters (`agent.NewRawLoggerForRun`,
// `agent.NewSessionLoggerForRun`) can write anything. A refusal here leaves the
// sessions tree byte-identical, which is the assertion the regression test
// makes.
//
// THE SEAM. `chatStdinProbe` is a var for the same reason `chatAgentFactory`
// is: a `go test` process has /dev/null on stdin, so every existing test that
// drives this function past the agent would be refused by a check that is
// working correctly. Tests that want the interactive path say so, and the
// guard's own tests drive the seam in both directions.

import (
	"fmt"
	"os"

	"github.com/nick/gopi/internal/lineedit"
)

// The refusal text. It is ONE constant because two readers owe the same
// sentence: the refusal itself and the usage line it points at.
const (
	// chatStdinNoInputNotice is printed on stderr when the console is started
	// with a stdin that carries nothing.
	chatStdinNoInputNotice = "gopi chat: stdin carries no input — it is not a terminal, not a pipe and not a file, " +
		"so this interactive console would never receive a single keystroke. Nothing was started and no session was created.\n" +
		"  one answer from a script:  gopi chat --once \"<prompt>\"\n" +
		"  re-run a recording:        gopi chat --replay <session-id>\n" +
		"  pipe a conversation:       printf 'hello\\n/quit\\n' | gopi chat\n"
)

// chatStdinProbe is the seam: the guard asks it whether stdin carries input.
// Production reads the descriptor; a test states the answer.
var chatStdinProbe = chatStdinCarriesInput

// chatStdinCarriesInput reports whether the bytes this run would read exist at
// all.
//
// TRUE for a terminal (the ordinary case), a FIFO (a pipe from a caller that is
// feeding this run), a regular file (a script's here-document written out) and
// a socket.
//
// FALSE for a character device that is not a terminal — /dev/null is the one
// that matters — and for a nil descriptor. `lineedit.IsTerminal` deliberately
// cannot make this distinction on its own: it asks the kernel for termios and
// answers "no" for /dev/null, a pipe and a file alike (internal/lineedit/
// raw.go says why it asks the kernel rather than trusting ModeCharDevice). The
// distinction this guard needs is a different question, so it is asked
// separately here instead of being squeezed into that predicate.
func chatStdinCarriesInput(f *os.File) bool {
	if f == nil {
		return false
	}
	if lineedit.IsTerminal(f) {
		return true
	}
	info, err := f.Stat()
	if err != nil {
		// A descriptor that cannot be stat'ed is not evidence of input. The
		// guard refuses, and the refusal text names the three ways forward —
		// refusing is recoverable, minting a phantom session is not.
		return false
	}
	mode := info.Mode()
	// Ordered as the reader would ask: is there a stream, a file, or a socket?
	return mode.IsRegular() || mode&os.ModeNamedPipe != 0 || mode&os.ModeSocket != 0
}

// chatRunIsTheInteractiveConsole reports whether this run is the shape that
// needs a live keyboard at all. Every carrier that answers by itself is
// excluded by name:
//
//   - `--once` (and every scripted sequence) arrives as provided != nil — the
//     line source is the caller's, not the terminal's;
//   - `--replay <id>` re-runs a recording, driven by its own replay driver;
//   - `--resume <id>` and a seed continue a conversation from a recorded log,
//     which is read from disk rather than typed;
//   - `--film <dir>` produces frames from a recording (it requires --replay and
//     is dispatched before this point, but it is named here so the five
//     carriers are visible in one place).
func chatRunIsTheInteractiveConsole(provided *chatInput, replayTarget, resumeTarget, seed string, filmSet bool) bool {
	if provided != nil || filmSet {
		return false
	}
	return replayTarget == "" && resumeTarget == "" && seed == ""
}

// chatStdinGuardRefusal answers the guard's one question and, when the answer
// is a refusal, prints it. It returns the exit status the caller must use, and
// ok=false when the run may proceed.
//
// It is a function rather than two lines at the call site so the regression
// test can drive the decision without a terminal: the test asserts the PAIR
// (decision, text) for every stdin shape, and a second test asserts that a
// refused run leaves the sessions tree byte-identical.
func chatStdinGuardRefusal(provided *chatInput, replayTarget, resumeTarget, seed string, filmSet bool) (code int, refused bool) {
	if !chatRunIsTheInteractiveConsole(provided, replayTarget, resumeTarget, seed, filmSet) {
		return chatExitOK, false
	}
	if chatStdinProbe(os.Stdin) {
		return chatExitOK, false
	}
	fmt.Fprint(os.Stderr, chatStdinNoInputNotice)
	return chatExitFailed, true
}
