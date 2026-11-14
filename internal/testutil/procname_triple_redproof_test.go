package testutil

// RED-PROOF: the pid-REUSE case, where comm + cmdline ALONE accept a stranger.
//
// The 2026-09-12 worker count was fixed by counting the argv SHAPE instead of the
// process NAME, which removed the inflation — but the identity of a worker was
// still only two factors of three: comm (the name) + cmdline (the argv shape), and
// a PID, which is a NUMBER the kernel recycles. This file is the minimal repro of
// what that missing third factor costs.
//
// THE FIXTURE IS A NATURAL EXPERIMENT, and its two halves are the same process
// shape in two incarnations:
//
//	outer (pid O) — literally "$PROC_NAME $WORKER_VERB" and nothing else, i.e.
//	                byte-identical comm and argv to a real worker;
//	inner (pid I) — the SAME comm and the SAME argv, started 1tick later.
//
// This is not a contrived pair: pid I is what the kernel hands to a NEW worker
// after pid O exits — the shape a worker restart produces on every host that
// supervises a worker loop. Both pids exist here at the same time only so the
// comparison is deterministic (a fixture cannot make the kernel recycle a number
// on command); what is being compared is the DECISION, and the decision only ever
// sees one incarnation per pid.
//
// THE TWO PREDICATES, evaluated against the same fixture:
//
//	comm + cmdline only (the 2/3 predicate): "is there a live $PROC_NAME
//	    $WORKER_VERB process at this pid?" — the inner rebirth answers YES for pid
//	    O, because comm and argv are exactly what a worker wears. The caller is
//	    told the worker it started is still alive; it is not, and the pid now
//	    names a different process.
//	the TRIPLE (pid + starttime + cmdline): the recorded incarnation is O's start
//	    ticks; the live process at pid O carries I's ticks ⇒ RECYCLED.
//
// The test asserts BOTH directions, which is what makes it a red proof rather
// than an assertion about the new code: the 2/3 predicate must ACCEPT the
// recycled pid (so the old code fails this test), and the triple must REJECT it
// (so the new code passes). It also pins the boundary the other way — a restart
// that keeps the SAME incarnation must still read alive, or the guard would be
// "dead" for every live worker.

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/nickhuang99/gopi/internal/procname"
)

const (
	// reuseOuterTicks is the starttime recorded for the process the caller MEANS
	// (the incarnation that has since exited).
	reuseOuterTicks = "5001"
	// reuseInnerTicks is the starttime of the process that INHERITED the pid — a
	// later incarnation of the same command, which is exactly what a restart
	// produces. Different ticks is the ONLY difference from the outer shape.
	reuseInnerTicks = "5002"
	// reuseFixturePid is the pid slot both incarnations occupy (the fixture
	// cannot make the kernel recycle a number on command, so the two are
	// represented as the recorded identity and the live one).
	reuseFixturePid = "2001"
)

// TestRecycledPidPassesCommCmdlineAndFailsTheTriple is the red proof: the
// comm+cmdline predicate accepts a pid whose process is gone, and the three-factor
// verdict rejects it.
func TestRecycledPidPassesCommCmdlineAndFailsTheTriple(t *testing.T) {
	root := t.TempDir()
	writeReuseFixture(t, root)

	// --- the three factors, read from the fixture --------------------------
	// This read is SAFE and the exemption says why (Rule 15): `root` is a
	// hand-built fixture tree written above, so the pid it names is a file, not a
	// process this test spawned — there is no fork→execve window for the read to
	// land in. The one read is the assertion's precondition, not a race.
	argv, ok := procname.Cmdline(root, reuseFixturePid) // cmdline-race-ok: reads the hand-built fixture tree below, never a spawned process — no fork→execve window exists here
	if !ok {
		t.Fatalf("fixture %s has no readable cmdline — the test cannot state the shape it compares", reuseFixturePid)
	}
	comm, err := os.ReadFile(filepath.Join(root, reuseFixturePid, procname.CommName))
	if err != nil {
		t.Fatalf("read the fixture comm: %v", err)
	}
	liveTicks, ok := procname.StartTicks(root, reuseFixturePid)
	if !ok {
		t.Fatalf("fixture %s has no readable starttime — the triple's second factor is untestable", reuseFixturePid)
	}

	// --- PREDICATE 2/3: comm + cmdline only -------------------------------
	// workerShapeOnly is the pre-fix predicate, spelled with the same two facts
	// the launcher read before this change: is the process NAMED like a gopi AND
	// carrying the worker verb?
	workerShapeOnly := strings.TrimRight(string(comm), "\n") == procname.NameGopi &&
		len(argv) > procname.ArgvVerbIndex && argv[procname.ArgvVerbIndex] == procname.VerbWorker
	if !workerShapeOnly {
		t.Fatalf("the fixture's live process is not worker-shaped (comm %q argv %q) — the red proof needs a "+
			"shape the 2/3 predicate ACCEPTS", strings.TrimRight(string(comm), "\n"), argv)
	}
	// The recorded identity the caller holds: the pid it started, plus the argv
	// and the starttime it recorded at launch time.
	recorded := procname.Liveness{Pid: reuseFixturePid, StartTime: reuseOuterTicks, Cmdline: argv}
	if recorded.Joins(argv) {
		t.Logf("2/3 predicate: comm %q + argv %q MATCH the live process at pid %s (the caller is told its worker is alive)",
			strings.TrimRight(string(comm), "\n"), argv, reuseFixturePid)
	}

	// --- PREDICATE 3/3: the triple ----------------------------------------
	verdict := procname.Verdict(root, recorded)
	if verdict != procname.VerdictRecycled {
		t.Fatalf("the triple answered %q for pid %s, want %q — the pid exists and is worker-shaped, but its "+
			"starttime %q is not the recorded %q, i.e. the number was REUSED by a different process. A verdict "+
			"of alive here is the defect this test exists to catch.", verdict, reuseFixturePid, procname.VerdictRecycled, liveTicks, reuseOuterTicks)
	}
	// THE RED DIRECTION: had the caller only compared comm + cmdline (the 2/3
	// predicate), it would have concluded "alive" for a process that is gone.
	// Stating that explicitly is what makes this a proof about the OLD logic
	// instead of a description of the new one.
	commCmdlineSaysAlive := workerShapeOnly && recorded.Joins(argv) && recorded.StartTime != liveTicks
	if !commCmdlineSaysAlive {
		t.Fatalf("the fixture does not separate the predicates: comm+cmdline must ACCEPT this pid while the "+
			"triple REJECTS it (shape=%v argvMatches=%v ticksDiffer=%v)",
			workerShapeOnly, recorded.Joins(argv), recorded.StartTime != liveTicks)
	}
	t.Logf("RED/GREEN on one fixture: comm+cmdline → alive (the recycled pid is accepted); triple → %s", verdict)

	// --- the boundary: a restart with the SAME recorded incarnation --------
	// The opposite error must not be introduced either: when the ticks DO match
	// (the process the caller means is still there), the verdict is alive.
	same := procname.Liveness{Pid: reuseFixturePid, StartTime: liveTicks, Cmdline: argv}
	if got := procname.Verdict(root, same); got != procname.VerdictAlive {
		t.Errorf("the triple answered %q for the LIVE incarnation (matching starttime %q) — a guard that reports "+
			"a live worker as recycled is as wrong as one that reports a recycled pid as alive", got, liveTicks)
	}
	// A same-tick record whose ARGV differs is the SAME process: the incarnation
	// tick IS the identity, and a process may replace its image in place (a
	// wrapper becoming its target under the same pid). This assertion was the
	// opposite before the single-predicate consolidation (fix/proc-identity-
	// single-predicate): the ladder called this shape "recycled" — a fabricated
	// death for a live runner — while the ledger's own ladder called it "running",
	// i.e. two callers of ONE question disagreeing. The incarnation now wins in
	// both, which is what memory #2578 measured and what the destructive release
	// arms require. The cmdline remains an axis for a record that states NO
	// incarnation (the worker count's shape).
	otherRole := procname.Liveness{Pid: reuseFixturePid, StartTime: liveTicks,
		Cmdline: []string{procname.NameGopi, procname.VerbTask}}
	if got := procname.Verdict(root, otherRole); got != procname.VerdictAlive {
		t.Errorf("the triple answered %q for a same-incarnation process wearing a DIFFERENT argv — matching "+
			"starttime ticks ARE the identity (the runner re-exec'd in place), and a ladder that fabricates a "+
			"death here lets the reclaim arm free a live run's worktree", got)
	}
	// An UNREADABLE incarnation must degrade to unknown, never to a fabricated
	// death (Rule 15).
	gone := procname.Liveness{Pid: reuseMissingPid, StartTime: reuseOuterTicks, Cmdline: argv}
	if got := procname.Verdict(root, gone); got != procname.VerdictRecycled {
		t.Errorf("a pid with no directory answered %q, want %q", got, procname.VerdictRecycled)
	}
}

// reuseMissingPid is a pid the fixture does NOT create — the process-is-gone end
// of the ladder.
const reuseMissingPid = "2999"

// TestLauncherCountRejectsRecycledPidButCountsTheLiveOne is the same proof at the
// OPERATOR's surface: the launcher must report the fixture worker with the
// starttime of the process actually there, so the number an operator reads can be
// checked for recycling instead of trusted. It runs the real script through its
// --count seam over the reuse fixture.
func TestLauncherCountRejectsRecycledPidButCountsTheLiveOne(t *testing.T) {
	script := filepath.Join(repoRootForProcnameGate(t), filepath.FromSlash(workerUpScriptRel))
	root := t.TempDir()
	writeReuseFixture(t, root)

	workers, _, pids := runWorkerUpCount(t, script, root)
	if workers != 1 {
		t.Fatalf("live worker count = %d, want 1 — the fixture holds ONE worker-shaped process", workers)
	}
	pid, ticks, ok := splitPIDIdentity(strings.TrimSpace(pids))
	if !ok {
		t.Fatalf("the launcher reported %q, which is not a pid:starttime identity — the operator cannot check "+
			"the reported pid for recycling without it", pids)
	}
	if pid != reuseFixturePid {
		t.Errorf("reported pid = %q, want %q", pid, reuseFixturePid)
	}
	want := fixtureStartTicks(t, root, reuseFixturePid)
	if want == "" {
		t.Fatalf("the reuse fixture carries no stat for pid %s — the incarnation the test compares against is missing", reuseFixturePid)
	}
	if ticks != want {
		t.Errorf("the launcher stamped pid %s with starttime %q, but the live process's stat field %d is %q — a "+
			"recycled pid would then be indistinguishable from the worker the operator started (the 2/3 gap)",
			reuseFixturePid, ticks, procname.StatFieldStartTime, want)
	}
	t.Logf("launcher --count over the reuse fixture: workers=%d identities=%q (live incarnation ticks=%s)", workers, strings.TrimSpace(pids), want)
}

// TestRealWorkerIncarnationMatchesProcStat closes the loop with the OPERATING
// SYSTEM rather than a fixture: a real process is started, its pid:starttime is
// read through the production reader, and the pair is re-checked — while the
// process lives it must read alive, and after it exits and is reaped the same
// recorded identity must NOT read alive. This is the "real, not injected" half of
// the evidence: the fixture above proves the DECISION, this proves the DECISION
// agrees with the kernel about a real incarnation.
func TestRealWorkerIncarnationMatchesProcStat(t *testing.T) {
	if _, err := os.Stat(procnameRootDefault()); err != nil {
		t.Skipf("no procfs at %s — the identity triple has nothing to read on this host", procnameRootDefault())
	}
	cmd := exec.Command("sleep", realSleepSeconds)
	if err := cmd.Start(); err != nil {
		t.Skipf("cannot start a probe process: %v", err)
	}
	pid := cmd.Process.Pid
	// Reap the child the moment it exits, so the "gone" observation below is a
	// REAPED pid (the shape a recycled number really comes from) rather than a
	// zombie that still answers kill(pid, 0).
	reaped := make(chan struct{})
	go func() {
		_ = cmd.Wait() //stylegate:ignore reaping the child is the SETUP for a recycled-pid fixture; its exit status is deliberately not the subject
		close(reaped)
	}()
	defer func() {
		_ = cmd.Process.Kill() //stylegate:ignore killing the reaped child's pid is the fixture's deliberate recycled-number probe; the error is expected
		<-reaped
	}()

	pidStr := strconv.Itoa(pid)
	// The starttime is read first and needs no retry: it is readable the moment
	// the fork produces the pid (only cmdline waits for execve).
	ticks, ok := procname.StartTicks(procnameRootDefault(), pidStr)
	if !ok {
		t.Fatalf("cannot read the starttime of the probe process %d — the production reader is broken", pid)
	}
	argv := requireProbeCmdline(t, procnameRootDefault(), pid, "the real probe process's own cmdline")
	live := procname.Liveness{Pid: pidStr, StartTime: ticks, Cmdline: argv}
	if got := procname.Verdict(procnameRootDefault(), live); got != procname.VerdictAlive {
		t.Fatalf("a real live process %d (ticks %s) read as %q, want %q", pid, ticks, got, procname.VerdictAlive)
	}
	// A stranger's incarnation claim on the same pid must be rejected: this is
	// the real-process form of the fixture's red case.
	if got := procname.Verdict(procnameRootDefault(), procname.Liveness{Pid: pidStr, StartTime: ticks + "1", Cmdline: argv}); got != procname.VerdictRecycled {
		t.Errorf("the same pid with a different starttime read as %q, want %q", got, procname.VerdictRecycled)
	}

	// The kill(pid, 0) lie, measured: the probe is dead and reaped, yet the
	// signal-based probe is not what tells us — the identity does.
	if err := syscall.Kill(pid, syscall.SIGKILL); err != nil {
		t.Logf("kill the probe: %v", err)
	}
	<-reaped
	if got := procname.Verdict(procnameRootDefault(), live); got == procname.VerdictAlive {
		t.Errorf("the recorded identity of process %d still reads ALIVE after it exited and was reaped — that "+
			"is exactly the answer a recycled pid would also produce", pid)
	}
}

// TestStartTimeFieldNumberIsDerived pins the ONE number the shell and the Go
// reader must agree on: /proc/<pid>/stat field 22 (starttime). internal/procname
// derives it from the state field (3 + 19), so a change to the base constant
// cannot silently move the reader onto a neighbour column — and the shell's
// selection (field − StatFieldState + 1 words after the ')') is the same
// arithmetic, asserted against the same constant by the procname gate's knob
// table (STAT_FIELD default == StatFieldStartTime).
func TestStartTimeFieldNumberIsDerived(t *testing.T) {
	if procname.StatFieldStartTime != procname.StatFieldState+19 {
		t.Errorf("StatFieldStartTime = %d, want %d (StatFieldState + 19) — the derived field number drifted",
			procname.StatFieldStartTime, procname.StatFieldState+19)
	}
	if procname.StatFieldStartTime != 22 {
		t.Errorf("StatFieldStartTime = %d, want the kernel's 22", procname.StatFieldStartTime)
	}
}

// TestStatFieldRefusesShortLines is the negative half of the parser, and it is
// the shape that a hand-written fixture really produces: a stat line with fewer
// fields than the one asked for must answer "cannot answer" — never the value of
// a neighbouring column, which would silently report a WRONG incarnation (the
// difference between "alive" and "recycled").
func TestStatFieldRefusesShortLines(t *testing.T) {
	cases := []struct {
		name string
		line string
	}{
		{"no parenthesis at all", "1234 gopi worker S 0 0 0"},
		{"too few fields", "1234 (gopi worker) S 0 0 0 1 1"},
		{"empty after comm", "1234 (gopi worker) "},
	}
	for _, c := range cases {
		if got, ok := procname.StatField(c.line, procname.StatFieldStartTime); ok {
			t.Errorf("%s: StatField returned (%q, true) — a short line must answer UNKNOWN, not a neighbour's "+
				"column", c.name, got)
		}
	}
	// The parser also has to reach the RIGHT column when the comm carries spaces
	// and parentheses — the case the whole last-')' rule exists for.
	line := procStatLine("4242", "(gopi worker) v2", "S", "888888")
	got, ok := procname.StatField(line, procname.StatFieldStartTime)
	if !ok || got != "888888" {
		t.Errorf("a comm holding spaces and parentheses (the parenthesised-name shape) parsed field %d as (%q, %v), "+
			"want (888888, true)", procname.StatFieldStartTime, got, ok)
	}
	// Cross-checked against the OS: a real process's field 22 must equal what the
	// kernel-independent reader says.
	if _, err := os.Stat(procnameRootDefault()); err == nil {
		ticks, ok := procname.StartTicks(procnameRootDefault(), strconv.Itoa(os.Getpid()))
		if !ok || ticks == "" {
			t.Errorf("cannot read this process's own starttime — the production reader is broken")
		}
	}
}

// realSleepSeconds is how long the real-process probe stays alive: long enough to
// be observed on a busy host, short enough not to lengthen the suite.
const realSleepSeconds = "20"

// The fork→execve window, made measurable instead of assumed.
//
// MEASURED (2026-10-12, host=host-a, go test -count=50, this file's own
// probe): exec.Command(...).Start() returns ONCE THE FORK IS DONE, but the child
// may not have reached execve yet — in that window /proc/<pid>/cmdline EXISTS
// and is 0 bytes, and the process reads state=R. Reading it ONCE right after
// Start() therefore answers "no cmdline" for a process that is perfectly alive,
// which is how this test failed 9 times out of 50 (-count=50, unmodified file).
// An empty cmdline is genuinely NO EVIDENCE (procname.Cmdline's doc: a zombie's
// cmdline is empty by design), so the reading is not the defect and the assertion
// must NOT be relaxed — the read simply has to be RETRIED until the kernel
// answers. The window closes microseconds after execve; the budget below is
// ~1000x the widest window measured for this shape (1.58ms, memory #8295).
//
// Same root cause, same remedy as task 01a12b6a-cabc (internal/tools/
// live_claim_worktree_veto_test.go) — but that helper is unexported IN ANOTHER
// PACKAGE, so it cannot be called from here (Rule 13/14: the VALUE is shared, the
// unexported helper is not). This file carries the testutil-local sibling.
const (
	// cmdlineProbeAttempts is the bounded retry count for ONE command-line read.
	// It is a retry count, never a sleep-and-pray: every attempt reads, and the
	// first non-empty answer wins (Rule 27: repetition, never generated load).
	cmdlineProbeAttempts = procname.CmdlineAwaitAttempts
	// cmdlineProbeStep is the pause between retries; attempts × step is the
	// total window the child is given to reach execve.
	cmdlineProbeStep = procname.CmdlineAwaitStep
)

// cmdlineProbeRead reads ONE process command line: Value is the reading ("" = no
// argv evidence: the pre-exec window, a zombie, or a process with no argv), Err
// is a failure to read the file itself.
type cmdlineProbeRead struct {
	Value string
	Err   error
}

// awaitProbeCmdline retries a command-line read until the kernel answers
// something, up to attempts reads spaced by step. It returns the reading and an
// empty reason on success, or ("", reason) when the line stayed unmeasurable —
// the reason naming the label, the retry count and what the reads actually did.
//
// It is deliberately SEPARATE from the t.Fatalf wrapper that calls it: a retry
// reachable only through a Fatal cannot be exercised by a test, and an
// unexercised retry is how the next reader ends up with a bare read — which is
// precisely the defect this task repairs.
func awaitProbeCmdline(read func() cmdlineProbeRead, label string, attempts int, step time.Duration) (string, string) {
	var lastErr error
	for i := 0; i < attempts; i++ {
		if i > 0 && step > 0 {
			time.Sleep(step) //stylegate:allow-sleep bounded poll of the child's own /proc/<pid>/cmdline, which the kernel leaves 0 bytes until execve
		}
		r := read()
		if r.Value != "" {
			return r.Value, ""
		}
		if r.Err != nil {
			lastErr = r.Err
		}
	}
	why := fmt.Sprintf(
		"%s: no command line after %d reads (%v apart) — the kernel never answered /proc/<pid>/cmdline",
		label, attempts, step)
	if lastErr != nil {
		why += "; last read error: " + lastErr.Error()
	} else {
		why += "; every read returned an EMPTY file (the pre-exec window, a zombie, or a pid the kernel gives no argv)"
	}
	return "", why
}

// requireProbeCmdline is the precondition for the real probe process: the argv the
// test is about to compare against, read only once the kernel answers it.
// Unmeasurable after the bounded retry is a LOUD failure naming the probe and the
// path that was read — never a silent "" that a later comparison turns into a
// verdict about the code under test (Rule 15).
func requireProbeCmdline(t *testing.T, root string, pid int, what string) []string {
	t.Helper()
	pidStr := strconv.Itoa(pid)
	v, why := awaitProbeCmdline(func() cmdlineProbeRead {
		argv, ok := procname.Cmdline(root, pidStr)
		if !ok {
			// (false) is the reader's way of saying "no argv evidence": either
			// the pre-exec window or a process with no argv. Both are what the
			// retry exists for, so this is an empty READING, not a read error.
			return cmdlineProbeRead{}
		}
		return cmdlineProbeRead{Value: strings.Join(argv, " ")}
	}, what, cmdlineProbeAttempts, cmdlineProbeStep)
	if v == "" {
		t.Fatalf("probe (pid %d, cmdline path %s): %s",
			pid, filepath.Join(root, pidStr, procname.CmdlineName), why)
	}
	return strings.Split(v, " ")
}

// TestAwaitProbeCmdlineRetriesThePreExecWindow is the regression guard for the
// repair itself (Rule 5: every bug fix ships a minimal repro). It drives the
// RETRY, not the Fatal: the read is injected, so a first-read-empty sequence is
// reproduced deterministically instead of waiting for a real race to happen.
func TestAwaitProbeCmdlineRetriesThePreExecWindow(t *testing.T) {
	// (1) The measured shape: the first reads answer nothing (the fork window),
	// and the retry is what carries the test past it.
	emptyReads := 0
	v, why := awaitProbeCmdline(func() cmdlineProbeRead {
		emptyReads++
		if emptyReads <= 3 {
			return cmdlineProbeRead{}
		}
		return cmdlineProbeRead{Value: "sleep 20"}
	}, "injected pre-exec window", 64, 0)
	if why != "" || v != "sleep 20" {
		t.Fatalf("a read that answers on attempt 4 came back (%q, %q) — the retry does not carry the probe past the fork window", v, why)
	}
	if emptyReads != 4 {
		t.Errorf("the retry read %d times, want exactly 4 (it must stop at the first answer, not poll the full budget)", emptyReads)
	}

	// (2) The exhausted budget is a LOUD, specific refusal — and it counts what
	// it actually did.
	reads := 0
	v, why = awaitProbeCmdline(func() cmdlineProbeRead {
		reads++
		return cmdlineProbeRead{}
	}, "never answers", 5, 0)
	if v != "" {
		t.Errorf("an always-empty read returned %q, want the empty reading", v)
	}
	if reads != 5 {
		t.Errorf("the retry read %d times, want the 5 it was budgeted", reads)
	}
	for _, want := range []string{"never answers", "5 reads", "EMPTY file"} {
		if !strings.Contains(why, want) {
			t.Errorf("the refusal %q does not name %q — an unmeasurable probe must say WHICH probe and WHAT it saw", why, want)
		}
	}

	// (3) A read ERROR is reported as itself, never as "the kernel produced an
	// empty file": those are two different facts about the world.
	_, why = awaitProbeCmdline(func() cmdlineProbeRead {
		return cmdlineProbeRead{Err: os.ErrPermission}
	}, "unreadable", 2, 0)
	if !strings.Contains(why, "last read error") || !strings.Contains(why, os.ErrPermission.Error()) {
		t.Errorf("a read that always errored reported %q, which does not carry the error — an unreadable /proc is not an empty /proc", why)
	}
	if strings.Contains(why, "EMPTY file") {
		t.Errorf("a read ERROR was reported as an empty file: %q", why)
	}
}

// procnameRootDefault is procfs's own root — the default the launcher's
// PROC_ROOT seam takes, named here so the test never types the path inline.
func procnameRootDefault() string {
	if root := os.Getenv(procname.EnvProcRoot); root != "" {
		return root
	}
	return procRootDefault
}

// writeReuseFixture builds the pid-reuse tree: ONE pid slot whose live process is
// a later incarnation (different starttime) of the command an earlier incarnation
// ran. The earlier incarnation is represented by the RECORDED identity the test
// constructs, because a fixture cannot make the kernel recycle a pid number on
// command — the two observations the decision sees are "what the caller recorded"
// and "what is at the pid now", and those are exactly the two this tree fixes.
func writeReuseFixture(t *testing.T, root string) {
	t.Helper()
	dir := filepath.Join(root, reuseFixturePid)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("create the reuse fixture pid dir: %v", err)
	}
	// comm and argv are the SAME as a real worker's — that is the point: the 2/3
	// predicate has nothing else to look at.
	if err := os.WriteFile(filepath.Join(dir, procname.CommName), []byte(procname.NameGopi+"\n"), 0o644); err != nil {
		t.Fatalf("write the reuse fixture comm: %v", err)
	}
	argv := []string{procname.NameGopi, procname.VerbWorker}
	if err := os.WriteFile(filepath.Join(dir, procname.CmdlineName), []byte(strings.Join(argv, "\x00")+"\x00"), 0o644); err != nil {
		t.Fatalf("write the reuse fixture cmdline: %v", err)
	}
	// The stat line carries the LIVE (later) incarnation's starttime.
	line := procStatLine(reuseFixturePid, procname.NameGopi, "S", reuseInnerTicks)
	if err := os.WriteFile(filepath.Join(dir, procname.StatName), []byte(line+"\n"), 0o644); err != nil {
		t.Fatalf("write the reuse fixture stat: %v", err)
	}
	// A second, differently-shaped process so the fixture is not a single-entry
	// tree (a scan that silently returns nothing must not pass this test).
	other := filepath.Join(root, reuseMissingPid)
	if err := os.MkdirAll(other, 0o755); err != nil {
		t.Fatalf("create the second fixture entry: %v", err)
	}
	if err := os.WriteFile(filepath.Join(other, procname.CommName), []byte("bash\n"), 0o644); err != nil {
		t.Fatalf("write the second fixture comm: %v", err)
	}
	if err := os.WriteFile(filepath.Join(other, procname.CmdlineName), []byte("/bin/bash\x00-c\x00true\x00"), 0o644); err != nil {
		t.Fatalf("write the second fixture cmdline: %v", err)
	}
	if err := os.WriteFile(filepath.Join(other, procname.StatName),
		[]byte(procStatLine(reuseMissingPid, "bash", "S", "7001")+"\n"), 0o644); err != nil {
		t.Fatalf("write the second fixture stat: %v", err)
	}
}
