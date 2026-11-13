package devworkspace

// ground_rebase.go — "is a rebase in progress in this ground?" as a MEASURED,
// THREE-VALUED fact, asked in exactly one place.
//
// WHY THIS FILE EXISTS (measured 2026-10-27, row 01a17a48). A ground summary
// line reported, in ONE sentence, that a directory was "a LINKED WORKTREE
// (.git is a FILE)" and that there was "No rebase in progress (no
// .git/rebase-merge|apply)". The two clauses cannot both rest on the same
// reading: when `.git` is a FILE — the linked-worktree shape — `.git/rebase-*`
// NEVER EXISTS, so a hand-built `ls .git/rebase-merge` cannot report "no rebase"
// there; it reports nothing at all, forever. The probe is only sound for the
// CLONE shape (`.git` a DIRECTORY), and the sentence did not know which shape it
// was in.
//
// THE FIX IS NOT A BETTER GUESS, it is asking the only party that knows: GIT.
//
//	$ git rev-parse --git-path rebase-merge
//	.git/rebase-merge                                            # CLONE ground
//	/main/.git/worktrees/<name>/rebase-merge                     # LINKED worktree
//
// The answer is per-worktree and shape-independent BY CONSTRUCTION, so the same
// command is correct for both ground models — which is exactly what a
// hand-joined `<dir>/.git/rebase-merge` is not. And when git cannot answer at
// all (not a repository, git missing, a broken worktree registration) the result
// is UNDETERMINED and SAYS SO: an unmeasured factor is never rendered as the
// reassuring answer (Rule 15 — silence is the defect).
//
// The shape half is not re-derived here either: `GroundModelOf` is the ONE
// discriminator (Rules 13/14), and `ResolveGroundState` now routes on it.

import (
	"context"
	"os"
	"path/filepath"
	"strings"
)

// RebaseState is the answer to "is a rebase in progress in this working tree?".
// It is THREE-valued on purpose: "none" is the reassuring answer and therefore
// the one that must never be reached by accident — a probe that could not run
// reports Undetermined, not None.
type RebaseState string

const (
	// RebaseStateNone: git named its rebase state directories and neither
	// exists — there is no rebase in progress.
	RebaseStateNone RebaseState = "none"
	// RebaseStateInProgress: one of git's own rebase state directories exists.
	// The probe names WHICH (a `rebase-merge` is an interactive/merge-backed
	// rebase, a `rebase-apply` is a patch-backed one).
	RebaseStateInProgress RebaseState = "in-progress"
	// RebaseStateUndetermined: the question was not answered — the directory is
	// not a repository git can read, git could not be run, or the probe was
	// never applicable. The Detail says which, by name.
	RebaseStateUndetermined RebaseState = "undetermined"
)

const (
	// gitRevParseVerb / gitPathFlag spell the ONE question this file asks:
	// `git rev-parse --git-path <name>`. Named here (Rule 1/13) because the
	// evidence this package renders quotes the command it actually ran.
	gitRevParseVerb = "rev-parse"
	gitPathFlag     = "--git-path"
	// rebaseMergeGitPathName / rebaseApplyGitPathName are git's OWN names for
	// the two on-disk markers of an unfinished rebase. They are passed to
	// `--git-path`, never joined onto a directory by hand — that is the defect
	// this file removes.
	rebaseMergeGitPathName = "rebase-merge"
	rebaseApplyGitPathName = "rebase-apply"
)

// RebaseProbe is one measurement: the verdict, the paths git resolved, and the
// sentence a reader can check against the filesystem.
type RebaseProbe struct {
	// State is the verdict (three-valued; see RebaseState).
	State RebaseState
	// MergePath / ApplyPath are what `git rev-parse --git-path` answered. They
	// are recorded even when the answer is "absent": a reader must be able to
	// re-run the same question, and the resolved path is also the proof that the
	// probe was PORTABLE (per-worktree) rather than a `<dir>/.git/…` guess.
	MergePath string
	ApplyPath string
	// Detail is the one-line account of what was asked and what was found.
	Detail string
}

// GroundRebaseProbe answers "is a rebase in progress in dir?" by asking GIT
// where its rebase state lives and then looking there — correct for a CLONE
// ground (`.git` is a DIRECTORY) and for a LINKED worktree (`.git` is a FILE)
// alike, because git resolves the per-worktree directory itself.
//
// It is a PURE READ: two `git rev-parse` calls and two stats. It creates,
// enters and repairs nothing.
func GroundRebaseProbe(dir string) RebaseProbe {
	d := strings.TrimSpace(dir)
	if d == "" {
		return RebaseProbe{State: RebaseStateUndetermined,
			Detail: "UNDETERMINED: no directory was named — nothing was measured"}
	}
	ctx := context.Background()
	mergePath, mergeWhy := groundGitPath(ctx, d, rebaseMergeGitPathName)
	applyPath, applyWhy := groundGitPath(ctx, d, rebaseApplyGitPathName)
	p := RebaseProbe{MergePath: mergePath, ApplyPath: applyPath}

	// ONE undetermined answer for EITHER half: a probe that answered half the
	// question cannot certify "no rebase", because the other marker may be the
	// one that is present. Both are reported separately so the reader sees which
	// half git could not answer.
	var whys []string
	if mergeWhy != "" {
		whys = append(whys, rebaseMergeGitPathName+": "+mergeWhy)
	}
	if applyWhy != "" {
		whys = append(whys, rebaseApplyGitPathName+": "+applyWhy)
	}
	if len(whys) > 0 {
		p.State = RebaseStateUndetermined
		p.Detail = "UNDETERMINED: `git " + gitRevParseVerb + " " + gitPathFlag + "` could not answer in " + d +
			" (" + strings.Join(whys, "; ") + ") — the rebase state is not measured here, so it is claimed as neither " +
			"absent nor present"
		return p
	}

	mergeThere := groundPathExists(mergePath)
	applyThere := groundPathExists(applyPath)
	switch {
	case mergeThere && applyThere:
		p.State = RebaseStateInProgress
		p.Detail = "IN PROGRESS: both " + mergePath + " and " + applyPath + " exist — " +
			"an unfinished rebase is standing in this ground (finish it with `git " + gitRebaseVerb + " --continue`, " +
			"or drop it with `git " + gitRebaseVerb + " " + gitRebaseAbortFlag + "`)"
	case mergeThere:
		p.State = RebaseStateInProgress
		p.Detail = "IN PROGRESS: " + mergePath + " exists — an unfinished rebase is standing in this ground " +
			"(`git " + gitRebaseVerb + " --continue`, or `git " + gitRebaseVerb + " " + gitRebaseAbortFlag + "`)"
	case applyThere:
		p.State = RebaseStateInProgress
		p.Detail = "IN PROGRESS: " + applyPath + " exists — an unfinished patch-backed rebase is standing in this ground " +
			"(`git " + gitRebaseVerb + " --continue`, or `git " + gitRebaseVerb + " " + gitRebaseAbortFlag + "`)"
	default:
		p.State = RebaseStateNone
		// THE EVIDENCE IS THE RESOLVED PATHS, and that is the half the broken
		// sentence got wrong: it named a location it had never asked git about.
		// Naming both resolved paths makes the claim checkable in one command by
		// whoever reads it, on either ground shape.
		p.Detail = "NONE: git resolved the rebase state paths for THIS working tree to " + mergePath + " and " +
			applyPath + ", and neither exists — no rebase is in progress (the paths come from `git " +
			gitRevParseVerb + " " + gitPathFlag + "`, so this answer is the same for a CLONE ground and for a " +
			"LINKED worktree; a hand-joined `.git/rebase-merge` is only correct for the first)"
	}
	return p
}

// groundGitPath asks git where one of its state entries lives in dir, and
// returns ("", "") when git answered. A non-empty second value is the REASON it
// could not — never an empty answer dressed up as an absent directory.
func groundGitPath(ctx context.Context, dir, name string) (string, string) {
	out, err := runCommand(ctx, dir, gitBinary, gitRevParseVerb, gitPathFlag, name)
	if err != nil {
		return "", "`git " + gitRevParseVerb + " " + gitPathFlag + " " + name + "` failed (" + err.Error() +
			", output: " + tail(out) + ")"
	}
	path := strings.TrimSpace(out)
	if path == "" {
		return "", "`git " + gitRevParseVerb + " " + gitPathFlag + " " + name + "` printed no path"
	}
	// git prints this path relative to the directory it ran in; it is used for a
	// stat only, so the caller's own spelling is completed rather than resolved
	// through symlinks (the same reading `git` itself performs).
	if !filepath.IsAbs(path) {
		path = filepath.Join(dir, path)
	}
	return path, ""
}

// groundPathExists reports whether the resolved rebase-state path is there. Any
// failure other than "it is not there" is TREATED AS PRESENT — an unreadable
// answer must not read as "no rebase" (Rule 15: an unmeasured factor is never
// the reassuring verdict).
func groundPathExists(path string) bool {
	if strings.TrimSpace(path) == "" {
		return false
	}
	_, err := os.Stat(path)
	return err == nil
}
