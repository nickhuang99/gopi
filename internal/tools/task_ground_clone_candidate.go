package tools

// task_ground_clone_candidate.go — the CANDIDATE ground of a CLONE-channel run,
// asked when the session reading names no branch.
//
// THE DEFECT THIS CLOSES (measured 2026-10-28, 8 rows). `task show`'s advisory
// line rendered `branch=(none)` for rows whose clone ground was standing on
// disk holding a live `refs/heads/task/<id>`:
//
//	$ gopi task show 01a17df2-1d64-7624-b08e-f6ba187631b2 | grep '^ground (resolved now)'
//	ground (resolved now): repo=/home/user/work/gopi worktree=(none) branch=(none) — the row's session "01a17df2-…" names no worktree-add and no in-place branch creation
//
// WHY THE SESSION CANNOT ANSWER IT: the SESSION arm of the reading
// (taskGroundSessionReadingFor) reconstructs the branch from the run's own
// EXECUTED commands — `git worktree add -b` / `git checkout -b`. A clone-channel
// run issues NEITHER: its clone arrives already checked out on the row's
// branch, so there is no command to parse and the reconstruction yields "".
// The reading then returns with the absence marker and never reaches the
// branch-axis probe (task_ground_branch_probe.go).
//
// WHAT ANSWERS IT INSTEAD: the channel names its ground deterministically —
// `devworkspace.Dir(root, taskID)` is `root/<full-task-id>`, the root read from
// the ONE reader (config.WorkspaceRootConfigured), and the branch is the row's
// own name from the ONE spelling (TaskGroundBranchName). So when the session
// names no branch, that directory is asked as a CANDIDATE: if its store holds
// `refs/heads/task/<id-no-dashes>` the branch axis is answered from it, and a
// CLONE-model directory also answers the worktree axis (it IS a directory of its
// own — asking the linker would be a different question).
//
// WHAT THIS IS NOT:
//   - NOT a writer. It states a directory and runs `git rev-parse --verify`;
//     `task show` never changes the deliverable it shows.
//   - NOT a second spelling of anything: the root comes from
//     config.WorkspaceRootConfigured, the directory from devworkspace.Dir, the
//     shape from devworkspace.GroundModelOf, the branch from
//     TaskGroundBranchName, and existence from the EXISTING taskBranchExists.
//   - NOT a guess: a candidate that does not pan out (no branch, no directory,
//     an unreadable root) says so in ONE clause and leaves the caller's existing
//     why standing (Rule 15 — the evidence is stated, and its absence is stated
//     as the absence it is).

import (
	"context"
	"fmt"
	"os/exec"
	"strings"

	"github.com/nick/gopi/internal/config"
	"github.com/nick/gopi/internal/devworkspace"
)

// The candidate arm's vocabulary (Rule 1b): the WHY a hit states, the reason an
// unreadable workspace root produces no candidate, and the reason a directory of
// the right name holds no row ref. Each names the directory it read, because a
// reading whose subject is a path must print that path.
const (
	// taskGroundWhyCloneCandidateFmt: the branch was found in the candidate
	// ground. The directory, its measured shape and the fact that the session
	// named nothing all ride into the sentence — the same "say where you read"
	// discipline the branch probe's found-elsewhere clause follows.
	taskGroundWhyCloneCandidateFmt = "resolved from the row's OWN ground directory %s (model %s) — the candidate the row's session named no branch for — the ref is live there"
	// taskGroundWhyCloneCandidateStaleFmt: the candidate directory exists and is
	// a ground, but its store holds no ref for this row — the honest negative,
	// with the directory named so a reader can go and look.
	taskGroundWhyCloneCandidateStaleFmt = "the row's clone ground %s (model %s) holds no %q ref"
	// taskGroundWhyCloneCandidateRootFmt: the workspace root could not be read,
	// so no candidate could be named. Folded into the existing why rather than
	// replacing it (the session reading's own words stay).
	taskGroundWhyCloneCandidateRootFmt = "no clone-ground candidate was asked: the workspace root could not be read: %v"
	// taskGroundWhyCloneCandidateAbsentFmt: the row's own ground directory is
	// simply not there.
	taskGroundWhyCloneCandidateAbsentFmt = "the row's clone ground %s is not there now"
	// taskGroundWhyCloneCandidateUndeliveredFmt: the ref is live in the ground,
	// but the COMMIT it points at is in no other tree — the measured 2-of-8
	// shape (a clone ground holding work nothing has delivered yet). It rides
	// BEHIND the hit's own sentence, never instead of it: "the branch exists"
	// and "its commit has not been delivered" are both true and a reader needs
	// both (Rule 15).
	taskGroundWhyCloneCandidateUndeliveredFmt = "; its tip %s is NOT in the recorded repo %s — the commit exists only in this ground and nothing has delivered it yet"
)

// taskGroundCloneCandidate is the answer of ONE candidate query: the ground
// directory, the shape measured there, the row's branch name, and whether that
// directory's store holds it, plus the TIP and whether the recorded repo knows
// that commit. ok=false means the arm had nothing to say and the
// caller's why must be left intact; why then carries the reason.
type taskGroundCloneCandidate struct {
	dir      string
	model    devworkspace.GroundModel
	branch   string
	exists   bool
	tip      string
	ok       bool
	worktree string
	why      string
}

// taskGroundCloneCandidateIn asks the row's own clone ground whether it holds
// the row's branch ref.
//
// Read-only by construction: one config read, one `os.Stat`-class shape
// measurement (devworkspace.GroundModelOf), one `git rev-parse --verify`.
func taskGroundCloneCandidateIn(taskID, repoRow string) taskGroundCloneCandidate {
	branch := TaskGroundBranchName(taskID)
	if strings.TrimSpace(branch) == "" {
		// No row name, no ref to look for: the arm has nothing to say, and the
		// caller's why stands.
		return taskGroundCloneCandidate{}
	}
	root, _, rerr := config.WorkspaceRootConfigured()
	if rerr != nil || strings.TrimSpace(root) == "" {
		return taskGroundCloneCandidate{branch: branch, why: fmt.Sprintf(taskGroundWhyCloneCandidateRootFmt, rerr)}
	}
	dir := devworkspace.Dir(root, taskID)
	// THE SHAPE IS THE ONE DISCRIMINATOR (Rules 13/14): a directory that is not
	// a ground at all is no candidate, and it must not be reported as one.
	model := devworkspace.GroundModelOf(dir)
	if model == devworkspace.GroundModelMissing {
		return taskGroundCloneCandidate{dir: dir, model: model, branch: branch,
			why: fmt.Sprintf(taskGroundWhyCloneCandidateAbsentFmt, dir)}
	}
	c := taskGroundCloneCandidate{dir: dir, model: model, branch: branch, ok: true}
	if !taskBranchExists(dir, branch) {
		c.why = fmt.Sprintf(taskGroundWhyCloneCandidateStaleFmt, dir, model, branch)
		return c
	}
	c.exists = true
	// The WORKTREE axis follows the SHAPE: a CLONE is a directory of its own, so
	// the candidate directory IS the run's ground. A linked worktree shares the
	// main repo's checkout semantics (the caller's injected resolver answers
	// that question, and this arm must not answer it a second way).
	if model == devworkspace.GroundModelClone {
		c.worktree = dir
	}
	c.why = fmt.Sprintf(taskGroundWhyCloneCandidateFmt, dir, model)
	c.tip = taskGroundRefTip(dir, branch)
	// WHERE THE COMMIT IS is a second, independent fact from where the REF is:
	// a clone ground's refs exist nowhere else until the delivery loop pushes
	// them, so a live ref can still point at a commit no other tree holds. The
	// reading says both (measured: 2 of the 8 rows).
	if c.tip != "" && strings.TrimSpace(repoRow) != "" && !taskGroundCommitInRepo(repoRow, c.tip) {
		c.why += fmt.Sprintf(taskGroundWhyCloneCandidateUndeliveredFmt, c.tip, repoRow)
	}
	return c
}

// taskGroundSessionReadingForShow is the ADVISORY face's reading: the shared
// session reading, with the clone-ground CANDIDATE asked when the session named
// no branch.
//
// WHY THE FALLBACK IS NOT INSIDE taskGroundSessionReadingFor ITSELF (measured
// reason, not taste): the SAME reading feeds the claim-time candidate seam
// (taskClaimGroundCandidate), whose contract is "the run's own EXECUTED
// worktree-add identifies its ground, and nothing else does". A candidate that
// named a clone ground there would narrow that seam's registered-worktree set to
// zero and produce a refusal sentence describing a directory that seam never
// asks about — a second, wrong opinion rather than evidence. The advisory face
// asks a DIFFERENT question ("what ground does this row stand on now?"), and a
// clone ground is a complete answer to it. So the fallback is added at the face
// that wants it, and the claim seam's reading is left exactly as it was.
func taskGroundSessionReadingForShow(taskID, sessionID, repoRow string,
	resolve func(repo, branch string) (worktree string, ok bool)) taskGroundSessionReading {
	r := taskGroundSessionReadingFor(taskID, sessionID, repoRow, resolve)
	if strings.TrimSpace(r.branch) != "" {
		// The session DID name a branch: that is evidence of what the run did,
		// and the candidate exists only for the question the session left open.
		return r
	}
	return taskGroundCloneCandidateApply(r, taskGroundCloneCandidateIn(taskID, repoRow))
}

// taskGroundCloneCandidateApply folds a candidate answer into a reading whose
// session-derived branch was empty. It is the ONLY place the fallback order is
// decided: a session-named branch always wins (it is evidence of what the run
// DID); the candidate answers only the question the session left open.
//
// The existing why is kept and EXTENDED, never replaced: the reason arises from
// the candidate arm needs a reader to be able to see both "the session named
// nothing" and "the candidate was / was not asked" (Rule 15).
func taskGroundCloneCandidateApply(r taskGroundSessionReading, c taskGroundCloneCandidate) taskGroundSessionReading {
	switch {
	case c.exists:
		r.branch, r.branchOK = c.branch, true
		if c.worktree != "" {
			r.worktree, r.worktreeOK = c.worktree, true
		}
		r.why = c.why
	case strings.TrimSpace(c.why) != "":
		// NOT a hit: the candidate is named in the why, and the session reading's
		// own words stay in front of it — a negative must be readable as "asked
		// here, not found" rather than as "nothing was asked".
		r.why = joinGroundWhy(r.why, c.why)
	}
	return r
}

// joinGroundWhy appends one why clause to another, keeping the sentence
// readable when the leading half is empty ("" + clause is the clause alone).
func joinGroundWhy(base, clause string) string {
	base, clause = strings.TrimSpace(base), strings.TrimSpace(clause)
	switch {
	case base == "":
		return clause
	case clause == "":
		return base
	}
	return base + "; " + clause
}

// taskGroundRefTip resolves a branch's commit in dir ("" when it does not
// resolve) — read-only, one `git rev-parse`.
func taskGroundRefTip(dir, branch string) string {
	dir, branch = strings.TrimSpace(dir), strings.TrimSpace(branch)
	if dir == "" || branch == "" {
		return ""
	}
	ctx, cancel := context.WithTimeout(context.Background(), taskBranchBackfillTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, "git", "-C", dir, "rev-parse", "--verify", "--quiet", "refs/heads/"+branch)
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

// taskGroundCommitInRepo reports whether repo knows the commit sha — the
// DELIVERY half of the reading. `git cat-file -e` asks about the object store,
// not about any ref: an object can be reachable from no branch and still be
// present, and a commit can be absent from one repository while its ref is live
// in another (that is exactly the shape a clone ground produces before the
// channel pushes).
func taskGroundCommitInRepo(repo, sha string) bool {
	repo, sha = strings.TrimSpace(repo), strings.TrimSpace(sha)
	if repo == "" || sha == "" {
		return false
	}
	ctx, cancel := context.WithTimeout(context.Background(), taskBranchBackfillTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, "git", "-C", repo, "cat-file", "-e", sha)
	return cmd.Run() == nil
}
