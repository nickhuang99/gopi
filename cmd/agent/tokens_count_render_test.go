package main

// tokens_count_render_test.go — the arms that pin `gopi tool tokens` to the
// OFFICIAL V4.1 chat template.
//
// THE DEFECT THIS FILE REPLACED, kept because it is the reason each arm below
// asserts a PROPERTY rather than a number. The old renderer translated the V3
// template and never rendered `requestBody.Tools` at all: measured on a real
// chat request (messages=3, tools=18, server 58,570) it answered 46,479 and was
// COMPLETELY IMMUNE to the tool count — truncating `tools` to 0, 1, 2, 5 or 18
// entries left it at 46,479 while the server moved 46,953 → 58,570. On top of
// that it carried four FITTED constants (`requestFramingOverhead` 28,
// `toolsSectionOverhead` 200, `promptOverheadTokens` 161,
// `promptPerMessageTokens` 16.5) and a per-marker cost table, which together
// absorbed whatever the render got wrong. Those are gone: this file's arms fail
// if one comes back, and the numbers it quotes are MEASURED against the live
// endpoint rather than remembered.
//
// The count is now `sum over segments of (1 for a template marker, BPE for the
// text)`. Every arm here is a statement about that structure.

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/nickhuang99/gopi/internal/tokenizer"
)

// renderFixtureMessages is the conversation both tool arms share: one user turn,
// so a difference between two renderings can only come from the field under test.
const renderFixtureMessages = `[{"role":"user","content":"hello"}]`

// renderFixtureTools is one tool schema, written in the Anthropic shape the
// client actually sends, with the whitespace a JSON encoder would emit.
const renderFixtureTools = `[{"name": "read","description": "read a file","input_schema": {"type": "object","properties": {"path": {"type": "string"}}}}]`

// countFixture renders and counts one body, failing the test on a render error.
func countFixture(t *testing.T, body string) int {
	t.Helper()
	var b requestBody
	if err := json.Unmarshal([]byte(body), &b); err != nil {
		t.Fatalf("fixture is not a request body: %v", err)
	}
	n, err := countRequest(&b)
	if err != nil {
		t.Fatalf("countRequest: %v", err)
	}
	return n
}

// renderFixture returns the rendered prompt of one body — the bytes the endpoint
// tokenizes — for the arms that assert SHAPE rather than a count.
func renderFixture(t *testing.T, body string) string {
	t.Helper()
	var b requestBody
	if err := json.Unmarshal([]byte(body), &b); err != nil {
		t.Fatalf("fixture is not a request body: %v", err)
	}
	schemas, err := toolList(b.Tools)
	if err != nil {
		t.Fatalf("toolList: %v", err)
	}
	msgs, err := tokenizerMessages(&b)
	if err != nil {
		t.Fatalf("tokenizerMessages: %v", err)
	}
	return tokenizer.RenderPrompt(systemText(b.System), msgs, schemas)
}

// TestRenderPromptCountsTools is the arm that was RED before the original fix and
// would be RED again if the schema block were dropped from the V4.1 translation:
// the messages are byte-identical and only `tools` varies.
func TestRenderPromptCountsTools(t *testing.T) {
	withTools := `{"model":"deepseek-flash","messages":` + renderFixtureMessages + `,"tools":` + renderFixtureTools + `}`
	withoutTools := `{"model":"deepseek-flash","messages":` + renderFixtureMessages + `}`

	nTools := countFixture(t, withTools)
	nBare := countFixture(t, withoutTools)

	if nTools <= nBare {
		t.Fatalf("ours with tools = %d, without = %d — rendering tools must ADD tokens; an equal count means the tools field is not reaching the renderer (this is the original defect: it read %d for both)",
			nTools, nBare, nBare)
	}
	// The schema's own text plus the ~205-token preamble the template emits
	// (measured: the preamble alone is exactly 205 tokens). 100 is a floor well
	// below the measured contribution so a wording change in the fixture does not
	// make the arm flaky, and well above any marker's cost.
	const minToolContribution = 100
	if delta := nTools - nBare; delta < minToolContribution {
		t.Fatalf("tools contributed %d tokens, want >= %d — the schema block is not being rendered, only framing", delta, minToolContribution)
	}
}

// TestRenderPromptReSerializesTools pins that the schema is RE-SERIALIZED, so the
// whitespace it arrived with cannot reach the count. The two fixtures below are
// the same schema at two spacings.
func TestRenderPromptReSerializesTools(t *testing.T) {
	spaced := `{"model":"deepseek-flash","messages":` + renderFixtureMessages + `,"tools":` + renderFixtureTools + `}`
	tight := `{"model":"deepseek-flash","messages":` + renderFixtureMessages +
		`,"tools":[{"name":"read","description":"read a file","input_schema":{"type":"object","properties":{"path":{"type":"string"}}}}]}`

	if a, b := countFixture(t, spaced), countFixture(t, tight); a != b {
		t.Fatalf("the same schema spaced counted %d and compact counted %d — the wire's whitespace is reaching the count", a, b)
	}
	// And the rendered schema is the SPACED shape the endpoint's own count
	// matches: measured on the 18-tool request, this spacing 11,639 tokens
	// against the server's 11,617, while compact separators give 11,182 and
	// ASCII escaping gives 13,143.
	got := renderFixture(t, spaced)
	if !strings.Contains(got, `"name": "read"`) {
		t.Fatalf("the rendered schema is not in the measured `%q` shape:\n%s", `"name": "read"`, got)
	}
}

// TestRenderPromptCountsToolTraffic covers the tool-call half of the template.
// An assistant `tool_use` block renders through the DSML branch and a
// `tool_result` through the user-turn block join; either one dropped removes the
// tokens of a whole tool exchange from every count on any request that has one.
func TestRenderPromptCountsToolTraffic(t *testing.T) {
	const resultText = "the file contains forty two lines of plain prose"

	call := `{"role":"assistant","content":[{"type":"tool_use","id":"call_1","name":"read","input":{"path":"/tmp/x.txt"}}]}`
	result := `{"role":"user","content":[{"type":"tool_result","tool_use_id":"call_1","content":"` + resultText + `"}]}`
	plainReply := `{"role":"assistant","content":[{"type":"text","text":"ok"}]}`

	bare := `{"model":"deepseek-flash","messages":[{"role":"user","content":"hello"},` + plainReply + `]}`
	withCall := `{"model":"deepseek-flash","messages":[{"role":"user","content":"hello"},` + call + `]}`
	withResult := `{"model":"deepseek-flash","messages":[{"role":"user","content":"hello"},` + plainReply + `,` + result + `]}`

	nBare := countFixture(t, bare)
	nCall := countFixture(t, withCall)
	nResult := countFixture(t, withResult)

	if nCall <= nBare {
		t.Fatalf("ours with a tool_use block = %d, with a plain reply = %d — an assistant tool call must cost tokens (the DSML framing, the name and its arguments)",
			nCall, nBare)
	}
	if nResult <= nBare {
		t.Fatalf("ours with a tool_result = %d, with a plain reply = %d — a tool_result block must cost tokens",
			nResult, nBare)
	}
	if nResult-nBare < 10 {
		t.Fatalf("tool_result contributed %d tokens, want >= 10 — the payload text is not reaching the renderer", nResult-nBare)
	}
}

// TestRenderPromptToolTrafficShape asserts the BYTES the V4.1 template emits.
// The previous version of this arm asserted V3's `<｜tool▁call▁begin｜>` /
// ```json grammar, which V4.1 does not have; keeping that assertion would have
// held the renderer to a template the endpoint does not use.
func TestRenderPromptToolTrafficShape(t *testing.T) {
	call := `{"role":"assistant","content":[{"type":"tool_use","id":"call_1","name":"read","input":{"path":"/tmp/x.txt"}}]}`
	result := `{"role":"user","content":[{"type":"tool_result","tool_use_id":"call_1","content":"done"}]}`
	body := `{"model":"deepseek-flash","messages":[{"role":"user","content":"hello"},` + call + `,` + result + `]}`

	text := renderFixture(t, body)

	for _, want := range []struct{ name, marker string }{
		{"the tool-calls section opener", `<｜DSML｜ calls>`},
		{"an invoke opener", `<｜DSML｜ invoke name="read">`},
		{"a parameter opener", `<｜DSML｜ parameter name="path" string="true">/tmp/x.txt`},
		{"the assistant end marker", `<｜end▁of▁sentence｜>`},
		{"the tool output text", `done`},
	} {
		if !strings.Contains(text, want.marker) {
			t.Errorf("rendered prompt has no %s (%q):\n%s", want.name, want.marker, text)
		}
	}

	// The V3 grammar must NOT appear: it is a different template's markers and
	// charges different tokens.
	for _, bad := range []struct{ name, marker string }{
		{"a V3 tool-call marker", "<｜tool\u2581call\u2581begin｜>"},
		{"a V3 separator", "<｜tool\u2581sep｜>"},
		{"a V3 outputs section", "<｜tool\u2581outputs\u2581begin｜>"},
		{"a ```json fence around the arguments", "```json"},
	} {
		if strings.Contains(text, bad.marker) {
			t.Errorf("rendered prompt still carries %s (%q) — that is V3's grammar, not V4.1's", bad.name, bad.marker)
		}
	}

	// The section is opened ONCE per prompt, not once per call: a second
	// occurrence would mean per-call framing where the template emits one.
	if n := strings.Count(text, `<｜DSML｜ calls>`); n != 1 {
		t.Errorf("the tool-calls section appears %d times, want 1", n)
	}
	// The call's id is never rendered: the template writes the name and the
	// arguments, never the id.
	if strings.Contains(text, "call_1") {
		t.Errorf("rendered prompt carries the call id, which the template never emits — counting it inflates the total")
	}
}

// TestNoAdditiveTermInTheCount is the anti-regression arm for the four FITTED
// constants this change deleted. It measures the count against an INDEPENDENT
// recomputation of the same definition — segments, then one token per marker
// plus BPE for the text — so any term added in `countRequest` shows up as a
// difference. A constant that fits one server build is exactly what this
// prevents.
func TestNoAdditiveTermInTheCount(t *testing.T) {
	tk, err := tokenizer.Default()
	if err != nil {
		t.Fatalf("load tokenizer: %v", err)
	}
	bodies := []string{
		`{"model":"deepseek-flash","messages":` + renderFixtureMessages + `}`,
		`{"model":"deepseek-flash","messages":` + renderFixtureMessages + `,"tools":` + renderFixtureTools + `}`,
		`{"model":"deepseek-flash","system":"a system prompt","messages":` + renderFixtureMessages + `}`,
		`{"model":"deepseek-flash","messages":[{"role":"user","content":"a"},{"role":"user","content":"b"}]}`,
		`{"model":"deepseek-flash","messages":[{"role":"user","content":"a"},{"role":"assistant","content":"b"},{"role":"user","content":"c"}]}`,
	}
	for _, body := range bodies {
		var b requestBody
		if err := json.Unmarshal([]byte(body), &b); err != nil {
			t.Fatalf("fixture: %v", err)
		}
		schemas, err := toolList(b.Tools)
		if err != nil {
			t.Fatalf("toolList: %v", err)
		}
		msgs, err := tokenizerMessages(&b)
		if err != nil {
			t.Fatalf("tokenizerMessages: %v", err)
		}
		want := 0
		for _, s := range tokenizer.RenderSegmentsWith(systemText(b.System), msgs, schemas, promptOptions(&b)) {
			if s.Template {
				want += tk.CountTemplate(s.Text)
				continue
			}
			want += tk.CountText(s.Text)
		}
		if got := countFixture(t, body); got != want {
			t.Fatalf("countRequest = %d, the segment definition = %d — an additive term is back in the count\nbody: %s",
				got, want, body)
		}
	}
}

// TestEmptyToolsArrayIsNotAToolsSection pins that `"tools":[]` renders no
// section: the template's `if tools` is false for an empty list, and a section
// paid unconditionally would be a constant by another name.
func TestEmptyToolsArrayIsNotAToolsSection(t *testing.T) {
	without := `{"model":"deepseek-flash","messages":` + renderFixtureMessages + `}`
	empty := `{"model":"deepseek-flash","messages":` + renderFixtureMessages + `,"tools":[]}`

	if a, b := countFixture(t, without), countFixture(t, empty); a != b {
		t.Fatalf("`\"tools\":[]` counted %d and no `tools` field counted %d — an empty array is not a tools section", b, a)
	}
	if strings.Contains(renderFixture(t, empty), "### Available Tool Schemas") {
		t.Fatalf("an empty tools array rendered the schemas preamble")
	}
}

// TestSystemPromptRendersItsOwnMarker pins the system branch of the template: a
// system prompt opens with the System marker and closes with the Assistant
// transition marker, and a request WITHOUT one must not pay either.
func TestSystemPromptRendersItsOwnMarker(t *testing.T) {
	withSystem := renderFixture(t, `{"model":"deepseek-flash","system":"S","messages":`+renderFixtureMessages+`}`)
	if !strings.HasPrefix(withSystem, "<｜begin▁of▁sentence｜><｜System｜>S") {
		t.Fatalf("a system prompt did not open with BOS+System:\n%q", withSystem)
	}
	without := renderFixture(t, `{"model":"deepseek-flash","messages":`+renderFixtureMessages+`}`)
	if strings.Contains(without, "<｜System｜>") {
		t.Fatalf("a request with no system prompt rendered a System marker:\n%q", without)
	}
}

// TestConsecutiveUserTurnsMerge pins template step 1: consecutive user turns are
// ONE turn joined by a blank line. MEASURED against the endpoint: `hi` then `x`
// costs 7, and a single turn `"hi\n\nx"` also costs 7 — two separate turns would
// emit two User markers and a transition marker that the server does not charge.
func TestConsecutiveUserTurnsMerge(t *testing.T) {
	merged := renderFixture(t, `{"model":"deepseek-flash","messages":[{"role":"user","content":"hi"},{"role":"user","content":"x"}]}`)
	// The template's chat-mode generation header is `<｜Assistant｜></think>`.
	if want := "<｜begin▁of▁sentence｜><｜User｜>hi\n\nx<｜Assistant｜></think>"; merged != want {
		t.Fatalf("two consecutive user turns rendered\n got %q\nwant %q", merged, want)
	}
	if n := strings.Count(merged, "<｜User｜>"); n != 1 {
		t.Fatalf("two consecutive user turns emitted %d User markers, want 1", n)
	}
}
