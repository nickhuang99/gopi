package agent

import (
	"os"
	"strings"
	"testing"

	"github.com/nick/gopi/internal/deepseek"
	"github.com/nick/gopi/internal/toolnames"
)

// THE REAL SHAPE (2026-10-31, row 01a18f4a, killed at iteration 317). This is
// the SECOND occurrence of the head-orphan family, and it is NOT the shape the
// 2026-09-22 fix covered. Both facts are the point of this file.
//
// Messages 0 and 1 below are the first two messages of the death dump's
// request, VERBATIM in structure and identity:
//
//	/home/user/.gopi/sessions/01a18f4a-6e0c-7a80-9c50-f72d60003262/
//	  01a18f4a-6e0c-7a80-9c50-f72d60003262.dump.json
//
//	[0] assistant  thinking + tool_use  id=call_00_xAMlopO2DVTawtVAbfYU7604  name=bash
//	[1] user       tool_result          tool_use_id=call_00_xAMlopO2DVTawtVAbfYU7604
//
// and the run's own output.log states the invariant that broke:
//
//	[gopi] 🧾 task commit: epic briefing at index 0 dropped — it carried a
//	       tool_use whose result lay inside the trimmed region ...
//	Error: tool_use/tool_result pairing invariant violated by the outbound
//	       request (iteration 317): deepseek: tool_use
//	       "call_00_xAMlopO2DVTawtVAbfYU7604" at messages.0 has no tool_result
//	       in the immediately following message
//
// THE DISTINCTION THIS FILE PINS — the 2026-09-22 fixture cannot see it:
//
//	covered then: the head carries a tool_use that NOTHING answers
//	              (poisonedHeadWindowDump: its result id is `call_other`) —
//	              trimCommittedTaskAreaHead reports it as unanswered and drops
//	              the head. GREEN, and it stayed green.
//	missed here:  the head carries a tool_use that ITS OWN NEXT MESSAGE
//	              answers, i.e. the answer exists and lies INSIDE the dropped
//	              window [1, ci). The old predicate asked "is it answered
//	              anywhere in [1, ci)?" and read YES as SAFE TO KEEP — while
//	              [1, ci) is precisely the region the trim is about to delete.
//	              The answer being there is the reason the head MUST go, and
//	              the predicate treated it as the reason it may stay.
//
// Hence the same HTTP 400 one layer up, at the same index, five weeks later.

// realShapeHeadWindow rebuilds the window the live run trimmed: the real head
// pair (assistant tool_use + its tool_result), a body of committed work, then
// the commit round the hook fires on.
//
// The head pair is deliberate: it is what an ordinary compaction leaves behind
// (a summary block was prepended and later evicted, or the sticky-head trim cut
// the previous round at an assistant boundary), and it is the one shape the
// previous regression test does not build.
func realShapeHeadWindow() []deepseek.Message {
	return []deepseek.Message{
		// [0] the real death-dump head: an assistant tool_use …
		{Role: "assistant", ToolCalls: []deepseek.ToolCall{{
			ID:        "call_00_xAMlopO2DVTawtVAbfYU7604",
			Name:      toolnames.ToolBash,
			Arguments: map[string]interface{}{"command": "echo '=== verify every line number the report cites ==='"},
		}}},
		// [1] … answered by its OWN next message, inside the window the trim
		// deletes. This single line is the whole defect.
		{Role: "tool", ToolCallID: "call_00_xAMlopO2DVTawtVAbfYU7604",
			Content: "[t+~12m] [tool_use id: call_00_xAMlopO2DVTawtVAbfYU7604]\nexit=0\n=== verify every line number the report cites ===\nReclaimSelfReleasedMarker @ task_reclaim_self_release.go:NNN"},
		// [2..3] committed analysis work (the region the trim exists to drop).
		{Role: "assistant", Content: "analyzing item 3",
			ToolCalls: []deepseek.ToolCall{{ID: "call-analysis-3", Name: toolnames.ToolCodeSearch, Arguments: map[string]interface{}{"symbol": "ReclaimSelfReleasedMarker"}}}},
		{Role: "tool", ToolCallID: "call-analysis-3", Content: "defined at task_reclaim_self_release.go"},
		// [4..5] the commit round the hook fires on (its result must survive).
		{Role: "assistant", Content: "committing item 3",
			ToolCalls: []deepseek.ToolCall{{ID: "call-commit-3", Name: toolnames.ToolTask, Arguments: map[string]interface{}{
				"action": toolnames.ToolTaskCommit, "task_id": "3", "status": "completed",
				"conclusion": "item3: verified every cited line",
			}}}},
		{Role: "tool", ToolCallID: "call-commit-3", Content: "committed subtask \"3\""},
	}
}

// TestTrimCommittedTaskArea_RealShapeHeadAnsweredInsideDroppedWindow is the
// regression test for the 2026-10-31 death.
//
// RED before the fix — and RED for the REAL reason, i.e. the error the live run
// died of, produced by the SAME three seams it went through:
//
//	trimCommittedTaskArea → BuildClaudeRequest → deepseek.ValidateToolUsePairs
//
// GREEN after: the head whose answer lies in the dropped window is dropped with
// it, and the wire request satisfies the API's rule.
func TestTrimCommittedTaskArea_RealShapeHeadAnsweredInsideDroppedWindow(t *testing.T) {
	ag := newTaskAreaTestAgent(t, "sess-real-shape-head")
	for _, m := range realShapeHeadWindow() {
		ag.Ctx.AddMessage(m)
	}

	ag.trimCommittedTaskArea()

	// The trim must still have happened: the commit round and its result stay.
	var sawCommit, sawCommitResult bool
	for _, m := range ag.Ctx.Messages {
		if strings.Contains(m.Content, "committing item 3") {
			sawCommit = true
		}
		if strings.Contains(m.Content, "committed subtask") {
			sawCommitResult = true
		}
	}
	if !sawCommit || !sawCommitResult {
		t.Fatalf("commit round/results dropped — the trim was supposed to keep them: %+v", ag.Ctx.Messages)
	}

	// THE ASSERTION THE LIVE RUN FAILED: the built request must pass the
	// fail-closed gate. Its error text is quoted verbatim in the failure
	// message so a RED run reports the same sentence the run died with.
	req := deepseek.BuildClaudeRequest("deepseek-v4-flash", ag.Ctx.Messages, "sys", "", claudeToolDefs, 4096, nil)
	if err := deepseek.ValidateToolUsePairs(req); err != nil {
		t.Fatalf("built request violates the pairing invariant (this is the live death, verbatim): %v", err)
	}
	// The source array must be sound too — the same request is what gets
	// checkpointed to the raw log, and a damaged checkpoint kills every resume.
	if err := verifyToolUsePairs(ag.Ctx.Messages); err != nil {
		t.Fatalf("source array violates the pairing invariant after the trim (would be persisted as the resume checkpoint): %v", err)
	}
}

// TestTrimCommittedTaskArea_HealthyHeadWithoutToolUseStays pins the OTHER half:
// a head that carries no tool_use has nothing to answer, so it must still be
// KEPT exactly as before (the epic-briefing path every existing test covers).
// A fix that simply always dropped index 0 would pass the test above and
// silently take the epic briefing away from every run.
func TestTrimCommittedTaskArea_HealthyHeadWithoutToolUseStays(t *testing.T) {
	ag := newTaskAreaTestAgent(t, "sess-healthy-head-kept")
	ag.Ctx.AddMessage(deepseek.Message{Role: "user", Content: "[EPIC GOAL] solve five items"})
	ag.Ctx.AddAssistantMessage("analyzing item 1", []deepseek.ToolCall{
		{ID: "call-analysis-1", Name: toolnames.ToolCodeSearch, Arguments: map[string]interface{}{"symbol": "X"}},
	})
	ag.Ctx.AddToolResult("call-analysis-1", "some symbol found")
	ag.Ctx.AddAssistantMessage("committing item 1", []deepseek.ToolCall{
		{ID: "call-commit-1", Name: toolnames.ToolTask, Arguments: map[string]interface{}{
			"action": toolnames.ToolTaskCommit, "task_id": "1", "status": "completed", "conclusion": "item1: resolved",
		}},
	})
	ag.Ctx.AddToolResult("call-commit-1", "committed subtask 1")

	ag.trimCommittedTaskArea()

	if len(ag.Ctx.Messages) == 0 || !strings.Contains(ag.Ctx.Messages[0].Content, "EPIC GOAL") {
		t.Fatalf("healthy head (no tool_use) must be kept, got %+v", ag.Ctx.Messages)
	}
	if err := verifyToolUsePairs(ag.Ctx.Messages); err != nil {
		t.Fatalf("healthy window broke the pairing invariant: %v", err)
	}
}

// realRawLogEnv names the env var that points this test at the REAL raw.log of
// the run that died. It is a test-only input, never a production knob: without
// it the test SKIPS and says so, so a machine without the artifact reports
// "not measured" instead of a green it did not earn.
const realRawLogEnv = "GOPI_REAL_RAW_LOG"

// TestResumeThenCommitOfTheRealDeadRunIsPaired is the END-TO-END evidence, on
// real bytes: it takes the checkpoint the dead run actually persisted, recovers
// it through the ONE production door a resume uses (LoadCheckpoint), seeds that
// recovered history into a live Agent, appends the commit round that fires the
// hook, and then runs the trim → wire-build → validate chain the live run died
// in.
//
// WHY THIS IS THE TEST THAT MATTERS: the recovered head of THIS raw log is an
// assistant carrying one tool_use (see the RESUME-PATH evidence line in the
// task report) — i.e. resuming this run lands the process on exactly the shape
// that killed it. A fix that only makes the trim safe for a synthetic fixture
// would leave the next resume of this artifact dying one commit later.
func TestResumeThenCommitOfTheRealDeadRunIsPaired(t *testing.T) {
	rawLog := os.Getenv(realRawLogEnv)
	if rawLog == "" {
		t.Skipf("%s not set — this test measures the REAL 01a18f4a raw.log and has nothing to measure without it", realRawLogEnv)
	}
	if _, statErr := os.Stat(rawLog); statErr != nil {
		t.Skipf("real raw log absent (%v) — the artifact lives on the host that ran the task", statErr)
	}

	req, history, _, err := LoadCheckpoint(rawLog)
	if err != nil {
		t.Fatalf("LoadCheckpoint(%s): %v", rawLog, err)
	}
	// The recovered history must already be sound — this is the resume half of
	// the invariant, on the real artifact.
	if err := verifyToolUsePairs(history); err != nil {
		t.Fatalf("recovered history violates the pairing invariant: %v", err)
	}
	t.Logf("recovered %d message(s) from %s; head role=%q carrying %d tool_use(s)",
		len(history), rawLog, history[0].Role, len(history[0].ToolCalls))

	ag := newTaskAreaTestAgent(t, "sess-real-resume-then-commit")
	ag.Ctx.Messages = append([]deepseek.Message{}, history...)
	// The commit round that fires the trim hook on the NEXT round — the same
	// event that killed the live run (its 317th iteration followed a commit).
	ag.Ctx.AddAssistantMessage("committing after the resume", []deepseek.ToolCall{{
		ID: "call-resume-commit", Name: toolnames.ToolTask, Arguments: map[string]interface{}{
			"action": toolnames.ToolTaskCommit, "task_id": "9", "status": "completed",
			"conclusion": "resumed and committed",
		},
	}})
	ag.Ctx.AddToolResult("call-resume-commit", "committed subtask 9")

	ag.trimCommittedTaskArea()

	built := deepseek.BuildClaudeRequest(req.Model, ag.Ctx.Messages, "sys", "", claudeToolDefs, 4096, nil)
	if err := deepseek.ValidateToolUsePairs(built); err != nil {
		t.Fatalf("after resume+commit the built request violates the pairing invariant (the live death, on real bytes): %v", err)
	}
	if err := verifyToolUsePairs(ag.Ctx.Messages); err != nil {
		t.Fatalf("source array over the real resumed history violates the invariant: %v", err)
	}
	t.Logf("post-trim context: %d message(s), head role=%q — validated", len(ag.Ctx.Messages), ag.Ctx.Messages[0].Role)
}
