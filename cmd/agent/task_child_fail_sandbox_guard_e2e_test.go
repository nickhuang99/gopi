package main

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"

	"github.com/nickhuang99/gopi/internal/config"
	"github.com/nickhuang99/gopi/internal/scratch"
	"github.com/nickhuang99/gopi/internal/tools"
	"github.com/nickhuang99/gopi/internal/worktreeroots"
	_ "modernc.org/sqlite"

	"github.com/nickhuang99/gopi/internal/testutil"
)

// TestTaskChildFailEscapeRefusesOutsideSandboxE2E is the MINIMAL REPRO of the
// 2026-09-16 incident (Rule 5), run against the REAL BINARY so the env
// inheritance that caused it is the same one under test.
//
// THE INCIDENT: a resident `gopi worker` inherited GOPI_TASK_CHILD_FAIL_TEST=1
// from its launcher shell, claimed 5 real operator rows and terminally burned
// them in 6 seconds (synthetic error, elapsed=0s) — nothing attempted, nothing
// judged. The ledger it burned them in was the operator's REAL one
// (~/.gopi/memory.db).
//
// WHAT THIS TEST DRIVES: exactly that shape, against a ledger path on `/` (the
// `mktemp -d` shape — the only safe way to run a fixture "against the real
// profile's ledger" without touching it) — armed escape, a claimable row, and
// a full `gopi worker --once` round.
//
// RED BEFORE THE FIX (measured: the escape's branch had zero precondition, so
// the row came back `failed` with detail "synthetic child failure (test
// escape)" and the worker exited 0). GREEN AFTER: the worker exits with
// taskChildFailRefusalExitCode, the refusal names the measured path, and the
// row's status is UNCHANGED — a pending row is still `pending`, with no
// event trail from this process.
func TestTaskChildFailEscapeRefusesOutsideSandboxE2E(t *testing.T) {
	bin := testutil.GopiBin(t)
	// REPO-CONTEXT PIN (master's repo-context gate, which landed after this
	// branch was parked): the children below are real `gopi worker` / `task
	// submit` processes. Inheriting THIS process's cwd would hand them the
	// developer's live repository, whose own-worktree-release pass enumerates
	// every clean unowned worktree past its age floor. This rig seeds its row
	// through the shipped CLI (`gopi task submit`), which REFUSES a non-repo cwd,
	// so the pin is the FIXTURE-REPO variant: a throwaway repository whose only
	// worktree is its main checkout (never removable).
	SandboxRepoContextInFixtureRepo(t)
	// The unsandboxed profile: GOPI_HOME on the `/` partition and the ledger
	// beside it — precisely what `mktemp -d` hands a careless test recipe, and
	// what the naive "is it /tmp" reading calls a sandbox.
	outside := unsandboxedProfileRoot(t)
	home := filepath.Join(outside, "gopi-home")
	dbPath := filepath.Join(outside, "memory.db")
	if err := os.MkdirAll(home, 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", home, err)
	}

	env := unsandboxedProfileEnv(t, home, dbPath, outside)
	if out, code := runGopiIn(t, bin, env, "", CliVerbTask, "submit",
		"escape-needs-sandbox repro", "--payload", "must never be attempted",
		"--task-id", tEscRefuseRow); code != 0 {
		t.Fatalf("submit: exit=%d\n%s", code, out)
	}
	if st := taskStatusIn(t, dbPath, tEscRefuseRow); st != "pending" {
		t.Fatalf("fixture precondition: row status = %q, want pending", st)
	}

	out, code := runGopiIn(t, bin, env, "", CliVerbWorker, workerVerbArgv)
	if code != wantTaskChildFailRefusalExit {
		t.Fatalf("an armed escape against a NON-sandbox ledger must be refused with exit %d, got exit=%d\n%s",
			wantTaskChildFailRefusalExit, code, out)
	}
	for _, want := range []string{taskChildFailTestEnv, "REFUSED", dbPath} {
		if !strings.Contains(out, want) {
			t.Errorf("the refusal must carry %q (loud, Rule 15), got:\n%s", want, out)
		}
	}
	// THE POINT OF THE WHOLE FIX: the row is untouched. Before the fix this is
	// `failed` with a synthetic detail and elapsed=0s.
	if st := taskStatusIn(t, dbPath, tEscRefuseRow); st != "pending" {
		t.Fatalf("the refused escape CHANGED the row: status = %q, want pending (no attempt, no judgement)", st)
	}
	if n := taskEventsIn(t, dbPath, tEscRefuseRow); n != 0 {
		t.Errorf("the refused escape wrote %d task_events row(s), want 0", n)
	}
}

// TestTaskChildFailEscapeStillFiresInsideSandboxE2E is the PAIRING half: the
// refusal must not disarm the escape for the hermetic tests that depend on it.
// Same binary, same armed variable, same row shape — only the profile moves
// under the measured temp root (a throwaway GOPI_HOME + ledger there).
//
// Without this half a guard that refused unconditionally would look like a
// pass, and TestRunTaskWorker_SurvivesFailedRun — the briefing's
// TestRunTaskServe_SurvivesFailedRun, renamed by the serve/worker split — which
// relies on the escape firing, would be the only thing left to notice.
func TestTaskChildFailEscapeStillFiresInsideSandboxE2E(t *testing.T) {
	bin := testutil.GopiBin(t)
	// REPO-CONTEXT PIN: same reason as the refusal half above — a real `gopi
	// worker` child must never inherit this process's cwd (the live repo), and
	// this rig also seeds through the CLI, so the cwd must stay a git repo.
	SandboxRepoContextInFixtureRepo(t)
	sandbox := t.TempDir() // under TMPDIR → measured off `/`
	home := filepath.Join(sandbox, "gopi-home")
	dbPath := filepath.Join(sandbox, "memory.db")
	if err := os.MkdirAll(home, 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", home, err)
	}
	env := sandboxedProfileEnv(t, home, dbPath)

	if out, code := runGopiIn(t, bin, env, "", CliVerbTask, "submit",
		"escape-inside-sandbox pairing", "--payload", "synthetic child, no model", "--task-id", tEscFireRow); code != 0 {
		t.Fatalf("submit: exit=%d\n%s", code, out)
	}
	out, code := runGopiIn(t, bin, env, "", CliVerbWorker, workerVerbArgv)
	if code != 0 {
		t.Fatalf("a sandboxed worker round must survive the synthetic child failure (exit 0), got exit=%d\n%s", code, out)
	}
	if strings.Contains(out, "REFUSED") {
		t.Fatalf("the escape was refused INSIDE a sandbox — the guard is too wide:\n%s", out)
	}
	// The escape did what it exists for: the row failed WITHOUT a model call.
	if st := taskStatusIn(t, dbPath, tEscFireRow); st != "failed" {
		t.Fatalf("row status = %q, want failed (the escape must still fire inside a sandbox)", st)
	}
	if !taskEventDetailContainsIn(t, dbPath, tEscFireRow, taskChildFailSyntheticMarker) {
		t.Errorf("the failure detail must be the synthetic escape marker, got none for %s", tEscFireRow)
	}
}

// TestTaskChildFailSandboxPredicateMeasured is the predicate's own test: the
// sandbox question is a MEASUREMENT, and the two answers it must give on this
// host are pinned here (Rule 10.4: the smallest sample, at the boundary).
func TestTaskChildFailSandboxPredicateMeasured(t *testing.T) {
	// ① a throwaway dir under the temp root IS a sandbox (t.TempDir's shape).
	tmp := t.TempDir()
	if !pathInAnyRoot(tmp, taskChildFailTempRoots()) {
		t.Errorf("t.TempDir() %q must read as inside a temp root — the whole test suite depends on it", tmp)
	}
	// ② the operator's REAL profile is NOT a sandbox, even though /home is a
	//    DIFFERENT device from `/` on this host (the false negative that makes
	//    a device-only predicate useless).
	home, err := os.UserHomeDir()
	if err != nil {
		t.Skipf("no home dir to probe: %v", err)
	}
	realLedger := filepath.Join(home, config.ProfileDirName, config.MemoryDBFileName)
	if pathInAnyRoot(realLedger, taskChildFailTempRoots()) {
		t.Errorf("the operator's real ledger %q read as inside a temp root — a sandbox would be indistinguishable from production", realLedger)
	}
	// ③ a path OUTSIDE every temp root (a repo checkout — this test's own cwd)
	//    is not a sandbox. t.TempDir() must NOT be used as the "outside" here:
	//    it IS under the temp root, which is exactly why case ① passes.
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("cwd: %v", err)
	}
	if pathInAnyRoot(cwd, taskChildFailTempRoots()) {
		t.Skipf("this checkout %q sits inside a temp root on this host, so it cannot witness the outside case", cwd)
	}
	// ④ the / partition is never a sandbox, whatever TMPDIR says.
	if ok, err := scratch.IsTempRootOffRootFS(scratch.RootDeviceProbePath); err != nil {
		t.Errorf("measuring %s: %v", scratch.RootDeviceProbePath, err)
	} else if ok {
		t.Errorf("%s must never read as off-root", scratch.RootDeviceProbePath)
	}
}

// The escape's two fixture rows and the synthetic detail the exit path writes
// (checked verbatim: the detail is the incident's fingerprint).
const (
	// Full uuids, not slugs: the submit verb refuses a row id no ground name
	// can be derived from (TaskSubmitOptions.RefuseUngroundableID, armed on the
	// CLI face by epic 01a12b86). A slug here would be refused AT SUBMIT, so
	// the fixture would never reach the child-fail escape it is built to pin.
	tEscRefuseRow = "e55ec593-12ff-55f0-a851-4eb349f30d09"
	tEscFireRow   = "3b74dab2-5fa8-5a93-abea-5e1fb9347bc9"
	// taskChildFailSyntheticMarker is the incident's fingerprint: the detail the
	// escape's exit path writes.
	taskChildFailSyntheticMarker = "synthetic child failure"
	// wantTaskChildFailRefusalExit is the refusal exit code, spelled HERE as a
	// literal so this repro compiles against the PRE-FIX production tree too —
	// which is what lets it be run red before the fix (Rule 5). Its equality
	// with the shipped constant is asserted by
	// TestTaskChildFailRefusalExitCodeMatchesTheRepro in the guard's own test, so
	// the literal cannot drift away from the code it mirrors.
	wantTaskChildFailRefusalExit = 96
	// rootPartitionForRepro is the boot partition the unsandboxed fixture is
	// created on. It is spelled HERE (rather than referencing
	// scratch.RootDeviceProbePath) so this repro compiles against the PRE-FIX
	// tree — the property that lets it run red before the fix (Rule 5).
	// TestRootPartitionReproMatchesTheConstant pins it to the shipped constant.
	rootPartitionForRepro = "/"
	// unsandboxedFixtureBannedRoot is the pre-2026-09-14 temp-root spelling: the
	// directory the /tmp ban exists for, used here ONLY as a fixture target and
	// always MEASURED before use — the counterexample the ban is about is
	// exactly what makes it a valid witness for the refusal.
	//
	// It is an ALIAS of the one definition (Rule 14,
	// worktreeroots.FixtureBannedRoot — itself the fixture-role name of
	// AmbientTempRoot), so the value is not re-typed. The role is what keeps
	// the reference honest: the fixture MUST name the ban's target, never a
	// path derived from this suite's TMPDIR — deriving it would make the
	// witness follow the very configuration the refusal is meant to catch.
	unsandboxedFixtureBannedRoot = worktreeroots.FixtureBannedRoot
	// skipNoUnsandboxedFixture explains, in one literal, why this host cannot run
	// the refusal's proof: no writable path on the boot partition lies outside
	// this suite's measured temp roots (the devices are on the log line above).
	skipNoUnsandboxedFixture = "no writable directory on the boot partition outside this suite's temp roots — " +
		"this host cannot witness an unsandboxed fixture, so there is no path for the refusal to be proved against"
)

// unsandboxedFixtureParents are the directories tried in order for an on-`/`
// fixture. The first is the BANNED root itself, which is legitimate here
// precisely because the refusal it witnesses is the behaviour under test;
// os.TempDir() is the second (on a default host it IS /tmp, while this suite
// pins TMPDIR to a memory filesystem, so it usually yields nothing and the
// first candidate carries the test).
var unsandboxedFixtureParents = []string{unsandboxedFixtureBannedRoot, os.TempDir()}

// rootDeviceIDForRepro is the measured device id of the boot partition, filled
// once by init (a const block cannot call a function). Measured, never
// hardcoded (Rule 22).
var rootDeviceIDForRepro uint64

func init() {
	dev, err := deviceIDForRepro(rootPartitionForRepro)
	if err != nil {
		// Leave 0: unsandboxedProfileRoot then skips with the reason (Rule 15 —
		// an unmeasurable premise is never a silent pass).
		return
	}
	rootDeviceIDForRepro = dev
}

// tempRootsForRepro / pathInAnyTempRootForRepro MIRROR the production sandbox
// predicate's containment half INSIDE THIS FILE, on purpose: this repro must
// compile against the PRE-FIX tree so it can run red before the fix
// (Rule 5), and the pre-fix tree has no such helpers. They are used only for
// SKIP DECISIONS, never for the assertion, and the guard's own test pins them to
// the shipped predicate so the two cannot drift.
func tempRootsForRepro() []string {
	roots := []string{os.TempDir()}
	if home := scratch.HomeDir(); home != "" {
		roots = append(roots, home)
	}
	return roots
}

// pathInAnyTempRootForRepro is component-wise containment (a prefix STRING
// match would call "/scratch-other" a child of "/scratch").
func pathInAnyTempRootForRepro(path string, roots []string) bool {
	abs, err := filepath.Abs(path)
	if err != nil {
		return false
	}
	for _, root := range roots {
		if abs == root {
			return true
		}
		if rel, err := filepath.Rel(root, abs); err == nil && !strings.HasPrefix(rel, "..") {
			return true
		}
	}
	return false
}

// deviceIDForRepro measures a path's filesystem id with the kernel — declared
// locally (not via internal/scratch) so this repro compiles against the PRE-FIX
// tree. An unmeasurable path is an error, never a guess (Rule 15).
func deviceIDForRepro(path string) (uint64, error) {
	var st syscall.Statfs_t
	if err := syscall.Statfs(path, &st); err != nil {
		return 0, fmt.Errorf("statfs %s: %w", path, err)
	}
	return uint64(st.Fsid.X__val[0]), nil
}

// unsandboxedProfileRoot returns a fresh directory on the boot partition whose
// paths are NOT inside any temp root — the `mktemp -d` shape the 2026-09-16
// recipe used, and the one a naive "is it /tmp" reading calls a sandbox.
//
// The premise is MEASURED before use (Rule 22: a criterion, not a value): the
// candidate must be on the boot partition AND outside the temp roots. When
// neither candidate qualifies, this host has no path that can witness the
// refusal and the test SKIPS by name — never a silent pass (Rule 15).
func unsandboxedProfileRoot(t *testing.T) string {
	t.Helper()
	for _, parent := range unsandboxedFixtureParents {
		dev, err := deviceIDForRepro(parent)
		if err != nil {
			continue // a candidate that cannot be measured decides nothing
		}
		if dev != rootDeviceIDForRepro {
			continue // not on the boot partition — cannot witness the refusal
		}
		if pathInAnyTempRootForRepro(parent, tempRootsForRepro()) {
			continue // inside a temp root: it IS a sandbox, so it proves nothing
		}
		dir, err := os.MkdirTemp(parent, "gopi-esc-repro-")
		if err != nil {
			continue
		}
		t.Cleanup(func() {
			if err := os.RemoveAll(dir); err != nil {
				t.Errorf("remove repro dir %s: %v", dir, err)
			}
		})
		return dir
	}
	t.Logf("boot-partition device %d; temp roots %s", rootDeviceIDForRepro, strings.Join(tempRootsForRepro(), ", "))
	//stylegate:ignore the reason is a literal constant because the message names BOTH measured devices, so it cannot be a bare string here; skipNoUnsandboxedFixture carries the >=MinReasonLen explanation
	t.Skip(skipNoUnsandboxedFixture)
	return ""
}

// unsandboxedProfileEnv is the operator-shell shape: the escape armed, the
// profile and its ledger on `/`, and every other knob that would reach the
// network or a peer turned off. GOPI_SETTINGS is pointed at a nonexistent file
// (the shared sandbox fixture) so the operator's real settings.json cannot
// supply sync peers or a notify sink.
func unsandboxedProfileEnv(t *testing.T, home, dbPath, outside string) []string {
	t.Helper()
	return stripAgentSignature(guardTestEnv(t, home)(
		config.MemoryDBEnv+"="+dbPath,
		config.SettingsEnv+"="+filepath.Join(outside, "nonexistent.json"),
		"GOPI_COMPUTE_NODES=",
		config.SyncPeersEnv+"=",
		taskChildFailTestEnv+"=1",
		workerIdleExitsEnv+"=1",
		workerIdleIntervalEnv+"=1",
	))
}

// sandboxedProfileEnv is the hermetic shape: a throwaway GOPI_HOME and a ledger
// inside it, both under the measured temp root, with the escape armed.
func sandboxedProfileEnv(t *testing.T, home, dbPath string) []string {
	t.Helper()
	return stripAgentSignature(guardTestEnv(t, home)(
		config.MemoryDBEnv+"="+dbPath,
		config.SettingsEnv+"="+filepath.Join(home, "nonexistent.json"),
		"GOPI_COMPUTE_NODES=",
		config.SyncPeersEnv+"=",
		taskChildFailTestEnv+"=1",
		workerIdleExitsEnv+"=1",
		workerIdleIntervalEnv+"=1",
	))
}

// taskStatusIn reads one row's status straight out of a ledger file — the
// before/after evidence, independent of the binary under test.
func taskStatusIn(t *testing.T, dbPath, taskID string) string {
	t.Helper()
	db := openLedger(t, dbPath)
	var st string
	if err := db.QueryRow(`SELECT status FROM tasks WHERE task_id = ?`, taskID).Scan(&st); err != nil {
		t.Fatalf("read status of %s from %s: %v", taskID, dbPath, err)
	}
	return st
}

// taskEventsIn counts the event rows of one task.
func taskEventsIn(t *testing.T, dbPath, taskID string) int {
	t.Helper()
	db := openLedger(t, dbPath)
	var n int
	if err := db.QueryRow(`SELECT COUNT(*) FROM task_events WHERE task_id = ?`, taskID).Scan(&n); err != nil {
		t.Fatalf("count events of %s: %v", taskID, err)
	}
	return n
}

// taskEventDetailContainsIn reports whether any event of the row carries needle.
func taskEventDetailContainsIn(t *testing.T, dbPath, taskID, needle string) bool {
	t.Helper()
	db := openLedger(t, dbPath)
	var n int
	if err := db.QueryRow(
		`SELECT COUNT(*) FROM task_events WHERE task_id = ? AND detail LIKE ?`,
		taskID, "%"+needle+"%").Scan(&n); err != nil {
		t.Fatalf("search events of %s: %v", taskID, err)
	}
	return n > 0
}

// openLedger opens a sandbox ledger read-only for assertions.
//
// The DSN comes from tools.LedgerReadOnlyDSN, never a bare path + "?mode=ro":
// this suite starts a RESIDENT daemon, and a ledger opened without production's
// busy timeout fails instantly with SQLITE_BUSY instead of queueing behind it
// (master's ledger DSN-contract gate names this site; Rule 14 — one definition,
// referenced, never re-typed at a call site).
func openLedger(t *testing.T, dbPath string) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", tools.LedgerReadOnlyDSN(dbPath))
	if err != nil {
		t.Fatalf("open ledger %s: %v", dbPath, err)
	}
	t.Cleanup(func() { _ = db.Close() }) //stylegate:ignore a read-only assertion handle's teardown; the assertions already ran and a Close error here can change no verdict
	return db
}
