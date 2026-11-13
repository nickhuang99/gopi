package tools

import (
	"context"
	"database/sql"
	"strings"
	"testing"
	"time"

	"github.com/nick/gopi/internal/testutil"
)

// openTestTasksDB opens the seeded DB directly for raw column assertions.
func openTestTasksDB(t *testing.T, path string) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", LedgerDSN(path))
	if err != nil {
		t.Fatalf("open %s: %v", path, err)
	}
	return db
}

// TestTouchLivenessColumns: the heartbeat touches stamp the two liveness
// columns on the in_progress row (and NOT on terminal rows).
func TestTouchLivenessColumns(t *testing.T) {
	path := testutil.NewTestDBPath(t)

	seedTaskRow(t, "sess-1", "[auto] live session")
	h := OpenTaskHeartbeatConn(context.Background())
	if h == nil {
		t.Fatalf("heartbeat conn nil")
	}
	if err := h.ExecTouchTasklist("sess-1", "sess-1", 1000); err != nil {
		t.Fatalf("touch tasklist: %v", err)
	}
	if err := h.ExecTouchLastToolcall("sess-1", "sess-1", 2000); err != nil {
		t.Fatalf("touch toolcall: %v", err)
	}
	h.Close() //stylegate:ignore test closes the heartbeat conn so TaskShow reads through a fresh handle; a close error would surface as a failed reopen below

	row, err := TaskShow("sess-1")
	if err != nil {
		t.Fatalf("show: %v", err)
	}
	if !strings.Contains(row, "sess-1") {
		t.Errorf("row broken:\n%s", row)
	}
	// Verify the raw values via a direct read.
	db := openTestTasksDB(t, path)
	defer db.Close()
	var issued, toolcall sql.NullFloat64
	if err := db.QueryRow(`SELECT tasklist_issued_at, last_toolcall_at FROM tasks WHERE task_id='sess-1'`).
		Scan(&issued, &toolcall); err != nil {
		t.Fatalf("query: %v", err)
	}
	if !issued.Valid || issued.Float64 != 1000 {
		t.Errorf("tasklist_issued_at = %v, want 1000", issued)
	}
	if !toolcall.Valid || toolcall.Float64 != 2000 {
		t.Errorf("last_toolcall_at = %v, want 2000", toolcall)
	}

	// A terminal row is not touched (dead rows keep their timestamps).
	seedTaskRow(t, "sess-2", "[auto] closed session")
	if _, err := TaskUpdate("sess-2", TaskStatusCompleted); err != nil {
		t.Fatalf("close: %v", err)
	}
	h2 := OpenTaskHeartbeatConn(context.Background())
	if err := h2.ExecTouchLastToolcall("sess-2", "sess-2", 9999); err != nil {
		t.Fatalf("touch terminal: %v", err)
	}
	h2.Close() //stylegate:ignore test closes the terminal-row heartbeat conn before reopening to assert the terminal state; a close error surfaces in the reopen
	db2 := openTestTasksDB(t, path)
	defer db2.Close()
	var tc2 sql.NullFloat64
	if err := db2.QueryRow(`SELECT last_toolcall_at FROM tasks WHERE task_id='sess-2'`).Scan(&tc2); err != nil {
		t.Fatalf("query2: %v", err)
	}
	if tc2.Valid {
		t.Errorf("terminal row must not be touched, got %v", tc2.Float64)
	}
}

// TestDisplayLivenessReadsNewestFact pins the ONE surviving reading of the
// row's liveness facts (Rules 1/13). It is the display's question — "how stale
// is this run's tick-tock?" — and the four shapes below are the ones that occur
// for real: no fact at all (a bare registered row), tasklist only (a run
// between batches, i.e. the task-liveness-coverage defect), toolcall only (the
// pre-existing shape), and both.
//
// THIS WAS THE SQL-VERSUS-GO AGREEMENT TEST. Its query-side twin
// (taskLivenessInstantSQL, with created_at as a fallback "heartbeat") fed the
// stale sweep, and both went with that sweep on 2026-11-05: the SQL half could
// never return 0 (created_at is NOT NULL), so it needed a fallback the display
// must never take — a row age rendered as a heartbeat. What is pinned now is
// the half the LEDGER still feeds: a recorded fact wins over created_at even
// when it is older, and a row that recorded NOTHING reads 0 (unknown), never a
// fabricated fresh/stale verdict (Rule 15).
func TestDisplayLivenessReadsNewestFact(t *testing.T) {
	path := testutil.NewTestDBPath(t)
	now := float64(time.Now().Unix())

	shapes := []struct {
		id       string
		tasklist sql.NullFloat64
		toolcall sql.NullFloat64
		created  float64
		want     float64 // 0 = no recorded fact: the display renders "unknown"
	}{
		{id: "t-live-none", created: now - 4000, want: 0},
		{id: "t-live-tasklist", tasklist: sql.NullFloat64{Float64: now - 100, Valid: true}, created: now - 4000, want: now - 100},
		{id: "t-live-toolcall", toolcall: sql.NullFloat64{Float64: now - 200, Valid: true}, created: now - 4000, want: now - 200},
		{id: "t-live-both", tasklist: sql.NullFloat64{Float64: now - 300, Valid: true}, toolcall: sql.NullFloat64{Float64: now - 100, Valid: true}, created: now - 4000, want: now - 100},
		// A young row whose toolcall stamp is ancient: created_at must NOT
		// mask the heartbeat — the stamp is the fact, the creation is the age.
		{id: "t-live-oldstamp", toolcall: sql.NullFloat64{Float64: now - 9000, Valid: true}, created: now - 60, want: now - 9000},
	}

	db := openTestTasksDB(t, path)
	for _, s := range shapes {
		seedTaskRow(t, s.id, "liveness reading "+s.id)
		if _, err := db.Exec(`UPDATE tasks SET created_at = ?, updated_at = ?,
			tasklist_issued_at = ?, last_toolcall_at = ? WHERE task_id = ?`,
			s.created, s.created, s.tasklist, s.toolcall, s.id); err != nil {
			t.Fatalf("seed %s: %v", s.id, err)
		}
	}
	db.Close() //stylegate:ignore fixture handle; the reader below opens its own

	for _, s := range shapes {
		row := taskRowByID(t, s.id)
		if got := taskRecordedLivenessAt(row); got != s.want {
			t.Errorf("%s: recorded liveness = %v, want %v (created_at=%v must NOT be substituted)", s.id, got, s.want, s.created)
		}
		if got, want := taskLivenessKnown(row), s.want > 0; got != want {
			t.Errorf("%s: livenessKnown = %v, want %v — an absence is rendered as an absence", s.id, got, want)
		}
	}
}
