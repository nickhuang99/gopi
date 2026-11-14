package tools

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/nickhuang99/gopi/internal/config"
	"github.com/nickhuang99/gopi/internal/sessionevent"
	"github.com/nickhuang99/gopi/internal/testutil"
)

// THE RED PROOF for the `dead` verdict's note (row
// 01a16f9d-88f3-7b53-99a3-130dbb12aa9a). Before this change the note asserted
// "the run ended (killed/crashed)" for EVERY dead holder — a cause the ladder
// never measured. A worker that finished its work and exited 0 read as a
// casualty, which is exactly what happened to that row.

// taskRunEndTestWriteSession writes one session JSONL at the layout a TASK RUN
// actually produces — sessions/<task-id>/<session-id>.jsonl, the very address
// taskRunRecordPath probes — carrying the given terminal event line VERBATIM
// (so a test states exit code and reason without a second writer).
func taskRunEndTestWriteSession(t *testing.T, taskID, sessionID, endLine string) {
	t.Helper()
	dir := filepath.Join(gopiDataRoot(), config.SessionsDirName, taskID)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir session dir: %v", err)
	}
	body := `{"type":"session_start","sessionId":"` + sessionID + `"}` + "\n" + endLine + "\n"
	if err := os.WriteFile(filepath.Join(dir, sessionID+taskSessionJSONLExt), []byte(body), 0o644); err != nil {
		t.Fatalf("write session jsonl: %v", err)
	}
}

// TestTaskRunEndReadsTheRunsOwnEnding is the MINIMAL-SAMPLE arm: the pure
// reader over the three shapes a log can carry, including the one an older
// binary wrote (no reason key at all).
func TestTaskRunEndReadsTheRunsOwnEnding(t *testing.T) {
	cases := []struct {
		name       string
		tail       string
		wantKnown  bool
		wantCode   int
		wantReason string
		wantClean  bool
	}{
		{
			name:       "clean finish (exit 0, reason complete)",
			tail:       `{"type":"session_end","exit_code":0,"reason":"complete"}`,
			wantKnown:  true,
			wantCode:   0,
			wantReason: sessionevent.SessionEndReasonComplete,
			wantClean:  true,
		},
		{
			name:       "failed turn (exit 1, reason run_failed)",
			tail:       `{"type":"session_end","exit_code":1,"reason":"run_failed"}`,
			wantKnown:  true,
			wantCode:   1,
			wantReason: sessionevent.SessionEndReasonRunFailed,
			wantClean:  false,
		},
		{
			name:       "older binary: exit 0, no reason key",
			tail:       `{"type":"session_end","exit_code":0}`,
			wantKnown:  true,
			wantCode:   0,
			wantReason: "",
			// A zero exit with NO recorded reason cannot separate "the work
			// finished" from "/quit" — the reason axis is what does that, so
			// an absent one is NOT a clean claim (Rule 15).
			wantClean: false,
		},
		{
			name:      "no terminal event at all",
			tail:      `{"type":"turn","text":"still working"}`,
			wantKnown: false,
		},
		{
			// Prompt/console text is echoed into the log; a substring match
			// would read this as a session_end (the false positive
			// sessionTailHasEnd documents).
			name:      "echoed text is not an event",
			tail:      `{"type":"turn","text":"{\"type\":\"session_end\",\"exit_code\":0}"}`,
			wantKnown: false,
		},
	}
	for _, c := range cases {
		got := taskRunEndFromTail([]byte(c.tail))
		if got.Known != c.wantKnown {
			t.Errorf("%s: Known = %v, want %v (end=%+v)", c.name, got.Known, c.wantKnown, got)
			continue
		}
		if !c.wantKnown {
			if got.Clean() {
				t.Errorf("%s: an UNREADABLE ending must never read as clean", c.name)
			}
			continue
		}
		if got.ExitCode != c.wantCode || got.Reason != c.wantReason {
			t.Errorf("%s: got (exit %d, reason %q), want (exit %d, reason %q)",
				c.name, got.ExitCode, got.Reason, c.wantCode, c.wantReason)
		}
		if got.Clean() != c.wantClean {
			t.Errorf("%s: Clean() = %v, want %v", c.name, got.Clean(), c.wantClean)
		}
	}
}

// TestTaskStatusDeadNoteDoesNotInventACause is the defect's own assertion, on
// the rendered line: the clean arm must NOT say killed/crashed, and the
// unreadable arm must not assert a cause it did not observe either.
func TestTaskStatusDeadNoteDoesNotInventACause(t *testing.T) {
	const banner = "killed/crashed"

	clean := taskProcessDeadNote(4242, taskRunEnd{Known: true, ExitCode: 0, Reason: sessionevent.SessionEndReasonComplete})
	if strings.Contains(clean, banner) {
		t.Errorf("a clean end still reads as a casualty: %q", clean)
	}
	if !strings.Contains(clean, "exit 0") || !strings.Contains(clean, sessionevent.SessionEndReasonComplete) {
		t.Errorf("the clean note must state what the run recorded, got %q", clean)
	}

	failed := taskProcessDeadNote(4242, taskRunEnd{Known: true, ExitCode: 1, Reason: sessionevent.SessionEndReasonRunFailed})
	if strings.Contains(failed, banner) {
		t.Errorf("the failed note should name the RECORDED reason, not a generic one: %q", failed)
	}
	if !strings.Contains(failed, "exit 1") || !strings.Contains(failed, sessionevent.SessionEndReasonRunFailed) {
		t.Errorf("the failed note must quote the recorded ending, got %q", failed)
	}

	unknown := taskProcessDeadNote(4242, taskRunEnd{})
	if strings.Contains(unknown, banner) {
		t.Errorf("an unreadable ending must not assert a cause: %q", unknown)
	}
	if !strings.Contains(unknown, "not proven") {
		t.Errorf("the unreadable note must say the cause is unproven, got %q", unknown)
	}
}

// TestTaskStatusDeadReadsTheRowsOwnSession is the END-TO-END arm through the
// production entry point taskProcessStatus: a row whose holder pid is gone and
// whose session log records exit 0/complete must render the clean note, and
// the same row with a failed log must render the casualty note. Both read the
// session file from the resolved layout, so a broken path resolution fails
// here rather than silently degrading to "unknown".
func TestTaskStatusDeadReadsTheRowsOwnSession(t *testing.T) {
	sandbox := t.TempDir()
	t.Setenv(config.GopiDirEnv, sandbox)
	testutil.NewTestDBPath(t)

	deadPid := taskStateTestDeadPid(t)
	now := time.Now()

	// The row shape the ladder calls dead: a claim stamp naming a gone pid, a
	// recorded identity for that same pid, and a stale heartbeat.
	deadRow := func(taskID, sessionID string) *Task {
		return taskStateTestStaleHeartbeat(&Task{
			TaskID:          taskID,
			Status:          taskStatusInProgress,
			SessionID:       sessionID,
			ClaimedBy:       taskStateTestHolder(deadPid),
			HasClaim:        true,
			ClaimedAt:       float64(now.Add(-4 * time.Minute).Unix()),
			HasProcIdentity: true,
			ProcPid:         deadPid,
			ProcStartTicks:  12345,
			ProcCmd:         "gopi worker",
		}, now)
	}

	// ① clean end
	taskRunEndTestWriteSession(t, "t-runend-clean", "sess-runend-clean",
		`{"type":"session_end","exit_code":0,"reason":"complete"}`)
	cleanRow := deadRow("t-runend-clean", "sess-runend-clean")
	st := taskProcessStatus(cleanRow)
	if st.Verdict != TaskProcessStateDead {
		t.Fatalf("fixture broken: verdict = %q, want %q (note=%s)", st.Verdict, TaskProcessStateDead, st.Note)
	}
	if strings.Contains(st.Note, "killed/crashed") {
		t.Errorf("a run that recorded exit 0/complete still reads as a casualty: %q", st.Note)
	}
	if st.RunEndExitCode == nil || *st.RunEndExitCode != 0 ||
		st.RunEndReason != sessionevent.SessionEndReasonComplete {
		t.Errorf("the row's own ending was not surfaced (exit=%v reason=%q)",
			st.RunEndExitCode, st.RunEndReason)
	}

	// ② failed end
	taskRunEndTestWriteSession(t, "t-runend-failed", "sess-runend-failed",
		`{"type":"session_end","exit_code":2,"reason":"run_failed"}`)
	failedRow := deadRow("t-runend-failed", "sess-runend-failed")
	stf := taskProcessStatus(failedRow)
	if stf.Verdict != TaskProcessStateDead {
		t.Fatalf("fixture broken: verdict = %q, want %q", stf.Verdict, TaskProcessStateDead)
	}
	if !strings.Contains(stf.Note, "exit 2") || !strings.Contains(stf.Note, sessionevent.SessionEndReasonRunFailed) {
		t.Errorf("a failed end must quote its own exit code and reason, got %q", stf.Note)
	}

	// ③ no readable log — the fixture must NOT resolve a session, otherwise
	// this arm measures nothing.
	noneRow := deadRow("t-runend-none", "sess-runend-absent")
	stn := taskProcessStatus(noneRow)
	if stn.Verdict != TaskProcessStateDead {
		t.Fatalf("fixture broken: verdict = %q, want %q", stn.Verdict, TaskProcessStateDead)
	}
	if stn.RunEndExitCode != nil {
		t.Errorf("no log was written for this row, yet an exit code was reported (%d) — a guess", *stn.RunEndExitCode)
	}
	if strings.Contains(stn.Note, "killed/crashed") {
		t.Errorf("an unreadable ending still asserts a cause: %q", stn.Note)
	}
}
