package tools

import (
	"context"
	"database/sql"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nick/gopi/internal/config"
	"github.com/nick/gopi/internal/tasksource"
	"github.com/nick/gopi/internal/testutil"
)

// ---------------------------------------------------------------------------
// Engine tests: TaskNew / TaskList / TaskShow / TaskUpdate against a fresh
// temp memory.db (GOPI_MEMORY_DB override — never the real ~/.gopi/memory.db).
// ---------------------------------------------------------------------------

func TestTaskNewAutoID(t *testing.T) {
	path := testutil.NewTestDBPath(t)

	out, err := TaskNew("fix the nudge bug", "", "", "")
	if err != nil {
		t.Fatalf("TaskNew: %v", err)
	}
	if !strings.HasPrefix(out, "Task created: ") || !strings.Contains(out, "status=in_progress") {
		t.Errorf("out = %q, want auto id + in_progress status", out)
	}
	// Auto ids are uuid v7 (36-char 8-4-4-4-12 shape); extract between
	// "Task created: " and the " (status=" trailer.
	id := strings.TrimPrefix(out, "Task created: ")
	if i := strings.Index(id, " (status="); i >= 0 {
		id = id[:i]
	}
	if len(id) != 36 || !strings.Contains(id, "-") {
		t.Errorf("auto id = %q, want uuid v7 shape", id)
	}
	// The table + row must really be in the DB file (audit layer = persistent).
	db, err := openTasksDB(t.Context(), path)
	if err != nil {
		t.Fatalf("openTasksDB: %v", err)
	}
	defer db.Close()
	var n int
	if err := db.QueryRow("SELECT COUNT(*) FROM tasks").Scan(&n); err != nil || n != 1 {
		t.Errorf("tasks count = %d (err %v), want 1", n, err)
	}
}

func TestTaskNewExplicitIDAndDuplicate(t *testing.T) {
	testutil.NewTestDBPath(t)

	out, err := TaskNew("first task", "t-fix-001", "", "")
	if err != nil || !strings.Contains(out, "t-fix-001") {
		t.Fatalf("TaskNew explicit: out=%q err=%v", out, err)
	}
	// Duplicate explicit id → error, never silently mangled.
	if _, err := TaskNew("second task", "t-fix-001", "", ""); err == nil || !strings.Contains(err.Error(), "already exists") {
		t.Fatalf("duplicate task_id must error, got %v", err)
	}
}

func TestTaskNewEmptySummary(t *testing.T) {
	testutil.NewTestDBPath(t)

	if _, err := TaskNew("   ", "", "", ""); err == nil || !strings.Contains(err.Error(), "summary") {
		t.Fatalf("empty summary must error, got %v", err)
	}
}

func TestTaskListAllAndStatusFilter(t *testing.T) {
	testutil.NewTestDBPath(t)

	// Three tasks in order; then move t-a to completed and t-b to failed
	// (t-c stays in_progress — the create default).
	for _, id := range []string{"t-a", "t-b", "t-c"} {
		if _, err := TaskNew("task "+id, id, "", ""); err != nil {
			t.Fatalf("TaskNew %s: %v", id, err)
		}
	}
	if _, err := TaskUpdate("t-a", "completed"); err != nil {
		t.Fatalf("TaskUpdate t-a: %v", err)
	}
	if _, err := TaskUpdate("t-b", "failed"); err != nil {
		t.Fatalf("TaskUpdate t-b: %v", err)
	}

	// All: newest first (created_at DESC). t-c is newest.
	all, err := TaskList("")
	if err != nil {
		t.Fatalf("TaskList all: %v", err)
	}
	if !strings.Contains(all, "3 task(s)") {
		t.Errorf("all = %q, want 3-task header", all)
	}
	posC := strings.Index(all, "t-c")
	posB := strings.Index(all, "t-b")
	posA := strings.Index(all, "t-a")
	if !(posC >= 0 && posC < posB && posB < posA) {
		t.Errorf("list order wrong (want t-c before t-b before t-a):\n%s", all)
	}

	// Filter by status: completed → only t-a.
	done, err := TaskList("completed")
	if err != nil {
		t.Fatalf("TaskList completed: %v", err)
	}
	if !strings.Contains(done, "1 task(s) with status=completed") || !strings.Contains(done, "t-a") {
		t.Errorf("completed filter = %q", done)
	}
	// The leak test reads the ROW lines only. Every row starts with "#", and
	// the render also carries a ledger-level NOTE line whose text names the
	// failed-needs-reissue debt by its own ids — a note is not a row, and
	// filtering it away with the rows would hide a failure from the operator
	// exactly where the note is most useful (taskreissue.go). Reading rows is
	// what "the completed filter leaked other statuses" has always meant.
	var rows []string
	for _, ln := range strings.Split(done, "\n") {
		if strings.HasPrefix(ln, TaskListRowPrefix) {
			rows = append(rows, ln)
		}
	}
	if strings.Contains(strings.Join(rows, "\n"), "t-b") || strings.Contains(strings.Join(rows, "\n"), "t-c") {
		t.Errorf("completed filter leaked other statuses:\n%s", done)
	}
}

// TestTaskListHashColumnIsTaskIDDigits pins the cross-node reconciliation
// display fix: the # column renders the task_id's numeric part (t-<unix> →
// <unix>), NOT the local SQLite rowid (which differs per replica node).
func TestTaskListHashColumnIsTaskIDDigits(t *testing.T) {
	testutil.NewTestDBPath(t)

	if _, err := TaskNew("shared task", "t-1788146419", "", ""); err != nil {
		t.Fatalf("TaskNew: %v", err)
	}
	got, err := TaskList("")
	if err != nil {
		t.Fatalf("TaskList: %v", err)
	}
	// The FULL legacy id must appear right after the "#"; the rowid is 1 here
	// but MUST NOT be rendered as #1. The id is NOT truncated to its digits
	// (epic full-id-no-prefix: a truncated id is ambiguous).
	if !strings.Contains(got, "\n#t-1788146419  [") {
		t.Errorf("# column must show the full task_id, got:\n%s", got)
	}
	if strings.Contains(got, "\n#1788146419  ") {
		t.Errorf("# column truncated the legacy id to its digits, got:\n%s", got)
	}
	if strings.Contains(got, "#1  t-1788146419") {
		t.Errorf("# column leaked the local rowid, got:\n%s", got)
	}
	// A uuid auto row renders the FULL id after the "#" marker — and nothing
	// else: the row used to carry a truncated 8-character chip beside the full
	// id, which was an ambiguous token that LOOKED like the row's address
	// (epic full-id-no-prefix). The full id is present verbatim; the chip is
	// NOT present as a bare token.
	if _, err := TaskNew("uuid row", "", "", ""); err != nil {
		t.Fatalf("TaskNew uuid: %v", err)
	}
	got2, err := TaskList("")
	if err != nil {
		t.Fatalf("TaskList: %v", err)
	}
	uuidID := taskExtractID(t, got2, "uuid row")
	if !strings.Contains(got2, "\n#"+uuidID+"  [") {
		t.Errorf("uuid row must render #<full-id>, got:\n%s", got2)
	}
	if strings.Contains(got2, "\n#"+uuidID[:8]+"  ") {
		t.Errorf("uuid row must NOT render a truncated 8-character chip, got:\n%s", got2)
	}
}

// taskExtractID pulls the task_id out of a TaskList line by summary marker.
func taskExtractID(t *testing.T, listOut, summary string) string {
	t.Helper()
	for _, ln := range strings.Split(listOut, "\n") {
		if strings.Contains(ln, summary) {
			f := strings.Fields(ln)
			if len(f) >= 1 {
				// The "#" column IS the id (one identity per row — epic
				// full-id-no-prefix); it used to be a chip in field 0 with the
				// full id in field 1.
				return strings.TrimPrefix(f[0], "#")
			}
		}
	}
	t.Fatalf("no row with %q in list output:\n%s", summary, listOut)
	return ""
}

// TestTaskHashDisplay covers the pure renderer: EVERY id form renders in full.
// It used to truncate (a legacy t-<unix> id to its digits, a uuid to its first
// 8 characters), which is the defect this epic closes — a truncated identity is
// ambiguous (an 8-hex chip can name more than one ledger row) and a resolution
// path that accepted one could return the wrong row.
func TestTaskHashDisplay(t *testing.T) {
	cases := []struct{ in, want string }{
		{"t-1788146419", "t-1788146419"}, // legacy auto: FULL
		{"019c2f1e-4a1b-7f2c-9d3e-8f4a5b6c7d8e", "019c2f1e-4a1b-7f2c-9d3e-8f4a5b6c7d8e"}, // uuid auto: FULL
		{"t-1788146419-1", "t-1788146419-1"},                                             // collision suffix
		{"pa-q-p3-wf", "pa-q-p3-wf"},                                                     // custom (PA) task_id
		{"", ""},                                                                         // empty: stays empty
	}
	for _, c := range cases {
		if got := taskHashDisplay(c.in); got != c.want {
			t.Errorf("taskHashDisplay(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestTaskListInvalidStatus(t *testing.T) {
	testutil.NewTestDBPath(t)

	if _, err := TaskList("done"); err == nil || !strings.Contains(err.Error(), "invalid status") {
		t.Fatalf("invalid status must error, got %v", err)
	}
}

func TestTaskShow(t *testing.T) {
	testutil.NewTestDBPath(t)

	if _, err := TaskNew("inspect this", "t-inspect", "sess-123", ""); err != nil {
		t.Fatalf("TaskNew: %v", err)
	}
	out, err := TaskShow("t-inspect")
	if err != nil {
		t.Fatalf("TaskShow: %v", err)
	}
	for _, want := range []string{"task_id:        t-inspect", "status:         in_progress", "summary:        inspect this", "session_id:     sess-123", "model:          (unknown)", "ended_at:       (not ended)"} {
		if !strings.Contains(out, want) {
			t.Errorf("TaskShow missing %q:\n%s", want, out)
		}
	}
	// Session log path: the row's session_id resolves to a deterministic
	// JSONL path — rendered with the "(not found)" marker when the file is
	// absent (the test sandbox has no session file for sess-123). Session
	// dir unify S1: a bare session's log is sess-123/session.jsonl.
	if !strings.Contains(out, "session_log:") || !strings.Contains(out, filepath.Join("sess-123", "session.jsonl")) {
		t.Errorf("TaskShow missing session_log path line:\n%s", out)
	}
	if !strings.Contains(out, "(not found") {
		t.Errorf("absent session file must show the (not found) marker:\n%s", out)
	}
	// Briefing block is ALWAYS present (task-show-briefing fix): empty
	// payload must render the explicit "(none)" line, never disappear.
	if !strings.Contains(out, "briefing:       (none — summary is the briefing)") {
		t.Errorf("TaskShow missing explicit empty-briefing line:\n%s", out)
	}
	if !strings.Contains(out, "submitted_by:   ") {
		t.Errorf("TaskShow missing submitted_by line:\n%s", out)
	}
}

// TestTaskStampRun: the run-provenance stamp (devdoc/run-method-provenance.md)
// records how a task's execution was launched + the re-runnable command
// line. Semantics pinned: an empty-method stamp is a NO-OP (never wipes an
// earlier stamp), a later real stamp overwrites method AND cmdline
// (last-non-empty-wins — a re-run records ITS launch), and the stamp
// survives writeback (never cleared by the terminal write).
func TestTaskStampRun(t *testing.T) {
	testutil.NewTestDBPath(t)

	if _, err := TaskNew("stamp me", "t-stamp", "sess-1", ""); err != nil {
		t.Fatalf("TaskNew: %v", err)
	}
	// 0) TaskRunProvenance on an unstamped row returns both empty; on a
	// missing row too (the auto-register mirror calls it on a possibly-
	// absent wrapper — must be a silent no-op).
	if m, c := TaskRunProvenance("t-stamp"); m != "" || c != "" {
		t.Fatalf("unstamped provenance = %q/%q, want empty", m, c)
	}
	if m, c := TaskRunProvenance("t-no-such"); m != "" || c != "" {
		t.Fatalf("missing row provenance = %q/%q, want empty", m, c)
	}
	// 1) No-op stamp (empty method) must not wipe anything — the row starts
	// unstamped, stays unstamped.
	if err := TaskStampRun("t-stamp", "", "gopi task run t-stamp"); err != nil {
		t.Fatalf("empty-method stamp must be a silent no-op, got %v", err)
	}
	out, _ := TaskShow("t-stamp") //stylegate:ignore task read after a ledger mutation the test performed; the compared row is the contract
	if strings.Contains(out, "run_method:") {
		t.Fatalf("empty-method stamp must not stamp the row:\n%s", out)
	}
	// 2) First real stamp lands method + cmdline.
	if err := TaskStampRun("t-stamp", "manual", "gopi task run t-stamp"); err != nil {
		t.Fatalf("TaskStampRun: %v", err)
	}
	out, _ = TaskShow("t-stamp") //stylegate:ignore task read after a ledger mutation the test performed; the compared row is the contract
	for _, want := range []string{"run_method:     manual", "run_cmd:        gopi task run t-stamp"} {
		if !strings.Contains(out, want) {
			t.Errorf("TaskShow missing %q after stamp:\n%s", want, out)
		}
	}
	// 2b) TaskRunProvenance now reflects the stamp.
	if m, c := TaskRunProvenance("t-stamp"); m != "manual" || c != "gopi task run t-stamp" {
		t.Fatalf("stamped provenance = %q/%q, want manual/gopi task run t-stamp", m, c)
	}
	// 3) Empty-cmdline stamp keeps the earlier cmdline (never clears), but
	// the method still updates.
	if err := TaskStampRun("t-stamp", "worker", ""); err != nil {
		t.Fatalf("TaskStampRun: %v", err)
	}
	out, _ = TaskShow("t-stamp") //stylegate:ignore task read after a ledger mutation the test performed; the compared row is the contract
	if !strings.Contains(out, "run_method:     worker") || !strings.Contains(out, "run_cmd:        gopi task run t-stamp") {
		t.Errorf("empty cmdline must update method and keep cmdline:\n%s", out)
	}
	// 4) A later full stamp overwrites (last-non-empty-wins).
	if err := TaskStampRun("t-stamp", "resume", "gopi session resume /tmp/raw.log --task t-stamp"); err != nil {
		t.Fatalf("TaskStampRun: %v", err)
	}
	out, _ = TaskShow("t-stamp") //stylegate:ignore task read after a ledger mutation the test performed; the compared row is the contract
	for _, want := range []string{"run_method:     resume", "run_cmd:        gopi session resume /tmp/raw.log --task t-stamp"} {
		if !strings.Contains(out, want) {
			t.Errorf("TaskShow missing %q after re-stamp:\n%s", want, out)
		}
	}
	// 5) Terminal writeback must NOT clear the provenance (stamp-once
	// semantics: run_cmd on a terminal row answers "how did this run").
	if _, err := TaskUpdate("t-stamp", taskStatusCompleted); err != nil {
		t.Fatalf("TaskUpdate completed: %v", err)
	}
	out, _ = TaskShow("t-stamp") //stylegate:ignore task read after a ledger mutation the test performed; the compared row is the contract
	if !strings.Contains(out, "run_method:     resume") || !strings.Contains(out, "status:         completed") {
		t.Errorf("stamp must survive terminal writeback:\n%s", out)
	}
}

// TestTaskShowSessionLogPath: a row whose session JSONL exists renders the
// full tail-able path (the epic's headline ask — `tail -f <path>` on a
// background worker's session).
func TestTaskShowSessionLogPath(t *testing.T) {
	testutil.NewTestDBPath(t)
	sessHome := t.TempDir()
	t.Setenv("HOME", sessHome)
	// Name the gopi data root too: the suite's profile sandbox sets GOPI_HOME
	// (testutil.PinSuiteGopiProfile), which WINS over HOME in config.GopiDir —
	// a fixture that only moved HOME would resolve sessions in the sandbox.
	t.Setenv(config.GopiDirEnv, sessHome)
	sessDir := filepath.Join(sessHome, ".gopi", "sessions")
	if err := os.MkdirAll(sessDir, 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(sessDir, "sess-456.jsonl"),
		[]byte(`{"type":"session_start","sessionId":"sess-456","model":"m","timestamp":"x"}`+"\n"), 0644); err != nil {
		t.Fatalf("write session: %v", err)
	}
	if _, err := TaskNew("[auto] live run", "t-live-456", "sess-456", "m"); err != nil {
		t.Fatalf("TaskNew: %v", err)
	}
	out, err := TaskShow("t-live-456")
	if err != nil {
		t.Fatalf("TaskShow: %v", err)
	}
	want := filepath.Join(sessDir, "sess-456.jsonl")
	if !strings.Contains(out, "session_log:    "+want) {
		t.Errorf("TaskShow missing exact session_log path %q:\n%s", want, out)
	}
	if strings.Contains(out, "(not found") {
		t.Errorf("existing session file must not show (not found):\n%s", out)
	}
	// No rawLogPath recorded in session_start → NO raw_log line (honest
	// absence — pre-resume sessions have no raw log linkage to show).
	if strings.Contains(out, "raw_log:") {
		t.Errorf("session_start without rawLogPath must render no raw_log line:\n%s", out)
	}
	// A row with no session id renders no session_log line at all.
	if _, err := TaskNew("plain row", "t-plain", "", ""); err != nil {
		t.Fatalf("TaskNew plain: %v", err)
	}
	plain, err := TaskShow("t-plain")
	if err != nil {
		t.Fatalf("TaskShow plain: %v", err)
	}
	if strings.Contains(plain, "session_log:") {
		t.Errorf("row with no session_id must render no session_log line:\n%s", plain)
	}
	if strings.Contains(plain, "raw_log:") {
		t.Errorf("row with no session_id must render no raw_log line:\n%s", plain)
	}
}

// TestTaskShowRawLogPath: a row whose session_start records a rawLogPath
// renders the full tail-able raw HTTP log path (this epic's ask — the raw
// log is the byte-exact debug level for `tail -f` on a background run). The
// recorded path is authoritative: it must render VERBATIM even when it lies
// OUTSIDE the default <gopi-dir>/raw (a custom GOPI_RAW_LOG_DIR setup the
// audit layer cannot reconstruct statically).
func TestTaskShowRawLogPath(t *testing.T) {
	testutil.NewTestDBPath(t)
	sessHome := t.TempDir()
	t.Setenv("HOME", sessHome)
	// Name the gopi data root too: the suite's profile sandbox sets GOPI_HOME
	// (testutil.PinSuiteGopiProfile), which WINS over HOME in config.GopiDir —
	// a fixture that only moved HOME would resolve sessions in the sandbox.
	t.Setenv(config.GopiDirEnv, sessHome)
	sessDir := filepath.Join(sessHome, ".gopi", "sessions")
	if err := os.MkdirAll(sessDir, 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	// Custom raw dir OUTSIDE the gopi home — statically unresolvable, the
	// recorded path is the only way to know it.
	customRaw := filepath.Join(t.TempDir(), "custom-raw", "gopi-raw-20260902-215417.log")
	if err := os.MkdirAll(filepath.Dir(customRaw), 0755); err != nil {
		t.Fatalf("mkdir raw dir: %v", err)
	}
	if err := os.WriteFile(customRaw, []byte("→ REQUEST\n"), 0644); err != nil {
		t.Fatalf("write raw: %v", err)
	}
	if err := os.WriteFile(filepath.Join(sessDir, "sess-raw.jsonl"),
		[]byte(`{"type":"session_start","sessionId":"sess-raw","model":"m","rawLogPath":"`+customRaw+`","timestamp":"x"}`+"\n"), 0644); err != nil {
		t.Fatalf("write session: %v", err)
	}
	if _, err := TaskNew("[auto] raw run", "t-raw-789", "sess-raw", "m"); err != nil {
		t.Fatalf("TaskNew: %v", err)
	}
	out, err := TaskShow("t-raw-789")
	if err != nil {
		t.Fatalf("TaskShow: %v", err)
	}
	if !strings.Contains(out, "session_log:    "+filepath.Join(sessDir, "sess-raw.jsonl")) {
		t.Errorf("TaskShow missing exact session_log path:\n%s", out)
	}
	if !strings.Contains(out, "raw_log:        "+customRaw) {
		t.Errorf("TaskShow missing exact raw_log path %q (recorded path must win over static resolution):\n%s", customRaw, out)
	}
	if strings.Contains(out, "(not found") {
		t.Errorf("existing raw file must not show (not found):\n%s", out)
	}
	// Recorded path whose file is gone → path + explicit marker.
	goneRaw := filepath.Join(t.TempDir(), "gone-raw.log")
	if err := os.WriteFile(filepath.Join(sessDir, "sess-raw2.jsonl"),
		[]byte(`{"type":"session_start","sessionId":"sess-raw2","model":"m","rawLogPath":"`+goneRaw+`","timestamp":"x"}`+"\n"), 0644); err != nil {
		t.Fatalf("write session2: %v", err)
	}
	if _, err := TaskNew("[auto] raw gone", "t-raw-gone", "sess-raw2", "m"); err != nil {
		t.Fatalf("TaskNew: %v", err)
	}
	out2, err := TaskShow("t-raw-gone")
	if err != nil {
		t.Fatalf("TaskShow: %v", err)
	}
	if !strings.Contains(out2, "raw_log:        "+goneRaw+" (not found") {
		t.Errorf("missing raw file must render path with (not found) marker:\n%s", out2)
	}
}

// TestTaskShowBriefingFromPayload: a task_submit-style row with a payload
// shows the payload verbatim as its briefing block (the payload IS the
// briefing — TaskSubmitOptions.Payload docs).
func TestTaskShowBriefingFromPayload(t *testing.T) {
	testutil.NewTestDBPath(t)
	testutil.PinBareTerminalProcess(t)

	if _, err := TaskSubmit(TaskSubmitOptions{
		Summary: "fix the login timeout",
		Payload: "Step 1: repro with a 5s budget.\nStep 2: root-cause the scheduler poll.",
		Source:  "human",
		TaskID:  "t-brief-pay",
	}); err != nil {
		t.Fatalf("TaskSubmit: %v", err)
	}
	out, err := TaskShow("t-brief-pay")
	if err != nil {
		t.Fatalf("TaskShow: %v", err)
	}
	// The briefing is AUTHORED text and therefore rides the quote gutter (see
	// taskShowQuotedIndent): the payload is rendered verbatim, one level in —
	// never at column 0, where a grep for a digest field could not tell it
	// from one (the 2026-10-29 defect).
	for _, want := range []string{
		"briefing:\n" + taskShowQuotedIndent + "Step 1: repro",
		taskShowQuotedIndent + "Step 2: root-cause the scheduler poll.",
		"submitted_by:   human (gopi task submit CLI)",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("TaskShow missing %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "(none — summary is the briefing)") {
		t.Errorf("payload row must not show the empty-briefing line:\n%s", out)
	}
}

// TestTaskShowBriefingFromSession: an auto-registered run row (payload=”,
// summary=[auto] <codename>) recovers its briefing from the session JSONL's
// first user message — the exact e3ca35ca incident shape, where a code-name
// summary alone forced a memory_search round-trip to learn the task.
func TestTaskShowBriefingFromSession(t *testing.T) {
	testutil.NewTestDBPath(t)
	testutil.PinBareTerminalProcess(t)
	sessHome := t.TempDir()
	t.Setenv("HOME", sessHome)
	// Name the gopi data root too: the suite's profile sandbox sets GOPI_HOME
	// (testutil.PinSuiteGopiProfile), which WINS over HOME in config.GopiDir —
	// a fixture that only moved HOME would resolve sessions in the sandbox.
	t.Setenv(config.GopiDirEnv, sessHome)
	sessDir := filepath.Join(sessHome, ".gopi", "sessions")
	if err := os.MkdirAll(sessDir, 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	sessID := "e3ca35ca-fixture"
	briefing := "[EPIC GOAL]\npa-q-p3-wf-contract\n\nBased on this Epic, establish an initial task list as a PLAN (first substantive round): emit it as [TASK_LIST_BEGIN]...[TASK_LIST_END], and re-emit only when it changes."
	userLine, err := json.Marshal(map[string]any{
		"type": "user",
		"message": map[string]any{
			"content": briefing,
			"role":    "user",
		},
	})
	if err != nil {
		t.Fatalf("marshal user line: %v", err)
	}
	if err := os.WriteFile(filepath.Join(sessDir, sessID+".jsonl"),
		[]byte(`{"type":"session_start","sessionId":"`+sessID+`","model":"glm-test","timestamp":"x"}`+"\n"+
			string(userLine)+"\n"+
			`{"message":{"content":[{"thinking":"go"}],"role":"assistant"},"type":"assistant"}`+"\n"), 0644); err != nil {
		t.Fatalf("write session: %v", err)
	}
	// The row under test IS an auto-registration row (a bare-prompt run that
	// registered itself at start), so the fixture writes the role that
	// registration writes — internal/agent/taskregister.go's tasksource.Server
	// (Rule 14: the role is spelled once, by its writer). Provenance is DERIVED
	// from the process signature (be17a9bd), so a fixture that left the source
	// empty would be born with whatever role the RUNNER's environment implies —
	// and the assertion below would be measuring the runner, not the renderer.
	//
	// testutil.PinBareTerminalProcess above is what makes this fixture's rendering
	// independent of the runner's own session env (family A of
	// devdoc/internal-tools-whole-package-triage-20260925.md), and the submitted_by
	// assertion below is written as the value the ONE renderer derives for the row
	// this fixture builds — so a future edit that re-couples the contract to worker
	// provenance shows up as a changed value here, in BOTH environments.
	if _, err := TaskNewOID(TaskNewOptions{
		Summary:   "[auto] pa-q-p3-wf-contract — elapsed=721.7s",
		TaskID:    "e3ca35ca-fixture",
		SessionID: sessID,
		Model:     "glm-test",
		Source:    tasksource.Server,
	}); err != nil {
		t.Fatalf("TaskNew: %v", err)
	}
	out, err := TaskShow("e3ca35ca-fixture")
	if err != nil {
		t.Fatalf("TaskShow: %v", err)
	}
	if !strings.Contains(out, "briefing:       (from session JSONL)") {
		t.Errorf("TaskShow missing session-derived briefing marker:\n%s", out)
	}
	// The [EPIC GOAL] wrapper and the appended framing paragraph are kernel
	// chrome — the user's actual task text must survive them.
	if !strings.Contains(out, "pa-q-p3-wf-contract") || strings.Contains(out, "[EPIC GOAL]") {
		t.Errorf("briefing not stripped of framing or codename lost:\n%s", out)
	}
	if strings.Contains(out, "Based on this Epic") {
		t.Errorf("framing paragraph leaked into briefing:\n%s", out)
	}
	wantSubmittedBy := taskSubmittedBy(&Task{Source: tasksource.Server, SessionID: sessID, TaskID: "e3ca35ca-fixture"})
	if !strings.Contains(out, "submitted_by:   "+wantSubmittedBy) {
		t.Errorf("auto row must explain itself as the run row (submitted_by want %q):\n%s", wantSubmittedBy, out)
	}
}

// testShowBriefingBudget is how many bytes of `task show` output this arm
// allows for the BRIEFING's own rendering plus the lines every row prints.
//
// It is a named constant (Rule 1/13) because the previous spelling was the bare
// expression `2*taskBriefingMaxChars+600`, and that expression stopped being a
// budget when the renderer grew rows: measured on 64301ad the fixture is 2310
// bytes, of which the briefing itself is 844 (800 + 44 of taskBriefingEllipsis),
// so the briefing is capped exactly as designed and the 110-byte excess came
// from the 280-byte `ground (resolved now): …` advisory and the 205-byte
// `run_dir: …` line — outputs this arm neither owns nor asserts. Anchoring the
// budget to the briefing's own constant keeps the arm measuring truncation
// instead of measuring how many rows `task show` happens to render.
const (
	// testShowBriefingChars is what the briefing line itself may carry: the
	// cap, plus the ellipsis that replaces the remainder.
	testShowBriefingChars = taskBriefingMaxChars + len(" [truncated]")

	// testShowFixedRowBytes is the room the fixed `label: value` rows around
	// the briefing get — status, summary, session_id, model, submitted_by,
	// the ground advisory, liveness, created_at and the rest of the record.
	// Measured on 64301ad they come to 1126 bytes for this fixture's row, so
	// the allowance is a stated budget rather than that measurement: it leaves
	// room for one more diagnostic row, and a row that starts rendering
	// multi-KB prose is exactly what this arm refuses.
	testShowFixedRowBytes = 1400

	// testShowPathSlack is the room the two long values this fixture prints
	// get BEYOND the fixture's own sessions directory — `<id>.jsonl` for
	// `session_log:` and a timestamped run directory for `run_dir:`. Stated
	// as a constant so the arm bounds those rows by their SHAPE instead of
	// pinning a byte count that a host with longer temp paths would break.
	testShowPathSlack = 128
)

// testShowLongestRun returns the length of the longest run of ch in s. The
// briefing assertion needs a RUN and not a character count: the fixture's text
// is all 'x', but the record renders other lines too, and one of them — the
// truncation ellipsis — spells the letter 'x' inside the words it carries
// ("full text:"). Counting characters therefore measured the ellipsis as if it
// were briefing, which is precisely the coupling this arm exists to avoid.
func testShowLongestRun(s string, ch byte) int {
	best, cur := 0, 0
	for i := 0; i < len(s); i++ {
		if s[i] == ch {
			cur++
			if cur > best {
				best = cur
			}
			continue
		}
		cur = 0
	}
	return best
}

// TestTaskShowBriefingTruncated: a long session briefing is capped at
// taskBriefingMaxChars so show output stays readable.
func TestTaskShowBriefingTruncated(t *testing.T) {
	testutil.NewTestDBPath(t)
	sessHome := t.TempDir()
	t.Setenv("HOME", sessHome)
	// Name the gopi data root too: the suite's profile sandbox sets GOPI_HOME
	// (testutil.PinSuiteGopiProfile), which WINS over HOME in config.GopiDir —
	// a fixture that only moved HOME would resolve sessions in the sandbox.
	t.Setenv(config.GopiDirEnv, sessHome)
	sessDir := filepath.Join(sessHome, ".gopi", "sessions")
	if err := os.MkdirAll(sessDir, 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	sessID := "trunc-fixture"
	long := strings.Repeat("x", taskBriefingMaxChars+50)
	userLine, err := json.Marshal(map[string]any{
		"type": "user",
		"message": map[string]any{
			"content": long,
			"role":    "user",
		},
	})
	if err != nil {
		t.Fatalf("marshal user line: %v", err)
	}
	if err := os.WriteFile(filepath.Join(sessDir, sessID+".jsonl"),
		[]byte(`{"type":"session_start","sessionId":"`+sessID+`","model":"m","timestamp":"x"}`+"\n"+
			string(userLine)+"\n"), 0644); err != nil {
		t.Fatalf("write session: %v", err)
	}
	if _, err := TaskNew("[auto] long", "trunc-fixture", sessID, "m"); err != nil {
		t.Fatalf("TaskNew: %v", err)
	}
	out, err := TaskShow("trunc-fixture")
	if err != nil {
		t.Fatalf("TaskShow: %v", err)
	}
	if !strings.Contains(out, taskBriefingEllipsis) {
		t.Errorf("long briefing must be truncated:\n%s", out)
	}
	// The subject, asserted EXACTLY: the fixture's briefing is `long`, made of
	// 'x' and nothing else, so the number of x's that survive is the cap —
	// no budget, no host-dependent path, no coupling to how many rows the
	// record renders. A renderer that stops truncating puts
	// taskBriefingMaxChars+50 here and fails on the number, not on a total.
	if got := testShowLongestRun(out, 'x'); got != taskBriefingMaxChars {
		t.Errorf("the briefing renders %d consecutive bytes of the fixture text, want exactly taskBriefingMaxChars=%d: "+
			"the cap is what keeps `task show` readable, and the ellipsis is asserted separately above:\n%s",
			got, taskBriefingMaxChars, out)
	}

	// The second subject, a readability bound: the briefing line (capped), the
	// fixed rows, and the two long values this fixture prints by design — the
	// per-test sessions path in `session_log:` and `run_dir:`. Those are
	// measured from the fixture (len(sessDir)) rather than pinned, because
	// t.TempDir() decides them on the host.
	budget := testShowBriefingChars + testShowFixedRowBytes + 2*(len(sessDir)+testShowPathSlack)
	if len(out) > budget {
		t.Errorf("show output unexpectedly large (%d bytes, budget %d = %d briefing + %d fixed rows + 2×(%d fixture path + %d slack)); "+
			"the briefing is capped, so a hit here means a rendered ROW grew, not that truncation stopped:\n%s",
			len(out), budget, testShowBriefingChars, testShowFixedRowBytes, len(sessDir), testShowPathSlack, out)
	}
}

// TestSubmittedByProvenance: each source renders an honest submitted_by.
func TestSubmittedByProvenance(t *testing.T) {
	cases := []struct {
		src, sess, taskID, want string
	}{
		{"llm", "submitter-sess", "t-child", "LLM session submitter-sess (task_submit)"},
		{"human", "", "t-cli", "human (gopi task submit CLI)"},
		{"auto", "run-sess", "t-run", "harness auto-registration (row IS the run: session run-sess)"},
		{"auto", "", "pa-q-external", "harness/external tooling (source=auto, no session recorded)"},
	}
	for _, c := range cases {
		got := taskSubmittedBy(&Task{Source: c.src, SessionID: c.sess, TaskID: c.taskID})
		if got != c.want {
			t.Errorf("source=%q: got %q, want %q", c.src, got, c.want)
		}
	}
}

// TestTaskShowBriefingMissingSession: no session file → the explicit
// "(none)" line (a pending pa-q-style external row has no session to read).
func TestTaskShowBriefingMissingSession(t *testing.T) {
	testutil.NewTestDBPath(t)
	testutil.PinBareTerminalProcess(t)
	sessHome := t.TempDir()
	t.Setenv("HOME", sessHome)
	// Name the gopi data root too: the suite's profile sandbox sets GOPI_HOME
	// (testutil.PinSuiteGopiProfile), which WINS over HOME in config.GopiDir —
	// a fixture that only moved HOME would resolve sessions in the sandbox.
	t.Setenv(config.GopiDirEnv, sessHome)

	// A pa-q row is machine-ENQUEUED by external tooling and carries no
	// session — that is exactly why the renderer labels it "harness/external
	// tooling", so the fixture writes the role such a row has (tasksource.Merger
	// is the ledger's machine-enqueue value; tasksource's package doc: "auto"
	// meant the merge path or the harness, and its majority writer was the
	// former).
	if _, err := TaskNewOID(TaskNewOptions{
		Summary: "[PA-QUEUE][P3] chat/web 内嵌 poll 循环",
		TaskID:  "pa-q-p3-serve-embed",
		Source:  tasksource.Merger,
	}); err != nil {
		t.Fatalf("TaskNew: %v", err)
	}
	out, err := TaskShow("pa-q-p3-serve-embed")
	if err != nil {
		t.Fatalf("TaskShow: %v", err)
	}
	if !strings.Contains(out, "briefing:       (none — summary is the briefing)") {
		t.Errorf("missing-session row must show explicit empty briefing:\n%s", out)
	}
	wantSubmittedBy := taskSubmittedBy(&Task{Source: tasksource.Merger, TaskID: "pa-q-p3-serve-embed"})
	if !strings.Contains(out, "submitted_by:   "+wantSubmittedBy) {
		t.Errorf("external row must be labeled as such (submitted_by want %q):\n%s", wantSubmittedBy, out)
	}
}

// TestTaskShowModelFromSession verifies the LEGACY FALLBACK: a task row
// created before model persistence (tasks.model=”) still resolves its model
// at show time from the session's JSONL session_start event. Rows created
// from now on carry the model column (see TestTaskNewModelPersisted) — the
// DB value wins and this lookup never fires.
func TestTaskShowModelFromSession(t *testing.T) {
	testutil.NewTestDBPath(t)

	// Point HOME at a temp dir so the session lookup hits our fixture only,
	// never the developer's real ~/.gopi/sessions.
	sessHome := t.TempDir()
	t.Setenv("HOME", sessHome)
	// Name the gopi data root too: the suite's profile sandbox sets GOPI_HOME
	// (testutil.PinSuiteGopiProfile), which WINS over HOME in config.GopiDir —
	// a fixture that only moved HOME would resolve sessions in the sandbox.
	t.Setenv(config.GopiDirEnv, sessHome)
	sessDir := filepath.Join(sessHome, ".gopi", "sessions")
	if err := os.MkdirAll(sessDir, 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	sessID := "abc-123"
	if err := os.WriteFile(filepath.Join(sessDir, sessID+".jsonl"),
		[]byte(`{"type":"session_start","sessionId":"abc-123","model":"deepseek-test-md","timestamp":"x"}`+"\n"), 0644); err != nil {
		t.Fatalf("write session: %v", err)
	}

	if _, err := TaskNew("inspect this", "t-model", sessID, ""); err != nil {
		t.Fatalf("TaskNew: %v", err)
	}
	out, err := TaskShow("t-model")
	if err != nil {
		t.Fatalf("TaskShow: %v", err)
	}
	if !strings.Contains(out, "model:          deepseek-test-md") {
		t.Errorf("TaskShow missing resolved model:\n%s", out)
	}
	// The fallback row has NO persisted model — list (DB truth only) shows
	// (unknown); only show consults the session JSONL.
	list, err := TaskList("")
	if err != nil {
		t.Fatalf("TaskList: %v", err)
	}
	if !strings.Contains(list, "(model (unknown),") {
		t.Errorf("TaskList should render (unknown) for a pre-persistence row:\n%s", list)
	}
}

func TestTaskShowNotFound(t *testing.T) {
	testutil.NewTestDBPath(t)

	if _, err := TaskShow("t-ghost"); err == nil || !strings.Contains(err.Error(), "no task with task_id") {
		t.Fatalf("missing task must error, got %v", err)
	}
}

// TestTaskShowNamesTheCanonicalSpellingForANearMiss is the LEDGER-face half of
// the widened shape rule. `task show 01a12f96-c2bf-732f-8cd24b63a367d4af` (one
// dash dropped) resolved to no row and said only so — the run that reported
// "files vanished from a live session directory" (bug row 01a12fd9) started
// from exactly this answer, because the spelling it typed is the one whose
// container was minted and the rows live under the canonical one. The refusal
// must now NAME the repair, and the id it names must still be queryable — a
// "did you mean" that is itself unresolvable is not a repair.
func TestTaskShowNamesTheCanonicalSpellingForANearMiss(t *testing.T) {
	testutil.NewTestDBPath(t)

	const (
		canonical = "01a12f96-c2bf-732f-8cd2-4b63a367d4af"
		nearMiss  = "01a12f96-c2bf-732f-8cd24b63a367d4af"
	)
	if _, err := TaskNew("shape gate: the row lives under the canonical spelling", canonical, "", ""); err != nil {
		t.Fatalf("TaskNew: %v", err)
	}

	_, err := TaskShow(nearMiss)
	if err == nil {
		t.Fatalf("TaskShow(%q) succeeded — a non-canonical spelling must not resolve", nearMiss)
	}
	if !strings.Contains(err.Error(), canonical) {
		t.Errorf("TaskShow(%q) = %v, want the canonical spelling %q named as the repair", nearMiss, err, canonical)
	}

	// The repair is reachable: the named id really is the row.
	shown, err := TaskShow(canonical)
	if err != nil {
		t.Fatalf("TaskShow(%q) [the id the refusal names]: %v", canonical, err)
	}
	if !strings.Contains(shown, canonical) {
		t.Errorf("TaskShow(%q) = %q, want the row's own digest", canonical, shown)
	}

	// A non-uuid id that resolves to no row keeps its ORDINARY answer — no
	// invented hint, no widening of what the ledger refuses (Rule 15: a wrong
	// "did you mean" is worse than none).
	if _, err := TaskShow("01a11548-tracked"); err == nil || strings.Contains(err.Error(), "did you mean") {
		t.Errorf("TaskShow(01a11548-tracked) = %v, want the ordinary no-such-row answer with no near-miss invented", err)
	}
}

// TestTaskShowDoesNotOfferASpellingRepairTheLedgerCannotMake is the repro of
// bug 01a130e4 (measured 2026-10-12) and the other half of the clause above: a
// SHAPE is not a repair. Two operands used to get the spelling advice while the
// ledger held neither of them —
//
//	(a) `00000000-0000-0000-0000-000000000000`, a uuid ALREADY in the canonical
//	    spelling: nothing can be re-grouped, so the hint quoted the operand
//	    ITSELF back ("did you mean \"00000000-0000-0000-0000-000000000000\"?"),
//	    and the true answer — this row is not in THIS ledger — was replaced by
//	    advice to fix a spelling that was never wrong (the reporting run went
//	    looking for a typo; the real reason was another host's ledger);
//	(b) a real near miss whose canonical row lives on a PEER's ledger: the
//	    re-spelling is a real change, but this ledger cannot resolve it, so the
//	    "did you mean" names an id that answers no task here.
//
// The clause prints only when the named spelling differs AND the ledger holds
// it — the same defect one step in, on the same seam.
func TestTaskShowDoesNotOfferASpellingRepairTheLedgerCannotMake(t *testing.T) {
	testutil.NewTestDBPath(t)

	for _, canonicalUnknown := range []string{
		"00000000-0000-0000-0000-000000000000",
		"01a12cb3-53c8-720b-a04e-bccdbd538ba2", // the row that lived on a peer's ledger
	} {
		_, err := TaskShow(canonicalUnknown)
		if err == nil || !strings.Contains(err.Error(), "no task with task_id") {
			t.Fatalf("TaskShow(%q) = %v, want the ordinary no-such-row answer", canonicalUnknown, err)
		}
		if strings.Contains(err.Error(), "did you mean") || strings.Contains(err.Error(), "non-canonical") {
			t.Errorf("TaskShow(%q) = %v — a canonical spelling has no other spelling to offer; the row is simply not in this ledger, and the spelling clause must not print", canonicalUnknown, err)
		}
	}

	// (b) A genuine near miss (dashes moved) whose canonical row this ledger does
	// NOT hold: re-spelling is a real change, but naming it would not repair
	// anything here.
	const absentNearMiss = "01a131e2-953f76e59839-ea82c17c2e35"
	_, err := TaskShow(absentNearMiss)
	if err == nil || !strings.Contains(err.Error(), "no task with task_id") {
		t.Fatalf("TaskShow(%q) = %v, want the ordinary no-such-row answer", absentNearMiss, err)
	}
	if strings.Contains(err.Error(), "did you mean") {
		t.Errorf("TaskShow(%q) = %v — the canonical row is not in this ledger, so the named repair resolves to nothing; no advice", absentNearMiss, err)
	}
}

func TestTaskUpdateLifecycle(t *testing.T) {
	testutil.NewTestDBPath(t)

	if _, err := TaskNew("lifecycle task", "t-life", "", ""); err != nil {
		t.Fatalf("TaskNew: %v", err)
	}
	// Terminal status stamps ended_at.
	if _, err := TaskUpdate("t-life", "completed"); err != nil {
		t.Fatalf("TaskUpdate completed: %v", err)
	}
	out, err := TaskShow("t-life")
	if err != nil {
		t.Fatalf("TaskShow: %v", err)
	}
	if !strings.Contains(out, "status:         completed") || !strings.Contains(out, "ended_at:       20") {
		t.Errorf("terminal update must stamp ended_at:\n%s", out)
	}
	// M4.20-B: completed → in_progress is now REFUSED (the rank ladder has
	// no backward exits from the terminal tier — reissue, never reopen).
	if _, err := TaskUpdate("t-life", "in_progress"); err == nil {
		t.Errorf("completed → in_progress must be refused under the rank model")
	}
	out, err = TaskShow("t-life")
	if err != nil {
		t.Fatalf("TaskShow: %v", err)
	}
	if !strings.Contains(out, "ended_at:       20") {
		t.Errorf("a refused backward move must keep ended_at:\n%s", out)
	}
}

// TestTaskUpdateSummaryOnly: M4.5 ⑤ flips this test's semantics — the
// summary is now IMMUTABLE (cancel and re-submit), so the old successful
// edit is asserted as a refusal, and the row keeps its original content.
func TestTaskUpdateSummaryOnly(t *testing.T) {
	testutil.NewTestDBPath(t)

	if _, err := TaskNew("old summary", "t-sum", "", ""); err != nil {
		t.Fatalf("TaskNew: %v", err)
	}
	if _, err := TaskUpdate("t-sum", ""); err == nil ||
		!strings.Contains(err.Error(), "nothing to update") {
		t.Fatalf("summary-only update must be refused, got err=%v", err)
	}
	if _, err := TaskUpdateWithResult("t-sum", "", "x"); err == nil ||
		!strings.Contains(err.Error(), "nothing to update") {
		t.Fatalf("status-empty TaskUpdateWithResult must be refused, got err=%v", err)
	}
	out, err := TaskShow("t-sum")
	if err != nil {
		t.Fatalf("TaskShow: %v", err)
	}
	if !strings.Contains(out, "summary:        old summary") {
		t.Errorf("summary must be immutable (kept the original):\n%s", out)
	}
	// A refused edit must not stamp ended_at.
	if !strings.Contains(out, "ended_at:       (not ended)") {
		t.Errorf("refused edit must not stamp ended_at:\n%s", out)
	}
}

// TestTaskUpdateWithResultKeepsSummaryImmutable is the bug056 regression
// (Epic requirement D): the harness writeback (TaskUpdateWithResult — task
// run's completed/failed + the auto-register end hook) must NOT overwrite
// the submit-time summary. The result digest lands in the append-only
// task_events trail, and TaskShow renders it as the `result:` line — the
// operator still sees the run stats, the summary keeps the payload digest.
func TestTaskUpdateWithResultKeepsSummaryImmutable(t *testing.T) {
	testutil.NewTestDBPath(t)

	submitSummary := "task-merge-CLI EPIC auto mirror — the original briefing text"
	if _, err := TaskSubmit(TaskSubmitOptions{
		Summary: submitSummary,
		Payload: "briefing body",
		TaskID:  "t-bug056",
		// provenance — inserted at birth, never defaulted: see insertTaskRow.
		Source: "auto",
	}); err != nil {
		t.Fatalf("TaskSubmit: %v", err)
	}
	// The full run lifecycle: claim → in_progress → completed writeback
	// (the exact writebackOutcome shape from runTaskRun).
	if _, err := TaskUpdate("t-bug056", TaskStatusInProgress); err != nil {
		t.Fatalf("in_progress: %v", err)
	}
	if _, err := TaskRunWriteback("t-bug056", TaskStatusCompleted,
		"gopi run completed — run: elapsed=2958.7s iter=307 tokens=25639983"); err != nil {
		t.Fatalf("writeback: %v", err)
	}

	out, err := TaskShow("t-bug056")
	if err != nil {
		t.Fatalf("TaskShow: %v", err)
	}
	// ① The summary is EXACTLY the submit-time digest — the writeback's
	// "gopi run completed" text must NOT appear in it (the digest only
	// shows on the result line below).
	if !strings.Contains(out, "summary:        "+submitSummary) {
		t.Errorf("summary must keep the submit-time value verbatim:\n%s", out)
	}
	summaryLine := "summary:        " + submitSummary
	if !strings.Contains(out, summaryLine+"\n") {
		t.Errorf("summary line must be exactly the submit-time digest:\n%s", out)
	}
	if strings.Contains(out, "summary:        gopi run completed") {
		t.Errorf("writeback digest leaked into the immutable summary:\n%s", out)
	}
	// ② The run stats ARE visible — in the independent task_events carrier
	// (show's result line).
	if !strings.Contains(out, "result:         gopi run completed — run: elapsed=2958.7s iter=307 tokens=25639983") {
		t.Errorf("writeback digest must be visible as the result line:\n%s", out)
	}
	if !strings.Contains(out, "status:         completed") {
		t.Errorf("writeback must flip the status:\n%s", out)
	}

	// ③ FTS consistency: search still matches the ORIGINAL summary, not the
	// writeback digest (Epic C: events are audit data, not indexed).
	search, err := TaskSearch("task-merge-CLI EPIC auto mirror")
	if err != nil || !strings.Contains(search, "t-bug056") {
		t.Errorf("search must hit the immutable summary: %q err=%v", search, err)
	}
	search2, err := TaskSearch("tokens=25639983")
	if err != nil || strings.Contains(search2, "t-bug056") {
		t.Errorf("search must NOT hit the audit-trail stats: %q err=%v", search2, err)
	}

	// ④ A second writeback (idempotent same-status no-op) still keeps the
	// summary untouched and appends another event row (the audit trail is
	// append-only — history is never rewritten).
	if _, err := TaskRunWriteback("t-bug056", TaskStatusCompleted, "gopi run completed — run: elapsed=1s"); err != nil {
		t.Fatalf("writeback 2: %v", err)
	}
	out, err = TaskShow("t-bug056")
	if err != nil {
		t.Fatalf("TaskShow 2: %v", err)
	}
	if !strings.Contains(out, "summary:        "+submitSummary) {
		t.Errorf("summary mutated by a second writeback:\n%s", out)
	}
	// The latest event's detail is what show renders (elapsed=1s wins).
	if !strings.Contains(out, "result:         gopi run completed — run: elapsed=1s") {
		t.Errorf("show must render the LATEST event detail:\n%s", out)
	}
}

func TestTaskUpdateErrors(t *testing.T) {
	testutil.NewTestDBPath(t)

	if _, err := TaskNew("x", "t-x", "", ""); err != nil {
		t.Fatalf("TaskNew: %v", err)
	}
	if _, err := TaskUpdate("t-x", ""); err == nil || !strings.Contains(err.Error(), "nothing to update") {
		t.Fatalf("empty update must error, got %v", err)
	}
	if _, err := TaskUpdate("t-x", "done"); err == nil || !strings.Contains(err.Error(), "invalid status") {
		t.Fatalf("invalid status must error, got %v", err)
	}
	if _, err := TaskUpdate("t-ghost", "completed"); err == nil || !strings.Contains(err.Error(), "no task with task_id") {
		t.Fatalf("missing task must error, got %v", err)
	}
}

// ---------------------------------------------------------------------------
// Model-as-DB-column tests (root-cause fix for the task-show-model epic,
// superseding 19a6eab's show-time-only resolution): the model is captured at
// task-creation time and persisted in tasks.model; list/show render it from
// the DB.
// ---------------------------------------------------------------------------

// TestTaskNewModelPersisted: the model passed at creation is stored in the
// row and rendered by BOTH TaskList and TaskShow straight from the DB — no
// session JSONL involved (taskModelFromSession must never fire for a row
// that has a persisted model, even when the session log is missing).
func TestTaskNewModelPersisted(t *testing.T) {
	testutil.NewTestDBPath(t)

	// No session file exists for sess-persist — only the DB column can
	// answer where this model came from.
	if _, err := TaskNew("inspect this", "t-persist", "sess-persist", "deepseek-v4-flash"); err != nil {
		t.Fatalf("TaskNew: %v", err)
	}
	list, err := TaskList("")
	if err != nil {
		t.Fatalf("TaskList: %v", err)
	}
	if !strings.Contains(list, "(model deepseek-v4-flash,") {
		t.Errorf("TaskList missing persisted model:\n%s", list)
	}
	out, err := TaskShow("t-persist")
	if err != nil {
		t.Fatalf("TaskShow: %v", err)
	}
	if !strings.Contains(out, "model:          deepseek-v4-flash") {
		t.Errorf("TaskShow missing persisted model:\n%s", out)
	}
	if strings.Contains(out, "(unknown)") {
		t.Errorf("persisted model must not fall through to (unknown):\n%s", out)
	}
}

// TestTaskNewDefaultModelEmpty: omitting --model persists ” (rendered as
// "(unknown)") — an empty model is a valid, non-error state.
func TestTaskNewDefaultModelEmpty(t *testing.T) {
	testutil.NewTestDBPath(t)

	if _, err := TaskNew("no model given", "t-nomodel", "", ""); err != nil {
		t.Fatalf("TaskNew: %v", err)
	}
	list, err := TaskList("")
	if err != nil {
		t.Fatalf("TaskList: %v", err)
	}
	if !strings.Contains(list, "(model (unknown),") {
		t.Errorf("TaskList should render (unknown) for empty model:\n%s", list)
	}
}

// TestEnsureTasksSchemaMigratesModelColumn: a PRE-MODEL ledger (a tasks
// table without the model column, like every DB created before this fix)
// must be widened in place by ensureTasksSchema — new tasks then persist a
// model, and old rows read back as ” (rendered "(unknown)"). The migration
// is idempotent: a second ensure must stay a no-op.
func TestEnsureTasksSchemaMigratesModelColumn(t *testing.T) {
	path := testutil.NewTestDBPath(t)
	ctx := t.Context()

	// Create a task (full current schema), then simulate a pre-model DB by
	// rebuilding the table WITHOUT the model column and re-inserting the
	// row in the old shape (the append-only ledger is never rewritten in
	// production — this is only the test fixture).
	if _, err := TaskNew("legacy row", "t-legacy", "", ""); err != nil {
		t.Fatalf("TaskNew legacy: %v", err)
	}
	db, err := openTasksDB(ctx, path)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if _, err := db.ExecContext(ctx, `CREATE TABLE tasks_old_shape (
	id         INTEGER PRIMARY KEY AUTOINCREMENT,
	task_id    TEXT NOT NULL UNIQUE,
	summary    TEXT NOT NULL,
	status     TEXT NOT NULL DEFAULT 'in_progress'
	           CHECK (status IN ('in_progress','completed','failed','cancelled')),
	session_id TEXT DEFAULT '',
	created_at REAL NOT NULL,
	updated_at REAL NOT NULL,
	ended_at   REAL
)`); err != nil {
		t.Fatalf("create old shape: %v", err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO tasks_old_shape
		SELECT id, task_id, summary, status, session_id, created_at, updated_at, ended_at FROM tasks`); err != nil {
		t.Fatalf("copy row: %v", err)
	}
	if _, err := db.ExecContext(ctx, `DROP TABLE tasks`); err != nil {
		t.Fatalf("drop: %v", err)
	}
	if _, err := db.ExecContext(ctx, `ALTER TABLE tasks_old_shape RENAME TO tasks`); err != nil {
		t.Fatalf("rename: %v", err)
	}
	db.Close() //stylegate:ignore process-lifetime handle closed at shutdown; the OS reclaims the descriptor

	// Re-open: ensureTasksSchema must add the missing model column.
	db, err = openTasksDB(ctx, path)
	if err != nil {
		t.Fatalf("reopen (migration must succeed): %v", err)
	}
	defer db.Close()
	rows, err := db.QueryContext(ctx, `PRAGMA table_info(tasks)`)
	if err != nil {
		t.Fatalf("table_info: %v", err)
	}
	defer rows.Close()
	hasModel := false
	hasReason := false
	for rows.Next() {
		var cid int
		var name, ctype string
		var notnull int
		var dfltValue any
		var pk int
		if err := rows.Scan(&cid, &name, &ctype, &notnull, &dfltValue, &pk); err != nil {
			t.Fatalf("scan table_info: %v", err)
		}
		if name == "model" {
			hasModel = true
		}
		if name == "reason" {
			hasReason = true
		}
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("rows: %v", err)
	}
	if !hasModel {
		t.Fatalf("ensureTasksSchema did not add tasks.model column")
	}
	if !hasReason {
		t.Fatalf("ensureTasksSchema did not add tasks.reason column (task-update-reason epic)")
	}
	// Old rows (no model at insert time) read back as '' → "(unknown)".
	out, err := TaskShow("t-legacy")
	if err != nil {
		t.Fatalf("TaskShow legacy row: %v", err)
	}
	if !strings.Contains(out, "task_id:        t-legacy") || !strings.Contains(out, "model:          (unknown)") {
		t.Errorf("legacy row must survive migration as (unknown):\n%s", out)
	}
	// A fresh task on the migrated DB persists its model (capture-at-create
	// works on the widened table).
	if _, err := TaskNew("after migration", "t-after", "", "glm-5"); err != nil {
		t.Fatalf("TaskNew after migration: %v", err)
	}
	out, err = TaskShow("t-after")
	if err != nil {
		t.Fatalf("TaskShow after: %v", err)
	}
	if !strings.Contains(out, "model:          glm-5") {
		t.Errorf("post-migration model capture failed:\n%s", out)
	}
}

// ---------------------------------------------------------------------------
// task submit --model + writeback model-backfill tests (task-submit-model
// epic): ① the CLI's --model flag lands in tasks.model at submit; an
// omitted flag leaves it EMPTY. ② the writeback path
// (TaskUpdateWithResult) backfills the EXECUTING session's model from the
// child session JSONL's session_start event — best-effort, the
// session_id-stamp precedent — never overwriting a submit-time value.
// ---------------------------------------------------------------------------

// TestTaskSubmitModelFlag: --model writes the row's model column verbatim;
// omitting it leaves ” (rendered "(unknown)") — the deferred-value default.
func TestTaskSubmitModelFlag(t *testing.T) {
	testutil.NewTestDBPath(t)

	if _, err := TaskSubmit(TaskSubmitOptions{
		Summary: "flagged submit",
		Source:  "human",
		TaskID:  "t-model-flag",
		Model:   "deepseek-v4-flash",
	}); err != nil {
		t.Fatalf("TaskSubmit --model: %v", err)
	}
	if _, err := TaskSubmit(TaskSubmitOptions{
		Summary: "bare submit",
		Source:  "human",
		TaskID:  "t-model-empty",
	}); err != nil {
		t.Fatalf("TaskSubmit bare: %v", err)
	}
	out, err := TaskShow("t-model-flag")
	if err != nil {
		t.Fatalf("TaskShow: %v", err)
	}
	if !strings.Contains(out, "model:          deepseek-v4-flash") {
		t.Errorf("submit --model must persist the model:\n%s", out)
	}
	out, err = TaskShow("t-model-empty")
	if err != nil {
		t.Fatalf("TaskShow bare: %v", err)
	}
	if !strings.Contains(out, "model:          (unknown)") {
		t.Errorf("submit without --model must leave the column empty:\n%s", out)
	}
}

// TestTasksRejectForeignDB: the ledger must never create tables inside a
// foreign distilled DB (hermes' production copy has distilled_qa but no
// memory_feedback). Simulate that shape and assert TaskNew refuses.
func TestTasksRejectForeignDB(t *testing.T) {
	path := filepath.Join(t.TempDir(), "foreign.db")
	t.Setenv("GOPI_MEMORY_DB", path)

	// Build a hermes-like DB: distilled_qa without memory_feedback.
	db, err := openStoreDB(t.Context(), path)
	if err != nil {
		t.Fatalf("openStoreDB: %v", err)
	}
	if _, err := db.Exec("CREATE TABLE memory_feedback_placeholder (id INTEGER)"); err != nil {
		t.Fatalf("placeholder: %v", err)
	}
	if _, err := db.Exec("DROP TABLE memory_feedback"); err != nil {
		t.Fatalf("drop feedback: %v", err)
	}
	db.Close() //stylegate:ignore process-lifetime handle closed at shutdown; the OS reclaims the descriptor

	if _, err := TaskNew("must not land", "t-foreign", "", ""); err == nil || !strings.Contains(err.Error(), "foreign distilled DB") {
		t.Fatalf("foreign DB must be rejected, got %v", err)
	}
}

// TestTaskUpdateWithResultBackfillsModel: a submit-time-empty row whose
// child session JSONL exists gets its model backfilled at writeback —
// the human-submits / child-executes loop closes without a submit-time
// model. The backfill must NOT overwrite a submit-time value and must
// stay silent (no error) when the session file is absent.
func TestTaskUpdateWithResultBackfillsModel(t *testing.T) {
	testutil.NewTestDBPath(t)
	sessHome := t.TempDir()
	t.Setenv("HOME", sessHome)
	// Name the gopi data root too: the suite's profile sandbox sets GOPI_HOME
	// (testutil.PinSuiteGopiProfile), which WINS over HOME in config.GopiDir —
	// a fixture that only moved HOME would resolve sessions in the sandbox.
	t.Setenv(config.GopiDirEnv, sessHome)
	sessDir := filepath.Join(sessHome, ".gopi", "sessions")
	if err := os.MkdirAll(sessDir, 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	sessID := "child-sess-1"
	if err := os.WriteFile(filepath.Join(sessDir, sessID+".jsonl"),
		[]byte(`{"type":"session_start","sessionId":"child-sess-1","model":"glm-exec","timestamp":"x"}`+"\n"), 0644); err != nil {
		t.Fatalf("write session: %v", err)
	}
	// The child gopi exports its session id at startup (printLogPaths);
	// the writeback resolves the executor's model through it — NOT via
	// task_id (a human-submitted row's task_id is t-…, not a session id,
	// the exact E2E gap this seam closes).
	t.Setenv(TaskExecutorSessionEnv, sessID)

	// ① Empty-model row + matching session file → backfilled at writeback.
	if _, err := TaskSubmit(TaskSubmitOptions{
		Summary:   "human submit",
		Payload:   "do the thing",
		Source:    "human",
		TaskID:    "t-backfill",
		SessionID: sessID, // the writeback resolves model via task_id here
	}); err != nil {
		t.Fatalf("TaskSubmit: %v", err)
	}
	if _, err := TaskUpdate("t-backfill", TaskStatusInProgress); err != nil {
		t.Fatalf("TaskStart seam: %v", err)
	}
	if _, err := TaskUpdateWithResult("t-backfill", TaskStatusCompleted, "done — run: elapsed=1s"); err != nil {
		t.Fatalf("TaskUpdateWithResult: %v", err)
	}
	out, err := TaskShow("t-backfill")
	if err != nil {
		t.Fatalf("TaskShow: %v", err)
	}
	if !strings.Contains(out, "model:          glm-exec") {
		t.Errorf("writeback must backfill the executor's model from the child session:\n%s", out)
	}
	// Cleanup: the JSONL must NOT leak the backfill into the next leg.
	if err := os.Remove(filepath.Join(sessDir, sessID+".jsonl")); err != nil {
		t.Fatalf("remove session: %v", err)
	}

	// Cleanup: the executor-session env must NOT leak into the next leg —
	// leg ② asserts a submit-time model SURVIVES, which only holds when no
	// executor session resolves (leg ② has no matching session file anyway;
	// dropping the env keeps legs independent). The capture below makes the
	// drop lossless: SnapshotEnv reads with os.LookupEnv, so the variable comes
	// back ABSENT rather than set-to-"" (the hand-rolled os.Getenv pair cannot
	// tell those apart, and a test-scoped t.Setenv restore is the wrong tool
	// here because legs ② and ③ must run with the key absent).
	snapshotExecutorEnv := testutil.SnapshotEnv(t, TaskExecutorSessionEnv)
	defer snapshotExecutorEnv()
	// env-leak-ok: per-call flip, not a test-wide leak — the matching t.Setenv
	// is test-scoped, but legs ②/③ below need the key ABSENT mid-test, and the
	// SnapshotEnv capture above restores absence exactly. No os.Getenv reads it.
	os.Unsetenv(TaskExecutorSessionEnv)
	// ② A submit-time --model value is NEVER overwritten (even when the
	// session file carries a different model).
	if _, err := TaskSubmit(TaskSubmitOptions{
		Summary: "flagged submit",
		Payload: "do the other thing",
		Source:  "human",
		TaskID:  "t-keep",
		Model:   "submit-side-model",
	}); err != nil {
		t.Fatalf("TaskSubmit 2: %v", err)
	}
	if _, err := TaskUpdate("t-keep", TaskStatusInProgress); err != nil {
		t.Fatalf("TaskStart seam 2: %v", err)
	}
	if _, err := TaskUpdateWithResult("t-keep", TaskStatusCompleted, "done"); err != nil {
		t.Fatalf("TaskUpdateWithResult 2: %v", err)
	}
	out, err = TaskShow("t-keep")
	if err != nil {
		t.Fatalf("TaskShow 2: %v", err)
	}
	if !strings.Contains(out, "model:          submit-side-model") {
		t.Errorf("backfill must not overwrite a submit-time model:\n%s", out)
	}

	// ③ No session file at all → writeback still succeeds (best-effort).
	if _, err := TaskSubmit(TaskSubmitOptions{
		Summary: "sessionless submit",
		Payload: "run me anyway",
		Source:  "human",
		TaskID:  "t-nosess",
	}); err != nil {
		t.Fatalf("TaskSubmit 3: %v", err)
	}
	// Leg 3 runs the real writeback path: pending → in_progress → failed
	// (the state machine gates it; the harness stamps in_progress first).
	if _, err := TaskUpdate("t-nosess", TaskStatusInProgress); err != nil {
		t.Fatalf("re-arm t-nosess: %v", err)
	}
	if _, err := TaskUpdateWithResult("t-nosess", TaskStatusFailed, "failed — no session"); err != nil {
		t.Fatalf("TaskUpdateWithResult 3 (no session must not fail the writeback): %v", err)
	}
}

// ---------------------------------------------------------------------------
// bug054 session_id stamp tests: TaskBackfillSession stamps the EXECUTING
// child session's id + model into a task row as soon as the child's session
// JSONL appears — NOT at writeback (which is whole-run later and left
// in_progress rows with session_id empty + model (unknown), exactly when
// tailing logs is most needed). The stamp is guarded on both columns being
// empty: it must never overwrite a submit-time --session-id/--model value,
// an auto row's creation stamp, or a previous stamp. Resolution uses the
// GOPI_HOME sandbox (config.GopiDir), the same knob the CLI seam uses.
// ---------------------------------------------------------------------------

// TestTaskBackfillSessionStampsEmptyRow: a human-submitted row (empty
// session_id + model) gets BOTH stamped from the child session JSONL's
// session_start event — the early stamp, while still in_progress.
func TestTaskBackfillSessionStampsEmptyRow(t *testing.T) {
	testutil.NewTestDBPath(t)
	// GOPI_HOME sandbox: the session JSONL must resolve through the same
	// data-root knob the CLI seam uses (config.GopiDir) — HOME alone would
	// not isolate a real profile.
	home := t.TempDir()
	t.Setenv("GOPI_HOME", home)
	// THE ROW'S OWN RUN DIRECTORY (epic 01a17df9): the early stamp now accepts a
	// session id ONLY when its record sits in sessions/<task-id>/, so the fixture
	// writes the record where a real run writes it. A flat record at the sessions
	// root is the layout the fix removes — it is what let a continuously written
	// chat session be adopted as "the session that ran this row".
	sessDir := filepath.Join(home, ".gopi", "sessions", "t-bug054")
	if err := os.MkdirAll(sessDir, 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	sessID := "child-bug054"
	if err := os.WriteFile(filepath.Join(sessDir, sessID+".jsonl"),
		[]byte(`{"type":"session_start","sessionId":"child-bug054","model":"glm-exec-bug054","timestamp":"x"}`+"\n"), 0644); err != nil {
		t.Fatalf("write session: %v", err)
	}

	if _, err := TaskSubmit(TaskSubmitOptions{
		Summary: "human submit",
		Payload: "do the thing",
		Source:  "human",
		TaskID:  "t-bug054",
	}); err != nil {
		t.Fatalf("TaskSubmit: %v", err)
	}
	// The task-run seam moves pending → in_progress before the child spawns.
	if _, err := TaskUpdate("t-bug054", TaskStatusInProgress); err != nil {
		t.Fatalf("TaskStart seam: %v", err)
	}
	// The early stamp (what runTaskRun's poll calls as soon as the child's
	// session JSONL appears — still in_progress, NOT at writeback).
	if err := TaskBackfillSession("t-bug054", sessID); err != nil {
		t.Fatalf("TaskBackfillSession: %v", err)
	}
	out, err := TaskShow("t-bug054")
	if err != nil {
		t.Fatalf("TaskShow: %v", err)
	}
	if !strings.Contains(out, "session_id:     child-bug054") {
		t.Errorf("early stamp must fill session_id:\n%s", out)
	}
	if !strings.Contains(out, "model:          glm-exec-bug054") {
		t.Errorf("early stamp must fill model from session_start:\n%s", out)
	}
	// The row must STILL be in_progress (the stamp never transitions it).
	if !strings.Contains(out, "status:         in_progress") {
		t.Errorf("stamp must not change status:\n%s", out)
	}
}

// TestTaskBackfillSessionNeverOverwrites: a submit-time --session-id /
// --model pair survives the early stamp untouched (guarded CASE UPDATE).
func TestTaskBackfillSessionNeverOverwrites(t *testing.T) {
	testutil.PinBareTerminalProcess(t)
	testutil.NewTestDBPath(t)
	home := t.TempDir()
	t.Setenv("GOPI_HOME", home)
	// The row's OWN run directory (epic 01a17df9) — see the note on
	// TestTaskBackfillSessionStampsEmptyRow.
	sessDir := filepath.Join(home, ".gopi", "sessions", "t-bug054-keep")
	if err := os.MkdirAll(sessDir, 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(sessDir, "child-keep.jsonl"),
		[]byte(`{"type":"session_start","sessionId":"child-keep","model":"exec-model","timestamp":"x"}`+"\n"), 0644); err != nil {
		t.Fatalf("write session: %v", err)
	}

	if _, err := TaskSubmit(TaskSubmitOptions{
		Summary:   "flagged submit",
		Payload:   "do the thing",
		Source:    "human",
		TaskID:    "t-bug054-keep",
		SessionID: "submitter-sess",
		Model:     "submit-side-model",
	}); err != nil {
		t.Fatalf("TaskSubmit: %v", err)
	}
	if _, err := TaskUpdate("t-bug054-keep", TaskStatusInProgress); err != nil {
		t.Fatalf("TaskStart seam: %v", err)
	}
	if err := TaskBackfillSession("t-bug054-keep", "child-keep"); err != nil {
		t.Fatalf("TaskBackfillSession: %v", err)
	}
	out, err := TaskShow("t-bug054-keep")
	if err != nil {
		t.Fatalf("TaskShow: %v", err)
	}
	// THE FIX, AT ITS ORIGINAL CRASH SITE (2026-10-28). This arm used to assert
	// that the early stamp could NOT write the run's session, because the cell was
	// already occupied by the SUBMITTER — the column was full of a different fact,
	// so the run's session was refused for the row's whole life. With the two
	// facts split, the run's session lands (child-keep) and the filer is still
	// reported as the filer.
	if !strings.Contains(out, "session_id:     child-keep") {
		t.Errorf("the early stamp must land the RUN's session in session_id:\n%s", out)
	}
	// The filer is NOT asserted here: this arm pins a `source=human` row, whose
	// submitted_by renders from the process signature (that is what
	// testutil.PinBareTerminalProcess above fixes), while the SPLIT itself is
	// pinned on an agent row by task_row_identity_test.go's arms.
	if !strings.Contains(out, "model:          submit-side-model") {
		t.Errorf("early stamp must NOT overwrite submit-time model:\n%s", out)
	}
}

// TestTaskBackfillSessionEmptySIDNoop: an empty session id is a silent
// no-op (the poll never fires with ""), and a row with no session file
// still resolves model "" without error — best-effort by contract.
func TestTaskBackfillSessionEmptySIDNoop(t *testing.T) {
	testutil.NewTestDBPath(t)
	if _, err := TaskSubmit(TaskSubmitOptions{
		Summary: "noop submit",
		Payload: "run me",
		Source:  "human",
		TaskID:  "t-bug054-noop",
	}); err != nil {
		t.Fatalf("TaskSubmit: %v", err)
	}
	if err := TaskBackfillSession("t-bug054-noop", ""); err != nil {
		t.Fatalf("empty session id must be a no-op, got: %v", err)
	}
	out, err := TaskShow("t-bug054-noop")
	if err != nil {
		t.Fatalf("TaskShow: %v", err)
	}
	if !strings.Contains(out, "session_id:     ") || strings.Contains(out, "ghost-session") {
		t.Errorf("no-op stamp must leave session_id empty:\n%s", out)
	}
	// A non-empty sid still stamps the session_id even when the model is
	// unresolvable (no session file) — the sid stamp is the primary bug054
	// fix, the model is best-effort; no error either way.
	if err := TaskBackfillSession("t-bug054-noop", "ghost-session"); err != nil {
		t.Fatalf("missing session file must not error, got: %v", err)
	}
	out, err = TaskShow("t-bug054-noop")
	if err != nil {
		t.Fatalf("TaskShow 2: %v", err)
	}
	if !strings.Contains(out, "session_id:     ghost-session") {
		t.Errorf("non-empty sid must be stamped even with no model:\n%s", out)
	}
}

// ---------------------------------------------------------------------------
// task-single-row-exec: the two-row structure (human wrapper + auto mirror)
// is REMOVED — a task-run child no longer mints an auto mirror row; the
// human wrapper row is the single execution row. These tests guard the
// single-row world: no linked_to / executor / dup-of rendering anywhere, and
// plain sessions still render clean.
// ---------------------------------------------------------------------------

// TestTaskLinkageSingleRowNoTwoRowRender: a legacy auto row still carrying
// the wrapper:<id> tag (pre-fix historical data) must render WITHOUT
// linked_to/executor lines and WITHOUT [dup-of] — the human row is the only
// execution row, display never re-creates the two-row reading.
func TestTaskLinkageSingleRowNoTwoRowRender(t *testing.T) {
	testutil.NewTestDBPath(t)

	if _, err := TaskNew("wrapper work", "t-wrapper-1", "", ""); err != nil {
		t.Fatalf("TaskNew wrapper: %v", err)
	}
	// The legacy mirror row: task_id = child session id (bug054 shape),
	// tagged wrapper:t-wrapper-1 (historical auto-register hook output).
	if _, err := TaskNew("[auto] wrapper work", "sess-child-1", "sess-child-1", "m"); err != nil {
		t.Fatalf("TaskNew mirror: %v", err)
	}
	if _, err := taskTagLegacyWrapperForTest(t, "sess-child-1", "t-wrapper-1"); err != nil {
		t.Fatalf("tag: %v", err)
	}
	if err := TaskBackfillSession("t-wrapper-1", "sess-child-1"); err != nil {
		t.Fatalf("TaskBackfillSession: %v", err)
	}

	// AUTO legacy row → no linked_to line (display removed).
	mirror, err := TaskShow("sess-child-1")
	if err != nil {
		t.Fatalf("TaskShow mirror: %v", err)
	}
	if strings.Contains(mirror, "linked_to:") {
		t.Errorf("legacy mirror show must NOT render linked_to (single-row):\n%s", mirror)
	}
	// HUMAN wrapper row → no executor line.
	wrapper, err := TaskShow("t-wrapper-1")
	if err != nil {
		t.Fatalf("TaskShow wrapper: %v", err)
	}
	if strings.Contains(wrapper, "executor:") {
		t.Errorf("wrapper show must NOT render executor (single-row):\n%s", wrapper)
	}
	// List → no [dup-of] marker anywhere.
	list, err := TaskList("")
	if err != nil {
		t.Fatalf("TaskList: %v", err)
	}
	if strings.Contains(list, "dup-of") {
		t.Errorf("list must NOT render [dup-of] (single-row):\n%s", list)
	}
}

// TestTaskLinkageNoFalseLinkage: an auto row with a session_id == its own
// task_id is its OWN run (plain session, no wrapper) — no linkage renders.
func TestTaskLinkageNoFalseLinkage(t *testing.T) {
	testutil.NewTestDBPath(t)

	if _, err := TaskNew("[auto] plain session", "sess-plain", "sess-plain", "m"); err != nil {
		t.Fatalf("TaskNew: %v", err)
	}
	out, err := TaskShow("sess-plain")
	if err != nil {
		t.Fatalf("TaskShow: %v", err)
	}
	if strings.Contains(out, "linked_to:") || strings.Contains(out, "executor:") {
		t.Errorf("plain session must link nothing:\n%s", out)
	}
}

// TestTaskLinkageListNoDupOfMarker (task-single-row-exec): even a legacy
// auto row tagged wrapper:<id> renders WITHOUT [dup-of] — the human row is
// the single execution row; the display never re-creates the two-row reading.
func TestTaskLinkageListNoDupOfMarker(t *testing.T) {
	testutil.NewTestDBPath(t)

	if _, err := TaskNew("wrapper work", "t-wrapper-list", "", ""); err != nil {
		t.Fatalf("TaskNew wrapper: %v", err)
	}
	if _, err := TaskNew("[auto] wrapper work", "sess-child-list", "sess-child-list", "m"); err != nil {
		t.Fatalf("TaskNew mirror: %v", err)
	}
	if _, err := taskTagLegacyWrapperForTest(t, "sess-child-list", "t-wrapper-list"); err != nil {
		t.Fatalf("tag mirror: %v", err)
	}

	list, err := TaskList("")
	if err != nil {
		t.Fatalf("TaskList: %v", err)
	}
	if strings.Contains(list, "dup-of") {
		t.Errorf("list must NOT render [dup-of] (single-row exec):\n%s", list)
	}
}

// taskTagLegacyWrapperForTest replicates the historical wrapper:<id> tag for
// test fixtures only (the exported TaskTagWrapper helper was removed in
// task-single-row-exec; the tag itself still exists on legacy rows and is
// resolved by TaskReconcileMirrorCloseout). Test-local so the production
// surface stays single-row.
func taskTagLegacyWrapperForTest(t *testing.T, mirrorID, wrapperID string) (string, error) {
	t.Helper()
	ctx := context.Background()
	db, err := openTasksDB(ctx, memoryDBPath())
	if err != nil {
		return "", err
	}
	defer db.Close()
	added, err := taskAddTag(ctx, db, mirrorID, taskTagWrapperPrefix+wrapperID)
	if err != nil {
		return "", err
	}
	if added {
		return "tagged", nil
	}
	return "already tagged", nil
}

// ---------------------------------------------------------------------------
// serve-waitpid close-out (epic t-1788384486): TaskReconcileMirrorCloseout —
// the PARENT-side reconciliation of a LEGACY child auto mirror row
// (task-single-row-exec no longer mints mirrors, but pre-fix in_progress
// ones must still be closable). The mirror (tagged
// wrapper:<wrapper-id> — the tag IS its identity, never its provenance) is
// normally closed by the child's own END hook; a
// child that died abnormally never ran it, leaving the mirror in_progress
// forever (the recurring zombie auto-row class). The wrapper closes the
// mirror itself after cmd.Wait, by its own action.
// ---------------------------------------------------------------------------

// seedWrapperAndMirror builds the LEGACY two-row shape: wrapper row + a mirror
// row tagged wrapper:<wrapper-id>, left in_progress (the child died without its
// END hook). The mirror's provenance is the one its REAL writer stamps —
// internal/agent/taskregister.go's tasksource.Server (see seedWrapperAndMirrorWithSource).
func seedWrapperAndMirror(t *testing.T, wrapperID, mirrorID string) {
	t.Helper()
	seedWrapperAndMirrorWithSource(t, wrapperID, mirrorID, tasksource.Server)
}

// seedWrapperAndMirrorWithSource is seedWrapperAndMirror with the mirror's
// provenance STATED, so a test can replay the row each writer produces.
//
// Provenance is not decoration here: the mirror lookup once carried a SECOND
// key beside the tag (`and source = 'auto'`), and be17a9bd retired that
// spelling at the ONLY write path — insertTaskRow derives a row's role from the
// process signature (tasksource.Derive), so a row born today reads
// server/agent/worker/merger/human and NEVER "auto"; the retired spelling can
// only survive on a row written before that commit. A fixture that minted its
// mirror with no stated source therefore measured a row no writer produces
// while the predicate stayed invisible (`TaskNew` with an empty source derives
// the RUNNER's role), which is exactly how the three reds below hid a
// provenance-shaped defect behind a fixture-shaped green.
func seedWrapperAndMirrorWithSource(t *testing.T, wrapperID, mirrorID, provenance string) {
	t.Helper()
	if _, err := TaskSubmit(TaskSubmitOptions{
		Summary: "wrapper work",
		TaskID:  wrapperID,
		Source:  tasksource.Agent,
	}); err != nil {
		t.Fatalf("TaskSubmit wrapper: %v", err)
	}
	if _, err := TaskNewOID(TaskNewOptions{
		Summary:   "[auto] wrapper work",
		TaskID:    mirrorID,
		SessionID: mirrorID,
		Model:     "m",
		Source:    provenance,
	}); err != nil {
		t.Fatalf("TaskNewOID mirror: %v", err)
	}
	if _, err := taskTagLegacyWrapperForTest(t, mirrorID, wrapperID); err != nil {
		t.Fatalf("tag mirror: %v", err)
	}
	// The wrapper row is claimed+started (in_progress) like a live run.
	if _, err := TaskStart(wrapperID, "tester"); err != nil {
		t.Fatalf("TaskStart wrapper: %v", err)
	}
}

// TestReconcileMirrorCloseout_SIGKILLClosesFailed: the acceptance (a) shape —
// a child SIGKILLed mid-run leaves its mirror in_progress; the parent-side
// reconcile closes it FAILED with the signal reason. The wrapper row is
// already terminal (the writeback ran); the mirror is the zombie.
func TestReconcileMirrorCloseout_SIGKILLClosesFailed(t *testing.T) {
	testutil.NewTestDBPath(t)
	seedWrapperAndMirror(t, "t-w-sigkill", "sess-sigkill")

	if err := TaskReconcileMirrorCloseout("t-w-sigkill", TaskStatusFailed,
		"child-exit: signal: killed (parent-side close-out)"); err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	show, err := TaskShow("sess-sigkill")
	if err != nil {
		t.Fatalf("show: %v", err)
	}
	if !strings.Contains(show, "status:") || !strings.Contains(show, "failed") {
		t.Errorf("mirror must be failed after reconcile:\n%s", show)
	}
	if !strings.Contains(show, "signal: killed") {
		t.Errorf("mirror result must carry the signal reason:\n%s", show)
	}
}

// TestReconcileMirrorCloseout_NonZeroExit: acceptance (b) — child exits
// non-zero → mirror FAILED with the exit code in the reason.
func TestReconcileMirrorCloseout_NonZeroExit(t *testing.T) {
	testutil.NewTestDBPath(t)
	seedWrapperAndMirror(t, "t-w-exit3", "sess-exit3")

	if err := TaskReconcileMirrorCloseout("t-w-exit3", TaskStatusFailed,
		"child-exit: exit status 3 (parent-side close-out)"); err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	show, _ := TaskShow("sess-exit3") //stylegate:ignore task read after a ledger mutation the test performed; the compared row is the contract
	if !strings.Contains(show, "failed") || !strings.Contains(show, "exit status 3") {
		t.Errorf("mirror must be failed with the exit code:\n%s", show)
	}
}

// TestReconcileMirrorCloseout_NoDoubleWrite (acceptance d): a mirror the
// child ALREADY closed terminal is left untouched — reconcile is a no-op
// (same-status update allowed, but no second digest event overwrites the
// child's own close). The reconcile returns nil and the row keeps the
// child's completed status + digest.
func TestReconcileMirrorCloseout_NoDoubleWrite(t *testing.T) {
	testutil.NewTestDBPath(t)
	seedWrapperAndMirror(t, "t-w-normal", "sess-normal")

	// The child's own END hook ran: mirror is completed.
	if _, err := TaskUpdateWithResult("sess-normal", TaskStatusCompleted,
		"child's own close: elapsed=1s iterations=2 tokens=10"); err != nil {
		t.Fatalf("child close: %v", err)
	}
	// Parent reconcile fires late (e.g. a verify read) — must NOT clobber.
	if err := TaskReconcileMirrorCloseout("t-w-normal", TaskStatusFailed,
		"child-exit: signal: killed (parent-side close-out)"); err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	show, _ := TaskShow("sess-normal") //stylegate:ignore task read after a ledger mutation the test performed; the compared row is the contract
	if !strings.Contains(show, "completed") {
		t.Errorf("already-terminal mirror must stay completed:\n%s", show)
	}
	if strings.Contains(show, "signal: killed") {
		t.Errorf("reconcile must not overwrite the child's own close:\n%s", show)
	}
}

// TestReconcileMirrorCloseout_NoMirrorNoop: no mirror row exists (the child
// never registered — died before the START hook) → reconcile is a silent
// no-op, nil.
func TestReconcileMirrorCloseout_NoMirrorNoop(t *testing.T) {
	testutil.NewTestDBPath(t)
	if _, err := TaskSubmit(TaskSubmitOptions{
		Summary: "wrapper work",
		TaskID:  "t-w-nomirror",
		Source:  "human",
	}); err != nil {
		t.Fatalf("TaskSubmit: %v", err)
	}
	if _, err := TaskStart("t-w-nomirror", "tester"); err != nil {
		t.Fatalf("TaskStart: %v", err)
	}
	if err := TaskReconcileMirrorCloseout("t-w-nomirror", TaskStatusFailed,
		"child-exit: signal: killed"); err != nil {
		t.Fatalf("no-mirror reconcile must be a no-op, got %v", err)
	}
}

// TestReconcileMirrorCloseout_Exit0ClosesCompleted is acceptance (c): the
// child exits 0 but its OWN writeback never ran (died between loop-end and
// the END hook) → the parent reconcile closes the still-in_progress mirror
// COMPLETED, by its own action.
func TestReconcileMirrorCloseout_Exit0ClosesCompleted(t *testing.T) {
	testutil.NewTestDBPath(t)
	seedWrapperAndMirror(t, "t-w-exit0", "sess-exit0")

	if err := TaskReconcileMirrorCloseout("t-w-exit0", TaskStatusCompleted,
		"child-exit: 0 (parent-side close-out)"); err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	show, _ := TaskShow("sess-exit0") //stylegate:ignore task read after a ledger mutation the test performed; the compared row is the contract
	if !strings.Contains(show, "completed") {
		t.Errorf("exit-0 mirror must be completed after reconcile:\n%s", show)
	}
	if !strings.Contains(show, "parent-side close-out") {
		t.Errorf("completed mirror must carry the parent close-out reason:\n%s", show)
	}
}

// TestReconcileMirrorCloseout_IdentityIsTheTagNotTheProvenance is the
// regression guard for the three sibling tests above.
//
// THE DEFECT IT PINS (measured 2026-09-25, fixed by 4f2255ac — which left no
// discriminating guard): TaskReconcileMirrorCloseout's mirror lookup carried a
// SECOND key beside the tag — `and source = 'auto'` — for a fact the
// `wrapper:<wrapperTaskID>` tag ALREADY carries. be17a9bd then retired the
// "auto" spelling at the write side and made insertTaskRow DERIVE a row's
// provenance from the process signature (tasksource.Derive), so no writer
// stamps "auto" any more — and the arm silently matched NOTHING. Every mirror
// row stayed in_progress after a reconcile that returned nil, i.e. the
// close-out reported success while closing no row.
//
// WHY THIS TEST AND NOT ANOTHER ASSERTION ON THE THREE ABOVE: those three
// seed whatever mirror their fixture happens to write, so they can be made
// green by a FIXTURE edit instead of by fixing the mechanism — and that is
// exactly how this defect hid: their fixture left the source EMPTY (so
// insertTaskRow derived the RUNNER's role), the predicate `source = 'auto'`
// matched nothing, and the three read green on a machine where the runner's
// role is not the value they assumed. This test drives the REAL writer's value
// (tasksource.Server, as internal/agent/taskregister.go stamps a mirror row)
// and ASSERTS that premise before it asserts the close-out, so the same
// fixture-weakening move fails the test loudly (`fixture premise:` …) instead
// of passing silently. It then states the ONE thing the mechanism promises: an
// in_progress row tagged wrapper:<id> is closed by the reconcile, whatever its
// provenance — a second key beside the tag is a defect, not a filter.
//
// The provenance is deliberately NOT 'auto' (the retired spelling) and NOT
// empty: it is the value a mirror row is born with today.
func TestReconcileMirrorCloseout_IdentityIsTheTagNotTheProvenance(t *testing.T) {
	testutil.NewTestDBPath(t)
	const wrapperID, mirrorID = "t-w-identity", "sess-identity"
	seedWrapperAndMirrorWithSource(t, wrapperID, mirrorID, tasksource.Server)

	// The fixture's premise, asserted rather than assumed: the row carries the
	// provenance a real writer stamps. A row born 'auto' would make this test
	// vacuous (the retired spelling is what the predicate was written for).
	if src := mirrorRowSource(t, mirrorID); src != tasksource.Server {
		t.Fatalf("fixture premise: mirror source = %q, want %q (a row no writer mints today would not discriminate)",
			src, tasksource.Server)
	}

	if err := TaskReconcileMirrorCloseout(wrapperID, TaskStatusFailed,
		"child-exit: signal: killed (parent-side close-out)"); err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	after := taskRowForTest(t, mirrorID)
	if after.Status != TaskStatusFailed {
		t.Errorf("mirror must be closed by its wrapper TAG alone (source=%q is not part of the mirror's identity): status = %q, want %q",
			after.Source, after.Status, TaskStatusFailed)
	}
	show, err := TaskShow(mirrorID)
	if err != nil {
		t.Fatalf("TaskShow mirror: %v", err)
	}
	if !strings.Contains(show, "parent-side close-out") {
		t.Errorf("the close-out digest must be rendered on the row:\n%s", show)
	}
}

// mirrorRowSource / taskRowForTest read a row back through the package's own
// accessor (no second query spelled in the test, Rule 14).
func mirrorRowSource(t *testing.T, taskID string) string {
	t.Helper()
	return taskRowForTest(t, taskID).Source
}

func taskRowForTest(t *testing.T, taskID string) *Task {
	t.Helper()
	ctx := context.Background()
	db, err := openTasksDB(ctx, memoryDBPath())
	if err != nil {
		t.Fatalf("open ledger: %v", err)
	}
	defer db.Close()
	tk, err := taskByID(ctx, db, taskID)
	if err != nil || tk == nil {
		t.Fatalf("taskByID(%s): %v (row %v)", taskID, err, tk)
	}
	return tk
}

// TestReconcileMirrorCloseout_EmptyArgs: guard rails — empty wrapper id or
// non-terminal status is an error, never a silent no-op.
func TestReconcileMirrorCloseout_EmptyArgs(t *testing.T) {
	testutil.NewTestDBPath(t)
	if err := TaskReconcileMirrorCloseout("", TaskStatusFailed, "x"); err == nil {
		t.Fatal("empty wrapper id must error")
	}
	if err := TaskReconcileMirrorCloseout("t-x", "in_progress", "x"); err == nil {
		t.Fatal("non-terminal status must error")
	}
}

// ---------------------------------------------------------------------------
// task-update-reason epic: --reason stamps the operator-visible WHY of a
// status move; a lazy caller (no --reason) still gets a status-specific
// default; same-status no-op updates never clobber an existing reason.
// ---------------------------------------------------------------------------

func TestTaskUpdateReasonExplicit(t *testing.T) {
	testutil.NewTestDBPath(t)

	if _, err := TaskNew("ship the reason feature", "t-reason", "", ""); err != nil {
		t.Fatalf("TaskNew: %v", err)
	}
	out, err := TaskUpdateReason("t-reason", TaskStatusCompleted, "delivery verified by green e2e", "")
	if err != nil {
		t.Fatalf("TaskUpdateReason: %v", err)
	}
	if !strings.Contains(out, "status=completed") || !strings.Contains(out, "reason=delivery verified by green e2e") {
		t.Errorf("update out = %q", out)
	}
	show, err := TaskShow("t-reason")
	if err != nil {
		t.Fatalf("TaskShow: %v", err)
	}
	if !strings.Contains(show, "reason:         delivery verified by green e2e") {
		t.Errorf("show must render the explicit reason:\n%s", show)
	}
}

func TestTaskUpdateReasonDefaultWhenLazy(t *testing.T) {
	testutil.NewTestDBPath(t)

	if _, err := TaskNew("lazy operator update", "t-lazy", "", ""); err != nil {
		t.Fatalf("TaskNew: %v", err)
	}
	// Plain TaskUpdate (no reason) on a REAL transition must still stamp the
	// status-specific default — the "why" is never left blank.
	if _, err := TaskUpdate("t-lazy", TaskStatusCompleted); err != nil {
		t.Fatalf("TaskUpdate: %v", err)
	}
	show, err := TaskShow("t-lazy")
	if err != nil {
		t.Fatalf("TaskShow: %v", err)
	}
	if !strings.Contains(show, "reason:         task finished") {
		t.Errorf("default reason must be stamped on a bare transition:\n%s", show)
	}
}

func TestTaskUpdateReasonDefaultVerified(t *testing.T) {
	testutil.NewTestDBPath(t)

	if _, err := TaskNew("verify loop row", "t-verify", "", ""); err != nil {
		t.Fatalf("TaskNew: %v", err)
	}
	if _, err := TaskUpdate("t-verify", TaskStatusCompleted); err != nil {
		t.Fatalf("to completed: %v", err)
	}
	if _, err := TaskUpdate("t-verify", TaskStatusVerified); err != nil {
		t.Fatalf("to verified: %v", err)
	}
	show, err := TaskShow("t-verify")
	if err != nil {
		t.Fatalf("TaskShow: %v", err)
	}
	if !strings.Contains(show, "reason:         verification passed") {
		t.Errorf("verified default reason missing:\n%s", show)
	}
}

func TestTaskUpdateReasonSameStatusNoClobber(t *testing.T) {
	testutil.NewTestDBPath(t)

	if _, err := TaskNew("no-clobber row", "t-noclob", "", ""); err != nil {
		t.Fatalf("TaskNew: %v", err)
	}
	if _, err := TaskUpdateReason("t-noclob", TaskStatusCompleted, "root cause fixed with repro test", ""); err != nil {
		t.Fatalf("first update: %v", err)
	}
	// Same-status no-op re-landing (idempotent writeback) WITHOUT a reason
	// must NOT replace the custom reason with the default.
	if _, err := TaskUpdate("t-noclob", TaskStatusCompleted); err != nil {
		t.Fatalf("same-status no-op: %v", err)
	}
	show, err := TaskShow("t-noclob")
	if err != nil {
		t.Fatalf("TaskShow: %v", err)
	}
	if !strings.Contains(show, "reason:         root cause fixed with repro test") {
		t.Errorf("same-status no-op must keep the custom reason:\n%s", show)
	}
	// An explicit reason on a same-status no-op IS written (the operator is
	// clarifying why the row still sits in this state).
	if _, err := TaskUpdateReason("t-noclob", TaskStatusCompleted, "re-verified after infra flake", ""); err != nil {
		t.Fatalf("explicit same-status reason: %v", err)
	}
	show, err = TaskShow("t-noclob")
	if err != nil {
		t.Fatalf("TaskShow: %v", err)
	}
	if !strings.Contains(show, "reason:         re-verified after infra flake") {
		t.Errorf("explicit same-status reason must be written:\n%s", show)
	}
}

func TestTaskUpdateReasonCarriesResultDigest(t *testing.T) {
	testutil.NewTestDBPath(t)

	if _, err := TaskNew("reason + digest", "t-both", "", ""); err != nil {
		t.Fatalf("TaskNew: %v", err)
	}
	// The CLI passes --reason AND --result together; both carriers land —
	// reason on the row, digest in task_events (summary immutable).
	if _, err := TaskUpdateReason("t-both", TaskStatusFailed, "integration test flakes under -race", "reason: repro: go test -race ./... fails 3/10"); err != nil {
		t.Fatalf("TaskUpdateReason: %v", err)
	}
	show, err := TaskShow("t-both")
	if err != nil {
		t.Fatalf("TaskShow: %v", err)
	}
	if !strings.Contains(show, "reason:         integration test flakes under -race") {
		t.Errorf("reason line missing:\n%s", show)
	}
	if !strings.Contains(show, "result:         reason: repro: go test -race ./... fails 3/10") {
		t.Errorf("result digest line missing:\n%s", show)
	}
	// The immutable summary never carried the digest.
	if !strings.Contains(show, "summary:        reason + digest") {
		t.Errorf("summary must stay immutable:\n%s", show)
	}
}

func TestTaskUpdateReasonRefusesEmptyStatus(t *testing.T) {
	testutil.NewTestDBPath(t)
	if _, err := TaskNew("empty status row", "t-empty", "", ""); err != nil {
		t.Fatalf("TaskNew: %v", err)
	}
	if _, err := TaskUpdateReason("t-empty", "", "why", ""); err == nil ||
		!strings.Contains(err.Error(), "nothing to update") {
		t.Fatalf("empty status must error, got %v", err)
	}
}

// TestTaskShowResultKeepsTheRunDigestAfterTheBranchCheckStep is the regression for the
// close-out-overwrites-the-digest defect (measured 2026-09-25 on the real
// binary; it made two cmd/agent E2E names red).
//
// `task show` renders the newest task_events detail as `result:`. The close-out
// appends MORE events after the run's writeback — the branch-check lane is the
// one every completed run gets — whose detail is the delivery sync's one-line
// verdict, NOT a run digest. Rendering it dropped the digest the writeback had
// just recorded: the `task-report: <path>` line (the report half) and Rule 31's
// `ran-line-missing:` finding (the ran-check half), which the E2E names assert
// are RECORDED. The absence they read was a render, not a ledger gap.
//
// The guard pins BOTH halves: the digest must survive a later mechanical step,
// and the row's own truncation/verify readers (which read the RAW newest event)
// must keep working — a fix that broke those would be the same class of defect
// from the other side.
func TestTaskShowResultKeepsTheRunDigestAfterTheBranchCheckStep(t *testing.T) {
	testutil.NewTestDBPath(t)

	const taskID = "t-digest-vs-align"
	if _, err := TaskSubmit(TaskSubmitOptions{
		Summary: "digest survives the align step",
		Payload: "briefing body",
		TaskID:  taskID,
		Source:  "auto",
	}); err != nil {
		t.Fatalf("TaskSubmit: %v", err)
	}
	if _, err := TaskUpdate(taskID, TaskStatusInProgress); err != nil {
		t.Fatalf("in_progress: %v", err)
	}
	digest := "gopi run completed — run: elapsed=12.5s — task-report: /tmp/x/task-report.md (1063 bytes)"
	if _, err := TaskRunWriteback(taskID, TaskStatusCompleted, digest); err != nil {
		t.Fatalf("writeback: %v", err)
	}
	// The close-out's mechanical step: appended AFTER the writeback, under its
	// own event name (never a status), with verdict text as its detail.
	taskEventWrite(context.Background(), sweepTestDB(t), taskID,
		finishCleanupPreMergeVerifiedEvent, finishCleanupBranchCheckPassMarker)

	out, err := TaskShow(taskID)
	if err != nil {
		t.Fatalf("TaskShow: %v", err)
	}
	if !strings.Contains(out, "result:         "+digest) {
		t.Errorf("the run digest must survive the branch-check verdict as the result line:\n%s", out)
	}
	if strings.Contains(out, "result:         "+finishCleanupBranchCheckPassMarker) {
		t.Errorf("the align verdict must not replace the run's digest:\n%s", out)
	}
	// The RAW newest event keeps its own readers working (truncation token,
	// merge-verify verdict, notify dedup) — those are not a render.
	if got := MergeVerifyLatestEvent(taskID); got != finishCleanupBranchCheckPassMarker {
		t.Errorf("the raw newest-event reader must still see the branch-check step, got %q", got)
	}
}

// sweepTestDB opens the pinned test ledger for a direct taskEventWrite — the
// "some other writer appended an event" half of the regression above, and the
// same access path the close-out uses (never a second spelling of the DSN).
func sweepTestDB(t *testing.T) *sql.DB {
	t.Helper()
	ctx := context.Background()
	db, err := openTasksDB(ctx, memoryDBPath())
	if err != nil {
		t.Fatalf("open ledger: %v", err)
	}
	//stylegate:ignore process-lifetime handle closed at shutdown; the OS reclaims the descriptor
	t.Cleanup(func() { db.Close() })
	return db
}

// TestTaskShowRendersBothDigestAndVerdict is the MIRROR of
// TestTaskShowResultKeepsTheRunDigestAfterTheBranchCheckStep, and the regression for
// the opposite direction of the same defect.
//
// The trail's two faces must coexist on `task show`: a run DIGEST (written
// under the row's status — the align step after it must not displace it) and a
// JUDGED VERDICT about the row (written under its own event name — the digest
// must not hide it). Reading only the newest status-named event fixed the first
// and broke the second: the merge-verify gate's FAIL on a row became invisible
// to the operator's audit path, measured on TestVerifyGateRecordsVerdictEvent.
// This test asserts ONE show output carries BOTH lines, which is the only
// assertion that fails whichever face the reader drops.
func TestTaskShowRendersBothDigestAndVerdict(t *testing.T) {
	testutil.NewTestDBPath(t)

	const taskID = "t-digest-and-verdict"
	if _, err := TaskSubmit(TaskSubmitOptions{
		Summary: "the row shows both faces",
		Payload: "briefing body",
		TaskID:  taskID,
		Source:  "auto",
	}); err != nil {
		t.Fatalf("TaskSubmit: %v", err)
	}
	if _, err := TaskUpdate(taskID, TaskStatusInProgress); err != nil {
		t.Fatalf("in_progress: %v", err)
	}
	digest := "gopi run completed — run: elapsed=9.1s — task-report: /tmp/r.md (2048 bytes)"
	if _, err := TaskRunWriteback(taskID, TaskStatusCompleted, digest); err != nil {
		t.Fatalf("writeback: %v", err)
	}
	// Both follow the writeback: the close-out's mechanical step, then the
	// gate's verdict on the row it judged.
	taskEventWrite(context.Background(), sweepTestDB(t), taskID,
		finishCleanupPreMergeVerifiedEvent, finishCleanupBranchCheckPassMarker)
	verdict := MergeVerifyReasonFail + ": repo=/srv/r branch=fix/x"
	MergeVerifyRecordEvent(taskID, verdict)

	out, err := TaskShow(taskID)
	if err != nil {
		t.Fatalf("TaskShow: %v", err)
	}
	if !strings.Contains(out, "result:         "+digest) {
		t.Errorf("the run digest must keep rendering after both later events:\n%s", out)
	}
	if strings.Contains(out, "result:         "+finishCleanupBranchCheckPassMarker) {
		t.Errorf("the align verdict must not take the result line:\n%s", out)
	}
	wantVerdict := TaskShowVerdictLabel + strings.Repeat(" ", taskShowLabelWidth-len(TaskShowVerdictLabel)) + verdict
	if !strings.Contains(out, wantVerdict) {
		t.Errorf("the verdict must render on its own %s line:\n%s", TaskShowVerdictLabel, out)
	}
	wantName := TaskShowVerdictNameLabel + strings.Repeat(" ", taskShowLabelWidth-len(TaskShowVerdictNameLabel)) + MergeVerifyEvent
	if !strings.Contains(out, wantName) {
		t.Errorf("the verdict line must name its event so a query and the render agree:\n%s", out)
	}
}

// TestShowLongestRunIgnoresTheEllipsis is the regression arm for the counting
// mistake this assertion cost once: the truncation ellipsis spells the letter
// 'x' inside the words it carries ("full text:"), so a character COUNT over the
// whole record reported 808 for a briefing capped at 800 and would have failed
// on a renderer that was correct. The run boundaries are what the cap is about.
func TestShowLongestRunIgnoresTheEllipsis(t *testing.T) {
	cases := []struct {
		in   string
		want int
	}{
		{"", 0},
		{"abc", 0},
		{"xxx…[truncated — full text: session JSONL]", 3},
		{"xx xx xxx", 3},
		{"…text:", 1}, // the 'x' in "text" is a run of ONE, never 800
	}
	for _, c := range cases {
		if got := testShowLongestRun(c.in, 'x'); got != c.want {
			t.Errorf("testShowLongestRun(%q) = %d, want %d", c.in, got, c.want)
		}
	}
}
