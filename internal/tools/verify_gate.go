package tools

// Post-merge verify gate (epic merger-verify-gate, 2026-09-12): the merger
// merges mechanically and never judges the RESULT on master — yet only the
// post-merge master is the truth (two branches each green alone can break
// each other; a REGRESSED BUGFIX is worse than an unshipped feature). This
// file is the merger tick's judgement half:
//
//	after a merge landed on master (clean merge OR the resolver-child
//	conflict path) AND the branch is a BUGFIXING branch (the machine-
//	decidable predicate: a ledger row delivering this branch carries the
//	`bug` tag), the merger spawns an INDEPENDENT verify child gopi that
//	judges the merged state ON THE REAL MASTER and BLOCKS (waitpid) on it —
//	gatekeeper, not dispatcher.
//
//	  PASS → keep the merge, continue (recorded as a merge-verify ledger
//	         event on the judged row; a human audits `gopi task show <id>`).
//	  FAIL → revert EXACTLY the merge (git revert -m 1 <merge-commit> when a
//	         merge commit exists, else reset --hard to the recorded pre-merge
//	         tip — asserted, never assumed), file a NEW rework task
//	         REFERENCING the old task id (append-only: the judged row's
//	         terminal status is never flipped, the old task is never re-run),
//	         and notify the operator through the existing notify side-channel.
//
// Everything here is a GIT/LEDGER JUDGEMENT (no process spawning), so the
// revert/detect/record core is unit-testable against real temp repos and a
// real ledger. The child spawn + the bounded waitpid live in the cmd/agent
// layer (merger_verify_child.go), exactly as the resolve child does.

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/nick/gopi/internal/tasksource"
	"github.com/nick/gopi/internal/tasktags"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/nick/gopi/internal/smokegate"
)

// --- gate vocabulary (Rule 1 / 1b / 14: one named place per value) ---

const (
	// MergeVerifyEvent is the notify event AND the ledger event name AND the
	// tag every gate-produced row carries: a gated merge is visible in one
	// grep (`merge-verify`) across the event trail, the tags and the
	// operator's GOPI_TASK_NOTIFY_EVENTS subscription — the same reuse
	// discipline as RevertedEvent (merge_revert.go).
	MergeVerifyEvent = "merge-verify"

	// MergeVerifyFailCap is the anti-loop cap: after this many terminal-
	// FAILED verdicts for the SAME branch the gate stops filing rework and
	// records a HUMAN-decision row instead. Precedent: the verify-reopen cap
	// and resolveRowMaxFailedBeforeRework — a machine loop that cannot
	// terminate must hand over, never spin forever.
	MergeVerifyFailCap = 3

	// MergeVerifyReason* are the verdict reasons recorded on the ledger event
	// and in the filed rework row.
	//
	// THE JUDGEMENT AXIS (epic verify-no-verdict-keeps-merge, 2026-09-14) runs
	// through these strings, and the gate's FATE turns on it: Fail is a
	// JUDGEMENT (evidence exists, the merged state was measured red ⇒ unwind),
	// while ChildDead/ChildTimeout are the ABSENCE of one (the merge is
	// unverified, which is not the same claim as broken ⇒ keep + requeue).
	// ReasonVerifyRetry is the witness that the second path ran.
	MergeVerifyReasonPass         = "post-merge verify PASS on master — merge kept"
	MergeVerifyReasonFail         = "post-merge verify FAIL on master — merge reverted, rework filed"
	MergeVerifyReasonChildDead    = "verify child exited without recording a verdict on the judged row"
	MergeVerifyReasonChildTimeout = "verify child exceeded the merger's wait cap"
	MergeVerifyReasonBudget       = "merge-verify failure cap reached — human decision required (no further automatic rework)"

	// MergeVerifyReasonVerifyRetry is recorded when NO judgement was ever
	// recorded — the child died, was killed at the wait cap, or never started.
	// It states the fate explicitly, because it is the OPPOSITE of Fail: the
	// merge is KEPT on master and the VERIFY (not a from-scratch rework) is
	// requeued for the same row/branch.
	MergeVerifyReasonVerifyRetry = "verify child recorded NO verdict — merge KEPT (unverified), the verify is requeued for the same branch"

	// MergeVerifyReasonRetryBudget is the retry lane's OWN cap witness: the
	// verify could not be completed after MergeVerifyRetryCap requeues, so the
	// decision is handed to a human. It is deliberately NOT
	// MergeVerifyReasonBudget (the FAILED-verdict cap): nothing here failed —
	// the merge is KEPT, and the machine is out of ways to MEASURE it. Two
	// different handovers must not be spelled the same, or an operator cannot
	// tell "the code keeps failing verification" from "the verifier keeps
	// dying".
	MergeVerifyReasonRetryBudget = "verify-retry cap reached — human decision required (the merge is KEPT; the verify was never completed)"

	// MergeVerifyRetryCap is the anti-loop cap on the retry lane: after this
	// many retry rows for the SAME (repo,branch) the gate stops requeueing and
	// records a human-decision row instead. Precedent: the resolve lane's
	// resolveRowMaxFailedBeforeRework — a lane whose gate can never judge must
	// terminate in a HANDOVER, and a requeue that always re-arms the same
	// unjudgeable merge is a machine that spins forever. Sized as "one
	// transient death + one timeout shape": 2 requeues = 3 verify attempts.
	MergeVerifyRetryCap = 2

	// mergeVerifyReworkSummaryPrefix heads every rework row the gate files.
	// A DIFFERENT verb from resolveRowReworkSummaryPrefix ("resolve merge
	// conflict: rework: ") on purpose: a merge that landed and then failed
	// verification is not a resolve failure, and the two lanes must not
	// dedup against each other.
	mergeVerifyReworkSummaryPrefix = "merge verify rework: "

	// mergeVerifyReworkTag is the tag vocabulary the merger's branch-rework
	// lane already speaks (resolveRowReworkTags pairs merge-conflict with it).
	mergeVerifyReworkTag = "branch-rework"

	// mergeVerifyVerifyRetrySummaryPrefix heads a requeued VERIFY row. A THIRD
	// distinct verb from mergeVerifyReworkSummaryPrefix and the resolve lane's
	// prefix, so no lane dedups against another: a verify that could not be
	// performed is not a rework of the code, and its row is not "a rework" to
	// the merger's lane predicate.
	mergeVerifyVerifyRetrySummaryPrefix = "merge verify retry: "

	// mergeVerifyRetryTag marks a requeued VERIFY row. It pairs with
	// mergeVerifyReworkTag on purpose — the branch IS still owned by the
	// merge-verify lane, so the merger's rework-lane predicate
	// (repo_sweep's branch-rework check) must see the lane as open and leave
	// the branch to the gate rather than spending a resolver on it.
	mergeVerifyRetryTag = "merge-verify-retry"
)

// MergeVerifyRecord is the ledger row that DELIVERED a branch — the input of
// the machine-decidable bugfixing predicate (the row carries the `bug` tag).
type MergeVerifyRecord struct {
	TaskID  string
	Summary string
	Tags    string
	Status  string
	Repo    string
	Branch  string
	// Worktree is the directory the delivering run RECORDED (the row's own
	// deliverable.worktree, backfilled at creation time). The verify gate hands
	// it to the independent verifier so the verification reuses the ground the
	// work is already in instead of minting a fresh checkout of its own: a
	// second worktree per judged branch is exactly how the ledger loses track
	// of a directory (see TaskWorktreeFor and the verify briefing).
	Worktree string
	// EndedAt is the row's terminal timestamp (0 when it never ended);
	// detection prefers the MOST RECENT row for a (repo,branch).
	EndedAt float64
}

// BugfixDetected reports the gate's predicate: the delivering row carries the
// reserved `bug` tag (the name states the VALUE, not an action — there is no
// get/is/handle prefix on a niladic reader). hasBugTag is the SAME matcher the queue's priority and
// the bug-flow tally use (tasks.go) — one definition of "bugfixing", so the
// gate can never drift from the bug-flow the operator reads.
func (r *MergeVerifyRecord) BugfixDetected() bool {
	return r != nil && hasBugTag(r.Tags)
}

// mergeVerifyNormalizeRepo makes repo paths comparable (a trailing slash or a
// ".." segment must not defeat the (repo,branch) match).
func mergeVerifyNormalizeRepo(repo string) string {
	repo = strings.TrimSpace(repo)
	if repo == "" {
		return ""
	}
	return filepath.Clean(repo)
}

// MergeVerifyRecordForBranch resolves the ledger row that DELIVERED branch in
// repo (nil = no such row). The match is exact on BOTH fields after parsing
// the deliverable JSON — never a naive substring match on the raw blob (a
// branch name is a prefix of another branch name: "fix/x" inside "fix/x-v2").
//
// Most-recent wins: a branch re-pushed after a rework can have several rows;
// the newest ENDED row is the one whose work is on master now.
//
// Best-effort by construction: a missing/unreadable ledger returns nil (the
// git-only face never opens one), which the gate reads as "not a bugfixing
// branch" — and the gate then never fires. That is the honest default: a gate
// that cannot PROVE the predicate must not block or revert merges.
func MergeVerifyRecordForBranch(repo, branch string) *MergeVerifyRecord {
	branch = strings.TrimSpace(branch)
	if branch == "" {
		return nil
	}
	dbPath := memoryDBPath()
	if dbPath == "" {
		return nil
	}
	if fi, err := os.Stat(dbPath); err != nil || fi.IsDir() {
		return nil
	}
	ctx := context.Background()
	db, err := openTasksDB(ctx, dbPath)
	if err != nil {
		taskNotifyLog("merge-verify detect open ledger: %v", err)
		return nil
	}
	defer db.Close()

	wantRepo := mergeVerifyNormalizeRepo(repo)
	// The LIKE prefilter is a NARROWING hint only (every deliverable writer
	// emits the literal "branch":"<name>"); the JSON parse below is the
	// authority.
	rows, err := db.QueryContext(ctx,
		`SELECT task_id, summary, tags, status, deliverable, COALESCE(ended_at, 0) FROM tasks `+
			`WHERE deliverable LIKE ?`, `%"branch":"`+branch+`"%`)
	if err != nil {
		taskNotifyLog("merge-verify detect query: %v", err)
		return nil
	}
	defer rows.Close()
	var best *MergeVerifyRecord
	for rows.Next() {
		var taskID, summary, tags, status, deliverable string
		var endedAt float64
		if err := rows.Scan(&taskID, &summary, &tags, &status, &deliverable, &endedAt); err != nil {
			taskNotifyLog("merge-verify detect scan: %v", err)
			return best
		}
		var d struct{ Repo, Branch, Worktree string }
		if err := json.Unmarshal([]byte(deliverable), &d); err != nil {
			continue // not JSON — not one of our rows
		}
		if strings.TrimSpace(d.Branch) != branch {
			continue
		}
		if wantRepo != "" && mergeVerifyNormalizeRepo(d.Repo) != wantRepo {
			continue
		}
		rec := &MergeVerifyRecord{
			TaskID: taskID, Summary: summary, Tags: tags, Status: status,
			Repo: d.Repo, Branch: d.Branch, Worktree: d.Worktree, EndedAt: endedAt,
		}
		if best == nil || endedAt >= best.EndedAt {
			best = rec
		}
	}
	if err := rows.Err(); err != nil {
		taskNotifyLog("merge-verify detect iter: %v", err)
	}
	return best
}

// MergeVerifyFailCount counts terminal-FAILED rows this gate produced for a
// (repo,branch) — the durable, crash-safe anti-loop counter (the same
// ledger-as-counter model as resolveRowFailedCount). excludeTaskID skips the
// row currently being judged: its own verdict must not count itself.
func MergeVerifyFailCount(repo, branch, excludeTaskID string) int {
	dbPath := memoryDBPath()
	if dbPath == "" {
		return 0
	}
	if fi, err := os.Stat(dbPath); err != nil || fi.IsDir() {
		return 0
	}
	ctx := context.Background()
	db, err := openTasksDB(ctx, dbPath)
	if err != nil {
		taskNotifyLog("merge-verify fail-count open ledger: %v", err)
		return 0
	}
	defer db.Close()

	wantRepo := mergeVerifyNormalizeRepo(repo)
	rows, err := db.QueryContext(ctx,
		`SELECT task_id, tags, deliverable FROM tasks WHERE status = ? AND tags LIKE ?`,
		taskStatusFailed, "%"+MergeVerifyEvent+"%")
	if err != nil {
		taskNotifyLog("merge-verify fail-count query: %v", err)
		return 0
	}
	defer rows.Close()
	n := 0
	for rows.Next() {
		var taskID, tags, deliverable string
		if err := rows.Scan(&taskID, &tags, &deliverable); err != nil {
			return n
		}
		if taskID == excludeTaskID || !mergeVerifyOwned(tags) {
			continue
		}
		var d struct{ Repo, Branch string }
		if err := json.Unmarshal([]byte(deliverable), &d); err != nil {
			continue
		}
		if strings.TrimSpace(d.Branch) != branch {
			continue
		}
		if wantRepo != "" && mergeVerifyNormalizeRepo(d.Repo) != wantRepo {
			continue
		}
		n++
	}
	if err := rows.Err(); err != nil {
		taskNotifyLog("merge-verify fail-count iter: %v", err)
	}
	return n
}

// mergeVerifyOwned reports whether a tag string carries this gate's marker as
// a WHOLE tag (never a substring of another tag such as "merge-verify-x").
func mergeVerifyOwned(tags string) bool {
	for _, t := range strings.Split(tags, ",") {
		if strings.TrimSpace(t) == MergeVerifyEvent {
			return true
		}
	}
	return false
}

// --- git side: tips, revert, branch preservation ---

// MergeVerifyHead is master's current tip in repo ("" on a git failure).
func MergeVerifyHead(repo string) string {
	head, err := mergeRevertGit(repo, "rev-parse", "HEAD")
	if err != nil {
		return ""
	}
	return head
}

// MergeVerifyTipPreMerge captures the pre-merge tip — the state a failed
// verify must restore. It prefers the recorded ref of the integration branch
// (the branch's own ref is authoritative even while a concurrent writer has
// the worktree on another commit) and falls back to HEAD. An empty master
// name defaults to "master".
func MergeVerifyTipPreMerge(repo, master string) string {
	if master == "" {
		master = "master"
	}
	if tip, err := mergeRevertGit(repo, "rev-parse", "--verify", "--quiet", refsHeadsPrefix+master); err == nil && tip != "" {
		return tip
	}
	return MergeVerifyHead(repo)
}

// MergeVerifyBranchTip reads branch's current tip in repo ("" = gone).
func MergeVerifyBranchTip(repo, branch string) string {
	tip, err := mergeRevertGit(repo, "rev-parse", "--verify", "--quiet", refsHeadsPrefix+branch)
	if err != nil {
		return ""
	}
	return tip
}

// MergeVerifyMergeCommit returns the merge commit a landing created whose
// FIRST PARENT is beforeTip, or "" when the landing fast-forwarded (no merge
// commit at all — the normal case for a sweep landing, since `git merge`
// fast-forwards whenever master is an ancestor of the branch).
func MergeVerifyMergeCommit(repo, beforeTip string) string {
	beforeTip = strings.TrimSpace(beforeTip)
	if beforeTip == "" {
		return ""
	}
	// "<sha> <parent1> <parent2> …" — a merge commit has ≥2 parents.
	out, err := mergeRevertGit(repo, "rev-list", "--parents", "-n", "1", "HEAD")
	if err != nil {
		return ""
	}
	fields := strings.Fields(out)
	if len(fields) < 3 || fields[1] != beforeTip {
		return ""
	}
	return fields[0]
}

// MergeVerifyRevertTo unwinds the merge that the verify judged FAIL,
// restoring master to the RECORDED pre-merge tip and ASSERTING the restore (a
// revert that silently leaves master elsewhere is the failure mode this whole
// epic exists to prevent). Returns the mechanism used, for the audit record.
//
//	mergeCommit != "" → `git revert -m 1 <merge-commit>` (the real merge
//	  commit the landing produced — the epic's mandated idiom).
//	mergeCommit == "" → `git reset --hard <preTip>`: the landing was a
//	  FAST-FORWARD with no merge commit, so there is nothing to revert
//	  (merge_revert.go documents this same truth for the build-failure
//	  revert). The reset goes to the recorded tip ONLY — never to an
//	  arbitrary point.
//
// The assertion is stated in the terms each mechanism can honor, and both are
// strict: the reset path proves `rev-parse HEAD == preTip` (identical sha —
// master IS the recorded tip again), the revert path proves the TREE is
// identical to the recorded tip (`git diff --quiet preTip HEAD`), because the
// mandated `git revert -m 1` idiom necessarily ADDS a revert commit on top —
// content-identical to the pre-merge state, by construction.
func MergeVerifyRevertTo(repo, preTip, mergeCommit string) (string, error) {
	if strings.TrimSpace(preTip) == "" {
		return "", fmt.Errorf("merge-verify revert: no recorded pre-merge tip — refusing to guess")
	}
	if mc := strings.TrimSpace(mergeCommit); mc != "" {
		if _, err := mergeRevertGit(repo, "revert", "-m", "1", "--no-edit", mc); err != nil {
			return "", fmt.Errorf("merge-verify revert -m 1 %s: %w", mc, err)
		}
		// Empty diff against the recorded tip == the landing's content is
		// fully undone; a non-empty diff means master is NOT back at the
		// pre-merge state and the gate must say so loudly.
		if _, err := mergeRevertGit(repo, "diff", "--quiet", preTip, "HEAD"); err != nil {
			return "", fmt.Errorf("merge-verify revert: master's tree differs from the recorded pre-merge tip %s after reverting %s (ASSERTED restore failed): %w",
				preTip, mc, err)
		}
		return "git revert -m 1 " + mc, nil
	}
	if _, err := mergeRevertGit(repo, "reset", "--hard", preTip); err != nil {
		return "", fmt.Errorf("merge-verify reset --hard %s: %w", preTip, err)
	}
	head, err := mergeRevertGit(repo, "rev-parse", "HEAD")
	if err != nil {
		return "", fmt.Errorf("merge-verify revert: assert HEAD: %w", err)
	}
	if head != preTip {
		return "", fmt.Errorf("merge-verify revert: master is at %s, want the recorded pre-merge tip %s (ASSERTED restore failed)",
			head, preTip)
	}
	return "git reset --hard " + preTip, nil
}

// MergeVerifyPreserveBranch pins a branch this gate is about to unwind, so a
// REVERTED bugfix is never lost work: the merge landing deleted the branch ref
// (taskMergeOne `branch -d`), and a reset --hard back to the pre-merge tip
// leaves that commit with NO ref — unreachable. An existing ref is left
// untouched (never clobber a branch a re-developer moved on); a deleted ref is
// recreated at the merged tip. Idempotent: an existing ref at the same sha is
// a no-op.
func MergeVerifyPreserveBranch(repo, branch, tip, preMergeTip string) error {
	branch = strings.TrimSpace(branch)
	tip = strings.TrimSpace(tip)
	if branch == "" || tip == "" || tip == strings.TrimSpace(preMergeTip) {
		return nil // nothing to preserve (a landing to itself is no work)
	}
	if _, err := mergeRevertGit(repo, "cat-file", "-e", tip+"^{commit}"); err != nil {
		return fmt.Errorf("merge-verify preserve %s: %s is not a commit in this repo: %w", branch, tip, err)
	}
	ref := refsHeadsPrefix + branch
	if cur, err := mergeRevertGit(repo, "rev-parse", "--verify", "--quiet", ref); err == nil && cur != "" {
		return nil // ref exists (same sha or moved on) — never clobber it
	}
	if _, err := mergeRevertGit(repo, "update-ref", ref, tip); err != nil {
		return fmt.Errorf("merge-verify preserve %s at %s: %w", branch, tip, err)
	}
	return nil
}

// MergeVerifyProtectedBranches returns the branch names checked out in a
// linked worktree of repo (ACTIVE BOND WORK — never a gate candidate).
func MergeVerifyProtectedBranches(repo string) (map[string]bool, error) {
	return repoSweepProtectedBranches(repo)
}

// --- ledger side: verdict record, notify, rework filing ---

// MergeVerifyNotify fires the merge-verify notify event through the existing
// side-channel (NotifySyntheticEvent → the same webhook/command lane as
// build-failed / merge-reverted), so an operator subscribed to
// GOPI_TASK_NOTIFY_EVENTS sees the gate's verdict without reading logs.
// Best-effort: delivery failure logs inside NotifySyntheticEvent.
func MergeVerifyNotify(event, repo, branch, summary string) {
	NotifySyntheticEvent(event, "", "", summary)
	taskNotifyLog("merge-verify [%s] %s %s: %s", event, repo, branch, summary)
}

// MergeVerifyRecordEvent appends the verdict to the JUDGED row's event trail
// (append-only: the row's status is never touched — the epic forbids flipping
// a terminal row). This is the human audit path: `gopi task show <task_id>`
// renders the latest event, and the merge-verify event names the verdict, the
// mechanism, the restored tip and the branch. Best-effort by design: a ledger
// write failure is logged, never allowed to overturn the git-level outcome the
// gate already enforced.
func MergeVerifyRecordEvent(taskID, detail string) {
	taskID = taskIDNormalize(taskID)
	if taskID == "" {
		return // no delivering row (git-only face) — nothing to annotate
	}
	ctx := context.Background()
	db, err := openTasksDB(ctx, memoryDBPath())
	if err != nil {
		taskNotifyLog("merge-verify event for %s: open ledger: %v", taskID, err)
		return
	}
	defer db.Close()
	taskEventWrite(ctx, db, taskID, MergeVerifyEvent, detail)
}

// MergeVerifyLatestEvent reads the newest event detail recorded on a row (the
// verdict audit trail — "" when none).
func MergeVerifyLatestEvent(taskID string) string {
	ctx := context.Background()
	db, err := openTasksDB(ctx, memoryDBPath())
	if err != nil {
		return ""
	}
	defer db.Close()
	_, detail := taskEventLatest(ctx, db, taskID)
	return detail
}

// MergeVerifyReworkOptions carries what the gate knows when it must file the
// rework half of a FAILED verify.
type MergeVerifyReworkOptions struct {
	Repo        string
	Master      string
	Branch      string
	OldTaskID   string
	OldSummary  string
	PreTip      string
	MergeCommit string
	Revert      string // the mechanism string MergeVerifyRevertTo returned
	Reason      string // verify FAIL (child verdict) or child-dead/timeout
	// VerifyEvidence is the verdict trail copied into the rework brief.
	VerifyEvidence string
}

// mergeVerifyReworkCauseClause says WHY the merge under a rework row was
// unwound — the one sentence a rework briefing must never get wrong.
//
// THE DEFECT (row 01a09e9b, measured): the clause was a fixed string claiming
// the verify child "judged the merged state ON THE REAL MASTER failing", while
// the SAME briefing carried the ledger's real reason in parentheses
// (`MergeVerifyReasonChildDead` = "verify child exited without recording a
// verdict") — a briefing that contradicts its own summary line, and that
// upgraded an ABSENCE OF MEASUREMENT into a measured failure. The rework agent
// then hunts a failure nobody ever observed (and, in the measured case, a
// branch that never existed).
//
// The judgement axis (epic verify-no-verdict-keeps-merge, verify_gate.go) is
// the authority: Fail is a JUDGEMENT (evidence exists), ChildDead/ChildTimeout
// are its ABSENCE. The two readings are spelled differently here — never
// collapsed into one sentence — and every other reason falls back to the
// generic FAIL wording (the only reason for which a forward rework of the
// branch is the owed deliverable).
func mergeVerifyReworkCauseClause(reason string) string {
	switch reason {
	case MergeVerifyReasonChildDead, MergeVerifyReasonChildTimeout:
		return "because the merger's post-merge verify child recorded NO VERDICT (" + reason + ": " +
			"nothing measured the merged state, so there is NO measured failure on master to reproduce)"
		// The clause deliberately does NOT say the merged state failed: a
		// no-verdict reason is an absence of judgement (the same distinction
		// the gate's own fate turns on), and a briefing that renders silence
		// as a failing measurement sends the rework agent hunting a red that
		// was never observed.
	}
	return "because the merger's post-merge verify child judged the merged state ON THE REAL MASTER failing " +
		"(" + reason + " — an observed, evidenced failure of that measured state)"
}

// MergeVerifyFileRework files the NEW task for a reverted branch: NEVER a
// re-run of the old task (whose row stays terminal and append-only) but a new
// row that REFERENCES the old task id in its summary and its payload, and
// carries the branch-rework vocabulary the merger's rework lane already
// speaks. Returns the new task id ("" when the row could not be located after
// the submit; a submit error is returned rather than swallowed).
func MergeVerifyFileRework(o MergeVerifyReworkOptions) (string, error) {
	reason := strings.TrimSpace(o.Reason)
	if reason == "" {
		reason = MergeVerifyReasonFail
	}
	var b strings.Builder
	fmt.Fprintf(&b, "You are the rework agent for a REVERTED merge in repo %q.\n\n", o.Repo)
	fmt.Fprintf(&b, "WHAT HAPPENED: branch %q was merged into %q and then REVERTED, %s. Master was restored "+
		"to the pre-merge tip %s by `%s`, so the work is NOT shipped.\n\n",
		o.Branch, o.Master, mergeVerifyReworkCauseClause(reason), o.PreTip, o.Revert)
	fmt.Fprintf(&b, "REFERENCED TASK: %s (%q) — that row is TERMINAL and is NEVER re-run; it delivered the "+
		"original fix and its verdict stays as recorded. This task is the FORWARD rework of the same branch.\n\n",
		o.OldTaskID, o.OldSummary)
	fmt.Fprintf(&b, "TASK:\n")
	fmt.Fprintf(&b, "1. Check out branch %q in repo %q (its work is preserved: the gate re-created the branch "+
		"ref at the merged tip). Reproduce the FAILURE the verifier saw ON MASTER: merge the branch into "+
		"master in a scratch worktree and run the project's own verification there — `go build ./...` plus "+
		"the SMOKE tests SCOPED TO WHAT THE BRANCH TOUCHED: `%s` (`%s`) — %s.\n",
		o.Branch, o.Repo, smokegate.ScopedCommand, smokegate.Command, smokegate.Doc)
	fmt.Fprintf(&b, "2. The bug the original fix addressed is presumably still fixed — what broke is the "+
		"POST-MERGE state (an interaction with newer master, a test the fix invalidated, an incomplete fix). "+
		"Fix THAT: keep the original fix's intent AND make the merged state pass.\n")
	fmt.Fprintf(&b, "3. Re-run the project's build + tests on the merged state until green, then leave the "+
		"branch mergeable — the merger lands it on a later tick and the verify gate judges it again. Do NOT "+
		"force-push over shared history.\n\n")
	fmt.Fprintf(&b, "Verify evidence from the gate (audit trail): task %s event %q: %s",
		o.OldTaskID, MergeVerifyEvent, strings.TrimSpace(o.VerifyEvidence))

	summary := mergeVerifyReworkSummaryPrefix + o.Branch
	if old := strings.TrimSpace(o.OldTaskID); old != "" {
		summary = summary + " (was " + old + ")"
	}
	_, reworkID, err := TaskSubmitReturningID(TaskSubmitOptions{
		Summary: summary,
		Payload: b.String(),
		Tags:    MergeVerifyEvent + "," + mergeVerifyReworkTag + "," + tasktags.ReworkLineageTag,
		Source:  tasksource.Merger,
		Repo:    o.Repo,
	})
	if err != nil {
		return "", err
	}
	// Rule 33's supersession edge, and this lane is the one that knows BOTH
	// ends exactly: the gate reverted the merge of o.OldTaskID's work and files
	// THIS row as its forward rework, so every row depending on the reverted
	// row is re-pointed at the rework row — through the ONE shared
	// implementation every branch-rework lane calls
	// (internal/tools/rework_reparent_children.go). o.OldTaskID may be empty (a
	// reverted merge whose row could not be named): the shared implementation
	// then REFUSES by name rather than guessing which row was superseded.
	rep := ReworkReparentChildren(o.OldTaskID, reworkID,
		fmt.Sprintf(reworkReparentReasonFmt, strings.TrimSpace(o.OldTaskID), reworkID, strings.TrimSpace(o.OldTaskID)))
	taskNotifyLog("%s", rep.Render())
	for _, n := range rep.Notes {
		taskNotifyLog("%s", n)
	}
	// TaskSubmit returns a rendered confirmation, not the bare id: locate the
	// row by its deterministic summary and stamp the deliverable with
	// repo+branch+origin so the notify line, the operator and the gate's own
	// rework detection all have the durable handle. The RETURNED id stays the
	// stamp's own resolution, which is the row the summary names even on the
	// mirror leg (where TaskSubmitReturningID hands back "").
	return mergeVerifyStampReworkDeliverable(summary, o), nil
}

// MergeVerifySetDeliverableBranchForTest stamps the (repo,branch) deliverable
// blob a delivering task row carries. In production that blob is written by
// the executor's backfill (taskBackfillDeliverableBranch) and by the sync /
// forward path; a cmd-layer e2e test constructs a delivering row without
// running an executor session, so this seam writes the SAME shape the gate
// detects on (the precedent is runFinishCleanupChainForTest, which exposes a
// production chain body to the same layer).
func MergeVerifySetDeliverableBranchForTest(taskID, repo, branch string) error {
	ctx := context.Background()
	db, err := openTasksDB(ctx, memoryDBPath())
	if err != nil {
		return err
	}
	defer db.Close()
	blob, _ := json.Marshal(map[string]string{"repo": repo, "branch": branch}) //stylegate:ignore in-process string map: marshal cannot fail (no channels/funcs/NaN); the stamp is best-effort telemetry, never a gate input
	_, err = db.ExecContext(ctx,
		`UPDATE tasks SET deliverable = ?, updated_at = ? WHERE task_id = ?`,
		string(blob), float64(time.Now().Unix()), taskIDNormalize(taskID))
	return err
}

// MergeVerifySetDeliverableGroundForTest stamps repo + branch + WORKTREE — the
// three axes `task stranded` judges. It exists beside the branch-only seam above
// because a fixture for the stranded reader must record a GROUND (the arm that
// reports "the directory is gone"); a hand-rolled UPDATE in a CLI test would be
// the second writer Rule 32 forbids.
func MergeVerifySetDeliverableGroundForTest(taskID, repo, branch, worktree string) error {
	ctx := context.Background()
	db, err := openTasksDB(ctx, memoryDBPath())
	if err != nil {
		return err
	}
	defer db.Close()                                                                                 //stylegate:ignore test seam handle
	blob, _ := json.Marshal(map[string]string{"repo": repo, "branch": branch, "worktree": worktree}) //stylegate:ignore in-process string map: marshal cannot fail (no channels/funcs/NaN)
	_, err = db.ExecContext(ctx,
		`UPDATE tasks SET deliverable = ?, updated_at = ? WHERE task_id = ?`,
		string(blob), float64(time.Now().Unix()), taskIDNormalize(taskID))
	return err
}

// MergeVerifyRowIDForTest resolves the newest ledger row's task_id for a
// summary — the seam a caller needs because TaskSubmit returns a rendered
// confirmation rather than the bare id (the same reason
// resolveRowStampDeliverableBranch keys its UPDATE on the summary).
func MergeVerifyRowIDForTest(summary string) string {
	ctx := context.Background()
	db, err := openTasksDB(ctx, memoryDBPath())
	if err != nil {
		taskNotifyLog("merge-verify row-id probe: open ledger: %v", err)
		return ""
	}
	defer db.Close()
	var id string
	if err := db.QueryRowContext(ctx,
		`SELECT task_id FROM tasks WHERE summary = ? ORDER BY id DESC LIMIT 1`,
		strings.TrimSpace(summary)).Scan(&id); err != nil {
		return ""
	}
	return id
}

// MergeVerifyRowPayloadForTest reads a row's payload through the ledger seam —
// the retry-lane e2e asserts what the requeued VERIFY row actually instructs
// (never a re-reading of the source that wrote it).
func MergeVerifyRowPayloadForTest(taskID string) (string, error) {
	ctx := context.Background()
	db, err := openTasksDB(ctx, memoryDBPath())
	if err != nil {
		return "", fmt.Errorf("merge-verify payload probe: open ledger: %w", err)
	}
	defer db.Close()
	var payload string
	if err := db.QueryRowContext(ctx,
		`SELECT payload FROM tasks WHERE task_id = ?`, taskIDNormalize(taskID)).Scan(&payload); err != nil {
		return "", fmt.Errorf("merge-verify payload probe %s: %w", taskID, err)
	}
	return payload, nil
}

// MergeVerifyReworkRows lists this gate's rework rows for a (repo,branch) —
// the rows the FAIL half filed, as the operator/audit sees them. Returns
// (task_id, status, payload, summary) tuples, newest last.
func MergeVerifyReworkRows(repo, branch string) (rows [][4]string, err error) {
	ctx := context.Background()
	db, err := openTasksDB(ctx, memoryDBPath())
	if err != nil {
		return nil, err
	}
	defer db.Close()
	cur, err := db.QueryContext(ctx,
		`SELECT task_id, status, payload, summary, deliverable FROM tasks WHERE summary LIKE ? ORDER BY id`,
		"%"+mergeVerifyReworkSummaryPrefix+branch+"%")
	if err != nil {
		return nil, err
	}
	defer cur.Close()
	wantRepo := mergeVerifyNormalizeRepo(repo)
	for cur.Next() {
		var id, status, payload, summary, deliverable string
		if err := cur.Scan(&id, &status, &payload, &summary, &deliverable); err != nil {
			return nil, err
		}
		var d struct {
			Repo   string `json:"repo"`
			Branch string `json:"branch"`
		}
		_ = json.Unmarshal([]byte(deliverable), &d) //stylegate:ignore best-effort decode of an optional field; the zero value is the documented fallback
		if wantRepo != "" && mergeVerifyNormalizeRepo(d.Repo) != wantRepo {
			continue
		}
		rows = append(rows, [4]string{id, status, payload, summary})
	}
	return rows, cur.Err()
}

// MergeVerifyRowStatus reads a ledger row's status through the exported
// reader (the gate's verdict channel), so a caller never re-implements it.
func MergeVerifyRowStatus(taskID string) (string, error) {
	return TaskStatusOf(taskID)
}

// MergeVerifyVerifyRetryOptions carries what the gate knows when a verify
// ended with NO judgement and the VERIFY (not the code) must be requeued.
type MergeVerifyVerifyRetryOptions struct {
	Repo   string
	Master string
	Branch string
	// OldTaskID is the row that DELIVERED the branch — the row the requeued
	// verify must judge (the verdict is written on it, exactly as the gate's
	// own child would have).
	OldTaskID string
	// PreTip / AfterTip are master's recorded pre-merge and merged tips: the
	// KEPT merge's witnesses, carried into the row so an operator (and the
	// next pass) can tell which landing is under judgement.
	PreTip   string
	AfterTip string
	// Reason is the no-judgement reason (child-dead / child-timeout / budget).
	Reason string
	// Evidence is the gate's audit trail copied into the brief.
	Evidence string
	// BudgetReached reports that the retry lane's own cap is spent: the row is
	// then a HUMAN-DECISION handover, never another automatic attempt.
	BudgetReached bool
}

// MergeVerifyFileVerifyRetry files the requeued VERIFY for an UNJUDGED merge:
// a NEW pending row that re-runs the VERIFY on the SAME row and branch, with
// NO revert and NO code-rework instruction.
//
// WHY THIS EXISTS (epic verify-no-verdict-keeps-merge, 2026-09-14): the gate
// used to treat "no judgement" exactly like "judged FAIL" — it unwound the
// landing (`git revert -m 1`) and filed a from-scratch rework. Measured
// incident: delivering row 01a09d2d-06e6-7c5c-976c-7c526e87e75d, elapsed
// 40m42s, NO verdict recorded; master was restored, the merge was consumed,
// and a rework row instructing a re-developer to redo work that had never been
// judged was filed. An absent measurement is not evidence of breakage: the
// honest response is to KEEP master as merged and re-measure.
//
// The row's contract is the opposite of the rework row's in three places, and
// each is load-bearing:
//
//	① it says the merge is KEPT (a re-developer reading "reverted" would
//	   rewrite shipped code);
//	② it instructs a VERIFY (`gopi task verify <id>` / the same locate-build-
//	   test pass), never an edit — the branch is untouched and its ref may
//	   already be consumed by the landing (git branch -d), so any instruction
//	   to "check out the branch" would be unexecutable;
//	③ it carries mergeVerifyRetryTag + mergeVerifyReworkTag so the merger's
//	   lane predicate still sees the branch as lane-owned (no resolver is
//	   spent on it) while the row ITSELF is a third, distinct verb.
//
// Dedup: one OPEN retry row per (repo,branch) — a tick that re-runs while the
// previous retry is still pending must not mint a second row (the same rule
// the rework lane follows).
func MergeVerifyFileVerifyRetry(o MergeVerifyVerifyRetryOptions) (string, error) {
	reason := strings.TrimSpace(o.Reason)
	if reason == "" {
		reason = MergeVerifyReasonChildDead
	}
	if o.BudgetReached {
		reason = MergeVerifyReasonRetryBudget
	}
	if !o.BudgetReached && len(MergeVerifyPendingRetryRows(o.Repo, o.Branch)) > 0 {
		// The previous requeue is STILL OPEN — the lane is armed and a second
		// row would double-verify the same landing. Report the existing row
		// rather than minting a duplicate (never a silent no-op: the caller
		// records the id it found).
		rows := MergeVerifyPendingRetryRows(o.Repo, o.Branch)
		return rows[len(rows)-1].TaskID, nil
	}

	var b strings.Builder
	if o.BudgetReached {
		fmt.Fprintf(&b, "HUMAN DECISION NEEDED: the post-merge verify of branch %q in repo %q could not be "+
			"performed after %d automatic attempts (%s). The merge is KEPT on master — the code was never "+
			"judged, so it was never found broken.\n\n", o.Branch, o.Repo, MergeVerifyRetryCap+1, reason)
	} else {
		fmt.Fprintf(&b, "You are the VERIFIER for an UNJUDGED merge in repo %q — a re-verify, not a rework.\n\n", o.Repo)
	}
	fmt.Fprintf(&b, "WHAT HAPPENED: branch %q was merged into %q (master %s → %s) and the post-merge verify "+
		"child ended WITHOUT recording a verdict: %s.\n\n", o.Branch, o.Master, o.PreTip, o.AfterTip, reason)
	fmt.Fprintf(&b, "THE MERGE WAS **KEPT**. An absent judgement is not a judgement: nothing has been reverted "+
		"and nothing is known to be broken. Do NOT edit, redevelop or re-land the branch — its ref may already "+
		"be consumed by the landing (the branch delete the close-out chain performs), so any instruction to check it out would be unexecutable.\n\n")
	fmt.Fprintf(&b, "TASK: re-run the VERIFICATION the previous child never completed, on the merged master state %s:\n", o.AfterTip)
	fmt.Fprintf(&b, "1. Locate the delivered fix of task %s in repo %q and read its acceptance criteria (payload).\n", o.OldTaskID, o.Repo)
	fmt.Fprintf(&b, "2. Build the project and run its own tests against that fix ON THE MERGED STATE — judge the "+
		"DELTA the merge added, never redness the tree already carried.\n")
	fmt.Fprintf(&b, "3. Record the verdict ON THAT SAME ROW with the task CLI (bash): PASS → "+
		"`gopi task update %s --status verified --result \"evidence: <the test command> + its output tail\"`, "+
		"or FAIL → `gopi task update %s --status failed --result \"reason: <what failed>\"`.\n", o.OldTaskID, o.OldTaskID)
	fmt.Fprintf(&b, "4. Start the long build/test command EARLY (bash job). The smoke command is `%s`, SCOPED TO "+
		"WHAT THE MERGE TOUCHED — `%s` (%s); a child that returns unjudged again spends another retry, and the "+
		"lane has a CAP.\n\n", smokegate.Command, smokegate.ScopedCommand, smokegate.Doc)
	// STEP 5 STATES THE HEAVY-TEST RULE AND NOTHING ELSE. It used to open by
	// restating the smoke command — the same three facts (`make test`, its
	// scoped form, smokegate.Doc) step 4 had just given, rendered a second time
	// because each step was written to stand alone. A briefing whose reader is
	// ONE child does not need standalone steps: measured on this producer, the
	// payload carried `make test` and the scoped form TWICE each, so the child
	// read the same prescription twice and could only guess whether the second
	// was a second requirement (Rule 1's defect in prose form — the same one
	// this file's sibling briefing had in tasks.go before the verb was
	// retired). The command is stated ONCE, in step 4; this step adds only what
	// step 4 does not say. Pinned by
	// TestTheVerifyRetryBriefingStatesTheSmokeCommandOnce.
	fmt.Fprintf(&b, "5. SMOKE, BRANCH-SCOPED — never the whole tree (the command is stated in step 4, "+
		"once, on purpose: it is ONE requirement). Tests "+
		"classified HEAVY (minutes of real device work, wide parallelism, host-condition-dependent) skip "+
		"themselves unless GOPI_HEAVY_TESTS is truthy — they are enumerated in testdata/heavy-tests.tsv, and "+
		"the Makefile's smoke target points the skip ledger variable at the file a run writes its skipped set "+
		"to. A skip is NOT a failure and NOT evidence about the code: judge the DELTA that ran, and if your "+
		"verdict depends on a heavy test, run `make test-heavy` deliberately (off-peak) and quote that output "+
		"instead. NEVER manufacture device load to force a number — GOPI.md Rule 27, mechanically gated.\n\n")
	fmt.Fprintf(&b, "6. HEAVY TESTS DID NOT RUN, BY DESIGN (the heavy-test boundary, 2026-09-17). Two things are true "+
		"of your run, and neither is a failure you must repair: (a) the heavy set SKIPPED — a skip is the "+
		"classification working, not a gap in the code; (b) if this child itself tried to opt in "+
		"(`GOPI_HEAVY_TESTS`), the opt-in was REFUSED loudly — a WORK context (an agent session above it, a "+
		"task child such as yours) may not spend that CPU on its own authority, so the run stayed smoke-only "+
		"and the refusal named its reason on stderr. You cannot obtain the heavy set, and you must not try: "+
		"the authority is the OPERATOR's declaration on his own terminal, which `make test-heavy` sets for "+
		"him, and the executor spawn seam strips that declaration from your environment so it cannot travel "+
		"down to you. If your verdict genuinely depends on a heavy test, record NO verdict: write the result "+
		"naming that heavy test and the command that would run it, and leave the decision to the operator. "+
		"Never start it yourself, and never manufacture device load to force a number (GOPI.md Rule 27).\n\n")
	fmt.Fprintf(&b, "Audit trail from the gate: task %s event %q: %s", o.OldTaskID, MergeVerifyEvent, strings.TrimSpace(o.Evidence))

	summary := mergeVerifyVerifyRetrySummaryPrefix + o.Branch
	if old := strings.TrimSpace(o.OldTaskID); old != "" {
		summary = summary + " (judge " + old + ")"
	}
	if _, err := TaskSubmit(TaskSubmitOptions{
		Summary: summary,
		Payload: b.String(),
		Tags:    MergeVerifyEvent + "," + mergeVerifyReworkTag + "," + mergeVerifyRetryTag + "," + tasktags.ReworkLineageTag,
		Source:  tasksource.Merger,
		Repo:    o.Repo,
	}); err != nil {
		return "", err
	}
	return mergeVerifyStampRetryDeliverable(summary, o), nil
}

// MergeVerifyRetryRow is one requeued-verify row as the gate and the operator
// read it.
type MergeVerifyRetryRow struct {
	TaskID string
	Status string
}

// MergeVerifyPendingRetryRows lists the OPEN (pending or in-progress) verify
// retry rows for a (repo,branch) — the gate's dedup witness AND the anti-loop
// counter (a pending retry means the lane is already armed; the count of them
// over time is what the cap bounds).
func MergeVerifyPendingRetryRows(repo, branch string) []MergeVerifyRetryRow {
	dbPath := memoryDBPath()
	if dbPath == "" {
		return nil
	}
	if fi, err := os.Stat(dbPath); err != nil || fi.IsDir() {
		return nil
	}
	ctx := context.Background()
	db, err := openTasksDB(ctx, dbPath)
	if err != nil {
		taskNotifyLog("merge-verify retry-count open ledger: %v", err)
		return nil
	}
	defer db.Close()
	cur, err := db.QueryContext(ctx,
		`SELECT task_id, status, summary, deliverable FROM tasks WHERE summary LIKE ? ORDER BY id`,
		"%"+mergeVerifyVerifyRetrySummaryPrefix+branch+"%")
	if err != nil {
		taskNotifyLog("merge-verify retry-count query: %v", err)
		return nil
	}
	defer cur.Close()
	wantRepo := mergeVerifyNormalizeRepo(repo)
	var out []MergeVerifyRetryRow
	for cur.Next() {
		var taskID, status, summary, deliverable string
		if err := cur.Scan(&taskID, &status, &summary, &deliverable); err != nil {
			return out
		}
		var d struct{ Repo, Branch string }
		if err := json.Unmarshal([]byte(deliverable), &d); err != nil {
			continue
		}
		if wantRepo != "" && mergeVerifyNormalizeRepo(d.Repo) != wantRepo {
			continue
		}
		// A row that already reached a terminal state spent its attempt: only
		// OPEN rows arm the lane and only OPEN rows must block a duplicate.
		if status != taskStatusPending && status != taskStatusInProgress {
			continue
		}
		out = append(out, MergeVerifyRetryRow{TaskID: taskID, Status: status})
	}
	if err := cur.Err(); err != nil {
		taskNotifyLog("merge-verify retry-count iter: %v", err)
	}
	return out
}

// MergeVerifyRetryAttempts counts EVERY retry row ever filed for a
// (repo,branch) — terminal ones included — because it is the anti-loop counter:
// a lane that keeps failing to judge must stop after MergeVerifyRetryCap
// attempts, and a count that only saw OPEN rows would reset itself the moment
// a retry was claimed.
func MergeVerifyRetryAttempts(repo, branch string) int {
	dbPath := memoryDBPath()
	if dbPath == "" {
		return 0
	}
	if fi, err := os.Stat(dbPath); err != nil || fi.IsDir() {
		return 0
	}
	ctx := context.Background()
	db, err := openTasksDB(ctx, dbPath)
	if err != nil {
		taskNotifyLog("merge-verify retry-attempts open ledger: %v", err)
		return 0
	}
	defer db.Close()
	cur, err := db.QueryContext(ctx,
		`SELECT deliverable FROM tasks WHERE summary LIKE ?`,
		"%"+mergeVerifyVerifyRetrySummaryPrefix+branch+"%")
	if err != nil {
		taskNotifyLog("merge-verify retry-attempts query: %v", err)
		return 0
	}
	defer cur.Close()
	wantRepo := mergeVerifyNormalizeRepo(repo)
	n := 0
	for cur.Next() {
		var deliverable string
		if err := cur.Scan(&deliverable); err != nil {
			return n
		}
		var d struct{ Repo, Branch string }
		if err := json.Unmarshal([]byte(deliverable), &d); err != nil {
			continue
		}
		if wantRepo != "" && mergeVerifyNormalizeRepo(d.Repo) != wantRepo {
			continue
		}
		n++
	}
	if err := cur.Err(); err != nil {
		taskNotifyLog("merge-verify retry-attempts iter: %v", err)
	}
	return n
}

func mergeVerifyStampRetryDeliverable(summary string, o MergeVerifyVerifyRetryOptions) string {
	ctx := context.Background()
	db, err := openTasksDB(ctx, memoryDBPath())
	if err != nil {
		taskNotifyLog("merge-verify retry stamp: open ledger: %v", err)
		return ""
	}
	defer db.Close()
	var taskID string
	if err := db.QueryRowContext(ctx,
		`SELECT task_id FROM tasks WHERE summary = ? ORDER BY id DESC LIMIT 1`, summary).Scan(&taskID); err != nil {
		taskNotifyLog("merge-verify retry stamp: locate row: %v", err)
		return ""
	}
	blob, _ := json.Marshal(map[string]string{
		"repo": o.Repo, "branch": o.Branch, "origin_task": strings.TrimSpace(o.OldTaskID),
		"kept_tip": strings.TrimSpace(o.AfterTip),
	}) //stylegate:ignore in-process string map: marshal cannot fail, and a missing blob is re-detected by the gate's own retry path
	if _, err := db.ExecContext(ctx,
		`UPDATE tasks SET deliverable = ?, updated_at = ? WHERE task_id = ?`,
		string(blob), float64(time.Now().Unix()), taskID); err != nil {
		taskNotifyLog("merge-verify retry stamp %s: %v", taskID, err)
	}
	return taskID
}

// the freshly filed rework row and returns that row's task_id. Best-effort:
// a stamp failure only weakens the annotation, never fails the filing (the
// row is queued and claimable either way).
func mergeVerifyStampReworkDeliverable(summary string, o MergeVerifyReworkOptions) string {
	ctx := context.Background()
	db, err := openTasksDB(ctx, memoryDBPath())
	if err != nil {
		taskNotifyLog("merge-verify rework stamp: open ledger: %v", err)
		return ""
	}
	defer db.Close()
	var taskID string
	if err := db.QueryRowContext(ctx,
		`SELECT task_id FROM tasks WHERE summary = ? ORDER BY id DESC LIMIT 1`, summary).Scan(&taskID); err != nil {
		taskNotifyLog("merge-verify rework stamp: locate row: %v", err)
		return ""
	}
	blob, _ := json.Marshal(map[string]string{
		"repo": o.Repo, "branch": o.Branch, "origin_task": strings.TrimSpace(o.OldTaskID),
	}) //stylegate:ignore same in-process string map shape as above: marshal cannot fail, and a missing blob is re-detected by the gate's own rework path
	if _, err := db.ExecContext(ctx,
		`UPDATE tasks SET deliverable = ?, updated_at = ? WHERE task_id = ?`,
		string(blob), float64(time.Now().Unix()), taskID); err != nil {
		taskNotifyLog("merge-verify rework stamp %s: %v", taskID, err)
	}
	return taskID
}

