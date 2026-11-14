package tools

// task_nothing_to_deliver_test.go — the close-out DECISION, pinned against the
// row that produced it.
//
// The measured defect (row 01a186b9-064c, 2026-10-29): one close-out wrote
// `status: completed` on a ground it had just measured as "carries no commit to
// deliver", while the run's own report opened `**Verdict: BLOCKED**` and stated
// that nothing was deleted and nothing was committed. The evidence sat in the
// report the whole time and NOTHING READ IT.
//
// These arms drive the production door (NothingToDeliverOutcome) with a REAL
// report on disk, resolved through the report seam's own path — never a
// hand-joined "sessions/<id>/task-report.md" (the precedent: task_report_test.go,
// where a mirrored path derivation would pass even if production pointed
// elsewhere).

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nick/gopi/internal/taskreport"
)

// ntdSandbox points GOPI_HOME at a temp dir and writes taskID's report there,
// returning the resolved report path.
func ntdSandbox(t *testing.T, taskID, body string) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("GOPI_HOME", home)
	t.Setenv("GOPI_MEMORY_DB", filepath.Join(home, "memory.db"))
	path, err := TaskReportPath(taskID)
	if err != nil {
		t.Fatalf("TaskReportPath: %v", err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatalf("write report: %v", err)
	}
	return path
}

// ntdChannelReason is the clone channel's own sentence, verbatim from the row
// that closed anyway (the exit's composed string, Rule 14).
const ntdChannelReason = "clone channel: nothing to deliver — clone channel: /home/user/workspace/01a186b9-064c-76c8-9d02-d4f9177e4361 " +
	"carries no commit to deliver: `rev-list --count HEAD --not origin/master` answered 0 against origin/master, so there is nothing to push"

// TestNothingToDeliverClosesFailedWhenTheReportDeclaresIt is THE repro for this
// bug: a run whose own report says it did not finish must not read `completed`,
// and the reason must QUOTE both facts rather than paraphrase either.
func TestNothingToDeliverClosesFailedWhenTheReportDeclaresIt(t *testing.T) {
	const row = "01a186b9-0000-7000-8000-000000000001"
	ntdSandbox(t, row, "# Task Report: 删光 `gopi task reopen` —— 本轮裁决 BLOCKED（前置仍未落地）\n"+
		"\n"+
		"**Verdict: BLOCKED** — 两条由 briefing 写死的硬前置在 master 上仍双双未落地。\n"+
		"本轮没有删除任何代码、没有提交、没有移动 ref；工作树干净。\n"+
		"\n"+
		"## Completed\n"+
		"\n"+
		"1. 取证。\n")

	status, summary := NothingToDeliverOutcome(row, ntdChannelReason)

	if status != TaskStatusFailed {
		t.Fatalf("status = %q — a run whose own report declares it did not finish must not close %q: "+
			"the ledger would read as finished work while the report says the work was never done",
			status, TaskStatusCompleted)
	}
	if !strings.Contains(summary, "BLOCKED") {
		t.Fatalf("the reason must QUOTE the author's verdict verbatim — got %q", summary)
	}
	if !strings.Contains(summary, ntdChannelReason) {
		t.Fatalf("the reason must also carry the channel's own measurement verbatim — got %q", summary)
	}
}

// TestNothingToDeliverKeepsCompletedForACounterProof is the arm that keeps this
// repair from re-opening row 01a18244: a run ASKED to produce no change made no
// admission about finishing, so it keeps the `completed` close the channel's
// measurement supports.
func TestNothingToDeliverKeepsCompletedForACounterProof(t *testing.T) {
	const row = "01a18244-0000-7000-8000-000000000002"
	ntdSandbox(t, row, "# Task Report: count the zero rows\n\n**Verdict: GREEN AS FOUND** — nothing needed changing.\n")

	status, summary := NothingToDeliverOutcome(row, ntdChannelReason)

	if status != TaskStatusCompleted {
		t.Fatalf("status = %q — an items=0 counter-proof closes %q (row 01a18244 fixed the opposite defect)",
			status, TaskStatusCompleted)
	}
	if summary != ntdChannelReason {
		t.Fatalf("an unconditional completion carries the channel's sentence VERBATIM — got %q", summary)
	}
}

// TestNothingToDeliverWithNoReportKeepsCompleted: a run that wrote no report made
// no admission. Reading "no report" as "unfinished" would turn every silent run
// into a failure — the lenient direction, stated as a case.
func TestNothingToDeliverWithNoReportKeepsCompleted(t *testing.T) {
	const row = "01a18244-0000-7000-8000-000000000003"
	home := t.TempDir()
	t.Setenv("GOPI_HOME", home)
	t.Setenv("GOPI_MEMORY_DB", filepath.Join(home, "memory.db"))

	status, summary := NothingToDeliverOutcome(row, ntdChannelReason)

	if status != TaskStatusCompleted {
		t.Fatalf("status = %q, want %q — a run with no report made no admission and must NOT be failed (Rule 5)",
			status, TaskStatusCompleted)
	}
	// THE ASSERTION CHANGED ON PURPOSE (2026-11-07, row 01a1b48a-8746): this arm
	// used to require the channel's sentence VERBATIM, and that is exactly what made
	// an idle ground indistinguishable from a counter-proof. A run that wrote NO
	// report and contributed NO commit is the idlest shape there is, so it now
	// carries the marker — what must still hold is that the channel's measurement
	// rides along, unaltered, inside the composed reason.
	if !strings.Contains(summary, ntdChannelReason) {
		t.Fatalf("summary = %q, want the channel's sentence verbatim inside it", summary)
	}
	if !strings.Contains(summary, NoDeliverableMarker) {
		t.Fatalf("summary = %q — a ground with no commit and no report must be marked, not silent", summary)
	}
}

// TestNothingToDeliverNeverClosesWithAnEmptyReason pins the other half of Rule
// 15: the exit may return no sentence at all, and a terminal row must still
// carry a measurement rather than a blank.
func TestNothingToDeliverNeverClosesWithAnEmptyReason(t *testing.T) {
	const row = "01a18244-0000-7000-8000-000000000004"
	home := t.TempDir()
	t.Setenv("GOPI_HOME", home)
	t.Setenv("GOPI_MEMORY_DB", filepath.Join(home, "memory.db"))

	_, summary := NothingToDeliverOutcome(row, "   ")
	if strings.TrimSpace(summary) == "" {
		t.Fatal("a terminal row must never close with an empty reason")
	}
}

// TestNothingToDeliverMarksTheIdleCloseOut is THE repro for the second defect this
// file answers (row 01a1b48a-8746, measured 2026-11-07): a run that contributed no
// commit AND left only the harness skeleton closed `completed` with the channel's
// sentence and NOTHING ELSE — indistinguishable, in `task list` and `task show`,
// from a counter-proof run that was asked to produce nothing and wrote up why.
//
// The status must stay `completed` (Rule 5: no report is not a failure); what must
// change is that the row says so, in a token a person can grep.
func TestNothingToDeliverMarksTheIdleCloseOut(t *testing.T) {
	const row = "01a1b48a-0000-7000-8000-000000000001"
	// The skeleton is what the harness writes when the model authored nothing —
	// produced through the production writer, never hand-typed.
	ntdSandbox(t, row, taskreport.Skeleton("row "+row, nil))

	status, summary := NothingToDeliverOutcome(row, ntdChannelReason)

	if status != TaskStatusCompleted {
		t.Fatalf("status = %q — a run that wrote no admission must NOT be failed for it (Rule 5); "+
			"the difference has to be visible, not fatal", status)
	}
	if !strings.Contains(summary, NoDeliverableMarker) {
		t.Fatalf("summary carries no %s marker: %q — this row is then indistinguishable from a "+
			"counter-proof run in every reading of the ledger", NoDeliverableMarker, summary)
	}
	if !strings.Contains(summary, ntdChannelReason) {
		t.Fatalf("the marker must WRAP the channel's measurement, not replace it (Rule 14): %q", summary)
	}
}

// TestNothingToDeliverLeavesAnAuthoredReportUnmarked is the COUNTER-PROOF arm, and
// without it the marker above is indistinguishable from "mark every empty ground".
// Row 01a18244 fixed the defect of closing those runs `failed`; a run that wrote
// down what it checked and why it produced nothing delivered the thing it was
// asked for, and its row must stay QUIET.
func TestNothingToDeliverLeavesAnAuthoredReportUnmarked(t *testing.T) {
	const row = "01a18244-0000-7000-8000-000000000005"
	ntdSandbox(t, row, "# Task Report: counter-proof — the verb is already gone\n"+
		"\n"+
		"## Completed\n"+
		"- Ran `gopi task reopen -h` and `gopi task -h`; neither lists the verb.\n"+
		"- Nothing to change, so no commit was made — this is the measurement, not a gap.\n")

	status, summary := NothingToDeliverOutcome(row, ntdChannelReason)

	if status != TaskStatusCompleted {
		t.Fatalf("status = %q, want %q", status, TaskStatusCompleted)
	}
	if strings.Contains(summary, NoDeliverableMarker) {
		t.Fatalf("an authored report must NOT be marked idle — summary = %q", summary)
	}
	if summary != ntdChannelReason {
		t.Fatalf("an unmarked completion carries the channel's sentence verbatim: %q", summary)
	}
}
