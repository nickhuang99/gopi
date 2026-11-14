package main

// cli_tasks_rebuild_vet_revert_test.go — the VET gate's revert arm driven through
// the REAL close loop (serveRebuild), which is the only place the epic's
// acceptance can actually be proven: the classification lives in internal/tools,
// but the DECISION and the git run here, ordered before the build.
//
// WHY THE ARM IS THE POINT, and why a unit test in internal/tools cannot replace
// it: the two interesting failures are ORDERING failures ("the build ran first and
// stamped a binary at a merge that was then reverted") and WIRING failures ("the
// classifier was never called on the close-loop path"). Neither is visible from
// the classifier's own package — they are properties of serveRebuild — so both
// arms are driven through serveRebuild with a real git fixture and the disposition
// is asserted on the REPO (master's tip), not on a returned struct.
//
// The pre-existing arm uses the documented substitution seam
// (GOPI_VET_GATE_BASELINE_OVERRIDE) rather than a second repo whose master is
// genuinely red: BOTH arms must run the same round through the same call, or the
// differential would be between two different fixtures instead of between two
// comparisons of the same measurement. The seam moves a verdict toward KEEPING a
// merge and can never cause the reversion this file's first arm asserts — so arm 1
// is unaffected by it (it is unset there), and arm 2 is exactly the "master was
// already red" input this design must keep.

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nickhuang99/gopi/internal/heavytest"
	"github.com/nickhuang99/gopi/internal/tools"
)

const (
	// srvVetFixturePkgDir / srvVetFixturePkgFile are the clean module the fixture
	// lands on: small enough that one real `go vet` is the only cost.
	srvVetFixturePkgDir  = "p"
	srvVetFixturePkgFile = "p.go"
	srvVetFixtureGoMod   = "go.mod"
	srvVetFixtureModule  = "p"
	srvVetFixtureGoLine  = "go 1.21\n"
	srvVetFixtureClean   = "package p\n\nfunc OK() int { return 1 }\n"
	// srvVetFixtureRedFile is the planted compile break (a symbol that exists
	// nowhere) — the shape the 2026-09-16 incident landed: `go build` stays green
	// for the rest of the module while the tree no longer holds together.
	srvVetFixtureRedFile = "z_broken.go"
	srvVetFixtureRedBody = "package p\n\nfunc Broken() { srvNeverDefinedSymbol() }\n"
	// srvVetFixtureBranchFile / srvVetFixtureBranchBody are the pre-existing arm's
	// BENIGN branch payload (a landing with no commit on the branch is not a
	// landing — git reports "Already up to date").
	srvVetFixtureBranchFile = "note.txt"
	srvVetFixtureBranchBody = "the branch's only change\n"
	// srvVetFixtureBranch is the branch whose landing introduces the break.
	srvVetFixtureBranch = "feat/planted-red"
	// srvVetFixtureBuildCmd makes the close loop's build step a NO-OP that
	// succeeds: the subject of these tests is the gate's ordering against the
	// build, so the build must be observable (it runs) without being expensive,
	// and a repo with no bin/gopi is the documented "the build command's exit 0 IS
	// its contract" shape (verifyBuiltVersion is not reached).
	srvVetFixtureBuildCmd = "true"
	// srvVetFixtureSettingsKey is the settings.json repos-table key (the build
	// CONTRACT that makes the close loop applicable to this repo at all).
	srvVetFixtureSettingsKey = "repos"
)

// srvVetFixtureRepo builds the module + the real merge and returns the repo, the
// pre-merge tip and the handle the close loop is given. plantedInMaster plants the
// break on master BEFORE the capture, which is what makes the tree the baseline
// measurement checks out genuinely red (arm 2's input).
func srvVetFixtureRepo(t *testing.T, plantedInMaster bool) (repo, preTip string, revert *tools.FailedMergeRevert) {
	t.Helper()
	repo = filepath.Join(t.TempDir(), "repo")
	if err := os.MkdirAll(filepath.Join(repo, srvVetFixturePkgDir), 0o755); err != nil {
		t.Fatal(err)
	}
	srvVetWrite(t, repo, srvVetFixtureGoMod, srvVetFixtureGoLine+"\nmodule "+srvVetFixtureModule+"\n")
	srvVetWrite(t, repo, filepath.Join(srvVetFixturePkgDir, srvVetFixturePkgFile), srvVetFixtureClean)
	srvVetGit(t, repo, "init", "-q", "-b", "master")
	srvVetGit(t, repo, "config", "user.email", "t@t")
	srvVetGit(t, repo, "config", "user.name", "t")
	srvVetGit(t, repo, "add", srvVetFixtureGoMod, filepath.Join(srvVetFixturePkgDir, srvVetFixturePkgFile))
	srvVetGit(t, repo, "commit", "-q", "-m", "fixture: a clean module")

	if plantedInMaster {
		srvVetWrite(t, repo, filepath.Join(srvVetFixturePkgDir, srvVetFixtureRedFile), srvVetFixtureRedBody)
		srvVetGit(t, repo, "add", filepath.Join(srvVetFixturePkgDir, srvVetFixtureRedFile))
		srvVetGit(t, repo, "commit", "-q", "-m", "master is already vet-red")
	}

	// THE CAPTURE, exactly where the merger performs it: before the landing, so
	// the recorded tip is the tree the round is measured AGAINST.
	preTip = srvVetGit(t, repo, "rev-parse", "HEAD")
	revert = &tools.FailedMergeRevert{Repo: repo, PreTip: preTip}

	srvVetGit(t, repo, "checkout", "-q", "-b", srvVetFixtureBranch)
	if plantedInMaster {
		// The branch must still carry a change, or `git merge` short-circuits to
		// "Already up to date" and there is no landing to judge. The payload is
		// deliberately benign: the red belongs to master, not to the branch.
		srvVetWrite(t, repo, srvVetFixtureBranchFile, srvVetFixtureBranchBody)
		srvVetGit(t, repo, "add", srvVetFixtureBranchFile)
		srvVetGit(t, repo, "commit", "-q", "-m", "a benign change, so the branch is landable")
	} else {
		srvVetWrite(t, repo, filepath.Join(srvVetFixturePkgDir, srvVetFixtureRedFile), srvVetFixtureRedBody)
		srvVetGit(t, repo, "add", filepath.Join(srvVetFixturePkgDir, srvVetFixtureRedFile))
		srvVetGit(t, repo, "commit", "-q", "-m", "plant a tree that does not compile")
	}
	srvVetGit(t, repo, "checkout", "-q", "master")
	srvVetGit(t, repo, "merge", "--no-ff", "-q", "-m", "land the planted red", srvVetFixtureBranch)

	// The build CONTRACT that makes this repo a close-loop repo (Rule 12: the
	// settings repos table IS the contract — an undeclared build means the close
	// loop is skipped entirely and the gate is never consulted).
	settings := filepath.Join(t.TempDir(), "settings.json")
	blob, err := json.Marshal(map[string]any{
		srvVetFixtureSettingsKey: map[string]any{repo: map[string]any{"build": srvVetFixtureBuildCmd}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(settings, blob, 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GOPI_SETTINGS", settings)
	return repo, preTip, revert
}

// TestServeRebuildRevertsIntroducedVetFailureThroughTheCloseLoop is the epic's
// FIRST acceptance arm on the real path: a branch lands a tree that does not hold
// together, master was green before it, and the ROUND IS UNWOUND — master's tip is
// back at the recorded pre-merge tip and the branch survives.
func TestServeRebuildRevertsIntroducedVetFailureThroughTheCloseLoop(t *testing.T) {
	heavytest.Require(t, heavytest.CriterionDeviceExec)
	repo, preTip, revert := srvVetFixtureRepo(t, false)

	reverted := serveRebuild(repo, revert)

	if !reverted {
		t.Fatalf("serveRebuild must report the reversion; master tip is %s (want the pre-merge tip %s)",
			srvVetGit(t, repo, "rev-parse", "HEAD"), preTip)
	}
	if head := srvVetGit(t, repo, "rev-parse", "HEAD"); head != preTip {
		t.Fatalf("master tip after the close loop = %s, want the recorded pre-merge tip %s", head, preTip)
	}
	if out := srvVetGit(t, repo, "branch", "--list", srvVetFixtureBranch); strings.TrimSpace(out) == "" {
		t.Fatal("the landed branch must survive the unwind — the reversion preserves the work, it does not discard it")
	}
}

// TestServeRebuildKeepsPreExistingVetFailureThroughTheCloseLoop is the SECOND
// arm on the same path: byte-identical gate output, but the comparison says master
// was ALREADY red, so the merge STANDS. This is the arm that stops the gate from
// unwinding honest work for a failure it did not cause.
func TestServeRebuildKeepsPreExistingVetFailureThroughTheCloseLoop(t *testing.T) {
	heavytest.Require(t, heavytest.CriterionDeviceExec)
	repo, preTip, revert := srvVetFixtureRepo(t, true)
	// The documented seam: it CONSTRUCTS the pre-existing state so BOTH arms go
	// through the same round. It can only move the verdict toward keeping the
	// merge (which is what this arm asserts), never toward the reversion arm 1
	// asserts — where it is unset.
	t.Setenv("GOPI_VET_GATE_BASELINE_OVERRIDE", "red")

	reverted := serveRebuild(repo, revert)

	if reverted {
		t.Fatal("a PRE-EXISTING vet failure must never unwind the merge")
	}
	if head := srvVetGit(t, repo, "rev-parse", "HEAD"); head == preTip {
		t.Fatalf("the merge must be KEPT (master advanced past %s), but the tip is still %s", preTip, head)
	}
	if _, err := srvVetGitE(t, repo, "merge-base", "--is-ancestor", srvVetFixtureBranch, "HEAD"); err != nil {
		t.Fatalf("the kept merge must still contain the landed branch: %v", err)
	}
}

// TestServeRebuildNoHandleStillMeasuresAndKeeps: the operator-invoked face (no
// merger-sweep snapshot) cannot unwind anything it did not land — and that must not
// turn the gate off. An introduced red here is left standing WITH its alarm (the
// gate reports through its own channel), because silently doing nothing would
// re-open the 2026-09-16 hole for the manual face.
func TestServeRebuildNoHandleStillMeasuresAndKeeps(t *testing.T) {
	heavytest.Require(t, heavytest.CriterionDeviceExec)
	repo, preTip, _ := srvVetFixtureRepo(t, false)

	reverted := serveRebuild(repo, nil)

	if reverted {
		t.Fatal("a close loop with no revert handle must not unwind a merge it cannot attribute")
	}
	if head := srvVetGit(t, repo, "rev-parse", "HEAD"); head == preTip {
		t.Fatal("the merge must stand when this face has no snapshot to restore")
	}
}

// srvVetWrite writes a fixture file (the directories are created by the caller).
func srvVetWrite(t *testing.T, repo, rel, body string) {
	t.Helper()
	path := filepath.Join(repo, rel)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

// srvVetGit runs git in the fixture and returns its trimmed output, failing the
// test on error (a fixture that cannot be built is not a measurement).
func srvVetGit(t *testing.T, repo string, args ...string) string {
	t.Helper()
	out, err := srvVetGitE(t, repo, args...)
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return strings.TrimSpace(out)
}

// srvVetGitE is srvVetGit for the callers that must be able to OBSERVE a nonzero
// exit (the ancestor probe: its exit code IS the assertion).
func srvVetGitE(t *testing.T, repo string, args ...string) (string, error) {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", repo}, args...)...)
	out, err := cmd.CombinedOutput()
	return strings.TrimSpace(string(out)), err
}
