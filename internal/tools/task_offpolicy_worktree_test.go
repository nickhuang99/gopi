// Regression guard (Rule 5) for the off-policy worktree detector: a linked
// worktree created OUTSIDE the home workspace (PROJECT.md Rule 6a) must be
// reported, never silently accepted.
//
// Why this exists (2026-09-13 sweep, 66 registered worktrees): 46 of the 66
// trees sat under /tmp or /dev/shm. Nothing sweeps those roots (the server
// finish-cleanup and MergedCleanupSweep both resolve candidates through the
// ledger + the home workspace), and most carried no ledger row at all — an
// agent had typed `git worktree add /dev/shm/gopi-<x> …` and moved on. The
// reclaim sweeps can only release a tree they can ATTRIBUTE, so the fix is
// visibility at the moment of detection: the deliverable.worktree backfill
// seam now flags a non-workspace location.
package tools

import (
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestOffPolicyWorktreePathDecision pins the predicate on both sides — the
// workspace root and everything under it are compliant; /tmp, /dev/shm and
// the home root itself are not. The HOME anchor is real (t.Setenv on HOME is
// not possible — os.UserHomeDir reads the process env — so the test asserts
// through the live home and compares against it).
func TestOffPolicyWorktreePathDecision(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		t.Skipf("no home dir in this environment: %v", err)
	}
	root := filepath.Join(home, taskWorkspaceDirName)

	compliant := []string{
		filepath.Join(root, "gopi-fix-something"),
		filepath.Join(root, "gopi-fix-something", "internal", "tools"),
		root,
	}
	for _, p := range compliant {
		if got, off := taskOffPolicyWorktreePath(p); off {
			t.Errorf("workspace path %q must be compliant, got offender %q", p, got)
		}
	}

	offending := []string{
		"/tmp/gopi-rg-master",
		"/dev/shm/gopi-bisect",
		"/var/tmp/gopi-x",
		home, // the home root itself is not the workspace
	}
	for _, p := range offending {
		got, off := taskOffPolicyWorktreePath(p)
		if !off {
			t.Errorf("off-policy path %q must be reported", p)
			continue
		}
		if got != p {
			t.Errorf("offender must be echoed verbatim: got %q want %q", got, p)
		}
	}

	// Empty input is never an offender (nothing to judge).
	if _, off := taskOffPolicyWorktreePath("   "); off {
		t.Error("an empty path must not be reported as off-policy")
	}
}

// TestOffPolicyWorktreePathPrefixTrap guards the separator trap: a sibling
// directory whose name merely STARTS with the workspace name
// (<home>/workspace-old) is NOT inside the workspace root and must be
// reported — filepath.Rel alone would have accepted it under a prefix match.
func TestOffPolicyWorktreePathPrefixTrap(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		t.Skipf("no home dir in this environment: %v", err)
	}
	sibling := filepath.Join(home, taskWorkspaceDirName+"-backup", "gopi-x")
	if _, off := taskOffPolicyWorktreePath(sibling); !off {
		t.Errorf("%q is a SIBLING of the workspace root, not inside it — must be reported", sibling)
	}
}

// TestTaskDeliverableWorktreeBashReportsOffPolicyTree drives the REAL
// production seam end to end: a wrapper row whose executor created its linked
// worktree under <home>/tmp (the /tmp-shape violation, in-fixture so no real
// /dev/shm tree is touched), then asserts the seam emits the off-policy line
// while still stamping the row's deliverable (detection must not break the
// information closure it rides on).
//
// This is the wiring proof: the predicate test above would stay green with the
// call site deleted, so it cannot be the only guard.
func TestTaskDeliverableWorktreeBashReportsOffPolicyTree(t *testing.T) {
	f := newDeliverableWTFixture(t)
	db := filepath.Join(t.TempDir(), "memory.db")
	t.Setenv(memoryDBEnv, db)
	// HOME = the fixture root, so the workspace root is <root>/workspace and
	// <root>/tmp/... is the off-policy location.
	t.Setenv("HOME", f.root)
	taskID := "t-wt-offpolicy"
	deliverableWTSeedWrapperRow(t, db, taskID, f.clientDir)
	t.Setenv(TaskWrapperEnv, taskID)
	defer t.Setenv(TaskWrapperEnv, "")

	offWT := filepath.Join(f.root, "tmp", "gopi-offpolicy")
	offBranch := "feat/offpolicy-tree"
	if _, err := os.Stat(offWT); err == nil {
		deliverableWTGit(t, f.clientDir, "worktree", "remove", offWT)
	}
	os.MkdirAll(filepath.Dir(offWT), 0o755)
	deliverableWTGit(t, f.clientDir, "worktree", "add", "-b", offBranch, offWT, "master")

	// Capture stderr for the duration of the seam call (taskNotifyLog writes
	// it there unconditionally).
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	prev := os.Stderr
	os.Stderr = w
	TaskDeliverableWorktreeBash(offWT, "git -C "+offWT+" status")
	if err := w.Close(); err != nil {
		t.Fatalf("close write end: %v", err)
	}
	os.Stderr = prev
	out, err := io.ReadAll(r)
	if err != nil {
		t.Fatalf("read captured stderr: %v", err)
	}
	if err := r.Close(); err != nil {
		t.Fatalf("close read end: %v", err)
	}

	got := string(out)
	if !strings.Contains(got, taskOffPolicyEvent) {
		t.Errorf("off-policy worktree must be REPORTED (Rule 15, no silent failure):\n%s", got)
	}
	if !strings.Contains(got, offWT) {
		t.Errorf("the report must name the offending path %q:\n%s", offWT, got)
	}
	if !strings.Contains(got, offBranch) {
		t.Errorf("the report must name the branch %q:\n%s", offBranch, got)
	}

	// The violation is REPORTED, not "fixed" by dropping the record: the row
	// must still carry the worktree the sweeps will look for.
	var d struct {
		Branch   string `json:"branch"`
		Worktree string `json:"worktree"`
	}
	if err := json.Unmarshal([]byte(deliverableWTRowBlob(t, db, taskID)), &d); err != nil {
		t.Fatalf("decode blob: %v", err)
	}
	if d.Worktree != offWT || d.Branch != offBranch {
		t.Fatalf("detection must not break the backfill: worktree=%q branch=%q", d.Worktree, d.Branch)
	}
}

// TestTaskDeliverableWorktreeBashSilentForCompliantTree is the negative
// control: a workspace-located worktree (Rule 6a compliant) must produce NO
// off-policy line — otherwise the alarm is noise and gets ignored.
func TestTaskDeliverableWorktreeBashSilentForCompliantTree(t *testing.T) {
	f := newDeliverableWTFixture(t)
	db := filepath.Join(t.TempDir(), "memory.db")
	t.Setenv(memoryDBEnv, db)
	t.Setenv("HOME", f.root)
	taskID := "t-wt-compliant"
	deliverableWTSeedWrapperRow(t, db, taskID, f.clientDir)
	t.Setenv(TaskWrapperEnv, taskID)
	defer t.Setenv(TaskWrapperEnv, "")

	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	prev := os.Stderr
	os.Stderr = w
	TaskDeliverableWorktreeBash(f.wtDir, "git -C "+f.wtDir+" status")
	if err := w.Close(); err != nil {
		t.Fatalf("close write end: %v", err)
	}
	os.Stderr = prev
	out, err := io.ReadAll(r)
	if err != nil {
		t.Fatalf("read captured stderr: %v", err)
	}
	if err := r.Close(); err != nil {
		t.Fatalf("close read end: %v", err)
	}
	if strings.Contains(string(out), taskOffPolicyEvent) {
		t.Errorf("a workspace worktree must NOT be reported as off-policy:\n%s", string(out))
	}
}
