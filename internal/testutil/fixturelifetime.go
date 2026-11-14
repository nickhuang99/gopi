package testutil

// fixturelifetime.go — THE ONE definition of "a fixture child is bound to the
// test binary's lifetime".
//
// THE DEFECT (measured on zpeer 2026-09-26). The daemon lifetime binding
// (internal/daemonlifetime) was built for `gopi <verb> --daemon` processes: the
// test suite arms ONE pipe, every daemon a launcher fork/execs inherits the read
// end, and the kernel closes the pipe when the test binary dies — so the daemon
// kills itself, SIGKILL included. FIXTURE children — the re-exec'd test binary
// standing in for a gopi process, a python3 stand-in server — never called the
// watchdog entry point, so they kept the DEFAULT lifetime: they outlive the test
// binary, reparent to init, and stand in the task's worktree forever. Measured:
// `python3 …/blackhole.py` (pid 1503983, ppid 1, 11 h+) and `signalrec.test`
// (pid 1525157) pinned `gopi-01a0d68746ed7ea090e56bff794e1b9f`, so
// finish-cleanup's holder guard — correctly — refused to release the directory,
// and the branch could not be deleted either.
//
// WHY THIS FILE EXISTS RATHER THAN A SECOND MECHANISM. The production
// mechanism is already right; what was missing is a single place a fixture
// author can reach for. Before this file the knowledge was spread over three
// call sites in two packages, each of which had to get right: the fd NUMBER
// (os/exec renumbers ExtraFiles from fd 3, and the inherited read end is parked
// above fd 200 precisely so a launcher's ExtraFiles cannot replace it), the
// group scoping (so an in-process exit path can still reap the child), and the
// Rule 15 duty to REPORT an unarmed binding instead of running unbound. A
// fixture author who gets any of the three wrong produces a leak that looks
// exactly like a passing test. So this is the ONE definition (Rule 14), and the
// sub-package helpers delegate to it rather than re-deriving it.
//
// THE THREE HALVES, and a fixture needs all three:
//
//	the SUITE arms   — ArmAndSweepForSuite → daemonlifetime.ArmThisProcess:
//	                   one pipe per test binary, write end held by the binary
//	                   ALONE (os.Pipe leaves O_CLOEXEC on it, so no descendant
//	                   can hold it — that is what makes the HUP mean "the test
//	                   binary died").
//	the CHILD adopts — WatchFixtureBinding: AdoptInheritedHoldFD (the hold-side
//	                   variable becomes the watch-side one, at the SAME number)
//	                   then the production WatchFromEnv, then a REPORT of the
//	                   verdict so the parent can refuse a child that is not
//	                   actually bound.
//	the PARENT checks— RequireFixtureBinding in the suite (fails rather than
//	                   passing for the wrong reason), RequireFixtureChildBinding
//	                   after the child's own readiness handshake.
//
// GROUP SCOPING is the fourth thing and it is not optional: the death pipe
// covers the test binary being KILLED (where no Go code runs), and the process
// group covers every in-process exit path (a normal return, t.Fatal, a panic —
// testing runs t.Cleanup on all three). A fixture child is therefore put in its
// OWN group by StartFixtureChild, and the shared reaper (mockproc.go) signals
// exactly that group — never a name, never a pattern, never a global scan.

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"syscall"
	"testing"

	"github.com/nick/gopi/internal/daemonlifetime"
)

const (
	// FixtureBindingReportEnv names the file a fixture child writes its binding
	// VERDICT to. It is a file rather than the sub-packages' own readiness FIFO
	// on purpose: every suite already owns a readiness wire with its own token
	// and its own polling discipline, and a shared mechanism must not force a
	// second wire on them. One line, written by the child, read by the parent
	// (Rule 15: an unarmed binding is a fact, and a fact needs a channel).
	FixtureBindingReportEnv = "GOPI_TEST_FIXTURE_BINDING_REPORT"

	// fixtureBindingArmedWord / fixtureBindingUnarmedWord are the verdict
	// vocabulary. The ARMED line carries the child's pid, so the parent gets
	// "it armed" and "which process armed" from the same read — the pid is what
	// a death assertion needs, and asking for it twice would let the two
	// answers disagree.
	fixtureBindingArmedWord   = "armed=true"
	fixtureBindingUnarmedWord = "armed=false"

	// fixtureBindingReportDir is the subdirectory of the suite temp root the
	// reports live in. It is inside the pinned scratch root, never the source
	// tree and never `/` (Rule 22): the report is a fixture artifact like any
	// other, and a test that wrote one into the checkout would be committing
	// litter (Rule 30).
	fixtureBindingReportDir = "fixture-binding"

	// fixtureBindingReportMax bounds the parent's read of that file. One line
	// is written; the cap exists so a child that somehow spewed cannot turn a
	// verdict read into an unbounded read.
	fixtureBindingReportMax = 4 << 10

	// fixtureBindingReportMode is the report's file mode: readable by the
	// parent (the same user), never executable.
	fixtureBindingReportMode = 0o644
)

// reportLine is what a bound fixture child writes: the verdict word plus the
// pid the parent needs for its death assertion. One spelling, used by the
// writer and parsed by the reader (Rule 13: a format with two spellings is a
// format that will disagree with itself).
func fixtureBindingReportLine(armed bool, pid int) string {
	word := fixtureBindingUnarmedWord
	if armed {
		word = fixtureBindingArmedWord
	}
	return fmt.Sprintf("%s pid=%d\n", word, pid)
}

// FixtureBindingReportPath returns the path a fixture child started under
// `name` reports its binding verdict to. The caller passes the path to
// BindFixtureChild (which puts it in the child's environment) and then reads it
// with RequireFixtureChildBinding.
//
// It lives under the suite temp root — the pinned scratch root, off '/' — so a
// report can never land in the checkout (Rule 22 / Rule 30).
func FixtureBindingReportPath(t *testing.T, name string) string {
	t.Helper()
	dir := filepath.Join(SuiteTempRoot(), fixtureBindingReportDir)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("create the fixture-binding report dir %s: %v", dir, err)
	}
	return filepath.Join(dir, name)
}

// BindFixtureChild makes cmd a fixture child bound to THIS process's lifetime —
// the parent-side half, and the only place a fixture child's environment is
// shaped.
//
// It does three things, and each is a way a fixture author gets it wrong alone:
//
//  1. THE DEATH PIPE. The child is told which descriptor to watch, named
//     EXPLICITLY with the number this process holds rather than relying on the
//     variable having been inherited. Values travel with the variable (fd
//     numbers survive exec unless reappeared as ExtraFiles), but an explicit
//     statement is what makes "the child is bound" a property of the spawn
//     rather than of the ambient environment — and the number this process
//     holds is the only one the child can be handed, since F_DUPFD parks it at
//     daemonlifetime.InheritFDFloor, above os/exec's renumbering range.
//  2. THE REPORT PATH, so the child states whether it really bound itself.
//  3. THE PROCESS GROUP, so every in-process exit path (return, t.Fatal,
//     panic) can still reap the whole group through the shared reaper. Setsid
//     already gives a child its own group (and setpgid on a session leader
//     fails with EPERM), so the flag is set only when the caller has not
//     claimed a session — the same reconciliation StartMockProcessCmd makes.
//
// An error means the binding is NOT in force — nothing is armed in this
// process. The caller reports it rather than starting an unbound child
// (Rule 15): a fixture child that outlives its suite is the defect this file
// exists to prevent, and "we could not bind it" must never look like success.
func BindFixtureChild(cmd *exec.Cmd, reportPath string, childEnv ...string) error {
	fdRaw := os.Getenv(daemonlifetime.HoldEnvName)
	if fdRaw == "" {
		return fmt.Errorf("no suite lifetime binding is armed in pid %d (%s is unset) — the fixture child would outlive this process; the suite's TestMain must call testutil.ArmAndSweepForSuite before spawning any fixture",
			os.Getpid(), daemonlifetime.HoldEnvName)
	}
	fd, err := strconv.Atoi(fdRaw)
	if err != nil || fd < 0 {
		return fmt.Errorf("%s=%q is not an fd number — refusing to spawn a fixture child that would not be bound", daemonlifetime.HoldEnvName, fdRaw)
	}
	// The child's environment is handed back in full rather than appended to in
	// place: Go's os.Getenv returns the FIRST match in the block, so appending
	// leaves a value the CALLER already set winning — and the value that must
	// win here is the one this session armed. MEASURED 2026-09-26: the suite
	// exports its hold variable process-wide, so an appended copy sat behind the
	// suite's own and a RE-ARMED holder's child watched the suite's pipe instead
	// of its parent's — the binding was in force and pointed at the wrong
	// process.
	cmd.Env = setFixtureEnv(cmd.Env,
		daemonlifetime.EnvName+"="+fdRaw,
		FixtureBindingReportEnv+"="+reportPath,
	)
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	if !cmd.SysProcAttr.Setsid {
		cmd.SysProcAttr.Setpgid = true
	}
	return nil
}

// setFixtureEnv returns env with each override REPLACING any entry that already
// sets the same variable, plus any extra entries. It is the one place the
// "which value wins" question is answered for a fixture child's environment.
func setFixtureEnv(env []string, overrides ...string) []string {
	names := make([]string, 0, len(overrides))
	for _, o := range overrides {
		name, _, _ := strings.Cut(o, "=")
		if name != "" {
			names = append(names, name)
		}
	}
	out := make([]string, 0, len(env)+len(overrides))
	for _, entry := range env {
		name, _, found := strings.Cut(entry, "=")
		if !found {
			continue
		}
		if slices.Contains(names, name) {
			continue
		}
		out = append(out, entry)
	}
	return append(out, overrides...)
}

// StartFixtureChild is the ONE way a test spawns a fixture child:
// BindFixtureChild (death pipe + report + group) followed by the shared reaper's
// spawn (StartMockProcessCmd), which registers the group-scoped cleanup that
// runs on every in-process exit path.
//
// It returns the reaper handle and the report path. The caller that needs the
// child's own initialisation handshake (a readiness FIFO, a "watchdog armed"
// byte) performs that itself and THEN calls RequireFixtureChildBinding — the
// report can only be read after the child has run its arming code.
//
// A spawn failure kills the test (StartMockProcessCmd's default: a missing
// fixture binary is an environment fault, not a case that quietly proves
// nothing).
func StartFixtureChild(t *testing.T, cmd *exec.Cmd, name string) (*MockProcess, string) {
	t.Helper()
	report := FixtureBindingReportPath(t, name)
	if err := BindFixtureChild(cmd, report); err != nil {
		t.Fatalf("bind fixture child %q: %v", name, err)
	}
	return StartMockProcessCmd(t, cmd), report
}

// FixtureChildEnv returns env with the childEnv overrides this process is
// handling applied on top — the ONE place a fixture child's environment is
// assembled, so a caller cannot regress into a blind append (see BindFixtureChild).
func FixtureChildEnv(env []string, overrides ...string) []string {
	return setFixtureEnv(env, overrides...)
}

// RequireFixtureBinding is the SUITE-side guard: a case that depends on the
// lifetime binding calls it first, so an unarmed suite FAILS the case instead of
// running it and passing for the wrong reason (the child survives the SIGKILL,
// the death-pipe assertion is skipped or — worse — silently satisfied by
// something else).
//
// It is also the check that keeps the arm itself honest: the arm lives in
// ArmAndSweepForSuite, and a suite that stopped routing through it would
// otherwise lose the binding with no test turning red.
func RequireFixtureBinding(t *testing.T) {
	t.Helper()
	if !DaemonLifetimeArmed() {
		t.Fatalf("the suite lifetime binding is NOT armed in pid %d — a fixture child started now would outlive this test binary (%s) and stand in this worktree as a PPID-1 orphan. The suite's TestMain must call testutil.ArmAndSweepForSuite.",
			os.Getpid(), daemonlifetime.HoldEnvName)
	}
}

// RequireFixtureChildBinding is the PARENT-side guard, called after the child's
// own readiness handshake: it reads the child's binding verdict and fails the
// case when the child could not arm its watchdog.
//
// WHY IT IS NOT OPTIONAL. The failure it catches is invisible from the parent's
// side: a child that silently failed to adopt the hold fd runs perfectly, does
// everything the test asks, and only reveals itself HOURS LATER as an orphan in
// the worktree. The report is the child's own statement, and a missing report is
// a failure too — "the child wrote nothing" is not evidence that it bound
// itself (Rule 15a: the absent value is never the passing value).
func RequireFixtureChildBinding(t *testing.T, report string) int {
	t.Helper()
	raw, err := os.ReadFile(report)
	if err != nil {
		t.Fatalf("fixture child reported no binding verdict (%s): %v — an unarmed child is the leak this guard exists for", report, err)
	}
	pid, why := fixtureChildBindingVerdict(raw)
	if why != "" {
		t.Fatalf("fixture child is NOT bound to this test binary's lifetime: %s (its own verdict: %q) — refusing the case rather than passing for the wrong reason",
			why, strings.TrimSpace(string(raw)))
	}
	return pid
}

// fixtureChildBindingVerdict is the ONE reader of a fixture child's binding
// verdict: it returns the child's pid, or a non-empty reason the verdict is a
// REFUSAL. The wait loop and the guard both call it, so the two cannot drift
// into disagreeing about what "bound" means (Rules 13/14) — and "the child
// wrote nothing" is never evidence that it bound itself (Rule 15a).
func fixtureChildBindingVerdict(raw []byte) (pid int, why string) {
	if len(raw) > fixtureBindingReportMax {
		raw = raw[:fixtureBindingReportMax]
	}
	line := strings.TrimSpace(string(raw))
	if line == "" {
		return 0, "the report is empty — the child wrote no verdict"
	}
	if !strings.Contains(line, fixtureBindingArmedWord) {
		return 0, "the child reported itself UNARMED"
	}
	n, ok := fixtureBindingReportPid(line)
	if !ok {
		return 0, "the verdict names no pid — the case cannot assert the child's death without it"
	}
	return n, ""
}

// fixtureBindingReportPid parses the pid out of a report line. A missing or
// unparsable pid reports false, and the caller FAILS on it: the pid is the
// subject of the assertions this report exists to enable.
func fixtureBindingReportPid(line string) (int, bool) {
	const field = "pid="
	i := strings.Index(line, field)
	if i < 0 {
		return 0, false
	}
	rest := line[i+len(field):]
	end := strings.IndexAny(rest, " \t")
	if end >= 0 {
		rest = rest[:end]
	}
	n, err := strconv.Atoi(rest)
	if err != nil || n <= 0 {
		return 0, false
	}
	return n, true
}

// WatchFixtureBinding is the CHILD-side entry point, and the whole of what a
// fixture child body must do: adopt the inherited death pipe, REPORT the
// verdict, and arm the production watchdog.
//
// It is called from a TestMain guard (or any child entry) BEFORE the child
// announces readiness, so the parent's first read of the report already carries
// the answer. The order matters: the report must not be written before the
// watchdog is armed, or the parent could see "armed=true" for a child whose
// watchdog never installed (register-then-announce, the same discipline the
// sub-packages' readiness FIFOs use).
//
// A non-nil error means the binding is NOT in force. The child REPORTS it (the
// stderr line is the channel a human operator reads, the verdict file the one
// the parent reads) and the caller must FAIL the case rather than continue: an
// unbound fixture child is exactly the leak. It never returns an error just
// because NO binding was inherited from a production start — that case is
// reported as unarmed, not as a fault (the caller knows whether it asked for
// one).
func WatchFixtureBinding() error {
	armed, err := daemonlifetime.AdoptInheritedHoldFD()
	if err == nil && armed {
		armed = daemonlifetime.WatchFromEnv()
		if !armed {
			err = fmt.Errorf("the inherited lifetime descriptor could not be validated as the death pipe — the watchdog did NOT arm")
		}
	}
	writeFixtureBindingReport(armed)
	if err != nil {
		return err
	}
	if !armed {
		return fmt.Errorf("no lifetime binding was inherited (%s and %s are both unset) — this fixture child would outlive its owning test binary",
			daemonlifetime.HoldEnvName, daemonlifetime.EnvName)
	}
	return nil
}

// writeFixtureBindingReport states the verdict in the two channels a fixture
// child has: stderr (what an operator running the suite reads, and what a
// sub-package's own diagnostics sit beside) and the report file the parent
// reads. Both are written, always — never only on the happy path (Rule 15b).
func writeFixtureBindingReport(armed bool) {
	line := fixtureBindingReportLine(armed, os.Getpid())
	fmt.Fprintf(os.Stderr, "testutil: fixture child %spid %d: %s\n",
		fixtureBindingPrefix(armed), os.Getpid(), strings.TrimSpace(line))
	path := os.Getenv(FixtureBindingReportEnv)
	if path == "" {
		return
	}
	if err := os.WriteFile(path, []byte(line), fixtureBindingReportMode); err != nil {
		// The parent's guard turns a missing report into a failure, so the
		// write failing is already visible — but it is REPORTED here too, with
		// the path, because this line is what tells a reader which of the two
		// (the child never armed, or the report never landed) happened.
		fmt.Fprintf(os.Stderr, "testutil: fixture child pid %d: cannot write the binding verdict to %s: %v\n", os.Getpid(), path, err)
	}
}

// fixtureBindingPrefix is the word that leads the stderr line, so a reader can
// tell a bound child from an unbound one at a glance in a noisy test log.
func fixtureBindingPrefix(armed bool) string {
	if armed {
		return ""
	}
	return "NOT BOUND — "
}

const (
	// fixtureProcStatFirstTailField is the proc(5) 1-based number of /proc/<pid>/
	// stat's first field AFTER the parenthesised comm — i.e. the state letter. It
	// is named because the arithmetic below (which field index a proc(5) field
	// number lands on once the comm is stripped) is exactly the kind of
	// hand-counting that silently reads the wrong column.
	fixtureProcStatFirstTailField = 3
	// fixtureProcStatExitCodeField is proc(5)'s field 52: the exit code the
	// kernel recorded for a process that has already exited (its low byte is the
	// wait status, so 0 means "exited cleanly"). It is what turns "the fixture
	// child is not there" into a cause.
	fixtureProcStatExitCodeField = 52
)
