package tools

import (
	"context"
	"database/sql"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nick/gopi/internal/testutil"
)

// M4.11 priority tests (t-1788087991): tags→priority derivation (the ONE
// mapping), ORDER BY priority in the claim path, explicit override (rule ④),
// and the legacy backfill. GOPI_MEMORY_DB per test — never the real db.

func TestTaskTagPriorityMapping(t *testing.T) {
	cases := []struct {
		tags string
		want int
	}{
		{"bug", taskPriorityBug},
		{"bug,urgent", taskPriorityBug},
		{"urgent,bug", taskPriorityBug},
		{"feature", taskPriorityDefault},
		{"P1.5,新能力", taskPriorityDefault},
		{"", taskPriorityDefault},
		{"docs", taskPriorityDocs},
		{"doc-only", taskPriorityDocs},
		{"docs,refactor", taskPriorityDocs},
		// bug beats docs (rule ⑤: a bug is a bug even with docs tag)
		{"bug,docs", taskPriorityBug},
		// substring must NOT match: a token merely CONTAINING bug mid-word
		// is not the bug family; but the bug PREFIX family (bug033, the
		// real ledger's tag convention) DOES count
		{"bugflow", taskPriorityBug},
		{"debugbug", taskPriorityDefault},
		{"bug033", taskPriorityBug},
		{"bug037,cache,glm", taskPriorityBug},
	}
	for _, c := range cases {
		if got := taskTagPriority(c.tags); got != c.want {
			t.Errorf("taskTagPriority(%q) = %d, want %d", c.tags, got, c.want)
		}
	}
}

func TestTaskSubmitDerivesPriorityFromTags(t *testing.T) {
	t.Setenv("GOPI_MEMORY_DB", filepath.Join(t.TempDir(), "memory.db"))

	cases := []struct {
		tags string
		want int
	}{
		{"bug", taskPriorityBug},
		{"feature", taskPriorityDefault},
		{"docs", taskPriorityDocs},
		{"", taskPriorityDefault},
	}
	for i, c := range cases {
		if _, err := TaskSubmit(TaskSubmitOptions{
			Summary: "derived " + c.tags, Payload: "briefing " + c.tags,
			Tags: c.tags, Source: "llm", SessionID: "s1",
		}); err != nil {
			t.Fatalf("submit %d: %v", i, err)
		}
	}
	db, err := openTasksDB(context.Background(), memoryDBPath())
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	rows, err := db.QueryContext(context.Background(),
		// The filer is its own column since the split (2026-10-28): session_id
		// now names the row's RUN session, which a pending row does not have.
		`SELECT task_id, summary, priority FROM tasks WHERE `+taskSubmitterColumnName+` = 's1' ORDER BY summary`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	seen := 0
	for rows.Next() {
		var id, summary string
		var prio int
		if err := rows.Scan(&id, &summary, &prio); err != nil {
			t.Fatal(err)
		}
		want := taskPriorityDefault
		switch {
		case strings.Contains(summary, "bug"):
			want = taskPriorityBug
		case strings.Contains(summary, "docs"):
			want = taskPriorityDocs
		}
		if prio != want {
			t.Errorf("%s (tags=%s): priority=%d, want %d", id, summary, prio, want)
		}
		seen++
	}
	if seen != len(cases) {
		t.Fatalf("expected %d rows, saw %d", len(cases), seen)
	}
}

func TestTaskSubmitExplicitPriorityOverridesTags(t *testing.T) {
	t.Setenv("GOPI_MEMORY_DB", filepath.Join(t.TempDir(), "memory.db"))

	// Rule ④: --priority always wins over the tags derivation.
	for _, tc := range []struct {
		tags string
		prio int
	}{
		{"bug", 5},
		{"docs", 3},
	} {
		if _, err := TaskSubmit(TaskSubmitOptions{
			Summary: "override " + tc.tags, Payload: "briefing",
			Tags: tc.tags, Source: "human", Priority: tc.prio,
		}); err != nil {
			t.Fatalf("submit %s: %v", tc.tags, err)
		}
	}
	db, err := openTasksDB(context.Background(), memoryDBPath())
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	for _, want := range []int{5, 3} {
		var n int
		if err := db.QueryRowContext(context.Background(),
			`SELECT COUNT(*) FROM tasks WHERE priority = ? AND summary LIKE 'override %'`, want).Scan(&n); err != nil || n != 1 {
			t.Errorf("explicit priority %d: count=%d err=%v", want, n, err)
		}
	}
}

func TestTaskNextOrdersByPriority(t *testing.T) {
	t.Setenv("GOPI_MEMORY_DB", filepath.Join(t.TempDir(), "memory.db"))

	// Submit oldest-first a docs row (prio 8), then a feature row (5),
	// then a bug row (1). The claim order must be bug → feature → docs —
	// NOT the insertion (created_at) order.
	for _, tc := range []struct{ summary, tags string }{
		{"low docs work", "docs"},
		{"mid feature work", "feature"},
		{"hot bug fix", "bug"},
	} {
		if _, err := TaskSubmit(TaskSubmitOptions{
			Summary: tc.summary, Payload: "briefing: " + tc.summary,
			Tags: tc.tags, Source: "human",
		}); err != nil {
			t.Fatalf("submit %s: %v", tc.summary, err)
		}
	}
	wantOrder := []string{"hot bug fix", "mid feature work", "low docs work"}
	for _, want := range wantOrder {
		res, err := TaskNext("worker", "")
		if err != nil {
			t.Fatalf("TaskNext: %v", err)
		}
		if res.Empty {
			t.Fatalf("queue empty, wanted %q", want)
		}
		if res.Task.Summary != want {
			t.Fatalf("claim order: got %q, want %q", res.Task.Summary, want)
		}
	}
	res, err := TaskNext("worker", "")
	if err != nil || !res.Empty {
		t.Fatalf("queue should be drained: empty=%v err=%v", res.Empty, err)
	}
}

// The legacy backfill: a pre-priority db (rows at the DEFAULT tier carrying
// the bug tag) must be re-tiered to bug=1 on ensureSchema — and an explicit
// priority must never be touched by the backfill.
func TestTaskPriorityBackfillLegacyBugRows(t *testing.T) {
	testutil.NewTestDBPath(t)

	// First open creates the schema; then simulate legacy rows: rewrite
	// their priority to the default tier as if the column never existed.
	if _, err := TaskSubmit(TaskSubmitOptions{
		Summary: "legacy bug A", Payload: "briefing a", Tags: "bug", Source: "human",
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := TaskSubmit(TaskSubmitOptions{
		Summary: "legacy bug B", Payload: "briefing b", Tags: "bug", Source: "llm", SessionID: "s",
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := TaskSubmit(TaskSubmitOptions{
		Summary: "explicit priority row", Payload: "briefing c", Tags: "bug", Source: "human", Priority: 7,
	}); err != nil {
		t.Fatal(err)
	}
	// Mid-token row: "debug-ui" CONTAINS the word bug but carries no
	// bug-family token, so the backfill must leave it at the docs/default
	// tier — the same population rule the tally uses (taskBugPopulationWhere).
	if _, err := TaskSubmit(TaskSubmitOptions{
		Summary: "legacy debug row", Payload: "briefing d", Tags: "debug-ui", Source: "human",
	}); err != nil {
		t.Fatal(err)
	}
	db, err := openTasksDB(context.Background(), memoryDBPath())
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if _, err := db.ExecContext(ctx,
		`UPDATE tasks SET priority = NULL WHERE summary LIKE 'legacy %'`); err != nil {
		t.Fatal(err)
	}
	db.Close() //stylegate:ignore process-lifetime handle closed at shutdown; the OS reclaims the descriptor

	// Re-open: ensureSchema must backfill only the default-tier bug rows.
	db2, err := openTasksDB(ctx, memoryDBPath())
	if err != nil {
		t.Fatal(err)
	}
	defer db2.Close()
	var n int
	if err := db2.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM tasks WHERE `+taskBugPopulationWhere+
			` AND summary LIKE 'legacy %' AND priority = ?`, taskPriorityBug).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 2 {
		t.Fatalf("legacy bug rows backfilled = %d, want 2", n)
	}
	// The mid-token row must NOT be re-tiered to bug: same token rule as the
	// tally's population query (the two faces of taskBugPopulationWhere).
	if err := db2.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM tasks WHERE summary LIKE 'legacy debug%' AND priority = ?`, taskPriorityBug).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Errorf("mid-token 'debug-ui' row entered the bug tier (%d rows) — token rule violated", n)
	}
	// Explicit 7 untouched: the backfill only rewrites NULL rows, never an
	// explicitly stamped priority (submit always writes one — legacy is
	// the only source of NULL).
	if err := db2.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM tasks WHERE summary LIKE 'explicit priority row' AND priority = 7`).Scan(&n); err != nil || n != 1 {
		t.Errorf("explicit priority row survived backfill: count=%d err=%v", n, err)
	}
	if err := db2.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM tasks WHERE priority IS NULL`).Scan(&n); err != nil || n != 0 {
		t.Errorf("NULL priorities must be backfilled: count=%d err=%v", n, err)
	}
	// E2E: the claim order after backfill must be A then B (created order
	// tiebreak within the bug tier) then the default-tier mid-token row
	// BEFORE the explicit-7 row — the explicit-7 row rides the default tier
	// but its created order puts it last.
	for _, want := range []string{"legacy bug A", "legacy bug B", "legacy debug row", "explicit priority row"} {
		res, err := TaskNext("worker", "")
		if err != nil || res.Empty {
			t.Fatalf("TaskNext %s: empty=%v err=%v", want, res.Empty, err)
		}
		if res.Task.Summary != want {
			t.Fatalf("claim got %q, want %q", res.Task.Summary, want)
		}
	}
	_ = sql.ErrNoRows //stylegate:ignore deliberate reference keeping the import honest while the assertions use their own error checks
}

// TaskShow surfaces the priority line (M4.11 visibility).
func TestTaskShowPriorityLine(t *testing.T) {
	t.Setenv("GOPI_MEMORY_DB", filepath.Join(t.TempDir(), "memory.db"))
	if _, err := TaskSubmit(TaskSubmitOptions{
		Summary: "show me", Payload: "briefing", Tags: "bug", Source: "human",
	}); err != nil {
		t.Fatal(err)
	}
	db, err := openTasksDB(context.Background(), memoryDBPath())
	if err != nil {
		t.Fatal(err)
	}
	var id string
	if err := db.QueryRowContext(context.Background(), `SELECT task_id FROM tasks`).Scan(&id); err != nil {
		t.Fatal(err)
	}
	db.Close() //stylegate:ignore process-lifetime handle closed at shutdown; the OS reclaims the descriptor
	out, err := TaskShow(id)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "priority:       1\n") {
		t.Errorf("TaskShow missing priority line:\n%s", out)
	}
}
