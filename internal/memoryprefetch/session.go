package memoryprefetch

// session.go — the TYPING-TIME half of this package's pure core: the query a
// half-typed line is worth (Query), and the debounce + differential state
// machine that decides WHEN such a query may be issued (Session).
//
// It answers a DIFFERENT question from the sibling machine in state.go, which is
// why it is a second type rather than more fields on State: Step decides whether
// a TURN is worth a retrieval and what its delta terms are (a turn boundary,
// driven by a composer that has the whole buffer), while Session decides whether
// the OPERATOR has paused at all and whether the query already in hand still
// describes the line being sealed (a keystroke stream, driven by a background
// sampler). Both are pure; only the executor in cmd/agent/chat_prefetch.go owns
// the goroutine, the read-only ledger handle, the bound and the record.

import (
	"strings"
	"sync"
	"time"
)

// The typing-time thresholds. Named (Rule 1) because each is the number a
// behaviour is judged by: a bare literal here would let the test and the
// behaviour drift apart silently.
const (
	// DefaultMaxQueryRunes bounds the query string a line becomes. A query is a
	// retrieval KEY, not a transcript: an unbounded one would be re-typed into
	// every prompt it annotates. The cut is MARKED (clipBody), so a model can
	// tell a bounded query from a terse one.
	DefaultMaxQueryRunes = 160

	// DefaultDebounceMS is how long the line must be STILL before a lookup may
	// fire, in milliseconds. It is the difference between prefetching on a
	// keystroke and prefetching on a pause: without it every keystroke would
	// queue a query, and the prefetch would compete with the foreground for the
	// ledger handle exactly when the operator is typing fastest.
	DefaultDebounceMS = 600

	// DefaultMinQueryDelta is the differential threshold: a re-fire is allowed
	// only when the candidate query carries at least this many keywords the
	// fetched one does NOT — i.e. when the line has gained new information.
	// Without it a long line would re-query on every pause for a result the
	// model already has.
	DefaultMinQueryDelta = 1
)

// Query builds the retrieval query for a half-typed line, or reports that the
// line is not worth one.
//
// It is the ONE entry point of the complexity gate on the typing path, and it
// delegates the gate's own decision to EvaluateGate rather than restating it:
// the primary signal is the keyword count (a long line with no extractable
// subject is skipped however long it is) and the secondary signal is the input
// length, and both live in gate.go so the two callers cannot disagree.
func Query(line string, cfg Config) (string, bool) {
	cfg = cfg.withDefaults()
	set, err := ExtractCandidates(line, cfg)
	if err != nil {
		// An unusable input is not a query: this is the typing path, each
		// keystroke calls it, and a refusal is the honest answer (Rule 15 —
		// the caller decides whether to record it; it cannot become a lookup).
		return "", false
	}
	in := GateInput{Text: line, RuneLen: runeLenOf(line)}
	if !EvaluateGate(in, set, cfg).Trigger {
		return "", false
	}
	query := strings.Join(set.TermsText(), deltaSeparator)
	if query == "" {
		return "", false
	}
	return clipBody(query, DefaultMaxQueryRunes), true
}

// Session is the debounce + differential memory of ONE typing session: what the
// operator has paused on, which query that produced, and the result waiting to
// be adopted at seal time.
//
// WHY A DIFFERENTIAL AND NOT A TIMER: the same line pauses twice (the operator
// stops to think), and a debounce alone would re-query a result already in hand.
// Session therefore compares the CANDIDATE query against the last one it claimed
// (Claim) and against the one whose section it holds (Take), by NEW-KEYWORD
// count, so an unchanged subject costs one lookup and not N.
//
// It is safe for concurrent use — one writer (the executor's sampling goroutine)
// and one reader (the REPL's sealing path) — and the lock is what makes "the
// result a turn adopts" a snapshot rather than a race.
type Session struct {
	mu sync.Mutex

	firedAt      time.Time // when the last lookup was claimed
	fetched      string    // the query of that lookup
	section      string    // its result, waiting to be adopted
	sectionQuery string    // the query that produced `section`
}

// Observe reports whether the operator has PAUSED — the debounce half of the
// machine. It starts no work itself: the caller owns the goroutine, and this
// function only answers a question about time.
func (s *Session) Observe(now time.Time, lastChange time.Time, cfg Config) bool {
	if s == nil {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	debounce := time.Duration(DefaultDebounceMS) * time.Millisecond
	if !lastChange.IsZero() && now.Sub(lastChange) < debounce {
		return false
	}
	if !s.firedAt.IsZero() && now.Sub(s.firedAt) < debounce {
		return false
	}
	return true
}

// Claim returns the query to fetch now, or ok=false when the differential says
// the one already fetched still describes this line. A successful claim records
// the query and the instant, so the next Observe is measured against it.
func (s *Session) Claim(line string, now time.Time, cfg Config) (string, bool) {
	if s == nil {
		return "", false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	query, ok := Query(line, cfg)
	if !ok {
		return "", false
	}
	// The DIFFERENTIAL gate, stated as NEW INFORMATION rather than as overlap:
	// a query re-fires when the line has gained at least MinQueryDelta keywords
	// the fetched one does not carry. Overlap would be the wrong test — a line
	// that GREW ("… ledger" -> "… ledger timeout") overlaps the old query
	// completely and is nonetheless a different question, while a line that was
	// merely re-worded is the same question and must cost nothing.
	if s.fetched != "" && newKeywordCount(s.fetched, query) < DefaultMinQueryDelta {
		return "", false
	}
	s.fetched = query
	s.firedAt = now
	return query, true
}

// Use records a lookup's result. A result whose query is no longer the line's
// own candidate is DISCARDED (the operator typed on while it was in flight), so
// a stale section can never be adopted.
func (s *Session) Use(query, section string) bool {
	if s == nil {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if query == "" || section == "" || query != s.fetched {
		return false
	}
	s.section = section
	s.sectionQuery = query
	return true
}

// Take returns the section to adopt at seal time, or ok=false when there is none
// for THIS line — the differential's last gate, and the one that makes the
// injection honest: a section fetched for an earlier line is not injected into a
// different one just because it is still in memory.
func (s *Session) Take(line string, cfg Config) (string, bool) {
	if s == nil {
		return "", false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.section == "" {
		return "", false
	}
	query, ok := Query(line, cfg)
	if !ok {
		return "", false
	}
	// The section describes the line being SEALED, or it is not adopted: zero
	// keywords of the current line may be missing from the query the hits were
	// fetched with. A line the operator kept typing on is a different question,
	// and the honest answer for it is "no section", not a slightly older one.
	if newKeywordCount(s.sectionQuery, query) != 0 {
		return "", false
	}
	out := s.section
	s.section = ""
	return out, true
}

// PendingQuery names the query that produced the section most recently taken. It
// exists for the RECORD: the executor reports which query a prompt actually
// absorbed, and reconstructing it there would be a second copy of the state
// machine's own memory of it.
func (s *Session) PendingQuery() string {
	if s == nil {
		return ""
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.sectionQuery != "" {
		return s.sectionQuery
	}
	return s.fetched
}

// Reset drops the pending result (the executor calls it when the run ends) so
// nothing can be adopted by a later, unrelated turn.
func (s *Session) Reset() {
	if s == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.section = ""
	s.sectionQuery = ""
	s.fetched = ""
	s.firedAt = time.Time{}
}

// newKeywordCount counts the keywords of candidate that are ABSENT from base —
// the differential's distance metric, and deliberately not a symmetric overlap.
// `alpha beta` and `beta alpha` are the same question (0 new keywords, no
// re-fire), while `alpha beta` against `alpha beta gamma` is a different one (1
// new keyword) even though it contains the first completely.
func newKeywordCount(base, candidate string) int {
	set := map[string]bool{}
	for _, t := range strings.Fields(base) {
		set[t] = true
	}
	n := 0
	for _, t := range strings.Fields(candidate) {
		if !set[t] {
			n++
		}
	}
	return n
}
