package main

// tokens_count_pair_test.go — the regression guard for `gopi tool tokens --raw`
// on BOTH raw-log framings.
//
// THE DEFECT THIS PINS. `pairRawLog` used to pair by testing each line for the
// legacy marker text (`→ REQUEST`) and then reading the NEXT line as the body.
// The writer changed (commit 88f3d8ce6) to a length-prefixed framing —
// `<tag> <len>\n<payload>\n` — so the marker word stopped existing, the test
// matched nothing, and the tool reported `requests: 0` for every log the current
// writer produces (measured: a 592 KB framed session log carrying three request
// records answered `requests: 0, with a server number: 0`).
//
// WHY THE FIXTURES ARE BUILT THROUGH internal/rawlogfmt RATHER THAN PASTED
// LITERALS: the framing is that package's definition (Rules 13/14), so a fixture
// that hand-writes `R 42\n` would pass a test that the real writer might already
// disagree with. Building each fixture with the package's own encoder means the
// test breaks when the framing changes, which is exactly when a reader must be
// revisited.
//
// The two arms are separate on purpose: a fix that only restored the legacy path
// would pass one and fail the other, and that asymmetry is the whole bug.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nick/gopi/internal/rawlogfmt"
)

// The two fixtures' bodies. They are tiny on purpose (Rules: minimal sample) —
// what is under test is the PAIRING, not the tokenizer, and a body carrying one
// user message is enough to make the renderer produce a non-zero count.
const (
	pairFixtureRequestA = `{"model":"deepseek-v4-flash","max_tokens":16384,"messages":[{"role":"user","content":"first"}]}`
	pairFixtureRequestB = `{"model":"deepseek-v4-flash","max_tokens":16384,"messages":[{"role":"user","content":"second"}]}`
	// A non-streamed response body: a complete JSON object carrying a usage block.
	pairFixtureResponseA = `{"id":"msg_a","type":"message","usage":{"input_tokens":400,"cache_creation_input_tokens":20,"cache_read_input_tokens":5}}`
	// One streamed SSE event carrying the usage block, in the shape the endpoint
	// actually streams it (measured from a real log: message_delta carries usage).
	pairFixtureChunkA = "event: message_delta\ndata: {\"type\":\"message_delta\",\"usage\":{\"input_tokens\":900,\"cache_creation_input_tokens\":0,\"cache_read_input_tokens\":0}}\n"
)

// The prompt-side totals the fixtures' server blocks carry: the sum of the three
// usage fields (tokens_types.go's promptUsage.prompt), written out so a change to
// that fold is caught here too rather than silently agreeing with itself.
const (
	pairWantServerA = 425
	pairWantServerB = 900
)

// writePairFixture materialises a raw log in the FRAMED framing (what the writer
// produces for every new log).
func writePairFixture(t *testing.T, framed bool) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "raw.log")
	var buf []byte
	appendRecord := func(tag, annotation string, payload string) {
		if framed {
			buf = append(buf, rawlogfmt.EncodeAnnotated(tag, annotation, []byte(payload))...)
			return
		}
		rec, err := rawlogfmt.EncodeLegacy(tag, annotation, []byte(payload))
		if err != nil {
			t.Fatalf("legacy fixture: tag %q: %v", tag, err)
		}
		buf = append(buf, rec...)
	}

	// Request A, then a non-streamed response carrying its usage.
	appendRecord(rawlogfmt.TagRequest, "", pairFixtureRequestA)
	appendRecord(rawlogfmt.TagResponse, "", pairFixtureResponseA)
	// Request B, then a streamed chunk carrying its usage.
	appendRecord(rawlogfmt.TagRequest, "", pairFixtureRequestB)
	appendRecord(rawlogfmt.TagChunk, "", pairFixtureChunkA)

	if err := os.WriteFile(path, buf, 0o600); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	return path
}

// TestPairRawLogReadsFramedLogs is the arm that was RED before the fix: the
// length-prefixed framing the writer has produced since 88f3d8ce6.
func TestPairRawLogReadsFramedLogs(t *testing.T) {
	path := writePairFixture(t, true)

	format, err := rawlogfmt.Detect(path)
	if err != nil {
		t.Fatalf("detect: %v", err)
	}
	if format != rawlogfmt.FormatFramed {
		t.Fatalf("fixture framing = %v, want %v — the fixture must exercise the framed arm",
			format, rawlogfmt.FormatFramed)
	}

	got, err := pairRawLog(path)
	if err != nil {
		t.Fatalf("pairRawLog: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("paired %d requests, want 2 (percent: framed arms must pair; a zero here is the reported defect)", len(got))
	}
	for i, want := range []int{pairWantServerA, pairWantServerB} {
		if got[i].server != want {
			t.Errorf("request %d: server = %d, want %d", i, got[i].server, want)
		}
		if got[i].res.server != want {
			t.Errorf("request %d: row.server = %d, want %d (the row must carry the same number the pair holds)",
				i, got[i].res.server, want)
		}
		if got[i].res.ours <= 0 {
			t.Errorf("request %d: ours = %d, want > 0 (the body must reach the renderer)", i, got[i].res.ours)
		}
	}
}

// TestPairRawLogReadsLegacyLogs is the arm that must NOT regress: every log
// written before 88f3d8ce6, and every log a continued run keeps appending to in
// that file's own framing (rawlogfmt's "a file's framing is fixed at creation").
func TestPairRawLogReadsLegacyLogs(t *testing.T) {
	path := writePairFixture(t, false)

	format, err := rawlogfmt.Detect(path)
	if err != nil {
		t.Fatalf("detect: %v", err)
	}
	if format != rawlogfmt.FormatLegacy {
		t.Fatalf("fixture framing = %v, want %v — the fixture must exercise the legacy arm",
			format, rawlogfmt.FormatLegacy)
	}

	got, err := pairRawLog(path)
	if err != nil {
		t.Fatalf("pairRawLog: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("paired %d requests, want 2 (the legacy framing must stay readable)", len(got))
	}
	for i, want := range []int{pairWantServerA, pairWantServerB} {
		if got[i].server != want {
			t.Errorf("request %d: server = %d, want %d", i, got[i].server, want)
		}
	}
}

// TestPairRawLogReportsAnUnrenderableBody is the regression guard for the arm
// that used to discard the renderer's error: `ours, _ := countRequest(&b)` put a
// ZERO in the `ours` column whenever the body could not be rendered, and a zero
// there reads as a MEASUREMENT ("we count 0 tokens, the server counts 1000")
// rather than as "this body was never counted" — the silent-failure shape Rule 15
// forbids, in the one tool whose whole purpose is comparing measurements.
//
// The body carries `"tools": "oops"` — a string where the template expects an
// array. json.Unmarshal into requestBody SUCCEEDS (Tools is a json.RawMessage, so
// the bad value rides through untouched) and the failure surfaces inside
// renderPrompt, which is exactly the path the discarded error hid.
//
// Red before the fix: pairRawLog returned (rows, nil) with ours == 0.
func TestPairRawLogReportsAnUnrenderableBody(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "raw.log")
	var buf []byte
	buf = append(buf, rawlogfmt.EncodeAnnotated(rawlogfmt.TagRequest, "",
		[]byte(`{"model":"deepseek-v4-flash","messages":[{"role":"user","content":"x"}],"tools":"oops"}`))...)
	buf = append(buf, rawlogfmt.EncodeAnnotated(rawlogfmt.TagResponse, "",
		[]byte(pairFixtureResponseA))...)
	if err := os.WriteFile(path, buf, 0o600); err != nil {
		t.Fatalf("write fixture: %v", err)
	}

	_, err := pairRawLog(path)
	if err == nil {
		t.Fatalf("pairRawLog accepted a body whose rendering fails — it must report the failure, " +
			"not print a zero in the `ours` column (Rule 15)")
	}
	if !strings.Contains(err.Error(), "count request at line") {
		t.Errorf("error does not name the request it could not count: %v", err)
	}
}
