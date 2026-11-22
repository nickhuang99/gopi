// declared_budget_evidence.go — the SECOND row over the test-budget artifact
// tree: the per-run JSON STREAMS (`run-<epoch>-<pid>.json`), bounded by their
// OWN mtime under a long retention window plus a newest-N floor.
//
// THE DEFECT THIS CLOSES (measured 2026-11-22, the host, task 01a200e6). The
// `budget-runs` row (declared_budget_runs.go) is EntryKindDir: its candidates are
// the `run-<tag>-tested-bin/` DIRECTORIES. Everything FLAT under the same root is
// kept by construction — and the flat half is where the bytes are. Measured at
// one instant on `<scratch home>/queue/test-budget`:
//
//	run-*-tested-bin/ DIRECTORIES        84      (the row's only candidates)
//	run-<tag>.json STREAMS           11,662   17.7 GiB   ← THIS ROW's candidates
//	everything else flat (records, package lists, stamps logs,
//	  the `last.json` symlink)       ~33,700    ~5 MiB   ← never collected, any age
//	                                 ───────────────────
//	                                 ~21 GiB, of which the row before this one
//	                                 could reach only the directories
//
// `gopi tool scratch-gc --explain` judged 79 lines for that directory — 78
// directories plus the container itself, which was kept as "the root of a
// declared tree is never a candidate". Not ONE line about the 45,283 flat files:
// a flat child is not a verdict line in the directory row, and the container is
// the row's ROOT, so the middle layer was invisible to both faces and 18 GB grew
// with nothing planning it.
//
// WHY A SEPARATE ROW AND NOT A WIDENING OF `budget-runs` — the same reasoning the
// build cache's stray-shard row records (declared_build_cache_stray.go), and here
// it is the difference between two RULES, not two name shapes:
//
//   - the directory row's age is BudgetRunsStaleAge (6h): a run's built binary is
//     complete the moment the build finishes and is never revisited, so it ages
//     like every other scratch entry;
//   - a STREAM is EVIDENCE a reader comes back to when a run is missing its
//     completion line (a killed run's only diagnostic). Six hours would delete
//     exactly that, so this row's window is a DIFFERENT number with a DIFFERENT
//     reason — and a window that guards evidence must be attributable to the
//     shape it protects, not shared with the binaries beside it.
//
// Widening the directory row would have to give one number to both shapes, i.e.
// either 18 GB of evidence held by a 6h rule (deleted far too early) or a
// 63 MB-per-run binary held for two weeks (the tree's original 10 GB incident).
// Two rows, two windows, one root — the arrangement the sessions tree already
// uses for its disjoint families.
//
// WHY THE BOUND IS HERE AND NOT (ONLY) IN THE WRITER. declared_budget_runs.go
// argued the bound belongs to `scripts/test-budget.sh` (keep the newest N
// streams). That is a correct description of WHERE the bytes are produced, and it
// is not a sufficient answer to "why is the sweep not allowed to plan it": a
// writer-side prune is a silent delete at write time, while this row is a
// DECLARED bound an operator can read BEFORE it happens —
// `gopi tool scratch-gc trees --dry-run --tree budget-evidence` prints every
// stream it would take, with its age and its bytes, and `--explain` says why each
// survivor survived. The safety argument is then the same one every other row
// carries (namespace + own mtime + no live holder + never a declared root), and
// it is reviewable. The two halves do not conflict: a writer that later keeps the
// newest N streams simply produces fewer candidates for this row.
//
// WHAT IS DELIBERATELY NOT A CANDIDATE, and why each exclusion is the rule, not a
// convenience:
//
//   - the START RECORD (`run-<tag>-start.txt`), the package lists
//     (`-scope-/‑stream-/‑out-of-scope-packages.txt`), the `-e2e-deferred.txt`
//     notes, the zero-byte `-disk-exhaustion` markers: ~33,700 files totalling
//     ~5 MiB. They are the INDEX that makes a stream findable ("a reader greps the
//     directory for the run's tag and finds the record, the stream and the built
//     binary together" — declared_budget_runs.go). Collecting a stream while
//     keeping its record is a coherent trade: the record names what existed and
//     says when; the megabytes of `go test -json` body are the part that grows
//     without bound. Deleting the record too would delete the only remaining trace
//     that the run ever happened, for 5 MiB.
//   - `stamps.log` and its `stamps-*.log` siblings: the appended per-run verdict
//     log a human reads. One line per run; bounded by construction.
//   - the `last.json` SYMLINK: the canonical pointer to the newest completed run.
//     It is not a stream (its name does not carry the run tag), and the sweep never
//     follows a symlink out of the tree in any case.
//   - every DIRECTORY: the binary directories are the `budget-runs` row's
//     candidates, judged by that row's window. Two rows over one root are correct
//     only while their predicates are DISJOINT, and the kind split (dirs vs the
//     one flat suffix) is what makes them so — mirroring the sessions tree's
//     legacy rows, which share a root under exactly this rule.
//
// THE ROOT SURVIVES, unconditionally, exactly as every declared tree's root does:
// the listing starts one level down, `IsDeclaredTreeRoot` is asked again per path
// at the moment of every delete (removeCollectableOfTree), and a root can never be
// produced by a listing that only reads direct children.
package scratch

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

const (
	// BudgetEvidenceTreeName is this row's identity on the watchdog line, in the
	// `--tree` selector and in a failure report. One definition (Rules 13/14).
	BudgetEvidenceTreeName = "budget-evidence"

	// BudgetEvidenceStreamSuffix is the suffix of a run's JSON stream — the
	// `go test -json` redirect target `scripts/test-budget.sh` writes per run.
	// The value is the script's own `raw_json_suffix`; it is declared here for
	// the same reason the tested-binary suffix is (the two spellings are held
	// against each other by the declared-set gate rather than by memory).
	BudgetEvidenceStreamSuffix = ".json"

	// BudgetEvidenceStaleAge is the age past which an unheld STREAM is
	// collectable. It is the LONGEST window this package applies, and the value
	// is a decision with a measurement behind it, not a preference:
	//
	//   - a stream is the only record of WHAT a run's tests did (which packages
	//     ran, which failed, which were skipped) and it is the one artifact a
	//     reader reaches for when a run is missing its completion line. So the
	//     window has to be measured in WEEKS, like the session raw logs
	//     (SessionRawLogStaleAge, 14 days, whose cost paragraph is the same
	//     argument: what is lost is re-reading an old run verbatim).
	//   - 14 days is also what makes the bound MEAN something: measured on this
	//     host on 2026-11-22, `run-*.json` older than 14 days was 15.3 GiB of the
	//     tree's 17.7 GiB of streams. A 30-day window left 3.4 GiB reclaimable
	//     while the tree keeps taking 0.25–1.1 GiB/day.
	//   - two full weeks is longer than any diagnosis this repository has actually
	//     performed: the incidents cited in this package's own comments were
	//     diagnosed from runs of the SAME DAY (2026-10-12, 2026-10-27), and a
	//     stream is not what a merge gate, a reviewer or a rework decision reads —
	//     the START RECORD and stamps.log are, and both are kept forever.
	//
	// It is deliberately NOT DefaultStaleAge: a 6h window here would delete the
	// evidence a killed run exists to leave, which is the trade
	// declared_budget_runs.go describes as "the bound belongs to the writer" —
	// this row's answer to the same trade is a window long enough to be evidence
	// and short enough to be a bound.
	BudgetEvidenceStaleAge = 14 * 24 * time.Hour

	// BudgetEvidenceRetainRuns is the FLOOR under the age rule: the newest N
	// streams are kept however old they are (by the stream's own mtime, newest
	// first). It exists because an age window alone has one failure mode a
	// repository can actually hit — a host that stops running the suite for a
	// month would, on its first sweep after the pause, delete EVERY stream,
	// including the ones from the last runs it ever did. The floor makes "the runs
	// I just ran" unrepresentable as garbage, and it is the same shape the writer
	// side was asked for ("keep the newest N streams"), applied where an operator
	// can see it in a dry-run.
	//
	// 200 is a bound, not a target: streams average ~1.5 MiB on this host
	// (measured 2026-11-22: 11,662 streams / 17.7 GiB), so the floor costs ~300 MB
	// resident and covers the last several hundred suite runs — far more than a
	// diagnosis needs, and one order of magnitude smaller than the daily intake.
	BudgetEvidenceRetainRuns = 200
)

// BudgetEvidenceStreamName is this row's NAMESPACE PREDICATE: a flat child of the
// tree is a run STREAM only when it carries the run tag (`run-`) and closes with
// the stream suffix. Everything else flat — the start record, the package lists,
// the stamps logs, the `last.json` symlink, another writer's file — is refused
// here, and a DIRECTORY is refused by the listing (a binary directory belongs to
// the `budget-runs` row, not to this one).
//
// It is a whole-name rule rather than a suffix alone for the reason the
// tested-binary predicate gives: a name that merely ends in `.json` could be
// another writer's, and Rule 15 says what cannot be attributed is kept.
func BudgetEvidenceStreamName(name string) bool {
	if !strings.HasSuffix(name, BudgetEvidenceStreamSuffix) {
		return false
	}
	return strings.HasPrefix(strings.TrimSuffix(name, BudgetEvidenceStreamSuffix), BudgetRunPrefix)
}

// budgetEvidenceCandidate is ONE measured stream: what the listing decided about
// it without deleting anything.
type budgetEvidenceCandidate struct {
	path string
	// age is measured from the FILE's own mtime — never the container's. The
	// container's mtime is refreshed by every `go test` run (measured: age=0s),
	// so an age read at the directory would keep the whole tree forever.
	age   time.Duration
	size  int64
	mtime time.Time
}

// budgetEvidenceListing is the ONE listing behind this row's BOTH faces (the plan
// reads it, the sweep removes exactly it) — two walks here would be two answers,
// which is the drift this package refuses everywhere.
//
// The three clauses, in order, each of them the row's own rule:
//
//  1. SHAPE — a DIRECT child that is not a directory and whose name the
//     namespace predicate claims. This is the clause that keeps the binary
//     directories out (the other row's candidates) and the 33,700 evidence/index
//     files out (never candidates at any age).
//  2. THE FLOOR — the newest BudgetEvidenceRetainRuns streams by their own
//     mtime are kept, whatever their age (see the constant's comment).
//  3. THE WINDOW — of what is left, a stream is a candidate once its own mtime
//     is older than olderThan (the row's BudgetEvidenceStaleAge).
//
// A root that does not exist yields an EMPTY listing and no error (a host that
// never ran the entry point has no such tree; the directory row already reports
// it as unconfigured once). A root that exists and cannot be READ is an error —
// never an empty listing, because "could not look" is not "nothing there"
// (Rule 15b).
func budgetEvidenceListing(root string, now time.Time, olderThan time.Duration) ([]budgetEvidenceCandidate, error) {
	entries, err := os.ReadDir(root)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("read budget evidence tree %s: %w", root, err)
	}
	all := make([]budgetEvidenceCandidate, 0, len(entries))
	for i, entry := range entries {
		if i >= TreeEntryScanLimit {
			return nil, errTreeScanLimit(DeclaredTree{Name: BudgetEvidenceTreeName})
		}
		if entry.IsDir() {
			continue // the binary directories: the budget-runs row's candidates
		}
		if !BudgetEvidenceStreamName(entry.Name()) {
			continue // the record, the package lists, stamps.log, last.json
		}
		info, ierr := entry.Info()
		if ierr != nil {
			// A file whose own mtime cannot be read is never a candidate: the age
			// rule has no measurement to apply. It is REPORTED rather than
			// silently dropped, so an unreadable stream cannot look like a
			// collected one.
			fmt.Fprintf(os.Stderr, "scratch: GC kept %s (%s) — the stream's own mtime could not be read: %v\n",
				filepath.Join(root, entry.Name()), BudgetEvidenceTreeName, ierr)
			continue
		}
		all = append(all, budgetEvidenceCandidate{
			path:  filepath.Join(root, entry.Name()),
			age:   now.Sub(info.ModTime()),
			size:  info.Size(),
			mtime: info.ModTime(),
		})
	}
	// NEWEST FIRST, and the tie-break is the NAME so the order is total: a report
	// whose lines reorder between two runs of the same tree cannot be compared, and
	// the floor's membership must not depend on ReadDir's order.
	sort.Slice(all, func(i, j int) bool {
		if !all[i].mtime.Equal(all[j].mtime) {
			return all[i].mtime.After(all[j].mtime)
		}
		return all[i].path > all[j].path
	})
	out := make([]budgetEvidenceCandidate, 0, len(all))
	for i, c := range all {
		if i < BudgetEvidenceRetainRuns {
			continue // the floor: the newest N runs are never garbage
		}
		if c.age <= olderThan {
			continue // inside the window
		}
		out = append(out, c)
	}
	return out, nil
}

// budgetEvidencePlan is the PLAN face: the same listing, as TreeEntries, with the
// tree's own window applied by the caller (entryStale is not consulted — the
// listing measures each stream's own mtime, which IS the rule this row needs; see
// budgetEvidenceListing's clause 3).
func budgetEvidencePlan(root string, olderThan time.Duration) ([]TreeEntry, error) {
	cands, err := budgetEvidenceListing(root, time.Now(), olderThan)
	if err != nil {
		return nil, err
	}
	out := make([]TreeEntry, 0, len(cands))
	for _, c := range cands {
		out = append(out, TreeEntry{Path: c.path, Age: c.age, Size: c.size})
	}
	return out, nil
}

// budgetEvidenceHolderScan is the ONE /proc pass this row takes per sweep. It is
// a seam, not a second policy: the production value IS scratch.ScanHolders, and
// the only caller that replaces it is the test that pins the COST (one pass for
// the whole row, never one per candidate — see sweepBudgetEvidence).
var budgetEvidenceHolderScan = ScanHolders

// sweepBudgetEvidence is the COLLECT face: it removes exactly the plan's entries,
// re-checking every last-moment guard the other declared trees get — the
// declared-root refusal (re-asked per path), the /proc holder verdict and the
// `.holder` registration. Nothing is removed here that the plan would not have
// named.
//
// THE LIVENESS PASS IS TAKEN ONCE FOR THE WHOLE ROW, and that is a correctness
// requirement of this row's SHAPE, not an optimisation (measured 2026-11-22, task
// 01a200e6 — the delivery attempt this comment exists because of). The row's
// candidate population is 9,892 FLAT files, and `removeCollectableOfTree` asks
// the holder question with `DirHolder`, which walks the WHOLE process table
// (cwd + exe + every fd + maps) for that one path. Per candidate that is
// O(candidates × processes): the sweep took longer than the clone channel's
// 300 s test-compile budget, the leg was SIGKILLed, and the gate — which reads a
// kill as `*exec.ExitError` — refused the delivery as "does not compile".
//
//	$ kill -QUIT <devworkspace.test>   # captured with -timeout 120s
//	scratch.readProcFile  ← holder.go:537
//	scratch.dirHolder     ← holder.go:139
//	scratch.removeCollectableOfTree ← declared_trees.go:1459
//	scratch.sweepBudgetEvidence     ← declared_budget_evidence.go:290
//	scratch.SweepDeclaredTree → sweepDeclaredTreeFromQueue → SweepHome
//	testutil.GCStaleScratchRoot → PinSuiteTempRoot → RunSuiteMain → TestMain
//
// The shape is the one the sessions tree and the declared-tree walk already use:
// ONE `ScanHolders()` per batch, indexed ONCE with `heldSet` (so a candidate costs
// one map lookup), plus the cheap per-candidate re-reads — Lstat, the root
// identity, the `.holder` record. It is the same rule, one implementation
// (`budgetEvidenceDeleteRefusal` reads `heldIn`/`unheldRegistered`, which are the
// declared-tree walk's own predicates), so a stream this row keeps and a
// directory the other row keeps are kept for the same reasons.
//
// A TRUNCATED PASS DELETES NOTHING and says so (heldTreeScan's rule): "no holder
// inside it" is evidence only if every live process was examined.
func sweepBudgetEvidence(rowName, root string, olderThan time.Duration) (int, int64, error) {
	cands, err := budgetEvidenceListing(root, time.Now(), olderThan)
	if err != nil {
		return 0, 0, err
	}
	if len(cands) == 0 {
		// Nothing to judge: a /proc pass here would cost the whole process table to
		// answer a question nobody asked (the row sweeps on every tick).
		return 0, 0, nil
	}
	scan := budgetEvidenceHolderScan()
	held := heldTreeScan{index: heldSet(scan, root), scan: scan}
	tree := DeclaredTree{Name: rowName, Class: TreeClassBudgetRun}
	removed := 0
	var freed int64
	for _, c := range cands {
		if reason := budgetEvidenceDeleteRefusal(tree, c.path, held); reason != "" {
			fmt.Fprintf(os.Stderr, "scratch: GC kept %s (%s) — %s\n", c.path, rowName, reason)
			continue
		}
		if rerr := removeCollectable(c.path); rerr != nil {
			fmt.Fprintf(os.Stderr, "scratch: GC could not remove %s (%s): %v\n", c.path, rowName, rerr)
			continue
		}
		removed++
		freed += c.size
	}
	return removed, freed, nil
}

// budgetEvidenceDeleteRefusal is the LAST question asked before one delete: ""
// means the stream may go. Every clause is the one `declaredEntryKeepReason`
// applies to a directory (the root invariant, the truncation veto, the live
// holder, the `.holder` record) and each names itself (Rule 15 — a keep that says
// nothing is a silent skip).
//
// The AGE is deliberately NOT re-read here: this row's window is applied by
// `budgetEvidenceListing` against each stream's own mtime, and re-stating it from
// a second clock is the drift `window()` exists to prevent (Rule 2 — one number,
// one reader).
func budgetEvidenceDeleteRefusal(tree DeclaredTree, path string, held heldTreeScan) string {
	if IsDeclaredTreeRoot(path) {
		return EntryKeepDeclaredRoot
	}
	if !treeScanComplete(held) {
		return HolderScanWithheldReason()
	}
	if heldIn(path, held.index) {
		return EntryKeepLiveHolder
	}
	if !unheldRegistered(path) {
		v := OwnerVerdictFor(path)
		if v.Registered && v.Reason != "" && !v.Collectable {
			return fmt.Sprintf("%s (%s)", EntryKeepLiveHolder, v.Reason)
		}
		return EntryKeepLiveHolder
	}
	return ""
}

// budgetEvidenceTree is this row as a DeclaredTree — the designed extension point
// (declared_build_cache_stray.go's stray-shard row is the precedent for a second
// row over another row's root).
//
// The Kind is EntryKindFileMatch with the namespace predicate set, so the shape
// half of the rule is declared in the table and READ by the listing above; the two
// closures exist because the row's rule is not expressible as "aged by mtime"
// alone — the newest-N floor needs the whole population, which only a listing can
// see. List and Sweep are built from the ONE listing, so the dry-run cannot promise
// a stream the sweep refuses (Rule 15).
func budgetEvidenceTree() DeclaredTree {
	name := BudgetEvidenceTreeName
	return DeclaredTree{
		Name:       name,
		Class:      TreeClassBudgetRun,
		Kind:       EntryKindFileMatch,
		StaleAge:   BudgetEvidenceStaleAge,
		EntryMatch: BudgetEvidenceStreamName,
		List: func(age time.Duration) ([]TreeEntry, error) {
			root, err := BudgetRunsRoot()
			if err != nil {
				return nil, err
			}
			return budgetEvidencePlan(root, age)
		},
		Sweep: func(age time.Duration) (int, int64, error) {
			root, err := BudgetRunsRoot()
			if err != nil {
				return 0, 0, err
			}
			return sweepBudgetEvidence(name, root, age)
		},
		Resolve: BudgetRunsRoot,
	}
}
