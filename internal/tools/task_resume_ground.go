package tools

// task_resume_ground.go — the RESOLVE-ONLY ground resolver for
// `gopi worker --resume`.
//
// WHY IT EXISTS (the measured breach, devdoc/worker-resume-ground.md): the
// close-out chain removes a finished row's worktree, and the ONE arm that may
// not is `guestRowOwnCheckoutRefusal` — a process standing in the directory it
// is about to remove pins the inode (the 2026-09-21 breach on row 01a0c574).
// `gopi session resume <task-id>` chdirs INTO the row's worktree by design, so the
// resumed session's own chain correctly refuses and every successful resume
// leaves its worktree behind. The fix is not a weaker arm — it is an actor that
// is NOT standing there. That actor is the worker, and this file is the half
// of it that answers "which directory, which branch, which row?" WITHOUT
// creating or entering anything.
//
// It is resolve-ONLY by contract:
//
//   - It never creates a worktree. A `--resume` that names ground that does not
//     exist is a REFUSAL with a named reason (Rule 15b) — resuming existing
//     work and minting an empty checkout are different acts, and silently doing
//     the second when the operator asked for the first is the degradation the
//     refusal exists for.
//   - It never chdir's. The directory is returned as a fact for the child
//     (`gopi session resume` chdir's into it itself, exactly as it does today) while
//     the CALLER's cwd stays where it was — which is the whole point, and the
//     reason this resolver is a pure read.
//
// THREE SELECTORS, ONE ADDRESS. The mode's two FLAG spellings name a GROUND and
// have the row derived from it (`--branch`, `--worktree` — the direction the
// operator who holds a branch name or a `pwd` needs). The DEFAULT spelling is
// TaskResumeGroundForTask: the ROW is the input and the ground is derived from
// the row's own deliverable — the direction every other face of this system
// already speaks, since the run that worked in a directory is what stamped it
// onto the row. All three end in the same TaskResumeGround, so the caller has
// one thing to run and the guards have one thing to protect.
//
// Every reading is an EXISTING reader (Rules 13/14 — no second parser):
// taskRepoLinkedWorktrees for the registrations, worktreeSweepMainCheckoutOf
// for the shared checkout, taskGroundResolveList for the branch→directory step,
// and TaskForBranch / TaskForWorktree for the ledger's answer to "who names
// this?".
import (
	"context"
	"fmt"
	"strings"

	"github.com/nickhuang99/gopi/internal/config"
	"github.com/nickhuang99/gopi/internal/devworkspace"
)

// The NO-ROW verdicts of the `--branch` spelling, one sentence per measured
// state of the GROUND (Rule 1b: the refusals and the tests agree by sharing
// these). They exist because "no ledger row names the branch" and "there is no
// ground to resume" are DIFFERENT facts, and the pre-fix code rendered the
// first as the second (measured 2026-09-27: branch fix/pushback-rule31-evidence
// was live in /home/user/workspace/gopi-evi-pushback, a registered linked
// worktree of /home/user/work/GoPi, and the verb said "no ground to resume").
//
// A row-less ground is reported as EXISTS-but-unresumable, never as absent: the
// resume checkpoint lives in the row's own run directory, so a ground the
// ledger does not name has nothing to continue from — an operator fact that is
// the LEDGER's to repair, and the sentence says so instead of sending them
// looking for a directory that is right there.
const (
	taskResumeGroundNoRowGroundFmt = "resume: no ledger row names --%s %s — the ground EXISTS and no row records it: " +
		"repo %s has it checked out in the linked worktree %s (tip %s). A row-less ground cannot be resumed — " +
		"the checkpoint a resume continues from lives in the row's own run directory — and the row is the ledger's to hold: " +
		"`gopi task show --%s %s` reads what the ledger does record"
	taskResumeGroundNoRowMainCheckoutFmt = "resume: no ledger row names --%s %s — and the branch is checked out in the MAIN checkout %s, " +
		"which is not a deliverable ground; a resume needs a LINKED worktree of %s registered on this branch"
	taskResumeGroundNoRowNoCheckoutFmt = "resume: no ledger row names --%s %s, and no worktree holds it: %s (tip %s) has no live registration on it — " +
		"there is no directory to resume in, and this mode never creates one"
	taskResumeGroundNoRowStaleFmt = "resume: no ledger row names --%s %s; %s still registers the worktree %s on it, but that directory is gone — " +
		"the ground was swept or removed, and this mode never creates one"
	taskResumeGroundNoRowRefGoneFmt = "resume: no ground to resume — no ledger row names --%s %s, and %s has no such branch " +
		"(git has no such ref)"
	// taskResumeGroundRepoInferredFmt qualifies a repo axis the LEDGER named
	// rather than the operator. `--branch` takes no repo input, so the
	// repositories searched for the branch come from the rows that carry it —
	// and a refusal reading "… and /path has no such branch (git has no such
	// ref)" reads as a verdict on the BRANCH. Measured 2026-10-07 (row
	// 01a112e3, cleaning two residue branches): the branch was present in the
	// repo the operator could see and absent only from the inferred candidate,
	// so the unqualified sentence sent the reader after the wrong object.
	// Naming the derivation is the repair: the operator keeps the branch in
	// hand and learns which repository the answer was taken from.
	taskResumeGroundRepoInferredFmt = "%s (inferred — no --repo was given, so this is the repo the ledger named)"
	taskResumeGroundNoRowUnknownFmt = "resume: no ground to resume — no ledger row names --%s %s, and no repository could be read to say whether " +
		"the branch or a worktree exists (%s)"
)

// taskResumeGroundKinds are the three selectors' names as they appear in every
// operator-facing sentence this file renders. Named (Rule 1b) because the
// refusal text, the detail line and the tests all have to agree about which
// selector was asked — a sentence that mislabels its own question is worse than
// no sentence (the same discipline TaskForBranchLookup.Worktree follows).
//
// TaskResumeGroundTaskKind is the DEFAULT spelling's name (`--resume <task-id>`):
// it is listed first in every message because it is the one an operator who
// knows the row needs, and the two flag spellings are its alternatives.
const (
	TaskResumeGroundTaskKind     = "task"
	TaskResumeGroundBranchKind   = "branch"
	TaskResumeGroundWorktreeKind = "worktree"
)

// taskResumeGroundMainCheckoutRefusalFmt is the refusal for `--worktree` naming
// a repository's MAIN checkout. It is its own sentence because it is a
// different defect from "no such directory": the path is real, it is a git
// worktree, and it is precisely the shared checkout a repo-aware run must never
// be handed (the main checkout is not a deliverable ground — it is where the
// operator stands, and removing it is the disaster the chain's own gates exist
// to prevent).
const taskResumeGroundMainCheckoutRefusalFmt = "no ground to resume: --%s %s is the MAIN checkout of %s — " +
	"a repo-aware row works in a LINKED worktree, and the main checkout is not a deliverable ground"

// taskResumeGroundBranchFlag / taskResumeGroundWorktreeFlag are the two
// selectors' spellings as the operator types them. They live HERE, beside the
// resolver that reads them, rather than in the command package: `gopi worker
// --resume` is not their only conceivable caller (a dispatcher building the
// command, a test asserting the grammar), and Rule 14 puts a value shared across
// packages in ONE exported home. The command package references these; it never
// re-spells them.
const (
	TaskResumeGroundBranchFlag   = "--branch"
	TaskResumeGroundWorktreeFlag = "--worktree"
)

// TaskResumeGround is the answer of one `--resume` resolution: the EXISTING
// ground, the branch it is checked out on, the repo that registered it, and the
// ledger row derived from it. Every field is READ; nothing here was created or
// modified by the resolution.
type TaskResumeGround struct {
	// Dir is the linked worktree the resume will run in — the directory the
	// child chdir's into. It EXISTS (the resolver refuses otherwise), and the
	// resolver did not enter it.
	Dir string
	// Branch is the branch the ground is checked out on (the row's branch).
	Branch string
	// Repo is the repository the ground is a LINKED worktree of — the anchor
	// every close-out reading (porcelain, worktree list) is taken from.
	Repo string
	// TaskID is the ledger row derived from the selector: the branch/worktree
	// is the input and the row is the answer (the dispatch path's direction
	// reversed). Never empty on success — a ground with no row is a refusal.
	TaskID string
	// Selector records WHICH spelling was asked (`branch`/`worktree`), so a
	// caller's report can name its own question rather than guessing from the
	// fields (both are non-empty on success, so the fields cannot tell).
	Selector string
	// Detail is the one-line account of HOW the ground and the row were
	// resolved — the evidence a later reader needs to re-derive the answer.
	Detail string
}

// TaskResumeGroundFor resolves a `gopi worker --resume` selector to the
// EXISTING ground it names plus the ledger row derived from it.
//
// Exactly one of branch/worktree must be set; either both or neither is a usage
// error, refused by name (the same shape ResolveTaskIDByDeliverable takes for
// the sibling selectors, because it is the same question asked by the same kind
// of caller).
//
// The two spellings resolve along DIFFERENT paths and that is deliberate:
//
//	--worktree P  the operator is standing somewhere and pasted `pwd`: P is
//	              verified to BE a registered linked worktree (the "is this a
//	              ground at all?" question), and the repo is the one git answers
//	              for at P. The row comes from the ledger's worktree lookup.
//	--branch B    the operator has the branch name (`git branch --show-current`)
//	              and no path: the REPO comes from the ledger rows that name B,
//	              and the DIRECTORY is that repo's registration of B. A branch
//	              the ledger knows but no repo has checked out is a refusal.
//
// Both end in the same place — one directory, one branch, one row — which is
// what makes them one address rather than two filters.
func TaskResumeGroundFor(branch, worktree string) (TaskResumeGround, error) {
	var out TaskResumeGround
	branch = strings.TrimSpace(branch)
	worktree = strings.TrimSpace(worktree)
	switch {
	case branch == "" && worktree == "":
		return out, fmt.Errorf("resume: a selector is required — pass the TASK ID as the argument (the "+
			"default spelling: the row's recorded worktree and branch are the ground), or name the ground "+
			"directly with --%s <name> / --%s <path>",
			TaskResumeGroundBranchKind, TaskResumeGroundWorktreeKind)
	case branch != "" && worktree != "":
		return out, fmt.Errorf("resume: --%s and --%s name ONE ground two ways, not a filter pair — pass one",
			TaskResumeGroundBranchKind, TaskResumeGroundWorktreeKind)
	}
	if worktree != "" {
		return taskResumeGroundFromWorktree(worktree)
	}
	return taskResumeGroundFromBranch(branch)
}

// TaskResumeGroundForOwnDir resolves the row that names a directory THIS
// PROCESS is standing in, WITHOUT asking the process where it is.
//
// WHY IT TAKES THE DIRECTORY RATHER THAN READING THE CWD (the measured defect,
// 2026-10-15): the caller of this question is a close-out that has ALREADY
// re-parented its own CWD to the filesystem root, because it must not hold a
// directory its next step is allowed to remove
// (internal/worktreerelease.ProcessRootPath). A caller that then asked
// TaskResumeGroundFor("", pwd) handed "/" to the resolver as "the directory I
// occupy" — and the resolver did exactly what it was told: `/` is a directory,
// it is not a work tree, and git said so. The result was a FALSE report printed
// on EVERY successful run:
//
//	gopi worker --resume: close-out for /: resume: --worktree / is not a work
//	tree: git rev-parse --show-toplevel in /: exit status 128
//
// twice per run. The defect is not in the resolver — "/" IS not a work tree —
// it is in the QUESTION: "which row does my current directory belong to" has no
// answer once the process has released the directory. The directory is a fact
// the caller HELD, so it passes it in; the reading itself never touches the
// process's own CWD.
//
// TWO READINGS, ONE ORDER, and the order is the point. The answer has two
// independent sources that can disagree, and the disagreement is a real,
// harmless shape rather than an error:
//
//	the LEDGER     TaskForWorktree — "which row names this directory?" — and
//	               its answer survives the directory being un-registered.
//	the GROUND     repoRootOf + the repo's `git worktree list` — "what does git
//	               say this directory is, and which repo registered it?" —
//	               which is what bounds a foreign checkout and a non-worktree.
//
// The GROUND is read FIRST, because a directory that is not a work tree at all
// must be reported as exactly that: answering "no ledger row names it" would be
// a statement about the ledger rendering a fact about the FILESYSTEM (the same
// misattribution taskResumeGroundNoRowRefusal was repaired for). And when the
// ground IS a registered linked worktree and the ledger names its row, that row
// is reported WITH the registration's branch and repo — the two facts a bare
// row id leaves the operator to look up.
//
// A directory git does not know is NOT an error here. The caller is a close-out
// reporting on its own working directory, which may legitimately be the main
// checkout, a foreign checkout, or a directory whose registration was already
// released. Each of those is a fact about the directory, and this function
// returns it as one (Rule 15: never a silent skip, never a bare failure).
func TaskResumeGroundForOwnDir(dir string) (TaskResumeGround, error) {
	var out TaskResumeGround
	path := strings.TrimSpace(dir)
	if path == "" {
		// The caller could not name the directory it stood in. Nothing can be
		// read about a directory nobody named, and guessing (the main checkout?
		// the profile root?) would report on a directory this face never
		// occupied.
		return out, fmt.Errorf("resume: no directory to report on — the caller could not name the " +
			"directory this face was standing in")
	}
	out.Dir = path
	// The GROUND half: the directory's own top level, then — only when git
	// answers — whether a repository registers it as a LINKED worktree (the
	// main checkout is a work tree git answers for but registers no linked entry
	// for, so it is reported as the repository's own checkout rather than as
	// ground nobody owns).
	if root, err := repoRootOf(path); err == nil {
		out.Repo = root
		if common, cerr := worktreeSweepCommonGitDir(root); cerr == nil {
			// main == "" is an unrecognisable layout (a bare/mirror common dir,
			// a `.git` file whose parent is not a checkout). No registration can
			// be read from it, and reporting the repository root as the repo is
			// the honest half-answer rather than a guess at a main checkout.
			if main := worktreeSweepMainCheckoutOf(common); main != "" {
				if linked, lerr := taskRepoLinkedWorktrees(main); lerr == nil {
					if entry, ok := taskResumeGroundLinkedEntry(linked, root); ok {
						out.Dir = entry.path
						out.Branch = entry.branch
						out.Repo = main
					}
				}
			}
		}
	}
	// The LEDGER half: the row that names the directory. The lookup is the SAME
	// reader `--worktree` uses (TaskForWorktree, Rules 13/14), so this report and
	// a resume by path cannot disagree about who owns a directory.
	lookup, lerr := TaskForWorktree(TaskForWorktreeQuery{Worktree: out.Dir}, "")
	if lerr != nil {
		return out, fmt.Errorf("resume: ledger lookup for the directory %s: %w", out.Dir, lerr)
	}
	if !lookup.Readable {
		// An unreadable ledger is NOT "no row names this directory" — the two
		// must never render the same way (Rule 15).
		return out, fmt.Errorf("resume: cannot read the ledger to find the row that names the directory %s", out.Dir)
	}
	if owner, ok := TaskForBranchResolvedRow(lookup); ok {
		out.TaskID = owner.TaskID
		if out.Branch == "" {
			out.Branch = owner.Branch
		}
		if out.Repo == "" {
			out.Repo = owner.Repo
		}
		out.Detail = fmt.Sprintf("resolved by %s: %d row(s) name the directory, chose %s (%s, %s)",
			TaskResumeGroundWorktreeKind, len(lookup.Owners), owner.TaskID,
			taskResumeGroundOwnerKind(owner), owner.Status)
		return out, nil
	}
	// No row names it. That is an ANSWER, not a failure — the report says which
	// of the two possible readings produced it, so a reader can tell "no row was
	// ever recorded here" from "the row's ground was un-registered".
	if out.Repo == "" {
		out.Detail = "no ledger row names this directory, and git does not answer for it as a work tree"
		return out, nil
	}
	out.Detail = fmt.Sprintf("no ledger row names this directory; git reads it as a work tree of %s (%d row(s) carry a branch)",
		out.Repo, lookup.RowsWithBranch)
	return out, nil
}

// TaskResumeGroundForTask resolves the DEFAULT spelling of the mode: the ROW is
// named and the ground is derived FROM it.
//
// It is the mirror of the two flag spellings, and the mirror is the point: the
// run that worked in a directory stamped it onto the row's deliverable, so the
// row is the ONE place both facts are already recorded — asking an operator who
// has the task id to re-derive the branch and the path from git is asking them
// to repeat work the ledger has done.
//
// The refusals are distinct because their repairs differ:
//
//	no such row             the id is wrong, or the ledger is somewhere else
//	the row names no ground nothing ever recorded a deliverable ground on it
//	the ground is gone      the row records a directory that is no longer a
//	                        LIVE registered worktree (merged and swept, removed
//	                        by hand, or never registered)
//
// Whatever the reason, NOTHING is created and nothing is entered: the ledger
// read is a read and the git read is git's own `worktree list` (Rule 15b — the
// mode never mints ground, whichever spelling asked).
func TaskResumeGroundForTask(taskID string) (TaskResumeGround, error) {
	var out TaskResumeGround
	taskID = taskIDNormalize(strings.TrimSpace(taskID))
	if taskID == "" {
		return out, fmt.Errorf("resume: a task id is required — the default spelling names the ROW, "+
			"or name the ground instead with --%s <name> / --%s <path>",
			TaskResumeGroundBranchKind, TaskResumeGroundWorktreeKind)
	}
	// The row must EXIST before any of its facts mean anything: a row's absent
	// deliverable and a row that is not there must never render the same way
	// (Rule 15), and TaskStatusOf is the ledger's own existence verdict (it
	// errors for no row, rather than answering "").
	status, err := TaskStatusOf(taskID)
	if err != nil {
		return out, fmt.Errorf("resume: no ledger row to resume: %w", err)
	}
	worktree, branch := TaskDeliverableWorktreeFor(taskID)
	repo := TaskDeliverableRepoFor(taskID)
	if worktree == "" && branch == "" {
		// The refusal is TYPED (TaskResumeGroundRefusal) so a caller rendering
		// "recorded path / model / detail / repair" does not parse a sentence,
		// and so "the row records no ground" can never be rendered as "the
		// ground is gone" — the two have different repairs (Rule 15).
		return out, TaskResumeGroundNoGroundRefusal(taskID, branch, repo)
	}
	// The repository, when the blob names one — it is the anchor the REGISTRATION
	// is read from (the same reading the two flag spellings use, so a directory
	// no repo registers is refused the same way whichever spelling asked).
	main := ""
	if repo != "" {
		main = worktreeSweepMainCheckoutOf(mustCommonGitDir(repo))
	}
	dir, recordedBranch := "", ""
	if worktree != "" {
		dir, recordedBranch = taskResumeGroundRegisteredDir(worktree, main)
		if dir == "" {
			// THE ONE GROUND JUDGE (measured 2026-10-19, row 01a150b3): the
			// registration-only reading above knows the LINKED-WORKTREE model
			// and reported a CLONE channel workspace ("its .git is a DIRECTORY,
			// registered nowhere") as "the ground is gone" — while the ledger's
			// advisory line called the same directory verified live. Which model
			// the directory is decides the question, and the decision belongs to
			// devworkspace.ResolveGroundState — the SAME function the advisory
			// line renders. Two readers, one judge: they cannot disagree again.
			state := devworkspace.ResolveGroundState(worktree)
			if !state.Alive {
				return out, TaskResumeGroundGoneRefusal(taskID, worktree, branch, repo,
					string(state.Model), state.Detail)
			}
			dir = state.Dir
			recordedBranch = state.Branch
		}
	}
	if dir == "" {
		// Branch axis: the same reading the `--branch` spelling takes, minus the
		// ledger walk (the row is already known), so the two spellings resolve
		// through one function and cannot disagree about where a branch lives.
		var ok bool
		dir, ok = taskGroundResolveList(repo, branch)
		if !ok {
			return out, fmt.Errorf("resume: row %s records branch %s, but no repository registers a LIVE "+
				"worktree on it (repo %s) — the ground is gone; this mode never creates one",
				taskID, branch, taskResumeGroundRepoText(repo))
		}
		if main != "" && SameDir(main, dir) {
			return out, fmt.Errorf(taskResumeGroundMainCheckoutRefusalFmt,
				TaskResumeGroundBranchKind, branch, repo)
		}
	}
	if branch == "" {
		// The row recorded a PATH and no branch (an older blob, or a row stamped
		// by a reader that only had the worktree): the branch is the registration's
		// own, which is the same answer `--worktree` derives.
		branch = recordedBranch
	}
	return TaskResumeGround{
		Dir: dir, Branch: branch, Repo: repo, TaskID: taskID,
		Selector: TaskResumeGroundTaskKind,
		Detail: fmt.Sprintf("resolved by %s: row %s (status %s) records worktree %s branch %s in %s — "+
			"the registration is live, so the resume runs in that directory",
			TaskResumeGroundTaskKind, taskID, status,
			taskResumeGroundAxisText(worktree), taskResumeGroundAxisText(branch),
			taskResumeGroundRepoText(repo)),
	}, nil
}

// taskResumeGroundRegisteredDir answers "which directory does this row's
// recorded worktree resolve to, if git still registers it?".
//
// It returns the REGISTRATION's own path (taskResumeGroundLinkedEntry matches by
// directory identity, so a `pwd` and a recorded path that spell one directory
// two ways still match) and, beside it, the branch that registration is checked
// out on — the second fact a row with a worktree and no branch needs.
//
// repoAxis is the row's repo when it names one. When it does not, the path is
// asked about directly (repoRootOf) rather than refused: a deliverable blob
// written before the repo was recorded must not make an otherwise resumable row
// unresumable, and the directory git answers for at the path is the same ground.
func taskResumeGroundRegisteredDir(worktree, mainCheckout string) (dir, branch string) {
	if mainCheckout == "" {
		root, err := repoRootOf(worktree)
		if err != nil {
			return "", ""
		}
		return root, ""
	}
	linked, err := taskRepoLinkedWorktrees(mainCheckout)
	if err != nil {
		return "", ""
	}
	entry, ok := taskResumeGroundLinkedEntry(linked, worktree)
	if !ok {
		return "", ""
	}
	return entry.path, entry.branch
}

// taskResumeGroundRepoText renders the repo axis of this file's refusals: the
// recorded repo, or the explicit absence marker — an unreachable path is a fact
// about the row, and a blank would read as "we did not look" (Rule 15).
func taskResumeGroundRepoText(repo string) string {
	return taskResumeGroundAxisText(repo)
}

// taskResumeGroundAxisText is the ONE renderer of a possibly-absent fact in this
// file's operator sentences. It reuses the package's own absence marker
// (taskGroundKeyAbsent) rather than minting a second one, so a reader who greps
// for it finds every place this mode says "we have nothing here".
func taskResumeGroundAxisText(v string) string {
	if strings.TrimSpace(v) == "" {
		return taskGroundKeyAbsent
	}
	return v
}

// taskResumeGroundFromWorktree resolves the `--worktree P` spelling: P must be a
// LIVE ground of the shape it MEASURES itself to be, and the ledger must hold a
// row naming it.
//
// THE SHAPE IS MEASURED FIRST, and that is a repair rather than tidiness
// (measured 2026-10-27, this epic's own ground). The two shapes answer a
// DIFFERENT question about "the main checkout":
//
//	LINKED worktree  `.git` is a FILE → git's common dir is the MAIN checkout's
//	                 `.git`, so "is P that checkout?" is a real question.
//	CLONE            `.git` is a DIRECTORY → the clone IS its own repository, so
//	                 `worktreeSweepMainCheckoutOf(P/.git)` answers P itself and
//	                 `SameDir(main, dir)` is trivially TRUE.
//
// Before the repair the LINKED body below was the only body, so EVERY clone
// ground was refused as "the MAIN checkout of itself" — while the SAME directory
// resolved fine through the default (task-id) spelling:
//
//	$ gopi worker --resume --worktree /home/user/workspace/01a17aba-…   → refused, exit 1
//	$ gopi worker --resume 01a17aba-…                                  → resolved (model clone)
//
// The two refusals of the LINKED path are distinct on purpose. "P is not there /
// not a directory" is a shape the operator can fix by looking (a stale path, a
// typo, a recycled directory). "P is the main checkout" is a different mistake
// with a different repair: the path is real and git IS there, and what is wrong
// is that P is not a deliverable ground at all — so the sentence names the main
// checkout rather than reporting a missing worktree, which would send the
// operator looking for a directory that is present.
func taskResumeGroundFromWorktree(worktree string) (TaskResumeGround, error) {
	var out TaskResumeGround
	// The worktree's OWN top level, which is where the resume will run — read
	// with the path-identity helper the rest of this package uses (realpath), so
	// the answer is the directory git knows, not a spelling of it.
	dir, err := repoRootOf(worktree)
	if err != nil {
		// git cannot answer for the path: it does not exist, or it is not
		// inside a work tree. Both are "there is no ground here", and git's own
		// words are the evidence (Rule 15: never a bare "not found").
		return out, fmt.Errorf("resume: --%s %s is not a work tree: %w",
			TaskResumeGroundWorktreeKind, worktree, err)
	}
	// WHICH SHAPE — asked of the ONE discriminator (Rules 13/14): the same
	// function the default spelling, the ledger's advisory line and the ground
	// announcement all ask. A directory whose shape is not measured here is a
	// directory whose "main checkout" question cannot be answered at all.
	if devworkspace.GroundModelOf(dir) == devworkspace.GroundModelClone {
		return taskResumeGroundFromCloneWorktree(dir, worktree)
	}
	// WHICH REPOSITORY. For a LINKED worktree git's common dir is the MAIN
	// checkout's .git, so the main checkout is its parent — this is the reading
	// the worktree sweep uses to tell a guest from a resident
	// (worktreeSweepMainCheckoutOf), reused rather than re-derived (Rule 14).
	common, cerr := worktreeSweepCommonGitDir(dir)
	if cerr != nil {
		return out, fmt.Errorf("resume: --%s %s: cannot identify its repository: %w",
			TaskResumeGroundWorktreeKind, worktree, cerr)
	}
	main := worktreeSweepMainCheckoutOf(common)
	if main == "" {
		// An unrecognisable layout (bare/mirror common dir, or a `.git` file
		// whose parent is not a checkout). The question this resolver answers
		// next is "is this the shared checkout?", and it cannot answer it —
		// refusing is the only honest direction (the same conservative default
		// worktreeSweepMainCheckoutOf documents).
		return out, fmt.Errorf("resume: --%s %s: cannot tell whether it is a repository's MAIN checkout "+
			"(its git dir %s does not sit in a checkout) — refusing rather than guessing",
			TaskResumeGroundWorktreeKind, worktree, common)
	}
	if SameDir(main, dir) {
		return out, fmt.Errorf(taskResumeGroundMainCheckoutRefusalFmt,
			TaskResumeGroundWorktreeKind, worktree, main)
	}
	// The path must be a REGISTRATION, not merely a directory that happens to be
	// inside a repository: the chain resolves the directory it removes from the
	// repo's `git worktree list`, and a directory git does not know would be
	// resumed into but never cleaned up — the exact residue this mode exists to
	// end. The listing is the ONE reader (taskRepoLinkedWorktrees).
	linked, lerr := taskRepoLinkedWorktrees(main)
	if lerr != nil {
		return out, fmt.Errorf("resume: --%s %s: cannot list the worktrees of %s: %w",
			TaskResumeGroundWorktreeKind, worktree, main, lerr)
	}
	entry, ok := taskResumeGroundLinkedEntry(linked, dir)
	if !ok {
		return out, fmt.Errorf("resume: --%s %s is not a registered linked worktree of %s — "+
			"resuming needs ground git already knows (%d linked worktree(s) registered there)",
			TaskResumeGroundWorktreeKind, worktree, main, len(linked))
	}
	if entry.branch == "" {
		return out, fmt.Errorf("resume: --%s %s is a DETACHED worktree — the row is derived from the branch "+
			"it is checked out on, and a detached HEAD names none",
			TaskResumeGroundWorktreeKind, worktree)
	}
	out = TaskResumeGround{Dir: entry.path, Branch: entry.branch, Repo: main, Selector: TaskResumeGroundWorktreeKind}
	row, detail, rerr := taskResumeGroundRowForWorktree(entry.path, main)
	if rerr != nil {
		return TaskResumeGround{}, rerr
	}
	out.TaskID = row
	out.Detail = detail
	return out, nil
}

// taskResumeGroundFromCloneWorktree is the CLONE arm of `--worktree P`: P holds
// its own repository (`.git` is a DIRECTORY), nothing registers it, and the
// "main checkout" question the LINKED arm asks has no meaning here — which is
// exactly why the LINKED arm's test refused EVERY clone: `worktreeSweepMainCheckoutOf(P/.git)`
// answers P itself, so `SameDir(main, dir)` was trivially true.
//
// THE PROTECTION THIS ARM KEEPS, because it is the one that matters: a SHARED
// CHECKOUT is never handed to a resume that closes a row out by removing its
// ground. Removing the operator's tree is the disaster the LINKED arm's
// main-checkout refusal exists to prevent, and the shape is the same. What
// changes is only the INSTRUMENT, because a clone cannot be detected by "am I
// the parent of the common git dir" (it IS its own parent). Two measurements,
// both facts about the directory rather than about a preference:
//
//	① it SERVES linked worktrees     `git worktree list` names at least one
//	                                 registration beside the main entry — a
//	                                 workspace cloned for one run serves none
//	                                 (measured 2026-10-27: this run's own clone
//	                                 ground registers zero).
//	② the settings `repos` table     a DECLARED repository is by definition the
//	   declares it                   host's shared checkout (it is where the
//	                                 merger and the operator work).
//
// An UNREADABLE settings file answers ② false — the same direction
// task_submit_repo_declared_warning takes: "I could not look" is not evidence,
// and a resume refused for a reason nobody can act on is worse than the risk it
// hedges (① is still measured, and the close-out chain's own dirty/holder guards
// hold a real handle on the directory).
func taskResumeGroundFromCloneWorktree(dir, worktree string) (TaskResumeGround, error) {
	var out TaskResumeGround
	if served, why := taskResumeGroundSharedCheckoutReason(dir); served {
		return out, fmt.Errorf(taskResumeGroundCloneSharedCheckoutFmt,
			TaskResumeGroundWorktreeKind, worktree, why)
	}
	// The branch the clone carries — the row's other deliverable axis, and the
	// one fact the LINKED arm reads off the registration (`entry.branch`). A
	// clone has no registration to read, so the branch comes from git itself
	// through the SAME judge the ledger's advisory line uses
	// (devworkspace.ResolveGroundState → its Branch field), never a second
	// parser (Rules 13/14).
	st := devworkspace.ResolveGroundState(dir)
	if !st.Alive {
		return out, fmt.Errorf("resume: --%s %s is not a live ground (model %s): %s",
			TaskResumeGroundWorktreeKind, worktree, string(st.Model), st.Detail)
	}
	if st.Branch == "" {
		// The same refusal the LINKED arm makes for a detached registration, in
		// the clone's own words: the row is derived from the branch, and a
		// detached HEAD names none.
		return out, fmt.Errorf("resume: --%s %s is a DETACHED clone workspace — the row is derived "+
			"from the branch it is checked out on, and a detached HEAD names none",
			TaskResumeGroundWorktreeKind, worktree)
	}
	// The row comes from the SAME lookup the LINKED arm uses. No repo narrowing:
	// a clone ground's row records the SOURCE repository it was cloned from in
	// deliverable.repo, not this directory, so narrowing by it would ask the
	// ledger a question no row answers (measured 2026-10-27 on this run's own
	// row: repo=/home/user/work/gopi, worktree=/home/user/workspace/<task>).
	row, detail, rerr := taskResumeGroundRowForWorktree(st.Dir, "")
	if rerr != nil {
		return TaskResumeGround{}, rerr
	}
	out = TaskResumeGround{
		Dir: st.Dir, Branch: st.Branch, Repo: st.Repo,
		Selector: TaskResumeGroundWorktreeKind, TaskID: row,
		Detail: detail + " (model clone: " + st.Dir + "/.git is a DIRECTORY, so the directory holds its " +
			"own repository and no worktree list registers it)",
	}
	return out, nil
}

// taskResumeGroundCloneSharedCheckoutFmt is the ONE spelling of the clone arm's
// shared-checkout refusal. It keeps the words "MAIN checkout" deliberately: the
// two shapes refuse ONE thing for ONE reason, and an operator who has learned the
// sentence on a linked worktree must recognise it on a clone (Rule 1b: the
// refusal and the tests agree by sharing it).
const taskResumeGroundCloneSharedCheckoutFmt = "resume: --%s %s is a repository's MAIN checkout — %s. " +
	"a repo-aware row works in a workspace of its own (the clone channel's ground), " +
	"and handing the shared checkout to a resume would let a close-out remove the tree the operator stands in"

// taskResumeGroundSharedCheckoutReason answers "is this clone-model directory a
// SHARED checkout rather than one run's workspace?", and returns the measured
// evidence in the operator's words rather than a bare true/false (Rule 15: a
// refusal nobody can re-derive is not an explanation).
func taskResumeGroundSharedCheckoutReason(dir string) (bool, string) {
	linked, err := taskRepoLinkedWorktrees(dir)
	if err == nil && len(linked) > 0 {
		return true, fmt.Sprintf("it serves %d linked worktree(s), so it is a repository other runs' "+
			"checkouts hang off, not one run's ground", len(linked))
	}
	if taskResumeGroundDeclaredRepoPath(dir) {
		return true, fmt.Sprintf("it is a key of the settings %q table, which declares it as this "+
			"host's shared repository", config.ReposSettingsKey)
	}
	return false, ""
}

// taskResumeGroundDeclaredRepoPath reports whether dir is one of the settings
// `repos` table's own keys — the host's declaration that a directory is a SHARED
// repository rather than a run's ground. Both spellings are tried (byte
// equality first, then SameDir) for the reason taskSubmitRepoUndeclaredWarningFor
// documents: a trailing slash or a symlinked home is one directory, and a
// refusal for a spelling would be a lie about the filesystem.
//
// An unreadable settings table answers false — see the caller's header.
func taskResumeGroundDeclaredRepoPath(dir string) bool {
	dir = strings.TrimSpace(dir)
	if dir == "" {
		return false
	}
	declared, err := config.ResolveMergerRepos()
	if err != nil {
		return false
	}
	for _, d := range declared {
		d = strings.TrimSpace(d)
		if d == "" {
			continue
		}
		if d == dir || SameDir(d, dir) {
			return true
		}
	}
	return false
}

// taskResumeGroundLinkedEntry finds the registration whose PATH is dir — by
// directory identity (SameDir), not by string equality: an operator pasting
// `pwd` and an operator pasting the recorded path may spell one directory two
// ways, and answering "not a worktree" for a path that IS one would be a lie
// about the filesystem (the same one-worktree-not-three rule
// taskWorktreePathKey's callers follow).
func taskResumeGroundLinkedEntry(linked []taskLinkedWorktree, dir string) (taskLinkedWorktree, bool) {
	for _, e := range linked {
		if SameDir(e.path, dir) {
			return e, true
		}
	}
	return taskLinkedWorktree{}, false
}

// taskResumeGroundRowForWorktree derives the ledger row from the worktree path.
//
// The plurality is handled by the SHARED policy (TaskForBranchResolvedRow: the
// live owner first, else the newest row — one ordering opinion, Rule 14), and
// the count of the rows passed over is reported rather than hidden: an operator
// resuming a ground four rows name must be able to see that three were not
// chosen.
func taskResumeGroundRowForWorktree(worktree, repo string) (taskID, detail string, err error) {
	lookup, lerr := TaskForWorktree(TaskForWorktreeQuery{Worktree: worktree, Repo: repo}, "")
	if lerr != nil {
		return "", "", fmt.Errorf("resume: ledger lookup for --%s %s: %w",
			TaskResumeGroundWorktreeKind, worktree, lerr)
	}
	return taskResumeGroundRowFromLookup(lookup, TaskResumeGroundWorktreeKind, worktree)
}

// taskResumeGroundFromBranch resolves the `--branch B` spelling.
//
// The REPO is not an input here — the operator named a branch, and which
// repository holds it is part of the answer. It comes from the ledger rows that
// name B, and each candidate repo is then asked for its own registration of B
// (taskGroundResolveList — the claim-time reader, reused rather than
// reimplemented). The first candidate that has a LIVE worktree wins, and the
// scan is REPORTED: a branch named by rows in two repos is a real shape (two
// hosts, two deliverables), and resolving it silently would be a coin flip.
func taskResumeGroundFromBranch(branch string) (TaskResumeGround, error) {
	var out TaskResumeGround
	lookup, err := TaskForBranch(TaskForBranchQuery{Branch: branch}, "")
	if err != nil {
		return out, fmt.Errorf("resume: ledger lookup for --%s %s: %w", TaskResumeGroundBranchKind, branch, err)
	}
	if !lookup.Readable {
		// An unreadable ledger is NOT "no row owns this branch" — the two must
		// never render the same way (Rule 15).
		return out, fmt.Errorf("resume: cannot read the ledger to find the row that owns --%s %s",
			TaskResumeGroundBranchKind, branch)
	}
	tried := make([]string, 0, len(lookup.Owners))
	for _, owner := range lookup.Owners {
		repo := strings.TrimSpace(owner.Repo)
		if repo == "" {
			tried = append(tried, owner.TaskID+" (no repo recorded)")
			continue
		}
		dir, ok := taskGroundResolveList(repo, branch)
		if !ok {
			tried = append(tried, repo+" (no live worktree on this branch)")
			continue
		}
		// The registration could be the MAIN checkout itself (a row whose branch
		// IS the repo's current one). That is the same defect the path spelling
		// refuses, and the branch spelling must not be the way around it.
		if main := worktreeSweepMainCheckoutOf(mustCommonGitDir(repo)); main != "" && SameDir(main, dir) {
			tried = append(tried, repo+" (the branch is checked out in the MAIN checkout, not a linked worktree)")
			continue
		}
		out = TaskResumeGround{
			Dir: dir, Branch: branch, Repo: repo,
			TaskID: owner.TaskID, Selector: TaskResumeGroundBranchKind,
			Detail: fmt.Sprintf("resolved by %s: repo %s holds a registered linked worktree on %s; row %s recorded it",
				TaskResumeGroundBranchKind, repo, branch, owner.TaskID),
		}
		return out, nil
	}
	if len(lookup.Owners) == 0 {
		return out, taskResumeGroundNoRowRefusal(branch, lookup)
	}
	// Rows DO name the branch; none of their repos has it checked out. The
	// refusal carries what was tried, because "the branch exists in the ledger
	// but no directory holds it" is a state the operator has to repair (create
	// the ground, or resume by path).
	return out, fmt.Errorf("resume: no ground to resume — the ledger names --%s %s, but no repo has a LIVE worktree checked out on it (tried: %s)",
		TaskResumeGroundBranchKind, branch, strings.Join(tried, "; "))
}

// taskResumeGroundNoRowRefusal renders the refusal for a branch NO ROW names —
// after MEASURING the ground, which is the whole repair.
//
// The pre-fix sentence ("no ground to resume — no ledger row names --branch B")
// claimed a property of the FILESYSTEM from a fact about the LEDGER. The two
// were measured against each other on 2026-09-27 and disagreed: the ground
// existed and was live. So the ledger answer is followed by the git reading —
// TaskGitProvenanceFor, the ONE reader `gopi task show --branch` already uses
// (task_deliverable_git.go), which resolves the candidate repositories exactly
// as the selector does (ledger rows → declared repos → working directory) and
// then reports the ref, the tip and the checkout. Reusing it is Rule 14's whole
// point: this verb and that one must not be able to disagree about where a
// branch lives.
func taskResumeGroundNoRowRefusal(branch string, lookup TaskForBranchLookup) error {
	sel := DeliverableSelector{Branch: branch}
	res := DeliverableResolution{Readable: lookup.Readable, RowsWithBranch: lookup.RowsWithBranch, Owners: lookup.Owners}
	p := TaskGitProvenanceFor(context.Background(), sel, res)
	// An inconclusive read is NOT an absence (Rule 15, p.RefKnown): a branch we
	// could not look at must never be reported as one that is not there.
	if !p.RepoKnown || !p.RefKnown {
		return fmt.Errorf(taskResumeGroundNoRowUnknownFmt, TaskResumeGroundBranchKind, branch,
			strings.Join(p.Notes, "; "))
	}
	if !p.RefExists {
		// The repo axis is REPORTED as inferred, never as a bare path: this
		// spelling takes no repo input, so p.Repo is a candidate the LEDGER
		// named. A refusal that printed it unqualified reads as a verdict on
		// the branch (see taskResumeGroundRepoInferredFmt).
		return fmt.Errorf(taskResumeGroundNoRowRefGoneFmt, TaskResumeGroundBranchKind, branch,
			fmt.Sprintf(taskResumeGroundRepoInferredFmt, p.Repo))
	}
	tip := taskResumeGroundAxisText(p.Tip)
	// THE GROUND EXISTS. Which shape it is decides the operator's repair, so
	// each shape gets its own sentence rather than one hedged line.
	if p.Checkout != "" {
		if SameDir(p.Checkout, p.Repo) {
			return fmt.Errorf(taskResumeGroundNoRowMainCheckoutFmt,
				TaskResumeGroundBranchKind, branch, p.Checkout, p.Repo)
		}
		if !taskPathExists(p.Checkout) {
			return fmt.Errorf(taskResumeGroundNoRowStaleFmt,
				TaskResumeGroundBranchKind, branch, p.Repo, p.Checkout)
		}
		return fmt.Errorf(taskResumeGroundNoRowGroundFmt, TaskResumeGroundBranchKind, branch,
			p.Repo, p.Checkout, tip, TaskResumeGroundBranchKind, branch)
	}
	return fmt.Errorf(taskResumeGroundNoRowNoCheckoutFmt, TaskResumeGroundBranchKind, branch, p.Repo, tip)
}

// mustCommonGitDir is the non-failing spelling of the common-git-dir probe for
// the ONE place the answer decides a REFUSAL rather than an answer: a repo whose
// common dir cannot be read yields "" here, and the caller then keeps the
// candidate out of the running (it reports what it tried). An unreadable repo
// must never be mistaken for "this is a fine linked worktree" (Rule 15).
func mustCommonGitDir(repo string) string {
	common, err := worktreeSweepCommonGitDir(repo)
	if err != nil {
		return ""
	}
	return common
}

// taskResumeGroundRowFromLookup applies the shared owner policy to a lookup and
// renders the evidence line. It is the ONE place the plurality is decided, so
// the two spellings cannot pick different rows for the same ground.
func taskResumeGroundRowFromLookup(lookup TaskForBranchLookup, kind, value string) (taskID, detail string, err error) {
	if !lookup.Readable {
		return "", "", fmt.Errorf("resume: cannot read the ledger to find the row that owns --%s %s", kind, value)
	}
	owner, ok := TaskForBranchResolvedRow(lookup)
	if !ok {
		return "", "", fmt.Errorf("resume: no ledger row names --%s %s (%d row(s) carry a branch) — "+
			"there is nothing to resume: the ground may be a foreign checkout, or the row was deleted",
			kind, value, lookup.RowsWithBranch)
	}
	detail = fmt.Sprintf("resolved by %s: %d row(s) name it, chose %s (%s, %s)",
		kind, len(lookup.Owners), owner.TaskID, taskResumeGroundOwnerKind(owner), owner.Status)
	return owner.TaskID, detail, nil
}

// taskResumeGroundOwnerKind names WHY a row was the one chosen — the live owner
// first (it still owes work through this branch), else the newest row of any
// other kind (the provenance record the branch ended with). Spelled from the
// owner's own recorded facts rather than re-deriving the policy (Rules 13/14).
func taskResumeGroundOwnerKind(owner TaskForBranchOwner) string {
	if owner.Live {
		return "the in-flight owner"
	}
	return "the newest recorded owner"
}

// taskResumeGroundReport renders the resolution as the caller's account of what
// it is about to do — one line, re-derivable from the fields (Rule 15: a
// verdict nobody can re-read is not an explanation).
func (g TaskResumeGround) Report() string {
	return fmt.Sprintf("resume ground: dir=%s branch=%s repo=%s row=%s (%s) — %s",
		g.Dir, g.Branch, g.Repo, g.TaskID, g.Selector, g.Detail)
}
