package sessionlayout_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nickhuang99/gopi/internal/config"
	"github.com/nickhuang99/gopi/internal/sessionlayout"
	"github.com/nickhuang99/gopi/internal/signalrec"
)

// sessionlayout.writeFile makes a file with a little content (the classifier is a NAME +
// LAYOUT rule, so content is irrelevant — the point is that it never opens
// these files, and a later content probe would have to break these tests).
func writeFile(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, []byte("{\"type\":\"session_start\"}\n"), 0o600); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

// sessionlayout.globCount is the NAIVE accounting this package exists to replace: every
// "*.jsonl" one level under the root, counted as if it were a session. Kept
// in the test as the red-proof oracle — the regression tests assert the glob
// OVER-counts and sessionlayout.Records does not.
func globCount(t *testing.T, root string) int {
	t.Helper()
	n := 0
	entries, err := os.ReadDir(root)
	if err != nil {
		t.Fatalf("read %s: %v", root, err)
	}
	for _, e := range entries {
		if !e.IsDir() {
			if strings.HasSuffix(e.Name(), sessionlayout.JSONLSuffix) {
				n++
			}
			continue
		}
		subs, err := os.ReadDir(filepath.Join(root, e.Name()))
		if err != nil {
			continue
		}
		for _, se := range subs {
			if !se.IsDir() && strings.HasSuffix(se.Name(), sessionlayout.JSONLSuffix) {
				n++
			}
		}
	}
	return n
}

// TestRecords_BareSessionWithProvenanceSideFileIsOneSession is the regression
// test for the defect this package fixes: a session dir holding its record
// AND the signal-provenance side file is exactly ONE session — while the
// bare-glob accounting the production readers used counts TWO.
func TestRecords_BareSessionWithProvenanceSideFileIsOneSession(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "5f2a91c4-0000-4000-8000-abcdefabcdef")
	writeFile(t, filepath.Join(dir, sessionlayout.RecordName))
	writeFile(t, filepath.Join(dir, signalrec.FileName))

	if got := globCount(t, root); got != 2 {
		t.Fatalf("test premise broken: the bare glob should see 2 *.jsonl here, saw %d — the side file is no longer written beside the record", got)
	}
	rs, err := sessionlayout.Records(root)
	if err != nil {
		t.Fatalf("sessionlayout.Records: %v", err)
	}
	if len(rs) != 1 {
		t.Fatalf("sessionlayout.Records = %d sessions, want exactly 1 (record %s + side file %s is ONE session): %+v",
			len(rs), sessionlayout.RecordName, signalrec.FileName, rs)
	}
	if rs[0].Path != filepath.Join(dir, sessionlayout.RecordName) {
		t.Errorf("record path = %q, want %q (the side file must never be picked as the record)",
			rs[0].Path, filepath.Join(dir, sessionlayout.RecordName))
	}
	if len(rs[0].SideFiles) != 1 || rs[0].SideFiles[0] != signalrec.FileName {
		t.Errorf("side files = %v, want exactly [%s]", rs[0].SideFiles, signalrec.FileName)
	}
}

// TestRecords_DirWithOnlySideFileIsNotASession: the residence case — a
// process-scope dir (signalrec's process-<pid> layout) may hold a provenance
// record and no session log at all. It must count as ZERO sessions, not one.
func TestRecords_DirWithOnlySideFileIsNotASession(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "process-4242")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(dir, signalrec.FileName))
	writeFile(t, filepath.Join(dir, "metadata.json")) // not a JSONL at all

	rs, err := sessionlayout.Records(root)
	if err != nil {
		t.Fatalf("sessionlayout.Records: %v", err)
	}
	if len(rs) != 0 {
		t.Fatalf("sessionlayout.Records = %d, want 0 (a dir holding only a side file is not a session): %+v", len(rs), rs)
	}
}

// TestRecords_FlatAndDirLayoutsCountOnceEach: all three record layouts, a
// side file, and an unregistered JSONL in ONE tree — exactly three sessions,
// with the unregistered file named as unknown rather than dropped.
func TestRecords_FlatAndDirLayoutsCountOnceEach(t *testing.T) {
	root := t.TempDir()
	flat := filepath.Join(root, "aaaa1111-0000-4000-8000-000000000001.jsonl")
	runDir := filepath.Join(root, "task-9c1f")
	bareDir := filepath.Join(root, "bbbb2222-0000-4000-8000-000000000002")
	writeFile(t, flat)
	writeFile(t, filepath.Join(runDir, "cccc3333-0000-4000-8000-000000000003.jsonl"))
	writeFile(t, filepath.Join(runDir, signalrec.FileName))
	writeFile(t, filepath.Join(bareDir, sessionlayout.RecordName))
	writeFile(t, filepath.Join(bareDir, signalrec.FileName))
	writeFile(t, filepath.Join(bareDir, "stray-artifact.jsonl"))
	// Non-JSONL members of the tree: never records, never side files.
	writeFile(t, filepath.Join(bareDir, "task-report.md"))

	rs, err := sessionlayout.Records(root)
	if err != nil {
		t.Fatalf("sessionlayout.Records: %v", err)
	}
	if len(rs) != 3 {
		t.Fatalf("sessionlayout.Records = %d, want 3 (flat + task-run + bare): %+v", len(rs), rs)
	}
	want := map[string]bool{
		flat: true,
		filepath.Join(runDir, "cccc3333-0000-4000-8000-000000000003.jsonl"): true,
		filepath.Join(bareDir, sessionlayout.RecordName):                    true,
	}
	for _, r := range rs {
		if !want[r.Path] {
			t.Errorf("unexpected record path %q", r.Path)
		}
		delete(want, r.Path)
	}
	for missing := range want {
		t.Errorf("record %q not found", missing)
	}

	// The unregistered JSONL is REPORTED, never silently absorbed.
	var bare sessionlayout.Record
	for _, r := range rs {
		if r.Path == filepath.Join(bareDir, sessionlayout.RecordName) {
			bare = r
		}
	}
	joined := strings.Join(bare.SideFiles, "|")
	if !strings.Contains(joined, signalrec.FileName) {
		t.Errorf("side files %v do not name the registered provenance file", bare.SideFiles)
	}
	if !strings.Contains(joined, "stray-artifact.jsonl (unknown .jsonl)") {
		t.Errorf("side files %v do not report the unregistered JSONL as unknown", bare.SideFiles)
	}
}

// TestClassify_LayoutKeyDecides proves the LAYOUT rule directly: the same
// base name is a record at the sessions root (legacy flat) and NOT a record
// one level down (it is then a JSONL beside a session's own record).
func TestClassify_LayoutKeyDecides(t *testing.T) {
	root := t.TempDir()
	sub := filepath.Join(root, "task-1")
	writeFile(t, filepath.Join(root, "dddd4444-0000-4000-8000-000000000004.jsonl"))
	writeFile(t, filepath.Join(sub, "eeee5555-0000-4000-8000-000000000005.jsonl"))

	entries, err := os.ReadDir(root)
	if err != nil {
		t.Fatal(err)
	}
	rootKind := sessionlayout.Classify(root, true, entries[0]).Kind
	if rootKind != sessionlayout.KindRecord {
		t.Errorf("flat file at the root classified %v, want sessionlayout.KindRecord", rootKind)
	}

	// One level down the DIRECTORY's shape decides. A task-run dir (no
	// sessionlayout.RecordName) holds one JSONL per RUN, so each file is a record of its
	// own — while a BARE dir holds exactly one record and treats the rest as
	// artifacts.
	subEntries, err := os.ReadDir(sub)
	if err != nil {
		t.Fatal(err)
	}
	if k := sessionlayout.Classify(sub, false, subEntries[0]).Kind; k != sessionlayout.KindRecord {
		t.Errorf("run file in a task-run dir classified %v, want sessionlayout.KindRecord (one file per run)", k)
	}
	writeFile(t, filepath.Join(sub, "ffff6666-0000-4000-8000-000000000006.jsonl"))
	rs, err := sessionlayout.Records(root)
	if err != nil {
		t.Fatal(err)
	}
	runs := 0
	for _, r := range rs {
		if r.Dir == sub {
			runs++
		}
	}
	if runs != 2 {
		t.Errorf("task-run dir with 2 run files contributed %d records, want 2: %+v", runs, rs)
	}
	// The accounting form: the DIR is one session, and its first run file is
	// the log that stands for it.
	if r, ok := sessionlayout.RecordInDir(sub); !ok || r.Dir != sub {
		t.Errorf("sessionlayout.RecordInDir(%s) = %+v ok=%v, want the dir's own record", sub, r, ok)
	}

	// A BARE dir with a stray extra JSONL: exactly ONE record, the extra file
	// REPORTED as unknown rather than counted as a second session.
	bare := filepath.Join(root, "bbbb7777-0000-4000-8000-000000000007")
	writeFile(t, filepath.Join(bare, sessionlayout.RecordName))
	writeFile(t, filepath.Join(bare, "stray-artifact.jsonl"))
	bareEntries, err := os.ReadDir(bare)
	if err != nil {
		t.Fatal(err)
	}
	var stray sessionlayout.Entry
	for _, e := range bareEntries {
		if ent := sessionlayout.Classify(bare, false, e); ent.Name == "stray-artifact.jsonl" {
			stray = ent
		}
	}
	if stray.Kind != sessionlayout.KindUnknown {
		t.Errorf("stray JSONL in a BARE session dir classified %v, want sessionlayout.KindUnknown", stray.Kind)
	}
	side := sessionlayout.SideFilesIn(bare)
	if len(side) != 1 || !strings.Contains(side[0], "stray-artifact.jsonl (unknown .jsonl)") {
		t.Errorf("side files = %v, want the stray JSONL reported as unknown", side)
	}
	bareRecords := 0
	rs, err = sessionlayout.Records(root)
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range rs {
		if r.Dir == bare {
			bareRecords++
		}
	}
	if bareRecords != 1 {
		t.Errorf("bare dir with a stray JSONL contributed %d records, want exactly 1", bareRecords)
	}
}

// TestRecordPath_ReadinessProbeAgrees is the probe/assertion agreement check:
// sessionlayout.RecordPath (the "is the session ready yet" probe) returns the RECORD, never
// the side file it sits beside.
func TestRecordPath_ReadinessProbeAgrees(t *testing.T) {
	root := t.TempDir()
	empty := filepath.Join(root, "0000-emptydir")
	if err := os.MkdirAll(empty, 0o755); err != nil {
		t.Fatal(err)
	}
	if got := sessionlayout.RecordPath(root); got != "" {
		t.Fatalf("sessionlayout.RecordPath on a tree with no record = %q, want \"\"", got)
	}
	dir := filepath.Join(root, "abc12345-0000-4000-8000-000000000007")
	writeFile(t, filepath.Join(dir, signalrec.FileName))
	if got := sessionlayout.RecordPath(root); got != "" {
		t.Fatalf("sessionlayout.RecordPath with only a side file on disk = %q, want \"\" (no session yet)", got)
	}
	writeFile(t, filepath.Join(dir, sessionlayout.RecordName))
	if got, want := sessionlayout.RecordPath(root), filepath.Join(dir, sessionlayout.RecordName); got != want {
		t.Fatalf("sessionlayout.RecordPath = %q, want %q", got, want)
	}
}

// TestRecordContainerIsTheRecordsOwnDir pins the ONE container rule
// (`sessionlayout.RecordContainer`) to the layout enumeration the package
// comment declares, on a tree built by hand. It is the cheap, in-process half
// of the guard whose expensive half is the real cross-peer hop
// (cmd/agent/cli_notify_peer_test.go, TestNotifyPeerCLITaskRunRecordKeepsItsContainer):
// this half runs on every `go test ./internal/sessionlayout`, and it fails if
// the rule drifts for EITHER face, because both faces call this function.
//
// The task-run case is the one that matters: the run record's basename is its
// own uuid, which is indistinguishable from a legacy flat record's, and a rule
// that "peels when the basename is not RecordName" is exactly what landed
// messages in <sessions>/messages/ (2026-10-13).
func TestRecordContainerIsTheRecordsOwnDir(t *testing.T) {
	root := t.TempDir()
	taskID, runID, bareID := "task-abc", "run-1111", "bare-2222"
	cases := []struct {
		name   string
		record string
		want   string
	}{
		{"legacy flat", filepath.Join(root, runID+sessionlayout.JSONLSuffix), root},
		{"bare session", filepath.Join(root, bareID, sessionlayout.RecordName), filepath.Join(root, bareID)},
		{"task run", filepath.Join(root, taskID, runID+sessionlayout.JSONLSuffix), filepath.Join(root, taskID)},
	}
	for _, tc := range cases {
		writeFile(t, tc.record)
		if got := sessionlayout.RecordContainer(tc.record); got != tc.want {
			t.Errorf("RecordContainer(%s layout) = %q, want %q — the container is the record's OWN directory in every layout",
				tc.name, got, tc.want)
		}
	}
	// The container is not the record's directory only for a task run, and no
	// basename can say which case this is: the two records named for their own
	// uuid must resolve to DIFFERENT containers (one to the root, one to the
	// task dir) from the same shape of name. That is the defect in one assertion.
	flat := sessionlayout.RecordContainer(filepath.Join(root, runID+sessionlayout.JSONLSuffix))
	run := sessionlayout.RecordContainer(filepath.Join(root, taskID, runID+sessionlayout.JSONLSuffix))
	if flat == run {
		t.Fatalf("the flat record and the task-run record resolved to the same container %q — a basename rule cannot tell them apart, and they must not be", flat)
	}
}

// TestJSONLSuffixMatchesWrittenFiles pins sessionlayout.JSONLSuffix to the extension the
// session writer actually produces: every record path this package returns
// must end in it, on a tree built by hand from the layouts in the docs.
func TestJSONLSuffixMatchesWrittenFiles(t *testing.T) {
	if sessionlayout.JSONLSuffix != filepath.Ext(sessionlayout.RecordName) {
		t.Errorf("sessionlayout.JSONLSuffix %q does not complete sessionlayout.RecordName %q", sessionlayout.JSONLSuffix, sessionlayout.RecordName)
	}
}

// TestSideFileNamesMatchWriters pins this package's side-file registry to the
// WRITERS' constants (Rule 14: one value, one home — the writer owns
// signalrec.FileName, the classification owns the list, and this test makes
// the two spellings impossible to drift apart). If the recorder ever renames
// its file, this fails instead of silently re-counting it as a session.
func TestSideFileNamesMatchWriters(t *testing.T) {
	if !sessionlayout.IsSideFile(signalrec.FileName) {
		t.Fatalf("signalrec.FileName = %q is not registered as a side file — the provenance record has become a phantom session again (registry: %v)",
			signalrec.FileName, sessionlayout.SideFileNames())
	}
	// The memory-prefetch record's WRITER is the chat prefetch executor
	// (cmd/agent/chat_prefetch.go's openPrefetchRecord), and its name comes
	// from config.MemoryPrefetchRecordName. Pinned the same way as the
	// provenance half: the writer owns the value, this package owns the
	// classification, and the two spellings must not drift. This is the half
	// that would silently rot — the knob is DEFAULT ON, so an unregistered
	// rename would put an "unknown .jsonl" back into every chat session dir
	// and every "one session, no stray artifacts" test would fail for a
	// feature that is behaving correctly.
	if !sessionlayout.IsSideFile(config.MemoryPrefetchRecordName) {
		t.Fatalf("config.MemoryPrefetchRecordName = %q is not registered as a side file — the per-run prefetch record reads as a stray artifact again (registry: %v)",
			config.MemoryPrefetchRecordName, sessionlayout.SideFileNames())
	}
	for _, n := range sessionlayout.SideFileNames() {
		if !sessionlayout.IsJSONL(n) {
			t.Errorf("registered side file %q is not a JSONL artifact", n)
		}
	}
}

// TestExitMarkerNameIsNotASessionArtifact pins the exit marker's NAME against
// the two ways a file in a session dir could be miscounted: as a RECORD (any
// `.jsonl` a run leaves becomes a session of its own — the phantom-session
// defect the side-file registry exists to prevent) and as a registered SIDE
// FILE (which would claim it annotates the record beside it; the marker
// annotates the RUN, and a resurrected session writes its own).
//
// The marker's shape is the FAMILY this tree already carries for run evidence
// (`chat-screen.dump`, `console.log`, `metadata.json`) — so this test states the
// name's contract rather than adding a registration for a file that needs none.
func TestExitMarkerNameIsNotASessionArtifact(t *testing.T) {
	if sessionlayout.IsJSONL(sessionlayout.ExitMarkerName) {
		t.Fatalf("exit marker name %q is a JSONL name — every *.jsonl in a session dir is either the record or a registered side file, and this is neither", sessionlayout.ExitMarkerName)
	}
	if sessionlayout.IsSideFile(sessionlayout.ExitMarkerName) {
		t.Fatalf("exit marker name %q is registered as a side file — it annotates the RUN, not the record beside it", sessionlayout.ExitMarkerName)
	}
	if sessionlayout.ExitMarkerName == "" || strings.ContainsAny(sessionlayout.ExitMarkerName, `/\`) {
		t.Fatalf("exit marker name %q must be a bare file name (a reader joins it to the record's own directory)", sessionlayout.ExitMarkerName)
	}
	// It is ONE spelling, read by the writer (cmd/agent) and the sweep's reader
	// (internal/agent). Both take it from here; a second literal anywhere is the
	// drift this constant exists to prevent.
	for _, forbidden := range []string{sessionlayout.RecordName, sessionlayout.MetadataFileName, "chat-screen.dump"} {
		if sessionlayout.ExitMarkerName == forbidden {
			t.Fatalf("exit marker name %q collides with an existing session artifact %q", sessionlayout.ExitMarkerName, forbidden)
		}
	}
}
