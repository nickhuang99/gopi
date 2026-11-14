package tools

// reframe_test.go — telemetry for the reframe+evaluate merge
// (SPEC-reframe-evaluate-merge.md §4, §7.7): every task(action=reframe) lands
// ONE append-only task_events row, and `task verdicts` reads back the verdict
// distribution plus the per-task reframe count. Two facts the operator can
// then observe: an honest exit really happens (the distribution is not 100%
// DOABLE), and a task reframed more than once — the bureaucracy loop's
// visible symptom — sorts to the top.

import (
	"context"
	"github.com/nickhuang99/gopi/internal/heavytest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nickhuang99/gopi/internal/taskverdict"
)

func TestTaskReframeRecordAndStats(t *testing.T) {
	heavytest.Require(t, heavytest.CriterionDiskFixture)
	t.Setenv("GOPI_MEMORY_DB", filepath.Join(t.TempDir(), "memory.db"))

	// Nothing recorded yet: the report says so rather than printing zeros.
	empty, err := TaskReframeStats()
	if err != nil {
		t.Fatalf("TaskReframeStats (empty): %v", err)
	}
	if !strings.Contains(empty, "no reframe records yet") {
		t.Fatalf("empty report must be explicit, got: %q", empty)
	}

	// Three tasks, four records: t-a reframed TWICE (the loop symptom), t-b
	// TOO_BIG with 3 roadmap items, t-c IMPOSSIBLE.
	if err := TaskReframeRecord("t-a", taskverdict.Doable, 0, ""); err != nil {
		t.Fatalf("record t-a doable: %v", err)
	}
	if err := TaskReframeRecord("t-a", taskverdict.TooBig, 3, ""); err != nil {
		t.Fatalf("record t-a too_big: %v", err)
	}
	if err := TaskReframeRecord("t-b", taskverdict.TooBig, 2, ""); err != nil {
		t.Fatalf("record t-b: %v", err)
	}
	if err := TaskReframeRecord("t-c", taskverdict.Impossible, 0, ""); err != nil {
		t.Fatalf("record t-c: %v", err)
	}

	out, err := TaskReframeStats()
	if err != nil {
		t.Fatalf("TaskReframeStats: %v", err)
	}
	for _, want := range []string{
		"4 reframe record(s) across 3 task(s)",
		taskverdict.Doable, taskverdict.TooBig, taskverdict.Impossible,
		"t-a ×2", // the repeat is visible
	} {
		if !strings.Contains(out, want) {
			t.Errorf("verdicts report missing %q:\n%s", want, out)
		}
	}
	// The ITEMS column sums the roadmap cardinality per verdict: TOO_BIG saw
	// 3 + 2 = 5 items (the new-work quantity the hard gates protect).
	for _, line := range strings.Split(out, "\n") {
		if !strings.HasPrefix(line, taskverdict.TooBig) {
			continue
		}
		if !strings.Contains(line, "5") {
			t.Errorf("TOO_BIG line must sum 5 roadmap items, got %q", line)
		}
	}
	// t-a (the repeated reframe) must sort above t-b/t-c.
	if strings.Index(out, "t-a") > strings.Index(out, "t-b") {
		t.Errorf("most-reframed task must sort first:\n%s", out)
	}
}

// TestTaskReframeRecordRejectsGarbage pins the never-silent contract: an
// unknown verdict or an empty task id is an ERROR the caller must record —
// telemetry must not invent a verdict or attribute a reframe to no task.
func TestTaskReframeRecordRejectsGarbage(t *testing.T) {
	t.Setenv("GOPI_MEMORY_DB", filepath.Join(t.TempDir(), "memory.db"))
	if err := TaskReframeRecord("t-x", "NEEDS_RESEARCH", 0, ""); err == nil {
		t.Fatal("a retired/unknown verdict must be rejected")
	}
	if err := TaskReframeRecord("", taskverdict.Doable, 0, ""); err == nil {
		t.Fatal("an empty task id must be rejected")
	}
}

// TestTaskReframeStatsSurfacesMalformedDetail: a malformed event detail is
// bucketed as UNKNOWN rather than dropped or attributed to a real verdict.
func TestTaskReframeStatsSurfacesMalformedDetail(t *testing.T) {
	heavytest.Require(t, heavytest.CriterionDiskFixture)
	t.Setenv("GOPI_MEMORY_DB", filepath.Join(t.TempDir(), "memory.db"))
	if err := TaskReframeRecord("t-a", taskverdict.Doable, 0, ""); err != nil {
		t.Fatalf("seed: %v", err)
	}
	ctxDB, err := openTasksDB(context.Background(), memoryDBPath())
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer ctxDB.Close()
	taskEventWrite(context.Background(), ctxDB, "t-b", ReframeEventName, "garbage detail")

	out, err := TaskReframeStats()
	if err != nil {
		t.Fatalf("TaskReframeStats: %v", err)
	}
	if !strings.Contains(out, reframeVerdictUnknown) {
		t.Errorf("malformed detail must be bucketed as %s, never dropped:\n%s", reframeVerdictUnknown, out)
	}
}

// TestTaskChildrenListsRoadmapChildren: the TOO_BIG auto-enqueue is readable
// back in roadmap order (a parent-linked, claimable child per item).
func TestTaskChildrenListsRoadmapChildren(t *testing.T) {
	heavytest.Require(t, heavytest.CriterionDiskFixture)
	t.Setenv("GOPI_MEMORY_DB", filepath.Join(t.TempDir(), "memory.db"))
	parent, err := TaskSubmit(TaskSubmitOptions{Summary: "parent epic", Source: "human"})
	if err != nil {
		t.Fatalf("parent submit: %v", err)
	}
	parentID := taskIDFromQueued(parent)
	if parentID == "" {
		t.Fatalf("no task id in submit output: %s", parent)
	}
	for _, s := range []string{"child one", "child two", "child three"} {
		if _, err := TaskSubmit(TaskSubmitOptions{
			Summary: s, Payload: "Goal:\n" + s + "\n\nAcceptance:\nverified",
			ParentTaskID: parentID, Source: "auto", Tags: ReframeRoadmapTag,
		}); err != nil {
			t.Fatalf("child submit %q: %v", s, err)
		}
	}
	children, err := TaskChildren(parentID)
	if err != nil {
		t.Fatalf("TaskChildren: %v", err)
	}
	if len(children) != 3 {
		t.Fatalf("expected 3 children, got %d", len(children))
	}
	for i, want := range []string{"child one", "child two", "child three"} {
		if children[i].Summary != want {
			t.Errorf("children[%d] = %q, want %q (roadmap order)", i, children[i].Summary, want)
		}
		if children[i].Status != TaskStatusPending {
			t.Errorf("children[%d] status = %q, want pending (claimable)", i, children[i].Status)
		}
	}
	if _, err := TaskChildren("  "); err == nil {
		t.Error("an empty parent id must be an error, not an unbounded query")
	}
}
