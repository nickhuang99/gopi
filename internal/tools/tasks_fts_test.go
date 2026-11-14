package tools

import (
	"os"
	"strings"
	"testing"

	"github.com/nickhuang99/gopi/internal/testutil"
)

// ---------------------------------------------------------------------------
// FTS5 index + TaskSearch tests. Every test uses a fresh temp memory.db via
// GOPI_MEMORY_DB (never the real ~/.gopi/memory.db). The FTS5 companions are
// created by ensureTasksSchema inside openTasksDB — so a row inserted through
// the public TaskNew/TaskUpdate API must be visible to TaskSearch.
// ---------------------------------------------------------------------------

func newSearchTestDB(t *testing.T) {
	t.Helper()
	testutil.NewTestDBPath(t)
}

func TestTaskSearchFTSAndTriggers(t *testing.T) {
	newSearchTestDB(t)

	if _, err := TaskNew("fix the nudge bug in heartbeat", "t-100", "sess-abc", ""); err != nil {
		t.Fatalf("TaskNew: %v", err)
	}
	// bug056: the end-hook path updates status and records the stats as a
	// task_events row — the SUMMARY stays the submit-time digest (immutable).
	// The ai/au triggers keep the FTS index in sync with the row, so the
	// ORIGINAL summary is what search sees.
	if _, err := TaskUpdateWithResult("t-100", "completed", "fix the nudge bug in heartbeat — elapsed=1.0s iter=3 tokens=42"); err != nil {
		t.Fatalf("TaskUpdate: %v", err)
	}

	// unicode61 tier: word query over the (immutable) summary.
	out, err := TaskSearch("nudge")
	if err != nil {
		t.Fatalf("TaskSearch: %v", err)
	}
	if !strings.Contains(out, "t-100") || !strings.Contains(out, "fix the nudge bug in heartbeat") || !strings.Contains(out, "completed") {
		t.Errorf("fts tier out = %q, want t-100 + original summary + status", out)
	}
	// The stats digest must NOT leak into the searchable summary (it rides
	// task_events, which has no FTS companion).
	if strings.Contains(out, "tokens=42") {
		t.Errorf("fts tier must not surface the task_events stats digest:\n%s", out)
	}
	// session_id and task_id columns are indexed too.
	for _, q := range []string{"sess-abc", "t-100"} {
		out, err := TaskSearch(q)
		if err != nil || !strings.Contains(out, "t-100") {
			t.Errorf("TaskSearch(%q) = %q, err %v; want hit", q, out, err)
		}
	}
	// CJK trigram tier: ≥3-char CJK substring inside a longer run.
	if _, err := TaskNew("配置网络测试环境并验证连通性", "t-200", "sess-xyz", ""); err != nil {
		t.Fatalf("TaskNew CJK: %v", err)
	}
	out, err = TaskSearch("网络测试")
	if err != nil || !strings.Contains(out, "t-200") {
		t.Errorf("trigram tier = %q, err %v; want t-200 hit", out, err)
	}
	// LIKE fallback tier: 2-char CJK below the trigram lower bound.
	out, err = TaskSearch("网络")
	if err != nil || !strings.Contains(out, "t-200") {
		t.Errorf("like tier = %q, err %v; want t-200 hit", out, err)
	}
}

func TestTaskSearchMissAndEmpty(t *testing.T) {
	newSearchTestDB(t)

	if _, err := TaskNew("unrelated task", "", "", ""); err != nil {
		t.Fatalf("TaskNew: %v", err)
	}
	out, err := TaskSearch("quantum-flux-capacitor")
	if err != nil || !strings.Contains(out, "No tasks matching") {
		t.Errorf("miss = %q, err %v; want No tasks matching", out, err)
	}
	if _, err := TaskSearch("   "); err == nil || !strings.Contains(err.Error(), "must not be empty") {
		t.Errorf("empty query err = %v, want must-not-be-empty", err)
	}
}

func TestTaskSearchQuotesAreLiteral(t *testing.T) {
	newSearchTestDB(t)

	// FTS5 syntax characters must be folded into a literal phrase (ftsQuote)
	// — never interpreted as operators, and never an engine error.
	if _, err := TaskNew(`handle "quoted" input`, "", "", ""); err != nil {
		t.Fatalf("TaskNew: %v", err)
	}
	// The tokens of a syntax-laden query that mirror the summary's token
	// sequence still hit (phrase matching, operators neutralized).
	out, err := TaskSearch(`handle "quoted" input`)
	if err != nil {
		t.Fatalf("TaskSearch with syntax chars: %v", err)
	}
	if !strings.Contains(out, "handle") {
		t.Errorf("out = %q, want literal-phrase hit", out)
	}
	// A pure-injection probe errors nowhere and matches nothing.
	out, err = TaskSearch(`") OR 1=1 --`)
	if err != nil || !strings.Contains(out, "No tasks matching") {
		t.Errorf("injection probe = %q, err %v; want clean miss", out, err)
	}
}

func TestTasksFTSIndexSurvivesReopen(t *testing.T) {
	testutil.NewTestDBPath(t)

	if _, err := TaskNew("deploy the renderer", "t-300", "sess-300", ""); err != nil {
		t.Fatalf("TaskNew: %v", err)
	}

	// Every public call re-opens the DB and re-runs ensureTasksSchema —
	// CREATE ... IF NOT EXISTS must be a no-op on the already-populated
	// external-content index (a rebuild/clobber here would desync the pair
	// and empty the results). Two sequential searches prove it stays sound.
	for i := 0; i < 2; i++ {
		out, err := TaskSearch("renderer")
		if err != nil || !strings.Contains(out, "t-300") {
			t.Fatalf("search #%d = %q, err %v; want t-300 hit after reopen", i+1, out, err)
		}
	}
}

// TestTaskNoDeleteFuncRegression is the append-only guard at the API-surface
// level (complementing the agent-side DELETE-statement grep): the engine must
// expose no delete-capable task entry point. The auto-register work added
// create + update only; a future delete verb would break this test on the
// name alone, before a single row is ever at risk.
func TestTaskNoDeleteFuncRegression(t *testing.T) {
	src, err := os.ReadFile("tasks.go")
	if err != nil {
		t.Fatalf("read tasks.go: %v", err)
	}
	body := string(src)
	for _, name := range []string{"func TaskDelete", "func TaskRemove", "func TaskClear", "func TaskPurge", "func TaskDrop"} {
		if strings.Contains(body, name) {
			t.Errorf("tasks.go declares %s — the ledger must stay append-only (no delete path)", name)
		}
	}
}
