// cwd_release_gate_test.go — the mechanical gate for the worktree-release CWD
// obligation (PROJECT.md Rule 6a's producer half; epic fix/cwd-release-coverage,
// 2026-09-14).
//
// THE DEFECT THIS GATE FREEZES: `cmd/agent` carried ONE cwd release — the
// chdir the `gopi task run` close-out performed — while four other process
// paths could stand inside a task's linked worktree and never released it: the
// worker round close-out, the merger pass, the merger's resolve/verify
// teardowns (which REMOVE a worktree), and the executor child's exit.
//
// WHY IT COSTS A WORKTREE FOREVER: Linux cannot move another process's cwd, so
// the holder is the ONLY party that can let go of it. A process standing in a
// removed worktree keeps the directory's inode (and every file under it) alive
// until it exits — and the long-lived faces (a worker loop, a merger daemon, a
// resumed session) outlive many rounds. finishCleanup's guard ③ ("no live
// process with cwd inside the tree") then refuses forever, `git worktree list`
// keeps naming the directory, scripts/safe-merge.sh gate [c] refuses the
// branch, and the finished deliverable is never merged — with nothing anywhere
// saying why.
//
// WHY A SOURCE GATE AND NOT A REVIEW NOTE: the coordinator's own standard
// ("原则无效，检查才有效" — a principle is worthless, a check is what counts).
// The four missing paths were not an oversight of knowledge — the function's
// own comment ALREADY named "the worker loop, a serve tick" as long-lived
// callers that outlive many rounds. Knowing was not enough; the next path that
// removes a worktree will know just as well. So the tree refuses to carry a
// worktree-removing producer in cmd/agent that does not name the release
// implementation.
//
// PRECISION (why this does not cry wolf): the obligation is a property of a
// PROCESS, and `cmd/agent` is exactly the tree where each file's code runs in
// the process it names. The library trees are deliberately out of scope —
// internal/tools' reclaim arms run in the CALLER's process (and chdir'ing the
// caller from a library sweep over other rows would be wrong), and
// internal/agent's executor is covered once, at its exit funnel in loop.go. A
// gate that flagged the libraries would demand a chdir where the ownership
// contract says not to put one.
package standardgate

import (
	"fmt"
	"go/scanner"
	"go/token"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
)

const (
	// cwdReleaseTrees are the trees the gate scans: the process faces. Both are
	// process-owning code — cmd/agent is where the wrapper/worker/merger loops
	// live, internal/agent is the executor child — and a worktree-removing
	// producer in either one runs in the process it names. The library trees
	// (internal/tools) are deliberately EXCLUDED: their reclaim arms run in the
	// CALLER's process and operate on OTHER rows, where chdir'ing the caller
	// would be wrong (see cwdReleaseViolation's precision note).
	cwdReleaseTrees = "cmd/agent\x00internal/agent"

	// cwdReleaseTreeSep splits the scanned-tree list (Rule 1: one home).
	cwdReleaseTreeSep = "\x00"

	// cwdReleaseToolsTree is the library the scanned trees reach their
	// worktree REMOVALS through. It is not scanned for violations (its arms run
	// in the CALLER's process — see cwdReleaseTrees), it is walked by the
	// census test to DERIVE the arm set this gate's triggers must cover: an
	// exported function of this tree that reaches a removal verb (directly or
	// through other functions of the tree) IS a removal arm, whether or not
	// anyone remembered to name it below.
	cwdReleaseToolsTree = "internal/tools"

	// cwdReleaseArmsNotListed names the DERIVED removal arms that are
	// deliberately not triggers, with the reason (Rule 15: silence is not a
	// policy). TestGateCwdReleaseCensusArmsAreListed asserts every derived arm
	// is either trigger-covered or named here — an arm that lands in neither
	// fails the gate, which is exactly the hole a hand-written phrase list
	// leaves open (measured 2026-09-16: three reachers were being CALLED from
	// the scanned trees while listed nowhere).
	//
	//   internal/tools.TaskMerge    the merge face. It reaches a removal only
	//   internal/tools.TaskVerify   through the private-scratch class below.
	//
	// The next two are the PLAN faces of the close-out chain: they classify
	// what WOULD be removed (planOnly) and remove nothing, so a file that calls
	// only them cannot be standing in a removed directory. The removal face is
	// TaskFinishCleanupSessionID — a trigger.
	//
	//   internal/tools.FinishCleanupPlan
	//   internal/tools.TaskFinishCleanupPlan
	//
	//   internal/tools.ClassifyVetGateRevert   the post-merge VET gate's
	//       classification arm. It reaches a removal only through the SAME
	//       private-scratch class as TaskMerge/TaskVerify: its baseline
	//       measurement materialises the recorded pre-merge tip in a scratch
	//       worktree (mergeBuildGateScratch) and reaps that registration with
	//       an explicit `-C <repo>` and an explicit dir. No producer ever
	//       chdir'd into one, so demanding a release of a caller that was never
	//       in the directory is the same false-positive class. (Derived
	//       2026-09-17, when this arm's baseline comparison first reached the
	//       reap; it is a REPORT, and the report was correct — the arm existed
	//       and was listed nowhere, which is what this list is for.)
	//
	//   internal/tools.RebuildAfterMergeMergeset   the post-merge merged-result
	//       gate (rebuild_vet_mergeset.go). Same private-scratch class, derived
	//       the same way and for the same reason as ClassifyVetGateRevert: it
	//       materialises the merge commit (and, for attribution, its first
	//       parent) in the scratch worktrees mergeBuildGateScratch creates, and
	//       reaps those registrations with an explicit `-C <repo>` and an
	//       explicit dir. No producer ever chdir'd into one — the step runs
	//       every toolchain command with cmd.Dir set to the checkout — so
	//       demanding a release of a caller that was never in the directory is
	//       that same false-positive class. (Derived 2026-09-18 on this arm's
	//       first landing, when its worktree add/reap reached the census; the
	//       report was correct — the arm existed and was listed nowhere, which
	//       is what this list is for.)
	//
	//   internal/tools.RebuildAfterMerge        the close loop itself, which
	//       invokes the arm above. It removes nothing on its OWN behalf — its
	//       only removal path is the call — but the census's criterion is
	//       transitive (a call to it IS a call to a removal), so it is recorded
	//       here rather than left to report as missing. (Derived 2026-09-18,
	//       immediately after the arm above: the same landing, one hop up.)
	//
	// WHY TaskMerge/TaskVerify ARE EXEMPT (the private-scratch class): their
	// removal target is a SCRATCH worktree the same call created
	// (internal/tools mergeBuildGateScratch / taskVerifyGround), removed with an
	// explicit `-C <repo>` and an explicit Dir — no producer ever chdir'd into
	// one. Demanding a chdir of a caller that was never in the directory is the
	// false-positive class cwdReleaseTrees already excludes internal/tools for.
	cwdReleaseArmsNotListed = "TaskMerge\x00" +
		"TaskVerify\x00" +
		"FinishCleanupPlan\x00" +
		"TaskFinishCleanupPlan\x00" +
		"ClassifyVetGateRevert\x00" +
		"RebuildAfterMergeMergeset\x00" +
		"RebuildAfterMerge\x00" +
		// RunGroundCollect removes a linked worktree REGISTRATION whose run
		// ground proved unowned (RunGroundAnchorLiveness) and re-asks the
		// occupancy question immediately before the act. It is called by the
		// merge tick / sweep, which never chdirs into the ground — the
		// process cwd is not inside the directory being removed, so there is
		// no cwd to release (the obligation the release library exists for).
		"RunGroundCollect"

	// cwdReleaseArmsNotListedSep splits the deliberate-omission list above.
	cwdReleaseArmsNotListedSep = "\x00"

	// cwdReleaseArmFloor is the census's own non-vacuity tripwire: the walk of
	// cwdReleaseToolsTree must DERIVE at least this many exported removal arms.
	// Measured on master 16ec3437 (the census's landing): 13 exported arms
	// (MergerRepoSweep, MergedCleanupSweep, ReclaimDeadHolderWorktrees,
	// ReclaimOwnFinishedWorktrees{,Within}, ReclaimSelfOwnRegisteredWorktrees
	// {,Within}, FinishCleanupPlan, TaskFinishCleanupPlan,
	// TaskFinishCleanupSessionID, TaskMerge, TaskVerify, WorktreeSweepRun). If
	// the walk matches nothing, the census proves nothing and says so (the
	// same read-floor contract as the env-reader gate, PROJECT.md Rule 21).
	cwdReleaseArmFloor = 10

	// cwdReleaseMarker is the ONLY thing that satisfies the gate — an actual
	// CALL into the single release implementation (internal/worktreerelease).
	// A bare os.Chdir would NOT satisfy it: Rules 13/14, one implementation of
	// the chdir, referenced everywhere, never a copy.
	//
	// It is the CALL, not the package qualifier, and that distinction was
	// measured: with the marker "worktreerelease." the gate stayed GREEN after
	// the release line was deleted from a producer — the file's own IMPORT
	// path still carried the qualifier. A file that imports the package and
	// never calls it is exactly the omission the gate exists to catch, so the
	// marker names the exported entry points (both live on the same prefix, so
	// one constant covers Release and ReleaseReporting — Rule 1b: the value
	// comes from here, never typed inline).
	cwdReleaseMarker = "worktreerelease.Release"

	// cwdReleaseRemovalTriggers are the worktree-REMOVING seams reachable from
	// the process faces. Each is a verb the file passes to git, or an arm whose
	// whole purpose is to remove a linked worktree's registration. Spelled once
	// (Rule 1b): the predicate and its probes read this list.
	//
	// WHAT QUALIFIES — a size criterion, not a naming style (re-read 2026-09-16,
	// when the floor below went red): a name belongs here when a CALL to it is a
	// worktree REMOVAL, because then a file that calls it WITHOUT calling the
	// release is a file that may still be standing in the directory it just
	// unregistered. Deliberately NOT listed (with the reason, Rule 15: silence
	// is not a policy):
	//
	//   internal/tools.ReclaimHeldBranches   branch-only — it deletes ORPHAN
	//       refs, never a linked worktree's registration (no production caller
	//       outside internal/tools).
	//   internal/tools.ReclaimRunResidue*    OWNERSHIP-BY-OTHER check — its
	//       header says the caller must NOT be the worktree's holder; it returns
	//       a typed outcome and is a liveness probe for a row whose holder may
	//       be another process. Demanding a chdir of a caller the contract says
	//       cannot be the holder is the false-positive class cwdReleaseTrees
	//       already excludes the library trees for.
	//   internal/tools.NotifySyntheticEvent  a heartbeat event renderer that
	//       mentions the removal in prose only; it removes nothing (the census
	//       below still reports it, because a prose mention makes its enclosing
	//       function look like a remover — the price of a source census).
	//
	// WIDENED 2026-09-16 (epic: verify the inventory by CENSUS, not by phrase
	// list — this verification's finding). The list a phrase list can only
	// APPROXIMATE: a producer that calls an arm through a spelling nobody
	// thought of slips past a trigger list while still removing a worktree. So
	// the arm set is now DERIVED, not recalled — TestGateCwdReleaseCensus-
	// ArmsAreListed walks every production source of this list's own tree
	// (cwdReleaseToolsTree) to fixpoint and asserts each derived removal arm is
	// either listed here or listed in cwdReleaseArmsNotListed (with its reason).
	// The three arms this widening added are exactly what that walk found being
	// CALLED from the scanned trees while unlisted:
	//
	//   internal/tools.WorktreeSweepRun          cmd/agent `task sweep-worktrees`
	//   internal/tools.MergedCleanupSweep        cmd/agent merged-cleanup pass
	//   internal/tools.ReclaimDeadHolderWorktrees cmd/agent merger dead-holder leg
	//
	// A new arm that DOES remove a linked worktree and is called from a scanned
	// tree belongs on this list — that is the point of the constant being the
	// one home for the trigger set, and the census test is what makes the
	// omission red instead of silent.
	//
	// NAMED 2026-09-26 (this branch, feat/standards-reds): the census's first
	// red after then-master's close-out SEAM work landed, whose two exported
	// faces are BOTH genuine removal arms. THE MECHANISM THAT INSPIRED THEM IS
	// GONE — the run close-out reap was deleted whole on 2026-11-05 in 386731b5
	// (22 files, −7597 lines), because its ownership test accepted an INHERITED
	// env record and SIGKILLed a nested run's children. The two faces below are
	// a DIFFERENT capability that survived it (a run releasing its own ground):
	//
	//   internal/tools.ReclaimOwnWorktreesAtCloseout       the seam itself. Its
	//   internal/tools.ReclaimOwnWorktreesAtCloseoutWithin  budget-guarded wrapper.
	//
	// Both are remover TRANSITIVELY and the census is right to derive them: the
	// seam calls the two ownership-independent halves, each of which reaches
	// reclaimGitRemove (task_reclaim_self.go) — the git `worktree remove` the
	// whole gate exists around. So they are triggers, NOT entries in
	// cwdReleaseArmsNotListed: everything in that list removes a PRIVATE SCRATCH
	// worktree the same call created, where no producer ever stood, and the
	// close-out seam is the exact OPPOSITE — it releases the ground the run is
	// standing in, which is the obligation this gate demands a chdir for.
	//
	// Verified (2026-09-26) rather than assumed: across the whole tree the two
	// faces have exactly TWO production callers, and both already call the
	// release — cmd/agent/cli_tasks.go (taskRunReclaimOwnDirectories, the second
	// step of the run's close-out, followed by worktreerelease.ReleaseReporting)
	// and cmd/agent/cli_tasks_worker.go (workerRoundOwnWorktrees, same). The four
	// earlier separate callers are gone: the seam REPLACED them (its own header
	// measures why two calls ran the second half unmarked), so this is an arm
	// arriving, not a producer losing its release. (The line numbers this note
	// used to carry went stale in 386731b5 — the close-out reap's deletion moved
	// both sites; the FUNCTIONS are the stable address, so they are what is named.)
	//
	// Listing them HERE (not exempting them) is what keeps the gate honest on
	// the next producer: a file that reaches a worktree removal through the
	// close-out seam and never releases is the very omission the gate reports,
	// and an exemption would have made that file invisible.
	//
	// NAMED 2026-10-12 (this branch, task/01a12e97): the census's next red, and
	// it arrived the same way the seam arms did — a new library path landed
	// under internal/tools and the walk derived three exported faces of it as
	// removal arms:
	//
	//   internal/tools.DeliveryRetryTaskID
	//   internal/tools.DeliveryRetryLines
	//   internal/tools.DeliveryRetryJSON
	//
	// the blocked-delivery REDELIVERY path (task_delivery_retry.go). They are
	// TRIGGERS, not exemptions, and the distinction is the same one the seam
	// arms above turn on: these arms reach a REAL removal of the row's OWN
	// ground — they re-run the delivery attempt, and the delivery attempt is
	// the close-out chain. They are not the private-scratch class.
	//
	// MEASURED, not assumed — every production call site of the three, with its
	// release status, at the granularity this gate actually reads (a FILE, not
	// a call path):
	//
	//   cmd/agent/cli_tasks.go:4040   `DeliveryRetryLines` on the serve
	//                                 lifecycle pass's delivery-retry leg. The
	//                                 file NAMES the release
	//                                 (taskReclaimOwnDirectories →
	//                                 worktreerelease.ReleaseReporting,
	//                                 cli_tasks.go:3223)… passed.
	//                                 HONEST SCOPE: the release sits on the run
	//                                 close-out while this arm rides the resident
	//                                 lifecycle pass, so "the file releases" is
	//                                 all this gate measures here — a chdir for
	//                                 THIS pass specifically is NOT asserted.
	//
	//   cli_tasks_retry_delivery.go (now deleted with the verb)  `gopi task retry-delivery`, the
	//                                 verb face that reaches the arms directly.
	//                                 It carried NO release when the census first
	//                                 derived these arms, and 6965883fc added one
	//                                 on the close-out path (the call the
	//                                 classification proof re-measures, leg ④).
	//                                 That omission is the whole reason a census
	//                                 beats a hand-written list (Rule 31).
	//                                 RETIRED 2026-10-30 (task 01a18799, slice
	//                                 2/4): the VERB was retired and this file was
	//                                 deleted with it (Rule 43 — its body WAS the
	//                                 verb). The arms and the obligation outlive
	//                                 it; the producer that reaches them is
	//                                 cmd/agent/cli_tasks.go's finish-cleanup
	//                                 delivery-retry leg, which is what
	//                                 cwdReleaseDeliveryRetryProducer names now.
	//
	// RETIRED 2026-10-31 (task/01a18ec8): four entries were DELETED from this
	// list, and the decision is recorded here rather than left as a silent
	// subtraction to quiet a red (Rule 15: silence is not a policy).
	//
	//   TaskFinishCleanupSessionID   the finish-cleanup mechanism was retired
	//       whole in 58b5618b ("refactor(task): retire the finish-cleanup
	//       mechanism (slice 3/4)"). The name survives ONLY as a fixtures-only
	//       TEST double — internal/tools/finish_cleanup_moved_helpers_test.go,
	//       where a `_test.go` function of that name still RUNS the retired
	//       chain so live tests can exercise what used to call it. The census
	//       walk skips `_test.go` BY DESIGN (cwdReleaseCensus reads the
	//       production files of cwdReleaseToolsTree), so it no longer derives
	//       the arm, and an entry nothing derives reads as coverage while
	//       covering nothing. Measured on this tree:
	//
	//         $ git grep -n 'TaskFinishCleanupSessionID' -- '*.go'
	//         internal/tools/finish_cleanup_moved_helpers_test.go:252:func TaskFinishCleanupSessionID(taskID string) (*FinishCleanupReport, error) {
	//
	//       — the ONLY `func` definition, and it is a test file. Every other
	//       hit is a comment recording the retirement (cmd/agent/cli_resume.go
	//       :1227 "used to run tools.TaskFinishCleanupSessionID", internal/agent
	//       /taskregister.go:794), i.e. a
	//       memory of the arm, not a caller of it.
	//
	//   DeliveryRetryTaskID / DeliveryRetryLines / DeliveryRetryJSON   the three
	//       blocked-delivery re-delivery faces (internal/tools/task_delivery_
	//       retry.go). They were triggers for ONE reason: they reached the
	//       worktree+branch close-out chain, i.e. a call to them could remove a
	//       directory a producer was standing in. Both halves of that reason are
	//       gone, and the second is the one my own first draft of this note got
	//       WRONG, so it is written out here as the gate measured it:
	//
	//         1. the close-out CHAIN ASK was retired first. The file's own note
	//            says so (task_delivery_retry.go:365-367): "THE CLOSE-OUT CHAIN
	//            ASK IS RETIRED (2026-10-30, task 01a18799, slice 3/4). The
	//            retry used to hand the row to the worktree+branch close-out
	//            chain and report that chain's verdict as the attempt's detail.
	//            The chain is gone", and the census
	//            agrees: task_delivery_retry.go no longer reaches a removal verb,
	//            so the walk does NOT derive these three as arms any more
	//            (derived: ClassifyVetGateRevert, MergedCleanupSweep,
	//            MergerRepoSweep, RebuildAfterMerge{,Mergeset},
	//            ReclaimDeadHolderWorktrees, ReclaimOwnFinishedWorktrees{,Within},
	//            ReclaimOwnWorktreesAtCloseout{,Within},
	//            ReclaimSelfOwnRegisteredWorktrees{,Within}, RunGroundCollect,
	//            TaskMerge, WorktreeSweepRun — three names absent).
	//         2. the PRODUCER was already gone. Measured on this tree:
	//
	//              $ git grep -n 'DeliveryRetryTaskID\|DeliveryRetryLines\|DeliveryRetryJSON' -- cmd/agent internal/agent
	//              (no output)
	//
	//            Every remaining reference lives inside internal/tools: the
	//            arms' own definitions, doc comments and `_test.go` callers. The
	//            file states the same thing about itself, as an HONEST GAP
	//            (task_delivery_retry.go:372-376): "DeliveryRetryTaskID has no
	//            production caller today (the `retry-delivery` verb was retired
	//            in slice 2/4), so this file is a library with tests, not a live
	//            leg."
	//
	//       A name the census cannot derive is not a coverage claim — it is dead
	//       weight that reads green (TestGateCwdReleaseCensusArmsAreListed's own
	//       sentence). So both trigger entries go. They are NOT moved to
	//       cwdReleaseArmsNotListed either: that list is the PRIVATE-SCRATCH
	//       class — worktrees a call created and reaped itself, that no producer
	//       ever stood in — and it is the wrong verdict for these arms, not a
	//       safe default.
	//
	//       Whether the ARMS THEMSELVES should be retired is a different
	//       decision and is NOT taken here: they are still exported library
	//       faces (their tests still run), and the file's own note says removal
	//       belongs with the delivery-owed event family that
	//       task_delivery_owed_*.go still writes and scans. What this change
	//       settles is only the TRIGGER classification, which is a claim about
	//       scanned-tree producers and is now false.
	//
	// Where that leaves the verdicts: each of the four names is absent from BOTH
	// this list and cwdReleaseArmsNotListed (a name in both cancels out and
	// reads a false green — TestGateCwdReleaseCensusArmsAreListed, line 952).
	// The read proofs that used to pin them are retired WITH them, which is what
	// lets a later landing that re-introduces a production caller turn the
	// census red again by itself, with no test and no list to remember.
	// cwdReleaseDeliveryRetryRetirement is the machine-readable form of the
	// second decision, and TestGateCwdReleaseDeliveryRetryRetirement is the leg
	// that fires when either half of it stops being true.
	//
	// The trigger entries are checked by TestGateCwdReleaseCensusFindsTheRealArms
	// (derivation + "not also exempt") and by
	// TestGateCwdReleaseCensusArmsAreListed (a trigger that matches no derived
	// arm), so a walk that stops deriving a listed trigger is red rather than a
	// green run carrying dead weight.
	cwdReleaseRemovalTriggers = "\"worktree\", \"remove\"\x00" +
		"ReclaimOwnFinishedWorktrees\x00" +
		"ReclaimOwnWorktreesAtCloseout\x00" +
		"reclaimGitRemove\x00" +
		"WorktreeSweepRun\x00" +
		"MergedCleanupSweep\x00" +
		"MergerRepoSweep\x00" +
		"ReclaimSelfOwnRegisteredWorktrees\x00" +
		"ReclaimDeadHolderWorktrees"
	// The three ledger-keyed re-delivery arms are NO LONGER concatenated here:
	// the census stopped deriving them once the finish-cleanup mechanism was
	// retired (58b5618b) and the serve lifecycle pass's delivery-retry leg went
	// with it. cwdReleaseDeliveryRetryArms keeps the one spelling of those
	// names, and cwdReleaseDeliveryRetryRetirement records the decision and
	// re-measures it — an entry the census cannot derive is exactly the red this
	// change closes (see the RETIRED block above cwdReleaseRemovalTriggers).

	// cwdReleaseTriggerSep is the in-band separator of the trigger list above
	// (a NUL byte cannot occur in Go source, so a trigger can never be forged
	// by splitting across two entries).
	cwdReleaseTriggerSep = "\x00"

	// cwdReleaseDeliveryRetryArms names the three arms the 2026-10-12 census
	// derived from internal/tools/task_delivery_retry.go. RETIRED as a TRIGGER
	// SET on 2026-10-31 (task/01a18ec8): cwdReleaseDeliveryRetryRetirement below
	// carries the decision, and the RETIRED block on cwdReleaseRemovalTriggers
	// carries the measurements behind it. What survives here is the ONE spelling
	// of the three names (Rule 1b) — the retirement leg reads it for every claim
	// it measures — together with the part of the original reasoning that still
	// holds: the answer for these arms was NEVER an entry in
	// cwdReleaseArmsNotListed, because that list is the PRIVATE-SCRATCH class
	// and these remove the row's OWN ground. (The other half — "they reach the
	// close-out chain, so a caller of one can remove a directory a producer
	// stands in" — is what stopped being true: the chain ask itself was retired
	// before the producer went.)
	//
	// RECONCILED 2026-10-12 (task/01a12f00…): this constant and
	// cwdReleaseDeliveryRetryProducer arrive from the SIBLING branch
	// task/01a12ebe6d3b739caa65122cf48d3592 — the same body of work as
	// task/01a12ea040a87e54865a2df000480cd4, and the sibling carried the
	// stronger form (one spelling + a producer measurement, rather than three
	// literals plus a hand-kept list of special names). Both branches collided
	// with master, which had already landed the classification in 6965883fc
	// while this branch was open, so the reconciliation keeps master's landed
	// semantics and takes the sibling's structure.
	cwdReleaseDeliveryRetryArms = "DeliveryRetryTaskID" + cwdReleaseTriggerSep +
		"DeliveryRetryLines" + cwdReleaseTriggerSep +
		"DeliveryRetryJSON"

	// cwdReleaseDeliveryRetryProducer is the scanned-tree file that USED to be
	// the producer of the three re-delivery arms — the constant is kept because
	// the retirement decision is a measurement ABOUT this file: the arms were
	// triggers only while it called them, and the retirement leg
	// (TestGateCwdReleaseDeliveryRetryRetirement) re-measures that it no longer
	// does. It is no longer read by a classification proof, because there is no
	// classification left to prove: the arms are not triggers.
	//
	// Its history is the reason the arms were revisited at all. It started as
	// `cli_tasks_retry_delivery.go` (the `gopi task retry-delivery` verb face,
	// deleted with that verb — task 01a18799, slice 2/4), and was retargeted at
	// the serve lifecycle pass's delivery-retry leg in cmd/agent/cli_tasks.go
	// (:4607 tools.DeliveryRetryOwedRows, :4610 tools.DeliveryRetryLines), which
	// the classification proof then measured for BOTH halves of the obligation —
	// a code call to an arm, and a reference to the release.
	//
	// RETIRED AS A PRODUCER 2026-10-31 (task/01a18ec8): that leg went with the
	// finish-cleanup mechanism (58b5618b, "slice 3/4"), so this file now calls
	// none of the three arms. Measured on this tree:
	//
	//	$ git grep -n 'DeliveryRetryTaskID\|DeliveryRetryLines\|DeliveryRetryJSON' -- cmd/agent internal/agent
	//	(no output)
	//
	// — no production caller of any of the three anywhere in either scanned
	// tree, which is what retired the trigger entries (Rule 15: the decision is
	// recorded here, not implied by a deletion) and what makes the neighbouring
	// classification proof's leg ④ a measurement nothing owes any more.
	cwdReleaseDeliveryRetryProducer = "cmd/agent/cli_tasks.go"

	// cwdReleaseDeliveryRetryLib is the library file the three re-delivery arms
	// are DEFINED in, named once so the retirement proof (① "still exported")
	// reads a constant instead of typing a path (Rule 1). It is the file the
	// 2026-10-12 census derived the three arms from.
	cwdReleaseDeliveryRetryLib = "internal/tools/task_delivery_retry.go"

	// cwdReleaseDeliveryRetryRetirement is the machine-readable half of the
	// 2026-10-31 decision: the three re-delivery arms are NOT triggers, and this
	// record says why. It is read by TestGateCwdReleaseDeliveryRetryRetirement,
	// which re-establishes the decision from the tree on every run instead of
	// trusting this text — each of the three claims below is measured, and a
	// landing that makes any of them stale turns the gate red rather than
	// leaving a comfortable paragraph nobody checks.
	//
	//   the ARMS still exist and are still derived (they are live library
	//       faces; internal/tools/task_delivery_retry.go exports all three);
	//   NO scanned-tree production file calls them (the only callers are
	//       `_test.go`, which the census and this leg both read as tests);
	//   they are triggers in NEITHER list (not in cwdReleaseRemovalTriggers —
	//       the entry was deleted — and not in cwdReleaseArmsNotListed, because
	//       that list is the private-scratch class and these remove the row's own
	//       ground; a name in both would cancel out and read a false green).
	cwdReleaseDeliveryRetryRetirement = "no production caller: the serve lifecycle pass's delivery-retry leg " +
		"(cmd/agent/cli_tasks.go) was removed with the finish-cleanup mechanism in 58b5618b; " +
		"the arms remain exported and derived, so the census still SEES them — they simply " +
		"owe no producer-side chdir any more, and their trigger entries were stale"

	// cwdReleaseReadFloor is the non-vacuity tripwire: the real tree must
	// contain AT LEAST this many worktree-removing producers across BOTH
	// scanned trees. If a refactor moves them elsewhere the gate fails LOUDLY
	// instead of reading green over an empty set — a gate that scans nothing
	// passes for the wrong reason (the same read-floor contract as the Rule 21
	// env-reader gate). Measured at the gate's landing:
	// cmd/agent/cli_tasks.go, cli_tasks_worker.go, merge_resolve_child.go,
	// merger_verify_child.go + internal/agent/taskregister.go.
	//
	// RE-ESTABLISHED 2026-09-16 (fix/cwd-release-gate-inventory, from master
	// 69975030) — the INVENTORY was re-read by hand and the floor re-counted
	// from 5 to 4; the number was NOT lowered to fit the red (which is the
	// distinction that makes this gate worth keeping).
	//
	// RE-MEASURED 2026-09-16 (this branch, epic: verify the inventory by
	// CENSUS over the real tree): the 4 below is the CURRENT number, read out
	// of the gate itself by a temporary probe that raised this floor to 99 and
	// let the failure message print the count ("found only 4 worktree-removing
	// producer(s)"). The two vanished producers were NOT replaced by anything —
	// see the INVENTORY note above the constants for the independent check of
	// that claim, and TestGateCwdReleaseCensusArmsAreListed for the half that
	// no longer depends on remembering which arms are removal arms.
	//
	// WHY IT WENT RED: 126fad74 ("refactor(merger): ff-only or park-and-file —
	// no child, no wait, no state", 2026-09-16 18:24) DELETED the merger's two
	// spawned-child faces and, with them, the TWO producers that were the
	// gate's original members 3 and 4:
	//
	//	cmd/agent/merge_resolve_child.go   (deleted, -225)
	//	cmd/agent/merger_verify_child.go   (deleted, -971, 6 files deleted whole)
	//
	// Their `git … worktree remove --force` existed only to clean up after the
	// resolver/verifier children the SAME commit removed. The producers are gone
	// WITH the machinery, not moved — verified, not assumed: the removal calls
	// those files carried have no successor anywhere in the scanned trees
	// (`grep -rn '"worktree", "remove"' cmd/agent internal/agent` matches no
	// production file today), and a tick is now pure git (`merge --ff-only`, or
	// a park of the branch ref) that spawns no child and therefore never stands
	// in a worktree. Conflict resolution moved to the ordinary worker path,
	// whose own close-out already releases — a removed producer, not a
	// relocation of the obligation.
	//
	// INDEPENDENTLY RE-VERIFIED 2026-09-16 (feat/cwd-release-inventory-verified,
	// the verification task whose request this claim prompted). The claim was
	// re-derived from the tree, not re-read: every `tools.*` call the scanned
	// trees make was traced into internal/tools to a removal verb (git
	// `worktree remove`, or the reclaimGitRemove helper). The removal-reaching
	// calls that exist are exactly the inventory's own arms; NO call reaches
	// the deleted files' machinery, so nothing was relocated into an arm the
	// trigger set cannot see. Both files are absent from the tree and no merger
	// child spawn remains (mergerResolveProg / mergerVerifyProg / resolveChild /
	// verifyChild: zero production hits). The loss is a genuine REMOVAL.
	//
	// The verification did find a REAL hole, and a different one: the trigger
	// set was a hand-written phrase list no machine checked against the
	// library's actual removal arms, and three arms being CALLED from the
	// scanned trees were named nowhere. That is closed by the census test and
	// the widened list (cwdReleaseRemovalTriggers / cwdReleaseArmsNotListed) —
	// NOT by moving this floor.
	//
	// THE REMAINING INVENTORY (re-counted by hand over both scanned trees,
	// production files only):
	//
	//	cmd/agent/cli_resume.go         TaskFinishCleanupSessionID (via var)
	//	cmd/agent/cli_tasks.go          TaskFinishCleanupSessionID + ReclaimOwnFinishedWorktreesWithin + ReclaimSelfOwnRegisteredWorktreesWithin + ReclaimDeadHolderWorktrees
	//	cmd/agent/cli_tasks_worker.go   ReclaimOwnFinishedWorktreesWithin + ReclaimSelfOwnRegisteredWorktreesWithin
	//	internal/agent/taskregister.go  TaskFinishCleanupSessionID
	//
	// All four still CALL the release implementation (the marker is
	// cwdReleaseMarker, matched in code with comments stripped), so the floor
	// keeps guarding its real job: a scan that matches nothing must fail loudly
	// rather than read green over an empty set.
	//
	// The sibling fix on branch fix/standards-reds (e21f501b) resolved this same
	// red the same way (5 → 4, deletion named); it never landed, so the red was
	// still live on master when this branch was opened (Rule 26: dispatch the
	// repair again rather than wait for someone else's branch).
	//
	// RE-ESTABLISHED 2026-10-31 (4 → 3, deletion named) — epic merge-removal
	// item 3 deleted the one-shot `gopi merge` implementation, and the producer
	// this floor loses with it is cmd/agent/merge_run.go: the file carried the
	// `"worktree", "remove"` verb pair (it resolved merge conflicts by handing a
	// scratch worktree to git) and it is the ONLY count that left. Measured both
	// sides with this gate's own trigger table, comments stripped, over the same
	// two scanned trees:
	//
	//	origin/master : 4 — cli_tasks.go, cli_tasks_worker.go, merge_run.go, taskregister.go
	//	this branch   : 3 — cli_tasks.go, cli_tasks_worker.go,           taskregister.go
	//
	// The three that remain are the same three, each still a producer, so the
	// trigger set still covers every worktree-removing producer the tree has;
	// the coverage claim is intact and the floor tracks it. This is NOT the
	// "lower the number to fit the red" move the paragraph above forbids: the
	// deletion is named, it is this change's own, and the inventory was re-read
	// file by file rather than assumed.
	cwdReleaseReadFloor = 3
)

// cwdReleaseTriggers splits the in-band trigger list (Rule 1: one value, one
// home — the predicate and both probes share it).
func cwdReleaseTriggers() []string {
	return strings.Split(cwdReleaseRemovalTriggers, cwdReleaseTriggerSep)
}

// cwdReleaseTreeList splits the scanned-tree list.
func cwdReleaseTreeList() []string {
	return strings.Split(cwdReleaseTrees, cwdReleaseTreeSep)
}

// cwdReleaseScannedSource is one production source a scanned tree carries —
// the name (for the failure message) and its bytes (for the verdict).
type cwdReleaseScannedSource struct {
	name string
	src  string
}

// cwdReleaseDeliveryRetryScanTree lists one scanned tree's PRODUCTION sources.
// `_test.go` is skipped exactly as the census skips it (a test naming an arm is
// not a caller of it in a process). Factored out of leg ② so that single leg is
// one predicate plus one list — the shape a positive-control test can drive
// with a fixture instead of the real tree.
func cwdReleaseDeliveryRetryScanTree(root, tree string) ([]cwdReleaseScannedSource, error) {
	dir := filepath.Join(root, filepath.FromSlash(tree))
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", tree, err)
	}
	sources := make([]cwdReleaseScannedSource, 0, len(entries))
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".go") || strings.HasSuffix(e.Name(), "_test.go") {
			continue
		}
		src, rerr := os.ReadFile(filepath.Join(dir, e.Name()))
		if rerr != nil {
			return nil, fmt.Errorf("read %s/%s: %w", tree, e.Name(), rerr)
		}
		sources = append(sources, cwdReleaseScannedSource{name: e.Name(), src: string(src)})
	}
	return sources, nil
}

// cwdReleaseDeliveryRetryCallerViolations is THE predicate of leg ② — "does
// this production source call any of the retired re-delivery arms, and is that
// a red?" — returning the offending arm names (empty means no obligation).
//
// IT IS UNCONDITIONAL ON PURPOSE, AND THAT IS WHAT THIS FUNCTION EXISTS TO
// KEEP. Commit 0ee5745c made this verdict conjunctive to buy a green: it
// required the calling file to ALSO match cwdReleaseRemovalVerb, and leg ①
// asserts that the arms' own library file reaches no removal verb — so the
// caller-side half was a condition nothing could satisfy, and the gate could
// never fire again for any caller. The t.Logf branch made the pass look
// re-measured ("calls X and removes no directory — no obligation") while the
// leg had simply stopped being a gate.
//
// The repair that DID work (row 01a1ab49, 1c85d569) moved the CALL into
// internal/tools, a tree this gate does not scan, so "no scanned-tree caller"
// became true again and this verdict could be the unconditional absence it was
// born as.
//
// So: a caller is a red BECAUSE of what it calls, never because of an extra
// property the caller has to satisfy as well. Committed it is a pure function
// of (source, arms) so TestGateCwdReleaseDeliveryRetryLeg2StillRejectsACaller
// can feed it the exact 0ee5745c-shaped caller — one that calls an arm and
// removes nothing — and require a red. Narrow this verdict back into a
// conjunction and that test fails, naming the fixture and the arm; the gate
// can no longer be quietly changed into accepting a call.
func cwdReleaseDeliveryRetryCallerViolations(src string, arms []string) []string {
	code := cwdReleaseCode(src)
	var offending []string
	for _, arm := range arms {
		if arm == "" {
			continue
		}
		if strings.Contains(code, arm) {
			offending = append(offending, arm)
		}
	}
	return offending
}

// cwdReleaseViolation reports whether one source file is a worktree-removing
// producer that does not reference the release implementation. Pure over
// (content), so the probes feed it synthetic sources and the exhaustive scan
// feeds it the real tree — ONE predicate, two callers.
//
// A file with no removal trigger is not in scope (false). A file WITH a
// trigger satisfies the gate by naming the marker anywhere in the file: the
// release is a close-out step of the same process, and a file that names the
// implementation has the seam in hand (the escape for a genuinely impossible
// case is a `// cwd-release-ok: <reason>` line, which the shared gate machinery
// honours — but no production file carries one today, by design).
func cwdReleaseViolation(src string) bool {
	if strings.Contains(src, cwdReleaseEscape) {
		return false
	}
	if !cwdReleaseHasTrigger(src) {
		return false
	}
	return !strings.Contains(cwdReleaseCode(src), cwdReleaseMarker)
}

// cwdReleaseCode returns the source with COMMENTS removed, so the marker must
// appear in CODE. Measured reason (the gate's second false-negative, found the
// same way as the first): a producer's explanatory comment quotes the release
// call — internal/agent/taskregister.go's comment spells
// "worktreerelease.ReleaseReporting(a.taskLogWriter(), agentProgName)" in
// prose. Matching raw text let a prose-only mention satisfy the gate, so a
// producer whose call was deleted but whose comment survived read green.
//
// It uses go/scanner rather than a hand-rolled // and /* scan, and that is a
// measured decision too: a hand-rolled version read the "feat/*" inside a
// STRING literal (cli_tasks.go's branch-glob prose) as a block-comment opener
// and ate the rest of the file — a FALSE POSITIVE on a file that does release.
// Only a real tokenizer knows a string is a string. Comments are blanked in
// place (byte for byte, spaces) so offsets and line structure are untouched.
func cwdReleaseCode(src string) string {
	fset := token.NewFileSet()
	file := fset.AddFile("", fset.Base(), len(src))
	var s scanner.Scanner
	s.Init(file, []byte(src), func(token.Position, string) {}, scanner.ScanComments)
	out := []byte(src)
	for {
		pos, tok, lit := s.Scan()
		if tok == token.EOF {
			break
		}
		if tok != token.COMMENT {
			continue
		}
		off := file.Offset(pos)
		for i := off; i < off+len(lit) && i < len(out); i++ {
			if out[i] != '\n' {
				out[i] = ' '
			}
		}
	}
	return string(out)
}

// cwdReleaseHasTrigger reports whether a source file's CODE carries ANY
// worktree removal trigger — the scope half of the predicate, split out so the
// exhaustive scan can count producers for its read floor without re-deriving
// the match.
//
// CODE ONLY (added 2026-09-16, measured while widening this set): comments are
// stripped first, because a comment cannot remove a worktree. Matching raw text
// made cmd/agent/task_lifecycle_tick.go a producer the moment "MergedCleanupSweep"
// joined the trigger list — its COMMENT names the pass it took over, and the
// file itself removes nothing: it is a pass body inside the worker/merger
// process, and that process's own round/pass boundary (workerRoundOwnWorktrees /
// mergerPassOnce) is the face that releases. A trigger that fires on prose
// demands a chdir of a file that cannot owe one. The release MARKER has been
// code-only since the second false negative; the trigger half now matches it.
func cwdReleaseHasTrigger(src string) bool {
	code := cwdReleaseCode(src)
	for _, trig := range cwdReleaseTriggers() {
		if trig != "" && strings.Contains(code, trig) {
			return true
		}
	}
	return false
}

// cwdReleaseEscape is the reviewed-exception marker for a worktree-removing
// producer that provably cannot release its own cwd. It follows the gate
// machinery's convention (`// no-wrap:`, `// else-ok:`, `// panic-ok:`); no
// production site uses it today.
const cwdReleaseEscape = "// cwd-release-ok:"

// TestGateCwdRelease_RedProof pins the predicate's precision: it must report a
// worktree-removing producer with no release, and must NOT report (a) a
// producer that names the release, (b) an ordinary file that neither removes a
// worktree nor releases anything, or (c) a reviewed exception.
func TestGateCwdRelease_RedProof(t *testing.T) {
	// The unfixed shape: the merger's resolve teardown removing a worktree
	// exactly as it did before this epic — no release anywhere in the file.
	unfixed := `package main

func teardown(repo, wtdir string) {
	exec.Command("git", "-C", repo, "worktree", "remove", "--force", wtdir).Run()
}
`
	if !cwdReleaseViolation(unfixed) {
		t.Errorf("gate did NOT report a worktree-removing producer with no cwd release — the gate is vacuous on the exact shape it exists for")
	}

	// The fixed shape: the same teardown plus the release.
	fixed := `package main

func teardown(repo, wtdir string) {
	worktreerelease.ReleaseReporting(os.Stderr, mergerResolveProg)
	exec.Command("git", "-C", repo, "worktree", "remove", "--force", wtdir).Run()
}
`
	if cwdReleaseViolation(fixed) {
		t.Errorf("gate reported a producer that DOES reference the release implementation — false positive")
	}

	// Negative probe: an unrelated file (no removal trigger) is out of scope,
	// even though it releases nothing — the gate is about removers.
	outOfScope := `package main

func banner() { fmt.Println("gopi merger: nothing to sweep") }
`
	if cwdReleaseViolation(outOfScope) {
		t.Errorf("gate reported a file with no worktree-removal trigger — the gate is a blanket, not a coverage check")
	}

	// Negative probe (measured: this is the shape that slipped past the first
	// version of this gate): a producer that IMPORTS the release package but
	// never CALLS it is still a violation — an unused import is not a release.
	importOnly := `package main

import "github.com/nickhuang99/gopi/internal/worktreerelease"

func teardown(repo, wtdir string) {
	exec.Command("git", "-C", repo, "worktree", "remove", "--force", wtdir).Run()
}
`
	if !cwdReleaseViolation(importOnly) {
		t.Errorf("gate accepted a producer that only IMPORTS the release package — the marker must be the CALL, not the package qualifier")
	}

	// Negative probe (measured: the gate's SECOND false negative — a producer
	// whose only mention of the release is PROSE). internal/agent/taskregister.go
	// really does spell the call inside a comment; matching raw text let that
	// comment satisfy the gate, so deleting the call while keeping the
	// explanation read green. The marker must live in CODE.
	commentOnly := `package main

// Close-out calls worktreerelease.ReleaseReporting(a.taskLogWriter(), agentProgName).
func teardown(repo, wtdir string) {
	exec.Command("git", "-C", repo, "worktree", "remove", "--force", wtdir).Run()
}
`
	if !cwdReleaseViolation(commentOnly) {
		t.Errorf("gate accepted a producer whose only mention of the release is a COMMENT — the marker must be in code")
	}

	// Negative probe (regression guard for the comment stripper itself): a
	// file whose earlier TEXT carries "feat/*" and a closing "*/" — exactly
	// cmd/agent/cli_tasks.go's real comment, which made an earlier stripper eat
	// 3000 lines of code and report the file as unreleased (a false positive).
	// Prose asterisks must not be read as a block comment.
	slashesInProse := `package main

// push local feat/* branches: the server fetches the client's feat/* and
// adopts them into refs/heads (see refs/remotes/origin/*).
func teardown(repo, wtdir string) {
	worktreerelease.ReleaseReporting(os.Stderr, mergerResolveProg)
	exec.Command("git", "-C", repo, "worktree", "remove", "--force", wtdir).Run()
}
`
	if cwdReleaseViolation(slashesInProse) {
		t.Errorf("gate reported a producer that DOES call the release — the comment stripper mistook prose for a block comment")
	}

	// A stale-trigger control: the FLOOR below is set from the producer census
	// (cwdReleaseReadFloor), the ARM list is checked against the derived arm set
	// (TestGateCwdReleaseCensusArmsAreListed). Malformed arm names are checked
	// here, where they would otherwise show up as a dead trigger.
	// TestGateCwdReleaseCensusFindsTheRealArms carries the same control against
	// the DERIVED set: ReclaimHeldBranches deletes orphan branch refs only.

	// Negative probe: the reclaim arms in a caller file count as triggers, so
	// the producer close-outs are covered even without a literal git verb.
	armNoRelease := `package main

func closeOut(id string) { tools.ReclaimOwnFinishedWorktreesWithin() }
`
	if !cwdReleaseViolation(armNoRelease) {
		t.Errorf("gate did NOT report a reclaim-arm producer without a release — the worker/wrapper close-outs would be uncovered")
	}

	// Negative probe (added with the WIDENED arm list, 2026-09-16): prose is
	// not a producer. cmd/agent/task_lifecycle_tick.go names MergedCleanupSweep
	// in a comment and removes nothing; a trigger list that matches comments
	// would demand a chdir of a pass body that cannot owe one.
	proseOnly := `package main

// the reap that closes that hole was wired into serveMergedCleanupSweep.
func pass() { fmt.Println("tick") }
`
	if cwdReleaseViolation(proseOnly) {
		t.Errorf("gate reported a FILE THAT ONLY NAMES an arm in prose — a comment cannot remove a worktree")
	}

	// Negative probe: a reviewed exception is honoured.
	escaped := `package main

// cwd-release-ok: this seam hands the directory to a child and exits at once.
func teardown(repo, w string) { exec.Command("git", "-C", repo, "worktree", "remove", w).Run() }
`
	if cwdReleaseViolation(escaped) {
		t.Errorf("gate ignored its own reviewed-exception marker")
	}
}

// TestGateCwdReleaseFindsTheRealTree is the exhaustive half: every
// worktree-removing producer in the process-owning trees must reference the
// release implementation. It fails on a new producer that forgets it — which is
// the whole point of a gate over a principle.
func TestGateCwdReleaseFindsTheRealTree(t *testing.T) {
	root := repoRoot(t)
	var violations []string
	scanned := 0
	producers := 0
	for _, tree := range cwdReleaseTreeList() {
		dir := filepath.Join(root, filepath.FromSlash(tree))
		entries, err := os.ReadDir(dir)
		if err != nil {
			t.Fatalf("read %s: %v", tree, err)
		}
		for _, e := range entries {
			name := e.Name()
			if e.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
				continue
			}
			src, rerr := os.ReadFile(filepath.Join(dir, name))
			if rerr != nil {
				t.Fatalf("read %s/%s: %v", tree, name, rerr)
			}
			scanned++
			if cwdReleaseHasTrigger(string(src)) {
				producers++
			}
			if cwdReleaseViolation(string(src)) {
				violations = append(violations, tree+"/"+name)
			}
		}
	}
	// Read floor: a scan that visited nothing must not read as compliance.
	if scanned == 0 {
		t.Fatalf("scanned no production file under %v — the gate read green over an empty set", cwdReleaseTreeList())
	}
	// Non-vacuity: the trigger set must actually match the tree. If a refactor
	// moved the worktree removers out of the scanned trees, the coverage claim
	// is stale and must be re-established deliberately.
	if producers < cwdReleaseReadFloor {
		t.Fatalf("found only %d worktree-removing producer(s) across %v (floor %d) — the trigger set no longer covers the tree, so this gate proves nothing; re-establish the inventory before lowering the floor",
			producers, cwdReleaseTreeList(), cwdReleaseReadFloor)
	}
	for _, v := range violations {
		t.Errorf("%s removes a linked worktree but never releases this process's cwd — it will pin the removed directory's inode (finish-cleanup guard ③ refuses the branch forever). Call worktreerelease.ReleaseReporting(...) on the close-out path, or mark the site `%s <reason>`.",
			v, cwdReleaseEscape)
	}
}

// --- the census: DERIVE the removal-arm set, never recall it ----------------

// cwdReleaseCensus is one walk over cwdReleaseToolsTree that answers, from the
// SOURCE, which functions of the library can reach a worktree REMOVAL. It is
// the half a hand-written trigger list cannot do: a producer that calls an arm
// nobody thought to name is invisible to a phrase list, and visible here.
//
// THE DIRECTION MATTERS (measured on this census's first draft, which had it
// backwards and derived 7 arms of which 3 were nonsense — `String`, `Close`,
// the go/ast token types a scanner switch mentions): a function is a remover
// when it REACHES a removal verb — i.e. when something it CALLS is a remover,
// or it carries a removal verb itself. "What does this function call" yields
// the arm set; "who calls the seed" yields every caller of the reclaim arms,
// which is a different (and useless) set. The walk starts from the functions
// that CARRY the verb and closes over CALLERS, exactly once per function.
func cwdReleaseCensus(t *testing.T) (arms []string, floor int) {
	t.Helper()
	root := repoRoot(t)
	dir := filepath.Join(root, filepath.FromSlash(cwdReleaseToolsTree))
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read %s: %v", cwdReleaseToolsTree, err)
	}
	calls := map[string]map[string]bool{} // function -> the functions it calls
	seed := map[string]bool{}             // functions carrying a removal verb
	files := 0
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".go") || strings.HasSuffix(e.Name(), "_test.go") {
			continue
		}
		src, rerr := os.ReadFile(filepath.Join(dir, e.Name()))
		if rerr != nil {
			t.Fatalf("read %s/%s: %v", cwdReleaseToolsTree, e.Name(), rerr)
		}
		files++
		code := cwdReleaseCode(string(src))
		for _, f := range cwdReleaseFuncs(t, e.Name(), code) {
			body := code[f.start:f.end]
			calls[f.name] = map[string]bool{}
			for _, c := range cwdReleaseCallNames(t, e.Name(), body) {
				calls[f.name][c] = true
			}
			if cwdReleaseRemovalVerb.MatchString(body) {
				seed[f.name] = true
			}
		}
	}
	if files == 0 {
		t.Fatalf("census read no production file under %s — it proves nothing over an empty set", cwdReleaseToolsTree)
	}
	// Close over CALLERS: a function that calls a remover is a remover.
	for changed := true; changed; {
		changed = false
		for name, callees := range calls {
			if seed[name] {
				continue
			}
			for c := range callees {
				if seed[c] {
					seed[name] = true
					changed = true
					break
				}
			}
		}
	}
	for name := range seed {
		if name != "" && name[0] >= 'A' && name[0] <= 'Z' {
			arms = append(arms, name)
		}
	}
	sort.Strings(arms)
	return arms, files
}

// cwdReleaseRemovalVerb is the removal verb the census seeds from, in the
// parameter spelling the library uses: a `git worktree remove` invocation, or
// the one helper every such invocation in the tree goes through
// (reclaimGitRemove, whose own body carries the verb pair). The census needs
// the SPELLING, the gate needs the NAME — one constant each (Rule 1b).
var cwdReleaseRemovalVerb = regexp.MustCompile(`(finishCleanupWorktreeVerb|WorktreeVerb),\s*\w*[Rr]emove\w*Verb|reclaimGitRemove\(`)

// cwdReleaseFunc is one top-level function of a census file.
type cwdReleaseFunc struct {
	name       string
	start, end int
}

// cwdReleaseFuncs splits one file into top-level functions by line, from each
// `func` line to the next one. Receiver names never contain the word "func", so
// the split is exact for the library's shape (no nested func literals are
// reported — a closure's calls belong to its enclosing function, which is what
// the census wants).
func cwdReleaseFuncs(t *testing.T, file, code string) []cwdReleaseFunc {
	t.Helper()
	lines := strings.Split(code, "\n")
	offsets := make([]int, len(lines)+1)
	for i, l := range lines {
		offsets[i+1] = offsets[i] + len(l) + 1
	}
	var out []cwdReleaseFunc
	var names []string
	var starts []int
	for i, l := range lines {
		if !strings.HasPrefix(l, "func ") {
			continue
		}
		name := cwdReleaseFuncName(l)
		if name == "" {
			t.Fatalf("%s:%d: a func line the census cannot name: %q", file, i+1, l)
		}
		names = append(names, name)
		starts = append(starts, offsets[i])
	}
	for i, st := range starts {
		end := len(code)
		if i+1 < len(starts) {
			end = starts[i+1]
		}
		out = append(out, cwdReleaseFunc{name: names[i], start: st, end: end})
	}
	return out
}

// cwdReleaseFuncName extracts the name from a `func` line — after an optional
// receiver group. Returns "" when the line names nothing (a malformed source,
// which the census reports rather than silently skipping).
func cwdReleaseFuncName(line string) string {
	s := strings.TrimPrefix(line, "func ")
	if strings.HasPrefix(s, "(") {
		i := strings.Index(s, ")")
		if i < 0 {
			return ""
		}
		s = strings.TrimSpace(s[i+1:])
	}
	for i, r := range s {
		if !(r == '_' || r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9') {
			return s[:i]
		}
	}
	return s
}

// cwdReleaseCallNames reports the call sites inside one function body: the
// identifier before `(` or before a generic `[`. Locals and stdlib calls come
// back too and are harmless — the census only asks whether a name is in its own
// function index. Sources are COMMENTS-STRIPPED (cwdReleaseCode), so a function
// that only QUOTES a removal in prose does not become an arm.
func cwdReleaseCallNames(t *testing.T, file, body string) []string {
	t.Helper()
	var out []string
	for _, m := range cwdReleaseCallRe.FindAllStringSubmatch(body, -1) {
		out = append(out, m[1])
	}
	return out
}

// cwdReleaseCallRe matches an identifier in call position. Group 1 is the name.
var cwdReleaseCallRe = regexp.MustCompile(`\b([A-Za-z_][A-Za-z0-9_]*)\s*(?:\[[^\]]*\]\s*)?\(`)

// TestGateCwdReleaseCensusArmsAreListed is the census's verdict: every removal
// arm the walk DERIVED from internal/tools is either a trigger of this gate or
// named in cwdReleaseArmsNotListed with a reason. An arm in neither is the hole
// the epic warns about — a producer calling it removes a worktree without the
// gate having an opinion.
func TestGateCwdReleaseCensusArmsAreListed(t *testing.T) {
	arms, files := cwdReleaseCensus(t)
	if len(arms) < cwdReleaseArmFloor {
		t.Fatalf("census derived only %d exported removal arm(s) over %d file(s) of %s (floor %d) — the walk no longer sees the library's removals, so its coverage verdict proves nothing",
			len(arms), files, cwdReleaseToolsTree, cwdReleaseArmFloor)
	}
	listed := map[string]bool{}
	for _, a := range strings.Split(cwdReleaseArmsNotListed, cwdReleaseArmsNotListedSep) {
		listed[a] = true
	}
	triggered := func(arm string) bool {
		for _, trig := range cwdReleaseTriggers() {
			if trig != "" && strings.Contains(arm, trig) {
				return true
			}
		}
		return false
	}
	var missing []string
	for _, arm := range arms {
		if !triggered(arm) && !listed[arm] {
			missing = append(missing, arm)
		}
	}
	if len(missing) > 0 {
		t.Errorf("census derived %d removal arm(s) that this gate neither triggers on nor records as deliberately exempt: %v — a caller of one removes a linked worktree with no release obligation attached. Add the arm to cwdReleaseRemovalTriggers, or record it in cwdReleaseArmsNotListed with its reason.",
			len(missing), missing)
	}
	// ...and the two verdicts are MUTUALLY EXCLUSIVE, checked over the arms the
	// walk actually derived rather than a hand-kept list of special names. An
	// arm in BOTH lists reads as coverage on one line and as suppression on the
	// next: the trigger entry makes the gate demand a release of every caller,
	// while the exemption entry makes it stop looking. The close-out seam's
	// reason (above) is why the trigger side is the one that must win for a
	// remover of the run's OWN ground, and this assertion is what keeps that
	// from being a comment nobody executes — it covers every arm, including the
	// ones a later widening adds.
	for _, arm := range arms {
		if triggered(arm) && listed[arm] {
			t.Errorf("arm %q is listed as a deliberate EXEMPTION in cwdReleaseArmsNotListed AND matches a trigger in cwdReleaseRemovalTriggers — the two verdicts cancel out and the gate reads green over a producer it never examines; keep the trigger and delete the exemption entry, or drop the trigger if the arm is genuinely out of scope", arm)
		}
	}
	// The trigger set must not outlive the arms it names: an entry matching NO
	// derived arm is dead weight that reads like coverage.
	for _, trig := range cwdReleaseTriggers() {
		if trig == "" {
			continue
		}
		if strings.Contains(trig, "worktree") && strings.Contains(trig, "remove") {
			continue // the git verb itself, not an arm name
		}
		if trig == "reclaimGitRemove" {
			continue // the private helper the arms route through: real, but lowercase (never an exported arm name)
		}
		found := false
		for _, arm := range arms {
			if strings.Contains(arm, trig) {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("trigger %q matches no arm the census derived from %s — a stale trigger reads as coverage while covering nothing", trig, cwdReleaseToolsTree)
		}
	}
}

// TestGateCwdReleaseCensusFindsTheRealArms is the census's read proof: on the
// real tree it must DERIVE the arms the gate's own history names by hand. It
// fails if the walk comes back empty (a broken regexp reading green) or if an
// arm this gate has been depending on stops being derivable.
func TestGateCwdReleaseCensusFindsTheRealArms(t *testing.T) {
	arms, _ := cwdReleaseCensus(t)
	have := map[string]bool{}
	for _, a := range arms {
		have[a] = true
	}
	// The four the gate was built around (three of them called from the scanned
	// trees, one the verb pair every removal routes through)…
	//
	// RETIRED 2026-10-31 (task/01a18ec8): TaskFinishCleanupSessionID is gone
	// from this list, not from the tree's history. The finish-cleanup mechanism
	// was retired in 58b5618b and the name survives only as a `_test.go` fixture
	// (internal/tools/finish_cleanup_moved_helpers_test.go:252) — which this
	// census skips BY DESIGN. Asserting it here asserted a coverage obligation
	// over a producer that no longer exists. The replacement for that assertion
	// is not another hand-kept name: it is the census itself
	// (TestGateCwdReleaseCensusArmsAreListed), which fails on ANY derived arm
	// nobody lists, and cwdReleaseArmFloor, which fails if the walk stops
	// deriving arms at all. If the mechanism ever comes back, the walk derives
	// it and the trigger list has to answer for it again.
	for _, want := range []string{
		"ReclaimOwnFinishedWorktreesWithin",
		"ReclaimSelfOwnRegisteredWorktreesWithin",
		"ReclaimDeadHolderWorktrees",
		"MergedCleanupSweep",
		"WorktreeSweepRun",
		// …plus the close-out seam's two faces (added 2026-09-26, the run that
		// closed the census red): the seam is what the two scanned-tree
		// producers now call, so if the walk stops deriving it the trigger
		// entries that name it become dead weight in a green run — the failure
		// this read proof exists to catch (cwdReleaseRemovalTriggers carries
		// why they are triggers rather than exemptions).
		"ReclaimOwnWorktreesAtCloseout",
		"ReclaimOwnWorktreesAtCloseoutWithin",
		// The blocked-delivery re-delivery faces were asserted here from
		// 2026-10-12 until 2026-10-31. They are STILL derived (they reach the
		// close-out chain transitively) and STILL exported, but they are no
		// longer triggers, so they are not coverage this read proof owes: their
		// maintenance is TestGateCwdReleaseDeliveryRetryRetirement, which asserts
		// the same derivation claim for exactly these three names and adds the
		// half that retired them (no production caller).
	} {
		if !have[want] {
			t.Errorf("census did not derive %s — the walk misses a removal arm this gate depends on, so its coverage claim is unsound (derived: %v)", want, arms)
		}
	}
	// The seam arms are TRIGGERS, never exemptions: a name that appears in both
	// lists would read as coverage while suppressing it.
	for _, seam := range []string{
		"ReclaimOwnWorktreesAtCloseout",
		"ReclaimOwnWorktreesAtCloseoutWithin",
	} {
		for _, exempt := range strings.Split(cwdReleaseArmsNotListed, cwdReleaseArmsNotListedSep) {
			if exempt == seam {
				t.Errorf("%s is listed as a deliberately exempt arm AND triggers the gate — the close-out seam removes the run's OWN ground, so exempting it would silence exactly the omission the gate reports", seam)
			}
		}
	}
	// …and a control that must NOT be derived: ReclaimHeldBranches deletes
	// orphan branch REFS only. If it shows up, the verb predicate has gone
	// fuzzy and every branch-only helper would look like a worktree remover.
	if have["ReclaimHeldBranches"] {
		t.Errorf("census derived ReclaimHeldBranches — it is branch-only; the removal-verb predicate is too loose")
	}
}

// TestGateCwdReleaseDeliveryRetryRetirement is the read proof that REPLACES the
// 2026-10-12 classification proof (TestGateCwdReleaseCensusArmsAreClassified),
// retired 2026-10-31 by task/01a18ec8. No assertion was dropped without a
// replacement: the claims that are still true are measured HERE, and the two
// that stopped being true are measured in their retired form.
//
// WHY THE OLD PROOF DIED. It asserted four legs, and every one of them was a
// claim about the arms being REMOVERS reached by a PRODUCER:
//
//	① the census still derives the three from internal/tools
//	② cwdReleaseRemovalTriggers carries them
//	③ none of them is also an exemption
//	④ a scanned-tree file calls one in code and carries the cwd release
//
// The close-out chain ask was retired one slice earlier (task 01a18799, slice
// 3/4 — task_delivery_retry.go's own note: "THE CLOSE-OUT CHAIN ASK IS
// RETIRED"), and the producer went with the finish-cleanup mechanism in
// 58b5618b. So ① became false (the arms are no longer removers), ② and ③ became
// assertions about entries that had to be deleted, and ④ became an assertion
// over a producer that no longer exists — a RED on every run, which is what the
// four stale trigger entries were the other half of.
//
// A read proof is retired honestly ONLY if what replaces it still fires. This
// test fires on all four of those subjects, from the tree:
//
//	① the three arms are still EXPORTED library faces (definitions present),
//	   and the library file no longer reaches a removal verb — that is WHY the
//	   census stopped deriving them, measured rather than asserted. If a
//	   removal path comes back into that file, this goes red and the trigger
//	   decision has to be re-taken.
//	② no scanned-tree PRODUCTION file calls any of them. `_test.go` is skipped
//	   exactly as the census skips it. A NEW production caller turns this red,
//	   full stop — the signal that a re-delivery leg has been put somewhere it
//	   owes a chdir obligation, and the repair is to move the CALL out of a
//	   scanned tree (not to restore trigger entries, which are retired for good
//	   in finish-cleanup 58b5618b and which the census could not derive any
//	   more), and not to weaken this leg.
//
//	   MEASURED TWICE, IN OPPOSITE DIRECTIONS, and the second one is why this
//	   paragraph now reads the way it first did:
//
//	   · 2026-11-05 (e7e332b8) put the `task sync --pass` re-driver in
//	     cmd/agent/task_sync_pass_delivery_owed.go, calling
//	     tools.DeliveryRetryTaskID from a scanned tree. This leg went red, which
//	     was CORRECT.
//	   · 2026-11-05 (0ee5745c) turned it green by redefining the obligation as
//	     "a caller that ALSO removes a directory". That is not the premise the
//	     retirement rested on: leg ① owns that half, it re-measures that the
//	     arms' library file reaches no removal verb, so the conjunction was
//	     permanently false and this leg could never fire again — a gate changed
//	     to accept the call.
//	   · 2026-11-06 (row 01a1ab49) moved the CALL instead: the leg now lives in
//	     internal/tools/task_delivery_owed_redrive.go (DeliveryRedriveLeg), which
//	     is NOT one of cwdReleaseTrees, and cmd/agent keeps only the pass's face
//	     and the ORDER. Nothing was deleted to buy the green, and this leg is
//	     back to asserting the absence it was written to assert.
//	③ they are in NEITHER list: not a trigger (deleted), and not an exemption
//	   (that list is the private-scratch class, a verdict these arms never
//	   earned). A name in both lists would cancel out and read a false green.
//	④ the producer constant still names a file that EXISTS, so the retirement
//	   note stays attributable — a dangling path would make "that file's leg
//	   was removed" a claim about nothing.
func TestGateCwdReleaseDeliveryRetryRetirement(t *testing.T) {
	root := repoRoot(t)
	named := strings.Split(cwdReleaseDeliveryRetryArms, cwdReleaseTriggerSep)
	if len(named) == 0 || named[0] == "" {
		t.Fatalf("cwdReleaseDeliveryRetryArms is empty — this proof would pass over no arm at all (its own non-vacuity tripwire)")
	}
	if cwdReleaseDeliveryRetryRetirement == "" {
		t.Fatalf("cwdReleaseDeliveryRetryRetirement carries no reason — the decision to drop these triggers must be recorded, not implied (Rule 15: silence is not a policy)")
	}

	// ① still exported library faces, and provably no longer removers.
	libSrc, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(cwdReleaseDeliveryRetryLib)))
	if err != nil {
		t.Fatalf("read %s: %v — this proof rests on the three arms still being LIBRARY faces; if the file was retired whole that is a DIFFERENT decision and the retirement note has to say so", cwdReleaseDeliveryRetryLib, err)
	}
	libCode := cwdReleaseCode(string(libSrc))
	for _, arm := range named {
		if !strings.Contains(libCode, "func "+arm+"(") {
			t.Errorf("%s is no longer defined in %s — the recorded retirement says these are live exported arms that lost their PRODUCER; an arm that no longer exists retires the trigger for a DIFFERENT reason and this note would be lying about which", arm, cwdReleaseDeliveryRetryLib)
		}
	}
	if cwdReleaseRemovalVerb.MatchString(libCode) {
		t.Errorf("%s now MATCHES the removal verb again (%s) — the three arms were dropped as triggers because this file stopped reaching a worktree removal; a removal path back in it means a call to an arm can remove a directory a producer stands in, so the trigger decision has to be re-taken", cwdReleaseDeliveryRetryLib, cwdReleaseRemovalVerb)
	}

	// ② no scanned-tree production caller.
	//
	// THE VERDICT IS A FUNCTION, NOT AN INLINE LOOP (2026-11-06, this row).
	// It used to be inline, which is how 0ee5745c could redefine it as
	// "…and the caller removes a directory too" without anything noticing:
	// an inline condition has no caller a test can drive, so "this leg still
	// rejects a caller" was an assumption about the source text rather than a
	// measurement of it. cwdReleaseDeliveryRetryCallerViolations is now the ONE
	// predicate, it is pure over (source, arms), and the positive-control test
	// below (TestGateCwdReleaseDeliveryRetryLeg2StillRejectsACaller) feeds it a
	// fixture shaped exactly like the caller 0ee5745c was written for. A landing
	// that narrows the verdict again turns THAT test red, naming the fixture.
	for _, tree := range cwdReleaseTreeList() {
		sources, serr := cwdReleaseDeliveryRetryScanTree(root, tree)
		if serr != nil {
			t.Fatalf("%v", serr)
		}
		for _, s := range sources {
			// ANY production caller is a red (restored 2026-11-06, row
			// 01a1ab49). This leg briefly tested "the caller ALSO removes a
			// directory" (0ee5745c); that conjunction is false by construction —
			// leg ① asserts the arms' library file reaches no removal verb, so
			// the caller-side half could never fire and the leg measured nothing.
			// A gate changed to accept a call is the defect, not the repair.
			//
			// The leg asserts the ABSENCE, and the absence is what the
			// retirement record states (cwdReleaseDeliveryRetryRetirement:
			// "no production caller"). The re-home that made it true again is
			// internal/tools/task_delivery_owed_redrive.go — the same code, in a
			// tree this gate does not scan, so it owes no producer-side chdir.
			// A future landing that puts a re-delivery leg back in cmd/agent or
			// internal/agent is told by this message what to do about it.
			for _, arm := range cwdReleaseDeliveryRetryCallerViolations(s.src, named) {
				t.Errorf("%s/%s CALLS %s — the recorded retirement says NO scanned-tree production file calls these arms (they are not triggers any more: the arms' library file reaches no removal verb, measured by leg ①). Do not restore the trigger entries (the census cannot derive them, so they would read as coverage while covering nothing); move the CALL into internal/tools, which is not one of cwdReleaseTrees, and leave this leg asserting the absence", tree, s.name, arm)
			}
		}
	}

	// ③ in neither list.
	triggered := map[string]bool{}
	for _, trig := range cwdReleaseTriggers() {
		triggered[trig] = true
	}
	exempt := map[string]bool{}
	for _, e := range strings.Split(cwdReleaseArmsNotListed, cwdReleaseArmsNotListedSep) {
		exempt[e] = true
	}
	for _, arm := range named {
		if triggered[arm] {
			t.Errorf("%s is a TRIGGER again but the retirement record says it was dropped — the two decisions disagree; restore the reason or delete the entry, in the same change", arm)
		}
		if exempt[arm] {
			t.Errorf("%s is recorded in cwdReleaseArmsNotListed — the retirement dropped it as a trigger because it is no longer a REMOVER reached by a producer, NOT because its removal is somebody else's; that list is the private-scratch class, a verdict this arm never earned", arm)
		}
	}

	// ④ the producer constant still names a real file.
	if _, err := os.Stat(filepath.Join(root, filepath.FromSlash(cwdReleaseDeliveryRetryProducer))); err != nil {
		t.Errorf("cwdReleaseDeliveryRetryProducer names %s: %v — the retirement note attributes the lost producer to that file's leg; if the file itself is gone the note needs re-deriving", cwdReleaseDeliveryRetryProducer, err)
	}
}
