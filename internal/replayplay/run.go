package replayplay

// run.go — the ONE entry point a caller uses to play a material on a terminal.
//
// The order of operations is the contract, and it is the same on every exit
// path:
//
//	load the material  →  take the terminal (raw + alt screen)  →  arm the
//	signal watcher  →  play  →  restore (deferred, so a panic or an error
//	restores too)  →  join the watcher
//
// Nothing here writes to stdout before the terminal is taken: a caller that
// refused (no material, no tty) must leave the operator's screen exactly as it
// found it.

import (
	"fmt"
	"io"
	"os"

	"github.com/nickhuang99/gopi/internal/lineedit"
)

// PlaybackCommand is what this package's chrome CALLS the operation it performs:
// the status line's title, the help overlay's title and the two refusals in Run
// are all built from this ONE value (Rule 13).
//
// IT IS A DESCRIPTION, NOT A DISPATCH WORD. There is deliberately no verb
// constant for it anywhere: a word that reaches a DISPATCH site is owned by the
// dispatch side, which here is cmd/agent's session sub-verb table (`replay`, a
// sub-verb of the already-censused `session` group). The top-level `gopi replay`
// verb is ABOLISHED and stays un-dispatched
// (devdoc/replay-abolished-verb-single-route.md; the regression gate is
// cmd/agent/session_replay_abolition_test.go).
const PlaybackCommand = "gopi session replay"

// RunOptions is what a caller tells Run.
type RunOptions struct {
	// Mat is the loaded material to play.
	Mat Material
	// In is the terminal the player reads keys from. It MUST be a terminal:
	// Run refuses anything else rather than reading from a pipe (a playback
	// driven by a script is a different, deliberate thing — see RunScripted).
	In *os.File
	// Out receives the frames. It is the SAME terminal as In in every ordinary
	// invocation (they are the two halves of one tty).
	Out io.Writer
	// Nowhere suppresses the alternate screen, so a caller capturing the bytes
	// (a test, a pipe) gets the frames without the screen swap. The terminal
	// state is still restored.
	NoAltScreen bool
}

// Run plays mat interactively on the terminal In/Out and returns when the
// operator quits.
func Run(opts RunOptions) error {
	if opts.Mat.PlayableUnits() == 0 {
		return fmt.Errorf("no rounds to play: the material is empty")
	}
	if opts.In == nil {
		return fmt.Errorf("no input terminal: `%s` needs a terminal to read keys from", PlaybackCommand)
	}
	if !lineedit.IsTerminal(opts.In) {
		return fmt.Errorf("stdin is not a terminal: `%s` is interactive (a playback needs a tty to read keys from and to restore afterwards)", PlaybackCommand)
	}
	sess, err := OpenSession(opts.In, opts.Out)
	if err != nil {
		return err
	}
	// The restore is deferred BEFORE the watcher is armed, and the watcher's own
	// Close is idempotent — so a signal, a quit key, an error and a panic all
	// pass through exactly one restore path.
	defer func() { _ = sess.Close() }() //stylegate:ignore Close is idempotent and the restore is deliberately deferred before the watcher is armed, so every exit path passes through one restore
	stopSignals := sess.WatchSignals()
	defer stopSignals()

	cols, rows := sess.Size(opts.Mat.Cols, opts.Mat.Rows)
	player := NewPlayer(opts.Mat, sess, cols, rows, truecolorFor(opts.Out))
	return player.Run()
}

// truecolorFor reports whether a sink can show 24-bit colour, using the SAME
// capability probe the rest of gopi uses (internal/lineedit's environment
// reading) rather than a second opinion about the terminal.
//
// A sink that is not a file (a buffer, a pipe) is treated as capable: the bytes
// are then faithful to the recording, and a downstream consumer that cannot show
// truecolour is the downstream's own degradation to announce.
func truecolorFor(out io.Writer) bool {
	f, ok := out.(*os.File)
	if !ok {
		return true
	}
	return lineedit.ColourWanted(false, f) == lineedit.ColourOn
}
