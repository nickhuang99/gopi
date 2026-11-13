package tools

// memory_session_hits_test.go — the [distilled] / [session] merge and the
// bookend+window slice, tested against the REAL production surfaces.
//
// WHAT IT PINS, in order: (1) a query only the distilled ledger answers still
// yields a [session] section that SAYS the note index was searched and had no
// match — a silent omission would read as "no past run touched this", which is a
// different fact; (2) a query only the round-note index answers yields a
// [session] section carrying the session id and a slice, with a [distilled]
// section that keeps its no-distillation hint; (3) neither source answering is
// still a non-empty, explicit result; (4) both answering renders TWO sections in
// a fixed order, each keeping its own ranking, with the distilled half's text
// unchanged by the merge; (5) the slice's boundaries — a hit at the very first
// note, a hit at the very last one, overlapping windows, a session with fewer
// notes than the bookends, and the ARITHMETIC that every omitted note is counted
// exactly once.
//
// The database comes from the scaffold (Rule 32): testutil.NewTestDBPath pins
// GOPI_MEMORY_DB at a fresh per-test file, which is also the path chatDBPath()
// resolves once GOPI_CHAT_DB is cleared — so both halves of the search land on
// the fixture and never on the operator's live ledger.

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/nick/gopi/internal/testutil"
)

// sessionFixture pins a fresh ledger for one test and clears the chat-DB
// override so the note index is the SAME file the distilled search reads.
func sessionFixture(t *testing.T) string {
	t.Helper()
	path := testutil.NewTestDBPath(t)
	t.Setenv(chatDBEnv, "")
	clearChatNoteHostEnv(t)
	return path
}

// seedNote writes one turn-numbered note into the index through the production
// writer.
func seedNote(t *testing.T, session string, turn int, note string) {
	t.Helper()
	if err := ChatNoteRecord(ChatNote{SessionID: session, Turn: turn, Note: note}); err != nil {
		t.Fatalf("seed note (session=%s turn=%d): %v", session, turn, err)
	}
}

// seedLesson writes one distilled row through the production writer.
func seedLesson(t *testing.T, topic, question, answer string) {
	t.Helper()
	if _, err := MemoryStore(topic, question, answer, "fixture", nil); err != nil {
		t.Fatalf("seed lesson %q: %v", topic, err)
	}
}

// TestMemorySearchDistilledOnlyStillNamesTheSessionHalf: a query the note index
// cannot answer must SAY so. The distilled half answers it, the session half
// reports a search that found nothing — never silence (Rule 15).
func TestMemorySearchDistilledOnlyStillNamesTheSessionHalf(t *testing.T) {
	sessionFixture(t)
	seedLesson(t, "plughdistilledonly cache", "why is plughdistilledonly slow?",
		"because the plughdistilledonly cache is cold; warm it first")
	seedNote(t, "sess-unrelated", 1, "a note about something else entirely")

	out, err := MemorySearch("plughdistilledonly", 5, "")
	if err != nil {
		t.Fatalf("MemorySearch: %v", err)
	}
	distilled, session := splitSections(t, out)
	if !strings.Contains(distilled, "plughdistilledonly cache") {
		t.Fatalf("the distilled half lost its hit:\n%s", out)
	}
	if !strings.Contains(session, "found nothing matching") {
		t.Fatalf("the session half must report a search with no match, not go silent:\n%s", out)
	}
	if strings.Contains(session, "[distilled]") {
		t.Fatalf("the session half swallowed the other section:\n%s", out)
	}
}

// TestMemorySearchSessionOnlyCarriesSessionIDAndSlice: a query only the
// round-note index answers must produce the session id, the matching note, and
// the anchor mark — that slice is the evidence a later `gopi session show` is
// followed up with.
func TestMemorySearchSessionOnlyCarriesSessionIDAndSlice(t *testing.T) {
	sessionFixture(t)
	const sid = "sess-sessiononly-0001"
	seedNote(t, sid, 1, "started the seszsolo investigation")
	seedNote(t, sid, 2, "the seszsolo marker appeared in round two")
	seedNote(t, sid, 3, "finished the seszsolo investigation")

	out, err := MemorySearch("seszsolo", 5, "")
	if err != nil {
		t.Fatalf("MemorySearch: %v", err)
	}
	distilled, session := splitSections(t, out)
	if !strings.Contains(distilled, "No distilled memory found") {
		t.Fatalf("the distilled half must keep its own no-hit hint:\n%s", out)
	}
	for _, want := range []string{sid, "seszsolo marker appeared", memorySessionAnchorMark, "3 note(s)"} {
		if !strings.Contains(session, want) {
			t.Fatalf("session half missing %q:\n%s", want, session)
		}
	}
	if strings.Contains(session, "found nothing matching") {
		t.Fatalf("the session half found a hit and said it found nothing:\n%s", session)
	}
}

// TestMemorySearchNeitherHalfAnswers: the honest empty. Both sections are
// present, both say so, and the text is non-empty — the acceptance criterion is
// an explicit "nothing found", not a blank line.
func TestMemorySearchNeitherHalfAnswers(t *testing.T) {
	sessionFixture(t)
	seedLesson(t, "unrelated lesson", "an unrelated question", "an unrelated answer")
	seedNote(t, "sess-other", 1, "another unrelated note")

	out, err := MemorySearch("zzzqqqxxx", 5, "")
	if err != nil {
		t.Fatalf("MemorySearch: %v", err)
	}
	if strings.TrimSpace(out) == "" {
		t.Fatal("a clean no-hit must be an explicit sentence, not an empty result")
	}
	distilled, session := splitSections(t, out)
	if !strings.Contains(distilled, "No distilled memory found") {
		t.Fatalf("distilled half must state its no-hit:\n%s", out)
	}
	if !strings.Contains(session, "found nothing matching") || !strings.Contains(session, "zzzqqqxxx") {
		t.Fatalf("session half must state its no-hit and the query it searched:\n%s", out)
	}
}

// TestMemorySearchBothHalvesRenderAsTwoSections: both sources answer, and the
// result carries TWO labels in a fixed order, each half keeping its own content.
// The distilled text must be byte-identical to what the pre-feature renderer
// produced for the same rows — the merge ADDS a section, it does not rewrite one.
func TestMemorySearchBothHalvesRenderAsTwoSections(t *testing.T) {
	sessionFixture(t)
	const marker = "bifurcatemarker"
	seedLesson(t, marker+" cache", "how does "+marker+" behave?", "warm the "+marker+" cache")
	const sid = "sess-bothhalves-0002"
	seedNote(t, sid, 7, "round seven mentions "+marker+" for the first time")

	out, err := MemorySearch(marker, 5, "")
	if err != nil {
		t.Fatalf("MemorySearch: %v", err)
	}
	distilled, session := splitSections(t, out)
	if !strings.Contains(distilled, marker+" cache") {
		t.Fatalf("distilled half lost its hit:\n%s", out)
	}
	if !strings.Contains(session, sid) || !strings.Contains(session, marker) {
		t.Fatalf("session half lost its hit:\n%s", out)
	}
	// The distilled half must be exactly what the renderer produces on its own.
	rows, err := MemorySearchHits(marker, 5, "")
	if err != nil {
		t.Fatalf("MemorySearchHits: %v", err)
	}
	if len(rows) == 0 {
		t.Fatal("fixture precondition: the distilled search must have a hit")
	}
	if !strings.Contains(distilled, "A: warm the "+marker+" cache") {
		t.Fatalf("distilled half is not the engine's own render:\n%s", distilled)
	}
}

// TestMergeMemorySearchSections never loses a half: the merge is the ONE place
// the two labels are placed, so its degenerate inputs are pinned directly.
func TestMergeMemorySearchSections(t *testing.T) {
	if got := MergeMemorySearchSections("", ""); got != "" {
		t.Fatalf("two empty halves = %q, want empty", got)
	}
	if got := MergeMemorySearchSections("only distilled", ""); got != memoryDistilledSectionHeader+"\nonly distilled" {
		t.Fatalf("distilled-only merge = %q", got)
	}
	if got := MergeMemorySearchSections("", "only session"); got != memorySessionSectionHeader+"\nonly session" {
		t.Fatalf("session-only merge = %q", got)
	}
	// A half that already carries its own label keeps it — no doubled marker.
	if got := MergeMemorySearchSections("D", memorySessionSectionHeader+"\nS"); strings.Count(got, memorySessionSectionHeader) != 1 {
		t.Fatalf("a labelled session half was labelled twice: %q", got)
	}
	both := MergeMemorySearchSections("D", "S")
	if strings.Index(both, memoryDistilledSectionHeader) > strings.Index(both, memorySessionSectionHeader) {
		t.Fatalf("the distilled section must come first, got %q", both)
	}
	if !strings.Contains(both, memoryDistilledSectionHeader) || !strings.Contains(both, memorySessionSectionHeader) {
		t.Fatalf("a two-half merge must carry both labels, got %q", both)
	}
}

// TestChatNoteSliceEntriesShortSessionKeepsEverything: a session with fewer
// notes than the bookends is shown WHOLE — the correct answer, not a special
// case, and with no gap lines to invent.
func TestChatNoteSliceEntriesShortSessionKeepsEverything(t *testing.T) {
	rows := sliceRows(1, memorySessionBookendHead+memorySessionBookendTail-1)
	got := ChatNoteSliceEntries(rows, []int{2})
	if len(got) != len(rows) {
		t.Fatalf("short session: kept %d of %d entries", len(got), len(rows))
	}
	for _, e := range got {
		if e.GapBefore != 0 {
			t.Fatalf("short session invented a gap of %d before id=%d", e.GapBefore, e.ID)
		}
	}
	assertAnchors(t, got, []int{2})
}

// TestChatNoteSliceEntriesHitAtTheFirstNote: the window clamps at the head — a
// slice that asked for notes before the first one must not panic or wrap.
func TestChatNoteSliceEntriesHitAtTheFirstNote(t *testing.T) {
	rows := sliceRows(1, 40)
	got := ChatNoteSliceEntries(rows, []int{1})
	if len(got) == 0 {
		t.Fatal("no entries kept")
	}
	if got[0].ID != 1 {
		t.Fatalf("the first entry is id=%d, want the session's first note (id=1)", got[0].ID)
	}
	if got[0].GapBefore != 0 {
		t.Fatalf("a gap was invented before the session's first note: %d", got[0].GapBefore)
	}
	assertAnchors(t, got, []int{1})
	assertEveryNoteAccountedFor(t, rows, got)
}

// TestChatNoteSliceEntriesHitAtTheLastNote: the mirror boundary — the window
// clamps at the tail.
func TestChatNoteSliceEntriesHitAtTheLastNote(t *testing.T) {
	rows := sliceRows(1, 40)
	last := rows[len(rows)-1].ID
	got := ChatNoteSliceEntries(rows, []int{last})
	if got[len(got)-1].ID != last {
		t.Fatalf("last kept entry is id=%d, want %d", got[len(got)-1].ID, last)
	}
	if got[len(got)-1].GapBefore != 0 {
		t.Fatalf("a gap was invented before the session's last note: %d", got[len(got)-1].GapBefore)
	}
	assertAnchors(t, got, []int{last})
	assertEveryNoteAccountedFor(t, rows, got)
}

// TestChatNoteSliceEntriesOverlappingWindows: two hits close enough that their
// windows overlap must produce ONE contiguous run — no duplicated entry and no
// gap between them.
func TestChatNoteSliceEntriesOverlappingWindows(t *testing.T) {
	rows := sliceRows(1, 60)
	a, b := 30, 31
	got := ChatNoteSliceEntries(rows, []int{a, b})
	seen := map[int]int{}
	for _, e := range got {
		seen[e.ID]++
	}
	for id, n := range seen {
		if n > 1 {
			t.Fatalf("id=%d appeared %d times — a window was added twice", id, n)
		}
	}
	idx := map[int]int{}
	for i, e := range got {
		idx[e.ID] = i
	}
	if idx[b]-idx[a] != 1 {
		t.Fatalf("two adjacent hits are not adjacent in the slice (%d then %d)", idx[a], idx[b])
	}
	if got[idx[b]].GapBefore != 0 {
		t.Fatalf("a gap was invented between two adjacent hits: %d", got[idx[b]].GapBefore)
	}
	assertAnchors(t, got, []int{a, b})
	assertEveryNoteAccountedFor(t, rows, got)
}

// TestChatNoteSliceEntriesCountsEveryOmission is the honesty contract: the shown
// entries plus the counted gaps must equal the session's real length, for a
// session far longer than any slice. Measured on this machine's own ledger, one
// session holds 805 notes — a slice that elided them silently would read as a
// whole session.
func TestChatNoteSliceEntriesCountsEveryOmission(t *testing.T) {
	rows := sliceRows(1, 805)
	got := ChatNoteSliceEntries(rows, []int{400})
	if len(got) >= len(rows) {
		t.Fatalf("a 805-note session was not sliced at all (%d entries)", len(got))
	}
	assertEveryNoteAccountedFor(t, rows, got)
	assertAnchors(t, got, []int{400})
	// Round numbers ascend and no entry is out of order.
	for i := 1; i < len(got); i++ {
		if got[i].ID < got[i-1].ID {
			t.Fatalf("slice is out of order at %d: id=%d after id=%d", i, got[i].ID, got[i-1].ID)
		}
	}
}

// TestChatNoteSliceEntriesUnknownAnchorIsIgnored: an anchor that is not in the
// session (the hit was filtered out) must not create a phantom window.
func TestChatNoteSliceEntriesUnknownAnchorIsIgnored(t *testing.T) {
	rows := sliceRows(1, 40)
	got := ChatNoteSliceEntries(rows, []int{9999})
	for _, e := range got {
		if e.Anchor {
			t.Fatalf("a non-existent anchor was marked on id=%d", e.ID)
		}
	}
	assertEveryNoteAccountedFor(t, rows, got)
	if entries := ChatNoteSliceEntries(nil, []int{1}); entries != nil {
		t.Fatalf("an empty session produced %d entries, want none", len(entries))
	}
}

// TestMemorySessionSectionStatesAbsentIndex: a ledger with no chat_notes table
// is an ABSENCE, and the section says so — it never claims a search found
// nothing there (Rule 15: absence is not emptiness).
func TestMemorySessionSectionStatesAbsentIndex(t *testing.T) {
	path := sessionFixture(t)
	// Build a distilled-only ledger: open it through the memory engine, which
	// does NOT create the note index, then hand a read-only handle to the
	// section renderer.
	if _, err := MemorySearch("warm the cache", 1, ""); err != nil {
		t.Fatalf("MemorySearch: %v", err)
	}
	db, err := openKnowledgeBaseDB(path, true)
	if err != nil {
		t.Fatalf("open fixture read-only: %v", err)
	}
	defer db.Close()
	got, err := MemorySessionSection(context.Background(), db, "anything", 5)
	if err != nil {
		t.Fatalf("an absent index must not be an error, got %v", err)
	}
	if !strings.Contains(got, "no round-note index") {
		t.Fatalf("absent index rendered as %q", got)
	}
	if strings.Contains(got, "found nothing matching") {
		t.Fatalf("absence was rendered as emptiness:\n%s", got)
	}
}

// ---------------------------------------------------------------------------
// helpers
// ---------------------------------------------------------------------------

// splitSections cuts a merged result into its two halves, asserting both labels
// are present and in order — the shape every test above reasons about.
func splitSections(t *testing.T, out string) (distilled, session string) {
	t.Helper()
	di := strings.Index(out, memoryDistilledSectionHeader)
	si := strings.Index(out, memorySessionSectionHeader)
	if di < 0 {
		t.Fatalf("result has no %s section:\n%s", memoryDistilledSectionHeader, out)
	}
	if si < 0 {
		t.Fatalf("result has no %s section:\n%s", memorySessionSectionHeader, out)
	}
	if di > si {
		t.Fatalf("sections are out of order (%s after %s):\n%s",
			memoryDistilledSectionHeader, memorySessionSectionHeader, out)
	}
	body := out[di+len(memoryDistilledSectionHeader) : si]
	return body, out[si+len(memorySessionSectionHeader):]
}

// sliceRows builds an ordered session of n notes with ids 1..n.
func sliceRows(from, to int) []ChatNoteHit {
	rows := make([]ChatNoteHit, 0, to-from+1)
	for i := from; i <= to; i++ {
		rows = append(rows, ChatNoteHit{
			ID:        i,
			SessionID: "sess-slice",
			Turn:      i,
			Note:      fmt.Sprintf("note %d", i),
		})
	}
	return rows
}

// assertAnchors requires the given ids to be exactly the marked entries.
func assertAnchors(t *testing.T, entries []ChatNoteSliceEntry, want []int) {
	t.Helper()
	got := map[int]bool{}
	for _, e := range entries {
		if e.Anchor {
			got[e.ID] = true
		}
	}
	if len(got) != len(want) {
		t.Fatalf("anchors = %v, want %v", got, want)
	}
	for _, id := range want {
		if !got[id] {
			t.Fatalf("id=%d is not marked as an anchor (anchors=%v)", id, got)
		}
	}
}

// assertEveryNoteAccountedFor is the arithmetic the slice promises: every note
// of the session is either SHOWN once or COUNTED in exactly one gap.
func assertEveryNoteAccountedFor(t *testing.T, rows []ChatNoteHit, entries []ChatNoteSliceEntry) {
	t.Helper()
	shown := len(entries)
	gaps := 0
	for _, e := range entries {
		gaps += e.GapBefore
	}
	if shown+gaps != len(rows) {
		t.Fatalf("shown %d + omitted %d = %d, but the session holds %d note(s)",
			shown, gaps, shown+gaps, len(rows))
	}
}
