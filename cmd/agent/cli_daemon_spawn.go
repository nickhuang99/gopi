package main

// cli_daemon_spawn.go — the SHARED self-daemonization seam for gopi's resident
// faces: `gopi worker --daemon` (the merger's daemon face is RETIRED).
//
// WHY THIS FILE EXISTS: the detach mechanism was born on the merger face
// (epic merger-daemon-flag, 2026-09-12, cli_merger_daemon.go). `gopi worker
// --daemon` needed the SAME mechanism — a caller (terminal or agent shell) must
// be able to start a resident worker without blocking on it — and a second
// COPY of fork/exec-self + setsid + stdio redirect + pipe handshake is exactly
// the drift the request forbids ("reusing the SAME daemonization mechanism
// ... rather than a second copy"). So the mechanism lives HERE, once, and the
// two faces supply only what genuinely differs (daemonSpec below).
//
// WHAT THE SEAM DOES (each step is load-bearing — the merger devdoc's §3/§4
// remains the reference for the reasoning):
//
//  1. VALIDATE FIRST (spec.prep): the parent rejects a bad argv BEFORE
//     anything is detached, so `--daemon` with a broken flag is a plain
//     synchronous non-zero exit and nothing is ever spawned. A child that
//     failed after the fork would have already been reported "started".
//  2. LOG FIRST: the child's stdout/stderr is redirected into the daemon's
//     log file BEFORE the fork, so the daemon's first line — and any
//     pre-handshake failure — is captured where the operator is told to look.
//  3. CONTROL FD (optional, spec.control): the face's single-instance flock is
//     acquired by the PARENT and inherited by the child via ExtraFiles, so a
//     refusal is synchronous and there is no post-fork re-race. BOTH current
//     faces have one — the worker's <gopi-dir>/worker.lock and the merger's
//     <gopi-dir>/serve.lock (separate files, so they never refuse each other).
//     control == nil is still supported by the mechanism (the child then
//     inherits nothing but the handshake pipe, hence daemonReadyChildFDSolo),
//     but NO face passes it: the worker passing nil was the PRE-singleton
//     contract, superseded by the worker-daemon-singleton epic — do not restore
//     it.
//  4. SETSID: the child becomes a session leader (no controlling terminal, no
//     SIGHUP, survives the caller's shell) — no external `setsid` wrapper.
//  5. PIPE HANDSHAKE: the parent's read of ONE byte is the START PROOF. The
//     child writes it only after it is provably running; on failure it writes
//     "FAIL: <reason>" and exits non-zero. No path returns 0 without positive
//     proof (Rule 15: a failed daemon start is never a silent success), and a
//     failed start SIGTERMs + reaps the child so no orphan is left behind.
//
// WHAT IT DELIBERATELY DOES NOT DO: it never signals by pattern (the operator
// has live merger/worker daemons on this host — 04:12 incident), and it never
// changes the FACE's own semantics: whether the daemon is resident or drains
// and self-exits is the face's business (the worker's drain-and-self-die
// contract is untouched by the flag).

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/nickhuang99/gopi/internal/borrowedfd"
	"github.com/nickhuang99/gopi/internal/cgroupscope"
	"github.com/nickhuang99/gopi/internal/config"
	"github.com/nickhuang99/gopi/internal/daemoncwd"
	"github.com/nickhuang99/gopi/internal/daemonlifetime"
	"github.com/nickhuang99/gopi/internal/runcmd"
	"github.com/nickhuang99/gopi/internal/scratch"
	"github.com/nickhuang99/gopi/internal/worktreerelease"
)

// daemonChildExecutableEnv is the launcher convention that publishes the REAL
// gopi binary to its children — internal/runcmd.EnvBinary, referenced rather
// than re-typed (Rules 13/14). It is consulted FIRST when the seam picks the
// binary a detached daemon re-execs, because inside `go test` the process's own
// executable is the TEST binary (see daemonChildExecutable).
const daemonChildExecutableEnv = runcmd.EnvBinary

// daemonChildIsTestBinaryFmt refuses a start whose re-exec target is a Go test
// binary. The wording names the measured mechanism rather than the symptom: the
// parent does NOT "lose" the readiness byte, it never had a daemon to write one.
//
// MEASURED 2026-10-04 (epic daemon-inprocess-spawn), in-process
// `workerDaemonSpawnErr(nil)` from `go test ./cmd/agent` with the escape on rung
// ①: the spawned child (pid 4091424, ppid = the test process) had cmdline
// `<builddir>/b001/agent.test worker`, fd 3 → worker.lock and fd 4 →
// pipe:[55270799] (so ExtraFiles DO survive the systemd-run wrapper), a cgroup
// inside the fresh scope unit, and a daemon log holding the cmd/agent SUITE's
// own output — `testutil: suite "cmd-agent": …`, chat-case transcripts. Its
// main() is Go's generated test main, so it runs TestMain and then the whole
// package suite: workerDaemonNoteReady is never reached, and the parent waits
// out daemonHandshakeWait (30s) before reporting "did not report readiness".
const daemonChildIsTestBinaryFmt = "%s: refusing to start a detached daemon from a Go test binary: " +
	"the child would re-exec %s, whose main() is Go's generated test main — it would run the test " +
	"suite instead of the daemon face and never write the readiness byte (the parent then waits out " +
	"the %s bound and reports it as a failed start). Publish the real binary instead: " +
	"`%s=<path to gopi>` %s"

// daemonChildExecutable resolves the binary a detached daemon re-execs: the
// launcher's published binary when it named one (daemonChildExecutableEnv),
// else this process's own executable — and it REFUSES a Go test binary, because
// that is not the CLI and can never answer the handshake.
//
// WHY THIS FUNCTION EXISTS (the defect it fixes, measured 2026-10-04): the seam
// used `os.Executable()` unconditionally. In production that is the gopi binary,
// so `main()` dispatches the child argv and the daemon face runs; inside `go
// test` it is `<pkg>.test`, and the re-exec'd child then runs the SUITE. The
// parent cannot tell the difference from the outside — it holds a live child, a
// healthy pipe, and an empty read — so the only bounded thing it can do is wait
// out the whole handshake budget and call a start that was never possible a
// timeout. The refusal converts that into an immediate, named verdict, which is
// the honest answer (Rule 15: a failed start names its cause) and the one an
// in-process caller can act on.
//
// THE PREDICATE IS THE SUFFIX, NOT `testing.Testing()`: a re-exec'd child is a
// real subprocess where Testing() reports false — the same two-instrument
// reasoning internal/tools' finish-cleanup fence documents — and the resolved
// path is what actually gets exec'd, whether it came from the environment or
// from os.Executable(). worktreerelease.TestProcessExecutableSuffix is the ONE
// definition of that spelling (Rules 13/14), referenced, never re-typed.
//
// A PATH THAT CANNOT BE RESOLVED IS NOT A REFUSAL IN DISGUISE: os.Executable's
// own failure is returned as itself, so the caller reports the real cause.
func daemonChildExecutable(prog string, args []string) (string, error) {
	exe := strings.TrimSpace(os.Getenv(daemonChildExecutableEnv))
	if exe == "" {
		self, err := os.Executable()
		if err != nil {
			return "", err
		}
		exe = self
	}
	if strings.HasSuffix(exe, worktreerelease.TestProcessExecutableSuffix) {
		return "", fmt.Errorf(daemonChildIsTestBinaryFmt,
			prog, exe, daemonHandshakeWait, daemonChildExecutableEnv, runcmd.Command(append([]string{prog}, args...)))
	}
	return exe, nil
}

// --- named constants (PROJECT.md Rule 1: one place to find and change) ---

const (
	// daemonFlagLong is the ONE flag that turns a resident gopi face into a
	// detached background daemon. Both faces' parsers, their usage text and
	// their re-exec argv builders reference this constant, so the spelling
	// exists exactly once.
	daemonFlagLong = "--daemon"
	// daemonFlagValueSep is the `--flag=VALUE` separator both spellings of the
	// flag are recognized through (a bare "=" is never written).
	daemonFlagValueSep = "="
	// daemonReadyEnv carries the CHILD-side fd number of the handshake pipe.
	// The parent writes the number the child WILL see (os/exec renumbers
	// ExtraFiles: the first entry is fd 3, the second fd 4), never the number
	// the parent itself held — that distinction was a real bug (`bad file
	// descriptor` in the child while the parent timed out waiting).
	daemonReadyEnv = "GOPI_DAEMON_READY_FD"
	// daemonMarkerValue is the value a component's child-marker env is stamped
	// with. A named value (not "1") so the marker is self-describing in
	// `ps eww` / /proc/<pid>/environ.
	daemonMarkerValue = "1"
	// daemonReadyByte is the single byte the child writes once it is provably
	// running. One byte on purpose: the parent's read returns the instant it
	// lands — "ok\n" would need a delimiter scan for no gain.
	daemonReadyByte = "R"
	// daemonFailPrefix prefixes the ONE line a failed child writes on the
	// handshake fd before exiting non-zero: the failure travels to the
	// caller's stderr rather than vanishing into a log the caller has not
	// been told about yet.
	daemonFailPrefix = "FAIL: "
	// daemonHandshakeWait bounds how long the parent waits for the child's
	// ready byte before declaring the start failed. A healthy child answers in
	// milliseconds; the budget exists so a wedged child leaves the CALLER
	// responsive instead of hanging forever.
	daemonHandshakeWait = 30 * time.Second
	// daemonExitGrace is how long the parent waits for a failed child to
	// finish exiting — purely to reap it (no zombie); a child that outlives it
	// is reported, never hunted down.
	daemonExitGrace = 5 * time.Second
	// daemonChildReapWait bounds the SUCCESSFUL start's reap goroutine: that
	// goroutine returns the instant the daemon exits, so this is a cover for a
	// child that never exits, not a deadline the start waits on.
	//
	// WHY THE SUCCESS PATH REAPS AT ALL. A daemon is started detached so it
	// outlives its LAUNCHER, but "outlive the launcher" is a statement about the
	// process TREE, not about this one call: a caller that stays alive after the
	// start — a launcher that starts two daemons, a REPL, and above all a TEST
	// BINARY, which outlives every start it makes — remains the child's parent,
	// and until somebody calls Wait the child's exit leaves a permanent zombie
	// (`state=Z`; `/proc/<pid>/comm` still reads `gopi` while `cmdline` and
	// `exe` are empty) in the process table, counted by every comm-based process
	// census on the host (measured 2026-10-13, task 01a132b0: the cmd/agent
	// suite's own child sweep named it `leaked child pid=… state=Z`, and
	// `scripts/gopi-worker-up.sh --count` reports it as an "other gopi
	// process").
	//
	// The reap is DECOUPLED from the start: it runs in its own goroutine, so the
	// caller's verdict is never delayed and nothing about the start's
	// asynchronous contract changes. Zombie-ness belongs to the parent-child
	// relation, and this call is the parent, so this call owns the reap.
	daemonChildReapWait = 10 * time.Minute
	// daemonHandshakeBufSize bounds the failure-line read (a diagnosis, not a
	// stream).
	daemonHandshakeBufSize = 256
	// daemonLogTailBytes bounds the log tail a start-failure verdict quotes.
	// The tail is the DIAGNOSIS (a systemd-run refusal, a lock refusal, a usage
	// dump), and the last lines hold the cause while the head holds the banner
	// — 2 KiB is several failure lines without pasting a whole log into the
	// caller's terminal.
	daemonLogTailBytes = 2048
	// daemonNoReportNote is the verdict's headline when the child died before
	// the handshake byte. It replaced the bare "(see the log): EOF" phrasing,
	// which named a transport event instead of the cause (Rule 15).
	daemonNoReportNote = "daemon exited during startup without reporting"
	// daemonLogTailFmt / daemonLogEmptyNote / daemonLogUnreadableNote render the
	// quoted cause. An unreadable log is REPORTED, never rendered as an empty
	// tail (which would read as "the log held nothing").
	daemonLogTailFmt        = "--- %s (tail) ---\n%s"
	daemonLogEmptyNote      = "--- %s is EMPTY: the child died before writing anything ---"
	daemonLogUnreadableNote = "--- %s could not be read: %v ---"
	// daemonControlChildFD / daemonReadyChildFDCtrl are the CHILD-side fd
	// numbers when the child inherits a control fd (spec.control != nil): the
	// first ExtraFiles entry is fd 3, the second fd 4. A face WITHOUT a
	// control fd passes daemonReadyChildFDSolo — the pipe is then the first
	// (and only) ExtraFiles entry, i.e. fd 3.
	daemonControlChildFD = 3
	// daemonReadyChildFDCtrl is the handshake fd when a control fd precedes it.
	daemonReadyChildFDCtrl = 4
	// daemonReadyChildFDSolo is the handshake fd when it is the only entry.
	daemonReadyChildFDSolo = 3
	// daemonLogDirName is the directory (under the gopi data root) a detached
	// daemon's stdio is redirected into.
	daemonLogDirName = "logs"
	// daemonLogFileMode / daemonLogDirMode are the log's modes.
	daemonLogFileMode = 0o644
	daemonLogDirMode  = 0o755
	// daemonArgv0Prefix prefixes the component name to form the detached
	// process's argv[0] ("gopi-merger", "gopi-worker"). Deliberately NOT the
	// parent's argv[0]: a detached process should not advertise an ephemeral
	// build path (a test binary in a temp dir that is gone before anyone reads
	// `ps`), and a stable name is what makes "is a merger/worker running?"
	// answerable with `ps`.
	//
	// It is an ALIAS of daemoncwd.Argv0Prefix, never a second literal: the
	// reclaim arm recognises a daemon HOLDER by exactly this prefix when it
	// reports why a worktree is in use (daemoncwd.IsDaemonArgv0), so two
	// spellings would silently stop agreeing about which processes are
	// immortal (Rules 1b/13/14).
	daemonArgv0Prefix = daemoncwd.Argv0Prefix
	// daemonStartNote / daemonStartedNote / daemonFailNote are the parent's
	// three lines: what it is about to do, the success report (pid + log path,
	// so the operator can stop it by EXPLICIT pid and read it), and the
	// failure report.
	daemonStartNote   = "%s: starting a detached %s daemon (%s)\n"
	daemonStartedNote = "%s daemon started: pid %d, log %s\n"
	daemonFailNote    = "%s: daemon start failed: %v\n"
	// daemonReapFailedNote / daemonReapStaleNote are the two ways the success
	// path's reap never completes. Both name the pid, because the pid is the
	// thing an operator has to look at: the first is a Wait that failed for a
	// reason other than the daemon's own exit status (so the status may still be
	// pending), and the second is a daemon still running when the reap's cover
	// elapsed. Neither is silent — a reap that quietly gave up would leave the
	// exact zombie this seam exists to prevent, without a word (Rule 15).
	daemonReapFailedNote = "%s: reap of started daemon pid %d failed: %v\n"
	daemonReapStaleNote  = "%s: started daemon pid %d is still running after %s — the reap goroutine gives up; its exit is NOT collected by this process\n"
	// daemonCwdMeasuredNote / daemonCwdUnreadableNote report the child's
	// MEASURED working directory (read back from /proc/<pid>/cwd after the
	// handshake — epic daemon-cwd-escape). The measurement is the evidence an
	// operator can re-check by hand with the pid this line names; an
	// unreadable cwd is reported, never passed over in silence (Rule 15).
	daemonCwdMeasuredNote   = "%s daemon cwd: pid %d stands in %s\n"
	daemonCwdUnreadableNote = "%s daemon cwd: pid %d cwd unreadable (check `ls -l /proc/%d/cwd`)\n"
)

// daemonSpec is everything that DIFFERS between two self-detaching faces. The
// mechanism (daemonSpawn) is shared; a face supplies its vocabulary, its
// validation, its child argv and its log destination — nothing else.
type daemonSpec struct {
	// component is the daemon's short name: it forms argv[0]
	// (daemonArgv0Prefix+component) and the parent's notes ("<component>
	// daemon started: pid …").
	component string
	// prog is the user-facing program name the diagnostics are prefixed with
	// (e.g. "gopi worker") — the same spelling the face's own messages use.
	prog string
	// markerEnv is the env var that identifies the re-exec'd child. It is
	// ABSENT from the parent's environment (the parent only ever writes it
	// into the child's), so its presence is the child's proof of identity: a
	// user typing the flag twice can never be mistaken for the daemon.
	markerEnv string
	// prep validates the parent's argv BEFORE anything is detached (step ①).
	prep func(args []string) error
	// control, when non-nil, acquires the fd the child inherits as
	// daemonControlChildFD — the face's single-instance flock (the worker's
	// worker.lock via acquireWorkerLock, the merger's serve.lock via
	// acquireServeLock). Both faces pass it; nil remains supported by the
	// mechanism (the child then inherits nothing but the handshake pipe), but
	// no face uses it — the worker's nil was the PRE-singleton contract,
	// superseded by the worker-daemon-singleton epic.
	control func() (*os.File, error)
	// controlWait, when non-nil, replaces the plain control acquisition with a
	// BOUNDED WAIT for it (epic merger-stop-immediate). The merger passes it
	// because a stop/restart is now a REQUEST the old daemon answers at its own
	// pass boundary: the successor cannot take the flock the instant it starts,
	// and refusing it would make `--restart` fail for doing exactly what it was
	// asked to do. The wait is the HANDOVER primitive — the ordering lives in
	// the flock, so there is no sleep and no "quiet window" to tune.
	//
	// It is used only when noWait is false; see noWait for why the distinction
	// exists (the wait must not apply to a plain start).
	controlWait func() (*os.File, error)
	// noWait, when true, forces the IMMEDIATE control acquisition even though
	// controlWait is set: the caller is not a handover, so a held lock is a
	// genuine "already running" and waiting for it would be a slow, silent
	// refusal. The merger sets it whenever the argv begins with an explicit
	// action (`--stop`/`--restart`), because THOSE are the commands that asked
	// for a transfer; a bare `--daemon` is not.
	noWait bool
	// readyChildFD is the fd number the CHILD will see for the handshake pipe:
	// daemonReadyChildFDCtrl with a control fd, daemonReadyChildFDSolo without.
	readyChildFD int
	// childArgv builds the child's argv, argv[0] included (the flag is
	// consumed by construction — the child is identified by markerEnv).
	childArgv func(args []string) []string
	// startDetail renders the parent's start-note detail; it must not LIE
	// about what the child will do.
	startDetail func(args []string) string
	// logPath resolves where the detached daemon's stdio lands (GOPI_HOME
	// aware, like every other gopi-data path).
	logPath func() (string, error)
}

// daemonFlagRequested reports whether argv carried the daemon flag, in either
// spelling ("--daemon" or "--daemon=<value>", which the CLI flag parser would
// otherwise read as a value-carrying flag). ONE predicate for both faces.
func daemonFlagRequested(args []string) bool {
	for _, a := range args {
		if a == daemonFlagLong || strings.HasPrefix(a, daemonFlagLong+daemonFlagValueSep) {
			return true
		}
	}
	return false
}

// daemonLogPathUnder resolves <gopi data root>/logs/<baseName> — the shell
// every face's logPath uses, so the location convention (and its GOPI_HOME
// awareness) has one definition.
func daemonLogPathUnder(baseName string) (string, error) {
	gd, err := config.GopiDir()
	if err != nil {
		return "", fmt.Errorf("resolve gopi dir: %w", err)
	}
	return filepath.Join(gd, daemonLogDirName, baseName), nil
}

// daemonSpecArgs0 returns the child argv's argv[0] for a component — the
// stable, readable process name a detached daemon advertises.
func daemonSpecArgv0(component string) string {
	return daemonArgv0Prefix + component
}

// daemonSpawn detaches the face described by spec and reports whether it
// started. See the file header for the numbered steps; each one is
// load-bearing and none of them may be reordered.
//
// It RETURNS the failure instead of exiting: the caller decides what a refused
// start MEANS. That distinction is the whole idempotency contract of the
// worker's --restart (epic worker-graceful-drain): a spawn refused because a
// CONCURRENT restart already won the single-instance flock is not a failure —
// the requested end state (exactly one fresh daemon) holds either way, and
// exiting 1 there would report a failure that did not happen.
//
// Its FIRST result is whether the requested end state was reached WITHOUT
// starting anything: true means the existing-unit decision found this profile's
// own live daemon already holding the daemon's scope unit (a reported no-op,
// never a failure). The distinction reaches the caller because the worker's
// --restart must record a state file for the daemon it did not start — a no-op
// it treated as an ordinary failure would leave the operation with no state to
// report (see workerControlStartDaemon).
func daemonSpawn(spec daemonSpec, args []string) (reused bool, err error) {
	// ① Validate in the PARENT, before anything is detached (Rule 15): the
	// child parses its own argv too, but its failure would arrive AFTER the
	// handshake — the caller would have been told "started" and the daemon
	// would die a moment later.
	if err := spec.prep(args); err != nil {
		return false, err
	}
	// ①b THE PROFILE GUARD, still before anything is opened or detached
	// (epic daemon-spawn-profile-guard): a daemon whose profile nobody NAMED
	// would fall through to the operator's real ~/.gopi and hold their
	// worker.lock for a lifetime nothing owns (measured 2026-10-04, pid
	// 639670). Refusing HERE means the refusal takes no lock, creates no
	// directory and detaches nothing — see cli_daemon_profile_guard.go.
	if err := guardDaemonProfile(spec); err != nil {
		return false, err
	}
	// ② The log, opened before the fork: the child's very first line — and any
	// pre-handshake failure — lands in the file the success line names.
	logPath, err := spec.logPath()
	if err != nil {
		return false, err
	}
	if err := scratch.MkdirAllWithSuiteRunOwnerPerm(filepath.Dir(logPath), daemonLogDirMode); err != nil {
		return false, fmt.Errorf("create daemon log directory %s: %w", filepath.Dir(logPath), err)
	}
	logFile, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, daemonLogFileMode)
	if err != nil {
		return false, fmt.Errorf("open daemon log %s: %w", logPath, err)
	}
	defer logFile.Close()

	// ③ The control fd (if this face has one), taken by THIS process — the one
	// the caller is waiting on — and inherited by the child. A start that
	// cannot get it fails HERE, visibly, before anything is detached.
	var control *os.File
	switch {
	case spec.controlWait != nil && !spec.noWait:
		// A WAITING acquisition owns its own timeout/refusal reporting; it
		// still returns an error for anything that is not "the predecessor is
		// finishing" (an unusable lock file, an ambiguous holder), which the
		// caller reports as the ordinary start failure.
		control, err = spec.controlWait()
		if err != nil {
			return false, err
		}
	case spec.control != nil:
		control, err = spec.control()
		if err != nil {
			return false, err
		}
	}
	// ③h THE RE-EXEC TARGET, resolved and REFUSED here — after the flock (so a
	// start that could not even win the singleton lock still reports THAT
	// refusal, the cause an operator is actually looking for) and BEFORE the
	// pipe, the cwd escape and the fork, so a refused start leaves no process
	// and no pipe behind.
	//
	// A Go test binary is not the CLI: handing it the child argv runs the
	// package's SUITE instead of the daemon face, and the child then never
	// reaches workerDaemonNoteReady — which the parent can only observe as an
	// empty pipe, i.e. after waiting out the whole daemonHandshakeWait budget
	// (see daemonChildExecutable: the measured 2026-10-04 defect this refusal
	// fixes). childArgv is built here too, so the refusal can name the command
	// nobody started.
	childArgv := spec.childArgv(args)
	exe, err := daemonChildExecutable(spec.prog, args)
	if err != nil {
		return false, err
	}
	// ③b THE CGROUP ESCAPE, decided before anything is detached (epic
	// daemon-cgroup-detach). The decision is a real probe of the two mechanisms
	// (see cli_daemon_scope.go) and it is REPORTED: a detach that could not
	// leave the login session scope is announced, never silently presented as a
	// full detach. --require-scope turns that announcement into a refusal.
	//
	// The announcement is made AFTER the start, from the child's MEASURED
	// /proc/<pid>/cgroup (daemonScopeMeasureNote) — not here, before the fork.
	// The parent's probe is a PREDICTION, and a prediction printed as the
	// verdict is how an operator is told "it did not escape" about a daemon that
	// did.
	scope := daemonScopeDecide(args)
	if scope.required && !scope.scopeEscaped() {
		fmt.Fprintf(os.Stderr, daemonScopeRequiredNote, spec.prog, daemonScopeRequireFlag, scope.detail)
		// The refusal is where the operator most wants the remedy, so it also
		// carries the MEASURED state behind rung ① — the blended reason alone
		// covers three host states with three different repairs (epic user-bus
		// diagnosis, 2026-09-15).
		daemonUserBusReport(spec.prog, scope.userBus)
		// And the measured answer to the question that state raises (a hard
		// failure must not send the reader looking for a privilege that would
		// not have helped — the same correction the degraded start prints).
		fmt.Fprintf(os.Stderr, daemonScopeNoPrivilegeNote, spec.prog)
		return false, fmt.Errorf("%w: %s", cgroupscope.ErrNoEscape, scope.detail)
	}

	readyR, readyW, err := os.Pipe()
	if err != nil {
		return false, err
	}
	child := exec.Command(exe, childArgv[1:]...)
	child.Args = childArgv
	// ③e THE DAEMON'S CWD, decided here and MEASURED (epic daemon-cwd-escape,
	// 2026-09-16). exec.Command with no Dir hands the child THIS process's
	// working directory — and a daemon's lifetime is unbounded, so a launcher
	// that stands in a task's linked worktree (the normal case: an agent
	// session running in its own worktree spawns the merger/worker) pins that
	// worktree forever. Guard ③ of the reclaim arm then refuses to recycle it
	// correctly, and can never stop refusing, because the holder never exits.
	//
	// The producer's obligation is therefore to hand every daemon a directory
	// that is provably outside every task worktree AND measured off the `/`
	// partition (Rule 22 — a measured device id, never a spelling): the
	// scratch tree's process root, or procfs when the scratch tree cannot be
	// proven. The escape is REPORTED (Rule 15) and the child's measured cwd is
	// re-read from /proc after the handshake (daemonCwdMeasureNote), so the
	// claim is never merely "a field was assigned".
	daemonCwd, daemonCwdEscaped, cwdErr := daemoncwd.Resolve(launcherWorkdir(), "")
	if cwdErr != nil {
		// No provable root: keep the inherited directory and SAY so, rather
		// than inventing a path (Rule 22) — the operator needs to know this
		// daemon may pin a worktree.
		fmt.Fprintf(os.Stderr, daemoncwd.FallbackFmt, spec.prog, launcherWorkdir())
	} else {
		child.Dir = daemonCwd
		daemoncwd.Report(spec.prog, daemonCwd, daemonCwdEscaped)
	}
	child.Env = append(reportContractStripAndLog(os.Environ(), spec.prog),
		spec.markerEnv+"="+daemonMarkerValue,
		daemonReadyEnv+"="+strconv.Itoa(spec.readyChildFD),
	)
	// ③c The escape is applied to the CHILD COMMAND (mechanism ① wraps the argv
	// in systemd-run; mechanisms ②/③ leave it alone) and the mode is stamped
	// into its environment, so the child can measure its own /proc/self/cgroup
	// and say what really happened.
	daemonScopeWrap(child, spec.component, scope)
	// ③d THE EXISTING-UNIT DECISION, before anything is started (epic
	// daemon-scope-per-profile). The unit name is now a function of THIS
	// profile, so "systemd says the name is taken" is a bounded condition with
	// exactly two meanings, and both are decided HERE instead of being
	// discovered as a handshake timeout in the child's log: this profile's own
	// live daemon (the requested end state already holds — nothing is
	// detached, and the reuse is REPORTED by the decision itself), or anything
	// else (REFUSED, naming the unit and the profile). Never a retry: a name
	// held by a live foreign unit does not free itself.
	reused, _, unitErr := daemonScopeResolveExistingUnit(spec, spec.component, scope)
	if unitErr != nil {
		return false, unitErr
	}
	if reused {
		// A no-op start is NOT a failure: the caller must not print the
		// start-failure line for an end state that already holds, and the
		// restart path (which treats a refused start as covered only when
		// another daemon is live) gets the same verdict from it.
		return true, nil
	}
	child.Stdin = nil // a daemon must never consume the caller's stdin
	child.Stdout = logFile
	child.Stderr = logFile
	// ExtraFiles order IS the child's fd numbering (step ③): a control fd, when
	// present, is fd 3 and the handshake pipe follows it — which is exactly
	// what spec.readyChildFD encodes. The death pipe (when a harness armed one)
	// is always appended AFTER those, so its number is derived from the final
	// slice length rather than hand-counted at a second site.
	if control != nil {
		child.ExtraFiles = []*os.File{control, readyW}
	} else {
		child.ExtraFiles = []*os.File{readyW}
	}
	// ③g DAEMON LIFETIME BINDING (opt-in, test-only). Two ways to ask for it:
	//
	// The holder hands over the READ end of a pipe it owns and names its fd
	// INSIDE THIS process (daemonlifetime.HoldEnvName) — for the test suite that
	// fd is inherited unchanged by every descendant, which is what makes one
	// process-wide pipe cover every daemon a test binary starts
	// (cmd/agent/daemon_lifetime_binding.go). The daemon child then receives
	// daemonlifetime.EnvName naming the fd IT must watch, and a watchdog thread
	// in the daemon kills it when that pipe reports the holder's death.
	//
	// A production start sets nothing, so the descriptor list here stays exactly
	// as before and no watchdog is armed (see internal/daemonlifetime).
	death, err := daemonDeathPipe()
	if err != nil {
		return false, err
	}
	if death != nil {
		child.ExtraFiles = append(child.ExtraFiles, death)
		child.Env = append(child.Env,
			daemonlifetime.EnvName+"="+strconv.Itoa(daemonlifetime.ExtraFilesOffset(len(child.ExtraFiles)-1)))
	}
	// ④ setsid: session leader, no controlling terminal — the detach itself.
	// It is NOT the cgroup escape: a session leader still sits in its login
	// session's scope, which is what ③b/③c fix. `systemd-run --scope` is a
	// process in its own right (it is NOT session-leader-safe to setsid the
	// wrapper), so Setsid applies to the direct-exec paths only.
	if scope.mode != daemonScopeModeSysd {
		child.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	}

	fmt.Fprintf(os.Stderr, daemonStartNote, spec.prog, spec.component, spec.startDetail(args))
	fmt.Fprintf(os.Stderr, daemonScopeChosenNote, spec.prog, daemonScopeModeLabel(scope))
	// The chosen-mechanism line above names the BLENDED reason when both rungs
	// failed; the MEASURED host state behind rung ① follows it (epic user-bus
	// diagnosis, 2026-09-15) so the operator can tell "there is no user manager"
	// from "the manager is alive and its socket path was unlinked" — three
	// states, three different repairs. Nothing is printed when the escape
	// worked, or when rung ①' s failure was not a bus failure at all.
	daemonUserBusReport(spec.prog, scope.userBus)
	if err := child.Start(); err != nil {
		return false, err
	}
	// The PARENT must let go of both fds: the child has its own copies, and a
	// parent-held readyW would keep the pipe open forever (the read below
	// would never see EOF on a child that died without writing). A Close error
	// here has no recovery path — the start verdict is the answer that
	// matters, and the fds die with this process either way.
	_ = readyW.Close() //stylegate:ignore fd handed to the child; a close error changes nothing (the verdict decides)
	if control != nil {
		_ = control.Close() //stylegate:ignore the child owns the control fd now; the parent's duplicate is what matters
	}

	// ⑤ The handshake: one byte read is the START PROOF (see daemonAwaitReady).
	verdict := daemonAwaitReady(readyR, logPath)
	_ = readyR.Close() //stylegate:ignore read ended; the pipe is done with, the verdict is already in hand
	if verdict != nil {
		daemonScopeAbandon(child, spec.prog)
		return false, verdict
	}
	// ⑤b THE STARTED CHILD'S REAP. Started only HERE, on the success path: the
	// failure path above reaps through daemonScopeAbandon, and two Waits on one
	// *exec.Cmd is an error, so exactly one of the two paths may own the child.
	// A successful start is the case whose parent stays alive afterwards — and
	// an unreaped child of a live parent is a permanent zombie (see
	// daemonChildReapWait). The goroutine is decoupled from the verdict below,
	// so the start note is never delayed by it.
	daemonReapStartedChild(child, spec.prog)
	fmt.Fprintf(os.Stdout, daemonStartedNote, spec.component, child.Process.Pid, logPath)
	// ③f The measured cwd of the started daemon, read back from procfs. This
	// is the epic's acceptance evidence in production form: the pid the start
	// note just named, and the directory the kernel says it stands in.
	daemonCwdMeasureNote(spec, child.Process.Pid)
	// ③d The cgroup verdict, from the MEASUREMENT (step ③c stamped the child,
	// this reads it back from procfs). It is reported only when the child could
	// NOT escape — the happy path is already covered by the start note above.
	daemonScopeMeasureNote(spec, child.Process.Pid, scope)
	return false, nil
}

// daemonReapStartedChild takes ownership of the STARTED child's exit so its
// parent — which may well outlive it — can never leave a zombie behind.
//
// It is the success-path counterpart of daemonScopeAbandon (which owns the
// failure path), and the two are mutually exclusive by construction: each is
// called on exactly one branch of the handshake verdict, so exactly one
// `Wait` ever reaches one *exec.Cmd.
//
// The goroutine is deliberately detached from the caller: the start's contract
// is asynchronous (the daemon outlives this call), so the reap must not delay,
// and must not be delayed by, the verdict the caller is waiting on. Every exit
// of the goroutine is accounted for (Rule 15b): a normal exit — including a
// non-zero status, which is a daemon's own business — is silent, while an
// abnormal Wait failure or a child that never exits is REPORTED with the pid,
// never swallowed. Reporting it must not resurrect the zombie the reap exists
// to prevent, so the message goes to stderr and the reap simply ends.
func daemonReapStartedChild(child *exec.Cmd, prog string) {
	if child.Process == nil {
		return
	}
	pid := child.Process.Pid
	go func() {
		done := make(chan error, 1)
		go func() { done <- child.Wait() }()
		select {
		case err := <-done:
			var exit *exec.ExitError
			if err != nil && !errors.As(err, &exit) {
				// A Wait that fails for a reason other than the child's own exit
				// status: the status (and so the reap) may not have happened.
				fmt.Fprintf(os.Stderr, daemonReapFailedNote, prog, pid, err)
			}
		case <-time.After(daemonChildReapWait):
			fmt.Fprintf(os.Stderr, daemonReapStaleNote, prog, pid, daemonChildReapWait)
		}
	}()
}

// daemonCwdMeasureNote reads the DETACHED CHILD's working directory back from
// procfs and prints it — the measurement that turns "we set cmd.Dir" into
// "here is where the daemon actually stands" (Rule 15; the acceptance evidence
// of epic daemon-cwd-escape).
//
// It is printed on EVERY start (unlike the cgroup verdict), because the daemon
// cwd is the value an operator must be able to check later with
// `ls -l /proc/<pid>/cwd`, and the pid is exactly what the start note just
// announced. An unreadable cwd is REPORTED as such, never skipped silently.
func daemonCwdMeasureNote(spec daemonSpec, pid int) {
	cwd, ok := daemoncwd.ProcessCwd(pid)
	if !ok {
		fmt.Fprintf(os.Stderr, daemonCwdUnreadableNote, spec.prog, pid, pid)
		return
	}
	fmt.Fprintf(os.Stderr, daemonCwdMeasuredNote, spec.prog, pid, cwd)
}

// launcherWorkdir returns the directory this process would hand a child by
// inheritance (its own cwd). It is the input of the daemon cwd decision and of
// the fallback report, so it is resolved once, here (Rule 2's single reader).
// An unresolvable cwd returns "" — the decision then has no inheritance to
// escape and keeps its measured process root.
func launcherWorkdir() string {
	wd, err := os.Getwd()
	if err != nil {
		return ""
	}
	return wd
}

// daemonSpawnOrExit is the plain-CLI spelling of daemonSpawn: a start the
// operator asked for directly either succeeds or ends the process with the
// named failure. It is what the `--daemon` faces use, so the ordinary
// `gopi worker --daemon` contract (non-zero exit + a line naming the reason) is
// byte-for-byte what it always was.
func daemonSpawnOrExit(spec daemonSpec, args []string) {
	// The reuse outcome needs no handling here: the decision already REPORTED
	// it (daemonScopeReuseReport), the end state the operator asked for holds,
	// and exit 0 is the truthful verdict for it.
	if _, err := daemonSpawn(spec, args); err != nil {
		daemonFail(spec, err)
	}
}

// daemonFail prints the face's start-failure line and exits non-zero — the ONE
// exit path for every failure in daemonSpawn (Rule 15: never a silent success,
// and never a bare exit without a message).
//
// The exit code is the FAILURE's, when the failure carries one
// (daemonProfileRefusalExitCode today): a refusal is its own outcome, and a
// caller/regression test must be able to tell "the guard said no" from "the
// start broke" (the same distinction config.AgentGuardRefusalExitCode draws on
// the agent-spawn guard). Every other failure keeps the generic 1.
func daemonFail(spec daemonSpec, err error) {
	fmt.Fprintf(os.Stderr, daemonFailNote, spec.prog, err)
	var coded interface{ ExitCode() int }
	if errors.As(err, &coded) {
		os.Exit(coded.ExitCode())
	}
	os.Exit(1)
}

// daemonAwaitReady blocks until the child reports success, reports failure, or
// dies without a word — all three bounded by daemonHandshakeWait. nil = the
// daemon is running.
//
// logPath is the START-FAILURE DIAGNOSIS's source: a child that dies before
// the handshake wrote nothing into the pipe, and its own stderr is in the log
// file — so the verdict carries that file's TAIL rather than the bare `EOF`
// the read produced. The bare EOF is exactly the shape that made these starts
// illegible: it reads as a transport accident, while the log held the cause
// ("Failed to start transient scope unit: Unit gopi-daemon-merger.scope
// already exists"). Rule 15: a failed start NAMES its cause.
func daemonAwaitReady(ready *os.File, logPath string) error {
	type readResult struct {
		msg string
		err error
	}
	done := make(chan readResult, 1)
	go func() {
		// One byte is the success proof; more means the child sent a failure
		// line (it writes those BEFORE exiting, so a single Read returns it).
		buf := make([]byte, daemonHandshakeBufSize)
		n, err := ready.Read(buf)
		if n > 0 && string(buf[:1]) == daemonReadyByte {
			done <- readResult{}
			return
		}
		if n > 0 {
			done <- readResult{msg: strings.TrimSpace(string(buf[:n]))}
			return
		}
		done <- readResult{err: err}
	}()
	select {
	case r := <-done:
		if r.msg != "" {
			return errors.New(strings.TrimPrefix(r.msg, daemonFailPrefix))
		}
		if r.err != nil {
			// EOF (or a read error) with no byte = the child died before it
			// could report; its own stderr went to the log, so the verdict
			// quotes what the log holds.
			return fmt.Errorf("%s: %w\n%s", daemonNoReportNote, r.err, daemonLogTailNote(logPath))
		}
		return nil
	case <-time.After(daemonHandshakeWait):
		return fmt.Errorf("daemon did not report readiness within %s\n%s", daemonHandshakeWait, daemonLogTailNote(logPath))
	}
}

// daemonLogTailNote renders the daemon log's tail for a start-failure verdict,
// and NEVER fails: an unreadable/absent log is REPORTED as such (an empty note
// would make the verdict look like a log that was read and held nothing —
// Rule 15b, a swallowed error hides the failure).
func daemonLogTailNote(logPath string) string {
	b, err := os.ReadFile(logPath)
	if err != nil {
		return fmt.Sprintf(daemonLogUnreadableNote, logPath, err)
	}
	tail := strings.TrimSpace(tailString(string(b), daemonLogTailBytes))
	if tail == "" {
		return fmt.Sprintf(daemonLogEmptyNote, logPath)
	}
	return fmt.Sprintf(daemonLogTailFmt, logPath, tail)
}

// daemonDeathPipe returns the descriptor the daemon child must watch for the
// spawner's death (the read end of a death pipe), or nil when the binding is not
// wanted, or an error when it WAS wanted and cannot be provided.
//
// It is the ONE reader of daemonlifetime.HoldEnvName and of the suite flag
// (Rule 2). A binding that was asked for and cannot be provided is an ERROR, so
// the start fails loudly instead of proceeding unbound — a caller that asked for
// the binding must never be told "started" while its daemon can outlive it
// (Rule 15).
// OWNERSHIP: the descriptor is BORROWED. It is the HOLDER's own read end,
// named in this process by the harness's variable (internal/daemonlifetime's
// suite pipe) — this process never opened it and must never close it. The
// wrapper is handed to os/exec for the child (ExtraFiles) and is then DROPPED
// on this process's side: exec does not retain it, cmd.Start does not close
// ExtraFiles (only Process.Release does), so a wrapper left to the garbage
// collector would be closed by its finalizer BEFORE the fork — the holder's
// live descriptor dies, the daemon starts unbound, and that is exactly the
// silent outliving-daemon failure the binding exists to prevent. Both exits
// therefore hand the wrapper to borrowedfd.Detach; the rejection path is the one
// that matters most, since the caller then refuses the start and nothing else
// references the wrapper at all.
//
// THE PRE-cmd.Start WINDOW, measured rather than assumed: the caller appends
// this wrapper to child.ExtraFiles, and os/exec retains that slice on the Cmd
// until Start returns (the slice does not borrow the wrapper — it holds it), so
// the wrapper is REACHABLE and the finalizer cannot run between this function
// returning and the fork. That reachability is a property of the CALLER and does
// not travel with the returned value, so it cannot protect the refusal exits or
// a future caller that stops populating ExtraFiles; the Detach above is the one
// guarantee that does not depend on the caller, which is why it is here and not
// only in the spawn path. A pre-fork child-arm guard for this window was written
// and then REMOVED: since the wrapper is reachable it cannot be driven red, and
// an unprovable guard is worse than a stated reason (Rule 5).
//
// The number is VALIDATED BEFORE it is wrapped, by
// daemonlifetime.ValidateHoldFD — the ONE predicate that owns the death-pipe
// shape (≥ InheritFDFloor, and a live descriptor whose kernel target is a pipe).
// os.NewFile cannot do this check: it returns nil only for a negative number and
// otherwise hands back a wrapper whose Fd() is whatever it was given (MEASURED:
// os.NewFile(9999) is non-nil with Fd()==9999). The old `death == nil ⇒ "not
// usable"` refusal therefore described a state no caller could reach, and a free
// or non-pipe number was appended to ExtraFiles only to fail later inside
// os/exec ("invalid argument") — a precise refusal replaced by a generic failure
// one layer down, which is exactly what Rule 15 forbids.
func daemonDeathPipe() (*os.File, error) {
	if v := os.Getenv(daemonlifetime.HoldEnvName); v != "" {
		fd, err := strconv.Atoi(v)
		if err != nil || fd < 0 {
			return nil, fmt.Errorf("%s=%q is not an fd number — refusing to start a daemon that would outlive its spawner",
				daemonlifetime.HoldEnvName, v)
		}
		// Checked BEFORE wrapping, so a refused number never becomes a wrapper
		// this package would have to own: the verdict names the variable, the
		// number and the kernel's own reading of it (below the floor / not open /
		// not a pipe, quoting the target).
		if _, verr := daemonlifetime.ValidateHoldFD(fd, daemonlifetime.InheritFDFloor); verr != nil {
			return nil, fmt.Errorf("%s=%d is not a usable death pipe — refusing to start a daemon that would outlive its spawner: %w",
				daemonlifetime.HoldEnvName, fd, verr)
		}
		death := os.NewFile(uintptr(fd), daemonlifetime.HoldEnvName)
		if death == nil {
			// Unreachable for a non-negative number (see the doc above), kept as
			// the last line of defence so a future os.NewFile that DID validate
			// could not make this function hand back a nil wrapper.
			return nil, fmt.Errorf("daemon death-pipe fd %d (%s) is not usable: os.NewFile refused the number",
				fd, daemonlifetime.HoldEnvName)
		}
		borrowedfd.Detach(death)
		return death, nil
	}
	return nil, nil
}

// daemonChild reports whether THIS process is a self-detached child of the
// face identified by markerEnv. A function (not a package-level var) so the
// predicate reads the env at the moment of use and stays testable in-process.
func daemonChild(markerEnv string) bool {
	return os.Getenv(markerEnv) == daemonMarkerValue
}

// daemonReadyFD returns the handshake fd this process inherited, or -1 when it
// has none (a child started by hand for debugging). One reader of
// daemonReadyEnv (Rule 2).
func daemonReadyFD() int {
	v := os.Getenv(daemonReadyEnv)
	if v == "" {
		return -1
	}
	n, err := strconv.Atoi(v)
	if err != nil || n < 0 {
		return -1
	}
	return n
}

// daemonNote writes one line to the handshake fd. Rule 15: a daemon that
// cannot start must SAY WHY — to the parent (which surfaces it to the caller)
// and, via the redirected stderr, to the log. With no handshake fd (a
// hand-started child), the line goes to stderr, which is the same log.
// OWNERSHIP: the descriptor is BORROWED (its number arrives through
// daemonReadyEnv, encoded by the launcher — this process never opened it), and
// the wrapper is CLOSED here on every exit (`defer f.Close()`), which is
// correct and needs no detach: os.File.Close clears the finalizer
// (os.(*file).close → runtime.SetFinalizer(file, nil), GOROOT/src/os/file_unix.go),
// so the close is explicit and happens exactly once, on a number this call has
// finished writing. Borrowing a descriptor to write one byte and closing it is
// the intended contract; only a DROPPED wrapper needs borrowedfd.Detach.
func daemonNote(prog string, readyFD int, msg string) {
	if readyFD < 0 {
		fmt.Fprintf(os.Stderr, "%s\n", msg)
		return
	}
	f := os.NewFile(uintptr(readyFD), daemonReadyEnv)
	if f == nil {
		return
	}
	defer f.Close()
	if _, err := f.WriteString(msg); err != nil {
		fmt.Fprintf(os.Stderr, "%s: handshake write failed: %v\n", msg, err)
	}
}
