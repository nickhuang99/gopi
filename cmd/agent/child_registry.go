package main

import (
	"sort"
	"sync"
	"sync/atomic"
	"time"
)

// --- the in-flight CHILD REGISTRY (shared with the worker/wrapper) --------
//
// ONE list of in-flight child processes, module-level, because the processes
// that need it are not the processes that spawn from it: `gopi task run` is a
// WRAPPER whose own child is the `gopi task` round (registered by
// runTaskRunOnce, cli_tasks.go), and the WRAPPER's signal handler is what must
// forward a stop to that child (forwardWorkerStopToChild,
// cli_tasks_worker_stop.go) and then WAIT for it (awaitWorkerStopChild /
// awaitWorkerDrainChild). Threading a handle through those call chains would put
// signal bookkeeping into the merge/run code.
//
// WHY IT NO LONGER PUBLISHES ANYTHING (epic merger-ff-park-file): the registry
// used to feed the merger's state FILE (a pass blocked on a resolve/verify
// child, since when, which work). The merger has no children any more, so that
// publisher is deleted with the state file — while the registry itself stays,
// because the task-run half is live and load-bearing: without it a stop signal
// reaches the wrapper but not the round it is running, and the wrapper's
// close-out (terminal writeback + the producer's own-worktree release) would
// wait on a child that never learns the run is over.

// passChild is one child process a wrapper is waiting on. Group is the child's
// own process GROUP id (== its pid whenever the caller set Setpgid), so a
// group-directed signal takes a child's grandchildren (a build, a test binary)
// along instead of leaving work running against the repo.
type passChild struct {
	Kind  string
	Label string
	PID   int
	Group int
	// Started is when this child's process was SPAWNED. Registration happens
	// immediately after a successful Start (so the entry never describes a
	// child that does not exist), which makes the registration instant the
	// child's start time.
	Started time.Time
}

// pendingPassChild is a child process registered BEFORE Start succeeded: its
// pid (and group) are unknown until then, so Kind/Label/Started travel alone.
// Splitting the two shapes keeps registerPassChild's contract exact — every
// entry in the registry always names a REAL, running pid (an entry for a child
// that never ran would make every graceful stop wait for a process that does
// not exist).
type pendingPassChild struct {
	Kind  string
	Label string
}

// passChildRegistry is the process-wide list of children currently in flight.
var passChildRegistry = struct {
	mu sync.Mutex
	m  map[int]passChild
}{m: map[int]passChild{}}

// registerPassChild records a child process that is ALREADY RUNNING (the caller
// calls it immediately after a successful Start) and returns the
// deregistration function its waiter must call (deferred immediately after, so
// no exit path — panic, early return, kill — can leak the entry). The entry's
// start time is stamped here: this instant IS the child's start.
func registerPassChild(c passChild) func() {
	if c.PID <= 0 {
		return func() {}
	}
	if c.Started.IsZero() {
		c.Started = time.Now()
	}
	passChildRegistry.mu.Lock()
	passChildRegistry.m[c.PID] = c
	passChildRegistry.mu.Unlock()
	return func() { unregisterPassChild(c.PID) }
}

// registerPendingPassChild reserves a registry slot for a child that is about
// to be started, and returns its upgrade (stampChild) and deregistration
// halves.
//
// WHY A RESERVATION IS NEEDED AT ALL: a child's pid does not exist until Start
// returns, but a wrapper must be able to say "I am waiting on this work" from
// the first moment. The slot carries no pid, so it is never mistaken for a
// running process (every liveness reader requires a positive pid).
//
// The upgrade is IDEMPOTENT: a caller that never reaches Start can drop the
// slot by calling stampChild(0, 0), and a caller that does can call it with the
// real pid — either way exactly one registry transition happens per pid.
func registerPendingPassChild(p pendingPassChild) (stampChild func(pid, group int), deregister func()) {
	key := passChildPendingKey()
	entry := passChild{Kind: p.Kind, Label: p.Label, Started: time.Now()}
	passChildRegistry.mu.Lock()
	passChildRegistry.m[key] = entry
	passChildRegistry.mu.Unlock()

	done := false
	var mu sync.Mutex
	stampChild = func(pid, group int) {
		mu.Lock()
		defer mu.Unlock()
		if done {
			return
		}
		done = true
		passChildRegistry.mu.Lock()
		delete(passChildRegistry.m, key)
		if pid > 0 {
			entry.PID, entry.Group = pid, group
			passChildRegistry.m[pid] = entry
		}
		passChildRegistry.mu.Unlock()
	}
	deregister = func() {
		mu.Lock()
		wasPending := !done
		done = true
		mu.Unlock()
		passChildRegistry.mu.Lock()
		if wasPending {
			delete(passChildRegistry.m, key)
		}
		passChildRegistry.mu.Unlock()
		if !wasPending && entry.PID > 0 {
			unregisterPassChild(entry.PID)
		}
	}
	return stampChild, deregister
}

// passChildPendingSeq mints registry keys for not-yet-started children. Pids
// are POSITIVE by definition on Linux, so a negative key can never collide with
// a real child's entry — and the reserved slot is therefore never read as a
// live process (all liveness readers require a positive pid).
var passChildPendingSeq int64

func passChildPendingKey() int {
	return -int(atomic.AddInt64(&passChildPendingSeq, 1))
}

// unregisterPassChild drops one registry entry by pid.
func unregisterPassChild(pid int) {
	passChildRegistry.mu.Lock()
	delete(passChildRegistry.m, pid)
	passChildRegistry.mu.Unlock()
}

// livePassChildren returns the in-flight children, sorted for deterministic
// reporting/tests: running children by pid ascending, then the not-yet-started
// reservations (no pid) ordered by the work they name.
func livePassChildren() []passChild {
	passChildRegistry.mu.Lock()
	defer passChildRegistry.mu.Unlock()
	out := make([]passChild, 0, len(passChildRegistry.m))
	for _, c := range passChildRegistry.m {
		out = append(out, c)
	}
	sort.Slice(out, func(i, j int) bool {
		pi, pj := out[i].PID, out[j].PID
		if (pi > 0) != (pj > 0) {
			return pi > 0
		}
		if pi != pj {
			return pi < pj
		}
		if out[i].Kind != out[j].Kind {
			return out[i].Kind < out[j].Kind
		}
		return out[i].Label < out[j].Label
	})
	return out
}

// waitForChildren blocks until no child is registered, up to budget, and
// returns whatever is still in flight. The reader is the worker's drain path; a
// caller passing a zero/negative budget gets a single read.
func waitForChildren(budget time.Duration) []passChild {
	deadline := time.Now().Add(budget)
	for {
		kids := livePassChildren()
		if len(kids) == 0 || time.Now().After(deadline) {
			return kids
		}
		time.Sleep(passChildSettlePoll) //stylegate:allow-sleep bounded settle wait for an in-flight child
	}
}

// passChildSettlePoll is that wait's poll cadence (Rule 1: named, one place).
const passChildSettlePoll = 50 * time.Millisecond

// passChildKindRun is the kind of the `gopi task run` CHILD a worker round /
// standalone wrapper is waiting on. Without a kind of its own the stop report
// could not tell "a merger resolve is in flight" from "the row's own execution
// is in flight" — the two need different operator reactions (and the merger,
// which no longer spawns either, has no kinds left to declare).
const passChildKindRun = "task-run-child"

// mergeVerifyChildRoleEnv is the role marker the merge-VERIFY child carried
// (epic merger-verify-gate, retired by epic merger-ff-park-file with the verify
// gate itself). The SPAWN is gone; the marker keeps its READER, because a child
// of a lock holder must not re-acquire the work lock (mergerWorkChildMarked) —
// and a child started by a still-running OLDER merger, or by a resolve child
// that survived a restart, is exactly such a child. Spelled once here (Rule 1b)
// rather than inline at the check, and read in exactly one place (Rule 21).
const mergeVerifyChildRoleEnv = "GOPI_MERGE_VERIFY_TASK"
