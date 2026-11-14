package main

// cli_resume_foreign_checkpoint_test.go — THE REGRESSION GUARD for row 01a180f5:
// a resume whose checkpoint sits OUTSIDE this profile's sessions tree must not
// take the checkpoint's FILE NAME as its session id.
//
// WHAT WAS MEASURED BEFORE THE FIX (2026-10-28, tip 8f06595, isolated profile):
//
//	[gopi] resume: continuing session gopi-raw-resume-e2e.log in /…/001
//	[gopi] resume: warning: task linkage failed (t-… ← gopi-raw-resume-e2e.log):
//	    task: tag-resumed: session id "gopi-raw-resume-e2e.log" names no session
//	    of this profile (no session log, raw log or evidence directory on disk)
//	    — nothing tagged, and a row's resume lineage must name a real session
//
// The id was a FILE NAME, so tools.TaskTagResumed refused it (correctly: no
// profile holds a session of that name) and the row's `resumed:<id>` was never
// written — the lineage the tag exists to record was silently missing. The run
// also wrote its record beside the checkpoint, outside the sessions root, where
// no session of this profile lives.
//
// WHAT THIS PINS. A checkpoint outside the sessions tree names no session to
// continue; the resume therefore STARTS a session — its own fresh uuid, recorded
// under the sessions root — while continuing the CONVERSATION from the
// checkpoint. The three assertions below fail on the pre-fix behaviour and on any
// later "shortcut" that reinstates it: the id in the row is a real session id, it
// is NOT the checkpoint's basename, and an artifact of that id exists on disk
// under the profile's own sessions root.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nickhuang99/gopi/internal/config"
	"github.com/nickhuang99/gopi/internal/fullid"
	"github.com/nickhuang99/gopi/internal/testutil"
	"github.com/nickhuang99/gopi/internal/tools"
)

// resumeForeignCheckpointName is the checkpoint's FILE NAME — the string the
// pre-fix resume adopted as a session id. It is deliberately not uuid-shaped: a
// uuid name would make the defect invisible in a rendered row.
const resumeForeignCheckpointName = "gopi-raw-resume-foreign.log"

// resumeForeignCheckpoint writes a checkpoint OUTSIDE the sessions tree (a copied
// log, an export, a fixture) and returns its path.
func resumeForeignCheckpoint(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, resumeForeignCheckpointName)
	checkpoint := `{"model":"deepseek-v4-flash","max_tokens":16384,"messages":[` +
		`{"role":"user","content":[{"type":"text","text":"[EPIC GOAL]\nfinish the deliverable"}]},` +
		`{"role":"assistant","content":[{"type":"text","text":"working"}]}]}`
	if err := os.WriteFile(path, []byte("→ REQUEST\n"+checkpoint+"\n"), 0o644); err != nil {
		t.Fatalf("write checkpoint: %v", err)
	}
	return path
}

// TestResumeForeignCheckpointMintsASessionID pins the fix in one shot, through
// the REAL binary: a foreign checkpoint yields a REAL session id, the row records
// `resumed:<that id>`, and the id names an artifact this profile holds.
func TestResumeForeignCheckpointMintsASessionID(t *testing.T) {
	const taskID = "t-resume-foreign-e2e"
	rawLog := resumeForeignCheckpoint(t)

	gopiHome := t.TempDir()
	dbPath := filepath.Join(t.TempDir(), "memory.db")
	mock := testutil.NewMockLLM(t, testutil.Resp{Text: "the deliverable is finished"})
	defer mock.Close()
	t.Setenv("GOPI_MEMORY_DB", dbPath)
	env := append(testutil.MockEnv(t, gopiHome, mock.URL()),
		"GOPI_MEMORY_DB="+dbPath, config.SkipClaimBroadcastEnv+"=1")

	// The row the resume LINKS to (the `--task` form: an operator who names a row
	// asserts it exists).
	if _, err := tools.TaskSubmit(tools.TaskSubmitOptions{
		Summary: "resume from a foreign checkpoint",
		Payload: "finish the deliverable",
		TaskID:  taskID,
		Source:  "human",
		Tags:    "e2e",
	}); err != nil {
		t.Fatalf("seed row: %v", err)
	}

	stdout, stderr, code := runGopiMapped(t,
		[]string{CliVerbSession, CliVerbResume, rawLog, "--task", taskID}, "", env, "")
	if code != 0 {
		t.Fatalf("the resume must succeed: code=%d\nstdout:\n%s\nstderr:\n%s", code, stdout, stderr)
	}

	// ① the run says WHAT IT IS: a new session continuing that checkpoint's
	// conversation — never "continuing session <file name>" (Rule 15).
	if strings.Contains(stderr, "continuing session "+resumeForeignCheckpointName) {
		t.Fatalf("the resume adopted the checkpoint's FILE NAME as its session id:\n%s", stderr)
	}
	if !strings.Contains(stderr, "continues the conversation in") {
		t.Errorf("the resume must state that it starts a session and continues the conversation:\n%s", stderr)
	}

	// ② the ROW records the lineage, and the id it records is the fresh session's
	// — read off `task show`, which is the surface an operator reads.
	show, _, showCode := runGopiMapped(t, []string{CliVerbTask, "show", taskID}, "", env, "")
	if showCode != 0 {
		t.Fatalf("task show must succeed: code=%d\n%s", showCode, show)
	}
	sessionID := resumedTagID(t, show)
	if sessionID == "" {
		t.Fatalf("the row records no resumed:<id> — the linkage the resume exists to write is missing:\n%s\nstderr:\n%s", show, stderr)
	}
	if sessionID == resumeForeignCheckpointName {
		t.Fatalf("the row's resumed id is the checkpoint's FILE NAME, not a session id:\n%s", show)
	}
	if !fullid.IsUUID(sessionID) {
		t.Fatalf("the row's resumed id %q is not a session id (a run mints a uuid):\n%s", sessionID, show)
	}

	// ③ and that id names an artifact THIS PROFILE holds under its own sessions
	// root — the fact tools.TaskTagResumed checks, re-checked here from the
	// filesystem so the assertion does not trust the tag alone.
	root := filepath.Join(gopiHome, config.ProfileDirName, config.SessionsDirName)
	if _, err := os.Stat(filepath.Join(root, sessionID, "session.jsonl")); err != nil {
		t.Errorf("the resumed session %s left no record under %s: %v", sessionID, root, err)
	}
}

// resumedTagID reads the id out of a rendered `task show`'s `resumed:<id>` tag —
// the row's own report of what it recorded.
//
// THE TAG IS RENDERED INSIDE THE `tags:` LINE, not on a line of its own
// (`tags:           e2e,resumed:<uuid>`), so the needle is the `resumed:` token
// anywhere in the page and the id runs to the next separator. Parsing it from the
// rendered surface rather than from the database is deliberate: `task show` is
// what an operator reads, and the row's own renderer is the thing that decides
// whether a lineage is visible at all (the pre-fix page summarised
// `resumed: ×1` beside an unusable id — the shape this row exists to stop).
func resumedTagID(t *testing.T, show string) string {
	t.Helper()
	const marker = "resumed:"
	idx := strings.Index(show, marker)
	if idx < 0 {
		return ""
	}
	id := show[idx+len(marker):]
	if i := strings.IndexAny(id, " \t\r\n,"); i >= 0 {
		id = id[:i]
	}
	return strings.TrimSpace(id)
}
