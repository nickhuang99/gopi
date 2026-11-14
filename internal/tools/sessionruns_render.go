package tools

import (
	"fmt"
	"strconv"

	"github.com/nickhuang99/gopi/internal/procstat"
	"github.com/nickhuang99/gopi/internal/sessionevent"
)

// sessionruns_render.go — THE MERGED LISTING'S ROW RENDERING.
//
// WHY THE RENDERER LIVES HERE. A row's SPECIES decides its line: a chat row is
// the chat index's own line (chatListRowLine — one definition, so `gopi chat
// list`, `gopi session list` and the merged view cannot print one row three
// ways), while a run row has no chat row to borrow from and renders the facts its
// own log carries (pid, liveness verdict, branch). Both arms must be here rather
// than in cmd/agent: chatListRowLine is unexported, and a caller outside this
// package could only reach it by re-implementing it (Rule 13's drift, exactly).
//
// THE FLAG WORDS ARE NOT HERE. The disclosure lines that teach a reader how to
// widen a view name flags (`--kind`, `--mode`, `--limit`), and a flag's spelling
// belongs to the CLI layer that parses it (cmd/agent's session*Flag constants) —
// so this file renders ROWS and cmd/agent renders the header and its
// disclosures. A flag word re-typed here would be a second definition that could
// drift from the parser.

// sessionSourceRowFormat renders ONE run row (Rule 1/13: one spelling, the
// facts are the only variables). The id comes first, like a chat row's, because
// it is the operand every sibling verb takes.
//
// THE TURN CLAUSE IS LAST ON PURPOSE. A chat row prints `turns N` inside its own
// parenthesised facts; a run row has no turn count to print, and the defect this
// page carried until 2026-11-04 was that the absence was INVISIBLE — one session
// id came back as two rows, one carrying a turn number and one carrying nothing,
// with no line saying why (epic: the ambiguity, not the row count). So the run
// row states its provenance in the slot where a chat row prints the number:
// `turns none (no chats row)`.
const sessionSourceRowFormat = "%s  [%s]  %s  %s  (pid %s, %s, %s, started %s, %s)\n"

// sessionSourceAliveNotRecorded is the liveness word for a row that never
// stamped a process — every chat row, and a run written before the stamp
// existed. It is a WORD rather than an empty column so a reader can tell "no
// process was recorded" from "the process was recorded and could not be probed"
// (procstat.VerdictUnknown): two different facts with two different follow-ups.
const sessionSourceAliveNotRecorded = "not-recorded"

// sessionSourceAliveWord renders a row's liveness as the one word its column
// carries: procstat's own verdict vocabulary (Rule 14: the leaf owns the words),
// or the not-recorded word when nothing was stamped.
func sessionSourceAliveWord(s SessionSource) string {
	if s.PID <= 0 {
		return sessionSourceAliveNotRecorded
	}
	if s.Alive == "" {
		// A stamp exists but nothing probed it: that is procstat's third
		// verdict, not a blank (Rule 15 — the gap must be visible).
		return string(procstat.VerdictUnknown)
	}
	return string(s.Alive)
}

// SessionSourcePIDWord is the pid column's value: the stamp, or the shared em
// dash for a row that stamped none.
func SessionSourcePIDWord(s SessionSource) string {
	if s.PID <= 0 {
		return orDash("")
	}
	return strconv.Itoa(s.PID)
}

// sessionSourceBranchWord is the branch column's value: the branch git reports
// for the run's directory, or the em dash when there is none to report (a
// detached HEAD, a swept ground, a row that recorded no workdir).
func sessionSourceBranchWord(s SessionSource) string {
	return orDash(s.GitBranch)
}

// THE RUN ROW'S TURN SLOT — the whole point of this epic, spelled once.
//
// One session id is listed TWICE (a chats row and the run log the sessions tree
// yields); the chat row prints the turn count, and the run log has none. The
// defect was that the absence was silent: a reader saw two rows for one id and
// could not tell which of them the number belonged to, and a `--json` consumer
// summing the column counted a 0 that meant "nothing measured" as a turn count
// (epic, 2026-11-04 — the AMBIGUITY is the defect, not the row count; the PA
// ruling keeps both rows).
//
// So the run row states it, in the same slot a chat row prints the number:
// `turns none (no chats row)`. A word rather than a blank keeps the column
// scannable and makes the row self-describing without a legend, and "none" is
// NOT "0": a measured zero is a claim about a conversation (it ran no turns),
// and a run log makes no such claim at all (Rule 15 — the absence is stated,
// and it is stated as an absence).
//
// The unit comes from sessionevent.CounterUnitTurn (Rule 14: the leaf owns the
// word both faces print), so this clause cannot spell `turns` differently from
// the chat row beside it.
const (
	// SessionSourceRunTurnsWord is the value a run row prints where a chat row
	// prints the turn count.
	SessionSourceRunTurnsWord = "none"
	// SessionSourceRunTurnsReason names WHY there is no count, on the row: the
	// number lives on the chats table (`chats.turn_count`) and a run log has no
	// such row behind it. Exported so the JSON face and the tests read the same
	// sentence (Rule 1/13/14).
	SessionSourceRunTurnsReason = "no chats row"
	// SessionSourceRunTurnsClause is the composed clause. It is a constant
	// rather than a literal at the call site so the table and any future face
	// print one spelling.
	SessionSourceRunTurnsClause = sessionevent.CounterUnitTurn + " " + SessionSourceRunTurnsWord +
		" (" + SessionSourceRunTurnsReason + ")"
)

// SessionSourceRowLine renders ONE merged row in the row's own species' shape:
// a chat row through the chat index's renderer, a run row through its own. It is
// the ONE place the choice is made, so the merged table cannot grow a second
// opinion about what a row looks like.
func SessionSourceRowLine(s SessionSource) string {
	if s.Kind == SessionKindChat {
		if d, ok := s.ChatDetail(); ok {
			return chatListRowLine(d)
		}
	}
	return fmt.Sprintf(sessionSourceRowFormat,
		s.SessionID, s.Kind, s.State, s.Head,
		SessionSourcePIDWord(s), sessionSourceAliveWord(s), sessionSourceBranchWord(s),
		formatChatTime(s.Started), SessionSourceRunTurnsClause)
}

// SessionSourceAliveWord is sessionSourceAliveWord for callers outside this
// package (the `session show` page prints the same word for the same fact — one
// vocabulary, two surfaces).
func SessionSourceAliveWord(s SessionSource) string { return sessionSourceAliveWord(s) }

// SessionRunAliveWord is sessionSourceAliveWord applied to a bare stamp, so a
// caller that holds a pid + verdict pair (the show page) renders the same words
// without building a whole SessionSource for them.
func SessionRunAliveWord(pid int, verdict procstat.Verdict) string {
	return sessionSourceAliveWord(SessionSource{PID: pid, Alive: verdict})
}
