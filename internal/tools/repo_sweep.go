package tools

// Per-repo merger sweep engine (epic architecture-fix-v2, 2026-09-03,
// nick's simpler config-driven design): the merger is a PURE git sweep
// driven by the settings.json "repos" table — NOT task-oriented, NOT
// DB-driven. It periodically cd's to each configured repo and merges all
// branches whose worktree protection has been removed ("worktree-removed
// branches can be merged" — nick). A branch appears in repo X → the merger
// (which sweeps repo X every tick) merges it when unprotected. Whether a
// task row tracks the branch is irrelevant to the merge DECISION — merging is
// fully decoupled from the task ledger.
//
// ONE TICK = PURE GIT, NO CHILD, NO LLM, NO WAIT (epic merger-ff-park-file,
// 2026-09-16; the PARK arm retired by epic merger-remove-park, 2026-09-17).
// Per configured repo, one sweep turn runs:
//
//	① DISCOVER unprotected merge candidates: local branches whose tip is
//	  NOT already in master AND NOT checked out in a linked worktree (a
//	  checked-out branch is ACTIVE BOND WORK — the "merge-time indicator"
//	  nick's design keys on. git would happily land it because only branch
//	  -d refuses a linked-worktree checkout, so the sweep's own occupancy
//	  guard is load-bearing). Archive-ledger branches (hp-unique-*,
//	  refs/tags/archive/<name>) are exempt, as in every other merge path.
//	② DECIDE each candidate with git ALONE:
//	  - ff-able (merge-base --is-ancestor <branch> master exits 0) →
//	    MERGE, and the branch ref is pruned;
//	  - not ff-able → LEAVE IT ALONE and report it (repoSweepNotFFOutcome).
//	    The tick does NOT tag it, does NOT file a row for it, does NOT touch
//	    its checkout: the branch stays a candidate of every later tick.
//	    Resolution is the take-over path's job — the merger LAUNCHER starts a
//	    gopi whose prompt hands the branch over as a self-contained diff plus
//	    a rework task (the merge briefing's §3.6: never a tag) — the take-over
//	    prompt that carried that section was deleted with the retired daemon
//	    (epic merger-delete-dead-impl, 2026-09-19), and the rule is unchanged.
//	  The tick itself NEVER spawns a child, NEVER runs an LLM and NEVER waits
//	  on another process.
//	③ DISCOVERY-ONLY tidy (epic merger-remove-park's successor, 2026-09-19):
//	  the integration-side recycle that used to run here — MergedCleanupSweep,
//	  which removed a merged pair's worktree and deleted its branch — is
//	  RETIRED from the tick. A mechanism whose job is "clean up what the merger
//	  did not handle" has no legitimate work (every branch merges and is
//	  deleted, or cannot merge and is filed as a task), so on a healthy system
//	  its only observable effect was deleting branches still in use. The tick
//	  now REPORTS every branch whose tip is already integrated, with owner
//	  evidence, and mutates no ref (repo_sweep_report.go).
//
// The tick reads the ledger for ONE thing: the OWNER EVIDENCE of a branch it
// would have pruned (TaskForBranch — the ONE branch→row loader, Rule 14). Every
// other decision is pure git. Fail-soft per repo: any git error degrades to
// that repo's report row, never aborts the turn or the daemon.

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"sort"
	"strings"
	"time"
)

// --- per-repo sweep tunables (Rule 1: every literal in one place) ---

const (
	// repoSweepGitTimeout caps each git invocation (a hung repo must
	// degrade to a skipped row, never wedge the merger tick).
	repoSweepGitTimeout = 30 * time.Second

	// repoSweepMaxBranches caps how many unprotected branches ONE turn
	// merges (a runaway branch factory cannot turn one tick into a fork
	// bomb — every other sweep pass carries the same cap).
	repoSweepMaxBranches = 20

	// refsHeadsPrefix / refsRemotesPrefix are the full ref namespaces a
	// branch name must be qualified with — the form `git rev-parse --verify
	// --quiet` needs (a bare name is ambiguous with a tag of the same name)
	// and the form an ancestry question must ask. Single definitions
	// (Rule 14): the sweep and the D2 terminal guard both resolve a branch
	// ref through these, so a rename cannot leave one of them probing a
	// namespace the other does not use.
	refsHeadsPrefix   = "refs/heads/"
	refsRemotesPrefix = "refs/remotes/"

	// masterBranchName / mainBranchName are the two integration-branch
	// spellings the sweep never treats as a merge candidate (Rule 1b: the
	// class has NO exported home package yet — task_merge.go still carries
	// bare "master" comparisons, so a cross-package constant package is its
	// own backfill task; these two are the sweep's single local source).
	masterBranchName = "master"
	mainBranchName   = "main"

	// protectedSummaryFmt / protectedDeadHolderFmt render the protected arm
	// for an operator: the count, and — separately — the NAMES of the
	// branches a provably dead holder is holding (Rule 1: one spelling, so
	// the merger log, the status preview and the tests agree).
	//
	// The dead-holder suffix deliberately does NOT promise a release. "The
	// holder is dead" is evidence about the HOLDER; whether the branch is
	// actually freed depends on the reclaim arm's guards, and on 2026-09-13
	// the promise was measurably false: 7 branches carried "reclaimable by the
	// finish-cleanup sweep" for over an hour while 6 were DIRTY (the arm
	// refuses uncommitted work by design) and the arm was reachable only from
	// a worker pass that nothing on the host ran. The suffix now states the
	// fact and points at the per-branch verdict (protectedHeldNoteFmt).
	protectedSummaryFmt    = "protected=%d"
	protectedDeadHolderFmt = " (dead holder: %d [%s] — the holder is gone, the worktree still blocks the merge)"

	// protectedHeldNoteFmt names the dead-holder branches the reclaim arm will
	// NOT free this pass, each with its reason. It is the honest-verdict
	// companion of protectedDeadHolderFmt: without it an operator reads a
	// withheld deliverable as "about to be released".
	protectedHeldNoteFmt = " (still refused: %d [%s])"

	// protectedAttributionFmt names the WORKTREE-PROTECTED branches the BRANCH
	// axis attributes to an unfinished ledger row, with the owning task id. It
	// is the third companion of the two above and it answers a question neither
	// of them can: DeadHolder/Held both key on a row that named a WORKTREE and
	// whose holder is provably dead, so an in-flight row whose claim pid is
	// already gone (measured live: row 01a0a996) is invisible to both — while
	// git still refuses its branch to the merger. Named, with the task id, so
	// the operator reads WHO to resume instead of a silent protected count.
	protectedAttributionFmt = " (attributed: %d [%s])"

	// protectedAttributionErrFmt is the honest "not measured" suffix: the
	// branch axis was not answered, and the line says which read failed rather
	// than presenting an empty list as a clean bill of health.
	protectedAttributionErrFmt = " (branch attribution UNAVAILABLE: %s)"

	// archivedSummaryFmt / archivedBuriedFmt render the ARCHIVED arm — the
	// decided-exclusion set the merger skips silently (repoSweepArchiveExempt).
	// Two constants, not one: the count answers "how much is out of the queue",
	// and the buried list answers the question an operator actually acts on —
	// "which of those branches belongs to a task the ledger calls FINISHED",
	// i.e. a deliverable that nothing else will ever mention again (fault 4 of
	// the 2026-09-16 audit: no surface listed the archived set at all).
	archivedSummaryFmt = " archived=%d"
	archivedBuriedFmt  = " (BURIED: %d [%s])"

	// branchInventoryNameMax caps how many branch NAMES one bucket lists in the
	// reconciliation line (Rule 1: named, one place). The COUNTS are always
	// exact; only a name list is truncated, and the truncation is stated.
	branchInventoryNameMax = 20

	// branchInventorySummaryFmt renders the RECONCILIATION line: every local
	// head the repo carries, split into the buckets the sweep itself uses. It
	// exists because the previous output was unreconcilable — an operator read
	// `git branch` (N heads), then "protected: 10 / pending: 1", and could
	// account for 11 of N. The archive-tagged decided losers, the hp-unique-*
	// ledger namespace and master itself were dropped silently, so the merger
	// looked like it was ignoring branches it had in fact already decided on
	// (bug 01a0aa74). Column order matches the summary line's arithmetic.
	branchInventorySummaryFmt = "  branches:  %d local head(s) = %d integration + %d worktree-held + %d archive-tagged + %d exempt-namespace + %d deferred + %d pending\n"

	// branchInventoryBucketFmt renders one bucket's NAME list — "which branch
	// went where", the operator's answer to "why does it ignore so many".
	branchInventoryBucketFmt = "    %s (%d): %s%s\n"

	// branchInventoryTruncatedSuffix is appended to a capped name list, so a
	// truncated list never reads as the complete set (Rule 15).
	branchInventoryTruncatedSuffix = " …"

	// branchInventorySpentScanMax bounds the read-only status preview's
	// already-merged probe (one `merge-base --is-ancestor` per candidate). It
	// is a cost ceiling for a command an operator runs by hand — never a
	// semantic limit on the sweep, which asks the same question per branch at
	// merge time with no cap.
	branchInventorySpentScanMax = 50
)

// Branch-inventory bucket labels (Rule 1b: the summary line, the bucket name
// lists and the tests spell each bucket from ONE definition).
const (
	bucketLabelWorktree = "worktree-held"
	bucketLabelArchived = "archive-tagged"
	bucketLabelExempt   = "exempt-namespace"
	bucketLabelDeferred = "deferred"
	bucketLabelSpent    = "already-in-master"

	// worktreeHeldLabelNote is the honest provenance of the worktree-held arm:
	// the set comes from git worktree REGISTRATION (`git worktree list`), a
	// fact about the CHECKOUT. It used to end with "holder liveness is not
	// measured here" — true of this line, and directly CONTRADICTED a few lines
	// below by a per-branch sentence asserting "a live run is working in its
	// checkout" (the operator read both and could not tell which to believe).
	// The contradiction was resolved by MAKING the per-branch sentence true:
	// checkout occupancy is measured once, on the branch-attribution axis, and
	// every sentence about a checkout now names that measurement. This label
	// therefore states only what this line is made of — a registration — and the
	// measured picture is rendered per branch by the candidate block.
	worktreeHeldLabelNote = " — git worktree list (a registration, not a liveness measurement)"

	// archivedBucketNote names WHY an archive-tagged branch is not in the
	// merge queue: the tag IS the decision (every merge path exempts it).
	archivedBucketNote = " — decided losers (archive tag): never merged, never deleted"

	// exemptBucketNote: the hp-unique-* archive-ledger namespace.
	exemptBucketNote = " — archive ledger: never merged or deleted"

	// deferredBucketNote: the caller owns these for this tick.
	deferredBucketNote = " — owned by the caller this tick"

	// spentBucketNote: candidates whose tip is already in the integration branch —
	// the sweep REPORTS them now, it never prunes the ref (epic
	// sweep-report-not-act, 2026-09-19).
	spentBucketNote = " — already integrated: the sweep reports, it never prunes"
)

// BranchInventory is the RECONCILING view of a repo's local heads: EVERY
// branch `git branch` prints lands in exactly one bucket, and the buckets sum
// to Total. The sweep itself only needs the candidate list; this is the
// operator's answer to "git branch shows 20, the merger accounts for 11 — what
// happened to the other 9" (bug 01a0aa74).
//
// The invariant (pinned by TestBranchInventory_PartitionsEveryLocalHead):
//
//	Total == Integration + Protected + Archived + Exempt + Deferred + Candidate + Capped
//
// Spent is a SUB-count of Candidate (those refs are pruned, not merged), so it
// deliberately never participates in the sum.
type BranchInventory struct {
	Total       int // every local head in the repo, exactly once
	Integration int // master/main — never a merge candidate
	Protected   int // checked out in a REGISTERED worktree — ACTIVE BOND WORK
	Archived    int // refs/tags/archive/<name> — a DECIDED LOSER, never swept
	Exempt      int // hp-unique-* ledger namespace — never merged or deleted
	Deferred    int // excluded by the caller for this tick (merger-verify-gate)
	Candidate   int // the sweep would act on it (merge, or prune if spent)
	Capped      int // a candidate beyond the per-tick cap — acted on a later tick
	Spent       int // of Candidate: already in master

	WorktreeNames []string
	ArchivedNames []string
	ExemptNames   []string
	DeferredNames []string
	SpentNames    []string
}

// addInventoryName records a bucket member, capped so a runaway branch factory
// cannot flood a terminal (the COUNT stays exact regardless).
func addInventoryName(dst *[]string, name string) {
	if len(*dst) >= branchInventoryNameMax {
		return
	}
	*dst = append(*dst, name)
}

// inventoryNameList renders one bucket's names with an honest truncation
// marker, so a capped list never reads as the whole set.
func inventoryNameList(names []string, total int) string {
	if len(names) == 0 {
		return ""
	}
	s := strings.Join(names, ", ")
	if total > len(names) {
		s += branchInventoryTruncatedSuffix
	}
	return s
}

// BucketLines renders the per-bucket NAME lists — which branch went where.
// Empty buckets are omitted; each names itself from the one label set above.
// protectedNote is appended to the worktree-held bucket: that is where the
// dead-holder/refusal verdict belongs (DeadHolder is always a subset of the
// worktree-held branches), so the protected arm is annotated on ONE line
// instead of being stated twice.
func (inv BranchInventory) BucketLines(protectedNote string) string {
	var b strings.Builder
	line := func(label string, names []string, total int, note string) {
		if total == 0 {
			return
		}
		fmt.Fprintf(&b, branchInventoryBucketFmt, label, total, inventoryNameList(names, total), note)
	}
	// The worktree bucket is the ONE bucket whose visibility also depends on
	// the caller's note, not only on its own count: the dead-holder names
	// arrive as protectedNote, and gating them on a count alone is how a NAMED
	// branch silently disappears from the operator line (bug 01a0aa74's exact
	// shape — a bare count where a branch name belongs). Every other bucket's
	// note is the constant label note, non-empty by construction, so the plain
	// count gate is correct there.
	if inv.Protected != 0 || protectedNote != "" {
		fmt.Fprintf(&b, branchInventoryBucketFmt, bucketLabelWorktree, inv.Protected,
			inventoryNameList(inv.WorktreeNames, inv.Protected), worktreeHeldLabelNote+protectedNote)
	}
	line(bucketLabelArchived, inv.ArchivedNames, inv.Archived, archivedBucketNote)
	line(bucketLabelExempt, inv.ExemptNames, inv.Exempt, exemptBucketNote)
	line(bucketLabelDeferred, inv.DeferredNames, inv.Deferred, deferredBucketNote)
	line(bucketLabelSpent, inv.SpentNames, inv.Spent, spentBucketNote)
	return b.String()
}

// SummaryLine renders the reconciliation arithmetic as ONE line, so an operator
// can add `git branch`'s heads up against the merger's own view of the repo.
func (inv BranchInventory) SummaryLine() string {
	return fmt.Sprintf(branchInventorySummaryFmt,
		inv.Total, inv.Integration, inv.Protected, inv.Archived,
		inv.Exempt, inv.Deferred, inv.Candidate)
}

// Merge-outcome actions that mean a branch LANDED this round (see
// TaskMergeBranchOutcome.Action). Named constants: the version-tag mint and
// the merge engine must agree on the spelling (Rule 1b) — a bare "merged"
// here and a renamed literal there would silently record an empty branch
// list in every version tag message.
const (
	mergeActionMerged  = "merged"
	mergeActionDeleted = "deleted"
)

// The non-fast-forward lane's spellings (Rule 1b: one place). The tick no
// longer ACTS on a branch it cannot land — it REPORTS it — so the vocabulary is
// a reason line plus one outcome action and nothing more: no park tag, no
// hand-over row, no worktree release (see repoSweepNotFFOutcome).
const (
	// notFFMergeBaseVerb / notFFIsAncestorFlag are the probe the reason line
	// names, so the operator's next command is spelled by the same constants the
	// tick runs.
	notFFMergeBaseVerb  = "merge-base"
	notFFIsAncestorFlag = "--is-ancestor"

	// repoSweepActionNotFF is the per-branch outcome for a branch the tick
	// cannot land. The branch is neither excluded nor modified: it stays a
	// candidate of every later tick, and the merge path (the foreground
	// `gopi task merge`, whose hand-over is the branch-rework row the engine
	// files — reworkLandingProbe / task_rework_successor carry the shape; the
	// retired `gopi merge` verb's merge_prompt.md is gone, epic merge-removal
	// 2026-11-01) is what hands its diff over as a rework task.
	repoSweepActionNotFF = "not-ff"

	// repoSweepActionError is the tick's outcome for a branch whose decision
	// could NOT be made (a git call failed). The branch keeps its place as a
	// candidate for the next tick and master is untouched; the operator sees it
	// as an error, never as a landed branch (Rule 15).
	repoSweepActionError = "error"
)

// repoSweepIntegrationBranchFn resolves the branch the per-branch verdicts ask
// git about ("is this tip already integrated?"). It is a SEAM, not a tunable of
// production: the tick is a function of the repo it is pointed at, and a
// fixture repo whose integration branch carries another name must be able to
// ask the same question production asks (the repo_sweep tests build their own
// throwaway repos). Production never reassigns it — the repo's own default
// stands, which is masterBranchName.
var repoSweepIntegrationBranchFn = func() string { return masterBranchName }

// repoSweepNotFFReason is the Detail a branch the tick could not land carries —
// the operator's one-line answer to "why is this branch not merged", plus the
// one command that re-asks the question the tick asked (Rule 1b: the reason and
// the probe are spelled from the same constants).
func repoSweepNotFFReason(branch string) string {
	return "cannot fast-forward into " + masterBranchName + " (" + notFFMergeBaseVerb + " " +
		notFFIsAncestorFlag + " " + branch + " " + masterBranchName + " says no) — left untouched: not tagged, " +
		"no rework row filed here; the merge command hands its diff over"
}

// repoSweepNotFFOutcome is the tick's WHOLE answer to a non-fast-forwardable
// branch: REPORT it and change NOTHING.
//
// WHY NOTHING (epic merger-remove-park, 2026-09-17). The retired park lane
// answered this same question with three side effects — an archive/<branch> tag
// (which permanently removed the branch from every merge candidate set via
// repoSweepArchiveExempt), one ledger row, and a forced worktree removal. Each
// contradicted the hand-over design (the merge briefing's §3.6),
// which requires the branch to STAY: the branch ref is what the hand-over diffs
// against, and that prompt's "archive tags are history, never a filter" rule is
// the same decision stated from the merger's side. The three side effects also
// emptied the candidate set (159 live archive tags on the GoPi repo, measured
// 2026-09-17), manufactured a junk row per tick, and destroyed a checkout
// nothing had asked to destroy.
//
// WHAT REMAINS IS THE HONEST ONE: the branch keeps its ref, its commits, its
// worktree and its place in the candidate set; the tick reports the refusal so
// an operator (or the launcher's take-over gopi) can see it. Re-reporting the
// same branch on every tick is deliberate — it is the only signal that the
// branch is still waiting, and it costs one git probe per branch.
func repoSweepNotFFOutcome(branch string) (TaskMergeBranchOutcome, string) {
	return TaskMergeBranchOutcome{Branch: branch, Action: repoSweepActionNotFF,
		Detail: repoSweepNotFFReason(branch)}, repoSweepActionNotFF
}

// MergedBranchNames extracts the branches a merge round LANDED from its
// per-branch outcomes, deduped and sorted. It feeds the version tag message
// (epic "gopi version must be meaningful"): the tag names the position, the
// message names what got it there. Exported because EVERY face that lands
// branches names the position it landed — the merger sweep's own result and
// the manual `gopi task merge` face both call this, so the two can never
// disagree about what "landed" means (Rule 13/14).
func MergedBranchNames(outcomes []TaskMergeBranchOutcome) []string {
	seen := make(map[string]bool)
	var names []string
	for _, o := range outcomes {
		if o.Action != mergeActionMerged && o.Action != mergeActionDeleted {
			continue
		}
		if o.Branch == "" || seen[o.Branch] {
			continue
		}
		seen[o.Branch] = true
		names = append(names, o.Branch)
	}
	sort.Strings(names)
	return names
}

// RepoSweepResult is one configured repo's sweep-turn outcome.
type RepoSweepResult struct {
	Repo   string
	Merged int // branches landed by fast-forward (the branch ref is REPORTED, never deleted)
	// MergedBranches names the branches this tick LANDED (action merged),
	// sorted. The merger's version-tag mint records them in the
	// tag MESSAGE so `git show <tag>` answers "what does this binary
	// entail" — the epic's promise. Named, never a count: a count cannot
	// answer that question. Pruned branches no longer join this list: a prune
	// is not a landing (epic sweep-report-not-act, 2026-09-19).
	MergedBranches []string
	// NotFF counts the branches this tick could NOT land (they do not
	// fast-forward into master). They are REPORTED ONLY: no tag, no ledger row,
	// no worktree touched — the branch keeps its place in the next tick's
	// candidate set and the merge path owns the hand-over
	// (repoSweepNotFFOutcome explains the removal of the park lane's three side
	// effects, epic merger-remove-park 2026-09-17).
	NotFF int

	// RefGone counts the candidates whose ref had been DELETED between the
	// discovery snapshot and the per-branch decision (repo_sweep_ref_guard.go).
	// Each one was skipped before any merge-time gate was asked, and every live
	// ledger row still claiming it received a `ref-gone` event on its own audit
	// trail. The count is separate from NotFF and from Failed on purpose: a
	// disappearance is neither a landing decision nor a git error, and folding
	// it into either would be the silent swallow this arm exists to prevent.
	RefGone int

	// Checkout is the main-checkout restore's OWN sentence for this tick: "" when
	// the checkout was already on the integration branch, the repair line when it
	// was parked on another branch and was moved back, and the refusal line when
	// it was parked and could not be moved (dirty tree, detached HEAD, unreadable).
	// It is reported for EVERY tick that had candidates, not only for a landing:
	// the parked state is exactly what makes the next landing wrong, so it must
	// reach the operator before the damage, and the same sentence rides the
	// `gopi task merge` run's report (Rule 15 — a state this tick found is never
	// swallowed because it did not have to act on it).
	Checkout string

	// Deleted is RETIRED (epic sweep-report-not-act, 2026-09-19): the tick no
	// longer prunes branches, so the field stays declared at its zero value for
	// the faces that still print a deleted tally. A prune today is a REPORT.
	Deleted int
	// Reported counts the branches this tick would have pruned: git answered
	// "the tip is already in the integration branch" and the tick REPORTED the
	// verdict with owner evidence instead of deleting the ref.
	Reported int
	// WouldPruneLive counts the reported branches a LIVE ledger row still names
	// — the incident's exact shape, each one separately alarmed on the notify
	// channel (repo_sweep_report.go).
	WouldPruneLive int
	// WouldPrune carries the full report entries (branch, tip, verdict, owner
	// evidence) so a caller can render or inspect what the tick would have
	// pruned (RepoSweepPruneLines is the ONE renderer).
	WouldPrune []RepoSweepPruneReport
	Protected  int // worktree-checked-out branches left untouched (active work)
	// DeadHolder names the protected branches a PROVABLY DEAD holder still
	// occupies (see RepoSweepPreview.DeadHolder) — reported so the merger
	// log never shows a silent protected count.
	DeadHolder []string
	// Held is the honest-verdict half (RepoSweepPreview.Held): the dead-holder
	// branches the reclaim arm will NOT free, with the reason.
	Held map[string]string
	// Attribution is the branch-axis picture (RepoSweepPreview.Attribution),
	// carried on the tick so the merger log names every branch an unfinished
	// row still owns — the rows the dead-holder arms are structurally blind to.
	Attribution    []BranchAttributionLine
	AttributionErr string
	// DirtySalvage carries one line per dirty dead-holder branch the salvage
	// arm acted on (a row filed) or refused to file (no ledger row, capture
	// failed, kill switch off) — an empty slice is the ordinary "nothing in
	// that state" tick. Named lines, never a bare count: the operator must be
	// able to see WHICH work was handed over.
	DirtySalvage []string
	Outcomes     []TaskMergeBranchOutcome // the per-candidate merge report
	// ContractRefused counts branches the merge-time CLI contract gate
	// refused this sweep: the branch newly wires a main.go dispatch
	// predicate its cli_contracts_test.go does not track, so landing it
	// would put a red test on master (bd3d34a5 voice_input). The refusal
	// happens BEFORE any `git merge` runs, so master is untouched; the
	// branch is routed to the branch-rework lane (ReworkSubmitted counts
	// that submit).
	ContractRefused int
	// ReworkSubmitted counts branch-rework fix tasks enqueued this tick (the
	// contract/builder refusal lane). The branch's land/not-ff outcome is
	// independent of it: a refused branch is neither merged nor reported here.
	ReworkSubmitted int
	// BuildRefused counts branches the merge-time BUILD gate refused this
	// sweep: the branch does not compile while the merge target does, so
	// landing it would stop master building (2026-09-13, 82b05c4f's ten
	// undefined symbols landed by 82b3b6f3 with no git conflict). Master is
	// untouched by construction; unlike a conflict this branch is NOT
	// reconcilable — see BuildArchived.
	BuildRefused int
	// BuildArchived counts the refused branches ARCHIVED in the same tick
	// (refs/tags/archive/<branch>): the branch is broken, not in conflict,
	// so it is out of the merge queue permanently and tagged so a human can
	// still mine the patch (and revive it with `git tag -d`). A branch whose
	// archive FAILED (a git error) stays a candidate for a later tick.
	BuildArchived int
	Err           string // fatal-ish (non-repo / git failure) — skip, don't sweep further
}

// MergerRepoSweepOptions configures one MergerRepoSweep turn. Every field is
// a caller-side STATEMENT about which branches to leave alone; none of them
// can make the tick spawn a child, run an LLM or wait on anything.
type MergerRepoSweepOptions struct {
	// ExcludeBranches lists branches this turn must NOT merge or report: the
	// caller owns them for this tick. Empty (the default) changes nothing. A
	// branch that is excluded AND protected is simply skipped; the exclusion
	// never overrides git safety.
	ExcludeBranches []string
}

// repoSweepGit runs one git command in repo with the shared timeout.
func repoSweepGit(repo string, args ...string) (string, error) {
	return repoSweepGitInput(repo, "", args...)
}

// repoSweepGitStdin is repoSweepGit with input written to git's stdin —
// required by the commands that take their payload there (`hash-object -w
// --stdin`, how a reason record becomes a blob without ever passing free text
// through argv). Same timeout, same error shape: ONE runner, so a caller
// cannot silently get a different git environment by picking the other helper.
func repoSweepGitStdin(repo, input string, args ...string) (string, error) {
	return repoSweepGitInput(repo, input, args...)
}

// repoSweepGitInput is the ONE git runner behind both helpers above.
func repoSweepGitInput(repo, input string, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), repoSweepGitTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, "git", append([]string{"-C", repo}, args...)...)
	// THE LOCALE IS PINNED for every git child this runner spawns. Its callers
	// include parsers that match the ENGLISH text of git's diagnostics (the
	// unreachable census's `unreachable` prefix, the archive namespace scans),
	// and git translates those diagnostics per the ambient locale — so the
	// language they arrive in is part of the contract, not a host preference.
	// Root cause and the measured zh_CN divergence: git_child_locale.go.
	cmd.Env = ChildLocaleEnv()
	var stderr strings.Builder
	cmd.Stderr = &stderr
	if input != "" {
		cmd.Stdin = strings.NewReader(input)
	}
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("git %s: %w: %s", strings.Join(args, " "),
			err, strings.TrimSpace(stderr.String()))
	}
	return strings.TrimSpace(string(out)), nil
}

// repoSweepProtectedBranches returns the set of branch names currently
// checked out in a LINKED worktree of repo. The main checkout (master) is
// excluded by mergedCleanupWorktrees — master lives there and is never a
// merge candidate anyway. Any returned branch is ACTIVE BOND WORK the
// merger must never sweep.
//
// A REGISTRATION whose directory is GONE is not a checkout, so it does not
// protect its branch (epic re-measure-park-worktree-live-row-guard). git
// keeps listing the entry and annotates it worktreePorcelainPrunable until
// `git worktree prune` drops it — measured on this host: the entry survives
// `rm -rf <wt>` and `git worktree remove --force <wt>` refuses it outright
// (exit 128, "is not a working tree"). Nothing can be working in a directory
// that does not exist, so withholding the branch was a PHANTOM occupancy:
// this arm held a branch out of every merge round for the lifetime of a
// leftover registration, and its dead-holder annotation reported the branch
// as held (DeadHolderBranches/ReclaimHeldBranches read the same registry).
// Master is excluded upstream, so the main checkout is never affected.
func repoSweepProtectedBranches(repo string) (map[string]bool, error) {
	wtByBranch, _, err := mergedCleanupWorktrees(repo)
	if err != nil {
		return nil, err
	}
	protected := make(map[string]bool, len(wtByBranch))
	for b, path := range wtByBranch {
		if repoSweepStaleWorktree(repo, path) {
			continue
		}
		protected[b] = true
	}
	return protected, nil
}

// repoSweepStaleWorktree reports whether path's registration is a leftover:
// its directory is gone AND git itself has stopped describing a checkout
// there. A missing directory alone is not proof (a checkout can be mid-move),
// and a registration alone is never evidence of occupancy — so this asks
// git's OWN registry and requires its marker. An unreadable registry, or a
// path git still lists without the marker, KEEPS the branch protected: that
// conservative half costs one merge round, while the other half would cost a
// live run its ground.
//
// The marker's spelling is the shared one (worktreePorcelainPrunable ←
// finishCleanupPorcelainPrunableMark, Rule 14).
func repoSweepStaleWorktree(repo, path string) bool {
	if path == "" {
		return false
	}
	if _, err := os.Stat(path); err == nil {
		return false // the checkout exists — a live bond's ground, never stale
	}
	out, err := repoSweepGit(repo, finishCleanupWorktreeVerb,
		finishCleanupWorktreeListVerb, finishCleanupWorktreeListFlag)
	if err != nil {
		return false // registry unreadable: keep the branch protected
	}
	cur := ""
	for _, raw := range strings.Split(out, "\n") {
		line := strings.TrimSpace(raw)
		if strings.HasPrefix(line, finishCleanupPorcelainWorktreePrefix) {
			cur = strings.TrimSpace(strings.TrimPrefix(line, finishCleanupPorcelainWorktreePrefix))
			continue
		}
		if cur != path {
			continue
		}
		if strings.HasPrefix(line, worktreePorcelainPrunable) {
			return true
		}
	}
	return false
}

// repoSweepArchiveExempt mirrors the exemption every merge/cleanup path
// shares: an archive-tagged branch (refs/tags/archive/<name>) is a DECIDED
// LOSER — never merged or deleted (the finish-cleanup exemption).
func repoSweepArchiveExempt(repo, branch string) bool {
	out, err := repoSweepGit(repo, "tag", "-l", archiveBranchPrefix+branch)
	if err != nil {
		return false
	}
	// git tag -l with no matches prints nothing.
	return strings.TrimSpace(out) != ""
}

// mergerStatusBranchListMax bounds how many candidate branch names the
// read-only status preview lists (Rule 1: named, one place — the preview has
// no fork-bomb risk, but an unbounded list would still flood an operator's
// terminal on a runaway branch factory).
const mergerStatusBranchListMax = 200

// repoSweepUnprotectedBranches is the ONE discovery loop every merge-candidate
// consumer shares: list local heads, then drop master/main, the hp-unique-*
// namespace, worktree-protected branches (ACTIVE BOND WORK — never touched),
// archive-tagged decided losers, and caller-excluded branches. It returns the
// candidates (sorted, capped at scanMax) plus the protected-branch count, so
// the merger's own sweep and the read-only status surface can never disagree
// about what is mergeable (Rule 13: one source of truth).
//
// The returned slice is a PREVIEW of intent, never a promise: the caller must
// still let git arbitrate each merge.
func repoSweepUnprotectedBranches(repo string, scanMax int, excluded map[string]bool) ([]string, int, error) {
	candidates, inv, err := repoSweepClassify(repo, scanMax, excluded)
	return candidates, inv.Protected, err
}

// repoSweepClassify is the ONE classification loop behind both the sweep and
// the status preview (Rule 13: one source of truth). It walks every local head
// EXACTLY ONCE and drops it into exactly one bucket, so the buckets always sum
// to the repo's head count — an operator can reconcile `git branch` against
// this output instead of wondering which branches the merger is ignoring
// (bug 01a0aa74: the archive-tagged, exempt and integration heads were dropped
// with no trace, so 9 of 20 branches were unaccounted for).
//
// The returned inventory's names are capped (branchInventoryNameMax) for
// terminal sanity; the counts are exact. The CANDIDATE slice keeps the
// historical semantics: sorted, capped at scanMax, and a PREVIEW of intent —
// the caller must still let git arbitrate each merge.
func repoSweepClassify(repo string, scanMax int, excluded map[string]bool) ([]string, BranchInventory, error) {
	if scanMax <= 0 {
		scanMax = repoSweepMaxBranches
	}
	inv := BranchInventory{}
	protected, err := repoSweepProtectedBranches(repo)
	if err != nil {
		return nil, inv, fmt.Errorf("worktree probe: %w", err)
	}
	names, err := repoSweepGit(repo, "for-each-ref", "--format=%(refname:short)", refsHeadsPrefix)
	if err != nil {
		return nil, inv, fmt.Errorf("branch list: %w", err)
	}
	// Worktree-protected = ACTIVE BOND WORK — never touch. An already-merged
	// UNPROTECTED branch stays in the candidate list: TaskMerge's per-branch
	// ancestry check (①) prunes it with branch -d, exactly the merge-time
	// delete nick's design calls for.
	var mergeable []string
	for _, name := range strings.Fields(names) {
		name = strings.TrimSpace(name)
		if name == "" {
			continue
		}
		// Every head is counted BEFORE any bucket test, so the sum invariant
		// holds by construction rather than by review.
		inv.Total++
		switch {
		case name == masterBranchName || name == mainBranchName:
			inv.Integration++
		case protected[name]:
			inv.Protected++
			addInventoryName(&inv.WorktreeNames, name)
		case taskMergeExempt(name):
			// Archive-ledger namespace (hp-unique-*): never merged or
			// deleted — shared with TaskMerge via its ONE predicate.
			inv.Exempt++
			addInventoryName(&inv.ExemptNames, name)
		case repoSweepArchiveExempt(repo, name):
			// A decided loser: the archive tag IS the decision, and every
			// merge/cleanup path exempts it. It is reported, never silent.
			inv.Archived++
			addInventoryName(&inv.ArchivedNames, name)
		case excluded[name]:
			// Gated by the caller (merger-verify-gate): it merges + judges
			// this branch AFTER the sweep. Never a candidate here.
			inv.Deferred++
			addInventoryName(&inv.DeferredNames, name)
		default:
			// A real candidate. Beyond the per-tick cap it is still ACCOUNTED
			// FOR (Capped) — the loop never breaks, or the later heads would
			// vanish from the sum exactly as the archive-tagged ones did.
			if len(mergeable) >= scanMax {
				inv.Capped++
				continue
			}
			mergeable = append(mergeable, name)
		}
	}
	sort.Strings(mergeable)
	inv.Candidate = len(mergeable)
	inv.Spent, inv.SpentNames = repoSweepSpentCandidates(repo, mergeable)
	return mergeable, inv, nil
}

// repoSweepSpentCandidates splits the candidate list into "spent" (the tip is
// already in the integration branch, so the sweep REPORTS the ref rather than
// merging it) and the rest. Without this the status line claimed every pending
// branch was one "the next sweep would merge" — an overstatement for a spent
// ref, and the second half of the counting complaint in bug 01a0aa74.
//
// The verdict is unchanged; only the disposition is (report, never prune).
//
// The scan is bounded (branchInventorySpentScanMax) and reports UNMEASURED as
// zero rather than guessing: a budget of ancestry invocations is a read-only
// command's cost ceiling, not a semantic limit.
func repoSweepSpentCandidates(repo string, candidates []string) (int, []string) {
	spent := 0
	var names []string
	for i, b := range candidates {
		if i >= branchInventorySpentScanMax {
			break
		}
		if _, err := repoSweepGit(repo, notFFMergeBaseVerb, notFFIsAncestorFlag, b, repoSweepIntegrationBranchFn()); err == nil {
			spent++
			addInventoryName(&names, b)
		}
	}
	return spent, names
}

// RepoSweepPreview is MergerRepoSweep's DISCOVERY HALF, exported read-only:
// exactly the branches the sweep would attempt to merge this turn, plus the
// counts an operator needs to see why. It runs NO merge, NO delete, NO
// rebuild and never takes the singleton lock — it is the ground truth
// the `gopi task merge` sweep's own candidate resolution reads (which branch is
// waiting, which is held by a live worktree) via taskMergeSweepCandidates; the
// `gopi merger --status` page this used to name went with the retired daemon
// (epic merger-delete-dead-impl).
type RepoSweepPreview struct {
	Repo       string
	Candidates []string // unprotected, non-exempt branches the sweep would consider
	Protected  int      // branches held by a live worktree (never swept)
	// DeadHolder names the protected branches a PROVABLY DEAD holder still
	// occupies (a terminal row whose claim pid is gone — see
	// DeadHolderBranches). They stay in Protected (the sweep still must not
	// merge a checkout it does not own), but they are no longer anonymous:
	// a silent protected count is what hid seven finished deliverables from
	// the merger. The MERGER pass's dead-holder reclaim leg is what frees
	// them (mergeDeadHolderReclaimLeg — it was the finish-cleanup sweep's leg
	// until that mechanism was retired in 58b5618b).
	DeadHolder []string
	// Held is the honest-verdict half: the dead-holder branches the reclaim
	// arm will NOT free on the next pass, each mapped to WHY (ReclaimHeldBranches)
	// — a dirty worktree above all, which the arm refuses by design. Without
	// it the DeadHolder list reads as a promise of release.
	Held map[string]string
	// Attribution is the BRANCH-axis answer (branch_attribution.go): for every
	// branch the merger could act on, WHO owns it — or that nobody does. It is
	// the axis DeadHolder/Held cannot speak: those two are keyed on a row that
	// named a WORKTREE and whose holder is provably dead, so an in-flight row
	// whose claim pid is already gone (measured live: row 01a0a996, status
	// in_progress, claim pid dead) appears in NEITHER — the merger's two
	// existing annotations are both silent about the branch somebody is still
	// carrying. It is REPORTING only: no arm's veto is relaxed by it. It is a
	// PEER of Inventory, not a substitute: Inventory reconciles the local heads
	// (bug 01a0aa74), this names the owner of each withheld one.
	Attribution []BranchAttributionLine
	// AttributionErr says the branch axis could NOT be answered (unreadable
	// ledger or repo). Kept beside Err for the same reason: an empty list must
	// never be read as "no branch is attributed" when the question was never
	// asked (Rule 15).
	AttributionErr string
	// WithheldInMaster marks the ATTRIBUTED branches already inside master
	// (repoSweepWithheldInMaster). The ordinary spent scan cannot see them: it
	// walks the candidates, and a worktree-held branch never becomes one.
	WithheldInMaster map[string]bool
	// Inventory reconciles EVERY local head into one named bucket, so an
	// operator can add `git branch` up against this output (bug 01a0aa74: the
	// arm's counts could not be reconciled, so the merger looked like it was
	// ignoring branches it had in fact already decided on).
	Inventory BranchInventory
	// Archived is the ARCHIVED SET: every archive-tagged branch of this repo,
	// with its owning ledger row and the reason its record carries. An archive
	// tag exempts a branch from every merge path (repoSweepArchiveExempt) and no
	// surface used to list them — fault 4 of the 2026-09-16 audit, which left 49
	// buried deliverables visible in neither `merger --status` nor `task list`.
	// REPORTING only: nothing in the sweep reads it back.
	Archived []ArchiveBacklogRow
	// ArchivedErr says the archived set could NOT be listed. It is kept beside
	// Err for the same reason AttributionErr is: "nothing is archived" and "the
	// listing failed" must never render the same way (Rule 15) — a repo whose
	// archive listing fails is exactly the repo where a buried deliverable hides.
	ArchivedErr string
	Err         string // non-repo / git failure — reported, never fatal
}

// DescribeArchived renders the operator/merger line for the archived arm alone
// — the count, the NAMES of the buried branches (archived while their owning row
// says the work finished) and the honest suffix when the listing failed. It is
// appended to the protected line so both halves of "what is NOT moving through
// the merger" are read together, and it renders on its own when nothing is
// worktree-protected (a repo whose only pending work is archive-tagged).
func (p RepoSweepPreview) DescribeArchived() string {
	if p.ArchivedErr != "" {
		return archivedErrNote(p.ArchivedErr)
	}
	if len(p.Archived) == 0 {
		return "" // nothing archived — the absence of the arm IS the answer
	}
	out := fmt.Sprintf(archivedSummaryFmt, len(p.Archived))
	buried := archiveBuriedRows(p.Archived)
	if len(buried) == 0 {
		return out
	}
	return out + fmt.Sprintf(archivedBuriedFmt, len(buried), strings.Join(buried, ", "))
}

// archiveBuriedRows renders the buried entries of the archived set: branch —
// owning row (status) — for exactly the entries whose owner reached a DECIDED
// status. The predicate is archiveOwnerDecided, shared with the refusal gate and
// the backlog check, so the line and the gate can never disagree about what
// "buried" means (Rule 13).
func archiveBuriedRows(recs []ArchiveBacklogRow) []string {
	var out []string
	for _, r := range recs {
		if !archiveOwnerDecided(r.OwnerStatus) {
			continue
		}
		out = append(out, r.Branch+" — "+r.OwnerTaskID+" ("+r.OwnerStatus+")")
	}
	return out
}

// archivedErrNote renders the "the archived set could not be listed" suffix —
// ONE spelling (Rule 14). Without it a failed listing reads as "nothing is
// archived", which is exactly how a buried deliverable stays hidden.
func archivedErrNote(err string) string {
	return " (archived: listing FAILED — " + err + ")"
}

// DescribeProtected renders the operator/merger line for the protected arm:
// the plain count, plus the NAMES of the branches a dead holder is holding —
// the difference between "protected=7" and "protected=7 (dead holder: x, y)".
//
// The ARCHIVED arm rides along (DescribeArchived): an archive-tagged branch is
// exempt from every merge path, so "what is not moving through the merger" has
// two halves and the archived half must render even when nothing is
// worktree-protected. The two are separate renderers because they are separate
// questions, composed here so the operator reads them together.
func (p RepoSweepPreview) DescribeProtected() string {
	// The guard keys on the NAMES as well as the count: a preview carrying
	// dead-holder or held branches must never be silenced by a zero count —
	// the entire point of this line is that the branch names are shown
	// (bug 01a0aa74: an anonymous count hid seven deliverable branches).
	archived := p.DescribeArchived()
	if p.Protected == 0 && len(p.DeadHolder) == 0 && len(p.Held) == 0 {
		return archived
	}
	return fmt.Sprintf(protectedSummaryFmt, p.Protected) +
		describeDeadHolderNote(p.DeadHolder) + describeHeldNote(p.Held) +
		describeAttributionNote(p.Attribution) + attributionErrNote(p.AttributionErr) +
		archived
}

// attributionErrNote renders the "the branch axis could not be answered" suffix
// — ONE spelling (Rule 14), shared by the merger log and the status surface.
func attributionErrNote(err string) string {
	if err == "" {
		return ""
	}
	return fmt.Sprintf(protectedAttributionErrFmt, err)
}

// BranchAttributionLine is ONE branch's attribution, already reduced to the
// three things an operator line needs. The reduction happens in
// BranchAttributionLines (ONE renderer, Rule 14) so the merger log and the
// status surface cannot describe the same branch differently.
type BranchAttributionLine struct {
	Branch  string
	Verdict string
	Task    string
	Reason  string
	// Reclaimer is the EXISTING arm whose own verdict decides the remedy for a
	// non-final branch (branchAttributionDecision.Reclaimer, carried through
	// unchanged). Without it a render could only say "withheld" — it could not
	// name what frees the branch, and naming the arm is the deliverable
	// (memory #2324: "reclaimable" is the ARM's verdict, never a re-derivation).
	Reclaimer branchAttributionArm
	// Checkout mirrors branchAttributionDecision.Checkout: a checkout of this
	// branch still exists on disk, so git refuses the branch to the merger.
	// Without it the compact line would name branches nobody is blocking.
	Checkout bool
	// Worktree is the directory that checkout occupies (branchAttributionDecision
	// .Worktree): the operator's answer to "which checkout is holding it". It is
	// empty when no checkout holds the branch, and it is never a guess — the
	// path comes from git's registration, cross-checked against the ledger's
	// recorded directory by the classifier.
	Worktree string
	// Held mirrors branchAttributionDecision.Held: a LIVE PROCESS is measured
	// standing in that checkout right now (the /proc holder scan). It is
	// carried through so no render has to infer "somebody is working here" from
	// the ledger: whether a run is inside the checkout is a MEASUREMENT, and it
	// is the only thing on this axis the operator is asked to care about.
	// Checkout and Held are separate because they answer different questions —
	// an empty checkout (no process in it) is still not a merge candidate.
	Held bool
	// ProbeMeasured records that the checkout holder scan RAN. Held=false with
	// ProbeMeasured=false means "could not be measured", which the render must
	// state as such and never as "nobody is inside" (Rule 15).
	ProbeMeasured bool
	// Inside names the live processes the scan found in the checkout, when it
	// found any — the evidence behind Held, so a positive sentence can name
	// what it saw instead of asserting it. It is optional by construction: a
	// line built by hand (a fixture) carries no names, so the renderer treats an
	// empty list as "no name available", never as a second measurement.
	Inside []string
	// RunnerLive/RunnerKnown are the ROW axis: whether the attributed ledger
	// row records a runner identity at all (RunnerKnown) and whether this host
	// can see that incarnation alive (RunnerLive). They are the second,
	// INDEPENDENT liveness measurement, and they exist because the checkout
	// scan alone cannot see a run whose process keeps no cwd/fd/mapped path in
	// its worktree (the `gopi worker` case measured 2026-09-18). A render must
	// print both axes; neither may be presented as the other.
	RunnerLive  bool
	RunnerKnown bool
	// Age is how long the checkout has existed (from the git worktree
	// registration), AgeKnown whether that could be measured. It is the
	// operator's own requested signal — "how long the worktree/branch lives" —
	// and it claims nothing about the run.
	Age      time.Duration
	AgeKnown bool
}

// describeAttributionNote renders the branch-axis suffix of the protected line:
// the branches attributed to a ledger row AND still held by a checkout, with the
// owning task id, next to the dead-holder annotations.
//
// TWO FILTERS, both deliberate. A branch with NO owner is unprotected — it is
// already named by the candidate list, and repeating it here would double-count
// it. A branch whose checkout is GONE is not what this line is about either:
// git no longer refuses it to the merger, so the line would name a "protected"
// branch nobody is blocking. What remains is exactly the population this line
// exists for — the branches the merger withholds and can now say WHOSE they are.
func describeAttributionNote(lines []BranchAttributionLine) string {
	var parts []string
	for _, l := range lines {
		if !l.Checkout {
			continue
		}
		if l.Verdict == string(branchAttribUnowned) || l.Verdict == string(branchAttribIntegration) {
			continue
		}
		parts = append(parts, l.Branch+" — "+l.Task+" ["+verdictOperatorWords(l.Verdict)+"]")
	}
	if len(parts) == 0 {
		return ""
	}
	return fmt.Sprintf(protectedAttributionFmt, len(parts), strings.Join(parts, "; "))
}

// describeHeldNote renders the still-refused suffix: for each dead-holder
// branch the reclaim will NOT free, its name and the arm's own reason in one
// line. Sorted, so the operator line is stable across ticks (the reclaim arm's
// reasons are stable strings, so grouping by branch keeps one line per branch).
func describeHeldNote(held map[string]string) string {
	if len(held) == 0 {
		return ""
	}
	branches := make([]string, 0, len(held))
	for b := range held {
		branches = append(branches, b)
	}
	sort.Strings(branches)
	parts := make([]string, 0, len(branches))
	for _, b := range branches {
		parts = append(parts, b+" — "+held[b])
	}
	return fmt.Sprintf(protectedHeldNoteFmt, len(parts), strings.Join(parts, "; "))
}

// describeDeadHolderNote renders the dead-holder suffix — the ONE definition
// of how a dead-holder branch is named to an operator (Rule 14). Both front
// doors compose it: the merger's per-repo log line (via DescribeProtected)
// and `task merger status`'s repo block. One spelling means a branch cannot
// be named on one surface and silently dropped from the other — the drift
// that would re-create the original anonymous protected count.
func describeDeadHolderNote(deadHolder []string) string {
	if len(deadHolder) == 0 {
		return ""
	}
	return fmt.Sprintf(protectedDeadHolderFmt, len(deadHolder), strings.Join(deadHolder, ", "))
}

// MergerSweepPreview reports what MergerRepoSweep would do for ONE repo
// without doing any of it. Discovery is shared with the sweep itself
// (repoSweepUnprotectedBranches), so the preview cannot drift from the real
// candidate set.
func MergerSweepPreview(repo string) RepoSweepPreview {
	preview := RepoSweepPreview{Repo: repo}
	candidates, inv, err := repoSweepClassify(repo, mergerStatusBranchListMax, nil)
	if err != nil {
		preview.Err = err.Error()
		return preview
	}
	preview.Candidates = candidates
	preview.Protected = inv.Protected
	preview.Inventory = inv
	preview.DeadHolder = deadHolderProtectedBranches(repo, candidates, inv.Protected)
	preview.Held = heldProtectedBranches(repo, preview.DeadHolder)
	preview.Attribution, preview.AttributionErr = BranchAttributionLines(repo)
	preview.WithheldInMaster = repoSweepWithheldInMaster(repo, preview.Attribution)
	// THE ARCHIVED SET — fault 4 of the 2026-09-16 audit. Every archive-tagged
	// branch is exempt from every merge path (repoSweepArchiveExempt), so
	// without this the set appears in NO candidate list and no task row mentions
	// it (the owning row says completed). Reported as records, not a count, so
	// each buried entry carries branch — task — status.
	archivedRows, archivedListErr := ArchiveRowsForRepo(repo)
	preview.Archived = archivedRows
	if archivedListErr != nil {
		preview.ArchivedErr = archivedListErr.Error()
	}
	return preview
}

// repoSweepWithheldInMaster marks the ATTRIBUTED branches whose tip is already
// an ancestor of master. It exists because the remedy an operator must take
// differs between the two shapes, and only a POSITIVE measurement may claim the
// difference (Rule 15):
//
//	already in master  -> releasing the blocker is the WHOLE remedy: the
//	                      deliverable is already landed, there is nothing left to
//	                      merge (the merger's own log says exactly this for the
//	                      protected arm).
//	not in master      -> releasing the blocker is the FIRST step; the branch
//	                      then reaches the merge sweep like any other deliverable.
//
// The ordinary spent scan (repoSweepSpentCandidates) cannot answer this: it
// walks the CANDIDATES, and a worktree-held branch never becomes one — which is
// the whole population this annotation is for. The scan is bounded by the SAME
// budget constant the spent scan uses (a read-only command's cost ceiling), and
// an unmeasured branch is left UNMARKED rather than guessed.
func repoSweepWithheldInMaster(repo string, lines []BranchAttributionLine) map[string]bool {
	var names []string
	for _, l := range lines {
		// BOTH populations are measured: a branch git still refuses (its
		// checkout is on disk) AND one whose checkout is already gone but whose
		// registration/row survives. The remedy sentence differs between them
		// (release the directory vs repair the registration), but "is the work
		// already in master?" is the same question for both, and it is the half
		// the operator needs to decide whether a merge is still owed.
		if l.Verdict == string(branchAttribUnowned) || l.Verdict == string(branchAttribIntegration) {
			continue
		}
		names = append(names, l.Branch)
	}
	if len(names) == 0 {
		return nil
	}
	out := map[string]bool{}
	for i, b := range names {
		if i >= branchInventorySpentScanMax {
			break // budget reached: the rest stay UNMEASURED, never assumed
		}
		if _, err := repoSweepGit(repo, notFFMergeBaseVerb, notFFIsAncestorFlag, b, repoSweepIntegrationBranchFn()); err == nil {
			out[b] = true
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// Dispositions is the preview's branch-axis answer GROUPED into the three
// situations an operator must be able to tell apart (merger_status_disposition.go):
// nothing to do / work withheld (with the reason and the existing remedy arm) /
// work attributed to nobody. It re-derives nothing — it groups the verdicts the
// preview already carries.
func (p RepoSweepPreview) Dispositions() []branchDisposition {
	return branchDispositions(p)
}

// UnownedBranchCount is the number of branches in the unowned class. It lives
// on the preview rather than being counted by each caller, so the header count
// and the listed names can never disagree (Rule 13).
func (p RepoSweepPreview) UnownedBranchCount() int {
	n := 0
	for _, l := range p.Attribution {
		if branchAttribution(l.Verdict) == branchAttribUnowned {
			n++
		}
	}
	return n
}

// heldProtectedBranches narrows the dead-holder list to the branches the
// reclaim arm will NOT free, with the arm's own reason (ReclaimHeldBranches).
// It is deliberately derived from the DeadHolder list, so the two annotations
// describe the same branches: DeadHolder says "the holder is gone", Held says
// "and here is why that still does not release it".
func heldProtectedBranches(repo string, deadHolder []string) map[string]string {
	if len(deadHolder) == 0 {
		return nil
	}
	all := ReclaimHeldBranches(repo)
	if len(all) == 0 {
		return nil
	}
	held := map[string]string{}
	for _, b := range deadHolder {
		if why, ok := all[b]; ok {
			held[b] = why
		}
	}
	if len(held) == 0 {
		return nil
	}
	return held
}

// deadHolderProtectedBranches is the dead-holder annotation for the
// protected arm: which of repo's worktree-protected branches the LEDGER
// attributes to a holder that is provably gone (DeadHolderBranches). The
// names are sorted so the operator line is stable across ticks, and the set
// is never a guess — an unattributed occupied worktree stays a plain
// protected branch (git alone cannot know a holder died).
func deadHolderProtectedBranches(repo string, _ []string, protected int) []string {
	if protected == 0 {
		return nil
	}
	dead := DeadHolderBranches(repo)
	if len(dead) == 0 {
		return nil
	}
	names := make([]string, 0, len(dead))
	for b := range dead {
		names = append(names, b)
	}
	sort.Strings(names)
	return names
}

// MergerRepoSweep runs ONE full per-repo sweep turn: discover unprotected
// candidate branches (in-master ones are the tidy pass's job, not ②) → decide
// every candidate with git alone (fast-forward it, or — when it does not
// fast-forward — report it and leave it untouched) → ③ NOTHING: the
// integration-side tidy (MergedCleanupSweep: worktree remove + branch -d +
// archive tag) was RETIRED from this tick by epic sweep-report-not-act
// (2026-09-19), so the sweep mutates no ref. Returns the outcome so the caller
// can log the tallies and rebuild when this turn landed work. Fail-soft: any
// git error lands in res.Err and the turn stops there (the repo is likely
// broken or not actually a gopi task repo).
//
// NO PRODUCTION CALLER, like its one caller (measured 2026-11-01, task
// 01a19466, tree 52657bb6): renaming this declaration makes the compiler name
// exactly ONE reference — the `tools.MergerRepoSweep(repo, 0)` call inside
// mergerSweepRepo — and mergerSweepRepo's only caller is mergerPassOnce, which
// has none. Everything below is what the function DOES when it is run, not what
// a shipped command runs today. The reachable merge path is `gopi task merge` →
// tools.TaskMerge, which meets the sweep through tools.MergerSweepPreview
// (task_merge.go) — the READ-ONLY half, which calls none of this.
//
// NO CHILD, NO LLM, NO WAIT (epic merger-ff-park-file, 2026-09-16). There is no
// resolver seam to inject, no stop channel to open, no exit code to poll and no
// grace/timeout ladder to bound: the whole tick is a sequence of git
// invocations and nothing else — a branch that does not fast-forward is
// REPORTED, not written to (epic merger-remove-park, 2026-09-17: the park
// lane's tag + row + forced checkout removal are gone). A branch the tick
// cannot land never ends the tick: the next branch is decided immediately.
//
// THE MAIN CHECKOUT IS RESTORED FIRST (the epic that demanded it was named for
// the RETIRED `gopi merge --fast` arm — that headless spelling is gone since
// 2026-09-27, and the live merge face is `gopi task merge`). See
// repoSweepRestoreCheckout for the measured failure this repairs.
//
// IntegrationBranchForRepo names the branch a LANDING must move in repo.
//
// It is masterBranchName unless the repo has no master ref at all AND has main —
// the same tolerated alternate the merge executor's guard accepts (task_merge_
// executor.go: `head != "master" && head != "main"` is the refusal). Resolving it
// from the REFS rather than assuming is what keeps the restore from refusing a
// repo that integrates into main: `git checkout master` there would fail, and a
// check that only ever compared against masterBranchName would report the repo
// permanently parked. An unresolvable ref list falls back to masterBranchName —
// the historical constant — and the caller then reports a refusal rather than
// inventing a branch.
//
// EXPORTED because several landing paths resolve it and must not drift: the
// resident sweep (repoSweepRestoreCheckout) and the merge engine
// (internal/tools/task_merge.go, the reachable `gopi task merge` face) both
// restore the checkout through the ONE implementation below (Rules 13/14 — a
// second resolver is a second answer).
//
// THE `cmd/agent mergerSweepRepo` CALLER IS NOT LIVE (measured 2026-11-01, task
// 01a19466, tree 52657bb6): mergerSweepRepo's only caller is mergerPassOnce,
// which has no caller, and renaming that declaration leaves `go build ./...`
// green. The reachable caller of THIS function is the `expected :=
// IntegrationBranchForRepo(repo)` site in internal/tools/task_merge.go.
func IntegrationBranchForRepo(repo string) string {
	for _, cand := range []string{masterBranchName, mainBranchName} {
		if _, err := repoSweepGit(repo, repoSweepRefProbeVerb, "-q", "--verify", refsHeadsPrefix+cand); err == nil {
			return cand
		}
	}
	return masterBranchName
}

// repoSweepHasCandidates reports whether repo has any local head that is not the
// integration branch (and not excluded by the caller). It is the WRITE GATE of
// the checkout restore: the restore MOVES the main checkout, so a repo with
// nothing to decide must not be touched at all.
//
// It is deliberately a cheap ref listing rather than a second classification:
// the question is only "is there anything this tick could act on", and the
// authoritative answer is the candidate set the sweep computes afterwards. The
// two disagree in one direction only — this gate can be true for a branch the
// classification then excludes (an archive-tagged or worktree-held one) — and
// that direction is harmless: it means the restore ran for a repo whose checkout
// was parked, which is exactly the state worth repairing.
func repoSweepHasCandidates(repo string, excluded map[string]bool) bool {
	out, err := repoSweepGit(repo, "for-each-ref", "--format=%(refname:short)", refsHeadsPrefix)
	if err != nil {
		return false // an unlistable repo is not a repo this tick may move
	}
	for _, b := range strings.Split(out, "\n") {
		b = strings.TrimSpace(b)
		if b == "" || b == masterBranchName || b == mainBranchName || excluded[b] {
			continue
		}
		return true
	}
	return false
}

// repoSweepRestoreCheckout puts the repo's MAIN checkout back on the integration
// branch BEFORE any branch is decided, and returns the ONE sentence to report
// ("" = the checkout was already there, so nothing happened).
//
// THE DEFECT THIS CLOSES (the epic that demanded it was named for the RETIRED
// `gopi merge --fast` arm): the landing below is `git merge --ff-only <branch>`
// run in the main checkout, so it fast-forwards whatever branch that checkout
// happens to hold. Measured on a two-branch fixture: with the checkout parked on
// a branch that already contains the candidate's work, git answers "Already up to
// date." and exits 0 — the tick then counts a LANDING while master never moved.
// That retired headless arm was the same shape with a worse tail: it took that
// same exit-0 as its cue to PRUNE the branch ref, so the work leaves the queue
// while the report says it arrived. Restoring the checkout first is what makes
// every `--ff-only` below mean what it says.
//
// It runs BEFORE the per-branch decisions and for EVERY tick with candidates —
// not only when a landing happens to follow: the parked state is the condition
// that makes the next landing wrong, and the restore is a pointer move in a clean
// tree (see FinishCleanupMainCheckoutRestore for why no process scan gates it).
// A refusal is REPORTED and the tick continues: every gate downstream (the spent
// arm, the not-ff report) still decides each branch correctly, and refusing here
// would freeze the queue on a host whose main checkout a human parked.
func repoSweepRestoreCheckout(repo string) string {
	ok, detail := FinishCleanupMainCheckoutRestore(context.Background(), repo, IntegrationBranchForRepo(repo))
	if ok {
		return detail // "" when it was already on the integration branch
	}
	return detail
}

// MergerRepoSweep performs one read-only-verdict tick over repo's branches.
func MergerRepoSweep(repo string, scanMax int, opts ...MergerRepoSweepOptions) RepoSweepResult {
	res := RepoSweepResult{Repo: repo}
	// The exclusion list is a caller-side "this branch is gated elsewhere"
	// statement — the only thing an option can say to this tick.
	var excluded map[string]bool
	if len(opts) > 0 && len(opts[0].ExcludeBranches) > 0 {
		excluded = make(map[string]bool, len(opts[0].ExcludeBranches))
		for _, b := range opts[0].ExcludeBranches {
			excluded[strings.TrimSpace(b)] = true
		}
	}
	if scanMax <= 0 {
		scanMax = repoSweepMaxBranches
	}

	// ①a THE CHECKOUT RESTORE, and it comes FIRST for a reason that is not
	// cosmetic: the classification below treats a branch checked out ANYWHERE in
	// a registered worktree as a live peer's bond and EXCLUDES it from the
	// candidate set — the main checkout included, because `git worktree list`
	// reports it as the first registration. A repo whose main checkout a finished
	// run left parked on its branch therefore presents NO candidates at all, and a
	// restore placed after the discovery would never see the very state it exists
	// to repair. Restoring first puts the parked branch back into the candidate
	// set (its ref is untouched by the move) and makes every `--ff-only` below
	// fast-forward the integration branch, which is what those commands mean.
	//
	// It is a WRITE, so it is gated: only a repo with at least one local head
	// other than the integration branch is walked, and the restore itself refuses
	// a detached HEAD, an unreadable checkout and a dirty tree (it never forces a
	// branch move over uncommitted work). A repo with nothing to decide is left
	// exactly as it is.
	if repoSweepHasCandidates(repo, excluded) {
		res.Checkout = repoSweepRestoreCheckout(repo)
	}

	// ① discover unprotected merge candidates (not in master).
	mergeable, protectedCount, err := repoSweepUnprotectedBranches(repo, scanMax, excluded)
	if err != nil {
		res.Err = err.Error()
		return res
	}
	res.Protected = protectedCount
	res.DeadHolder = deadHolderProtectedBranches(repo, mergeable, protectedCount)
	res.Held = heldProtectedBranches(repo, res.DeadHolder)
	res.Attribution, res.AttributionErr = BranchAttributionLines(repo)

	// ①b dirty dead-holder salvage (epic 2026-09-15): of the held branches the
	// reclaim arm refuses, the DIRTY ones carry uncommitted work whose only
	// copy is a dead holder's directory. File ONE salvage row per such branch
	// (durable patch + untracked capture as attachments) so the work has a
	// carrier — the tree is never written, cleaned or removed. Deduped through
	// the live-summary probe, so a tick that finds nothing new adds nothing.
	res.DirtySalvage = SalvageDirtyDeadHolderWorktrees(repo, res.Held)

	// ② FF OR LEAVE-ALONE, one branch at a time, git as the only judge. The loop
	// is sequential on purpose: the ff question must be asked per branch,
	// immediately before the action it justifies, and a branch the tick cannot
	// land must not stop the tick.
	if len(mergeable) > 0 {
		sort.Strings(mergeable)
		for _, branch := range mergeable {
			outcome, action := repoSweepDecideBranch(repo, branch, &res)
			res.Outcomes = append(res.Outcomes, outcome)
			switch action {
			case mergeActionMerged:
				res.Merged++
			case repoSweepActionReported:
				// A branch git answered "its tip is already in the integration
				// branch" for: REPORTED with owner evidence, nothing mutated
				// (repoSweepReportSpentBranch).
				res.Reported++
			case repoSweepActionNotFF:
				// Reported, nothing touched: the branch keeps its ref, its
				// checkout and its place in the next tick's candidate set
				// (repoSweepNotFFOutcome).
				res.NotFF++
			case repoSweepActionRefGone:
				// A candidate whose ref had been deleted between discovery and
				// the decision (repo_sweep_ref_guard.go). Skipped before any
				// gate was asked, and the disappearance is RECORDED — never
				// swallowed (Rule 15). Reported as its own count so a tick can
				// never hide it behind the merged/not-ff tallies.
				res.RefGone++
			}
		}
		res.MergedBranches = MergedBranchNames(res.Outcomes)
		// The merge-time compile gates refuse a branch BEFORE any git merge
		// runs (repoSweepDecideBranch asks them first), so their refusals are
		// routed into the existing branch-rework lane here.
		repoSweepRouteContractRefusals(repo, res.Outcomes, &res)
		repoSweepRouteBuildRefusals(repo, res.Outcomes, &res)
	}

	// ③ NOTHING. The integration-side tidy (MergedCleanupSweep: worktree remove
	// + branch -d + archive tag) is RETIRED from this tick — the sweep mutates
	// no ref (epic sweep-report-not-act, 2026-09-19; briefing
	// briefings/brief-sweep-report-not-act.md). Every branch whose tip is
	// already integrated was reported by ① with its owner evidence; there is no
	// cleanup lane left to run, because under the operator's model every branch
	// has a destination and none is ever left ownerless in refs/heads.
	return res
}

// repoSweepDecideBranch decides ONE candidate branch with git alone and
// performs the landing or the report that decision implies. Returns the outcome
// plus the action the caller tallies.
//
// The order is master-safety by construction:
//
//	⓪ the candidate's REF is re-verified FIRST: the candidate set is a
//	  DISCOVERY-TIME snapshot of refs/heads (repoSweepClassify), and another
//	  merger round may have landed and deleted the branch since. A name git no
//	  longer has is skipped and the disappearance is recorded — never handed to
//	  a gate (repo_sweep_ref_guard.go, the 2026-09-16 ghost-ref incident);
//	⓪b the merge-time COMPILE gates (CLI contract, branch build, union build)
//	   are asked BEFORE anything touches master or the branch: a branch that
//	   would put red tests or a broken build on master is refused, archived by
//	   the gate itself, and never reaches the merge path below;
//	① git merge-base --is-ancestor answers "is this branch spent?" (its tip is
//	  already in master) — a spent branch is pruned, nothing else;
//	② git merge --ff-only performs the landing. --ff-only IS the merge: it
//	  either advances master to the branch tip or fails without touching
//	  master, so this arm can never mint a merge commit and can never leave
//	  master half-merged;
//	③ anything that does not fast-forward is LEFT ALONE and REPORTED
//	  (repoSweepNotFFOutcome) — no tag, no ledger row, no worktree touched.
func repoSweepDecideBranch(repo, branch string, res *RepoSweepResult) (TaskMergeBranchOutcome, string) {
	// ⓪ ref re-verification: a discovery-time snapshot is not a promise (Rule 15
	// — the disappearance is skipped AND recorded, never swallowed).
	present, probeErr := repoSweepRefPresentFn(repo, branch)
	if probeErr != nil {
		// An INCONCLUSIVE probe is neither a disappearance nor a licence to
		// merge: report the git error and leave the branch for the next tick.
		return TaskMergeBranchOutcome{Branch: branch, Action: repoSweepActionError,
			Detail: probeErr.Error()}, repoSweepActionError
	}
	if !present {
		outcome, action, _ := repoSweepRefGoneOutcome(repo, branch)
		return outcome, action
	}

	// ⓪a the TARGET-DEVDOC lane USED TO STAND HERE, and it is gone with the corpus
	// it measured. It asked one question about the TARGET rather than about the
	// union — whether the integration branch's own devdoc anchor corpus carried
	// unpermitted anchor-NUMBER drift — and held or dispatched accordingly
	// (internal/tools/merge_target_doc_gate.go, plus the config-index lane that
	// shared its scope). The baseline table, its permits and the generator that
	// wrote them were internal/docclass, deleted with the document pipeline
	// (removal slice 3/5), so there is no drift left for a lane to find: the arm
	// went with the thing it measured, and Res.TargetDocDrift/Res.TargetDocRefused
	// with it. This is a REMOVED check, not a weakened one — nothing below asks
	// the question in another shape, and the gates that do remain (⓪b contract,
	// ⓪b' build, ⓪c union compile, ⓪d manifest union, ⓪e published anchor) are
	// untouched.

	// ⓪b compile gates: a refusal's archive + notification happen inside the
	// gate (taskMergeRefuseUncompilable), so this arm only reports.
	if gate := mergeContractGate(repo, masterBranchName, branch); gate.Refused {
		return TaskMergeBranchOutcome{Branch: branch, Action: mergeContractGateActionRefused,
			Detail: gate.Detail}, mergeContractGateActionRefused
	}
	if gate := mergeBuildGate(repo, masterBranchName, branch); gate.Refused {
		return taskMergeRefuseUncompilable(repo, masterBranchName, branch, mergeBuildGateName,
			gate.Detail, gate.BranchErr, gate.BuildCmd), mergeBuildGateActionRefused
	}
	if gate := mergeUnionGate(repo, masterBranchName, branch); gate.Refused {
		return taskMergeRefuseUncompilable(repo, masterBranchName, branch, mergeUnionGateName,
			gate.Detail, gate.BranchErr, gate.BuildCmd), mergeBuildGateActionRefused
	}
	if gate := manifestUnionGate(repo, masterBranchName, branch); gate.Refused {
		return taskMergeRefuseManifest(repo, masterBranchName, branch, gate.Manifest, gate.Detail), mergeBuildGateActionRefused
	}

	// ① spent work: the branch tip is already in the integration branch — the
	// verdict the sweep has always reached, with the MUTATION REMOVED. The ref
	// is REPORTED (branch, tip, verdict, owner evidence) and every live ledger
	// row still naming it fires a loud alarm on the existing notify channel
	// (repo_sweep_report.go; briefing brief-sweep-report-not-act.md).
	if outcome, action, spent := repoSweepReportSpentBranch(repo, branch, res); spent {
		return outcome, action
	}
	// An UNANSWERABLE ancestry question is its own read-only skip, and the
	// report arm already decided that: `git merge-base --is-ancestor` exit 1 is
	// the ordinary "not an ancestor" answer (the branch is real work — proceed
	// to the landing below), while any other failure came back as the error arm
	// above. There is deliberately NO second probe here: re-asking would be a
	// second spelling of the same question, and treating exit 1 as an error is
	// exactly how the landing path gets silently disabled.

	// ② the landing. --ff-only cannot conflict, cannot commit and cannot leave
	// a MERGE_HEAD behind: on the not-ff branch it fails with master untouched,
	// which is exactly the input the not-ff report below needs.
	if _, err := repoSweepGit(repo, "merge", "--ff-only", branch); err == nil {
		// A LANDING is not a prune: master advanced to the branch tip, so the
		// tick's job here is done and the branch is NOT deleted (the sweep
		// mutates no ref — epic sweep-report-not-act, 2026-09-19). The branch
		// keeps its ref, its checkout and its place in the next tick's candidate
		// set, where the report arm above names it on every later tick.
		//
		// THE LANDING RECORDS THE DURABLE MARK (Rule 12's close loop). Master has
		// just advanced here, and the rebuild that must follow it is a LATER step
		// of this same tick — the per-repo sweep only reaches it after the whole
		// branch surface is decided. A pass killed inside that gap cannot be
		// caught from the git state alone: a successor's report arm sees an
		// already-integrated tip and reports it without replaying the landing
		// (measured 2026-09-16: a landing at 07:29:34 with bin/gopi left at
		// 07:17:53, and no later round came back for it).
		//
		// So the landing writes the mark, and the mark SURVIVES the process that
		// wrote it: the next pass's START drains it (mergerPassCloseLoop(nil) at
		// the top of mergerPassOnce — the pass END is exactly the step a SIGKILLed
		// pass never reaches, because a SIGKILL runs no defer). NOTE (measured
		// 2026-11-01, task 01a19466): that pass has no production caller, but the
		// DRAIN does not depend on it — mergerCloseLoopAfterLanding reaches the
		// same function from the live `gopi task merge-execute` face
		// (cli_tasks_rebuild.go), so a mark written here is still discharged.
		//
		// UNCONDITIONAL on purpose, not a branch on the build contract:
		// MarkRebuildPending refuses a repo that declares no build, IN SILENCE —
		// the same deliberate silence serveRebuild keeps for an undeclared build
		// (Rule 12: no entry, no build). Asking here would duplicate that check
		// and could only disagree with it.
		MarkRebuildPending(repo, MarkRebuildAfterLandingReason)
		return TaskMergeBranchOutcome{Branch: branch, Action: mergeActionMerged,
			Detail: "fast-forward into " + masterBranchName + " — landed; the branch ref is REPORTED, never deleted by the sweep"}, mergeActionMerged
	}

	// ③ not ff-able: LEAVE IT ALONE and say so. The tick has no mechanism left
	// that ACTS on such a branch — no archive tag (which would permanently remove
	// it from every candidate set), no ledger row, no worktree removal.
	// Resolution is the merge command's job (the live family spelling
	// `gopi task merge`, whose hand-over is the branch-rework row it files —
	// reworkLandingProbe / task_rework_successor), and the branch must SURVIVE
	// for that hand-over to diff against. The retired one-shot `gopi merge`
	// verb and its shipped prompt cmd/agent/merge_prompt.md are gone with the
	// merge-verb-removal epic (2026-11-01), and the retired merger launcher's
	// take-over prompt went earlier with the daemon (epic
	// merger-delete-dead-impl 2026-09-19). See repoSweepNotFFOutcome.
	return repoSweepNotFFOutcome(branch)
}
