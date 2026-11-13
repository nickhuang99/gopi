package gopilog

import (
	"strings"
	"testing"
)

// ---- Round framing: base markers, streaming request, blank-line framing ----

func TestParseHTTPRoundsBaseStreaming(t *testing.T) {
	// Sandbox $HOME: writeRawLog resolves <HOME>/.gopi/raw, and the seam
	// refuses the operator's live profile (internal/testutil/profileguard.go).
	t.Setenv("HOME", t.TempDir())
	content := "" +
		"→ REQUEST\n" +
		`{"model":"m","stream":true,"system":[{"type":"text","text":"sys"}],"messages":[{"role":"user","content":"hi"}]}` + "\n" +
		"← CHUNK\n" +
		"event: message_start\n" +
		`data: {"type":"message_start","message":{"usage":{"input_tokens":100,"cache_read_input_tokens":200}}}` + "\n" +
		"\n" +
		"← CHUNK\n" +
		"event: content_block_delta\n" +
		`data: {"type":"content_block_delta","delta":{"type":"text_delta","text":"hello"}}` + "\n" +
		"\n" +
		"← CHUNK\n" +
		"event: message_delta\n" +
		`data: {"type":"message_delta","delta":{"stop_reason":"end_turn"},"usage":{"output_tokens":42}}` + "\n" +
		"\n" +
		"→ REQUEST\n" +
		`{"model":"m","messages":[{"role":"user","content":"again"}]}` + "\n" +
		"← RESPONSE\n" +
		`{"content":"final"}` + "\n"

	rounds, err := ParseHTTPRounds(writeRawLog(t, "rounds-test.log", content))
	if err != nil {
		t.Fatalf("ParseHTTPRounds: %v", err)
	}
	if len(rounds) != 2 {
		t.Fatalf("len(rounds) = %d, want 2", len(rounds))
	}

	r1 := rounds[0]
	if r1.Number != 1 || r1.Annotation != "" {
		t.Errorf("round1 Number/Annotation = %d/%q, want 1/\"\"", r1.Number, r1.Annotation)
	}
	if !r1.Stream {
		t.Error("round1 Stream = false, want true (request had stream:true)")
	}
	if r1.Request == nil || len(r1.Request.Messages) != 1 {
		t.Errorf("round1 Request messages not parsed")
	}
	resp := r1.Response
	if resp.Chunks != 3 {
		t.Errorf("round1 chunks = %d, want 3", resp.Chunks)
	}
	if resp.InputTokens != 100 || resp.CacheRead != 200 {
		t.Errorf("round1 usage input/cache-read = %d/%d, want 100/200", resp.InputTokens, resp.CacheRead)
	}
	if resp.OutputTokens != 42 {
		t.Errorf("round1 output = %d, want 42 (from message_delta)", resp.OutputTokens)
	}
	if resp.TextChars != 5 {
		t.Errorf("round1 text chars = %d, want 5", resp.TextChars)
	}
	if resp.StopReason != "end_turn" {
		t.Errorf("round1 stop = %q, want end_turn", resp.StopReason)
	}

	r2 := rounds[1]
	if r2.Number != 2 || r2.Response.Kind != "response" {
		t.Errorf("round2 Number/Kind = %d/%q, want 2/response", r2.Number, r2.Response.Kind)
	}
	if r2.Stream {
		t.Error("round2 Stream = true, want false (no stream flag)")
	}

	// Line spans + response-side start. Round1 = request marker(line1) +
	// body(2) + three SSE events (3..13) + blank(14); its response side begins
	// at the first ← CHUNK (line3). Round2 = marker(15) + body(16) + ← RESPONSE
	// (17) + body(18); its response side begins at the ← RESPONSE marker(17).
	if r1.LineStart != 1 || r1.RespLineStart != 3 || r1.LineEnd != 14 {
		t.Errorf("round1 span = LineStart %d RespLineStart %d LineEnd %d, want 1/3/14",
			r1.LineStart, r1.RespLineStart, r1.LineEnd)
	}
	if r2.LineStart != 15 || r2.RespLineStart != 17 || r2.LineEnd != 18 {
		t.Errorf("round2 span = LineStart %d RespLineStart %d LineEnd %d, want 15/17/18",
			r2.LineStart, r2.RespLineStart, r2.LineEnd)
	}
}

// ---- Legacy single-line JSON chunk (no blank-line separator) ----

func TestParseHTTPRoundsLegacySingleLineChunks(t *testing.T) {
	// Sandbox $HOME: writeRawLog resolves <HOME>/.gopi/raw, and the seam
	// refuses the operator's live profile (internal/testutil/profileguard.go).
	t.Setenv("HOME", t.TempDir())
	// Older raw logs (deepseek chat.completion.chunk wire format) log each
	// chunk as ONE JSON line directly after the marker with no blank line —
	// the next line is the next marker.
	chunk1 := `{"id":"x","object":"chat.completion.chunk","choices":[{"index":0,"delta":{"reasoning_content":"think"},"finish_reason":null}]}`
	chunk2 := `{"id":"x","object":"chat.completion.chunk","choices":[{"index":0,"delta":{"content":"Hi"},"finish_reason":null}]}`
	chunk3 := `{"id":"x","object":"chat.completion.chunk","choices":[{"index":0,"delta":{},"finish_reason":"stop"}]}`
	content := "" +
		"→ REQUEST\n" +
		`{"model":"m","messages":[{"role":"user","content":"hi"}]}` + "\n" +
		"← CHUNK\n" + chunk1 + "\n" +
		"← CHUNK\n" + chunk2 + "\n" +
		"← CHUNK\n" + chunk3 + "\n" +
		"→ REQUEST\n" +
		`{"model":"m","messages":[{"role":"user","content":"bye"}]}` + "\n" +
		"← RESPONSE\n" +
		`{"ok":true}` + "\n"

	rounds, err := ParseHTTPRounds(writeRawLog(t, "rounds-test.log", content))
	if err != nil {
		t.Fatalf("ParseHTTPRounds: %v", err)
	}
	if len(rounds) != 2 {
		t.Fatalf("len(rounds) = %d, want 2", len(rounds))
	}
	r1 := rounds[0]
	if r1.Response.Chunks != 3 {
		t.Fatalf("round1 chunks = %d, want 3 (single-line shape must flush on the next marker)", r1.Response.Chunks)
	}
	if r1.Response.ThinkingChars != 5 || r1.Response.TextChars != 2 {
		t.Errorf("round1 thinking/text chars = %d/%d, want 5/2", r1.Response.ThinkingChars, r1.Response.TextChars)
	}
	if r1.Response.StopReason != "stop" {
		t.Errorf("round1 stop = %q, want stop (deepseek finish_reason)", r1.Response.StopReason)
	}
}

// ---- Guard/ladder annotated request markers are their own rounds ----

func TestParseHTTPRoundsAnnotatedMarkers(t *testing.T) {
	// Sandbox $HOME: writeRawLog resolves <HOME>/.gopi/raw, and the seam
	// refuses the operator's live profile (internal/testutil/profileguard.go).
	t.Setenv("HOME", t.TempDir())
	content := "" +
		"→ REQUEST (1214-guard, stream)\n" +
		`{"model":"m","stream":true,"messages":[{"role":"user","content":"hi"}]}` + "\n" +
		"← RESPONSE (1214-guard, stream)\n" +
		`{"error":true}` + "\n" +
		"→ REQUEST\n" +
		`{"model":"m","stream":true,"messages":[{"role":"user","content":"hi"}]}` + "\n" +
		"← CHUNK\n" +
		`{"id":"x","choices":[{"index":0,"delta":{"content":"ok"},"finish_reason":"stop"}]}` + "\n"

	rounds, err := ParseHTTPRounds(writeRawLog(t, "rounds-test.log", content))
	if err != nil {
		t.Fatalf("ParseHTTPRounds: %v", err)
	}
	if len(rounds) != 2 {
		t.Fatalf("len(rounds) = %d, want 2 (guard probe + real send)", len(rounds))
	}
	if rounds[0].Annotation != "1214-guard, stream" {
		t.Errorf("round1 annotation = %q, want 1214-guard, stream", rounds[0].Annotation)
	}
	if rounds[0].Response.Kind != "response" {
		t.Errorf("round1 kind = %q, want response", rounds[0].Response.Kind)
	}
	if rounds[1].Annotation != "" {
		t.Errorf("round2 annotation = %q, want \"\"", rounds[1].Annotation)
	}
}

// ---- Tool-use block start is captured ----

func TestParseHTTPRoundsToolUse(t *testing.T) {
	// Sandbox $HOME: writeRawLog resolves <HOME>/.gopi/raw, and the seam
	// refuses the operator's live profile (internal/testutil/profileguard.go).
	t.Setenv("HOME", t.TempDir())
	content := "" +
		"→ REQUEST\n" +
		`{"model":"m","stream":true,"messages":[{"role":"user","content":"hi"}]}` + "\n" +
		"← CHUNK\n" +
		"event: content_block_start\n" +
		`data: {"type":"content_block_start","index":0,"content_block":{"type":"tool_use","id":"call_1","name":"bash"}}` + "\n" +
		"\n" +
		"← CHUNK\n" +
		"event: content_block_start\n" +
		`data: {"type":"content_block_start","index":1,"content_block":{"type":"tool_use","id":"call_2","name":"read"}}` + "\n" +
		"\n"

	rounds, err := ParseHTTPRounds(writeRawLog(t, "rounds-test.log", content))
	if err != nil {
		t.Fatalf("ParseHTTPRounds: %v", err)
	}
	if len(rounds) != 1 {
		t.Fatalf("len(rounds) = %d, want 1", len(rounds))
	}
	tools := rounds[0].Response.UniqueToolUses()
	if len(tools) != 2 || tools[0] != "bash" || tools[1] != "read" {
		t.Errorf("tools = %v, want [bash read]", tools)
	}
}

// ---- Marker text inside a body is never a marker (whole-line equality) ----

func TestParseHTTPRoundsMarkerInsideBody(t *testing.T) {
	// Sandbox $HOME: writeRawLog resolves <HOME>/.gopi/raw, and the seam
	// refuses the operator's live profile (internal/testutil/profileguard.go).
	t.Setenv("HOME", t.TempDir())
	// The user content literally quotes the marker line; it is NOT a marker
	// because it is not a whole line equal to the marker.
	body := `{"model":"m","stream":true,"messages":[{"role":"user","content":"use: → REQUEST and ← CHUNK"}]}`
	content := "" +
		"→ REQUEST\n" + body + "\n" +
		"← CHUNK\n" +
		"event: message_delta\n" +
		`data: {"type":"message_delta","delta":{"stop_reason":"end_turn"}}` + "\n" +
		"\n"

	rounds, err := ParseHTTPRounds(writeRawLog(t, "rounds-test.log", content))
	if err != nil {
		t.Fatalf("ParseHTTPRounds: %v", err)
	}
	if len(rounds) != 1 {
		t.Fatalf("len(rounds) = %d, want 1 (body text quoted the markers)", len(rounds))
	}
}

// ---- HTTP-ERROR diagnostics are folded into the round ----

func TestParseHTTPRoundsHTTPError(t *testing.T) {
	// Sandbox $HOME: writeRawLog resolves <HOME>/.gopi/raw, and the seam
	// refuses the operator's live profile (internal/testutil/profileguard.go).
	t.Setenv("HOME", t.TempDir())
	content := "" +
		"→ REQUEST\n" +
		`{"model":"m","stream":true,"messages":[{"role":"user","content":"hi"}]}` + "\n" +
		"HTTP-ERROR\n" +
		"← streaming attempt — HTTP 429 (non-OK)\n" +
		`{"error":{"message":"rate limited"}}` + "\n" +
		"→ REQUEST\n" +
		`{"model":"m","stream":true,"messages":[{"role":"user","content":"hi"}]}` + "\n" +
		"← RESPONSE\n" +
		`{"ok":1}` + "\n"

	rounds, err := ParseHTTPRounds(writeRawLog(t, "rounds-test.log", content))
	if err != nil {
		t.Fatalf("ParseHTTPRounds: %v", err)
	}
	if len(rounds) != 2 {
		t.Fatalf("len(rounds) = %d, want 2", len(rounds))
	}
	if rounds[0].Response.Kind != "http-error" {
		t.Errorf("round1 kind = %q, want http-error", rounds[0].Response.Kind)
	}
	if !strings.Contains(string(rounds[0].Response.ErrorBody), "rate limited") {
		t.Errorf("round1 error body missing payload: %q", rounds[0].Response.ErrorBody)
	}
}

// ---- Request body size is the context-growth signal ----

func TestParseHTTPRoundsRequestBytes(t *testing.T) {
	// Sandbox $HOME: writeRawLog resolves <HOME>/.gopi/raw, and the seam
	// refuses the operator's live profile (internal/testutil/profileguard.go).
	t.Setenv("HOME", t.TempDir())
	small := `{"model":"m","messages":[{"role":"user","content":"a"}]}`
	big := `{"model":"m","messages":[{"role":"user","content":"` + strings.Repeat("x", 50000) + `"}]}`
	content := "→ REQUEST\n" + small + "\n" +
		"← CHUNK\n" + `{"choices":[{"delta":{},"finish_reason":"stop"}]}` + "\n" +
		"→ REQUEST\n" + big + "\n" +
		"← CHUNK\n" + `{"choices":[{"delta":{},"finish_reason":"stop"}]}` + "\n"

	rounds, err := ParseHTTPRounds(writeRawLog(t, "rounds-test.log", content))
	if err != nil {
		t.Fatalf("ParseHTTPRounds: %v", err)
	}
	if len(rounds) != 2 {
		t.Fatalf("len(rounds) = %d, want 2", len(rounds))
	}
	if rounds[0].Request == nil || rounds[1].Request == nil {
		t.Fatal("requests not parsed")
	}
	if len(rounds[1].RequestRaw) <= len(rounds[0].RequestRaw) {
		t.Errorf("round2 request bytes (%d) not > round1 (%d)", len(rounds[1].RequestRaw), len(rounds[0].RequestRaw))
	}
}
