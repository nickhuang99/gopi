package testutil

// ProcIdentity — the pid+starttime+cmdline TRIPLE that decides whether a pid is
// still the process we mean.
//
// WHY A TRIPLE AND NOT kill(pid, 0): pid liveness probes lie in three ways that
// all matter to a process-leak cleanup.
//
//  1. PID REUSE. The kernel recycles pids; after the original process exits, the
//     same number can name a stranger. A sweep that signals on a bare
//     kill(pid, 0) success can therefore kill an unrelated process — on a
//     developer host with live merger/worker daemons that is exactly the
//     catastrophe the "never kill by pattern" rule exists to prevent.
//     /proc/<pid>/stat field 22 (starttime, in clock ticks since boot) is
//     monotonic per pid slot, so pid+starttime identifies an INCARNATION.
//  2. ZOMBIES. kill(pid, 0) SUCCEEDS for a zombie: the pid table entry is still
//     there, but the process is already dead. The state letter, read from the
//     same /proc/<pid>/stat that supplies starttime, makes "dead" reportable.
//  3. NAME REUSE. comm (field 2 of the same stat) is truncated to 15 bytes and
//     is settable by the process itself, and a wrapper and its target share it.
//     /proc/<pid>/cmdline is the argv vector the operator can also see, and it is
//     what tells OUR daemon from a foreign one.
//
// So the triple is pid + starttime + cmdline, and comm is deliberately NOT part
// of it.

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/nick/gopi/internal/procname"
)

const (
	// procDir is procfs's mount point (Rule 1a: one named literal). Alias of
	// internal/procname's value — the leaf package that owns every process FACT
	// this repo reads (Rules 13/14).
	procDir = procname.ProcRootDefault
	// procStatStartTimeField is /proc/<pid>/stat's 1-based field number for
	// starttime (field 22). The parser indexes from the LAST ')' because the
	// comm field (field 2) is parenthesized and may itself contain spaces and
	// ')', so a naive field split shifts every later field. Alias of
	// procname.StatFieldStartTime, and the parse itself is procname.StatField —
	// ONE implementation repo-wide (Rules 13/14).
	procStatStartTimeField = procname.StatFieldStartTime
	// procStatStateField is field 3, the state letter ("Z" = zombie).
	procStatStateField = procname.StatFieldState
	// procStatPPIDField is field 4, the parent pid.
	procStatPPIDField = procname.StatFieldParent
	// procStatSessionField is field 6, the session id: pid == session means the
	// process is a session leader (detached from a controlling terminal).
	procStatSessionField = procname.StatFieldSession
	// procCmdlineSep is the NUL separator /proc/<pid>/cmdline uses.
	procCmdlineSep = "\x00"
	// procArgvJoin rejoins argv words for the comparison identity. A single
	// space is enough: the comparison is always between two reads of the SAME
	// argv vector, never between an argv and a shell string.
	procArgvJoin = " "
	// procStatName / procCmdlineName / procEnvironName / procCwdName /
	// procFDName are procfs's per-pid entries (Rule 1b: named, never inline).
	// The first two alias internal/procname's definitions — the values this
	// package shares with every other process-fact reader (Rules 13/14).
	procStatName    = procname.StatName
	procCmdlineName = procname.CmdlineName
	procEnvironName = "environ"
	procCwdName     = procname.CwdName
	procFDName      = "fd"
	// procReaperArgv is how much of a reaper candidate's cmdline is read to
	// decide whether it is a USER manager: the flag lives in argv[1], far
	// inside the first 4 KiB, and the cap keeps a process with a 128 KiB argv
	// from making the reaper set expensive.
	procReaperArgv = 4096
	// procReaperMaxParents bounds the ancestry walk ProcReaperOf performs (see
	// its comment): it reaches the top of a real process tree in a handful of
	// levels, and the bound is what turns a cycle in a corrupt ppid table into a
	// reported refusal instead of a hung suite.
	procReaperMaxParents = 12
	// procReaperChainMaxLevels bounds the ANCESTRY CHAIN walk Orphan() consults
	// (see procAncestryChain). It is deliberately WIDER than the reaper bound:
	// the chain must reach all the way to init, because its whole job is to
	// recognize the kernel's NEAREST marked ancestor — a process the sweep has
	// no name for — and a nested launcher tree puts more levels between the
	// test binary and init than a flat one does. Exhausting it FAILS LOUDLY
	// (Orphan() refuses rather than judging a chain it could not finish), so a
	// deeper tree is reported, never silently mis-read.
	procReaperChainMaxLevels = 40
	// procReaperKnownUniverse caps how large "every live process whose parent
	// is a reaper" is allowed to grow before the reaper set is REJECTED. It is
	// a correctness guard, not a performance one: the only channel that makes
	// a process eligible for the sweep is membership in this set, and a sweep
	// is destructive — a set computed off a mis-read table could contain
	// hundreds of unrelated pids, and a bounded, reported rejection is the
	// only honest answer. A real user session has a few dozen such processes.
	procReaperKnownUniverse = 500
)

// procReaperKind tells the two REAPERS apart, because which one a process has
// been handed is the whole point of Orphan(): both mean "nobody holds me", and
// they must therefore never be compared as if pid %d were one value.
type procReaperKind uint8

const (
	// procReaperUnknown is the zero value: NOT a reaper. It exists so that a
	// zero ProcReaperSet can never be mistaken for "everything is a reaper"
	// (Rule 15b: an unpopulated set is reported, never silently permissive).
	procReaperUnknown procReaperKind = iota
	// procReaperInit is the init process (ppid 1) — the ordinary host's
	// reaper.
	procReaperInit
	// procReaperUserManager is a systemd USER instance: the kernel's
	// child-subreaper for user.slice on a host with a user session. A process
	// that setsid(2)'d out of its session and was then abandoned lands HERE,
	// not on init.
	procReaperUserManager
)

// procReaperInts is the SET of pids REAPING this host right now: init, plus
// every live systemd user instance.
type procReaperInts map[int]bool

// ProcReaperSet returns the pids that reap this host's abandoned processes.
//
// WHY A SET AND NOT THE LITERAL 1. "Nobody will reap me any more" is a fact
// about the KERNEL's parenting, and the kernel does not express it as one
// number. On a plain host an abandoned process is reparented to init (ppid 1);
// on a host running a systemd user session the kernel installs `systemd --user`
// as the child-subreaper of user.slice, so the same abandoned process is
// reparented to THAT manager and carries its pid (measured on host-a:
// ppid 3100 = /lib/systemd/systemd --user, and never 1). Asserting the literal
// 1 is therefore asserting one host's reaper rather than the mechanism — the
// same mistake Rule 24 names for process independence (judge the mechanism,
// never a coincidental value).
//
// IDENTIFICATION, not shape-matching. A reaper is recognized by what it IS: pid
// 1, or a process whose comm is the systemd manager's AND which carries the
// `--user` flag on its own argv (the comm alone is shared with the system
// instance, so the flag is what makes "the user's own reaper" positive). A
// process that merely LOOKS like a reaper is not one, and a process that is not
// a reaper can never be a member of this set.
//
// IT FAILS LOUDLY, NEVER OPEN. The set is computed by reading every pid's
// parent out of the pid table, so an unreadable /proc or an implausible table
// (more than procReaperKnownUniverse processes parented to a reaper) returns an
// ERROR rather than a smaller set — a sweep that silently lost its eligibility
// test would report "no orphan found" for a scene full of them, which is the
// one failure mode this whole file exists to prevent (Rule 15b).
func ProcReaperSet() (procReaperInts, error) {
	reapers := procReaperInts{procname.OrphanReparentPPID: true}
	pids, err := ListPIDs()
	if err != nil {
		return nil, err
	}
	// Pass 1: every process the kernel has parented to a reaper — the only
	// population a reaper can be found in. Reading the pid table is how the
	// candidate is DISCOVERED; what it IS is decided in pass 2.
	db := newProcReaperDB()
	candidates := make([]int, 0, 8)
	for _, pid := range pids {
		if pid <= 0 {
			continue
		}
		parent, ok := db.parent(pid)
		if !ok {
			continue // gone, or not this user's to read — not a candidate
		}
		if !reapers[parent] {
			continue
		}
		candidates = append(candidates, pid)
		if len(candidates) > procReaperKnownUniverse {
			return nil, fmt.Errorf("reaper set: %d processes are parented to a reaper — more than the %d this reading believes (%s is unreadable in a meaningful way, or the host is not what it claims) — refusing to compute a reaper set from it",
				len(candidates), procReaperKnownUniverse, procDir)
		}
	}
	// Pass 2: keep only the candidates that ARE a user manager.
	for _, pid := range candidates {
		if db.isUserManager(pid) {
			reapers[pid] = true
		}
	}
	return reapers, nil
}

// procReaperDB is this package's pid-table reader for the reaper computation:
// an uncached parent lookup, and a small cache for the (comparatively
// expensive, argv-reading) user-manager test, so the two passes do not read the
// same /proc entry twice per candidate.
type procReaperDB struct {
	managerCache map[int]bool
}

func newProcReaperDB() *procReaperDB {
	return &procReaperDB{managerCache: make(map[int]bool)}
}

// parent reads pid's parent out of /proc/<pid>/stat. cmdline/comm are NOT read:
// the parent column is the only field this needs, and reading less is what
// makes the pid-table walk cheap.
func (db *procReaperDB) parent(pid int) (int, bool) {
	id, err := readProcStat(pid)
	if err != nil {
		return 0, false
	}
	return id.PPID, true
}

// isUserManager reports whether pid is a systemd USER instance: comm names the
// manager, and argv carries the `--user` flag. argv[0] alone would not do — the
// system manager is also `/lib/systemd/systemd` — so the flag is the
// discriminator, and it is read from the process's own cmdline, never guessed
// from a path.
func (db *procReaperDB) isUserManager(pid int) bool {
	if v, ok := db.managerCache[pid]; ok {
		return v
	}
	verdict := false
	if comm, err := readProcComm(pid); err == nil && comm == procname.UserManagerComm {
		if argv, err := readProcArgv(pid); err == nil {
			for _, word := range argv {
				if word == procname.UserManagerArg {
					verdict = true
					break
				}
			}
		}
	}
	db.managerCache[pid] = verdict
	return verdict
}

// readProcComm reads /proc/<pid>/comm, trimmed of the kernel's trailing
// newline. It is the NAME the kernel holds (stat field 2), which is what a
// reaper's own identity is expressed in.
func readProcComm(pid int) (string, error) {
	b, err := os.ReadFile(filepath.Join(procDir, strconv.Itoa(pid), procname.CommName))
	if err != nil {
		return "", fmt.Errorf("read comm of pid %d: %w (%w)", pid, err, ErrProcessGone)
	}
	return strings.TrimSpace(string(b)), nil
}

// readProcArgv reads /proc/<pid>/cmdline as its argv words, capped at
// procReaperArgv bytes. Unlike ProcIdentity.Cmdline (a rejoin of the whole
// vector for display and comparison) this keeps the words separate, because the
// question is "is THIS WORD present", and a rejoined string would match a word
// inside a quoted argument too.
func readProcArgv(pid int) ([]string, error) {
	b, err := os.ReadFile(filepath.Join(procDir, strconv.Itoa(pid), procCmdlineName))
	if err != nil {
		return nil, fmt.Errorf("read cmdline of pid %d: %w (%w)", pid, err, ErrProcessGone)
	}
	if len(b) > procReaperArgv {
		b = b[:procReaperArgv]
	}
	trimmed := strings.TrimRight(string(b), procCmdlineSep)
	if trimmed == "" {
		return nil, nil
	}
	return strings.Split(trimmed, procCmdlineSep), nil
}

// ProcIdentity is one process INCARNATION: its pid, the starttime read from
// /proc/<pid>/stat (field 22), its argv vector rejoined, and the state/ppid/
// session read from the same stat line.
type ProcIdentity struct {
	Pid       int
	StartTime uint64
	Cmdline   string
	// State is the /proc state letter (R/S/D/Z/…). A zombie is reported dead by
	// Alive() even though its pid entry still answers kill(pid, 0).
	State   byte
	PPID    int
	Session int
	// ProcReaper is the pid that would reap THIS process once its parent dies —
	// the kernel's child-subreaper of the reader's own user slice, which is init
	// (1) on a plain host and `systemd --user` on a host with a user session.
	// It is a fact about the READER's parenting (not about pid), so it is STAMPED
	// by the caller that measured it — ProcReaperOf, once per sweep — and a bare
	// ReadProcIdentity leaves it 0, meaning "unknown". Reading it per identity
	// would make one sweep walk the whole pid table once per candidate, and
	// stamping a guess would be the same defect as the literal 1 it replaces
	// (Rule 41: unmeasured evidence is marked, never assumed).
	ProcReaper int
	// ProcReaperChain is the pid chain from THIS process upward (parent, then
	// grandparent, … , up to and including the stopped ancestor), a fact about
	// the READER's own place in the tree — exactly like ProcReaper above, and
	// stamped by the same caller for the same reason.
	//
	// WHY THE PREDICATE NEEDS IT. An orphan is reparented to the NEAREST
	// ancestor marked PR_SET_CHILD_SUBREAPER, and to init only when none is
	// marked. Naming that ancestor is impossible from /proc below Linux 6.10 —
	// the mark is not readable there — and the process carrying it is NOT
	// necessarily systemd --user: a gopi run wrapper, a terminal multiplexer or
	// any launcher that called prctl(2) carries it too. The chain is the
	// visible half of that mechanism: a leaked process is a DESCENDANT of this
	// run, so when the kernel hands it to a marked ancestor, that ancestor is
	// on THIS chain by construction, whatever its name. A third party's
	// process is a descendant of some OTHER tree, so its parent is not on this
	// chain — which is what keeps eligibility from widening.
	//
	// EMPTY means "not stamped": the predicate then judges exactly as it did
	// before (procReaper > 0 keeps that arm a loud no, never a yes).
	ProcReaperChain []int
}

// ErrProcessGone reports a pid whose /proc entry cannot be read — the process is
// gone, or was never readable by this user.
var ErrProcessGone = errors.New("process is gone (no readable /proc entry)")

// ProcReaperOf returns the pid that reaps the CALLER's abandoned children, or
// (0, reason) when it cannot be measured. It is the reader Orphan() uses, and
// it exists so that the failure is reportable rather than silent: a caller that
// cannot name this host's reaper must say so, never fall back to the literal 1.
//
// THE MECHANISM IS THE KERNEL'S, NOT A GUESS: when a process dies, its orphaned
// children are reparented to the NEAREST ANCESTOR marked PR_SET_CHILD_SUBREAPER,
// and to init when no ancestor is marked. The reaper is therefore found by
// walking up THIS process's own ancestry and taking the first ancestor that IS
// one — never by looking for a reaper "parented to me", which is a different
// (and wrong) question: on this host the user session's manager (3100) is an
// ancestry ancestor with ppid 1, so the reaper of our leaks is a grandparent,
// not a child of ours.
//
// It is measured ONCE per process and cached: the answer is a property of this
// process's place in the tree, which does not change while it lives, while
// re-deriving it costs a pid-table walk (so the suite-start sweep would pay it
// once per candidate).
func ProcReaperOf() (int, string) {
	measureParenting()
	return procReaperPID, procReaperWhy
}

var (
	procReaperOnce        sync.Once
	procReaperPID         int
	procReaperWhy         string
	procReaperChain       []int
	procReaperChainClosed bool
)

// measureParenting runs the once-only measurement behind BOTH accessors below
// (the reaper and the ancestry chain): they are two halves of one fact about
// this process's place in the tree, and measuring them in one walk is what
// keeps them consistent with each other.
func measureParenting() {
	procReaperOnce.Do(func() {
		procReaperPID, procReaperWhy = measureProcReaper()
	})
}

// ProcReaperChainOf returns the pid chain above THIS process — parent,
// grandparent, … — and whether the walk reached the TOP of the tree (a closed
// chain). It is the second half of the parenting fact ProcReaperOf publishes,
// measured by the same once-only walk, and it exists for the eligibility
// predicate: see ProcIdentity.ProcReaperChain for why an orphan's reparent
// target is only recognizable as "one of ours" through this chain.
//
// The false (open) case is the LOUD one: a chain that could not be walked to the
// top of the tree (an unreadable ancestor, or a tree deeper than
// procReaperChainMaxLevels) means the predicate cannot tell a third party's
// process from its own leak, and it must refuse rather than guess (Rule 15b).
func ProcReaperChainOf() ([]int, bool) {
	measureParenting()
	return procReaperChain, procReaperChainClosed
}

// measureProcReaper is the once-only walk behind ProcReaperOf: it records the
// ancestry chain (for the eligibility predicate) and names the reaper among it.
//
// The bound (procReaperMaxParents) is what makes a corrupt ppid table a LOUD
// STOP instead of a hang: the walk is over a kernel-maintained tree, but a
// cycle in a table this process did not write is exactly the case where an
// unbounded loop would take the suite down, and "gave up after N levels" is a
// reportable fact (Rule 15b). The chain walk has its own, wider bound
// (procReaperChainMaxLevels) because it must reach init to be a mechanism.
func measureProcReaper() (int, string) {
	// The chain is measured FIRST and unconditionally: it is the fact the
	// eligibility predicate needs, and it must be available even on a host where
	// no reaper can be named (which is exactly the subreaper-launcher shape the
	// old enumeration got wrong).
	procReaperChain, procReaperChainClosed = walkAncestry()
	reapers, err := ProcReaperSet()
	if err != nil {
		return 0, err.Error()
	}
	for i, ancestor := range procReaperChain {
		if i >= procReaperMaxParents {
			break
		}
		if reapers[ancestor] {
			return ancestor, ""
		}
	}
	return 0, fmt.Sprintf("no reaper among this host's %d appears in the %d levels of ancestry above pid %d — a process leaked by this run would be reaped by something the sweep does not recognize, and the sweep must not pretend to know which",
		len(reapers), procReaperMaxParents, os.Getpid())
}

// walkAncestry returns the pids above this process — parent first, then
// grandparent, and so on — plus whether the walk reached the top of the tree.
//
// It walks by reading each level's parent out of /proc/<pid>/stat, which is the
// only channel for it below Linux 6.10. An ancestor that cannot be read (this
// user may not inspect a parent above their own session) leaves the chain OPEN,
// and an open chain is what makes the predicate refuse: a walk that stopped
// early cannot distinguish "a stranger's parent" from "a marked ancestor I could
// not see" (Rule 15b).
func walkAncestry() ([]int, bool) {
	db := newProcReaperDB()
	chain := make([]int, 0, procReaperChainMaxLevels)
	pid := os.Getpid()
	for i := 0; i < procReaperChainMaxLevels; i++ {
		parent, ok := db.parent(pid)
		if !ok {
			return chain, false // unreadable ancestor: the chain is NOT closed
		}
		if parent <= 0 || parent == pid {
			// The top of the tree. pid 1's own parent column is 0, so this is
			// the ordinary end of a complete walk — and a self-parented entry
			// (which a corrupt table can produce) is treated as an end rather
			// than followed, so it cannot spin the loop.
			return chain, true
		}
		chain = append(chain, parent)
		pid = parent
	}
	return chain, false // the cap was reached: a chain this long is not closed either
}

// procDescendMaxLevels bounds the ancestry walk ProcDescendsFrom performs. It is
// the same order of magnitude as the reaper chain cap: deeper than any tree this
// suite builds, shallow enough that a corrupt ppid column is a REPORTED stop
// rather than a hang.
const procDescendMaxLevels = 64

// ProcDescendsFrom reports whether pid is a DESCENDANT of ancestor — the pid
// itself is not counted, so ProcDescendsFrom(p, p) is false.
//
// WHY THIS EXISTS (Rule 42's precondition, made readable). "The caller must OWN
// the target" is a fact about a handle, and a caller that FOUND its target by
// scanning /proc holds no handle at all: the only ownership evidence available
// to it is the kernel's own parent column. An environment variable is NOT that
// evidence — it is INHERITED, so any process that was forked from the run that
// exported it carries it too (a re-executed `gopi worker`, a helper spawned by a
// sibling round), and matching on it alone hands a stranger to the caller as
// "my child".
//
// The walk reads one field per level out of /proc/<pid>/stat, so it costs a
// handful of file reads and no argv. It REFUSES rather than guesses: an
// unreadable level, a cycle, or an over-long chain comes back as ok=false with
// the reason, never as a silent "no" (a refusal that reads as "not mine" would
// turn every such target into a skip, which is how the defect hides).
func ProcDescendsFrom(pid, ancestor int) (bool, string) {
	if pid <= 0 || ancestor <= 0 {
		return false, fmt.Sprintf("pid %d / ancestor %d is not a process id", pid, ancestor)
	}
	db := newProcReaperDB()
	cur := pid
	for i := 0; i < procDescendMaxLevels; i++ {
		parent, ok := db.parent(cur)
		if !ok {
			return false, fmt.Sprintf("read the parent of pid %d while walking up from %d: the tree is unreadable here, so ownership cannot be established", cur, pid)
		}
		if parent <= 0 || parent == cur {
			return false, fmt.Sprintf("pid %d is not a descendant of %d: the walk reached the top of the tree (pid %d)", pid, ancestor, cur)
		}
		if parent == ancestor {
			return true, ""
		}
		cur = parent
	}
	return false, fmt.Sprintf("pid %d is not a descendant of %d within %d levels — the chain is too long to judge", pid, ancestor, procDescendMaxLevels)
}

// ReadProcIdentity reads the triple for pid.
func ReadProcIdentity(pid int) (ProcIdentity, error) {
	if pid <= 0 {
		return ProcIdentity{}, fmt.Errorf("pid %d is not a process id: %w", pid, ErrProcessGone)
	}
	id, err := readProcStat(pid)
	if err != nil {
		return ProcIdentity{}, err
	}
	// cmdline may legitimately be EMPTY (a zombie, a kernel thread). That is not
	// an error — it is part of the identity, and a mismatch against a non-empty
	// captured cmdline is exactly how a reused pid is caught.
	cmd, err := readProcCmdline(pid)
	if err != nil {
		return ProcIdentity{}, err
	}
	id.Cmdline = cmd
	return id, nil
}

const (
	// procArgvPublishPoll is the cadence of the wait AwaitProcIdentity performs
	// for a just-spawned child's argv. It is short because the window it polls
	// across is measured in hundreds of microseconds (see AwaitProcIdentity).
	procArgvPublishPoll = 500 * time.Microsecond
	// procArgvPublishBudget bounds that wait, and it is deliberately generous
	// against a window measured below one millisecond: the budget is not a
	// timing assertion, it is the bound that turns "never published" (a pid that
	// is not this run's fresh child at all) into a REPORTED refusal instead of a
	// hung suite.
	procArgvPublishBudget = 5 * time.Second
)

// ArgvUnpublished reports whether this identity was captured while its argv was
// not yet readable — a state MEASURED on a freshly spawned child, and NOT the
// same fact as "this process has no argv".
//
// THE MEASUREMENT (host-a, Linux, 2026-10-12; the probe is
// artifacts/capture-race-probe in the bug row's session dir): a child spawned by
// exec.Cmd.Start() was read the instant Start returned, and 194 of 200 reads
// found /proc/<pid>/cmdline EMPTY while the child was state R (running, NOT a
// zombie), with comm and /proc/<pid>/exe already resolved. The empty window
// closed 0.39 ms – 1.02 ms after Start returned. A later probe that read three
// other /proc entries BEFORE cmdline saw it empty only once in 120 spawns —
// i.e. the window is real and its width decides whether a read lands inside it,
// which is exactly why the bug it caused was flaky rather than deterministic.
//
// WHY IT IS NOT A PID REUSE, stated as the kernel fact it is: starttime (stat
// field 22) is fixed when the process is CREATED and an exec preserves it, so a
// captured identity and a later read of the same child carry the SAME starttime.
// A stored emptiness therefore cannot be evidence that the pid was recycled —
// judging it as one is judging a capture artifact as a fact about the process
// (memory #4487/#4492: a matching starttime with a differing cmdline is the SAME
// process; cmdline is an independent axis only for a record that carries no
// incarnation).
//
// THE ONE OTHER SHAPE THAT READS THIS WAY is a kernel thread (a user process
// with no argv and no zombie state letter), which is NOT what this predicate is
// about and never a capture target of this package — every fixture here spawns a
// user process, and a kernel thread is not addressable by an unprivileged pid
// scan of these tests. It is named here so the predicate's scope is visible:
// the caller that needs "argv is not yet readable" is a WAITER, and waiting on a
// kernel thread would time out and be REPORTED, never mistaken for success.
func (id ProcIdentity) ArgvUnpublished() bool {
	return id.Cmdline == "" && !id.Zombie()
}

// AwaitProcIdentity is the CAPTURE half of ReadProcIdentity for a process this
// run has just spawned: it reads the triple and, while the child's argv has not
// been published yet (ArgvUnpublished), polls until it has — returning a
// HALF-FILLED identity never.
//
// WHY A SEPARATE ENTRY POINT. ReadProcIdentity answers two different questions
// with one function, and only one of them may wait. A RE-CHECK ("is this
// recorded identity still that process?") must observe the process NOW and
// decide; making it wait would turn a liveness verdict into a guess with a
// delay. A CAPTURE ("what is this child I just spawned?") is the opposite: the
// child is known to be alive (Start returned its pid), and a read that lands
// inside the publication window does not describe the process, it describes the
// kernel's progress — so the honest answer is "not yet", not an identity whose
// empty argv will later read as a mismatch against its own process.
//
// The failure mode it removes is measured, not hypothetical: TestProcIdentity
// RefusesASignalledRecycledPID captured an empty argv and its own re-check read
// the published one, so the test reported `pid N was REUSED: now … cmdline="sleep
// 300", was … cmdline=""` — a REUSE verdict for one incarnation with one
// starttime, which is a contradiction. The same race was measured in
// daemonlifetime_test.go (whose stored cmdline was empty in the failure report
// of tasks/branch-audit-stale-merges-report.md) and mis-diagnosed there as "the
// watchdog fires too early".
//
// A pid that never publishes an argv (not this run's fresh child, already gone,
// or a shape this package does not capture) is a REPORTED refusal naming the
// budget and the last identity read — an error, never a silent half-fact.
func AwaitProcIdentity(pid int) (ProcIdentity, error) {
	return awaitProcIdentity(pid, ProcIdentityReader, procArgvPublishBudget)
}

// ProcIdentityReader is the reader AwaitProcIdentity polls. It is a named
// variable so the wait's DECISION RULE can be exercised without a live process
// (and so the one production reader stays the one implementation).
func ProcIdentityReader(pid int) (ProcIdentity, error) { return ReadProcIdentity(pid) }

// awaitProcIdentity is AwaitProcIdentity against an injectable reader and
// budget, which is what makes the wait's rule testable at all: a synthetic
// sequence (empty, empty, published) pins the retry, and a sequence that never
// publishes pins the refusal.
func awaitProcIdentity(pid int, read func(int) (ProcIdentity, error), budget time.Duration) (ProcIdentity, error) {
	deadline := time.Now().Add(budget)
	for {
		id, err := read(pid)
		if err != nil {
			return ProcIdentity{}, err
		}
		if !id.ArgvUnpublished() {
			return id, nil
		}
		if !time.Now().Before(deadline) {
			return ProcIdentity{}, fmt.Errorf("pid %d: its argv was still unpublished after %s of polling (%s) — refusing to return a half-filled identity whose empty cmdline would read as a mismatch against its own process",
				pid, budget, id)
		}
		time.Sleep(procArgvPublishPoll) //stylegate:allow-sleep bounded poll (procArgvPublishBudget) for a just-spawned child's argv to be published; the deadline owns the outcome and exits the loop
	}
}

// readProcStat parses the fields the triple and the detachment test need.
func readProcStat(pid int) (ProcIdentity, error) {
	b, err := os.ReadFile(filepath.Join(procDir, strconv.Itoa(pid), procStatName))
	if err != nil {
		return ProcIdentity{}, fmt.Errorf("read stat of pid %d: %w (%w)", pid, err, ErrProcessGone)
	}
	return parseProcStat(pid, string(b))
}

// parseProcStat is readProcStat's pure half (testable without a live process).
//
// It DELEGATES the field extraction to internal/procname.StatField — the ONE
// implementation of the count-from-the-last-')' rule this package used to carry
// a second copy of (Rules 13/14). The error shape is this package's own: a
// missing or non-numeric column is an error naming the pid and the column, never
// a zero-valued identity.
func parseProcStat(pid int, raw string) (ProcIdentity, error) {
	at := func(n int) (string, bool) { return procname.StatField(raw, n) }

	startRaw, ok := at(procStatStartTimeField)
	if !ok {
		return ProcIdentity{}, fmt.Errorf("stat of pid %d has no field %d (starttime): %q", pid, procStatStartTimeField, raw)
	}
	startTime, err := strconv.ParseUint(startRaw, 10, 64)
	if err != nil {
		return ProcIdentity{}, fmt.Errorf("stat of pid %d: starttime %q is not a number: %w", pid, startRaw, err)
	}
	ppidRaw, ok := at(procStatPPIDField)
	if !ok {
		return ProcIdentity{}, fmt.Errorf("stat of pid %d has no field %d (ppid): %q", pid, procStatPPIDField, raw)
	}
	ppid, err := strconv.Atoi(ppidRaw)
	if err != nil {
		return ProcIdentity{}, fmt.Errorf("stat of pid %d: ppid %q is not a number: %w", pid, ppidRaw, err)
	}
	sessionRaw, ok := at(procStatSessionField)
	if !ok {
		return ProcIdentity{}, fmt.Errorf("stat of pid %d has no field %d (session): %q", pid, procStatSessionField, raw)
	}
	session, err := strconv.Atoi(sessionRaw)
	if err != nil {
		return ProcIdentity{}, fmt.Errorf("stat of pid %d: session %q is not a number: %w", pid, sessionRaw, err)
	}
	state := byte('?')
	if s, ok := at(procStatStateField); ok && s != "" {
		state = s[0]
	}
	return ProcIdentity{Pid: pid, StartTime: startTime, State: state, PPID: ppid, Session: session}, nil
}

// readProcCmdline reads /proc/<pid>/cmdline as the argv words rejoined.
func readProcCmdline(pid int) (string, error) {
	b, err := os.ReadFile(filepath.Join(procDir, strconv.Itoa(pid), procCmdlineName))
	if err != nil {
		return "", fmt.Errorf("read cmdline of pid %d: %w (%w)", pid, err, ErrProcessGone)
	}
	return joinArgv(b), nil
}

// joinArgv turns a raw cmdline blob into the comparison identity.
func joinArgv(b []byte) string {
	trimmed := strings.TrimRight(string(b), procCmdlineSep)
	if trimmed == "" {
		return ""
	}
	return strings.Join(strings.Split(trimmed, procCmdlineSep), procArgvJoin)
}

// StampParenting returns id with THIS process's parenting facts stamped onto it:
// the reaper it can name, and its own ancestry chain. It is the ONE way to
// prepare an identity for Orphan(), because the two facts are halves of one
// measurement and a caller that stamped only one would silently weaken the
// predicate (measured: the sweep's own case helper did exactly that and judged a
// marked-subreaper leak with the name arm alone).
func (id ProcIdentity) StampParenting() ProcIdentity {
	reaper, _ := ProcReaperOf()
	chain, _ := ProcReaperChainOf()
	id.ProcReaper = reaper
	id.ProcReaperChain = chain
	return id
}

// Zombie reports whether the process has exited but not been reaped — kill(pid,
// 0) still succeeds for it, so a verdict based on that probe alone would call a
// corpse alive.
func (id ProcIdentity) Zombie() bool { return id.State == 'Z' }

// Detached reports whether the process is a session leader (pid == session): the
// shape a self-daemonized gopi face has, and the reason it outlives the scope
// that started it.
func (id ProcIdentity) Detached() bool { return id.Session == id.Pid }

// Orphan reports whether the process has been reparented to the process that
// REAPS this host — the signature of "my spawner died and I was left behind".
//
// THE JUDGEMENT IS THE MECHANISM, NOT A NUMBER. The literal form was
// `PPID == 1`, and it asserts that init is the reaper. The kernel does not
// promise that: when a process dies, its orphaned children are reparented to the
// NEAREST ANCESTOR marked PR_SET_CHILD_SUBREAPER, and to init only when no
// ancestor is marked. On a host running a systemd user session the kernel
// installs `systemd --user` in that role for user.slice, so a process that
// setsid(2)'d out of its session and was then abandoned is reparented to THAT
// manager — measured on this host as ppid 3100 (/lib/systemd/systemd --user)
// while pid 1's ppid is never seen at all (Rule 24's lesson applied to
// parenting: judge the mechanism, never a coincidental value).
//
// TWO ARMS, AND WHY THE SECOND ONE IS NOT AN ENUMERATION. The first arm is
// "my parent is the process that reaps ME" (ProcReaper == PPID) — the shape the
// original spelling was after. It is correct, but it can only ever fire when the
// reaper happens to be one of the two processes ProcReaperSet NAMES, and naming
// reapers is not the mechanism: below Linux 6.10 /proc does not expose the
// PR_SET_CHILD_SUBREAPER mark, so a launcher that carries it — a run wrapper, a
// terminal multiplexer, any program that called prctl(2) — is invisible to
// ProcReaperSet, and under such a launcher every leaked orphan carries THAT
// launcher's pid. Measured: two TestOrphanSweep cases RED at 30.06 s + 30.04 s
// (a 30 s discovery budget each) with the sweep's predicate reporting "no
// reaper" the whole time.
//
// The second arm therefore asks a question the kernel's own tree already
// answers: is my parent an ancestor of the SWEEPING process (ProcReaperChain)?
// A leaked process is a descendant of this run, so when the kernel hands it to a
// marked ancestor, that ancestor is above this run by construction — whatever it
// is CALLED. A third party's process is a descendant of another tree, so its
// parent is not on this chain, and the sweep leaves it alone: the predicate
// widens from "a reaper I can name" to "a reaper that is provably one of mine",
// and never to "anything".
//
// IT IS DETACHMENT-FREE ON PURPOSE. Detached() (session leader) is the OTHER
// half of the sweep's shape test and stays at the call site: a process
// reparented between two reapers without detaching is still ownerless, and
// folding the two facts together here would let a future reader think one
// implies the other.
//
// AN UNMEASURABLE PARENTING IS THE LOUD NO, NEVER A YES. Neither arm can be
// taken on faith: a chain that is empty or was not walked to the top of the tree
// means this reader cannot tell its own leak from a stranger's process, and the
// name arm alone is exactly the enumeration that failed (measured: two cases RED
// under a marked subreaper). Both are stamped together by
// ProcIdentity.StampParenting, so a caller cannot supply one and leave the other
// at its zero value by accident. Reporting false keeps the sweep's negative half
// honest (an identity that cannot judge is not evidence of a leak); the sweep
// reports the refusal separately, so this is a visible full-stop rather than a
// silent loosening (Rule 15b).
func (id ProcIdentity) Orphan() bool {
	if len(id.ProcReaperChain) == 0 {
		return false
	}
	if id.ProcReaper > 0 && id.PPID == id.ProcReaper {
		return true
	}
	for _, ancestor := range id.ProcReaperChain {
		if id.PPID == ancestor {
			return true
		}
	}
	return false
}

// String renders the triple for a log line.
func (id ProcIdentity) String() string {
	return fmt.Sprintf("pid=%d starttime=%d cmdline=%q", id.Pid, id.StartTime, id.Cmdline)
}

// Same reports whether other names the SAME incarnation. starttime is the
// discriminator that defeats pid reuse; cmdline catches a reassignment the
// kernel performed within one clock tick.
//
// It is the strict comparison, and it is deliberately NOT used on its own to
// decide liveness: a zombie's cmdline is empty by design, so Alive() applies the
// state test first (see its comment).
func (id ProcIdentity) Same(other ProcIdentity) bool {
	return id.Pid == other.Pid && id.StartTime == other.StartTime && id.Cmdline == other.Cmdline
}

// SameIncarnation is the state-blind half of Same: same pid AND same starttime.
// It is what a caller compares when it only needs "is this still that
// incarnation" and the process may legitimately be mid-exit (cmdline already
// emptied).
func (id ProcIdentity) SameIncarnation(other ProcIdentity) bool {
	return id.Pid == other.Pid && id.StartTime == other.StartTime
}

// Alive re-reads pid and reports whether it is still the SAME incarnation and
// not a zombie. The reason comes back with the verdict, so a caller never has to
// guess whether "false" meant exited, reused or unreadable (Rule 15b).
func (id ProcIdentity) Alive() (bool, string) {
	now, err := ReadProcIdentity(id.Pid)
	if err != nil {
		return false, err.Error()
	}
	// starttime FIRST: it is the incarnation's own identity, and it is the only
	// field that still means anything for a dead process.
	if now.StartTime != id.StartTime {
		return false, fmt.Sprintf("pid %d was REUSED: now %s, was %s", id.Pid, now, id)
	}
	// Then the state — BEFORE any cmdline comparison, because /proc/<pid>/cmdline
	// of a zombie is EMPTY by design (the kernel dropped the argument block), so
	// comparing cmdline first would report a zombie as a reused pid and lose the
	// diagnosis the caller needs.
	if now.Zombie() {
		return false, fmt.Sprintf("pid %d (%s) is a zombie", id.Pid, id.Cmdline)
	}
	if now.Cmdline != id.Cmdline {
		return false, fmt.Sprintf("pid %d was REUSED: now %s, was %s", id.Pid, now, id)
	}
	return true, ""
}

// KillIfIdentical signals pid ONLY if it is still exactly the incarnation the
// caller captured: the triple is re-read and compared immediately before the
// signal, which is what makes a pid-reuse race harmless.
//
// A mismatch is an error, never a silent skip: the caller's bookkeeping is then
// wrong about a live process, which is worth knowing.
func KillIfIdentical(id ProcIdentity, sig syscall.Signal) error {
	now, err := ReadProcIdentity(id.Pid)
	if err != nil {
		return fmt.Errorf("refuse to signal pid %d: %w", id.Pid, err)
	}
	// The INCARNATION test (pid+starttime), not the full triple: a process whose
	// cmdline has already changed (an exec, or the kernel emptying it as it
	// exits) is still the same process, and the point here is to never signal a
	// REUSED pid.
	if !now.SameIncarnation(id) {
		return fmt.Errorf("refuse to signal pid %d: identity changed (now %s, want %s) — a reused pid must never be signalled",
			id.Pid, now, id)
	}
	// THE INCARNATION CHECK IS HALF THE GUARD (measured 2026-10-17). It answers
	// "is this still the process I recorded"; it cannot answer "would delivering
	// here be catastrophic", and a delivery point that formed its pid some other
	// way (a pattern match, a group id, a sibling's pid, a ledger row written by
	// another run) hands it a coherent identity and it correctly agrees. The
	// second question is the one whose answer costs the operator a desktop
	// session: SIGINT to a systemd USER instance STOPS THE WHOLE USER SESSION
	// (journalctl -b "Received SIGINT from PID … (agent.test)", five times in one
	// boot on this host).
	if err := procname.RefuseLoginSessionManagerTarget(id.Pid); err != nil {
		return err
	}
	if err := syscall.Kill(id.Pid, sig); err != nil {
		return fmt.Errorf("signal pid %d (%s): %w", id.Pid, sig, err)
	}
	return nil
}

// EnvValue reads one variable out of a live process's environment
// (/proc/<pid>/environ) — the ownership test a /proc scan can trust (memory
// #2606: a worker-shaped process must be attributed by ITS OWN environment,
// never by its shape alone).
func EnvValue(pid int, name string) (string, error) {
	b, err := os.ReadFile(filepath.Join(procDir, strconv.Itoa(pid), procEnvironName))
	if err != nil {
		return "", fmt.Errorf("read environ of pid %d: %w (%w)", pid, err, ErrProcessGone)
	}
	for _, kv := range strings.Split(string(b), procCmdlineSep) {
		if v, ok := strings.CutPrefix(kv, name+"="); ok {
			return v, nil
		}
	}
	return "", nil
}

// Cwd reads a live process's working directory (/proc/<pid>/cwd) — the second
// ownership input for a daemon (the spawn seam hands every detached daemon a
// measured directory, so its cwd names the tree it belongs to).
func Cwd(pid int) (string, error) {
	p, err := os.Readlink(filepath.Join(procDir, strconv.Itoa(pid), procCwdName))
	if err != nil {
		return "", fmt.Errorf("read cwd of pid %d: %w (%w)", pid, err, ErrProcessGone)
	}
	return p, nil
}

// ListPIDs returns the numeric entries of /proc — the ONLY process enumeration
// this package performs. It is never paired with a signal by itself: every
// caller must additionally prove ownership (see orphansweep.go).
func ListPIDs() ([]int, error) {
	entries, err := os.ReadDir(procDir)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", procDir, err)
	}
	pids := make([]int, 0, len(entries))
	for _, e := range entries {
		pid, err := strconv.Atoi(e.Name())
		if err != nil {
			continue // not a pid entry (procfs also has "self", "meminfo", …)
		}
		pids = append(pids, pid)
	}
	return pids, nil
}

// SignalZeroAlive reports whether a pid still answers signal 0. It is the
// CHEAP probe — deliberately not sufficient on its own (a zombie answers it, and
// a recycled pid answers it for a stranger): callers that must be sure use
// ReadProcIdentity and the triple instead. It exists for the cases where a
// rough "is anything there" is enough, such as a fixture's cleanup check.
func SignalZeroAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	return syscall.Kill(pid, 0) == nil
}

// PathWithin reports whether p is inside root (or is root itself) — the
// path-scoped half of the orphan sweep, which never signals a process merely
// because its name looks right.
func PathWithin(root, p string) bool {
	if root == "" || p == "" {
		return false
	}
	root = strings.TrimRight(root, "/")
	if p == root {
		return true
	}
	return strings.HasPrefix(p, root+"/")
}
