package main

// Legibility for this suite's LAST stall shape: `go test ./cmd/agent/` can reach
// its own `panic: test timed out after 10m0s` while NO test goroutine is running.
//
// Measured 2026-09-16 on this host (ledger-busy-ab/accept-final/, both trees): the
// panic dump's "running tests:" line names a test that is no longer on any stack,
// and the live goroutines are all NON-test: a leaked `gopi task submit …` child
// (from TestAgentSpawnGuardWorkerTaskRunChainIsAllowed), a `net/http` server
// goroutine parked in epoll with a client that never reads, and an
// `os/exec` output copier waiting on that child. The stock panic names none of
// them, so the operator gets a 10-minute wait and a stack they must decode
// (Rule 15: a failure must name its cause).
//
// The watchdog is a REPORTER, not a fixer: it observes the suite from TestMain and,
// when the wall clock passes its budget, prints the measured cause — which child
// processes are still alive, in what state, with what command line. It never
// exits, never kills, and never changes what a test observes.

import (
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"github.com/nick/gopi/internal/testutil"
)

const (
	// suiteStallBudgetEnv lowers (or raises) the watchdog's budget in SECONDS so
	// a test can exercise the reporter without waiting out a real budget.
	suiteStallBudgetEnv = "GOPI_SUITE_STALL_BUDGET_SEC"

	// testTimeoutFlagName is Go's own `-test.timeout` flag name — the budget this
	// process must sit just below. Named because it is a value that decides
	// behaviour, not a comment (Rule 1).
	testTimeoutFlagName = "test.timeout"

	// suiteStallBudget is how long this test binary may run before the watchdog
	// reports why it is still running, USED ONLY when the caller set no
	// `-test.timeout` at all (0 = disabled). Otherwise the budget is derived from
	// the caller's own timeout by suiteStallBudgetFrom: a fixed budget is a
	// WALL-CLOCK alarm, and this suite legitimately takes longer than any fixed
	// value on a loaded host — measured 2026-10-07: 580.23 s of test time inside a
	// 600.42 s run at load 17-25 on 4 cores, and 1123.8 s under load 42-63
	// (scripts/test-budget.sh's own header). A fixed 8m budget declared a stall on
	// that perfectly healthy run.
	suiteStallBudget = 8 * time.Minute

	// suiteStallBudgetFlagPercent is the share of the caller's `-test.timeout`
	// the watchdog waits before reporting. Below 100 on purpose: the report must
	// reach the log BEFORE Go's own panic, which is the only ordering in which it
	// is readable (Go's panic prints a stack that names no cause).
	suiteStallBudgetFlagPercent = 90

	// suiteStallTickDivisor splits the live budget into the watchdog's cadence, so
	// the report lands within a small fraction of the budget rather than at the
	// next multiple of a fixed tick.
	suiteStallTickDivisor = 10

	// suiteStallTickMin floors that cadence, so a very small budget cannot spin.
	suiteStallTickMin = 100 * time.Millisecond

	// suiteStallTickInterval is how often the watchdog looks at the clock when it
	// must fall back to the fixed budget (a disabled `-test.timeout`). A live
	// budget gets its tick derived instead — see suiteStallTickFor.
	suiteStallTickInterval = 15 * time.Second

	// suiteStallCmdlineMax bounds each reported child's command line, so one
	// pathological argv cannot bury the verdict.
	suiteStallCmdlineMax = 160

	// procRootDir is the Linux procfs mount point every observation is read
	// from — named (Rule 1b), never spelled at a call site.
	procRootDir = "/proc"

	// childrenFileName is the per-thread file procfs exposes the children in
	// (/proc/<pid>/task/<tid>/children): exact, unlike a ppid scan.
	childrenFileName = "children"

	// suiteProcUnknownState / suiteProcUnknownCmdline are the honest rendering of
	// a process that vanished between two reads (Rule 15b: never an empty string
	// that looks like "no cause").
	suiteProcUnknownState   = "?"
	suiteProcUnknownCmdline = "(cmdline unreadable — the process exited between reads)"

	// suiteProcZombieState is the state letter procfs reports for a process that
	// has EXITED while its parent has not collected it (field 3 of
	// /proc/<pid>/stat). It is the ONE criterion for "this is a zombie" — never
	// `kill(pid, 0)`, which SUCCEEDS for a zombie (the pid is still allocated)
	// and so reads a dead process as alive.
	suiteProcZombieState = "Z"

	// stallProbeMarker identifies the probe child TestLiveSuiteChildrenNamesARealChild
	// starts, so the measurement can be recognised in the reported command line.
	stallProbeMarker = "gopi-stall-probe"

	// stallProbeSleepSec is how long that probe sleeps: long enough that the
	// measurement observes it alive, short enough that a leaked probe cannot
	// outlive the suite.
	stallProbeSleepSec = 30

	// stallProbeObserveBudget / stallProbeObserveInterval bound the measurement
	// test's poll: the child is alive for the whole window, but a Go runtime
	// thread can exit between the readdir and the per-thread `children` read, so
	// one sample is not evidence.
	stallProbeObserveBudget   = 2 * time.Second
	stallProbeObserveInterval = 10 * time.Millisecond

	// repoModuleMarker identifies a frame that belongs to THIS project inside a
	// goroutine dump. The module path, not a worktree path: a worktree's absolute
	// path changes per checkout and would silently match nothing.
	repoModuleMarker = "github.com/nick/gopi/"

	// goroutineHeaderPrefix is the line every goroutine block in a runtime.Stack
	// dump starts with (`goroutine <n> [<state>]:`) — named, never spelled inline.
	goroutineHeaderPrefix = "goroutine "

	// goroutineDumpBytes bounds the whole dump capture, so an enormous goroutine
	// population cannot turn the reporter into the problem.
	goroutineDumpBytes = 4 << 20

	// stallGoroutineFrameMax bounds the lines kept per reported goroutine.
	stallGoroutineFrameMax = 12

	// goroutineCreatedByPrefix labels the line naming a goroutine's spawner in a
	// runtime.Stack dump — named for the same reason as the header prefix.
	goroutineCreatedByPrefix = "created by "

	// goSourceSuffix identifies a stack frame's file:line line (an absolute .go
	// path), which carries no module marker of its own.
	goSourceSuffix = ".go:"

	// suiteSweepSettle is how long the settle window lasts: a test's own cleanup
	// (t.Cleanup → cmd.Wait / Kill) runs a few instructions after the case
	// reports, so a sweep run instantly would name a child that is about to be
	// reaped — a false report is worse than no report. One measured pass, never
	// a retry loop: the window is a fact about our own cleanup order, not a race
	// to win.
	suiteSweepSettle = time.Second

	// suiteSweepSettleStep is the poll step inside the window.
	suiteSweepSettleStep = 20 * time.Millisecond

	// suiteSweepChildGrace / suiteSweepChildKillGrace bound one child's polite
	// exit after the sweep signals it, and its exit after SIGKILL. A child that
	// leaves inside the first window is reaped without escalation.
	suiteSweepChildGrace     = 2 * time.Second
	suiteSweepChildKillGrace = 2 * time.Second

	// suiteSweepPollInterval is the liveness poll step while a reaped child
	// leaves.
	suiteSweepPollInterval = 10 * time.Millisecond

	// suiteSweepMinPGID is the smallest process-group id the sweep may signal —
	// group 0 is the test binary's own group, which must never be addressed
	// (killing it would take the whole run down). Mirrors the shared reaper's
	// floor (internal/testutil).
	suiteSweepMinPGID = 2

	// suiteSweepLeakLimit is how many leaked children this suite tolerates before
	// the guard calls it a leak. It is NOT zero on purpose: the three children
	// fixture ② starts EXIST to outlive their test's SIGKILL (a dead daemon would
	// assert nothing about inherited locks), so the honest boundary is "no child
	// beyond the documented trio", and the leak classes in
	// devdoc/cmd-agent-child-reaper.md name which trio.
	suiteSweepLeakLimit = 3
)

// execCommandForStallProbe builds a long-lived child for the measurement test: a
// plain `sleep` whose argv carries stallProbeMarker, so the test neither depends on
// gopi's own daemon machinery nor can be confused with another child.
func execCommandForStallProbe(t *testing.T) *exec.Cmd {
	t.Helper()
	cmd := exec.Command("sleep", strconv.Itoa(stallProbeSleepSec))
	cmd.Args = []string{stallProbeMarker, strconv.Itoa(stallProbeSleepSec)}
	// The probe's SUBJECT is its argv marker (the sweep must find it); its cwd is
	// never the subject, and Pdeathsig keeps it from outliving a killed binary.
	cmd.Dir = t.TempDir()
	cmd.SysProcAttr = &syscall.SysProcAttr{Pdeathsig: syscall.SIGKILL}
	return cmd
}

// suiteChildProcess is one live child of this test binary, as measured.
type suiteChildProcess struct {
	PID     int
	State   string
	Cmdline string
}

// suiteStallBudgetDuration resolves the watchdog budget: the env override when it
// is a positive number of seconds, else the budget derived from the caller's own
// `-test.timeout` (else suiteStallBudget when that is disabled) — Rule 2: the
// constant is the default and exactly this one place reads the env var.
func suiteStallBudgetDuration() time.Duration {
	if v := os.Getenv(suiteStallBudgetEnv); v != "" {
		if n, err := strconv.Atoi(strings.TrimSpace(v)); err == nil && n > 0 {
			return time.Duration(n) * time.Second
		}
	}
	return suiteStallBudgetFrom(testTimeoutFlag())
}

// testTimeoutFlag reads the package's EFFECTIVE `-test.timeout`, as cmd/go passed
// it (`-test.timeout=10m` by default; 0 = disabled). It is read from the flag
// set rather than from os.Args so a caller can pass it in any accepted spelling.
func testTimeoutFlag() time.Duration {
	f := flag.Lookup(testTimeoutFlagName)
	if f == nil {
		return 0
	}
	d, ok := f.Value.(flag.Getter)
	if !ok {
		return 0
	}
	v, ok := d.Get().(time.Duration)
	if !ok {
		return 0
	}
	return v
}

// suiteStallBudgetFrom derives the watchdog's budget from the caller's own test
// timeout. WHY IT IS DERIVED AND NOT FIXED: the watchdog exists so the measured
// cause reaches the log BEFORE Go's `panic: test timed out after <budget>` (whose
// own stack names no cause). A fixed value can only be right for one caller: at
// 8m it fires on every healthy run longer than 8m (measured — a 25m-budget run
// printed "suite stall … running 4m0s" while it was still completing tests
// normally), and it fires arbitrarily LATE on a short budget (a 2m run reached
// Go's panic with no verdict at all). A share of the caller's budget is the one
// value that is always just-before the panic, whatever budget was chosen.
func suiteStallBudgetFrom(flagTimeout time.Duration) time.Duration {
	if flagTimeout <= 0 {
		// A disabled timeout has no "just before" to sit at: fall back to the
		// constant, which is the only remaining statement of intent.
		return suiteStallBudget
	}
	return flagTimeout * suiteStallBudgetFlagPercent / 100
}

// suiteStallTickFor is the cadence at which the watchdog checks whether its budget
// has elapsed, derived from that budget.
//
// WHY THE TICK IS DERIVED TOO (measured 2026-10-07). A fixed 15 s tick does not go
// away when the budget becomes a fraction of the caller's timeout — it becomes a
// cliff. At `-timeout 60s` the budget is 54 s, and the tick that carries it is at
// 60 s: the arming run's own measurement was `go test ./cmd/agent/ -count=1
// -timeout 60s` → Go's `panic: test timed out after 1m0s`, the watchdog's verdict
// never printed, which is exactly the ordering the derivation exists to guarantee.
// A tick proportional to the budget is always a small fraction of it, so the
// verdict lands just before the panic at every budget.
//
// The floor keeps a pathologically small budget from spinning the clock read.
func suiteStallTickFor(budget time.Duration) time.Duration {
	tick := budget / suiteStallTickDivisor
	if tick < suiteStallTickMin {
		return suiteStallTickMin
	}
	return tick
}

// suiteStallVerdict renders the cause while it is still measurable: what the suite
// is waiting on (live children), and where its own goroutines are parked.
func suiteStallVerdict(budget, elapsed time.Duration, children []suiteChildProcess, dump string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "suite stall: this test binary has been running %s (watchdog budget %s) — Go's own "+
		"timeout prints a stack that names no cause; this is the measured one.\n",
		elapsed.Round(time.Second), budget.Round(time.Second))
	if len(children) == 0 {
		b.WriteString("live child processes: none — nothing this suite spawned is still alive, so the stall " +
			"is inside the test binary itself (a goroutine blocked on the runtime, a file lock or a " +
			"socket nobody reads), not a child handshake.\n")
	} else {
		fmt.Fprintf(&b, "live child processes (%d) — a test is waiting on one of these:\n", len(children))
		for _, c := range children {
			fmt.Fprintf(&b, "  pid=%d state=%s cmd=%q\n", c.PID, c.State, c.Cmdline)
		}
	}
	b.WriteString(suiteStuckFrames(dump))
	b.WriteString("where to look: a child's own log (this run's scratch root, see " +
		"testutil.SuiteTempRoot()) carries its refusal; a child in state D is blocked in the kernel " +
		"(disk), state S is an ordinary wait, state Z is a reaped-but-unwaited child.\n")
	return b.String()
}

// suiteStuckFrames filters a full goroutine dump down to the goroutines that are
// inside THIS repository, keeping each block's header and its own frames (capped)
// — the minimal set that answers "which work is stuck, and in what call". Frames
// outside this repository (runtime, database/sql, the sqlite driver) are dropped:
// the stock 10m panic already prints all of them, and the operator drowns in it.
//
// The filter is what makes the report actionable; it is deliberately a KEEP-list
// keyed on the module path, so a new frame shape needs no reporter change.
func suiteStuckFrames(dump string) string {
	if strings.TrimSpace(dump) == "" {
		return "goroutines of this repository: the dump could not be captured — the stall is real, its " +
			"location is not attributable from here (see the run's own log for what happened last).\n"
	}
	var b strings.Builder
	kept := 0
	for _, block := range suiteGoroutineBlocks(dump) {
		if !strings.Contains(block, repoModuleMarker) {
			continue
		}
		lines := suiteRepoFrames(block)
		if len(lines) == 0 {
			continue
		}
		kept++
		b.WriteString(strings.Join(lines, "\n"))
		b.WriteString("\n")
	}
	if kept == 0 {
		return "goroutines of this repository: none still on a stack — nothing in this project is parked " +
			"in Go code, so the wait is in the kernel or in a child (see the child list above).\n"
	}
	return fmt.Sprintf("goroutines of this repository still running (%d) — the blocked work is here:\n%s",
		kept, b.String())
}

// suiteRepoFrames selects the informative lines of one goroutine block: its header,
// its own repository frames together with the file:line beneath each, and the
// goroutine that created it. Non-repository frames (runtime, database/sql, the
// sqlite driver) are skipped — the stock panic already prints them — and the kept
// frames are capped so one deep recursion cannot bury the verdict.
func suiteRepoFrames(block string) []string {
	lines := strings.Split(block, "\n")
	var kept []string
	for i, line := range lines {
		if line == "" {
			continue
		}
		switch {
		case strings.HasPrefix(line, goroutineHeaderPrefix):
			kept = append(kept, line)
		case strings.Contains(line, repoModuleMarker):
			kept = append(kept, line)
			// The file:line of a repository frame sits on the NEXT line and is an
			// absolute path, so it carries no module marker of its own.
			if i+1 < len(lines) && strings.Contains(lines[i+1], goSourceSuffix) {
				kept = append(kept, lines[i+1])
			}
		case strings.HasPrefix(strings.TrimSpace(line), goroutineCreatedByPrefix):
			kept = append(kept, line)
		}
		if len(kept) >= stallGoroutineFrameMax {
			break
		}
	}
	return kept
}

// suiteGoroutineBlocks splits a runtime.Stack(all=true) dump into per-goroutine
// blocks: each starts at a `goroutine <n> [<state>]:` header and runs to the next
// header.
func suiteGoroutineBlocks(dump string) []string {
	var blocks []string
	var cur []string
	for _, line := range strings.Split(dump, "\n") {
		if strings.HasPrefix(line, goroutineHeaderPrefix) {
			if len(cur) > 0 {
				blocks = append(blocks, strings.Join(cur, "\n"))
			}
			cur = []string{line}
			continue
		}
		if len(cur) > 0 {
			cur = append(cur, line)
		}
	}
	if len(cur) > 0 {
		blocks = append(blocks, strings.Join(cur, "\n"))
	}
	return blocks
}

// captureSuiteGoroutineDump renders every goroutine's stack into a bounded buffer.
func captureSuiteGoroutineDump() string {
	buf := make([]byte, goroutineDumpBytes)
	n := runtime.Stack(buf, true)
	return string(buf[:n])
}

// suiteProcState reads a process's single-letter state from /proc/<pid>/stat
// (the field after the parenthesised comm, which may itself contain spaces).
func suiteProcState(pid int) string {
	raw, err := os.ReadFile(filepath.Join(procRootDir, strconv.Itoa(pid), "stat"))
	if err != nil {
		return suiteProcUnknownState
	}
	s := string(raw)
	i := strings.LastIndex(s, ") ")
	if i < 0 || i+2 >= len(s) {
		return suiteProcUnknownState
	}
	return string(s[i+2])
}

// suiteProcCmdline reads a process's command line, NUL-separated by the kernel.
func suiteProcCmdline(pid int) string {
	raw, err := os.ReadFile(filepath.Join(procRootDir, strconv.Itoa(pid), "cmdline"))
	if err != nil {
		return suiteProcUnknownCmdline
	}
	s := strings.TrimRight(strings.ReplaceAll(string(raw), "\x00", " "), " ")
	if s == "" {
		return suiteProcUnknownCmdline
	}
	if len(s) > suiteStallCmdlineMax {
		s = s[:suiteStallCmdlineMax] + "…"
	}
	return s
}

// liveSuiteChildren returns every live child of pid, deduplicated across the
// process's threads. Reading each thread's `children` file is exact; a ppid scan
// would miss a child re-parented to a subreaper and would race the table.
func liveSuiteChildren(pid int) []suiteChildProcess {
	taskDir := filepath.Join(procRootDir, strconv.Itoa(pid), "task")
	threads, err := os.ReadDir(taskDir)
	if err != nil {
		return nil
	}
	seen := make(map[int]bool)
	var out []suiteChildProcess
	for _, th := range threads {
		raw, err := os.ReadFile(filepath.Join(taskDir, th.Name(), childrenFileName))
		if err != nil {
			continue // the thread exited between the readdir and the read
		}
		for _, field := range strings.Fields(string(raw)) {
			cpid, err := strconv.Atoi(field)
			if err != nil || seen[cpid] {
				continue
			}
			seen[cpid] = true
			out = append(out, suiteChildProcess{
				PID:     cpid,
				State:   suiteProcState(cpid),
				Cmdline: suiteProcCmdline(cpid),
			})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].PID < out[j].PID })
	return out
}

// runSuiteStallWatchdog is the watchdog's whole body, injected so a test can drive
// it with a millisecond tick, a synthetic process list and a synthetic dump instead
// of waiting out a real budget. It returns when stopped or when the budget has
// elapsed and the verdict has been written.
func runSuiteStallWatchdog(
	budget, tick time.Duration,
	out io.Writer,
	stop <-chan struct{},
	children func(int) []suiteChildProcess,
	dump func() string,
) {
	start := time.Now()
	t := time.NewTicker(tick)
	defer t.Stop()
	for {
		select {
		case <-stop:
			return
		case <-t.C:
			elapsed := time.Since(start)
			if elapsed < budget {
				continue
			}
			fmt.Fprint(out, suiteStallVerdict(budget, elapsed, children(os.Getpid()), dump()))
			return
		}
	}
}

// startSuiteStallWatchdog arms the watchdog for this run and returns its stop
// function. It is called from TestMain only; the returned function is idempotent.
func startSuiteStallWatchdog(budget time.Duration) (stop func()) {
	stopCh := make(chan struct{})
	var once sync.Once
	// done is the JOIN: closing stopCh only makes the watchdog RUNNABLE, so a
	// caller that goes on to measure the process (TestMain's close-out sweep does
	// exactly that) can sample the watchdog's own frame and report this suite's
	// instrument as the suite's leak — measured, a fully green run failed on
	// "parked repository goroutines: 1" naming runSuiteStallWatchdog. A stop
	// function that returns before the thing it stopped is not a stop function:
	// it waits for the goroutine to leave.
	done := make(chan struct{})
	// out is the writer, captured HERE, before the goroutine exists. Reading
	// os.Stderr inside the literal is a read of a PACKAGE GLOBAL, and this suite
	// has a fixture that swaps it for the duration of a subtest
	// (chat_argv_test.go's chatRunArgs, which redirects os.Stdout/os.Stderr onto
	// pipes): under -race the pair is reported as a data race — measured,
	// `go test ./cmd/agent/ -run TestChatCLIVocabularyIsClosedEveryAcceptedFlagParses
	// -race` → "WARNING: DATA RACE … Write … chatRunArgs … Previous read …
	// startSuiteStallWatchdog.func1". The capture removes the concurrent read; it
	// cannot change what the watchdog reports, because it writes only on a stall
	// measured in minutes, long after any subtest's temporary swap is restored.
	out := os.Stderr
	// The budget is computed by the CALLER, on the goroutine that owns it, and
	// captured here. It used to be re-derived INSIDE the literal
	// (`budget := suiteStallBudgetDuration()`), which shadows the parameter and
	// reads `-test.timeout`'s flag.Value from this new goroutine — while the MAIN
	// goroutine is inside m.Run() → flag.Parse() writing that same address. The
	// race detector named the pair exactly, and the red proof is the reading
	// below (measured 2026-10-26, task 01a1761c, BEFORE this change):
	//
	//	go test ./cmd/agent -race -count=1 -run TestSuiteStall
	//	→ WARNING: DATA RACE
	//	     Read at … by goroutine 43:  flag.(*durationValue).Get()
	//	         testTimeoutFlag()  suite_stall_watchdog_test.go:223
	//	         suiteStallBudgetDuration()  suite_stall_watchdog_test.go:208
	//	         startSuiteStallWatchdog.func1()  suite_stall_watchdog_test.go:510
	//	   Previous write at … by main goroutine:  flag.(*durationValue).Set()
	//	         flag.(*FlagSet).Parse() … testing.(*M).Run()
	//	         cmd/agent.TestMain()  testmain_test.go:82
	//
	// Nothing is lost by removing the second read: the parameter IS the value the
	// inline read recomputed — TestMain calls
	// startSuiteStallWatchdog(suiteStallBudgetDuration()) one instruction earlier,
	// on the main goroutine, before the goroutine exists. Computing it twice bought
	// no freshness, and the second computation is what raced.
	go func() {
		defer close(done)
		runSuiteStallWatchdog(budget, suiteStallTickFor(budget), out, stopCh, liveSuiteChildren, captureSuiteGoroutineDump)
	}()
	return func() {
		once.Do(func() { close(stopCh) })
		<-done
	}
}

// TestSuiteStallWatchdogStopJoinsTheGoroutine pins a regression measured on
// 2026-09-25: the stop function closed the watchdog's channel and returned, so
// TestMain's close-out — which runs the very next instruction and MEASURES the
// process — could sample the watchdog's own frame and fail a fully green run
// with "parked repository goroutines: 1" naming runSuiteStallWatchdog. A stop
// that returns before the thing it stopped is not a stop, so this asserts the
// goroutine has LEFT the stack by the time stop() returns.
func TestSuiteStallWatchdogStopJoinsTheGoroutine(t *testing.T) {
	// The count is a DIFFERENCE, not an absolute: TestMain has already armed a
	// watchdog for this run, so its instance is on the stack throughout and an
	// absolute check would have measured that one (measured: it did, and failed
	// this test on a tree where the join was already fixed).
	count := func() int {
		return strings.Count(captureSuiteGoroutineDump(), "runSuiteStallWatchdog(")
	}
	before := count()
	stop := startSuiteStallWatchdog(suiteStallBudgetDuration())
	// Bounded wait for the new goroutine to reach the stack: a difference is only
	// meaningful once both ends have been observed.
	deadline := time.Now().Add(suiteWatchdogJoinProbeWindow)
	for count() <= before && time.Now().Before(deadline) {
		runtime.Gosched()
	}
	if mid := count(); mid <= before {
		t.Fatalf("the watchdog never appeared on the stack (before=%d, after start=%d) — the join cannot be observed from here", before, mid)
	}
	stop()
	if after := count(); after != before {
		t.Errorf("stop() returned with %d watchdog goroutine(s) parked (this test's start took it from %d to %d) — the stop function closed the channel but did not JOIN, so TestMain's close-out sweep, which runs the next instruction, can sample this suite's own instrument (measured false positive: `parked repository goroutines: 1` naming runSuiteStallWatchdog on a fully green run)", after, before, before+1)
	}
}

// suiteWatchdogJoinProbeWindow bounds the wait for the watchdog goroutine to
// reach the stack in the repro test above.
const suiteWatchdogJoinProbeWindow = 5 * time.Second

// TestSuiteStallWatchdogReportsLiveChildren drives the reporter end to end with a
// millisecond budget: the verdict must name the live child, so an operator reading
// the log knows what the suite is waiting on instead of decoding a stack.
func TestSuiteStallWatchdogReportsLiveChildren(t *testing.T) {
	stop := make(chan struct{})
	defer close(stop)
	var buf strings.Builder
	runSuiteStallWatchdog(20*time.Millisecond, 5*time.Millisecond, &buf, stop,
		func(int) []suiteChildProcess {
			return []suiteChildProcess{{PID: 4242, State: "S", Cmdline: "gopi task submit guard chain"}}
		},
		func() string { return stallSyntheticDump })
	got := buf.String()
	for _, want := range []string{
		"suite stall",
		"pid=4242",
		"state=S",
		"gopi task submit guard chain",
		"watchdog budget",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("verdict must name %q:\n%s", want, got)
		}
	}
}

// TestSuiteStallWatchdogReportsNoChildren covers the other measured shape: the
// stall is inside the test binary (nothing it spawned is alive). The verdict must
// say so explicitly rather than print an empty list that reads as "no cause".
func TestSuiteStallWatchdogReportsNoChildren(t *testing.T) {
	got := suiteStallVerdict(suiteStallBudget, 9*time.Minute, nil, "")
	if !strings.Contains(got, "live child processes: none") {
		t.Errorf("an empty child list must be stated, not omitted:\n%s", got)
	}
	if !strings.Contains(got, "inside the test binary") {
		t.Errorf("the verdict must place the stall when no child is alive:\n%s", got)
	}
}

// TestLiveSuiteChildrenNamesARealChild is the real-process half: a child this
// process actually starts must appear in the measurement. Without it the watchdog
// could be reporting a permanently empty list and still pass the synthetic tests.
func TestLiveSuiteChildrenNamesARealChild(t *testing.T) {
	child := execCommandForStallProbe(t)
	if err := child.Start(); err != nil {
		t.Fatalf("start probe child: %v", err)
	}
	defer func() {
		_ = child.Process.Kill()    //stylegate:ignore deferred teardown kill of the probe child; the process is being abandoned at test end
		_, _ = child.Process.Wait() //stylegate:ignore deferred teardown reap of the probe child just killed; the exit status is not asserted
	}()
	// The /proc read races nothing on the child's side (it is forked and alive),
	// but it does race a Go runtime thread that may have exited between the
	// readdir and the per-thread `children` read — so poll for a bounded moment
	// instead of asserting on a single sample.
	found := false
	deadline := time.Now().Add(stallProbeObserveBudget)
	for time.Now().Before(deadline) && !found {
		for _, c := range liveSuiteChildren(os.Getpid()) {
			if strings.Contains(c.Cmdline, stallProbeMarker) {
				found = true
				break
			}
		}
		if !found {
			time.Sleep(stallProbeObserveInterval) //stylegate:allow-sleep bounded poll for the child this test just started; the deadline in the loop caps it and exits on the marker
		}
	}
	if !found {
		t.Errorf("liveSuiteChildren(%d) did not report the child this test started within %v (marker %q)",
			os.Getpid(), stallProbeObserveBudget, stallProbeMarker)
	}
}

// TestSuiteStallBudgetEnvOverridesTheDefault pins the single env reader (Rule 2)
// and its fallback: a non-numeric or non-positive value must not disable the
// watchdog into a zero budget.
func TestSuiteStallBudgetEnvOverridesTheDefault(t *testing.T) {
	t.Setenv(suiteStallBudgetEnv, "3")
	if got := suiteStallBudgetDuration(); got != 3*time.Second {
		t.Errorf("budget = %v, want 3s", got)
	}
	t.Setenv(suiteStallBudgetEnv, "not-a-number")
	if got, want := suiteStallBudgetDuration(), suiteStallBudgetFrom(testTimeoutFlag()); got != want {
		t.Errorf("budget = %v, want the value derived from -test.timeout %v (%v)", got, testTimeoutFlag(), want)
	}
}

// TestSuiteStallBudgetDerivesFromTheTestTimeout is the regression for the
// false-stall defect measured 2026-10-07.
//
// THE DEFECT. The watchdog's budget was the fixed constant 8m, while a package
// budget is a caller's choice (`-test.timeout`, and this repo's ONE declared
// entry point scripts/test-budget.sh sets 25m). A fixed value is right for
// exactly one caller. Measured, same host, same tree:
//
//	old (fixed 8m)  a HEALTHY 25m-budget run still completing tests printed
//	                "suite stall: this test binary has been running 4m0s
//	                (watchdog budget 4m0s)" — a stall verdict on a run that was
//	                fine, and the suite's own README-shaped noise on every
//	                long run;
//	                the brief's 2m probe reached Go's panic with NO verdict at all
//	                8m never came, so the report the watchdog exists to make was
//	                simply absent.
//
// THE PROPERTY PINNED HERE. The verdict must be printable BEFORE Go's own panic
// whatever budget the caller chose — i.e. the watchdog's budget is always a
// fraction of `-test.timeout`, never a fixed wall clock. The 2026-10-07 healthy
// run is the sample: 600.42 s of wall time with 580.23 s of it inside tests.
func TestSuiteStallBudgetDerivesFromTheTestTimeout(t *testing.T) {
	// The measured healthy run, and the two entry points a caller actually uses.
	const measuredHealthyRun = 600 * time.Second
	declaredEntryBudget := 25 * time.Minute // scripts/test-budget.sh test_budget
	probeBudget := 90 * time.Second         // the shape of a narrow -timeout probe

	for _, c := range []struct {
		flagTimeout time.Duration
		want        time.Duration
	}{
		{flagTimeout: 0, want: suiteStallBudget},
		{flagTimeout: declaredEntryBudget, want: 22*time.Minute + 30*time.Second},
		{flagTimeout: probeBudget, want: 81 * time.Second},
		{flagTimeout: time.Minute, want: 54 * time.Second},
	} {
		if got := suiteStallBudgetFrom(c.flagTimeout); got != c.want {
			t.Errorf("suiteStallBudgetFrom(%v) = %v, want %v", c.flagTimeout, got, c.want)
		}
		if c.flagTimeout > 0 && !(suiteStallBudgetFrom(c.flagTimeout) < c.flagTimeout) {
			t.Errorf("the watchdog budget for -test.timeout %v is not below it — its verdict could never "+
				"precede Go's panic, which is the only ordering in which it is readable", c.flagTimeout)
		}
	}

	// The red half: the OLD fixed constant declared a stall on that healthy run.
	// If this ever stops holding, the defect above is not the one being regressed.
	if suiteStallBudget > measuredHealthyRun {
		t.Fatalf("precondition: the fixed constant %v no longer sits below the measured healthy run %v, so "+
			"this test is no longer pinned against the measured defect", suiteStallBudget, measuredHealthyRun)
	}
	// The green half: the declared entry point's derived budget clears it.
	if got := suiteStallBudgetFrom(declaredEntryBudget); got <= measuredHealthyRun {
		t.Errorf("under the declared entry point (-timeout %v) the watchdog budget is %v, which does not "+
			"clear the measured healthy run %v — a healthy long run would be reported as stalled again",
			declaredEntryBudget, got, measuredHealthyRun)
	}
}

// TestSuiteStallTickFollowsTheBudget pins the second half of the same ordering
// guarantee, measured the same day: a budget derived from `-test.timeout` is only
// useful if the watchdog actually LOOKS before the panic. With the fixed 15 s tick
// the arming run's own command (`go test ./cmd/agent/ -count=1 -timeout 60s`)
// derived a 54 s budget, was armed to check at 60 s — and printed nothing, going
// straight to Go's `panic: test timed out after 1m0s`.
func TestSuiteStallTickFollowsTheBudget(t *testing.T) {
	const probeBudget = 60 * time.Second // the arming run's own `-timeout`
	budget := suiteStallBudgetFrom(probeBudget)

	got := suiteStallTickFor(budget)
	if got >= budget {
		t.Fatalf("tick %v for budget %v (from -timeout %v): the watchdog would check for the first time at "+
			"or after its own budget, so its verdict can never precede Go's panic", got, budget, probeBudget)
	}
	// The verdict must land within a small fraction of the budget — a tick that is
	// merely "less than" the budget could still be 99% of it and report nothing in
	// practice.
	if got > budget/suiteStallTickDivisor {
		t.Errorf("tick %v for budget %v exceeds budget/%d — the report would land too late to be read",
			got, budget, suiteStallTickDivisor)
	}
	if tick := suiteStallTickFor(suiteStallTickMin / 4); tick != suiteStallTickMin {
		t.Errorf("a tiny budget must be floored at %v, got %v", suiteStallTickMin, tick)
	}
	// The end-to-end property, on the measured pair: the first check that can
	// report happens strictly before the caller's own timeout.
	firstCheck := budget + got
	if firstCheck >= probeBudget {
		t.Errorf("under -timeout %v the first reporting check is at %v, not before the caller's timeout %v",
			probeBudget, firstCheck, probeBudget)
	}
}

// TestSuiteStallWatchdogStopsWithoutReporting guards the quiet path: a suite that
// finishes inside its budget must produce NO verdict (the watchdog is a reporter,
// never noise on a green run).
func TestSuiteStallWatchdogStopsWithoutReporting(t *testing.T) {
	stop := make(chan struct{})
	var buf strings.Builder
	go runSuiteStallWatchdog(time.Hour, time.Millisecond, &buf, stop,
		func(int) []suiteChildProcess { return nil },
		func() string { return stallSyntheticDump })
	time.Sleep(20 * time.Millisecond) //stylegate:allow-sleep inside a one-hour budget, sleeping past the watchdog's 1ms interval is the condition being asserted (it must report NOTHING)
	close(stop)
	if buf.Len() != 0 {
		t.Errorf("watchdog wrote to the log inside the budget:\n%s", buf.String())
	}
}

// TestSuiteTempRootIsOffTheRootFilesystem keeps this file's own claim honest: the
// verdict points the reader at testutil.SuiteTempRoot() for the child's log, and
// that path must be off the `/` partition (Rule 22) or the pointer is useless.
func TestSuiteTempRootIsOffTheRootFilesystem(t *testing.T) {
	root := testutil.SuiteTempRoot()
	if root == "" {
		t.Fatal("SuiteTempRoot() is empty — the verdict's pointer would name nothing")
	}
	if err := testutil.AssertTempRootOffRootFS(); err != nil {
		t.Errorf("the scratch root this verdict points at is not off the `/` partition: %v", err)
	}
}

// stallSyntheticDump is a faithful miniature of a runtime.Stack(all=true) dump: two
// goroutines, one parked inside this repository (with its own frame, its file:line
// and the goroutine that created it) and one parked outside it (a runtime frame the
// reporter must drop). Both shapes matter — the attribution is a KEEP-list, so a
// filter that keeps everything would look identical on a real dump.
const stallSyntheticDump = `goroutine 1 [chan receive]:
testing.(*T).Run(0xc000102000, {0xef5440?, 0xc000289ae8?}, 0xef5440)
	/usr/local/go/src/testing/testing.go:1859 +0x431
github.com/nick/gopi/cmd/agent.TestTaskRunBriefingInlinePayloadUnchanged(0xc000308c40)
	/home/user/wl2/cmd/agent/cli_tasks_payload_spill_test.go:125 +0x178
created by testing.(*T).Run in goroutine 1
	/usr/local/go/src/testing/testing.go:1851 +0x413

goroutine 3195 [IO wait]:
internal/poll.runtime_pollWait(0x7f84fb509000, 0x72)
	/usr/local/go/src/runtime/netpoll.go:351 +0x85
database/sql.(*DB).connectionOpener(0xc000222820, {0xffc428, 0xc000ec8780})
	/usr/local/go/src/database/sql/sql.go:1261 +0x87
`

// TestSuiteStuckFramesNamesRepoGoroutinesOnly is the attribution half: the verdict
// must name the repository frame (and its file:line) of the goroutine that is stuck,
// and must NOT re-print the runtime / database-sql frames the stock panic already
// gives — a report that repeats the panic has added nothing.
func TestSuiteStuckFramesNamesRepoGoroutinesOnly(t *testing.T) {
	got := suiteStuckFrames(stallSyntheticDump)
	for _, want := range []string{
		repoModuleMarker + "cmd/agent.TestTaskRunBriefingInlinePayloadUnchanged",
		"cli_tasks_payload_spill_test.go:125",
		"created by testing.(*T).Run",
		"goroutines of this repository still running (1)",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("attribution must carry %q:\n%s", want, got)
		}
	}
	for _, unwanted := range []string{
		"database/sql.(*DB).connectionOpener",
		"netpoll.go",
	} {
		if strings.Contains(got, unwanted) {
			t.Errorf("attribution must not re-print the non-repository frame %q:\n%s", unwanted, got)
		}
	}
}

// TestSuiteStuckFramesStatesAnEmptyAttribution pins the honest rendering when the
// stall is not in this project's Go code (or no dump could be captured): the verdict
// must SAY so, because an operator reading an empty section cannot tell it from a
// reporter that silently did nothing (Rule 15b).
func TestSuiteStuckFramesStatesAnEmptyAttribution(t *testing.T) {
	noRepo := suiteStuckFrames("goroutine 1 [running]:\nruntime.goexit()\n\t/usr/local/go/src/runtime/asm_amd64.s:1700\n")
	if !strings.Contains(noRepo, "none still on a stack") {
		t.Errorf("a dump with no repository frame must say so:\n%s", noRepo)
	}
	missing := suiteStuckFrames("")
	if !strings.Contains(missing, "could not be captured") {
		t.Errorf("an uncapturable dump must be reported, not rendered empty:\n%s", missing)
	}
}

// TestSuiteStuckFramesCapturesTheRealDump is the real-runtime half of the
// attribution: captureSuiteGoroutineDump must produce a dump that this very test
// appears in. Without it the reporter could be filtering a permanently empty
// string and still pass every synthetic case.
func TestSuiteStuckFramesCapturesTheRealDump(t *testing.T) {
	got := suiteStuckFrames(captureSuiteGoroutineDump())
	if !strings.Contains(got, "cmd/agent.TestSuiteStuckFramesCapturesTheRealDump") {
		t.Errorf("the live dump must contain this test's own frame:\n%s", got)
	}
}

// --- the child sweep: the reporter above, turned into a guard ----------------
//
// WHY IT EXISTS (Rule 26 — the fix for an incomplete fix). The watchdog above
// only SPEAKS, and it speaks on a stall that is measured in MINUTES. The leaks
// it named (devdoc/cmd-agent-master-reds-20260925.md §2) had a lifetime of
// seconds: a `gopi worker` child, a net/http test server, four io.Pipe readers.
// A leak like that never tripped the watchdog, because the suite went on to
// finish — the binary simply stayed alive past the last verdict, and nothing
// ever said so. The consequence is not cosmetic: by the time main returns, every
// goroutine, descriptor and child is the process's own litter, and on this host
// the binary is what decides whether a run is a verdict or a 10-minute panic.
//
// This is the reporter's shape, applied at the END: measure what this test
// binary started, say it out loud, stop it, and fail by NAME if its number
// exceeds the documented limit. It never touches a process this binary did not
// start — the child set comes from /proc/<pid>/task/<tid>/children, i.e. the
// kernel's own record of OUR descendants (never a name scan, never pkill; Rule
// 24's sibling trap).

// suiteChildCount renders the sweep's measured line. It is the sentence the
// acceptance reads: "live child processes: N".
func suiteChildCount(n int) string {
	return fmt.Sprintf("live child processes: %d", n)
}

// sweepSuiteChildren stops every child this test binary still has, and reports
// what it found in os.Stderr's own words (not a failing assertion — TestMain has
// no *testing.T). The return value is the count BEFORE the sweep, so the guard
// can turn a NEW leak into a red test.
//
// The order is deliberate: MEASURE (the verdict is about what existed, not about
// what the sweep could catch), REPORT (so a green run still records the number),
// STOP (so the binary cannot be held open by a child nobody owns), then let the
// caller decide whether the number was a defect.
func sweepSuiteChildren(pid int, settle time.Duration, children func(int) []suiteChildProcess) int {
	deadline := time.Now().Add(settle)
	var last []suiteChildProcess
	for {
		last = children(pid)
		if len(last) == 0 || !time.Now().Before(deadline) {
			break
		}
		time.Sleep(suiteSweepSettleStep) //stylegate:allow-sleep bounded settle window for the test's own cleanup; the deadline in the loop caps it
	}
	fmt.Fprintf(os.Stderr, "suite child sweep: %s (settled %s)\n", suiteChildCount(len(last)), settle)
	for _, c := range last {
		fmt.Fprintf(os.Stderr, "suite child sweep: leaked child pid=%d state=%s cmd=%q\n", c.PID, c.State, c.Cmdline)
		stopSuiteChild(c.PID)
	}
	return len(last)
}

// stopSuiteChild stops one leaked child the way the shared reaper does: the
// child leads its OWN process group (Setpgid — the fixtures all spawn with it),
// so the group is signalled to take any grandchild with it, and the DIRECT child
// is waited for while the group is polled — a zombie is still a group member, so
// polling before the wait would burn the whole grace window on a process that is
// already dead.
//
// Every branch is a refusal with a reason, never a silent skip:
//   - a pid below suiteSweepMinPGID, or a pgid equal to this process's own
//     group, is never signalled (it would take the run down);
//   - an already-empty group is not signalled at all (a recycled pgid would
//     reach an unrelated process);
//   - a child that survives SIGTERM AND SIGKILL is REPORTED.
func stopSuiteChild(pid int) {
	pgid, err := syscall.Getpgid(pid)
	if err != nil {
		reportSuiteSweep("child pid %d: process group not resolvable (%v) — it exited between the measurement and the stop, there is nothing to signal", pid, err)
		return
	}
	if pgid < suiteSweepMinPGID || pgid == syscall.Getpgrp() {
		reportSuiteSweep("child pid %d is in group %d — refusing to signal a group this run shares (kill(2) there would take the test binary down)", pid, pgid)
		return
	}
	if err := syscall.Kill(-pgid, 0); err != nil {
		reportSuiteSweep("child pid %d: group %d is already gone — not signalling a stale id (a fully-reaped pgid can be recycled)", pid, pgid)
		return
	}
	_ = syscall.Kill(-pgid, syscall.SIGTERM) //stylegate:ignore the child may be racing to exit and ESRCH is expected; the survival poll below is the checked step
	if !suiteGroupGone(pid, pgid, suiteSweepChildGrace) {
		_ = syscall.Kill(-pgid, syscall.SIGKILL) //stylegate:ignore SIGKILL cannot fail on a live group; the survival check below is the checked step
		if !suiteGroupGone(pid, pgid, suiteSweepChildKillGrace) {
			reportSuiteSweep("child pid %d (group %d) survived SIGTERM and SIGKILL — it is unkillable and the binary may be held open by it", pid, pgid)
		}
	}
}

// suiteGroupGone reaps the direct child (non-blocking) while polling whether the
// group has emptied, and reports whether it converged inside the window.
func suiteGroupGone(pid, pgid int, window time.Duration) bool {
	deadline := time.Now().Add(window)
	for {
		var ws syscall.WaitStatus
		_, _ = syscall.Wait4(pid, &ws, syscall.WNOHANG, nil) //stylegate:ignore the reap is best-effort here: the group poll below is what decides, and a not-yet-exited child is the expected answer
		if syscall.Kill(-pgid, 0) != nil {
			return true
		}
		if !time.Now().Before(deadline) {
			return false
		}
		time.Sleep(suiteSweepPollInterval) //stylegate:allow-sleep bounded poll for a signalled group to empty
	}
}

// reportSuiteSweep writes one sweep sentence to stderr. TestMain has no
// *testing.T, so a failure here can only be reported (Rule 15b: never swallowed,
// never dressed up as success).
func reportSuiteSweep(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "suite child sweep: %s\n", fmt.Sprintf(format, args...))
}

// TestSuiteLeavesNoLeakedChildren is the regression guard (Rule 5's "every fix
// ships a minimal repro test", Rule 26's successor to the reporter alone).
//
// WHY TestMain IS NOT ENOUGH: a sweep that runs after m.Run() can reset the
// process state, but it cannot FAIL the run — TestMain exits with m.Run()'s code,
// so a leak would be reported and forgotten. This case runs the SAME sweep as its
// own assertion, while a *testing.T still exists, and hands TestMain a count to
// re-report at the end (a leak AFTER this case, e.g. in a later case's cleanup,
// is caught by that final line).
//
// WHY THE LIMIT IS NOT ZERO: three children of this suite are DESIGNED to
// outlive their test's SIGKILL — fixture ② (an inherited worker-lock descriptor
// is only observable in a process the test did not kill), the pty lifecycle
// worker, and its flock sibling. They are named in
// devdoc/cmd-agent-child-reaper.md, and they are reaped by this sweep, so their
// effect on the run is bounded by design. Every OTHER leftover is a defect with
// a file:line.
//
// RED PROOF (the guard is worthless if it cannot fail): revert ONE fix — put
// io.Pipe back into g5Probe.runLive, or drop the child reaper from the worker
// case, or the srv.Shutdown from the voice case — and this case fails, naming
// the pid and the command line. Measured in the task report; the limit exists so
// that the leaking shape is a RED TEST rather than a green run with a heavy tail.
func TestSuiteLeavesNoLeakedChildren(t *testing.T) {
	n := sweepSuiteChildren(os.Getpid(), suiteSweepSettle, liveSuiteChildren)
	accumulatedLeakedChildren.Store(int64(n))
	if n > suiteSweepLeakLimit {
		t.Errorf("this suite left %d live child process(es) — the documented design maximum is %d (see devdoc/cmd-agent-child-reaper.md for which children legitimately outlive their test, and which are leaks)", n, suiteSweepLeakLimit)
	}
}

// accumulatedLeakedChildren is the count TestMain re-reports after m.Run(), so a
// child leaked by a LATER test (this case runs in file order, not last) is still
// visible in the run's own log. It is written by the guard case and read once by
// the close-out — the two do not overlap in time (Go runs the non-parallel cases
// one goroutine at a time), so a plain atomic suffices.
var accumulatedLeakedChildren atomic.Int64

// --- the goroutine half: the same close-out measurement for parked work ------

// suiteParkedGoroutine is one goroutine of this repository still on the stack
// after the suite has finished: by construction, work no test is waiting for.
type suiteParkedGoroutine struct {
	Header string
	Frames []string
}

// suiteParkedRepoGoroutines is the close-out attribution: the goroutines that
// are OURS and are NOT the test framework's.
//
// The filter is a keep-list with three refusals, in this order:
//   - a block with no repository frame is not ours (the runtime's own workers
//     carry none);
//   - a block whose own frames mention `testing.` is the framework's runner —
//     at close-out the test binary's exit path legitimately sits in
//     testing.(*M).Run / main.main, and naming those would make the check
//     permanently red (the leak shape is the test's OWN goroutine, not the
//     harness's);
//   - the block's `created by …` line is dropped before that test, because a
//     leaked goroutine was legitimately CREATED by a test while being owned by
//     nobody — that line says where it came from, not what is holding it.
//
// Everything left is work this project started and never ended: the four
// io.PipeReaders and the net/http test server of the 2026-09-25 measurement are
// exactly this shape.
func suiteParkedRepoGoroutines(dump string) []suiteParkedGoroutine {
	var out []suiteParkedGoroutine
	for _, block := range suiteGoroutineBlocks(dump) {
		if !strings.Contains(block, repoModuleMarker) {
			continue
		}
		var own []string
		for _, line := range strings.Split(block, "\n") {
			if strings.Contains(line, goroutineCreatedByPrefix) {
				continue
			}
			own = append(own, line)
		}
		body := strings.Join(own, "\n")
		if strings.Contains(body, testingFrameMarker) || strings.Contains(body, suiteGoroutineSweepMarker) {
			continue
		}
		frames := suiteRepoFrames(block)
		if len(frames) == 0 {
			continue
		}
		out = append(out, suiteParkedGoroutine{Header: frames[0], Frames: frames})
	}
	return out
}

// testingFrameMarker identifies a frame belonging to the test FRAMEWORK, whose
// goroutines are expected at close-out.
const testingFrameMarker = "testing."

// suiteGoroutineSweepMarker identifies the close-out REPORT itself inside a
// dump: the goroutine capturing the dump carries this file's frames, and a
// sweep that counted itself would report a leak on every run (measured: the
// close-out named exactly one parked goroutine — its own capture).
const suiteGoroutineSweepMarker = "reportSuiteGoroutinesAtCloseOut"

// reportSuiteGoroutinesAtCloseOut reports (and counts) the repository goroutines
// still parked after the suite has finished. It returns the count, so the caller
// can turn a leak into a failing run rather than a log line.
func reportSuiteGoroutinesAtCloseOut() int {
	parked := suiteParkedRepoGoroutines(captureSuiteGoroutineDump())
	fmt.Fprintf(os.Stderr, "suite goroutine sweep: parked repository goroutines: %d\n", len(parked))
	for _, g := range parked {
		fmt.Fprintf(os.Stderr, "suite goroutine sweep: %s\n  %s\n", g.Header, strings.Join(g.Frames, "\n  "))
	}
	return len(parked)
}

// reportLeakedChildrenAtCloseOut re-reports the guard's count (and sweeps again)
// after m.Run(), so the suite's LAST word about children is measured rather than
// inherited. It returns the leftover count when that count is over the design
// limit, and 0 otherwise.
//
// WHY THE EXIT CODE IS THE CALLER'S: the in-run guard above can only see the
// tests that ran BEFORE it (Go runs the cases in source order), so a child
// leaked by a later case would pass it and would only be *mentioned* here. A
// leak whose only trace is a log line is the reporter-shaped blind spot this
// whole file exists to close, so the caller turns this count into a non-zero
// exit — the run reports FAIL, with the pid and command line above it.
func reportLeakedChildrenAtCloseOut() int {
	earlier := accumulatedLeakedChildren.Load()
	now := sweepSuiteChildren(os.Getpid(), suiteSweepSettle, liveSuiteChildren)
	fmt.Fprintf(os.Stderr, "suite child sweep: %s (guard measured %s earlier; %s at close-out)\n",
		suiteChildCount(now), suiteChildCount(int(earlier)), suiteChildCount(now))
	if now > suiteSweepLeakLimit {
		fmt.Fprintf(os.Stderr, "suite child sweep: LEAK — %s at close-out, over the documented design maximum of %d; the run is marked FAILED for it (see devdoc/cmd-agent-child-reaper.md)\n",
			suiteChildCount(now), suiteSweepLeakLimit)
		return now
	}
	return 0
}
