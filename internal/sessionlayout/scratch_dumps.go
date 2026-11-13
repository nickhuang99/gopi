// scratch_dumps.go — the SCRATCH dumps class trees in the SIBLING registry: the
// two directories the error-path dump writers create when nothing points them
// elsewhere, and the census names that reach them on a DEFAULT configuration.
//
// WHY THEY ARE REGISTERED (the defect, measured 2026-10-11 on this host).
// CONFIG.md advertised `gopi tool scratch-gc families --tree dumps` as the census
// recipe for `GOPI_ERROR_DUMP_DIR` / `GOPI_1214_DUMP_DIR`. That name is the
// PROFILE's `dumps/` (sibling_families.go), which exists only when an operator
// exports one of those knobs; on the documented default the writers land in the
// SCRATCH home's class directories instead (`internal/scratch.DumpDir(<class>)` —
// `<scratch home>/dumps/gopi-dumps`), and no name could see them. Measured: the
// recipe exits 1 on a default configuration while 405 dumps sat in
// `<scratch home>/dumps/gopi-dumps`.
//
// WHY THE SAME REGISTRY AND NOT A SECOND MECHANISM (Rules 3/35). The census face
// enumerates `SiblingTrees()`, prints one line per SHAPE with the family that
// claims it and the decision taken about it, and reports an unclaimed shape as
// unclassified. That is EXACTLY the question these two roots need answered, for
// the same kind of entry (`<stamp>.dump.json`), so the rows are declared HERE and
// the FAMILY LIST is the SAME function (dumpsFamilies) — one registry, four
// roots. A caller asks the group it wants
// (`SiblingTrees()` for the profile's children, `ScratchDumpsTrees()` for the
// scratch class trees) because the two answer to different homes, and a name that
// resolved to "whichever happens to be configured" would answer a question nobody
// asked.
//
// WHERE THE ROOTS COME FROM. The resolver is NOT spelled here: it is a map of
// tree NAME → root resolver, installed by internal/config from
// `internal/scratch.ScratchDumpClassRoots()` — the package that owns both the
// scratch home and the declared rows, so a name and its root cannot be paired
// wrongly. internal/scratch imports internal/tools and this package's own
// consumer; the reverse import is impossible (Rules 13/14).
package sessionlayout

import (
	"errors"
	"strconv"
	"strings"
)

// The SCRATCH dumps trees' names. They are constants (Rules 1b/13/14) because the
// registry, the census's report line, the declared-tree row in internal/scratch and
// CONFIG.md all name the same two trees.
const (
	// ScratchDumpsTree is the census name for the scratch home's FIRST dump class
	// directory (the generic error-path class): what
	// `gopi tool scratch-gc families --tree scratch-dumps` censuses, and the tree
	// `error-request-dump` is bound to there.
	ScratchDumpsTree = "scratch-dumps"
	// ScratchDumpsTree1214 is the same for the SECOND class directory (the
	// provider's 1214 error class).
	ScratchDumpsTree1214 = "scratch-dumps-1214"
)

// ScratchDumpsTrees returns the two SCRATCH dumps trees, in a stable order
// (the generic class first, then the 1214 class).
//
// Their FAMILIES are `dumpsFamilies` — the same registry row the profile's dump
// trees carry, so an entry beside the profile's is classified by the SAME
// predicate here. The declared tree each row names is its own row in
// internal/scratch (`scratch-dumps` / `scratch-dumps-1214`), which is what makes
// the census's `bound:` decision about this root enforceable rather than
// decorative.
func ScratchDumpsTrees() []SiblingTree {
	return []SiblingTree{
		{
			Name:     ScratchDumpsTree,
			Families: dumpsFamilies(ScratchDumpsTree),
			Resolve:  scratchDumpsRoot(ScratchDumpsTree),
		},
		{
			Name:     ScratchDumpsTree1214,
			Families: dumpsFamilies(ScratchDumpsTree1214),
			Resolve:  scratchDumpsRoot(ScratchDumpsTree1214),
		},
	}
}

// ScratchDumpsTreeNames returns the two tree names in the same stable order, so a
// reader (a doc, a test, a usage line) does not spell them again.
func ScratchDumpsTreeNames() []string {
	return []string{ScratchDumpsTree, ScratchDumpsTree1214}
}

// ScratchDumpsTreeRoot resolves one class tree's root. The census's own walk uses
// the row's Resolve (they are the same function), and a caller that has a NAME
// and no row — a test asserting the resolved path, a diagnostic — comes here.
func ScratchDumpsTreeRoot(name string) (string, error) {
	for _, t := range ScratchDumpsTrees() {
		if t.Name == name {
			return t.Resolve()
		}
	}
	return "", fmtNoScratchDumpsTree(name)
}

// scratchDumpsRoot returns ONE class tree's root resolver, reading the map
// internal/config installed. The name is captured rather than passed by the
// caller, so a row cannot ask for another tree's root.
func scratchDumpsRoot(name string) func() (string, error) {
	return func() (string, error) {
		if f, ok := scratchDumpsRootResolvers[name]; ok && f != nil {
			return f()
		}
		return "", errScratchDumpsResolverUnwired
	}
}

// RegisterScratchDumpsRootResolvers installs the map of tree NAME → root
// resolver, built by internal/scratch (ScratchDumpClassRoots) and wired once from
// internal/config's own init, for the same reason the profile resolver is wired
// there: internal/scratch imports internal/tools, which imports this package, so
// the reverse import is impossible.
//
// A nil entry is refused BY NAME rather than installed: a tree whose resolver is
// missing would otherwise resolve "" and walk the process's own working
// directory, which is the failure mode the sibling trees' own unwired resolver
// exists to refuse (Rule 15).
func RegisterScratchDumpsRootResolvers(resolvers map[string]func() (string, error)) {
	for name, f := range resolvers {
		if f == nil {
			continue
		}
		scratchDumpsRootResolvers[name] = f
	}
}

// scratchDumpsRootResolvers is the seam above. Empty (a test that builds this
// package standalone) makes every class tree resolve to a NAMED error instead of
// an empty path.
var scratchDumpsRootResolvers = map[string]func() (string, error){}

// errScratchDumpsResolverUnwired reports the one failure mode of the seam: a
// caller that resolved a scratch dumps tree before internal/config wired the
// resolvers.
var errScratchDumpsResolverUnwired = errors.New(
	"sessionlayout: scratch dumps root resolver is not wired (import internal/config)")

// fmtNoScratchDumpsTree names a lookup for a tree this registry does not carry,
// listing the ones it does — a caller that misspelled a name reads which names
// exist rather than an empty string.
func fmtNoScratchDumpsTree(name string) error {
	return errors.New("sessionlayout: no scratch dumps tree is named " + strconv.Quote(name) +
		" (this registry carries " + strings.Join(ScratchDumpsTreeNames(), ", ") + ")")
}
