package tools

import (
	"fmt"
	"strings"
	"testing"
	"time"
)

// statusword_test.go — the two halves of the split that keeps a `status:` field
// READABLE while it carries measurements (statusword.go).
//
// THE BUG THESE GUARD (measured, master e6fee1f / 974bc5c): `task show` renders
// `status: <word> + holder dead (pid N gone)`, and cmd/agent's send-gate tests
// compared the WHOLE field to a status value, so a row whose holder pid the rig
// had already reaped read as a different status. The fix splits the word off at
// StatusWordMarkerSep. The split is CORRECT only while two facts hold, and each
// fact is a table below rather than a comment:
//
//	① no status VALUE may contain the separator — otherwise trimming at the
//	  first occurrence would cut a real word in half;
//	② every marker the renderer emits must BEGIN with it — otherwise a marker
//	  would be glued onto the word and ①'s split would never fire.
func TestStatusWordMarkerSepIsAbsentFromEveryStatusValue(t *testing.T) {
	if len(taskStatusSet) == 0 {
		t.Fatal("taskStatusSet is empty — a scan over nothing is not a pass")
	}
	for _, s := range taskStatusSet {
		if s == "" {
			t.Errorf("taskStatusSet carries an empty status value")
			continue
		}
		if strings.Contains(s, StatusWordMarkerSep) {
			t.Errorf("status value %q CONTAINS the marker separator %q: a reader "+
				"trimming at the first occurrence would cut the word in half "+
				"(statusword.go)", s, StatusWordMarkerSep)
		}
	}
	// A VACUUM GUARD: the set must be large enough for the loop above to mean
	// something — measured 9 statuses on this rev.
	if len(taskStatusSet) < 5 {
		t.Fatalf("taskStatusSet holds %d values — too few to judge the separator", len(taskStatusSet))
	}
}

func TestEveryHolderMarkerBeginsWithTheSeparator(t *testing.T) {
	// The renderer's own formats, rendered with a probe pid so the check reads
	// what a caller actually receives rather than the format string.
	const probePid = 4242
	renderers := []struct {
		name string
		fmt  string
		word string // the marker text after the separator; "" = not checked
		want []string
	}{
		{
			name: "gone",
			fmt:  taskHolderGoneMarkerFmt,
			want: []string{StatusWordMarkerSep, "gone", "4242"},
		},
		{
			name: "recycled",
			fmt:  taskHolderRecycledMarkerFmt,
			want: []string{StatusWordMarkerSep, "recycled", "4242"},
		},
	}
	for _, r := range renderers {
		t.Run(r.name, func(t *testing.T) {
			got := fmt.Sprintf(r.fmt, probePid)
			if !strings.HasPrefix(got, StatusWordMarkerSep) {
				t.Fatalf("marker %q does not BEGIN with the separator %q: a "+
					"`status:` reader would read it as part of the status word "+
					"(statusword.go)", got, StatusWordMarkerSep)
			}
			// The separator is ONE occurrence, not several: a doubled separator
			// would make the first trim leave a leading space behind.
			if strings.Count(got, StatusWordMarkerSep) != 1 {
				t.Fatalf("marker %q carries the separator %d times, want exactly 1 "+
					"(the leading boundary)", got, strings.Count(got, StatusWordMarkerSep))
			}
			for _, want := range r.want {
				if !strings.Contains(got, want) {
					t.Errorf("marker %q does not carry %q", got, want)
				}
			}
		})
	}
}

// TestTheHolderMarkerOnlyEverDecoratesInProgress pins the ONE structural fact
// that keeps the RAW readers of a `status:` field honest.
//
// THE GAP THIS CLOSES. The split above (statusWord / StatusWordMarkerSep) is
// applied by the readers that were measured red. Other readers compare the
// WHOLE field — `got != tools.TaskStatusPending`, `tk.Status != "pending"`,
// a `strings.Contains(row, "status: failed")` scan — and they are correct only
// because the field carries a marker for NO status except in_progress
// (taskholder_gone.go's two entry points both open with exactly that test).
// Nothing pinned it, so widening the marker to, say, a stale `claimed` row
// would silently redden readers that have nothing to do with holder liveness,
// and the failure would name the wrong subsystem. This test turns that into a
// failure that names THIS invariant.
//
// THE FIXTURE IS PROVEN LIVE FIRST. A row that carries a marker is asserted to
// carry one while it is in_progress; without that control the loop below would
// pass against a fixture whose proof never fires, and a guard over nothing is
// not a pass (Rule 15).
func TestTheHolderMarkerOnlyEverDecoratesInProgress(t *testing.T) {
	deadPid := taskStateTestDeadPid(t)

	row := &Task{
		TaskID: "t-marker-status-axis", Status: taskStatusInProgress,
		ClaimedBy: taskStateTestHolder(deadPid), HasClaim: true,
		ClaimedAt:       float64(time.Now().Add(-time.Hour).Unix()),
		HasInProgressAt: true, InProgressAt: float64(time.Now().Add(-time.Hour).Unix()),
		HasProcIdentity: true, ProcPid: deadPid, ProcStartTicks: 1,
	}

	// CONTROL: this fixture really does produce the marker, on BOTH emit points.
	want := fmt.Sprintf(taskHolderGoneMarkerFmt, deadPid)
	if got := taskHolderGoneMarker(row, taskStateUnknown); got != want {
		t.Fatalf("CONTROL FAILED: the fixture renders no marker on the list "+
			"(got %q, want %q) — a status-axis check over a dead fixture proves "+
			"nothing", got, want)
	}
	if got := taskHolderGoneStatusMarker(row); got != want {
		t.Fatalf("CONTROL FAILED: the fixture renders no marker on `task show` "+
			"(got %q, want %q) — the status-axis check would be vacuous", got, want)
	}

	// THE INVARIANT: every OTHER status renders no marker at all — not the
	// separator, not the wording. Both emit points are checked, because the
	// list view and `task show` reach the marker through different functions.
	checked := 0
	for _, status := range taskStatusSet {
		if status == taskStatusInProgress {
			continue
		}
		row.Status = status
		// The list surface, over every state label it can be handed.
		for _, state := range []string{taskStateUnknown, taskStateDied, taskStateRunning} {
			if got := taskHolderGoneMarker(row, state); got != "" {
				t.Errorf("status %q renders the holder marker %q on the LIST (state "+
					"%q): a raw `status:` reader comparing the whole field would "+
					"measure holder liveness as though it were the status "+
					"(%s)", status, got, state, statuswordRawReadSites)
			}
		}
		// The `task show` surface.
		if got := taskHolderGoneStatusMarker(row); got != "" {
			t.Errorf("status %q renders the holder marker %q on `task show`: a raw "+
				"`status:` reader comparing the whole field would measure holder "+
				"liveness as though it were the status (%s)",
				status, got, statuswordRawReadSites)
		}
		checked++
	}

	// VACUUM GUARD: the loop must have judged a real population.
	if checked < 5 {
		t.Fatalf("the loop judged %d non-in_progress statuses — too few to judge "+
			"the marker's status axis (taskStatusSet holds %d values)",
			checked, len(taskStatusSet))
	}
}

// statuswordRawReadSites names the readers the invariant above protects, so a
// future failure says WHICH comparisons would start measuring the wrong thing
// instead of sending the reader to the holder-gone code. Enumerated from the
// tree (grep '"status:"' --include=*_test.go); a reader that strips the marker
// is NOT listed here, because the split already makes it immune.
//
// THE LIST WAS RE-DERIVED, because the previous one named four files that no
// longer exist. Its head was cmd/agent's `syncSurfaceField` comparisons
// against tools.TaskStatusPending in cli_tasks_send_chain_test.go,
// cli_tasks_pull_test.go, cli_tasks_send_dependency_test.go and
// cli_tasks_send_cancel_test.go — all four went with the cross-node sync
// family (commit 5677cc6ef, `remove(task): 第 1 批 —— 删掉 cmd/agent 的 8 个
// 跨节点 sync CLI 入口`), and `syncSurfaceField` has no definition left in the
// tree either. A named reader that cannot be found is a note a future failure
// reads as current, which is why the population below was measured, not
// inherited.
const statuswordRawReadSites = "this invariant is what keeps the raw readers " +
	"correct: internal/agent/task_tools_test.go's toolsTaskByID, the " +
	"`strings.Contains(row, \"status: failed\")` scans in cmd/agent " +
	"(cli_serve_test.go, serve_worker_split_test.go, singlenode_serve_test.go), " +
	"cmd/agent/cli_tasks_statusword_test.go's rawStatusField (which reads the " +
	"raw line ON PURPOSE, to prove the marker is there before the split is " +
	"asserted), and cmd/agent/cli_tasks_reparent_cli_test.go's status: presence " +
	"check"
