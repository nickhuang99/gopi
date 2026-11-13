package agent

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nick/gopi/internal/deepseek"
	"github.com/nick/gopi/internal/toolnames"
	"github.com/nick/gopi/internal/tools"
)

// toolDefByName returns the claudeToolDefs entry for name, or nil.
func toolDefByName(t *testing.T, name string) *deepseek.ClaudeToolDef {
	t.Helper()
	for i := range claudeToolDefs {
		if claudeToolDefs[i].Name == name {
			return &claudeToolDefs[i]
		}
	}
	return nil
}

// schemaProps returns the schema's properties map (nil when the schema is
// not the expected map shape). InputSchema is interface{} by contract, so
// the lookup is best-effort.
func schemaProps(t *testing.T, td *deepseek.ClaudeToolDef) map[string]interface{} {
	t.Helper()
	m, ok := td.InputSchema.(map[string]interface{})
	if !ok {
		t.Fatalf("InputSchema of %s is not a map", td.Name)
	}
	props, _ := m["properties"].(map[string]interface{})
	return props
}

// schemaRequired returns the schema's required list (nil when absent).
func schemaRequired(t *testing.T, td *deepseek.ClaudeToolDef) []string {
	t.Helper()
	m, ok := td.InputSchema.(map[string]interface{})
	if !ok {
		t.Fatalf("InputSchema of %s is not a map", td.Name)
	}
	req, _ := m["required"].([]string)
	return req
}

// TestMemoryToolSchemasPresent: the merged memory tool must appear in the
// schema list with ONE def carrying the action enum plus every action's
// parameters (acceptance: "工具 schema 出现在 schema 列表").
func TestMemoryToolSchemasPresent(t *testing.T) {
	mem := toolDefByName(t, toolnames.ToolMemory)
	if mem == nil {
		t.Fatal("memory missing from claudeToolDefs")
	}
	props := schemaProps(t, mem)
	for _, key := range []string{"action", "query", "limit", "intent", "topic", "question",
		"answer", "source", "keywords", "type", "name", "trigger", "preconditions",
		"steps", "tools_used", "id", "rating", "reason", "tier", "confidence"} {
		if _, ok := props[key]; !ok {
			t.Errorf("memory schema missing parameter %q", key)
		}
	}
	// The only required parameter is action — each action's own params are
	// conditional (validated in validateToolArgs).
	if req := schemaRequired(t, mem); len(req) != 1 || req[0] != "action" {
		t.Errorf("memory required = %v, want [action]", req)
	}
	// Action enum must carry all four actions.
	actProps := props["action"].(map[string]interface{})
	enum, _ := actProps["enum"].([]string)
	want := []string{toolnames.ToolMemoryActionSearch, toolnames.ToolMemoryActionStore,
		toolnames.ToolMemoryActionUpdate, toolnames.ToolMemoryActionFeedback}
	if len(enum) != len(want) {
		t.Fatalf("memory action enum = %v, want %v", enum, want)
	}
	for i := range want {
		if enum[i] != want[i] {
			t.Errorf("memory action enum[%d] = %q, want %q", i, enum[i], want[i])
		}
	}
}

// TestMemoryToolDescriptionStrategy guards the 描述即策略 quality bar (6788
// design consensus): the merged memory tool's description must teach when to
// use / when to skip (value-gated, not dumb RAG) for search, and enforce the
// store write gate — distilled experience only, never raw conversation logs
// (蒸馏, 流水账) — while keeping the 5W1H breadcrumb philosophy.
func TestMemoryToolDescriptionStrategy(t *testing.T) {
	mem := toolDefByName(t, toolnames.ToolMemory)
	if mem == nil {
		t.Fatal("memory missing from claudeToolDefs")
	}
	for _, want := range []string{
		"action=" + toolnames.ToolMemoryActionSearch,           // the search action is described
		"USE FIRST at the start of every task",                 // when to use (persuasion draft §3.2)
		"SKIP off-topic",                                       // when NOT to use
		"not dumb RAG",                                         // non-dumb retrieval
		"tier / confidence / lesson_type / stored+age / about", // value signals → LLM judgment
		"CONTRADICTS",                                          // hit wrong → useless + update (bug-report culture)
		"action=" + toolnames.ToolMemoryActionStore,            // the store action is described
		"DISTILLED",                                            // distilled experience
		"NEVER store raw conversation transcripts",             // 流水账 write gate
		"USE MID-TASK",                                         // mid-task notable trigger (floor, not ceiling)
		"task end is the floor",                                // interruption loss framing
		"how to succeed / what NOT to do",                      // answer structure
		"5W1H",                                                 // breadcrumb philosophy
		"breadcrumbs",                                          // 检索面包屑 — keywords are for future self
		"action=" + toolnames.ToolMemoryActionFeedback,         // feedback action described
		"action=" + toolnames.ToolMemoryActionUpdate,           // update action described
		"One vote per session per hit",                         // dedup contract
		"cold-tier rows excluded",                              // tier gating visible to the model
	} {
		if !strings.Contains(mem.Description, want) {
			t.Errorf("memory description missing strategy %q", want)
		}
	}
}

// TestValidateToolArgsMemory: action is required; each action enforces its
// own write-gate params before dispatch, with the standardized error message
// the model can self-correct from.
func TestValidateToolArgsMemory(t *testing.T) {
	// Missing action entirely → invalid.
	if msg := validateToolArgs(toolnames.ToolMemory, map[string]interface{}{}); !strings.Contains(msg, "Invalid memory action") {
		t.Errorf("memory without action: %q", msg)
	}

	// action=search requires query.
	searchMissing := validateToolArgs(toolnames.ToolMemory, map[string]interface{}{"action": toolnames.ToolMemoryActionSearch})
	if searchMissing != "Missing required parameter: query (memory action=search)" {
		t.Errorf("memory action=search missing query: %q", searchMissing)
	}
	if msg := validateToolArgs(toolnames.ToolMemory, map[string]interface{}{"action": toolnames.ToolMemoryActionSearch, "query": "x"}); msg != "" {
		t.Errorf("memory action=search valid args rejected: %q", msg)
	}

	// action=store requires the topic/question/answer triple.
	for _, key := range []string{"topic", "question", "answer"} {
		args := map[string]interface{}{"action": toolnames.ToolMemoryActionStore, "topic": "t", "question": "q", "answer": "a"}
		delete(args, key)
		if msg := validateToolArgs(toolnames.ToolMemory, args); msg != "Missing required parameter: "+key+" (memory action=store)" {
			t.Errorf("memory action=store missing %s: %q", key, msg)
		}
	}
	if msg := validateToolArgs(toolnames.ToolMemory, map[string]interface{}{
		"action": toolnames.ToolMemoryActionStore, "topic": "t", "question": "q", "answer": "a",
	}); msg != "" {
		t.Errorf("memory action=store valid args rejected: %q", msg)
	}

	// action=feedback requires id + rating.
	for _, key := range []string{"id", "rating"} {
		args := map[string]interface{}{"action": toolnames.ToolMemoryActionFeedback, "id": float64(1), "rating": "useful"}
		delete(args, key)
		if msg := validateToolArgs(toolnames.ToolMemory, args); msg != "Missing required parameter: "+key+" (memory action=feedback)" {
			t.Errorf("memory action=feedback missing %s: %q", key, msg)
		}
	}
	if msg := validateToolArgs(toolnames.ToolMemory, map[string]interface{}{
		"action": toolnames.ToolMemoryActionFeedback, "id": float64(1), "rating": "useless",
	}); msg != "" {
		t.Errorf("memory action=feedback valid args rejected: %q", msg)
	}

	// action=update: id required; with only id there is nothing to update.
	if msg := validateToolArgs(toolnames.ToolMemory, map[string]interface{}{"action": toolnames.ToolMemoryActionUpdate}); msg != "Missing required parameter: id (memory action=update)" {
		t.Errorf("memory action=update missing id: %q", msg)
	}
	if msg := validateToolArgs(toolnames.ToolMemory, map[string]interface{}{"action": toolnames.ToolMemoryActionUpdate, "id": float64(1)}); msg != "memory(action=update): nothing to update (set at least one field)" {
		t.Errorf("memory action=update empty update rejected wrong: %q", msg)
	}
	for _, args := range []map[string]interface{}{
		{"action": toolnames.ToolMemoryActionUpdate, "id": float64(1), "tier": "cold"},
		{"action": toolnames.ToolMemoryActionUpdate, "id": float64(1), "topic": "t"},
		{"action": toolnames.ToolMemoryActionUpdate, "id": float64(1), "answer": "a"},
		{"action": toolnames.ToolMemoryActionUpdate, "id": float64(1), "keywords": []interface{}{"k"}},
		{"action": toolnames.ToolMemoryActionUpdate, "id": float64(1), "confidence": float64(0.9)},
		{"action": toolnames.ToolMemoryActionUpdate, "id": float64(1), "source": "s"},
	} {
		if msg := validateToolArgs(toolnames.ToolMemory, args); msg != "" {
			t.Errorf("memory action=update valid args rejected: %q (args=%v)", msg, args)
		}
	}

	// Unknown action is rejected with the enum spelled out.
	if msg := validateToolArgs(toolnames.ToolMemory, map[string]interface{}{"action": "bogus"}); !strings.Contains(msg, "Invalid memory action") {
		t.Errorf("unknown memory action: %q", msg)
	}
}

// TestMemoryToolInHeader: the system prompt routes the memory tool by its
// merged name, so the model knows to reach for it even before reading the
// schemas.
func TestMemoryToolInHeader(t *testing.T) {
	p := assembleSystemPrompt()
	for _, name := range []string{
		"memory(action=search)", "memory(action=store)",
		"memory(action=feedback", "memory(action=update",
	} {
		if !strings.Contains(p, name) {
			t.Errorf("prompt missing memory routing %q", name)
		}
	}
}

// TestMemoryFeedbackToolSchema: the merged memory def's feedback action keeps
// the three-valued rating contract (id + rating required on feedback).
func TestMemoryFeedbackToolSchema(t *testing.T) {
	mem := toolDefByName(t, toolnames.ToolMemory)
	if mem == nil {
		t.Fatal("memory missing from claudeToolDefs")
	}
	props := schemaProps(t, mem)
	for _, key := range []string{"id", "rating", "reason"} {
		if _, ok := props[key]; !ok {
			t.Errorf("memory schema missing feedback parameter %q", key)
		}
	}
	desc := mem.Description
	for _, want := range []string{
		"useful",                       // vote directions
		"useless",                      // vote directions
		"CONTRADICTS",                  // useless-only-when-contradiction gate (bug-report culture)
		"feedback says WHAT is wrong",  // division of labor: feedback says, update fixes
		"One vote per session per hit", // dedup contract
		"action=" + toolnames.ToolMemoryActionFeedback,
	} {
		if !strings.Contains(desc, want) {
			t.Errorf("memory feedback description missing %q", want)
		}
	}
}

// TestMemoryFeedbackToolDispatch: memory(action=feedback) records a vote
// through the engine; the session key is the agent's session id when present.
func TestMemoryFeedbackToolDispatch(t *testing.T) {
	path := filepath.Join(t.TempDir(), "memory.db")
	t.Setenv("GOPI_MEMORY_DB", path)
	if _, err := tools.MemoryStore("fb topic", "fb question here", "fb answer text that is long enough to pass the gate", "s", nil); err != nil {
		t.Fatalf("seed store: %v", err)
	}
	call := func(ag *Agent, rating string) string {
		return ag.executeTool(deepseek.ToolCall{Name: toolnames.ToolMemory, Arguments: map[string]interface{}{
			"action": toolnames.ToolMemoryActionFeedback, "id": float64(1), "rating": rating, "reason": "worked",
		}})
	}
	// With a session logger the session id is the dedup key: the first vote
	// records, the same-session second vote is ignored (one per run).
	a := &Agent{SessionLog: &SessionLogger{sessionID: "test-session"}}
	if res := call(a, "useful"); strings.HasPrefix(res, "memory error:") || !strings.Contains(res, "rating=useful") {
		t.Fatalf("dispatch failed: %s", res)
	}
	if res := call(a, "useless"); !strings.Contains(res, "already voted") {
		t.Errorf("same-session second vote must be deduped: %s", res)
	}
	// Without a session logger the task key is empty → votes always record.
	a2 := &Agent{}
	if res := call(a2, "useless"); !strings.Contains(res, "rating=useless") {
		t.Errorf("sessionless vote must record: %s", res)
	}
}

// TestMemoryUpdateToolSchema: the merged memory def's update action keeps the
// optional-field contract — id required, at least one editable field — plus
// the 描述即策略 guardrails: trust boundary (only retrieved records),
// no-delete, and the conservative demotion-first ladder.
func TestMemoryUpdateToolSchema(t *testing.T) {
	mem := toolDefByName(t, toolnames.ToolMemory)
	if mem == nil {
		t.Fatal("memory missing from claudeToolDefs")
	}
	props := schemaProps(t, mem)
	for _, key := range []string{"id", "tier", "topic", "question", "answer", "source", "confidence", "keywords"} {
		if _, ok := props[key]; !ok {
			t.Errorf("memory schema missing update parameter %q", key)
		}
	}
	desc := mem.Description
	for _, want := range []string{
		"VERIFIED evidence", // certainty bar: hard edit, not a hunch
		"hot/warm",          // corrected records stay retrievable
		"action=" + toolnames.ToolMemoryActionFeedback, // division of labor: feedback = soft, update = hard
		"Do NOT use update to delete history",          // audit trail stays intact
		"topic/question/answer/tier",                   // input contract
		"CORRECTED field",                              // fix the wrong field, not a patch-note
		"action=" + toolnames.ToolMemoryActionUpdate,
	} {
		if !strings.Contains(desc, want) {
			t.Errorf("memory update description missing strategy %q", want)
		}
	}
}

// TestMemoryUpdateToolDispatch: memory(action=update) edits through the same
// engine function as the CLI (gopi memory update <id> ...) — updated keywords
// become searchable and the answer tail is rewritten; a bad id surfaces the
// standardized error.
func TestMemoryUpdateToolDispatch(t *testing.T) {
	path := filepath.Join(t.TempDir(), "memory.db")
	t.Setenv("GOPI_MEMORY_DB", path)
	if _, err := tools.MemoryStore("up topic", "up question here", "up answer text that is long enough to pass the gate", "s", nil); err != nil {
		t.Fatalf("seed store: %v", err)
	}
	a := &Agent{}
	res := a.executeTool(deepseek.ToolCall{Name: toolnames.ToolMemory, Arguments: map[string]interface{}{
		"action":   toolnames.ToolMemoryActionUpdate,
		"id":       float64(1),
		"tier":     "warm",
		"keywords": []interface{}{"unique-breadcrumb-xyz"},
	}})
	if !strings.Contains(res, "Memory updated (id=1)") {
		t.Fatalf("dispatch failed: %s", res)
	}
	// The new keyword breadcrumb must now retrieve the record (MemoryUpdate
	// rewrites both search_kw and the answer's "keywords:" tail).
	out, err := tools.MemorySearch("unique-breadcrumb-xyz", 5, "")
	if err != nil {
		t.Fatalf("search after update: %v", err)
	}
	if !strings.Contains(out, "unique-breadcrumb-xyz") {
		t.Errorf("updated keywords not searchable: %s", out)
	}
	// Missing record → standardized engine error, same as the CLI.
	if res := a.executeTool(deepseek.ToolCall{Name: toolnames.ToolMemory, Arguments: map[string]interface{}{
		"action": toolnames.ToolMemoryActionUpdate, "id": float64(999), "tier": "cold",
	}}); !strings.Contains(res, "memory error:") {
		t.Errorf("missing record must error: %s", res)
	}
}

// TestMemoryActionAwareParallelSafety — the merged `memory` tool is
// action-aware for parallelism (mirror of task(action=view)'s R7 rule): only
// the READ-ONLY action=search may overlap with other calls; the
// side-effect actions (store writes a record, feedback mutates trust scores
// and may drop context, update rewrites a record) must run serial — their
// ordering and atomicity matter.
func TestMemoryActionAwareParallelSafety(t *testing.T) {
	search := deepseek.ToolCall{Name: toolnames.ToolMemory, Arguments: map[string]interface{}{
		"action": toolnames.ToolMemoryActionSearch, "query": "x"}}
	store := deepseek.ToolCall{Name: toolnames.ToolMemory, Arguments: map[string]interface{}{
		"action": toolnames.ToolMemoryActionStore, "topic": "t", "question": "q", "answer": "a"}}
	feedback := deepseek.ToolCall{Name: toolnames.ToolMemory, Arguments: map[string]interface{}{
		"action": toolnames.ToolMemoryActionFeedback, "id": float64(1), "rating": "useful"}}
	update := deepseek.ToolCall{Name: toolnames.ToolMemory, Arguments: map[string]interface{}{
		"action": toolnames.ToolMemoryActionUpdate, "id": float64(1), "tier": "cold"}}

	if !parallelSafeCall(search) {
		t.Error("memory(action=search) must be parallel-safe (read-only retrieval)")
	}
	if parallelSafeCall(store) {
		t.Error("memory(action=store) must run serial (writes a record)")
	}
	if parallelSafeCall(feedback) {
		t.Error("memory(action=feedback) must run serial (mutates trust, may drop context)")
	}
	if parallelSafeCall(update) {
		t.Error("memory(action=update) must run serial (rewrites a record)")
	}
}

// TestMemoryLegacyNames_DispatchLiteralGate (Rule 13 anti-regression guard):
// the four pre-merge tool wire names must never re-enter the AGENT DISPATCH
// surface as tool names — the merged `memory` tool + its action consts are
// the single source of truth. loop.go and compact.go are the dispatch /
// context-classification files; any literal `memory_search` /
// `memory_store` / `memory_update` / `memory_feedback` there would mean a
// bare bypass of toolnames.ToolMemoryAction* (the retired-name tool dispatch
// is what the merge eliminated).
func TestMemoryLegacyNames_DispatchLiteralGate(t *testing.T) {
	legacy := []string{
		`"memory_search"`, `"memory_store"`, `"memory_update"`, `"memory_feedback"`,
	}
	for _, f := range []string{"loop.go", "compact.go"} {
		b, err := os.ReadFile(f)
		if err != nil {
			t.Fatalf("read %s: %v", f, err)
		}
		for _, lit := range legacy {
			if bytes.Contains(b, []byte(lit)) {
				t.Errorf("%s carries legacy wire literal %s — the memory family dispatches via toolnames.ToolMemory + ToolMemoryAction* (merged tool)",
					f, lit)
			}
		}
	}
}
