// THE GIT HALF of a deliverable answer — what git says about the branch or
// worktree the operator asked for, for the case where the LEDGER says nothing.
//
// WHY IT EXISTS (measured 2026-09-23). `gopi task show --branch <B>` answers
// from the ledger's deliverable blobs alone (task_deliverable_selector.go): no
// row records B ⇒ "no ledger row names branch B", an error, a non-zero exit.
// The operator ran it on the branch he was STANDING IN — his own live,
// never-dispatched worktree (`git branch --show-current` printed
// fix/memory-escape-decode, the worktree held uncommitted work) — and the verb
// told him the branch does not exist as far as gopi is concerned. Both facts
// were true and only one of them was useful: the ledger genuinely had no row
// (measured: 0 of the 1019 branch-carrying rows name it), and git genuinely had
// the branch (tip ccdd5f1e, checked out in a linked worktree).
//
// A run only records its deliverable when it goes through the DISPATCH path
// (devdoc/branch-task-linkage.md §2: "per-task WORKTREES should be created by
// gopi itself … we cannot assign worktree that no task has"). Work an operator
// or another agent creates by hand — the exact work the linkage epic exists to
// make visible — therefore has NO row, and the selector's primary use case ("I
// am in a worktree; which row is this?") collapses into a refusal that reads
// like "this branch does not exist".
//
// WHAT THE ANSWER IS. The ledger fact and the git fact, SEPARATE and LABELLED.
// This file never merges them: `devdoc/branch-task-linkage.md` §7 item [5/5]
// already rules that a git-only answer must say WHICH channel answered and
// "must not be silently merged into a ledger answer", and the distinction is
// what tells the operator whether to dispatch a task (no row, no record — a
// dispatch is owed) or to read the row (a record exists, go look at it).
//
// READ-ONLY, like every other member of this family: no ledger write, no
// status move, no notify event, no git command that mutates anything.
//
// THE GIT COMMANDS ARE THE EXISTING ONES. The ref probe is repoSweepRefPresent
// (repo_sweep_ref_guard.go — the ONE "does this ref resolve to a commit"
// predicate, which already distinguishes git's named absence from an
// inconclusive probe), the tip is repoSweepReadTip, the linked-worktree answer
// is finishCleanupBranchWorktree (the ONE `git worktree list --porcelain`
// reader), and the dirty check is AdoptionGitStatus. A second spelling of any
// of them would be free to disagree with the sweep about the same repository
// (Rules 13/14).

package tools

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/nickhuang99/gopi/internal/config"
)

const (
	// TaskGitProvenanceLabel is the block's first line: it names the CHANNEL
	// the lines under it were read from, so an operator — or a script grepping
	// for the ledger's own fields — can never mistake a git reading for a
	// ledger record (Rule 15, and branch-task-linkage.md §7's "the answer must
	// say which channel answered").
	TaskGitProvenanceLabel = "git provenance (read from the git repository now — NOT recorded in the ledger):"

	// The verdict sentences. A sentence per OUTCOME (present / absent), never
	// one sentence with a boolean spliced in: the two answers lead to different
	// operator actions (read the checkout / dispatch a task) and must not be
	// mistakable for one another at a glance.
	taskGitBranchExistsFmt = "branch %q EXISTS in %s at %s"
	taskGitBranchGoneFmt   = "branch %q does NOT exist in %s — git has no such ref"
	// taskGitWorktreeExistsFmt is the worktree selector's own verdict: the
	// directory IS a work tree; the branch line below says which branch it has.
	taskGitWorktreeExistsFmt = "worktree %s is a git work tree"
	// taskGitWorktreeGoneFmt is the absence, named as absence — never a blank.
	taskGitWorktreeGoneFmt = "worktree %s is not a readable git work tree"

	// The checkout half: WHERE the branch is, and whether that place holds work
	// that exists nowhere else. Both are facts the operator standing in a
	// dirty, unrecorded worktree needs before touching anything.
	taskGitCheckedOutFmt     = "checked out at %s"
	taskGitMainCheckoutFmt   = "checked out in the main checkout %s"
	taskGitNotCheckedOutText = "not checked out in any worktree of that repository"
	taskGitDirtyFmt          = "that worktree HAS uncommitted work: %s"
	taskGitCleanText         = "that checkout has no uncommitted work"

	// The ledger half of the answer, stated in the SAME block so the operator
	// sees both channels at once: the read succeeded and nobody names it, or the
	// read failed and the answer is UNKNOWN (never "no owner" — Rule 15).
	taskGitLedgerNoRowFmt      = "the ledger was read: %d row(s) carry a branch and none names %s %q"
	taskGitLedgerNoRowEmptyFmt = "the ledger was read: no row carries a branch at all"
	taskGitLedgerUnreadableFmt = "the ledger could NOT be read, so whether a row claims it is UNKNOWN"
	taskGitNoRepoText          = "no git repository could be located for this selector (neither a ledger row nor the declared repos table nor the working directory supplied one)"
	taskGitRepoNotWorktreeFmt  = "%s is not a readable git repository"
	taskGitSeveralReposFmt     = "%d declared repositories contain %q — a branch name is not unique per host; read each: %s"
	taskGitDeclaredReposErrFmt = "the declared repos table could not be read (%v), so only the ledger and the working directory supplied repositories"
	// taskGitBranchUncheckableFmt is the honest limit: no candidate repository
	// could be read, so whether the branch exists there is UNKNOWN — never
	// "does not exist" (an inconclusive probe must not be rendered as absence).
	taskGitBranchUncheckableFmt = "%s %q could not be checked — no readable git repository"

	// taskGitWorktreeBranchNoteFmt / taskGitDetachedHeadFmt say which branch a
	// readable worktree has.
	taskGitWorktreeBranchNoteFmt = "%s has branch %q checked out"
	taskGitDetachedHeadFmt       = "%s has a detached HEAD (no branch checked out)"
)

const (
	// taskGitAbbrevRefFlag / taskGitHeadToken spell the branch-at-a-directory
	// probe: `git -C <dir> rev-parse --abbrev-ref HEAD`. On a detached HEAD git
	// answers with the literal "HEAD" (its own token, named here so the check
	// below is not a bare string — Rule 1b).
	taskGitAbbrevRefFlag = "--abbrev-ref"
	taskGitHeadToken     = "HEAD"

	// taskGitRevParseVerb / taskGitShortFlag / taskGitCommonDirFlag spell the
	// two other git questions this file asks (Rule 1b, one spelling each):
	// "abbreviate this ref" and "which repository is this checkout part of".
	taskGitRevParseVerb    = "rev-parse"
	taskGitShortFlag       = "--short"
	taskGitCommonDirFlag   = "--git-common-dir"
	taskGitSameRepoFmt     = "%s is the SAME repository as %s — one repository, reached by two paths, read once"
	taskGitCommonDirErrFmt = "%s: the repository identity could not be read (%v)"
)

// TaskGitProvenance is what git says about one deliverable selector: the
// repository that was read, whether the name resolves there, its tip, where it
// is checked out, and whether that checkout holds uncommitted work.
//
// Every field is OPTIONAL-but-honest: an axis that could not be read is
// reported through Notes (the why), never left blank and never guessed — the
// same contract the advisory ground line follows (task_ground_advisory.go).
type TaskGitProvenance struct {
	// Kind is "branch" or "worktree" — the selector's own spelling (it names
	// what the VERDICT sentence is about).
	Kind string
	// Value is the branch name or the worktree path asked about.
	Value string
	// Repo is the repository that was read ("" when none could be located).
	Repo string
	// RepoKnown reports that Repo is a real directory the probes ran in. It is
	// separate from `Repo != ""` so a repository that was found but could not
	// be read is never rendered as "no repository" (Rule 15).
	RepoKnown bool
	// RefExists reports the branch resolves to a commit in Repo. Meaningful
	// only when RefKnown (an inconclusive probe is neither present nor absent).
	RefExists bool
	// RefKnown reports the ref probe actually answered.
	RefKnown bool
	// Tip is the branch's commit in Repo ("" when unknown).
	Tip string
	// Checkout is the worktree path the branch is checked out at ("" = not
	// checked out anywhere; Repo itself when it is the main checkout).
	Checkout string
	// Dirty is the one-line `git status --porcelain` body of Checkout ("" =
	// clean or nothing checked out).
	Dirty string
	// Notes carries every honest limit of this reading, in the order it was
	// discovered.
	Notes []string
}

// TaskGitProvenanceFor reads the git side of `sel` — the answer that stands in
// for a ledger row when the ledger has none.
//
// res is the failed (or empty) ledger resolution: it supplies the ledger half
// (readable? how many rows carry a branch?) so the two channels are reported
// from ONE reading rather than two lookups that could disagree.
func TaskGitProvenanceFor(ctx context.Context, sel DeliverableSelector, res DeliverableResolution) TaskGitProvenance {
	branch := strings.TrimSpace(sel.Branch)
	worktree := strings.TrimSpace(sel.Worktree)
	if branch != "" {
		return taskGitProvenanceForBranch(ctx, branch, res)
	}
	return taskGitProvenanceForWorktree(worktree, res)
}

// taskGitProvenanceForBranch answers for a BRANCH selector: which of the
// candidate repositories has the branch, where it is checked out, and whether
// that checkout is dirty.
func taskGitProvenanceForBranch(ctx context.Context, branch string, res DeliverableResolution) TaskGitProvenance {
	p := TaskGitProvenance{Kind: deliverableSelectorBranchKind, Value: branch}
	candidates, notes := taskGitProvenanceRepos(res)
	p.Notes = append(p.Notes, notes...)
	// WHICH REPOSITORY THIS ANSWER IS ABOUT is stated before the reading (epic
	// 01a0e074, gap 2): the candidate list is led by the repository the LEDGER's
	// row records, and a reader who expected the shell's own checkout to be read
	// must be told, in words, which one answered.
	p.Notes = append(p.Notes, res.RepoNotes...)

	// WHICH REPOSITORY ANSWERS. A branch name is not unique per host (the same
	// name lives in several repos — a fact the ledger itself records per row),
	// so the repositories that actually CONTAIN the branch outrank the rest, and
	// when more than one does, the reading is reported and the ambiguity NAMED
	// rather than silently resolved (Rule 15).
	//
	// A LINKED WORKTREE IS NOT A SECOND REPOSITORY. Every task worktree is a
	// checkout OF the same repository as its main checkout, and `git worktree
	// add` is exactly how gopi makes them — so without a repository-identity
	// check the operator's own worktree (`…/gopi-<task>`, added inside a listed
	// test worktree) enters as a "second repository that contains the branch"
	// and the answer reports a phantom ambiguity. Measured 2026-09-23 (the
	// reported invocation, run from the fix's own worktree): the note read "2
	// declared repositories contain …" about ONE repository.
	candidates, notes = taskGitProvenanceDistinctRepos(candidates)
	p.Notes = append(p.Notes, notes...)
	if len(candidates) == 0 {
		p.Notes = append(p.Notes, taskGitNoRepoText)
		return p
	}
	var holders []string
	var readable []string
	for _, repo := range candidates {
		exists, err := repoSweepRefPresent(repo, branch)
		if err != nil {
			p.Notes = append(p.Notes, fmt.Sprintf("%s: %v", repo, err))
			continue
		}
		readable = append(readable, repo)
		if exists {
			holders = append(holders, repo)
		}
	}
	switch {
	case len(holders) > 0:
		p.Repo, p.RepoKnown, p.RefKnown, p.RefExists = holders[0], true, true, true
	case len(readable) > 0:
		p.Repo, p.RepoKnown, p.RefKnown, p.RefExists = readable[0], true, true, false
	default:
		// Every candidate failed the probe: the reading is inconclusive, and an
		// inconclusive read is never a disappearance (Rule 15). Name the first
		// candidate so the operator knows what was tried.
		p.Repo, p.RepoKnown = candidates[0], false
		p.Notes = append(p.Notes, fmt.Sprintf(taskGitRepoNotWorktreeFmt, candidates[0]))
		return p
	}
	if len(holders) > 1 {
		p.Notes = append(p.Notes, fmt.Sprintf(taskGitSeveralReposFmt, len(holders), branch, strings.Join(holders, ", ")))
	}
	if !p.RefExists {
		return p
	}
	p.Tip = shortSha(repoSweepReadTip(p.Repo, branch))
	p.Checkout, p.Dirty = taskGitBranchCheckout(ctx, p.Repo, branch)
	return p
}

// taskGitProvenanceForWorktree answers for a WORKTREE selector. A linked
// worktree shares its repository's ref store, so every probe runs with the
// worktree directory ITSELF as the repository: `git -C <worktree>` resolves
// the same refs, and it answers the one question the operator actually has
// ("what is checked out HERE, and is it dirty?") without needing to find the
// main checkout first.
func taskGitProvenanceForWorktree(path string, res DeliverableResolution) TaskGitProvenance {
	p := TaskGitProvenance{Kind: deliverableSelectorWorktreeKind, Value: path}
	if path == "" {
		p.Notes = append(p.Notes, taskGitNoRepoText)
		return p
	}
	p.Repo = path
	branch, err := taskGitCurrentBranchAt(path)
	if err != nil {
		p.Notes = append(p.Notes, fmt.Sprintf(taskGitRepoNotWorktreeFmt, path))
		return p
	}
	p.RepoKnown, p.RefKnown = true, true
	if branch == "" {
		// A detached HEAD is a real answer, not a missing one: the worktree
		// exists, it just is not on any branch.
		p.Notes = append(p.Notes, fmt.Sprintf(taskGitDetachedHeadFmt, path))
	} else {
		p.RefExists = true
		p.Tip = shortSha(repoSweepReadTip(path, branch))
		p.Notes = append(p.Notes, fmt.Sprintf(taskGitWorktreeBranchNoteFmt, path, branch))
	}
	if dirty, statusErr := AdoptionGitStatus(path); statusErr == nil && dirty != "" {
		p.Dirty = reclaimPorcelainOneLine(dirty)
	}
	return p
}

// taskGitBranchCheckout reports where a branch is checked out in repo
// ("" = nowhere) and the one-line dirty state of that checkout ("" = clean).
//
// The linked half is finishCleanupBranchWorktree (the ONE porcelain reader);
// the main-checkout half is the current branch of repo itself, because `git
// worktree list --porcelain` deliberately reports the FIRST record — the main
// checkout — separately and that reader never returns it (a main checkout is
// not a linked worktree by that contract, but it IS where a branch can live).
func taskGitBranchCheckout(ctx context.Context, repo, branch string) (checkout, dirty string) {
	if linked := finishCleanupBranchWorktree(ctx, repo, branch); linked != "" {
		checkout = linked
	} else if current, err := taskGitCurrentBranchAt(repo); err == nil && current == branch {
		checkout = repo
	}
	if checkout == "" {
		return "", ""
	}
	if body, err := AdoptionGitStatus(checkout); err == nil && body != "" {
		dirty = reclaimPorcelainOneLine(body)
	}
	return checkout, dirty
}

// taskGitCurrentBranchAt returns the branch checked out at dir ("" when the
// HEAD is detached) and an error when dir is not a readable work tree. It is
// the non-exempting sibling of taskBranchFromWorkdir: that one answers "what
// may be backfilled as a deliverable" (so it hides master/main and archive
// refs), and this one answers "what is here", which must not hide anything.
func taskGitCurrentBranchAt(dir string) (string, error) {
	out, err := repoSweepGit(dir, repoSweepRefProbeVerb, taskGitAbbrevRefFlag, taskGitHeadToken)
	if err != nil {
		return "", err
	}
	name := strings.TrimSpace(out)
	if name == taskGitHeadToken {
		return "", nil // detached HEAD — git answers with the literal "HEAD"
	}
	return name, nil
}

// taskGitProvenanceDistinctRepos collapses the candidate list to DISTINCT
// REPOSITORIES: two paths that are checkouts of the same repository (a main
// checkout and its linked worktree — how every gopi task worktree is made)
// answer `git rev-parse --git-common-dir` with the same directory, and reading
// the same repository twice would report an ambiguity that does not exist.
//
// The first sighting of a repository wins (candidates arrive in precedence
// order) and the merge is REPORTED as a note, so the collapse is auditable
// rather than silent — the same contract the plural-owner note follows (Rule
// 15). A path whose identity cannot be read is kept as its own candidate: an
// unreadable answer never silently removes a repository from the search.
func taskGitProvenanceDistinctRepos(candidates []string) ([]string, []string) {
	var out []string
	var notes []string
	identity := map[string]string{} // repository identity key → the path that claimed it
	for _, path := range candidates {
		key, err := taskGitRepoIdentityKey(path)
		if err != nil {
			notes = append(notes, fmt.Sprintf(taskGitCommonDirErrFmt, path, err))
			out = append(out, path)
			continue
		}
		if first, seen := identity[key]; seen {
			notes = append(notes, fmt.Sprintf(taskGitSameRepoFmt, path, first))
			continue
		}
		identity[key] = path
		out = append(out, path)
	}
	return out, notes
}

// taskGitRepoIdentityKey returns the key two paths share when they are the SAME
// repository: the absolute path of git's own common directory (`git rev-parse
// --git-common-dir` — the .git directory all of a repository's checkouts share,
// which is precisely what makes a linked worktree the same repository as its
// main checkout).
//
// A relative answer is resolved against the path it was asked about, because
// git answers relative to the directory when it is inside the repository.
func taskGitRepoIdentityKey(path string) (string, error) {
	out, err := repoSweepGit(path, taskGitRevParseVerb, taskGitCommonDirFlag)
	if err != nil {
		return "", err
	}
	common := strings.TrimSpace(out)
	if common == "" {
		return "", fmt.Errorf("git %s %s returned nothing", taskGitRevParseVerb, taskGitCommonDirFlag)
	}
	if !filepath.IsAbs(common) {
		common = filepath.Join(path, common)
	}
	abs, absErr := filepath.Abs(common)
	if absErr != nil {
		return "", absErr
	}
	return filepath.Clean(abs), nil
}

// taskGitProvenanceRepos is the ordered candidate list of repositories to read
// a BRANCH selector against, plus the notes explaining what could not be added.
//
// Order is precedence, and it is the operator's own:
//
//	① a repository the LEDGER names for this branch — a row that records the
//	   branch in another repo is the strongest evidence of where it lives;
//	② the DECLARED repos table (settings.json "repos" — Rule 12's contract, and
//	   the merger's own sweep list);
//	③ the process's working directory — the shell the operator typed the verb
//	   in is, in the ordinary case, the checkout they are asking about.
//
// A repository appears once (deduped by path key, the worktree selector's own
// comparison) and a directory that cannot be read is simply not a candidate —
// the caller reports the inconclusive read.
func taskGitProvenanceRepos(res DeliverableResolution) ([]string, []string) {
	var notes []string
	var out []string
	seen := map[string]bool{}
	add := func(path string) {
		path = strings.TrimSpace(path)
		if path == "" {
			return
		}
		key := taskWorktreePathKey(path)
		if key == "" || seen[key] {
			return
		}
		seen[key] = true
		out = append(out, path)
	}

	for _, owner := range res.Owners {
		add(owner.Repo)
	}
	// A caller that already decided the list (ResolveTaskIDByDeliverable) hands
	// it back: the ledger rows are in it, in the same precedence, and the two
	// halves then read the same repositories by construction (Rules 13/14).
	for _, r := range res.Repos {
		add(r)
	}
	declared, err := config.ResolveMergerRepos()
	if err != nil {
		notes = append(notes, fmt.Sprintf(taskGitDeclaredReposErrFmt, err))
	}
	sort.Strings(declared) // ResolveMergerRepos is already sorted; kept explicit
	for _, repo := range declared {
		add(repo)
	}
	if cwd, cwdErr := os.Getwd(); cwdErr == nil {
		add(cwd)
	} else {
		// The working directory is only a convenience candidate: a failure to
		// read it removes one source, it does not fail the answer (Rule 15).
		notes = append(notes, fmt.Sprintf("the working directory could not be read (%v)", cwdErr))
	}
	return out, notes
}

// taskGitLedgerLine is the ledger half of the answer, in one sentence: what the
// ledger read actually established. It is rendered from the resolution's own
// counters so the sentence and the lookup can never disagree about how much was
// read (Rule 15).
func taskGitLedgerLine(sel DeliverableSelector, res DeliverableResolution) string {
	if !res.Readable {
		return taskGitLedgerUnreadableFmt
	}
	what, value := deliverableSelectorBranchKind, strings.TrimSpace(sel.Branch)
	if strings.TrimSpace(sel.Worktree) != "" {
		what, value = deliverableSelectorWorktreeKind, strings.TrimSpace(sel.Worktree)
	}
	if res.RowsWithBranch == 0 {
		return taskGitLedgerNoRowEmptyFmt + fmt.Sprintf(" (asked about %s %q)", what, value)
	}
	return fmt.Sprintf(taskGitLedgerNoRowFmt, res.RowsWithBranch, what, value)
}

// Text renders the whole answer block: the labelled git reading, the ledger
// half, and every honest limit. It ends with a newline so a caller can print it
// as-is.
func (p TaskGitProvenance) Text(sel DeliverableSelector, res DeliverableResolution) string {
	var b strings.Builder
	b.WriteString(TaskGitProvenanceLabel + "\n")
	for _, line := range p.lines() {
		b.WriteString("  " + line + "\n")
	}
	b.WriteString("  ledger: " + taskGitLedgerLine(sel, res) + "\n")
	for _, note := range p.Notes {
		b.WriteString("  note: " + note + "\n")
	}
	return b.String()
}

// lines is the git half's own sentences: the verdict first (is the thing there
// at all), then where it is and whether that place holds work.
func (p TaskGitProvenance) lines() []string {
	var out []string
	switch {
	case p.Kind == deliverableSelectorWorktreeKind:
		// The WORKTREE's verdict is its own sentence in every state — the
		// directory either is a work tree (and the note says which branch is
		// checked out) or it is not, and "no repository could be located" would
		// be a different (and false) claim about a path the operator named.
		if p.RefKnown && p.RefExists {
			out = append(out, fmt.Sprintf(taskGitWorktreeExistsFmt, p.Value))
		} else {
			out = append(out, fmt.Sprintf(taskGitWorktreeGoneFmt, p.Value))
		}
	case !p.RepoKnown:
		out = append(out, fmt.Sprintf(taskGitBranchUncheckableFmt, p.Kind, p.Value))
	case p.RefKnown && p.RefExists:
		out = append(out, fmt.Sprintf(taskGitBranchExistsFmt, p.Value, p.Repo, p.Tip))
	default:
		out = append(out, fmt.Sprintf(taskGitBranchGoneFmt, p.Value, p.Repo))
	}
	if p.Kind == deliverableSelectorBranchKind && p.RefKnown && p.RefExists {
		switch {
		case p.Checkout == "":
			out = append(out, taskGitNotCheckedOutText)
		case p.Checkout == p.Repo:
			out = append(out, fmt.Sprintf(taskGitMainCheckoutFmt, p.Checkout))
		default:
			out = append(out, fmt.Sprintf(taskGitCheckedOutFmt, p.Checkout))
		}
	}
	// The dirt sentence is printed only where a checkout was actually READ: a
	// path that is no repository has no working tree to call clean, and saying
	// so would be a claim about nothing (Rule 15).
	if p.Dirty != "" {
		out = append(out, fmt.Sprintf(taskGitDirtyFmt, p.Dirty))
	} else if p.Checkout != "" || (p.Kind == deliverableSelectorWorktreeKind && p.RepoKnown) {
		out = append(out, taskGitCleanText)
	}
	return out
}

// DeliverableGitProvenanceAnswer is the ONE entry point the show face uses: it
// reads git for the selector and renders the full block. It returns a non-empty
// answer for every input, so a caller never has to invent a sentence of its own
// (and cannot invent a second, disagreeing one).
func DeliverableGitProvenanceAnswer(ctx context.Context, sel DeliverableSelector, res DeliverableResolution) string {
	return TaskGitProvenanceFor(ctx, sel, res).Text(sel, res)
}
