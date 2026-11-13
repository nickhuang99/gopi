package main

// tokens_count_endpoint_test.go — the arm that pins a COUNT to a number the
// ENDPOINT answered, at the smallest request that has one.
//
// The other arms in this package assert PROPERTIES (a delta, a shape, an
// identity against an independent recomputation). Those are the right guard for
// a renderer, but they are all blind to a FLAT offset that the render and the
// count share: `TestNoAdditiveTermInTheCount` recomputes the same definition
// from the same segments, so a rendering that is one marker short is counted
// consistently WRONG by both sides of that comparison and the arm stays green.
// A flat offset is exactly the defect this task closed (ours used to answer 6
// short at n=0), and it can only be seen against the server's own number.
//
// So this arm carries MEASURED endpoint values, not fitted ones. They are not
// constants in the counting path — nothing in `countRequest`,
// `PromptTokensWith` or `EncodeMasked` adds a term, which is separately pinned
// by TestNoAdditiveTermInTheCount — they are the expected ANSWER of three
// requests whose measured answer is known. The measurement is one command per
// body, and it is quoted so a reader can re-take it rather than trust this file:
//
//	GOPI_TOKENS_COUNT_URL=https://api.deepseek.com/anthropic/v1/messages/count_tokens \
//	GOPI_TOKENS_COUNT_KEY="$KEY" \
//	  gopi tool tokens --body <body.json> --compare
//
// Measured on 2026-11-03 against that endpoint (chat mode, `thinking:disabled`):
//
//	{} minimal, content ""                              -> 4
//	{} minimal, content "hi"                            -> 5
//	{system:"You are a helpful assistant.", "hi"}       -> 12
//
// The three rows are not one number repeated: they separate the FRAME from the
// content by 1 token (empty vs "hi") and the system turn by 7 (5 -> 12), so a
// render that drops a marker, doubles one, or pays an unconditional constant
// moves exactly one of them and leaves the pattern broken.

import (
	"strings"
	"testing"
)

// endpointPinnedRows are the bodies whose count the endpoint answered, with the
// answer beside each. Every body is spelled in full so a reader can pipe it to
// the endpoint without reconstructing it from parts.
var endpointPinnedRows = []struct {
	name   string
	body   string
	server int
	// render is the exact string the template produces for this request. It is
	// asserted separately from the count because a count can be right for the
	// wrong reason (two errors cancelling), while the bytes cannot.
	render string
}{
	{
		name:   "empty content",
		body:   `{"model":"deepseek-chat","messages":[{"role":"user","content":""}],"thinking":{"type":"disabled"}}`,
		server: 4,
		render: "<\uff5cbegin\u2581of\u2581sentence\uff5c><\uff5cUser\uff5c><\uff5cAssistant\uff5c></think>",
	},
	{
		name:   "one word",
		body:   `{"model":"deepseek-chat","messages":[{"role":"user","content":"hi"}],"thinking":{"type":"disabled"}}`,
		server: 5,
		render: "<\uff5cbegin\u2581of\u2581sentence\uff5c><\uff5cUser\uff5c>hi<\uff5cAssistant\uff5c></think>",
	},
	{
		name:   "with a system prompt",
		body:   `{"model":"deepseek-chat","system":"You are a helpful assistant.","messages":[{"role":"user","content":"hi"}],"thinking":{"type":"disabled"}}`,
		server: 12,
		render: "<\uff5cbegin\u2581of\u2581sentence\uff5c><\uff5cSystem\uff5c>You are a helpful assistant." +
			"<\uff5cUser\uff5c>hi<\uff5cAssistant\uff5c></think>",
	},
}

// TestTokenCountMatchesTheEndpointsMeasuredValues is the arm the epic's
// acceptance names: the count for the smallest request must BE the number the
// endpoint answered. It fails on a flat offset by one token, which no arm in
// this package asserted before (see the file comment) — the pre-fix tree
// answered 12 here only after the tools preamble and the V4.1 tokenizer data
// landed; on `be3be388` the same body answered 6 short.
func TestTokenCountMatchesTheEndpointsMeasuredValues(t *testing.T) {
	for _, row := range endpointPinnedRows {
		t.Run(row.name, func(t *testing.T) {
			if got := renderFixture(t, row.body); got != row.render {
				t.Fatalf("the rendered bytes are not the template's:\n got %q\nwant %q", got, row.render)
			}
			if got := countFixture(t, row.body); got != row.server {
				t.Fatalf("the count is %d, the endpoint answered %d — a flat offset of %+d tokens "+
					"at the smallest request (this is the shape the tools-preamble gap had)\nbody: %s",
					got, row.server, got-row.server, row.body)
			}
		})
	}
}

// TestTokenCountSeparatesFrameFromContent pins the STRUCTURE of the three rows
// above rather than their absolute values: the empty-content request and the
// one-word request differ by exactly the content's own cost (1 token), and the
// system turn costs the same whether it is present in both or neither. A render
// that pays an unconditional term satisfies the absolute rows only until the
// endpoint changes shape; this arm says why the rows can be trusted to move
// together.
func TestTokenCountSeparatesFrameFromContent(t *testing.T) {
	empty := countFixture(t, endpointPinnedRows[0].body)
	oneWord := countFixture(t, endpointPinnedRows[1].body)
	withSystem := countFixture(t, endpointPinnedRows[2].body)

	if d := oneWord - empty; d != endpointPinnedRows[1].server-endpointPinnedRows[0].server {
		t.Fatalf("content \"hi\" moved the count by %d, the endpoint charges %d — the frame is absorbing "+
			"what the content costs", d, endpointPinnedRows[1].server-endpointPinnedRows[0].server)
	}
	if d := withSystem - oneWord; d != endpointPinnedRows[2].server-endpointPinnedRows[1].server {
		t.Fatalf("a system prompt moved the count by %d, the endpoint charges %d", d,
			endpointPinnedRows[2].server-endpointPinnedRows[1].server)
	}
	// And no body's render may be a prefix of another's by accident of a
	// dropped marker: every row carries its own BOS + transition pair.
	for _, row := range endpointPinnedRows {
		if n := strings.Count(row.render, "<\uff5cbegin\u2581of\u2581sentence\uff5c>"); n != 1 {
			t.Fatalf("%s: %d BOS markers in the render, want 1", row.name, n)
		}
		if n := strings.Count(row.render, "<\uff5cAssistant\uff5c>"); n != 1 {
			t.Fatalf("%s: %d assistant transition markers in the render, want 1", row.name, n)
		}
	}
}
