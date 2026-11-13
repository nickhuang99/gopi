package tools

// worker-log-session-attribution epic (2026-09-12) — the ACCEPTANCE test of
// the attribution contract, phrased exactly as the epic's核心验收:
//
//	"hand a failed task id to a later agent, with NO other record, and it must
//	 reach every output that task produced."
//
// The test therefore starts from the task id and NOTHING else — no directory
// listing, no session uuid from any other source — and walks the two faces a
// human/agent actually has:
//
//	gopi task show <id> → the rendered paths (this file's renderer)
//	TaskLogGlob(<id>) → the on-disk set those paths name
//
// It also pins the RETIRED shape's absence: nothing lands in the flat public
// <gopi-dir>/logs/ worker-*.log area.

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/nick/gopi/internal/config"
	"github.com/nick/gopi/internal/testutil"
)

func TestTaskOutputLogReachableFromTaskIDAlone(t *testing.T) {
	home := t.TempDir()
	t.Setenv(config.GopiDirEnv, home)
	testutil.NewTestDBPath(t)

	const taskID = "01a09aaa-dead-beef-cafe-000000000001"
	const sessionID = "5f5f5f5f-6a6a-7b7b-8c8c-9d9d9d9d9d9d"
	if _, err := TaskNew("[auto] run that failed", taskID, sessionID, "m"); err != nil {
		t.Fatalf("TaskNew: %v", err)
	}

	// The worker's output: written by the SAME resolver the worker uses, so
	// the test can never agree with `task show` by accident of a mirrored
	// literal.
	logPath, err := TaskLogPath(taskID, os.Getpid())
	if err != nil {
		t.Fatalf("task log path: %v", err)
	}
	const marker = "the output the failing run produced\n"
	if err := os.WriteFile(logPath, []byte(marker), 0o644); err != nil {
		t.Fatalf("write output log: %v", err)
	}

	// ── the walk: task id → every output ────────────────────────────────
	shown, err := TaskShow(taskID)
	if err != nil {
		t.Fatalf("TaskShow: %v", err)
	}
	if !strings.Contains(shown, TaskShowOutputLogLabel+logPath) {
		t.Fatalf("`task show %s` must render the output log path %q (that line is what makes the id sufficient):\n%s",
			taskID, logPath, shown)
	}
	// Every path the renderer printed must exist and be readable from the id
	// alone — resolve them back out of the RENDERED text, not from the
	// variables above (otherwise the assertion proves nothing about the
	// surface a later agent actually reads).
	resolved := outputLogPathsFromShow(t, shown)
	if len(resolved) == 0 {
		t.Fatalf("no %s line parsed out of:\n%s", TaskShowOutputLogLabel, shown)
	}
	for _, p := range resolved {
		b, err := os.ReadFile(p)
		if err != nil {
			t.Fatalf("a path rendered by `task show %s` is unreadable: %v", taskID, err)
		}
		if !strings.Contains(string(b), marker) {
			t.Errorf("output at %s is not the run's output: %q", p, b)
		}
		// The session dir is keyed by the TASK id (the stability reason in
		// TaskLogDir's doc — the session_id is backfilled mid-run, so it
		// cannot be the key).
		if !strings.HasPrefix(p, filepath.Join(home, ".gopi", config.SessionsDirName, taskID)) {
			t.Errorf("output %s does not live in the task's session dir", p)
		}
	}

	// The retired public area: no worker log file may appear there at all.
	if entries, err := os.ReadDir(filepath.Join(home, ".gopi", "logs")); err == nil {
		for _, e := range entries {
			if strings.HasPrefix(e.Name(), "worker-") {
				t.Errorf("a worker log landed in the retired public logs/ area: %s", e.Name())
			}
		}
	}

	// One glob reaches the whole set — the "list this task's outputs" line.
	glob, err := TaskLogGlob(taskID)
	if err != nil {
		t.Fatalf("TaskLogGlob: %v", err)
	}
	paths, err := filepath.Glob(glob)
	if err != nil {
		t.Fatalf("glob %s: %v", glob, err)
	}
	sort.Strings(paths)
	sort.Strings(resolved)
	if strings.Join(paths, "|") != strings.Join(resolved, "|") {
		t.Errorf("the glob and the rendered lines must name the SAME set:\nglob     %v\nrendered %v", paths, resolved)
	}
}

// outputLogPathsFromShow parses the paths out of `task show`'s own rendered
// text — deliberately NOT from the variables the test wrote them with.
func outputLogPathsFromShow(t *testing.T, shown string) []string {
	t.Helper()
	var out []string
	for _, line := range strings.Split(shown, "\n") {
		if !strings.HasPrefix(line, TaskShowOutputLogLabel) {
			continue
		}
		p := strings.TrimSpace(strings.TrimPrefix(line, TaskShowOutputLogLabel))
		p = strings.TrimSuffix(p, " (not found — output log pruned)")
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}

// TestTaskShowWithoutOutputLogRendersNoLine pins the honest-absence rule: a
// row the worker never executed has no output log to point at, so `task show`
// renders NO line (an empty or placeholder marker would suggest an output
// exists somewhere).
func TestTaskShowWithoutOutputLogRendersNoLine(t *testing.T) {
	home := t.TempDir()
	t.Setenv(config.GopiDirEnv, home)
	t.Setenv("GOPI_MEMORY_DB", filepath.Join(t.TempDir(), "memory.db"))

	if _, err := TaskNew("never executed by a worker", "t-no-output", "sess-no-output", ""); err != nil {
		t.Fatalf("TaskNew: %v", err)
	}
	shown, err := TaskShow("t-no-output")
	if err != nil {
		t.Fatalf("TaskShow: %v", err)
	}
	if strings.Contains(shown, TaskShowOutputLogLabel) {
		t.Errorf("a row with no worker output log must render no %s line:\n%s", TaskShowOutputLogLabel, shown)
	}
}
