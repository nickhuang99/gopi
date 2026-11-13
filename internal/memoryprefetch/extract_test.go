package memoryprefetch

import (
	"errors"
	"strings"
	"testing"
	"unicode/utf8"
)

// TestExtract_ShortInputYieldsNoCandidates pins the floor of the whole feature:
// two and three character fragments are not retrieval terms.
func TestExtract_ShortInputYieldsNoCandidates(t *testing.T) {
	set, err := ExtractCandidates("ab cd ef", Config{})
	if err != nil {
		t.Fatalf("extract: %v", err)
	}
	if set.HitCount() != 0 {
		t.Fatalf("hits = %d (%v), want 0 — words below %d runes are not candidates",
			set.HitCount(), set.TermsText(), minCandidateRunes)
	}
	if !hasDegradation(set, DegradationNoUsableTerms) {
		t.Fatalf("an empty candidate set must record %s, got %v",
			DegradationNoUsableTerms, set.Degradations)
	}
}

// TestExtract_LatinWordsAndStopwords pins word extraction: lower-cased, deduped,
// stop words excluded, short words excluded.
func TestExtract_LatinWordsAndStopwords(t *testing.T) {
	set, err := ExtractCandidates("Memory the MEMORY leak of token-free prefetch", Config{})
	if err != nil {
		t.Fatalf("extract: %v", err)
	}
	got := set.TermsText()
	want := []string{"memory", "leak", "token", "free", "prefetch"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("terms = %v, want %v (lower-cased, deduped, stop words dropped)", got, want)
	}
	for _, c := range set.Terms {
		if c.Source != FieldTyped {
			t.Fatalf("candidate %q source = %q, want %q", c.Text, c.Source, FieldTyped)
		}
		if c.Kind != KindLatin {
			t.Fatalf("candidate %q kind = %q, want %q", c.Text, c.Kind, KindLatin)
		}
		if c.Runes != len([]rune(c.Text)) {
			t.Fatalf("candidate %q records Runes=%d, want %d", c.Text, c.Runes, len([]rune(c.Text)))
		}
	}
}

// TestExtract_CJKSegmentedNotBigramFragments is THE regression this task exists
// for (task 01a115e6): a CJK run must be cut by the project's segmenter into
// WORDS, and the 2-rune sliding window must never produce a term on the ordinary
// path.
//
// It pins the measured failure, not a shape: before the segmenter was wired in,
// `目前我们的 memory 预取实现有问题` yielded 目前 / 前我 / 们的 … — every overlapping
// bigram of the run — so the retrieval key carried 前我 and 们的, which are not
// words in any language. Those two literals are the assertion: a key that still
// contains them is the old mechanism (red), and one that contains the segmenter's
// own boundary (目前 + 我们的) is the new one (green).
func TestExtract_CJKSegmentedNotBigramFragments(t *testing.T) {
	const typed = "目前我们的 memory 预取实现有问题"
	set, err := ExtractCandidates(typed, Config{MaxKeywords: 32})
	if err != nil {
		t.Fatalf("extract: %v", err)
	}
	got := set.TermsText()
	// NEVER — the fragments the sliding window manufactured.
	for _, banned := range []string{"前我", "们的", "取实", "现的"} {
		if contains(got, banned) {
			t.Errorf("term %q is a %d-rune sliding-window fragment, not a word — the CJK pass "+
				"is not using the segmenter; terms=%v", banned, cjkBigramRunes, got)
		}
	}
	// EVERY CJK candidate is the segmenter's own output, and it says so.
	for _, c := range set.Terms {
		if c.Kind == KindCJKBigram || c.Kind == KindCJKRun {
			t.Errorf("candidate %q kind = %q — the fallback kinds are produced ONLY when the "+
				"segmenter could not answer (they were produced with no segmenter error)",
				c.Text, c.Kind)
		}
		if c.Kind == KindCJKWord && !contains(got, c.Text) {
			t.Errorf("segmented word %q missing from the terms", c.Text)
		}
	}
	if !contains(got, "目前") {
		t.Errorf("terms %v do not carry the segmenter's own boundary 目前 for %q", got, typed)
	}
	// No fallback ran, so the segmenter-gap degradation must NOT be present: a
	// code that fires on the ordinary path would make the fallback unattributable
	// (Rule 15).
	if _, ok := findDegradation(set, DegradationCJKTokenizerGap); ok {
		t.Errorf("the segmenter answered, yet %s was recorded: %v",
			DegradationCJKTokenizerGap, set.Degradations)
	}
}

// TestExtract_CJKFallbackIsExplicit is the CJK acceptance case: a CJK run is
// extracted through the bigram FALLBACK, and the set says so in a named
// degradation rather than pretending the terms are ordinary words.
//
// The fallback is now reachable only ONE way — the segmenter refusing the input —
// so this test forces that condition through the package's own seam
// (segmenterWords) instead of asserting the shape indirectly. Rule 19's control
// flow is unaffected: the seam is a package-level function value the production
// code calls, replaced for the duration of one test and restored by it (Rule 17's
// reviewed per-call pair — the point here is that the CALLER's own path runs with
// a failing segmenter, which no environment can express).
func TestExtract_CJKFallbackIsExplicit(t *testing.T) {
	const typed = "打字中后台记忆预取"
	restore := stubSegmenterFailing(t)
	defer restore()

	set, err := ExtractCandidates(typed, Config{})
	if err != nil {
		t.Fatalf("extract: %v", err)
	}
	if set.HitCount() == 0 {
		t.Fatalf("CJK input %q yielded no candidates at all — the fallback path did not run", typed)
	}
	for _, c := range set.Terms {
		if c.Kind != KindCJKBigram && c.Kind != KindCJKRun {
			t.Fatalf("CJK candidate %q kind = %q, want %q or %q",
				c.Text, c.Kind, KindCJKBigram, KindCJKRun)
		}
	}
	d, ok := findDegradation(set, DegradationCJKTokenizerGap)
	if !ok {
		t.Fatalf("CJK extraction must record %s; got %v", DegradationCJKTokenizerGap, set.Degradations)
	}
	if !strings.Contains(d.Detail, "bigram fallback") {
		t.Fatalf("the degradation detail must name the fallback it used, got %q", d.Detail)
	}
	if !strings.Contains(d.Detail, cjkSegmenterName) {
		t.Fatalf("the degradation must name the segmenter that refused, got %q", d.Detail)
	}
}

// TestExtract_CJKNoDataLossOnTinyRun pins the no-silent-drop half of the CJK
// path: a single CJK rune cannot be extracted, and that loss is NAMED — on the
// ORDINARY path, where the segmenter answers and the run is simply too short to
// be a retrieval term.
func TestExtract_CJKNoDataLossOnTinyRun(t *testing.T) {
	set, err := ExtractCandidates("中 x", Config{})
	if err != nil {
		t.Fatalf("extract: %v", err)
	}
	if _, ok := findDegradation(set, DegradationCJKBelowFloor); !ok {
		t.Fatalf("a CJK run too short to extract must still be reported; got %v", set.Degradations)
	}
	if _, ok := findDegradation(set, DegradationNoUsableTerms); !ok {
		t.Fatalf("an empty candidate set must record %s; got %v",
			DegradationNoUsableTerms, set.Degradations)
	}
}

// TestExtract_CJKNoDataLossOnTinyRunWhenTheSegmenterIsDown is the SAME loss on
// the degraded path: with the segmenter refusing, the fallback's own short-run
// count is what carries it. Both halves are pinned because the two passes count
// different things and only one of them is reached in production.
func TestExtract_CJKNoDataLossOnTinyRunWhenTheSegmenterIsDown(t *testing.T) {
	restore := stubSegmenterFailing(t)
	defer restore()

	// The input carries BOTH halves: a one-rune run (which no window can use)
	// and a run long enough that the segmenter is actually consulted — without
	// the second, segmentCJKWords never calls the segmenter and there is no
	// refusal to degrade from (the honest reason this fixture is what it is).
	set, err := ExtractCandidates("中 打字中后台记忆预取", Config{MaxKeywords: 32})
	if err != nil {
		t.Fatalf("extract: %v", err)
	}
	d, ok := findDegradation(set, DegradationCJKTokenizerGap)
	if !ok {
		t.Fatalf("the fallback must report itself; got %v", set.Degradations)
	}
	if !strings.Contains(d.Detail, "shorter than the fallback window") {
		t.Errorf("the fallback's detail must carry the short-run loss, got %q", d.Detail)
	}
	if _, ok := findDegradation(set, DegradationCJKBelowFloor); ok {
		t.Errorf("the fallback's own detail already carries the loss; a second code would "+
			"report the same fact twice: %v", set.Degradations)
	}
}

// TestExtract_CJKShortSegmentedWordIsNamed pins the OTHER loss the segmenter path
// can produce: the segmenter answers, and one of its words is below the retrieval
// floor. It is dropped — and NAMED, under its own code, so a reader can tell it
// from "the segmenter refused" (Rule 15).
//
// The fixture is a run the segmenter really does split into a single-character
// word (`打字中后台记忆预取` → … + 中 + …): the assertion is that the drop is
// reported, not merely absent.
func TestExtract_CJKShortSegmentedWordIsNamed(t *testing.T) {
	set, err := ExtractCandidates("打字中后台记忆预取", Config{MaxKeywords: 32})
	if err != nil {
		t.Fatalf("extract: %v", err)
	}
	if set.HitCount() == 0 {
		t.Fatalf("terms are empty — the fixture proves nothing")
	}
	for _, c := range set.Terms {
		if utf8.RuneCountInString(c.Text) < minCJKCandidateRunes {
			t.Errorf("candidate %q is below minCJKCandidateRunes=%d and should have been dropped "+
				"with the %s degradation", c.Text, minCJKCandidateRunes, DegradationCJKBelowFloor)
		}
	}
	d, ok := findDegradation(set, DegradationCJKBelowFloor)
	if !ok {
		t.Fatalf("this fixture's run contains a single-character segmenter word; the drop must be "+
			"recorded as %s, got %v", DegradationCJKBelowFloor, set.Degradations)
	}
	if !strings.Contains(d.Detail, "below the retrieval floor") {
		t.Errorf("the below-floor degradation must say what was lost, got %q", d.Detail)
	}
	if _, ok := findDegradation(set, DegradationCJKTokenizerGap); ok {
		t.Errorf("%s must not fire when the segmenter answered: %v",
			DegradationCJKTokenizerGap, set.Degradations)
	}
}

// TestExtract_MixedLatinAndCJK pins that the two paths do not eat each other:
// a mixed line yields both, and the CJK words are the segmenter's.
func TestExtract_MixedLatinAndCJK(t *testing.T) {
	set, err := ExtractCandidates("排查 memory leak 问题", Config{})
	if err != nil {
		t.Fatalf("extract: %v", err)
	}
	texts := set.TermsText()
	for _, want := range []string{"memory", "leak", "排查", "问题"} {
		if !contains(texts, want) {
			t.Fatalf("terms %v missing %q — the two extraction paths must coexist", texts, want)
		}
	}
}

// TestExtract_LongCJKCandidateIsClipped guards MaxCJKCandidateRunes, which now
// governs the FALLBACK's whole-run promotion only: with the segmenter unavailable
// a very long run is not promoted whole, only its bigrams survive. The ordinary
// (segmenter) path has no such cap — its words are already the index's own
// granularity.
func TestExtract_LongCJKCandidateIsClipped(t *testing.T) {
	restore := stubSegmenterFailing(t)
	defer restore()

	cfg := Config{MaxCJKCandidateRunes: 4, MaxKeywords: 32}
	set, err := ExtractCandidates("打字中后台记忆预取", cfg)
	if err != nil {
		t.Fatalf("extract: %v", err)
	}
	for _, c := range set.Terms {
		if c.Kind == KindCJKRun {
			t.Fatalf("run %q was promoted whole, want bigram fallback only (MaxCJKCandidateRunes=%d)",
				c.Text, cfg.MaxCJKCandidateRunes)
		}
	}
	if set.HitCount() == 0 {
		t.Fatal("the bigram fallback must still produce candidates")
	}
}

// TestExtract_KeywordCapIsNamed pins that the cap is a NAMED degradation, never a
// silent truncation.
func TestExtract_KeywordCapIsNamed(t *testing.T) {
	cfg := Config{MaxKeywords: 3}
	set, err := ExtractCandidates("alpha bravo charlie delta echo", cfg)
	if err != nil {
		t.Fatalf("extract: %v", err)
	}
	if set.HitCount() != 3 {
		t.Fatalf("hits = %d (%v), want 3 (MaxKeywords)", set.HitCount(), set.TermsText())
	}
	if _, ok := findDegradation(set, DegradationKeywordCapReached); !ok {
		t.Fatalf("hitting the cap must record %s; got %v", DegradationKeywordCapReached, set.Degradations)
	}
}

// TestExtract_PureAndDeterministic pins that extraction is a function: the same
// input yields the same terms in the same order, twice.
func TestExtract_PureAndDeterministic(t *testing.T) {
	const in = "prefetch memory while typing 打字 中台"
	first, err := ExtractCandidates(in, Config{})
	if err != nil {
		t.Fatalf("extract: %v", err)
	}
	second, err := ExtractCandidates(in, Config{})
	if err != nil {
		t.Fatalf("extract: %v", err)
	}
	if strings.Join(first.TermsText(), ",") != strings.Join(second.TermsText(), ",") {
		t.Fatalf("extraction is not deterministic: %v vs %v", first.TermsText(), second.TermsText())
	}
}

// TestConfig_ZeroValueTakesDefaults pins the Config contract: a hand-built zero
// Config is legal and equals DefaultConfig after defaults are applied.
func TestConfig_ZeroValueTakesDefaults(t *testing.T) {
	if got, want := (Config{}).withDefaults(), DefaultConfig(); got != want {
		t.Fatalf("zero Config defaults = %+v, want %+v", got, want)
	}
	if err := (Config{}).Validate(); err != nil {
		t.Fatalf("the zero Config must be valid, got %v", err)
	}
	if err := (Config{MinInputRunes: 100, WeakInputRunes: 10}).Validate(); err == nil {
		t.Fatal("WeakInputRunes below MinInputRunes must be refused")
	}
}

// helpers.

// stubSegmenterFailing replaces the segmenter call with one that refuses the
// input, and returns the restore function. It is the fixture for the package's
// ONE degraded path, and it is honest about what it does: the REAL cjkseg is not
// being tested here (it has its own tests and its dictionary loads in this very
// binary); what is under test is the CALLER's behaviour when the segmenter
// cannot answer.
//
// Rule 17: the value is restored by the caller's deferred restore in the same
// test that set it — no environment is touched, and no sibling test can observe
// it (a t.Parallel() test in this file would, so this file keeps none).
func stubSegmenterFailing(t *testing.T) func() {
	t.Helper()
	prev := segmenterWords
	segmenterWords = func(string) ([]string, error) {
		return nil, errSegmenterRefusedForTest
	}
	return func() { segmenterWords = prev }
}

// errSegmenterRefusedForTest is the stand-in refusal. It names itself so a
// failure message that quotes it cannot be mistaken for a real cjkseg error.
var errSegmenterRefusedForTest = errors.New("test fixture: the segmenter refused this input")

func hasDegradation(set CandidateSet, code DegradationCode) bool {
	_, ok := findDegradation(set, code)
	return ok
}

func findDegradation(set CandidateSet, code DegradationCode) (Degradation, bool) {
	for _, d := range set.Degradations {
		if d.Code == code {
			return d, true
		}
	}
	return Degradation{}, false
}

func contains(list []string, want string) bool {
	for _, s := range list {
		if s == want {
			return true
		}
	}
	return false
}
