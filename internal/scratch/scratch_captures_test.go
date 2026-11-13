// scratch_captures_test.go — the capture class tree's own three claims, each of
// which the fix would be worthless without (task 01a1b3b1):
//
//  1. the WRITER's destination is inside the class directory and its NAME is one
//     the reclaim predicate accepts (a destination with no collector is not a
//     fix, it is the same leak in a different directory — the PA's own objection
//     to "just move it to the session dir", which has no collector at all);
//  2. the PREDICATE refuses a hand-placed file in the same directory (the tree is
//     on the operator's filesystem; a `diagram.png` an operator dropped there is
//     not ours to collect);
//  3. the SWEEP collects an aged capture and keeps a fresh one — driven through
//     the REAL declared row, not a copy of its logic.
package scratch

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestCaptureDumpNameTakesOnlyTheWritersNames pins the namespace: what the
// writer produces is collected, what a human drops there is not.
func TestCaptureDumpNameTakesOnlyTheWritersNames(t *testing.T) {
	ours := []string{
		"screen-1794016903.dump.png",
		"shot-1794016912.dump.png",
		"screen-1794016903-2.dump.png", // the same-second collision form
	}
	for _, n := range ours {
		if !IsCaptureDumpName(n) {
			t.Errorf("%q must be a capture name — the writer produces it, so a predicate that refused it would leave the file forever", n)
		}
	}
	theirs := []string{
		"diagram.png",                 // an operator's own file in the same directory
		"screen-1794016903.png",       // the PRE-FIX spelling: not ours to bulk-collect
		"img.dump.png",                // a suffix match with no stamp
		"1794016903.dump.png",         // no family prefix
		"screen.dump.png",             // no stamp
		"screen-1794016903.dump.json", /* wrong family entirely */
	}
	for _, n := range theirs {
		if IsCaptureDumpName(n) {
			t.Errorf("%q must NOT be a capture candidate — this tree sits on the operator's filesystem and must never collect what it did not write", n)
		}
	}
	// The WRITER and the COLLECTOR must agree by construction (Rule 14): the name
	// the writer mints is the name the predicate accepts. Asserted through the
	// real writer below rather than by re-spelling the shape here.
}

// TestCaptureDumpFileLandsInTheDeclaredTree pins the writer's destination and the
// same-second collision form.
func TestCaptureDumpFileLandsInTheDeclaredTree(t *testing.T) {
	home := t.TempDir()
	t.Setenv(HomeDirEnvName, home)

	root, err := CaptureDumpRoot()
	if err != nil {
		t.Fatalf("CaptureDumpRoot: %v", err)
	}
	if root == "" {
		t.Fatal("CaptureDumpRoot answered an empty path — the writer has nowhere to write")
	}
	if !strings.HasPrefix(root, home) {
		t.Fatalf("CaptureDumpRoot %q is outside the pinned scratch home %q", root, home)
	}

	first, err := CaptureDumpFile("screen-")
	if err != nil {
		t.Fatalf("CaptureDumpFile: %v", err)
	}
	if !filepath.IsAbs(first) {
		t.Fatalf("CaptureDumpFile answered a RELATIVE path %q — a relative path is resolved against the caller's cwd, which is the defect this exists to prevent", first)
	}
	if filepath.Dir(first) != root {
		t.Fatalf("CaptureDumpFile landed at %q, not in the class directory %q", first, root)
	}
	if !IsCaptureDumpName(filepath.Base(first)) {
		t.Fatalf("the writer's own name %q is refused by the collector — the file would never be reclaimed", filepath.Base(first))
	}
	// The writer never creates the file itself (the caller writes it), so make it
	// exist to drive the collision arm.
	if err := os.WriteFile(first, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	second, err := CaptureDumpFile("screen-")
	if err != nil {
		t.Fatalf("second CaptureDumpFile: %v", err)
	}
	if second == first {
		t.Fatalf("two captures in the same second resolved to the SAME path %q — the second would overwrite the first", first)
	}
	if !IsCaptureDumpName(filepath.Base(second)) {
		t.Fatalf("the collision form %q is refused by the collector", filepath.Base(second))
	}
}

// TestCaptureTreeSweepsAgedAndKeepsFresh drives the REAL declared row: an aged
// capture in the class directory is collected, a fresh one survives, a foreign
// file survives, and the class DIRECTORY itself survives at any age.
func TestCaptureTreeSweepsAgedAndKeepsFresh(t *testing.T) {
	home := t.TempDir()
	t.Setenv(HomeDirEnvName, home)

	tree, ok := treeByName(CaptureTreeName)
	if !ok {
		t.Fatalf("no declared tree named %q — the capture destination would have no collector", CaptureTreeName)
	}
	root, err := tree.Resolve()
	if err != nil {
		t.Fatalf("resolving %q: %v", tree.Name, err)
	}
	if err := os.MkdirAll(root, DirPerm); err != nil {
		t.Fatal(err)
	}
	aged := filepath.Join(root, "screen-1700000000.dump.png")
	fresh := filepath.Join(root, "screen-1794016903.dump.png")
	foreign := filepath.Join(root, "diagram.png")
	for _, p := range []string{aged, fresh, foreign} {
		if err := os.WriteFile(p, []byte("png"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	old := time.Now().Add(-(CaptureStaleAge + 24*time.Hour))
	if err := os.Chtimes(aged, old, old); err != nil {
		t.Fatal(err)
	}

	removed, _, err := SweepDeclaredTree(tree)
	if err != nil {
		t.Fatalf("SweepDeclaredTree(%s): %v", tree.Name, err)
	}
	if removed != 1 {
		t.Fatalf("sweep removed %d entries, want exactly the aged capture (1)", removed)
	}
	if _, serr := os.Stat(aged); !os.IsNotExist(serr) {
		t.Errorf("the aged capture %s survived the sweep", aged)
	}
	if _, serr := os.Stat(fresh); serr != nil {
		t.Errorf("the FRESH capture %s was collected — the window is not being applied: %v", fresh, serr)
	}
	if _, serr := os.Stat(foreign); serr != nil {
		t.Errorf("the foreign file %s was collected — the namespace predicate is not being applied: %v", foreign, serr)
	}
	if _, serr := os.Stat(root); serr != nil {
		t.Errorf("the class directory %s was removed — a declared root is never a candidate: %v", root, serr)
	}
}
