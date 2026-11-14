package tools

import (
	"context"
	"os"
	"strings"
	"testing"

	"github.com/nickhuang99/gopi/internal/config"
	"github.com/nickhuang99/gopi/internal/testutil"
)

// The regression suite for the three-element status-event record (epic
// 01a1a899-d05d). Each arm names the fact it pins, and each one fails if the
// record collapses back to "a digest OR an empty string", which is the shape
// the ledger was in before this file existed.
//
// Arms:
//   - ① who / why / from→to are ALL present on a transition with a --reason and
//     a caller-supplied digest (and the digest survives `result:`).
//   - ② a run identity is distinguished from an operator one: the agent-session
//     handle is the discriminator, and NOBODY gets attributed to `run:` without
//     one.
//   - ③ a historical (empty-detail) event renders as UNRECORDED, naming no
//     executor — the 01a1a194e1 shape, which must never be dressed as a culprit.
//   - ④ `--reason` reaches the detail (the operator's grounds are the record's
//     WHY, not the default sentence).
//   - ⑤ a plain status move records the record WITHOUT a digest trailer, and
//     `task show` renders it on `transition:` while `result:` stays empty —
//     i.e. a lifecycle fact cannot impersonate a run result.

// statusEventReadDetail reads the detail of the newest event whose name is
// `event` on a row — the ledger-truth read every arm below asserts against
// (no render, no selector: what is actually stored).
func statusEventReadDetail(t *testing.T, taskID, event string) string {
	t.Helper()
	var detail string
	if err := sweepTestDB(t).QueryRowContext(context.Background(),
		`SELECT COALESCE(detail, '') FROM task_events WHERE task_id = ? AND event = ? ORDER BY id DESC LIMIT 1`,
		taskID, event).Scan(&detail); err != nil {
		t.Fatalf("read task_events detail for %s/%s: %v", taskID, event, err)
	}
	return detail
}

// statusEventSeedRow submits + starts a row through the real doors, so the trail
// the arms read is the one the program writes.
func statusEventSeedRow(t *testing.T, taskID string) {
	t.Helper()
	if _, err := TaskSubmit(TaskSubmitOptions{
		Summary: "status-event record fixture",
		Payload: "briefing body",
		TaskID:  taskID,
		Source:  "auto",
	}); err != nil {
		t.Fatalf("TaskSubmit(%s): %v", taskID, err)
	}
	if _, err := TaskUpdate(taskID, TaskStatusInProgress); err != nil {
		t.Fatalf("TaskUpdate(%s, in_progress): %v", taskID, err)
	}
}

// ① + ④ — the three elements, and the caller's reason as the WHY.
func TestTaskStatusEventRecordsWhoWhyAndMove(t *testing.T) {
	testutil.NewTestDBPath(t)
	const taskID = "t-status-event-elements"
	statusEventSeedRow(t, taskID)

	const reason = "test reason — the operator's grounds"
	const digest = "gopi run failed — run: elapsed=3.2s — task-report: /tmp/r.md (900 bytes)"
	out, err := TaskUpdateReason(taskID, TaskStatusFailed, reason, digest)
	if err != nil {
		t.Fatalf("TaskUpdateReason: %v", err)
	}
	t.Logf("TaskUpdateReason output: %s", strings.TrimSpace(out))

	detail := statusEventReadDetail(t, taskID, TaskStatusFailed)
	t.Logf("task_events detail for %s: %q", TaskStatusFailed, detail)

	// ① the three elements are ALL there.
	if !strings.HasPrefix(detail, TaskStatusEventWhoLabel) {
		t.Fatalf("detail must OPEN with %q (the read side's one discriminator), got %q",
			TaskStatusEventWhoLabel, detail)
	}
	if !strings.Contains(detail, taskStatusEventWho()) {
		t.Errorf("detail must name the writer (%q), got %q", taskStatusEventWho(), detail)
	}
	wantMove := TaskStatusInProgress + TaskStatusEventMoveSeparator + TaskStatusFailed
	if !strings.Contains(detail, wantMove) {
		t.Errorf("detail must carry the move %q, got %q", wantMove, detail)
	}
	// ④ the operator's --reason IS the why (not the status default).
	if !strings.Contains(detail, TaskStatusEventWhySeparator+reason) {
		t.Errorf("detail must carry the caller's reason as why=%q, got %q", reason, detail)
	}
	if strings.Contains(detail, TaskStatusEventWhySeparator+taskDefaultReason(TaskStatusFailed)) {
		t.Errorf("a supplied --reason must not be replaced by the status default, got %q", detail)
	}
	// The writeback digest rides the trailer, verbatim, for `result:` to render.
	if !strings.Contains(detail, TaskStatusEventTrailerMarker+digest) {
		t.Errorf("a writeback digest must ride the trailer line, got %q", detail)
	}

	// …and `result:` still shows the DIGEST (never the record).
	shown, err := TaskShow(taskID)
	if err != nil {
		t.Fatalf("TaskShow: %v", err)
	}
	if !strings.Contains(shown, "result:         "+digest) {
		t.Errorf("`result:` must render the run digest, not the transition record:\n%s", shown)
	}
	if strings.Contains(shown, "result:         "+TaskStatusEventWhoLabel) {
		t.Errorf("`result:` must never carry the transition record:\n%s", shown)
	}
	// The record renders on its own face. The RENDER is one line per face, so
	// the assertion is against the record's FIRST line (the digest rides the
	// trailer line, indented below it, exactly as the run digest does elsewhere).
	wantTransition := TaskShowTransitionLabel + strings.Repeat(" ", taskShowLabelWidth-len(TaskShowTransitionLabel)) +
		strings.SplitN(detail, "\n", 2)[0]
	if !strings.Contains(shown, wantTransition) {
		t.Errorf("`task show` must render the record on %q:\n%s", TaskShowTransitionLabel, shown)
	}
}

// ② — a run identity and an operator identity are DISTINGUISHED, and the
// discriminator is the agent-session handle (never a guess).
func TestTaskStatusEventDistinguishesRunFromOperator(t *testing.T) {
	testutil.NewTestDBPath(t)

	const runID = "t-status-event-run"
	const opID = "t-status-event-operator"

	// A run: the agent-session handle is present (the runner exports it for
	// every agent process and everything it spawns).
	const sessID = "unit-test-session-abc123"
	t.Setenv(TaskExecutorSessionEnv, sessID)
	statusEventSeedRow(t, runID)
	if _, err := TaskUpdateReason(runID, TaskStatusFailed, "run-side failure", ""); err != nil {
		t.Fatalf("TaskUpdateReason(run): %v", err)
	}
	runDetail := statusEventReadDetail(t, runID, TaskStatusFailed)
	t.Logf("run-side detail: %q", runDetail)
	wantRunWho := TaskStatusEventWhoRunPrefix + sessID
	if !strings.Contains(runDetail, TaskStatusEventWhoLabel+wantRunWho+TaskStatusEventFieldSeparator) {
		t.Errorf("a run transition must record who=%q, got %q", wantRunWho, runDetail)
	}
	if strings.Contains(runDetail, TaskStatusEventWhoOperator) {
		t.Errorf("a run transition must NOT be recorded as the operator, got %q", runDetail)
	}

	// An operator: no agent-session handle in force. The variable is restored
	// to ABSENT (not set-to-"") so the second leg measures the real shape.
	snap := testutil.SnapshotEnv(t, TaskExecutorSessionEnv)
	snap()
	// env-leak-ok: per-call flip, not a test-wide leak — the SnapshotEnv capture
	// above restores ABSENCE exactly (os.Unsetenv alone cannot express "was
	// absent"), and the two legs must run under DIFFERENT identities.
	if err := os.Unsetenv(TaskExecutorSessionEnv); err != nil {
		t.Fatalf("unset %s: %v", TaskExecutorSessionEnv, err)
	}
	statusEventSeedRow(t, opID)
	if _, err := TaskUpdateReason(opID, TaskStatusFailed, "operator-side failure", ""); err != nil {
		t.Fatalf("TaskUpdateReason(operator): %v", err)
	}
	opDetail := statusEventReadDetail(t, opID, TaskStatusFailed)
	t.Logf("operator-side detail: %q", opDetail)
	if !strings.Contains(opDetail, TaskStatusEventWhoLabel+TaskStatusEventWhoOperator+TaskStatusEventFieldSeparator) {
		t.Errorf("a transition with no agent session must record who=%q, got %q",
			TaskStatusEventWhoOperator, opDetail)
	}
	if strings.Contains(opDetail, TaskStatusEventWhoRunPrefix) {
		t.Errorf("a transition with no agent session must NOT invent a run identity, got %q", opDetail)
	}

	// The two records are DIFFERENT — the whole point of the arm.
	if runDetail == opDetail {
		t.Fatalf("run-side and operator-side records are identical (%q) — the identities are not distinguished",
			runDetail)
	}
}

// ③ — a historical event whose detail is empty renders as UNRECORDED, naming no
// executor. This is the 01a1a194e1 shape: two bare `failed` events and no
// answer to "who did it".
func TestTaskStatusEventLegacyEmptyDetailRendersUnrecorded(t *testing.T) {
	testutil.NewTestDBPath(t)
	const taskID = "t-status-event-legacy"
	statusEventSeedRow(t, taskID)

	// The PRE-RECORD shape, written through the same append-only writer the old
	// code used: a status event with an empty detail.
	taskEventWrite(context.Background(), sweepTestDB(t), taskID, TaskStatusFailed, "")

	if got := statusEventReadDetail(t, taskID, TaskStatusFailed); got != "" {
		t.Fatalf("fixture must leave the detail EMPTY (that is the historical shape), got %q", got)
	}

	db := sweepTestDB(t)
	ev, detail := taskEventLatestTransition(context.Background(), db, taskID)
	if ev != TaskStatusFailed {
		t.Fatalf("newest transition event = %q, want %q", ev, TaskStatusFailed)
	}
	t.Logf("legacy render: %q", detail)

	if !strings.Contains(detail, TaskStatusEventWhoUnrecorded) {
		t.Errorf("a historical empty detail must render who=%q, got %q",
			TaskStatusEventWhoUnrecorded, detail)
	}
	// It must NOT name any executor — no run, no operator, no invention.
	for _, forbidden := range []string{TaskStatusEventWhoOperator, TaskStatusEventWhoRunPrefix, TaskStatusEventWhoAgentPrefix} {
		if strings.Contains(detail, forbidden) {
			t.Errorf("the unrecorded render must name NO executor, found %q in %q", forbidden, detail)
		}
	}
	// The move's TARGET is real information (the event name is the new status),
	// and it renders.
	if !strings.Contains(detail, TaskStatusEventMoveSeparator+TaskStatusFailed) {
		t.Errorf("the legacy render must carry the arrival state %q, got %q", TaskStatusFailed, detail)
	}

	// …and `task show` puts it on the transition face.
	shown, err := TaskShow(taskID)
	if err != nil {
		t.Fatalf("TaskShow: %v", err)
	}
	if !strings.Contains(shown, TaskShowTransitionLabel) {
		t.Errorf("`task show` must render the legacy transition face:\n%s", shown)
	}
	if !strings.Contains(shown, TaskStatusEventWhoUnrecorded) {
		t.Errorf("`task show` must render the unrecorded who, not an invented one:\n%s", shown)
	}
}

// ⑤ — a plain status move records the three elements and NO digest trailer, and
// `task show` keeps `result:` empty for it: a lifecycle fact must not be
// mistaken for a run result (the displacement this epic had to preserve).
func TestTaskStatusEventPlainMoveCarriesNoDigest(t *testing.T) {
	testutil.NewTestDBPath(t)
	const taskID = "t-status-event-plain"
	statusEventSeedRow(t, taskID)

	if _, err := TaskUpdate(taskID, TaskStatusCancelled); err != nil {
		t.Fatalf("TaskUpdate(cancelled): %v", err)
	}
	detail := statusEventReadDetail(t, taskID, TaskStatusCancelled)
	t.Logf("plain-move detail: %q", detail)

	if !strings.HasPrefix(detail, TaskStatusEventWhoLabel) {
		t.Fatalf("a plain move still records the three elements, got %q", detail)
	}
	if strings.Contains(detail, TaskStatusEventTrailerMarker) {
		t.Errorf("a plain move carries no run digest — no trailer, got %q", detail)
	}
	if !taskStatusEventIsTransitionOnly(detail) {
		t.Errorf("a plain move's detail must be a transition record, got %q", detail)
	}
	// A real change always records SOME why (the caller's or the status default).
	if strings.Contains(detail, TaskStatusEventWhyUnrecorded) {
		t.Errorf("a real status change must resolve a why, got %q", detail)
	}

	if got := taskEventLatestDetail(context.Background(), sweepTestDB(t), taskID); got != "" {
		t.Errorf("`result:` must stay empty when no status event carried a digest, got %q", got)
	}

	shown, err := TaskShow(taskID)
	if err != nil {
		t.Fatalf("TaskShow: %v", err)
	}
	if strings.Contains(shown, "result:         "+TaskStatusEventWhoLabel) {
		t.Errorf("a plain move must not land on `result:`:\n%s", shown)
	}
	if !strings.Contains(shown, TaskStatusEventWhoLabel) {
		t.Errorf("a plain move must still be visible on the transition face:\n%s", shown)
	}
}

// The identity resolver itself, at its two shapes, so a later edit to the
// discriminator fails here first (the arms above can pass with a resolver that
// happens to be right for their fixtures).
func TestTaskStatusEventWhoResolver(t *testing.T) {
	t.Setenv(TaskExecutorSessionEnv, "sess-xyz")
	if got, want := taskStatusEventWho(), TaskStatusEventWhoRunPrefix+"sess-xyz"; got != want {
		t.Errorf("with an agent session: got %q, want %q", got, want)
	}
	snap := testutil.SnapshotEnv(t, TaskExecutorSessionEnv)
	snap()
	// env-leak-ok: per-call flip; the capture above restores ABSENCE exactly.
	if err := os.Unsetenv(TaskExecutorSessionEnv); err != nil {
		t.Fatalf("unset %s: %v", TaskExecutorSessionEnv, err)
	}
	if got := taskStatusEventWho(); got != TaskStatusEventWhoOperator {
		t.Errorf("with no agent session: got %q, want %q", got, TaskStatusEventWhoOperator)
	}
	// An EMPTY handle is not an identity: it must not render as `run:`.
	t.Setenv(TaskExecutorSessionEnv, "   ")
	if got := taskStatusEventWho(); got != TaskStatusEventWhoOperator {
		t.Errorf("a blank handle must fall back to %q, got %q", TaskStatusEventWhoOperator, got)
	}
}

// The trailer split, in isolation: the read side must recover the DIGEST exactly
// and must not touch a detail that carries no trailer (history).
func TestTaskStatusEventTrailerSplit(t *testing.T) {
	rec := taskStatusEventDetailText(taskStatusEventDetailParts{
		Who: TaskStatusEventWhoRunPrefix + "s1", From: TaskStatusPending,
		To: TaskStatusCompleted, Why: "done", Digest: "line one\nline two",
	})
	if got := taskStatusEventStripTrailer(rec); got != "line one\nline two" {
		t.Errorf("strip must return the digest verbatim, got %q from %q", got, rec)
	}
	const legacyDigest = "gopi run completed — run: elapsed=1s"
	if got := taskStatusEventStripTrailer(legacyDigest); got != legacyDigest {
		t.Errorf("a detail with no trailer must be returned unchanged, got %q", got)
	}
	// A pre-record digest is NOT a transition record (so `result:` still renders it).
	if taskStatusEventIsTransitionDetail(legacyDigest) {
		t.Errorf("a bare digest must not be classified as a transition record")
	}
	// The two packages must spell the agent-session variable ONE way (Rules
	// 13/14): tools reads it, config documents it as the guard's signature.
	if TaskExecutorSessionEnv != config.AgentSessionEnv {
		t.Errorf("tools and config spell the agent-session variable differently: %q vs %q",
			TaskExecutorSessionEnv, config.AgentSessionEnv)
	}
}

// ⑥ — THE WORKER'S OWN CLOSE-OUT DOOR. A worker does not call TaskUpdateReason:
// its terminal fact is written through TaskRunWriteback → TaskUpdateWithResult →
// the same state gate, carrying the session handle the runner exported. This arm
// runs THAT door and asserts the identity it records is the worker's run, so the
// epic's acceptance ("a real worker writeback records who=run:<session_id>") is
// pinned by the mechanism rather than by a hand-made call.
func TestTaskStatusEventWorkerWritebackRecordsRunIdentity(t *testing.T) {
	testutil.NewTestDBPath(t)
	const taskID = "t-status-event-worker"
	const workerSess = "worker-sess-DEADBEEF"
	const digest = "gopi run completed — run: elapsed=42s — task-report: /tmp/w/task-report.md (1063 bytes)"

	t.Setenv(TaskExecutorSessionEnv, workerSess)
	statusEventSeedRow(t, taskID)

	out, err := TaskRunWriteback(taskID, TaskStatusCompleted, digest)
	if err != nil {
		t.Fatalf("TaskRunWriteback: %v", err)
	}
	t.Logf("worker close-out door returned: %s", strings.TrimSpace(out))

	detail := statusEventReadDetail(t, taskID, TaskStatusCompleted)
	t.Logf("task_events detail for %s: %q", TaskStatusCompleted, detail)

	wantWho := TaskStatusEventWhoLabel + TaskStatusEventWhoRunPrefix + workerSess + TaskStatusEventFieldSeparator
	if !strings.HasPrefix(detail, wantWho) {
		t.Errorf("a worker writeback must record who=%q, got %q", strings.TrimPrefix(wantWho, TaskStatusEventWhoLabel), detail)
	}
	wantMove := TaskStatusInProgress + TaskStatusEventMoveSeparator + TaskStatusCompleted
	if !strings.Contains(detail, wantMove) {
		t.Errorf("the record must carry the move %q, got %q", wantMove, detail)
	}
	if !strings.Contains(detail, TaskStatusEventTrailerMarker+digest) {
		t.Errorf("the run digest must ride the trailer, got %q", detail)
	}
	// `result:` renders the digest — the close-out chain and Rule 31 read it there.
	if got := taskEventLatestDetail(context.Background(), sweepTestDB(t), taskID); got != digest {
		t.Errorf("`result:` must render the run digest verbatim, got %q", got)
	}
}

// ⑦ — the THIRD legacy shape: a status event that recorded a sentence which is
// not this regime's record (a pre-format run digest, or the sync lane's own move
// line). Its grounds ARE carried; its identity is not, and the render says so
// WITHOUT the pre-<date> wording — a sync move written today would otherwise be
// mis-dated, sending a reader after a ledger boundary that does not exist.
func TestTaskStatusEventUnclassifiedDetailKeepsTheRecordedGrounds(t *testing.T) {
	testutil.NewTestDBPath(t)
	const taskID = "t-status-event-sync-shaped"
	statusEventSeedRow(t, taskID)
	if _, err := TaskUpdate(taskID, TaskStatusCompleted); err != nil {
		t.Fatalf("TaskUpdate: %v", err)
	}

	// The sync lane's spelling, appended by its own writer AFTER the transition.
	syncDetail := "sync harvest from peer1: in_progress → completed"
	taskEventWrite(context.Background(), sweepTestDB(t), taskID, TaskStatusCompleted, syncDetail)

	db := sweepTestDB(t)
	ev, detail := taskEventLatestTransition(context.Background(), db, taskID)
	if ev != TaskStatusCompleted {
		t.Fatalf("newest transition event = %q, want %q", ev, TaskStatusCompleted)
	}
	t.Logf("sync-shaped render: %q", detail)

	if !strings.Contains(detail, TaskStatusEventWhoUnrecordedUnclassified) {
		t.Errorf("the who slot must say no identity was recorded (%q), got %q",
			TaskStatusEventWhoUnrecordedUnclassified, detail)
	}
	if strings.Contains(detail, TaskStatusEventWhoUnrecorded) {
		t.Errorf("a detail written TODAY must not claim the pre-%s shape, got %q",
			TaskStatusEventDetailFormatDate, detail)
	}
	if !strings.Contains(detail, syncDetail) {
		t.Errorf("the recorded grounds must be carried, got %q", detail)
	}
	for _, forbidden := range []string{TaskStatusEventWhoOperator, TaskStatusEventWhoRunPrefix} {
		if strings.Contains(detail, forbidden) {
			t.Errorf("the render must name no executor, found %q in %q", forbidden, detail)
		}
	}
}

// ⑧ — the why slot is NEVER empty and never filler. The epic's constraint is
// explicit: do not stuff a meaningless string ("status changed") in to make the
// detail non-empty; what is wanted is who + why, or a stated absence. A caller
// that supplies no grounds gets `(none given)` — which is a claim about a write
// THIS program made, distinct from the historical `(not recorded)`.
func TestTaskStatusEventMissingWhyStatesTheAbsence(t *testing.T) {
	const to = TaskStatusCompleted
	got := taskStatusEventDetailText(taskStatusEventDetailParts{
		Who:  TaskStatusEventWhoOperator,
		From: TaskStatusInProgress,
		To:   to,
	})
	t.Logf("no-grounds record: %q", got)

	if strings.Contains(got, TaskStatusEventWhySeparator+TaskStatusEventFieldSeparator) ||
		strings.HasSuffix(got, TaskStatusEventWhySeparator) {
		t.Errorf("the why slot must never render empty, got %q", got)
	}
	if !strings.Contains(got, TaskStatusEventWhySeparator+TaskStatusEventWhyNoneGiven) {
		t.Errorf("a record with no caller-supplied grounds must say %q, got %q",
			TaskStatusEventWhyNoneGiven, got)
	}
	if strings.Contains(got, TaskStatusEventWhyUnrecorded) {
		t.Errorf("%q is the HISTORICAL word; a write made now must not borrow it, got %q",
			TaskStatusEventWhyUnrecorded, got)
	}
	// And it is still a record with both provenance slots filled.
	for _, want := range []string{
		TaskStatusEventWhoLabel + TaskStatusEventWhoOperator,
		TaskStatusInProgress + TaskStatusEventMoveSeparator + to,
	} {
		if !strings.Contains(got, want) {
			t.Errorf("record must carry %q, got %q", want, got)
		}
	}
}

// ⑩ — the notify bus reads the SAME detail. A notification about a run must
// quote the run's line; the record's grammar (`who=… → … why=…`) is not a run
// summary and must never be substituted for one where a digest exists.
func TestTaskStatusEventNotifySourceQuotesTheRunLine(t *testing.T) {
	testutil.NewTestDBPath(t)
	const taskID = "t-status-event-notify"
	const digest = "gopi run failed — run: elapsed=3.2s — task-report: /tmp/n.md (900 bytes)"

	t.Setenv(TaskExecutorSessionEnv, "sess-notify")
	statusEventSeedRow(t, taskID)
	if _, err := TaskUpdateWithResult(taskID, TaskStatusFailed, digest); err != nil {
		t.Fatalf("TaskUpdateWithResult: %v", err)
	}
	row, err := taskNotifyReread(taskID)
	if err != nil {
		t.Fatalf("taskNotifyReread: %v", err)
	}
	got := taskNotifyStatsSource(row)
	t.Logf("notify source: %q", got)

	if got != digest {
		t.Errorf("the notify source must be the run's line verbatim, got %q", got)
	}
	if strings.Contains(got, TaskStatusEventWhoLabel) || strings.Contains(got, TaskStatusEventTrailerMarker) {
		t.Errorf("neither the record's grammar nor the trailer marker may reach a notification, got %q", got)
	}
}
