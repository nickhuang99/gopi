package tools

import (
	"path/filepath"
	"strings"
	"testing"
)

// syncDifferingBodyTopic / Question / Answers are the fixture of the
// differing-body regression: ONE exact dedup key (topic + question), TWO
// different answers. The second answer is what the fold used to eat in
// silence — the key matches, so nothing is stored, and before this test
// existed nothing said so.
const (
	syncDifferingBodyTopic   = "brew: differing body regression"
	syncDifferingBodyQ       = "同键不同正文时正文去哪了 where does a same-key different body go?"
	syncDifferingBodyAnswerA = "正文 A：exact key 命中时以库里已有的这份为准，这是最终知识。"
	syncDifferingBodyAnswerB = "正文 B：同键但内容完全不同的一份，按 exact key 折叠后不会入库。"
)

// syncDifferingBodyPatch writes a one-row patch whose answer is `answer` and
// returns its path. Both imports in the test go through THIS builder so the
// patch format cannot drift between the two halves of the assertion.
func syncDifferingBodyPatch(t *testing.T, answer string) string {
	t.Helper()
	patch := &SyncPatch{
		Version: syncPatchVersion,
		Node:    "hp-differing",
		Rows: []SyncRow{{
			Topic:    syncDifferingBodyTopic,
			Question: syncDifferingBodyQ,
			Answer:   answer,
			Keywords: []string{"exact key 判重", "正文 body", "静默丢弃 silent drop"},
		}},
	}
	path := filepath.Join(t.TempDir(), "patch.json")
	if err := WriteSyncPatch(patch, path); err != nil {
		t.Fatal(err)
	}
	return path
}

// TestSyncImportExactKeyDifferingBodyNotSilent — the Rule 15 regression: an
// import row that hits the exact key with a DIFFERENT body must be counted as
// deduped AND reported as differing, while the fold itself keeps its semantics
// (the stored row is untouched, the arrival is not inserted).
//
// The red proof: without the body comparison in memoryStoreRow the second
// import reports RowsDedupedDiffering=0 and no note — the assertion below fails
// with "the differing body was folded in SILENCE".
func TestSyncImportExactKeyDifferingBodyNotSilent(t *testing.T) {
	dbPath := syncTestDB(t)

	// ① First import: the row lands, so the key now exists in the DB.
	first, err := SyncImport(syncDifferingBodyPatch(t, syncDifferingBodyAnswerA))
	if err != nil {
		t.Fatal(err)
	}
	if first.RowsImported != 1 || first.RowsDedupedDiffering != 0 {
		t.Fatalf("first import: want imported=1 differing=0, got %+v", first)
	}

	// ② Second import: SAME topic+question, different answer.
	second, err := SyncImport(syncDifferingBodyPatch(t, syncDifferingBodyAnswerB))
	if err != nil {
		t.Fatal(err)
	}
	if second.RowsImported != 0 || second.RowsDeduped != 1 {
		t.Fatalf("second import: want imported=0 deduped=1 (fold semantics unchanged), got %+v", second)
	}
	if second.RowsDedupedDiffering != 1 {
		t.Fatalf("the differing body was folded in SILENCE: RowsDedupedDiffering=%d, want 1 (import stats: %+v)",
			second.RowsDedupedDiffering, second)
	}
	if len(second.Errors) != 1 {
		t.Fatalf("want exactly 1 per-row note naming the discarded body, got %d: %+v", len(second.Errors), second.Errors)
	}
	note := second.Errors[0]
	if !strings.Contains(note, memoryDedupDifferingMarker) {
		t.Errorf("the note does not carry the marker %q: %q", memoryDedupDifferingMarker, note)
	}
	// The note must say WHERE the knowledge went — a note that only says
	// "something differed" leaves the operator with nowhere to look.
	if !strings.Contains(note, "id=") {
		t.Errorf("the note does not name the surviving row: %q", note)
	}
	// It is a NOTE, not a refusal: the patch committed in full.
	if second.RowsRefused != 0 {
		t.Errorf("a folded body must not be counted as a refusal, got refused=%d", second.RowsRefused)
	}

	// ③ The fold's SEMANTICS are untouched: still ONE row, and the knowledge
	// there is the FIRST body — the rule "the existing row is the truth" holds.
	if n := countDistilledRows(t, dbPath); n != 1 {
		t.Fatalf("the fold must keep exactly 1 row, got %d", n)
	}
	stored := stripKeywordsTail(queryAnswer(t, dbPath, syncDifferingBodyQ))
	if stored != syncDifferingBodyAnswerA {
		t.Fatalf("the stored body changed — the fold was supposed to keep the existing row:\n got %q\nwant %q",
			stored, syncDifferingBodyAnswerA)
	}
	if strings.Contains(stored, syncDifferingBodyAnswerB) {
		t.Fatalf("the discarded body reached the DB: %q", stored)
	}

	// ④ An IDEMPOTENT re-import must NOT be flagged: the same body arriving a
	// second time is the ordinary case, and a warning on it would make the new
	// signal worthless (every re-import would be "suspicious").
	third, err := SyncImport(syncDifferingBodyPatch(t, syncDifferingBodyAnswerA))
	if err != nil {
		t.Fatal(err)
	}
	if third.RowsDeduped != 1 || third.RowsDedupedDiffering != 0 {
		t.Fatalf("an idempotent re-import must dedupe WITHOUT a differing verdict: %+v", third)
	}
	if len(third.Errors) != 0 {
		t.Fatalf("an idempotent re-import must print no body note, got %+v", third.Errors)
	}

	// ⑤ And the discrimination is stable: the differing patch reports it again.
	fourth, err := SyncImport(syncDifferingBodyPatch(t, syncDifferingBodyAnswerB))
	if err != nil {
		t.Fatal(err)
	}
	if fourth.RowsDedupedDiffering != 1 {
		t.Fatalf("the differing verdict is not reproducible on a repeat run: %+v", fourth)
	}
}

// TestSyncImportExactKeyDifferingBodyIgnoresKeywordsTail — the keywords tail is
// a REGENERABLE breadcrumb (memoryStoreRow appends it from the keywords column
// on every write), not knowledge. A re-import that carries the same body with a
// different breadcrumb set must therefore NOT be reported as a differing body:
// flagging it would fire on a perfectly ordinary round trip and turn the new
// signal into noise.
func TestSyncImportExactKeyDifferingBodyIgnoresKeywordsTail(t *testing.T) {
	syncTestDB(t)

	body := syncDifferingBodyAnswerA
	first := &SyncPatch{Version: syncPatchVersion, Node: "hp-differing", Rows: []SyncRow{{
		Topic: syncDifferingBodyTopic, Question: syncDifferingBodyQ, Answer: body,
		Keywords: []string{"第一组 breadcrumb", "first keyword set"},
	}}}
	firstPath := filepath.Join(t.TempDir(), "patch-a.json")
	if err := WriteSyncPatch(first, firstPath); err != nil {
		t.Fatal(err)
	}
	if _, err := SyncImport(firstPath); err != nil {
		t.Fatal(err)
	}

	// Same body, different breadcrumbs (a round trip through a node that
	// rebuilt its keywords renders a different tail over the same knowledge).
	second := &SyncPatch{Version: syncPatchVersion, Node: "hp-differing", Rows: []SyncRow{{
		Topic: syncDifferingBodyTopic, Question: syncDifferingBodyQ, Answer: body,
		Keywords: []string{"第二组 breadcrumb", "second keyword set"},
	}}}
	secondPath := filepath.Join(t.TempDir(), "patch-b.json")
	if err := WriteSyncPatch(second, secondPath); err != nil {
		t.Fatal(err)
	}
	stats, err := SyncImport(secondPath)
	if err != nil {
		t.Fatal(err)
	}
	if stats.RowsDeduped != 1 {
		t.Fatalf("want deduped=1, got %+v", stats)
	}
	if stats.RowsDedupedDiffering != 0 {
		t.Fatalf("a keywords-tail-only difference must NOT be a differing body (the tail is regenerable): %+v", stats)
	}
}
