package tools

// Dead-holder worktree reclaim (epic 2026-09-12, defect 2).
//
// THE INCIDENT: a run whose wrapper dies leaves its linked worktree occupied.
// Two consumers then treat the branch as untouchable —
// scripts/safe-merge.sh gate [c] refuses a worktree-occupied branch, and the
// merger's protected arm (repoSweepProtectedBranches) drops it from the
// candidate list — so the finished deliverable is withheld, silently and
// forever (measured: merged=0 … protected=7 deliverable commits never landed).
//
// The fix makes "held by a DEAD holder" a REPAIRABLE state instead of an
// invisible one, on two levels:
//
//	report — the merger's protected arm now names such a branch (MergerSweep
//	         Preview.DeadHolder + a log line), so an operator stops seeing a
//	         bare protected count.
//	reclaim — this file's guarded removal frees the DIRECTORY so gate [c]
//	         passes and the normal merger loop picks the branch up. The
//	         BRANCH IS NEVER TOUCHED: it is the deliverable.
//
// THREE independent guards must hold before a directory is removed, and each
// one alone is sufficient to refuse:
//
//	① the ledger must NAME the branch, on a TERMINAL row whose claim pid is
//	   provably dead (a live holder's worktree is never a candidate, and a
//	   foreign/stampless row is unknowable — never called dead);
//	② the directory must be CLEAN (`git status --porcelain` empty) — a dead
//	   holder's uncommitted work is still work, left for a human;
//	③ no live process may be working inside it (the finish-cleanup cwd scan,
//	   reused verbatim — one implementation of "in use", Rule 14).
//
// The removal itself is the ordinary `git worktree remove` (no --force), so
// git refuses anything the three guards missed: the guards decide, git is the
// backstop.

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/nick/gopi/internal/boolvocab"
	"github.com/nick/gopi/internal/config"
	"github.com/nick/gopi/internal/devworkspace"
	"github.com/nick/gopi/internal/removalaudit"
)

const (
	// ReclaimDeadHolderEnv is the kill switch for the reclaim REMOVAL
	// (the finish-cleanup env convention: "0"/"off"/"no"/"false"/"disabled"
	// turns it off). Off = report only: the sweep still names the
	// dead-holder branches, it just stops freeing them.
	ReclaimDeadHolderEnv = "GOPI_DEAD_HOLDER_RECLAIM"

	// reclaimScanMax caps how many terminal rows one pass examines (the
	// sweep's runaway-ledger bound, same contract as its siblings).
	reclaimScanMax = 200

	// reclaimGitTimeout caps each git probe (status / worktree list / the
	// removal itself): a busy repo must fail fast, never hang a lifecycle
	// pass.
	reclaimGitTimeout = 30 * time.Second

	// reclaimRemovedMarker is the phrase the reclaim's report line carries —
	// one marker for the log AND the tests, so an operator greps one string.
	reclaimRemovedMarker = "dead-holder worktree reclaimed"

	// reclaimRepairedMarker is the same for the registration REPAIR: the
	// ledger recorded a worktree whose directory is gone, so there was nothing
	// to remove — but the stale registration git still held was pruned (see
	// reclaimRepairRegistration for why that is the repair, not bookkeeping).
	reclaimRepairedMarker = "dead-holder worktree registration pruned"

	// reclaimCandidateMarker is the same for the dry-run/proposal line: the
	// branch gate [c] is blocking, named as reclaimable.
	reclaimCandidateMarker = "dead-holder worktree reclaimable"

	// reclaimUnreachableMarker is the token a row carries once this sweep has
	// found its ground PERMANENTLY unreachable (the four dispositions that no
	// later pass can change by itself: the repo directory is gone, the
	// directory is a linked worktree of ANOTHER repo, the directory is this
	// row's own CLONE ground — the clone collection leg's, never this arm's —
	// or the directory is not a linked worktree of this repo at all).
	//
	// WHY IT EXISTS: the disposition for those three is a REFUSAL, and a
	// refusal is returned again by every tick of a host that runs the merger
	// or the worker — forever, because the ledger is append-only and no verb
	// deletes the row. The in-process memo below (reclaimRefusalSeen) only
	// silences a REFUSAL INSIDE ONE PROCESS; a resident merger that restarts,
	// and any concurrently running worker, re-prints the same line, so a log
	// that should carry the day's real faults carries this fossil instead
	// (devdoc/reclaim-fossil-20260915.md's sibling defect). The marker is the
	// DURABLE half of the same bargain: reported ONCE per row, and every later
	// pass — in this process or any other — is silent for it.
	//
	// WHAT IT IS NOT: it does not change the row's STATUS (the row is left
	// exactly as the ledger holds it), it does not touch the directory (Rule
	// 42), and it changes none of the three refusal criteria (they are the
	// same predicates, word for word).
	reclaimUnreachableMarker = "reclaim-unreachable"

	// reclaimUnreachableTrailArmClaim is the tail's SCOPING half: what THIS ARM
	// did, in the arm's own voice. Asserted by the regression test, so the scope
	// cannot be dropped from the sentence by a later edit.
	reclaimUnreachableTrailArmClaim = "this arm wrote no status transition and removed nothing"

	// reclaimUnreachableEventName is the append-only trail event under which
	// the marker is recorded — the ledger's OWN spelling, so a reader greps
	// task_events for it (`gopi task show` renders it as this row's verdict
	// line, see verdictTrailEventNames).
	reclaimUnreachableEventName = "reclaim-unreachable"

	// reclaimUnreachableAggregateNote is the ONE line a pass prints for the
	// rows it stayed silent about: a bounded summary keeps the condition
	// VISIBLE (Rule 15 — a permanent skip is disclosed, never swallowed) while
	// the per-row flood ends. It names a count, never a row: the rows are
	// named by the marker on their own trail.
	reclaimUnreachableAggregateNote = "dead-holder reclaim: %d row(s) skipped as previously recorded " +
		reclaimUnreachableMarker + " — the marker is on the row's trail; clear the ground to make it reclaimable again"

	// reclaimGroundOutcomeEvent is the append-only trail event under which THIS
	// pass records what its REMOVAL actually did — the one place the
	// directory's fate is stated, and it is written only after the removal ran.
	//
	// WHY A SECOND EVENT AND NOT THE REASON COLUMN (epic 01a17e1e, measured
	// 2026-10-27). reclaimDeadHolderCoDeath writes the row's reason BEFORE the
	// removal, because the close-out GATES the removal (a ground freed under a
	// live row cannot be resumed — the 2026-10-12 contract this file exists
	// for). That ordering is not negotiable; the sentence was. It used to end
	// "…CLOSED the attempt as failed and freed the directory (branch kept for
	// merge)", and on a row it could not free — the CLONE ground, whose
	// `deliverable.repo` and `deliverable.worktree` are the same path, so
	// `git worktree remove <clone>` exits 128 "is a main working tree" — the
	// ledger carried "freed the directory" while the directory stood, which
	// misinforms the reconcile pass and the operator about safe-merge gate [c],
	// the very gate this arm exists to unblock. The reason now states only what
	// the pass HOLDS when it writes it; the outcome lands here, afterwards.
	//
	// It is a member of verdictTrailEventNames (tasks.go), so `task show`
	// renders it on its `verdict:` line — a failed removal must not survive
	// only as one stderr line belonging to a process that has already exited
	// (Rule 15).
	reclaimGroundOutcomeEvent = "reclaim-ground-outcome"

	// reclaimCoDeathFreedClaim is the phrase the CO-DEATH REASON must never
	// carry: it is a claim about the removal, and the reason is written before
	// the removal exists. Asserted by the regression test, so a later edit
	// cannot put the outcome back into a sentence that cannot know it.
	reclaimCoDeathFreedClaim = "freed the directory"

	// reclaimGroundFreedMarker / reclaimGroundNotFreedMarker are the two halves
	// every reclaimGroundOutcomeEvent record opens with — the marker an operator
	// (and the regression test) greps to tell a freeing from a failed one
	// without reading the whole sentence.
	reclaimGroundFreedMarker    = "directory freed"
	reclaimGroundNotFreedMarker = "directory NOT freed"

	// reclaimFailedMarker is the report-line marker for a row whose removal was
	// ATTEMPTED and failed: the branch is still withheld (gate [c] still greps
	// it out of `git worktree list`), so the line must never read as a freeing —
	// the count of `reclaimRemovedMarker` lines stays an honest count.
	reclaimFailedMarker = "dead-holder worktree reclaim failed"

	// reclaimCoDeathReasonTail is the co-death REASON's tail: the facts this
	// pass HOLDS at the moment it writes it — the attempt is closed, and why,
	// and that the branch is kept. It deliberately says nothing about the
	// directory: the removal runs AFTER this write (the close-out GATES the
	// removal), so the reason cannot know the removal's outcome, and a sentence
	// that asserted one would be a claim the ledger can never retract
	// (reclaimGroundOutcomeEvent records the outcome afterwards, where
	// `task show` reads it).
	reclaimCoDeathReasonTail = " — a dead holder is not a finished row, and a ground freed under a live row " +
		"cannot be resumed, so this pass CLOSED the attempt as failed; the branch is kept for merge and " +
		"re-dispatch is a NEW row (the directory's own fate is recorded separately, after the removal ran: " +
		reclaimGroundOutcomeEvent + ")"

	// The three PERMANENT refusal wordings. Each is COMPOSED (see
	// reclaimDisposition) and each is the token this file classifies on, so the
	// wording a test greps and the wording the classifier reads can never
	// drift apart (Rule 13: one definition, referenced — never mirrored).
	//
	// reclaimUnreachableCloneGroundToken is the CLONE ground's own wording
	// (epic 01a17b2f, measured 2026-10-27: 113 of 114 `reclaim-unreachable`
	// markers carried the FOREIGN sentence about a directory whose `.git` is a
	// DIRECTORY). It is a PERMANENT refusal like the other three — a clone
	// ground is never this arm's to free, and the clone collection leg
	// (runGroundCollect) is the one that converges it — but it is NOT foreign,
	// so it is not silent: a row still STANDING (live status) is an anomaly,
	// and the sentence says the true thing.
	reclaimUnreachableRepoGoneToken      = " is gone — this row cannot be reconciled here"
	reclaimUnreachableForeignGroundToken = " is a linked worktree of ANOTHER repo, not "
	reclaimUnreachableCloneGroundToken   = " is a CLONE ground — its own repository, not a linked worktree of " +
		"another repo — and holds no worktree registration in "
	reclaimUnreachableUnregisteredToken = " is not a linked worktree of "
	reclaimUnreachableRefusalTail       = " — refusing to remove a directory this row does not own"
	// reclaimCloneCollectTail names the ONE leg that does converge a clone
	// ground, so the refusal is actionable rather than a dead end (Rule 15).
	reclaimCloneCollectTail = " — the clone ground is collected by the clone leg (runGroundCollect)"

	// reclaimStatusClean is the empty `git status --porcelain` output: a
	// clean worktree (guard ②).
	reclaimStatusClean = ""

	// reclaimPorcelainUntrackedMarker is porcelain's untracked-entry code. It
	// is the ONE discriminator between the two kinds of dirt (Rule 1b): an
	// entry that does NOT start with it changes a TRACKED file and is
	// somebody's uncommitted work.
	reclaimPorcelainUntrackedMarker = "??"

	// reclaimUntrackedResidueReason is the branch's name for guard ②'s OTHER
	// refusal — the ONE the classifier answers with. This merge found TWO
	// independently verified names for ONE state: ours and master's
	// reclaimUntrackedResidueMarker. The state is defined once below and this
	// name aliases it (Rules 13/14: never a second literal, never a mirrored
	// copy that can drift), so both sides' callers and regression tests read
	// the same string and the two operator wordings can never disagree about
	// which kind of dirt a tree holds.
	reclaimUntrackedResidueReason = reclaimUntrackedResidueMarker

	// reclaimSkipDirtyReason is guard ②'s refusal wording — the operator-sink
	// line the arm prints and the reason reclaimDisposition returns. ONE
	// spelling: the sweep's log and any verdict about a dirty directory must
	// refuse it in the same words (Rule 14).
	reclaimSkipDirtyReason = "worktree is DIRTY (uncommitted work) — left for a human"

	// reclaimUntrackedResidueMarker is reclaimSkipDirtyReason's SIBLING: the
	// same guard, the same refusal to remove, but the SUBJECT is different and
	// the distinction is the whole point of the marker. `git status --porcelain`
	// is non-empty in two states that used to be reported identically:
	//
	//	tracked modifications — a judgment call about somebody's WORK, left for
	//	  a human (reclaimSkipDirtyReason);
	//	UNTRACKED entries only — the tree's own content is IDENTICAL to its
	//	  commit, so no work is at risk: what pins it is the residue a run left
	//	  behind (a build output, a scratch file, a stray probe). A gopi run is
	//	  required to leave its worktree without untracked files at all, so this
	//	  state is a NAMED hygiene violation, not an unnameable "dirty" flood.
	//
	// Measured 2026-09-16 (this epic's case 1): the refusal arm rendered only
	// `reclaimSkipDirtyReason`, so an operator reading the live tally of
	// refusals cannot tell the hygiene violations from the genuine judgment
	// calls, and the one actionable class (a run that must clean up after
	// itself before it exits) is invisible.
	//
	// MERGE NOTE (resolve merge feat/worktree-refusal-arms-v2 into master): the
	// two sides worded this ONE new state differently — master above, the branch
	// "worktree is DIRTY (untracked-only residue, no tracked change) — the run
	// must clean its own scratch before it reports ready". The merge kept the
	// branch's WORDING because it is the one an operator contract already keys
	// on: the pre-existing DIRTY word is what every live arm, the salvage lane,
	// ClaimHolderFreshness and the red proofs match
	// (zz_redproof_reclaim_promise_test.go asserts the operator line carries
	// "DIRTY"), and master's sentence drops it. Everything master's wording was
	// FOR is preserved — the state is still named apart, still refused, still
	// reported as the run's own hygiene debt — and the two names for it
	// (this one and reclaimUntrackedResidueReason) are one definition, so the
	// split can never drift.
	reclaimUntrackedResidueMarker = "worktree is DIRTY (untracked-only residue, no tracked change) — the run must clean its own scratch before it reports ready"

	// reclaimUntrackedProbePrefix is the porcelain status prefix of an
	// untracked entry — the SAME discriminator as
	// reclaimPorcelainUntrackedMarker, under the name the finish-cleanup arm
	// and master's split read it by. This merge kept ONE definition and made
	// the other name an alias (Rules 13/14): two literals for one porcelain
	// code is exactly the drift surface the constant exists to remove. Every
	// OTHER porcelain v1 status (M/A/D/R/C/U/…) names a path the index or HEAD
	// already tracks, i.e. a possible home of real, committable work.
	reclaimUntrackedProbePrefix = reclaimPorcelainUntrackedMarker

	// reclaimSurvivedPruneNote is the operator note for the (rare) row whose
	// registration git did NOT drop even though its directory is gone (a
	// locked or unreadable administrative entry): recorded rather than
	// reported as a repair the pass did not make (Rule 15).
	reclaimSurvivedPruneNote = "registration survived prune"

	// reclaimDeadLocalMarker is the phrase the reclaim line carries when the
	// freed directory was held by a live-STATUS row whose local holder is
	// provably DEAD (orphan epic, 2026-09-12). It is the SHARED disposition
	// word — finishCleanupDeadLocalHolderMarker in task_finish_cleanup.go owns
	// the value, this alias keeps the reclaim arm from re-typing it (Rule 14),
	// and the row's own status follows it, so an operator sees at a glance
	// that the ledger still calls the row in_progress.
	reclaimDeadLocalMarker = finishCleanupDeadLocalHolderMarker

	// reclaimOrphanRowNote is the tag the report carries on a dead-holder row
	// whose STATUS was still live when this pass reached it (2026-10-12
	// incident, rows 01a130f0 / 01a130ee / 01a130e4). It is a CO-DEATH note, not
	// a warning about a stranding: the pass ends the attempt and frees the ground
	// in one act (reclaimDeadHolderCoDeath), so the line reports BOTH halves.
	//
	// What the incident was: the arm freed the DIRECTORY and left the ROW
	// `in_progress`, naming the row "still open" as a consequence an operator
	// must not miss. The note was the disclosure standing in for the missing
	// step (Rule 15) — and it was not enough. The ground was gone, the branch
	// deleted with it, and every spelling of `gopi worker --resume` (default
	// positional / --branch / --worktree) needs a ground, so the row was
	// permanently unclaimable and permanently open. The evidence that it was NOT
	// finished is the arm's own: a live status means no completion claim was
	// ever written (the completion IS that write), and all the pid layer proves
	// is that the holder DIED.
	//
	// "Ground gone + row open" is not a state any verb can leave, so the pass no
	// longer produces it — and it does not refuse either (releasing an orphaned
	// dead-holder row's directory is the deliberate, tested contract of the
	// 2026-09-12 orphan epic; refusing would reverse it). It closes the attempt.
	reclaimOrphanRowNote = "dead-holder co-death: the row's live status was CLOSED as failed in the same act that ATTEMPTED the release of its ground (a dead holder is not a finished row, and a ground freed under a live row can never be resumed) — re-dispatch is a NEW row, and the branch is kept for merge"
)

// reclaimPruneCall is the reclaim's registration-repair entry point (the
// `git worktree prune` invocation). It is a variable so a test can COUNT how
// many repo-wide prunes one pass runs: the pass must consult the registration
// list first and never spawn a prune it has no reason for — the prune is
// repo-wide, has no per-path form, and a pass with nothing stale must cost
// zero subprocesses.
var reclaimPruneCall = reclaimRepairRegistration

// reclaimDeadHolderRow is one terminal row whose branch a dead holder still
// occupies — the reconciliation unit of this file.
type reclaimDeadHolderRow struct {
	TaskID    string
	Status    string
	ClaimedBy string
	Repo      string
	Branch    string
	Worktree  string
	// LiveRow marks the orphan shape (orphan epic, 2026-09-12): the row's
	// STATUS is still live (pending/claimed/in_progress) while this host
	// proves its holder is dead. The disposition (free the directory) is the
	// same as a terminal dead holder's; the mark exists so the report names
	// the difference and never implies the row was closed out (Rule 15).
	LiveRow bool
	// Ident is the row's recorded RUNNER identity (pid + start ticks + cmd),
	// carried rather than re-read: the live-row veto asks it the same question
	// the candidate scan already asked (Rule 14 — one capture, one verdict).
	Ident procIdentity
}

// reclaimLiveSessionRefusal answers "is this row's run still breathing?" for
// the removal arm and NAMES the evidence ("" = nothing here proves a run is
// alive, so the arm's other guards decide).
//
// It is the removal arm's Guard ⓪ and it rests on channels readable ACROSS
// sessions, never on a foreign process's open files: the row's recorded runner
// identity (pid + start ticks + cmd — the 2026-09-14 fix) is asked first by the
// candidate scan itself, and the second question — a live process standing in
// the row's OWN session directory (`<gopi dir>/sessions/<task id>`, measured as
// the CWD of every worker that adopts a row) — is the one the /proc holder scan
// of guard ③ cannot answer: that scan keys on the DIRECTORY being removed, and
// under kernel.yama.ptrace_scope=1 only a `cwd` readlink survives cross-session.
//
// Why the recorded runner identity alone is not enough (the 2026-09-17 incident,
// row 01a0af50): the ledger records ONE runner per row, so a row running in TWO
// sessions loses its veto the moment the recorded runner exits — while the other
// session keeps building. The session-directory half is what keeps that window
// closed.
//
// THE ASKER'S OWN PROCESS IDENTITY IS NEVER EVIDENCE HERE (measured on the
// LEDGER-KEYED arm, row 01a0d58e). Both bits above were asked WITHOUT any
// exclusion, and that is a defect on this arm specifically: the candidate set is
// `claimed_by = MY claim stamp` (reclaimSelfRowsFrom), so the ledger's recorded
// runner for every candidate IS the process asking the question, and its pid is
// alive by construction (the worker's round-end defer runs inside the very
// process the claim stamp names). Measured, verbatim:
//
//	[own-release] claimed by this worker: zpeer:771230        ← the asker's own stamp
//	own worktree release refused: feat/own-release (rig-own-release-own)
//	  — live task rig-own-release-own still has a run standing
//	    (its recorded runner (pid 771230) is still running)
//
// Both lines quote ONE pid: the asker answered its own liveness question with
// itself. The production shape is every finish of a run whose candidate row
// records this process: its ground is never freed, `git worktree list` keeps
// naming it, and scripts/safe-merge.sh gate [c] keeps refusing the branch — the
// very withholding this arm exists to end.
//
// So the evidence is asked with the ASKER ITSELF REMOVED — and with the
// exclusion stopping there (see reclaimLiveSessionRefusal's own note: the tree
// would forgive a live runner that is the asker's descendant). What remains
// unforgiven is the whole point of the guard: a live run of ANOTHER process (a
// foreign session that kept working after the recorded runner exited, and a
// runner that is a CHILD of this one), and every third party standing in the
// row's own session directory, still refuse.
//
// Self-exclusion is not self-permission: the asker's own position is withheld by
// the arm's OWN-GROUND rung, which runs before this disposition ever does
// (guestRowOwnCheckoutRefusal, guest_row_guard.go — a process never frees the
// checkout it stands in, and the loss stays invisible until a later step asks for
// its own cwd).
func reclaimLiveSessionRefusal(r reclaimDeadHolderRow) string {
	// The asker's OWN pid is not evidence — and the exclusion is EXACTLY the
	// asker, never its process tree (measured 2026-09-25, this row). Widening it
	// to finishCleanupSelfTree would forgive a live runner that happens to be a
	// DESCENDANT of the asker, because the red-proof rigs plant the live run as a
	// CHILD of the process under test: five veto tests
	// (TestSelfReclaimRefusesALiveInProgressRowsGround,
	// TestReclaimRefusesALiveRunnersGroundWhenTheRowIsTerminal,
	// TestZZRedProofSelfReclaimKeepsALiveRunnersGround, …) all went from refusing
	// to FREEING a live run's ground. A child's liveness IS a veto; only the
	// questioner answering itself is a non-answer.
	//
	// The exclusion covers the predicate's ① bit ONLY (the recorded runner). Its ②
	// bit — the session-directory scan — is asked unexcluded on purpose: the
	// 2026-09-17 concurrent-session guard's fixture places the ASKING process
	// inside the row's session directory, so there the asker's own position IS the
	// evidence (see worktreeSweepSessionLiveEvidenceFor's comment).
	if live := worktreeSweepSessionLiveEvidenceFor(r.Status, r.Ident, r.TaskID, os.Getpid()); live != "" {
		return "live task " + r.TaskID + " still has a run standing (" + live + ") — refusing to free a directory or branch a running session may be building in"
	}
	// ② the TERMINAL-status arm, which ① structurally cannot reach. A row's
	// status is written by a DIFFERENT party than the process the row records
	// as its runner: the wrapper writes `completed` the instant the child
	// EXITS, and a run whose own close-out already refused (a conflicted
	// pre-merge, a failed vet) still has a live process standing in its ground
	// — measured 2026-09-21, row 01a0c574: its directory was removed at
	// 23:09:45 by the self-reclaim arm on the evidence "disposition
	// reclaimFreable: " (an EMPTY checklist) while a live process of that run
	// was still working, the row's alignment having REFUSED in the same second.
	// A terminal status is therefore NOT evidence that the run ended; the
	// process record is. Asked on the SAME channels the rest of the tree uses
	// (Rules 13/14 — one liveness ladder, never a second one).
	if why := reclaimForeignRunnerRefusal(r); why != "" {
		return why
	}
	return ""
}

// reclaimForeignRunnerRefusal answers "is a live process that is NOT this one
// still running this row?" — the veto the live-status arm cannot express, and
// the honest reading of "the run is over".
//
// SELF IS EXCLUDED ON PURPOSE, and that exclusion is the producer's whole
// mandate: the release pass runs INSIDE the process that holds the row (the
// wrapper's close-out tail, the worker's round defer), so its own pid is
// necessarily alive at the moment it releases its own finished ground. Reading
// "my own pid is alive" as "the run is live" would refuse every legitimate
// release — measured on this arm, where the candidate set is `claimed_by = my
// stamp`, so the ledger's recorded runner for a candidate IS this process
// (the refusal line and the claim line quoted the same pid, row 01a0d58e).
//
// The exclusion is the ASKER'S OWN PID, not its process tree: the two differ
// exactly where it matters — a tree-wide set forgives a live runner that is the
// asker's DESCENDANT (the shape the red-proof rigs plant), and a child's
// liveness is a VETO, not a self-answer. Measured on this row: the tree-wide
// form freed five live runs' grounds that must be kept. A genuinely FOREIGN
// runner — another session's process still working the same row — keeps its veto
// (the positive control of the regression test).
//
// The channel is the row's recorded RUNNER identity (proc_pid + proc_start_ticks
// + proc_cmd) — the cross-session triple of the 2026-09-14 fix, and the one
// property that makes the question answerable at all after the ledger CLEARS
// those columns on a terminal transition: the SELF arm's rows carry the triple
// explicitly (reclaimSelfRowsFrom), because on this arm the row's status is
// written by a different party than the process it records as its runner.
//
// The CLAIM STAMP's pid is deliberately NOT re-asked here. On this arm it names
// THIS process by construction (the candidate set is `claimed_by = my stamp`),
// so the self-exclusion above would make it vacuous; and the arm that does see
// a foreign claim — the dead-holder scan — already refused such a row before it
// ever reached this disposition (`reclaimDeadHolderCandidate`'s pidAlive check).
// One implementation per question (Rules 13/14), not a third redundant rung.
func reclaimForeignRunnerRefusal(r reclaimDeadHolderRow) string {
	if r.Ident.Pid <= 0 || r.Ident.Pid == os.Getpid() {
		return ""
	}
	if finishCleanupProcessHolderLive(r.Ident) {
		return "row " + r.TaskID + " still has a run standing (its recorded runner pid " +
			strconv.Itoa(r.Ident.Pid) + " is alive) — a terminal status is not proof the run ended, so this ground is not free"
	}
	return ""
}

// detailSuffix is the report fragment a candidate's disposition adds to its
// line — empty for the ordinary (terminal) dead holder, the orphan note for a
// live-status row. One function so both the removal line and the report-only
// lines carry the same words (Rule 14).
func (r reclaimDeadHolderRow) detailSuffix() string {
	if !r.LiveRow {
		return ""
	}
	return " [" + reclaimDeadLocalMarker + " " + r.Status + ": " + reclaimOrphanRowNote + "]"
}

// reclaimDisposition is what one candidate row is DUE this pass. The step is
// what converges the candidate set: a row whose directory is already gone is
// repaired to "nothing left to free" (git prune drops the registration) rather
// than re-attempted every tick, and a row whose ground is unreadable is
// reported ONCE, not as a fresh failure burst every pass.
type reclaimVerdict int

const (
	reclaimSkip           reclaimVerdict = iota // refusal or unreadable ground — this file's sweep is not the right arm
	reclaimFreable                              // clean + unowned: this pass's job is to free the directory
	reclaimVanishedProven                       // directory gone AND git still lists that exact path: the repair is due
)

// reclaimStage names HOW FAR the disposition got before it answered — the
// question a candidate row had actually reached. It exists because "not this
// pass's to free" is four different facts and only ONE of them is a refusal
// worth a line:
//
//	reclaimStageGround — the GROUND's own identity was asked and answered: the
//	  row's repo is not on this host, its directory is gone, or the path is no
//	  worktree of that repo at all. For a row whose life is OVER this is the
//	  NORMAL end state of a finished run (its ground was released, or the row
//	  was re-homed), not an anomaly — see reclaimTerminalRowUnreachable.
//	reclaimStageRow — the ROW's own identity refused it before the ground was
//	  ever probed (a live session standing on the row, a guest's manual run).
//	  Nothing about the directory has been established, so an empty reason here
//	  is the "registration already clean" case and a named one is a refusal.
//	reclaimStageLive — the ground EXISTS and the row still owes something to
//	  it (uncommitted work, a live process inside, a foreign branch checked
//	  out). Always a real refusal: something is standing that a human or a run
//	  must resolve.
//	reclaimStageForeign — the path is a linked worktree of ANOTHER repo. It is
//	  in the ground family (nothing here is this row's) yet distinct from it,
//	  because Rule 42 makes it permanently out of reach: there is no later pass
//	  that could ever make this arm's answer different, for ANY status, so a
//	  line about it is not a disclosure — it is a fossil.
//	reclaimStageClone — the path is a CLONE ground: `.git` is a DIRECTORY, so
//	  the directory IS its own repository (measured by
//	  devworkspace.GroundModelOf, this repository's ONE shape discriminator).
//	  It is NOT foreign — the question that used to produce that verdict
//	  (`git -C <wt> worktree list` names <wt>) is answered "yes" by EVERY
//	  repository naming its own main checkout, which is what a clone ground is.
//	  It belongs to the GROUND family for the same Rule 42 reason (this arm
//	  does not `os.RemoveAll` somebody's clone — devworkspace.Reclaim, driven by
//	  the clone collection leg runGroundCollect, is the one writer that does),
//	  but unlike reclaimStageForeign it is NOT silent for a live row: a run that
//	  died and left its clone ground behind is exactly what an operator must
//	  read (Rule 15), and the wording says what the directory is.
//	reclaimStageOther — no stage was reached (the verdict is not a skip).
type reclaimStage int

const (
	reclaimStageOther   reclaimStage = iota // conservative default: judge the row as an ordinary refusal
	reclaimStageRow                         // the row's identity refused it; the ground was not asked about
	reclaimStageGround                      // the ground's identity refused it (repo/directory gone, unregistered)
	reclaimStageLive                        // the ground exists and is not free for a reason that can clear
	reclaimStageForeign                     // the path is ANOTHER repo's linked worktree — never this row's to free
	reclaimStageClone                       // the path is this row's CLONE ground (its own repository) — the clone collection leg's, not this arm's
)

// String names a verdict for the AUDIT RECORD (Rule 14: one rendering of the
// verdict, so two arms cannot describe the same disposition differently). The
// zero value renders as its constant's own name rather than a bare "0", because
// a record whose evidence says "verdict 0" is exactly the unattributable shape
// this epic removes.
func (v reclaimVerdict) String() string {
	switch v {
	case reclaimSkip:
		return "reclaimSkip"
	case reclaimFreable:
		return "reclaimFreable"
	case reclaimVanishedProven:
		return "reclaimVanishedProven"
	default:
		return "reclaimVerdict(" + strconv.Itoa(int(v)) + ")"
	}
}

// reclaimRefusalSeen memoizes the refusals this PROCESS has already reported,
// keyed by row+reason. The convergence contract in reclaimDisposition's doc
// ("reported ONCE, not as a fresh failure burst every pass") was only honour-
// ed for the empty-reason case; a refusal that CANNOT converge — the clearest
// one is a row whose REPO IS GONE, where the ledger is append-only and no verb
// deletes the row — was re-printed by every pass, forever. Measured live
// 2026-09-15: a single fixture row (t-stale-sibling, deliverable naming the
// never-created /tmp/other-repo) made the merger's 5-minute loop emit
// "[task-notify] dead-holder reclaim  (t-stale-sibling) skipped: repo
// directory /tmp/other-repo is gone — this row cannot be reconciled here"
// every tick, so the operator's log was a standing puzzle (devdoc/
// reclaim-fossil-20260915.md) and a REAL refusal had to be read out of the
// noise of a permanent one.
//
// Silence after the first report is the same bargain path_policy's
// settingsWarnOnce strikes: one loud line names the condition, and a line
// nobody can act on (the row is not this pass's to remove) must not become
// the steady state of the sweep. The FIRST occurrence is always reported, so
// Rule 15 holds: the refusal is disclosed, never swallowed.
// It is a POINTER so a test can install a fresh memo without copying a
// sync.Map (which carries a noCopy marker).
var reclaimRefusalSeen = &sync.Map{} // refusal key -> struct{}

// reclaimRefusalKey is the identity of one refusal: the row AND the reason, so
// a row whose refusal REASON changes (a fresh diagnosis of the same row) is
// reported again rather than hidden behind the first verdict.
func reclaimRefusalKey(r reclaimDeadHolderRow, why string) string {
	return r.TaskID + "\x00" + r.Repo + "\x00" + why
}

// reclaimRefuse reports one refusal at most once per ROW, on two levels that
// answer different failures:
//
//	the PROCESS memo (reclaimRefusalSeen) — a row+reason this process already
//	  printed is not printed again by this process;
//	the LEDGER marker (reclaimUnreachableRecorded) — a PERMANENT refusal is
//	  recorded on the row's append-only trail the first time, and from then on
//	  every pass (this process, a restarted merger, a concurrent worker) is
//	  silent for that row.
//
// The second level is what ends the FLOOD: a resident merger's log carried the
// same sentence per tick forever, because a process-local memo cannot outlive
// the process that made it. Only the three PERMANENT refusals get the marker —
// a transient one (a dirty tree, an unreadable status, a live process inside)
// is expected to clear itself and must still be reported when it returns.
//
// `silent` is the caller's count of rows this pass stayed quiet about, so the
// pass can print ONE aggregate line for them (reclaimUnreachableAggregateNote)
// instead of nothing at all.
//
// It returns whether this call was the reporting one (tests assert the count).
func reclaimRefuse(r reclaimDeadHolderRow, why string, silent *int) bool {
	if why == "" {
		// The "registration already clean" case: nothing to do, nothing to
		// say — a line per pass would be the very burst this fix ends.
		return false
	}
	permanent := reclaimRefusalIsPermanent(why)
	if permanent && reclaimUnreachableRecorded(r.TaskID) {
		if silent != nil {
			*silent++
		}
		return false
	}
	if _, seen := reclaimRefusalSeen.LoadOrStore(reclaimRefusalKey(r, why), struct{}{}); seen {
		// This process printed this row+reason already. A PERMANENT refusal
		// still gets its marker written (the memo is not the durable record;
		// a ledger read is), but nothing is printed.
		if permanent {
			reclaimUnreachableRecord(r, why)
			if silent != nil {
				*silent++
			}
		}
		return false
	}
	taskNotifyLog("dead-holder reclaim %s (%s) skipped: %s", r.Branch, r.TaskID, why)
	if permanent {
		reclaimUnreachableRecord(r, why)
	}
	return true
}

// reclaimRefusalIsPermanent answers "can any later pass of this sweep clear
// this refusal by itself?" for the three dispositions that cannot be cleared
// from here: the repo is gone, the directory belongs to another repo, or the
// directory is not this repo's worktree at all. Those are decided by the
// GROUND and the LEDGER, never by this arm, so re-asking them every tick
// produces the same sentence forever.
//
// A refusal whose grounds can change (dirty tree, unreadable status, a live
// process inside the directory) is NOT permanent and keeps its old behaviour:
// it is reported when it appears, and again if it returns later.
func reclaimRefusalIsPermanent(why string) bool {
	return strings.Contains(why, reclaimUnreachableRepoGoneToken) ||
		strings.Contains(why, reclaimUnreachableForeignGroundToken) ||
		strings.Contains(why, reclaimUnreachableCloneGroundToken) ||
		strings.Contains(why, reclaimUnreachableUnregisteredToken)
}

// reclaimUnreachableRecorded reports whether this row's trail already carries
// the reclaim-unreachable marker. Read-only (openTasksDBReadOnly), and an
// unreadable ledger answers FALSE: an unknown state must not silence a report
// (Rule 15 — a disclosure is only withheld on evidence).
func reclaimUnreachableRecorded(taskID string) bool {
	taskID = taskIDNormalize(taskID)
	if taskID == "" {
		return false
	}
	ctx := context.Background()
	db, err := openTasksDBReadOnly(ctx, memoryDBPath())
	if err != nil {
		taskNotifyLog("reclaim marker read for %s failed (%v) — reporting as if unrecorded", taskID, err)
		return false
	}
	defer db.Close()
	var one int
	err = db.QueryRowContext(ctx,
		`SELECT 1 FROM task_events WHERE task_id = ? AND event = ? LIMIT 1`,
		taskID, reclaimUnreachableEventName).Scan(&one)
	return err == nil
}

// reclaimUnreachableRecord lands the marker on the row's append-only trail
// through the ledger's ONE trail writer (AppendTaskTrailEvent — a note, never a
// status transition), so nothing about the row's STATUS changes and the marker
// is readable by `task show` (verdictTrailEventNames) and by any later pass.
// Best-effort by the trail's own contract: a failure is reported, never allowed
// to break the pass.
func reclaimUnreachableRecord(r reclaimDeadHolderRow, why string) {
	AppendTaskTrailEvent(r.TaskID, reclaimUnreachableEventName,
		reclaimUnreachableMarker+": "+why+" ("+reclaimUnreachableTrailTail(r.Status)+")")
	// AND WHAT THE REFUSAL LEAVES BEHIND, on the SAME row (epic: failed row,
	// stranded commit — measured 2026-11-01 on 01a16043-2b43). The marker above
	// closes with "this pass will not report this row again", so without this
	// second record the arm's last word about a clone ground that holds the only
	// copy of a run's commits is that refusal.
	//
	// PRECISE, because the first wording overstated it: the ledger DID carry the
	// tip on 65 of 82 undelivered grounds via `delivery-proof-owed` (804 chars on
	// 01a16043, naming f2215b227ac3) — but that event is not in
	// verdictTrailEventNames, so `task show` never rendered it, and 8 grounds had
	// no delivery record at all. This record is written by the arm that refused
	// the ground, is rendered, and covers the case nothing else covers.
	//
	// `task_reclaim_stranded_commits.go` owns the measurement and every word of
	// the record; this is only the call site, placed here because this function
	// is the arm's single writer of the permanent-refusal marker and its own
	// caller already gates it to once per row (reclaimUnreachableRecorded).
	reclaimStrandedCommitsRecord(r)
}

// reclaimGroundOutcomeRecord lands the removal's OWN outcome on the row's
// append-only trail through the ledger's ONE trail writer (AppendTaskTrailEvent
// — a note, never a status transition), AFTER the removal ran and with the
// result it produced.
//
// WHY IT MUST EXIST AT ALL (epic 01a17e1e, measured 2026-10-27). A failed
// removal used to leave exactly one trace — a taskNotifyLog line in the operator
// sink — which is an egress for a process that may have exited hours later, and
// is invisible to `task show`, the surface an operator and the reconcile pass
// actually read. The row's own reason was no substitute: it is written before
// the removal, so it can only be guessed at (and used to be guessed wrong).
//
// `freed` is the measured result, never an assumption: the caller passes the
// value it obtained from reclaimGitRemove. Best-effort by the trail's own
// contract — a ledger that cannot be opened is reported (Rule 15) and never
// rolls back the removal that already happened.
func reclaimGroundOutcomeRecord(r reclaimDeadHolderRow, freed bool, detail string) {
	marker := reclaimGroundNotFreedMarker
	if freed {
		marker = reclaimGroundFreedMarker
	}
	body := marker + ": " + r.Worktree + " — branch " + r.Branch + " kept for merge"
	if strings.TrimSpace(detail) != "" {
		body += "; " + detail
	}
	AppendTaskTrailEvent(r.TaskID, reclaimGroundOutcomeEvent, body)
}

// reclaimUnreachableTrailTail renders the sentence every `reclaim-unreachable`
// marker ends with, SCOPED TO THIS ARM and carrying the status the arm OBSERVED.
//
// THE DEFECT IT REPLACES (measured 2026-10-27, epic 01a17dd0). The tail used to
// read "(row status and directory left untouched; this pass will not report this
// row again)". That was true about the ARM — it writes a task_events note and
// nothing else — but it was written as a claim about the ROW, and it is
// append-only, so it can never be withdrawn. Rows 01a17a67 / 01a17a5f carry
// theirs from 11:58:37 (1793073517) while their status read `completed` by
// 22:42:21 / 22:42:26 (1793112141 / 1793112146) — moved NOT by this arm but by an
// operator's `gopi task update <id> --status completed` with
// GOPI_TASK_DELIVERY_PROOF_ALLOW=1 (session 66243525-16ab-4ce7-9133-84d74c539840,
// whose trail events 10101/10102 and 10103/10104 are the write). `task show`
// renders that verdict line directly UNDER `status:`, so the operator read a
// contradiction: "status: completed" above, "row status … left untouched" below.
//
// WHAT THE NEW SENTENCE DOES, and why it cannot rot the same way:
//   - it names the ARM as the author ("this arm"), never the row;
//   - it records the status the arm SAW and when it saw it, so a later reader
//     compares two timestamps instead of reading a contradiction — the snapshot
//     is the arm's evidence, not a promise about the row's future;
//   - it points at the row's own events for whatever moved the row afterwards,
//     which is where that fact actually lives.
//
// The wording is a constant (Rule 1/13) because the arm, its classifier and the
// regression test must read the SAME spelling — a tail a test greps and a tail
// the arm writes can otherwise drift apart silently.
func reclaimUnreachableTrailTail(observedStatus string) string {
	return "this arm wrote no status transition and removed nothing — the row's status was " +
		quoteOrUnknown(observedStatus) + " when this arm saw it (a task_events note only; " +
		"any LATER status change on this row is another actor's, see this row's own events); " +
		"this pass will not report this row again"
}

// reclaimUnreachableTrailStatusUnknown is what the tail says when the row's
// status could not be read off the candidate: an unknown is NAMED, never
// rendered as a plausible value (Rule 15 — a recording that guesses is worse
// than one that says it could not look).
const reclaimUnreachableTrailStatusUnknown = "unread"

// quoteOrUnknown renders an observed status for the tail, naming an empty one
// rather than printing a pair of empty quotes a reader would misread as "".
func quoteOrUnknown(status string) string {
	if strings.TrimSpace(status) == "" {
		return reclaimUnreachableTrailStatusUnknown
	}
	return `"` + status + `"`
}

// reclaimUnreachableRepoGoneReason / reclaimUnreachableForeignGroundReason /
// reclaimUnreachableUnregisteredReason render the three PERMANENT refusal
// wordings. They are functions rather than inline concatenations so that the
// classifier (reclaimRefusalIsPermanent) and every test read the same tokens
// the arm emits — the wording can never drift from the thing that keys on it.
func reclaimUnreachableRepoGoneReason(repo string) string {
	return "repo directory " + repo + reclaimUnreachableRepoGoneToken
}

func reclaimUnreachableForeignGroundReason(repo string) string {
	return "path" + reclaimUnreachableForeignGroundToken + repo + reclaimUnreachableRefusalTail
}

// reclaimUnreachableCloneGroundReason is the CLONE ground's refusal: it names
// what the directory IS (its own repository) and what this arm is not (its
// remover), so the sentence an operator reads is true (Rule 15). Called only
// after devworkspace.GroundModelOf measured the CLONE shape.
func reclaimUnreachableCloneGroundReason(repo string) string {
	return "path" + reclaimUnreachableCloneGroundToken + repo + reclaimUnreachableRefusalTail +
		reclaimCloneCollectTail
}

func reclaimUnreachableUnregisteredReason(repo string) string {
	return "path exists but" + reclaimUnreachableUnregisteredToken + repo + reclaimUnreachableRefusalTail
}

// reclaimDeadHolderCoDeath is the ONE answer to "the holder of a LIVE-STATUS row
// is provably dead": END the row and free its ground in the same act — or refuse
// both. It is the fix for the 2026-10-12 incident (rows 01a130f0 / 01a130ee /
// 01a130e4), where the ground was freed at 16:28–16:39 and the ROW was left
// `in_progress`, permanently.
//
// WHY NOT A REFUSAL. Refusing is the other shape co-death admits, and it is
// WRONG here: releasing an orphaned dead-holder row's directory is a DELIBERATE,
// tested contract. The orphan epic of 2026-09-12 exists precisely because these
// rows' directories — and the branch gate [c] refuses — stayed parked, and a
// human had to clear them BY HAND (TestDeadHolderReclaimFreesALiveStatusOrphan,
// TestAcceptance_DeadLocalHolderOrphanReleasesItsDirectory, and the acceptance
// chain that ends with safe-merge exiting 0). Refusing would reverse all three.
//
// WHY NOT A BARE RELEASE (what the incident did). `in_progress` is a LIVE status
// and nothing else on this host ends it: the stale-report grace (1800s by
// default) only REPORTS the row — since 2026-10-26 nothing disposes it
// (devdoc/zombie-sweep-removed.md) — so a reclaim minutes after the claim leaves
// the row open with its ground gone — and every resume spelling needs the ground (the default positional
// target resolves the row's recorded worktree; --branch/--worktree name it
// outright). "Ground gone, row open" is a state no verb can leave: not finished,
// not resumable, not re-dispatchable.
//
// WHY `failed`, AND WHY THE WRITE IS A PRECONDITION. The dispatcher's table
// (taskAllowedTransitions) has NO path from a live status back to a resumable
// one — `in_progress` leaves only for terminal, and the ledger's own word for
// "this attempt ended, the work must run again" is `failed` plus a REISSUED
// pending row (append-only: "the rework itself is a NEW pending row"). So
// `failed` is the terminal state that both closes the row and makes re-dispatch
// reachable — a row left live with no ground is the one state that makes neither
// true.
//
// The write therefore happens BEFORE the removal and GATES it (Rule 15): an error
// — or a table that forbids the move — refuses the release with the reason named,
// so the incident's state is never produced by this pass. The row is only ever
// moved by the ONE mutation core (TaskUpdateReason), which records the WHY on the
// append-only task_events trail.
func reclaimDeadHolderCoDeath(r *reclaimDeadHolderRow) error {
	if !taskLiveHolderStatus(r.Status) {
		return nil // already terminal: the incumbent path owns this row
	}
	reason := "dead-holder reclaim: holder " + r.ClaimedBy + " is provably dead while the row is still " +
		r.Status + reclaimCoDeathReasonTail
	if _, err := TaskUpdateReason(r.TaskID, taskStatusFailed, reason, ""); err != nil {
		return err
	}
	// The status the audit record and the report now carry is the one this write
	// produced, not the pre-write image.
	r.Status = taskStatusFailed
	return nil
}

// ReclaimDeadHolderWorktrees frees every worktree the ledger attributes to a
// PROVABLY DEAD holder and returns one report line per freed (or, with
// remove=false, per reclaimable) branch. It WAS the finish-cleanup sweep's
// repair leg; that sweep is retired (58b5618b) and this arm is not — its faces
// are the MERGER pass's mergeDeadHolderReclaimLeg (the live one) and the
// worker-tick sites that predate the retirement. It exists because the
// close-out row scans never reach these rows (their status is terminal but
// their holder is the reason nothing moved), so the directory — and gate [c] —
// stay put forever without it.
//
// The report is the point even when removal is off: a branch withheld from
// merging by a dead holder must never be a silent protected count again.
func ReclaimDeadHolderWorktrees(remove bool) []string {
	rows, err := reclaimDeadHolderRows()
	if err != nil {
		// Rule 15: a ledger that cannot be read is REPORTED, never rendered
		// as "nothing to reclaim".
		return []string{reclaimCandidateMarker + ": ledger scan failed: " + err.Error()}
	}
	// The registration repair is repo-wide (`git worktree prune` has no
	// per-path form) so one pass plans it ACROSS rows: the lister is memoized
	// per repo, so a ledger with hundreds of vanished rows in one repo costs
	// one `git worktree list` and at most one prune — and zero of either when
	// nothing is stale.
	prunes := newReclaimPrunePlan()
	var out []string
	var toRepair []reclaimDeadHolderRow
	// silent counts the rows this pass withheld a line for because an earlier
	// pass already recorded them (reclaimRefuse's ledger half): ONE aggregate
	// line at the end keeps the condition visible without the per-row flood.
	silent := 0
	for i := range rows {
		r := &rows[i]
		disp, why, stage := reclaimDispositionJudged(*r, prunes)
		// A refusal is a fact about ONE directory, not a repair: it goes to
		// the operator sink (taskNotifyLog — the same surface the sweeps use)
		// and never into the returned repair list, so "the sweep freed N"
		// stays an honest count.
		if disp == reclaimSkip {
			// A row whose LIFE IS OVER and whose ground was never found here is
			// not an anomaly to be disclosed — it is the ordinary end state of a
			// finished run, and it leaves the report before any wording is
			// chosen (reclaimTerminalRowUnreachable). Asked BEFORE reclaimRefuse
			// on purpose: the marker, the memo and the aggregate count are all
			// consequences of "there is something here to disclose", which is
			// exactly what this row does not have.
			if reclaimTerminalRowUnreachable(*r, stage) {
				continue
			}
			reclaimRefuse(*r, why, &silent)
			continue
		}
		// A directory the holder no longer occupies is REPAIRED here (git
		// prune drops the stale registration gate [c] greps) and then leaves
		// the candidate set — the one disposition that converges. A repair
		// that cannot run keeps the row a NAMED candidate (the conservative
		// half: the branch stays visible as possibly-occupied).
		if disp == reclaimVanishedProven {
			toRepair = append(toRepair, *r)
			continue
		}
		if !remove {
			out = append(out, reclaimCandidateMarker+r.detailSuffix()+": "+r.Branch+" ("+r.TaskID+", "+r.ClaimedBy+")")
			continue
		}
		if !reclaimRemovalEnabled() {
			// The kill switch is report-only by contract: the branch is still
			// NAMED (an operator must see what is being withheld), nothing is
			// removed.
			out = append(out, reclaimCandidateMarker+r.detailSuffix()+": "+r.Branch+" ("+r.TaskID+", "+r.ClaimedBy+") — removal disabled by "+ReclaimDeadHolderEnv)
			continue
		}
		// THE DETERMINISTIC CHECKLIST (reclaim_ground_checklist.go): rung ① the
		// ground is not mine, rung ② the branch shares history with the
		// integration branch. The row's own life and its holder are NOT re-asked
		// here — reclaimDisposition above owns them, and asking twice would be the
		// second implementation Rules 13/14 forbid. A refusal KEEPS the directory
		// and NAMES the failed check, so the gap stays observable instead of
		// becoming a silent skip.
		if why := reclaimGroundChecklistVerdict(r.Repo, r.Worktree, r.Branch); why != "" {
			out = append(out, reclaimCandidateMarker+r.detailSuffix()+": "+r.Branch+" ("+r.TaskID+", "+r.ClaimedBy+") — "+why)
			continue
		}
		// CO-DEATH (the 2026-10-12 fix, reclaimDeadHolderCoDeath): a row still in a
		// LIVE status has its attempt ENDED in the same act that frees its ground —
		// or neither happens. A row that cannot be closed keeps its directory, so
		// the incident's "ground gone, row open" is unreachable from here.
		if err := reclaimDeadHolderCoDeath(r); err != nil {
			out = append(out, reclaimCandidateMarker+r.detailSuffix()+": "+r.Branch+" ("+r.TaskID+", "+r.ClaimedBy+
				") — NOT freed: the row could not be closed out (a ground freed under a live row cannot be resumed): "+err.Error())
			continue
		}
		// Audit record BEFORE the removal (package removalaudit). The deciding
		// evidence is this row's own disposition: a holder proven dead. The
		// live-session channel is recorded AS CONSULTED, with what it ANSWERED —
		// it is the one veto this verdict must have consulted (the sibling
		// live-row-session-veto arm shares the evidence:
		// worktreeSweepSessionLiveEvidence / reclaimLiveSessionRefusal).
		//
		// WORDING FIX (2026-10-12 incident). This sentence used to read "past the
		// <live-session-veto> channel", which an operator read — reasonably — as
		// "this arm BYPASSED the veto". It did not: reclaimDisposition's Guard ⓪
		// calls reclaimLiveSessionRefusal FIRST and returns here only because
		// that call answered "". But "past" cannot distinguish "consulted and it
		// raised no objection" from "went around it", and a record that reads as
		// a bypass when it is not one is indistinguishable from the real thing —
		// which is how a post-mortem is misled. The sentence now says what
		// happened: the channels were consulted, and they did not refuse.
		removalaudit.WorktreeRemove(removalaudit.ArmRowReclaim, r.Repo, r.Worktree, r.Branch, r.TaskID,
			"dead-holder disposition "+disp.String()+" — decided only after the "+
				removalaudit.EvidenceChannelLiveSession+" channel was consulted and did not refuse ("+why+
				"); row status "+r.Status+" is terminal, so the channel's live-status arm was not applicable "+
				"(worktreeSweepSessionLiveEvidenceFor returns no evidence for a terminal row); checklist "+reclaimGoalChecklistSummary)
		if err := reclaimGitRemove(r.Repo, r.Worktree); err != nil {
			// THE REMOVAL'S OUTCOME IS A DURABLE FACT, on the row's OWN trail —
			// the surface `task show` renders — not merely a sink line
			// (reclaimGroundOutcomeRecord's doc: the reason was already written
			// and cannot carry it). Both sinks get it: the trail for the reader
			// of the row, taskNotifyLog for a host with no chat attached.
			reclaimGroundOutcomeRecord(*r, false, "removal attempted after the row was closed: "+err.Error())
			taskNotifyLog("dead-holder reclaim %s (%s) remove failed: %v", r.Branch, r.TaskID, err)
			// Fail-soft per row, loud per row: one unremovable worktree never
			// stops the pass on the others — and its line must not read as a
			// freeing, because the branch is still withheld from merging.
			out = append(out, reclaimFailedMarker+r.detailSuffix()+": "+r.Branch+" ("+r.TaskID+") — "+
				reclaimGroundNotFreedMarker+" (the row was closed separately; the branch stays withheld from "+
				"merging until the ground is cleared): "+err.Error())
			continue
		}
		// The freeing is recorded only NOW — after the removal succeeded — so
		// the ledger never holds a claim this pass did not obtain.
		reclaimGroundOutcomeRecord(*r, true, "")
		out = append(out, reclaimRemovedMarker+r.detailSuffix()+": "+r.Branch+" ("+r.TaskID+") freed for merge; branch kept")
	}
	out = append(out, prunes.repair(toRepair, remove)...)
	// ONE line for the rows this pass stayed silent about (and which an
	// earlier pass already recorded on their trail) — the condition stays
	// visible without re-printing a sentence per row per tick.
	if silent > 0 {
		taskNotifyLog(reclaimUnreachableAggregateNote, silent)
	}
	return out
}

// reclaimRemovalEnabled reads the kill switch (one reader, Rule 2); the OFF
// spellings come from the ONE vocabulary definition (internal/boolvocab) rather
// than a list re-spelled here (Rules 13/14).
func reclaimRemovalEnabled() bool {
	return !boolvocab.IsFalse(os.Getenv(ReclaimDeadHolderEnv))
}

// reclaimDisposition decides what one candidate row is due this pass: the
// three guards, plus the two states the guards alone got wrong.
//
// The guards answer "may this directory be removed?" but the sweep also has to
// answer "is there anything left to remove?"
//
//	reclaimVanishedProven — the ledger records a worktree whose DIRECTORY IS
//	  GONE *and* the repo's worktree list still names that exact path.
//	  This file used to treat "directory gone" as "all guards held" (nothing
//	  to inspect, so nothing refuses) and fall through to the removal, which
//	  git answers with exit 128 ("is not a working tree") on EVERY lifecycle
//	  pass, forever: the registration is dropped only by a successful remove
//	  or by `git worktree prune`, so the doomed attempt never converges. It is
//	  also the state that keeps safe-merge gate [c] blocking the branch,
//	  because `git worktree list` (which [c] greps) keeps printing a
//	  gone-but-registered worktree until the registration is pruned. The "git
//	  still lists it" half is what makes the repair a REPAIR: once the
//	  registration is gone (the fixed ledger's normal state) the row is a
//	  silent skip — there is nothing left to converge, and a repo-wide prune
//	  per row would be the same burst one layer down.
//	reclaimSkip — a refusal (the returned reason names it), or the
//	  registration-already-clean case (an EMPTY reason: nothing to do, nothing
//	  to say).
func reclaimDispositionJudged(r reclaimDeadHolderRow, prunes *reclaimPrunePlan) (reclaimVerdict, string, reclaimStage) {
	if strings.TrimSpace(r.Worktree) == "" || strings.TrimSpace(r.Repo) == "" {
		return reclaimSkip, "row carries no worktree/repo — nothing to free", reclaimStageGround
	}
	// Guard ⓪ THE LIVE ROW VETO, asked FIRST and on the LEDGER alone — before
	// any probe that needs the directory to exist. It exists because the
	// earlier guards cannot answer it: guard ③ is a /proc scan of processes
	// standing inside THIS directory, which is blind to a foreign session's
	// process under kernel.yama.ptrace_scope=1 (measured 2026-09-14: 185 of
	// ~360 /proc entries unreadable across sessions), and the row's recorded
	// runner names only ONE of a row's sessions. A row whose runner record this
	// host cannot prove dead — or that still has a live process standing in its
	// own session directory — is still working, and its rows must never have a
	// directory or a branch removed underneath them. Checked before the
	// repo/directory probes so the refusal is the SAME sentence whether the
	// directory is present, gone, or mid-removal.
	if why := reclaimLiveSessionRefusal(r); why != "" {
		return reclaimSkip, why, reclaimStageRow
	}
	// Guard ⓪′ THE GUEST REFUSAL — the ACTOR's half of the same question, asked
	// here because this is where the row's repo is in hand (see
	// guest_row_guard.go for the whole criterion and the measured defect).
	// It sits beside the live-session veto and BEFORE every probe that touches
	// the directory or the repository, so a guest's refusal is the same
	// sentence whether the tree is present, gone, or mid-removal — and so a
	// guest never even runs a repo-wide `git worktree prune` against a repo it
	// does not own. Checked per row, because one pass may span several repos
	// and a guest keeps its mandate over every repo it does not stand in.
	//
	// ASKED IN THE CLOSE-OUT FORM (reclaim_own_ground.go): when the row being
	// judged is one THIS pass is closing out — its ground is in the marks — the
	// guest shape is the manual-run contract rather than the hazard, because the
	// producer's passes run BEFORE the cwd release (cmd/agent
	// taskReclaimOwnDirectories, workerRoundOwnWorktrees) and the run's cwd is
	// therefore still the ground while its own row is being released. Measured
	// on master f4451b9d, this rung's refusal is the one the epic's own
	// reproduction prints ("a guest does not release … another row's worktree");
	// a FOREIGN row is never in a pass's marks, so it still reads as a guest's
	// attempt and is still refused.
	if !SelfGroundReleasePendingFor(r.Worktree) {
		if why := guestRowReleaseRefusal(r.Repo); why != "" {
			return reclaimSkip, why, reclaimStageRow
		}
	}
	// Guard ⓪″ THE LIVE-STATUS ROW'S GROUND IS NOT THIS ARM'S TO FREE (2026-10-12
	// incident: rows 01a130f0 / 01a130ee / 01a130e4, whose ground this arm freed
	// at 16:28–16:39 while the ledger still read in_progress).
	//
	// A row whose status is live has NOT been completed — no run of it ever
	// wrote a completion claim, which is exactly what the status transition
	// records. All this arm proved is that the holder DIED (the pid layer).
	// "The holder is gone" and "the row is finished" are different claims, and
	// this arm may only act on the first.
	//
	// What the old behaviour cost: the directory was freed, the row stayed
	// non-terminal, and every spelling of `gopi worker --resume` needs the
	// ground (the default positional target resolves the row's recorded
	// worktree; --branch and --worktree name it outright) — so the row was
	// stranded: not finished, not resumable, not dispatchable. That is a worse
	// outcome than a parked directory, and it is also the OPPOSITE of what the
	// sibling arm (ResumeDeadHolderRuns) exists for: it CONTINUES the very run
	// this arm was destroying the ground of, and it refuses to act once the
	// directory is gone.
	//
	// So: refuse, NAME the row and its status, and leave the directory (and its
	// registration, so `git worktree list` and safe-merge gate [c] keep naming
	// it) exactly where they are. The row's end belongs to the ledger's own
	// close-out — the run's own resume, the wrapper's writeback, or the zombie
	// sweep — never to an arm that holds no evidence about completion.
	//
	// The terminal half of this arm is untouched: once the row IS terminal the
	// run is over, the ground is dead weight, and the directory is freed as
	// before (the whole point of this file, and the withholding it exists to
	// end — measured: merged=0, protected=7 deliverable commits never landed).
	// NOTHING HERE. The live-status disposition is decided by the CO-DEATH step
	// in ReclaimDeadHolderWorktrees (the removal path), because the honest remedy
	// is not a refusal and not a bare release: a row whose holder died mid-run
	// must LEAVE the live terminal state in the same act that frees its ground,
	// or it is stranded. See reclaimDeadHolderCoDeath.
	// A row whose REPO is gone cannot be reconciled at all: `git worktree
	// prune` (the repair) would fail with `chdir <repo>: no such file or
	// directory` and the removal could not run either. Refusing says so in one
	// honest line instead of a failed git call per pass.
	if _, err := os.Stat(r.Repo); err != nil {
		return reclaimSkip, reclaimUnreachableRepoGoneReason(r.Repo), reclaimStageGround
	}
	if _, err := os.Stat(r.Worktree); err != nil {
		// The directory is gone — but "gone" alone does NOT mean this pass has
		// a repair to make: git may have dropped the registration already (the
		// normal end state of a fixed ledger), and `git worktree prune` is
		// REPO-WIDE, so running it per row would spawn hundreds of subprocesses
		// per pass for nothing. Ask git first: only a row whose exact path the
		// repo's worktree list STILL names is due the repair; the rest are
		// silent (their registration is already clean — there is nothing left
		// to converge).
		if !prunes.listed(r.Repo, r.Worktree) {
			return reclaimSkip, "", reclaimStageGround
		}
		return reclaimVanishedProven, "directory already gone", reclaimStageGround
	}
	// Ground probe, before any git call that runs INSIDE the directory: `git
	// status` in a path git cannot resolve exits 128 ("not a git repository"),
	// which reads as "status unreadable" and hides the actual state. A path
	// this repo has no worktree registration for is not this row's directory
	// at all (a re-created path, a moved repo, another checkout's ground) —
	// refusing is the honest answer, and the branch stays NAMED. The probe is
	// identical to the one below for the OTHER repo: it asks the directory's
	// own git whether it is a worktree of some repo, which both names the
	// refusal precisely and stays a refusal either way (a false positive can
	// only change the wording, never whether a removal happens).
	registered := reclaimWorktreeRegisteredIn(r.Repo, r.Worktree)
	if !registered {
		// WHICH SHAPE IS THE DIRECTORY? (epic 01a17b2f, measured 2026-10-27.)
		// The second probe below asks the DIRECTORY's own repo whether it lists
		// itself — and every repository lists its OWN MAIN CHECKOUT, so a CLONE
		// ground (`.git` is a DIRECTORY: it IS a repository) answered "yes" and
		// was then reported as "a linked worktree of ANOTHER repo". 113 of 114
		// `reclaim-unreachable` markers on this host carried that sentence about
		// clone grounds. So the SHAPE is measured BEFORE the foreign judgement,
		// with this repository's ONE discriminator (devworkspace.GroundModelOf,
		// internal/devworkspace/ground.go) — never a second implementation of
		// "is this a clone?"
		//
		// CLONE ⇒ this row's OWN ground, just not a registration of r.Repo: its
		// own stage and its own wording, and the clone collection leg
		// (runGroundCollect) is the ONE writer that converges it. NOT foreign —
		// Rule 42 keeps the directory where it is either way; only the sentence
		// and the classification change.
		//
		// Only a LINKED worktree (`.git` is a FILE naming a gitdir) can be
		// another repo's, and it can only be judged foreign when that other
		// repo REALLY answers for it — which is what the second probe measures.
		if devworkspace.GroundModelOf(r.Worktree) == devworkspace.GroundModelClone {
			return reclaimSkip, reclaimUnreachableCloneGroundReason(r.Repo), reclaimStageClone
		}
		if reclaimWorktreeRegisteredIn(r.Worktree, r.Worktree) {
			return reclaimSkip, reclaimUnreachableForeignGroundReason(r.Repo), reclaimStageForeign
		}
		return reclaimSkip, reclaimUnreachableUnregisteredReason(r.Repo), reclaimStageGround
	}
	// Guard ②: a dirty worktree holds uncommitted work — a human's call, not
	// a sweep's. The REFUSAL IS UNCHANGED (nothing is removed either way); the
	// REASON now separates the two states porcelain conflates, so an operator
	// can see which refusals are a run's own untracked residue (see
	// reclaimDirtyReason).
	out, err := reclaimGitStatus(r.Worktree)
	if err != nil {
		return reclaimSkip, "worktree status unreadable: " + err.Error(), reclaimStageLive
	}
	if why := reclaimDirtyReason(out); why != "" {
		return reclaimSkip, why, reclaimStageLive
	}
	// Guard ③: the cwd liveness scan shared with the finish-cleanup arms —
	// one implementation of "a live process works inside this directory"
	// (TaskGroundInUse, task_ground_in_use.go: the criterion `gopi task status`
	// and the resume gate read too, so the STATUS view and this arm can never
	// disagree about whether a ground is occupied). The orphan reading is
	// INSIDE that criterion (Gap A), so a directory whose occupancy is entirely
	// orphaned cwd-inheritors is not held here either.
	if _, reason := TaskGroundInUse(r.Worktree); reason != "" {
		return reclaimSkip, reason, reclaimStageLive
	}
	// Guard ①: the branch checked out in the directory must be the one the
	// dead holder's row names. A directory that re-attached a DIFFERENT
	// branch (`git worktree add <path> <other>`) is not this row's to free.
	// (It is the last check because it needs the directory to be readable —
	// a path git cannot answer for is already refused above.)
	checked := finishCleanupWorktreeBranch(context.Background(), r.Worktree)
	if checked != "" && checked != r.Branch {
		return reclaimSkip, "worktree checks out " + checked + ", not " + r.Branch + " — refusing to free a directory the row does not own", reclaimStageLive
	}
	return reclaimFreable, "", reclaimStageOther
}

// reclaimDisposition is the verdict+reason shape the rest of the file reads; the
// stage is the classification half (see reclaimStage) and only the removal arm
// consults it, so it is asked where it is used and not carried across the tree.
func reclaimDisposition(r reclaimDeadHolderRow, prunes *reclaimPrunePlan) (reclaimVerdict, string) {
	v, why, _ := reclaimDispositionJudged(r, prunes)
	return v, why
}

// reclaimTerminalRowUnreachable answers the question this whole scan got wrong:
// is this candidate row's refusal something to DISCLOSE, or simply the ordinary
// state of a row whose life is over?
//
// THE DEFECT (epic: dead-holder 回收扫描的候选集合). r.LiveRow is FALSE for exactly
// the rows whose status is terminal (pending/claimed/in_progress set it TRUE —
// see reclaimDeadLocalHolderRowsFrom / finishCleanupDeadLocalHolder), so a
// COMPLETED row whose recorded directory is gone — the NORMAL end state, every
// finished run leaves one, and a re-homed row leaves one too — reached
// reclaimRefuse and was printed as
//
//	dead-holder reclaim <branch> (<id>) skipped: repo directory /x is gone — this
//	row cannot be reconciled here
//
// every tick, forever: the ledger is append-only, nothing deletes the row, and
// the arm's own closing contract is "the row is not this pass's to remove". A
// finished row is not a defect to reconcile, so a line about it is pure noise —
// and worse than noise: it puts a sentence the operator cannot act on beside the
// refusals that a human MUST read.
//
// The judge is the STAGE, not the wording, and it has two halves:
//
//	A GROUND THAT IS ANOTHER REPO'S WORKTREE is silent for ANY status
//	(reclaimStageForeign). Rule 42 puts it permanently out of this arm's reach
//	and no later pass can change that, so the sentence would be a fossil with
//	nothing to converge and nothing to disclose — whether the row is finished or
//	still open. The ledger's answer to "why is this branch still held?" is
//	ReclaimHeldBranches, which is untouched.
//
//	A row whose OWN GROUND IS GONE OR UNREGISTERED is silent only when its life
//	is over (reclaimStageGround + !LiveRow): that is a finished run's ordinary
//	end state. It is never silenced for a LIVE-status row — the orphan arm's
//	ground vanishing is the one shape that strands a row (not finished, not
//	resumable, not re-dispatchable), so Rule 15 makes it a report.
//
//	A CLONE GROUND gets the same half (reclaimStageClone + !LiveRow), NOT the
//	foreign silence: it is this row's OWN repository (measured by
//	devworkspace.GroundModelOf), so a finished run's clone is the ordinary end
//	state the clone collection leg collects — while a run that DIED and left its
//	clone behind is an anomaly an operator must read. Treating the two shapes
//	alike is the measured defect (epic 01a17b2f: 113 of 114 markers were the
//	FOREIGN wording about clone grounds).
//
// Neither is ever silent about the ROW's own identity (a live session standing
// on a terminal row — claim_holder_freshness's measured shape, where a terminal
// status is NOT proof the run ended) nor about a ground that EXISTS and is
// merely busy (uncommitted work, a live process inside, a foreign branch
// checked out): those are what a human is being asked to read.
//
// Nothing is deleted and no row is rewritten (Rule 42): the row keeps its
// branch, its lock-free status and its recorded directory; only the LOG line
// stops. The operator-facing answer to "why is this branch still protected?"
// lives in ReclaimHeldBranches, which is untouched and still names every held
// branch — this function removes a per-tick NOISE line, never a fact.
func reclaimTerminalRowUnreachable(r reclaimDeadHolderRow, stage reclaimStage) bool {
	if stage == reclaimStageForeign {
		return true // Rule 42: another repo's worktree is out of reach for every status
	}
	if stage == reclaimStageClone {
		// The clone ground is THIS row's repository, so its silence is the
		// finished-run half only — a live row that left a clone behind is
		// exactly the disclosure the false wording used to suppress.
		return !r.LiveRow
	}
	return !r.LiveRow && stage == reclaimStageGround
}

// reclaimPlanWorktreePrefix is the `git worktree list` line prefix this file
// keys on when it needs the path-only shape of the list (the porcelain form —
// derived from the verb, never re-typed).
const reclaimPlanWorktreePrefix = finishCleanupPorcelainWorktreePrefix

// reclaimPrunePlan is one pass's registration-repair plan, spanning every row
// of the pass. `git worktree prune` is REPO-WIDE — it takes no path, so it
// cannot be aimed at one row — and with several vanished rows in the SAME repo
// a per-row prune both mis-attributes the repair (the first row's call drops
// the SECOND row's registration) and spawns one subprocess per row. The plan
// therefore:
//
//	reads the repo's worktree list ONCE (memoized per repo),
//	runs at most ONE prune per repo, and
//	credits a repair only to a row whose path the list NAMED before the prune
//	and no longer names after it — git's own before/after statement instead of
//	a hopeful assumption.
type reclaimPrunePlan struct {
	// listers memoizes `git worktree list` per repo, so N rows in one repo cost
	// ONE subprocess, not N.
	listers map[string]func() []string
	// listedPaths is the BEFORE image per repo (the registration ground truth
	// the disposition consults).
	listedPaths map[string][]string
	// attempts memoizes the prune per repo: one call, and a failed call is not
	// retried for every later row of the same repo.
	attempts map[string]reclaimPruneAttempt
}

// reclaimPruneAttempt is one repo's prune outcome (the empty error means git
// ran and its output is in output).
type reclaimPruneAttempt struct {
	output []string
	err    error
}

// newReclaimPrunePlan makes an empty plan (one per pass).
func newReclaimPrunePlan() *reclaimPrunePlan {
	return &reclaimPrunePlan{
		listers:     map[string]func() []string{},
		listedPaths: map[string][]string{},
		attempts:    map[string]reclaimPruneAttempt{},
	}
}

// listed reports whether repo's CURRENT worktree list still names exactly this
// worktree path — the precondition for a repair (a row whose registration is
// already clean has nothing to repair and must stay silent).
func (p *reclaimPrunePlan) listed(repo, worktree string) bool {
	for _, path := range p.paths(repo) {
		if path == worktree {
			return true
		}
	}
	return false
}

// paths returns repo's worktree paths (memoized). An unreadable list yields no
// paths — the conservative direction for a REPAIR (nothing is pruned on the
// word of a list git would not give), and the disclosure direction for the
// caller, which simply keeps the row a candidate.
func (p *reclaimPrunePlan) paths(repo string) []string {
	if cached, ok := p.listedPaths[repo]; ok {
		return cached
	}
	lister, ok := p.listers[repo]
	if !ok {
		lister = reclaimWorktreePathLister(repo)
		p.listers[repo] = lister
	}
	p.listedPaths[repo] = lister()
	return p.listedPaths[repo]
}

// repair runs the plan for one pass: prune each repo with a proven-stale row
// at most once, then report ONE line per row whose registration really went.
//
// With remove=false — or with the kill switch off — the prune is NOT run at
// all: "report only" must be true for the whole arm, and `git worktree prune`
// writes to the repo's administrative area (it is the ordinary `git worktree
// remove --force` body's prune step), so a report-only pass that pruned would
// be a documented contract broken silently. The rows are NAMED instead, so the
// operator still sees which registrations a real pass would repair.
func (p *reclaimPrunePlan) repair(rows []reclaimDeadHolderRow, remove bool) []string {
	var out []string
	for _, r := range rows {
		if !remove || !reclaimRemovalEnabled() {
			out = append(out, reclaimCandidateMarker+": "+r.Branch+" ("+r.TaskID+", "+r.ClaimedBy+
				") — directory already gone; stale registration left untouched (report only)")
			continue
		}
		attempt := p.prune(r.Repo)
		if attempt.err != nil {
			taskNotifyLog("dead-holder reclaim %s (%s) registration not repaired: %v", r.Branch, r.TaskID, attempt.err)
			out = append(out, reclaimCandidateMarker+": "+r.Branch+" ("+r.TaskID+", "+r.ClaimedBy+")")
			continue
		}
		if !p.listed(r.Repo, r.Worktree) {
			// Git dropped exactly this row's registration — the repair this row
			// was due. The AFTER image is what makes the credit honest: with
			// several rows in one repo only the ones git actually dropped are
			// reported, never the first row of the group.
			out = append(out, reclaimRepairedMarker+": "+r.Branch+" ("+r.TaskID+") — directory already gone; stale registration pruned; branch kept")
			continue
		}
		// Git kept the registration (a locked or unreadable administrative
		// entry): there was no repair here, so the row is NAMED as still
		// withheld rather than credited with a repair that did not happen
		// (Rule 15 — the branch must never look free while gate [c] still
		// refuses it).
		taskNotifyLog("dead-holder reclaim %s (%s) %s: %s", r.Branch, r.TaskID, reclaimSurvivedPruneNote, r.Worktree)
		out = append(out, reclaimCandidateMarker+": "+r.Branch+" ("+r.TaskID+", "+r.ClaimedBy+")")
	}
	return out
}

// prune runs the repo-wide registration repair through the counted seam.
func (p *reclaimPrunePlan) prune(repo string) reclaimPruneAttempt {
	if _, ok := p.attempts[repo]; ok {
		return p.attempts[repo]
	}
	output, err := reclaimPruneCall(repo)
	var lines []string
	if strings.TrimSpace(output) != "" {
		lines = strings.Split(strings.TrimSpace(output), "\n")
	}
	attempt := reclaimPruneAttempt{output: lines, err: err}
	p.attempts[repo] = attempt
	// The list cache holds the BEFORE image — the whole point of the AFTER
	// check is that it must be re-read, not remembered.
	delete(p.listedPaths, repo)
	return attempt
}

// reclaimWorktreePathLister returns a memoizable reader of repo's registered
// worktree paths (`git worktree list --porcelain`). It returns nil paths when
// git cannot answer.
func reclaimWorktreePathLister(repo string) func() []string {
	return func() []string {
		ctx, cancel := context.WithTimeout(context.Background(), reclaimGitTimeout)
		defer cancel()
		out, err := finishCleanupGitParsedStdout(ctx, repo, reclaimGitTimeout, finishCleanupWorktreeVerb, finishCleanupWorktreeListVerb, finishCleanupWorktreeListFlag)
		if err != nil {
			return nil
		}
		return reclaimPlanParsePaths(out)
	}
}

// reclaimPlanParsePaths extracts the worktree paths from porcelain list output.
func reclaimPlanParsePaths(out string) []string {
	var paths []string
	for _, line := range strings.Split(out, "\n") {
		if path, ok := strings.CutPrefix(strings.TrimSpace(line), reclaimPlanWorktreePrefix); ok {
			paths = append(paths, strings.TrimSpace(path))
		}
	}
	return paths
}

// FinishCleanupDeadLocalHolderCount reports how many rows the orphan census
// finds: live-STATUS rows (pending/claimed/in_progress) whose claim stamp
// resolves on THIS host to a provably dead pid. It is the operator-facing
// number the sweep logs (orphan epic, 2026-09-12) — the state used to be
// invisible as well as un-freeable — and the same predicate the reclaim arm
// and the cleanup chain's holder verdict use (finishCleanupDeadLocalHolder),
// so the count can never disagree with what the pass actually frees.
//
// A row without a worktree is counted too: the census is about PEOPLE-shaped
// ledger debt (a row nothing will ever close out), not about directories.
func FinishCleanupDeadLocalHolderCount() (int, error) {
	ctx, cancel := context.WithTimeout(context.Background(), finishCleanupDBTimeout)
	defer cancel()
	db, err := openTasksDB(ctx, memoryDBPath())
	if err != nil {
		return 0, err
	}
	defer db.Close()
	rows, err := db.QueryContext(ctx,
		`SELECT status, COALESCE(claimed_by,''), COALESCE(`+tasksProcPidColumnName+`,0),
		        COALESCE(`+tasksProcStartColumnName+`,0), COALESCE(`+tasksProcCmdColumnName+`,'') FROM tasks WHERE status IN (`+taskLiveStatusSQLList()+`)`)
	if err != nil {
		return 0, err
	}
	defer rows.Close()
	n := 0
	for rows.Next() {
		var status, claimedBy string
		var procPid, procStart int64
		var procCmd string
		if err := rows.Scan(&status, &claimedBy, &procPid, &procStart, &procCmd); err != nil {
			return 0, err
		}
		if finishCleanupDeadLocalHolder(status, claimedBy,
			procIdentity{Pid: int(procPid), StartTicks: procStart, Cmd: procCmd}) {
			n++
		}
	}
	return n, rows.Err()
}

// reclaimWorktreeRegisteredIn reports whether dir's git lists worktree as ONE
// of its linked worktrees — whole-path equality, because a prefix match would
// accept a sibling directory whose name merely starts the same.
func reclaimWorktreeRegisteredIn(dir, worktree string) bool {
	ctx, cancel := context.WithTimeout(context.Background(), reclaimGitTimeout)
	defer cancel()
	out, err := finishCleanupGitParsedStdout(ctx, dir, reclaimGitTimeout, finishCleanupWorktreeVerb, finishCleanupWorktreeListVerb, finishCleanupWorktreeListFlag)
	if err != nil {
		return false
	}
	for _, line := range strings.Split(out, "\n") {
		path, ok := strings.CutPrefix(strings.TrimSpace(line), finishCleanupPorcelainWorktreePrefix)
		if ok && strings.TrimSpace(path) == worktree {
			return true
		}
	}
	return false
}

// reclaimRepairRegistration drops the stale worktree registration git still
// holds for a directory that is gone (`git worktree prune -v`, idempotent),
// returning prune's own statement of what it dropped ("" = there was nothing
// stale — the caller reports only real repairs). This is a REPAIR with an
// operator-visible consequence, not bookkeeping: safe-merge gate [c] greps
// `git worktree list` for the branch, and a gone-but-registered worktree keeps
// appearing there until it is pruned — so a vanished directory used to withhold
// its own branch from merging forever, the exact withholding this whole file
// exists to end.
func reclaimRepairRegistration(repo string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), reclaimGitTimeout)
	defer cancel()
	out, err := finishCleanupGitRaw(ctx, repo, finishCleanupWorktreeVerb, finishCleanupWorktreePruneVerb, finishCleanupWorktreePruneFlag)
	if err != nil {
		return "", fmt.Errorf("worktree prune in %s: %w", repo, err)
	}
	return out, nil
}

// reclaimDeadHolderRows scans the ledger for rows whose branch a provably
// DEAD holder still occupies: a TERMINAL row (finished/cancelled — its life
// is over), carrying a worktree, whose claim stamp resolves on THIS host and
// whose pid is dead. Everything else is deliberately out of reach:
//
//	live status        — the row may still be working (never touched);
//	no worktree        — nothing parked;
//	foreign/blank host — unknowable here, so never called dead;
//	pid alive          — including a zombie caught by pidAlive's Z check.
//
// The one exception (orphan epic, 2026-09-12) is reclaimDeadLocalHolderRows:
// a live-STATUS row whose LOCAL pid is provably dead is stranded by the very
// same evidence, and neither scan reached it before. That arm is scanned
// SEPARATELY, and the two sets are merged here, because the diff — status is
// live — is also what the pass must REPORT (a human may still have to close
// the row out). Both scans share the row classifier below, so they cannot
// drift on what "provably dead local holder" means (Rule 14).
func reclaimDeadHolderRows() ([]reclaimDeadHolderRow, error) {
	ctx, cancel := context.WithTimeout(context.Background(), finishCleanupDBTimeout)
	defer cancel()
	db, err := openTasksDB(ctx, memoryDBPath())
	if err != nil {
		return nil, err
	}
	defer db.Close()
	return reclaimDeadHolderRowsFrom(ctx, db)
}

// reclaimDeadHolderCandidate is the row-level classifier BOTH scans feed: the
// claimed holder must resolve on THIS host and be provably DEAD (claimHolder's
// ok=false covers blank / malformed / foreign stamps — "unknown" is never
// "dead"; pidAlive's Z check makes an unreaped wrapper dead) AND no process the
// ledger knows as the row's RUNNER may still be alive.
//
// The runner half is the 2026-09-14 fix (task 01a09d67) and it is not
// redundant with the claim half: "the claim pid is gone" repeatedly turned out
// to be a statement about the SWEEPER's relation to the process (pid namespace,
// host, churn), not about the holder, because the only liveness channel that
// could see a foreign session's process — /proc/<pid>/cwd — is unreadable across
// sessions under ptrace_scope=1 (measured 185 of ~360 entries). This arm had NO
// live-row veto at all, so a directory whose runner was demonstrably alive
// (recorded identity matching a live process) was freed. Returns (row, true)
// only when the row is provably unheld.
func reclaimDeadHolderCandidate(id, status, claimedBy, deliverable string, rec procIdentity) (reclaimDeadHolderRow, bool) {
	// pidGone, not `!pidAlive`: this arm FREES a worktree, so only a MEASURED
	// death may open it. An EPERM answer says the holder exists under a user we
	// may not signal — reading that as "gone" frees a directory a live process
	// is standing in (epic 01a17e87; the same class as the 2026-09-16 incident
	// this ladder was built for).
	if _, pid, known := claimHolder(claimedBy); !known || !pidGone(pid) {
		return reclaimDeadHolderRow{}, false
	}
	if finishCleanupProcessHolderLive(rec) {
		// The runner still runs: even the orphan release (a live-STATUS row
		// whose claim pid is gone) must not fire, whatever the status says.
		return reclaimDeadHolderRow{}, false
	}
	wt := taskDeliverableWorktree(deliverable)
	if wt == "" {
		return reclaimDeadHolderRow{}, false
	}
	return reclaimDeadHolderRow{
		TaskID:    id,
		Status:    status,
		ClaimedBy: claimedBy,
		Repo:      finishCleanupParseDeliverable(deliverable).Repo,
		Branch:    taskDeliverableBranchOrEmpty(deliverable),
		Worktree:  wt,
		LiveRow:   finishCleanupDeadLocalHolder(status, claimedBy, rec),
		Ident:     rec,
	}, true
}

// reclaimDeadHolderRowsFrom is the query body over an injected handle (the
// sweep and the tests share ONE reader).
func reclaimDeadHolderRowsFrom(ctx context.Context, db *sql.DB) ([]reclaimDeadHolderRow, error) {
	rows, err := db.QueryContext(ctx,
		// NO LIMIT (incident 2026-09-14, task 01a09d67): an age-ASC LIMIT of
		// reclaimScanMax dropped the NEWEST rows on a 286-row ledger — the live
		// ones. Completeness is the whole point of this scan; the runaway bound
		// is enforced once, after the read (reclaimDeadHolderRowScanMax).
		`SELECT task_id, status, COALESCE(claimed_by,''), COALESCE(deliverable,''),
		        COALESCE(`+tasksProcPidColumnName+`,0), COALESCE(`+tasksProcStartColumnName+`,0), COALESCE(`+tasksProcCmdColumnName+`,'') FROM tasks
		 WHERE deliverable LIKE ?
		   AND status IN (?, ?, ?, ?, ?)`,
		"%"+finishCleanupWorktreeVerb+"%",
		taskStatusCompleted, taskStatusFailed, taskStatusCancelled, taskStatusVerified,
		taskStatusSuperseded)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []reclaimDeadHolderRow
	for rows.Next() {
		var id, status, claimedBy, deliverable string
		var procPid, procStart int64
		var procCmd string
		if err := rows.Scan(&id, &status, &claimedBy, &deliverable, &procPid, &procStart, &procCmd); err != nil {
			return nil, err
		}
		if !taskCleanupEligibleStatus(status) {
			continue
		}
		r, ok := reclaimDeadHolderCandidate(id, status, claimedBy, deliverable,
			procIdentity{Pid: int(procPid), StartTicks: procStart, Cmd: procCmd})
		if !ok {
			continue
		}
		out = append(out, r)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	live, err := reclaimDeadLocalHolderRowsFrom(ctx, db)
	if err != nil {
		return nil, err
	}
	return append(out, live...), nil
}

// reclaimDeadLocalHolderRowsFrom scans the OTHER half of the same predicate
// (orphan epic, 2026-09-12): rows whose STATUS is still LIVE (pending /
// claimed / in_progress) while this host proves their holder is DEAD.
//
// Why a second scan: the arm above is keyed on TERMINAL rows, and
// taskLiveHolderStatus makes a live-status row a holder BY DESIGN — so an
// orphaned in_progress row (a killed wrapper's row, which nothing ever
// closed out) was protected by BOTH paths and its directory stayed parked,
// gate [c] refusing its branch, until a human cleared it BY HAND.
// The evidence for freeing it is identical to the terminal arm's (a pid this
// host can see is gone); only the ROW's status differs, and that is what the
// pass reports rather than hides.
//
// Foreign hosts never qualify: claimHolder's ok=false for a stamp another
// host wrote, so an unknowable holder keeps its veto (conservative direction
// intact). The evidence is the pid layer ALONE — the stale-heartbeat chain
// is deliberately not consulted: a stale stamp proves neither life nor death
// (claimStampFreshness), and a directory must never be freed on "probably".
func reclaimDeadLocalHolderRowsFrom(ctx context.Context, db *sql.DB) ([]reclaimDeadHolderRow, error) {
	rows, err := db.QueryContext(ctx,
		`SELECT task_id, status, COALESCE(claimed_by,''), COALESCE(deliverable,''),
		        COALESCE(`+tasksProcPidColumnName+`,0), COALESCE(`+tasksProcStartColumnName+`,0), COALESCE(`+tasksProcCmdColumnName+`,'') FROM tasks
		 WHERE deliverable LIKE ?
		   AND status IN (`+taskLiveStatusSQLList()+`)`,
		"%"+finishCleanupWorktreeVerb+"%")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []reclaimDeadHolderRow
	for rows.Next() {
		var id, status, claimedBy, deliverable string
		var procPid, procStart int64
		var procCmd string
		if err := rows.Scan(&id, &status, &claimedBy, &deliverable, &procPid, &procStart, &procCmd); err != nil {
			return nil, err
		}
		// finishCleanupDeadLocalHolder is the ONE definition of this shape
		// (live status + locally-dead pid) — the reclaim arm and the cleanup
		// chain's holder verdict therefore cannot disagree about a row.
		rec := procIdentity{Pid: int(procPid), StartTicks: procStart, Cmd: procCmd}
		if !finishCleanupDeadLocalHolder(status, claimedBy, rec) {
			continue
		}
		r, ok := reclaimDeadHolderCandidate(id, status, claimedBy, deliverable, rec)
		if !ok {
			continue
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// DeadHolderBranches returns the set of branches a provably-dead holder still
// occupies in repo — the discovery half the merger's protected arm consumes
// (repo_sweep.go), so "protected" stops being an opaque count. A branch is
// reported only when the ledger attributes it to a dead holder AND git agrees
// the branch is checked out in a linked worktree of repo.
func DeadHolderBranches(repo string) map[string]bool {
	out := map[string]bool{}
	rows, err := reclaimDeadHolderRows()
	if err != nil || len(rows) == 0 {
		return out // no ledger / no rows: nothing to attribute (never a guess)
	}
	wtByBranch, _, err := mergedCleanupWorktrees(repo)
	if err != nil {
		return out
	}
	for _, r := range rows {
		if r.Branch == "" {
			continue
		}
		// OCCUPANCY is what the protected arm reports, and git is its source
		// (ask git first): a ledger row naming a directory that is GONE or no
		// longer a linked worktree of repo does not occupy the branch at all,
		// so naming it "protected" would both over-count the arm and claim a
		// held checkout that does not exist.
		if len(wtByBranch) == 0 {
			// No worktree list to confirm against (repo unreadable): the
			// recorded directory IS the only occupancy evidence — keep the
			// ledger's word rather than inventing a clean bill of health.
			if _, err := os.Stat(r.Worktree); err == nil {
				out[r.Branch] = true
			}
			continue
		}
		if path, ok := wtByBranch[r.Branch]; ok && path == r.Worktree {
			// A registration is not an occupancy: git keeps listing a
			// worktree whose directory is gone (until it is pruned), while
			// the claim "this branch is held by a checkout" needs the
			// checkout to exist on disk.
			if _, err := os.Stat(r.Worktree); err == nil {
				out[r.Branch] = true
			}
		}
	}
	return out
}

// reclaimHolderVeto is WHY a provably dead holder's branch is STILL withheld
// from the merger: the reclaim arm's guards refused to free the directory, so
// the branch remains worktree-protected and gate [c] keeps refusing it.
//
// It is empty whenever the row does not veto the branch — either the arm WILL
// free the directory on its next pass (nothing to explain) or the refusal
// carries no reason (the "registration already clean" case: nothing is left to
// converge). Only a NAMED refusal explains a branch that is still held.
func reclaimHolderVeto(r reclaimDeadHolderRow, prunes *reclaimPrunePlan) string {
	disp, why := reclaimDisposition(r, prunes)
	if disp != reclaimSkip {
		return ""
	}
	return why
}

// reclaimHeldVetoNoteDirty / reclaimHeldVetoNoteRegistrationOnly name the two
// states a dead holder's branch can be stuck in once the reclaim arm has had
// its say (Rule 1b: one spelling, rendered by both merger front doors).
//
// The DIRTY one is the live-measured majority (6 of 7 branches on the 2026-09-13
// host): the arm refuses an uncommitted directory BY DESIGN — the work is
// recoverable and only a human may judge it — so the branch stays protected and
// gate [c] keeps refusing it. An operator reading "dead holder … reclaimable"
// would conclude the merger was about to free it; it is not, and saying so is
// the whole point of this note.
const (
	reclaimHeldVetoNoteDirty            = "its worktree is DIRTY — the reclaim leaves uncommitted work for a human, so gate [c] still refuses the branch"
	reclaimHeldVetoNoteRegistrationOnly = "its directory is gone and only the git registration is left — the registration repair frees the branch, not the worktree"
)

// ReclaimHeldBranches returns, per branch, WHY a provably dead holder's
// branch is still worktree-protected in repo — i.e. the branches the reclaim
// arm will NOT free, with the reason, so the merger can stop advertising every
// dead holder as "reclaimable".
//
// It is the honest-verdict half of DeadHolderBranches: that function answers
// "the holder is dead" (evidence: the ledger's claim pid is gone + git agrees
// on the occupancy), which is NOT the same claim as "the branch will be freed".
// Freeing additionally requires the reclaim arm's guards — a clean directory
// (guard ②) above all. Measured on the live host 2026-09-13: the merger named
// 7 dead-holder branches "reclaimable by the finish-cleanup sweep" for over an
// hour while 6 of them were DIRTY and therefore un-reclaimable by design, and
// the 7th needed an arm no resident process ever ran.
//
// A branch is reported only when git confirms the row's own directory occupies
// it — the same occupancy rule DeadHolderBranches applies, so the two
// annotations can never disagree about which branch is held (Rule 13).
func ReclaimHeldBranches(repo string) map[string]string {
	out := map[string]string{}
	rows, err := reclaimDeadHolderRows()
	if err != nil || len(rows) == 0 {
		return out // no ledger / no rows: nothing to attribute (never a guess)
	}
	wtByBranch, _, err := mergedCleanupWorktrees(repo)
	if err != nil {
		return out // occupancy unreadable: never guess a veto
	}
	prunes := newReclaimPrunePlan()
	for _, r := range rows {
		if r.Branch == "" {
			continue
		}
		path, ok := wtByBranch[r.Branch]
		if !ok || path != r.Worktree {
			continue // git does not say this row's directory holds the branch
		}
		if _, err := os.Stat(r.Worktree); err != nil {
			// The directory is gone; only the registration (what gate [c]
			// greps) is left. The arm's registration repair is what clears it —
			// and until that runs the branch IS still refused, which is exactly
			// the state worth naming.
			out[r.Branch] = reclaimHeldVetoNoteRegistrationOnly
			continue
		}
		if why := reclaimHolderVeto(r, prunes); why != "" {
			out[r.Branch] = why
		}
	}
	return out
}

// reclaimGitStatus runs `git status --porcelain` in dir (guard ②) and returns
// the PARSED porcelain body — STDOUT ONLY.
//
// The distinction is load-bearing, not stylistic: this body is classified by
// reclaimDirtyVerdict, so a merged stream would let any stderr text (a
// `GIT_TRACE=1` trace line, a `warning: |` line) read as DIRT and refuse a clean
// worktree (measured 2026-10-26, epic git-parsed-stdout-only). stderr is
// diagnostics and survives only inside the returned error.
func reclaimGitStatus(dir string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), reclaimGitTimeout)
	defer cancel()
	return finishCleanupGitParsedStdout(ctx, dir, reclaimGitTimeout,
		finishCleanupStatusVerb, finishCleanupStatusPorcelainFlag)
}

// reclaimDirtyVerdict is the ONE dirty-state classifier for every worktree
// REMOVAL gate in this repo (Rule 14: one definition, referenced by all three
// gates of the finish-cleanup chain and by the reclaim sweep — the three
// copies of `strings.TrimSpace(dirty) != ""` were the drift surface).
//
// It answers "may this dirt be removed?" with THREE outcomes:
//
//	""                              clean (or the git no-output sentinel) — nothing refuses
//	reclaimUntrackedResidueMarker   ONLY untracked entries — residue, not work
//	reclaimSkipDirtyReason          at least one TRACKED change — uncommitted work,
//	                                a human's call, and the reason names the files
//
// The SPLIT is the fix, not a weakening (the precedent is the Rule-12 rebuild
// guard, devdoc/rebuild-guard-tracked-only.md: a permanently-untracked repo
// must not freeze the close loop). BEFORE this function, guard ② rendered
// every non-empty porcelain body as reclaimSkipDirtyReason, so a tree whose
// only entries were `??` reported itself as somebody's uncommitted work and an
// operator could not tell the two apart. What does NOT change: a tracked
// change still refuses (reclaimSkipDirtyReason), no gate removes anything it
// did not remove before, and the untracked body is QUOTED back (Rule 15: the
// refusal names the files that caused it, it never becomes a silent delete).
func reclaimDirtyVerdict(porcelain string) string {
	body := strings.TrimSpace(porcelain)
	if body == reclaimStatusClean || strings.Contains(body, reclaimStatusNoOutputSentinel) {
		return reclaimStatusClean
	}
	// The split itself is the PREDICATE (reclaimUntrackedOnly); this function
	// only renders the sentence for whichever side the measurement lands on.
	// One definition of the classification, one definition of each wording —
	// the enforcement arm and the sweep can therefore never disagree about
	// which kind of dirt a tree holds (Rule 14).
	if reclaimUntrackedOnly(body) != "" {
		// Every entry is untracked: a RUN's own residue (build output, a report
		// it wrote, a scratch checkout). No tracked content is at risk — the
		// refusal is the same (the sweep removes nothing), but the reason says
		// WHICH kind of dirt it is, because only an untracked-only tree is the
		// run's own to clean and the operator's next step differs.
		return reclaimUntrackedResidueReason + "\n" + body
	}
	return reclaimSkipDirtyReason + "\n" + body
}

// reclaimDirtyHeld reports whether a worktree's held reason is guard ②'s OWN
// dirty refusal — in EITHER of its two shapes (a tracked change, or an
// untracked-only tree). The salvage lane asks this question, not "which
// wording is it": both shapes mean "this arm refused a directory that may
// hold work", which is exactly the population the salvage exists for. Keeping
// one predicate here means the wording split cannot silently drop the
// untracked half out of the salvage scope (the regression
// TestZZRedProof_DirtyDeadHolderWorkIsOrphaned is red for).
func reclaimDirtyHeld(reason string) bool {
	return strings.Contains(reason, reclaimSkipDirtyReason) ||
		strings.Contains(reason, reclaimUntrackedResidueReason)
}

// reclaimStatusNoOutputSentinel is the git plumbing helpers' no-output
// sentinel — the shape they produce for an empty body. This merge found the
// literal in two production files, which is exactly what
// TestFinishCleanupNoOutputSentinelLiteralGate (Rule 13) forbids: the value is
// finishCleanupNoOutput's, so this name is an ALIAS of the one definition
// rather than a second literal (and the literal is not re-typed here either —
// that gate scans this file's bytes, comments included).
const reclaimStatusNoOutputSentinel = finishCleanupNoOutput

// reclaimDirtyReason is the reclaim's SECOND entry point into the SAME
// classification (Rule 14): this merge found two independently verified
// spellings of guard (2)'s dirty split — ours, reclaimDirtyVerdict (which
// also quotes the offending entries back, Rule 15), and master's
// reclaimDirtyReason — and it keeps BOTH names live over ONE definition
// rather than dropping either side's callers. Master's callers
// (task_reclaim_self.go's ownership-independent producer pass and the
// salvage lane) and ours (reclaimDisposition) therefore agree by
// construction: same clean sentinel, same untracked/tracked split, same
// operator word DIRTY.
func reclaimDirtyReason(porcelain string) string {
	return reclaimDirtyVerdict(porcelain)
}

// reclaimUntrackedOnly answers the ONE question this arm's gate asks — "is
// every porcelain entry an untracked file?" — as a PREDICATE over the raw
// measurement, not as a comparison of two rendered sentences.
//
// The distinction is the whole point of the merge note above. An operator
// line is EVIDENCE (it quotes the offending entries back, Rule 15), so a
// classifier's OUTPUT is the wrong thing to branch on: any future edit to the
// wording — e.g. master and this branch had two different sentences for this
// one state — silently moves a `!=` comparison to "false" and an enforcement
// arm goes quiet without a single test noticing. This predicate reads the
// measurement itself, so it is wording-independent by construction, and
// reclaimDirtyVerdict stays the ONE renderer of the operator line (Rule 14:
// one definition of the classification, one definition of each wording).
func reclaimUntrackedOnly(porcelain string) string {
	body := strings.TrimSpace(porcelain)
	if body == reclaimStatusClean || strings.Contains(body, reclaimStatusNoOutputSentinel) {
		return ""
	}
	for _, line := range strings.Split(body, "\n") {
		if !strings.HasPrefix(strings.TrimSpace(line), reclaimPorcelainUntrackedMarker) {
			return ""
		}
	}
	return body
}

// reclaimRunResidueArmMarker opens the enforcement arm's source region. It is
// a CONSTANT so the arm's Rule-13 gate (run_untracked_residue_test.go) and the
// arm itself cannot drift apart about where the region starts — the marker and
// the region are one fact. (It also gives the gate a scan window that cannot be
// widened by accident with a re-typed literal.)
const reclaimRunResidueArmMarker = "── the ENFORCEMENT half: a run's OWN untracked scratch at close-out ──"

// ── the ENFORCEMENT half: a run's OWN untracked scratch at close-out ─────────
//
// The classifier above NAMES the two dirty states apart. This half makes the
// run answer for the one that is ITS OWN: the operator's standing requirement
// is that a finished run leaves no untracked file behind in its worktree, so a
// `completed` writeback that lands on untracked-only residue in the row's OWN
// recorded worktree is recorded as exactly that — attributed to the row, the
// offending entries quoted back — and stops being reported as somebody's
// uncommitted work (which is what master's single wording did; measured
// 2026-09-16: 8 untracked entries in the main checkout were each rendered
// "worktree is DIRTY (uncommitted work)").
//
// The mechanism is a RECORD for the untracked shape, and — for a run's OWN
// uncommitted TRACKED deliverable — a BINDING REFUSAL of the `completed` claim
// (see "the BINDING half" below; measured case 01a0af50 showed a record the row
// outlives is not enough). Neither half ever touches a byte:
//
//   - CLEANING the residue is not ours to do. An untracked file in a
//     worktree is single-copy data — the classifier cannot tell the run's
//     `build.out` from a human's scratch note, and deleting the second is
//     data loss. The only arms that DELETE anything here are the worktree
//     removal arms, and they stay absent (no `git worktree remove`, no
//     `--force`, never a branch mutation — guard strength unchanged).
//   - REFUSING the `completed` writeback is not ours for the UNTRACKED shape:
//     it is the operator's standing switch, because untracked litter is
//     hygiene rather than a missing deliverable. For the TRACKED shape the
//     refusal is the default — that shape means the run's deliverable is not
//     in a ref, which is a fact about the RUN, not about hygiene.
//
// The arm speaks for BOTH shapes of "the worktree is not clean at close-out",
// because the rule it serves covers both (PROJECT.md Rule 30): untracked litter,
// which the run must delete or relocate, and TRACKED dirt, which is an
// uncommitted DELIVERABLE the run must commit. They are recorded as distinct
// events (reclaimRunResidueEvent / reclaimRunResidueDirtyEvent), because they
// call for opposite remedies — deleting tracked dirt destroys work.
//
// So the refusal is NON-SILENT and ACTIONABLE: one task_events row (the
// append-only audit trail an operator already reads as `result:`) plus one
// stderr note (Rule 15 — a degradation is never swallowed), both carried by
// the row so the next sweep has an attributed handle. The per-ROW reclaim
// report (reclaimUntrackedResidueMarker) remains the other handle: it names
// the same residue with the file list when a sweep walks the worktree.

// reclaimRunResidueUncleanEvent is the SHARED stem of every close-out residue
// event (Rule 14: the family has ONE definition and the shapes are suffixed
// from it) — "unclean" is the vocabulary this ledger already uses for a run
// that left something behind ("the run ended unclean"). Because both shapes
// grow from this stem, the idempotence guard's single `event LIKE '<stem>%'`
// probe still sees the whole family.
const reclaimRunResidueUncleanEvent = taskStatusCompleted + "-unclean"

// reclaimRunResidueEvent is the UNTRACKED-LITTER shape's event — a run's own
// scratch, the operator's greppable handle on the enforcement.
//
// THE FAMILY IS ORDERED, NOT MERELY SUFFIXED (measured while landing
// reclaimMainCheckoutDepositEvent): `reclaimRunResidueFiled` matches the family
// by `event LIKE '<stem>%'`, so EVERY member of the family shares one
// idempotence slot — and the main-checkout deposit arm files its record at the
// state GATE, which is EARLIER in the close-out than this record. A new member
// written earlier therefore tripped this guard and SUPPRESSED the run's own
// worktree record: a close-out arm going quiet because a SIBLING spoke. The
// guard below excludes the siblings it does not own (reclaimRunResidueFiled),
// and any future member of this family must either be written after the
// untracked record or be excluded the same way.
const reclaimRunResidueEvent = reclaimRunResidueUncleanEvent

// reclaimRunResidueDirtyEvent is the TRACKED shape's event: the close-out of a
// worktree left DIRTY WITH TRACKED CONTENT. The classifier above can already
// tell the two kinds of dirt apart, but the close-out record used to speak only
// for the untracked half — so a run that never committed its real deliverable
// closed out `completed` with NOTHING on its trail saying so. That is the worse
// of the epic's two shapes and it is the one that makes a branch permanently
// invisible to the merge queue while its row reads `completed` (measured
// 2026-09-16: row 01a0aa8b `completed` with 4 tracked edits + a new test file
// still uncommitted in its worktree).
//
// The event is kept DISTINCT from the untracked one (not folded into it)
// because the two shapes call for different remedies and an operator greps for
// the one they need: the untracked half is hygiene, this half is an uncommitted
// deliverable. Nothing has to be inferred by parsing a sentence.
const reclaimRunResidueDirtyEvent = reclaimRunResidueUncleanEvent + "-tracked-work"

// The three verdicts of reclaimResidueClass (Rule 1b: named, one definition).
// Their only purpose is to let every close-out decision — which sentence, which
// event, whether the enforcement switch may refuse — read the SAME
// classification instead of re-asking git's output in its own way.
const (
	reclaimResidueClean     = "clean"
	reclaimResidueUntracked = "untracked-only"
	reclaimResidueTracked   = "tracked"
)

// reclaimRunResidueNote is the named, attributed close-out refusal: WHO left
// it (the row being closed out), WHAT the state is (the classifier's own
// wording, referenced — never re-spelled, Rule 14) and WHERE it is (the row's
// recorded worktree). One definition, because the event detail and the stderr
// line must say the same thing.
func reclaimRunResidueNote(taskID, worktree, porcelain string) string {
	switch reclaimResidueClass(porcelain) {
	case reclaimResidueUntracked:
		return reclaimRunResidueUntrackedNote(taskID, worktree, porcelain)
	case reclaimResidueTracked:
		return reclaimRunResidueTrackedNote(taskID, worktree, porcelain)
	}
	return "" // clean: nothing to say
}

// reclaimRunResidueUntrackedNote is the untracked-litter sentence: the run's
// OWN scratch, still in its worktree at close-out.
func reclaimRunResidueUntrackedNote(taskID, worktree, porcelain string) string {
	return fmt.Sprintf("run %s closed out with its OWN untracked residue in %s: %s — entries: %s; the run's scratch was never cleaned (the outcome stands, nothing deleted)",
		taskID, worktree, reclaimUntrackedResidueMarker, reclaimPorcelainOneLine(porcelain))
}

// reclaimRunResidueTrackedNote is the TRACKED shape's sentence: the worktree's
// dirt includes a tracked change, i.e. an UNCOMMITTED DELIVERABLE. It quotes
// the class's own wording (referenced, never re-spelled — Rule 14) and states
// the remedy the rule prescribes for THIS shape, which is not the scratch
// remedy: the run must COMMIT what is deliverable (deleting it, or "cleaning
// its scratch", would destroy the work).
func reclaimRunResidueTrackedNote(taskID, worktree, porcelain string) string {
	return fmt.Sprintf("run %s closed out with UNCOMMITTED TRACKED WORK in %s: %s — entries: %s; the deliverable was never committed (commit it before reporting ready — a dirty close-out makes the branch invisible to the merge queue; the outcome stands, nothing deleted)",
		taskID, worktree, reclaimSkipDirtyReason, reclaimPorcelainOneLine(porcelain))
}

// reclaimRunResidueBlockedEvent is the event value for the residue that
// CANNOT be attributed to a directory (the row records no usable worktree):
// recorded as an explicit block rather than dripping one refusal per writeback
// (the idempotence guard below).
const reclaimRunResidueBlockedEvent = reclaimRunResidueEvent + "-unattributed"

// reclaimRunResidueBlockedNote names that state — a residue-shaped dirt in the
// run's own tree that this arm cannot attribute, so an operator (or the next
// worktree-stamping run) is told to fix the ground, not the file.
func reclaimRunResidueBlockedNote(taskID, porcelain string) string {
	return fmt.Sprintf("run %s closed out with untracked residue in its own ground, but the row records no usable code worktree to attribute it to: %s — entries: %s",
		taskID, reclaimUntrackedResidueMarker, reclaimPorcelainOneLine(porcelain))
}

// reclaimEnvEnforce names the operator switch that turns the close-out
// record into a REFUSAL: the row is left OPEN (status untouched, exit path
// unchanged) with the same attributed note, so the residue is visible in the
// queue's open list instead of only in the row's audit trail. Default OFF: the
// standing contract is the record, and an operator who wants the hard stop
// asks for it.
const reclaimEnvEnforce = "GOPI_TASK_RUN_RESIDUE_ENFORCE"

// ── the BINDING half: a TRACKED dirty close-out is REFUSED ───────────────────
//
// The record above makes a dirty close-out VISIBLE; measured case 01a0af50
// (2026-09-16) showed visibility was not enough. That run detected its own dirt
// correctly and said so four times — and the row was written back `completed`
// anyway, while the run's own report claimed "`git status --porcelain` empty in
// both worktrees (Rule 30)". ~1000 lines of finished, tested work (a 398-line
// implementation, a 332-line test file, a 177-line devdoc) sat uncommitted in
// an abandoned worktree and a human had to rescue it.
//
// So for the ONE shape where "the run did not deliver" is a fact ABOUT THE RUN,
// the record becomes a GATE: the run's OWN recorded worktree holding a TRACKED
// change (porcelain v1 — M/A/D/R/C/U/…; an entry that does not start with `??`
// names a path the index or HEAD already tracks) refuses the `completed`
// writeback at the state gate every door passes through (taskUpdate). The row
// stays OPEN; nothing is committed, stashed, deleted or rolled back.
//
// WHY THE GATE IS SAFE TO MAKE BINDING (the epic's own boundary, "do not make
// this fire for worktrees the run never worked in"): three independent
// conditions must ALL hold, and each is evidence the tree is the closing run's
// deliverable ground rather than somebody else's live work —
//
//	① the row's OWN recorded worktree: git must confirm the directory is a
//	   LINKED registration of the row's repo standing on the row's branch
//	   (reclaimRunCodeWorktree). A shared main checkout, a detached scratch
//	   ground, a recycled directory: never measured, never refused;
//	② no OTHER row's claim is live on that same directory
//	   (reclaimWorktreeHeldByLiveClaim): another run standing in the tree the
//	   closing row records is that tree's owner, not this one's — the epic's
//	   "dirty = a SIBLING run's live work" class. Unknowable ledger or claim =
//	   treated as live (never act on a tree we cannot attribute);
//	③ the untracked-only shape is NOT this gate's business. Untracked litter is
//	   nobody's work but it is also recoverable hygiene, and its refusal stays
//	   exactly where it was: the operator's switch
//	   (GOPI_TASK_RUN_RESIDUE_ENFORCE), the run's own scratch clause.
//
// The terminal status stays the one the run asked for: the gate REFUSES the
// claim, it does not re-label the row. `checked`/'blocked'/… would need a new
// value in the tasks CHECK constraint and the transition table (see
// taskStatusSet / taskAllowedTransitions — `reopened` is retired and nothing
// else exists to mean "work exists, uncommitted"), inventing one is the thing
// the epic forbids, and `failed` would be a lie in the other direction: the
// work IS done and IS in the tree. "Open at the refused writeback" is precisely
// the state a human needs — greppable, resumable, and honest — and the change
// the operator asks for is one `git commit` away.
//
// Deliberate asymmetry with the untracked half, stated once: the untracked
// switch is OFF by default (hygiene; the operator opts in), this gate is ON by
// default (an uncommitted deliverable is invisible work). Both keep an operator
// escape, and neither ever touches a byte of the tree.

// reclaimEnvDirtyCloseout names the operator override for the BINDING gate: an
// operator who has reviewed the tree (it is another author's work, or the
// commit is deliberately being made elsewhere) allows this writeback to proceed.
// Setting it never deletes the record — the tracked shape is filed on the trail
// either way, with the override named in its own event.
const reclaimEnvDirtyCloseout = "GOPI_TASK_DIRTY_CLOSEOUT_ALLOW"

// reclaimRunTrackedDirtRefusalEvent is the event value for the BINDING refusal — kept
// distinct from the plain record (reclaimRunResidueDirtyEvent) and from the
// untracked refusal (reclaimRunResidueRefusedEvent), because an operator
// grepping "which rows were stopped" must not have to parse a sentence.
const reclaimRunTrackedDirtRefusalEvent = reclaimRunResidueUncleanEvent + "-tracked-work-refused"

// reclaimRunTrackedDirtOverrideEvent is the event for a tracked dirty close-out
// the operator's override LET THROUGH: the shape was recorded, the refusal was
// lifted on purpose, and the trail says which of the two happened.
const reclaimRunTrackedDirtOverrideEvent = reclaimRunResidueUncleanEvent + "-tracked-work-override"

// reclaimRunTrackedDirtRefusalNote is the operator-facing sentence of the
// binding refusal: WHO (the row), WHERE (the run's own worktree) and WHAT
// (the exact entries), plus the two remedies — the run commits its own work, or
// an operator who has read the tree lifts the gate. Built from the classifier's
// own wording by reference (Rule 14), never re-spelled.
func reclaimRunTrackedDirtRefusalNote(taskID, worktree, porcelain string) string {
	return fmt.Sprintf("run %s left UNCOMMITTED TRACKED WORK in its OWN worktree %s: %s — entries: %s; the %s writeback is REFUSED, so the row stays open (nothing was committed, stashed or deleted — commit the work and write back; an operator who has reviewed the tree may allow this one writeback with %s=%s)",
		taskID, worktree, reclaimSkipDirtyReason,
		reclaimPorcelainOneLine(porcelain), taskStatusCompleted,
		reclaimEnvDirtyCloseout, reclaimEnvDirtyCloseoutOn)
}

// reclaimEnvDirtyCloseoutOn is the OVERRIDE's documented value. config.EnvBool
// accepts more truthy spellings (Rule 1b: the value the MESSAGE advertises has
// one definition, and it is the one the predicate accepts).
const reclaimEnvDirtyCloseoutOn = "1"

// reclaimDirtyCloseoutOverridden reports the operator's override (Rule 2: the
// SINGLE reader of reclaimEnvDirtyCloseout).
func reclaimDirtyCloseoutOverridden() bool { return config.EnvBool(reclaimEnvDirtyCloseout) }

// reclaimWorktreeHeldByLiveClaim reports whether ANY OTHER task row has a LIVE
// claim on this exact worktree directory (the row being closed out is excluded
// by finishCleanupWorktreeOwnerRows). It is condition ② of the gate's safety
// argument and it reuses the SAME liveness predicate the worktree-removal arm
// uses (finishCleanupOwnerRowLive → TaskWorktreeRow.HolderLive) rather than
// growing a second opinion about who owns a directory (Rule 14).
//
// Fail-CLOSED on an unreadable ledger: a tree this arm cannot attribute to
// somebody is not a tree it may refuse a close-out for. Same for a dead claim
// pid whose process identity cannot be resolved — the predicate calls that
// unknowable, and unknowable is never acted on.
func reclaimWorktreeHeldByLiveClaim(taskID, worktree string) bool {
	if strings.TrimSpace(worktree) == "" {
		return false
	}
	owners, err := finishCleanupWorktreeOwnerRows(taskIDNormalize(taskID))
	if err != nil {
		taskNotifyLog("dirty close-out ownership scan (%s): %v", worktree, err)
		return true
	}
	target := filepath.Clean(worktree)
	for _, o := range owners {
		if filepath.Clean(o.Deliverable) == target && o.HolderLive {
			return true
		}
	}
	return false
}

// reclaimRunTrackedDirtRefusal is the BINDING close-out gate for the tracked
// shape: it answers "may this run write back `completed`?" for the run's OWN
// worktree holding an uncommitted tracked change. Returns the refusal note and
// true when the writeback must NOT proceed.
//
// It shares ONE measurement with the record half (ReclaimRunResidueCheck) and
// one idempotence guard (reclaimRunResidueFiled), so the trail carries one
// family of facts however many doors the close-out passes through. Every
// failure mode is reported and NON-blocking (Rule 15): a run's exit must never
// depend on our ability to run git.
func reclaimRunTrackedDirtRefusal(taskID string) (string, bool) {
	out, err := ReclaimRunResidueCheck(taskID)
	if err != nil {
		taskNotifyLog("dirty close-out check for %s: %v", taskID, err)
		return "", false
	}
	if !out.Attributed || reclaimResidueClass(out.Porcelain) != reclaimResidueTracked {
		return "", false // not the run's own ground, or not a tracked change
	}
	if reclaimWorktreeHeldByLiveClaim(taskID, out.Worktree) {
		return "", false // another run's LIVE work: never this run's refusal to make
	}
	note := reclaimRunTrackedDirtRefusalNote(taskID, out.Worktree, out.Porcelain)
	if !reclaimRunResidueFiled(taskID) {
		reclaimRunResidueRecord(taskID, out) // the shape, in the classifier's own words
	}
	event := reclaimRunTrackedDirtRefusalEvent
	if overridden := reclaimDirtyCloseoutOverridden(); overridden {
		// The gate is lifted for this writeback on the operator's word — and the
		// trail says so, because "recorded" and "recorded AND let through" are
		// different facts about a row.
		event = reclaimRunTrackedDirtOverrideEvent
	}
	reclaimRunCloseoutEventOnce(taskID, event, note)
	return note, !reclaimDirtyCloseoutOverridden()
}

// reclaimRunCloseoutEventOnce files one close-out verdict as a ledger fact,
// bounded by the same per-row limit the untracked refusal uses: a run that keeps
// re-writing back without committing does not get to grow the trail without end.
func reclaimRunCloseoutEventOnce(taskID, event, detail string) {
	ctx, cancel := context.WithTimeout(context.Background(), reclaimGitTimeout)
	defer cancel()
	db, err := openTasksDB(ctx, memoryDBPath())
	if err != nil {
		taskNotifyLog("dirty close-out record %s: %v", taskID, err)
		return
	}
	defer db.Close()
	if taskEventCountForDetail(taskID, event, detail) < reclaimRunResidueRefusalLimit {
		taskEventWrite(ctx, db, taskID, event, detail)
	}
}

// reclaimRunResidueEnforced reports the operator's choice (Rule 2: the single
// reader of reclaimEnvEnforce).
func reclaimRunResidueEnforced() bool { return config.EnvBool(reclaimEnvEnforce) }

// reclaimRunCodeWorktree is the row's own recorded worktree, admissible ONLY
// when git agrees it is a linked worktree of the row's repo standing on the
// row's branch. Anything else — the MAIN checkout (removing/attributing a run
// to the shared tree is the 2026-09-16 leak this epic is about), a detached
// scratch checkout, a recycled directory, an unreadable registration — is ""
// (never a guess about somebody's directory, Rule 10.4: git's own plumbing is
// the evidence).
func reclaimRunCodeWorktree(repo, branch, worktree string) string {
	if repo == "" || branch == "" || worktree == "" {
		return ""
	}
	if liveDir(worktree) == "" {
		return "" // recycled / never created: nothing to measure
	}
	byBranch, _, err := mergedCleanupWorktrees(repo)
	if err != nil {
		return "" // occupancy unreadable: never guess
	}
	if byBranch[branch] != worktree {
		return "" // git does not say this directory holds the row's branch
	}
	return worktree
}

// reclaimRunAttributableWorktree is the ONE answer to "may the close-out residue
// gate MEASURE this directory as the closing row's own ground?" — a disjunction
// of the two shapes a ground can have, each proven by its own evidence:
//
//	① a LINKED worktree of the row's repo standing on the row's branch
//	  (reclaimRunCodeWorktree — git's worktree list is the evidence);
//	② a CLONE channel ground whose `.git` is a DIRECTORY and whose path, origin,
//	  identity and branch all name THIS row (reclaimRunCloneGroundWorktree — the
//	  filesystem and the ledger together are the evidence).
//
// WHY ② HAD TO BE ADDED (measured 2026-10-27, epic 01a17b9d): `dev_repo` is this
// host's default channel and the clone channel registers NOTHING anywhere, so
// every clone ground answered "" to ①, the gate measured nothing, filed no
// event, and the operator's enforce switch had nothing to refuse — six grounds
// holding 104 uncommitted entries read as `{Attributed:false Porcelain:""}`.
//
// WHAT IS DELIBERATELY NOT ADDED: "any live directory", or "the main checkout".
// Attribution stays EXCLUSIVE (the epic's hard constraint) — the shared checkout
// is outside the workspace root and named by no row's id, so neither arm admits
// it, and a detached scratch checkout fails the branch rung of both.
func reclaimRunAttributableWorktree(taskID, repo, branch, worktree string) string {
	if wt := reclaimRunCodeWorktree(repo, branch, worktree); wt != "" {
		return wt
	}
	return reclaimRunCloneGroundWorktree(taskID, repo, branch, worktree)
}

// detail (the audit trail is one row per event — a multi-line body would
// break the `result:` rendering). Order is git's own; nothing is re-sorted.
func reclaimPorcelainOneLine(porcelain string) string {
	var parts []string
	for _, line := range strings.Split(porcelain, "\n") {
		if line = strings.TrimSpace(line); line != "" {
			parts = append(parts, line)
		}
	}
	return strings.Join(parts, reclaimResidueFileSeparator)
}

// reclaimResidueFileSeparator separates the quoted entries of a residue
// report — one definition for every call site (Rule 14).
const reclaimResidueFileSeparator = " | "

// ReclaimRunResidueOutcome is the run's close-out residue verdict:
// Porcelain is the raw `git status --porcelain` body of the ground measured
// (empty = clean, or nothing measurable), and the two sentence fields are the
// operator lines the callers render — "" when there is nothing to say.
type ReclaimRunResidueOutcome struct {
	Porcelain string
	Note      string
	// Attributed: Note names the row's OWN worktree (the only shape the
	// enforcement switch may refuse).
	Attributed bool
	// Blocked: residue-shaped dirt this arm cannot attribute to a directory.
	Blocked  bool
	Worktree string
}

// reclaimResidueClass answers WHICH of the three close-out states a raw
// `git status --porcelain` measurement is in — clean, untracked-only litter,
// or dirty with tracked content (an uncommitted deliverable).
//
// It is a PREDICATE over the measurement (delegating to reclaimUntrackedOnly,
// so the two can never disagree), NOT a comparison of rendered sentences: the
// epic's own history shows a wording comparison silently silencing a gate
// (see reclaimUntrackedOnly's note). One classification, read by the sentence
// builders, the event pickers and the enforcement switch alike (Rule 14).
func reclaimResidueClass(porcelain string) string {
	if reclaimUntrackedOnly(porcelain) != "" {
		return reclaimResidueUntracked
	}
	if strings.TrimSpace(porcelain) == reclaimStatusClean ||
		strings.Contains(porcelain, reclaimStatusNoOutputSentinel) {
		return reclaimResidueClean
	}
	return reclaimResidueTracked
}

// reclaimRunResidueKind picks the ledger event for a measured class — the ONE
// place a shape becomes an event name, so the record, the refusal and any
// future reader cannot disagree about which event belongs to which shape.
func reclaimRunResidueKind(porcelain string) string {
	if reclaimResidueClass(porcelain) == reclaimResidueTracked {
		return reclaimRunResidueDirtyEvent
	}
	return reclaimRunResidueEvent
}

// reclaimRunResidueOutcome is the worktree-taking core of the close-out check,
// shared by the writeback (which has the row's task_id) and the CLI close-out
// (which has the row). See ReclaimRunResidueOutcome for the contract.
func reclaimRunResidueOutcome(repo, branch, worktree, dir string) (ReclaimRunResidueOutcome, error) {
	out := ReclaimRunResidueOutcome{Worktree: worktree}
	porcelain, err := reclaimGitStatus(dir)
	if err != nil {
		return out, err // unreadable ground: the caller REPORTS it (Rule 15)
	}
	out.Porcelain = porcelain
	if reclaimResidueClass(porcelain) == reclaimResidueClean {
		// Nothing left behind: the only state the rule calls correct.
		return out, nil
	}
	// BOTH dirty shapes are the run's own to answer for once the ground is the
	// row's OWN worktree (reclaimRunCodeWorktree): untracked litter by the
	// scratch clause, tracked dirt by the commit clause. Neither is deleted,
	// and neither rolls back the outcome — the record names it (Rule 15).
	if worktree == "" {
		// Dirt this arm cannot attribute to a directory. Only the untracked
		// shape is worth a standing block: a residue-shaped dirt in an
		// unattributable ground is drift the next stamping run must fix. An
		// unattributable TRACKED change is somebody's work in a tree this row
		// does not own, and it keeps today's handling EXACTLY (the sweep's
		// reclaimSkipDirtyReason) — the epic's own prohibition: never weaken or
		// add traffic to the uncommitted-work guards.
		if reclaimResidueClass(porcelain) == reclaimResidueUntracked {
			out.Blocked = true
		}
		return out, nil
	}
	out.Attributed = true
	return out, nil
}

// ReclaimRunResidueCheck measures the ROW's OWN recorded code worktree for the
// untracked-only residue a finished run failed to clean, at the moment it
// claims it is done.
//
// It is measured ONLY for a row whose recorded worktree git confirms (see
// reclaimRunCodeWorktree): a row with no worktree — the main checkout, a
// detached scratch ground, a recycled directory — is NOT measured here at all,
// because a refusal attributing the SHARED checkout's untracked files to this
// run would be evidence nobody has (that misattribution is the very finding
// this epic opened on).
//
// The returned outcome carries the raw measurement and (when there is
// something to say) ONE attributed sentence built from the classifier's own
// vocabulary. Nothing is deleted, no status is written, no branch is touched.
func ReclaimRunResidueCheck(taskID string) (ReclaimRunResidueOutcome, error) {
	taskID = taskIDNormalize(taskID)
	if taskID == "" {
		return ReclaimRunResidueOutcome{}, fmt.Errorf("run residue check: task_id must not be empty")
	}
	repo, branch, worktree := taskRowDeliverable(taskID)
	if wt := reclaimRunAttributableWorktree(taskID, repo, branch, worktree); wt != "" {
		out, err := reclaimRunResidueOutcome(repo, branch, worktree, wt)
		if err != nil {
			return out, err
		}
		if out.Attributed {
			out.Note = reclaimRunResidueNote(taskID, worktree, out.Porcelain)
		}
		return out, nil
	}
	// No attributable code worktree. The main checkout is never measured (it is
	// not a LINKED registration — see reclaimRunUnattributableGround, which
	// resolves the ground through git's own worktree list); a registered-but-
	// unattributable path (the row's branch moved on, or its record points at a
	// sibling run's tree) IS measured, because a run standing there is
	// demonstrably not that tree's owner — and `liveDir` keeps this entry point
	// off unreadable directories (never a git error per writeback).
	if wt := reclaimRunUnattributableGround(repo, worktree); wt != "" {
		out, err := reclaimRunResidueOutcome(repo, branch, "", wt)
		if err != nil {
			return out, err
		}
		if out.Blocked {
			out.Note = reclaimRunResidueBlockedNote(taskID, out.Porcelain)
		}
		return out, nil
	}
	// The SAME second leg for the CLONE shape (epic 01a17b9d): a clone ground of
	// this row that git will not confirm stands on the row's recorded branch is
	// still this run's directory, so it is MEASURED — attributed=false, the same
	// asymmetry (untracked-only blocks, tracked work never does).
	if wt := reclaimRunCloneGroundUnattributable(taskID, repo, worktree); wt != "" {
		out, err := reclaimRunResidueOutcome(repo, branch, "", wt)
		if err != nil {
			return out, err
		}
		if out.Blocked {
			out.Note = reclaimRunResidueBlockedNote(taskID, out.Porcelain)
		}
		return out, nil
	}
	return ReclaimRunResidueOutcome{}, nil
}

// reclaimRunUnattributableGround reports whether a row's recorded worktree is
// worth MEASURING even though git will not attribute it to the row: it exists,
// and it lives under the scratch tree a run's own scratch checkouts are made
// in (Rule 22). The main checkout is excluded by construction — it is not
// inside the scratch tree — which is what keeps this arm from reading (and
// then blaming) the shared tree.
func reclaimRunUnattributableGround(repo, worktree string) string {
	if repo == "" || worktree == "" || liveDir(worktree) == "" {
		return ""
	}
	byBranch, _, err := mergedCleanupWorktrees(repo)
	if err != nil {
		return ""
	}
	for _, path := range byBranch {
		if path == worktree {
			return worktree // a LINKED registration of the row's repo — never the main checkout
		}
	}
	return ""
}

// reclaimRunResidueFiled reports whether closing this row ALREADY carries a
// close-out residue record — the idempotence guard (a re-landed writeback of
// the same run must not drip a second refusal into the row's trail).
func reclaimRunResidueFiled(taskID string) bool {
	ctx, cancel := context.WithTimeout(context.Background(), reclaimGitTimeout)
	defer cancel()
	db, err := openTasksDB(ctx, memoryDBPath())
	if err != nil {
		return false // best-effort: an unreadable trail must not block a close-out
	}
	defer db.Close()
	var one int
	err = db.QueryRowContext(ctx,
		`SELECT 1 FROM task_events WHERE task_id = ? AND event LIKE ? AND event <> ? LIMIT 1`,
		taskID, reclaimRunResidueUncleanEvent+"%", reclaimMainCheckoutDepositEvent).Scan(&one)
	return err == nil
}

// reclaimRunResidueRecord is the ACT of recording: it writes the attributed
// event and the stderr note together, so the audit trail and the operator's
// terminal can never disagree about what happened (Rule 14). It returns the
// note that was filed ("" when nothing was).
func reclaimRunResidueRecord(taskID string, out ReclaimRunResidueOutcome) string {
	if out.Note == "" {
		return ""
	}
	event := reclaimRunResidueKind(out.Porcelain)
	if out.Blocked {
		event = reclaimRunResidueBlockedEvent
	}
	ctx, cancel := context.WithTimeout(context.Background(), reclaimGitTimeout)
	defer cancel()
	db, err := openTasksDB(ctx, memoryDBPath())
	if err != nil {
		// The trail is the carrier; without it the note still goes to stderr
		// (Rule 15: reported, never swallowed).
		taskNotifyLog("run residue record %s: %v", taskID, err)
		fmt.Fprintln(os.Stderr, "[task] "+out.Note)
		return out.Note
	}
	defer db.Close()
	taskEventWrite(ctx, db, taskID, event, out.Note)
	taskNotifyLog("%s", out.Note)
	return out.Note
}

// reclaimRunResidueAtWriteback is the close-out seam: measure the row's own
// worktree and RECORD the attributed refusal. It NEVER changes the outcome
// being written — the caller's terminal write is authoritative — and it does
// nothing at all for a row with no own worktree to attribute (the common case
// for a run that never left the main checkout: zero subprocesses).
//
// status is the terminal status being written; only a COMPLETED writeback is
// the run's "development is ready" claim this arm enforces (a failed run's
// residue is not a readiness claim).
func reclaimRunResidueAtWriteback(taskID, status string) {
	if status != taskStatusCompleted {
		return
	}
	out, err := ReclaimRunResidueCheck(taskID)
	if err != nil {
		taskNotifyLog("run residue check for %s: %v", taskID, err)
		return
	}
	if out.Note == "" {
		return // the common case: no own worktree / clean / a tracked change
	}
	if reclaimRunResidueFiled(taskID) {
		return // idempotent: the closing row already carries the record
	}
	reclaimRunResidueRecord(taskID, out)
}

// reclaimRunResidueRefusal is the close-out REFUSAL half (the operator's
// enforce switch): the row's status is left UNTOUCHED and the row stays OPEN —
// the same attributed note is already filed, so the queue's open list carries
// the handle and the run can clean its own scratch and write back again. A
// false return means "not refused": either nothing was found or the switch is
// off. The caller supplies the completion context (this function answers only
// for the OWN-worktree case a refusal may act on).
func reclaimRunResidueRefusal(taskID string) (string, bool) {
	if !reclaimRunResidueEnforced() {
		return "", false
	}
	out, err := ReclaimRunResidueCheck(taskID)
	if err != nil {
		taskNotifyLog("run residue refusal check for %s: %v", taskID, err)
		return "", false
	}
	// Attributed now covers BOTH dirty shapes. The switch's contract, though,
	// is fixed to the shape it was built for: an untracked-only tree is the
	// run's own hygiene debt and stopping it is safe, while a tree holding
	// TRACKED work may hold another session's in-progress edits (the epic's ten
	// worktrees are exactly that). So the tracked shape is ALWAYS recorded and
	// never refused — the same line the epic draws: report the violation, never
	// build a machine that acts on somebody's uncommitted work.
	if !out.Attributed || reclaimResidueClass(out.Porcelain) != reclaimResidueUntracked {
		return "", false // only the row's OWN untracked residue is refusable
	}
	event := reclaimRunResidueKind(out.Porcelain)
	if out.Blocked {
		event = reclaimRunResidueBlockedEvent
	}
	if !reclaimRunResidueFiled(taskID) {
		reclaimRunResidueRecord(taskID, out)
	}
	ctx, cancel := context.WithTimeout(context.Background(), reclaimGitTimeout)
	defer cancel()
	db, err := openTasksDB(ctx, memoryDBPath())
	if err != nil {
		taskNotifyLog("run residue refusal record %s: %v", taskID, err)
		return "", false
	}
	defer db.Close()
	// The refusal is itself a ledger fact: it is WHY the row is still open, so
	// it rides the same append-only trail as the record. Re-typing the same
	// refusal on a repeated writeback is deduped against the trail.
	detail := out.Note
	if taskEventCountForDetail(taskID, event, detail) < reclaimRunResidueRefusalLimit {
		taskEventWrite(ctx, db, taskID, reclaimRunResidueRefusedEvent, detail)
	}
	return out.Note, true
}

// reclaimRunResidueRefusedEvent is the event value for the HARD refusal, kept
// distinct from the plain record so an operator can tell "the run was told"
// from "the run was stopped" without parsing any sentence.
const reclaimRunResidueRefusedEvent = reclaimRunResidueEvent + "-refused"

// reclaimRunResidueRefusalLimit bounds how many refusal rows one row can
// accumulate: a run that keeps re-writing back without cleaning does not get
// to grow the trail without end (the record itself is idempotent; only the
// refusal is counted).
const reclaimRunResidueRefusalLimit = 3

// taskEventCountForDetail counts the row's events of one kind carrying one
// detail verbatim — the refusal's dedup/bound guard. It opens its own
// connection, so callers that already hold a transaction must use
// taskEventCountForDetailOn instead: a second connection cannot read while
// the caller's write transaction is still open.
func taskEventCountForDetail(taskID, event, detail string) int {
	ctx, cancel := context.WithTimeout(context.Background(), reclaimGitTimeout)
	defer cancel()
	db, err := openTasksDB(ctx, memoryDBPath())
	if err != nil {
		return 0
	}
	defer db.Close()
	return taskEventCountForDetailOn(ctx, db, taskID, event, detail)
}

// taskEventCountForDetailOn is the transaction-aware form of the same count:
// the caller hands in the handle it already owns (its *sql.Tx while inside a
// write transaction, an *sql.DB elsewhere). Without this arm the count is
// issued on a SECOND connection, which blocks on the caller's own open
// transaction and reports 0 — so the dedup/bound guard would read "nothing
// recorded yet" and the caller could never see its own trail entry.
func taskEventCountForDetailOn(ctx context.Context, q QueryExecer, taskID, event, detail string) int {
	var n int
	if err := q.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM task_events WHERE task_id = ? AND event = ? AND detail = ?`,
		taskID, event, detail).Scan(&n); err != nil {
		return 0
	}
	return n
}

// reclaimGitRemove runs the ordinary `git worktree remove` (never --force):
// git re-checks dirty/locked state itself, so a guard this file missed still
// cannot destroy work.
func reclaimGitRemove(repo, worktree string) error {
	// NO emit here on purpose: this is the shared low-level runner, and the
	// arms that call it each emit their OWN record (with their own arm name and
	// deciding evidence) immediately before calling — one record per removal,
	// never two. A single emit at this depth would have to guess the arm.
	ctx, cancel := context.WithTimeout(context.Background(), reclaimGitTimeout)
	defer cancel()
	// removal-audit-ok: shared runner — every caller (worktree-sweep-reclaim,
	// task-row-reclaim, task-self-reclaim, task-self-own-sweep) emits its own
	// record immediately before this call, asserted by their own tests.
	if _, err := finishCleanupGitRaw(ctx, repo, finishCleanupWorktreeVerb, finishCleanupWorktreeRemoveVerb, worktree); err != nil {
		return fmt.Errorf("worktree remove %s: %w", worktree, err)
	}
	return nil
}
