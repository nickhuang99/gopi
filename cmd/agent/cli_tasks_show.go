package main

// runTaskShow is the `gopi task show` CLI face (epic 01a0bb4d follow-up,
// gap B). It renders the row's own digest through tools.TaskShow and NOTHING
// else — no second renderer, no second read of the ledger.
//
// WHY IT EXISTS AS A NAMED SEAM: the advisory ground line lives INSIDE
// tools.TaskShow (tools/task_ground_advisory.go), so that the shell verb, the
// `/task show` slash face (internal/agent → commands.go) and any future
// consumer all get the same line from the same resolution — one renderer for
// one line (Rules 13/14). This wrapper exists so cmd/agent's own suite can
// drive the CLI's show face in-process and assert the line reaches the
// OPERATOR'S output, which is the acceptance the epic states.
//
// Usage errors and ledger errors stay fatal exactly as they were (the verb
// prints them to stderr and exits 1); the ADVISORY line's own failure modes
// never surface here — they are rendered as a why-suffix on the line.

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/nickhuang99/gopi/internal/tools"
)

// runTaskShow renders one row for the show verb.
//
// THE ROW IS ADDRESSED THREE WAYS and all three land on the same renderer:
// the task_id positional, --branch <b> (the branch the row's deliverable
// records — what `git branch --show-current` prints in a task worktree), and
// --worktree <path> (its worktree directory — what `pwd` prints there). The
// operators' measured workflow is "I am in a task worktree; which row is
// this?", and the row id is the one thing that workflow does not carry — so
// the two deliverable selectors are resolved to a task id HERE, before the
// digest is rendered, by the SAME ledger-side resolver that owns the singular
// answer (tools.ResolveTaskIDByDeliverable → the shared live-first policy).
//
// A SHARED SELECTOR IS NOT AN ERROR. The same branch is commonly named by more
// than one row (the in-flight run plus the completed row the completion-time
// backfill stamped — see task_for_branch.go), so the resolution picks the owner
// by that one policy and prints the remaining rows as a NOTE line above the
// digest: the choice is visible, never silent (Rule 15). The note goes to
// STDERR so stdout stays exactly the digest a script already parses.
func runTaskShow(opts *tasksOptions) (string, error) {
	if opts == nil {
		return tools.TaskShow("")
	}
	taskID, provenance, err := resolveDeliverableSelector(context.Background(), opts)
	if err != nil {
		// The provenance block of a selector no row records rides the SAME
		// output channel as a digest (it is returned, and the CLI prints it
		// BEFORE it reports the refusal), while every other failure mode keeps
		// the silent-stdout shape: resolveDeliverableSelector returns "" for
		// all of them (see runTaskShow's caller in cli_tasks.go).
		return provenance, err
	}
	if taskID == "" {
		return tools.TaskShow("")
	}
	return tools.TaskShow(taskID)
}

// resolveDeliverableSelector is the ONE place a row is addressed by a
// DELIVERABLE (`--branch` / `--worktree`) instead of by its task id — the seam
// `gopi task show` and `gopi task status` both call, so the two cannot disagree
// about which row a selector names (Rule 14: one resolution, two renderers).
//
// It returns the resolved task id, the output a face should print BEFORE it
// reports the refusal ("" when there is nothing to print), and the error.
//
// The three address shapes and their ONE outcome each:
//
//   - a task_id (positional) and no selector: the id comes back unchanged, no
//     ledger read, no git command — the canonical address never pays for the
//     selector machinery.
//   - exactly one selector: resolved through tools.ResolveTaskIDByDeliverable
//     (the live-first policy lives there and nowhere else). A SHARED selector
//     prints the plurality NOTE on stderr so stdout stays exactly what the
//     verb's own renderer produces.
//   - a selector NO row names: the git provenance block (what git itself holds
//     for that branch or worktree, LABELLED as read from git, never a ledger
//     record) is returned as output, and the error is tools.ErrDeliverableNoOwner
//     so a caller can tell "the ledger was read and no row names it" from "the
//     ledger could not be read" — the two must never be confusable (Rule 15).
//
// The `Row` field is the task_id the operator ALSO gave, if any: it is the one
// fact that decides which repository the git half reads when the selector
// resolves no row (epic 01a0e074, gap 2).
func resolveDeliverableSelector(ctx context.Context, opts *tasksOptions) (string, string, error) {
	taskID := strings.TrimSpace(opts.taskID)
	sel := tools.DeliverableSelector{
		Branch:   strings.TrimSpace(opts.selBranch),
		Worktree: strings.TrimSpace(opts.selWorktree),
		Row:      taskID,
	}
	if sel.Branch == "" && sel.Worktree == "" {
		return taskID, "", nil
	}
	res, err := tools.ResolveTaskIDByDeliverable(sel, opts.db)
	if err != nil {
		// NO LEDGER ROW IS NOT "NO SUCH BRANCH" (measured 2026-09-23: the
		// operator asked about the branch he was standing in — a live
		// worktree with uncommitted work — and got a refusal that read like
		// the branch did not exist). A run only records its deliverable
		// through the DISPATCH path, so work created by hand has no row and
		// the selector's primary question collapses into that refusal.
		//
		// So the refusal keeps its callers' own exit code (the ROW was not
		// resolved — scripts relying on the non-zero status are unaffected)
		// while the operator gets the two facts SEPARATELY LABELLED: what the
		// ledger read established, and what git says about the branch or
		// worktree itself. The block's own first line says it is not a ledger
		// record, so a reader cannot mistake a git reading for one. It is
		// RETURNED as output (not printed here) so each face keeps owning its
		// own stdout, exactly as the CLI does for a digest.
		if errors.Is(err, tools.ErrDeliverableNoOwner) {
			return "", tools.DeliverableGitProvenanceAnswer(ctx, sel, res), err
		}
		return "", "", err
	}
	if note := tools.DeliverableResolutionNote(res, sel); note != "" {
		fmt.Fprintln(os.Stderr, note)
	}
	return res.TaskID, "", nil
}
