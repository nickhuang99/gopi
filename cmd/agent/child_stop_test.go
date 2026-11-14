package main

// child_stop_test.go — the regression guard for the TWO defects that made the
// mass kill both destructive and untraceable (epic signal-provenance,
// 2026-09-13):
//
//	1. a bounded wait ended with a bare SIGKILL, so the child never ran a defer
//	   (no session_end, no ledger close-out) — the child's work died with it;
//	2. nothing recorded WHO killed it, and a SIGKILL's victim cannot record its
//	   own death — so the fleet's logs could only ever show the victim's terminal
//	   line, and the sender stayed undetermined.
//
// The tests drive real child processes (never a real gopi fleet) through the
// real helper and assert on the real provenance file, because the property in
// question is "what happened to this OS process", which no in-process fake can
// answer.

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/nickhuang99/gopi/internal/config"
	"github.com/nickhuang99/gopi/internal/signalrec"
	"github.com/nickhuang99/gopi/internal/tools"
)

// Child-startup and death-probe budgets (Rule 1a: named, never inline).
const (
	// csChildStartupBuddy is how long a freshly spawned `sh -c` child is given
	// to reach its script body before it is signalled. It is a start-up
	// HANDSHAKE budget, not a timing assumption about the stop under test.
	csChildStartupBuddy = 150 * time.Millisecond
	// csPollInterval is the death-probe poll interval (SIGKILL escalation).
	csPollInterval = 20 * time.Millisecond
)

// csChild starts a child in its OWN process group (matching how the merger and
// the verify gate spawn theirs: Setpgid, so a bounded stop can reach
// grandchildren) and returns it with a waiter channel.
func csChild(t *testing.T, script string) (*exec.Cmd, <-chan struct{}) {
	t.Helper()
	// Provenance handoff (epic signalrec-session-attribution): this TEST process
	// is the killer whose intent records are asserted below. It hands the
	// recorder a REAL session dir of its own — process-scoped, keyed by its pid,
	// carrying the container's metadata.json — so the records land in a session
	// dir by the ordinary rule (no test-only seam, and nothing is written into
	// the agent session that runs the suite). csKillerSessionDir is that dir; the
	// Set below is what makes the resolution USE it.
	home := t.TempDir()
	t.Setenv(config.GopiDirEnv, home)
	t.Setenv(tools.TaskRunIDEnv, "")
	t.Setenv(tools.TaskWrapperEnv, "")
	t.Setenv(signalrec.SessionDirEnv, "")
	t.Setenv(signalrec.TaskIDEnv, "")
	dir := csKillerSessionDir(t, home)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("create killer session dir %s: %v", dir, err)
	}
	if err := os.WriteFile(filepath.Join(dir, "metadata.json"), []byte("{}"), 0o644); err != nil {
		t.Fatalf("seed the killer session dir's marker: %v", err)
	}
	signalrec.SetSessionDir(dir, "", "")
	t.Cleanup(func() { signalrec.SetSessionDir("", "", "") })
	cmd := exec.Command("sh", "-c", script)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := cmd.Start(); err != nil {
		t.Fatalf("start child %q: %v", script, err)
	}
	done := make(chan struct{})
	go func() {
		_ = cmd.Wait() //stylegate:ignore the exit status IS delivered via done; Wait's error is not the signal under test
		close(done)
	}()
	// Make sure the child has reached its sleep before it is signalled.
	time.Sleep(csChildStartupBuddy)                                           //stylegate:allow-sleep start-up handshake: the child must be INSIDE its script before the signal, or the assertion proves nothing about the stop path
	t.Cleanup(func() { _ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL) }) //stylegate:ignore best-effort signal to the process GROUP in t.Cleanup; the child is reaped through the done channel this test asserts on
	return cmd, done
}

// csRecords reads the intent records the killer (this test process) wrote.
// epic signalrec-session-attribution (2026-09-14): the records land in a SESSION
// dir, not in the retired public logs dir — csChild hands this test process its
// own process-scoped session dir (keyed by the test binary's pid, so no agent
// session's directory is ever written). Asserting on that PATH is the point: a
// test that read a global file would pass while the artifact stayed
// unattributable.
func csRecords(t *testing.T) []map[string]any {
	t.Helper()
	return csIntentRecordsIn(t, csKillerSessionDir(t, csHome(t)))
}

// csHome is the temp gopi home csChild pinned (it Setenv'd GOPI_HOME; the value
// is read back rather than duplicated at every call site).
func csHome(t *testing.T) string {
	t.Helper()
	return os.Getenv(config.GopiDirEnv)
}

// csKillerSessionDir is where a `task stop`-shaped test process files its
// records: the process-scoped session dir keyed by this test binary's pid.
func csKillerSessionDir(t *testing.T, home string) string {
	t.Helper()
	return filepath.Join(home, ".gopi", config.SessionsDirName,
		signalrec.ProcessDirPrefix+strconv.Itoa(os.Getpid()))
}

// csIntentRecordsIn parses the provenance file of one session dir (nil when the
// file is absent).
func csIntentRecordsIn(t *testing.T, dir string) []map[string]any {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(dir, signalrec.FileName))
	if err != nil {
		return nil
	}
	var out []map[string]any
	for _, line := range strings.Split(strings.TrimSpace(string(b)), "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		var m map[string]any
		if err := json.Unmarshal([]byte(line), &m); err != nil {
			t.Fatalf("provenance line %q is not JSON: %v", line, err)
		}
		out = append(out, m)
	}
	return out
}

// csAssertNoPublicSink fails when anything was written under the retired public
// logs dir — the no-fallback assertion of epic signalrec-session-attribution.
func csAssertNoPublicSink(t *testing.T, home string) {
	t.Helper()
	legacy := filepath.Join(home, ".gopi", signalrec.LogDirName)
	entries, err := os.ReadDir(legacy)
	if err != nil {
		return // absent = nothing written there
	}
	if len(entries) > 0 {
		names := make([]string, 0, len(entries))
		for _, e := range entries {
			names = append(names, e.Name())
		}
		t.Errorf("the retired public sink dir %s is non-empty (%v) — signalrec must never write there", legacy, names)
	}
}

// TestStopChildGracefullySignalsTermFirst is the CORE regression: a child that
// honours SIGTERM must be stopped by SIGTERM, and the helper must say so. Before
// this change the only path was SIGKILL, so a child never got the chance to run
// the close-out work that keeps the ledger coherent.
func TestStopChildGracefullySignalsTermFirst(t *testing.T) {
	cmd, done := csChild(t, "sleep 60")
	outcome := stopChildGracefully(cmd.Process, cmd.Process.Pid,
		"test-child", "unit test reached its wait cap", waitChildExitGone(done))

	if outcome != childStopStopped {
		t.Errorf("outcome = %q, want %q — SIGTERM alone must end a cooperative child", outcome, childStopStopped)
	}

	// The intent line must name the killer, the target and the reason: it is
	// the ONLY evidence a SIGKILL would have left, so it must be written on
	// every path, not just the escalated one.
	recs := csRecords(t)
	if len(recs) != 1 {
		t.Fatalf("want exactly 1 intent record, got %d", len(recs))
	}
	r := recs[0]
	if r["kind"] != signalrec.KindIntent {
		t.Errorf("kind = %v, want %q", r["kind"], signalrec.KindIntent)
	}
	if int(r["target_pid"].(float64)) != cmd.Process.Pid {
		t.Errorf("target_pid = %v, want the child %d", r["target_pid"], cmd.Process.Pid)
	}
	if reason, _ := r["reason"].(string); !strings.Contains(reason, "unit test reached its wait cap") {
		t.Errorf("reason = %q, want the caller's reason verbatim", reason)
	}
	if int(r["pid"].(float64)) == 0 {
		t.Error("intent record does not name the killer pid")
	}
}

// TestStopChildGracefullyEscalatesOnlyAfterTheGrace pins the ordering: a child
// that IGNORES SIGTERM is still killed — but only after the grace the helper
// granted it has actually expired (here: simulated by a waiter reporting
// "not gone"). Without this, a wedged child could hold the merger forever,
// which is the reason the original code reached for SIGKILL first.
func TestStopChildGracefullyEscalatesOnlyAfterTheGrace(t *testing.T) {
	// trap "" TERM: the child deliberately ignores the graceful signal.
	cmd, _ := csChild(t, `trap "" TERM; sleep 60`)
	probeCalled := false
	outcome := stopChildGracefully(cmd.Process, cmd.Process.Pid,
		"test-child", "unit test — child ignores SIGTERM",
		func(time.Duration) bool { probeCalled = true; return false })

	if !probeCalled {
		t.Error("the grace probe was never called — the helper escalated without offering a graceful stop")
	}
	if outcome != childStopKilled {
		t.Errorf("outcome = %q, want %q for a child that ignores SIGTERM", outcome, childStopKilled)
	}
	// And it really is dead (SIGKILL cannot be ignored).
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if err := syscall.Kill(cmd.Process.Pid, 0); err != nil {
			return
		}
		time.Sleep(csPollInterval) //stylegate:allow-sleep bounded poll for the escalated child's death; the loop exits the moment the kernel reports ESRCH and the deadline caps it
	}
	t.Error("child survived the escalation — the bounded wait can no longer bound anything")
}

// TestStopChildGracefullyToleratesAnAlreadyDeadChild guards the bounded-wait
// path's most common real case: the child finished in the millisecond between
// the cap firing and the signal. This must be a benign no-op, never a panic or
// a wrong outcome label.
func TestStopChildGracefullyToleratesAnAlreadyDeadChild(t *testing.T) {
	cmd, done := csChild(t, "true")
	<-done // it is gone
	dead := cmd.Process

	outcome := stopChildGracefully(dead, dead.Pid, "test-child", "already gone", waitChildExitGone(done))
	if outcome != childStopStopped {
		t.Errorf("outcome = %q for an already-exited child, want %q", outcome, childStopStopped)
	}
	if recs := csRecords(t); len(recs) != 0 {
		t.Errorf("an already-exited child produced %d intent records — no signal was sent, so nothing happened to record", len(recs))
	}
	csAssertNoPublicSink(t, csHome(t))
}
