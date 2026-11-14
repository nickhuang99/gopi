package terminalgeom

// terminalgeom is the ONE measurement of "what terminal is this process on", in
// the shape a session records (internal/sessionevent.TerminalGeometry).
//
// WHY A PACKAGE. Two producers ask this question — the session writer
// (internal/agent, on session_start) and the chat verb (its own diagnostic /
// record) — and a second copy of the descriptor ORDER or of the ioctl would make
// two runs record two different answers for the same terminal. The ioctl itself
// is NOT re-implemented here: lineedit.TerminalCols/TerminalRows ARE the
// package's TIOCGWINSZ request (Rule 14), and this file only decides WHICH
// descriptor to ask.
//
// THE ORDER IS THE SURFACE'S OWN. stdout first, then stderr, then stdin: the
// chat layout paints on stdout when stdout is a terminal and on stderr when it
// is not (cmd/agent/chat_stream_split.go's chatLayoutFile), so the first two
// are the descriptors a painted surface could be using, and stdin is the
// fallback for a run whose output is redirected but which still has a keyboard.
// A run where all three answer ENOTTY — the `--once` face, a filer's producer
// before its pty is installed, a CI run — has NO terminal, and that is recorded
// as UNKNOWN rather than as a zero: an unmeasurable geometry and a terminal that
// is 0 cells wide are different facts, and only one of them exists (Rule 15a).

import (
	"os"

	"github.com/nickhuang99/gopi/internal/lineedit"
	"github.com/nickhuang99/gopi/internal/sessionevent"
)

// Standard measures the geometry of this process's own standard descriptors.
//
// It never returns a "measured" geometry that is not a real size: a descriptor
// that answers 0 (the kernel refused, or reported a zero dimension) is not a
// measurement — terminalgeom moves on to the next descriptor and, when none
// answers, reports UNKNOWN with the reason.
func Standard() sessionevent.TerminalGeometry {
	return measure(standardDescriptors())
}

// measure is the order-and-threshold logic of Standard, taking the descriptor
// list as a parameter so the check can be driven against a terminal the test
// OWNS (a pty of a known size) instead of the descriptors the test binary
// happens to have — which under `go test` are pipes. The production list and the
// test's list go through the SAME code, so a test cannot pass while the shipped
// probe asks a different question.
func measure(files []*os.File) sessionevent.TerminalGeometry {
	for _, f := range files {
		if f == nil {
			continue
		}
		cols, rows := lineedit.TerminalCols(int(f.Fd())), lineedit.TerminalRows(int(f.Fd()))
		if cols > 0 && rows > 0 {
			return sessionevent.MeasuredTerminalGeometry(cols, rows)
		}
	}
	return sessionevent.UnknownTerminalGeometry(sessionevent.GeometryReasonNoTerminal)
}

// standardDescriptors is the probe order (see the file comment). It is a
// function rather than a package-level slice so a caller cannot mutate the
// order for everyone else, and so a test can call the SAME list the production
// path walks rather than restating it.
func standardDescriptors() []*os.File {
	return []*os.File{os.Stdout, os.Stderr, os.Stdin}
}
