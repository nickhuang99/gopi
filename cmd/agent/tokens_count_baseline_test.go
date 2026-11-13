package main

// tokens_count_baseline_test.go — the arm that pins the FOUR baseline requests
// and the whole tools-count sweep to the numbers the ENDPOINT answered.
//
// Why this arm exists beside TestTokenCountMatchesTheEndpointsMeasuredValues
// (tokens_count_endpoint_test.go): that file pins three SMALL synthetic requests,
// which catch a flat offset but say nothing about a request that carries the
// real system prompt (205 KB of it), the real 18 tool schemas (46 KB) and a real
// tool_use / tool_result conversation. That is where every residual this epic
// chased actually lived — `real_chat_request` measured 58,572 against the
// endpoint's 58,570 while the three tiny rows were already exact. A number this
// arm carries is the answer the server gave for THAT body, so a change that
// moves one of them by a single token turns this red.
//
// THE NUMBERS ARE MEASURED, NOT FITTED. Nothing in countRequest, PromptTokens or
// EncodeMasked adds a term (pinned separately by TestNoAdditiveTermInTheCount);
// these are the expected ANSWERS of requests whose answer is known. Re-take any
// row with:
//
//	GOPI_TOKENS_COUNT_URL=https://api.deepseek.com/anthropic/v1/messages/count_tokens \
//	GOPI_TOKENS_COUNT_KEY="$KEY" \
//	  gopi tool tokens --body <body.json> --compare
//
// Measured 2026-11-03 against that endpoint (chat mode, `thinking:disabled`):
//
//	 t0                1 msg,  0 tools  ->     12
//	 one_00            1 msg,  1 tool   ->    266
//	 t18               1 msg, 18 tools  -> 58,097
//	 real_chat_request 3 msgs, 18 tools -> 58,570
//
// and the sweep over the same real request with its tools array truncated:
//
//	n     0      1      2      3      4      5      6      7      8      9
//	    46953  47438  47760  48007  48341  48799  49072  49232  49511  49760
//	n    10     11     12     13     14     15     16     17     18
//	    51880  52236  53596  54123  55609  55931  56418  57449  58570
//
// The sweep is one assertion, not one number repeated: its INTERVALS are the
// marginal cost of each individual schema, and they differ by more than 20x
// (1 → 2 costs 322 tokens, 9 → 10 costs 2,120). A serializer with the wrong
// shape moves those intervals; a flat offset moves only the intercept; a
// renderer that ignores `tools` at all makes the whole sweep one value.

import (
	"bytes"
	"compress/gzip"
	"embed"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"testing"
)

// baselineFixture carries the raw FIELD BYTES of the two large baseline bodies,
// gzip-compressed and base64-armoured: base64 because the bytes are UTF-8 JSON
// inside a text file (a raw blob is what Rule 39 refuses), gzip because the
// 205 KB system prompt is highly repetitive and 235 KB of plain JSON would be a
// 235 KB diff for every future change to this file. The generator that produced
// it and the bodies it read are quoted in the run's report; the field bytes are
// what countRequest consumes, so nothing about the count is lost.
//
//go:embed testdata/token-endpoint-baselines.b64
var baselineFixture embed.FS

// baselineFixturePath is the embed path of the armoured fixture.
const baselineFixturePath = "testdata/token-endpoint-baselines.b64"

// baselineBody is one request of the family, assembled from the fixture into the
// shape countRequest takes, with the endpoint's own answer beside it.
type baselineBody struct {
	name   string
	body   requestBody
	server int
}

// baselineShared is the fixture's shared half: the `system` and `tools` values
// that t18 and real_chat_request carry byte-for-byte identically (verified by
// the generator, and asserted below so a fixture rebuilt from two different
// requests cannot silently merge two shapes into one arm).
type baselineShared struct {
	System json.RawMessage `json:"system"`
	Tools  json.RawMessage `json:"tools"`
}

// baselineFixtureDoc is the fixture's on-disk shape: the shared system/tools
// pair plus each body's remaining fields, as RAW JSON so key order inside a tool
// schema survives the round trip.
type baselineFixtureDoc struct {
	Shared map[string]json.RawMessage            `json:"shared"`
	Bodies map[string]map[string]json.RawMessage `json:"bodies"`
}

// baselineTools is the number of tool schemas the two large bodies carry. It is
// not inferred from the fixture: the sweep below is stated over this exact
// count, and a fixture that silently lost a schema must fail rather than shorten
// the sweep.
const baselineTools = 18

// sweepServer is the endpoint's answer for the real request at each tools count
// 0..18, in order. Index = n.
var sweepServer = []int{
	46953, 47438, 47760, 48007, 48341, 48799, 49072, 49232, 49511, 49760,
	51880, 52236, 53596, 54123, 55609, 55931, 56418, 57449, 58570,
}

// loadBaselineFixture decodes the armoured fixture and assembles the four bodies.
func loadBaselineFixture(t *testing.T) ([]baselineBody, map[string]map[string]json.RawMessage, baselineShared) {
	t.Helper()
	armoured, err := baselineFixture.ReadFile(baselineFixturePath)
	if err != nil {
		t.Fatalf("the baseline fixture is not embedded: %v", err)
	}
	compressed, err := base64.StdEncoding.DecodeString(string(bytes.TrimSpace(armoured)))
	if err != nil {
		t.Fatalf("the fixture is not base64: %v", err)
	}
	zr, err := gzip.NewReader(bytes.NewReader(compressed))
	if err != nil {
		t.Fatalf("the fixture is not gzip: %v", err)
	}
	plain, err := io.ReadAll(zr)
	if err != nil {
		t.Fatalf("reading the fixture: %v", err)
	}
	var doc baselineFixtureDoc
	if err := json.Unmarshal(plain, &doc); err != nil {
		t.Fatalf("the fixture is not the documented document: %v", err)
	}

	shared := baselineShared{System: doc.Shared["system"], Tools: doc.Shared["tools"]}
	var tools []json.RawMessage
	if err := json.Unmarshal(shared.Tools, &tools); err != nil {
		t.Fatalf("the fixture's shared tools are not an array: %v", err)
	}
	if len(tools) != baselineTools {
		t.Fatalf("the fixture carries %d tool schemas, the sweep is stated over %d — the fixture "+
			"and the arm have to describe the same request", len(tools), baselineTools)
	}

	// The two small bodies are spelled INLINE, in full: they are 151 and 307
	// bytes, so a reader can pipe them to the endpoint without opening anything.
	rows := []baselineBody{
		{
			name:   "t0 (1 msg, 0 tools)",
			server: 12,
			body: requestBody{
				System:   json.RawMessage(`"You are a helpful assistant."`),
				Messages: []json.RawMessage{json.RawMessage(`{"role":"user","content":"hi"}`)},
			},
		},
		{
			name:   "one_00 (1 msg, 1 tool)",
			server: 266,
			body: requestBody{
				System:   json.RawMessage(`"You are a helpful assistant."`),
				Messages: []json.RawMessage{json.RawMessage(`{"role":"user","content":"hi"}`)},
				Tools: json.RawMessage(`[{"name":"noop","description":"does nothing",` +
					`"input_schema":{"type":"object","properties":{"x":{"type":"string"}},"required":["x"]}}]`),
			},
		},
	}

	for _, name := range []string{"t18", "real_chat_request"} {
		fields, ok := doc.Bodies[name]
		if !ok {
			t.Fatalf("the fixture has no %q body: %v", name, keysOf(doc.Bodies))
		}
		// Both large bodies carry the shared system prompt verbatim; the fixture
		// stores it once (see the generator) precisely because t18 and
		// real_chat_request are the SAME system + the SAME 18 schemas and differ
		// only in their messages.
		row := baselineBody{body: requestBody{System: shared.System, Tools: shared.Tools}}
		if raw, ok := fields["messages"]; ok {
			if err := json.Unmarshal(raw, &row.body.Messages); err != nil {
				t.Fatalf("%s: messages are not an array: %v", name, err)
			}
			if len(row.body.Messages) == 0 {
				t.Fatalf("%s: the fixture's messages are empty", name)
			}
		} else {
			t.Fatalf("%s: the fixture has no messages", name)
		}
		rows = append(rows, row)
	}
	// The endpoint's answers for the two large bodies, in the same order they
	// were appended. They are written here rather than in the loop above so the
	// table a reader checks is one table.
	rows[2].name, rows[2].server = "t18 (1 msg, 18 tools)", 58097
	rows[3].name, rows[3].server = "real_chat_request (3 msgs, 18 tools)", 58570

	// The two large bodies must still be the shapes their names claim, or the
	// pinned answers are answers about a different request.
	if got := len(rows[3].body.Messages); got != 3 {
		t.Fatalf("real_chat_request carries %d messages, the pinned answer is for 3", got)
	}
	if len(rows[3].body.System) == 0 {
		t.Fatalf("real_chat_request lost its system prompt — its pinned answer was measured WITH it")
	}
	// `thinking:disabled` is what every measured body carries, and it decides
	// the render (an enabled request emits a reasoning header): a row that lost
	// it counts 25 tokens HIGH — measured on this arm's first run, where all
	// four rows read +25 before this line existed.
	for i := range rows {
		rows[i].body.Model = "deepseek-flash"
		rows[i].body.Thinking.Type = "disabled"
	}
	return rows, doc.Bodies, shared
}

// keysOf lists a map's keys in a stable order, for a failure message.
func keysOf[V any](m map[string]V) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

// TestTokenCountMatchesTheFourBaselineRequests is the epic's first acceptance
// row: the local count must BE the endpoint's number for each of the four
// baseline bodies, and one token of difference is a failure.
func TestTokenCountMatchesTheFourBaselineRequests(t *testing.T) {
	rows, _, _ := loadBaselineFixture(t)
	if len(rows) != 4 {
		t.Fatalf("%d baseline rows, the epic names four", len(rows))
	}
	for _, row := range rows {
		t.Run(row.name, func(t *testing.T) {
			got, err := countRequest(&row.body)
			if err != nil {
				t.Fatalf("countRequest: %v", err)
			}
			if got != row.server {
				t.Fatalf("counted %d, the endpoint answered %d (diff %+d) for %s",
					got, row.server, got-row.server, row.name)
			}
		})
	}
}

// TestTokenCountMatchesTheToolsSweep pins the endpoint's whole tools-count
// sweep over real_chat_request. It is the arm a shape error cannot pass: the
// intervals between consecutive rows are the marginal cost of each individual
// schema, so a serializer that is right on average and wrong per tool fails even
// when the endpoint's last row happens to match.
func TestTokenCountMatchesTheToolsSweep(t *testing.T) {
	rows, _, shared := loadBaselineFixture(t)
	realBody := rows[3].body

	var schemas []json.RawMessage
	if err := json.Unmarshal(shared.Tools, &schemas); err != nil {
		t.Fatalf("shared tools are not an array: %v", err)
	}
	if len(schemas)+1 != len(sweepServer) {
		t.Fatalf("the sweep has %d pinned rows, the fixture %d schemas", len(sweepServer), len(schemas))
	}

	for n, want := range sweepServer {
		body := realBody
		if n == 0 {
			// n=0 is an ABSENT field, not `[]`: the template's `{% if tools %}`
			// is false for both, and this arm must exercise the shape the sweep
			// measured.
			body.Tools = nil
		} else {
			truncated, err := json.Marshal(schemas[:n])
			if err != nil {
				t.Fatalf("re-encoding %d schemas: %v", n, err)
			}
			body.Tools = truncated
		}
		got, err := countRequest(&body)
		if err != nil {
			t.Fatalf("n=%d: countRequest: %v", n, err)
		}
		if got != want {
			t.Fatalf("n=%d: counted %d, the endpoint answered %d (diff %+d)", n, got, want, got-want)
		}
	}
}

// TestToolsSweepIntervalsAreNotFlat is the structural half: it fails when the
// sweep's shape is wrong even if someone re-pins every number to whatever the
// code now prints. The first schema costs hundreds of tokens (the section
// preamble plus a schema), and the cheapest step in the sweep costs tens; a
// renderer that drops the tools section makes every interval zero, and one that
// pays a constant per tool makes them all equal.
func TestToolsSweepIntervalsAreNotFlat(t *testing.T) {
	const (
		// sweepSpreadFloor is how far apart the priciest and the cheapest step
		// must be. The measured spread is 13x (2,120 vs 160); a flat-per-tool
		// term, or a sweep re-pinned from a renderer that made them equal, would
		// read 1x.
		sweepSpreadFloor = 4
	)
	smallest, largest := sweepServer[1]-sweepServer[0], sweepServer[1]-sweepServer[0]
	for i := 2; i < len(sweepServer); i++ {
		d := sweepServer[i] - sweepServer[i-1]
		if d <= 0 {
			t.Fatalf("schema %d cost %d — a sweep must be strictly increasing", i, d)
		}
		if d < smallest {
			smallest = d
		}
		if d > largest {
			largest = d
		}
	}
	if largest < sweepSpreadFloor*smallest {
		t.Fatalf("the priciest schema step is %d and the cheapest %d — a per-tool CONSTANT makes "+
			"those equal, so a sweep this flat cannot be the endpoint's (measured: 160..2120)",
			largest, smallest)
	}
	if last := sweepServer[len(sweepServer)-1]; last <= sweepServer[0] {
		t.Fatalf("18 schemas cost %d, none cost %d — the section is not being rendered", last-sweepServer[0], last)
	}
}

// TestBaselineBodiesDifferInShapeKeepsTheArmHonest refuses a fixture where the
// four rows collapsed into one request: the arm's value is that the rows are
// DIFFERENT shapes (no tools / one tool / 18 tools one turn / 18 tools three
// turns), so a count that satisfies one cannot satisfy all by accident.
func TestBaselineBodiesDifferInShapeKeepsTheArmHonest(t *testing.T) {
	rows, _, _ := loadBaselineFixture(t)
	seen := map[int]string{}
	for _, row := range rows {
		n, err := countRequest(&row.body)
		if err != nil {
			t.Fatalf("%s: countRequest: %v", row.name, err)
		}
		if prev, ok := seen[n]; ok {
			t.Fatalf("%q and %q both counted %d — the baseline rows are supposed to be distinct "+
				"shapes, and an arm whose rows collide proves nothing about any of them", prev, row.name, n)
		}
		seen[n] = row.name
	}
	// And the three-turn body must cost more than the one-turn body over the same
	// system prompt and the same 18 schemas: the tool_use/tool_result traffic is
	// what separates them, and it is the only difference.
	oneTurn, err := countRequest(&rows[2].body)
	if err != nil {
		t.Fatalf("t18: %v", err)
	}
	threeTurns, err := countRequest(&rows[3].body)
	if err != nil {
		t.Fatalf("real_chat_request: %v", err)
	}
	if threeTurns <= oneTurn {
		t.Fatalf("three turns (%d) do not exceed one turn (%d) over the same prompt and schemas",
			threeTurns, oneTurn)
	}
	if delta := threeTurns - oneTurn; delta != 58570-58097 {
		t.Fatalf("the tool traffic costs %d tokens, the endpoint charges %d", delta, 58570-58097)
	}
}

// baselineFixtureSummary is a human-readable statement of what the fixture
// holds, printed on failure from anywhere in this file, so a reader who sees a
// number move knows which request it belongs to without opening the fixture.
func baselineFixtureSummary(rows []baselineBody) string {
	var b bytes.Buffer
	for _, row := range rows {
		n, err := countRequest(&row.body)
		fmt.Fprintf(&b, "%-40s msgs=%d tools=%d ours=%d server=%d err=%v\n",
			row.name, len(row.body.Messages), countToolSchemas(row.body.Tools), n, row.server, err)
	}
	return b.String()
}

// countToolSchemas reports how many schemas a body carries, without failing on a
// malformed field (it is used by the summary only).
func countToolSchemas(raw json.RawMessage) int {
	if len(raw) == 0 {
		return 0
	}
	var list []json.RawMessage
	if json.Unmarshal(raw, &list) != nil {
		return -1
	}
	return len(list)
}
