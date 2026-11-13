package tools

import (
	"testing"
)

// TestTaskBashWorkdirCandidates_ReadOnlyCarryIsNotABind is the minimal sample
// (PROJECT.md Rule 10.4/10.5) for the read-only probe that BOUND a row through the
// SHELL spelling of the same inspection.
//
// THE DEFECT (measured on master fa12d97f, red-proofed before the fix): a round
// that only LOOKS at a directory must never become a bind candidate — the
// candidates are what the creation-time backfill stamps into
// deliverable.worktree, and a stamped worktree makes that row its PROTECTED
// OWNER (the sweeper's ownership arm keeps a named worktree alive). The `-C`
// spelling was already gated on a mutating subcommand; the `cd` carry added for
// the branch axis (f42f8cb4) was gated on nothing, so `cd <someone else's
// worktree> && git status` re-opened the hijack through the other door and the
// guard test TestTaskDeliverableWorktreeBashReadOnlyProbeDoesNotBind went red
// on master.
//
// The sample separates exactly the two cases: the same directory, the same
// `cd` shape, differing only in the git subcommand — read-only (no candidate)
// vs mutating/branch-creating (candidate). Anything broader would not tell the
// two apart; anything narrower would pass with the carry deleted.
func TestTaskBashWorkdirCandidates_ReadOnlyCarryIsNotABind(t *testing.T) {
	inspected := t.TempDir() // another task's worktree, as the scan sees it
	workdir := t.TempDir()   // the round's own directory, never the subject

	carries := func(command string) bool {
		for _, c := range taskBashWorkdirCandidates(workdir, command) {
			if c == inspected {
				return true
			}
		}
		return false
	}

	readOnly := []string{
		"cd " + inspected + " && git status",
		"cd " + inspected + " && git status --porcelain",
		"cd " + inspected + " && git log --oneline -1",
		"cd " + inspected + " && git diff --stat",
		"cd " + inspected + " && git worktree list",
	}
	for _, command := range readOnly {
		if carries(command) {
			t.Errorf("a read-only probe bound a candidate: %q — the inspecting row then becomes that worktree's protected owner and the sweeper can never collect it", command)
		}
	}

	// The other half of the same property (the seam must not have been silenced
	// altogether): the SAME `cd` with a binding subcommand still carries it.
	binding := []string{
		"cd " + inspected + " && git commit --allow-empty -m work",
		"cd " + inspected + " && git checkout -b feat/x",
		"cd " + inspected + " && git switch -c feat/x",
		"cd " + inspected + " && git push origin HEAD",
	}
	for _, command := range binding {
		if !carries(command) {
			t.Errorf("a mutating round in the cd-carried directory must still bind: %q (candidates %v)", command, taskBashWorkdirCandidates(workdir, command))
		}
	}
}
