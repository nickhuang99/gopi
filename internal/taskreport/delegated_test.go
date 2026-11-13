package taskreport

// delegated_test.go — the leaf half of the result-delegation check
// (delegated.go): the motivating sentence must be recognised, and every
// honest sentence that LOOKS like it must not be.
//
// The motivating case is reproduced verbatim from row 01a12d6b's own
// `## Verification` (2026-10-12) — the run whose acceptance stream afterwards
// exited 2 with two red tests while the report already stood.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// delegatedMotivatingSentence is the defect, verbatim: an instrument, an arrow,
// a delegation phrase and a path — and no result anywhere in the sentence.
const delegatedMotivatingSentence = "`make test-budget PKGS='./internal/testutil'` → see the stream at\n  `artifacts/make-testbudget-testutil.txt`."

// delegatedMotivatingReport is that sentence in the report shape it shipped in,
// WITH both sections a real report carries — the shape that proved the
// adjudication must read the UNION of them: a section-picking rule reads the
// evidence section and misses the sentence entirely.
const delegatedMotivatingReport = `# Task Report: capture an identity once the child's argv is published

## Completed
- the fix is on the branch

## Files
- internal/testutil/proc_identity.go

## Verification
- **Build**: ` + "`go build ./...`" + ` → OK (exit 0).
- **Tests**: ` + "`make test-budget PKGS='./internal/testutil'`" + ` → see the stream at
  ` + "`artifacts/make-testbudget-testutil.txt`" + `.
- **Ran** (verbatim, on the fixed tree):
  ` + "```bash" + `
  go test -count 1 ./internal/testutil
  ` + "```" + `

## Evidence (self-filed)
- the four named tests: all PASS.

## Not Done
- the pre-existing red pair, proven at my own base commit
`

// TestDelegatedClaimsFindsTheMotivatingSentence: the defect is recognised, and
// the PATH it hands the reader to comes back with it (the seal must be able to
// say whether that path exists at all).
func TestDelegatedClaimsFindsTheMotivatingSentence(t *testing.T) {
	v := DelegatedClaimsIn(delegatedMotivatingReport)
	if len(v.Claims) != 1 {
		t.Fatalf("want exactly the motivating sentence, got %d claim(s): %+v", len(v.Claims), v.Claims)
	}
	c := v.Claims[0]
	if !strings.Contains(c.Sentence, "see the stream at") {
		t.Errorf("sentence not the motivating one: %q", c.Sentence)
	}
	want := "artifacts/make-testbudget-testutil.txt"
	if len(c.Paths) != 1 || c.Paths[0] != want {
		t.Errorf("paths = %v, want [%s]", c.Paths, want)
	}
	if !strings.Contains(v.Section, VerificationHeading()) {
		t.Errorf("section = %q, want it to name %s", v.Section, VerificationHeading())
	}
}

// TestDelegatedClaimsAcceptsAStatedResult: the SAME shape with a result stated
// in the same sentence is a finished delegation, not a gap — the report is
// checkable BY the artifact, and a reader is not left guessing.
func TestDelegatedClaimsAcceptsAStatedResult(t *testing.T) {
	body := `# Task Report: x

## Verification
- **Tests**: ` + "`make test-budget PKGS='./internal/testutil'`" + ` → see the stream at
  ` + "`artifacts/make-testbudget-testutil.txt`" + ` (exit=0, 2 red → green).
`
	if v := DelegatedClaimsIn(body); len(v.Claims) != 0 {
		t.Fatalf("a sentence stating its result must not be flagged, got %+v", v.Claims)
	}
}

// TestDelegatedClaimsAcceptsARanLine: Rule 31's `Ran:` block is the surface
// being EXECUTED, and a fenced transcript is not the report's prose about an
// outcome — flagging it would punish the behaviour Rule 31 asks for. Measured
// corpus check: without the fence clause, the only hits a 369-report corpus
// produced beyond the motivating sentence were pasted shell scripts.
func TestDelegatedClaimsAcceptsARanLine(t *testing.T) {
	body := `# Task Report: x

## Verification
- **Ran** (verbatim):
  ` + "```bash" + `
  make test-budget PKGS='./internal/tools' 2>&1 | tee artifacts/budget.txt
  ./bin/gopi task show 01a12d6b
  ` + "```" + `
- **Result**: exit=0, the stream is at ` + "`artifacts/budget.txt`" + `.
`
	if v := DelegatedClaimsIn(body); len(v.Claims) != 0 {
		t.Fatalf("a fenced invocation is a transcript, not a delegation, got %+v", v.Claims)
	}
}

// TestDelegatedClaimsIgnoresAPassingAside: a sentence that states its result
// and mentions "see Not Done" in passing, while a path token sits EARLIER in it,
// is not a delegation — what a delegation hands over sits AFTER the phrase (the
// measured false positive: session 01a10f9c's doccheck line).
func TestDelegatedClaimsIgnoresAPassingAside(t *testing.T) {
	body := `# Task Report: x

## Verification
- ` + "`./bin/gopi tool doccheck`" + ` → prints the pre-existing
  ` + "`normative-no-check devdoc/scripts-retired-verb-sweep.md`" + ` gap and ` + "`1 gap(s)`" + `
  (see Not Done).
`
	if v := DelegatedClaimsIn(body); len(v.Claims) != 0 {
		t.Fatalf("a stated result with a passing aside must not be flagged, got %+v", v.Claims)
	}
}

// TestDelegatedClaimsFindsTheChineseSpelling: the same delegation in the
// operator's own language (the sibling real report: "→ 见 `artifacts/26-budget.txt`
// （最终读数）") is the same defect and must be recognised — a check that only
// reads English would leave half this repo's reports unexamined.
func TestDelegatedClaimsFindsTheChineseSpelling(t *testing.T) {
	body := `# Task Report: x

## Verification
- ` + "`make test-budget PKGS='./internal/tools ./cmd/agent'`" + ` → 见 ` + "`artifacts/26-budget.txt`" + `
  （最终读数）。
`
	v := DelegatedClaimsIn(body)
	if len(v.Claims) != 1 || v.Claims[0].Paths[0] != "artifacts/26-budget.txt" {
		t.Fatalf("the Chinese delegation spelling must be recognised, got %+v", v.Claims)
	}
}

// TestDelegatedClaimsReadsBothSections: the union is the MEASURED correction —
// the motivating report carries both sections and its sentence lives under
// Verification, so a first-present-section rule (what the sibling checks do)
// reads the evidence section and misses it. Pinned here so a later
// "consistency" edit cannot silently reintroduce the miss.
func TestDelegatedClaimsReadsBothSections(t *testing.T) {
	withEvidence := delegatedMotivatingReport
	if v := DelegatedClaimsIn(withEvidence); len(v.Claims) != 1 {
		t.Fatalf("a report carrying both sections must still be adjudicated on its Verification text, got %d", len(v.Claims))
	}
	// The same sentence with NO evidence section is found too (the section
	// cannot be deleted to dodge the check).
	withoutEvidence := strings.Split(delegatedMotivatingReport, "\n## Evidence")[0] +
		"\n\n## Not Done\n- nothing\n"
	if v := DelegatedClaimsIn(withoutEvidence); len(v.Claims) != 1 {
		t.Fatalf("deleting the evidence section must not dodge the check, got %d", len(v.Claims))
	}
	// A body with NEITHER section is adjudicated on the whole body.
	whole := "# Task Report: x\n\n" + delegatedMotivatingSentence + "\n"
	if v := DelegatedClaimsIn(whole); len(v.Claims) != 1 {
		t.Fatalf("a body with no section at all must still be adjudicated, got %d", len(v.Claims))
	}
}

// TestDelegatedPathShapeRejectsNonArtifacts: the path token is what the seal
// MEASURES, so the shape test is the one thing keeping the record honest —
// the instrument's own argv (`./bin/gopi`, `./internal/testutil`), a URL and a
// bare filename are not artifacts.
func TestDelegatedPathShapeRejectsNonArtifacts(t *testing.T) {
	for _, tok := range []string{
		"./bin/gopi",
		"./internal/testutil",
		"https://example.com/x.txt",
		"out.txt",
		"artifacts/",
	} {
		if delegatedPathShape(tok) {
			t.Errorf("%q must NOT count as a delegated artifact path", tok)
		}
	}
	for _, tok := range []string{
		"artifacts/make-testbudget-testutil.txt",
		"~/.gopi/sessions/x/artifacts/y.log",
	} {
		if !delegatedPathShape(tok) {
			t.Errorf("%q must count as a delegated artifact path", tok)
		}
	}
}

// TestDelegatedRecordCarriesTheStatusItWasGiven: the record names each path
// with the caller's status, and a path the caller did NOT status is recorded
// WITHOUT a verdict (Rule 15 — an unknown is stated, never invented).
func TestDelegatedRecordCarriesTheStatusItWasGiven(t *testing.T) {
	v := DelegatedClaimsIn(delegatedMotivatingReport)
	rec := DelegatedRecord(v, map[string]string{
		"artifacts/make-testbudget-testutil.txt": DelegatedPathStatusAbsent,
	})
	if !strings.HasPrefix(rec, DelegatedDigestPrefix) {
		t.Fatalf("record must carry the digest prefix, got %q", rec)
	}
	if !strings.Contains(rec, DelegatedPathStatusAbsent) {
		t.Errorf("record must carry the path's status, got %q", rec)
	}
	if bare := DelegatedRecord(v, nil); strings.Contains(bare, "present") || strings.Contains(bare, "ABSENT") {
		t.Errorf("an unstatused path must carry no verdict, got %q", bare)
	}
}

// TestDelegatedPathStatusPresentNamesTheSize: the present case is the WEAKER,
// true statement — the delegation is recoverable and the report itself states
// no result — and it must never read as an endorsement.
func TestDelegatedPathStatusPresentNamesTheSize(t *testing.T) {
	s := DelegatedPathStatusPresent(1234)
	if !strings.Contains(s, "1234 bytes") || !strings.Contains(s, "recoverable") {
		t.Fatalf("present status must name the size and the limit, got %q", s)
	}
	if strings.Contains(strings.ToLower(s), "pass") || strings.Contains(strings.ToLower(s), "ok ") {
		t.Errorf("present status must not read as a verdict, got %q", s)
	}
}

// TestDelegatedWarningQuotesTheSentenceAndItsPaths: the stderr face carries the
// author's own words and the paths — a finding that is recorded but never shown
// is half a record (the gateclaim.go discipline).
func TestDelegatedWarningQuotesTheSentenceAndItsPaths(t *testing.T) {
	v := DelegatedClaimsIn(delegatedMotivatingReport)
	w := DelegatedWarning("t-x", v, map[string]string{"artifacts/make-testbudget-testutil.txt": DelegatedPathStatusAbsent})
	for _, want := range []string{DelegatedWarningLabel, "t-x", "see the stream at", "artifacts/make-testbudget-testutil.txt", DelegatedPathStatusAbsent} {
		if !strings.Contains(w, want) {
			t.Errorf("warning must contain %q, got:\n%s", want, w)
		}
	}
}

// TestDelegatedSentenceExcerptIsBounded: a bullet can be a paragraph; a stamp
// has to stay a stamp.
func TestDelegatedSentenceExcerptIsBounded(t *testing.T) {
	long := strings.Repeat("x", delegatedClaimSentenceMaxRunes*2)
	got := DelegatedSentenceExcerpt(long)
	if n := len([]rune(got)); n > delegatedClaimSentenceMaxRunes+1 {
		t.Errorf("excerpt = %d runes, want <= %d", n, delegatedClaimSentenceMaxRunes+1)
	}
}

// TestDelegatedClaimsSkipTheSiblingSections: the check reads the two sections a
// report states RESULTS in. A sentence in `## Completed` (a narrative) is not a
// gate delegation, and reading it would be a false positive on the shape of
// honest work.
func TestDelegatedClaimsSkipTheSiblingSections(t *testing.T) {
	body := `# Task Report: x

## Completed
- ` + "`make test-budget PKGS='./internal/x'`" + ` → see the stream at ` + "`artifacts/x.txt`" + `

## Verification
- Build: ` + "`go build ./...`" + `: OK

## Not Done
- nothing
`
	if v := DelegatedClaimsIn(body); len(v.Claims) != 0 {
		t.Fatalf("a narrative sentence outside the result sections must not be flagged, got %+v", v.Claims)
	}
}

// TestDelegatedClaimsOnTheTranscriptOfTheMotivatingRun: the real text, read off
// disk, must yield the one sentence — the corpus measurement, pinned as a test
// so the predicate's behaviour on a true report is not a one-off observation.
func TestDelegatedClaimsOnTheTranscriptOfTheMotivatingRun(t *testing.T) {
	root := os.Getenv("HOME")
	if root == "" {
		t.Skip("no HOME to read the transcript from")
	}
	p := filepath.Join(root, ".gopi", "sessions", "01a12d6b-b00a-7d1d-9477-1be79b5a8fe7", FileName)
	body, err := os.ReadFile(p)
	if err != nil {
		t.Skipf("the motivating report is not on this host: %v", err)
	}
	v := DelegatedClaimsIn(string(body))
	if len(v.Claims) == 0 {
		t.Fatalf("the motivating report must yield its delegating sentence")
	}
	for _, c := range v.Claims {
		if !strings.Contains(c.Sentence, "see the stream at") {
			t.Errorf("unexpected claim on the real report: %q", c.Sentence)
		}
	}
}
