package main

import (
	"errors"
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/nickhuang99/gopi/internal/scratch"
)

// cli_scratch_gc_profile.go — the OPERATOR-FACING half of the PROFILE-ROOT arm:
// `gopi tool scratch-gc profile [--dry-run]`.
//
// WHAT THIS ARM ANSWERS (the question nothing could answer before it): "what is
// directly under my gopi profile root, and which of those children did nothing
// in this repository declare?" — the observation that was missing when four
// hand-made evidence directories (684 MB) sat in ~/.gopi for ten days and every
// existing collector was namespace-limited past them (see
// internal/scratch/profile_root.go for the four misses and the ruling that the
// profile root is a STATE directory, not a scratch one).
//
// THREE CLASSES, ONE REPORT: each direct child is printed with its class —
// `state` (the live state a gopi process reads to be itself), `declared` (a
// child a repository writer creates), `residue` (the one narrow hand-made
// per-run evidence family) — and a child that no row and no family explains is
// printed as `undeclared`. Every line carries the sentence that decided it, so a
// zero is a readable statement rather than an absence.
//
// THE DELETING FACE IS EXPLICIT ONLY, AND THAT IS NOT AN OVERSIGHT: the
// automatic tick calls the REPORT (`--dry-run`'s own function) and has no way to
// reach the removal — `SweepProfileRootResidue` is called from exactly this file
// and from nowhere else. The precedent is measured: a resident tick that deleted
// a family its own comment declared off-limits (memory #7096), on a machine
// where nobody could see it happen. The profile root holds the ledger, so the
// standing decision is REPORT ALWAYS, DELETE ONLY WHEN ASKED BY A HUMAN.
//
// The predicate, the registry and the refusal all live in internal/scratch; this
// file drives them and decides nothing (Rule 3/14).

// scratchGCProfileFlag is the arm selector: `scratch-gc profile`.
const scratchGCProfileFlag = "profile"

// scratchGCProfileRole labels the profile-root arm, so an operator (and the
// tick's own output) tells the arms apart by the `role=` field alone.
const scratchGCProfileRole = "cli-profile"

// scratchGCProfileLogFmt is the profile arm's count line: the shared
// scratchGCLogFmt shape EXTENDED with the three numbers this arm exists to
// report — how many children were seen, how many an operator's registry
// explains, and how many nobody declared. The undeclared count is the one an
// operator acts on, so it is printed even when it is zero (Rule 15: a fact that
// can only be read from a non-zero line cannot be distinguished from an arm that
// never ran).
//
// Composed from the shared format (Rules 13/14), so a field added to the family
// format lands here automatically.
const scratchGCProfileLogFmt = scratchGCLogFmt + " children=%d declared=%d undeclared=%d"

// scratchGCProfileChildLogFmt is the PER-CHILD line: the class, the name and the
// sentence that decided it. It is deliberately its own format — the count line
// above is about the tree, this one about one child — and it is what makes the
// report usable without a second command.
const scratchGCProfileChildLogFmt = "profile-child class=%s name=%s removable=%t reason=%s"

// scratchGCDryRunSuffix is the trailing marker every read-only face of this
// verb prints (the trees/base/sandbox arms spell it the same way), so a log
// reader can tell a report from a run by the line itself. It is a constant
// because three arms and their tests quote it.
const scratchGCDryRunSuffix = " dry-run=true"

// printScratchGCProfileEmpty is the one line a host with no profile root gets:
// the count line with three zeros, so a watchdog that greps for it cannot read
// "nothing to observe" as "the arm never ran" (Rule 15).
func printScratchGCProfileEmpty(root string, dryRun bool) {
	countLine := scratchGCProfileLogFmt
	if dryRun {
		countLine += scratchGCDryRunSuffix
	}
	fmt.Printf(countLine+"\n", scratchGCProfileRole, root, 0, scratch.ResolveStaleAge(), false, 0, 0, 0)
}

// scratchGCProfileClassUndeclared is the class word an undeclared child prints.
// It is a constant because three faces quote it (this report, the tick's line and
// the tests), and a second spelling would be a second policy.
const scratchGCProfileClassUndeclared = "undeclared"

// runScratchGCProfile reports (and, only when the operator asks, reclaims) the
// profile root's direct children.
//
// exit codes: 0 when the tree was walked and judged (including a root that does
// not exist yet — "this host has no profile" is a reported no-op, not an alarm);
// scratchGCExitError when the root is REFUSED (a mistyped path is a
// configuration error the operator must see) or when a requested removal failed.
func runScratchGCProfile(dryRun, explain, yes bool) {
	root, err := scratch.ResolveProfileRoot()
	if err != nil {
		fmt.Fprintf(os.Stderr, "gopi %s %s: %v\n", CliVerbScratchGC, scratchGCProfileFlag, err)
		os.Exit(scratchGCExitError)
	}
	// The refusal runs BEFORE any measurement, apply or not: a refused root must
	// be NAMED, not inspected (Rule 15). It is also, in practice, unreachable
	// from this face — the resolver returns the profile this process is running
	// under — which is exactly why it is asserted here rather than assumed.
	if rerr := scratch.ProfileRootRefusal(root); rerr != nil {
		if errors.Is(rerr, scratch.ErrProfileRootMissing) {
			// A host that never ran gopi has no profile root. There is nothing to
			// observe and nothing to alarm about — and the line says which of the
			// two it is (Rule 15: an absent tree is a measured zero, not a
			// failure).
			printScratchGCProfileEmpty(root, dryRun)
			return
		}
		fmt.Fprintf(os.Stderr, "gopi %s %s: %v\n", CliVerbScratchGC, scratchGCProfileFlag, rerr)
		os.Exit(scratchGCExitError)
	}
	verdicts, err := scratch.ProfileRootReport(root, scratch.ResolveStaleAge())
	if err != nil {
		if errors.Is(err, scratch.ErrProfileRootMissing) {
			printScratchGCProfileEmpty(root, dryRun)
			return
		}
		fmt.Fprintf(os.Stderr, "gopi %s %s: %v\n", CliVerbScratchGC, scratchGCProfileFlag, err)
		os.Exit(scratchGCExitError)
	}
	counts := scratch.TallyProfileRoot(verdicts)
	// PER-CHILD LINES FIRST, so the count line below is a SUMMARY of what the
	// reader has already seen rather than the only place a name appears.
	//
	// WHICH LINES PRINT: every child when --explain asked; otherwise the ones an
	// operator acts on (undeclared, and removable). The quiet default is a
	// deliberate choice — the profile root has ~90 children and most of them are
	// the state this system needs, so printing all of them by default would bury
	// the four lines that matter.
	for _, v := range verdicts {
		if !explain && !v.Collectable && v.Declared {
			continue
		}
		fmt.Printf(scratchGCProfileChildLogFmt+"\n", scratchGCProfileClassOf(v), v.Name, v.Collectable, v.Reason)
	}
	// APPLY ONLY WHEN THE OPERATOR SAID SO. The arm follows the family's own
	// convention (the worktree-stub arm's `--yes`): the DEFAULT is the report,
	// and the removing form is a separate, spelled-out invocation. Two reasons,
	// neither cosmetic: the usage text promises a report for the bare form, and a
	// verb whose default deletes a directory in the operator's state tree is one
	// ambiguity away from the incident this whole arm exists to prevent.
	apply := yes && !dryRun
	countLine := scratchGCProfileLogFmt
	if !apply {
		countLine += scratchGCDryRunSuffix
	}
	fmt.Printf(countLine+"\n", scratchGCProfileRole, root, 0, scratch.ResolveStaleAge(), false,
		counts.Children, counts.Declared, counts.Undeclared)
	if counts.Undeclared > 0 {
		// The ACTIONABLE half: an operator who sees this can look at the names
		// and decide. It goes to stderr so a watchdog that greps stdout for the
		// count line keeps parsing, and it names the command that shows the
		// per-child verdicts.
		fmt.Fprintf(os.Stderr, "gopi %s %s: %d undeclared child(ren) under %s: %s — see them with: gopi %s %s --explain\n",
			CliVerbScratchGC, scratchGCProfileFlag, counts.Undeclared, root,
			strings.Join(counts.UndeclaredNames, ", "), CliVerbScratchGC, scratchGCProfileFlag)
	}
	if !apply {
		if counts.Collectable > 0 {
			fmt.Fprintf(os.Stderr, "gopi %s %s: %d collectable child(ren) reported; add %s to reclaim them\n",
				CliVerbScratchGC, scratchGCProfileFlag, counts.Collectable, scratchGCStubsYesFlag)
		}
		return
	}
	// The EXPLICIT deleting face, and it is narrow by construction: ONE family,
	// aged, and held by no live process.
	removed, bytes, rerr := scratch.SweepProfileRootResidue(root, scratch.ResolveStaleAge())
	if rerr != nil {
		fmt.Fprintf(os.Stderr, "gopi %s %s: removal failed after %d entry(ies): %v\n",
			CliVerbScratchGC, scratchGCProfileFlag, removed, rerr)
		os.Exit(scratchGCExitError)
	}
	// holding=/holding_n= are 0 for this arm because the profile ROOT is an
	// operator-named directory, not a declared tree: its entry rule is the plain
	// scratch-entry rule, and a "how much is this holding" walk would be a
	// second measurement of a tree the profile arm already reports entry by
	// entry (would-remove / removed lines). Named here so the zero is a decision
	// a reader can see, not a field someone forgot to fill in.
	// present= is MEASURED (the named root's existence), for the same reason the
	// declared-tree rows measure it: this arm is about an operator-named
	// directory, and "the sweep found nothing here" and "there is nothing here"
	// must not print the same line. entries= is 0 by the same decision as
	// holding= below: the profile arm reports entry by entry (would-remove /
	// removed lines), so a count here would be a second measurement of the same
	// walk.
	_, statErr := os.Stat(root)
	fmt.Printf(scratchGCTreesLogFmt+"\n", scratchGCProfileRole, root, removed, scratch.ResolveStaleAge(),
		false, scratchGCProfileFlag, statErr == nil, 0, bytes, int64(0), 0)
}

// scratchGCProfileClassOf renders a verdict's class word: the registry row's own
// class when something declared the child, and the undeclared word otherwise —
// one spelling each, so a report reader can group by it.
func scratchGCProfileClassOf(v scratch.ProfileChildVerdict) string {
	if !v.Declared {
		return scratchGCProfileClassUndeclared
	}
	return string(v.Class)
}

// scratchGCProfileUndeclaredNames is the tick's evidence line: the names of the
// undeclared children, sorted, so the resident report is stable across ticks
// (an unstable line would make a log diff useless). Sorted here rather than in
// the shared tally because the tally's order is the report's own.
func scratchGCProfileUndeclaredNames(vs []scratch.ProfileChildVerdict) []string {
	var out []string
	for _, v := range vs {
		if !v.Declared {
			out = append(out, v.Name)
		}
	}
	sort.Strings(out)
	return out
}
