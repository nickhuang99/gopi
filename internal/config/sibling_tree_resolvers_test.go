// sibling_tree_resolvers_test.go — the PROFILE'S SIBLING TREES are resolvable
// from the ONE place that owns the profile root.
//
// WHY THIS PIN LIVES HERE. Two registries describe the four (plus one, for the
// second dump knob) directories that live beside the sessions tree under the gopi
// profile: internal/sessionlayout's family census and internal/scratch's
// declared-tree row that bounds `dumps/`. NEITHER may import internal/config (the
// direction is config -> them), so each takes the profile root as a function
// value, and internal/config's init supplies it. A seam wired in ONE place and
// read by TWO registries is exactly the shape that rots silently: the day the
// wiring line is dropped, both registries fall back to their unwired sentinel and
// every sibling tree reports "not configured on this host" — a CLEAN-LOOKING
// answer for a tree that is sitting right there, which is the failure mode these
// assertions exist to refuse.
//
// It asserts the RESOLUTION, not the existence: a tree need not exist on a host
// (a fresh profile has no `archive/`), but the path the census would walk must be
// the profile's own child, under the profile root this package just resolved.
package config

import (
	"path/filepath"
	"testing"

	"github.com/nick/gopi/internal/scratch"
	"github.com/nick/gopi/internal/sessionlayout"
)

// TestProfileSiblingTreeResolversAreWired pins the seam: with GOPI_HOME set to a
// profile the test owns, every sibling tree resolves to `<profile>/<name>`, from
// BOTH registries, and neither falls back to its unwired sentinel.
func TestProfileSiblingTreeResolversAreWired(t *testing.T) {
	// GopiDirEnv is a GOPI_HOME, and GopiDir resolves the PROFILE DIR inside it
	// (ProfileDirName = ".gopi") — so the profile the sibling trees hang off is
	// GopiDir's own answer, not the directory the test set. Asking GopiDir is the
	// point: a test that spelled `.gopi` itself would be pinning its own
	// assumption about the profile layout instead of the resolver's.
	home := t.TempDir()
	t.Setenv(GopiDirEnv, home)

	root, err := GopiDir()
	if err != nil {
		t.Fatalf("GopiDir with %s=%s: %v", GopiDirEnv, home, err)
	}

	// 1) The family census's registry (internal/sessionlayout).
	trees := sessionlayout.SiblingTrees()
	if len(trees) != len(sessionlayout.SiblingTreeNames()) {
		t.Fatalf("SiblingTrees() returned %d trees but SiblingTreeNames() %d names — the enumeration and the registry disagree",
			len(trees), len(sessionlayout.SiblingTreeNames()))
	}
	if len(trees) == 0 {
		t.Fatal("no sibling tree is declared at all — the census face would have nothing to walk; this is the defect the test exists to refuse")
	}
	for _, tree := range trees {
		got, rerr := tree.Resolve()
		if rerr != nil {
			t.Fatalf("sibling tree %s did not resolve with %s set: %v — the seam internal/config wires is not wired (the census would report the tree as absent)",
				tree.Name, GopiDirEnv, rerr)
		}
		want := filepath.Join(root, tree.Name)
		if got != want {
			t.Errorf("sibling tree %s resolved to %s, want %s — the registry does not hang off the profile root this package owns", tree.Name, got, want)
		}
	}

	// 2) The declared-tree rows (internal/scratch) that BOUND the dump trees. They
	//    are the same resolution through the same seam; checked separately because
	//    they are wired by their own call and a one-line omission would leave one
	//    of them unwired while the other passes.
	for _, tc := range []struct {
		tree string
		leaf string
		root func() (string, error)
	}{
		{scratch.SiblingDumpsTreeName, scratch.SiblingDumpsDirName, scratch.SiblingDumpsRoot},
	} {
		got, rerr := tc.root()
		if rerr != nil {
			t.Fatalf("declared tree %s did not resolve with %s set: %v — the sweep would report it as unconfigured while its directory exists",
				tc.tree, GopiDirEnv, rerr)
		}
		want := filepath.Join(root, tc.leaf)
		if got != want {
			t.Errorf("declared tree %s resolved to %s, want %s", tc.tree, got, want)
		}
	}

	// 3) Every tree the registry binds a family to must be a tree this package can
	//    resolve AND a name internal/scratch really declared — the `bound:`
	//    decision is only worth the machinery behind it.
	if len(sessionlayout.SiblingBoundTreeNames()) == 0 {
		t.Error("no sibling family is `bound:` — the sibling trees have no enforcement left, only prose")
	}
}

// TestProfileSiblingTreesAreDistinctDirectories refuses the copy-paste slip the
// two dump trees invite: they share one implementation, and a single wrong
// argument would point both knobs' trees at the same directory — the census would
// then count one tree twice and report the other as empty forever.
func TestProfileSiblingTreesAreDistinctDirectories(t *testing.T) {
	profile := t.TempDir()
	t.Setenv(GopiDirEnv, profile)
	seen := map[string]string{}
	for _, tree := range sessionlayout.SiblingTrees() {
		root, err := tree.Resolve()
		if err != nil {
			t.Fatalf("sibling tree %s: %v", tree.Name, err)
		}
		if prev, dup := seen[root]; dup {
			t.Fatalf("sibling trees %s and %s resolve to the SAME directory %s — two census rows for one tree, and the other knob's directory uncensused", prev, tree.Name, root)
		}
		seen[root] = tree.Name
	}
}
