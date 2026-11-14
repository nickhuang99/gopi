package main

// runTaskMerge — the `gopi task merge` CLI face (internal/tools/task_merge.go
// engine): branch → master integration with git as the arbiter. Conflict
// semantics: exit non-zero (script truth — the hook logs it), master left
// untouched; with --submit the conflict lands in the ledger for an executor
// agent. Output is one grep-able line per branch (e2e assertions key on it).

import (
	"fmt"
	"os"

	"github.com/nickhuang99/gopi/internal/tools"
)

// runTaskMerge executes the merge face. Positional branches merge in order;
// --all sweeps every local branch except master (engine-side). Exit codes:
// 0 all clean/deleted/deduped, 1 conflict or a merge-time CLI contract
// refusal present (or engine error), 2 any per-branch error.
func runTaskMerge(opts *tasksOptions) {
	// Single-instance guard FIRST — before the engine touches a ref: this face
	// lands branches and deletes them, i.e. it IS merge work (epic
	// merger-always-flock; measured: it advanced master while a live merger
	// held serve.lock, because the post-receive hook triggers it out of band).
	// A refusal names the holder and exits 1; the branches stay put for the
	// next merger pass.
	defer mergerWorkLock(mergeFaceProg)()
	counts, err := tools.TaskMerge(tools.TaskMergeOptions{
		Repo:   opts.repo,
		Submit: opts.submit,
		DBPath: opts.db,
	}, opts.mergeBranches, opts.mergeAll)
	if err != nil {
		fmt.Fprintf(os.Stderr, "gopi task merge error: %v\n", err)
		os.Exit(1)
	}
	for _, o := range counts.Outcomes {
		fmt.Printf("merge %-32s %-10s %s\n", o.Branch, o.Action, o.Detail)
	}
	fmt.Printf("task merge (%s): repos=%d merged=%d deleted=%d dispatched=%d duped=%d concluded=%d rework-owned=%d conflict=%d exempt=%d contract-refused=%d build-refused=%d "+tools.TaskMergeActionHeadOffIntegration+"=%d failed=%d\n",
		map[bool]string{false: "no-submit", true: "submit"}[opts.submit],
		counts.Repos,
		counts.Merged, counts.Deleted, counts.Dispatched, counts.Duplicated,
		counts.Concluded, counts.ReworkOwned,
		counts.Conflicted, counts.Exempt, counts.ContractRefused, counts.BuildRefused,
		counts.HeadOffIntegration, counts.Failed)
	// Rebuild-after-merge (epic 2026-09-02): the manual merge round's LAST
	// step — branches landed (merged/deleted), so the repo's declared build
	// runs to make the binary follow master. No build contract → silent
	// skip. Zero merges (conflict-only / nothing to do) → no rebuild.
	//
	// The head-off-integration refusal does not reach here as a merge, so this
	// step is skipped by the counts it is given — deliberately: a repo whose
	// checkout is on another branch was not built, not merged, and must not be
	// treated as if it had landed something (the refusal's own line carries the
	// remedy).
	// The per-repo tallies ride through: a repos-table sweep that landed work in
	// SEVERAL repos must rebuild EACH one (Rule 12's close loop is per repo), and
	// a union total cannot name them.
	runTaskMergeRebuildTallies(opts, counts.Merged, counts.Deleted, tools.MergedBranchNames(counts.Outcomes), counts.RepoTallies)
	switch {
	case counts.Failed > 0:
		os.Exit(2)
	case counts.Conflicted > 0 || counts.Dispatched > 0 || counts.ContractRefused > 0 ||
		counts.BuildRefused > 0 || counts.HeadOffIntegration > 0:
		// A refusal is a non-zero exit for the same reason the gates above are:
		// a script or a post-receive hook reading this face must not be told
		// "decided everything" when the work is still in the queue (Rule 15 —
		// the status is the machine-readable half of the refusal).
		os.Exit(1)
	}
}
