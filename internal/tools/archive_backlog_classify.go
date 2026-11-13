// archive_backlog_classify.go — the archived set as a CLASSIFICATION rather
// than a listing, and the re-dispatchable subset inside it.
//
// WHY THIS EXISTS. The reason-less backlog check (ArchiveReasonlessBacklog)
// answers ONE cell of a two-dimensional question: "reason-less AND owner row
// decided". It deliberately narrows to decided rows, and its own doc says why
// (a branch whose owner row says failed/cancelled is legible from the row). The
// measured consequence of that narrowing is the rest of the corpus: on the real
// repo 137 branches are archived, 133 carry no reason, and the overwhelming
// majority have NO owner row at all — they render `(none) (unknown)` and are
// reported by NOTHING. A branch in that group can hold a finished deliverable
// and no surface will ever say so.
//
// So this face classifies EVERY archived branch on three axes, and reports the
// intersection an operator can ACT on:
//
//	reason   x  owner row            x  landing
//	--------    ------------------     -------------------------------
//	recorded    decided (completed/    landed  (tip is in the integration ref,
//	            verified)              --      or its added paths are all present)
//	absent      open (any other        missing (tip is not, and the paths it
//	            status)                --      added are absent from the ref)
//	--          no row at all          unknown (the question could not be asked)
//
// The actionable set is `<absent or recorded> x decided x missing`: an owner row
// that says the work FINISHED, over a branch whose work is NOWHERE. That is the
// "important work buried without an audit trail" class stated as a list instead
// of a rumour, and each row carries the one command that re-arms it.
//
// COST DISCIPLINE (Rule 27). This face is explicit-invocation only
// (`gopi task archive --check`). It is NOT wired into `gopi merge --status`,
// which runs on every merger tick: one `merge-base` probe per archived branch is
// affordable on demand and is not affordable per tick.
package tools

import (
	"fmt"
	"sort"
	"strings"
)

// archiveRowNoOwnerLabel is what the classification prints when no ledger row
// names the branch. Spelled once here (Rule 1b/14); the merge-status face spells
// the same absence its own way for its own column layout, and both are honest
// about it being an ABSENCE rather than a status.
const archiveRowNoOwnerLabel = "(none)"

// ArchiveRowClass is one archived branch, classified on the three axes. It is
// the unit of the report and (in the actionable set) the unit of a re-dispatch.
type ArchiveRowClass struct {
	Branch string
	Tag    string
	// Reason is the recorded reason code, or "" when the record is absent. The
	// operator-facing text for "" is ArchiveReasonMissingLabel, exactly as the
	// merge-status face renders it (Rule 13: one spelling per fact).
	Reason string
	// OwnerTaskID / OwnerStatus are the owning ledger row, empty when none names
	// the branch.
	OwnerTaskID string
	OwnerStatus string
	// Land is the landing verdict (ArchiveLandLanded / Missing / Unknown).
	Land ArchiveLandVerdict
}

// ReasonLess reports whether the branch carries no recorded reason.
func (c ArchiveRowClass) ReasonLess() bool { return strings.TrimSpace(c.Reason) == "" }

// OwnerDecided reports whether the owning row says the work FINISHED. It reads
// the ONE predicate the refusal gate, the sweep line and the backlog check all
// read (archiveOwnerDecided — Rule 13), so "decided" cannot mean two things.
func (c ArchiveRowClass) OwnerDecided() bool { return archiveOwnerDecided(c.OwnerStatus) }

// OwnerOpen reports whether a ledger row names the branch and does NOT say the
// work finished — a row still being carried, or one the ledger decided against.
func (c ArchiveRowClass) OwnerOpen() bool {
	return strings.TrimSpace(c.OwnerTaskID) != "" && !c.OwnerDecided()
}

// OwnerAbsent reports whether NO ledger row names the branch.
func (c ArchiveRowClass) OwnerAbsent() bool { return strings.TrimSpace(c.OwnerTaskID) == "" }

// OwnerCell renders the owner column: the row and its status, or the explicit
// absence — never an empty field a reader would take for a parsing failure.
func (c ArchiveRowClass) OwnerCell() string {
	if c.OwnerAbsent() {
		return archiveRowNoOwnerLabel
	}
	return c.OwnerTaskID + " (" + c.OwnerStatus + ")"
}

// ReasonCell renders the reason column, naming the absence in the same words
// the merge-status face uses.
func (c ArchiveRowClass) ReasonCell() string {
	if c.ReasonLess() {
		return ArchiveReasonMissingLabel
	}
	return c.Reason
}

// LandCell renders the landing column.
func (c ArchiveRowClass) LandCell() string {
	switch c.Land.Verdict {
	case ArchiveLandLanded:
		return ArchiveLandLandedLabel
	case ArchiveLandMissing:
		return ArchiveLandMissingLabel
	default:
		return ArchiveLandUnknownLabel
	}
}

// ArchiveBacklogClass is the classified archived set: every branch, in the
// namespace's stable order, plus the counts each face renders.
type ArchiveBacklogClass struct {
	Repo string
	Rows []ArchiveRowClass
	// IntegrationRef is the ref the landing question was asked of, named so a
	// reader can tell which of this repo's two "is it on master?" answers
	// decided (integration_claim_target.go). Empty when it could not be resolved.
	IntegrationRef string
	// LandErr says the landing question could NOT be asked (the bulk added-path
	// read failed). It is kept so the report can say "land-unmeasured" out loud
	// rather than presenting an all-Unknown table as a measured one (Rule 15).
	LandErr string
	// Malformed lists the companion-namespace refs the reason scan had to SKIP
	// because they do not point at a blob (measured defect 01a143b4: one
	// commit-typed ref used to abort the whole scan). They are carried in the
	// report rather than folded into a per-row "no reason", because a record
	// that could not be READ is not a record that was never WRITTEN (Rule 15).
	Malformed []ArchiveReasonMalformed
}

// ReasonLessCount is how many archived branches carry no recorded reason — the
// figure the operator had to grep out of the raw listing, reported by the
// surface that knows it.
func (b ArchiveBacklogClass) ReasonLessCount() int {
	n := 0
	for _, r := range b.Rows {
		if r.ReasonLess() {
			n++
		}
	}
	return n
}

// BuriedUnlanded returns the RE-DISPATCHABLE set: branches whose owning row says
// the work FINISHED while the work itself is not on the integration branch.
//
// It admits BOTH reason states deliberately. A recorded reason can itself say
// "this was a mistake" (ArchiveReasonHandMistake) or "the intent landed
// elsewhere" (ArchiveReasonSuperseded) — neither of which is evidence that the
// WORK landed, so filtering on the reason would hide exactly the rows this face
// exists to surface. The landing verdict is the only axis that speaks to whether
// the deliverable is reachable, and it is the axis that decides here.
func (b ArchiveBacklogClass) BuriedUnlanded() []ArchiveRowClass {
	var out []ArchiveRowClass
	for _, r := range b.Rows {
		if r.OwnerDecided() && r.Land.Verdict == ArchiveLandMissing {
			out = append(out, r)
		}
	}
	return out
}

// ClassifyArchiveBacklog classifies every archived branch of repo. It reads the
// archived set and the reason records through the namespace's existing readers,
// resolves the owning rows through the existing pick, resolves the integration
// ref through the ONE resolver, and asks the landing question with the bulk
// added-path instrument.
//
// A FAILURE ON ANY READ IS RETURNED, never folded into an empty or clean
// classification: a scan that could not see the ledger, the namespace or the
// integration ref must not be mistaken for a scan that found nothing (Rule 15).
func ClassifyArchiveBacklog(repo string) (ArchiveBacklogClass, error) {
	entries, err := archiveNamespaceEntries(repo)
	if err != nil {
		return ArchiveBacklogClass{}, err
	}
	reasons, malformed, err := archiveReasonRecords(repo)
	if err != nil {
		return ArchiveBacklogClass{}, err
	}
	owners, err := archiveBranchOwners()
	if err != nil {
		return ArchiveBacklogClass{}, err
	}
	tips, err := ArchiveEntriesForRepo(repo)
	if err != nil {
		return ArchiveBacklogClass{}, err
	}
	tipOf := make(map[string]string, len(tips))
	for _, t := range tips {
		tipOf[t.Branch] = t.TipSHA
	}

	out := ArchiveBacklogClass{Repo: repo, IntegrationRef: IntegrationClaimTargetRef(repo), Malformed: malformed}
	landEntries := entriesOf(entries, tipOf)
	// The bulk read is asked for the MISS-set alone, and its absence is read
	// carefully: no miss-set means every tip is already in the integration ref
	// (a MEASURED all-landed answer, not a failure), while a miss-set whose read
	// returned nothing IS a failure — and it must be reported, never rendered as
	// "nothing is missing" (Rule 15).
	var probe func(string) []string
	if tips := ArchiveTipsForLandCheck(repo, out.IntegrationRef, landEntries); len(tips) > 0 {
		if probe = ArchiveLandProbe(repo, out.IntegrationRef, tips); probe == nil {
			out.LandErr = archiveLandReadFailedText
		}
	}
	land := ArchiveLandFunc(repo, out.IntegrationRef, probe, landEntries)
	for _, e := range entries {
		o := owners[e.branch]
		out.Rows = append(out.Rows, ArchiveRowClass{
			Branch: e.branch, Tag: e.tag,
			Reason:      ArchiveTagReason(reasons[e.branch]),
			OwnerTaskID: o.taskID, OwnerStatus: o.status,
			Land: land[e.branch],
		})
	}
	sort.Slice(out.Rows, func(i, j int) bool { return out.Rows[i].Branch < out.Rows[j].Branch })
	return out, nil
}

// entriesOf projects the namespace entries into the land check's shape.
func entriesOf(entries []archiveNamespaceEntry, tipOf map[string]string) []ArchiveEntry {
	out := make([]ArchiveEntry, 0, len(entries))
	for _, e := range entries {
		out = append(out, ArchiveEntry{Tag: e.tag, Branch: e.branch, TipSHA: tipOf[e.branch], Subject: e.subject})
	}
	return out
}

// archiveLandReadFailedText is the ONE spelling of "the landing question could
// not be asked" (Rule 1b/14), rendered by the classification report.
const archiveLandReadFailedText = "the branch's added-path read could not be taken"

// ArchiveBacklogClassReport renders the classification for a terminal: the
// per-branch trio, then the four counted sets an operator acts on. A check whose
// output does not name the branches is unreadable; a check whose output does not
// COUNT the classes cannot be watched over time (Rule 5).
//
// The second return is the number of findings the CLI exits non-zero on. The
// re-dispatchable set counts double-weighted: it is the defect, while the other
// three classes are honest states a reader still needs to see.
func ArchiveBacklogClassReport(repo string) (string, int, error) {
	cls, err := ClassifyArchiveBacklog(repo)
	if err != nil {
		return "", 0, err
	}
	var b strings.Builder
	fmt.Fprintf(&b, "archive classification: %s\n", repo)
	if len(cls.Rows) == 0 {
		b.WriteString("  clean — no branch is tagged out of the merge queue\n")
		return b.String(), 0, nil
	}
	fmt.Fprintf(&b, "  ref for the landing question: %s\n", cls.IntegrationRef)
	if cls.LandErr != "" {
		fmt.Fprintf(&b, "  WARNING: %s — every landing verdict below is %s\n", cls.LandErr, ArchiveLandUnknownLabel)
	}
	fmt.Fprintf(&b, "  %d branch(es): %d reason-less, %d decided-owner, %d not-landed, %d decided-and-not-landed\n",
		len(cls.Rows), cls.ReasonLessCount(), cls.countDecided(), cls.countLand(ArchiveLandMissing),
		len(cls.BuriedUnlanded()))
	b.WriteString("  (reason × owner × landing; every branch accounted for)\n")
	for _, r := range cls.Rows {
		fmt.Fprintf(&b, "    %s — %s — %s — %s\n", r.Branch, r.ReasonCell(), r.OwnerCell(), r.LandCell())
		// THE VERDICT'S OWN EVIDENCE sits under the verdict, not only in the
		// re-dispatch section below: a NOT LANDED carried by a branch nobody owns
		// (no row, so not "buried") would otherwise print the word and nothing
		// else. Both arms render here through ONE function (Rule 13).
		b.WriteString(renderArchiveLandEvidence(r.Land, archiveLandEvidenceIndent))
	}
	b.WriteString(cls.renderBuriedUnlanded())
	if n := cls.ReasonLessCount(); n > 0 {
		fmt.Fprintf(&b, "  %d reason-less archive(s): record a reason with\n"+
			"    gopi task archive <branch> --reason <code> [--override-completed]\n", n)
	}
	return b.String(), len(cls.BuriedUnlanded()), nil
}

// renderBuriedUnlanded renders the RE-DISPATCHABLE set — the section an operator
// acts on, and the only one where the report gives a next command per row: the
// namespace's own documented un-archive (removing the tag re-arms the branch),
// which is the ONE revival path (task_merge.go's build-gate element 6 spells the
// same command).
func (b ArchiveBacklogClass) renderBuriedUnlanded() string {
	rows := b.BuriedUnlanded()
	if len(rows) == 0 {
		return "  none buried-and-unlanded: every decided owner row's work is on the integration ref\n"
	}
	var out strings.Builder
	fmt.Fprintf(&out, "  %d decided-but-NOT-LANDED deliverable(s) — the work finished, the branch is out of\n"+
		"  every merge path, and its work is on the integration ref NOWHERE:\n", len(rows))
	for _, r := range rows {
		fmt.Fprintf(&out, "    %s — task %s (%s) — %s\n", r.Branch, r.OwnerTaskID, r.OwnerStatus, r.ReasonCell())
		if len(r.Land.MissingPaths) > 0 {
			fmt.Fprintf(&out, "      absent from %s: %s\n", r.Land.Ref, strings.Join(r.Land.MissingPaths, ", "))
		}
		out.WriteString(renderArchiveLandEvidence(r.Land, archiveLandEvidenceIndent))
		fmt.Fprintf(&out, "      re-dispatch: %s\n", fmt.Sprintf(buildGateReviveCmdFmt, r.Tag))
	}
	out.WriteString("  (re-dispatch = file a task against the buried work, or re-arm the branch by\n" +
		"   removing its archive tag — the TWO routes, neither of which edits the tag's commit)\n")
	return out.String()
}

func (b ArchiveBacklogClass) countDecided() int {
	n := 0
	for _, r := range b.Rows {
		if r.OwnerDecided() {
			n++
		}
	}
	return n
}

func (b ArchiveBacklogClass) countLand(v string) int {
	n := 0
	for _, r := range b.Rows {
		if r.Land.Verdict == v {
			n++
		}
	}
	return n
}
