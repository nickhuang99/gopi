package main

// chat_prefetch_shared_test.go — the PREFIX-KEY shape, pinned as the DESIGN it
// measurably is (task 01a1163c; the sibling 01a11601 removed the
// verbatim-equal duplicate and is cherry-picked here).
//
// THE SECOND SENTENCE OF THE SAME RULE, and it is the repo's own: a key that is
// a STRICT PREFIX of another is a DIFFERENT question. `newKeywordCount`'s
// doc (internal/memoryprefetch/session.go) states it about exactly this pair —
// "`alpha beta` against `alpha beta gamma` is a different one (1 new keyword)
// even though it contains the first completely" — and both of the typing path's
// gates are built on it: `Claim` re-fires only for new keywords, and `Take`
// REFUSES a section whose query is missing a keyword of the line being sealed
// ("A line the operator kept typing on is a different question, and the honest
// answer for it is 'no section', not a slightly older one").
//
// SO WHY IS THE PAUSE'S LOOKUP STILL PAID FOR, and why is it not a duplicate of
// the turn's? Because the two questions were each the WHOLE line when it was
// asked: the pause IS the trigger (Observe's debounce is "the line has not
// changed for DefaultDebounceMS"), so at that instant the prefix is the entire
// input the operator typed — a complete, answerable question about the sealed
// text so far. Whether the line will grow is unknowable then, and the typing
// path must not guess: the only criteria that could look ahead ("a longer
// window", "the line is still changing") are TIME criteria, and this feature
// bans them (a lookup that fires later than the pause has already missed the
// Enter). One line therefore costs one lookup for the pause and one for the
// turn, and the pause's section is refused at the seal by Take — which is why
// the premium is spent only on the case that pays off, an operator who pauses
// and then presses Enter (there the keys ARE equal and the turn SHARES the
// section: TestATypingPauseOnAPartialLineIsPaidForAtThatPause).
//
// THE QUESTION THIS FILE ANSWERS WITH A RUN, not with an opinion. The row was
// filed on the hypothesis that a typing-time retrieval of a PREFIX key (the line
// as it stood when the operator paused mid-typing) followed by the turn
// boundary's retrieval of the FULL key is the same duplicate in another shape,
// and that the end of the prefix relation should join the sibling's "share one
// section" rule.
//
// IT IS NOT THE SAME SHAPE, and the difference is measurable in two places:
//
//  1. THE TWO KEYS ARE DIFFERENT QUESTIONS, so the turn's lookup is not a
//     duplicate of the typing one: the turn's key carries terms the typing
//     section was never retrieved for. Reusing it — the widening the row
//     proposes — would hand the prompt a section that does not cover the turn's
//     own words, which is the failure the whole feature is built to prevent
//     (TestAPrefixFetchedSectionNeverServesAFullerTurn pins that refusal).
//  2. THE PREFIX LOOKUP CANNOT BE SUPPRESSED, because at the moment it runs the
//     prefix IS the whole line: the operator has stopped, and the debounce the
//     typing path obeys is exactly "the line has not changed for
//     DefaultDebounceMS". Whether the line will grow later is not knowable then,
//     and the only criterion that could look ahead — a longer time window, a
//     "still changing" flag — is a TIME criterion, which this feature's own
//     discipline bans (the sibling's row states the ban; a lookup that fires
//     later than the pause is a lookup that has already missed the Enter).
//
// SO WHAT DOES THE PREFIX LOOKUP COST? One lookup, spent as the PREMIUM on the
// case that pays off: an operator who pauses and then presses Enter is served
// from it (the turn boundary then SHARES it — TestATypingPauseOnAPartialLineIsPaidForAtThatPause
// asserts both halves). The line that grows instead replaces it, and the record
// says so: the prefix line carries no adoption event.
//
// WHAT WOULD BREAK IF THIS WERE "FIXED" BY WIDENING THE SHARING RULE is asserted
// too: a turn whose key carries one term the typing path never had in hand must
// run its OWN retrieval (TestASharedKeyNeverSuppressesADifferentQuestion).
//
// These drive the REAL seams — chatPrefetchAdoptTurn, chatPrefetchAdopt,
// chatPrefetchTurnTerms, Session.Claim/fetch/Take — not copies of their logic,
// so a regression fails here rather than only on the next live run.

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/nick/gopi/internal/config"
	"github.com/nick/gopi/internal/memoryprefetch"
)

// The typing-prefix fixture, named (Rule 1) because the claims below are made
// about THESE strings: the pause happens after the words below, the turn carries
// the whole sentence.
const (
	// sharedTypedPrefix is the line as it stood when the operator paused — long
	// enough and with enough distinct terms for the typing path's own gate, and
	// a STRICT PREFIX of the sentence below. Its spelled prefix relation is
	// asserted by the test, whose subject it is.
	sharedTypedPrefix = "the notify delivered retrieval key"
	// sharedSealedLine is the line the operator pressed Enter on: the prefix
	// plus the words typed after the pause. Its key STRICTLY CONTAINS the
	// prefix's, and that is the whole reason its lookup is its own.
	sharedSealedLine = sharedTypedPrefix + " reaches the turn boundary"
	// sharedForeignLine is a turn whose key carries a term the typing path never
	// had in hand ("adoption" appears in no prefix above). It must therefore run
	// its OWN retrieval: sharing on mere overlap would silence a real question.
	sharedForeignLine = "memory prefetch adoption across a whole-block paste"
)

// sharedPrefetchRecord is one line of the run's record, decoded. The assertions
// below count OUTCOMES, which is what the acceptance measures, so they read the
// record through its own schema rather than substring-matching JSON.
type sharedPrefetchRecord struct {
	Query   string `json:"query"`
	Outcome string `json:"outcome"`
	Adopted string `json:"adopted"`
	Source  string `json:"source"`
}

// decodePrefetchRecords decodes every record line the executor wrote, failing on
// a line that is not JSON (a torn record is a defect, not a line to skip).
func decodePrefetchRecords(t *testing.T, dir string) []sharedPrefetchRecord {
	t.Helper()
	raw := readPrefetchRecord(t, dir)
	var out []sharedPrefetchRecord
	for _, line := range strings.Split(strings.TrimSpace(raw), "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		var r sharedPrefetchRecord
		if err := json.Unmarshal([]byte(line), &r); err != nil {
			t.Fatalf("record line is not JSON (%v): %s", err, line)
		}
		out = append(out, r)
	}
	return out
}

// countOutcome counts the record lines carrying one outcome.
func countOutcome(recs []sharedPrefetchRecord, outcome string) int {
	n := 0
	for _, r := range recs {
		if r.Outcome == outcome {
			n++
		}
	}
	return n
}

// queries renders the record's retrieval keys in order, for a failure message
// (and for the subset assertions, which are about the keys themselves).
func queries(recs []sharedPrefetchRecord) []string {
	var out []string
	for _, r := range recs {
		if r.Outcome == prefetchOutcomeHits {
			out = append(out, r.Query)
		}
	}
	return out
}

// typeAndPause drives the TYPING half exactly as the executor's loop does: it
// claims the line through the session's own differential (Observe is the
// debounce, and it is the loop's caller — Claim is the claim the loop makes once
// the pause is observed) and runs the lookup the loop would run. The section is
// left waiting in the session, which is the state the seal then meets.
func typeAndPause(t *testing.T, p *chatPrefetch, line string, now time.Time) {
	t.Helper()
	query, ok := p.session.Claim(line, now, p.cfg)
	if !ok {
		t.Fatalf("the typing path refused the line %q — the fixture no longer reaches its gate", line)
	}
	p.fetch(query)
}

// TestATypingPauseOnAPartialLineIsPaidForAtThatPause is the DESIGN, pinned.
//
// TWO lookups for this input sequence, and they are two different questions: the
// pause's (the line as it stood) and the turn's (the line as it was sealed). The
// pause's section is SUPERSEDED at the typing path itself — the second fetch's
// Use overwrites it, because the machine's memory holds the answer for the line
// it last fetched — so it carries no adoption event, while the turn's section is
// SHARED (the keys are equal by then, which is the sibling row's contract).
func TestATypingPauseOnAPartialLineIsPaidForAtThatPause(t *testing.T) {
	p, dir := turnEnabled(t)
	start := time.Now()
	typeAndPause(t, p, sharedTypedPrefix, start)
	// The operator types on: the differential grants a NEW claim exactly because
	// the line gained terms the fetched query did not carry (DefaultMinQueryDelta)
	// — that is the designed re-fire, not a duplicate.
	typeAndPause(t, p, sharedSealedLine, start.Add(2*time.Second))

	out, adopted := chatPrefetchAdoptTurn(
		chatOutbound{Message: sharedSealedLine, Display: sharedSealedLine},
		p, "human", chatPrefetchTurnTerms(false, nil, sharedSealedLine))
	if !adopted {
		t.Fatalf("the turn did not carry a section — record:\n%s", readPrefetchRecord(t, dir))
	}
	if !strings.Contains(out.Prefetch.Section, "memory-prefetch") {
		t.Errorf("the section did not ride the message:\n%s", out.describe())
	}
	if out.Display != sharedSealedLine {
		t.Errorf("Display was modified: %q", out.Display)
	}

	recs := decodePrefetchRecords(t, dir)
	got := queries(recs)
	if len(got) != 2 {
		t.Fatalf("recorded %d retrieval line(s), want 2 (the pause's own and the turn's own):\n%s",
			len(got), readPrefetchRecord(t, dir))
	}
	// The two keys, and the relation between them: the pause's is a STRICT
	// SUBSET of the turn's — which is exactly why they are not one question and
	// why neither can be dropped for the other.
	prefixTerms := map[string]bool{}
	for _, w := range strings.Fields(got[0]) {
		prefixTerms[w] = true
	}
	missing := 0
	for _, w := range strings.Fields(got[1]) {
		if !prefixTerms[w] {
			missing++
		}
	}
	if missing == 0 {
		t.Fatalf("the fixture no longer shows the prefix shape (second key %q carries no term the first %q lacks) — the case would prove nothing",
			got[1], got[0])
	}
	if got[1] == got[0] {
		t.Fatalf("the two retrievals are the SAME key (%q) — that is the sibling row's duplicate, not this one", got[0])
	}
	// The turn's retrieval reused what the typing path already had for the
	// SEALED line (one section per turn, and it was not paid for twice).
	if n := countOutcome(recs, prefetchOutcomeShared); n != 1 {
		t.Errorf("recorded %d outcome=%s line(s), want 1 (the turn reused the section fetched for the sealed key):\n%s",
			n, prefetchOutcomeShared, readPrefetchRecord(t, dir))
	}
	// THE PAUSE'S SECTION NEVER REACHED A PROMPT — no adoption event names its
	// key, and the record says so rather than leaving a reader to infer it. That
	// is the premium the pause bought, stated as the fact it is.
	for _, r := range recs {
		if r.Outcome != prefetchOutcomeAdopted && r.Outcome != prefetchOutcomeShared {
			continue
		}
		if r.Query == got[0] {
			t.Errorf("the pause's section was adopted although the line had moved on: %+v", r)
		}
	}
}

// TestAPrefixFetchedSectionNeverServesAFullerTurn is the REFUSAL, and it is the
// reason the sharing rule is "the whole key" rather than "the key is a prefix":
// a section fetched for the line as it stood does NOT cover the line as it was
// sealed, so the session's own adoption test must refuse it. Widening the rule —
// what this row asked for — would inject it.
func TestAPrefixFetchedSectionNeverServesAFullerTurn(t *testing.T) {
	p, _ := turnEnabled(t)
	typeAndPause(t, p, sharedTypedPrefix, time.Now())

	if section, ok := p.session.Take(sharedSealedLine, p.cfg); ok {
		t.Fatalf("a section fetched for the PREFIX was handed to the fuller line (%d bytes): the turn's own terms were never retrieved for it", len(section))
	}
	// The control: the same section IS the prefix line's own answer, so a turn
	// that sealed THAT line gets it.
	section, ok := p.session.Take(sharedTypedPrefix, p.cfg)
	if !ok || section == "" {
		t.Fatal("the pause's own section was refused for the line it was fetched for — the feature is inert, not conservative")
	}
}

// TestASharedKeyNeverSuppressesADifferentQuestion is the CONSERVATIVE DIRECTION,
// and it is the reason the criterion is a whole-key relation: a turn whose key
// carries one term the typing path never had in hand is a DIFFERENT question, so
// it pays its own retrieval. Skipping on mere overlap would silence it.
func TestASharedKeyNeverSuppressesADifferentQuestion(t *testing.T) {
	p, dir := turnEnabled(t)
	typeAndPause(t, p, sharedTypedPrefix, time.Now())

	out, adopted := chatPrefetchAdoptTurn(
		chatOutbound{Message: sharedForeignLine, Display: sharedForeignLine},
		p, "llm", chatPrefetchTurnTerms(false, nil, sharedForeignLine))
	if !adopted {
		t.Fatalf("the turn's OWN retrieval did not adopt a section — record:\n%s", readPrefetchRecord(t, dir))
	}
	if !strings.Contains(out.Prefetch.Section, "memory-prefetch") {
		t.Errorf("the section did not ride the message:\n%s", out.describe())
	}
	recs := decodePrefetchRecords(t, dir)
	if got := countOutcome(recs, prefetchOutcomeHits); got != 2 {
		t.Errorf("recorded %d outcome=%s lookup line(s), want 2 (the typing fetch AND the turn's own):\n%s",
			got, prefetchOutcomeHits, readPrefetchRecord(t, dir))
	}
	if got := countOutcome(recs, prefetchOutcomeShared); got != 0 {
		t.Errorf("a turn carrying an unheld term was served from the typing section (%d shared line(s)):\n%s",
			got, readPrefetchRecord(t, dir))
	}
}

// TestTheSharedSectionIsNotAdoptedTwice pins the other half of "one section per
// turn": the turn's adoption CONSUMES the typing section, so the REPL's fallback
// (chatPrefetchAdopt, taken when the turn retrieved nothing) cannot append the
// same block a second time.
func TestTheSharedSectionIsNotAdoptedTwice(t *testing.T) {
	p, dir := turnEnabled(t)
	start := time.Now()
	typeAndPause(t, p, sharedTypedPrefix, start)
	typeAndPause(t, p, sharedSealedLine, start.Add(2*time.Second))

	out, adopted := chatPrefetchAdoptTurn(
		chatOutbound{Message: sharedSealedLine, Display: sharedSealedLine},
		p, "human", chatPrefetchTurnTerms(false, nil, sharedSealedLine))
	if !adopted {
		t.Fatalf("the turn did not carry a section — record:\n%s", readPrefetchRecord(t, dir))
	}
	withSection := out.Prefetch.Section
	if withSection == "" {
		t.Fatalf("the turn carried no section to consume — record:\n%s", readPrefetchRecord(t, dir))
	}
	again, ok := chatPrefetchAdopt(out, p, sharedSealedLine)
	if ok {
		t.Error("the typing section was adopted a SECOND time after the turn consumed it")
	}
	// `ok=false` means the caller's own value came back untouched, so the turn
	// that must be unchanged is compared WHOLE — the shape the reuse contract is
	// about ("cannot append the same block a second time"), and unlike a
	// comparison against the copy's empty seed it says something once the
	// fixture reaches the injection floor (a declined adoption never clears the
	// seed it was handed).
	if again.Message != out.Message || again.Display != out.Display || again.Prefetch != out.Prefetch {
		t.Errorf("a second adoption changed the turn:\n%q\n%q", withSection, again.describe())
	}
	if got := countOutcome(decodePrefetchRecords(t, dir), prefetchOutcomeAdopted); got != 0 {
		// The turn's adoption rides outcome=shared (its own line), so an
		// outcome=adopted line here would mean a second, unpaid-for adoption.
		t.Errorf("recorded %d outcome=%s line(s) after the turn consumed the section:\n%s",
			got, prefetchOutcomeAdopted, readPrefetchRecord(t, dir))
	}
}

// TestALiftedKeyNeverCoversTheSealedLine is the WIDENING'S OWN RED PROOF: the
// epic's proposal, taken literally, is "share when the typed key is a PREFIX of
// the turn's" — and this is the measurement that refuses it. A section fetched
// for the prefix key does not COVER the sealed line's key, so `Take` refuses it
// (the same predicate the sibling's equality test reduces to at equality: zero
// new keywords). The assertion is the invariant, not a snapshot of one fixture:
// the typing section is adoptable for the sealed line EXACTLY WHEN its own key
// covers it, so any change that made a lifted key adoptable fails here rather
// than on the next live run.
func TestALiftedKeyNeverCoversTheSealedLine(t *testing.T) {
	p, dir := turnEnabled(t)
	typeAndPause(t, p, sharedTypedPrefix, time.Now())

	pending := p.session.PendingQuery()
	if pending == "" {
		t.Fatal("no pending query after the pause — the fixture no longer reaches the typing path")
	}
	sealed, ok := memoryprefetch.Query(sharedSealedLine, p.cfg)
	if !ok {
		t.Fatalf("the sealed line %q yields no key", sharedSealedLine)
	}
	// The measured relation, asserted rather than described: the sealed key
	// carries at least one term the lifted key does not, so the two are not one
	// question (the epic's own `P≠Q`). Counted here rather than through a new
	// exported helper: the test asserts the RELATION between two strings it
	// printed, and adding an export for it would be production surface bought
	// for a test.
	lifted := map[string]bool{}
	for _, w := range strings.Fields(pending) {
		lifted[w] = true
	}
	uncovered := 0
	for _, w := range strings.Fields(sealed) {
		if !lifted[w] {
			uncovered++
		}
	}
	if uncovered == 0 {
		t.Fatalf("keys %q and %q cover each other — the fixture no longer has the prefix shape", pending, sealed)
	}
	if section, ok := p.session.Take(sharedSealedLine, p.cfg); ok {
		t.Fatalf("the lifted key's section was adopted for the sealed line (%d bytes): the turn's own terms were never retrieved for it", len(section))
	}
	// The turn's OWN retrieval is what supplies its section — asserted through
	// the turn boundary, because "the typing section was refused" is only half
	// of "the question was still asked".
	out, adopted := chatPrefetchAdoptTurn(
		chatOutbound{Message: sharedSealedLine, Display: sharedSealedLine},
		p, "human", chatPrefetchTurnTerms(false, nil, sharedSealedLine))
	if !adopted || !strings.Contains(out.Prefetch.Section, "memory-prefetch") {
		t.Fatalf("the turn carrying a term the lifted key never had ran NO retrieval — adopted=%v:\n%s", adopted, out.describe())
	}
	// The turn's own key, on the record — the `P≠Q` pair as the run reports it.
	recs := decodePrefetchRecords(t, dir)
	got := queries(recs)
	if len(got) != 2 || got[0] != pending || got[1] != sealed {
		t.Fatalf("the record does not carry the two questions in order (lifted %q, sealed %q):\n%v",
			pending, sealed, got)
	}
	if n := countOutcome(recs, prefetchOutcomeShared); n != 0 {
		t.Errorf("a turn whose key the lifted one does not cover was served the lifted section (%d shared):\n%s",
			n, readPrefetchRecord(t, dir))
	}
}

// TestSharedPrefetchIsOffWhenTheSwitchIsOff keeps the OFF contract in this file's
// scope too: with no executor there is no share, no adoption and no record.
func TestSharedPrefetchIsOffWhenTheSwitchIsOff(t *testing.T) {
	t.Setenv(config.MemoryPrefetchEnv, "0")
	dir := t.TempDir()
	if p := chatPrefetchFor(dir, nil, nil); p != nil {
		t.Fatal("chatPrefetchFor built an executor with the switch OFF")
	}
	out, adopted := chatPrefetchAdoptTurn(
		chatOutbound{Message: sharedSealedLine, Display: sharedSealedLine},
		nil, "human", []string{sharedSealedLine})
	if adopted || out.Message != sharedSealedLine || out.Prefetch.Section != "" {
		t.Errorf("the OFF path changed the turn: adopted=%v turn=%q", adopted, out.describe())
	}
}
