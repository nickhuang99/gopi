package main

// The `status:` line splitter, and the marker it splits at.
//
// WHY IT LIVES IN A FILE OF ITS OWN. Both symbols shipped inside
// cmd/agent/cli_tasks_send_cancel_test.go — the cross-node `task send` test —
// purely because that is the file where the FIRST caller needed them. They were
// never about sending a row: they read the line `gopi task show` prints, and
// four other test files read it the same way (cli_tasks_test.go,
// cli_tasks_reportfacts_seam_test.go, cli_tasks_waitpid_test.go,
// clone_channel_delivery_before_claim_e2e_test.go). When the send test went with
// the cross-node sync family (2026-11-05), a naive `git rm` took the splitter
// with it and those four files stopped compiling — the exact cost Rule 43's
// recipe exists to avoid: the removal judge is the reference DISTRIBUTION, not
// the file name.
//
// THE VALUE IS THE PRODUCTION ONE (Rule 14): it is defined beside the renderer
// that emits it — internal/tools/statusword.go's StatusWordMarkerSep — and this
// alias exists only so the splitter below reads the boundary it is splitting
// at. A second literal here would let the test agree with itself while drifting
// from what `task show` prints; internal/tools' own
// TestEveryHolderMarkerBeginsWithTheSeparator pins the renderer's half.

import (
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/nick/gopi/internal/testutil"
	"github.com/nick/gopi/internal/tools"
)

// statusWordMarkerSep is the separator a measured marker is chained onto the
// status word with (the production constant, aliased — never re-typed).
const statusWordMarkerSep = tools.StatusWordMarkerSep

// statusWord returns the status word a `status:` field STARTS WITH, dropping
// every measured marker chained after it. A field carrying no marker comes back
// unchanged, which is why this is a no-op for the states that measure nothing.
//
// IT ANSWERS THE STATUS WORD, NOT THE WHOLE LINE — the line carries more than
// one reading. `task show` appends the MEASURED holder facts to it
// (` + holder dead (pid N gone)`, internal/tools/taskholder_gone.go's
// taskHolderGoneMarkerFmt, and its `recycled` wording), and a third measurement
// will append a third. Comparing the whole field to a status value therefore
// measures the MARKER logic as a side effect: measured red on 974bc5c, both
// TestTaskSendRefusesANonPendingRow/in_progress and
// TestTaskSendChainRefusesAnIncompleteChainAtTheCommandLine, whose rows were
// in_progress with a holder pid the rig had already reaped.
//
// THOSE TWO NAMES ARE AN OBITUARY, NOT A LIVE CITATION. Both functions existed
// at 974bc5c (cmd/agent/cli_tasks_send_cancel_test.go and
// cli_tasks_send_chain_test.go) and were DELETED with the cross-node
// `gopi task send` CLI surface they exercised — commit 5677cc6ef,
// `remove(task): 第 1 批 —— 删掉 cmd/agent 的 8 个跨节点 sync CLI 入口`. Neither
// file is in this tree, so `go test -run` on either name prints "no tests to
// run" and exits 0, and a reader who pastes one learns nothing. The measurement
// above is history and stands as the REASON the two readings were split; the
// check that keeps the reason from rotting is
// TestStatusWordAnswersTheWordNotTheMarker, below.
//
// The two readings are split here, once (Rule 14) — the same separation a
// caller that wants a marker keeps by asserting on the RAW field instead.
func statusWord(field string) string {
	if i := strings.Index(field, statusWordMarkerSep); i >= 0 {
		return strings.TrimSpace(field[:i])
	}
	return field
}

// statusWordMeasuredPid renders an `in_progress` row whose holder the
// production renderer MEASURES, and returns the `status:` field exactly as
// `task show` prints it. Two shapes, chosen by what the row RECORDS:
//
//	recorded incarnation (proc_pid + a start tick it cannot hold)  → "recycled"
//	the claim stamp's pid alone, provably no such process           → "gone"
//
// The splitter reads the RENDERED field, so a fixture that built the text
// itself would test nothing but its own spelling. The only thing staged here is
// the ROW; the wording comes from internal/tools' own renderer (Rule 14).
func statusWordMeasuredPid(t *testing.T, taskID string, recycled bool) string {
	t.Helper()
	if _, err := tools.TaskSubmit(tools.TaskSubmitOptions{
		Summary: "status-word splitter fixture",
		Payload: "a row whose rendered status line carries a measured marker",
		TaskID:  taskID,
		Source:  "human",
	}); err != nil {
		t.Fatalf("seed submit %s: %v", taskID, err)
	}
	db, err := openRawTasksDB(memoryDBPathForTest(t))
	if err != nil {
		t.Fatalf("open raw db: %v", err)
	}
	now := float64(time.Now().Unix())
	stamp := `UPDATE tasks SET status = ?, claimed_by = ?, claimed_at = ?, created_at = ?, updated_at = ?,
		in_progress_at = ?, proc_pid = ?, proc_start_ticks = ?, proc_cmd = ? WHERE task_id = ?`
	var claimedBy string
	var pid int
	var startTicks int64
	if recycled {
		// This host's own live pid, recorded with an incarnation tick no
		// process can carry: the number is ALIVE but it is not the recorded
		// run, so the ladder answers Recycled and the marker says so.
		pid = os.Getpid()
		startTicks = 1
		claimedBy = statusWordTestHolder(t, pid)
	} else {
		// No recorded incarnation; the claim stamp's pid is the only evidence,
		// and no process holds it — the ladder answers Dead.
		pid = statusWordTestDeadPid(t)
		startTicks = 0
		claimedBy = statusWordTestHolder(t, pid)
	}
	if _, err := db.Exec(stamp, tools.TaskStatusInProgress, claimedBy, now, now, now, now,
		pid, startTicks, "", taskID); err != nil {
		db.Close() //stylegate:ignore the test is already failing and cannot use the handle
		t.Fatalf("stamp measured holder on %s: %v", taskID, err)
	}
	db.Close() //stylegate:ignore process-lifetime handle closed at shutdown; the OS reclaims the descriptor
	return rawStatusField(t, taskID)
}

// TestStatusWordAnswersTheWordNotTheMarker is the splitter's OWN regression
// test. The three sibling files pin its three CALLERS
// (taskStatusOf / taskShowStatus / cloneClaimStatusIs); the splitter they all
// route through had no direct check, so its behaviour was only ever observed
// through a chain of hand-built fixtures. It is pinned here on the wording the
// RENDERER actually emits, in both measured shapes and in the no-op half.
//
// EVERY CASE DOES TWO THINGS, and the first is not decoration. ① proves the
// field really CARRIES the production separator, ② asserts the split. Without
// ① this test would pass the day the renderer stopped marking anything — the
// split would be untested and green (Rule 15: a scan that found nothing must
// not read as a pass), which is the failure the obituary above is about.
func TestStatusWordAnswersTheWordNotTheMarker(t *testing.T) {
	testutil.NewTestDBPath(t)
	cases := []struct {
		name     string
		taskID   string
		recycled bool
		// wantWording is the marker text the renderer emits for this shape,
		// assembled from its OWN format strings (taskHolderGoneMarkerFmt /
		// taskHolderRecycledMarkerFmt, internal/tools/taskholder_gone.go) —
		// the assertion below would otherwise accept any marker at all.
		wantWording string
	}{
		{
			name:        "holder gone",
			taskID:      "01a1ae2f-0000-7000-8000-00000000w001",
			recycled:    false,
			wantWording: "holder dead (pid %s gone)",
		},
		{
			name:        "holder recycled",
			taskID:      "01a1ae2f-0000-7000-8000-00000000w002",
			recycled:    true,
			wantWording: "holder dead (pid %s recycled)",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			field := statusWordMeasuredPid(t, tc.taskID, tc.recycled)
			// ① NON-VACUITY: the rendered field carries the production
			//    separator AND this shape's marker wording.
			if !strings.Contains(field, tools.StatusWordMarkerSep) {
				t.Fatalf("the fixture field %q does not carry the production separator %q — "+
					"the split below would be untested; fix the fixture, not the splitter",
					field, tools.StatusWordMarkerSep)
			}
			pidText := statusWordMeasuredPidText(field)
			if want := fmt.Sprintf(tc.wantWording, pidText); !strings.Contains(field, want) {
				t.Fatalf("the fixture rendered %q, want it to carry %q — the marker's own wording "+
					"is what makes this case distinct from its sibling", field, want)
			}
			// ② THE SPLIT: the word, without the measurement.
			if got := statusWord(field); got != tools.TaskStatusInProgress {
				t.Fatalf("statusWord(%q) = %q, want %q — the splitter is returning the rendered "+
					"field (status word + measured marker) instead of the status word; a caller "+
					"comparing it against a ledger status value then goes red for a MEASUREMENT it "+
					"never asked about", field, got, tools.TaskStatusInProgress)
			}
		})
	}

	// ③ THE NO-OP HALF: a state that measures nothing renders no marker, and
	//    the splitter must return the field UNCHANGED. Without this arm, an
	//    implementation that truncated at some later offset would pass ①②.
	markerless := tools.TaskStatusPending
	if strings.Contains(markerless, tools.StatusWordMarkerSep) {
		t.Fatalf("the fixture word %q carries the separator — it cannot test the no-op half", markerless)
	}
	if got := statusWord(markerless); got != markerless {
		t.Fatalf("statusWord(%q) = %q on a markerless field, want it unchanged", markerless, got)
	}
}

// statusWordMeasuredPidText reads the pid number out of a rendered status
// field, so a case can rebuild the renderer's wording without re-typing it.
func statusWordMeasuredPidText(field string) string {
	open := strings.Index(field, "(pid ")
	rest := field[open+len("(pid "):]
	return strings.TrimSpace(rest[:strings.Index(rest, " ")])
}
