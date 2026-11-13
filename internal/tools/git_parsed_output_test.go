package tools

// Regression guard for the defect measured 2026-10-26 (epic
// git-parsed-stdout-only): git writes its TRACE lines to STDERR, and a reader
// that returned `strings.TrimSpace(string(cmd.CombinedOutput()))` therefore
// handed git's diagnostics to a PARSER. `GIT_TRACE=1 go test ./internal/tools/
// -run TestSelfDelivery` failed 4/5 on a PROVABLY CLEAN worktree, because the
// trace line `… git.c:463 trace: built-in: git status --porcelain` became the
// "porcelain" body and `reclaimDirtyVerdict` rendered `reclaimSkipDirtyReason`
// ("worktree is DIRTY (uncommitted work)").
//
// The test is TWO-SIDED on purpose, so neither half can pass by measuring
// nothing:
//
//	RED arm   — the PRE-FIX shape (one merged stream) must SEE the pollution.
//	            If git stops writing trace to stderr, this arm fails and the
//	            guard says so instead of silently proving nothing.
//	GREEN arm — the shared parsed readers must read the SAME polluted
//	            invocation as CLEAN, and must still read real dirt as DIRTY.

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// gitPollutionTraceEnv is the env var that makes git emit trace lines on stderr
// — the injection channel this guard uses (git's own documented tracing knob).
const gitPollutionTraceEnv = "GIT_TRACE"

// gitParsedTestFile is the one committed file the fixture repo holds.
const gitParsedTestFile = "tracked.txt"

// gitParsedTestDirtFile is the file the DIRTY arm adds (untracked ⇒ the reader
// must report it; the point is only that the body is non-empty and REAL).
const gitParsedTestDirtFile = "untracked-residue.txt"

// gitParsedTestGit runs one git command in repo and fails the test on error.
func gitParsedTestGit(t *testing.T, repo string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = repo
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %s: %v: %s", strings.Join(args, " "), err, strings.TrimSpace(string(out)))
	}
}

// gitParsedTestRepo builds a repo with exactly ONE commit and a CLEAN tree.
func gitParsedTestRepo(t *testing.T) string {
	t.Helper()
	repo := t.TempDir()
	gitParsedTestGit(t, repo, "init", "-q")
	gitParsedTestGit(t, repo, "config", "user.email", "git-parsed@example.invalid")
	gitParsedTestGit(t, repo, "config", "user.name", "git parsed output guard")
	if err := os.WriteFile(filepath.Join(repo, gitParsedTestFile), []byte("content\n"), 0o600); err != nil {
		t.Fatalf("write %s: %v", gitParsedTestFile, err)
	}
	gitParsedTestGit(t, repo, "add", gitParsedTestFile)
	gitParsedTestGit(t, repo, "commit", "-q", "-m", "fixture")
	return repo
}

// gitParsedTestMergedStream is the PRE-FIX read shape, kept HERE as the red
// arm's instrument: one merged stream, exactly what finishCleanupGitRaw did to
// every parsed read before this fix. It is not production code.
func gitParsedTestMergedStream(t *testing.T, repo string, args ...string) string {
	t.Helper()
	cmd := exec.CommandContext(context.Background(), gitBinaryName, args...)
	cmd.Dir = repo
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s: %v: %s", strings.Join(args, " "), err, strings.TrimSpace(string(out)))
	}
	return strings.TrimSpace(string(out))
}

func TestParsedGitReadsIgnoreStderrPollution(t *testing.T) {
	repo := gitParsedTestRepo(t)
	// Rule 17: t.Setenv, never os.Setenv — the scope is this test.
	t.Setenv(gitPollutionTraceEnv, "1")

	// ── RED arm: the pollution is REAL on this git. Without this, the green arm
	// below could pass because nothing was ever written to stderr.
	merged := gitParsedTestMergedStream(t, repo, finishCleanupStatusVerb, finishCleanupStatusPorcelainFlag)
	if merged == "" {
		t.Fatalf("the merged-stream read of a CLEAN tree returned an empty body, so %s=%s produced no "+
			"stderr pollution on this git — the guard would prove nothing (measured expectation: a "+
			"`trace: built-in: git status --porcelain` line arrives on stderr)", gitPollutionTraceEnv, "1")
	}
	if !strings.Contains(merged, finishCleanupStatusVerb) {
		t.Fatalf("the merged-stream body does not carry git's trace line, so it cannot demonstrate the "+
			"defect this guard exists for; got %q", merged)
	}

	// ── GREEN arm 1: the shared two-stream reader keeps them APART.
	streams, err := finishCleanupGitParsedStreams(context.Background(), repo, finishCleanupGitTimeout,
		finishCleanupStatusVerb, finishCleanupStatusPorcelainFlag)
	if err != nil {
		t.Fatalf("parsed streams: %v", err)
	}
	if streams.Stdout != "" {
		t.Fatalf("a CLEAN tree must have an EMPTY parsed stdout, got %q", streams.Stdout)
	}
	if strings.TrimSpace(streams.Stderr) == "" {
		t.Fatalf("the diagnostics stream is empty although %s=1 is in force — the separation is not "+
			"being measured", gitPollutionTraceEnv)
	}

	// ── GREEN arm 2: the dirty GUARD's own reader reads the same tree CLEAN.
	porcelain, err := reclaimGitStatus(repo)
	if err != nil {
		t.Fatalf("reclaimGitStatus: %v", err)
	}
	if v := reclaimDirtyVerdict(porcelain); v != reclaimStatusClean {
		t.Fatalf("the dirty guard read a CLEAN tree as %q (body %q) under %s=1 — stderr pollution is "+
			"still reaching the parser", v, porcelain, gitPollutionTraceEnv)
	}

	// ── GREEN arm 3: the SIBLING parsed readers (the sweep's and the merged-
	// cleanup sweep's dirty readers) answer clean through the same stream rule.
	if why := worktreeSweepDirtyReason(repo); why != "" {
		t.Fatalf("worktreeSweepDirtyReason reported a CLEAN tree as dirty: %q", why)
	}
	if why := mergedCleanupDirtyReason(repo); why != "" {
		t.Fatalf("mergedCleanupDirtyReason reported a CLEAN tree as dirty: %q", why)
	}
	// ── and the repo-root reader, whose value is a PATH (parsed too).
	root, err := repoRootOf(repo)
	if err != nil {
		t.Fatalf("repoRootOf: %v", err)
	}
	//stylegate:ignore this is the resolution PROBE itself: an unresolvable path falls through to the second comparison, which is the branch the arm is asserting
	if resolved, _ := filepath.EvalSymlinks(repo); filepath.Clean(root) != filepath.Clean(resolved) &&
		filepath.Clean(root) != filepath.Clean(repo) {
		t.Fatalf("repoRootOf returned %q for %q under %s=1 — a trace line reached the path parser",
			root, repo, gitPollutionTraceEnv)
	}

	// ── NO FALSE NEGATIVE: real dirt is still read as dirt under the SAME
	// pollution (the fix must not buy its green by making the reader blind).
	if err := os.WriteFile(filepath.Join(repo, gitParsedTestDirtFile), []byte("residue\n"), 0o600); err != nil {
		t.Fatalf("write %s: %v", gitParsedTestDirtFile, err)
	}
	dirty, err := reclaimGitStatus(repo)
	if err != nil {
		t.Fatalf("reclaimGitStatus (dirty): %v", err)
	}
	if !strings.Contains(dirty, gitParsedTestDirtFile) {
		t.Fatalf("an untracked file must still be read as dirt, got %q", dirty)
	}
	if v := reclaimDirtyVerdict(dirty); !strings.Contains(v, reclaimUntrackedResidueMarker) {
		t.Fatalf("an untracked-only tree must classify as residue, got %q", v)
	}
	if strings.Contains(dirty, "trace:") {
		t.Fatalf("the parsed body still carries git's stderr trace: %q", dirty)
	}
}
