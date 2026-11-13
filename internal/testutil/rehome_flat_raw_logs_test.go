package testutil

// The FLAT-RAW-LOG re-homing contract (epic task-session-cwd, 2026-09-12):
// ~/.gopi/raw/ is the RETIRED flat raw-log directory. A raw HTTP log is a
// session's evidence, so it belongs INSIDE that session's directory — the flat
// area is what an operator can neither attribute nor safely delete.
//
// WHY A SCRIPT TEST: like rehome-task-logs.sh, the sweep is a one-off over the
// operator's EXISTING tree (files no current code writes), so it lives in
// scripts/ and is driven through its own env seams here — the rules are never
// reimplemented in Go, which is what keeps this a test OF the sweep.
//
// WHAT IT PINS (each a rule a future editor could break):
//  1. LOCATED evidence (a record quoting the file's path — session_start's
//     rawLogPath) with ONE owner → the log moves into that session's directory;
//  2. LOCATED evidence with >1 owner → AMBIGUOUS: left in place, never guessed
//     — mis-attributing puts a stranger's evidence inside a task dir;
//  3. a bare MENTION with no location is WEAK, not ownership: the file is left
//     in place (one `ls ~/.gopi/raw` names every file in the directory, so bare
//     names over-attribute — measured 794 files mentioned vs 228 located);
//  4. NO mention at all → reported, deleted only under --delete;
//  5. the summary reports all four buckets, and a dry run SAYS nothing was
//     removed (the operator must never have to infer whether data was lost);
//  6. --move is idempotent: a second run finds nothing left to move;
//  7. the sweep only ever considers the legacy name shape
//     (gopi-raw-<stamp>.log) — a session's OWN raw log is not its business.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// flatRawScriptName is the sweep under test.
const flatRawScriptName = "rehome-flat-raw-logs.sh"

// writeFlatRawFixture builds one synthetic profile covering all four buckets:
//
//	located (session_start rawLogPath)      → 20260803-184426
//	located by TWO sessions (contested)     → 20260803-190611
//	mentioned without a location (weak)     → 20260803-210000
//	mentioned by nobody (invisible)         → 20260804-000000
func writeFlatRawFixture(t *testing.T) (sessions, flat string) {
	t.Helper()
	root := t.TempDir()
	sessions = filepath.Join(root, "sessions")
	flat = filepath.Join(root, "raw")
	for _, d := range []string{"aaa", "bbb", "ccc"} {
		if err := os.MkdirAll(filepath.Join(sessions, d), 0o755); err != nil {
			t.Fatalf("mkdir session %s: %v", d, err)
		}
	}
	write := func(path, body string) {
		t.Helper()
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			t.Fatalf("write %s: %v", path, err)
		}
	}
	// Rule 1: the located reference — the session's OWN rawLogPath record.
	write(filepath.Join(sessions, "aaa", "session.jsonl"),
		`{"type":"session_start","sessionId":"aaa","rawLogPath":"`+flat+`/gopi-raw-20260803-184426.log"}`+"\n")
	// Rule 2: a SECOND session locates the same file → contested.
	write(filepath.Join(sessions, "bbb", "session.jsonl"),
		`{"type":"message","note":"resume `+flat+`/gopi-raw-20260803-190611.log --task t-1"}`+"\n")
	write(filepath.Join(sessions, "ccc", "session.jsonl"),
		`{"type":"message","note":"seeded from `+flat+`/gopi-raw-20260803-190611.log"}`+"\n")
	// Rule 3: a bare mention (the shape an `ls` of the raw dir leaves in a
	// transcript) — names the weak file AND some of its neighbours.
	write(filepath.Join(sessions, "aaa", "notes.txt"),
		"ls -la "+flat+":\ngopi-raw-20260803-210000.log\ngopi-raw-20260803-184426.log\n")

	if err := os.MkdirAll(flat, 0o755); err != nil {
		t.Fatalf("mkdir flat raw: %v", err)
	}
	for _, n := range []string{"20260803-184426", "20260803-190611", "20260803-210000", "20260804-000000"} {
		write(filepath.Join(flat, "gopi-raw-"+n+".log"), "RAW-"+n+"\n")
	}
	// Rule 7: a NEW-layout raw log inside a session dir is not the sweep's
	// business (only the flat dir + legacy name shape are in scope).
	write(filepath.Join(sessions, "aaa", "aaa.raw.log"), "NEW-LAYOUT\n")
	return sessions, flat
}

// runFlatRaw drives the sweep against the synthetic profile.
func runFlatRaw(t *testing.T, sessions, flat string, args ...string) string {
	t.Helper()
	return runScript(t, []string{"GOPI_SESSIONS_ROOT=" + sessions, "GOPI_FLAT_RAW_DIR=" + flat},
		flatRawScriptName, args...)
}

// TestRehomeFlatRawLogsMovesOnlyWhatASessionLocates is the whole contract: a
// located log follows its session, a contested or merely-mentioned one is left
// alone, an invisible one is only removed on --delete, and the run is
// idempotent.
func TestRehomeFlatRawLogsMovesOnlyWhatASessionLocates(t *testing.T) {
	sessions, flat := writeFlatRawFixture(t)

	// (5) the default is a dry run: it plans, reports, and deletes nothing.
	plan := runFlatRaw(t, sessions, flat)
	if !strings.Contains(plan, "DRY RUN") {
		t.Fatalf("the default mode must say it changed nothing, got:\n%s", plan)
	}
	if !strings.Contains(plan, "located, one owner : 1 file(s)") ||
		!strings.Contains(plan, "located, contested : 1 file(s)") ||
		!strings.Contains(plan, "weak mention only  : 1 file(s)") ||
		!strings.Contains(plan, "invisible          : 1 file(s)") {
		t.Fatalf("the summary must report all four buckets, got:\n%s", plan)
	}
	for _, n := range []string{"20260803-184426", "20260803-190611", "20260803-210000", "20260804-000000"} {
		if _, err := os.Stat(filepath.Join(flat, "gopi-raw-"+n+".log")); err != nil {
			t.Fatalf("a dry run removed gopi-raw-%s.log: %v", n, err)
		}
	}

	// (1) --move re-homes the located log into its OWN session dir.
	out := runFlatRaw(t, sessions, flat, "--move")
	if !strings.Contains(out, "moved gopi-raw-20260803-184426.log") {
		t.Fatalf("the located log must move to its referencing session, got:\n%s", out)
	}
	moved := filepath.Join(sessions, "aaa", "gopi-raw-20260803-184426.log")
	body, err := os.ReadFile(moved)
	if err != nil {
		t.Fatalf("the located log must now live in its session dir %s: %v", moved, err)
	}
	if !strings.Contains(string(body), "RAW-20260803-184426") {
		t.Errorf("the moved file must be the log itself, got:\n%s", body)
	}

	// (2) the contested log stays put — never guessed into one of the two
	// locating sessions.
	for _, d := range []string{"bbb", "ccc"} {
		if _, err := os.Stat(filepath.Join(sessions, d, "gopi-raw-20260803-190611.log")); err == nil {
			t.Fatalf("a CONTESTED log was attributed to %s — a wrong owner is worse than a stale file", d)
		}
	}
	if _, err := os.Stat(filepath.Join(flat, "gopi-raw-20260803-190611.log")); err != nil {
		t.Fatalf("the contested log must remain in the flat dir: %v", err)
	}
	// (3) the barely-mentioned log stays put too: a mention is not ownership.
	// (4) neither is the invisible one touched without --delete.
	for _, n := range []string{"20260803-210000", "20260804-000000"} {
		if _, err := os.Stat(filepath.Join(flat, "gopi-raw-"+n+".log")); err != nil {
			t.Fatalf("--move must not remove gopi-raw-%s.log (only --delete, only invisible): %v", n, err)
		}
	}

	// (6) idempotent: the located bucket is empty on the second run.
	again := runFlatRaw(t, sessions, flat, "--move")
	if !strings.Contains(again, "located, one owner : 0 file(s)") {
		t.Fatalf("a second --move must find nothing left to move, got:\n%s", again)
	}

	// (4 again) --delete drops ONLY the invisible file.
	del := runFlatRaw(t, sessions, flat, "--delete")
	if !strings.Contains(del, "deleted invisible gopi-raw-20260804-000000.log") {
		t.Fatalf("--delete must remove the invisible log, got:\n%s", del)
	}
	if _, err := os.Stat(filepath.Join(flat, "gopi-raw-20260804-000000.log")); err == nil {
		t.Fatal("the invisible log must be gone after --delete")
	}
	for _, n := range []string{"20260803-190611", "20260803-210000"} {
		if _, err := os.Stat(filepath.Join(flat, "gopi-raw-"+n+".log")); err != nil {
			t.Fatalf("--delete must NEVER remove a located-but-contested or merely-mentioned log (gopi-raw-%s.log): %v", n, err)
		}
	}

	// (7) the session's own new-layout raw log is not the sweep's business.
	if _, err := os.Stat(filepath.Join(sessions, "aaa", "aaa.raw.log")); err != nil {
		t.Fatalf("a session's own raw log must be untouched: %v", err)
	}
}
