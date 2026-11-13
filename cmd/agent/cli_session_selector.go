// The session verbs' DELIVERABLE SELECTOR: `gopi session log|raw|stats` addressing a
// session by the branch (`git branch --show-current`) or the worktree directory
// (`pwd`) the operator is standing in, instead of by a <session-id> that
// workflow does not carry.
//
// WHY IT IS ONE FILE, and not three local copies. `gopi session log`, `gopi session raw` and
// `gopi session stats` are three faces of one question — "which session am I looking
// at?" — and the operator typing any of them holds the same two facts. Three
// copies of the admission/parse/copy loop is three chances for `gopi session raw
// --branch X` to mean something `gopi session stats --branch X` does not; the spellings,
// the copy into the session-id slot and the plurality note live here once
// (Rules 1b/13), and each verb's dispatch calls the one function.
//
// THE ROW RESOLUTION IS NOT HERE: internal/tools owns it
// (tools.ResolveSessionByDeliverable → the shared live-first deliverable policy
// `gopi task show --branch` already uses), so `gopi session log --branch X` and `gopi
// task show --branch X` can never name different rows for one address.
//
// EXACTLY ONE INPUT. The selector names the SAME input as the positional
// <session-id>, so passing both, or combining the selector with a different
// selector (--last / --all / --file), is refused by name — never silently
// preferring one (Rule 15: an accepted-and-ignored option is a defect).

package main

import (
	"fmt"
	"os"
	"strings"

	"github.com/nick/gopi/internal/tools"
)

// sessionBranchFlag / sessionWorktreeFlag are the two deliverable selectors, by
// the CLI's ONE spelling of them (the ledger group's constants — a second
// spelling of `--branch` would be the drift Rules 1b/13 close).
const (
	sessionBranchFlag   = taskBranchFlagName
	sessionWorktreeFlag = taskWorktreeFlagName
)

// sessionSelectorFromArgs reports the deliverable selector a session verb's argv
// names, or a nil-shaped (empty) one when the operator addressed the session the
// ordinary way. It is deliberately NOT the flag table's job: the table decides
// ADMISSION (is --branch a flag of this verb at all), this reads the VALUE, and
// the two live together in the same commit.
//
// Both spellings (`--branch X` and `--branch=X`) are read, because the verbs'
// shared flag parser accepts both and a selector only the `=` form reached would
// be a flag that half works.
func sessionSelectorFromArgs(args []string) (tools.DeliverableSelector, error) {
	var sel tools.DeliverableSelector
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case a == "--"+sessionBranchFlag:
			if i+1 >= len(args) || strings.HasPrefix(args[i+1], "--") {
				return sel, fmt.Errorf("--%s needs a value: --%s <branch>", sessionBranchFlag, sessionBranchFlag)
			}
			sel.Branch = strings.TrimSpace(args[i+1])
			i++
		case strings.HasPrefix(a, "--"+sessionBranchFlag+"="):
			sel.Branch = strings.TrimSpace(strings.TrimPrefix(a, "--"+sessionBranchFlag+"="))
		case a == "--"+sessionWorktreeFlag:
			if i+1 >= len(args) || strings.HasPrefix(args[i+1], "--") {
				return sel, fmt.Errorf("--%s needs a value: --%s <worktree path>", sessionWorktreeFlag, sessionWorktreeFlag)
			}
			sel.Worktree = strings.TrimSpace(args[i+1])
			i++
		case strings.HasPrefix(a, "--"+sessionWorktreeFlag+"="):
			sel.Worktree = strings.TrimSpace(strings.TrimPrefix(a, "--"+sessionWorktreeFlag+"="))
		}
	}
	if sel.Branch == "" && strings.Contains(strings.Join(args, "\n"), "--"+sessionBranchFlag+"=") {
		// `--branch=` (an EMPTY value) is a mistake, not "no selector": the
		// operator named the flag and handed it nothing, and reading it as
		// absent would fall through to "missing <session-id>" — a message about
		// a different problem (Rule 15).
		return sel, fmt.Errorf("--%s needs a value: --%s <branch>", sessionBranchFlag, sessionBranchFlag)
	}
	if sel.Worktree == "" && strings.Contains(strings.Join(args, "\n"), "--"+sessionWorktreeFlag+"=") {
		return sel, fmt.Errorf("--%s needs a value: --%s <worktree path>", sessionWorktreeFlag, sessionWorktreeFlag)
	}
	if sel.Branch != "" && sel.Worktree != "" {
		return sel, fmt.Errorf("--%s and --%s are two ways to name ONE session, not a filter pair — pass one",
			sessionBranchFlag, sessionWorktreeFlag)
	}
	return sel, nil
}

// sessionSelectorInputCheck refuses the two shapes a session verb may not be
// invoked with, both of them "two inputs where one was meant" (Rule 15 — an
// accepted-and-ignored option is a defect, and a silently-preferring one is
// worse):
//
//   - the selector beside the verb's own <session-id> positional: the SAME
//     input, named twice. positional is that parsed positional ("" = none).
//   - the selector beside a selector of a DIFFERENT input: `others` names the
//     ones actually given (`--last`, `--all`, `--file`), in the verb's own
//     spelling, so the message names the invocation the operator typed.
//
// It is silent when the selector is absent, so a verb calls it unconditionally
// in one place.
func sessionSelectorInputCheck(sel tools.DeliverableSelector, positional string, others []string) error {
	if sel.Branch == "" && sel.Worktree == "" {
		return nil
	}
	if p := strings.TrimSpace(positional); p != "" {
		return fmt.Errorf("--%s %s and the <session-id> positional %q are the SAME input — pass one",
			sessionSelectorFlagInUse(sel), sessionSelectorValue(sel), p)
	}
	if len(others) > 0 {
		return fmt.Errorf("--%s %s and %s are different inputs — pass one",
			sessionSelectorFlagInUse(sel), sessionSelectorValue(sel), strings.Join(others, "/"))
	}
	return nil
}

// sessionSelectorFlagInUse names the selector flag the operator used (the
// branch spelling unless the worktree one was given) — one definition, so a
// refusal can never name the flag that was not typed.
func sessionSelectorFlagInUse(sel tools.DeliverableSelector) string {
	if sel.Worktree != "" {
		return sessionWorktreeFlag
	}
	return sessionBranchFlag
}

// sessionSelectorValue is the value of the selector for a message.
func sessionSelectorValue(sel tools.DeliverableSelector) string {
	if sel.Worktree != "" {
		return sel.Worktree
	}
	return sel.Branch
}

// sessionIDForSelector resolves the selector to the session id the verb should
// read, rendering the SHARED plurality note on stderr when more than one row
// names the selector (the same note `gopi task show` prints, from the same
// function — the choice is visible, never silent, and stdout stays exactly the
// output a script already parses).
func sessionIDForSelector(verb string, sel tools.DeliverableSelector) (string, error) {
	brief, err := tools.ResolveSessionByDeliverable(sel, "")
	if err != nil {
		// The row was resolved but nothing has run for it: say WHICH row, so
		// the operator can go look at it rather than wonder which of several
		// rows the selector picked.
		if brief.TaskID != "" {
			return "", fmt.Errorf("gopi %s: %s (%w)", verb, brief.TaskID, err)
		}
		return "", fmt.Errorf("gopi %s: %w", verb, err)
	}
	if note := tools.DeliverableResolutionNote(tools.DeliverableResolution{
		TaskID:         brief.TaskID,
		Owners:         brief.Owners,
		Readable:       brief.Readable,
		RowsWithBranch: brief.RowsWithBranch,
	}, sel); note != "" {
		fmt.Fprintln(os.Stderr, note)
	}
	return brief.SessionID, nil
}
