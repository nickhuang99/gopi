package agent

import (
	"strings"
	"testing"

	"github.com/nickhuang99/gopi/internal/deepseek"
)

// This file proves the REPAIR half of the tool_use/tool_result pairing
// invariant at the source-message layer. It is deliberately separate from
// toolpair_invariant_test.go (which holds the assertions that RED on master
// without any new symbol, so the RED proof is a real behavioural failure and
// not a compile error). The symbols here (repairToolUsePairs /
// verifyToolUsePairs) exist only with the fix.

// TestRepairToolUsePairs_AgentSideSynthesizesMissingResult proves the repair
// half at the source layer: the missing half is supplied (never removed), the
// proof then passes, and the repaired array converts to a wire request the API
// accepts.
func TestRepairToolUsePairs_AgentSideSynthesizesMissingResult(t *testing.T) {
	msgs := poisonedHeadWindowDump()

	if _, orphan := findUnpairedSourceToolUse(msgs); !orphan {
		t.Fatal("test setup: expected an unpaired tool_use in the poisoned window")
	}
	msgs, ids := repairToolUsePairs(msgs)
	if len(ids) != 1 || ids[0] != "call_00_ET_TBCTnLz8k12TGe2ZEVxl2105" {
		t.Fatalf("repair ids = %v, want the dumped orphan", ids)
	}
	if err := verifyToolUsePairs(msgs); err != nil {
		t.Fatalf("verify after repair: %v", err)
	}
	req := deepseek.BuildClaudeRequest("m", msgs, "sys", "", claudeToolDefs, 4096, nil)
	assertClaudeWirePairing(t, "repaired wire", req)
}

// TestVerifyToolUsePairs_HealthyIsUntouched is the proof half's negative case:
// a sound array validates, and an intentionally orphaned one does not.
func TestVerifyToolUsePairs_HealthyIsUntouched(t *testing.T) {
	healthy := []deepseek.Message{
		{Role: "user", Content: "[EPIC GOAL]\nbuild X"},
		{Role: "assistant", ToolCalls: []deepseek.ToolCall{{ID: "c1", Name: "read"}}},
		{Role: "tool", Content: "file body", ToolCallID: "c1"},
		{Role: "assistant", Content: "done"},
	}
	if err := verifyToolUsePairs(healthy); err != nil {
		t.Fatalf("healthy array must validate: %v", err)
	}
	damaged := []deepseek.Message{
		{Role: "assistant", ToolCalls: []deepseek.ToolCall{{ID: "c1", Name: "read"}}},
		{Role: "tool", Content: "unrelated", ToolCallID: "c_other"},
	}
	if err := verifyToolUsePairs(damaged); err == nil {
		t.Fatal("damaged array must NOT validate — the proof is inert")
	} else if !strings.Contains(err.Error(), "c1") {
		t.Errorf("violation error must name the orphan id: %v", err)
	}
}
