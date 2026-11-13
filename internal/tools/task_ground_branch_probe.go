package tools

// task_ground_branch_probe.go — WHERE a row's branch ref is asked for: by the
// ground's SHAPE, never by the row's recorded repo alone.
//
// THE DEFECT THIS ANSWERS (measured 2026-10-28, live row
// 01a17df2-1d64-7624-b08e-f6ba187631b2). The advisory ground line verified the
// branch with ONE call — taskBranchExists(recRepo, recBranch) — against the
// row's recorded repo, which on a clone-channel run is the MAIN checkout
// (/home/nick/work/gopi). A clone ground holds the run's branch in its OWN ref
// namespace (/home/nick/workspace/<task-id>), so the two answers disagreed on
// the same minute of the same machine:
//
//	$ git -C /home/nick/work/gopi rev-parse --verify --quiet \
//	      refs/heads/task/01a17df21d647624b08ef6ba187631b2 ; echo $?
//	1                                     # the recorded repo has no such ref
//	$ git -C /home/nick/workspace/01a17df2-1d64-7624-b08e-f6ba187631b2 rev-parse \
//	      --verify --quiet refs/heads/task/01a17df21d647624b08ef6ba187631b2
//	3af81a93271e80356c99b95263a125a808a27e67      # the clone ground does
//	$ ls -ld /home/nick/workspace/01a17df2-1d64-7624-b08e-f6ba187631b2/.git
//	drwxrwxr-x 8 nick nick 4096 Oct 28 01:47 …/.git   # a DIRECTORY = clone
//
// and `task show` rendered `branch=task/01a17df2… (NOT FOUND now — a stale
// reading)` for a run that had committed its work. The ledger therefore could
// not tell "the run delivered but the row is stuck" apart from "the run did
// nothing" — the exact reading a dispatcher needs, wrong.
//
// WHY THE SHAPE DECIDES IT (and not a preference): the two ground models have
// DIFFERENT ref stores. A CLONE (`.git` is a DIRECTORY) is its own repository —
// refs created in it exist nowhere else until the delivery loop pushes them. A
// LINKED WORKTREE (`.git` is a FILE naming a gitdir under `<main>/.git/
// worktrees/…`) shares the shared repo's ref store, so asking the worktree and
// asking the repo are the same question. `devworkspace.GroundModelOf` is the
// ONE discriminator for that (Rules 13/14) — the same function the briefing's
// publish clause and the publish guard already ask.
//
// WHAT THIS IS NOT: a writer. It shells out to `git rev-parse --verify` and
// reads the directory's `.git` kind. It never creates, pushes, repairs, or
// re-points anything — the delivery TIMING is not this file's business.
//
// NOT FOUND therefore means what it says: the branch exists in NONE of the
// places this ground can hold it, and the WHY names every place that was asked
// (Rule 15 — a "no" without its evidence is indistinguishable from a reader
// that looked in the wrong tree, which is the defect above).

import (
	"fmt"
	"strings"

	"github.com/nick/gopi/internal/devworkspace"
)

// taskGroundRefSite is ONE directory the row's branch ref can be asked in, with
// the LABEL that says why it is in the list. The label is the ground MODEL for
// the run's own ground and the recorded-repo key for the shared checkout — a
// reader must be able to tell "asked the clone" from "asked the repo" without
// resolving a path themselves.
type taskGroundRefSite struct {
	dir   string
	label string
}

// taskGroundBranchRef is the answer to "where does this row's branch exist
// NOW?", carrying the full list of places asked so a negative answer can name
// its own evidence.
type taskGroundBranchRef struct {
	branch string
	exists bool
	// where is the directory that answered YES — "" when none did.
	where string
	// sites is every directory probed, in the order they were asked.
	sites []taskGroundRefSite
}

// taskGroundBranchRefIn resolves the branch across the ground's shape: the
// run's OWN ground first (a clone's refs are its own; a linked worktree's are
// the shared repo's), then the row's recorded repo. First YES wins and is
// reported; no YES anywhere is the honest NOT FOUND.
//
// A site whose directory does not exist is still LISTED and still ASKED: git
// refuses it, the answer is "no", and the why must be able to say the clone
// ground itself was looked in (a reader who is told "asked nowhere" cannot
// distinguish this from the pre-fix defect).
func taskGroundBranchRefIn(repo, worktree, branch string) taskGroundBranchRef {
	v := taskGroundBranchRef{branch: strings.TrimSpace(branch)}
	if v.branch == "" {
		return v
	}
	v.sites = taskGroundBranchRefSites(repo, worktree)
	for _, s := range v.sites {
		if taskBranchExists(s.dir, v.branch) {
			v.exists, v.where = true, s.dir
			return v
		}
	}
	return v
}

// taskGroundBranchRefSites builds the ordered, deduplicated list of directories
// the row's branch can live in.
//
// ORDER IS THE ANSWER: the run's own ground comes FIRST because that is where a
// clone-channel run's ref actually is, and reporting THAT directory is what
// makes the line readable ("exists — in the clone"), rather than a bare "exists"
// that leaves the operator guessing which of two trees answered.
func taskGroundBranchRefSites(repo, worktree string) []taskGroundRefSite {
	var out []taskGroundRefSite
	add := func(dir, label string) {
		dir = strings.TrimSpace(dir)
		if dir == "" {
			return
		}
		clean := SamePath(dir)
		for _, e := range out {
			if SamePath(e.dir) == clean {
				return // one directory is one question (a trailing-slash or
				// symlinked spelling is the same ref store)
			}
		}
		out = append(out, taskGroundRefSite{dir: dir, label: label})
	}
	wt := strings.TrimSpace(worktree)
	if wt != "" {
		switch model := devworkspace.GroundModelOf(wt); model {
		case devworkspace.GroundModelClone:
			// A clone is its own repository: its refs exist nowhere else.
			add(wt, string(model))
		case devworkspace.GroundModelMissing:
			// No `.git` entry at all: still ask it (the why must be able to say
			// the ground itself was looked in), labelled as what it measured.
			add(wt, string(model))
		default:
			// A linked worktree shares the shared repo's ref store: the repo is
			// the authoritative spelling, and the worktree's own view answers
			// the same refs. Ask the repo first so the reported site is the one
			// a reader can act on, then the worktree (which also covers the case
			// where the recorded repo is not the worktree's host repo).
			add(repo, taskGroundKeyRepo)
			add(wt, string(model))
			// The recorded repo is added below regardless; this arm skips it.
			return out
		}
	}
	add(repo, taskGroundKeyRepo)
	return out
}

// siteText renders the asked places as one clause: `<dir> (<label>), …` —
// the evidence a NOT FOUND answer must carry.
func (v taskGroundBranchRef) siteText() string {
	parts := make([]string, 0, len(v.sites))
	for _, s := range v.sites {
		parts = append(parts, s.dir+" ("+s.label+")")
	}
	return strings.Join(parts, ", ")
}

// elsewhereText names the asked places that did NOT answer — everything except
// v.where. Used by the "found HERE, not THERE" clause.
func (v taskGroundBranchRef) elsewhereText() string {
	parts := make([]string, 0, len(v.sites))
	for _, s := range v.sites {
		if SamePath(s.dir) == SamePath(v.where) {
			continue
		}
		parts = append(parts, s.dir+" ("+s.label+")")
	}
	return strings.Join(parts, ", ")
}

// whereNote is the clause appended to a "verified live" WHY when the branch was
// resolved in the run's OWN CLONE ground rather than in the recorded repo: the
// two are DIFFERENT REPOSITORIES, and only one of them is the delivery point, so
// "exists" without "where" leaves the operator guessing which tree answered.
//
// It fires ONLY for a CLONE answer, and that is the shape's doing, not a
// preference: a clone's refs exist nowhere else (asking two trees really is
// asking two stores), while a linked worktree and its shared repo are ONE store
// — a clause saying "found here, not there" would describe a difference that
// does not exist (measured: the linked-shape arm of the regression test reports
// the same ref in both directories).
func (v taskGroundBranchRef) whereNote() string {
	if !v.exists || strings.TrimSpace(v.where) == "" || len(v.sites) < 2 {
		return ""
	}
	if SamePath(v.where) == SamePath(v.sites[len(v.sites)-1].dir) {
		return ""
	}
	if v.whereLabel() != string(devworkspace.GroundModelClone) {
		return ""
	}
	return fmt.Sprintf(taskGroundWhyBranchWhereFmt, v.branch, v.where, v.elsewhereText())
}

// whereLabel is the label of the site that answered YES ("" when nothing did).
func (v taskGroundBranchRef) whereLabel() string {
	for _, s := range v.sites {
		if SamePath(s.dir) == SamePath(v.where) {
			return s.label
		}
	}
	return ""
}

// The WHY spellings of this file, named once (Rule 1b) so the renderer and the
// regression tests address the same sentence.
const (
	// taskGroundWhyBranchStaleFmt: the branch exists in NO place the ground can
	// hold it. It names every directory asked, in order — the fix for "a no
	// with no evidence looks like a reader that asked the wrong tree".
	taskGroundWhyBranchStaleFmt = "recorded ground — the branch %q exists in NONE of the places this ground can hold it (asked %s) — the reading is stale"
	// taskGroundWhyBranchNowhereFmt: the row records a branch but records no
	// directory to ask in (no repo and no worktree).
	taskGroundWhyBranchNowhereFmt = "recorded ground — the branch %q could not be verified: the row records no repo and no worktree to ask in"
	// taskGroundWhyBranchWhereFmt: found in the run's own ground, absent from
	// the place(s) also asked. `%s`×3 = branch, where, elsewhere.
	taskGroundWhyBranchWhereFmt = "; branch %q exists in %s — NOT in %s"
)
