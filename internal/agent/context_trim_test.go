package agent

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/nick/gopi/internal/deepseek"
	"github.com/nick/gopi/internal/toolnames"
)

// ── Schema + description guards (描述即策略) ───────────────────────────────

// TestContextTrimToolSchema: call_id is required, replace_with is optional,
// and the description teaches the dead-end trim contract (干净工作台 + 留结论).
func TestContextTrimToolSchema(t *testing.T) {
	ct := toolDefByName(t, "context_trim")
	if ct == nil {
		t.Fatal("context_trim missing from claudeToolDefs")
	}
	props := schemaProps(t, ct)
	if _, ok := props["call_id"]; !ok {
		t.Fatal("context_trim schema missing required parameter call_id")
	}
	if _, ok := props["replace_with"]; !ok {
		t.Fatal("context_trim schema missing optional parameter replace_with")
	}
	if req := schemaRequired(t, ct); len(req) != 1 || req[0] != "call_id" {
		t.Fatalf("context_trim required = %v, want [call_id] (replace_with stays optional)", req)
	}
	for _, want := range []string{
		"dead end",
		"replace_with",
		"one-line conclusion",
		"copy it verbatim",             // stamped-id enabler (models hallucinate call ids — live smoke find)
		"at most once",                 // dedup promise
		"no-op",                        // silent no-op promise
		"TRIM DEAD-ENDS NOW",           // rewrite anchor #1: command trigger first sentence
		"ALWAYS trim before moving on", // rewrite anchor #2: ALWAYS/NEVER discipline
	} {
		if !strings.Contains(ct.Description, want) {
			t.Errorf("context_trim description missing %q", want)
		}
	}
}

// TestContextTrimValidateToolArgs: call_id is enforced; replace_with stays
// optional (missing = delete).
func TestContextTrimValidateToolArgs(t *testing.T) {
	if msg := validateToolArgs("context_trim", map[string]interface{}{}); msg != "Missing required parameter: call_id" {
		t.Fatalf("empty args msg = %q, want call_id error", msg)
	}
	if msg := validateToolArgs("context_trim", map[string]interface{}{"call_id": "  "}); msg != "Missing required parameter: call_id" {
		t.Fatalf("blank call_id msg = %q, want call_id error", msg)
	}
	if msg := validateToolArgs("context_trim", map[string]interface{}{"call_id": "call_x"}); msg != "" {
		t.Fatalf("valid call_id msg = %q, want ''", msg)
	}
}

// ── Context.RemoveToolPair generalized unit tests ────────────────────────

// ctxWithBashPair builds a context whose history ends with a failed bash
// tool_use + tool_result pair (the classic dead end).
func ctxWithBashPair() *Context {
	c := &Context{}
	c.AddUserMessage("epic task input")
	c.AddAssistantMessageWithThinking("trying approach A", []deepseek.ToolCall{
		{ID: "call_b1", Name: "bash", Arguments: map[string]interface{}{"command": "ls /nonexistent"}},
	}, "", "")
	c.AddToolResult("call_b1", "exit=1\nls: cannot access '/nonexistent': No such file or directory")
	return c
}

// TestRemoveToolPair_ReplaceKeepsPairSwapsContent: replace_with swaps only the
// tool_result content; both messages survive, roles unchanged, and the
// conclusion is retrievable (防遗忘).
func TestRemoveToolPair_ReplaceKeepsPairSwapsContent(t *testing.T) {
	c := ctxWithBashPair()
	before := len(c.Messages)

	removed, replaced := c.RemoveToolPair("call_b1", "ls /nonexistent failed because the path does not exist — don't retry")
	if removed != 0 || !replaced {
		t.Fatalf("replace result = (%d, %v), want (0, true)", removed, replaced)
	}
	if len(c.Messages) != before {
		t.Fatalf("replace must not change message count: %d -> %d", before, len(c.Messages))
	}
	if c.Messages[2].Role != "tool" || c.Messages[2].ToolCallID != "call_b1" {
		t.Fatalf("tool result message must survive with structure intact: %+v", c.Messages[2])
	}
	if !strings.Contains(c.Messages[2].Content, "don't retry") || strings.Contains(c.Messages[2].Content, "cannot access") {
		t.Fatalf("tool result content must be the conclusion, not the bulky output: %q", c.Messages[2].Content)
	}
	// The tool_use stays (assistant role kept) — no alternation break.
	if c.Messages[1].Role != "assistant" || c.Messages[1].ToolCalls[0].ID != "call_b1" {
		t.Fatalf("assistant tool_use must survive replace: %+v", c.Messages[1])
	}
	if got := joinRoles(c.Messages); got != "user,assistant,tool" {
		t.Fatalf("roles after replace = %q, want user,assistant,tool", got)
	}
}

// TestRemoveToolPair_ReplaceWireRoleAlternation: after a replace, the built
// wire request must still satisfy the tool_use↔tool_result / alternation
// contract (roles were never touched, so this is near-tautological — the
// guard exists to catch a future refactor that "cleans up" the pair).
func TestRemoveToolPair_ReplaceWireRoleAlternation(t *testing.T) {
	c := ctxWithBashPair()
	c.RemoveToolPair("call_b1", "dead end — path missing, don't retry")

	req := deepseek.BuildClaudeRequest("m", c.Messages, "sys", "", claudeToolDefs, 4096, nil)
	wire := req.Messages
	for i := 1; i < len(wire); i++ {
		if wire[i].Role == wire[i-1].Role {
			t.Fatalf("role alternation violated on the wire after replace: %s at %d/%d", wire[i].Role, i-1, i)
		}
	}
	found := false
	for _, m := range wire {
		for _, b := range m.Content {
			if b.Type == "tool_result" && b.ToolUseID == "call_b1" {
				if s, ok := b.Content.(string); ok && strings.Contains(s, "don't retry") {
					found = true
				}
			}
		}
	}
	if !found {
		t.Fatal("conclusion must be on the wire in the (kept) tool_result")
	}
}

// TestRemoveToolPair_DedupOnce: the same call_id is processed at most once —
// the second trim (delete OR replace) is a silent no-op (防重复).
func TestRemoveToolPair_DedupOnce(t *testing.T) {
	c := ctxWithBashPair()
	before := len(c.Messages)

	if removed, replaced := c.RemoveToolPair("call_b1", ""); removed != 1 || replaced {
		t.Fatalf("first delete = (%d, %v), want (1, false) — assistant kept (has text), only the tool result drops", removed, replaced)
	}
	if len(c.Messages) != before-1 {
		t.Fatalf("after delete: %d messages, want %d", len(c.Messages), before-1)
	}
	// Second attempt on the same id must no-op — nothing left to find anyway,
	// and the dedup set guarantees it even for a re-added pair.
	if removed, replaced := c.RemoveToolPair("call_b1", ""); removed != 0 || replaced {
		t.Fatalf("second delete must no-op, got (%d, %v)", removed, replaced)
	}

	// Replace-then-replace dedup: first replace lands, second is a no-op
	// even though the pair is still present.
	c2 := ctxWithBashPair()
	if _, replaced := c2.RemoveToolPair("call_b1", "conclusion one"); !replaced {
		t.Fatal("first replace must land")
	}
	if removed, replaced := c2.RemoveToolPair("call_b1", "conclusion two"); removed != 0 || replaced {
		t.Fatalf("second replace must no-op, got (%d, %v)", removed, replaced)
	}
	if !strings.Contains(c2.Messages[2].Content, "conclusion one") {
		t.Fatalf("first conclusion must survive (not overwritten by second call): %q", c2.Messages[2].Content)
	}
}

// TestRemoveToolPair_ReplaceIncompletePairNoOp: replace on a pair missing its
// tool_result is a silent no-op — never partially trim.
func TestRemoveToolPair_ReplaceIncompletePairNoOp(t *testing.T) {
	c := &Context{}
	c.AddUserMessage("epic")
	c.AddAssistantMessageWithThinking("", []deepseek.ToolCall{
		{ID: "call_b1", Name: "bash", Arguments: map[string]interface{}{"command": "x"}},
	}, "", "")
	if removed, replaced := c.RemoveToolPair("call_b1", "conclusion"); removed != 0 || replaced {
		t.Fatalf("incomplete-pair replace must no-op, got (%d, %v)", removed, replaced)
	}
}

// ── Integration smoke: full loop, request-body verification ──────────────

// TestAgent_Run_ContextTrimReplaceSlimming is the 真实冒烟 verification at the
// request-body level (replace variant): the harness runs end-to-end against a
// mock API; the mock captures every request body. Round 1 calls bash which
// fails (dead-end noise), round 2 calls context_trim with replace_with, round
// 3 (final request) must carry the one-line conclusion and NOT the failed
// command's bulky output — the pair stayed (roles legal) but the noise is
// gone.
func TestAgent_Run_ContextTrimReplaceSlimming(t *testing.T) {
	type captured struct{ body string }
	var rounds []captured

	callCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		w.Header().Set("Content-Type", "text/event-stream")
		rounds = append(rounds, captured{body: readBody(t, r)})

		switch callCount {
		case 1:
			// Round 1: a bash call that FAILS — the dead end to be trimmed.
			claudeToolUseStreamID(w, "call_b1", "bash",
				map[string]interface{}{"command": "ls /nonexistent"})
		case 2:
			// Round 2: trim it, leaving a conclusion + memory_store (so the
			// completion checkpoint has nothing to force in round 3).
			claudeToolUseStreamTwo(w,
				"call_ct1", "context_trim",
				map[string]interface{}{"call_id": "call_b1", "replace_with": "ls /nonexistent failed: path missing — don't retry"},
				"call_st1", toolnames.ToolMemory,
				map[string]interface{}{"action": toolnames.ToolMemoryActionStore, "topic": "smoke topic", "question": "smoke question", "answer": "smoke answer that is long enough to pass the distillation gate comfortably"})
		default:
			// Round 3: all-completed end_turn → run exits.
			claudeTextStream(w, completedTaskListResponse, "end_turn")
		}
	}))
	defer server.Close()

	ag := &Agent{
		Client: &deepseek.Client{
			BaseURL:    server.URL,
			APIKey:     "test-key",
			HTTPClient: &http.Client{Transport: &http.Transport{}},
		},
		Ctx:           &Context{},
		Model:         "deepseek-v4-pro",
		MaxIterations: 5,
	}

	result, err := ag.Run("Do the context-trim task")
	if err != nil {
		t.Fatalf("Run() unexpected error: %v", err)
	}
	if !strings.Contains(result, "All tasks are complete") {
		t.Fatalf("expected substantive completion, got %q", result)
	}
	if callCount != 3 || len(rounds) != 3 {
		t.Fatalf("expected 3 rounds, got %d calls / %d bodies", callCount, len(rounds))
	}

	// Round 2's request (before the trim executed) still carries the bulky
	// failed output + the stamped call id the LLM copies verbatim.
	if !strings.Contains(rounds[1].body, "cannot access '/nonexistent'") {
		t.Fatalf("round 2 request must carry the failed bash output (baseline)")
	}
	if !strings.Contains(rounds[1].body, "[tool_use id: call_b1]") {
		t.Fatalf("round 2 request must carry the stamped tool_use id (verbatim-copy enabler)")
	}
	// Round 3's request: the bulky output is GONE, the conclusion is IN, and
	// the pair structure survives (roles legal).
	if strings.Contains(rounds[2].body, "cannot access '/nonexistent'") {
		t.Fatalf("round 3 request still contains the replaced dead-end output")
	}
	if !strings.Contains(rounds[2].body, "don't retry") {
		t.Fatalf("round 3 request must carry the one-line conclusion (防遗忘): %s", truncate(rounds[2].body, 400))
	}
	if !strings.Contains(rounds[2].body, "call_b1") {
		t.Fatalf("round 3 request must keep the tool_use id (pair kept, content swapped)")
	}
	// Wire role alternation holds in round 3.
	req3 := decodeClaudeRequest(t, rounds[2].body)
	for i := 1; i < len(req3.Messages); i++ {
		if req3.Messages[i].Role == req3.Messages[i-1].Role {
			t.Fatalf("role alternation violated in round 3 wire: %s at %d/%d", req3.Messages[i].Role, i-1, i)
		}
	}
}

// TestAgent_Run_ContextTrimDeleteSlimming is the delete variant: without
// replace_with the whole pair vanishes from the round-3 request.
func TestAgent_Run_ContextTrimDeleteSlimming(t *testing.T) {
	type captured struct{ body string }
	var rounds []captured

	callCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		w.Header().Set("Content-Type", "text/event-stream")
		rounds = append(rounds, captured{body: readBody(t, r)})

		switch callCount {
		case 1:
			claudeToolUseStreamID(w, "call_r1", "read",
				map[string]interface{}{"path": "/obsolete"})
		case 2:
			// No replace_with → pure delete; + memory_store for the checkpoint.
			claudeToolUseStreamTwo(w,
				"call_ct1", "context_trim",
				map[string]interface{}{"call_id": "call_r1"},
				"call_st1", toolnames.ToolMemory,
				map[string]interface{}{"action": toolnames.ToolMemoryActionStore, "topic": "smoke topic", "question": "smoke question", "answer": "smoke answer that is long enough to pass the distillation gate comfortably"})
		default:
			claudeTextStream(w, completedTaskListResponse, "end_turn")
		}
	}))
	defer server.Close()

	ag := &Agent{
		Client: &deepseek.Client{
			BaseURL:    server.URL,
			APIKey:     "test-key",
			HTTPClient: &http.Client{Transport: &http.Transport{}},
		},
		Ctx:           &Context{},
		Model:         "deepseek-v4-pro",
		MaxIterations: 5,
	}
	result, err := ag.Run("Do the delete variant task")
	if err != nil {
		t.Fatalf("Run() unexpected error: %v", err)
	}
	if !strings.Contains(result, "All tasks are complete") {
		t.Fatalf("expected substantive completion, got %q", result)
	}
	if len(rounds) != 3 {
		t.Fatalf("captured %d request bodies, want 3", len(rounds))
	}
	if !strings.Contains(rounds[1].body, "/obsolete") {
		t.Fatalf("round 2 request must still carry the read output (baseline)")
	}
	if strings.Contains(rounds[2].body, "/obsolete") {
		t.Fatalf("round 3 request still contains the deleted read pair (context not trimmed)")
	}
	// The trim tool's own pair stays visible (the LLM sees the trim happened).
	if !strings.Contains(rounds[2].body, "context_trim") {
		t.Fatalf("round 3 request must keep the context_trim call (evidence of the trim)")
	}
}

// TestContextTrimDispatchNoOp: dispatch on an unknown id returns a truthful
// no-op result (no error, silent by contract).
func TestContextTrimDispatchNoOp(t *testing.T) {
	ag := &Agent{Ctx: &Context{}}
	ag.Ctx.AddUserMessage("epic")
	res := ag.executeToolDefault(deepseek.ToolCall{
		ID:   "call_ct1",
		Name: "context_trim",
		Arguments: map[string]interface{}{
			"call_id": "call_ghost",
		},
	})
	if !strings.Contains(res, "no-op") || strings.Contains(res, "error") {
		t.Fatalf("unknown-id dispatch result = %q, want truthful no-op (no error)", res)
	}
}

// ── Dead-end reactive nudge (deadEndTrimMessage) ─────────────────────────

func TestResultsHaveDeadEnd(t *testing.T) {
	type ccase struct {
		name   string
		tool   deepseek.ToolCall
		result string
		want   bool
	}
	search := func() deepseek.ToolCall {
		return deepseek.ToolCall{Name: toolnames.ToolMemory, Arguments: map[string]interface{}{"action": toolnames.ToolMemoryActionSearch}}
	}
	cases := []ccase{
		{"bash non-zero exit", deepseek.ToolCall{Name: "bash"}, "exit=1\n[stderr] no such file", true},
		{"bash success exit=0", deepseek.ToolCall{Name: "bash"}, "exit=0\ncompiled ok", false},
		{"Error reading missing file", deepseek.ToolCall{Name: "read"}, "Error reading x.md: no such file", true},
		{"python traceback", deepseek.ToolCall{Name: "bash"}, "Traceback\nModuleNotFoundError: No module named 'x'", true},
		{"clean useful output", deepseek.ToolCall{Name: "bash"}, "12 tests passed\nall green", false},
		{"empty result not dead end", deepseek.ToolCall{Name: "bash"}, "", false},
		{"dead and alive batch", deepseek.ToolCall{Name: "bash"}, "exit=0 fine", false},
		{"bash command with failed word", deepseek.ToolCall{Name: "bash"}, "exit=1\ntest failed: x", true},
		// False-positive regression (live bug 2026-08-29): a memory
		// action=search result whose ANSWER PROSE contains generic dead-end
		// words must NOT be flagged — the tool succeeded, the prose is
		// retrieved content (action-aware: only action=search is exempt).
		{"memory prose with 'failed'", search(), "#9 t1 tier=warm conf=0.85\nQ: why\nA: X failed because Y", false},
		{"memory prose with 'not found'", search(), "#9 t1 tier=warm\nQ: w\nA: the file was not found, so we retried", false},
		{"memory prose with '[stderr]'", search(), "#9 t1 tier=warm\nA: [stderr] is captured in the log", false},
		{"memory prose with 'Exception'", search(), "#9 t1\nQ: w\nA: an Exception was raised in parse", false},
		{"memory prose with traceback words", search(), "#9 t1\nA: Traceback line in the crash log", false},
		{"memory real tool error", search(), "memory error: empty output from memory engine", true},
		{"memory unparseable error", search(), "memory error: unparseable memory engine output", true},
		{"web_search prose 'not found'", deepseek.ToolCall{Name: "web_search"}, "results: page not found on host", false},
		{"code_search prose 'failed'", deepseek.ToolCall{Name: "code_search"}, "definition of FailedBuild", false},
	}
	for _, c := range cases {
		tc := []deepseek.ToolCall{c.tool}
		if got := resultsHaveDeadEnd(tc, []string{c.result}); got != c.want {
			t.Errorf("%s: resultsHaveDeadEnd(%s, %q) = %v, want %v", c.name, c.tool.Name, c.result, got, c.want)
		}
	}
}

func TestResultsHaveDeadEnd_batchMixed(t *testing.T) {
	// A mixed batch: one genuine bash failure + one productive memory
	// action=search. Both must be classified independently: bash dead-end
	// true, memory false.
	tc := []deepseek.ToolCall{
		{Name: "bash"},
		{Name: toolnames.ToolMemory, Arguments: map[string]interface{}{"action": toolnames.ToolMemoryActionSearch}},
	}
	res := []string{"exit=0\nall green", "#9 t1 tier=warm\nA: it failed but we fixed it"}
	if got := resultsHaveDeadEnd(tc, res); got {
		t.Errorf("mixed batch should NOT dead-end (memory prose 'failed' is not a tool failure), got %v", got)
	}
	tc2 := []deepseek.ToolCall{{Name: "bash"}, {Name: "read"}}
	res2 := []string{"exit=0\nok", "Error reading missing: no such file"}
	if got := resultsHaveDeadEnd(tc2, res2); !got {
		t.Errorf("mixed batch with real dead end should flag it")
	}
}

func TestDeadEndTrimMessage_hasCallIdGuidance(t *testing.T) {
	for _, want := range []string{"call_id", "one-line conclusion", "[tool_use id", "context_trim", "dead end"} {
		if !strings.Contains(deadEndTrimMessage, want) {
			t.Errorf("deadEndTrimMessage missing %q", want)
		}
	}
}
