package tools

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/nickhuang99/gopi/internal/config"
)

// What REMAINS of the merger's ledger vocabulary after the resolve machinery
// was deleted (epic merger-ff-park-file, 2026-09-16).
//
// The merger no longer owns a resolve TRACKING row, no longer counts per-branch
// resolve failures, no longer submits a rework row of its own and no longer
// spawns a resolver — a non-fast-forwardable branch is REPORTED and left
// completely alone (repo_sweep.go's repoSweepNotFFOutcome; the park row that
// used to carry it is gone, epic merger-remove-park, 2026-09-17), and conflict
// resolution is the ordinary worker path's job. What survives here is exactly
// the vocabulary the surviving lanes still SHARE, and nothing else:
//
//   - the merger-owned ROW MARKERS (merge-conflict + auto-spawn), because
//     TaskNextFiltered still excludes historical merger-owned rows from the
//     worker dispatch (tasks.go resolveRowIsMergerOwned) — the marker must
//     keep matching rows written by older binaries;
//   - the branch-REWORK lane's summary prefix + tags, shared by the two
//     merge-time compile gate refusals (task_merge.go) and the dirty
//     dead-holder salvage arm (dirty_holder_salvage.go) so ONE branch can only
//     ever have ONE rework row however it was refused;
//   - the branch stamp the rework lane and the salvage lane both apply to the
//     row they just filed.

// resolveRowTagMergeConflict and resolveRowTagAutoSpawn are the merger-owned
// row markers. auto-spawn ALONE is the merger's exclusive discriminator
// (TaskNextFiltered excludes it). A scan matches a row when tags contain BOTH
// markers.
//
// The merge-conflict VALUE is shared package-wide: taskTagMergeConflict (the
// adoption dispatch const in task_adoption.go) is the single definition —
// resolveRowTagMergeConflict aliases it so a rename can never desync the
// dispatch gate (tasks.go resolveRowIsMergerOwned) from the row creators.
// These stay after the resolve machinery's deletion because the ROWS stay: a
// ledger written by an older binary still carries them.
const (
	resolveRowTagMergeConflict = taskTagMergeConflict
	resolveRowTagAutoSpawn     = "auto-spawn"
)

// resolveRowTags is the merger-owned row's tag SET (merge-conflict +
// auto-spawn) — the exact shape the merger's resolve rows carried, and the
// shape the dispatch gate must still recognise in ledgers written before the
// resolve machinery was removed. Spelled once from the two markers above
// (Rule 1b), so a rename cannot leave the gate matching a tag set no row
// creator produces.
var resolveRowTags = resolveRowTagMergeConflict + "," + resolveRowTagAutoSpawn

// resolveRowReworkSummaryPrefix heads every branch-rework fix task, so the
// queue reads the intent at a glance and the dedup query has a deterministic
// marker (summary = prefix + "<branch>").
const resolveRowReworkSummaryPrefix = "resolve merge conflict: rework: "

// resolveRowReworkTags marks a branch-rework task (merge-conflict +
// branch-rework). This is a QUEUE row a future agent/PA claims.
const resolveRowReworkTags = resolveRowTagMergeConflict + ",branch-rework"

// resolveRowStampDeliverableBranch merges branch into the fresh branch-rework
// row's deliverable JSON ({"repo":...} → {"repo":...,"branch":...}). The row is
// identified by its deterministic summary (resolveRowReworkSummaryPrefix +
// branch) because TaskSubmit returns a rendered confirmation, not the bare
// task_id. Best-effort: a stamp failure must NOT fail the submit (the row is
// queued and claimable either way). The stamp itself is shared with the
// dirty-holder salvage lane — ONE implementation of "record the branch on the
// row I just filed" (Rule 14).
func resolveRowStampDeliverableBranch(repo, branch string) error {
	return stampDeliverableBranchForSummary(reworkLaneName, resolveRowReworkSummaryPrefix+branch, repo, branch)
}

// reworkLaneName is the branch-rework lane's name in the shared stamp's notify
// line (Rule 1b: the lane identifies itself with a constant, not a literal at
// the call site).
const reworkLaneName = "rework"

// repoSweepArchiveBranch marks branch a DECIDED EXCLUSION on its tip
// refs/tags/archive/<branch> — the exemption every merge path honors
// (repoSweepArchiveExempt, taskMergeExempt's sibling; cf. PROJECT.md Rule 9 +
// scripts/safe-merge.sh: an archive/<branch> tag = decided loser, never
// merged). The branch REF itself is PRESERVED (never branch -d): a human / PA
// may still mine the patch, and removing the tag re-arms the branch for a
// future merge. Idempotent: a branch whose archive already exists is success
// (the decision is idempotent — the writer leaves an existing reason record
// alone).
//
// reason is the caller's WHY, and it is REQUIRED — this function is a thin
// delegation to ArchiveTagReasoned (epic hand-archive-gate), never a second
// writer. ONE caller puts a branch OUT of the merge queue with it: the
// merge-time compile gate (a branch that does not build), which passes
// ArchiveReasonCompileFailure. The former second caller — the park lane, for a
// branch that does not fast-forward — is GONE (epic merger-remove-park,
// 2026-09-17): a non-ff branch is now reported and left alone, and the merge
// path (cmd/agent/merge_prompt.md §3.6 — the retired daemon's take-over prompt
// was deleted with it, epic merger-delete-dead-impl 2026-09-19) needs its ref to
// stay alive and UNTAGGED — "archive tags are history, never a filter" is the
// same rule stated from the merger's side, so minting one there would defeat
// the hand-over it exists for.
//
// Before the reason lane existed this function minted a bare LIGHTWEIGHT tag, so
// an automatic refusal and a human `git tag` produced indistinguishable records
// — the ambiguity the 2026-09-16 21:04 incident turned on. Returns the git error
// when the record cannot be written — the caller then leaves the branch for a
// later retry.
func repoSweepArchiveBranch(repo, branch, reason string) error {
	if strings.TrimSpace(reason) == "" {
		// Rule 15: the reason is the gate's whole point. An empty one would
		// write exactly the reason-less record the 21:04 incident produced, so
		// it is refused HERE rather than recorded as a nameless decision.
		return fmt.Errorf("repoSweepArchiveBranch: refusing to archive %s with no reason", branch)
	}
	// IDEMPOTENCE FIRST, and it is asked BEFORE the owner gate because the
	// answer is already on the record: a branch whose archive exists was
	// decided in an earlier run, and a LATER run finding a row over that branch
	// must not turn that standing decision into a refusal. This is what makes
	// `gopi merge`'s unrelated-history retirement safe to reach through a
	// repeated pass (epic unrelated-history-durable-retire: the second pass
	// comes up empty), and it is a no-op for every caller that probes a
	// not-yet-archived branch — the compile gate's branch is unarchived by
	// construction, so its owner gate below is unchanged.
	if repoSweepArchiveExempt(repo, branch) {
		return nil // already archived — idempotent
	}
	// THE OWNER GATE — the same predicate the human `gopi task archive` face refuses
	// on (ArchiveDecide → archiveOwnerDecided), applied where the MECHANISM
	// writes. Without it the epics are only half-closed: the hand path is gated
	// while the lane that fires unattended (the merge-time compile gate, and
	// now the unrelated-history verdict) still buries a finished task's
	// deliverable in silence, which is the 21:04 defect reached by a different
	// door. The refusal runs BEFORE any ref exists, so a refused archive leaves
	// no trace at all.
	if err := repoSweepArchiveOwnerGate(repo, branch, reason); err != nil {
		return err
	}
	_, err := ArchiveTagReasoned(repo, branch, reason, repoSweepArchiveDetail(reason, branch))
	return err
}

// repoSweepArchiveOwnerGate refuses to archive a branch whose owning ledger row
// says the work FINISHED (completed/verified). The override for the mechanism is
// the settings key repos.<path>.archive_bury_override — a deliberate, recorded
// decision — because an unattended lane cannot be asked a question and must
// never invent one: with no override configured the refusal is the only honest
// outcome, and the caller reports it loudly (Rule 15b: never a swallow).
//
// The gate is LOAD-BEARING for `gopi merge`'s unrelated-history retirement
// (epic unrelated-history-durable-retire): a branch whose row the ledger calls
// FINISHED is NOT retired by that verdict — the caller keeps today's report-and
// -leave-alone arm and says why. Retiring it would bury a finished deliverable
// under an exclusion tag nothing ever mentions again, which is the 21:04 defect
// reached through a third door.
//
// An unreadable ledger is NOT permission: the archive proceeds and the gap is
// recorded in the reason record's body, because a census failure must not turn
// a legitimate refusal into a silent write, nor a legitimate write into a
// spurious refusal (the reason record carries the evidence either way).
func repoSweepArchiveOwnerGate(repo, branch, reason string) error {
	owners, err := archiveBranchOwners()
	if err != nil {
		// The reason record says the census could not be read (archiveLedgerUnreadableReason).
		return nil
	}
	owner, ok := owners[branch]
	if !ok || archiveOwnerArchivable(owner.status) {
		return nil // unowned, or a row the ledger already decided AGAINST the work
	}
	decided := archiveOwnerDecided(owner.status)
	if decided && config.ArchiveBuryOverrideFor(repo) != "" {
		return nil
	}
	if !decided {
		return fmt.Errorf(
			"archive-why refused: task %s owns %s and is %q — the work is IN FLIGHT, so archiving it would bury a "+
				"running deliverable (repoSweepArchiveExempt hides the branch from every merge path). Wait for the row "+
				"to conclude, or cancel it deliberately",
			owner.taskID, branch, owner.status)
	}
	return fmt.Errorf(
		"archive-why refused: %s is owned by a FINISHED ledger row (%s (%s)) — archiving it would bury a deliverable "+
			"nothing would ever mention again (repoSweepArchiveExempt hides it from every merge path). Reason given: %s. "+
			"Either land the work (a fresh branch off master, per GOPI.md Rule 23) or set %s.%s naming why the pair is intended",
		branch, owner.taskID, owner.status, reason, config.ReposSettingsKey, config.RepoArchiveBuryOverrideKey)
}

// archiveOwnerArchivable reports whether a ledger status lets an archive write
// proceed WITHOUT an override: the ledger already decided AGAINST the work
// (failed/cancelled), so tagging the branch is bookkeeping, never burial. It is
// the ONE predicate both faces share (Rule 13) — the human `gopi task archive` gate
// (ArchiveDecide) and the mechanism gate above — so the two can never disagree
// about which owner rows block a burial.
func archiveOwnerArchivable(status string) bool {
	return status == TaskStatusFailed || status == TaskStatusCancelled || status == TaskStatusSuperseded
}

// repoSweepArchiveDetail is the free-text detail the merger's lanes record
// under the reason line. One derivation per lane (Rule 13), so the reason
// record and the log line cannot describe the same decision differently.
func repoSweepArchiveDetail(reason, branch string) string {
	switch reason {
	case ArchiveReasonCompileFailure:
		return "the merge-time compile gate refused this branch: it does not build against " + masterBranchName
	case ArchiveReasonUnrelatedHistory:
		return "the since-removed `gopi merge` verb could not merge this branch by construction: it shares no commit with " + masterBranchName +
			" (git merges such a branch only with --allow-unrelated-histories), so no merge lane may ever land it; " +
			"retired as a decided exclusion, ref preserved"
	}
	return "archived by the merger sweep"
}

// resolveRowDeliverable renders the JSON deliverable stamped on a branch-scoped
// row ({"repo":...,"branch":...}) — the same repo-scoped deliverable every
// task-row reports, written by BOTH the branch-rework lane and the dirty
// dead-holder salvage arm.
func resolveRowDeliverable(repo, branch string) string {
	return `{"repo":"` + resolveRowJSON(repo) + `","branch":"` + resolveRowJSON(branch) + `"}`
}

// resolveRowJSON escapes user-supplied strings for the JSON deliverable.
func resolveRowJSON(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, `"`, `\"`)
	return s
}

// resolveRowDeliverableRepoBranch reports whether a row's deliverable JSON names
// exactly this repo AND branch — the readback both the branch-rework lane and
// the dirty dead-holder salvage lane use to prove the row they filed is scoped
// to the work they meant (a row stamped with the wrong branch would dedup
// against the wrong worker).
func resolveRowDeliverableRepoBranch(deliverable, repo, branch string) bool {
	var d struct {
		Repo, Branch string
	}
	if err := json.Unmarshal([]byte(deliverable), &d); err != nil {
		return false
	}
	return d.Repo == repo && d.Branch == branch
}

// resolveRowOwnerPID decodes a row's claimed_by stamp into the holder pid.
func resolveRowOwnerPID(claimedBy string) (int, bool) {
	_, pid, ok := claimHolder(claimedBy)
	return pid, ok
}

// resolveRowIsMergerOwned reports whether a tags string marks a merger-owned
// row (carries BOTH merge-conflict AND auto-spawn). The dispatch filter
// (TaskNextFiltered) uses it to keep merger-internal rows — including rows
// written by binaries predating the park redesign (a lane since removed, epic
// merger-remove-park 2026-09-17) — out of the worker queue.
func resolveRowIsMergerOwned(rowTags string) bool {
	hasMC, hasAS := false, false
	for _, tg := range strings.Split(rowTags, ",") {
		switch strings.TrimSpace(tg) {
		case resolveRowTagMergeConflict:
			hasMC = true
		case resolveRowTagAutoSpawn:
			hasAS = true
		}
	}
	return hasMC && hasAS
}
