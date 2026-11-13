package tools

// task_ground_removal_record.go — THE TIP GOES INTO THE REMOVAL RECORD, so the
// delivery proof can still judge BY SHA after the ground directory is gone.
//
// THE DEFECT THIS FILE EXISTS FOR (measured 2026-10-29 on this host). A CLONE
// ground is its own repository, and the close-out deletes it with
// `devworkspace.Reclaim` → `os.RemoveAll`. The only place its HEAD could be read
// was that directory, and the removal record kept no sha:
//
//	$ python3 - <<'EOF'   # over ~/.gopi/logs/removal-audit.log
//	… kind counts and sha presence …
//	EOF
//	worktree-removal 17704 with-sha 0
//	branch-delete     5259 with-sha 495
//	worktree-ground    778 with-sha 0
//
// So `deliveryProofTipOf`'s three witnesses were: the ground's HEAD (deleted),
// the branch-delete record's sha (no such record for `task/<id>` — the arm that
// deletes that ref is the branch arm, and it ran only for rows that reached it),
// and the row's landed record (written on the DELIVERED path only). A row whose
// commit was an ancestor of origin/master could therefore be judged
// `delivery-proof-unverified` for the sole reason that the DIRECTORY it lived in
// no longer existed — a judgement made conditional on a filesystem fact instead
// of on a revision.
//
// THE SHAPE OF THE FIX. The arm that deletes the ground captures the tip WHILE
// THE DIRECTORY STILL EXISTS (only the caller can — a moment later the question
// has no subject) and writes it into its `kind=worktree-removal` record
// (`removalaudit.WorktreeRemoveAt`), which is the ONE sink the delivery proof
// already reads. Nothing new is scheduled, no second store is introduced: the
// removal record that every arm already owes gains the field its reader needed.
//
// WHY IT IS ONE FUNCTION AND NOT SIX INLINE CALLS (Rule 14). The capture and the
// emit must not drift: a site that emitted without capturing, or captured
// without emitting, would look correct in review and lose exactly the sha. The
// callers pass their OWN arm constant (the record must distinguish the channel's
// reclaim from the anchor sweep), and everything else — the read, the empty-value
// policy, the record — lives here once.
//
// AN UNREADABLE TIP IS SAID OUT LOUD AND RECORDED AS ABSENT (Rule 15 / Rule
// 15a): "" is written rather than an invented sha, and the refusal is logged, so
// a row that later comes back `delivery-proof-unverified` has an explanation on
// the sink instead of a silent hole.

import (
	"strings"

	"github.com/nick/gopi/internal/removalaudit"
)

// RecordGroundRemovalTip writes the worktree-removal record for a ground about
// to be destroyed, carrying the tip its HEAD names.
//
// The caller MUST call it BEFORE the removal (the record precedes the act — the
// contract every removal arm honours, and the reason the structural gate can
// check the order), while `ground` is still a readable repository.
func RecordGroundRemovalTip(arm removalaudit.Arm, repo, ground, branch, taskID, evidence string) {
	tip := CloneDeliveryTip(ground)
	if tip == "" {
		// NOT a silent "" (Rule 15): the removal still proceeds — evidence never
		// blocks the act it explains — but the loss is named while it happens.
		taskNotifyLog("removal audit: the tip of %s could not be read before its removal (%s/%s) — "+
			"the record carries no sha, so this row's delivery proof will have to rely on its landed record",
			ground, taskID, branch)
	}
	if !deliveryProofIsHexSHA(tip) {
		tip = ""
	}
	removalaudit.WorktreeRemoveAt(arm, repo, ground, branch, taskID, strings.TrimSpace(tip), evidence)
}
