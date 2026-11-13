// families_scan.go — the ENUMERATION half of the family registry: walk a real
// sessions tree and classify every entry in it.
//
// WHY A WALK AND NOT A NAMESPACE PREDICATE. The declared-tree registry
// (internal/scratch) bounds a tree by asking "is this name OURS?" — which is
// answerable only for the families somebody already named. Measured on this
// host (2026-10-01), 202,534 files / 9.3 GiB of the sessions tree answered "no"
// to that question, and nothing reported them. A walk asks the RANGE question
// instead — "what IS here?" — so every family the registry does not claim is
// returned as unclassified and a reader has to decide (Rule 15: an unreported
// family is a silent failure, not a tidy default).
//
// SHAPE, not name. Two entries of one family produce one SHAPE token
// (`<id>.jsonl`, `<id>.raw.log`, `attachments`), so a report line stands for a
// family rather than for a file. That is what lets the census in
// devdoc/sessions-tree-scope-census.md and the gate's fixture agree about what
// a "family" is.
//
// COST. A walk over ~200k entries in ~80k directories costs a handful of
// seconds (measured: the same shape of walk over this host's tree). It calls
// os.Stat only where a family's membership test needs it: the two
// checkout/profile families ask about `.git` and `.gopi`, and nothing else
// stats at all.
package sessionlayout

import (
	"os"
	"path/filepath"
)

// The rollups the census reports. They are the sink of ScanFamiliesByShape,
// keyed by the SHAPE token so two entries of one family collapse.
const (
	// ScanFilesKey is the rollup key of the total file count.
	ScanFilesKey = "files"
	// ScanDirsKey is the rollup key of the total directory count.
	ScanDirsKey = "dirs"
)

// FamilyScan is the census of ONE sessions tree, by shape.
type FamilyScan struct {
	// Root is the tree that was walked.
	Root string
	// ByShape maps a shape token to what was measured for it.
	ByShape map[string]ShapeScan
	// Unclassified lists every entry whose shape no family claims — the state
	// a caller REPORTS (Rule 15), because it means the tree grew a family the
	// registry does not know.
	Unclassified []Unclassified
	// Dirs/files are the totals (they include every entry, classified or not).
	Dirs  int
	Files int
	// Bytes is the sum of the regular files' sizes.
	Bytes int64
}

// ShapeScan is one shape's measurement inside a scan.
type ShapeScan struct {
	// Family is the registered family that claims the shape ("" when
	// unclassified).
	Family string
	// Decision is that family's decision ("" when unclassified).
	Decision Decision
	// Dir marks a directory shape.
	Dir bool
	// Count is how many entries carried the shape.
	Count int
	// Bytes is their summed size (0 for directories: a subtree is measured by
	// the family that owns it, never re-measured here).
	Bytes int64
}

// ScanFamilies walks a sessions tree and classifies every entry, one level
// deep plus the root itself:
//
//	<root>/<entry>                 classified at WhereRoot
//	<root>/<container>/<entry>     classified at WhereContainer
//
// Exactly two levels, because that is the tree's own shape (internal/tools'
// session layout): anything deeper belongs to a container's own subtree and is
// the container family's business, not the registry's.
//
// A tree that does not exist is an ERROR, never an empty census: "nothing
// there" and "could not look" must not render the same (Rule 15).
func ScanFamilies(root string) (FamilyScan, error) {
	return ScanFamiliesWith(root, families)
}

// ScanFamiliesWith walks the same tree but classifies against the family list
// it is GIVEN. It exists for one reason: a gate must be able to drive the scan
// with a REDUCED registry and show that an entry the reduced list no longer
// claims comes back UNCLASSIFIED — a red-proof that wrote its own copy of the
// walk would prove nothing about the code that runs.
func ScanFamiliesWith(root string, fams []Family) (FamilyScan, error) {
	scan := FamilyScan{Root: root, ByShape: map[string]ShapeScan{}}
	top, err := os.ReadDir(root)
	if err != nil {
		return FamilyScan{}, err
	}
	for _, e := range top {
		child := filepath.Join(root, e.Name())
		scan.add(fams, FamilyEntryFor(child, e.IsDir(), WhereRoot), sizeOf(child, e.IsDir()))
		if !e.IsDir() {
			continue
		}
		if !IsSessionContainerName(e.Name()) {
			// A root-level directory that is NOT an id-named container (a
			// probe's `smoke-clone`, a hand-made evidence dir): it was
			// classified above as a family of its own, and its CONTENTS are
			// that family's business, not the registry's. Descending into a
			// checkout would turn a two-level census into a whole-repository
			// walk.
			continue
		}
		subs, rerr := os.ReadDir(child)
		if rerr != nil {
			// A container that cannot be listed is REPORTED as unclassified
			// rather than skipped: an unreadable subtree is exactly the fact a
			// census must not swallow.
			scan.Unclassified = append(scan.Unclassified, Unclassified{
				Path: child, Name: e.Name(), Shape: ShapeOf(e.Name()), Dir: true, Where: WhereContainer,
			})
			continue
		}
		for _, se := range subs {
			sub := filepath.Join(child, se.Name())
			scan.add(fams, FamilyEntryFor(sub, se.IsDir(), WhereContainer), sizeOf(sub, se.IsDir()))
		}
	}
	return scan, nil
}

// add folds one classified entry into the scan's rollup, classifying it against
// the family list it is handed.
func (s *FamilyScan) add(fams []Family, e Entry2, size int64) {
	if e.Dir {
		s.Dirs++
	} else {
		s.Files++
		s.Bytes += size
	}
	key := e.Shape
	if e.Dir {
		key = e.Shape + "/"
	}
	entry, ok := s.ByShape[key]
	if !ok {
		entry = ShapeScan{Dir: e.Dir}
	}
	entry.Count++
	entry.Bytes += size
	fam, ok := classifyWith(fams, e)
	if !ok {
		entry.Family = ""
		entry.Decision = DecisionUnclassified
		s.Unclassified = append(s.Unclassified, Unclassified{
			Path: e.Path, Name: e.Name, Shape: e.Shape, Dir: e.Dir, Where: e.Where,
		})
	} else {
		entry.Family = fam.Name
		entry.Decision = fam.Decision
	}
	s.ByShape[key] = entry
}

// sizeOf returns one entry's own size: a regular file's byte count, 0 for a
// directory (its subtree is the container family's business) and 0 for anything
// that is not a regular file. It never follows a symlink and never walks.
func sizeOf(path string, dir bool) int64 {
	if dir {
		return 0
	}
	fi, err := os.Lstat(path)
	if err != nil || !fi.Mode().IsRegular() {
		return 0
	}
	return fi.Size()
}

// UnclassifiedShapes returns the DISTINCT shapes of the scan's unclassified
// entries with their counts, so one report line stands for a family rather
// than for ten thousand files.
func (s FamilyScan) UnclassifiedShapes() map[string]int {
	out := map[string]int{}
	for _, u := range s.Unclassified {
		key := u.Shape
		if u.Dir {
			key += "/"
		}
		out[key]++
	}
	return out
}
