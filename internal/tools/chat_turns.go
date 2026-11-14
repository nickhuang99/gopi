package tools

import (
	"fmt"

	"github.com/nickhuang99/gopi/internal/sessionevent"
)

// chat_turns.go — THE PERSON-ONLY SUBSET OF A SESSION'S TURNS, read from the
// session log through the ONE counter that owns it.
//
// THE NUMBER THIS FILE MEASURES IS A DETAIL, NOT THE TURN COUNT. The turn count
// is `chats.turn_count` — the turns the session RAN, every input face equal —
// and it is already on the index row (tools.ChatSessionDetail.TurnCount, printed
// under the bare word `turns`). What this file adds is the narrower question
// "how many of those did the PERSON open?", which a reader can ask for on the
// show page and which must never be printed as a rival total of the count.
//
// THE HISTORY, because the two used to swap places (measured 2026-10-26 on
// session 80c07a7d-bb6b-48b4-be66-ea1867f403f2): `gopi session list --mode chat`
// printed `repl turns 18` — `chats.turn_count` under a qualifier — while `gopi
// session log <full-id>` printed `Turns: 11` for the operator's own rows. Two
// surfaces, two numbers, one word family. The first repair qualified BOTH
// (`turns 66 (repl turns 200)`); the operator's ruling (2026-10-27) is that the
// pair has no subject — an external notify is an input, a self-driven turn is a
// turn — so the count keeps the bare word alone and the subset is what needs the
// qualifier.
//
// WHY A HOOK AND NOT AN IMPORT (measured, `go build` this tree 2026-10-26):
//
//	internal/tools → internal/gopilog   ⇒   … → internal/agent → internal/tools
//	                                        import cycle not allowed
//
// The counter lives in internal/gopilog (gopilog.SessionTurns, whose predicate is
// internal/sessionevent.IsHumanRoundBoundary) and that package cannot be
// imported from here. The alternative — a second scanner in this package that
// counts operator rows itself — is exactly the MIRROR Rule 14 forbids: two
// predicates over the same log drift, and the drift is invisible because each
// surface is internally consistent. So internal/tools DECLARES the dependency
// and the binary's own entry point (cmd/agent) INJECTS the one implementation:
// `gopilog.ParseFile` → `gopilog.SessionTurns`, which is the same function
// `gopi session log --turns` and `gopi chat --replay` print.
//
// AN UNINSTALLED READER IS NOT A FAILURE, it is an UNMEASURED FACT, and it is
// stated rather than hidden (Rule 15): the show page says WHY the subset is
// unknown (ChatSessionDetail.TurnsNote) and the count above it is unaffected,
// because the count comes from the ledger and needs no log at all.

// ChatSessionTurnCounts is ONE session log's two turn readings, taken in ONE
// parse: how many turns the log OPENED (the axis `--turn N`, a replay band and
// the `--turns` table all number along) and how many of those a PERSON opened.
//
// BOTH COME FROM THE SAME PARSE ON PURPOSE. The alternative — two readers over
// one file — is two parses of a 600 MB log per index row and, worse, two
// predicates that could drift (Rule 14).
type ChatSessionTurnCounts struct {
	// Opened is gopilog.SessionTurns: one per turn boundary the log recorded.
	Opened int
	// Human is gopilog.SessionHumanTurns: the subset whose opening row the log
	// records as the person's own input.
	Human int
}

// ChatSessionTurnsReader reads one session log's turn counts from disk. It is
// internal/gopilog's SessionTurns + SessionHumanTurns behind a function value —
// see this file's header for why the dependency cannot be an import.
//
// An error means the log EXISTS and could not be read. That is a fact the caller
// must carry (it is not the same as "the log is not here"), so the reader
// returns it rather than a zero.
type ChatSessionTurnsReader func(logPath string) (ChatSessionTurnCounts, error)

// chatSessionTurnsReader is the injected counter. nil = nothing published (a
// library caller, a test that did not install one), which every render states.
var chatSessionTurnsReader ChatSessionTurnsReader

// InstallChatSessionTurnsReader publishes the process's turn counter. It is
// called ONCE, by cmd/agent, at the top of main() and of the suite's TestMain —
// the two entry points of the binary that owns the ledger and the renderer both.
//
// A nil argument CLEARS the reader (a test restoring the process state); it is
// not a way to disable the feature silently, because the render for an
// unmeasured row already says which count it is printing.
func InstallChatSessionTurnsReader(r ChatSessionTurnsReader) {
	chatSessionTurnsReader = r
}

// ChatSessionTurnsInstalled reports whether a counter is published — the fact a
// caller (and a test) checks instead of guessing from a rendered string.
func ChatSessionTurnsInstalled() bool { return chatSessionTurnsReader != nil }

// chatMeasuredTurns reads one row's turn counts out of its own session log.
//
// ok=false means UNMEASURED, and note says why in words a reader can use:
//   - no reader was installed (a library caller, or a test);
//   - the row's session log is not on this machine (its FilePresent is false);
//   - the log is here but could not be read — note carries the error.
//
// It never returns (zero, false) for a log that really holds no operator row:
// that is a MEASUREMENT of zero turns and comes back ok=true. Returning them the
// same way would make "the person typed nothing" and "nobody looked" one value —
// the silent substitution Rule 15 exists to forbid.
func chatMeasuredTurns(logPath string, present bool) (counts ChatSessionTurnCounts, ok bool, note string) {
	if !present || logPath == "" {
		return ChatSessionTurnCounts{}, false, ChatTurnsLogAbsentNote
	}
	if chatSessionTurnsReader == nil {
		return ChatSessionTurnCounts{}, false, ChatTurnsNoCounterNote
	}
	n, err := chatSessionTurnsReader(logPath)
	if err != nil {
		return ChatSessionTurnCounts{}, false, ChatTurnsUnreadablePrefix + err.Error()
	}
	return n, true, ""
}

// The three ways a row's turn count can be unmeasured, spelled ONCE (Rule 1/13:
// both the index row and the show page read these, so the two surfaces cannot
// describe the same gap in two sentences).
const (
	// ChatTurnsLogAbsentNote: the row is in the index, its log is elsewhere.
	ChatTurnsLogAbsentNote = "not measured — this machine does not hold this session's log"
	// ChatTurnsNoCounterNote: the binary that rendered the row installed no turn
	// counter. It names the mechanism so the gap is diagnosable rather than
	// mysterious.
	ChatTurnsNoCounterNote = "not measured — no turn counter is installed in this process (internal/gopilog.SessionTurns is injected by the gopi binary's entry point)"
	// ChatTurnsDirUnresolvedNote: the sessions tree itself could not be
	// resolved, so neither the presence test nor the turn read could run. It is
	// a DIFFERENT fact from ChatTurnsLogAbsentNote — "I could not look" vs "I
	// looked and it is not here" — and rules out a claim the process never made.
	ChatTurnsDirUnresolvedNote = "not measured — the sessions directory could not be resolved in this process, so neither the log's presence nor its turns could be read"
	// ChatTurnsUnreadablePrefix opens the read-failure note; the parse error
	// follows it, so the reader learns WHICH log and WHY (Rule 15: an error is
	// recorded, never swallowed into a dash).
	ChatTurnsUnreadablePrefix = "not measured — the session log could not be read: "
)

// chatTurnClause renders one index row's turn fact.
//
// THE BARE WORD CARRIES THE LOG'S OPENED COUNT (epic 2026-11-04). It used to
// carry `chats.turn_count`, and the two disagree by design: the ledger advances
// when a turn COMPLETES (cmd/agent/cli_chat.go's `runErr == nil && !interrupted`
// gate), the log records a seam when one OPENS. Measured on session
// 3a93ac1c-c0a3-448d-a14f-9eb673b64f9f: `session list` printed `turns 29` while
// `session replay --turns` printed 30 rows and the replay screen's last band read
// `──── turn 30` — the session's thirtieth turn opened and then quit, so the
// ledger never charged it. One session, one word, two numbers.
//
// WHY THE OPENED COUNT IS THE ONE THE BARE WORD KEEPS: `--turn N`, a replay band
// number and a table row are POSITIONS, and only a position count can carry one.
// A completed-count sits one behind this session and — measured on
// e364e357-a9ea-4210-a0d3-c606c6997f03 — 175 AHEAD of it (ledger 209 against 34
// boundaries), so anchoring the ordinal on it addresses a different turn on
// either side of that line. `chat --replay` and `session replay` already number
// along the opened axis; this row now does too.
//
// THE LEDGER'S NUMBER IS NOT DELETED, IT MOVES UNDER ITS OWN WORD: the completed
// count prints as `turns completed N` whenever it differs, so the gap is
// accounted in numbers instead of a reader meeting two unqualified totals
// (sessionevent.CounterUnitTurnCompleted). An unmeasured row prints the reason in
// place of both numbers (Rule 15) — never a 0.
func chatTurnClause(d ChatSessionDetail) string {
	if !d.TurnsKnown {
		// AN UNMEASURED ROW CARRIES NO COMPLETED CLAUSE, because on a ONE-LINE
		// row the two clauses sit side by side with nothing but punctuation
		// between them and the ledger's number then reads as a measurement of the
		// word it stands beside (measured: `turns session log not on this machine
		// (completed 18)` was reported as exactly that ambiguity). The ledger's
		// number is not lost — it is on the per-session faces, where a row of its
		// own says which quantity it is.
		return fmt.Sprintf(ChatTurnUnmeasuredFormat, sessionevent.CounterUnitTurn, d.TurnsNote)
	}
	// ONE NUMBER ON A ONE-LINE ROW. The ledger's completed count prints on the
	// per-session faces (`session show`, `stats`, `log`, `replay`), where a reader
	// is asking about THIS session; on the index it would ride nearly every row
	// (measured: 15 of 15 rows differ on this host) and turn a scannable table
	// into a two-number column — the 2026-10-27 ruling's own point, kept while
	// the NUMBER it protects changed.
	return fmt.Sprintf(ChatTurnClauseFormat, sessionevent.CounterUnitTurn, d.TurnsOpened)
}

// The clause's spelling (Rule 1: the only variables are the numbers).
const (
	// ChatTurnClauseFormat is the bare word and its number.
	ChatTurnClauseFormat = "%s %d"
	// ChatTurnUnmeasuredFormat states an unmeasured row, with its reason.
	ChatTurnUnmeasuredFormat = "%s %s"
)

// A `ChatTurnCompletedFormat` CONSTANT WAS WRITTEN HERE AND IS GONE (delivery
// rebase, 2026-11-04): it spelled ` (%s %d)` — the ledger's number appended in
// parentheses — and NOTHING formatted with it, because chatTurnClause above
// decided the opposite for a measured reason (`turns session log not on this
// machine (completed 18)` reads as a measurement of the word beside it). A
// constant whose doc promises a rendering the renderer refuses is the same
// defect class as a rule nobody obeys: it survives by being referenced by no
// one. The ledger's count is published on the per-session faces instead.
