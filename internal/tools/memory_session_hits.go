package tools

// memory_session_hits — the SESSION half of one memory(action=search).
//
// WHAT THIS IS. memory(action=search) used to answer from ONE source: the
// distilled knowledge base (distilled_qa). The round-level note index
// (chat_notes, searched by ChatNoteSearchOn) held a second kind of memory that
// nobody could reach from the tool: what a previous RUN was doing, round by
// round, in its own words. This file joins the two into ONE search result with
// TWO SECTIONS — [distilled] and [session] — and the sections are ordered
// separately because they are different QUANTITIES: a distilled row is ranked by
// the memory engine's value signals (tier, confidence, feedback, staleness),
// while a note hit is ranked by bm25 over raw note text. Mixing them in one
// ordering would compare a confidence to a bm25 score, which is meaningless.
//
// WHY A SLICE AND NOT THE WHOLE SESSION. A session can hold 805 notes (measured
// on this machine's own ledger, 2026-11-07). Pasted whole, one hit would spend
// the entire context budget on a single past run. The evidence shape is instead
// the one that makes a session READABLE: its two BOOKENDS (the first and last
// few rounds, where the run states what it set out to do and how it ended) plus
// a WINDOW around each hit (the neighbouring rounds, so the hit is not a
// sentence torn out of its story). Every omitted stretch is COUNTED, never
// silently elided, so a reader can tell a session with 3 notes from a session
// with 805 and a slice.
//
// The anchoring is explicit: the entry that matched is marked, and the slice's
// gap counts tell the reader exactly how much is not shown.
//
// NO NEW TOOL, NO NEW INDEX, NO NEW DEPENDENCY (Rule 3, Rule 35): this reads the
// SAME index ChatNoteSearchOn already serves and the SAME search it already
// runs, and the caller goes on to widen the window with the ordinary session
// verbs (`gopi session notes <query>`, `gopi session show <id>`).

import (
	"context"
	"database/sql"
	"fmt"
	"strconv"
	"strings"
)

// ---------------------------------------------------------------------------
// Named constants (Rule 1 / Rule 13: every limit, marker and heading is spelled
// once and referenced).
// ---------------------------------------------------------------------------

const (
	// memoryDistilledSectionHeader / memorySessionSectionHeader are the two
	// section markers of a merged search result. They are the CONTRACT: the
	// model reads this word to know which quantity the lines under it are.
	memoryDistilledSectionHeader = "[distilled]"
	memorySessionSectionHeader   = "[session]"

	// memorySessionBookendHead / memorySessionBookendTail are how many of a
	// session's first and last notes are always shown. Three is deliberate: the
	// first rounds state the task and the last rounds state how it finished, and
	// that pair is what makes a past run placeable.
	memorySessionBookendHead = 3
	memorySessionBookendTail = 3

	// memorySessionWindowBefore / memorySessionWindowAfter are how many notes
	// on each side of a HIT are shown, so the hit is read in its neighbourhood
	// rather than alone.
	memorySessionWindowBefore = 2
	memorySessionWindowAfter  = 2

	// memorySessionMaxSessions caps how many distinct sessions the session
	// section describes. The notes of ONE session are wide; five sessions of
	// slices would already be a page.
	memorySessionMaxSessions = 3

	// memorySessionHitsPerSession caps how many hit anchors ONE session
	// contributes. Twenty hits inside one session are one story, not twenty.
	memorySessionHitsPerSession = 2

	// chatNoteTurnWordMark / chatNoteRoundWordMark are the ONE-LETTER marks the
	// slice renders beside a note's number — the same letters the record FILE's
	// stamps use (`t<n> ` / `r<n> `), so a reader who greps `t12 ` in
	// self-record.md finds `t12` here too. Rule 1/13: spelled once.
	chatNoteTurnWordMark  = "t"
	chatNoteRoundWordMark = "r"

	// memorySessionGapLine marks an omitted stretch INSIDE a slice, and
	// memorySessionAnchorMark marks the entry that matched. Both are rendered
	// facts, not decoration: an unmarked slice reads as a complete session.
	memorySessionGapLine    = "… %d note(s) omitted …"
	memorySessionAnchorMark = "← hit"

	// memorySessionEntryIndent indents one slice entry under its session
	// heading, so the anchors line up and a reader scans the turn numbers.
	memorySessionEntryIndent = "    "
)

// ChatNoteSliceEntry is ONE line of a session's evidence slice.
//
// GapBefore is how many of the session's notes were OMITTED immediately before
// this entry — zero for a contiguous step. It rides the entry rather than living
// in a parallel list so the two can never disagree about where a gap sits, and it
// is what makes the slice honest: a reader can add the gaps and the shown
// entries up to the session's real length.
type ChatNoteSliceEntry struct {
	ID int
	// Turn is the note's turn ordinal (0 = the row predates the turn stamp).
	// Round is what a pre-turn row recorded instead; it is carried, never
	// converted, so the render can name the axis the row is really on.
	Turn      int
	Round     int
	Note      string
	Anchor    bool
	GapBefore int
}

// ChatNoteSliceEntries builds the bookend+window slice from a session's notes,
// in ASCENDING order (turn, then id — the order the session happened in).
//
// It is a PURE FUNCTION over data the caller already has (Rule: the shape is
// unit-testable without a database), which is why the boundaries — a hit at the
// very first note, a hit at the very last note, two hits whose windows overlap,
// a session shorter than the bookends themselves — are all pinned by tests that
// never open a handle.
//
// It never invents a row and never drops one silently: what is not shown is
// counted in the next entry's GapBefore.
func ChatNoteSliceEntries(rows []ChatNoteHit, anchors []int) []ChatNoteSliceEntry {
	if len(rows) == 0 {
		return nil
	}
	isAnchor := make(map[int]bool, len(anchors))
	for _, id := range anchors {
		isAnchor[id] = true
	}
	keep := make([]bool, len(rows))
	mark := func(i int) {
		if i >= 0 && i < len(rows) {
			keep[i] = true
		}
	}
	// The head and tail bookends. A session shorter than head+tail keeps
	// everything, and that is the correct answer, not a special case.
	for i := 0; i < memorySessionBookendHead && i < len(rows); i++ {
		mark(i)
	}
	for i := len(rows) - memorySessionBookendTail; i < len(rows); i++ {
		mark(i)
	}
	// A window around every anchor that is actually in this session.
	for i, r := range rows {
		if !isAnchor[r.ID] {
			continue
		}
		for j := i - memorySessionWindowBefore; j <= i+memorySessionWindowAfter; j++ {
			mark(j)
		}
	}
	out := make([]ChatNoteSliceEntry, 0, len(rows))
	gap := 0
	for i, r := range rows {
		if !keep[i] {
			gap++
			continue
		}
		out = append(out, ChatNoteSliceEntry{
			ID:        r.ID,
			Turn:      r.Turn,
			Round:     r.Round,
			Note:      r.Note,
			Anchor:    isAnchor[r.ID],
			GapBefore: gap,
		})
		gap = 0
	}
	return out
}

// memorySessionNotes loads ONE session's notes in the order they happened.
//
// The handle is the caller's, read-only, so this function runs no DDL and
// writes nothing: a search is a read, and the one writer on that path stays
// bumpRetrievalCounts (memory.go), visible and deliberate.
func memorySessionNotes(ctx context.Context, db *sql.DB, sessionID string) ([]ChatNoteHit, error) {
	// ORDERED BY TURN, with the ROUND as the tie-break and the fallback: a row
	// written before the turn stamp carries turn_index=0, so ordering on the turn
	// alone would sort every historical row ahead of the live ones. The pair puts
	// history in its own recorded order first and the turn-numbered rows after
	// it, and `id` — the insertion order — breaks the remaining ties.
	rows, err := db.QueryContext(ctx,
		`SELECT id, `+chatNoteSessionCol+`, `+chatNoteTurnCol+`, `+chatNoteRoundCol+`, `+chatNoteTextCol+` FROM `+chatNotesTableName+
			` WHERE `+chatNoteSessionCol+` = ? ORDER BY `+chatNoteTurnCol+` ASC, `+chatNoteRoundCol+` ASC, id ASC`,
		sessionID)
	if err != nil {
		return nil, err // no-wrap: the caller names the section it was building
	}
	defer rows.Close()
	var out []ChatNoteHit
	for rows.Next() {
		var h ChatNoteHit
		if err := rows.Scan(&h.ID, &h.SessionID, &h.Turn, &h.Round, &h.Note); err != nil {
			return nil, err // no-wrap: same reason as above
		}
		out = append(out, h)
	}
	if err := rows.Err(); err != nil {
		return nil, err // no-wrap: same reason as above
	}
	return out, nil
}

// memorySessionCount reports how many notes the index holds, so the rendered
// section can say WHAT was searched — a count that is absent whenever the index
// is absent, never a fabricated zero (Rule 15a).
func memorySessionCount(ctx context.Context, db *sql.DB) (int, error) {
	var n int
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM `+chatNotesTableName).Scan(&n); err != nil {
		return 0, err // no-wrap: the caller turns an absent index into its own sentence
	}
	return n, nil
}

// MemorySessionSection renders the [session] half of a merged search result.
//
// Three outcomes, each stated rather than implied:
//   - hits: one block per session, each a bookend+window slice with anchors;
//   - the index answered with nothing: a line saying the second source was
//     SEARCHED and had no match (so "nothing found" is a measurement, not a
//     silence);
//   - the index is not on this machine: a line saying so, never a claim that a
//     search found nothing (Rule 15: absence is not emptiness).
//
// An error is returned only for a real failure — a corrupt or unreadable index.
// A missing table/file is the ABSENCE branch, above.
func MemorySessionSection(ctx context.Context, db *sql.DB, query string, limit int) (string, error) {
	sessionCount, err := memorySessionCount(ctx, db)
	if err != nil {
		if ChatNoteIndexMissing(err) {
			return memorySessionSectionHeader + " no round-note index on this machine — no session notes were searched.\n", nil
		}
		return "", fmt.Errorf("memory_search: session note index: %w", err)
	}
	hits, tier, err := ChatNoteSearchOn(ctx, db, query, limit)
	if err != nil {
		if ChatNoteIndexMissing(err) {
			return memorySessionSectionHeader + " no round-note index on this machine — no session notes were searched.\n", nil
		}
		return "", fmt.Errorf("memory_search: session notes: %w", err)
	}
	if len(hits) == 0 {
		return fmt.Sprintf("%s searched %d round note(s) and found nothing matching %q.\n",
			memorySessionSectionHeader, sessionCount, query), nil
	}
	// Group the hits by session, PRESERVING the search's own ranking: the first
	// hit of a session is the best evidence that session holds, and the order the
	// sessions are described in is the order their best hit ranked.
	var order []string
	bySession := map[string][]ChatNoteHit{}
	for _, h := range hits {
		sid := strings.TrimSpace(h.SessionID)
		if sid == "" {
			// A note with no session cannot be sliced (there is no session to
			// walk), so it is not evidence this section can offer. It is
			// reported as such rather than dropped in silence.
			continue
		}
		if _, seen := bySession[sid]; !seen {
			order = append(order, sid)
		}
		if len(bySession[sid]) < memorySessionHitsPerSession {
			bySession[sid] = append(bySession[sid], h)
		}
	}
	if len(order) == 0 {
		return fmt.Sprintf("%s matched %d round note(s), but none of them records a session id, so no session could be walked.\n",
			memorySessionSectionHeader, len(hits)), nil
	}
	if len(order) > memorySessionMaxSessions {
		order = order[:memorySessionMaxSessions]
	}

	var b strings.Builder
	fmt.Fprintf(&b, "%s %d session(s) matched %q in the round-note index (%d note row(s) on this machine, via %s).\n",
		memorySessionSectionHeader, len(order), query, sessionCount, tier)
	fmt.Fprintf(&b, "Each block below is an evidence SLICE: the session's first %d and last %d note(s), plus ±%d around every hit; %q marks the matching note.\n",
		memorySessionBookendHead, memorySessionBookendTail, memorySessionWindowBefore, memorySessionAnchorMark)
	fmt.Fprintf(&b, "Widen it with: gopi session notes %q  /  gopi session show <session-id>\n", query)

	for _, sid := range order {
		all, err := memorySessionNotes(ctx, db, sid)
		if err != nil {
			return "", fmt.Errorf("memory_search: session %s notes: %w", sid, err)
		}
		anchors := make([]int, 0, len(bySession[sid]))
		for _, h := range bySession[sid] {
			anchors = append(anchors, h.ID)
		}
		slice := ChatNoteSliceEntries(all, anchors)
		fmt.Fprintf(&b, "--- %s (%d note(s), %d hit(s)) ---\n", sid, len(all), len(anchors))
		for _, e := range slice {
			if e.GapBefore > 0 {
				fmt.Fprintf(&b, memorySessionEntryIndent+memorySessionGapLine+"\n", e.GapBefore)
			}
			// THE UNIT IS NAMED, and it is the row's OWN: a turn-numbered row
			// prints `t<N>`, a historical one prints `r<N>` — the axis it really
			// recorded. Printing a round as a turn (or a `t?` for a row whose
			// number exists on the other axis) would be exactly the silent
			// substitution Rule 15 forbids.
			mark, number := chatNoteRoundWordMark, e.Round
			if e.Turn > 0 {
				mark, number = chatNoteTurnWordMark, e.Turn
			}
			stamp := mark + "?"
			if number > 0 {
				stamp = mark + strconv.Itoa(number)
			}
			anchor := ""
			if e.Anchor {
				anchor = " " + memorySessionAnchorMark
			}
			fmt.Fprintf(&b, "%sid=%d %s %s%s\n", memorySessionEntryIndent, e.ID, stamp, truncateChatNote(e.Note), anchor)
		}
	}
	return b.String(), nil
}

// memorySessionSection is the render-side wrapper: it opens the note index
// READ-ONLY for the duration of one search and closes it again.
//
// IT IS BEST-EFFORT BY CONTRACT, and it says so: a failure here must not take
// the distilled answer down with it — the memory engine's own result is what the
// caller asked for first. A failure is therefore RENDERED into the section (Rule
// 15b: never swallowed silently), and the section then reads as the failure it
// is rather than as "no session notes matched".
func memorySessionSection(query string, limit int) string {
	path := strings.TrimSpace(ChatNoteIndexPath())
	if path == "" || !fileExists(path) {
		return memorySessionSectionHeader + " no round-note index on this machine — no session notes were searched.\n"
	}
	ctx := context.Background()
	db, err := openKnowledgeBaseDB(path, true)
	if err != nil {
		return fmt.Sprintf("%s the round-note index could not be opened (%v).\n", memorySessionSectionHeader, err)
	}
	defer db.Close()
	section, err := MemorySessionSection(ctx, db, query, limit)
	if err != nil {
		return fmt.Sprintf("%s the round-note index search failed (%v).\n", memorySessionSectionHeader, err)
	}
	return section
}

// MergeMemorySearchSections joins the distilled half and the session half into
// the ONE result a caller receives, and it is where the two section headers are
// placed.
//
// It is a separate function because the contract it implements is a test
// surface of its own: whatever the engine produces for the distilled half (hits,
// the empty hint, procedural rows riding along), the merged text always names
// which half each line came from, and the session half is always LAST — the
// distilled answer is the tool's original product and keeps its place.
func MergeMemorySearchSections(distilled, session string) string {
	distilled = strings.TrimSpace(distilled)
	session = strings.TrimSpace(session)
	// The now= anchor belongs on the RESULT's first line, not one level down
	// inside the distilled section: every age= in the result was measured
	// against it, so a reader who sees the ages must see the anchor ahead of
	// any hit. The distilled half carries it (it is what the renderer emits);
	// it is lifted here rather than rendered here, so the renderer stays the
	// ONE place that decides the anchor's shape and this function stays the
	// ONE place that decides the section order.
	anchor := ""
	if line, rest, ok := strings.Cut(distilled, "\n"); ok && strings.HasPrefix(line, memoryNowAnchorPrefix) {
		anchor, distilled = line, rest
	}
	// The two labels are placed HERE and only here. A half already carrying its
	// own header keeps it — MemorySessionSection emits its own, so a caller that
	// built the session text by hand gets the same shape (no doubled marker).
	labelled := func(body, header string) string {
		if strings.HasPrefix(body, header) {
			return body
		}
		return header + "\n" + body
	}
	body := func() string {
		switch {
		case distilled == "" && session == "":
			return ""
		case distilled == "":
			return labelled(session, memorySessionSectionHeader)
		case session == "":
			return labelled(distilled, memoryDistilledSectionHeader)
		default:
			return labelled(distilled, memoryDistilledSectionHeader) + "\n\n" + labelled(session, memorySessionSectionHeader)
		}
	}()
	if anchor == "" || body == "" {
		return body
	}
	return anchor + "\n" + body
}
