package agent

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/nickhuang99/gopi/internal/deepseek"
)

// The measured defect (2026-09-30): `gopi chat --once --tools` died with HTTP
// 422 on the SECOND round of every multi-round turn —
//
//	{"model":"deepseek-flash","max_tokens":16384,"messages":null}
//	→ "Failed to deserialize the JSON body into the target type: messages:
//	   invalid type: null, expected a sequence"
//	(dump ~/.gopi/dumps/20260930-194502-1.dump.json)
//
// The null was produced locally, one call before the wire: Context.TrimToFit
// deleted the WHOLE message array. The shape it needed is a short multi-round
// chat whose history is exactly one tool round, while the bulk of the estimate
// is the system prompt + project context:

// trimToFitToolRound builds that shape: one completed tool round, with the
// measured prompt-token count recorded where a real round records it (after
// the assistant message, before its tool result lands — loop.go's
// SetPromptTokens call site).
func trimToFitToolRound(t *testing.T) *Context {
	t.Helper()
	c := &Context{
		SystemPrompt:   "kernel prompt",
		ProjectContext: "GOPI.md project context",
	}
	c.AddUserMessage("use the bash tool once")
	c.AddAssistantMessage("", []deepseek.ToolCall{{
		ID:        "call_1",
		Name:      "bash",
		Arguments: map[string]interface{}{"command": "head -c 30000 GOPI.md"},
	}})
	// The API measurement: it covers the two messages present at that moment.
	c.SetPromptTokens(31296)
	// The tool result lands AFTER the measurement (the round's own result).
	c.AddToolResult("call_1", "X")
	if len(c.Messages) != 3 {
		t.Fatalf("fixture: want 3 messages (user, assistant, tool), got %d", len(c.Messages))
	}
	return c
}

// TestTrimToFitNeverEmptiesMessages is the regression guard for the 422: a
// trim may leave the context over budget (TakeOldest's floors do the same
// deliberately), but it must NEVER leave it EMPTY — an empty array serializes
// as "messages":null and the provider rejects the request outright.
func TestTrimToFitNeverEmptiesMessages(t *testing.T) {
	c := trimToFitToolRound(t)

	removed := c.TrimToFit(15000)

	if len(c.Messages) == 0 {
		t.Fatalf("REGRESSION: TrimToFit emptied Messages (removed=%d) — the wire field would serialize as messages:null and the endpoint answers HTTP 422", removed)
	}
	if got := removed; got > 1 {
		t.Errorf("trim removed %d messages from a 3-message history that must keep its last round; the head run is dropped atomically, so at most 1 message can go", got)
	}
	t.Logf("kept %d message(s) (head=%s), removed=%d — the trim stopped above the floor instead of emptying the history",
		len(c.Messages), c.Messages[0].Role, removed)
}

// TestTrimToFitEmptyMessagesWouldSerializeAsNull pins the WIRE consequence of
// the empty array, so the guard above is anchored to the failure the operator
// actually saw rather than to an internal invariant. ClaudeRequest.Messages
// deliberately carries no `omitempty` (a request without messages is invalid,
// so the field must be visible) — which is exactly why an empty slice must
// never be reachable.
func TestTrimToFitEmptyMessagesWouldSerializeAsNull(t *testing.T) {
	c := trimToFitToolRound(t)
	c.TrimToFit(15000)

	req := deepseek.BuildClaudeRequest("deepseek-flash", c.WireMessages(false), c.SystemPrompt, c.ProjectContext, nil, 16384, nil)
	body, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if got := string(body); strings.Contains(got, `"messages":null`) {
		t.Fatalf("REGRESSION: the request serializes as %s — this is the exact body the endpoint rejected with HTTP 422", got)
	}
}

// TestTrimToFitInvalidatesStaleMeasurement guards the amplifier: the measured
// prompt-token count still covered the messages the trim REMOVED, so leaving
// it in place makes EstimatedTokens score the survivors against a count that
// included them — the trim then keeps deleting (the loop's budget test never
// comes true) until the array runs out. Compaction already resets the same
// pair for the same reason.
func TestTrimToFitInvalidatesStaleMeasurement(t *testing.T) {
	c := trimToFitToolRound(t)
	before := c.EstimatedTokens()

	if removed := c.TrimToFit(15000); removed == 0 {
		t.Fatal("fixture: nothing was trimmed, so the measurement cannot have been invalidated")
	}
	if c.PromptTokens != 0 || c.PromptTokensIndex != 0 {
		t.Fatalf("stale measurement survived a trim (PromptTokens=%d index=%d) — EstimatedTokens would report %d for an array that no longer contains those messages",
			c.PromptTokens, c.PromptTokensIndex, c.EstimatedTokens())
	}
	if after := c.EstimatedTokens(); after >= before {
		t.Errorf("estimate did not fall after trimming %d messages (%d → %d)", before-after, before, after)
	}
}

// TestTrimToFitKeepsToolResultWithItsAssistant — the head run is dropped in one
// step, so no iteration of the trim can end BETWEEN an assistant's tool_use and
// the tool_result answering it (an orphaned result at the head is invalid on
// its own, and it is what the old safety net then deleted).
func TestTrimToFitKeepsToolResultWithItsAssistant(t *testing.T) {
	c := &Context{}
	c.AddUserMessage("u1")
	c.AddAssistantMessage("a1", nil)
	c.AddUserMessage("u2")
	c.AddAssistantMessage("", []deepseek.ToolCall{{ID: "call_a", Name: "bash", Arguments: map[string]interface{}{"command": "x"}}})
	c.AddToolResult("call_a", "result-a")
	c.AddUserMessage("u3")
	c.AddAssistantMessage("", []deepseek.ToolCall{{ID: "call_b", Name: "bash", Arguments: map[string]interface{}{"command": "y"}}})
	c.AddToolResult("call_b", "result-b")

	c.TrimToFit(0) // 0 forces the trim as far as its floor allows

	if len(c.Messages) == 0 {
		t.Fatal("REGRESSION: trim emptied the history")
	}
	if c.Messages[0].Role == "tool" {
		t.Fatalf("REGRESSION: trim left an orphaned tool result at the head (%d messages kept)", len(c.Messages))
	}
	// Whatever survives must still be a well-formed history: a tool result is
	// only ever preceded by the assistant that called it.
	for i, m := range c.Messages {
		if m.Role == "tool" && (i == 0 || c.Messages[i-1].Role != "assistant") {
			t.Fatalf("REGRESSION: tool result at index %d is not preceded by its assistant", i)
		}
	}
	t.Logf("kept %d message(s): %v", len(c.Messages), rolesOf(c))
}

// rolesOf renders the surviving roles for a failure message.
func rolesOf(c *Context) []string {
	out := make([]string, 0, len(c.Messages))
	for _, m := range c.Messages {
		out = append(out, m.Role)
	}
	return out
}
