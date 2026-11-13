package main

// task_ground_e2e_test.go — ACCEPTANCE ① and ③ ON THE REAL SURFACE (Rule 31):
// a `gopi task run` over a repo-aware row whose ground does not exist yet must
// start its executor child INSIDE a linked worktree of that repo, and the row
// must name that ground WHILE THE RUN IS HAPPENING.
//
// WHY THIS TEST DRIVES THE REAL BINARY AND MEASURES /proc. The defect it closes
// is invisible to every in-process assertion: the pre-fix spawn was CORRECTLY
// configured to start in the wrong place (childDir resolved to the main
// checkout, which is a legal directory), so nothing complained and no unit test
// could see it. The only instrument that separates the two shapes is the
// child's OWN cwd, read from the kernel while it is alive — /proc/<pid>/cwd.
//
// THE RUN IS HELD IN FLIGHT DELIBERATELY. The mock LLM's Hold response keeps
// the child blocked inside its first model round trip, so the measurement and
// the row query land mid-run (status in_progress) rather than after the
// writeback — which is what makes this evidence for "the ground is decided
// BEFORE the spawn" instead of a reading of some later state.
//
// WHICH PROCESS IS MEASURED, and why the pid is not guessed: the child is found
// by its environment — the spawn seam exports GOPI_TASK_WRAPPER_ID=<row id> to
// every executor child it starts (cmd.Env, before the spawn), and nothing else
// carries that pair. A pid found any other way (a `pgrep -f` pattern, ancestry)
// would be guessing — Rule 24, and the pattern self-match trap.

import (
	"database/sql"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	_ "modernc.org/sqlite"

	"github.com/nick/gopi/internal/config"
	"github.com/nick/gopi/internal/heavytest"
	"github.com/nick/gopi/internal/testutil"
	"github.com/nick/gopi/internal/tools"
	"github.com/nick/gopi/internal/worktreeroots"
)

const (
	// groundE2EProbeWait bounds the wait for the child to reach its held model
	// round (and for its pid to appear) — a starting gopi does real work before
	// its first model call.
	groundE2EProbeWait = 60 * time.Second
	// groundE2EProbePeriod is the poll interval of that bounded wait.
	groundE2EProbePeriod = 200 * time.Millisecond
	// groundE2EWrapperWait bounds the wait for the wrapper to end after the
	// held round is released.
	groundE2EWrapperWait = 60 * time.Second
)

// TestTaskRunSpawnsTheChildInsideItsOwnLinkedWorktree is the epic's acceptance
// walk: `task run` over a fresh repo-aware row.
func TestTaskRunSpawnsTheChildInsideItsOwnLinkedWorktree(t *testing.T) {
	heavytest.Require(t, heavytest.CriterionDeviceExec)
	bin := testutil.GopiBin(t)
	if _, err := os.Stat(bin); err != nil {
		t.Skipf("gopi binary unavailable: %v", err)
	}
	assertGopiBinIsFresh(t, bin)

	gopiHome := t.TempDir()
	dbPath := filepath.Join(t.TempDir(), "memory.db")
	root := t.TempDir()
	t.Setenv(worktreeroots.WorktreeRootEnvName, root)

	repo := executorCwdProbeRepo(t, "ground-e2e-repo")
	const taskID = "f1c751ee-2e99-4d2f-9c0d-5e7e60be4a92"

	// The held response keeps the child inside its first model round; the
	// script's second entry answers the confirmation round after release.
	mock := testutil.NewMockLLM(t,
		testutil.Resp{Hold: true},
		testutil.Resp{Text: "ground e2e done"},
	)
	t.Cleanup(mock.Close)

	env := executorProbeEnv(t, gopiHome, dbPath, mock.URL())
	env = append(env, worktreeroots.WorktreeRootEnvName+"="+root)
	if _, stderr, code := runGopiMapped(t, []string{"task", "submit",
		"ground e2e", "--payload", "say something", "--task-id", taskID, "--repo", repo},
		"", env, repo); code != 0 {
		t.Fatalf("submit: code=%d stderr=%s", code, stderr)
	}

	// BEFORE the run: the row names a repo and NO ground — the defect's input
	// shape, asserted so a green run cannot be explained by an earlier stamp.
	preRepo, preBranch, preWorktree := groundRowDeliverable(t, dbPath, taskID)
	if preRepo == "" {
		t.Fatalf("the row must carry the submit-time deliverable.repo")
	}
	if preWorktree != "" || preBranch != "" {
		t.Fatalf("fixture precondition: the row must start with NO ground; got branch=%q worktree=%q", preBranch, preWorktree)
	}

	// The runner is `gopi worker <id>` (the retired `task run` face is gone from
	// master), launched from the repo so the pre-epic resolution would have
	// fallen back to it — the defect's exact input.
	wrapper := exec.Command(bin, "worker", taskID)
	wrapper.Dir = repo
	wrapper.Env = env
	var wout, werr strings.Builder
	wrapper.Stdout = &wout
	wrapper.Stderr = &werr
	if err := wrapper.Start(); err != nil {
		t.Fatalf("start `gopi worker %s`: %v", taskID, err)
	}
	wrapperPid := wrapper.Process.Pid
	t.Cleanup(func() {
		if wrapper.ProcessState == nil {
			_ = wrapper.Process.Kill()    //stylegate:ignore kills the fixture process the test spawned
			_, _ = wrapper.Process.Wait() //stylegate:ignore reaps the fixture process the test spawned
		}
	})

	// The child's pid: the process carrying GOPI_TASK_WRAPPER_ID=<row>, which
	// only an executor child of THIS row can have.
	childPid, cwd := groundWaitForChildCwd(t, taskID, wrapperPid)
	t.Cleanup(func() { groundKill(t, childPid) })

	if SameDirString(cwd, repo) || strings.HasPrefix(cwd, repo+string(filepath.Separator)) {
		t.Fatalf("the executor child stands in the repo's MAIN checkout %s (measured /proc/%d/cwd = %s) — this is the epic's defect: a repo-aware row must be spawned in its own linked worktree", repo, childPid, cwd)
	}
	if !strings.HasPrefix(cwd, root+string(filepath.Separator)) {
		t.Fatalf("/proc/%d/cwd = %s — the ground must be under the repo's worktree root %s", childPid, cwd, root)
	}
	// A LINKED worktree: .git is a FILE (a directory would be a main checkout).
	fi, err := os.Stat(filepath.Join(cwd, ".git"))
	if err != nil {
		t.Fatalf("stat %s/.git: %v", cwd, err)
	}
	if fi.IsDir() {
		t.Fatalf("%s is a MAIN checkout (.git is a directory), not a linked worktree", cwd)
	}
	if !groundRegisteredIn(t, repo, cwd) {
		t.Fatalf("git must register %s as a worktree of %s; listing:\n%s", cwd, repo, groundWorktreeListOf(t, repo))
	}

	// ACCEPTANCE ③ — the row names the ground WHILE THE RUN IS IN FLIGHT. The
	// status assertion is what makes this mid-run rather than post-hoc.
	gotRepo, gotBranch, gotWorktree := groundRowDeliverable(t, dbPath, taskID)
	if gotWorktree == "" || gotBranch == "" {
		t.Fatalf("the row must name its ground during the run; got repo=%q branch=%q worktree=%q", gotRepo, gotBranch, gotWorktree)
	}
	if !SameDirString(gotWorktree, cwd) {
		t.Fatalf("the row's deliverable.worktree (%s) must be the directory the child actually stands in (%s)", gotWorktree, cwd)
	}
	if status := groundRowStatus(t, dbPath, taskID); status != "in_progress" {
		t.Errorf("the readings above must be taken from an IN-FLIGHT run; row status is %q", status)
	}

	// The seam's own record, in the round's log — the operator-facing half.
	roundLog := filepath.Join(gopiHome, ".gopi", config.SessionsDirName, taskID, tools.WorkerLogFileName)
	if data, err := os.ReadFile(roundLog); err == nil {
		if !strings.Contains(string(data), cwd) {
			t.Errorf("the spawn seam must RECORD the ground it chose (%s) in the round's log (Rule 15); got:\n%s", cwd, data)
		}
		t.Logf("ground record:\n%s", groundGroundLines(string(data)))
	} else {
		t.Errorf("read the round log %s: %v", roundLog, err)
	}

	t.Logf("measured: pid %d cwd %s (row %s branch %s)", childPid, cwd, taskID, gotBranch)

	// TEARDOWN (not an assertion): the readings above are already taken, so the
	// held child and its wrapper are stopped EXPLICITLY and by pid (never by
	// pattern — Rule 24). A wrapper that needs killing after its child is gone
	// is a slow exit, not a failed walk, so this never fails the test.
	mock.ReleaseHold(1)
	groundStopRun(t, wrapper, childPid, &wout, &werr)
}

// groundGroundLines returns the seam's own ground/cwd lines from a round log.
func groundGroundLines(logText string) string {
	var out []string
	for _, line := range strings.Split(logText, "\n") {
		if strings.Contains(line, "child ground →") || strings.Contains(line, "executor cwd →") {
			out = append(out, line)
		}
	}
	return strings.Join(out, "\n")
}

// groundEmptyRowIDRefusal is the ONE predicate behind the empty-row-id guard,
// split out so a PURE test can drive it (Rule 31 does not apply to a predicate
// with no side effect; the walk's own call site is what it protects).
//
// WHY THIS EXISTS: with taskID == "" the candidate filter in groundChildPid
// cannot exclude anything — every process without GOPI_TASK_WRAPPER_ID in its
// environ reads back "" and therefore MATCHES. Measured on this host while
// fixing task_briefing_delivery_e2e_test.go's missing table entry: 43,825 pids
// were enumerated and logged one "skipping pid …" line each, and the walk still
// burned its full 60s budget and then reported "no executor child of  appeared",
// a message that names no cause. A caller that passed "" had a table defect; the
// verdict it got was "the run never spawned a child".
func groundEmptyRowIDRefusal(taskID string) string {
	if strings.TrimSpace(taskID) != "" {
		return ""
	}
	return "groundWaitForChildCwd was called with an EMPTY task id — every process matches " +
		tools.TaskWrapperEnv + "= then, so this wait cannot measure anything and would only burn its " +
		groundE2EProbeWait.String() + " budget; the caller must pass the row id it submitted"
}

// groundWaitForChildCwd polls /proc for the row's executor child and returns its
// pid and measured cwd. Bounded and REPORTED: a child that never appears is a
// failure of this walk, not a silent skip.
func groundWaitForChildCwd(t *testing.T, taskID string, wrapperPid int) (int, string) {
	t.Helper()
	// AN EMPTY ROW ID IS NOT A SLOW RUN, IT IS A WILDCARD (Rule 15). With
	// taskID == "" the candidate filter below cannot exclude anything: every
	// process that lacks GOPI_TASK_WRAPPER_ID in its environment reads back "" and
	// therefore MATCHES. Measured on this host while fixing
	// task_briefing_delivery_e2e_test.go's missing table entry: 43,825 pids were
	// enumerated and logged one "skipping pid …" line each, and the walk still
	// timed out at its full 60s budget with a message ("no executor child of
	//  appeared") that named no cause. The cause is knowable here at zero cost.
	if refusal := groundEmptyRowIDRefusal(taskID); refusal != "" {
		t.Fatalf("%s", refusal)
	}
	deadline := time.Now().Add(groundE2EProbeWait)
	for {
		if pid := groundChildPid(t, taskID, wrapperPid); pid > 0 {
			if cwd, err := testutil.Cwd(pid); err == nil && cwd != "" {
				return pid, cwd
			}
		}
		if time.Now().After(deadline) {
			t.Fatalf("no executor child of %s carrying %s appeared within %s — the run never spawned a measurable child", taskID, tools.TaskWrapperEnv, groundE2EProbeWait)
		}
		time.Sleep(groundE2EProbePeriod) //stylegate:allow-sleep bounded wait for a just-spawned child's pid to appear (no event fires when it lands)
	}
}

// groundChildPid finds the live process whose environment carries this row's
// wrapper id (the executor child), or 0.
//
// THE ENVIRONMENT IS NOT OWNERSHIP (measured 2026-10-27/28). GOPI_TASK_WRAPPER_ID
// is INHERITED, so every process forked from the run that exported it carries the
// same pair — including a `gopi worker` in an unrelated session directory that
// re-executes the same row, and any helper a sibling round spawned. The pre-fix
// body accepted such a stranger as "the executor child", and the walk then SIGNALS
// what it found (groundStopRun → groundKill → SIGINT): `signal-provenance.jsonl`
// records five such deliveries between 23:29 and 00:12, sender_comm `agent.test`,
// victims `gopi`/`gopi worker` in OTHER sessions' directories (01a17dee, 01a17dfa,
// 01a17dac, 01a17db3 — none of them this walk's own row). Each one died of it.
//
// So the env match is a CANDIDATE filter, and the kernel's own parent column is
// the ownership test: the pid must descend from THIS process (or from the wrapper
// this process started). A target that is not ours is skipped and REPORTED — the
// walk is then the "no executor child appeared" timeout, which is the honest
// verdict for a child that cannot be told apart from a stranger.
func groundChildPid(t *testing.T, taskID string, wrapperPid int) int {
	t.Helper()
	pids, err := testutil.ListPIDs()
	if err != nil {
		return 0
	}
	self := os.Getpid()
	for _, pid := range pids {
		if pid == wrapperPid || pid == self {
			continue
		}
		got, err := testutil.EnvValue(pid, tools.TaskWrapperEnv)
		if err != nil || strings.TrimSpace(got) != taskID {
			continue
		}
		// OWNERSHIP, from the parent column — not from the env it inherited.
		mine, why := testutil.ProcDescendsFrom(pid, self)
		if !mine && wrapperPid > 0 {
			mine, why = testutil.ProcDescendsFrom(pid, wrapperPid)
		}
		if !mine {
			t.Logf("skipping pid %d: it carries %s=%s but is not this walk's child (%s) — signalling it would reach a stranger's process (Rule 42)",
				pid, tools.TaskWrapperEnv, taskID, why)
			continue
		}
		return pid
	}
	return 0
}

// groundKillSignal is the ONE place groundKill delivers a signal, and it takes
// the target's IDENTITY (pid + starttime) rather than the pid alone.
//
// WHY THE IDENTITY, AND WHY IT IS NOT OPTIONAL (measured 2026-10-17, five
// times on this host): a pid is a NUMBER THE KERNEL RECYCLES, so "signal pid
// 12345" is a statement about a number, not about a process. groundKill holds
// its pid across a t.Cleanup — the target has usually finished the walk long
// before the teardown runs — and `os.FindProcess` on Unix ALWAYS succeeds (it
// is a local handle, not a lookup), so the pre-fix body's very first act,
// `proc.Signal(os.Interrupt)`, delivered SIGINT to WHOEVER now owned that
// number. On this host that was `systemd --user`, and SIGINT to the user
// manager STOPS THE WHOLE USER SESSION: `journalctl -b | grep "Received SIG"`:
//
//	2026-10-17T01:14:12 systemd[3175]:    Received SIGINT from PID 3199746 (agent.test)
//	2026-10-17T01:16:09 systemd[3322902]: Received SIGINT from PID 3324839 (agent.test)
//	2026-10-17T01:22:15 systemd[3325473]: Received SIGINT from PID 3329111 (agent.test)
//	2026-10-17T01:52:31 systemd[3329727]: Received SIGINT from PID 3445033 (agent.test)
//	2026-10-17T07:28:35 systemd[3539644]: Received SIGINT from PID 4144045 (agent.test)   ← the operator's GNOME session died here
//
// A VAR so the repro test can drive the PRE-FIX shape (pid alone) and the
// shipped shape against the same live process, which is what makes the guard's
// red→green transition an observation rather than a claim.
var groundKillSignal = func(id testutil.ProcIdentity, sig syscall.Signal) error {
	return testutil.KillIfIdentical(id, sig)
}

// groundKill stops one pid EXPLICITLY (never by pattern — Rule 24), and only
// while that pid is still the process the caller captured: the identity is read
// FIRST, and both the graceful signal and the escalation are delivered through
// groundKillSignal, which refuses a pid whose incarnation has changed.
//
// Before the signal, a pid that is already gone is a RETURN, never a signal:
// ESRCH is the outcome this helper wants, and signalling a number to learn that
// is how a teardown reaches a stranger.
func groundKill(t *testing.T, pid int) {
	t.Helper()
	if pid <= 0 {
		return
	}
	id, err := testutil.ReadProcIdentity(pid)
	if err != nil || id.Pid <= 0 {
		return // already gone: nothing to signal, and nothing to learn from one
	}
	_ = groundKillSignal(id, syscall.SIGINT) //stylegate:ignore signals the fixture process the test spawned, and only while it is the recorded incarnation
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if alive, _ := id.Alive(); !alive {
			return
		}
		time.Sleep(100 * time.Millisecond) //stylegate:allow-sleep bounded wait for an explicitly-signalled child to exit before escalation
	}
	_ = groundKillSignal(id, syscall.SIGKILL) //stylegate:ignore escalation against the SAME recorded incarnation; ESRCH means it exited while we waited, which is the goal state
}

// groundStopRun tears the walk down: the child first (it is what the hold was
// keeping alive), then the wrapper, each bounded and escalated to SIGKILL. It
// reports what happened and never fails the walk — the assertions were taken
// before it ran.
func groundStopRun(t *testing.T, wrapper *exec.Cmd, childPid int, wout, werr *strings.Builder) {
	t.Helper()
	groundKill(t, childPid)
	done := make(chan error, 1)
	go func() { done <- wrapper.Wait() }()
	select {
	case err := <-done:
		t.Logf("wrapper exited: %v", err)
	case <-time.After(groundE2EWrapperWait):
		_ = wrapper.Process.Kill() //stylegate:ignore kills the fixture process the test spawned
		<-done
		t.Logf("wrapper did not exit within %s after its child was stopped — killed (not an assertion: the readings were taken before teardown)", groundE2EWrapperWait)
	}
	t.Logf("wrapper stdout:\n%s\nwrapper stderr:\n%s", wout.String(), werr.String())
}

// groundRowDeliverable reads a row's deliverable axes straight from the ledger —
// the recorded truth, not a rendering of it.
func groundRowDeliverable(t *testing.T, dbPath, taskID string) (repo, branch, worktree string) {
	t.Helper()
	raw := groundRowBlob(t, dbPath, taskID)
	if strings.TrimSpace(raw) == "" {
		return "", "", ""
	}
	var d tools.TaskDeliverableSpec
	if err := json.Unmarshal([]byte(raw), &d); err != nil {
		t.Fatalf("the row's deliverable %q is not JSON: %v", raw, err)
	}
	return d.Repo, d.Branch, d.Worktree
}

// groundRowStatus reads a row's status from the ledger.
func groundRowStatus(t *testing.T, dbPath, taskID string) string {
	t.Helper()
	raw := groundRowField(t, dbPath, taskID, "status")
	return raw
}

// groundRowBlob reads the deliverable column of one row.
func groundRowBlob(t *testing.T, dbPath, taskID string) string {
	t.Helper()
	return groundRowField(t, dbPath, taskID, "deliverable")
}

// groundRowField is the ONE ledger read this file does (Rule 14): open the
// ledger read-only, select one column of one row.
func groundRowField(t *testing.T, dbPath, taskID, column string) string {
	t.Helper()
	db, err := sql.Open("sqlite", tools.LedgerReadOnlyDSN(dbPath))
	if err != nil {
		t.Fatalf("open ledger %s: %v", dbPath, err)
	}
	defer db.Close()
	var out sql.NullString
	if err := db.QueryRow("SELECT "+column+" FROM tasks WHERE task_id = ?", taskID).Scan(&out); err != nil {
		t.Fatalf("read %s of %s: %v", column, taskID, err)
	}
	return strings.TrimSpace(out.String)
}

// groundRegisteredIn reports whether git registers dir as a worktree of repo.
func groundRegisteredIn(t *testing.T, repo, dir string) bool {
	t.Helper()
	want := filepath.Clean(dir)
	for _, line := range strings.Split(groundWorktreeListOf(t, repo), "\n") {
		p, ok := strings.CutPrefix(strings.TrimSpace(line), "worktree ")
		if ok && filepath.Clean(p) == want {
			return true
		}
	}
	return false
}

// groundWorktreeListOf renders `git worktree list --porcelain` for repo.
func groundWorktreeListOf(t *testing.T, repo string) string {
	t.Helper()
	cmd := exec.Command("git", "worktree", "list", "--porcelain")
	cmd.Dir = repo
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git worktree list in %s: %v\n%s", repo, err, out)
	}
	return string(out)
}

// SameDirString is a small local equality on CLEANED paths (the production
// predicate lives in internal/tools and is not reachable for a plain string
// pair from this package without a ledger).
func SameDirString(a, b string) bool {
	return filepath.Clean(strings.TrimSpace(a)) == filepath.Clean(strings.TrimSpace(b))
}

// TestWorkerRefusesWhenTheGroundCannotBeMade is ACCEPTANCE ③'s refusal half on
// the REAL surface (Rule 31): a repo-aware row whose repository cannot answer
// any more must NOT be executed from the main checkout — the run is refused
// BEFORE the spawn, the reason is on stderr, and the row carries it.
//
// The repo is real at submit time (submit verifies it) and is then destroyed —
// exactly the shape the ground cannot be created from while the row still names
// it.
func TestWorkerRefusesWhenTheGroundCannotBeMade(t *testing.T) {
	bin := testutil.GopiBin(t)
	if _, err := os.Stat(bin); err != nil {
		t.Skipf("gopi binary unavailable: %v", err)
	}
	assertGopiBinIsFresh(t, bin)

	gopiHome := t.TempDir()
	dbPath := filepath.Join(t.TempDir(), "memory.db")
	root := t.TempDir()
	t.Setenv(worktreeroots.WorktreeRootEnvName, root)
	repo := executorCwdProbeRepo(t, "ground-refuse-repo")
	// A full uuid: the submit verb refuses a row id no ground name can be
	// derived from (TaskSubmitOptions.RefuseUngroundableID, armed on the CLI
	// face by epic 01a12b86), and this fixture submits through the REAL binary
	// precisely so the ground refusal it pins is reached at GROUND time.
	const taskID = "e5c77cc8-75ba-5a97-8c80-c36fe91856cc"

	// No model round is expected: the refusal precedes the spawn, so a mock URL
	// that would answer anything is enough (and no request may arrive).
	mock := testutil.NewMockLLM(t, testutil.Resp{Text: "must not be reached"})
	t.Cleanup(mock.Close)

	env := executorProbeEnv(t, gopiHome, dbPath, mock.URL())
	env = append(env, worktreeroots.WorktreeRootEnvName+"="+root)
	if _, stderr, code := runGopiMapped(t, []string{"task", "submit",
		"ground refuse", "--payload", "say something", "--task-id", taskID, "--repo", repo},
		"", env, repo); code != 0 {
		t.Fatalf("submit: code=%d stderr=%s", code, stderr)
	}
	// The ground cannot be made: the repository is gone.
	if err := os.RemoveAll(filepath.Join(repo, ".git")); err != nil {
		t.Fatalf("remove .git: %v", err)
	}

	_, stderrOut, code := runGopiMapped(t, []string{"worker", taskID}, "", env, repo)
	if code == 0 {
		t.Fatalf("the run must FAIL when the ground cannot be made; got exit 0\nstderr:\n%s", stderrOut)
	}
	for _, want := range []string{taskID, repo, "refusing to start"} {
		if !strings.Contains(stderrOut, want) {
			t.Errorf("the refusal must name %q on the operator's console; stderr:\n%s", want, stderrOut)
		}
	}
	// The refusal is not silent either: the row carries the reason (the ledger's
	// `reason` column is what TaskUpdateReason writes).
	reason := groundRowField(t, dbPath, taskID, "reason")
	if !strings.Contains(reason, "refusing to start") {
		t.Errorf("the row must record the refusal reason so the next reader sees why it did not run; reason=%q", reason)
	}
	if status := groundRowStatus(t, dbPath, taskID); status == "in_progress" {
		t.Errorf("a refused run must not leave the row in_progress")
	}
	if mock.RequestCount() != 0 {
		t.Errorf("the refusal must precede the spawn: %d model round(s) were made", mock.RequestCount())
	}
	if _, _, worktree := groundRowDeliverable(t, dbPath, taskID); worktree != "" {
		t.Errorf("a refused run must not stamp a ground on the row; worktree=%q", worktree)
	}
}

// TestGroundEmptyRowIDRefusalIsNotVacuous is the PURE gate over the empty-row-id
// guard: it pins the refusal on the exact input that produced the measured 60s
// silent timeout, and pins SILENCE on a real row id so the guard cannot start
// refusing ordinary walks.
//
// RED PROOF: delete the empty-string arm from groundEmptyRowIDRefusal (or
// weaken it to a no-op) and the first case below fails; tighten the predicate to
// refuse everything and the second case fails.
func TestGroundEmptyRowIDRefusalIsNotVacuous(t *testing.T) {
	for _, empty := range []string{"", "   ", "\t\n"} {
		if got := groundEmptyRowIDRefusal(empty); got == "" {
			t.Errorf("an empty row id %q must be REFUSED before the probe loop: with it, every process lacking %s in its environ reads back \"\" and matches, so the wait cannot measure anything (measured: 43,825 pids enumerated, the full %s budget burned, and a failure message that names no cause)",
				empty, tools.TaskWrapperEnv, groundE2EProbeWait)
		}
	}
	// THE POSITIVE CONTROL: a real row id must NOT be refused, or every walk in
	// this package dies at its first call.
	for _, ok := range []string{"9f6b1c2d-3e4a-4b5c-8d7e-1f2a3b4c5d6e", "t-1788277288"} {
		if got := groundEmptyRowIDRefusal(ok); got != "" {
			t.Errorf("row id %q must NOT be refused (the guard must stay silent on ordinary walks); got %q", ok, got)
		}
	}
}
