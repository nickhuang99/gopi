package main

// worker_status_residue_e2e_test.go — the CLI-level guard for the 2026-09-13
// "why are there so many worker processes?" defect.
//
// THE DEFECT (measured on the live host): `gopi worker --status` listed FOUR
// "gopi worker pid N" lines of which only two were real workers, and then
// dumped 66 state-file paths onto one line. Of the four, two were another
// agent's leaked TEST fixture workers (gone minutes later); the 66 were dead-pid
// RESIDUE — files left in <gopi>/worker/ by SIGKILLed workers and leaked tests.
// The enumeration trusted a state file on its NAME plus liveness alone, so a
// RECYCLED pid (pid_max is 4194304 on that host, the counter wraps) turned an
// unrelated live process into a reported worker — proven by repro, where a
// fabricated file for a live process rendered "gopi worker pid N: idle".
//
// WHAT THESE TESTS PIN, all against the REAL binary and a private GOPI_HOME
// (the property is "does the SHIPPED CLI still over-report", which a unit
// assertion over scan fields cannot answer):
//
//	① a state file for a DEAD pid and one for a RECYCLED pid are both REFUSED —
//	   no "gopi worker pid N" line exists for either, whatever the file says;
//	② they are reported as residue with the exact count and a BOUNDED number of
//	   named samples (the operator reads a count, not 4KB of pid noise);
//	③ the run writes and removes NOTHING (the read-only contract of --status,
//	   which is also why the self-healing prune lives on the worker START-UP
//	   path — a unit test pins that half).

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/nickhuang99/gopi/internal/procname"
	"github.com/nickhuang99/gopi/internal/tools"
)

// wsrStatusEnv is the private profile the child runs against: a temp GOPI_HOME
// with the agent-spawn signature stripped (the operator's shell shape, so a
// refusal could never be confused with the guard's own).
func wsrStatusEnv(t *testing.T, home string) []string {
	t.Helper()
	return stripAgentSignature(guardTestEnv(t, home)(
		"GOPI_SETTINGS="+filepath.Join(home, "nonexistent.json"),
		"GOPI_COMPUTE_NODES=",
		"GOPI_SYNC_PEERS=",
	))
}

// wsrRunStatus drives the shipped `gopi worker --status` and returns its exit
// code plus combined output.
func wsrRunStatus(t *testing.T, env []string, bin string) (int, string) {
	// predicate-excluded: `worker --status` is a read-only control (it answers a
	// question about the pid file and runs no round), and the env vector is the
	// caller's (wsrStatusEnv), which pins a private GOPI_HOME and a nonexistent
	// settings path.
	t.Helper()
	cmd := exec.Command(bin, CliVerbWorker, workerControlStatus)
	cmd.Env = env
	out, err := cmd.CombinedOutput()
	code := 0
	if err != nil {
		var ee *exec.ExitError
		if !asExitError(err, &ee) {
			t.Fatalf("run %s %s: %v\n%s", CliVerbWorker, workerControlStatus, err, out)
		}
		code = ee.ExitCode()
	}
	return code, string(out)
}

func TestWorkerStatusCLIRefusesGhostRecordsAndBoundsTheResidue(t *testing.T) {
	bin := gopiTestBinPath(t)
	// repo-context sandbox: the ledger is pinned by this fixture, but the CHILD
	// would otherwise inherit this process's cwd — the live repository — and its
	// own-worktree-release pass would enumerate (and free) the developer's
	// worktrees. See repo_context_sandbox_test.go.
	SandboxRepoContext(t)
	home := t.TempDir()

	// The state directory the child will read: the private profile's worker dir
	// (the same resolution the production reader uses).
	t.Setenv("GOPI_HOME", home)
	dir, err := tools.ControlDir(workerStateDirName)
	if err != nil {
		t.Fatalf("resolve worker state dir: %v", err)
	}

	// (i) SIGKILL residue: a pid that is long gone.
	dead := 999999
	deadPath := filepath.Join(dir, workerStateFilePrefix+strconv.Itoa(dead))
	if werr := os.WriteFile(deadPath, []byte(`{"pid":`+strconv.Itoa(dead)+`,"state":"idle"}`), 0o644); werr != nil {
		t.Fatalf("write dead-pid record: %v", werr)
	}
	// (ii) the RECYCLED pid: a record whose pid is ALIVE and belongs to another
	// process. This test process is that other process — its comm is the test
	// binary's name, exactly the shape measured for residue file 899174.
	self := os.Getpid()
	selfPath := filepath.Join(dir, workerStateFilePrefix+strconv.Itoa(self))
	if werr := os.WriteFile(selfPath, []byte(`{"pid":`+strconv.Itoa(self)+`,"state":"idle"}`), 0o644); werr != nil {
		t.Fatalf("write recycled-pid record: %v", werr)
	}

	code, out := wsrRunStatus(t, wsrStatusEnv(t, home), bin)
	if code != workerControlExitOK {
		t.Fatalf("`%s %s` exited %d, want 0:\n%s", CliVerbWorker, workerControlStatus, code, out)
	}

	// ① neither ghost is a WORKER: the worker-line shape is "<prog> pid N:" and
	// neither pid may appear in it.
	for _, pid := range []int{dead, self} {
		ghost := fmt.Sprintf("%s pid %d:", workerProgTopLevel, pid)
		if strings.Contains(out, ghost) {
			t.Fatalf("a state file for pid %d was rendered as a worker (%q):\n%s", pid, ghost, out)
		}
	}
	// ② both are counted, and the list stays bounded far below the 66-entry
	// incident dump.
	if !strings.Contains(out, fmt.Sprintf(workerStatusResidueFmt, workerProgTopLevel, 2)) {
		t.Fatalf("the residue count must be exact and explicit:\n%s", out)
	}
	if got := residueRowCount(strings.Split(out, "\n")); got > workerStatusResidueSample {
		t.Fatalf("the block named %d residue rows; the bound is %d:\n%s", got, workerStatusResidueSample, out)
	}
	// ②b the two questions the old line never answered: WHAT the files are, and
	// the ONE command that clears them. A count with no next step is the dead end
	// the operator complained about.
	if !strings.Contains(out, workerStatusResidueMeaningA) {
		t.Fatalf("the block must say what the residue is:\n%s", out)
	}
	if !strings.Contains(out, fmt.Sprintf(workerStatusRemedyCommandFmt, dir, workerStatusStateFileGlob)) {
		t.Fatalf("the block must name the command that clears them:\n%s", out)
	}
	// ③ read-only: the run created nothing and removed nothing — the residue is
	// still on disk, which is what makes the START-UP prune the only place it is
	// ever cleaned up.
	for _, p := range []string{deadPath, selfPath} {
		if _, serr := os.Stat(p); serr != nil {
			t.Fatalf("`--status` touched %s — the status face must be side-effect free: %v", p, serr)
		}
	}
}

// TestWorkerStatusCLIVerboseListsEveryResiduePath pins the escape hatch: the
// default block samples the residue, and --status-verbose is the SAME read at a
// longer altitude. Both must be routed (the verbose spelling is not an ACTION,
// so it is not a conflict flag either) and the verbose form must name every path
// the count counted.
func TestWorkerStatusCLIVerboseListsEveryResiduePath(t *testing.T) {
	bin := gopiTestBinPath(t)
	home := t.TempDir()
	t.Setenv("GOPI_HOME", home)
	dir, err := tools.ControlDir(workerStateDirName)
	if err != nil {
		t.Fatalf("resolve worker state dir: %v", err)
	}
	const dead = 999998
	path := filepath.Join(dir, workerStateFilePrefix+strconv.Itoa(dead))
	if werr := os.WriteFile(path, []byte(`{"pid":`+strconv.Itoa(dead)+`,"state":"idle"}`), 0o644); werr != nil {
		t.Fatalf("write dead-pid record: %v", werr)
	}
	// The argv is the BARE verbose flag — the exact word the default block
	// prints ("rerun with --status-verbose"), with no `--status` beside it. The
	// first shipped shape routed only `--status`, so this printed remedy exited
	// workerUnexpectedArgExitCode from the loop scanner: the report's advice was
	// the one command the binary refused.
	cmd := exec.Command(bin, CliVerbWorker, workerStatusResidueVerboseFlag)
	cmd.Env = wsrStatusEnv(t, home)
	out, rerr := cmd.CombinedOutput()
	if rerr != nil {
		t.Fatalf("`%s %s %s` failed: %v\n%s", CliVerbWorker, workerControlStatus, workerStatusResidueVerboseFlag, rerr, out)
	}
	if !strings.Contains(string(out), path) {
		t.Fatalf("verbose must name the residue file's full path %s:\n%s", path, out)
	}
	if _, serr := os.Stat(path); serr != nil {
		t.Fatalf("verbose status touched %s — the read-only contract covers both spellings: %v", path, serr)
	}
}

// TestWorkerStatusCLIRemovesNothingAndReportsNoWorkersWhenClean is the ZERO
// case at CLI level: an empty profile says so (an empty list must never be
// indistinguishable from a broken scan) and exits 0.
func TestWorkerStatusCLIRemovesNothingAndReportsNoWorkersWhenClean(t *testing.T) {
	bin := gopiTestBinPath(t)
	// repo-context sandbox: the ledger is pinned by this fixture, but the CHILD
	// would otherwise inherit this process's cwd — the live repository — and its
	// own-worktree-release pass would enumerate (and free) the developer's
	// worktrees. See repo_context_sandbox_test.go.
	SandboxRepoContext(t)
	home := t.TempDir()
	t.Setenv("GOPI_HOME", home)
	// The argv FALLBACK half of the enumeration reads the real process table, so
	// a clean profile on a host that genuinely runs workers still lists them
	// (that is the honest answer, and why this test drives the seam instead:
	// an empty procfs root means "no worker process on this host").
	t.Setenv(procname.EnvProcRoot, t.TempDir())
	if _, err := tools.ControlDir(workerStateDirName); err != nil {
		t.Fatalf("resolve worker state dir: %v", err)
	}

	code, out := wsrRunStatus(t, wsrStatusEnv(t, home), bin)
	if code != workerControlExitOK {
		t.Fatalf("clean `%s %s` exited %d, want 0:\n%s", CliVerbWorker, workerControlStatus, code, out)
	}
	if !strings.Contains(out, workerStatusNoWorkers) {
		t.Fatalf("a clean profile must say %q, got:\n%s", workerStatusNoWorkers, out)
	}
	if strings.Contains(out, workerStatusResidueFmt[:strings.Index(workerStatusResidueFmt, "(")]) {
		t.Fatalf("a clean profile produced a residue block:\n%s", out)
	}
	// The empty case must also SAY what it means (the operator asked "why no
	// worker?") and how one appears — the two halves the bare line left open.
	for _, want := range []string{"why:", "lock:", "start one:"} {
		if !strings.Contains(out, want) {
			t.Fatalf("the empty case must answer %q:\n%s", want, out)
		}
	}
}

// TestWorkerStatusCLIRefusesTheVerboseAliasBesideAnAct is the CLI-level half of
// the read+act rule for the read-only ALIAS. The rule is what keeps a monitor
// honest: a read beside an act must never exit 0 (the F1 defect), and the alias
// is a read — so an unclassified alias would drop the act and print a success
// code. Measured 2026-09-14 on the shipped binary: `gopi worker --status-verbose
// --stop` printed the status report and exited 0, silently dropping the --stop.
func TestWorkerStatusCLIRefusesTheVerboseAliasBesideAnAct(t *testing.T) {
	bin := gopiTestBinPath(t)
	// repo-context sandbox: the ledger is pinned by this fixture, but the CHILD
	// would otherwise inherit this process's cwd — the live repository — and its
	// own-worktree-release pass would enumerate (and free) the developer's
	// worktrees. See repo_context_sandbox_test.go.
	SandboxRepoContext(t)
	home := t.TempDir()
	cmd := exec.Command(bin, CliVerbWorker, workerStatusResidueVerboseFlag, workerControlStop)
	cmd.Env = wsrStatusEnv(t, home)
	out, rerr := cmd.CombinedOutput()
	if rerr == nil {
		t.Fatalf("`%s %s %s` exited 0 — a read beside an act is ambiguous, never a silent no-op:\n%s",
			CliVerbWorker, workerStatusResidueVerboseFlag, workerControlStop, out)
	}
	if strings.Contains(string(out), "no workers running") {
		t.Fatalf("the status report ran despite the act beside it:\n%s", out)
	}
	for _, want := range []string{workerControlStop, workerStatusResidueVerboseFlag} {
		if !strings.Contains(string(out), want) {
			t.Fatalf("the refusal must name %s:\n%s", want, out)
		}
	}
}
