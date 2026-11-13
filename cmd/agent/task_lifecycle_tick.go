// The task-lifecycle ownership mark — and the ledger-reconciliation pass that
// used to sit beside it, RETIRED 2026-10-30 with every leg that had no carrier.
//
// WHAT USED TO LIVE HERE. A resident pass (runTaskLifecycleTick →
// taskLifecyclePass → taskLifecyclePassOnce) composed six legs: the stale
// report, the finish cleanup, the signal-terminated close-out, the WIP-snapshot
// sweep, the failed-needs-reissue census and the transport-dead reroute. Each
// leg's docstring (here and in internal/tools) named a carrier for it — "the
// tick both resident components already fire", "`gopi task reconcile` runs it
// by hand". MEASURED 2026-10-30, every one of those sentences was false by the
// time it was read: the worker round's call was removed by 187d05c (operator
// ruling 2026-10-26: a worker dispatches, it does not reconcile the ledger), the
// merger tick that was the other half was deleted 2026-09-19, and the
// `gopi task reconcile` one-shot was itself retired in 01a18799 (it is declared
// in taskverbs.RemovedVerbs and exits with a removal sentence). So the pass had
// NO production caller at all — its only callers were tests — and neither did
// any leg whose only caller was the pass. A duty documented as live and reached
// by nothing is a silent failure (Rule 15), and a docstring is not a fix for it.
//
// THE FATE OF EACH LEG, measured rather than assumed (the measurement is the
// `grep -rn … | grep -v _test.go` in the commit message; Rule 43's recipe). A
// leg was KEPT only when a live verb already carried its ENGINE, and DELETED
// with that engine when nothing did:
//
//	STALE REPORT      — DELETED 2026-11-05, whole: `staleSweep` →
//	                    `agentStaleSweep` (agent.DetectStaleTasks), its three
//	                    carriers (`gopi task list --sweep` and the `task run` /
//	                    `session resume` prologues), the persisted throttle stamp
//	                    (cmd/agent/tasklistsweep_gate.go), and the re-arm UPDATE.
//	                    The operator's ruling: no process watches another process
//	                    for it. A stale row is READ as stale wherever it is
//	                    displayed and the repair is the ledger's own verbs.
//	SIGNAL CLOSEOUT  — DELETED: engine (internal/tools/task_signal_closeout.go),
//	                    its tests, and the leg. The plan to hang it on
//	                    `gopi task show` was dropped — show is a pure read, and
//	                    this leg writes a row's status.
//	WIP SNAPSHOT     — the periodic sweep (`SnapshotLiveRunsPeriodic` + its
//	                    per-row throttle/cap) is DELETED. The snapshot CAPABILITY
//	                    is live on its own carrier: the `failed` writeback calls
//	                    SnapshotBeforeFailed (internal/tools/tasks.go), and the
//	                    successor mint names the deposit in the new row's payload.
//	REISSUE CENSUS   — the sweep wrapper is DELETED; the CENSUS is live in the
//	                    `gopi task list` render (failedReissueNote →
//	                    FailedNeedsReissue → FailedReissueNote).
//	TRANSPORT REROUTE— DELETED whole: the sweep, the mint engine
//	                    (internal/tools/task_reroute.go), its tests, and
//	                    task_transport_failure.go — whose ONLY reader was that
//	                    engine, i.e. a parser with no subject once the leg went.
//
// WHY NOTHING WAS GIVEN A NEW CARRIER INSTEAD. Rule 35 forbids inventing a
// timer, daemon, script or config key to hold a duty up, and every candidate
// here is a WRITE (a close-out, a mint, a reissue) that the operator has
// repeatedly ruled must not ride a worker's dispatch round or a read verb. The
// two duties with an independent reason to be written keep exactly the carriers
// they already had; the three without one were deleted rather than left to a
// scheduler that fires them into nothing. That is a fate decision, not a
// disappearance: the reasoning is in the commit message and here.
//
// WHAT REMAINS: the ownership mark below. It is NOT part of the pass — it
// records that a worker process is the task-lifecycle owner, and it is reached
// by `gopi worker` (the loop), `gopi worker <id>` and `gopi session resume`
// (acquireTaskLifecycleOwnership). Its original purpose (a second in-process
// tick skipping a duplicate pass) retired with the pass; the mark survives
// because releaseTaskLifecycleOwnership also establishes the one invariant a
// resident tick still owes — the row-owned-worktree remover seam is nil.
package main

import (
	"fmt"
	"os"
	"sync"
	"time"

	"github.com/nick/gopi/internal/tools"
)

// taskLifecycleOwnerNever is the owner-pid placeholder for "no worker loop was
// detected when the mark was taken" — non-zero on purpose (0 is the
// impossible-pid sentinel, and a mark must never read as "no mark").
const taskLifecycleOwnerNever = -1

// taskLifecycleOwnerLoop is the mark acquireTaskLifecycleOwnership sets and
// releaseTaskLifecycleOwnership clears; taskLifecycleOwnerAt is when it was
// taken. The release runs through a defer in every runner face, so a panicking
// or early-returning loop still clears the mark.
var (
	taskLifecycleOwnerMu   sync.Mutex
	taskLifecycleOwnerLoop bool
	taskLifecycleOwnerAt   time.Time
)

// acquireTaskLifecycleOwnership marks THIS process as the task-lifecycle owner
// and returns the release to defer. A second take while a mark is already held
// is a re-entrant runner face in one process — reported, because the inner
// face's release would otherwise clear the outer one's mark.
//
// The row-owned-worktree handover this mark used to wire
// (setFinishCleanupSweepOwnerRemoverWired) is RETIRED (epic sweep-report-not-act,
// 2026-09-19): the reaper REPORTS a row-owned pair with its owner evidence
// instead of handing the directory to that row's delivery chain, so no
// production face may be wired as a remover across a task-lifetime handover.
func acquireTaskLifecycleOwnership() func() {
	taskLifecycleOwnerMu.Lock()
	if taskLifecycleOwnerLoop {
		fmt.Fprintf(os.Stderr,
			"[%s] [lifecycle] a runner face in pid %d already owns task-lifetime duty — this one joins it (the mark is released when the LAST of them exits)\n",
			workerRoleLabel, os.Getpid())
	}
	taskLifecycleOwnerLoop = true
	taskLifecycleOwnerAt = time.Now()
	taskLifecycleOwnerMu.Unlock()
	return releaseTaskLifecycleOwnership
}

// releaseTaskLifecycleOwnership clears the mark. It is idempotent: a face that
// returns through several exits releases once.
//
// It also CLEARS the row-owned-worktree handover seam, so a one-shot task agent
// that rewires the package-level seam cannot leave the remover pointing into
// its own closed ledger session. That clear is the invariant this function
// still owes production (the mark's second half — a resident tick yielding to
// the owner — retired with the pass).
func releaseTaskLifecycleOwnership() {
	tools.SetFinishCleanupSweepOwnerRemover(nil)
	taskLifecycleOwnerMu.Lock()
	taskLifecycleOwnerLoop = false
	taskLifecycleOwnerAt = time.Time{}
	taskLifecycleOwnerMu.Unlock()
}
