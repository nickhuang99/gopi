package tools

// THE CWD-ROW RUNG's regression suite (taskproc_cwd_row.go).
//
// THE BUG THESE TESTS PIN (measured on this host 2026-11-01, row
// 01a19075-80b5): a `gopi worker` that finished row A and went on to drain the
// queue for row B still matches A's recorded identity EXACTLY — same pid, same
// incarnation, same argv — so A read `verdict: running`, `resumable: no`, and
// A's recovery path was blocked by a process that had not touched A again.
// /proc/1604177/cwd pointed at row 01a1954e's session container.
//
// WHY THE FIXTURE IS A SYNTHETIC PROCFS TREE. The defect needs a live pid whose
// /proc/<pid>/cwd names a DIFFERENT row than the one its ledger row records, and
// a test cannot move a real child process's cwd. So the identity side and the
// cwd side are fed from one fixture root (identityProcRoot) while the pid itself
// is a REAL spawned child — its pid and start ticks are recorded verbatim and the
// fixture's stat line repeats them, so the pid-level factors AGREE and only the
// cwd can be what changes the verdict. Anything else would let the test pass for
// the wrong reason (an identity mismatch dressed up as a cwd finding).

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"

	"github.com/nickhuang99/gopi/internal/config"
)

const (
	// cwdRowFixtureA / cwdRowFixtureB are the two ledger rows the scenario needs:
	// A records the pid, B is the row that pid is REALLY serving.
	// The two ids are the REAL ones from the measured incident, and they must be
	// canonical uuids: the rung recognises a row by internal/scratch's MINTED
	// container-name predicate, so a made-up id would make every arm here pass
	// for the wrong reason ("the name is not a row name").
	cwdRowFixtureA = "01a19075-80b5-7a85-b0df-02a55f1c8b16"
	cwdRowFixtureB = "01a1954e-17a6-7954-ad7a-99cad43f41cb"
	// cwdRowFixtureSleepArg is the probe child's argv[1], and it is the ONE
	// definition both sides read: the spawned command AND the fixture's cmdline
	// file AND the recorded column. It was a literal `60` spelled in three
	// places during this suite's own red-proof, and the drift made a fully
	// matching identity read `recycled` ("command line does not match") — a
	// false negative that would have hidden the cwd finding behind a pid-level
	// verdict.
	cwdRowFixtureSleepArg = "60"
	// cwdRowFixtureSleeperCmd is the command line recorded for the holder,
	// derived from the same argument so the two cannot drift.
	cwdRowFixtureSleeperCmd = "sleep " + cwdRowFixtureSleepArg
)

// cwdRowFixture describes the synthetic procfs tree the rung is driven over.
type cwdRowFixture struct {
	pid        int
	startTicks int64
	procRoot   string // the fixture procfs root (identityProcRoot)
	rowDirA    string // session container of row A
	rowDirB    string // session container of row B
}

// newCwdRowFixture builds the two ledger rows, the two session containers and a
// synthetic procfs tree whose <pid>/cwd points at row A's container. The caller
// REPOINTS that symlink to model the handoff.
func newCwdRowFixture(t *testing.T) *cwdRowFixture {
	t.Helper()
	home := t.TempDir()
	t.Setenv(config.GopiDirEnv, home)
	t.Setenv("GOPI_MEMORY_DB", filepath.Join(t.TempDir(), "memory.db"))

	sessionsRoot := filepath.Join(home, config.ProfileDirName, config.SessionsDirName)
	f := &cwdRowFixture{
		rowDirA: filepath.Join(sessionsRoot, cwdRowFixtureA),
		rowDirB: filepath.Join(sessionsRoot, cwdRowFixtureB),
	}
	for _, dir := range []string{f.rowDirA, f.rowDirB} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatalf("fixture: mkdir %s: %v", dir, err)
		}
	}

	// A REAL child process: pid + start ticks are the recorded identity, so the
	// incarnation factors are genuine (a forged pid would make the test's
	// "identity agrees" precondition a claim rather than a measurement).
	cmd := exec.Command("sleep", cwdRowFixtureSleepArg)
	cmd.Dir = t.TempDir()
	cmd.SysProcAttr = &syscall.SysProcAttr{Pdeathsig: syscall.SIGKILL}
	if err := cmd.Start(); err != nil {
		t.Skipf("cannot spawn a probe process: %v", err)
	}
	t.Cleanup(func() {
		_ = cmd.Process.Kill() //stylegate:ignore kills the child process the test spawned and is tearing down
		_ = cmd.Wait()         //stylegate:ignore reaps the child the test spawned; its exit status is not the assertion
	})
	f.pid = cmd.Process.Pid
	start, ok := procStartTicks(f.pid)
	if !ok {
		t.Skip("no procfs on this host — the identity layer degrades to unknown by design")
	}
	f.startTicks = start

	// The fixture procfs tree: stat (start ticks), cmdline (the child's argv)
	// and cwd (a symlink the test repoints).
	f.procRoot = t.TempDir()
	pidDir := filepath.Join(f.procRoot, strconv.Itoa(f.pid))
	if err := os.MkdirAll(pidDir, 0o755); err != nil {
		t.Fatalf("fixture: mkdir %s: %v", pidDir, err)
	}
	after := make([]string, procStatStartTimeField-procStatFieldOffset+1)
	for i := range after {
		after[i] = "0"
	}
	after[0] = "S"
	after[len(after)-1] = strconv.FormatInt(start, 10)
	statLine := strconv.Itoa(f.pid) + " (sleep) " + strings.Join(after, " ") + "\n"
	if err := os.WriteFile(filepath.Join(pidDir, procStatName), []byte(statLine), 0o644); err != nil {
		t.Fatalf("fixture: write stat: %v", err)
	}
	if err := os.WriteFile(filepath.Join(pidDir, procCmdlineName),
		[]byte("sleep\x00"+cwdRowFixtureSleepArg+"\x00"), 0o644); err != nil {
		t.Fatalf("fixture: write cmdline: %v", err)
	}
	f.pointCwdAt(t, f.rowDirA)
	withIdentityProcRoot(t, f.procRoot)

	// Two real ledger rows, forged onto this identity AFTER the real start
	// transition (the capture path records the TEST process, and the subject
	// here is a child that serves rows).
	for _, id := range []string{cwdRowFixtureA, cwdRowFixtureB} {
		procIdentitySeedRow(t, id, hostPid(t, os.Getpid()))
	}
	ctx := context.Background()
	db := openTestTasksDB(t, memoryDBPath())
	defer db.Close()
	for _, id := range []string{cwdRowFixtureA, cwdRowFixtureB} {
		// The CLAIM STAMP is forged onto the same pid as the identity: the
		// holder ladder refuses to judge a pid the stamp does not name
		// (taskHolderVerdictFor's "the record cannot judge this pid" arm), so a
		// fixture that left the stamp on the TEST process would answer `unknown`
		// for a reason that has nothing to do with the cwd — the exact
		// false-negative shape this suite must not have.
		if _, err := db.ExecContext(ctx,
			`UPDATE tasks SET claimed_by = ?, `+tasksProcPidColumnName+` = ?, `+tasksProcStartColumnName+` = ?, `+tasksProcCmdColumnName+` = ? WHERE task_id = ?`,
			hostPid(t, f.pid), f.pid, start, cwdRowFixtureSleeperCmd, id); err != nil {
			t.Fatalf("fixture: forge identity of %s: %v", id, err)
		}
	}
	return f
}

// pointCwdAt repoints the fixture's cwd symlink — the ONE mutation that models
// "the worker moved on to another row".
func (f *cwdRowFixture) pointCwdAt(t *testing.T, dir string) {
	t.Helper()
	link := filepath.Join(f.procRoot, strconv.Itoa(f.pid), procCwdName)
	if err := os.Remove(link); err != nil && !os.IsNotExist(err) {
		t.Fatalf("fixture: clear cwd link: %v", err)
	}
	if err := os.Symlink(dir, link); err != nil {
		t.Fatalf("fixture: point cwd at %s: %v", dir, err)
	}
}

// dropCwd removes the cwd link — the UNREADABLE shape (EPERM, a container, an
// X-forwarded pid namespace, a race with the process exiting all answer this
// way to this process).
func (f *cwdRowFixture) dropCwd(t *testing.T) {
	t.Helper()
	if err := os.Remove(filepath.Join(f.procRoot, strconv.Itoa(f.pid), procCwdName)); err != nil {
		t.Fatalf("fixture: remove cwd link: %v", err)
	}
}

// statusOf is the ONE reader every arm below asserts through — the same
// function `gopi task status` prints and `gopi worker --resume` gates on.
func statusOf(t *testing.T, taskID string) *TaskProcessStatus {
	t.Helper()
	st, err := TaskProcessStatusFor(taskID)
	if err != nil {
		t.Fatalf("status(%s): %v", taskID, err)
	}
	return st
}

// TestCwdRowEvidenceHandedOnPidIsNotRunning is the epic's ACCEPTANCE arm: with
// two ledger rows recording the SAME live pid, row A's verdict stops being
// `running` exactly when that pid's cwd names row B — and the reason NAMES the
// cwd evidence. The positive control is the same row before the handoff: cwd
// naming A keeps A `running`.
func TestCwdRowEvidenceHandedOnPidIsNotRunning(t *testing.T) {
	f := newCwdRowFixture(t)

	// (a) POSITIVE CONTROL — the pid stands in A's own container.
	stA := statusOf(t, cwdRowFixtureA)
	if stA.Verdict != TaskProcessStateRunning {
		t.Fatalf("A before the handoff: verdict = %q (note %q), want %q — a pid standing in its OWN row's directory is running",
			stA.Verdict, stA.Note, TaskProcessStateRunning)
	}
	if !stA.IdentityRecorded || !stA.LiveAlive || stA.LiveStartTicks != stA.RecordedStart {
		t.Fatalf("precondition: the identity must AGREE (recorded=%v alive=%v recStart=%d liveStart=%d) — otherwise the arm below proves nothing about the cwd",
			stA.IdentityRecorded, stA.LiveAlive, stA.RecordedStart, stA.LiveStartTicks)
	}
	if stA.CwdRow != cwdRowFixtureA || !strings.Contains(stA.Note, "names this row") {
		t.Fatalf("A's cwd evidence = row %q note %q, want row %q and the 'names this row' sentence", stA.CwdRow, stA.Note, cwdRowFixtureA)
	}

	// (b) THE HANDOFF — the same pid now stands in B's container.
	f.pointCwdAt(t, f.rowDirB)

	stB := statusOf(t, cwdRowFixtureB)
	if stB.Verdict != TaskProcessStateRunning || stB.CwdRow != cwdRowFixtureB {
		t.Fatalf("B (the row the pid really serves): verdict = %q cwd_row = %q, want %q/%q — the handoff must NOT break the live row",
			stB.Verdict, stB.CwdRow, TaskProcessStateRunning, cwdRowFixtureB)
	}

	stA = statusOf(t, cwdRowFixtureA)
	if stA.Verdict == TaskProcessStateRunning {
		t.Fatalf("A after the handoff: verdict = %q (note %q) — a pid whose cwd names ANOTHER row must not read running",
			stA.Verdict, stA.Note)
	}
	if stA.Verdict != TaskProcessStateRecycled {
		t.Fatalf("A after the handoff: verdict = %q, want %q (the vocabulary's word for 'the pid is alive but no longer this row's runner')",
			stA.Verdict, TaskProcessStateRecycled)
	}
	if !strings.Contains(stA.Note, cwdRowFixtureB) || !strings.Contains(stA.Note, procCwdName) {
		t.Fatalf("A's reason must NAME the cwd evidence and the row it points at; got %q", stA.Note)
	}
	// The recovery path is the whole point: a handed-on row must be resumable.
	if !stA.Resumable {
		t.Fatalf("A after the handoff: resumable = false — the bug being fixed is that the resume path stayed blocked")
	}
}

// TestCwdRowEvidenceUnreadableIsNamedNotSilentlyDropped is the Rule 15 arm: an
// unreadable cwd must NOT silently fall back to the pid-only answer. The verdict
// stays conservative (running — never a fabricated death) and the evidence line
// says the row half is MISSING.
func TestCwdRowEvidenceUnreadableIsNamedNotSilentlyDropped(t *testing.T) {
	f := newCwdRowFixture(t)
	f.dropCwd(t)

	st := statusOf(t, cwdRowFixtureA)
	if st.Verdict != TaskProcessStateRunning {
		t.Fatalf("verdict = %q — an unreadable cwd may only WEAKEN the answer, never manufacture a death/recycle",
			st.Verdict)
	}
	if st.CwdSeen != "" || st.CwdRow != "" {
		t.Fatalf("cwd fields = (%q, %q), want both empty for a failed read", st.CwdSeen, st.CwdRow)
	}
	if !strings.Contains(st.Note, "cwd could NOT be read") || !strings.Contains(st.Note, "MISSING") {
		t.Fatalf("the evidence line must NAME the missing reading and its consequence; got %q", st.Note)
	}
	// The rendered view carries the same fact, with its own word.
	if report := TaskProcessStatusReport(st); !strings.Contains(report, "cwd_row:     NOT READ") {
		t.Fatalf("report has no NOT READ line:\n%s", report)
	}
}

// TestCwdRowRungNeverWeakensANonRunningVerdict keeps the rung's blast radius
// honest (Rule 14: one rung, one transition). A pid-level `dead` or `recycled`
// is already a measurement; the cwd may not rewrite it, and the reason must not
// grow a cwd sentence about a process that is not the recorded one.
func TestCwdRowRungNeverWeakensANonRunningVerdict(t *testing.T) {
	f := newCwdRowFixture(t)
	f.pointCwdAt(t, f.rowDirB) // a contradiction is on offer

	if got := taskHolderCwdRung(f.pid, taskProcIdentity{CwdRow: cwdRowFixtureA},
		taskProcVerdict{State: TaskProcessStateDead, Reason: "pid is gone"}); got.State != TaskProcessStateDead || got.Reason != "pid is gone" {
		t.Fatalf("dead verdict was rewritten to %#v — the rung owns exactly one transition", got)
	}
	if got := taskHolderCwdRung(f.pid, taskProcIdentity{CwdRow: cwdRowFixtureA},
		taskProcVerdict{State: TaskProcessStateUnknown, Reason: "unmeasured"}); got.State != TaskProcessStateUnknown || got.Reason != "unmeasured" {
		t.Fatalf("unknown verdict was rewritten to %#v", got)
	}
}

// TestCwdRowRungComparesOnlyWhenTheRecordNamesARow: a legacy record (no row
// identity) gets the reading REPORTED, never used as a contradiction — there is
// nothing to contradict. This is the arm that keeps the rung from inventing a
// rule for rows that predate it.
func TestCwdRowRungComparesOnlyWhenTheRecordNamesARow(t *testing.T) {
	f := newCwdRowFixture(t)
	f.pointCwdAt(t, f.rowDirB)

	got := taskHolderCwdRung(f.pid, taskProcIdentity{},
		taskProcVerdict{State: TaskProcessStateRunning, Reason: "pid matches the recorded identity"})
	if got.State != TaskProcessStateRunning {
		t.Fatalf("verdict = %q — a record with no row identity must keep running", got.State)
	}
	if !strings.Contains(got.Reason, "not compared") {
		t.Fatalf("reason %q must state that the cwd was NOT compared (Rule 15b)", got.Reason)
	}
}

// TestCwdRowEvidenceNameIsAReadNotAPathPrefix pins the predicate: the ROW is
// carried by the directory's NAME (a minted container name), so a directory
// reached through a DIFFERENT parent — the same clone registered under two
// workspace roots on this host — still resolves to its row. A path-prefix test
// against one configured root would answer "not a row directory" for the other,
// which is the silent fallback this rung must not have.
func TestCwdRowEvidenceNameIsAReadNotAPathPrefix(t *testing.T) {
	for _, dir := range []string{
		"/home/user/workspace/" + cwdRowFixtureA,
		"/home/user/workspaces/" + cwdRowFixtureA,
		"/home/user/.gopi/sessions/" + cwdRowFixtureA,
		"/somewhere/else/" + cwdRowFixtureA + "-c1",
		"/somewhere/else/" + cwdRowFixtureA + "-r2",
	} {
		row, ok := sessionContainerRowOf(dir)
		if !ok {
			t.Fatalf("%s: not recognised as a row directory", dir)
		}
		if want := filepath.Base(filepath.Clean(dir)); row != want {
			t.Fatalf("%s: row = %q, want the directory's own name %q", dir, row, want)
		}
	}
	for _, dir := range []string{
		"/home/user/work/gopi",
		"/home/user/.gopi/sessions",
		"/",
		"/home/user/workspace/not-a-row-name",
		"/home/user/workspace/" + cwdRowFixtureA + "-z1",
	} {
		if row, ok := sessionContainerRowOf(dir); ok {
			t.Fatalf("%s: read as row %q — a non-minted name must answer 'names no row'", dir, row)
		}
	}
}
