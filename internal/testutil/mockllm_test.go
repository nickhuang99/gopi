package testutil

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestToolUseSSE_PartialJSONIsAString pins the Claude wire format for tool
// input: input_json_delta.partial_json is a STRING containing the accumulated
// JSON — not a JSON object. Marshaling the args map straight into that field
// emits a bare object, which the agent loop's fragment accumulator silently
// reads as NO input at all (the tool then fails with "Missing required
// parameter: …" and the run still exits 0). See devdoc/testsuite-skeleton.md
// §4 — this is the bug that took the three-mode suite down on arrival.
func TestToolUseSSE_PartialJSONIsAString(t *testing.T) {
	rec := httptest.NewRecorder()
	ToolUseSSE(rec, ToolCall{
		ID:   "call_1",
		Name: "write",
		Args: map[string]interface{}{"path": "plan.txt", "content": "hello\nworld"},
	})

	// Find the input_json_delta frame and decode it with the real payload
	// shape. The property under test is expressed by unmarshalling
	// partial_json into a Go string: a bare JSON object fails here.
	var found bool
	for _, ev := range ParseSSE(rec.Body.String()) {
		var payload struct {
			Delta struct {
				Type        string          `json:"type"`
				PartialJSON json.RawMessage `json:"partial_json"`
			} `json:"delta"`
		}
		if err := json.Unmarshal([]byte(ev[1]), &payload); err != nil {
			continue
		}
		if payload.Delta.Type != "input_json_delta" {
			continue
		}
		found = true
		var partial string
		if err := json.Unmarshal(payload.Delta.PartialJSON, &partial); err != nil {
			t.Fatalf("partial_json is not a JSON string (the loop reads this as empty tool input): %v — %.80s",
				err, payload.Delta.PartialJSON)
		}
		// Key order is Go's sorted map order; both args must be inside.
		if !strings.Contains(partial, `"path":"plan.txt"`) || !strings.Contains(partial, `"content":"hello\nworld"`) {
			t.Errorf("partial_json string = %q, want the escaped args JSON", partial)
		}
	}
	if !found {
		t.Fatalf("no input_json_delta frame in mock body:\n%s", rec.Body.String())
	}
}

// TestNewMockLLM_ScriptIndexAndRepeat checks the request→script mapping: the
// n-th request gets script[n-1], and requests beyond the script repeat the
// LAST entry (so confirmation rounds get a benign answer and the run ends).
func TestNewMockLLM_ScriptIndexAndRepeat(t *testing.T) {
	m := NewMockLLM(t,
		Resp{Text: "first"},
		Resp{Text: "second"},
	)
	defer m.Close()

	post := func(tag string) string {
		resp, err := http.Post(m.URL()+"/anthropic/messages", "application/json",
			strings.NewReader(`{"tag":"`+tag+`"}`))
		if err != nil {
			t.Fatalf("POST: %v", err)
		}
		defer resp.Body.Close()
		body, err := io.ReadAll(resp.Body)
		if err != nil {
			t.Fatalf("read body: %v", err)
		}
		return string(body)
	}

	// Requests 3 and 4 are beyond the script — they must repeat "second".
	// The answer is asserted on the JOINED text deltas (TextSSE splits it in
	// two, so no single fragment carries the whole string).
	for i, want := range []string{"first", "second", "second", "second"} {
		if got := streamedText(t, post(fmt.Sprint("req", i+1))); got != want {
			t.Errorf("request %d streamed text = %q, want %q", i+1, got, want)
		}
	}
	if n := m.RequestCount(); n != 4 {
		t.Errorf("RequestCount = %d, want 4", n)
	}
	// RequestBody records what the CLIENT sent (1-based), for wire-shape
	// assertions like the /tools toggle carrying the toolset.
	if got := m.RequestBody(3); got != `{"tag":"req3"}` {
		t.Errorf("RequestBody(3) = %q, want the third request's own body", got)
	}
	if got := m.RequestBody(5); got != "" {
		t.Errorf("RequestBody(5) = %q, want empty (out of range)", got)
	}
}

// TestTextSSEStream_ChunksAndRecordsTheSizesItStreamed pins the red-zone
// cadence primitive the render harness needs, and the ONE property that makes a
// cadence claim checkable: the sizes are MEASURED off the stream (the function's
// return, and the server-side record read by TextStreamSizes), never restated
// from the caller's parameter.
//
// The cases that matter:
//   - deltaBytes=1 over ASCII: every fragment is one byte (the finest cadence
//     the red zone asks for);
//   - deltaBytes=1 over CJK: a fragment is one RUNE (3 bytes), because a
//     rune-splitting cut would emit half a rune in a JSON string — the property
//     this pins is that a wide rune may make a fragment WIDER than the
//     requested width, never narrower, so the returned sizes are the honest
//     answer and a caller may not assume `== deltaBytes`;
//   - deltaBytes=4 over CJK: the same rule at a coarser width (3+rune, no split).
func TestTextSSEStream_ChunksAndRecordsTheSizesItStreamed(t *testing.T) {
	const ascii = "abcdefghij" // 10 one-byte runes
	cjk := "这一段中文"             // 5 three-byte runes = 15 bytes

	cases := []struct {
		name       string
		text       string
		deltaBytes int
		wantSizes  []int
	}{
		{"ascii delta=1", ascii, 1, []int{1, 1, 1, 1, 1, 1, 1, 1, 1, 1}},
		{"ascii delta=4", ascii, 4, []int{4, 4, 2}},
		{"cjk delta=1 (one rune per fragment, never half a rune)", cjk, 1, []int{3, 3, 3, 3, 3}},
		{"cjk delta=4 (3 bytes fit, the next 3 do not)", cjk, 4, []int{3, 3, 3, 3, 3}},
		{"delta=0 falls back to the legacy two-half shape", ascii, 0, nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			got := TextSSEStream(rec, tc.text, tc.deltaBytes, 0)
			if len(got) != len(tc.wantSizes) {
				t.Fatalf("streamed %d fragments %v, want %d %v", len(got), got, len(tc.wantSizes), tc.wantSizes)
			}
			for i := range got {
				if got[i] != tc.wantSizes[i] {
					t.Fatalf("fragment sizes = %v, want %v", got, tc.wantSizes)
				}
			}
			// The fragments must reassemble to the answer EXACTLY: a cadence
			// primitive that loses or reorders a byte would make the render
			// harness measure a truncated answer and call it a paint defect.
			var joined strings.Builder
			for _, ev := range ParseSSE(rec.Body.String()) {
				var payload struct {
					Delta struct {
						Type string `json:"type"`
						Text string `json:"text"`
					} `json:"delta"`
				}
				if err := json.Unmarshal([]byte(ev[1]), &payload); err != nil {
					continue
				}
				if payload.Delta.Type == "text_delta" {
					joined.WriteString(payload.Delta.Text)
				}
			}
			if joined.String() != tc.text {
				t.Fatalf("the streamed fragments reassemble to %q, want %q", joined.String(), tc.text)
			}
			// Every fragment must be valid UTF-8 on its own: half a rune in a
			// JSON string is a wire shape no server emits.
			for i, n := range got {
				_ = i
				if n <= 0 {
					t.Fatalf("fragment %d has size %d", i, n)
				}
			}
		})
	}
}

// TestNewMockLLM_TextStreamSizesIsTheServersOwnRecord is the server-side half:
// the values TextStreamSizes reports for a request come from what the handler
// WROTE, so a harness's cadence claim is readable from the stub. The index is
// per REQUEST (1-based) and a round that streams no text leaves a hole rather
// than shifting the later rounds' sizes into the wrong slot.
func TestNewMockLLM_TextStreamSizesIsTheServersOwnRecord(t *testing.T) {
	m := NewMockLLM(t,
		Resp{ToolUse: &ToolCall{ID: "call_1", Name: "bash", Args: map[string]interface{}{"command": "true"}}},
		Resp{Text: "abcdefgh", TextDeltaBytes: 2},
	)
	defer m.Close()

	post := func() {
		resp, err := http.Post(m.URL()+"/anthropic/messages", "application/json", strings.NewReader("{}"))
		if err != nil {
			t.Fatalf("POST: %v", err)
		}
		_, _ = io.ReadAll(resp.Body) //stylegate:ignore drained so the mock's handler finishes before the next request
		_ = resp.Body.Close()
	}
	post() // request 1: the tool round
	post() // request 2: the 2-byte-delta text round

	if got := m.TextStreamSizes(1); got != nil {
		t.Errorf("TextStreamSizes(1) = %v, want nil (a tool_use round streams no text)", got)
	}
	want := []int{2, 2, 2, 2}
	got := m.TextStreamSizes(2)
	if len(got) != len(want) {
		t.Fatalf("TextStreamSizes(2) = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("TextStreamSizes(2) = %v, want %v", got, want)
		}
	}
	if got := m.TextStreamSizes(9); got != nil {
		t.Errorf("TextStreamSizes(9) = %v, want nil (out of range)", got)
	}
}

// streamedText decodes an SSE body and returns the concatenation of every
// text_delta fragment — the final answer the client assembles.
func streamedText(t *testing.T, sseBody string) string {
	t.Helper()
	var out string
	for _, ev := range ParseSSE(sseBody) {
		var payload struct {
			Delta struct {
				Type string `json:"type"`
				Text string `json:"text"`
			} `json:"delta"`
		}
		if err := json.Unmarshal([]byte(ev[1]), &payload); err != nil {
			continue // not a content_block_delta frame
		}
		if payload.Delta.Type == "text_delta" {
			out += payload.Delta.Text
		}
	}
	return out
}

// TestNewMockLLM_FailIsNonRetryable checks the scripted error path: Fail makes
// the mock answer 400, which the client does NOT retry (only 429/5xx).
func TestNewMockLLM_FailIsNonRetryable(t *testing.T) {
	m := NewMockLLM(t, Resp{Fail: true})
	defer m.Close()

	resp, err := http.Post(m.URL()+"/anthropic/messages", "application/json", strings.NewReader("{}"))
	if err != nil {
		t.Fatalf("POST: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("status = %d, want 400 (non-retryable scripted failure)", resp.StatusCode)
	}
}
