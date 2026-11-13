// task_show_single_status_test.go — THE MINIMAL REPRO for the epic's defect:
// `gopi task show <one id>` prints MORE THAN ONE line beginning `status:`.
//
// THE MEASURED SHAPE (2026-10-29, three real rows on this host, after 0bdf81b4d
// removed the schema crash that had been masking it):
//
//	$ gopi task show 01a184e8-4d91-747d-ab3a-7759d72f4a93 | grep -c '^status:'
//	2
//	$ gopi task show 01a187c3-87e8-75fc-b6ce-2ce736c7d4fa | grep -c '^status:'
//	2
//	$ gopi task show 01a17e76-5ad7-7cfd-964d-9806d54dc7d0 | grep -c '^status:'
//	5
//
// THE ROOT CAUSE IS NOT A LEDGER READ. `taskShowWithDB` renders exactly ONE
// `status:` field, from ONE row, resolved by `taskByID` = `WHERE task_id = ?`
// (an equality, not a LIKE) — a same-prefix sibling can never reach that
// SELECT. The extra lines are AUTHORED TEXT: the row's payload (the briefing),
// the newest event's `detail`, and the bounded task-report body are all
// appended VERBATIM at COLUMN 0, so a briefing that quotes a shell transcript
//
//	$ gopi task status <id>
//	status:      completed                   ← 正常
//
// contributes a line that is byte-indistinguishable from the digest's own
// field. The ledger is right; the RENDERER lets someone else's text wear the
// digest's clothes.
//
// THE THREE SHAPES, all present on the real ledger and all exercised here:
//
//	① a payload whose prose carries a `status:` line under a code fence;
//	② an event `detail` (the `result:` digest / a `verdict:`) that is
//	   multi-line authored prose;
//	③ a task-report body quoted under `task_report:`.
//
// This test drives the REAL render (`taskShowWithDB`) over a fixture ledger and
// counts the lines a `grep -c '^status:'` would count — the epic's own
// acceptance command, spelled the way the operator spells it.
package tools

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nick/gopi/internal/taskreport"
	"github.com/nick/gopi/internal/testutil"
)

// The same-prefix siblings. The fixture deliberately gives THREE rows one
// shared head, so a reader that resolved by prefix (LIKE ?||'%') would render
// three digests and the count would be ≥3 — the shape this test must exclude
// by construction as well as by the equality predicate.
const (
	statusReproRowA = "01a20100-0000-7000-8000-000000000001"
	statusReproRowB = "01a20100-0000-7000-8000-000000000002"
	statusReproRowC = "01a20100-0000-7000-8000-000000000003"
)

// statusReproPayload is the measured ① shape verbatim in structure: a briefing
// that QUOTES a shell transcript, fences and all. Its indented `status:` line
// under the fence is exactly what the real row 01a184e8 carried.
const statusReproPayload = "# 现象\n\n" +
	"`gopi task show` 对任何行直接报 SQL logic error。\n\n" +
	"# 实测\n\n" +
	"```\n" +
	"$ ./bin/gopi task status 01a1848b-b125-763d-b75c-813d3d905df1\n" +
	"status:      completed                   ← 正常\n" +
	"\n" +
	"$ ./bin/gopi task show <any>\n" +
	"SQL logic error: no such column: submitter_session (1)    ← 全线失败\n" +
	"```\n\n" +
	"# 根因\n"

// statusReproEventDetail is the measured ② shape: an event detail that is
// multi-line authored prose beginning with a digest-looking line (the delivery
// proof's own sentence, as the real row 01a17e76 recorded it four times).
const statusReproEventDetail = "status:         completed\n" +
	"verdict:        delivery proof for 01a17de7-fe23-72c1-b4ad-70141607c6ea " +
	"NOT judged: the tip this run produced cannot be named\n"

// countStatusLines is the operator's own instrument: `grep -c '^status:'`.
// It is a plain line scan with NO padding tolerance on purpose — the epic's
// acceptance is stated as `grep -c '^status:'`, and a looser match here would
// pass while the operator's command still printed 2.
func countStatusLines(out string) int {
	n := 0
	for _, ln := range strings.Split(out, "\n") {
		if strings.HasPrefix(ln, "status:") {
			n++
		}
	}
	return n
}

// TestTaskShowPrintsExactlyOneStatusLine is the repro: one id, same-prefix
// siblings present, authored text carrying `status:` lines in all three slots —
// and exactly ONE line at column 0 may begin `status:`.
//
// RED ON THE PRE-FIX TREE: the renderer appends the payload, the event detail
// and the report body at column 0, so the count is 1 (digest) + 1 (payload) +
// 2 (event detail) + 1 (report) = 5, and the failure text quotes the whole
// render with the imposter line's own column position.
func TestTaskShowPrintsExactlyOneStatusLine(t *testing.T) {
	path := testutil.NewTestDBPath(t)
	t.Setenv("GOPI_MEMORY_DB", path)

	// Three rows sharing one 8-char prefix — the sibling shape that makes
	// "several rows for a single id" a thing a reader COULD do. Only row A
	// carries the authored text; B and C are noise for the prefix reader.
	for _, id := range []string{statusReproRowA, statusReproRowB, statusReproRowC} {
		if _, err := TaskSubmit(TaskSubmitOptions{
			Summary: "show renders several status: lines — fixture " + id[len(id)-1:],
			Payload: "sibling row (no authored status: line here)",
			TaskID:  id,
			Source:  "human",
		}); err != nil {
			t.Fatalf("submit %s: %v", id, err)
		}
	}

	ctx := context.Background()
	db := openTestTasksDB(t, path)
	defer db.Close() //stylegate:ignore test fixture handle

	// Row A gets the measured payload verbatim (the ① shape).
	if _, err := db.ExecContext(ctx,
		`UPDATE tasks SET payload = ? WHERE task_id = ?`, statusReproPayload, statusReproRowA); err != nil {
		t.Fatalf("stamp payload: %v", err)
	}
	// …and the measured event detail (the ② shape), written through the SAME
	// writer the real seam uses.
	taskEventWrite(ctx, db, statusReproRowA, taskStatusCompleted, statusReproEventDetail)

	// …and the measured report body (the ③ shape), at the path the reader
	// actually resolves.
	reportDir, err := taskSessionDir(statusReproRowA)
	if err != nil {
		t.Fatalf("session dir: %v", err)
	}
	if err := os.MkdirAll(reportDir, 0o755); err != nil {
		t.Fatalf("mkdir report dir: %v", err)
	}
	const reportBody = "# Task Report: fixture\n\n## Completed\n- nothing (this is a fixture)\n\n" +
		"```\nstatus:         completed\n```\n"
	if err := os.WriteFile(filepath.Join(reportDir, taskreport.FileName), []byte(reportBody), 0o644); err != nil {
		t.Fatalf("seed report: %v", err)
	}

	out, err := taskShowWithDB(ctx, db, statusReproRowA)
	if err != nil {
		t.Fatalf("taskShowWithDB(%s): %v", statusReproRowA, err)
	}

	if got := countStatusLines(out); got != 1 {
		t.Fatalf("`task show %s` prints %d line(s) beginning \"status:\", want EXACTLY 1 — "+
			"authored text is riding at column 0 where a digest field lives.\n"+
			"----- full render (the imposter lines are the ones NOT padded to the label width) -----\n%s",
			statusReproRowA, got, out)
	}

	// The one surviving line is the DIGEST's: it names this row's own status.
	if !strings.Contains(out, "status:         "+TaskStatusPending) {
		t.Errorf("the single status: line must carry the row's own status:\n%s", out)
	}

	// The authored text is still RENDERED — indented, not dropped. A fix that
	// deleted the briefing would satisfy the count and destroy the surface.
	for _, want := range []string{"SQL logic error: no such column: submitter_session", "← 正常"} {
		if !strings.Contains(out, want) {
			t.Errorf("the briefing must still be rendered (indented), missing %q:\n%s", want, out)
		}
	}
}

// TestTaskShowStatusLineCountIsIndependentOfSiblings pins the OTHER half of the
// diagnosis as a regression guard: the count is 1 for EVERY row in the fixture,
// sibling or not. A reader that resolved by prefix would print the same
// multi-row digest for all three; an equality read prints one digest each.
func TestTaskShowStatusLineCountIsIndependentOfSiblings(t *testing.T) {
	path := testutil.NewTestDBPath(t)
	t.Setenv("GOPI_MEMORY_DB", path)

	for _, id := range []string{statusReproRowA, statusReproRowB, statusReproRowC} {
		if _, err := TaskSubmit(TaskSubmitOptions{
			Summary: "sibling " + id,
			Payload: "no authored status: line",
			TaskID:  id,
			Source:  "human",
		}); err != nil {
			t.Fatalf("submit %s: %v", id, err)
		}
	}

	ctx := context.Background()
	db := openTestTasksDB(t, path)
	defer db.Close() //stylegate:ignore test fixture handle

	for _, id := range []string{statusReproRowA, statusReproRowB, statusReproRowC} {
		out, err := taskShowWithDB(ctx, db, id)
		if err != nil {
			t.Fatalf("taskShowWithDB(%s): %v", id, err)
		}
		if got := countStatusLines(out); got != 1 {
			t.Errorf("`task show %s` prints %d status: lines, want 1:\n%s", id, got, out)
		}
		// A sibling's row must not be rendered inside this row's digest: the
		// summary column is unique per fixture row, so its absence is proof
		// the SELECT stayed on one row.
		for _, other := range []string{statusReproRowA, statusReproRowB, statusReproRowC} {
			if other == id {
				continue
			}
			if strings.Contains(out, "sibling "+other) {
				t.Errorf("`task show %s` rendered sibling row %s — the read must be one row:\n%s", id, other, out)
			}
		}
	}
}
