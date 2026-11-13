package main

// chat_prefetch_adoption_semantics_test.go — the ADOPTION LINE's semantics, as a
// GATE (Rule 15a).
//
// THE DEFECT THIS PINS (measured on this host 2026-10-07, task
// 01a115ce-4209-73bf-a159f631a3a2cfaa; the corpus is cited by path and command in
// devdoc/memory-prefetch-field-audit.md). The record is ONE LINE PER EVENT: a
// LOOKUP event (what the retrieval found) and an ADOPTION event (a prompt
// absorbed a section) are two different facts, and they are two different
// lines. The lookup line carries the measurement — `hits`, `ms` and the segment
// breakdown — because it is the line that ran the query. The adoption line is
// the second half of the story, and it was serialised with those two fields left
// at their ZERO VALUE, so every adoption event rendered as `"hits":0,"ms":0`.
//
// WHY THAT IS A DEFECT AND NOT A TIDINESS (Rule 15a, and it is the exact shape
// the rule names — a required semantic value lost at the zero value): a reader
// of `"hits":0` cannot tell "the retrieval returned nothing" from "this line did
// not measure hits at all". The corpus proves the ambiguity was live, not
// theoretical: a census asking the record's own documented question ("how many
// lookups found nothing?") reported 47 hits=0 lines, and 35 of them were
// ADOPTION events that had found something. That is a wrong answer produced by a
// field, not by an analysis mistake. The same collision makes a query-less line
// read as an empty query rather than as an event with no question of its own.
//
// THE CONTRACT (both halves asserted below): a field that does not apply to a
// line is ABSENT from that line's JSON; a measured 0 is PRESENT and readable as
// 0. Absent is the only spelling that survives a reader who never saw this file,
// because json.Unmarshal gives an absent key the same zero value a spelled-out 0
// has — so presence must be decided by the KEY, which is why the parser below
// keeps the key set rather than only the decoded values.

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/nick/gopi/internal/config"
	"github.com/nick/gopi/internal/inputsource"
)

// prefetchEventLine is one record line as a READER sees it: the decoded object
// plus the SET OF KEYS the line actually carries. Keeping the key set is the
// whole point — `obj["hits"]` returns 0 for "absent" and for "measured 0" alike,
// which is precisely the ambiguity this contract removes, so a test that read
// values alone could not state the contract it is here to pin.
type prefetchEventLine struct {
	object map[string]any
	keys   map[string]bool
}

// adoptedYes reads the record's `adopted` vocabulary.
func (l prefetchEventLine) adoptedYes() bool {
	return l.str("adopted") == prefetchRecordAdoptedYes
}

// number reads an integer field and reports whether the line CARRIES it. The
// second return is the contract: (0, false) means the field is absent.
func (l prefetchEventLine) number(key string) (int64, bool) {
	if !l.has(key) {
		return 0, false
	}
	f, ok := l.object[key].(float64)
	if !ok {
		return 0, false
	}
	return int64(f), true
}

// has reports whether the line carries the key at all.
func (l prefetchEventLine) has(key string) bool { return l.keys[key] }

// str reads a string field.
func (l prefetchEventLine) str(key string) string {
	s, _ := l.object[key].(string)
	return s
}

// jsonKeysOf decodes ONE record value into (object, key set). It fails the test
// when the value is not one JSON object, because a record that cannot be read
// back is not an observable.
func jsonKeysOf(t *testing.T, b []byte) prefetchEventLine {
	t.Helper()
	var obj map[string]any
	if err := json.Unmarshal(b, &obj); err != nil {
		t.Fatalf("the record is not one JSON object: %v (%s)", err, string(b))
	}
	keys := make(map[string]bool, len(obj))
	for k := range obj {
		keys[k] = true
	}
	return prefetchEventLine{object: obj, keys: keys}
}

// readPrefetchLines decodes the run's record into one line per event, keys
// included. A missing file fails the test: an observable that was never written
// is not a passing gate.
func readPrefetchLines(t *testing.T, dir string) []prefetchEventLine {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(dir, config.MemoryPrefetchRecordName))
	if err != nil {
		t.Fatalf("read the prefetch record: %v", err)
	}
	var out []prefetchEventLine
	for _, raw := range strings.Split(strings.TrimSpace(string(b)), "\n") {
		if strings.TrimSpace(raw) == "" {
			continue
		}
		out = append(out, jsonKeysOf(t, []byte(raw)))
	}
	return out
}

// firstEvent returns the first recorded line with the given outcome.
func firstEvent(t *testing.T, dir, outcome string) prefetchEventLine {
	t.Helper()
	for _, l := range readPrefetchLines(t, dir) {
		if l.str("outcome") == outcome {
			return l
		}
	}
	t.Fatalf("no %q event was recorded:\n%s", outcome, readPrefetchRecord(t, dir))
	return prefetchEventLine{}
}

// assertNoFabricatedMeasurement is the shared assertion: an ADOPTION line must
// be silent about the retrieval's measurement.
func assertNoFabricatedMeasurement(t *testing.T, l prefetchEventLine, door string) {
	t.Helper()
	// The fabricated pair: an adoption event ran no query of its own to time and
	// found no hits itself.
	if v, ok := l.number("ms"); ok {
		t.Errorf("%s: the adoption line carries \"ms\":%d — an adoption event measured no lookup, so the field must be ABSENT (Rule 15a: a reader cannot tell a fabricated 0 from a measured one)", door, v)
	}
	if v, ok := l.number("hits"); ok {
		t.Errorf("%s: the adoption line carries \"hits\":%d — an adoption event found no hits itself, so the field must be ABSENT", door, v)
	}
	// The facts that DO belong to this event stay present and readable: an
	// observable that can only report success is not an observable, and removing
	// the fabrication must not remove the adoption's own measurement.
	if !l.adoptedYes() {
		t.Errorf("%s: the adoption line does not report adopted=%q", door, prefetchRecordAdoptedYes)
	}
	if !l.has("section_bytes") {
		t.Errorf("%s: the adoption line omits section_bytes — the size of what the prompt absorbed is this event's own measurement", door)
	} else if n, _ := l.number("section_bytes"); n <= 0 {
		t.Errorf("%s: section_bytes = %d, want the adopted section's size", door, n)
	}
}

// TestTurnAdoptionLineCarriesNoFabricatedMeasurement is the contract on the
// TURN-BOUNDARY adoption door, measured on a real adoption.
func TestTurnAdoptionLineCarriesNoFabricatedMeasurement(t *testing.T) {
	p, dir := turnEnabled(t)
	out := chatOutbound{Message: turnNoticeText, Display: "external message d1"}
	got, adopted := chatPrefetchAdoptTurn(out, p, inputsource.Notify, []string{turnNoticeText})
	if !adopted {
		t.Fatal("the turn-boundary retrieval did not adopt its section — the gate would be vacuous")
	}
	if !strings.Contains(got.Prefetch.Section, "memory-prefetch") {
		t.Fatalf("the section did not ride the message:\n%s", got.describe())
	}
	assertNoFabricatedMeasurement(t, firstEvent(t, dir, prefetchOutcomeAdopted), "turn-boundary adoption")
}

// TestTypingAdoptionLineCarriesNoFabricatedMeasurement is the same contract on
// the OTHER adoption door (chatPrefetchAdopt, the typing path). Both doors must
// be covered: a gate that pinned one of them would let the defect survive on the
// path the operator actually types on, which is the path that produced most of
// the corpus.
func TestTypingAdoptionLineCarriesNoFabricatedMeasurement(t *testing.T) {
	line := "prefetch the memory ledger read-only"
	dir := t.TempDir()
	path := seedPrefetchLedger(t)
	t.Setenv("GOPI_MEMORY_DB", path)
	t.Setenv(config.MemoryPrefetchEnv, config.EnvBoolOn)

	p := chatPrefetchFor(dir, func() string { return line }, os.Stderr)
	if p == nil {
		t.Fatal("chatPrefetchFor returned nil with the switch ON")
	}
	defer p.Stop()

	claimed, ok := p.session.Claim(line, time.Now(), p.cfg)
	if !ok {
		t.Fatal("the state machine refused a line the gate should accept")
	}
	section, outcome, hits, err := prefetchLookup(context.Background(), claimed, p.cfg)
	if err != nil || hits == 0 {
		t.Fatalf("the fixture ledger produced no usable hits: outcome=%s hits=%d err=%v", outcome, hits, err)
	}
	if !p.session.Use(claimed, section) {
		t.Fatal("the executor refused to store its own query's result")
	}
	if _, adopted := chatPrefetchAdopt(chatOutbound{Message: line, Display: line}, p, line); !adopted {
		t.Fatal("chatPrefetchAdopt did not adopt a section that is in hand")
	}
	assertNoFabricatedMeasurement(t, firstEvent(t, dir, prefetchOutcomeAdopted), "typing adoption")
}

// TestLookupLineKeepsItsMeasuredZero is the SECOND half, and it is the half that
// makes the first one safe rather than merely tidy: dropping these fields from
// the adoption line must not spread to the LOOKUP line, where a measured 0 is a
// real and important answer. A gate that removed the field wherever its value
// was zero would turn "this lookup found nothing" back into "this line did not
// measure" — the same defect with the sign flipped.
func TestLookupLineKeepsItsMeasuredZero(t *testing.T) {
	b, err := json.Marshal(prefetchRecord{
		Query: "prefetch ledger", Hits: prefetchHitsLabel(0), Ms: prefetchMsLabel(7),
		Outcome: prefetchOutcomeEmpty, Adopted: prefetchRecordAdoptedNo,
	})
	if err != nil {
		t.Fatalf("marshal the lookup line: %v", err)
	}
	l := jsonKeysOf(t, b)
	for _, key := range []string{"hits", "ms"} {
		if !l.has(key) {
			t.Errorf("the lookup line omits %q — a LOOKUP measured that field, and a measured 0 must stay readable as 0 (the removal belongs to the adoption line only): %s", key, string(b))
		}
	}
	if n, ok := l.number("hits"); !ok || n != 0 {
		t.Errorf("hits = %v (present=%v), want the measured 0 present", l.object["hits"], ok)
	}
	if n, ok := l.number("ms"); !ok || n != 7 {
		t.Errorf("ms = %v (present=%v), want 7 present", l.object["ms"], ok)
	}
}

// TestQuerylessEventDoesNotReadAsAnEmptyQuery pins the SAME contract on the one
// field where the ambiguity is silent today: `query` is a string without
// omitempty, so every supersession line in the corpus renders `"query":""` —
// which a reader must interpret as "this event asked nothing", not as "this
// event asked the empty string". With the field absent, the two readings
// coincide by construction and a consumer ranging over the lines that carry
// `query` no longer has to special-case the empty one.
func TestQuerylessEventDoesNotReadAsAnEmptyQuery(t *testing.T) {
	b, err := json.Marshal(prefetchRecord{Outcome: prefetchOutcomeSuperseded, Adopted: prefetchRecordAdoptedNo})
	if err != nil {
		t.Fatalf("marshal the supersession line: %v", err)
	}
	l := jsonKeysOf(t, b)
	if l.has("query") {
		t.Errorf("a query-less event still carries \"query\"=%q — an event that asked nothing must not render as one that asked the empty string (Rule 15a): %s", l.str("query"), string(b))
	}
	// The event's own facts stay.
	for _, key := range []string{"outcome", "adopted"} {
		if !l.has(key) {
			t.Errorf("the supersession line omits %q: %s", key, string(b))
		}
	}
}
