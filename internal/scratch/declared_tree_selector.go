// declared_tree_selector.go — THE ONE RESOLVER from a tree NAME to the rows of
// the declared set that name selects.
//
// WHY THIS FILE EXISTS (the defect it closes). `gopi tool scratch-gc trees`
// swept EVERY declared tree and there was no way to sweep one. On the operator's
// host that arm is one press of a button away from reclaiming the ~50 GB build
// cache whether or not the build cache was what the operator was after, and the
// two trees differ in what losing them costs: a `go-build*` directory left by a
// killed build is refuse, while the build cache is the thing every later build
// pays for. "Sweep the go-tmp tree and leave the cache alone" was not
// expressible.
//
// WHY IT IS A FUNCTION HERE AND NOT A LOOP IN cmd/ (the constraint the epic
// states). The declared set is this package's table, so the mapping from a NAME
// to ROWS belongs beside the table. A `for _, tree := range scratch.DeclaredTrees()
// { if tree.Name == want … }` written in the CLI would be a second, un-syncable
// answer to "what does this selector mean", and the CLI cannot import the future
// name of a tree it has not heard of. One resolver, one place, and the CLI only
// decides how to render its ERROR (Rule 13/14).
//
// THE VALID-NAME LIST IS DERIVED, NEVER SPELLED (Rule 13: a constant defined but
// bypassed is a defect; Rule 14: one definition). DeclaredTreeNames walks
// DeclaredTrees() itself, so a row added to the table — including the
// per-worktree-root rows whose names are built at runtime
// (`build-cache/<root>`, `build-cache/shards`, …) — is selectable and listed the
// moment it exists, and a row renamed cannot leave a stale name selectable.
// Nothing here enumerates names a second time.
//
// WHAT AN EMPTY SELECTOR MEANS, AND WHY THE CLI REFUSES AN EXPLICIT EMPTY ONE.
// An ABSENT selector (`""`) means every declared tree — that is the arm's
// behaviour today, preserved exactly. An EXPLICITLY GIVEN empty selector
// (`--tree=`) is refused at the CLI rather than read as "every tree": widening a
// bounded pass to the whole set because of a truncated argument is the same
// footgun as the unscoped stale window (see devdoc/scratch-gc-declared-trees.md),
// and fail-closed costs one line.
package scratch

import (
	"errors"
	"fmt"
	"strings"
)

const (
	// DeclaredTreeFlagName is the tree selector's flag NAME without its dashes.
	// It is spelled once and referenced by every face that accepts the
	// selector (`trees --tree NAME` and the family census's own `--tree NAME`),
	// so the two arms cannot drift into two spellings of one option (Rule 14).
	DeclaredTreeFlagName = "tree"
	// DeclaredTreeFlagSpelling is the spelling a caller types: the flag name
	// with its dashes, DERIVED from DeclaredTreeFlagName rather than re-typed
	// (Rule 1/13 — a second literal is what the mirror gates exist to catch).
	DeclaredTreeFlagSpelling = "--" + DeclaredTreeFlagName

	// DeclaredTreeSelectorAll is the selector value that asks for every declared
	// tree by NAME. It exists so a script can be explicit about the full set
	// without spelling an absence: `--tree all` is the same pass as no selector
	// at all, and saying which of the two a script meant is the difference
	// between an intention and an omission.
	DeclaredTreeSelectorAll = "all"
)

// ErrUnknownDeclaredTreeName reports a selector that names no row of the
// declared set. It is a NAMED error so the caller can render its own refusal
// (the CLI prints the valid names; a test asserts the identity), and it WRAPS
// the offending name so a log line carries it.
//
// A refusal rather than an empty result on purpose (Rule 15): a selector that
// matched nothing must never render like a selector whose tree held nothing,
// because the two are the difference between "I mistyped the tree" and "the tree
// I asked for is empty" — and the sweep would report a clean, sweeping nothing.
var ErrUnknownDeclaredTreeName = errors.New("no declared tree has that name")

// DeclaredTreeNames returns every name the selector accepts, in the table's own
// order: it WALKS DeclaredTrees(), so the list cannot fall behind the table.
//
// Duplicates are collapsed but impossible in practice — a name is the identity
// the sweep's log line is attributed by, and two rows sharing one would make
// that line unattributable (which is why BuildCacheTrees names the second root's
// row after the root).
func DeclaredTreeNames() []string {
	trees := DeclaredTrees()
	out := make([]string, 0, len(trees))
	seen := make(map[string]bool, len(trees))
	for _, tree := range trees {
		if seen[tree.Name] {
			continue
		}
		seen[tree.Name] = true
		out = append(out, tree.Name)
	}
	return out
}

// DeclaredTreesForName resolves a selector to the rows it names:
//
//	""                        → every declared tree (today's behaviour, preserved)
//	DeclaredTreeSelectorAll   → every declared tree, asked for by name
//	a tree's own Name         → exactly that row
//	anything else             → ErrUnknownDeclaredTreeName, wrapping the name
//
// The per-root rows contribute MORE THAN ONE ROW under distinct names
// (`build-cache`, `build-cache/shards`, `build-cache/suite-sandbox`,
// `build-cache/<root>`), so the result is a SLICE even when one name was given:
// the caller must not have to know which rows are one-per-host and which are
// one-per-worktree-root.
//
// The returned slice is a copy of the matching rows taken from a fresh
// DeclaredTrees() call, so a caller may hold it while another tree's root is
// resolved without the table moving underneath (DeclaredTrees rebuilds every
// row, closures included, on each call).
func DeclaredTreesForName(selector string) ([]DeclaredTree, error) {
	if selector == "" || selector == DeclaredTreeSelectorAll {
		return DeclaredTrees(), nil
	}
	want := strings.TrimSpace(selector)
	var out []DeclaredTree
	for _, tree := range DeclaredTrees() {
		if tree.Name == want {
			out = append(out, tree)
		}
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("%w: %q (valid names: %s)", ErrUnknownDeclaredTreeName, selector, strings.Join(DeclaredTreeNames(), ", "))
	}
	return out, nil
}
