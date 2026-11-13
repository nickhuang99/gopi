package tools

// D3 — the merge-backlog DISPATCH guard: a pending merge row whose branch ref
// does not resolve is never handed to a solver.
//
// THE DEFECT (measured 2026-09-23, row 01a0c614-00aa-7e17-8be5-b37ef5a0fcec).
// An e2e fixture pushed `feat/e2e-orphan-probe1`, the audit filed the ordinary
// "merge feat/e2e-orphan-probe1 into master" row, and the fixture's own
// cleanup then DELETED the branch ref. The row stayed pending, so a whole solver
// session was claimed and paid for on a branch that cannot be merged:
// `gopi merge <branch>` refuses it by name and `git merge` says "not something
// we can merge" — but only AFTER the session was spent.
//
// WHY IT LIVES AT CLAIM TIME, and why `taskRowClaimable` was NOT widened.
// The pending view's `[queue-skip]` marker and the claim ladder are ONE pure
// predicate over ROW FIELDS (taskRowClaimable — Rule 14, no I/O): whether a
// branch resolves is a fact about a REPOSITORY, not about a row, so folding it
// in would make the predicate query git and the marker's answer depend on the
// ledger reader's own cwd. The claim ATTEMPT is the right place — it is the
// same funnel every dispatcher shares (worker round / serve dispatch /
// `task run next`) and the same shape the merge-verify staleness guard already
// established there: refuse the row loudly, close it with the reason, and let
// the queue drain past it.
//
// THE LANE IS NARROW ON PURPOSE. Only a merge-backlog FAMILY row
// (mergeBacklogFamilyBranch: the audit verb or the scan verb) is governed, and
// only when the branch ref is MEASURABLY absent — "git could not answer" is
// unmeasured, never a refusal (Rule 15: unreadable is not the same fact as
// gone), and a branch whose work is provably ON the integration branch (the
// merger deletes a branch precisely when its merge succeeded) is never
// cancelled as a ghost: the D2 terminal guard's own probe decides that, and
// this guard defers to it rather than duplicating the landing ladder.

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"strings"
)

// Dispatch-guard tunables (Rule 1: every literal in one place).
const (
	// mergeBacklogGhostEvent is the notify event fired when the guard closes a
	// ghost-branch row (same side-channel as merge-verify-stale-rework /
	// build-failed, so an operator subscribed to GOPI_TASK_NOTIFY_EVENTS sees
	// the refusal without reading logs).
	mergeBacklogGhostEvent = "merge-backlog-ghost-ref"

	// MergeBacklogGhostMarker is the durable grep marker of a ghost-ref close;
	// it heads the row's summary so `task list` shows WHY the row ended.
	MergeBacklogGhostMarker = "[merge-backlog ghost-ref guard]"

	// MergeBacklogGhostReasonPrefix heads the row's `reason` column — the
	// operator-visible one-liner naming the missing ref and the measured
	// probe, so a reader can re-run the exact command this guard ran.
	MergeBacklogGhostReasonPrefix = "merge-backlog ghost-ref guard — the row's branch does not resolve: "

	// mergeBacklogGhostSweepCap bounds how many ghost rows ONE claim attempt
	// closes before moving on (a ledger full of them must not turn a claim
	// into an unbounded loop — same cap discipline as the staleness sweep).
	mergeBacklogGhostSweepCap = 8

	// mergeBacklogGhostGuardHolder is the claimed_by stamp the guard writes
	// when it advances a ghost row to its terminal. It names the GUARD as the
	// closer (never an operator pid), so the claim trail shows no worker ran it.
	mergeBacklogGhostGuardHolder = "merge-backlog-ghost-ref-guard"

	// mergeBacklogRetiredEvent is the notify event fired when the guard closes
	// a row whose branch is a RETIRED, DECIDED EXCLUSION (a different fact from
	// a ghost ref: the branch was decided, not lost), so an operator subscribed
	// to everything hears that refusal too.
	mergeBacklogRetiredEvent = "merge-backlog-retired-branch"

	// MergeBacklogRetiredMarker heads a retirement close, and it is a SEPARATE
	// marker from the ghost one on purpose: "the row's ref does not resolve" and
	// "git has recorded a decision that voids the row" are different facts, and
	// a reader of `task list` must not have to open the row to tell them apart
	// (Rule 15 — a sentence must not collapse two measurements into one).
	MergeBacklogRetiredMarker = "[merge-backlog retired-branch guard]"

	// MergeBacklogRetiredReasonPrefix heads the retirement close's `reason`
	// column — the operator-visible one-liner naming the decision.
	MergeBacklogRetiredReasonPrefix = "merge-backlog retired-branch guard — no lane can land this row's branch: "
)

// MergeBacklogGhostText is the message a closed ghost row carries: the refs
// probed, their exit statuses, and what the operator must do instead. One
// builder for every writer (Rule 14) — the summary marker, the reason column
// and the notify line are all this text.
func MergeBacklogGhostText(branch, repo, evidence string) string {
	return fmt.Sprintf(
		"refusal — the row names branch %q but NO ref for it resolves in %q: neither %s%s, nor %s%s, nor the plain name. "+
			"The commit the row was filed for is unreachable (a fixture/probe cleanup deleted the ref, a sweep pruned it, or the name is a typo). "+
			"Nothing is checked out and no session is spent: re-file with the REAL branch name, or close the row as abandoned. evidence: %s",
		branch, repo,
		refsHeadsPrefix, branch, refsRemotesPrefix, branch, evidence)
}

// mergeBacklogGhostRefJudge judges ONE row's branch-ref existence — the
// read-only entry an operator (or PA) uses to ask "what would the guard say
// about this row?" before a round. Same judgement the claim path runs, so the
// answer and the enforcement cannot drift.
func mergeBacklogGhostRefJudge(taskID string) (note string, refuse bool, retired bool) {
	taskID = taskIDNormalize(taskID)
	if taskID == "" {
		return "", false, false
	}
	ctx := context.Background()
	db, err := openTasksDB(ctx, memoryDBPath())
	if err != nil {
		return "", false, false
	}
	defer db.Close()
	t, err := taskByID(ctx, db, taskID)
	if err != nil || t == nil {
		return "", false, false
	}
	return mergeBacklogGhostRefusal(t)
}

// MergeBacklogGhostClaimNote renders the guard's claim-time line for ONE row
// (the read-only face above, as a printable sentence) — "" when the row is not
// governed or the answer is not a refusal.
func MergeBacklogGhostClaimNote(taskID string) string {
	note, refuse, retired := mergeBacklogGhostRefJudge(taskID)
	if !refuse {
		return ""
	}
	return fmt.Sprintf("%s %s: %s", mergeBacklogGhostArmMarker(retired), taskIDNormalize(taskID), note)
}

// mergeBacklogGhostArmMarker names which arm judged the row: a lost ref and a
// recorded decision are different facts and carry different markers, so a reader
// of `task list` (or of the notify stream) never has to open the row to tell
// which one fired (Rule 15).
func mergeBacklogGhostArmMarker(retired bool) string {
	if retired {
		return MergeBacklogRetiredMarker
	}
	return MergeBacklogGhostMarker
}

// mergeBacklogGhostArmReasonPrefix is the same pairing for the `reason` column,
// kept beside the marker so the two cannot drift.
func mergeBacklogGhostArmReasonPrefix(retired bool) string {
	if retired {
		return MergeBacklogRetiredReasonPrefix
	}
	return MergeBacklogGhostReasonPrefix
}

// mergeBacklogGhostArmName is the arm's name as a log line reads it.
func mergeBacklogGhostArmName(retired bool) string {
	if retired {
		return "retired-branch guard"
	}
	return "ghost-ref guard"
}

// mergeBacklogGhostArmEvent is the trail event name for the arm that fired
// (task_events). It is NOT used for the notify call: that one keeps its
// constant LITERAL at the call site, because the alarm registry derives its
// required set by resolving the argument syntactically and a helper call is
// out of its reach (tasknotify_alarms_test.go — a call it cannot resolve is
// skipped, which would silently drop the alarm from GOPI_TASK_NOTIFY_EVENTS=all).
func mergeBacklogGhostArmEvent(retired bool) string {
	if retired {
		return mergeBacklogRetiredEvent
	}
	return mergeBacklogGhostEvent
}

// mergeBacklogGhostRefusal decides whether ONE row is a ghost-branch row, and
// returns the note to record. Three-valued by construction: it refuses only on
// a MEASURED absence.
func mergeBacklogGhostRefusal(t *Task) (note string, refuse bool, retired bool) {
	if t == nil || t.Status != taskStatusPending {
		return "", false, false
	}
	branch, isFamily := mergeBacklogFamilyBranch(t.Summary, t.Tags)
	if !isFamily || branch == "" {
		return "", false, false
	}
	// The deliverable's own branch wins when stamped (the executor's
	// authoritative record); the summary's branch is the fallback.
	if b := mergeBacklogGuardBranch(t); b != "" {
		branch = b
	}
	repo := mergeBacklogGuardRepo(t)
	if repo == "" {
		// No repo to question (the row was minted outside a work tree): the
		// premise is UNMEASURED — never a refusal (Rule 15).
		return "", false, false
	}
	// THE RETIREMENT ARM, asked FIRST and before any ref-existence probe. A
	// branch can be a decided exclusion and STILL be present (the retirement
	// preserves the ref) or absent (a sweep pruned it) — both shapes reach the
	// same verdict, and asking the decision first is what makes the refusal NAME
	// the decision instead of reporting a missing ref (measured defect, task
	// 01a1446e's bug row: a row whose branch carried
	// refs/tags/archive/task/probe-zombie + its `unrelated-history` reason record
	// was handed over cross-host and claimed, spending a worker session on a
	// premise git had already refused). The adjudication is the ONE predicate
	// (ArchivedBranchExclusion) over the row's OWN branch, resolved through the
	// family's shared resolver (mergeBacklogGuardBranch) — the same reader the
	// rest of this file and the terminal guard use (Rule 14).
	if rec, retired := RowBranchRetirement(repo, t.Summary, t.Tags, t.Deliverable); retired {
		return ArchivedBranchExclusionText(rec), true, true
	}
	// Existence, per spelling, through this package's ONE git probe reader
	// (mergeVerifyStaleGit + its two NEGATIVE exit constants: exit 1 with empty
	// output = a MEASURED absence, any other non-zero = git could not answer).
	unmeasured := false
	var evidence []string
	for _, ref := range []string{refsHeadsPrefix + branch, refsRemotesPrefix + branch, branch} {
		tip, measured, ev := mergeBacklogGhostResolveRef(repo, ref)
		evidence = append(evidence, ev)
		if !measured {
			unmeasured = true
			continue
		}
		if tip != "" {
			// The ref resolves: this row is ordinary work, whatever its
			// merge posture. The ancestry question belongs to the D2 terminal
			// guard, not here.
			return "", false, false
		}
	}
	if unmeasured {
		return "", false, false
	}
	// Every spelling measured absent. Before refusing, ask the D2 probe
	// whether the work SHIPPED anyway (the merger deletes a branch exactly
	// when its merge succeeded, so "ref gone" is the normal post-merge state):
	// a MERGED posture is a delivery, never a ghost — the row is left for the
	// executor / terminal guard to close properly.
	//
	// An UNMERGED posture is NOT a flat refusal either: a branch whose ref is
	// gone but whose archive tag still resolves is a recorded loser-archive
	// decision (Rule 9), and that row's disposition belongs to the archive
	// path, not here. Only a posture that resolves NOTHING — or one whose only
	// surviving evidence is the archived remainder — reaches the refusal.
	state, why := mergeBacklogProbeMergeState(repo, mergeBacklogTerminalTarget{
		Branch: branch,
		Repo:   repo,
		Tip:    mergeBacklogGuardTip(t),
	})
	evidence = append(evidence, why)
	if state == mergeBacklogMergeMerged {
		return "", false, false
	}
	if state == mergeBacklogMergeUnmerged && len(mergeBacklogArchiveRefsFor(repo, branch)) > 0 {
		// The branch is archived: the decision is already recorded elsewhere.
		return "", false, false
	}
	return MergeBacklogGhostText(branch, repo, joinEvidence(evidence)), true, false
}

// mergeBacklogGhostResolveRef resolves ONE ref spelling through this package's
// shared git probe reader (mergeVerifyStaleGit + its two NEGATIVE exit
// constants — the SAME reader the merge-verify staleness guard uses, so
// "measured absence" means one thing everywhere) and reports the three-valued
// answer:
//
//	exit 0 + a sha   → the ref resolves (measured, present)
//	exit 1 + nothing → a MEASURED absence (git's documented --quiet contract)
//	anything else    → git could not answer (measured=false, never "gone")
func mergeBacklogGhostResolveRef(repo, ref string) (tip string, measured bool, evidence string) {
	out, exit, detail := mergeVerifyStaleGit(repo, "rev-parse", "--verify", "--quiet", ref)
	evidence = fmt.Sprintf("git rev-parse --verify --quiet %s ⇒ exit=%d %s", ref, exit, detail)
	switch {
	case exit == 0:
		return strings.TrimSpace(strings.SplitN(strings.TrimSpace(out), "\n", 2)[0]), true, evidence
	case exit == mergeVerifyGitExitNo:
		return "", true, evidence
	default:
		return "", false, evidence
	}
}

// mergeBacklogVerbPre is the fixed prefix of the audit's summary verb
// ("merge %s into master") — split from its single source (Rule 14), never a
// retyped literal.
func mergeBacklogVerbPre() string {
	pre, _, ok := mergeBacklogVerbHalves()
	if !ok {
		return ""
	}
	return pre
}

// joinEvidence renders the probe trail (bounded: the evidence rides the row's
// reason column, which an operator reads in `task list`).
func joinEvidence(parts []string) string {
	out := ""
	for _, p := range parts {
		if p == "" {
			continue
		}
		if out != "" {
			out += " | "
		}
		out += p
	}
	if len(out) > mergeVerifyStaleEvidenceTail*4 {
		out = out[:mergeVerifyStaleEvidenceTail*4]
	}
	return out
}

// mergeBacklogGhostSweepPending closes the ghost-branch merge rows that are
// sitting in the pending queue, returning the refusal lines for the claim that
// triggered the sweep. It is the entry the dispatcher calls at claim time
// (worker round / serve dispatch / `task run next` all funnel through
// TaskNextFiltered), exactly like mergeVerifyStaleSweepPending.
func mergeBacklogGhostSweepPending(db *sql.DB) []string {
	if db == nil {
		return nil
	}
	ctx := context.Background()
	// SQL narrowing hint only (mergeBacklogFamilyBranch is the authority): the
	// audit verb's rows start with "merge " and every family row carries the
	// tag, so the scan touches the lane's rows and nothing else.
	rows, err := db.QueryContext(ctx,
		`SELECT `+taskSelectColumns+` FROM tasks `+
			`WHERE status = ? AND (summary LIKE ? OR tags LIKE ?) ORDER BY priority ASC, created_at ASC, id ASC LIMIT ?`,
		taskStatusPending, mergeBacklogVerbPre()+"%", "%"+mergeBacklogTag+"%", mergeBacklogGhostSweepCap)
	if err != nil {
		taskNotifyLog("merge-backlog ghost-ref sweep: query: %v", err)
		return nil
	}
	var candidates []*Task
	for rows.Next() {
		c, err := scanTask(rows)
		if err != nil {
			rows.Close() //stylegate:ignore read-only cursor at end of iteration; the error carries no state to recover
			taskNotifyLog("merge-backlog ghost-ref sweep: scan: %v", err)
			return nil
		}
		candidates = append(candidates, c)
	}
	if err := rows.Err(); err != nil {
		rows.Close() //stylegate:ignore read-only cursor at end of iteration; the error carries no state to recover
		taskNotifyLog("merge-backlog ghost-ref sweep: rows: %v", err)
		return nil
	}
	rows.Close() //stylegate:ignore read-only cursor at end of iteration; the error carries no state to recover

	var closed []string
	for _, c := range candidates {
		note, refuse, retired := mergeBacklogGhostRefusal(c)
		if !refuse {
			continue
		}
		if ok, reason := mergeBacklogGhostCloseRow(c, note, retired); ok {
			closed = append(closed, reason)
		}
	}
	return closed
}

// mergeBacklogGhostCloseRow closes a ghost row: pending → claimed → cancelled
// through the SAME exported transitions every other closer uses, so the claim
// trail records WHO closed it and no worker ever ran it. The terminal write
// goes through taskUpdate (the shared mutation core) rather than
// TaskUpdateWithResult, because the REASON column is this close's whole
// operator-facing content (Rule 15 — a close whose reason does not say WHY is
// a silent close).
//
// cancelled, never failed: the row is not a failed attempt at anything — its
// premise was never true. A `failed` row would read as "we tried and could not"
// and invite a retry of the same ghost.
func mergeBacklogGhostCloseRow(t *Task, note string, retired bool) (bool, string) {
	if err := TaskClaimExplicit(t.TaskID, mergeBacklogGhostGuardHolder); err != nil {
		taskNotifyLog("merge-backlog ghost-ref guard: %s claim step failed (row stays claimable): %v", t.TaskID, err)
		return false, ""
	}
	marker := mergeBacklogGhostArmMarker(retired)
	reason := marker + " " + mergeBacklogGhostArmReasonPrefix(retired) + note
	if _, err := taskUpdate(t.TaskID, taskStatusCancelled, marker+" "+note, reason); err != nil {
		taskNotifyLog("merge-backlog ghost-ref guard: %s cancelled write failed (row stays claimed): %v", t.TaskID, err)
		return false, ""
	}
	// The judgement rides the ROW'S OWN trail, not only the process log: an
	// operator reading `gopi task show <id>` (or grepping task_events) must be
	// able to see who closed the row and on what evidence. Best-effort by
	// design (Rule 15b: a ledger annotation failure is logged, never allowed
	// to overturn the close the ledger already accepted).
	ghostRecordEvent(t.TaskID, note, retired)
	if retired {
		NotifySyntheticEvent(mergeBacklogRetiredEvent, "", "", reason)
	} else {
		NotifySyntheticEvent(mergeBacklogGhostEvent, "", "", reason)
	}
	taskNotifyLog("%s %s: %s refused (NOT dispatched) — %s", marker, t.TaskID, mergeBacklogGhostArmName(retired), note)
	fmt.Fprintf(os.Stderr, "%s %s: %s\n", marker, t.TaskID, note)
	return true, reason
}

// ghostRecordEvent appends the judgement to the CLOSED row's event trail
// (append-only; the row's status was already moved). Same shape as
// MergeVerifyRecordEvent — best-effort, because the close has already been
// accepted by the ledger and an annotation failure must not overturn it.
func ghostRecordEvent(taskID, detail string, retired bool) {
	taskID = taskIDNormalize(taskID)
	if taskID == "" {
		return
	}
	ctx := context.Background()
	db, err := openTasksDB(ctx, memoryDBPath())
	if err != nil {
		taskNotifyLog("merge-backlog ghost-ref guard: event for %s: open ledger: %v", taskID, err)
		return
	}
	defer db.Close()
	taskEventWrite(ctx, db, taskID, mergeBacklogGhostArmEvent(retired), detail)
}

// MergeBacklogGhostSweepSummary is the audit line the sweep returns to its
// caller (the CLI prints it; a test asserts on it). One definition, so the
// shape cannot drift between the sweep and its readers.
func MergeBacklogGhostSweepSummary(reasons []string) string {
	if len(reasons) == 0 {
		return ""
	}
	out := fmt.Sprintf("%s closed %d pending merge row(s) whose branch cannot be dispatched:",
		MergeBacklogRetiredMarker+" / "+MergeBacklogGhostMarker, len(reasons))
	for _, r := range reasons {
		out += "\n  - " + r
	}
	return out
}
