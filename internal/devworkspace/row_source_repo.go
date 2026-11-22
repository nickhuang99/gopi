package devworkspace

// row_source_repo.go — WHICH REPOSITORY DOES THIS ROW CLONE FROM (and therefore
// push to)?
//
// THE DEFECT (measured 2026-11-22, row 01a20059-fc11-75fe-aba6-49767b263f25).
// The channel opened a ground from `settings dev_repo` — this host declares
// `peer1:/home/user/work/gopi` — while that row's own `deliverable.repo` was
// `/home/user/work/sudoku`. Two repositories, nothing in common:
//
//	$ gopi task show 01a20059-…
//	deliverable.repo: /home/user/work/sudoku
//	deliverable.worktree: /home/user/workspace/01a20059-…
//	$ cd /home/user/workspace/01a20059-… && git remote -v && git log -1 --format='%s'
//	origin  peer1:/home/user/work/gopi (fetch)
//	origin  peer1:/home/user/work/gopi (push)
//	… fix(task): --result 是从属 flag，按名拒绝而不是静默丢弃
//
// So the ground carried the GOPI repository's code while the row was about a
// sudoku feature: the work the row asked for could not be written in the tree
// the row was given, and the delivery loop would have pushed that gopi clone's
// `master` onto the gopi server — a publish that has nothing to do with the
// row's deliverable.
//
// THE ANSWER, AND IT IS THE ORDER reposource ALREADY STATES (operator ruling
// 2026-10-23, internal/reposource): the row's own `deliverable.repo` comes
// FIRST — the row already says where it works — and the host's `dev_repo` is
// the FALLBACK, not an override. The channel's ground is that repository's
// clone, so "which repo does the row work in" and "which repo does this run
// clone from" are one question asked once (Rule 13/14: one resolver, and both
// the spawn leg and the delivery leg take the answer from HERE).
//
// THE THREE DECLINES, each naming its own measured reason (Rule 15). The row's
// repo does NOT decide when:
//
//   - it is empty — the row names no repository at all;
//   - it lies UNDER the workspace root — that is a run's own clone, and the
//     channel's clones are reclaimed when their push lands, so cloning one is
//     cloning a directory that may already be gone (the dead-path family,
//     epic 01a15629). The provenance of such a checkout is a different question
//     and it already has an owner (`TraceToSource`, used by the build lookup);
//   - nothing usable is on it — missing, or not a git work tree
//     (`RepoPathReadable`, the same probe the build lookup uses).
//
// It writes nothing and spawns nothing: the whole resolver is a stat plus a
// string comparison, so a run pays exactly the price it paid before.

import (
	"fmt"
	"strings"

	"github.com/nickhuang99/gopi/internal/config"
	"github.com/nickhuang99/gopi/internal/worktreeroots"
)

const (
	// CloneSourceArmRowRepo / CloneSourceArmDevRepo are the two arms' NAMES, and
	// the narration prints them: a decision is never anonymous (Rule 15's
	// positive half).
	CloneSourceArmRowRepo = "the row's deliverable.repo"
	CloneSourceArmDevRepo = config.DevRepoSettingsKey

	// CloneSourceDeclinedEmpty / …InsideRoot / …Unreadable are the reasons the
	// row's repo does not decide, spelled once so the narration and the test
	// cannot drift apart (Rule 1b).
	CloneSourceDeclinedEmpty      = "the row names no repository"
	CloneSourceDeclinedInsideRoot = "it lies under the workspace root, where the channel's own reclaimable clones live"
	CloneSourceDeclinedUnreadable = "nothing usable is on it (%s)"

	// CloneSourceLinePrefix opens every clone-source narration line, so a caller
	// (and a test) can recognise the line without copying the format's wording.
	CloneSourceLinePrefix = "clone source: "

	// CloneSourceRowWinsFmt reports the FIRST arm: the row's repository decided
	// the clone source, and the host's declaration does not apply.
	// Arity: the repo, the arm name, the workspace root, the settings key, the
	// declared dev_repo.
	CloneSourceRowWinsFmt = CloneSourceLinePrefix + "%q (from %s — it is a git work tree outside the workspace root %q, " +
		"so %s=%q does not decide this run)"

	// CloneSourceDevWinsFmt reports the FALLBACK, and says WHY the row's repo
	// did not decide — a fallback nobody can attribute is a silent switch.
	// Arity: the repo, the arm name, the row's repo, the decline reason.
	CloneSourceDevWinsFmt = CloneSourceLinePrefix + "%q (from %s — the row's deliverable.repo %q did not decide: %s)"
)

// CloneSource is what ONE run clones from and delivers to.
type CloneSource struct {
	// Repo is the value to hand to Config.DevRepo: the clone origin, and — for
	// the clone channel — the destination its push moves.
	Repo string
	// Arm is CloneSourceArmRowRepo or CloneSourceArmDevRepo.
	Arm string
	// Note is the ONE narration line, already rendered (empty when the row named
	// no repository at all: there is nothing to explain).
	Note string
}

// ResolveCloneSource answers "which repository does THIS row clone from?".
//
// The order is reposource's: the row's deliverable.repo when it is a usable
// work tree of its own, else the host's dev_repo. `root` is the workspace root
// the channel materialises clones under — the boundary that makes a path "the
// channel's own clone" rather than "a repository the row works in".
func ResolveCloneSource(rowRepo, root, devRepo string) CloneSource {
	rowRepo, root, devRepo = strings.TrimSpace(rowRepo), strings.TrimSpace(root), strings.TrimSpace(devRepo)
	declined := ""
	switch {
	case rowRepo == "":
		declined = CloneSourceDeclinedEmpty
	case root != "" && worktreeroots.Within(rowRepo, root):
		declined = CloneSourceDeclinedInsideRoot
	default:
		readable, why := RepoPathReadable(rowRepo)
		if readable {
			return CloneSource{Repo: rowRepo, Arm: CloneSourceArmRowRepo, Note: fmt.Sprintf(
				CloneSourceRowWinsFmt, rowRepo, CloneSourceArmRowRepo, root, config.DevRepoSettingsKey, devRepo)}
		}
		declined = fmt.Sprintf(CloneSourceDeclinedUnreadable, why)
	}
	note := ""
	if rowRepo != "" {
		note = fmt.Sprintf(CloneSourceDevWinsFmt, devRepo, CloneSourceArmDevRepo, rowRepo, declined)
	}
	return CloneSource{Repo: devRepo, Arm: CloneSourceArmDevRepo, Note: note}
}
