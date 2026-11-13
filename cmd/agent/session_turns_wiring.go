package main

// session_turns_wiring.go — WHO INSTALLS THE INDEX ROWS' TURN COUNTER.
//
// THE LAYERING PROBLEM IT SOLVES, measured rather than assumed (`go build
// ./internal/tools` with the import in place, 2026-10-26):
//
//	github.com/nick/gopi/internal/tools
//		imports internal/gopilog
//		imports internal/agent      (formatter.go)
//		imports internal/hippocampus
//		imports internal/tools: import cycle not allowed
//
// The ONE turn counter — gopilog.SessionTurns, whose predicate is
// sessionevent.IsTurnOpeningRow — therefore cannot be reached from
// internal/tools by import. The alternatives were both worse: a second scanner
// inside internal/tools is the MIRROR Rule 14 forbids (two predicates over one
// log, silently drifting), and moving the counter out of gopilog would take the
// definition away from the four surfaces that already read it. So internal/tools
// DECLARES the dependency (tools.ChatSessionTurnsReader, chat_turns.go) and this
// file — the binary's own entry point, which already imports both packages —
// INJECTS the one implementation.
//
// WHY IT MUST RUN BEFORE THE SUBCOMMAND CHAIN in main(): the group verbs return
// from main() at that chain, and `gopi session list` is one of them. A wiring
// placed after it would leave exactly the verb this epic is about with no
// counter — the feature would look absent while compiling cleanly (Rule 31: the
// surface must be executed, not merely built).

import (
	"github.com/nick/gopi/internal/gopilog"
	"github.com/nick/gopi/internal/tools"
)

// sessionTurnsFromLog is the injected implementation: parse the row's own
// session log ONCE and take both turn counts from it, the SAME functions
// `gopi session log <id> --turns`, `gopi chat --replay` and `gopi session stats`
// print (internal/gopilog.SessionTurns / SessionHumanTurns).
//
// ONE PARSE, TWO NUMBERS: the opened count is what the bare word `turns` prints
// (the axis the replays number along) and the human count is the subset the show
// page names. Parsing twice would double the read for a row that needs both.
//
// A parse failure is RETURNED, not folded into zeros: "this log exists and could
// not be read" and "this log holds no operator row" are different facts, and the
// caller renders them differently (tools' ChatTurnsNote vs a measured zero).
func sessionTurnsFromLog(logPath string) (tools.ChatSessionTurnCounts, error) {
	s, err := gopilog.ParseFile(logPath)
	if err != nil {
		return tools.ChatSessionTurnCounts{}, err
	}
	return tools.ChatSessionTurnCounts{
		Opened: gopilog.SessionTurns(s),
		Human:  gopilog.SessionHumanTurns(s),
	}, nil
}

// installSessionTurnsReader publishes the counter into the tools layer. It is
// idempotent and silent — there is no state to report, only a function value
// handed over — and it is called from main() and from the suite's TestMain, the
// two entry points whose processes render `session list`.
func installSessionTurnsReader() {
	tools.InstallChatSessionTurnsReader(sessionTurnsFromLog)
}

// installLedgerTurnCounter is the SAME hand-over in the OTHER direction: the
// renderers in internal/gopilog print the LEDGER's turn count
// (`chats.turn_count`) under the bare word `turns`, and internal/gopilog cannot
// import internal/tools, so this binary — which imports both — publishes the
// reader. Without it `gopi session log <id>` would state that the count was not
// measured, which is honest but useless (Rule 15: absence is stated, but a
// shipped surface should not be absent by default).
//
// It is called beside installSessionTurnsReader() at both entry points, so the
// two directions of the cycle-break are installed by the same two lines and a
// future entry point cannot get one without the other.
func installLedgerTurnCounter() {
	gopilog.InstallLedgerTurnCount(tools.ChatLedgerTurnCount)
}
