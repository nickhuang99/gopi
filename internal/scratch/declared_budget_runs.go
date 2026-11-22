// declared_budget_runs.go — the declared tree that bounds the test-budget entry
// point's OWN artifact tree: <scratch home>/queue/test-budget.
//
// THE DEFECT THIS CLOSES (measured 2026-10-12, the run that found it). The `/`
// partition reached 0 bytes free mid-run: a `git commit` failed exit 128 and a
// one-byte write returned ENOSPC, while this system's OWN sweep reported it had
// nothing to collect — and 10.0 GB of this tree sat there. The measured census
// of <scratch home>/queue/test-budget: 320 `run-<epoch>-<pid>-tested-bin/`
// directories holding 10.27 GB (ONE ~63 MB built `gopi` binary each — one per
// `make test-budget` run), 120 of them past the 6h window (4.3 GB).
//
// WHY THE EXISTING ARMS COULD NOT SEE IT — and why this is the SAME defect the
// queue arm was written for, stopped one level too high:
//
//	`gopi tool scratch-gc --explain --dry-run` judged exactly ONE entry for the
//	whole subtree — the `test-budget` CONTAINER itself — and kept it:
//	  scratch-verdict …/queue/test-budget verdict=keep age=18s outcome=unregistered
//	    (no .holder record) a live process holds it (the /proc cwd|fd|exe|maps
//	    scan found a live process inside it) [kept]
//	One live `make test-budget` shields all 320 run directories and 10 GB with
//	it. The queue arm exists precisely so a fresh child cannot shield its stale
//	siblings (queueSweepChildren) — this tree sits one level below the queue
//	children the queue arm walks, so nothing reached it.
//
//	`gopi tool scratch-gc trees --dry-run` printed a line for every declared
//	tree and NOT ONE for this one: it was never in the declared set, so the
//	resident tick could not walk it either.
//
// THE SHAPE, MEASURED — and why it is a plain EntryKindDir row. Every DIRECTORY
// under this tree is a run's built-binary directory: `run-<epoch>-<pid>-tested-bin`
// (`scripts/test-budget.sh` `mkdir -p`s it, builds ONE binary into it, and never
// revisits it), and each holds exactly that one flat `gopi`. The tree's other
// children are FLAT: the per-run JSON stream (`run-<tag>.json`), the run's START
// RECORD (`run-<tag>-start.txt` — the run's own name+stream, written before
// `go test` starts so a KILLED run is queryable instead of orphaned), the
// package lists (`run-<tag>-{scope,stream}-packages.txt`), `stamps.log`, and
// `last.json` (a SYMLINK to the newest run's stream). Those are EVIDENCE —
// reports cite them, the script's own readers resolve them — so they are never
// candidates, which is exactly EntryKindDir's rule: a directory's children may be
// collectable, a flat child never is.
//
// THE START RECORD IS CLASSIFIED HERE, deliberately, as that fourth flat
// evidence class (Rule 35: a new persistent artifact is classified where the
// queue's contents are defined). It is a few hundred bytes per run, bounded the
// same way `stamps.log` is — both accumulate one line-shaped entry per run and
// neither is ever collected, because a run's record is exactly what a reader
// needs when the run is MISSING its completion line. Keeping it flat is the
// point: a reader greps the directory for the run's tag and finds the record,
// the stream and the built-binary directory together.
//
// WHAT THIS ROW BOUNDS — AND WHAT IT DOES NOT (measured 2026-10-27, the host;
// this paragraph used to claim the age-GC'd half was "what bounds the tree's
// bytes" and that the records were "noise beside that", and BOTH HALVES OF THAT
// CLAIM ARE FALSE on the live tree): the flat evidence is NOT noise and the tree
// is NOT bounded. Measured on <scratch home>/queue/test-budget AT ONE INSTANT
// (2026-10-27 12:46 +0800; the counts drift upward all day, so re-measure rather
// than quoting these back as current):
//
//	run-*-tested-bin/ DIRECTORIES   433   ~8.7 GiB   ← the only collectable class
//	flat EVIDENCE (streams, records,
//	  package lists, stamps.log)   19790   ~7.0 GiB   ← never a candidate, ANY age
//	                               ─────────────────
//	                               ~15.7 GiB, of which the sweep can only reach
//	                                            about half
//
// and the flat half GROWS with every run (a `run-<tag>.json` stream averages
// 1.38 MiB; measured daily intake 0.25–1.1 GiB). The oldest flat entry on this
// host is 2026-10-12 — FIFTEEN DAYS past every window this row applies — and no
// face reports it: `--explain` prints nothing for it (a flat child is not a
// verdict line), and `holding=` does not count it either (that figure is
// namespace-filtered; see TreeHeldTotal). So a reader who reads `collectable=0
// kept=<n>` on this tree as "nothing here needs attention" is right about the
// DIRECTORIES and wrong about most of the BYTES.
//
// NOT A DEFECT THIS ROW CAN FIX BY WIDENING ITS PREDICATE: a stream older than
// the window is still the EVIDENCE a reader reaches for when a run is missing its
// completion line, so collecting it by this row's 6h window would delete exactly
// what makes a killed run diagnosable. That half is bounded by a SECOND ROW over
// the same root — `budget-evidence` (declared_budget_evidence.go), EntryKindFileMatch
// over `run-<tag>.json`, its own 14-day window plus a newest-N floor — and the
// figure an operator acts on (Rule 41) is the TWO lines together: this one can
// honestly read `collectable=0` while the row beside it plans gigabytes. What
// still has no bound at any age is the per-run INDEX (the START RECORD, the
// package lists, `stamps.log`, the `last.json` symlink), and that is deliberate:
// it is the record that names a run when the run was killed, kept forever for
// ~5 MiB.
//
// BOTH HALVES ARE THEREFORE THE TREE'S OWN NAME RULES, nothing more:
//
//	candidate : a DIRECTORY named `run-<tag>-tested-bin`, aged and unheld
//	            (the generic declared-tree walk: root refusal → age → indexed
//	            /proc holder verdict → .holder registration)
//	kept      : every FLAT entry (an artifact is not a run) and every container
//	            without the suffix
//
// WHAT THE ROW DOES NOT HAVE, on purpose: no inside-the-container arm. A second
// shape would need a second namespace rule, and the measured tree has exactly
// one shape of directory in it. If the entry point ever grows an inner
// per-run directory, the walk's `measureTreeDir` (dirSize) reports it as the
// measured subtree the container's own candidate reclaims, which is the honest
// number: the container is taken WHOLE and its bytes are what came back.
//
// THE ROOT SURVIVES, unconditionally, as every declared tree's root does:
// declaredEntryCollectable refuses a declared root FIRST, and
// removeCollectableOfTree re-checks that identity at the moment of every delete.
// The live-holder veto, the age rule and the `.holder` registration are the
// sweep's existing implementation, reused and never weakened — the point of this
// row is to put the tree IN SCOPE, not to make the sweep reach further.
package scratch

import (
	"path/filepath"
	"strings"
)

const (
	// BudgetDirName is the queue child the test-budget entry point writes its
	// artifacts into — the tree's own leaf name under the queue. It is the ONE
	// spelling of that name in Go (Rule 1): `scripts/test-budget.sh` carries
	// its own `budget_subdir`, and the declared-set gate reads THAT line to hold
	// the two against each other rather than trusting either alone.
	BudgetDirName = "test-budget"

	// BudgetRunsTreeName is this tree's identity on the watchdog log line
	// (`tree=`), in the declared-set gate's diagnostics, and in a failure
	// report. One definition (Rules 13/14).
	BudgetRunsTreeName = "budget-runs"

	// BudgetRunsDirName is the tree's own leaf under the queue. Its value is
	// BudgetDirName — one name, one home, referenced and never re-typed.
	BudgetRunsDirName = BudgetDirName

	// BudgetRunPrefix is a run directory's name prefix: the script names each
	// run `run-<epoch>-<pid>`, which is also how that run's JSON stream and its
	// package lists are named. The prefix alone is therefore NOT the candidate
	// rule — it is the shared tag — and it is what an operator greps to follow
	// one run from the line the sweep printed to the stream it wrote.
	BudgetRunPrefix = "run-"

	// BudgetRunTestedBinSuffix is the suffix of a run's built-binary directory.
	// Its value is the script's own `tested_bin_dir_suffix`, and the
	// declared-set gate reads that line out of `scripts/test-budget.sh` to hold
	// the two spellings against each other (a drift there would leave the sweep
	// reporting the tree as visited while matching nothing).
	BudgetRunTestedBinSuffix = "-tested-bin"

	// BudgetStartRecordSuffix is the suffix of a run's START RECORD — the flat
	// file a run writes before `go test` starts, naming the run's stream so a
	// KILLED run is queryable instead of orphaned. Its value is the script's own
	// `start_record_suffix`, and it is declared here for the same reason the
	// tested-binary suffix is: this tree's namespace has ONE definition per
	// shape, and the sweep's "flat children are evidence" rule is what keeps the
	// record out of the candidate set by construction rather than by spelling.
	BudgetStartRecordSuffix = "-start.txt"

	// BudgetRunsStaleAge bounds this tree's entries. The value is the sweep's
	// OWN default window (DefaultStaleAge), and that is the decision, not a
	// coincidence: a run's built-binary directory is COMPLETE the moment the
	// script's build finishes — the directory is written once and never
	// revisited — so it ages exactly like every other scratch entry, and the
	// window that protects a live run elsewhere protects it here.
	//
	// IT IS A NAMED CONSTANT WITH ITS OWN NAME even though another window holds
	// the same duration, because the two carry different reasons (a run
	// directory vs a suite root) and a reader follows the NAME to the reason.
	// The measured set this window selects on this host (2026-10-12): 120 of the
	// 320 directories, 4.3 GB of the tree's 10.27 GB — the artifacts of runs
	// that finished hours ago, while a run that started minutes ago is inside
	// the window and untouched.
	BudgetRunsStaleAge = DefaultStaleAge
)

// BudgetRunsRoot resolves the tree's ROOT — the ONE spelling of this tree's path
// (Rule 2). The join goes through the existing leaf constants rather than
// re-spelling any of them.
func BudgetRunsRoot() (string, error) {
	return CanonicalPath(filepath.Join(HomeDir(), QueueDirName, BudgetRunsDirName)), nil
}

// budgetRunContainerChild is the row's namespace predicate: a name that is a run
// TAG closed by the tested-binary suffix (`run-<tag>-tested-bin`).
//
// WHY THE TAG IS PART OF IT AND NOT ONLY THE SUFFIX: a directory that merely
// ends in `-tested-bin` could be another writer's — the suffix is the entry
// point's, but the namespace has to be the entry point's AND this tree's name
// shape (`run-…`), so a future neighbour's `something-tested-bin/` is left
// alone rather than guessed at (Rule 15: what cannot be attributed is kept).
//
// The KIND half is enforced by the walk, not here (EntryKindDir), which is what
// keeps a FLAT `run-<tag>.json` — the tree's own evidence, with the same tag and
// no suffix — out of the candidate set by construction rather than by this
// predicate's spelling.
func budgetRunContainerChild(name string) bool {
	if !strings.HasSuffix(name, BudgetRunTestedBinSuffix) {
		return false
	}
	return strings.HasPrefix(strings.TrimSuffix(name, BudgetRunTestedBinSuffix), BudgetRunPrefix)
}

// budgetRunsTree is this row as a DeclaredTree — the designed extension point:
// the `trees` face and the resident tick reach it through DeclaredTrees(), and
// the declared-set gate names it.
func budgetRunsTree() DeclaredTree {
	return DeclaredTree{
		Name:       BudgetRunsTreeName,
		Class:      TreeClassBudgetRun,
		Kind:       EntryKindDir,
		EntryMatch: budgetRunContainerChild,
		StaleAge:   BudgetRunsStaleAge,
		Resolve:    BudgetRunsRoot,
	}
}
