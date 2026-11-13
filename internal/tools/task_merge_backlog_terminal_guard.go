package tools

// D2 — the merge-backlog TERMINAL guard.
//
// THE DEFECT (ledger audit 2026-09-17, devdoc/merge-backlog-terminal-guard.md).
// A "merge-backlog: <branch> unmerged — merge/archive decision needed" row
// reached `verified` (t-1788218295) while its branch was provably NOT merged
// (`git rev-list --count master..archive/fix-memsync-import-atomic-20260901`
// == 5, a 482-insertion atomicity fix). The status vocabulary
// (pending/claimed/completed/verified) has no word for "still unmerged", and
// NO write site asked git whether the branch landed — so a terminal row could
// read as "the decision was made / the merge is fine" while the work stayed in
// the branches. The re-arming half of the same family was already fixed
// (7162092, dedupe on the task identity); this is the surviving half: the
// STATUS is the lie.
//
// ROOT CAUSE (5-why, file:line):
//   why 1  the row is `verified` while the branch is unmerged — the status
//          was written without consulting git.
//   why 2  the write is legal: `taskAllowedTransitions` only asks whether
//          cancelled|completed|failed → verified, and nothing on that path
//          looks at a ref (internal/tools/tasks.go:2825).
//   why 3  the write that produced THIS row did not even walk that table:
//          `syncTaskMigrateFrom` (internal/tools/memory_sync_task.go:1120 at
//          the time of this audit — master's cf314174 renamed it
//          `syncTaskMoveStatusTx` and moved it inside the import transaction
//          as (ctx, *sql.Tx); the defect is the raw `UPDATE tasks SET status
//          = ?` itself, not its name) issues a raw `UPDATE tasks SET status
//          = ?` — the "completed wins"
//          ruling (memory_sync_task.go:1009 live-row arm, :1027
//          cancelled-vs-terminal arm) uses it to forward-migrate a ROW, and a
//          row-level truth is not a work-level truth. t-1788218295's empty
//          task_events trail plus its sync-rewritten tags
//          (merge-backlog,yield-dead,w423-shield) are the fingerprint of that
//          path: it was cancelled, then a terminal patch row collided with it.
//   why 4  the local door has the same hole: `taskUpdateStampedFrom`
//          (internal/tools/tasks.go:3361) is the one mutation core, and its
//          only git-shaped gate is `reclaimRunResidueRefusal` — about the
//          run's WORKTREE, never about the branch's ancestry.
//   why 5  the branch-scan mints a row whose whole PREMISE is "this branch is
//          unmerged" (cmd/agent/cli_tasks.go agentBranchScan → the summary
//          verb), and then nothing re-checks that premise at the moment the
//          row is closed. The premise can therefore be FALSE at close time
//          with no writer noticing.
//
// THE GUARD (this file), wired into the one local mutation core and recorded
// on the sync door:
//
//   a merge-backlog FAMILY row may reach a success terminal
//   (completed|verified) only when the branch it names is PROVABLY MERGED
//   (`git rev-list --count master..<ref>` == 0, on the branch ref, a
//   recorded tip, or the branch's archive tag) — or when the row records an
//   EXPLICIT ARCHIVE DECISION with a reason (the Rule 9 loser-archive
//   convention, named in the reason).
//
// Everything else is REFUSED loudly at the local door (Rule 15: the row stays
// open and carries the reason the write was refused) and RECORDED on the sync
// door (sync may not refuse — refusing would break bilateral convergence —
// but it may not be silent either). Verification that is IMPOSSIBLE (no repo,
// no ref, no tip) is recorded, never guessed into a refusal: a guard that
// refuses what it cannot see would wedge the merge loop it is protecting.

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
)

// merge-backlog terminal-guard constants (Rule 1/1b: every literal named
// once; the summary verbs and the tag come from their single sources).
const (
	// mergeBacklogArchiveDecisionVerb is the word an EXPLICIT archive
	// decision must record in the row's reason. A success terminal on a
	// provably unmerged branch is legitimate only as an archive (Rule 9:
	// tag archive/<branch> + delete), so the reason must say so.
	mergeBacklogArchiveDecisionVerb = "archive"

	// mergeBacklogCherryAbsentMark WAS the `git cherry` line prefix marking a
	// commit whose patch is ABSENT upstream ("+"). The guard no longer parses
	// `git cherry` itself — it asks the one content-fingerprint seam
	// (DeliveryContentLanded, delivery_content_landed.go) — so the mark now
	// lives where it is read, in that seam's own vocabulary
	// (deliveryProofCherryAbsentMark).

	// mergeBacklogTerminalRefusedEvent is the append-only trail entry for a
	// REFUSED terminal write (the local door): an operator sees why the row
	// is still open without re-deriving it.
	mergeBacklogTerminalRefusedEvent = "merge-backlog-terminal-refused"

	// mergeBacklogTerminalUnverifiedEvent is the trail entry for a write
	// that could not be VERIFIED (no repo / no ref / no tip). It is the
	// loud half of "no silent failure": the row closed unproven, and that
	// fact is on its trail rather than in nobody's memory.
	mergeBacklogTerminalUnverifiedEvent = "merge-backlog-terminal-unverified"
)

// mergeBacklogMergeState is the three-valued answer of the ancestry probe.
// Three values, not two, on purpose: "cannot tell" must never be collapsed
// into "unmerged" (that refusal would wedge every row whose repo is not
// reachable) nor into "merged" (that silence is the defect).
type mergeBacklogMergeState int

const (
	// mergeBacklogMergeUnknown: no ref, no recorded tip, no archive tag —
	// the merge posture of the branch cannot be established from here.
	mergeBacklogMergeUnknown mergeBacklogMergeState = iota
	// mergeBacklogMergeMerged: the branch's work is in master (ahead == 0).
	mergeBacklogMergeMerged
	// mergeBacklogMergeUnmerged: the branch's work is NOT in master
	// (ahead > 0) — the D2 state.
	mergeBacklogMergeUnmerged
)

// mergeBacklogSuccessTerminal reports whether s is a success terminal — the
// statuses this guard governs (an abandoned row, cancelled/failed, is a
// legitimate way to say "not merged" and is left alone).
func mergeBacklogSuccessTerminal(s string) bool {
	return s == taskStatusCompleted || s == taskStatusVerified
}

// mergeBacklogTerminalTarget is one row's guard inputs, resolved from the
// ledger: the branch it names, the repo to ask, and the tip it recorded.
type mergeBacklogTerminalTarget struct {
	// Branch is the branch the summary names ("" = the row is not a
	// merge-backlog family row, or names no branch).
	Branch string
	// Repo is where the ancestry question is asked (deliverable.repo, else
	// the process' own work tree).
	Repo string
	// Tip is the tip the row recorded (payload "branch tip: <sha>"), when
	// the row carries one.
	Tip string
	// Reason is the reason recorded on the terminal write.
	Reason string
}

// mergeBacklogFamilyBranch extracts the branch a merge-backlog family row
// names, plus whether the row belongs to the family at all. Both faces are
// recognized through their SINGLE-SOURCE verbs (Rule 13: no mirrored
// literal — the scan verb and the audit verb are the constants the
// submitters build with):
//
//	scan face  "merge-backlog: <branch> unmerged — merge/archive …"
//	audit face "merge <branch> into master"        (mergeBacklogSummaryVerb)
//
// The summary is the durable carrier: the cross-node delta union REPLACES
// tags wholesale (574/617 legacy rows lost the merge-backlog tag while
// keeping the summary), so a tags-only classifier would go blind on exactly
// the rows this guard exists for. Tags are still consulted as a second face
// (a row whose summary was somehow rewritten but which carries the tag).
func mergeBacklogFamilyBranch(summary, tags string) (string, bool) {
	summary = strings.TrimSpace(summary)
	tags = strings.TrimSpace(tags)
	scanPrefix := mergeBacklogTag + ": "
	if rest := strings.TrimPrefix(summary, scanPrefix); rest != summary {
		if fields := strings.Fields(rest); len(fields) > 0 {
			return fields[0], true
		}
	}
	// The audit verb is a single-source format string ("merge %s into
	// master"); split it rather than re-typing either half.
	if pre, post, ok := mergeBacklogVerbHalves(); ok {
		if strings.HasPrefix(summary, pre) && strings.HasSuffix(summary, post) {
			branch := strings.TrimSuffix(strings.TrimPrefix(summary, pre), post)
			if branch = strings.TrimSpace(branch); branch != "" && !strings.Contains(branch, " ") {
				return branch, true
			}
		}
	}
	if tags != "" {
		for _, tag := range strings.Split(tags, ",") {
			if strings.TrimSpace(tag) == mergeBacklogTag {
				return branchFromAnySummaryVerb(summary), true
			}
		}
	}
	return "", false
}

// mergeBacklogVerbHalves splits the audit summary verb into its fixed
// prefix/suffix around the branch placeholder — one source (Rule 14), never
// two typed literals.
func mergeBacklogVerbHalves() (pre, post string, ok bool) {
	if !strings.Contains(mergeBacklogSummaryVerb, "%s") {
		return "", "", false
	}
	halves := strings.SplitN(mergeBacklogSummaryVerb, "%s", 2)
	return halves[0], halves[1], true
}

// branchFromAnySummaryVerb is the tag-face fallback: a row that carries the
// merge-backlog tag but whose summary matches neither verb shape names no
// branch we can question — the guard then treats it as unverifiable (loud),
// never as a violation.
func branchFromAnySummaryVerb(summary string) string {
	scanPrefix := mergeBacklogTag + ": "
	if rest := strings.TrimPrefix(summary, scanPrefix); rest != summary {
		if fields := strings.Fields(rest); len(fields) > 0 {
			return fields[0]
		}
	}
	return ""
}

// mergeBacklogGuardBranch resolves the branch a row names from the
// deliverable first (the executor's authoritative record when present), and
// the summary otherwise (the branch-scan face never stamps a deliverable.branch).
func mergeBacklogGuardBranch(row *Task) string {
	if branch := taskDeliverableParse(row.Deliverable).Branch; strings.TrimSpace(branch) != "" {
		return strings.TrimSpace(branch)
	}
	branch, _ := mergeBacklogFamilyBranch(row.Summary, row.Tags)
	return branch
}

// mergeBacklogGuardRepo resolves the repo the ancestry question is asked in:
// the row's recorded deliverable.repo, else the process' own work tree (the
// same default the submit engine resolves — taskSubmitResolveDefaultRepo —
// so a scan row minted without a repo is still verifiable at the door it is
// closed through).
func mergeBacklogGuardRepo(row *Task) string {
	if repo := taskDeliverableParse(row.Deliverable).Repo; strings.TrimSpace(repo) != "" {
		return strings.TrimSpace(repo)
	}
	return taskSubmitResolveDefaultRepo()
}

// mergeBacklogGuardTip reads the tip a row recorded (the audit face's payload
// line, mergeBacklogTipLine) — the fallback proof when the branch ref is
// already gone: the merger deletes a merged branch BEFORE writing the row
// completed (internal/tools/task_merge_executor.go deletes, then writes back).
//
// The scan face records no tip (its payload names the branch, not a commit),
// so a scan row whose branch ref was swept without an archive tag is
// UNVERIFIABLE, not unmerged — the guard records that instead of guessing
// (Rule 15), see mergeBacklogTerminalUnverifiedEvent.
func mergeBacklogGuardTip(row *Task) string {
	prefix := fmt.Sprintf(mergeBacklogTipLine, "")
	for _, line := range strings.Split(row.Payload, "\n") {
		line = strings.TrimSpace(line)
		if rest := strings.TrimPrefix(line, prefix); rest != line {
			if fields := strings.Fields(rest); len(fields) > 0 {
				return fields[0]
			}
		}
	}
	return ""
}

// mergeBacklogPatchSuperseded reports whether the WORK of ref is already on
// master, i.e. whether the ref holds no patch master lacks.
//
// IT ASKS THE ONE SEAM (delivery_content_landed.go, DeliveryContentLanded)
// rather than a private `git cherry` of its own. Why that had to change: this
// repository lands work by cherry-pick / rebase, so a sha comparison answers
// about identity while the question is about work — four faces of this machinery
// each built their own weaker answer and each was wrong in its own way. The seam
// contributes what a bare `git cherry` could not: the ANCESTRY fast path, the
// patch-id leg, AND the line-content leg for the rebase that re-derived the
// content (a regenerated table measured against a moved base), where no patch id
// can ever match.
//
// THE DIRECTION OF THE THREE-VALUED ANSWER IS THE SAFETY ARGUMENT: `judged=false`
// (git could not answer, an unreadable object, a range past the bounded walk)
// answers NOT-superseded, which leaves the caller's UNMERGED posture standing —
// the safe side (a refusal an operator can review), never a silent pass.
func mergeBacklogPatchSuperseded(repo, ref string) (bool, string) {
	ctx, cancel := context.WithTimeout(context.Background(), reclaimGitTimeout)
	defer cancel()
	landed, why, judged := DeliveryContentLanded(ctx, repo, repo, mergeBacklogMergeTarget, ref)
	if !judged || !landed {
		return false, ""
	}
	return true, fmt.Sprintf("the work of %s is already on %s (%s)", ref, mergeBacklogMergeTarget, why)
}

// mergeBacklogAncestryAhead runs `git rev-list --count master..<ref>` and
// returns the count. An unresolvable ref is an ERROR (the caller decides
// what "cannot tell" means) — never silently 0, which would read as merged.
func mergeBacklogAncestryAhead(repo, ref string) (int, error) {
	out, err := mergeBacklogGit(repo, "rev-list", "--count", mergeBacklogMergeTarget+".."+ref)
	if err != nil {
		return 0, err
	}
	n := 0
	for _, f := range strings.Fields(out) {
		fmt.Sscanf(f, "%d", &n)
	}
	return n, nil
}

// mergeBacklogRefResolvable reports whether ref exists in repo (exit 0 from
// `git rev-parse --verify --quiet`). A missing ref is a legitimate answer
// (the branch was archived/deleted), not a failure.
func mergeBacklogRefResolvable(repo, ref string) bool {
	_, err := mergeBacklogGit(repo, "rev-parse", "--verify", "--quiet", ref)
	return err == nil
}

// mergeBacklogArchiveRefsFor returns the archive tags whose name names the
// branch (possibly more than one: a re-tag, or the host-prefixed and local
// faces of the same branch). The loser-archive convention (Rule 9) tags
// refs/tags/archive/<branch>-<date>, so the tag is the surviving record of a
// branch whose ref is gone — and its ahead count answers the ancestry
// question the deleted branch no longer can (this is exactly how the D2 row's
// branch was proven unmerged: ahead(archive/fix-memsync-import-atomic-
// 20260901) == 5).
//
// The branch name cannot be taken at face value: a row submitted from a
// REMOTE scan names the remote-tracking branch ("hp/fix/memsync-import-
// atomic"), while the tag was made from the local branch name
// ("archive/fix-memsync-import-atomic-20260901"). So the search walks the
// name's successive tails — the full name, then with the leading segment
// (the host prefix) removed, down to the base name — in both the
// slash-preserving and the slash-flattened spelling the convention uses.
func mergeBacklogArchiveRefsFor(repo, branch string) []string {
	tags, err := mergeBacklogGit(repo, "for-each-ref", "--format=%(refname:short)", archiveTagRefPrefix)
	if err != nil || strings.TrimSpace(tags) == "" {
		return nil
	}
	available := strings.Fields(tags)
	seen := map[string]bool{}
	var found []string
	add := func(tag string) {
		if tag != "" && !seen[tag] {
			seen[tag] = true
			found = append(found, tag)
		}
	}
	// Every tail of the branch name (full name first, then progressively
	// shorter — the host prefix is dropped one segment at a time).
	for _, tail := range mergeBacklogBranchTails(branch) {
		flat := strings.ReplaceAll(tail, "/", "-")
		for _, stem := range []string{archiveBranchPrefix + tail, archiveBranchPrefix + flat} {
			for _, tag := range available {
				if tag == stem || strings.HasPrefix(tag, stem+"-") {
					add(tag)
				}
			}
		}
	}
	return found
}

// mergeBacklogBranchTails returns the branch name and each of its tails after
// dropping leading path segments: "hp/fix/x" → ["hp/fix/x", "fix/x", "x"].
// The base name is deliberately included last (it is the loosest match, and
// the caller treats every match as evidence of the same branch's work).
func mergeBacklogBranchTails(branch string) []string {
	branch = strings.Trim(strings.TrimSpace(branch), "/")
	if branch == "" {
		return nil
	}
	parts := strings.Split(branch, "/")
	tails := make([]string, 0, len(parts))
	for i := range parts {
		if tail := strings.Join(parts[i:], "/"); tail != "" {
			tails = append(tails, tail)
		}
	}
	return tails
}

// mergeBacklogProbeMergeState establishes the branch's merge posture from
// the strongest available evidence, in this order:
//
//  1. the branch ref itself (local, then remote-tracking, then plain) —
//     `rev-list --count master..<ref>`: 0 merged, >0 unmerged;
//  2. the tip the row recorded — `merge-base --is-ancestor <tip> master`
//     (the merger deletes the branch before its own completed writeback, so
//     this arm is what keeps a LEGITIMATE merge writable);
//  3. the branch's archive tag — its ahead count answers for the deleted
//     branch (an archived branch whose tag is still ahead of master is
//     provably unmerged work);
//  4. nothing resolvable → Unknown (recorded loudly, never guessed).
func mergeBacklogProbeMergeState(repo string, target mergeBacklogTerminalTarget) (mergeBacklogMergeState, string) {
	if strings.TrimSpace(repo) == "" || strings.TrimSpace(target.Branch) == "" {
		return mergeBacklogMergeUnknown, "the row records no repo/branch to question"
	}
	refs := []string{
		refsHeadsPrefix + target.Branch,
		refsRemotesPrefix + target.Branch,
		target.Branch,
	}
	for _, ref := range refs {
		if !mergeBacklogRefResolvable(repo, ref) {
			continue
		}
		ahead, err := mergeBacklogAncestryAhead(repo, ref)
		if err != nil {
			continue
		}
		if ahead == 0 {
			return mergeBacklogMergeMerged, fmt.Sprintf("%s is an ancestor of %s (rev-list --count %s..%s == 0)",
				ref, mergeBacklogMergeTarget, mergeBacklogMergeTarget, ref)
		}
		// Ahead by commit identity, but the patches may already be on
		// master (cherry-pick / rebase adoption) — the work landed, only
		// the commits differ. That is a MERGED posture, not the D2 one.
		if superseded, why := mergeBacklogPatchSuperseded(repo, ref); superseded {
			return mergeBacklogMergeMerged, why
		}
		return mergeBacklogMergeUnmerged, fmt.Sprintf("%s is NOT merged (%s ahead of %s: rev-list --count %s..%s == %d)",
			ref, plural(ahead), mergeBacklogMergeTarget, mergeBacklogMergeTarget, ref, ahead)
	}
	if tip := strings.TrimSpace(target.Tip); tip != "" {
		if _, err := mergeBacklogGit(repo, "merge-base", "--is-ancestor", tip, mergeBacklogMergeTarget); err == nil {
			return mergeBacklogMergeMerged, fmt.Sprintf("recorded tip %s is an ancestor of %s (the branch ref is already deleted)",
				tip, mergeBacklogMergeTarget)
		}
		return mergeBacklogMergeUnmerged, fmt.Sprintf("recorded tip %s is NOT an ancestor of %s and the branch ref %s no longer exists",
			tip, mergeBacklogMergeTarget, target.Branch)
	}
	if tags := mergeBacklogArchiveRefsFor(repo, target.Branch); len(tags) > 0 {
		// The evidence is the STRONGEST (lowest ahead count) among the tags
		// naming this branch: a re-tag or a re-landed patch can leave more
		// than one archive tag, and this branch's work is in master iff the
		// most-advanced tail of it is.
		best, bestTag := -1, ""
		for _, t := range tags {
			ahead, err := mergeBacklogAncestryAhead(repo, t)
			if err != nil {
				continue
			}
			if best < 0 || ahead < best {
				best, bestTag = ahead, t
			}
		}
		if best == 0 {
			return mergeBacklogMergeMerged, fmt.Sprintf("archive tag %s is an ancestor of %s", bestTag, mergeBacklogMergeTarget)
		}
		if best > 0 {
			return mergeBacklogMergeUnmerged, fmt.Sprintf("archive tag %s keeps %d unmerged commit(s) (%s..%s) — the branch was archived, not merged",
				bestTag, best, mergeBacklogMergeTarget, bestTag)
		}
	}
	return mergeBacklogMergeUnknown, fmt.Sprintf("neither the branch ref %q, nor a recorded tip, nor an archive tag resolves in %s",
		target.Branch, repo)
}

// plural renders "1 commit"/"N commits" without a plural helper dependency.
func plural(n int) string {
	if n == 1 {
		return "1 commit"
	}
	return fmt.Sprintf("%d commits", n)
}

// mergeBacklogArchiveDecisionRecorded reports whether the row's reason
// carries an EXPLICIT archive decision (Rule 9's loser-archive convention,
// named in the reason). This is the one legitimate way to close a provably
// unmerged branch's row as a success: the decision was made, it was ARCHIVE,
// and the operator said so on the row.
func mergeBacklogArchiveDecisionRecorded(reason string) bool {
	return strings.Contains(strings.ToLower(strings.TrimSpace(reason)), mergeBacklogArchiveDecisionVerb)
}

// mergeBacklogTerminalVerdict is the guard's decision for one row/status
// pair: may the write proceed, and why.
type mergeBacklogTerminalVerdict struct {
	// Governed is false when the row is not a merge-backlog family row —
	// every other task close is untouched by this guard.
	Governed bool
	// Allow is the decision.
	Allow bool
	// Note is the human-readable reason (always non-empty when Governed).
	Note string
	// Unverified is true when the write is allowed but the merge posture
	// could NOT be established (the loud case, never a silent pass).
	Unverified bool
}

// mergeBacklogTerminalVerdictFor decides whether a merge-backlog family row
// may reach a success terminal, and says why. It is the single predicate both
// doors call (the local mutation core refuses on it; the sync door records
// it) so the rule cannot mean two different things on two paths.
func mergeBacklogTerminalVerdictFor(row *Task, status string) mergeBacklogTerminalVerdict {
	if row == nil || !mergeBacklogSuccessTerminal(status) {
		return mergeBacklogTerminalVerdict{Allow: true}
	}
	branch, family := mergeBacklogFamilyBranch(row.Summary, row.Tags)
	if !family {
		return mergeBacklogTerminalVerdict{Allow: true}
	}
	target := mergeBacklogTerminalTarget{
		Branch: mergeBacklogGuardBranch(row),
		Repo:   mergeBacklogGuardRepo(row),
		Tip:    mergeBacklogGuardTip(row),
		Reason: row.Reason,
	}
	if target.Branch == "" {
		target.Branch = branch
	}
	if strings.TrimSpace(target.Branch) == "" {
		return mergeBacklogTerminalVerdict{
			Governed: true, Allow: true, Unverified: true,
			Note: fmt.Sprintf("row %s names no branch (summary %q) — merge posture unverifiable", row.TaskID, row.Summary),
		}
	}
	state, evidence := mergeBacklogProbeMergeState(target.Repo, target)
	switch state {
	case mergeBacklogMergeMerged:
		return mergeBacklogTerminalVerdict{
			Governed: true, Allow: true,
			Note: fmt.Sprintf("branch %s is merged — %s", target.Branch, evidence),
		}
	case mergeBacklogMergeUnmerged:
		if mergeBacklogArchiveDecisionRecorded(target.Reason) {
			return mergeBacklogTerminalVerdict{
				Governed: true, Allow: true,
				Note: fmt.Sprintf("branch %s is unmerged but the row records an explicit archive decision — %s", target.Branch, evidence),
			}
		}
		return mergeBacklogTerminalVerdict{
			Governed: true, Allow: false,
			Note: fmt.Sprintf("branch %s is NOT merged and the row records no archive decision — %s. "+
				"A merge-backlog row may reach %s only when the branch is MERGED (rev-list --count %s..%s == 0) "+
				"or when the decision recorded is an ARCHIVE with a reason (e.g. `--reason \"archive %s: superseded by <sha>\"`). "+
				"To say 'still unmerged', leave the row OPEN — the status vocabulary has no word for it, and closing it "+
				"as %s reads as 'the merge is fine' while the work stays in the branch (D2: t-1788218295).",
				target.Branch, evidence, status, mergeBacklogMergeTarget, target.Branch, target.Branch, status),
		}
	default:
		return mergeBacklogTerminalVerdict{
			Governed: true, Allow: true, Unverified: true,
			Note: fmt.Sprintf("branch %s: merge posture could not be verified — %s", target.Branch, evidence),
		}
	}
}

// mergeBacklogTerminalRefusal is the LOCAL DOOR's face of the guard: when the
// verdict refuses, the refusal is recorded on the row's append-only trail
// (so the still-open row explains itself) and returned to the caller, which
// refuses the write loudly (Rule 15). A non-governed row, or one that is
// merely unverifiable, returns refused=false.
//
// mergeBacklogTerminalRefusalOn is the uniform RULE half of both doors: the
// caller hands in the handle it already owns — its *sql.Tx while inside a
// write transaction, an *sql.DB when it holds one — so the guard's row read
// and trail write ride the SAME connection the caller is using. That matters
// twice over: a separate connection would block against the caller's open
// transaction (measured: the trail INSERT then fails with SQLITE_BUSY and the
// refusal is never recorded — the exact silence this guard exists to
// prevent), and a trail entry written outside the transaction would survive a
// rolled-back import, announcing a decision that never actually landed.
//
// The caller decides the POLICY half (refuse the write, or let it through and
// record it): this function only judges and writes the trail.
func mergeBacklogTerminalRefusalOn(ctx context.Context, q QueryExecer, taskID, status, reason string) (string, bool) {
	taskID = taskIDNormalize(taskID)
	if taskID == "" || !mergeBacklogSuccessTerminal(status) {
		return "", false
	}
	row, err := taskByIDOn(ctx, q, taskID)
	if err != nil {
		taskNotifyLog("merge-backlog terminal guard: read %s: %v", taskID, err)
		return "", false
	}
	// The reason being written RIGHT NOW is the one the decision must name:
	// an explicit archive decision supplied on this very write has not
	// reached the row yet.
	if row != nil && strings.TrimSpace(reason) != "" {
		row.Reason = reason
	}
	verdict := mergeBacklogTerminalVerdictFor(row, status)
	if !verdict.Governed {
		return "", false
	}
	if verdict.Allow && !verdict.Unverified {
		return "", false
	}
	event := mergeBacklogTerminalUnverifiedEvent
	if !verdict.Allow {
		event = mergeBacklogTerminalRefusedEvent
	}
	if taskEventCountForDetailOn(ctx, q, taskID, event, verdict.Note) < reclaimRunResidueRefusalLimit {
		taskEventWrite(ctx, q, taskID, event, verdict.Note)
	}
	if !verdict.Allow {
		taskNotifyLog("merge-backlog terminal guard: REFUSED %s → %s — %s", taskID, status, verdict.Note)
	}
	return verdict.Note, !verdict.Allow
}

// mergeBacklogTerminalRecord is the SYNC DOOR's face: the sync layer adopts
// distributed truth and must not refuse (a refusal would break bilateral
// convergence), but it must not be SILENT either — this is the door that
// actually produced t-1788218295 (the raw status UPDATE). A governed write
// that is refused-by-rule or unverifiable is recorded on the trail; the write
// itself proceeds.
//
// It runs INSIDE the import's write transaction (tx), for two reasons: a
// second connection cannot read or write the ledger while that transaction is
// open — the guard would silently lose its own trail entry to SQLITE_BUSY,
// which is the silence it exists to prevent — and a trail entry written
// outside the transaction would announce a decision that a rolled-back patch
// never actually adopted.
func mergeBacklogTerminalRecord(ctx context.Context, tx *sql.Tx, taskID, status string) {
	taskID = taskIDNormalize(taskID)
	if taskID == "" || !mergeBacklogSuccessTerminal(status) {
		return
	}
	row, err := taskByIDOn(ctx, tx, taskID)
	if err != nil {
		taskNotifyLog("merge-backlog terminal guard (sync): read %s: %v", taskID, err)
		return
	}
	verdict := mergeBacklogTerminalVerdictFor(row, status)
	if !verdict.Governed || (verdict.Allow && !verdict.Unverified) {
		return
	}
	event := mergeBacklogTerminalUnverifiedEvent
	if !verdict.Allow {
		event = mergeBacklogTerminalRefusedEvent
	}
	if taskEventCountForDetailOn(ctx, tx, taskID, event, verdict.Note) < reclaimRunResidueRefusalLimit {
		taskEventWrite(ctx, tx, taskID, event, verdict.Note)
	}
	taskNotifyLog("merge-backlog terminal guard (sync): %s → %s adopted without merge proof — %s", taskID, status, verdict.Note)
}
