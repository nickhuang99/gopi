package main

import (
	"fmt"
	"os"

	"github.com/nickhuang99/gopi/internal/scratch"
)

// cli_scratch_gc_roots.go — the WORKTREE-ROOT CENSUS arm:
// `gopi tool scratch-gc roots [--explain] [--dry-run] [--yes] [--root DIR]`.
//
// WHAT IT ANSWERS (the question nothing could answer before it, measured
// 2026-10-07): "what is directly under my worktree root, and which of these
// children is actually a worktree?" The root held 95 `gopi-*` directories plus
// ~98 children in other naming schemes, and exactly SEVEN were `git worktree
// list` registrations. Every sweep in internal/scratch is namespace- or
// root-limited, so the ad-hoc families in this SHARED root were in nobody's
// scope and the tree simply read as "many worktrees".
//
// FOUR CLASSES, ONE REPORT: each direct child prints its class — `worktree` (a
// live registration; never a candidate at any age), `stub` (the dead test-leak
// shape the existing stub arm collects; the ONE removable class), `residue` (an
// ad-hoc name of this system's own making that is not a worktree — REPORTED,
// never removed, because these are the evidence directories reports cite by
// name), `foreign` (a name this system does not mint — out of scope at any
// age). Every line carries the sentence that decided it, so a zero is a readable
// statement rather than an absence.
//
// THE DELETING FACE IS THE EXISTING ONE, AND IT IS EXPLICIT ONLY: the removal
// half delegates to sweepWorktreeStubs — the SAME predicate and the SAME sweep
// the `stubs` arm uses — so this arm cannot acquire a second, wider idea of what
// is removable. The default is the report, `--yes` is the confirmation the
// stubs arm already requires, and the resident tick reaches the REPORT only.
// Two reasons, neither cosmetic (the `profile` arm's ruling, same verb): the
// root is a directory the OPERATOR owns and shares with their real worktrees,
// and a report that can delete is one ambiguity away from the incident this arm
// exists to prevent.
//
// The predicate lives in internal/scratch/worktree_root_residue.go; this file
// drives it and decides nothing (Rule 3/14).

// scratchGCRootsFlag is the arm selector: `scratch-gc roots`.
const scratchGCRootsFlag = "roots"

// scratchGCRootsRole labels the worktree-root arm, so an operator tells the arms
// apart by the `role=` field alone.
const scratchGCRootsRole = "cli-roots"

// scratchGCRootsLogFmt is the worktree-root arm's count line: the shared
// scratchGCLogFmt shape EXTENDED with the four class counts this arm exists to
// report. The residue count is the one an operator acts on, so it prints even
// when zero (Rule 15: a fact readable only from a non-zero line cannot be
// distinguished from an arm that never ran).
//
// Composed from the shared format (Rules 13/14), so a field added to the family
// format lands here automatically.
const scratchGCRootsLogFmt = scratchGCLogFmt + " root=%s children=%d worktree=%d stub=%d residue=%d foreign=%d"

// scratchGCRootsChildLogFmt is the PER-CHILD line: the class, the path and the
// sentence that decided it — what makes the report usable without a second
// command.
const scratchGCRootsChildLogFmt = "root-child class=%s path=%s removable=%t reason=%s"

// scratchGCRootsRemovableMismatchFmt is the sentence a dry-run-then-apply flow
// must never produce: the census and the sweep disagreeing about the removable
// set. A mismatch is an ERROR, never a warning — the operator vetted a set, and
// a different set went.
const scratchGCRootsRemovableMismatchFmt = "the census named %d removable stub(s) but the sweep removed %d — the two disagree"

// runScratchGCRoots censuses every resolved worktree root and, only with `--yes`,
// delegates the ONE removable class to the existing stub sweep.
//
// exit codes: 0 when every root was censused (including a root that does not
// exist yet — "this host has no worktree root" is a reported no-op, not an
// alarm); scratchGCExitError when a root is REFUSED (a mistyped path is a
// configuration error the operator must see), when a census failed, or when a
// requested removal failed.
func runScratchGCRoots(dryRun, yes, explain bool, rootFlag string) {
	type target struct{ root, source string }
	var targets []target
	if rootFlag != "" {
		targets = append(targets, target{root: trimmedRoot(rootFlag), source: "explicit " + scratchGCStubsRootFlag})
	} else {
		for _, r := range stubWorktreeRoots(true) {
			targets = append(targets, target{root: r.root, source: r.source})
		}
	}
	if len(targets) == 0 {
		fmt.Fprintf(os.Stderr, "gopi %s %s: no worktree root to census\n", CliVerbScratchGC, scratchGCRootsFlag)
		os.Exit(scratchGCExitError)
	}
	apply := yes && !dryRun
	for _, t := range targets {
		// The refusal runs BEFORE any measurement, apply or not: a refused root
		// must be NAMED, not inspected (Rule 15).
		if rerr := scratch.WorktreeStubRootRefusal(t.root); rerr != nil {
			fmt.Fprintf(os.Stderr, "gopi %s %s: %v\n", CliVerbScratchGC, scratchGCRootsFlag, rerr)
			os.Exit(scratchGCExitError)
		}
		plans, err := scratch.PlanWorktreeRootCensus(t.root)
		if err != nil {
			fmt.Fprintf(os.Stderr, "gopi %s %s: %v\n", CliVerbScratchGC, scratchGCRootsFlag, err)
			os.Exit(scratchGCExitError)
		}
		counts := map[scratch.RootChildClass]int{}
		removable := 0
		for _, p := range plans {
			counts[p.Class]++
			if p.Removable {
				removable++
			}
			// WHICH LINES PRINT: the classes an operator with no other arm for
			// them (residue, stub) always; the worktree and foreign classes only
			// under --explain, because a real worktree roster is the ledger's
			// business and printing ~95 lines of them by default would bury the
			// ones that matter (the profile arm's quiet default, same reason).
			if p.Class == scratch.RootChildResidue || p.Class == scratch.RootChildStub || explain {
				fmt.Printf(scratchGCRootsChildLogFmt+"\n",
					scratch.RootChildClassLabel(p.Class), p.Path, p.Removable, p.Reason)
			}
		}
		// The count line. `disabled` is FALSE unconditionally: it is the shared
		// format's account of the SWEEP'S off switch, which this arm does not
		// read (a census is the thing an operator does BECAUSE they disabled the
		// sweeps). This arm's own account of whether it applied anything is
		// scratchGCDryRunSuffix, appended only when it did not — the profile arm's
		// convention, so a log reader tells the two apart by the field's name and
		// not by its position.
		countLine := scratchGCRootsLogFmt
		if !apply {
			countLine += scratchGCDryRunSuffix
		}
		fmt.Printf(countLine+"\n",
			scratchGCRootsRole, t.root, 0, scratch.StubNoAgeWindow, false, t.root, len(plans),
			counts[scratch.RootChildLiveWorktree], counts[scratch.RootChildStub],
			counts[scratch.RootChildResidue], counts[scratch.RootChildForeign])
		if !apply {
			continue
		}
		// The EXPLICIT deleting face, and it is the EXISTING one by construction:
		// one call, one predicate, nothing widened here.
		removed, bytes, serr := scratch.SweepWorktreeStubs(t.root)
		fmt.Printf(scratchGCStubsLogFmt+"\n",
			scratchGCRootsRole, t.root, removed, scratch.StubNoAgeWindow, false, bytes)
		if serr != nil {
			fmt.Fprintf(os.Stderr, "gopi %s %s: removal failed after %d entry(ies): %v\n",
				CliVerbScratchGC, scratchGCRootsFlag, removed, serr)
			os.Exit(scratchGCExitError)
		}
		// The invariant a dry-run-then-apply flow promises: the stubs the census
		// called removable are the ones that went.
		if removed != removable {
			fmt.Fprintf(os.Stderr, "gopi %s %s: %s: "+scratchGCRootsRemovableMismatchFmt+"\n",
				CliVerbScratchGC, scratchGCRootsFlag, t.root, removable, removed)
			os.Exit(scratchGCExitError)
		}
	}
}
