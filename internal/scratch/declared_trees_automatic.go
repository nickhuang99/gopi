// declared_trees_automatic.go — the DECLARED TREES' automatic face: one call
// that sweeps every row of the declared set at its OWN window.
//
// THE DEFECT THIS CLOSES (measured on nick-HP-Laptop, 2026-10-17). `/home`
// (device 66309) reached 100% and a `git worktree add` inside `cmd/agent`'s own
// suite died with `cannot create directory … 设备上没有空间`. `gopi tool
// scratch-gc trees --dry-run` — the ONLY face that can free a declared tree —
// reported, on that host, 45.7 GB collectable in the shared build cache, 5.4 GB
// of aged `*.raw.log` in the sessions tree, 1.27 GB of abandoned `go-build*`
// directories in GOTMPDIR, and 8.1 GB held by the test-budget tree. All of it
// was AGE-AND-HOLDER-vetted and none of it was ever collected, because the
// declared-trees sweep had NO caller outside the CLI verb: the resident tick
// (cmd/agent/scratch_gc_tick.go) swept the home, the memory base, the sandboxes
// and the profile root, and never the declared set — and the host-level
// watchdog that was written to drive the verb
// (`scripts/scratch-gc-watchdog.sh`) is not installed on this host at all
// (measured: no systemd user timer, no crontab line, no copy under
// ~/.local/bin or ~/bin). A tree bounded only by a command nobody runs is
// unbounded.
//
// THE CODE ALREADY SAID THE TICK WAS THE INTENDED CALLER. declared_budget_runs.go
// justifies its row with: "it was never in the declared set, so the resident
// tick could not walk it either" — the row exists so the tick CAN walk the
// tree. The wiring was never done (Rule 41: the mismatch was measured, not
// inferred). This file is that wiring's predicate half; the call site is the
// tick's.
//
// WHY THIS IS NOT A SECOND POLICY (and not a new dependency, Rule 35). Every
// decision stays where it already was: the row's namespace predicate and its own
// age window live in DeclaredTrees(); the root refusal, the live-holder veto and
// the `.holder` registration live in declaredTreeEntries /
// removeCollectableOfTree, and this function calls the SAME SweepDeclaredTree
// the CLI arm calls. Nothing here can reach a path the CLI could not, and
// nothing here widens a window or skips a guard. It adds NO resident process, NO
// schedule, NO state file: it rides the tick that already exists.
//
// WHY AUTOMATIC IS THE RIGHT SHAPE FOR THESE TREES — and why it is NOT for the
// toolchain cache (the distinction matters, and it is measured, not stylistic).
// `gopi tool scratch-gc trees` DELETES on invocation: there is no `--yes`, no
// dry-run requirement, nothing an operator vets at the moment of deletion. Its
// safety argument is entirely in the predicates (namespace + age + no live
// holder + never a declared root), and that argument does not weaken when the
// caller is the tick rather than a human. The agent-side toolchain cache
// (`~/.cache/go-build`) is the opposite case and is deliberately ABSENT from
// this function: toolchain_cache.go states the decision — "the finder of a
// defect proposes the reclaim, the operator decides when it happens" — and its
// CLI face requires an explicit `--trim`. That decision is left exactly as it
// was; this face sweeps the DECLARED TREES only, and the toolchain cache is not
// one of them.
package scratch

import "time"

// DeclaredTreeSweepResult is ONE declared tree's outcome from an automatic pass:
// what the row was, the window actually applied to it, and what came back.
//
// It is carried per tree rather than as a total because the total is the one
// number a reader cannot act on: "removed 45 entries" does not say whether the
// sessions tree or the build cache was touched, and the two carry different
// costs if the window is ever wrong. Err is the row's own failure (an
// unresolvable root, an unreadable tree), reported honestly instead of being
// folded into a zero (Rule 15: "could not look" is never "nothing there").
type DeclaredTreeSweepResult struct {
	// Name is the row's identity — the same string the CLI's `tree=` field and
	// the declared-set gate use (Rule 13: one spelling).
	Name string
	// Age is the window THIS row applied, resolved by its own staleAge. It is
	// reported rather than assumed so a log line can say which clock decided.
	Age time.Duration
	// Removed is how many entries this row collected.
	Removed int
	// Bytes is the measured size of what came back.
	Bytes int64
	// Err is the row's own error, nil when the sweep ran.
	Err error
}

// SweepDeclaredTreesAutomatic sweeps EVERY declared tree at its OWN age window
// and returns one result per row, in the table's order.
//
// A row that cannot be resolved or walked is REPORTED in its own result and does
// not stop the pass: the declared set spans independent trees written by
// different producers (the toolchain, the merge gate, the agent, the test
// budget entry point), and one unreadable tree must never shield the others —
// the same reason queueSweepChildren judges the queue's children one by one.
//
// The caller decides how to render and whether to count; this function deletes
// only what the existing predicates already authorise, and the window it reports
// is the row's OWN (window()) — the same value the sweep is handed, because the
// sweep takes no other.
func SweepDeclaredTreesAutomatic() []DeclaredTreeSweepResult {
	trees := DeclaredTrees()
	out := make([]DeclaredTreeSweepResult, 0, len(trees))
	for _, tree := range trees {
		res := DeclaredTreeSweepResult{Name: tree.Name, Age: tree.window()}
		res.Removed, res.Bytes, res.Err = SweepDeclaredTree(tree)
		out = append(out, res)
	}
	return out
}
