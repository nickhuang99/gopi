package tools

// Session-tree accounting regression for the brew sweep (epic: production
// readers glob *.jsonl and cannot tell a session RECORD from a side file).
//
// listSessionJSONLs chose its inputs with a bare "*.jsonl" suffix match, so a
// session dir's signal-provenance.jsonl (signalrec.FileName — the same
// session's provenance record) was swept as if it were a session: it has no
// conversation to slice, so the slicer would emit meaningless "lessons" while
// the real session was counted twice. The scan now goes through
// internal/sessionlayout.Records, the shared layout-keyed classification.

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/nickhuang99/gopi/internal/config"
	"github.com/nickhuang99/gopi/internal/sessionlayout"
	"github.com/nickhuang99/gopi/internal/signalrec"
)

// brewWriteFile writes a file with an mtime offset so the newest-first
// ordering is deterministic.
func brewWriteFile(t *testing.T, path, body string) string {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
	return path
}

// TestListSessionJSONLs_SkipsSideFile: one bare session dir holding
// session.jsonl + signal-provenance.jsonl yields exactly the record.
func TestListSessionJSONLs_SkipsSideFile(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "aaaa1111-2222-4333-8444-555555555555")
	record := brewWriteFile(t, filepath.Join(dir, sessionlayout.RecordName),
		`{"type":"session_start","sessionId":"aaaa1111-2222-4333-8444-555555555555"}`+"\n")
	brewWriteFile(t, filepath.Join(dir, signalrec.FileName), `{"kind":"signal"}`+"\n")

	got, err := listSessionJSONLs(root, 0)
	if err != nil {
		t.Fatalf("listSessionJSONLs: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("listSessionJSONLs = %d files, want 1 (a provenance side file is not a session to brew): %v", len(got), got)
	}
	if got[0] != record {
		t.Errorf("brewed %s, want the session record %s", got[0], record)
	}
}

// TestListSessionJSONLs_StillSeesEveryRecordLayout: the fix must not narrow
// the sweep — a flat record, a task-run record and a bare record all remain
// brewable, and a side-file-only dir contributes nothing.
func TestListSessionJSONLs_StillSeesEveryRecordLayout(t *testing.T) {
	root := t.TempDir()
	flat := brewWriteFile(t, filepath.Join(root, "bbbb1111-2222-4333-8444-555555555555.jsonl"), "{}\n")
	run := brewWriteFile(t, filepath.Join(root, "task-7", "cccc1111-2222-4333-8444-555555555555.jsonl"), "{}\n")
	bare := brewWriteFile(t, filepath.Join(root, "dddd1111-2222-4333-8444-555555555555", sessionlayout.RecordName), "{}\n")
	// A resident process's dir: provenance only, no session.
	brewWriteFile(t, filepath.Join(root, signalrec.ProcessDirPrefix+"4242", signalrec.FileName), "{}\n")

	got, err := listSessionJSONLs(root, 0)
	if err != nil {
		t.Fatalf("listSessionJSONLs: %v", err)
	}
	want := map[string]bool{flat: true, run: true, bare: true}
	if len(got) != len(want) {
		t.Fatalf("listSessionJSONLs = %v, want exactly %d records", got, len(want))
	}
	for _, p := range got {
		if !want[p] {
			t.Errorf("unexpected brew input %s", p)
		}
		delete(want, p)
	}
	for missing := range want {
		t.Errorf("record %s missing from the brew sweep", missing)
	}

	// The GOPI_HOME-driven default dir resolves to the same scan (the path
	// brew actually uses in production). GOPI_HOME is a HOME, not a data root:
	// pinning it to `<home>/.gopi` would name a PROFILE and resolve one level
	// deeper — the near miss config.LookupSandboxProfileMisuse refuses (and this
	// fixture's own error text is how that refusal first showed up here).
	home := t.TempDir()
	t.Setenv(config.GopiDirEnv, home)
	dir, err := brewSessionsDir()
	if err != nil {
		t.Fatalf("brewSessionsDir: %v", err)
	}
	if want := filepath.Join(home, config.ProfileDirName, config.SessionsDirName); dir != want {
		t.Errorf("brewSessionsDir = %s, want %s (the resolved profile's sessions dir)", dir, want)
	}
	if filepath.Base(dir) != config.SessionsDirName {
		t.Errorf("brewSessionsDir = %s, want a %s dir", dir, config.SessionsDirName)
	}
}
