// index_layout_test.go — the two raw-log layouts, and a zero result that
// explains itself.
//
// THE DEFECT (session-dir-unify S1, 3a816775). The raw HTTP log used to land
// flat in <gopi dir>/raw (gopi-raw-YYYYMMDD-HHMMSS.log) and moved to co-locate
// with the run that wrote it (<sessions>/<run-dir>/<run-uuid>.raw.log). The
// indexer kept its single-directory, "gopi-raw-" prefix filter, so on this
// machine 1265 logs / ~41 GB under ~/.gopi/sessions were indexed as ZERO —
// `gopi session checkpoint list` answered "no raw logs found" while the evidence sat
// one directory down.
//
// WHAT IS PINNED HERE, both directions:
//   - the NEW layout indexes (a run directory's <run-uuid>.raw.log, a task
//     run's, and the flat file a run directory may also hold),
//   - the OLD layout does not regress (a flat raw dir still lists exactly what
//     it listed before, and nothing else),
//   - the descent is BOUNDED (a private profile nested under a run directory's
//     evidence tree is not walked — the whole point of keeping the sweep
//     shallow),
//   - a zero result carries the SCAN REPORT: the directories listed and the
//     criteria applied, so "not found" is diagnosable.
package checkpoint

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nick/gopi/internal/config"
	"github.com/nick/gopi/internal/scratch"
)

// sessionsRootDir builds <root>/sessions and returns it: the directory whose
// OWN NAME is what makes IndexTree descend (config.SessionsDirName).
func sessionsRootDir(t *testing.T) string {
	t.Helper()
	root := filepath.Join(t.TempDir(), config.SessionsDirName)
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatalf("mkdir sessions root: %v", err)
	}
	return root
}

// runDirRawLog writes a raw log named exactly as the WRITER names it — from the
// writer's own constants, never a re-spelled literal (Rules 1/13/14).
func runDirRawLog(t *testing.T, dir, name string) string {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", dir, err)
	}
	return writeRawLog(t, dir, name, [][2]string{
		{"→ REQUEST\n", `{"model":"m-run","max_tokens":7,"messages":[{"role":"user","content":[{"type":"text","text":"co-located"}]}]}`},
	})
}

// TestIndexTree_SessionDirLayout pins the co-located layout: both the bare-run
// shape (<sessions>/<run-uuid>/<run-uuid>.raw.log) and the task-run shape
// (<sessions>/<task-id>/<run-uuid>.raw.log) are indexed from the sessions root.
func TestIndexTree_SessionDirLayout(t *testing.T) {
	root := sessionsRootDir(t)
	const (
		runUUID = "01a0ed99-0000-4000-8000-000000000001"
		taskID  = "t-fixture-task"
	)
	bare := runDirRawLog(t, filepath.Join(root, runUUID), runUUID+scratch.SessionRawLogFileSuffix)
	task := runDirRawLog(t, filepath.Join(root, taskID), "01a0ed99-0000-4000-8000-000000000002"+scratch.SessionRawLogFileSuffix)

	idxs, rep, err := IndexTree(root)
	if err != nil {
		t.Fatalf("IndexTree: %v", err)
	}
	got := map[string]bool{}
	for _, idx := range idxs {
		got[idx.Path] = true
		if idx.LastModel != "m-run" {
			t.Errorf("%s: expected the parsed last model, got %q", idx.Path, idx.LastModel)
		}
	}
	for _, want := range []string{bare, task} {
		if !got[want] {
			t.Errorf("co-located raw log %s was NOT indexed (indexed: %v)", want, keysOf(got))
		}
	}
	// The report must have listed the sessions root AND each run directory: a
	// scan that cannot say where it looked cannot explain a zero result.
	if rep.ScanRoot != root {
		t.Errorf("report ScanRoot = %q, want %q", rep.ScanRoot, root)
	}
	for _, want := range []string{root, filepath.Dir(bare), filepath.Dir(task)} {
		if !reportListed(rep, want) {
			t.Errorf("scan report does not list %s — it lists: %v", want, reportPaths(rep))
		}
	}
	if rep.Files == 0 {
		t.Errorf("report counted no files at all")
	}
}

// TestIndexTree_LegacyFlatLayoutStillIndexes is the REGRESSION guard: a flat raw
// directory (no sessions root name, no subdirectories) lists exactly the
// gopi-raw-*.log files it held before this change, and refuses the rest.
func TestIndexTree_LegacyFlatLayoutStillIndexes(t *testing.T) {
	dir := t.TempDir()
	log1 := writeRawLog(t, dir, scratch.RawLogNamePrefix+"20260826-120000"+scratch.RawLogFileSuffix, [][2]string{
		{"→ REQUEST\n", `{"model":"m-flat","max_tokens":1,"messages":[]}`},
	})
	// A human's own dump in the same directory carries the same suffix and must
	// never be indexed (the flat tree's namespace predicate, Rules 1/13).
	human := filepath.Join(dir, "fmt-detail.log")
	if err := os.WriteFile(human, []byte("not a gopi session raw log\n"), 0o644); err != nil {
		t.Fatalf("write human dump: %v", err)
	}
	idxs, rep, err := IndexTree(dir)
	if err != nil {
		t.Fatalf("IndexTree: %v", err)
	}
	if len(idxs) != 1 || idxs[0].Path != log1 {
		t.Fatalf("flat dir indexed %v, want exactly [%s]", pathsOf(idxs), log1)
	}
	if rep.Skipped < 1 {
		t.Errorf("the human dump must be REPORTED as a skip, got Skipped=%d", rep.Skipped)
	}
}

// TestIndexTree_DescentIsBounded pins the SECOND half of the defect's fix: a
// shallow, bounded walk. A run directory holds a whole private profile under its
// evidence tree on this host; walking it would re-read a task's own scratch (the
// #1313 hang shape) and would index foreign logs as if they were this
// profile's.
func TestIndexTree_DescentIsBounded(t *testing.T) {
	root := sessionsRootDir(t)
	outer := "01a0ed99-0000-4000-8000-00000000000a"
	deepRun := runDirRawLog(t,
		filepath.Join(root, outer, "evidence", "probe-profile", ".gopi", config.SessionsDirName, "inner-run"),
		"inner-run"+scratch.SessionRawLogFileSuffix)
	// A LEGITIMATE co-located log beside it, at the allowed depth.
	shallow := runDirRawLog(t, filepath.Join(root, outer), outer+scratch.SessionRawLogFileSuffix)

	idxs, _, err := IndexTree(root)
	if err != nil {
		t.Fatalf("IndexTree: %v", err)
	}
	for _, idx := range idxs {
		if idx.Path == deepRun {
			t.Errorf("the descent walked a nested private profile (%s) — the bound is %d level(s)",
				deepRun, sessionScanDepth)
		}
	}
	found := false
	for _, idx := range idxs {
		found = found || idx.Path == shallow
	}
	if !found {
		t.Errorf("the co-located log beside the evidence tree was NOT indexed (%s)", shallow)
	}
}

// TestScanReport_ZeroResultExplainsItself pins the diagnosis surface: a scan
// that finds nothing still reports the directory it listed and the exact
// acceptance rule, and names the entries it refused. Without this, "no raw logs
// found" is indistinguishable from a wrong directory or an outgrown filename
// rule (Rule 15).
func TestScanReport_ZeroResultExplainsItself(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "notes.txt"), []byte("x\n"), 0o644); err != nil {
		t.Fatalf("write notes: %v", err)
	}
	idxs, rep, err := IndexTree(dir)
	if err != nil {
		t.Fatalf("IndexTree: %v", err)
	}
	if len(idxs) != 0 {
		t.Fatalf("expected a zero-result scan, got %v", pathsOf(idxs))
	}
	lines := rep.Describe()
	if len(lines) == 0 {
		t.Fatal("Describe returned nothing — a zero result would print a bare 'not found'")
	}
	joined := strings.Join(lines, "\n")
	if !strings.Contains(joined, dir) {
		t.Errorf("the report does not name the directory it looked in:\n%s", joined)
	}
	for _, want := range []string{scratch.RawLogNamePrefix, scratch.SessionRawLogFileSuffix, scratch.RawLogFileSuffix} {
		if !strings.Contains(joined, want) {
			t.Errorf("the report does not carry the acceptance rule %q:\n%s", want, joined)
		}
	}
	if !strings.Contains(joined, "criteria") {
		t.Errorf("the report does not name its criteria section:\n%s", joined)
	}
	// The refusal is named too, so a wrong-shaped filename is visible.
	if !strings.Contains(joined, "notes.txt") {
		t.Errorf("the refused entry is not reported:\n%s", joined)
	}
	// The rule printed by the CLI and the rule the filter applies come from ONE
	// constant — assert the report's own entry point says the same thing.
	if !strings.Contains(rep.Criteria(), scratch.SessionRawLogFileSuffix) {
		t.Errorf("Criteria() and the filter have drifted: %q", rep.Criteria())
	}
}

// TestIndexDir_MissingDirIsAnError pins that the single-directory face still
// errors on an unreadable directory (its caller's own argument) rather than
// reporting a tidy empty result.
func TestIndexDir_MissingDirIsAnError(t *testing.T) {
	_, err := IndexDir(filepath.Join(t.TempDir(), "does-not-exist"))
	if err == nil {
		t.Fatal("IndexDir on a missing directory must return an error")
	}
}

// TestIsRawLogName pins the ONE name predicate both layouts are judged by.
func TestIsRawLogName(t *testing.T) {
	cases := map[string]bool{
		"gopi-raw-20260826-120000" + scratch.RawLogFileSuffix:                    true,
		"01a0ed99-0000-4000-8000-000000000001" + scratch.SessionRawLogFileSuffix: true,
		"raw" + scratch.SessionRawLogFileSuffix:                                  true,
		"fmt-detail" + scratch.RawLogFileSuffix:                                  false,
		"session.jsonl":                                                          false,
		"gopi-raw-20260826-120000.txt":                                           false,
	}
	for name, want := range cases {
		if got := IsRawLogName(name); got != want {
			t.Errorf("IsRawLogName(%q) = %v, want %v", name, got, want)
		}
	}
}

// --- small helpers -----------------------------------------------------------

func reportListed(rep ScanReport, path string) bool {
	for _, d := range rep.Roots {
		if d.Path == path {
			return true
		}
	}
	return false
}

func reportPaths(rep ScanReport) []string {
	out := make([]string, 0, len(rep.Roots))
	for _, d := range rep.Roots {
		out = append(out, d.Path)
	}
	return out
}

func pathsOf(idxs []LogIndex) []string {
	out := make([]string, 0, len(idxs))
	for _, idx := range idxs {
		out = append(out, idx.Path)
	}
	return out
}

func keysOf(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
