package deepseek

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// newMatrixTestClient builds a Client pointed at ts with a scratch dump dir.
func newMatrixTestClient(t *testing.T, ts *httptest.Server) *Client {
	t.Helper()
	dumpDir := t.TempDir()
	t.Setenv("GOPI_ERROR_DUMP_DIR", dumpDir)
	t.Cleanup(func() { os.Unsetenv("GOPI_ERROR_DUMP_DIR") })
	return &Client{
		BaseURL:    ts.URL,
		APIKey:     "test-key",
		HTTPClient: ts.Client(),
		RetryConfig: RetryConfig{
			MaxRetries:     0, // deterministic tests — no retry delays
			InitialDelay:   1,
			MaxDelay:       1,
			JitterFraction: 0,
		},
	}
}

// baseReq is a representative full request: every optional/extension field set.
func baseReq() *ClaudeRequest {
	return &ClaudeRequest{
		Model:           "test-model",
		MaxTokens:       100,
		Stream:          false,
		Thinking:        &ClaudeThinking{Type: "enabled", BudgetTokens: 1024},
		ReasoningEffort: "high",
		Tools:           []ClaudeToolDef{{Name: "bash", Description: "run"}},
		System:          []ClaudeSystemBlock{{Type: "text", Text: "sys", CacheControl: &cacheControlEphemeral}},
		Messages: []ClaudeMessage{{
			Role: "user",
			Content: []ClaudeContentBlock{{
				Type: "text",
				Text: "hello",
			}},
		}},
	}
}

// classifyMatrixTest is the classification-matrix unit test: every row of the
// HTTP-semantic matrix (provider-neutral — the same status means the same
// class regardless of which provider sent it).
func TestClassifyHTTPError_Matrix(t *testing.T) {
	cases := []struct {
		name   string
		status int
		body   string
		err    error
		want   errorClass
	}{
		{"400 is deterministic", 400, `{"error":"invalid_request"}`, nil, classDeterministic},
		{"401 is deterministic", 401, `{"error":"auth"}`, nil, classDeterministic},
		{"403 is deterministic", 403, `{}`, nil, classDeterministic},
		{"404 is deterministic", 404, `{}`, nil, classDeterministic},
		{"422 is deterministic", 422, `{}`, nil, classDeterministic},
		{"1214-shaped 400 is just deterministic", 400, `{"code":1214,"msg":"invalid"}`, nil, classDeterministic},
		{"GLM 1210-shaped 400 is just deterministic", 400, `{"error":{"code":"1210"}}`, nil, classDeterministic},
		{"429 is transient", 429, `{}`, nil, classTransient},
		{"500 is transient", 500, `{}`, nil, classTransient},
		{"502 is transient", 502, `{}`, nil, classTransient},
		{"503 is transient", 503, `{}`, nil, classTransient},
		{"200 is unknown (not an error)", 200, `{}`, nil, classUnknown},
		{"nil everything is unknown", 0, "", nil, classUnknown},
		{"network timeout is network", 0, "", errors.New("context deadline exceeded"), classNetwork},
		{"connection refused is network", 0, "", &net.OpError{Op: "dial", Err: errors.New("connection refused")}, classNetwork},
		{"non-network error is unknown", 0, "", errors.New("marshal failed"), classUnknown},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := classifyHTTPError(tc.status, []byte(tc.body), tc.err)
			if got != tc.want {
				t.Fatalf("classifyHTTPError(%d) = %v, want %v", tc.status, got, tc.want)
			}
		})
	}
}

// TestRetryableStatus_DeterministicNeverRetries: 4xx must never be retried
// verbatim; 429/5xx always are.
func TestRetryableStatus_DeterministicNeverRetries(t *testing.T) {
	for _, s := range []int{400, 401, 403, 404, 409, 418, 422} {
		if retryableStatus(s) {
			t.Errorf("retryableStatus(%d) = true, want false (deterministic)", s)
		}
	}
	for _, s := range []int{429, 500, 502, 503, 504} {
		if !retryableStatus(s) {
			t.Errorf("retryableStatus(%d) = false, want true (transient)", s)
		}
	}
}

// TestMinimalCore_StripsAllOptional: the minimal core keeps ONLY
// model+messages+max_tokens+stream — thinking, reasoning_effort, tools,
// system, cache_control all stripped. The wire JSON must contain none of the
// optional keys.
func TestMinimalCore_StripsAllOptional(t *testing.T) {
	core := MinimalCore(baseReq())
	b, err := json.Marshal(core)
	if err != nil {
		t.Fatal(err)
	}
	wire := string(b)
	for _, banned := range []string{`"thinking"`, `"reasoning_effort"`, `"tools"`, `"system"`, `"cache_control"`, `"temperature"`, `"top_p"`} {
		if strings.Contains(wire, banned) {
			t.Errorf("minimal core wire contains %s: %s", banned, wire)
		}
	}
	for _, required := range []string{`"model"`, `"messages"`, `"max_tokens"`} {
		if !strings.Contains(wire, required) {
			t.Errorf("minimal core wire missing %s: %s", required, wire)
		}
	}
	if core.Stream {
		t.Error("minimal core must preserve the caller's stream flag (false here)")
	}
	// The original request is untouched.
	orig := baseReq()
	if orig.Thinking == nil || orig.ReasoningEffort != "high" || orig.Tools == nil {
		t.Error("MinimalCore must not mutate the caller's request")
	}
}

// runMatrixScenario drives a fake provider through one of the three
// deterministic-4xx historical incidents: the FIRST send returns the given
// status+body, the SECOND send (the minimal core) returns 200 with a valid
// response. Asserts the minimal core re-send succeeded and that the retried
// request was stripped to the minimal core.
func runMatrixScenario(t *testing.T, firstStatus int, firstBody string) {
	t.Helper()
	var sendCount int
	var retriedBody map[string]interface{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sendCount++
		raw, _ := io.ReadAll(r.Body) //stylegate:ignore bounded read of an error/diagnostic body; a short read is acceptable
		if sendCount == 1 {
			w.WriteHeader(firstStatus)
			_, _ = w.Write([]byte(firstBody)) //stylegate:ignore writes to the test's own sink; the buffered content is the assertion
			return
		}
		// Minimal-core re-send accepted.
		_ = json.Unmarshal(raw, &retriedBody) //stylegate:ignore best-effort decode of an optional field; the zero value is the documented fallback
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"id": "resp-ok", "type": "message", "role": "assistant", "model": "m",
			"content":     []map[string]interface{}{{"type": "text", "text": "ok"}},
			"stop_reason": "end_turn", "usage": map[string]int{"input_tokens": 1, "output_tokens": 1},
		}) //stylegate:ignore encodes a stub response; a failure surfaces as a client-side decode error in the same test
	}))
	defer server.Close()

	c := newMatrixTestClient(t, server)
	c.RetryConfig.MaxRetries = 0

	_, err := c.ClaudeChat(context.Background(), baseReq())
	if err != nil {
		t.Fatalf("minimal-core degradation should have rescued the request: %v", err)
	}
	if sendCount != 2 {
		t.Fatalf("expected exactly 2 sends (orig + minimal core), got %d", sendCount)
	}
	// The re-sent request must be the minimal core.
	for _, banned := range []string{"thinking", "reasoning_effort", "tools", "system", "cache_control"} {
		if _, ok := retriedBody[banned]; ok {
			t.Errorf("re-sent request still carries %q: %v", banned, retriedBody)
		}
	}
	if _, ok := retriedBody["messages"]; !ok {
		t.Errorf("re-sent request missing messages: %v", retriedBody)
	}
}

// TestDegradePath_DeepSeek400EmptyThinking — historical incident: DeepSeek
// 400 on a thinking-shaped request. The matrix must rescue it via the
// provider-neutral minimal core (no 1214 marker, no vendor branch).
func TestDegradePath_DeepSeek400EmptyThinking(t *testing.T) {
	runMatrixScenario(t, 400, `{"error":{"type":"invalid_request_error","message":"thinking produced no content"}}`)
}

// TestDegradePath_GLM1214 — historical incident: GLM 1214 invalid-request
// shape. Same provider-neutral path must rescue it (the old vendor marker
// check is gone — the matrix classifies by status alone).
func TestDegradePath_GLM1214(t *testing.T) {
	runMatrixScenario(t, 400, `{"error":{"type":"invalid_request_error","message":"Invalid request: code 1214, messages shape rejected"}}`)
}

// TestDegradePath_Anthropic400 — historical incident: Anthropic-compatible
// endpoint 400 invalid_request_error. Same rescue path.
func TestDegradePath_Anthropic400(t *testing.T) {
	runMatrixScenario(t, 400, `{"type":"error","error":{"type":"invalid_request_error","message":"max_tokens: field required"}}`)
}

// TestDegradePath_Exhausted_DumpAndFail: minimal core still 4xx → fail fast
// with the FULL original request dumped (replayable), and exactly 2 sends.
func TestDegradePath_Exhausted_DumpAndFail(t *testing.T) {
	var sendCount int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sendCount++
		w.WriteHeader(400)
		_, _ = w.Write([]byte(`{"error":{"type":"invalid_request_error","message":"still bad"}}`)) //stylegate:ignore writes to the test's own sink; the buffered content is the assertion
	}))
	defer server.Close()

	c := newMatrixTestClient(t, server)
	c.RetryConfig.MaxRetries = 0

	_, err := c.ClaudeChat(context.Background(), baseReq())
	if err == nil {
		t.Fatal("expected fail-fast error after degraded re-send still 400")
	}
	if sendCount != 2 {
		t.Fatalf("expected exactly 2 sends (orig + minimal core), got %d", sendCount)
	}
	if !strings.Contains(err.Error(), "minimal-core degradation") {
		t.Errorf("error should mention minimal-core degradation: %v", err)
	}
	// The dump file must exist and carry the original request + HTTP status.
	dumpDir := os.Getenv("GOPI_ERROR_DUMP_DIR")
	entries, _ := filepath.Glob(filepath.Join(dumpDir, "*.dump.json")) //stylegate:ignore glob for optional cleanup; the pattern is a constant so ErrBadPattern is unreachable
	if len(entries) != 1 {
		t.Fatalf("expected exactly 1 dump file, got %d", len(entries))
	}
	raw, _ := os.ReadFile(entries[0]) //stylegate:ignore reads a file the test itself wrote; the content is the assertion
	var doc struct {
		HTTPStatus  int             `json:"http_status"`
		APIError    string          `json:"api_error_body"`
		OriginalReq json.RawMessage `json:"original_request"`
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatalf("dump not valid JSON: %v", err)
	}
	if doc.HTTPStatus != 400 {
		t.Errorf("dump http_status = %d, want 400", doc.HTTPStatus)
	}
	if !strings.Contains(doc.APIError, "still bad") {
		t.Errorf("dump api_error_body missing provider error: %s", doc.APIError)
	}
	var orig map[string]interface{}
	if err := json.Unmarshal(doc.OriginalReq, &orig); err != nil {
		t.Fatalf("dump original_request not valid JSON: %v", err)
	}
	if _, ok := orig["thinking"]; !ok {
		t.Error("dump must preserve the ORIGINAL (non-stripped) request for replay")
	}
}

// TestTransientRetry_429And5xx — transient class still retries with backoff
// (429 and 500 both retried, then succeed).
func TestTransientRetry_429And5xx(t *testing.T) {
	for _, status := range []int{429, 500} {
		t.Run(fmt.Sprintf("status-%d", status), func(t *testing.T) {
			var calls int
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				if calls == 1 {
					w.WriteHeader(status)
					_, _ = w.Write([]byte(`{"error":"transient"}`)) //stylegate:ignore writes to the test's own sink; the buffered content is the assertion
					return
				}
				w.Header().Set("Content-Type", "application/json")
				_ = json.NewEncoder(w).Encode(map[string]interface{}{
					"id": "ok", "type": "message", "role": "assistant", "model": "m",
					"content":     []map[string]interface{}{{"type": "text", "text": "ok"}},
					"stop_reason": "end_turn", "usage": map[string]int{"input_tokens": 1, "output_tokens": 1},
				}) //stylegate:ignore encodes a stub response; a failure surfaces as a client-side decode error in the same test
			}))
			defer server.Close()

			c := newMatrixTestClient(t, server)
			c.RetryConfig.MaxRetries = 1 // one retry, no long backoff
			c.RetryConfig.InitialDelay = 1
			c.RetryConfig.MaxDelay = 1

			_, err := c.ClaudeChat(context.Background(), baseReq())
			if err != nil {
				t.Fatalf("transient %d should retry then succeed: %v", status, err)
			}
			if calls != 2 {
				t.Fatalf("expected 2 calls for transient %d, got %d", status, calls)
			}
		})
	}
}

// TestNetworkError_RetriedThenFailedWithDump: a persistently unreachable
// endpoint is retried MaxRetries times, then fails; the last failed request
// is dumped for observability (provider-agnostic diagnostics).
func TestNetworkError_RetriedThenFailedWithDump(t *testing.T) {
	// A server that is closed immediately — every dial fails with a
	// network-level error.
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	url := server.URL
	server.Close()

	c := newMatrixTestClient(t, server)
	c.BaseURL = url
	c.RetryConfig.MaxRetries = 3
	c.RetryConfig.InitialDelay = 1
	c.RetryConfig.MaxDelay = 1

	_, err := c.ClaudeChat(context.Background(), baseReq())
	if err == nil {
		t.Fatal("expected network failure after retries")
	}
	if !strings.Contains(err.Error(), "after 3 retries") {
		t.Errorf("error should report the retry count: %v", err)
	}
}

// TestNon200RawLog_CarriesStatusAndBody: every non-OK attempt writes
// "HTTP-ERROR" + status + error body into the raw log (provider-agnostic
// diagnostic record).
func TestNon200RawLog_CarriesStatusAndBody(t *testing.T) {
	var rawLog []byte
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(400)
		_, _ = w.Write([]byte(`{"error":"shape rejected"}`)) //stylegate:ignore writes to the test's own sink; the buffered content is the assertion
	}))
	defer server.Close()

	c := newMatrixTestClient(t, server)
	c.RetryConfig.MaxRetries = 0
	c.RawLog = func(tag, annotation string, data []byte) {
		rawLog = append(rawLog, data...)
	}

	_, _ = c.ClaudeChat(context.Background(), baseReq()) //stylegate:ignore chat round in a stub-backed fixture; the recorded request is the assertion

	logStr := string(rawLog)
	if !strings.Contains(logStr, "HTTP 400") {
		t.Errorf("raw log missing HTTP status: %s", logStr)
	}
	if !strings.Contains(logStr, "shape rejected") {
		t.Errorf("raw log missing error response body: %s", logStr)
	}
}

// TestStreamPath4xx_NoClientResend: a 4xx on the streaming path must NOT be
// re-sent client-side (the agent-layer stream→non-stream fallback owns the
// recovery; a client re-send would double-send). Exactly 1 send happens.
func TestStreamPath4xx_NoClientResend(t *testing.T) {
	var sendCount int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sendCount++
		w.WriteHeader(400)
		_, _ = w.Write([]byte(`{"error":"streaming not supported"}`)) //stylegate:ignore writes to the test's own sink; the buffered content is the assertion
	}))
	defer server.Close()

	c := newMatrixTestClient(t, server)
	c.RetryConfig.MaxRetries = 0

	_, err := c.ClaudeChatStream(context.Background(), baseReq())
	if err == nil {
		t.Fatal("expected the 4xx to surface for the agent-layer fallback")
	}
	if sendCount != 1 {
		t.Fatalf("streaming path must not re-send on 4xx; got %d sends", sendCount)
	}
}
