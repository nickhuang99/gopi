package agent

import (
	"errors"
	"strings"
	"testing"

	"github.com/nickhuang99/gopi/internal/deepseek"
)

// toolDefFixture returns n distinct definitions shaped like the real ones: a
// name, a full-sentence description and a JSON-schema-ish map.
func toolDefFixture(n int) []deepseek.ClaudeToolDef {
	defs := make([]deepseek.ClaudeToolDef, 0, n)
	for i := 0; i < n; i++ {
		defs = append(defs, deepseek.ClaudeToolDef{
			Name:        "tool" + strings.Repeat("x", i+1),
			Description: "Read a file's contents as text, with a description long enough to matter on the wire.",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"path": map[string]interface{}{"type": "string", "description": "Path to the file to read."},
				},
			},
		})
	}
	return defs
}

// TestEstimatedTokensCountsTools is the regression arm the defect needed: with
// no measured prompt count (the resumed-session state — PromptTokens == 0), the
// estimate must INCLUDE the tool definitions, so it grows when they grow. Before
// the fix it was blind to them: the array is a top-level field of the request
// body and the server bills every byte of it.
func TestEstimatedTokensCountsTools(t *testing.T) {
	newCtx := func() *Context {
		return &Context{
			SystemPrompt: "you are gopi",
			Messages:     []deepseek.Message{{Role: "user", Content: strings.Repeat("hello ", 40)}},
		}
	}
	none := newCtx()
	base := none.EstimatedTokens()

	one := newCtx()
	one.SetTools(toolDefFixture(1))
	withOne := one.EstimatedTokens()
	if withOne <= base {
		t.Fatalf("EstimatedTokens must grow with the tool definitions: no tools=%d, one=%d", base, withOne)
	}

	ten := newCtx()
	ten.SetTools(toolDefFixture(10))
	withTen := ten.EstimatedTokens()
	if withTen <= withOne {
		t.Fatalf("EstimatedTokens must grow with MORE tool definitions: one=%d, ten=%d", withOne, withTen)
	}
	if got := ten.ToolsTokens(); got != withTen-base {
		t.Fatalf("ToolsTokens must be exactly the delta the estimate gained: ToolsTokens=%d, delta=%d", got, withTen-base)
	}
}

// TestEstimatedTokensDoesNotDoubleCountTools is the other half: once the API has
// measured the prompt, that count ALREADY includes the tools, so the estimate is
// the measurement plus the uncovered tail — adding the tools again would report a
// number larger than the one the server is about to charge.
func TestEstimatedTokensDoesNotDoubleCountTools(t *testing.T) {
	c := &Context{
		SystemPrompt:      "you are gopi",
		Messages:          []deepseek.Message{{Role: "user", Content: "hi"}},
		PromptTokens:      5000,
		PromptTokensIndex: 1,
	}
	c.SetTools(toolDefFixture(10))
	measured := c.EstimatedTokens()
	c2 := &Context{
		SystemPrompt:      "you are gopi",
		Messages:          []deepseek.Message{{Role: "user", Content: "hi"}},
		PromptTokens:      5000,
		PromptTokensIndex: 1,
	}
	if measured != c2.EstimatedTokens() {
		t.Fatalf("the measured branch must ignore the tools (they are inside the measurement): with=%d, without=%d",
			measured, c2.EstimatedTokens())
	}
}

// TestSetToolsNilKeepsTheRecordedDefs pins the compaction arm's contract: a
// tool-less request built elsewhere (compaction sends nil) must not erase what
// the main wire carries, or the next repaint would under-report again.
func TestSetToolsNilKeepsTheRecordedDefs(t *testing.T) {
	c := &Context{}
	c.SetTools(toolDefFixture(3))
	before := c.ToolsTokens()
	if before <= 0 {
		t.Fatalf("precondition: ToolsTokens must be positive after SetTools, got %d", before)
	}
	c.SetTools(nil)
	if after := c.ToolsTokens(); after != before {
		t.Fatalf("a nil SetTools must be a no-op, got %d → %d", before, after)
	}
}

// TestCalibratedTokensFallsBackToCharsPerToken drives the UNAVAILABLE branch: the
// tokenizer parses a 7.8 MB file on first use and reports an error when that
// fails, and the read-out must then print the chars/4 estimate rather than
// nothing. The seam is swapped rather than the data corrupted, so the fallback is
// a path a reader can see taken.
func TestCalibratedTokensFallsBackToCharsPerToken(t *testing.T) {
	restore := countTokensFn
	t.Cleanup(func() { countTokensFn = restore })
	countTokensFn = func(string) (int, error) { return -1, errors.New("tokenizer unavailable") }

	c := &Context{
		SystemPrompt: strings.Repeat("x", 400),
		Messages:     []deepseek.Message{{Role: "user", Content: strings.Repeat("y", 400)}},
	}
	c.SetTools(toolDefFixture(2))

	got := c.CalibratedTokens()
	want := heuristicTextTokens(c.SystemPrompt) + EstimateTokens(c.Messages[0]) + c.ToolsTokens()
	if got != want {
		t.Fatalf("fallback must equal the chars/4 estimate: got %d, want %d", got, want)
	}
	if got <= 0 {
		t.Fatalf("fallback must still report a positive size, got %d", got)
	}
}

// TestCalibratedTokensIsBPEExactWhenAvailable is the positive side of the same
// seam: with the real tokenizer, CJK text counts roughly 2.4x what chars/4 says,
// which is the under-estimate the operator saw on a Chinese conversation.
func TestCalibratedTokensIsBPEExactWhenAvailable(t *testing.T) {
	cjk := strings.Repeat("状态栏的读数偏低是因为漏算了工具定义", 10)
	c := &Context{SystemPrompt: cjk}
	got := c.CalibratedTokens()
	heuristic := heuristicTextTokens(cjk)
	if got <= heuristic {
		t.Fatalf("the BPE count of CJK text must exceed chars/4: bpe=%d heuristic=%d", got, heuristic)
	}
}

// TestCalibratedTokensPricesTheRecordedPrompt pins the chat wire's composition
// gap: the wire composes its system prompt per request (chatOpsSystemPrompt), so
// a reading that priced only Ctx.SystemPrompt would report the base prompt's size
// and miss the routing + chat-surface blocks — measured on a real body at 19k vs
// the server's 27,358. SetEstimatePrompt is estimate-only: the wire must keep
// rendering from SystemPrompt.
func TestCalibratedTokensPricesTheRecordedPrompt(t *testing.T) {
	c := &Context{SystemPrompt: "base"}
	base := c.CalibratedTokens()
	c.SetEstimatePrompt(strings.Repeat("routing ", 200))
	if got := c.CalibratedTokens(); got <= base {
		t.Fatalf("the recorded composed prompt must be priced: base=%d composed=%d", base, got)
	}
	if c.SystemPrompt != "base" {
		t.Fatalf("SetEstimatePrompt must not touch the wire's prompt, got %q", c.SystemPrompt)
	}
}

// TestCalibratedTokensCachesPerMessage pins the cost that makes this safe to call
// on every repaint and inside the trim loops: the second reading of an unchanged
// context does NO new tokenization. The counter is taken at the seam, so the
// claim is measured rather than argued.
func TestCalibratedTokensCachesPerMessage(t *testing.T) {
	calls := 0
	restore := countTokensFn
	t.Cleanup(func() { countTokensFn = restore })
	countTokensFn = func(s string) (int, error) {
		calls++
		return len(s) / charsPerToken, nil
	}

	c := &Context{SystemPrompt: "sys", ProjectContext: "proj"}
	for i := 0; i < 20; i++ {
		c.AddUserMessage(strings.Repeat("u", 100+i))
		c.AddAssistantMessage("a", nil)
	}
	c.SetTools(toolDefFixture(4))

	c.CalibratedTokens()
	first := calls
	if first == 0 {
		t.Fatalf("precondition: the first reading must tokenize something")
	}
	for i := 0; i < 5; i++ {
		c.CalibratedTokens()
	}
	if calls != first {
		t.Fatalf("an unchanged context must not re-tokenize: %d calls after the first reading, %d later", first, calls)
	}

	// A trimmed context keeps the surviving messages' costs (they are the same
	// messages) — only the dropped ones stop being counted.
	c.Messages = c.Messages[10:]
	c.CalibratedTokens()
	if calls != first {
		t.Fatalf("dropping the head must not re-tokenize the survivors: %d → %d calls", first, calls)
	}
}
