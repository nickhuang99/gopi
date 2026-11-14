package tools

// header_hook_test.go — the WRITE PATH'S HEADER HOOK, pinned at zero cost.
//
// WHAT THIS REPLACED. The write path used to run, after every landing, an
// external classifier: `python3 tools/docclass/classify.py --only <path>`
// followed by `banner.py --only`, which wrote a row into the shared
// devdoc/DOC-INDEX.tsv. Measured on this host: 14.76 s for ONE path (write-free
// `--dry-run --only`, i.e. the cheapest possible invocation), and the reported
// edit path cost was ~29 s per path. That number is why the hook had its own
// timeout, its own sentinel and a substitutable seam: the machinery existed to
// survive an expensive hook.
//
// WHAT IT IS NOW. A file's class is its own first line, so the hook is a read of
// the bytes the caller already handed us plus a parse against a closed
// vocabulary. No process, no interpreter, no second artifact. The assertions
// below are therefore about BEHAVIOUR (what the bytes look like afterwards) and
// about COST (that nothing is spawned), and the cost arm is a hard bound rather
// than a benchmark: a subprocess launch cannot fit in it.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/nickhuang99/gopi/internal/devdoc"
	"github.com/nickhuang99/gopi/internal/testutil"
)

// headerHookCostBound is the ceiling for one corpus write's header handling. It
// is deliberately far above the true cost (a few microseconds: one string
// prefix test plus, on a missing header, one 4 KB read) and far BELOW what a
// `python3` launch costs even fully warm — measured 14.76 s for classify.py on
// this host. A bound, not a benchmark: it fails when a process comes back.
const headerHookCostBound = 250 * time.Millisecond

// headerFixtureRoot builds a throwaway MODULE root (go.mod + devdoc/) so the
// corpus locator — which walks up to the nearest go.mod — sees a real corpus.
// It lives off the `/` partition through the shared device-scratch scaffold
// (Rule 22/32), never in the source tree and never in t.TempDir().
func headerFixtureRoot(t *testing.T) string {
	t.Helper()
	root := testutil.TestDeviceScratchDir(t, "header-hook")
	if err := os.WriteFile(filepath.Join(root, "go.mod"), []byte("module example.test/fixture\n"), 0o644); err != nil {
		t.Fatalf("write go.mod: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(root, devdoc.Root), 0o755); err != nil {
		t.Fatalf("mkdir corpus: %v", err)
	}
	return root
}

// TestWriteGivesACorpusDocItsClassHeaderAndSpawnsNothing is the write half: a
// body landing with no declaration is given one (the fresh class), the author's
// text survives intact, and the whole thing costs no subprocess.
func TestWriteGivesACorpusDocItsClassHeaderAndSpawnsNothing(t *testing.T) {
	root := headerFixtureRoot(t)
	path := filepath.Join(root, devdoc.Root, "fresh-record.md")
	body := "# a run's record\n\nwhat happened, in order.\n"

	start := time.Now()
	if err := Write(path, body); err != nil {
		t.Fatalf("Write: %v", err)
	}
	elapsed := time.Since(start)

	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read back: %v", err)
	}
	want := devdoc.Banner(devdocFreshClass) + "\n" + body
	if string(got) != want {
		t.Fatalf("written content mismatch\n got: %q\nwant: %q", string(got), want)
	}
	if class, cerr := devdoc.HeaderClass(firstLineOf(string(got))); cerr != nil || class != devdocFreshClass {
		t.Fatalf("the landed file's header does not parse as %q: class=%q err=%v", devdocFreshClass, class, cerr)
	}
	if elapsed > headerHookCostBound {
		t.Fatalf("classifying one corpus write took %s, over the %s bound — a subprocess has come back into the write path",
			elapsed, headerHookCostBound)
	}
}

// TestWriteNeverOverridesADeclaredClass is the ownership rule, now a property of
// the data: the header IS the source of truth, so a document that already says
// `normative` is left exactly as it is.
func TestWriteNeverOverridesADeclaredClass(t *testing.T) {
	root := headerFixtureRoot(t)
	path := filepath.Join(root, devdoc.Root, "standing-claim.md")
	body := devdoc.Banner(devdoc.ClassNormative) + "\n# a rule\n\never edited elsewhere.\n"
	if err := Write(path, body); err != nil {
		t.Fatalf("Write: %v", err)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read back: %v", err)
	}
	if string(got) != body {
		t.Fatalf("a declared class was re-decided:\n got: %q\nwant: %q", string(got), body)
	}
}

// TestWriteLeavesNonCorpusPathsAlone is the SCOPE arm: the hook is a corpus rule,
// not a markdown rule. A .md outside devdoc/ (and a non-markdown record inside
// it) keeps exactly the bytes the caller gave.
func TestWriteLeavesNonCorpusPathsAlone(t *testing.T) {
	root := headerFixtureRoot(t)
	for _, rel := range []string{"elsewhere/notes.md", devdoc.Root + "/capture.txt"} {
		path := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", filepath.Dir(path), err)
		}
		body := "# no declaration\n"
		if err := Write(path, body); err != nil {
			t.Fatalf("Write %s: %v", rel, err)
		}
		got, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read back %s: %v", rel, err)
		}
		if string(got) != body {
			t.Fatalf("%s is out of the corpus but was rewritten: %q", rel, string(got))
		}
	}
}

// TestEditKeepsTheCorpusHeader is the EDIT half, and it is the arm that a
// body-only replacement would break: edit lands only the replaced bytes, so a
// document whose header is outside the match window must get it back — with its
// OWN class, never the fresh one (an edit may not reclassify a document).
func TestEditKeepsTheCorpusHeader(t *testing.T) {
	root := headerFixtureRoot(t)
	path := filepath.Join(root, devdoc.Root, "standing-claim.md")
	original := devdoc.Banner(devdoc.ClassNormative) + "\n# a rule\n\nold sentence.\n"
	if err := os.WriteFile(path, []byte(original), 0o644); err != nil {
		t.Fatalf("seed: %v", err)
	}
	start := time.Now()
	if err := Edit(path, "old sentence.", "new sentence.", false); err != nil {
		t.Fatalf("Edit: %v", err)
	}
	elapsed := time.Since(start)

	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read back: %v", err)
	}
	if !strings.HasPrefix(string(got), devdoc.Banner(devdoc.ClassNormative)) {
		t.Fatalf("the edit dropped or changed the document's class header; first line: %q", firstLineOf(string(got)))
	}
	if !strings.Contains(string(got), "new sentence.") {
		t.Fatalf("the edit did not land: %q", string(got))
	}
	if elapsed > headerHookCostBound {
		t.Fatalf("an edit's header handling took %s, over the %s bound", elapsed, headerHookCostBound)
	}
}

// TestEditRestoresTheHeaderWhenTheReplacementDropsIt is the sharp case: the
// caller's oldStr CONTAINS the banner, so the replacement erases it. The class
// must come back from the file's own old bytes.
func TestEditRestoresTheHeaderWhenTheReplacementDropsIt(t *testing.T) {
	root := headerFixtureRoot(t)
	path := filepath.Join(root, devdoc.Root, "intent.md")
	original := devdoc.Banner(devdoc.ClassRoadmap) + "\n# intended work\n\nphase one.\n"
	if err := os.WriteFile(path, []byte(original), 0o644); err != nil {
		t.Fatalf("seed: %v", err)
	}
	// Replace the whole document — header included — with a headerless body.
	if err := Edit(path, original, "# intended work\n\nphase one and two.\n", false); err != nil {
		t.Fatalf("Edit: %v", err)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read back: %v", err)
	}
	if !strings.HasPrefix(string(got), devdoc.Banner(devdoc.ClassRoadmap)) {
		t.Fatalf("the class was not restored from the file's own bytes; got first line %q (want %q)",
			firstLineOf(string(got)), devdoc.Banner(devdoc.ClassRoadmap))
	}
	if class, cerr := devdoc.HeaderClass(firstLineOf(string(got))); cerr != nil || class != devdoc.ClassRoadmap {
		t.Fatalf("restored header does not parse as roadmap: class=%q err=%v", class, cerr)
	}
}
