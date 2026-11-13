package tools

import (
	"context"
	"strings"
	"testing"

	"github.com/nick/gopi/internal/tasksource"
)

// chat_source_test.go — the regression guard for the defect measured
// 2026-10-03: verification/replay runs drove the REAL `gopi chat` binary in the
// OPERATOR's own profile, and every one of them left an ordinary chats row that
// `session list --mode chat` rendered exactly like a conversation the operator
// had himself ("Say the single word: pong. Do not call any tool." ×21).
//
// The fix is provenance, not deletion: the row records WHO held the console,
// `session list` renders it, and the rows nobody can vouch for say so in their
// own word. These tests pin both halves — the labelling AND the fact that the
// labelling never becomes hiding.

// seedChatSourceRow writes one chats row with an explicit provenance.
func seedChatSourceRow(t *testing.T, sessionID, source, firstMsg string) {
	t.Helper()
	if _, err := ChatEnsureRow(ChatSession{
		SessionID: sessionID, Mode: ChatModeCLI, Source: source,
		Model: "deepseek-flash", FirstMsg: firstMsg,
	}); err != nil {
		t.Fatalf("ensure %s: %v", sessionID, err)
	}
}

func TestChatSourceDistinguishesAVerificationRunFromTheOperator(t *testing.T) {
	chatTestEnv(t)
	seedChatSourceRow(t, "chat-human-0001", "human", "what did we decide about the merger?")
	seedChatSourceRow(t, "chat-agent-0002", "agent", "Say the single word: pong. Do not call any tool.")

	// 1) The operator's UNFILTERED index still shows the verification row —
	//    provenance distinguishes, it does not hide (the whole point: an
	//    operator must be able to see that verification traffic exists).
	all, err := ChatList(ChatListFilter{Mode: ChatModeCLI})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if !strings.Contains(all, "chat-agent-0002") || !strings.Contains(all, "chat-human-0001") {
		t.Fatalf("unfiltered list dropped a row: %q", all)
	}
	if !strings.Contains(all, "human") || !strings.Contains(all, "agent") {
		t.Fatalf("unfiltered list does not carry the provenance: %q", all)
	}

	// 2) The verification row is NOT in the operator's own list. This is the
	//    acceptance criterion: run a verification chat, then read the human
	//    list — it does not appear there.
	human, err := ChatList(ChatListFilter{Mode: ChatModeCLI, Source: tasksource.Human})
	if err != nil {
		t.Fatalf("list human: %v", err)
	}
	if !strings.Contains(human, "chat-human-0001") {
		t.Fatalf("the operator's own row is missing from --source human: %q", human)
	}
	if strings.Contains(human, "chat-agent-0002") {
		t.Errorf("a verification run still appears in the operator's list: %q", human)
	}

	// 3) …and the verification row is still FINDABLE by its own provenance, so
	//    "not in the human list" is a filter, never a disappearance.
	agent, err := ChatList(ChatListFilter{Mode: ChatModeCLI, Source: "agent"})
	if err != nil {
		t.Fatalf("list agent: %v", err)
	}
	if !strings.Contains(agent, "chat-agent-0002") || strings.Contains(agent, "chat-human-0001") {
		t.Errorf("--source agent selects the wrong rows: %q", agent)
	}

	// 4) The JSON face carries the same fact as the table face.
	rows, err := ChatSessionRows(ChatListFilter{Mode: ChatModeCLI, Source: "agent"})
	if err != nil {
		t.Fatalf("rows: %v", err)
	}
	if len(rows) != 1 || rows[0].Source != "agent" || rows[0].SessionID != "chat-agent-0002" {
		t.Errorf("ChatSessionRows source row = %+v", rows)
	}
}

func TestChatSourceIsNeverInferredForARowThatRecordedNone(t *testing.T) {
	chatTestEnv(t)
	// A row that carries NO provenance (the shape every row written before the
	// column existed has) must render as ChatSourceUnrecorded — never as
	// "human". "Nothing was recorded" is not evidence that a person typed it
	// (Rule 15a: the zero value must not be silently promoted to a claim).
	seedChatSourceRow(t, "chat-unrecorded-0003", "", "an old conversation")

	all, err := ChatList(ChatListFilter{Mode: ChatModeCLI})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if !strings.Contains(all, ChatSourceUnrecorded) {
		t.Errorf("a source-less row did not render as %q: %q", ChatSourceUnrecorded, all)
	}
	if strings.Contains(all, "human") {
		t.Errorf("a source-less row was rendered with a provenance nobody recorded: %q", all)
	}

	// The unrecorded rows are askable FOR BY NAME — that is the only way to
	// reach them, since their stored value is the empty string.
	viaWord, err := ChatList(ChatListFilter{Mode: ChatModeCLI, Source: ChatSourceUnrecorded})
	if err != nil {
		t.Fatalf("list %s: %v", ChatSourceUnrecorded, err)
	}
	if !strings.Contains(viaWord, "chat-unrecorded-0003") {
		t.Errorf("--source %s did not select the source-less row: %q", ChatSourceUnrecorded, viaWord)
	}
	viaHuman, err := ChatList(ChatListFilter{Mode: ChatModeCLI, Source: tasksource.Human})
	if err != nil {
		t.Fatalf("list human: %v", err)
	}
	if strings.Contains(viaHuman, "chat-unrecorded-0003") {
		t.Errorf("--source human selected a row whose provenance was never recorded: %q", viaHuman)
	}
}

func TestChatSourceRejectsAnUnknownSpelling(t *testing.T) {
	chatTestEnv(t)
	seedChatSourceRow(t, "chat-human-0004", "human", "hello")
	// A typo'd filter must fail loudly: an unknown provenance returning an
	// empty list looks exactly like "the operator has no sessions" (Rule 15).
	if _, err := ChatList(ChatListFilter{Source: "person"}); err == nil {
		t.Error("an unknown --source spelling was accepted")
	} else if !strings.Contains(err.Error(), "invalid source") {
		t.Errorf("unknown-source error does not name the problem: %v", err)
	}
	if _, err := ChatSessionRows(ChatListFilter{Source: "person"}); err == nil {
		t.Error("ChatSessionRows accepted an unknown --source spelling")
	}
	// An unknown source on the WRITE side is refused the same way.
	if _, err := ChatEnsureRow(ChatSession{SessionID: "chat-bad-0005", Mode: ChatModeCLI, Source: "person"}); err == nil {
		t.Error("an invalid source was written into the index")
	}
	// A recognized ALIAS folds to its canonical value (a second row written
	// under the retired spelling must not become a second provenance).
	seedChatSourceRow(t, "chat-legacy-auto-0006", "auto", "hello again")
	rows, err := ChatSessionRows(ChatListFilter{Source: tasksource.Merger})
	if err != nil {
		t.Fatalf("rows: %v", err)
	}
	if len(rows) != 1 || rows[0].SessionID != "chat-legacy-auto-0006" {
		t.Errorf("the legacy alias did not fold into %s: %+v", tasksource.Merger, rows)
	}
	// The vocabulary is case-SENSITIVE (tasksource.Normalize's own contract),
	// so a case variant is an unknown spelling — refused, not silently
	// lower-cased into a provenance the caller did not write.
	if _, err := ChatEnsureRow(ChatSession{SessionID: "chat-bad-0007", Mode: ChatModeCLI, Source: "Human"}); err == nil {
		t.Error("a case-variant spelling was accepted as a provenance")
	}
}

// TestChatSourceColumnIsAddedToAnIndexBuiltBeforeIt is the migration guard: the
// operator's live index already has 222 chat rows and a CREATE TABLE IF NOT
// EXISTS will never touch it, so the column must arrive by ALTER — and the rows
// that predate it must keep reading as unrecorded, not be back-filled with a
// guess.
func TestChatSourceColumnIsAddedToAnIndexBuiltBeforeIt(t *testing.T) {
	chatTestEnv(t)
	ctx := context.Background()
	db, err := openDB(memoryDBPath(), false)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer db.Close()

	// The PRE-FIX shape, verbatim minus the source column.
	if _, err := db.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS chats (
		session_id        TEXT PRIMARY KEY,
		mode              TEXT NOT NULL CHECK (mode IN ('chat','web')),
		model             TEXT DEFAULT '',
		title             TEXT DEFAULT '',
		first_msg         TEXT DEFAULT '',
		turn_count        INTEGER DEFAULT 0,
		input_tokens      INTEGER DEFAULT 0,
		output_tokens     INTEGER DEFAULT 0,
		cache_tokens      INTEGER DEFAULT 0,
		workdir           TEXT DEFAULT '',
		log_path          TEXT DEFAULT '',
		git_branch        TEXT DEFAULT '',
		git_repo_root     TEXT DEFAULT '',
		parent_session_id TEXT DEFAULT '',
		end_reason        TEXT DEFAULT '',
		started_at        REAL DEFAULT 0,
		last_msg_at       REAL DEFAULT 0,
		ended_at          REAL DEFAULT 0
	)`); err != nil {
		t.Fatalf("legacy chats table: %v", err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO chats (session_id, mode, first_msg, started_at)
		VALUES ('chat-legacy-0007', 'chat', 'written before the column existed', 1788000000)`); err != nil {
		t.Fatalf("legacy row: %v", err)
	}
	db.Close() //stylegate:ignore the fixture's own handle, closed so the next ensure reopens and widens the table; the reopened schema is the assertion

	// The next ensure (any chat run) widens the table in place.
	seedChatSourceRow(t, "chat-human-0008", "human", "after the upgrade")

	row, err := ChatShow("chat-legacy-0007")
	if err != nil {
		t.Fatalf("show legacy: %v", err)
	}
	if row.Source != "" {
		t.Errorf("a legacy row acquired a provenance nobody recorded: %q", row.Source)
	}
	if ChatSourceWord(row.Source) != ChatSourceUnrecorded {
		t.Errorf("legacy row renders %q, want %q", ChatSourceWord(row.Source), ChatSourceUnrecorded)
	}
	newRow, err := ChatShow("chat-human-0008")
	if err != nil {
		t.Fatalf("show new: %v", err)
	}
	if newRow.Source != "human" {
		t.Errorf("new row lost its source: %q", newRow.Source)
	}
}
