package tools

// task_report_test.go — the MECHANISM proof for the bounded task report seam
// (epic task-report, acceptance: "mechanism proven independent of model
// compliance: a unit test on the writer/reader seam"). Every test here drives
// the harness faces directly — no model, no child process, no API key:
//
//	SealTaskReport      — the writer (skeleton when the model wrote nothing,
//	                      bound when it wrote too much, never clobbering)
//	TaskReportShowBlock — the reader (`task show`'s report section)
//
// The path is driven through GOPI_HOME (the real resolution), never by
// hand-joining "sessions/<id>" — a test that mirrors the production path
// derivation would pass even if production pointed somewhere else.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nick/gopi/internal/config"
	"github.com/nick/gopi/internal/taskreport"
)

// reportSandbox points GOPI_HOME at a temp dir and returns the resolved
// session directory of taskID (created on demand by the seam itself).
func reportSandbox(t *testing.T, taskID string) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("GOPI_HOME", home)
	t.Setenv("GOPI_MEMORY_DB", filepath.Join(home, "memory.db"))
	path, err := TaskReportPath(taskID)
	if err != nil {
		t.Fatalf("TaskReportPath: %v", err)
	}
	want := filepath.Join(home, ".gopi", config.SessionsDirName, taskID, taskreport.FileName)
	if path != want {
		t.Fatalf("TaskReportPath = %q, want %q", path, want)
	}
	return filepath.Dir(path)
}

// TestSealTaskReportWritesHonestSkeleton: the model wrote NOTHING — the seal
// must still leave a report on disk carrying every required section and the
// harness-owned facts, with an explicit "not recorded" marker (never a
// fabricated narrative). This is the anti-silent-hole guarantee.
func TestSealTaskReportWritesHonestSkeleton(t *testing.T) {
	const taskID = "t-report-skeleton"
	dir := reportSandbox(t, taskID)

	line, err := SealTaskReport(taskID)
	if err != nil {
		t.Fatalf("SealTaskReport: %v", err)
	}
	path := filepath.Join(dir, taskreport.FileName)
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("report not written at %s: %v", path, err)
	}
	if missing := taskreport.MissingSections(string(body)); len(missing) > 0 {
		t.Errorf("skeleton report missing required sections %v:\n%s", missing, body)
	}
	if !strings.Contains(string(body), taskreport.SkeletonPlaceholder) {
		t.Errorf("skeleton must mark the unrecorded parts explicitly:\n%s", body)
	}
	if !strings.Contains(string(body), taskID) {
		t.Errorf("skeleton must carry the harness-owned task id:\n%s", body)
	}
	// The ledger line names the path + size — the index that outlives the file.
	if !strings.Contains(line, path) || !strings.Contains(line, taskreport.DigestLinePrefix) {
		t.Errorf("digest line = %q, want %s<path>", line, taskreport.DigestLinePrefix)
	}
	if got := taskreport.PathFromDigest(line); got != path {
		t.Errorf("PathFromDigest(line) = %q, want %q", got, path)
	}
	if len(body) > taskreport.MaxBytes {
		t.Errorf("skeleton is %d bytes > cap %d", len(body), taskreport.MaxBytes)
	}
}

// TestSealTaskReportPreservesModelReport: a model-authored report is NEVER
// replaced by the skeleton — the seal bounds it and leaves the rest alone.
func TestSealTaskReportPreservesModelReport(t *testing.T) {
	const taskID = "t-report-authored"
	dir := reportSandbox(t, taskID)

	authored := "# Task Report: mine\n\n## Completed\n- did the thing\n\n" +
		"## Files\n- a.go, branch feat/x, commit deadbeef\n\n" +
		"## Verification\n- go test ./...: ok\n\n## Not Done\n- nothing\n"
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir session dir: %v", err)
	}
	path := filepath.Join(dir, taskreport.FileName)
	if err := os.WriteFile(path, []byte(authored), 0o644); err != nil {
		t.Fatalf("seed authored report: %v", err)
	}

	line, err := SealTaskReport(taskID)
	if err != nil {
		t.Fatalf("SealTaskReport: %v", err)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read back: %v", err)
	}
	if string(got) != authored {
		t.Errorf("seal CLOBBERED the model's report:\n--- want ---\n%s\n--- got ---\n%s", authored, got)
	}
	if strings.Contains(line, "missing sections") {
		t.Errorf("complete report must not be flagged: %q", line)
	}
}

// TestSealTaskReportBoundsOversizedReport: the "bounded" half of the epic. An
// oversized model report is capped at MaxBytes on a line boundary with an
// explicit truncation marker — a silently shortened report would read as the
// author's own ending (Rule 15).
func TestSealTaskReportBoundsOversizedReport(t *testing.T) {
	const taskID = "t-report-huge"
	dir := reportSandbox(t, taskID)

	var b strings.Builder
	b.WriteString("# Task Report: huge\n\n## Completed\n")
	for b.Len() < taskreport.MaxBytes*2 {
		b.WriteString("- a line of report body that keeps going and going\n")
	}
	for _, s := range taskreport.RequiredSections {
		b.WriteString("\n" + s.Heading + "\n- present\n")
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir session dir: %v", err)
	}
	path := filepath.Join(dir, taskreport.FileName)
	if err := os.WriteFile(path, []byte(b.String()), 0o644); err != nil {
		t.Fatalf("seed huge report: %v", err)
	}

	line, err := SealTaskReport(taskID)
	if err != nil {
		t.Fatalf("SealTaskReport: %v", err)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read back: %v", err)
	}
	// The marker itself is part of the allowed length.
	if len(got) > taskreport.MaxBytes+len(taskreport.TruncationMarker()) {
		t.Errorf("bounded report is %d bytes, over cap %d + marker", len(got), taskreport.MaxBytes)
	}
	if !strings.HasSuffix(string(got), taskreport.TruncationMarker()) {
		t.Errorf("truncated report must END with the marker, got tail: %q",
			string(got[len(got)-120:]))
	}
	if !strings.Contains(line, "truncated") {
		t.Errorf("digest line must record the truncation: %q", line)
	}
}

// TestTaskReportShowBlockRendersBody: the reader face — a present report is
// rendered bounded, with its path, in the output an operator already reads.
func TestTaskReportShowBlockRendersBody(t *testing.T) {
	const taskID = "t-report-show"
	dir := reportSandbox(t, taskID)

	body := "# Task Report: show me\n\n## Completed\n- wired the seam\n\n" +
		"## Files\n- internal/tools/task_report.go\n\n## Verification\n- go build ./...: ok\n\n## Not Done\n- (nothing)\n"
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir session dir: %v", err)
	}
	path := filepath.Join(dir, taskreport.FileName)
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatalf("seed report: %v", err)
	}

	block := TaskReportShowBlock(taskID, 0, "")
	if !strings.Contains(block, "task_report:") || !strings.Contains(block, path) {
		t.Errorf("show block must name the report path:\n%s", block)
	}
	if !strings.Contains(block, "## Completed") || !strings.Contains(block, "## Not Done") {
		t.Errorf("show block must render the report body:\n%s", block)
	}
	if strings.Contains(block, "missing required section") {
		t.Errorf("complete report must not warn:\n%s", block)
	}
}

// TestTaskReportShowBlockDegradesHonestly: the three no-body shapes, each
// stating its own truth — never a silent absence (epic acceptance: historical
// tasks degrade honestly rather than pretending).
func TestTaskReportShowBlockDegradesHonestly(t *testing.T) {
	const taskID = "t-report-absent"
	dir := reportSandbox(t, taskID)

	// 1. Nothing on disk, no recorded path, PRE-seam row → the legacy note.
	//    (created_at before the seam's cutoff date: the row never had the
	//    chance to carry a report — say so instead of implying a failure.)
	block := TaskReportShowBlock(taskID, 1, "")
	if !strings.Contains(block, taskreport.LegacyNote) {
		t.Errorf("pre-seam row must render %q, got %q", taskreport.LegacyNote, block)
	}

	// 2. Nothing on disk, no recorded path, POST-seam row → a plain gap note.
	block = TaskReportShowBlock(taskID, taskreport.LegacyCutoffUnix+1, "")
	if !strings.Contains(block, taskreport.NoneNote) {
		t.Errorf("post-seam row must render %q, got %q", taskreport.NoneNote, block)
	}

	// 3. The body is GONE but the ledger recorded the path → render the path
	//    (it was recorded at run end; the file was pruned later) — the
	//    recorded index is why the path outlives the directory.
	recorded := filepath.Join(dir, taskreport.FileName)
	block = TaskReportShowBlock(taskID, 0, "gopi run completed — "+taskreport.DigestLine(recorded, 42, nil))
	if !strings.Contains(block, recorded) || !strings.Contains(block, "not found") {
		t.Errorf("pruned body must render the recorded path + 'not found', got %q", block)
	}

	// 4. An EMPTY file (the model wrote nothing and the seal never ran) is
	//    reported as empty, not as "no report recorded".
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir session dir: %v", err)
	}
	if err := os.WriteFile(recorded, []byte("\n  \n"), 0o644); err != nil {
		t.Fatalf("seed empty report: %v", err)
	}
	block = TaskReportShowBlock(taskID, 0, "")
	if !strings.Contains(block, "empty — the model wrote no report") {
		t.Errorf("empty file must be named as such, got %q", block)
	}
}

// TestTaskReportSectionContractIsSingleSource: the headings the WRITER emits
// and the headings the READER requires are one list (Rule 14) — a report the
// seal produces must never be flagged by the show-side check.
func TestTaskReportSectionContractIsSingleSource(t *testing.T) {
	const taskID = "t-report-contract"
	dir := reportSandbox(t, taskID)

	if _, err := SealTaskReport(taskID); err != nil {
		t.Fatalf("SealTaskReport: %v", err)
	}
	body, err := os.ReadFile(filepath.Join(dir, taskreport.FileName))
	if err != nil {
		t.Fatalf("read skeleton: %v", err)
	}
	for _, s := range taskreport.RequiredSections {
		if !strings.Contains(string(body), s.Heading) {
			t.Errorf("skeleton lacks required heading %q", s.Heading)
		}
	}
	block := TaskReportShowBlock(taskID, 0, "")
	if strings.Contains(block, "missing required section") {
		t.Errorf("the harness's own skeleton must satisfy the reader's contract:\n%s", block)
	}
}
