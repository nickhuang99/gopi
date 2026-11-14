// Producer-side (self) worktree release — the arm that frees the directory of
// a task THIS process ran, at the moment this process finishes it.
//
// THE GAP THIS CLOSES (live row 01a095b2, 2026-09-12): ReclaimDeadHolderWorktrees
// — the only release arm that existed — is keyed on the claiming pid being
// provably DEAD, and the claim stamp a worker round writes is the long-lived
// `gopi worker` PID. So the moment the row finished, its holder pid was still
// alive (the worker loop goes on to the next round), which made the row
// INVISIBLE to every sweep:
//
//	the finish-cleanup sweep's stranded scan — capped windows, and the row's
//	  chain can be skipped by the ownership / live-owner gates (that sweep is
//	  RETIRED as of 58b5618b; the paragraph below describes the gap this arm
//	  was built to close, which is why the arm outlived it);
//	ReclaimDeadHolderWorktrees — requires a DEAD pid, so it never looks;
//	while `git worktree list` keeps naming the directory, scripts/safe-merge.sh
//	  gate [c] refuses the branch and the merger's protected arm drops it —
//	  the finished deliverable is withheld, silently and forever.
//
// The producer is the ONE party that knows the truth without inference: it is
// the process NAMED in the claim stamp, and it has just finished the row. The
// release below is therefore keyed on the liveness fact a terminal state
// cannot express — "the row is over" — plus the one thing that makes it safe:
// THE HOLDER IS THIS PROCESS. No other worker's row is ever a candidate, so
// nothing alive can be torn down: the guards only have to protect against
// FRESH work in the directory, never against a live peer.
//
// THE THREE GUARDS ARE THE EXISTING ONES (Rule 14 — one implementation of
// "may this directory go", never a second copy): reclaimDisposition decides,
// and the removal is reclaimGitRemove (the ordinary `git worktree remove`,
// no --force; the BRANCH IS NEVER TOUCHED — it is the deliverable).
//
//	① the row must be THIS process's and its life must be over (in_progress
//	   included — see below), so a live peer's ground is never in the set;
//	② the directory must be CLEAN — uncommitted work is work, left for a human;
//	③ no live process may be working inside it (the cwd scan, whose self-chain
//	   walk already excludes this process), and the checkout must still be on
//	   the branch the row names.
//
// Every refusal is NAMED (Rule 15): the guard phrase travels in the report
// line, so an operator sees WHY a directory a finished run parked is still
// there instead of the pass printing nothing at all.
package tools

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/nickhuang99/gopi/internal/boolvocab"
	"github.com/nickhuang99/gopi/internal/removalaudit"
	"github.com/nickhuang99/gopi/internal/scratch"
	"github.com/nickhuang99/gopi/internal/worktreerelease"
	"github.com/nickhuang99/gopi/internal/worktreeroots"
)

const (
	// ReclaimSelfEnv is the kill switch for the producer-side removal (the
	// finish-cleanup env convention: "0"/"off"/"no"/"false"/"disabled" turns
	// it off). Off = report only: the pass still NAMES what it would free.
	//
	// It deliberately does NOT reuse ReclaimDeadHolderEnv: that switch is the
	// operator's emergency brake on the SWEEP clearing somebody else's dead
	// ground, while this leg only ever clears ground THIS process owns — one
	// knob silently covering both would make "stop the sweep" also stop the
	// producer's own hygiene, which is the regression this file exists to fix.
	ReclaimSelfEnv = "GOPI_SELF_WORKTREE_RELEASE"

	// ProcRootPath is the PROCESSS-FILE-SYSTEM mount point ("/proc"): the entry
	// directory every procfs read in this package and in cmd/agent resolves
	// under (<procfs>/<pid>/{comm,stat,cwd,cmdline}). It is NOT the release
	// destination — the two coincided in an earlier spelling of this file and
	// conflating them silently broke /proc reads, because "/" holds no
	// "<pid>/comm". An alias is only correct between names that mean the SAME
	// thing; these are procfs knowledge, so it is kept under a procfs name.
	ProcRootPath = procRootName

	// CwdReleasePath is the directory a producer's own close-out re-parents its
	// CWD to once it has released the linked worktree it ran in. It is an ALIAS
	// of worktreerelease.ProcessRootPath — the ONE definition of the release
	// destination (Rules 13/14), kept so callers here need not import the leaf
	// package for a path.
	//
	// WHY the root and not a temp dir: `git worktree remove` deletes the
	// directory the process is standing in, and a process holding a deleted
	// directory keeps its inode (and every file under it) alive until it exits.
	// The root always exists, so the chdir cannot fail on a directory somebody
	// else removed first.
	CwdReleasePath = worktreerelease.ProcessRootPath

	// reclaimSelfScanMax caps how many finished rows ONE process releases per
	// pass. A worker is single-threaded, so this is a small number; the cap
	// exists for the same reason its siblings have one — a runaway ledger must
	// never turn one close-out into an unbounded loop of git subprocesses.
	reclaimSelfScanMax = 20

	// ReclaimSelfReleasedMarker is the phrase the release line carries — one
	// marker for the log AND the tests, so an operator greps one string.
	ReclaimSelfReleasedMarker = "own worktree released"

	// reclaimSelfRefusedMarker is the phrase every REFUSAL carries (the
	// finishCleanupInUseMarker convention): the guard's own sentence follows
	// it, so a withheld directory is never a silent one.
	reclaimSelfRefusedMarker = "own worktree release refused"

	// reclaimSelfOwnLiveHolderReason is rung ②'s refusal sentence. It is a
	// constant (Rule 1b) because the test that proves an orphaned
	// cwd-inheritor is NOT a holder must name the very sentence the rung
	// renders, and because it is quoted from the ledger-keyed sibling arm's own
	// convention ("one implementation of 'in use'").
	reclaimSelfOwnLiveHolderReason = "held by a live process (cwd/fd/exe/maps)"

	// reclaimSelfSkipNoWorktree is the row-side refusal: the deliverable names
	// a repo but no directory, so there is nothing this pass could free.
	reclaimSelfSkipNoWorktree = "the row's deliverable names no worktree"
)

// ReclaimOwnFinishedWorktrees releases the linked worktrees of the rows THIS
// process finished (or is finishing), and returns one line per row it acted
// on — released, refused (named), or unreadable. It is the producer half of
// the release lifecycle: the worker's own close-out, not a sweep over other
// people's rows.
//
// The candidate set is "claimed_by THIS process" — the claim stamp the run
// ladder writes is `host:pid` for the executing process (TaskClaimOwner →
// taskDefaultClaimHolder), so the match is exact rather than inferred: a row
// this process did not run can never be in it.
func ReclaimOwnFinishedWorktrees() []string {
	rows, err := reclaimSelfRows()
	if err != nil {
		// Rule 15: an unreadable ledger is REPORTED, never rendered as
		// "nothing to release".
		return []string{reclaimSelfRefusedMarker + ": ledger scan failed: " + err.Error()}
	}
	if len(rows) == 0 {
		return nil
	}
	// THE CLOSE-OUT MARK (reclaim_own_ground.go): the rows below ARE this
	// process's own finished (or finishing) work — every one of them was
	// executed by THIS pid, which is what the claim stamp means — so their
	// ground is the one thing this pass may release even though the pass is
	// standing in it, even though the release is seconds old, and even though a
	// guest would be refused. It also carries the directory this process was
	// LAUNCHED in when that directory is a linked worktree of one of these rows'
	// repos: a run started inside a task's worktree works in its own created
	// ground, so no row names the directory the operator launched from, and
	// without it the measured residue survives every ledger-keyed arm. Published
	// HERE, by the pass that knows (Rules 13/14), and disarmed when the pass
	// ends: the sweep's arms never reach this function and are never armed.
	done := SelfGroundReleaseArmCloseout(rows)
	defer done()
	// One plan per pass: it memoizes `git worktree list` per repo, so a
	// handful of finished rows cost one subprocess each, not one per guard.
	prunes := newReclaimPrunePlan()
	var out []string
	for _, r := range rows {
		out = append(out, reclaimSelfRelease(r, prunes)...)
	}
	return out
}

// reclaimSelfRelease is one row's release: the shared disposition decides, the
// plan repairs a registration whose directory is already gone, and only a
// PROVEN-FREE directory is removed. Returns the line(s) this row contributes.
func reclaimSelfRelease(r reclaimDeadHolderRow, prunes *reclaimPrunePlan) []string {
	if strings.TrimSpace(r.Worktree) == "" {
		return []string{reclaimSelfRefusedMarker + ": " + r.Branch + " (" + r.TaskID + ") — " + reclaimSelfSkipNoWorktree}
	}
	// The GUEST refusal, asked on THIS arm's own terms (path B): this process
	// releases the worktrees of the rows IT ran, and a run owns its row's
	// worktree only where it also owns the repository it stands in. A process
	// standing in a LINKED worktree of that repo is not the process the row's
	// worktree belongs to — releasing from there is how the measured case lost
	// the checkout it was running from. See guest_row_guard.go; the predicate
	// asking the identical question is guestRowOwnRefusal (a distinct ARM name,
	// deliberately, because "a foreign row was freed" and "my own row's
	// worktree was freed" are two different incidents to an operator).
	//
	// ASKED IN THE CLOSE-OUT FORM (reclaim_own_ground.go): when THIS pass is the
	// one releasing this row's ground, the guest shape the arm describes is the
	// CONTRACT, not the hazard — the run stands in the ground because the pass
	// deliberately runs before the cwd release (cmd/agent taskReclaimOwnDirectories
	// documents that order as load-bearing), so refusing here would make the chain
	// unable to satisfy its own contract. The mark narrows the arm to the exact
	// directories this pass is closing out; every OTHER call of the arm (the
	// ledger-keyed sweep, the unowned-registration pass) is unarmed and refuses
	// exactly as before.
	if !SelfGroundReleasePendingFor(r.Worktree) {
		if why := guestRowOwnRefusal(r.Repo); why != "" {
			return []string{reclaimSelfRefusedMarker + ": " + r.Branch + " (" + r.TaskID + ") — " + why}
		}
	}
	// The OWN-GROUND refusal (arm ③ of the same file): a row's deliverable records
	// exactly ONE worktree, and this pass's candidate set is keyed on the claim
	// stamp — so the directory under judgement can be the very checkout THIS
	// process is standing in. `guestRowOwnRefusal` answers "" for that pair (the
	// repo the row names IS the repo the actor is a guest of), so the ground needs
	// its own rung; the breach was measured on 2026-09-21
	// (`~/workspace/gopi-jieba-searchkw`, row 01a0c574, holder alive, branch 14
	// ahead). Asked BEFORE the disposition so a refusal does not cost the
	// directory probes (the sibling rungs' order).
	if !SelfGroundReleasePendingFor(r.Worktree) {
		if why := guestRowOwnCheckoutRefusal(r.Worktree); why != "" {
			return []string{reclaimSelfRefusedMarker + ": " + r.Branch + " (" + r.TaskID + ") — " + why}
		}
	}
	// ASKED IN THE CLOSE-OUT FORM (reclaim_own_ground.go): a ground the CURRENT
	// pass is closing out is that pass's own to release — the manual-run contract
	// is that the pass runs BEFORE the cwd release, so the asker is still standing
	// in it BY DESIGN. Outside a close-out pass nothing is armed and the rung above
	// reads exactly as before (the 2026-09-21 breach is still refused).
	// THE DETERMINISTIC CHECKLIST (reclaim_ground_checklist.go): rung ① the ground
	// is not mine, rung ② the branch shares history with the integration branch.
	// The guards this arm shares with every other (dirty / in-use / the row's
	// life) are asked by reclaimDisposition below, which is where they already
	// lived — the checklist does not re-ask them.
	if why := reclaimGroundChecklistVerdict(r.Repo, r.Worktree, r.Branch); why != "" {
		return []string{reclaimSelfRefusedMarker + ": " + r.Branch + " (" + r.TaskID + ") — " + why}
	}
	disp, why := reclaimDisposition(r, prunes)
	switch disp {
	case reclaimSkip:
		// An EMPTY reason is the "registration already clean" case: the
		// directory is gone and git dropped it too, so there is nothing left to
		// free AND nothing to say — a line per pass would be the very burst this
		// arm exists to end (the sweep's skip honours the same contract). A
		// NAMED reason is a REFUSAL — a guard said no, and a withheld directory
		// must never be a silent one (Rule 15).
		if strings.TrimSpace(why) == "" {
			return nil
		}
		return []string{reclaimSelfRefusedMarker + ": " + r.Branch + " (" + r.TaskID + ") — " + why}
	case reclaimVanishedProven:
		// The directory is gone but git still holds its registration, and gate
		// [c] greps that list: the repair is what unblocks the branch. It is
		// the SAME repair the sweep runs, credited per row by git's own
		// before/after statement.
		return prunes.repair([]reclaimDeadHolderRow{r}, reclaimSelfRemovalEnabled())
	}
	if !reclaimSelfRemovalEnabled() {
		return []string{reclaimSelfRefusedMarker + ": " + r.Branch + " (" + r.TaskID +
			") — removal disabled by " + ReclaimSelfEnv}
	}
	// THE AUDIT RECORD GOES WITH THE REMOVAL THAT ACTUALLY HAPPENS, so each
	// path below emits exactly ONE (the package contract: one record per
	// removal, with the arm that decided it). The chain's own step ③ records
	// under its own arm; this arm records only when IT does the removing.
	// Audit record BEFORE the removal (package removalaudit): this row is
	// THIS run's own finished work, and the disposition proved the directory
	// free (the live-session channel included).
	emitSelfReclaimRecord := func() {
		removalaudit.WorktreeRemove(removalaudit.ArmSelfReclaim, r.Repo, r.Worktree, r.Branch, r.TaskID,
			"this run's own finished row "+r.TaskID+", disposition "+disp.String()+": "+why+
				"; checklist "+reclaimGoalChecklistSummary)
	}
	// THE DELIVERY CHAIN IS NO LONGER ASKED (2026-10-30, task 01a18799,
	// slice 3/4). This pass used to ask the close-out chain's ②push ③worktree
	// ④-de for this row before freeing the directory, because a row's
	// deliverable was a BRANCH somebody had to hand over. Under Rule 6c a
	// dispatched run's delivery is its clone ground's own END-hook publish, so
	// there is no branch to hand over and nothing for this pass to ask: the
	// directory is freed by the guarded removal below, and the branch (when one
	// exists) is the merger's input, left alone.
	emitSelfReclaimRecord()
	if err := reclaimGitRemove(r.Repo, r.Worktree); err != nil {
		return []string{reclaimSelfRefusedMarker + ": " + r.Branch + " (" + r.TaskID + ") — remove failed: " + err.Error()}
	}
	return []string{ReclaimSelfReleasedMarker + ": " + r.Branch + " (" + r.TaskID + ") freed for merge; branch kept"}
}

// reclaimSelfRemovalEnabled reads the kill switch (one reader, Rule 2); the OFF
// spellings come from the ONE vocabulary definition (internal/boolvocab) rather
// than a list re-spelled here (Rules 13/14).
func reclaimSelfRemovalEnabled() bool {
	return !boolvocab.IsFalse(os.Getenv(ReclaimSelfEnv))
}

// reclaimSelfStamp returns the claim stamp that names THIS process — the
// ledger's own renderer, so the "is this mine" test compares exactly the shape
// the claim ladder wrote (never a hand-rolled hostname:pid that the encoding
// could drift away from). "" when the hostname is unreadable: an unnameable
// process claims nothing, which is the safe direction (a release that cannot
// prove ownership does not run).
func reclaimSelfStamp() string {
	stamp := strings.TrimSpace(TaskClaimOwner(""))
	if stamp == "" || strings.HasPrefix(stamp, reclaimSelfUnresolvedHostPrefix) {
		// taskDefaultClaimHolder's fallback name when os.Hostname fails: it
		// names no host, so no row can be attributed to this process by it.
		return ""
	}
	return stamp
}

// reclaimSelfUnresolvedHostPrefix is the prefix of the claim-holder fallback
// used when the hostname cannot be read (the value is what
// taskDefaultClaimHolder writes in that case — referenced, never re-typed,
// through reclaimSelfStamp's caller-side filter below).
const reclaimSelfUnresolvedHostPrefix = syncUnknownHost

// reclaimSelfRows scans the ledger for rows THIS process holds and whose life
// is over — the producer's own finished work.
//
//	in_progress IS in the set, and that is the crux. The worker writes the
//	terminal state only AFTER its release pass runs (the pass is a defer on a
//	state function that fires while the row is still in_progress; see
//	cli_tasks_worker.go), and the wrapper writes `completed` only after the
//	child process is already gone (runTaskRunOnce writes it once cmd.Run
//	returns). So at close-out time the honest status is still in_progress. The
//	status axis is what makes that safe where the sweep map could not: the
//	claim pid IS this process, so the row can only be one this process ran —
//	"in_progress" here means "my run is at its end", never "somebody is
//	working in this directory".
func reclaimSelfRows() ([]reclaimDeadHolderRow, error) {
	stamp := reclaimSelfStamp()
	if stamp == "" {
		return nil, nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), finishCleanupDBTimeout)
	defer cancel()
	db, err := openTasksDB(ctx, memoryDBPath())
	if err != nil {
		return nil, err
	}
	defer db.Close()
	return reclaimSelfRowsFrom(ctx, db, stamp)
}

// reclaimSelfRowsFrom is the query body over an injected handle (the pass and
// the tests share ONE reader).
func reclaimSelfRowsFrom(ctx context.Context, db *sql.DB, stamp string) ([]reclaimDeadHolderRow, error) {
	rows, err := db.QueryContext(ctx,
		`SELECT task_id, status, deliverable,
		        COALESCE(`+tasksProcPidColumnName+`,0), COALESCE(`+tasksProcStartColumnName+`,0), COALESCE(`+tasksProcCmdColumnName+`,'') FROM tasks
		 WHERE claimed_by = ?
		   AND status IN (?, ?, ?)
		   AND deliverable LIKE ?
		 ORDER BY updated_at ASC LIMIT ?`,
		stamp,
		taskStatusInProgress, taskStatusCompleted, taskStatusFailed,
		"%"+DeliverableWorktreeKey+"%",
		reclaimSelfScanMax)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []reclaimDeadHolderRow
	for rows.Next() {
		var id, status, deliverable string
		var procPid, procStart int64
		var procCmd string
		if err := rows.Scan(&id, &status, &deliverable, &procPid, &procStart, &procCmd); err != nil {
			return nil, err
		}
		if !taskCleanupEligibleStatus(status) && status != taskStatusInProgress {
			continue
		}
		wt := taskDeliverableWorktree(deliverable)
		if wt == "" {
			continue
		}
		out = append(out, reclaimDeadHolderRow{
			TaskID:    id,
			Status:    status,
			ClaimedBy: stamp,
			Repo:      finishCleanupParseDeliverable(deliverable).Repo,
			Branch:    taskDeliverableBranchOrEmpty(deliverable),
			Worktree:  wt,
			// The row's recorded RUNNER identity, carried rather than left
			// empty: it is the one channel on this arm that can name a run
			// other than THIS process, and an empty Ident is precisely how
			// the live-run veto went blind on the 2026-09-21 breach
			// (`disposition reclaimFreable: ` — an empty checklist). The
			// ledger CLEARS these columns on a terminal transition
			// (tasks.go), so a row that reached `completed` would otherwise
			// have no way to answer "was my run alive".
			Ident: procIdentity{Pid: int(procPid), StartTicks: procStart, Cmd: procCmd},
		})
	}
	return out, rows.Err()
}

// reclaimSelfRefused reports whether a report line is a REFUSAL worth an
// operator's attention (as opposed to a plain release). Kept here next to the
// marker so the two can never drift apart.
func reclaimSelfRefused(line string) bool {
	return strings.Contains(line, reclaimSelfRefusedMarker)
}

// reclaimSelfLog surfaces a refused release on the operator sink. The release
// pass itself returns its lines to the caller, which prints them; a refusal is
// additionally recorded here because the worker's stdout goes to the round's
// output log, while the notify sink is what an operator watches.
func reclaimSelfLog(lines []string) {
	for _, line := range lines {
		if reclaimSelfRefused(line) {
			taskNotifyLog("%s", line)
		}
	}
}

// reclaimSelfGitTimeoutNote is the operator note emitted when the release pass
// overran its own git budget: recorded rather than silent (Rule 15) — the
// directory was left alone, which is the safe direction.
const reclaimSelfGitTimeoutNote = "own worktree release timed out"

// reclaimSelfBudget bounds the WHOLE pass, so a hung git can never hold a
// worker's close-out open. It is expressed as a multiple of the per-probe
// timeout rather than a second magic number: the pass makes a known, small
// number of probes per row, so the budget and the probe cap stay in one
// relation.
//
// THE DELIVERY ALLOWANCE IS PART OF IT (2026-10-17). The pass now asks the
// close-out chain for the row's delivery, and that chain contains a step —
// `branch-checks`, the branch's own scoped test run at the revision being handed
// over — whose bound is finishCleanupBranchCheckDefaultTimeoutSec, NOT a git
// probe timeout. Composing the budget without it would make the wrapper abandon
// a chain in the middle of a legitimately long test run: the goroutine survives
// (buffered channel), but the ROUND has already reported "exceeded" and moved
// on, so a delivery that was about to land reads as a timeout — the measured
// class of defect this whole change exists to remove (Rule 15, and one
// relation per bound, Rule 2). Two terms, both taken from the owner of the
// bound, never typed as seconds here.
const reclaimSelfBudget = reclaimSelfProbesPerRow*reclaimGitTimeout +
	time.Duration(finishCleanupBranchCheckDefaultTimeoutSec)*time.Second

// reclaimSelfProbesPerRow is the number of git probes ONE row can cost the
// disposition (worktree list, status, branch, plus the removal), rounded up —
// the multiplier that keeps reclaimSelfBudget tied to reclaimGitTimeout.
const reclaimSelfProbesPerRow = 4

// ReclaimSelfOwnRegisteredWorktrees is the OWNERSHIP-INDEPENDENT half of the
// producer release: it frees the linked worktrees a run of THIS process created
// in the repos it worked in, whether or not the ledger ever recorded them.
//
// WHY THE LEDGER CANNOT ANSWER THIS (this epic's case 2, measured 2026-09-16).
// Every other arm is ledger-keyed, and a row's deliverable records exactly ONE
// worktree — so of the two linked worktrees a single run typically creates (its
// Rule 6 development tree, plus whatever scratch checkout a round spins up:
// `git worktree add --detach <dir> <tip>` for a baseline or a verification),
// only the FIRST is ever owned. The seam that stamps the row refuses to REBIND
// an existing stamp (a foreign probe must not hijack a row's record), so the
// second registration is named by NO ledger row, owned by NO row, and invisible
// to ReclaimDeadHolderWorktrees, the age sweep's ownership rung and the
// finish-cleanup chain alike. Measured live: a verification run's `wt-tip`
// under ~/workspace/gopi-verify-<taskid> sat registered while its own row named
// the sibling directory; `gopi task sweep-worktrees --dry-run` reported it
// "younger than the threshold" and the ledger reported it unowned.
//
// The EVIDENCE ladder here deliberately does not consult the ledger at all — it
// consults the PROCESS, because the producer is the one party that knows:
//
//	① AGE is measured from the registration's creation record (the same
//	   git-internal backlink the sweeper uses, never a directory mtime), and a
//	   registration younger than reclaimSelfOwnMinAge is KEPT. That floor is what
//	   protects a CONCURRENT round of this same process from having its fresh
//	   checkout deleted as it is being made (a phase-2 round runs while phase 1
//	   closes out, and it legitimately holds a seconds-old worktree).
//	② a live holder vetoes — the shared cwd/fd/exe/maps scan, so a checkout some
//	   running work (a background test job, a daemon started inside) is standing
//	   in is never torn down. This is the guard that makes the wider ownership
//	   rule safe, and it is the same implementation the sweeper uses (Rule 14).
//	③ the tree must be CLEAN, with the SAME two-state reading as every sibling
//	   arm (reclaimDirtyReason): tracked changes and untracked-only residue both
//	   refuse, the second under its own named reason.
//	④ the registration must be a LINKED worktree inside a DECLARED root
//	   (worktreeroots policy, resolved per repo) — the main checkout, anything
//	   outside a declared root, a locked entry and a bare registration are never
//	   this pass's business.
//
// Unlike the sweeper's rung, a BRANCH is not a veto here: the branch is not
// touched by `git worktree remove` (only the directory is), so a live row that
// still names the branch loses nothing — and a run of this process that parked
// a branch's checkout is exactly the leak being closed.
//
// The pass is BOUNDED (reclaimSelfOwnMax registrations) and REPORTED: every kept
// registration carries its reason, so a directory that is not freed is never a
// silent one (Rule 15).
// ReclaimOwnWorktreesAtCloseout is the close-out SEAM: the ONE entry point a
// producer's terminal pass calls, and the only place both ownership-independent
// halves run under the close-out mark (reclaim_own_ground.go).
//
// WHY A SEAM AND NOT TWO CALLS. The two halves have different callers with
// different standing — a producer's close-out (which must release its own ground,
// including the directory the run was launched in, named by no ledger row) and
// the ownership-independent SWEEP (which must free only what is aged, unheld and
// owned by nobody). A permission attached to the FUNCTION is therefore handed to
// the sweep too, which is not a hypothetical: arming inside
// ReclaimSelfOwnRegisteredWorktrees turned the pre-existing criterion
// `TestGuestRowRefusesToReconcileFromAGuestWorktree` ("guest frees its OWN row of
// its own repo" → that directory must SURVIVE) RED. The permission belongs to the
// STANDING, so it is published here, at the seam, and the halves stay inert.
//
// The mark is armed ONCE around both halves (not per half), because the
// ledger-keyed half disarms its own on return and the second half would then run
// unarmed — measured as the pass-order defect this seam exists to close.
func ReclaimOwnWorktreesAtCloseout() []string {
	done := selfGroundReleaseArmCloseoutPass()
	defer done()
	out := ReclaimOwnFinishedWorktrees()
	out = append(out, ReclaimSelfOwnRegisteredWorktrees()...)
	return out
}

// Self = this PROCESS, not this ROW.
//
// ReclaimSelfOwnRegisteredWorktrees is the OWNERSHIP-INDEPENDENT half of the
// producer release: it frees the linked worktrees a run of THIS process created
// in the repos it worked in, whether or not the ledger ever recorded them. The
// EVIDENCE ladder it walks (age floor, live holder, cleanliness, declared root,
// the closure checklist) is documented at reclaimSelfOwnWhy and
// reclaimSelfOwnOutOfRootDisposal; what follows is the SUBJECT it reports under,
// which is the part this name got wrong.
//
// WHY THE DECLARATION IS THE FIRST LINE, AND WHY IT IS MACHINE-CHECKED. `Self`
// here is the SUBJECT OF THE SCAN — this process, whose claim stamps it reads —
// and NEVER the scope of the report. The distinction is not pedantry: with no
// task-id parameter there was nothing to contradict a reader who took `Self` to
// mean "this row's", so the pass was believed row-scoped while it enumerates
// EVERY repo the ledger names and then EVERY registration of each. Measured
// 2026-10-27 (own_worktree_ownership.go): a chat's hand-made, dirty checkout
// that no row had ever created was printed as
// `task 01a17dcd-…: own registered worktree kept: … — worktree is DIRTY …`,
// an assertion about THAT ROW's state which was false in every particular.
// The report's subject is decided per registration by GroundOwnership, so the
// signature needs no row id and must not grow one (an argument would restore the
// reading that caused the defect while the body still walks every repo).
// TestReclaimSelfFunctionsDeclareTheirSubject
// (own_worktree_self_declaration_test.go) pins both halves — this first line and
// the empty parameter list — so neither can quietly regress.
func ReclaimSelfOwnRegisteredWorktrees() []string {
	// NO mark is armed here, deliberately: this pass has TWO callers with
	// different standing — a producer's close-out (which must release its own
	// ground) and the ownership-independent SWEEP (which must not) — and giving
	// the close-out's permission to whoever calls the function would hand it to
	// the sweep as well. Measured the hard way while this change was written: the
	// pre-existing criterion `TestGuestRowRefusesToReconcileFromAGuestWorktree`
	// ("guest frees its OWN row of its own repo" → the directory must SURVIVE)
	// went RED through exactly that door. The close-out's mark is therefore armed
	// by the close-out SEAM (ReclaimOwnWorktreesAtCloseout) and by the
	// ledger-keyed pass, never by this one.
	repos := reclaimSelfOwnRepos()
	if len(repos) == 0 {
		return nil
	}
	// THE ROWS ARE READ ONCE, HERE, and handed to every repo's walk: they are the
	// SUBJECT set the ownership question is asked against
	// (own_worktree_ownership.go). An unreadable ledger claims nothing, which is
	// the fail-safe direction for a pass that removes directories — but it is
	// also the direction that makes every registration look unowned, so it is
	// REPORTED rather than rendered as a clean pass (Rule 15).
	verdictRows := reclaimSelfOwnVerdictRows()
	var out []string
	for _, repo := range repos {
		out = append(out, reclaimSelfOwnRepo(repo, verdictRows)...)
	}
	return out
}

// reclaimSelfOwnVerdictRows is the ONE read of the subject set the ownership
// resolver judges against: this process's own closing rows. A row with no
// recorded worktree is dropped here rather than at every call site — the
// ownership question is about grounds, and a placeholder row names none.
func reclaimSelfOwnVerdictRows() []ownGroundRow {
	rows, err := reclaimSelfRows()
	if err != nil {
		taskNotifyLog("%s: %s — the ownership of every registration is unverifiable this pass: %v",
			reclaimSelfOwnKeptMarker, ownGroundRowsUnreadablePhrase, err)
		return nil
	}
	return ownGroundRowsFrom(rows)
}

// reclaimSelfOwnMinAge is the floor below which a registration is never freed by
// the producer pass: a concurrent round of the same process may be mid-creation
// in it (see the doc above). It is short by design — the residue this pass
// closes is the worktree a FINISHED round left, and a run's own scratch
// checkout is hours old by the time its row closes out.
const reclaimSelfOwnMinAge = 5 * time.Minute

// reclaimSelfOwnMax bounds how many registrations ONE pass frees (the same
// runaway bound every sibling arm carries).
const reclaimSelfOwnMax = 8

// reclaimSelfOwnQuarantineAge is the age floor of the OUT-OF-ROOT DISPOSAL arm
// (reclaimSelfOwnOutOfRootDisposal) — deliberately LONGER than the ordinary
// release floor, because that arm discards the registration's working DIRECTORY
// rather than following the ordinary refusal path. A path a concurrent round is
// creating is minutes old by construction, so nothing this arm is for can be
// younger than a day; a day-old clean registration outside every declared root,
// owned by no live row and held by no process, is residue in the literal sense.
const reclaimSelfOwnQuarantineAge = 24 * time.Hour

// reclaimSelfOwnRootFSReason is the REASON an out-of-root registration on the `/`
// device is refused by the disposal arm, and it is Rule 22's ban stated as the
// ban it is. The refusal is deliberate and permanent: gopi does not repair a
// forbidden location by deleting what is in it.
const reclaimSelfOwnRootFSReason = "it shares the BOOT PARTITION's filesystem (device measured), which Rule 22 forbids for any gopi temp workspace — this pass will not delete under it (removing a `/`-device directory is how the 2026-09-11 exhaustion site was created, not how it is repaired)"

// reclaimSelfOwnOutOfRootHint is the operator's own next step for a registration
// this pass still refuses, so the refusal is actionable rather than terminal. It
// names the ordinary git verbs and states the rule that matters: the BRANCH is
// the deliverable and is never the thing to delete.
const reclaimSelfOwnOutOfRootHint = "finish it by hand where the directory lives: `git worktree remove <path>` (or delete the directory and `git worktree prune`) — the branch is the deliverable and is not to be deleted"

// reclaimSelfOwnFreedMarker / reclaimSelfOwnKeptMarker are the pass's two
// report phrases (one spelling each, so an operator greps one string).
const (
	// ReclaimSelfOwnFreedPrefix is the sentence the ownership-independent pass
	// prints for every directory it frees, ending in a space so a caller appends
	// the PATH (the E2E asserts on `prefix+path`: the fact that matters is that
	// the close-out reported THIS directory released, not which of the two
	// producer passes reached it first — the ledger-keyed pass names the branch,
	// this one names the directory).
	ReclaimSelfOwnFreedPrefix = "own registered worktree freed: "

	reclaimSelfOwnFreedMarker = "own registered worktree freed"
	reclaimSelfOwnKeptMarker  = "own registered worktree kept"

	// ReclaimSelfOwnUnownedLead is the LEAD every report line about a
	// registration that is NOT the ground of any of this process's closing rows
	// carries (own_worktree_ownership.go): it disclaims the SUBJECT first, and
	// the ordinary marker then states what happened to the directory.
	//
	// WHY THE SUBJECT IS THE DEFECT AND NOT THE FACT (measured 2026-10-27). The
	// pass used to print a chat's hand-made, dirty verification checkout under
	// whatever row was closing out, as `task <id>: own-worktree-release: own
	// registered worktree kept: … DIRTY …` — an assertion about THAT ROW's state
	// which was false in every particular. The observation must keep being
	// reported (Rule 15: a dirty directory is a human's decision, never a silent
	// one); what must not happen is a directory no row owns being recorded on a
	// row's account. So the fact stays, the row id goes: cmd/agent prints a line
	// carrying this lead WITHOUT a `task <id>:` prefix.
	//
	// EXPORTED because the two producer seams (cmd/agent's wrapper tail and the
	// worker round) are the ones that decide the prefix, and they must ask the
	// pass which kind of line they were handed instead of re-spelling a marker
	// (Rules 13/14).
	ReclaimSelfOwnUnownedLead = "unowned (not this close-out's ground): "

	// ReclaimSelfOwnOwnSubjectFmt is the sentence an OWN line carries — the
	// positive half of the same statement, and the half the neutral lead alone
	// cannot state: a reader must be able to tell that the line describes THIS
	// CLOSE-OUT'S OWN ROW, not merely that some row is closing out. It is
	// APPENDED to the ordinary line (never replacing it), so an operator's
	// existing `<marker>: <path>` match keeps working, and it is built from the
	// verdict's own TaskID + Evidence — the sentence names WHICH row and WHICH
	// test decided it, so a caller that never asked the ownership question
	// cannot produce it.
	//
	// The unowned lead and this sentence are the two halves of ONE decision
	// (own_worktree_ownership.go's reclaimSelfOwnLine), which is why they live
	// beside each other: changing one without the other is the shape that put a
	// row id on a directory no row owned.
	ReclaimSelfOwnOwnSubjectFmt = " — this close-out's row %s (%s)"

	// reclaimSelfOwnQuarantinedMarker is the disposal arm's report phrase: its
	// subject left for a DIFFERENT reason than the ladder's ordinary release (it
	// was outside every declared root and only the disposal arm could reach it),
	// and an operator reading the log must be able to tell the two releases apart.
	reclaimSelfOwnQuarantinedMarker = "own registered worktree quarantined"
)

// reclaimSelfOwnOutOfRootDisposal is the DISPOSAL arm for a registration that sits
// OUTSIDE EVERY DECLARED WORKTREE ROOT — the one refusal that was also the only
// sweep path, and therefore made such a registration permanently un-reclaimable.
//
// THE MEASURED CASE (2026-09-27, this host): a worker's own close-out printed, on
// every pass,
//
//	own registered worktree kept: /tmp/gopi-ctl-01a0ddea — outside every declared worktree root — not swept
//	own registered worktree kept: /tmp/gopi-prefix-fix — outside every declared worktree root — not swept
//
// Both are real registrations (`git worktree list`, detached HEAD, both BEHIND
// master — their work had already landed). The refusal was CORRECT: `/tmp` shares
// the root device, and Rule 22 forbids a gopi temp workspace there. The defect was
// that the same declaration was also the ONLY door, so the directory stayed
// forever and its branch stayed withheld (`scripts/safe-merge.sh` gate [c] refuses
// a branch whose worktree still stands) — a finished deliverable lost silently.
//
// THE DISPOSAL, and how it keeps the refusal intact:
//
//	① THE `/` DEVICE IS STILL REFUSED, and BEFORE any other rung: a path sharing
//	   the boot partition's device is REPORTED, never touched. THAT IS WHY THIS ARM
//	   CANNOT BE A WIDENING OF THE ROOT LIST — an out-of-root path on `/` keeps its
//	   permanent refusal (the two registrations above do), so the operator-visible
//	   policy is unchanged and the residue stays their call, with the command that
//	   finishes it named in the line.
//	② what leaves is the CHECKOUT, never the branch: the removal is the ordinary
//	   `git worktree remove` (no --force) and the ref is untouched. Every commit
//	   lives in the main repo's object store; a registration is a disposable view
//	   of it, which is what makes discarding the directory non-destructive.
//	③ the age floor is reclaimSelfOwnQuarantineAge, LONGER than the ordinary
//	   release floor — a path a concurrent round is creating is minutes old, and
//	   this is the arm that discards rather than refuses.
//	④ every other rung still decides: clean, no live holder, and no live row
//	   owning it (asked by the caller's ladder for the dregs, and by the checks
//	   below for this arm).
//
// It returns (line, freed, handled): handled=false means the registration is
// inside a declared root and the ordinary ladder must decide it.
func reclaimSelfOwnOutOfRootDisposal(reg worktreeRegistration, repo string, roots []worktreeroots.Root, holders scratch.HolderScan) (string, bool, bool) {
	if _, ok := worktreeroots.PermitsAny(roots, reg.Path); ok {
		return "", false, false // inside a declared root: not this arm's business
	}
	kept := func(reason string) (string, bool, bool) {
		return reclaimSelfOwnKeptMarker + ": " + reg.Path + " — " + reason, false, true
	}
	// ① the `/` device: the ban is the REASON for the refusal, and it comes first.
	if err := scratch.AssertPathOffRootFS(reg.Path); err != nil {
		return kept(reclaimSelfOwnRootFSReason + "; " + reclaimSelfOwnOutOfRootHint)
	}
	// ③ age.
	created, err := worktreeSweepCreatedAt(reg.Path)
	if err != nil {
		return kept("outside every declared worktree root and its age could not be measured (" + err.Error() +
			") — keeping (an unmeasurable registration is never removed)")
	}
	if age := time.Since(created); age < reclaimSelfOwnQuarantineAge {
		return kept("outside every declared worktree root and younger than the " + reclaimSelfOwnQuarantineAge.String() +
			" quarantine floor (" + age.Round(time.Second).String() + ") — a concurrent round may be creating it")
	}
	// ④a a live holder — the shared scan, consulted exactly as the ladder consults it.
	if why := scratch.HolderVeto(reg.Path, holders); why != "" {
		if _, orphanWhy := finishCleanupOrphanCwdInheritorReason(reg.Path); orphanWhy == "" {
			return kept(why)
		}
	}
	// ④b cleanliness — an out-of-root checkout with uncommitted work in it is a
	// human's decision, and this pass never makes that one.
	porcelain, serr := reclaimGitStatus(reg.Path)
	if serr != nil {
		return kept("outside every declared worktree root and its status is unreadable: " + serr.Error())
	}
	if why := reclaimDirtyReason(porcelain); why != "" {
		return kept("outside every declared worktree root and it carries " + why + " — a human's decision, kept")
	}
	// The DISPOSAL arm asks the SHARED checklist before it frees anything — the
	// branch's lineage, whether the directory is a guest in a peer's row, and the
	// unmerged-commit question. It runs here, immediately before the audit record,
	// so a refusal leaves the registration exactly as the ordinary ladder does (kept,
	// reported, nothing touched). No exemption is claimed: an out-of-root directory
	// is not automatically this pass's own ground, and the arm that discards is the
	// last one that should skip the question.
	if why := reclaimGroundChecklistVerdict(repo, reg.Path, reg.Branch); why != "" {
		return kept("outside every declared worktree root and the removal ground checklist refused it (" + why + ")")
	}
	// The audit record BEFORE the removal (package removalaudit), naming the rung
	// that decided: an out-of-root, off-`/`, clean, unheld registration of this
	// run's own repo.
	removalaudit.WorktreeRemove(removalaudit.ArmSelfOwnSweep, repo, reg.Path, reg.Branch, "",
		"out-of-root disposal: the registration is outside every declared worktree root (so the ordinary ladder refused it forever), off the / device, clean, unheld and past the quarantine floor; the branch "+orDashBranch(reg.Branch)+" is untouched")
	if rerr := reclaimGitRemove(repo, reg.Path); rerr != nil {
		return kept("outside every declared worktree root and removal refused by git (kept, never forced): " + rerr.Error())
	}
	// ② and ⑤: the checkout is gone, the branch is not.
	return reclaimSelfOwnQuarantinedMarker + ": " + reg.Path +
		" (outside every declared worktree root and off the / device: the working directory is gone, branch " +
		orDashBranch(reg.Branch) + " untouched)", true, true
}

// reclaimSelfOwnRepos resolves the repos this process worked in: the ledger
// repos of the rows it claims (already the producer pass's own evidence set),
// unioned with the repo the process was started in.
//
// THE CWD HALF IS CONDITIONAL ON LEDGER AUTHORITY (the defect this guard
// closes, measured 2026-09-16). The union was unconditional, so a run whose
// LEDGER is not the authority for the repository it stands in — the classic
// `gopi worker <task-id>` launched from ~/work/GoPi with a sandboxed
// GopiHome/GOPI_MEMORY_DB — resolved the live repository from its CWD alone and
// freed whatever clean, unheld registration older than the floor it found
// there, including another task's live, unmerged work (the directory is
// removed, the branch survives — which is why the loss looked random).
//
// The pass may only sweep a repo whose OWNERS it can read: a repository is
// enrolled by the cwd ONLY when the ledger this process reads demonstrably
// covers it — i.e. it names at least one row carrying THAT repo. The evidence
// is a second ledger read (`reclaimSelfOwnLedgerNamedRepos`), answering the
// same question from the other side: the claim-stamp rows answer "does this
// process still owe a release?", while the repo-naming rows answer "is this
// ledger authoritative for this repo?". An empty/absent ledger names no repo,
// so the cwd half contributes nothing. Nothing else changes: a repo already
// enrolled by the ledger half is swept exactly as before. A ledger SCAN
// FAILURE enrolls nothing from the cwd either: an unreadable ledger proves
// neither ownership nor authority, and the safe direction for a pass that
// REMOVES directories is to keep.
//
// An entry therefore comes from a claim stamp this process owns, or from the
// cwd's own git AND the repo-naming evidence the ledger carries. It never
// GUESSES a repo.
func reclaimSelfOwnRepos() []string {
	seen := map[string]bool{}
	var repos []string
	add := func(repo string) {
		repo = strings.TrimSpace(repo)
		if repo == "" {
			return
		}
		repo = filepath.Clean(repo)
		if seen[repo] {
			return
		}
		seen[repo] = true
		repos = append(repos, repo)
	}
	var named []string
	if rows, err := reclaimSelfRows(); err == nil {
		for _, r := range rows {
			add(r.Repo)
		}
	} else {
		// Rule 15: an unreadable ledger is reported, never rendered as "no
		// repos to sweep" — and the cwd half is refused with its own NAMED
		// line, so a withheld sweep is never silent either.
		taskNotifyLog("%s: ledger scan failed: %v", reclaimSelfOwnKeptMarker, err)
		taskNotifyLog("%s: cwd repo not enrolled — the ledger is unreadable, so its ownership of any repo is unverifiable",
			reclaimSelfOwnKeptMarker)
		return repos
	}
	named, lerr := reclaimSelfOwnLedgerNamedRepos()
	if lerr != nil {
		// The ledger named no repo (a sandbox ledger, or one that could not be
		// read at all): either way nothing authorises the cwd's repo.
		taskNotifyLog("%s: cwd repo not enrolled — the ledger named no repo (%v): a repository this ledger does not cover is never swept",
			reclaimSelfOwnKeptMarker, lerr)
		return repos
	}
	cwd, err := os.Getwd()
	if err != nil {
		return repos
	}
	repo := reclaimSelfOwnRepoOf(cwd)
	if repo == "" {
		return repos
	}
	// The GUEST enrolment refusal (path B, epic 01a0c2b5): the pass frees
	// whatever clean, unheld registration it finds in the CWD's repo, so a
	// process standing in a LINKED worktree of that repo must not enrol it from
	// its cwd at all — the repo it merely stands in is not the repo whose
	// registrations it owns. This is the CWD half only: a repo the LEDGER half
	// already enrolled is untouched (those are rows this process claims, and a
	// guest's own row release is guarded separately in reclaimSelfRelease, so
	// that withholding the whole repo cannot silently retire the producer leg).
	if why := guestRowOwnRefusal(repo); why != "" {
		taskNotifyLog("%s: cwd repo %s not enrolled — %s", reclaimSelfOwnKeptMarker, repo, why)
		return repos
	}
	if !reclaimSelfOwnRepoNamed(named, repo) {
		// Rule 15: the pass's own legitimate purpose is unchanged — it still
		// frees the run's unowned registrations in the repos its ledger names.
		taskNotifyLog("%s: cwd repo %s not enrolled — the ledger this process reads names no row carrying it (%d repo(s) named), so this process cannot prove it owns it",
			reclaimSelfOwnKeptMarker, repo, len(named))
		return repos
	}
	add(repo)
	return repos
}

// reclaimSelfOwnLedgerNamedRepos returns the repos named by the ledger THIS
// process reads — the authority evidence the cwd half must produce before it
// may enrol a repository (see reclaimSelfOwnRepos). A ledger with no rows
// naming a repo returns an empty, non-error answer: "this ledger covers
// nothing", which is exactly the sandbox case the guard closes.
func reclaimSelfOwnLedgerNamedRepos() ([]string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), finishCleanupDBTimeout)
	defer cancel()
	db, err := openTasksDB(ctx, memoryDBPath())
	if err != nil {
		return nil, err
	}
	defer db.Close()
	rows, err := db.QueryContext(ctx,
		`SELECT DISTINCT deliverable FROM tasks WHERE COALESCE(deliverable,'') <> ''`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	seen := map[string]bool{}
	var repos []string
	for rows.Next() {
		var deliverable string
		if err := rows.Scan(&deliverable); err != nil {
			return nil, err
		}
		repo := finishCleanupParseDeliverable(deliverable).Repo
		repo = scratch.CanonicalPath(repo)
		if repo == "" || seen[repo] {
			continue
		}
		seen[repo] = true
		repos = append(repos, repo)
	}
	return repos, rows.Err()
}

// reclaimSelfOwnRepoNamed reports whether one of the ledger's named repos IS
// repo. Both sides are compared as CANONICAL paths (scratch.CanonicalPath —
// the shared renderer, Rule 14), so the comparison is the same one every
// sibling arm makes between a recorded path and a path read off the disk.
func reclaimSelfOwnRepoNamed(named []string, repo string) bool {
	want := scratch.CanonicalPath(repo)
	if want == "" {
		return false
	}
	for _, have := range named {
		if scratch.CanonicalPath(have) == want {
			return true
		}
	}
	return false
}

// reclaimSelfOwnRepoOf answers the repo a directory belongs to (the main
// checkout path git reports), or "" when the directory is not in a repo. The
// main checkout is the FIRST porcelain record — the same reading the rest of
// this package uses (taskWorktreeOwningDir).
func reclaimSelfOwnRepoOf(dir string) string {
	ctx, cancel := context.WithTimeout(context.Background(), reclaimGitTimeout)
	defer cancel()
	// PARSED porcelain: stdout only, stderr is a diagnostic (a `GIT_TRACE=1`
	// line would become a bogus worktree path).
	out, err := finishCleanupGitParsedStdout(ctx, dir, reclaimGitTimeout,
		finishCleanupWorktreeVerb, finishCleanupWorktreeListVerb, finishCleanupWorktreeListFlag)
	if err != nil {
		return ""
	}
	for _, line := range strings.Split(out, "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), finishCleanupPorcelainWorktreePrefix) {
			return strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(line), finishCleanupPorcelainWorktreePrefix))
		}
	}
	return ""
}

// reclaimSelfOwnRepo frees this process's own unowned registrations in one repo.
//
// rows is the SUBJECT set the ownership question is asked against
// (own_worktree_ownership.go): every line this walk produces is rendered
// knowing whether the registration is the ground of one of those rows, and a
// registration that is not carries the NEUTRAL lead instead of a row's account.
// The free/keep DECISIONS are unchanged — ownership classifies the report and
// answers `own`, it never widens or narrows what may be removed.
func reclaimSelfOwnRepo(repo string, rows []ownGroundRow) []string {
	if rows == nil {
		// ONE derivation of the subject set, so a caller that has the rows in
		// hand passes them and a caller that does not gets the pass's own read —
		// never "no subject" (which would report every ground as unowned).
		rows = reclaimSelfOwnVerdictRows()
	}
	regs, err := worktreeSweepRegistrations(repo)
	if err != nil {
		return []string{reclaimSelfOwnKeptMarker + ": " + repo + " — registrations unreadable: " + err.Error()}
	}
	holders := scratch.ScanHolders()
	roots := worktreeSweepRoots(repo)
	var out []string
	freed := 0
	for _, reg := range regs {
		if reg.Main || reg.Bare || reg.Prunable || reg.Locked {
			continue // never this pass's business (the sweeper's rung ①)
		}
		// THE OWNERSHIP QUESTION, ASKED FIRST AND ASKED ONCE (the 2026-10-27
		// defect): is this the ground of one of THIS process's closing rows?
		// It decides the REPORT'S SUBJECT, never whether the directory may be
		// removed — the guard ladder below keeps every safety rung it had.
		own := GroundOwnership(worktreeRegistration{Path: reg.Path, Branch: reg.Branch, Repo: repo}, rows)
		if freed >= reclaimSelfOwnMax {
			out = append(out, reclaimSelfOwnLine(own,
				fmt.Sprintf("%s: %s — run cap reached (%d), remaining registrations are for the next pass",
					reclaimSelfOwnKeptMarker, reg.Path, reclaimSelfOwnMax)))
			break
		}
		// THE OUT-OF-ROOT DISPOSAL ARM GOES FIRST, and only for what it claims
		// (outside every declared root): inside a declared root it declines
		// (handled=false) and the ordinary ladder below decides, unchanged.
		if note, disposed, handled := reclaimSelfOwnOutOfRootDisposal(reg, repo, roots, holders); handled {
			if disposed {
				freed++
			}
			out = append(out, reclaimSelfOwnLine(own, note))
			continue
		}
		if why := reclaimSelfOwnWhy(reg, repo, holders); why != "" {
			out = append(out, reclaimSelfOwnLine(own, reclaimSelfOwnKeptMarker+": "+reg.Path+" — "+why))
			continue
		}
		// The DETERMINISTIC CHECKLIST, asked here TOO: reclaimSelfOwnWhy answers
		// the REGISTRATION's questions (age / holder / own row) and never the
		// ground's or the branch's, so this arm could otherwise free a checkout
		// the run stands in — the same defect class, through the other door.
		if why := reclaimGroundChecklistVerdict(repo, reg.Path, reg.Branch); why != "" {
			out = append(out, reclaimSelfOwnLine(own, reclaimSelfOwnKeptMarker+": "+reg.Path+" — "+why))
			continue
		}
		// Audit record BEFORE the removal (package removalaudit): an unowned
		// registration of this run's own repo, past the guard ladder
		// (reclaimSelfOwnWhy's empty verdict is the deciding evidence).
		removalaudit.WorktreeRemove(removalaudit.ArmSelfOwnSweep, repo, reg.Path, reg.Branch, "",
			"unowned registration of this run's own repo; "+removalaudit.EvidenceChannelLiveSession+
				" and the live-holder scan did not refuse; checklist "+reclaimGoalChecklistSummary)
		// The removal is the ordinary one (never --force): git re-checks the
		// state itself, so a guard this pass missed still cannot destroy work.
		if rerr := reclaimGitRemove(repo, reg.Path); rerr != nil {
			out = append(out, reclaimSelfOwnLine(own,
				reclaimSelfOwnKeptMarker+": "+reg.Path+" — removal refused by git (kept, never forced): "+rerr.Error()))
			continue
		}
		freed++
		out = append(out, reclaimSelfOwnLine(own, ReclaimSelfOwnFreedPrefix+reg.Path+
			" ("+reclaimSelfOwnFreedClause(own)+"; branch "+orDashBranch(reg.Branch)+" untouched)"))
	}
	return out
}

// reclaimSelfOwnFreedClause is the parenthesis a freed registration's line
// carries, and it follows the ownership verdict because the two statements
// contradict each other otherwise: a directory the ownership resolver has just
// answered `own` is NOT one "no ledger row owned". The clause is a sentence a
// reader acts on, so it is derived, never hard-coded (the 2026-10-27 fix made the
// old single spelling false for half the lines that carried it).
func reclaimSelfOwnFreedClause(v ownGroundVerdict) string {
	if v.Own {
		return "the ground this close-out's row records; the row is terminal and " +
			removalaudit.EvidenceChannelLiveSession + " found no live holder"
	}
	return "a worktree of this run's repo that no ledger row owned"
}

// reclaimSelfOwnWhy is the guard ladder's verdict for one registration: "" when
// it may be freed, else the NAMED reason it is kept.
func reclaimSelfOwnWhy(reg worktreeRegistration, repo string, holders scratch.HolderScan) string {
	// ① age — a concurrent round's fresh checkout is not residue.
	//
	// The floor does not apply to the CLOSE-OUT's own ground (reclaim_own_ground.go):
	// this registration is the directory of a row THIS process is releasing at
	// this instant, so the hazard the floor protects against ("a concurrent round
	// may be creating it") is not a hypothesis here but the measured, current
	// state of the very run doing the releasing — its ground IS young, and
	// vetoing it strands the deliverable of the run that just finished (measured
	// on master f4451b9d: `own registered worktree kept: … — younger than the 5m0s
	// floor (3s)` for the ground of the row whose run had just completed). Every
	// other registration keeps the floor; the exemption is scoped by the mark,
	// never by loosening reclaimSelfOwnMinAge.
	if !SelfGroundReleasePendingFor(reg.Path) {
		created, err := worktreeSweepCreatedAt(reg.Path)
		if err != nil {
			return "its age could not be measured (" + err.Error() + ") — keeping (an unmeasurable registration is never removed)"
		}
		if age := time.Since(created); age < reclaimSelfOwnMinAge {
			return "younger than the " + reclaimSelfOwnMinAge.String() + " floor (" + age.Round(time.Second).String() + ") — a concurrent round may be creating it"
		}
	}
	// ② a live holder — the shared scan (one implementation of "in use").
	//
	// NOT EVERY LIVE HOLDER IS A HOLDER (epic worktree-not-released, measured
	// 2026-09-24): this arm used to read the raw scan alone and keep the
	// registration forever, while its LEDGER-keyed sibling asks the orphan
	// ladder on refusal (task_reclaim.go, the identical sentence one file over).
	// So a finished round whose only occupant was an ORPHANED cwd-inheritor —
	// a leaked executor child / server whose launcher is gone, reparented to
	// init, in a session that holds no live gopi process — was reported as
	// "held by a live process" on every pass, by the one arm whose whole job is
	// freeing what nothing owns. The measured shape: three `gopi` processes
	// leaked out of finished rounds (ppid=1, their recorded wrapper pid dead,
	// cwd pinned in a worktree that had already been deleted under them).
	//
	// The ladder is the SAME implementation the ledger arm calls (Rules 13/14),
	// and it is consulted ONLY on refusal, ONLY to distinguish "a genuine holder
	// stands here" (the veto then STANDS, with this rung's own sentence) from
	// "nothing but inherited cwd". Its rungs keep the asker's own live child a
	// holder (not reparented to init ⇒ HOLD), so this widening cannot free a
	// directory a live run of this very process is working in.
	if why := scratch.HolderVeto(reg.Path, holders); why != "" {
		if _, orphanWhy := finishCleanupOrphanCwdInheritorReason(reg.Path); orphanWhy == "" {
			return why
		}
	}
	// ③ cleanliness — the shared two-state reading.
	porcelain, serr := reclaimGitStatus(reg.Path)
	if serr != nil {
		return "status unreadable: " + serr.Error()
	}
	if why := reclaimDirtyReason(porcelain); why != "" {
		return why
	}
	// ④ the ledger — a LIVE row naming this path owns it, whatever the holder
	// scan saw. This is the veto that makes an ownership-independent pass safe
	// against a SIBLING session: a live task legitimately idles between its
	// rounds (a build in the background, an agent thinking), and for exactly
	// that window its worktree is clean and hours old — indistinguishable from
	// residue by every other rung. The evidence is the shared ownership reader
	// (worktreeSweepTaskRows), and the question is the one the sweeper's rung ③
	// asks; the row-entry predicate is the only difference (see the doc above).
	ownerRows, ownerErr := worktreeSweepTaskRows(context.Background())
	if ownerErr != nil {
		return "the task ledger is unreadable (" + ownerErr.Error() + ") — ownership unverifiable, keeping"
	}
	for _, row := range ownerRows {
		if row.terminal() {
			continue // history never protects a registration
		}
		if sameWorkingPath(row.Worktree, reg.Path) {
			return "owned by live task " + row.TaskID + " (" + row.Status + ")"
		}
	}
	// ⑤ the registration must sit inside a DECLARED root (the worktreeroots
	// policy the sweeper enforces) — the main checkout, a locked entry and a
	// bare registration are already excluded above, and anything outside the
	// operator's declared roots is not this pass's to remove either.
	//
	// THE CLOSE-OUT'S OWN GROUND IS EXEMPT (reclaim_own_ground.go), and this is
	// the ONE rung whose subject is a POLICY rather than a hazard: the roots
	// answer "may an ownership-independent pass reach into this directory?", and
	// a directory the operator launched the run in (or that a row of THIS
	// process records) is inside the run's own working set by definition — which
	// is the measured case, a task worktree beside its repository under
	// t.TempDir(), inside no declared root and therefore stranded forever. Every
	// other rung above still decides: the ground must be clean, unheld by any
	// foreign process, and owned by no live row.
	// A registration OUTSIDE every declared root never reaches this line in the
	// ordinary pass: reclaimSelfOwnOutOfRootDisposal claims it first and decides it
	// there (the `/` device refused, else age/holder/cleanliness and a REPORTED
	// disposal). This rung stays as it is — it is the policy statement, and it is
	// what the disposal arm asks to decide WHICH registrations are its own.
	if !SelfGroundReleasePendingFor(reg.Path) && !reclaimSelfOwnInRoot(reg.Path, worktreeSweepRoots(repo)) {
		return "outside every declared worktree root — not swept"
	}
	return ""
}

// reclaimSelfOwnInRoot reports whether path sits inside one of the repo's
// declared roots (the policy the sweeper enforces — one implementation, so this
// pass can never be more permissive than the operator's declaration).
func reclaimSelfOwnInRoot(path string, roots []worktreeroots.Root) bool {
	_, ok := worktreeroots.PermitsAny(roots, path)
	return ok
}

// orDashBranch renders a registration's branch for a report line (a detached
// registration has none).
func orDashBranch(branch string) string {
	if strings.TrimSpace(branch) == "" {
		return "detached"
	}
	return branch
}

// Self = this PROCESS, not this ROW.
//
// ReclaimSelfOwnRegisteredWorktreesWithin is the bounded wrapper (the same
// contract as ReclaimOwnFinishedWorktreesWithin): a pass that cannot finish in
// time returns a NAMED note instead of blocking the close-out that called it.
// It takes no argument for the same reason its subject does: the scan is THIS
// PROCESS's, the report's subject is decided per registration
// (own_worktree_ownership.go), and a timeout note about a registration no row
// owns must not arrive wearing a row id.
func ReclaimSelfOwnRegisteredWorktreesWithin() []string {
	done := make(chan []string, 1)
	go func() { done <- ReclaimSelfOwnRegisteredWorktrees() }()
	timer := time.NewTimer(reclaimSelfBudget)
	defer timer.Stop()
	select {
	case lines := <-done:
		return lines
	case <-timer.C:
		note := fmt.Sprintf("%s: %s — this close-out continues without it", reclaimSelfOwnKeptMarker, reclaimSelfGitTimeoutNote)
		taskNotifyLog("%s", note)
		return []string{note}
	}
}

// ReclaimOwnFinishedWorktreesWithin runs the producer release under a wall
// budget: a pass that cannot finish in time returns a NAMED note instead of
// blocking the close-out that called it. Returns the lines to report.
// ReclaimOwnWorktreesAtCloseoutWithin is ReclaimOwnWorktreesAtCloseout under the
// same budget guard its two halves already carry (a close-out pass that hangs
// must not hang the round it is closing out): the work runs on its own
// goroutine, the caller keeps whichever finishes first, and an overrun is
// REPORTED rather than rendered as "nothing to release".
func ReclaimOwnWorktreesAtCloseoutWithin() []string {
	done := make(chan []string, 1)
	go func() { done <- ReclaimOwnWorktreesAtCloseout() }()
	timer := time.NewTimer(reclaimSelfBudget)
	defer timer.Stop()
	select {
	case lines := <-done:
		reclaimSelfLog(lines)
		return lines
	case <-timer.C:
		return []string{reclaimSelfOwnKeptMarker + ": the close-out release pass exceeded " + reclaimSelfBudget.String() + " — reported, not retried in this round"}
	}
}

func ReclaimOwnFinishedWorktreesWithin() []string {
	done := make(chan []string, 1)
	go func() { done <- ReclaimOwnFinishedWorktrees() }()
	// The budget covers the pass; the dispatcher keeps whichever finishes
	// first (the note is only ever a fallback, never a replacement).
	timer := time.NewTimer(reclaimSelfBudget)
	defer timer.Stop()
	select {
	case lines := <-done:
		reclaimSelfLog(lines)
		return lines
	case <-timer.C:
		// Report the overrun; the pass's own line (when it lands) still goes
		// to the caller through its own path, so nothing is swallowed.
		note := fmt.Sprintf("%s: %s — this close-out continues without it", reclaimSelfRefusedMarker, reclaimSelfGitTimeoutNote)
		taskNotifyLog("%s", note)
		return []string{note}
	}
}
