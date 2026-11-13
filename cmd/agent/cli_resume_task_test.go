package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nick/gopi/internal/config"
	"github.com/nick/gopi/internal/sessionlayout"
	"github.com/nick/gopi/internal/tools"
)

// TestParseResumeArgs: the --task linkage option (M4.7) — accepted in
// space and = form, positionals pass through, unknown flags / empty values
// are refused.
func TestParseResumeArgs(t *testing.T) {
	cases := []struct {
		name         string
		args         []string
		wantTask     string
		wantContFile string
		wantErr      bool
	}{
		{"absent", []string{}, "", "", false},
		{"continuation only", []string{"/tmp/cont.md"}, "", "/tmp/cont.md", false},
		{"space form", []string{"--task", "t-1788048877"}, "t-1788048877", "", false},
		{"eq form", []string{"--task=t-123"}, "t-123", "", false},
		{"task + continuation", []string{"--task", "t-123", "/tmp/cont.md"}, "t-123", "/tmp/cont.md", false},
		{"continuation + task", []string{"/tmp/cont.md", "--task", "t-123"}, "t-123", "/tmp/cont.md", false},
		{"trims spaces", []string{"--task", "  t-123  "}, "t-123", "", false},
		{"missing value", []string{"--task"}, "", "", true},
		{"empty value", []string{"--task", ""}, "", "", true},
		{"value looks like flag", []string{"--task", "--db"}, "", "", true},
		{"unknown flag", []string{"--db", "x"}, "", "", true},
	}
	for _, tc := range cases {
		gotTask, gotCont, err := parseResumeArgs(tc.args)
		if tc.wantErr {
			if err == nil {
				t.Errorf("%s: want error, got taskID=%q continuation=%q", tc.name, gotTask, gotCont)
			}
			continue
		}
		if err != nil {
			t.Errorf("%s: unexpected error: %v", tc.name, err)
		}
		if gotTask != tc.wantTask || gotCont != tc.wantContFile {
			t.Errorf("%s: got (%q, %q), want (%q, %q)", tc.name, gotTask, gotCont, tc.wantTask, tc.wantContFile)
		}
	}
}

// TestTaskTagResumedRoundTrip: `gopi session resume --task T` stamps the fresh
// session id into the task row as resumed:<session-id> — visible in show's
// tags, counted by TaskResumeCount, surfaced by the list marker, idempotent
// on re-tag, and distinct sessions accumulate.
//
// THE SESSION IDS HERE ARE BACKED BY ARTIFACTS (F16.1): the funnel refuses an
// id that names no session of this profile, so the fixture lays down one bare
// session directory per id — exactly the shape internal/agent writes
// (sessions/<session-id>/session.jsonl) — under a sandbox GOPI_HOME this test
// owns. Without the artifacts the calls would be refused, which is the point of
// the sibling refusal tests in internal/tools.
func TestTaskTagResumedRoundTrip(t *testing.T) {
	home := t.TempDir()
	path := filepath.Join(home, "memory.db")
	t.Setenv("GOPI_MEMORY_DB", path)
	t.Setenv(config.GopiDirEnv, home)
	t.Setenv("HOME", home)
	sessionsRoot := filepath.Join(home, config.ProfileDirName, config.SessionsDirName)

	if _, err := tools.TaskNew("orphan resume linkage", "t-resume-1", "", ""); err != nil {
		t.Fatalf("TaskNew: %v", err)
	}

	for _, sess := range []string{"sess-aaaa", "sess-bbbb"} {
		seedResumeSession(t, sessionsRoot, sess)
	}

	if _, err := tools.TaskTagResumed("t-resume-1", "sess-aaaa"); err != nil {
		t.Fatalf("TaskTagResumed 1: %v", err)
	}
	// Idempotent re-tag of the SAME session.
	if _, err := tools.TaskTagResumed("t-resume-1", "sess-aaaa"); err != nil {
		t.Fatalf("TaskTagResumed repeat: %v", err)
	}
	// A second, different session (resume ran again).
	if _, err := tools.TaskTagResumed("t-resume-1", "sess-bbbb"); err != nil {
		t.Fatalf("TaskTagResumed 2: %v", err)
	}
	// Unknown task is an error.
	if _, err := tools.TaskTagResumed("t-nope", "sess-cccc"); err == nil {
		t.Fatalf("TaskTagResumed unknown task: want error")
	}
	// Empty args are an error.
	if _, err := tools.TaskTagResumed("", "sess-dddd"); err == nil {
		t.Fatalf("TaskTagResumed empty taskID: want error")
	}
	// A session with NO artifact is an error, and it leaves the row alone
	// (F16.1: the tag is an audit record).
	if _, err := tools.TaskTagResumed("t-resume-1", "sess-cccc"); err == nil {
		t.Fatalf("TaskTagResumed unknown session: want error")
	}

	if got := tools.TaskResumeCount("t-resume-1"); got != 2 {
		t.Fatalf("TaskResumeCount = %d, want 2", got)
	}
	show, err := tools.TaskShow("t-resume-1")
	if err != nil {
		t.Fatalf("TaskShow: %v", err)
	}
	for _, want := range []string{"resumed:sess-aaaa", "resumed:sess-bbbb", "resumed:        ×2"} {
		if !strings.Contains(show, want) {
			t.Errorf("TaskShow output missing %q:\n%s", want, show)
		}
	}
}

// seedResumeSession lays down the artifact a real session leaves for sess: the
// bare-session layout (sessions/<session-id>/session.jsonl) whose first line is
// the session_start event carrying that id — the minimal sample the funnel's
// check probes by NAME (Rule 10.4).
func seedResumeSession(t *testing.T, sessionsRoot, sess string) {
	t.Helper()
	dir := filepath.Join(sessionsRoot, sess)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", dir, err)
	}
	line := `{"type":"session_start","sessionId":"` + sess + `"}` + "\n"
	if err := os.WriteFile(filepath.Join(dir, sessionlayout.RecordName), []byte(line), 0o644); err != nil {
		t.Fatalf("write session record: %v", err)
	}
}

// TestTaskListShowsResumedMarker: a task carrying resumed: tags renders the
// [resumed×N] marker in `gopi task list`; a never-resumed task does not.
func TestTaskListShowsResumedMarker(t *testing.T) {
	home := t.TempDir()
	t.Setenv("GOPI_MEMORY_DB", filepath.Join(home, "memory.db"))
	t.Setenv(config.GopiDirEnv, home)
	t.Setenv("HOME", home)
	seedResumeSession(t, filepath.Join(home, config.ProfileDirName, config.SessionsDirName), "sess-zzzz")

	if _, err := tools.TaskNew("resumed task", "t-resume-list", "", ""); err != nil {
		t.Fatalf("TaskNew: %v", err)
	}
	if _, err := tools.TaskNew("plain task", "t-plain", "", ""); err != nil {
		t.Fatalf("TaskNew: %v", err)
	}
	if _, err := tools.TaskTagResumed("t-resume-list", "sess-zzzz"); err != nil {
		t.Fatalf("TaskTagResumed: %v", err)
	}

	list, err := tools.TaskList("")
	if err != nil {
		t.Fatalf("TaskList: %v", err)
	}
	if !strings.Contains(list, "[resumed×1]") {
		t.Errorf("TaskList output missing [resumed×1] marker:\n%s", list)
	}
	if strings.Count(list, "[resumed") != 1 {
		t.Errorf("TaskList: resumed marker should appear exactly once:\n%s", list)
	}
}
