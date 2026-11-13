package main

// worker_status_task_test.go — the TASK FIELD on every worker line (epic
// worker-status-task-field, 2026-10-23).
//
// The measured defect: three worker shapes, one of which named a task, and it
// named it only while a round was in flight. The FOREGROUND shape — the common
// one on this host — printed a raw command line and nothing structured, so the
// operator had to re-parse ps output to answer "which task is pid N running?".
//
// These tests pin the three shapes (state record / argv fallback / foreground)
// plus the EMPTY case, through the REAL derivation (workerStatusWorker.Task and
// workerTaskOfPID over a synthetic /proc) so a refactor cannot quietly drop the
// field from one renderer.

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/nick/gopi/internal/procname"
)

// wstProc fakes ONE /proc entry (the shared procFixture, with the gopi program
// name) — the pid stays REAL, because tools.PidAlive is not test-seamable and a
// fabricated pid could never pass liveness.
func wstProc(t *testing.T, root string, pid int, argv []string, session int) {
	t.Helper()
	procFixture(t, root, pid, binaryNameGopi, argv, session)
}

// --- ① the STATE-RECORD shape ----------------------------------------------

func TestWorkerTaskStateRecordShapeCarriesTheField(t *testing.T) {
	dir := t.TempDir()
	stateDir := filepath.Join(dir, workerStateDirName)
	pid := gopiNamedChild(t)
	stateFixture(t, stateDir, workerStateRecord{
		PID: pid, Started: workerStatusNow(), Version: "test",
		State:          workerStatusStateBusy,
		LastRoundStart: workerStatusNow(), TaskID: "01a0state-row", SessionID: "sess-9",
		Updated: workerStatusNow(),
	})
	scan, err := workerStatusEnumerate(stateDir)
	if err != nil {
		t.Fatalf("enumerate: %v", err)
	}
	var found *workerStatusWorker
	for i := range scan.Workers {
		if scan.Workers[i].PID == pid && !scan.Workers[i].Fallback {
			found = &scan.Workers[i]
		}
	}
	if found == nil {
		t.Fatalf("the live worker's record was not enumerated: %+v", scan.Workers)
	}
	if got := found.Task(); got.ID != "01a0state-row" || got.Source != workerTaskSourceState {
		t.Fatalf("state-record task = %+v, want the record's row from source %q", got, workerTaskSourceState)
	}
	line := found.Report()
	if !strings.Contains(line, workerStatusTaskFieldPrefix+"01a0state-row") {
		t.Fatalf("the state-record line does not carry the field: %q", line)
	}
	// The id must appear ONCE: the prose half repeats the session, never the task.
	if n := strings.Count(line, "01a0state-row"); n != 1 {
		t.Fatalf("the task id appears %d times on one line — the field and the prose disagree about which is the field: %q", n, line)
	}
}

// A daemon BETWEEN rounds has no in-flight row, and the writer clears TaskID at
// the round end — so the field must say so instead of repeating the last row.
func TestWorkerTaskBetweenRoundsSaysNotRecorded(t *testing.T) {
	rec := workerStateRecord{
		PID: 4242, LastRoundStart: workerStatusStamp(time.Now()),
		LastRoundEnd: workerStatusNow(), State: workerStatusStateIdle,
	}
	// TaskID deliberately empty: the writer's own state after SetRoundEnd.
	w := workerStatusWorker{PID: 0, Record: rec} // PID 0 → no /proc read to fall back on
	got := w.Task()
	if got.ID != "" || got.Recorded() {
		t.Fatalf("a between-rounds record claimed a task: %+v", got)
	}
	if got.Field() != workerStatusTaskFieldPrefix+workerStatusTaskNotRecorded {
		t.Fatalf("the empty field renders %q, want the declared not-recorded marker", got.Field())
	}
	if !strings.Contains(w.Report(), workerStatusTaskNotRecorded) {
		t.Fatalf("the rendered line hides the unknown instead of saying it: %q", w.Report())
	}
}

// --- ② the ARGV-FALLBACK shape ---------------------------------------------

func TestWorkerTaskArgvFallbackShapeDerivesFromItsOwnArgv(t *testing.T) {
	root := t.TempDir()
	self := os.Getpid()
	// A DETACHED worker (pid == session id) with NO state file, started for one
	// named row: the row exists only in its argv.
	wstProc(t, root, self, []string{"gopi", procname.VerbWorker, "01a0argv-row"}, self)
	t.Setenv(procname.EnvProcRoot, root)

	got := workerTaskOfPID(self)
	if got.ID != "01a0argv-row" || got.Source != workerTaskSourceArgv {
		t.Fatalf("argv task = %+v, want the row from the process's own argv", got)
	}
	w := workerStatusWorker{PID: self, Record: workerStateRecord{PID: self, Weaker: true}, Fallback: true}
	line := w.Report()
	if !strings.Contains(line, workerStatusTaskFieldPrefix+"01a0argv-row") {
		t.Fatalf("the argv-fallback line does not carry the field: %q", line)
	}
}

// The predicate that matters most: a FLAG OPERAND is not a task id. The grammar
// scanner consumes `--repo`'s operand, so a raw substring search would have
// reported the repo path as the running task.
func TestWorkerTaskIgnoresFlagOperands(t *testing.T) {
	argv := []string{"gopi", procname.VerbWorker,
		flagReportTo, "zbook", flagRepo, "/home/nick/work/gopi", workerFlagDB, "/tmp/x.db"}
	if got := workerTaskFromArgv(argv); got.Recorded() {
		t.Fatalf("an argv with no positional reported a task %q — a flag operand was harvested as a row", got.ID)
	}
	if got := workerTaskFromArgv([]string{"gopi", procname.VerbWorker, workerFlagOnce}); got.Recorded() {
		t.Fatalf("a --once loop worker reported a task %q", got.ID)
	}
}

// A bare loop worker — the shape on this host — has no row at all and must say
// so rather than render an empty field.
func TestWorkerTaskLoopWorkerSaysNotRecorded(t *testing.T) {
	for _, argv := range [][]string{
		{"gopi", procname.VerbWorker},
		{"./bin/gopi", procname.VerbWorker},
	} {
		got := workerTaskFromArgv(argv)
		if got.Recorded() {
			t.Fatalf("argv %q reported a task %q", argv, got.ID)
		}
		if got.Field() != workerStatusTaskFieldPrefix+workerStatusTaskNotRecorded {
			t.Fatalf("argv %q renders %q, want the not-recorded marker", argv, got.Field())
		}
	}
}

// An argv this binary's grammar REFUSES is not guessed at: the process is real
// but its shape is unknown, so the honest answer is the marker.
func TestWorkerTaskRefusesToGuessAtAnUnparsableArgv(t *testing.T) {
	got := workerTaskFromArgv([]string{"gopi", procname.VerbWorker, "--not-a-flag-i-know"})
	if got.Recorded() {
		t.Fatalf("an unknown flag produced a task %q — the derivation guessed", got.ID)
	}
	// A word that is not the worker verb is not a worker at all.
	if got := workerTaskFromArgv([]string{"gopi", "task", "run", "01a0x"}); got.Recorded() {
		t.Fatalf("a non-worker argv produced a task %q", got.ID)
	}
}

// --- ③ the FOREGROUND shape (the common one on this host) -------------------

func TestWorkerTaskForegroundShapeCarriesTheField(t *testing.T) {
	root := t.TempDir()
	self := os.Getpid()
	const row = "01a0fg-row"
	// FOREGROUND: NOT a session leader (session is the shell's).
	wstProc(t, root, self, []string{"gopi", procname.VerbWorker, row}, 1000)
	t.Setenv(procname.EnvProcRoot, root)

	line := workerForegroundStatusLine(self)
	if !strings.Contains(line, workerStatusTaskFieldPrefix+row) {
		t.Fatalf("the foreground line does not carry the task field: %q", line)
	}
	if !strings.Contains(line, workerStatusTaskNotRecorded) && !strings.Contains(line, row) {
		t.Fatalf("the foreground line names no task at all: %q", line)
	}
}

// The empty/unknown case for the foreground shape: a bare loop worker.
func TestWorkerTaskForegroundLoopWorkerSaysNotRecorded(t *testing.T) {
	root := t.TempDir()
	self := os.Getpid()
	wstProc(t, root, self, []string{"gopi", procname.VerbWorker}, 1000)
	t.Setenv(procname.EnvProcRoot, root)

	line := workerForegroundStatusLine(self)
	if !strings.Contains(line, workerStatusTaskFieldPrefix+workerStatusTaskNotRecorded) {
		t.Fatalf("a bare loop worker must render the not-recorded marker, got %q", line)
	}
}

// An unreadable /proc entry still lists the pid (the worker is real) and says
// the task is unknown — never blank.
func TestWorkerTaskUnreadableProcSaysUnknown(t *testing.T) {
	t.Setenv(procname.EnvProcRoot, t.TempDir()) // no entry for this pid
	got := workerTaskOfPID(os.Getpid())
	if got.Recorded() || got.sourceWord() != workerTaskSourceNone {
		t.Fatalf("an unreadable /proc entry produced %+v, want source %q and no id", got, workerTaskSourceNone)
	}
	if got.Field() != workerStatusTaskFieldPrefix+workerStatusTaskNotRecorded {
		t.Fatalf("unreadable /proc rendered %q", got.Field())
	}
}

// --- ④ the MACHINE form ----------------------------------------------------

// The JSON form must be a pure re-rendering of the SAME scan: every worker the
// text form lists appears once, with the same task value and source.
func TestWorkerStatusJSONCarriesTheTaskField(t *testing.T) {
	root := t.TempDir()
	// The pid must be a LIVE process that is NOT this test process:
	// foregroundWorkerPIDs excludes the caller by pid (a `--status` invocation
	// carries the same argv shape as a worker).
	pid := gopiNamedChild(t)
	const row = "01a0json-row"
	wstProc(t, root, pid, []string{"gopi", procname.VerbWorker, row}, 1000)
	t.Setenv(procname.EnvProcRoot, root)

	dir := filepath.Join(t.TempDir(), workerStateDirName)
	scan, err := workerStatusEnumerate(dir)
	if err != nil {
		t.Fatalf("enumerate: %v", err)
	}
	doc := workerStatusJSON(scan, dir, false)
	if len(doc.Workers) != 1 {
		t.Fatalf("the machine form listed %d workers, want 1: %+v", len(doc.Workers), doc.Workers)
	}
	w := doc.Workers[0]
	if w.PID != pid || w.Shape != workerStatusJSONShapeForeground {
		t.Fatalf("machine row = %+v", w)
	}
	if w.Task != row || w.Source != workerTaskSourceArgv || !w.TaskRecorded {
		t.Fatalf("the machine form's task fields are wrong: %+v", w)
	}
	// The document must survive a round trip (it is a wire shape).
	body, err := json.Marshal(doc)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var back workerStatusJSONDoc
	if err := json.Unmarshal(body, &back); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(back.Workers) != 1 || back.Workers[0].Task != row {
		t.Fatalf("round trip lost the task: %+v", back.Workers)
	}
}

// Rule 15a: the machine form's task keys are ALWAYS present — a consumer must
// be able to tell "no task" from "this version has no such field".
func TestWorkerStatusJSONUnknownTaskKeepsTheKey(t *testing.T) {
	doc := workerStatusJSON(workerStatusScan{Dir: "/nonexistent"}, "/nonexistent", false)
	body, err := json.Marshal(doc)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	for _, key := range []string{`"workers"`, `"foreign"`, `"residue_count"`} {
		if !strings.Contains(string(body), key) {
			t.Fatalf("the empty document omits %s — an absent key reads as a different lie than an empty list: %s", key, body)
		}
	}
	// `--json` beside an act is still a READ+ACT conflict (the alias must not
	// leave the refusal blind — the F1 defect).
	if got := workerControlRoleOf(workerStatusJSONFlag); got != controlActionReadOnly {
		t.Fatalf("%s classified %v, want read-only", workerStatusJSONFlag, got)
	}
	opts, _ := workerControlParse([]string{workerControlStatus, workerStatusJSONFlag})
	if controlConflictMessage(workerControlConflictParse(nil, opts.actions, opts.read, opts.act, opts.flags)) != "" {
		t.Fatalf("%s %s must not be refused — it is one read", workerControlStatus, workerStatusJSONFlag)
	}
	opts, _ = workerControlParse([]string{workerControlStatus, workerStatusJSONFlag, workerControlStop})
	if controlConflictMessage(workerControlConflictParse(nil, opts.actions, opts.read, opts.act, opts.flags)) == "" {
		t.Fatalf("%s %s %s must be refused as read+act", workerControlStatus, workerStatusJSONFlag, workerControlStop)
	}
	// A BARE --json is NOT a status read (it is an unknown word for the loop
	// scanner): routing it alone would turn a misspelled launch into a report.
	if workerStatusRequested([]string{workerStatusJSONFlag}) {
		t.Fatalf("a bare %s opened the status route — a misspelled worker launch would become a status report", workerStatusJSONFlag)
	}
	if !workerStatusRequested([]string{workerControlStatus, workerStatusJSONFlag}) {
		t.Fatalf("%s %s does not open the status route", workerControlStatus, workerStatusJSONFlag)
	}
	if !workerStatusJSONRequested([]string{workerControlStatus, workerStatusJSONFlag}) {
		t.Fatalf("the JSON modifier was not read off the routing argv")
	}
}

// The stable query token is what the operator greps. One definition, both
// renderers: if a future line drops it, this fails.
func TestWorkerStatusTaskFieldTokenIsStable(t *testing.T) {
	if workerStatusTaskFieldPrefix != "task=" {
		t.Fatalf("the query token changed to %q — every consumer's grep breaks silently", workerStatusTaskFieldPrefix)
	}
	var b strings.Builder
	b.WriteString(workerTask{ID: "01a0x"}.Field())
	if got := b.String(); got != "task=01a0x" {
		t.Fatalf("field renders %q", got)
	}
	if got := (workerTask{}.Field()); got != "task="+workerStatusTaskNotRecorded {
		t.Fatalf("the zero task renders %q, want the honest marker", got)
	}
}
