package memoryprefetch

import (
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/nickhuang99/gopi/internal/cjkseg"
)

// CandidateKind names HOW a candidate term was obtained. The distinction is the
// honest part of this file: a Latin word is a word, a CJK word is a word the
// project's OWN segmenter produced, and the two fallback kinds say that the
// segmenter did not produce the term.
type CandidateKind string

const (
	// KindLatin is a word of the typed input, lower-cased, >= minCandidateRunes
	// runes and not a stop word.
	KindLatin CandidateKind = "latin-word"
	// KindCJKWord is a word the segmenter (internal/cjkseg — gse over its
	// embedded zh_s dictionary) returned for a CJK run. It is the CJK term this
	// package WANTS: it is the same granularity the memory index is written at
	// (internal/tools joins cjkseg.Words' output into the indexed token column),
	// so index and query speak about the same units.
	KindCJKWord CandidateKind = "cjk-word"
	// KindCJKRun is a whole run of CJK runes promoted as ONE candidate — the
	// fallback's long-run shape. Like KindCJKBigram it is a stand-in rather than
	// a tokenizer, and it is only produced when the segmenter could not answer;
	// its use is recorded as DegradationCJKTokenizerGap.
	KindCJKRun CandidateKind = "cjk-run"
	// KindCJKBigram is a 2-rune window inside a CJK run — the fallback's other
	// shape, and the one this file's segmenter wiring exists to retire: every
	// overlapping bigram of a run, most of which are not words in any language.
	KindCJKBigram CandidateKind = "cjk-bigram-fallback"
)

// SourceField names which part of the typed input a candidate came from. Every
// candidate carries it so the caller (and the rendered section) can say where a
// retrieval term originated; "typed" is the honest default here, since this
// package only ever sees typed text.
type SourceField string

const (
	// FieldTyped is the operator's typed input.
	FieldTyped SourceField = "typed-input"
)

// DegradationCode is a NAMED degradation of the extraction: something was lost
// or approximated, and the caller is told which. Rule 15 — a silent loss is the
// defect, so every path that drops something records one of these.
type DegradationCode string

const (
	// DegradationCJKTokenizerGap records that CJK candidates came from the
	// bigram/whole-run fallback because the SEGMENTER could not answer
	// (internal/cjkseg's cjkseg.Words returned an error — a dictionary that
	// failed to load, in practice). It is NOT recorded on the ordinary path: a
	// run the segmenter did tokenize is a word, not a gap. The detail carries
	// the counts, including the runs that were too short to enter the fallback
	// window.
	DegradationCJKTokenizerGap DegradationCode = "cjk-tokenizer-gap"
	// DegradationNotesLegFailed records that the SECOND retrieval leg — the
	// round-level note index (notes.go) — could not be consulted at all, with
	// the reason. The memory leg's own result is unaffected by it (the caller
	// renders what it has), but the two facts must never be confused: "no notes
	// matched" and "the note index could not be read" send a reader in opposite
	// directions, and only the first is a reason to stop looking (Rule 15).
	DegradationNotesLegFailed DegradationCode = "notes-leg-failed"
	// DegradationNoteSelfEcho records that a note hit was NOT injected because
	// the SAME run that was asking had recorded it — text already in the
	// reader's own context (see NoteHits). It is recorded rather than dropped in
	// silence so the suppression is countable: a leg whose recall is dominated by
	// echoes says so.
	DegradationNoteSelfEcho DegradationCode = "note-self-echo"
	// DegradationNoteHitEmpty records a note row that carries no text: it is
	// dropped by name, never rendered as an anonymous line.
	DegradationNoteHitEmpty DegradationCode = "note-hit-empty"
	// DegradationKeywordCapReached records that the candidate set hit
	// MaxKeywords and further terms were not extracted.
	DegradationKeywordCapReached DegradationCode = "keyword-cap-reached"
	// DegradationNoUsableTerms records that extraction yielded nothing at all,
	// so the caller can stop without guessing why.
	DegradationNoUsableTerms DegradationCode = "no-usable-terms"
	// DegradationBudgetTooSmall records a configuration whose budget cannot
	// hold even the smallest renderable hit.
	DegradationBudgetTooSmall DegradationCode = "budget-too-small-for-one-hit"
	// DegradationAllHitsDropped records that every retrieved hit was dropped
	// inside the budget, so the section carries a header and nothing else.
	DegradationAllHitsDropped DegradationCode = "all-hits-dropped"
	// DegradationTurnPayloadClipped records that a referenced payload entered a
	// turn's retrieval key clipped to MaxTurnPayloadRunes — the loss a
	// whole-block paste or a delivered file would otherwise hide (turn.go).
	DegradationTurnPayloadClipped DegradationCode = "turn-payload-clipped"
	// DegradationCJKBelowFloor records that CJK material was dropped for being
	// too SHORT to retrieve on, in the one case where the segmenter DID answer:
	// a run shorter than its smallest window, or a word it returned below
	// minCJKCandidateRunes. It is a second, narrower code than
	// DegradationCJKTokenizerGap on purpose: "the segmenter could not answer"
	// and "the segmenter answered, and this fragment is below the floor" are
	// different facts, and a reader fixing one must not have to guess which
	// happened (Rule 15).
	DegradationCJKBelowFloor DegradationCode = "cjk-below-retrieval-floor"
)

// Degradation is one recorded loss: a code a caller may switch on, and a detail
// string for a human (and for the rendered section).
type Degradation struct {
	Code   DegradationCode
	Detail string
}

// Candidate is one retrieval term.
type Candidate struct {
	// Text is the term as it will be handed to retrieval.
	Text string
	// Source is which part of the input it came from.
	Source SourceField
	// Kind is how it was obtained (see CandidateKind).
	Kind CandidateKind
	// Runes is len(Text) in runes — recorded once here so no caller re-counts
	// it with a different unit.
	Runes int
}

// CandidateSet is the extraction result: the terms, plus every degradation that
// happened while producing them.
type CandidateSet struct {
	// InputRunes is the rune length of the input the set was extracted from.
	InputRunes int
	// Terms are the extracted candidates, in a deterministic order.
	Terms []Candidate
	// Degradations are the named losses, in the order they were recorded.
	Degradations []Degradation
}

// HitCount is the gate's PRIMARY signal: how many distinct candidate terms the
// input yielded. Niladic accessor, named for the value (Rule 20).
func (s CandidateSet) HitCount() int { return len(s.Terms) }

// Degraded reports whether extraction approximated or dropped anything.
func (s CandidateSet) Degraded() bool { return len(s.Degradations) > 0 }

// Terms returns the candidate texts, in order — the shape the state machine's
// delta is computed over.
func (s CandidateSet) TermsText() []string {
	out := make([]string, 0, len(s.Terms))
	for _, t := range s.Terms {
		out = append(out, t.Text)
	}
	return out
}

// extraction vocabulary — spelled once (Rule 1): the words carrying no retrieval
// signal in English and in Chinese. Kept deliberately small: this is a
// noise-reduction list, not a linguistic resource.
const (
	stopwordList = "the,a,an,and,or,of,to,in,is,are,was,were,it,its,on,at,by,be,as,for,with,that,this,from,not,but,if,then,than,so,we,you,i,he,she,they,them,us"
	// cjkStopwordList holds single characters and short function words that
	// carry no retrieval signal in Chinese.
	cjkStopwordList = "的,了,是,在,和,就,都,而,及,与,着,或,你,我,他,她,它,们,这,那,有,不,也,还,把,被,让,给,到,从,对,为,以,之,其,此,我们,你们,他们,什么,怎么,为什么,可以,一个"
	// minCandidateRunes is the shortest Latin word worth retrieving on; shorter
	// fragments are noise in a bag-of-words index.
	minCandidateRunes = 3
	// cjkBigramRunes is the FALLBACK window: two runes. It is only reached when
	// the segmenter could not answer (see segmentCJKWords), and its use is
	// always recorded as DegradationCJKTokenizerGap.
	cjkBigramRunes = 2
	// minCJKCandidateRunes is the shortest SEGMENTED CJK word worth retrieving
	// on. It is the same floor the fallback window implies, applied to a
	// segmenter's output: a single Chinese character ("的", "中") carries too
	// little retrieval signal to be a query term, and the drop is counted and
	// reported rather than silent (Rule 15).
	minCJKCandidateRunes = 2
)

// runCensus is what one pass over the input's CJK runs found: the runs long
// enough to extract from, and the runs too short to enter any window. It is a
// named struct rather than an extra return value because BOTH passes — the
// segmenter's and the fallback's — need the same census, and a second signature
// would be a second place to get it wrong.
type runCensus struct {
	// runs are the runs long enough to extract from (>= cjkBigramRunes).
	runs []string
	// shortRuns counts the runs too short for any window — a single CJK
	// character disappears from the candidate set, and Rule 15 forbids dropping
	// it without saying so.
	shortRuns int
}

// takeCensus is the ONE reading of the input's CJK runs; cjkRuns below owns the
// counting itself.
func takeCensus(input string) runCensus {
	runs, shortRuns := cjkRuns(input)
	return runCensus{runs: runs, shortRuns: shortRuns}
}

// cjkSegmenterName names the ONE segmenter this package delegates CJK word
// boundary detection to. It exists so the degradation text (and any report)
// names the mechanism it fell back FROM instead of describing it from memory
// (Rule 38: a figure names its generator) — and so the name is not re-typed
// into the two places that mention it.
const cjkSegmenterName = "internal/cjkseg"

var stopwords = newTokenSet(stopwordList)

var cjkStopwords = newTokenSet(cjkStopwordList)

// newTokenSet parses a comma-separated vocabulary into a set.
func newTokenSet(list string) map[string]bool {
	set := make(map[string]bool)
	for _, tok := range strings.Split(list, ",") {
		if tok = strings.TrimSpace(tok); tok != "" {
			set[tok] = true
		}
	}
	return set
}

// ExtractCandidates turns typed input into retrieval candidates.
//
// It is pure: same input, same Config, same result. It records a Degradation for
// every approximation or drop instead of returning a quietly shorter list.
func ExtractCandidates(input string, cfg Config) (CandidateSet, error) {
	cfg = cfg.withDefaults()
	if err := cfg.Validate(); err != nil {
		return CandidateSet{}, fmt.Errorf("extract candidates: %w", err)
	}
	set := CandidateSet{InputRunes: utf8.RuneCountInString(input)}
	seen := make(map[string]bool)
	capped := false

	// add records one candidate. The duplicate check runs BEFORE the cap check
	// so a repeated word never masquerades as a truncation.
	add := addFn(func(text string, kind CandidateKind) {
		if seen[text] {
			return
		}
		if len(set.Terms) >= cfg.MaxKeywords {
			capped = true
			return
		}
		seen[text] = true
		set.Terms = append(set.Terms, Candidate{
			Text:   text,
			Source: FieldTyped,
			Kind:   kind,
			Runes:  utf8.RuneCountInString(text),
		})
	})

	// Pass 1 — Latin words.
	for _, w := range splitLatinWords(input) {
		if utf8.RuneCountInString(w) < minCandidateRunes {
			continue
		}
		if stopwords[w] {
			continue
		}
		add(w, KindLatin)
	}

	// Pass 2 — CJK, through the project's ONE segmenter.
	//
	// The terms this produces are the granularity the memory index is WRITTEN at
	// (internal/tools joins cjkseg.Words' output into the indexed token column),
	// so a query of words looks for the same units the index holds. The
	// 2-rune sliding window this replaced could not: it emitted every
	// overlapping bigram of a run, so `目前我们的` became 目前/前我/们的 — the
	// fragments 前我 and 们的 are not words in any language and matched the
	// index by accident or not at all, diluting the retrieval key.
	dropped, segmentErr := segmentCJKWords(input, add, &set, cfg)
	if segmentErr != nil {
		// The ONE fallback path. It runs only when the segmenter could not
		// answer, and it says so in a named degradation — never silently
		// (Rule 15).
		fallbackCJKRuns(input, add, &set, cfg, segmentErr)
	} else if dropped > 0 {
		set.Degradations = append(set.Degradations, Degradation{
			Code: DegradationCJKBelowFloor,
			Detail: fmt.Sprintf(
				"%d CJK fragment(s) were below the retrieval floor (%d runes) and were not extracted as candidates",
				dropped, minCJKCandidateRunes)})
	}
	if capped {
		set.Degradations = append(set.Degradations, Degradation{
			Code:   DegradationKeywordCapReached,
			Detail: fmt.Sprintf("candidate set capped at MaxKeywords=%d; further terms were not extracted", cfg.MaxKeywords),
		})
	}
	if len(set.Terms) == 0 {
		set.Degradations = append(set.Degradations, Degradation{
			Code:   DegradationNoUsableTerms,
			Detail: "no candidate term passed the extraction rules (too short, all stop words, or no word-like rune)",
		})
	}
	return set, nil
}

// addFn records one candidate into the set being extracted. It is a named type
// rather than an inline func value so the segmenter and the fallback can be
// handed the SAME recorder (the one the cap and the dedup live in) without the
// signature being re-spelled at three call sites.
type addFn func(text string, kind CandidateKind)

// segmenterWords is the CALL the CJK pass makes for word boundaries, held as a
// package-level function value so a test can drive the ONE degraded path — a
// segmenter that refuses the input — which nothing in this process can otherwise
// provoke (the dictionary is embedded in the binary; loading it is the only way
// cjkseg.Words fails). Production never reassigns it: the value IS
// cjkseg.Words, and the fallback's existence is only meaningful against a real
// refusal.
//
// It is the package's only mutable global, and deliberately so: the alternative —
// a Config field carrying the function — would put a callable into the
// configuration surface that a caller could use to switch the segmenter OFF,
// which is exactly the silent degradation Rule 15 forbids.
var segmenterWords = cjkseg.Words

// segmentCJKWords is the CJK pass: every CJK run in the input is segmented by
// the project's ONE segmenter (internal/cjkseg — gse over its embedded zh_s
// dictionary) and each returned word becomes a KindCJKWord candidate.
//
// This is delegation, not a second tokenizer (Rule 8): the words are whatever
// cjkseg.Words answers, and the index side asks the same function, so query and
// index cannot drift apart in granularity.
//
// It COUNTS what it could not use (the returned int: input runs below the
// fallback window, and segmenter words below the retrieval floor) and returns the
// segmenter's error verbatim when there is one — the caller turns that error into
// the named fallback (fallbackCJKRuns) and turns the count into a named
// degradation. What it does NOT do is hide either: a fragment that was too short
// and a segmenter that refused are different outcomes and are reported
// differently (Rule 15).
func segmentCJKWords(input string, add addFn, set *CandidateSet, cfg Config) (dropped int, err error) {
	c := takeCensus(input)
	if len(c.runs) == 0 {
		return c.shortRuns, nil
	}
	dropped = c.shortRuns
	for _, run := range c.runs {
		words, werr := segmenterWords(run)
		if werr != nil {
			// The error travels with the run's own count so the fallback's
			// detail names both halves; the caller returns immediately after.
			return 0, werr
		}
		for _, w := range words {
			if cjkStopwords[w] {
				continue
			}
			if utf8.RuneCountInString(w) < minCJKCandidateRunes {
				dropped++
				continue
			}
			add(w, KindCJKWord)
		}
	}
	return dropped, nil
}

// fallbackCJKRuns is the DEGRADED CJK pass: it runs only when segmentCJKWords
// reported that the segmenter could not answer, and it reproduces what this file
// did before the segmenter was wired in — a whole run promoted while it fits
// MaxCJKCandidateRunes, plus every 2-rune window inside it.
//
// Its use is the fact recorded as DegradationCJKTokenizerGap, so a set built
// this way says which mechanism produced its terms rather than looking like an
// ordinary one (Rule 15).
func fallbackCJKRuns(input string, add addFn, set *CandidateSet, cfg Config, segErr error) {
	runs, shortRuns := cjkRuns(input)
	for _, run := range runs {
		if cjkStopwords[run] {
			continue
		}
		if utf8.RuneCountInString(run) <= cfg.MaxCJKCandidateRunes {
			add(run, KindCJKRun)
		}
		runes := []rune(run)
		for i := 0; i+cjkBigramRunes <= len(runes); i++ {
			gram := string(runes[i : i+cjkBigramRunes])
			if cjkStopwords[gram] {
				continue
			}
			add(gram, KindCJKBigram)
		}
	}
	set.Degradations = append(set.Degradations, Degradation{
		Code: DegradationCJKTokenizerGap,
		Detail: fmt.Sprintf(
			"%d CJK run(s) used the %d-rune bigram fallback because the segmenter (%s) refused the input (%v); %d run(s) shorter than the fallback window were not extracted",
			len(runs), cjkBigramRunes, cjkSegmenterName, segErr, shortRuns),
	})
}

// splitLatinWords lower-cases and splits the input on every rune that is not a
// letter or digit. A rune that is CJK is a letter to unicode.IsLetter, so the
// CJK runs are removed first — otherwise a mixed line would glue a Chinese run
// onto the Latin word beside it.
func splitLatinWords(input string) []string {
	var words []string
	var b strings.Builder
	for _, r := range input {
		if isCJKRune(r) {
			flushWord(&b, &words)
			continue
		}
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			b.WriteRune(unicode.ToLower(r))
			continue
		}
		flushWord(&b, &words)
	}
	flushWord(&b, &words)
	return words
}

// flushWord ends the word being accumulated, if any.
func flushWord(b *strings.Builder, words *[]string) {
	if b.Len() == 0 {
		return
	}
	*words = append(*words, b.String())
	b.Reset()
}

// cjkRuns returns the maximal runs of CJK runes, plus the number of runs that
// were too short to be extracted at all. The second return is not decoration: a
// single CJK character disappears from the candidate set, and Rule 15 forbids
// dropping it without saying so.
func cjkRuns(input string) (runs []string, shortRuns int) {
	var b strings.Builder
	for _, r := range input {
		if isCJKRune(r) {
			b.WriteRune(r)
			continue
		}
		if b.Len() > 0 {
			appendRun(b.String(), &runs, &shortRuns)
			b.Reset()
		}
	}
	if b.Len() > 0 {
		appendRun(b.String(), &runs, &shortRuns)
	}
	return runs, shortRuns
}

// appendRun classifies one candidate CJK run.
func appendRun(run string, runs *[]string, shortRuns *int) {
	if utf8.RuneCountInString(run) < cjkBigramRunes {
		*shortRuns++
		return
	}
	*runs = append(*runs, run)
}

// CJK ranges (Han, Hiragana, Katakana, Hangul, CJK punctuation-free blocks).
// These are literal code points — a range IS its value, so Rule 1's constants
// requirement is satisfied by naming the predicate, not by naming each number.
func isCJKRune(r rune) bool {
	switch {
	case r >= 0x4E00 && r <= 0x9FFF: // CJK Unified Ideographs
		return true
	case r >= 0x3400 && r <= 0x4DBF: // CJK Extension A
		return true
	case r >= 0xF900 && r <= 0xFAFF: // CJK Compatibility Ideographs
		return true
	case r >= 0x3040 && r <= 0x30FF: // Hiragana + Katakana
		return true
	case r >= 0xAC00 && r <= 0xD7AF: // Hangul syllables
		return true
	}
	return false
}
