// repo_sweep_report.go — the branch sweeper's REPORT arm: the ONE place the
// per-repo sweep judges a branch, and it MUTATES NOTHING.
//
// WHY THIS FILE EXISTS (operator's decision, 2026-09-19; briefing
// briefings/brief-sweep-report-not-act.md):
//
//	"先改成报告不要动作，发警告。立刻派单" — turn the sweep into a report, not an
//	action; emit a warning.
//
// The measured incident: a live session's branch fix/conflict-branch-rename-recovery
// and its worktree were deleted mid-session. Its tip was 1fb4ea4a (a
// delivery-point sync that merged master INTO the working branch) — and a tip
// that contains master IS an ancestor of master, so the old arm ①
// (`merge-base --is-ancestor` + `git branch -d`) could not tell a live working
// branch from a spent one. Nothing in that predicate ever asked WHO was using
// the branch.
//
// The operator's model, which is the spec: every branch has exactly two
// destinations (it merges and is deleted, or it cannot merge, a task is filed,
// and it eventually merges and is deleted). Under that model no branch is ever
// left ownerless in refs/heads/, so a mechanism whose job is "clean up whatever
// the merger did not handle" has no legitimate work — on a healthy system its
// only observable effect is deleting branches that are still in use.
//
// WHAT THE SWEEP DOES NOW: it reads refs, asks git the same ancestry question it
// always asked, names the OWNER EVIDENCE (the ledger rows that name the branch —
// TaskForBranch, the ONE branch→row loader, Rule 14), and PRINTS what it would
// have pruned plus why. When a LIVE owner names such a branch it emits a loud
// inconsistency on the existing notify channel (NotifySyntheticEvent, the same
// lane the stale / build-failed / worktree-reaped alarms use).
//
// WHAT IT MUST NEVER DO: `git branch -d`/`-D`, `git tag` (mint or delete), a
// branch rename, `git worktree remove`, or any other ref mutation. The gate that
// enforces this is repo_sweep_report_only_test.go, which READS this file (and its
// two siblings) and fails on a mutating git verb — so a future re-inline cannot
// land silently.
package tools

import (
	"fmt"
	"os"
	"sort"
	"strings"
)

// repoSweepWarnf is the warn face for a would-be prune of a LIVE branch: stderr,
// ALWAYS ON — it is a bug signal by definition and must be visible even when the
// notify channel is unconfigured or the event is outside the operator's
// allow-list (Rule 15a; see the call site in repoSweepReportSpentBranch).
func repoSweepWarnf(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "[sweep][warn] "+format+"\n", args...)
}

const (
	// repoSweepActionReported is the per-branch outcome for a branch whose tip
	// is already in the integration branch. It replaces the old "deleted"
	// action: the verdict is UNCHANGED (the tip really is spent), only the
	// disposition changed — REPORTED, not pruned.
	repoSweepActionReported = "reported"

	// repoSweepVerdictAlreadyInFmt is the verdict a spent branch carries — the
	// operator's "why". The surviving read-only skip reasons (ref-gone, not-ff,
	// error) sit beside it in the same vocabulary. It names the integration
	// branch the ancestry question was asked against, so a repo whose
	// integration branch is not master reads the truth rather than a guess.
	repoSweepVerdictAlreadyInFmt = "tip already in %s"

	// repoSweepWouldPruneFmt is the ONE spelling of the report line (Rule 13/14:
	// a second rendering would drift from this one). Every field the operator
	// needs to ACT is present: the branch, the tip git resolved for it, the
	// verdict that produced the report, the owner evidence, and the git command
	// that re-asks the question.
	repoSweepWouldPruneFmt = "would prune branch %s (tip %s) — verdict: %s; %s; no mutation performed (the sweep reports, it never acts)"

	// repoSweepWouldPruneNoticeEvent is the notify event a would-be prune fires
	// when — and ONLY when — a LIVE ledger row names the branch. A terminal
	// owner is the ordinary post-merge state (the row ended, the branch was
	// delivered) and is not an alarm; a live owner on a branch the sweep would
	// have deleted IS the incident above, and it must be loud.
	repoSweepWouldPruneNoticeEvent = "sweep-would-prune-live-branch"

	// repoSweepWouldPruneNoticeFmt is the alarm summary: branch, tip, verdict
	// and the live rows that name it. The operator's next action (resume the
	// row, or retire the branch deliberately) needs exactly these three.
	repoSweepWouldPruneNoticeFmt = "branch sweeper: would have pruned branch %s (tip %s, %s) while LIVE ledger row(s) %s still name it — the branch was REPORTED, not deleted"

	// repoSweepOwnerLedgerUnreadableFmt is the honest "not measured" answer: the
	// ledger could NOT be read, which is never "no row names the branch"
	// (Rule 15 — the same direction repoSweepRefGoneEvidence and
	// branchAttribution take).
	repoSweepOwnerLedgerUnreadableFmt = "the ledger could not be read (%v), so whether a row claims this branch is UNKNOWN"

	// repoSweepOwnerForeignRepoFmt names a row that claims the SAME branch name
	// in a DIFFERENT repo: two hosts legitimately carry one branch name, so a
	// foreign row is named rather than read as this repo's owner.
	repoSweepOwnerForeignRepoFmt = "row %s (%s) names this branch in a DIFFERENT repo (%s) — not this repo's owner"

	// repoSweepOwnerLiveFmt / repoSweepOwnerTerminalFmt are the two owner
	// answers the report renders (LIVE first — it is the one that matters).
	repoSweepOwnerLiveFmt     = "LIVE owner: row %s (status=%s)"
	repoSweepOwnerTerminalFmt = "terminal owner: row %s (status=%s)"

	// repoSweepOwnerNoneText is the third honest answer: the ledger was READ and
	// no row names this branch in this repo.
	repoSweepOwnerNoneText = "owner evidence: no ledger row names this branch"

	// repoSweepOwnerEvidenceLabel prefixes the owner half of the report line, so
	// a reader (and a test) can match it without knowing the wording around it.
	repoSweepOwnerEvidenceLabel = "owner evidence: "

	// repoSweepWouldPruneTitle is the human title of one report entry.
	repoSweepWouldPruneTitle = "would prune"

	// repoSweepTipUnreadableText is the tip field's honest failure value: git
	// could not resolve the ref at report time, so the line says so instead of
	// printing an empty field an operator would read as "no tip at all".
	repoSweepTipUnreadableText = "(tip unreadable: git " + repoSweepRefProbeVerb + " failed)"

	// repoSweepVerdictUnanswerableText is the verdict for a branch whose ancestry
	// question git could not answer — neither "spent" nor "not an ancestor".
	repoSweepVerdictUnanswerableText = "ancestry UNANSWERABLE — " + notFFMergeBaseVerb + " " + notFFIsAncestorFlag + " failed, so nothing is claimed about this branch"
)

// RepoSweepPruneReport is ONE branch the tick would have pruned: the branch, the
// tip git resolved for it, the verdict that produced the report, and the owner
// evidence. Read-only — building one performs no git write of any kind.
type RepoSweepPruneReport struct {
	Branch  string `json:"branch"`
	Tip     string `json:"tip"`
	Verdict string `json:"verdict"`
	// Owners is the owner-evidence sentence (LIVE vs terminal vs none vs
	// unreadable), the same one rendered in Detail.
	Owners string `json:"owners"`
	// LiveRows names the ledger rows whose owner is LIVE. Non-empty here is the
	// incident's shape and is what fires the notify alarm.
	LiveRows []string `json:"live_rows,omitempty"`
	// Detail is the operator-facing one-liner (branch, tip, verdict, owners).
	Detail string `json:"detail"`
}

// Summary renders the report entry as one operator line.
func (r RepoSweepPruneReport) Summary() string { return r.Detail }

// repoSweepWouldPrune is the ONE probe the report arm runs: it resolves the
// branch tip, asks git's ancestry question, and collects owner evidence.
//
// It returns (report, true, nil) only for a branch whose tip really is in the
// integration branch — the arm's whole verdict, unchanged from the destructive
// version, with the mutation removed. Git's own exit 1 ("NOT an ancestor") is
// the ordinary not-spent answer and is handed back as (zero, false, nil) so the
// caller proceeds to the landing; any OTHER probe failure is returned as its
// error, because a wedged repo must never be reported as either "spent" or
// "not an ancestor" (Rule 15) — the same two-answer split the destructive arm
// drew with isGitExit1.
func repoSweepWouldPrune(repo, branch string) (RepoSweepPruneReport, bool, error) {
	intBranch := repoSweepIntegrationBranchFn()
	_, ancErr := repoSweepGit(repo, notFFMergeBaseVerb, notFFIsAncestorFlag, branch, intBranch)
	if ancErr != nil {
		if isGitExit1(ancErr) {
			return RepoSweepPruneReport{}, false, nil
		}
		return RepoSweepPruneReport{}, false, ancErr
	}
	rep := RepoSweepPruneReport{Branch: branch, Verdict: fmt.Sprintf(repoSweepVerdictAlreadyInFmt, intBranch)}
	// The tip is READ from git, never guessed: a report that cannot name the
	// commit it is talking about is not actionable.
	rep.Tip = repoSweepReadTip(repo, branch)
	rep.Owners, rep.LiveRows = repoSweepOwnerEvidence(repo, branch)
	rep.Detail = fmt.Sprintf(repoSweepWouldPruneFmt, rep.Branch, rep.Tip, rep.Verdict, rep.Owners)
	return rep, true, nil
}

// repoSweepReadTip resolves refs/heads/<branch> to its commit sha for the
// report. A failure is REPORTED as its own sentence rather than leaving an
// empty field the operator would read as "no tip".
func repoSweepReadTip(repo, branch string) string {
	out, err := repoSweepGit(repo, repoSweepRefProbeVerb, refsHeadsPrefix+branch)
	if err != nil {
		return repoSweepTipUnreadableText
	}
	if tip := strings.TrimSpace(out); tip != "" {
		return tip
	}
	return repoSweepTipUnreadableText
}

// repoSweepOwnerEvidence is the branch→row lookup the report renders: the ledger
// rows that name the branch, split LIVE vs terminal. It reuses TaskForBranch —
// the ONE branch→row loader (Rule 14) the ref-guard already consults — so the
// sweep never grows a second owner lookup.
//
// It returns the evidence sentence plus the LIVE row ids (the alarm's payload).
// The ledger is consulted ONLY for a branch the sweep would prune: a tick whose
// branches all fast-forward pays nothing here.
func repoSweepOwnerEvidence(repo, branch string) (string, []string) {
	lookup, err := TaskForBranch(TaskForBranchQuery{Branch: branch}, "")
	if err != nil {
		return fmt.Sprintf(repoSweepOwnerLedgerUnreadableFmt, err), nil
	}
	var live, terminal, foreign []string
	var liveIDs []string
	for _, o := range lookup.Owners {
		// The repo filter is applied HERE, not in the query: a row whose
		// deliverable omits the repo is UNKNOWN, never a mismatch, and a
		// CONFLICTING one is named rather than silently dropped.
		if r := strings.TrimSpace(o.Repo); r != "" && r != repo {
			foreign = append(foreign, fmt.Sprintf(repoSweepOwnerForeignRepoFmt, o.TaskID, o.Status, r))
			continue
		}
		if o.Terminal {
			terminal = append(terminal, fmt.Sprintf(repoSweepOwnerTerminalFmt, o.TaskID, o.Status))
			continue
		}
		live = append(live, fmt.Sprintf(repoSweepOwnerLiveFmt, o.TaskID, o.Status))
		liveIDs = append(liveIDs, o.TaskID)
	}
	if len(live) == 0 && len(terminal) == 0 && len(foreign) == 0 {
		return repoSweepOwnerNoneText, nil
	}
	// LIVE first: it is the half an operator must act on.
	parts := append(append(live, terminal...), foreign...)
	sort.Strings(liveIDs)
	return repoSweepOwnerEvidenceLabel + strings.Join(parts, "; "), liveIDs
}

// repoSweepReportSpentBranch is the tick's WHOLE answer to a branch whose tip is
// already in the integration branch: REPORT it, warn when a live owner names it,
// and change NOTHING.
//
// The warning goes through the existing notify channel (NotifySyntheticEvent),
// so a would-be deletion is visible to whoever is subscribed rather than buried
// in a log line — the incident's other half was that the deletion was silent.
func repoSweepReportSpentBranch(repo, branch string, res *RepoSweepResult) (TaskMergeBranchOutcome, string, bool) {
	rep, spent, ancErr := repoSweepWouldPrune(repo, branch)
	if ancErr != nil {
		// git could not answer the ancestry question at all (wedged repo, unknown
		// revision). Never report on an unproven verdict and never merge blind:
		// the caller reports the git error and leaves the branch for the next
		// tick (Rule 15). This is the SAME read-only skip the destructive arm
		// had, kept deliberately.
		return TaskMergeBranchOutcome{Branch: branch, Action: repoSweepActionError,
				Detail: notFFMergeBaseVerb + " " + notFFIsAncestorFlag + ": " + ancErr.Error()},
			repoSweepActionError, true
	}
	if !spent {
		// Git answered "not an ancestor" — the branch is NOT spent, so the
		// caller's landing arm gets it (the ordinary path for real work).
		return TaskMergeBranchOutcome{}, "", false
	}
	res.WouldPrune = append(res.WouldPrune, rep)
	if rep.LiveRows != nil {
		res.WouldPruneLive++
		summary := fmt.Sprintf(repoSweepWouldPruneNoticeFmt, rep.Branch, rep.Tip, rep.Verdict,
			strings.Join(rep.LiveRows, ", "))
		// ALWAYS-ON stderr warn FIRST, then the notify event. The notify half
		// alone is not enough (Rule 15a): NotifySyntheticEvent returns SILENTLY
		// when the event is outside the operator's allow-list, and the shipped
		// default list is "claimed,in_progress,completed,failed,superseded" —
		// so on a
		// default install the alarm this epic exists to raise would have been
		// invisible. Measured 2026-09-19: the host's settings.json lists
		// "…,stale,build-failed" and would still have dropped it. A live owner
		// about to be pruned is a bug signal by definition; it must be visible
		// even with notify unconfigured.
		repoSweepWarnf("%s", summary)
		NotifySyntheticEvent(repoSweepWouldPruneNoticeEvent, "", "", summary)
	}
	return TaskMergeBranchOutcome{Branch: branch, Action: repoSweepActionReported, Detail: rep.Detail},
		repoSweepActionReported, true
}

// RepoSweepPruneLines renders the report entries as operator log lines — the ONE
// renderer (Rule 13/14) shared by the merger tick and the sweep result.
func RepoSweepPruneLines(reports []RepoSweepPruneReport) []string {
	if len(reports) == 0 {
		return nil
	}
	out := make([]string, 0, len(reports))
	for _, r := range reports {
		out = append(out, repoSweepWouldPruneTitle+" branch "+r.Branch+": "+r.Detail)
	}
	return out
}

// The merged-cleanup reaper's REPORT-ONLY half (epic sweep-report-not-act,
// 2026-09-19). The reaper's guard ladder is unchanged and its mutating arms are
// retired; a caller that asks for the report-only disposition gets the pair
// NAMED — lane, verdict, owner evidence — and nothing touched.
const (
	// mergedCleanupLanePairPrefix / …OrphanPrefix name the lane a reported pair
	// came from, so an operator reading the report knows which guard ladder
	// cleared (the two lanes clear different ladders).
	mergedCleanupLanePairPrefix   = "pair lane (merged branch + linked worktree): "
	mergedCleanupLaneOrphanPrefix = "orphan lane (merged branch, no linked worktree): "

	// mergedCleanupReportOnlyClearNote is the verdict half of a reported pair:
	// the guard ladder cleared, so the OLD code would have removed the ground.
	mergedCleanupReportOnlyClearNote = "every guard this lane asks cleared (merged ancestor recheck, no live ledger owner, no live holder)"

	// mergedCleanupReportOnlyFmt is the ONE spelling of the reported pair's
	// detail: what would have happened, on which branch/worktree, and why not.
	mergedCleanupReportOnlyFmt = "WOULD HAVE REAPED (%s%s) — %s; %s; no mutation performed (the reaper reports, it never acts)"
)

// mergedCleanupReportStep builds the report entry for a pair the retired
// disposal would have reaped: the branch, the worktree (`wt` is the orphan
// lane's own label when there is no directory), and the owner evidence — the
// SAME loader the sweep's report arm uses (TaskForBranch via
// repoSweepOwnerEvidence, Rule 14: never a second owner lookup).
func mergedCleanupReportStep(repo, lanePrefix, branch, wt string) MergedCleanupStep {
	owners, _ := repoSweepOwnerEvidence(repo, branch)
	return MergedCleanupStep{
		Branch:   branch,
		Worktree: wt,
		OK:       false,
		Detail:   fmt.Sprintf(mergedCleanupReportOnlyFmt, lanePrefix, branch, mergedCleanupReportOnlyClearNote, owners),
	}
}
