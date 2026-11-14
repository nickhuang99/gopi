package gopilog

// ledger_turns.go — THE TURN COUNT THIS PACKAGE CANNOT READ, AND HOW IT GETS IT.
//
// THE DEFECT THIS FILE REPAIRS (measured, 2026-10-25/27). There are two numbers
// in this tree that answer "how many turns did this session run", and the word
// `turns` was printed for whichever one the surface happened to be able to
// reach:
//
//	`chats.turn_count`      — the LEDGER's count: every turn the session RAN,
//	                          whatever face opened it (the person's line, a
//	                          `gopi notify` delivery, a `chat_surface type`
//	                          self-typed line). It is what `gopi session show
//	                          <id>` prints under the bare word and what the
//	                          live status bar counts up.
//	the person-only subset  — the rows the OPERATOR typed. A true measurement,
//	                          and a DIFFERENT quantity.
//
// `gopi session log <id>` printed NEITHER under the word: its COST block had no
// `turns` line at all (the count lives in internal/tools, which imports this
// package, so the import cannot go back the other way), and the surfaces that
// did print a number printed the SUBSET under `human turns` plus a pointer at
// another command. Measured 2026-10-27 on session
// a60787b0-9081-451f-bd5a-9e396cfc048b: `session show` said `turns 16`,
// `session stats` and `session log` said `human turns 8`, and the ledger
// disagreed with itself to anyone comparing the two screens.
//
// THE SHAPE OF THE FIX (Rule 14: one definition, injected where the import
// cannot reach). internal/tools OWNS the ledger's number — it is a column in the
// chats table — and this package DECLARES the dependency as a function value
// that cmd/agent (which imports both) hands over at startup. It is the same
// mechanism internal/tools/chat_turns.go already uses in the other direction for
// the person-only subset, so the cycle is broken exactly once per direction and
// neither package grows a second scanner over the same data.
//
// AN UNINSTALLED READER IS NOT A FAILURE, IT IS AN UNMEASURED FACT (Rule 15):
// the render then prints the subset under its own word plus the sentence that
// says the total was not measured here and which command prints it. It never
// prints the subset under `turns` — a substituted number is indistinguishable
// from a measured one once it is on the screen, which is the collision this
// whole epic removes.

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/nickhuang99/gopi/internal/sessionevent"
)

// LedgerTurnCount reads a session's LEDGER turn count (`chats.turn_count`).
//
// ok=false means UNMEASURED, and note says why in words a reader can use — a
// library caller with nothing installed, a session the index does not hold (a
// log this machine has but the ledger never indexed), or a ledger read that
// failed. The three are different facts and the note is what keeps them apart
// (Rule 15); a bare 0 would collapse "nobody looked" into "no turns ran".
type LedgerTurnCount func(sessionID string) (count int, ok bool, note string)

// ledgerTurnCount is the injected reader. nil = nothing published.
var ledgerTurnCount LedgerTurnCount

// InstallLedgerTurnCount publishes the process's ledger turn reader. It is
// called by cmd/agent at the top of main() and of the suite's TestMain — the two
// entry points of the binary that owns both the ledger and this renderer.
//
// A nil argument CLEARS the reader (a test restoring the process state); every
// render already states an unmeasured count, so clearing it hides nothing.
func InstallLedgerTurnCount(r LedgerTurnCount) { ledgerTurnCount = r }

// LedgerTurnCountInstalled reports whether a reader is published — the fact a
// test checks instead of inferring it from a rendered string.
func LedgerTurnCountInstalled() bool { return ledgerTurnCount != nil }

// SessionLedgerTurnCount is a session's turn count as the LEDGER recorded it,
// with the two other facts its renderer needs.
//
//	s *Session nil → unmeasured (there is no session id to ask about)
//	no id on the log → unmeasured (a log that recorded no session_start)
//	no reader installed → unmeasured
func SessionLedgerTurnCount(s *Session) (count int, ok bool, note string) {
	if s == nil || s.ID == "" {
		return 0, false, TurnCountNoSessionIDNote
	}
	if ledgerTurnCount == nil {
		return 0, false, TurnCountNoReaderNote
	}
	return ledgerTurnCount(s.ID)
}

// LedgerTurnCountFor is SessionLedgerTurnCount for a caller that holds only a
// session ID: `gopi session replay <id> --turns` has the boundary count from its
// own material and must be able to print the LEDGER's number beside it under its
// own word. It applies the same three unmeasured answers
// (TurnCountNoSessionIDNote has no analogue here — an empty id is
// TurnCountNoReaderNote's sibling and is stated, never returned as a zero).
func LedgerTurnCountFor(sessionID string) (count int, ok bool, note string) {
	if strings.TrimSpace(sessionID) == "" {
		return 0, false, TurnCountNoSessionIDNote
	}
	if ledgerTurnCount == nil {
		return 0, false, TurnCountNoReaderNote
	}
	return ledgerTurnCount(sessionID)
}

// The two ways THIS package can know a count is unmeasured, spelled once
// (Rule 1/13). The third — the ledger has no row, the read failed — comes from
// the injected reader, which is the layer that can tell them apart.
const (
	// TurnCountNoReaderNote: no reader was published in this process.
	TurnCountNoReaderNote = "no ledger reader installed in this process"
	// TurnCountNoSessionIDNote: the log recorded no session_start, so there is
	// no id to ask the ledger about. It is a fact about THIS log, not a
	// configuration gap, and a reader must be able to see which one it is.
	TurnCountNoSessionIDNote = "this session log recorded no session id, so the ledger's count cannot be looked up"
	// TurnCountUnmeasuredPrefix opens the rendered line when the total could not
	// be read; the reason follows it.
	TurnCountUnmeasuredPrefix = "not measured — "
)

// TurnCountHomeSurface names the ONE command whose whole job is to print this
// count for a session (Rule 14: a pointer in prose names a real door).
const TurnCountHomeSurface = sessionTurnsHome

// TurnCountSource is the provenance sentence printed BESIDE a measured count: it
// names the producer, so a reader comparing two screens knows the number came
// from the ledger and not from a scan of this log.
//
// IT NO LONGER CLAIMS TO BE "the count session show prints": since epic
// 2026-11-04 the bare word on that page carries the log's OPENED count, and this
// number rides its own word (`completed`). A provenance sentence that named the
// wrong surface would be a second, quieter version of the defect.
const TurnCountSource = "the LEDGER's `chats.turn_count` — the turns that FINISHED, which the live status bar counts and `" + sessionTurnsHome + "` prints under its own word, beside the log's opened count"

// The two shapes a surface prints the turn count in (Rule 1: the only variables
// are the number and the prose).
const (
	// turnCountMeasuredFmt is the COST block's value column for a measured
	// count: "16  — <unit words>".
	turnCountMeasuredFmt = "%d  — %s"
	// turnCountClauseFmt is the parenthetical a TABLE header carries:
	// "16 turns".
	turnCountClauseFmt = "%d %s"
)

// turnCountValue renders the value of the bare word `turns`: THE LOG'S OPENED
// COUNT.
//
// WHY NOT THE LEDGER'S (epic 2026-11-04, measured on session
// 3a93ac1c-c0a3-448d-a14f-9eb673b64f9f): `gopi session list` printed `turns 29`
// while `session replay --turns` printed 30 rows and the replay screen ended on
// `──── turn 30`. The ledger advances only when a turn COMPLETES; the log records
// a seam when one OPENS, and that session's thirtieth turn opened and quit. The
// completed count can also sit AHEAD (e364e357: ledger 209, log 34), so an
// ordinal (`--turn N`, a band number, a table row) cannot be anchored on it —
// ordinals are POSITIONS, and only the position count carries them. Every replay
// surface already numbers along this axis; the tables and the index row now do
// too.
//
// IT IS NEVER "UNMEASURED": this function holds the parsed session, so the count
// is a property of its iteration slice. The neighbour that CAN be unmeasured is
// the ledger's, and it prints under its own word (turnCompletedValue).
func turnCountValue(s *Session) string {
	return fmt.Sprintf(turnCountMeasuredFmt, SessionTurns(s), turnUnitWords)
}

// turnCompletedValue renders the LEDGER's `chats.turn_count` — the turns that
// FINISHED — under its own word, or the sentence saying why it was not read.
//
// IT PRINTS ONLY WHEN IT DIFFERS from the opened count: on a session that closed
// every turn it opened the two are equal, and a second identical number under a
// second word is noise a reader learns to skip (which is how the line that
// mattered gets skipped too). Rule 15 in both directions.
func turnCompletedValue(s *Session) string {
	n, ok, note := SessionLedgerTurnCount(s)
	if !ok {
		// AN UNMEASURED LEDGER PRINTS NO COMPLETED LINE, and the REASON is not
		// dropped — it is this function's note, returned to callers that have a
		// row for it (`session log --turns`' banner, `stats`). A blank line where a
		// number would go is the same silence as a fabricated 0 (Rule 15).
		_ = note
		return ""
	}
	if n == SessionTurns(s) {
		return ""
	}
	// THE VALUE IS THE NUMBER ALONE: its `IsNot` sentence is printed by the
	// caller on the line under it, the same shape the turn count uses — folding
	// it in here printed that sentence twice (measured in this very change's
	// first `session log --turns` run).
	return strconv.Itoa(n)
}

// HumanTurnsUnknownWord is how an unmeasured subset prints where its reason has
// no room — inside a clause or a banner, beside a count of a DIFFERENT quantity.
// It is a named constant because "unknown" is a rendered word, not prose
// (Rule 1), and because a second spelling ("n/a", "?") would be a second claim
// about the same gap.
const HumanTurnsUnknownWord = "unknown"

// humanTurnsValue renders the COST block's value for the PERSON-ONLY SUBSET.
//
// IT IS NOT SessionHumanTurns ANY MORE, AND THAT IS THE FIX (measured
// 2026-10-29 on session 794cbd02-4722-43c3-924c-f7ba6f36534d): the block printed
//
//	Iterations: 85   (the `request`-row count)
//	turns: not measured — the chat index holds no row for this session …
//	human turns: 85  (a DERIVED turn count)
//	Passes:     85   (session_end total_iterations)
//
// Three of the four quantities equal because two of them were not measurements.
// The 85 on the `human turns` line did not come from an operator row — that log
// holds none (94 `user` rows, 91 of them `[system]` ticks and 3
// `[WRITE_TASK_AREA …]` state rows): it came from SessionTurnBounds' fallback,
// which hands a session WITH CONTENT but NO recorded seam one turn at index 0
// (the right answer for a producer that "gave it one pass"), and the reader's
// no-tick fallback then marked every content round a seam. So the derived count
// was `len(Iterations)` — another counter's number under a word that owns a
// different quantity, which is the whole defect this package's unit work
// removes (Rule 15: unknown is stated, never borrowed).
//
// The criterion is SessionHumanTurnsUnmeasured's, declared once (Rule 13/14);
// a MEASURED subset renders exactly as it always did.
func humanTurnsValue(s *Session) string {
	if note, unmeasured := SessionHumanTurnsUnmeasured(s); unmeasured {
		return TurnCountUnmeasuredPrefix + note
	}
	return fmt.Sprintf(turnCountMeasuredFmt, SessionHumanTurns(s), humanTurnUnitWords)
}

// turnCountClause renders the turn count as a PHRASE, for the places whose
// sentence already carries the parentheses (the two tables' headers).
//
// Measured: "16 turns". Unmeasured: "turns not measured here; 8 human turns" —
// the subset keeps its own word and never borrows the total's (Rule 14).
func turnCountClause(s *Session) string {
	return fmt.Sprintf(turnCountClauseFmt, SessionTurns(s), sessionevent.CounterUnitTurn)
}
