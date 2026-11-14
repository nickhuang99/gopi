package tools

// chat_notes_ladder_test.go — the note index's LADDER tier, which exists because
// a PREFETCH query is not a phrase.
//
// THE DEFECT IT PINS (measured on the operator's own index, 2026-12-08): the
// three phrase tiers match the query as one quoted string, so a whole
// conversation line matched nothing — a real chat turn's note leg came back
// `empty`, 0 rows in 12ms, for a query whose answer was in the notes and which
// the memory ledger's ladder answered from its FIRST hit. The ladder is now the
// fourth tier, and the tests below hold BOTH ends of that: it fires when the
// phrase tiers cannot answer, and it changes NOTHING when they can.

import (
	"context"
	"database/sql"
	"strings"
	"testing"

	"github.com/nickhuang99/gopi/internal/testutil"
)

// ladderNoteFixture indexes a note and returns the read-only handle the search
// runs through — the same opener the CLI uses (openChatNotesDB builds the schema
// through the production DDL).
func ladderNoteFixture(t *testing.T, notes []ChatNote) (*sql.DB, context.Context) {
	t.Helper()
	path := testutil.NewTestDBPath(t)
	t.Setenv("GOPI_MEMORY_DB", path)
	t.Setenv("GOPI_CHAT_DB", path)
	ctx := context.Background()
	for _, n := range notes {
		if err := ChatNoteRecord(n); err != nil {
			t.Fatalf("index the fixture note: %v", err)
		}
	}
	db, err := openChatNotesDB(ctx)
	if err != nil {
		t.Fatalf("open the fixture index: %v", err)
	}
	t.Cleanup(func() { db.Close() }) //stylegate:ignore fixture drain-close: the test already read the file, so a close failure is unobservable here
	return db, ctx
}

// TestChatNoteSearchOnLadderTierAnswersAPrefetchShapedQuery is the RED→GREEN case
// the task was filed on: a whole conversation line — many words, none of them
// adjacent to each other in the note — must still find the note, and the tier it
// reports must SAY it was the ladder (the caller records that).
func TestChatNoteSearchOnLadderTierAnswersAPrefetchShapedQuery(t *testing.T) {
	db, ctx := ladderNoteFixture(t, []ChatNote{
		{
			SessionID: "an-earlier-run",
			Turn:      28,
			Note:      "auto-memory 捕获对象从 [IMPORTANT] note 改为 response 正文：标定阈值实测过了",
			Host:      "host-a",
			Path:      "/home/user/.gopi/sessions/an-earlier-run/self-record.md",
		},
	})

	// The PHRASE tiers cannot answer this: "auto memory important note response
	// 捕获 对象 改为" is not a contiguous run in the note above (the note carries
	// the CJK and the brackets the query lacks, and the ASCII words are not
	// adjacent). Before the ladder tier this returned ZERO rows.
	hits, tier, err := ChatNoteSearchOn(ctx, db, "auto memory important note response 捕获 对象 改为", 0)
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if len(hits) == 0 {
		t.Fatalf("the ladder tier did not answer a prefetch-shaped query (tier=%q)", tier)
	}
	if tier != ChatNoteTierLadder {
		t.Fatalf("tier = %q, want %q — the caller records WHICH tier answered", tier, ChatNoteTierLadder)
	}
	if !strings.Contains(hits[0].Note, "捕获对象") {
		t.Fatalf("wrong row: %q", hits[0].Note)
	}
}

// TestChatNoteSearchOnPhraseTiersKeepTheirPrecedence is the CONTROL: the ladder
// is an ADDITION, never a replacement. A query the phrase tier can answer must
// still be answered by that tier — and a query that matches nothing at any tier
// must still report no tier and no rows, not an error.
func TestChatNoteSearchOnPhraseTiersKeepTheirPrecedence(t *testing.T) {
	db, ctx := ladderNoteFixture(t, []ChatNote{
		{
			SessionID: "an-earlier-run",
			Turn:      5,
			Note:      "the phrase tiers must keep their precedence over the ladder",
			Host:      "host-a",
			Path:      "/home/user/.gopi/sessions/an-earlier-run/self-record.md",
		},
	})

	hits, tier, err := ChatNoteSearchOn(ctx, db, "phrase tiers must keep", 0)
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if len(hits) == 0 || tier != ChatNoteTierFTS {
		t.Fatalf("a phrase the fts tier can answer reported tier=%q hits=%d, want %q — the ladder must not pre-empt it",
			tier, len(hits), ChatNoteTierFTS)
	}

	// Nothing matches at ANY tier: zero rows, no tier, NO error. The ladder's
	// walk legitimately finds no subset, and that is an empty search, not a
	// failure (Rule 15: the two are different facts and only one is an error).
	none, tier2, err := ChatNoteSearchOn(ctx, db, "zzzqqq nothing at all here whatsoever", 0)
	if err != nil {
		t.Fatalf("an empty search must not error: %v", err)
	}
	if len(none) != 0 || tier2 != ChatNoteTierNone {
		t.Fatalf("empty search reported tier=%q hits=%d, want %q and 0", tier2, len(none), ChatNoteTierNone)
	}
}

// TestChatNoteSearchOnLadderRespectsTheLimit: the ladder's expression is re-run
// through the SAME limit contract the phrase tiers honour — chatNoteQuery reads
// `limit+1` rows ON PURPOSE, so the render can say "(capped at N)" instead of
// silently ending (see renderChatNoteHits and chatNoteQuery). The fourth tier
// must obey the same contract, and no more: it may return the extra row, and it
// must never return more than that.
func TestChatNoteSearchOnLadderRespectsTheLimit(t *testing.T) {
	notes := []ChatNote{}
	for i := 0; i < 6; i++ {
		notes = append(notes, ChatNote{
			SessionID: "an-earlier-run",
			Turn:      i + 1,
			Note:      "ladder limit probe: the term subset walk must honour the caller's cap",
			Host:      "host-a",
			Path:      "/home/user/.gopi/sessions/an-earlier-run/self-record.md",
		})
	}
	db, ctx := ladderNoteFixture(t, notes)

	hits, tier, err := ChatNoteSearchOn(ctx, db, "ladder limit probe term subset walk caller cap", 2)
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if tier != ChatNoteTierLadder {
		t.Fatalf("tier = %q, want %q", tier, ChatNoteTierLadder)
	}
	const cap = 2
	if len(hits) > cap+1 {
		t.Fatalf("the ladder tier returned %d rows for a cap of %d: the contract is at most cap+1 (the capped indicator)", len(hits), cap)
	}
	// And the production render is what actually enforces the cap — so assert
	// THERE, on the surface a caller sees.
	rendered := renderChatNoteHits("ladder limit probe", tier, hits, cap)
	if !strings.Contains(rendered, "(capped at 2)") {
		t.Fatalf("the render did not report the cap:\n%s", rendered)
	}
	if got := strings.Count(rendered, "round "); got > cap {
		t.Fatalf("the render printed %d rows for a cap of %d:\n%s", got, cap, rendered)
	}
}
