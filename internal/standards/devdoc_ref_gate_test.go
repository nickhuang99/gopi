package standardgate

// The devdoc-citation gate's proofs: the red proof (a planted pointer at a
// document that does not exist TURNS IT RED), the clean control (the same
// comment pointing at a document that DOES exist is green), the escape arm (a
// deliberate non-citation is green BECAUSE it says so), and the real-tree arm
// with its non-vacuity floor.
//
// Every arm drives the SAME predicate the scan drives (devdocRefFind), and the
// fixture is a real directory on disk: the predicate's whole job is an
// os.Stat, so a pure in-memory string cannot prove it. Rule 32's temp rule is
// honoured by writing nothing at all — t.TempDir() is the only destination.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// devdocRefFixtureRoot builds a throwaway module tree: a devdoc/ directory
// holding exactly the documents named in existing, and an internal/ directory
// holding one Go source file carrying commentLines verbatim. It returns the
// parsed file, ready to be handed to the predicate.
func devdocRefFixtureRoot(t *testing.T, existing []string, commentLines []string) *parsed {
	t.Helper()
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, devdocRefRootDir), 0o755); err != nil {
		t.Fatalf("mkdir devdoc: %v", err)
	}
	for _, name := range existing {
		path := filepath.Join(root, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", name, err)
		}
		if err := os.WriteFile(path, []byte("<!-- doc-class: historical -->\n# fixture\n"), 0o644); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}
	if err := os.MkdirAll(filepath.Join(root, "internal"), 0o755); err != nil {
		t.Fatalf("mkdir internal: %v", err)
	}
	src := "package internal\n\n"
	for _, line := range commentLines {
		src += line + "\n"
	}
	src += "\nfunc Fixture() {}\n"
	rel := "internal/fixture.go"
	abs := filepath.Join(root, filepath.FromSlash(rel))
	if err := os.WriteFile(abs, []byte(src), 0o644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	return parseRepoFile(t, root, rel)
}

// devdocRefFixtureHits runs the real predicate over one fixture file.
func devdocRefFixtureHits(t *testing.T, p *parsed) []hit {
	t.Helper()
	return devdocRefFind(devdocRefGate(), p)
}

// TestGate10DevdocRefRedProof is the RED PROOF: a production comment citing a
// devdoc that does not exist in the tree is reported. It is the planted
// reproduction of the defect the gate was written for — the same shape that
// stood twice in this repository (a pointer nobody could follow).
func TestGate10DevdocRefRedProof(t *testing.T) {
	p := devdocRefFixtureRoot(t, nil, []string{
		"// the measurement is written up in " + devdocRefRootDir + "/planted-missing-doc" + devdocRefExt + ".",
	})
	hits := devdocRefFixtureHits(t, p)
	if len(hits) != 1 {
		t.Fatalf("a citation of a document that does not exist must be ONE hit, got %d: %v", len(hits), hits)
	}
	if hits[0].rel != p.rel {
		t.Errorf("the hit must name the file that carries the comment, got %q", hits[0].rel)
	}
	if hits[0].line != 3 {
		t.Errorf("the hit must name the COMMENT's line (3), got %d", hits[0].line)
	}
	if !strings.Contains(hits[0].desc, "does not exist") {
		t.Errorf("the failure must say the target is missing, not just that something is wrong: %q", hits[0].desc)
	}
}

// TestGate10DevdocRefGreenWhenTheTargetExists is the CLEAN CONTROL: the very
// same comment shape, pointing at a document that IS there, is not reported. A
// predicate that reported every citation would pass the red proof and make the
// gate useless.
func TestGate10DevdocRefGreenWhenTheTargetExists(t *testing.T) {
	doc := devdocRefRootDir + "/present-doc" + devdocRefExt
	p := devdocRefFixtureRoot(t, []string{doc}, []string{
		"// the measurement is written up in " + doc + ".",
	})
	if hits := devdocRefFixtureHits(t, p); len(hits) != 0 {
		t.Fatalf("a citation of an existing document must not be reported, got %v", hits)
	}
}

// TestGate10DevdocRefRedProofNestedPathAndCase pins the two shapes a reader
// writes when a corpus grows subdirectories: a slash inside the name is part of
// the citation, and a name that differs only by CASE is a different (and here,
// absent) document — a case-insensitive filesystem must not make this arm
// silently vacuous, so the fixture's citation is the one that does NOT exist.
func TestGate10DevdocRefRedProofNestedPathAndCase(t *testing.T) {
	present := devdocRefRootDir + "/evidence/record.md"
	p := devdocRefFixtureRoot(t, []string{present}, []string{
		"// " + present + " exists.",
		"// " + devdocRefRootDir + "/evidence/missing.md does not.",
	})
	hits := devdocRefFixtureHits(t, p)
	if len(hits) != 1 {
		t.Fatalf("exactly the absent nested citation must be reported, got %d: %v", len(hits), hits)
	}
	if hits[0].line != 4 {
		t.Errorf("the hit must land on the absent citation's line (4), got %d", hits[0].line)
	}
}

// TestGate10DevdocRefEscapeIsVisibleAndRatched proves the escape arm: a
// reported shape that is NOT a citation (an illustrative example, a quoted
// message) is green only because the line SAYS so, and the site stays visible as
// `escaped` in the accounting rather than disappearing from it.
func TestGate10DevdocRefEscapeIsVisibleAndRatched(t *testing.T) {
	g := devdocRefGate()
	p := devdocRefFixtureRoot(t, nil, []string{
		"// " + devdocRefEscape + " this line quotes the SHAPE, it is not a citation: " +
			devdocRefRootDir + "/some-example" + devdocRefExt,
	})
	hits := devdocRefFixtureHits(t, p)
	if len(hits) != 1 {
		t.Fatalf("the escape must not HIDE the site, it must explain it — expected 1 hit, got %d", len(hits))
	}
	files := map[string]*parsed{p.rel: p}
	stamp(hits, files)
	if got := classify(g, hits[0], p); got != classEscaped {
		t.Fatalf("a line carrying %q must classify as escaped, got %v", devdocRefEscape, got)
	}
	c := classifyAll(g, hits, files)
	if c.escaped != 1 || c.unannot != 0 {
		t.Errorf("the accounting must keep the escaped site visible (escaped=%d unannot=0), got %+v", c.escaped, c)
	}
}

// TestGate10DevdocRefRefusesACommentlessFile proves the Rule 15 half: a parsed
// file with no comment data cannot answer the question, and it must be reported
// as a GATE defect rather than passing as a clean file.
func TestGate10DevdocRefRefusesACommentlessFile(t *testing.T) {
	hits := devdocRefFind(devdocRefGate(), &parsed{rel: "internal/x.go"})
	if len(hits) != 1 {
		t.Fatalf("a file parsed without comments must produce one loud hit, got %d", len(hits))
	}
	if !strings.Contains(hits[0].desc, "GATE") {
		t.Errorf("the refusal must name whose defect it is (the gate, not the tree): %q", hits[0].desc)
	}
}

// TestGate10DevdocRefCannotResolveTheRootIsNotASilentPass proves the other Rule
// 15 half at the MISSING-check itself: with no module root the citation is
// reported with the reason it could not be checked, never passed as present.
func TestGate10DevdocRefCannotResolveTheRootIsNotASilentPass(t *testing.T) {
	desc, missing := devdocRefMissing("", devdocRefRootDir+"/unchecked"+devdocRefExt)
	if !missing {
		t.Fatal("an unresolvable root must report the citation, not pass it")
	}
	if !strings.Contains(desc, "could not be resolved") {
		t.Errorf("the reason must be the resolution failure, not a claim that the file is absent: %q", desc)
	}
}

// TestGate10DevdocRefJudgesTheRealTree is the enforcement arm over THIS tree,
// with its non-vacuity floors stated out loud: it prints what it walked, how
// many citations it found, and how many of those it judged — and it refuses a
// green it did not measure. The population it reports is counted by the ONE
// walker (devdocRefSites), not from the failures, so a recognizer that matched
// nothing in 1000 files cannot look like a clean corpus.
func TestGate10DevdocRefJudgesTheRealTree(t *testing.T) {
	g := devdocRefGate()
	hits, files := scan(t, g)
	c := classifyAll(g, hits, files)
	occurrences := 0
	nested := 0
	for _, p := range files {
		for _, site := range devdocRefSites(p) {
			occurrences++
			if len(strings.Split(site.ref, "/")) > 2 {
				nested++
			}
		}
	}
	t.Logf("%s: walked %d production file(s), %d citation occurrence(s) "+
		"(nested %d), %d reported unreachable (baselined=%d escaped=%d new=%d), "+
		"%d distinct unreachable target(s): %v",
		g.id, len(files), occurrences, nested, len(hits),
		c.baselined, c.escaped, c.unannot,
		len(devdocRefTargets(hits)), devdocRefTargets(hits))
	if len(files) < devdocRefFileFloor {
		t.Fatalf("the scan read %d file(s), below the floor of %d — a scan that read (almost) "+
			"nothing must not print a green", len(files), devdocRefFileFloor)
	}
	if occurrences < devdocRefCitationFloor {
		t.Fatalf("the scan found %d citation occurrence(s), below the floor of %d — the "+
			"recognizer stopped matching, and a green here would be a vacuum",
			occurrences, devdocRefCitationFloor)
	}
	if nested == 0 {
		t.Errorf("the scan found NO citation with a path segment — the corpus has subdirectories "+
			"(devdoc/competitor/, devdoc/requirements/), so a walker that never matched one is "+
			"blind to them: %s", "check devdocRefRe")
	}
	if c.unannot > 0 {
		for _, h := range hits {
			if classify(g, h, files[h.rel]) == classUnannotated {
				t.Errorf("unreachable devdoc citation at %s:%d — %s\n"+
					"  fix it: delete the pointer, repoint it at a document that exists, or mark it\n"+
					"  %s <why it is not a citation> when it is an example rather than a reference.",
					h.rel, h.line, h.desc, devdocRefEscape)
			}
		}
	}
}
