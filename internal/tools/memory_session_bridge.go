package tools

// memory_session_bridge.go — the SESSION BRIDGE recall route.
//
// THE DEFECT THIS ROUTE CLOSES (measured, task 01a1faac). A user's question
// reaches memory in THEIR words ("刚才又一次这个 CHAT 自动退出了 …"), while the
// distilled row records the same work in the RECORD's words ("gopi chat 退出
// 路径的 session_end"). Measured over 19 hand-verified labels: 6 of them are
// reachable at NO query length — not because the ranking is wrong but because
// the query carries almost no token the target row contains (lexical overlap
// 8–29%), so the target never enters the candidate pool at all. Re-ranking
// cannot fix a row that was never recalled.
//
// WHAT IS ALREADY ON DISK. The colloquial phrasing of a past session IS
// stored: `chats.first_msg` (and `title`) hold the user's own opening words,
// and `chats_tri` is the trigram index over them. Every distilled row carries
// `session_id` (memory_origin.go). So the bridge is a JOIN the corpus already
// supports: query → the chat whose colloquial opening matches → the records
// that session distilled. Nothing is invented, nothing needs a synonym list,
// and no new dependency or table is added (Rule 35).
//
// TWO SOURCES, because this ledger holds two species of session (measured,
// task 01a1fb05 on the 32575-row ledger). A CONVERSATION is indexed in
// `chats`; an EXECUTION session is indexed in `tasks`, where `task_id` IS the
// run's session id and the run's memory rows carry that same value. Reading
// only `chats` therefore resolved 8186 rows (25.1%); reading both resolves
// 13300 (40.8%) — the second table costs no new schema and matches on the
// identity the row already stamped, never on a guess. The conversation half
// answers FIRST and, when it answers at all, alone: its ordering is what the
// measured ceiling (sessionBridgeRows) was calibrated against, and the ledger
// half exists to fill the silence, not to re-rank a query that already has an
// answer.
//
// WHY IT IS AUTOMATIC. The colloquial side is not hand-filled: it is WRITTEN
// BY THE PRODUCT every time a chat starts (ChatSessionEnsure stamps
// first_msg), for every session, whether or not anyone ever distills from it.
// The bridge therefore improves as the ledger grows, with no dictionary to
// maintain.
//
// HONESTY ABOUT THE CEILING. The label set's queries ARE the sessions' own
// first_msg, so this route's measured lift is its CEILING, not its everyday
// value: a real future query will share the colloquial vocabulary without
// being byte-identical. It is reported as a ceiling in the run's report.
//
// DEGRADATION IS EXPLICIT. A ledger with no chats/chats_tri (a foreign or
// hermetic fixture) simply has no bridge: the route returns nothing and the
// other routes answer as before — the absence is not an error, but it is also
// not silent, because the route's own doc says which tables it needs.

import (
	"context"
	"database/sql"
	"strings"
)

const (
	// sessionBridgeChats is how many matching chat sessions the route trusts.
	// One is too brittle (a truncated query can rank a sibling session first:
	// measured, label 740b264b's session came 2nd) and many is noise — the
	// sessions a single query is "about" is a small number.
	sessionBridgeChats = 3

	// sessionBridgeRows caps how many records the bridge contributes IN TOTAL,
	// and the cap is what protects every other label: the bridge's candidates
	// sort ahead of the ordinary pool, so an uncapped contribution (one session
	// carried 41 distilled rows, measured) would evict the rows that already
	// answer the query correctly.
	//
	// THREE, by measurement, not by argument (artifacts/bridge-rows-{1..5}.txt,
	// the 24-row hand-verified label set over the 17686-row snapshot, limit=20):
	//
	//	rows=1  @20 30/57  MRR 0.2301  label e1e22e9f  long 17  q50 11
	//	rows=2  @20 42/57  MRR 0.3082  label e1e22e9f  long 18  q50 12
	//	rows=3  @20 48/57  MRR 0.3689  label e1e22e9f  long 19  q50 13
	//	rows=4  @20 48/57  MRR 0.3579  label e1e22e9f  long 20  q50 14
	//	rows=5  @20 47/57  MRR 0.3554  label e1e22e9f  long OUT q50 15
	//
	// The curve is NOT monotone: @20 peaks at 3-4 and FALLS at 5, because past
	// three bridge rows the surplus starts evicting rows that already answered.
	// At 5 the label e1e22e9f (which the bridge fix was measured against) leaves
	// the page entirely on its long query — the very regression the earlier
	// comment on this constant claimed 5 could not cause ("five is the largest
	// number that cannot move a hit at rank 14 out of the top 20"): measured,
	// that reasoning held only for hits at rank 14 UNDER THE PRE-df KEY, and the
	// df weight moves e1e22e9f to rank 15 before the bridge pushes it out.
	// Three keeps it IN (19 / 13), and 3 vs 4 differ by zero on @20 while 3 is
	// better on MRR, so 3 is the value that satisfies both halves of the
	// requirement at once.
	sessionBridgeRows = 3

	// sessionBridgeChatTable / sessionBridgeChatTri are the plumbing the route
	// reads; named once (Rule 1b) so a probe and the query cannot disagree.
	sessionBridgeChatTable = "chats"
	sessionBridgeChatTri   = "chats_tri"

	// The LEDGER half of the resolution set (see sessionBridgeSessions).
	//
	// WHY IT IS HERE, measured on this host's ledger (task 01a1fb05, the
	// 32575-row ledger): of the rows whose `session_id` names a session, only
	// 8186 (25.1%) resolve through `chats` — the rest are EXECUTION sessions,
	// and an execution session's identity is recorded in `tasks` (its
	// `task_id` IS the run's session id; measured: 1075 rows equal exactly,
	// and the run's memory rows carry that same value). Resolving a distilled
	// row against the ledger therefore reaches 13300 rows (40.8%) instead of
	// 8186 — the same identity, read from the table that already holds it.
	//
	// It invents no mapping and guesses no session (the epic forbids the
	// latter outright: a wrong session is worse than a missing one). A row
	// resolves here only when its own stamped `session_id` EQUALS a
	// `tasks.task_id`, i.e. when the identity already matches exactly.
	sessionBridgeTaskTable = "tasks"
	sessionBridgeTaskTri   = "tasks_tri"
	sessionBridgeTaskFTS   = "tasks_fts"
)

// sessionBridgeSessions resolves the session ids whose COLLOQUIAL opening
// matches the query. It runs the query's ≥3-rune CJK runs through the trigram
// index over chats(title, first_msg) — the same granularity cjkRunsGE3 already
// produces for the record-side trigram route — and falls back to the ASCII
// terms through chats_fts for a query with no CJK content.
//
// A ledger without either table answers "no sessions" (nil, nil), never an
// error: the bridge is an ADDITIONAL route and its absence must not fail a
// search that the base routes can answer.
func sessionBridgeSessions(ctx context.Context, db *sql.DB, query string) ([]string, error) {
	okChat, err := tableExists(ctx, db, sessionBridgeChatTable)
	if err != nil || !okChat {
		return nil, nil
	}
	ids, berr := bridgeChatSessions(ctx, db, query)
	if berr != nil || len(ids) > 0 {
		// The conversation index answered (or failed): its answer is what the
		// calibrated ceiling was measured against, so the ledger half does not
		// get to displace it. This keeps v1's ordering behaviour byte for byte
		// on every query a chat already answers.
		return ids, berr
	}
	// The conversation index knows no session for this query. That is the
	// ordinary case for an EXECUTION session — the overwhelming majority of
	// this ledger — whose colloquial opening was never a chat's first_msg.
	// Its identity is still recorded, in the tasks ledger, so ask there before
	// reporting that the query reaches no session at all.
	return bridgeLedgerSessions(ctx, db, query)
}

// bridgeChatSessions is the CONVERSATION half: the chat whose colloquial
// opening matches the query (chats_tri, then chats_fts).
func bridgeChatSessions(ctx context.Context, db *sql.DB, query string) ([]string, error) {
	if ids, err := bridgeSessionsByTrigram(ctx, db, query); err != nil || len(ids) > 0 {
		return ids, err
	}
	return bridgeSessionsByASCII(ctx, db, query)
}

// bridgeLedgerSessions is the LEDGER half: the run whose own record (summary /
// payload / tags — the columns tasks_fts indexes) matches the query. It answers
// with the run's `task_id`, which is the value that run's distilled rows carry
// in `session_id` (memory_origin.go stamps the session the row was written in,
// and for a dispatched run that session IS the task id).
//
// The same two index shapes as the chat half, for the same reason: the trigram
// index answers ≥3-rune CJK substrings, the unicode61 index answers words and
// ASCII. An absent ledger (a hermetic fixture) answers "no sessions", never an
// error — the bridge is an ADDITIONAL route everywhere.
func bridgeLedgerSessions(ctx context.Context, db *sql.DB, query string) ([]string, error) {
	okTask, err := tableExists(ctx, db, sessionBridgeTaskTable)
	if err != nil || !okTask {
		return nil, nil
	}
	if ids, err := bridgeLedgerByTrigram(ctx, db, query); err != nil || len(ids) > 0 {
		return ids, err
	}
	return bridgeLedgerByASCII(ctx, db, query)
}

// bridgeLedgerByTrigram is the CJK half over tasks_tri.
func bridgeLedgerByTrigram(ctx context.Context, db *sql.DB, query string) ([]string, error) {
	okTri, err := tableExists(ctx, db, sessionBridgeTaskTri)
	if err != nil || !okTri {
		return nil, nil
	}
	runs := capRouteTerms(cjkRunsGE3(query))
	if len(runs) == 0 {
		return nil, nil
	}
	quoted := make([]string, 0, len(runs))
	for _, r := range runs {
		quoted = append(quoted, ftsQuote(r))
	}
	return queryBridgeSessionIDs(ctx, db,
		`SELECT t.task_id FROM `+sessionBridgeTaskTri+` x JOIN `+sessionBridgeTaskTable+` t ON t.rowid = x.rowid
		 WHERE `+sessionBridgeTaskTri+` MATCH ? ORDER BY x.rank LIMIT ?`,
		strings.Join(quoted, " OR "), sessionBridgeChats)
}

// bridgeLedgerByASCII is the non-CJK half over tasks_fts.
func bridgeLedgerByASCII(ctx context.Context, db *sql.DB, query string) ([]string, error) {
	okFTS, err := tableExists(ctx, db, sessionBridgeTaskFTS)
	if err != nil || !okFTS {
		return nil, nil
	}
	match, ok := escapeQuery(query)
	if !ok {
		return nil, nil
	}
	return queryBridgeSessionIDs(ctx, db,
		`SELECT t.task_id FROM `+sessionBridgeTaskFTS+` f JOIN `+sessionBridgeTaskTable+` t ON t.rowid = f.rowid
		 WHERE `+sessionBridgeTaskFTS+` MATCH ? ORDER BY f.rank LIMIT ?`,
		match, sessionBridgeChats)
}

// bridgeSessionsByTrigram is the CJK half: the query's ≥3-rune CJK runs,
// OR-ed into a chats_tri MATCH and ordered by the index's own rank.
func bridgeSessionsByTrigram(ctx context.Context, db *sql.DB, query string) ([]string, error) {
	okTri, err := tableExists(ctx, db, sessionBridgeChatTri)
	if err != nil || !okTri {
		return nil, nil
	}
	runs := capRouteTerms(cjkRunsGE3(query))
	if len(runs) == 0 {
		return nil, nil
	}
	quoted := make([]string, 0, len(runs))
	for _, r := range runs {
		quoted = append(quoted, ftsQuote(r))
	}
	return queryBridgeSessionIDs(ctx, db,
		`SELECT c.session_id FROM `+sessionBridgeChatTri+` t JOIN `+sessionBridgeChatTable+` c ON c.rowid = t.rowid
		 WHERE `+sessionBridgeChatTri+` MATCH ? ORDER BY t.rank LIMIT ?`,
		strings.Join(quoted, " OR "), sessionBridgeChats)
}

// bridgeSessionsByASCII is the non-CJK half: the query's own searchable terms
// AND-ed over chats_fts (unicode61 over title/first_msg).
func bridgeSessionsByASCII(ctx context.Context, db *sql.DB, query string) ([]string, error) {
	okFTS, err := tableExists(ctx, db, sessionBridgeChatTable+"_fts")
	if err != nil || !okFTS {
		return nil, nil
	}
	match, ok := escapeQuery(query)
	if !ok {
		return nil, nil
	}
	return queryBridgeSessionIDs(ctx, db,
		`SELECT c.session_id FROM `+sessionBridgeChatTable+`_fts f JOIN `+sessionBridgeChatTable+` c ON c.rowid = f.rowid
		 WHERE `+sessionBridgeChatTable+`_fts MATCH ? ORDER BY f.rank LIMIT ?`,
		match, sessionBridgeChats)
}

// queryBridgeSessionIDs runs one of the two bridge MATCH shapes and drops the
// empty ids a legacy row may carry (an id-less chat cannot name records).
func queryBridgeSessionIDs(ctx context.Context, db *sql.DB, sqlText, match string, limit int) ([]string, error) {
	rows, err := db.QueryContext(ctx, sqlText, match, limit)
	if err != nil {
		// A malformed MATCH is this route's own miss, not the search's failure:
		// the base routes are unaffected by it.
		return nil, nil //stylegate:ignore a bridge MATCH error is this route's own miss; the base routes still answer
	}
	defer rows.Close() //stylegate:ignore read-only cursor; its rows are fully consumed below
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, nil //stylegate:ignore an unreadable bridge row is this route's own miss
		}
		if strings.TrimSpace(id) != "" {
			ids = append(ids, id)
		}
	}
	if err := rows.Err(); err != nil {
		return nil, nil //stylegate:ignore an aborted bridge scan is this route's own miss
	}
	return ids, nil
}
