package tools

// Parsed stdout vs diagnostic stderr — ONE reader for every git read whose
// OUTPUT IS PARSED (epic git-parsed-stdout-only, Rule 14: one definition, no
// per-caller copy).
//
// THE DEFECT THIS FILE EXISTS TO REMOVE (measured 2026-10-26): the dirty guard
// read `git status --porcelain` through `finishCleanupGitRaw`, which returned
// `strings.TrimSpace(string(cmd.CombinedOutput()))`. `CombinedOutput` merges
// git's stderr into the SAME body, so any stderr text becomes the "porcelain"
// body. Measured consequence: `GIT_TRACE=1 go test ./internal/tools/ -run
// TestSelfDelivery` failed 4/5 — the trace line
// `06:54:17.718401 git.c:463               trace: built-in: git status
// --porcelain` was read as the porcelain body, so `reclaimDirtyVerdict`
// classified a PROVABLY CLEAN worktree as `reclaimSkipDirtyReason`
// ("worktree is DIRTY (uncommitted work)") and refused to release it.
//
// THE RULE THAT FOLLOWS: a helper whose RETURN VALUE IS PARSED must never see
// git's stderr — stdout is the verdict, stderr is diagnostics. A helper whose
// value is only PRINTED (a `worktree remove` / `worktree prune` statement that
// goes into a report) may keep CombinedOutput: git's own sentence is the
// deliverable there.
//
// WHY ONE HELPER AND NOT A FLAG: a `combined bool` parameter on one function is
// the same latent coupling (a caller opts back into the merged stream by
// accident of its argument list), so "I need both streams" and "show me
// everything" are two FUNCTIONS. The struct shape is what makes the mistake
// unavailable rather than merely discouraged.

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"strings"
	"time"
)

// Parsed-read vocabulary (Rule 1/13: no bare literals at a call site).
const (
	// gitParsedShowTopLevelFlag makes `git rev-parse` print the working tree's
	// root — a PATH, therefore parsed.
	gitParsedShowTopLevelFlag = "--show-toplevel"

	// gitParsedCommonDirFlag makes `git rev-parse` print the shared git dir (the
	// identity of a repository across its main checkout and every linked
	// worktree) — also a PATH, also parsed.
	gitParsedCommonDirFlag = "--git-common-dir"

	// gitParsedPathFormatAbsoluteFlag makes `git rev-parse --git-common-dir`
	// answer an ABSOLUTE path instead of a repo-relative one. The result is a
	// PATH — parsed.
	gitParsedPathFormatAbsoluteFlag = "--path-format=absolute"
)

// gitParsedOutputStreams is a git invocation's two streams, KEPT APART: Stdout
// is the value a caller PARSES, Stderr is diagnostics that never reach a
// parser. Neither is ever the other.
type gitParsedOutputStreams struct {
	Stdout string
	Stderr string
}

// finishCleanupGitParsedStreams runs one git command and returns BOTH streams
// unmerged. It is the reader behind every parsed read in this package.
//
// On a non-zero exit the error carries git's own sentence (the first non-empty
// stderr line, or stdout's when git said nothing on stderr) — a refusal that
// quotes git must still be able to, and it gets that text from the ERROR, never
// by parsing a merged body (Rule 15: stderr is not swallowed).
//
// `timeout` is passed in by the caller from the package constant that already
// governs that lane (Rule 2: one default per lane, no second place decides it).
func finishCleanupGitParsedStreams(ctx context.Context, dir string, timeout time.Duration, args ...string) (gitParsedOutputStreams, error) {
	out := gitParsedOutputStreams{}
	cctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	cmd := exec.CommandContext(cctx, gitBinaryName, args...)
	cmd.Dir = dir
	// Two BUFFERS, not one: `exec.Cmd` copies each stream on its own goroutine,
	// so neither can block the child and neither can land in the other's body.
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	// Read the buffers AFTER Run: exec's copies finish before Run returns.
	out.Stdout = strings.TrimSpace(stdout.String())
	out.Stderr = strings.TrimSpace(stderr.String())
	if err != nil {
		diag := finishCleanupFirstLine([]byte(out.Stderr))
		if diag == finishCleanupNoOutput {
			diag = finishCleanupFirstLine([]byte(out.Stdout))
		}
		return out, fmt.Errorf("git %s: %w: %s", strings.Join(args, " "), err, diag)
	}
	return out, nil
}

// finishCleanupGitParsedStdout is the single-value form for the common case:
// one parsed stdout body, stderr folded into the error only. Every dirty /
// porcelain / rev-parse / worktree-list reader goes through THIS function or
// its two-stream sibling above (Rule 14) — never through a merged stream.
func finishCleanupGitParsedStdout(ctx context.Context, dir string, timeout time.Duration, args ...string) (string, error) {
	out, err := finishCleanupGitParsedStreams(ctx, dir, timeout, args...)
	if err != nil {
		return "", err
	}
	return out.Stdout, nil
}
