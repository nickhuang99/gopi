package tools

import (
	"os"
	"strconv"
	"strings"
	"testing"

	"github.com/nick/gopi/internal/taskverdict"
)

// ---------------------------------------------------------------------------
// Regression guard for the reframe-telemetry double-count epic (2026-09-10).
//
// Incident: session 01a0898e dispatched task(action=reframe) TWICE (session
// JSONL: 2 tool_use blocks) yet its ledger row carried 22 task_events rows with
// event='reframe' — so `gopi task verdicts`, the anti-bureaucracy meter, read
// pure noise. Mechanism: GOPI_TASK_WRAPPER_ID is exported to the `task run`
// child with cmd.Env and inherited by EVERY descendant, so a nested `go test
// ./internal/agent/` believed it was the wrapper child and wrote its telemetry
// under the wrapper's task_id — in the developer's real ~/.gopi/memory.db.
// Measured: one suite run with an inherited wrapper id = 2 extra rows per run.
//
// Two halves, both guarded here:
//   - the transport half: spawned shells no longer inherit the identity
//     (stripWrapperIdentity → bashChildEnv / JobStart);
//   - the attribution half: every row stamps the dispatching process
//     (origin=<session>/<pid>), so a foreign writer is visible instead of
//     silently inflating the count.
// ---------------------------------------------------------------------------

// TestStripWrapperIdentity_RemovesOnlyWrapperIdentity pins the strip: the two
// identity vars go, everything else (including other GOPI_* config the child
// legitimately needs) stays.
func TestStripWrapperIdentity_RemovesOnlyWrapperIdentity(t *testing.T) {
	in := []string{
		"PATH=/usr/bin",
		TaskWrapperEnv + "=01a0898e-parent",
		TaskChildSessionEnv + "=e0d7e7eb-child",
		TaskExecutorSessionEnv + "=e0d7e7eb-exec",
		"GOPI_MEMORY_DB=/tmp/x/memory.db",
	}
	got, removed := stripWrapperIdentity(in)
	if len(removed) != 2 {
		t.Fatalf("expected the 2 identity vars removed, got %v", removed)
	}
	joined := strings.Join(got, "\n")
	for _, keep := range []string{"PATH=/usr/bin", TaskExecutorSessionEnv + "=e0d7e7eb-exec", "GOPI_MEMORY_DB=/tmp/x/memory.db"} {
		if !strings.Contains(joined, keep) {
			t.Errorf("strip dropped %q (only the wrapper identity may go):\n%s", keep, joined)
		}
	}
	if strings.Contains(joined, TaskWrapperEnv) || strings.Contains(joined, TaskChildSessionEnv+"=") {
		t.Errorf("the wrapper identity survived the strip:\n%s", joined)
	}
}

// TestBashChildEnv_StripsInheritedWrapperIdentity is the incident's transport
// guard at the bash level: a bash tool call from inside a task run must NOT
// hand the wrapper id to the shell (bashChildEnv copies os.Environ otherwise).
func TestBashChildEnv_StripsInheritedWrapperIdentity(t *testing.T) {
	t.Setenv(TaskWrapperEnv, "01a0898e-parent")
	t.Setenv(TaskChildSessionEnv, "e0d7e7eb-child")
	t.Setenv("GOPI_MEMORY_DB", "/tmp/keep-me/memory.db")

	for _, kv := range bashChildEnv() {
		if strings.HasPrefix(kv, TaskWrapperEnv+"=") || strings.HasPrefix(kv, TaskChildSessionEnv+"=") {
			t.Fatalf("bashChildEnv leaked the wrapper-child identity to the shell: %s", kv)
		}
	}
	var kept bool
	for _, kv := range bashChildEnv() {
		if kv == "GOPI_MEMORY_DB=/tmp/keep-me/memory.db" {
			kept = true
		}
	}
	if !kept {
		t.Fatal("bashChildEnv dropped GOPI_MEMORY_DB — the strip must be identity-only")
	}
}

// TestReframeRecord_StampsOriginPerWriter pins the attribution contract: one
// dispatch writes ONE row, and the row names the writing process. Two writers
// that share a task id (the incident's real + nested writer) are then
// distinguishable in the trail instead of summing into one meaningless count.
func TestReframeRecord_StampsOriginPerWriter(t *testing.T) {
	t.Setenv("GOPI_MEMORY_DB", t.TempDir()+"/memory.db")

	if err := TaskReframeRecord("t-incident", taskverdict.Doable, 0, "e0d7e7eb-child"); err != nil {
		t.Fatalf("record with a session origin: %v", err)
	}
	if err := TaskReframeRecord("t-incident", taskverdict.Doable, 0, ""); err != nil {
		t.Fatalf("record without a session origin: %v", err)
	}

	out, err := TaskReframeStats()
	if err != nil {
		t.Fatalf("TaskReframeStats: %v", err)
	}
	if !strings.Contains(out, "t-incident ×2") { // one row per dispatch, no other count
		t.Fatalf("expected exactly 2 rows for 2 dispatches, got:\n%s", out)
	}
	// Provenance: more than one origin under one task is the incident's
	// fingerprint — a count alone cannot say that, the report must.
	if !strings.Contains(out, "2 distinct origins reported under this task") {
		t.Fatalf("verdicts report must flag the multi-origin task:\n%s", out)
	}

	// Every row carries an origin: the session one completed with this
	// process's pid, the session-less one as "no-session/<pid>" (never empty —
	// two writers sharing a task id stay distinguishable).
	db, err := openTasksDB(t.Context(), os.Getenv("GOPI_MEMORY_DB"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	rows, err := db.QueryContext(t.Context(),
		`SELECT COALESCE(detail,'') FROM task_events WHERE task_id='t-incident' ORDER BY id ASC`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var details []string
	for rows.Next() {
		var d string
		if err := rows.Scan(&d); err != nil {
			t.Fatal(err)
		}
		details = append(details, d)
	}
	if len(details) != 2 {
		t.Fatalf("expected one row per dispatch, got %d (%v)", len(details), details)
	}
	pid := strconv.Itoa(os.Getpid())
	if got, want := parseReframeOrigin(details[0]), "e0d7e7eb-child/"+pid; got != want {
		t.Fatalf("row 1 origin = %q, want %q (session completed with this process's pid)", got, want)
	}
	if got, want := parseReframeOrigin(details[1]), taskOriginNoSession+"/"+pid; got != want {
		t.Fatalf("row 2 origin = %q, want %q (a session-less writer is still identifiable)", got, want)
	}
}
