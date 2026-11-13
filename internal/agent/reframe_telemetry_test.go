package agent

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nick/gopi/internal/deepseek"
	"github.com/nick/gopi/internal/taskverdict"
	"github.com/nick/gopi/internal/toolnames"
	"github.com/nick/gopi/internal/tools"
)

// ---------------------------------------------------------------------------
// Regression guard for the reframe-telemetry double-count epic (2026-09-10).
//
// The incident (session 01a0898e): TWO real task(action=reframe) dispatches, 22
// task_events rows with event='reframe'. `gopi task verdicts` — the
// anti-bureaucracy meter — therefore counted 22 "reframes" and the repeat-call
// detector (count > 1 = bureaucracy) was confounded by construction.
//
// Measured mechanism: one in-process dispatch writes exactly ONE row, so the
// extra 20 rows were written by OTHER processes carrying an INHERITED
// GOPI_TASK_WRAPPER_ID (the `task run` child export is inherited by every
// descendant — a nested `go test ./internal/agent/` writes its reframe rows
// into the real ledger under the wrapper's task id). Measured at 2 rows per
// full suite run with the id inherited.
//
// Two guards below: the writer's cardinality + provenance, and the transport
// (a spawned shell must not receive the identity).
// ---------------------------------------------------------------------------

// TestReframeDispatch_WritesExactlyOneRowWithOrigin is the cardinality
// assertion the epic asks for: ONE reframe dispatch -> exactly ONE task_events
// row, and that row says who wrote it.
func TestReframeDispatch_WritesExactlyOneRowWithOrigin(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "memory.db")
	t.Setenv(memoryDBEnv, dbPath)
	t.Setenv(memoryDBEnvLegacy, "")
	t.Setenv("GOPI_EXECUTOR_SESSION_ID", "") // provenance comes from the agent, never the env

	ag := &Agent{Ctx: &Context{}, autoTaskRowID: "t-one-row", autoTaskSessionID: "sess-test"}
	out := ag.handleTaskReframe(reframeValidArgs(taskverdict.Doable))
	if !strings.Contains(out, taskverdict.Doable) {
		t.Fatalf("dispatch reply must carry the verdict, got %q", out)
	}

	rows := readReframeRows(t, dbPath, "t-one-row")
	if len(rows) != 1 {
		t.Fatalf("ONE reframe dispatch must write exactly ONE task_events row, got %d: %v", len(rows), rows)
	}
	wantPrefix := "verdict=" + taskverdict.Doable + " items=0 origin=sess-test/"
	if !strings.HasPrefix(rows[0], wantPrefix) {
		t.Fatalf("row detail = %q, want prefix %q (the dispatching session + pid)", rows[0], wantPrefix)
	}

	// A REPEAT dispatch in the same run is recorded (that IS the loop
	// evidence) — the count must be exactly 2, not more, never fewer.
	ag.handleTaskReframe(reframeValidArgs(taskverdict.Doable))
	if rows := readReframeRows(t, dbPath, "t-one-row"); len(rows) != 2 {
		t.Fatalf("two dispatches must write exactly two rows, got %d: %v", len(rows), rows)
	}
}

// TestBashToolCall_DoesNotPropagateWrapperIdentity is the transport guard end
// to end: a real Run whose bash tool call reads the env must see NO wrapper id,
// so a script/tool/gopi launched from inside a task run can never believe it is
// the wrapper child (that belief is what attributed nested writers' telemetry
// to the wrapper's task id).
func TestBashToolCall_DoesNotPropagateWrapperIdentity(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "memory.db")
	t.Setenv(memoryDBEnv, dbPath)
	t.Setenv(memoryDBEnvLegacy, "")
	t.Setenv(tools.TaskWrapperEnv, "01a0898e-parent") // the inherited identity under test
	t.Setenv(tools.TaskChildSessionEnv, "child-session")

	var reqs []*deepseek.ClaudeRequest
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var decoded deepseek.ClaudeRequest
		_ = json.NewDecoder(r.Body).Decode(&decoded) //stylegate:ignore decodes the request the test's own client sent; the decoded value is the assertion
		reqs = append(reqs, &decoded)
		switch len(reqs) {
		case 1:
			claudeTextPlusToolStream(w, "Probing the child env.", toolnames.ToolBash, map[string]interface{}{
				"command": `printf 'WRAPPER=[%s] CHILD=[%s]' "${GOPI_TASK_WRAPPER_ID:-unset}" "${GOPI_TASK_CHILD_SESSION:-unset}"`,
			})
		default:
			claudeTextStream(w, "Probe done.", "end_turn")
		}
	}))
	defer server.Close()

	ag := &Agent{Client: newWfContractClient(server.URL), Ctx: &Context{}, Model: "deepseek-v4-pro", MaxIterations: 4}
	if _, err := ag.Run("probe the wrapper identity propagation"); err != nil {
		t.Fatalf("Run() unexpected error: %v", err)
	}

	var seen string
	for i := range ag.Ctx.Messages {
		if c := ag.Ctx.Messages[i].Content; strings.Contains(c, "WRAPPER=[") {
			seen = c
		}
	}
	if seen == "" {
		t.Fatal("the bash probe produced no tool result — the guard would be vacuous")
	}
	if !strings.Contains(seen, "WRAPPER=[unset]") || !strings.Contains(seen, "CHILD=[unset]") {
		t.Fatalf("a spawned shell inherited the wrapper-child identity (nested writers would write under the parent row):\n%s", seen)
	}
}

// readReframeRows returns the reframe event details of one task, in id order.
func readReframeRows(t *testing.T, dbPath, taskID string) []string {
	t.Helper()
	db, err := openLedgerForTest(dbPath)
	if err != nil {
		t.Fatalf("open ledger %s: %v", dbPath, err)
	}
	defer db.Close()
	rows, err := db.Query(`SELECT COALESCE(detail,'') FROM task_events WHERE task_id = ? AND event = 'reframe' ORDER BY id ASC`, taskID)
	if err != nil {
		t.Fatalf("query reframe rows: %v", err)
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var d string
		if err := rows.Scan(&d); err != nil {
			t.Fatalf("scan reframe row: %v", err)
		}
		out = append(out, d)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate reframe rows: %v", err)
	}
	return out
}

// openLedgerForTest opens the suite ledger path for a raw count query (the
// package's own driver import — internal/agent tests already use "sqlite").
func openLedgerForTest(path string) (*sql.DB, error) {
	if _, err := os.Stat(path); err != nil {
		return nil, err
	}
	return sql.Open("sqlite", tools.LedgerDSN(path))
}
