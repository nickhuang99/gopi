package tools

// chat_ledger_turn_count.go — THE LEDGER'S TURN COUNT, READ BY THE PACKAGE THAT
// OWNS THE COLUMN.
//
// `chats.turn_count` is the ONE turn count on the session surfaces: the turns
// the session RAN, whatever face opened each one (the person's line, a `gopi
// notify` delivery, a `chat_surface action=type` self-typed line). The operator's
// ruling of 2026-10-27 is that this is what the bare word `turns` means — and
// that the person-only subset is a DETAIL under its own word, not a second total.
//
// WHY THIS FUNCTION EXISTS HERE rather than in internal/gopilog, which renders
// most of the session surfaces: gopilog cannot import this package (the cycle
// runs tools → gopilog → agent → tools; see chat_turns.go). The dependency is
// therefore DECLARED by the renderer and INJECTED by cmd/agent — this function is
// the implementation the binary hands over (gopilog.InstallLedgerTurnCount), so
// `gopi session log <id>`'s COST block prints the SAME number `gopi session show
// <id>` prints, from the same column, with no second scanner over the log.
//
// THE THREE OUTCOMES ARE THREE DIFFERENT FACTS (Rule 15), never one zero:
//
//	measured      — the row exists and holds a count (0 included: a session that
//	                ran no turn really did run none);
//	no row        — this machine holds a session log the index never recorded.
//	                Stated with the verb that fixes it (gopi chat backfill);
//	failed        — the ledger could not be read. Stated with the error.

import (
	"context"
	"database/sql"
	"errors"
)

// ChatLedgerTurnCountQuery reads one session's recorded turn count. It is a
// named constant so the column it reads is visible where the reader is
// explained, and so a test can name the same statement (Rule 1/13).
const ChatLedgerTurnCountQuery = `SELECT turn_count FROM chats WHERE session_id = ?`

// The three unmeasured sentences this reader can produce, spelled once
// (Rule 1/13): two failures and one absence, each a DIFFERENT fact.
const (
	// ChatLedgerOpenFailedPrefix: the index itself could not be opened. A read
	// that cannot even start is NOT "no row" — the reader would otherwise be
	// told the session was never indexed when nobody could look.
	ChatLedgerOpenFailedPrefix = "the chat index could not be opened: "
	// ChatLedgerReadFailedPrefix: the index opened but the query failed.
	ChatLedgerReadFailedPrefix = "the chat index could not be read: "
	// ChatLedgerNoRowNote: the index has no row for this session. It names the
	// verb that fixes it, so the sentence is actionable rather than merely true.
	ChatLedgerNoRowNote = "the chat index holds no row for this session, so no turn count was recorded for it (index the logs with `gopi chat backfill`)"
)

// ChatLedgerTurnCount reads the turn count the LEDGER recorded for a session —
// `chats.turn_count`, the number `gopi session show <id>` prints under `turns`.
//
// ok=false means UNMEASURED and note says which of the two reasons applies; the
// caller renders that sentence rather than a number. A measured zero comes back
// ok=true, because "the session ran no turn" is a fact and not a gap.
func ChatLedgerTurnCount(sessionID string) (count int, ok bool, note string) {
	ctx := context.Background()
	db, err := openChatDB(ctx)
	if err != nil {
		return 0, false, ChatLedgerOpenFailedPrefix + err.Error()
	}
	defer db.Close() //stylegate:ignore process-lifetime handle closed at shutdown; the OS reclaims the descriptor
	var n int
	err = db.QueryRowContext(ctx, ChatLedgerTurnCountQuery, sessionID).Scan(&n)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, false, ChatLedgerNoRowNote
	}
	if err != nil {
		return 0, false, ChatLedgerReadFailedPrefix + err.Error()
	}
	return n, true, ""
}
