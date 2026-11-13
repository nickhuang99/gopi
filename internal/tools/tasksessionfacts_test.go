package tools

// tasksessionfacts_test.go — the RED-FIRST contrasts for the `session:` line.
//
// The epic's claim is that a ledger word alone cannot distinguish "never
// started" from "ran and failed". Each test below builds ONE session directory
// whose artifacts measure a different shape and asserts the four readings the
// line derives from them:
//
//	stopped + no request + no turn   → never-started
//	ended   + turns                  → ended, and NEVER never-started
//	running (live pid)               → running, and NEVER never-started
//	unstamped (no pid) + no request  → unstamped, and NEVER never-started (Rule
//	                                   15: a missing stamp is missing evidence)
//
// The fixtures are written under a temp GOPI_HOME through the same resolvers
// production uses (rundir.Dir, config.GopiDir), so the reader is exercised on
// the real layout rather than on a hand-rolled one (Rule 32's spirit for the
// filesystem half: never invent a second path shape).

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/nick/gopi/internal/config"
	"github.com/nick/gopi/internal/sessionlayout"
)

const (
	// factsSessionStart is the fixture's session_start timestamp.
	factsSessionStart = "2026-11-01T10:12:39+08:00"
	// factsRawHeader is the creation line every real raw log opens with — the
	// exact shape this host measured on session ae02028b (one line, no request).
	factsRawHeader = "# gopi session raw HTTP log — created 2026-11-01T02:12:39Z pid=%d session=%s log=%s.raw.log"
)

// factsFixture lays out ONE session directory for a row and returns the ids.
//
// shape controls the artifacts:
//
//	"stopped"    session_start only (no session_end, no request, no turn),
//	             with the recorded pid of a process that is gone
//	"ended"      session_start + `turns` request events + a session_end verdict
//	"running"    session_start stamped with THIS process's pid (alive)
//	"unstamped"  session_start carrying no pid at all
//
// The raw log is always written (the creation header, plus one `→ REQUEST`
// record per turn) so a "0 requests" reading is a MEASUREMENT, never an absence.
func factsFixture(t *testing.T, shape string, turns int) (taskID, sessionID string) {
	t.Helper()
	home := t.TempDir()
	t.Setenv(config.GopiDirEnv, home)
	taskID = "t-facts-" + shape
	sessionID = "sess-facts-" + shape
	dir := filepath.Join(home, config.ProfileDirName, config.SessionsDirName, taskID)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir session dir: %v", err)
	}
	rawPath := filepath.Join(dir, sessionID+".raw.log")
	pid := 0
	switch shape {
	case "stopped":
		// A pid that cannot exist: pid_max on this host is 4194304, and this
		// fixture asserts the probe's GONE verdict, so the number must be one
		// no live process holds. 999999 is used by no process on this host.
		pid = factsDeadPID
	case "running":
		pid = os.Getpid()
	}
	header := map[string]any{
		"type":       "session_start",
		"sessionId":  sessionID,
		"model":      "m",
		"rawLogPath": rawPath,
		"timestamp":  factsSessionStart,
		"workdir":    dir,
	}
	if pid > 0 {
		header["pid"] = pid
	}
	line, err := json.Marshal(header)
	if err != nil {
		t.Fatalf("marshal session_start: %v", err)
	}
	rec := string(line) + "\n"
	for i := 1; i <= turns; i++ {
		rec += `{"type":"request","iteration":` + strconv.Itoa(i) + `}` + "\n"
	}
	if shape == "ended" {
		rec += `{"type":"session_end","exit_code":0,"total_iterations":` + strconv.Itoa(turns) +
			`,"total_tokens":10,"elapsed_sec":1,"timestamp":"` + factsSessionStart + `"}` + "\n"
	}
	if err := os.WriteFile(filepath.Join(dir, sessionlayout.RecordName), []byte(rec), 0o644); err != nil {
		t.Fatalf("write session record: %v", err)
	}
	raw := "# gopi session raw HTTP log — created 2026-11-01T02:12:39Z pid=" +
		strconv.Itoa(pid) + " session=" + sessionID + " log=" + sessionID + ".raw.log\n"
	for i := 0; i < turns; i++ {
		raw += "→ REQUEST\n{}\n"
	}
	if err := os.WriteFile(rawPath, []byte(raw), 0o644); err != nil {
		t.Fatalf("write raw log: %v", err)
	}
	if shape == "stopped" {
		// The death this epic is about leaves a crash document; the category is
		// the writer's own field name (internal/agent's crashdump.go).
		doc := `{"type":"crash_dump","category":"orphan-no-session-end","session_id":"` + sessionID + `"}`
		if err := os.WriteFile(filepath.Join(dir, sessionlayout.ShapeSessionCrashDump), []byte(doc), 0o600); err != nil {
			t.Fatalf("write crash dump: %v", err)
		}
	}
	return taskID, sessionID
}

// factsDeadPID is a pid this fixture can be certain no live process holds. The
// premise is asserted by the probe itself: if a process DID hold it the state
// would come back `running` and the stopped arm would fail loudly rather than
// silently passing on a wrong premise.
const factsDeadPID = 999999

// TestSessionFactsStoppedWithNoRequestIsNeverStarted is the epic's headline
// reading: the artifacts of a run that died before its first model call.
func TestSessionFactsStoppedWithNoRequestIsNeverStarted(t *testing.T) {
	taskID, sessionID := factsFixture(t, "stopped", 0)
	f := TaskSessionFactsFor(taskID, sessionID)
	if !f.Resolved {
		t.Fatalf("no session record resolved for %s: %+v", taskID, f)
	}
	if f.State != SessionRunStateStopped {
		t.Fatalf("state = %q, want %q (a dead writer with no session_end is stopped): %+v",
			f.State, SessionRunStateStopped, f)
	}
	if !f.RequestsKnown || f.Requests != 0 {
		t.Errorf("requests = %d (known=%v), want a MEASURED 0 from the raw log: %+v",
			f.Requests, f.RequestsKnown, f)
	}
	if f.Turns != 0 {
		t.Errorf("turns = %d, want 0", f.Turns)
	}
	if !f.HasCrash || f.CrashCategory != "orphan-no-session-end" {
		t.Errorf("crash = %q (has=%v), want orphan-no-session-end read from session.dump.json",
			f.CrashCategory, f.HasCrash)
	}
	if !f.NeverStarted {
		t.Errorf("stopped + 0 turns + 0 requests must derive %q: %+v", TaskSessionNeverStarted, f)
	}
	line := taskSessionFactsValue(f)
	for _, want := range []string{
		taskSessionStateKey + SessionRunStateStopped,
		taskSessionCrashKey + "orphan-no-session-end",
		taskSessionTurnsKey + "0",
		taskSessionRequestsKey + "0",
		TaskSessionNeverStarted,
	} {
		if !strings.Contains(line, want) {
			t.Errorf("rendered line is missing %q:\n%s", want, line)
		}
	}
}

// TestSessionFactsEndedWithTurnsIsNotNeverStarted is the first reverse contrast:
// a run that really executed must never wear the never-started word.
func TestSessionFactsEndedWithTurnsIsNotNeverStarted(t *testing.T) {
	const turns = 7
	taskID, sessionID := factsFixture(t, "ended", turns)
	f := TaskSessionFactsFor(taskID, sessionID)
	if f.State != SessionRunStateEnded {
		t.Fatalf("state = %q, want %q (a log with a session_end is ended): %+v", f.State, SessionRunStateEnded, f)
	}
	if f.Turns != turns {
		t.Errorf("turns = %d, want %d (session_end's total_iterations)", f.Turns, turns)
	}
	if !f.RequestsKnown || f.Requests != turns {
		t.Errorf("requests = %d (known=%v), want %d from the raw log", f.Requests, f.RequestsKnown, turns)
	}
	if f.NeverStarted {
		t.Errorf("an ENDED session with %d turns must never be labelled %q: %+v",
			turns, TaskSessionNeverStarted, f)
	}
	if strings.Contains(taskSessionFactsValue(f), TaskSessionNeverStarted) {
		t.Errorf("the rendered line carries %q for an ended run:\n%s",
			TaskSessionNeverStarted, taskSessionFactsValue(f))
	}
}

// TestSessionFactsRunningIsNotNeverStarted is the second reverse contrast: a
// live writer outranks every other reading, and the never-started word is
// reserved for sessions whose writer is PROVEN gone.
func TestSessionFactsRunningIsNotNeverStarted(t *testing.T) {
	taskID, sessionID := factsFixture(t, "running", 0)
	f := TaskSessionFactsFor(taskID, sessionID)
	if f.State != SessionRunStateRunning {
		t.Fatalf("state = %q, want %q (the fixture stamps this live process): %+v",
			f.State, SessionRunStateRunning, f)
	}
	if f.NeverStarted {
		t.Errorf("a RUNNING session with no requests yet is not never-started — it has not finished: %+v", f)
	}
}

// TestSessionFactsUnstampedIsNotNeverStarted is the Rule 15 arm: with no pid
// stamp there is no evidence the writer is gone, so the reader must not accuse
// the session of never having run — it must say `unstamped` and stop there.
func TestSessionFactsUnstampedIsNotNeverStarted(t *testing.T) {
	taskID, sessionID := factsFixture(t, "unstamped", 0)
	f := TaskSessionFactsFor(taskID, sessionID)
	if f.State != SessionRunStateUnstamped {
		t.Fatalf("state = %q, want %q (no pid stamp at all): %+v", f.State, SessionRunStateUnstamped, f)
	}
	if f.NeverStarted {
		t.Errorf("missing evidence must NOT yield %q — an absent stamp is not proof of death: %+v",
			TaskSessionNeverStarted, f)
	}
}

// TestSessionFactsAbsenceIsStated: a row with no session layer at all renders a
// line that NAMES the absence (Rule 15) — never a block of zeros that reads as
// a measurement.
func TestSessionFactsAbsenceIsStated(t *testing.T) {
	home := t.TempDir()
	t.Setenv(config.GopiDirEnv, home)
	f := TaskSessionFactsFor("t-facts-nothing", "sess-that-does-not-exist")
	if f.Resolved {
		t.Fatalf("resolved a session for a row with no artifacts: %+v", f)
	}
	if f.NeverStarted {
		t.Errorf("an absent session must not derive %q: %+v", TaskSessionNeverStarted, f)
	}
	line := taskSessionFactsValue(f)
	if !strings.Contains(line, "no run log") {
		t.Errorf("an unresolvable session must state what was searched:\n%s", line)
	}
}

// TestSessionFactsLineIsLabelledAndGutterAligned: the line must sit in the
// `task show` table (label + the block's own gutter) so it reads next to
// `status:` instead of floating outside the table.
func TestSessionFactsLineIsLabelledAndGutterAligned(t *testing.T) {
	taskID, sessionID := factsFixture(t, "stopped", 0)
	line := taskSessionFactsLine(taskID, sessionID)
	if !strings.HasPrefix(line, TaskShowSessionLabel) {
		t.Fatalf("line does not start with %q:\n%s", TaskShowSessionLabel, line)
	}
	rest := strings.TrimPrefix(line, TaskShowSessionLabel)
	if n := len(rest) - len(strings.TrimLeft(rest, " ")); n != taskShowLabelWidth-len(TaskShowSessionLabel) {
		t.Errorf("label gutter = %d spaces, want %d (taskShowLabelWidth):\n%s",
			n, taskShowLabelWidth-len(TaskShowSessionLabel), line)
	}
}
