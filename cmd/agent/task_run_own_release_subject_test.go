package main

// The close-out release pass prints two kinds of line, and until 2026-10-27 it
// printed BOTH under `task <id>:` — the prefix that claims "this is that row's
// state". Measured (row 01a17dcd's close-out on this host):
//
//	task 01a17dcd-c59c-7344-a99a-83133d4416c7: own-worktree-release: own registered worktree kept: /home/user/workspace/cptest-verify — worktree is DIRTY (uncommitted work) — left for a human
//
// The directory was a chat's hand-made verification checkout; the seven files in
// it belonged to a DIFFERENT task. So the row id is now printed ONLY for a line
// the pass classifies as its own ground (tools.ReclaimSelfOwnIsOwnLine — the
// pass's own derivation, never re-derived here).

import (
	"strings"
	"testing"

	"github.com/nickhuang99/gopi/internal/tools"
)

const taskRunOwnReleaseTestRow = "01a17dcd-c59c-7344-a99a-83133d4416c7"

// TestOwnReleaseLineCarriesTheRowOnlyWhenItIsTheRowsGround is the seam's half of
// the defect: the SAME body text must be rendered differently depending on the
// subject the pass asserted.
func TestOwnReleaseLineCarriesTheRowOnlyWhenItIsTheRowsGround(t *testing.T) {
	body := "own registered worktree kept: /home/user/workspace/cptest-verify — worktree is DIRTY (uncommitted work)"

	var own strings.Builder
	taskRunPrintOwnReleaseLine(&own, taskRunOwnReleaseTestRow, body)
	if !strings.Contains(own.String(), "task "+taskRunOwnReleaseTestRow+": own-worktree-release: ") {
		t.Fatalf("an own-ground line lost its row prefix: %q", own.String())
	}

	var unowned strings.Builder
	taskRunPrintOwnReleaseLine(&unowned, taskRunOwnReleaseTestRow,
		tools.ReclaimSelfOwnUnownedLead+body)
	got := unowned.String()
	if strings.Contains(got, taskRunOwnReleaseTestRow) {
		t.Fatalf("an unowned line names the closing row (%q) — the 2026-10-27 defect", got)
	}
	if !strings.HasPrefix(got, "own-worktree-release: ") {
		t.Fatalf("an unowned line must keep the pass's own prefix, got %q", got)
	}
	// Rule 15: the observation itself is still there, verbatim.
	if !strings.Contains(got, body) {
		t.Fatalf("the observation was dropped, got %q", got)
	}
}
