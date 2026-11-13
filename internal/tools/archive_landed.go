// archive_landed.go — "did this archived branch's work actually LAND?" — the
// question that turns the reason-less archive backlog from a count into a
// re-dispatchable set.
//
// THE DEFECT THIS ANSWERS. `gopi merge --status` and `ArchiveReasonlessBacklog`
// both answer "is this archived branch's owner row decided?" — a question about
// the LEDGER. Neither asks the question about the REPOSITORY. So the two states
//
//	(a) archived, owner row completed/verified, and the work IS on master, and
//	(b) archived, owner row completed/verified, and the work is NOWHERE
//
// render IDENTICALLY, and (b) is the whole of "a finished deliverable buried out
// of every merge path that nothing else will ever mention again". The 2026-09-16
// audit counted that class; the 2026-10-12 report measured it at 108 rows on the
// peer. A count of them is not actionable. "(b) alone, as a set" is.
//
// WHAT "LANDED" MEANS, and why the obvious instruments are wrong. A tag whose
// tip is an ancestor of the integration branch is trivially landed. A tag whose
// tip is NOT an ancestor is the ambiguous class, and every cheap test mislabels
// it:
//
//   - "commits ahead of master" labels almost EVERY archive tag, because an
//     archive tag is by construction a decision taken on a branch that was never
//     merged (devdoc/archive-tag-sweep-88-20260916.md records the tally).
//   - `git cherry` / `rev-list` are non-sufficient: a rebase or a squash lands
//     the same patch under a NEW hash, so they read "+" (unlanded) for work that
//     is fully present on the integration branch (memory #4972, measured).
//   - a NAME test ("does master define this symbol?") reads UNLANDED whenever
//     the file that held the symbol was DELETED on the integration branch. The
//     2026-09-18 audit called 42 rows UNLANDED by a name test; a path-presence
//     instrument cut that to 33, and a blob check cut it to 6 real ones.
//
// The instrument that survived that review is PATH PRESENCE, and it is the
// first arm this file implements: the files the branch ADDED, checked for
// presence on the integration branch. A branch that added no path the
// integration branch lacks carries no work the integration branch does not have.
//
// WHY PATH PRESENCE ALONE WAS STILL ONE ARM SHORT (repaired 2026-10-28, epic
// 01a17e06). The sentence above is exact about ADDED paths and silent about
// MODIFIED ones, so a branch whose whole change is a DIFFERENT VALUE IN A LINE
// THE INTEGRATION BRANCH ALREADY CARRIES adds no path at all — `missing` comes
// back empty, the branch reads LANDED, and the operator is told there is nothing
// to re-dispatch for a branch whose work is on the integration ref NOWHERE. The
// measured shape: `-return oldFlag` / `+return newFlag` in a file that exists on
// both sides. The same blindness covers a rebase that landed a DIFFERENT change
// in the same path: presence of the path is not presence of the work.
//
// So the second arm asks the only question that separates those cases — are the
// LINES this branch's commits added present in the integration tree? — and it is
// the delivery proof's OWN arm, reused rather than re-implemented (Rule 35,
// Rule 14): deliveryProofNormalizeLines decides which lines carry work (dropping
// blank, comment-only and self-referential `measured-at:` lines, so a rebase's
// prose drift cannot manufacture a "missing"), and deliveryProofCommitAddedLines
// reads them off each commit. This file adds no comparer of its own.
//
// THE TWO ARMS SPLIT BY WHAT THE BRANCH DID, and the split is the cost
// discipline as much as the correctness one:
//
//	added a path the integration ref lacks  → MISSING, decided by the bulk path
//	                                          arm alone (no per-commit read)
//	added no absent path (modified-only, or  → the CONTENT arm decides; a branch
//	  every added path already present)         that added nothing and changed
//	                                          nothing new stays LANDED (arm one's
//	                                          answer, unchanged)
//
// COST. One `merge-base --is-ancestor` per entry, the added-path read only for
// the entries that miss (ONE git invocation for the WHOLE miss-set,
// `git log --no-walk --diff-filter=A --name-only`, not one per entry — the chunk
// discipline archive_unreachable.go keeps for the same reason), and the
// per-commit content read ONLY on the verdict path-presence could not settle,
// bounded by archiveLandContentScanCommits. It is therefore usable on the
// explicit report face (`gopi task archive --check`) and MUST NOT be wired into
// `gopi merge --status`, which runs on every merger tick (Rule 27).
package tools

import (
	"context"
	"fmt"
	"strings"
	"time"
)

// The git spellings the land check is built from (Rule 1: named once).
const (
	archiveLandMergeBaseVerb   = "merge-base"
	archiveLandIsAncestorFlag  = "--is-ancestor"
	archiveLandCatFileVerb     = "cat-file"
	archiveLandExistsFlag      = "-e"
	archiveLandDiffFilterFlag  = "--diff-filter=A"
	archiveLandNameOnlyFlag    = "--name-only"
	archiveLandCommitHeaderFmt = "@@%H"
	// archiveLandContentScanCommits bounds how many of the branch's own commits
	// the CONTENT arm walks (newest first). A branch larger than this is not a
	// question one verdict answers: the arm reports the read as UNMEASURED
	// (ArchiveLandUnknown) rather than run long or guess (Rule 27 — this face is
	// the explicit report, not a tick).
	archiveLandContentScanCommits = 32
	archiveLandCommitHeaderPref   = "@@"
	// archiveLandRevListVerb / archiveLandNoMergesFlag / archiveLandMaxCountFlag /
	// archiveLandRangeSep are git's own spellings for the branch range the CONTENT
	// arm walks. Named once (Rule 1b/13) so the arm and its test cannot drift.
	archiveLandRevListVerb  = "rev-list"
	archiveLandNoMergesFlag = "--no-merges"
	archiveLandMaxCountFlag = "--max-count"
	archiveLandRangeSep     = ".."
	// archiveLandDeletedStatus is the leading character of `diff-tree
	// --name-status`'s DELETION status ("D", or "D<similarity>" for a rename's
	// deleted side). The arm reads the LEADING character only.
	archiveLandDeletedStatus = "D"
	// archiveLandContentTimeout bounds the whole arm ③ read of one branch. It is
	// NOT a deadline on a caller's request: it exists so a git child that wedges
	// answers UNMEASURED instead of hanging the report face (Rule 2 — the value
	// lives here and nowhere else).
	archiveLandContentTimeout = 30 * time.Second
	// archiveLandDeletionNotLandedText is the evidence line a MISSING verdict
	// carries when the branch DELETED a path the destination still has. It is a
	// sentence, not a code line, because a deletion has no line to quote.
	archiveLandDeletionNotLandedText = "this branch deletes this path and the integration ref still carries it"
	// archiveEntryRefFormat is the archive namespace asked for the PEELED COMMIT
	// (the land check's own question) rather than the reason reader's
	// object-type/subject triple: `%(objectname)` follows an annotated tag to the
	// commit it points at, and one of the 137 refs on the real repo IS an
	// annotated tag (measured 2026-10-12, archive/openai-sdk-backup-20260826).
	archiveEntryRefFormat  = "%(refname:short)%09%(objectname)%09%(contents:subject)"
	archiveEntryFieldCount = 3
)

// The verdict vocabulary of the land check. It is a TRI-state on purpose: the
// evidence can say the work is there, can say the added paths are absent, or can
// FAIL TO SAY — and the third must never be folded into either of the others
// (Rule 15: a scan that could not answer is not a clean answer).
const (
	// ArchiveLandLanded — the tag's tip is an ancestor of the integration ref
	// (its commits are literally in the integration history), or it is not an
	// ancestor while every path it added is present (landed under a new hash).
	ArchiveLandLanded = "landed"
	// ArchiveLandMissing — the tip is not an ancestor AND every path the branch
	// added is absent from the integration branch: the work is not there by
	// either instrument. This is the buried-deliverable class.
	ArchiveLandMissing = "missing"
	// ArchiveLandUnknown — the question could not be answered (no resolvable
	// integration ref, or the added-path read failed).
	ArchiveLandUnknown = "unknown"
)

// The operator-facing spellings of the three verdicts. They live HERE, in the
// package that owns the vocabulary, so two faces cannot spell one verdict
// differently (Rule 13/14 — the same reason ArchiveReasonMissingLabel lives in
// archive_reason.go).
const (
	ArchiveLandLandedLabel  = "landed"
	ArchiveLandMissingLabel = "NOT LANDED"
	ArchiveLandUnknownLabel = "land-unmeasured"
)

// archiveLandPathLimit caps how many missing paths one verdict carries. The
// verdict is exact either way (a single absent path decides it); only the NAME
// list is truncated, and the truncation is stated (Rule 1).
const archiveLandPathLimit = 3

// ArchiveLandVerdict is one archived branch's answer, with the evidence that
// produced it. Evidence rather than a bare boolean: "NOT LANDED" is a claim an
// operator (or a re-dispatch) acts on, so it names the paths that were checked
// and found absent.
type ArchiveLandVerdict struct {
	Verdict string
	// Ref is the integration ref the ancestry question was asked of. Named
	// because THIS repo carries two answers to "is it on master?" (the local
	// branch and the delivery lane's forced mirror, integration_claim_target.go)
	// and a reader must be able to see which one decided.
	Ref string
	// MissingPaths names the paths the "missing" verdict rests on — the direct
	// counter-evidence for it. Two shapes, and the sentence around it says which:
	// the paths the branch ADDED that the integration ref lacks, or the paths whose
	// WORK the content arm could not find there (see MissingLines).
	MissingPaths []string
	// MissingLines names the first work-carrying line the CONTENT arm could not
	// find in the destination's version of one path — the evidence a
	// modified-only branch leaves. Empty when the path arm alone decided, which is
	// why the two fields are separate rather than one list: a reader must be able
	// to tell "a whole file is absent" from "the file is there, this line is not".
	MissingLines []string
	// MissingLinePath is the one path MissingLines was read from (the arm reports
	// the first path it lost, so a verdict stays readable).
	MissingLinePath string
}

// ArchiveEntry is one archive-namespace ref, as the land check needs it: the
// branch it gates and the commit its tag resolves to. It is separate from
// archiveNamespaceEntry (which carries the reason reader's rendering fields)
// because the land check needs the PEELED COMMIT.
type ArchiveEntry struct {
	Tag     string
	Branch  string
	TipSHA  string
	Subject string
}

// ArchiveEntriesForRepo resolves every archive tag to the commit it names, in
// ONE `for-each-ref` call. It reads the tag's own tip rather than the branch of
// the same name: the branch is deleted at archive time, which is the namespace's
// whole purpose. An unreadable namespace is REPORTED, never returned as an empty
// set (Rule 15).
func ArchiveEntriesForRepo(repo string) ([]ArchiveEntry, error) {
	out, err := repoSweepGit(repo, "for-each-ref", "--format="+archiveEntryRefFormat, archiveTagRefPrefix)
	if err != nil {
		return nil, fmt.Errorf("archive namespace entry scan: %w", err)
	}
	var entries []ArchiveEntry
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimRight(line, "\r")
		if strings.TrimSpace(line) == "" {
			continue
		}
		f := strings.SplitN(line, archiveFieldSep, archiveEntryFieldCount)
		if len(f) < archiveEntryFieldCount {
			continue
		}
		entries = append(entries, ArchiveEntry{
			Tag: f[0], Branch: archiveBranchOfEntry(f[0], f[2]), TipSHA: f[1], Subject: f[2],
		})
	}
	return entries, nil
}

// ArchiveTipsForLandCheck returns the tips the bulk added-path read should be
// asked about: the entries whose tip is NOT already an ancestor of ref. It is
// the caller-side half of the cost discipline — the read never happens when
// every tip is already in the integration history.
func ArchiveTipsForLandCheck(repo, ref string, entries []ArchiveEntry) []string {
	var tips []string
	for _, e := range entries {
		if strings.TrimSpace(e.TipSHA) == "" {
			continue
		}
		if _, err := repoSweepGit(repo, archiveLandMergeBaseVerb, archiveLandIsAncestorFlag, e.TipSHA, ref); err == nil {
			continue
		}
		tips = append(tips, e.TipSHA)
	}
	return tips
}

// ArchiveLandProbe answers "which of the paths this tip ADDED does the
// integration ref lack?" for a whole set of tips, paying ONE `git log`
// regardless of how many tips there are. It returns nil on failure, and a nil
// probe answers ArchiveLandUnknown for every tip — so a read that could not be
// taken is REPORTED as such and never rendered as "nothing is missing"
// (Rule 15).
//
// The scan is `git log --no-walk --diff-filter=A --name-only --format=@@%H`: the
// `--diff-filter=A` clause selects the commits that ADDED each listed path, so
// the names under each `@@<sha>` header are exactly the paths that commit
// introduced, and `--no-walk` keeps it to the named commits (no history walk) —
// which is what makes one call enough.
func ArchiveLandProbe(repo, ref string, tips []string) func(tip string) []string {
	if len(tips) == 0 {
		return nil
	}
	args := append([]string{
		"log", "--no-walk", archiveLandDiffFilterFlag, archiveLandNameOnlyFlag,
		"--format=" + archiveLandCommitHeaderFmt,
	}, tips...)
	out, err := repoSweepGit(repo, args...)
	if err != nil {
		return nil
	}
	added := archiveLandParseAddedPaths(out)
	return func(tip string) []string {
		var missing []string
		for _, p := range added[tip] {
			if _, err := repoSweepGit(repo, archiveLandCatFileVerb, archiveLandExistsFlag, ref+":"+p); err != nil {
				missing = append(missing, p)
			}
		}
		return missing
	}
}

// archiveLandParseAddedPaths splits the bulk `git log --name-only` output into
// tip → the paths that tip's own commit added. One traversal, two facts: the
// commit header (which tip this block describes) and the names under it.
func archiveLandParseAddedPaths(out string) map[string][]string {
	res := map[string][]string{}
	var cur string
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(strings.TrimRight(line, "\r"))
		if line == "" {
			continue
		}
		if strings.HasPrefix(line, archiveLandCommitHeaderPref) {
			cur = strings.TrimPrefix(line, archiveLandCommitHeaderPref)
			continue
		}
		if cur != "" {
			res[cur] = append(res[cur], line)
		}
	}
	return res
}

// ArchiveLandClassify decides, for ONE archived entry, whether its work is on
// the integration branch. Exported because the report face and any future
// re-dispatch check must read the SAME predicate (Rule 13). A nil probe (the
// bulk read failed) answers Unknown, never Missing.
//
// THE ARMS, IN THE ORDER THEY DECIDE (and each one answers a question the
// previous one cannot):
//
//	① ancestry      the tip IS in the integration history — landed, no read.
//	② path presence an ADDED path is absent — MISSING, no per-commit read.
//	③ content       no added path is absent, but does the destination carry the
//	                LINES this branch added? Only ③ can answer a branch whose
//	                whole change is a modified line (no path added at all) or a
//	                rebase that re-derived the change in a path both sides carry.
//
// ③ runs through the CONTENT ARM below, which is the delivery proof's own
// (task_delivery_proof_content.go) — one normaliser, one added-lines reader, one
// containment rule (Rule 14). A destination that could not be read answers
// UNKNOWN, never Missing (Rule 15: an unmeasured read is not an accusation).
func ArchiveLandClassify(repo, ref, tip string, probe func(string) []string) ArchiveLandVerdict {
	v := ArchiveLandVerdict{Verdict: ArchiveLandUnknown, Ref: ref}
	if strings.TrimSpace(tip) == "" || strings.TrimSpace(ref) == "" {
		return v
	}
	if _, err := repoSweepGit(repo, archiveLandMergeBaseVerb, archiveLandIsAncestorFlag, tip, ref); err == nil {
		v.Verdict = ArchiveLandLanded
		return v
	}
	if probe == nil {
		return v
	}
	missing := probe(tip)
	if len(missing) > 0 {
		// ② An ADDED path the integration ref does not have decides it: whatever
		// the content arm might say about the lines, the branch introduced a file
		// that lives nowhere at the destination.
		v.Verdict = ArchiveLandMissing
		if len(missing) > archiveLandPathLimit {
			missing = missing[:archiveLandPathLimit]
		}
		v.MissingPaths = missing
		return v
	}
	// ③ No absent path. Either the work landed under a new hash, or the branch's
	// change is a MODIFIED line whose new value is nowhere at the destination —
	// the shape path presence is blind to by construction.
	landed, linePath, line, judged := archiveLandContentLanded(repo, ref, tip)
	if !judged {
		// The content question could not be answered. UNKNOWN, not Landed and not
		// Missing: the path arm already said no path is absent, so calling it
		// Missing would manufacture a re-dispatch, and calling it Landed would
		// ship the very false verdict this arm exists to remove.
		return v
	}
	if landed {
		// Not an ancestor, every path present, every added LINE present — landed
		// under a new hash (rebase/squash). Counting this as "missing" is the
		// false positive the path instrument exists to avoid.
		v.Verdict = ArchiveLandLanded
		return v
	}
	v.Verdict = ArchiveLandMissing
	v.MissingLinePath = linePath
	if strings.TrimSpace(line) != "" {
		v.MissingLines = []string{line}
	}
	return v
}

// archiveLandContentLanded is arm ③ for ONE branch: do the integration tree's
// copies of the files this branch touched carry the LINES its commits added?
//
// It reuses the delivery proof's OWN reading (Rule 35 — no second comparer):
// deliveryProofCommitPaths says which paths each commit touched and how, and
// deliveryProofCommitAddedLines + deliveryProofNormalizeLines produce the
// work-carrying lines it added. The comparison is deliveryProofLinesMissingAt,
// i.e. the same containment rule, so "present" means exactly what it means at
// the close-out seam.
//
// WHAT IT DELIBERATELY DOES NOT DO: walk the destination's history looking for a
// later commit that carried the line (deliveryProofHistoricallyCarried). An
// archived branch is judged ONCE, long after the fact, and a wrong "landed"
// retires a deliverable nobody will mention again — so this arm answers on the
// destination's CURRENT tree, the narrow direction. A change that landed and was
// then superseded reads MISSING here, which is a re-dispatch the operator can
// dismiss in one line; the opposite error is a lost deliverable.
//
// ok=false means the question could not be answered: the branch range was
// unreadable, larger than archiveLandContentScanCommits, or a commit's paths
// could not be listed. judged is never inferred from an empty diff — a commit
// that touches nothing is skipped, and a branch of only such commits answers
// LANDED (it adds nothing the destination must carry).
func archiveLandContentLanded(repo, ref, tip string) (landed bool, path, line string, judged bool) {
	ctx, cancel := context.WithTimeout(context.Background(), archiveLandContentTimeout)
	defer cancel()
	// The range is the branch's OWN work: the integration ref's merge base to the
	// tip. An unreadable merge base (unrelated history) is not a measurement of
	// "nothing" — it is a question that could not be asked.
	base, err := repoSweepGit(repo, archiveLandMergeBaseVerb, ref, tip)
	if err != nil || strings.TrimSpace(base) == "" {
		return false, "", "", false
	}
	out, err := repoSweepGit(repo, archiveLandRevListVerb, archiveLandNoMergesFlag, archiveLandMaxCountFlag,
		fmt.Sprint(archiveLandContentScanCommits+1), strings.TrimSpace(base)+archiveLandRangeSep+tip)
	if err != nil {
		return false, "", "", false
	}
	commits := strings.Fields(out)
	if len(commits) > archiveLandContentScanCommits {
		return false, "", "", false
	}
	for _, c := range commits {
		paths, ok := deliveryProofCommitPaths(ctx, repo, c)
		if !ok {
			return false, "", "", false
		}
		for _, entry := range paths {
			status, p := entry[0], entry[1]
			if strings.HasPrefix(status, archiveLandDeletedStatus) {
				// A deletion lands when the destination no longer carries the path;
				// a destination that still has it is the ONE shape this arm cannot
				// call landed, and it names the path.
				if _, err := repoSweepGit(repo, archiveLandCatFileVerb, archiveLandExistsFlag, ref+":"+p); err == nil {
					return false, p, archiveLandDeletionNotLandedText, true
				}
				continue
			}
			added := deliveryProofCommitAddedLines(ctx, repo, c, p)
			if len(added) == 0 {
				// Comment/blank/metadata only: content-free by the normaliser's own
				// definition, so nothing is owed at the destination.
				continue
			}
			destText, ok := deliveryProofBlobText(ctx, repo, ref, p)
			if !ok {
				// The destination does not carry this path at all. It was not
				// ADDED by this branch (arm ② already cleared added paths), so the
				// work it changed cannot be there either.
				return false, p, added[0], true
			}
			if missing := deliveryProofLinesMissingAt(destText, added); len(missing) > 0 {
				return false, p, missing[0], true
			}
		}
	}
	return true, "", "", true
}

// archiveLandEvidenceIndent is the indent every evidence line under a verdict
// carries — the one spelling both render sites use (Rule 1b/13), so a reader sees
// one shape whether the evidence sits in the per-branch listing or in the
// re-dispatch section.
const archiveLandEvidenceIndent = "      "

// renderArchiveLandEvidence renders ONE verdict's counter-evidence: the paths the
// path arm found absent, and — the arm added 2026-10-28 — the line the content
// arm could not find at the destination. Both arms are here because the two
// verdicts rest on different evidence and a NOT LANDED with nothing under it is a
// claim without its proof.
//
// It renders "" for a verdict that carries no counter-evidence (LANDED, UNKNOWN),
// so no caller needs to pre-test the verdict string.
func renderArchiveLandEvidence(v ArchiveLandVerdict, indent string) string {
	if len(v.MissingPaths) == 0 && v.MissingLinePath == "" {
		return ""
	}
	var b strings.Builder
	if len(v.MissingPaths) > 0 {
		fmt.Fprintf(&b, "%sabsent from %s: %s\n", indent, v.Ref, strings.Join(v.MissingPaths, ", "))
	}
	if v.MissingLinePath != "" {
		// ONE sentence for both shapes the content arm produces: a work line the
		// destination lacks, or a deletion it has not applied (that one's "line"
		// is the arm's own sentence — a deletion has no line to quote).
		fmt.Fprintf(&b, "%swork absent from %s in %s: %s\n", indent, v.Ref, v.MissingLinePath,
			strings.Join(v.MissingLines, " / "))
	}
	return b.String()
}

// ArchiveLandFunc classifies EVERY entry with the tip-and-instrument pair the
// caller resolved. It runs the loop in ONE place (Rule 13) and takes the
// instrument as an argument so the three verdict arms are drivable with a
// measured pair — the real one, a deliberately wrong ref, or nil — without a
// fixture repo per case.
func ArchiveLandFunc(repo, ref string, probe func(string) []string, entries []ArchiveEntry) map[string]ArchiveLandVerdict {
	out := make(map[string]ArchiveLandVerdict, len(entries))
	for _, e := range entries {
		out[e.Branch] = ArchiveLandClassify(repo, ref, e.TipSHA, probe)
	}
	return out
}
