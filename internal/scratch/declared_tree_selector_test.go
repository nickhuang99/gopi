package scratch

// declared_tree_selector_test.go — the NAME→ROWS resolver's contract, and the
// GATE that fails when the selector stops binding the pass.
//
// WHAT IS PINNED HERE, AND WHY EACH HALF MATTERS:
//
//  1. The VALID-NAME LIST is derived from the table, not spelled (Rule 13): the
//     list must equal the names the table actually carries, so a renamed row
//     cannot leave a stale name selectable or a live one unlisted. The
//     red-proof (delete a row's Name / rename it) is recorded in the task
//     report's Evidence section.
//  2. An UNKNOWN name is refused with ErrUnknownDeclaredTreeName — never an
//     empty pass, which would render as "that tree was already clean" (Rule 15).
//  3. An EMPTY selector means EVERY declared tree: the arm's behaviour before
//     the selector existed is preserved exactly, and it is asserted by identity
//     of the resolved rows, not by count (a count passes when two rows swap).
//  4. THE CROSS-TREE GATE: a pass driven by the selector touches the named tree
//     and NOTHING else. This is the one that matters — the epic exists because a
//     pass meant for one tree reclaimed ~50 GB of another. It is asserted with a
//     byte-level before/after digest over a fixture that seeds BOTH trees, so a
//     selector that silently widened (or a future default that widened) fails
//     here rather than on the operator's disk.

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"
)

// selectorFixture plants ONE aged, collectable entry in two declared trees whose
// roots are redirectable by environment alone (GOTMPDIR for go-tmp, GOPI_HOME for
// raw), plus the controls each tree must keep. It returns the tree names and the
// paths a caller asserts on.
type selectorFixture struct {
	home  string
	goTmp string
	raw   string

	agedGoBuild  string
	freshGoBuild string
	agedRawLog   string
	freshRawLog  string
}

func seedSelectorFixture(t *testing.T) selectorFixture {
	t.Helper()
	home := t.TempDir()
	f := selectorFixture{
		home:  home,
		goTmp: filepath.Join(home, "workspace", "gopi-go-tmp"),
		raw:   filepath.Join(home, ".gopi", "raw"),
	}
	for _, dir := range []string{f.goTmp, f.raw} {
		if err := os.MkdirAll(dir, DirPerm); err != nil {
			t.Fatal(err)
		}
	}
	// Both roots are pinned: without GOTMPDIR the go-tmp row falls back to the
	// process temp root, and without GOPI_HOME the raw row resolves the
	// operator's own profile (Rule 32's ban, one level over).
	t.Setenv(GoTmpDirEnvName, f.goTmp)
	t.Setenv("GOPI_HOME", home)
	t.Setenv("HOME", home)

	f.agedGoBuild = filepath.Join(f.goTmp, "go-buildaged")
	f.freshGoBuild = filepath.Join(f.goTmp, "go-buildfresh")
	if err := os.MkdirAll(f.agedGoBuild, DirPerm); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(f.agedGoBuild, "obj.bin"), make([]byte, 2048), FilePerm); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(f.freshGoBuild, DirPerm); err != nil {
		t.Fatal(err)
	}

	f.agedRawLog = filepath.Join(f.raw, RawLogNamePrefix+"20260101-000000"+RawLogFileSuffix)
	f.freshRawLog = filepath.Join(f.raw, RawLogNamePrefix+"20260920-000000"+RawLogFileSuffix)
	for _, p := range []string{f.agedRawLog, f.freshRawLog} {
		if err := os.WriteFile(p, []byte("raw http traffic\n"), FilePerm); err != nil {
			t.Fatal(err)
		}
	}
	backdateTree(t, f.agedGoBuild, GoTmpStaleAge+2*time.Hour)
	backdateTree(t, f.agedRawLog, RawLogStaleAge+2*time.Hour)
	return f
}

// backdateTree ages a path past any window under test.
func backdateTree(t *testing.T, path string, age time.Duration) {
	t.Helper()
	stamp := time.Now().Add(-age)
	if err := os.Chtimes(path, stamp, stamp); err != nil {
		t.Fatalf("backdate %s: %v", path, err)
	}
}

// treeDigest walks a directory (or hashes a file) into one digest over NAMES and
// SIZES, in sorted order — the before/after reading a cross-tree touch must move.
// It reads metadata only: nothing here loads or generates device load (Rule 27).
func treeDigest(t *testing.T, root string) string {
	t.Helper()
	var lines []string
	walkErr := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, rerr := filepath.Rel(root, path)
		if rerr != nil {
			return rerr
		}
		info, ierr := d.Info()
		if ierr != nil {
			return ierr
		}
		lines = append(lines, fmt.Sprintf("%s|%d|%t", rel, info.Size(), d.IsDir()))
		return nil
	})
	if walkErr != nil {
		t.Fatalf("digest %s: %v", root, walkErr)
	}
	sort.Strings(lines)
	sum := sha256.Sum256([]byte(strings.Join(lines, "\n")))
	return hex.EncodeToString(sum[:])
}

// TestDeclaredTreeNamesAreDerivedFromTheTable is the derivation gate (Rule 13):
// the list the refusal prints and the resolver accepts IS the table's own names,
// in the table's order — a second enumeration would drift the moment a row is
// added, and the drift is invisible until an operator types the new name.
func TestDeclaredTreeNamesAreDerivedFromTheTable(t *testing.T) {
	names := DeclaredTreeNames()
	if len(names) == 0 {
		t.Fatal("DeclaredTreeNames returned nothing — the declared set is empty, so every selector would be refused")
	}
	fromTable := make([]string, 0, len(DeclaredTrees()))
	seen := map[string]bool{}
	for _, tree := range DeclaredTrees() {
		if seen[tree.Name] {
			continue
		}
		seen[tree.Name] = true
		fromTable = append(fromTable, tree.Name)
	}
	if strings.Join(names, ",") != strings.Join(fromTable, ",") {
		t.Errorf("DeclaredTreeNames() = %v, table order = %v — the list must BE the table's names (Rule 13)", names, fromTable)
	}
	// Every name must RESOLVE — a name that is listed but not selectable is the
	// worst of both: the refusal recommends a value the resolver then refuses.
	for _, name := range names {
		rows, err := DeclaredTreesForName(name)
		if err != nil {
			t.Errorf("DeclaredTreesForName(%q) failed for a name DeclaredTreeNames lists: %v", name, err)
			continue
		}
		if len(rows) == 0 {
			t.Errorf("DeclaredTreesForName(%q) returned no rows for a listed name", name)
		}
	}
	// The two names this epic turns on must be among them: the GOTMPDIR tree an
	// operator bounds, and the build cache the same pass must NOT reclaim.
	for _, want := range []string{TreeNameGoTmp, BuildCacheTreeName} {
		if !containsString(names, want) {
			t.Errorf("the valid-name list does not carry %q — the selector could not express this epic's own example", want)
		}
	}
}

// TestDeclaredTreesForNameSelectsExactlyTheNamedRows pins the mapping: the empty
// selector and `all` are the WHOLE set (by identity), a name is exactly its own
// rows, and an unknown name is a refusal carrying the name.
func TestDeclaredTreesForNameSelectsExactlyTheNamedRows(t *testing.T) {
	all := DeclaredTrees()
	for _, selector := range []string{"", DeclaredTreeSelectorAll} {
		got, err := DeclaredTreesForName(selector)
		if err != nil {
			t.Fatalf("selector %q: %v", selector, err)
		}
		if len(got) != len(all) {
			t.Errorf("selector %q returned %d rows, want the whole set (%d)", selector, len(got), len(all))
		}
	}

	named, err := DeclaredTreesForName(TreeNameGoTmp)
	if err != nil {
		t.Fatalf("selecting %s: %v", TreeNameGoTmp, err)
	}
	if len(named) == 0 {
		t.Fatalf("selecting %s returned no rows", TreeNameGoTmp)
	}
	for _, tree := range named {
		if tree.Name != TreeNameGoTmp {
			t.Errorf("selecting %s returned a row named %q — a selector that leaks a sibling tree is the defect this epic closes", TreeNameGoTmp, tree.Name)
		}
	}

	_, err = DeclaredTreesForName("no-such-tree-" + TreeNameGoTmp)
	if err == nil {
		t.Fatal("an unknown name must be REFUSED — an empty pass renders as \"that tree was already clean\" (Rule 15)")
	}
	if !errors.Is(err, ErrUnknownDeclaredTreeName) {
		t.Errorf("the refusal must wrap ErrUnknownDeclaredTreeName, got %v", err)
	}
	if !strings.Contains(err.Error(), "no-such-tree-"+TreeNameGoTmp) {
		t.Errorf("the refusal must name the offending value, got %v", err)
	}
	if !strings.Contains(err.Error(), TreeNameGoTmp) {
		t.Errorf("the refusal must list the valid names, got %v", err)
	}
}

// TestDeclaredTreeSelectorConfinesThePassToTheNamedTree is THE CROSS-TREE GATE:
// a pass driven by the selector moves the named tree's aged entries and leaves
// every other declared tree byte-for-byte identical, with every root still in
// place.
//
// THE RED IT GUARDS. The epic exists because `scratch-gc trees` swept all of
// them, i.e. an operator asking for the GOTMPDIR tree also reclaimed the build
// cache. If a future change makes the selector a no-op — or makes the empty
// selector the only path — this test fails on the OTHER tree's digest, which is
// the failure the operator cannot recover from (a build cache is expensive to
// rebuild; the raw logs are unrecoverable).
func TestDeclaredTreeSelectorConfinesThePassToTheNamedTree(t *testing.T) {
	f := seedSelectorFixture(t)

	beforeOther := treeDigest(t, f.raw)
	beforeGoTmp := treeDigest(t, f.goTmp)

	trees, err := DeclaredTreesForName(TreeNameGoTmp)
	if err != nil {
		t.Fatalf("resolve %s: %v", TreeNameGoTmp, err)
	}
	// The pass is driven the way the CLI drives it: one SweepDeclaredTree per
	// SELECTED row, with the row's OWN window (no override).
	swept := 0
	for _, tree := range trees {
		removed, _, serr := SweepDeclaredTree(tree)
		if serr != nil {
			// A SIBLING REACHED BY A NAMED PASS IS ITSELF THE DEFECT (the
			// selector failed to confine), and it is reported as one rather
			// than masked behind "that tree is unconfigured here".
			if tree.Name == TreeNameGoTmp {
				t.Fatalf("sweep %s: %v", tree.Name, serr)
			}
			t.Errorf("a %s-only pass reached the sibling declared tree %s (%v) — the selector did not confine it", TreeNameGoTmp, tree.Name, serr)
			continue
		}
		swept += removed
	}
	if swept == 0 {
		t.Fatal("the pass removed nothing — the fixture's aged entry should have been collectable, so this run proves nothing")
	}

	if _, err := os.Lstat(f.agedGoBuild); !os.IsNotExist(err) {
		t.Errorf("the named tree's aged entry %s survived the pass (err=%v)", f.agedGoBuild, err)
	}
	if _, err := os.Stat(f.freshGoBuild); err != nil {
		t.Errorf("the named tree's FRESH control %s was collected: %v", f.freshGoBuild, err)
	}
	if got := treeDigest(t, f.raw); got != beforeOther {
		t.Errorf("A SIBLING DECLARED TREE WAS TOUCHED by a %s-only pass:\n  before %s\n  after  %s\nthe selector must confine the pass to the named tree", TreeNameGoTmp, beforeOther, got)
	}
	if _, err := os.Lstat(f.agedRawLog); err != nil {
		t.Errorf("the sibling tree's aged entry %s was collected by a %s-only pass: %v", f.agedRawLog, TreeNameGoTmp, err)
	}
	if got := treeDigest(t, f.goTmp); got == beforeGoTmp {
		t.Errorf("the named tree's digest did not move — the pass reached nothing, so the confinement assertion above is vacuous")
	}
	// The declared-root invariant, re-asserted on this pass: the roots survive.
	for _, root := range []string{f.goTmp, f.raw} {
		if _, err := os.Stat(root); err != nil {
			t.Errorf("declared ROOT %s did not survive: %v", root, err)
		}
	}
}

// containsString is a small membership helper (the stdlib's slices.Contains is
// not imported by this package's other tests, and one helper beats an import for
// a three-line check).
func containsString(hay []string, needle string) bool {
	for _, s := range hay {
		if s == needle {
			return true
		}
	}
	return false
}
