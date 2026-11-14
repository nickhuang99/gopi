package tools

// Merged-branch cleanup (epic 2026-09-02, the operator's finding): the serve tick's
// merge paths (merge-backlog audit/executor, branch adoption ③a) merge a
// branch into master, then attempt `git branch -d <branch>` — which git
// REFUSES when the branch is checked out in a linked worktree ("Cannot
// delete branch ... checked out at ..."). The code tolerated that as a
// "reportable wart" (task_adoption.go:236, task_merge_executor.go:310), so
// the branch AND its worktree stayed forever: merged branches accumulate,
// `git branch` never shrinks, the cleanup work is never finished.
//
// This pass is the missing SERVER/INTEGRATION-side half: discover branches
// that are already IN master yet still exist as local branches with a
// linked worktree, and — when SAFE — remove the worktree, then delete the
// branch with -d. Fail-soft per branch (log + continue), like every other
// sweep.
//
// Safety argument (order = the guard chain; every guard is load-bearing):
//
//	① merged    — discovered via `git branch --merged master` (the branch
//	               tip is reachable from master). An UNMERGED branch is
//	               never touched (it still carries work master does not).
//	               CAVEAT (bug 01a08e8f, 2026-09-11): "reachable from master"
//	               is NOT the same as "was merged". A branch CREATED at the
//	               master tip and never committed to (`git worktree add -b X
//	               <path>` — the Rule 6a worktree a session opens FIRST, then
//	               reads code in) is trivially an ancestor of master, so
//	               `git branch --merged master` lists it and ① alone reads
//	               FRESH WIP as a merged leftover. Guard ①b below is what
//	               tells the two apart.
//	①b not-wip  — the pair must be a DELIVERY, not a session's fresh
//	               worktree. Spared (skipped) when the branch NEVER MOVED
//	               since it was created (its reflog carries only the
//	               creation entry → nobody committed on it) AND no ledger
//	               row claims its branch. Rationale (bug 01a08e8f): such a
//	               branch is trivially an ancestor of the integration
//	               branch, so ① lists a worktree a session has just opened
//	               (`git worktree add -b X`, then reading code) right
//	               beside a genuine merged leftover — and reaping it
//	               destroys the session's ground and its branch ref for
//	               NOTHING. The discriminator is a PROPERTY of the
//	               worktree, deliberately not a snapshot of where the
//	               integration branch sits right now (master advancing
//	               after the worktree was created must not re-open the
//	               hole). Evidence requirement = "never reap what no
//	               delivery ever owned".
//	② linked wt — the branch must be checked out in a linked worktree
//	               (`git worktree list --porcelain`). A branch without a
//	               worktree needs no cleanup here (branch -d alone is the
//	               merge path's own job).
//	③ clean     — the worktree's `git status --porcelain` must be EMPTY
//	               (untracked included — a dirty worktree is a LIVE BOND
//	               still writing there; never pull the ground out).
//	④ recheck   — `git merge-base --is-ancestor <branch> master`
//	               re-checked at removal time (race-free): the discovery
//	               snapshot may be stale — a Bond that recommitted after
//	               its branch was merged moved the branch ref OFF master.
//	④b on-tip   — the worktree HEAD must equal the branch tip. A worktree
//	               parked on an old commit (branch ref moved elsewhere) is
//	               not a merged-tip checkout — skip, never guess.
//	⑤ no live   — no LIVE ledger task (pending/claimed/in_progress) may
//	               reference the branch (deliverable.branch match or the
//	               "merge <branch> into master" summary). A live task = a
//	               Bond may be working it; skip. Terminal rows and rows
//	               with NO match are fine — the branch's commits are in
//	               master by construction (①), so no further work can be
//	               owed through this branch.
//
// Only after ①–⑤ all pass: `git worktree remove <path>` then
// `git branch -d <branch>` (in the MAIN repo dir). -d succeeds because the
// branch is an ancestor of master (①) and no longer checked out (② cleared).
//
// Exemptions (never cleaned — the finish-cleanup chain's set): master/main
// and archive branches (the loser-archive ledger), plus GOPI_FINISH_CLEANUP_
// EXEMPT additions. The MAIN checkout (master itself) is never a linked
// worktree target.

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
	"time"

	"github.com/nickhuang99/gopi/internal/removalaudit"
)

// Merged-branch cleanup tunables (Rule 1 — every literal in one place).
const (
	// mergedCleanupGitTimeout caps each git invocation (a wedged repo must
	// not stall the serve pass).
	mergedCleanupGitTimeout = 30 * time.Second

	// mergedCleanupDBTimeout caps the live-task ledger read.
	mergedCleanupDBTimeout = 10 * time.Second

	// mergedCleanupIntegrationBranch is the branch this sweep's discovery
	// and guard ①b both resolve against (`git branch --merged <it>` lists
	// the candidates; `HEAD == <it>` identifies an at-tip worktree). ONE
	// spelling for one rule inside this file.
	mergedCleanupIntegrationBranch = "master"

	// mergedCleanupBranchVerb / mergedCleanupMergedFlag spell the discovery
	// read `git branch --merged <integration>` whose OUTPUT IS PARSED (one name
	// per line) — hence the parsed-stdout reader (Rule 14), not CombinedOutput.
	mergedCleanupBranchVerb = "branch"
	mergedCleanupMergedFlag = "--merged"

	// mergedCleanupHeadArg is the revision the at-tip guard resolves in a
	// worktree (`git rev-parse HEAD`) — an object id, therefore parsed.
	mergedCleanupHeadArg = "HEAD"

	// mergedCleanupReflogVerb / mergedCleanupReflogShowVerb /
	// mergedCleanupReflogFormatFlag spell the moved-tip probe
	// (`git reflog show --format=%H <branch>`): one object id per entry,
	// COUNTED — parsed.
	mergedCleanupReflogVerb       = "reflog"
	mergedCleanupReflogShowVerb   = "show"
	mergedCleanupReflogFormatFlag = "--format=%H"

	// mergedCleanupReapedEvent is the synthetic notify event every reap
	// fires (subscribe via GOPI_TASK_NOTIFY_EVENTS / settings "notify"
	// events, exactly like the stale / build-failed alarms). The naming
	// follows the existing synthetic-event literals.
	mergedCleanupReapedEvent = "worktree-reaped"

	// mergedCleanupNoWorkMarker is the phrase the guard-①b skip carries —
	// one string for an operator (or a test) to match without knowing the
	// wording around it (the finishCleanupInUseMarker convention).
	mergedCleanupNoWorkMarker = "fresh work-in-progress worktree (its branch carries work no merge produced"

	// mergedCleanupLiveOwnerMarker is the phrase the guard-⑤ skip carries.
	mergedCleanupLiveOwnerMarker = "live task references the branch or worktree (active bond protected)"

	// mergedCleanupBranchJSONKey is the deliverable JSON field the branch
	// claim lives in — the directory scan's LIKE needle (the JSON decode
	// below stays the authority).
	mergedCleanupBranchJSONKey = `"branch"`

	// mergedCleanupBranchDeleteLabel is the operator-facing spelling of the
	// ref-only delete this sweep performs (finishCleanupBranchVerb +
	// finishCleanupBranchDeleteFlag). Composed from the verbs themselves, so
	// the label in a report can never drift from the git call it describes
	// (Rule 13).
	mergedCleanupBranchDeleteLabel = finishCleanupBranchVerb + " " + finishCleanupBranchDeleteFlag

	// mergedCleanupOrphanBranchNote is the note an orphan-branch reap carries
	// — one string for an operator (or a test) to match without knowing the
	// wording around it (the mergedCleanupNoWorkMarker convention).
	mergedCleanupOrphanBranchNote = "orphan branch reaped (merged, no worktree left by a prior removal)"

	// mergedCleanupOrphanWorktreeLabel stands in for the worktree path in a
	// report step for a branch that has none (the orphan-ref lane). A report
	// line that left the field empty would read as "the sweep does not know
	// what it deleted"; this names the shape explicitly.
	mergedCleanupOrphanWorktreeLabel = "(no worktree — orphan ref)"

	// mergedCleanupOrphanBranchExemptMarker / mergedCleanupOrphanLiveOwnerMarker
	// are the two skip phrases of the orphan-ref lane (the
	// mergedCleanupLiveOwnerMarker convention: one string, matchable without
	// knowing the wording around it).
	mergedCleanupOrphanBranchExemptMarker = "branch is exempt"
	mergedCleanupOrphanLiveOwnerMarker    = mergedCleanupLiveOwnerMarker
)

// mergedCleanupWorktreeOwnerRow is one ledger row's worktree claim, kept on
// BOTH axes: the directory it names AND the branch it delivers (a session's
// environment row records the worktree at creation, before the branch it
// will deliver exists as a distinct name).
type mergedCleanupWorktreeOwnerRow struct {
	Claimed  bool
	Terminal bool
}

// liveTaskStatuses is THE list of statuses meaning "a session still owes
// work through this row": queued (pending) or held by a live runner
// (claimed/in_progress). ONE source of truth (Rule 14) for both readers —
// the Go predicate below and the SQL IN list (taskLiveStatusSQLList) — so a
// status rename or a new live status cannot reach one and miss the other.
var liveTaskStatuses = []string{TaskStatusPending, TaskStatusClaimed, TaskStatusInProgress}

// isLiveTaskStatus reports whether a task status is a live one.
//
// merge-note (feat/worktree-live-owner-guard <- master): keep this — HEAD's
// own task_finish_cleanup.go taskLoadLiveClaims predicate (line ~1421) calls
// it, so dropping it breaks the build.
func isLiveTaskStatus(status string) bool {
	status = strings.TrimSpace(status)
	for _, s := range liveTaskStatuses {
		if status == s {
			return true
		}
	}
	return false
}

// merge-note: master grew its OWN taskLiveStatusSQLList in tasks.go (same
// three constants, rendered as an SQL IN literal — the canonical home). Only
// THIS file's copy is dropped, because two definitions of taskLiveStatusSQLList
// in one package is a redeclaration (compile error) and a second source of
// truth (Rule 14) for a set that must have exactly one. The live-status list
// and its Go predicate above are kept: they are this branch's intent and
// nothing on master replaces them.
// MergedCleanupStep is one branch's outcome (the report is the audit
// trail — which branch, what git said, where it stopped).
type MergedCleanupStep struct {
	Branch   string `json:"branch"`
	Worktree string `json:"worktree,omitempty"`
	OK       bool   `json:"ok"`
	Detail   string `json:"detail,omitempty"` // skip reason / git output
}

// MergedCleanupReport is the outcome of one cleanup pass.
type MergedCleanupReport struct {
	Repo    string              `json:"repo"`
	Removed []MergedCleanupStep `json:"removed,omitempty"` // worktree+branch gone
	Skipped []MergedCleanupStep `json:"skipped,omitempty"` // guarded, untouched
	Failed  []MergedCleanupStep `json:"failed,omitempty"`  // guard/step error
	// Reported carries the pairs this pass WOULD have reaped — the guard ladder
	// cleared, the ancestors recheck agreed, no live owner — when the caller
	// asked for the report-only disposition (MergedCleanupOptions.ReportOnly).
	// Nothing in this lane mutates a ref: the reaper's mutating arms are retired
	// (epic sweep-report-not-act, 2026-09-19), so a caller that still needs the
	// verdict gets it here instead of from a destroyed branch.
	Reported []MergedCleanupStep `json:"reported,omitempty"`
	// Notes carry the non-blocking observations a skip would otherwise hide
	// (a STALE holder binding, the removal-not-preferred rule): the sweep's
	// own report — one line per note, so a stranded worktree is visible in
	// the log instead of vanishing into a wordless skip (Rule 15).
	Notes []MergedCleanupStep `json:"notes,omitempty"`
}

// MergedCleanupOptions controls one cleanup pass.
type MergedCleanupOptions struct {
	// Repo is the git repo to clean ("" = cwd). Must be the main checkout
	// or inside it — linked worktrees are discovered from it.
	Repo string
	// ScanMax caps branches examined per pass (a runaway branch factory
	// cannot turn one tick into a fork bomb).
	ScanMax int
	// ReportOnly makes the pass NAME what it would have reaped and mutate
	// nothing — no worktree removal, no branch delete. Every production caller
	// sets it: the reaper's mutating arms are retired because under the
	// operator's model every branch has a destination and none is ever left
	// ownerless in refs/heads (epic sweep-report-not-act, 2026-09-19). It stays
	// a PARAMETER so the guard ladder's own tests can still exercise the
	// removal step they were written for, on their throwaway fixtures.
	ReportOnly bool
}

// mergedCleanupDefaultScanMax caps branches per pass when ScanMax <= 0.
const mergedCleanupDefaultScanMax = 20

// mergedCleanupIntegrationRef is the branch this sweep's guards ①/④ and its own
// delete lock all resolve against (Rule 2: one spelling, from the package
// constant — Rule 14, never a literal).
const mergedCleanupIntegrationRef = IntegrationBranchName

// finishCleanupSweepOwnerRow returns the row that recorded worktree as its
// deliverable home (nil when no row names the directory) — the sweep's
// removal-not-preferred test: the directory belongs to a row's delivery
// chain, not to this sweep's blind branch -d. A row whose holder is provably
// gone is returned too: its record is what keeps the directory out of the
// orphan path, and the owner chain (which may keep the branch) is still the
// better remover.
func finishCleanupSweepOwnerRow(owners []TaskWorktreeRow, worktree string) *TaskWorktreeRow {
	for i := range owners {
		o := owners[i]
		if !finishCleanupSameWorktreePath(o.Deliverable, worktree) {
			continue
		}
		if !o.HolderLive {
			return &o
		}
	}
	for i := range owners {
		o := owners[i]
		if finishCleanupSameWorktreePath(o.Deliverable, worktree) {
			return &o
		}
	}
	return nil
}

// finishCleanupSweepNotPreferred is the prefix of every note carrying the
// removal-not-preferred rule (the sweep saw a row-owned worktree and left it
// to that row's delivery chain). One string: the serve face and an operator's
// eyes read the same words (Rule 1).
const finishCleanupSweepNotPreferred = "row-owned worktree: "

// MergedCleanupNotePrefix is the prefix of every non-blocking observation in
// the sweep report (a Notes line) — one string for the serve face's renderer
// and the operator's eye (Rule 1: no bare literal at the render site).
const MergedCleanupNotePrefix = "note: "

// The evidence sentence the merged-branch removal records (removalaudit) —
// the phrase an operator greps to learn WHY the liveness guard cleared. The
// orphan-release half is appended from the classifier's own reading, so the
// record names the holder that was released, never merely the fact that one
// existed.
const (
	// mergedCleanupNoHolderEvidence is the ordinary reading: nothing was
	// standing in the directory at all.
	mergedCleanupNoHolderEvidence = "merged branch, no live claim, no live cwd, no foreign owner, no owner row"
	// mergedCleanupOrphanReleaseEvidence leads the release half.
	mergedCleanupOrphanReleaseEvidence = "merged branch, liveness guard cleared on orphaned cwd-inheritors"
)

// SweepOwnerRemoval reports whether a worktree owned by a task ROW was
// finally released by its owner's delivery chain (the sweep's remover), and
// what happened when it was not.
type SweepOwnerRemoval struct {
	// Removed = the directory is gone (the owner row's chain recycled it).
	Removed bool
	// Detail = one line for the sweep report (the chain's own skip reason
	// when it could not finish; what happened when it did).
	Detail string
}

// finishCleanupSweepOwnerRemover is the sweep's way to hand a row-owned
// worktree to the DELIVERY chain instead of removing it itself (wired by
// the command package, where the ledger-backed chain lives; nil in
// package-level tests and in any embedder without a ledger). See
// MergedCleanupSweep's removal-not-preferred rule.
var finishCleanupSweepOwnerRemover func(worktree string) SweepOwnerRemoval

// SetFinishCleanupSweepOwnerRemover wires (or clears, with nil) the
// row-owned-worktree remover used by MergedCleanupSweep. Called once at
// startup by the command package; a nil remover degrades to "leave the
// directory to its owner row's chain" — never to a blind removal.
func SetFinishCleanupSweepOwnerRemover(fn func(worktree string) SweepOwnerRemoval) {
	finishCleanupSweepOwnerRemover = fn
}

// FinishCleanupOwnerRowForWorktree resolves the task row that recorded
// worktree as its deliverable home — preferring a row whose holder is
// provably GONE (the stranded case the sweep is there to release). Returns
// nil when no row names the directory (nothing to hand over).
func FinishCleanupOwnerRowForWorktree(worktree string) (*TaskWorktreeRow, error) {
	owners, err := finishCleanupWorktreeOwnerRows("")
	if err != nil {
		return nil, err
	}
	var stale *TaskWorktreeRow
	for i := range owners {
		o := owners[i]
		if !finishCleanupSameWorktreePath(o.Deliverable, worktree) {
			continue
		}
		if !o.HolderLive {
			row := o
			stale = &row
			break
		}
		if stale == nil {
			row := o
			stale = &row
		}
	}
	return stale, nil
}

// MergedCleanupScanStatus renders one line describing what a sweep pass has
// to work with (linked worktree count + close-out candidate row count) —
// the serve face prints it every pass so a no-op sweep is still visible.
func MergedCleanupScanStatus(repo string) string { return mergedCleanupScanStatus(repo) }

// mergedCleanupScanStatusQuery lists the rows the sweep's candidate scan
// looks at, for the per-pass REPORT only: without it a sweep that examines
// rows and removes nothing logs nothing at all, and the operator cannot
// tell "nothing to do" from "everything was skipped" (the 2026-09-12
// incident: two log lines ever, both meaningless). Same predicate as the
// serve sweep's candidate query (terminal/cancelled + a deliverable naming
// a repo) minus the row cap — the count is the point.
const mergedCleanupScanStatusQuery = `SELECT COUNT(*) FROM tasks
	WHERE status IN ('` + taskStatusCompleted + `','` + taskStatusFailed + `','` + taskStatusCancelled + `','` + taskStatusSuperseded + `')
	  AND deliverable LIKE '%"repo"%'`

// mergedCleanupScanStatus renders one line describing what this pass had to
// work with: the linked worktrees git knows about, and how many ledger rows
// are close-out candidates. Best-effort: an unreadable ledger reports the
// error, never blocks the sweep (Rule 15 — reported, not swallowed).
func mergedCleanupScanStatus(repo string) string {
	wts := 0
	if byBranch, _, err := mergedCleanupWorktrees(repo); err == nil {
		wts = len(byBranch)
	}
	ctx, cancel := context.WithTimeout(context.Background(), mergedCleanupDBTimeout)
	defer cancel()
	db, err := openTasksDB(ctx, memoryDBPath())
	if err != nil {
		return fmt.Sprintf("%d linked worktree(s); ledger unreadable: %v", wts, err)
	}
	defer db.Close()
	var rows int
	if err := db.QueryRowContext(ctx, mergedCleanupScanStatusQuery).Scan(&rows); err != nil {
		return fmt.Sprintf("%d linked worktree(s); ledger scan failed: %v", wts, err)
	}
	return fmt.Sprintf("%d linked worktree(s), %d close-out candidate row(s)", wts, rows)
}

// MergedCleanupSweep runs one merged-branch cleanup pass over repo.
// Discover → guard (①–⑤) → remove → branch -d, fail-soft per branch.
// Returns the full report (the serve face renders the log lines).
func MergedCleanupSweep(opts MergedCleanupOptions) *MergedCleanupReport {
	repo := strings.TrimSpace(opts.Repo)
	if repo == "" {
		repo = "."
	}
	rep := &MergedCleanupReport{Repo: repo}
	scanMax := opts.ScanMax
	if scanMax <= 0 {
		scanMax = mergedCleanupDefaultScanMax
	}

	// Discover the linked worktrees once: map branch → worktree path, plus
	// the main checkout dir. The main checkout is where branch discovery,
	// the ancestor recheck and branch -d all run (the worktree-list's first
	// entry — the finish-cleanup --git-common-dir convention).
	wtByBranch, mainDir, err := mergedCleanupWorktrees(repo)
	if err != nil {
		rep.Failed = append(rep.Failed, MergedCleanupStep{Detail: "worktree list: " + err.Error()})
		return rep
	}
	if mainDir == "" {
		mainDir = repo
	}

	live, liveWTs, err := mergedCleanupLiveBranches()
	if err != nil {
		rep.Failed = append(rep.Failed, MergedCleanupStep{Detail: "ledger scan: " + err.Error()})
		return rep
	}

	// Ownership binding (2026-09-10 epic): the sweep resolves its removal
	// target from `git worktree list` — the branch→worktree map — so it can
	// land on a directory ANY row owns (or a re-attached directory two rows
	// claim). Load every row's recorded deliverable.worktree ONCE per pass
	// and refuse a target another row claims (terminal rows included: the
	// worktree is still that row's deliverable home).
	owners, err := finishCleanupWorktreeOwnerRows("")
	if err != nil {
		rep.Failed = append(rep.Failed, MergedCleanupStep{Detail: "ownership scan: " + err.Error()})
		return rep
	}

	// Ownership binding, second axis (bug 01a08e8f): every row's recorded
	// deliverable.branch, with the row's status. A session's own ledger row
	// is created BEFORE the worktree and carries the worktree path (the
	// shape the victim had); the branch is what makes the binding
	// reachable even after the directory has been re-created somewhere
	// else.
	branchClaims, err := mergedCleanupBranchClaims()
	if err != nil {
		rep.Failed = append(rep.Failed, MergedCleanupStep{Detail: "branch claim scan: " + err.Error()})
		return rep
	}

	branches, err := mergedCleanupMergedBranches(mainDir, scanMax)
	if err != nil {
		rep.Failed = append(rep.Failed, MergedCleanupStep{Detail: "branch discovery: " + err.Error()})
		return rep
	}
	for _, branch := range branches {
		wt, hasWT := wtByBranch[branch]
		if !hasWT {
			// No linked worktree → the PAIR lane below has nothing to do.
			// That is NOT the same as "nothing owable": a branch whose
			// worktree a prior removal already took (the merge executor, the
			// adoption chain, or the merge-build gate's own defer) keeps its
			// ref with NO registration at all — measured: `git worktree
			// remove --force` does not touch the branch, so every worktree
			// removal in the system leaves this shape behind. The orphan
			// lane collects it on the same guards, minus the worktree ones.
			mergedCleanupOrphanBranch(mainDir, branch, rep, live, branchClaims, opts)
			continue
		}
		if mergedCleanupBranchExempt(mainDir, branch) {
			rep.Skipped = append(rep.Skipped, MergedCleanupStep{
				Branch: branch, Worktree: wt, Detail: "branch is exempt"})
			continue
		}
		// ③ clean worktree.
		if reason := mergedCleanupDirtyReason(wt); reason != "" {
			rep.Skipped = append(rep.Skipped, MergedCleanupStep{
				Branch: branch, Worktree: wt, Detail: reason})
			continue
		}
		// ④ race-free merged recheck: the discovery's --merged snapshot may
		// be stale by now — a Bond that recommitted after its branch was
		// merged moved the branch ref (and the worktree HEAD with it, in
		// lockstep) OFF master. `merge-base --is-ancestor` re-checked NOW
		// catches it: only a branch STILL in-master at removal time is safe.
		if reason := mergedCleanupStillMergedReason(mainDir, branch); reason != "" {
			rep.Skipped = append(rep.Skipped, MergedCleanupStep{
				Branch: branch, Worktree: wt, Detail: reason})
			continue
		}
		// ④b worktree HEAD must equal the branch tip (a worktree parked on
		// an old commit while the branch ref moved elsewhere is not a
		// merged-tip checkout — skip, never guess what it holds).
		if reason := mergedCleanupOffTipReason(wt, branch, mainDir); reason != "" {
			rep.Skipped = append(rep.Skipped, MergedCleanupStep{
				Branch: branch, Worktree: wt, Detail: reason})
			continue
		}
		// ⑤ no live task referencing the branch OR its worktree. The
		// worktree half (bugfix epic 2026-09-03) protects a fresh worktree
		// the moment its row records deliverable.worktree at creation —
		// before the branch diverges from master (when the branch-based
		// match alone would miss it: branch still at master tip AND the
		// row's branch name may differ from the worktree's checked-out
		// branch until the executor commits under the row's branch).
		if live[branch] || liveWTs[wt] {
			rep.Skipped = append(rep.Skipped, MergedCleanupStep{
				Branch: branch, Worktree: wt, Detail: mergedCleanupLiveOwnerMarker})
			continue
		}
		// ①b a branch that never moved is not a merged leftover — it is a
		// session's brand-new worktree (bug 01a08e8f). Only a pair a ledger
		// row actually DELIVERED (a claim) may be reaped without committed
		// work of its own.
		if reason := mergedCleanupNoWorkReason(mainDir, wt, branch, branchClaims); reason != "" {
			rep.Skipped = append(rep.Skipped, MergedCleanupStep{
				Branch: branch, Worktree: wt, Detail: reason})
			continue
		}
		// ⑤b live CWD (2026-09-10): the ledger says the branch is free, but a
		// LIVE PROCESS may still sit inside the worktree — a session / build
		// whose CWD is the checkout, or a git process holding its private git
		// dir. Removing it mid-run tears the directory out from under that
		// process (bogus package-load failures; with a dirty tree, work loss).
		// Same liveness guard as the finish-cleanup chain — skipped branches
		// are reported, never silently kept. The refusal is checked against its
		// INPUT (epic worktree-reclaim-two-gaps, Gap A): a directory occupied
		// ONLY by orphaned cwd-inheritors — reparented to init, launcher record
		// dead, no live gopi process left in their session — is not held. The
		// release rides the removal's own audit evidence below, so it is
		// recorded rather than silent (Rule 15).
		orphanRelease := ""
		if _, why := finishCleanupWorktreeInUse(wt); why != "" {
			if _, orphanRelease = finishCleanupOrphanCwdInheritorReason(wt); orphanRelease == "" {
				rep.Skipped = append(rep.Skipped, MergedCleanupStep{
					Branch: branch, Worktree: wt, Detail: why})
				continue
			}
		}
		// ⑤c ownership binding (2026-09-10 epic): the branch→worktree map
		// above is git's view, not an ownership record — a directory can be
		// claimed by a DIFFERENT row (a re-attached worktree), and this
		// sweep would remove another row's deliverable home. Refuse any
		// target another row's deliverable.worktree names.
		//
		// A STALE binding (the holder is provably gone) is NOT a refusal
		// (2026-09-12): the row that recorded the directory is history, so
		// this sweep is the last thing left to release it. It is reported
		// and kept for the removal-not-preferred rule below.
		ownership, staleNote := finishCleanupForeignOwnerReason(owners, wt)
		if ownership != "" {
			rep.Skipped = append(rep.Skipped, MergedCleanupStep{
				Branch: branch, Worktree: wt, Detail: ownership})
			continue
		}
		// Removal-not-preferred (2026-09-12): when the directory is some
		// row's recorded deliverable home, the DELIVERY chain owns the
		// recycle — it holds the row's own branch/worktree record, the
		// dirty and live-CWD evidence and the branch-selection step, and its
		// removal KEEPS the branch (the server arm) instead of deleting it
		// here. This sweep releases the directory itself only when no row
		// names it (the orphan shape), or — with the serve sweep's remover
		// wired in — after that owner chain was actually tried and could not
		// finish.
		if owner := finishCleanupSweepOwnerRow(owners, wt); owner != nil {
			note := finishCleanupSweepNotPreferred + "row " + owner.TaskID + " (" + owner.Status + ") recorded it"
			if staleNote != "" {
				note += "; " + staleNote
			}
			if finishCleanupSweepOwnerRemover == nil {
				rep.Notes = append(rep.Notes, MergedCleanupStep{
					Branch: branch, Worktree: wt,
					Detail: note + " — the owner row's delivery chain owns the recycle"})
				continue
			}
			res := finishCleanupSweepOwnerRemover(wt)
			if !res.Removed {
				rep.Notes = append(rep.Notes, MergedCleanupStep{
					Branch: branch, Worktree: wt,
					Detail: note + " — the owner row's delivery chain did not release it: " + res.Detail})
				continue
			}
			rep.Notes = append(rep.Notes, MergedCleanupStep{
				Branch: branch, Worktree: wt,
				Detail: note + " — released by the owner row's delivery chain (branch kept): " + res.Detail})
			continue // the owner chain released it; the branch is its deliverable, not this sweep's to delete
		}
		// Safe to recycle: remove the worktree, then delete the branch (-d; the
		// merge ancestor guard ① makes -d succeed once the checkout is gone).
		//
		// REPORT-ONLY (epic sweep-report-not-act, 2026-09-19): the production
		// faces pass ReportOnly=true, because the sweep RETIRED its mutating
		// arms — on a healthy system every branch has a destination (it merges
		// and is deleted, or it cannot merge, a task is filed, and it eventually
		// merges and is deleted), so a reaper's only observable effect was
		// removing ground still in use. The guard ladder above is unchanged; the
		// pass now NAMES what it would have reaped, with its owner evidence.
		if opts.ReportOnly {
			rep.Reported = append(rep.Reported, mergedCleanupReportStep(mainDir, mergedCleanupLanePairPrefix, branch, wt))
			continue
		}
		// Audit record BEFORE the removal (package removalaudit). The deciding
		// evidence is the guard ladder this lane just cleared: a merged branch,
		// no live ledger claim, no live CWD, no foreign owner, no owner row.
		// reclaim-checklist-exempt: the ground checklist
		// (reclaim_ground_checklist.go) is NOT consulted — this lane is the third shape that needs no rung from it: the
		// ladder it just cleared is strictly STRONGER on both questions — a
		// live CWD in this worktree is refused at ⑤b (finishCleanupWorktreeInUse,
		// rung ①'s question, answered here per-worktree), and rung ②'s question
		// is answered by ④'s `merge-base --is-ancestor` recheck plus ①b/④b/⑤c
		// (a merged branch is by construction descended from the integration
		// branch — the opposite lineage question, and a stronger one).
		evidence := mergedCleanupNoHolderEvidence
		if orphanRelease != "" {
			// The occupancy was released as orphaned cwd-inheritors: the
			// removal record carries WHICH reading cleared the liveness guard,
			// so the release is attributable afterwards (removalaudit).
			evidence = mergedCleanupOrphanReleaseEvidence + finishCleanupReasonSeparator + orphanRelease
		}
		removalaudit.WorktreeRemove(removalaudit.ArmMergedCleanupWorktree, mainDir, wt, branch, "", evidence)
		if _, err := mergedCleanupGit(mainDir, finishCleanupWorktreeVerb, finishCleanupWorktreeRemoveVerb, wt); err != nil {
			rep.Failed = append(rep.Failed, MergedCleanupStep{
				Branch: branch, Worktree: wt, Detail: "worktree remove: " + err.Error()})
			continue
		}
		// THE TIP, for the same reason worktreeSweepDeleteBranch now carries it
		// (measured 2026-10-29: every `merged-cleanup-*` record on this host's sink
		// had NO sha, so a deleted ref left nothing that named its commit). The
		// read happens before the delete below, which is the last moment it can.
		removalaudit.BranchDeleteAt(removalaudit.ArmMergedCleanupBranch, mainDir, branch, "",
			branchDeleteTipOf(context.Background(), mainDir, branch), "",
			"merged (ancestor guard ①) and its checkout was just released by this sweep")
		// lock ② (epic 01a12f68): guard ① seeded its candidate list and ④
		// re-checked ancestry earlier in the pass — both READS. The delete site
		// asks the same question once more at the ref as it stands, because
		// `git branch -d` is not a substitute for it (its own lock is the
		// UPSTREAM, and a scratch mirror can satisfy that — measured). A
		// refusal is fail-soft like the `-d` branch below it: reported, never
		// forced, and the worktree the sweep already released stays released.
		if why := branchDeleteNonDurableTipRefusal(mainDir, branch, mergedCleanupIntegrationRef); why != "" {
			rep.Failed = append(rep.Failed, MergedCleanupStep{
				Branch: branch, Worktree: wt, Detail: why})
			continue
		}
		if _, err := mergedCleanupGit(mainDir, finishCleanupBranchVerb, finishCleanupBranchDeleteFlag, branch); err != nil {
			// The worktree is gone; a -d refusal now means the branch is
			// NOT actually an ancestor of master (guard ① raced / odd
			// shape). Fail-soft: report, never force.
			rep.Failed = append(rep.Failed, MergedCleanupStep{
				Branch: branch, Worktree: wt, Detail: "branch -d: " + err.Error()})
			continue
		}
		rep.Removed = append(rep.Removed, MergedCleanupStep{Branch: branch, Worktree: wt, OK: true})
		// The removal signal: EVERY reap announces itself on the operator
		// channel (event=worktree-reaped, branch + worktree + the reason
		// the pair was reaped). A deletion that nobody is told about is
		// how a live session's ground disappears without a trace (bug
		// 01a08e8f) — the log line is for whoever reads the log, the
		// event is for whoever is subscribed.
		mergedCleanupNotifyReaped(branch, wt)
	}
	return rep
}

// mergedCleanupOrphanBranch is the sweep's ORPHAN-REF lane: a branch that is
// already merged into the integration branch and has NO linked worktree at
// all. It exists because the pair lane above can only see branches git still
// registers a worktree for (`!hasWT → continue`), while the shape a prior
// removal LEAVES BEHIND is exactly that: ref present, registration gone.
//
// Measured (git 2.34.1, probes P1–P5): `git worktree remove --force` does NOT
// delete the branch created by `worktree add -b` — the registration is the
// only thing it drops. Every worktree removal in the system (the merge
// executor's post-merge cleanup, the adoption chain, the merge-build gate's
// defer, this sweep's own pair lane) therefore leaves a merged ref nobody
// can collect. Live evidence: two merged delivery branches on this host had
// no registration left at all, with reflogs showing they had MOVED after
// creation (genuine deliveries, not WIP).
//
// The lane is REF-ONLY: no worktree verb, no directory, no force. It reuses
// the pair lane's guards minus the two that presuppose a checkout (dirty
// worktree, worktree HEAD/off-tip, live-CWD, ownership binding — there is no
// directory to be dirty, busy or owned), and it keeps the two that carry the
// whole safety burden:
//
//	① a LIVE ledger row still references the branch (its deliverable.branch
//	   or its merge summary) → the row's own chain owns it, keep;
//	①b the pair lane's authorisation test → a ref that never moved and that
//	   no row delivered is a bare ref somebody just created for later work,
//	   NOT a merged leftover, keep;
//	② the race-free merged recheck → a branch that stopped being an ancestor
//	   of the integration branch (recommit, revert) is never deleted.
//
// Deletion itself is `branch -d` (never -D): git's merged-only check is the
// last line of defence, and a refusal (a branch checked out somewhere this
// pass cannot see, an odd ref shape) is REPORTED, never overridden (Rule 15b).
//
// REPORT-ONLY (epic sweep-report-not-act, 2026-09-19): with opts.ReportOnly the
// lane NAMES the ref it would have deleted — branch, lane, the guards that
// cleared, and the owner evidence — and deletes nothing. Every production
// caller sets it.
func mergedCleanupOrphanBranch(mainDir, branch string, rep *MergedCleanupReport,
	live map[string]bool, claims map[string]*mergedCleanupWorktreeOwnerRow, opts MergedCleanupOptions) {
	// Exemption first: the operator's protected set (archive/*, the exempt
	// pattern list) is never touched on either lane.
	if mergedCleanupBranchExempt(mainDir, branch) {
		rep.Skipped = append(rep.Skipped, MergedCleanupStep{
			Branch: branch, Detail: mergedCleanupOrphanBranchExemptMarker})
		return
	}
	// ① the live row axis. The worktree axis of the pair lane does not
	// apply — there is no directory left to be live in.
	if live[branch] {
		rep.Skipped = append(rep.Skipped, MergedCleanupStep{
			Branch: branch, Detail: mergedCleanupOrphanLiveOwnerMarker})
		return
	}
	// ①b authorisation: the branch holds work of its own (its reflog moved)
	// or a ledger row delivered it. Also reports the conservative side (no
	// reflog readable) as a NOTE, not as a wordless skip (Rule 15).
	if reason := mergedCleanupNoWorkReason(mainDir, "", branch, claims); reason != "" {
		rep.Notes = append(rep.Notes, MergedCleanupStep{
			Branch: branch, Worktree: mergedCleanupOrphanWorktreeLabel, Detail: reason})
		return
	}
	// ② race-free merged recheck — the discovery's `--merged` snapshot may be
	// stale by now, and this lane deletes with no checkout to cross-check.
	if reason := mergedCleanupStillMergedReason(mainDir, branch); reason != "" {
		rep.Skipped = append(rep.Skipped, MergedCleanupStep{
			Branch: branch, Worktree: mergedCleanupOrphanWorktreeLabel, Detail: reason})
		return
	}
	// ③ the ref-only delete. The label is composed from the verbs, so the
	// report can never claim a delete the call did not make (Rule 13).
	// REPORT-ONLY first: name the ref the retired disposal would have deleted.
	if opts.ReportOnly {
		rep.Reported = append(rep.Reported,
			mergedCleanupReportStep(mainDir, mergedCleanupLaneOrphanPrefix, branch, mergedCleanupOrphanWorktreeLabel))
		return
	}
	// THE TIP (same measurement, same reason): this is the ref-only lane, so the
	// audit record is the ONLY place the deleted commit can stay nameable.
	removalaudit.BranchDeleteAt(removalaudit.ArmMergedCleanupOrphanBranch, mainDir, branch, "",
		branchDeleteTipOf(context.Background(), mainDir, branch), "",
		"orphan lane: still an ancestor of master on the race-free recheck, no live row, no checkout left")
	if _, err := mergedCleanupGit(mainDir, finishCleanupBranchVerb, finishCleanupBranchDeleteFlag, branch); err != nil {
		rep.Failed = append(rep.Failed, MergedCleanupStep{
			Branch: branch, Worktree: mergedCleanupOrphanWorktreeLabel,
			Detail: mergedCleanupBranchDeleteLabel + ": " + err.Error()})
		return
	}
	rep.Removed = append(rep.Removed, MergedCleanupStep{
		Branch: branch, Worktree: mergedCleanupOrphanWorktreeLabel, OK: true,
		Detail: mergedCleanupOrphanBranchNote})
	mergedCleanupNotifyReaped(branch, mergedCleanupOrphanWorktreeLabel)
}

// mergedCleanupNotifyReaped fires the worktree-reaped alarm for one reaped
// pair (Rule 15 — no silent failure: the removal path is no longer silent).
func mergedCleanupNotifyReaped(branch, wt string) {
	NotifySyntheticEvent(mergedCleanupReapedEvent, "", "",
		fmt.Sprintf("worktree reaped: branch=%s worktree=%s (merged branch at the integration tip, no ledger row owed it)", branch, wt))
}

// mergedCleanupNoWorkReason is guard ①b — the discriminator between a
// MERGED LEFTOVER (safe to recycle) and a FRESH WORK-IN-PROGRESS worktree
// (bug 01a08e8f: never reap it). Guard ① cannot tell them apart: a branch
// created at the integration tip is trivially an ancestor of it, so
// `git branch --merged` lists both.
//
// The rule needs no clock and no assumption about how far the integration
// branch has moved since (measured: with "is it at the tip NOW?" a WIP
// worktree was still reaped the moment a concurrent commit advanced the
// tip — the property has to be stable):
//
//	spare the pair unless something AUTHORISES the reap:
//	· the branch's tip MOVED since creation — its reflog
//	  (`git reflog show <branch>`) carries more than the single
//	  creation entry, so the branch holds work of its own and the
//	  ordinary guard chain decides its fate; OR
//	· a ledger row CLAIMS the branch (terminal = delivered, live =
//	  still owned; the sharper ⑤/⑤b/⑤c reasons report those first).
//
// A branch that committed even once has a second reflog entry → it is
// someone's work → never reaped on this lane. A branch with only its
// creation entry and no ledger claim is a session's brand-new worktree
// (or a bare duplicate ref) → left alone. Note the deliberately ABSENT
// third condition — "nothing master lacks" (`git log master..HEAD` empty):
// it is true for EVERY merged branch (a merged branch's commits ARE in
// master, so the range only ever holds post-merge commits) and froze the
// whole recycle lane (regression-tested by
// TestMergedCleanupProgressedWorktreeStillRemoved). A branch whose reflog
// is gone (expired/imported) reports "never moved" — the conservative side.
func mergedCleanupNoWorkReason(mainDir, wt, branch string,
	branchClaims map[string]*mergedCleanupWorktreeOwnerRow) string {
	if mergedCleanupBranchTipMoved(mainDir, branch) {
		return ""
	}
	if mergedCleanupClaimed(branchClaims[branch]) {
		return ""
	}
	return mergedCleanupNoWorkMarker + ", no committed work of its own, and no ledger row delivered it)"
}

// mergedCleanupBranchTipMoved reports whether a branch's tip ever moved
// after it was created — i.e. its reflog carries more than the single
// creation entry ("branch: Created from …"). That is the stateless proof
// that someone committed (or rewrote) on the branch, so the pair is a
// delivery rather than a just-created worktree, and the ordinary guard
// chain decides its fate.
//
// Direction of the conservative side: NO reflog / a git error reports FALSE
// — read as "never moved", i.e. keep the worktree unless a ledger claim
// authorises the reap. Losing a merged leftover's cleanup (it is collected
// on a later pass, once the branch has a moving reflog) is cheap; deleting a
// session's ground is not — so the unknown falls on the safe side.
func mergedCleanupBranchTipMoved(mainDir, branch string) bool {
	ctx, cancel := context.WithTimeout(context.Background(), mergedCleanupGitTimeout)
	defer cancel()
	// PARSED object ids, one per reflog entry: stdout only (a stderr line would
	// be counted as a second entry and read as "the branch moved").
	out, err := finishCleanupGitParsedStdout(ctx, mainDir, mergedCleanupGitTimeout,
		mergedCleanupReflogVerb, mergedCleanupReflogShowVerb, mergedCleanupReflogFormatFlag, branch)
	if err != nil {
		return false
	}
	entries := 0
	for _, line := range strings.Split(out, "\n") {
		if strings.TrimSpace(line) != "" {
			entries++
		}
	}
	return entries > 1
}

// mergedCleanupClaimed reports whether a row claim (either axis) exists for
// the pair — terminal (delivery) or live (still owed).
func mergedCleanupClaimed(c *mergedCleanupWorktreeOwnerRow) bool {
	return c != nil && (c.Terminal || c.Claimed)
}

// mergedCleanupBranchClaims loads the ledger's branch-axis claims: for every
// row whose deliverable names a branch, whether the row is LIVE
// (pending/claimed/in_progress) or TERMINAL (completed/failed). Best-effort
// by design — a ledger error returns it so the caller skips the pass (never
// a silent reap on an unreadable ledger).
func mergedCleanupBranchClaims() (map[string]*mergedCleanupWorktreeOwnerRow, error) {
	ctx, cancel := context.WithTimeout(context.Background(), mergedCleanupDBTimeout)
	defer cancel()
	db, err := openTasksDB(ctx, memoryDBPath())
	if err != nil {
		return nil, err
	}
	defer db.Close()
	rows, err := db.QueryContext(ctx,
		`SELECT deliverable, status FROM tasks WHERE deliverable LIKE ?`,
		"%"+mergedCleanupBranchJSONKey+"%")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]*mergedCleanupWorktreeOwnerRow{}
	for rows.Next() {
		var deliverable, status string
		if err := rows.Scan(&deliverable, &status); err != nil {
			return nil, err
		}
		b, err := taskDeliverableBranch(deliverable)
		if err != nil || b == "" {
			continue
		}
		mergedCleanupClaimMerge(out, b, status)
	}
	return out, rows.Err()
}

// mergedCleanupClaimMerge folds one row's status into a claim bucket: a
// terminal status and a live status can both be present for the same target
// (two rows, one finished and one running) — the claim keeps BOTH flags so
// neither caller's decision is masked by the other.
func mergedCleanupClaimMerge(m map[string]*mergedCleanupWorktreeOwnerRow, key, status string) {
	c := m[key]
	if c == nil {
		c = &mergedCleanupWorktreeOwnerRow{}
		m[key] = c
	}
	switch strings.TrimSpace(status) {
	case taskStatusPending, taskStatusClaimed, taskStatusInProgress:
		c.Claimed = true
	case taskStatusCompleted, taskStatusFailed:
		c.Terminal = true
	}
}

// mergedCleanupWorktrees lists the linked worktrees: map branch name →
// worktree path, plus the main checkout dir. The MAIN checkout is excluded
// from the branch map (never a cleanup target — master lives there).
func mergedCleanupWorktrees(repo string) (map[string]string, string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), mergedCleanupGitTimeout)
	defer cancel()
	// PARSED porcelain: stdout only.
	out, err := finishCleanupGitParsedStdout(ctx, repo, mergedCleanupGitTimeout,
		finishCleanupWorktreeVerb, finishCleanupWorktreeListVerb, finishCleanupWorktreeListFlag)
	if err != nil {
		return nil, "", fmt.Errorf("git %s %s: %w", finishCleanupWorktreeVerb, finishCleanupWorktreeListVerb, err)
	}
	byBranch := map[string]string{}
	mainDir := ""
	var curPath, curBranch string
	flush := func() {
		if curPath == "" {
			return
		}
		if mainDir == "" {
			// The FIRST worktree entry is always the main checkout — it
			// sets mainDir and NEVER enters the linked map (removing the
			// main checkout would delete the whole repo).
			mainDir = curPath
			curPath, curBranch = "", ""
			return
		}
		b := strings.TrimPrefix(curBranch, "refs/heads/")
		if (curBranch == "" || strings.HasPrefix(curBranch, "refs/heads/")) &&
			b != "" && b != "master" && b != "main" {
			byBranch[b] = curPath
		}
		curPath, curBranch = "", ""
	}
	for _, line := range strings.Split(string(out), "\n") {
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
	// The LAST record is closed HERE, not by a trailing blank line: the parsed
	// reader returns stdout TRIMMED, so the final entry has no "" line after it
	// (measured 2026-10-27: without this, the last — often only — linked
	// worktree was dropped and repoSweepProtectedBranches reported protected=0).
	flush()
	return byBranch, mainDir, nil
}

// mergedCleanupTrim renders git's stderr first line for an error report.
func mergedCleanupTrim(out []byte) string {
	for _, line := range strings.Split(string(out), "\n") {
		if l := strings.TrimSpace(line); l != "" {
			return l
		}
	}
	return finishCleanupNoOutput
}

// mergedCleanupMergedBranches lists local branches that are ancestors of
// the integration branch (`git branch --merged <it>`), master/main
// excluded, capped.
func mergedCleanupMergedBranches(repo string, max int) ([]string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), mergedCleanupGitTimeout)
	defer cancel()
	// PARSED branch names, one per line: stdout only (a stderr trace line would
	// be read as a branch name and the sweep would delete that "branch").
	out, err := finishCleanupGitParsedStdout(ctx, repo, mergedCleanupGitTimeout,
		mergedCleanupBranchVerb, mergedCleanupMergedFlag, mergedCleanupIntegrationBranch)
	if err != nil {
		return nil, fmt.Errorf("git %s %s %s: %w",
			mergedCleanupBranchVerb, mergedCleanupMergedFlag, mergedCleanupIntegrationBranch, err)
	}
	var names []string
	for _, line := range strings.Split(out, "\n") {
		name := strings.TrimSpace(strings.TrimPrefix(line, "* "))
		name = strings.TrimSpace(strings.TrimPrefix(name, "+ "))
		if name == "" || name == "master" || name == "main" {
			continue
		}
		names = append(names, name)
		if len(names) >= max {
			break
		}
	}
	return names, nil
}

// mergedCleanupDirtyReason returns "" when the worktree is clean, else the
// porcelain dirt (untracked included — the finish-cleanup convention).
func mergedCleanupDirtyReason(wt string) string {
	ctx, cancel := context.WithTimeout(context.Background(), mergedCleanupGitTimeout)
	defer cancel()
	// PARSED porcelain: stdout only — stderr is a diagnostic, never dirt.
	out, err := finishCleanupGitParsedStdout(ctx, wt, mergedCleanupGitTimeout,
		finishCleanupStatusVerb, finishCleanupStatusPorcelainFlag)
	if err != nil {
		return "git status: " + err.Error()
	}
	dirt := out
	if dirt == "" {
		return ""
	}
	return "worktree dirty (live bond protected) — " + dirt
}

// mergedCleanupStillMergedReason returns "" when the branch is STILL an
// ancestor of master right now (the race-free ④ recheck), else a skip
// reason. A Bond that recommitted to an already-merged branch moves the
// branch ref off master — the coarse discovery snapshot can no longer be
// trusted; this re-check is the load-bearing guard against interrupting
// continuing work.
func mergedCleanupStillMergedReason(mainDir, branch string) string {
	ctx, cancel := context.WithTimeout(context.Background(), mergedCleanupGitTimeout)
	defer cancel()
	// merge-base --is-ancestor PARSEs nothing (git prints nothing; the answer is
	// the exit status), so the merged stream is not a parsing defect here — but
	// stdout/stderr stay apart anyway and the error keeps git's sentence.
	cmd := exec.CommandContext(ctx, gitBinaryName, gitMergeBaseVerb, gitIsAncestorArg, branch, mergedCleanupIntegrationBranch)
	cmd.Dir = mainDir
	if err := cmd.Run(); err == nil {
		return ""
	}
	return "branch no longer an ancestor of master (bond recommitted after merge — protected)"
}

// mergedCleanupOffTipReason returns "" when the worktree HEAD equals the
// branch tip in the main repo, else a skip reason. A Bond that committed
// AGAIN after its branch was merged keeps working on a NEW tip the merged
// branch ref no longer points at — never interrupt it.
func mergedCleanupOffTipReason(wt, branch, mainDir string) string {
	ctx, cancel := context.WithTimeout(context.Background(), mergedCleanupGitTimeout)
	defer cancel()
	// Two OBJECT IDs compared for equality — parsed, so stdout only on both.
	wtOut, err := finishCleanupGitParsedStdout(ctx, wt, mergedCleanupGitTimeout,
		gitRevParseVerb, mergedCleanupHeadArg)
	if err != nil {
		return "worktree HEAD: " + err.Error()
	}
	brOut, err := finishCleanupGitParsedStdout(ctx, mainDir, mergedCleanupGitTimeout,
		gitRevParseVerb, branch)
	if err != nil {
		return "branch tip: " + err.Error()
	}
	if wtOut != brOut {
		return "worktree HEAD != merged branch tip (bond recommitted after merge — protected)"
	}
	return ""
}

// mergedCleanupLiveBranches reports the set of branch names referenced by
// any LIVE ledger row (pending/claimed/in_progress): deliverable.branch
// match or the audit's "merge <branch> into master" summary. Live rows are
// few (serve capacity bounds them) — a full live scan is cheap.
//
// Deliverable.worktree closure (bugfix epic 2026-09-03): the live set ALSO
// carries every worktree path a live row's deliverable.worktree names —
// a row that recorded its worktree at creation protects that worktree
// REGARDLESS of branch attribution (the branch-based match can miss a
// freshly-created worktree whose branch is still at master tip and whose
// branch name diverged from the row's deliverable.branch). The sweep's
// guard ⑤ consults BOTH sets.
func mergedCleanupLiveBranches() (liveBranches, liveWorktrees map[string]bool, err error) {
	liveBranches = map[string]bool{}
	liveWorktrees = map[string]bool{}
	ctx, cancel := context.WithTimeout(context.Background(), mergedCleanupDBTimeout)
	defer cancel()
	db, err := openTasksDB(ctx, memoryDBPath())
	if err != nil {
		return nil, nil, err
	}
	defer db.Close()
	// The live filter comes from taskLiveStatusSQLList, i.e. from the same
	// liveTaskStatuses list isLiveTaskStatus reads (Rule 14) — never a bare
	// status literal typed here (Rule 1b/13).
	rows, err := db.QueryContext(ctx,
		`SELECT task_id, summary, deliverable FROM tasks WHERE status IN (`+taskLiveStatusSQLList()+`)`)
	if err != nil {
		return nil, nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var id, summary, deliverable string
		if err := rows.Scan(&id, &summary, &deliverable); err != nil {
			return nil, nil, err
		}
		if b, err := taskDeliverableBranch(deliverable); err == nil && b != "" {
			liveBranches[b] = true
		}
		if w := taskDeliverableWorktree(deliverable); w != "" {
			liveWorktrees[w] = true
		}
		if strings.HasPrefix(summary, "merge ") && strings.HasSuffix(summary, " into master") {
			branch := strings.TrimSuffix(strings.TrimPrefix(summary, "merge "), " into master")
			if branch != "" {
				liveBranches[branch] = true
			}
		}
	}
	return liveBranches, liveWorktrees, rows.Err()
}

// mergedCleanupBranchExempt reuses the finish-cleanup exemption set
// (master/main/archive/loser + GOPI_FINISH_CLEANUP_EXEMPT), and it takes the
// repo for the same reason the shared predicate does: the archive half of the
// exemption is a TAG (`refs/tags/archive/<branch>`), which cannot be read
// without a git database (epic archive-ref-reclaim — the criterion repair in
// task_finish_cleanup.go). The repo is the main checkout every caller of this
// sweep already resolves as the directory its git commands run in.
func mergedCleanupBranchExempt(repo, branch string) bool {
	return finishCleanupBranchExempt(repo, branch)
}

// mergedCleanupGit runs one git command in dir with the cleanup timeout.
//
// Its three callers are REMOVALS (`worktree remove`, `branch -d`) whose value is
// only PRINTED into the sweep log, so the merged stream is correct here — git's
// own refusal sentence is the deliverable. A caller that PARSES the value must
// use finishCleanupGitParsedStdout instead (Rule 14, epic
// git-parsed-stdout-only).
func mergedCleanupGit(dir string, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), mergedCleanupGitTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		return finishCleanupDiagnosticLine(out, err), fmt.Errorf("git %s: %w", strings.Join(args, " "), err)
	}
	return strings.TrimSpace(string(out)), nil
}
