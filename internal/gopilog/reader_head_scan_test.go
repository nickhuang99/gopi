package gopilog

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/nick/gopi/internal/config"
	"github.com/nick/gopi/internal/sessionlayout"
)

// Regression tests for the `gopi session log --last` listing path (2026-09-23).
//
// The command used to spend its wall time re-reading every session file's head
// once per FACT and once per COMPARISON, and each of those reads pulled a full
// 1 MiB off the disk because the scanner's initial buffer was the read window.
// On a 3.8k-session profile that is ~90k reads of 1 MiB each: the command
// printed nothing for minutes. One listing now scans each head exactly once
// (collectSessionFiles → scanSessionHead) and the scanner reads one page at a
// time.
//
// The tests below pin the OBSERVABLE contract that made that possible —
// ListSessionRefs yields the order, the display id and the activity flag from
// one pass, so no caller has a reason to re-scan — plus the read window itself,
// which is invisible in behaviour and therefore needs its own guard.

// writeSessionFile creates path (parent dirs included) with the given raw JSONL
// lines.
func writeHeadTestSession(t *testing.T, path string, lines ...string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatalf("mkdir %s: %v", filepath.Dir(path), err)
	}
	body := ""
	for _, l := range lines {
		body += l + "\n"
	}
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

// sessionStartLine renders a session_start event in the on-disk shape.
func sessionStartLine(id, ts string) string {
	return `{"type":"session_start","sessionId":"` + id + `","timestamp":"` + ts + `","model":"m","workdir":"/w"}`
}

// listingSandbox points the process at a fresh GOPI_HOME and returns the
// sessions root, so every listing call below resolves hermetically.
func listingSandbox(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	root := filepath.Join(home, ".gopi", config.SessionsDirName)
	if err := os.MkdirAll(root, 0o700); err != nil {
		t.Fatalf("mkdir %s: %v", root, err)
	}
	t.Setenv(config.GopiDirEnv, home)
	return root
}

// TestListSessionRefsOrdersByStartAndCarriesActivity: the ref carries the
// order key and the activity flag, so a caller needs no second scan.
func TestListSessionRefsOrdersByStartAndCarriesActivity(t *testing.T) {
	root := listingSandbox(t)

	// Deliberately anti-correlated with the UUID order: the OLDEST session has
	// the lexicographically LARGEST name, so an order that ignores timestamps
	// cannot accidentally pass.
	old := "ffffffff-0000-4000-8000-000000000001"
	mid := "aaaaaaaa-0000-4000-8000-000000000002"
	// A bare session_start and nothing else: the degenerate "did nothing" file.
	dead := "bbbbbbbb-0000-4000-8000-000000000003"

	writeHeadTestSession(t, filepath.Join(root, old+".jsonl"),
		sessionStartLine(old, "2026-09-01T10:00:00+08:00"),
		`{"type":"user","message":{"content":"hi"}}`)
	writeHeadTestSession(t, filepath.Join(root, mid+".jsonl"),
		sessionStartLine(mid, "2026-09-02T10:00:00+08:00"),
		`{"type":"assistant","message":{"content":"yo"}}`)
	writeHeadTestSession(t, filepath.Join(root, dead+".jsonl"),
		sessionStartLine(dead, "2026-09-03T10:00:00+08:00"))

	refs, err := ListSessionRefs()
	if err != nil {
		t.Fatalf("ListSessionRefs: %v", err)
	}
	if len(refs) != 3 {
		t.Fatalf("got %d refs, want 3: %+v", len(refs), refs)
	}
	// Newest first — by session_start, NOT by filename (dead's name sorts
	// before mid's, yet its start is later).
	wantOrder := []string{dead, mid, old}
	for i, want := range wantOrder {
		if refs[i].ID != want {
			t.Errorf("refs[%d].ID = %q, want %q (order: %+v)", i, refs[i].ID, want, ids(refs))
		}
	}
	// Activity rides the SAME ref: the bare session_start file is inactive.
	wantActive := map[string]bool{old: true, mid: true, dead: false}
	for _, r := range refs {
		if r.Active != wantActive[r.ID] {
			t.Errorf("ref %s Active = %v, want %v", r.ID, r.Active, wantActive[r.ID])
		}
	}
	// The legacy path-only listing must agree with the ref listing.
	files, err := ListSessionFiles()
	if err != nil {
		t.Fatalf("ListSessionFiles: %v", err)
	}
	if len(files) != len(refs) {
		t.Fatalf("ListSessionFiles returned %d paths, refs hold %d", len(files), len(refs))
	}
	for i := range files {
		if files[i] != refs[i].Path {
			t.Errorf("ListSessionFiles[%d] = %s, refs[%d] = %s (the two listings must agree)", i, files[i], i, refs[i].Path)
		}
	}
}

// TestListSessionRefsDeterministicOnTiedTimestamps: two sessions started in the
// same second must order the same way on every call. The previous
// comparison-time form was not a total order (a file whose session_start was
// unreadable fell back to a filename comparison inside a timestamp sort), so
// the order could vary between runs.
func TestListSessionRefsDeterministicOnTiedTimestamps(t *testing.T) {
	root := listingSandbox(t)
	ts := "2026-09-02T10:00:00+08:00"
	// Same timestamp, filenames in opposite order to the timestamps, plus one
	// file with NO session_start at all (the unreadable-head case).
	writeHeadTestSession(t, filepath.Join(root, "00000000-0000-4000-8000-000000000001.jsonl"),
		sessionStartLine("00000000-0000-4000-8000-000000000001", ts))
	writeHeadTestSession(t, filepath.Join(root, "00000000-0000-4000-8000-000000000002.jsonl"),
		sessionStartLine("00000000-0000-4000-8000-000000000002", ts))
	writeHeadTestSession(t, filepath.Join(root, "00000000-0000-4000-8000-000000000003.jsonl"),
		`{"type":"benchmark-note","detail":"no session_start, no activity"}`)

	first, err := ListSessionRefs()
	if err != nil {
		t.Fatalf("ListSessionRefs: %v", err)
	}
	for i := 0; i < 3; i++ {
		again, err := ListSessionRefs()
		if err != nil {
			t.Fatalf("ListSessionRefs (repeat %d): %v", i, err)
		}
		if len(again) != len(first) {
			t.Fatalf("repeat %d returned %d refs, first returned %d", i, len(again), len(first))
		}
		for j := range again {
			if again[j].Path != first[j].Path {
				t.Fatalf("repeat %d order differs at %d: %s vs %s", i, j, again[j].Path, first[j].Path)
			}
		}
	}
	// The no-session_start file must still be listed (a truncated head must not
	// make a session vanish) and must be flagged inactive.
	found := false
	for _, r := range first {
		if filepath.Base(r.Path) == "00000000-0000-4000-8000-000000000003.jsonl" {
			found = true
			if r.Active {
				t.Errorf("file with no session_start reported active")
			}
			if r.ID != "00000000-0000-4000-8000-000000000003" {
				t.Errorf("display id = %q, want the basename stem fallback", r.ID)
			}
		}
	}
	if !found {
		t.Errorf("the session_start-less file vanished from the listing: %+v", ids(first))
	}
}

// TestListSessionRefsBareLayoutIDComesFromEvent: in the bare layout the record
// file is literally session.jsonl, so the display id MUST come from the
// session_start event — the stem "session" is not a session id.
func TestListSessionRefsBareLayoutIDComesFromEvent(t *testing.T) {
	root := listingSandbox(t)
	const id = "7aa1e0f5-1111-4222-8333-444455556666"
	writeHeadTestSession(t, filepath.Join(root, id, sessionlayout.RecordName),
		sessionStartLine(id, "2026-09-04T09:00:00+08:00"),
		`{"type":"user","message":{"content":"bare"}}`)

	refs, err := ListSessionRefs()
	if err != nil {
		t.Fatalf("ListSessionRefs: %v", err)
	}
	if len(refs) != 1 {
		t.Fatalf("got %d refs, want 1", len(refs))
	}
	if refs[0].ID != id {
		t.Errorf("bare-layout display id = %q, want %q", refs[0].ID, id)
	}
	if !refs[0].Active {
		t.Errorf("bare-layout session with a user event reported inactive")
	}
}

// TestSessionStartNotFirstLineStillResolves: the head scan must not assume the
// session_start is line 1 — a leading unparsable or other-type line must not
// cost the session its id or its timestamp.
func TestSessionStartNotFirstLineStillResolves(t *testing.T) {
	root := listingSandbox(t)
	const id = "c0ffee00-2222-4333-8444-555566667777"
	writeHeadTestSession(t, filepath.Join(root, id+".jsonl"),
		`{"type":"benchmark-note","detail":"not a session event"}`,
		sessionStartLine(id, "2026-09-05T09:00:00+08:00"),
		`{"type":"user","message":{"content":"late start event"}}`)

	refs, err := ListSessionRefs()
	if err != nil {
		t.Fatalf("ListSessionRefs: %v", err)
	}
	if len(refs) != 1 {
		t.Fatalf("got %d refs, want 1", len(refs))
	}
	if refs[0].ID != id {
		t.Errorf("id = %q, want %q (the start event is not line 1 but still resolves)", refs[0].ID, id)
	}
	if !refs[0].Active {
		t.Errorf("session with a user event reported inactive")
	}
}

// TestScannerInitBufSizeStaysOnePage is the guard for the READ AMPLIFICATION
// half of the regression. It has no behavioural signature — a large initial
// buffer produces exactly the same output, only 47x more device reads — so it
// is pinned as a value, the way scratch_tree_pin_test.go pins a tree name.
//
// bufio fills its ENTIRE window on the first read of a file, so
// scannerInitBufSize IS the number of bytes pulled off disk to answer "what is
// this session's first line?" — for every session a listing walks. Measured on
// this host (3.8k session files, 4 KiB pages): 4 KiB = 59ms, 64 KiB = 204ms,
// 256 KiB = 774ms, 1 MiB = 2.75s.
func TestScannerInitBufSizeStaysOnePage(t *testing.T) {
	const maxWindow = 16 << 10
	if scannerInitBufSize > maxWindow {
		t.Errorf("scannerInitBufSize = %d bytes, want <= %d: bufio reads the whole window on the first Read, and a session listing opens thousands of files to read one short session_start line each — a larger window is a linear read amplification of `gopi session log --last` (1 MiB measured 47x the 4 KiB floor)", scannerInitBufSize, maxWindow)
	}
	if scannerInitBufSize <= 0 {
		t.Errorf("scannerInitBufSize = %d, want positive", scannerInitBufSize)
	}
}

// ids is a test-only helper for readable failure messages.
func ids(refs []SessionRef) []string {
	out := make([]string, 0, len(refs))
	for _, r := range refs {
		out = append(out, r.ID)
	}
	return out
}
