package main

// WORKTREE-RESIDUE CLOSE-OUT COVERAGE (epic: "git worktree not removed",
// 2026-09-15).
//
// The residue this file guards against is not a missing mechanism — every
// producer path already had a release call — but a release that exists on the
// HAPPY PATH ONLY. Two measured holes, both closed in this branch:
//
//	① `gopi task run`'s FAILED returns (both the hermetic test escape and the
//	   real failed-child path) walked past the reclaim + cwd-release tail that
//	   sat only next to the success return. A failed run therefore kept its
//	   row's worktree registered — the survivor shape of the attachment.
//	② internal/agent's END HOOK ran the cwd release after the early returns
//	   (`!autoTaskRegistered || SessionLog == nil`, and a failed ledger close),
//	   so a child that reached the end hook but bailed there stayed IN the
//	   worktree, vetoing its removal for the rest of the process's life.
//
// ① is proven at RUNTIME here (real ledger, real git, the real wrapper
// function) and fails on the unfixed tree. ②'s action cannot be observed
// in-process — internal/worktreerelease deliberately skips compiled test
// binaries, because moving a test harness's cwd is a hijack rather than a
// release (see the package's ReleaseReporting doc) — so ② is pinned
// STRUCTURALLY: the release is a DEFER in the end hook, which is what makes it
// cover every return path. The structural half is the regression guard (the
// project's own precedent: the literal gates in internal/standards).

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/nick/gopi/internal/config"
	"github.com/nick/gopi/internal/testutil"
	"github.com/nick/gopi/internal/tools"
)

// TestTaskRunFailedChildReleasesOwnWorktree drives the REAL failed-return path
// of the `task run` wrapper and asserts its close-out tail ran: the run's own
// worktree is gone from disk AND from `git worktree list`, while the branch
// (the deliverable) survives byte-identical.
//
// It is an A/B-able acceptance: with the tail removed from the failure return
// the directory and its registration both survive, which is exactly the
// residue the epic reports.
func TestTaskRunFailedChildReleasesOwnWorktree(t *testing.T) {
	// repo-context sandbox: the wrapper's close-out runs the producer release,
	// which unions the LEDGER repos with the repo of THIS process's cwd — so
	// without the pin this test also enumerates the developer's live repository
	// (its own assertions are about the fixture repo, which the ledger still
	// names). See repo_context_sandbox_test.go.
	SandboxRepoContext(t)
	releaseLedger(t)
	f := ownReleaseFixture(t)
	_ = ownReleaseStamp(t) // skips when this host cannot render a host:pid claim stamp

	// The fixture's OWN row is CLAIMABLE (pending) and its deliverable already
	// names the worktree — so the wrapper's own claim stamps claimed_by to THIS
	// process, which is exactly what makes the row's worktree this process's to
	// release. Asserted rather than assumed.
	row := releaseClaimableRow(t, "failpath", f.repo, f.b1, f.wt1)
	if !branchListedInWorktrees(t, f.repo, f.b1) {
		t.Fatalf("fixture precondition: the branch must be registered before the run:\n%s", worktreeListOf(t, f.repo))
	}

	// The wrapper's cwd contract needs a gopi dir (taskWorkdirEnter), and the
	// hermetic child escape keeps the run model-free — the failure is the real
	// failed-child path's writeback + error return, never a model call.
	home := t.TempDir()
	t.Setenv(config.GopiDirEnv, home)
	// The ledger is already INSIDE a measured temp root: releaseLedger(t) above
	// pinned it to a t.TempDir file, which is what the escape's sandbox
	// precondition measures — so it admits this fixture without a second pin.
	// (Re-pointing it HERE would break the fixture instead: the row seeded above
	// must stay resolvable, and a fresh ledger path after the seed is a different
	// database.) See the 2026-09-16 launcher note in the guard's devdoc.
	t.Setenv(taskChildFailTestEnv, "1")

	opts := &tasksOptions{taskID: row}
	err := runTaskRunOnce(opts)
	if err == nil {
		t.Fatalf("the hermetic child failure must surface as an error from the wrapper")
	}
	if !strings.Contains(err.Error(), "FAILED (child:") {
		t.Fatalf("the run must fail through the child path, got: %v", err)
	}

	// ① THE ARM: the failed run's own directory is GONE.
	if _, serr := os.Stat(f.wt1); !os.IsNotExist(serr) {
		t.Fatalf("a FAILED `task run` must still free its own worktree (stat err=%v) — the release tail is missing from the failure return", serr)
	}
	// …and so is its registration: gate [c] greps that list, so a stale
	// registration withholds the finished branch exactly as a live directory
	// does.
	if branchListedInWorktrees(t, f.repo, f.b1) {
		t.Fatalf("the failed run's registration must be gone:\n%s", worktreeListOf(t, f.repo))
	}
	// ② the deliverable is never collateral: the branch tip is byte-identical.
	if now := releaseGit(t, f.repo, "rev-parse", f.b1); now != f.b1Tip {
		t.Fatalf("the branch tip must survive the failed run's release: %s → %s", f.b1Tip, now)
	}
	// ③ the row's terminal state is still the failure the child produced —
	// the release is a side effect and must never rewrite the outcome.
	if st := taskStatusOf(t, row); st != tools.TaskStatusFailed {
		t.Fatalf("the row must stay failed after the release tail, got %q", st)
	}
}

// releaseClaimableRow submits a row whose deliverable ALREADY names its
// worktree and leaves it PENDING: the wrapper's own claim (not the fixture's)
// then stamps claimed_by with this process's host:pid, and leaving the row
// pending keeps the arm honest — the release is driven by the run's claim, not
// by a fixture that pre-stamped ownership.
func releaseClaimableRow(t *testing.T, taskID, repo, branch, wt string) string {
	t.Helper()
	row := "rig-own-release-" + taskID
	if _, err := tools.TaskSubmit(tools.TaskSubmitOptions{
		TaskID:  row,
		Summary: "failed-run release coverage",
		Payload: "fixture row — driven through the wrapper's real failed return",
		Source:  "human",
		Repo:    repo,
	}); err != nil {
		t.Fatalf("submit claimable row: %v", err)
	}
	// The SAME backfill seam the executor child uses, so the deliverable names
	// the directory the run is expected to free.
	func() {
		defer testutil.SnapshotEnv(t, tools.TaskWrapperEnv)()
		// env-leak-ok: reviewed SnapshotEnv pair — per-call flip, undone on return.
		if err := os.Setenv(tools.TaskWrapperEnv, row); err != nil { //stylegate:ignore the flip must undo when THIS CALL returns, not at test cleanup; SnapshotEnv restores it (absence included) // env-leak-ok: SnapshotEnv restores on return, absence included
			t.Fatalf("set %s: %v", tools.TaskWrapperEnv, err)
		}
		tools.TaskDeliverableWorktreeBash(wt, "git worktree add "+wt+" -b "+branch+" master")
	}()
	if gotWT := taskDeliverableWorktreePath(t, row); gotWT != wt {
		t.Fatalf("the backfill seam must stamp the row's worktree: got %q, want %q", gotWT, wt)
	}
	return row
}

// TestTaskRunReleaseIsDeferredInTheEndHook pins ② structurally: the executor's
// cwd release must be a DEFER at the top of the end hook, so the hook's early
// returns cannot skip it. A direct (un-deferred) call is the shape that left
// children standing in the worktree — the assertion names the function, so a
// future refactor that moves the call back below the gates fails here.
func TestTaskRunReleaseIsDeferredInTheEndHook(t *testing.T) {
	const endHook = "finishSessionTaskRecorded"
	path := endHookSourcePath(t)
	fset := token.NewFileSet()
	file, perr := parser.ParseFile(fset, path, nil, 0)
	if perr != nil {
		t.Fatalf("parse %s: %v", path, perr)
	}
	fn := findFuncDecl(file, endHook)
	if fn == nil {
		t.Fatalf("%s not found in %s — the guard's anchor moved, re-point it", endHook, path)
	}
	var deferred, direct bool
	ast.Inspect(fn, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		if !releaseReportingCall(call) {
			return true
		}
		if enclosingDefer(fn, call) {
			deferred = true
		} else {
			direct = true
		}
		return true
	})
	if !deferred {
		t.Errorf("%s must DEFER worktreerelease.ReleaseReporting — a direct call is skipped by the hook's early returns, which is how a child stays standing in its worktree and vetoes its removal", endHook)
	}
	if direct {
		t.Errorf("%s must have exactly ONE release spelling: the deferred one (a second, direct call re-opens the coverage hole it closes)", endHook)
	}
}

// endHookSourcePath resolves internal/agent/taskregister.go from the
// COMPILER-EMBEDDED source root rather than from the process cwd: the
// cmd/agent suite contains tests that deliberately move the process cwd (the
// worktree-release rig among them), so a relative path here would fail or
// silently parse the wrong tree depending on test order. The embedded root is
// stable for the whole binary.
func endHookSourcePath(t *testing.T) string {
	t.Helper()
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Skip("cannot resolve this test file's path — runtime.Caller unavailable")
	}
	repoRoot := filepath.Dir(filepath.Dir(filepath.Dir(thisFile))) // cmd/agent/x_test.go → repo root
	return filepath.Join(repoRoot, "internal", "agent", "taskregister.go")
}

// findFuncDecl returns the named function declaration, or nil.
func findFuncDecl(file *ast.File, name string) *ast.FuncDecl {
	for _, d := range file.Decls {
		if fd, ok := d.(*ast.FuncDecl); ok && fd.Name != nil && fd.Name.Name == name {
			return fd
		}
	}
	return nil
}

// releaseReportingCall reports whether the call is `(<pkg>.)ReleaseReporting(...)`.
func releaseReportingCall(call *ast.CallExpr) bool {
	sel, ok := call.Fun.(*ast.SelectorExpr)
	return ok && sel.Sel != nil && sel.Sel.Name == "ReleaseReporting"
}

// enclosingDefer reports whether the call is the CALL of a deferred statement
// (the `defer f(...)` form) inside fn — the property that makes the release
// cover every return path.
func enclosingDefer(fn *ast.FuncDecl, target *ast.CallExpr) bool {
	found := false
	ast.Inspect(fn, func(n ast.Node) bool {
		if found {
			return false
		}
		d, ok := n.(*ast.DeferStmt)
		if !ok {
			return true
		}
		// DeferStmt.Call IS the deferred call expression (not a wrapper around
		// it): the node identity comparison is the whole test.
		if d.Call == target {
			found = true
			return false
		}
		return true
	})
	return found
}
