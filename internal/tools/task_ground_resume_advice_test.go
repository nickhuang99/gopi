package tools

// task_ground_resume_advice_test.go — the SECOND half of `gopi task show`'s
// ground advisory: the row NAMES which resume face applies to it.
//
// WHY IT IS ON THE ROW. Two faces resume work — `gopi session resume` (holds no
// ground, releases none) and `gopi worker --resume <task-id>` (takes the ground
// over and RELEASES the worktree) — and the operator's starting point is the
// ROW, not a help page. So the choice is printed where it is made.
//
// WHICH FACE IS PRINTED IS DECIDED BY THE READING, NOT BY THE RECORD: a row
// whose recorded worktree directory is gone is told the session face, because
// the worker face would refuse that row. The red proof for the classification
// is therefore a pair of cases over the SAME row shape with one live and one
// dead ground.

import (
	"context"
	"os"
	"strings"
	"testing"

	"github.com/nick/gopi/internal/cliverbs"
)

// TestTaskShowResumeAdviceFollowsTheLiveReading is the classifier: a live
// ground gets the worker face, a dead one gets the session face, and the two
// are never printed together (a row that printed both would leave the reader
// where they started).
func TestTaskShowResumeAdviceFollowsTheLiveReading(t *testing.T) {
	t.Setenv(memoryDBEnv, t.TempDir()+"/memory.db")
	f := newAdvisoryShowFixture(t, "feat/resume-advice", "sess-resume-advice")
	advisorySeedRow(t, "t-advice-live", `{"repo":"`+f.repo+`","branch":"`+f.branch+`","worktree":"`+f.wt+`"}`, f.session)

	out, err := TaskShow("t-advice-live")
	if err != nil {
		t.Fatalf("TaskShow: %v", err)
	}
	advice := advisoryResumeAdviceLine(t, out)
	if !strings.Contains(advice, cliverbs.ResumeTaskRowAddress) {
		t.Errorf("a row with a LIVE ground must be pointed at the ground-holding face %q, got %q",
			cliverbs.ResumeTaskRowAddress, advice)
	}
	if strings.Contains(advice, cliverbs.ResumeSessionAddress) {
		t.Errorf("the live-ground row must NOT also be offered the session face (one claim, not a menu): %q", advice)
	}
	if !strings.Contains(advice, "releases it") {
		t.Errorf("the advice must say what the face it names DOES with the ground (Rule 15): %q", advice)
	}
}

// TestTaskShowResumeAdviceSendsADeadGroundToTheSessionFace pins the other half
// of the classifier, and the reason it follows the READING: the worker face
// resolves an existing ground and refuses a row whose worktree is gone, so
// pointing such a row at it would send the operator to a refusal.
func TestTaskShowResumeAdviceSendsADeadGroundToTheSessionFace(t *testing.T) {
	t.Setenv(memoryDBEnv, t.TempDir()+"/memory.db")
	repo := t.TempDir()
	advisorySeedRow(t, "t-advice-dead", `{"repo":"`+repo+`","branch":"feat/gone","worktree":"`+repo+`/no-such-worktree"}`, "sess-advice-dead")

	out, err := TaskShow("t-advice-dead")
	if err != nil {
		t.Fatalf("TaskShow: %v", err)
	}
	advice := advisoryResumeAdviceLine(t, out)
	if !strings.Contains(advice, cliverbs.ResumeSessionAddress) {
		t.Errorf("a row whose ground is GONE must be pointed at the session face %q, got %q",
			cliverbs.ResumeSessionAddress, advice)
	}
	if strings.Contains(advice, cliverbs.ResumeTaskRowAddress) {
		t.Errorf("the dead-ground row must not be sent to the face that would refuse it: %q", advice)
	}
	if !strings.Contains(advice, "released") {
		t.Errorf("the advice must say why the session face fits (nothing is held, so nothing is released): %q", advice)
	}
}

// TestTaskShowResumeAdviceNeverWrites is the advisory's own invariant carrying
// over to the new line: the show is read-only by construction, and the advice
// is part of the show.
func TestTaskShowResumeAdviceNeverWrites(t *testing.T) {
	t.Setenv(memoryDBEnv, t.TempDir()+"/memory.db")
	f := newAdvisoryShowFixture(t, "feat/resume-advice-ro", "sess-resume-advice-ro")
	blob := `{"repo":"` + f.repo + `","branch":"` + f.branch + `","worktree":"` + f.wt + `"}`
	advisorySeedRow(t, "t-advice-ro", blob, f.session)

	before := advisoryRawDeliverable(t, "t-advice-ro")
	if _, err := TaskShow("t-advice-ro"); err != nil {
		t.Fatalf("TaskShow: %v", err)
	}
	if after := advisoryRawDeliverable(t, "t-advice-ro"); after != before {
		t.Fatalf("the advice line must not touch the recorded deliverable:\nbefore %s\nafter  %s", before, after)
	}
}

// advisoryResumeAdviceLine returns the advice line, failing loudly when it is
// missing (Rule 15: an absent line is a failure, never a skipped assertion).
func advisoryResumeAdviceLine(t *testing.T, show string) string {
	t.Helper()
	for _, l := range strings.Split(show, "\n") {
		if strings.HasPrefix(l, cliverbs.ResumeGroundLineLabel) {
			return l
		}
	}
	t.Fatalf("show carries no %q line:\n%s", cliverbs.ResumeGroundLineLabel, show)
	return ""
}

// advisoryRawDeliverable reads a row's deliverable column verbatim.
func advisoryRawDeliverable(t *testing.T, taskID string) string {
	t.Helper()
	ctx := context.Background()
	conn, err := openTasksDB(ctx, os.Getenv(memoryDBEnv))
	if err != nil {
		t.Fatalf("open ledger: %v", err)
	}
	defer conn.Close()
	var blob string
	if err := conn.QueryRowContext(ctx, `SELECT deliverable FROM tasks WHERE task_id=?`, taskID).Scan(&blob); err != nil {
		t.Fatalf("read deliverable: %v", err)
	}
	return blob
}
