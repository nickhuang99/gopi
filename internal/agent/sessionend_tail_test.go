package agent

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/nickhuang99/gopi/internal/config"
)

// Tests for HasSessionEnd's tail walk. Regression guard for: a cleanly-ended
// session whose JSONL carries a half-written trailing line (SIGKILL mid-write)
// must still report as ended — the candidate slice must be newline-bounded
// (moving end pointer), never pinned to the file end, or the trailing garbage
// splices onto every earlier candidate and the scan-back branch is dead code.

// writeSessionFile writes content verbatim (no newline appended) and returns
// the path.
func writeSessionFile(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "session.jsonl")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write session file: %v", err)
	}
	return path
}

const (
	startLine = `{"type":"session_start","session_id":"s-1"}` + "\n"
	endLine   = `{"type":"session_end","exit_code":0}` + "\n"
)

// TestHasSessionEndGarbageTailAfterSessionEnd is the minimal repro: the file
// ends with a real session_end followed by a torn (newline-free) line. Before
// the fix the candidate was data[i+1 : len(data)-1], so the torn tail was
// glued onto the session_end candidate, json.Unmarshal always failed, and this
// clean run was misclassified as an orphan by DetectOrphanSessions.
func TestHasSessionEndGarbageTailAfterSessionEnd(t *testing.T) {
	path := writeSessionFile(t, startLine+endLine+`{"type":"assistant_text","text":"half-writ`)
	if !HasSessionEnd(path) {
		t.Fatalf("clean session with a torn trailing line must report ended (got false) — tail walk is not newline-bounded")
	}
}

// TestHasSessionEndCases pins the full decision table, including the empty and
// single-line shapes that used to reach the data[len(data)-1] fallback.
func TestHasSessionEndCases(t *testing.T) {
	// Long torn tail: still bounded by the FIRST newline boundary, so the
	// session_end line is found no matter how long the garbage is.
	longTornTail := startLine + endLine + strings.Repeat("x", 1400)

	// Garbage further back than the scan-back bound: the bound must stop the
	// walk instead of degenerating into a full-file scan.
	buriedEnd := startLine + endLine
	for i := 0; i < 60; i++ {
		buriedEnd += `{"type":"noise","i":` + strings.Repeat("9", 100) + `}` + "\n"
	}

	cases := []struct {
		name    string
		content string
		want    bool
	}{
		{"clean with trailing newline", startLine + endLine, true},
		{"clean without trailing newline", strings.TrimSuffix(startLine+endLine, "\n"), true},
		{"garbage tail after session_end (repro)", startLine + endLine + `{"type":"assistant_text"`, true},
		{"long garbage tail after session_end", longTornTail, true},
		{"blank lines after session_end", startLine + endLine + "\n\n", true},
		{"single line session_end no newline", strings.TrimSuffix(endLine, "\n"), true},
		{"single line session_start no newline", strings.TrimSuffix(startLine, "\n"), false},
		{"orphan (no session_end)", startLine, false},
		{"orphan with torn tail", startLine + `{"type":"assistant_text"`, false},
		{"empty file", "", false},
		{"session_end beyond scan-back bound", buriedEnd, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := HasSessionEnd(writeSessionFile(t, tc.content)); got != tc.want {
				t.Fatalf("HasSessionEnd(%q...) = %v, want %v", tc.name, got, tc.want)
			}
		})
	}
}

// TestHasSessionEndMissingFile: unreadable paths stay best-effort false.
func TestHasSessionEndMissingFile(t *testing.T) {
	if HasSessionEnd(filepath.Join(t.TempDir(), "nope.jsonl")) {
		t.Fatal("missing file must report false")
	}
}

// TestDetectOrphanSessionsTornTail: the user-visible symptom of the tail-walk
// bug — a cleanly-ended run (session_end present, torn line after it) was
// listed as an orphan and crash-dumped as if the process had died.
func TestDetectOrphanSessionsTornTail(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv(config.GopiDirEnv, tmp)
	sessionsDir := filepath.Join(tmp, ".gopi", config.SessionsDirName)
	if err := os.MkdirAll(sessionsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	write := func(name, content string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(sessionsDir, name), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("clean.jsonl", deadStampStartLine("clean")+"\n"+endLine)
	write("torn.jsonl", deadStampStartLine("torn")+"\n"+endLine+`{"type":"assistant_text","text":"half-writ`)
	// Orphan: writer gone, NO session_end (killed mid-run) → orphan — but only
	// once it has been QUIET past the live window (a JSONL still being written
	// is a live session, not a crash), so its mtime is backdated.
	write("orphan.jsonl", deadStampStartLine("orphan")+"\n")
	backdate(t, filepath.Join(sessionsDir, "orphan.jsonl"), 2*time.Hour)

	orphans := DetectOrphanSessions()
	if len(orphans) != 1 || filepath.Base(orphans[0]) != "orphan.jsonl" {
		t.Fatalf("orphans = %v, want exactly [orphan.jsonl] — a torn tail after session_end must not orphan a clean run", orphans)
	}
}
