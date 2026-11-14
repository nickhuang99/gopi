package tools

// chat_notes — the ROUND-LEVEL note index (epic: notes become searchable the
// moment they are recorded).
//
// WHAT THIS IS, and what it deliberately is NOT.
//
// `self-record.md` (internal/agent/selfrecord.go) is the EVIDENCE layer: the
// per-run side-channel file holding every [IMPORTANT] block the model wrote,
// one line per round, written at the instant the round produced it. It is a
// FILE: greppable, but not addressable — a reader can find a note only if he
// already knows which run directory holds it, and nothing can rank two runs
// against one query.
//
// This table is the RETRIEVAL layer over the same notes: one row per recorded
// note, written from the SAME call that appends the file (ONE write point —
// internal/agent/selfrecord_noteindex.go), indexed by the SAME FTS5 pair the
// rest of this repository's indexes use (unicode61 for words, trigram for CJK
// substrings) plus a LIKE floor for fragments below trigram's 3-character
// lower bound.
//
// IT IS NOT a messages-level index, and the boundary is the hermes 4.3GB
// lesson this package's chat index already records: chat *content* lives in
// the JSONL pointer files and the DB stores only index facts. A note is not a
// message dump — it is the model's own one-line summary, capped at
// maxSelfRecordNoteChars (500 runes) by collapseSelfNote before either writer
// sees it, so this table is O(rounds) in bounded-size rows and can never grow
// the way a message mirror does. THIS FILE IS THE SECOND HALF OF THAT SENTENCE:
// the chats table (chat.go) indexes CONVERSATIONS; chat_notes indexes the
// round-level records INSIDE them — the same file, two grains, no third store.
//
// WHY source_host AND source_path ARE BOTH COLUMNS. The two machines of this
// topology have home directories that are literally the same string
// (`/home/user/...`, measured on host-b and peer1), so a PATH alone cannot say
// which machine a note came from and a HOST alone cannot say where to look for
// the rest of the run. Both are stamped, from sources that already exist —
// tools.OriginHostName() (the node-identity override > hostname, Rule 14: one
// definition, shared with the memory provenance column) and the run's own
// resolved session directory — so a reader can tell two same-looking paths
// apart instead of silently attributing a note to the wrong machine.
//
// WRITES ARE BEST-EFFORT (the caller's contract, stated here so a reader of
// either site sees it): a note is a side channel, and the round it belongs to
// is worth more than its index row. A write failure is returned to the caller,
// which logs it and continues — never a fatal error, never a silent one
// (Rule 15b).

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"
)

// ---------------------------------------------------------------------------
// Named constants (Rule 1 / Rule 13: every table name, column name and limit is
// spelled once and referenced).
// ---------------------------------------------------------------------------

const (
	// chatNotesTableName / chatNotesFTSName / chatNotesTriName are the three
	// storage objects this feature owns. The FTS names keep the sibling
	// convention (chats_fts / chats_tri, tasks_fts / tasks_tri), so a reader
	// learns one naming rule for every index in this database.
	chatNotesTableName = "chat_notes"
	chatNotesFTSName   = "chat_notes_fts"
	chatNotesTriName   = "chat_notes_tri"

	// The column names, one definition each. They are spelled ONCE because the
	// DDL, the trigger bodies, the INSERT and the search projection all read
	// them — a column renamed in one place and not another is the defect this
	// prevents.
	chatNoteSessionCol = "session_id"
	chatNoteTurnCol    = "turn_index"
	chatNoteRoundCol   = "round_index"
	chatNoteTextCol    = "note"
	chatNoteHostCol    = "source_host"
	chatNotePathCol    = "source_path"
	chatNoteCreatedCol = "created_at"

	// chatNoteTextDDL is the declared DDL of the TEXT columns — TEXT DEFAULT ''
	// throughout, the house pattern for this database (an ALTER-added column
	// cannot be NOT NULL without a default, and '' reads as "not recorded").
	chatNoteTextDDL = "TEXT DEFAULT ''"

	// chatNoteTurnDDL is turn_index's DDL — INTEGER, never TEXT, for the same
	// reason the round column is: it is a NUMBER a reader sorts and selects by.
	//
	// IT COEXISTS WITH round_index RATHER THAN REPLACING IT, and the reason is
	// append-only evidence at a second layer: the rows written before the
	// turn-stamp epic carry a ROUND (one HTTP request/response) and no turn, and
	// rewriting them would be inventing a number the writer never had. So an old
	// row reads turn_index=0 ("no turn recorded" — the absence value the whole
	// house uses) beside its real round, and a new row reads a turn beside
	// round_index=0. Neither column is ever guessed from the other.
	chatNoteTurnDDL = "INTEGER DEFAULT 0"

	// chatNoteRoundDDL is round_index's DDL — INTEGER, never TEXT. The round is
	// a NUMBER (sessionevent.RoundIndexBase plus the session's own 0-based
	// iteration), so it is stored and ordered as one: as text, "10" would sort
	// before "9".
	//
	// IT IS RETIRED AS A WRITE TARGET (`ChatNote.Round` is history; the writer
	// stamps `Turn`) and stays in the DDL because the rows that carry it are
	// evidence of when they were written.
	chatNoteRoundDDL = "INTEGER DEFAULT 0"

	// chatNoteCreatedDDL is created_at's DDL — a Unix-seconds REAL, the same
	// clock type the chats table's started_at / last_msg_at use.
	chatNoteCreatedDDL = "REAL DEFAULT 0"

	// chatNoteSearchDefaultLimit caps a search that was not given a limit; the
	// CLI passes its own (chatNoteSearchMaxLimit) when asked. A capped result
	// says so in its header — a silent truncation reads as "that is all there
	// is" (Rule 15).
	chatNoteSearchDefaultLimit = 20

	// chatNoteSearchMaxLimit is the largest limit a caller may ask for: the
	// render is a terminal page, not an export.
	chatNoteSearchMaxLimit = 200

	// chatNoteRenderChars caps the note text shown per hit. The stored text is
	// the record (up to maxSelfRecordNoteChars, the model-facing cap); the
	// RENDER is a page, and a 500-rune note five times over is not a list a
	// reader scans. Truncation is marked, never silent.
	chatNoteRenderChars = 160

	// chatNoteRenderEllipsis marks a rendered note that was cut.
	chatNoteRenderEllipsis = "…"

	// chatNoteTurnWord / chatNoteRoundWord are the two units a rendered hit can
	// carry, spelled once (Rule 1/13) because the render, the tests and the
	// operator's own grep all compare against them. `turn` is what a row written
	// from the turn-stamp epic on says; `round` is what the rows before it say.
	chatNoteTurnWord  = "turn"
	chatNoteRoundWord = "round"

	// chatNotePathIndent indents the second line of a hit — the full path of the
	// run the note came from, which is the fact that says WHICH machine's
	// same-looking home directory holds it.
	chatNotePathIndent = "      "
)

// chatNotesTable is the note index itself: one row per recorded note, with the
// provenance a reader needs to walk back to the run.
func chatNotesTable() string {
	return `CREATE TABLE IF NOT EXISTS ` + chatNotesTableName + ` (
	id           INTEGER PRIMARY KEY,
	` + chatNoteSessionCol + ` TEXT NOT NULL DEFAULT '',
	` + chatNoteTurnCol + ` ` + chatNoteTurnDDL + `,
	` + chatNoteRoundCol + ` ` + chatNoteRoundDDL + `,
	` + chatNoteTextCol + ` ` + chatNoteTextDDL + `,
	` + chatNoteHostCol + ` ` + chatNoteTextDDL + `,
	` + chatNotePathCol + ` ` + chatNoteTextDDL + `,
	` + chatNoteCreatedCol + ` ` + chatNoteCreatedDDL + `
)`
}

// chatNotesFTSTable / chatNotesTriTable are the FTS5 companions, mirroring
// chats_fts / chats_tri exactly (external-content, content_rowid='id'): the
// content lives in chat_notes and the indexes hold only the inverted form, so
// the three keep-in-sync triggers below are MANDATORY, not optional.
//
// WHICH COLUMN IS INDEXED, and why the others are UNINDEXED. The SEARCHABLE
// surface is the note text — a provenance column (session, round, host, path)
// is fetched back through the JOIN, never MATCHed, so indexing it would only
// add tokens that can never be a hit. They ride along as UNINDEXED so the
// trigger bodies and the projection read the same column list.
const (
	chatNotesFTSTable = `CREATE VIRTUAL TABLE IF NOT EXISTS ` + chatNotesFTSName + ` USING fts5(
		` + chatNoteSessionCol + ` UNINDEXED, ` + chatNoteRoundCol + ` UNINDEXED, ` +
		chatNoteHostCol + ` UNINDEXED, ` + chatNotePathCol + ` UNINDEXED, ` + chatNoteTextCol + `,
		content='` + chatNotesTableName + `', content_rowid='id', tokenize='unicode61'
	)`
	chatNotesTriTable = `CREATE VIRTUAL TABLE IF NOT EXISTS ` + chatNotesTriName + ` USING fts5(
		` + chatNoteTextCol + `, content='` + chatNotesTableName + `', content_rowid='id', tokenize='trigram'
	)`
)

// chatNotesFTSTriggers keep both indexes in lockstep with every chat_notes
// write. They are the chats_ai/chats_ad/chats_au bodies with this table's
// columns: the AFTER DELETE and AFTER UPDATE arms write the ('delete', …)
// tombstone row the external-content contract requires, without which a
// replaced note would leave its old tokens searchable forever.
var chatNotesFTSTriggers = []string{
	`CREATE TRIGGER IF NOT EXISTS ` + chatNotesTableName + `_ai AFTER INSERT ON ` + chatNotesTableName + ` BEGIN
		INSERT INTO ` + chatNotesFTSName + `(rowid, ` + chatNoteSessionCol + `, ` + chatNoteRoundCol + `, ` +
		chatNoteHostCol + `, ` + chatNotePathCol + `, ` + chatNoteTextCol + `)
			VALUES (new.id, new.` + chatNoteSessionCol + `, new.` + chatNoteRoundCol + `, new.` +
		chatNoteHostCol + `, new.` + chatNotePathCol + `, new.` + chatNoteTextCol + `);
		INSERT INTO ` + chatNotesTriName + `(rowid, ` + chatNoteTextCol + `)
			VALUES (new.id, new.` + chatNoteTextCol + `);
	END`,
	`CREATE TRIGGER IF NOT EXISTS ` + chatNotesTableName + `_ad AFTER DELETE ON ` + chatNotesTableName + ` BEGIN
		INSERT INTO ` + chatNotesFTSName + `(` + chatNotesFTSName + `, rowid, ` + chatNoteSessionCol + `, ` +
		chatNoteRoundCol + `, ` + chatNoteHostCol + `, ` + chatNotePathCol + `, ` + chatNoteTextCol + `)
			VALUES ('delete', old.id, old.` + chatNoteSessionCol + `, old.` + chatNoteRoundCol + `, old.` +
		chatNoteHostCol + `, old.` + chatNotePathCol + `, old.` + chatNoteTextCol + `);
		INSERT INTO ` + chatNotesTriName + `(` + chatNotesTriName + `, rowid, ` + chatNoteTextCol + `)
			VALUES ('delete', old.id, old.` + chatNoteTextCol + `);
	END`,
	`CREATE TRIGGER IF NOT EXISTS ` + chatNotesTableName + `_au AFTER UPDATE ON ` + chatNotesTableName + ` BEGIN
		INSERT INTO ` + chatNotesFTSName + `(` + chatNotesFTSName + `, rowid, ` + chatNoteSessionCol + `, ` +
		chatNoteRoundCol + `, ` + chatNoteHostCol + `, ` + chatNotePathCol + `, ` + chatNoteTextCol + `)
			VALUES ('delete', old.id, old.` + chatNoteSessionCol + `, old.` + chatNoteRoundCol + `, old.` +
		chatNoteHostCol + `, old.` + chatNotePathCol + `, old.` + chatNoteTextCol + `);
		INSERT INTO ` + chatNotesTriName + `(` + chatNotesTriName + `, rowid, ` + chatNoteTextCol + `)
			VALUES ('delete', old.id, old.` + chatNoteTextCol + `);
		INSERT INTO ` + chatNotesFTSName + `(rowid, ` + chatNoteSessionCol + `, ` + chatNoteRoundCol + `, ` +
		chatNoteHostCol + `, ` + chatNotePathCol + `, ` + chatNoteTextCol + `)
			VALUES (new.id, new.` + chatNoteSessionCol + `, new.` + chatNoteRoundCol + `, new.` +
		chatNoteHostCol + `, new.` + chatNotePathCol + `, new.` + chatNoteTextCol + `);
		INSERT INTO ` + chatNotesTriName + `(rowid, ` + chatNoteTextCol + `)
			VALUES (new.id, new.` + chatNoteTextCol + `);
	END`,
}

// chatNotesIndexes: the provenance columns a reader narrows by (which machine,
// which run, which turn). Value-signal from day one — the same rule the chat
// index states, for the same measured reason (an inert column is decoration).
//
// THE SESSION INDEX IS NAMED FOR THE TURN (`idx_chat_notes_session_turn`), not
// renamed in place: a name that already exists makes CREATE INDEX IF NOT EXISTS a
// no-op, so keeping the old name would leave every already-built index keyed on
// round_index and the new column unindexed — while looking migrated. A database
// that predates this change therefore keeps its old index (nothing in this house
// drops an index; it is inert, not wrong) beside the new one.
var chatNotesIndexes = []string{
	`CREATE INDEX IF NOT EXISTS idx_chat_notes_session_turn ON ` + chatNotesTableName + `(` + chatNoteSessionCol + `, ` + chatNoteTurnCol + `)`,
	`CREATE INDEX IF NOT EXISTS idx_chat_notes_host ON ` + chatNotesTableName + `(` + chatNoteHostCol + `)`,
	`CREATE INDEX IF NOT EXISTS idx_chat_notes_created ON ` + chatNotesTableName + `(` + chatNoteCreatedCol + `)`,
}

// chatNotesTurnColumn widens an index built before the turn column existed
// must be widened IN PLACE — CREATE TABLE IF NOT EXISTS does not touch an
// existing table, so without this every already-recorded note row would be
// unaddressable by turn and the new INSERT would fail on the live ledger. It is
// the SAME pattern (and the same swallowed "duplicate column name") the chats
// table's `source` column uses: one migration per added column, applied by the
// one opener every face goes through.
const chatNotesTurnColumn = "ALTER TABLE " + chatNotesTableName + " ADD COLUMN " + chatNoteTurnCol + " " + chatNoteTurnDDL

// ensureChatNotesSchema creates the table, its indexes, the FTS pair and the
// keep-in-sync triggers. Idempotent, exactly like ensureChatSchema — safe on
// every open, and safe under two processes opening the shared database at once
// (CREATE … IF NOT EXISTS is the only statement form used).
func ensureChatNotesSchema(ctx context.Context, db *sql.DB) error {
	// The chats schema first: this is the SAME database file (chatDBPath), and
	// the note index is a second grain over the same conversations rather than a
	// second store — one open, both schemas, no path of its own to drift.
	if err := ensureChatSchema(ctx, db); err != nil {
		return err
	}
	// THE TABLE, THEN THE COLUMN WIDENING, THEN EVERYTHING THAT NAMES THE COLUMN.
	// The order is load-bearing on an already-built index: CREATE TABLE IF NOT
	// EXISTS does not touch an existing table, so the base table on this host
	// predates turn_index, and any statement naming the column before the ALTER
	// runs fails the whole open (Rule 15 — it fails loudly rather than leaving a
	// half-migrated index; this order is what keeps that loud failure from
	// happening at all).
	if _, err := db.ExecContext(ctx, chatNotesTable()); err != nil {
		return fmt.Errorf("chat notes schema: %w", err)
	}
	if _, err := db.ExecContext(ctx, chatNotesTurnColumn); err != nil {
		// "duplicate column name" is the expected no-op on an already-migrated
		// database — swallowed, exactly as tasks.go and chat.go swallow it.
		if !strings.Contains(err.Error(), chatDuplicateColumnErr) {
			return fmt.Errorf("chat notes schema %s: %w", chatNoteTurnCol, err)
		}
	}
	stmts := append([]string{}, chatNotesIndexes...)
	stmts = append(stmts, chatNotesFTSTable, chatNotesTriTable)
	stmts = append(stmts, chatNotesFTSTriggers...)
	for _, s := range stmts {
		if _, err := db.ExecContext(ctx, s); err != nil {
			return fmt.Errorf("chat notes schema: %w", err)
		}
	}
	return nil
}

// openChatNotesDB opens the note index database and ensures its schema. It is
// the ONE opener (Rule 14): the writer and the reader both come through here,
// so a row written by the agent is a row the CLI can see by construction.
//
// It comes through openLedgerIndexDB for the reason openChatDB does: this is
// the SHARED ledger file (chatDBPath), and a face that opens it first must
// leave it a ledger — the union of the faces — not a slice of it.
func openChatNotesDB(ctx context.Context) (*sql.DB, error) {
	return openLedgerIndexDB(ctx, chatDBPath(), ensureChatNotesSchema)
}

// ChatNoteTurnWord / ChatNoteRoundWord expose the two unit words a rendered hit
// can carry (Rule 13: a caller that asserts on the render reads the same
// constant the renderer writes, never a copy of the literal).
func ChatNoteTurnWord() string { return chatNoteTurnWord }

// ChatNoteRoundWord is the retired unit's word, kept exported for the same
// reason: a historical row still renders it.
func ChatNoteRoundWord() string { return chatNoteRoundWord }

// ChatNoteSearchDefaultLimit / ChatNoteSearchMaxLimit expose the two limits to
// the CLI usage page, so the page cannot advertise a number the search does not
// honour (Rule 13: the value is referenced, never mirrored into prose).
func ChatNoteSearchDefaultLimit() int { return chatNoteSearchDefaultLimit }

// ChatNoteSearchMaxLimit is the exported face of chatNoteSearchMaxLimit.
func ChatNoteSearchMaxLimit() int { return chatNoteSearchMaxLimit }

// ChatNote is one recorded note: the TURN it belongs to and the provenance of
// the run that produced it.
//
// The zero values are MEANINGFUL and are preserved as such (Rule 15a): an empty
// Host is filled from OriginHostName() (the machine's own identity is a fact
// this process knows better than its caller), while an empty Path stays empty —
// "this run had no session directory to name" is a real state that must not be
// papered over with a path nobody resolved.
type ChatNote struct {
	SessionID string
	// Turn is the session's own 1-based TURN number — the ordinal of the
	// complete input this note was written in (sessionevent.IsTurnOpeningRow is
	// the predicate; SessionTurnBounds is the numbering), or
	// sessionevent.RoundIndexUnknown when the run could not say which turn it
	// was in. Never a fabricated turn 1.
	//
	// IT IS THE SAME NUMBER THE FILE LINE CARRIES (`t<turn> <note>`), evaluated
	// once by the caller and handed to both writers, and the same number
	// `gopi session log --turns` / `session replay --turn N` give that turn —
	// one axis, three readers.
	Turn int
	Note string
	// Host is the machine identity; empty resolves to OriginHostName() at the
	// write seam.
	Host string
	// Path is the FULL local path of the record file this note was appended to
	// (or the run directory it lives in), so a reader can tell two machines
	// whose home directories are the same string apart.
	Path string
	// CreatedAt is Unix seconds; <= 0 takes the clock at write time.
	CreatedAt float64
}

// ChatNoteRecord writes ONE note into the index. It is called from the same
// agent call that appends the side-channel file, so a returned error means "the
// record is on disk, the index row is not" — the honest partial state, which the
// caller LOGS and the round survives (see the file header).
//
// Validation is deliberately minimal and asymmetric: a note with no session and
// a note with no text are refused (a row that cannot be walked back to a run, or
// that holds nothing, is not an index entry), while the turn may legitimately be
// absent and the path may legitimately be unknown — both are stored as the
// absence they are.
func ChatNoteRecord(n ChatNote) error {
	session := strings.TrimSpace(n.SessionID)
	if session == "" {
		return fmt.Errorf("chat note: refusing to index a note with no session_id (a row that cannot be walked back to a run)")
	}
	text := strings.TrimSpace(n.Note)
	if text == "" {
		return fmt.Errorf("chat note: refusing to index an empty note")
	}
	host := strings.TrimSpace(n.Host)
	if host == "" {
		host = OriginHostName()
	}
	createdAt := n.CreatedAt
	if createdAt <= 0 {
		createdAt = float64(time.Now().UnixNano()) / float64(time.Second)
	}
	ctx := context.Background()
	db, err := openChatNotesDB(ctx)
	if err != nil {
		return fmt.Errorf("chat note: %w", err)
	}
	defer db.Close()
	if _, err := db.ExecContext(ctx,
		`INSERT INTO `+chatNotesTableName+`(`+chatNoteSessionCol+`, `+chatNoteTurnCol+`, `+chatNoteTextCol+`, `+
			chatNoteHostCol+`, `+chatNotePathCol+`, `+chatNoteCreatedCol+`) VALUES (?, ?, ?, ?, ?, ?)`,
		session, n.Turn, text, host, strings.TrimSpace(n.Path), createdAt); err != nil {
		return fmt.Errorf("chat note: insert: %w", err)
	}
	return nil
}

// ChatNoteHit is one row a search returned, joined back from the base table
// (the FTS tables hold only the inverted index, so every rendered fact comes
// from chat_notes itself).
//
// IT IS EXPORTED because there are TWO consumers of one search: the CLI's
// terminal page (renderChatNoteHits below) and the prefetch's second retrieval
// leg, which needs the rows as DATA to render inside a different block
// (cmd/agent/chat_prefetch.go → internal/memoryprefetch.NoteHits). The tier
// ladder, the ranking and the provenance columns stay HERE — one implementation
// of the search, two faces of it (Rule 14), so the two can never disagree about
// what the index holds.
type ChatNoteHit struct {
	// ID is the row's identity in chat_notes — the address a reader hands back
	// to `gopi session notes` or to the note leg of a prefetch line.
	ID        int
	SessionID string
	// Turn is the note's turn ordinal; 0 means the row predates the turn stamp
	// and records no turn (its Round column carries what it does have).
	Turn      int
	Round     int
	Note      string
	Host      string
	Path      string
	CreatedAt float64
}

// ChatNoteHitTier names WHICH tier of the ladder answered, or that none did. It
// is the word renderChatNoteHits prints (`via fts`) and the prefetch records, so
// the two faces of one search report the same fact in the same vocabulary.
const (
	ChatNoteTierFTS     = "fts"
	ChatNoteTierTrigram = "trigram"
	ChatNoteTierLike    = "like"
	// ChatNoteTierNone means every tier ran and none matched. It is a VALUE, not
	// an empty string: "the index answered with nothing" and "no search ran" are
	// different facts, and a caller that has to record one must be able to.
	ChatNoteTierNone = ""

	// ChatNoteTierLadder is the LAST tier, and it exists because of a measured
	// gap between the two callers of this search.
	//
	// The three tiers above match the query as a PHRASE (ftsQuote wraps it in
	// quotes), which is the right shape for a human who types a few words into
	// `gopi session notes`. A PREFETCH query is not that shape: it is a whole
	// conversation line, e.g. "auto memory important note response 捕获 对象 改为",
	// and no note contains that sentence. Measured on the operator's own index
	// (2026-12-08): a real chat turn's note leg came back `empty` — 0 rows, in
	// 12ms — for a query the memory ledger answered from its FIRST hit, because
	// the ledger's ladder ANDs the terms while this search demanded the exact
	// phrase.
	//
	// So the ladder is reused here as an ADDITIONAL tier, never as a replacement:
	// the phrase tiers still win when they answer (a phrase hit is the most
	// precise evidence), and only a search that would otherwise report nothing
	// descends into the term-subset shape. internal/tools' own ladder contract
	// says exactly this — "the ladder is an addition to recall, never a
	// replacement" — and there is ONE implementation of it (memory_ladder.go),
	// shared by the memory search and this one (Rule 14, Rule 8).
	ChatNoteTierLadder = "ladder"
)

// chatNoteSearchSQL builds the three tiers' statements in ONE place. It is used
// by both faces of the search, so a tier cannot be spelled one way for the
// terminal and another for the prefetch.
func chatNoteSearchTiers(phrase, likePattern string) []struct {
	name string
	sql  string
	args []any
} {
	projection := `SELECT n.id, n.` + chatNoteSessionCol + `, n.` + chatNoteTurnCol + `, n.` + chatNoteRoundCol + `, n.` + chatNoteTextCol + `, n.` +
		chatNoteHostCol + `, n.` + chatNotePathCol + `, n.` + chatNoteCreatedCol + `
	         FROM ` + chatNotesTableName + ` n`
	order := ` ORDER BY n.` + chatNoteCreatedCol + ` DESC, n.id DESC`
	return []struct {
		name string
		sql  string
		args []any
	}{
		{ChatNoteTierFTS, projection + ` JOIN ` + chatNotesFTSName + ` f ON n.id = f.rowid
		         WHERE ` + chatNotesFTSName + ` MATCH ? ORDER BY bm25(` + chatNotesFTSName + `), n.` + chatNoteCreatedCol + ` DESC`, []any{phrase}},
		{ChatNoteTierTrigram, projection + ` JOIN ` + chatNotesTriName + ` f ON n.id = f.rowid
		         WHERE ` + chatNotesTriName + ` MATCH ? ORDER BY bm25(` + chatNotesTriName + `), n.` + chatNoteCreatedCol + ` DESC`, []any{phrase}},
		{ChatNoteTierLike, projection + ` WHERE n.` + chatNoteTextCol + ` LIKE ? ESCAPE '\'` + order, []any{likePattern}},
	}
}

// ChatNoteIndexPath resolves the note index's database file — the SAME path the
// CLI's search opens (chatDBPath, Rule 14: one resolution, two callers). It is
// exported because the chat executor's prefetch opens the index ITSELF: it has a
// read-only contract and its own (much shorter) busy timeout, so it dials
// through tools.LedgerReadOnlyDSNIn rather than through the CLI's opener.
func ChatNoteIndexPath() string { return chatDBPath() }

// ErrChatNoteIndexAbsent reports that the note index does not exist on this
// machine — the ledger file is missing, or it carries no chat_notes table yet.
// It is a TYPED absence rather than an empty result (Rule 15): a caller that
// must record WHY it found nothing can tell "no notes matched the query" from
// "there is no note index here", and the two are different facts.
var ErrChatNoteIndexAbsent = errors.New("chat note: the note index does not exist on this machine")

// ErrChatNoteUnusableHandle reports that a caller reached this package's
// CALLER-OWNED-handle face with a handle that cannot run a query — a nil
// *sql.DB — instead of with a missing index.
//
// IT EXISTS BECAUSE THE ALTERNATIVE WAS A SILENT SIGSEGV (Rule 15). The first
// statement of the search dereferences the handle inside database/sql
// (`(*DB).conn`), so a nil one took the WHOLE PROCESS down — no error, no
// caller name, no test name: just a stack trace whose top frames are the
// standard library's, which is exactly the shape a caller's lifecycle defect
// takes when nobody checks for it. The two facts are different and send a
// reader in opposite directions: ErrChatNoteIndexAbsent means "there is nothing
// to read here" (a state of the machine), this one means "the handle you handed
// me was never usable" (a defect in the caller, whose handle was closed or
// never dialled).
var ErrChatNoteUnusableHandle = errors.New("chat note: no usable database handle was handed to the note index search")

// chatNoteHandleUsable reports whether a caller-owned handle can run a query at
// all. It is asked at BOTH entry points that can reach the tier runner — the
// exported face (a caller's own handle) and the tier runner itself (the CLI's
// walk, which no exported entry point has vetted) — so every path to the
// dereference is guarded by the same predicate (Rule 14: one predicate, asked
// where the pointer is actually used).
func chatNoteHandleUsable(db *sql.DB) bool { return db != nil }

// ChatNoteIndexMissing reports whether an error means "the index is not there",
// which is an ABSENCE rather than a failure — the sibling of
// cmd/agent/chat_prefetch.go's isEmptyLedger for the memory ledger. It matches
// SQLite's own words for a missing table/view and nothing else: a permission
// error or a corrupt file stays a FAILURE, or a broken index would read as an
// empty one (Rule 15).
func ChatNoteIndexMissing(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return strings.Contains(msg, "no such table") || strings.Contains(msg, chatNotesTableName)
}

// ChatNoteSearchOn runs the tier ladder against a CALLER-OWNED handle.
//
// IT IS THE FACE THE PREFETCH'S SECOND LEG USES, and the handle is the caller's
// because the two callers have different contracts: the CLI opens through
// openChatNotesDB (which ENSURES the schema — a CREATE on a cold file, right for
// a verb and wrong for a keystroke), while the prefetch must be provably
// read-only and carries its own sub-second busy timeout. Everything AFTER the
// open is identical, which is the whole point: one implementation of the ladder,
// so the CLI's page and the injected leg can never rank differently (Rule 14).
//
// A ctx deadline applies to the query, so a slow index costs the caller its
// result and never its budget.
func ChatNoteSearchOn(ctx context.Context, db *sql.DB, query string, limit int) ([]ChatNoteHit, string, error) {
	query = strings.TrimSpace(query)
	if query == "" {
		return nil, ChatNoteTierNone, fmt.Errorf("chat note: search query must not be empty")
	}
	if !chatNoteHandleUsable(db) {
		// THE HANDLE IS CHECKED BEFORE THE FIRST QUERY, and it is checked by
		// NAME. Reaching the tier walk with a nil handle used to be a SIGSEGV
		// inside database/sql; a caller whose lifecycle handed over a closed or
		// never dialled handle now gets a sentence it can act on, and the process
		// survives to record it (see ErrChatNoteUnusableHandle). The check sits
		// AFTER the query validation on purpose: the pre-existing contract is
		// that an empty query is reported as such, and the argument a caller
		// passed is a better thing to hear about than the state of its handle.
		return nil, ChatNoteTierNone, ErrChatNoteUnusableHandle
	}
	if limit <= 0 {
		limit = chatNoteSearchDefaultLimit
	}
	if limit > chatNoteSearchMaxLimit {
		limit = chatNoteSearchMaxLimit
	}
	phrase := ftsQuote(query)
	likePattern := "%" + likeEscape(query) + "%"
	for _, tier := range chatNoteSearchTiers(phrase, likePattern) {
		hits, err := chatNoteQuery(ctx, db, tier.sql, tier.args, limit)
		if err != nil {
			return nil, ChatNoteTierNone, fmt.Errorf("chat note: search (%s): %w", tier.name, err)
		}
		if len(hits) == 0 {
			continue
		}
		return hits, tier.name, nil
	}
	// THE LADDER TIER — last, and only when nothing above answered.
	//
	// WHY IT EXISTS (measured, 2026-12-08). The three phrase tiers match the
	// query as ONE quoted string, which is the right shape for a human typing a
	// few words into `gopi session notes`. A PREFETCH query is a whole
	// conversation line, and no note contains that sentence: a real chat turn on
	// the operator's index came back `empty` — 0 rows in 12ms — for a query whose
	// answer was sitting in the notes, while the memory leg's ladder found it on
	// its first hit. Reusing the ladder is the fix, and it is REUSE: the walk,
	// its thresholds and its probe budget are memory_ladder.go's, parameterised
	// by the index's name (Rule 8, Rule 14).
	//
	// WHY LAST, NOT FIRST. A phrase hit is strictly better evidence than a
	// term-subset hit (the phrase proves the words are ADJACENT), so the cheap
	// precise tiers keep their precedence and the ladder only gets the query the
	// phrase tiers could not answer at all. Nothing that answered before answers
	// differently now.
	hits, err := chatNoteLadderSearch(ctx, db, query, limit)
	if err != nil {
		if ChatNoteIndexMissing(err) {
			// The index has no ladder-able FTS table at all (a ledger old enough
			// not to carry one), or it went away between the phrase tiers and
			// here: that is the same ABSENCE the callers already recognise, and
			// it must not turn a working phrase search into an error (Rule 15).
			return nil, ChatNoteTierNone, err
		}
		return nil, ChatNoteTierNone, fmt.Errorf("chat note: search (%s): %w", ChatNoteTierLadder, err)
	}
	if len(hits) == 0 {
		return nil, ChatNoteTierNone, nil
	}
	return hits, ChatNoteTierLadder, nil
}

// chatNoteLadderSearch runs the shared ladder against this index and reads its
// winning MATCH expression back into rows.
//
// THE LADDER ANSWERS WITH AN EXPRESSION, NOT WITH ROWS — that is its contract on
// the memory ledger (a candidate-set generator whose RANKING belongs to the
// caller) — so this re-runs the expression through the SAME projection, the SAME
// projection-side bm25 ordering and the SAME limit the phrase tiers use. One
// consequence worth stating: the ladder's walk already ordered its subsets by
// precision (terms retained, then dropped-token DF), so the expression it hands
// back is the most specific term set the probe budget found, and bm25 ranks the
// rows WITHIN it.
//
// IT LIVES HERE, not in the prefetch: the ladder tier is a property of the note
// INDEX's search (its fourth tier), so `gopi session notes` gets it too — one
// behaviour for one index, whenever the phrase tiers cannot answer.
func chatNoteLadderSearch(ctx context.Context, db *sql.DB, query string, limit int) ([]ChatNoteHit, error) {
	match, ok := LadderMatchOn(ctx, db, chatNotesFTSName, query)
	if !ok || match == "" {
		return nil, nil
	}
	projection := `SELECT n.id, n.` + chatNoteSessionCol + `, n.` + chatNoteTurnCol + `, n.` + chatNoteRoundCol + `, n.` + chatNoteTextCol + `, n.` +
		chatNoteHostCol + `, n.` + chatNotePathCol + `, n.` + chatNoteCreatedCol + `
	         FROM ` + chatNotesTableName + ` n`
	sqlText := projection + ` JOIN ` + chatNotesFTSName + ` f ON n.id = f.rowid
	         WHERE ` + chatNotesFTSName + ` MATCH ? ORDER BY bm25(` + chatNotesFTSName + `), n.` + chatNoteCreatedCol + ` DESC`
	return chatNoteQuery(ctx, db, sqlText, []any{match}, limit)
}

// ChatNoteSearch full-text searches the round-level note index, mirroring
// TaskSearch's tier ladder (one strategy for every index in this database):
//
//	fts      the unicode61 index — words and ASCII phrases, BM25-ranked
//	trigram  the trigram index — CJK substrings of 3+ characters, which
//	         unicode61 cannot answer because it keeps a contiguous CJK run as
//	         ONE token ("网络测试" is one token, never "网络" + "测试")
//	like     a scan over the note text for anything below trigram's lower
//	         bound (a 2-character CJK fragment)
//
// Tiers are tried in order and the FIRST hit-bearing tier wins: mixing tiers
// would double-rank a row both indexes hold. A tier that errors fails the whole
// search rather than falling through silently — a search that answered "no
// hits" because its index was broken is the failure Rule 15 exists to prevent.
//
// It IS ChatNoteHitTierSearch plus the terminal render — one query, one ranking,
// one set of rows, two faces.
func ChatNoteSearch(query string, limit int) (string, error) {
	query = strings.TrimSpace(query)
	if query == "" {
		return "", fmt.Errorf("chat note: search query must not be empty")
	}
	ctx := context.Background()
	db, err := openChatNotesDB(ctx)
	if err != nil {
		return "", err
	}
	defer db.Close()
	hits, tier, err := ChatNoteSearchOn(ctx, db, query, limit)
	if err != nil {
		return "", err
	}
	if len(hits) == 0 {
		return fmt.Sprintf("No notes matching %q.", query), nil
	}
	effective := limit
	if effective <= 0 {
		effective = chatNoteSearchDefaultLimit
	}
	if effective > chatNoteSearchMaxLimit {
		effective = chatNoteSearchMaxLimit
	}
	return renderChatNoteHits(query, tier, hits, effective), nil
}

// chatNoteQuery runs one tier and scans its rows (fetch `limit`+1 so a capped
// result can SAY it was capped instead of silently ending).
func chatNoteQuery(ctx context.Context, db *sql.DB, sqlText string, args []any, limit int) ([]ChatNoteHit, error) {
	if !chatNoteHandleUsable(db) {
		// THE DEREFERENCE THIS FUNCTION PERFORMS IS THE ONE THAT USED TO CRASH.
		// `db.QueryContext` reaches (`*DB`).conn internally, so the check has to
		// be HERE as well as at the exported face: this runner is also reached
		// through the tier walk of a caller that never went past an exported
		// entry point. The error is the same value, so the two guards cannot
		// drift apart in what they report (Rule 14).
		return nil, ErrChatNoteUnusableHandle
	}
	rows, err := db.QueryContext(ctx, sqlText, args...)
	if err != nil {
		return nil, err // no-wrap: the caller names the tier; wrapping here would need the same string twice
	}
	defer rows.Close()
	hits := make([]ChatNoteHit, 0, limit)
	for rows.Next() {
		var h ChatNoteHit
		if err := rows.Scan(&h.ID, &h.SessionID, &h.Turn, &h.Round, &h.Note, &h.Host, &h.Path, &h.CreatedAt); err != nil {
			return nil, err // no-wrap: same reason as above
		}
		hits = append(hits, h)
		if len(hits) > limit {
			break
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err // no-wrap: same reason as above
	}
	return hits, nil
}

// renderChatNoteHits formats the hits: a header naming the tier that answered
// and whether the result was capped, then one TWO-LINE block per hit — the
// provenance line (host, TURN, time, session) and the note's own full path
// beneath it. The path is on its own line because it is long, and it is shown
// because it is the fact that disambiguates two machines with the same home.
func renderChatNoteHits(query, tier string, hits []ChatNoteHit, limit int) string {
	var b strings.Builder
	capped := ""
	if len(hits) > limit {
		capped = fmt.Sprintf(" (capped at %d)", limit)
		hits = hits[:limit]
	}
	fmt.Fprintf(&b, "%d note(s) matching %q via %s%s:\n", len(hits), query, tier, capped)
	for _, h := range hits {
		// THE UNIT IS PRINTED, not implied: a new row says `turn N` (the axis every
		// surface addresses), and an old row says `round N` — the axis it really
		// recorded — so a reader can never mistake one number for the other
		// (Rule 15: absence and substitution are different facts).
		//
		// THE BOTH-ZERO CASE RESOLVES TO THE RETIRED WORD, and that is the
		// conservative answer rather than a leftover: a row whose turn is absent
		// AND whose round is absent records no number on either axis, and the two
		// columns cannot say which era wrote it (turn_index did not exist before
		// this change, so its 0 is also the DDL default). Printing `round ?` never
		// claims a turn the writer did not record — which is the whole point of
		// this paragraph; printing `turn ?` would.
		unit := chatNoteTurnWord
		number := h.Turn
		if number <= 0 {
			unit, number = chatNoteRoundWord, h.Round
		}
		label := unit + " ?"
		if number > 0 {
			label = unit + " " + strconv.Itoa(number)
		}
		host := h.Host
		if host == "" {
			host = "unknown-host"
		}
		fmt.Fprintf(&b, "  %s  %s  %s  %s\n", host, label, formatChatTime(h.CreatedAt), h.SessionID)
		fmt.Fprintf(&b, chatNotePathIndent+"%s\n", chatNoteDisplayPath(h.Path))
		fmt.Fprintf(&b, chatNotePathIndent+"%s\n", truncateChatNote(h.Note))
	}
	return b.String()
}

// chatNoteDisplayPath renders the run's path, saying so when the writer had no
// session directory to name — an empty path is a fact, not a blank.
func chatNoteDisplayPath(path string) string {
	if strings.TrimSpace(path) == "" {
		return "(no path recorded)"
	}
	return path
}

// truncateChatNote caps the rendered text on a RUNE boundary (a CJK note is
// never cut mid-character) and marks the cut.
func truncateChatNote(note string) string {
	runes := []rune(note)
	if len(runes) <= chatNoteRenderChars {
		return note
	}
	return string(runes[:chatNoteRenderChars]) + chatNoteRenderEllipsis
}
