package tools

// The merger status surface's DISPOSITION block — the answer to the operator
// complaint this file exists for:
//
//	`pending: none — nothing to merge` reads identically whether there is
//	genuinely no work, whether all the work is WITHHELD from the merger, or
//	whether the merger is not running at all.
//
// Measured live on the GoPi repo (2026-09-17): `gopi merger --status` printed
// `pending: 5 branch(es) the next sweep would act on` while TWELVE branches the
// merger cannot merge sat in the same repo — two held by a live run, two held
// by a checkout whose owner already ended, four remembered only by a completed
// ledger row, four in master. All of that was already IN the status output
// (the buckets and the attribution note), but nowhere did it add up to a
// sentence an operator could act on; the pending count alone reads as "the
// merger is keeping up".
//
// THE THREE SITUATIONS ARE RENDERED DISTINGUISHABLY, and nothing is deleted to
// achieve it: `pending:` keeps its existing meaning (the next sweep's work
// list) and this block adds the DISPOSITION of everything that is NOT pending.
// The distinction is a CLASSIFICATION, not a new heuristic:
//
//	nothing  — the repo has no work anywhere (no withheld branch, no candidate)
//	withheld — branches exist that the merger is NOT going to merge, each with
//	           the reason and the existing arm that is their remedy
//	unowned  — a ledger row is absent, so the branch is recyclable outright
//
// WHERE THE FACTS COME FROM (no re-derivation — Rule 13). The verdicts are the
// SAME branch-attribution verdicts the protected line already renders
// (branch_attribution.go), the remedies are the arms that already own them
// (BranchAttributionDecision.Reclaimer), and the worktree-sweep disposition is
// the sweep's own classifier (worktreeWholeDepositClean). This file adds no
// probe of its own: it GROUPS the evidence the preview already carries.
//
// WHY THE CLASSIFICATION IS BY (VERDICT, ARM) AND NOT BY BUCKET. The worktree
// sweep's own disposition answers "is this checkout still a crime scene?"
// (age, dirt, ledger life) and knows nothing about the branch axis — a branch
// whose checkout is GONE is not in its input at all, which is exactly the
// population the operator could not see. The verdict+arm pair is the only
// answer that covers BOTH populations, and it is already computed once per
// branch for the protected line.

import (
	"fmt"
	"sort"
	"strings"
	"time"
)

// branchDispositionKind is the closed set of disposition CLASSES this block
// reports. Named (Rule 1b): the render and the classifier compare the same
// constants, and a reader of the output can grep the class.
type branchDispositionKind string

const (
	// branchDispositionNothing: no branch in this repo is withheld from the
	// merger AND none is waiting — there is genuinely nothing to do. It is NOT
	// the same statement as `pending: none`, which is also printed when every
	// branch is withheld.
	branchDispositionNothing branchDispositionKind = "nothing"
	// branchDispositionWithheld: branches the merger will not merge, each with
	// its reason and its existing remedy arm.
	branchDispositionWithheld branchDispositionKind = "withheld"
	// branchDispositionUnowned: branches no ledger row names (the ledger WAS
	// read). The only class safe to recycle outright — attributed to nobody.
	branchDispositionUnowned branchDispositionKind = "unowned"
)

// The withheld REASONS — the operator-facing half of the disposition line.
// Each names WHY the merger is not merging the branch, in the vocabulary the
// remedy arm uses, so the sentence and the arm cannot drift (Rule 1b/14).
const (
	// withheldReasonLiveRunnerIdle: the ledger's live-holder verdict — a task
	// row's recorded runner is alive — but the MEASURED checkout scan found
	// nobody standing in the branch's checkout. Both halves are stated, and the
	// sentence is careful about WHICH is which: the scan is structurally blind
	// to a `gopi worker` (its cwd is its session directory, measured
	// 2026-09-18), so "nobody inside" is a fact about the checkout and never
	// evidence that the run is over. The first half is the ROW's measurement
	// (pid + start ticks), and it is the one that answers "is this task still
	// running".
	withheldReasonLiveRunnerIdle = "the task row's recorded runner is still alive; the checkout scan found no process inside it (that scan cannot see a runner whose cwd is its session directory)"
	// withheldReasonLiveRunnerHeld: both halves agree — the row's recorded
	// runner is alive AND a live process is measured inside the checkout.
	withheldReasonLiveRunnerHeld = "the task row's recorded runner is still alive and a live process IS inside its checkout (measured) — the work is genuinely in progress"
	// withheldReasonLiveRunnerUnknown: the row records NO runner identity, so
	// the row axis is unverified and the checkout scan is the only evidence
	// there is. Saying so is the honest answer; it is never rendered as either
	// lifecycle conclusion (Rule 15).
	withheldReasonLiveRunnerUnknown = "no runner identity is recorded for its task row, and the checkout scan found no process inside (both axes unproven)"
	// withheldReasonLiveRunner: the sentence for a live-holder verdict whose
	// checkout occupancy could not be measured (no checkout registered, or the
	// decision carries no ground). It names the ledger as its source rather than
	// asserting what the checkout contains (Rule 15).
	withheldReasonLiveRunner = "a task row's recorded runner process is still alive (the ledger's verdict — the checkout itself was not measured)"
	// withheldReasonLiveRunnerClaimOnly: the live-holder verdict rests on the
	// row's CLAIM STAMP alone — the stamp names a live process, while neither
	// the checkout scan nor the row's own recorded runner identity reproduces
	// any life. Saying which evidence carried the verdict is the point: the
	// operator asked for precise text, and "a run is alive" would overstate what
	// this arm measured.
	withheldReasonLiveRunnerClaimOnly = "its task row's claim stamp names a live process, but no process is inside its checkout and the runner identity recorded on the row is not alive — the ownership rests on the claim stamp alone"
	// withheldReasonOwnerEndedResume: the row that owned the checkout is over
	// but the run was INTERRUPTED — the resume arm continues it.
	withheldReasonOwnerEndedResume = "the owner's run ended with its work UNFINISHED — the resume arm continues it"
	// withheldReasonOwnerEndedReclaim: the owner's work is DONE; only the
	// checkout directory needs releasing, after which the branch merges.
	withheldReasonOwnerEndedReclaim = "the owner's work is DONE — the reclaim arm releases the checkout, then the branch merges like any other deliverable"
	// withheldReasonRecordOnly: a ledger row remembers the branch, nothing
	// holds it, and no interrupted run is left to resume — a ledger-hygiene
	// item, remedied by the age sweep's registration repair.
	withheldReasonRecordOnly = "a ledger row remembers it and nothing holds it — registration repair (there is no interrupted run to resume)"
	// withheldReasonHeldByArm: the reclaim arm itself refuses (a dirty
	// checkout above all). The arm's own sentence is appended.
	withheldReasonHeldByArm = "the reclaim arm refuses to release the checkout"
	// withheldReasonInCheckout: a checkout holds it but no ledger row could be
	// matched — reported as such rather than guessed (Rule 15).
	withheldReasonInCheckout = "a checkout holds it and no ledger row could be matched to it"
)

// withheldReasonFor maps a classifier verdict + remedy arm to the ONE sentence
// the disposition block prints. It is total over the withheld population by
// construction: every branch that reaches it carries a verdict, and the
// default states the verdict instead of inventing a reason for it.
func withheldReasonFor(verdict string, arm branchAttributionArm, checkout, held bool, runnerKnown, runnerLive bool) string {
	switch branchAttribution(verdict) {
	case branchAttribLiveHolder:
		// The live-holder verdict is about a TASK ROW's runner; whether a
		// process stands in the CHECKOUT is a separate measurement. The reason
		// names which of the two was measured, so the sentence can never again
		// claim more than the probe said (Rule 15).
		switch {
		case held:
			return withheldReasonLiveRunnerHeld
		case !checkout:
			return withheldReasonLiveRunner
		case runnerKnown && runnerLive:
			// The two axes DISAGREE, and this is the case the operator's
			// complaint came from: the row's runner is alive while the checkout
			// scan sees nobody (a worker's cwd is its session directory). The
			// sentence reports both measurements and picks no winner.
			return withheldReasonLiveRunnerIdle
		case !runnerKnown:
			return withheldReasonLiveRunnerUnknown
		default:
			return withheldReasonLiveRunnerClaimOnly
		}
	case branchAttribOwnerEnded:
		if arm == branchArmResume {
			return withheldReasonOwnerEndedResume
		}
		return withheldReasonOwnerEndedReclaim
	case branchAttribOwnerRecordOnly:
		return withheldReasonRecordOnly
	case branchAttribUngoverned:
		// The ledger could not be read: the branch is withheld by the honest
		// refusal, and saying so is the whole point (Rule 15).
		return branchAttribUngovernedReason
	default:
		return withheldReasonInCheckout
	}
}

// branchAttribUngovernedReason is the shared sentence for "no attribution is
// possible", printed by the disposition block and by the verdict itself.
const branchAttribUngovernedReason = "the task ledger could not be read — nothing may be concluded from its absence"

// The verdict tags the operator surface prints. The classifier's own verdict
// strings (branchAttribLiveHolder = "live-holder", …) are WIRE values: they are
// compared, counted and persisted, so they must not change. They are also not
// sentences — `live-holder` is what the operator reported as unreadable, and a
// status surface's whole job is to be readable. So the render maps each verdict
// to operator words through ONE table (Rule 14: one place to change, and a new
// verdict cannot silently reach the operator in wire spelling).
const (
	verdictWordsLiveHolder      = "an unfinished run still owns it"
	verdictWordsOwnerEnded      = "its run is over, the checkout is still there"
	verdictWordsOwnerRecordOnly = "only a ledger row remembers it (no checkout)"
	verdictWordsUnowned         = "no ledger row names it"
	verdictWordsUngoverned      = "could not be determined"
	verdictWordsUnrecognized    = "unclassified verdict"
)

// verdictOperatorWords is the table above as a function, so the fallback for a
// verdict this render does not know is a SENTENCE about the gap rather than the
// raw wire value leaking to the operator (Rule 15).
func verdictOperatorWords(verdict string) string {
	switch branchAttribution(verdict) {
	case branchAttribLiveHolder:
		return verdictWordsLiveHolder
	case branchAttribOwnerEnded:
		return verdictWordsOwnerEnded
	case branchAttribOwnerRecordOnly:
		return verdictWordsOwnerRecordOnly
	case branchAttribUnowned:
		return verdictWordsUnowned
	case branchAttribUngoverned:
		return verdictWordsUngoverned
	default:
		return verdictWordsUnrecognized
	}
}

// branchDisposition is ONE class's members plus the per-name facts the render
// needs, aligned by index so a name can never be paired with another branch's
// verdict (the silent-mis-attribution shape this epic is about).
type branchDisposition struct {
	Kind       branchDispositionKind
	Names      []string
	Verdicts   []string
	Reclaimers []branchAttributionArm
	// InMaster marks a branch whose tip is already an ancestor of master: the
	// remedy is named in the branch's own terms because the ordinary
	// deliverable path (merge) does not apply to it — a branch already in
	// master needs its BLOCKER released, not merging. Only ever set from a
	// POSITIVE measurement (the spent scan); an unmeasured branch is not
	// marked (Rule 15).
	InMaster []bool
	// HeldReason is the reclaim arm's own refusal sentence, when it has one
	// (RepoSweepPreview.Held). Present only for the branches that arm handles.
	HeldReason []string
	// Checkouts/Helds are the MEASURED ground per branch (index-aligned like
	// every other per-name fact here): whether a checkout holds the branch, and
	// whether a live process is inside that checkout. They exist so the withheld
	// sentence can name which of the two was measured instead of asserting the
	// checkout's contents from the ledger (the misreading this epic reported).
	Checkouts []bool
	Helds     []bool
	// RunnersKnown/RunnersLive are the ROW axis per branch (index-aligned):
	// whether the attributed row records a runner identity at all, and whether
	// this host can see that incarnation alive. They exist so the withheld
	// sentence can say WHICH measurement supports the live-holder verdict
	// instead of asserting a liveness the checkout scan never established.
	RunnersKnown []bool
	RunnersLive  []bool
	// Ages/AgesKnown are the checkout registration's age per branch — the
	// operator's requested "how long has this been standing" signal, printed
	// only when it was measured (Rule 15: no age beats a wrong age).
	Ages      []time.Duration
	AgesKnown []bool
}

// branchDispositions groups the preview's branch-axis evidence into the three
// classes above, in a stable order (names sorted inside each class).
//
// Every branch of the preview's attribution lands in exactly one group — the
// integration namespaces are dropped by the ONE renderer that produced the
// lines (BranchAttributionLines), so this function never re-tests them.
func branchDispositions(p RepoSweepPreview) []branchDisposition {
	groups := map[branchDispositionKind]*branchDisposition{
		branchDispositionWithheld: {Kind: branchDispositionWithheld},
		branchDispositionUnowned:  {Kind: branchDispositionUnowned},
	}
	// TWO measurements answer "is this branch already in master?", and the
	// union is the honest answer: the candidate spent scan (SpentNames) only
	// ever saw candidates, the withheld scan (WithheldInMaster) only ever saw
	// the withheld population. A branch in neither is UNMEASURED, which is why
	// the marker is absent rather than false.
	spent := map[string]bool{}
	for _, n := range p.Inventory.SpentNames {
		spent[n] = true
	}
	for b, ok := range p.WithheldInMaster {
		if ok {
			spent[b] = true
		}
	}
	for _, l := range p.Attribution {
		var kind branchDispositionKind
		switch branchAttribution(l.Verdict) {
		case branchAttribUnowned:
			kind = branchDispositionUnowned
		case branchAttribUngoverned, branchAttribOwnerEnded, branchAttribOwnerRecordOnly, branchAttribLiveHolder:
			kind = branchDispositionWithheld
		default:
			// A verdict this block does not classify is NOT silently dropped:
			// it is reported as withheld with its own verdict sentence, which
			// is the conservative direction (an unclassified branch must never
			// read as "nothing to do").
			kind = branchDispositionWithheld
		}
		g := groups[kind]
		g.Names = append(g.Names, l.Branch)
		g.Verdicts = append(g.Verdicts, l.Verdict)
		g.Reclaimers = append(g.Reclaimers, l.Reclaimer)
		g.InMaster = append(g.InMaster, spent[l.Branch])
		g.Checkouts = append(g.Checkouts, l.Checkout)
		g.Helds = append(g.Helds, l.Held)
		g.RunnersKnown = append(g.RunnersKnown, l.RunnerKnown)
		g.RunnersLive = append(g.RunnersLive, l.RunnerLive)
		g.Ages = append(g.Ages, l.Age)
		g.AgesKnown = append(g.AgesKnown, l.AgeKnown)
		reason := withheldReasonFor(l.Verdict, l.Reclaimer, l.Checkout, l.Held, l.RunnerKnown, l.RunnerLive)
		if held, ok := p.Held[l.Branch]; ok && held != "" {
			// The arm's own refusal outranks the generic sentence: it names
			// the measured obstacle (a dirty checkout) rather than the class.
			reason = withheldReasonHeldByArm + " (" + held + ")"
		}
		g.HeldReason = append(g.HeldReason, reason)
	}
	var out []branchDisposition
	// Withheld first: if a repo has both, the withheld population is the one
	// the operator acted on this surface to find.
	for _, k := range []branchDispositionKind{branchDispositionWithheld, branchDispositionUnowned} {
		if g := groups[k]; len(g.Names) > 0 {
			// The attribution renderer emits branches in sorted order and the
			// per-name slices are index-aligned, so the group is sorted here
			// only to make the ALIGNMENT independent of that upstream sort.
			sortAlignedDisposition(g)
			out = append(out, *g)
		}
	}
	return out
}

// sortAlignedDisposition sorts a group's names and carries every index-aligned
// fact along with them — the ONE reason the per-name slices exist rather than a
// map: a name must never be printed beside a verdict that belongs to another
// branch.
func sortAlignedDisposition(g *branchDisposition) {
	idx := make([]int, len(g.Names))
	for i := range idx {
		idx[i] = i
	}
	sort.Slice(idx, func(a, b int) bool { return g.Names[idx[a]] < g.Names[idx[b]] })
	names := make([]string, len(idx))
	verdicts := make([]string, len(idx))
	arms := make([]branchAttributionArm, len(idx))
	inMaster := make([]bool, len(idx))
	reasons := make([]string, len(idx))
	checkouts := make([]bool, len(idx))
	helds := make([]bool, len(idx))
	runnersKnown := make([]bool, len(idx))
	runnersLive := make([]bool, len(idx))
	ages := make([]time.Duration, len(idx))
	agesKnown := make([]bool, len(idx))
	for i, j := range idx {
		names[i], verdicts[i], arms[i] = g.Names[j], g.Verdicts[j], g.Reclaimers[j]
		inMaster[i], reasons[i] = g.InMaster[j], g.HeldReason[j]
		checkouts[i], helds[i] = g.Checkouts[j], g.Helds[j]
		runnersKnown[i], runnersLive[i] = g.RunnersKnown[j], g.RunnersLive[j]
		ages[i], agesKnown[i] = g.Ages[j], g.AgesKnown[j]
	}
	g.Names, g.Verdicts, g.Reclaimers, g.InMaster, g.HeldReason = names, verdicts, arms, inMaster, reasons
	g.Checkouts, g.Helds = checkouts, helds
	g.RunnersKnown, g.RunnersLive = runnersKnown, runnersLive
	g.Ages, g.AgesKnown = ages, agesKnown
}

// The render's format strings, named once (Rule 1b/14) so the status block and
// any future consumer of the same classes print the same sentences.
const (
	// dispositionSummaryFmt is the class header: how many branches are in this
	// disposition. It never replaces `pending:` — it is the OTHER half of the
	// answer (what is NOT pending).
	dispositionSummaryFmt = "  dispositions: %s\n"
	// dispositionWithheldSummaryFmt heads the withheld case: how many branches
	// the merger is NOT merging, out of the repo's heads, plus the liveness
	// parenthetical (which changes what the operator must do).
	dispositionWithheldSummaryFmt = "%d branch(es) withheld from the merger, %d head(s) accounted for%s"
	// dispositionWithheldHeaderFmt heads the withheld list with its count. It
	// names the branch axis first ("no checkout releases it") because that is
	// the question this list answers for the operator; the remedy column follows.
	dispositionWithheldHeaderFmt = "  withheld:  %d branch(es) the merger will NOT merge this tick — each names what withholds it and the remedy arm that frees it:\n"
	// dispositionWithheldLineFmt is ONE withheld branch: its name, WHAT owns it
	// (in operator words, plus whether it is already inside master — which
	// decides whether the remedy is "release, then it merges" or "release, and
	// it is already in"), and the measured reason.
	dispositionWithheldLineFmt = "    - %s [%s%s] — %s\n"
	// dispositionWithheldInMasterNote marks a branch already inside master.
	dispositionWithheldInMasterNote = ", already in " + masterBranchName + "; release it and the deliverable is IN " + masterBranchName
	dispositionUnownedHeaderFmt     = "  unowned:   %d branch(es) no ledger row names — the only class attributed to nobody:\n"
	dispositionUnownedLineFmt       = "    - %s\n"
	// withheldCheckoutAgeFmt prefixes a withheld branch's reason with the
	// MEASURED age of its checkout registration — the operator's own requested
	// attention signal, printed only when the measurement succeeded.
	withheldCheckoutAgeFmt = "its checkout has stood for %s; "
)

// renderDispositions renders the block — but only when it has something to say
// (at least one withheld or unowned branch). That is an output-contract
// decision, not a shortcut:
//
//   - a repo with NOTHING withheld and NOTHING waiting already reads correctly:
//     `pending: none — nothing to merge` is then TRUE, and the header's
//     `merger: running` / `not running` plus the lock-file lines already name
//     the dead-merger case. The situation that had NO representation on any line
//     is the withheld one — measured live: twelve heads the merger could not
//     touch beside `pending: 5 branch(es) …`, with the word on no line at all.
//   - so the block appears exactly when it carries information. Every
//     pre-existing line of the GoPi repo's output is untouched: the diff is
//     ADDITIONS only, which is what "the existing output contract stays
//     byte-identical" means for a surface whose whole defect was a missing
//     sentence (deleting or rewriting a line to make room would break the
//     contract for real).
func renderDispositions(groups []branchDisposition, inv BranchInventory, unownedTotal int, state string) string {
	withheld := 0
	for _, g := range groups {
		if g.Kind == branchDispositionWithheld {
			withheld = len(g.Names)
		}
	}
	if withheld == 0 && unownedTotal == 0 {
		return ""
	}
	var b strings.Builder
	if withheld > 0 {
		fmt.Fprintf(&b, dispositionSummaryFmt, fmt.Sprintf(
			dispositionWithheldSummaryFmt, withheld, inv.Total, dispositionMergeNote(state)))
		b.WriteString(renderWithheld(groups))
	}
	b.WriteString(renderUnowned(groups, unownedTotal))
	return b.String()
}

// dispositionMergeNote is the liveness parenthetical for the WITHHELD case: the
// branches are held by their owners, but if nothing is polling the repo the
// remedies below will never run by themselves — an operator must start the
// merger. It is the same fact as the nothing-case note, stated for the case
// where it changes what the operator does.
//
// IT ASKS THE QUESTION THE OPERATOR ASKS ("is a merger going to act on these?"),
// not whether one state letter is set: G3 split the old single StatusNotRunning
// into StatusNotRunning (nothing was ever started) and StatusHolderGone (a merger
// ran and exited), and a stale-lock profile needs this note EXACTLY as much as a
// never-started one — keyed on the letter alone, half the dead-merger shapes
// would have silently lost it.
func dispositionMergeNote(state string) string {
	if !StateHasLiveMerger(state) {
		return " — the merger is NOT running, so none of the remedies below will run by itself"
	}
	return ""
}

// StateHasLiveMerger reports whether a status state means a merger IS running —
// the ONE predicate for "will anything act on this by itself", so no caller
// re-spells the set of live states (Rule 13/14). The three live states are
// exactly the ones whose headline says a merge process (or a merge round) is
// alive; every other state, INCLUDING a state this package has never seen, reads
// as NOT live: the fail-closed direction, because the sentence it guards tells an
// operator a remedy will not run on its own.
func StateHasLiveMerger(state string) bool {
	switch state {
	case StatusAlive, StatusOrphanLocked, StatusTakeoverUnlocked:
		return true
	default:
		return false
	}
}

func renderWithheld(groups []branchDisposition) string {
	var b strings.Builder
	for _, g := range groups {
		if g.Kind != branchDispositionWithheld {
			continue
		}
		fmt.Fprintf(&b, dispositionWithheldHeaderFmt, len(g.Names))
		for i, name := range g.Names {
			extra := ""
			if i < len(g.InMaster) && g.InMaster[i] {
				extra = dispositionWithheldInMasterNote
			}
			// The checkout's AGE rides the line when it was measured (index-
			// aligned like every other per-name fact here): the operator asked
			// for it by name — "just report how long the worktree/branch lives,
			// which can arouse user attention" — and it is the one fact that
			// stays true no matter which liveness probe was blind. An unmeasured
			// age prints nothing rather than a zero (Rule 15).
			age := ""
			if i < len(g.AgesKnown) && g.AgesKnown[i] {
				age = fmt.Sprintf(withheldCheckoutAgeFmt, candidateAgeWords(g.Ages[i]))
			}
			fmt.Fprintf(&b, dispositionWithheldLineFmt, name, verdictOperatorWords(g.Verdicts[i]), extra, age+g.HeldReason[i])
		}
	}
	return b.String()
}

func renderUnowned(groups []branchDisposition, total int) string {
	var b strings.Builder
	for _, g := range groups {
		if g.Kind != branchDispositionUnowned {
			continue
		}
		fmt.Fprintf(&b, dispositionUnownedHeaderFmt, total)
		for _, name := range g.Names {
			fmt.Fprintf(&b, dispositionUnownedLineFmt, name)
		}
	}
	return b.String()
}
