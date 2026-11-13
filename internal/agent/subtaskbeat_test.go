package agent

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nick/gopi/internal/toolnames"
)

// The subtask-beat pointer (brief: briefings/brief-memory-prefetch-at-subtask-beats.md).
//
// The commit beat is where the model decides what to do next, so the commit
// RETURN carries one short PROSE line naming the next incomplete item — a
// pointer at the retrieval the model may want, never a payload. No query runs,
// no results are injected, no new tool, no goroutine: the line is a pure
// function of the task list the harness already tracks.
//
// These tests are the acceptance for the two behavioural halves (pointer
// present exactly when a next incomplete item exists; nothing extra when the
// committed item was the last), the preserved return shape when the budget
// line is empty, the no-retrieval proof, and the schema-clause diff guard.

// pointerCommitAgent returns an agent whose commit path works (fresh GOPI_HOME
// + ledger) and whose tracked task list is the given one.
func pointerCommitAgent(t *testing.T, tasks []TaskItem) *Agent {
	t.Helper()
	a := newAutoTaskAgent(t)
	if tasks != nil {
		ts := newTodoState()
		ts.update(tasks)
		a.TaskState = ts
	}
	return a
}

func commitArgs(id string) map[string]interface{} {
	return map[string]interface{}{
		"task_id":    id,
		"status":     "completed",
		"conclusion": "pointer beat check",
	}
}

// TestCommitTaskItemPointsAtNextIncompleteItem: with a next incomplete item on
// the list, the commit return names it (id + title) and names the retrieval
// that is one call away. RED on the pre-change tree: the return carried only
// the budget line.
func TestCommitTaskItemPointsAtNextIncompleteItem(t *testing.T) {
	t.Setenv(sessionBudgetEnv, "")
	a := pointerCommitAgent(t, []TaskItem{
		{TaskID: "1", Summary: "land the pointer helper", Status: TaskStatusProgressing},
		{TaskID: "2", Summary: "extend the memory schema description", Status: TaskStatusPending},
		{TaskID: "3", Summary: "write the report", Status: TaskStatusCompleted},
	})

	got := a.commitTaskItem(commitArgs("1"))

	if !strings.Contains(got, "write_task_area: committed subtask \"1\"") {
		t.Fatalf("commit receipt missing from the return — the pointer must decorate it, not replace it: %q", got)
	}
	if !strings.Contains(got, "Next item 2") {
		t.Fatalf("commit return must name the NEXT incomplete item (2), got %q", got)
	}
	if !strings.Contains(got, "extend the memory schema description") {
		t.Fatalf("commit return must carry the next item's title, got %q", got)
	}
	if !strings.Contains(got, "memory(action="+toolnames.ToolMemoryActionSearch+")") {
		t.Fatalf("the pointer must name the retrieval call it points at, got %q", got)
	}
	if strings.Contains(got, "Next item 3") {
		t.Fatalf("a COMPLETED item is not a next beat — the walk must skip it, got %q", got)
	}
	// Still prose: no record shape, no injected hits, single line.
	if strings.Contains(got, "tier=") || strings.Contains(got, "\n") {
		t.Fatalf("the pointer must be one line of prose, never a payload: %q", got)
	}
}

// TestCommitTaskItemPointerAbsentWhenCommitWasLast: the last item's commit has
// no next beat, so nothing extra is appended. This is the case that rots.
func TestCommitTaskItemPointerAbsentWhenCommitWasLast(t *testing.T) {
	t.Setenv(sessionBudgetEnv, "")
	a := pointerCommitAgent(t, []TaskItem{
		{TaskID: "1", Summary: "land the pointer helper", Status: TaskStatusCompleted},
		{TaskID: "2", Summary: "extend the memory schema description", Status: TaskStatusCompleted},
	})

	got := a.commitTaskItem(commitArgs("2"))
	if strings.Contains(got, "Next item") {
		t.Fatalf("no next incomplete item exists — the return must carry nothing extra, got %q", got)
	}
	if !strings.Contains(got, "write_task_area: committed subtask \"2\"") {
		t.Fatalf("commit receipt missing: %q", got)
	}

	// No task list tracked at all (the state was never established): same
	// silent degrade — never a fabricated topic.
	a = pointerCommitAgent(t, nil)
	if got := a.commitTaskItem(commitArgs("1")); strings.Contains(got, "Next item") {
		t.Fatalf("no tracked list — the pointer must stay silent, got %q", got)
	}
}

// TestCommitTaskItemShapeUnchangedWithoutBudgetLine: budget 0 (unlimited)
// renders no budget line, and the return's shape must stay exactly as it was —
// receipt plus, when a next item exists, the pointer, separated by one space.
func TestCommitTaskItemShapeUnchangedWithoutBudgetLine(t *testing.T) {
	t.Setenv(sessionBudgetEnv, "0")

	// (a) no next item: the return is the untouched receipt, nothing appended.
	a := pointerCommitAgent(t, []TaskItem{{TaskID: "1", Summary: "only item", Status: TaskStatusCompleted}})
	bare := a.commitTaskItem(commitArgs("1"))
	if strings.Contains(bare, "session budget:") {
		t.Fatalf("unlimited budget must not decorate the return, got %q", bare)
	}
	if bare != strings.TrimSpace(bare) || strings.Contains(bare, "  ") {
		t.Fatalf("return shape drifted (padding/whitespace): %q", bare)
	}
	if !strings.HasSuffix(bare, "as authoritative context.") {
		t.Fatalf("the write-area receipt must be intact when nothing is appended, got %q", bare)
	}

	// (b) a next item exists: exactly one space, then the pointer.
	a = pointerCommitAgent(t, []TaskItem{
		{TaskID: "1", Summary: "first", Status: TaskStatusCompleted},
		{TaskID: "2", Summary: "second", Status: TaskStatusPending},
	})
	got := a.commitTaskItem(commitArgs("1"))
	if strings.Contains(got, "session budget:") {
		t.Fatalf("unlimited budget must not decorate the return, got %q", got)
	}
	if !strings.HasPrefix(got, "write_task_area: committed subtask \"1\"") {
		t.Fatalf("the receipt must still lead the return, got %q", got)
	}
	if strings.Contains(got, "  ") || strings.Contains(got, " .") {
		t.Fatalf("pointer must be joined by a single space: %q", got)
	}
	if !strings.HasSuffix(got, "memory(action="+toolnames.ToolMemoryActionSearch+") is free and one call away.") {
		t.Fatalf("the pointer must close the return, got %q", got)
	}
}

// TestNextIncompletePointerIsPurelyStateDerived is the no-retrieval proof at
// the callable seam: the pointer is produced by a ZERO-VALUE Agent — no
// session logger, no clock, no context, no database configured at all. A path
// that reads the memory database cannot run under that agent.
//
// The runtime half is paired with a source-level half (the next test): a
// retrieval could only enter through a package call, and the helper's body
// carries none.
func TestNextIncompletePointerIsPurelyStateDerived(t *testing.T) {
	a := &Agent{}
	ts := newTodoState()
	ts.update([]TaskItem{
		{TaskID: "1", Summary: "done", Status: TaskStatusCompleted},
		{TaskID: "2", Summary: "  next\nstep   one  ", Status: TaskStatusProgressing},
	})
	a.TaskState = ts

	got := a.nextIncompletePointer("1")
	if !strings.Contains(got, "Next item 2") {
		t.Fatalf("zero-value agent must still render the pointer (it needs no DB), got %q", got)
	}
	if !strings.Contains(got, "next step one") {
		t.Fatalf("title must be whitespace-collapsed to one line, got %q", got)
	}
	if strings.Contains(got, "\n") {
		t.Fatalf("pointer must be one line, got %q", got)
	}
	// The committed item itself is never "next", and an all-completed list
	// renders nothing.
	ts.update([]TaskItem{{TaskID: "1", Summary: "done", Status: TaskStatusCompleted}})
	if got := a.nextIncompletePointer("1"); got != "" {
		t.Fatalf("all-completed list must render nothing, got %q", got)
	}
}

// TestNextIncompletePointerBodyTouchesNoDatabase pins the source shape: the
// helper that builds the pointer may reference nothing but the tracked state.
// This is the mechanical half of the "no retrieval on the pointer path"
// acceptance — a runtime assertion alone cannot rule out a swallowed read.
func TestNextIncompletePointerBodyTouchesNoDatabase(t *testing.T) {
	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatalf("glob: %v", err)
	}
	fset := token.NewFileSet()
	found := false
	for _, f := range files {
		if strings.HasSuffix(f, "_test.go") {
			continue
		}
		src, err := os.ReadFile(f)
		if err != nil {
			t.Fatalf("read %s: %v", f, err)
		}
		af, err := parser.ParseFile(fset, f, src, 0)
		if err != nil {
			t.Fatalf("parse %s: %v", f, err)
		}
		for _, d := range af.Decls {
			fd, ok := d.(*ast.FuncDecl)
			if !ok || fd.Name == nil || fd.Name.Name != nextIncompletePointerFuncName {
				continue
			}
			found = true
			body := string(src[fset.Position(fd.Pos()).Offset:fset.Position(fd.End()).Offset])
			for _, banned := range []string{"tools.", "sql.", "MemorySearch", "openSearchDB", "memoryDBPath", "db."} {
				if strings.Contains(body, banned) {
					t.Errorf("%s touches %q — the pointer path must read nothing but the tracked task state",
						nextIncompletePointerFuncName, banned)
				}
			}
			// The pointer must go through the tracked-state accessor, not a
			// fresh list of its own.
			if !strings.Contains(body, "TaskState") {
				t.Errorf("%s does not consult TaskState at all: %s", nextIncompletePointerFuncName, body)
			}
		}
	}
	if !found {
		t.Fatalf("%s not found in any production file of this package — the no-retrieval guard is blind",
			nextIncompletePointerFuncName)
	}
}

// TestMemorySearchDescriptionAppendsTheCommitBeatClause is the schema diff
// guard: the three original triggers survive BYTE-IDENTICAL inside one
// appended clause, and the fourth beat is named in the same imperative voice.
func TestMemorySearchDescriptionAppendsTheCommitBeatClause(t *testing.T) {
	mem := toolDefByName(t, toolnames.ToolMemory)
	if mem == nil {
		t.Fatal("memory missing from claudeToolDefs")
	}
	desc := mem.Description

	const originalTriggers = "USE FIRST at the start of every task, when stuck, or when the task looks done before — a known recipe beats re-deriving it (5 分钟 vs 重造轮子)."
	if n := strings.Count(desc, originalTriggers); n != 1 {
		t.Fatalf("the original trigger sentence must survive byte-identical exactly once (found %d) — one clause is APPENDED, never a rewrite", n)
	}
	for _, trig := range []string{"at the start of every task", "when stuck", "when the task looks done before"} {
		if !strings.Contains(desc, trig) {
			t.Errorf("original trigger %q was lost — append only, never rewrite", trig)
		}
	}
	// One appended clause, naming the commit beat the way the harness spells it.
	for _, want := range []string{
		"right after you commit a subtask (task(action=commit))",
		"the next subtask is about to start",
	} {
		if !strings.Contains(desc, want) {
			t.Errorf("memory search description missing the appended commit-beat clause %q", want)
		}
	}
	// Append-only: the clause sits AFTER the original trigger sentence, in the
	// same imperative voice, and never replaces it.
	if strings.Index(desc, "right after you commit a subtask") < strings.Index(desc, originalTriggers) {
		t.Errorf("the commit-beat clause must be APPENDED after the original triggers, not spliced in front of them")
	}
	if strings.Count(desc, "task(action=commit)") != 1 {
		t.Errorf("the commit beat must be named exactly once in the search clause, got %d",
			strings.Count(desc, "task(action=commit)"))
	}
}

// TestNextIncompletePointerTruncatesAndFallsBack keeps the line bounded (it
// rides every commit return) and never fabricates a topic from a titleless
// item, where it names the beat by id alone.
func TestNextIncompletePointerTruncatesAndFallsBack(t *testing.T) {
	a := &Agent{}
	ts := newTodoState()
	a.TaskState = ts

	long := strings.Repeat("topic ", 40)
	ts.update([]TaskItem{{TaskID: "9", Summary: long, Status: TaskStatusPending}})
	got := a.nextIncompletePointer("")
	if !strings.Contains(got, "Next item 9") || !strings.Contains(got, "…") {
		t.Fatalf("long titles must be truncated with an ellipsis, got %q", got)
	}
	if n := len([]rune(got)); n > 240 {
		t.Fatalf("pointer is %d runes — it rides every commit return and must stay short", n)
	}

	ts.update([]TaskItem{{TaskID: "9", Summary: "   ", Status: TaskStatusPending}})
	got = a.nextIncompletePointer("")
	if !strings.Contains(got, "Next item 9") {
		t.Fatalf("a titleless item still has a next beat — name it by id, got %q", got)
	}
	if strings.Contains(got, `""`) {
		t.Fatalf("no fabricated empty topic in the pointer, got %q", got)
	}

	// Alt-name fallback (LLMs emit id/content) must resolve like ParseTaskList does.
	ts.update([]TaskItem{{AltID: "7", AltSummary: "alt shaped item", Status: TaskStatusPending}})
	if got := a.nextIncompletePointer(""); !strings.Contains(got, "Next item 7") || !strings.Contains(got, "alt shaped item") {
		t.Fatalf("alt field names must resolve, got %q", got)
	}
}
