package main

import (
	"fmt"
	"os"
	"sort"

	"github.com/nickhuang99/gopi/internal/config"
	"github.com/nickhuang99/gopi/internal/scratch"
	"github.com/nickhuang99/gopi/internal/worktreeroots"
)

// cli_scratch_gc_stubs.go — the OPERATOR-FACING half of the worktree-stub sweep:
// `gopi tool scratch-gc stubs`.
//
// WHAT IT RECLAIMS, AND WHY EVERY ROOT IS NAMED BEFORE ANYTHING IS DELETED: the
// predicate (internal/scratch/worktree_stub.go) collects `gopi-<row stem>`
// directories holding nothing but a `.git` pointer to a gitdir that died with the
// suite run that created it — the residue of a test-process worktree leak in a
// REAL worktree root. The roots are therefore directories the OPERATOR owns and
// shares with their own worktrees, so this face (a) ENUMERATES its roots and
// prints each one, (b) plans and prints a verdict per candidate, and (c) removes
// nothing unless the operator asks with `--yes`. A dry run is the default for the
// same reason the plan exists at all: the deletion an operator vetted must be the
// deletion that happens.
//
// WHY THERE IS NO AGE WINDOW HERE (the `--stale-min` flag the other arms take):
// every other arm bounds a tree that a live run is still WRITING, so it needs
// "old enough to be dead". A stub is not aged-out — it is DEGENERATE: its only
// content points at a path that no longer exists, in a run directory this
// repository's own suite entry point created. Age would add a second, weaker axis
// and would let a fresh leak survive; the predicate's own provenance condition
// already establishes "produced by a finished test run".

// stubWorktreeRoot is one root this arm may sweep, with the reason it is on the
// list — REPORTED, so an operator reading a dry run can see which configuration
// put a directory there.
//
// scratchGCStubsVerdictKeep / scratchGCStubsVerdictWouldRemove are the two
// verdict words the plan prints. They are constants because the per-candidate
// verdict line is a CONTRACT three readers depend on: the operator vetting a dry
// run, the summary line's `would-remove=` count, and the help-surface gate's
// typed case — which derives its assertion from the same const (Rule 1: a value
// compared or printed is named, never typed inline).
const (
	scratchGCStubsVerdictKeep        = "keep"
	scratchGCStubsVerdictWouldRemove = "would-remove"
	// scratchGCStubsDryRunField is the field the arm's log line carries for its
	// own account of whether it applied anything. The dry run is the arm's
	// DEFAULT, so this word is read by three separate consumers — the operator
	// vetting a plan, whatever greps the shared log format, and the help-surface
	// gate's typed case — and a reworded field must reach all three at once.
	scratchGCStubsDryRunField = "dry-run="
)

type stubWorktreeRoot struct {
	root   string
	source string
}

// stubWorktreeRoots enumerates the roots the sweep covers, in a deterministic
// order (sorted, so two runs of the same configuration cannot disagree about
// what they would sweep — the acceptance check compares the dry run with the
// apply).
//
// The list is exactly "every root a task's ground can actually resolve to",
// because that is where the leak lands, and it is derived from the ONE resolver
// rather than from a path spelled here (Rule 14):
//
//  1. worktreeroots.Resolve("") — the operator's override (GOPI_WORKTREE_ROOT)
//     when set, else the DEFAULT root ($HOME/workspace). The measured leak
//     landed in the default root, so the default MUST be covered or the arm
//     would report "nothing to do" on the very tree it exists for.
//  2. each configured repo's resolved worktree root (config.ResolveRepoWorktreeRoot)
//     — a repo that declares `worktree_root` in the settings repos table has its
//     grounds placed there instead, so its leaks land there too.
//
// A root that cannot be resolved (no HOME, an unreadable settings file) is
// REPORTED and skipped: this face must never fail a whole sweep because one
// configured repo is broken (Rule 15 — the skip is named, never silent).
func stubWorktreeRoots(report bool) []stubWorktreeRoot {
	seen := map[string]bool{}
	var out []stubWorktreeRoot
	add := func(root, source string) {
		root = trimmedRoot(root)
		if root == "" || seen[root] {
			return
		}
		seen[root] = true
		out = append(out, stubWorktreeRoot{root: root, source: source})
	}

	if r, err := worktreeroots.Resolve(""); err != nil {
		if report {
			fmt.Fprintf(os.Stderr, "gopi %s %s: no default worktree root to sweep: %v\n",
				CliVerbScratchGC, scratchGCStubsFlag, err)
		}
	} else {
		add(r.Path, "default ("+r.Source+")")
	}

	repos, err := config.ResolveMergerRepos()
	if err != nil && report {
		fmt.Fprintf(os.Stderr, "gopi %s %s: the settings repos table could not be read (%v) — only the default worktree root is swept\n",
			CliVerbScratchGC, scratchGCStubsFlag, err)
	}
	for _, repo := range repos {
		r, rerr := config.ResolveRepoWorktreeRoot(repo)
		if rerr != nil {
			if report {
				fmt.Fprintf(os.Stderr, "gopi %s %s: repo %s declares no usable worktree root: %v\n",
					CliVerbScratchGC, scratchGCStubsFlag, repo, rerr)
			}
			continue
		}
		add(r.Path, repo+" ("+r.Source+")")
	}

	sort.Slice(out, func(i, j int) bool { return out[i].root < out[j].root })
	return out
}

// trimmedRoot normalizes a resolved root for comparison purposes only: the path
// the sweep is handed stays the resolver's own answer (canonicalization is the
// resolver's job, Rule 14).
func trimmedRoot(root string) string {
	for len(root) > 1 && root[len(root)-1] == os.PathSeparator {
		root = root[:len(root)-1]
	}
	return root
}

// runScratchGCStubs plans (always) and sweeps (only with --yes) the worktree
// roots. `--root` restricts the whole run to ONE named root, which is how a dry
// run and the apply that follows it are guaranteed to look at the same tree.
//
// exit codes: 0 when every root was resolved and judged (INCLUDING a root where
// nothing matched — "nothing to do" is success), scratchGCExitError when a root
// is REFUSED (a mistyped path is a configuration error the operator must see) or
// when a removal failed.
func runScratchGCStubs(dryRun, yes bool, rootFlag string, explain bool) {
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
		fmt.Fprintf(os.Stderr, "gopi %s %s: no worktree root to sweep\n", CliVerbScratchGC, scratchGCStubsFlag)
		return
	}

	// The destructive step requires BOTH --yes and the absence of --dry-run:
	// --dry-run is the arm's stated contract, and an operator who spells both has
	// asked for a report (the safe reading of an ambiguous request).
	apply := yes && !dryRun
	refused := 0
	for _, t := range targets {
		// The root guard runs BEFORE anything else, apply or not: a refused root
		// must be NAMED, not inspected (Rule 15) — and a dry run over the
		// filesystem root is as much a mistake as a sweep of it.
		if err := scratch.WorktreeStubRootRefusal(t.root); err != nil {
			fmt.Fprintf(os.Stderr, "gopi %s %s: %v\n", CliVerbScratchGC, scratchGCStubsFlag, err)
			refused++
			continue
		}
		plans, err := scratch.PlanWorktreeStubs(t.root)
		if err != nil {
			fmt.Fprintf(os.Stderr, "gopi %s %s: plan for %s failed: %v\n",
				CliVerbScratchGC, scratchGCStubsFlag, t.root, err)
			refused++
			continue
		}
		removable, kept := 0, 0
		for _, p := range plans {
			if p.Keep {
				kept++
				continue
			}
			removable++
		}
		// The verdict lines: printed for the plan always, and for the sweep's
		// kept candidates when --explain asks (a sweep that removes something
		// out of a root the operator owns owes them the reasons for what stayed).
		if !apply || explain {
			fmt.Fprintf(os.Stderr, "gopi %s %s: root=%s source=%s candidates=%d would-remove=%d keep=%d\n",
				CliVerbScratchGC, scratchGCStubsFlag, t.root, t.source, len(plans), removable, kept)
			for _, p := range plans {
				verdict := scratchGCStubsVerdictKeep
				if !p.Keep {
					verdict = scratchGCStubsVerdictWouldRemove
				}
				fmt.Printf("%s %s reason=%q\n", verdict, p.Path, p.Reason)
			}
		}

		if !apply {
			// The stub arm's line: the shared `stale=` field carries
			// StubNoAgeWindow — the word "none" — because this arm applies no age
			// threshold at all (see the file header and scratch.StubNoAgeWindow).
			fmt.Printf(scratchGCStubsLogFmt+" "+scratchGCStubsDryRunField+"true\n",
				scratchGCStubsRole, t.root, removable, scratch.StubNoAgeWindow, false, int64(0))
			continue
		}

		removed, bytes, serr := scratch.SweepWorktreeStubs(t.root)
		fmt.Printf(scratchGCStubsLogFmt+" "+scratchGCStubsDryRunField+"false\n",
			scratchGCStubsRole, t.root, removed, scratch.StubNoAgeWindow, false, bytes)
		if serr != nil {
			fmt.Fprintf(os.Stderr, "gopi %s %s: sweep of %s failed: %v\n",
				CliVerbScratchGC, scratchGCStubsFlag, t.root, serr)
			os.Exit(scratchGCExitError)
		}
		// The acceptance invariant, printed on the line the operator's own check
		// reads: what was planned is what went. A mismatch would mean the plan and
		// the sweep disagree, which is the ONE thing a dry-run-then-apply flow
		// promises never happens.
		if removed != removable {
			fmt.Fprintf(os.Stderr, "gopi %s %s: %s: planned %d but removed %d — the plan and the sweep disagree\n",
				CliVerbScratchGC, scratchGCStubsFlag, t.root, removable, removed)
			os.Exit(scratchGCExitError)
		}
	}
	if refused > 0 {
		os.Exit(scratchGCExitError)
	}
}
