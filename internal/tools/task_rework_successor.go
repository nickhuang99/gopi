// task_rework_successor.go — THE SUCCESSOR SHAPE, read from a ledger row's own
// tags and summary, for the archive gate.
//
// THE CLOSED LOOP THIS FILE EXISTS TO BREAK (measured 2026-10-18, merge run
// 01a14b08, master 4ae648fa5). `cmd/agent/merge_prompt.md` §3.6.1 case ④ orders
// a hand-over as: (1) archive the branch's diff as an ATTACHMENT, (2) submit the
// rework row — whose deliverable.branch IS that very branch ("rework task/<id>:
// cannot auto-merge into master") — (3) remove the ref through
// `gopi task archive`. Step 2 makes step 3 impossible: the row minted in step 2
// is LIVE and names the branch as its DELIVERABLE, so the archive gate read it as
// a live deliverable in flight and refused. Eleven branches were handed over that
// run, ten refs were removed, and the eleventh stayed forever — and because the
// ref was still a merge candidate, the next pass filed a SECOND rework row for
// the same branch. The run's own record (task 01a14b7a) names it: "a closed loop
// that keeps the ref forever".
//
// WHY THE SHAPE IS NOT "A LIVE ROW OWNS IT". The gate's existing question —
// "does a live row name this branch?" — is the right question for a run that is
// BUILDING ON the branch. It is the wrong question for a row whose whole
// purpose is to REPLACE it: the successor's deliverable names the branch as its
// SOURCE (the thing it is a rework OF), and the work that used to live on the
// ref is already durably archived on that row as an attachment. Burying a spent
// ref is exactly what the archive namespace records; the deliverable is on the
// row, not on the ref.
//
// SO THE SHAPE IS READ FROM THE ROW ITSELF, never from a caller's claim:
//
//	(a) the row is LIVE (the only case this predicate can even be asked about),
//	(b) its TAGS carry the rework lane's vocabulary (reworkSelectionTags: what
//	    §3.6's own recipe writes and what the in-code lanes write), and
//	(c) its SUMMARY names THIS branch as the work it is a rework OF — the verb
//	    the lanes build their subject with ("rework <branch>: …", "resolve merge
//	    conflict: rework: <branch>", "merge verify rework: <branch>").
//
// Both live spellings are covered on purpose, and the second one is the one the
// 2026-10-18 loop actually hit. The hand-over row's deliverable may name the
// SPENT branch (resolveRowStampDeliverableBranch stamps exactly that, so the
// resolver lane can find its subject) or the FRESH branch its run will create —
// the deliverable therefore cannot decide this question. What decides it is the
// row's own SUBJECT: a row whose summary says it is a rework OF this branch is
// the branch's successor; a row whose summary says it is work ON the branch
// ("implement …", "build …", any subject naming it as the thing being built) is
// its tenant, and stays refused.
//
// WHY THAT IS STILL THE SAFE READING. Archiving writes a DECISION tag; it is not
// a deletion. The archive-reclaim arm refuses to remove a head the ledger still
// shows a live row on (archiveReclaimRowLiveness — LIVE means KEEP), so a
// successor row that is still in flight keeps its ref physically present while
// the tag tells `gopi merge` the branch is spent. What the exception buys is
// exactly the loop's other half: the branch stops being a merge candidate, so
// the next pass stops filing a duplicate rework row for it.
//
// SAFETY DIRECTION. This exception makes the archive gate LOOSER for exactly one
// shape and leaves every other shape byte-identical (Rule 15's default-deny
// reading is unchanged for all of them). The direction is deliberate and
// documented: without it the shape is not merely cautious, it is a closed loop —
// the successor row is what the merge run minted so that the ref could go, so a
// gate that blocks on it blocks the very thing it was created to allow.
package tools

import "strings"

// reworkSelectionTags is the tag vocabulary that says "this row is a branch's
// rework successor". Every member already exists in the codebase or in the
// merger's own hand-over recipe — this constant is where the ARCHIVE face reads
// them (Rule 13: one definition site, the existing ones referenced rather than
// re-spelled):
//
//   - reworkLaneName ("rework") — §3.6 step 3's own tag, and the lane's name in
//     the shared deliverable stamp (repo_resolve_row.go);
//   - reworkSelectionMergeTag ("merge") — §3.6 step 3's second tag;
//   - resolveRowTagMergeConflict ("merge-conflict") and mergeVerifyReworkTag
//     ("branch-rework") — what the IN-CODE lanes (the resolve row, the merge
//     ceiling, the hand-over escalation, the verify gate) tag their rows with.
//
// A row must carry at least ONE of them (reworkSelectionTagged): the vocabulary
// is shared, the presence test is the lane predicate.
var reworkSelectionTags = []string{
	reworkLaneName,
	reworkSelectionMergeTag,
	resolveRowTagMergeConflict,
	mergeVerifyReworkTag,
}

// reworkSelectionMergeTag is §3.6 step 3's second tag (`--tags rework,merge` in
// cmd/agent/merge_prompt.md). It is named here because that recipe's text is
// what the live merge rows actually carry, and a reader of this file must be
// able to see the spelling without opening the prompt.
const reworkSelectionMergeTag = "merge"

// reworkSelectionTagged reports whether a row's tags carry the rework lane's
// vocabulary. The tags column is comma-separated and one writer's own spelling
// (taskHasTagToken is the ONE reader of that shape, Rule 14).
func reworkSelectionTagged(tags string) bool {
	for _, want := range reworkSelectionTags {
		if taskHasTagToken(tags, want) {
			return true
		}
	}
	return false
}

// reworkSelectionPrefixes is the ONE list of SUBJECT SHAPES a rework row is
// written with, each followed IMMEDIATELY by the branch it is a rework OF:
//
//   - "rework " — §3.6 step 3's own verb in cmd/agent/merge_prompt.md
//     ("rework <branch>: cannot auto-merge into master"), spelled from
//     reworkLaneName so the lane's name has one definition;
//   - resolveRowReworkSummaryPrefix — the in-code resolve lane
//     ("resolve merge conflict: rework: <branch>");
//   - mergeVerifyReworkSummaryPrefix — the verify gate's rework row
//     ("merge verify rework: <branch>").
//
// A PREFIX is what makes the reading safe. "The summary mentions this branch"
// is not the shape: a row summarised "rework the task/X implementation in place"
// mentions X and is work ON it. The lane's identity is the prefix its writer
// builds, so the branch must be the SUBJECT of that prefix — the token directly
// after it — and nothing else in the sentence counts.
var reworkSelectionPrefixes = []string{
	reworkLaneName + " ",
	resolveRowReworkSummaryPrefix,
	mergeVerifyReworkSummaryPrefix,
}

// reworkSelectionBranch returns the branch a rework row names as the SOURCE it
// replaces, read from the row's own summary: the ref that IMMEDIATELY follows
// one of the lane prefixes above. ok is false when the summary is not a rework
// subject at all (or names no ref there), which is the conservative answer — a
// row whose subject cannot be read is not evidence that this branch is spent.
//
// WHY THE SUMMARY AND NOT A NEW COLUMN. The row's deliverable is the branch its
// run will create (or, in the hand-over lane, the spent one it was stamped
// with), so the SOURCE is not reliably in the deliverable; and a sixth field on
// the deliverable blob would be a dependency (Rule 35) for a fact both writers
// already state in the summary the lane keys its dedup on. ONE reader, ONE
// spelling, no new stored state.
func reworkSelectionBranch(summary string) (string, bool) {
	summary = strings.TrimSpace(summary)
	for _, prefix := range reworkSelectionPrefixes {
		rest, ok := strings.CutPrefix(summary, prefix)
		if !ok {
			continue
		}
		fields := strings.Fields(rest)
		if len(fields) == 0 {
			return "", false
		}
		return deliverableRefToken(fields[0])
	}
	return "", false
}

// reworkRowShape is ONE ledger row's text, as the successor predicate reads it:
// the three fields a row states its own purpose in. It is a plain struct rather
// than the full TaskForBranchOwner because the predicate must be answerable from
// whichever enumeration the caller holds (the archive gate's owner map, the axis
// records) without either side rebuilding the other's type.
type reworkRowShape struct {
	Status            string
	DeliverableBranch string
	Tags              string
	Summary           string
}

// taskReworkSuccessorOf reports whether one ledger row is the REWORK SUCCESSOR
// of branch — a live row in the rework lane that names the branch as its SOURCE
// and not as its deliverable. It is the ONE spelling of the successor shape
// (the archive gate's exception, Rule 13), so a later caller cannot invent a
// second reading of it.
//
// The live test is the gate's own: this predicate only ever removes a REFUSAL
// that the gate is about to issue for a live row, so it answers about the row
// the gate is holding.
func taskReworkSuccessorOf(row reworkRowShape, branch string) bool {
	branch = strings.TrimSpace(branch)
	if branch == "" || !isLiveTaskStatus(row.Status) {
		return false
	}
	if !reworkSelectionTagged(row.Tags) {
		return false
	}
	source, ok := reworkSelectionBranch(row.Summary)
	return ok && source == branch
}

// ArchiveReworkLaneName exposes the rework lane's name to the CLI renderer, so
// the sentence an operator reads after a successful archive cannot spell the
// lane differently from the mechanism that decided it (Rule 13/14: one
// definition site — reworkLaneName — and one accessor that returns it).
func ArchiveReworkLaneName() string { return reworkLaneName }

// deliverableRefToken reports whether one whitespace-separated token of a
// summary is a deliverable.branch-shaped ref, returning it with the trailing
// punctuation the lanes' sentence shapes leave behind trimmed (a trailing ':'
// after "rework <branch>:", a trailing ')' after "… (DELETED)").
//
// The shape test is deliberately narrow: a URL, an @-path, a flag or a bare word
// is never a branch name, and a branch name always carries its namespace
// separator in this project's lane vocabulary (task/<id>, feat/<name>, fix/<x>).
// Being narrow is the safe direction — an unreadable subject answers "not a
// successor", which leaves the gate refusing.
func deliverableRefToken(field string) (string, bool) {
	ref := strings.Trim(field, `"',;:`)
	ref = strings.TrimSuffix(ref, ")")
	if ref == "" || strings.HasPrefix(ref, "-") || strings.ContainsAny(ref, "@") || strings.Contains(ref, "://") {
		return "", false
	}
	if !strings.Contains(ref, "/") {
		return "", false
	}
	return ref, true
}
