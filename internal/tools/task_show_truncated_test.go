package tools

// task_show_truncated_test.go — `gopi task show` must let an operator tell a
// truncated run from a finished one WITHOUT opening the session log, and must
// not invent the line on an ordinary row (a false truncation is as bad as a
// missing one: it would send a reader chasing a run that actually finished).
//
// The row is built through the REAL writers (TaskNew → TaskUpdateReason, the
// pair the agent's end hook calls), not by poking columns — the renderer is
// being tested against the ledger the close-out actually produces.

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/nickhuang99/gopi/internal/runend"
)

func TestTaskShowRendersTruncatedRun(t *testing.T) {
	t.Setenv("GOPI_MEMORY_DB", filepath.Join(t.TempDir(), "memory.db"))

	for _, r := range []runend.Reason{runend.WallclockCap, runend.StuckLoop, runend.MaxIterations} {
		id := "t-show-" + r.Token()
		if _, err := TaskNew("truncated run fixture "+r.Token(), id, "", ""); err != nil {
			t.Fatalf("TaskNew: %v", err)
		}
		// The close-out's two slots: the reason column carries the marker, and
		// the event detail is the result digest an operator reads.
		digest := " — elapsed=25.0s iter=2 tokens=17 | " + r.Detail()
		if _, err := TaskUpdateReason(id, TaskStatusFailed, r.ReasonText(), digest); err != nil {
			t.Fatalf("TaskUpdateReason: %v", err)
		}

		out, err := TaskShow(id)
		if err != nil {
			t.Fatalf("TaskShow: %v", err)
		}
		if !strings.Contains(out, "truncated:") {
			t.Fatalf("task show must render the truncation line for %q:\n%s", r, out)
		}
		if !strings.Contains(out, runend.TruncationMarker) || !strings.Contains(out, r.Token()) {
			t.Fatalf("the truncation line must name the marker and the token for %q:\n%s", r, out)
		}
		if !strings.Contains(out, runend.TruncationExplain) {
			t.Fatalf("the line must state the consequence, not just the fact, for %q:\n%s", r, out)
		}
		// The marker is on the result digest too, so a reader who greps the
		// ledger (not the render) is equally warned.
		if !strings.Contains(out, r.Detail()) {
			t.Fatalf("the result digest must carry the marker for %q:\n%s", r, out)
		}
	}
}

func TestTaskShowOrdinaryRowHasNoTruncationLine(t *testing.T) {
	t.Setenv("GOPI_MEMORY_DB", filepath.Join(t.TempDir(), "memory.db"))

	if _, err := TaskNew("ordinary finished run", "t-show-plain", "", ""); err != nil {
		t.Fatalf("TaskNew: %v", err)
	}
	if _, err := TaskUpdateReason("t-show-plain", TaskStatusCompleted, "", " — elapsed=12.5s iter=7 tokens=4321"); err != nil {
		t.Fatalf("TaskUpdateReason: %v", err)
	}
	out, err := TaskShow("t-show-plain")
	if err != nil {
		t.Fatalf("TaskShow: %v", err)
	}
	if strings.Contains(out, "truncated:") || strings.Contains(out, runend.TruncationMarker) {
		t.Fatalf("an ordinary finished run must carry no truncation:\n%s", out)
	}
	if !strings.Contains(out, "result:") || !strings.Contains(out, "— elapsed=12.5s iter=7 tokens=4321") {
		t.Fatalf("the ordinary result line changed shape:\n%s", out)
	}
}

// TestRunEndReasonForReadsTheTrailWhenTheReasonSlotIsClean: the wrapper's
// writeback reads the ledger AFTER the child exited, and a partial history is
// a real shape (the reason column can be cleared by a later transition while
// the append-only trail keeps the marker). Both doors must answer.
func TestRunEndReasonForReadsTheTrailWhenTheReasonSlotIsClean(t *testing.T) {
	t.Setenv("GOPI_MEMORY_DB", filepath.Join(t.TempDir(), "memory.db"))

	if _, err := TaskNew("trail-only truncation", "t-trail-only", "", ""); err != nil {
		t.Fatalf("TaskNew: %v", err)
	}
	// A close-out that wrote the digest and no reason.
	if _, err := TaskUpdateReason("t-trail-only", TaskStatusFailed, "", " — elapsed=3.0s | "+runend.StuckToolLoop.Detail()); err != nil {
		t.Fatalf("TaskUpdateReason: %v", err)
	}
	if got := RunEndReasonFor("t-trail-only"); got != runend.StuckToolLoop {
		t.Fatalf("RunEndReasonFor = %q, want %q (read from the trail)", got, runend.StuckToolLoop)
	}
	// …and the renderer agrees with the reader.
	out, err := TaskShow("t-trail-only")
	if err != nil {
		t.Fatalf("TaskShow: %v", err)
	}
	if !strings.Contains(out, runend.StuckToolLoop.Token()) {
		t.Fatalf("show must render the trail's truncation:\n%s", out)
	}
}
