// Regression guard for a remedy the corpus taught and the state machine
// refuses (2026-10-30, task 01a1879a).
//
// The retired `gopi task reopen` briefings — and the doc page this test's
// sibling edit repairs — told a reader that a stuck row is put back in the
// queue with `gopi task update <id> --status pending`. It is not: the
// transition table admits no such edge from `claimed` or from `failed`, so
// following that advice produces a SECOND refusal, exactly the shape
// taskupdate_status_message_test.go was written about ("following the advice
// produced a second refusal").
//
// Why a table test rather than a CLI walk: the claim here is about ONE
// predicate (taskTransitionAllowed) that both the ledger writer and the
// refusal text read, so the smallest honest sample is the predicate itself
// plus the two strings the operator actually sees. Rule 31's execution
// evidence for the CLI surface is in the task report — 2026-10-30, a private
// sandbox ledger:
//
//	$ gopi task update <claimed-id> --status pending --reason probe
//	gopi task update error: task: illegal status transition claimed → pending (allowed from claimed: in_progress|completed|failed|verified|cancelled|superseded)
//	exit=1
//	$ gopi task update <failed-id> --status pending --reason p
//	gopi task update error: task: illegal status transition failed → pending (allowed from failed: in_progress|verified|completed)
//	exit=1
//
// The rows below are the POSITIVE half too: the documented doors must stay
// open, or this guard would pass while the recovery path it describes is gone.
package tools

import (
	"os"
	"strings"
	"testing"
)

func TestClaimedAndFailedCannotBeRequeuedByStatus(t *testing.T) {
	refused := []struct {
		from, to string
		allowed  string
	}{
		// `claimed` grew the exec-never-happened exits (epic 01a1a87b): a run that
		// fails BEFORE exec, and the two closers that never spawn a child, must be
		// able to reach a verdict without claiming an `in_progress` process. The
		// re-queue refusal this test guards is unchanged — `pending` is still not
		// on the list.
		{taskStatusClaimed, taskStatusPending, "in_progress|completed|failed|verified|cancelled|superseded"},
		{taskStatusFailed, taskStatusPending, "in_progress|verified|completed"},
	}
	for _, c := range refused {
		if taskTransitionAllowed(c.from, c.to) {
			t.Errorf("taskAllowedTransitions admits %s → %s — the state machine no longer refuses the re-queue the retired briefings taught", c.from, c.to)
			continue
		}
		advice := taskTransitionAdvice(c.from, c.to)
		if want := " (allowed from " + c.from + ": " + c.allowed + ")"; advice != want {
			t.Errorf("advice for %s → %s = %q, want %q — the refusal text is what the operator acts on", c.from, c.to, advice, want)
		}
	}

	// The doors the doc paragraph names must exist, in both directions of the
	// claim: `claimed` recovers through the resume edge, and `failed` keeps the
	// resume edge plus the two terminal exits.
	open := [][2]string{
		{taskStatusClaimed, taskStatusInProgress},
		{taskStatusFailed, taskStatusInProgress},
		{taskStatusFailed, taskStatusVerified},
		{taskStatusFailed, taskStatusCompleted},
	}
	for _, edge := range open {
		if !taskTransitionAllowed(edge[0], edge[1]) {
			t.Errorf("taskAllowedTransitions lost %s → %s — the documented remedy is now unreachable", edge[0], edge[1])
		}
	}

	// A same-status write stays a no-op (the source of the false "it worked"
	// reading: `--status pending` on a PENDING row exits 0 and proves nothing
	// about the claimed/failed case this test pins).
	if !taskTransitionAllowed(taskStatusPending, taskStatusPending) {
		t.Error("pending → pending must stay a no-op; the demo probe that misled the corpus depends on it")
	}
}

// TestTheLedgerArchitecturePageDoesNotTeachTheRefusedRequeue keeps the doc and
// the table in step at the one place they disagreed. It reads the tracked page
// by path (the test's own working directory is internal/tools) and refuses the
// sentence that taught the refused move OUTSIDE a paragraph that says it is
// refused — a bare teaching line, not a discussion of it.
func TestTheLedgerArchitecturePageDoesNotTeachTheRefusedRequeue(t *testing.T) {
	const page = "../../devdoc/gopi-task-ledger-architecture.md"
	b, err := os.ReadFile(page)
	if err != nil {
		t.Fatalf("read %s: %v", page, err)
	}
	body := string(b)
	const refusedSpelling = "gopi task update --status pending` puts it back"
	if strings.Contains(body, refusedSpelling) {
		t.Errorf("%s still teaches the refused re-queue (%q): the transition table admits claimed → in_progress|cancelled|superseded only, so that sentence sends a reader into a second refusal", page, refusedSpelling)
	}
	// Non-vacuity: the page must still be the page this test is about.
	for _, marker := range []string{"### `claimed` — the crash-recovery window", "taskAllowedTransitions"} {
		if !strings.Contains(body, marker) {
			t.Errorf("%s lost %q — this guard is scanning a page that no longer carries the claim it repairs", page, marker)
		}
	}
}
