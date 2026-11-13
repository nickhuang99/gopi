package tools

// task_merge_refuse_anchor.go — the refusal arm of the merge-time
// PUBLISHED-ANCHOR gate (⓪e): ARCHIVE + route-to-rework at the refusal's own
// site, so both faces that refuse (the merger's sweep tick and a manual
// `gopi task merge`) leave identical state behind.
//
// WHY A REWORK ROW AND NOT A CONFLICT ROW. The defect is a document that was
// never regenerated against the sources it publishes — a TEXT edit plus a
// re-run of the document's own generator, performed by the branch's author.
// It is not a semantic code merge, so it takes the same lane the merge-time
// BUILD and MANIFEST gates take (a branch-rework brief on a NEW branch off
// master, the predecessor archived as a decided loser) rather than the
// conflict resolver's.
//
// WHY NOT THE BUILD GATE'S BRIEF. That brief's payload is the COMPILER's own
// text plus the command to re-run — and here there is no build command to name.
// A brief carrying an empty command field would be the "a brief that points at
// nothing is a lie" defect the build gate's own archive ordering exists to
// avoid. This brief carries, instead, the two things a reworker cannot
// reconstruct: the DOCUMENT'S OWN FINDINGS (the gate package's failing test
// output, which names the anchor and the line it disagrees with) and the
// MANDATE the edge rendered for this change (what the document is, which
// generator rewrites it, which package blocks on it).

import (
	"fmt"
	"strings"
)

// anchorGateRefuseFmt renders the self-contained branch-rework briefing for a
// branch the merge-time PUBLISHED-ANCHOR gate refused.
const anchorGateRefuseFmt = `You are the merger's BRANCH-REWORK agent for repo %q. Branch %q was refused at MERGE TIME because the MERGE RESULT would carry a DOCUMENT OF RECORD whose published anchors no longer match the sources it publishes, and the merger ARCHIVED it in the same tick. Master (%q) is untouched.

ELEMENT 2 — THE DOCUMENT'S OWN FINDINGS (the gate package's failing test output, measured on the union of %q with %q):
%s

ELEMENT 3 — WHAT THIS EDGE IS (rendered by the edge itself, not written by hand):
%s

WHY THE UNION AND NOT THE BRANCH: each side can be internally consistent — the document regenerated on the branch's own tip, the document regenerated on master — and their ANSWER TOGETHER still be wrong, because the two sides regenerated the same document separately. That is the measured shape (2026-10-12: three commits each moved a routing predicate in its own branch and each regenerated the document in its own branch; every branch was green alone and the master they landed on carried a stale anchor). A branch is not "wrong" for this; the document it lands with is.

THE FIX (regenerate the document, never hand-edit its block):
1. Work on a NEW branch off master — do NOT edit the archived branch %q in place, and do not push a "regenerate" commit onto it. The archive tag refs/tags/%s%s is the decision; ` + "`git tag -d %s`" + ` revives the OLD ref if that is genuinely what you want.
2. Carry over the branch's INTENT (its diff is the specification), then re-run the document's own generator on the MERGED state — after master is in, not before: the anchors are line numbers in the sources the union composes.
3. The generator command is the one the document itself names (element 3 quotes the edge that found it). Re-running it rewrites the generated block; never hand-edit inside the block.
4. Re-run the packages the findings name (` + "`go test -count=1 ./<package>/...`" + `) and make them pass on your branch. That is the measurement the merger ran.

When those pass, the merger will land the new branch on the next tick.
`

// taskMergeRefuseAnchor is the PUBLISHED-ANCHOR gate's refusal arm: ARCHIVE +
// NOTIFY at the refusal's own site.
func taskMergeRefuseAnchor(repo, master, branch, detail string, findings []string, mandate string) TaskMergeBranchOutcome {
	if aerr := repoSweepArchiveBranch(repo, branch, ArchiveReasonPublishedAnchor); aerr != nil {
		taskNotifyLog("merger: branch %s refused by the %s but ARCHIVE FAILED: %v (branch stays a candidate)",
			branch, MergeAnchorGateName, aerr)
		return TaskMergeBranchOutcome{Branch: branch, Action: mergeBuildGateActionRefused,
			Detail: detail + " (archive failed: " + aerr.Error() + ")"}
	}
	action, submitDetail := anchorGateSubmitRework(repo, master, branch, mandate, findings)
	taskNotifyLog("merger: branch %s refused by the %s — archived as %s, notification row: %s",
		branch, MergeAnchorGateName, archiveBranchPrefix+branch, action)
	return TaskMergeBranchOutcome{Branch: branch, Action: mergeBuildGateActionRefused,
		Detail:        detail + " — archived as " + archiveBranchPrefix + branch + ", notification row: " + action + " (" + submitDetail + ")",
		BuildNotified: action == "rework-submitted"}
}

// anchorGateSubmitRework enqueues the branch-rework fix task for a branch the
// PUBLISHED-ANCHOR gate refused, deduping through the SAME deterministic
// summary every merge-time refusal lane uses (resolveRowReworkSummaryPrefix +
// branch) — one branch, ONE rework row, however it was refused — and stamping
// the branch into the fresh row's deliverable through the shared helper.
// Returns (action, detail) in taskMergeDispatchConflict's vocabulary.
func anchorGateSubmitRework(repo, master, branch, mandate string, findings []string) (string, string) {
	summary := resolveRowReworkSummaryPrefix + branch
	blocked, why, err := reworkSummaryBlocked(summary)
	if err != nil {
		return "error", err.Error()
	}
	if blocked {
		return "duplicate", fmt.Sprintf(reworkGateDuplicateDetailFormat, branch, why)
	}
	tag := archiveBranchPrefix + branch
	payload := fmt.Sprintf(anchorGateRefuseFmt,
		repo, branch, master,
		branch, master,
		strings.Join(findings, mergeAnchorGateFindingSep),
		anchorGateMandateOrFallback(mandate),
		branch, archiveBranchPrefix, branch, tag)
	_, reworkID, err := TaskSubmitReturningID(TaskSubmitOptions{
		Summary: summary,
		Payload: payload,
		Tags:    resolveRowReworkTags,
		Source:  taskMergeSourceAuto,
		Repo:    repo,
	})
	if err != nil {
		return "error", "submit: " + err.Error()
	}
	if derr := resolveRowStampDeliverableBranch(repo, branch); derr != nil {
		taskNotifyLog("merger: published-anchor refusal deliverable.branch stamp skipped: %v", derr)
	}
	// Rule 33's supersession edge (see rework_reparent_children.go): the row this
	// branch's work was falsified under must stop satisfying its children.
	reworkReparentFromBranch(repo, branch, reworkID, reworkLaneName)
	return "rework-submitted", "stale published anchor in the merge result → branch-rework fix task (branch archived as " + tag + ", master untouched)"
}

// anchorGateMandateOrFallback renders the brief's element 3: the edge's own
// mandate when it could be rendered, and a STATED fallback when it could not —
// never an empty element a reworker would read as "no document is involved"
// (Rule 15: a missing answer is not a silent one).
func anchorGateMandateOrFallback(mandate string) string {
	// The mandate is carried by the verdict (the edge's own sentence, or a
	// failure it stated); the brief only has to say which of the two it holds,
	// so a reworker is never handed an empty element it would read as "no
	// document is involved" (Rule 15).
	if strings.TrimSpace(mandate) == "" {
		return anchorGateMandateMissingNote
	}
	return mandate
}

// anchorGateMandateMissingNote is the brief's own sentence when the edge
// rendered no mandate (the findings still name the failing package).
const anchorGateMandateMissingNote = "(the edge rendered no mandate for this change — the findings above name the package that blocks on the document; the document is the tree's generated document of record)"
