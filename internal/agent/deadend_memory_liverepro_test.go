package agent

import (
	"strings"
	"testing"

	"github.com/nickhuang99/gopi/internal/deepseek"
	"github.com/nickhuang99/gopi/internal/toolnames"
)

// Live-repro regression: the exact prior round that misfired (2026-08-29).
// Round results = [memory_search records (prose contains 'failed'/'[stderr]'/'not found'), bash exit=0 wc].
// The old detector flagged it as a dead end and nudged the model to trim its
// productive memory_search. The fixed detector must NOT flag it.
func TestDeadEnd_NoFalsePositiveOnRealMemoryRound(t *testing.T) {
	memoryResult := `#1022 gopi code review 2026-08-28 buddy cross-review findings tier=warm conf=0.85
Q: gopi code review 二审发现
A: Code review ... parseProxyArgs ` + "`unknown flag`" + ` double-dashes ... ` + "[stderr]" + ` ... not found ... failed ... Exception ... scanSSEEvents swallows buffered-path I/O errors ...
#876 gopi memory_feedback 采纳率低 ... failed ... root cause=采纳率低非功能故障`
	everythingCheck := "#896 gopi context_trim ... failed ... #904 rewrite ... [stderr]"
	for _, r := range []string{memoryResult, everythingCheck} {
		tc := []deepseek.ToolCall{{Name: toolnames.ToolMemory, Arguments: map[string]interface{}{"action": toolnames.ToolMemoryActionSearch}}}
		if got := resultsHaveDeadEnd(tc, []string{r}); got {
			t.Errorf("memory_search result with failure words in prose flagged as dead end: %q", r)
		}
	}
	// genuine memory tool failure still flagged
	tc := []deepseek.ToolCall{{Name: toolnames.ToolMemory, Arguments: map[string]interface{}{"action": toolnames.ToolMemoryActionSearch}}}
	if !resultsHaveDeadEnd(tc, []string{"memory error: unparseable memory engine output: xyz"}) {
		t.Errorf("real memory tool error must be flagged")
	}
	// the round ALSO had a clean bash result (exit=0) → must never dead-end
	batch := []deepseek.ToolCall{{Name: toolnames.ToolMemory, Arguments: map[string]interface{}{"action": toolnames.ToolMemoryActionSearch}}, {Name: "bash"}}
	if resultsHaveDeadEnd(batch, []string{memoryResult, "exit=0\n150 /tmp/thinking.txt"}) {
		t.Errorf("real prior round (memory prose + clean bash) must not dead-end")
	}
	_ = strings.Contains
}
