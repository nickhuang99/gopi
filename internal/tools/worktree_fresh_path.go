package tools

// worktree_fresh_path.go — A WORKTREE PATH MUST NOT EXIST BEFORE WE CREATE IT.
//
// THE DEFECT, MEASURED. `git worktree add <path>` protects its target only
// PARTIALLY:
//
//	$ mkdir -p <path> && touch <path>/EXISTING          # NON-empty target
//	$ git worktree add <path> -b probe/collide
//	fatal: '<path>' already exists                      # REFUSED
//
//	$ mkdir -p <path2>                                  # EMPTY target
//	$ git worktree add <path2> -b probe/collide2
//	Preparing worktree (new branch 'probe/collide2')
//	Updating files: 100% (4276/4276), done.             # SILENTLY REUSED
//
// So a leftover EMPTY directory — a GC-reaped scratch dir, an aborted setup, a
// `mkdir -p` in a script, the previous run's residue — becomes the new
// worktree's home without a word. A worktree's identity IS its path, so a
// REUSED path is a SHARED path: the motivating incident found
// ~/workspace/gopi-tasksync-switch carrying three different tasks' work, and
// #11663 (this repo's own epic 01a0c656 worktree half) is the same shape one
// level down — a fresh worktree's `git status` inherited another checkout's
// file.
//
// WHAT THIS FILE ADDS, AND WHAT IT DELIBERATELY DOES NOT. Exactly ONE
// predicate about EXISTENCE-BEFORE-CREATION, and a renderer for its error.
// Every other question about a worktree directory already has an owner and is
// NOT re-asked here:
//
//   - is the directory IN USE / HELD by a live process — task_reclaim*.go
//     (reclaimWorktreeRegisteredIn, the live-holder veto) and
//     task_finish_cleanup.go (finishCleanupRemovalStateReason,
//     taskLiveClaims.anyLiveRowOf) ask it of a path that EXISTS;
//   - is it LOCKED / INITIALIZING (a SIGKILLed `worktree add`'s
//     `.git/worktrees/<id>/locked`), is it REGISTERED, is it ORPHANED, what is
//     its AGE — worktree_sweep.go (worktreeSweepRegistrationLocked,
//     worktreeSweepLockAge, worktreeDropStaleLock) and worktree_sweep.go's
//     reclaim arm own all of it, again for a path that EXISTS;
//   - is the path inside a DECLARED ROOT at all — internal/tools/path_policy.go
//     (judgeTarget / worktreeroots.PermitsAny) owns that, including for the
//     bash-tool spelling of `git worktree add`.
//
// Those guards are all POST-EXISTENCE: each one needs a directory to inspect.
// The hole this file closes is the moment BEFORE any of them has a subject —
// the creation itself. Nothing here weakens, duplicates or bypasses them, and
// nothing here removes a pre-existing path: a human decides (Rule 25's spirit —
// a refusal names the remedy, a privilege or a cleanup is never taken for the
// operator).

import (
	"errors"
	"fmt"
	"os"
	"strings"
)

// worktreeFreshPathExhaustedMarker is the stable token the refusal carries, for
// a caller (or a test) that must recognise the refusal without parsing prose.
// Rule 1b: the spelling lives here, never inline at a call site.
const worktreeFreshPathExhaustedMarker = "worktree-path-not-fresh"

// worktreeFreshPathRemedy is the ONE operator-facing remedy sentence (Rule 1b:
// one spelling, rendered by every refusal). It is deliberately a HUMAN step:
// the path must be inspected and removed by a person, because the thing
// standing there may be somebody's live work — a guard that cleaned up after
// itself would be the very data loss this predicate exists to prevent.
const worktreeFreshPathRemedy = "choose another path, or inspect the existing entry " +
	"(it may be another run's live work) and remove it by hand before retrying — this guard never deletes it for you"

// errWorktreePathExhausted is the NAMED error `worktreeFreshPath` returns.
// errors.Is(err, ErrWorktreePathExhausted) is the machine-readable half; the
// wrapped message is the human half, and it always carries the offending path.
var errWorktreePathExhausted = errors.New(worktreeFreshPathExhaustedMarker)

// ErrWorktreePathExhausted is the exported identity of the refusal, so a caller
// outside this package (cmd/agent's merge faces) can branch on it rather than
// on strings.
var ErrWorktreePathExhausted = errWorktreePathExhausted

// worktreeFreshPath refuses a worktree target path that ALREADY EXISTS, in any
// form, and returns nil when nothing is there.
//
// os.Lstat, never os.Stat: a DANGLING SYMLINK at the path must be refused too.
// `Stat` follows the link, so a link whose target does not exist reads as
// "absent" and `git worktree add` would then write THROUGH the link — the
// checkout would land wherever the link points, which is by construction not
// the path the caller believes it owns. os.Lstat answers the question actually
// being asked: does anything occupy this path?
//
// The predicate is deliberately the STRONGEST form — "must not exist", not
// "must not be a worktree" and not "must be empty":
//
//   - an existing EMPTY dir is indistinguishable from one that is empty RIGHT
//     NOW (a concurrent creator mid-setup, a directory whose contents were just
//     reaped), and git lets exactly that case through;
//   - "does not exist" is the only property that survives a race with the
//     sweepers, which create and remove directories under the same roots.
//
// A nil error means the path is ABSENT — never "absent or whatever, proceed".
// `git worktree add` refuses a non-empty target on its own, so the residual
// case this leaves to git is the one git already handles; what this predicate
// adds is the EMPTY case, which git does not.
func worktreeFreshPath(path string) error {
	p := strings.TrimSpace(path)
	if p == "" {
		return fmt.Errorf("worktree path is empty: %w (nothing to check before creation)", errWorktreePathExhausted)
	}
	info, err := os.Lstat(p)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		// A path that cannot be stat'd is NOT assumed absent: an unverifiable
		// target must never be created (the same fail-closed rule
		// path_policy.go's judgeTarget applies to an unresolvable target).
		return fmt.Errorf("worktree path %s cannot be examined (%w): %w", p, err, errWorktreePathExhausted)
	}
	return fmt.Errorf("worktree path %s already exists (%s): %w — %s",
		p, worktreeFreshPathKind(info), errWorktreePathExhausted, worktreeFreshPathRemedy)
}

// CheckWorktreePathFresh is the SAME predicate as worktreeFreshPath, exported
// for the call sites outside this package (cmd/agent's merge probe faces run
// `git worktree add` themselves and cannot reach an unexported helper). It is a
// delegation, never a second implementation (Rules 13/14): the check, the error
// identity and the remedy sentence all have exactly one home, above.
//
// It is called `Check…` and not `…FreshPath` because cmd/agent must not reshape
// the predicate; the name it carries is the one this file's tests pin.
func CheckWorktreePathFresh(path string) error { return worktreeFreshPath(path) }

// worktreeFreshPathKind names what is standing at the path, so the refusal says
// WHAT is in the way rather than only that something is. A symlink is reported
// as a symlink even when its target is a directory (Lstat does not follow), and
// it is checked FIRST because a link-to-a-directory would otherwise be
// reported as a plain directory and hide the write-through-the-link hazard.
func worktreeFreshPathKind(info os.FileInfo) string {
	switch mode := info.Mode(); {
	case mode&os.ModeSymlink != 0:
		return "a symbolic link"
	case mode.IsDir():
		return "a directory"
	case mode.IsRegular():
		return "a regular file"
	default:
		return "a " + mode.String()
	}
}
