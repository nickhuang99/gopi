package tools

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	"github.com/nickhuang99/gopi/internal/scratch"
)

// session_gc_ledger.go — the task-ledger half of `gopi session gc`'s
// UNREFERENCED predicate.
//
// WHY THE READER LIVES HERE AND NOT IN internal/scratch: the reclaim predicate
// is scratch's (one liveness probe, one staleness rule), but the LEDGER is
// this package's — internal/tools imports internal/scratch, so the reverse
// import would be a cycle. scratch therefore declares a SEAM
// (scratch.RegisterSessionsLedgerNames) and this file provides the ONE
// implementation, wired from cmd/agent at startup exactly as internal/config
// wires the declared-tree resolvers.
//
// WHAT A ROW NAMES, and why EVERY naming column of EVERY table is read.
//
// The first draft of this file consulted `tasks` alone, and the measured dry run
// on this host's real tree caught the defect it caused: of the 112 directories
// the predicate called collectable, 2 are named by a `chats` row, 1 by a
// `session_conclusions` row and 4 by a `chat_notes` row. A chat session
// directory IS what `gopi session list --mode chat` resolves through, what
// `gopi session replay` plays back and what `gopi session show` prints — so
// "no TASK row names it" was never the right question. The right question is
// "does the ledger, in ANY of its naming columns, still point at this
// directory", and the four sources are:
//
//	tasks.task_id                 → sessions/<task_id>            (the container)
//	tasks.session_id              → sessions/<id>                 (a run's log)
//	chats.session_id              → sessions/<id>                 (a conversation)
//	session_conclusions.session_id→ sessions/<id>                 (a run's summary)
//	session_conclusions.task_id   → sessions/<task_id>            (its container)
//	chat_notes.session_id         → sessions/<id>                 (its round notes)
//
// The set is deliberately READ FROM the schema rather than assumed: a table this
// reader does not know about is a table whose rows it cannot see, so every
// naming column is enumerated in the query list below and a table that is ABSENT
// (an older ledger era) contributes no names and is not an error — while a table
// that is PRESENT and fails to read is an error that keeps everything.
//
// EVERY STATUS COUNTS, deliberately. The keep set is not "live rows": a
// COMPLETED row is exactly the row a human still reads (`gopi task show`,
// `gopi session log`, the report a review cites), and this verb's whole safety
// argument is that a directory an operator can still reach is never collected.
// The row's status is carried into the reason string so the report says WHAT
// kind of row protects a directory.
//
// A LEDGER THAT IS NOT THERE IS AN ERROR, never an empty set — the same call
// TaskStrandedRows makes, for the same reason: an empty set means "no row
// names anything", which would authorise collecting EVERY aged directory. A
// caller that cannot read the ledger must keep everything and say so (Rule 15).
func sessionGCLedgerNames() (map[string]string, error) {
	path := memoryDBPath()
	if !fileExists(path) {
		return nil, fmt.Errorf("ledger not found: %s", path)
	}
	ctx := context.Background()
	db, err := openTasksDBReadOnly(ctx, path)
	if err != nil {
		return nil, fmt.Errorf("open ledger: %w", err)
	}
	defer db.Close() //stylegate:ignore process-lifetime handle closed at shutdown; the OS reclaims the descriptor

	present, err := sessionGCLedgerTables(ctx, db)
	if err != nil {
		return nil, err
	}
	names := make(map[string]string, sessionGCLedgerNameHint)
	for _, src := range sessionGCLedgerNameSources {
		if !present[src.table] {
			// A table this ledger era does not have names nothing. Not an error:
			// the schema legitimately grew, and a reader that refused an older
			// ledger would make the verb unrunnable on any host that has not run
			// the newer code yet.
			continue
		}
		if err := sessionGCLedgerCollect(ctx, db, src, names); err != nil {
			return nil, err
		}
	}
	// A LEDGER WHOSE NAMING TABLES ARE ALL EMPTY is a readable ledger with
	// nothing claimed — a real answer, distinguishable from the unreadable case
	// above precisely because it arrived through a successful query. It returns
	// an empty (non-nil) map so the caller can tell the two apart.
	return names, nil
}

// sessionGCLedgerTables lists the tables this DSN really holds, so an absent one
// is told apart from an unreadable one.
func sessionGCLedgerTables(ctx context.Context, db *sql.DB) (map[string]bool, error) {
	rows, err := db.QueryContext(ctx, `SELECT name FROM sqlite_master WHERE type='table'`)
	if err != nil {
		return nil, fmt.Errorf("list tables: %w", err)
	}
	defer rows.Close()
	present := map[string]bool{}
	for rows.Next() {
		var n string
		if err := rows.Scan(&n); err != nil {
			return nil, fmt.Errorf("scan table name: %w", err)
		}
		present[n] = true
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("table names: %w", err)
	}
	return present, nil
}

// sessionGCLedgerCollect runs ONE source's query and adds its claimed names.
func sessionGCLedgerCollect(ctx context.Context, db *sql.DB, src sessionGCLedgerNameSource, names map[string]string) error {
	rows, err := db.QueryContext(ctx, src.query)
	if err != nil {
		return fmt.Errorf("read %s: %w", src.table, err)
	}
	defer rows.Close()
	for rows.Next() {
		var name, desc string
		if err := rows.Scan(&name, &desc); err != nil {
			return fmt.Errorf("scan %s: %w", src.table, err)
		}
		name = strings.TrimSpace(name)
		if name == "" {
			continue // a row that names no directory cannot protect one
		}
		// First writer wins: the description only has to say WHICH kind of row
		// holds the directory, and the task sources are queried first because
		// their sentence is the one an operator recognises.
		if _, seen := names[name]; !seen {
			names[name] = desc
		}
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("rows %s: %w", src.table, err)
	}
	return nil
}

// sessionGCLedgerNameSource is ONE query that claims directory names, paired
// with the table it reads (so a missing table is recognised by name).
type sessionGCLedgerNameSource struct {
	table string
	query string
}

// sessionGCLedgerNameSources is the complete set of ledger columns that can name
// a session directory. It is a SLICE, not a set of ad-hoc queries, so adding a
// naming column is one row here rather than a second code path (and a future
// table that names sessions without appearing here is the defect this shape is
// meant to make obvious: the reason string of every keep says which source
// claimed it, so a missing source shows up as a directory collected while a
// verb still lists it).
var sessionGCLedgerNameSources = []sessionGCLedgerNameSource{
	{
		table: "tasks",
		query: `SELECT task_id, 'task ' || task_id || ' is ' || status FROM tasks WHERE TRIM(COALESCE(task_id,'')) <> ''`,
	},
	{
		table: "tasks",
		query: `SELECT session_id, 'task ' || task_id || ' is ' || status FROM tasks WHERE TRIM(COALESCE(session_id,'')) <> ''`,
	},
	{
		table: "chats",
		query: `SELECT session_id, 'a chat row (' || mode || ') records it' FROM chats WHERE TRIM(COALESCE(session_id,'')) <> ''`,
	},
	{
		table: "session_conclusions",
		query: `SELECT session_id, 'a round conclusion records it' FROM session_conclusions WHERE TRIM(COALESCE(session_id,'')) <> ''`,
	},
	{
		table: "session_conclusions",
		query: `SELECT task_id, 'a round conclusion records task ' || task_id FROM session_conclusions WHERE TRIM(COALESCE(task_id,'')) <> ''`,
	},
	{
		table: "chat_notes",
		query: `SELECT session_id, 'a round note records it' FROM chat_notes WHERE TRIM(COALESCE(session_id,'')) <> ''`,
	},
}

const (
	// sessionGCLedgerNameHint pre-sizes the claim map. It is a hint, not a
	// bound: the map grows past it without cost worth avoiding, and this host's
	// ledger measured 5,365 task rows / 272 chats / 6,908 conclusions / 4,488
	// notes on 2026-10-29.
	sessionGCLedgerNameHint = 8192
)

// RegisterSessionGCLedger wires the ONE task-ledger reader into the sessions
// reclaim arm. cmd/agent calls it once at startup; internal/scratch keeps the
// seam so it stays a leaf package (internal/tools imports it, never the
// reverse).
func RegisterSessionGCLedger() {
	scratch.RegisterSessionsLedgerNames(sessionGCLedgerNames)
}
