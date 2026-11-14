package deepseek

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/nickhuang99/gopi/internal/rawlogfmt"
)

// streamResponseFixture is a SMALL but COMPLETE Anthropic-style stream: a
// thinking block, a text block, a tool_use block whose input arrives as
// input_json_delta fragments, a message_delta carrying the final usage, and the
// closing message_stop. It is deliberately short enough to read in one screen and
// complete enough that every field the recorded body must carry is exercised at
// least once.
//
// The usage values are chosen so the four counters are distinct — a body that
// dropped cache_read or cache_creation would still look plausible if they were
// equal to input_tokens.
const streamResponseFixture = `event: message_start
data: {"type":"message_start","message":{"id":"msg_fixture_01","type":"message","role":"assistant","model":"deepseek-v4-flash","content":[],"stop_reason":null,"stop_sequence":null,"usage":{"input_tokens":109,"cache_creation_input_tokens":7,"cache_read_input_tokens":256,"output_tokens":0,"service_tier":"standard"}}}

event: content_block_start
data: {"type":"content_block_start","index":0,"content_block":{"type":"thinking","thinking":"","signature":""}}

event: content_block_delta
data: {"type":"content_block_delta","index":0,"delta":{"type":"thinking_delta","thinking":"pondering"}}

event: content_block_delta
data: {"type":"content_block_delta","index":0,"delta":{"type":"signature_delta","signature":"sig-fixture"}}

event: content_block_stop
data: {"type":"content_block_stop","index":0}

event: content_block_start
data: {"type":"content_block_start","index":1,"content_block":{"type":"text","text":""}}

event: content_block_delta
data: {"type":"content_block_delta","index":1,"delta":{"type":"text_delta","text":"hello "}}

event: content_block_delta
data: {"type":"content_block_delta","index":1,"delta":{"type":"text_delta","text":"world"}}

event: content_block_stop
data: {"type":"content_block_stop","index":1}

event: content_block_start
data: {"type":"content_block_start","index":2,"content_block":{"type":"tool_use","id":"call_fixture","name":"read","input":{}}}

event: content_block_delta
data: {"type":"content_block_delta","index":2,"delta":{"type":"input_json_delta","partial_json":"{\"path\":"}}

event: content_block_delta
data: {"type":"content_block_delta","index":2,"delta":{"type":"input_json_delta","partial_json":"\"/tmp/x.txt\"}"}}

event: content_block_stop
data: {"type":"content_block_stop","index":2}

event: message_delta
data: {"type":"message_delta","delta":{"stop_reason":"tool_use","stop_sequence":null},"usage":{"input_tokens":109,"cache_creation_input_tokens":7,"cache_read_input_tokens":256,"output_tokens":76,"service_tier":"standard"}}

event: message_stop
data: {"type":"message_stop"}
`

// runStreamingFixture drives the REAL streaming parse path over transcript and
// returns every record the client's raw-log callback received.
func runStreamingFixture(t *testing.T, transcript string) []rawlogfmt.Record {
	t.Helper()
	var tags []string
	var payloads [][]byte
	var annotations []string
	client := &Client{}
	client.RawLog = func(tag, annotation string, data []byte) {
		tags = append(tags, tag)
		annotations = append(annotations, annotation)
		payloads = append(payloads, append([]byte(nil), data...))
	}
	ch := make(chan ClaudeStreamEvent, 32)
	drained := make(chan struct{})
	go func() {
		defer close(drained)
		for range ch { //nolint:revive // draining: this test asserts the RECORD, not the events
		}
	}()
	client.parseClaudeSSE(context.Background(), io.NopCloser(strings.NewReader(transcript)), ch)
	<-drained

	records := make([]rawlogfmt.Record, 0, len(tags))
	for i := range tags {
		records = append(records, rawlogfmt.Record{Tag: tags[i], Annotation: annotations[i], Payload: payloads[i]})
	}
	return records
}

// TestStreamWritesOneCompleteResponseRecord is the regression guard for bug
// 01a19bf1-ff52: a streamed call used to record ONLY chunks, so a reader could
// not pair one request with one response and `usage` was recoverable only by
// scanning the chunk sequence for message_start.
//
// It asserts the two properties the fix must have: exactly ONE TagResponse record,
// and a body from which `usage` is directly readable — the same shape a
// non-streaming response has, so one reader handles both.
func TestStreamWritesOneCompleteResponseRecord(t *testing.T) {
	records := runStreamingFixture(t, streamResponseFixture)

	var responses []rawlogfmt.Record
	for _, r := range records {
		if r.Tag == rawlogfmt.TagResponse {
			responses = append(responses, r)
		}
	}
	if len(responses) != 1 {
		t.Fatalf("expected exactly 1 %q (complete response) record, got %d — tags: %v",
			rawlogfmt.TagResponse, len(responses), recordTags(records))
	}
	resp := responses[0]
	if resp.Annotation != rawlogfmt.StreamResponseAnnotation {
		t.Errorf("response record annotation = %q, want %q (a synthesized stream body must say so)",
			resp.Annotation, rawlogfmt.StreamResponseAnnotation)
	}

	// IT MUST FOLLOW its own request's chunks: a reader pairing sequentially
	// reads R, C…C, S. The last record of the run is therefore the response.
	if got, want := records[len(records)-1].Tag, rawlogfmt.TagResponse; got != want {
		t.Fatalf("last record tag = %q, want %q — the response record must close the stream, tags: %v",
			got, want, recordTags(records))
	}

	// The body is the NON-STREAMING shape — that is the whole point: one reader
	// handles both paths, with usage at the top level.
	var wire struct {
		ID         string               `json:"id"`
		Content    []ClaudeContentBlock `json:"content"`
		StopReason string               `json:"stop_reason"`
		Usage      *ClaudeUsage         `json:"usage"`
	}
	if err := json.Unmarshal(resp.Payload, &wire); err != nil {
		t.Fatalf("response record body is not valid JSON: %v\nbody: %s", err, resp.Payload)
	}
	if wire.Usage == nil {
		t.Fatalf("response record carries no top-level usage — a reader cannot pair it with a request\nbody: %s", resp.Payload)
	}
	if wire.ID != "msg_fixture_01" {
		t.Errorf("id = %q, want msg_fixture_01", wire.ID)
	}
	if wire.StopReason != "tool_use" {
		t.Errorf("stop_reason = %q, want tool_use", wire.StopReason)
	}

	// THE USAGE IS THE SERVER'S, and the three prompt-side terms sum to the
	// number the server reported (109 + 7 + 256 = 372). Hard-coded rather than
	// re-derived from the fixture constant so a change to the fixture cannot
	// silently move the assertion too.
	want := ClaudeUsage{InputTokens: 109, CacheCreationInputTokens: 7, CacheReadInputTokens: 256, OutputTokens: 76}
	if *wire.Usage != want {
		t.Errorf("usage = %+v, want %+v (message_start fills the prompt side, message_delta the final counters)", *wire.Usage, want)
	}
	if got, wantSum := wire.Usage.PromptSideTokens(), 372; got != wantSum {
		t.Errorf("prompt-side sum = %d, want %d (input + cache_creation + cache_read)", got, wantSum)
	}

	// The content is FOLDED, not replayed: the streamed deltas must appear as
	// whole blocks, in wire order, with the tool call's input ASSEMBLED from its
	// fragments (ClaudeContentBlock drops InputJSON when marshaling, so an
	// unfolded record would say the tool took no arguments).
	if len(wire.Content) != 3 {
		t.Fatalf("content blocks = %d, want 3 (thinking, text, tool_use): %s", len(wire.Content), resp.Payload)
	}
	if wire.Content[0].Type != "thinking" || wire.Content[0].Thinking != "pondering" || wire.Content[0].Signature != "sig-fixture" {
		t.Errorf("thinking block not folded: %+v", wire.Content[0])
	}
	if wire.Content[1].Type != "text" || wire.Content[1].Text != "hello world" {
		t.Errorf("text block not folded: %+v", wire.Content[1])
	}
	tool := wire.Content[2]
	if tool.Type != "tool_use" || tool.Name != "read" {
		t.Errorf("tool_use block not folded: %+v", tool)
	}
	if tool.Input["path"] != "/tmp/x.txt" {
		t.Errorf("tool_use input = %v, want {\"path\":\"/tmp/x.txt\"} (assembled from input_json_delta)", tool.Input)
	}
}

// TestStreamChunkRecordsAreUnchanged pins the OTHER half of the contract: the
// fix added a record, it did not rewrite the wire evidence. The chunks must
// still be exactly what the server sent, so a replay or a byte audit is
// unaffected.
func TestStreamChunkRecordsAreUnchanged(t *testing.T) {
	records := runStreamingFixture(t, streamResponseFixture)
	var chunks []string
	for _, r := range records {
		if r.Tag == rawlogfmt.TagChunk {
			chunks = append(chunks, string(r.Payload))
		}
	}
	// One record per SSE event in the fixture — 15 of them (a stream of 15
	// events must produce 15 chunk records and no more).
	if len(chunks) != 15 {
		t.Fatalf("chunk records = %d, want 15 (one per SSE event); tags: %v", len(chunks), recordTags(records))
	}
	// Verbatim: the first chunk carries the fixture's own bytes, prefixes and all.
	if !strings.Contains(chunks[0], "event: message_start") || !strings.Contains(chunks[0], "msg_fixture_01") {
		t.Errorf("first chunk is not the verbatim SSE event: %q", chunks[0])
	}
}

// TestStreamWithNoMessageWritesNoResponseRecord pins the honesty floor: a stream
// that never announced a message produced no response, so inventing a body for it
// would claim something that did not happen. (A connection that is closed before
// the first event is the realistic shape.)
func TestStreamWithNoMessageWritesNoResponseRecord(t *testing.T) {
	records := runStreamingFixture(t, "")
	for _, r := range records {
		if r.Tag == rawlogfmt.TagResponse {
			t.Fatalf("a stream that never announced a message must write no response record, got %q", r.Payload)
		}
	}
}

// TestStreamBrokenMidwayWritesAPartialResponseRecord pins the truncated case:
// a stream that stops BEFORE the server signs off still yields a record (the
// usage it did send is real) but one a reader can tell apart from a whole body.
//
// THE CUT IS IN THE FIXTURE, not in a failing reader, and that is the honest
// shape: a connection that dies mid-stream ends in the SAME clean io.EOF a
// finished stream does (scanSSEEvents flushes the buffered partial event and
// returns nil), so no error is available to distinguish them. The witness is the
// missing message_stop, which is why the accumulator watches for it.
func TestStreamBrokenMidwayWritesAPartialResponseRecord(t *testing.T) {
	truncated := streamResponseFixture[:strings.Index(streamResponseFixture, "event: message_delta")]
	if strings.Contains(truncated, "message_stop") {
		t.Fatal("fixture cut failed: the truncated transcript still carries message_stop")
	}
	records := runStreamingFixture(t, truncated)

	var responses []rawlogfmt.Record
	for _, r := range records {
		if r.Tag == rawlogfmt.TagResponse {
			responses = append(responses, r)
		}
	}
	if len(responses) != 1 {
		t.Fatalf("expected 1 response record from a truncated stream, got %d; tags: %v", len(responses), recordTags(records))
	}
	wantAnn := rawlogfmt.StreamResponseAnnotation + rawlogfmt.StreamResponsePartialSuffix
	if responses[0].Annotation != wantAnn {
		t.Errorf("annotation = %q, want %q (a stream that never signed off must not claim a whole body)",
			responses[0].Annotation, wantAnn)
	}
	var wire struct {
		Usage *ClaudeUsage `json:"usage"`
	}
	if err := json.Unmarshal(responses[0].Payload, &wire); err != nil {
		t.Fatalf("partial record body is not valid JSON: %v", err)
	}
	if wire.Usage == nil || wire.Usage.PromptSideTokens() == 0 {
		t.Errorf("partial record lost the usage the server did send: %s", responses[0].Payload)
	}
}

// recordTags renders the tag sequence of a record list, for failure messages.
func recordTags(records []rawlogfmt.Record) []string {
	tags := make([]string, 0, len(records))
	for _, r := range records {
		if r.Annotation != "" {
			tags = append(tags, r.Tag+"("+r.Annotation+")")
			continue
		}
		tags = append(tags, r.Tag)
	}
	return tags
}

// TestStreamThroughTheRealClientWritesAReadableResponseRecord is the END-TO-END
// guard (Rule 31: the surface is EXECUTED, not just unit-called). It drives the
// real HTTP client against a real SSE server with a real RawLogger writing a real
// file, then READS THE FILE BACK through rawlogfmt — so the assertion is made on
// the bytes a reader will actually parse, not on a callback's arguments.
//
// This is the acceptance criterion in executable form: for each streamed request
// the log carries one S record after its chunks, and that record's `usage` is the
// server's own numbers.
func TestStreamThroughTheRealClientWritesAReadableResponseRecord(t *testing.T) {
	const (
		serverInput  = 52170 // the prompt side the server reports
		serverCache  = 6400
		serverOutput = 42
	)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if _, err := io.Copy(io.Discard, r.Body); err != nil {
			t.Errorf("drain request body: %v", err)
		}
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		stream := `event: message_start
data: {"type":"message_start","message":{"id":"msg_e2e","usage":{"input_tokens":52170,"cache_creation_input_tokens":0,"cache_read_input_tokens":6400,"output_tokens":0}}}

event: content_block_start
data: {"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}

event: content_block_delta
data: {"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"ok"}}

event: content_block_stop
data: {"type":"content_block_stop","index":0}

event: message_delta
data: {"type":"message_delta","delta":{"stop_reason":"end_turn"},"usage":{"input_tokens":52170,"cache_creation_input_tokens":0,"cache_read_input_tokens":6400,"output_tokens":42}}

event: message_stop
data: {"type":"message_stop"}
`
		if _, err := w.Write([]byte(stream)); err != nil {
			t.Errorf("write stream: %v", err)
		}
	}))
	defer server.Close()

	logPath := filepath.Join(t.TempDir(), "e2e.raw.log")
	f, err := os.Create(logPath)
	if err != nil {
		t.Fatalf("create raw log: %v", err)
	}
	// The client's RawLog callback is the seam the agent wires to a RawLogger; the
	// file is written with the SAME framing a run writes (rawlogfmt.Encode /
	// EncodeAnnotated), so reading it back exercises the real decoder.
	var mu sync.Mutex
	records := make([][]byte, 0, 16)
	client := &Client{
		BaseURL:     server.URL,
		APIKey:      "test-key",
		HTTPClient:  server.Client(),
		RetryConfig: DefaultRetryConfig(),
	}
	client.RawLog = func(tag, annotation string, payload []byte) {
		var rec []byte
		if annotation == "" {
			rec = rawlogfmt.Encode(tag, payload)
		} else {
			rec = rawlogfmt.EncodeAnnotated(tag, annotation, payload)
		}
		mu.Lock()
		records = append(records, rec)
		mu.Unlock()
	}

	req := &ClaudeRequest{
		Model:     "deepseek-v4-flash",
		MaxTokens: DefaultClaudeMaxTokens,
		Messages:  []ClaudeMessage{{Role: "user", Content: []ClaudeContentBlock{{Type: "text", Text: "hi"}}}},
	}
	events, err := client.ClaudeChatStream(context.Background(), req)
	if err != nil {
		t.Fatalf("ClaudeChatStream: %v", err)
	}
	for range events { //nolint:revive // draining the real channel: the assertion is on the LOG
	}

	mu.Lock()
	all := append([][]byte(nil), records...)
	mu.Unlock()
	if len(all) == 0 {
		t.Fatal("the real client wrote no raw-log records")
	}
	for _, rec := range all {
		if _, err := f.Write(rec); err != nil {
			t.Fatalf("write record: %v", err)
		}
	}
	if err := f.Close(); err != nil {
		t.Fatalf("close raw log: %v", err)
	}

	// READ IT BACK the way a reader does — through the package's own reader, so
	// the assertion is made on decoded records and not on the bytes we just built.
	reader, err := rawlogfmt.NewReader(logPath)
	if err != nil {
		t.Fatalf("open the log this run wrote: %v", err)
	}
	defer reader.Close()
	var tags []string
	var responseBody []byte
	for {
		rec, rerr := reader.Next()
		if rerr == io.EOF {
			break
		}
		if rerr != nil {
			t.Fatalf("decode record %d of the log this run wrote: %v", len(tags), rerr)
		}
		tags = append(tags, rec.Tag)
		if rec.Tag == rawlogfmt.TagResponse {
			responseBody = rec.Payload
			if rec.Annotation != rawlogfmt.StreamResponseAnnotation {
				t.Errorf("response annotation = %q, want %q", rec.Annotation, rawlogfmt.StreamResponseAnnotation)
			}
		}
	}
	if responseBody == nil {
		t.Fatalf("no complete-response record in a log the real client wrote; tags: %v", tags)
	}
	// ORDER: the response record arrives after the chunks of its own request.
	last := tags[len(tags)-1]
	if last != rawlogfmt.TagResponse {
		t.Errorf("last record tag = %q, want %q (the response must close the stream)", last, rawlogfmt.TagResponse)
	}

	var wire struct {
		Usage *ClaudeUsage `json:"usage"`
	}
	if err := json.Unmarshal(responseBody, &wire); err != nil {
		t.Fatalf("response body is not JSON: %v\n%s", err, responseBody)
	}
	if wire.Usage == nil {
		t.Fatalf("response body carries no usage: %s", responseBody)
	}
	if got, want := wire.Usage.InputTokens, serverInput; got != want {
		t.Errorf("input_tokens = %d, want %d (the server's own number)", got, want)
	}
	if got, want := wire.Usage.CacheReadInputTokens, serverCache; got != want {
		t.Errorf("cache_read_input_tokens = %d, want %d", got, want)
	}
	if got, want := wire.Usage.OutputTokens, serverOutput; got != want {
		t.Errorf("output_tokens = %d, want %d (message_delta's final counter, not message_start's 0)", got, want)
	}
	if got, want := wire.Usage.PromptSideTokens(), serverInput+serverCache; got != want {
		t.Errorf("prompt-side sum = %d, want %d (input + cache_creation + cache_read)", got, want)
	}
}
