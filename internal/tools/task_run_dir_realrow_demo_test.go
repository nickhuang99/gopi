package tools

// MEASURED DEMO on the LIVE ledger (epic run-identity-unify, 2026-09-20).
//
// Skipped by default: it reads the operator's real ~/.gopi ledger and sessions
// tree, so it must never run in CI or on the merge path (Rule 27's spirit — a
// test that needs a real host is a demo, not a gate). Run it explicitly:
//
//	GOPI_TASK_RUNID_DEMO_REAL=1 go test ./internal/tools/ \
//	  -run TestRunIdentityDemoRealMultiRunRow -v -count=1
//
// What it produces is the epic's evidence: on a REAL multi-run row, the answer
// the OLD session-keyed chain gives, the answer the NEW row-keyed path gives,
// and the wall-clock cost of each.

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/nickhuang99/gopi/internal/config"
)

// demoRealRowTaskIDEnv names the ledger row to demo on, so the test is not
// welded to one host's data.
const demoRealRowTaskIDEnv = "GOPI_TASK_RUNID_DEMO_TASK_ID"

// demoRealRowEnv is the opt-in switch (see the file header).
const demoRealRowEnv = "GOPI_TASK_RUNID_DEMO_REAL"

func TestRunIdentityDemoRealMultiRunRow(t *testing.T) {
	if !envTruthy(demoRealRowEnv) {
		t.Skipf("%s not set — this demo reads the operator's live ledger", demoRealRowEnv)
	}
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatalf("home: %v", err)
	}
	// Point the resolution at the REAL data root and ledger.
	t.Setenv(config.GopiDirEnv, home)
	t.Setenv("GOPI_MEMORY_DB", filepath.Join(home, config.ProfileDirName, "memory.db"))
	taskID := os.Getenv(demoRealRowTaskIDEnv)
	if taskID == "" {
		t.Fatalf("%s must name the row to demo", demoRealRowTaskIDEnv)
	}

	// The row's ledger column (what the old chain took as its key).
	sid, err := TaskSessionID(taskID)
	if err != nil {
		t.Fatalf("TaskSessionID(%s): %v", taskID, err)
	}
	t.Logf("row %s: ledger session_id column = %q", taskID, sid)

	// OLD chain, verbatim and timed: column -> raw log (whole-tree search).
	t0 := time.Now()
	oldChain := taskRawLogPathFromSession(sid)
	oldDur := time.Since(t0)
	t.Logf("OLD chain  taskRawLogPathFromSession(%q) = %q  [%s]", sid, oldChain, oldDur)

	// NEW path, timed: row id -> its own run directory.
	t1 := time.Now()
	recs := taskRuns(taskID)
	newChain := taskRunResumeRawLog(taskID)
	newDur := time.Since(t1)
	t.Logf("NEW path   runs in %s = %d; taskRunResumeRawLog = %q  [%s]",
		taskRunDir(taskID), len(recs), newChain, newDur)
	for i, r := range recs {
		t.Logf("  run[%d] id=%s started=%v end=%v turns=%d tokens=%d rawLog=%q",
			i, r.RunID, r.Started.Format(time.RFC3339), r.HasEnd, r.Turns, r.Tokens, r.RawLogPath)
	}

	// The RESUME ENTRY POINT end to end (`gopi session resume <task-id>`): this is the
	// call whose old body read the ledger column and paid the tree search.
	t2 := time.Now()
	_, resumeRaw, err := TaskResumeTarget(taskID)
	resumeDur := time.Since(t2)
	if err != nil {
		t.Fatalf("TaskResumeTarget(%s): %v", taskID, err)
	}
	t.Logf("RESUME ENTRY TaskResumeTarget(%s) -> %q  [%s]", taskID, resumeRaw, resumeDur)
	t.Logf("  (recorded-but-missing: %v)", !fileExistsAt(resumeRaw))

	// The rendered row: what an operator sees now.
	out, err := TaskShow(taskID)
	if err != nil {
		t.Fatalf("TaskShow(%s): %v", taskID, err)
	}
	t.Logf("task show %s:\n%s", taskID, out)
}

// envTruthy is the demo's opt-in parse (1/true/yes).
func envTruthy(name string) bool {
	switch v := os.Getenv(name); v {
	case "1", "true", "TRUE", "yes", "YES":
		return true
	}
	return false
}

// fileExistsAt is the demo's existence probe for a resolved checkpoint.
func fileExistsAt(path string) bool {
	if path == "" {
		return false
	}
	fi, err := os.Stat(path)
	return err == nil && !fi.IsDir()
}
