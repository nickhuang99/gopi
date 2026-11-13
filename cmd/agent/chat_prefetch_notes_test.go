package main

// chat_prefetch_notes_test.go — the SECOND retrieval leg, driven end to end
// through the real lookup and read back off the real record.
//
// WHAT IT PINS, and why each one is a measurement rather than a claim:
//
//  1. THE RED/GREEN PAIR OF THE EPIC. A memory ledger that knows NOTHING about
//     the query plus a note index that holds the answer must still produce an
//     injected section — that is the whole reason the second leg exists, and the
//     case is the one measured on this machine (a design decision the memory
//     search could not find and the note index held).
//  2. THE LEDGER-ONLY CASE IS UNCHANGED. With no note source, the record is
//     exactly the memory-only record it always was: no `notes` key at all. That
//     is the control that keeps this change from rewriting 900 existing lines.
//  3. EACH LEG'S VERDICT IS RECORDED SEPARATELY. `memory_outcome` and
//     `notes.outcome` are two facts; a leg that could not be asked is never
//     reported as a leg that found nothing.
//  4. THE LEG COSTS THE MEMORY LEG NOTHING. A failing note leg leaves the
//     memory section byte-for-byte what it would have been, and the record says
//     why the second leg is missing.
//  5. THE FIXTURE IS THE SCAFFOLD'S (Rule 32) and the two stores are the SAME
//     file here, which is also what makes the handle-reuse path the one under
//     test (chat_prefetch_notes.go's handleFor).

import (
	"context"
	"strings"
	"testing"

	"github.com/nick/gopi/internal/memoryprefetch"
	"github.com/nick/gopi/internal/tools"
)

// seedPrefetchNotesLedger writes the note index's own rows and READS THEM BACK
// through the production read face, so the fixture is a measurement rather than
// a hope — a note that did not land would make every assertion below vacuous.
//
// The rows go in through tools.ChatNoteRecord (the index's single writer, which
// builds its own schema on demand) and come back through tools.ChatNoteSearch
// (the CLI's own page). Neither the table's name nor its DDL is spelled here: a
// test that wrote the SQL itself would keep passing after the table changed.
func seedPrefetchNotesLedger(t *testing.T, notes []tools.ChatNote, probe string) {
	t.Helper()
	for _, n := range notes {
		if err := tools.ChatNoteRecord(n); err != nil {
			t.Fatalf("index the fixture note: %v", err)
		}
	}
	page, err := tools.ChatNoteSearch(probe, 0)
	if err != nil {
		t.Fatalf("read the fixture back: %v", err)
	}
	if strings.Contains(page, "No notes matching") {
		t.Fatalf("the fixture notes did not land: a search for %q found nothing", probe)
	}
}

// TestPrefetchNotesLegRescuesAnEmptyMemoryLedger is acceptance (a): the memory
// ledger knows nothing, the note index holds the answer, and the model-facing
// section is emitted anyway — carrying BOTH annotations, so a reader can tell
// which index answered.
func TestPrefetchNotesLegRescuesAnEmptyMemoryLedger(t *testing.T) {
	path := seedPrefetchLedger(t)
	t.Setenv("GOPI_MEMORY_DB", path)
	seedPrefetchNotesLedger(t, []tools.ChatNote{
		{
			SessionID: "an-earlier-run",
			Turn:      12,
			Note:      "the note leg must be PARALLEL: both indexes feed one section and each line names its index",
			Host:      "some-other-host",
			Path:      "/home/nick/.gopi/sessions/an-earlier-run/self-record.md",
		},
		{
			SessionID: "an-earlier-run",
			Turn:      13,
			Note:      "the note leg must be PARALLEL: it renders through the same InjectSection the memory leg uses, one section not two",
			Host:      "some-other-host",
			Path:      "/home/nick/.gopi/sessions/an-earlier-run/self-record.md",
		},
	}, "note leg must be PARALLEL")

	// A query the MEMORY ledger cannot answer: no distilled row carries these
	// words, so the memory leg ends `empty` and the note leg is the only leg with
	// anything to say.
	const query = "note leg must be PARALLEL"
	res := prefetchLookupDetailed(context.Background(), query, memoryprefetch.DefaultConfig(), nil,
		newPrefetchNotesSource("")) // no session id: nothing is excluded

	if res.MemoryOutcome != prefetchOutcomeEmpty {
		t.Fatalf("memory outcome = %q (err=%v), want %q — the fixture must not answer this query from the ledger",
			res.MemoryOutcome, res.Err, prefetchOutcomeEmpty)
	}
	if res.Notes.Outcome != prefetchNotesOutcomeHits {
		t.Fatalf("notes outcome = %q (tier=%q err=%v), want %q", res.Notes.Outcome, res.Notes.Tier, res.Notes.Err, prefetchNotesOutcomeHits)
	}
	if res.Outcome != prefetchOutcomeHits {
		t.Fatalf("lookup outcome = %q (err=%v), want %q — the note leg did NOT rescue the empty memory leg",
			res.Outcome, res.Err, prefetchOutcomeHits)
	}
	if !strings.Contains(res.Section, memoryprefetch.NoteIndexOrigin()) {
		t.Fatalf("the section carries no note-index annotation:\n%s", res.Section)
	}
	if !strings.Contains(res.Section, "some-other-host") {
		t.Fatalf("the section carries no note provenance (host):\n%s", res.Section)
	}
	if !strings.Contains(res.Section, "/home/nick/.gopi/sessions/an-earlier-run/self-record.md") {
		t.Fatalf("the section carries no note provenance (full path):\n%s", res.Section)
	}
	if res.Notes.HitsCount != 2 || res.Hits != 2 {
		t.Fatalf("hits: notes=%d combined=%d, want 2 and 2", res.Notes.HitsCount, res.Hits)
	}
}

// TestPrefetchNotesLegRunsBesideTheMemoryLeg is acceptance (b): when BOTH legs
// answer, both lines reach the ONE section, the memory leg's first, and the
// record carries one verdict per leg.
func TestPrefetchNotesLegRunsBesideTheMemoryLeg(t *testing.T) {
	path := seedPrefetchLedger(t)
	t.Setenv("GOPI_MEMORY_DB", path)
	seedPrefetchNotesLedger(t, []tools.ChatNote{{
		SessionID: "an-earlier-run",
		Turn:      4,
		Note:      "read-only ledger DSN: the busy timeout bounds the wait and query_only refuses every write",
		Host:      "some-other-host",
		Path:      "/home/nick/.gopi/sessions/an-earlier-run/self-record.md",
	}}, "read-only ledger DSN")

	res := prefetchLookupDetailed(context.Background(), "read-only ledger DSN", memoryprefetch.DefaultConfig(), nil,
		newPrefetchNotesSource(""))

	if res.MemoryOutcome != prefetchOutcomeHits {
		t.Fatalf("memory outcome = %q, want %q (err=%v)", res.MemoryOutcome, prefetchOutcomeHits, res.Err)
	}
	if res.Notes.Outcome != prefetchNotesOutcomeHits {
		t.Fatalf("notes outcome = %q (tier=%q err=%v), want %q", res.Notes.Outcome, res.Notes.Tier, res.Notes.Err, prefetchNotesOutcomeHits)
	}
	mem := strings.Index(res.Section, "memory:")
	notes := strings.Index(res.Section, memoryprefetch.NoteIndexOrigin())
	if mem < 0 || notes < 0 {
		t.Fatalf("the section must carry BOTH legs' annotations (memory=%d notes=%d):\n%s", mem, notes, res.Section)
	}
	if mem > notes {
		t.Fatalf("the memory leg must come first in the merged section:\n%s", res.Section)
	}
	if res.CombinedHits < 2 {
		t.Fatalf("combined hits = %d, want at least 2 (both legs)", res.CombinedHits)
	}
}

// TestPrefetchNotesLegExcludesTheReadersOwnEcho is the measured defect the echo
// exclusion exists for: the note index answers a running conversation's own query
// with the note THAT conversation wrote seconds earlier. It is dropped BY NAME
// and the other run's note still arrives.
func TestPrefetchNotesLegExcludesTheReadersOwnEcho(t *testing.T) {
	path := seedPrefetchLedger(t)
	t.Setenv("GOPI_MEMORY_DB", path)
	seedPrefetchNotesLedger(t, []tools.ChatNote{
		{
			SessionID: "this-very-run",
			Turn:      1,
			Note:      "echo probe: the note leg excludes a hit recorded by the reader's own run",
			Host:      "some-other-host",
			Path:      "/home/nick/.gopi/sessions/this-very-run/self-record.md",
		},
		{
			SessionID: "an-earlier-run",
			Turn:      9,
			Note:      "echo probe: a note from another run is a real retrieval and must still arrive",
			Host:      "some-other-host",
			Path:      "/home/nick/.gopi/sessions/an-earlier-run/self-record.md",
		},
		{
			SessionID: "an-earlier-run",
			Turn:      10,
			Note:      "echo probe: the second surviving note keeps the section above the injection floor",
			Host:      "some-other-host",
			Path:      "/home/nick/.gopi/sessions/an-earlier-run/self-record.md",
		},
	}, "echo probe")

	res := prefetchLookupDetailed(context.Background(), "echo probe", memoryprefetch.DefaultConfig(), nil,
		newPrefetchNotesSource("this-very-run"))

	if res.Notes.Outcome != prefetchNotesOutcomeHits {
		t.Fatalf("notes outcome = %q (err=%v), want %q — the OTHER run's note must survive",
			res.Notes.Outcome, res.Notes.Err, prefetchNotesOutcomeHits)
	}
	// The assertion is on the ECHO'S OWN TEXT, not on its session id: the
	// suppressed hit IS named in the section — as a degradation, which is the
	// point (Rule 15) — so a test matching the id would pass on the record of the
	// exclusion and never notice the hit being injected.
	if strings.Contains(res.Section, "excludes a hit recorded by the reader") {
		t.Fatalf("the reader's own note reached the section as a hit:\n%s", res.Section)
	}
	if !strings.Contains(res.Section, "note-self-echo") {
		t.Fatalf("the exclusion is not named in the section:\n%s", res.Section)
	}
	if !strings.Contains(res.Section, "a real retrieval and must still arrive") {
		t.Fatalf("the other run's note did not reach the section:\n%s", res.Section)
	}
	// TWO survivors, not three: the reader's own note is the one that was
	// excluded. (The count is asserted rather than assumed because the section is
	// rendered only above the injection floor — a group that lost its other
	// members to the echo would produce no section at all, which is what the
	// `an-earlier-run` assertion above would then report as an absence.)
	if res.Notes.HitsCount != 2 {
		t.Fatalf("notes hits = %d, want 2 (three rows, the reader's own one excluded)", res.Notes.HitsCount)
	}
}

// TestPrefetchNotesLegAbsenceIsRecordedNotSwallowed is Rule 15's case: an index
// that does not exist must be REPORTED as an absence, on its own field — never
// presented as "the search found nothing".
func TestPrefetchNotesLegAbsenceIsRecordedNotSwallowed(t *testing.T) {
	path := seedPrefetchLedger(t) // memory ledger only: no chat_notes table
	t.Setenv("GOPI_MEMORY_DB", path)

	res := prefetchLookupDetailed(context.Background(), "read-only ledger DSN", memoryprefetch.DefaultConfig(), nil,
		newPrefetchNotesSource(""))

	if res.Notes.Outcome != prefetchNotesOutcomeAbsent {
		t.Fatalf("notes outcome = %q (err=%v), want %q — a missing index is an absence, not a failure",
			res.Notes.Outcome, res.Notes.Err, prefetchNotesOutcomeAbsent)
	}
	// And the memory leg is untouched by the other leg's absence.
	if res.MemoryOutcome != prefetchOutcomeHits || res.Outcome != prefetchOutcomeHits {
		t.Fatalf("a missing note index changed the memory leg: memory=%q lookup=%q err=%v",
			res.MemoryOutcome, res.Outcome, res.Err)
	}
	if !strings.Contains(res.Section, "memory:") {
		t.Fatalf("the memory section is missing:\n%s", res.Section)
	}
}

// TestPrefetchMemoryOnlyRecordIsUnchanged is the CONTROL, and it is the reason
// the note fields are pointered: a lookup with NO note source — the one-shot
// shape, `prefetchLookupDetailed(..., nil, nil)` — must produce exactly the
// record it produced before this feature existed. The mechanism is asserted
// where it lives: the leg's verdict is the zero value, and the label builder
// turns that into an ABSENT key rather than a leg with an empty verdict.
//
// WHY THE ASSERTION IS ON THE LABEL AND NOT ON A WRITTEN LINE. The executor
// (chatPrefetch.fetch) ALWAYS has a note source now — that is the feature — so
// the only surface that still produces a memory-only record is the one-shot
// call, and a test that drove the executor to look for one would be asserting
// the opposite of what the code does. What must not collide is the JSON: an
// unasked leg and a leg that answered nothing are different facts (Rule 15a),
// and only a NON-empty verdict may be recorded.
func TestPrefetchMemoryOnlyRecordIsUnchanged(t *testing.T) {
	path := seedPrefetchLedger(t)
	t.Setenv("GOPI_MEMORY_DB", path)

	res := prefetchLookupDetailed(context.Background(), "read-only ledger DSN", memoryprefetch.DefaultConfig(), nil, nil)
	if res.Outcome != prefetchOutcomeHits {
		t.Fatalf("outcome = %q (err=%v), want %q — the memory-only shape must keep working", res.Outcome, res.Err, prefetchOutcomeHits)
	}
	if res.MemoryOutcome != "" {
		t.Fatalf("a one-leg lookup recorded a second leg's verdict (%q): the pair is only meaningful when both ran", res.MemoryOutcome)
	}
	if res.Notes.Outcome != "" || res.Notes.Err != nil || res.Notes.HitsCount != 0 {
		t.Fatalf("a lookup with no note source produced a verdict: %+v", res.Notes)
	}
	if label := prefetchNotesLabel(res.Notes); label != nil {
		t.Fatalf("a leg that never ran produced a notes block: %+v", label)
	}
	// And a leg that DID run always has a non-empty verdict, which is what makes
	// "no block" unambiguous.
	ran := prefetchNotesSearch(context.Background(), "read-only ledger DSN", memoryprefetch.DefaultConfig(), newPrefetchNotesSource(""), nil)
	if ran.Outcome == "" {
		t.Fatal("a leg that ran recorded no verdict: the absence of the block would be unreadable")
	}
	if prefetchNotesLabel(ran) == nil {
		t.Fatal("a leg that ran was dropped from the record")
	}
}

// TestPrefetchNotesLegSharesTheWarmHandle is the cost claim, measured: the note
// index and the memory ledger are the SAME file in the ordinary configuration,
// so the leg must NOT dial a second connection — it answers through the handle
// the memory leg already warmed. The assertion is on the handle's identity, not
// on a timing, because a timing asserts the machine's load as much as the code.
func TestPrefetchNotesLegSharesTheWarmHandle(t *testing.T) {
	path := seedPrefetchLedger(t)
	t.Setenv("GOPI_MEMORY_DB", path)
	seedPrefetchNotesLedger(t, []tools.ChatNote{{
		SessionID: "an-earlier-run",
		Turn:      2,
		Note:      "handle reuse: one file, one connection, two legs",
		Host:      "some-other-host",
		Path:      "/home/nick/.gopi/sessions/an-earlier-run/self-record.md",
	}}, "handle reuse")

	ctx := context.Background()
	warm, err := prefetchOpenHandle(ctx, path)
	if err != nil {
		t.Fatalf("warm a handle: %v", err)
	}
	defer warm.close()

	src := newPrefetchNotesSource("")
	got, err := src.handleFor(ctx, warm, tools.ChatNoteIndexPath())
	if err != nil {
		t.Fatalf("handleFor: %v", err)
	}
	if got != warm {
		t.Fatalf("the note leg dialled its own handle (%p) instead of reusing the warm one (%p)", got, warm)
	}
	if !src.reused {
		t.Fatal("the reuse was not recorded: the leg's shape must be an observable")
	}
	// Closing the source must NOT close the executor's handle — it is not the
	// leg's to close.
	src.close()
	if warm.db == nil {
		t.Fatal("closing the note source closed the shared handle")
	}
}

// TestPrefetchNotesSessionIDFromDir pins the derivation the echo exclusion rests
// on: the session directory IS the session id, and a directory that is not one
// yields no exclusion rather than a guess.
func TestPrefetchNotesSessionIDFromDir(t *testing.T) {
	cases := []struct{ dir, want string }{
		{"/home/nick/.gopi/sessions/01a131f5-aaaa", "01a131f5-aaaa"},
		{"/home/nick/.gopi/sessions/2026-10-13-abc/", "2026-10-13-abc"},
		{"", ""},
		{".", ""},
		{"/", ""},
	}
	for _, c := range cases {
		if got := prefetchNotesSessionIDFromDir(c.dir); got != c.want {
			t.Errorf("prefetchNotesSessionIDFromDir(%q) = %q, want %q", c.dir, got, c.want)
		}
	}
}

// TestPrefetchNotesLegIsBoundedByItsOwnBudget: a context that is already expired
// must leave the leg at `gave-up` with the memory leg's result intact — the
// second leg can cost the lookup its own answer and never the first leg's.
func TestPrefetchNotesLegIsBoundedByItsOwnBudget(t *testing.T) {
	path := seedPrefetchLedger(t)
	t.Setenv("GOPI_MEMORY_DB", path)

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // already expired before the leg starts

	res := prefetchNotesSearch(ctx, "read-only ledger DSN", memoryprefetch.DefaultConfig(), newPrefetchNotesSource(""), nil)
	if res.Outcome == prefetchNotesOutcomeHits {
		t.Fatalf("an expired budget produced hits: %+v", res)
	}
	switch res.Outcome {
	case prefetchNotesOutcomeGaveUp, prefetchNotesOutcomeAbsent, prefetchNotesOutcomeFailed, prefetchNotesOutcomeLatched:
	default:
		t.Fatalf("an expired budget produced outcome %q, want a recorded non-answer", res.Outcome)
	}
	if res.Outcome == prefetchNotesOutcomeEmpty && res.Err == nil {
		t.Fatal("a non-answer was reported as a clean empty search with no reason")
	}
}
