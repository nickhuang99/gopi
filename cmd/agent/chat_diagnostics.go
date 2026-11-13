package main

// chat_diagnostics.go — where the agent's own diagnostics go while gopi chat
// OWNS the screen.
//
// The defect this closes (measured, 2026-09-19): the reserved chat layout
// (internal/lineedit) paints the answer band and the status row at ABSOLUTE
// rows and repaints them; the agent's logger writes to stderr, which is the
// SAME file descriptor. Nothing stops a log line from landing on a row the
// compositor owns — a 3-row wrapped `[gopi] ℹ️ thinking budget … clamped …`
// landed inside the band (raw capture: `[13;1H[2Kyou> what is in …` followed by
// a bare write of the diagnostic at row 12), and `[gopi] chat table: chat row
// ensured: …` painted over the status row. A wrapped write inside the scroll
// region also scrolls the whole screen, so the banner and the editing line move
// with it: the user reads "the UI is fighting itself".
//
// The fix is not to clip the logger (its lines are diagnostics, not layout) —
// it is to give the diagnostics a destination that is NOT this screen. A chat
// run in front of a terminal has no console record at all today
// (consoleLogAttachFor deliberately refuses a TTY), so the file the front run
// skips is exactly the file the diagnostics should fill: the session
// directory's console.log, beside session.jsonl where every other piece of this
// run's record already lives.
//
// The redirect is unconditional once the screen is owned: a diagnostic that is
// visible is also a diagnostic that destroys the UI, and "the band is intact"
// is the property the user asked for. The path is announced under --verbose
// (the same place the session paths are), and the file is one directory away
// from the session id the status row already shows.

import (
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/nick/gopi/internal/agent"
	"github.com/nick/gopi/internal/sessionlayout"
)

// chatDiagnosticsFile is the open console.log an agent's logger is currently
// pointed at, so the chat closeout can close exactly what it opened (one
// redirect per process — a chat run has one agent for its whole session).
var chatDiagnosticsFile *os.File

// routeChatDiagnostics points the agent's own logger at this session's
// console.log instead of the terminal, for a chat run whose answer band owns
// the screen. It reports the destination path, or "" when no redirect was made
// (no session log, no logger, or the file cannot be opened — in which case the
// caller is TOLD and stderr behaviour is left exactly as it was, Rule 15).
//
// screen is the layout's own single writer (the flow seam); when one is given
// the logger's output becomes an io.MultiWriter of BOTH destinations, so a
// notice is drawn as a whole row by the layout's one writer AND appended to the
// record. Passing nil keeps the file-only behaviour the branch had (a caller
// with no layout on screen).
//
// This is deliberately NOT "the redirected writer only": a writer that kept
// only the file would file the notice away from the user, and a writer that
// kept only stderr is the tear this file exists to remove.
func routeChatDiagnostics(ag *agent.Agent, screen io.Writer) (string, error) {
	if ag == nil || ag.Logger == nil || ag.SessionLog == nil {
		return "", nil
	}
	path := filepath.Join(sessionlayout.RecordContainer(ag.SessionLog.Path()), consoleLogFileName)
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, consoleLogMode)
	if err != nil {
		return "", fmt.Errorf("open %s: %w", path, err)
	}
	chatDiagnosticsFile = f
	if screen != nil {
		ag.Logger.SetOutput(io.MultiWriter(screen, f))
	} else {
		ag.Logger.SetOutput(f)
	}
	return path, nil
}

// closeChatDiagnostics releases the redirect (idempotent; called on every chat
// exit path). The logger is pointed back at stderr first, so a diagnostic
// emitted during closeout is not written to a closed file.
func closeChatDiagnostics(ag *agent.Agent) {
	if chatDiagnosticsFile == nil {
		return
	}
	if ag != nil && ag.Logger != nil {
		ag.Logger.SetOutput(os.Stderr)
	}
	if err := chatDiagnosticsFile.Close(); err != nil {
		// The run is ending either way; recording it is the point (Rule 15a).
		fmt.Fprintf(os.Stderr, "[gopi] chat: closing the diagnostics log: %v\n", err)
	}
	chatDiagnosticsFile = nil
}
