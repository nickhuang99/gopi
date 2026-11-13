package tools

// header_hook_retired_test.go — the two arms that keep the RETIRED generator
// pipeline from coming back.
//
// WHY THIS FILE EXISTS. The write path's class hook used to be an out-of-process
// python pipeline: `classify.py --only <path>` derived a manifest row from
// heuristics, then `banner.py --only` rewrote the document's line 1 FROM THAT
// ROW. Both directions of that arrangement were the same defect — the class was
// decided by a generator reading the manifest, never by the author, so a write
// could overwrite a declared `normative` with a derived `historical` and, on a
// hand-written document, consume the banner entirely.
//
// The Go hook (header_hook.go) removed the behaviour, and the generators were
// deleted (tools/docclass/, document-pipeline teardown slice 3/5). Behaviour and
// artifact are two different claims, so this file pins both:
//
//	① NO TRACKED GENERATOR — the deleted scripts stay deleted. A file that is
//	   back in the index is a file a reader can run again.
//	② NO WRITE PATH RE-DECIDES A CLASS — every class an author can declare
//	   survives BOTH entry points (write and edit) byte-for-byte. The existing
//	   arms pin `normative` through Write and the header through Edit; this arm
//	   walks the whole closed vocabulary through both, because the original
//	   defect was a FLIP (`normative` → `historical`), not an erasure, and a
//	   single-class arm cannot see the other three.
//
// Both arms are read-only over the real tree: no fixture is built, nothing is
// written, no subprocess runs (the cost bound in header_hook_test.go is the
// third arm of the same invariant, and a `python3` launch cannot fit in it).

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nick/gopi/internal/devdoc"
)

// retiredDocclassDir is the generator directory that must not be tracked again.
// It is a test-side literal on purpose: the constant it used to live beside was
// deleted with the directory, and a test asserting "not tracked" cannot import
// its subject.
const retiredDocclassDir = "tools/docclass"

// retiredGeneratorNames are the four scripts the teardown removed, by name — a
// directory that reappears with a different member is still a reappearance.
var retiredGeneratorNames = []string{"classify.py", "banner.py", "classify_test.py", "arm6_census.py"}

// moduleRootOfTest locates the module root this test binary was compiled from:
// walk up from the package's own working directory to the nearest go.mod. It is
// the REAL tree, not a fixture — the claim is about what this repository tracks.
func moduleRootOfTest(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd: %v", err)
	}
	for {
		if _, serr := os.Stat(filepath.Join(dir, "go.mod")); serr == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatalf("no go.mod above %q — cannot locate the module root", dir)
		}
		dir = parent
	}
}

// TestRetiredDocclassGeneratorsAreNotTracked is arm ①. It measures the INDEX
// (`git ls-files`), not the worktree: an untracked leftover is a local mess,
// while a TRACKED generator is one every clone inherits and every reader can
// run — which is the only form that can reproduce the overwrite inside a fresh
// tree.
func TestRetiredDocclassGeneratorsAreNotTracked(t *testing.T) {
	root := moduleRootOfTest(t)

	cmd := exec.Command("git", "ls-files", "--", retiredDocclassDir)
	cmd.Dir = root
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("git ls-files in %q: %v — the arm cannot report a pass it did not measure", root, err)
	}
	if tracked := strings.TrimSpace(string(out)); tracked != "" {
		t.Fatalf("%s is tracked again (a re-added generator can reproduce the class overwrite):\n%s",
			retiredDocclassDir, tracked)
	}

	// The worktree arm, so "not tracked" cannot be satisfied by a file that is
	// merely staged for deletion while still on disk and executable.
	for _, name := range retiredGeneratorNames {
		path := filepath.Join(root, retiredDocclassDir, name)
		if _, serr := os.Stat(path); serr == nil {
			t.Fatalf("%s is still on disk — a retired generator present in the tree can be run", path)
		}
	}
}

// TestEveryDeclaredClassSurvivesBothWritePaths is arm ②: the closed vocabulary
// through both entry points. A write or an edit that lands a document already
// declaring class C must leave the document declaring C — not the fresh class,
// not a derived one, not none.
func TestEveryDeclaredClassSurvivesBothWritePaths(t *testing.T) {
	// The vocabulary comes from the package that owns it (Rule 13): a class
	// added there is covered here without a second list to keep in step.
	for _, class := range devdoc.Classes {
		class := class
		t.Run(class, func(t *testing.T) {
			root := headerFixtureRoot(t)
			path := filepath.Join(root, devdoc.Root, "declared-"+class+".md")
			declared := devdoc.Banner(class) + "\n# a title\n\nfirst body.\n"

			// WRITE half: a create whose bytes already carry the declaration.
			if err := Write(path, declared); err != nil {
				t.Fatalf("Write: %v", err)
			}
			assertLineOne(t, path, devdoc.Banner(class), "Write re-decided the class")

			// EDIT half: the replacement lands INSIDE the body, so the header is
			// outside the match window and the hook has to carry it across.
			if err := Edit(path, "first body.", "second body.", false); err != nil {
				t.Fatalf("Edit: %v", err)
			}
			assertLineOne(t, path, devdoc.Banner(class), "Edit re-decided the class")
		})
	}
}

// assertLineOne reads the file back and requires line 1 to be exactly want.
func assertLineOne(t *testing.T, path, want, why string) {
	t.Helper()
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read back: %v", err)
	}
	line := strings.SplitN(string(got), "\n", 2)[0]
	if line != want {
		t.Fatalf("%s:\n got line 1: %q\nwant line 1: %q", why, line, want)
	}
}
