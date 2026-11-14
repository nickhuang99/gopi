package tools

// The BUCKET split of failed-needs-reissue (internal/tools/taskreissue.go).
//
// THE DEFECT these arms exist for, measured on the operator's live ledger
// 2026-11-01: the line read
//
//	failed-needs-reissue: 329 failed row(s), no successor row and no "resolved"
//	tag — reissue or tag them; …
//
// and 71 of those 329 were rows the harness mints for a run that was never
// asked for anything — `hi`, `list`, `config show`, `replay 56507166-…`. Their
// payload is EMPTY BY CONTRACT (taskregister.go: "Payload stays EMPTY for bare
// auto rows on purpose") and they carry no ground, so no deliverable was ever
// owed on them: "reissue" is not a debt an operator can pay. One number mixing
// two opposite meanings is how an alarm teaches a reader to skip it.
//
// The three arms below pin the split from both sides:
//
//	1. the PREDICATE — briefing-only and ground-only rows are ACTIONABLE; a row
//	   with neither is the named bucket, and it never reaches the id list;
//	2. the RENDER — actionable count and ids first, the named bucket after, on
//	   ONE line, and the bucket's ids never printed beside the actionable ones;
//	3. the REPRODUCTION — the rendered bucket count equals an INDEPENDENT SQL
//	   statement written from the documented rule, so the number on the line is
//	   not the renderer's own say-so (acceptance: "分桶数可由一条独立 SQL 复现").
//
// Database: testutil.NewTestDBPath + the tools layer (Rule 32) — a real seeded
// ledger, never a hand-rolled path, never the operator's profile.

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/nickhuang99/gopi/internal/testutil"
)

// seedBarePromptRow writes the row a bare-prompt/chat run registers: no
// briefing (payload stays empty, by contract), no ground (no branch, no
// worktree), marked with the harness's own summary prefix.
//
// It goes through the SAME create face the harness uses (TaskNewOID with no
// Payload/Deliverable), because the defect is about what that face produces —
// a fixture that typed the empty columns directly could disagree with it.
func seedBarePromptRow(t *testing.T, id, summary string) {
	t.Helper()
	if _, err := TaskNewOID(TaskNewOptions{
		TaskID:  id,
		Summary: summary,
	}); err != nil {
		t.Fatalf("create bare-prompt row %s: %v", id, err)
	}
	if _, err := TaskUpdate(id, taskStatusFailed); err != nil {
		t.Fatalf("fail bare-prompt row %s: %v", id, err)
	}
}

// reissueBucketCounts counts the two populations the renderer reports.
func reissueBucketCounts(rows []FailedReissueRow) (debt, bucket int) {
	for _, r := range rows {
		if r.Actionable() {
			debt++
			continue
		}
		bucket++
	}
	return debt, bucket
}

// TestFailedReissueBucketSplitsDebtFromConversation is the PREDICATE arm, both
// directions:
//
//	no briefing, no ground   → NOT actionable (the named bucket)
//	briefing only            → actionable (a task was asked for)
//	ground only              → actionable (work was done somewhere)
//	briefing AND ground      → actionable
//
// The negative half is the one a naive `status='failed'` count gets wrong, and
// the two single-fact rows are what stop the split from collapsing into "has a
// ground" (which would silently reclassify 26 real, ground-less executions as
// chat noise).
func TestFailedReissueBucketSplitsDebtFromConversation(t *testing.T) {
	testutil.NewTestDBPath(t)
	ctx := context.Background()

	// The bare-prompt row: the population the epic is about.
	seedBarePromptRow(t, "t-bare-hi", "[auto] hi")

	// An execution somebody asked for, with NO ground: a briefing alone is
	// enough to owe a reissue.
	seedReissueRow(t, "t-briefing-only", taskStatusFailed, "", "", "")
	if _, err := TaskUpdateReason("t-briefing-only", taskStatusFailed, "", ""); err != nil {
		t.Fatalf("noop: %v", err)
	}

	// An execution that recorded a GROUND but no briefing text (the `task run`
	// wrapper shape): the deliverable is the obligation.
	groundRow := `{"repo":"/home/user/work/gopi","branch":"task/t-ground-only","worktree":"/home/user/workspace/t-ground-only"}`
	seedReissueRow(t, "t-ground-only", taskStatusFailed, "", "", groundRow)

	db := openReissueLedger(t)
	defer db.Close()

	rows, err := FailedNeedsReissue(ctx, db)
	if err != nil {
		t.Fatalf("FailedNeedsReissue: %v", err)
	}
	byID := map[string]FailedReissueRow{}
	for _, r := range rows {
		byID[r.TaskID] = r
	}
	for _, id := range []string{"t-bare-hi", "t-briefing-only", "t-ground-only"} {
		if _, ok := byID[id]; !ok {
			t.Fatalf("row %s is not in the counted population at all — the count and the split would disagree:\n%v", id, rows)
		}
	}
	if got := byID["t-bare-hi"].Bucket; got != FailedReissueBucketNoBriefingNoGround {
		t.Fatalf("no-briefing/no-ground row bucketed as %q, want %q", got, FailedReissueBucketNoBriefingNoGround)
	}
	if byID["t-bare-hi"].Actionable() {
		t.Fatalf("a bare-prompt/chat registration counted as a debt — nothing was ever asked of it")
	}
	for _, id := range []string{"t-briefing-only", "t-ground-only"} {
		if got := byID[id].Bucket; got != FailedReissueBucketActionable {
			t.Fatalf("row %s bucketed as %q, want %q — it carried a briefing/ground, so a reissue IS a debt", id, got, FailedReissueBucketActionable)
		}
	}

	debt, bucket := reissueBucketCounts(rows)
	if debt != 2 || bucket != 1 {
		t.Fatalf("counted rows split into debt=%d bucket=%d, want 2/1:\n%v", debt, bucket, rows)
	}
}

// TestFailedReissueNoteRendersDebtFirstThenNamedBucket is the RENDER arm: the
// actionable number and its ids come FIRST (that is the debt), the bucket is
// NAMED after it and says plainly that nothing is owed, and the bucket's ids
// are never printed as if they were actionable.
func TestFailedReissueNoteRendersDebtFirstThenNamedBucket(t *testing.T) {
	testutil.NewTestDBPath(t)
	ctx := context.Background()

	seedBarePromptRow(t, "t-bare-config-show", "[auto] config show")
	seedReissueRow(t, "t-real-debt", taskStatusFailed, "", "", "")

	db := openReissueLedger(t)
	defer db.Close()
	rows, err := FailedNeedsReissue(ctx, db)
	if err != nil {
		t.Fatalf("FailedNeedsReissue: %v", err)
	}
	note := FailedReissueNote(rows)
	body := strings.TrimSuffix(note, "\n")
	if strings.Contains(body, "\n") {
		t.Fatalf("the note must stay ONE line (one log line, one webhook body):\n%q", note)
	}

	// The debt leads: the prefix, then the ACTIONABLE count (1, not 2).
	if !strings.HasPrefix(body, TaskFailedReissuePrefix+"1 ") {
		t.Fatalf("the note must open with the actionable count (%q + \"1 \"), got:\n%s", TaskFailedReissuePrefix, note)
	}
	debtAt := strings.Index(body, TaskFailedReissuePrefix+"1 ")
	bucketAt := strings.Index(body, "not reissue debt:")
	if bucketAt < 0 {
		t.Fatalf("the note does not name the second bucket — the reader cannot tell the 71 are owed nothing:\n%s", note)
	}
	if bucketAt < debtAt {
		t.Fatalf("the bucket sentence precedes the debt count; the actionable number must come FIRST:\n%s", note)
	}

	// The bucket is NAMED, counted, and explained.
	if !strings.Contains(body, fmt.Sprintf(taskFailedReissueBucketFmt, 1)) {
		t.Fatalf("the bucket sentence does not carry its own count (want %d):\n%s", 1, note)
	}
	if !strings.Contains(body, "no briefing and no ground") {
		t.Fatalf("the bucket is not described by the two facts that define it:\n%s", note)
	}
	if !strings.Contains(body, "nothing is owed") {
		t.Fatalf("the bucket is not stated as NOT a debt — that is the whole point of splitting it out:\n%s", note)
	}

	// The id list carries the debt row and NOT the bucket row.
	if !strings.Contains(body, "t-real-debt") {
		t.Fatalf("the note does not name the actionable row an operator has to open:\n%s", note)
	}
	if strings.Contains(body, "t-bare-config-show") {
		t.Fatalf("the bucket row appears in the rendered line — pasting the line's ids would land on a chat registration:\n%s", note)
	}

	// The actionable verbs still ride the line (they are the next step for the
	// debt, and an operator must not have to look up their spelling) — and they
	// belong to the DEBT segment, so they precede the bucket sentence rather
	// than trailing it (a prompt printed after "nothing is owed" reads as if it
	// applied to the bucket).
	verbs := strings.TrimSpace(taskFailedReissueNextFmt)
	verbsAt := strings.Index(body, verbs)
	if verbsAt < 0 {
		t.Fatalf("the note does not carry the actionable verbs %q:\n%s", verbs, note)
	}
	if verbsAt > bucketAt {
		t.Fatalf("the actionable verbs follow the bucket sentence; they belong to the debt:\n%s", note)
	}

	// A ledger whose ONLY counted rows are bucket rows still renders — silence
	// there would hide that the population exists — and it still renders NO
	// id as actionable.
	// The debt is PAID the way an operator pays it: `resolved` is the archive
	// mark the predicate excludes (a failed row is terminal, so a status flip is
	// refused by design — the archive tag is the sanctioned exit).
	if _, err := TaskArchive("t-real-debt"); err != nil {
		t.Fatalf("archive the debt row: %v", err)
	}
	rows, err = FailedNeedsReissue(ctx, db)
	if err != nil {
		t.Fatalf("FailedNeedsReissue after the debt is paid: %v", err)
	}
	debt, bucket := reissueBucketCounts(rows)
	if debt != 0 || bucket != 1 {
		t.Fatalf("after paying the debt: debt=%d bucket=%d, want 0/1", debt, bucket)
	}
	note = FailedReissueNote(rows)
	if !strings.Contains(note, TaskFailedReissuePrefix) {
		t.Fatalf("a ledger carrying only bucket rows rendered no line at all:\n%q", note)
	}
	if !strings.Contains(note, "nothing to reissue") {
		t.Fatalf("the zero-debt arm must say so instead of printing a count with no ids:\n%s", note)
	}
	if strings.Contains(note, "t-bare-config-show") {
		t.Fatalf("the bucket row leaked into the id list:\n%s", note)
	}

	// The ZERO contract is unchanged: no counted rows at all → no line.
	for _, r := range rows {
		if _, err := TaskArchive(r.TaskID); err != nil {
			t.Fatalf("retire %s: %v", r.TaskID, err)
		}
	}
	rows, err = FailedNeedsReissue(ctx, db)
	if err != nil {
		t.Fatalf("FailedNeedsReissue on a clean ledger: %v", err)
	}
	if note := FailedReissueNote(rows); note != "" {
		t.Fatalf("FailedReissueNote rendered %q for zero rows, want \"\"", note)
	}
}

// TestFailedReissueBucketIsReproducibleByIndependentSQL is the REPRODUCTION
// arm: the number the renderer prints for the bucket must equal what an
// INDEPENDENT statement computing the documented rule returns over the same
// ledger. Without it, "258 + 71" is the renderer's own say-so.
//
// The statement below is written the way a READER would write it (a
// `NOT EXISTS` successor test, the resolved-tag test, and the two facts from
// the epic — no ground, no payload) rather than by calling the production
// helpers, so an accidental change to the mechanism that keeps its own unit
// green still turns this red.
func TestFailedReissueBucketIsReproducibleByIndependentSQL(t *testing.T) {
	testutil.NewTestDBPath(t)
	ctx := context.Background()

	seedBarePromptRow(t, "t-bare-list", "[auto] list")
	seedBarePromptRow(t, "t-bare-replay", "[auto] replay 56507166-ef62-4d2d-bafe-af504fca9311")
	seedReissueRow(t, "t-debt-one", taskStatusFailed, "", "", "")
	seedReissueRow(t, "t-debt-two", taskStatusFailed, "", "",
		`{"repo":"/home/user/work/gopi","branch":"task/t-debt-two"}`)
	// Excluded from the count entirely (already dealt with), so a bucket query
	// that forgot the debt predicate would over-report.
	seedReissueRow(t, "t-already-resolved", taskStatusFailed, taskTagResolved, "", "")

	db := openReissueLedger(t)
	defer db.Close()

	rows, err := FailedNeedsReissue(ctx, db)
	if err != nil {
		t.Fatalf("FailedNeedsReissue: %v", err)
	}
	renderedDebt, renderedBucket := reissueBucketCounts(rows)
	if renderedDebt != 2 || renderedBucket != 2 {
		t.Fatalf("renderer split debt=%d bucket=%d, want 2/2", renderedDebt, renderedBucket)
	}

	// The independent statement: same two facts, same successor rule, same
	// resolved-tag rule — expressed from the epic's words.
	groundExpr := `(json_valid(COALESCE(deliverable, '')) AND (
		COALESCE(json_extract(deliverable, '$.' || ?), '') <> ''
		OR COALESCE(json_extract(deliverable, '$.' || ?), '') <> ''))`
	const where = `FROM tasks
		WHERE status = ?
		  AND NOT EXISTS (SELECT 1 FROM tasks c WHERE c.parent_task_id = tasks.task_id)
		  AND (',' || replace(COALESCE(tags, ''), ' ', '') || ',') NOT LIKE ?`
	// The WHERE clause's own two parameters, in the order the placeholders
	// appear; the ground expression's two are appended AFTER them, because the
	// bucket/debt statements add that expression to the right of the filter.
	// (Binding them in the wrong order is not a theoretical mistake: it makes
	// `status = 'branch'` match nothing and the count reads 0 while the
	// mechanism reads 4 — which is exactly how this arm caught itself.)
	whereArgs := []any{taskStatusFailed, taskFailedReissueResolvedLike}
	groundArgs := []any{DeliverableBranchKey, DeliverableWorktreeKey}

	var sqlCounted, sqlBucket, sqlDebt int
	if err := db.QueryRowContext(ctx, `SELECT count(*) `+where, whereArgs...).Scan(&sqlCounted); err != nil {
		t.Fatalf("independent counted query: %v", err)
	}
	if err := db.QueryRowContext(ctx, `SELECT count(*) `+where+
		` AND NOT `+groundExpr+` AND trim(COALESCE(payload, '')) = ''`,
		append(append([]any{}, whereArgs...), groundArgs...)...).Scan(&sqlBucket); err != nil {
		t.Fatalf("independent bucket query: %v", err)
	}
	if err := db.QueryRowContext(ctx, `SELECT count(*) `+where+
		` AND NOT (NOT `+groundExpr+` AND trim(COALESCE(payload, '')) = '')`,
		append(append([]any{}, whereArgs...), groundArgs...)...).Scan(&sqlDebt); err != nil {
		t.Fatalf("independent debt query: %v", err)
	}

	if sqlCounted != len(rows) {
		t.Fatalf("independent count=%d but the mechanism counted %d rows — the two populations are not the same", sqlCounted, len(rows))
	}
	if sqlBucket != renderedBucket {
		t.Fatalf("independent SQL says the bucket is %d row(s); the rendered line says %d", sqlBucket, renderedBucket)
	}
	if sqlDebt != renderedDebt {
		t.Fatalf("independent SQL says the debt is %d row(s); the rendered line says %d", sqlDebt, renderedDebt)
	}
	// The two halves must partition the counted population — an operator reads
	// "258 + 71" as the whole, so a row falling in neither (or both) would make
	// the line add up to a lie.
	if sqlDebt+sqlBucket != sqlCounted {
		t.Fatalf("debt(%d) + bucket(%d) != counted(%d)", sqlDebt, sqlBucket, sqlCounted)
	}
}
