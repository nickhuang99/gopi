// filteredclaim_test.go — the filtered-package claim's BIDIRECTIONAL proof.
//
// Every fixture below is a sentence the run under investigation actually wrote
// (run 01a12ee4, 2026-10-12), paired with the reading the same rev produced, so
// the check is pinned against measurements rather than against invented text:
//
//	RED    the report's package-level `ok` from a `-run`-narrowed command, on a
//	       rev where the whole package was FAIL (the motivating defect) → FIRES;
//	RED    the same defect in the `make test-budget` spelling, and the
//	       `-skip` / `-bench` / `-run=Ping` spellings of the narrowing → FIRE;
//	GREEN  the WHOLE-PACKAGE reading (`go test ./internal/standards/` → ok on a
//	       green rev; FAIL exit 1 on the red one) → SILENT. This is the
//	       counterexample the epic names: a reading that is worth something must
//	       never be stamped, and condition 2 (a narrowing flag) is what keeps it
//	       out;
//	GREEN  the TEST-LEVEL measurement (`go test -run … ./pkg/` with the run's
//	       own `executed: N test(s)` count and no package verdict) → SILENT: the
//	       remedy sentence the record prescribes is not itself a defect.

package taskreport

import (
	"strings"
	"testing"
)

// filteredClaimMeasuredSentence is the motivating claim, verbatim from the
// delivery run's report under `## Verification` — the package-level `ok` of a
// command whose `-run` pattern matched no test in that package, on a rev where
// `go test ./internal/standards/` printed FAIL exit 1.
const filteredClaimMeasuredSentence = "`go test -run 'TestConfigDoc|TestCensusGate|TestRepoHygiene' " +
	"./internal/standards/` → `ok`."

// filteredClaimBudgetSentence is the same defect in the other instrument
// spelling: the repository's declared test entry point, narrowed by source
// filters — the shape `scripts/test-budget.sh` itself records as `filter:` and
// answers with exit 5 only when the filter matched NOTHING.
const filteredClaimBudgetSentence = "`make test-budget PKGS='./internal/tools' -run 'Holder|Sweep'` → " +
	"`ok github.com/nickhuang99/gopi/internal/tools 61.1s`"

// filteredClaimHonestWholePackage is the counterexample: the whole package ran,
// so the package-level line IS a reading of the package. On the red rev it
// printed FAIL; on a green one `ok`. Both spellings must stay silent.
const filteredClaimHonestWholePackage = "`go test ./internal/standards/` → FAIL exit 1 (88.9s); " +
	"on master `make test-budget PKGS='./internal/standards'` → `ok github.com/nickhuang99/gopi/internal/standards 17.5s`"

// filteredClaimTestLevelMeasurement is the shape the record's remedy asks for:
// the run states WHICH tests ran instead of a package verdict.
const filteredClaimTestLevelMeasurement = "`go test -count=1 -run 'Holder|Sweep' ./internal/scratch/` → " +
	"executed: 12 test(s) ran in this run; the stream named each one, no package verdict is claimed"

// filteredClaimBody wraps sentences in the section a run states its gate results
// in — the motivating report's own `## Verification`.
func filteredClaimBody(sentences ...string) string {
	var b strings.Builder
	b.WriteString("# Task Report: the narrowed reading\n\n## Completed\n- (a change)\n\n")
	b.WriteString(VerificationHeading())
	b.WriteString("\n")
	for _, s := range sentences {
		b.WriteString("- ")
		b.WriteString(s)
		b.WriteString("\n")
	}
	b.WriteString("\n## Not Done\n- (nothing)\n")
	return b.String()
}

// TestFilteredClaimFiresOnTheMeasuredSentence is the RED proof: the delivery
// run's own sentence — a package-level `ok` from a `-run`-narrowed command on a
// rev where that package was FAIL — must be recorded.
func TestFilteredClaimFiresOnTheMeasuredSentence(t *testing.T) {
	v := FilteredClaimsIn(filteredClaimBody(filteredClaimMeasuredSentence))
	if len(v.Claims) != 1 {
		t.Fatalf("claims = %d, want 1 — the measured sentence must be recorded:\n%s",
			len(v.Claims), v.Section)
	}
	if got := v.Claims[0].Filters; len(got) != 1 || got[0] != narrowingFilterFlags[0] {
		t.Errorf("filters = %v, want exactly [%s]", got, narrowingFilterFlags[0])
	}
	if v.Claims[0].Sentence != filteredClaimMeasuredSentence {
		t.Errorf("the record must quote the author's sentence verbatim:\n got %q\nwant %q",
			v.Claims[0].Sentence, filteredClaimMeasuredSentence)
	}
	if v.Section != VerificationHeading() {
		t.Errorf("section = %q, want %q", v.Section, VerificationHeading())
	}
}

// TestFilteredClaimFiresOnTheBudgetInstrumentSpelling: the defect is not about
// `go test` — the declared entry point narrowed by a source filter is the same
// reading.
func TestFilteredClaimFiresOnTheBudgetInstrumentSpelling(t *testing.T) {
	v := FilteredClaimsIn(filteredClaimBody(filteredClaimBudgetSentence))
	if len(v.Claims) != 1 {
		t.Fatalf("claims = %d, want 1 (the `make test-budget … -run` spelling):\n%s", len(v.Claims), v.Section)
	}
}

// TestFilteredClaimFiresOnEveryNarrowingFlag pins the flag vocabulary: each
// selecting spelling fires, and the record names the flag that narrowed it.
func TestFilteredClaimFiresOnEveryNarrowingFlag(t *testing.T) {
	for _, tc := range []struct{ name, sentence, want string }{
		{"run spaced", "`go test -run 'A|B' ./internal/tools/` → `ok`", "-run"},
		{"run glued", "`go test -run=Ping ./internal/tools/` → `ok`", "-run"},
		{"skip", "`go test -skip 'Flaky' ./internal/tools/` → `ok`", "-skip"},
		{"bench", "`go test -bench . -run '^$' ./internal/tools/` → `ok`", "-run"},
		{"bench only", "`go test -bench=. ./internal/tools/` → `pass`", "-bench"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			v := FilteredClaimsIn(filteredClaimBody(tc.sentence))
			if len(v.Claims) != 1 {
				t.Fatalf("claims = %d, want 1 for %q", len(v.Claims), tc.sentence)
			}
			if !containsString(v.Claims[0].Filters, tc.want) {
				t.Errorf("filters = %v, want it to name %q", v.Claims[0].Filters, tc.want)
			}
		})
	}
}

// TestFilteredClaimIgnoresTheWholePackageReading is the epic's explicit
// counter-proof: a package-level result from a command that narrows NOTHING is
// the honest shape and must never be stamped — on the green rev and on the red
// one (the same command, two readings).
func TestFilteredClaimIgnoresTheWholePackageReading(t *testing.T) {
	for _, body := range []string{
		filteredClaimBody(filteredClaimHonestWholePackage),
		filteredClaimBody("`make test-budget PKGS='./internal/standards'` → `ok github.com/nickhuang99/gopi/internal/standards 17.5s`"),
		filteredClaimBody("`go build ./...` → exit 0; `go vet ./internal/tools/` → exit 0"),
	} {
		if v := FilteredClaimsIn(body); len(v.Claims) != 0 {
			t.Errorf("false positive on an honest whole-package reading: %v", v.Claims)
		}
	}
}

// TestFilteredClaimIgnoresTheFilteredTestLevelMeasurement: a filtered command
// reported with the run's OWN reading (which tests ran) is the remedy shape,
// not the defect. The package name appears as context and states no verdict.
func TestFilteredClaimIgnoresTheFilteredTestLevelMeasurement(t *testing.T) {
	if v := FilteredClaimsIn(filteredClaimBody(filteredClaimTestLevelMeasurement)); len(v.Claims) != 0 {
		t.Errorf("false positive on a filtered TEST-LEVEL measurement: %v", v.Claims)
	}
	// A report that never states a result at all is not this defect either: the
	// ran-check and the delegation record own those shapes.
	for _, s := range []string{
		"`go test -run 'A|B' ./internal/standards/` — the invocation is on the Ran: line",
		"`go test -run 'A|B' ./internal/tools/`",
	} {
		if v := FilteredClaimsIn(filteredClaimBody(s)); len(v.Claims) != 0 {
			t.Errorf("false positive on a sentence that states no package result: %q", s)
		}
	}
}

// TestFilteredClaimIgnoresSubstringSpellings: a spelling that merely CONTAINS a
// flag is not a flag — the anchoring is what keeps prose and paths out.
func TestFilteredClaimIgnoresSubstringSpellings(t *testing.T) {
	for _, s := range []string{
		"`go test ./internal/run-not-this/` → `ok`",
		"the package `./internal/benchmarks/` → `ok`",
		"`go test ./internal/tools/` → `ok` (no filter was used for this run)",
	} {
		if v := FilteredClaimsIn(filteredClaimBody(s)); len(v.Claims) != 0 {
			t.Errorf("false positive on %q: %v", s, v.Claims)
		}
	}
}

// TestFilteredClaimRecordAndWarningCarryTheRemedy: a record that says only
// "wrong" leaves the author guessing the fix. The remedy — re-run the package
// itself — rides both the digest fragment and the stderr line.
func TestFilteredClaimRecordAndWarningCarryTheRemedy(t *testing.T) {
	v := FilteredClaimsIn(filteredClaimBody(filteredClaimMeasuredSentence))
	rec := FilteredRecord(v)
	if !strings.HasPrefix(rec, FilteredClaimDigestPrefix) {
		t.Fatalf("record = %q, want the %s prefix", rec, FilteredClaimDigestPrefix)
	}
	if !strings.Contains(rec, FilteredClaimRemedy) {
		t.Errorf("the digest record must carry the remedy:\n%s", rec)
	}
	if !strings.Contains(rec, filteredClaimMeasuredSentence) {
		t.Errorf("the digest record must quote the sentence:\n%s", rec)
	}
	warn := FilteredWarning("t-rule31", v)
	for _, want := range []string{FilteredClaimWarningLabel, "t-rule31", FilteredClaimRemedy, VerificationHeading()} {
		if !strings.Contains(warn, want) {
			t.Errorf("warning does not name %q:\n%s", want, warn)
		}
	}
}

// TestFilteredClaimIsSilentOnACleanReport pins the no-op direction: the record
// and the warning render nothing at all when there is nothing to record (a
// caller appends the fragment unconditionally — an empty one must not put a
// stray separator on the row's digest).
func TestFilteredClaimIsSilentOnACleanReport(t *testing.T) {
	v := FilteredClaimsIn(filteredClaimBody(filteredClaimHonestWholePackage))
	if rec := FilteredRecord(v); rec != "" {
		t.Errorf("record = %q, want empty", rec)
	}
	if w := FilteredWarning("t-x", v); w != "" {
		t.Errorf("warning = %q, want empty", w)
	}
}

// TestFilteredClaimReadsTheEvidenceSectionUnion: the motivating report states
// its readings under `## Verification` while carrying a non-empty evidence
// section; a "first present section wins" rule would read past the defect
// (delegated.go's measured correction). Both spellings are pinned here.
func TestFilteredClaimReadsTheEvidenceSectionUnion(t *testing.T) {
	body := "# Task Report: x\n\n## Completed\n- y\n\n" + VerificationHeading() + "\n- " +
		filteredClaimMeasuredSentence + "\n\n" + SectionEvidence + "\n- the red proof: `go test ./internal/tools/` → FAIL exit 1\n"
	v := FilteredClaimsIn(body)
	if len(v.Claims) != 1 {
		t.Fatalf("claims = %d, want 1 — the union must reach the Verification sentence:\n%s", len(v.Claims), v.Section)
	}
	if !strings.Contains(v.Section, VerificationHeading()) || !strings.Contains(v.Section, SectionEvidence) {
		t.Errorf("section = %q, want both headings named", v.Section)
	}
}

// TestFilteredClaimAdjudicatesTheWholeBodyWithoutSections: deleting the headings
// must not dodge the check (the delegated.go precedent).
func TestFilteredClaimAdjudicatesTheWholeBodyWithoutSections(t *testing.T) {
	v := FilteredClaimsIn("- " + filteredClaimMeasuredSentence + "\n")
	if len(v.Claims) != 1 {
		t.Fatalf("claims = %d, want 1 on a sectionless body", len(v.Claims))
	}
	if v.Section != "" {
		t.Errorf("section = %q, want \"\" for a body with no section", v.Section)
	}
}

// TestFilteredClaimTruncatesALongQuotedSentence: a bullet can be a paragraph and
// a stamp has to stay a stamp.
func TestFilteredClaimTruncatesALongQuotedSentence(t *testing.T) {
	long := "`go test -run 'A|B' ./internal/tools/` → `ok` " + strings.Repeat("padding ", 40)
	v := FilteredClaimsIn(filteredClaimBody(long))
	if len(v.Claims) != 1 {
		t.Fatalf("claims = %d, want 1", len(v.Claims))
	}
	if n := len([]rune(v.Claims[0].Sentence)); n > filteredClaimSentenceMaxRunes+len(filteredClaimEllipsis) {
		t.Errorf("quoted sentence is %d runes, want it bounded", n)
	}
	if !strings.HasSuffix(v.Claims[0].Sentence, filteredClaimEllipsis) {
		t.Errorf("a truncated sentence must be marked: %q", v.Claims[0].Sentence)
	}
}

// containsString reports whether xs holds want.
func containsString(xs []string, want string) bool {
	for _, x := range xs {
		if x == want {
			return true
		}
	}
	return false
}
