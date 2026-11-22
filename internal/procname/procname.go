// Package procname is the single source of truth for the PROCESS-NAME facts a
// shell script or a Go test must agree on when it asks the kernel "which of
// these processes is a gopi <verb>?" (Rule 14: a shared value lives in one
// small exported-constant package).
//
// WHY IT EXISTS (the 2026-09-12 worker-up miscount): scripts/gopi-worker-up.sh
// printed "live gopi workers: 13" while `ps` showed 5 workers, because its last
// line was `pgrep -x gopi | wc -l` — a count of EVERY process whose NAME is
// gopi (task-serve daemons, the merger, one-shot `gopi task …` children), not
// of worker processes. The fix counts by argv SHAPE, and it needs three
// strings that must not drift apart:
//
//   - NameGopi — the process NAME field (/proc/<pid>/comm, `ps -o comm`), i.e.
//     the basename of the executed binary. It is the filter that keeps the
//     scan away from every other program on the host, and it is exactly what
//     the old pgrep -x matched, so reusing the same value makes the new count
//     provably narrower than the old one rather than merely different.
//   - VerbWorker — the top-level CLI verb of the worker loop. cmd/agent
//     spells the same value as CliVerbWorker; cmd/agent is package main and
//     cannot be imported, so this leaf package is where both sides read it.
//   - VerbTask — the top-level CLI verb of the `gopi task` group. A task-run
//     invocation re-executes THIS SAME BINARY (`gopi task run <id>` spawns a
//     `gopi` child with inherited stdio, NUL-separated cmdline), so a
//     positional scan without an explicit exclusion would count a one-shot task
//     child as a worker. Naming the verb here is what lets the counter subtract
//     it deliberately and report it as its own line ("N other gopi processes").
//
// The package imports NOTHING but the standard library, so every
// consumer — the launcher script, the test that drives it, and any future Go
// caller — can depend on it without a cycle.
package procname

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

const (
	// NameGopi is the process name of every gopi process: the basename of the
	// gopi binary, as the kernel reports it in /proc/<pid>/comm. A launcher
	// script must reach this value through EnvName rather than typing the
	// literal — Rule 1b.
	NameGopi = "gopi"
	// VerbWorker is the top-level CLI verb that puts a process into the worker
	// loop. It equals cmd/agent's CliVerbWorker.
	VerbWorker = "worker"
	// VerbTask is the top-level CLI verb of the `gopi task` group — the verb
	// whose `run` form re-executes the gopi binary as a one-shot child, the
	// shape a worker scan must subtract rather than count.
	VerbTask = "task"
)

// The BOUNDED RETRY budget for reading /proc/<pid>/cmdline of a process that was
// JUST spawned — one home for a value two packages need (Rule 14: a shared value
// gets exactly one definition, in a leaf every consumer can import).
//
// WHY THE RETRY EXISTS AT ALL (measured 2026-10-12, host=the host):
// exec.Command's Start() returns once the FORK is done, but the child may not
// have reached execve yet. In that window /proc/<pid>/cmdline EXISTS and is 0
// bytes, and the process reads state=R — so ONE read immediately after Start()
// answers "no cmdline" for a process that is perfectly alive. Measured: 9 of 50
// runs of internal/testutil's real-process probe failed on exactly that reading.
// The window closes microseconds after execve (widest measured for this shape:
// 1.58ms), so a bounded re-read closes it with room to spare.
//
// An empty cmdline is genuinely NO EVIDENCE — Cmdline documents it and a zombie's
// cmdline is empty by design — so the READER is right and no assertion may be
// relaxed. The read is retried until the kernel answers; only an exhausted budget
// is a failure, and it names what it saw (Rule 15: never a silent "").
const (
	// CmdlineAwaitAttempts is the bounded retry count for ONE command-line read.
	// A retry count, never a sleep-and-pray: every attempt reads, and the first
	// non-empty answer wins.
	CmdlineAwaitAttempts = 64
	// CmdlineAwaitStep is the pause between retries; attempts × step is the total
	// window the child is given to reach execve (64 × 2ms = 128ms, ~80× the
	// widest window measured).
	CmdlineAwaitStep = 2 * time.Millisecond
)

// Env names — the seam a shell script uses to learn these values without a
// bare literal. The repo's scripts already take their process/command names
// from the environment (GOPI_BIN is the precedent); the test that drives the
// script exports all of them from this package, so each value is written once
// and read twice (Rule 13/14).
const (
	// EnvName is the environment variable carrying NameGopi to a script.
	EnvName = "GOPI_PROC_NAME"
	// EnvVerbWorker is the environment variable carrying VerbWorker.
	EnvVerbWorker = "GOPI_WORKER_VERB"
	// EnvVerbTask is the environment variable carrying VerbTask.
	EnvVerbTask = "GOPI_TASK_VERB"
	// EnvProcRoot is the environment variable that points the counter at a
	// different procfs ROOT — the testability seam: a synthetic tree of
	// <pid>/comm + <pid>/cmdline drives the exact production scan over a
	// fixture the test controls (including argv decoys), instead of asserting
	// against whatever this host happens to be running.
	EnvProcRoot = "GOPI_PROC_ROOT"
	// EnvImagePrefix is the environment variable carrying ImageGopiPrefix to a
	// script. It exists for the same reason EnvName does: the IMAGE prefix is a
	// process fact two sides must agree on exactly (Rule 14), and before it was
	// named, the shell side compared comm to NameGopi — the proxy that made a
	// bin/gopi-fix worker invisible to the count (bug 01a131fcb8337c1b).
	EnvImagePrefix = "GOPI_PROC_IMAGE_PREFIX"
	// EnvWorkerRepo is the environment variable naming the checkout whose
	// bin/<NameGopi> the launcher starts.
	EnvWorkerRepo = "GOPI_WORKER_REPO"
	// EnvWorkerN is the environment variable carrying the worker COUNT — the
	// one number a scheduler hands to the launcher. It is shared deliberately:
	// a second, privately-spelled count in a scheduler is how two launchers
	// disagree about how many workers "N" means.
	EnvWorkerN = "GOPI_WORKER_N"
	// EnvHome is the HOME environment variable — the profile fallback a
	// process's OWN environment carries. A reader that asks "which profile
	// does this process belong to?" (e.g. `gopi worker --status` deciding
	// whether a detached worker in the process table is the caller's) reads
	// it through this name, never as a bare "HOME" literal (Rule 1b/13): the
	// value is a process fact shared by the shell, the Go side and the tests,
	// which is exactly what this leaf package exists for.
	EnvHome = "HOME"
	// EnvStatField is the environment variable carrying the one /proc/<pid>/stat
	// FIELD NUMBER the starttime comparison reads. A field number is a fact a
	// shell and a Go side must agree on exactly as much as a process name is
	// (Rule 1a: never a literal 22 in the middle of a scan loop), so it is
	// declared once here and reaches the launcher through the environment like
	// every other value of this package.
	EnvStatField = "GOPI_PROC_STAT_FIELD"
)

// Worker-liveness facts: the two artifacts/facts that answer "did a worker
// really start?". They live here for the same reason the verb does — a shell
// script, a Go test and cmd/agent (package main, not importable) all have to
// agree on them, and a disagreement is silent (a count that reads zero for
// every good launch, or a grep that matches nothing).
const (
	// WorkerLoopStartLine is the fixed banner a worker prints as its very
	// first act — the fact that says "the loop is running". It is the only
	// launch evidence available for a worker that finds no task to claim in a
	// sandbox: such a worker opens no per-task output log, so a test counting
	// those logs would report zero for a perfectly good launch.
	WorkerLoopStartLine = "worker: loop start"
	// WorkerStartupLogName is the ONE stable file a launcher appends the
	// pre-claim worker output (the loop banner and idle lines) to, under
	// <gopi-dir>/logs/. Fixed on purpose: one record cannot proliferate the way
	// the retired per-worker timestamp-named files did.
	WorkerStartupLogName = "worker-startup.log"
)

// --- procfs layout ---------------------------------------------------------
//
// The path components and field numbers the identity triple reads, named so no
// caller joins a path or indexes a column by hand (Rule 1b/1a).
const (
	// ProcRootDefault is the procfs mount point gopi reads process facts from.
	// It is the definition site — internal/tools and internal/scratch alias
	// THIS constant rather than re-typing "/proc" (Rules 13/14: one definition,
	// referenced by name).
	ProcRootDefault = "/proc"
	// CommName is the /proc/<pid>/ entry holding the process NAME (the comm
	// field — the executable's basename, truncated to 15 bytes by the kernel).
	CommName = "comm"
	// CwdName is the /proc/<pid>/ working-directory LINK: Readlink yields the
	// directory the process is working in. It is the channel the worktree-in-use
	// guard and the scratch holder check both read, so it is declared once here
	// and referenced by name (Rules 13/14).
	CwdName = "cwd"
	// CmdlineName is the /proc/<pid>/ entry holding the NUL-separated argv
	// vector.
	CmdlineName = "cmdline"
	// EnvironName is the /proc/<pid>/ entry holding the process's EXEC-TIME
	// environment block (NUL-separated NAME=VALUE). It is the one channel that
	// survives reparenting: a launcher's own record of who it was stays in its
	// children's block for their whole life, while getppid() renumbers to 1.
	// Readable for a process of the same user, EACCES for a foreign one — which
	// is why every reader treats "unreadable" as "no record", never as a death.
	EnvironName = "environ"
	// StatName is the /proc/<pid>/ entry holding the one-line stat record.
	StatName = "stat"
	// PidDigits is the glob a procfs scan selects process directories with:
	// a name starting with a digit IS a pid directory (`net/`, `sys/` are not).
	PidDigits = "[0-9]*"
	// StatFieldState is /proc/<pid>/stat's 1-based field 3 — the single-letter
	// state ("R" running, "Z" zombie, …).
	StatFieldState = 3
	// StatFieldStartTime is /proc/<pid>/stat's 1-based field 22 — starttime in
	// clock ticks since boot, the INCARNATION of the pid slot. Expressed with
	// the offset so the shell side (scripts/gopi-worker-up.sh, which selects the
	// word "field − StatFieldState + 1" positions after the ')') and this reader
	// are derived from ONE number rather than two literals that can drift.
	StatFieldStartTime = StatFieldState + statFieldStartOffset
	// statFieldStartOffset is how many procfs fields starttime sits after the
	// state letter (field 3): 22 − 3, named so the arithmetic has one home.
	statFieldStartOffset = 19
	// StatFieldParent is /proc/<pid>/stat's 1-based field 4 — the parent pid, the
	// link a caller-ancestry walk follows. It is a FIELD NUMBER like starttime,
	// so it lives here (one definition) rather than as a literal index at the
	// walk's site (Rules 1a/13/14).
	StatFieldParent = 4
	// StatFieldPgrp is /proc/<pid>/stat's 1-based field 5 — the process GROUP
	// id. A caller that must decide whether a process belongs to a group it
	// itself created (a Setpgid leader and its descendants) follows this field;
	// it is a FIELD NUMBER like the others, so it lives here (one definition)
	// rather than as a literal index at the caller's site (Rules 1a/13/14).
	StatFieldPgrp = 5
	// StatFieldSession is /proc/<pid>/stat's 1-based field 6 — the session id
	// (pid == session means a session leader, i.e. detached from a controlling
	// terminal). Named here for the same reason as the two above.
	StatFieldSession = 6
	// ChildrenName and TaskName name the per-THREAD directory of a process and
	// the file inside it that lists that thread's DIRECT CHILDREN. The kernel
	// maintains both, so "which processes did this one start?" is a read rather
	// than a guess — the channel a reader uses to find a launcher's children
	// when the launcher is not `gopi` itself (measured on this repo's classifier
	// leg, where the shell the gopi child starts in is named `sh` and the
	// process tree under the launcher is the only place the real child appears).
	TaskName = "task"
	// ChildrenName is the children file inside a thread directory.
	ChildrenName = "children"
	// StatFieldUTime and StatFieldStime are /proc/<pid>/stat's 1-based fields
	// 14 and 15 — the CPU time the process itself has been scheduled for, in
	// USER and in SYSTEM mode, both in clock ticks (ClockTicksPerSecond below).
	// They are the only CPU accounting the kernel keeps for a process, and a
	// reader watching a long child has no other way to tell "working" from
	// "stopped": the number is a COUNTER, so two readings separated by an
	// interval answer "has it been computing in between?" (a flat pair does
	// not), and a reading beside the STATE field answers "is it on the CPU
	// right now?" (Rule 15: measure, never infer from silence).
	StatFieldUTime = 14
	// StatFieldStime is field 15; see StatFieldUTime. Summing them is what
	// CPUTicks below does, and both numbers are TICKS rather than seconds, so
	// ClockTicksPerSecond is the only way to render one.
	StatFieldStime = 15
	// ClockTicksPerSecond is the userspace clock-tick rate (sysconf
	// _SC_CLK_TCK) that turns a tick count into a duration. The CGO-free build
	// cannot call sysconf, so the value is named and documented instead:
	// Linux's tick rate is 100 Hz on the architectures gopi ships on. It is
	// the single definition — internal/tools' own procClockTicksPerSec is an
	// alias of it (Rules 13/14) — and it is used ONLY to render a
	// human-readable duration: identity comparison between two readings is
	// done on the raw tick counts, so a host with another HZ can never produce
	// a wrong verdict, only an approximate display.
	ClockTicksPerSecond = 100
	// OrphanReparentPPID is the ppid a process carries once NOBODY is left to
	// reap it. It is named here, in the leaf package that owns every process
	// FACT this repo reads (Rules 13/14), because the value is NOT a universal
	// constant and every reader must be able to say WHICH reaper it means:
	//
	//   - on an ordinary host it is pid 1, the init process;
	//   - on a systemd user session the kernel's child-subreaper for
	//     user.slice is `systemd --user` (pid 3100 on this host, measured), so
	//     a process that setsid(2)'d out of its session and was then abandoned
	//     is reparented to THAT manager and carries ppid 3100, never 1.
	//
	// A test that demands ppid == 1 is therefore asserting one host's reaper
	// rather than the mechanism ("nobody holds me any more") — which is why
	// the predicate (testutil.ProcIdentity.Orphan) compares against
	// ProcReaperSet() below and never against this literal alone.
	OrphanReparentPPID = 1
	// UserManagerComm is the comm of a systemd USER instance — the process the
	// kernel installs as the child-subreaper of user.slice, and the reaper a
	// detached, abandoned process is handed on such a host. It is the comm
	// (not argv) because that is what the kernel truncates and what /proc
	// reports in field 2, and it is kept as the plain name (never the
	// "/lib/systemd/systemd" path) so both spellings of the manager — the
	// `--user` instance and a `--system` one running a user service — are
	// recognized by the same string.
	UserManagerComm = "systemd"
	// UserManagerArg is the ONE argv word that tells a systemd USER manager
	// from the system init/system manager. Both share the comm above, so the
	// comm alone would attribute a process to the wrong one; the flag makes
	// "the user's own reaper" a positive identification, and it is the same
	// test the login session's own machinery passes (the manager starts every
	// user service with this flag on its own command line).
	UserManagerArg = "--user"
	// ArgvVerbIndex is the argv index of the top-level CLI verb: argv[0] is the
	// binary path, argv[1] is the verb (or the first process-global flag).
	ArgvVerbIndex = 1
	// LongFlagPrefix marks a process-global flag in raw argv. config's
	// BootstrapEnvFromArgs consumes these BEFORE the top-level verb, so a
	// worker-shape scan must walk them the same way (Rule 14: one spelling).
	LongFlagPrefix = "--"
	// FlagAssignSep separates a long flag's name from its inline value in the
	// `--name=value` form, which consumes a single argv element.
	FlagAssignSep = "="
)

// --- the liveness IDENTITY TRIPLE ------------------------------------------
//
// Counting worker-SHAPED processes (comm + argv) answers "does a process that
// looks like a worker exist right now?". It cannot answer "is this the process I
// saw / started?" — a worker is an ordinary shape that any process can be given
// the name of, and a pid is a NUMBER the kernel recycles. Deciding whether a
// recorded pid still names the same live process therefore takes three factors,
// each defending against a different lie:
//
//   - PID — the SLOT: which process-table entry is meant, the only factor a
//     caller can look up. It defends against nothing on its own: it is reused
//     within seconds on a busy host, and it outlives the process (a zombie keeps
//     its pid until it is reaped — kill(pid, 0) succeeds for a corpse).
//   - STARTTIME (/proc/<pid>/stat field 22, clock ticks since boot) — the
//     INCARNATION: the kernel hands every process a starttime and never reuses a
//     (pid, starttime) pair, so when the ticks differ the number has been
//     recycled and the process the caller means is GONE. This is the factor that
//     comm + cmdline cannot stand in for: a supervisor that restarts the same
//     command produces a byte-identical comm and argv under the same pid slot.
//   - CMDLINE (/proc/<pid>/cmdline, NUL-separated argv) — the ROLE: it separates
//     a worker from the task-serve daemon, the merger and a one-shot `task run`
//     child that re-exec the SAME binary with different argv. It is the weakest
//     factor alone: a process sets its own argv, and a wrapper shares its
//     target's.
//
// The verdict is three-valued — alive / recycled / unknown — never a bool. When
// procfs cannot be read the honest answer is UNKNOWN (Rule 15: never fabricate a
// death, and never fabricate a life either).
const (
	// VerdictAlive: the pid exists and every recorded factor still matches — the
	// pid names the live process the caller means.
	VerdictAlive = "alive"
	// VerdictRecycled: the pid exists but a recorded factor does not match — the
	// number was reused by a different process, and acting on it would act on a
	// stranger.
	VerdictRecycled = "recycled"
	// VerdictUnknown: the factors could not be read (no procfs, restricted
	// /proc, a race with the process's exit) — never collapsed into "dead".
	VerdictUnknown = "unknown"
)

// StatField extracts ONE field (numbered as procfs numbers the fields of
// /proc/<pid>/stat, 1-based) from a stat LINE.
//
// IT PARSES FROM THE LAST ')' ON PURPOSE. The line reads
// "<pid> (<comm>) <state> …", and comm is the process's own name truncated to 15
// bytes — it may itself contain spaces and parentheses (a shell substituted into
// a command is named "(bash)"; this host's workers are named "gopi worker"). A
// whitespace split therefore shifts every field after comm by however many
// spaces the name contained, and silently returns the WRONG COLUMN — the
// difference between "same incarnation" and "recycled", decided by a process
// name. The kernel parenthesises only comm, so the LAST ')' terminates it and
// the tokens after it are positionally exact.
//
// (false) = "cannot answer": no parenthesis, or the line is shorter than field.
// Callers MUST treat that as VerdictUnknown, never as a value.
func StatField(statLine string, field int) (string, bool) {
	if field < StatFieldState {
		return "", false
	}
	close := strings.LastIndex(statLine, ")")
	if close < 0 {
		return "", false
	}
	fields := strings.Fields(statLine[close+1:])
	// The tokens after comm are field 3 onward, so token N holds field N+2 — an
	// invariant measured on this host's /proc/<pid>/stat (token 11 = the wchan
	// field 13, token 20 = starttime field 22), NOT a guess: a short line (a
	// hand-built fixture, a kernel that grows a field) must answer UNKNOWN
	// rather than return a neighbour's value.
	idx := field - StatFieldState
	if idx >= len(fields) {
		return "", false
	}
	return fields[idx], true
}

// StartTicks reads the starttime (stat field 22) of procRoot/<pid>, returned as
// the raw decimal string so a compare against a recorded value is an exact
// string compare. (false) = unreadable (gone, restricted, malformed).
func StartTicks(procRoot, pid string) (string, bool) {
	return StatFieldString(StatStatPath(procRoot, pid), StatFieldStartTime)
}

// StatFieldString reads ONE stat field from a PATH — StatField plus the file
// read, so a caller holding a path does not re-open the file by hand.
// (false) = unreadable file or unusable line.
func StatFieldString(statPath string, field int) (string, bool) {
	raw, err := os.ReadFile(statPath)
	if err != nil {
		return "", false
	}
	return StatField(string(raw), field)
}

// State reads the single-letter run state of procRoot/<pid> ("R" running, "S"
// sleeping, "D" uninterruptible, "Z" zombie — StatFieldState's own column).
// (false) = unreadable, restricted, or gone. It is the state at the moment of
// the read and nothing else: a reader watching a long child combines it with a
// CPU reading (CPUTicks) rather than reading it alone, because a process inside
// a syscall it will return from also sleeps.
func State(procRoot, pid string) (string, bool) {
	return StatFieldString(StatStatPath(procRoot, pid), StatFieldState)
}

// CPUTicks reads the CPU time procRoot/<pid> has been scheduled for — user plus
// system, in clock ticks (ClockTicksPerSecond) — and reports whether it could
// be MEASURED. It is the ONE reader of the two CPU columns (StatFieldUTime /
// StatFieldStime), so a caller watching a long-running child never indexes them
// by hand (Rules 13/14).
//
// (false) is "cannot answer" — gone, restricted /proc, malformed line — and it
// is deliberately NOT 0: 0 is a real MEASUREMENT (a process that exists and has
// not been scheduled yet), and a caller deciding "is my child still computing?"
// must not read "unreadable" as "computed nothing" (Rule 15).
func CPUTicks(procRoot, pid string) (uint64, bool) {
	raw, err := os.ReadFile(StatStatPath(procRoot, pid))
	if err != nil {
		return 0, false
	}
	return CPUTicksOfLine(string(raw))
}

// CPUTicksOfLine is CPUTicks' parse half: the user + system tick columns of ONE
// stat line. It is a pure function of the line — the same split StatField makes
// — so a fixture can pin the two field numbers without a live process.
func CPUTicksOfLine(statLine string) (uint64, bool) {
	user, ok := statUintField(statLine, StatFieldUTime)
	if !ok {
		return 0, false
	}
	system, ok := statUintField(statLine, StatFieldStime)
	if !ok {
		return 0, false
	}
	return user + system, true
}

// statUintField is StatField's numeric render: (0, false) when the column is
// missing OR is not a number, so no reader ever works on a half-parsed count.
func statUintField(statLine string, field int) (uint64, bool) {
	raw, ok := StatField(statLine, field)
	if !ok {
		return 0, false
	}
	n, err := strconv.ParseUint(raw, 10, 64)
	if err != nil {
		return 0, false
	}
	return n, true
}

// StatStartTime extracts starttime (field 22) from the raw TEXT of
// /proc/<pid>/stat and returns it as the tick count a caller records and
// compares. It is StatField's value-shaped sibling: the SAME parse (there is
// one), rendered as (0, error) for the callers whose contract is an error
// rather than an ok flag — internal/scratch's holder registration reports the
// failure instead of reading a zero as a value (Rule 15: no fabricated 0).
//
// The error is returned, never a partially-trusted number, for both the
// "unparseable line" and the "not a number" cases; a caller keeping its own
// `n <= 0` guard still sees 0 here on any failure.
func StatStartTime(statLine string) (uint64, error) {
	raw, ok := StatField(statLine, StatFieldStartTime)
	if !ok {
		return 0, fmt.Errorf("%s: field %d (starttime) missing — the line carries no count-from-the-last-')' field at that column", StatName, StatFieldStartTime)
	}
	return strconv.ParseUint(raw, 10, 64)
}

// StatStatPath is the /proc/<pid>/stat path under a procfs root, built from the
// named entry constants so no caller joins path components by hand.
func StatStatPath(procRoot, pid string) string {
	return procRoot + "/" + pid + "/" + StatName
}

// ProcSessionKey returns the process-group SESSION id of pid — stat's
// StatFieldSession, the id of the session whose lifetime bounds every process
// that inherited that session. It is the kernel's own record of "which launch
// owns this process", which is what makes it usable where an ancestry walk is
// not: the kernel KEEPS a session id after its leader dies, while getppid()
// silently renumbers the orphan to 1.
//
// The reader copies the one parse this repo has (Rules 13/14: the parent and
// the session column are adjacent, so StatFieldParent would return the WRONG
// field here — this is deliberately its own exact column, through the same
// count-from-the-last-')' reader).
//
// (0, false) = unreadable (the process is gone, or procfs is not mounted).
func ProcSessionKey(procRoot, pid string) (int, bool) {
	raw, ok := StatFieldString(StatStatPath(procRoot, pid), StatFieldSession)
	if !ok {
		return 0, false
	}
	sid, err := strconv.Atoi(raw)
	if err != nil || sid <= 0 {
		return 0, false
	}
	return sid, true
}

// OrphanReparentTargets returns every pid an orphan on THIS host can be handed
// to when its parent dies — the set `ppid == 1` was a shorthand for, and the ONE
// definition of that set (Rules 13/14).
//
// WHY IT IS A SET AND NOT A LITERAL. The kernel reparents an orphaned child to
// the NEAREST ancestor marked PR_SET_CHILD_SUBREAPER, and to the init of its pid
// namespace only when no ancestor is marked. A host running a systemd user
// session has such an ancestor: `systemd --user` (pid 3100 on this host,
// measured — comm "systemd", argv carries --user, its own ppid 1), so a process
// that setsid(2)'d out of its session and was then abandoned carries ppid 3100
// and NEVER 1. Measured 2026-10-12: `sh -c 'sleep 300 & echo $!'` → the orphan's
// /proc/<pid>/stat reads ppid=3100.
//
// The mark is NOT readable on this host — /proc/sys/kernel/child_subreaper first
// exists in Linux 6.10 and this host runs 6.8 — so the set is DERIVED from what
// /proc does expose: the namespace init (always a target: it is what remains when
// nothing is marked) plus every live systemd USER instance (identified by comm
// AND the --user argv word, the same positive identification UserManagerArg
// documents — a --system manager shares the comm and is correctly excluded).
//
// The kernel bounds a marked ancestor to one of the process's OWN ancestors, and
// a launcher that sets the mark itself (a run wrapper, a multiplexer) is
// invisible here exactly as it is to every /proc reader below 6.10 — so callers
// that sweep a process tree they own compare against the set AND walk their own
// ancestry (testutil.ProcIdentity.Orphan's two arms), never against this set
// alone.
//
// An unreadable procfs is an ERROR, never an empty set: a caller that read
// "nothing is a target" would classify NOTHING and keep a veto it has no reason
// to keep (Rule 15 — a missing measurement is reported, not spent as a verdict).
func OrphanReparentTargets(procRoot string) (map[int]bool, error) {
	entries, err := os.ReadDir(procRoot)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", procRoot, err)
	}
	targets := map[int]bool{OrphanReparentPPID: true}
	for _, e := range entries {
		name := e.Name()
		if name == "" || name[0] < '0' || name[0] > '9' {
			continue // not a pid entry (procfs also carries "self", "meminfo", …)
		}
		pid, convErr := strconv.Atoi(name)
		if convErr != nil || pid <= 0 {
			continue
		}
		if userManagerInstance(procRoot, name) {
			targets[pid] = true
		}
	}
	return targets, nil
}

// userManagerInstance reports whether <procRoot>/<pid> IS a systemd user
// instance: its comm names the manager AND its own argv carries the --user flag.
// The flag is the discriminator (a --system manager shares the comm), and it is
// read from the process's own cmdline, never guessed from a path.
func userManagerInstance(procRoot, pid string) bool {
	comm, err := os.ReadFile(procRoot + "/" + pid + "/" + CommName)
	if err != nil || strings.TrimSpace(string(comm)) != UserManagerComm {
		return false
	}
	argv, ok := Cmdline(procRoot, pid)
	if !ok {
		return false
	}
	for _, word := range argv {
		if word == UserManagerArg {
			return true
		}
	}
	return false
}

// Liveness is what a caller RECORDED about the process it means: the pid, its
// starttime at record time, and its argv vector. An empty Cmdline means "not
// recorded" (a legacy row, or a shell count that never had the text) — the
// factor is then not compared rather than compared against "".
type Liveness struct {
	Pid       string
	StartTime string
	// StartTicks is StartTime as the integer the kernel reports, for callers
	// whose ledger column is an int64 (a task row). StartTime is the string
	// spelling the same fact for callers that captured it verbatim. A caller
	// sets ONE of them; a record with neither cannot have its incarnation
	// checked, which RunnerLive reports as UNMEASURABLE rather than as a death.
	StartTicks int64
	// Cmdline is the recorded argv VECTOR (a caller that captured the process's
	// own argv — the worker count does).
	Cmdline []string
	// CmdlineText is the recorded command line as TEXT: a ledger column and a
	// key=value record hold a string, not a vector. It is compared as text, so
	// the two sides are never a raw capture against a differently normalised
	// spelling.
	CmdlineText string
}

// Joins reports whether argv equals the recorded Cmdline (the argv ROLE factor).
// An unrecorded Cmdline (empty slice) matches anything: the caller cannot
// demand a factor it never recorded. An EMPTY live argv is a MISSING factor —
// the kernel gave no answer (a zombie's cmdline is empty by design, a restricted
// read fails) — so it constrains nothing either: comparing it would turn an
// unmeasured factor into a mismatch, i.e. into a fabricated "recycled".
func (l Liveness) Joins(argv []string) bool {
	if len(l.Cmdline) == 0 || len(argv) == 0 {
		return true
	}
	if len(argv) != len(l.Cmdline) {
		return false
	}
	for i := range argv {
		if argv[i] != l.Cmdline[i] {
			return false
		}
	}
	return true
}

// Verdict answers the WORKER-liveness question for ONE procfs root — the
// three-valued identity check the worker COUNT follows, where the cmdline is a
// full identity axis:
//
//	procRoot/<pid> absent                      → VerdictRecycled (the pid is gone;
//	                                             "recycled" and "dead" are the
//	                                             same answer to a caller holding
//	                                             a recorded pid: not our process)
//	a zombie at the pid                        → VerdictRecycled (it does no work)
//	starttime recorded, live differs           → VerdictRecycled
//	cmdline recorded, live differs             → VerdictRecycled
//	unreadable stat / no procfs                → VerdictUnknown
//	all recorded factors agree, state != Z     → VerdictAlive
//
// TWO QUESTIONS, ONE COMPARISON, TWO VOCABULARIES — read this before routing a
// new caller here. A recorded RUNNER (a task row's proc_pid/proc_start_ticks/
// proc_cmd) and a counted WORKER are both asked "is this still the same live
// process?", and the answer comes from ONE comparison (identityCore below), but
// the two callers need different WORDS for it:
//
//   - the worker count has no ledger, so "the pid is gone" and "a stranger owns
//     the number" are one answer: not my worker → VerdictRecycled;
//   - a ledger row's operator text distinguishes them: our process gone with the
//     number reused is "recycled", a process that is gone outright is "dead" →
//     RunnerIdentity's Outcome names them separately.
//
// A caller asking the LEDGER question must use RunnerIdentity/RunnerLive, whose
// doc states the question and its boundary; this function exists for the worker
// count and keeps its own value set for that caller's vocabulary.
func Verdict(procRoot string, want Liveness) string {
	outcome, _ := RunnerIdentity(procRoot, want)
	switch outcome {
	case OutcomeLive:
		return VerdictAlive
	case OutcomeUnmeasurable:
		return VerdictUnknown
	default:
		// Gone and Recycled are the same fact to this caller: the process it
		// means is not there.
		return VerdictRecycled
	}
}

// Outcome is the named answer of the ONE identity question "is the process a
// record names still, right now, the same live process?". It is four-valued on
// purpose (Rule 15a): a factor the kernel would not answer is a THIRD answer
// next to live and not-live, and it must not share a value with either — a
// caller that reads "could not measure" as "not live" deletes a worktree whose
// runner is sitting in it (memory #2632).
type Outcome string

const (
	// OutcomeLive: the pid names a live process and every factor the record
	// states agrees — the recorded process IS the process at that pid.
	OutcomeLive Outcome = "live"
	// OutcomeGone: the recorded process is not live and nobody else has taken
	// the number — no process directory at the pid, or a terminated-but-unreaped
	// zombie (which does no work while still answering kill(pid, 0)).
	OutcomeGone Outcome = "gone"
	// OutcomeRecycled: the pid is alive but a recorded factor differs — the
	// kernel handed the number to a different process, so acting on it would act
	// on a stranger.
	OutcomeRecycled Outcome = "recycled"
	// OutcomeUnmeasurable: the kernel did not answer, and UnmeasurableReason
	// names which factor was missing. NEVER rendered as "not live".
	OutcomeUnmeasurable Outcome = "unmeasurable"
)

// UnmeasurableReason names WHY a liveness probe could not measure a factor — the
// named-unknown half of the predicate's contract (Rule 15a: "the kernel did not
// answer" is neither "the process is here" nor "the process is gone").
type UnmeasurableReason string

const (
	// UnmeasurableNoRecord: the caller supplied no record naming a process, or a
	// record stating no factor at all — there is nothing to compare.
	UnmeasurableNoRecord UnmeasurableReason = "no-record"
	// UnmeasurableNoProcfs: no /proc/<pid>/stat could be read even though the
	// process directory is present — no procfs on this host, or this process may
	// not look. The recorded process MAY be running.
	UnmeasurableNoProcfs UnmeasurableReason = "no-procfs"
	// UnmeasurableStatUnparsable: a stat line was read but its columns did not
	// parse (a short or malformed line) — the incarnation factor is unknown,
	// never a mismatch.
	UnmeasurableStatUnparsable UnmeasurableReason = "stat-unparsable"
	// UnmeasurableNoCmdline: the record states a command line but /proc/<pid>/
	// cmdline could not be read, so the one factor it states cannot be compared.
	UnmeasurableNoCmdline UnmeasurableReason = "no-cmdline"
)

// RunnerIdentity answers THE LEDGER-VERDICT QUESTION: "is the runner a task row
// RECORDED — pid + /proc starttime + cmdline — still, right now, the same live
// process?"
//
// It does NOT answer the CHECKOUT-OCCUPANCY question ("is a process standing in
// this worktree?"). That is a different question about a different channel
// (/proc/<pid>/cwd, plus fd/exe/maps), it has its own single implementation
// (scratch.HeldByProcesses / scratch.LiveHolders), and a caller that needs it
// must ask it there — this predicate can neither confirm nor deny occupancy, and
// reading its answer as one is exactly the substitution it exists to prevent.
//
// ONE LADDER, so two callers cannot disagree about the same row (Rule 14: a
// second ladder drifts, and this drift is fail-open — memory #2632). The
// factors are compared strongest-first, and an unmeasured factor is never a
// mismatch:
//
//	the process directory is absent          → Gone        (measured: it is not there)
//	the pid names a zombie                   → Gone        (it does no work)
//	starttime recorded and it differs        → Recycled    (the number was reused)
//	no incarnation recorded, command line
//	  recorded and it differs                → Recycled    (then the command line
//	                                                       is the ONLY factor there
//	                                                       is — a caller's own argv
//	                                                       role check, e.g. the worker
//	                                                       count)
//
// procRoot is a parameter rather than a constant so a test can drive the exact
// production ladder over a synthetic tree (see EnvProcRoot); an empty procRoot
// means DefaultProcRoot.
func RunnerIdentity(procRoot string, want Liveness) (Outcome, UnmeasurableReason) {
	root := strings.TrimSpace(procRoot)
	if root == "" {
		root = DefaultProcRoot
	}
	if strings.TrimSpace(want.Pid) == "" {
		return OutcomeUnmeasurable, UnmeasurableNoRecord
	}
	if !want.statesAnyFactor() {
		// A pid with no starttime and no command line is NOT an identity: a pid
		// alone is a NUMBER the kernel recycles, so reading this as Live would
		// report a stranger as the recorded process, and reading it as Gone would
		// fabricate a death. The record measured nothing ⇒ the answer is
		// UNMEASURABLE (Rule 15a).
		return OutcomeUnmeasurable, UnmeasurableNoRecord
	}
	return identityCore(root, want)
}

// statesAnyFactor reports whether the record states at least ONE of the two
// comparison factors (the incarnation or the command line). A record that states
// neither has nothing to compare, which is an unmeasurable answer rather than a
// live or a dead one.
func (l Liveness) statesAnyFactor() bool {
	return l.StartTime != "" || l.StartTicks > 0 || len(l.Cmdline) > 0 || l.CmdlineText != ""
}

// textCmdline is the record's command-line factor as ONE string: the text form
// when the caller recorded one, else its argv joined the way procfs reports it.
func (l Liveness) textCmdline() string {
	if l.CmdlineText != "" {
		return l.CmdlineText
	}
	return strings.Join(l.Cmdline, " ")
}

// tickValue is the record's incarnation as a NUMBER: the integer spelling when
// the caller has one (a ledger column), else the verbatim string column parsed.
// (false) = the record states no readable incarnation.
func (l Liveness) tickValue() (int64, bool) {
	if l.StartTicks > 0 {
		return l.StartTicks, true
	}
	if l.StartTime == "" {
		return 0, false
	}
	return parseTickCount(l.StartTime)
}

// parseTickCount converts a starttime COLUMN into the tick number a ledger column
// holds. (false) = not a positive number, i.e. the incarnation was NOT measured.
func parseTickCount(ticks string) (int64, bool) {
	n, err := strconv.ParseInt(strings.TrimSpace(ticks), 10, 64)
	if err != nil || n <= 0 {
		return 0, false
	}
	return n, true
}

// RunnerLive is RunnerIdentity reduced to the yes/no a caller that only needs
// the boolean asks. The second value is non-empty ONLY when the kernel could not
// be asked — the named UNMEASURABLE outcome — and a caller must treat it as
// "unknown", never as "not live" (Rule 15a: false ≠ unknown; the two must not
// share a value).
//
// A RECORDED RUNNER'S CMDLINE IS A HINT, NOT AN IDENTITY AXIS, and the rule is
// enforced INSIDE the ladder rather than left to each caller's discretion: once
// an incarnation is recorded the command line is NOT compared, so a caller may
// pass the cmdline it recorded (it constrains nothing) and a caller whose record
// has no incarnation passes it as the only factor it has. Leaving this to the
// callers is precisely how two of them came to disagree about one record.
func RunnerLive(procRoot string, want Liveness) (bool, UnmeasurableReason) {
	outcome, why := RunnerIdentity(procRoot, want)
	if outcome == OutcomeLive {
		return true, ""
	}
	return false, why
}

// DefaultProcRoot is the procfs root a production caller reads: the real /proc.
// It is a named constant rather than a literal so every caller and the test seam
// (EnvProcRoot) derive from one value (Rule 1b/13).
const DefaultProcRoot = "/proc"

// identityCore is the ONE procfs READ of the three factors — every exported
// answer above (RunnerIdentity, RunnerLive, Verdict) is a MAPPING of what this
// returns, and the comparison itself is Match, so a fix or a rename lands once
// instead of in a per-caller ladder (Rule 14).
func identityCore(root string, want Liveness) (Outcome, UnmeasurableReason) {
	raw, err := os.ReadFile(StatStatPath(root, want.Pid))
	if err != nil {
		// No stat file: either the process is gone or we are not allowed to
		// look. The process DIRECTORY separates them — it is there for a live
		// process even when stat is restricted.
		if dirAbsent(root, want.Pid) {
			return OutcomeGone, ""
		}
		return OutcomeUnmeasurable, UnmeasurableNoProcfs
	}
	line := string(raw)
	state, ok := StatField(line, StatFieldState)
	if !ok {
		return OutcomeUnmeasurable, UnmeasurableStatUnparsable
	}
	ticks, ok := StatField(line, StatFieldStartTime)
	if !ok {
		return OutcomeUnmeasurable, UnmeasurableStatUnparsable
	}
	// The LIVE side, as the kernel states it. The command line is read only when
	// the record STATES one — an unreadable cmdline is a missing factor, never a
	// mismatch, and a caller cannot demand a factor it never recorded.
	live := Liveness{Pid: want.Pid, StartTime: ticks}
	if len(want.Cmdline) > 0 || want.CmdlineText != "" {
		argv, ok := Cmdline(root, want.Pid)
		if !ok {
			return OutcomeUnmeasurable, UnmeasurableNoCmdline
		}
		live.Cmdline = argv
	}
	outcome, why := Match(want, live)
	if outcome != OutcomeLive {
		return outcome, why
	}
	if state == ZombieState {
		// Terminated but unreaped: it answers kill(pid, 0) while doing no work,
		// so for every caller of this predicate it is GONE, not live.
		return OutcomeGone, ""
	}
	return OutcomeLive, ""
}

// Match is the ONE three-factor comparison: it decides whether the process a
// RECORD describes is the process whose factors a CALLER read. RunnerIdentity is
// Match over factors read from procfs; a caller that must read procfs once for
// several purposes (a report that prints the factors it compared) calls Match
// instead of re-inlining the ladder (Rule 14 — a second ladder drifts, and this
// drift is fail-open: memory #2632).
//
// Precedence, and why: the INCARNATION (starttime) is the strong factor — the
// kernel never hands one (pid, starttime) pair to two processes, so once the
// ticks agree the process IS the recorded one and the command line is
// corroboration rather than an axis (a runner may replace its image in place:
// a wrapper `sh -c …` becoming its target under the same pid). The command line
// is an axis only for a record that states NO incarnation — there it is the only
// factor there is (the worker count's argv ROLE check).
//
// An unmeasured factor on EITHER side is Unmeasurable, never a mismatch and
// never a match (Rule 15a): "the kernel did not answer" must not share a value
// with "the process is gone", because the value a destructive arm acts on is
// exactly the not-live one.
func Match(rec, live Liveness) (Outcome, UnmeasurableReason) {
	if !rec.statesAnyFactor() {
		return OutcomeUnmeasurable, UnmeasurableNoRecord
	}
	if rec.StartTime != "" || rec.StartTicks > 0 {
		wantTicks, ok := rec.tickValue()
		if !ok {
			return OutcomeUnmeasurable, UnmeasurableStatUnparsable
		}
		liveTicks, ok := live.tickValue()
		if !ok {
			return OutcomeUnmeasurable, UnmeasurableStatUnparsable
		}
		if wantTicks != liveTicks {
			return OutcomeRecycled, ""
		}
		return OutcomeLive, ""
	}
	// No incarnation recorded: the command line is the only factor there is.
	wantCmd := rec.textCmdline()
	liveCmd := live.textCmdline()
	if wantCmd == "" || liveCmd == "" {
		// The record states a factor the live side did not answer (an empty
		// /proc/<pid>/cmdline is what the kernel really produces for a zombie, and
		// a restricted read fails): unmeasured, never a mismatch.
		return OutcomeUnmeasurable, UnmeasurableNoCmdline
	}
	if wantCmd != liveCmd {
		return OutcomeRecycled, ""
	}
	return OutcomeLive, ""
}

// dirAbsent reports whether the process DIRECTORY is provably not there — the
// one statement that separates "the process is gone" (an answer) from "we were
// not allowed to read its stat" (an unknown).
func dirAbsent(root, pid string) bool {
	_, err := os.Stat(root + "/" + pid)
	return os.IsNotExist(err)
}

// ZombieState is the /proc state letter of a terminated-but-unreaped process: it
// answers kill(pid, 0) while doing no work at all, which is why every liveness
// verdict in this package reports it as not-alive.
const ZombieState = "Z"

// StateRunning is the /proc state letter of a process the scheduler currently has
// ON a CPU ("R", running or runnable). It answers a question that wall time
// cannot: whether a long wait has a process behind it that is really computing
// right now, as opposed to one that is blocked, or none at all. It is a
// single-letter state among the others (State/SleepingState/ZombieState), named
// here so no reader spells a state letter inline (Rule 1b).
const StateRunning = "R"

// Children reads the DIRECT children of procRoot/<pid>, as the kernel lists them,
// and returns their pids DE-DUPLICATED and sorted ascending. An unreadable or
// absent tree is an EMPTY list, never an error: a pid that has exited has no
// children to report, and the caller's question ("is anything still running under
// this process?") is answered by whatever IS listed.
//
// It enumerates EVERY THREAD, not just the main one. The kernel records a direct
// child under the thread that forked it (a new task's ppid is the process, but its
// parent TASK is the thread), so a process that spawned from a non-main thread
// lists that child only under /proc/<pid>/task/<tid>/children. Reading the main
// thread's file alone therefore answers a FLOOR, not the tree — and a floor is the
// wrong shape for the question every caller actually asks ("is anything still
// running under this process?"), because "1 process" and "the only process I can
// see" are then indistinguishable. Measured cost of the main-thread-only read: a
// whole-package Go test run whose `go test` children hung was reported as
// `processes=1` with no progress, which the run-progress watchdog read as "nothing
// is running" while the tree was in fact blocked (task 01a1b964's diagnosis,
// internal/testutil/runprogress.go's measureRunProgress).
//
// A thread directory that disappears between the ReadDir and the ReadFile is
// skipped, not an error: threads exit constantly, and the caller asked for
// children, not for a census of threads.
func Children(procRoot, pid string) []int {
	taskDir := filepath.Join(procRoot, pid, TaskName)
	threads, err := os.ReadDir(taskDir)
	if err != nil {
		return nil
	}
	seen := make(map[int]bool)
	out := make([]int, 0, len(threads))
	for _, th := range threads {
		raw, rerr := os.ReadFile(filepath.Join(taskDir, th.Name(), ChildrenName))
		if rerr != nil {
			continue
		}
		for _, f := range strings.Fields(string(raw)) {
			n, cerr := strconv.Atoi(f)
			if cerr != nil {
				continue
			}
			seen[n] = true
		}
	}
	for n := range seen {
		out = append(out, n)
	}
	sort.Ints(out)
	return out
}

// Cmdline reads and splits /proc/<pid>/cmdline under a procfs root. (false) =
// unreadable, or empty (which the kernel really produces — a zombie's cmdline is
// empty by design), so an empty read is NEVER silently returned as an empty argv.
func Cmdline(procRoot, pid string) ([]string, bool) {
	raw, err := os.ReadFile(procRoot + "/" + pid + "/" + CmdlineName)
	if err != nil {
		return nil, false
	}
	text := strings.TrimRight(string(raw), "\x00")
	if text == "" {
		return nil, false
	}
	return strings.Split(text, "\x00"), true
}

// ExeName is the procfs entry holding a process's EXECUTABLE LINK — the
// kernel's own answer to "which file was this process started from". It is the
// channel the process-IMAGE identity reads (Rules 1b/13/14: spelled once, in the
// leaf package that already owns every process fact).
const ExeName = "exe"

// DeletedImageMarker is what the kernel appends to /proc/<pid>/exe's target when
// the image file has been unlinked ("/tmp/build/gopi (deleted)"). A reader that
// compares base names strips it: the process still IS the gopi program, and
// treating the marker as part of the name is a fabricated mismatch.
const DeletedImageMarker = " (deleted)"

// ImageGopiPrefix is the prefix a process's IMAGE base name must carry to be
// running the gopi program. It is the ONE definition of "looks like gopi" for
// every judgement site (Rules 13/14) — and it is deliberately a PREFIX rather
// than the equality with NameGopi, because the equality was the defect this
// constant replaces:
//
// THE FAMILY OF FALSE ABSENCE (bug 01a131fcb8337c1b, measured 2026-10-13). Four
// judgement sites asked "is this process the gopi program?" and answered it with
// comm == NameGopi — a proxy for the real question. comm is the basename of the
// executable FILE, and a perfectly good gopi binary can be built under another
// file name (bin/gopi-fix, bin/gopi-test, a per-task build), or be launched via a
// differently-named copy. Against such a worker the four sites reported, in
// their own words, "no live gopi worker", "no FOREGROUND worker in the process
// table" and "not a worker" — each a FABRICATED ABSENCE of a process that was
// alive and running this program (Rule 15: a proxy metric may never be reported
// as the measurement).
//
// The real measurement is the process's IMAGE: /proc/<pid>/exe, the kernel's
// answer, stripped of DeletedImageMarker, falling back to the process's OWN
// argv[0] basename when the link cannot be read (a deleted image, a restricted
// entry). It is a PREFIX test because an image named gopi-<anything> is still
// this program, while a process whose image is merely called something else
// entirely is not.
const ImageGopiPrefix = "gopi"

// ImageName resolves a process's IMAGE base name: /proc/<pid>/exe first (the
// kernel's answer), then the caller-supplied argv0 (the process's own record of
// what it was started as). It returns "" when neither channel answers — a
// MISSING measurement, which a caller must report as such and never spend as
// "not a gopi process" (Rule 15a: unknown is not a value of the no-branch).
//
// argv0 is the process's OWN argv[0], which the caller reads from /proc/<pid>/
// cmdline; passing a value the caller guessed (a file name it expects) is the
// proxy substitution this function exists to end.
func ImageName(procRoot, pid, argv0 string) string {
	if procRoot != "" && pid != "" {
		if target, err := os.Readlink(procRoot + "/" + pid + "/" + ExeName); err == nil {
			if name := strings.TrimSuffix(filepath.Base(target), DeletedImageMarker); name != "" {
				return name
			}
		}
	}
	if argv0 == "" {
		return ""
	}
	name := filepath.Base(argv0)
	if name == "." || name == string(filepath.Separator) {
		// A argv[0] that names a DIRECTORY, not an executable (".", "/"): there
		// is no image name in it, and returning the directory's spelling would
		// hand every caller a plausible-looking name that identifies nothing.
		return ""
	}
	return name
}

// IsGopiImage reports whether a process runs the gopi PROGRAM, from the two
// channels that can answer it: the image base name resolved by ImageName, and —
// when even that is empty — the process's comm, which is the name the kernel
// truncated from the SAME image file (a link the kernel will not read and a
// cmdline that is empty still leave comm as evidence, and dropping that evidence
// would re-create the false absence one channel further out).
//
// (true, false): the image says gopi.
// (true, true): the image was UNREADABLE and comm says gopi — accepted, and the
//
//	caller is told the acceptance rests on the weaker channel, so a face that
//	wants to report its evidence can say which one it used.
//
// (false, _): neither channel names the gopi program.
func IsGopiImage(procRoot, pid, argv0, comm string) (isGopi, fromComm bool) {
	name := ImageName(procRoot, pid, argv0)
	if name != "" {
		return strings.HasPrefix(name, ImageGopiPrefix), false
	}
	comm = strings.TrimRight(comm, "\n")
	if comm == "" {
		// NO channel answered: the result is "not this program" (a caller may
		// not count it) and the flag is FALSE, because there is no weaker
		// channel to blame — nothing was measured at all (Rule 15a).
		return false, false
	}
	return strings.HasPrefix(comm, ImageGopiPrefix), true
}

// WorkerVerbIndex returns the argv index of the top-level `worker` verb, or
// -1 when argv carries no worker verb. argv[0] is the binary path.
//
// WHY THE VERB IS NOT ALWAYS argv[1]. This binary accepts process-global
// config flags BEFORE the subcommand word — the launcher this host actually
// uses starts workers as `gopi --settings <path> worker <task-id>` (the HY4
// profile's standard shape). `main.go` consumes those flags through
// config.BootstrapEnvFromArgs before dispatching, so inside the process the
// verb is argv[1] of the DISPATCH remainder — but /proc/<pid>/cmdline records
// the RAW argv the process was started with, where the verb sits after the
// flag and its value. Every worker-shape judgement reads that raw argv, so a
// hardcoded argv[1] == VerbWorker dropped every such worker in the discovery
// phase (measured 2026-11-18: three real HY4 workers with `worker` at argv[3]
// were invisible to `gopi worker --status` while two in_progress rows ran).
//
// THE RULE, stated so the scan cannot drift from the dispatcher: walk from
// ArgvVerbIndex and interpret each token by its SYNTACTIC ROLE — process-global
// flag, that flag's value, or the top-level verb. A leading `--name=value`
// flag is one token; a leading `--name value` flag is two, so its value is
// skipped. The FIRST token that is neither a flag nor a flag's value is the
// top-level verb: stop there and answer whether it is VerbWorker. Scanning on
// to the end of argv is precisely the 007c774a regression — in
// `gopi task submit worker` the `worker` is the submit SUMMARY (a positional
// argument), in `chat --once --prompt worker` it is a subcommand flag's value;
// neither is the top-level verb, and both were falsely counted as workers
// (measured 2026-11-18: `worker --status` reported a live worker while a
// `task submit worker` child was the only gopi process). Stopping at the verb
// needs no per-verb or per-flag whitelist: anything after the verb belongs to
// the subcommand and is never this scan's business.
//
// It is a PURE function on an argv slice (no os.Args, no env, no exit), so the
// process scanner can attribute ANOTHER process's argv without touching its
// own environment — the reason it lives in this leaf package rather than
// calling config.BootstrapEnvFromArgs (which mutates env and may os.Exit).
func WorkerVerbIndex(argv []string) int {
	for i := ArgvVerbIndex; i < len(argv); i++ {
		a := argv[i]
		if strings.HasPrefix(a, LongFlagPrefix) {
			// A process-global flag before the verb consumes its value.
			// `--name=value` is inline (one token); `--name value` is two.
			// Skip that value so a VALUE equal to VerbWorker is never read
			// as the verb. A following flag is not a value — leave it for
			// the next iteration.
			if !strings.Contains(a, FlagAssignSep) && i+1 < len(argv) &&
				!strings.HasPrefix(argv[i+1], LongFlagPrefix) {
				i++
			}
			continue
		}
		// The first non-flag, non-value token IS the top-level verb. Decide
		// here and STOP: every later token is a positional argument or a
		// subcommand flag whose value may equal VerbWorker by coincidence.
		if a == VerbWorker {
			return i
		}
		return -1
	}
	return -1
}

// WorkerShape reports whether a process — its comm, its argv and the image the
// kernel says it executes — is a WORKER by shape: the process runs the gopi
// program (IsGopiImage — NEVER the comm equality, which was the false absence) AND
// its top-level verb (after any process-global flags) is VerbWorker. It is the counting predicate the launcher implements in
// shell (scripts/gopi-worker-up.sh: "which processes are workers at this
// instant"), stated here so the Go side of the same contract has one definition
// to call (Rule 13/14) and so a test can pin the shape the shell mirrors.
//
// procRoot is the procfs root the image link is read under ("" = DefaultProcRoot,
// and a synthetic tree in a test); pid is the process the argv/comm belong to.
func WorkerShape(procRoot, pid, comm string, argv []string) bool {
	if isGopi, _ := IsGopiImage(procRoot, pid, argvFirst(argv), comm); !isGopi {
		return false
	}
	return WorkerVerbIndex(argv) >= 0
}

// WorkerImageIsGopi is IsGopiImage for a caller that has the pid as a STRING and
// no comm in hand — the shape the CLI faces are written in (they hold a pid from
// a directory name). The comm channel is left empty: the image and argv[0] are
// the channels that can answer, and an unreadable image with no comm is a
// MISSING measurement, reported by the false it returns rather than guessed at.
func WorkerImageIsGopi(procRoot, pid, argv0 string) bool {
	isGopi, _ := IsGopiImage(procRoot, pid, argv0, "")
	return isGopi
}

// argvFirst is argv[0] or "" — the process's own record of the image it was
// started from, which ImageName falls back to. An empty argv is not an error
// here: it is one of the channels being silent, and IsGopiImage has two others.
func argvFirst(argv []string) string {
	if len(argv) == 0 {
		return ""
	}
	return argv[0]
}
