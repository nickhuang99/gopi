package main

// worker_status_profile_attribution_test.go — the guard for the 2026-09-13
// "why three workers when I ran `gopi worker --daemon` once?" defect.
//
// THE DEFECT (measured, attachment1 of the epic): the operator's own
// `gopi worker --status` listed THREE workers while exactly ONE was theirs. The
// extra two were real gopi workers that had been started by ANOTHER agent's test
// run with HOME/GOPI_HOME pointing into a deleted sandbox (`/dev/shm/…`), so they
// drained the SANDBOX's empty queue forever — "idle" beside a non-empty pending
// queue, which is the half of the report that made no sense to the operator.
//
// THE GAP: the enumeration's process-table half is MACHINE-WIDE by construction
// (comm == gopi AND argv[1] == the worker verb AND detached — the launcher's own
// --count predicate), and no test ever asked it the SECOND question: whose
// worker is this? The answer is in the process's OWN environment (GOPI_HOME,
// else HOME), and a process whose profile is not the caller's is not the
// caller's fleet.
//
// WHAT THESE TESTS PIN (all against the REAL enumeration / the SHIPPED binary,
// because the property is "does the report still mis-count", which no pure
// predicate assertion answers):
//
//	① attribution reads the process's OWN environment, with GOPI_HOME winning
//	   over HOME (the same rule config.GopiDir applies to this process);
//	② a worker-shaped process of ANOTHER profile is NOT in the worker list and
//	   IS named in the foreign block, with the profile it belongs to;
//	③ the control half: the SAME shape IS counted when the profile is mine —
//	   the seam is attribution, not a blanket suppression;
//	④ the degradation: an UNATTRIBUTABLE worker (unreadable /run environ, or an
//	   unresolvable own profile) is COUNTED, never dropped — an invisible
//	   under-report is worse than a visible miscount.

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"

	"github.com/nickhuang99/gopi/internal/config"
	"github.com/nickhuang99/gopi/internal/procname"
)

// --- ① the attribution rule -------------------------------------------------

func TestWorkerStatusProfileRuleReadsTheProcessesOwnEnvironment(t *testing.T) {
	own := "/own"
	foreign := "/foreign"
	cases := []struct {
		name string
		env  []string
		want string
	}{
		{"GOPI_HOME wins over HOME",
			[]string{procname.EnvHome + "=" + foreign, config.GopiDirEnv + "=" + own},
			filepath.Join(own, config.ProfileDirName)},
		{"HOME is the fallback",
			[]string{procname.EnvHome + "=" + own},
			filepath.Join(own, config.ProfileDirName)},
		{"an EMPTY GOPI_HOME is unset, not a profile",
			[]string{procname.EnvHome + "=" + own, config.GopiDirEnv + "="},
			filepath.Join(own, config.ProfileDirName)},
		{"a whitespace-only GOPI_HOME is unset too",
			[]string{procname.EnvHome + "=" + own, config.GopiDirEnv + "=  "},
			filepath.Join(own, config.ProfileDirName)},
		{"neither variable: UNATTRIBUTABLE, never a guess",
			[]string{"PATH=/usr/bin"},
			""},
		{"surrounding space is trimmed, not part of the path",
			[]string{config.GopiDirEnv + "= " + own + " "},
			filepath.Join(own, config.ProfileDirName)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := workerProfileFromEnv(tc.env); got != tc.want {
				t.Fatalf("workerProfileFromEnv(%q) = %q, want %q", tc.env, got, tc.want)
			}
		})
	}
}

// TestWorkerStatusProfileRuleAgreesWithTheDataPathResolution is the single
// source of truth check (Rule 13/14): the attribution rule must equal the
// resolution every other gopi data path uses, or the scan would attribute
// workers to a profile that does not exist.
func TestWorkerStatusProfileRuleAgreesWithTheDataPathResolution(t *testing.T) {
	home := t.TempDir()
	// absOwn is what a real gopi process in this environment reports as its
	// profile: the child resolves its data root from the SAME environment.
	absOwn, err := filepath.Abs(home)
	if err != nil {
		t.Fatalf("abs: %v", err)
	}
	for _, env := range [][]string{
		{config.GopiDirEnv + "=" + absOwn},
		{procname.EnvHome + "=" + absOwn},
	} {
		got := workerProfileFromEnv(env)
		t.Setenv(config.GopiDirEnv, absOwn)
		want, gerr := config.GopiDir()
		if gerr != nil {
			t.Fatalf("config.GopiDir: %v", gerr)
		}
		if got != want {
			t.Fatalf("attribution %q != the data-path resolution %q for env %q", got, want, env)
		}
	}
}

// --- ② / ③ the enumeration over a synthetic procfs root ----------------------

// liveWorkerChildren starts n real child processes and returns their pids. The
// fixtures below are keyed to REAL live pids on purpose: tools.PidAlive is not
// test-seamable (kill(0) + the real /proc), so a fabricated pid could never pass
// liveness and the fixture would assert nothing.
func liveWorkerChildren(t *testing.T, n int) []int {
	t.Helper()
	pids := make([]int, 0, n)
	for i := 0; i < n; i++ {
		cmd := exec.Command("/bin/sleep", "30")
		// Direct children: Pdeathsig covers SIGKILL, and the cwd is not the
		// subject (a REAL live pid is), so it is parked outside the checkout.
		cmd.SysProcAttr = &syscall.SysProcAttr{Pdeathsig: syscall.SIGKILL}
		cmd.Dir = t.TempDir()
		if err := cmd.Start(); err != nil {
			t.Fatalf("spawn child %d: %v", i, err)
		}
		pid := cmd.Process.Pid
		pids = append(pids, pid)
		t.Cleanup(func() {
			_ = cmd.Process.Kill()    //stylegate:ignore best-effort teardown of the test's own child
			_, _ = cmd.Process.Wait() //stylegate:ignore reaping the test's own child on cleanup
		})
	}
	return pids
}

// workerFixtureWithEnv plants a detached worker shape for pid under root AND the
// process's OWN environment file (/proc/<pid>/environ, NUL separated) — the file
// the attribution reads. An empty environ means "the environment could not be
// read", which is the unattributable case.
func workerFixtureWithEnv(t *testing.T, root string, pid int, environ []string) {
	t.Helper()
	procFixture(t, root, pid, binaryNameGopi, []string{"gopi", procname.VerbWorker}, pid)
	if len(environ) == 0 {
		return
	}
	dir := filepath.Join(root, strconv.Itoa(pid))
	body := strings.Join(environ, "\x00") + "\x00"
	if err := os.WriteFile(filepath.Join(dir, procWorkerEnvironName), []byte(body), 0o644); err != nil {
		t.Fatalf("write environ for pid %d: %v", pid, err)
	}
}

// TestWorkerStatusForeignProfileWorkerIsNotCounted is the DEFECT's minimal
// sample: two worker-shaped processes, ONE profile each, and only the caller's
// may be counted.
func TestWorkerStatusForeignProfileWorkerIsNotCounted(t *testing.T) {
	pids := liveWorkerChildren(t, 2)
	minePid, foreignPid := pids[0], pids[1]

	ownHome := t.TempDir()
	foreignHome := t.TempDir()
	root := t.TempDir()
	workerFixtureWithEnv(t, root, minePid, []string{config.GopiDirEnv + "=" + ownHome})
	workerFixtureWithEnv(t, root, foreignPid, []string{config.GopiDirEnv + "=" + foreignHome})

	t.Setenv(procname.EnvProcRoot, root)
	// The caller's own profile: THIS process's environment, exactly as the CLI
	// resolves it for a real `gopi worker --status` run.
	t.Setenv(config.GopiDirEnv, ownHome)
	own := workerStatusOwnProfile()
	if want := filepath.Join(ownHome, config.ProfileDirName); own != want {
		t.Fatalf("own profile = %q, want %q", own, want)
	}

	mine, foreign := fallbackWorkerPIDs(own)
	if !containsPid(mine, minePid) {
		t.Fatalf("the CALLER's own worker (pid %d, profile %s) must be counted, got %v", minePid, own, mine)
	}
	if containsPid(mine, foreignPid) {
		t.Fatalf("another profile's worker (pid %d) was counted in this profile's fleet: %v", foreignPid, mine)
	}
	if len(foreign) != 1 || foreign[0].PID != foreignPid {
		t.Fatalf("want exactly the foreign worker in the foreign list, got %+v", foreign)
	}
	if want := filepath.Join(foreignHome, config.ProfileDirName); foreign[0].Profile != want {
		t.Fatalf("the foreign row must name the PROCESS's profile %q, got %q", want, foreign[0].Profile)
	}
}

// TestWorkerStatusUnattributableWorkerIsCountedNotDropped is the DEGRADATION
// half: a worker whose own environment cannot be read (and one whose caller
// profile is unresolvable) is COUNTED. Reporting it as this profile's is a
// visible one-line miscount; dropping it is an invisible under-report of the
// whole fleet — the failure mode an operator cannot audit (Rule 15).
func TestWorkerStatusUnattributableWorkerIsCountedNotDropped(t *testing.T) {
	pids := liveWorkerChildren(t, 1)
	blindPid := pids[0]
	root := t.TempDir()
	// No environ file at all: /proc/<pid>/environ unreadable for this user.
	workerFixtureWithEnv(t, root, blindPid, nil)
	t.Setenv(procname.EnvProcRoot, root)
	t.Setenv(config.GopiDirEnv, t.TempDir())

	mine, foreign := fallbackWorkerPIDs(workerStatusOwnProfile())
	if !containsPid(mine, blindPid) {
		t.Fatalf("an UNATTRIBUTABLE worker must be counted, not dropped: mine=%v foreign=%+v", mine, foreign)
	}
	// And with an unresolvable OWN profile nothing is foreign either: the scan
	// falls back to the pre-attribution behaviour rather than emptying the list.
	mineAll, foreignAll := fallbackWorkerPIDs("")
	if !containsPid(mineAll, blindPid) {
		t.Fatalf("an unresolvable own profile must still count the worker: %v", mineAll)
	}
	if len(foreignAll) != 0 {
		t.Fatalf("with no own profile there is no foreign verdict to make, got %+v", foreignAll)
	}
}

func containsPid(pids []int, want int) bool {
	for _, pid := range pids {
		if pid == want {
			return true
		}
	}
	return false
}

// --- ② the SHIPPED binary ---------------------------------------------------

// TestWorkerStatusCLIReportsAForeignWorkerWithoutCountingIt drives the real CLI:
// the operator's exact symptom (their own worker + two leaked fixtures) in a
// controlled procfs root, and both halves of the contract — the fixture is NOT a
// worker line, IS a foreign row naming its profile, and the control fixture (same
// shape, the caller's profile) IS still counted.
func TestWorkerStatusCLIReportsAForeignWorkerWithoutCountingIt(t *testing.T) {
	bin := gopiTestBinPath(t)
	pids := liveWorkerChildren(t, 3)
	minePid, foreignPid, otherPid := pids[0], pids[1], pids[2]

	home := t.TempDir()
	foreignHome := t.TempDir()
	root := t.TempDir()
	// The caller's own worker, a leaked fixture of another profile, and a second
	// foreign profile (the report must name EACH row's own profile, never one
	// borrowed from the first).
	workerFixtureWithEnv(t, root, minePid, []string{config.GopiDirEnv + "=" + home})
	workerFixtureWithEnv(t, root, foreignPid, []string{config.GopiDirEnv + "=" + foreignHome})
	workerFixtureWithEnv(t, root, otherPid, []string{procname.EnvHome + "=" + filepath.Join(foreignHome, "second")})

	t.Setenv("GOPI_HOME", home)
	env := append(wsrStatusEnv(t, home), procname.EnvProcRoot+"="+root)
	code, out := wsrRunStatus(t, env, bin)
	if code != workerControlExitOK {
		t.Fatalf("`%s %s` exited %d, want 0:\n%s", CliVerbWorker, workerControlStatus, code, out)
	}

	// ① the caller's own worker is listed, with the ordinary worker-line prefix.
	if !strings.Contains(out, fmt.Sprintf("%s pid %d:", workerProgTopLevel, minePid)) {
		t.Fatalf("the caller's own worker (pid %d) must be listed as a worker:\n%s", minePid, out)
	}
	// ② the foreign workers are NOT worker lines — this is the epic's defect.
	for _, pid := range []int{foreignPid, otherPid} {
		if strings.Contains(out, fmt.Sprintf("%s pid %d:", workerProgTopLevel, pid)) {
			t.Fatalf("a worker of ANOTHER profile (pid %d) was rendered as this profile's worker:\n%s", pid, out)
		}
	}
	// ③ …and are not silently dropped either: the block names the count and each
	// pid with the profile the process's own environment names.
	wantHeader := fmt.Sprintf(workerStatusForeignFmt, workerProgTopLevel, 2, binaryNameGopi, procname.VerbWorker)
	if !strings.Contains(out, wantHeader) {
		t.Fatalf("the foreign block header %q is missing:\n%s", wantHeader, out)
	}
	for pid, profile := range map[int]string{
		foreignPid: filepath.Join(foreignHome, config.ProfileDirName),
		otherPid:   filepath.Join(foreignHome, "second", config.ProfileDirName),
	} {
		// The row is capped to fit a terminal line, so the assertion is the
		// honest one: the row names THE PROCESS'S OWN profile (the rendered text
		// is a prefix of it — two long sandbox paths must not both render as the
		// same truncated string, which is what a hardcoded 120 could not tell).
		got := foreignRowProfile(t, out, pid)
		if got == "" || !strings.HasPrefix(profile, got) {
			t.Fatalf("foreign row for pid %d names profile %q, which is not a prefix of its own profile %q:\n%s",
				pid, got, profile, out)
		}
	}
	// ④ the whole block obeys the terminal-row cap (the report is read on a
	// terminal; the measured sandbox paths are 130+ characters long).
	for _, l := range strings.Split(out, "\n") {
		if strings.Contains(l, "belong to ANOTHER profile") || strings.Contains(l, ": profile ") {
			if len([]rune(l)) > workerStatusResidueMaxRowRunes {
				t.Fatalf("a foreign row is %d runes (cap %d): %q", len([]rune(l)), workerStatusResidueMaxRowRunes, l)
			}
		}
	}
}

// foreignRowProfile returns the profile text the foreign block rendered for pid,
// or "" when there is no such row. The ellipsis the cap appends is trimmed, so
// the caller compares paths rather than layout.
func foreignRowProfile(t *testing.T, out string, pid int) string {
	t.Helper()
	prefix := fmt.Sprintf(workerStatusForeignRowPrefixFmt, workerStatusForeignIndent, pid)
	for _, l := range strings.Split(out, "\n") {
		if strings.HasPrefix(l, prefix) {
			return strings.TrimSuffix(strings.TrimPrefix(l, prefix), "…")
		}
	}
	return ""
}

// TestWorkerStatusCLIEmptyEnumerationExplainsAForeignFleet is the operator's
// other half: with a pending queue that WILL NOT drain and no worker of their
// own, the report must say the workers on the host belong to another profile
// rather than let "no workers running" read as a broken scan.
func TestWorkerStatusCLIEmptyEnumerationExplainsAForeignFleet(t *testing.T) {
	bin := gopiTestBinPath(t)
	pids := liveWorkerChildren(t, 1)
	foreignPid := pids[0]

	home := t.TempDir()
	foreignHome := t.TempDir()
	root := t.TempDir()
	workerFixtureWithEnv(t, root, foreignPid, []string{config.GopiDirEnv + "=" + foreignHome})

	t.Setenv("GOPI_HOME", home)
	env := append(wsrStatusEnv(t, home), procname.EnvProcRoot+"="+root)
	code, out := wsrRunStatus(t, env, bin)
	if code != workerControlExitOK {
		t.Fatalf("`%s %s` exited %d, want 0:\n%s", CliVerbWorker, workerControlStatus, code, out)
	}
	if !strings.Contains(out, workerStatusNoWorkers) {
		t.Fatalf("this profile runs no worker, so the report must say %q:\n%s", workerStatusNoWorkers, out)
	}
	if !strings.Contains(out, fmt.Sprintf(workerStatusNoWorkersForeignFmt, 1)) {
		t.Fatalf("the empty enumeration must explain the foreign fleet (1 process):\n%s", out)
	}
	if strings.Contains(out, fmt.Sprintf("%s pid %d:", workerProgTopLevel, foreignPid)) {
		t.Fatalf("the foreign worker was counted beside the empty message:\n%s", out)
	}
}
