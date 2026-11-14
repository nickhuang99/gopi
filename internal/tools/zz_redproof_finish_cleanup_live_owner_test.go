package tools

// RED PROOF for the 2026-09-21/22 incident, second arm: the finish-cleanup
// chain removed `~/workspace/gopi-stash-chain` (branch
// fix/stash-guard-compound-chain) while two `in_progress` rows still owned that
// branch and a variant build was still running.
//
// The arm's standing for the removal was `sync.OK()` — "delivery chain's local
// sync landed: branch already contains the integration branch" — a statement
// about a MERGE read as a statement about the WORK.
//
// This file is written against the PRE-FIX API ONLY so it compiles and runs on
// both trees:
//
//	pre-fix tree (master bf36bf69): FAILS — the branch is named by an
//	  `in_progress` row whose holder (this process) is alive, and the pass
//	  offers to free the directory anyway.
//	post-fix tree: PASSES — the standing is a completion fact about the WORK,
//	  so the live owner is refused BY NAME and nothing is offered.
//
// Reproduce:
//
//	git worktree add --detach /tmp/gopi-redproof2 bf36bf69
//	cp internal/tools/zz_redproof_finish_cleanup_live_owner_test.go /tmp/gopi-redproof2/internal/tools/
//	cd /tmp/gopi-redproof2 && go test -run TestZZRedProofFinishCleanupKeepsALiveOwnersBranchGround ./internal/tools/

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nickhuang99/gopi/internal/config"
)

// TestZZRedProofFinishCleanupKeepsALiveOwnersBranchGround builds the incident's
// ledger shape — a TERMINAL row whose deliverable names the directory, plus a
// LIVE row that names the same BRANCH — and runs the finish-cleanup arm's own
// removal decision (finishCleanupRemovalStateReason, the predicate that decides
// whether the removal may be planned at all).
//
// The live row deliberately records NO proc identity and NO claim pid: it is
// this process's own work, described the way a run describes a directory it is
// standing in. The pre-fix arm frees it because nothing in that predicate asks
// about the branch's other owners.
func TestZZRedProofFinishCleanupKeepsALiveOwnersBranchGround(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	t.Setenv(memoryDBEnv, filepath.Join(t.TempDir(), "memory.db"))
	settings := filepath.Join(t.TempDir(), "settings.json")
	if err := os.WriteFile(settings, []byte(`{"sync_peers":[]}`), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv(config.SettingsEnv, settings)
	t.Setenv(config.SyncPeersEnv, "")

	root := t.TempDir()
	repo := filepath.Join(root, "repo")
	wt := filepath.Join(root, "wt")
	branch := "fix/redproof-stash-chain"
	gitRun(t, root, "init", repo)
	gitRun(t, repo, "config", "user.email", "t@t")
	gitRun(t, repo, "config", "user.name", "t")
	gitRun(t, repo, "checkout", "-b", "master")
	if err := os.WriteFile(filepath.Join(repo, "a.txt"), []byte("base"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitRun(t, repo, "add", "a.txt")
	gitRun(t, repo, "commit", "-m", "init")
	gitRun(t, repo, "checkout", "-b", branch)
	if err := os.WriteFile(filepath.Join(repo, "deliverable.txt"), []byte("the delivery"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitRun(t, repo, "add", "deliverable.txt")
	gitRun(t, repo, "commit", "-m", "the deliverable")
	gitRun(t, repo, "checkout", "master")
	gitRun(t, repo, finishCleanupWorktreeVerb, "add", wt, branch)

	// The TERMINAL row this close-out is about: its deliverable names the
	// directory and the branch.
	if _, err := TaskSubmit(TaskSubmitOptions{Summary: "closing row", Payload: "p", TaskID: "t-redproof-closing", Source: "human"}); err != nil {
		t.Fatalf("submit closing row: %v", err)
	}
	// The LIVE row that still owns the same BRANCH (the incident had two).
	if _, err := TaskSubmit(TaskSubmitOptions{Summary: "live owner", Payload: "p", TaskID: "t-redproof-live-owner", Source: "human"}); err != nil {
		t.Fatalf("submit live row: %v", err)
	}
	ctx := context.Background()
	db := openTestTasksDB(t, memoryDBPath())
	stamp := TaskClaimOwner("")
	if stamp == "" {
		t.Skip("no hostname on this machine — the live-owner shape is untestable here")
	}
	if _, err := db.ExecContext(ctx,
		`UPDATE tasks SET status = ?, deliverable = ?
		  WHERE task_id = ?`,
		taskStatusCompleted,
		`{"repo":"`+repo+`","branch":"`+branch+`","worktree":"`+wt+`"}`,
		"t-redproof-closing"); err != nil {
		db.Close() //stylegate:ignore process-lifetime handle closed at shutdown; the OS reclaims the descriptor
		t.Fatalf("stamp closing row: %v", err)
	}
	if _, err := db.ExecContext(ctx,
		`UPDATE tasks SET status = ?, claimed_by = ?, claimed_at = ?, `+tasksInProgressAtColumnName+
			` = ?, updated_at = ?, deliverable = ? WHERE task_id = ?`,
		taskStatusInProgress, stamp, 1000.0, 2000.0, 2000.0,
		`{"repo":"`+repo+`","branch":"`+branch+`"}`, "t-redproof-live-owner"); err != nil {
		db.Close() //stylegate:ignore process-lifetime handle closed at shutdown; the OS reclaims the descriptor
		t.Fatalf("stamp live row: %v", err)
	}
	db.Close() //stylegate:ignore process-lifetime handle closed at shutdown; the OS reclaims the descriptor

	reason := finishCleanupRemovalStateReason(ctx, &FinishCleanupReport{}, wt)
	if reason == "" {
		t.Fatalf("the finish-cleanup arm offered no refusal for a directory a LIVE row's branch owns — " +
			"the removal would stand on \"the delivery chain's local sync landed\", which is a statement about a MERGE, not about the WORK")
	}
	if !strings.Contains(reason, "t-redproof-live-owner") {
		t.Errorf("the refusal must NAME the live owner row, got %q", reason)
	}
	if _, err := os.Stat(wt); err != nil {
		t.Fatalf("the fixture directory must still be there (this predicate refuses, it never removes): %v", err)
	}
}
