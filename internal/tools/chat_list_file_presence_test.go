package tools

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// seedChatRowWithLog writes ONE chat-index row whose log_path the caller
// chooses — "" records none — through the same START hook the live surfaces
// call, so the presence filter is exercised against rows the real writers
// produce rather than against hand-built table inserts.
func seedChatRowWithLog(t *testing.T, sessionID, logPath string) {
	t.Helper()
	if _, err := ChatEnsureRow(ChatSession{
		SessionID: sessionID, Mode: ChatModeCLI, Model: "glm-5.3-flash",
		FirstMsg: "presence sample " + sessionID, Workdir: "/tmp/proj", LogPath: logPath,
	}); err != nil {
		t.Fatalf("seed %s: %v", sessionID, err)
	}
}

// TestChatListFilePresenceHidesMissingByDefault is the ruled behaviour's
// small-sample guard: a row whose log is not on this machine is DROPPED from
// the local-only view, the view SAYS how many it dropped and names the flag that
// reveals them, and the row that IS readable is untouched.
func TestChatListFilePresenceHidesMissingByDefault(t *testing.T) {
	chatTestEnv(t)
	presentID := "chat-presence-11111111-aaaa"
	missingID := "chat-presence-22222222-bbbb"
	// A real file this machine holds, and a path that names nothing.
	have := writeChatSessionJSONL(t, presentID, "hello from a live session")
	void := filepath.Join(t.TempDir(), "not-here.jsonl")
	seedChatRowWithLog(t, presentID, have)
	seedChatRowWithLog(t, missingID, void)

	got, err := ChatList(ChatListFilter{Mode: ChatModeCLI, LocalOnly: true})
	if err != nil {
		t.Fatalf("ChatList: %v", err)
	}
	if !strings.Contains(got, presentID) {
		t.Errorf("a row whose log IS on this machine was dropped from the local-only view:\n%s", got)
	}
	if strings.Contains(got, missingID) {
		t.Errorf("a row whose log is NOT on this machine survived the local-only view:\n%s", got)
	}
	// Rule 15: the drop is disclosed, in numbers, and the disclosure names the
	// flag that reveals the rows (a disclosure that named a dead end would be
	// worse than none).
	note := ChatFileHiddenNote(1)
	if !strings.Contains(got, note) {
		t.Errorf("the local-only view hid a row without disclosing it (want %q):\n%s", note, got)
	}
	if !strings.Contains(note, ChatListAllFlag) {
		t.Errorf("the disclosure does not name the revealing flag %q: %q", ChatListAllFlag, note)
	}
	// A view that hid nothing must not carry the line (the zero case is a
	// claim too, and it must not be made).
	if ChatFileHiddenNote(0) != "" {
		t.Errorf("ChatFileHiddenNote(0) = %q, want the empty string", ChatFileHiddenNote(0))
	}
	if !strings.Contains(got, presentID) {
		t.Errorf("the readable row is missing from the local-only render:\n%s", got)
	}
	// The reader twin must have dropped the SAME row: two faces, one filter.
	rows, err := ChatSessionRows(ChatListFilter{Mode: ChatModeCLI, LocalOnly: true})
	if err != nil {
		t.Fatalf("ChatSessionRows: %v", err)
	}
	for _, r := range rows {
		if r.SessionID == missingID {
			t.Errorf("the JSON face kept a row the local-only table dropped: %+v", r)
		}
		if !r.FilePresent {
			t.Errorf("row %s is in the local-only view but reports file_present=false", r.SessionID)
		}
	}
}

// TestChatListAllShowsMissingWithMarker is the other half of the ruling: the
// complete index costs one explicit flag, the re-admitted row is MARKED, and a
// view that hid nothing carries no disclosure line.
func TestChatListAllShowsMissingWithMarker(t *testing.T) {
	chatTestEnv(t)
	presentID := "chat-allview-11111111-aaaa"
	missingID := "chat-allview-22222222-bbbb"
	have := writeChatSessionJSONL(t, presentID, "live session")
	void := filepath.Join(t.TempDir(), "gone.jsonl")
	seedChatRowWithLog(t, presentID, have)
	seedChatRowWithLog(t, missingID, void)

	got, err := ChatList(ChatListFilter{Mode: ChatModeCLI, LocalOnly: false})
	if err != nil {
		t.Fatalf("ChatList(--all): %v", err)
	}
	if !strings.Contains(got, presentID) || !strings.Contains(got, missingID) {
		t.Fatalf("the complete index dropped a row:\n%s", got)
	}
	// The marker rides the MISSING row and only that row: a present row
	// carrying it would make the marker say nothing.
	for _, line := range strings.Split(got, "\n") {
		switch {
		case strings.Contains(line, missingID):
			if !strings.Contains(line, ChatFileMissingPrefix) {
				t.Errorf("the absent row is shown without its marker: %q", line)
			}
		case strings.Contains(line, presentID):
			if strings.Contains(line, ChatFileMissingPrefix) {
				t.Errorf("a readable row carries the absent marker: %q", line)
			}
		}
	}
	if strings.Contains(got, ChatFileHiddenNote(1)) {
		t.Errorf("a view that hid nothing carries a disclosure line:\n%s", got)
	}
	// The JSON face carries the same fact as a field, so a script does not have
	// to parse a bracketed marker out of a human table.
	rows, err := ChatSessionRows(ChatListFilter{Mode: ChatModeCLI, LocalOnly: false})
	if err != nil {
		t.Fatalf("ChatSessionRows(--all): %v", err)
	}
	seen := map[string]bool{}
	for _, r := range rows {
		seen[r.SessionID] = true
		want := r.SessionID == presentID
		if r.FilePresent != want {
			t.Errorf("row %s: file_present=%v, want %v (measured, not assumed)", r.SessionID, r.FilePresent, want)
		}
	}
	if !seen[presentID] || !seen[missingID] {
		t.Errorf("the JSON face lost a row the table showed: %v", seen)
	}
}

// TestChatPresenceFindsTheRunDirLayout is the regression guard for the defect
// this filter shipped with (found by measuring the live profile, 2026-10-05):
// a row whose recorded log_path is stale — a session written under the
// run-dir layout, `sessions/<id>/<id>.jsonl` — is READABLE here and must not be
// reported absent. The first implementation required the row's log_path AND a
// flat `<id>.jsonl`, so it reported 85 of 248 live rows absent while 69 of them
// were readable right there.
func TestChatPresenceFindsTheRunDirLayout(t *testing.T) {
	chatTestEnv(t)
	runDirID := "chat-rundir-33333333-cccc"
	dir, err := chatSessionsDir()
	if err != nil {
		t.Fatalf("sessions dir: %v", err)
	}
	// The run-dir layout: the log sits INSIDE the session's own directory,
	// under the session's own name — which is the address the flat-layout-only
	// probe missed.
	sessionDir := filepath.Join(dir, runDirID)
	if err := os.MkdirAll(sessionDir, 0o755); err != nil {
		t.Fatalf("mkdir session dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(sessionDir, runDirID+taskSessionJSONLExt), []byte("{}\n"), 0o644); err != nil {
		t.Fatalf("write run-dir log: %v", err)
	}
	// The row's OWN log_path is stale, as it is for exactly this family.
	seedChatRowWithLog(t, runDirID, filepath.Join(t.TempDir(), "stale-of-another-host.jsonl"))

	got, err := ChatList(ChatListFilter{Mode: ChatModeCLI, LocalOnly: true})
	if err != nil {
		t.Fatalf("ChatList: %v", err)
	}
	if !strings.Contains(got, runDirID) {
		t.Errorf("a session readable at sessions/<id>/<id>.jsonl was reported absent (the measured defect):\n%s", got)
	}
	rows, err := ChatSessionRows(ChatListFilter{Mode: ChatModeCLI, LocalOnly: true})
	if err != nil {
		t.Fatalf("ChatSessionRows: %v", err)
	}
	for _, r := range rows {
		if r.SessionID == runDirID && !r.FilePresent {
			t.Errorf("the run-dir layout did not count as present: %+v", r)
		}
	}
}

// TestChatPresenceIsMeasuredInBothViews: `--all` DROPS nothing, but it must not
// report every row present to save work — the marker and the file_present field
// are claims about this machine, and a claim nobody measured is worse than an
// absent field (Rule 15).
func TestChatPresenceIsMeasuredInBothViews(t *testing.T) {
	chatTestEnv(t)
	void := filepath.Join(t.TempDir(), "absent.jsonl")
	id := "chat-measured-44444444-dddd"
	seedChatRowWithLog(t, id, void)

	for _, tc := range []struct {
		localOnly bool
		wantRows  int
	}{{localOnly: true, wantRows: 0}, {localOnly: false, wantRows: 1}} {
		rows, err := ChatSessionRows(ChatListFilter{Mode: ChatModeCLI, LocalOnly: tc.localOnly})
		if err != nil {
			t.Fatalf("ChatSessionRows(localOnly=%v): %v", tc.localOnly, err)
		}
		if len(rows) != tc.wantRows {
			t.Fatalf("localOnly=%v: got %d rows, want %d", tc.localOnly, len(rows), tc.wantRows)
		}
		// The row --all re-admits is MEASURED, not assumed present: that is the
		// whole difference between a marker that means something and one that
		// decorates every row.
		for _, r := range rows {
			if r.FilePresent {
				t.Errorf("localOnly=%v: a row with no log on this machine reports file_present=true", tc.localOnly)
			}
		}
	}
}
