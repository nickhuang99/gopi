// scratch_dumps.go — the SCRATCH dumps CLASS trees: the two directories the
// error-path dump writers create when nothing points them elsewhere, and the
// census names that reach them on a DEFAULT configuration.
//
// WHY THIS FILE EXISTS (the defect, measured 2026-10-11 on this host).
// internal/deepseek.minimalCoreDumpDir / guardDumpDir document their default as
// `scratch.DumpDir(<class>)` — `<scratch home>/dumps/gopi-dumps` and
// `<scratch home>/dumps/gopi-1214-dumps`, scratch.go joining HomeDir() +
// DumpsDirName + the class. That default is what runs on ANY host that has not
// exported the dump knobs, and it is the default the repo's own suite writes
// through (measured: 405 dumps in `<scratch home>/dumps/gopi-dumps`).
//
// What the census could see was only the PROFILE-rooted pair
// (`sibling-dumps` / `sibling-dumps-1214`, sibling_dumps.go), which exists solely
// when an operator points `GOPI_ERROR_DUMP_DIR` / `GOPI_1214_DUMP_DIR` at their
// profile. CONFIG.md advertised `gopi tool scratch-gc families --tree dumps` as
// the census recipe for both knobs; on a default configuration that command exits
// 1 with "no such file or directory", because the tree it names is the profile's
// and the profile has none. The dumps that EXIST — the scratch class tree — were
// reachable by no name at all.
//
// WHAT THIS FILE ADDS, and it is deliberately ONE pair of rows built by the SAME
// constructor the profile pair uses: a declared-tree row per class directory, so
// the sweep machinery that already ticks bounds them too — the shared walk, the
// ONE /proc pass, the `.holder` registration, the "a declared root is never a
// candidate" refusal and the removal re-check, NONE of it re-implemented
// (Rule 35: no new machinery, no new dependency). The census trees that reach
// them (sessionlayout/scratch_dumps.go) carry the SAME family list the profile
// dump rows carry (`dumpsFamilies`), so an entry classified
// `error-request-dump` beside the profile dump is classified the same way here:
// one registry, four roots.
package scratch

import (
	"path/filepath"
	"time"
)

const (
	// ScratchDumpsTreeName is the SCRATCH dumps class tree's declared identity:
	// the logfmt `tree=` field on the watchdog line, the name the declared-set
	// gate diagnoses with, and the tree the census's `bound:` family names. The
	// census registry cannot import this package (it is imported BY this
	// package's own consumer internal/tools), so this constant's VALUE is held
	// against the registry's (sessionlayout.ScratchDumpsTree) by a pin that
	// reads BOTH sources — a pair of literals with nothing joining them is
	// exactly the drift Rules 13/14 ban.
	//
	// It is deliberately NOT one of the sibling trees the CENSUS enumerates
	// automatically (`sessions/archive/backups/notify/dumps/dumps-1214`): that
	// list is the PROFILE's own children, and this tree is not under the profile
	// at all — it is under the scratch home. It is reachable by name
	// (`--tree scratch-dumps`) and printed by `--list`, which is what the
	// enumeration is for.
	ScratchDumpsTreeName = "scratch-dumps"

	// ScratchDumps1214TreeName is the second class directory's identity: the
	// same shape for the provider's 1214 error class (guardDumpClass). A
	// separate row rather than a second root on the first, for the same reason
	// the profile pair is two rows: the two knobs answer to different guards, so
	// one may hold dumps while the other is empty.
	ScratchDumps1214TreeName = "scratch-dumps-1214"

	// ScratchDumpsClassDirName is the class directory `minimalCoreDumpDir`'s
	// default lands in (`<scratch home>/dumps/gopi-dumps`). Its VALUE is
	// internal/deepseek's `minimalCoreDumpClass`, re-spelled because that package
	// imports THIS one (through its own `scratch.DumpDir` call), so the constant
	// cannot be imported back; the equality of the two is asserted by this
	// package's own boundary test, which reads both sources.
	ScratchDumpsClassDirName = "gopi-dumps"

	// ScratchDumps1214ClassDirName is the second class directory
	// (`<scratch home>/dumps/gopi-1214-dumps`, `guardDumpClass`), re-spelled for
	// the same reason and pinned by the same test.
	ScratchDumps1214ClassDirName = "gopi-1214-dumps"
)

// ScratchDumpsStaleAge is how long a request dump may sit in a class directory
// before the sweep may collect it.
//
// It is the SAME VALUE as the profile dump tree's window (SiblingDumpsStaleAge)
// and a separate NAME on purpose: the two are two decisions — one about the
// operator's profile directory, one about the scratch home the suite itself
// writes through — and the declared-set gate accounts for windows BY NAME (an
// unreferenced constant is a bound nobody applies). The value relationship is
// what the gate pins, so a later change to one window cannot silently move the
// other: see TestScratchDumpWindowsAgree.
const ScratchDumpsStaleAge = 14 * 24 * time.Hour

// scratchDumpsTree is the declared row for the first class tree.
func scratchDumpsTree() DeclaredTree {
	return scratchDumpClassTree(ScratchDumpsTreeName, ScratchDumpsClassDirName)
}

// scratchDumps1214Tree is the second row: the same shape for the 1214 class.
func scratchDumps1214Tree() DeclaredTree {
	return scratchDumpClassTree(ScratchDumps1214TreeName, ScratchDumps1214ClassDirName)
}

// scratchDumpClassTree builds one class tree's declared row. It is the PROFILE
// dump rows' own shape (siblingDumpTree) pointed at the other home: the
// candidates are the dump FILES (`<siblingDumpFileSuffix>`), the namespace is the
// same predicate the profile row uses, and the window is the same 14 days — so
// the two homes cannot drift into two different sweep rules for the same kind of
// file.
//
// The ROOT is the class directory itself, which is what the writer MkdirAll's, so
// the row needs no namespace predicate of its own: it cannot claim anything the
// writer did not create, and the shared walk's own rule (a declared ROOT is never
// a candidate, at any age) keeps the class directory from ever being removed as a
// unit — the writer re-creates it on the next dump either way.
func scratchDumpClassTree(name, dirName string) DeclaredTree {
	return DeclaredTree{
		Name:       name,
		Class:      TreeClassErrorDump,
		Kind:       EntryKindFileSuffix,
		Suffix:     siblingDumpFileSuffix,
		StaleAge:   ScratchDumpsStaleAge,
		EntryMatch: isSiblingDumpName,
		Resolve:    scratchDumpClassResolver(dirName),
	}
}

// scratchDumpClassResolver returns ONE class tree's root resolver: the class
// directory under the scratch home's `dumps/` (ScratchDumpsClassRoot — the SAME
// join the writer makes). The leaf is a PARAMETER rather than a second function,
// so the two rows cannot resolve to the same directory by a copy-paste slip.
func scratchDumpClassResolver(dirName string) func() (string, error) {
	return func() (string, error) {
		if HomeDir() == "" {
			return "", nil
		}
		return ScratchDumpsClassRoot(dirName)
	}
}

// ScratchDumpsClassRoot returns the directory a class's dumps are written to —
// `DumpDir(class)`'s own join, spelled through the SAME two names (HomeDir +
// DumpsDirName), so a reader that asks this function and the writer that calls
// DumpDir can never disagree about where a class's dumps live.
//
// The directory is NOT created here: a sweep that materialised the tree it bounds
// would make "the writers have never run on this host" and "the tree is empty"
// render the same. The walk reports the missing root as UNCONFIGURED instead, and
// the census reports it as "no root on this host" — the existing contract for a
// declared tree that need not exist.
func ScratchDumpsClassRoot(class string) (string, error) {
	if class == "" {
		class = defaultDumpClass
	}
	if HomeDir() == "" {
		return "", nil
	}
	return CanonicalPath(filepath.Join(HomeDir(), DumpsDirName, class)), nil
}

// ScratchDumpClassRoots returns the class trees' root resolvers keyed by their
// declared tree NAME. It is the ONE place the two are paired, and it is what
// internal/config wires into the census registry with a single call: the names
// and the roots come from the package that owns both, so the census cannot hold a
// name whose root resolves somewhere else.
func ScratchDumpClassRoots() map[string]func() (string, error) {
	return map[string]func() (string, error){
		ScratchDumpsTreeName:     scratchDumpClassResolver(ScratchDumpsClassDirName),
		ScratchDumps1214TreeName: scratchDumpClassResolver(ScratchDumps1214ClassDirName),
	}
}

// ScratchDumpsClassTrees returns the two class trees' declared rows. It is the
// ONE place they are assembled (DeclaredTrees appends this), so a caller that
// needs the set reads the same construction the sweep walks rather than a second
// spelling of it.
func ScratchDumpsClassTrees() []DeclaredTree {
	return []DeclaredTree{scratchDumpsTree(), scratchDumps1214Tree()}
}
