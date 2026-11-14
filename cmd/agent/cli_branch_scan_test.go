package main

// The branch-backlog PUSH-BACK face, after F4 (devdoc/merge-resolve-effort-audit.md,
// task 01a0b1b3) retired the rest of this feature.
//
// WHAT SURVIVES. A successful push-back still records a 'branch-pushed' hint
// row (notifyBranchPushed) — that is the HP face of the db-sync epic: the
// branch becomes visible in the 台账 the moment it is pushed, before the
// center's sweep sees it.
//
// WHAT WAS RETIRED, and why the ~500 lines of tests around it are gone. This
// file used to guard the serve pass's unmerged-branch INVENTORY: agentBranchScan
// → merge-backlog rows (one per unmerged branch, deduped against terminal
// decisions), plus its throttle (serveBranchScanThrottleMin), its tip recorder
// (branchTip) and its archive-tag filter (archiveTagTips / isBranchArchived).
// The audit's F4 measured what the epic suspected: d11af9a6 (2026-09-04,
// "merger pass is config-driven per-repo sweep") deleted EVERY caller of that
// inventory together with serveMergeBacklogAudit and serveMergeExecSweep —
// `serveBranchScan()` is referenced only by its own definition and its own
// tests, so the whole pipeline was dead automatic dispatch that read as live.
// Its retirement is therefore not a behaviour change: nothing called it.
//
// THE LIVE FACES are the ones this file still pins — the push-back hint — plus
// the manual seams an operator drives: `gopi task merge [--submit]`,
// `gopi task audit-merges` and `gopi task merge-execute <task-id>` (pinned in
// cli_tasks_audit_test.go, which is also the retirement's own gate).
//
// The ledger-side half of the inventory is NOT dead and stays where it lives:
// tools.TaskOpenMergeBacklogBranches (internal/tools/tasks.go) is the
// "already covered by a decision row" reader the merge-backlog dedup contract
// needs, and internal/tools/task_merge_backlog_dedup_test.go exercises it there.

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/nick/gopi/internal/tools"
)

func listLedgerSummaries(t *testing.T, path string) []string {
	t.Helper()
	rows, err := tools.TaskList("")
	if err != nil {
		t.Fatalf("TaskList: %v", err)
	}
	var out []string
	for _, line := range strings.Split(rows, "\n") {
		if line == "" {
			continue
		}
		out = append(out, line)
	}
	return out
}

// record a branch-pushed hint row (tags=branch-pushed) naming the branch
func TestNotifyBranchPushed_RecordsHintTask(t *testing.T) {
	path := filepath.Join(t.TempDir(), "memory.db")
	t.Setenv("GOPI_MEMORY_DB", path)

	notifyBranchPushed("feat/hp-did-thing", "zpeer:~/work/GoPi")

	summaries := listLedgerSummaries(t, path)
	joined := strings.Join(summaries, "\n")
	if !strings.Contains(joined, "branch-pushed: feat/hp-did-thing awaits merge decision") {
		t.Fatalf("ledger must carry a branch-pushed hint for the branch, got:\n%s", joined)
	}
	if !strings.Contains(joined, "zpeer:~/work/GoPi") {
		t.Errorf("hint must name the push target repo, got:\n%s", joined)
	}
}

// the terminal guard reads back when it questions a terminal row).
