package tools

// The merger status surface's MERGE-CANDIDATE block — the second half of the
// operator complaint this epic carries:
//
//	fix/merger-tick-takeover-gate (live-holder) — a live run is working in its checkout
//
// An operator reading that line could not tell what it MEANT. Two distinct
// facts were welded into one sentence, and only one of them was measured:
//
//	① the branch is not a merge candidate because a CHECKOUT holds it — a fact
//	   about git registration, measured (`git worktree list` + the directory);
//	② "a live run is working in its checkout" — a fact about the LEDGER: a
//	   branch-attribution verdict (`live-holder`) that says a TASK ROW's recorded
//	   runner is alive. The row's runner is a `gopi worker` process; it is NOT
//	   measured to be standing in the checkout, and the same surface elsewhere
//	   said so in as many words ("holder liveness is not measured here").
//
// MEASURED on the live repo while writing this file (2026-09-18): both branches
// the line flagged (one of them a row already `completed`) had ZERO processes
// with a working directory inside their checkouts — the confident sentence and
// the honest disclaimer were describing different axes.
//
// THE OPERATOR'S MODEL IS THE CONTRACT (the epic's own words): a branch either
// (a) is not protected by a checkout, so it IS a merge candidate and the next
// sweep tries the ff-merge, or (b) is under a worktree, so it is NOT a
// candidate. That is the takeover's whole duty. This block therefore answers
// ONE question per branch — CANDIDATE: yes or no — and, when the answer is no,
// the MEASURED reason:
//
//	a checkout holds it … and no live process is inside that checkout
//	a checkout holds it … and a live process IS working in it
//
// The second clause is the "little branch-development monitoring" this file was
// written to answer (for the now-retired merger status render): check whether a
// worktree is still under development by checking whether a process is actually
// running inside it — the /proc holder scan the worktree sweep already uses for
// safety, never a re-inference from the ledger (retired status render).
//
// NOTHING IS DELETED TO ACHIEVE THIS. The `withheld:` list keeps its branches
// and its remedy sentences (that population and its remedies are unchanged);
// what this block adds is the ANSWER SHAPE the operator asked for, in their own
// vocabulary, with each sentence traceable to a measurement.
//
// WHERE THE FACTS COME FROM (no re-derivation — Rule 13): the same
// branch-attribution verdicts and the same measured ground the protected line
// already renders (BranchAttributionLine.Checkout / .Held). This file adds no
// probe of its own: it GROUPS and RENDERS evidence the preview already carries.

import (
	"fmt"
	"strings"
	"time"
)

// The candidate ANSWER — the closed set of two, named because the render, the
// monitor tally and the tests compare the same words (Rule 1b: a bare `"yes"`
// inlined in three places is a drift waiting to happen).
const (
	mergeCandidateYes = "candidate"
	mergeCandidateNo  = "NOT a candidate"
)

// candidateCheckoutClause is the MEASURED first clause of a non-candidate line:
// git refuses the branch because a registered checkout exists on disk. The path
// is appended by the render.
const candidateCheckoutClause = "a checkout holds it"

// candidateReleaseNote says what the merger does about a non-candidate — the
// operator's "so when will it merge?" question, answered without a promise the
// merger cannot keep (the remedy arms' own verdicts still decide).
const candidateReleaseNote = "the merger can merge it only after that checkout is released"

// candidateLiveClause is the SECOND measured clause: a live process was found
// (the /proc holder scan) with its working directory, an open fd, a mapped
// binary or its executable inside the checkout. This is the sentence the epic's
// misreading was about — it now states a MEASUREMENT, not a ledger opinion.
const candidateLiveClause = "and a live process IS working in that checkout"

// candidateNothingInsideClause is the measured NEGATIVE of that same probe: the
// holder scan ran and found nobody inside.
//
// IT DELIBERATELY DOES NOT SAY "the run is over". The probe measures the
// CHECKOUT (cwd / open fds / mapped binary / exe — scratch.HeldByProcesses), and
// a task's runner does not have to be visible through any of those four
// channels: a `gopi worker <task-id>` process runs with its SESSION directory as
// its working directory (measured 2026-09-18 in /proc on this host), so a run
// whose tool children have all exited leaves its worktree with nobody inside
// while the run itself is very much alive. The live repo was rendered "NO live
// process is inside that checkout" for exactly such runs — the operator's
// complaint — which is why this sentence now states the MEASUREMENT instead of
// the conclusion (Rule 15: never assert what the probe did not say).
const candidateNothingInsideClause = "and the scan found no process inside that checkout"

// candidateUnmeasuredClause is the third state of the same probe: it could not
// be run (no /proc). "Not measured" must never render as "nobody inside" (Rule
// 15) — the render says which of the two happened.
const candidateUnmeasuredClause = "and whether a process is inside could not be measured (the /proc scan was unreadable)"

// The ROW's own axis, rendered BESIDE the checkout's — the second, independent
// measurement of "is this task still running", from the ledger's recorded runner
// identity (pid + kernel start ticks + cmdline): the same three-factor identity
// `gopi task show` reports and the operator can re-probe with one `ps`. Printing
// both axes is the fix for the collapsed sentence: each clause states only what
// its own probe said.
const (
	// candidateRunnerLiveClause: the row's recorded runner incarnation is alive.
	candidateRunnerLiveClause = "the task row's recorded runner is still alive"
	// candidateRunnerNotLiveClause: the row records a runner identity and this
	// host cannot see it alive — said as the ROW's liveness, never as a
	// measurement of the checkout.
	candidateRunnerNotLiveClause = "the task row's recorded runner is NOT alive"
	// candidateRunnerUnknownClause: the row records no runner identity, so this
	// axis is UNVERIFIED — said rather than silently omitted, because an omitted
	// clause reads as a clean measurement.
	candidateRunnerUnknownClause = "the task row records no runner identity (not verified)"
	// candidateCheckoutAgeClause is the signal the operator asked for by name: how
	// long the checkout has existed ("just report how long the worktree/branch
	// lives, which can arouse user attention"). It claims nothing about the run —
	// it is a measurement of the registration, and it is the honest way to raise
	// suspicion about a checkout nobody has released.
	candidateCheckoutAgeClause = "the checkout has stood for %s"
)

// candidateCandidateNote is the YES answer's sentence: the branch is not
// protected, so it is the merger's work.
const candidateCandidateNote = "no checkout holds it — the next sweep tries the ff-merge against " + masterBranchName

// candidateNoRowNote is the branch↔task GAP, stated rather than hidden: the
// checkout is held by nobody the ledger can name. The epic's expectation is
// that whoever claims a task and creates its worktree records that ground on
// the task row; until it is recorded, the merger can only report the gap.
const candidateNoRowNote = "no ledger row names this branch (a gap: the run holding this checkout should record it on its task)"

// The render's format strings, named once (Rule 1b/14).
const (
	// candidateHeaderFmt heads the block with the COUNT ANSWER: how many of the
	// repo's heads the merger could actually try to merge this tick.
	candidateHeaderFmt = "  candidates: %d of %d branch(es) answer the merge question — a candidate has no checkout holding it, and the next sweep tries its ff-merge:\n"
	// candidateLineFmt is ONE branch's answer: its name, the yes/no, and the
	// measured reason.
	candidateLineFmt = "    - %s — %s: %s\n"
	// candidateMonitorFmt is the (retired merger status render's) monitor:
	// of the repo's heads, how many sit in a checkout at all, how many of those
	// have no process inside them, and how many are being worked in right now.
	// It counts MEASUREMENTS only — it does not claim the "no process inside"
	// ones are finished runs, because that is the one thing the scan cannot
	// establish (a `gopi worker`'s cwd is its session directory, not its
	// worktree); the per-branch lines name the row axis beside it for exactly
	// that reason.
	candidateMonitorFmt = "  monitor:   %d of %d head(s) sit in a checkout: %d with no process inside and %d being worked in right now — a checkout with no process inside is NOT proof its run ended (see each line's task-row clause)\n"
)

// mergeCandidateLine is ONE branch's answer to the merge question, assembled
// from measurements only. It exists as a function so the monitor tally and the
// per-branch lines cannot disagree about what was measured (Rule 14).
type mergeCandidateLine struct {
	Branch string
	Yes    bool
	// Note is the MEASURED clause behind the answer (a sentence, never a label).
	Note string
	// Held records whether the holder scan found a live process in the
	// checkout. Only meaningful when Yes is false.
	Held bool
	// Inside names the live processes the scan DID find in the checkout
	// ("pid 1234 (cwd …)"), so a positive sentence carries its own evidence.
	Inside []string
	// ProbeMeasured records that the checkout scan actually RAN. A scan that
	// could not run must never render as "nobody inside" (Rule 15) — the
	// render says which of the two happened.
	ProbeMeasured bool
	// RunnerLive/RunnerKnown are the SECOND, independent axis: the ledger row's
	// own recorded runner identity (pid + start ticks + cmdline), the same
	// three-factor identity `gopi task show` reports. RunnerKnown says the row
	// records an identity at all; RunnerLive says this host can see it alive.
	// The two axes are rendered side by side because ONE of them being blind is
	// exactly what produced the operator's complaint.
	RunnerLive  bool
	RunnerKnown bool
	// Age/AgeKnown are the checkout's registration age — the operator's own
	// requested signal ("how long the worktree/branch lives").
	Age      time.Duration
	AgeKnown bool
	// Task names the owning ledger row when one exists ("" = the gap above).
	Task string
}

// mergeCandidateLines answers the merge question for every attributed branch of
// the repo, sorted by name.
func mergeCandidateLines(lines []BranchAttributionLine) []mergeCandidateLine {
	out := make([]mergeCandidateLine, 0, len(lines))
	for _, l := range lines {
		if l.Verdict == string(branchAttribIntegration) {
			continue // master/main/archive namespaces answer no question
		}
		mc := mergeCandidateLine{
			Branch: l.Branch, Task: l.Task,
			Inside: l.Inside, ProbeMeasured: l.ProbeMeasured,
			RunnerLive: l.RunnerLive, RunnerKnown: l.RunnerKnown,
			Age: l.Age, AgeKnown: l.AgeKnown,
		}
		if !l.Checkout {
			// git does not report a checkout holding this branch: it is the
			// merger's work, full stop. This is the (a) half of the operator's
			// own model.
			mc.Yes = true
			mc.Note = candidateCandidateNote
			// …AND THE OWNER TRAVELS WITH IT (epic 01a0b182). A candidate that a
			// live task row names is NOT anonymous free work: the merger's
			// monitor duty is precisely to tell "nobody owns this, merge it" from
			// "a run owns this and is still working" — and before the claim-/
			// creation-time branch stamp existed, the second population was
			// invisible here (its row named no branch at all, so the branch was
			// classified `unowned`). The clause is the SAME one the non-candidate
			// arm prints (ownerClause — one spelling of "who owns this and is
			// their runner alive"), never a second mapper. A candidate no row
			// names stays silent: that IS the merger's own work, and the no-row
			// GAP note belongs to a checkout nobody owns.
			if l.Task != "" {
				mc.Note = candidateCandidateNote + " — " + ownerClause(l)
			}
			out = append(out, mc)
			continue
		}
		// (b): a checkout holds it. WHETHER a run is inside that checkout is the
		// MEASURED half (Held), never the ledger's opinion of the row's runner —
		// the conflation this epic reported. And because that measurement can be
		// BLIND to a live run, the row's own runner liveness is printed beside
		// it rather than being replaced by it.
		mc.Held = l.Held
		mc.Note = fmt.Sprintf("%s (%s) %s — %s; %s",
			candidateCheckoutClause, l.Worktree, checkoutClause(l), ownerClause(l), candidateReleaseNote)
		out = append(out, mc)
	}
	return out
}

// checkoutClause renders the checkout-axis measurement of a
// checkout-holding branch: the scan's own verdict (found / found nobody / could
// not be measured) plus, when it found somebody, the evidence it found.
func checkoutClause(l BranchAttributionLine) string {
	// Held FIRST: a positive hold needs no further qualification (a positive
	// observation is its own proof), and ordering the flag check ahead of it
	// would render the strongest evidence — a named process standing in the
	// checkout — as "could not be measured". The flag decides only between the
	// two NEGATIVE readings, where "the scan found nobody" and "the scan never
	// ran" are different statements (Rule 15).
	switch {
	case l.Held:
		return candidateLiveClause + insideClause(l.Inside)
	case !l.ProbeMeasured:
		return candidateUnmeasuredClause
	default:
		return candidateNothingInsideClause
	}
}

// insideClause names the processes a positive scan found, when the line carries
// them — a sentence about "a live process" is worth more when it can name it.
func insideClause(inside []string) string {
	if len(inside) == 0 {
		return ""
	}
	return " (" + strings.Join(inside, "; ") + ")"
}

// ownerClause renders the row axis: who the branch is attributed to, the ROW's
// own recorded runner liveness, and the checkout's age. Nothing is invented for
// a branch no row names — that gap is stated as a gap (candidateNoRowNote).
func ownerClause(l BranchAttributionLine) string {
	var b strings.Builder
	if l.Task == "" {
		b.WriteString(candidateNoRowNote)
	} else {
		b.WriteString("task " + l.Task)
		switch {
		case !l.RunnerKnown:
			b.WriteString(" — " + candidateRunnerUnknownClause)
		case l.RunnerLive:
			b.WriteString(" — " + candidateRunnerLiveClause)
		default:
			b.WriteString(" — " + candidateRunnerNotLiveClause)
		}
	}
	if l.AgeKnown {
		b.WriteString(fmt.Sprintf("; "+candidateCheckoutAgeClause, candidateAgeWords(l.Age)))
	}
	return b.String()
}

// candidateAgeWords renders a duration for an operator: coarse on purpose (the
// question it answers is "is this checkout young or has it been standing for a
// while?", not "how many seconds"), and bounded in width so one long-standing
// checkout cannot distort the block.
func candidateAgeWords(d time.Duration) string {
	if d < 0 {
		d = 0
	}
	switch {
	case d < time.Minute:
		return fmt.Sprintf("%ds", int(d.Seconds()))
	case d < time.Hour:
		return fmt.Sprintf("%dm", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh%02dm", int(d.Hours()), int(d.Minutes())%60)
	default:
		return fmt.Sprintf("%dd%02dh", int(d.Hours())/24, int(d.Hours())%24)
	}
}

// renderMergeCandidates renders the block for ONE repo. It is silent only when
// the repo has no attributed branch at all (nothing to answer about) — a repo
// whose branches are all candidates still gets the block, because "every branch
// here is the merger's work" is precisely the reassuring answer this block
// exists to give without the operator having to count.
func renderMergeCandidates(lines []BranchAttributionLine, inv BranchInventory) string {
	mcs := mergeCandidateLines(lines)
	if len(mcs) == 0 {
		return ""
	}
	var yes, idle, live int
	for _, mc := range mcs {
		if mc.Yes {
			yes++
			continue
		}
		if mc.Held {
			live++
		} else {
			idle++
		}
	}
	var b strings.Builder
	fmt.Fprintf(&b, candidateHeaderFmt, yes, inv.Total)
	for _, mc := range mcs {
		answer := mergeCandidateYes
		if !mc.Yes {
			answer = mergeCandidateNo
		}
		fmt.Fprintf(&b, candidateLineFmt, mc.Branch, answer, mc.Note)
	}
	if idle+live > 0 {
		fmt.Fprintf(&b, candidateMonitorFmt, idle+live, inv.Total, idle, live)
	}
	return b.String()
}
