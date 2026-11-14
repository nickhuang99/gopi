package tools

// chats table (M4.6, t-1788041668 + t-1788041900): chat conversations are a
// DIFFERENT species from execution tasks — of the tasks ledger's 13 columns
// the queue contract (parent/tags/payload/source/claim trail/状态机) is
// meaningless for a conversation (the 116-row ledger audit: 79% of auto rows
// were chats with every queue column permanently empty). Chats get their OWN
// table here — a session INDEX layer over the authoritative session JSONL
// files (hermes' 4.3GB messages_fts lesson: content stays in the pointer
// files, the DB stores ONLY index facts — no messages table).
//
// Registration routing (M4.6 钩子分流): chat processes (gopi chat / gopi
// web) land HERE via agent.RegisterChatSession / RecordChatTurn /
// CloseChatRow (internal/agent/chatregister.go); the runEngine path keeps
// writing the tasks ledger (taskregister.go) — plain one-shot runs stay
// execution rows. The [auto]-shaped chat rows in tasks are RETIRED.
//
// All columns are FACTS (no state machine): a conversation is born running
// and ends once (end_reason: normal|interrupt|crash). log_path is a pointer;
// the JSONL remains the authoritative full text. mode carries "chat"|"web"
// (the CLI entry face). title is reserved (LLM session titles — generator
// deferred; the column exists so the schema does not churn when it lands).
// brewed_at is the reserved M3 蒸馏登记 column (the brew pipeline's input
// source upgrades from scanning the sessions dir to reading this table).

// chatsFTSTable / chatsTriTable index CONVERSATIONS (title + first_msg).
// The ROUND-LEVEL notes inside a conversation are indexed by the SIBLING
// pair over chat_notes (chat_notes_fts / chat_notes_tri, chat_notes.go):
// the same database, the same external-content + trigger shape, a finer
// grain. That is still NOT a messages-level index — see chat_notes.go's
// header for the boundary (a note is the model's own capped summary, never
// a message dump; the hermes 4.3GB lesson is why the content stays in the
// JSONL pointer files).

import (
	"bufio"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/nickhuang99/gopi/internal/cliverbs"
	"github.com/nickhuang99/gopi/internal/config"
	"github.com/nickhuang99/gopi/internal/inputsource"
	"github.com/nickhuang99/gopi/internal/sessionevent"
	"github.com/nickhuang99/gopi/internal/sessionlayout"
	"github.com/nickhuang99/gopi/internal/tasksource"
)

// chatMode values (the CLI entry face — set by the caller).
//
// TWO modes, and every chat SURFACE is one of them: a carrier that changes the
// WIRE (the terminal surface of `gopi cli`, the SSE page of `gopi web`) still
// records an ordinary chat conversation, so it registers ChatModeCLI or
// ChatModeWeb rather than inventing a third value. A `"cli"` literal was tried
// for the terminal carrier and this table's own gate rejected it during the
// live acceptance run ("chat: invalid mode \"cli\" (want chat|web)"); the fix
// was to stop inventing a mode, not to widen the schema for a carrier detail.
const (
	ChatModeCLI = "chat"
	ChatModeWeb = "web"
)

// chatEndReason values (the semantic exit, t-1788041900 ②).
const (
	chatEndNormal    = "normal"    // exit 0 — the user quit cleanly
	chatEndInterrupt = "interrupt" // non-zero exit — an API/turn failure cut the chat
	chatEndCrash     = "crash"     // reserved: killed process (no clean close ran)
)

// validChatMode reports whether m is a known chat mode.
func validChatMode(m string) bool {
	return m == ChatModeCLI || m == ChatModeWeb
}

// chatDBEnv is the --db override for the chat index (read in exactly one
// place — chatDBPath, Rule 2). Same file as the tasks ledger by default
// (one db, several index tables).
const chatDBEnv = "GOPI_CHAT_DB"

// chatSourceColumnName is the chats.source column: WHO held the console for
// this conversation. Its vocabulary is NOT invented here — it is
// tasksource's (human|agent|worker|merger|server|…), the same one the tasks
// ledger's own `source` column carries, so a reader learns ONE provenance
// vocabulary for both tables instead of two that happen to agree today
// (Rule 14).
//
// WHY THE COLUMN EXISTS (the defect, measured 2026-10-03). Verification and
// replay runs drive the REAL `gopi chat` binary in the OPERATOR's own
// profile, and each such run left an ordinary chats row — indistinguishable
// in `session list --mode chat` from a conversation the operator had himself.
// The operator's index read "Say the single word: pong. Do not call any
// tool." 21 times among his own chats. The row already carried every fact
// EXCEPT the one that answers "is this mine?".
//
// WHAT IT DOES NOT DO: it does not hide, filter away or delete those rows
// (distinguishing is not hiding). A row whose source is empty renders as the
// explicit ChatSourceUnrecorded word, NEVER as an assumed value — "not
// recorded" is not evidence of "the operator's" (Rule 15a).
const chatSourceColumnName = "source"

// chatsSourceColumn widens a pre-existing chat index in place (the
// ensureSchema ALTER pattern tasks.go already uses for its own late columns —
// no migration script, and a duplicate-column error is the no-op case).
const chatsSourceColumn = "ALTER TABLE chats ADD COLUMN " + chatSourceColumnName + " TEXT DEFAULT ''"

// ChatSourceUnrecorded is how a row with NO recorded provenance renders. It
// is a WORD, not the shared em dash: the dash means "this column has no
// value", and the whole point of this column is that its absence must not be
// read as any provenance at all. It is exported because `session list` and
// `session show` render the same word (Rule 13: one definition, referenced).
const ChatSourceUnrecorded = "(unrecorded)"

// chatDBPath resolves the chat index's database file.
func chatDBPath() string {
	if p := strings.TrimSpace(os.Getenv(chatDBEnv)); p != "" {
		return p
	}
	return memoryDBPath()
}

// chatsTable is the chat index itself — session facts only, no queue
// contract. Numeric columns backfill lazily (ChatRecordTurn folds the
// cumulative CostTracker totals monotonically; ChatCloseSession stamps the
// ending), so a mid-run crash leaves a running row the next backfill/turn
// heals or the close finishes.
func chatsTable() string {
	return `CREATE TABLE IF NOT EXISTS chats (
	session_id        TEXT PRIMARY KEY,
	mode              TEXT NOT NULL CHECK (mode IN ('chat','web')),
	source            TEXT DEFAULT '',
	model             TEXT DEFAULT '',
	title             TEXT DEFAULT '',
	first_msg         TEXT DEFAULT '',
	turn_count        INTEGER DEFAULT 0,
	input_tokens      INTEGER DEFAULT 0,
	output_tokens     INTEGER DEFAULT 0,
	cache_tokens      INTEGER DEFAULT 0,
	workdir           TEXT DEFAULT '',
	log_path          TEXT DEFAULT '',
	parent_session_id TEXT DEFAULT '',
	git_branch        TEXT DEFAULT '',
	git_repo_root     TEXT DEFAULT '',
	end_reason        TEXT DEFAULT '' CHECK (end_reason IN ('','normal','interrupt','crash')),
	started_at        REAL NOT NULL,
	last_msg_at       REAL,
	ended_at          REAL,
	brewed_at         REAL
)`
}

// chatsFTSTable / chatsTriTable are the FTS5 companions over the chat index
// (title + first_msg are the searchable content), mirroring tasks_fts /
// tasks_tri. No messages-level index — the JSONL pointer files hold the
// content (the hermes 4.3GB lesson).
const (
	chatsFTSTable = `CREATE VIRTUAL TABLE IF NOT EXISTS chats_fts USING fts5(
		session_id UNINDEXED, mode UNINDEXED, title, first_msg,
		content='chats', content_rowid='rowid', tokenize='unicode61'
	)`
	chatsTriTable = `CREATE VIRTUAL TABLE IF NOT EXISTS chats_tri USING fts5(
		title, first_msg, content='chats', content_rowid='rowid', tokenize='trigram'
	)`
)

// chatsFTSTriggers keep the two indexes in lockstep with chats writes.
var chatsFTSTriggers = []string{
	`CREATE TRIGGER IF NOT EXISTS chats_ai AFTER INSERT ON chats BEGIN
		INSERT INTO chats_fts(rowid, session_id, mode, title, first_msg)
			VALUES (new.rowid, new.session_id, new.mode, new.title, new.first_msg);
		INSERT INTO chats_tri(rowid, title, first_msg)
			VALUES (new.rowid, new.title, new.first_msg);
	END`,
	`CREATE TRIGGER IF NOT EXISTS chats_ad AFTER DELETE ON chats BEGIN
		INSERT INTO chats_fts(chats_fts, rowid, session_id, mode, title, first_msg)
			VALUES ('delete', old.rowid, old.session_id, old.mode, old.title, old.first_msg);
		INSERT INTO chats_tri(chats_tri, rowid, title, first_msg)
			VALUES ('delete', old.rowid, old.title, old.first_msg);
	END`,
	`CREATE TRIGGER IF NOT EXISTS chats_au AFTER UPDATE ON chats BEGIN
		INSERT INTO chats_fts(chats_fts, rowid, session_id, mode, title, first_msg)
			VALUES ('delete', old.rowid, old.session_id, old.mode, old.title, old.first_msg);
		INSERT INTO chats_tri(chats_tri, rowid, title, first_msg)
			VALUES ('delete', old.rowid, old.title, old.first_msg);
		INSERT INTO chats_fts(rowid, session_id, mode, title, first_msg)
			VALUES (new.rowid, new.session_id, new.mode, new.title, new.first_msg);
		INSERT INTO chats_tri(rowid, title, first_msg)
			VALUES (new.rowid, new.title, new.first_msg);
	END`,
}

// chatIndexes: value-signal from day one (the memory-lowquality lesson —
// ranking/filter columns must be live on day one, never inert decoration).
var chatIndexes = []string{
	`CREATE INDEX IF NOT EXISTS idx_chats_mode ON chats(mode)`,
	`CREATE INDEX IF NOT EXISTS idx_chats_started ON chats(started_at)`,
}

// ensureChatSchema creates the chats table, its indexes and the FTS pair.
// Idempotent (the ensureSchema pattern) — safe on every open.
func ensureChatSchema(ctx context.Context, db *sql.DB) error {
	stmts := append([]string{chatsTable()}, chatIndexes...)
	stmts = append(stmts, chatsFTSTable, chatsTriTable)
	stmts = append(stmts, chatsFTSTriggers...)
	for _, s := range stmts {
		if _, err := db.ExecContext(ctx, s); err != nil {
			return fmt.Errorf("chat schema: %w", err)
		}
	}
	// CREATE TABLE IF NOT EXISTS does not touch an existing table, so an index
	// built before this column existed must be widened in place. "duplicate
	// column name" is the expected no-op on an already-migrated db — swallowed,
	// exactly as tasks.go swallows it (the ensureSchema pattern).
	if _, err := db.ExecContext(ctx, chatsSourceColumn); err != nil {
		if !strings.Contains(err.Error(), chatDuplicateColumnErr) {
			return fmt.Errorf("chat schema %s: %w", chatSourceColumnName, err)
		}
	}
	return nil
}

// chatDuplicateColumnErr is the driver's wording for the already-migrated
// case, named once (Rule 1b: a comparison target is never typed inline twice
// — this literal is also the one cmd/agent asserts on).
const chatDuplicateColumnErr = "duplicate column name"

// openChatDB opens the chat index database and ensures the schema.
//
// The chat index shares its file with the knowledge base and the tasks ledger
// (chatDBPath's default IS memoryDBPath), so it is NOT this face's database to
// shape: it goes through openLedgerIndexDB, which builds the UNION of the
// faces when it creates the file. Before that door existed, whoever opened the
// shared file first decided what it was — and this opener published it as a
// chats-only file, after which `gopi task list` on the same path refused for
// ever with "missing distilled_qa table" (measured, ledger_schema_union.go).
func openChatDB(ctx context.Context) (*sql.DB, error) {
	return openLedgerIndexDB(ctx, chatDBPath(), ensureChatSchema)
}

// openChatDBReadOnly is the same door in the READ-ONLY shape: a caller that has
// promised to report without writing (`chat backfill --dry-run`) opens here.
//
// It goes through the SAME opener, on the same path resolution (chatDBPath), so
// the two shapes cannot disagree about WHICH file the face reads (Rule 14). What
// differs is only what openLedgerIndexDBFor does with the mode: no writable
// DSN, no `_txlock=immediate`, no schema ensure — see its doc for the measured
// reason a query_only pragma on the writable opener was not enough.
func openChatDBReadOnly(ctx context.Context) (*sql.DB, error) {
	return openLedgerIndexDBReadOnly(ctx, chatDBPath())
}

// ChatSession is one chat row's registration payload (the START hook's
// arguments — internal/agent/chatregister.go). facts carries the backfill's
// re-derived session JSONL stats (nil for the live-registration path).
type ChatSession struct {
	SessionID string
	Mode      string // chat | web
	// Source is the provenance of the conversation — WHO held this console.
	// It carries tasksource's vocabulary (internal/tasksource), the same one
	// the tasks ledger's `source` column carries, and it is derived at the
	// write seam from the process signature, never guessed. An EMPTY value is
	// preserved as empty: a row that recorded no provenance renders as
	// ChatSourceUnrecorded, because "nothing was recorded" is not a claim
	// that a person held the console (Rule 15a).
	Source  string
	Model   string
	LogPath string
	// FirstMsg is a CANDIDATE first operator line, not "the text this turn
	// carried". It is stored only when FirstMsgSource says the person produced
	// it (chatLineIsOperatorOwn, chat_first_msg.go); otherwise the row gets
	// ChatFirstMsgPlaceholder and the candidate is preserved where it belongs
	// — the session JSONL, which is the record this table indexes.
	FirstMsg string
	// FirstMsgSource is the FACE FirstMsg was recorded with (internal/inputsource:
	// human, queue, notify, llm). "" means the row predates faces and is read as
	// human — every such row really was the person's (the default the session
	// reader applies too).
	FirstMsgSource string
	Workdir        string
	GitBranch      string
	GitRepoRoot    string

	facts *chatSessionFacts
}

// chatTextMaxChars caps one normalized text column (first_msg / title).
const chatTextMaxChars = 200

// normalizeChatText folds text single-line, whitespace-collapsed and
// hard-capped (the [auto] summary convention: the row is the index, the
// JSONL carries the full text).
func normalizeChatText(s string) string {
	s = strings.Join(strings.Fields(s), " ")
	r := []rune(s)
	if len(r) > chatTextMaxChars {
		s = string(r[:chatTextMaxChars]) + "…"
	}
	return s
}

// ChatEnsureRow ensures the chat's row exists (the START hook). INSERT on
// first sight; on conflict the non-empty incoming facts refresh the row.
//
// FIRST_MSG IS THE OPERATOR'S FIRST LINE AND NOTHING ELSE (the 2026-10-28 fix;
// the whole reasoning is in chat_first_msg.go). The column used to take
// `excluded.first_msg` whenever it was non-empty, so whatever text the LAST turn
// carried became the index head — and on a notice turn that text is the
// framework's own sentence, which is how three of the operator's rows came to be
// titled "[system] 16 notices are pending: …". Now:
//
//   - the candidate is stored ONLY when chatLineIsOperatorOwn accepts it under
//     its recorded face;
//   - otherwise the row carries ChatFirstMsgPlaceholder, and the injected text
//     stays in the session JSONL (nothing is discarded — it is a turn, it is
//     logged, it counts);
//   - the column is written ONCE: an existing operator line is never replaced,
//     so "first" means first rather than latest.
//
// started_at is set once — the row's birth, never rewritten.
func ChatEnsureRow(s ChatSession) (string, error) {
	if !validChatMode(s.Mode) {
		return "", fmt.Errorf("chat: invalid mode %q (want chat|web)", s.Mode)
	}
	if strings.TrimSpace(s.SessionID) == "" {
		return "", fmt.Errorf("chat: session_id must not be empty")
	}
	firstMsg := ChatFirstMsgPlaceholder
	if chatLineIsOperatorOwn(s.FirstMsg, s.FirstMsgSource) {
		firstMsg = normalizeChatText(s.FirstMsg)
	}
	s.FirstMsg = firstMsg
	s.Source = strings.TrimSpace(s.Source)
	if s.Source != "" {
		value, ok := tasksource.Normalize(s.Source)
		if !ok {
			return "", fmt.Errorf("chat: invalid source %q (want %s)", s.Source, tasksource.Policy())
		}
		s.Source = value
	}
	s.Workdir = strings.TrimSpace(s.Workdir)
	s.LogPath = strings.TrimSpace(s.LogPath)
	s.Model = strings.TrimSpace(s.Model)
	s.GitBranch = strings.TrimSpace(s.GitBranch)
	s.GitRepoRoot = strings.TrimSpace(s.GitRepoRoot)
	ctx := context.Background()
	db, err := openChatDB(ctx)
	if err != nil {
		return "", err
	}
	defer db.Close()
	now := float64(time.Now().Unix())
	// FIRST-WRITER-WINS, and the writer must be the operation's own line: the
	// clause only fires when the stored value is still UNSET (empty, or the
	// placeholder) — the predicate is chatFirstMsgUnsetSQLPredicate's, bound here
	// so the constant exists once (Rule 1/13).
	_, err = db.ExecContext(ctx, `INSERT INTO chats (
		session_id, mode, source, model, first_msg, workdir, log_path,
		git_branch, git_repo_root, started_at, last_msg_at)
	VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	ON CONFLICT(session_id) DO UPDATE SET
		source=CASE WHEN excluded.source != '' THEN excluded.source ELSE chats.source END,
		model=CASE WHEN excluded.model != '' THEN excluded.model ELSE chats.model END,
		first_msg=CASE WHEN `+chatFirstMsgUnsetSQLPredicate+` THEN excluded.first_msg ELSE chats.first_msg END,
		workdir=CASE WHEN excluded.workdir != '' THEN excluded.workdir ELSE chats.workdir END,
		log_path=CASE WHEN excluded.log_path != '' THEN excluded.log_path ELSE chats.log_path END,
		git_branch=CASE WHEN excluded.git_branch != '' THEN excluded.git_branch ELSE chats.git_branch END,
		git_repo_root=CASE WHEN excluded.git_repo_root != '' THEN excluded.git_repo_root ELSE chats.git_repo_root END,
		last_msg_at=excluded.last_msg_at`,
		s.SessionID, s.Mode, s.Source, s.Model, s.FirstMsg, s.Workdir, s.LogPath,
		s.GitBranch, s.GitRepoRoot, now, now, ChatFirstMsgPlaceholder)
	if err != nil {
		return "", fmt.Errorf("chat: ensure %s: %w", s.SessionID, err)
	}
	return fmt.Sprintf("chat row ensured: %s (mode=%s)", s.SessionID, s.Mode), nil
}

// ChatRecordTurn bumps the chat row's per-turn activity facts (the per-turn
// hook): turn_count +1, tokens folded MONOTONICALLY (the caller passes the
// CUMULATIVE CostTracker totals each turn — max() keeps the largest seen,
// so a duplicate or out-of-order call can never shrink the row).
func ChatRecordTurn(sessionID string, prompt, completion, cacheTokens int) error {
	return ChatRecordTurnOutcome(sessionID, true, prompt, completion, cacheTokens)
}

// ChatRecordTurnOutcome is ChatRecordTurn with the TURN question made explicit:
// `opened` says whether the turn just finished OPENED A SESSION TURN under the
// one predicate that decides it (sessionevent.IsTurnOpeningRow — the reader's).
//
// WHY IT EXISTS (task 01a17a37, measured 2026-10-27): `chats.turn_count` was
// advanced by EVERY completed turn, including the ones the session log opens no
// turn at (a background-job notice the harness wrote from the human face), so the
// ledger ran AHEAD of the reader — session 80c07a7d-bb6b-48b4-be66-ea1867f403f2
// held 18 in the ledger and 11 in `session log --turns`, both describing one
// session. Two numbers for one word is the defect the epic removes; the ledger
// column must be the reader's counter, not a request counter wearing its name.
//
// `opened == false` still folds the TOKENS: the model really did spend them, and
// dropping them would make the row's cost depend on a labelling decision. Only the
// TURN column is withheld.
func ChatRecordTurnOutcome(sessionID string, opened bool, prompt, completion, cacheTokens int) error {
	if strings.TrimSpace(sessionID) == "" {
		return fmt.Errorf("chat: session_id must not be empty")
	}
	ctx := context.Background()
	db, err := openChatDB(ctx)
	if err != nil {
		return err
	}
	defer db.Close()
	now := float64(time.Now().Unix())
	// Rule 1: the two shapes are one statement each, named here rather than
	// inline, because the difference between them IS the fix.
	const recordTurnSQL = `UPDATE chats SET
		turn_count = turn_count + 1,
		input_tokens = MAX(input_tokens, ?),
		output_tokens = MAX(output_tokens, ?),
		cache_tokens = MAX(cache_tokens, ?),
		last_msg_at = ?
		WHERE session_id = ?`
	const foldTokensSQL = `UPDATE chats SET
		input_tokens = MAX(input_tokens, ?),
		output_tokens = MAX(output_tokens, ?),
		cache_tokens = MAX(cache_tokens, ?),
		last_msg_at = ?
		WHERE session_id = ?`
	query := recordTurnSQL
	if !opened {
		query = foldTokensSQL
	}
	res, err := db.ExecContext(ctx, query, prompt, completion, cacheTokens, now, sessionID)
	if err != nil {
		return fmt.Errorf("chat: record turn %s: %w", sessionID, err)
	}
	//stylegate:ignore n==0 is the meaningful branch; a driver error yields the same 0 and same path
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("chat: no row for session %s (ensure it first)", sessionID)
	}
	return nil
}

// ChatCloseSession stamps the chat's ending: ended_at + end_reason
// (normal|interrupt|crash). Idempotent — already-ended rows keep their
// first ending (one conversation ends once).
func ChatCloseSession(sessionID, reason string) error {
	if strings.TrimSpace(sessionID) == "" {
		return fmt.Errorf("chat: session_id must not be empty")
	}
	switch reason {
	case chatEndNormal, chatEndInterrupt, chatEndCrash:
	default:
		return fmt.Errorf("chat: invalid end_reason %q (want normal|interrupt|crash)", reason)
	}
	ctx := context.Background()
	db, err := openChatDB(ctx)
	if err != nil {
		return err
	}
	defer db.Close()
	res, err := db.ExecContext(ctx, `UPDATE chats SET ended_at = ?, end_reason = ?
		WHERE session_id = ? AND ended_at IS NULL`,
		float64(time.Now().Unix()), reason, sessionID)
	if err != nil {
		return fmt.Errorf("chat: close %s: %w", sessionID, err)
	}
	//stylegate:ignore n==0 is the meaningful branch; a driver error yields the same 0 and same path
	if n, _ := res.RowsAffected(); n == 0 {
		// Already ended (idempotent no-op) or never registered (an empty
		// chat — nothing to close); both are fine for an END hook.
		return nil
	}
	return nil
}

// chatListEmptyResult is the rendered index's zero-row text — the standard
// "nothing here" line, shared by the chat index and the session family so the
// two faces of one query cannot spell emptiness two ways.
const chatListEmptyResult = "No chats found."

// chatListDefaultLimit caps one ChatList render when the caller passes no
// limit (an index page, not an export).
const chatListDefaultLimit = 50

// ChatListAllFlag is the ONE spelling of the flag that brings the rows whose
// session LOG is not on this machine back into an index view: `gopi session
// list --all` and `gopi chat list --all`. It is named here (Rule 1b/13)
// because the DISCLOSURE LINE below tells the reader which command reveals the
// rows the default view hid — an instruction that must name the real flag, not
// a re-typed string that can drift from the parsers' whitelists (the
// TaskListAllFlag precedent, and its same reasoning).
const ChatListAllFlag = "--all"

// ChatFileHiddenPrefix opens the ABSENT-ROW disclosure line — the "[hidden] "
// token a reader (and a test) greps for. A bracketed word rather than bare
// digits, for the same reason as its ledger sibling: the line rides under a
// count header, and a line leading with a number is readable as a second
// header by anything that parses the render.
const ChatFileHiddenPrefix = "[hidden] "

// chatFileHiddenFormat spells the disclosure line's body ONCE (Rule 1/13: the
// count and the two named values are the only variables).
const chatFileHiddenFormat = "%s%d session(s) hidden — their session log is not on this machine; %s shows them"

// ChatFileMissingWord is the ONE spelling of "this row's session log is not on
// this machine" — the word the per-row marker and the summary line are both
// built from, so the two renders cannot describe the same fact in two words.
const ChatFileMissingWord = "missing"

// ChatFilePresentWord is the complement, for the surfaces that render the fact
// as a field rather than as an absence marker (`session show`, `--json`'s
// file_present).
const ChatFilePresentWord = "present"

// ChatFileMissingPrefix marks ONE rendered row whose session log is not on this
// machine. It is a MARKER, not a filter: with --all the row is shown, and this
// is how a reader tells "the index knows about this conversation" from "this
// conversation can be read here". The bracketed word is BUILT from
// ChatFileMissingWord (Rule 13: one definition of the word, referenced).
const ChatFileMissingPrefix = "[" + ChatFileMissingWord + "] "

// ChatTokensUnknownWord is the ONE spelling of "the upstream API never reported
// usage for this session" — the machine-readable word the `--json` faces carry
// and the word the two human renderings are built from (Rule 1b/13, the
// ChatFileMissingWord precedent).
//
// WHY A WORD AND NOT "0" (Rule 15). The token columns are folded by
// Agent.RecordChatTurn from deepseek.CostTracker totals, and those totals only
// move on a POSITIVE count. Measured (2026-10-04) on a real session log:
// `"usage":{"input_tokens":0,"output_tokens":0}` — the provider DID send a
// usage object and every counter in it was zero, so the tracker absorbed
// nothing. A session like that lands on the schema's DEFAULT 0 — byte-for-byte
// the same row as a turn that really spent nothing. Rendering both as
// `in=0 out=0 cache=0` states a measured spend for a session that was never
// measured; "unknown" states the truth: no positive count was ever reported,
// so there is no number to print. (The A-vs-B distinction is deliberate: 0 is
// a NUMBER, unknown is the ABSENCE of one, and a row whose usage WAS reported
// non-zero keeps printing real counts.)
//
// The zero can therefore arrive by either of the two shapes a silent provider
// produces — no usage block at all, or a usage block whose counters are all 0 —
// and the criterion does not pretend to tell them apart, because both are
// equally "nothing was measured". What the WORD claims is exactly what is
// known: no positive usage was ever reported.
const ChatTokensUnknownWord = "unknown"

// ChatTokenUsageNote is the index row's whole token clause when the usage is
// unknown. Long on purpose: it sits in a parenthesised fact list whose other
// members are short, and a bare `tok unknown` would leave the reader unable to
// tell "the provider stayed silent" from "gopi failed to record it".
//
// It says POSITIVE usage for the reason the constant above records: the
// measured shape is a usage object full of zeros, so "the API reported no
// usage" would be a claim about bytes that are sometimes present in the log.
const ChatTokenUsageNote = "tok " + ChatTokensUnknownWord + " — no positive usage was ever reported"

// ChatTokenCountsFormat spells the MEASURED token clause once (Rule 1/13: the
// three counts are the only variables) — the clause BOTH human faces render
// when the usage IS known, so the index and the summary cannot drift.
//
// IT SAYS "total" BECAUSE THAT IS WHAT THE COLUMNS HOLD, AND ITS PREDECESSOR
// SAID "peak" AND WAS WRONG (task 01a1954e, measured 2026-11-01).
//
// THE FOLD IS MAX, AND THE VALUE FOLDED IS CUMULATIVE — so the column is a
// TOTAL, not a high-water mark. The recorder's three operands are
// `ag.Cost.TotalPromptTokens` / `TotalCompletionTokens` /
// `TotalCacheHitTokens+TotalCacheMissTokens` (cmd/agent/cli_chat.go:4584,
// term.go:417, cli_web.go:1605), and those fields only ever advance:
// `CostTracker.AddClaudeUsage` (internal/deepseek/claude_client.go:303) does
// `ct.TotalPromptTokens += u.PromptSideTokens()` ONCE PER API REQUEST. A
// monotonically increasing sequence folded with MAX yields its LAST element,
// which is the session's running total. The ONLY thing the MAX buys is
// replay-safety: a duplicated or out-of-order write cannot shrink the row.
//
// MEASURED (not inferred from the SQL's shape — Rule 41): session
// d6120d04-98fa-454d-a2f0-51ea7c661956 wrote `input_tokens` 109705 and its
// three wire requests carry prompt sides 36455 / 36576 / 36674, whose SUM is
// 109705 and whose MAX is 36674. Session 34bb9adc… wrote 8086768 against the
// same 8086768 SUM and a per-request MAX of 102312. The column is the SUM.
//
// The word "peak" (introduced by 9f00fd60c) is therefore RETIRED: it described
// the fold's shape rather than the quantity, and the doc that froze it compared
// this column against `gopi session stats` — a surface which (a) adds up the
// session log's own rows and (b) is fed CUMULATIVE rows on the chat paths, so
// neither of its two numbers is the per-request reading it claims. The note
// below names that difference; the defect is filed separately, because the fix
// belongs to that reader and not to this label.
const ChatTokenCountsFormat = "total tok in=%d out=%d cache=%d"

// ChatTokenBasisWord is the MACHINE-READABLE name of the quantity
// ChatTokenCountsFormat's three counts carry — published as `tokens_basis` on
// every JSON row, because the human label is not available to a script and a
// consumer has to know what it is adding up (Rule 15: the machine-readable face
// must not be the one that loses the fact the human face carries).
//
// IT READ "per-request-peak" FOR ONE COMMIT, AND THAT WAS FALSE (9f00fd60c,
// corrected by 01a1954e). That word named the FOLD's shape instead of the
// QUANTITY: MAX(x, ?) over a monotonically growing series returns the LAST
// element of that series, and the series here is the session's running total,
// so the value is a total. A wire value is a contract, which is why changing it
// is stated rather than slipped in: it shipped in one commit the same day, no
// reader was built on it, and leaving a wrong word in place would be the
// permanent defect while renaming costs nothing yet.
const ChatTokenBasisWord = "session-total"

// ChatTokenBasisNote is the sentence printed beside the total clause. A label
// ("total") says what the number IS; this says what it is NOT, and why the
// stored column can be trusted not to shrink — the two things a reader needs to
// stop re-filing this defect.
//
// IT POINTS AT THE RAW WIRE LOG, AND THAT POINTER WAS CORRECTED BY MEASUREMENT
// (2026-11-01). It first said `gopi session log`; run against session d6120d04,
// that verb prints a SUMMARY COST block, not per-request figures — and its
// `Prompt: 219,191` is the sum of that log's own rows, which on the chat paths
// are CUMULATIVE (internal/agent/chat_ops.go:843 hands LogRequest
// `a.Cost.TotalPromptTokens`; the task loop at internal/agent/loop.go:5105 hands
// it the per-request DELTA). So it reported 219,191 for a session whose total is
// 109,705 — the identical defect as `gopi session stats`, which printed
// 250,670,748,726 for e1e22e9f against a true 266,909,263. `gopi session raw`
// DOES carry the per-request counts (verified: `"input_tokens":36455` blocks),
// because it prints the wire frames themselves rather than a derived sum.
//
// It is a SLICE OF LINES, not one long sentence, because the show page renders
// it in the value column's indented slot: one long run wraps wherever the
// reader's terminal decides and stops reading as an explanation.
var ChatTokenBasisNote = []string{
	"these are this session's RUNNING TOTALS, not per-request peaks: the recorder",
	"is handed the tracker's cumulative count each turn, and the MAX fold makes",
	"the last write win so a replay can never shrink it. Per-request figures live",
	"in the RAW WIRE LOG — `" + cliverbs.Program + " " + cliverbs.SessionRawAddress + "` prints each request.",
}

// ChatTokenBasisNoteText is the same note joined into one line of prose — for
// messages and assertions that quote it without caring where the page breaks it.
// Derived from ChatTokenBasisNote (Rule 13: one definition, two renderings), so a
// reworded note cannot leave a stale copy behind in a message.
func ChatTokenBasisNoteText() string { return strings.Join(ChatTokenBasisNote, " ") }

// ChatTokensUnmeasuredNote is the `session show` tokens line's value when the
// usage was never reported. It names what IS known (nothing was measured),
// what the stored columns hold (the default zero), and that the zero is not a
// spend — the three things a reader needs to stop reading 0 as a measurement.
const ChatTokensUnmeasuredNote = ChatTokensUnknownWord +
	" — no positive token count was ever reported for this session: every recorded turn carried either no usage block or all-zero counters, so the stored token columns hold their default 0 rather than a measured spend"

// ChatLogAbsentNextStep is what the operator can DO about a row whose session
// log is not on this machine. It is not advice — each clause names a real
// surface: the flag is referenced from its own constant (ChatListAllFlag), the
// re-index verb is quoted as the CLI spells it, and the log's absence is stated
// as the hard limit it is (no local command can conjure the bytes).
const ChatLogAbsentNextStep = "the bytes are only on the machine that recorded the session — copy that machine's sessions tree over, " +
	"or re-index the logs THIS machine holds with `gopi chat backfill`"

// ChatLogAbsentIndexNote is the `session show` line that answers the question a
// reader of a row with `log file missing` immediately has: is this session
// gone, or is its LOG elsewhere? The row IS in the index (the page is being
// rendered from it), so the honest sentence says exactly that and hands over
// the next step. It exists as one constant because the sentence must not be
// re-typed at a second surface — and because a message that denies the
// session's existence for a row the operator is reading is the Rule 15 defect
// this line removes.
const ChatLogAbsentIndexNote = "the session EXISTS in the chat index here, but its session log is NOT on this machine (" +
	ChatListAllFlag + " on `gopi session list` lists the row); " + ChatLogAbsentNextStep

// ChatSessionNotIndexedNextStep is the next step for the OPPOSITE gap: the log
// IS on this machine, the chats index simply has no row for it. The remedy is a
// real, existing surface (the same re-index verb named above), so the sentence
// can hand the operator a command instead of a denial.
const ChatSessionNotIndexedNextStep = "this machine HAS a session log for it — re-index the logs this machine holds with " +
	"`gopi chat backfill`, then try again"

// ChatSessionNotIndexedError is the answer for that second gap: the id resolves
// to a session log on THIS machine, so "no such session" would be false, while
// the index genuinely cannot describe the run. It is a named type so a caller
// can recognise the case (the CLI prints its message; a future `--json` face can
// carry the code) — the not-found branch stays a plain error, so the two are
// distinguishable in code and not only in prose.
type ChatSessionNotIndexedError struct {
	SessionID string
	NextStep  string
}

func (e *ChatSessionNotIndexedError) Error() string {
	return fmt.Sprintf("chat: no row for session %s in the chat index, but %s", e.SessionID, e.NextStep)
}

// ChatSessionLogOnMachine answers, for a bare session id, the question the
// not-found branch needs: does THIS machine hold a log for it? It reaches the
// presence criterion through the SAME implementation the list filter and the
// show path use (chatSessionLogPresent over a detail carrying only the id) —
// Rule 13/14: there is one definition of "the log is on this machine", and a
// caller that has no chat row must not get a second, weaker answer.
func ChatSessionLogOnMachine(sessionID string) (bool, error) {
	id := strings.TrimSpace(sessionID)
	if id == "" {
		return false, nil
	}
	dir, err := chatSessionsDir()
	if err != nil {
		return false, err
	}
	return chatSessionLogPresent(ChatSessionDetail{SessionID: id}, dir)
}

// ChatFileHiddenNote renders the absent-row disclosure for a view that dropped
// n rows — and "" for n == 0, because a view that hid nothing must not claim it
// did (Rule 15 cuts both ways). It is a FUNCTION, not a const, so the zero case
// cannot be rendered by accident.
//
// WHY THIS EXISTS. The chats table is an INDEX over session JSONL files: a row
// records where the conversation WAS. Pointed at another host's profile, a
// cleaned scratch root, or an `--db` fixture, the index still answers — and
// every row it answers with looks exactly like a session the operator can open
// (measured 2026-10-05: 48 of 50 rows in `session list --mode chat` named a
// file that is not on this machine). A READ COMMAND THAT OVER-REPORTS is a read
// command that lies by omission the other way round: the operator cannot tell
// "these are your sessions" from "these are rows pointing at files you do not
// have". So the default view shows what is HERE and SAYS, in numbers, what it
// dropped, and `--all` is the door to the complete index.
func ChatFileHiddenNote(n int) string {
	if n <= 0 {
		return ""
	}
	return fmt.Sprintf(chatFileHiddenFormat, ChatFileHiddenPrefix, n, ChatListAllFlag)
}

// ChatList renders the chat index, newest first (started_at DESC). The filter
// is the caller's own typing (query/model/mode); limit ≤0 means the default
// cap. task search deliberately does NOT cover chats (different species,
// different retrieval) — this is the chat-native retrieval surface, and it is
// the ONE renderer behind both `gopi chat list` and `gopi session list`.
type ChatListFilter struct {
	// Query narrows via LIKE over first_msg / title / session_id.
	Query string
	// Model filters exact (an empty value means "every model").
	Model string
	// Mode is the chat index's own mode column: ChatModeCLI ("chat") or
	// ChatModeWeb ("web"). An empty value means "every mode"; any other value
	// is an ERROR, never a silently empty list (a typo'd filter that returns
	// nothing looks exactly like an empty table — Rule 15).
	Mode string
	// Source narrows by the row's recorded PROVENANCE (tasksource vocabulary).
	// An empty value means "every source".
	//
	// ChatSourceUnrecorded is a selectable value rather than a curiosity: it is
	// how the operator asks for the rows that predate the column — the ones
	// nobody can vouch for — and it is the ONLY way to name them, since their
	// stored value is the empty string (Rule 15a: the absent provenance is
	// asked for by its own word, never silently folded into `human`).
	//
	// Any other unrecognized spelling is an ERROR, never an empty list: the
	// filter answers with the rows the caller asked for or it says why not.
	Source string
	// LocalOnly drops the rows whose session LOG is not on this machine — the
	// LOCAL-ONLY view `gopi session list` shows by default (the operator's
	// ruling: the default view lists the sessions this machine really holds;
	// the complete index is one explicit flag away).
	//
	// What it claims is narrow and checkable: these conversations' session
	// files exist HERE. A chats row is an INDEX entry — it records where the
	// conversation WAS — so its log_path can point into another host's profile,
	// a reclaimed scratch root or a fixture db, and listing such a row beside a
	// readable one, with nothing to tell them apart, over-reports what is on
	// the machine (measured 2026-10-05: `session list --mode chat` answered 50
	// rows of which 48 named a file this machine does not have).
	//
	// The zero value is FALSE, and it is not the ruled surface's default: it
	// keeps the render every OTHER caller of this shared renderer already had
	// (`gopi chat list`, the tests). The ruled default lives at the CLI that
	// was ruled on — cmd/agent's sessionListLocalOnly — because the disclosure
	// line names a flag (ChatListAllFlag) the sibling verb does not parse, and
	// a render must never instruct a reader to type a flag its own verb
	// refuses. FilePresent is measured EITHER WAY: with LocalOnly false every
	// row is returned and the absent ones carry ChatFileMissingPrefix, so
	// "shown" and "readable here" stay distinguishable facts.
	LocalOnly bool
	// Limit ≤ 0 means chatListDefaultLimit.
	Limit int
}

func ChatList(f ChatListFilter) (string, error) {
	ctx := context.Background()
	db, err := openChatDB(ctx)
	if err != nil {
		return "", err
	}
	defer db.Close()
	rows, err := chatListFetch(ctx, db, f)
	if err != nil {
		return "", err
	}
	kept, dropped, err := chatListPartition(rows, f.LocalOnly)
	if err != nil {
		return "", err
	}
	note := ""
	if strings.TrimSpace(f.Query) != "" || strings.TrimSpace(f.Model) != "" ||
		strings.TrimSpace(f.Mode) != "" || strings.TrimSpace(f.Source) != "" {
		note = " (filtered)"
	}
	var out strings.Builder
	count := 0
	for _, d := range kept {
		out.WriteString(chatListRowLine(d))
		count++
	}
	// THE COUNT HEADER is emitted on EVERY path, so a drop is never silent
	// (Rule 15). It carries the pre-cut count — the fetch is cut at the limit,
	// so this header describes the view, not the table.
	header := fmt.Sprintf("%d chat(s)%s:", count, note)
	// The disclosure rides the header (one line, no second header): the row
	// count below it and the number here agree, and a view that dropped rows
	// says so in numbers, in the same breath.
	if disclosure := ChatFileHiddenNote(dropped); disclosure != "" {
		header += " " + disclosure
	}
	if count == 0 {
		return chatListEmptyResult + "\n" + header, nil
	}
	return header + "\n" + out.String(), nil
}

// chatListRowLine renders ONE index row — the one line ChatList emits per row,
// and the SAME line the session family's merged listing emits for a chat row
// (internal/tools/sessionruns_render.go). It is a function rather than an inline
// Fprintf so the two views cannot drift into two formats for one row (Rule 13:
// one definition, referenced — the same reasoning `chatListWhere` carries for
// the filter).
func chatListRowLine(d ChatSessionDetail) string {
	head := d.Title
	if head == "" {
		head = d.FirstMsg
	}
	if head == "" {
		head = chatNoTextWord
	}
	state, ended := sessionStateWord(d.EndedAt, d.EndReason)
	return fmt.Sprintf(chatListRowFormat,
		d.SessionID, d.Mode, ChatSourceWord(d.Source), chatFileMarker(d.FilePresent), head,
		state, orDash(d.Model), chatTurnClause(d), chatTokenClause(d),
		formatChatTime(d.StartedAt), ended)
}

// chatListRowFormat spells the row's shape once: id, mode, source, presence
// marker, head, then the parenthesised facts (Rule 1/13).
const chatListRowFormat = "%s  [%s]  %s  %s%s  (%s, model %s, %s, %s, started %s%s)\n"

// chatSelectedColumns is the projection both faces share — the ONE column list
// (Rule 1b/13), so ChatList and ChatSessionRows cannot start disagreeing about
// which facts a row has.
const chatSelectedColumns = `session_id, mode, source, model, title, first_msg, turn_count,
		input_tokens, output_tokens, cache_tokens, workdir, log_path, git_branch,
		git_repo_root, parent_session_id, end_reason, started_at, last_msg_at, ended_at`

// chatNoTextWord is the head a row with neither a title nor a first message
// renders (one spelling, every face).
const chatNoTextWord = "(no text)"

// ChatNoTextWord is the EXPORTED spelling of the value above, for the faces
// that live outside this package (`gopi session show`, cmd/agent). It is an
// ALIAS, not a second literal — the string still has exactly ONE definition
// site (Rule 1/13/14, precedent TaskStatus* in tasks.go). Rule 14's point is
// that the consumer must READ the owner's value: while this const was
// unexported the cmd layer had no way to reference it and re-typed the string
// (cmd/agent/cli_session.go), which is the mirror the gate
// TestChatNoTextWordIsSingleSourced now refuses.
const ChatNoTextWord = chatNoTextWord

// chatListWhere builds the shared WHERE clause — one filter type, one clause,
// two renderings (Rule 13). A malformed filter (an unknown --mode or --source
// spelling) is a NAMED error here, never an empty result (Rule 15).
func chatListWhere(f ChatListFilter) (string, []any, error) {
	conds := []string{}
	args := []any{}
	if q := strings.TrimSpace(f.Query); q != "" {
		p := "%" + likeEscape(q) + "%"
		conds = append(conds, `(first_msg LIKE ? ESCAPE '\' OR title LIKE ? ESCAPE '\' OR session_id LIKE ? ESCAPE '\')`)
		args = append(args, p, p, p)
	}
	if m := strings.TrimSpace(f.Model); m != "" {
		conds = append(conds, `model = ?`)
		args = append(args, m)
	}
	if m := strings.TrimSpace(f.Mode); m != "" {
		if !validChatMode(m) {
			return "", nil, fmt.Errorf("chat: list: invalid mode %q (want %s|%s)", m, ChatModeCLI, ChatModeWeb)
		}
		conds = append(conds, `mode = ?`)
		args = append(args, m)
	}
	sourceCond, sourceArgs, err := chatSourceFilter(f.Source)
	if err != nil {
		return "", nil, err
	}
	if sourceCond != "" {
		conds = append(conds, sourceCond)
		args = append(args, sourceArgs...)
	}
	if len(conds) == 0 {
		return "", args, nil
	}
	return strings.Join(conds, ` AND `), args, nil
}

// chatListFetch selects and scans the rows one filter matches, newest first —
// the ONE read both faces are built on.
func chatListFetch(ctx context.Context, db *sql.DB, f ChatListFilter) ([]ChatSessionDetail, error) {
	where, args, err := chatListWhere(f)
	if err != nil {
		return nil, err
	}
	limit := f.Limit
	if limit <= 0 {
		limit = chatListDefaultLimit
	}
	q := `SELECT ` + chatSelectedColumns + ` FROM chats`
	if where != "" {
		q += ` WHERE ` + where
	}
	q += ` ORDER BY started_at DESC LIMIT ?`
	args = append(args, limit)
	return chatScanDetails(db.QueryContext(ctx, q, args...))
}

// chatScanDetails decodes a row set into ChatSessionDetail values (the shared
// column decodation; a NULLable timestamp folds to 0 = "absent").
func chatScanDetails(rows *sql.Rows, err error) ([]ChatSessionDetail, error) {
	if err != nil {
		return nil, fmt.Errorf("chat: list: %w", err)
	}
	defer rows.Close()
	var out []ChatSessionDetail
	for rows.Next() {
		var d ChatSessionDetail
		var lastMsg, ended sql.NullFloat64
		if err := rows.Scan(&d.SessionID, &d.Mode, &d.Source, &d.Model, &d.Title, &d.FirstMsg,
			&d.TurnCount, &d.InputTokens, &d.OutputTokens, &d.CacheTokens, &d.Workdir,
			&d.LogPath, &d.GitBranch, &d.GitRepoRoot, &d.ParentSession, &d.EndReason,
			&d.StartedAt, &lastMsg, &ended); err != nil {
			return nil, fmt.Errorf("chat: list: %w", err)
		}
		if lastMsg.Valid {
			d.LastMsgAt = lastMsg.Float64
		}
		if ended.Valid {
			d.EndedAt = ended.Float64
		}
		d.TokensReported = ChatTokensReported(d.TurnCount, d.InputTokens, d.OutputTokens, d.CacheTokens)
		out = append(out, d)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("chat: list: %w", err)
	}
	return out, nil
}

// chatListPartition applies the presence criterion to the rows one filter
// matched — ONCE for both faces (Rule 13: the table render and the --json array
// must agree about which rows are shown and which were dropped).
//
// It stamps FilePresent on EVERY row it looks at, in both views: the fact "this
// machine holds the log" is what the local-only view is a claim about, and it is
// also what the marker on a re-admitted row says — so `--all` measures it too
// rather than asserting a value nobody checked (Rule 15: a field that reports
// `true` because the code took a shortcut is worse than one that is absent).
//
// localOnly=true is the ruled default view: the absent rows are DROPPED, and
// `dropped` counts them so the render can disclose the number (never silently).
// localOnly=false (`--all`) drops nothing — dropped is 0 because nothing was
// hidden, whatever FilePresent says.
//
// The kept rows keep the input order (newest first): the filter narrows, it
// never reorders.
func chatListPartition(rows []ChatSessionDetail, localOnly bool) (kept []ChatSessionDetail, dropped int, err error) {
	sessionsDir, derr := chatSessionsDir()
	if derr != nil {
		// The gopi dir cannot be resolved, so PRESENCE IS UNMEASURABLE. An
		// unmeasurable criterion must not drop every row — an empty list would
		// read as "you have no sessions" (Rule 15) — so the answer is the
		// unfiltered index, with the rows marked present (nothing was measured
		// absent, and a bare `false` would be an unmeasured claim).
		for _, d := range rows {
			d.FilePresent = true
			// The turn count needs the log's ADDRESS, and the address is exactly
			// what this branch could not resolve — so the fact is UNMEASURED and
			// says why, in ITS OWN words: reporting "this machine does not hold
			// the log" here would be a claim nobody established (Rule 15).
			d.TurnsKnown = false
			d.TurnsNote = ChatTurnsDirUnresolvedNote
			// The OPENED count is unmeasured in this branch for the same reason,
			// and 0 here is the field's "nothing was measured" default — the
			// render consults TurnsKnown before printing either number.
			kept = append(kept, d)
		}
		return rows, 0, nil
	}
	// ONE probe for the whole page: the id-named stat paths answer the common
	// case, and the sessions-tree index behind them is built at most once.
	probe := newChatPresenceProbe(sessionsDir)
	for _, d := range rows {
		// The SAME resolution answers both facts — "is the log here?" and "which
		// file do I read the turns from?" — so the turn count is read from the
		// very file the presence criterion found (Rule 14: one criterion, one
		// address; a second, independent search could pick a different layout).
		path, present, perr := probe.resolve(d)
		if perr != nil {
			return nil, 0, perr
		}
		d.FilePresent = present
		counts, known, note := chatMeasuredTurns(path, present)
		d.TurnsOpened, d.Turns, d.TurnsKnown, d.TurnsNote = counts.Opened, counts.Human, known, note
		if present || !localOnly {
			kept = append(kept, d)
			continue
		}
		dropped++
	}
	return kept, dropped, nil
}

// chatPresenceProbe answers "does this machine hold this row's session log?"
// for a whole page of rows, and is built so the expensive half runs at most
// once per page.
//
// WHY THIS SHAPE (measured 2026-10-05). The first implementation asked two
// candidates and required BOTH, and it knew only two file layouts. On the live
// profile that reported 85 of 248 chat rows absent — and 69 of those 85 were
// readable right there at `sessions/<id>/<id>.jsonl` (the run-dir layout, where
// the log sits inside the session's own directory under its own name). A
// presence criterion that reports a readable session as gone is the same defect
// as one that reports a gone session as readable, pointing the other way: the
// operator loses rows they have.
//
// So the criterion is: the row's EXPLICIT log_path when it recorded one, OR any
// of the id-named layouts, OR a record for its id anywhere in the sessions tree
// — joined by OR (a row is readable HERE if any address of it is readable), with
// the tree index consulted only after the two cheap stats miss.
type chatPresenceProbe struct {
	dir string
	// ids is the sessions tree's record index: every record file's session id,
	// keyed flat (a flat `<id>.jsonl` or a run-dir `<uuid>.jsonl`, whose base
	// name IS the id) and by directory (the bare layout's `session.jsonl`,
	// whose containing directory is the id). nil = not built yet.
	//
	// THE VALUE IS THE RECORD'S PATH, not `true`: the TURN reader (chat_turns.go)
	// must read the very file this criterion found, and a name index that only
	// answers "yes" would force a second, independent search for the address —
	// two lookups that can disagree about which layout a row uses.
	ids   map[string]string
	built bool
}

// newChatPresenceProbe returns a probe over one sessions tree. The tree is not
// read here: a page whose rows all hold readable log_paths never pays for it.
func newChatPresenceProbe(sessionsDir string) *chatPresenceProbe {
	return &chatPresenceProbe{dir: sessionsDir}
}

// resolve answers "is this row's log here?" and, when it is, the ADDRESS that
// answered: the path of the session log this machine holds for the row, or
// ok=false.
//
// It is the ONE resolution behind every single-row and whole-page verdict: the
// list scan calls it directly, chatSessionLogFor wraps it, and
// chatSessionLogPresent is a thin read of that wrapper. It exists so the TURN
// reader (chat_turns.go) reads the very file the presence criterion found — one
// criterion, one path, no second guess about which layout a row uses.
//
// There is deliberately no shorter `present` method: a presence-only helper for
// one row was one, and it became dead the moment the show page needed the
// address too — a wrapper nothing calls would keep claiming a caller it does
// not have (Rule 41: a comment asserts what is true, not what was true).
func (p *chatPresenceProbe) resolve(d ChatSessionDetail) (path string, ok bool, err error) {
	if logPath := strings.TrimSpace(d.LogPath); logPath != "" {
		fi, statErr := os.Stat(logPath)
		switch {
		case statErr == nil && fi.Mode().IsRegular():
			return logPath, true, nil
		case statErr == nil, errors.Is(statErr, fs.ErrNotExist):
			// A directory, or no such file: fall through to the id-named
			// layouts — an unreadable log_path is not proof that no log exists.
		default:
			return "", false, fmt.Errorf("chat: list: stat log_path %q: %w", logPath, statErr)
		}
	}
	id := strings.TrimSpace(d.SessionID)
	if id == "" {
		return "", false, nil // neither a pointer nor an id: no log to find
	}
	if p.dir == "" {
		return "", false, fmt.Errorf("chat: list: sessions dir unresolved for session %q", id)
	}
	// The two id-named layouts, as plain stats: the fast path, and the answer
	// for every caller that only has one row (ChatShow).
	for _, cand := range []string{
		filepath.Join(p.dir, id+taskSessionJSONLExt),
		filepath.Join(p.dir, id, taskBareSessionRecordName),
	} {
		if fi, statErr := os.Stat(cand); statErr == nil && fi.Mode().IsRegular() {
			return cand, true, nil
		}
	}
	if err := p.build(); err != nil {
		return "", false, err
	}
	if path, ok := p.ids[id]; ok {
		return path, true, nil
	}
	return "", false, nil
}

// build reads the sessions tree's record index ONCE. A tree that does not exist
// is an empty index (nothing is on this machine, which is a real answer, not an
// error); a tree that cannot be read is a named error.
func (p *chatPresenceProbe) build() error {
	if p.built {
		return nil
	}
	p.built = true
	p.ids = map[string]string{}
	recs, err := sessionlayout.Records(p.dir)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil // no sessions tree at all: nothing is on this machine
		}
		return fmt.Errorf("chat: list: read sessions dir %q: %w", p.dir, err)
	}
	for _, r := range recs {
		base := filepath.Base(r.Path)
		if base == sessionlayout.RecordName {
			p.ids[filepath.Base(r.Dir)] = r.Path // the bare layout names the dir
			continue
		}
		p.ids[strings.TrimSuffix(base, sessionlayout.JSONLSuffix)] = r.Path
	}
	return nil
}

// chatSessionLogFor answers BOTH single-row questions with ONE resolution:
// "does this machine hold this row's log?" AND "which file is it?". It exists
// because a caller that needs both — `session show` does, since it has to read
// the turns out of the very file it just found — must not run the search twice.
// The expensive half of a miss builds a whole sessions-tree index, so two
// searches per row is not a style issue, it is a doubled cost over the family
// of layouts (and two searches could in principle answer two layouts).
//
// One implementation, so a caller with a single row cannot get a different
// verdict from the list view (Rule 14): chatListPartition reaches the same
// probe.resolve, and chatSessionLogPresent is a thin read of THIS function.
//
// The single-row shape still pays for the tree index when the two cheap
// id-named stats miss, because the row may be readable under a task-run dir the
// session id does not name. That is deliberate: an index build answers the
// question CORRECTLY for one row too, and a presence test that guesses "absent"
// to stay cheap is the defect this function exists to remove.
func chatSessionLogFor(d ChatSessionDetail, sessionsDir string) (path string, present bool, err error) {
	return newChatPresenceProbe(sessionsDir).resolve(d)
}

// chatSessionLogPresent answers the presence question for ONE row — the same
// criterion chatPresenceProbe applies to a whole page, reached through the same
// implementation (one probe, one answer: a caller that has a single row must not
// get a different verdict from the list view). It reads chatSessionLogFor, so
// the address and the verdict are never two different searches.
func chatSessionLogPresent(d ChatSessionDetail, sessionsDir string) (bool, error) {
	_, present, err := chatSessionLogFor(d, sessionsDir)
	return present, err
}

// chatTokenClause renders the index row's token fact: the MEASURED counts, or
// the unknown word when no positive usage was ever reported. One criterion decides
// (ChatTokensReported) and one format spells the measured clause, so the index
// cannot render an unmeasured zero the way it renders a real number.
func chatTokenClause(d ChatSessionDetail) string {
	if !d.TokensReported {
		return ChatTokenUsageNote
	}
	return fmt.Sprintf(ChatTokenCountsFormat, d.InputTokens, d.OutputTokens, d.CacheTokens)
}

// chatFileMarker renders the per-row marker a row whose log is NOT on this
// machine carries (`--all`): "the index knows about this conversation" is a
// different fact from "you can read it here", and a reader must be able to tell
// them apart at a glance. A present row carries no marker — the common case
// stays uncluttered, and the marker means exactly one thing.
func chatFileMarker(present bool) string {
	if present {
		return ""
	}
	return ChatFileMissingPrefix
}

// chatSourceFilter translates a ChatListFilter.Source into its WHERE clause.
// It is the ONE place the special value ChatSourceUnrecorded is mapped onto
// the stored empty string, so the two faces (ChatList and ChatSessionRows)
// cannot disagree about which rows "unrecorded" selects. A non-empty spelling
// the provenance vocabulary does not know is an ERROR (Rule 15: a filter that
// matches nothing must not look like an empty index).
func chatSourceFilter(source string) (cond string, args []any, err error) {
	s := strings.TrimSpace(source)
	if s == "" {
		return "", nil, nil
	}
	if s == ChatSourceUnrecorded {
		// No placeholder: the condition is self-contained, and handing SQL a
		// parameter nobody bound is the datatype-mismatch this signature
		// prevents by construction (an args slice per branch, not a bare any).
		return `TRIM(COALESCE(source,'')) = ''`, nil, nil
	}
	value, ok := tasksource.Normalize(s)
	if !ok {
		return "", nil, fmt.Errorf("chat: list: invalid source %q (want %s|%s)", s, ChatSourceUnrecorded, tasksource.Policy())
	}
	return `source = ?`, []any{value}, nil
}

// ChatSourceWord renders one row's provenance for the index: the recorded
// source, or ChatSourceUnrecorded when the row carries none.
//
// The empty case is a WORD rather than a blank or the shared em dash, and
// that is deliberate (Rule 15a): this column answers "did a PERSON hold this
// console?", and a row written before the column existed has no answer to
// give. Printing it as "human" would be a fabricated claim; printing it as a
// blank would read as a rendering gap; ChatSourceUnrecorded says exactly what
// is true — nobody recorded it — so the operator can tell "no provenance was
// captured" from "the provenance was captured and it was not mine".
func ChatSourceWord(source string) string {
	if s := strings.TrimSpace(source); s != "" {
		return s
	}
	return ChatSourceUnrecorded
}

// sessionStateWord is the row's lifecycle word, derived from the SAME two
// columns both renderings read, so the TABLE (ChatList) and the JSON list face
// cannot describe one row in two different words (Rule 13: one definition,
// referenced — the caller renders it, this decides it). endedAt ≤ 0 means the
// conversation has no recorded ending (the detail struct's own "0 = running"
// contract).
func sessionStateWord(endedAt float64, endReason string) (state, endedSuffix string) {
	if endedAt <= 0 {
		return ChatStateRunning, ""
	}
	if endReason == "" {
		return ChatStateEnded, fmt.Sprintf(", ended %s", formatChatTime(endedAt))
	}
	return endReason, fmt.Sprintf(", ended %s", formatChatTime(endedAt))
}

// The row's lifecycle words. Exported: `gopi session list --json` renders the
// SAME word, so a script reading the JSON and a human reading the table are
// looking at one vocabulary rather than two that happen to agree today.
const (
	ChatStateRunning = "running"
	ChatStateEnded   = "ended"
)

// ChatSessionRows is ChatList's READER twin: the same filter, the same ORDER BY,
// the same rows — returned as data instead of as a rendered table. It exists so
// a caller that needs the facts (the `--json` face, a test asserting on a row's
// mode) can have them WITHOUT a second query written somewhere else: one filter
// type, one WHERE clause, one ordering, two renderings. A caller that wants the
// human table calls ChatList; a caller that wants the values calls this. They
// cannot disagree about which sessions matched, because they are the same SQL —
// and they cannot disagree about the presence filter either, because both run
// the SAME chatListPartition: the JSON array carries exactly the rows the table
// showed, each with FilePresent stamped — so a `--json` consumer can tell a row
// it can open from one pointing at a file this machine does not have.
// ChatSessionPage is ChatSessionRows plus the DISCLOSURE number: how many rows
// the view dropped because their session log is not on this machine. It is the
// one implementation behind both reader entry points — a caller that must TELL
// the user what it hid (a CLI render, Rule 15) calls this; a caller that only
// wants the rows calls ChatSessionRows, which delegates here.
//
// hidden is 0 whenever nothing was dropped, including the complete-index case
// (LocalOnly false): a view that hid nothing must not claim it did.
func ChatSessionPage(f ChatListFilter) (rows []ChatSessionDetail, hidden int, err error) {
	ctx := context.Background()
	db, err := openChatDB(ctx)
	if err != nil {
		return nil, 0, err
	}
	defer db.Close()
	matched, err := chatListFetch(ctx, db, f)
	if err != nil {
		return nil, 0, err
	}
	kept, dropped, err := chatListPartition(matched, f.LocalOnly)
	if err != nil {
		return nil, 0, err
	}
	return kept, dropped, nil
}

func ChatSessionRows(f ChatListFilter) ([]ChatSessionDetail, error) {
	rows, _, err := ChatSessionPage(f)
	return rows, err
}

// ChatSessionDetail is one chat row's full fact set — the `gopi session show`
// summary. It is the SAME row ChatList renders (one table, one reader), with
// the columns the index line has no space for: the working directory, the git
// branch/repo the conversation ran against, its token sums, its ending and the
// paths a reader needs to reach the raw material (log_path = the session JSONL,
// nil when the row carries none).
type ChatSessionDetail struct {
	SessionID string
	Mode      string
	// Source is the row's recorded provenance (tasksource vocabulary), or ""
	// when nothing was recorded — rendered as ChatSourceUnrecorded, never as a
	// guessed value. See ChatSession.Source.
	Source    string
	Model     string
	Title     string
	FirstMsg  string
	TurnCount int
	// TurnsOpened is how many turns the session LOG opens — the axis `--turn N`,
	// a replay band (`── turn 30`) and the `--turns` table all number along, so
	// it is the number `session replay` and `chat --replay` print as the total.
	//
	// IT IS THE BARE WORD'S NUMBER, AND THAT IS THE FIX (epic 2026-11-04,
	// measured on session 3a93ac1c-c0a3-448d-a14f-9eb673b64f9f): `session list`
	// printed `turns 29` while `session replay --turns` printed 30 rows and the
	// replay screen ended on `──── turn 30`. The row was printing TurnCount —
	// `chats.turn_count`, which advances only when a turn COMPLETES
	// (cmd/agent/cli_chat.go's `runErr == nil && !interrupted` gate) — and that
	// session's last turn OPENED and then quit, so the ledger holds one fewer
	// than the session has. A count that can sit behind the ordinals a reader
	// types cannot be the total those ordinals belong to.
	TurnsOpened int
	// Turns is the PERSON-ONLY SUBSET of the session's turn count: how many of
	// the turns the ledger charged opened at a row the operator typed
	// (internal/gopilog.SessionTurns, reached through the injected
	// chatSessionTurnsReader — see chat_turns.go for why it cannot be an
	// import).
	//
	// IT IS NOT TurnCount AND IT IS NOT A RIVAL TOTAL FOR IT (the operator's
	// ruling, 2026-10-27). TurnCount is the count itself — `chats.turn_count`,
	// every input face equal — and the bare word `turns` is its alone. This
	// field is a DETAIL a reader can ask for (`session show`'s `human turns`
	// line, printed UNDER the total, never beside it as a second count).
	// Measured on 41e5dade-71e1-4efc-a012-456d0532e762: TurnCount 200, Turns 66.
	Turns int
	// TurnsKnown reports whether Turns is a MEASUREMENT (Rule 15). false means
	// nobody measured it — no counter is installed in this process, the session
	// log is not on this machine, or it could not be read — and the render then
	// states the gap in words instead of printing a number nobody took. It never
	// affects the turn count, which comes from the ledger and is always present.
	TurnsKnown bool
	// TurnsNote says WHY Turns is unmeasured, in words a reader can act on. ""
	// when TurnsKnown.
	TurnsNote string
	// InputTokens / OutputTokens / CacheTokens are the session's RUNNING TOTALS
	// at its last recorded turn — NOT per-request peaks, and not a high-water
	// mark of a single request. The field names say "tokens" only because the
	// COLUMNS are spelled input_tokens/output_tokens/cache_tokens (a schema
	// rename would break every reader for a label's sake).
	// ChatRecordTurnOutcome folds each of the three with MAX(col, ?), whose ONLY
	// job is idempotence: a replayed or out-of-order annotation must not shrink
	// the row. That fold looks like a peak because it is spelled MAX, and it was
	// documented as one for one commit (9f00fd60) — the shape of the SQL is not
	// the shape of the series it folds.
	//
	// WHERE A TRUE PER-REQUEST FIGURE LIVES: the raw wire log —
	// `gopi session raw <id>` prints each request's own usage block. It is NOT
	// in `session stats` or `session log`: both sum this column over a session's
	// rows, and a sum of cumulative readings is not a total (measured: both
	// print 219191 for a session whose true total is 109705). Both are unfixed
	// defects of this same root cause, and neither is a place to send a reader
	// looking for a per-request number; devdoc/ledger-token-columns-are-a-total.md
	// records them.
	//
	// THE PARAGRAPH THAT PRECEDED THIS ONE IN 9f00fd60 SAID THE OPPOSITE, AND
	// THE MEASUREMENT IS WHY IT WAS WRONG (task 01a1954e, 2026-11-01). The three
	// operands the recorder receives are the CostTracker's CUMULATIVE counts —
	// `ag.Cost.TotalPromptTokens`,
	// `TotalCompletionTokens`, `TotalCacheHitTokens+TotalCacheMissTokens`
	// (cmd/agent/cli_chat.go:4584, term.go:417, cli_web.go:1605) — and those
	// fields only ever advance, one increment per API REQUEST
	// (internal/deepseek/claude_client.go:303-307). MAX over a monotonically
	// increasing series returns its LAST element, so the column is the session's
	// running TOTAL at the moment of its last recorded turn — not a peak, and not
	// a high-water mark of a per-request size.
	//
	// MEASURED, not read off the SQL's shape: session
	// d6120d04-98fa-454d-a2f0-51ea7c661956 stored `input_tokens` 109705 while its
	// three wire requests carry prompt sides 36455 / 36576 / 36674 — SUM 109705,
	// MAX 36674. Session 34bb9adc… stored 8086768 against a per-request MAX of
	// 102312. The stored number is the SUM. The MAX's ONLY job is idempotence: a
	// replayed or out-of-order write must not shrink the row. Do NOT "fix" the
	// fold — it is correct — and do not read this column as a per-request figure.
	InputTokens  int
	OutputTokens int
	CacheTokens  int
	// TokensReported says whether the three counts above are a MEASUREMENT.
	// false means the upstream API never returned a usage block for this
	// session, so the counts are the schema's default 0 — reading them as "the
	// session spent nothing" is the silent failure this field removes (Rule
	// 15). It is derived (ChatTokensReported) rather than stored: the fact is
	// "turns happened, no usage was ever folded", and a stored flag could
	// disagree with the columns it describes.
	TokensReported bool
	Workdir        string
	LogPath        string
	GitBranch      string
	GitRepoRoot    string
	ParentSession  string
	EndReason      string
	StartedAt      float64
	LastMsgAt      float64 // 0 = never
	EndedAt        float64 // 0 = still running
	// FilePresent reports whether THIS machine holds the row's session log —
	// the fact a list view is a claim about, and therefore also the fact the
	// `--json` face must carry (a script filtering out rows it cannot open needs
	// the same fact the human reading the table sees).
	//
	// It is filled by the list path (chatListPartition, which is why
	// ChatSessionRows and ChatList can never disagree about it) and by ChatShow
	// for the one row it resolves, so `session show` answers the same question
	// about the session its reader is about to open. It is MEASURED in both
	// views: even --all, which drops nothing, stamps each row from the same
	// probe, so the marker on a row means something (Rule 15).
	FilePresent bool
}

// Running reports whether the conversation has no recorded ending.
func (d ChatSessionDetail) Running() bool { return d.EndedAt <= 0 }

// ChatTokensReported is the ONE criterion behind ChatSessionDetail's
// TokensReported: are a row's token columns a MEASUREMENT, or the schema
// default left behind by a provider that never reported usage?
//
// It is derived from the row rather than stored, and it is an AND of two facts
// that must BOTH hold before a count may be called measured (Rule 15):
//
//   - at least one turn was recorded — the recorder (Agent.RecordChatTurn) is
//     the ONLY writer of the token columns, and a row with no recorded turn has
//     no evidence that any response was ever accounted for;
//   - at least one of the three counts is non-zero — only a POSITIVE usage
//     count moves deepseek.CostTracker, whose totals the recorder folds in
//     (measured: a provider may send a usage object carrying zeros, which
//     absorbs nothing).
//
// A row failing either is "unknown", never "0": printing 0 would assert a
// measured spend for a session nobody measured. (A genuinely zero-spend session
// is NOT expressible from gopi's own data, and this criterion does not pretend
// otherwise — see ChatTokensUnknownWord: the honest third answer is that no
// positive spend was ever reported, not that it was zero.)
func ChatTokensReported(turns, in, out, cache int) bool {
	if turns <= 0 {
		return false
	}
	return in > 0 || out > 0 || cache > 0
}

// ChatShow resolves ONE chat row by its exact session id (the value
// `gopi session list` prints, and the same operand `gopi session log`/`gopi session raw` take
// — that is the whole reason this exists rather than a second id space). An
// unknown id is a NAMED error, never an empty summary (Rule 15).
func ChatShow(sessionID string) (ChatSessionDetail, error) {
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return ChatSessionDetail{}, fmt.Errorf("chat: show: session_id must not be empty")
	}
	ctx := context.Background()
	db, err := openChatDB(ctx)
	if err != nil {
		return ChatSessionDetail{}, err
	}
	defer db.Close()
	var d ChatSessionDetail
	var lastMsg, ended sql.NullFloat64
	err = db.QueryRowContext(ctx, `SELECT session_id, mode, source, model, title, first_msg,
		turn_count, input_tokens, output_tokens, cache_tokens, workdir, log_path,
		git_branch, git_repo_root, parent_session_id, end_reason, started_at,
		last_msg_at, ended_at
		FROM chats WHERE session_id = ?`, sessionID).Scan(
		&d.SessionID, &d.Mode, &d.Source, &d.Model, &d.Title, &d.FirstMsg,
		&d.TurnCount, &d.InputTokens, &d.OutputTokens, &d.CacheTokens,
		&d.Workdir, &d.LogPath, &d.GitBranch, &d.GitRepoRoot, &d.ParentSession,
		&d.EndReason, &d.StartedAt, &lastMsg, &ended)
	if errors.Is(err, sql.ErrNoRows) {
		// The row is absent — but "the index has no row" and "the session does
		// not exist" are different facts, and only the FIRST is established
		// here. Ask the one presence criterion whether this machine holds a log
		// for the id: if it does, the honest answer names that gap and its
		// remedy (measured 2026-10-05: 3435 session logs sit in peer1's own
		// profile with no chats row, and `session show` called every one of
		// them a session that does not exist, while `gopi chat backfill` is the
		// command that indexes them). If it does not, the id is genuinely
		// unknown and the original sentence is exactly right.
		if onMachine, perr := ChatSessionLogOnMachine(sessionID); perr == nil && onMachine {
			return ChatSessionDetail{}, &ChatSessionNotIndexedError{
				SessionID: sessionID,
				NextStep:  ChatSessionNotIndexedNextStep,
			}
		}
		return ChatSessionDetail{}, fmt.Errorf("chat: no session with id %q in the chat index", sessionID)
	}
	if err != nil {
		return ChatSessionDetail{}, fmt.Errorf("chat: show %s: %w", sessionID, err)
	}
	if lastMsg.Valid {
		d.LastMsgAt = lastMsg.Float64
	}
	if ended.Valid {
		d.EndedAt = ended.Float64
	}
	// The presence fact is derived the SAME way the list view derives it (one
	// criterion, one implementation) — so `session show <id>` cannot tell a
	// reader a different story about the file than `session list` told them
	// about the row. An unresolvable directory is a NAMED error, not a
	// `false`: rendering the unmeasured case as "missing" would assert an
	// absence nobody established (Rule 15).
	dir, derr := chatSessionsDir()
	if derr != nil {
		return ChatSessionDetail{}, fmt.Errorf("chat: show %s: %w", sessionID, derr)
	}
	// ONE resolution answers both facts this page needs: is the log here, and
	// which file do the turns come from. The turn count comes from
	// chatMeasuredTurns — the SAME helper the list scan calls — so the detail
	// page cannot report a different number than the row a user just clicked
	// through (Rule 14: one criterion, one implementation, one address).
	//
	// UNMEASURED IS STATED, NEVER RENDERED AS 0 (Rule 15): chatMeasuredTurns
	// returns ok=false plus the sentence that says why (no counter installed, no
	// log on this machine, the log could not be parsed), and renderSessionDetail
	// prints that sentence in place of a number.
	path, present, perr := chatSessionLogFor(d, dir)
	if perr != nil {
		return ChatSessionDetail{}, perr
	}
	d.FilePresent = present
	counts, known, note := chatMeasuredTurns(path, present)
	d.TurnsOpened, d.Turns, d.TurnsKnown, d.TurnsNote = counts.Opened, counts.Human, known, note
	d.TokensReported = ChatTokensReported(d.TurnCount, d.InputTokens, d.OutputTokens, d.CacheTokens)
	return d, nil
}

// formatChatTime renders a unix-seconds float for the index ("2006-01-02
// 15:04" local, — for 0).
func formatChatTime(ts float64) string {
	if ts <= 0 {
		return "—"
	}
	return time.Unix(int64(ts), 0).Format("2006-01-02 15:04")
}

// ChatBackfillOptions tune the legacy-rows migration.
type ChatBackfillOptions struct {
	DryRun  bool // report without writing — INCLUDING at the open layer
	Verbose bool // one line per migrated row in the report
}

// chatBackfillMark is the summary suffix tagging a tasks row whose chat
// facts were imported into the chats table. The ledger is append-only —
// nothing is deleted; the mark makes re-runs skip and `task list` readers
// see the migration happened.
const chatBackfillMark = " — chat: backfilled"

// chatLegacyAutoSummaryPrefix is the [auto] prefix the legacy harness start
// hook prepended (package agent's AutoTaskSummaryPrefix — duplicated here:
// tools cannot import agent, and the legacy rows are matched by literal).
const chatLegacyAutoSummaryPrefix = "[auto] "

// chatBackfillZeroStatsLine is the stats line a run with nothing to migrate
// reports — the same SHAPE the ordinary path prints (`migrated=0 … dryrun=true`)
// so a caller parsing it cannot tell the two zero cases apart by grammar, and
// the human sentence above it is what says WHY (no ledger / no tasks table).
const chatBackfillZeroStatsLine = "migrated=0 skipped(unreadable-log)=0 total=0 dryrun=true"

// chatBackfillZeroStats is the human report of that same run.
const chatBackfillZeroStats = "chat backfill: " + chatBackfillZeroStatsLine + "\n"

// chatBackfillTasksTableName is the ledger table the legacy [auto] rows live
// in, named once: the candidate SELECT and the dry run's existence probe must
// be about the SAME table (Rule 1b/13).
const chatBackfillTasksTableName = "tasks"

// chatIndexOpenDB is the ONE funnel a chat-index face uses to pick its OPEN
// SHAPE from the caller's own read-only flag (Rule 14: one decision, one place).
//
// BOTH chat-index callers that have a dry run resolve their connection here —
// `ChatBackfill` (`chatIndexOpenDB(ctx, opts.DryRun)`) and `ChatRepairFirstMsgs`
// (`chatIndexOpenDB(ctx, dryRun)`) — so a caller cannot reach the writable
// opener on a run that promised not to write. The repair's leg was finished on
// the branch that carries it (01a17a8a): the funnel landed with the defect's
// fix, and the second caller followed in the same change.
func chatIndexOpenDB(ctx context.Context, readOnly bool) (*sql.DB, error) {
	if readOnly {
		return openChatDBReadOnly(ctx)
	}
	return openChatDB(ctx)
}

// ChatBackfill migrates the legacy tasks-table chat rows (the [auto]-shaped
// harness-registered chats from before the分流) into the chats table: facts
// are re-derived from each row's session JSONL (first user message, turn
// count, token sums — the authoritative log). Converted tasks rows are
// marked " — chat: backfilled" (append-only) and skipped on re-runs.
// Returns (statsLine, humanReport, error).
//
// THE DRY RUN'S READ-ONLY PROMISE IS KEPT AT THE OPEN, NOT AT THE UPDATE
// (measured defect, 2026-10-30, task 01a188f7). Guarding the UPDATE alone was
// not "reporting without writing": the LOCK/DDL of a writable open rewrote the
// ledger's WAL on every run of `--dry-run` (forensics in
// openLedgerIndexDBReadOnly). So dryRun now selects the opener, and the flag
// means the same thing at both layers — one flag, one meaning.
func ChatBackfill(opts ChatBackfillOptions) (string, string, error) {
	ctx := context.Background()
	if opts.DryRun && !fileExists(chatDBPath()) {
		// A dry run on a ledger that is not there has nothing to migrate and
		// nothing to create: the writable opener would have BUILT the file, which
		// is exactly the write this mode exists to avoid. Said out loud, not
		// silently — "the ledger does not exist yet" is a different answer from
		// "it exists and carries no legacy rows" (Rule 15).
		report := fmt.Sprintf("chat backfill: %s does not exist yet — nothing to migrate, and a dry run creates nothing\n",
			chatDBPath()) + chatBackfillZeroStats
		return chatBackfillZeroStatsLine, report, nil
	}
	db, err := chatIndexOpenDB(ctx, opts.DryRun)
	if err != nil {
		return "", "", err
	}
	defer db.Close()
	// In the read-only shape nothing ensures the schema, so a ledger whose
	// tasks face was never created answers "no such table" to the scan below.
	// Ask the FACT first (— a probe, not a guess at the error text) and report
	// zero candidates: a migration with nothing to migrate is not a failure.
	if opts.DryRun {
		hasTasks, terr := tableExists(ctx, db, chatBackfillTasksTableName)
		if terr != nil {
			return "", "", fmt.Errorf("chat backfill: probe %s: %w", chatBackfillTasksTableName, terr)
		}
		if !hasTasks {
			report := fmt.Sprintf("chat backfill: the ledger carries no %s table yet — nothing to migrate\n",
				chatBackfillTasksTableName) + chatBackfillZeroStats
			return chatBackfillZeroStatsLine, report, nil
		}
	}
	// Candidates: harness auto rows with a session, not yet marked.
	rows, err := db.QueryContext(ctx, `SELECT task_id, session_id, summary, model, created_at
		FROM `+chatBackfillTasksTableName+`
		WHERE session_id != ''
		  AND summary LIKE '`+chatLegacyAutoSummaryPrefix+`%'
		  AND summary NOT LIKE '%`+chatBackfillMark+`'`)
	if err != nil {
		return "", "", fmt.Errorf("chat backfill: select: %w", err)
	}
	type legacyRow struct {
		taskID, sessionID, summary, model string
		createdAt                         float64
	}
	var legacy []legacyRow
	for rows.Next() {
		var r legacyRow
		if err := rows.Scan(&r.taskID, &r.sessionID, &r.summary, &r.model, &r.createdAt); err != nil {
			rows.Close() //stylegate:ignore read-only cursor at end of iteration; the error carries no state to recover
			return "", "", fmt.Errorf("chat backfill: scan: %w", err)
		}
		legacy = append(legacy, r)
	}
	rowsErr := rows.Err()
	rows.Close() //stylegate:ignore read-only cursor at end of iteration; the error carries no state to recover
	if rowsErr != nil {
		return "", "", fmt.Errorf("chat backfill: rows: %w", rowsErr)
	}

	var report strings.Builder
	migrated, noLog := 0, 0
	report.WriteString(fmt.Sprintf("chat backfill: %d legacy auto row(s) to migrate\n", len(legacy)))
	for _, r := range legacy {
		rec := chatBackfillRecord(r.sessionID, r.summary, r.model, r.createdAt)
		if rec == nil {
			noLog++
			report.WriteString(fmt.Sprintf("  skip %s (session log unreadable, no bare row written — rerun after the log is located)\n", r.sessionID))
			continue
		}
		if opts.Verbose {
			if rec.facts != nil {
				report.WriteString(fmt.Sprintf("  %s [%s] %s ("+sessionevent.CounterUnitTurn+"=%d in=%d out=%d cache=%d)\n",
					rec.SessionID, rec.Mode, rec.FirstMsg, rec.facts.Turns,
					rec.facts.InputTokens, rec.facts.OutputTokens, rec.facts.CacheTokens))
			} else {
				// JSONL unreadable — the row rides on summary-only facts.
				report.WriteString(fmt.Sprintf("  %s [%s] %s (facts: summary-only — log unreadable)\n",
					rec.SessionID, rec.Mode, rec.FirstMsg))
			}
		}
		if opts.DryRun {
			migrated++
			continue
		}
		if err := chatBackfillInsert(ctx, db, rec); err != nil {
			return "", report.String(), err
		}
		if _, err := db.ExecContext(ctx, `UPDATE `+chatBackfillTasksTableName+` SET summary = summary || ?
			WHERE task_id = ?`, chatBackfillMark, r.taskID); err != nil {
			return "", report.String(), fmt.Errorf("chat backfill: mark source row %s: %w", r.taskID, err)
		}
		migrated++
	}
	stats := fmt.Sprintf("migrated=%d skipped(unreadable-log)=%d total=%d dryrun=%v",
		migrated, noLog, len(legacy), opts.DryRun)
	report.WriteString("chat backfill: " + stats + "\n")
	return stats, report.String(), nil
}

// chatBackfillRecord derives one chat row from a legacy tasks row + its
// session JSONL. nil = the session log is unreadable AND the row carries no
// usable facts (the caller reports the skip).
func chatBackfillRecord(sessionID, summary, model string, createdAt float64) *ChatSession {
	summaryText := strings.TrimSpace(strings.TrimPrefix(summary, chatLegacyAutoSummaryPrefix))
	rec := &ChatSession{
		SessionID: sessionID,
		Mode:      ChatModeCLI, // the legacy auto rows were the CLI carrier
		Model:     strings.TrimSpace(model),
	}
	if facts := chatSessionFactsOf(sessionID); facts != nil {
		if facts.FirstMsg != "" {
			rec.FirstMsg = facts.FirstMsg
			rec.FirstMsgSource = facts.FirstMsgSource
		}
		if facts.Model != "" {
			rec.Model = facts.Model
		}
		rec.Workdir = facts.Workdir
		rec.LogPath = facts.LogPath
		rec.facts = facts
	}
	if rec.FirstMsg == "" && chatSummaryIsOperatorOwn(summaryText) {
		// The legacy tasks row's summary, accepted only when it reads as the
		// person's own line. A harness-shaped summary stays out of the head
		// column and the row gets the placeholder instead — migrating a
		// framework sentence into the index is the very defect this repairs.
		rec.FirstMsg = summaryText
		rec.FirstMsgSource = inputsource.Human
	}
	rec.FirstMsg = normalizeChatText(rec.FirstMsg)
	if rec.FirstMsg == "" {
		rec.FirstMsg = ChatFirstMsgPlaceholder
	}
	if rec.FirstMsg == ChatFirstMsgPlaceholder && rec.LogPath == "" {
		return nil // neither the JSONL nor the summary says anything usable
	}
	return rec
}

// chatBackfillInsert writes ONE migrated row (the facts path): the numeric
// columns come from the JSONL facts; started_at falls back to the tasks
// row's created_at when the log is unreadable.
func chatBackfillInsert(ctx context.Context, db *sql.DB, rec *ChatSession) error {
	facts := rec.facts
	startedAt := float64(0)
	lastMsgAt := sql.NullFloat64{}
	turns, inTok, outTok, cacheTok := 0, 0, 0, 0
	if facts != nil {
		turns = facts.Turns
		inTok = facts.InputTokens
		outTok = facts.OutputTokens
		cacheTok = facts.CacheTokens
		if !facts.Started.IsZero() {
			startedAt = float64(facts.Started.Unix())
		}
		if facts.LastMsgAt > 0 {
			lastMsgAt = sql.NullFloat64{Float64: facts.LastMsgAt, Valid: true}
		}
		if facts.EndedAt > 0 {
			// A legacy session with a session_end is a finished chat.
			if _, err := db.ExecContext(ctx, `INSERT INTO chats (
				session_id, mode, model, title, first_msg, turn_count,
				input_tokens, output_tokens, cache_tokens, workdir, log_path,
				started_at, last_msg_at, ended_at, end_reason)
				VALUES (?, ?, ?, '', ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
				ON CONFLICT(session_id) DO UPDATE SET
					turn_count=excluded.turn_count,
					input_tokens=excluded.input_tokens,
					output_tokens=excluded.output_tokens,
					cache_tokens=excluded.cache_tokens,
					log_path=CASE WHEN excluded.log_path != '' THEN excluded.log_path ELSE chats.log_path END,
					ended_at=excluded.ended_at, end_reason=excluded.end_reason`,
				rec.SessionID, rec.Mode, rec.Model, rec.FirstMsg, turns,
				inTok, outTok, cacheTok, rec.Workdir, rec.LogPath,
				startedAt, lastMsgAt, facts.EndedAt, chatEndNormal); err != nil {
				return fmt.Errorf("chat backfill: insert %s: %w", rec.SessionID, err)
			}
			return nil
		}
	}
	if startedAt == 0 {
		startedAt = float64(time.Now().Unix()) // last resort — the migration moment
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO chats (
		session_id, mode, model, title, first_msg, turn_count,
		input_tokens, output_tokens, cache_tokens, workdir, log_path,
		started_at, last_msg_at)
		VALUES (?, ?, ?, '', ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(session_id) DO UPDATE SET
			turn_count=excluded.turn_count,
			input_tokens=excluded.input_tokens,
			output_tokens=excluded.output_tokens,
			cache_tokens=excluded.cache_tokens,
			log_path=CASE WHEN excluded.log_path != '' THEN excluded.log_path ELSE chats.log_path END`,
		rec.SessionID, rec.Mode, rec.Model, rec.FirstMsg, turns,
		inTok, outTok, cacheTok, rec.Workdir, rec.LogPath,
		startedAt, lastMsgAt); err != nil {
		return fmt.Errorf("chat backfill: insert %s: %w", rec.SessionID, err)
	}
	return nil
}

// chatSessionFacts is one session JSONL's extracted facts (backfill input).
type chatSessionFacts struct {
	FirstMsg string
	// FirstMsgSource is the FACE ChatSessionFactsOf read FirstMsg from
	// (internal/inputsource), or "" when no operator row exists at all — in
	// which case FirstMsg is empty and the repair writes
	// ChatFirstMsgPlaceholder rather than a framework sentence.
	FirstMsgSource string
	Turns          int
	InputTokens    int
	OutputTokens   int
	CacheTokens    int
	Workdir        string
	LogPath        string
	Model          string
	Started        time.Time
	LastMsg        time.Time
	EndedSec       float64
	LastMsgAt      float64
	EndedAt        float64
}

// chatSessionFactsOf summarizes one session JSONL. nil = file unreadable.
// Written as a raw-line scan rather than importing gopilog: gopilog imports
// internal/agent (formatter.go), so tools→gopilog would close an import
// cycle — the scan reads the same events (session_start / user / request /
// session_end) gopilog's processEvents consumes, field-for-field.
//
// THE ADDRESS COMES FROM THE ONE RESOLVER (chatSessionLogFor), not from
// chatFindSessionJSONL — and that is a measured correction, not tidiness. The
// flat-only lookup answered "" for every row on a profile using the BARE layout
// (`sessions/<id>/session.jsonl`: 544 dirs, 0 flat files measured 2026-10-30),
// which made the first repair run report 29 rows as "this machine does not hold
// the session log" while the logs sat there. A reader that cannot find a log it
// has is the same class of defect as one that reports a missing log as present:
// both turn a fact into a guess. One resolver, three layouts, used by the list
// page, `session show` and this scan alike (Rule 14).
func chatSessionFactsOf(sessionID string) *chatSessionFacts {
	dir, err := chatSessionsDir()
	if err != nil {
		return nil
	}
	path, present, err := chatSessionLogFor(ChatSessionDetail{SessionID: sessionID}, dir)
	if err != nil || !present || path == "" {
		return nil
	}
	f, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer f.Close()
	st := &chatSessionFacts{LogPath: path}
	type chatJSONLEvent struct {
		Type      string `json:"type"`
		Timestamp string `json:"timestamp"`
		Model     string `json:"model"`
		Workdir   string `json:"workdir"`
		// Source is the FACE a `user` row was recorded with (internal/inputsource,
		// internal/gopilog.RawEvent's own wire key). Read here for the same reason
		// the reader reads it: a `gopi notify` delivery is a complete input whose
		// TEXT is the framework's own sentence, so provenance — not text — is what
		// tells the operator's first line from an injected one.
		Source  string `json:"source"`
		Message struct {
			Role    string          `json:"role"`
			Content json.RawMessage `json:"content"`
		} `json:"message"`
		PromptTokens     int     `json:"prompt_tokens"`
		CacheHit         int     `json:"cache_hit"`
		CompletionTokens int     `json:"completion_tokens"`
		ElapsedSec       float64 `json:"elapsed_sec"`
		ExitCode         int     `json:"exit_code"`
	}
	firstUser := ""
	firstUserSource := ""
	turns := 0
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), chatJSONLMaxLineBytes) // 16MiB line cap (Rule 1: named cap)
	for sc.Scan() {
		line := sc.Bytes()
		if len(line) == 0 {
			continue
		}
		var ev chatJSONLEvent
		if err := json.Unmarshal(line, &ev); err != nil {
			continue // a torn/unparseable line never kills the migration
		}
		switch ev.Type {
		case sessionevent.EventTypeSessionStart:
			if ev.Model != "" {
				st.Model = ev.Model
			}
			if ev.Workdir != "" {
				st.Workdir = ev.Workdir
			}
			chatParseTS(ev.Timestamp, &st.Started)
		case "user":
			msg := chatUserText(ev.Message.Content)
			// THE FIRST OPERATOR LINE, not the first user row: a delivery's
			// carrier is a user row too, and its text is the framework's own
			// sentence (chatLineIsOperatorOwn decides, in ONE place shared with
			// the live write path).
			if msg != "" && firstUser == "" && chatLineIsOperatorOwn(msg, ev.Source) {
				firstUser = msg
				firstUserSource = normalizeSourceOrHuman(ev.Source)
			}
			turns++
		case "request":
			st.InputTokens += ev.PromptTokens
			st.OutputTokens += ev.CompletionTokens
			st.CacheTokens += ev.CacheHit
			chatParseTS(ev.Timestamp, &st.LastMsg)
		case sessionevent.EventTypeSessionEnd:
			st.EndedSec = ev.ElapsedSec
		}
	}
	st.FirstMsg = firstUser
	st.FirstMsgSource = firstUserSource
	st.Turns = turns
	if !st.Started.IsZero() {
		endAt := st.Started
		if st.EndedSec > 0 {
			endAt = st.Started.Add(time.Duration(st.EndedSec * float64(time.Second)))
			st.EndedAt = float64(endAt.Unix())
		}
		if !st.LastMsg.IsZero() {
			st.LastMsgAt = float64(st.LastMsg.Unix())
		} else if st.EndedSec > 0 {
			st.LastMsgAt = float64(endAt.Unix())
		}
	}
	return st
}

// chatJSONLMaxLineBytes caps one scanned JSONL line (a session file can
// carry huge single-line request payloads).
const chatJSONLMaxLineBytes = 16 * 1024 * 1024

// chatParseTS parses an RFC3339 timestamp into *dst (no-op on empty/malformed).
func chatParseTS(ts string, dst *time.Time) {
	if t, err := time.Parse(time.RFC3339, ts); err == nil {
		*dst = t
	}
}

// chatUserText extracts a user event's text: a JSON string body, or the
// first text block of a content array.
func chatUserText(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		return s
	}
	var blocks []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	}
	if err := json.Unmarshal(raw, &blocks); err == nil {
		for _, b := range blocks {
			if b.Type == "text" && strings.TrimSpace(b.Text) != "" {
				return b.Text
			}
		}
	}
	return ""
}

// chatSessionsDir resolves the sessions dir under the gopi data root
// (GOPI_HOME aware — the GopiDir resolution, not os.UserHomeDir).
func chatSessionsDir() (string, error) {
	return config.SessionsRoot()
}

// chatFindSessionJSONL locates a session file by its FULL id (exact match
// only). It used to accept a prefix as well — the FindSession convention it was
// mirrored from — but a prefix names more than one session and the FIRST hit
// won silently, so a chat turn could be attributed to another session's log
// (epic full-id-no-prefix, 2026-09-23). The full id is what callers hold: the
// chat row's session_id column.
//
// "" means "no such session", which is also the answer for a truncated id — a
// chat row's session id never arrives truncated, so a prefix here is a caller
// bug, and silently picking a file would hide it rather than surface it.
func chatFindSessionJSONL(dir, id string) string {
	if id == "" {
		return ""
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return ""
	}
	for _, e := range entries {
		if strings.TrimSuffix(e.Name(), taskSessionJSONLExt) == id {
			return filepath.Join(dir, e.Name())
		}
	}
	return ""
}
