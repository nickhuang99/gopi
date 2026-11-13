package tokenizer

// prompt_test.go — the rendering's SHAPE, and the one thing that can be checked
// without a network: the template's grammar, branch for branch.
//
// These arms replace a suite that pinned a CALIBRATION (161 + 16.5 per message)
// against sixteen embedded server counts. That suite is gone with the constants
// it guarded: the render is now a translation of the V4.1 template, and a
// translation is checked by the bytes it produces and by the token costs the
// endpoint charges for them, both of which are below.
//
// Every server number quoted here was MEASURED (POST /anthropic/v1/messages/
// count_tokens, chat mode i.e. `thinking: {"type":"disabled"}`, on this machine);
// the arm that uses them compares them with each other, never with a constant.

import (
	"encoding/json"
	"strings"
	"testing"
)

func loadTk(t *testing.T) *Tokenizer {
	t.Helper()
	tk, err := Default()
	if err != nil {
		t.Fatalf("load tokenizer: %v", err)
	}
	return tk
}

// render is the test's shorthand: no tool schemas.
func render(system string, msgs ...Message) string {
	return RenderPrompt(system, msgs, nil)
}

// count is the test's shorthand for a rendered count with no tool schemas.
func count(t *testing.T, system string, msgs ...Message) int {
	t.Helper()
	n, err := loadTk(t).PromptTokens(system, msgs, nil)
	if err != nil {
		t.Fatalf("PromptTokens: %v", err)
	}
	return n
}

// countWith fails the test rather than returning a zero when the tokenizer
// reports an error: a count is a budget, and a silent zero is the one answer
// no caller can tell apart from an empty prompt (PROJECT.md Rule 15).
func countWith(t *testing.T, tk *Tokenizer, system string, msgs []Message, schemas []json.RawMessage) int {
	t.Helper()
	n, err := tk.PromptTokens(system, msgs, schemas)
	if err != nil {
		t.Fatalf("PromptTokens: %v", err)
	}
	return n
}

// TestRenderPromptFollowsTheTemplateGrammar pins the exact bytes of the ordinary
// conversation shapes. The expectations are the template's own output, which is
// what the endpoint tokenizes: a rendering that is merely close is a wrong
// answer, and every estimate built on it is wrong in the same direction.
//
// The trailing <｜Assistant｜> is the template's TRANSITION marker: it is emitted
// when the next turn is an assistant turn or when the message is the last one.
// In chat mode the template follows it with `</think>` (line 249 of the
// template: the `<think>` branch requires thinking mode, the `</think>` branch
// is the else).
func TestRenderPromptFollowsTheTemplateGrammar(t *testing.T) {
	transition := markAssistant + markThinkEnd
	cases := []struct {
		name   string
		system string
		msgs   []Message
		want   string
	}{
		{
			name: "one user turn",
			msgs: []Message{{Role: RoleUser, Text: "hi"}},
			want: markBOS + markUser + "hi" + transition,
		},
		{
			name: "two user turns are MERGED into one, joined by a blank line",
			msgs: []Message{{Role: RoleUser, Text: "hi"}, {Role: RoleUser, Text: "ok"}},
			want: markBOS + markUser + "hi\n\nok" + transition,
		},
		{
			name: "an assistant reply is closed by EOS and opens the next turn",
			msgs: []Message{{Role: RoleUser, Text: "hi"}, {Role: RoleAssistant, Text: "yo"}, {Role: RoleUser, Text: "ok"}},
			want: markBOS + markUser + "hi" + transition + "yo" + markEOS + markUser + "ok" + transition,
		},
		{
			name:   "a system prompt opens the prompt with its own marker",
			system: "S",
			msgs:   []Message{{Role: RoleUser, Text: "hi"}},
			want:   markBOS + markSystem + "S" + markUser + "hi" + transition,
		},
		{
			name: "an EMPTY user turn still renders its marker",
			msgs: []Message{{Role: RoleUser}},
			want: markBOS + markUser + transition,
		},
	}
	for _, c := range cases {
		got := render(c.system, c.msgs...)
		if got != c.want {
			t.Fatalf("%s:\n got %q\nwant %q", c.name, got, c.want)
		}
	}
}

// TestPromptTokensChargesMarkersOneTokenAndTextAsBPE pins the two DIFFERENT
// tokenizations the endpoint applies to one rendered prompt, which is the reason
// this package renders SEGMENTS instead of a string.
//
// MEASURED: the template's own markers are single added tokens (a marker costs
// 1), while the same spelling arriving as message CONTENT is ordinary text and
// costs its BPE price — `<｜User｜>` 5, `<｜Assistant｜>` 5, `<｜end▁of▁sentence｜>` 12,
// `<think>` 3, `｜DSML｜` 4. Encoding the rendered string with added-token matching
// would charge 1 for content markers and under-count a real request that carries
// templates in its own rules text (this project's does).
func TestPromptTokensChargesMarkersOneTokenAndTextAsBPE(t *testing.T) {
	tk := loadTk(t)

	// One emitted marker costs exactly one token; the text is its own BPE cost.
	// BOS + User + hi + Assistant + </think>: four markers and one text token.
	if got, want := count(t, "", Message{Role: RoleUser, Text: "hi"}), 5; got != want {
		t.Fatalf("BOS+User+hi+Assistant+</think> rendered %d tokens, want %d (four markers at one token each, plus the text)", got, want)
	}

	// The SAME marker as content costs more than one.
	asContent := count(t, "", Message{Role: RoleUser, Text: markEOS})
	asMarker := count(t, "", Message{Role: RoleUser})
	if asContent <= asMarker {
		t.Fatalf("a marker inside message content cost %d tokens against a bare turn's %d — content is being charged the added-token price", asContent, asMarker)
	}
	if n := tk.CountText(markEOS); n < 2 {
		t.Fatalf("CountText(%q) = %d, want >= 2: the endpoint charges 12 for it as content", markEOS, n)
	}
}

// TestRenderPromptRendersAssistantToolCallsAsDSML pins the V4.1 tool-call
// grammar. This is where the previous renderer was translating the WRONG
// template: V3 wrote `<｜tool▁call▁begin｜>function<｜tool▁sep｜>name\n```json`,
// V4.1 writes `<｜DSML｜ invoke name="…">` / `<｜DSML｜ parameter name="…"
// string="true|false">`, and the schemas arrive in a preamble instead of being
// appended as JSON.
func TestRenderPromptRendersAssistantToolCallsAsDSML(t *testing.T) {
	got := render("", Message{Role: RoleUser, Text: "go"}, Message{Role: RoleAssistant,
		Blocks: []Block{{Type: BlockToolUse, Name: "bash", ID: "c1", Input: `{"command":"ls -la"}`}}})

	for _, want := range []string{
		`<｜DSML｜ calls>`,
		`<｜DSML｜ invoke name="bash">`,
		`<｜DSML｜ parameter name="command" string="true">ls -la`,
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("the rendering does not contain %q:\n%s", want, got)
		}
	}
	// A non-string argument is JSON and says so.
	got = render("", Message{Role: RoleAssistant,
		Blocks: []Block{{Type: BlockToolUse, Name: "bash", Input: `{"n":3,"ok":true}`}}})
	if !strings.Contains(got, `"string="false">3`) && !strings.Contains(got, `string="false">true`) {
		t.Fatalf("a non-string argument was not rendered as string=\"false\":\n%s", got)
	}
	if strings.Contains(got, "tool\u2581call\u2581begin") {
		t.Fatalf("the V3 tool-call grammar is still being rendered:\n%s", got)
	}
}

// TestRenderPromptToolResultIsAUserBlockNotAToolMessage pins step 1 of the
// template: a tool result is a `tool_result` content block on a USER turn, and a
// following user text block joins it with a blank line. Rendering it as its own
// role would emit a marker that does not exist in V4.1.
func TestRenderPromptToolResultIsAUserBlockNotAToolMessage(t *testing.T) {
	got := render("",
		Message{Role: RoleUser, Text: "go"},
		Message{Role: RoleAssistant, Blocks: []Block{{Type: BlockToolUse, Name: "bash", ID: "c1", Input: `{"command":"ls"}`}}},
		Message{Role: RoleUser, Blocks: []Block{
			{Type: BlockToolResult, ID: "c1", Content: "OUT"},
			{Type: BlockText, Text: "now explain"},
		}},
	)
	want := markBOS + markUser + "go" + markAssistant + markThinkEnd +
		"\n\n<｜DSML｜ calls>\n<｜DSML｜ invoke name=\"bash\">\n<｜DSML｜ parameter name=\"command\" string=\"true\">ls</｜DSML｜ parameter>\n</｜DSML｜ invoke>\n</｜DSML｜ calls>" +
		markEOS +
		markUser + "<tool_result>OUT</tool_result>\n\nnow explain" + markAssistant + markThinkEnd
	if got != want {
		t.Fatalf("tool traffic rendered\n got %q\nwant %q", got, want)
	}
}

// TestRenderPromptCountsTheToolsPreamble pins that the tool list is rendered —
// dropping it costs ~11,600 tokens on an 18-tool request (measured) — and that it
// lands in a system turn, attaching to a NEW empty one when the request has no
// system prompt.
func TestRenderPromptCountsTheToolsPreamble(t *testing.T) {
	schema := json.RawMessage(`{"name":"bash","description":"run","input_schema":{"type":"object"}}`)
	got := RenderPrompt("", []Message{{Role: RoleUser, Text: "hi"}}, []json.RawMessage{schema})

	if !strings.HasPrefix(got, markBOS+markSystem) {
		t.Fatalf("a tools-only request did not open with a system marker:\n%.120q", got)
	}
	for _, want := range []string{
		"## Tools\n\nYou have access to a set of tools",
		"### Available Tool Schemas",
		"You MUST strictly follow the above defined tool name and parameter schemas",
		`"name": "bash"`,
		`"description": "run"`,
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("the tools section does not contain %q", want)
		}
	}

	bare := count(t, "", Message{Role: RoleUser, Text: "hi"})
	withTools := countWith(t, loadTk(t), "", []Message{{Role: RoleUser, Text: "hi"}}, []json.RawMessage{schema})
	if withTools-bare < 100 {
		t.Fatalf("the tool list contributed %d tokens — it did not reach the encoder", withTools-bare)
	}
}

// TestPromptTokensGrowsWithContent is the monotonicity a budget depends on.
func TestPromptTokensGrowsWithContent(t *testing.T) {
	tk := loadTk(t)
	short := countWith(t, tk, "s", []Message{{Role: RoleUser, Text: "hi"}}, nil)
	long := countWith(t, tk, "s", []Message{{Role: RoleUser, Text: strings.Repeat("hi", 200)}}, nil)
	if long <= short {
		t.Fatalf("longer text did not increase the count: %d vs %d", long, short)
	}
}

// TestPromptTokensIsTheRenderedCount pins the definition: PromptTokens is the
// ONE-PASS encoding of the rendered string, with added-token matching allowed
// only at template positions. No additive term may creep back in — a constant
// added here would fit one server build and silently break every other shape,
// which is exactly what the deleted calibration did.
func TestPromptTokensIsTheRenderedCount(t *testing.T) {
	tk := loadTk(t)
	msgs := []Message{
		{Role: RoleUser, Text: "hi"},
		{Role: RoleAssistant, Text: "yo"},
	}
	segs := RenderSegments("", msgs, nil)
	var b strings.Builder
	var mask []bool
	for _, s := range segs {
		b.WriteString(s.Text)
		for i := 0; i < len(s.Text); i++ {
			mask = append(mask, s.Template)
		}
	}
	masked, err := tk.EncodeMasked(b.String(), mask)
	if err != nil {
		t.Fatalf("EncodeMasked: %v", err)
	}
	want := len(masked)
	if got := countWith(t, tk, "", msgs, nil); got != want {
		t.Fatalf("PromptTokens = %d, the masked encoding of the render is %d — an additive term is back", got, want)
	}

	// And the concatenation of the segments IS the rendered string.
	if got, wantStr := RenderPrompt("", msgs, nil), concatSegments(segs); got != wantStr {
		t.Fatalf("RenderPrompt and the segments disagree:\n%q\n%q", got, wantStr)
	}
}

// concatSegments is the string form of a segment list.
func concatSegments(segs []Segment) string {
	var sb strings.Builder
	for _, s := range segs {
		sb.WriteString(s.Text)
	}
	return sb.String()
}

// TestRenderToolsIgnoresCacheControl pins a MEASURED fact about the endpoint: a
// tool's `cache_control` hint never reaches its tokenizer. Measured on the
// 18-tool request this task carried — the SAME tools sent with and without the
// hint return the IDENTICAL server total (58096 both times, `gopi tool tokens
// --body … --compare`), while charging it locally cost 13 tokens per occurrence.
func TestRenderToolsIgnoresCacheControl(t *testing.T) {
	tk := loadTk(t)
	withHint := json.RawMessage(`{"name":"bash","description":"run",` +
		`"cache_control":{"type":"ephemeral"},"input_schema":{"type":"object"}}`)
	without := json.RawMessage(`{"name":"bash","description":"run",` +
		`"input_schema":{"type":"object"}}`)
	msgs := []Message{{Role: RoleUser, Text: "hi"}}

	got := countWith(t, tk, "", msgs, []json.RawMessage{withHint})
	want := countWith(t, tk, "", msgs, []json.RawMessage{without})
	if got != want {
		t.Fatalf("cache_control changed the count: %d with it, %d without — the endpoint "+
			"counts the tool DEFINITION, not its caching hint", got, want)
	}
	if strings.Contains(RenderPrompt("", msgs, []json.RawMessage{withHint}), cacheControlKey) {
		t.Fatalf("the rendered prompt still carries %q", cacheControlKey)
	}
}

// toolsSectionMarginal is the count that ONE more identical schema adds. It is
// read as a difference between two renders that differ in nothing else, so the
// preamble, the message turn and the closing sentence all cancel.
func toolsSectionMarginal(t *testing.T, tk *Tokenizer, schema json.RawMessage, n int) int {
	t.Helper()
	msgs := []Message{{Role: RoleUser, Text: "hi"}}
	grow := func(k int) int {
		schemas := make([]json.RawMessage, k)
		for i := range schemas {
			schemas[i] = schema
		}
		return countWith(t, tk, "", msgs, schemas)
	}
	return grow(n) - grow(n-1)
}

// TestRenderToolsHasNoPerSchemaSlope is the regression arm for the defect this
// file's own rendering produced: `gopi tool tokens` was off the endpoint's count
// by an amount that GREW with the number of tool schemas, which means the error
// was attached to the schema serialization rather than to a fixed frame.
//
// The measured shape (live endpoint, ablation on the tree at be3be388): the old
// renderer re-encoded each schema through `map[string]any`, so `encoding/json`
// sorted members at EVERY level. Sorting every level −3 tokens/schema; the
// client's order +2/schema; sorting the TOP level alone 0 — so the cost rides on
// the NESTED objects and is paid once per schema, which is what makes it a slope.
//
// WHAT THIS ARM CAN AND CANNOT SEE, stated because the distinction matters: it
// asserts ours is LOCALLY linear in n, i.e. that one more schema costs the same
// at every n. That is necessary for `diff` to have zero slope, not sufficient —
// a wrong-per-schema-CONSTANT render satisfies it and is still offset from the
// server at every n. Matching the server's marginal is what makes it zero, and
// that is measured against the live endpoint (`gopi tool tokens --body …
// --compare`, 18/18 points +0, recorded in the run's report), not here. The arm
// below is the guard that fails when the per-schema cost starts DRIFTING again,
// which the old shape did and no single-sample arm could see.
//
// The assertion is a PROPERTY, not a number: adding one more identical schema
// must cost the same number of tokens at every n. A per-schema serialization
// error cannot satisfy it, and a fixed framing offset trivially does.
func TestRenderToolsHasNoPerSchemaSlope(t *testing.T) {
	tk := loadTk(t)
	schemas := map[string]json.RawMessage{
		"tiny": json.RawMessage(`{"name":"f0","description":"f","input_schema":{"type":"object","properties":{}}}`),
		"mid": json.RawMessage(`{"name":"tool_0","description":"Read a file from disk and return its contents.",` +
			`"input_schema":{"type":"object","properties":{"path":{"type":"string","description":"The path."}},` +
			`"required":["path"]}}`),
		"big": json.RawMessage(`{"name":"tool_0","description":"Execute a shell command in a sandboxed working ` +
			`directory and return its standard output, standard error and exit status. The caller must supply a ` +
			`command string; the working directory defaults to the sandbox root. Long outputs are truncated at the ` +
			`configured cap.","input_schema":{"type":"object","properties":{"command":{"type":"string"},` +
			`"workdir":{"type":"string"},"timeout_sec":{"type":"integer"},"env":{"type":"object"}},` +
			`"required":["command"]}}`),
	}
	for name, schema := range schemas {
		// n=1 is NOT a marginal measurement: going from no tools to one tool also
		// pays the section's PREAMBLE and closing sentence, which are fixed. The
		// slope lives strictly in n -> n+1 with n >= 1, so that is what is pinned.
		first := toolsSectionMarginal(t, tk, schema, 2)
		for n := 3; n <= 9; n++ {
			if got := toolsSectionMarginal(t, tk, schema, n); got != first {
				t.Fatalf("%s: the marginal cost of one schema is NOT constant — %d tokens going 1->2 "+
					"schemas, %d going %d->%d. The count carries a per-schema slope, so the diff "+
					"against the endpoint grows with the tool count instead of staying put.",
					name, first, got, n-1, n)
			}
		}
		// A schema whose marginal is a fraction of its own text would mean the
		// section stopped being rendered per entry (the original -10,806 defect).
		if first < 20 {
			t.Fatalf("%s: one schema costs only %d tokens — the section is not rendering it", name, first)
		}
		// And the FIRST schema must also cost its own text on top of the fixed
		// preamble, which is what makes the section a function of n at all.
		if opening := toolsSectionMarginal(t, tk, schema, 1); opening < first+100 {
			t.Fatalf("%s: the first schema added only %d tokens over no tools, the second %d — "+
				"the preamble is missing from the render", name, opening, first)
		}
	}
}

// TestRenderToolsEmitsTheGatewaysFunctionShape pins the BYTES, which is the
// only thing a per-schema slope can be diagnosed from: the schema must arrive in
// the shape the endpoint counts — the gateway's OpenAI function object, members
// in the CLIENT's order, `input_schema` renamed `parameters`, no `cache_control`
// — with `json.dumps` spacing.
//
// The order half is not cosmetic, and the ablation that established it is worth
// stating precisely. Re-encoding the schema through `map[string]any` sorted
// members at EVERY level, and measured against the endpoint the per-schema error
// moved with it: sorting every level −3/schema, the client's order +2/schema —
// the single variable being sorted vs client order. Sorting the TOP level alone
// measured 0, which is why the failure is a slope and not a flat offset: the
// cost is attached to the NESTED objects (`properties`, `input_schema`), so it
// is paid once per schema and grows with n exactly as a slope does.
//
// Keeping the Anthropic key name instead of the gateway's `parameters` is a
// separate, FLAT error — worth about a token, not a token per schema — and that
// distinction is the whole point: a flat offset hides inside a sample, and the
// old shape had both, which is how their sum came out looking close to right.
func TestRenderToolsEmitsTheGatewaysFunctionShape(t *testing.T) {
	schema := json.RawMessage(`{"name":"bash","description":"run","input_schema":{"type":"object",` +
		`"properties":{"cmd":{"type":"string"}}},"cache_control":{"type":"ephemeral"}}`)
	got := RenderPrompt("", []Message{{Role: RoleUser, Text: "hi"}}, []json.RawMessage{schema})

	want := `{"name": "bash", "description": "run", "parameters": {"type": "object", ` +
		`"properties": {"cmd": {"type": "string"}}}}`
	if !strings.Contains(got, want) {
		t.Fatalf("the rendered schema is not the gateway's function object.\nwant to contain: %s\n"+
			"the tools section was:\n%s", want, got)
	}
	if strings.Contains(got, `"input_schema"`) {
		t.Fatalf("the Anthropic key name survived into the rendered prompt — the gateway renames it")
	}
	// Member ORDER, asserted separately from the whole string so a future
	// spacing change reports as a spacing failure and not as an order failure.
	order := []string{`"name": "bash"`, `"description": "run"`, `"parameters":`}
	at := -1
	for _, piece := range order {
		i := strings.Index(got, piece)
		if i < 0 {
			t.Fatalf("the rendered schema does not contain %q", piece)
		}
		if i < at {
			t.Fatalf("the rendered schema reorders its members: %q appears before %q — the "+
				"client's order is what the endpoint counts", piece, order[0])
		}
		at = i
	}
}
