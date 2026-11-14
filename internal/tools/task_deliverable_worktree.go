// Deliverable.worktree backfill at worktree-CREATION time (bugfix epic
// 2026-09-03, the operator's workflow-gap finding — the M4.8 information-closure
// loop's missing worktree half). A task row carries repo at submit; the
// executor's worktree (PROJECT.md Rule 6a — the branch + linked worktree the
// actual work happens in) was recorded NOWHERE until close-out, so:
//
//   - `task show` mid-run showed repo but no worktree/branch — the user
//     cannot tell where the task is actually working;
//   - resume restored the session workdir (the MAIN repo) — a mid-worktree
//     crash resumed the WRONG context, blind to the worktree's commits;
//   - the recycle logic (server-side finish-cleanup, MergedCleanupSweep)
//     could not attribute the worktree to a live row → ownerless worktree
//     leaks until manual cleanup, and a freshly-created worktree whose
//     branch is still at master tip got REAPED as "merged" because no live
//     row referenced it.
//
// The executor creates its worktree by RUNNING git (bash tool — there is no
// Go-side `git worktree add` site), so the backfill is a DETECTION seam:
// after every executor bash call, resolve where the executor now works
// (cwd or an explicit workdir) and, when it is inside a linked worktree of
// the task's repo, stamp deliverable.worktree + deliverable.branch into the
// WRAPPER row (GOPI_TASK_WRAPPER_ID — the repo-bearing row the task run
// wrapper claimed). This is information closure at CREATION time: the row
// carries repo (submit) + worktree + branch (creation) while the task runs,
// not only at the end.
//
// Best-effort by contract, like every other side-channel backfill: a
// failure (no wrapper row, non-repo, main-checkout cwd, DB error) logs and
// returns — it never blocks or fails the executor's bash round.

package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/nickhuang99/gopi/internal/config"
	"github.com/nickhuang99/gopi/internal/worktreeroots"
)

// taskWorktreeBackfillTimeout caps every git invocation resolving the
// executor's worktree (a wedged repo must not stall a bash round).
const taskWorktreeBackfillTimeout = 10 * time.Second

// Off-policy worktree location (PROJECT.md Rule 6a): the protocol puts a linked
// worktree under the home workspace (~/workspace/gopi-<task>). A worktree
// created anywhere else — /tmp, /dev/shm, /var/tmp — is a policy violation
// and, measurably, the single biggest source of worktree leakage: nothing
// sweeps those roots (the server finish-cleanup and MergedCleanupSweep both
// work off the ledger + the home workspace), the tmpfs copy dies on reboot,
// and the ~65MB checkout sits invisible until an operator runs
// `git worktree list` by hand (2026-09-13: 66 registered worktrees, 46 of
// them under /tmp or /dev/shm).
//
// The seam below therefore also acts as the DETECTOR of that violation: the
// moment a wrapper row's worktree resolves outside the workspace root, one
// line is logged and one synthetic notify event fires, naming the row, the
// branch and the path. Detection is deliberately fail-open — an unreadable
// HOME or a non-worktree path never blocks the bash round (best-effort
// side-channel contract, same as the backfill itself).
const (
	// taskWorkspaceDirName is an ALIAS of worktreeroots.WorkspaceDirName
	// (Rule 14): the directory is the default worktree root, so its value has
	// one definition and this private name exists only so the call sites below
	// read in the vocabulary they were written in. The literal gate
	// (internal/standards) fails any second definition of it.
	taskWorkspaceDirName = worktreeroots.WorkspaceDirName
	taskOffPolicyEvent   = "worktree-off-policy"
)

// taskOffPolicyWorktreePath reports whether wt sits outside the home
// workspace root (PROJECT.md Rule 6a) and what to print as the offender. The
// workspace root itself is derived from HOME because that is the same anchor
// the protocol text uses (`~/workspace/gopi-<task>`); no new env knob is
// introduced (Rule 2) and every literal above is a named constant (Rule 1b).
func taskOffPolicyWorktreePath(wt string) (string, bool) {
	wt = strings.TrimSpace(wt)
	if wt == "" {
		return "", false
	}
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return "", false // no anchor to judge against — never guess
	}
	root := filepath.Join(home, taskWorkspaceDirName)
	rel, err := filepath.Rel(root, wt)
	if err != nil {
		return "", false
	}
	// Inside the workspace root (rel has no ".." escape) → policy-compliant.
	if rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", false
	}
	return wt, true
}

// taskNotifyOffPolicyWorktree records an off-policy worktree location on the
// row's behalf. Reporting only — the tree is never moved or removed here; a
// move is the owner's call and the reclaim sweeps must still find it by the
// row's recorded path.
func taskNotifyOffPolicyWorktree(taskID, wt, branch string) {
	summary := taskOffPolicyEvent + ": worktree outside the home workspace (GOPI.md Rule 6a): " + wt +
		" branch=" + branch + " task=" + taskID
	taskNotifyLog("task %s: %s", taskID, summary)
	NotifySyntheticEvent(taskOffPolicyEvent, taskID, branch, summary)
}

// TaskWorktreeBackfillRow resolves the ledger row whose deliverable a
// creation-time worktree backfill must stamp. wrapperID is the row the CALLER
// knows it executes under ("" when it knows none): the `task run`/worker
// wrapper exports it as GOPI_TASK_WRAPPER_ID pre-spawn, and the harness's
// auto-registration hook holds it in autoTaskRowID for the run it registered
// itself (a bare prompt / chat — there is no wrapper row).
//
// WHY IT IS NOT JUST os.Getenv(TaskWrapperEnv) (epic raw-prompt-worktree-
// release, measured 2026-09-14): the bare-prompt face has no wrapper row AND
// no wrapper env, so a seam keyed on that env alone is blind to it — the
// executor could create a Rule-6 worktree and commit in it, and the row that
// DID exist for that same run stayed empty. The env remains the fallback so a
// caller that cannot name its row keeps today's behaviour.
func TaskWorktreeBackfillRow(wrapperID string) string {
	if id := strings.TrimSpace(wrapperID); id != "" {
		return id
	}
	return strings.TrimSpace(os.Getenv(TaskWrapperEnv))
}

// TaskDeliverableWorktree resolves the linked worktree the executor
// currently works in, relative to the task's repo, and backfills
// deliverable.worktree + deliverable.branch into the row when the caller
// knows which task row owns the execution. Exported as the single seam the
// executor tool path calls after each bash round.
//
// workdir is the directory the executor's LAST command ran in (the bash
// tool's cwd/workdir; "" = resolve the process cwd). repo is the task row's
// deliverable.repo (the main checkout the worktree belongs to). The
// detection: when workdir is inside a LINKED worktree of repo (not the main
// checkout) whose checked-out branch is non-exempt, that IS the executor's
// Rule-6 worktree.
//
// Returns (worktree, branch) resolved — "" when the executor is not inside
// a linked worktree of repo (main checkout / unrelated dir) — and reports
// whether a backfill was written.
func TaskDeliverableWorktree(workdir, repo string) (wt, branch string, backfilled bool) {
	return TaskDeliverableWorktreeForRow(TaskWorktreeBackfillRow(""), workdir, repo)
}

// taskWorktreeBorrowedGroundLineFmt is the ONE spelling of the line the
// CREATION seam emits when it refuses to record a ground another row already
// owns (Rule 1b/15). It names the row and the measured reason (the directory or
// branch, and the row that already names it) — the two facts an operator needs
// to tell "this run took its own ground" from "this run merely stood in
// somebody else's".
const taskWorktreeBorrowedGroundLineFmt = "task %s: creation-time ground stamp REFUSED — %s (a directory or branch another row already names is that row's; a borrowed ground is never recorded)"

// taskWorktreeDeadOwnerGroundLineFmt is the ONE spelling of the line the
// creation seam emits when the ONE-OWNER rule does NOT fire because the row
// that names the ground is HISTORY: its life is over (a terminal status) and
// its holder is provably gone on this host. %s = the axis fact (the directory
// or branch plus the naming row), %s = the dead-owner reason in full.
//
// It is a REPORT, not a refusal (Rule 15): the ground IS recorded, and a
// reader must be able to tell "this run took its own ground" from "this run
// re-recorded a directory a dead row still names" — the second is the state
// the delivery-time recycle exists to resolve (see the decision note below).
const taskWorktreeDeadOwnerGroundLineFmt = "task %s: creation-time ground stamp: %s — the binding is history, not an owner, so the ground IS recorded"

// THE DECISION: a DEAD OWNER is not a borrow (2026-10-03).
//
// taskCreationGroundBorrowed refuses to record a ground another row NAMES
// (the 2026-10-03 borrowed-ground leak, row 01a10025). The DELIVERY-time
// recycle refuses to let a dead row BLOCK a directory's release
// (finishCleanupForeignOwnerReason: only a LIVE holder blocks; a row whose
// life is over and whose holder is provably gone is history — the 2026-09-12
// stranded-worktree fix). The two landed rules met on 2026-10-03 in the shape
// the recycle was BUILT for: a delivered row taking the ground a
// cancelled/completed sibling still names.
//
// THE RECONCILIATION, decided once and stated here so no seam re-decides it:
//
//	ONE GROUND, ONE LIVE OWNER — the *live* owner wins, and only a live owner
//	blocks. A row whose status is terminal AND whose holder this host can
//	PROVE is gone holds nothing (the family's own verdict:
//	finishCleanupOwnerRowLive, the ONE holder predicate — Rule 14), so the
//	directory it names is free ground, and a run taking free ground is not a
//	borrower. Recording it lets the delivered row resolve its own deliverable
//	(the merger's attribution, the finish chain's gate ①) instead of losing
//	its provenance for good.
//
// WHY NOT the other direction (leave the refusal, rewrite the fixture): the
// refusal would be right only if the dead row still OWNED the directory — but
// the whole point of the reconcile is that this exact state (a terminal row
// parked on a directory its holder abandoned) must end in the directory being
// REUSED and eventually released. A fixture rewritten around the refusal
// would leave the seam unable to record the ground for a live run, i.e. the
// production shape would keep a row repo-only while it works in a real
// worktree: the delivered row's own finish chain would then depend on gate ②
// alone (declared-vs-resolved has nothing to bind against), which is exactly
// the mis-attribution the deliverable exists to prevent.
//
// THE TWO REMAINING REFUSALS ARE UNTOUCHED: a row that is still LIVE (any
// live status, or an unknowable foreign claim) keeps its ground, and an
// UNPROVABLE ledger keeps it too (unreadable/truncated scan ⇒ refuse ⇒ the
// naming row holds — fail-closed, Rule 15). Only a MEASURED dead owner is
// history.
//
// The reader asks the SAME row loader the refuse path asks (taskGroundClaimRows)
// and the SAME expiry predicate the recycle asks (finishCleanupOwnerRowLive),
// so the creation seam and the delivery arm can never disagree about whether a
// naming row still holds its ground (Rules 13/14 — one predicate per axis).
//
// WHAT THE SEAM IS FOR (measured 2026-10-03, row 01a10025): the seam's probe
// (taskGroundOwningDir) answers "which ground does this directory belong to",
// never "does another row already own it". A run that merely STANDS in another
// run's worktree therefore bound that worktree AND its branch onto its own row.
// The case: a bare `gopi replay` (run_method=auto, pid 611399) whose process
// stood in task 01a10016's ground wrote deliverable = {branch: task/01a10016…,
// worktree: /…/gopi-01a10016…} onto a row of its own — minting a SECOND owner of
// a directory the ledger had already attributed, in the direction the
// deliverable is READ (the reclaim arms keep a named worktree alive for the row
// that names it). Ownership then lied in both directions: the borrower blocked
// its owner's recycle and was itself blocked by it — the 2026-09-12 mutual-lock
// family, one seam over.
//
// FAIL-CLOSED (Rule 15), exactly as the claim seam: an unprovable scan is a
// REFUSAL, never a proceed. "No owner could be PROVEN" is not "there is no
// owner", and the cost of guessing wrong is two rows owning one directory.
func taskCreationGroundBorrowed(taskID, repo, wt, branch string) string {
	wt = strings.TrimSpace(wt)
	branch = strings.TrimSpace(branch)
	if wt == "" && branch == "" {
		return ""
	}
	// ONE ledger read for both axes (and for the dead-owner half below): the
	// two scans that used to answer this separately are the same question
	// asked twice ("which other row names this ground?"), and the answer now
	// has to carry the row's HOLDER verdict, which neither scan returned
	// (Rules 13/14).
	claims, err := taskGroundClaimRows()
	if err != nil {
		if wt != "" {
			return fmt.Sprintf("cannot prove %s is unowned: %v", wt, err)
		}
		return fmt.Sprintf("cannot prove branch %s is unclaimed: %v", branch, err)
	}
	owner := taskGroundClaimRowNaming(claims, taskID, wt, branch, repo)
	if owner == nil {
		return ""
	}
	if !owner.HolderLive {
		// A DEAD OWNER IS NOT A BORROW (see the decision note above): the
		// naming row's life is over and this host can prove its holder gone,
		// so the ground is free and the seam records it. The observation is
		// REPORTED (Rule 15) so an operator can still see the two-row history.
		if wt != "" && sameWorkingPath(owner.Worktree, wt) {
			taskNotifyLog(taskWorktreeDeadOwnerGroundLineFmt, taskID, taskGroundOwnerHistoryNote(*owner, taskGroundWorktreeAxis, wt))
		} else {
			taskNotifyLog(taskWorktreeDeadOwnerGroundLineFmt, taskID, taskGroundOwnerHistoryNote(*owner, taskGroundBranchAxis, branch))
		}
		return ""
	}
	if wt != "" && sameWorkingPath(owner.Worktree, wt) {
		return fmt.Sprintf("%s is already named by row %s", wt, owner.TaskID)
	}
	return fmt.Sprintf("branch %s is already named by row %s", branch, owner.TaskID)
}

// TaskGroundOwnedByAnotherLiveRow is the EXPORTED form of the ONE-OWNER rule
// (taskCreationGroundBorrowed), for the seams outside this package that decide
// whether a run may TAKE a ground at all.
//
// WHY IT EXISTS AS ITS OWN DOOR (measured 2026-11-01, rows 01a19648-4fbe →
// 01a196bf-8cbb). The creation seam and the claim seam both consult the rule
// before stamping, but the CLONE channel ADOPTS a ground before any stamp is
// attempted: internal/agent's adoptAnnouncedCloneGround reads GOPI_RUN_GROUND
// and chdir's into it, on the premise that "the runner announced this to me".
// That premise is false for an INHERITED variable: a `gopi --prompt` started by
// the bash tool of a RUNNING RUN inherits that run's GOPI_RUN_GROUND, so the
// child adopted its PARENT's workspace and the stamp that followed wrote the
// parent's directory and branch onto the child's own row. The read-only
// question-answer run then failed its delivery leg — on a dirty tree its
// neighbour was still editing — having never touched a file.
//
// SO THE DECISION IS MADE BEFORE THE CHDIR, not after the stamp: a ground
// another LIVE row names is that row's, and the caller opens its own instead.
// Returns "" when the ground is free (or already this row's own), and the
// refusal sentence — naming the owning row — when it is not.
func TaskGroundOwnedByAnotherLiveRow(taskID, repo, worktree, branch string) string {
	return taskCreationGroundBorrowed(taskIDNormalize(taskID), repo, worktree, branch)
}

// taskGroundWorktreeAxis / taskGroundBranchAxis name the two axes a ground
// can be named on, so the seam's report sentence says which one it resolved
// without a second spelling of "worktree"/"branch" (Rule 1b).
const (
	taskGroundWorktreeAxis = DeliverableWorktreeKey
	taskGroundBranchAxis   = DeliverableBranchKey
)

// taskGroundClaimRow is one ledger row that NAMES a ground, carrying the
// verdict of the ONE holder predicate (finishCleanupOwnerRowLive) so the
// creation seam asks the same question about the same inputs the
// delivery-time recycle asks (Rule 14).
type taskGroundClaimRow struct {
	TaskID     string
	Status     string
	ClaimedBy  string
	Repo       string
	Branch     string
	Worktree   string
	HolderLive bool
}

// taskGroundClaimRows loads from the ledger every row that could NAME a
// ground, with each row's holder verdict. It is the creation seam's reader —
// deliberately NOT taskLiveClaims (that loader drops terminal rows, and a
// terminal row is exactly the case this reconciliation must classify).
//
// The SQL pre-filter is a cheap LIKE over the two deliverable keys (Rule 1b:
// the keys come from their constants, never a typed literal); the JSON decode
// stays the authority, as in every other deliverable scan.
func taskGroundClaimRows() ([]taskGroundClaimRow, error) {
	ctx, cancel := context.WithTimeout(context.Background(), taskWorktreeBackfillTimeout)
	defer cancel()
	db, err := openTasksDB(ctx, memoryDBPath())
	if err != nil {
		return nil, err
	}
	defer db.Close()
	rows, err := db.QueryContext(ctx,
		`SELECT task_id, status, COALESCE(claimed_by,''), COALESCE(deliverable,''), COALESCE(`+
			tasksProcPidColumnName+`,0), COALESCE(`+tasksProcStartColumnName+`,0), COALESCE(`+tasksProcCmdColumnName+`,'')`+
			` FROM tasks WHERE deliverable LIKE ? OR deliverable LIKE ?`,
		"%"+DeliverableWorktreeKey+"%", "%"+DeliverableBranchKey+"%")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []taskGroundClaimRow
	for rows.Next() {
		var id, status, claimedBy, deliverable string
		var procPid, procStart int64
		var procCmd string
		if err := rows.Scan(&id, &status, &claimedBy, &deliverable, &procPid, &procStart, &procCmd); err != nil {
			return nil, err
		}
		del := taskDeliverableParse(deliverable)
		if del.Worktree == "" && del.Branch == "" {
			continue
		}
		ident := procIdentity{Pid: int(procPid), StartTicks: procStart, Cmd: procCmd}
		out = append(out, taskGroundClaimRow{
			TaskID: id, Status: status, ClaimedBy: claimedBy,
			Repo: del.Repo, Branch: del.Branch, Worktree: del.Worktree,
			HolderLive: finishCleanupOwnerRowLive(status, claimedBy, ident, del.Worktree),
		})
	}
	return out, rows.Err()
}

// taskGroundClaimRowNaming returns the row that NAMES the ground, preferring a
// row that still HOLDS it over one whose binding is history: a directory (or
// branch) can be named by both a live row and a dead one, and whether the
// ground may be recorded must rest on the holder, never on scan order.
//
// The worktree axis is matched on the DIRECTORY and the branch axis on the
// branch, scoped to the repository the same way taskBranchRowOwner always
// scoped it (branch names are per-repository). Nothing matching returns nil.
func taskGroundClaimRowNaming(rows []taskGroundClaimRow, taskID, wt, branch, repo string) *taskGroundClaimRow {
	var other *taskGroundClaimRow
	for i := range rows {
		r := &rows[i]
		if r.TaskID == taskID {
			continue // a row never names its own ground twice
		}
		namedWorktree := wt != "" && sameWorkingPath(r.Worktree, wt)
		namedBranch := !namedWorktree && branch != "" && r.Branch == branch && sameWorkingPath(r.Repo, repo)
		if !namedWorktree && !namedBranch {
			continue
		}
		if r.HolderLive {
			return r
		}
		if other == nil {
			other = r
		}
	}
	return other
}

// taskGroundOwnerHistoryNote renders the ONE sentence the creation seam prints
// when the row naming the ground is HISTORY. It shares the stale-binding
// marker (finishCleanupOwnerStaleMarker) and the holder suffix with the
// delivery-time gate, so the two ends of the file describe one disposition in
// one vocabulary (Rule 14).
func taskGroundOwnerHistoryNote(r taskGroundClaimRow, axis, target string) string {
	return finishCleanupOwnerStaleMarker + ": " + axis + " " + target +
		" is named by row " + r.TaskID + " (" + r.Status + finishCleanupOwnerHolderSuffix(r.ClaimedBy) +
		") whose holder is provably gone here"
}

// TaskDeliverableWorktreeForRow is TaskDeliverableWorktree for a caller that
// NAMES the row it executes under (see TaskWorktreeBackfillRow for why the
// caller's own answer must win over the ambient wrapper env).
func TaskDeliverableWorktreeForRow(taskID, workdir, repo string) (wt, branch string, backfilled bool) {
	taskID = taskIDNormalize(taskID)
	if taskID == "" {
		return "", "", false
	}
	repo = strings.TrimSpace(repo)
	if repo == "" {
		repo = TaskDeliverableRepoFor(taskID)
	}
	if repo == "" {
		return "", "", false // row has no deliverable.repo yet — nothing to pair
	}
	if strings.TrimSpace(workdir) == "" {
		wd, err := os.Getwd()
		if err != nil {
			return "", "", false
		}
		workdir = wd
	}
	wt, branch = taskGroundOwningDir(repo, workdir)
	if branch == "" {
		return wt, branch, false // main checkout on an exempt branch / unrelated dir
	}
	// ONE GROUND, ONE OWNING ROW (see taskCreationGroundBorrowed): the ground
	// this seam RESOLVED is not yet the ground this row may RECORD. A directory
	// (or branch) another row already names is that row's — this run may stand
	// in it, it may not claim it. Refusal is reported, never silent, and the
	// run keeps working in the directory; only the ledger stamp is withheld.
	if why := taskCreationGroundBorrowed(taskID, repo, wt, branch); why != "" {
		taskNotifyLog(taskWorktreeBorrowedGroundLineFmt, taskID, why)
		return wt, branch, false
	}
	if err := taskBackfillDeliverableWorktree(taskID, wt, branch); err != nil {
		taskNotifyLog("task %s: deliverable.worktree backfill skipped: %v", taskID, err)
		return wt, branch, false
	}
	if _, offPolicy := taskOffPolicyWorktreePath(wt); offPolicy {
		taskNotifyOffPolicyWorktree(taskID, wt, branch)
	}
	// THE RUN-GROUND ANCHOR (epic worktree-reclaim-two-gaps, Gap B): this seam
	// runs at the moment a run is observed to have TAKEN a ground, which is the
	// only moment ownership can be RECORDED rather than reconstructed later. It
	// is called by the PRODUCER — never by a reclaim sweep — so no reader has to
	// guess a directory's owner from absence (see run_ground_anchor.go).
	TaskGroundAnchorRecord(taskID, wt, branch)
	return wt, branch, true
}

// TaskDeliverableWorktreeBash is the executor bash-round seam
// (internal/agent loop.go): after a successful bash command, detect whether
// the executor now works inside a linked worktree of the wrapper row's repo
// and backfill deliverable.worktree + deliverable.branch. Candidates probed
// in order: ① the bash tool's explicit workdir ("" = process cwd), ② any
// `git -C <path>` target in the command line (executors run git from the
// main repo cwd with an explicit -C worktree path — the workdir alone would
// miss the worktree). The FIRST candidate inside a linked worktree wins;
// others are not probed (a worktree move mid-run re-stamps on the next
// round). Best-effort: plain sessions (no row at all) return immediately.
func TaskDeliverableWorktreeBash(workdir, command string) {
	TaskDeliverableWorktreeBashForRow(TaskWorktreeBackfillRow(""), workdir, command)
}

// TaskDeliverableWorktreeBashForRow is TaskDeliverableWorktreeBash for a
// caller that NAMES the row it executes under. The row argument is what makes
// the seam reach the bare-prompt face: that run's row is minted by the
// harness's own registration hook (autoTaskRowID) and never appears in
// GOPI_TASK_WRAPPER_ID, so an env-keyed-only seam could not see it (the
// measured leak this epic closes).
func TaskDeliverableWorktreeBashForRow(taskID, workdir, command string) {
	taskDeliverableWorktreeCommandForRow(taskID, workdir, command)
}

// TaskDeliverableWorktreeJobForRow is the SAME seam for the run's
// BACKGROUND-JOB commands (the merged job tool's start action,
// internal/agent's job-notice drain). Why it must exist: the seam above is
// wired to the bash tool alone, so a run whose worktree/branch is created by
// a DETACHED job — `job(action=start, command="… git worktree add <wt> -b
// <branch> …")`, or the `git checkout -b` / long-running variant of it —
// recorded NO ground at all, neither in flight nor by this seam, while the
// ledger row stayed repo-only (the shape row 01a0aa0f-f6e2 left behind: a
// completed wrapper row whose deliverable is `{"repo":…}`, although an
// executor's own worktree command is exactly what the bash seam catches).
// The command is the same kind of evidence the bash round carries, so it is
// fed through the same candidate reader and the same writer — a second
// caller, never a second rule (Rules 13/14).
//
// workdir is deliberately "" (the process cwd): a job's command line carries
// its own directory in the dominant shapes (`cd <dir> && git …`, `git -C
// <dir> …`), and the row's own deliverable.repo remains the ground reference
// the resolver pairs against.
func TaskDeliverableWorktreeJobForRow(taskID, command string) {
	taskDeliverableWorktreeCommandForRow(taskID, "", command)
}

// taskDeliverableWorktreeCommandForRow is the ONE body of the creation-time
// detection seam, shared by its two callers (the bash round and the
// background-job notice). See TaskDeliverableWorktreeBashForRow's doc for the
// contract; the ONLY difference between the callers is the workdir the
// candidate scan starts from.
func taskDeliverableWorktreeCommandForRow(taskID, workdir, command string) {
	if taskIDNormalize(taskID) == "" {
		return // no row to pair — nothing to backfill
	}
	// Cheap pre-filter before any git spawn: the seam exists to catch the
	// worktree CREATION moment (and the -C worktree commits that follow).
	// A round whose command has no git/worktree/cd/workspace signal (pure
	// file edit, memory tool, ...) cannot have moved the executor into a
	// worktree — skip the probe entirely (the detection stays
	// best-effort; the FIRST git-touching round after a move still fires).
	needle := strings.ToLower(strings.TrimSpace(command))
	if !strings.Contains(needle, "git") && !strings.Contains(needle, "worktree") &&
		!strings.Contains(needle, "checkout") && !strings.Contains(needle, "branch") &&
		!strings.Contains(needle, "cd ") && !strings.Contains(needle, "push") {
		return
	}
	bound, created := taskBashWorkdirCandidateGroups(workdir, command)
	cands := append(append([]string{}, bound...), created...)
	// THE BOUND GROUND (the row's PRIMARY axes): the first directory the round
	// bound that resolves to a ground is the row's own — and the search STOPS
	// there, because the primary stamp must never be taken from a directory the
	// run merely created alongside (see the grounds pass below).
	boundStamped := false
	for _, c := range bound {
		if c == "" {
			continue
		}
		if _, _, ok := TaskDeliverableWorktreeForRow(taskID, c, ""); ok {
			boundStamped = true
			break
		}
	}
	// THE CREATED GROUNDS (the SECONDARY axis — epic 01a0dff1, measured row
	// 01a0ddc4-c410). This pass is the whole point of the split above: a round
	// that CREATES a worktree with `git worktree add <dir> -b <branch>` records
	// that directory as a ground the run OWNS, whether or not the row already
	// names its own ground. Before this, the loop returned at its first hit —
	// candidate #1 IS the run's own ground by construction, so the created
	// directory was never probed and the row had no record of it at all: the
	// ground stayed live and owner-less (`for-branch` answered "no owner") and
	// nothing could resume it.
	//
	// It deliberately does NOT call the primary writer: a created directory must
	// never REBIND the row's own ground (the 2026-09-12 foreign-owner hijack),
	// so it goes to the plural axis, which refuses to duplicate the primary and
	// only ever ADDS an entry.
	//
	// WHICH AXIS A CREATED GROUND LANDS ON is decided by what the ROW already
	// records, read once (never by the resolver's return, which reports "resolved"
	// even when the stamp was refused — that ambiguity is what would silently
	// swallow this pass):
	//
	//	the row records NO ground  → the created directory IS the row's ground
	//	                            (the Rule-6a solo shape: launched in the main
	//	                            checkout, minted its own worktree) → PRIMARY
	//	                            axes, exactly as before this change.
	//	the row records its own    → the created directory is a SECOND ground →
	//	                            the plural axis, and the primary is never
	//	                            touched (the 2026-09-12 hijack stays closed).
	rowWT, rowBranchAxis := TaskDeliverableWorktreeFor(taskID)
	for _, c := range created {
		if c == "" {
			continue
		}
		if strings.TrimSpace(rowWT) == "" && strings.TrimSpace(rowBranchAxis) == "" {
			if _, _, ok := TaskDeliverableWorktreeForRow(taskID, c, ""); ok {
				boundStamped = true
			}
			continue
		}
		rec, recorded := taskGroundRecordedForCreatedDir(taskID, c)
		if !recorded {
			continue
		}
		// THE SAME ONE-OWNER RULE ON THE PLURAL AXIS (see
		// taskCreationGroundBorrowed). The created-grounds pass enumerates the
		// directory a round NAMED after `git worktree add` — and the command text
		// is read whether or not git performed the creation (a failing `git
		// worktree add <a worktree another task already registered> -b feat/x`
		// still parses), so this axis can be handed a ground another row already
		// names by exactly the same route. The rule is applied once, through the
		// same two scans, before anything is appended to deliverable.grounds.
		if why := taskCreationGroundBorrowed(taskID, rec.Repo, rec.Worktree, rec.Branch); why != "" {
			taskNotifyLog(taskWorktreeBorrowedGroundLineFmt, taskID, why)
			continue
		}
		// The round DID record a ground — so the Rule-15b miss line below (which
		// speaks about a branch-creating round that recorded nothing) must not
		// fire for it. The second ground is recorded evidence exactly as the
		// primary axes are.
		boundStamped = true
		// The anchor records the ground the run TOOK — the created directory is
		// exactly that, and the anchor is the record that survives the terminal
		// transition (run_ground_anchor.go). It is written here because this seam
		// is the only moment the acquisition is observed.
		TaskGroundAnchorRecord(taskID, rec.Worktree, rec.Branch)
		if err := taskBackfillDeliverableGrounds(taskID, rec.Worktree, rec.Branch); err != nil {
			taskNotifyLog("task %s: deliverable.grounds backfill skipped: %v", taskID, err)
		}
	}
	if boundStamped {
		return // the row's own ground is recorded — the primary axis is done
	}
	// RULE 15b — THE MISS IS NO LONGER SILENT. A round that CREATED a branch and
	// recorded NOTHING is exactly the state this seam exists to prevent: the
	// branch would reach the ledger only at completion, and the merger's
	// attribution would print it in the `unowned:`/gap class for the whole run.
	// It happens when the ground is unreachable to us (a git failure, a directory
	// this process cannot stat, a wrapper-spawned shell whose cwd never became
	// visible) — a case in which every candidate loop above simply finds nothing.
	//
	// The line names the row, the branch-creating command and the directories
	// that were probed, because that is what makes the miss actionable: it is the
	// same sentence the stamp itself would have logged, with the outcome
	// inverted. Emitted ONLY when the command actually creates a branch, so a
	// round with no branch intent stays as quiet as before (a signal on every
	// git round is noise, and noise is how a real miss gets ignored).
	if !taskBranchCreationsFromCommand(command) {
		return
	}
	taskNotifyLog(taskWorktreeUnstampedLineFmt, taskID, strings.Join(cands, ", "), strings.TrimSpace(command))
}

// taskGroundRecordedForCreatedDir resolves the ground a CREATED directory names,
// for the secondary-axis pass above: the repo is the row's own deliverable.repo
// (never guessed from cwd — the same anchor every other stamp pairs against) and
// the branch is the branch that directory has checked out.
//
// It reports recorded=false when the directory does not resolve to a ground of
// the row's repo (a foreign repo, a directory with no branch), when the branch is
// an integration branch (taskGroundOwningDir's own exemption), or when the row is
// repo-less — in every one of those cases there is nothing to attribute, and the
// pass stays silent rather than recording a ground nobody owns.
func taskGroundRecordedForCreatedDir(taskID, dir string) (taskDeliverableGround, bool) {
	repo := TaskDeliverableRepoFor(taskID)
	if repo == "" {
		return taskDeliverableGround{}, false
	}
	wt, branch := taskGroundOwningDir(repo, dir)
	if branch == "" {
		return taskDeliverableGround{}, false
	}
	return taskDeliverableGround{Repo: repo, Branch: branch, Worktree: wt}, true
}

// taskBashWorkdirCandidates enumerates the directories a just-finished bash
// command BOUND this row to: the round's own workdir (the directory the command
// ran in), every linked worktree the command CREATES or MOVES (`git worktree
// add|move <path>` — the creation moment this seam exists for), and the
// directory a MUTATING git invocation runs in — git's own `-C <dir>` target, or
// the directory the command's `cd`s carry into it (`cd <dir> && git commit`),
// which is the same fact in the shell's spelling.
//
// WHY "MENTIONED" IS NOT ENOUGH (measured 2026-09-15 — the live leak, and the
// shape that left the two registrations in this epic's own attachment): the scan
// used to accept EVERY `-C <path>` in the command line, whatever the git
// subcommand was. A round that merely LOOKED at another task's worktree — the
// routine inspection of residue like
//
//	for wt in <orphan-A> <orphan-B>; do git -C $wt status; git -C $wt log -1; done
//
// — was read as "this run works in <orphan-A>", and the seam re-stamped THIS
// row's deliverable.worktree with it. The row that never worked there then
// became the orphan's PROTECTED OWNER: the worktree sweeper's ownership arm
// matched `deliverable.worktree` and printed "owned by live task <this row>
// (in_progress)" — so the orphan could never be collected while the run was
// alive, and every later run that inspected it re-created the same claim. The
// seam's own comment already named the mechanism as a past incident ("a probe
// such as `git -C ~/workspace/gopi-OTHER status` … re-pointed THIS row's
// deliverable at that other worktree, so the row lost its own directory and
// became a FOREIGN OWNER of someone else's") and the repair it got (never
// REBIND an existing stamp) only closed the case where the row had already
// stamped its own worktree — a row with NO stamp yet (this epic's row, minted
// from a `task run` wrapper with a repo-only deliverable) had nothing to
// protect it, and was hijacked by its own first inspection round.
//
// Detection must only ever ADD information about where THIS run works, and a
// read-only look is not working: `status`/`log`/`list`/`diff` never bind, while
// a mutation does (Rule 10.4: the smallest sample that separates the two is the
// git subcommand, so that is what the rule reads).
func taskBashWorkdirCandidates(workdir, command string) []string {
	bound, created := taskBashWorkdirCandidateGroups(workdir, command)
	return append(append([]string{}, bound...), created...)
}

// taskBashWorkdirCandidateGroups enumerates the SAME directories as
// taskBashWorkdirCandidates, split by what the round's evidence actually says
// about them. The split is load-bearing (epic 01a0dff1, measured row
// 01a0ddc4-c410) and it is why the enumerator has two outputs:
//
//	BOUND   — the round's own workdir, and the directory a MUTATING git
//	          invocation ran in (git's `-C` target, or the dir a preceding `cd`
//	          carried into it). These are the directories the round WORKED in,
//	          which is the only evidence the PRIMARY axes accept — the first one
//	          that resolves to a ground is the row's own ground, and the
//	          primary search stops there.
//	CREATED — the directories the round CREATED or MOVED
//	          (`git worktree add|move <path>`). A created directory says the run
//	          OWNS a ground there; it says nothing about where the round worked.
//
// KEEPING THE TWO APART IS THE FIX: with ONE list, the created directory sat
// behind the run's own ground in the same loop, and the loop RETURNED at its
// first hit — so a run that minted a second worktree (the incident's evidence
// worktree) recorded nothing for it anywhere, and the live ground stayed
// owner-less. The bound group still stops at its first hit (a primary axis is
// single-valued); the created group is walked on its own and lands on the
// plural axis, which never rebinds the primary.
func taskBashWorkdirCandidateGroups(workdir, command string) (bound, created []string) {
	seen := map[string]bool{}
	add := func(dst *[]string, p string) {
		p = strings.TrimSpace(p)
		if p == "" {
			return
		}
		// Shell tilde: executors write the Rule-6 creation command the way
		// they would type it — `git worktree add ~/workspace/gopi-x -b feat/x`
		// — so the path token carries a literal "~" that the SHELL would
		// have expanded. `os.Stat("~/workspace/...")` resolves against the
		// process cwd and fails, so the candidate was silently dropped and
		// the worktree never got stamped (row 01a08913-d4eb: deliverable
		// stayed {"repo":...} while the child had created + committed in the
		// worktree). Expand before statting — the same routine read/write/
		// edit use, so the two paths cannot drift.
		if expanded, err := expandHome(p); err == nil {
			p = expanded
		}
		abs, err := filepath.Abs(p)
		if err != nil {
			return
		}
		if info, err := os.Stat(abs); err != nil || !info.IsDir() {
			return
		}
		if !seen[abs] {
			seen[abs] = true
			*dst = append(*dst, abs)
		}
	}
	if strings.TrimSpace(workdir) != "" {
		add(&bound, workdir)
	} else if wd, err := os.Getwd(); err == nil {
		add(&bound, wd)
	}
	// The command's own bind evidence, judged ONCE per simple command in
	// shell order: a worktree it creates/moves, or the directory a MUTATING
	// git invocation runs against. Each simple command is parsed through the
	// ONE readers this package already owns — commandChangeDir/
	// carriedChangeDir for the shell half, gitInvocationStart for the git half
	// (Rules 13/14: a second hand-rolled `-C` or `cd` scan is exactly how the
	// two spellings drifted apart).
	//
	// THE `cd` CARRY IS GATED BY THE SAME RULE AS `-C` (measured 2026-09-19:
	// TestTaskDeliverableWorktreeBashReadOnlyProbeDoesNotBind, red on master).
	// A `cd <dir>` — like every other shell builtin — carries to the commands
	// that FOLLOW it in the same shell line, and by itself says nothing about
	// where the round worked: `cd <another task's worktree> && git status` is
	// the read-only inspection of someone else's tree in its `cd` spelling,
	// exactly as `git -C <tree> status` is in the `-C` spelling. The carry was
	// added unconditionally (f42f8cb4, the branch axis) to reach the observed
	// `cd <repo> && git switch -c feat/x` shape, and that made the read-only
	// spelling bind the row — the hijack the sed-like subcommand gate below
	// exists to refuse, re-opened through the other door: the row became the
	// inspected worktree's protected owner and the sweeper could never collect
	// it. So the carry is resolved at the BINDING invocation, from the cds
	// that precede it, and only when that invocation binds.
	var cdDirs []string
	for _, simple := range splitSimpleCommands(stripQuotes(command)) {
		if dir, ok := commandChangeDir(simple); ok {
			cdDirs = append(cdDirs, dir)
			continue // a `cd` is not a git invocation — nothing binds here
		}
		tokens := strings.Fields(simple)
		baseDir, start, ok := gitInvocationStart(tokens)
		if !ok {
			continue
		}
		// ① the creation/move moment: `git worktree add [-b <branch>] <path>`
		//    (also the `-C <repo>` form, whose path operand is resolved from
		//    argument order, never from the -C value).
		if start+1 < len(tokens) && tokens[start] == worktreeToken &&
			(tokens[start+1] == worktreeAddToken || tokens[start+1] == worktreeMoveToken) {
			if target, ok := firstPositional(tokens[start+2:]); ok {
				// A CREATED directory goes to the created group: it is the
				// secondary axis' evidence, never the primary's (see this
				// function's header).
				add(&created, target)
			}
			continue
		}
		// ② a mutation BINDS the directory it ran in — git's own `-C` target
		//    when the command names one, else the directory the cds before it
		//    carry (the observed `cd <repo> && git checkout -b feat/x` /
		//    `cd <repo> && git switch -c feat/x` shape, which names no `-C` at
		//    all). A read-only subcommand proves nothing about where the run
		//    works, in EITHER spelling — see the header of this comment.
		if !taskBindMutatingGitSubcommandSet[tokens[start]] {
			continue
		}
		if baseDir == "" {
			baseDir = carriedChangeDir(cdDirs, workdir)
		}
		add(&bound, baseDir)
	}
	return bound, created
}

// taskBindMutatingGitSubcommands are the git subcommands that WRITE: a round
// that ran one of these against a directory is working there, which is the only
// evidence the creation-time backfill accepts (see taskBashWorkdirCandidates for
// the measured hijack this separates). Named once (Rule 1b) and read as a set —
// one definition, two readers (this file and its tests).
const taskBindMutatingGitSubcommands = " add am apply checkout cherry-pick commit fetch merge mv pull push rebase reset restore revert rm stash switch tag "

// taskBindMutatingGitSubcommandSet is taskBindMutatingGitSubcommands as a lookup
// set — one definition, two readers (the candidate scan above and its tests).
var taskBindMutatingGitSubcommandSet = func() map[string]bool {
	set := map[string]bool{}
	for _, s := range strings.Fields(taskBindMutatingGitSubcommands) {
		set[s] = true
	}
	return set
}()

// taskGroundOwningDir is the ONE resolver of the ground a run stands on in repo,
// for a directory the run works in — the BRANCH axis added to
// taskWorktreeOwningDir's worktree axis (epic 01a0b182; one resolver, Rules
// 13/14, so the two axes can never disagree about what "the ground" means).
//
// TWO SHAPES, one answer:
//
//	(a) LINKED WORKTREE — dir is inside a linked worktree of repo. Both axes are
//	    real: the worktree, and the branch that worktree has checked out.
//	(b) MAIN CHECKOUT — dir is repo (or inside it) and repo's HEAD carries a
//	    NON-exempt branch. There is no linked worktree, so the worktree axis is
//	    "". The branch is the run's deliverable exactly as much as in (a): a run
//	    working in the main checkout on feat/x delivers feat/x, and the ordinary
//	    in-place creation (`cd <repo> && git checkout -b feat/x`) is the
//	    commonest way a row acquires one.
//
// WHY (b) IS NOT OPTIONAL (the measured gap this closes): taskWorktreeOwningDir
// cannot see (b) by construction — the first `git worktree list --porcelain`
// record IS the main checkout and is excluded — so every row claiming or
// creating its branch in place stayed repo-only until COMPLETION
// (taskBackfillDeliverableBranch, terminal status) and the merger's branch
// attribution classified that branch `unowned` for the whole in-flight window
// (branchAttributionRows loads no row, branchAttribUnowned, the `unowned:`/gap
// class) while its owner was demonstrably still working.
//
// An exempt branch (taskBranchExempt: master/main/archive-*) resolves to nothing
// in BOTH shapes: an integration branch is never a deliverable, and the main
// checkout on master is the ordinary fresh-claim ground, not a claim.
func taskGroundOwningDir(repo, dir string) (worktree, branch string) {
	if wt, br := taskWorktreeOwningDir(repo, dir); wt != "" && br != "" {
		return wt, br // (a) the linked worktree — both axes
	}
	if !taskDirInsideRepo(repo, dir) {
		return "", "" // an unrelated directory is not this repo's ground
	}
	return "", taskRepoHeadBranch(repo) // (b) the main checkout's own HEAD
}

// taskDirInsideRepo reports whether dir IS repo or lies inside it. Both paths go
// through the ONE lexical path comparison the rest of this package uses
// (sameWorkingPath), so a symlinked or differently-spelled root cannot make one
// directory read as two.
func taskDirInsideRepo(repo, dir string) bool {
	repoAbs, err := filepath.Abs(strings.TrimSpace(repo))
	if err != nil || repoAbs == "" {
		return false
	}
	dirAbs, err := filepath.Abs(strings.TrimSpace(dir))
	if err != nil {
		return false
	}
	if sameWorkingPath(repoAbs, dirAbs) {
		return true
	}
	return strings.HasPrefix(dirAbs, repoAbs+string(filepath.Separator))
}

// taskRepoHeadBranch returns the branch repo's HEAD carries, "" when that is
// unreadable, detached, or EXEMPT (master/main/archive-*). It is the branch axis
// of shape (b) above, and it is deliberately NOT a second branch reader: the git
// call shape and the exemption rule are the writeback resolver's own
// (taskBranchFromWorkdir + taskBranchExempt — one rule, two readers).
func taskRepoHeadBranch(repo string) string {
	repo = strings.TrimSpace(repo)
	if repo == "" {
		return ""
	}
	return taskBranchFromWorkdir(repo)
}

// The BRANCH-CREATING spellings of git, named once (Rule 1b): the command-level
// reader below decides "did this round CREATE a branch?" from them, and the
// session-scan reader (task_deliverable_branch.go) keys its own -b/-B scan on the
// same flag spellings — two readers, one spelling (Rule 13).
const (
	// gitCheckoutSubcmd / gitSwitchSubcmd are git's two branch-moving verbs.
	// `git checkout` also serves file restores, so only the CREATING form of
	// each verb counts (see taskBranchCreateFlagSet).
	gitCheckoutSubcmd = "checkout"
	gitSwitchSubcmd   = "switch"
	// taskBranchCreateFlagShort / taskBranchCreateFlagForce are `git checkout
	// -b|-B <branch>`: the flags that move HEAD onto a NEWLY created branch.
	// The force spelling resets an existing branch to HEAD, which is the same
	// "this round named a branch" event as the non-force form.
	taskBranchCreateFlagShort = "-b"
	taskBranchCreateFlagForce = "-B"
	// taskBranchCreateFlagSwitch / taskBranchCreateFlagSwitchForce are the `git
	// switch` spellings of the same pair — separate constants because they are
	// separate argv tokens, not because they mean anything different.
	taskBranchCreateFlagSwitch      = "-c"
	taskBranchCreateFlagSwitchForce = "-C"
)

// taskBranchCreateFlagSet is the lookup set of every branch-CREATING flag — one
// definition, read by the command-level reader below and by its tests.
var taskBranchCreateFlagSet = map[string]bool{
	taskBranchCreateFlagShort:       true,
	taskBranchCreateFlagForce:       true,
	taskBranchCreateFlagSwitch:      true,
	taskBranchCreateFlagSwitchForce: true,
}

// taskBranchCreationsFromCommand reports whether the shell command line CREATES a
// branch — `git checkout -b|-B <name>`, `git switch -c|-C <name>` — among its
// simple commands. It answers the Rule 15b question ("this round named a branch;
// was the claim reachable?"), so it reads the COMMAND, never the row.
//
// WHY THE RESOLVER CANNOT ANSWER IT: a branch-creating round whose ground is
// unreadable to us (a git failure, a directory this process cannot stat, a
// wrapper-spawned shell whose cwd never became visible) leaves NO candidate and
// therefore no signal at all — the round would look like every other round that
// has nothing to do with branches, and its branch would reach the ledger only at
// completion, which is the defect. Detecting the INTENT to create is exactly what
// makes the miss SAYABLE.
//
// It reads the command's structure through the package's ONE splitter
// (splitSimpleCommands), like every other command inspector here: a second
// hand-rolled split is how two spellings drift apart (Rules 13/14).
func taskBranchCreationsFromCommand(command string) bool {
	for _, simple := range splitSimpleCommands(stripQuotes(command)) {
		tokens := strings.Fields(simple)
		_, start, ok := gitInvocationStart(tokens)
		if !ok || start >= len(tokens) {
			continue
		}
		sub := tokens[start]
		if sub != gitCheckoutSubcmd && sub != gitSwitchSubcmd {
			continue
		}
		for _, tok := range tokens[start+1:] {
			if taskBranchCreateFlagSet[tok] {
				return true
			}
			if strings.ContainsAny(tok, "&|;") {
				break // the verb's own argument list ends at a chain boundary
			}
		}
	}
	return false
}

// taskBranchCreatedFromCommand returns the branch a command CREATES
// (`git checkout -b|-B <name>` / `git switch -c|-C <name>`) when that
// creation happens on repo's ground — the directory the git call runs in is
// repo itself (or inside it, or unnamed ⇒ the caller's workdir, which IS
// repo), per the SAME directory readers the policy uses (gitInvocationStart's
// `-C` half and carriedChangeDir's `cd` half, Rules 13/14). "" when the
// command creates no branch or creates one somewhere else.
//
// WHY THE DIRECTORY GATE (and why it is not optional): the session-level
// writeback scan reads every command a child ever ran, including the routine
// cross-task inspection (`cd <another task's worktree> && git checkout -b
// probe`). Reading a branch NAME without asking WHERE it was created would
// stamp this row with someone else's branch — the deliverable hijack the
// worktree axis refuses for directories, here refused for names.
//
// WHY IT IS NEEDED AT ALL: the terminal writeback resolves the branch from
// the child's launch workdir (`git -C <workdir> branch --show-current`), which
// names the right branch only while HEAD still points at it. A run that
// created its branch in place and then left (or a job that ran the creation)
// has no worktree to resolve, and the branch lives in the COMMAND. The
// caller verifies the returned name still exists in the repo before using it.
func taskBranchCreatedFromCommand(command, repo, workdir string) string {
	var cdDirs []string
	for _, simple := range splitSimpleCommands(stripQuotes(command)) {
		if dir, ok := commandChangeDir(simple); ok {
			cdDirs = append(cdDirs, dir)
			continue // a `cd` is not a git invocation
		}
		tokens := strings.Fields(simple)
		baseDir, start, ok := gitInvocationStart(tokens)
		if !ok || start >= len(tokens) {
			continue
		}
		sub := tokens[start]
		if sub != gitCheckoutSubcmd && sub != gitSwitchSubcmd {
			continue
		}
		name := ""
		for i := start + 1; i+1 < len(tokens); i++ {
			if strings.ContainsAny(tokens[i], "&|;") {
				break // the verb's own argument list ends at a chain boundary
			}
			if taskBranchCreateFlagSet[tokens[i]] {
				name = strings.TrimRight(tokens[i+1], ";&|")
				break
			}
		}
		if name == "" || taskBranchExempt(name) {
			continue
		}
		dir := baseDir
		if dir == "" {
			dir = carriedChangeDir(cdDirs, workdir)
		}
		// An unnamed directory means the command ran where the shell stood —
		// the caller's workdir, which is repo for a task-run child.
		if dir == "" || taskDirInsideRepo(repo, dir) {
			return name
		}
	}
	return ""
}

// taskLinkedWorktree is ONE record of `git worktree list --porcelain`: the
// directory and the branch checked out there ("" when detached or unnamed).
// The pair is what a task row's deliverable names, so it is the unit both
// readers below work in.
type taskLinkedWorktree struct {
	path   string
	branch string
}

// taskParseWorktreeListPorcelain parses `git worktree list --porcelain` into
// the MAIN checkout's path and every LINKED worktree record. ONE parser, two
// readers (Rules 13/14): the owning-dir resolver below and the claim-time
// candidate reader (taskRepoLinkedWorktrees) must never disagree about what
// the format says.
//
// The format: each record is "worktree <path>" + optional branch / detached /
// bare / prunable-marker lines (see finishCleanupPorcelainPrunableMark),
// blank-line terminated. The FIRST
// record is always the MAIN checkout — returned separately, never as a linked
// entry (a main checkout is not a worktree a run can be bound to).
func taskParseWorktreeListPorcelain(out string) (mainDir string, linked []taskLinkedWorktree) {
	var curPath, curBranch string
	flush := func() {
		if curPath != "" {
			if mainDir == "" {
				mainDir = curPath // the first record IS the main checkout
			} else {
				linked = append(linked, taskLinkedWorktree{
					path:   curPath,
					branch: strings.TrimPrefix(curBranch, "refs/heads/"),
				})
			}
		}
		curPath, curBranch = "", ""
	}
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		switch {
		case strings.HasPrefix(line, finishCleanupPorcelainWorktreePrefix):
			flush()
			curPath = strings.TrimSpace(strings.TrimPrefix(line, finishCleanupPorcelainWorktreePrefix))
		case strings.HasPrefix(line, finishCleanupPorcelainBranchPrefix):
			curBranch = strings.TrimSpace(strings.TrimPrefix(line, finishCleanupPorcelainBranchPrefix))
		case line == "":
			flush()
		}
	}
	flush()
	return mainDir, linked
}

// taskWorktreeOwningDir finds the LINKED worktree of repo whose path is a
// prefix of dir (or equals it), and the branch that worktree has checked
// out. Returns ("","") when dir is the MAIN checkout or not inside any
// worktree of repo, or the branch is exempt (master/main/archive — nothing
// to backfill). Reads `git worktree list --porcelain` through the package's
// ONE parser of that format (taskParseWorktreeListPorcelain).
func taskWorktreeOwningDir(repo, dir string) (string, string) {
	abs, err := filepath.Abs(dir)
	if err != nil {
		return "", ""
	}
	ctx, cancel := context.WithTimeout(context.Background(), taskWorktreeBackfillTimeout)
	defer cancel()
	// PARSED porcelain: stdout only, stderr never enters the parse (a
	// `GIT_TRACE=1` line would become a bogus entry).
	out, err := finishCleanupGitParsedStdout(ctx, repo, taskWorktreeBackfillTimeout,
		finishCleanupWorktreeVerb, finishCleanupWorktreeListVerb, finishCleanupWorktreeListFlag)
	if err != nil {
		return "", "" // not a git repo (or wedged) — nothing to pair
	}
	_, entries := taskParseWorktreeListPorcelain(out)
	for _, e := range entries {
		if e.path == "" || e.branch == "" || e.branch == e.path {
			continue // detached or no branch — never attribute a worktree
		}
		if taskBranchExempt(e.branch) {
			continue // master/main/archive — nothing to backfill
		}
		if abs == e.path || strings.HasPrefix(abs, e.path+string(filepath.Separator)) {
			return e.path, e.branch
		}
	}
	return "", ""
}

// taskRepoLinkedWorktrees lists repo's registered LINKED worktrees — the main
// checkout excluded — that still EXIST on disk. Read-only, single git call,
// through the ONE porcelain parser above.
//
// The error IS returned, not swallowed (Rule 15), because its only caller is a
// fail-closed decision: an unreadable `git worktree list` cannot prove "this
// repo has no linked worktree", and the caller binds a row to a directory on
// the strength of that answer.
//
// A registration whose directory is GONE is dropped: `git worktree list`
// reports registrations, and the child's ground is a directory it will start
// in — a prunable registration is not a place (the same distinction the sweep
// makes between held-by-registration and held-by-liveness).
func taskRepoLinkedWorktrees(repo string) ([]taskLinkedWorktree, error) {
	repo = strings.TrimSpace(repo)
	if repo == "" {
		return nil, fmt.Errorf("no repo to list worktrees of")
	}
	ctx, cancel := context.WithTimeout(context.Background(), taskWorktreeBackfillTimeout)
	defer cancel()
	// PARSED porcelain: stdout only (see taskWorktreeOwningDir).
	out, err := finishCleanupGitParsedStdout(ctx, repo, taskWorktreeBackfillTimeout,
		finishCleanupWorktreeVerb, finishCleanupWorktreeListVerb, finishCleanupWorktreeListFlag)
	if err != nil {
		return nil, fmt.Errorf("git worktree list in %s: %w", repo, err)
	}
	_, linked := taskParseWorktreeListPorcelain(out)
	live := make([]taskLinkedWorktree, 0, len(linked))
	for _, e := range linked {
		if e.path == "" {
			continue
		}
		if info, statErr := os.Stat(e.path); statErr != nil || !info.IsDir() {
			continue // registration without a directory — not a place to start in
		}
		live = append(live, e)
	}
	return live, nil
}

// taskRowDeliverableRepo reads ONLY the repository a row's deliverable records
// ("" when the row is gone, records no repo, or its blob is unreadable).
//
// It exists as its own reader so the show face's gap-2 repair reads ONE field
// through the ONE row reader (taskRowDeliverable) rather than growing a second
// ledger query that could disagree about a row's deliverable (Rules 13/14).
func taskRowDeliverableRepo(taskID string) string {
	if strings.TrimSpace(taskID) == "" {
		return ""
	}
	repo, _, _ := taskRowDeliverable(taskID)
	return strings.TrimSpace(repo)
}

// taskRowDeliverable reads a task row's deliverable blob fields leniently
// (all "" when the row is gone / has no deliverable / not JSON).
func taskRowDeliverable(taskID string) (repo, branch, worktree string) {
	ctx, cancel := context.WithTimeout(context.Background(), taskWorktreeBackfillTimeout)
	defer cancel()
	// READ-ONLY OPENER (see openTasksDBReadOnly): this is a pure read, called
	// from the `task show` advisory line among others, and the read-write
	// opener would put a write-lock acquisition — and dbBusyTimeoutMSec of
	// waiting behind a live writer — in front of every one of them. The
	// lenient contract below is unchanged: an opener error (which now includes
	// a ledger that does not exist yet) still reads as "no deliverable".
	db, err := openTasksDBReadOnly(ctx, memoryDBPath())
	if err != nil {
		return "", "", ""
	}
	defer db.Close()
	t, err := taskByID(ctx, db, taskID)
	if err != nil || t == nil {
		return "", "", ""
	}
	var d TaskDeliverableSpec
	if err := json.Unmarshal([]byte(t.Deliverable), &d); err != nil {
		return "", "", ""
	}
	return strings.TrimSpace(d.Repo), strings.TrimSpace(d.Branch), strings.TrimSpace(d.Worktree)
}

// TaskDeliverableWorktreeFor returns the worktree a task's executor works
// in, from the row's deliverable.worktree (backfilled at creation time by
// the executor seam). Returns ("", "") when the row carries no worktree
// (repo-only submit whose executor never created one / not yet detected).
// The RESUME path reads this to chdir into the worktree instead of the
// session workdir — a mid-worktree crash resumes inside the worktree with
// its commits visible.
func TaskDeliverableWorktreeFor(taskID string) (worktree, branch string) {
	taskID = taskIDNormalize(taskID)
	if taskID == "" {
		return "", ""
	}
	_, branch, worktree = taskRowDeliverable(taskID)
	return worktree, branch
}

// TaskDeliverableRepoFor returns the repo a task's work happens in, from
// the row's deliverable.repo (stamped at submit, M4.8). "" when the row is
// gone or carries no repo.
func TaskDeliverableRepoFor(taskID string) string {
	taskID = taskIDNormalize(taskID)
	repo, _, _ := taskRowDeliverable(taskID)
	return repo
}

// taskBackfillDeliverableWorktree stamps worktree (+branch) into a task
// row's deliverable JSON — {"repo":...} → {"repo":...,"branch":...,
// "worktree":...} — ONLY when the row exists, its deliverable still carries
// a repo, and the worktree is non-empty. Merges, never replaces: an
// existing branch/worktree written by another stamp survives when the new
// value is empty; a non-empty new worktree/branch OVERWRITES the old field
// (the executor may legitimately move to a fresh worktree mid-run — the
// latest worktree is the one to resume/recycle). Guarded single UPDATE
// keyed on the read-back raw blob (CAS-by-value — a concurrent writer
// cannot be clobbered). Any status: the creation-time stamp must land on a
// row that is still pending/claimed/in_progress. Best-effort: every failure
// logs via taskNotifyLog and returns nil — never fatal to the caller.
func taskBackfillDeliverableWorktree(taskID, worktree, branch string) error {
	taskID = taskIDNormalize(taskID)
	_, err := taskBackfillDeliverableWorktreeAt(taskID, worktree, branch, nil, taskWorktreeStampMomentCreation)
	return err
}

// The TWO moments the SAME writer serves, and the ONE spelling of each (Rule
// 14; epic branch↔task linkage, item [2/5], 2026-09-18).
//
// The writer is shared on purpose — one blob parser, one rebuild, one key —
// but its OUTCOME LINE is an operator-facing record of WHEN the ground was
// learned, and there are two different answers:
//
//	creation — the executor's own bash round created (or moved into) the
//	           linked worktree, so the ground is being learned as it appears
//	           (internal/agent's detection seam).
//	claim    — the wrapper that just CLAIMED the row resolved the directory
//	           the executor child starts in and it is already inside the
//	           row's linked worktree (tools.TaskWorktreeStampClaimerFor ←
//	           cmd/agent's taskClaimGroundStamp, called from runTaskRunOnce
//	           BEFORE the child is spawned).
//
// MEASURED 2026-09-18 (live claim observation, epic 01a0b182 item 2/5): the
// claim-time write was logged as "… backfilled … at creation", and the
// measured write order in the wrapper's own log places it BEFORE the
// `executor cwd →` line — i.e. before the child existed, when nothing had
// been created. The byte was written correctly and attributed to the wrong
// moment: the CREATION reading was not a hedged wrong-by-one, it was a claim.
// The two spellings below keep the shared writer (the anti-drift half of the
// original contract) while making the line say which moment it records.
const (
	taskWorktreeStampMomentClaim    = "claim"
	taskWorktreeStampMomentCreation = "creation"
	// taskWorktreeStampMomentRepair is the OPERATOR-moment writer: the row's
	// ground was stamped by a repair verb acting on the operator's argument
	// (task_repo_repair.go), not by a seam that watched a run. It exists as a
	// third spelling rather than reusing one of the two above because the line
	// this renders is a CLAIM about when the byte was written — attributing an
	// operator's repair to "creation" would be the same measured defect the
	// claim/creation split was introduced for (see the note above).
	taskWorktreeStampMomentRepair = "repair"
)

// taskWorktreeStampLineFmt is the ONE spelling of the outcome line (Rule 1b:
// the message shape lives in the constant, never at the call site), and
// taskWorktreeStampLog is the ONE sink it is written through. The sink is a
// package-level seam so a test can capture WHICH moment a write records —
// production rebinds nothing. Precedent: taskWorktreeOwnerScan.
const taskWorktreeStampLineFmt = "task %s: deliverable.worktree backfilled to %q (branch %q) at %s"

// taskWorktreeUnstampedLineFmt is the ONE spelling of the Rule-15b line the
// creation seam emits when a branch-CREATING round recorded NO ground (see
// TaskDeliverableWorktreeBashForRow). It names the row, the directories that
// were actually probed, and the command — the three facts an operator needs to
// tell "the run works somewhere we cannot see" from "the run created no branch".
const taskWorktreeUnstampedLineFmt = "task %s: a branch-creating command recorded NO ground (probed: %s) — the row keeps its previous deliverable and this branch will not be attributed to it until completion: %s"

var taskWorktreeStampLog = taskNotifyLog

// taskClaimGroundLog is the ONE sink the claim-time seam's OWN decision lines
// are written through (the no-non-exempt-branch line and the candidate INFO
// line). A package-level seam for the same reason taskWorktreeStampLog is one:
// the claim-time DECISION is what a test must be able to observe without
// scraping a process's stderr. Production rebinds nothing.
var taskClaimGroundLog = taskNotifyLog

// taskWorktreeStampMomentArg renders a moment for the outcome line's %s.
// Only the claim/repair moments are named by a caller with its own reading; an
// unknown moment renders as the creation spelling, which is what every other
// caller of this writer was. The operator-moment spelling is named explicitly.
func taskWorktreeStampMomentArg(moment string) string {
	switch moment {
	case taskWorktreeStampMomentClaim, taskWorktreeStampMomentRepair:
		return moment
	}
	return taskWorktreeStampMomentCreation
}

// taskWorktreeStampClaimer is the ONE claim-time entry to the worktree stamp
// (epic branch↔task linkage, item [2/5]): it records the ground the run is
// ABOUT to execute in — the branch and the linked worktree — while the row
// is still in flight, so an in_progress row names where it works instead of
// waiting for the completion-time backfill (which only fires on a terminal
// status). The caller is the wrapper that just claimed the row
// (cmd/agent/cli_tasks.go runTaskRunOnce) and passes the row's own repo as
// the ground reference, so the resolution cannot be hijacked by a relative
// or foreign cwd (TaskRepoForLaunch's measured trap).
//
// It is the SAME single writer as the creation seam, with the SAME
// rules — parse/rebuild merge (the ONE blob reader/writer), refuse on a row
// that already names a worktree, CAS the UPDATE on the raw blob. Nothing
// here is a second source of truth: the completion path overwrites/confirms
// the same keys, and a row the executor later moves re-stamps through the
// detection seam.
//
// Returns (worktree, branch, written) — the resolved ground and whether the
// row now carries it. Best-effort by contract, like every side-channel
// stamp: a resolution/Db failure logs via taskNotifyLog and reports false.
func taskWorktreeStampClaimer(taskID, repo, workdir string) (string, string, bool) {
	taskID = taskIDNormalize(taskID)
	if taskID == "" {
		return "", "", false
	}
	repo = strings.TrimSpace(repo)
	if repo == "" {
		return "", "", false // repo-less row — nothing to pair (no-op, not an error)
	}
	if strings.TrimSpace(workdir) == "" {
		wd, err := os.Getwd()
		if err != nil {
			taskNotifyLog("task %s: claim-time ground stamp skipped: no workdir and no process cwd: %v", taskID, err)
			return "", "", false
		}
		workdir = wd
	}
	wt, branch := taskGroundOwningDir(repo, workdir)
	if branch == "" {
		// The claim dir is the repo's MAIN checkout on an EXEMPT branch
		// (master/main — the ordinary fresh-claim shape) or not this repo's
		// ground at all. Normally not a failure: the executor's own
		// branch/worktree is created mid-run (Rule 6a solo shape) and stamped by
		// the creation seam the moment it exists.
		//
		// THE ONE CASE WHERE THAT IS TOO LATE (gap A): the run's ground was
		// ALREADY created before the claim — a resumed run, a provisioning step,
		// or a sibling process — and it is registered as this repo's LINKED
		// worktree. The creation seam may never fire for such a run (nothing
		// creates anything mid-run), so the row would stay {"repo":…} for its
		// whole life. Consider the registered candidates under the SAME
		// fail-closed ONE-OWNER guards; ambiguous or unprovable ⇒ no stamp plus
		// one INFO line (taskClaimGroundStampCandidate).
		oc := taskClaimGroundStampCandidate(taskID, repo)
		if oc.written {
			return oc.worktree, oc.branch, true
		}
		if strings.TrimSpace(oc.reason) != "" {
			// A candidate existed (or could not be listed/verified) and was NOT
			// bound: the ONE honest INFO line naming it (Rule 15).
			taskClaimGroundLog(taskClaimGroundCandidateLineFmt, taskID, oc.reason)
		}
		taskClaimGroundLog(taskClaimGroundNoBranchLineFmt, taskID, workdir, branch)
		return wt, branch, false
	}
	// Branch axis only (no linked worktree): the row can now CLAIM the branch,
	// so the same ONE-OWNER rule applies to it — a branch another row already
	// names is that row's, and binding this one to it is the foreign-owner
	// hijack the worktree guard refuses (the merger's attribution would then
	// name two owners for one branch). FAIL-CLOSED (Rule 15): an unprovable scan
	// is a refusal, never a proceed.
	if wt == "" {
		owner, oErr := taskBranchRowOwner(taskID, repo, branch)
		if oErr != nil {
			taskNotifyLog("task %s: claim-time ground stamp skipped: cannot prove branch %s is unclaimed: %v", taskID, branch, oErr)
			return wt, branch, false
		}
		if owner != "" {
			taskNotifyLog("task %s: claim-time ground stamp: branch %s is already named by row %s — not binding this row to another row's branch", taskID, branch, owner)
			return wt, branch, false
		}
	}
	// ONE WORKTREE, ONE OWNING ROW (the 2026-09-12 foreign-owner hijack, whose
	// repair was the never-rebind guard below). A directory probe alone cannot
	// tell "this run's ground" from "a directory this run merely stands in" —
	// a worker started inside another task's worktree would bind THIS row to
	// it, and the deliverable.worktree scan is what keeps a worktree alive
	// (reclaimSelfRowsFrom, the sweep's ownership arm). The occupation test is
	// therefore the missing half of the probe: a worktree ANOTHER row already
	// names belongs to that row, and this claim records nothing rather than
	// double-own it.
	//
	// FAIL-CLOSED (Rule 15): the guard's error is a REFUSAL to stamp, never a
	// proceed-with-no-owner — an unreadable ledger cannot prove the worktree
	// is unowned, and binding a directory another row names is the hijack the
	// guard exists to prevent.
	if wt != "" {
		owner, oErr := taskWorktreeOwnerScan(taskID, wt)
		if oErr != nil {
			taskNotifyLog("task %s: claim-time ground stamp skipped: cannot prove %s is unowned: %v", taskID, wt, oErr)
			return wt, branch, false
		}
		if owner != "" {
			taskNotifyLog("task %s: claim-time ground stamp: %s is already named by row %s — not binding this row to another row's worktree", taskID, wt, owner)
			return wt, branch, false
		}
	}
	written, err := taskBackfillDeliverableWorktreeAt(taskID, wt, branch, &repo, taskWorktreeStampMomentClaim)
	if err != nil {
		taskNotifyLog("task %s: claim-time ground stamp skipped: %v", taskID, err)
		return wt, branch, false
	}
	if !written {
		// The ONE writer refused: the row already names a worktree (never
		// rebound — the 2026-09-12 foreign-owner hijack) or the blob moved
		// under the CAS. The run is still on the ground resolved above; the
		// ROW is simply not re-pointed.
		return wt, branch, false
	}
	if _, offPolicy := taskOffPolicyWorktreePath(wt); offPolicy {
		taskNotifyOffPolicyWorktree(taskID, wt, branch)
	}
	return wt, branch, true
}

// TaskWorktreeStampClaimerFor is the exported claim-time seam: the wrapper
// that just claimed taskID records the ground the run is about to execute in
// (repo is the row's deliverable.repo, workdir the directory the executor
// child will start in). See taskWorktreeStampClaimer for the contract.
func TaskWorktreeStampClaimerFor(taskID, repo, workdir string) (wt, branch string, written bool) {
	return taskWorktreeStampClaimer(taskID, repo, workdir)
}

// THE CLAIM-TIME CANDIDATE (gap A, epic 01a0bb4d's residual half).
//
// The seam above reads the ground of the directory the child is ABOUT to start
// in. When that directory IS the row's repo — the dominant wrapper shape — the
// main-checkout arm returns the repo's own HEAD branch, which at claim time is
// master: EXEMPT, so nothing is stamped and the row keeps its submit-time
// {"repo":…} blob. That is correct for a run that will create its ground
// later (the creation seam catches it), and wrong for the run whose worktree
// was ALREADY created — by an earlier step, a resumed run, or a sibling
// process that provisioned the ground before the claim. Measured on the live
// ledger (2026-09-20): task 01a0bb71-ec9d sat in_progress with {"repo":…}
// while /home/user/workspace/gopi-u1-editorrows existed and was registered as
// that repo's linked worktree on a non-exempt branch; its sibling row
// 01a0bb71-eae2, claimed through the OTHER claim shape (cwd = the worktree),
// recorded the same ground immediately. So the blind spot is the main-checkout
// claim path, and the ground was registered and readable the whole time.
//
// WHAT THIS DOES: when the claim dir resolves to the repo's main checkout on
// an exempt branch AND the row names no worktree yet, consider the repo's
// registered LINKED worktrees and accept ONE candidate —
//   - the worktree must exist on disk and carry a non-exempt branch, and
//   - BOTH ONE-OWNER guards must answer cleanly and cleanly-empty:
//     taskWorktreeRowOwner (nobody else names this DIRECTORY) and
//     taskBranchRowOwner (nobody else claims this BRANCH), each FAIL-CLOSED —
//     an error is a refusal, never a proceed (Rule 15).
//
// Fail-closed in the other direction too: a repo with TWO live candidates is
// AMBIGUOUS, and a guess would bind the row to a directory another run may
// own — precisely the 2026-09-12 foreign-owner hijack, whose repair is the
// never-rebind guard this path must not re-open through the other door. An
// ambiguous (or refused) set stamps NOTHING and leaves ONE line naming the
// candidate(s) it considered — the honest outcome, not a silent one.
type taskClaimCandidate struct {
	worktree string
	branch   string
}

// taskClaimGroundCandidates are the linked worktrees of repo that could be the
// claiming run's ground, in `git worktree list` order, with the DETACHED and
// EXEMPT-branch records dropped (neither is an adoptable deliverable: a
// detached worktree names no branch, and master/main/archive-* is not a
// deliverable — taskBranchExempt).
//
// An error from the worktree listing is returned, never swallowed: its caller
// decides by refusing (Rule 15).
func taskClaimGroundCandidates(repo string) ([]taskClaimCandidate, error) {
	linked, err := taskRepoLinkedWorktrees(repo)
	if err != nil {
		return nil, err
	}
	cands := make([]taskClaimCandidate, 0, len(linked))
	for _, e := range linked {
		if e.branch == "" || taskBranchExempt(e.branch) {
			continue
		}
		cands = append(cands, taskClaimCandidate{worktree: e.path, branch: e.branch})
	}
	return cands, nil
}

// taskClaimGroundSessionReasonFmt *and* the session reading below are the SAME
// sentence the advisory show line prints (gap B) — ONE reading of the run's own
// session, two faces. The reason is written through the claim INFO line.

// taskGroundSessionReading is the SHARED reading of a row's run ground: what
// the run's own session names, whether it was VERIFIED to still exist, and the
// honest why when it could not be resolved. It is the ONE session reading of
// this package, used by BOTH faces the epic asks for:
//
//	① the claim-time CANDIDATE (gap A): the claim dir is the repo's main
//	   checkout on an exempt branch, so the directory probe says nothing — the
//	   run's OWN session may already name the ground it will use;
//	② the ADVISORY show line (gap B): `task show` renders what the row
//	   RECORDS; this reading is what it advises.
//
// It reads the JSONL through the EXISTING session scan (taskGroundFromSession —
// the same reader the terminal writeback uses, so the two can never disagree
// about what a session names), resolves the log through the EXISTING cost-aware
// lookup (taskFindSessionJSONLForRow), and verifies the branch with the
// EXISTING verifier (taskBranchExists). Nothing here is a second parser
// (Rules 13/14).
type taskGroundSessionReading struct {
	repo       string
	worktree   string
	branch     string
	worktreeOK bool
	branchOK   bool
	why        string
}

// taskGroundSessionReadingFor resolves the ground a row's run stands on, from
// the run's own session, WITHOUT writing anything (read-only by construction:
// a session scan, a git resolution, a `git rev-parse --verify`).
//
// repoRow is the repo the row records ("" = none); it is PREFERRED as the
// ground reference, and the session's own launch directory answers only when
// the row records none (taskLaunchRepo's measured trap: a foreign reference
// makes a real reading look foreign).
//
// resolve is the worktree resolution the caller wants used — injected so each
// face pays only what it needs (the claim path asks the repo's REGISTERED
// worktrees once, the show path asks the single directory) and so the tests can
// drive the failure branches deterministically. A nil resolve leaves the
// worktree axis empty.
func taskGroundSessionReadingFor(taskID, sessionID, repoRow string,
	resolve func(repo, branch string) (worktree string, ok bool)) taskGroundSessionReading {
	r := taskGroundSessionReading{repo: strings.TrimSpace(repoRow)}
	sessionID = strings.TrimSpace(sessionID)
	gd, err := config.GopiDir()
	if err != nil {
		r.why = fmt.Sprintf(taskGroundWhyNoSessionFmt, sessionID)
		return r
	}
	path := taskFindSessionJSONLForRow(filepath.Join(gd, config.SessionsDirName), taskID, sessionID)
	if path == "" {
		r.why = fmt.Sprintf(taskGroundWhyNoSessionFmt, sessionID)
		return r
	}
	wd := taskWorkdirFromSession(sessionID)
	if wd == "" {
		r.why = fmt.Sprintf(taskGroundWhyNoWorkdirFmt, sessionID)
		return r
	}
	groundRepo := taskLaunchRepo(sessionID, wd)
	if groundRepo == "" {
		r.why = fmt.Sprintf(taskGroundWhyNoGroundRepoFmt, sessionID, wd)
		return r
	}
	if r.repo == "" {
		r.repo = groundRepo
	}
	addBranch, createdBranch := taskGroundFromSession(sessionID, groundRepo)
	branch := addBranch
	if branch == "" {
		branch = createdBranch
	}
	if branch == "" {
		r.why = fmt.Sprintf(taskGroundWhyNoReadingFmt, sessionID)
		return r
	}
	r.branch = branch
	if !taskBranchExists(groundRepo, branch) {
		r.why = fmt.Sprintf(taskGroundWhyStaleFmt, sessionID, branch, groundRepo)
		return r
	}
	r.branchOK = true
	// The worktree axis is filled ONLY for a worktree-ADD reading: an in-place
	// creation (`git checkout -b` in the main checkout) has no worktree by
	// construction, and asking the resolver would return the repo's own
	// directory — reporting a ground the run does not have.
	if addBranch != "" && resolve != nil {
		if wt, ok := resolve(groundRepo, addBranch); ok && wt != "" {
			r.worktree, r.worktreeOK = wt, true
		}
	}
	r.why = fmt.Sprintf(taskGroundWhyResolvedFmt, sessionID)
	return r
}

// The WHY suffixes of the shared reading — each the honest words an operator
// reads when a reading is partial (Rule 15: stated, never silent, never dressed
// up as resolved).
const (
	taskGroundWhyNoSessionFmt    = "the row's run session could not be located (session %q)"
	taskGroundWhyNoWorkdirFmt    = "the row's session %q records no launch directory — no ground reference to judge against"
	taskGroundWhyNoGroundRepoFmt = "the row's session %q launched in %s, which is not a git work tree"
	taskGroundWhyNoReadingFmt    = "the row's session %q names no worktree-add and no in-place branch creation"
	taskGroundWhyStaleFmt        = "the row's session %q names branch %q, which no longer exists in %s — the reading is stale"
	taskGroundWhyResolvedFmt     = "resolved from the row's own session %q"
)

// TaskWorktreeStampClaimerCandidateFor is the exported claim-time CANDIDATE
// entry (gap A): called when the executor child starts in the row's repo's MAIN
// checkout on an exempt branch, so the directory probe proves nothing. See
// taskClaimGroundStampCandidate for the guards and the contract.
func TaskWorktreeStampClaimerCandidateFor(taskID, repo string) (wt, branch string, written bool) {
	oc := taskClaimGroundStampCandidate(taskID, repo)
	if !oc.written && strings.TrimSpace(oc.reason) != "" {
		// The ONE honest INFO line naming the candidate considered (Rule 15).
		taskClaimGroundLog(taskClaimGroundCandidateLineFmt, taskIDNormalize(taskID), oc.reason)
	}
	return oc.worktree, oc.branch, oc.written
}

// it resolved, whether the ROW was written, and the honest reason when nothing
// was bound ("" = the candidate path had nothing to say — the row already
// names a ground, or names another repo).
type taskClaimCandidateOutcome struct {
	worktree string
	branch   string
	written  bool
	reason   string
}

// taskClaimGroundCandidate is the resolver proper, run by the claim-time
// stamp when the claim dir is the repo's MAIN checkout on an exempt branch.
// See taskClaimGroundStampCandidate's doc for when it is called and why.
//
// taskID is the row being claimed; repo its own deliverable.repo.
func taskClaimGroundCandidate(taskID, repo string) taskClaimCandidateOutcome {
	cands, err := taskClaimGroundCandidates(repo)
	if err != nil {
		return taskClaimCandidateOutcome{reason: fmt.Sprintf("cannot list %s's linked worktrees: %v", repo, err)}
	}
	// THE RUN'S OWN SESSION IS REQUIRED EVIDENCE (gap A, second half).
	//
	// The registry alone CANNOT decide this, and the measured case is why. A
	// claim whose cwd is the repo's MAIN checkout records that directory, whose
	// HEAD at claim time is master — EXEMPT — so the directory probe learns
	// nothing (that is the whole reason this seam exists). Two worktrees can be
	// registered while the claim dir names neither, leaving the registry's own
	// verdict unprovable. What makes the reading PROVABLE is the row's OWN
	// session: a `git worktree add` the run executed names the ground that run
	// chose. Row 01a0bb71-ec9d (measured 2026-09-20) is exactly this shape —
	// its session launched in the MAIN checkout, indistinguishable from its
	// sibling's, and only the executed worktree-add separates them.
	//
	// So the session reading is a PRECONDITION, not a tie-breaker, and that is
	// a CORRECTNESS requirement rather than caution: in gap A's shape the run
	// standing in the worktree records NOTHING on its own row, so
	// taskWorktreeRowOwner and taskBranchRowOwner — which scan for rows that
	// NAME the worktree/branch — are blind to it BY CONSTRUCTION. A
	// registry-only bind would therefore hand this row a LIVE foreign run's
	// worktree (the 2026-09-12 foreign-owner hijack, one level up). Registry-
	// only claims stay repo-only, exactly as before this seam existed — pinned
	// by TestClaimTimeGroundStampLeavesMainCheckoutRowRepoOnly.
	reading := taskGroundSessionReadingFor(taskID, taskSessionIDForRow(taskID), repo, taskGroundResolveList)
	switch {
	case reading.worktree != "" && reading.worktreeOK:
		// The run's own executed worktree-add, resolved to a registered
		// worktree of this repo. Nothing else narrows, and nothing else binds.
		cands = taskClaimNarrowToSession(cands, reading)
		if len(cands) != 1 || cands[0].worktree != reading.worktree || cands[0].branch != reading.branch {
			// The session's ground is real but not adoptable HERE (the repo no
			// longer registers it, or the branch moved under it). Binding a
			// DIFFERENT registered worktree would be the very guess this seam
			// exists to refuse.
			return taskClaimCandidateOutcome{reason: fmt.Sprintf("the row's session names %s (%s), which %s does not register as a linked worktree on a non-exempt branch — nothing to bind",
				reading.worktree, reading.branch, repo)}
		}
	case reading.branch != "" && reading.branchOK:
		// An IN-PLACE creation (no worktree): the ground is the repo's main
		// checkout on that branch. There is no worktree to bind, and this seam
		// writes worktree+branch as a PAIR — so it is reported and not bound
		// (the terminal writeback adopts it at completion, with the same
		// verification).
		return taskClaimCandidateOutcome{reason: fmt.Sprintf("%s's session names in-place branch %s (no worktree to bind — the writeback adopts it at completion)",
			repo, reading.branch)}
	default:
		// NO PROVABLE EVIDENCE: the row's run cannot be identified from its own
		// session, so which registered worktree (if any) it stands on cannot be
		// established. The candidate set is NOT a substitute for that — say so.
		return taskClaimCandidateOutcome{reason: fmt.Sprintf("the row's run session proves no ground at %s: %s", repo, reading.why)}
	}
	if len(cands) > 1 {
		return taskClaimCandidateOutcome{reason: fmt.Sprintf("%s registers %d candidate worktrees (%s) — the claim dir names none of them, so which one this run works in is unprovable",
			repo, len(cands), taskClaimCandidateList(cands))}
	}
	c := cands[0]
	// ONE WORKTREE, ONE OWNING ROW — the SAME guard the ground probe runs
	// (taskWorktreeOwnerScan), through the SAME seam, so the fail-closed
	// branch stays observable and there is no second opinion about ownership.
	owner, oErr := taskWorktreeOwnerScan(taskID, c.worktree)
	if oErr != nil {
		return taskClaimCandidateOutcome{reason: fmt.Sprintf("cannot prove %s is unowned: %v", c.worktree, oErr)}
	}
	if owner != "" {
		return taskClaimCandidateOutcome{reason: fmt.Sprintf("%s is already named by row %s", c.worktree, owner)}
	}
	// ONE BRANCH, ONE OWNING ROW — the branch axis of the same rule.
	bOwner, bErr := taskBranchRowOwner(taskID, repo, c.branch)
	if bErr != nil {
		return taskClaimCandidateOutcome{reason: fmt.Sprintf("cannot prove branch %s is unclaimed: %v", c.branch, bErr)}
	}
	if bOwner != "" {
		return taskClaimCandidateOutcome{reason: fmt.Sprintf("branch %s is already named by row %s", c.branch, bOwner)}
	}
	return taskClaimCandidateOutcome{worktree: c.worktree, branch: c.branch}
}

// taskClaimNarrowToSession keeps only the candidate the row's own session
// named — the run's executed worktree-add is EVIDENCE, so the set it names is
// what the run actually chose. A session naming a worktree the repo does not
// register leaves the set untouched (nothing was proven) and the caller falls
// back to the registry's own verdict.
func taskClaimNarrowToSession(cands []taskClaimCandidate, reading taskGroundSessionReading) []taskClaimCandidate {
	for _, c := range cands {
		if c.worktree == reading.worktree && c.branch == reading.branch {
			return []taskClaimCandidate{c}
		}
	}
	return cands
}

// taskSessionIDForRow reads the session id the row currently records — the
// handle the shared reading resolves the run's log with.
func taskSessionIDForRow(taskID string) string {
	ctx, cancel := context.WithTimeout(context.Background(), taskWorktreeBackfillTimeout)
	defer cancel()
	db, err := openTasksDB(ctx, memoryDBPath())
	if err != nil {
		return ""
	}
	defer db.Close()
	task, err := taskByID(ctx, db, taskIDNormalize(taskID))
	if err != nil || task == nil {
		return ""
	}
	return strings.TrimSpace(task.SessionID)
}

// taskClaimCandidateList renders candidates for the ambiguity line — the
// "worktree (branch)" pairs, comma-separated, in listing order.
func taskClaimCandidateList(cands []taskClaimCandidate) string {
	parts := make([]string, 0, len(cands))
	for _, c := range cands {
		parts = append(parts, fmt.Sprintf("%s (%s)", c.worktree, c.branch))
	}
	return strings.Join(parts, ", ")
}

// taskClaimGroundStampCandidate is the claim-time seam's CANDIDATE half: it
// runs the candidate resolver and, when one candidate survives every guard,
// writes it through the SAME blob writer as every other ground stamp (Rules
// 13/14). It refuses to touch a row that already names a branch or a worktree
// — the claim-time pair is only ever a CLOSURE of an empty field, never a
// re-point (the 2026-09-12 hijack guard, applied one level up).
func taskClaimGroundStampCandidate(taskID, repo string) taskClaimCandidateOutcome {
	taskID = taskIDNormalize(taskID)
	repo = strings.TrimSpace(repo)
	if taskID == "" || repo == "" {
		return taskClaimCandidateOutcome{} // nothing to resolve against — say nothing
	}
	// The row's CURRENT ground: a row that already names either axis has a
	// ground, and this seam exists to close an EMPTY one.
	rowRepo, rowBranch, rowWT := taskRowDeliverable(taskID)
	if rowRepo != "" && rowRepo != repo {
		return taskClaimCandidateOutcome{} // the row moved repos — not this seam's business
	}
	if rowBranch != "" || rowWT != "" {
		return taskClaimCandidateOutcome{} // already grounded — grounded ≠ refused, no line
	}
	oc := taskClaimGroundCandidate(taskID, repo)
	if oc.reason != "" || oc.worktree == "" || oc.branch == "" {
		return oc
	}
	written, err := taskBackfillDeliverableWorktreeAt(taskID, oc.worktree, oc.branch, &repo, taskWorktreeStampMomentClaim)
	if err != nil {
		oc.written, oc.reason = false, fmt.Sprintf("bind to %s failed: %v", oc.worktree, err)
		return oc
	}
	oc.written = written
	if !written {
		// The ONE writer refused (the blob moved under the CAS, or it gained a
		// ground while we resolved). The run is still on the candidate; the ROW
		// is simply not re-pointed.
		oc.reason = fmt.Sprintf("%s %s was resolved but the row refused or moved — not bound", oc.worktree, oc.branch)
		return oc
	}
	if _, offPolicy := taskOffPolicyWorktreePath(oc.worktree); offPolicy {
		taskNotifyOffPolicyWorktree(taskID, oc.worktree, oc.branch)
	}
	return oc
}

// taskClaimGroundCandidateLineFmt is the ONE spelling of the claim-time
// candidate INFO line (Rule 1b: the message shape lives in the constant, never
// at the call site). It is emitted ONLY when the candidate path ran and bound
// nothing — so a repo with no registered candidates, an ambiguous candidate
// set, and a refused candidate are all SAYABLE (Rule 15), while an ordinary
// master-checkout claim keeps its previous single line.
const taskClaimGroundCandidateLineFmt = "task %s: claim-time ground stamp: no candidate bound at the main checkout — %s"

// taskClaimGroundNoBranchLineFmt is the pre-existing INFO line for the
// ordinary master-checkout claim (see the call site) — named here (Rule 1b)
// because it is now written through the same capture seam as the candidate
// line, and a test reads it by constant.
const taskClaimGroundNoBranchLineFmt = "task %s: claim-time ground stamp: no non-exempt branch at %s (branch %q) — the run's ground is stamped when the executor creates it"

// taskWorktreeOwnerScan is the package-level seam over taskWorktreeRowOwner:
// production binds the ledger scan, and the ownership-guard tests rebind it to
// drive the FAIL-CLOSED branch deterministically. Rebinding the seam (rather
// than breaking the ledger) is required for that branch to be observable at
// all: an unreadable ledger fails at openTasksDB in EVERY writer on this path,
// so a ledger-level corruption test cannot separate "the guard refused" from
// "the write would have refused anyway" — the A/B on that shape passes either
// way, i.e. it proves nothing. Precedent: taskLiveHolderProbe (tasks.go).
var taskWorktreeOwnerScan = taskWorktreeRowOwner

// taskWorktreeRowOwner returns the task_id of ANOTHER row whose deliverable
// already names worktree wt ("" when none or when the only row naming it is
// taskID itself). The worktree key's consumer set treats a named worktree as
// an OWNERSHIP claim — task_reclaim_self.go's `deliverable LIKE '%worktree%'`
// scan and the sweep's ownership arm both keep the directory alive for the
// row that names it — so a claim-time stamp must not mint a second owner
// (the 2026-09-12 foreign-owner hijack). Read-only, single query.
//
// THE ERROR IS RETURNED, NOT SWALLOWED (Rule 15), and its meaning is
// FAIL-CLOSED: "no owner could be PROVEN" is not "there is no owner". An
// unreadable or truncated scan (a wedged ledger, rows.Err()) reported "" would
// read as permission to bind, and the consequence of binding a worktree that
// another row already names is exactly the hijack this guard exists to refuse
// — two rows owning one directory, each blocking the other's recycle. The
// caller therefore treats a non-nil error as "refuse to stamp", the same
// direction as every other unresolved-ground outcome. (Same family as the
// sweep loader's "no needle may truncate an axis" repair: a silent partial
// read must never masquerade as a complete answer.)
func taskWorktreeRowOwner(taskID, wt string) (string, error) {
	wt = strings.TrimSpace(wt)
	if wt == "" {
		return "", nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), taskWorktreeBackfillTimeout)
	defer cancel()
	db, err := openTasksDB(ctx, memoryDBPath())
	if err != nil {
		return "", err
	}
	defer db.Close()
	rows, err := db.QueryContext(ctx,
		`SELECT task_id, deliverable FROM tasks WHERE deliverable LIKE ?`,
		"%"+DeliverableWorktreeKey+"%")
	if err != nil {
		return "", err
	}
	defer rows.Close()
	for rows.Next() {
		var id, raw string
		if err := rows.Scan(&id, &raw); err != nil {
			return "", err
		}
		if id == taskID {
			continue
		}
		existing := taskDeliverableParse(raw).Worktree
		if existing == wt {
			return id, nil
		}
	}
	if err := rows.Err(); err != nil {
		return "", err
	}
	return "", nil
}

// taskBranchRowOwner returns the task_id of ANOTHER row that already CLAIMS
// branch of repo ("" when none, or when the only row naming it is taskID itself).
// It is the BRANCH axis of the ONE-OWNER rule taskWorktreeRowOwner enforces for
// the worktree axis (epic 01a0b182): a row that names a branch is the row the
// merger's attribution reports as that branch's owner (branchAttributionRows →
// BranchAttribution → the "task <id>" clause), so a second row claiming the same
// branch would make the attribution name two owners for one branch — the same
// double-ownership the worktree guard exists to refuse, and the state a
// branch-only stamp could otherwise walk into.
//
// THE REPO HALF IS PART OF THE QUESTION, not an extra check: branch names are
// per-repository, so feat/x in repo A says nothing about feat/x in repo B — and a
// branch-only row records no directory to tell them apart (that is what
// branch-only means). The candidate rows' own deliverable.repo is therefore the
// discriminator, read from the same ONE blob parser the branch comes from.
//
// THE ERROR IS RETURNED, NOT SWALLOWED (Rule 15), with the same FAIL-CLOSED
// meaning as the worktree scan: "no owner could be PROVEN" is not "there is no
// owner" — an unreadable or truncated scan (rows.Err()) reported as "" would read
// as permission to bind, and the consequence is two rows owning one branch.
func taskBranchRowOwner(taskID, repo, branch string) (string, error) {
	repo = strings.TrimSpace(repo)
	branch = strings.TrimSpace(branch)
	if repo == "" || branch == "" {
		return "", nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), taskWorktreeBackfillTimeout)
	defer cancel()
	db, err := openTasksDB(ctx, memoryDBPath())
	if err != nil {
		return "", err
	}
	defer db.Close()
	rows, err := db.QueryContext(ctx,
		`SELECT task_id, deliverable FROM tasks WHERE deliverable LIKE ?`,
		"%"+DeliverableBranchKey+"%")
	if err != nil {
		return "", err
	}
	defer rows.Close()
	for rows.Next() {
		var id, raw string
		if err := rows.Scan(&id, &raw); err != nil {
			return "", err
		}
		if id == taskID {
			continue
		}
		rowDel := taskDeliverableParse(raw)
		rowRepo, rowBranch := rowDel.Repo, rowDel.Branch
		if rowBranch == branch && sameWorkingPath(rowRepo, repo) {
			return id, nil
		}
	}
	if err := rows.Err(); err != nil {
		return "", err
	}
	return "", nil
}

// taskBackfillDeliverableWorktreeAt is taskBackfillDeliverableWorktree
// against an EXPLICIT ground reference (groundRef non-nil = the caller
// states the row's repo; nil = the row's own deliverable.repo is the only
// reference, the creation seam's rule). It reports the write failure to its
// caller — the seam above logs it with the claim-time wording, the
// detection seam keeps its log-and-continue contract.
func taskBackfillDeliverableWorktreeAt(taskID, worktree, branch string, groundRef *string, moment string) (bool, error) {
	taskID = taskIDNormalize(taskID)
	worktree = strings.TrimSpace(worktree)
	branch = strings.TrimSpace(branch)
	// The BRANCH axis alone is a complete ground (epic 01a0b182): a run working
	// in the repo's MAIN checkout delivers its branch and no worktree. Only "no
	// worktree AND no branch" carries nothing to record.
	if taskID == "" || (worktree == "" && branch == "") {
		return false, nil
	}
	ctx := context.Background()
	db, err := openTasksDB(ctx, memoryDBPath())
	if err != nil {
		return false, err
	}
	defer db.Close()
	t, err := taskByID(ctx, db, taskID)
	if err != nil {
		return false, err
	}
	if t == nil {
		return false, nil // row gone — nothing to backfill
	}
	raw := strings.TrimSpace(t.Deliverable)
	if raw == "" {
		return false, nil // no deliverable blob (auto mirror rows) — nothing to merge
	}
	// Parse + MERGE (taskDeliverableParse/Rebuild): the rebuild preserves
	// any attachments array already in the blob — a worktree backfill must
	// never drop the Phase-1 attachment manifest (the row's assets stay
	// addressable through the whole repo→repo+branch→repo+branch+worktree
	// stamp chain). Non-JSON blobs are left untouched (return nil — the
	// merge simply cannot happen; consistent with the repo-less case).
	parsed := taskDeliverableParse(raw)
	repo, branchExisting, worktreeExisting := parsed.Repo, parsed.Branch, parsed.Worktree
	if repo == "" {
		return false, nil // repo-less blob — nothing to pair
	}
	if groundRef != nil && strings.TrimSpace(*groundRef) != repo {
		// The caller resolved the ground against a DIFFERENT repo than the
		// blob now names (a concurrent re-stamp moved it). A claim-time
		// stamp must never bind the row to another project's worktree.
		return false, nil
	}
	// A row that ALREADY names a worktree keeps it (2026-09-12 hijack fix):
	// the backfill's job is to CLOSE an information gap — stamp the worktree
	// the run works in when the row has none. Overwriting an existing stamp
	// re-bound rows to whatever directory the executor last touched: a probe
	// such as `git -C ~/workspace/gopi-OTHER status` (a read-only look at
	// another task's worktree, the routine form of a cross-task comparison)
	// re-pointed THIS row's deliverable at that other worktree, so the row
	// lost its own directory and became a FOREIGN OWNER of someone else's.
	// That is exactly the lock observed live (row 01a09250's deliverable
	// flipped from gopi-wtrecycle2 to gopi-cov-agent mid-run) and it is what
	// wedges the finish-cleanup ownership gate for every row involved: the
	// hijacker blocks the real owner's recycle, and the real owner blocks the
	// hijacker's. A genuinely moved run does not rely on this path — the move
	// is recorded at reconciliation (taskDeliverable*move) and by the row's
	// own finish writeback; a DETECTION seam must only ever ADD information.
	//
	// The guard is the WORKTREE axis's, and it stays exactly that (epic
	// 01a0b182): a branch-only stamp (worktree=="" — the shape an in-place
	// `git checkout -b` produces) has no directory to hijack, and the row's
	// branch it would fill in is the same single-valued fact the completion
	// path confirms. Refusing a branch-only write here would keep the in-place
	// row repo-only for the whole run — the very gap this epic closes — while
	// protecting nothing: one branch name is one owner by construction, and the
	// caller's ownership scan (taskBranchRowOwner) is what enforces it.
	if worktree != "" && worktreeExisting != "" && worktreeExisting != worktree {
		return false, nil // already bound — never rebound by a detection probe
	}
	if branch == "" {
		branch = branchExisting
	}
	if worktree == "" {
		worktree = worktreeExisting
	}
	merged := taskDeliverableRebuild(taskDeliverable{Repo: repo, Branch: branch, Worktree: worktree, Grounds: parsed.Grounds, Attachments: parsed.Attachments})
	if merged == raw {
		return false, nil // already stamped — no write
	}
	res, err := db.ExecContext(ctx,
		`UPDATE tasks SET deliverable = ?, updated_at = ? WHERE task_id = ? AND deliverable = ?`,
		merged, float64(time.Now().Unix()), taskID, raw)
	if err != nil {
		return false, err
	}
	if n, err := res.RowsAffected(); err == nil && n > 0 {
		taskWorktreeStampLog(taskWorktreeStampLineFmt, taskID, worktree, branch, taskWorktreeStampMomentArg(moment))
		return true, nil
	}
	return false, nil // the CAS lost (a concurrent writer moved the blob) — no write
}
