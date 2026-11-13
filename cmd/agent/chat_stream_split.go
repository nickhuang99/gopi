package main

// chat_stream_split.go — WHICH STREAM THE INTERACTIVE CHAT LAYOUT MAY BE PAINTED
// ON, and which stream carries this surface's diagnostics.
//
// THE CONVENTION (one decision, stated once, enforced by the two accessors below)
//
//	fd 1 (stdout) — THE LAYOUT STREAM. The banner, the reserved layout, the
//	                answer band, the turn headers, the status row, the editing
//	                line, the goodbye: every byte a READER is meant to see
//	                painted on the screen. This is the stream a filmer captures.
//	fd 2 (stderr) — THE DIAGNOSTIC STREAM. Errors, refusals, degradations and
//	                the `[gopi] …` plumbing notices: what a reader must be TOLD
//	                even when the layout is not on screen.
//
// WHY THE SPLIT EXISTS. A terminal is not one stream: a launcher may point its
// two descriptors at two different destinations, and a FILMER must be able to
// say "give me the screen, file the noise". The measured case (this task's
// epic): scripts/replay/replay.sh runs the product inside a private pty and
// redirects the child's stderr to a log — deliberately, so that a diagnostic
// line can never be drawn onto the screen a frame photographs — while it
// photographs what the pty carries. When the layout went to fd 2, the pty
// carried only the two startup lines, every frame was the same blank window
// (16 beats, 1 distinct PNG, one md5) and the layout itself was sitting in
// `<scratch>/replay-stderr.log`.
//
// THE RULE, in one sentence: A STREAM MAY FUNCTION AS THE LAYOUT STREAM ONLY IF
// EVERY DESCRIPTOR A TERMINAL OWNS IS THE SAME DESTINATION. That is the criterion
// — not "fd 1" as a spelling, and not "fd 2" as a spelling. It is what makes the
// choice SAFE as well as correct: when stdout and stderr are the same pty (a
// human's terminal, always, and every pty-based test harness here), moving the
// layout's bytes between the two changes NOTHING a reader can observe — same
// destination, same order, and the layout's painters reach the terminal through
// whichever descriptor they were handed. When the two differ (a redirected run),
// fd 1 is the only defensible answer, because it is the descriptor a caller
// photographs by default and because fd 2 is the one a filmer must be able to
// redirect to a log without losing the film.
//
// THE FALLBACK, and why it is not a second policy: when fd 1 is NOT a terminal
// and fd 2 IS, the layout goes to fd 2 — i.e. exactly where it went before this
// convention existed (`gopi chat 2>/dev/null` and `gopi chat >file` keep the
// bytes they always produced, and a run whose stdout is a file still gets the
// layout on the terminal the human is actually looking at). Piping stdout is
// UNAFFECTED: the non-interactive flow path writes to fd 1 by construction (see
// chatInput.flowWriter), so `echo … | gopi chat > out.txt` captures exactly what
// it always captured.

import (
	"io"
	"os"

	"github.com/nick/gopi/internal/lineedit"
)

// The two descriptors, named (Rule 1: the decision is data, not an incidental
// number typed at each call site). They are the convention's own vocabulary and
// the regression test reads them from here.
const (
	// chatLayoutFd is the LAYOUT stream: what a reader is meant to see, and what
	// a filmer photographs.
	chatLayoutFd = 1
	// chatDiagnosticFd is the DIAGNOSTIC stream: what a reader must be told.
	chatDiagnosticFd = 2
)

// chatLayoutFile is the descriptor the layout stream resolves to on this run:
// fd 1 when it is a terminal, else fd 2. It is the ONE resolver behind the
// accessors below — see the criterion in this file's header.
//
// The test is lineedit.IsTerminal, which asks the KERNEL about the descriptor
// (the termios ioctl succeeds), so the answer is a measurement of this run's
// descriptors rather than a guess about how it was launched.
func chatLayoutFile() *os.File {
	if lineedit.IsTerminal(os.Stdout) {
		return os.Stdout
	}
	// NOT A REFUSAL. A run with a redirected stdout is ordinary (a filmer's
	// second shape, a shell that captured the transcript), and the layout still
	// has a terminal to be drawn on in every such run that can draw at all —
	// stdin's own terminal, reported through fd 2. The refusal for a run with NO
	// terminal anywhere is the editor's own (newChatInputWithPalette degrades to
	// the cooked path and says so), and the cooked path writes its own prompt to
	// this same resolver's answer.
	return os.Stderr
}

// chatLayoutStream is the LAYOUT stream as a writer: where the banner, the
// reserved layout, the answer band, the status row and the goodbye are painted.
func chatLayoutStream() io.Writer { return chatLayoutFile() }

// chatLayoutSink is chatLayoutStream as the *os.File the geometry probe and the
// capability decision need (both ask the kernel about a descriptor, which a
// writer cannot express).
func chatLayoutSink() *os.File { return chatLayoutFile() }

// chatDiagnosticStream is the DIAGNOSTIC stream: the errors and the `[gopi] …`
// plumbing notices. It is a function rather than a bare os.Stderr so the
// convention has ONE spelling, and so a reader of this file can see both halves
// of it side by side.
func chatDiagnosticStream() io.Writer { return os.Stderr }
