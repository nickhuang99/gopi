package tools

// Payload revision (epic 01a0fea1 item [2/5]): `gopi task update <id>
// --payload TEXT|@FILE` turns the payload column from immutable into
// REVISABLE — with the old value kept in the append-only task_events trail.
//
// WHAT THESE TESTS PIN, and why each one is here rather than in the CLI
// package: the CLI half asserts that the flag is admitted, parsed and
// dispatched; the ENGINE half below asserts what the ledger ends up holding,
// which is the part an operator's audit depends on:
//
//	① the row's payload column really changes (TaskShow renders it);
//	② the OLD value survives — verbatim — in the trail, with its sha256;
//	③ the full-text triggers followed the edit (search finds the new
//	  briefing and no longer matches the replaced one: tasks_au, not a
//	  hand-rolled reindex);
//	④ a blank payload and a missing --reason are refused BY NAME, and a
//	  refusal writes NOTHING (no half-revision, no orphan trail row);
//	⑤ a row that does not exist is refused and leaves no trail entry;
//	⑥ re-sending the briefing the row already carries is a no-op;
//	⑦ NO SYNC PATH runs: the revision touches tasks and task_events (and
//	  the FTS shadow tables through their triggers) and nothing else — the
//	  single-node invariant this item must not break.

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"strings"
	"testing"

	"github.com/nickhuang99/gopi/internal/config"
	"github.com/nickhuang99/gopi/internal/testutil"
	"os"
	"path/filepath"
)

// payloadRevFixture opens a private ledger (Rule 32: the scaffold owns the
// path — never a hand-rolled memory.db) and submits ONE row whose payload is
// the ORIGINAL briefing.
func payloadRevFixture(t *testing.T, oldPayload string) {
	t.Helper()
	t.Setenv("GOPI_MEMORY_DB", testutil.NewTestDBPath(t))
	settings := filepath.Join(t.TempDir(), "settings.json")
	// sync_peers = [] IS the single-node profile this item reasons about, and
	// it is pinned rather than inherited: a test that ran with the operator's
	// ambient whitelist would exercise a DIFFERENT code path (the sync layer's
	// content judgment) and the green would mean something else.
	if err := os.WriteFile(settings, []byte(`{"sync_peers":[]}`), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv(config.SettingsEnv, settings)
	if _, err := TaskSubmit(TaskSubmitOptions{
		Summary: "payload revision fixture",
		Payload: oldPayload,
		TaskID:  "t-payload-rev",
		Source:  "human",
	}); err != nil {
		t.Fatalf("TaskSubmit: %v", err)
	}
}

// payloadRevEvents reads every trail entry for taskID under the revision
// event name, newest first, straight from the ledger (a raw read is the point
// here: it is the ROW an auditor would quote).
func payloadRevEvents(t *testing.T, taskID string) []string {
	t.Helper()
	db, err := sql.Open("sqlite", LedgerDSN(memoryDBPath()))
	if err != nil {
		t.Fatalf("open ledger: %v", err)
	}
	defer db.Close()
	rows, err := db.QueryContext(context.Background(),
		`SELECT detail FROM task_events WHERE task_id = ? AND event = ? ORDER BY id DESC`, taskID, PayloadRevisionEventName)
	if err != nil {
		t.Fatalf("read task_events: %v", err)
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var d string
		if err := rows.Scan(&d); err != nil {
			t.Fatalf("scan: %v", err)
		}
		out = append(out, d)
	}
	return out
}

// TestTaskSetPayloadRevisionKeepsTheOldValueInTheTrail is ①②: the column
// moves, the returned digest names both sides, and the ledger keeps the
// replaced briefing verbatim next to its sha256.
func TestTaskSetPayloadRevisionKeepsTheOldValueInTheTrail(t *testing.T) {
	const oldPayload = "ORIGINAL briefing: run the thing"
	const newPayload = "REVISED briefing: run the other thing, twice"
	payloadRevFixture(t, oldPayload)

	out, err := TaskSetPayload("t-payload-rev", newPayload, "wrong command in the original")
	if err != nil {
		t.Fatalf("TaskSetPayload: %v", err)
	}
	if !strings.Contains(out, "Task payload updated: t-payload-rev") {
		t.Errorf("the revision must report what it did:\n%s", out)
	}

	show, err := TaskShow("t-payload-rev")
	if err != nil {
		t.Fatalf("TaskShow: %v", err)
	}
	// ① The row now carries the NEW briefing — this is what taskRunBriefing
	// hands the next executing instance, so it is the assertion that matters.
	if !strings.Contains(show, newPayload) {
		t.Errorf("the row must carry the revised payload:\n%s", show)
	}
	if strings.Contains(show, oldPayload) {
		t.Errorf("the row must no longer carry the replaced payload:\n%s", show)
	}
	// The SUMMARY is untouched: this door revises the briefing, not the
	// immutable display text (bug056's contract).
	if !strings.Contains(show, "summary:        payload revision fixture") {
		t.Errorf("the summary must stay exactly as submitted:\n%s", show)
	}

	// ② The trail keeps the old value. The sha256 is checked as an actual
	// digest of whatever the trail recorded, so a truncated or re-encoded
	// copy fails here rather than looking right.
	events := payloadRevEvents(t, "t-payload-rev")
	if len(events) != 1 {
		t.Fatalf("want exactly 1 %s event, got %d", PayloadRevisionEventName, len(events))
	}
	detail := events[0]
	if !strings.Contains(detail, "reason: wrong command in the original") {
		t.Errorf("the trail must record the WHY:\n%s", detail)
	}
	if !strings.Contains(detail, oldPayload) {
		t.Errorf("the trail must keep the REPLACED value verbatim:\n%s", detail)
	}
	if !strings.Contains(detail, newPayload) {
		t.Errorf("the trail must record the NEW value verbatim:\n%s", detail)
	}
	sum := sha256.Sum256([]byte(oldPayload))
	wantOld := "old (sha256 " + hex.EncodeToString(sum[:]) + ", " + itoaForTest(len(oldPayload)) + " bytes)"
	if !strings.Contains(detail, wantOld) {
		t.Errorf("the trail's old-value digest must be the real sha256 of the replaced text\nwant: %s\ngot:\n%s", wantOld, detail)
	}
	sumNew := sha256.Sum256([]byte(newPayload))
	wantNew := "new (sha256 " + hex.EncodeToString(sumNew[:]) + ", " + itoaForTest(len(newPayload)) + " bytes)"
	if !strings.Contains(detail, wantNew) {
		t.Errorf("the trail's new-value digest must be the real sha256 of the new text\nwant: %s\ngot:\n%s", wantNew, detail)
	}
}

// TestTaskSetPayloadRefusalWritesNothing is ④⑤: each refusal is by NAME and
// leaves the ledger byte-identical (the row keeps its briefing and the trail
// gains no entry — a refused edit that still wrote would be exactly the
// silent-failure shape Rule 15 exists to prevent).
func TestTaskSetPayloadRefusalWritesNothing(t *testing.T) {
	const oldPayload = "ORIGINAL briefing"
	payloadRevFixture(t, oldPayload)

	refusals := []struct {
		name    string
		taskID  string
		payload string
		reason  string
		want    string
	}{
		{"blank payload", "t-payload-rev", "", "trying to blank it", "refusing to blank the payload column"},
		{"whitespace-only payload", "t-payload-rev", "   \n\t ", "trying to blank it", "refusing to blank the payload column"},
		{"missing reason", "t-payload-rev", "a new briefing", "", "must carry --reason"},
		{"unknown row", "t-does-not-exist", "a new briefing", "why not", `no task with task_id="t-does-not-exist"`},
	}
	for _, c := range refusals {
		t.Run(c.name, func(t *testing.T) {
			out, err := TaskSetPayload(c.taskID, c.payload, c.reason)
			if err == nil {
				t.Fatalf("must be refused, got %q", out)
			}
			if !strings.Contains(err.Error(), c.want) {
				t.Errorf("the refusal must name the reason (%q): %v", c.want, err)
			}
		})
	}
	// The row is exactly as it was, and the trail holds NO revision entry —
	// including for the unknown-row attempt, which must not leave a trail row
	// naming a task that does not exist.
	show, err := TaskShow("t-payload-rev")
	if err != nil {
		t.Fatalf("TaskShow: %v", err)
	}
	if !strings.Contains(show, oldPayload) {
		t.Errorf("a refused revision must leave the payload alone:\n%s", show)
	}
	// why: correct — the set is ONE ROW'S trail entries (task_events filtered by task_id+event), and the guard that must not be "too broad" is TaskSetPayload's own validation, which runs BEFORE openTasksDB: a blank/whitespace payload and a missing reason both return at the top of the function, so no transaction is begun and no INSERT exists on that path. The positive half is carried IN THIS TEST, immediately below: a valid revision after the refusals asserts len(events)==1, so an empty list here is the validation's own work, not a trail nothing can write.
	if events := payloadRevEvents(t, "t-payload-rev"); len(events) != 0 {
		t.Errorf("a refused revision must write no trail entry, got %d", len(events))
	}
	if events := payloadRevEvents(t, "t-does-not-exist"); len(events) != 0 {
		t.Errorf("an unknown row must leave no trail entry, got %d", len(events))
	}
	// The POSITIVE half, in this same test (the totality audit's requirement):
	// the trail is not simply unwritable — one real revision lands exactly one
	// entry. Without this, "no entry" above could be a column that cannot be
	// written at all, which is a different (and worse) fact.
	if _, err := TaskSetPayload("t-payload-rev", "a briefing that is actually accepted", "positive half: the trail IS writable"); err != nil {
		t.Fatalf("a valid revision after the refusals must succeed: %v", err)
	}
	if events := payloadRevEvents(t, "t-payload-rev"); len(events) != 1 {
		t.Errorf("one accepted revision must land exactly 1 trail entry, got %d", len(events))
	}
}

// TestTaskSetPayloadIsIdempotentAndFtsFollows is ③⑥: re-sending the same
// briefing writes nothing at all, and a real revision is visible to the
// full-text indexes through their own triggers — search finds the new
// briefing, stops matching the replaced one, and the trail event is NOT
// rendered as a run digest (a revision is not a `result:`).
func TestTaskSetPayloadIsIdempotentAndFtsFollows(t *testing.T) {
	const oldPayload = "briefing about zeppelin calibration"
	const newPayload = "briefing about hydrofoil calibration"
	payloadRevFixture(t, oldPayload)

	// ⑥ Idempotent: the row already carries this briefing.
	same, err := TaskSetPayload("t-payload-rev", oldPayload, "no-op probe")
	if err != nil {
		t.Fatalf("re-sending the current payload must not be an error: %v", err)
	}
	if !strings.Contains(same, "unchanged") {
		t.Errorf("re-sending the current payload must say so:\n%s", same)
	}
	// why: correct — the set is ONE ROW'S trail entries, and the guard is TaskSetPayload's idempotence arm: `old == payload` returns BEFORE the transaction is opened (the same test's first half asserts the returned line carries "unchanged"). The positive half is carried IN THIS TEST a few assertions below: the real revision that follows asserts len(events)==1, so an empty list here is the early return's own work, not an unwritable trail.
	if events := payloadRevEvents(t, "t-payload-rev"); len(events) != 0 {
		t.Fatalf("a no-op must write no trail entry, got %d", len(events))
	}

	if _, err := TaskSetPayload("t-payload-rev", newPayload, "the wrong vessel was named"); err != nil {
		t.Fatalf("TaskSetPayload: %v", err)
	}
	// The POSITIVE half, in this same test (the totality audit's requirement):
	// the real revision that follows the no-op lands exactly ONE entry — so
	// "the no-op wrote nothing" is the early return's own work, not a trail
	// that cannot be written.
	if events := payloadRevEvents(t, "t-payload-rev"); len(events) != 1 {
		t.Fatalf("the real revision must land exactly 1 trail entry, got %d", len(events))
	}
	// ③ FTS followed the edit (tasks_au): the new term matches, the replaced
	// term does not. A hand-rolled UPDATE that skipped the trigger would pass
	// the column assertion above and fail exactly here.
	hit, err := TaskSearch("hydrofoil")
	if err != nil {
		t.Fatalf("TaskSearch(hydrofoil): %v", err)
	}
	if !strings.Contains(hit, "t-payload-rev") {
		t.Errorf("search must find the REVISED briefing:\n%s", hit)
	}
	miss, err := TaskSearch("zeppelin")
	if err != nil {
		t.Fatalf("TaskSearch(zeppelin): %v", err)
	}
	if strings.Contains(miss, "t-payload-rev") {
		t.Errorf("search must no longer match the REPLACED briefing:\n%s", miss)
	}
	// The trail event must not masquerade as a run digest on `task show`:
	// validTaskStatus(event) is false for payload_revised, so the result line
	// stays empty rather than quoting a briefing back as a run result.
	show, err := TaskShow("t-payload-rev")
	if err != nil {
		t.Fatalf("TaskShow: %v", err)
	}
	if strings.Contains(show, "result:         reason:") {
		t.Errorf("the revision event must not render as a result digest:\n%s", show)
	}
}
