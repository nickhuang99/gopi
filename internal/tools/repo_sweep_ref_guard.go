// repo_sweep_ref_guard.go — the merge candidate's REF RE-VERIFICATION step.
//
// THE INCIDENT IT CLOSES (measured 2026-09-16/17,
// ~/.gopi/logs/merger.log:27630): one tick listed
// `merge/merger-inflight-both-axes` among its merge candidates — the name came
// from the git-ref discovery (repoSweepClassify → `git for-each-ref
// refs/heads/`, repo_sweep.go:538) — and by the time the per-branch decision
// ran, the ref was GONE:
//
//	[task-notify] merger: UNION gate ABSTAINS for merge/merger-inflight-both-axes
//	  (merge-base master merge/merger-inflight-both-axes failed: git merge-base
//	   master merge/merger-inflight-both-axes: exit status 128:
//	   fatal: Not a valid object name merge/merger-inflight-both-axes)
//
// The very next line the same tick printed was the older park lane failing to
// tag the SAME missing name (`git tag archive/merge/merger-inflight-both-axes
// merge/merger-inflight-both-axes: fatal: Failed to resolve … as a valid ref`).
// The branch had already been landed and deleted by a concurrent round —
// refs/heads/master@{69} records "merge merge/merger-inflight-both-axes: Merge
// made by the 'ort' strategy" (commit fd6727ad, an ancestor of master) — and
// its name was still on file in a COMPLETED ledger row (deliverable.branch of
// row 2305).
//
// WHY A GUARD AND NOT A WIDER CANDIDATE SOURCE. The candidate set is git refs
// and it must STAY git refs: a branch the ledger names but git does not have
// cannot be fast-forwarded, cannot be compiled and cannot be merged, so a
// ledger-derived candidate set would feed names to three compile gates that can
// only ever answer "not a valid object name" (the build gate's own declared-not-
// discovered discipline, Rule 12, is the same shape). The defect is not the
// SOURCE — it is that the DECISION step trusted a discovery-time snapshot. A
// snapshot of a ref namespace that other rounds mutate concurrently (this same
// log holds several merger pids appending interleaved passes) needs ONE cheap
// re-read at the point of decision, which is exactly what this file adds.
//
// WHAT IT DOES. Before any gate or `git merge` is asked about a branch, a single
// `git rev-parse --verify --quiet refs/heads/<branch>^{commit}` decides:
//
//	ref resolves    → the decision continues EXACTLY as before. This is the
//	                  no-weakening half: a real branch reaches every gate and
//	                  every git step unchanged.
//	ref is GONE     → the branch is SKIPPED with the `ref-gone` action, and the
//	                  disappearance is RECORDED (Rule 15 — never swallowed): the
//	                  ledger is consulted for the rows naming this branch
//	                  (TaskForBranch — the ONE branch→row loader, Rule 14), a
//	                  LIVE-status owner makes it an INCONSISTENCY (a row still
//	                  claiming a branch git no longer has) which is written to
//	                  that row's own audit trail (task_events), and the merger
//	                  log NAMES the state either way. A terminal owner is the
//	                  ordinary post-merge state and is named as such, so the
//	                  operator reads a reason rather than a silence.
//	probe fails     → the branch becomes an `error` outcome. A wedged repo must
//	                  never read as "the branch is gone" and must never read as
//	                  "merge it blind" (Rule 15).
//
// The probe asks refs/heads ALONE, because that is the candidate set: a name a
// tag or a remote-tracking ref carries is not a local merge candidate, and
// answering "present" for it would hand a non-head to `git merge --ff-only`.
package tools

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
	"time"
)

const (
	// repoSweepActionRefGone is the decision step's outcome for a candidate
	// whose ref had been deleted between discovery and the decision. It is NOT
	// an error: git answered every question that was asked, and the honest
	// answer to "merge this branch" is "there is no such branch any more" —
	// the same vocabulary discipline repoSweepActionNotFF follows.
	repoSweepActionRefGone = "ref-gone"

	// repoSweepRefProbeVerb / …VerifyFlag / …QuietFlag / …CommitSuffix spell the
	// probe (Rule 1b: the dispatched-on values are constants). `--quiet` plus
	// `^{commit}` is what makes the answer MACHINE-readable: a missing ref exits
	// 1 AND prints nothing, while a repo-level failure (an unreadable HEAD,
	// "fatal: Needed a single revision") exits non-zero WITH text — the two must
	// not collapse into the same verdict, and this pair is what separates them.
	repoSweepRefProbeVerb         = "rev-parse"
	repoSweepRefProbeVerifyFlag   = "--verify"
	repoSweepRefProbeQuietFlag    = "--quiet"
	repoSweepRefProbeCommitSuffix = "^{commit}"

	// repoSweepRefGoneEventName is the task_events event written on a LIVE row
	// whose branch has vanished — the ledger's own audit channel for the
	// inconsistency (the same channel the reframe/closure records use).
	repoSweepRefGoneEventName = "ref-gone"

	// repoSweepRefGoneInconsistentFmt names the INCONSISTENCY half: a row that
	// has not ended still claims the branch. It is a separate sentence from the
	// ordinary case on purpose — the operator's next action differs (a live row
	// whose branch is gone is a lost deliverable; a completed row whose branch
	// is gone is a finished task).
	repoSweepRefGoneInconsistentFmt = "INCONSISTENCY: live ledger row %s (status=%s) still names branch %s, which git no longer has"

	// repoSweepRefGoneTerminalFmt names the ORDINARY half: the row ended, and
	// the branch it delivered was already landed and deleted (the terminal
	// state of every merged deliverable).
	repoSweepRefGoneTerminalFmt = "ledger row %s (status=%s) named this branch and has ended — the ordinary post-merge state"

	// repoSweepRefGoneNoRowText is the third honest answer: the ledger was READ
	// and no row names the branch in this repo at all.
	repoSweepRefGoneNoRowText = "no ledger row names this branch"

	// repoSweepRefGoneForeignRepoFmt is the fourth: a row names the branch but in
	// a DIFFERENT repo — the same branch name legitimately exists in several
	// repos (two hosts, two deliverables), so a foreign row is named rather than
	// read as this repo's owner.
	repoSweepRefGoneForeignRepoFmt = "row %s (status=%s) names this branch in a DIFFERENT repo (%s) — not this repo's owner"

	// repoSweepRefGoneLedgerUnreadableFmt is the fourth: the ledger could NOT be
	// read, which is never "no row names it" (Rule 15, the same direction
	// BranchAttributionReport refuses to conclude from an unreadable ledger).
	repoSweepRefGoneLedgerUnreadableFmt = "the ledger could not be read (%v), so whether a row claims this branch is UNKNOWN"

	// repoSweepRefGoneProbeErrFmt is the ABSTAIN reason when the probe itself
	// could not answer (a wedged repo, an unreadable HEAD): reported as an error
	// outcome, never as a disappearance.
	repoSweepRefGoneProbeErrFmt = "ref probe inconclusive (git %s %s failed with output: %s) — the branch is neither merged nor skipped"
)

// repoSweepRefPresentFn is the PROBE SEAM: the decision step calls the variable,
// never the function directly, so a test can drive the mid-tick disappearance
// deterministically (a concurrent merger round deleting a branch between
// discovery and decision is a race no fixture can schedule — the repo's
// established pattern for proving what a sweep actually ran). Production code
// never reassigns it.
var repoSweepRefPresentFn = repoSweepRefPresent

// repoSweepRefPresent reports whether refs/heads/<branch> resolves to a commit
// in repo.
//
// It returns (false, nil) ONLY for git's own "no such ref" answer: exit status
// 1 with EMPTY output. Any other failure is an error — an inconclusive probe is
// never a disappearance (a missing ref must not be invented out of a broken
// repo) and never a licence to merge (Rule 15).
func repoSweepRefPresent(repo, branch string) (bool, error) {
	branch = strings.TrimSpace(branch)
	if branch == "" {
		return false, fmt.Errorf("ref probe: empty branch name")
	}
	ctx, cancel := context.WithTimeout(context.Background(), repoSweepGitTimeout)
	defer cancel()
	out, err := exec.CommandContext(ctx, "git", "-C", repo,
		repoSweepRefProbeVerb, repoSweepRefProbeVerifyFlag, repoSweepRefProbeQuietFlag,
		refsHeadsPrefix+branch+repoSweepRefProbeCommitSuffix).CombinedOutput()
	text := strings.TrimSpace(string(out))
	if err == nil {
		return true, nil
	}
	if _, ok := err.(*exec.ExitError); ok && text == "" {
		// git's named absence: `--verify --quiet` exits 1 and says nothing.
		return false, nil
	}
	return false, fmt.Errorf(repoSweepRefGoneProbeErrFmt, repoSweepRefProbeVerb, refsHeadsPrefix+branch, text)
}

// repoSweepRefGoneEvidence renders WHAT the ledger says about a branch whose
// ref has vanished, and (for a live owner) RECORDS the inconsistency on that
// row's own audit trail. It returns the evidence sentence for the merger log
// plus the names of the live rows the record landed on.
//
// The ledger is consulted ONLY here — on the disappearance path — so a tick
// whose refs are all real pays nothing for this file (no extra query, no extra
// git call beyond the one probe).
//
// The query is NOT narrowed by repo (TaskForBranch's own Repo filter would drop
// a row whose blob omits the repo field, and "the row did not record a repo" is
// unknown, never a mismatch): the narrowing happens here instead, where an
// unrecorded repo is kept and a CONFLICTING one is reported as its own sentence
// rather than silently vanishing.
func repoSweepRefGoneEvidence(repo, branch string) (evidence string, recorded []string) {
	lookup, err := TaskForBranch(TaskForBranchQuery{Branch: branch}, "")
	if err != nil {
		// The unreadable ledger is REPORTED as the unknown it is — never
		// rendered as "no row names it" (Rule 15).
		return fmt.Sprintf(repoSweepRefGoneLedgerUnreadableFmt, err), nil
	}
	owners := make([]TaskForBranchOwner, 0, len(lookup.Owners))
	var foreign []string
	for _, o := range lookup.Owners {
		if r := strings.TrimSpace(o.Repo); r != "" && r != repo {
			foreign = append(foreign, fmt.Sprintf(repoSweepRefGoneForeignRepoFmt, o.TaskID, o.Status, r))
			continue
		}
		owners = append(owners, o)
	}
	if len(owners) == 0 && len(foreign) == 0 {
		return repoSweepRefGoneNoRowText, nil
	}
	var live, terminal []string
	for _, o := range owners {
		if o.Terminal {
			terminal = append(terminal, fmt.Sprintf(repoSweepRefGoneTerminalFmt, o.TaskID, o.Status))
			continue
		}
		live = append(live, fmt.Sprintf(repoSweepRefGoneInconsistentFmt, o.TaskID, o.Status, branch))
	}
	// Record FIRST, so the log line below can state what was written rather
	// than what was attempted (a verdict without its evidence is a self-report).
	for _, o := range owners {
		if o.Terminal {
			continue
		}
		if err := repoSweepRecordRefGone(branch, repo, o); err != nil {
			// Rule 15b: the record failure is REPORTED, never discarded — and
			// the branch is still skipped, so the merger never merges a name it
			// cannot re-resolve because the audit write failed.
			live = append(live, fmt.Sprintf("row %s: recording FAILED: %v", o.TaskID, err))
			continue
		}
		recorded = append(recorded, o.TaskID)
	}
	parts := append(append(live, terminal...), foreign...)
	if len(parts) == 0 {
		return repoSweepRefGoneNoRowText, nil
	}
	return strings.Join(parts, "; "), recorded
}

// repoSweepRecordRefGone appends the inconsistency to the row's own audit trail
// (the `ref-gone` event), deduped so a branch that stays gone across many ticks
// leaves ONE record on the row rather than one per tick (the same one-branch-one-
// record discipline the branch-rework summaries use).
func repoSweepRecordRefGone(branch, repo string, owner TaskForBranchOwner) error {
	detail := fmt.Sprintf("merger: branch %s is GONE from %s (repo %s) while this row (status=%s) still claims it — the merge candidate path skipped it instead of asking the compile gates about a ref git cannot resolve",
		branch, refsHeadsPrefix+branch, repo, owner.Status)
	if taskEventHasDetail(owner.TaskID, repoSweepRefGoneEventName+": "+branch) {
		return nil // already recorded for this row: the record is idempotent
	}
	ctx, cancel := context.WithTimeout(context.Background(), repoSweepGitTimeout)
	defer cancel()
	db, err := openTasksDB(ctx, memoryDBPath())
	if err != nil {
		return fmt.Errorf("open ledger: %w", err)
	}
	defer db.Close()
	// The INSERT is issued here rather than through taskEventWrite, which logs
	// its own failure and returns nothing — this arm must be able to REPORT that
	// the record did not land (Rule 15b), because "the inconsistency was written"
	// is exactly the claim a swallowed error would falsify.
	_, err = db.ExecContext(ctx,
		`INSERT INTO task_events (task_id, event, detail, created_at) VALUES (?, ?, ?, ?)`,
		owner.TaskID, repoSweepRefGoneEventName, repoSweepRefGoneEventName+": "+branch+" — "+detail,
		float64(time.Now().Unix()))
	if err != nil {
		return fmt.Errorf("append task_events: %w", err)
	}
	return nil
}

// repoSweepRefGoneOutcome builds the SKIP outcome for a candidate whose ref is
// gone: the action, the evidence the operator reads, the live rows the record
// landed on, and the recorded inconsistency's own line.
//
// The branch is left COMPLETELY untouched — no archive tag, no `branch -d`
// (there is no ref to delete), no ledger status move — which is what makes this
// arm safe to run every tick: the inconsistency keeps being reported until a
// human (or the reclaim arm) resolves it, and re-reporting costs one git probe.
func repoSweepRefGoneOutcome(repo, branch string) (TaskMergeBranchOutcome, string, []string) {
	evidence, recorded := repoSweepRefGoneEvidence(repo, branch)
	detail := fmt.Sprintf("the branch ref no longer exists (git %s %s %s%s says so) — skipped BEFORE the merge-time gates, so no gate was asked about a name git cannot resolve; %s",
		repoSweepRefProbeVerb, repoSweepRefProbeVerifyFlag, refsHeadsPrefix+branch, repoSweepRefProbeCommitSuffix, evidence)
	return TaskMergeBranchOutcome{Branch: branch, Action: repoSweepActionRefGone, Detail: detail},
		repoSweepActionRefGone, recorded
}
