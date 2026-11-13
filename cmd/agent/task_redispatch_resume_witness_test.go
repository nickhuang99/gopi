package main

// task_redispatch_resume_witness_test.go — THE RE-DISPATCH DOOR'S RESUME WITNESS
// (epic: land the resume witness on the RE-DISPATCH door).
//
// THE DEFECT. `gopi worker <task-id>` on a row that has already run re-enters
// the row: the executor child appends a SECOND `session_start` to the SAME
// record file, and that header used to say nothing about why the file holds
// more than one — the record of a re-dispatched row was indistinguishable from a
// corrupt one. The sibling witness already worked on the RESUME faces
// (cmd/agent/cli_resume.go's resumeIdentityOnRecord threads
// agent.RunIdentity.ResumedFrom); the re-dispatch door passed RunIdentity{}.
//
// WHY THIS WALK IS THE HONEST SAMPLE. A unit arm over NewSessionLogger would
// prove the writer writes what it is given — it would say nothing about whether
// the DOOR declares the fact on the path a re-dispatch actually takes. So this
// walk runs the REAL surface twice: `gopi worker <id>` spawns a real executor
// child through the ONE spawn seam (cli_tasks.go's runTaskRunOnce), the child
// writes its own session_start, and the assertion is made on the record file the
// two runs left behind.
//
// THE SECOND RUN'S LEDGER STATUS IS FIXTURE, NOT PRODUCT. A completed row is not
// claimable, so the test puts the row back to `pending` the way any requeue does
// — the fact under test is that the DOOR recognises a row whose run directory
// already holds a record, and that predicate reads the run directory, not the
// status (a row is `claimed`/`in_progress` on its first run AND its second, so
// the status cannot tell them apart; see tools.TaskRedispatchResumeFrom).

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"

	"github.com/nick/gopi/internal/config"
	"github.com/nick/gopi/internal/sessionevent"
	"github.com/nick/gopi/internal/testutil"
	"github.com/nick/gopi/internal/tools"
)

const (
	// redispatchWitnessTaskID is the row this walk dispatches twice.
	redispatchWitnessTaskID = "6f3b1c2d-8a44-4e19-9c07-redispatchwit1"
	// redispatchWitnessSummary is the row's summary.
	redispatchWitnessSummary = "redispatch resume witness e2e"
	// redispatchWitnessPayload is the row's payload.
	redispatchWitnessPayload = "the payload the executor receives in both runs"
)

// redispatchWitnessBin builds gopi from THIS tree once per test binary and pins
// it in testutil.BinaryPinEnv.
//
// WHY NOT requireE2EGopiBin: that helper's build arm runs `go build -o <out> .`
// with no Dir set, so it builds in the OUTPUT directory — and this suite's own
// pin has already moved the test's cwd into a scratch root that holds no go.mod
// (measured: "go.mod file not found in current directory or any parent
// directory"). The runner given the fix is the one that must be built, so the
// build runs in THIS package's directory (resolved from the test file's own
// path, never from cwd).
func redispatchWitnessBin(t *testing.T) string {
	t.Helper()
	redispatchWitnessBinOnce.Do(func() {
		goBin, err := exec.LookPath("go")
		if err != nil {
			redispatchWitnessBinErr = fmt.Errorf("no `go` toolchain on PATH to build the runner: %w", err)
			return
		}
		_, self, _, ok := runtime.Caller(0)
		if !ok {
			redispatchWitnessBinErr = fmt.Errorf("cannot resolve this test file's own path to locate the package directory")
			return
		}
		dir := filepath.Join(testutil.SuiteTempRoot(), "redispatch-witness-bin")
		if err := os.MkdirAll(dir, 0o755); err != nil {
			redispatchWitnessBinErr = fmt.Errorf("create %s: %w", dir, err)
			return
		}
		bin := filepath.Join(dir, "gopi")
		cmd := exec.Command(goBin, "build", "-o", bin, ".")
		cmd.Dir = filepath.Dir(self)
		cmd.Env = append(os.Environ(), "CGO_ENABLED=0")
		if out, err := cmd.CombinedOutput(); err != nil {
			redispatchWitnessBinErr = fmt.Errorf("go build -o %s . in %s: %w\n%s",
				bin, cmd.Dir, err, strings.TrimSpace(string(out)))
			return
		}
		redispatchWitnessBinPath = bin
	})
	if redispatchWitnessBinErr != nil {
		t.Fatalf("the walk needs a runner built from THIS tree: %v", redispatchWitnessBinErr)
	}
	t.Setenv(testutil.BinaryPinEnv, redispatchWitnessBinPath)
	return redispatchWitnessBinPath
}

var (
	redispatchWitnessBinOnce sync.Once
	redispatchWitnessBinPath string
	redispatchWitnessBinErr  error
)

// TestRedispatchDoorWritesTheResumeWitness is the acceptance walk.
func TestRedispatchDoorWritesTheResumeWitness(t *testing.T) {
	SandboxRepoContext(t)
	// THE SURFACE UNDER TEST IS A BINARY, NOT AN IN-PROCESS FUNCTION. The door
	// (cli_tasks.go) and the record writer (internal/agent) are two PROCESSES
	// here, so a walk that drove an in-process constructor would judge a caller
	// the fix never reaches.
	redispatchWitnessBin(t)
	mock := testutil.NewMockLLM(t, testutil.Resp{Text: "run done"})
	defer mock.Close()

	gopiHome := t.TempDir()
	dbPath := filepath.Join(t.TempDir(), "memory.db")
	t.Setenv("GOPI_MEMORY_DB", dbPath) // parent-process tools.* calls below
	env := testutil.MockEnv(t, gopiHome, mock.URL())
	env = append(env,
		"GOPI_MEMORY_DB="+dbPath,
		config.SkipClaimBroadcastEnv+"=1",
	)

	if _, err := tools.TaskSubmit(tools.TaskSubmitOptions{
		Summary: redispatchWitnessSummary, Payload: redispatchWitnessPayload, TaskID: redispatchWitnessTaskID,
	}); err != nil {
		t.Fatalf("submit: %v", err)
	}

	sessionDir := filepath.Join(gopiHome, ".gopi", config.SessionsDirName, redispatchWitnessTaskID)

	// ── RUN 1 — the row's FIRST run: a session record, and NO witness ────────
	stdout, stderr, code := runGopiMapped(t, []string{"worker", redispatchWitnessTaskID}, "", env, "")
	if code != 0 {
		t.Fatalf("first run must succeed on a well-formed row: code=%d\nstdout:\n%s\nstderr:\n%s", code, stdout, stderr)
	}
	firstStarts := readSessionStarts(t, sessionDir)
	if len(firstStarts) != 1 {
		t.Fatalf("the first run must leave exactly one session_start, got %d — the walk cannot judge a second one it never produced:\n%s",
			len(firstStarts), joinStarts(firstStarts))
	}
	if got := firstStarts[0][sessionevent.SessionStartFieldResumedFrom]; got != nil && got != "" {
		t.Fatalf("the FIRST run of a row carries a resume witness (%v) — absence is what makes a witness mean something: a first run must not claim to continue anything",
			got)
	}
	firstSessionID := stringField(firstStarts[0], "sessionId")
	if firstSessionID == "" {
		t.Fatalf("the first session_start names no session id — nothing identifies the segment a re-dispatch would continue:\n%s", joinStarts(firstStarts))
	}

	// ── the requeue (fixture) — see the file header ─────────────────────────
	requeueRowForRedispatch(t, dbPath, redispatchWitnessTaskID)

	// ── RUN 2 — the RE-DISPATCH: the same row, the same record file ─────────
	stdout, stderr, code = runGopiMapped(t, []string{"worker", redispatchWitnessTaskID}, "", env, "")
	if code != 0 {
		t.Fatalf("the re-dispatch must succeed: code=%d\nstdout:\n%s\nstderr:\n%s", code, stdout, stderr)
	}
	starts := readSessionStarts(t, sessionDir)
	if len(starts) < 2 {
		t.Fatalf("the re-dispatch wrote no second session_start under %s (got %d) — the door never re-entered the record, so this walk cannot judge the witness it exists for:\n%s",
			sessionDir, len(starts), joinStarts(starts))
	}
	second := starts[len(starts)-1]
	witness := stringField(second, sessionevent.SessionStartFieldResumedFrom)
	if witness == "" {
		t.Fatalf("the SECOND session_start of a re-dispatched row carries NO resume witness — this is the defect: the record holds %d headers and cannot say why. Header: %v",
			len(starts), second)
	}
	if witness != firstSessionID {
		t.Fatalf("the witness names %q, but the run it continues is %q — the door must declare the session that actually ran, not a nearby id",
			witness, firstSessionID)
	}
	// THE PAIR IS WRITTEN TOGETHER (the writer's own contract): a witness
	// without the raw-log half would tell a reader the file was continued but
	// not which wire log grew with it.
	rawLogPath := stringField(second, sessionevent.SessionStartFieldResumedFromRawLog)
	if rawLogPath == "" {
		t.Fatalf("the witness carries no %s — the record says it was continued but not which raw log grew with it: %v",
			sessionevent.SessionStartFieldResumedFromRawLog, second)
	}
	// The raw-log half must name the log of THIS row's run directory, never a
	// path the door invented.
	if filepath.Dir(rawLogPath) != sessionDir {
		t.Fatalf("the witness's raw-log half names %q, which is not inside the row's own run directory %q — a resumed segment must grow the files the session already owns",
			rawLogPath, sessionDir)
	}
}

// readSessionStarts decodes every `session_start` row of the run directory's
// records, in file order.
func readSessionStarts(t *testing.T, dir string) []map[string]interface{} {
	t.Helper()
	matches, err := filepath.Glob(filepath.Join(dir, "*"+".jsonl"))
	if err != nil || len(matches) == 0 {
		return nil
	}
	var out []map[string]interface{}
	for _, path := range matches {
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read %s: %v", path, err)
		}
		for _, line := range strings.Split(string(data), "\n") {
			line = strings.TrimSpace(line)
			if line == "" || !strings.Contains(line, sessionevent.EventTypeSessionStart) {
				continue
			}
			var rec map[string]interface{}
			if err := json.Unmarshal([]byte(line), &rec); err != nil {
				continue // a torn tail line is not a header this walk judges
			}
			if rec["type"] != sessionevent.EventTypeSessionStart {
				continue
			}
			out = append(out, rec)
		}
	}
	return out
}

// stringField reads a string cell out of a decoded JSON object, "" when absent
// or not a string (a header is decoded loosely on purpose: the walk asserts on
// what the record SAYS, with no struct that could silently default it).
func stringField(rec map[string]interface{}, key string) string {
	if v, ok := rec[key].(string); ok {
		return v
	}
	return ""
}

func joinStarts(starts []map[string]interface{}) string {
	var b strings.Builder
	for i, s := range starts {
		b.WriteString(strings.TrimSpace(stringField(s, "timestamp")))
		b.WriteString(" session=" + stringField(s, "sessionId"))
		b.WriteString(" resumedFrom=" + stringField(s, sessionevent.SessionStartFieldResumedFrom))
		if i < len(starts)-1 {
			b.WriteString("\n")
		}
	}
	return b.String()
}

// requeueRowForRedispatch puts a terminal row back in the queue — FIXTURE, not
// product (see the file header). It writes the ledger the sandboxed run already
// owns, so nothing outside this test's temp profile is touched.
func requeueRowForRedispatch(t *testing.T, dbPath, taskID string) {
	t.Helper()
	t.Setenv("GOPI_MEMORY_DB", dbPath)
	ctx := context.Background()
	db, err := tools.OpenTasksDBForSweep(ctx)
	if err != nil {
		t.Fatalf("open ledger: %v", err)
	}
	defer db.Close()
	res, err := db.ExecContext(ctx,
		`UPDATE tasks SET status = ?, claimed_by = '' WHERE task_id = ?`,
		tools.TaskStatusPending, taskID)
	if err != nil {
		t.Fatalf("requeue %s: %v", taskID, err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		t.Fatalf("requeue %s: cannot read how many rows the requeue touched: %v", taskID, err)
	}
	if n != 1 {
		t.Fatalf("requeue %s touched %d rows — the row the walk dispatched is not the row it requeued", taskID, n)
	}
}
