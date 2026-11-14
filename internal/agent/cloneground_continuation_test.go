package agent

// cloneground_continuation_test.go — the CONTINUATION face's clone ground
// (bug 01a193f8, measured 2026-10-31 on rows 01a190bb @05:57 and 01a190b4
// @06:02).
//
// THE DEFECT THESE TESTS CLOSE. `gopi worker --resume <row>` runs
// `gopi session resume <raw-log> --task <row>` in a child, and that child is a
// CONTINUATION: it mints no row, so loop.go's start hook — which registered the
// row AND adopted the clone ground — was skipped whole (`if !resume`). The
// consequences chained through three layers, and every one of them is pinned
// below:
//
//	cloneGroundDir stayed ""  →  CloneRunGround() answered ("", "")
//	                          →  finishCloneDelivery took its COMPATIBILITY arm
//	                             (the worktree channel's), so the clone channel
//	                             attempted NO delivery at all
//	                          →  the end hook wrote `completed` on a claim that
//	                             had never tried to publish, and the completion
//	                             witness recorded `witnessless` — which by its own
//	                             contract changes no status.
//
// The row read `completed` while its commits sat in the clone and the peer's
// `git ls-remote` never saw them. The control group (same host, same day, a
// QUEUE-launched `gopi worker`, no --resume) pushed normally, which is what
// located the defect in the resume close-out leg rather than in the environment.
//
// The four arms below are deliberately the SAME fixture with one variable
// changed at a time, so a green cannot come from anything but the branch under
// test:
//
//	① a CLONE ground this run already stands in is ADOPTED, not re-minted
//	② that adoption ARMS the delivery leg (the compat arm is not taken)
//	③ a LINKED worktree is NOT adopted (the discriminator is unchanged)
//	④ the channel OFF keeps the compatibility arm byte-identical

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nickhuang99/gopi/internal/config"
	"github.com/nickhuang99/gopi/internal/devworkspace"
	"github.com/nickhuang99/gopi/internal/testutil"
	"github.com/nickhuang99/gopi/internal/tools"
)

// cloneGroundRunGit runs ONE git command through testutil.GitEnv — the fixture
// gate (internal/standards) forbids a test handing git the INHERITED repository
// routing, because that is the class that once rewrote the real master.
func cloneGroundRunGit(dir string, args ...string) (string, error) {
	cmd := exec.Command("git", args...)
	if strings.TrimSpace(dir) != "" {
		cmd.Dir = dir
	}
	cmd.Env = testutil.GitEnv(
		"GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_SYSTEM=/dev/null",
		"GIT_AUTHOR_NAME=clone ground test", "GIT_AUTHOR_EMAIL=ground@test.invalid",
		"GIT_COMMITTER_NAME=clone ground test", "GIT_COMMITTER_EMAIL=ground@test.invalid",
	)
	out, err := cmd.CombinedOutput()
	return string(out), err
}

// continuationCloneGround builds a clone-shaped directory under root — the
// SHAPE is what the discriminator reads (`.git` a DIRECTORY), and the fixture
// makes it by cloning the same minimal source repo the sibling tests use, so
// nothing about the check is simulated.
func continuationCloneGround(t *testing.T, src, root, name string) string {
	t.Helper()
	dir := filepath.Join(root, name)
	if out, err := cloneGroundRunGit("", "clone", "-q", src, dir); err != nil {
		t.Fatalf("git clone %s %s: %v\n%s", src, dir, err, out)
	}
	return dir
}

// TestContinuationAdoptsTheCloneGroundItStandsIn is arm ① — the fix itself. A
// continuation standing in a clone adopts THAT directory: it does not open a
// second workspace, and the directory it adopted is the one the ledger and the
// delivery leg will name.
func TestContinuationAdoptsTheCloneGroundItStandsIn(t *testing.T) {
	src := cloneGroundFixtureRepo(t)
	root := t.TempDir()
	before, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	cloneGroundRestoreCWD(t, before)

	// The directory the resume path already chdir'd into (cmd/agent's
	// resumeChdirToWorktree is the ONE reader of deliverable.worktree).
	ground := continuationCloneGround(t, src, root, "01a193f8-continuation")
	if err := os.Chdir(ground); err != nil {
		t.Fatalf("enter the ground: %v", err)
	}

	a := newAutoTaskAgent(t)
	t.Setenv(config.DevRepoEnvName, src)
	t.Setenv(config.WorkspaceRootEnvName, root)
	t.Setenv(tools.TaskGroundDirEnv, "")
	t.Setenv(tools.TaskGroundBranchEnv, "")

	// The row a resume continues: adopted BEFORE the engine starts, exactly as
	// cmd/agent's adoptResumeLedgerRow does. (A resume mints no row of its own.)
	a.maybeRegisterSessionTask("a continuation re-enters the ground its first run worked in")
	if !a.autoTaskRegistered {
		t.Fatalf("fixture premise: the row must exist before the adoption can name it")
	}
	row := a.autoTaskRowID
	if !a.adoptContinuationCloneGround(row) {
		t.Fatalf("a CONTINUATION standing in a clone did not adopt it — the delivery leg is then never armed, which is the defect this row exists for")
	}

	if workspace, _ := a.CloneRunGround(); workspace != ground {
		t.Fatalf("ground = %q, want the directory this run stands in %q — a continuation re-enters its ground, it does not mint one", workspace, ground)
	}
	if cwd, cerr := os.Getwd(); cerr != nil || cwd != ground {
		t.Fatalf("cwd = %q (err %v), want the adopted ground %q", cwd, cerr, ground)
	}
	// THE NEGATIVE HALF: no second workspace was minted under <root>/<row>.
	// That is the shape that made the commits invisible — two directories, one
	// of them empty, and the briefing naming the other.
	if second := filepath.Join(root, row); second != ground {
		if _, statErr := os.Stat(second); statErr == nil {
			t.Fatalf("a second workspace %s was created beside the ground the run stands in — the run would then publish from a directory the ledger does not name", second)
		}
	}
	if wt, _ := tools.TaskDeliverableWorktreeFor(row); wt != ground {
		t.Fatalf("row %s deliverable.worktree = %q, want the adopted ground %q — the anchor gate refuses a delivery from a directory the row does not name", row, wt, ground)
	}
}

// TestContinuationGroundArmsTheDeliveryLeg is arm ② — the CHAIN, not just the
// variable. It pins the consequence the bug was made of: with the ground
// adopted, finishCloneDelivery RUNS (Attempted), so the close-out cannot read a
// never-tried delivery as a clean bill.
//
// The attempt itself is injected (cloneDeliveryAttempt is a field for exactly
// this) because the real one pushes to a peer: what this test must prove is that
// the leg is REACHED, and the delivery's own result is the channel's business,
// pinned by the devworkspace suite.
func TestContinuationGroundArmsTheDeliveryLeg(t *testing.T) {
	src := cloneGroundFixtureRepo(t)
	root := t.TempDir()
	before, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	cloneGroundRestoreCWD(t, before)

	ground := continuationCloneGround(t, src, root, "01a193f8-armed")
	if err := os.Chdir(ground); err != nil {
		t.Fatalf("enter the ground: %v", err)
	}

	a := newAutoTaskAgent(t)
	t.Setenv(config.DevRepoEnvName, src)
	t.Setenv(config.WorkspaceRootEnvName, root)
	t.Setenv(tools.TaskGroundDirEnv, "")
	t.Setenv(tools.TaskGroundBranchEnv, "")
	a.maybeRegisterSessionTask("arm the delivery leg on a continuation")
	row := a.autoTaskRowID
	if !a.adoptContinuationCloneGround(row) {
		t.Fatalf("fixture premise: the continuation must adopt its ground first")
	}

	// THE SIGNAL: was the attempt called at all?
	called := false
	a.cloneDeliveryAttempt = func(_ context.Context, cfg devworkspace.Config, dir string) (devworkspace.Result, *devworkspace.Blocker, error) {
		called = true
		if dir != ground {
			t.Errorf("attempt dir = %q, want the adopted ground %q", dir, ground)
		}
		if cfg.TaskID != row {
			t.Errorf("attempt task id = %q, want the adopted row %q", cfg.TaskID, row)
		}
		// AN EMPTY-GROUND CONCLUSION: the fixture's clone has no commit past the
		// integration branch, which is the honest answer for it and the one shape
		// that must NOT be converted into a non-delivery (bug 01a182a8).
		return devworkspace.Result{}, &devworkspace.Blocker{
			Kind:  devworkspace.BlockerKindEmpty,
			Step:  "rev-list",
			Words: "the fixture ground carries no commit past the integration branch",
		}, nil
	}

	out := a.finishCloneDelivery("")
	if !called {
		t.Fatalf("the delivery leg never ran on a continuation with an adopted clone ground — this is the compatibility arm the bug took, and it is how a `completed` row kept commits nobody could fetch")
	}
	if !out.Attempted {
		t.Fatalf("outcome.Attempted = false after an attempt was made — the end hook decides the status from this value")
	}
	if !out.NothingToDeliver {
		t.Fatalf("an EMPTY blocker must be a CONCLUSION (NothingToDeliver=true), got %+v — folding it into a non-delivery writes the row back to in_progress (bug 01a182a8)", out)
	}
	if out.Delivered {
		t.Fatalf("Delivered = true for an empty ground")
	}
	if !strings.Contains(out.Reason, cloneNothingToDeliverToken) {
		t.Fatalf("reason = %q, want the nothing-to-deliver conclusion (%q)", out.Reason, cloneNothingToDeliverToken)
	}
}

// TestContinuationDoesNotAdoptLinkedWorktreeOrChannelOff is arms ③ and ④: the
// discriminator and the compatibility contract are UNTOUCHED by the new call
// site. A linked worktree is the other channel's ground, and a channel that is
// OFF must behave exactly as it did before this row existed.
func TestContinuationDoesNotAdoptLinkedWorktreeOrChannelOff(t *testing.T) {
	src := cloneGroundFixtureRepo(t)
	root := t.TempDir()
	before, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	cloneGroundRestoreCWD(t, before)

	t.Run("a LINKED worktree is not this channel's ground", func(t *testing.T) {
		// A linked worktree is a directory whose `.git` is a FILE. The fixture
		// makes one the only way it can be made — `git worktree add` — so the
		// discriminator reads a real shape rather than a hand-built file.
		linked := filepath.Join(root, "linked-ground")
		if out, werr := cloneGroundRunGit(src, "worktree", "add", "-q", linked, "HEAD"); werr != nil {
			t.Fatalf("git worktree add %s: %v\n%s", linked, werr, out)
		}
		if err := os.Chdir(linked); err != nil {
			t.Fatalf("enter the linked worktree: %v", err)
		}
		a := newAutoTaskAgent(t)
		t.Setenv(config.DevRepoEnvName, src)
		t.Setenv(config.WorkspaceRootEnvName, root)
		t.Setenv(tools.TaskGroundDirEnv, "")
		a.maybeRegisterSessionTask("a linked worktree is the OTHER channel's ground")
		if a.adoptContinuationCloneGround(a.autoTaskRowID) {
			t.Fatalf("a continuation standing in a LINKED worktree adopted it — the clone channel would then publish from the worktree channel's ground")
		}
		if workspace, _ := a.CloneRunGround(); workspace != "" {
			t.Fatalf("ground = %q, want empty (this run is not in the clone channel)", workspace)
		}
	})

	t.Run("channel OFF is the compatibility arm", func(t *testing.T) {
		ground := continuationCloneGround(t, src, root, "01a193f8-off")
		if err := os.Chdir(ground); err != nil {
			t.Fatalf("enter the ground: %v", err)
		}
		a := newAutoTaskAgent(t)
		t.Setenv(config.DevRepoEnvName, "")
		t.Setenv(config.WorkspaceRootEnvName, root)
		t.Setenv(tools.TaskGroundDirEnv, "")
		if on, _, cerr := config.CloneChannelConfigured(); cerr != nil || on {
			t.Fatalf("fixture premise: the channel must be OFF (on=%v err=%v)", on, cerr)
		}
		a.maybeRegisterSessionTask("the channel is off for this host")
		if a.adoptContinuationCloneGround(a.autoTaskRowID) {
			t.Fatalf("the continuation adopted a ground with the clone channel OFF — the OFF arm must stay byte-identical")
		}
		out := a.finishCloneDelivery("")
		if out.Attempted {
			t.Fatalf("the delivery leg ran with no clone ground — the compatibility arm is what keeps every non-clone face unchanged")
		}
	})
}

// TestResumeStartHookAdoptsTheGroundItStandsIn is the WIRING arm — the one that
// pins loop.go's branch rather than the helper it calls. The arms above would
// stay green if the start hook never called the new door, which is exactly the
// shape that let the original defect live: the mechanism existed for the
// bare-prompt face and the resume face simply never reached it (Rule 31 — a
// surface nobody executes is not delivered).
//
// The engine is driven for real against a stub endpoint returning a plain text
// turn, so the run goes through runEngine's OWN start hook (the `if !resume`
// branch and its new continuation half). Nothing about the adoption is
// simulated; the ONE thing the fixture controls is the model. The row's own
// ledger STAMP is arm ①'s subject (it has a real row); this arm asks only the
// question the defect was: does the resume face arrive at the adoption at all.
func TestResumeStartHookAdoptsTheGroundItStandsIn(t *testing.T) {
	src := cloneGroundFixtureRepo(t)
	root := t.TempDir()
	before, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	cloneGroundRestoreCWD(t, before)

	ground := continuationCloneGround(t, src, root, "01a193f8-wiring")
	if err := os.Chdir(ground); err != nil {
		t.Fatalf("enter the ground: %v", err)
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		claudeTextStream(w, "done", "end_turn")
	}))
	defer server.Close()

	t.Setenv(config.DevRepoEnvName, src)
	t.Setenv(config.WorkspaceRootEnvName, root)
	t.Setenv(tools.TaskGroundDirEnv, "")
	t.Setenv(tools.TaskGroundBranchEnv, "")

	a := newTestAgent(t, server)
	// THE ADOPTED ROW: a resume continues an existing row, and cmd/agent adopts
	// it BEFORE the engine starts (adoptResumeLedgerRow → AdoptLedgerRow). The
	// start hook's continuation half reads that latch, so the fixture sets it
	// through the same door. The id is the one the brief's samples carry — a
	// resume's row always exists in production; what this arm isolates is
	// whether the START HOOK consults it.
	a.AdoptLedgerRow(continuationWiringRowID)

	if _, rerr := a.Resume("", nil); rerr != nil {
		t.Fatalf("resume run: %v", rerr)
	}

	if workspace, _ := a.CloneRunGround(); workspace != ground {
		t.Fatalf("after a RESUME the run's clone ground is %q, want %q — with an empty ground the close-out's delivery leg takes the worktree channel's compatibility arm and the row closes `completed` having never attempted a push (rows 01a190bb / 01a190b4)", workspace, ground)
	}
}

// continuationWiringRowID is the row the wiring arm adopts. It is deliberately
// the SAME shape a resume's row has (a ledger id), and the arm asserts the GROUND
// latch rather than the ledger stamp, which is arm ①'s subject.
const continuationWiringRowID = "01a193f8-0000-7000-8000-000000000001"
