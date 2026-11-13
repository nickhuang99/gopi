package testutil

// The TASK-LOG-RE-HOMING contract (epic task-session-cwd, 2026-09-12): the
// retired per-worker log area (<gopi-dir>/logs/worker-<ts>-<i>.log and the
// anonymous gopi-<ts>.log) is swept once — every byte that can be attributed to
// a task is moved into THAT task's session directory, and what cannot be
// attributed is deleted.
//
// WHY A SCRIPT TEST AND NOT A GO UNIT TEST: the sweep is a host-level
// one-off over the operator's real profile (an existing tree of files nothing
// produced any more), so it lives in scripts/ and is driven here through its
// own --home / --dry-run / --apply seams — never by reimplementing its rules.
//
// WHAT IT PINS (each item is a rule a future editor could break):
//  1. dry-run is the DEFAULT and changes nothing (the script's own promise);
//  2. a worker log with ONE claim is re-homed WHOLE to that task's dir;
//  3. a worker log with N claims is SPLIT at the claim boundaries — each task
//     gets its own round and NOTHING of its neighbours' (the mis-attribution
//     the epic exists to remove: 13 of the real profile's 20 files served
//     2..20 tasks);
//  4. an idle worker log (no claim at all) is deleted — it belongs to no task;
//  5. the retired leveled logger's output (gopi-<ts>.log) is deleted, since no
//     task id or session id exists anywhere in the file;
//  6. process-scoped logs that are NOT the retired shape (merger.log,
//     worker-startup.log) are never touched;
//  7. --apply is idempotent: a second run finds nothing to do;
//  8. the ACCEPTANCE PROPERTY of the whole epic: after the sweep, the task id
//     ALONE leads to its output (glob <sessions>/<task>/output*.log — the same
//     glob `gopi task show <id>` renders).

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// rehomeScriptName is the sweep under test (a repo root sibling of the
// launcher this package already drives).
const rehomeScriptName = "rehome-task-logs.sh"

// rehomeFixture is one synthetic profile: the files the sweep classifies, with
// the task dirs they must end up in. Every entry's comment states which rule it
// exercises, so a deleted fixture is a visible loss of coverage.
func writeRehomeFixture(t *testing.T, gd string) {
	t.Helper()
	logs := filepath.Join(gd, "logs")
	if err := os.MkdirAll(logs, 0o755); err != nil {
		t.Fatalf("mkdir logs: %v", err)
	}
	files := map[string]string{
		// Rule 2: one claim → the whole file is that task's round (the banner
		// before the claim travels with it).
		"worker-20260901-010101-1.log": "worker: loop start\n" +
			"worker: claimed t-rehome-one\n" +
			"worker: resumed claimed task t-rehome-one\n" +
			"UNIQUE-ONE\n",
		// Rule 3: two claims → split; each slice must carry ONLY its own lines.
		"worker-20260901-010102-2.log": "worker: loop start\n" +
			"worker: claimed t-rehome-first\n" +
			"FIRST-SLICE-A\n" +
			"worker: claimed t-rehome-second\n" +
			"SECOND-SLICE-B\n",
		// Rule 4: an idle worker never claimed anything.
		"worker-20260901-010103-3.log": "worker: loop start\n" +
			"worker: queue drained (1/3 idle rounds) — waiting 30s\n" +
			"worker: 3 idle round(s) with no new task — exiting 0\n",
		// Rule 5: the retired leveled logger — no task id, no session id.
		"gopi-20260802-151504.log": "GoPi log started at 2026-08-02T15:15:04+08:00 (level=INFO)\n" +
			"[15:15:05] ERROR DONE ⏱ 0.3s | 1 iterations\n",
		// Rule 6: process-scoped logs that are STILL LIVE and not the shape.
		"merger.log":         "merger: tick\n",
		"worker-startup.log": "worker: loop start\n",
	}
	for name, body := range files {
		if err := os.WriteFile(filepath.Join(logs, name), []byte(body), 0o644); err != nil {
			t.Fatalf("write fixture %s: %v", name, err)
		}
	}
}

// runRehome drives the sweep with --home pointed at the synthetic profile.
func runRehome(t *testing.T, gd string, args ...string) string {
	t.Helper()
	return runScript(t, []string{"GOPI_HOME="}, rehomeScriptName, append([]string{"--home", gd}, args...)...)
}

// TestRehomeTaskLogsSplitsByClaimAndDropsTheUnattributable is the whole
// contract in one walk: dry-run changes nothing, apply re-homes every
// attributable line, and a second apply is a no-op.
func TestRehomeTaskLogsSplitsByClaimAndDropsTheUnattributable(t *testing.T) {
	gd := filepath.Join(t.TempDir(), ".gopi")
	writeRehomeFixture(t, gd)
	logs := filepath.Join(gd, "logs")
	sessions := filepath.Join(gd, "sessions")

	// (1) the default is a dry run: it must PRINT the plan and touch nothing.
	plan := runRehome(t, gd)
	if !strings.Contains(plan, "dry-run") || !strings.Contains(plan, "would re-home") {
		t.Fatalf("the default mode must be a dry run that prints the plan, got:\n%s", plan)
	}
	if entries, err := os.ReadDir(sessions); err == nil && len(entries) > 0 {
		t.Fatalf("a dry run created %d entries under %s — dry-run must change nothing", len(entries), sessions)
	}
	for _, name := range []string{"worker-20260901-010101-1.log", "gopi-20260802-151504.log"} {
		if _, err := os.Stat(filepath.Join(logs, name)); err != nil {
			t.Fatalf("a dry run removed %s: %v", name, err)
		}
	}

	// (2..6) apply.
	if out := runRehome(t, gd, "--apply"); !strings.Contains(out, "apply done") {
		t.Fatalf("--apply must report its outcome, got:\n%s", out)
	}

	// (8) THE ACCEPTANCE PROPERTY: from the task id alone to its output.
	readTaskOutput := func(taskID string) string {
		t.Helper()
		matches, err := filepath.Glob(filepath.Join(sessions, taskID, "output*.log"))
		if err != nil || len(matches) == 0 {
			t.Fatalf("task %s alone must lead to its output (glob %s): %v", taskID, filepath.Join(sessions, taskID, "output*.log"), err)
		}
		var b strings.Builder
		for _, m := range matches {
			raw, rerr := os.ReadFile(m)
			if rerr != nil {
				t.Fatalf("read %s: %v", m, rerr)
			}
			b.Write(raw)
		}
		return b.String()
	}

	one := readTaskOutput("t-rehome-one")
	if !strings.Contains(one, "UNIQUE-ONE") || !strings.Contains(one, "worker: claimed t-rehome-one") {
		t.Errorf("a single-claim worker log must be re-homed whole; got:\n%s", one)
	}
	// The banner precedes the claim and belongs to no earlier task, so a
	// single-claim file keeps it (the round started there).
	if !strings.Contains(one, "worker: loop start") {
		t.Errorf("the single-claim slice must start at the file's first line (the round's own banner); got:\n%s", one)
	}

	first := readTaskOutput("t-rehome-first")
	second := readTaskOutput("t-rehome-second")
	if !strings.Contains(first, "FIRST-SLICE-A") {
		t.Errorf("the first claim's slice is missing; got:\n%s", first)
	}
	if strings.Contains(first, "SECOND-SLICE-B") {
		t.Errorf("MIS-ATTRIBUTION: the first task's output carries a LATER task's line (the defect this epic removes); got:\n%s", first)
	}
	if !strings.Contains(second, "SECOND-SLICE-B") {
		t.Errorf("the second claim's slice is missing; got:\n%s", second)
	}
	if strings.Contains(second, "FIRST-SLICE-A") || strings.Contains(second, "worker: loop start") {
		t.Errorf("the second task's output carries an EARLIER slice (its own round starts at its claim line); got:\n%s", second)
	}

	// (4)(5) the unattributable shapes are GONE, and nothing else was taken.
	for _, gone := range []string{
		"worker-20260901-010103-3.log", // idle: no claim to attribute
		"gopi-20260802-151504.log",     // retired logger: no id anywhere in the file
		"worker-20260901-010101-1.log", // re-homed: its bytes are in the session dirs
		"worker-20260901-010102-2.log",
	} {
		if _, err := os.Stat(filepath.Join(logs, gone)); !os.IsNotExist(err) {
			t.Errorf("%s must be gone after --apply (stat err = %v)", gone, err)
		}
	}
	for _, kept := range []string{"merger.log", "worker-startup.log"} {
		if _, err := os.Stat(filepath.Join(logs, kept)); err != nil {
			t.Errorf("%s is a LIVE process-scoped log and must never be touched: %v", kept, err)
		}
	}

	// (7) idempotence: a second --apply has nothing left to do.
	again := runRehome(t, gd, "--apply")
	if !strings.Contains(again, "re-homed=0 split=0") {
		t.Errorf("a second --apply must be a no-op, got:\n%s", again)
	}
	// …and the re-homed bytes are still there (a repeat must not truncate the
	// destination it just wrote).
	if !strings.Contains(readTaskOutput("t-rehome-one"), "UNIQUE-ONE") {
		t.Errorf("a second --apply destroyed the re-homed output")
	}
}

// TestRehomeTaskLogsRefusesAnUnusableSessionsTree: the destination is the ONLY
// home of the output, so a tree that cannot be created must ABORT before any
// source is deleted — never "delete now, write later".
func TestRehomeTaskLogsRefusesAnUnusableSessionsTree(t *testing.T) {
	gd := filepath.Join(t.TempDir(), ".gopi")
	writeRehomeFixture(t, gd)
	// A FILE where the sessions root must be: mkdir -p fails for every slice.
	if err := os.WriteFile(filepath.Join(gd, "sessions"), []byte("not a directory\n"), 0o644); err != nil {
		t.Fatalf("occlude sessions root: %v", err)
	}
	root, err := repoRootFromCwd()
	if err != nil {
		t.Fatalf("repo root: %v", err)
	}
	cmd := exec.Command("bash", filepath.Join(root, scriptsDirName, rehomeScriptName), "--home", gd, "--apply")
	out, runErr := cmd.CombinedOutput()
	if runErr == nil {
		t.Errorf("the sweep must FAIL on an unusable sessions tree, it exited 0:\n%s", out)
	}
	if !strings.Contains(string(out), "cannot create") {
		t.Fatalf("an unusable sessions tree must abort loudly, got:\n%s", out)
	}
	for _, kept := range []string{
		"worker-20260901-010101-1.log",
		"worker-20260901-010102-2.log",
	} {
		if _, err := os.Stat(filepath.Join(gd, "logs", kept)); err != nil {
			t.Errorf("%s is the ONLY copy of its task's output and must survive a failed run: %v", kept, err)
		}
	}
}
