package scratch

import (
	"os"
	"path/filepath"
	"time"
)

// declared_tmpdir_residue.go — the row that bounds the DECLARED TEMP ROOT itself.
//
// WHY THIS ROW EXISTS (measured 2026-11-02 on nick-sager). `_tmp` is not a wild
// directory: it is gopi's OWN declared default temp workspace
// (`config.ResolveTmpDirConfigured` → `DefaultTmpDir()` → `<home>/workspace/_tmp`,
// `DefaultTmpDirLeaf` in scratch.go, documented at CONFIG.md:82/557). gopi
// publishes it into TMPDIR for every child, so every `scratch.MkdirTemp` that
// falls through to `os.MkdirTemp("", pattern)` — and every `mktemp -d` a run
// shells out to — lands a directory HERE. The declared-trees set rooted at that
// workspace for every OTHER family (the raw logs, the sessions tree, the build
// cache, the budget runs) and at its `go-build*` children (`go-tmp`), while the
// root's OWN residue was bounded by nothing:
//
//	total 20 886 MB across 528 top-level entries, of which the only arm that
//	reached the tree at all was `go-tmp` (106 `go-build*` / 7 466 MB), and the
//	rest — 224 run-chosen names / 11 815 MB — was named by no row.
//
// That is a real defect with a measured shape, and it is NOT "delete everything
// old in _tmp": the 224 run-chosen names are directories a RUN invented
// (`sigpipe-repro-…`, `walrep`, `probe-repo`, `loadbearing`), whose owner is
// unknowable from the name, and a sweep of those would be exactly the blunt
// "everything older than N here" that DeclaredTree.EntryMatch exists to prevent
// (Rule 42: nothing signals or destroys what it does not own). So this row takes
// the ONE namespace the repository already trusts for residue it created itself
// — `IsBaseResidueName`, the base sweep's predicate, whose three families are
// gopi-<token> / gocache-<token> / probe-<token> — and nothing else.
//
// MEASURED REACH of that predicate over the real root (2026-11-02): 36 of the
// 339 non-go-build entries, 345 MB. Every one of them is residue: probe homes, a
// probe profile, two 146 MB `gopi-base-*` snapshots from a base-sweep
// investigation. The remaining 303 entries / 18 606 MB are REFUSED by the
// predicate, which is the intended answer and the reason this row is safe to run
// unattended: a directory whose name carries no gopi/probe/gocache token is not
// ours to collect, whatever its age.
//
// THE PREDICATE IS BORROWED, NOT COPIED. `IsBaseResidueName` is one definition
// (Rule 14); a second spelling of "what names did gopi generate" is precisely the
// drift the base sweep's own comment warns about. That borrow is also why this
// row needs no new name table and no new writer anchor: the families it takes are
// already declared, already tested, and already the ones the base sweep collects.

const (
	// TmpDirResidueTreeName is this row's identity on the watchdog log line
	// (`tree=`), in the declared-set gate's diagnostics and in `--tree`'s
	// selector. One definition (Rules 13/14).
	TmpDirResidueTreeName = "tmpdir-residue"

	// TmpDirResidueStaleAge is this tree's window.
	//
	// IT IS LONGER THAN THE BASE SWEEP'S 6h ON PURPOSE, and the difference is a
	// measurement, not a preference. The base sweep collects residue at the
	// FILESYSTEM ROOT OF SCRATCH (`/dev/shm` by default), whose entries a run
	// mints and abandons within one command. This tree is the OPERATOR'S
	// WORKSPACE ROOT: a `gopi-base-*` snapshot or a `probe-home-*` fixture there
	// is something a human opens by hand while investigating, and the measured
	// population (2026-11-02) includes fixtures dated 228h–474h that a report
	// cites by name. 7 days is many times the longest run on this host and still
	// bounds the tree: everything older than a week is a run nobody is returning
	// to. The entries that dominate the byte count (the two 146 MB
	// `gopi-base-*` snapshots, 350h) clear it comfortably.
	TmpDirResidueStaleAge = 7 * 24 * time.Hour
)

// TmpDirResidueRoot resolves the tree's ROOT — the ONE spelling of this tree's
// path.
//
// IT RESOLVES THE ROOT THE RESIDUE ACTUALLY FOLLOWS rather than the knob's
// built-in default: a residue directory is created by `os.MkdirTemp("", …)`,
// which reads the PROCESS's temp root, and gopi publishes its resolved tmpdir
// into TMPDIR for every child it spawns. So the honest order is "the temp root
// this process would use when it is explicit, else gopi's own default" — and
// EffectiveTempRoot() is the ONE probe for the first half (it is the same
// function the Rule 22 ban reads, so the sweep and the ban can never disagree
// about where the process's temp workspace is).
//
// A root that does not exist is ErrDeclaredTreeUnconfigured: a reported no-op,
// never an alarm (the same contract every other resolver here honours).
func TmpDirResidueRoot() (string, error) {
	root := ""
	if effective, explicit, err := EffectiveTempRoot(); err == nil && explicit {
		root = effective
	}
	if root == "" {
		d, derr := DefaultTmpDir()
		if derr != nil {
			return "", derr
		}
		root = d
	}
	root = CanonicalPath(filepath.Clean(root))
	fi, err := os.Stat(root)
	if err != nil || !fi.IsDir() {
		return "", ErrDeclaredTreeUnconfigured
	}
	return root, nil
}

// tmpDirResidueTree is this row as a DeclaredTree — the designed extension
// point: the `trees` face and the resident tick reach it through
// DeclaredTrees(), and the declared-set gate names it.
//
// EntryKindDir + a non-nil EntryMatch is the SHAPE the gate requires of a row
// that takes directories (a row with no predicate would make every aged
// operator directory collectable) — and the predicate here is the borrowed
// residue namespace, so the run-chosen majority of the root is refused by
// construction.
func tmpDirResidueTree() DeclaredTree {
	return DeclaredTree{
		Name:       TmpDirResidueTreeName,
		Class:      TreeClassTmpDirResidue,
		Kind:       EntryKindDir,
		EntryMatch: IsBaseResidueName,
		StaleAge:   TmpDirResidueStaleAge,
		Resolve:    TmpDirResidueRoot,
	}
}
