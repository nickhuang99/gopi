package memoryprefetch

import (
	"errors"
	"strings"
	"testing"
)

// note fixture: a note that is long enough to be worth injecting, recorded on
// another machine at a known path.
func sampleNote(id int, session string) NoteHit {
	return NoteHit{
		Record:    id,
		Session:   session,
		Round:     7,
		Note:      "prefetch 只查 memory 知识库一条腿；round 级 [IMPORTANT] 笔记索引必须并列检索",
		Host:      "nick-HP-Laptop",
		Path:      "/home/nick/.gopi/sessions/2026-12-08-abc/notes.jsonl",
		CreatedAt: 1765000000,
	}
}

// TestNoteHits_CarryTheirSourceIndexAndProvenance is the acceptance case for the
// SECOND leg: a note hit must reach the section (a) through the SAME renderer the
// memory leg uses, (b) annotated with the INDEX it came from, and (c) carrying
// the machine and the FULL record path — the two facts that tell two hosts whose
// home directories are the same string apart.
func TestNoteHits_CarryTheirSourceIndexAndProvenance(t *testing.T) {
	hits, degs := NoteHits([]NoteHit{sampleNote(48, "other-run"), sampleNote(49, "another-run")}, "", DefaultConfig())
	if len(degs) != 0 {
		t.Fatalf("unexpected degradations: %+v", degs)
	}
	if len(hits) != 2 {
		t.Fatalf("got %d hits, want 2", len(hits))
	}
	for i, h := range hits {
		if h.ID != 48+i {
			t.Fatalf("hit %d identity: got %d, want %d (#N addresses the NOTE index)", i, h.ID, 48+i)
		}
		if h.Source != noteIndexOrigin {
			t.Fatalf("hit %d source: got %q, want %q", i, h.Source, noteIndexOrigin)
		}
		if h.Note == "" {
			t.Fatalf("hit %d carries no note text", i)
		}
		if !strings.Contains(h.TierCell, "nick-HP-Laptop") {
			t.Fatalf("hit %d provenance cell does not name the host: %q", i, h.TierCell)
		}
		if !strings.Contains(h.TierCell, "/home/nick/.gopi/sessions/2026-12-08-abc/notes.jsonl") {
			t.Fatalf("hit %d provenance cell does not carry the FULL path: %q", i, h.TierCell)
		}
		if !strings.Contains(h.TierCell, "round 7") {
			t.Fatalf("hit %d provenance cell does not name the round: %q", i, h.TierCell)
		}
	}

	// The rendered line must carry BOTH annotations, and it is the section the
	// model reads — so this is the assertion that the second leg is visible
	// rather than merely present.
	seg := RenderSegment(hits, nil, DefaultConfig())
	for i, line := range []string{seg.Hits[0].Line, seg.Hits[1].Line} {
		if !strings.Contains(line, "["+noteIndexOrigin+":") {
			t.Fatalf("hit %d line does not name the note index: %q", i, line)
		}
		if !strings.Contains(line, string(AdjudicabilityRecordedNote)) {
			t.Fatalf("hit %d line does not state that it is a recorded note: %q", i, line)
		}
		if !strings.Contains(line, "#4") {
			t.Fatalf("hit %d line carries no record address: %q", i, line)
		}
	}
}

// TestNoteHits_ExcludeTheReadersOwnEcho is the measured case the exclusion
// exists for: the note index answers a running conversation's own query with the
// note that conversation just wrote. That row is not a retrieval — it is text
// already in the reader's context — so it is dropped BY NAME (recorded, never
// silently) and a hit from another run still comes through.
func TestNoteHits_ExcludeTheReadersOwnEcho(t *testing.T) {
	notes := []NoteHit{sampleNote(48, "this-run"), sampleNote(49, "other-run")}
	hits, degs := NoteHits(notes, "this-run", DefaultConfig())

	if len(hits) != 1 {
		t.Fatalf("got %d hits, want 1 (the echo must be dropped)", len(hits))
	}
	if hits[0].ID != 49 {
		t.Fatalf("kept hit %d, want the OTHER run's note (49)", hits[0].ID)
	}
	deg, ok := findDegradationIn(degs, DegradationNoteSelfEcho)
	if !ok {
		t.Fatalf("the suppressed echo is not recorded; degradations=%+v", degs)
	}
	if !strings.Contains(deg.Detail, "this-run") {
		t.Fatalf("the echo degradation does not name the session: %q", deg.Detail)
	}

	// And with no session id, nothing is excluded — the honest answer for a
	// caller that cannot say who it is.
	all, _ := NoteHits(notes, "", DefaultConfig())
	if len(all) != 2 {
		t.Fatalf("with no session id: got %d hits, want 2", len(all))
	}
}

// TestNoteHits_RefuseAnEmptyRowByName: a row with no text is dropped and NAMED.
// An anonymous line in an injected block costs tokens to say nothing (Rule 15).
func TestNoteHits_RefuseAnEmptyRowByName(t *testing.T) {
	blank := sampleNote(50, "other-run")
	blank.Note = "   \n  "
	hits, degs := NoteHits([]NoteHit{sampleNote(48, "other-run"), blank}, "", DefaultConfig())
	if len(hits) != 1 || hits[0].ID != 48 {
		t.Fatalf("got %d hits, want only 48", len(hits))
	}
	if _, ok := findDegradationIn(degs, DegradationNoteHitEmpty); !ok {
		t.Fatalf("the empty row is not recorded; degradations=%+v", degs)
	}
}

// TestNoteHits_StateEveryAbsence: an unset clock, host, path, round or session
// must read as a STATED absence rather than a blank a reader could take for a
// formatting accident — the same discipline the memory leg's cell follows.
func TestNoteHits_StateEveryAbsence(t *testing.T) {
	hits, _ := NoteHits([]NoteHit{{Record: 51, Note: strings.Repeat("x", 80)}}, "", DefaultConfig())
	if len(hits) != 1 {
		t.Fatalf("got %d hits, want 1", len(hits))
	}
	cell, line := hits[0].TierCell, RenderSegment(hits, nil, DefaultConfig()).Hits[0].Line
	for _, want := range []string{noteHostUnknown, noteRoundUnknown, notePathUnknown, noteSessionAbsent, noteTimeUnknown} {
		if !strings.Contains(cell, want) {
			t.Fatalf("cell %q does not state the absence %q", cell, want)
		}
	}
	if !strings.Contains(line, "unknown") {
		t.Fatalf("line does not carry the stated absences: %q", line)
	}
}

// TestNoteHits_MergeWithTheMemoryLegIntoOneSection is the EPIC's acceptance case,
// and it is the BUG REPRODUCTION: the memory ledger has NOTHING for this query
// (the measured origin of this work — a design decision that only the note index
// held), the note index has the answer, and the injected section must therefore
// exist, be built by the one renderer, and carry the note's own line.
//
// It is driven through InjectSection, the single entry point a prefetched block
// takes into a model request — so what is asserted here is the block the model
// would read, not a helper's intermediate value.
func TestNoteHits_MergeWithTheMemoryLegIntoOneSection(t *testing.T) {
	cfg := DefaultConfig() // MinInjectHits = 2
	noteHits, _ := NoteHits([]NoteHit{sampleNote(48, "other-run")}, "", cfg)
	if len(noteHits) != 1 {
		t.Fatalf("fixture: got %d note hits, want 1", len(noteHits))
	}
	memoryHits := []Hit{{ID: 907, Topic: "text2img", Answer: "text2img 的 20px 配方在我们自己的库里", Tier: tierHot, Confidence: 1}}

	// TWO legs, one hit each: the combined count is what the floor is judged on,
	// which is exactly how the note index rescues a lookup the ledger could not
	// answer. The merged order is the memory leg's first.
	merged := append(append([]Hit{}, memoryHits...), noteHits...)
	section, ok := InjectSection("", merged, cfg)
	if !ok {
		t.Fatal("the merged legs produced no section: the note leg did NOT rescue the empty memory leg")
	}
	if !strings.Contains(section, noteIndexOrigin) {
		t.Fatalf("section does not carry the note leg's line:\n%s", section)
	}
	if !strings.Contains(section, memoryLedgerOrigin) {
		t.Fatalf("section does not carry the memory leg's line:\n%s", section)
	}
	if strings.Index(section, memoryLedgerOrigin) > strings.Index(section, noteIndexOrigin) {
		t.Fatalf("the memory leg must come first in the merged section:\n%s", section)
	}

	// ONE hit (either leg alone) is still refused by the floor — the pre-existing
	// contract is not widened by the second leg. This is the control that proves
	// the case above passed because BOTH legs contributed.
	if _, ok := InjectSection("", noteHits, cfg); ok {
		t.Fatal("a single note hit must not pass the injection floor: the floor still counts hits")
	}
	if _, ok := InjectSection("", memoryHits, cfg); ok {
		t.Fatal("a single memory hit must not pass the injection floor")
	}
}

// TestInjectSection_CarriesTheCallersDegradations: a loss the CALLER knows about
// (today: the note index could not be read at all) must reach the section the
// model reads, and it must not be able to smuggle free text in — the renderer
// still owns the block's shape.
func TestInjectSection_CarriesTheCallersDegradations(t *testing.T) {
	cfg := DefaultConfig()
	hits := []Hit{
		{ID: 1, Answer: strings.Repeat("a", 30), Tier: tierHot},
		{ID: 2, Answer: strings.Repeat("b", 30), Tier: tierWarm},
	}
	caller := DegradationNoteLegFailed(errors.New("no such table: chat_notes"))
	section, ok := InjectSection("", hits, cfg, caller)
	if !ok {
		t.Fatal("expected a section")
	}
	if !strings.Contains(section, caller.Detail) {
		t.Fatalf("the caller's degradation did not reach the section:\n%s", section)
	}
	if !strings.Contains(section, string(DegradationNotesLegFailed)) {
		t.Fatalf("the section does not name the note leg's failure:\n%s", section)
	}
}

// TestNoteRecordPrefixRoundTrips: the prefix the renderer writes and the prefix a
// caller strips are ONE function — a reader who copies "#48" out of an injected
// line must be able to hand it back to the note index unchanged.
func TestNoteRecordPrefixRoundTrips(t *testing.T) {
	if got := NoteRecordPrefix(48); got != "#48" {
		t.Fatalf("got %q, want #48", got)
	}
	if !strings.HasPrefix(NoteRecordPrefix(7), "#") {
		t.Fatal("the prefix lost its address marker")
	}
	if got := NoteTimeCell(0); got != noteTimeUnknown {
		t.Fatalf("an unset clock rendered as %q, want %q", got, noteTimeUnknown)
	}
	if got := NoteTimeCell(1765000000); got != "1765000000" {
		t.Fatalf("a set clock rendered as %q", got)
	}
}
