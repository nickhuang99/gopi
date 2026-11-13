package main

import (
	"github.com/nick/gopi/internal/config"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nick/gopi/internal/testutil"
	"github.com/nick/gopi/internal/tools"
)

// TestTaskRunExplicitRefusesInProgress (P0 regression — review/cli-task-core):
// `gopi task run <id>` on an ALREADY-RUNNING (in_progress) task must REFUSE
// and exit non-zero WITHOUT executing the briefing. Before the fix,
// TaskClaimExplicit's crash-recovery shape ignored RowsAffected and returned
// nil for any non-pending row, so a second runner claimed the in_progress
// row, TaskStart no-op'd, and the briefing was executed a SECOND time
// (two children racing on the same task — E2E-proven in the review).
// The mock LLM must see ZERO requests: a refusal means no child was spawned.
func TestTaskRunExplicitRefusesInProgress(t *testing.T) {
	// repo-context sandbox: the wrapper child must not inherit this process's cwd
	// — the live repository — or its own-worktree-release pass would enumerate
	// (and free) the developer's worktrees. See repo_context_sandbox_test.go.
	SandboxRepoContext(t)
	mock := testutil.NewMockLLM(t, testutil.Resp{Text: "must never be reached"})
	defer mock.Close()

	gopiHome := t.TempDir()
	dbPath := filepath.Join(t.TempDir(), "memory.db")
	t.Setenv("GOPI_MEMORY_DB", dbPath) // parent-process tools.* calls below
	env := testutil.MockEnv(t, gopiHome, mock.URL())
	env = append(env,
		"GOPI_MEMORY_DB="+dbPath,
		config.SkipClaimBroadcastEnv+"=1",
	)

	// Seed a row and drive it to in_progress under "runner-1" — exactly the
	// shape a first `task run` leaves behind mid-execution.
	if _, err := tools.TaskSubmit(tools.TaskSubmitOptions{
		Summary: "double-exec victim", Payload: "real briefing payload", TaskID: "t-de-victim",
	}); err != nil {
		t.Fatalf("submit: %v", err)
	}
	res, err := tools.TaskNext("runner-1", "")
	if err != nil || res.Empty {
		t.Fatalf("first claim: %v empty=%v", err, res.Empty)
	}
	if _, err := tools.TaskStart("t-de-victim", "runner-1"); err != nil {
		t.Fatalf("first start: %v", err)
	}

	// A SECOND `gopi task run t-de-victim` must refuse at the claim gate:
	// non-zero exit, refusal on stderr naming the in_progress status, and
	// NO briefing execution (no child spawn → zero mock LLM requests).
	stdout, stderr, code := runGopiMapped(t, []string{
		"worker", "t-de-victim", "--as", "runner-2",
	}, "", env, "")
	if code == 0 {
		t.Fatalf("second task run must exit non-zero\nstdout:\n%s\nstderr:\n%s", stdout, stderr)
	}
	if !strings.Contains(stderr, "in_progress") || !strings.Contains(stderr, "runner-1") {
		t.Errorf("refusal must name status+holder, stderr:\n%s", stderr)
	}
	if mock.RequestCount() != 0 {
		t.Errorf("briefing must NOT execute on refusal — mock saw %d LLM request(s)", mock.RequestCount())
	}

	// The row is untouched: still in_progress under the original holder.
	show, _, _ := runGopiMapped(t, []string{"task", "show", "t-de-victim"}, "", env, "")
	if !strings.Contains(show, "in_progress") || !strings.Contains(show, "runner-1") {
		t.Errorf("in_progress row must be untouched by the refused run:\n%s", show)
	}
}

// TestTaskRunExplicitResumeClaimedStillWorks: the legitimate crash-recovery
// path is preserved — `task run <id>` on a CLAIMED row (holder died between
// the CAS and TaskStart) resumes it, executes, and writes back. This pins
// the acceptance criterion that the RowsAffected fix did not break resume.
func TestTaskRunExplicitResumeClaimedStillWorks(t *testing.T) {
	// repo-context sandbox: the wrapper child must not inherit this process's cwd
	// — the live repository — or its own-worktree-release pass would enumerate
	// (and free) the developer's worktrees. See repo_context_sandbox_test.go.
	SandboxRepoContext(t)
	mock := testutil.NewMockLLM(t, testutil.Resp{Text: "resumed and done"})
	defer mock.Close()

	gopiHome := t.TempDir()
	dbPath := filepath.Join(t.TempDir(), "memory.db")
	t.Setenv("GOPI_MEMORY_DB", dbPath) // parent-process tools.* calls below
	env := testutil.MockEnv(t, gopiHome, mock.URL())
	env = append(env,
		"GOPI_MEMORY_DB="+dbPath,
		config.SkipClaimBroadcastEnv+"=1",
	)

	if _, err := tools.TaskSubmit(tools.TaskSubmitOptions{
		Summary: "crashed claimer victim", Payload: "brief me", TaskID: "t-de-resume",
	}); err != nil {
		t.Fatalf("submit: %v", err)
	}
	// A claimer died right after the Phase-1 CAS: row is claimed, never
	// started.
	if _, err := tools.TaskNext("dead-holder:999", ""); err != nil {
		t.Fatalf("claim: %v", err)
	}

	// `task run <id>` resumes the claimed row (crash recovery), the child
	// executes against the mock, and the writeback closes the row.
	stdout, stderr, code := runGopiMapped(t, []string{
		"worker", "t-de-resume", "--as", "recovery:2",
	}, "", env, "")
	if code != 0 {
		t.Fatalf("resume run must succeed: code=%d\nstdout:\n%s\nstderr:\n%s", code, stdout, stderr)
	}
	if mock.RequestCount() == 0 {
		t.Errorf("resume run must actually execute the briefing (mock saw no requests)")
	}
	// Writeback closes the row out of claimed/in_progress.
	show, _, _ := runGopiMapped(t, []string{"task", "show", "t-de-resume"}, "", env, "")
	if !strings.Contains(show, "completed") {
		t.Errorf("resumed+run row must end completed:\n%s", show)
	}
}
