package tools

// Failed-merge auto-revert (epic merger-failure-dual-mechanism, the operator
// 2026-09-08 mechanism 2 — "merger is responsible to maintain master
// buildable. if new merging failure causing master not buildable, then
// revert the failed merging branch back to original buildable state,
// period.").
//
// When a merger sweep round lands ≥1 branch into master and the post-merge
// rebuild FAILS, the landed merge leaves master UNBUILDABLE — the exact
// regression the operator's mechanism 2 eliminates. This helper restores master to
// the PRE-SWEEP buildable tip and re-creates the branch refs the sweep
// deleted on landing (git branch -d), so the branch WORK survives for a
// re-developer even though the merge itself is unwound.
//
// Reset vs revert-commit: the sweep merges with plain `git merge <branch>`
// (taskMergeOne), which on a local worker branch whose tip contains master
// is a FAST-FORWARD — no merge commit exists to `git revert -m 1`. For a
// fast-forward landing the only sound restore of "master buildable" is a
// hard reset to the pre-sweep master tip. Master here is the throwaway
// integration branch (not a shared published history), and every content
// change is preserved: merge commits come from branches whose refs are
// re-created at their pre-merge tips. So no work is lost by the reset; the
// throwaway master simply returns to the last buildable state, exactly
// the operator's verdict.

import (
	"context"
	"fmt"
	"os/exec"
	"sort"
	"strings"
	"time"
)

// --- failed-merge revert tunables (Rule 1: every literal in one place) ---

const (
	// mergeRevertGitTimeout bounds each git invocation in the revert —
	// a wedged repo must degrade to an error, never stall the sweep end.
	mergeRevertGitTimeout = 30 * time.Second
)

// FailedMergeRevert carries the captured pre-sweep state needed to restore
// master after the rebuild of a landing merge FAILS. The merger sweep
// caller captures it BEFORE running the sweep (so master's advance and the
// merged branch refs' deletion can be rolled back afterward):
//
//	PreTip     — master's tip immediately before this sweep round's merges
//	             (git rev-parse HEAD). The buildable state to restore.
//	BranchTips — short branch name → its commit sha at sweep start, for
//	             EVERY unprotected branch that was a merge candidate. The
//	             restore re-creates any that the landing deleted (branch -d),
//	             so no landed branch's work is lost to the reset.
type FailedMergeRevert struct {
	Repo   string
	PreTip string
	// NOTE (epic version-identity, 2026-09-24): this handle used to carry
	// MintedTag — the version tag a round minted at the position a revert
	// unwinds — and MergedBranches, the branch list the mint's MESSAGE was
	// built from. Both fed `gopi merge`'s version-tag ritual; with that
	// ritual deleted (internal/version/self.go carries gopi's own identity
	// now, and no merge path writes a tag) neither has a reader: MintedTag
	// was the tag DROP in RestoreMaster step ③, and MergedBranches reached
	// only the deleted mint. They are removed rather than left as dormant
	// fields, because a dormant field is the same coupling with a bigger
	// surface — nothing in a revert may touch a tag (Rule 26).
	BranchTips map[string]string
}

// RevertNotify fires the merge-reverted synthetic alarm (event
// merge-reverted) when the merger auto-reverts a landing whose rebuild
// failed — master was restored to buildable, but an operator/PA must know a
// would-be merge was unwound so the underlying branch gets reworked.
// Best-effort: delivery failure logs inside NotifySyntheticEvent, never fatal.
func RevertNotify(repo, restoredTip, cmd string, exit int, errMsg string) {
	NotifySyntheticEvent(RevertedEvent, repo, "", fmt.Sprintf(
		"merge REVERTED after build failure: repo=%s restored=%s landed-build cmd=%q exit=%d err=%s",
		repo, restoredTip, cmd, exit, errMsg))
}

// RevertedEvent is the synthetic notify event fired when the merger
// auto-reverts a merge whose post-merge rebuild failed (mechanism 2).
// Subscribers add it to GOPI_TASK_NOTIFY_EVENTS (like "build-failed").
const RevertedEvent = "merge-reverted"

// mergeRevertGit runs one git command in repo with the shared timeout.
func mergeRevertGit(repo string, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), mergeRevertGitTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, "git", append([]string{"-C", repo}, args...)...)
	var stderr strings.Builder
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("git %s: %w: %s", strings.Join(args, " "),
			err, strings.TrimSpace(stderr.String()))
	}
	return strings.TrimSpace(string(out)), nil
}

// FailedMergeRevertCapture is a merger sweep caller's pre-round snapshot of
// the state a failed landing must restore: master's pre-sweep tip plus the
// branch→tip map of every unprotected branch about to be a merge candidate.
// Capture BEFORE running the sweep round; on a rebuild failure of a round
// that landed work, the caller builds a FailedMergeRevert from it.
type FailedMergeRevertCapture struct {
	// PreTip is master's HEAD before the sweep (the buildable state).
	PreTip string
	// BranchTips maps each unprotected short branch name → its commit sha
	// at capture time, so RestoreMaster can re-create refs the landing
	// deleted.
	BranchTips map[string]string
}

// CaptureFailedMergeRevert snapshots the pre-sweep restore state for repo's
// unprotected branches (the same exclusion the sweep ① uses: not the
// integration branch, not an archive-ledger hp-unique-*). Returns a capture
// with no PreTip on a pre-flight git error — the caller degrades to "no
// auto-revert possible this round" (log + continue), never aborts.
func CaptureFailedMergeRevert(repo string) FailedMergeRevertCapture {
	c := FailedMergeRevertCapture{BranchTips: map[string]string{}}
	head, err := mergeRevertGit(repo, "rev-parse", "HEAD")
	if err != nil {
		return c // PreTip stays "" → caller skips auto-revert this round
	}
	c.PreTip = head

	out, err := mergeRevertGit(repo, "for-each-ref",
		"--format=%(refname:short) %(objectname)", "refs/heads")
	if err != nil {
		c.PreTip = "" // branch snapshot failed → no reliable restore state
		return c
	}
	master := "master"
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) != 2 {
			continue
		}
		name := fields[0]
		if name == master || name == "main" || name == "HEAD" ||
			strings.HasPrefix(name, "hp-unique-") {
			continue
		}
		c.BranchTips[name] = fields[1]
	}
	return c
}

// Retriable reports whether a restore should even be attempted:
// r.PreTip is set AND master has actually advanced past it since the
// capture (current HEAD != PreTip). A nil PreTip (caller had no revert
// context) or an already-reset master means nothing to unwind — the
// regression guard that stops a sweep from resetting a master someone
// else advanced after this round's own merges landed clean.
func (r *FailedMergeRevert) Retriable() bool {
	if r == nil || r.Repo == "" || r.PreTip == "" {
		return false
	}
	head, err := mergeRevertGit(r.Repo, "rev-parse", "HEAD")
	if err != nil || head == r.PreTip {
		return false
	}
	return true
}

// RestoreMaster unwinds the sweep round that left master unbuildable: it
// re-creates every branch the landing deleted (BranchTips entries whose ref
// no longer exists) and resets master hard back to the pre-sweep buildable
// PreTip. Returns the branch refs re-created ("" slice when none needed).
// The reset's --hard is safe here: the sweep owns the repo's working tree
// this turn (serveRebuildAfterMerge already ran the dirty-tree guard), and
// the caller only invokes RestoreMaster after a genuine rebuild FAILURE of
// merges this round landed — not speculatively.
func (r *FailedMergeRevert) RestoreMaster() ([]string, error) {
	if r == nil || r.Repo == "" {
		return nil, fmt.Errorf("merge revert: no repo")
	}
	if r.PreTip == "" {
		return nil, fmt.Errorf("merge revert: no pre-sweep tip captured")
	}

	// ① Re-create branch refs that the landing deleted (git branch -d in
	// taskMergeOne). Skip refs that still exist — a merge may only have
	// deleted SOME of the captured candidates; a living branch (e.g. one
	// that conflicts and stayed, or a second merge round's re-created ref)
	// must not be clobbered. Only refs GONE from refs/heads are restored,
	// at their captured pre-merge tips — branches are preserved (epic).
	var restored []string
	if len(r.BranchTips) > 0 {
		names, nerr := mergeRevertGit(r.Repo, "for-each-ref", "--format=%(refname:short)", "refs/heads")
		if nerr != nil {
			return nil, fmt.Errorf("merge revert: list branches: %w", nerr)
		}
		living := make(map[string]bool)
		for _, n := range strings.Fields(names) {
			living[n] = true
		}
		var gone []string
		for name := range r.BranchTips {
			if !living[name] {
				gone = append(gone, name)
			}
		}
		sort.Strings(gone)
		for _, name := range gone {
			if _, err := mergeRevertGit(r.Repo, "branch", name, r.BranchTips[name]); err != nil {
				return nil, fmt.Errorf("merge revert: re-create branch %s: %w", name, err)
			}
			restored = append(restored, name)
		}
	}

	// ② Reset master hard to the pre-sweep buildable tip. The merge commits
	// (fast-forwards, or the occasional real merge of a diverged branch)
	// are unwound; master returns to the state the binary already matched.
	if _, err := mergeRevertGit(r.Repo, "reset", "--hard", r.PreTip); err != nil {
		return nil, fmt.Errorf("merge revert: reset master to %s: %w", r.PreTip, err)
	}
	// NOTE (epic version-identity, 2026-09-24): a step ③ used to stand here —
	// it dropped the `version/*` tag a pre-rebuild mint had laid on the
	// unwound merge. The merge path no longer mints a tag at all (gopi's
	// identity is internal/version.Version, compiled into the binary), so
	// there is nothing left to drop and NO revert path touches a tag: a tag
	// in a repo gopi merged is the repo owner's, never gopi's to delete.
	return restored, nil
}
