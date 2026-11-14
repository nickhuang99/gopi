package tools

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nickhuang99/gopi/internal/sessionevent"
)

// chatTestEnv isolates the real profile: temp GOPI_HOME (sessions dir) +
// temp GOPI_MEMORY_DB (the chats table lands in the same memory db).
func chatTestEnv(t *testing.T) {
	t.Helper()
	t.Setenv("GOPI_HOME", t.TempDir())
	t.Setenv("GOPI_MEMORY_DB", filepath.Join(t.TempDir(), "memory.db"))
	// The tasks ledger's gate (openTasksDB) requires a gopi-owned distilled
	// db — seed the knowledge base shell so backfill's UPDATE on tasks can
	// run through the normal task engine in the same db.
	ctx := context.Background()
	db, err := openDB(memoryDBPath(), false)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer db.Close()
	if err := initKnowledgeBase(ctx, memoryDBPath()); err != nil {
		t.Fatalf("init knowledge base: %v", err)
	}
}

// writeChatSessionJSONL fabricates a session log exercising every event the
// backfill scan reads (session_start / user / request / session_end).
func writeChatSessionJSONL(t *testing.T, sessionID, firstUser string) string {
	t.Helper()
	dir, err := chatSessionsDir()
	if err != nil {
		t.Fatalf("sessions dir: %v", err)
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir sessions: %v", err)
	}
	lines := []string{
		`{"type":"session_start","timestamp":"2026-08-30T08:00:00Z","model":"glm-5.3-flash","sessionId":"` + sessionID + `","workdir":"/tmp/proj"}`,
		`{"type":"user","timestamp":"2026-08-30T08:00:01Z","message":{"role":"user","content":"` + firstUser + `"}}`,
		`{"type":"request","timestamp":"2026-08-30T08:00:05Z","iteration":1,"prompt_tokens":100,"cache_hit":90,"completion_tokens":20}`,
		`{"type":"user","timestamp":"2026-08-30T08:01:00Z","message":{"role":"user","content":"second question"}}`,
		`{"type":"request","timestamp":"2026-08-30T08:01:05Z","iteration":2,"prompt_tokens":300,"cache_hit":280,"completion_tokens":50}`,
		`{"type":"session_end","timestamp":"2026-08-30T08:02:00Z","elapsed_sec":120,"exit_code":0,"total_iterations":2,"total_tokens":470}`,
	}
	path := filepath.Join(dir, sessionID+".jsonl")
	if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0o644); err != nil {
		t.Fatalf("write session jsonl: %v", err)
	}
	return path
}

func TestChatEnsureRecordCloseCycle(t *testing.T) {
	chatTestEnv(t)
	sid := "chat-11111111-aaaa-bbbb-cccc-dddddddddddd"

	// START hook: the row is born running with its first message.
	out, err := ChatEnsureRow(ChatSession{
		SessionID: sid, Mode: ChatModeCLI, Model: "glm-5.3-flash",
		FirstMsg: "fix the login bug", Workdir: "/tmp/proj", LogPath: "/tmp/x.jsonl",
	})
	if err != nil || !strings.Contains(out, sid) {
		t.Fatalf("ensure = %q, err %v", out, err)
	}
	row, err := ChatList(ChatListFilter{})
	if err != nil || !strings.Contains(row, "running") || !strings.Contains(row, "fix the login bug") {
		t.Fatalf("list after ensure = %q, err %v; want running row with first msg", row, err)
	}

	// Per-turn hook ×2: turns bump; cumulative token totals fold monotonically.
	if err := ChatRecordTurn(sid, 100, 20, 90); err != nil {
		t.Fatalf("record turn 1: %v", err)
	}
	if err := ChatRecordTurn(sid, 300, 50, 280); err != nil {
		t.Fatalf("record turn 2: %v", err)
	}
	row2, _ := ChatList(ChatListFilter{}) //stylegate:ignore chat-table read in a fixture assertion; the compared value is the contract and an error yields an empty slice that fails the comparison loudly
	// The index row prints the bare turn word — the LOG'S OPENED COUNT — and the
	// ledger's completed count under its own word (epic 2026-11-04).
	//
	// THIS FIXTURE HAS NO SESSION LOG, so the opened count is UNMEASURED here and
	// the row says so; that is the branch this assertion pins, and the ledger's
	// number must survive it (a column is a measurement even when a scan is not).
	// THE ROW IS UNMEASURED (no session log) so it carries ONE clause: the bare
	// word with its reason. It must NOT borrow the ledger's number beside that
	// reason — measured 2026-11-04, `turns session log not on this machine
	// (completed 18)` reads as a measurement of the same thing.
	if !strings.Contains(row2, sessionevent.CounterUnitTurn+" ") ||
		!strings.Contains(row2, "in=300 out=50 cache=280") {
		t.Errorf("list after turns = %q; want the turn line + folded token maxima", row2)
	}
	if strings.Contains(row2, sessionevent.CounterUnitTurnCompleted) {
		t.Errorf("an UNMEASURED row also carried the ledger's completed clause — that is the ambiguity:\n%s", row2)
	}

	// END hook: ended_at + end_reason=normal; a second close is a no-op.
	if err := ChatCloseSession(sid, chatEndNormal); err != nil {
		t.Fatalf("close: %v", err)
	}
	if err := ChatCloseSession(sid, chatEndInterrupt); err != nil {
		t.Fatalf("second close (idempotent): %v", err)
	}
	row3, _ := ChatList(ChatListFilter{}) //stylegate:ignore chat-table read in a fixture assertion; the compared value is the contract and an error yields an empty slice that fails the comparison loudly
	if !strings.Contains(row3, "normal") || !strings.Contains(row3, "ended") {
		t.Errorf("list after close = %q; want ended/normal — and the first ending kept (no interrupt)", row3)
	}
	if strings.Contains(row3, "interrupt") {
		t.Errorf("second close overwrote the first ending: %q", row3)
	}
}

func TestChatRecordTurnMissingRow(t *testing.T) {
	chatTestEnv(t)
	if err := ChatRecordTurn("no-such-session", 1, 1, 1); err == nil {
		t.Fatal("record turn on a missing row must error (the agent re-ensures on it)")
	}
}

func TestChatEnsureEmptyFirstMsgKeepsStored(t *testing.T) {
	chatTestEnv(t)
	sid := "chat-22222222-aaaa-bbbb-cccc-dddddddddddd"
	if _, err := ChatEnsureRow(ChatSession{SessionID: sid, Mode: ChatModeWeb, FirstMsg: "hello there"}); err != nil {
		t.Fatalf("ensure 1: %v", err)
	}
	// The re-ensure path (missing-row recovery) passes an empty FirstMsg —
	// it must NOT blank the stored one.
	if _, err := ChatEnsureRow(ChatSession{SessionID: sid, Mode: ChatModeWeb}); err != nil {
		t.Fatalf("ensure 2: %v", err)
	}
	row, _ := ChatList(ChatListFilter{}) //stylegate:ignore chat-table read in a fixture assertion; the compared value is the contract and an error yields an empty slice that fails the comparison loudly
	if !strings.Contains(row, "hello there") {
		t.Errorf("re-ensure blanked the stored first_msg: %q", row)
	}
}

func TestChatListQueryAndModelFilters(t *testing.T) {
	chatTestEnv(t)
	for _, s := range []ChatSession{
		{SessionID: "chat-33333333-1", Mode: ChatModeCLI, Model: "glm-5.3-flash", FirstMsg: "kubernetes pods question"},
		{SessionID: "chat-33333333-2", Mode: ChatModeWeb, Model: "deepseek-v4-pro", FirstMsg: "golang generics question"},
	} {
		if _, err := ChatEnsureRow(s); err != nil {
			t.Fatalf("ensure %s: %v", s.SessionID, err)
		}
	}
	//stylegate:ignore chat-table read in a fixture assertion; the compared value is the contract and an error yields an empty slice that fails the comparison loudly
	if row, _ := ChatList(ChatListFilter{Query: "kubernetes"}); !strings.Contains(row, "chat-33333333-1") || strings.Contains(row, "chat-33333333-2") {
		t.Errorf("query filter broken: %q", row)
	}
	//stylegate:ignore chat-table read in a fixture assertion; the compared value is the contract and an error yields an empty slice that fails the comparison loudly
	if row, _ := ChatList(ChatListFilter{Model: "deepseek-v4-pro"}); !strings.Contains(row, "chat-33333333-2") || strings.Contains(row, "chat-33333333-1") {
		t.Errorf("model filter broken: %q", row)
	}
	//stylegate:ignore chat-table read in a fixture assertion; the compared value is the contract and an error yields an empty slice that fails the comparison loudly
	if row, _ := ChatList(ChatListFilter{}); !strings.Contains(row, "2 chat(s)") {
		t.Errorf("unfiltered count wrong: %q", row)
	}
}

func TestChatBackfillMigratesLegacyAutoRows(t *testing.T) {
	chatTestEnv(t)
	logged := "chat-44444444-aaaa-bbbb-cccc-dddddddddddd"
	logPath := writeChatSessionJSONL(t, logged, "what is hermes parity?")

	// Legacy ledger state: two [auto] chat rows (one with a readable JSONL,
	// one without), one already-migrated row, one non-auto (execution) row.
	ctx := context.Background()
	db, err := openDB(memoryDBPath(), false)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer db.Close()
	if err := ensureTasksSchema(ctx, db); err != nil {
		t.Fatalf("ensure tasks schema: %v", err)
	}
	seed := []struct{ taskID, sessionID, summary, model string }{
		{"t-100", logged, "[auto] what is hermes parity?", "glm-5.3-flash"},
		{"t-101", "chat-55555555-lost", "[auto] lost session chat", ""},
		{"t-102", "chat-66666666-done", "[auto] already migrated — chat: backfilled", ""},
		{"t-103", "", "[auto] execution runs have no session here", ""},
	}
	for _, s := range seed {
		if _, err := db.ExecContext(ctx, `INSERT INTO tasks (task_id, summary, session_id, model, created_at, updated_at)
			VALUES (?, ?, ?, ?, 1788000000, 1788000000)`, s.taskID, s.summary, s.sessionID, s.model); err != nil {
			t.Fatalf("seed %s: %v", s.taskID, err)
		}
	}

	stats, report, err := ChatBackfill(ChatBackfillOptions{})
	if err != nil {
		t.Fatalf("backfill: %v (report: %s)", err, report)
	}
	if !strings.Contains(stats, "migrated=2") {
		t.Errorf("stats = %q; want migrated=2 (logged + bare; pre-marked and no-session excluded)", stats)
	}

	// The logged row: facts re-derived from the JSONL (first user message,
	// turns, token sums, workdir, log pointer, ended).
	row, err := ChatList(ChatListFilter{Query: "hermes parity"})
	if err != nil || !strings.Contains(row, logged) {
		t.Fatalf("migrated row missing: %q, err %v", row, err)
	}
	// THE BARE TURN WORD IS UNMEASURED HERE (a hand-written fixture log with no
	// turn counter installed) and the row says so with its reason; the ledger's 2
	// is checked on the per-session faces, not on a one-line index row (epic
	// 2026-11-04).
	for _, want := range []string{"turns ", "in=400", "out=70", "cache=370", "normal"} {
		if !strings.Contains(row, want) {
			t.Errorf("migrated row missing %s: %q", want, row)
		}
	}
	// The bare row: summary-derived first_msg, no stats, still there.
	//stylegate:ignore chat-table read in a fixture assertion; the compared value is the contract and an error yields an empty slice that fails the comparison loudly
	if row2, _ := ChatList(ChatListFilter{Query: "lost session chat"}); !strings.Contains(row2, "chat-55555555-lost") {
		t.Errorf("bare row missing: %q", row2)
	}

	// Source rows are MARKED, not deleted (append-only ledger). Marked rows
	// read back with the suffix on their summary. Count = 2 newly marked
	// (t-100, t-101) + 1 pre-marked seed (t-102).
	listOut, err := TaskList("")
	if err != nil {
		t.Fatalf("task list: %v", err)
	}
	if got := strings.Count(listOut, chatBackfillMark); got != 3 {
		t.Errorf("marked source rows = %d, want 3 (2 migrated + 1 pre-marked): %q", got, listOut)
	}

	// Re-run: the marked rows are skipped — one migration, ever.
	stats2, _, err := ChatBackfill(ChatBackfillOptions{})
	if err != nil {
		t.Fatalf("backfill 2: %v", err)
	}
	if !strings.Contains(stats2, "total=0") {
		t.Errorf("re-run stats = %q; want total=0 (marks skip)", stats2)
	}

	// The JSONL the row points at is the fabricated one (pointer truth).
	facts := chatSessionFactsOf(logged)
	if facts == nil || facts.LogPath != logPath {
		t.Errorf("chatSessionFactsOf(%s) = %+v; want log at %s", logged, facts, logPath)
	}
}

func TestChatSearchIsolationFromTasks(t *testing.T) {
	chatTestEnv(t)
	sid := "chat-88888888-aaaa-bbbb-cccc-dddddddddddd"
	if _, err := ChatEnsureRow(ChatSession{SessionID: sid, Mode: ChatModeCLI, FirstMsg: "kafka consumer lag question"}); err != nil {
		t.Fatalf("ensure: %v", err)
	}
	// The chat-native surface finds it; the TASKS retrieval surface must
	// not see chats at all (different species — the M4.6 ④ retrieval split).
	//stylegate:ignore chat-table read in a fixture assertion; the compared value is the contract and an error yields an empty slice that fails the comparison loudly
	if row, _ := ChatList(ChatListFilter{Query: "kafka consumer lag"}); !strings.Contains(row, sid) {
		t.Errorf("chat list lost the row: %q", row)
	}
	out, err := TaskSearch("kafka consumer lag")
	if err != nil {
		t.Fatalf("task search: %v", err)
	}
	if !strings.Contains(out, "No tasks matching") {
		t.Errorf("task search leaked a chat row: %q", out)
	}
}

func TestChatBackfillDryRunWritesNothing(t *testing.T) {
	chatTestEnv(t)
	sid := "chat-77777777-aaaa-bbbb-cccc-dddddddddddd"
	ctx := context.Background()
	db, err := openDB(memoryDBPath(), false)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer db.Close()
	if err := ensureTasksSchema(ctx, db); err != nil {
		t.Fatalf("ensure tasks schema: %v", err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO tasks (task_id, summary, session_id, created_at, updated_at)
		VALUES ('t-200', '[auto] dry run me', ?, 1788000000, 1788000000)`, sid); err != nil {
		t.Fatalf("seed: %v", err)
	}
	stats, _, err := ChatBackfill(ChatBackfillOptions{DryRun: true})
	if err != nil {
		t.Fatalf("dry-run backfill: %v", err)
	}
	if !strings.Contains(stats, "migrated=1") || !strings.Contains(stats, "dryrun=true") {
		t.Errorf("dry-run stats = %q", stats)
	}
	//stylegate:ignore chat-table read in a fixture assertion; the compared value is the contract and an error yields an empty slice that fails the comparison loudly
	if row, _ := ChatList(ChatListFilter{}); !strings.Contains(row, "No chats found.") {
		t.Errorf("dry run wrote rows: %q", row)
	}
	listOut, _ := TaskList("") //stylegate:ignore task-list read in a fixture assertion; the rows are the contract and an error yields an empty slice that fails loudly
	if strings.Contains(listOut, chatBackfillMark) {
		t.Errorf("dry run marked the source row: %q", listOut)
	}
}
