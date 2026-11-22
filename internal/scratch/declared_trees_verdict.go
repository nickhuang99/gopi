// declared_trees_verdict.go — the TREES arm's REPORTING TWIN: one keep verdict
// per entry the sweep JUDGED, carrying the reason the decision point produced.
//
// WHY THIS FILE EXISTS (the defect it closes, measured 2026-10-14). `gopi tool
// scratch-gc trees` reported only the entries a sweep WOULD REMOVE. The keep
// half was not merely unprinted — it was never COMPUTED: declaredTreeEntries
// returns the candidates and drops every refusal at its `continue` sites, and
// declaredEntryCollectable answered with a bare bool, so no reason string
// existed at the decision point. The consequence is the one question `--explain`
// exists to answer, answered with SILENCE: "this entry is protected, and here is
// why" and "this entry was never a candidate in the first place" both rendered
// as empty output, and a reader could not tell a protected tree from a tree the
// arm never looked at.
//
// THE RULE THIS FILE OBEYS, and the reason the walk below is a twin rather than
// a second implementation (the package's own standing rule — ONE PREDICATE, TWO
// FACES): the DELETE decision and the REPORT decision come from the same code.
// declaredEntryKeepReason is the ONE reason-bearing predicate;
// declaredEntryCollectable is its bool face (delegation, not a copy). A report
// that re-derived "why" would drift from the sweep the first time either half
// changed, and a report that drifts is worse than no report — it is a report
// that can be WRONG about a delete.
//
// WHAT IS NOT REPORTED, and why that is not the defect again: an entry that is
// not this tree's shape at all (a foreign name in a shared tree, a flat file
// where the tree expects a directory) is never a candidate and is NOT given a
// verdict line — reporting the neighbours would bury the answer, which is the
// base arm's own measured decision (see the base's explain face). The
// difference between that and the defect above is that this arm STATES THE
// COUNT it examined, so a reader sees the walk covered the entry either way.
package scratch

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// The trees arm's keep vocabulary (Rule 1b/13: the CLI line, the tests and this
// package's report all quote these, and a sentence typed twice drifts).
const (
	// EntryKeepScanIncomplete is the /proc pass's own veto, and it is a
	// DIFFERENT fact from EntryKeepLiveHolder — an unfinished scan observed
	// nothing, it merely did not finish looking. Rendering the two the same
	// would fabricate an observation (Rule 15).
	EntryKeepScanIncomplete = "the holder scan did not finish — nothing was proven unheld"
	// EntryKeepContainerHeld is a container arm's veto: a live process inside
	// the container, so nothing below it is judged.
	EntryKeepContainerHeld = "a live process holds the container — nothing below it is judged"
	// EntryKeepContainerNotDrained is the container's own keep once its children
	// were judged and at least one of them stays. It names the count, so a
	// reader can tell "one session record pins this" from "the whole container
	// is young".
	EntryKeepContainerNotDrained = "the container still holds entries that are not collectable"
	// EntryKeepLegacyKind is the keep of a tree whose entries are decided by
	// their own KIND rather than by the tree's naming rule — a tree that owns
	// its collect face owns its report face too, and this arm does not invent a
	// verdict the predicate never made.
	EntryKeepLegacyKind = "this tree's decision is per KIND, not per age — see the tree's own predicate"
)

// EntryKeepSummaryFmt is the per-tree SUMMARY sentence's format — the line that
// makes an empty plan impossible to misread as proof of protection.
//
// WHY A SUMMARY LINE AND NOT ONLY THE VERDICT LINES (the epic's own clause): a
// report that prints nothing when everything is kept leaves the reader unable
// to distinguish FOUR states that all look like empty output — the arm never
// ran, the tree does not exist on this host, the tree was walked and every
// entry was protected, and the tree was walked and had no entry at all. The
// summary carries the counts that separate them (examined / not-ours /
// collectable / kept), and it is printed UNCONDITIONALLY on the arm, dry-run or
// live, `--explain` or not, because "how much did this arm actually look at" is
// asked by a reader who did not pass the flag.
const EntryKeepSummaryFmt = "tree %s: examined=%d not-ours=%d collectable=%d kept=%d"

// TreeKeepSummary is one declared tree's REPORTED census: what the tree's walk
// judged, and how it came out. It is produced by the SAME walk the plan and the
// sweep use, so the counts cannot disagree with the verdict list beside them.
type TreeKeepSummary struct {
	// Name is the tree's identity on the report line.
	Name string
	// Examined is how many of the tree's direct entries the arm JUDGED — the
	// namespace-matched population, plus the outside-namespace entries the
	// reports lane counts separately.
	Examined int
	// NotOurs is how many direct entries the tree's own walk REFUSED ON SHAPE —
	// entryShapeExpected, i.e. the walk's own first clause — asked once so this
	// count and the walk cannot answer differently.
	//
	// IT IS A SHAPE COUNT, NOT AN OWNERSHIP COUNT, and the name is the older of
	// the two facts (measured 2026-10-27, tree `budget-runs` on the host). The
	// two populations it mixes are DIFFERENT and a reader who assumes the
	// wrong one mis-diagnoses the tree:
	//
	//  1. a genuinely FOREIGN name — another writer's entry in a shared tree
	//     (`~/.gopi/sessions` holds neighbours' directories), and
	//  2. this tree's OWN EVIDENCE — a FLAT child where the tree's entries are
	//     DIRECTORIES. `budget-runs` is the measured case and it is almost
	//     entirely this second class: 19482 of 19881 direct entries were the
	//     run STREAMS (`run-<tag>.json`), the package lists, the start records,
	//     `stamps.log` and the `last.json` symlink. Those are the tree's own
	//     artifacts — never candidates BY CONSTRUCTION (declared_budget_runs.go:
	//     "a flat child is EVIDENCE, never a candidate") — and counting them
	//     here is the mechanism working, not a defect.
	//
	// So a large not_ours is NOT a signal that a tree is misconfigured; it says
	// the tree keeps a lot of material beside its collectable entries. The
	// population that answers "is the tree bounded?" is Collectable+Kept, and
	// the per-entry keep REASONS (KeptReasons) say which rule protects each.
	// They are examined-and-refused facts; they are not verdict lines (see this
	// file's header).
	NotOurs int
	// Collectable is how many entries the plan would take.
	Collectable int
	// Kept is how many judged entries the predicate refused.
	Kept int
	// KeptReasons maps one keep REASON to how many entries carry it — the
	// distribution that says WHICH rule is doing the protecting, which a single
	// total cannot.
	KeptReasons map[string]int
	// Truncated reports that the tree held more entries than TreeEntryScanLimit,
	// so the walk stopped. It is a fact the summary must carry: `examined` is a
	// floor, never a total, and a reader comparing it to the tree's real size
	// needs to know which one they hold (Rule 15).
	Truncated bool
}

// KeptTotal is the summary's kept count as the report line renders it.
func (s TreeKeepSummary) KeptTotal() int { return s.Kept }

// declaredEntryKeepReason is the hard rule applied to a declared tree's entry,
// answering with the REASON it is kept — the empty string when it is
// collectable.
//
// It is the ONE decision point: declaredEntryCollectable delegates to it, so the
// predicate the sweep obeys and the sentence the report prints are the same
// code. Each clause is a distinct, named fact (Rule 15 — every keep names
// itself; no silent skip, no unmeasured fact rendered as a measured negative).
func declaredEntryKeepReason(tree DeclaredTree, path string, now time.Time, age time.Duration, held heldTreeScan) string {
	if IsDeclaredTreeRoot(path) {
		// A declared ROOT is structure, never a candidate, at any age — the
		// invariant this package's declared-tree machinery exists to make
		// unrepresentable.
		return EntryKeepDeclaredRoot
	}
	stale, err := tree.entryStale(path, now, age)
	if err != nil {
		// Unmeasurable: keep, and SAY SO — the error is the reason and it names
		// the tree whose rule failed (Rules 15).
		return fmt.Sprintf("%s: %v", EntryKeepUnmeasurable, err)
	}
	if !stale {
		return fmt.Sprintf("%s (%s)", EntryKeepFresh, age)
	}
	if !treeScanComplete(held) {
		// The pass did not finish: nothing is proven unheld. It is NOT the same
		// fact as a sighting, and only one of the two sentences is true.
		return EntryKeepScanIncomplete
	}
	if heldIn(path, held.index) {
		return fmt.Sprintf("%s (the %s scan saw a process inside it)", EntryKeepLiveHolder, HolderFileName)
	}
	if !unheldRegistered(path) {
		// The .holder half: a registration INSIDE the candidate, read without
		// the process table. The sentence is the owner's OWN reason wherever it
		// has one — "the recorded owner is live" — and the generic holder
		// sentence only when the record exists but says nothing usable.
		v := OwnerVerdictFor(path)
		if v.Registered && v.Reason != "" && !v.Collectable {
			return fmt.Sprintf("%s (%s)", EntryKeepLiveHolder, v.Reason)
		}
		return EntryKeepLiveHolder
	}
	return "" // collectable: the ONE form the bool face reads
}

// collectableEntryReason is the ONE sentence a collected entry carries, built on
// the shared EntryCollectablePrefix (which already ends in a space — a second
// space here is the kind of drift a second spelling produces).
func collectableEntryReason(tree DeclaredTree) string {
	return EntryCollectablePrefix + fmt.Sprintf("aged, unheld entry of declared tree %s", tree.Name)
}

// collectableContainerReason is the container arm's collectable sentence: the
// container was emptied of its candidates AND passed its own longer window, and
// the sentence names the window so a reader can see which clock released it.
func collectableContainerReason(rootAge time.Duration) string {
	return EntryCollectablePrefix + fmt.Sprintf("emptied of its candidates and past the %s container window", rootAge)
}

// declaredEntryCollectable is the bool FACE of the predicate above — the same
// decision, one implementation. It is deliberately not a second predicate: the
// sweep's delete decision is `reason == ""`, so a reason added above reaches the
// sweep and the report together, and neither can drift from the other.
func declaredEntryCollectable(tree DeclaredTree, path string, now time.Time, age time.Duration, held heldTreeScan) bool {
	return declaredEntryKeepReason(tree, path, now, age, held) == ""
}

// DeclaredTreeVerdicts is the REPORTING TWIN of declaredTreeEntries: the whole
// content of one tree that the sweep examined, one EntryVerdict per judged entry,
// in the SAME order and by the SAME predicate the plan and the collect face use.
//
// WHAT IT REPORTS, and the four states it separates:
//
//   - the tree itself — its ROOT survives by design, and that is a LINE, not a
//     silence (a reader who knows the container exists must be able to find it
//     and read why it is still there);
//   - every direct entry the tree's naming rule MATCHED: collectable, or kept
//     with the reason the decision point produced;
//   - for a container-kind tree, the container's OWN keep once its children were
//     judged — held / unreadable / still holding non-collectable entries;
//   - and, alongside the verdicts, a TreeKeepSummary carrying the counts —
//     including how many entries the rule REFUSED as not this tree's shape, so
//     "nothing was reported" is always accompanied by "and here is what was
//     looked at".
//
// A tree that does not exist on this host, or whose root cannot be read, is
// REPORTED (Rule 15: an unmeasured fact is never a measured negative) and its
// summary is returned with zeroed counts so a caller can print the line either
// way.
//
// The window is the tree's own (window()) — the reporting twin reads the SAME
// clock as the plan and the sweep, or an `--explain` line would be an opinion
// about a judgement nobody made.
func DeclaredTreeVerdicts(tree DeclaredTree) ([]EntryVerdict, TreeKeepSummary) {
	summary := TreeKeepSummary{Name: tree.Name, KeptReasons: map[string]int{}}
	root, err := tree.Resolve()
	if err != nil || root == "" {
		// Declared, but not configured on this host: a verdict that NAMES the
		// state, so an absent tree and a clean tree never print the same.
		reason := EntryKeepDeclaredRoot
		if err != nil {
			reason = fmt.Sprintf("%s: %v", EntryKeepUnmeasurable, err)
		}
		return []EntryVerdict{{Path: root, Reason: reason}}, summary
	}
	out := []EntryVerdict{{
		Path:   root,
		Reason: fmt.Sprintf("%s (the ROOT of declared tree %s survives every sweep by design)", EntryKeepDeclaredRoot, tree.Name),
	}}
	age := tree.window()
	entries, rerr := readTreeRoot(tree, root)
	if rerr != nil {
		// The walk could not look: REPORTED, with the state the read produced
		// (unconfigured vs unreadable), never as an empty tree.
		reason := fmt.Sprintf("%s: %v", EntryKeepUnmeasurable, rerr)
		if errors.Is(rerr, ErrDeclaredTreeUnconfigured) {
			reason = fmt.Sprintf("%s (%v)", EntryKeepDeclaredRoot, rerr)
		}
		out = append(out, EntryVerdict{Path: root, Reason: reason})
		return out, summary
	}
	if tree.List != nil {
		// A tree that owns its collect face owns its candidate listing (the
		// plan reads List), so the report reads the PLAN rather than inventing a
		// second walk: the entries it names are reported collectable, and the
		// tree's own decision about everything else is not this arm's to render
		// (the plan cannot see a keep the tree never asked it about).
		plan, perr := treeOwnedPlan(tree)
		if perr != nil && plan == nil {
			out = append(out, EntryVerdict{Path: root, Reason: fmt.Sprintf("%s: %v", EntryKeepUnmeasurable, perr)})
			return out, summary
		}
		summary.Collectable = len(plan)
		for _, e := range plan {
			out = append(out, EntryVerdict{
				Path:        e.Path,
				Age:         e.Age,
				Collectable: true,
				Reason:      collectableEntryReason(tree),
			})
		}
		return out, summary
	}
	now := time.Now()
	// ONE /proc pass for the whole walk, indexed once — the same pass, the same
	// index and the same order as the plan and the sweep (a second pass here
	// would let the report judge on evidence the sweep never saw).
	scan := ScanHolders()
	held := heldTreeScan{index: heldSet(scan, root), scan: scan}
	for i, entry := range entries {
		if i >= TreeEntryScanLimit {
			summary.Truncated = true
			break
		}
		path := filepath.Join(root, entry.Name())
		verdicts, judged := declaredEntryVerdicts(tree, path, entry, now, age, held)
		summary.Examined += judged
		out = append(out, verdicts...)
		summary.Kept += countKept(verdicts, summary.KeptReasons)
		summary.Collectable += countCollectable(verdicts)
		// NOT-OURS is the count of direct entries the tree's OWN naming rule
		// refused: for a container tree the entries are container DIRECTORIES
		// (its entryName answers false for a directory by construction), so the
		// question is the shape test, not the naming rule — applying entryName
		// here would count every container as a stranger.
		if !entryShapeExpected(tree, entry) {
			summary.NotOurs++
		}
	}
	return out, summary
}

// isContainerKind reports whether a tree's entries are CONTAINERS whose content is
// judged one level down — the arm that has its own keep verdicts to render.
func isContainerKind(tree DeclaredTree) bool { return tree.Kind == EntryKindTreeFileSuffix }

// entryShapeExpected reports whether one direct entry of a tree is the SHAPE the
// tree's own walk expects — the container kind's containers, every other kind's
// naming rule. It is the walk's own first clause, asked once so the summary's
// not-ours count and the walk cannot answer differently.
func entryShapeExpected(tree DeclaredTree, entry os.DirEntry) bool {
	if isContainerKind(tree) {
		return entry.IsDir()
	}
	return tree.entryName(entry.Name(), entry.IsDir())
}

// declaredEntryVerdicts is ONE direct entry's twin, and it is the direct mirror
// of the walk's own switch: a container-kind tree asks containerEntries, every
// other kind asks the one predicate. It answers with the verdicts to report and
// how many entries of the tree it JUDGED (container child files included, so the
// counts describe the population the walk actually looked at).
func declaredEntryVerdicts(tree DeclaredTree, path string, entry os.DirEntry, now time.Time, age time.Duration, held heldTreeScan) ([]EntryVerdict, int) {
	if tree.Kind == EntryKindTreeFileSuffix {
		if !entry.IsDir() {
			// A flat entry where a container is expected: not this tree's shape,
			// never a candidate, and containerEntries says the same by returning
			// nil. Reported as examined-and-refused.
			return nil, 0
		}
		return containerVerdicts(tree, path, entry, now, age, held)
	}
	if !tree.entryName(entry.Name(), entry.IsDir()) {
		return nil, 0 // not this tree's shape: examined, refused, not a verdict line
	}
	reason := declaredEntryKeepReason(tree, path, now, age, held)
	measured := measuredEntry(tree, path, entry.IsDir(), now)
	v := EntryVerdict{Path: path, Age: measured.Age, Reason: reason, Collectable: reason == ""}
	if v.Collectable {
		v.Reason = collectableEntryReason(tree)
	}
	return []EntryVerdict{v}, 1
}

// containerVerdicts is the container arm's twin: the container's own judgement,
// and — when the container is judged at all — the verdicts for its children, one
// level down, exactly as containerEntries walks them.
//
// THE THREE WAYS A CONTAINER IS KEPT, each named (and each one of them was,
// before this file, ZERO OUTPUT): the walk's holder pass did not finish, a live
// process holds the container, or the container could not be read. And a fourth,
// produced by judging its children: the container still holds entries that are
// not collectable — the case the briefing measured, where `--explain` printed
// nothing about a container that was pinned by one session record.
func containerVerdicts(tree DeclaredTree, path string, entry os.DirEntry, now time.Time, age time.Duration, held heldTreeScan) ([]EntryVerdict, int) {
	containerVerdict := func(reason string) []EntryVerdict {
		e := measuredEntry(tree, path, true, now)
		return []EntryVerdict{{Path: path, Age: e.Age, Reason: reason}}
	}
	// The pass did not finish: the WHOLE container is kept, read once before any
	// child is judged — the same first clause containerEntries applies.
	if !treeScanComplete(held) {
		return containerVerdict(fmt.Sprintf("%s (%s)", EntryKeepScanIncomplete, HolderFileName)), 1
	}
	if heldIn(path, held.index) {
		return containerVerdict(fmt.Sprintf("%s (the %s scan saw a process inside it)", EntryKeepContainerHeld, HolderFileName)), 1
	}
	children, err := os.ReadDir(path)
	if err != nil {
		return containerVerdict(fmt.Sprintf("%s: %v", EntryKeepUnmeasurable, err)), 1
	}
	var out []EntryVerdict
	judged := 1 // the container itself is judged
	keptFiles := 0
	for i, child := range children {
		if i >= TreeEntryScanLimit {
			break
		}
		childPath := filepath.Join(path, child.Name())
		if child.IsDir() {
			// The run's own working area: the hook's nil means KEEP, and the
			// candidate it returns is the DIRECTORY itself. The hook's own rule
			// is not re-derived here — a container directory it refuses is
			// reported as kept by that family's decision, which is the only
			// sentence this arm can honestly print about another family's gate.
			cand := containerScratchDirEntries(tree, childPath, path, now, age, held)
			if len(cand) > 0 {
				out = append(out, collectableChildVerdicts(tree, cand)...)
				continue
			}
			keptFiles++
			judged++
			out = append(out, EntryVerdict{Path: childPath, Reason: EntryKeepLegacyKind})
			continue
		}
		if !tree.entryName(child.Name(), child.IsDir()) {
			// The session record and the side files: a container holding one is
			// never emptied. This is the measured case — and the line that used
			// to be silence.
			keptFiles++
			judged++
			e := measuredEntry(tree, childPath, false, now)
			out = append(out, EntryVerdict{Path: childPath, Age: e.Age, Reason: EntryKeepNotOurs})
			continue
		}
		reason := declaredEntryKeepReason(tree, childPath, now, age, held)
		judged++
		e := measuredEntry(tree, childPath, false, now)
		v := EntryVerdict{Path: childPath, Age: e.Age, Reason: reason, Collectable: reason == ""}
		if v.Collectable {
			v.Reason = collectableEntryReason(tree)
		}
		out = append(out, v)
		if !v.Collectable {
			keptFiles++
		}
	}
	if keptFiles > 0 {
		// The container survives because its children do. The count is IN the
		// sentence, so the reader learns which half did the protecting.
		out = append(out, containerVerdict(fmt.Sprintf("%s (%d)", EntryKeepContainerNotDrained, keptFiles))...)
		return out, judged
	}
	// Every child is collectable: the container goes with them, but only once it
	// has reached the LONGER container window — the same second gate
	// containerEntries applies, reported with its age so a reader can see the
	// clock is what is holding it.
	if rootAge := tree.RootStaleAge; rootAge > 0 {
		if stale, serr := ScratchEntryStale(path, now, rootAge); serr == nil && stale {
			e := measuredEntry(tree, path, true, now)
			out = append(out, EntryVerdict{
				Path:        path,
				Age:         e.Age,
				Collectable: true,
				Reason:      collectableContainerReason(rootAge),
			})
			return out, judged
		}
		out = append(out, containerVerdict(fmt.Sprintf("%s (%s)", EntryKeepFresh, rootAge))...)
		return out, judged
	}
	return out, judged
}

// collectableChildVerdicts renders the container hook's own candidates (it
// answers with []TreeEntry, already decided) as collectable verdicts — one shape
// for both faces, so a report can never disagree with what the sweep took.
func collectableChildVerdicts(tree DeclaredTree, cand []TreeEntry) []EntryVerdict {
	out := make([]EntryVerdict, 0, len(cand))
	for _, c := range cand {
		out = append(out, EntryVerdict{
			Path:        c.Path,
			Age:         c.Age,
			Collectable: true,
			Reason:      collectableEntryReason(tree),
		})
	}
	return out
}

// countKept tallies the kept verdicts into the summary's distribution.
func countKept(verdicts []EntryVerdict, into map[string]int) int {
	kept := 0
	for _, v := range verdicts {
		if v.Collectable {
			continue
		}
		kept++
		into[v.Reason]++
	}
	return kept
}

// countCollectable tallies the collectable verdicts.
func countCollectable(verdicts []EntryVerdict) int {
	n := 0
	for _, v := range verdicts {
		if v.Collectable {
			n++
		}
	}
	return n
}
