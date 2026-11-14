// declared_session_legacy.go — the sessions tree's LEGACY-RESIDUE rows: the
// families internal/sessionlayout registers with a `bound:` decision that the
// `session-raw` tree does not cover.
//
// WHY A SECOND SET OF ROWS ON THE SAME ROOT. The declared-tree registry
// (declared_trees.go) bounds the sessions tree by ONE namespace rule —
// `*.raw.log` one level down — so every other family in that tree was named by
// nothing and aged by nothing. internal/sessionlayout's family registry
// (families.go) is the RANGE half: it classifies EVERY entry a real sessions
// tree yields and records an explicit decision per family. A family whose
// decision is `bound:` needs a declared tree that really EXISTS, and this file
// is that tree: three rows over the SAME root (the sessions tree itself), each
// carrying ONE of the registry's exported predicates as its namespace rule.
//
//	row                          candidates                    predicate
//	session-legacy               root-level DIRECTORIES        IsRootTaskContainerName
//	                                                           or IsStrayRootDirName
//	session-legacy/files         root-level FILES              IsRootResidueFileName
//	session-legacy/containers    one level down inside an      GitMarkerName or
//	                             aged, unheld container         SandboxProfileMarkerName
//
// ONE ROW PER SHAPE, NEVER ONE WIDE ROW. A declared tree's entry shape is
// decided by its Kind (a directory removed whole, a file, a file one level
// down) and its namespace predicate is asked per NAME — so a row that took both
// directories and files could not be spelled at all, and a row whose predicate
// were broader than a family's membership test would collect what the registry
// calls somebody else's. Splitting is also what keeps the predicates
// attributable in a log line: a reader sees which SHAPE went.
//
// THE ROOT IS NEVER A CANDIDATE, and here that IS the safety story rather than
// a detail: all three rows root at the sessions tree ITSELF (the same root the
// `session-raw` row uses) and removeCollectableOfTree refuses a declared root
// unconditionally (IsDeclaredTreeRoot). A row therefore can never remove the
// sessions tree — only the aged, unheld CHILDREN its own predicate claims.
//
// WHY THE AGE IS MUCH LONGER THAN EVERY OTHER DECLARED TREE'S
// (SessionLegacyDirStaleAge, 180 days), and why that is the honest bound rather
// than a timid one. Three measurements, taken on this host 2026-10-01, decided
// it:
//
//   - THE CANDIDATE IS A DIRECTORY REMOVED WHOLE. `evidence/` (113 of them),
//     `t-<epoch>/` (296 containers), a probe's clone: nothing re-derives any of
//     them, and no reader can inspect a directory the sweep has taken. Every
//     other declared tree's candidate is a FILE whose loss costs one artifact;
//     here it costs a subtree.
//   - THE WRITER IS GONE, BUT SOP IS NOT. These are the pre-session layout's
//     containers and the hand-run probes' directories. That they are residue is
//     a statement about the CODE, not about how recently a human read one — and
//     a human's own probe directory is exactly the file they come back for.
//   - 30 DAYS WOULD TAKE ALMOST ALL OF IT ON THE FIRST TICK. The measured set's
//     newest container is 2026-09-20, eleven days old at the time of writing; a
//     30-day window means the first sweep after this lands removes 296
//     containers, 113 `evidence/` trees and 391 orphaned dumps — directories
//     nobody has reviewed. A bound whose first tick is a mass deletion of
//     directories is not a bound an operator can consent to.
//
// 180 days keeps the property the epic is after — the tree stops growing
// without limit: nothing can outlive the window — while making the FIRST sweep
// collect nothing on this host (measured: not one member is older than the
// window), so the operator sees the plan (`gopi tool scratch-gc trees --dry-run`)
// before anything goes. (No window parameter reaches this row on ANY arm — the
// plan, the sweep and the verdict faces take none — so seeing a shorter plan
// today is a test or a code change, not a flag.)
//
// It is strictly LONGER than SessionRawLogStaleAge on purpose: the file arm
// must have finished with a container long before a container-level row looks
// at it — the ordering SessionRawLogDirStaleAge already establishes.
package scratch

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/nickhuang99/gopi/internal/sessionlayout"
)

const (
	// SessionLegacyDirStaleAge is the age past which an unheld legacy-residue
	// entry (a task-named container, a stray root directory or file, a probe's
	// checkout, a private profile) is garbage. See the file header for why it
	// is longer than the raw-log window.
	SessionLegacyDirStaleAge = 180 * 24 * time.Hour

	// SessionLegacyContainerStaleAge is the age a CONTAINER must reach before
	// anything below it is considered at all, and before the container itself
	// may be removed for being empty of this row's candidates. It is strictly
	// longer than SessionLegacyDirStaleAge for the reason the raw-log pair
	// already states: the inner arm must have finished with a container long
	// before the container arm looks at it.
	SessionLegacyContainerStaleAge = SessionLegacyDirStaleAge + 24*time.Hour

	// TreeNameSessionLegacyDirs / TreeNameSessionLegacyFiles are the two
	// ROOT-LEVEL rows' identities; the container row carries the registry's own
	// constant (Rule 14: the family registry names the tree it is bound to and
	// the declared set holds the same name — one spelling, two readers), so the
	// cross-check in sessions_tree_families_test.go compares two references to
	// ONE value rather than two spellings that could drift.
	//
	// DIRS carries the name the registry binds three of its families to
	// (`session-legacy`): those families all live at the sessions ROOT and
	// their predicates are disjoint by construction — an id-named session
	// container is refused by IsRootTaskContainerName, and the stray shape
	// tables are EXACT lists, never catch-alls — so ONE row can carry the union
	// without ever collecting a directory the registry did not name. FILES is a
	// second name because a declared tree's Kind is ONE shape: a row cannot
	// take both.
	TreeNameSessionLegacyDirs  = sessionlayout.TreeSessionLegacy
	TreeNameSessionLegacyFiles = sessionlayout.TreeSessionLegacy + "/files"
)

// SessionLegacyRootTrees returns the two ROOT-LEVEL rows of the legacy set.
func SessionLegacyRootTrees() []DeclaredTree {
	return []DeclaredTree{
		sessionLegacyRootLevelTree(TreeNameSessionLegacyDirs, true, sessionLegacyRootDirName),
		sessionLegacyRootLevelTree(TreeNameSessionLegacyFiles, false, sessionlayout.IsRootResidueFileName),
	}
}

// sessionLegacyRootLevelTree assembles ONE root-level row: its candidates are
// the sessions tree's DIRECT children, its namespace is the caller's predicate,
// its age is the legacy window, and its root is the sessions tree itself.
//
// dir selects the SHAPE (a directory removed whole, or a file) and is the only
// difference between the two rows.
func sessionLegacyRootLevelTree(name string, dir bool, match func(string) bool) DeclaredTree {
	// A FILE row carries no one suffix: the registry's residue names are an
	// EXACT shape table (a crash dump, `run-*.log`, `note.txt`, …), so the
	// predicate IS the whole namespace — EntryKindFileMatch, which the row's
	// own contract requires to carry an EntryMatch.
	kind := EntryKindFileMatch
	if dir {
		kind = EntryKindDir
	}
	return DeclaredTree{
		Name:       name,
		Class:      TreeClassLegacy,
		Kind:       kind,
		StaleAge:   SessionLegacyDirStaleAge,
		EntryMatch: match,
		Resolve:    SessionRawLogRoot, // the SAME root as the raw-log row: the sessions tree itself
	}
}

// sessionLegacyRootDirName is the root-level DIRECTORY row's namespace
// predicate: the union of the registry's two `bound:` directory families at
// that depth — the task-named container and the measured stray shape. Both are
// the registry's OWN exported functions (Rules 13/14): a second spelling here
// would be a second answer to "is this directory the residue family?", and the
// disagreement would either leave the family unbounded or collect a directory
// the registry never named.
//
// It is deliberately NOT "anything that is not a session container": the
// registry's stray-dir family is an EXACT shape table, and a catch-all here
// would sweep a root-level directory nobody has decided about — the one
// outcome this pair of registries exists to report instead.
func sessionLegacyRootDirName(name string) bool {
	return sessionlayout.IsRootTaskContainerName(name) || sessionlayout.IsStrayRootDirName(name)
}

// sessionLegacyContainerRow is the CONTAINER-LEVEL row: a probe's clone or a
// private profile, one level down inside a session container.
//
// It is the same SHAPE as the raw-log row's container arm (EntryKindTreeFileSuffix
// is the one-level-down kind) with its own predicate and a longer window.
func sessionLegacyContainerRow() DeclaredTree {
	root := SessionRawLogRoot
	return DeclaredTree{
		Name:         sessionlayout.TreeSessionLegacyContainers,
		Class:        TreeClassLegacy,
		Kind:         EntryKindTreeFileSuffix,
		StaleAge:     SessionLegacyDirStaleAge,
		RootStaleAge: SessionLegacyContainerStaleAge,
		// The container arm's namespace is "a directory carrying a marker
		// CHILD", which no NAME answers: the row's own listing
		// (sessionLegacyContainerEntries) is the ONE predicate, and the entry
		// the generic walk sees — a container, which is not itself this row's
		// candidate — is refused here. It is spelled as a refusal rather than
		// left nil so the row can never fall back to "every stale directory in
		// the tree".
		EntryMatch: func(string) bool { return false },
		Resolve:    root,
		// ONE listing behind BOTH faces (Rule 15: a declared field nothing
		// reads is a lie — and two walks would be two answers): the plan names
		// exactly what the sweep removes. The window argument the hook receives
		// is ignored on purpose: this row's candidates are decided by TWO windows
		// (the container's and the directory's), and collapsing them into one
		// caller-supplied number would make a live container's contents
		// collectable under a short window.
		List: func(time.Duration) ([]TreeEntry, error) {
			r, err := root()
			if err != nil {
				return nil, err
			}
			return sessionLegacyContainerEntries(r, time.Now(), SessionLegacyDirStaleAge, SessionLegacyContainerStaleAge)
		},
		Sweep: func(time.Duration) (int, int64, error) {
			r, err := root()
			if err != nil {
				return 0, 0, err
			}
			entries, lerr := sessionLegacyContainerEntries(r, time.Now(), SessionLegacyDirStaleAge, SessionLegacyContainerStaleAge)
			if lerr != nil {
				return 0, 0, lerr
			}
			return sweepLegacyEntries(sessionlayout.TreeSessionLegacyContainers, entries)
		},
	}
}

// sessionLegacyContainerEntries is the container row's ONE listing walk, shared
// by its plan face and its sweep, because two walks would be two answers.
//
// THE THREE RULES IT APPLIES, each of which alone keeps a live run's directory
// out of the plan:
//
//  1. the container is not held by any live process — the INDEXED /proc verdict
//     (heldSet: one pass for the whole tree, then one map lookup per candidate,
//     the shape that replaced the per-candidate table walk, memory #2632);
//  2. the container is AGED past the row's own container window;
//  3. only a DIRECTORY whose OWN marker child exists is a candidate, and the
//     marker is read as a child of the candidate's path — never through a
//     symlink the sweep would otherwise follow (nothing here follows one).
//
// A container or a directory that cannot be listed or measured is SKIPPED,
// never guessed at (Rule 15: an unmeasurable entry is a KEEP). A root that does
// not exist yields an empty listing — a host with no sessions tree has nothing
// to bound — while a root that exists and cannot be READ is an error.
func sessionLegacyContainerEntries(root string, now time.Time, olderThan, containerOlderThan time.Duration) ([]TreeEntry, error) {
	entries, err := os.ReadDir(root)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	held := heldSet(ScanHolders(), root)
	measurer := DeclaredTree{Kind: EntryKindDir, Class: TreeClassLegacy}
	var out []TreeEntry
	for i, entry := range entries {
		if i >= TreeEntryScanLimit {
			break
		}
		if !entry.IsDir() {
			continue
		}
		container := filepath.Join(root, entry.Name())
		if heldIn(container, held) {
			continue // a live process stands in the container: nothing below it is ours
		}
		if !unheldRegistered(container) {
			// The writer-owned half of the same verdict, read from the
			// candidate itself: a recorded owner the kernel still confirms
			// live keeps the container at ANY age. It is checked HERE as well
			// as at the delete, because a plan that named an entry the delete
			// then refuses would be a plan nobody can act on (Rule 15).
			continue
		}
		stale, serr := ScratchEntryStale(container, now, containerOlderThan)
		if serr != nil || !stale {
			continue
		}
		children, cerr := os.ReadDir(container)
		if cerr != nil {
			continue // unreadable: KEEP (Rule 15)
		}
		for j, child := range children {
			if j >= TreeEntryScanLimit {
				break
			}
			if !child.IsDir() {
				continue
			}
			path := filepath.Join(container, child.Name())
			if !sessionLegacyMarkerDir(path) {
				continue // carries neither marker: not this row's family
			}
			stale, serr := ScratchEntryStale(path, now, olderThan)
			if serr != nil || !stale {
				continue
			}
			if heldIn(path, held) || !unheldRegistered(path) {
				continue
			}
			out = append(out, measuredEntry(measurer, path, true, now))
		}
	}
	return out, nil
}

// sessionLegacyMarkerDir reports whether a directory carries one of the two
// markers this row's families are identified by: git's own metadata directory
// (a checkout) or the profile directory a sandbox materialises. Both names come
// from the registry (sessionlayout.GitMarkerName / SandboxProfileMarkerName),
// referenced rather than re-spelled — the registry's Match functions ask about
// the same two names, and a second spelling would let the sweep and the
// classifier disagree about which directory is this family.
func sessionLegacyMarkerDir(path string) bool {
	if _, err := os.Stat(filepath.Join(path, sessionlayout.GitMarkerName)); err == nil {
		return true
	}
	if _, err := os.Stat(filepath.Join(path, sessionlayout.SandboxProfileMarkerName)); err == nil {
		return true
	}
	return false
}

// sweepLegacyEntries removes the candidates a row's own listing produced, one
// by one, through removeCollectableOfTree — the ONE delete path every declared
// tree uses, so the root refusal, the live-holder re-check and the .holder
// verdict stay one rule with one implementation instead of a second one written
// here.
//
// A refused delete is REPORTED and the rest of the listing still runs: one
// unremovable entry must not abandon the sweep of the others (Rule 15).
func sweepLegacyEntries(rowName string, entries []TreeEntry) (int, int64, error) {
	tree := DeclaredTree{Name: rowName, Class: TreeClassLegacy}
	removed := 0
	var freed int64
	for _, e := range entries {
		if rerr := removeCollectableOfTree(tree, e.Path); rerr != nil {
			fmt.Fprintf(os.Stderr, "scratch: GC could not remove legacy residue %s (%s): %v\n", e.Path, rowName, rerr)
			continue
		}
		removed++
		freed += e.Size
	}
	return removed, freed, nil
}

// SessionLegacyTreeNames returns the three rows' names — the list the
// cross-check in sessions_tree_families_test.go reads against the declared set.
func SessionLegacyTreeNames() []string {
	return []string{TreeNameSessionLegacyDirs, TreeNameSessionLegacyFiles, sessionlayout.TreeSessionLegacyContainers}
}
