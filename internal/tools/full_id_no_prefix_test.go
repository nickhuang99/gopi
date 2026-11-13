package tools

import (
	"path/filepath"
	"testing"
)

// This file is the regression guard for the epic's core rule: a task id and a
// session id are ALWAYS used in FULL, and a TRUNCATED id is never resolved by a
// prefix match.
//
// WHY IT EXISTS. Measured on 2026-09-23, the resolution paths had grown a
// prefix fallback whose FIRST HIT won silently: `gopi task show <row>` could
// attach ANOTHER task's transcript to the row (a truncated id in a listing
// collided with a session id and the scan returned whichever record the walk
// visited first), and the flake registry's owner column — an 8-hex chip —
// resolved 2 of its 5 rows through such a prefix. An exact-match-only lookup
// has no ordering dependence, so these tests pin BOTH halves: the full id
// resolves, the truncated id resolves to nothing.

// TestTaskFindSessionJSONLIsExactOnly pins the sessions-root pass: two records
// whose ids share an 8-character prefix, one of them a bare session dir and one
// a task-run dir. The truncated id must find NEITHER (the pre-fix code returned
// whichever the walk reached first — a wrong transcript on the row).
func TestTaskFindSessionJSONLIsExactOnly(t *testing.T) {
	root := t.TempDir()
	sessDir := root
	wantID := "aaaa1111-2222-4333-8444-555566667777"
	otherID := "aaaa1111-9999-4999-8999-000011112222"

	// Full id: a task-run layout (task_id/<session>.jsonl).
	taskDir := filepath.Join(sessDir, "01a0ce09-6395-7e7e-9f04-6ca51716c90b")
	writeFile(t, filepath.Join(taskDir, wantID+taskSessionJSONLExt), sessionStartLine(wantID))
	// Same 8-char prefix, bare session dir layout.
	writeFile(t, filepath.Join(sessDir, otherID, "session.jsonl"), sessionStartLine(otherID))

	got := taskFindSessionJSONL(sessDir, wantID)
	if got == "" {
		t.Fatalf("the FULL session id must resolve; got %q", got)
	}
	if filepath.Base(got) != wantID+taskSessionJSONLExt {
		t.Errorf("taskFindSessionJSONL(full id) = %q, want the record for %s", got, wantID)
	}
	if got := taskFindSessionJSONL(sessDir, "aaaa1111"); got != "" {
		t.Errorf("a TRUNCATED id must resolve to nothing, got %q", got)
	}
}

// TestTaskFindSessionJSONLFlatIsExactOnly pins the legacy flat pass directly:
// the exact basename wins and a truncation of it resolves to nothing.
func TestTaskFindSessionJSONLFlatIsExactOnly(t *testing.T) {
	dir := t.TempDir()
	id := "bbbb1111-2222-4333-8444-555566667777"
	neighbour := "bbbb1111-9999-4999-8999-000011112222"
	// The near-miss sorts FIRST, so a prefix-accepting lookup returns it.
	writeFile(t, filepath.Join(dir, neighbour+taskSessionJSONLExt), sessionStartLine(neighbour))
	writeFile(t, filepath.Join(dir, id+taskSessionJSONLExt), sessionStartLine(id))

	if got := findSessionJSONLFlat(dir, id); got != filepath.Join(dir, id+taskSessionJSONLExt) {
		t.Errorf("findSessionJSONLFlat(full id) = %q", got)
	}
	if got := findSessionJSONLFlat(dir, "bbbb1111"); got != "" {
		t.Errorf("a TRUNCATED id must resolve to nothing, got %q — it used to return the first prefix hit", got)
	}
}

// TestTaskFindSessionJSONLDeepOnlyIsExactOnly pins the deep (sub-directory)
// pass: the shared tree walk must match ids exactly, and must never hand the
// truncated operand a record it merely prefixes.
func TestTaskFindSessionJSONLDeepOnlyIsExactOnly(t *testing.T) {
	root := t.TempDir()
	sessDir := root
	id := "cccc1111-2222-4333-8444-555566667777"
	neighbour := "cccc1111-9999-4999-8999-000011112222"
	writeFile(t, filepath.Join(sessDir, neighbour, "session.jsonl"), sessionStartLine(neighbour))
	writeFile(t, filepath.Join(sessDir, id, "session.jsonl"), sessionStartLine(id))

	if got := taskFindSessionJSONLDeepOnly(sessDir, id); got == "" {
		t.Errorf("the FULL session id must resolve in the deep pass")
	}
	if got := taskFindSessionJSONLDeepOnly(sessDir, "cccc1111"); got != "" {
		t.Errorf("a TRUNCATED id must resolve to nothing in the deep pass, got %q", got)
	}
}

// TestChatFindSessionJSONLIsExactOnly pins the chat surface: a chat row's
// session_id is the full id, and a truncation of it must NOT be answered with
// some other session's transcript (the pre-fix first-hit prefix branch did
// exactly that, silently).
func TestChatFindSessionJSONLIsExactOnly(t *testing.T) {
	dir := t.TempDir()
	id := "dddd1111-2222-4333-8444-555566667777"
	neighbour := "dddd1111-9999-4999-8999-000011112222"
	writeFile(t, filepath.Join(dir, neighbour+taskSessionJSONLExt), sessionStartLine(neighbour))
	writeFile(t, filepath.Join(dir, id+taskSessionJSONLExt), sessionStartLine(id))

	if got := chatFindSessionJSONL(dir, id); got != filepath.Join(dir, id+taskSessionJSONLExt) {
		t.Errorf("chatFindSessionJSONL(full id) = %q", got)
	}
	if got := chatFindSessionJSONL(dir, "dddd1111"); got != "" {
		t.Errorf("a TRUNCATED id must resolve to nothing, got %q", got)
	}
	if got := chatFindSessionJSONL(dir, ""); got != "" {
		t.Errorf("an empty id must resolve to nothing, got %q", got)
	}
}
