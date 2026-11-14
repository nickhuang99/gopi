package standardgate

// docclass_corpus_gate_test.go — Rule 36's corpus gate, the one PROJECT.md
// names: TestEveryCorpusDocDeclaresItsClassInItsHeader.
//
// WHAT WAS MISSING. Rule 36 named this gate for months and the repository did
// not contain it: `go test ./internal/standards/ -run
// TestEveryCorpusDocDeclaresItsClassInItsHeader` matched nothing, so it printed
// "no tests to run" and exited 0. A rule whose stated check is a vacuum reads
// green forever — the worst shape a gate can have, because the green is
// indistinguishable from a clean corpus. This file is the check the rule
// describes.
//
// WHAT IT DOES. It walks the corpus (`devdoc.Root`), takes the files
// `devdoc.IsProse` claims (markdown under the corpus — a fixture, a capture or
// a TSV owes no banner a reader would never meet), and parses each one's FIRST
// LINE with `devdoc.HeaderClass`. A failure is printed as `<path>: <reason>`,
// one line per file, and the run also prints how many files it walked, how many
// it judged and how many carry each class — so "I scanned nothing" cannot pass
// as "the corpus is clean".
//
// WHAT IT DOES NOT DO. It never writes a header. The class is the author's
// statement of what the page is, and a gate that guessed one would be the
// filename heuristic Rule 36 exists to have removed. It is a READER, and the
// only repair it prescribes is the one line a human or the write/edit hook
// stamps (internal/tools/header_hook.go).
//
// HOW IT IS PROVEN. Reading a clean corpus and saying "clean" is the half that
// can be true by accident, so the proofs below run the SAME verdict function
// over fixture corpora — one defect per arm, each of the three classes the
// parser distinguishes (missing banner, malformed banner, illegal class value),
// plus the vacuity arm (an EMPTY corpus must be refused rather than passed).
// If the predicate stops naming a defect, the arm fails and the suite turns
// red; there is no path on which this file reports a green it did not measure.

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nickhuang99/gopi/internal/devdoc"
)

const (
	// docCorpusScanFloor is the non-vacuity floor of the REAL-TREE run: the
	// minimum number of prose files the walk must JUDGE before its green counts
	// as evidence (measured on THIS rev: 895 markdown files under devdoc/, 0
	// exceptions). It is an absolute count and not a ratio because there is no
	// second quantity to compare against — the scan's own report is the only
	// number there is. The floor is slack deliberately (≈20% below the measured
	// corpus) so that archiving documents does not need a change here, while a
	// walk that reads a wrong root, an empty directory or a filtered listing is
	// caught by name.
	//
	// THE FLOOR MOVED DOWN WITH THE CORPUS, and that is the point of it being a
	// floor rather than a target: the public export carries only the documents
	// that still make a standing claim (doc-class normative + roadmap) plus the
	// handful of historical records a code literal or a root document links to,
	// so the judged corpus fell from 2368 to 895 in ONE deliberate cut. A floor
	// left at the old value would not have measured anything — it would have
	// refused the corpus that exists, which is the opposite failure from the one
	// it was written to catch (a green that covered nothing). The floor still
	// sits far above zero and far above any single directory, so a broken walk
	// root, an empty devdoc/ or a filtered listing is still caught by name.
	docCorpusScanFloor = 700

	// docCorpusProvenance names who is speaking, so a failure attributed to this
	// gate is distinguishable from the other standards gates (Rule 1: the
	// spelling lives here, once).
	docCorpusProvenance = "Rule 36 corpus gate"
)

// corpusLineReader reads ONE file's first line, given its absolute path. It is
// the seam the proofs inject on: the real tree reads the disk, a fixture reads
// an in-memory map, and the gate itself cannot tell the difference.
type corpusLineReader func(absPath string) (string, error)

// docCorpusReport is what one scan measured. Walked and Judged are separate
// numbers on purpose: a corpus that only holds non-markdown records walks many
// files and judges none, and that is precisely the shape a silent green hides.
type docCorpusReport struct {
	Walked   int            // regular files visited under the corpus root
	Judged   int            // prose files the header rule applies to, and which were parsed
	Clean    int            // judged files whose line 1 names a legal class
	Classes  map[string]int // judged files per declared class
	Failures []string       // "<path>: <reason>", one per offending file
}

// Summary renders the scan's own accounting, so a reader of a green run knows
// what the green is worth.
func (r docCorpusReport) Summary() string {
	var b strings.Builder
	fmt.Fprintf(&b, "%s: walked %d file(s) under %s/, judged %d, clean %d", docCorpusProvenance, r.Walked, devdoc.Root, r.Judged, r.Clean)
	// The vocabulary, in its declared order, so a class that vanished from the
	// corpus is visible as a 0 rather than by absence.
	for _, c := range devdoc.Classes {
		fmt.Fprintf(&b, "; %s=%d", c, r.Classes[c])
	}
	return b.String()
}

// scanDocCorpus walks the corpus under absRoot and judges every prose file it
// finds. It reports rather than fails: the caller (a test) owns the verdict,
// which is what lets the proofs drive the same scan over a fixture.
func scanDocCorpus(absRoot string, read corpusLineReader) docCorpusReport {
	rep := docCorpusReport{Classes: make(map[string]int)}
	corpusRoot := filepath.Join(absRoot, devdoc.Root)
	walkErr := filepath.Walk(corpusRoot, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			rep.Failures = append(rep.Failures, fmt.Sprintf("%s: walk: %v", path, err))
			return nil
		}
		if info.IsDir() {
			return nil
		}
		rep.Walked++
		relToCorpus, relErr := filepath.Rel(corpusRoot, path)
		if relErr != nil {
			rep.Failures = append(rep.Failures, fmt.Sprintf("%s: relate to %s/: %v", path, devdoc.Root, relErr))
			return nil
		}
		rel := devdoc.Root + "/" + filepath.ToSlash(relToCorpus)
		if !devdoc.IsProse(rel) {
			// A capture, a fixture or the manifest: out of scope by a written
			// rule (devdoc.IsProse), so it owes no banner and is not judged.
			return nil
		}
		rep.Judged++
		line, readErr := read(path)
		if readErr != nil {
			rep.Failures = append(rep.Failures, fmt.Sprintf("%s: %v", rel, readErr))
			return nil
		}
		class, classErr := devdoc.HeaderClass(line)
		if classErr != nil {
			rep.Failures = append(rep.Failures, fmt.Sprintf("%s: %v", rel, classErr))
			return nil
		}
		rep.Clean++
		rep.Classes[class]++
		return nil
	})
	if walkErr != nil {
		rep.Failures = append(rep.Failures, fmt.Sprintf("%s/: walk: %v", devdoc.Root, walkErr))
	}
	return rep
}

// docCorpusVerdict turns a finished scan into the gate's failure sentences: one
// per offending file (so a reader repairs in one pass instead of one file per
// run), plus a refusal when the scan itself judged too little to be evidence.
//
// The vacuity arm is not politeness. The defect this file was written to repair
// IS a vacuum: a named gate that matched no test and therefore passed.
func docCorpusVerdict(rep docCorpusReport, floor int) []string {
	out := append([]string{}, rep.Failures...)
	if rep.Judged == 0 {
		return append(out, fmt.Sprintf("%s: judged 0 prose file(s) under %s/ — a scan of nothing is not a clean corpus (walked %d file(s); the root, the extension or the filter is wrong)",
			docCorpusProvenance, devdoc.Root, rep.Walked))
	}
	// floor 0 says "this scan is about SHAPE, not size": a fixture proves which
	// defects the predicate names, and cannot and should not pretend to be the
	// corpus. The real-tree run states the corpus floor and gets it enforced.
	if floor > 0 && rep.Judged < floor {
		return append(out, fmt.Sprintf("%s: judged %d prose file(s) under %s/, below the non-vacuity floor %d — the green would rest on a fragment of the corpus",
			docCorpusProvenance, rep.Judged, devdoc.Root, floor))
	}
	return out
}

// diskLineReader reads the first line of a file on disk. A read failure is its
// own error: "unreadable" must never be reported as "unclassified" (Rule 15).
func diskLineReader(absPath string) (string, error) {
	fh, err := os.Open(absPath)
	if err != nil {
		return "", fmt.Errorf("open: %w", err)
	}
	//stylegate:ignore deferred close of a read-only file whose read result is already returned
	defer func() { _ = fh.Close() }()
	line, err := bufio.NewReader(fh).ReadString('\n')
	if err != nil && err != io.EOF {
		return "", fmt.Errorf("read first line: %w", err)
	}
	return line, nil
}

// TestEveryCorpusDocDeclaresItsClassInItsHeader is Rule 36's gate: every
// markdown document under the corpus declares its truth class on line 1 of
// itself, and a document that does not is named here with its reason.
func TestEveryCorpusDocDeclaresItsClassInItsHeader(t *testing.T) {
	root := repoRoot(t)
	rep := scanDocCorpus(root, diskLineReader)
	t.Log(rep.Summary())
	for _, failure := range docCorpusVerdict(rep, docCorpusScanFloor) {
		t.Error(failure)
	}
}

// --- the proofs -----------------------------------------------------------
//
// Each arm writes a fixture corpus into a temporary directory and drives the
// REAL scan + verdict over it. The three arms below are the three shapes
// devdoc.HeaderClass distinguishes; the fourth is the vacuum.

// fixtureDoc is one file of a fixture corpus: its path under the corpus root
// and the exact bytes it holds.
type fixtureDoc struct {
	name string
	body string
}

// writeFixtureCorpus materialises docs under <tmp>/devdoc/ and returns the
// temporary root plus the reader the scan will use.
func writeFixtureCorpus(t *testing.T, docs []fixtureDoc) (string, corpusLineReader) {
	t.Helper()
	root := t.TempDir()
	// The corpus root exists even when the fixture holds no documents: the
	// vacuum arm is about a scan that judged nothing, not about a missing
	// directory (which is a different, louder failure — see scanDocCorpus).
	if err := os.MkdirAll(filepath.Join(root, devdoc.Root), 0o755); err != nil {
		t.Fatalf("mkdir corpus root: %v", err)
	}
	firstLines := make(map[string]string, len(docs))
	for _, d := range docs {
		abs := filepath.Join(root, devdoc.Root, filepath.FromSlash(d.name))
		if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
			t.Fatalf("mkdir for %s: %v", d.name, err)
		}
		if err := os.WriteFile(abs, []byte(d.body), 0o644); err != nil {
			t.Fatalf("write %s: %v", d.name, err)
		}
		firstLines[abs] = devdoc.FirstLine(abs)
	}
	return root, func(absPath string) (string, error) {
		line, ok := firstLines[absPath]
		if !ok {
			return "", fmt.Errorf("fixture has no such file (the walk found one the fixture did not create)")
		}
		return line, nil
	}
}

// requireNamed runs the scan+verdict over the fixture and asserts the gate names
// exactly one file — wantPath — with a reason carrying wantPhrase. Phrasing the
// assertion this way means a predicate that stopped distinguishing one of the
// three shapes cannot pass on the strength of the other two.
func requireNamed(t *testing.T, docs []fixtureDoc, wantPath, wantPhrase string) {
	t.Helper()
	root, read := writeFixtureCorpus(t, docs)
	rep := scanDocCorpus(root, read)
	verdict := docCorpusVerdict(rep, 0)
	if len(verdict) != 1 {
		t.Fatalf("the gate reported %d failure(s) for a fixture with ONE defect, want exactly 1:\n%s", len(verdict), strings.Join(verdict, "\n"))
	}
	if !strings.HasPrefix(verdict[0], wantPath+": ") {
		t.Fatalf("the gate did not name %s — it said:\n%s", wantPath, verdict[0])
	}
	if !strings.Contains(verdict[0], wantPhrase) {
		t.Fatalf("the failure for %s does not carry %q, so a reader cannot tell WHICH shape is wrong:\n%s", wantPath, wantPhrase, verdict[0])
	}
}

const fixtureGoodDoc = "<!-- doc-class: normative -->\n\n# A standing claim\n"

// TestCorpusGateRedProofMissingHeader is the RED proof for a document that
// never got a banner: line 1 is its title, not a class.
func TestCorpusGateRedProofMissingHeader(t *testing.T) {
	requireNamed(t, []fixtureDoc{
		{name: "good.md", body: fixtureGoodDoc},
		{name: "sub/never-bannered.md", body: "# A title where the banner should be\n\nThe bytes landed through a door with no hook.\n"},
	}, devdoc.Root+"/sub/never-bannered.md", "no "+devdoc.HeaderKind)
}

// TestCorpusGateRedProofMalformedHeader is the RED proof for a line that OPENS
// with the banner prefix and never closes it. The neighbouring shape — a
// well-formed banner carrying commentary INSIDE it — is the illegal-VALUE arm
// below (the parser reaches the vocabulary with "historical — …" and refuses
// it there), so the two arms cover both halves of Rule 36's "an error, not a
// near miss".
func TestCorpusGateRedProofMalformedHeader(t *testing.T) {
	requireNamed(t, []fixtureDoc{
		{name: "good.md", body: fixtureGoodDoc},
		{name: "commented.md", body: "<!-- doc-class: " + devdoc.ClassHistorical + " — classified in the 2026 review\n\n# Body\n"},
	}, devdoc.Root+"/commented.md", "malformed "+devdoc.HeaderKind)
}

// TestCorpusGateRedProofIllegalClass is the RED proof for a well-formed banner
// naming a value outside the closed vocabulary.
func TestCorpusGateRedProofIllegalClass(t *testing.T) {
	requireNamed(t, []fixtureDoc{
		{name: "good.md", body: fixtureGoodDoc},
		{name: "invented.md", body: "<!-- doc-class: philosophical -->\n\n# Body\n"},
	}, devdoc.Root+"/invented.md", "illegal "+devdoc.HeaderKind+" value")
}

// TestCorpusGateRefusesAVacuum is the RED proof for the defect this gate was
// written to repair: a scan that judged nothing must FAIL, never pass silently.
func TestCorpusGateRefusesAVacuum(t *testing.T) {
	root, read := writeFixtureCorpus(t, nil)
	if verdict := docCorpusVerdict(scanDocCorpus(root, read), 0); len(verdict) != 1 {
		t.Fatalf("an EMPTY corpus produced %d failure(s), want the vacuity refusal exactly once: %v", len(verdict), verdict)
	} else if !strings.Contains(verdict[0], "judged 0") {
		t.Fatalf("the vacuity refusal does not say what it measured:\n%s", verdict[0])
	}

	// The floor arm: a corpus that IS scanned but only fragmentarily judged.
	floorRoot, floorRead := writeFixtureCorpus(t, []fixtureDoc{{name: "only.md", body: fixtureGoodDoc}})
	floorVerdict := docCorpusVerdict(scanDocCorpus(floorRoot, floorRead), docCorpusScanFloor)
	if len(floorVerdict) != 1 || !strings.Contains(floorVerdict[0], "non-vacuity floor") {
		t.Fatalf("a fragmentary scan (%d prose file(s)) was not refused by the floor: %v", 1, floorVerdict)
	}
}

// TestCorpusGateDoesNotJudgeNonProse is the COMPLEMENT arm: the predicate must
// not answer wider than its rule. A capture beside the prose owes no banner, and
// a markdown file OUTSIDE the corpus root is not this gate's subject at all.
func TestCorpusGateDoesNotJudgeNonProse(t *testing.T) {
	root, read := writeFixtureCorpus(t, []fixtureDoc{
		{name: "good.md", body: fixtureGoodDoc},
		{name: "DOC-INDEX.tsv", body: "path\tclass\treason\tevidence\n"},
		{name: "evidence/capture.txt", body: "not prose, no banner, no complaint\n"},
	})
	outside := filepath.Join(root, "docs", "archive.md")
	if err := os.MkdirAll(filepath.Dir(outside), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(outside, []byte("# no banner here, and none is owed\n"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	rep := scanDocCorpus(root, read)
	if rep.Judged != 1 {
		t.Fatalf("the gate judged %d file(s), want only the 1 markdown document under %s/ (walked %d)", rep.Judged, devdoc.Root, rep.Walked)
	}
	if len(rep.Failures) != 0 {
		t.Fatalf("the gate complained about out-of-scope files: %v", rep.Failures)
	}
	if rep.Classes[devdoc.ClassNormative] != 1 {
		t.Fatalf("the judged document's class was not counted: %v", rep.Classes)
	}
}
