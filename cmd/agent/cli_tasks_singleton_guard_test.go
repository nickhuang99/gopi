package main

// E2E wiring proof for the pre-spawn singleton guard (epic
// task-run-singleton-guard, 2026-09-15): `gopi task run <id>` on a row a LIVE
// process already holds must exit non-zero WITHOUT running the briefing, and
// `--takeover` must be the deliberate escape. The decision itself is proven
// deterministically in internal/tools/taskrunguard_test.go; what these tests
// add is the wiring: the real binary, a real ledger, a real holder process, and
// the real exit code a duplicate launcher sees.

import (
	"database/sql"
	"errors"
	"github.com/nick/gopi/internal/config"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	_ "modernc.org/sqlite"

	"github.com/nick/gopi/internal/testutil"
	"github.com/nick/gopi/internal/tools"
)

// guardProbeShell is the shell the fixture holder runs (Rule 1b: no bare path
// literal) and its argv — it must outlive the test's assertions.
const (
	guardProbeShell = "/bin/bash"
	guardProbeArgv  = "exec sleep 60"
)

// guardProbeIdent spawns a REAL process that holds the row and returns its
// three-factor identity the way the ledger records it: pid, /proc start ticks
// and the command line. The parse mirrors internal/tools' procStartTicks (the
// fields after the parenthesised comm, starttime = field 22).
func guardProbeIdent(t *testing.T) (pid int, start int64, cmd string, stop func()) {
	t.Helper()
	child := exec.Command(guardProbeShell, "-c", guardProbeArgv)
	if err := child.Start(); err != nil {
		t.Skipf("cannot spawn the holder fixture: %v", err)
	}
	pid = child.Process.Pid
	stop = func() { guardStopProbe(t, child) }
	raw, err := os.ReadFile(filepath.Join("/proc", strconv.Itoa(pid), "stat"))
	if err != nil {
		stop()
		t.Skipf("cannot read the fixture's stat: %v", err)
	}
	i := strings.LastIndexByte(string(raw), ')')
	if i < 0 {
		stop()
		t.Skipf("unparsable stat for pid %d", pid)
	}
	fields := strings.Fields(string(raw)[i+1:])
	// starttime is field 22 overall; three fields precede the post-comm list.
	const startTimeField, postCommOffset = 22, 3
	if len(fields) <= startTimeField-postCommOffset {
		stop()
		t.Skipf("stat for pid %d has too few fields", pid)
	}
	start, err = strconv.ParseInt(fields[startTimeField-postCommOffset], 10, 64)
	if err != nil {
		stop()
		t.Skipf("unparsable starttime for pid %d: %v", pid, err)
	}
	cmdRaw, err := os.ReadFile(filepath.Join("/proc", strconv.Itoa(pid), "cmdline"))
	if err != nil {
		stop()
		t.Skipf("cannot read the fixture's cmdline: %v", err)
	}
	cmd = strings.TrimSpace(strings.Join(strings.Split(strings.TrimRight(string(cmdRaw), "\x00"), "\x00"), " "))
	return pid, start, cmd, stop
}

// guardStopProbe ends the holder fixture, RECORDING a teardown failure instead
// of discarding it (Rule 15b): a failed kill would leave a `sleep` behind, and
// the next fixture that reuses the pid would then be judged against the wrong
// process.
func guardStopProbe(t *testing.T, child *exec.Cmd) {
	t.Helper()
	if err := child.Process.Kill(); err != nil && !errors.Is(err, os.ErrProcessDone) {
		t.Errorf("probe teardown: kill: %v", err)
	}
	if _, err := child.Process.Wait(); err != nil {
		t.Errorf("probe teardown: wait: %v", err)
	}
}

// guardSeedRow submits a row and stamps it as CLAIMED by the given holder with
// the given recorded identity — the state a duplicate launcher meets.
func guardSeedRow(t *testing.T, dbPath, taskID, holder string, pid int, start int64, cmd string) {
	t.Helper()
	db, err := sql.Open("sqlite", tools.LedgerDSN(dbPath))
	if err != nil {
		t.Fatalf("open ledger: %v", err)
	}
	defer db.Close()
	if _, err := db.Exec(
		`UPDATE tasks SET status = 'claimed', claimed_by = ?, proc_pid = ?, proc_start_ticks = ?, proc_cmd = ?
		 WHERE task_id = ?`, holder, pid, start, cmd, taskID); err != nil {
		t.Fatalf("seed %s: %v", taskID, err)
	}
}

// TestTaskRunRefusesLiveHolderE2E is the epic's acceptance shape at the CLI:
// a second `gopi task run <id>` for a row a live process holds exits non-zero,
// names the holder, and leaves the ledger untouched (no in_progress flip, no
// child spawned).
func TestTaskRunRefusesLiveHolderE2E(t *testing.T) {
	gopiHome := t.TempDir()
	dbPath := filepath.Join(t.TempDir(), "memory.db")
	work := t.TempDir()
	t.Setenv("GOPI_MEMORY_DB", dbPath)
	env := testutil.MockEnv(t, gopiHome, "http://127.0.0.1:1")
	env = append(env, "GOPI_MEMORY_DB="+dbPath, config.SkipClaimBroadcastEnv+"=1")

	if _, stderr, code := runGopiMapped(t, []string{"task", "submit",
		"singleton guard e2e", "--payload", "do the work", "--task-id", "a07a3562-eed3-5e23-ba8a-9fb6d82ed6a8"}, "", env, ""); code != 0 {
		t.Fatalf("submit: code=%d stderr=%s", code, stderr)
	}
	pid, start, cmd, stop := guardProbeIdent(t)
	defer stop()
	host, err := os.Hostname()
	if err != nil {
		t.Skipf("no hostname: %v", err)
	}
	holder := host + ":" + strconv.Itoa(pid)
	guardSeedRow(t, dbPath, "a07a3562-eed3-5e23-ba8a-9fb6d82ed6a8", holder, pid, start, cmd)

	stdout, stderr, code := runGopiMapped(t, []string{"worker", "a07a3562-eed3-5e23-ba8a-9fb6d82ed6a8"}, "", env, work)
	if code == 0 {
		t.Fatalf("a second run of a live-held row must exit non-zero\nstdout:\n%s\nstderr:\n%s", stdout, stderr)
	}
	combined := stdout + stderr
	for _, want := range []string{"already RUNNING", holder, "task status"} {
		if !strings.Contains(combined, want) {
			t.Errorf("the refusal must carry %q, got:\n%s", want, combined)
		}
	}
	// The ledger is UNTOUCHED: still claimed by the same holder (a refused run
	// must not leave a half-claim behind, and must not have advanced the row).
	show, err := tools.TaskShow("a07a3562-eed3-5e23-ba8a-9fb6d82ed6a8")
	if err != nil {
		t.Fatalf("show: %v", err)
	}
	if !strings.Contains(show, tools.TaskStatusClaimed) {
		t.Errorf("the refused run must leave the row claimed:\n%s", show)
	}
	st, err := tools.TaskProcessStatusFor("a07a3562-eed3-5e23-ba8a-9fb6d82ed6a8")
	if err != nil {
		t.Fatalf("status: %v", err)
	}
	if st.Holder != holder {
		t.Errorf("holder = %q, want the original %q (the refused run re-stamped it)", st.Holder, holder)
	}
	if st.Verdict != tools.TaskProcessStateRunning {
		t.Errorf("verdict = %q, want %q — the live holder is still there", st.Verdict, tools.TaskProcessStateRunning)
	}
}

// TestTaskRunTakeoverOverrideE2E: --takeover is the deliberate escape — the run
// proceeds past the guard, and the row then follows the ordinary run path (the
// hermetic synthetic child failure is used here, so no model call happens).
func TestTaskRunTakeoverOverrideE2E(t *testing.T) {
	gopiHome := t.TempDir()
	dbPath := filepath.Join(t.TempDir(), "memory.db")
	work := t.TempDir()
	t.Setenv("GOPI_MEMORY_DB", dbPath)
	env := testutil.MockEnv(t, gopiHome, "http://127.0.0.1:1")
	env = append(env,
		"GOPI_MEMORY_DB="+dbPath,
		config.SkipClaimBroadcastEnv+"=1",
		taskChildFailTestEnv+"=1", // hermetic child: no model call, no API key
	)
	if _, stderr, code := runGopiMapped(t, []string{"task", "submit",
		"takeover e2e", "--payload", "do the work", "--task-id", "12e77764-5ab0-53ea-97d0-16b32a83a4c4"}, "", env, ""); code != 0 {
		t.Fatalf("submit: code=%d stderr=%s", code, stderr)
	}
	pid, start, cmd, stop := guardProbeIdent(t)
	defer stop()
	host, err := os.Hostname()
	if err != nil {
		t.Skipf("no hostname: %v", err)
	}
	guardSeedRow(t, dbPath, "12e77764-5ab0-53ea-97d0-16b32a83a4c4", host+":"+strconv.Itoa(pid), pid, start, cmd)

	stdout, stderr, _ := runGopiMapped(t, []string{"worker", "12e77764-5ab0-53ea-97d0-16b32a83a4c4", "--takeover", "--as", "taker:1"}, "", env, work)
	combined := stdout + stderr
	if !strings.Contains(combined, "--takeover") {
		t.Errorf("--takeover must announce the override, got:\n%s", combined)
	}
	// The run reached the ordinary path: the hermetic child failed, so the row
	// is terminal rather than still claimed by the probe holder.
	if !strings.Contains(combined, "synthetic child failure") {
		t.Errorf("the overridden run must reach the child path, got:\n%s", combined)
	}
	if st := taskStatusOf(t, "12e77764-5ab0-53ea-97d0-16b32a83a4c4"); st == tools.TaskStatusClaimed {
		t.Errorf("the overridden run must leave the claim state (status still %q)", st)
	}
}

// TestTaskStatusAgreesWithGuard: the operator's inspection verb and the run
// door must read the SAME verdict for the same row — one decision, two faces
// (Rule 14). A live holder is "running" to `task status` and a refusal to
// `task run`.
func TestTaskStatusAgreesWithGuard(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "memory.db")
	t.Setenv("GOPI_MEMORY_DB", dbPath)
	pid, start, cmd, stop := guardProbeIdent(t)
	defer stop()
	host, err := os.Hostname()
	if err != nil {
		t.Skipf("no hostname: %v", err)
	}
	if _, err := tools.TaskSubmit(tools.TaskSubmitOptions{
		Summary: "status agrees with guard", Payload: "payload", TaskID: "t-guard-agree", Source: "human",
	}); err != nil {
		t.Fatalf("submit: %v", err)
	}
	holder := host + ":" + strconv.Itoa(pid)
	guardSeedRow(t, dbPath, "t-guard-agree", holder, pid, start, cmd)

	st, err := tools.TaskProcessStatusFor("t-guard-agree")
	if err != nil {
		t.Fatalf("status: %v", err)
	}
	verdict, refusal, err := tools.TaskRunGuardFor("t-guard-agree")
	if err != nil {
		t.Fatalf("guard: %v", err)
	}
	if st.Verdict != tools.TaskProcessStateRunning {
		t.Fatalf("task status verdict = %q, want %q", st.Verdict, tools.TaskProcessStateRunning)
	}
	if verdict != tools.TaskRunHolderLive {
		t.Fatalf("guard verdict = %q, want %q (the two faces disagree)", verdict, tools.TaskRunHolderLive)
	}
	if refusal == "" {
		t.Errorf("a live-holder refusal must explain itself")
	}
}
