package tools

// task_report_destination_test.go — the EXPORT DESTINATION precondition
// (epic report-export-destination, 2026-10-28).
//
// The seam exports an absolute report path into the executor child's
// environment, and the child's contract — stated in the prompt, obeyed by every
// run that writes a report — is `cat > "$GOPI_TASK_REPORT_PATH"`. The criterion
// this file pins is the epic's own sentence: an exported path must be WRITABLE,
// else it must not enter the environment.
//
// The arms below drive the real resolution (through GOPI_HOME, never a
// hand-joined "sessions/<id>" — a test that mirrors the production derivation
// would pass even if production pointed elsewhere):
//
//	1. the destination's PARENT EXISTS AND ACCEPTS A WRITE, even when the row's
//	   session container is absent (the measured state: a container never
//	   created by submit, pruned since, or removed by the run itself);
//	2. the destination and the report path are ONE resolution (Rule 14);
//	3. an UNWRITABLE destination is REFUSED, loudly, naming the directory —
//	   it never reaches an environment (Rule 15).

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nick/gopi/internal/taskreport"
)

// taskReportDestinationSandbox points GOPI_HOME at a temp dir and returns the
// row's session container path (which this helper does NOT create — that is the
// precondition under test).
func taskReportDestinationSandbox(t *testing.T, taskID string) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("GOPI_HOME", home)
	t.Setenv("GOPI_MEMORY_DB", filepath.Join(home, "memory.db"))
	return filepath.Join(home, ".gopi", sessionsDirNameForTest(), taskID)
}

// sessionsDirNameForTest is the sessions root's directory name as the rest of
// this package reads it — one reference, never a literal (Rule 1b/13).
func sessionsDirNameForTest() string { return taskSessionsRootName }

// TestReportDestinationParentExists is arm 1: the export resolution MATERIALISES
// the destination's parent and proves it writable.
//
// RED on the pre-fix code: the export answered TaskReportPath's string, whose
// parent is absent whenever the container is — the exact state that handed a
// run a destination `cat >` answers with ENOENT.
func TestReportDestinationParentExists(t *testing.T) {
	const taskID = "01a11500-0000-7000-8000-0000000000e2"
	sessionDir := taskReportDestinationSandbox(t, taskID)

	// THE PRECONDITION THE DEFECT NEEDS: the container is ABSENT at export time.
	if _, err := os.Stat(sessionDir); err == nil {
		t.Fatalf("fixture precondition: %s already exists, so the export below would not observe the defect", sessionDir)
	}

	path, err := TaskReportDestination(taskID)
	if err != nil {
		t.Fatalf("TaskReportDestination(%q): %v — an absent container is the state the export must repair, not a reason to refuse", taskID, err)
	}

	// THE CRITERION: the exported destination's parent exists...
	dir := filepath.Dir(path)
	fi, statErr := os.Stat(dir)
	if statErr != nil {
		t.Fatalf("the exported destination %q has no parent directory (%v) — a run obeying `cat > \"$%s\"` gets ENOENT there",
			path, statErr, taskreport.PathEnv)
	}
	if !fi.IsDir() {
		t.Fatalf("the exported destination's parent %q is not a directory (mode %v)", dir, fi.Mode())
	}
	// ...and accepts the contract's own write (existence without writability is
	// the state a read-only mount or another user's container leaves behind).
	if err := os.WriteFile(path, []byte("# Task Report: destination probe\n"), 0o644); err != nil {
		t.Fatalf("the exported destination %q is not writable: %v", path, err)
	}
	// The probe this seam uses must not survive its own check: an exported
	// destination is not left carrying the seam's litter.
	leftovers, globErr := filepath.Glob(filepath.Join(dir, taskReportProbeFile))
	if globErr != nil {
		t.Fatalf("scan %s for the writability probe: %v", dir, globErr)
	}
	if len(leftovers) > 0 {
		t.Errorf("the writability probe left %v behind in the exported destination", leftovers)
	}
}

// TestReportDestinationIsTheReportPath is arm 2: ONE resolution (Rule 14). The
// export adds a PRECONDITION to the report path; it never resolves a second
// path, so `task show` (which reads TaskReportPath) and the run's own
// destination can never name different directories.
func TestReportDestinationIsTheReportPath(t *testing.T) {
	const taskID = "01a11500-0000-7000-8000-0000000000e3"
	taskReportDestinationSandbox(t, taskID)

	want, err := TaskReportPath(taskID)
	if err != nil {
		t.Fatalf("TaskReportPath: %v", err)
	}
	got, err := TaskReportDestination(taskID)
	if err != nil {
		t.Fatalf("TaskReportDestination: %v", err)
	}
	if got != want {
		t.Fatalf("the export resolved %q while the read face resolves %q — two views of one contract that disagree", got, want)
	}
	if filepath.Base(got) != taskreport.FileName {
		t.Errorf("the export destination's file name is %q, want %q", filepath.Base(got), taskreport.FileName)
	}
}

// TestReportDestinationRefusesUnwritableParent is arm 3: an unwritable
// destination is REFUSED and NAMED. A silent best-effort export is what put an
// unopenable path into a run's environment, so the refusal is the deliverable
// here (Rule 15).
func TestReportDestinationRefusesUnwritableParent(t *testing.T) {
	const taskID = "01a11500-0000-7000-8000-0000000000e4"
	sessionDir := taskReportDestinationSandbox(t, taskID)
	if err := os.MkdirAll(sessionDir, taskReportDirMode); err != nil {
		t.Fatalf("prepare the container: %v", err)
	}
	if err := os.Chmod(sessionDir, 0o500); err != nil {
		t.Fatalf("make the container read-only: %v", err)
	}
	// The owner can chmod it back, so the restore must survive any exit path;
	// without it the temp tree is left unremovable and the failure would be
	// reported as a cleanup error rather than as this arm's verdict.
	t.Cleanup(func() {
		if err := os.Chmod(sessionDir, taskReportDirMode); err != nil {
			t.Errorf("cleanup: restore %s to %v so the temp tree is removable: %v", sessionDir, taskReportDirMode, err)
		}
	})

	_, err := TaskReportDestination(taskID)
	if err == nil {
		t.Fatalf("a read-only destination directory (%s) was accepted — an exported path that cannot be written is exactly what the contract must refuse", sessionDir)
	}
	if !strings.Contains(err.Error(), sessionDir) {
		t.Errorf("the refusal must name the directory it could not write (%s):\n%v", sessionDir, err)
	}
	if !strings.Contains(err.Error(), "not writable") {
		t.Errorf("the refusal must say the directory is not writable, so the operator knows what to fix:\n%v", err)
	}
}
