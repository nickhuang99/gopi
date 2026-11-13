// Deliverable selector → ONE row: "show me the task that owns this branch /
// this worktree".
//
// WHY IT EXISTS. `gopi task show` addressed a row by its task id alone, while
// the operator standing in a git worktree holds `git branch --show-current` or
// `pwd` — never the row id. The ledger already records both facts on the row's
// deliverable blob, so the missing piece was a SELECTOR: read the blob, find
// the row, render the digest.
//
// WHY IT IS NOT A SECOND LOOKUP. The plural answer ("every row that names this
// branch") is TaskForBranch / TaskForWorktree, and this file is the ONE
// narrowing of that answer to one row — through the SAME policy the merge fence
// uses (TaskForBranchResolvedRow: a LIVE owner outranks a decided one). A
// second tie-break rule here would let `task show --branch X` and
// `gopi merge --status` disagree about who owns X, which is the drift Rule 13/14
// forbid and the exact bug the live-owner fence epic fixed.
//
// WHY PLURALITY IS A NOTE, NOT AN ERROR. Measured on the live ledger
// (2026-09-16, recorded in task_for_branch.go's header): the same branch is
// COMMONLY named by more than one row — the in-flight run plus the completed
// row the completion-time backfill stamped. An ambiguity refusal would fail on
// precisely the branch whose owner is most interesting. So the resolution
// picks the owner by the shared policy and REPORTS the other rows, so the
// choice is visible rather than silent (Rule 15).

package tools

import (
	"errors"
	"fmt"
	"strings"
)

// The selector's two spellings, named once (Rule 1b): the resolver, the git
// provenance reader and every message that names which of the two was asked
// about key on these, so "branch" is never spelled twice with two meanings.
const (
	deliverableSelectorBranchKind   = "branch"
	deliverableSelectorWorktreeKind = "worktree"
)

// ErrDeliverableNoOwner is the ONE sentinel a caller can test to tell "the
// ledger was READ and no row records this selector" from every other failure
// (a usage error, an unreadable ledger, a resolution that came back empty).
//
// It exists because the two answers now lead to DIFFERENT operator advice: a
// selector nobody recorded is the case where the git provenance is worth
// reading (the work may exist and simply never have gone through the dispatch
// path), while an unreadable ledger is a read failure to fix. Matching on the
// error's prose would make that distinction a string comparison (Rule 15);
// errors.Is makes it a contract.
var ErrDeliverableNoOwner = errors.New("deliverable selector: no ledger row names it")

// The two sentences the git half's candidate list is reported with (epic
// 01a0e074, gap 2). They are named here, next to the resolution that produces
// them, because the resolution and the reading that consumes it must agree
// about WHICH repository answered and why (Rule 1b).
const (
	// DeliverableRepoFromRowNoteFmt names the repository the LEDGER's row
	// recorded — the first candidate, and the one the reading is about.
	DeliverableRepoFromRowNoteFmt = "reading the repository the row records: %s (the working directory is only a fallback)"
	// DeliverableRepoUnrecordedNote is the FALLBACK, said out loud: the row
	// records no repository, so the reading falls back to the declared repos
	// table and then the working directory.
	DeliverableRepoUnrecordedNote = "the row records no repository, so the reading falls back to the declared repos table and the working directory"
)

// deliverableNoOwnerError is that sentinel WRAPPED in the operator's own
// sentence: Unwrap carries the classification (errors.Is), while Error()
// renders the explanation the operator reads.
//
// The split is deliberate. Splicing the sentinel's text into the refusal (the
// `%w` habit) made the verb print "…when it is created through the dispatch
// path: deliverable selector: no ledger row names it" — the operator's answer
// ending in a duplicated sentence about an internal sentinel. The message is
// the deliverable here; the sentinel is plumbing.
type deliverableNoOwnerError struct{ msg string }

func (e deliverableNoOwnerError) Error() string { return e.msg }
func (e deliverableNoOwnerError) Unwrap() error { return ErrDeliverableNoOwner }

// DeliverableSelector addresses a ledger row by what its deliverable blob
// records instead of by its task id: the branch (git branch --show-current) or
// the worktree directory (pwd). Exactly ONE of the two must be set — they are
// two spellings of one selector, not a filter pair (setting both is refused by
// name rather than silently preferring one).
type DeliverableSelector struct {
	Branch   string
	Worktree string
	// Repo, when non-empty, narrows the answer to rows whose deliverable names
	// that repo (the same branch name legitimately exists in several repos).
	Repo string
	// Row, when non-empty, is a row id the caller ALREADY holds (the positional
	// `gopi task show <id> --branch B`). It is not a second selector: the
	// selector still decides WHICH row is shown, and Row supplies the ONE fact
	// the selector cannot when it resolves nothing — the repository the git
	// half must read (epic 01a0e074, gap 2). Measured 2026-09-27: the operator
	// asked about a branch a row's own deliverable.repo named, and the reading
	// answered about whatever repository the process stood in.
	Row string
}

// DeliverableResolution is the answer: the resolved row plus EVERY row that
// named the selector (the candidates, in the plurality's own order — live
// first). Owners carries more than one entry when the selector is shared, which
// is the ordinary shape for a branch; the caller renders the rest as a note so
// the resolved row's precedence is auditable rather than implicit.
type DeliverableResolution struct {
	TaskID string
	Owners []TaskForBranchOwner
	// Readable records that the ledger was READ at all, and RowsWithBranch how
	// many rows it holds that carry a branch (the evidence size). They are
	// carried OUT of the resolver even when it resolves no row, so the
	// no-owner face can say "the ledger was read, N rows name a branch, none is
	// this one" instead of guessing — and so "no owner" can never be rendered
	// from an unreadable ledger (Rule 15).
	Readable       bool
	RowsWithBranch int
	// Repos is the ordered set of repositories the GIT half must read, decided
	// HERE and carried out with the answer (epic 01a0e074, gap 2): the row the
	// call resolved is the only thing that knows which repositories the
	// selector is about, and the show face used to ignore it and read whatever
	// repository the process happened to stand in — measured 2026-09-27: the
	// operator asked about a row whose deliverable records /home/nick/work/GoPi
	// and the git half answered about his desktop's own checkout instead, then
	// declared the two "the SAME repository".
	//
	// Order is precedence (see taskGitProvenanceRepos): the repositories the
	// LEDGER names for this selector, then the DECLARED repos table (Rule 12),
	// then the working directory — the process's own directory LAST, because it
	// is the weakest evidence of where the asked ground lives.
	Repos []string
	// RepoNotes carries what could not be added to Repos (an unreadable
	// declared-repos table, a working directory that could not be read). It
	// rides with the answer so the git half renders the limit instead of
	// silently reading a shorter candidate list (Rule 15).
	RepoNotes []string
	// Reconciled reports that the owner was RECONSTRUCTED from evidence rather
	// than read from a recorded axis (epic 01a0e074, historical owner-less
	// grounds): the ledger names no row, so the answer came from the ground's
	// git registration plus the session that minted it
	// (task_ground_reconcile.go). A reader must be able to tell a RECORDED
	// owner from a RECONSTRUCTED one at a glance (Rule 15).
	Reconciled bool
	// ReconcileNote is the Rule-15b sentence of the reconciliation — WHAT was
	// looked for (the git registration, the rows of that repository alive when
	// it was created, the minting command in their sessions) and WHERE. It is
	// set even when the reconciliation named no row, so the no-owner face
	// never renders a blank assertion.
	ReconcileNote string
}

// ResolveTaskIDByDeliverable returns the ONE task id a selector addresses.
//
// Errors (never a silent empty row — Rule 15):
//
//   - both selectors set, or neither: a usage error, refused by name;
//   - the ledger could not be read: the read error, propagated;
//   - NO row names the selector: an error that names the selector and points at
//     the plural verb, because "no row owns this" is an answer the operator must
//     not be able to mistake for a rendered row.
//
// It is a PURE READ: no claim, no status move, no ledger write, no git command.
func ResolveTaskIDByDeliverable(sel DeliverableSelector, dbPath string) (DeliverableResolution, error) {
	var out DeliverableResolution
	branch := strings.TrimSpace(sel.Branch)
	worktree := strings.TrimSpace(sel.Worktree)
	repo := strings.TrimSpace(sel.Repo)
	switch {
	case branch == "" && worktree == "":
		return out, fmt.Errorf("task: a deliverable selector needs a branch or a worktree — there is nothing to look up")
	case branch != "" && worktree != "":
		return out, fmt.Errorf("task: --branch and --worktree are two ways to name ONE row, not a filter pair — pass one")
	}

	var lookup TaskForBranchLookup
	var err error
	what, value := deliverableSelectorBranchKind, branch
	if branch != "" {
		lookup, err = TaskForBranch(TaskForBranchQuery{Branch: branch, Repo: repo}, dbPath)
	} else {
		what, value = deliverableSelectorWorktreeKind, worktree
		lookup, err = TaskForWorktree(TaskForWorktreeQuery{Worktree: worktree, Repo: repo}, dbPath)
	}
	if err != nil {
		return out, err
	}
	// THE ROW THE CALLER ALREADY HOLDS LEADS THE GIT HALF'S CANDIDATE LIST
	// (epic 01a0e074, gap 2). It is the one fact a selector cannot supply when
	// it resolves no row: the positional row id names the deliverable
	// (`gopi task show <id> --branch B`), and its recorded repo is where B is to
	// be read. Deciding the list HERE — at the ONE resolution every show face
	// goes through, from the row's own repo plus the rows the ledger matched —
	// is what keeps the two spellings from answering about different
	// repositories. The list is built in ONE call so its dedup and precedence
	// stay the reader's own (Rules 13/14).
	rowRepo := taskRowDeliverableRepo(strings.TrimSpace(sel.Row))
	out.Repos, out.RepoNotes = taskGitProvenanceRepos(DeliverableResolution{
		Repos: []string{rowRepo}, Owners: lookup.Owners,
	})
	if rowRepo != "" {
		out.RepoNotes = append([]string{fmt.Sprintf(DeliverableRepoFromRowNoteFmt, rowRepo)}, out.RepoNotes...)
	}
	out.Readable = lookup.Readable
	out.RowsWithBranch = lookup.RowsWithBranch
	out.Reconciled = lookup.Reconciled
	if len(lookup.Owners) == 0 {
		out.ReconcileNote = lookup.ReconcileNote
		return out, deliverableNoOwnerError{msg: fmt.Sprintf(
			"task: no ledger row names %s %q — a row records its deliverable when it is created through the dispatch path, so a deliverable this host never recorded has no owner here; `gopi task list` shows the rows that DO carry one",
			what, value)}
	}
	resolved, ok := TaskForBranchResolvedRow(lookup)
	if !ok {
		// Unreachable while the policy returns a row whenever Owners is
		// non-empty; stated as its own branch so a future policy change cannot
		// silently fall through to an empty task id (Rule 15).
		return out, fmt.Errorf("task: %s %q has %d candidate row(s) but none could be resolved to a task id",
			what, value, len(lookup.Owners))
	}
	out.TaskID = resolved.TaskID
	out.Owners = lookup.Owners
	// The ROW's own recorded repository decides which repository the git half
	// reads (epic 01a0e074, gap 2). It is already the FIRST candidate above —
	// the owner rows supply it, live owner first — so nothing is re-ordered
	// here; what this adds is the HONEST SENTENCE for the shape that has
	// nothing to resolve: a row that records no repository at all, where the
	// reading falls back to the declared table and the working directory. That
	// fallback must be stated, never left to look like a row-derived answer.
	if r := strings.TrimSpace(resolved.Repo); r != "" {
		out.RepoNotes = append(out.RepoNotes, fmt.Sprintf(DeliverableRepoFromRowNoteFmt, r))
	} else {
		out.RepoNotes = append(out.RepoNotes, DeliverableRepoUnrecordedNote)
	}
	return out, nil
}

// DeliverableResolutionNote renders the plural-owner note for a resolution: a
// SINGLE LINE naming every other row that also claimed the selector, or "" when
// the selector had exactly one owner. It is a line rather than a block so it
// rides above the digest without being mistaken for a field of it (the show
// digest is machine-read by scripts; a foreign line shape inside it would be
// read as a field).
func DeliverableResolutionNote(res DeliverableResolution, sel DeliverableSelector) string {
	if len(res.Owners) <= 1 {
		return ""
	}
	what, value := deliverableSelectorBranchKind, strings.TrimSpace(sel.Branch)
	if strings.TrimSpace(sel.Worktree) != "" {
		what, value = deliverableSelectorWorktreeKind, strings.TrimSpace(sel.Worktree)
	}
	others := make([]string, 0, len(res.Owners)-1)
	for _, o := range res.Owners {
		if o.TaskID == res.TaskID {
			continue
		}
		others = append(others, fmt.Sprintf("%s (%s)", o.TaskID, o.Status))
	}
	return fmt.Sprintf("%d rows name %s %s; showing %s. Other owner(s): %s",
		len(res.Owners), what, value, res.TaskID, strings.Join(others, ", "))
}
