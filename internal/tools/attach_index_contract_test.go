package tools

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"testing"

	"github.com/nick/gopi/internal/config"
)

// attachment-index positional contract regression (devdoc/attachment-index-
// contract.md). BEFORE the fix: taskDeliverableAttachmentIndex SKIPPED a
// manifest entry whose replica_rel_path was empty, so a 3-entry manifest with
// an empty MIDDLE rel produced a 2-element index — task show then paired
// atts[1] (the empty-rel entry) with idx[1] (the THIRD entry's replica) and
// atts[2] with nothing, mislabelling every later replica line, and the
// [attachmentN:] briefing numbering (rendered from the shortened list)
// diverged from task show for the same row.

// attIdxCtrTaskID is the fixture row id (task-unique prefix: sibling coverage
// slices on this package use covLife*/covSync* — never reuse theirs).
const attIdxCtrTaskID = "t-attidx-contract"

// attIdxCtrReplicaLine matches one rendered attachment line from task show.
var attIdxCtrReplicaLine = regexp.MustCompile(`(?m)^attachment(\d+):\s+(.*)$`)

// attIdxCtrEnv isolates GOPI_HOME + the tasks ledger in a t.TempDir().
func attIdxCtrEnv(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv(config.GopiDirEnv, home)
	t.Setenv(memoryDBEnv, filepath.Join(home, memoryDefaultDBFile))
	return home
}

// attIdxCtrWrite writes a fixture file, creating its parent directory.
func attIdxCtrWrite(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

// attIdxCtrSeedRow inserts a terminal ledger row carrying the given raw
// deliverable blob (both read faces only need the row + its blob).
func attIdxCtrSeedRow(t *testing.T, taskID, deliverable string) {
	t.Helper()
	if _, err := TaskNew("attach index fixture "+taskID, taskID, "", ""); err != nil {
		t.Fatalf("TaskNew(%s): %v", taskID, err)
	}
	if _, err := TaskUpdate(taskID, TaskStatusCompleted); err != nil {
		t.Fatalf("TaskUpdate(%s): %v", taskID, err)
	}
	db, err := openTasksDB(t.Context(), memoryDBPath())
	if err != nil {
		t.Fatalf("openTasksDB: %v", err)
	}
	defer db.Close()
	if _, err := db.Exec(`UPDATE tasks SET deliverable = ? WHERE task_id = ?`, deliverable, taskID); err != nil {
		t.Fatalf("seed deliverable for %s: %v", taskID, err)
	}
}

// attIdxCtrShowPairs parses task show's attachment lines into an ordered
// slice of replica renderings (index N-1 holds the rendering of attachmentN).
func attIdxCtrShowPairs(t *testing.T, shown string) []string {
	t.Helper()
	byN := map[int]string{}
	maxN := 0
	for _, m := range attIdxCtrReplicaLine.FindAllStringSubmatch(shown, -1) {
		n, err := strconv.Atoi(m[1])
		if err != nil {
			t.Fatalf("unparsable attachment index %q: %v", m[1], err)
		}
		byN[n] = m[2]
		if n > maxN {
			maxN = n
		}
	}
	out := make([]string, 0, maxN)
	for n := 1; n <= maxN; n++ {
		line, ok := byN[n]
		if !ok {
			t.Fatalf("task show skipped attachment%d (rendered: %v)", n, byN)
		}
		out = append(out, line)
	}
	return out
}

// TestAttIdxCtrEmptyMiddleReplicaRel: a 3-entry manifest whose 2nd entry has
// replica_rel_path "" must render as THREE attachments on both read faces,
// with entry 2 carrying the explicit missing label and entries 1/3 keeping
// their own replicas (no shift, no mislabel) — and both faces agreeing on
// order and count.
func TestAttIdxCtrEmptyMiddleReplicaRel(t *testing.T) {
	home := attIdxCtrEnv(t)

	// Two REAL copied replicas (entries 1 and 3).
	srcA := filepath.Join(home, "srcA", "one.txt")
	srcC := filepath.Join(home, "srcC", "three.txt")
	attIdxCtrWrite(t, srcA, "content-A")
	attIdxCtrWrite(t, srcC, "content-C")
	copied, err := TaskCopyAttachments(attIdxCtrTaskID, []string{srcA, srcC})
	if err != nil {
		t.Fatalf("TaskCopyAttachments: %v", err)
	}
	if len(copied) != 2 {
		t.Fatalf("copied %d manifest entries, want 2", len(copied))
	}
	relA := copied[0].ReplicaRelPath
	relC := copied[1].ReplicaRelPath
	if relA == "" || relC == "" {
		t.Fatalf("copies carry empty replica_rel_path: %q / %q", relA, relC)
	}

	// Hand-built 3-entry manifest: real, EMPTY-rel, real.
	blob, err := json.Marshal(map[string]interface{}{
		"repo": "/tmp/repo",
		"attachments": []TaskAttachment{
			copied[0],
			{
				OriginalAbsPath: filepath.Join(home, "srcB", "two.txt"),
				ReplicaRelPath:  "",
				SHA256:          "deadbeef",
				FileSize:        7,
			},
			copied[1],
		},
	})
	if err != nil {
		t.Fatalf("marshal blob: %v", err)
	}
	attIdxCtrSeedRow(t, attIdxCtrTaskID, string(blob))

	// Face 1: the run-time read face (EPIC [attachmentN:] numbering source).
	atts, replicaPaths, err := TaskDeliverableAttachments(attIdxCtrTaskID)
	if err != nil {
		t.Fatalf("TaskDeliverableAttachments: %v", err)
	}
	if len(atts) != 3 {
		t.Fatalf("manifest read back %d entries, want 3", len(atts))
	}
	if len(replicaPaths) != len(atts) {
		t.Fatalf("replicaPaths = %d, want %d (one per manifest entry — an empty replica_rel_path must not shorten the index)", len(replicaPaths), len(atts))
	}
	sd, err := TaskSessionDir(attIdxCtrTaskID)
	if err != nil {
		t.Fatalf("TaskSessionDir: %v", err)
	}
	wantPaths := []string{
		filepath.Join(sd, filepath.FromSlash(relA)),
		TaskAttachmentReplicaMissingLabel,
		filepath.Join(sd, filepath.FromSlash(relC)),
	}
	for i, want := range wantPaths {
		if replicaPaths[i] != want {
			t.Errorf("replicaPaths[%d] = %q, want %q", i, replicaPaths[i], want)
		}
	}
	// The empty-rel slot must never resolve to a path (the session dir root
	// would be a plausible-but-wrong join).
	if replicaPaths[1] == sd || replicaPaths[1] == "" {
		t.Errorf("replicaPaths[1] = %q — an empty replica_rel_path must render the explicit label, not an empty/joined path", replicaPaths[1])
	}

	// Face 2: task show — same order, same count, no mislabel.
	shown, err := TaskShow(attIdxCtrTaskID)
	if err != nil {
		t.Fatalf("TaskShow: %v", err)
	}
	lines := attIdxCtrShowPairs(t, shown)
	if len(lines) != len(replicaPaths) {
		t.Fatalf("task show rendered %d attachment lines, want %d:\n%s", len(lines), len(replicaPaths), shown)
	}
	for i := range lines {
		if lines[i] != replicaPaths[i] {
			t.Errorf("face divergence at attachment%d: task show %q vs TaskDeliverableAttachments %q", i+1, lines[i], replicaPaths[i])
		}
	}
	for i, a := range atts {
		want := "from:         " + a.OriginalAbsPath
		if !regexp.MustCompile(`(?m)^\s*` + regexp.QuoteMeta(want)).MatchString(shown) {
			t.Errorf("task show lacks the source line for attachment%d (%s):\n%s", i+1, a.OriginalAbsPath, shown)
		}
	}
	// The middle line must not steal a neighbour's replica (the pre-fix bug).
	if want := TaskAttachmentReplicaMissingLabel; lines[1] != want {
		t.Errorf("attachment2 = %q, want the explicit label %q", lines[1], want)
	}
	for i, wrong := range map[int]string{1: wantPaths[2], 3: wantPaths[0]} {
		if lines[i-1] == wrong {
			t.Errorf("attachment%d carries another entry's replica %q (positional mislabel):\n%s", i, wrong, shown)
		}
	}

	// The label leaked into the EPIC numbering source only for the empty
	// slot — entries 1 and 3 still point at READABLE replicas.
	for _, i := range []int{0, 2} {
		if _, err := os.Stat(replicaPaths[i]); err != nil {
			t.Errorf("replicaPaths[%d] = %q not readable: %v", i, replicaPaths[i], err)
		}
	}
}
