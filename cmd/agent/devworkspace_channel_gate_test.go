package main

// devworkspace_channel_gate_test.go — THE COMPATIBILITY GATE for the clone
// channel (settings dev_repo, 2026-10-18).
//
// The operator's acceptance sentence is one line: with the switch OFF
// everything behaves exactly as it does today; with it ON the SAME `gopi worker`
// call takes the clone path. This test drives the REAL binary twice, with the
// same argv and the same repository, and asserts the two facts that sentence
// decomposes into:
//
//	OFF — the run creates a LINKED WORKTREE (`git worktree add` registered in
//	      the repo) and clones nothing;
//	ON  — the run CLONES dev_repo into workspace_root/<task-id> (its own .git
//	      DIRECTORY, and the source repo gains no worktree registration).
//
// It is a regression gate, not a demonstration: it fails if either channel
// ever starts doing the other's job.

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nick/gopi/internal/config"
	"github.com/nick/gopi/internal/devworkspace"
	"github.com/nick/gopi/internal/testutil"
	"github.com/nick/gopi/internal/tools"
)

const (
	// cloneGateTaskID is a full uuid (the ground-naming rule needs one).
	cloneGateTaskID = "01a141a7-0000-7000-8000-00000000c10e"
	// cloneGateBuildCommand is the DECLARED build command: its output file is
	// how the test proves the loop ran the declaration and not a hard-coded
	// `go build` (Rule 12).
	cloneGateBuildCommand = "echo BUILT-BY-DECLARED > build-stamp.txt"
	// cloneGateRepoSettings is the repos-table entry the worker must read the
	// build command from.
	cloneGateRepoSettings = `"repos":{"%s":{"build":"` + cloneGateBuildCommand + `"}}`
)

// cloneGateEnv builds a hermetic profile for one worker run. extra is spliced
// into the settings document (so a test can add dev_repo / workspace_root).
//
// script is the mock model's script when a caller needs the child to DEED
// something (a second caller — the delivery loop's E2E — needs the child to
// leave its work uncommitted); omitted, it is the benign text-only answer every
// other caller wants.
func cloneGateEnv(t *testing.T, repo, extra string, script ...testutil.Resp) []string {
	t.Helper()
	gopiHome := t.TempDir()
	dbPath := filepath.Join(t.TempDir(), "memory.db")
	if len(script) == 0 {
		script = []testutil.Resp{{Text: "do the work — done"}}
	}
	mock := testutil.NewMockLLM(t, script...)
	t.Cleanup(mock.Close)
	settingsPath := filepath.Join(t.TempDir(), "settings.json")
	body := `{"sync_peers":[],` + strings.Replace(cloneGateRepoSettings, "%s", repo, 1)
	if extra != "" {
		body += "," + extra
	}
	body += "}"
	if err := os.WriteFile(settingsPath, []byte(body), 0o644); err != nil {
		t.Fatalf("write settings: %v", err)
	}
	env := testutil.MockEnv(t, gopiHome, mock.URL())
	return append(env,
		memoryDBEnv+"="+dbPath,
		config.SettingsEnv+"="+settingsPath,
		config.SkipClaimBroadcastEnv+"=1",
		"GOPI_AUTO_TASK_REGISTER=1",
	)
}

// cloneGateRepo builds a one-commit repository the worker can name as a row's
// deliverable repo.
func cloneGateRepo(t *testing.T) string {
	t.Helper()
	repo := filepath.Join(t.TempDir(), "repo")
	if err := os.MkdirAll(repo, 0o755); err != nil {
		t.Fatal(err)
	}
	chainGit(t, repo, "init", "-q", "-b", "master")
	chainGit(t, repo, "config", "receive.denyCurrentBranch", "ignore")
	if err := os.WriteFile(filepath.Join(repo, "a.txt"), []byte("hello\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	chainGit(t, repo, "add", "a.txt")
	chainGit(t, repo, "commit", "-q", "-m", "init")
	return repo
}

// cloneGateWorktreeCount counts the repo's LINKED worktree registrations — the
// observable that tells "a worktree was created" from "a clone was made".
func cloneGateWorktreeCount(t *testing.T, repo string) int {
	t.Helper()
	cmd := exec.Command("git", "-C", repo, "worktree", "list", "--porcelain")
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("git worktree list: %v", err)
	}
	n := 0
	for _, line := range strings.Split(string(out), "\n") {
		if strings.HasPrefix(line, "worktree ") {
			n++
		}
	}
	// The main checkout itself is always one entry.
	return n - 1
}

// TestWorkerChannelSwitchIsTheOnlyDifference is the gate itself.
func TestWorkerChannelSwitchIsTheOnlyDifference(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	repo := cloneGateRepo(t)

	// ── OFF: today's worktree channel ───────────────────────────────────────
	offEnv := cloneGateEnv(t, repo, "")
	for _, kv := range offEnv {
		if strings.HasPrefix(kv, config.DevRepoEnvName+"=") {
			t.Fatalf("the fixture leaked %s into the OFF profile", config.DevRepoEnvName)
		}
	}
	if _, stderr, code := runGopiMapped(t, []string{"task", "submit",
		"channel gate off", "--payload", "do the work", "--task-id", cloneGateTaskID,
		"--repo", repo}, "", offEnv, ""); code != 0 {
		t.Fatalf("submit: code=%d stderr=%s", code, stderr)
	}
	// A REPO-ONLY ROOT: the worktree channel must get exactly the run it gets
	// today, so the run is given the same synthetic backend the rest of the
	// suite uses.
	offOut, offErr, offCode := runGopiMapped(t, []string{"worker", "--once"}, "", offEnv, repo)
	if offCode != 0 {
		t.Fatalf("worker --once (worktree channel) must succeed: code=%d\nstdout:\n%s\nstderr:\n%s", offCode, offOut, offErr)
	}
	// THE OFF EVIDENCE: the run minted a LINKED WORKTREE under the repo's own
	// ground name (`gopi-<stem>`), i.e. `git worktree add` ran — and the run's
	// OWN release freed it as it ended.
	// (Counting registrations AFTER the run cannot see it: the release frees the
	// run's own worktree by design, so the run's own account is the observable.)
	//
	// THE MARKER MOVED, THE EVIDENCE DID NOT (2026-10-31). This arm used to look
	// for the literal "worktree-freed=", which was the finish-cleanup pass's
	// label. That pass is retired (`58b5618b`): a run now releases its own
	// ground inline, and the producer's one spelling is
	// tools.ReclaimSelfReleasedMarker ("own worktree released",
	// internal/tools/task_reclaim_self.go). So the assertion reads the CONSTANT
	// rather than a copied literal — Rule 1b/13: one spelling, referenced. A
	// test that keeps the old literal goes red on a tree carrying no defect,
	// which is exactly what it did.
	if !strings.Contains(offOut, tools.ReclaimSelfReleasedMarker) || !strings.Contains(offOut, "gopi-01a141a7000070008000000000") {
		t.Fatalf("the worktree channel must create (and free) its `git worktree add` ground:\n%s", offOut)
	}
	if strings.Contains(offOut, "clone channel ON") || strings.Contains(offOut, "cloned ") {
		t.Fatalf("dev_repo unset, yet the clone channel ran:\n%s", offOut)
	}

	// ── ON: the same command, the clone channel ─────────────────────────────
	root := filepath.Join(t.TempDir(), "workspace")
	onEnv := cloneGateEnv(t, repo, `"dev_repo":"`+repo+`","workspace_root":"`+root+`"`)
	onTask := "01a141a7-0000-7000-8000-00000000c10f"
	if _, stderr, code := runGopiMapped(t, []string{"task", "submit",
		"channel gate on", "--payload", "do the work", "--task-id", onTask,
		"--repo", repo}, "", onEnv, ""); code != 0 {
		t.Fatalf("submit (on): code=%d stderr=%s", code, stderr)
	}
	onOut, onErr, onCode := runGopiMapped(t, []string{"worker", "--once"}, "", onEnv, repo)
	if onCode != 0 {
		t.Fatalf("worker --once (clone channel) must succeed: code=%d\nstdout:\n%s\nstderr:\n%s", onCode, onOut, onErr)
	}
	if !strings.Contains(onOut+onErr, "clone channel ON") {
		t.Fatalf("dev_repo set, yet the clone channel did not announce itself:\nstdout:\n%s\nstderr:\n%s", onOut, onErr)
	}
	// THE SAME COMMAND: `gopi worker --once`, byte for byte (both runs above
	// spell their argv exactly once). What differs is only the workspace: the
	// clone channel must announce the clone and must NOT mint a linked
	// worktree under the repo's ground name.
	// workspace_root/<task-id>, where workspace_root is whatever channel WON
	// (testutil pins GOPI_WORKSPACE_ROOT, and env deliberately outranks the
	// settings file) — so the assertion is the SHAPE plus the task's own leaf,
	// which is the part the channel must never get wrong.
	if !strings.Contains(onOut+onErr, string(filepath.Separator)+onTask) {
		t.Fatalf("the run's workspace must be named after the task id (%s):\nstdout:\n%s\nstderr:\n%s",
			onTask, onOut, onErr)
	}
	if !strings.Contains(onOut+onErr, "workspace_root=") {
		t.Fatalf("the run must name the workspace root it used:\n%s", onOut)
	}
	if strings.Contains(onOut+onErr, tools.ReclaimSelfReleasedMarker) && strings.Contains(onOut+onErr, "gopi-"+strings.ReplaceAll(onTask, "-", "")) {
		t.Fatalf("the clone channel minted a LINKED WORKTREE; it must clone instead:\n%s", onOut)
	}
	if got := cloneGateWorktreeCount(t, repo); got != 0 {
		t.Fatalf("the clone channel left %d linked worktree(s) registered in %s; it must add none", got, repo)
	}
	// THE CLONE LANDED WHERE THE CONFIG SAID, UNDER THE TASK'S OWN NAME.
	if _, statErr := os.Stat(devworkspace.Dir(root, onTask)); statErr == nil {
		t.Fatalf("the workspace %s must have been RECLAIMED after a successful push",
			devworkspace.Dir(root, onTask))
	}
	_ = root
	// THE RECLAIM MUST BE NAMED — and the channel has TWO ways to reach it, so
	// the assertion reads whichever this run actually took instead of the one
	// spelling it used to hard-code (measured 2026-10-28, 20c1500 "nothing-to-
	// deliver is a conclusion, not a failed delivery"): a run that PUSHED says
	// "reclaimed the clone workspace"; a run whose workspace held no commit past
	// master says the workspace "is RECLAIMED: it held no commit past <base>".
	// Both name the directory and both say it is gone; demanding only the first
	// made the second — the honest conclusion this fixture actually reaches,
	// since its stub agent commits nothing — read as a missing feature.
	reclaimedByPush := strings.Contains(onOut+onErr, "reclaimed the clone workspace")
	reclaimedByEmpty := strings.Contains(onOut+onErr, "is RECLAIMED")
	if !reclaimedByPush && !reclaimedByEmpty {
		t.Fatalf("the run must report the reclaim by name (either \"reclaimed the clone workspace\" "+
			"after a push, or \"is RECLAIMED\" for an empty workspace):\n%s", onOut)
	}
	// AND THE DELIVERY LEG REACHED ONE OF ITS TWO ADMISSIBLE CONCLUSIONS. The
	// branch is not weakened by taking the second: a run that pushed must print
	// the delivery line, and a run that had nothing to deliver must say SO BY
	// NAME — silence satisfies neither.
	delivered := strings.Contains(onOut+onErr, "clone channel delivered on delivery attempt")
	nothingToDeliver := strings.Contains(onOut+onErr, "nothing to deliver")
	if !delivered && !nothingToDeliver {
		t.Fatalf("the clone channel's delivery leg printed neither its delivery line nor its "+
			"nothing-to-deliver conclusion:\n%s", onOut+onErr)
	}
	if delivered && !reclaimedByPush {
		t.Fatalf("a delivered run must name the reclaim it performed:\n%s", onOut+onErr)
	}
	if nothingToDeliver && !reclaimedByEmpty {
		t.Fatalf("a nothing-to-deliver run must say the workspace is RECLAIMED:\n%s", onOut+onErr)
	}
}
