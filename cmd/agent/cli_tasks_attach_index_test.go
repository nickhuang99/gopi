package main

// Attachment-index empty-replica_rel_path contract, BRIEFING face
// (devdoc/attachment-index-contract.md). The internal/tools regression
// (attach_index_contract_test.go) fixes the index reader; THIS test proves the
// two faces actually agree end-to-end in the real process: the EPIC briefing's
// [attachmentN:] numbering (taskRunBriefing → TaskDeliverableAttachments) and
// `task show`'s per-entry line must report the SAME count and order for a row
// whose 3-entry manifest carries an empty replica_rel_path in the MIDDLE.
// Pre-fix, the dropped middle entry shifted every later replica by one slot,
// so the briefing named the wrong file for entry 3 (mislabel).

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nickhuang99/gopi/internal/tools"
)

// attIdxBriefTaskID is the fixture row id (task-unique prefix: sibling
// cmd/agent coverage slices use t-attach-brief / t-mirrored — never reuse
// theirs, a merge would silently re-bind the fixture).
const attIdxBriefTaskID = "t-attidx-brief"

// attIdxBriefSeedRow hands back the row's raw deliverable blob after
// splicing a hand-built empty-replica_rel_path entry into the MIDDLE of an
// otherwise real 2-entry manifest (position 2 of 3).
func attIdxBriefSeedRow(t *testing.T, taskID, dbPath string, mid tools.TaskAttachment) {
	t.Helper()
	db, err := sql.Open("sqlite", tools.LedgerDSN(dbPath))
	if err != nil {
		t.Fatalf("open ledger: %v", err)
	}
	defer db.Close()
	var raw string
	if err := db.QueryRow(`SELECT deliverable FROM tasks WHERE task_id = ?`, taskID).Scan(&raw); err != nil {
		t.Fatalf("read deliverable: %v", err)
	}
	var blob map[string]interface{}
	if err := json.Unmarshal([]byte(raw), &blob); err != nil {
		t.Fatalf("unmarshal deliverable %q: %v", raw, err)
	}
	arr, ok := blob["attachments"].([]interface{})
	if !ok || len(arr) != 2 {
		t.Fatalf("fixture manifest = %v (want the 2 copied entries)", blob["attachments"])
	}
	var first, last tools.TaskAttachment
	if err := json.Unmarshal(mustJSON(t, arr[0]), &first); err != nil {
		t.Fatalf("decode first entry: %v", err)
	}
	if err := json.Unmarshal(mustJSON(t, arr[1]), &last); err != nil {
		t.Fatalf("decode last entry: %v", err)
	}
	blob["attachments"] = []tools.TaskAttachment{first, mid, last}
	spliced, err := json.Marshal(blob)
	if err != nil {
		t.Fatalf("marshal spliced blob: %v", err)
	}
	if _, err := db.Exec(`UPDATE tasks SET deliverable = ? WHERE task_id = ?`, string(spliced), taskID); err != nil {
		t.Fatalf("write spliced blob: %v", err)
	}
}

// mustJSON re-encodes a decoded JSON value for typed unmarshalling.
func mustJSON(t *testing.T, v interface{}) []byte {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("re-encode manifest entry: %v", err)
	}
	return b
}

// TestAttIdxBriefFacesAgreeOnEmptyMiddleEntry: the briefing's [attachmentN:]
// lines and `task show`'s attachment lines must agree — same count, same
// order — with the empty-rel slot rendering the explicit missing label and
// entries 1/3 keeping their OWN replicas (no shift, hence no mislabel).
func TestAttIdxBriefFacesAgreeOnEmptyMiddleEntry(t *testing.T) {
	home := t.TempDir()
	dbPath := filepath.Join(home, "memory.db")
	t.Setenv("GOPI_HOME", home)
	t.Setenv("GOPI_MEMORY_DB", dbPath)

	dirA := filepath.Join(home, "srcA")
	dirC := filepath.Join(home, "srcC")
	for _, d := range []string{dirA, dirC} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	fileA := filepath.Join(dirA, "one.txt")
	fileC := filepath.Join(dirC, "three.txt")
	if err := os.WriteFile(fileA, []byte("content-A"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(fileC, []byte("content-C"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := tools.TaskSubmit(tools.TaskSubmitOptions{
		Summary:     "attach-index briefing face",
		Payload:     "read the three attachments",
		Attachments: []string{fileA, fileC},
		TaskID:      attIdxBriefTaskID,
	}); err != nil {
		t.Fatalf("submit: %v", err)
	}
	attIdxBriefSeedRow(t, attIdxBriefTaskID, dbPath, tools.TaskAttachment{
		OriginalAbsPath: filepath.Join(home, "srcB", "two.txt"),
		ReplicaRelPath:  "",
		SHA256:          "deadbeef",
		FileSize:        7,
	})

	atts, replicaPaths, err := tools.TaskDeliverableAttachments(attIdxBriefTaskID)
	if err != nil {
		t.Fatalf("TaskDeliverableAttachments: %v", err)
	}
	if len(atts) != 3 || len(replicaPaths) != 3 {
		t.Fatalf("manifest read back (%d atts, %d paths), want (3, 3)", len(atts), len(replicaPaths))
	}
	if replicaPaths[1] != tools.TaskAttachmentReplicaMissingLabel {
		t.Fatalf("replicaPaths[1] = %q, want the contract label %q",
			replicaPaths[1], tools.TaskAttachmentReplicaMissingLabel)
	}

	// Face 1: the EPIC briefing the child actually receives.
	briefing, err := taskRunBriefing(attIdxBriefTaskID)
	if err != nil {
		t.Fatalf("taskRunBriefing: %v", err)
	}
	if n := strings.Count(briefing, "[attachment"); n != 3 {
		t.Errorf("briefing carries %d [attachmentN: lines, want 3:\n%s", n, briefing)
	}
	for i, p := range replicaPaths {
		want := fmt.Sprintf("[attachment%d: %s]", i+1, p)
		if !strings.Contains(briefing, want) {
			t.Errorf("briefing missing %q:\n%s", want, briefing)
		}
	}

	// Face 2: `task show` for the SAME row — the pre-fix divergence.
	shown, err := tools.TaskShow(attIdxBriefTaskID)
	if err != nil {
		t.Fatalf("TaskShow: %v", err)
	}
	for i, p := range replicaPaths {
		line := fmt.Sprintf("attachment%d:", i+1)
		idx := strings.Index(shown, line)
		if idx < 0 {
			t.Fatalf("task show lacks %q:\n%s", line, shown)
		}
		if !strings.Contains(shown[idx:], p) {
			t.Errorf("face divergence at %s task show does not carry %q:\n%s", line, p, shown)
		}
	}
	// The middle entry must never borrow a neighbour's replica path.
	for _, other := range []string{replicaPaths[0], replicaPaths[2]} {
		mid := strings.Index(shown, "attachment2:")
		next := strings.Index(shown[mid:], "attachment3:")
		if mid >= 0 && next > 0 && strings.Contains(shown[mid:mid+next], other) {
			t.Errorf("attachment2 carries another entry's replica %q (positional mislabel):\n%s", other, shown)
		}
	}
}
