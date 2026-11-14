package main

// worker_status_task_rounddir_test.go — the ROUND'S SESSION DIRECTORY as the
// third task source (bug: `gopi worker --status` regressed to
// `task=(not recorded)` for the plain loop worker).
//
// THE MEASURED DEFECT (2026-10-28, before this fix):
//
//	$ gopi worker --status
//	gopi worker pid 3456940: FOREGROUND task=(not recorded) (holds no daemon lock) — gopi worker; `gopi worker --stop` signals it
//	… six such lines …
//	gopi worker pid 3687131: FOREGROUND task=01a17ff1-… (holds no daemon lock) — ./bin/gopi worker 01a17ff1-…; `gopi worker --stop` signals it
//
// The row was NOT unknowable: every one of those six processes stood in
//
//	/proc/<pid>/cwd -> /home/user/.gopi/sessions/01a17df9-524a-7362-9ebd-efc24de76275
//
// — the container `taskWorkdirEnter` moves a worker into at its claim boundary,
// and out of at the round's end (task_session_cwd.go). The derivation only asked
// the state record (a FOREGROUND worker writes none) and argv (a bare `gopi
// worker` carries no row), so the one fact the loop face DOES publish was never
// read.
//
// These tests drive the REAL derivation over a synthetic /proc (procnameRoot's
// own seam) plus a synthetic sessions root (config.SessionsRootEnv's own
// channel), so the fix cannot be satisfied by a hand-rolled shortcut: the
// positive case proves the id comes back, and the five refusals prove no id is
// invented when the directory is not a task's container.

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/nickhuang99/gopi/internal/config"
	"github.com/nickhuang99/gopi/internal/procname"
)

// wstRoundCwd plants /proc/<pid>/cwd for a fixture pid: a REAL symlink, so the
// reader under test follows the same channel it follows in production (readlink
// on the link, never a string the fixture handed it).
func wstRoundCwd(t *testing.T, root string, pid int, dir string) {
	t.Helper()
	if err := os.Symlink(dir, filepath.Join(root, strconv.Itoa(pid), procname.CwdName)); err != nil {
		t.Fatalf("plant cwd link -> %s: %v", dir, err)
	}
}

// wstRoundFixture sets up the two seams and a bare loop worker: argv is exactly
// `gopi worker` (the regressed shape), standing in dir.
func wstRoundFixture(t *testing.T, pid int, dir string) {
	t.Helper()
	root := t.TempDir()
	wstProc(t, root, pid, []string{"gopi", procname.VerbWorker}, 1000)
	wstRoundCwd(t, root, pid, dir)
	t.Setenv(procname.EnvProcRoot, root)
}

// THE REGRESSION: a bare loop worker standing in a row's session container
// names that row, from the third source.
func TestWorkerTaskRoundSessionDirNamesTheRunningRow(t *testing.T) {
	self := os.Getpid()
	const row = "01a17df9-524a-7362-9ebd-efc24de76275"
	sessionsRoot := filepath.Join(t.TempDir(), config.SessionsDirName)
	dir := filepath.Join(sessionsRoot, row)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", dir, err)
	}
	t.Setenv(config.SessionsRootEnv, sessionsRoot)
	wstRoundFixture(t, self, dir)

	got := workerTaskOfPID(self)
	if got.ID != row {
		t.Fatalf("a loop worker standing in row %q's session container reported %q — the round's own cwd is the fact this derivation exists to read", row, got.Field())
	}
	if got.Source != workerTaskSourceSessionDir {
		t.Fatalf("the id came back with source %q, want %q (the operator must be able to tell how much to trust it)", got.Source, workerTaskSourceSessionDir)
	}
	// And the rendered line carries it, since the field is what the operator greps.
	line := workerForegroundStatusLine(self)
	if !strings.Contains(line, workerStatusTaskFieldPrefix+row) {
		t.Fatalf("the foreground line does not carry the field: %q", line)
	}
}

// The refusals — the five shapes where the directory is NOT a task's container,
// so naming a row from it would be an invented id.
func TestWorkerTaskRoundSessionDirRefusesToInventARow(t *testing.T) {
	const row = "01a17df9-524a-7362-9ebd-efc24de76275"
	sessionsRoot := filepath.Join(t.TempDir(), config.SessionsDirName)
	for _, tc := range []struct {
		name string
		// dir is computed from sessionsRoot; root overrides the env value when
		// the case is about standing under a DIFFERENT root.
		dir     func() string
		rootEnv func() string
		why     string
	}{
		{
			name:    "nested one level deep is not a task container",
			dir:     func() string { return filepath.Join(sessionsRoot, row, "sub") },
			rootEnv: func() string { return sessionsRoot },
			why:     "a task container is a DIRECT child of the sessions root (taskSessionDir joins exactly one element)",
		},
		{
			name:    "the sessions root itself",
			dir:     func() string { return sessionsRoot },
			rootEnv: func() string { return sessionsRoot },
			why:     "the root is not a task",
		},
		{
			name:    "a uuid spelled without its dashes",
			dir:     func() string { return filepath.Join(sessionsRoot, strings.ReplaceAll(row, "-", "")) },
			rootEnv: func() string { return sessionsRoot },
			why:     "the branch/worktree spelling is the shape sessionid refuses; it must not be reported as a row",
		},
		{
			// THE KNOWN EDGE of this arm, stated rather than hidden: the shape
			// gate is internal/sessionid, whose documented contract is
			// deliberately NARROW — it refuses only the spellings it can PROVE
			// are a mangled uuid (`not-a-task-id` is accepted, because the tree
			// really does mint non-uuid id-shaped tokens and over-blocking was
			// measured to be the worse defect). So this arm does not answer
			// "does row X exist?" — that is the ledger's question, and asking it
			// per worker would turn one read-only process scan into N database
			// reads. What it DOES guarantee is narrower and still the point of
			// the fix: only a directory this profile's own sessions root
			// produced (taskWorkdirEnter joins exactly `<root>/<task-id>`, and
			// the id it joins has already passed the same gate) can name a row.
			name:    "a task-id-shaped directory under a DIFFERENT root",
			dir:     func() string { return filepath.Join(t.TempDir(), row) },
			rootEnv: func() string { return sessionsRoot },
			why:     "only THIS profile's sessions root defines containers; any other directory named like a row is somebody else's path",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			self := os.Getpid()
			dir := tc.dir()
			if err := os.MkdirAll(dir, 0o755); err != nil {
				t.Fatalf("mkdir %s: %v", dir, err)
			}
			t.Setenv(config.SessionsRootEnv, tc.rootEnv())
			wstRoundFixture(t, self, dir)
			got := workerTaskOfPID(self)
			if got.Recorded() {
				t.Fatalf("a worker standing in %q reported row %q — %s", dir, got.ID, tc.why)
			}
			if got.Field() != workerStatusTaskFieldPrefix+workerStatusTaskNotRecorded {
				t.Fatalf("the refusal must render the not-recorded marker, got %q", got.Field())
			}
		})
	}
}

// argv OUTRANKS the round directory: a named-row worker whose cwd is elsewhere
// (before its first claim, between rounds) is still that row.
func TestWorkerTaskArgvOutranksTheRoundSessionDir(t *testing.T) {
	self := os.Getpid()
	const argvRow = "01a0argv-row"
	const cwdRow = "01a17df9-524a-7362-9ebd-efc24de76275"
	sessionsRoot := filepath.Join(t.TempDir(), config.SessionsDirName)
	dir := filepath.Join(sessionsRoot, cwdRow)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", dir, err)
	}
	t.Setenv(config.SessionsRootEnv, sessionsRoot)
	root := t.TempDir()
	wstProc(t, root, self, []string{"gopi", procname.VerbWorker, argvRow}, 1000)
	wstRoundCwd(t, root, self, dir)
	t.Setenv(procname.EnvProcRoot, root)

	got := workerTaskOfPID(self)
	if got.ID != argvRow || got.Source != workerTaskSourceArgv {
		t.Fatalf("a named-row worker reported %+v, want %q from source %q — the started row outranks a directory", got, argvRow, workerTaskSourceArgv)
	}
}

// An unreadable /proc entry still says the task is unknown — the round-dir arm
// must not turn "nothing readable" into a claim.
func TestWorkerTaskRoundDirUnreadableProcStaysUnknown(t *testing.T) {
	t.Setenv(procname.EnvProcRoot, t.TempDir())
	t.Setenv(config.SessionsRootEnv, filepath.Join(t.TempDir(), config.SessionsDirName))
	got := workerTaskOfPID(os.Getpid())
	if got.Recorded() || got.sourceWord() != workerTaskSourceNone {
		t.Fatalf("an unreadable /proc entry produced %+v, want source %q and no id", got, workerTaskSourceNone)
	}
}
