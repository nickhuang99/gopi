package main

import (
	"fmt"
	"os"

	"github.com/nickhuang99/gopi/internal/gopilog"
	"github.com/nickhuang99/gopi/internal/sessionevent"
)

// runSessionToolCall prints ONE tool call of a session in full: `gopi session
// log <id> --toolcall N`.
//
// WHY IT IS A FLAG AND NOT A ROUND (the defect it answers). "How many rounds?"
// had four answers on four surfaces for one session, and the largest
// (`gopi session stats --rounds`, 339) was precisely the session's tool-call
// count: calls were being counted under the word "round". The ability to point
// at "the 3rd tool call" is real and is kept — the WORD is what changed, so the
// count that means calls says so (sessionevent.CounterUnitToolCall) and the
// canonical round (one `request` row = one HTTP request/response) says that.
//
// The rendering itself is internal/gopilog's (FormatSessionToolCall), so this
// file owns no numbering and no format of its own.
func runSessionToolCall(sessionID string, opts *logOptions) {
	dir, err := gopilog.ResolveSessionDir()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
	path, err := findSessionLogPath(dir, sessionID)
	if err != nil {
		fmt.Fprintf(os.Stderr, "gopi session log: %v\n", err)
		os.Exit(1)
	}
	if path == "" {
		fmt.Fprintf(os.Stderr, "Session not found: %s\n", sessionID)
		os.Exit(1)
	}
	s, err := gopilog.ParseFile(path)
	if err != nil {
		fmt.Fprintf(os.Stderr, "gopi session log: %v\n", err)
		os.Exit(1)
	}
	calls := gopilog.SessionToolCalls(s)
	n, ok := gopilog.SessionToolCallIndex(s, opts.toolCallSel)
	if !ok {
		fmt.Fprintf(os.Stderr,
			"gopi session log: tool call %d does not exist — session %s recorded %d %s (1-based, in log order; a tool call is an event inside a round, so this is NOT the round count)\n",
			opts.toolCallSel.Round, s.ID, len(calls), sessionevent.CounterUnitToolCall)
		os.Exit(1)
	}
	fmt.Print(gopilog.FormatSessionToolCall(s, calls[n], len(calls)))
}
