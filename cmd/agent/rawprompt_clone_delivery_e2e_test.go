package main

// rawprompt_clone_delivery_e2e_test.go — the delivery LEG on the real binary.
//
// THE GAP (epic raw-prompt-clone-channel): a bare prompt with a declared
// `dev_repo` takes the clone channel, works and COMMITS in
// <workspace_root>/<task-id>, and used to leave those commits there — the
// channel's delivery loop is shaped for a job that can spawn agent rounds, and a
// bare prompt IS the agent. internal/agent/clonedelivery.go is the leg that
// closes it, and this file is its end-to-end proof on the shipped binary:
//
//	BEFORE the run: the source repo's master tip and its status.
//	RUN: a raw prompt that commits a real change in the clone workspace.
//	AFTER the run: the source repo's master tip has MOVED (the push landed) and
//	the operator's stderr says so.
//
// It asserts the two facts an operator actually needs — the code arrived, and the
// run said which way it went — and it reads them from the SOURCE REPO and the
// process's own stream, never from the workspace (whose fate is a different
// question).

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nick/gopi/internal/config"
	"github.com/nick/gopi/internal/testutil"
	"github.com/nick/gopi/internal/toolnames"
)

// TestRawPromptCloneChannelDeliversItsCommits is the acceptance walk.
func TestRawPromptCloneChannelDeliversItsCommits(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	bin := testutil.GopiBin(t)
	if _, err := os.Stat(bin); err != nil {
		t.Skipf("gopi binary unavailable: %v", err)
	}
	assertGopiBinIsFresh(t, bin)

	repo := filepath.Join(t.TempDir(), "repo")
	if err := os.MkdirAll(repo, 0o755); err != nil {
		t.Fatal(err)
	}
	// receive.denyCurrentBranch=ignore: the source repo is a NON-BARE local
	// checkout standing in for the remote (the shape the channel's other e2e
	// fixtures use), so a push into it is accepted.
	rawPromptGit(t, repo, "init", "-q", "-b", "master")
	rawPromptGit(t, repo, "config", "receive.denyCurrentBranch", "ignore")
	if err := os.WriteFile(filepath.Join(repo, "a.txt"), []byte("hello\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	rawPromptGit(t, repo, "add", "a.txt")
	rawPromptGit(t, repo, "commit", "-q", "-m", "init")

	before := rawPromptGit(t, repo, "log", "--oneline", "-1")
	beforeStatus := rawPromptGit(t, repo, "status", "--porcelain")
	t.Logf("BEFORE: master tip=%q status=%q", before, beforeStatus)

	gopiHome := t.TempDir()
	dbPath := filepath.Join(t.TempDir(), "memory.db")
	root := filepath.Join(t.TempDir(), "workspace")
	settingsPath := filepath.Join(t.TempDir(), "settings.json")
	// The declaration lives where Rule 12 puts it: the repos table, keyed by the
	// SOURCE repo (the clone channel's own precedence). `true` is a real command
	// with a real exit status — the leg pushes nothing it did not build.
	body := `{"sync_peers":[],"dev_repo":"` + repo + `","workspace_root":"` + root +
		`","repos":{"` + repo + `":{"build":"true"}}}`
	if err := os.WriteFile(settingsPath, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}

	// The scripted executor: ONE real commit in the workspace it stands in (the
	// clone ground — the process chdir'd there), then an answer. The identity is
	// passed per-invocation so the host's global git config cannot leak in.
	mock := testutil.NewMockLLM(t,
		testutil.Resp{ToolUse: &testutil.ToolCall{ID: "call_commit", Name: toolnames.ToolBash,
			Args: map[string]interface{}{"command": "printf 'delivered by a raw prompt\\n' >> a.txt && " +
				"git add -A && git -c user.email=t@t -c user.name=t commit -q -m 'raw prompt clone delivery'"}}},
		testutil.Resp{Text: "committed in the clone workspace"},
	)
	t.Cleanup(mock.Close)

	env := testutil.MockEnv(t, gopiHome, mock.URL())
	env = append(env,
		"GOPI_MEMORY_DB="+dbPath,
		config.SettingsEnv+"="+settingsPath,
		config.SkipClaimBroadcastEnv+"=1",
		"GOPI_SKIP_TERMINAL_PUSH=1",
		"GOPI_AUTO_TASK_REGISTER=1",
		"GOPI_CRASH_DUMP_SWEEP=off",
		// The clone channel's knobs are ENV-first (config.DevRepoConfigured /
		// WorkspaceRootConfigured), and this run must not inherit the host's.
		config.DevRepoEnvName+"="+repo,
		config.WorkspaceRootEnvName+"="+root,
	)
	// cwd = a directory OUTSIDE the source repo: the run is the FACE with no
	// wrapper row, and its work must happen in the clone ground, not here.
	launchDir := t.TempDir()
	stdout, stderr, code := runGopiMapped(t, []string{"commit a small change"}, "", env, launchDir)
	t.Logf("run exit=%d\nstdout:\n%s\nstderr:\n%s", code, stdout, stderr)

	if code != 0 {
		t.Fatalf("the raw-prompt run must succeed: code=%d\nstdout:\n%s\nstderr:\n%s", code, stdout, stderr)
	}

	// ① THE DELIVERY ACTUALLY RAN, and the operator was told which way it went.
	//    Either verdict is asserted as a NAME (not a silent absence): this test
	//    exists to prove the leg is wired, so "no line at all" is the failure.
	if !strings.Contains(stderr, "clone channel: delivery delivered") &&
		!strings.Contains(stderr, "clone channel: delivery BLOCKED") &&
		!strings.Contains(stderr, "clone channel: delivery BROKEN") {
		t.Fatalf("the run must report its delivery verdict on stderr:\n%s", stderr)
	}

	// ② THE COMMIT ARRIVED AT THE SOURCE REPO. This is the fact the epic is
	//    about: a bare-prompt run's work must not be stranded in a workspace.
	after := rawPromptGit(t, repo, "log", "--oneline", "-1")
	afterStatus := rawPromptGit(t, repo, "status", "--porcelain")
	t.Logf("AFTER: master tip=%q status=%q", after, afterStatus)
	if after == before {
		t.Fatalf("the source repo's master did not move — the run's commit never reached origin\nbefore=%q after=%q\nstderr:\n%s",
			before, after, stderr)
	}
	if !strings.Contains(after, "raw prompt clone delivery") {
		t.Fatalf("the new tip must BE the run's commit, got %q\nstderr:\n%s", after, stderr)
	}
	if !strings.Contains(stderr, "delivery delivered") {
		t.Fatalf("the push landed but the leg did not say `delivered`:\n%s", stderr)
	}
}
