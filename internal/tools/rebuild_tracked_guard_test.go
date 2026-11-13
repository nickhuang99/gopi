package tools

// Regression tests for the TRACKED-ONLY dirty predicate split (epic
// 2026-09-12) — real temp git repos, the smallest sample that exercises
// `git status --porcelain` flag semantics.
//
// The two helpers are deliberately different contracts:
//
//	AdoptionGitStatus        — untracked-INCLUSIVE (the adoption chain's
//	                           strict pristine-tree guard: probe merges and
//	                           ③a commits must not entangle with untracked
//	                           in-flight work)
//	AdoptionGitStatusTracked — tracked-ONLY (the post-merge rebuild guard:
//	                           untracked scratch cannot change any committed
//	                           source file, so it must not defer the build)
//
// And buildVCSStatusEnv closes the second half of the same bug: the go
// toolchain stamps vcs.modified from `git status --porcelain`, which counts
// untracked files, so a build over an untracked-only tree produced a
// "-dirty" binary that versionMatches (correctly) rejects — a false
// build-failed alarm + auto-revert after an untracked-only merge round.

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	gopiversion "github.com/nick/gopi/internal/version"
)

// TestAdoptionGitStatusTrackedIgnoresUntracked is the predicate's red-proof:
// untracked entries must NOT appear in the tracked-only view, while the
// strict helper still reports them.
func TestAdoptionGitStatusTrackedIgnoresUntracked(t *testing.T) {
	repo, _ := rebuildTestRepo(t)
	if err := os.WriteFile(filepath.Join(repo, "untracked.md"), []byte("scratch\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	tracked, err := AdoptionGitStatusTracked(repo)
	if err != nil {
		t.Fatalf("AdoptionGitStatusTracked: %v", err)
	}
	if strings.TrimSpace(tracked) != "" {
		t.Fatalf("tracked-only view must be EMPTY for an untracked-only tree, got %q", tracked)
	}

	strict, err := AdoptionGitStatus(repo)
	if err != nil {
		t.Fatalf("AdoptionGitStatus: %v", err)
	}
	if !strings.Contains(strict, "untracked.md") {
		t.Fatalf("strict (untracked-inclusive) view must report the untracked file, got %q", strict)
	}
}

// TestAdoptionGitStatusTrackedReportsTrackedDirt: a tracked modification is
// visible in BOTH views — the split must not have blinded the rebuild guard.
func TestAdoptionGitStatusTrackedReportsTrackedDirt(t *testing.T) {
	repo, _ := rebuildTestRepo(t)
	if err := os.WriteFile(filepath.Join(repo, "f.txt"), []byte("edited\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	tracked, err := AdoptionGitStatusTracked(repo)
	if err != nil {
		t.Fatalf("AdoptionGitStatusTracked: %v", err)
	}
	if !strings.Contains(tracked, "f.txt") {
		t.Fatalf("tracked-only view must report a modified tracked file, got %q", tracked)
	}
}

// TestBuildVCSStatusEnvDropsCallerConfig: the subprocess env must not carry
// a duplicate GIT_CONFIG_COUNT (which would make the stamp depend on
// getenv's first-match order) and must not leak the caller's GIT_CONFIG_*.
func TestBuildVCSStatusEnvDropsCallerConfig(t *testing.T) {
	t.Setenv("GIT_CONFIG_COUNT", "2")
	t.Setenv("GIT_CONFIG_KEY_0", "status.showUntrackedFiles")
	t.Setenv("GIT_CONFIG_VALUE_0", "yes")
	env := buildVCSStatusEnv()
	count := 0
	for _, kv := range env {
		if strings.HasPrefix(kv, "GIT_CONFIG_COUNT=") {
			count++
		}
		if strings.HasPrefix(kv, "GIT_CONFIG_VALUE_0=") && strings.HasSuffix(kv, "yes") {
			t.Fatalf("caller's GIT_CONFIG_* leaked into the build env: %q", kv)
		}
	}
	if count != 1 {
		t.Fatalf("expected exactly ONE GIT_CONFIG_COUNT (no duplicate), got %d", count)
	}
	if !containsKV(env, gitConfigCountEnv+"="+gitConfigCountOne) ||
		!containsKV(env, gitConfigKeyEnv+"="+gitConfigShowUntrackedKey) ||
		!containsKV(env, gitConfigValueEnv+"="+gitConfigShowUntrackedValue) {
		t.Fatalf("build env missing the showUntrackedFiles=no pair: %v", env)
	}
}

// containsKV reports whether env holds the exact key=value entry.
func containsKV(env []string, kv string) bool {
	for _, e := range env {
		if e == kv {
			return true
		}
	}
	return false
}

// rebuildGuardProbeMainSrc is the probe program the end-to-end guard tests
// compile. It mirrors cmd/agent's buildVersion: on --version it prints the
// toolchain's short VCS revision, with the "-dirty" suffix the toolchain's
// vcs.modified stamp produces. Asserting on THIS is asserting on the real
// stamping path a GoPi-convention build takes.
const rebuildGuardProbeMainSrc = `package main

import (
	"fmt"
	"os"
	"runtime/debug"
)

func main() {
	bi, ok := debug.ReadBuildInfo()
	if !ok {
		fmt.Println("dev")
		return
	}
	rev, dirty := "", false
	for _, s := range bi.Settings {
		switch s.Key {
		case "vcs.revision":
			rev = s.Value
		case "vcs.modified":
			dirty = s.Value == "true"
		}
	}
	if rev == "" {
		fmt.Println("dev")
		return
	}
	if len(rev) > 12 {
		rev = rev[:12]
	}
	if len(os.Args) > 1 && os.Args[1] == "--stamp" {
		fmt.Println("vcs.modified =", dirty)
		return
	}
	if dirty {
		fmt.Println(rev + "-dirty")
		return
	}
	fmt.Println(rev)
}
`

// writeGuardProbeModule writes a nested go module under repo and commits it,
// returning the module directory.
func writeGuardProbeModule(t *testing.T, repo string) string {
	t.Helper()
	src := filepath.Join(repo, "probe")
	if err := os.MkdirAll(filepath.Join(src, "cmd", "p"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src, "go.mod"), []byte("module probe\n\ngo 1.24\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src, "cmd", "p", "main.go"), []byte(rebuildGuardProbeMainSrc), 0o644); err != nil {
		t.Fatal(err)
	}
	rebuildTestGit(t, repo, "add", "probe")
	rebuildTestGit(t, repo, "commit", "-q", "-m", "probe")
	return src
}

// TestRunBuildStampsCleanUnderUntrackedOnly is the END-TO-END red-proof of
// the second half: a real `go build` over a tree that has ONLY untracked
// files must NOT stamp vcs.modified=true. The build command is a tiny Go
// program reporting its own buildinfo, so the assertion is on the toolchain's
// actual stamp, not on a simulation.
func TestRunBuildStampsCleanUnderUntrackedOnly(t *testing.T) {
	if _, err := exec.LookPath("go"); err != nil {
		t.Skip("go toolchain not on PATH")
	}
	repo, _ := rebuildTestRepo(t)
	src := writeGuardProbeModule(t, repo)

	// The dirty-tree state under test: untracked scratch ONLY.
	if err := os.WriteFile(filepath.Join(repo, "scratch.md"), []byte("x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	outBin := filepath.Join(repo, "probe-out")
	buildCmd := "cd " + src + " && go build -o " + outBin + " ./cmd/p && " + outBin + " --stamp"

	exit, out := runBuild(repo, buildCmd, 120)
	if exit != 0 {
		t.Fatalf("probe build failed (exit %d):\n%s", exit, out)
	}
	if !strings.Contains(out, "vcs.modified = false") {
		t.Fatalf("GO STAMPED THE BINARY DIRTY from untracked-only scratch — the rebuild's "+
			"version assertion would fail and auto-revert a good merge. build output:\n%s", out)
	}
}

// TestRunBuildStampsDirtyUnderTrackedDirt is the paired control: a TRACKED
// modification still stamps -dirty (the guard exists for exactly this case,
// so the env must not blind it).
func TestRunBuildStampsDirtyUnderTrackedDirt(t *testing.T) {
	if _, err := exec.LookPath("go"); err != nil {
		t.Skip("go toolchain not on PATH")
	}
	repo, _ := rebuildTestRepo(t)
	src := writeGuardProbeModule(t, repo)

	// The state under test: a TRACKED modification.
	if err := os.WriteFile(filepath.Join(repo, "f.txt"), []byte("edited\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	outBin := filepath.Join(repo, "probe-out")
	buildCmd := "cd " + src + " && go build -o " + outBin + " ./cmd/p && " + outBin + " --stamp"

	exit, out := runBuild(repo, buildCmd, 120)
	if exit != 0 {
		t.Fatalf("probe build failed (exit %d):\n%s", exit, out)
	}
	if !strings.Contains(out, "vcs.modified = true") {
		t.Fatalf("a TRACKED modification must still stamp -dirty (the guard's reason to exist):\n%s", out)
	}
}

// TestRebuildE2EUntrackedOnlyBinFollowsMasterAfterMerge is the epic's
// acceptance criterion in its strongest form, end to end on a real
// toolchain: a merge round lands a branch into master while the repo holds
// untracked artifacts, and RebuildAfterMerge must produce a bin/gopi whose
// version EQUALS the new master tip.
//
// The build command is a REAL `go build` of a probe module that copies
// itself to bin/gopi and prints buildVersion-equivalent output, so the
// actual vcs stamping path runs. Before the tracked-only guard this test
// never reached the build at all (the untracked artifacts made the guard
// defer forever); with only the guard fixed it would build a "-dirty"
// binary and fail the version assertion.
func TestRebuildE2EUntrackedOnlyBinFollowsMasterAfterMerge(t *testing.T) {
	if _, err := exec.LookPath("go"); err != nil {
		t.Skip("go toolchain not on PATH")
	}
	repo, _ := rebuildTestRepo(t)
	writeGuardProbeModule(t, repo)

	// A merge round lands a feature branch into master (as the sweep does).
	rebuildTestGit(t, repo, "checkout", "-q", "-b", "feat/x")
	if err := os.WriteFile(filepath.Join(repo, "feat.txt"), []byte("feature\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	rebuildTestGit(t, repo, "add", "feat.txt")
	rebuildTestGit(t, repo, "commit", "-q", "-m", "feat: x")
	rebuildTestGit(t, repo, "checkout", "-q", "master")
	rebuildTestGit(t, repo, "merge", "-q", "--no-ff", "feat/x", "-m", "Merge branch 'feat/x'")
	tip := rebuildTestGit(t, repo, "rev-parse", "--short", "HEAD")

	// The busy task field's permanent state: untracked artifacts, zero
	// tracked changes.
	for _, name := range []string{"briefings/b.md", "scratch.md"} {
		p := filepath.Join(repo, name)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte("untracked\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if tracked := rebuildTestGit(t, repo, "status", "--porcelain", "--untracked-files=no"); tracked != "" {
		t.Fatalf("fixture must have zero tracked changes, got %q", tracked)
	}

	// The declared build: a real go build whose produced binary reports the
	// toolchain's vcs stamp (GoPi convention: bin/gopi + --version).
	probeSrc := filepath.Join(repo, "probe")
	buildCmd := "mkdir -p " + filepath.Join(repo, "bin") + " && cd " + probeSrc +
		" && go build -o " + filepath.Join(repo, "bin", "gopi") + " ./cmd/p"
	rebuildTestSettings(t, map[string]map[string]any{
		repo: {"build": buildCmd, "build_timeout_sec": 180},
	})

	res := RebuildAfterMerge(repo)
	if res.Skipped {
		t.Fatalf("expected the declared build to run, got a skip: %+v", res)
	}
	if !res.OK {
		t.Fatalf("untracked-only tree must NOT fail the rebuild (err=%q)", res.Err)
	}
	// res.ActualVersion is the DISPLAY (`--version`'s answer: the tagged name
	// plus its revision group); the verifier compares the revision it
	// recovers, so the assertion reads that revision out (never the whole
	// display — a tagged display never equals a bare tip string).
	gotRev, gotDirty, gotOK := gopiversion.RevisionOf(res.ActualVersion)
	if !gotOK || gotDirty {
		t.Fatalf("binary display %q carries no clean revision (rev=%q dirty=%v ok=%v)", res.ActualVersion, gotRev, gotDirty, gotOK)
	}
	if !versionMatches(gotRev, tip) {
		t.Fatalf("binary version %q does not follow the new master tip %q", res.ActualVersion, tip)
	}
	// The stamp must be clean: a "-dirty" binary is what the tracked-only
	// build env exists to prevent (versionMatches rejects it by contract).
	if strings.HasSuffix(res.ActualVersion, gopiversion.DirtySuffix) {
		t.Fatalf("untracked-only tree stamped the binary -dirty: %q", res.ActualVersion)
	}
}

// rebuildGuardProbeBuildBody is kept out of the gate's error-shape scope by
// being a plain string constant (see rebuildGuardProbeMainSrc above).
