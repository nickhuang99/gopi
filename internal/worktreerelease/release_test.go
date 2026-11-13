// release_test.go — the shared release action's own contract, so the ONE
// implementation is pinned where it lives (the epic's acceptance rig in
// cmd/agent drives it end-to-end; this file pins the pieces).
package worktreerelease

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestReleaseMovesCwdToProcessRoot is the base contract: a process standing
// somewhere else ends up at ProcessRootPath.
func TestReleaseMovesCwdToProcessRoot(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	if err := Release(); err != nil {
		t.Fatalf("Release: %v", err)
	}
	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd: %v", err)
	}
	if wd != ProcessRootPath {
		t.Fatalf("after Release cwd = %q, want %q", wd, ProcessRootPath)
	}
	// Idempotent: the second call is a no-op, not an error (close-outs run on
	// several exit paths and a nested wrapper must not report a failure).
	if err := Release(); err != nil {
		t.Fatalf("second Release: %v (must be idempotent)", err)
	}
}

// TestReleaseReportingSkipsInsideATestProcess pins the MEASURED contract that
// made the guard necessary: a test binary's cwd belongs to the harness, so the
// seam reports success and leaves it alone. Measured cost of NOT having this:
// wiring the seam into the producer close-outs moved the WHOLE cmd/agent suite's
// cwd to "/" and broke 30 cwd-relative tests ("read fixture
// ../../tests/fixtures/…: no such file"), because close-out seams are exercised
// in-process by the suite.
func TestReleaseReportingSkipsInsideATestProcess(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	var buf bytes.Buffer
	if ok := ReleaseReporting(&buf, "gopi test"); !ok {
		t.Fatalf("ReleaseReporting reported failure: %s", buf.String())
	}
	if buf.Len() != 0 {
		t.Fatalf("skip wrote %q, want no output", buf.String())
	}
	if wd, err := os.Getwd(); err != nil || wd != dir {
		t.Fatalf("cwd = %q (err=%v), want the harness's %q — a test process is not released", wd, err, dir)
	}
	if !inTestProcess() {
		t.Fatalf("this IS a test process (executable ends in %q) — the guard's premise is not holding", TestProcessExecutableSuffix)
	}
	// The FALLBACK direction is the other half of the contract, and it is the
	// one that matters in production: an unreadable executable must NOT be
	// mistaken for a test process, because that would silently skip a producer
	// obligation. Release() itself is unconditional (measured above); only the
	// seam consults the guard.
	if err := Release(); err != nil {
		t.Fatalf("Release must remain unconditional: %v", err)
	}
	if wd, err := os.Getwd(); err != nil || wd != ProcessRootPath {
		t.Fatalf("direct Release left cwd = %q (err=%v), want %q", wd, err, ProcessRootPath)
	}
}

// TestReleaseRootAlwaysExists guards the choice of destination: the release
// chdirs into a directory that cannot have been removed under it first (the
// defect's own shape was "the directory is gone"). If ProcessRootPath ever
// gains a prefix, this fails before the field does.
func TestReleaseRootAlwaysExists(t *testing.T) {
	st, err := os.Stat(ProcessRootPath)
	if err != nil {
		t.Fatalf("ProcessRootPath %q is not statable: %v", ProcessRootPath, err)
	}
	if !st.IsDir() {
		t.Fatalf("ProcessRootPath %q is not a directory", ProcessRootPath)
	}
	if !filepath.IsAbs(ProcessRootPath) || strings.Contains(ProcessRootPath, "..") {
		t.Fatalf("ProcessRootPath %q must be an absolute, unadorned directory", ProcessRootPath)
	}
}
