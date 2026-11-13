package memoryprefetch

import (
	"strconv"
	"strings"
	"testing"
	"time"
)

// The typing-path tests: the query a half-typed line is worth (Query) and the
// debounce + differential machine that decides when it may be fetched
// (Session). They drive the REAL entry points — the executor in
// cmd/agent/chat_prefetch.go calls exactly these — because a test that
// re-implements the differential asserts nothing about the binary (Rule 31).
//
// The same behaviours are pinned at the executor's own boundary in
// cmd/agent/chat_prefetch_test.go (adoption, byte identity when off, give-up);
// here they are pinned WITHOUT a ledger, a goroutine or a terminal, which is the
// whole reason the decisions live in this package.

// TestQueryIsGatedByKeywordsAndLength pins the complexity gate's two signals on
// the typing path: the PRIMARY signal is the keyword count (a long line with no
// extractable subject is skipped, which a length-only gate would wave through)
// and the secondary signal is the input length.
func TestQueryIsGatedByKeywordsAndLength(t *testing.T) {
	cfg := DefaultConfig()
	if _, ok := Query("!!!! ???? ---- .....", cfg); ok {
		t.Errorf("a long line with no keywords passed the gate — the keyword count is supposed to be primary")
	}
	if _, ok := Query("ab", cfg); ok {
		t.Errorf("a line shorter than MinInputRunes passed the gate")
	}
	query, ok := Query("prefetch the memory ledger read-only", cfg)
	if !ok || query == "" {
		t.Fatalf("a line with keyword content was refused: q=%q ok=%v", query, ok)
	}
	if strings.Contains(query, "the") {
		t.Errorf("query %q carries a stopword", query)
	}
}

// TestQueryIsBounded pins the query budget: a query is a retrieval key, not a
// transcript, and an unbounded one would be re-typed into every prompt it
// annotates. The cut is MARKED, so a bounded key is never read as a terse one.
func TestQueryIsBounded(t *testing.T) {
	cfg := DefaultConfig()
	words := make([]string, 0, 40)
	for i := 0; i < 40; i++ {
		// DISTINCT, LONG tokens: 40 copies of one word are one keyword after
		// dedup (which would exercise the gate instead of the bound), and short
		// ones would leave the joined query inside the budget.
		words = append(words, "keywordword"+strconv.Itoa(i)+strings.Repeat("x", 20))
	}
	query, ok := Query(strings.Join(words, " "), cfg)
	if !ok {
		t.Fatal("a long keyword line was refused")
	}
	if n := runeLenOf(query); n > DefaultMaxQueryRunes {
		t.Errorf("query is %d runes, over the %d bound", n, DefaultMaxQueryRunes)
	}
	if !strings.HasSuffix(query, clipMark) {
		t.Errorf("a bounded query carries no %q mark — a truncated key must not read as a terse one: %q", clipMark, query)
	}
}

// TestDebounceDoesNotFireWhileTyping pins the debounce: a lookup may fire only
// after the line has been STILL for the window, which is what keeps the
// prefetch off the keystroke path.
func TestDebounceDoesNotFireWhileTyping(t *testing.T) {
	cfg := DefaultConfig()
	var session Session
	now := time.Now()
	debounce := time.Duration(DefaultDebounceMS) * time.Millisecond
	if session.Observe(now, now.Add(-debounce/2), cfg) {
		t.Errorf("Observe fired while the operator was still typing")
	}
	if !session.Observe(now, now.Add(-debounce*2), cfg) {
		t.Errorf("Observe refused a line that has been still for the whole window")
	}
	// A nil receiver answers false rather than panicking: the executor's zero
	// value is inert by contract.
	var nilSession *Session
	if nilSession.Observe(now, now.Add(-debounce*2), cfg) {
		t.Errorf("a nil Session claimed the operator had paused")
	}
}

// TestDifferentialDoesNotRefetchTheSameSubject is the machine's other half: the
// same line pausing twice must cost ONE lookup — the result is already in hand.
func TestDifferentialDoesNotRefetchTheSameSubject(t *testing.T) {
	cfg := DefaultConfig()
	var session Session
	now := time.Now()
	line := "prefetch the memory ledger read-only"
	query, ok := session.Claim(line, now, cfg)
	if !ok {
		t.Fatal("the first claim was refused")
	}
	if !session.Use(query, "SECTION") {
		t.Fatal("Use refused its own query")
	}
	if again, ok := session.Claim(line, now.Add(time.Minute), cfg); ok {
		t.Errorf("the same line re-fired a lookup (%q) — the differential is not working", again)
	}

	// A line that moved on by a keyword is a DIFFERENT question and does fire.
	moved := line + " timeout"
	if _, ok := session.Claim(moved, now.Add(time.Minute), cfg); !ok {
		t.Errorf("a line with a new keyword did not re-fire")
	}
}

// TestTakeRefusesAStaleSection is the honesty gate on the injection: a section
// fetched for a line the operator has since replaced must NOT be appended to the
// new one. It is the difference between an evidence block and a hallucination
// with a citation.
func TestTakeRefusesAStaleSection(t *testing.T) {
	cfg := DefaultConfig()
	var session Session
	now := time.Now()
	query, ok := session.Claim("prefetch the memory ledger", now, cfg)
	if !ok {
		t.Fatal("claim refused")
	}
	if !session.Use(query, "SECTION") {
		t.Fatal("use refused")
	}
	if got, ok := session.Take("a completely different subject entirely", cfg); ok {
		t.Errorf("a stale section was adopted into an unrelated line: %q", got)
	}
	got, ok := session.Take("prefetch the memory ledger", cfg)
	if !ok || got != "SECTION" {
		t.Errorf("the section for its own line was refused: (%q, %v)", got, ok)
	}
	// Taken ONCE: a section cannot be injected into two turns.
	if _, ok := session.Take("prefetch the memory ledger", cfg); ok {
		t.Errorf("the same section was taken twice")
	}
}

// TestUseDiscardsAResultForAMovedLine pins the in-flight case: the operator
// typed on while the query ran, so its result describes a question nobody is
// asking any more.
func TestUseDiscardsAResultForAMovedLine(t *testing.T) {
	cfg := DefaultConfig()
	var session Session
	now := time.Now()
	if _, ok := session.Claim("first subject here now please", now, cfg); !ok {
		t.Fatal("claim refused")
	}
	if _, ok := session.Claim("second subject there now thanks", now, cfg); !ok {
		t.Fatal("second claim refused")
	}
	if session.Use("first subject here now please", "SECTION") {
		t.Errorf("a result for a superseded query was accepted")
	}
}

// TestInjectSectionJoinsOnlyARealSection pins the injected FORM: the message
// gains a separated block when hits survived the trim, and is returned
// BYTE-identical when none did (a header plus a degradation list is a statement
// about nothing, and appending it would cost the model tokens to read it).
//
// IT CARRIES TWO HITS, and that number is not incidental: the injection floor
// (Config.MinInjectHits, config.go) refuses a retrieval BELOW 2 outright, so a
// one-hit fixture would stop at the floor and never reach the FORM this test is
// about. The floor's own boundary is pinned next door, in inject_floor_test.go.
func TestInjectSectionJoinsOnlyARealSection(t *testing.T) {
	cfg := DefaultConfig()
	const out = "what does the prefetch epic do?"
	usable := []Hit{
		{ID: 3, Topic: "prefetch", Answer: "open the ledger read-only", Tier: "hot", Confidence: 0.9},
		{ID: 7, Topic: "prefetch", Answer: "the floor decides what is worth injecting", Tier: "hot", Confidence: 0.9},
	}
	joined, ok := InjectSection(out, usable, cfg)
	if !ok {
		t.Fatal("InjectSection refused a usable hit")
	}
	if !strings.HasPrefix(joined, out+sectionSeparator) {
		t.Errorf("the section was not appended as a separate block:\n%q", joined)
	}
	if !strings.Contains(joined, "memory-prefetch") {
		t.Errorf("the appended block is not the rendered segment:\n%s", joined)
	}

	empty, ok := InjectSection(out, []Hit{{ID: 4, Topic: "x", Answer: "", Question: ""}}, cfg)
	if ok {
		t.Errorf("a hit with no renderable body produced a section: %q", empty)
	}
	if empty != out {
		t.Errorf("the message changed with nothing to inject: %q", empty)
	}
}
