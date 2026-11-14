package agent

import (
	"strings"
	"testing"

	"github.com/nickhuang99/gopi/internal/tools"
)

// TestCloseOutSkipSummaryNamesTheStepsThatRan pins the AGENT face of the
// measured defect of task 01a12dc4.
//
// The console's close-out signal and the CLI's one-line report are composed from
// different call sites — `finishCleanupRepSummary` here, `finishCleanupReportLine`
// in the tools package — and they must not disagree about which steps ran. Before
// the fix the agent face re-typed the verdict (`"skipped: " + reason`) and dropped
// the successful steps entirely, so a chain that had just PUSHED its branch logged
// itself as a skip. The tail comes from the tools package's one renderer; this test
// is what fails if the agent face ever re-inlines its own copy again.
func TestCloseOutSkipSummaryNamesTheStepsThatRan(t *testing.T) {
	rep := &tools.FinishCleanupReport{
		TaskID: "t-agent", Branch: "task/x",
		Skipped:    true,
		SkipReason: "delivery owed: the recorded worktree directory is not there (delivery not established)",
		Steps: []tools.FinishCleanupStep{
			{Step: "push", Target: "task/x", OK: true},
			{Step: "worktree", Detail: "removal skipped"},
		},
	}
	got := finishCleanupRepSummary(rep)
	if !strings.HasPrefix(got, tools.FinishCleanupSkipVerdict) {
		t.Errorf("the agent summary must lead with the shared verdict token, got %q", got)
	}
	if !strings.Contains(got, "push") {
		t.Errorf("the landed push must appear in the agent summary, got %q", got)
	}
	if strings.Contains(got, "done=[") {
		t.Errorf("a skipped chain must not claim a completed one, got %q", got)
	}

	// A skip with nothing successful carries no tail — the summary must not
	// invent one (Rule 15).
	bare := finishCleanupRepSummary(&tools.FinishCleanupReport{
		TaskID: "t-bare", Skipped: true, SkipReason: "no such task row",
	})
	if strings.Contains(bare, "steps that DID run") {
		t.Errorf("a skip with no successful step must carry no tail, got %q", bare)
	}
}
