package standardgate

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestGateConflictMarkerRedProof proves the conflict-marker gate reports the
// residue shape and stays silent on the QUOTED shape — the second half is the
// half that failed in production, so it is asserted as loudly as the first.
//
// Both halves run the REAL predicate (gateByProbe) against REAL files in a
// throwaway tree, not a synthetic *parsed: the gate reads files off disk and a
// probe that never touched the disk could pass while the walker was broken.
func TestGateConflictMarkerRedProof(t *testing.T) {
	g := gateByProbe(t, "TestGateConflictMarkerRedProof")
	if g.scanTextRoot == "" {
		t.Fatal("the conflict-marker gate carries no text scan root — it would walk the Go " +
			"topology only, and the marker that motivated it lives under devdoc/")
	}
	doc := conflictMarkerProbeDoc()
	rel, hits := probeTextFile(t, g, doc)

	// The unfenced pair is reported, at the exact lines it sits on. Line numbers
	// are asserted by CONTENT, not by a hand-counted constant: the probe body is
	// rebuilt from the gate's own vocabulary, so a byte offset here would be a
	// number that rots the first time the probe text changes.
	var reported []int
	for _, h := range hits {
		reported = append(reported, h.line)
	}
	lines := strings.Split(doc, "\n")
	for _, line := range lines {
		if !conflictMarkerRe.MatchString(line) {
			continue
		}
		lineNo := lineNumber(lines, line)
		inFence := fencedAt(lines, lineNo)
		found := containsInt(reported, lineNo)
		switch {
		case inFence && found:
			t.Errorf("FALSE POSITIVE: the gate reported line %d, a marker QUOTED inside a "+
				"fenced block — the fence rule is broken, and a document quoting a conflict "+
				"can only satisfy this gate by deleting its evidence", lineNo)
		case !inFence && !found:
			t.Errorf("RED PROOF FAILED: the gate missed the unfenced marker at line %d — a "+
				"half-resolved merge would ship green", lineNo)
		}
	}
	if len(reported) == 0 {
		t.Fatal("RED PROOF FAILED: an unfenced conflict marker was not reported at all — " +
			"the gate is vacuous")
	}
	if got, want := len(hits), unfencedMarkerCount(lines); got != want {
		t.Fatalf("RED PROOF: reported %d hit(s) at %v, want %d", got, reported, want)
	}
	if hits[0].rel != rel {
		t.Errorf("hit carries path %q, want %q — a hit must name the file it came from", hits[0].rel, rel)
	}

	// NEGATIVE PROBE: the compliant half alone (the same quote, nothing else)
	// reports nothing. Without this, a predicate that reported EVERY line would
	// pass the positive half above.
	quotedOnly := conflictFence + "\n" +
		conflictStartMarker + " HEAD\n\tSandboxRepoContext(t)\n" +
		conflictEndMarker + " fix/heavy-tests-classification\n" + conflictFence + "\n"
	if _, hits := probeTextFile(t, g, quotedOnly); len(hits) != 0 {
		t.Fatalf("FALSE POSITIVE on a fenced quote alone: %+v", hits)
	}

	// NEGATIVE PROBE, prose shapes: the gate must not fire on a marker run that
	// is not a marker — indented (a nested quote), inside a sentence, longer than
	// the vocabulary, or the `=======`-only banner shape it deliberately omits.
	prose := "a line mentioning " + conflictStartMarker + " inline\n" +
		"\t" + conflictStartMarker + " HEAD\n" +
		conflictStartMarker + conflictMarkerOpener + " HEAD\n" +
		"===== case red =====\n"
	if _, hits := probeTextFile(t, g, prose); len(hits) != 0 {
		t.Fatalf("FALSE POSITIVE on prose: %+v", hits)
	}
}

// probeTextFile writes doc into a throwaway module tree and runs the gate's
// predicate over it, returning the file's path and the hits.
func probeTextFile(t *testing.T, g gate, doc string) (string, []hit) {
	t.Helper()
	dir := t.TempDir()
	sub := filepath.Join(dir, "docs")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatalf("mkdir probe tree: %v", err)
	}
	if err := os.WriteFile(filepath.Join(sub, "probe.md"), []byte(doc), 0o644); err != nil {
		t.Fatalf("write probe doc: %v", err)
	}
	rel := "docs/probe.md"
	p := &parsed{rel: rel, src: []byte(doc)}
	return rel, g.find(g, p)
}

// lineNumber is the 1-based index of the first line equal to want.
func lineNumber(lines []string, want string) int {
	for i, l := range lines {
		if l == want {
			return i + 1
		}
	}
	return 0
}

// fencedAt reports whether the 1-based line sits inside a fenced block, tracked
// the same way the predicate tracks it (a closing run must be at least as long
// as the opening one and be a fence line itself).
func fencedAt(lines []string, lineNo int) bool {
	open := ""
	for i, l := range lines {
		if i+1 >= lineNo {
			break
		}
		if run := fenceOpeningRun(l); run != "" {
			if open == "" {
				open = run
			} else if len(run) >= len(open) {
				open = ""
			}
		}
	}
	return open != ""
}

// unfencedMarkerCount is the expected hit count for a probe document.
func unfencedMarkerCount(lines []string) int {
	n := 0
	for i, l := range lines {
		if !conflictMarkerRe.MatchString(l) {
			continue
		}
		if !fencedAt(lines, i+1) {
			n++
		}
	}
	return n
}

// containsInt reports membership, used by the probe's line assertions.
func containsInt(xs []int, want int) bool {
	for _, x := range xs {
		if x == want {
			return true
		}
	}
	return false
}

// TestGateConflictMarkerNeverSatisfiedByDeletingTheQuote pins the property the
// gate exists for: GoPi's own devdoc quote — the verbatim conflict shape inside a
// fenced block — is NOT a hit, and the file that carries it is in the gate's
// coverage (so a future change cannot make the gate green by excluding it).
//
// It asserts the shape in the REAL tree rather than a probe document: the
// failure it guards against (an agent deleting the quoted markers to satisfy a
// scan) happened against this exact file.
func TestGateConflictMarkerNeverSatisfiedByDeletingTheQuote(t *testing.T) {
	root := repoRoot(t)
	rel := quotedConflictDoc
	path := filepath.Join(root, filepath.FromSlash(rel))
	src, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v — the document that carries the quoted conflict marker is "+
			"the gate's own subject; deleting it would remove the evidence, not the defect", rel, err)
	}
	if !strings.Contains(string(src), conflictFence) {
		t.Fatalf("%s no longer carries a fenced block — the quoted conflict shape this gate "+
			"documents must stay fenced (and must stay in the document)", rel)
	}
	g := gateByProbe(t, "TestGateConflictMarkerRedProof")
	hits := g.find(g, &parsed{rel: rel, src: src})
	if len(hits) != 0 {
		t.Fatalf("%s: the fenced quote is reported as residue: %+v — the gate must be "+
			"fence-aware, never satisfied by deleting the quote", rel, hits)
	}
	// Coverage half: the file IS walked by the gate's scan.
	covered := false
	for _, f := range textFilesUnder(t, root, g.scanTextRoot) {
		if f == rel {
			covered = true
			break
		}
	}
	if !covered {
		t.Fatalf("%s is outside the gate's scan — a marker added to it would never be "+
			"reported, and the exclusion would look like a clean tree", rel)
	}
}

// quotedConflictDoc is the document whose fenced block quotes the conflict shape
// the gate must not report. Named once: the red proof and the tree check read the
// same path, so neither can drift onto a different file.
const quotedConflictDoc = "devdoc/repo-context-gate-reverify.md"
