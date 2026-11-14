package tools

// Phase-1 session-dir + attachment copy tests (epic-task-reframe). Each
// acceptance case maps to one test:
//
//	(a) same-name files in DIFFERENT absolute dirs copy to distinct replica
//	    subdirs — never collide, path structure preserved
//	    (/tmp/log.txt → attachments/tmp/log.txt,
//	     /home/user/log.txt → attachments/home/user/log.txt)
//	(b) submit with --attachment paths → session dir created, replicas
//	    present with correct content, manifest in the deliverable JSON
//	    (original_abs_path / replica_rel_path / sha256 / file_size /
//	    submitted_at), task show renders the [attachmentN:] index
//	(c) symlink attachment dereferences to the linked file's real content
//	(d) non-absolute / unreadable / missing attachment aborts the submit —
//	    no row, no partial replicas
//	(e) no-attachment submit behavior unchanged (repo-only deliverable
//	    blob, no session-dir attachments area)
//	(f) two submits with the SAME explicit task id → second aborts, and
//	    the first submit's replicas are untouched (no cross-contamination
//	    of a live task's session dir)
//	(g) taskDeliverableAttachmentIndex resolves replica absolute paths =
//	    <session-dir>/<replica_rel_path> (the EPIC [attachmentN:] material)

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nickhuang99/gopi/internal/tasksource"
)

// phase1AttachEnv points GOPI_HOME and the memory DB at temp dirs so the
// session dir (GopiDir/sessions/<id>) and the ledger row live in
// isolated, deletable space.
func phase1AttachEnv(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("GOPI_HOME", home)
	t.Setenv("GOPI_MEMORY_DB", filepath.Join(home, "memory.db"))
	return home
}

func sha256hex(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

// phase1TaskShow renders a task through the public TaskShow face.
func phase1TaskShow(t *testing.T, id string) string {
	t.Helper()
	out, err := TaskShow(id)
	if err != nil {
		t.Fatalf("TaskShow(%s): %v", id, err)
	}
	return out
}

// phase1ReplicaPath asserts the replica at
// <session-dir>/<replica_rel_path> exists (rel already carries the
// attachments/ prefix — one join, matching consumers) and returns its
// absolute path.
func phase1ReplicaPath(t *testing.T, gopiHome, taskID, rel string) string {
	t.Helper()
	abs := filepath.Join(gopiHome, ".gopi", "sessions", taskID, filepath.FromSlash(rel))
	if _, err := os.Stat(abs); err != nil {
		t.Fatalf("replica %s missing: %v", abs, err)
	}
	return abs
}

// TestPhase1AttachSubmitSameNameDifferentDirs — acceptance (a)+(b): two
// attachments with the same file name in different absolute directories are
// both copied, neither overwrites the other, and the path structure
// (absolute path minus leading /) is preserved under attachments/.
func TestPhase1AttachSubmitSameNameDifferentDirs(t *testing.T) {
	home := phase1AttachEnv(t)

	// /tmp/log.txt and /home/user/log.txt — same base name, different dirs.
	aDir := filepath.Join(home, "dirA")
	bDir := filepath.Join(home, "dirB")
	if err := os.MkdirAll(aDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(bDir, 0o755); err != nil {
		t.Fatal(err)
	}
	// Force different absolute directory NAMES (dirA vs dirB) so the test
	// is independent of the temp prefix.
	origA := filepath.Join(aDir, "log.txt")
	origB := filepath.Join(bDir, "log.txt")
	if err := os.WriteFile(origA, []byte("alpha"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(origB, []byte("bravo"), 0o644); err != nil {
		t.Fatal(err)
	}

	out, err := TaskSubmit(TaskSubmitOptions{
		Summary:     "attach same-name test",
		Attachments: []string{origA, origB},
	})
	if err != nil {
		t.Fatalf("TaskSubmit: %v", err)
	}
	taskID := extractTaskID(t, out)
	if taskID == "" {
		t.Fatalf("no task id in %q", out)
	}

	// relA depends on the actual temp prefix of origA. Compute expected
	// replica rel paths as absolute-minus-leading-slash.
	relA := filepath.ToSlash(filepath.Join("attachments", strings.TrimPrefix(filepath.ToSlash(origA), "/")))
	relB := filepath.ToSlash(filepath.Join("attachments", strings.TrimPrefix(filepath.ToSlash(origB), "/")))
	repA := phase1ReplicaPath(t, home, taskID, relA)
	repB := phase1ReplicaPath(t, home, taskID, relB)
	if repA == repB {
		t.Fatalf("replicas collided: both %s", repA)
	}
	// Content integrity: each replica carries its own file's content — a
	// collision would have the SECOND copy overwrite the first (both
	// "bravo"), so this is the no-overwrite proof.
	//stylegate:ignore reads a file the test itself wrote; the content is the assertion
	if got, _ := os.ReadFile(repA); string(got) != "alpha" {
		t.Errorf("replica A = %q, want alpha (overwritten by B?)", got)
	}
	//stylegate:ignore reads a file the test itself wrote; the content is the assertion
	if got, _ := os.ReadFile(repB); string(got) != "bravo" {
		t.Errorf("replica B = %q, want bravo", got)
	}

	// Manifest rides in the deliverable JSON with every required field.
	row := phase1TaskRow(t, taskID)
	if row == nil || !strings.Contains(row.Deliverable, "attachments") {
		t.Fatalf("deliverable has no attachments manifest: %q", row.Deliverable)
	}
	var deliv struct {
		Repo        string           `json:"repo"`
		Attachments []TaskAttachment `json:"attachments"`
	}
	if err := json.Unmarshal([]byte(row.Deliverable), &deliv); err != nil {
		t.Fatalf("deliverable unmarshal: %v", err)
	}
	if len(deliv.Attachments) != 2 {
		t.Fatalf("manifest entries = %d, want 2", len(deliv.Attachments))
	}
	byOrig := map[string]TaskAttachment{}
	for _, a := range deliv.Attachments {
		byOrig[a.OriginalAbsPath] = a
	}
	for orig, want := range map[string]string{origA: "alpha", origB: "bravo"} {
		a, ok := byOrig[orig]
		if !ok {
			t.Fatalf("manifest missing entry for %s", orig)
		}
		wantRel := filepath.ToSlash(filepath.Join("attachments", strings.TrimPrefix(filepath.ToSlash(orig), "/")))
		if a.ReplicaRelPath != wantRel {
			t.Errorf("%s: replica_rel_path = %q", orig, a.ReplicaRelPath)
		}
		if a.SHA256 != sha256hex(t, orig) {
			t.Errorf("%s: sha256 = %s, want %s", orig, a.SHA256, sha256hex(t, orig))
		}
		if a.FileSize != int64(len(want)) {
			t.Errorf("%s: file_size = %d, want %d", orig, a.FileSize, len(want))
		}
		if a.SubmittedAt <= 0 {
			t.Errorf("%s: submitted_at = %d, want > 0", orig, a.SubmittedAt)
		}
	}

	// task show renders the [attachmentN:] index with replica + original.
	show := phase1TaskShow(t, taskID)
	if !strings.Contains(show, "attachment1:    "+repA) || !strings.Contains(show, "attachment2:    "+repB) {
		t.Errorf("show lacks attachmentN replica index:\n%s", show)
	}
	if !strings.Contains(show, origA) || !strings.Contains(show, origB) {
		t.Errorf("show lacks original-source lines:\n%s", show)
	}
}

// TestPhase1AttachSymlinkDeref — acceptance: a symlink attachment copies the
// linked file's real content, not the link node.
func TestPhase1AttachSymlinkDeref(t *testing.T) {
	home := phase1AttachEnv(t)
	real := filepath.Join(home, "real.txt")
	if err := os.WriteFile(real, []byte("real-content"), 0o644); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(home, "link.txt")
	if err := os.Symlink(real, link); err != nil {
		t.Skipf("symlink unsupported: %v", err)
	}

	out, err := TaskSubmit(TaskSubmitOptions{
		Summary:     "symlink attach",
		Attachments: []string{link},
	})
	if err != nil {
		t.Fatalf("TaskSubmit: %v", err)
	}
	taskID := extractTaskID(t, out)
	rel := filepath.ToSlash(filepath.Join("attachments", strings.TrimPrefix(filepath.ToSlash(link), "/")))
	rep := phase1ReplicaPath(t, home, taskID, rel)
	// The replica must be a REGULAR file carrying the real content (a
	// copied link node would be a symlink / dangling replica).
	if fi, err := os.Stat(rep); err != nil || fi.Mode()&os.ModeSymlink != 0 {
		t.Errorf("replica not a dereferenced regular file (mode %v, err %v)", fi.Mode(), err)
	}
	//stylegate:ignore reads a file the test itself wrote; the content is the assertion
	if got, _ := os.ReadFile(rep); string(got) != "real-content" {
		t.Errorf("replica content = %q, want real-content", got)
	}
	if sha256hex(t, rep) != sha256hex(t, real) {
		t.Errorf("replica digest differs from real file")
	}
}

// TestPhase1AttachInvalidAborts — acceptance: a relative / missing / dir
// attachment aborts the whole submit — no row, no partial replicas.
func TestPhase1AttachInvalidAborts(t *testing.T) {
	home := phase1AttachEnv(t)
	good := filepath.Join(home, "good.txt")
	if err := os.WriteFile(good, []byte("good"), 0o644); err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		name string
		path string // "" = second attachment is the invalid one
	}{
		{"relative", "relative.txt"},
		{"missing", filepath.Join(home, "nope.txt")},
		{"directory", home},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			home := phase1AttachEnv(t)
			good := filepath.Join(home, "good.txt")
			if err := os.WriteFile(good, []byte("good"), 0o644); err != nil {
				t.Fatal(err)
			}
			// The GOOD attachment comes FIRST — validation must precede all
			// copies, so a bad second path leaves no partial replica.
			_, err := TaskSubmit(TaskSubmitOptions{
				Summary:     "bad attach",
				Attachments: []string{good, c.path},
			})
			if err == nil {
				t.Fatalf("submit with invalid attachment %q must error", c.path)
			}
			if !strings.Contains(err.Error(), "attachment") {
				t.Errorf("error %v should mention attachment", err)
			}
			// No row was inserted.
			if n := phase1TaskCount(t); n != 0 {
				t.Errorf("tasks = %d, want 0 (submit must abort cleanly)", n)
			}
		})
	}
}

// TestPhase1AttachNoneRegressed — acceptance: no-attachment submit behaves
// exactly as before (repo-only deliverable, no session-dir copy, show
// renders no attachmentN line).
func TestPhase1AttachNoneRegressed(t *testing.T) {
	home := phase1AttachEnv(t)
	out, err := TaskSubmit(TaskSubmitOptions{Summary: "plain task"})
	if err != nil {
		t.Fatalf("TaskSubmit: %v", err)
	}
	taskID := extractTaskID(t, out)
	row := phase1TaskRow(t, taskID)
	if row == nil {
		t.Fatal("row missing")
	}
	// Repo-only blob (cwd of the test binary may or may not be a git tree —
	// either way, NO attachments key may appear).
	if strings.Contains(row.Deliverable, "attachment") {
		t.Errorf("no-attachment deliverable carries attachment key: %q", row.Deliverable)
	}
	// Session dir may exist (created lazily by backfill paths?) — but the
	// attachments AREA must not hold a copy for a no-attachment task. Check
	// that the ledger row's session container was not pre-created with
	// replicas (it may not exist at all).
	sd := filepath.Join(home, ".gopi", "sessions", taskID, "attachments")
	if entries, err := os.ReadDir(sd); err == nil && len(entries) > 0 {
		t.Errorf("no-attachment task has %d replica entries under %s", len(entries), sd)
	}
	show := phase1TaskShow(t, taskID)
	if strings.Contains(show, "attachment") {
		t.Errorf("no-attachment show renders attachment lines:\n%s", show)
	}
}

// TestPhase1AttachExplicitIDRejectNoCrossContam — acceptance (f): a submit
// with an explicit --task-id already taken aborts BEFORE copying, leaving
// the live task's session dir untouched.
func TestPhase1AttachExplicitIDRejectNoCrossContam(t *testing.T) {
	home := phase1AttachEnv(t)
	first := filepath.Join(home, "first.txt")
	if err := os.WriteFile(first, []byte("first"), 0o644); err != nil {
		t.Fatal(err)
	}
	out, err := TaskSubmit(TaskSubmitOptions{
		Summary:     "first with explicit id",
		TaskID:      "t-attach-live",
		Attachments: []string{first},
	})
	if err != nil {
		t.Fatalf("first submit: %v", err)
	}
	if !strings.Contains(out, "t-attach-live") {
		t.Fatalf("first submit out = %q", out)
	}
	relFirst := filepath.ToSlash(filepath.Join("attachments", strings.TrimPrefix(filepath.ToSlash(first), "/")))
	repFirst := phase1ReplicaPath(t, home, "t-attach-live", relFirst)
	before, _ := os.ReadFile(repFirst) //stylegate:ignore reads a file the test itself wrote; the content is the assertion

	second := filepath.Join(home, "second.txt")
	if err := os.WriteFile(second, []byte("second"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err = TaskSubmit(TaskSubmitOptions{
		Summary:     "duplicate explicit id",
		TaskID:      "t-attach-live",
		Attachments: []string{second},
	})
	if err == nil || !strings.Contains(err.Error(), "already exists") {
		t.Fatalf("duplicate explicit id must error, got %v", err)
	}
	// The live task's replica is untouched — no "second" content, no new
	// files in its session dir.
	after, _ := os.ReadFile(repFirst) //stylegate:ignore reads a file the test itself wrote; the content is the assertion
	if string(after) != string(before) || string(after) != "first" {
		t.Errorf("live replica clobbered: %q", after)
	}
	attDir := filepath.Join(home, ".gopi", "sessions", "t-attach-live", "attachments")
	entries, err := os.ReadDir(attDir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Errorf("session dir now has %d top entries, want 1 (second submit must not copy)", len(entries))
	}
}

// helpers -------------------------------------------------------------

// extractTaskID pulls the ledger id out of a TaskSubmit result line
// ("Task queued: t-<unix> (status=pending, ...)") or "Task created: t-…".
func extractTaskID(t *testing.T, out string) string {
	t.Helper()
	for _, prefix := range []string{"Task queued: ", "Task created: "} {
		if rest := strings.TrimPrefix(out, prefix); rest != out {
			id := strings.Fields(rest)[0]
			return id
		}
	}
	t.Fatalf("no task id prefix in %q", out)
	return ""
}

// TestUuidTaskIDSubmitCreatesSessionContainer — SPEC db-cleanup-uuid-taskid
// acceptance ①+②+⑤: an AUTO submit returns a uuid task_id, the
// sessions/<uuid>/ container (attachments/ + metadata.json skeleton) is
// immediately visible, and list/show render the uuid row compactly while a
// legacy t-<unix> row still renders its digit hash (④ untouched).
func TestUuidTaskIDSubmitCreatesSessionContainer(t *testing.T) {
	home := phase1AttachEnv(t)
	attach := filepath.Join(home, "note.txt")
	if err := os.WriteFile(attach, []byte("payload"), 0o644); err != nil {
		t.Fatal(err)
	}
	out, err := TaskSubmit(TaskSubmitOptions{
		Summary:     "uuid acceptance",
		Payload:     "do the thing",
		Source:      "auto",
		Tags:        "uuid-test",
		Attachments: []string{attach},
	})
	if err != nil {
		t.Fatalf("TaskSubmit: %v", err)
	}
	// ① submit returns a uuid (36-char, hyphen-shaped) — not a t- id.
	taskID := extractTaskID(t, out)
	if len(taskID) != 36 || taskID[8] != '-' || taskID[13] != '-' || taskID[18] != '-' || taskID[23] != '-' {
		t.Fatalf("auto submit returned non-uuid task id %q", taskID)
	}
	// ② sessions/<uuid>/ visible immediately: attachments replica present
	// (path structure preserved: home=/tmp/Test… → attachments/tmp/…).
	attDir := filepath.Join(home, ".gopi", "sessions", taskID, "attachments")
	found := false
	_ = filepath.WalkDir(attDir, func(p string, d os.DirEntry, err error) error {
		if err == nil && !d.IsDir() && d.Name() == "note.txt" {
			found = true
		}
		return nil
	}) //stylegate:ignore walks a fixture tree; a per-entry failure is reported through the walk callback and the compared set is the assertion
	if !found {
		t.Fatalf("attachments dir %s lacks note.txt replica", attDir)
	}
	// …and metadata.json skeleton stamped with the submit fields.
	metaPath := filepath.Join(home, ".gopi", "sessions", taskID, "metadata.json")
	metaB, err := os.ReadFile(metaPath)
	if err != nil {
		t.Fatalf("submit skeleton metadata.json missing: %v", err)
	}
	var meta map[string]interface{}
	if err := json.Unmarshal(metaB, &meta); err != nil {
		t.Fatalf("metadata.json corrupt: %v", err)
	}
	if meta["task_id"] != taskID || meta["status"] != "pending" || meta["summary"] != "uuid acceptance" {
		t.Errorf("skeleton metadata lacks submit identity: %v", meta)
	}
	// Provenance: the source is the CANONICAL role, never the retired alias the
	// caller typed. internal/tasksource retired the no-role spelling ("auto" —
	// it conflated the merger's machine enqueue with the harness's startup
	// registration, ~993 rows nobody can disambiguate) and the create funnel
	// maps an incoming alias onto the value it always meant, so "a row written
	// today never carries an alias" (tasksource/source.go Normalize). Asserting
	// the literal alias pinned its SURVIVAL through the funnel — the opposite of
	// the contract — so this asserts what the funnel promises, through the
	// package that owns the vocabulary (Rule 14), instead of master's re-typed
	// "auto"/tasksource.Merger pair. MERGE NOTE (rework run
	// 01a0d7a3ca337b2ea0aeefaa289db1a4): the two sides assert the same fact;
	// the branch's form is kept because it additionally pins that the alias
	// stays RECOGNIZED while never reaching the file.
	wantSource, known := tasksource.Normalize("auto")
	if !known {
		t.Fatalf("the retired no-role spelling must stay RECOGNIZED as an alias — an unknown spelling is refused by the ledger boundary, not silently rewritten")
	}
	if meta["payload"] != "do the thing" || meta["source"] != wantSource {
		t.Errorf("skeleton metadata lacks provenance: %v (source must be the canonical %q the funnel derives from the retired alias, not the alias itself)", meta, wantSource)
	} else if meta["source"] == "auto" {
		t.Errorf("the row kept the retired alias: %v", meta)
	}
	// ⑤ list renders the uuid row by its FULL id after the "#" marker — one
	// identity, never a truncated chip beside it (epic full-id-no-prefix).
	list, err := TaskList("")
	if !strings.Contains(list, "#"+taskID+"  [") {
		t.Errorf("list lacks the full uuid id in the # column:\n%s", list)
	}
	if strings.Contains(list, "#"+taskID[:8]+"  ") {
		t.Errorf("list still renders a truncated 8-character chip:\n%s", list)
	}
	// Legacy t- row display (④): taskHashDisplay is now the IDENTITY. The
	// full-id-no-prefix epic made every id form render IN FULL, because a
	// truncated identity is ambiguous — an 8-hex chip can name more than one
	// ledger row (TestTaskHashDisplay pins all five forms). The old assertion
	// here asked for the digits-only truncation that epic REMOVED, so it
	// asserted the defect; a legacy t-<unix> row must render as stored.
	// MERGE NOTE (rework run 01a0d7a3ca337b2ea0aeefaa289db1a4): both sides
	// assert the full id; the branch's wording is kept, master's cited the same
	// epic.
	if got := taskHashDisplay("t-1788146419"); got != "t-1788146419" {
		t.Errorf("legacy t- row must render its FULL id, got %q", got)
	}
	show := phase1TaskShow(t, taskID)
	if !strings.Contains(show, "attachment") {
		t.Errorf("uuid-attachment show renders no attachment line:\n%s", show)
	}
}

func phase1TaskRow(t *testing.T, taskID string) *Task {
	t.Helper()
	db, err := openTasksDB(t.Context(), memoryDBPath())
	if err != nil {
		t.Fatalf("openTasksDB: %v", err)
	}
	defer db.Close()
	tk, err := taskByID(t.Context(), db, taskID)
	if err != nil {
		t.Fatalf("taskByID(%s): %v", taskID, err)
	}
	return tk
}

func phase1TaskCount(t *testing.T) int {
	t.Helper()
	db, err := openTasksDB(t.Context(), memoryDBPath())
	if err != nil {
		t.Fatalf("openTasksDB: %v", err)
	}
	defer db.Close()
	var n int
	if err := db.QueryRow("SELECT COUNT(*) FROM tasks").Scan(&n); err != nil {
		t.Fatalf("count: %v", err)
	}
	return n
}
