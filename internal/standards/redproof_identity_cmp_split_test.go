package standardgate

import (
	"strings"
	"testing"
)

// ---------------------------------------------------------------------------
// REGRESSION: `cmp` is a SECOND, STRICTER predicate than the digest commands.
//
// The defect this file pins (measured 2026-10-26, task 01a1750d): `cmp` shipped
// inside redproofIdentityCmdRe — the digest commands' boundary, which only asks
// whether the token follows a space/quote/separator. That boundary is a fact
// about shell TEXT; `cmp` is also a Go identifier (the standard library package,
// `cmp := p`, `cmp.Compare`) and an English word. So a 17-line Go file that
// compares NO content at all —
//
//	redproofIdentityCmpTok = "cmp"
//	cmpv := cmp.Compare(a, b)
//	if cmpv == 0 { return 0 }
//
// — turned TestGateRedProofRestoreIdentityRealTreeIsGreen RED, naming the line
// that carries the identifier.
//
// HOW THIS FILE PROVES THE FIX, TWO-SIDED AND WITHOUT A MUTATION. The pre-split
// predicate survives as redproofIdentityCmdRePreSplit behind
// redproofIdentityScanStrict, so both columns run over the SAME fixtures in the
// SAME process at the SAME revision. A green "after" column proves nothing on
// its own (a predicate that never fires satisfies it), and a red "before" column
// proves nothing on its own (a predicate that fires on everything satisfies
// that), so every fixture declares BOTH expectations.
//
// red-proof-identity-ok: this file quotes the fragile shapes on purpose and each
// one is exercised as a probe against the predicate; the fixtures are also
// spliced from redproofIdentityCmpTok so the file's own source is not a corpus hit.
// ---------------------------------------------------------------------------

const (
	// The comparator's spelling is NOT re-declared here: it lives in ONE place,
	// redproofIdentityCmpTok in the production file (Rule 13/14), and every
	// fixture below splices it from there — so no bare token reaches this file's
	// source and a rename of the subject is a single edit.
	// redproofCmpFile / redproofCmpSaved are the two artifacts a proof compares:
	// a file and the local copy a fragile reading checks it against.
	redproofCmpFile  = "prod.go"
	redproofCmpSaved = "prod.go.saved"
	// redproofSumTok is the digest command the OTHER half of the arm polices.
	// It is SPLICED for the same reason the neighbouring test file splices its
	// verdict token: this line is a Go assignment (`tok = "..."`), and the digest
	// half's rule is "a digest command on a line that also carries an equality
	// operator" — a fixture that spelled the whole word here would be a corpus
	// hit against the file that quotes it.
	redproofSumTok = "sha256" + "sum"
)

// redproofCmpCase is one fixture with BOTH columns' expectation: how many hits
// the PRE-SPLIT predicate reports (wantLoose) and how many the SHIPPED one does
// (wantStrict). A (0,0) pair proves nothing on its own and is used ONLY for the
// two shapes that were already silent in both columns (regression guards); the
// census at the bottom requires at least one fixture in each DIRECTION, so the
// table cannot be satisfied by a predicate that changed in one direction only.
type redproofCmpCase struct {
	why        string
	src        string
	wantLoose  int
	wantStrict int
}

// TestGateRedProofRestoreIdentityCmpSplit is the regression guard: the Go
// spellings that used to be reported are silent now, and every real byte
// comparison — plus the digest commands, which this fix deliberately does not
// touch — still reports.
func TestGateRedProofRestoreIdentityCmpSplit(t *testing.T) {
	g := gateByProbe(t, "TestGateRedProofRestoreRedProof")
	cases := []redproofCmpCase{
		// --- FALSE POSITIVES: ordinary Go, no content compared ---------------
		//
		// The loose column is the PRE-SPLIT predicate as it actually behaved, and
		// it demanded the token AND an equality operator on the SAME LINE. That is
		// why the shapes below are written the way they were measured: the Go
		// package selector beside a `==` on one line (`cmp.Compare(b, a) == 0`)
		// and the briefing's own one-line local (`cmp := p; if cmp == subject`).
		// Both go red-before / green-after. The multi-line Go shapes in the second
		// group were ALREADY silent before the change — they are here as
		// regression guards against the next loosening, not as evidence.
		{"the briefing's own probe line: a local named for the token, compared",
			redproofIdentityCmpTok + " := p; if " + redproofIdentityCmpTok + " == subject {\n\treturn\n}\n", 1, 0},
		{"`cmp.Compare(...) == 0` inline — the Go package selector beside an operator",
			"if " + redproofIdentityCmpTok + ".Compare(b, a) == 0 {\n\treturn 1\n}\n", 1, 0},

		// --- GO SHAPES THAT WERE ALREADY SILENT: guards, both columns 0 ------
		{"the standard library's `cmp.Compare` into a local, then `==` on the local",
			"v := " + redproofIdentityCmpTok + ".Compare(a, b)\nif v == 0 {\n\treturn 0\n}\n", 0, 0},
		{"a Go identifier that merely ENDS in the token, compared",
			"xc" + redproofIdentityCmpTok + " := f()\nif xc" + redproofIdentityCmpTok + " == 1 {\n\treturn\n}\n", 0, 0},
		{"a field access whose name ends in the token, compared",
			"if p.xc" + redproofIdentityCmpTok + " == want {\n\treturn\n}\n", 0, 0},
		{"the identifier as a sort callback, compared against nil — MEASURED red-before",
			"slices.SortFunc(xs, " + redproofIdentityCmpTok + ")\nif " + redproofIdentityCmpTok + " == nil {\n\treturn\n}\n", 1, 0},

		// --- REAL DEFECTS: a byte comparison, neither side a revision --------
		//
		// Loose 0 / shipped 1 on the first three: the OLD predicate demanded an
		// equality operator on the line, but the byte comparator's verdict IS its
		// exit status, so these real byte comparisons went UNREPORTED. That is the
		// second half of this change, and it is measured here rather than asserted
		// — a fix that only NARROWED would be trading a false positive for a false
		// negative.
		{"the byte comparator with a flag and two placeholder paths",
			redproofIdentityCmpTok + " -s a b\n", 0, 1},
		{"the byte comparator over a file and the copy a proof saved",
			redproofIdentityCmpTok + " -s " + redproofCmpFile + " " + redproofCmpSaved + "\n", 0, 1},
		{"the byte comparator with NO flag (the plain two-path form)",
			redproofIdentityCmpTok + " " + redproofCmpFile + " " + redproofCmpSaved + "\n", 0, 1},
		{"the byte comparator inside a shell conditional",
			"if " + redproofIdentityCmpTok + " -s a b; then echo restored; fi\n", 0, 1},
		{"the byte comparator beside a verdict comment, both columns red",
			redproofIdentityCmpTok + " -s " + redproofCmpFile + " " + redproofCmpSaved +
				"   # exit 0 = identical = restored\n", 1, 1},

		// --- THE OTHER HALF IS UNTOUCHED: digest commands keep their red flag --
		{"a shell digest equality, both sides computed from local files",
			"test \"$(" + redproofSumTok + " " + redproofCmpFile + ")\" = \"$(" +
				redproofSumTok + " " + redproofCmpSaved + ")\"\n", 1, 1},
		{"the recorded-verdict shape the arm was built for",
			redproofIdentityProbePair(), 1, 1},
		{"a digest command with no comparison at all stays silent",
			redproofSumTok + " " + redproofCmpFile + "\n", 0, 0},

		// --- REVISION-ANCHORED SPELLINGS: silent in BOTH columns --------------
		{"the byte comparator against a revision-qualified path",
			redproofIdentityCmpTok + " -s " + redproofCmpFile + " <(git show 0123456:" +
				redproofCmpFile + ")\n", 0, 0},
		{"a multi-line recipe whose byte comparison names a revision through `git cat-file`",
			"cp " + redproofCmpFile + " " + redproofCmpSaved + "\n" +
				redproofIdentityCmpTok + " -s " + redproofCmpFile + " <(git cat-file -p HEAD:" +
				redproofCmpFile + ")\n", 0, 0},
		{"the corrected spelling: worktree blob against the committed blob",
			"test \"$(git hash-object " + redproofCmpFile + ")\" = \"$(git rev-parse HEAD:" +
				redproofCmpFile + ")\"\n", 0, 0},
	}

	loose, strict := 0, 0
	for _, c := range cases {
		looseGot := redproofIdentityFindWith(g, &parsed{rel: redproofProbeDocRel, src: []byte(c.src)}, false)
		strictGot := redproofIdentityFind(g, &parsed{rel: redproofProbeDocRel, src: []byte(c.src)})
		loose += len(looseGot)
		strict += len(strictGot)

		if len(looseGot) != c.wantLoose {
			t.Errorf("PRE-SPLIT column on «%s»: got %d hit(s) %+v, want %d — the pre-fix predicate "+
				"must still be the thing this fix was measured against", c.why, len(looseGot), looseGot, c.wantLoose)
		}
		if len(strictGot) != c.wantStrict {
			t.Errorf("SHIPPED column on «%s»: got %d hit(s) %+v, want %d — a false positive here is a "+
				"gate the next agent routes around, a false negative is the second half of Rule 5 left "+
				"unguarded", c.why, len(strictGot), strictGot, c.wantStrict)
		}
		if c.wantStrict > 0 && !strings.Contains(strictGot[0].desc, "revision") {
			t.Errorf("the hit's message on «%s» does not carry the correction: %q", c.why, strictGot[0].desc)
		}
	}

	// CENSUS, two-directional, so neither column can be green by looking at
	// nothing AND the fix cannot be a one-way door. Raw totals are NOT the
	// evidence here: this change moves hits in BOTH directions (it drops the
	// identifier false positives and picks up the real byte comparisons the old
	// operator requirement missed), so the totals alone could cancel out and say
	// nothing. The per-case delta does the accounting.
	if strict == 0 {
		t.Fatal("the shipped predicate reported NOTHING across every fixture — a fix that stops " +
			"the arm from matching turns the second half of Rule 5 unguarded while its own red " +
			"proof stays green")
	}
	if loose == 0 {
		t.Fatal("the pre-split predicate reported NOTHING — the fix would then have nothing to be " +
			"measured against, and the red column of this table is what makes it two-sided")
	}
	removed, added := 0, 0
	for _, c := range cases {
		switch {
		case c.wantLoose > c.wantStrict:
			removed += c.wantLoose - c.wantStrict
		case c.wantStrict > c.wantLoose:
			added += c.wantStrict - c.wantLoose
		}
	}
	if removed == 0 {
		t.Fatal("no fixture goes red-before/green-after — the identifier false positives this " +
			"change exists for are not represented, so the table cannot detect their return")
	}
	if added == 0 {
		t.Fatal("no fixture goes green-before/red-after — the operator requirement this change " +
			"relaxes without the byte comparison's own argument test would be a false NEGATIVE " +
			"trade, and nothing here would notice")
	}
	t.Logf("cmp split: %d fixture(s); pre-split column %d hit(s), shipped column %d hit(s); "+
		"%d identifier false positive(s) removed, %d real byte comparison(s) newly reported",
		len(cases), loose, strict, removed, added)
}

// TestGateRedProofRestoreIdentityCmpLocator pins WHERE the hit lands: the line
// a reader has to change — the comparison, never the escaping, never the fence.
// A silent arm cannot satisfy this, and an arm that reports the wrong line sends
// the reader to the wrong edit.
func TestGateRedProofRestoreIdentityCmpLocator(t *testing.T) {
	g := gateByProbe(t, "TestGateRedProofRestoreRedProof")
	comparison := redproofIdentityCmpTok + " -s " + redproofCmpFile + " " + redproofCmpSaved
	doc := "## Red proof\n\n" +
		"```\n" +
		"cp " + redproofCmpFile + " " + redproofCmpSaved + "\n" +
		comparison + "\n" +
		"```\n"
	want := lineNumber(strings.Split(doc, "\n"), comparison)
	if want == 0 {
		t.Fatal("probe construction error: the comparison line is not in the probe document")
	}
	got := redproofIdentityFind(g, &parsed{rel: redproofProbeDocRel, src: []byte(doc)})
	if len(got) != 1 {
		t.Fatalf("got %d hit(s) %+v, want exactly 1 — the byte comparison must still be reported "+
			"after the split", len(got), got)
	}
	if got[0].line != want {
		t.Fatalf("hit at line %d, want %d — the failure must point at the COMPARISON line", got[0].line, want)
	}
}
