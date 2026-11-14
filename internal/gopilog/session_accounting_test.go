package gopilog

// Session-tree accounting regression (epic: production readers glob *.jsonl
// and cannot tell a session RECORD from a side file). A session DIRECTORY
// legitimately holds signal-provenance.jsonl (signalrec.FileName — one JSON
// record per caught termination signal for THAT session) beside its
// session.jsonl. A reader that globs "*.jsonl" and treats every hit as a
// session reports ONE session as TWO: `gopi session log --last` shows a phantom row,
// `gopi session log <id>` can resolve to the provenance file, and LoadAllSummaries
// emits an empty summary for it.
//
// These tests pin the fix on the READER side: gopilog classifies by layout
// (internal/sessionlayout) and never counts, parses or returns a side file as
// a session. The old accounting is kept in the test as a red-proof oracle so
// the contrast is executable rather than a comment.

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/nickhuang99/gopi/internal/sessionlayout"
	"github.com/nickhuang99/gopi/internal/signalrec"
)

// writeSessionFile writes a minimal session record (one session_start line)
// with a distinct sessionId, so a reader can be asked what it found.
func writeSessionFile(t *testing.T, path, sessionID string) string {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", filepath.Dir(path), err)
	}
	line := `{"type":"session_start","sessionId":"` + sessionID + `","model":"m","timestamp":"2026-08-06T09:00:00+08:00"}` + "\n" +
		`{"type":"user","message":{"role":"user","content":"hi"}}` + "\n"
	if err := os.WriteFile(path, []byte(line), 0o600); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
	return path
}

// writeSideFile writes a signal-provenance record (one JSON object per line),
// the artifact the SIGTERM path files beside a session's own record.
func writeSideFile(t *testing.T, dir string) string {
	t.Helper()
	p := filepath.Join(dir, signalrec.FileName)
	if err := os.WriteFile(p, []byte(`{"kind":"signal","signal":"SIGTERM"}`+"\n"), 0o600); err != nil {
		t.Fatalf("write %s: %v", p, err)
	}
	return p
}

// legacyJSONLGlob replicates the PRE-FIX reader accounting: every *.jsonl one
// level under the sessions root, counted as a session. It exists so the
// regression is pinned by an executable contrast — if a reader ever goes back
// to globbing, this oracle says what it will report (2 for one session).
func legacyJSONLGlob(t *testing.T, root string) []string {
	t.Helper()
	var out []string
	entries, err := os.ReadDir(root)
	if err != nil {
		t.Fatalf("read %s: %v", root, err)
	}
	for _, e := range entries {
		if !e.IsDir() {
			if sessionlayout.IsJSONL(e.Name()) {
				out = append(out, filepath.Join(root, e.Name()))
			}
			continue
		}
		subs, err := os.ReadDir(filepath.Join(root, e.Name()))
		if err != nil {
			continue
		}
		for _, se := range subs {
			if !se.IsDir() && sessionlayout.IsJSONL(se.Name()) {
				out = append(out, filepath.Join(root, e.Name(), se.Name()))
			}
		}
	}
	return out
}

// TestCollectSessionFiles_SideFileIsNotASession is the reader-side regression:
// one bare session dir holding session.jsonl + signal-provenance.jsonl must
// yield exactly ONE session ref, and it must be the RECORD.
func TestCollectSessionFiles_SideFileIsNotASession(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "5f2a91c4-1111-4111-8111-abcdefabcdef")
	record := writeSessionFile(t, filepath.Join(dir, sessionlayout.RecordName), "5f2a91c4-1111-4111-8111-abcdefabcdef")
	side := writeSideFile(t, dir)

	if got := legacyJSONLGlob(t, root); len(got) != 2 {
		t.Fatalf("test premise broken: the old glob saw %d files, want 2 (%v) — the side file must still be written beside the record", len(got), got)
	}

	refs, err := collectSessionFiles(root)
	if err != nil {
		t.Fatalf("collectSessionFiles: %v", err)
	}
	if len(refs) != 1 {
		t.Fatalf("collectSessionFiles = %d sessions, want exactly 1 — %s is a side file of %s: %+v",
			len(refs), filepath.Base(side), filepath.Base(record), refs)
	}
	if refs[0].path != record {
		t.Errorf("session path = %s, want the record %s (never the provenance file)", refs[0].path, record)
	}
	if refs[0].id != "5f2a91c4-1111-4111-8111-abcdefabcdef" {
		t.Errorf("session id = %q, want the record's session_start sessionId", refs[0].id)
	}

	// The id-based lookup must also resolve to the record, not the side file —
	// by the FULL id (a truncated one is refused, epic full-id-no-prefix).
	const recordID = "5f2a91c4-1111-4111-8111-abcdefabcdef"
	got, err := FindSession(root, recordID)
	if err != nil {
		t.Fatalf("FindSession: %v", err)
	}
	if got != record {
		t.Errorf("FindSession = %s, want %s", got, record)
	}
	// And the side file's own basename stem must not be resolvable as a
	// session id at all (it is not a session).
	if p, err := FindSession(root, "signal-provenance"); err != nil || p != "" {
		t.Errorf("FindSession(signal-provenance) = (%q, %v), want (\"\", nil)", p, err)
	}
}

// TestListSessionFiles_IgnoresSideFile drives the PUBLIC reader the CLI uses
// (`gopi session log --last`): one session, listed once.
func TestListSessionFiles_IgnoresSideFile(t *testing.T) {
	home := t.TempDir()
	t.Setenv("GOPI_HOME", home)
	root := filepath.Join(home, ".gopi", sessionsDir)
	dir := filepath.Join(root, "aaaa1111-2222-4333-8444-555555555555")
	writeSessionFile(t, filepath.Join(dir, sessionlayout.RecordName), "aaaa1111-2222-4333-8444-555555555555")
	writeSideFile(t, dir)

	files, err := ListSessionFiles()
	if err != nil {
		t.Fatalf("ListSessionFiles: %v", err)
	}
	if len(files) != 1 {
		t.Fatalf("ListSessionFiles = %d files, want 1 (a side file is not a session): %v", len(files), files)
	}
	if filepath.Base(files[0]) != sessionlayout.RecordName {
		t.Errorf("listed %s, want the session record", files[0])
	}

	summaries, err := LoadAllSummaries(0)
	if err != nil {
		t.Fatalf("LoadAllSummaries: %v", err)
	}
	if len(summaries) != 1 || summaries[0].SessionID != "aaaa1111-2222-4333-8444-555555555555" {
		t.Fatalf("LoadAllSummaries = %+v, want exactly the one real session", summaries)
	}
}

// TestSessionsRecordingRawLog_IgnoresSideFile: the shared-raw-log scan reads
// each candidate's session_start head. Feeding it a provenance record is
// pointless (no session_start) — and, worse, a raw-log path could be matched
// by a side file that happens to quote one. The record-only candidate set
// removes the case.
func TestSessionsRecordingRawLog_IgnoresSideFile(t *testing.T) {
	home := t.TempDir()
	t.Setenv("GOPI_HOME", home)
	root := filepath.Join(home, ".gopi", sessionsDir)
	dir := filepath.Join(root, "bbbb1111-2222-4333-8444-555555555555")
	rawLog := filepath.Join(home, "gopi-raw-20260806-090000.log")
	p := writeSessionFile(t, filepath.Join(dir, sessionlayout.RecordName), "bbbb1111-2222-4333-8444-555555555555")
	// The record's session_start names the raw log (rewritten with the field).
	if err := os.WriteFile(p, []byte(`{"type":"session_start","sessionId":"bbbb1111-2222-4333-8444-555555555555","rawLogPath":"`+rawLog+`"}`+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	// The provenance record ALSO quotes the same raw log path (a signal record
	// carries the run's context, including its log paths).
	if err := os.WriteFile(filepath.Join(dir, signalrec.FileName),
		[]byte(`{"kind":"signal","rawLogPath":"`+rawLog+`"}`+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	ids := SessionsRecordingRawLog(rawLog)
	if len(ids) != 1 || ids[0] != "bbbb1111-2222-4333-8444-555555555555" {
		t.Fatalf("SessionsRecordingRawLog = %v, want exactly [bbbb1111-2222-4333-8444-555555555555] (the side file must not be scanned as a session)", ids)
	}
}
