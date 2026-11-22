package tools

// archived_branch_retirement.go — ONE exported predicate answering "is this
// branch a RETIRED, DECIDED EXCLUSION?", read from the durable record the
// project's one archive writer leaves, and plumbed into every seam that decides
// what to do with a branch-shaped row.
//
// THE MEASURED DEFECT (task 01a1446e's bug row; measured on peer1 by run
// 01a141c4, 2026-10-16). A merge-backlog row ("merge task/probe-zombie into
// master") stayed LIVE after the branch was retired — archive tag
// refs/tags/archive/task/probe-zombie plus its companion reason record
// refs/gopi/archive-reason/task/probe-zombie, code `unrelated-history`, written
// 2026-10-16T06:57:41. The row was then handed over cross-host by `gopi task
// sync --handover` (its cancel reason names the handover and says nothing about
// the retirement) and claimed on sager nine seconds later, so a worker session
// was spent on a void premise twelve hours after the branch had been taken out
// of every merge lane. The sibling reconciler (archive_row_reconcile.go) existed
// and did not close that row: its own doctrine says why, in its header — it
// closes a row "ONLY when the row's own summary carries the branch through the
// ONE merge-backlog family parser", and the reconciliation only ever runs from
// the DECIDING pass (cmd/agent/merge_run.go), which had already finished on the
// other host.
//
// WHY THE ARMS ARE READ THIS WAY (Rule 5's Evidence — the classes are MEASURED
// against the live repo /home/user/work/GoPi, not assumed):
//
//   - THE MATCH IS ON THE CANONICAL BRANCH (refArchivedExclusion's spec), because
//     branch names reach a repository through two renamed spellings. The live
//     namespaces carry all of them:
//
//     refs/gopi/archive-reason/task/probe-zombie
//     refs/tags/archive/task/probe-zombie
//     refs/tags/archive/01a113e467767eb9a4c33cb1df1cdfbb-20261007
//     refs/tags/archive/01a12d6b-2026-10-12
//
//     A reader that compared the row's summary branch to the ref name verbatim
//     would miss `probe-zombie` (the task-id task_id vs ground branch
//     rename); a reader that stripped every "task/" prefix would fold
//     /home/user/work/GoPi (which IS a real branch on this host — the probe
//     branch 01a1446e's own filer measured) onto the branch `GoPi`, i.e. onto
//     the MAIN work tree's name. ONE reader, KEYED ON THE WHOLE CANONICAL
//     SPEC, is what the three live spellings above demand, and
//     archive_row_reconcile.go (the member reader) plus the guard call sites
//     below are the whole of it.
//
//   - THE REASON RECORD IS READ THROUGH archiveReasonRecordOf (archive_reason.go)
//     — the namespace's ONE reader — and its code through ArchiveTagReason, the
//     ONE parser of the one line the record owns. There is no second parser and
//     no second scan here (Rule 14).
//
// WHAT IT IS NOT. It is not a merge path, not a writer and not a membership
// rule for rows: the merge lanes read their own exemption
// (repoSweepArchiveExempt), the merge-backlog family is judged by
// mergeBacklogFamilyBranch, and this predicate answers exactly one question —
// "has git already recorded a DECISION that takes this branch out of every
// merge lane?". A tag WITHOUT a vocabulary reason is not a decision (Rule 15:
// no credit for a typo), so it is not an exclusion either.

import (
	"strings"
)

// ArchivedBranchRetirement is the answer: whether the branch is a retired,
// decided exclusion, and — when it is — WHICH decision, so every refusal this
// predicate feeds can NAME it (the archive tag, the reason code and the
// human gloss of that code) instead of restating the why from memory.
type ArchivedBranchRetirement struct {
	// Branch is the branch the DECISION was recorded about, as the record's own
	// ref spells it (the spec matched it to the spelling the caller asked with,
	// and the record's spelling is the one a `git tag -d` command has to use).
	Branch string
	// Tag is the archive tag ref name (`refs/tags/` + ArchiveBranchPrefix +
	// branch) that carries the decision.
	Tag string
	// Reason is the reason CODE from the closed vocabulary, read from the
	// record's own `reason:` line.
	Reason string
	// ReasonDoc is ArchiveReasonDoc(Reason) — the one-line gloss, carried so a
	// refusal sentence cannot describe a lane differently from `gopi task
	// archive -h` (Rule 13).
	ReasonDoc string
}

// branchRetirementSpecPrefixes are the leading path segments a branch name
// arrives with when the row's spelling and the ref's spelling differ. They are
// STRIPPED for the comparison, and only these: an arbitrary number of segments
// would fold unrelated names together, which is why the spec is a closed list
// and not a loop.
var branchRetirementSpecPrefixes = []string{
	// The fallback shape the archive-ref-name comparison below needs: the merge
	// gate mints a tag for the row's own branch, "task/<task-id>", while the
	// ground the row reports is "<task-id>".
	"task/",
}

// CanonicalBranchRetirementSpec reduces the spellings of ONE branch to a
// COMPARISON KEY. THE KEY IS A SPEC, NOT A BRANCH NAME: it is the value
// archiveBranchRetirementSpecKey produces from the row's branch, and the value
// CanonicalBranchRetirementSpec produces from a REF NAME's spelling (which is
// the canonical spec — it may still carry the leading slash a ref name
// contributes).
//
// The rules, in order:
//
//  1. trim, and drop every trailing slash — the suffix a ref NAME contributes
//     (`archiveBranchPrefix + "task/x"` has none, but the spelling
//     `refs/tags/archive/` does, and the empty suffix must not turn one name
//     into two key candidates).
//  2. drop leading slashes, so a name that reached the comparison through a ref
//     NAME is normalized to the shape a branch name has.
//  3. drop ONE leading "task/" segment — the task-id vs ground-branch rename.
//     Only ONE, and only when a segment follows it.
//
// A name outside this spec (a branch spelled with a leading "task/" AND a slash
// inside the id, e.g. "task/x/y") keeps its slashes and matches only an
// identical spelling: the comparison never guesses which segment is the id.
func CanonicalBranchRetirementSpec(name string) string {
	spec := strings.TrimRight(strings.TrimSpace(name), "/")
	spec = strings.TrimLeft(spec, "/")
	for _, prefix := range branchRetirementSpecPrefixes {
		if rest := strings.TrimPrefix(spec, prefix); rest != spec {
			// "rest != spec" already excluded the empty remainder, so a bare
			// "task/" keeps its own spelling instead of collapsing to "".
			return rest
		}
	}
	return spec
}

// archiveBranchRetirementSpecKey is the same reduction applied to a REF NAME's
// own spelling, which is the only place the leading slash appears. It is this
// function, not a second spec, that keeps a ref-name spelling comparable to a
// branch-name spelling (Rule 14: ONE predicate — the two spellings are the two
// ends of it).
func archiveBranchRetirementSpecKey(refName string) string {
	return CanonicalBranchRetirementSpec(strings.TrimLeft(strings.TrimSpace(refName), "/"))
}

// ArchivedBranchExclusion answers "is this branch a retired, decided
// exclusion?" by reading the repo's decision records.
//
// THREE-VALUED BY CONSTRUCTION, and the third value is the important one: a
// repository git could not be asked answers (zero value, false) — NOT an
// exclusion. Rule 15's direction: "I could not look" must never be rendered as
// "decided". The caller that needs to distinguish the two reads the error the
// work is done through (repoSweepGit reports it), and every caller of this
// predicate treats false as "no decision recorded here", which is the state
// that leaves today's behaviour exactly as it was.
//
// It is a PURE READ: no ref is written, no row is touched, nothing is logged.
func ArchivedBranchExclusion(repo, branch string) (ArchivedBranchRetirement, bool) {
	spec := CanonicalBranchRetirementSpec(branch)
	if spec == "" {
		return ArchivedBranchRetirement{}, false
	}
	records, _, err := archiveReasonRecords(repo)
	if err != nil {
		return ArchivedBranchRetirement{}, false
	}
	for refBranch, record := range records {
		if archiveBranchRetirementSpecKey(refBranch) != spec {
			continue
		}
		code := ArchiveTagReason(record)
		if code == "" {
			// A record without a code from the closed vocabulary is not a
			// decision this predicate may act on (Rule 15: no credit for a
			// typo). The scan is deterministic in its input but not in its
			// iteration ORDER (a Go map), so the answer is made order-free
			// instead: a code-less record is skipped and the search continues,
			// and the vocabulary guarantees at most one code per record.
			continue
		}
		return ArchivedBranchRetirement{
			Branch:    refBranch,
			Tag:       archiveTagRefPrefix + refBranch,
			Reason:    code,
			ReasonDoc: ArchiveReasonDoc(code),
		}, true
	}
	return ArchivedBranchRetirement{}, false
}

// ArchivedBranchExclusionText is the ONE sentence every refusal of a retired
// branch's row carries (Rule 13: one spelling, so the handover seam, the
// dispatch guard and any future reader cannot describe the same decision
// differently). It names the branch, the decision (tag + code + gloss), why no
// lane can land it, and — because a refusal an operator cannot act on is a
// dead end — the two ways out that exist.
func ArchivedBranchExclusionText(rec ArchivedBranchRetirement) string {
	return "branch " + rec.Branch + " is a RETIRED, DECIDED EXCLUSION in git: it carries the archive decision " +
		rec.Tag + " with reason " + rec.Reason + " (" + rec.ReasonDoc + "). No merge lane can land it " +
		"(it is exempt from every merge path, and safe-merge.sh refuses it by the same tag), so this row's " +
		"premise is void and dispatching it would spend a session on nothing. The decision is durable and the " +
		"branch ref is PRESERVED: merge it by re-creating the branch when the work is wanted, or drop the tag " +
		"(`git tag -d " + ArchiveBranchPrefix + rec.Branch + "`) to re-arm the branch."
}

// RowBranchRetirement asks the same question over EVERY branch spelling a
// merge-backlog-family ROW carries, and it exists because one row names two
// different branches for two different purposes:
//
//   - the SUMMARY's branch is the row's SUBJECT — "merge <branch> into master",
//     the premise the whole row rests on;
//   - the DELIVERABLE's branch is the run's own GROUND (the executor stamps
//     task/<task-id> there), a fact about where the work happens, not about what
//     is to be merged.
//
// THE MEASURED SHAPE THAT MAKES THE UNION NECESSARY (task 01a1446e's bug row —
// the live row 01a141c4 read back from this host's ledger on 2026-10-16):
//
//	summary:     merge task/probe-zombie into master
//	deliverable: {"branch":"task/01a141c4615b7b0d9f47a8c5e946ce07",
//	              "repo":"/home/user/work/GoPi", "worktree":"…"}
//
// The retired branch is the SUMMARY's; the deliverable's branch is the worker's
// ground and is not retired and never will be. A reader that consulted only the
// deliverable (the shape mergeBacklogGuardBranch prefers, for good reasons: an
// executor's stamp is authoritative about the branch it is WORKING on) answers
// "no decision here" about the one branch that has one — and that is exactly
// how a void premise survived a retirement, was handed over and was claimed.
//
// The two spellings are asked through the SAME predicate (ArchivedBranchExclusion,
// Rule 14) — this function adds no judgement of its own: it is the plumbing that
// presents a row's branch spellings to it.
func RowBranchRetirement(repo, summary, tags, deliverable string) (ArchivedBranchRetirement, bool) {
	if strings.TrimSpace(repo) == "" {
		return ArchivedBranchRetirement{}, false
	}
	var spellings []string
	if named, family := mergeBacklogFamilyBranch(summary, tags); family {
		spellings = append(spellings, named)
	}
	if ground := taskDeliverableParse(deliverable).Branch; strings.TrimSpace(ground) != "" {
		spellings = append(spellings, strings.TrimSpace(ground))
	}
	for _, spelling := range spellings {
		if rec, retired := ArchivedBranchExclusion(repo, spelling); retired {
			return rec, true
		}
	}
	return ArchivedBranchRetirement{}, false
}
