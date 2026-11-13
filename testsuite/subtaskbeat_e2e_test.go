// Package testsuite — the subtask-beat pointer, observed end to end.
//
// The unit tests in internal/agent prove the pointer is BUILT correctly; this
// one proves it REACHES THE MODEL, which is a different claim (Rule 31: a
// build that compiles and a suite that passes are not evidence a beat fires).
// It drives the real binary against a scripted mock LLM and reads the pointer
// text back out of the session JSONL — the wire log, not an in-process value.
package testsuite

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/nick/gopi/internal/testutil"
	"github.com/nick/gopi/internal/toolnames"
)

// subtaskBeatTaskList is the list the mock model emits in its FIRST answer:
// item 1 committed, item 2 still pending — the exact shape the commit beat is
// for. The block is parsed into the harness's tracked task state, which is
// what the commit-return pointer reads.
const subtaskBeatTaskList = "[TASK_LIST_BEGIN]\n" +
	"[\n" +
	`  {"task_id":"1","summary":"land the pointer helper","status":"completed"},` + "\n" +
	`  {"task_id":"2","summary":"wire the schema clause","status":"pending"}` + "\n" +
	"]\n" +
	"[TASK_LIST_END]\n"

// taskBeatFinalAnswer closes the run (all items completed, substantive text —
// the R3 first-end_turn trust condition).
const taskBeatFinalAnswer = "[TASK_LIST_BEGIN]\n" +
	"[\n" +
	`  {"task_id":"1","summary":"land the pointer helper","status":"completed"},` + "\n" +
	`  {"task_id":"2","summary":"wire the schema clause","status":"completed"}` + "\n" +
	"]\n" +
	"[TASK_LIST_END]\n" +
	"Beat probe complete: the commit carried the pointer and the run finished."

// taskBeatClosingAnswer is the answer AFTER the forced task-end memory_store —
// deliberately different text from taskBeatFinalAnswer, because a byte-identical
// repeated no-tool round trips the stuck detector.
const taskBeatClosingAnswer = "Closing the beat probe: the pointer was recorded in the session log and the store is committed."

// TestSubtaskBeatPointerReachesTheModelE2E is the Rule 31 end-to-end
// observation: the REAL binary drives the REAL loop, the harness's own commit
// path decorates the tool result, and the pointer text is read back out of the
// session JSONL — not asserted from a unit call.
func TestSubtaskBeatPointerReachesTheModelE2E(t *testing.T) {
	home := t.TempDir()
	work := t.TempDir()

	llm := testutil.NewMockLLM(t,
		// Round 1: establish the tracked list (no tool call — a plan round).
		testutil.Resp{Text: subtaskBeatTaskList + "Established the list for the beat probe."},
		// Round 2: the commit beat.
		testutil.Resp{ToolUse: &testutil.ToolCall{
			ID:   "call_commit_1",
			Name: toolnames.ToolTask,
			Args: map[string]interface{}{
				"action":     toolnames.ToolTaskCommit,
				"task_id":    "1",
				"status":     "completed",
				"conclusion": "the pointer helper landed",
			},
		}},
		// Round 3: finish (the mock repeats this for any further round).
		testutil.Resp{Text: taskBeatFinalAnswer},
		// Round 4: the harness's forced task-end memory_store (completion
		// checkpoint) — the run owns an all-completed list here.
		testutil.Resp{ToolUse: &testutil.ToolCall{
			ID:   "call_store_1",
			Name: toolnames.ToolMemory,
			Args: map[string]interface{}{
				"action":   toolnames.ToolMemoryActionStore,
				"topic":    "subtask beat pointer",
				"question": "does the commit return carry the pointer?",
				"answer":   "yes — observed in the session JSONL",
			},
		}},
		// Round 5: the real closing answer, distinct text (an identical
		// repeated round trips the stuck detector).
		testutil.Resp{Text: taskBeatClosingAnswer},
	)
	defer llm.Close()

	_, stderr, code := testutil.RunGopi(t, nil, "probe the subtask beat\n",
		testutil.MockEnv(t, home, llm.URL()), work)
	if code != 0 {
		t.Fatalf("gopi exit = %d, want 0 (stderr: %s)", code, stderr)
	}

	sess := testutil.LoadSession(t, home)
	var commitResult string
	for _, ev := range sess.Events {
		if ev.Type != "tool_result" {
			continue
		}
		raw, err := json.Marshal(ev.Raw)
		if err != nil {
			continue
		}
		if strings.Contains(string(raw), "committed subtask") {
			commitResult = string(raw)
		}
	}
	if commitResult == "" {
		t.Fatalf("no commit tool_result in the session log — the beat never fired (session types: %v)", sess.Types())
	}
	// The pointer, in the real log, with the real next item.
	t.Logf("RECORDED COMMIT TOOL RESULT (session %s):\n%s", sess.Path, commitResult)
	if !strings.Contains(commitResult, "Next item 2") {
		t.Fatalf("the recorded commit result must carry the pointer to item 2, got %s", commitResult)
	}
	if !strings.Contains(commitResult, "wire the schema clause") {
		t.Fatalf("the pointer must carry the next item's title, got %s", commitResult)
	}
	if !strings.Contains(commitResult, "memory(action="+toolnames.ToolMemoryActionSearch+")") {
		t.Fatalf("the pointer must name the retrieval call, got %s", commitResult)
	}
	// The no-retrieval contract, observed end to end: the run issued NO
	// memory(action=search) call, so the pointer cannot have been a payload
	// riding an automatic search. (The one memory call in the script is the
	// task-end store the harness forces through its completion checkpoint —
	// a write, not a retrieval.)
	for _, tu := range sess.ToolUses() {
		if tu.Name != toolnames.ToolMemory {
			continue
		}
		if action, _ := tu.Input["action"].(string); action == toolnames.ToolMemoryActionSearch {
			t.Fatalf("the harness ran memory(action=search) off the commit beat — the pointer must be prose only, never a retrieval: %+v", tu)
		}
	}

	// Part 1 on the WIRE: the appended fourth trigger is not merely in a Go
	// string — it must reach the model inside the memory tool's schema, which
	// is the surface whose persuasion is the point of the clause. The wire
	// body of the first request is the ground truth for what the model saw.
	body := llm.RequestBody(1)
	if body == "" {
		t.Fatalf("no request body captured from the mock — cannot verify the schema reached the model")
	}
	if !strings.Contains(body, "right after you commit a subtask (task(action=commit))") {
		t.Fatalf("the memory tool schema on the wire does not carry the commit-beat clause — the appended trigger never reached the model")
	}
	if !strings.Contains(body, "USE FIRST at the start of every task, when stuck, or when the task looks done before") {
		t.Fatalf("the original three triggers are missing from the schema on the wire")
	}
}
