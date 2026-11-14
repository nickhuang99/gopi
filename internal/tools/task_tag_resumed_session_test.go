package tools

// F16.1 regression guards: tools.TaskTagResumed (the resume path's lineage
// stamp — the `gopi task tag-resumed` CLI spelling was deleted 2026-10-16, the
// FUNCTION and its checks were not) stamps an AUDIT record, so the session id
// must name an artifact this profile actually wrote.
//
// THE DEFECT THESE PIN (measured, docs/archive/demo/materials/16-tag-resumed/05-finding.md,
// row 01a0ed25-cd2b-7342-a0dc-9c8fd8a9d000 on 2026-09-27):
//
//	$ gopi task tag-resumed <row> 00000000-0000-0000-0000-000000000000  (the pre-2026-10-16 spelling)
//	Task <row> tagged resumed:00000000-0000-0000-0000-000000000000
//	exit=0
//
// and the row then rendered `session_id:` EMPTY beside `resumed: ×1` — the
// row contradicting itself, with the pollution propagating into rows reopened
// from it. The tag was satisfiable by any string, i.e. it was not an audit
// record at all (PROJECT.md Rule 15: bad input is refused, never silently
// accepted).
//
// The fixtures are MINIMAL SAMPLES (Rule 10.4): one row, one artifact, and the
// artifact is laid down with the SHAPE the real writer uses (the layouts
// internal/agent.NewSessionLoggerForRun and NewRawLoggerForRun create), so a
// test that passes cannot be passing because the fixture wrote something the
// product never writes.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nickhuang99/gopi/internal/config"
)

// resumedSessionSandbox points the ledger AND the gopi data root at one fresh
// temp tree, so the session-artifact check reads the TEST's own sessions tree
// instead of the developer's live profile (Rule 32's isolation, applied to the
// sessions root the check reads).
func resumedSessionSandbox(t *testing.T) (gopiHome, dbPath string) {
	t.Helper()
	home := t.TempDir()
	t.Setenv(config.GopiDirEnv, home)
	t.Setenv("HOME", home)
	dbPath = filepath.Join(home, "memory.db")
	t.Setenv(memoryDBEnv, dbPath)
	return home, dbPath
}

// resumedSessionsRoot resolves the sessions tree of the sandbox through the
// ONE resolver the production check uses (config.GopiDir + the shared dir-name
// constant — never a literal).
func resumedSessionsRoot(t *testing.T, gopiHome string) string {
	t.Helper()
	gd, err := config.GopiDir()
	if err != nil {
		t.Fatalf("gopi dir: %v", err)
	}
	if want := filepath.Join(gopiHome, config.ProfileDirName); gd != want {
		t.Fatalf("the sandbox data root is %q, want %q — the check would read the developer's profile", gd, want)
	}
	return filepath.Join(gd, config.SessionsDirName)
}

// resumedSeedRow creates the row under test and returns its id.
func resumedSeedRow(t *testing.T, taskID string) {
	t.Helper()
	if _, err := TaskNewOID(TaskNewOptions{Summary: "tag-resumed fixture " + taskID, TaskID: taskID}); err != nil {
		t.Fatalf("seed %s: %v", taskID, err)
	}
}

// resumedTags reads the row's tags back (the assertion the refusal must leave
// UNCHANGED).
func resumedTags(t *testing.T, taskID string) string {
	t.Helper()
	tags, err := TaskTags(taskID)
	if err != nil {
		t.Fatalf("TaskTags(%s): %v", taskID, err)
	}
	return tags
}

// TestTaskTagResumedRefusesASessionWithNoArtifact is the RED path of F16.1:
// the all-zero uuid — the exact operand from the finding — and a gibberish id
// are both refused, the refusal NAMES the operand, and NOTHING is written to
// the row's tags (so a later `task show` cannot render `resumed: ×1` beside an
// empty `session_id:`).
func TestTaskTagResumedRefusesASessionWithNoArtifact(t *testing.T) {
	_, _ = resumedSessionSandbox(t)
	const taskID = "t-tag-resumed-refuse"
	resumedSeedRow(t, taskID)
	// A real artifact for a DIFFERENT session exists, so the refusal cannot be
	// passing merely because the sandbox tree is empty.
	root := resumedSessionsRoot(t, os.Getenv(config.GopiDirEnv))
	writeFile(t, filepath.Join(root, "01a0ed25-cd2b-7342-a0dc-9c8fd8a9d000", "session.jsonl"),
		sessionStartLine("01a0ed25-cd2b-7342-a0dc-9c8fd8a9d000"))

	// A TASK id is not a session id, even though the sessions tree is organised
	// BY TASK: sessions/<task-id>/ is a real directory whose name looks like an
	// operand, so its interior must not be mistaken for a session record. The
	// fixture is the shape a real task run leaves — sessions/<task-id>/
	// <run-uuid>.jsonl — which holds NO session.jsonl: only a bare session
	// writes that name, and it writes it under its OWN id.
	writeFile(t, filepath.Join(root, "t-tag-resumed-barerefuse", "01a0ed25-cd2b-7342-a0dc-9c8fd8a9d077"+taskSessionJSONLExt),
		sessionStartLine("01a0ed25-cd2b-7342-a0dc-9c8fd8a9d077"))
	if err := os.MkdirAll(filepath.Join(root, "t-empty-run-dir"), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	for _, bad := range []string{
		resumedZeroUUID,
		"zzzz-not-a-session",
		"01a0ed25-cd2b-7342-a0dc-9c8fd8a9d001", // one digit off a real session
		"t-tag-resumed-barerefuse",             // a task id, not a session
		"t-empty-run-dir",                      // an empty run directory
	} {
		out, err := TaskTagResumed(taskID, bad)
		if err == nil {
			t.Fatalf("session id %q must be refused — a tag naming no session is not an audit record; got %q", bad, out)
		}
		if !strings.Contains(err.Error(), bad) {
			t.Errorf("the refusal must NAME the operand %q (Rule 15): %v", bad, err)
		}
		if tags := resumedTags(t, taskID); tags != "" {
			t.Fatalf("a refused call must write NOTHING; tags = %q", tags)
		}
		if n := TaskResumeCount(taskID); n != 0 {
			t.Fatalf("resume count after refusals = %d, want 0", n)
		}
	}
}

// TestTaskTagResumedAcceptsEveryRealArtifactShape pins the ACCEPT half: each
// shape a real session leaves on disk is admitted, with the tag carrying the
// CANONICAL id (a short operand is expanded, never stored short).
func TestTaskTagResumedAcceptsEveryRealArtifactShape(t *testing.T) {
	cases := []struct {
		name   string
		id     string
		lay    func(t *testing.T, root, id string)
		expect string // "" = the operand itself
	}{
		{
			name: "task run layout (sessions/<task-id>/<session-id>.jsonl)",
			id:   "01a0ed3a-1111-4111-8111-111111111111",
			lay: func(t *testing.T, root, id string) {
				t.Helper()
				writeFile(t, filepath.Join(root, "t-tag-resumed-shape-run", id+taskSessionJSONLExt), sessionStartLine(id))
			},
		},
		{
			name: "raw log only in the row's own run dir (the JSONL was pruned)",
			id:   "01a0ed3a-2222-4222-8222-222222222222",
			lay: func(t *testing.T, root, id string) {
				t.Helper()
				writeFile(t, filepath.Join(root, "t-tag-resumed-shape-run", id+taskSessionRawLogSuffix), "raw\n")
			},
		},
		{
			name: "bare session layout (sessions/<session-id>/session.jsonl)",
			id:   "01a0ed3a-3333-4333-8333-333333333333",
			lay: func(t *testing.T, root, id string) {
				t.Helper()
				writeFile(t, filepath.Join(root, id, taskBareSessionRecordName), sessionStartLine(id))
			},
		},
		{
			name: "legacy flat layout (sessions/<session-id>.jsonl)",
			id:   "01a0ed3a-4444-4444-8444-444444444444",
			lay: func(t *testing.T, root, id string) {
				t.Helper()
				writeFile(t, filepath.Join(root, id+taskSessionJSONLExt), sessionStartLine(id))
			},
		},
		{
			name: "run directory whose log is named for the id (a second task's run)",
			id:   "01a0ed3a-5555-4555-8555-555555555555",
			lay: func(t *testing.T, root, id string) {
				t.Helper()
				writeFile(t, filepath.Join(root, "t-tag-resumed-shape-run", id+taskSessionJSONLExt), sessionStartLine(id))
			},
		},
		{
			name:   "short id expands to the canonical session dir",
			id:     "01a0ed3a-6666-4666-8666-666666666666",
			expect: "01a0ed3a-6666-4666-8666-666666666666",
			lay: func(t *testing.T, root, id string) {
				t.Helper()
				writeFile(t, filepath.Join(root, id, taskBareSessionRecordName), sessionStartLine(id))
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, _ = resumedSessionSandbox(t)
			taskID := "t-tag-resumed-shape-run"
			resumedSeedRow(t, taskID)
			root := resumedSessionsRoot(t, os.Getenv(config.GopiDirEnv))
			tc.lay(t, root, tc.id)

			operand := tc.id
			want := tc.expect
			if tc.name == "short id expands to the canonical session dir" {
				operand = tc.id[:fullidShortPrefixChars]
				want = tc.id
			}
			out, err := TaskTagResumed(taskID, operand)
			if err != nil {
				t.Fatalf("a session artifact IS on disk for %q; got error %v", operand, err)
			}
			if !strings.Contains(out, taskTagResumedPrefix+want) {
				t.Fatalf("the tag must carry the canonical id %q: %s", want, out)
			}
			if tags := resumedTags(t, taskID); !strings.Contains(tags, taskTagResumedPrefix+want) {
				t.Fatalf("tags = %q, want %q written", tags, taskTagResumedPrefix+want)
			}
			// Idempotent: the same session re-stamped is a no-op, so the count
			// stays 1 (the semantics the help text states).
			if _, err := TaskTagResumed(taskID, operand); err != nil {
				t.Fatalf("re-tag must be a no-op: %v", err)
			}
			if n := TaskResumeCount(taskID); n != 1 {
				t.Fatalf("resume count = %d after a re-tag, want 1", n)
			}
		})
	}
}

// TestTaskTagResumedRefusesAnAmbiguousShortID pins the named-ambiguity half of
// the resolution: two sessions sharing the chunk are BOTH named in the refusal
// (internal/fullid's one spelling of it), and neither tag is written.
func TestTaskTagResumedRefusesAnAmbiguousShortID(t *testing.T) {
	_, _ = resumedSessionSandbox(t)
	const taskID = "t-tag-resumed-ambiguous"
	resumedSeedRow(t, taskID)
	root := resumedSessionsRoot(t, os.Getenv(config.GopiDirEnv))
	a := "01a0ed3b-aaaa-4aaa-8aaa-aaaaaaaaaaaa"
	b := "01a0ed3b-bbbb-4bbb-8bbb-bbbbbbbbbbbb"
	writeFile(t, filepath.Join(root, a, taskBareSessionRecordName), sessionStartLine(a))
	writeFile(t, filepath.Join(root, b, taskBareSessionRecordName), sessionStartLine(b))

	_, err := TaskTagResumed(taskID, a[:fullidShortPrefixChars])
	if err == nil {
		t.Fatalf("a chunk naming two sessions must be refused, never silently picked")
	}
	for _, want := range []string{a, b, a[:fullidShortPrefixChars]} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("the ambiguity refusal must name %q: %v", want, err)
		}
	}
	if tags := resumedTags(t, taskID); tags != "" {
		t.Fatalf("an ambiguous refusal must write nothing; tags = %q", tags)
	}
}

// TestTaskTagResumedIsIdempotentAndCountsDistinctSessions pins the counting
// semantics the help text promises: `resumed:` tags are one per DISTINCT
// session, so re-stamping a session does not inflate the count and a second
// session does.
func TestTaskTagResumedIsIdempotentAndCountsDistinctSessions(t *testing.T) {
	_, _ = resumedSessionSandbox(t)
	const taskID = "t-tag-resumed-count"
	resumedSeedRow(t, taskID)
	root := resumedSessionsRoot(t, os.Getenv(config.GopiDirEnv))
	first := "01a0ed3c-1111-4111-8111-111111111111"
	second := "01a0ed3c-2222-4222-8222-222222222222"
	for _, id := range []string{first, second} {
		writeFile(t, filepath.Join(root, id, taskBareSessionRecordName), sessionStartLine(id))
	}
	for _, id := range []string{first, first, second} {
		if _, err := TaskTagResumed(taskID, id); err != nil {
			t.Fatalf("tag %s: %v", id, err)
		}
	}
	if n := TaskResumeCount(taskID); n != 2 {
		t.Fatalf("resume count = %d, want 2 (one per DISTINCT session)", n)
	}
	show, err := TaskShow(taskID)
	if err != nil {
		t.Fatalf("show: %v", err)
	}
	if !strings.Contains(show, "resumed:        ×2") {
		t.Errorf("task show must summarise the distinct session count:\n%s", show)
	}
}

// TestTaskTagResumedEmptySessionIDIsRefused pins the zero-value path — the ONE
// case that stays a pure input check (nothing to resolve).
func TestTaskTagResumedEmptySessionIDIsRefused(t *testing.T) {
	_, _ = resumedSessionSandbox(t)
	const taskID = "t-tag-resumed-empty"
	resumedSeedRow(t, taskID)
	for _, operand := range []string{"", "   "} {
		if _, err := TaskTagResumed(taskID, operand); err == nil {
			t.Fatalf("an empty session id (%q) must be refused", operand)
		}
	}
	if tags := resumedTags(t, taskID); tags != "" {
		t.Fatalf("nothing may be written for an empty operand; tags = %q", tags)
	}
}

// resumedZeroUUID is the exact operand from the demo finding (F16.1) — the
// all-zero uuid a caller reaches for when it has no session in hand.
const resumedZeroUUID = "00000000-0000-0000-0000-000000000000"

// fullidShortPrefixChars is the chunk length a rendered listing prints (the
// value internal/fullid names ShortPrefixLen) — the shape the short-id arm
// must resolve.
const fullidShortPrefixChars = 8
