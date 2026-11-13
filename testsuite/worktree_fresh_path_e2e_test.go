package testsuite

// worktree_fresh_path_e2e_test.go — RULE 31 for the fresh-path guard: the
// surface is EXECUTED, not merely unit-tested.
//
// The unit tests (internal/tools/worktree_fresh_path_test.go) call the guard
// directly. This one drives the REAL bin/gopi through the REAL bash tool
// against REAL git state — the path a task actually takes — and asserts the
// observable outcome: the command is refused, the name and the remedy appear in
// the tool result the model would read, and the pre-existing directory is
// untouched. The green half runs the same invocation at a path that does not
// exist and shows the worktree is created.
//
// Nothing here needs a load generator, a device, or a network: the mock LLM
// scripts one bash tool_use, and the guard answers before git is ever reached.

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nick/gopi/internal/testutil"
	"github.com/nick/gopi/internal/worktreeroots"
)

// e2eGit runs a git command in dir, failing the test on error.
func e2eGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME=e2e", "GIT_AUTHOR_EMAIL=e2e@example.invalid",
		"GIT_COMMITTER_NAME=e2e", "GIT_COMMITTER_EMAIL=e2e@example.invalid",
	)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v in %s: %v\n%s", args, dir, err, out)
	}
}

// e2eRepo builds a one-commit throwaway repo for the invocation under test.
func e2eRepo(t *testing.T) string {
	t.Helper()
	repo := filepath.Join(t.TempDir(), "repo")
	if err := os.MkdirAll(repo, 0o755); err != nil {
		t.Fatal(err)
	}
	e2eGit(t, repo, "init", "-q", "-b", "master")
	if err := os.WriteFile(filepath.Join(repo, "README"), []byte("e2e\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	e2eGit(t, repo, "add", "README")
	e2eGit(t, repo, "commit", "-q", "-m", "fixture commit")
	return repo
}

// runBashToolThroughGopi runs ONE bash tool_use through the real binary and
// returns the tool result text the model would have seen, plus the run's exit
// code. The mock answers the tool call, then closes the task out.
func runBashToolThroughGopi(t *testing.T, command, work string, extraEnv ...string) (toolResult string, code int, stdout, stderr string) {
	t.Helper()
	home := t.TempDir()
	llm := testutil.NewMockLLM(t,
		testutil.Resp{ToolUse: &testutil.ToolCall{
			ID:   "call_bash_wt",
			Name: "bash",
			Args: map[string]interface{}{"command": command},
		}},
		testutil.Resp{Text: taskCompleteAnswer},
	)
	defer llm.Close()

	// MockEnv STRIPS every GOPI_* key (it scrubs the parent agent's own
	// environment), so the declared worktree root is appended AFTER it — a
	// t.Setenv in the test process would be removed before the child starts.
	env := append(testutil.MockEnv(t, home, llm.URL()), extraEnv...)
	stdout, stderr, code = testutil.RunGopi(t, nil, "run the command\n", env, work)
	sess := testutil.LoadSession(t, home)
	return e2eToolResultText(t, sess), code, stdout, stderr
}

// e2eToolResultText renders every tool_result block's text from a session, which
// is exactly what the MODEL read back. Asserting on the session record (rather
// than on stdout) is deliberate: stdout only proves the process printed
// something, the record proves the refusal reached the tool-result channel a
// second round would be built from.
func e2eToolResultText(t *testing.T, sess *testutil.Session) string {
	t.Helper()
	var b strings.Builder
	for _, ev := range sess.Events {
		if ev.Type != "tool_result" {
			continue
		}
		raw, err := json.Marshal(ev.Raw)
		if err != nil {
			t.Fatalf("session: re-encode %v: %v", ev.Raw, err)
		}
		b.Write(raw)
		b.WriteString("\n")
	}
	return b.String()
}

// TestE2E_BashToolRefusesWorktreeAddIntoExistingEmptyDirectory is the Rule 31
// transcript for the refusal: the pre-existing EMPTY directory is the case git
// itself lets through, so a green here is the guard reaching the live path.
func TestE2E_BashToolRefusesWorktreeAddIntoExistingEmptyDirectory(t *testing.T) {
	repo := e2eRepo(t)
	work := t.TempDir()
	target := filepath.Join(work, "declared-root", "gopi-collide")
	if err := os.MkdirAll(target, 0o755); err != nil {
		t.Fatal(err)
	}
	command := "git -C " + repo + " worktree add " + target + " -b feat/collide"

	// The target lives inside the declared worktree root, so the PRE-EXISTING
	// policy (path_policy.go's root judgement) cannot be the reason for a
	// refusal — this test isolates the fresh-path predicate.
	root := filepath.Join(work, "declared-root")

	res, code, stdout, stderr := runBashToolThroughGopi(t, command, work,
		worktreeroots.WorktreeRootEnvName+"="+root)
	if code != 0 {
		t.Fatalf("gopi exit = %d, want 0 (the refusal is a tool result, not a crash)\n--- stdout ---\n%s\n--- stderr ---\n%s", code, stdout, stderr)
	}
	t.Logf("OBSERVED tool result for %q:\n%s", command, res)
	if !strings.Contains(res, "worktree-path-not-fresh") {
		t.Errorf("the bash tool did NOT refuse the occupied path.\ncommand: %s\ntool results: %q", command, res)
	}
	if !strings.Contains(res, target) {
		t.Errorf("the refusal did not name the path %s\ntool results: %q", target, res)
	}
	if !strings.Contains(res, "choose another path") {
		t.Errorf("the refusal did not carry the remedy\ntool results: %q", res)
	}
	// The guard REFUSES; it never removes. The directory is still there and
	// still empty — nothing was checked out into it.
	entries, err := os.ReadDir(target)
	if err != nil {
		t.Fatalf("the pre-existing directory was removed: %v", err)
	}
	if len(entries) != 0 {
		t.Fatalf("the refused command still wrote %d entries into %s", len(entries), target)
	}
}

// TestE2E_BashToolAllowsWorktreeAddOnAFreshPath is the green half: the same
// invocation at an ABSENT path must go through and really create the worktree.
// Without it, "refuse everything" would pass the test above.
func TestE2E_BashToolAllowsWorktreeAddOnAFreshPath(t *testing.T) {
	repo := e2eRepo(t)
	work := t.TempDir()
	root := filepath.Join(work, "declared-root")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(root, "gopi-fresh")
	command := "git -C " + repo + " worktree add " + target + " -b feat/fresh"

	res, code, stdout, stderr := runBashToolThroughGopi(t, command, work,
		worktreeroots.WorktreeRootEnvName+"="+root)
	if code != 0 {
		t.Fatalf("gopi exit = %d\n--- stdout ---\n%s\n--- stderr ---\n%s", code, stdout, stderr)
	}
	t.Logf("OBSERVED tool result for %q:\n%s", command, res)
	if strings.Contains(res, "worktree-path-not-fresh") {
		t.Fatalf("a worktree on a FRESH path was refused:\n%s", res)
	}
	if _, err := os.Stat(filepath.Join(target, ".git")); err != nil {
		t.Fatalf("the worktree was not created at %s (tool results: %q): %v", target, res, err)
	}
}
