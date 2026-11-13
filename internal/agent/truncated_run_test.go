package agent

// truncated_run_test.go — the regression guard for the false-success defect:
// a run the ENGINE cut short (wallclock cap / stuck net / iteration budget)
// used to close its ledger row `completed`, because the close-out decided the
// status from the exit code alone and every engine-forced stop exits ZERO.
//
// The live reproduction (real binary against a scripted slow LLM) is in
// devdoc/truncated-run-is-not-completed.md §1; these tests pin the decision in
// the process, where a regression is caught in seconds instead of in the
// ledger.
//
// Flipped claim: these tests assert the OPPOSITE of what this repo asserted
// before the fix — an engine-forced stop is NOT a completion.
// TestAgent_WallclockCap_GracefulFinish (loop_stuck_test.go) still owns the
// unrelated half (the run returns nil error and notifies); this file owns what
// the ROW says afterwards.

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"regexp"
	"strings"
	"testing"

	"github.com/nick/gopi/internal/deepseek"
	"github.com/nick/gopi/internal/runend"
	"github.com/nick/gopi/internal/sessionevent"
	"github.com/nick/gopi/internal/tools"
)

// newTruncationTestAgent wires a REGISTERED auto row onto a live-loop agent
// (real HTTP mock server → real Run loop → real END hook → real ledger row).
// newAutoTaskAgent (taskregister_test.go) supplies the isolated profile +
// ledger; this adds the loop knobs. maxIter is the iteration budget handed to
// the loop (0 = unlimited, so the ENGINE and not the budget ends the run).
func newTruncationTestAgent(t *testing.T, serverURL string, maxIter int) *Agent {
	t.Helper()
	a := newAutoTaskAgent(t)
	t.Setenv(tools.TaskWrapperEnv, "")
	base := newStuckTestAgent(serverURL, maxIter)
	a.Client, a.Ctx, a.Model = base.Client, base.Ctx, base.Model
	a.MaxIterations = maxIter
	a.executeToolFn = func(tc deepseek.ToolCall) string { return "mock result for " + tc.Name }
	a.maybeRegisterSessionTask("long-running task the engine will cut short")
	if !a.autoTaskRegistered {
		t.Fatal("start hook did not register a row — the end hook would be a no-op and this test would read green-blind")
	}
	return a
}

// assertTerminalRowWriteWitness proves the END HOOK PERFORMED THE LEDGER WRITE
// from the run's own record — the acceptance's evidence, and the half the row's
// status alone cannot supply.
//
// Why it is the strong assertion: the defect this epic closes was a hook that
// returned EARLY while the status rule was already correct, so "the write never
// happened" and "the write happened somewhere I did not look" were
// indistinguishable from the transcript. The session JSONL's task_row_closed
// event (sessionevent.EventTypeTaskRowClosed) is written AT the ledger write,
// by the same code path, so its presence with the right row id, status and
// reason can only be produced by the hook reaching `tools.TaskUpdateReason`.
//
// The event type is referenced, never spelled (Rule 13); a missing event FAILS
// rather than skipping, so it cannot degrade into a vacuously green check.
func assertTerminalRowWriteWitness(t *testing.T, a *Agent, wantRowID, wantStatus, wantReason string) {
	t.Helper()
	if a.SessionLog == nil {
		t.Fatal("no session log — the terminal-write witness cannot exist for this run")
	}
	data, err := os.ReadFile(a.SessionLog.Path())
	if err != nil {
		t.Fatalf("read session JSONL %s: %v", a.SessionLog.Path(), err)
	}
	var found []map[string]any
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		var ev map[string]any
		if json.Unmarshal([]byte(line), &ev) != nil {
			continue
		}
		if ev["type"] == sessionevent.EventTypeTaskRowClosed {
			found = append(found, ev)
		}
	}
	if len(found) != 1 {
		t.Fatalf("session JSONL must carry exactly ONE %s event (the end hook's terminal write), got %d — the end-hook path is not proven by the row alone",
			sessionevent.EventTypeTaskRowClosed, len(found))
	}
	ev := found[0]
	if got := ev[taskRowClosedKeyTaskID]; got != wantRowID {
		t.Errorf("%s: task_id = %v, want the run's own row %q", sessionevent.EventTypeTaskRowClosed, got, wantRowID)
	}
	if got := ev[taskRowClosedKeyStatus]; got != wantStatus {
		t.Errorf("%s: status = %v, want %q", sessionevent.EventTypeTaskRowClosed, got, wantStatus)
	}
	if got := ev[taskRowClosedKeyReason]; got != wantReason {
		t.Errorf("%s: reason = %v, want the run's end reason %q", sessionevent.EventTypeTaskRowClosed, got, wantReason)
	}
	if got, ok := ev[taskRowClosedKeyError]; ok {
		t.Errorf("%s: the ledger write reported an error (%v) — a terminal write that failed is not a close-out", sessionevent.EventTypeTaskRowClosed, got)
	}
}

// TestAgent_WallclockCap_TruncatedRunIsNotCompleted: the cap ends a live run
// against a mock that keeps producing varying tool rounds. The ledger row must
// read `failed` with the truncation recorded, NOT `completed`.
func TestAgent_WallclockCap_TruncatedRunIsNotCompleted(t *testing.T) {
	t.Setenv(sessionWallclockEnv, "1")

	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		// Varying tool_use per round: an identical one would trip the
		// toolStreak net and end the run for the WRONG reason.
		stuckToolUseStreamArgs(t, w, "task", fmt.Sprintf(`{"action":"view","query":"tick %d"}`, calls))
	}))
	defer server.Close()

	a := newTruncationTestAgent(t, server.URL, 0)

	result, err := a.Run("long-running task")
	if err != nil {
		t.Fatalf("the cap finishes the run gracefully (nil error), got: %v", err)
	}
	if result == "" {
		t.Fatal("expected the last substantive text as the graceful result")
	}

	row, err := tools.TaskShow(a.autoTaskRowID)
	if err != nil {
		t.Fatalf("TaskShow: %v", err)
	}
	if !regexp.MustCompile(`status:\s+failed`).MatchString(row) {
		t.Fatalf("a run the wallclock cap cut short must NOT close `completed`:\n%s", row)
	}
	if !strings.Contains(row, runend.TruncationMarker) || !strings.Contains(row, runend.WallclockCap.Token()) {
		t.Fatalf("the row must record WHY it stopped (%q %q):\n%s", runend.TruncationMarker, runend.WallclockCap.Token(), row)
	}
	if !strings.Contains(row, "truncated:") {
		t.Fatalf("task show must render the truncation line:\n%s", row)
	}
	// The end-hook PATH is proven by the run's own record, not by the row
	// alone: the JSONL's task_row_closed event is written at the ledger write.
	assertTerminalRowWriteWitness(t, a, a.autoTaskRowID, tools.TaskStatusFailed, runend.WallclockCap.Token())
	// The row is still the resume candidate: truncation is not a dead end.
	if st, _, err := tools.TaskResumeTarget(a.autoTaskRowID); err != nil || st == nil {
		t.Fatalf("a truncated row must still resolve as a resume target: %v", err)
	}
	sessionBefore, err := tools.TaskSessionID(a.autoTaskRowID)
	if err != nil {
		t.Fatalf("read the run session before the handoff: %v", err)
	}
	if err := tools.TaskStampSelfClaim(a.autoTaskRowID); err != nil {
		t.Fatalf("resume's ownership handoff must still work on this row: %v", err)
	}
	// THE HANDOFF IS ALSO THE RESUME (2026-10-29, bc87f0d — the two-level
	// failure policy, and the assertion here was written before it).
	//
	// Failure handling is two-level and RESUME IS LEVEL 1: a failure judges one
	// ATTEMPT, not the work, so a `failed` row is resumed as the SAME row — same
	// task id, same session id, same directory. TaskStampHolder therefore moves
	// a failed row to in_progress IN THE SAME STATEMENT that records the holder
	// (`WHERE status IN (in_progress, failed)`, internal/tools/tasks.go), so the
	// ownership handoff stops matching zero rows on a failed row — the silent
	// no-op that used to read as "the resume failed again".
	//
	// So this row is EXPECTED to read in_progress after the handoff. What the
	// handoff must never do is lose the identity: pinning that, rather than the
	// old "untouched" claim, keeps this test's own point — truncation is not a
	// dead end — and stops it from asserting the retired posture.
	if got, err := tools.TaskStatusOf(a.autoTaskRowID); err != nil || got != tools.TaskStatusInProgress {
		t.Fatalf("status after the resume handoff = %q (err %v), want %q — level 1 resumes the SAME row", got, err, tools.TaskStatusInProgress)
	}
	if sessionBefore == "" {
		t.Fatal("the row records no run session — the identity half of this assertion would be vacuous")
	}
	if got, err := tools.TaskSessionID(a.autoTaskRowID); err != nil || got != sessionBefore {
		t.Fatalf("run session after the handoff = %q (err %v), want %q unchanged — a resume continues the same record, it does not mint one", got, err, sessionBefore)
	}
}

// TestAgent_StuckLoop_TruncatedRunIsNotCompleted: the stuck net is the sibling
// engine-forced exit. It returns a NON-nil error (exit 1), so this row already
// read `failed` before the fix — the test therefore also pins that the REASON
// is recorded regardless of the exit code, which is what lets `task show` tell
// "the engine stopped this" from "the model's attempt failed".
func TestAgent_StuckLoop_TruncatedRunIsNotCompleted(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		claudeTextStream(w, "已完成：HEAD", "max_tokens") // identical truncated text → stuck net
	}))
	defer server.Close()

	a := newTruncationTestAgent(t, server.URL, 20)

	if _, err := a.Run("stuck task"); err == nil {
		t.Fatal("expected the stuck net to terminate the run")
	}

	row, err := tools.TaskShow(a.autoTaskRowID)
	if err != nil {
		t.Fatalf("TaskShow: %v", err)
	}
	if !regexp.MustCompile(`status:\s+failed`).MatchString(row) {
		t.Fatalf("stuck-terminated run must read failed:\n%s", row)
	}
	if !strings.Contains(row, runend.TruncationMarker) || !strings.Contains(row, runend.StuckLoop.Token()) {
		t.Fatalf("the stuck exit must record its reason (%q %q):\n%s", runend.TruncationMarker, runend.StuckLoop.Token(), row)
	}
	// Same proof for the sibling engine-forced exit: the hook reached the
	// ledger and left the witness in the session JSONL.
	assertTerminalRowWriteWitness(t, a, a.autoTaskRowID, tools.TaskStatusFailed, runend.StuckLoop.Token())
}

// TestFinishSessionTask_EngineReasonDecidesStatus is the direct unit pin of
// the close-out rule, independent of any loop: the exit code AND the end
// reason both decide, and the ordinary finished run is untouched (no marker,
// no reason, still `completed`) — the guard against over-application.
func TestFinishSessionTask_EngineReasonDecidesStatus(t *testing.T) {
	engineStops := []runend.Reason{runend.WallclockCap, runend.StuckLoop, runend.StuckToolLoop, runend.MaxIterations}
	for _, r := range engineStops {
		a := newAutoTaskAgent(t)
		t.Setenv(tools.TaskWrapperEnv, "")
		a.maybeRegisterSessionTask("engine-stopped run " + r.Token())
		a.finishSessionTaskRecorded(0, 5, 100, 3.0, nil, r)

		row, err := tools.TaskShow(a.autoTaskRowID)
		if err != nil {
			t.Fatalf("TaskShow (%s): %v", r, err)
		}
		if !regexp.MustCompile(`status:\s+failed`).MatchString(row) {
			t.Fatalf("exit 0 + end reason %q must NOT close `completed`:\n%s", r, row)
		}
		if !strings.Contains(row, r.Token()) {
			t.Fatalf("row for %q must carry its token:\n%s", r, row)
		}
		// The row keeps the SAME `recoverable: gopi session resume` wording a genuine
		// failure uses — one vocabulary for "this row is not a delivered
		// result", and the resume pointer a truncated run most needs.
		if !strings.Contains(row, "recoverable: `gopi session resume") {
			t.Fatalf("truncated row for %q lost the resume pointer:\n%s", r, row)
		}
		if strings.Contains(row, "FAILED (HTTP") || strings.Contains(row, "category=") {
			t.Fatalf("a truncated row must not invent a provider failure record for %q:\n%s", r, row)
		}
	}

	// Control: a model-finished run (the zero reason) stays `completed` with
	// no marker and no reason stamp — every ordinary run's row is unchanged.
	a := newAutoTaskAgent(t)
	t.Setenv(tools.TaskWrapperEnv, "")
	a.maybeRegisterSessionTask("ordinary finished run")
	a.finishSessionTaskRecorded(0, 7, 4321, 12.5, nil, runend.Finished)

	row, err := tools.TaskShow(a.autoTaskRowID)
	if err != nil {
		t.Fatalf("TaskShow (finished): %v", err)
	}
	if !regexp.MustCompile(`status:\s+completed`).MatchString(row) {
		t.Fatalf("a model-finished run must still close `completed`:\n%s", row)
	}
	if strings.Contains(row, runend.TruncationMarker) || strings.Contains(row, "truncated:") {
		t.Fatalf("a model-finished run must carry no truncation marker:\n%s", row)
	}
	// The digest is byte-identical to the pre-fix one (every existing reader
	// of the ordinary result line stays valid).
	if !strings.Contains(row, "result:         — elapsed=12.5s iter=7 tokens=4321") {
		t.Fatalf("the ordinary result digest changed shape:\n%s", row)
	}
}

// TestRunEndReasonFor_ReadsWhatTheRunRecorded pins the second door to the same
// fact (the CLI wrapper's writeback reads it): the reason column AND the event
// trail are both consulted, and a row that records neither reads as a finished
// run (never a false truncation claim).
//
// The WRAPPER-CHILD case — the one where this read-back is load-bearing — is
// pinned end-to-end by TestRunTruncationNoteRecordsForWrapperChild below: a
// child of `task run` has no auto row, so the run-end NOTE is the only place
// the truncation reaches the wrapper's row, and the wrapper's writeback must
// find the truncation it wrote.
func TestRunEndReasonFor_ReadsWhatTheRunRecorded(t *testing.T) {
	a := newAutoTaskAgent(t)
	t.Setenv(tools.TaskWrapperEnv, "")
	a.maybeRegisterSessionTask("row whose reason is read back")
	a.finishSessionTaskRecorded(0, 3, 60, 2.0, nil, runend.WallclockCap)

	if got := tools.RunEndReasonFor(a.autoTaskRowID); got != runend.WallclockCap {
		t.Fatalf("RunEndReasonFor = %q, want %q (read back from the row)", got, runend.WallclockCap)
	}
	// An unknown row / no truncation is NOT a truncation claim.
	if got := tools.RunEndReasonFor("t-not-a-row"); got.Truncated() {
		t.Fatalf("a row with no record must not read as truncated, got %q", got)
	}
	// The TRAIL door alone is enough: a row whose reason column carries no
	// truncation marker but whose newest event detail does still reports its
	// truncation.
	//
	// THE ROW IS A FRESH ONE, WHICH IS THE WHOLE POINT (epic 01a18f3f, the
	// terminal-state guard). This arm used to write `completed` over the failed
	// row above; that move — failed → completed with no evidence — is REFUSED
	// now on every door, and it should be: a reader's contract may not be proved
	// by rewriting a terminal verdict. Production reaches "no marker in the
	// reason column, marker in the trail" through the WRAPPER-CHILD shape
	// (recordRunTruncationOnWrapperRow → tools.RecordRunEnd): the child records
	// an EVENT whose detail carries the marker, and the reason column keeps
	// whatever the status write put there. So the arm below reproduces exactly
	// that pair of writers, on a row whose live close is `failed` for a reason
	// that says nothing about truncation.
	b := newAutoTaskAgent(t)
	b.maybeRegisterSessionTask("row whose reason column carries no marker")
	if _, err := tools.TaskUpdateReason(b.autoTaskRowID, tools.TaskStatusFailed,
		"failed: the run died on a network error", ""); err != nil {
		t.Fatalf("closing the row failed: %v", err)
	}
	if got := tools.RunEndReasonFor(b.autoTaskRowID); got.Truncated() {
		t.Fatalf("the row's own reason must not read as truncated before the note, got %q", got)
	}
	// The child's note: an event, never a status write (tools.RecordRunEnd).
	tools.RecordRunEnd(b.autoTaskRowID, runend.MaxIterations)
	if got := tools.RunEndReasonFor(b.autoTaskRowID); got != runend.MaxIterations {
		t.Fatalf("RunEndReasonFor after a reason-less writeback = %q, want the trail's %q", got, runend.MaxIterations)
	}
}

// TestRunTruncationNoteRecordsForWrapperChild is the WRAPPER-CHILD regression
// guard. A child of `task run` registers NO auto row (task-single-row-exec), so
// the auto-row writeback is a no-op and the run-end NOTE is the child's only
// ledger contact.
//
// The truncation has to reach the row because every engine-forced stop exits
// ZERO: the wrapper's post-Wait writeback reads this note back
// (tools.RunEndReasonFor) to refuse `completed` — without it a cap-truncated
// `task run` closed `completed` one door after the auto-row fix.
//
// The deleted child-side close-out (task 01a16f7c) was ALSO called from the END
// hook here, and part of this test's original point was that it must not
// rewrite the wrapper's row while the wrapper is alive. That half is preserved
// as an assertion on the SURVIVING door's contract: writing the note must leave
// the row's STATUS untouched — the note is testimony, never a close.
func TestRunTruncationNoteRecordsForWrapperChild(t *testing.T) {
	a := newAutoTaskAgent(t)

	// The wrapper row a `task run` claims pre-spawn, and the child env the
	// wrapper sets (tools.TaskRunIDEnv; the wrapper's claim stamp is what makes
	// the wrapper the row's live writer).
	const wrapperRow = "t-run-child-trunc"
	if _, err := tools.TaskNew("wrapper-owned run", wrapperRow, "", ""); err != nil {
		t.Fatalf("TaskNew wrapper row: %v", err)
	}
	if err := tools.TaskStampSelfClaim(wrapperRow); err != nil {
		t.Fatalf("wrapper claim stamp: %v", err)
	}
	t.Setenv(tools.TaskWrapperEnv, wrapperRow)
	t.Setenv(tools.TaskRunIDEnv, wrapperRow)

	// The child's END hook, on a run the wallclock cap cut short (exit 0).
	a.recordRunTruncationOnWrapperRow(runend.WallclockCap)

	// The note is NOT a close: the row's status is the wrapper's to write.
	if st, err := tools.TaskStatusOf(wrapperRow); err != nil || st == tools.TaskStatusCompleted {
		t.Fatalf("status = %q (err %v) — the child must not complete a row its live wrapper owns", st, err)
	}
	// …but the TRUNCATION reached the row, which is what the wrapper reads.
	if got := tools.RunEndReasonFor(wrapperRow); got != runend.WallclockCap {
		t.Fatalf("RunEndReasonFor(wrapper row) = %q, want %q — the wrapper's writeback would stamp `completed` again", got, runend.WallclockCap)
	}
	row, err := tools.TaskShow(wrapperRow)
	if err != nil {
		t.Fatalf("TaskShow: %v", err)
	}
	if !strings.Contains(row, runend.TruncationMarker) || !strings.Contains(row, "truncated:") {
		t.Fatalf("the wrapper row must show the truncation the child recorded:\n%s", row)
	}
}

// TestRunTruncationNoteUntruncatedChildLeavesTheRowClean is the control for the
// wrapper-child path: an engine-untouched child records NOTHING, leaves the row
// to its live wrapper, and the wrapper's writeback therefore keeps its ordinary
// behaviour (pinned end-to-end by TestWritebackOutcomeCompletesAnOrdinaryRow).
func TestRunTruncationNoteUntruncatedChildLeavesTheRowClean(t *testing.T) {
	a := newAutoTaskAgent(t)

	const wrapperRow = "t-run-child-plain"
	if _, err := tools.TaskNew("wrapper-owned plain run", wrapperRow, "", ""); err != nil {
		t.Fatalf("TaskNew wrapper row: %v", err)
	}
	if err := tools.TaskStampSelfClaim(wrapperRow); err != nil {
		t.Fatalf("wrapper claim stamp: %v", err)
	}
	t.Setenv(tools.TaskWrapperEnv, wrapperRow)
	t.Setenv(tools.TaskRunIDEnv, wrapperRow)

	a.recordRunTruncationOnWrapperRow(runend.Finished)

	if got := tools.RunEndReasonFor(wrapperRow); got.Truncated() {
		t.Fatalf("an untruncated child must record no truncation, got %q", got)
	}
	row, err := tools.TaskShow(wrapperRow)
	if err != nil {
		t.Fatalf("TaskShow: %v", err)
	}
	if strings.Contains(row, runend.TruncationMarker) || strings.Contains(row, "truncated:") {
		t.Fatalf("an untruncated child must leave no truncation on the wrapper row:\n%s", row)
	}
}
