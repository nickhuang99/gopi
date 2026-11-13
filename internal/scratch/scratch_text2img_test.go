// scratch_text2img_test.go — the text2img class tree's own claims, each of which
// the fix would be worthless without (task 01a1b3cf; the capture class's sibling
// suite scratch_captures_test.go states the same three, for the same reasons):
//
//  1. the WRITER's destination is inside the class directory and its NAME is one
//     the reclaim predicate accepts — and that holds for EVERY PAGE of a
//     multi-page render, which is the arm a single-page predicate would silently
//     leave behind;
//  2. the PREDICATE refuses a hand-placed file in the same directory (the tree is
//     on the operator's filesystem; a `diagram.png` dropped there is not ours);
//  3. the SWEEP collects an aged render and keeps a fresh one — driven through
//     the REAL declared row, not a copy of its logic.
package scratch

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

// TestText2ImgDumpNameTakesOnlyTheWritersNames pins the namespace: what the
// writer produces is collected, what a human drops there is not.
func TestText2ImgDumpNameTakesOnlyTheWritersNames(t *testing.T) {
	ours := []string{
		"text2img-1794016903.dump.png",    // page 1 / single page
		"text2img-1794016903-2.dump.png",  // page 2
		"text2img-1794016903-12.dump.png", // page 12 (no single-digit assumption)
		"text2img-1794016903-1.dump.png",  // explicit page 1 of a multi-page render
	}
	for _, n := range ours {
		if !IsText2ImgDumpName(n) {
			t.Errorf("%q must be a text2img name — the writer produces it, so a predicate that refused it would leave the PNG forever", n)
		}
	}
	theirs := []string{
		"diagram.png",                     // an operator's own file in the same directory
		"probe.png",                       // the PRE-FIX spelling (beside the source): not ours to bulk-collect
		"text2img-1794016903.png",         // the suffix is the safety property; without it the name is not this writer's
		"text2img.dump.png",               // no stamp
		"1794016903.dump.png",             // no family prefix
		"text2img-1794016903.dump.json",   /* wrong family suffix */
		"text2img-1794016903-0.dump.png",  // page 0 does not exist
		"text2img-1794016903-02.dump.png", /* a zero-padded page is not the shape Text2ImgDumpName mints */
	}
	for _, n := range theirs {
		if IsText2ImgDumpName(n) {
			t.Errorf("%q must NOT be a text2img candidate — this tree sits on the operator's filesystem and must never collect what it did not write", n)
		}
	}
}

// TestText2ImgDumpNameMatchesTheWriterByConstruction is Rule 14's claim as a
// test: the name the writer MINTS is the name the collector ACCEPTS, for every
// page, with the page number BEFORE the class suffix.
func TestText2ImgDumpNameMatchesTheWriterByConstruction(t *testing.T) {
	const stamp = int64(1794016903)
	stem := Text2ImgDumpStem(stamp)
	for page := 1; page <= 12; page++ {
		name := Text2ImgDumpName(stamp, page)
		if !IsText2ImgDumpName(name) {
			t.Fatalf("page %d name %q is refused by the collector — the writer and the reclaimer disagree", page, name)
		}
		if !strings.HasSuffix(name, Text2ImgDumpFileSuffix) {
			t.Fatalf("page %d name %q does not end in %q — the page number must sit BEFORE the class suffix", page, name, Text2ImgDumpFileSuffix)
		}
		want := stem + Text2ImgDumpFileSuffix
		if page > 1 {
			want = stem + "-" + strconv.Itoa(page) + Text2ImgDumpFileSuffix
		}
		if name != want {
			t.Fatalf("page %d name = %q, want %q", page, name, want)
		}
	}
}

// TestText2ImgDumpFileLandsInTheDeclaredTree pins the writer's destination and
// that two renders inside one second do not collide.
func TestText2ImgDumpFileLandsInTheDeclaredTree(t *testing.T) {
	home := t.TempDir()
	t.Setenv(HomeDirEnvName, home)

	root, err := Text2ImgDumpRoot()
	if err != nil {
		t.Fatalf("Text2ImgDumpRoot: %v", err)
	}
	if root == "" {
		t.Fatal("Text2ImgDumpRoot answered an empty path — the writer has nowhere to write")
	}
	if !strings.HasPrefix(root, home) {
		t.Fatalf("Text2ImgDumpRoot %q is outside the pinned scratch home %q", root, home)
	}

	first, err := Text2ImgDumpFile()
	if err != nil {
		t.Fatalf("Text2ImgDumpFile: %v", err)
	}
	if !filepath.IsAbs(first) {
		t.Fatalf("Text2ImgDumpFile answered a RELATIVE path %q — a relative path is resolved against the caller's cwd, which is the defect this prevents", first)
	}
	if filepath.Dir(first) != root {
		t.Fatalf("Text2ImgDumpFile landed at %q, not in the class directory %q", first, root)
	}
	if !IsText2ImgDumpName(filepath.Base(first)) {
		t.Fatalf("the writer's own name %q is refused by the collector — the file would never be reclaimed", filepath.Base(first))
	}
	// The writer never creates the file (the renderer does), so create it to drive
	// the collision arm.
	if err := os.WriteFile(first, []byte("png"), 0o644); err != nil {
		t.Fatal(err)
	}
	second, err := Text2ImgDumpFile()
	if err != nil {
		t.Fatalf("second Text2ImgDumpFile: %v", err)
	}
	if second == first {
		t.Fatalf("two renders in the same second resolved to the SAME path %q — the second would overwrite the first", first)
	}
	if !IsText2ImgDumpName(filepath.Base(second)) {
		t.Fatalf("the collision form %q is refused by the collector", filepath.Base(second))
	}
}

// TestText2ImgTreeSweepsAgedAndKeepsFresh drives the REAL declared row: an aged
// render in the class directory is collected, a fresh one survives, a foreign
// file survives, and the class DIRECTORY itself survives at any age.
func TestText2ImgTreeSweepsAgedAndKeepsFresh(t *testing.T) {
	home := t.TempDir()
	t.Setenv(HomeDirEnvName, home)

	tree, ok := treeByName(Text2ImgTreeName)
	if !ok {
		t.Fatalf("no declared tree named %q — the default destination would have no collector", Text2ImgTreeName)
	}
	root, err := tree.Resolve()
	if err != nil {
		t.Fatalf("resolving %q: %v", tree.Name, err)
	}
	if err := os.MkdirAll(root, DirPerm); err != nil {
		t.Fatal(err)
	}
	aged := filepath.Join(root, Text2ImgDumpName(1700000000, 1))
	agedPage2 := filepath.Join(root, Text2ImgDumpName(1700000000, 2))
	fresh := filepath.Join(root, Text2ImgDumpName(1794016903, 1))
	foreign := filepath.Join(root, "diagram.png")
	for _, p := range []string{aged, agedPage2, fresh, foreign} {
		if err := os.WriteFile(p, []byte("png"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	old := time.Now().Add(-(Text2ImgStaleAge + 24*time.Hour))
	for _, p := range []string{aged, agedPage2} {
		if err := os.Chtimes(p, old, old); err != nil {
			t.Fatal(err)
		}
	}

	removed, _, err := SweepDeclaredTree(tree)
	if err != nil {
		t.Fatalf("SweepDeclaredTree(%s): %v", tree.Name, err)
	}
	// BOTH pages of the aged render go: a multi-page render is one artifact, and
	// collecting page 1 while keeping page 2 would be the shape a predicate
	// written for a single-page name produces.
	if removed != 2 {
		t.Fatalf("sweep removed %d entries, want exactly the aged render's 2 pages", removed)
	}
	for _, p := range []string{aged, agedPage2} {
		if _, serr := os.Stat(p); !os.IsNotExist(serr) {
			t.Errorf("the aged page %s survived the sweep", p)
		}
	}
	if _, serr := os.Stat(fresh); serr != nil {
		t.Errorf("the FRESH render %s was collected — the window is not being applied: %v", fresh, serr)
	}
	if _, serr := os.Stat(foreign); serr != nil {
		t.Errorf("the foreign file %s was collected — the namespace predicate is not being applied: %v", foreign, serr)
	}
	if _, serr := os.Stat(root); serr != nil {
		t.Errorf("the class directory %s was removed — a declared root is never a candidate: %v", root, serr)
	}
}
