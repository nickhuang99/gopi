package tools

// Function-level coverage for the task-session / attachment read+write faces
// (task_session.go): TaskSessionDir, TaskAttachmentsDir, TaskAttachmentRelPath,
// TaskCopyAttachments, TaskDeliverableAttachments.
//
// These assert CONTRACTS, not just calls:
//
//	① path shape  — <gopi-dir>/sessions/<task_id>/attachments (S1 root), and
//	                the path is PURE (no directory is created by resolving it);
//	② round-trip  — every manifest entry's replica_rel_path resolves back to
//	                the replica the copy wrote, whose bytes hash to sha256;
//	③ collision   — same-basename files in different absolute dirs land in
//	                DIFFERENT replicas (the absolute-minus-root mapping);
//	④ errors      — relative path / missing file / non-regular file abort the
//	                whole copy (no partial replicas), an unresolvable ledger
//	                read errors, an unknown row or an attachment-free row is
//	                (nil, nil, nil) — never an error.
//
// Isolated space: every test points GOPI_HOME at a t.TempDir() and restores
// it via t.Setenv (GoPi Rule 5 discipline: no repo or ~/.gopi writes).

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nickhuang99/gopi/internal/config"
)

// covLifeTaskID is a fixed ledger id used by the attachment tests.
const covLifeTaskID = "t-cov-final"

// covLifeSessionEnv isolates GOPI_HOME + the tasks ledger in a t.TempDir() and
// returns the temp HOME (Rule 1b: the env NAMES come from their constants,
// never re-typed literals).
func covLifeSessionEnv(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv(config.GopiDirEnv, home)
	t.Setenv(memoryDBEnv, filepath.Join(home, memoryDefaultDBFile))
	return home
}

// TestCovTaskSessionDirShape asserts the session-dir contract: the S1 root
// (<gopi-dir>/sessions/<task_id>), the attachments area one level below it,
// and that resolving a path creates NOTHING (the container is created on
// use, by taskEnsureTaskSessionDirs/taskAttachmentCopy only).
func TestCovTaskSessionDirShape(t *testing.T) {
	home := covLifeSessionEnv(t)

	sd, err := TaskSessionDir("t-cov-shape")
	if err != nil {
		t.Fatalf("TaskSessionDir: %v", err)
	}
	wantSD := filepath.Join(home, ".gopi", config.SessionsDirName, "t-cov-shape")
	if sd != wantSD {
		t.Errorf("TaskSessionDir = %q, want %q (<gopi-dir>/%s/<task_id>)", sd, wantSD, config.SessionsDirName)
	}
	if _, err := os.Stat(sd); !os.IsNotExist(err) {
		t.Errorf("TaskSessionDir must not create %s (stat err = %v)", sd, err)
	}

	ad, err := TaskAttachmentsDir("t-cov-shape")
	if err != nil {
		t.Fatalf("TaskAttachmentsDir: %v", err)
	}
	if want := filepath.Join(wantSD, taskAttachmentsDirName); ad != want {
		t.Errorf("TaskAttachmentsDir = %q, want %q", ad, want)
	}
	if _, err := os.Stat(ad); !os.IsNotExist(err) {
		t.Errorf("TaskAttachmentsDir must not create %s (stat err = %v)", ad, err)
	}
}

// TestCovTaskSessionDirEmptyIDBoundary pins the empty/unknown-id boundary:
// an empty id resolves to the sessions ROOT (filepath.Join cleans the empty
// element) — no error, no panic, no invented subdirectory. Callers validate
// ids; this documents the unguarded contract.
func TestCovTaskSessionDirEmptyIDBoundary(t *testing.T) {
	home := covLifeSessionEnv(t)

	sd, err := TaskSessionDir("")
	if err != nil {
		t.Fatalf("TaskSessionDir(\"\"): %v", err)
	}
	if want := filepath.Join(home, ".gopi", config.SessionsDirName); sd != want {
		t.Errorf("TaskSessionDir(\"\") = %q, want the sessions root %q", sd, want)
	}
}

// TestCovTaskSessionDirMigratesLegacyContainer covers the Phase-1 → S1
// relocation that taskSessionDir performs on first use: the retired
// <gopi-dir>/task-sessions/<id> container moves wholesale into the sessions
// tree (attachments intact), the legacy root is left empty, and a second
// call is an idempotent no-op.
func TestCovTaskSessionDirMigratesLegacyContainer(t *testing.T) {
	home := covLifeSessionEnv(t)
	legacy := filepath.Join(home, ".gopi", taskLegacySessionsRootName, "t-cov-legacy")
	if err := os.MkdirAll(filepath.Join(legacy, taskAttachmentsDirName), 0o755); err != nil {
		t.Fatalf("seed legacy container: %v", err)
	}
	legacyFile := filepath.Join(legacy, taskAttachmentsDirName, "keep.txt")
	if err := os.WriteFile(legacyFile, []byte("legacy payload"), 0o644); err != nil {
		t.Fatalf("seed legacy file: %v", err)
	}

	sd, err := TaskSessionDir("t-cov-legacy")
	if err != nil {
		t.Fatalf("TaskSessionDir: %v", err)
	}
	migrated := filepath.Join(sd, taskAttachmentsDirName, "keep.txt")
	if data, err := os.ReadFile(migrated); err != nil || string(data) != "legacy payload" {
		t.Fatalf("legacy container not migrated to %s (err=%v data=%q)", migrated, err, data)
	}
	if _, err := os.Stat(legacy); !os.IsNotExist(err) {
		t.Errorf("legacy container %s must be gone after migration (stat err = %v)", legacy, err)
	}

	// Idempotent: the second resolve finds nothing legacy to move.
	if sd2, err := TaskSessionDir("t-cov-legacy"); err != nil || sd2 != sd {
		t.Errorf("second TaskSessionDir = (%q, %v), want (%q, nil)", sd2, err, sd)
	}
}

// TestCovTaskSessionDirLegacyCollisionNewWins covers the migration's
// collision arm: when a container already exists at the S1 location, the
// NEW copy wins and the legacy copy is dropped (never merged over live work).
func TestCovTaskSessionDirLegacyCollisionNewWins(t *testing.T) {
	home := covLifeSessionEnv(t)
	gd := filepath.Join(home, ".gopi")
	legacy := filepath.Join(gd, taskLegacySessionsRootName, "t-cov-collide")
	current := filepath.Join(gd, config.SessionsDirName, "t-cov-collide")
	for _, d := range []string{legacy, current} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", d, err)
		}
	}
	if err := os.WriteFile(filepath.Join(legacy, "old.txt"), []byte("old"), 0o644); err != nil {
		t.Fatalf("seed legacy file: %v", err)
	}
	if err := os.WriteFile(filepath.Join(current, "new.txt"), []byte("new"), 0o644); err != nil {
		t.Fatalf("seed current file: %v", err)
	}

	sd, err := TaskSessionDir("t-cov-collide")
	if err != nil {
		t.Fatalf("TaskSessionDir: %v", err)
	}
	if sd != current {
		t.Fatalf("TaskSessionDir = %q, want the existing S1 container %q", sd, current)
	}
	if _, err := os.Stat(filepath.Join(current, "new.txt")); err != nil {
		t.Errorf("S1 container content lost: %v", err)
	}
	if _, err := os.Stat(legacy); !os.IsNotExist(err) {
		t.Errorf("legacy collision copy %s must be dropped (stat err = %v)", legacy, err)
	}
}

// TestCovTaskAttachmentRelPathMapping pins the bidirectional mapping key:
// the original ABSOLUTE path minus its leading "/" — and, for the same
// basename in two different directories, two DIFFERENT replica paths.
func TestCovTaskAttachmentRelPathMapping(t *testing.T) {
	cases := []struct{ in, want string }{
		{"/tmp/log.txt", "tmp/log.txt"},
		{"/home/user/log.txt", "home/the operator/log.txt"},
		{"  /tmp/trimmed.txt  ", "tmp/trimmed.txt"}, // whitespace-trimmed
		{"//tmp/double.txt", "/tmp/double.txt"},     // exactly ONE leading slash removed
		{"/", ""},                                   // the root is empty after the strip
		{"tmp/relative.txt", ""},                    // non-absolute — the caller validates
		{"", ""},
		{"relative", ""},
	}
	for _, c := range cases {
		if got := TaskAttachmentRelPath(c.in); got != c.want {
			t.Errorf("TaskAttachmentRelPath(%q) = %q, want %q", c.in, got, c.want)
		}
	}

	// Collision avoidance: same basename, different dirs → distinct rels, and
	// distinct resolved replica paths under the same attachments area.
	a := TaskAttachmentRelPath("/tmp/log.txt")
	b := TaskAttachmentRelPath("/home/user/log.txt")
	if a == b {
		t.Fatalf("same-basename attachments collided: both map to %q", a)
	}
	sd := filepath.Join(t.TempDir(), "sessions", covLifeTaskID)
	pa := filepath.Join(sd, taskAttachmentsDirName, filepath.FromSlash(a))
	pb := filepath.Join(sd, taskAttachmentsDirName, filepath.FromSlash(b))
	if pa == pb {
		t.Errorf("resolved replicas collide: %s", pa)
	}
}

// TestCovTaskCopyAttachmentsRoundTrip copies two same-named files from
// different directories and asserts the full contract: order preserved,
// original path recorded verbatim, replica path resolvable with ONE join
// (<session-dir>/<replica_rel_path>), bytes identical, sha256 + size exact,
// and the two replicas are separate files with their own content.
func TestCovTaskCopyAttachmentsRoundTrip(t *testing.T) {
	covLifeSessionEnv(t)
	srcA := filepath.Join(t.TempDir(), "a", "log.txt")
	srcB := filepath.Join(t.TempDir(), "b", "log.txt")
	covLifeWriteFile(t, srcA, "AAA content\n")
	covLifeWriteFile(t, srcB, "BBB content\n")

	atts, err := TaskCopyAttachments(covLifeTaskID, []string{srcA, srcB})
	if err != nil {
		t.Fatalf("TaskCopyAttachments: %v", err)
	}
	if len(atts) != 2 {
		t.Fatalf("manifest has %d entries, want 2 (order/duplication broken)", len(atts))
	}
	sd, err := TaskSessionDir(covLifeTaskID)
	if err != nil {
		t.Fatalf("TaskSessionDir: %v", err)
	}
	seen := map[string]string{}
	for i, want := range []string{srcA, srcB} {
		got := atts[i]
		if got.OriginalAbsPath != want {
			t.Errorf("manifest[%d].OriginalAbsPath = %q, want %q (submit order must be preserved)", i, got.OriginalAbsPath, want)
		}
		if wantRel := taskAttachmentsDirName + "/" + TaskAttachmentRelPath(want); got.ReplicaRelPath != wantRel {
			t.Errorf("manifest[%d].ReplicaRelPath = %q, want %q", i, got.ReplicaRelPath, wantRel)
		}
		replica := filepath.Join(sd, filepath.FromSlash(got.ReplicaRelPath))
		data, err := os.ReadFile(replica)
		if err != nil {
			t.Fatalf("replica %s unreadable: %v", replica, err)
		}
		if got.FileSize != int64(len(data)) || got.FileSize == 0 {
			t.Errorf("manifest[%d].FileSize = %d, want %d", i, got.FileSize, len(data))
		}
		if want := sha256hex(t, replica); got.SHA256 != want {
			t.Errorf("manifest[%d].SHA256 = %q, want %q (digest must match the replica bytes)", i, got.SHA256, want)
		}
		if prev, dup := seen[replica]; dup {
			t.Errorf("two attachments share one replica %s (collision) — other content %q", replica, prev)
		}
		seen[replica] = string(data)
	}
	if seen[filepath.Join(sd, filepath.FromSlash(atts[0].ReplicaRelPath))] == seen[filepath.Join(sd, filepath.FromSlash(atts[1].ReplicaRelPath))] {
		t.Errorf("replicas carry the same content — the second copy overwrote the first")
	}
}

// TestCovTaskCopyAttachmentsErrors covers the validation contract: every
// path is validated BEFORE the first copy, so a bad argument leaves no
// partial replica behind; a no-argument call is a nil-manifest no-op that
// creates nothing.
func TestCovTaskCopyAttachmentsErrors(t *testing.T) {
	home := covLifeSessionEnv(t)
	good := filepath.Join(t.TempDir(), "ok.txt")
	covLifeWriteFile(t, good, "ok")
	dir := t.TempDir()

	cases := []struct {
		name  string
		paths []string
	}{
		{"relative path", []string{"relative/x.txt"}},
		{"missing file", []string{filepath.Join(dir, "nope.txt")}},
		{"directory not a file", []string{dir}},
		{"good then bad — nothing may land", []string{good, filepath.Join(dir, "nope.txt")}},
	}
	for _, c := range cases {
		atts, err := TaskCopyAttachments("t-cov-err-"+strings.ReplaceAll(c.name, " ", "-"), c.paths)
		if err == nil {
			t.Errorf("%s: want an error, got manifest %+v", c.name, atts)
		}
		if atts != nil {
			t.Errorf("%s: want a nil manifest on failure, got %+v", c.name, atts)
		}
	}
	// The failed "good then bad" case must not have created a session dir for
	// its id (validation precedes any copy/container creation).
	if _, err := os.Stat(filepath.Join(home, ".gopi", config.SessionsDirName, "t-cov-err-good-then-bad")); !os.IsNotExist(err) {
		t.Errorf("aborted copy created a session container (stat err = %v)", err)
	}

	atts, err := TaskCopyAttachments("t-cov-none", nil)
	if err != nil || atts != nil {
		t.Errorf("TaskCopyAttachments(nil) = (%+v, %v), want (nil, nil)", atts, err)
	}
	if _, err := os.Stat(filepath.Join(home, ".gopi", config.SessionsDirName, "t-cov-none")); !os.IsNotExist(err) {
		t.Errorf("empty attachment list must create no container (stat err = %v)", err)
	}
}

// TestCovTaskDeliverableAttachmentsRoundTrip seeds a ledger row with a v2
// deliverable built from real copies, then reads it back through the
// run-time read face: the typed manifest and the replica ABSOLUTE paths must
// agree with what was copied (manifest ↔ disk round-trip).
func TestCovTaskDeliverableAttachmentsRoundTrip(t *testing.T) {
	covLifeSessionEnv(t)
	src := filepath.Join(t.TempDir(), "shot", "pic.png")
	covLifeWriteFile(t, src, "PNG-ish bytes")

	atts, err := TaskCopyAttachments(covLifeTaskID, []string{src})
	if err != nil {
		t.Fatalf("TaskCopyAttachments: %v", err)
	}
	blob := taskDeliverableRenderV2("/tmp/repo", atts)
	covLifeSeedTaskRow(t, covLifeTaskID, blob)

	gotAtts, replicaPaths, err := TaskDeliverableAttachments(covLifeTaskID)
	if err != nil {
		t.Fatalf("TaskDeliverableAttachments: %v", err)
	}
	if len(gotAtts) != 1 || len(replicaPaths) != 1 {
		t.Fatalf("read back (%d atts, %d replica paths), want (1, 1)", len(gotAtts), len(replicaPaths))
	}
	if gotAtts[0].SHA256 != atts[0].SHA256 || gotAtts[0].OriginalAbsPath != src {
		t.Errorf("manifest round-trip mismatch: got %+v, want %+v", gotAtts[0], atts[0])
	}
	data, err := os.ReadFile(replicaPaths[0])
	if err != nil {
		t.Fatalf("replica path %s unreadable: %v", replicaPaths[0], err)
	}
	if string(data) != "PNG-ish bytes" {
		t.Errorf("replica content = %q, want the copied bytes", data)
	}
	sd, _ := TaskSessionDir(covLifeTaskID) //stylegate:ignore resolves the fixture session directory; the compared path is the assertion
	if want := filepath.Join(sd, filepath.FromSlash(atts[0].ReplicaRelPath)); replicaPaths[0] != want {
		t.Errorf("replica path = %q, want the ONE-join resolution %q", replicaPaths[0], want)
	}
}

// TestCovTaskDeliverableAttachmentsBoundaries covers the read face's
// non-error boundaries: an unknown task id and an attachment-free row both
// return (nil, nil, nil); an unreadable ledger is a real error (never a
// silent empty result — Rule 15b).
func TestCovTaskDeliverableAttachmentsBoundaries(t *testing.T) {
	home := covLifeSessionEnv(t)

	atts, paths, err := TaskDeliverableAttachments("t-cov-absent")
	if err != nil || atts != nil || paths != nil {
		t.Errorf("unknown row = (%+v, %+v, %v), want (nil, nil, nil)", atts, paths, err)
	}

	covLifeSeedTaskRow(t, "t-cov-plain", `{"repo":"/tmp/repo"}`)
	atts, paths, err = TaskDeliverableAttachments("t-cov-plain")
	if err != nil || atts != nil || paths != nil {
		t.Errorf("attachment-free row = (%+v, %+v, %v), want (nil, nil, nil)", atts, paths, err)
	}

	// Unreadable ledger: a directory where the DB file is expected.
	t.Setenv(memoryDBEnv, home) // a DIRECTORY → sqlite cannot open it
	if _, _, err := TaskDeliverableAttachments("t-cov-plain"); err == nil {
		t.Errorf("unopenable ledger must return an error, got nil")
	}
}

// covLifeWriteFile writes a fixture file, creating its parent directory.
func covLifeWriteFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

// covLifeSeedTaskRow creates a terminal ledger row carrying the given raw
// deliverable blob (the read faces only need the row + its blob).
func covLifeSeedTaskRow(t *testing.T, taskID, deliverable string) {
	t.Helper()
	if _, err := TaskNew("cov fixture "+taskID, taskID, "", ""); err != nil {
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
