package agent

import (
	"fmt"
	"strings"
	"testing"

	"github.com/nick/gopi/internal/toolnames"
)

// memoryToolDescription returns the memory tool's description from the SSOT
// slice (claudeToolDefs — Rule 3). Fails the test when the tool is missing, so
// a rename cannot silently turn the assertions below into vacuous passes.
func memoryToolDescription(t *testing.T) string {
	t.Helper()
	for _, d := range claudeToolDefs {
		if d.Name == toolnames.ToolMemory {
			return d.Description
		}
	}
	t.Fatalf("claudeToolDefs carries no %q tool — the store-timing contract has no surface", toolnames.ToolMemory)
	return ""
}

// TestEventDrivenStoreTriggers_SurviveInBothSurfaces guards the epic's (A)
// deliverable: the store-timing contract must name the events a model can
// RECOGNIZE mid-run (user says 记住 / teaches a mechanism or pitfall / a
// recurring pitfall gets solved), not only "at task end". It asserts the
// shared constant AND both renderings, because a contract that lives in one
// surface only is a contract half the LLM's reading surfaces never see.
//
// The two surfaces are deliberate: both are OUTSIDE the system-prompt density
// gate (taskStartSearchMessage is harness-injected, the tool description is
// schema-side), so the event text costs zero prompt budget — the routing block
// is already at its 4500-char ceiling (measured 4495/4500).
func TestEventDrivenStoreTriggers_SurviveInBothSurfaces(t *testing.T) {
	for _, want := range []string{
		"记住",
		"remember this",
		"teaches you a mechanism, rule or pitfall",
		"recurring pitfall gets solved",
		"memory(action=store) THAT round",
		"上下文随时可能重置",
		"未记录=丢失",
		"task end is the floor, not the ceiling",
	} {
		if !strings.Contains(eventDrivenStoreTriggers, want) {
			t.Errorf("eventDrivenStoreTriggers missing %q", want)
		}
	}

	surfaces := map[string]string{
		"taskStartSearchMessage":  taskStartSearchMessage,
		"memory tool description": memoryToolDescription(t),
	}
	for name, text := range surfaces {
		if !strings.Contains(text, eventDrivenStoreTriggers) {
			t.Errorf("%s does not carry eventDrivenStoreTriggers verbatim — the store triggers were edited out of this surface", name)
		}
		if !strings.Contains(text, "EVENT-DRIVEN") {
			t.Errorf("%s missing the EVENT-DRIVEN framing word", name)
		}
	}
}

// TestStoreTrigger_IsAnEventNotATaskEnd guards the specific regression this
// epic measured: the surfaces said "store at task end" (a deadline the model
// answers by holding the lesson), and the mid-run events were unnamed. The
// floor-not-ceiling sentence is the anti-pattern's own counter-example and
// must not be compressible away.
func TestStoreTrigger_IsAnEventNotATaskEnd(t *testing.T) {
	for _, s := range []struct {
		name string
		text string
	}{
		{"taskStartSearchMessage", taskStartSearchMessage},
		{"memory tool description", memoryToolDescription(t)},
	} {
		if !strings.Contains(s.text, "floor, not the ceiling") {
			t.Errorf("%s missing the store-is-a-floor-not-a-ceiling sentence", s.name)
		}
		if !strings.Contains(s.text, "THAT round") {
			t.Errorf("%s missing the THAT-round immediacy anchor", s.name)
		}
	}
}

// TestTrajectoryStats_CountMemoryCallsByAction is the epic's (B) mechanism at
// the unit level: the run-end summary must report the session's memory calls
// split by action (search / store / feedback_or_update), so a run whose
// store-side timing contract never fired is VISIBLE as store=0 instead of
// leaving the operator to re-parse the JSONL by hand.
func TestTrajectoryStats_CountMemoryCallsByAction(t *testing.T) {
	path := writeTrajectoryFixture(t, []string{
		fmt.Sprintf(`{"type":"session_start","timestamp":%q}`, ts(0)),
		fmt.Sprintf(`{"type":"user","message":{"role":"user","content":"task"},"timestamp":%q}`, ts(1)),
		// Two searches in the SAME round: two CALLS, one ROUND — the pair of
		// counters must report the difference rather than one proxy of the other.
		memToolUse("c1", toolnames.ToolMemoryActionSearch, ""),
		memToolUse("c4", toolnames.ToolMemoryActionSearch, ""),
		memToolUse("c2", toolnames.ToolMemoryActionStore, `,"topic":"t"`),
		memToolUse("c3", toolnames.ToolMemoryActionFeedback, `,"id":7,"rating":"useful"`),
		fmt.Sprintf(`{"type":"request","iteration":0,"completion_tokens":42,"timestamp":%q}`, ts(10)),
		fmt.Sprintf(`{"type":"tool_result","message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"c1","content":"#7 foo\n"}]},"timestamp":%q}`, ts(11)),
		fmt.Sprintf(`{"type":"request","iteration":1,"completion_tokens":10,"timestamp":%q}`, ts(20)),
		fmt.Sprintf(`{"type":"session_end","exit_code":0,"timestamp":%q}`, ts(30)),
	})

	stats, err := ParseTrajectoryStats(path)
	if err != nil {
		t.Fatalf("ParseTrajectoryStats: %v", err)
	}
	if stats.MemorySearchCalls != 2 {
		t.Errorf("MemorySearchCalls = %d, want 2", stats.MemorySearchCalls)
	}
	if stats.MemoryStoreCalls != 1 {
		t.Errorf("MemoryStoreCalls = %d, want 1", stats.MemoryStoreCalls)
	}
	if stats.MemoryFeedbackCalls != 1 {
		t.Errorf("MemoryFeedbackCalls = %d, want 1", stats.MemoryFeedbackCalls)
	}
	// Two searches inside ONE round are two CALLS on one ROUND — the two
	// counters answer different questions and must not be conflated.
	if stats.MemorySearchRounds != 1 {
		t.Errorf("MemorySearchRounds = %d, want 1 (both searches landed in round 0)", stats.MemorySearchRounds)
	}
	if out := stats.Format(); !strings.Contains(out, "memory store calls: search=2 store=1 feedback_or_update=1") {
		t.Errorf("Format() must carry the store-call line verbatim, got:\n%s", out)
	}
}

// TestTrajectoryStats_ZeroStoreIsVisible is the honest-zero half — the exact
// measurement this epic reports about itself: a session that searched and
// never stored must print store=0. A summary that omitted the line when the
// count is zero would hide the defect it exists to expose.
func TestTrajectoryStats_ZeroStoreIsVisible(t *testing.T) {
	path := writeTrajectoryFixture(t, []string{
		fmt.Sprintf(`{"type":"session_start","timestamp":%q}`, ts(0)),
		memToolUse("c1", toolnames.ToolMemoryActionSearch, ""),
		fmt.Sprintf(`{"type":"request","iteration":0,"completion_tokens":5,"timestamp":%q}`, ts(10)),
		fmt.Sprintf(`{"type":"session_end","exit_code":0,"timestamp":%q}`, ts(30)),
	})

	stats, err := ParseTrajectoryStats(path)
	if err != nil {
		t.Fatalf("ParseTrajectoryStats: %v", err)
	}
	if stats.MemoryStoreCalls != 0 {
		t.Fatalf("MemoryStoreCalls = %d, want 0", stats.MemoryStoreCalls)
	}
	if out := stats.Format(); !strings.Contains(out, "memory store calls: search=1 store=0 feedback_or_update=0") {
		t.Errorf("a zero-store run must still print the store line, got:\n%s", out)
	}
}

// TestTrajectoryStats_NonMemoryCallsAreNotCounted pins the boundary: only
// memory tool_use blocks fold into the memory counters, so a bash-heavy run
// (this repo's ordinary case) never inflates them.
func TestTrajectoryStats_NonMemoryCallsAreNotCounted(t *testing.T) {
	path := writeTrajectoryFixture(t, []string{
		fmt.Sprintf(`{"type":"session_start","timestamp":%q}`, ts(0)),
		fmt.Sprintf(`{"type":"assistant","message":{"role":"assistant","content":[{"type":"tool_use","id":"c1","name":"bash","input":{"command":"ls"}}]},"timestamp":%q}`, ts(2)),
		fmt.Sprintf(`{"type":"request","iteration":0,"completion_tokens":5,"timestamp":%q}`, ts(3)),
		fmt.Sprintf(`{"type":"session_end","exit_code":0,"timestamp":%q}`, ts(4)),
	})

	stats, err := ParseTrajectoryStats(path)
	if err != nil {
		t.Fatalf("ParseTrajectoryStats: %v", err)
	}
	if stats.MemorySearchCalls != 0 || stats.MemoryStoreCalls != 0 || stats.MemoryFeedbackCalls != 0 {
		t.Errorf("non-memory tools must not fold into the memory counters, got search=%d store=%d feedback=%d",
			stats.MemorySearchCalls, stats.MemoryStoreCalls, stats.MemoryFeedbackCalls)
	}
	if out := stats.Format(); !strings.Contains(out, "memory store calls: search=0 store=0 feedback_or_update=0") {
		t.Errorf("Format() must print the memory-call line for a run that never used the memory tool, got:\n%s", out)
	}
}
