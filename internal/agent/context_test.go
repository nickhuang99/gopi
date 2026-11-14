package agent

import (
	"fmt"
	"strings"
	"testing"

	"github.com/nickhuang99/gopi/internal/deepseek"
)

func TestEstimateTokens(t *testing.T) {
	// Empty content: role + fixed overhead, always >= 1 token.
	base := EstimateTokens(deepseek.Message{Role: "user", Content: ""})
	if base < 1 {
		t.Fatalf("expected at least 1 token for empty message, got %d", base)
	}

	// chars/4 heuristic including role + fixed overhead:
	// (100 content + 4 role + 2 overhead)/4 + 1 = 27.
	msg := deepseek.Message{Role: "user", Content: strings.Repeat("a", 100)}
	want := (100+len("user")+2)/4 + 1
	if got := EstimateTokens(msg); got != want {
		t.Fatalf("expected %d tokens for 100 chars, got %d", want, got)
	}

	// Tool calls contribute their name, ID and serialized arguments.
	plain := EstimateTokens(deepseek.Message{Role: "assistant", Content: ""})
	withTool := EstimateTokens(deepseek.Message{
		Role:    "assistant",
		Content: "",
		ToolCalls: []deepseek.ToolCall{
			{Name: "read", ID: "call_1", Arguments: map[string]interface{}{"path": "/tmp/x"}},
		},
	})
	if withTool <= plain {
		t.Fatalf("tool-call message (%d) should estimate more than plain message (%d)", withTool, plain)
	}
}

func TestContextEstimatedTokensIncludesSystemPrompt(t *testing.T) {
	c := &Context{SystemPrompt: strings.Repeat("s", 400)}
	c.AddUserMessage(strings.Repeat("u", 400))
	// System prompt alone would estimate 400/4+1 = 101; total must exceed it.
	if got := c.EstimatedTokens(); got <= 101 {
		t.Fatalf("estimated tokens %d should include the system prompt", got)
	}
}

func TestTrimToFit_NoTrimWhenUnderBudget(t *testing.T) {
	c := &Context{}
	for i := 0; i < 5; i++ {
		c.AddUserMessage("short")
	}
	removed := c.TrimToFit(10000)
	if removed != 0 {
		t.Fatalf("expected no trimming under budget, removed=%d", removed)
	}
	if len(c.Messages) != 5 {
		t.Fatalf("expected 5 messages preserved, got %d", len(c.Messages))
	}
}

func TestTrimToFit_RemovesOldestKeepsNewest(t *testing.T) {
	c := &Context{}
	for i := 0; i < 20; i++ {
		c.AddUserMessage(strings.Repeat(fmt.Sprintf("message-%02d-", i), 100)) // ~1200 chars ≈ 300 tokens each
	}

	removed := c.TrimToFit(1000)
	if removed == 0 {
		t.Fatal("expected trimming to occur")
	}
	if len(c.Messages) >= 20 {
		t.Fatalf("expected fewer messages after trim, got %d", len(c.Messages))
	}
	if got := c.EstimatedTokens(); got > 1000 {
		t.Fatalf("estimated tokens %d still exceed budget 1000 after trim", got)
	}
	// The newest message must survive; the oldest must be gone.
	last := c.Messages[len(c.Messages)-1]
	if !strings.Contains(last.Content, "message-19") {
		t.Fatalf("expected newest message to survive, got %q", last.Content)
	}
	if strings.Contains(c.Messages[0].Content, "message-00") {
		t.Fatalf("expected oldest message to be trimmed, got %q", c.Messages[0].Content)
	}
}

func TestTrimToFit_NeverLeavesOrphanToolHead(t *testing.T) {
	c := &Context{}
	c.AddUserMessage(strings.Repeat("u", 2000)) // big user msg
	c.AddAssistantMessage("", []deepseek.ToolCall{
		{Name: "bash", Arguments: map[string]interface{}{"command": strings.Repeat("c", 1000)}},
	})
	c.AddToolResult("call_1", strings.Repeat("r", 1000)) // tool result follows the assistant call
	c.AddUserMessage(strings.Repeat("final", 50))

	// Total ≈ 1076 estimated tokens; budget 500 forces the trim past the
	// assistant tool-call message, which must take its tool result with it.
	removed := c.TrimToFit(500)
	if removed == 0 {
		t.Fatal("expected trimming to occur")
	}
	if len(c.Messages) == 0 {
		t.Fatal("expected at least one message to survive")
	}
	for i, m := range c.Messages {
		if m.Role == "tool" {
			t.Fatalf("trim left orphaned tool message at index %d: %+v", i, m)
		}
	}
	if got := c.EstimatedTokens(); got > 500 {
		t.Fatalf("estimated tokens %d still exceed budget 500 after trim", got)
	}
	// The most recent user message must still be there.
	last := c.Messages[len(c.Messages)-1]
	if last.Role != "user" || !strings.Contains(last.Content, "final") {
		t.Fatalf("expected newest user message to survive, got %+v", last)
	}
}

func TestTrimToFit_EmptyAndSingleMessage(t *testing.T) {
	c := &Context{}
	if removed := c.TrimToFit(1); removed != 0 {
		t.Fatalf("empty context should not trim, removed=%d", removed)
	}
	c.AddUserMessage(strings.Repeat("x", 1000))
	if removed := c.TrimToFit(10); removed != 0 {
		t.Fatalf("single message should be preserved, removed=%d", removed)
	}
	if len(c.Messages) != 1 {
		t.Fatalf("expected single message to survive, got %d", len(c.Messages))
	}
}

func TestGetMessages_ProjectContextInjection(t *testing.T) {
	c := &Context{
		SystemPrompt:   "kernel",
		ProjectContext: "gopi-md-content",
	}
	c.AddUserMessage("user-msg")

	msgs := c.GetMessages()
	if len(msgs) != 3 {
		t.Fatalf("expected 3 messages (system + project + user), got %d", len(msgs))
	}
	if msgs[0].Role != "system" || msgs[0].Content != "kernel" {
		t.Fatalf("msg[0]: expected system 'kernel', got role=%q content=%q", msgs[0].Role, msgs[0].Content)
	}
	if msgs[1].Role != "system" || msgs[1].Content != "gopi-md-content" {
		t.Fatalf("msg[1]: expected system 'gopi-md-content', got role=%q content=%q", msgs[1].Role, msgs[1].Content)
	}
	if msgs[2].Role != "user" || msgs[2].Content != "user-msg" {
		t.Fatalf("msg[2]: expected user 'user-msg', got role=%q content=%q", msgs[2].Role, msgs[2].Content)
	}
}

func TestGetMessages_NoProjectContext(t *testing.T) {
	c := &Context{SystemPrompt: "kernel"}
	c.AddUserMessage("user-msg")

	msgs := c.GetMessages()
	if len(msgs) != 2 {
		t.Fatalf("expected 2 messages (system + user), got %d", len(msgs))
	}
	if msgs[0].Content != "kernel" || msgs[1].Content != "user-msg" {
		t.Fatalf("msg order wrong: [0]=%q [1]=%q", msgs[0].Content, msgs[1].Content)
	}
}

func TestGetMessages_NoSystemPromptWithProject(t *testing.T) {
	c := &Context{ProjectContext: "gopi-only"}
	c.AddUserMessage("user-msg")

	msgs := c.GetMessages()
	if len(msgs) != 2 {
		t.Fatalf("expected 2 messages (project + user), got %d", len(msgs))
	}
	if msgs[0].Content != "gopi-only" || msgs[1].Content != "user-msg" {
		t.Fatalf("msg order wrong: [0]=%q [1]=%q", msgs[0].Content, msgs[1].Content)
	}
}

func TestEstimatedTokens_IncludesProjectContext(t *testing.T) {
	c := &Context{SystemPrompt: "kernel", ProjectContext: strings.Repeat("p", 400)}
	c.AddUserMessage(strings.Repeat("u", 400))
	got := c.EstimatedTokens()
	if got < 200 {
		t.Fatalf("estimated tokens %d should include project context (expected >= 200)", got)
	}
}

// TestSummarizeOldToolResults_FoldsOnlyOldLargeResults verifies the R1
// folding policy: tool results older than maxVerbatimToolResults with more
// than maxToolResultLines collapse into one-line stubs; the newest results
// and short results stay verbatim; message count and order never change.
func TestSummarizeOldToolResults_FoldsOnlyOldLargeResults(t *testing.T) {
	c := &Context{}
	for i := 0; i < 10; i++ {
		c.AddAssistantMessage(fmt.Sprintf("call %d", i), nil)
		lines := strings.Repeat("line\n", 50) // 51 lines — foldable when old
		if i%2 == 1 {
			lines = "short\n" // 2 lines — never folded
		}
		c.AddToolResult(fmt.Sprintf("call_%d", i), lines)
	}
	orig := len(c.Messages)

	folded := c.summarizeOldToolResults()

	// 10 tool results; the newest maxVerbatimToolResults (8) stay verbatim
	// (i=9..i=2). The 2 oldest (i=1, i=0) are folding candidates: i=1 is
	// short, i=0 is long → exactly 1 folded.
	if folded != 1 {
		t.Fatalf("expected 1 folded, got %d", folded)
	}
	if len(c.Messages) != orig {
		t.Fatalf("folding must not change message count: %d → %d", orig, len(c.Messages))
	}
	// Newest results — even long ones — stay verbatim.
	if !strings.HasPrefix(c.Messages[len(c.Messages)-1].Content, "short") {
		t.Fatalf("newest tool result must stay verbatim, got %q", c.Messages[len(c.Messages)-1].Content)
	}
	// The oldest long result became a stub carrying the line count and the
	// first line (for a real bash result that first line is the exit status).
	first := c.Messages[1] // index 0 = assistant "call 0", index 1 = its tool result
	if first.Role != "tool" || !strings.HasPrefix(first.Content, "[tool result summarized") {
		t.Fatalf("expected stub at oldest long result, got %+v", first)
	}
	if !strings.Contains(first.Content, "was 51 lines") || !strings.Contains(first.Content, "line") {
		t.Fatalf("stub missing line count / first line: %q", first.Content)
	}
}

func TestNewAgent_DefaultContextWindow(t *testing.T) {
	// NewAgent requires DeepSeek env credentials; use the field default
	// documented on the type instead of invoking network setup.
	if DefaultContextWindow != 200000 {
		t.Fatalf("expected DefaultContextWindow 200000 (deployment window), got %d", DefaultContextWindow)
	}
	if DefaultCompactTriggerRatio <= 0 || DefaultCompactTriggerRatio >= 1 {
		t.Fatalf("DefaultCompactTriggerRatio must be in (0,1), got %f", DefaultCompactTriggerRatio)
	}
}
