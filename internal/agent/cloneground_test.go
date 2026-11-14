package agent

// cloneground_test.go — the BARE-PROMPT face's clone channel, pinned on both
// arms.
//
// THE DEFECT THESE TESTS CLOSE (measured on peerbox 2026-10-23, row 01a16520):
// with settings `dev_repo` set, `echo "…" | gopi` was the ONE face that ignored
// the channel — it auto-registered a row whose deliverable was the process's
// own git work tree (tools.TaskAutoDeliverableJSON) and worked in a Rule-6a
// linked worktree of the main checkout, while `gopi worker` / `gopi task run`
// worked in a clone of `dev_repo`.
//
// The pair below is deliberately the SAME fixture with ONE variable changed
// (the channel's own env channel), so a green cannot come from anything but
// the branch under test:
//
//	channel OFF — the value is BYTE-IDENTICAL to the pre-epic one (the
//	              compatibility contract, not "close enough")
//	channel ON  — the row names the declared source repo, and the run STANDS in
//	              the clone workspace (chdir + the ground announcement), never
//	              in a linked worktree of that source.

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nickhuang99/gopi/internal/config"
	"github.com/nickhuang99/gopi/internal/testutil"
	"github.com/nickhuang99/gopi/internal/tools"
)

// cloneGroundFixtureRepo builds a minimal local git repository to clone FROM:
// one commit, on whatever branch `git init` names (master here, matched by the
// assertion helper rather than assumed by the channel).
func cloneGroundFixtureRepo(t *testing.T) string {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skipf("git not installed: the clone channel needs it")
	}
	dir := filepath.Join(t.TempDir(), "source-repo")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("create fixture dir: %v", err)
	}
	git := func(args ...string) string {
		t.Helper()
		cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
		// The fixture's git child gets its environment through
		// testutil.GitEnv: the gate (internal/standards TestGate9GitEnvRedProof,
		// Rule 13/15) forbids a fixture handing git the INHERITED repository
		// routing, because that is the class that once rewrote the real master.
		cmd.Env = testutil.GitEnv(
			"GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_SYSTEM=/dev/null",
			"GIT_AUTHOR_NAME=clone ground test", "GIT_AUTHOR_EMAIL=ground@test.invalid",
			"GIT_COMMITTER_NAME=clone ground test", "GIT_COMMITTER_EMAIL=ground@test.invalid",
		)
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
		}
		return string(out)
	}
	git("init", "-q")
	git("config", "commit.gpgsign", "false")
	if err := os.WriteFile(filepath.Join(dir, "seed.txt"), []byte("seed\n"), 0o644); err != nil {
		t.Fatalf("write fixture file: %v", err)
	}
	git("add", "seed.txt")
	git("commit", "-q", "-m", "init")
	return dir
}

// cloneGroundRestoreCWD puts the process back where it was. adoptCloneGround
// chdirs on purpose (that IS the mechanism), so every test that drives it owes
// the rest of the suite its directory back — and the restore's error is
// CHECKED, never discarded.
func cloneGroundRestoreCWD(t *testing.T, dir string) {
	t.Helper()
	t.Cleanup(func() {
		if err := os.Chdir(dir); err != nil {
			t.Errorf("restore cwd to %s: %v", dir, err)
		}
	})
}

// TestAutoTaskDeliverableFollowsTheCloneChannel is the RED→GREEN arm the epic
// asks for: the auto row's deliverable is the DECLARED source repo when the
// channel is on, and the historical value when it is off.
func TestAutoTaskDeliverableFollowsTheCloneChannel(t *testing.T) {
	src := cloneGroundFixtureRepo(t)

	t.Run("channel OFF is byte-identical to the pre-epic value", func(t *testing.T) {
		a := newAutoTaskAgent(t)
		t.Setenv(config.DevRepoEnvName, "")
		if on, _, err := config.CloneChannelConfigured(); err != nil || on {
			t.Fatalf("fixture premise: the clone channel must be OFF (on=%v err=%v)", on, err)
		}
		got, want := a.autoTaskDeliverableJSON(), tools.TaskAutoDeliverableJSON()
		if got != want {
			t.Fatalf("channel OFF: deliverable = %q, want the historical value %q byte for byte — the OFF arm IS the compatibility contract", got, want)
		}
		if got == "" {
			t.Fatalf("fixture premise: the test process must stand in a git work tree, so the historical value is not empty")
		}
	})

	t.Run("channel ON names the declared source repo", func(t *testing.T) {
		a := newAutoTaskAgent(t)
		t.Setenv(config.DevRepoEnvName, src)
		t.Setenv(config.WorkspaceRootEnvName, t.TempDir())

		got := a.autoTaskDeliverableJSON()
		if want := tools.TaskDeliverableRepoJSONFor(src); got != want {
			t.Fatalf("channel ON: deliverable = %q, want %q (the declared dev_repo source repository, the same repo `task submit`/`worker` resolve)", got, want)
		}
		// The ROW is what the rest of the machinery reads: assert through the
		// production resolver, not the blob (Rule 13 — one parser).
		a.plainPrompt = true
		a.maybeRegisterSessionTask("pin the clone channel's deliverable")
		if !a.autoTaskRegistered {
			t.Fatalf("the start hook did not register a row — nothing to assert on")
		}
		if repo := tools.TaskDeliverableRepoFor(a.autoTaskRowID); repo != src {
			t.Errorf("row %s deliverable.repo = %q, want %q", a.autoTaskRowID, repo, src)
		}
		if wt, branch := tools.TaskDeliverableWorktreeFor(a.autoTaskRowID); wt != "" || branch != "" {
			t.Errorf("row %s already carries a ground (%q/%q) before adoptCloneGround ran — registration must stamp the repo only", a.autoTaskRowID, wt, branch)
		}
	})
}

// TestBarePromptCloneChannelGroundsTheRunInTheCloneWorkspace pins the second
// half: the run moves INTO the clone workspace, publishes it, and the source
// repo gains no linked worktree (the Rule-6a shape this face used to take).
func TestBarePromptCloneChannelGroundsTheRunInTheCloneWorkspace(t *testing.T) {
	src := cloneGroundFixtureRepo(t)
	root := t.TempDir()
	before, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	cloneGroundRestoreCWD(t, before)

	a := newAutoTaskAgent(t)
	t.Setenv(config.DevRepoEnvName, src)
	t.Setenv(config.WorkspaceRootEnvName, root)
	t.Setenv(tools.TaskGroundDirEnv, "")
	t.Setenv(tools.TaskGroundBranchEnv, "")
	a.plainPrompt = true

	a.maybeRegisterSessionTask("work in the clone workspace, not a linked worktree")
	if !a.autoTaskRegistered {
		t.Fatalf("the start hook did not register a row")
	}
	a.adoptCloneGround(a.autoTaskRowID)

	workspace, branch := a.CloneRunGround()
	if workspace == "" {
		t.Fatalf("adoptCloneGround took no ground — the run stayed in %s", before)
	}
	if want := filepath.Join(root, a.autoTaskRowID); workspace != want {
		t.Errorf("workspace = %q, want %q (workspace_root/<task-id>)", workspace, want)
	}
	if cwd, err := os.Getwd(); err != nil || cwd != workspace {
		t.Fatalf("cwd = %q (err %v), want the clone workspace %q — the chdir IS the mechanism", cwd, err, workspace)
	}
	if branch == "" {
		t.Errorf("no branch recorded for the ground — a ground without a branch cannot be handed back to the channel")
	}
	// The ground ANNOUNCEMENT (env half): the pair every bash call inherits.
	if dir, br := tools.TaskGroundAnnounced(); dir != workspace || br != branch {
		t.Errorf("announced ground = (%q, %q), want (%q, %q)", dir, br, workspace, branch)
	}
	// The row carries the ground, which is what makes it reclaimable later.
	if wt, br := tools.TaskDeliverableWorktreeFor(a.autoTaskRowID); wt != workspace || br != branch {
		t.Errorf("row %s ground = (%q, %q), want (%q, %q)", a.autoTaskRowID, wt, br, workspace, branch)
	}
	// THE NEGATIVE HALF: the source repo gained no linked worktree — this run
	// is a CLONE, not the Rule-6a branch of PROJECT.md. `git worktree list` is
	// the merger's own input, so it is the right surface to read.
	worktreeList := exec.Command("git", "-C", src, "worktree", "list")
	worktreeList.Env = testutil.GitEnv()
	out, err := worktreeList.CombinedOutput()
	if err != nil {
		t.Fatalf("git worktree list in %s: %v\n%s", src, err, out)
	}
	if strings.Contains(string(out), workspace) {
		t.Errorf("the clone workspace must NOT be a linked worktree of %s:\n%s", src, out)
	}
}

// TestRetryRoundAdoptsTheAnnouncedGround is the RED→GREEN arm for the defect the
// delivery-retry round hit (measured 2026-10-27, rows 01a17d6d → 01a17da5).
//
// THE SHAPE: a retry round is not a bare prompt. The channel spawns it INTO the
// ground it is trying to deliver (spawnCloneChannelAgent's cmd.Dir) and announces
// that directory through GOPI_RUN_GROUND — but that child is still a `gopi` run
// reading a briefing on stdin, so it reaches adoptCloneGround, which used to
// treat every such run as a FRESH one: it cloned <root>/<new-row-id>, chdir'd
// there and published THAT as its ground. Two directories then existed and the
// run's own briefing named the one it had just left:
//
//	01a17da5's briefing: "工作区仍是你的，在 /home/user/workspace/01a17d6d-…"
//	01a17da5's ground:   /home/user/workspace/01a17da5-…   (0 ahead, 0 behind)
//
// The old ground held the three undelivered commits; the announced one was
// empty; the delivery leg publishes the ANNOUNCED one — so delivery could never
// land, which is the exact failure the retry round exists to repair.
//
// THE PREDICATE UNDER TEST: a run that was HANDED a ground adopts it. Its
// briefing, its commits and the delivery leg's destination are then one
// directory, by construction rather than by the executor noticing.
func TestRetryRoundAdoptsTheAnnouncedGround(t *testing.T) {
	src := cloneGroundFixtureRepo(t)
	root := t.TempDir()
	before, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	cloneGroundRestoreCWD(t, before)

	// THE ANNOUNCED GROUND: an existing clone of the source, exactly the shape
	// the channel hands its retry round (a directory that is its OWN repository,
	// standing on the integration branch, possibly holding undelivered commits).
	announced := filepath.Join(root, "01a17d6d-0579-716f-9a5b-d3c58b495b1a")
	if err := os.MkdirAll(announced, 0o755); err != nil {
		t.Fatalf("mkdir announced ground: %v", err)
	}
	clone := exec.Command("git", "clone", "-q", src, announced)
	clone.Env = testutil.GitEnv("GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_SYSTEM=/dev/null")
	if out, err := clone.CombinedOutput(); err != nil {
		t.Fatalf("clone the announced ground: %v\n%s", err, out)
	}

	a := newAutoTaskAgent(t)
	t.Setenv(config.DevRepoEnvName, src)
	t.Setenv(config.WorkspaceRootEnvName, root)
	t.Setenv(tools.TaskGroundDirEnv, announced)
	t.Setenv(tools.TaskGroundBranchEnv, "")
	a.plainPrompt = true

	a.maybeRegisterSessionTask("交付没有落地。工作区仍是你的，在 " + announced)
	if !a.autoTaskRegistered {
		t.Fatalf("the start hook did not register a row")
	}
	a.adoptCloneGround(a.autoTaskRowID)

	workspace, _ := a.CloneRunGround()
	if workspace != announced {
		t.Fatalf("ground = %q, want the ANNOUNCED one %q — a run handed a ground must adopt it, or its briefing names a directory the delivery leg never publishes", workspace, announced)
	}
	if cwd, err := os.Getwd(); err != nil || cwd != announced {
		t.Fatalf("cwd = %q (err %v), want the announced ground %q", cwd, err, announced)
	}
	if dir, _ := tools.TaskGroundAnnounced(); dir != announced {
		t.Fatalf("announced ground was rewritten to %q — the announcement is the runner's claim, not this run's to redraw", dir)
	}
	// THE NEGATIVE HALF: no second workspace was minted for this row. A clone at
	// <root>/<task-id> is the shape that made the run's commits invisible.
	if second := filepath.Join(root, a.autoTaskRowID); second != announced {
		if _, err := os.Stat(second); err == nil {
			t.Fatalf("a second workspace %s was created beside the announced ground — the retry round is then split across two directories", second)
		}
	}
	if wt, _ := tools.TaskDeliverableWorktreeFor(a.autoTaskRowID); wt != announced {
		t.Fatalf("row ground = %q, want %q — the ledger must name the directory the run actually works in", wt, announced)
	}
}

// TestBarePromptStillClonesWhenNoGroundIsAnnounced is the compatibility half of
// the same predicate: with NOTHING announced (an operator's `echo "…" | gopi`),
// the bare-prompt face keeps opening its own clone workspace.
func TestBarePromptStillClonesWhenNoGroundIsAnnounced(t *testing.T) {
	src := cloneGroundFixtureRepo(t)
	root := t.TempDir()
	before, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	cloneGroundRestoreCWD(t, before)

	a := newAutoTaskAgent(t)
	t.Setenv(config.DevRepoEnvName, src)
	t.Setenv(config.WorkspaceRootEnvName, root)
	t.Setenv(tools.TaskGroundDirEnv, "")
	t.Setenv(tools.TaskGroundBranchEnv, "")
	a.plainPrompt = true

	a.maybeRegisterSessionTask("work in a clone of the declared dev_repo")
	if !a.autoTaskRegistered {
		t.Fatalf("the start hook did not register a row")
	}
	a.adoptCloneGround(a.autoTaskRowID)

	if workspace, _ := a.CloneRunGround(); workspace != filepath.Join(root, a.autoTaskRowID) {
		t.Fatalf("ground = %q, want a fresh clone at %q (nothing was announced, so nothing may be adopted)", workspace, filepath.Join(root, a.autoTaskRowID))
	}
}
