package tools

// Merge-backlog EXECUTOR (epic tasks-merge-executor-live): the LLM merge
// executor that actually lands pending merge-backlog rows. The audit
// (task_merge_backlog.go) enqueues "merge <branch> into master" rows; the
// executor is the closing half — claim a pending row, guard it, merge it,
// close the row and prune the branch. It is INFRASTRUCTURE (a git
// operation driven by the ledger), NOT an LLM tool (Rule 3) — the same
// decision boundary as AuditMergeBacklog, AdoptionSweep and the client
// finish-cleanup chain: the Go code only GUARDS / MERGES / CLOSES, every
// semantic judgment stays human or agent-side.
//
// The chain (fail-soft per step, order = the safety argument):
//
//	① row gate   — the row must be pending (or claimed); its summary must
//	               parse as "merge <branch> into master".
//	② dirty gate — the main checkout (master's tree) must be CLEAN
//	               (`git status --porcelain` empty, untracked included —
//	               the hook-v5 lesson: one untracked file = dirty = a live
//	               bond may be writing; never touch it). Dirty → the row
//	               is LEFT pending (nothing ran — next pass retries), zero
//	               steps run.
//	③ safe-merge — the safe-merge.sh guard in Go: archive tag on the
//	               branch / loser or archive name prefix / worktree
//	               occupancy. Any hit → the row is claimed, started and
//	               marked FAILED with the reason (a blocked merge is a
//	               REPORT a human must see, never a silent skip).
//	④ merge      — git merge --no-commit --no-ff <branch> into the
//	               checked-out master (the adoption-chain probe shape: a
//	               conflict exits non-zero with a restorable tree).
//	               Clean → commit with the explicit merge message.
//	               Conflict → merge --abort (tree restored), row marked
//	               failed with the conflict report — master untouched, the
//	               branch survives for a human decision.
//	⑤ close-out  — completed writeback + branch -d. The branch op runs
//	               against the MAIN repo dir resolved BEFORE the merge (the
//	               repo==worktree deliverable shape: the row's repo field
//	               may point at a linked worktree — resolve the main
//	               checkout via --git-common-dir, the 83f73f1 fix).
//
// State transitions (legal per taskAllowedTransitions): the guards that
// CONSUME a row (safe-merge block, conflict) claim (pending → claimed) and
// start (claimed → in_progress) BEFORE the failed writeback (in_progress →
// failed is legal); the dirty guard consumes NOTHING (the row stays
// pending). A successful merge claims, starts, then writes back
// in_progress → completed.
//
// Only ONE row is executed per call (deterministic, testable); the serve
// pass and the CLI drive it row-by-row.

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/nick/gopi/internal/gitcommit"
	"github.com/nick/gopi/internal/removalaudit"
)

// Merge-executor tunables (Rule 1: every literal in one place).
const (
	// mergeExecGitTimeout caps each git invocation (a wedged repo must not
	// hang the executor or the serve pass).
	mergeExecGitTimeout = 60 * time.Second

	// mergeExecDBTimeout caps the ledger reads/writes.
	mergeExecDBTimeout = 10 * time.Second

	// mergeExecSummaryVerb is the summary prefix the executor recognizes
	// (mirrors mergeBacklogSummaryVerb — the audit's enqueue shape).
	mergeExecSummaryVerb = "merge %s into master"

	// mergeExecMergeMsg is the explicit merge commit message (the audit
	// trail greppability contract — "merge-backlog: merge <branch>").
	mergeExecMergeMsg = "merge-backlog: merge %s"

	// mergeExecBlacklistPrefixes are the branch-name prefixes safe-merge.sh
	// refuses (loser/* and archive/* — the loser-archive ledger). The
	// archive/ member is DERIVED from archiveBranchPrefix (single source in
	// tasks.go) — the safe-merge guard and the finish-cleanup exemption
	// cannot drift apart.
	mergeExecBlacklistPrefixes = "loser/," + archiveBranchPrefix

	// mergeExecClaimHolder is the claimed_by stamp on executor-claimed rows.
	mergeExecClaimHolder = "merge-executor"

	// mergeExecCompletedSummary is the completed writeback's new summary.
	mergeExecCompletedSummary = "merged %s into master via merge-executor"
)

// MergeExecResult is one executor run's outcome — the audit trail the CLI
// and the serve pass print, and the reason a row was left/skipped/failed.
type MergeExecResult struct {
	TaskID string `json:"task_id"`
	// Branch is the parsed merge target ("" = unparseable row).
	Branch string `json:"branch,omitempty"`
	// Repo is the repo the merge runs in (the MAIN checkout resolved from
	// the row's deliverable.repo, or the cwd when the row has no repo).
	Repo string `json:"repo,omitempty"`
	// Status is the row's outcome: completed | failed | skipped.
	// skipped = nothing ran and the row stays pending (dirty-tree guard /
	// non-merge row / already-terminal) — the executor is idempotent-safe.
	Status string `json:"status"`
	// Reason names the guard that stopped the run (failed/skipped).
	Reason string `json:"reason,omitempty"`
	// Steps records what the run did (merge/commit/branch/abort).
	Steps []MergeExecStep `json:"steps,omitempty"`
}

// MergeExecStep is one executed action (the report is the audit trail).
type MergeExecStep struct {
	Step   string `json:"step"` // merge | commit | branch | abort
	OK     bool   `json:"ok"`
	Detail string `json:"detail,omitempty"`
}

// MergeExec outcome constants (Rule 1: the executor's terminal vocabulary).
// EXPORTED on purpose (Rule 14): the vocabulary is compared across packages —
// cmd/agent gates its post-landing close loop on MergeExecLanded, which is
// defined in terms of these values, and a caller re-typing "completed" would be
// a second source of truth for a value that decides whether master moved.
const (
	MergeExecStatusCompleted = "completed"
	MergeExecStatusFailed    = "failed"
	MergeExecStatusSkipped   = "skipped"
)

// mergeExecIntegrationRef is the integration branch the executor's own
// ancestry probe and its delete lock both resolve against (Rule 2: one spelling
// of "where the work was merged to" for this file). The name is the package
// constant, not a literal (Rule 14).
const mergeExecIntegrationRef = IntegrationBranchName

// MergeExecLanded reports whether a run ADVANCED the integration branch — the
// single condition under which a caller owes the post-merge close loop
// (rebuild + version name). Exported as a predicate rather than as the status
// vocabulary because the caller's question is semantic ("did master move?"),
// not textual: a conflict or a guard skip leaves the repo exactly where it was,
// and the row stays pending/failed for a later decision.
//
// One completed shape did NOT move master: the already-merged row ("branch
// already merged into master" → prune the ref only). It is deliberately NOT
// special-cased — the close loop is idempotent (RebuildAfterMerge reports
// "already current" for a binary that follows the tip), so the false positive
// costs one `--version` probe, never a build.
func MergeExecLanded(rep *MergeExecResult) bool {
	return rep != nil && rep.Status == MergeExecStatusCompleted
}

// TaskMergeBacklogExecute runs the executor chain for ONE pending
// merge-backlog row (looked up by task_id). Fail-soft: guard mismatches
// return a skipped report (the row stays pending — re-audit/next pass
// retries); real merge blockers mark the row failed with the reason. A
// row in any terminal state is a skipped no-op (idempotent re-runs).
func TaskMergeBacklogExecute(taskID string) (*MergeExecResult, error) {
	rep := &MergeExecResult{TaskID: taskIDNormalize(taskID)}
	if rep.TaskID == "" {
		return nil, fmt.Errorf("merge-execute: task_id must not be empty")
	}

	ctx, cancel := context.WithTimeout(context.Background(), mergeExecDBTimeout)
	defer cancel()
	db, err := openTasksDB(ctx, memoryDBPath())
	if err != nil {
		return nil, fmt.Errorf("merge-execute: open ledger: %w", err)
	}
	defer db.Close()

	t, err := taskByID(ctx, db, rep.TaskID)
	if err != nil {
		return nil, fmt.Errorf("merge-execute: %w", err)
	}
	if t == nil {
		rep.Status = MergeExecStatusSkipped
		rep.Reason = "no such task row"
		return rep, nil
	}
	if t.Status != taskStatusPending && t.Status != taskStatusClaimed {
		rep.Status = MergeExecStatusSkipped
		rep.Reason = "status=" + t.Status + " not pending (already handled)"
		return rep, nil
	}
	// A CLAIMED row is only ours to recover when OUR holder stamped it (a
	// previous merge-executor died mid-flight). A row claimed by someone
	// else — e.g. the serve dispatch running it as an LLM task, or a
	// concurrent executor — must be LEFT alone: double-claiming would
	// double-merge (the LLM agent and this executor both landing the same
	// branch). Claimed-by-other = skip, never steal.
	if t.Status == taskStatusClaimed && !strings.EqualFold(strings.TrimSpace(t.ClaimedBy), mergeExecClaimHolder) {
		rep.Status = MergeExecStatusSkipped
		rep.Reason = "claimed by " + t.ClaimedBy + " (someone else holds it — not stealing)"
		return rep, nil
	}

	// ① Parse the branch out of the summary. The audit's shape is
	// "merge <branch> into master"; anything else is not ours to execute
	// (a hand-submitted row with a merge-ish summary is left pending).
	branch := mergeExecParseBranch(t.Summary)
	if branch == "" {
		rep.Status = MergeExecStatusSkipped
		rep.Reason = "summary does not parse as " +
			fmt.Sprintf(mergeExecSummaryVerb, "<branch>") + ": " + t.Summary
		return rep, nil
	}
	rep.Branch = branch

	// Repo resolution: the row's deliverable.repo wins (the audit stamps
	// it); fallback = cwd (the audit's own default). The repo field may
	// point at a linked WORKTREE (the live t-1788157112 shape) — the merge
	// must run where master is checked out; resolve the MAIN checkout via
	// --git-common-dir (the 83f73f1 fix), BEFORE any step that could
	// remove the worktree.
	repo := mergeExecRowRepo(t.Deliverable)
	if repo == "" {
		repo, _ = filepath.Abs(".") //stylegate:ignore fallback repo path; an empty result fails loudly on the next git call
	}
	mainRepo, err := mergeExecMainRepo(repo)
	if err != nil {
		rep.Status = MergeExecStatusFailed
		rep.Reason = "resolve main repo: " + err.Error()
		return rep, nil
	}
	rep.Repo = mainRepo

	// ② Dirty-tree guard: the merge runs in the main checkout — a dirty
	// tree is a LIVE BOND still writing there. Skip the whole chain; the
	// row stays pending (nothing ran, nothing to report as done).
	dirty, err := mergeExecGit(mainRepo, "status", "--porcelain")
	if err != nil {
		rep.Status = MergeExecStatusFailed
		rep.Reason = "git status: " + dirty
		return rep, nil
	}
	// finishCleanupProbeClean reads the probe: EMPTY porcelain comes back as
	// the empty-output sentinel (clean tree), anything else IS real dirt
	// (untracked included). Master's inline `strings.Contains(dirty,
	// "no output")` test is exactly the substring defeat this side fixed — a
	// real untracked file named "no output" would read as a clean tree — so
	// the exact-comparison reader wins the hunk; master's reason text and its
	// MergeExecStatusSkipped constant are kept (that is the name the merged
	// tree defines; the resumed side's mergeExecOutcomeSkipped had no
	// definition left after the union).
	if dirtyClean, dirt := finishCleanupProbeClean(dirty); !dirtyClean {
		rep.Status = MergeExecStatusSkipped
		rep.Reason = "worktree dirty (live bond protected) — " + dirt
		return rep, nil
	}

	// ②b Integration-branch guard: the contract is "merge INTO master" —
	// the main checkout must be ON master (or main). Merging into whatever
	// branch happens to be checked out would silently move the wrong ref.
	head, err := mergeExecGit(mainRepo, "rev-parse", "--abbrev-ref", "HEAD")
	if err != nil {
		rep.Status = MergeExecStatusFailed
		rep.Reason = "resolve HEAD: " + head
		return rep, nil
	}
	if head != "master" && head != "main" {
		rep.Status = MergeExecStatusFailed
		rep.Reason = "main checkout is on branch " + head + ", not master (merge target mismatch — refusing)"
		if err := mergeExecClaimStart(rep.TaskID); err != nil {
			return nil, fmt.Errorf("merge-execute: %w", err)
		}
		if _, err := TaskUpdateWithResult(rep.TaskID, taskStatusFailed,
			"merge-target-mismatch: main checkout on "+head+", not master"); err != nil {
			return nil, fmt.Errorf("merge-execute: writeback failed: %w", err)
		}
		return rep, nil
	}

	// ③ safe-merge guard (the Go mirror of scripts/safe-merge.sh): a
	// blocked merge is a REPORT — the row is consumed and failed so a
	// human sees the reason in the ledger, never a silent skip. Claim →
	// start first (pending → claimed → in_progress), then the failed
	// writeback (in_progress → failed is a legal transition).
	if guardErr := mergeExecSafeMergeBlock(mainRepo, branch); guardErr != nil {
		if err := mergeExecClaimStart(rep.TaskID); err != nil {
			return nil, fmt.Errorf("merge-execute: %w", err)
		}
		// The refusal names the branch it refuses (missing ref /
		// unrelated history) — the ledger carries THAT reason, never the
		// "merge conflict" the merge leg would produce after git rejected
		// the same branch for the same reason.
		reason := guardErr.Error()
		rep.Status = MergeExecStatusFailed
		rep.Reason = "safe-merge guard: " + reason
		if _, err := TaskUpdateWithResult(rep.TaskID, taskStatusFailed,
			"merge-blocked: "+reason); err != nil {
			return nil, fmt.Errorf("merge-execute: writeback failed: %w", err)
		}
		return rep, nil
	}

	// Claim + start the row so concurrent executors / a serve dispatch
	// cannot double-run it (pending → claimed → in_progress).
	if err := mergeExecClaimStart(rep.TaskID); err != nil {
		return nil, fmt.Errorf("merge-execute: %w", err)
	}

	// ④ Merge (probe shape: stage without committing — the adoption-chain
	// contract). First, the already-merged case: if the branch's tip is
	// already an ancestor of master, the row's INTENT is satisfied —
	// complete it (prune the branch) without re-merging (git merge would
	// exit 0 with "Already up to date." but leave nothing to commit).
	if mergeExecIsAncestor(mainRepo, branch) {
		removalaudit.BranchDelete(removalaudit.ArmMergeExecBranch, mainRepo, branch, rep.TaskID,
			"branch tip is an ancestor of master")
		// lock ② (epic 01a12f68): the arm's own read was taken against HEAD a
		// moment ago; re-ask it at the ref as it stands, because `git branch -d`
		// is not a lock against a scratch-only holder (measured — see
		// branchDeleteNonDurableTipRefusal). A refusal KEEPS the ref and the row
		// still completes: the intent (the work is in the integration branch) is
		// satisfied either way.
		if why := branchDeleteNonDurableTipRefusal(mainRepo, branch, mergeExecIntegrationRef); why != "" {
			rep.Steps = append(rep.Steps, MergeExecStep{Step: "branch", Detail: why})
		} else if _, derr := mergeExecGit(mainRepo, "branch", "-d", branch); derr == nil {
			rep.Steps = append(rep.Steps, MergeExecStep{Step: "branch", OK: true})
		} else {
			rep.Steps = append(rep.Steps, MergeExecStep{Step: "branch", Detail: derr.Error()})
		}
		rep.Status = MergeExecStatusCompleted
		rep.Reason = "branch already merged into master"
		if _, werr := TaskUpdateWithResult(rep.TaskID, taskStatusCompleted,
			fmt.Sprintf(mergeExecCompletedSummary, branch)+" (already merged)"); werr != nil {
			return nil, fmt.Errorf("merge-execute: writeback failed: %w", werr)
		}
		return rep, nil
	}
	if _, err := mergeExecGit(mainRepo, "merge", "--no-commit", "--no-ff", branch); err != nil {
		abortDetail := ""
		if _, aerr := mergeExecGit(mainRepo, "merge", "--abort"); aerr != nil {
			abortDetail = "; abort: " + aerr.Error()
		}
		rep.Steps = append(rep.Steps, MergeExecStep{Step: "abort", Detail: err.Error() + abortDetail})
		rep.Status = MergeExecStatusFailed
		rep.Reason = "merge conflict: " + err.Error()
		// The conflict report is the failed row's summary — master is
		// untouched (abort), the branch survives for a human decision.
		if _, werr := TaskUpdateWithResult(rep.TaskID, taskStatusFailed,
			taskTagMergeConflict+": "+err.Error()); werr != nil {
			return nil, fmt.Errorf("merge-execute: writeback failed: %w", werr)
		}
		return rep, nil
	}
	rep.Steps = append(rep.Steps, MergeExecStep{Step: "merge", OK: true})

	// ⑤ Close-out: commit the staged merge, completed writeback, branch -d.
	msg := fmt.Sprintf(mergeExecMergeMsg, branch)
	if err := mergeExecCommitMerge(mainRepo, msg, rep.TaskID); err != nil {
		// A merge that staged clean should commit; a commit failure is a
		// reportable wart — the merge is already in the index.
		rep.Steps = append(rep.Steps, MergeExecStep{Step: "commit", Detail: err.Error()})
	} else {
		rep.Steps = append(rep.Steps, MergeExecStep{Step: "commit", OK: true})
	}
	removalaudit.BranchDelete(removalaudit.ArmMergeExecBranch, mainRepo, branch, rep.TaskID,
		"the merge of this branch was just committed")
	if why := branchDeleteNonDurableTipRefusal(mainRepo, branch, mergeExecIntegrationRef); why != "" {
		// The merge just landed, so this is normally unreachable — and when it
		// fires it is the shape the arm's own `-d` comment calls "weird": the
		// landing is reported, the ref is KEPT, and the reason says which lock
		// was missing (Rule 15).
		rep.Steps = append(rep.Steps, MergeExecStep{Step: "branch", Detail: why})
	} else if _, err := mergeExecGit(mainRepo, "branch", "-d", branch); err != nil {
		// -d refuses unmerged branches; the merge just landed so -d is
		// normally a no-op success. A refusal (master moved / worktree
		// checkout / weird shape) is a reportable wart, never a failed
		// merge — the MergedCleanupSweep serve pass (bugfix 2026-09-02)
		// removes the worktree and deletes the branch once it is provably
		// dead (merged + clean + no live task).
		rep.Steps = append(rep.Steps, MergeExecStep{Step: "branch", Detail: err.Error()})
	} else {
		rep.Steps = append(rep.Steps, MergeExecStep{Step: "branch", OK: true})
	}
	rep.Status = MergeExecStatusCompleted
	if _, err := TaskUpdateWithResult(rep.TaskID, taskStatusCompleted,
		fmt.Sprintf(mergeExecCompletedSummary, branch)); err != nil {
		return nil, fmt.Errorf("merge-execute: writeback failed: %w", err)
	}
	return rep, nil
}

// mergeExecClaimStart claims a pending row (pending → claimed via the
// Phase-1 CAS — TaskClaimExplicit) and settles the claim-lock record. It does
// NOT advance to in_progress (epic 01a1a87b): this lane merges a branch, it
// never exec's a child, so `in_progress` — which now means "a child process was
// exec'd here and is being waited on" — would be a false claim about a process
// that does not exist. The row reaches its verdict from `claimed`
// (taskAllowedTransitions' claimed→terminal edge), and lock_phase=both records
// the lock half this lane really did settle. A claimed row is re-stamped.
func mergeExecClaimStart(taskID string) error {
	if err := TaskClaimExplicit(taskID, mergeExecClaimHolder); err != nil {
		return fmt.Errorf("claim: %w", err)
	}
	if err := TaskStampLockPhase(taskID, TaskLockPhaseBoth); err != nil {
		return fmt.Errorf("lock phase: %w", err)
	}
	return nil
}

// mergeExecCommitMerge creates the merge commit through the ONE seam
// (internal/gitcommit), so the commit names the host that produced it AND the
// row whose branch just landed (`task: <id>`) — the association
// `git log --grep` could not answer before. `--no-verify` is preserved from the
// argv this replaced: a merge-lane commit must not run a hook that can block
// the lane.
//
// Named so a test can drive it directly against a real repository: inline, it
// would only be reachable through a full merge-backlog fixture.
func mergeExecCommitMerge(repo, msg, taskID string) error {
	_, err := gitcommit.Commit(context.Background(), repo,
		gitcommit.Message{Subject: msg},
		gitcommit.Options{TaskID: taskID, NoVerify: true, Timeout: mergeExecGitTimeout})
	return err
}

// mergeExecParseBranch extracts the branch from a "merge <branch> into
// master" summary. Returns "" when the summary does not match (not ours).
func mergeExecParseBranch(summary string) string {
	summary = strings.TrimSpace(summary)
	// The audit's verb shape: "merge " + branch + " into master".
	const prefix = "merge "
	const suffix = " into master"
	if !strings.HasPrefix(summary, prefix) || !strings.HasSuffix(summary, suffix) {
		return ""
	}
	branch := strings.TrimSpace(strings.TrimSuffix(strings.TrimPrefix(summary, prefix), suffix))
	if branch == "" || strings.ContainsAny(branch, " \t") {
		return "" // a branch name cannot contain whitespace — refuse to guess
	}
	return branch
}

// mergeExecRowRepo decodes the deliverable blob's repo field (the same
// lenient parse as the finish-cleanup chain — missing fields stay empty).
func mergeExecRowRepo(deliverable string) string {
	return finishCleanupParseDeliverable(deliverable).Repo
}

// MergeSameRepo reports whether two paths name the SAME repository — the main
// checkout and any LINKED WORKTREE of it included.
//
// The identity comes from `--git-common-dir` (mergeExecMainRepo), not from
// string equality, because the two paths a caller compares are commonly the
// main checkout and a worktree of it: a worker submits from inside
// /home/nick/workspace/gopi-<task> while the settings table sweeps
// /home/nick/work/GoPi, and a comparison that called those two different would
// silently fail to park a LIVE branch — the exact failure direction this
// predicate exists to prevent (a false park is a visible freeze; a missed park
// is an unfinished delivery on master).
//
// When either path cannot be resolved as a repository at all, the comparison
// degrades to the CLEANED paths and says so by its answer, never by an error: a
// caller asking "did the row name my repo?" must get a bool, and an unresolvable
// path is not evidence of sameness (so the fallback is strict equality).
func MergeSameRepo(a, b string) bool {
	if strings.TrimSpace(a) == "" || strings.TrimSpace(b) == "" {
		return false
	}
	am, aerr := mergeExecMainRepo(a)
	bm, berr := mergeExecMainRepo(b)
	if aerr != nil || berr != nil {
		return filepath.Clean(a) == filepath.Clean(b)
	}
	return am == bm
}

// mergeExecMainRepo resolves the MAIN checkout that owns repo (which may
// itself be the main checkout, or a linked worktree whose common git dir
// lives under the main repo). `git rev-parse --path-format=absolute
// --git-common-dir` yields <main>/.git (main checkout) or
// <main>/.git/worktrees/<name> (linked worktree) — the main repo dir is
// the path BEFORE the first ".git" component. A BARE repo resolves to
// itself (it has no master checkout to merge into — the merge fails with
// git's own words).
func mergeExecMainRepo(repo string) (string, error) {
	bare, err := mergeExecGit(repo, "rev-parse", "--is-bare-repository")
	if err != nil {
		return "", fmt.Errorf("%s: %w", repo, err)
	}
	if strings.TrimSpace(bare) == "true" {
		abs, aerr := filepath.Abs(repo)
		if aerr != nil {
			return repo, nil
		}
		return abs, nil
	}
	out, err := mergeExecGit(repo, "rev-parse", "--path-format=absolute", "--git-common-dir")
	if err != nil {
		return "", fmt.Errorf("%s: %w", repo, err)
	}
	common := strings.TrimSpace(out)
	if common == "" || !strings.Contains(common, ".git") {
		return repo, nil
	}
	abs, err := filepath.Abs(repo)
	if err != nil {
		return repo, nil
	}
	if idx := strings.Index(common, ".git"); idx > 0 {
		parent := filepath.Dir(common[:idx+len(".git")])
		if parent != "" {
			return parent, nil
		}
	}
	return abs, nil
}

// mergeExecSafeMergeBlock is the Go mirror of scripts/safe-merge.sh's FIVE
// REFUSALS (a/b/c/e/f; d is informational only). It returns nil when the
// branch may be merged, or the FIRST blocking refusal as a wrapped error —
// wrapped so the refusal cannot be discarded as a value and so the refusal
// KIND (safe_merge_contract.go) stays introspectable by the caller.
//
// THE PAIRING (Rule 14, documented here because the pair must not drift):
// the bash gate and this function are two homes of ONE contract. The kinds,
// the rendered sentences and the probe's exit-code table live in ONE Go
// definition — internal/tools/safe_merge_contract.go — with the bash half in
// scripts/lib/safe-merge-contract.sh; TestSafeMergeContractShellVocabularyParity
// fails when either side's vocabulary moves alone, and the mirror's own
// behaviour is pinned by TestMergeExecSafeMergeMissingRef /
// TestMergeExecSafeMergeUnrelatedHistory (task_merge_executor_safemerge_test.go),
// which drive THIS function and the executor step ③ that calls it.
//
// [e]/[f] were added because the four checks above certified a GHOST branch
// as safe: a name that no longer resolves sailed through [a]–[d] (each probe
// simply found nothing to object to) and the executor, mirroring the same
// blind spot, let the merge leg fail and reported it as "merge conflict" —
// telling a human to resolve conflicts against a branch that does not exist.
func mergeExecSafeMergeBlock(repo, branch string) error {
	// a) archive tag on the branch — the loser ledger says "decided, no".
	if out, err := mergeExecGit(repo, "tag", "-l", archiveBranchPrefix+branch); err == nil {
		// finishCleanupProbeClean reads the probe: an empty tag list comes
		// back as the empty-output sentinel and is NOT a tag.
		if taggedClean, _ := finishCleanupProbeClean(out); !taggedClean {
			return fmt.Errorf("safe-merge [%s]: branch has archive tag %s%s (archived loser — no merge)",
				safeMergeRefusalArchiveTag, archiveBranchPrefix, branch)
		}
	}
	// b) blacklist prefixes (loser/*, archive/*).
	for _, p := range strings.Split(mergeExecBlacklistPrefixes, ",") {
		if strings.HasPrefix(branch, p) {
			return fmt.Errorf("safe-merge [%s]: branch name hits blacklist prefix '%s'", safeMergeRefusalBlacklistPrefix, p)
		}
	}
	// c) worktree occupancy — the branch is checked out in a linked
	// worktree: safe-merge.sh's default policy is deny (the merge would
	// silently not apply the branch's commits / git refuses checked-out
	// updates).
	if out, err := mergeExecGitRaw(repo, finishCleanupWorktreeVerb, finishCleanupWorktreeListVerb, finishCleanupWorktreeListFlag); err == nil {
		for _, line := range strings.Split(out, "\n") {
			if strings.HasPrefix(line, finishCleanupPorcelainBranchPrefix) &&
				strings.TrimSpace(strings.TrimPrefix(line, finishCleanupPorcelainBranchPrefix)) == "refs/heads/"+branch {
				return fmt.Errorf("safe-merge [%s]: branch is checked out in a linked worktree (remove it first: git worktree remove <path>)",
					safeMergeRefusalWorktreeOccupied)
			}
		}
	}

	// e) ref EXISTENCE — the ghost-branch refusal. The probe is the ONE the
	// repo-sweep ref guard already owns (Rule 14: repoSweepRefPresent, not a
	// second hand-rolled rev-parse): it returns (false, nil) ONLY for git's
	// own named absence (exit 1, EMPTY output) and an error for anything
	// else — so "the branch is gone" and "the repo cannot answer" never
	// collapse into one verdict (Rule 15).
	refPresent, refErr := repoSweepRefPresent(repo, branch)
	if refErr != nil {
		return fmt.Errorf("safe-merge [%s]: %s: %w",
			safeMergeRefusalProbeFailed, fmt.Sprintf(safeMergeMsgProbeFailed, refErr.Error()), refErr)
	}
	if !refPresent {
		return fmt.Errorf("safe-merge [%s]: %s (git rev-parse --verify --quiet refs/heads/%s^{commit})",
			safeMergeRefusalMissingRef, fmt.Sprintf(safeMergeMsgMissingRef, branch), branch)
	}

	// f) common history — an orphan root (a scratch `git init` pushed into
	// the real repository) shares NO commit with the integration branch, so
	// every merge channel fails on it forever. The probe is the SAME question
	// the shell half asks — `git merge-base --all`, whose EMPTY output (on a
	// clean exit) is the one measured shape meaning "no common ancestor".
	//
	// WHY NOT `merge-base --is-ancestor`: "one is an ancestor of the other" is
	// a STRICTER question than "they share history", and it answers 1 for the
	// ORDINARY case — two branches that both descend from a common base and
	// have since diverged. That is the shape of every real feature branch, so
	// an is-ancestor probe refuses exactly the merges this guard exists to
	// let through (measured 2026-09-23: it refused the plain
	// `master`/`feat/e2e-merge` fixture in TestMergeExecConflict).
	base, ancErr := mergeExecCommonAncestorProbe(repo, mergeExecIntegrationBranch, branch)
	if ancErr != nil {
		return fmt.Errorf("safe-merge [%s]: %s (git merge-base --all %s %s): %w",
			safeMergeRefusalProbeFailed,
			fmt.Sprintf(safeMergeMsgAncestryProbeFailed, ancErr.Error()), mergeExecIntegrationBranch, branch, ancErr)
	}
	if !base {
		return fmt.Errorf("safe-merge [%s]: %s (git merge-base --all %s %s reports no common ancestor)",
			safeMergeRefusalUnrelatedHistory,
			fmt.Sprintf(safeMergeMsgUnrelatedHistory, mergeExecIntegrationBranch),
			mergeExecIntegrationBranch, branch)
	}
	return nil
}

// mergeExecCommonAncestorProbe answers "do these two refs share history?" by
// running `git merge-base --all`. The EXIT STATUS and the OUTPUT together are
// the answer, and each shape is one of the contract's three states:
//
//	exit 0, non-empty output → common history (true, nil)
//	exit 1, EMPTY output     → no common ancestor (false, nil)  ← the refusal
//	                           (MEASURED 2026-09-23 on a real orphan root: git
//	                           exits 1 with empty output, not 0)
//	any other shape          → the probe could not answer (false, err)
//
// The error return is reserved for "the probe never ran or did not answer" —
// collapsing that with "no common ancestor" is what would turn "I could not
// check" into a refusal on the merits (Rule 15).
func mergeExecCommonAncestorProbe(repo, left, right string) (bool, error) {
	ctx, cancel := context.WithTimeout(context.Background(), mergeExecGitTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, "git", "merge-base", "--all", left, right)
	cmd.Dir = repo
	out, err := cmd.Output()
	if err != nil {
		// A non-zero exit that carried NO output is git's own named answer for
		// "no common ancestor" — the ONE non-zero exit that is a verdict rather
		// than a failure (the same split repoSweepRefPresent makes for
		// rev-parse). Every other failure is "the probe did not answer".
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) && exitErr.ExitCode() == safeMergeAncestryRefusal && strings.TrimSpace(string(out)) == "" {
			return false, nil
		}
		return false, err
	}
	if strings.TrimSpace(string(out)) == "" {
		// Exit 0 with no output is NOT the orphan signature — it is a probe
		// shape we do not understand, so it must not read as a refusal OR a
		// pass (Rule 15: unreadable is not gone).
		return false, fmt.Errorf("merge-base --all %s %s exited 0 with no output", left, right)
	}
	return true, nil
}

// mergeExecIntegrationBranch is the integration branch the guard measures
// common history against — the merge-backlog audit's own target
// (mergeBacklogMergeTarget), so the guard and the queue that files the rows
// cannot disagree about what "master" is.
const mergeExecIntegrationBranch = mergeBacklogMergeTarget

// mergeExecIsAncestor reports whether branch's tip is an ancestor of HEAD
// (already merged). `git merge-base --is-ancestor` exits 0 when true —
// the exit code is the truth; output is empty either way.
func mergeExecIsAncestor(repo, branch string) bool {
	ctx, cancel := context.WithTimeout(context.Background(), mergeExecGitTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, "git", "merge-base", "--is-ancestor", branch, "HEAD")
	cmd.Dir = repo
	return cmd.Run() == nil
}

// mergeExecGitRaw runs one git command and returns the RAW (untruncated)
// trimmed output — the multi-line parser (worktree list --porcelain) needs
// every line, unlike the first-line summarizer.
//
// Its only caller PARSES the porcelain (mergeExecLinkedWorktree), so stdout is
// the value and stderr is diagnostics — the merged stream would let a
// `GIT_TRACE=1` line become a worktree entry (epic git-parsed-stdout-only).
func mergeExecGitRaw(dir string, args ...string) (string, error) {
	return finishCleanupGitParsedStdout(context.Background(), dir, mergeExecGitTimeout, args...)
}

// mergeExecGit runs one git command in dir with the executor timeout and
// returns (first meaningful output line, error). No shell — exec.Command
// argument contract.
func mergeExecGit(dir string, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), mergeExecGitTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	line := finishCleanupDiagnosticLine(out, err)
	if err != nil {
		return line, fmt.Errorf("git %s: %w: %s", strings.Join(args, " "), err, line)
	}
	return line, nil
}

// OpenMergeBacklogPendingDB opens the ledger for the serve-pass pending
// merge-backlog scan (the sweep's DB handle — one opener, Rule 2).
func OpenMergeBacklogPendingDB(ctx context.Context) (*sql.DB, error) {
	return openTasksDB(ctx, memoryDBPath())
}
