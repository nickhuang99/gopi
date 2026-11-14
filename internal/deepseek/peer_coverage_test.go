// hp_coverage_test.go — function-level coverage closure for niche lanes.
//
// Scope (task cov-deepseek-hp, branch feat/cov-deepseek-hp): the exported
// functions the periodic func_coverage.py audit reported as UNCOVERED —
// ToolCall.MarshalJSON / ToolCall.UnmarshalJSON (wire format), the
// CostTracker pair (AddUsage / Summary), the two exported classification
// windows (IsAccountLevelStatus / LookupVendorAccountCode), the
// AccountLevelMessage builder (incl. the runtime-composed sentinel body),
// the accountLevelErrorValue.Unwrap chain, the failing-credential resolution
// pair (ResolveClaudeAPIKey / ResolveClaudeBaseURL), the constructor
// NewClaudeClient, and the LastHTTPFailure.RecordForTest seam.
//
// Determinism rules honored here (epic constraint):
//   - no real sleeps: every retry config in this file injects InitialDelay
//     and MaxDelay of exactly 1 nanosecond (time.Nanosecond), so a retry
//     clause costs nothing measurable;
//   - no live API calls: every wire path is served by net/http/httptest;
//   - no timing race for the failure probe: the send counter is read only
//     AFTER the synchronous call returns (the send happens inside the
//     operation closure retryDo has already joined);
//   - env vars that flip resolution order are forced to a known state through
//     t.Setenv (auto-restored) — the tests are hermetic whichever key the
//     developer has exported.
//
// Helper names carry the hp prefix to avoid collisions with the other
// coverage lanes landing in this package (epic constraint).

package deepseek

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// hpNanoRetryConfig is the deterministic retry config shared by every test in
// this file: at most 3 attempts, 1ns delays, no jitter — a retry clause is
// observable (via the send counter) but free (no measurable sleep).
func hpNanoRetryConfig() RetryConfig {
	return RetryConfig{
		MaxRetries:     3,
		InitialDelay:   time.Nanosecond,
		MaxDelay:       time.Nanosecond,
		JitterFraction: 0,
	}
}

// claudeDefaultBaseURL mirrors the default ResolveClaudeBaseURL returns: the
// LEGACY base (no "/anthropic" suffix), so claudeMessagesURL() is what
// appends the prefix. The DeepSeek Anthropic default (DefaultVisionBaseURL)
// is the OTHER default in this package and deliberately differs.
const claudeDefaultBaseURL = "https://api.deepseek.com"

// hpClearCredentialEnv forces the credential-resolution env vars to an unset
// state so the config-file branch of ResolveClaudeAPIKey /
// ResolveClaudeBaseURL is reachable regardless of the developer's shell
// (t.Setenv restores the previous values automatically at cleanup).
func hpClearCredentialEnv(t *testing.T) {
	t.Helper()
	t.Setenv("ANTHROPIC_AUTH_TOKEN", "")
	t.Setenv("DEEPSEEK_API_KEY", "")
	t.Setenv("ANTHROPIC_BASE_URL", "")
	t.Setenv("DEEPSEEK_BASE_URL", "")
}

// TestHPToolCall_WireRoundTrip pins ToolCall.MarshalJSON: the flat Go value
// goes onto the wire in the NESTED OpenAI/DeepSeek shape
// {id, type:"function", function:{name, arguments}} — arguments as a
// JSON-ENCODED STRING, never a nested object.
func TestHPToolCall_WireRoundTrip(t *testing.T) {
	tc := ToolCall{
		ID:        "call_abc",
		Name:      "read",
		Arguments: map[string]interface{}{"path": "/tmp/x", "limit": float64(10)},
	}

	wire, err := json.Marshal(tc)
	if err != nil {
		t.Fatalf("ToolCall.MarshalJSON: %v", err)
	}

	// Decode into a generic map so the SHAPE is asserted, not a reserialization.
	var got map[string]interface{}
	if err := json.Unmarshal(wire, &got); err != nil {
		t.Fatalf("marshal produced invalid JSON %s: %v", wire, err)
	}
	if got["id"] != tc.ID {
		t.Errorf("id = %v, want %v", got["id"], tc.ID)
	}
	if got["type"] != "function" {
		t.Errorf("type = %v, want \"function\" (nested wire format)", got["type"])
	}
	fn, ok := got["function"].(map[string]interface{})
	if !ok {
		t.Fatalf("function = %T, want an object (nested wire format): %s", got["function"], wire)
	}
	if fn["name"] != tc.Name {
		t.Errorf("function.name = %v, want %v", fn["name"], tc.Name)
	}
	args, ok := fn["arguments"].(string)
	if !ok {
		t.Fatalf("function.arguments = %T, want a JSON-encoded STRING: %s", fn["arguments"], wire)
	}
	var argMap map[string]interface{}
	if err := json.Unmarshal([]byte(args), &argMap); err != nil {
		t.Fatalf("function.arguments is not parseable JSON (%q): %v", args, err)
	}
	if argMap["path"] != "/tmp/x" {
		t.Errorf("arguments.path = %v, want /tmp/x", argMap["path"])
	}

	// Full round trip: UnmarshalJSON restores the flat Go representation.
	var back ToolCall
	if err := json.Unmarshal(wire, &back); err != nil {
		t.Fatalf("ToolCall.UnmarshalJSON: %v", err)
	}
	if back.ID != tc.ID || back.Name != tc.Name {
		t.Errorf("round trip = {%q,%q}, want {%q,%q}", back.ID, back.Name, tc.ID, tc.Name)
	}
	if back.Arguments["path"] != "/tmp/x" {
		t.Errorf("round-trip arguments.path = %v, want /tmp/x", back.Arguments["path"])
	}
}

// TestHPToolCall_MarshalNoArguments — BOUNDARY: a nil Arguments map encodes
// as the JSON literal "null" (encoding/json's nil-map encoding), NOT as "{}".
// That is not a wire hazard today: the round trip normalizes it back to an
// empty map on the read side (ToolCall.UnmarshalJSON), and the REQUEST path
// never marshals a ToolCall — BuildClaudeRequest converts ToolCalls into
// tool_use blocks and substitutes an empty input map itself. Pinned here so a
// change of either face is caught.
func TestHPToolCall_MarshalNoArguments(t *testing.T) {
	wire, err := json.Marshal(ToolCall{ID: "call_nil", Name: "bash"})
	if err != nil {
		t.Fatalf("ToolCall.MarshalJSON(nil args): %v", err)
	}
	if !strings.Contains(string(wire), `"arguments":"null"`) {
		t.Errorf("nil arguments encode as the string \"null\", got %s", wire)
	}

	var back ToolCall
	if err := json.Unmarshal(wire, &back); err != nil {
		t.Fatalf("round trip of a nil-args call: %v", err)
	}
	if back.Arguments == nil {
		t.Error("the read side must normalize a null arguments payload to an empty map")
	}
	if len(back.Arguments) != 0 {
		t.Errorf("Arguments = %v, want empty after the null round trip", back.Arguments)
	}
}

// TestHPToolCall_MarshalUnmarshalableArguments — ERROR path: json.Marshal of
// a channel value fails, so MarshalJSON must surface an error rather than
// emitting a half-built frame.
func TestHPToolCall_MarshalUnmarshalableArguments(t *testing.T) {
	tc := ToolCall{
		ID:        "call_bad",
		Name:      "bash",
		Arguments: map[string]interface{}{"ch": make(chan int)},
	}
	if _, err := json.Marshal(tc); err == nil {
		t.Fatal("marshal of an unmarshalable argument value must fail")
	}
}

// TestHPToolCall_Unmarshal_FragmentAndMalformed covers the streaming and
// error lanes of ToolCall.UnmarshalJSON:
//
//   - an incremental (unparseable) function.arguments fragment must NOT fail:
//     the raw text is kept in RawArgs and Arguments degrades to an empty map;
//   - an absent "arguments" key leaves RawArgs empty and Arguments non-nil
//     (callers index it without a nil check);
//   - a non-object frame is a hard error.
func TestHPToolCall_Unmarshal_FragmentAndMalformed(t *testing.T) {
	t.Run("fragment_is_tolerated", func(t *testing.T) {
		var tc ToolCall
		err := json.Unmarshal([]byte(`{"id":"c1","type":"function","function":{"name":"read","arguments":"{\"pa"}}`), &tc)
		if err != nil {
			t.Fatalf("incremental fragment must parse: %v", err)
		}
		if tc.RawArgs != `{"pa` {
			t.Errorf("RawArgs = %q, want the raw fragment %q", tc.RawArgs, `{"pa`)
		}
		if tc.Arguments == nil {
			t.Error("Arguments must never be left nil on a fragment")
		}
		if len(tc.Arguments) != 0 {
			t.Errorf("fragment must not populate Arguments, got %v", tc.Arguments)
		}
		if tc.ID != "c1" || tc.Name != "read" {
			t.Errorf("id/name = %q/%q, want c1/read", tc.ID, tc.Name)
		}
	})

	t.Run("no_arguments_key", func(t *testing.T) {
		var tc ToolCall
		if err := json.Unmarshal([]byte(`{"id":"c2","type":"function","function":{"name":"bash"}}`), &tc); err != nil {
			t.Fatalf("frame without arguments must parse: %v", err)
		}
		if tc.RawArgs != "" {
			t.Errorf("RawArgs = %q, want empty", tc.RawArgs)
		}
		if tc.Arguments == nil {
			t.Error("Arguments must be initialized even without an arguments payload")
		}
	})

	t.Run("malformed_frame", func(t *testing.T) {
		var tc ToolCall
		if err := json.Unmarshal([]byte(`"not-an-object"`), &tc); err == nil {
			t.Error("a non-object frame must be an error")
		}
	})
}

// TestHPCostTracker_AddUsageSummary covers the CostTracker pair: AddUsage
// accumulates every counter, Summary renders the compact operator line.
func TestHPCostTracker_AddUsageSummary(t *testing.T) {
	var ct CostTracker
	ct.AddUsage(Usage{PromptTokens: 100, CompletionTokens: 20, TotalTokens: 120, PromptCacheHitTokens: 40, PromptCacheMissTokens: 60})
	ct.AddUsage(Usage{PromptTokens: 50, CompletionTokens: 5, TotalTokens: 55, PromptCacheHitTokens: 10, PromptCacheMissTokens: 40})

	if ct.TotalPromptTokens != 150 || ct.TotalCompletionTokens != 25 {
		t.Errorf("totals = %d/%d, want 150/25", ct.TotalPromptTokens, ct.TotalCompletionTokens)
	}
	if ct.TotalCacheHitTokens != 50 || ct.TotalCacheMissTokens != 100 {
		t.Errorf("cache = %d/%d, want 50/100", ct.TotalCacheHitTokens, ct.TotalCacheMissTokens)
	}
	if got, want := ct.Summary(), "prompt=150(hit=50 miss=100) completion=25"; got != want {
		t.Errorf("Summary() = %q, want %q", got, want)
	}
}

// TestHPCostTracker_SummaryMissApproximation — BOUNDARY: when the provider
// reports cache hits but no miss counter (miss == 0 && hit > 0), Summary
// derives miss = prompt - hit instead of printing a structurally impossible
// "hit=40 miss=0".
func TestHPCostTracker_SummaryMissApproximation(t *testing.T) {
	ct := CostTracker{TotalPromptTokens: 100, TotalCacheHitTokens: 40, TotalCompletionTokens: 7}
	if got, want := ct.Summary(), "prompt=100(hit=40 miss=60) completion=7"; got != want {
		t.Errorf("Summary() = %q, want the derived-miss form %q", got, want)
	}

	// Zero value: no approximation when there are no hits at all.
	var zero CostTracker
	if got, want := zero.Summary(), "prompt=0(hit=0 miss=0) completion=0"; got != want {
		t.Errorf("zero Summary() = %q, want %q", got, want)
	}
}

// TestHPIsAccountLevelStatus_ExportedMatrix pins the exported window onto the
// classification predicate — HTTP semantics first, then vendor codes, and the
// gate that only lets a code count inside a status the request could not have
// earned (429 or 4xx; a 5xx envelope keeps its code transient).
func TestHPIsAccountLevelStatus_ExportedMatrix(t *testing.T) {
	cases := []struct {
		name   string
		status int
		body   string
		want   bool
	}{
		{"402_payment_required_code_agnostic", http.StatusPaymentRequired, rateLimit429Body, true},
		{"429_vendor_account_code", http.StatusTooManyRequests, glmBalance429Body, true},
		{"400_flat_account_code", http.StatusBadRequest, `{"code":"1113"}`, true},
		{"429_genuine_rate_limit", http.StatusTooManyRequests, rateLimit429Body, false},
		{"500_carrying_account_code_stays_transient", http.StatusInternalServerError, glmBalance429Body, false},
		{"200_never_account_level", http.StatusOK, glmBalance429Body, false},
		{"503_plain", http.StatusServiceUnavailable, `{"error":{"message":"overloaded"}}`, false},
	}
	for _, c := range cases {
		if got := IsAccountLevelStatus(c.status, []byte(c.body)); got != c.want {
			t.Errorf("%s: IsAccountLevelStatus(%d, %s) = %v, want %v", c.name, c.status, c.body, got, c.want)
		}
	}
	if !IsAccountLevelStatus(http.StatusPaymentRequired, nil) {
		t.Error("a bare 402 with no body must still be account-level (provider-neutral)")
	}
}

// TestHPLookupVendorAccountCode covers the exported lookup: a known vendor
// account code returns its operator-facing info, and every non-account body
// (unknown code, non-JSON, empty) returns the zero value + false.
func TestHPLookupVendorAccountCode(t *testing.T) {
	info, ok := LookupVendorAccountCode([]byte(`{"error":{"code":"1113"}}`))
	if !ok {
		t.Fatal("GLM 1113 must resolve to a vendor account code")
	}
	if info.Label == "" || info.Remedy == "" {
		t.Errorf("resolved info must carry a label and a remedy, got %+v", info)
	}

	bad := []struct {
		name string
		body []byte
	}{
		{"unknown_code", []byte(`{"error":{"code":"9999"}}`)},
		{"non_json_html", []byte("<html>502 bad gateway</html>")},
		{"empty_body", nil},
		{"429_without_account_code", []byte(rateLimit429Body)},
	}
	for _, c := range bad {
		got, ok := LookupVendorAccountCode(c.body)
		if ok {
			t.Errorf("%s: unexpectedly resolved %+v", c.name, got)
		}
		if got.Label != "" || got.Remedy != "" {
			t.Errorf("%s: zero info expected on a miss, got %+v", c.name, got)
		}
	}
}

// TestHPAccountLevelMessage_Shapes covers AccountLevelMessage end to end:
// the operator text for a vendor code carried in a 429, the plain-402
// fallback, the provider label taken from the failing base URL, the empty
// string for non-account failures, and the text built from the
// runtime-composed sentinel body produced by the CLIENT.
func TestHPAccountLevelMessage_Shapes(t *testing.T) {
	t.Run("vendor_code_429", func(t *testing.T) {
		msg := AccountLevelMessage(&HTTPStatusError{
			Status:       http.StatusTooManyRequests,
			Body:         []byte(glmBalance429Body),
			ProviderBase: "https://open.bigmodel.cn/api/anthropic",
		})
		for _, want := range []string{accountLevelMessagePrefix, "1113", "429", "open.bigmodel.cn", "top up"} {
			if !strings.Contains(msg, want) {
				t.Errorf("message missing %q: %s", want, msg)
			}
		}
	})

	t.Run("bare_402", func(t *testing.T) {
		msg := AccountLevelMessage(&HTTPStatusError{
			Status: http.StatusPaymentRequired,
			Body:   []byte(`{"error":{"message":"insufficient credit"}}`),
		})
		if !strings.Contains(msg, "payment required") || !strings.Contains(msg, providerLabelGeneric) {
			t.Errorf("402 message must name the condition and a (generic) provider: %s", msg)
		}
	})

	t.Run("non_account_is_empty", func(t *testing.T) {
		cases := []error{
			nil,
			errors.New("dial tcp 127.0.0.1:1: connect: connection refused"),
			&HTTPStatusError{Status: http.StatusInternalServerError, Body: []byte(`{"error":{"message":"boom"}}`)},
			&HTTPStatusError{Status: http.StatusTooManyRequests, Body: []byte(rateLimit429Body)},
		}
		for i, err := range cases {
			if msg := AccountLevelMessage(err); msg != "" {
				t.Errorf("case %d: non-account error produced %q, want \"\"", i, msg)
			}
		}
	})

	t.Run("client_composed_sentinel_body", func(t *testing.T) {
		var sends int32
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			atomic.AddInt32(&sends, 1)
			w.WriteHeader(http.StatusTooManyRequests)
			_, _ = w.Write([]byte(glmBalance429Body)) //stylegate:ignore writes to the test's own sink; the buffered content is the assertion
		}))
		defer srv.Close()

		c := newMatrixTestClient(t, srv)
		c.RetryConfig = hpNanoRetryConfig()

		_, err := c.ClaudeChat(t.Context(), baseReq())
		if err == nil {
			t.Fatal("an account-level 429 must fail the round")
		}
		msg := AccountLevelMessage(err)
		if msg == "" {
			t.Fatalf("the client's account-level error must yield a message, got error %v", err)
		}
		info, ok := LookupVendorAccountCode([]byte(glmBalance429Body))
		if !ok {
			t.Fatal("the fixture body must resolve to a vendor account code")
		}
		for _, want := range []string{info.Label, info.Remedy, "1113"} {
			if !strings.Contains(msg, want) {
				t.Errorf("message built from the runtime body is missing %q: %s", want, msg)
			}
		}
		if n := atomic.LoadInt32(&sends); n != 1 {
			t.Errorf("account-level failure must be sent once, got %d sends", n)
		}
	})
}

// TestHPAccountLevelErrorValue_Unwrap pins the Unwrap chain of the concrete
// account-level error: the wrapper's Error() is the operator message, and
// errors.As still reaches the underlying *HTTPStatusError (that is what lets
// an outer layer re-classify without string matching).
func TestHPAccountLevelErrorValue_Unwrap(t *testing.T) {
	raw := &HTTPStatusError{
		Status:       http.StatusPaymentRequired,
		Body:         []byte(`{"error":{"message":"insufficient credit"}}`),
		ProviderBase: "https://api.deepseek.com",
	}
	wrap := &accountLevelErrorValue{HTTPStatusError: raw}

	if got := wrap.Unwrap(); got != error(raw) {
		t.Errorf("Unwrap() = %v, want the underlying *HTTPStatusError", got)
	}
	if !strings.Contains(wrap.Error(), accountLevelMessagePrefix) {
		t.Errorf("Error() must be the operator message, got %q", wrap.Error())
	}

	var apiErr *HTTPStatusError
	if !errors.As(wrap, &apiErr) || apiErr.Status != http.StatusPaymentRequired {
		t.Errorf("errors.As must reach the raw status error, got %+v", apiErr)
	}
	if !IsAccountLevelError(wrap) {
		t.Error("IsAccountLevelError must classify the wrapper")
	}
}

// TestHPResolveClaudeAPIKey covers every rung of the credential ladder:
// ANTHROPIC_AUTH_TOKEN > DEEPSEEK_API_KEY > settings api_key > error.
func TestHPResolveClaudeAPIKey(t *testing.T) {
	t.Run("anthropic_token_wins", func(t *testing.T) {
		hpClearCredentialEnv(t)
		t.Setenv("DEEPSEEK_API_KEY", "deepseek-env-key")
		t.Setenv("ANTHROPIC_AUTH_TOKEN", "anthropic-env-key")
		got, err := ResolveClaudeAPIKey(&Config{APIKey: "file-key"})
		if err != nil || got != "anthropic-env-key" {
			t.Fatalf("got (%q, %v), want (anthropic-env-key, nil)", got, err)
		}
	})

	t.Run("deepseek_env_fallback", func(t *testing.T) {
		hpClearCredentialEnv(t)
		t.Setenv("DEEPSEEK_API_KEY", "deepseek-env-key")
		got, err := ResolveClaudeAPIKey(&Config{APIKey: "file-key"})
		if err != nil || got != "deepseek-env-key" {
			t.Fatalf("got (%q, %v), want (deepseek-env-key, nil)", got, err)
		}
	})

	t.Run("config_fallback", func(t *testing.T) {
		hpClearCredentialEnv(t)
		got, err := ResolveClaudeAPIKey(&Config{APIKey: "file-key"})
		if err != nil || got != "file-key" {
			t.Fatalf("got (%q, %v), want (file-key, nil)", got, err)
		}
	})

	t.Run("unresolvable_is_an_error", func(t *testing.T) {
		hpClearCredentialEnv(t)
		for _, cfg := range []*Config{nil, {}} {
			got, err := ResolveClaudeAPIKey(cfg)
			if err == nil {
				t.Fatalf("cfg %+v: want an error when no key is resolvable, got %q", cfg, got)
			}
			if got != "" {
				t.Errorf("cfg %+v: key must be empty on failure, got %q", cfg, got)
			}
			if !strings.Contains(err.Error(), "ANTHROPIC_AUTH_TOKEN") {
				t.Errorf("error must name the missing setting: %v", err)
			}
		}
	})
}

// TestHPResolveClaudeBaseURL covers the base-URL ladder and the default:
// ANTHROPIC_BASE_URL > DEEPSEEK_BASE_URL > settings base_url > built-in
// default (the default is the LEGACY base, so the path derivation — not the
// base itself — supplies the "/anthropic" prefix).
func TestHPResolveClaudeBaseURL(t *testing.T) {
	const (
		anthropicBase = "https://anthropic.example/anthropic"
		deepseekBase  = "https://deepseek.example"
		fileBase      = "https://file.example"
	)

	t.Run("anthropic_env_wins", func(t *testing.T) {
		hpClearCredentialEnv(t)
		t.Setenv("DEEPSEEK_BASE_URL", deepseekBase)
		t.Setenv("ANTHROPIC_BASE_URL", anthropicBase)
		if got := ResolveClaudeBaseURL(&Config{BaseURL: fileBase}); got != anthropicBase {
			t.Errorf("got %q, want %q", got, anthropicBase)
		}
	})

	t.Run("deepseek_env_fallback", func(t *testing.T) {
		hpClearCredentialEnv(t)
		t.Setenv("DEEPSEEK_BASE_URL", deepseekBase)
		if got := ResolveClaudeBaseURL(&Config{BaseURL: fileBase}); got != deepseekBase {
			t.Errorf("got %q, want %q", got, deepseekBase)
		}
	})

	t.Run("config_fallback", func(t *testing.T) {
		hpClearCredentialEnv(t)
		if got := ResolveClaudeBaseURL(&Config{BaseURL: fileBase}); got != fileBase {
			t.Errorf("got %q, want %q", got, fileBase)
		}
	})

	t.Run("default_and_nil_config", func(t *testing.T) {
		hpClearCredentialEnv(t)
		for _, cfg := range []*Config{nil, {}} {
			got := ResolveClaudeBaseURL(cfg)
			if got != claudeDefaultBaseURL {
				t.Errorf("cfg %+v: got %q, want the default %q", cfg, got, claudeDefaultBaseURL)
			}
			c := &Client{BaseURL: got}
			if want := claudeDefaultBaseURL + claudeMessagesPath; c.claudeMessagesURL() != want {
				t.Errorf("derived URL = %q, want %q", c.claudeMessagesURL(), want)
			}
		}
	})
}

// TestHPNewClaudeClient_EnvOverrides — happy path: the constructor resolves
// the credential, the base URL and the transport retry policy without
// touching a live endpoint.
func TestHPNewClaudeClient_EnvOverrides(t *testing.T) {
	hpClearCredentialEnv(t)
	t.Setenv("ANTHROPIC_AUTH_TOKEN", "hp-test-key")
	t.Setenv("ANTHROPIC_BASE_URL", "https://anthropic.example/anthropic")
	t.Setenv("GOPI_SETTINGS", filepath.Join(t.TempDir(), "empty-settings.json"))

	c, err := NewClaudeClient()
	if err != nil {
		t.Fatalf("NewClaudeClient: %v", err)
	}
	if c.APIKey != "hp-test-key" {
		t.Errorf("APIKey = %q, want the env credential", c.APIKey)
	}
	if c.BaseURL != "https://anthropic.example/anthropic" {
		t.Errorf("BaseURL = %q, want the env base", c.BaseURL)
	}
	if c.HTTPClient == nil {
		t.Error("HTTPClient must be built")
	}
	if c.RetryConfig != DefaultRetryConfig() {
		t.Errorf("RetryConfig = %+v, want DefaultRetryConfig()", c.RetryConfig)
	}
	if c.MessagesPath != "" {
		t.Errorf("MessagesPath = %q, want empty without a settings override", c.MessagesPath)
	}
	if c.LastErrDetail.HasFailed() {
		t.Error("a fresh client must have recorded no failure")
	}
}

// TestHPNewClaudeClient_SettingsFile — the settings-file lane: the credential
// and the messages_path override both come from settings.json.
func TestHPNewClaudeClient_SettingsFile(t *testing.T) {
	hpClearCredentialEnv(t)
	settings := filepath.Join(t.TempDir(), "settings.json")
	if err := os.WriteFile(settings, []byte(`{"api_key":"file-key","base_url":"https://file.example","messages_path":"/v1/messages"}`), 0o600); err != nil {
		t.Fatalf("write settings: %v", err)
	}
	t.Setenv("GOPI_SETTINGS", settings)

	c, err := NewClaudeClient()
	if err != nil {
		t.Fatalf("NewClaudeClient: %v", err)
	}
	if c.APIKey != "file-key" {
		t.Errorf("APIKey = %q, want the settings credential", c.APIKey)
	}
	if c.BaseURL != "https://file.example" {
		t.Errorf("BaseURL = %q, want the settings base", c.BaseURL)
	}
	if c.MessagesPath != "/v1/messages" {
		t.Errorf("MessagesPath = %q, want the settings override", c.MessagesPath)
	}
	if got, want := c.claudeMessagesURL(), "https://file.example/v1/messages"; got != want {
		t.Errorf("claudeMessagesURL() = %q, want the verbatim override %q", got, want)
	}
}

// TestHPNewClaudeClient_MalformedSettings — ERROR path: an unparsable
// settings file must fail the constructor rather than silently presenting an
// unauthenticated client.
func TestHPNewClaudeClient_MalformedSettings(t *testing.T) {
	hpClearCredentialEnv(t)
	settings := filepath.Join(t.TempDir(), "settings.json")
	if err := os.WriteFile(settings, []byte(`{"api_key":`), 0o600); err != nil {
		t.Fatalf("write settings: %v", err)
	}
	t.Setenv("GOPI_SETTINGS", settings)

	c, err := NewClaudeClient()
	if err == nil {
		t.Fatalf("malformed settings must fail the constructor, got client %+v", c)
	}
	if !strings.Contains(err.Error(), "failed to load config") {
		t.Errorf("error must name the config load stage: %v", err)
	}
}

// TestHPNewClaudeClient_MissingCredential — ERROR path: a readable settings
// file with no key and no credential env leaves the client unusable, so the
// constructor must fail with the resolution error (never a keyless client).
func TestHPNewClaudeClient_MissingCredential(t *testing.T) {
	hpClearCredentialEnv(t)
	settings := filepath.Join(t.TempDir(), "settings.json")
	if err := os.WriteFile(settings, []byte(`{}`), 0o600); err != nil {
		t.Fatalf("write settings: %v", err)
	}
	t.Setenv("GOPI_SETTINGS", settings)

	c, err := NewClaudeClient()
	if err == nil {
		t.Fatalf("a missing credential must fail the constructor, got client %+v", c)
	}
	if !strings.Contains(err.Error(), "ANTHROPIC_AUTH_TOKEN") {
		t.Errorf("error must be the resolution error, got: %v", err)
	}
}

// TestHPLastHTTPFailure_RecordForTest — the test seam records a failure
// snapshot (status + body, /test/path) and flips HasFailed.
func TestHPLastHTTPFailure_RecordForTest(t *testing.T) {
	var lf LastHTTPFailure
	if lf.HasFailed() {
		t.Fatal("the zero value must report no failure")
	}
	if snap := lf.Snapshot(); snap.StatusCode != 0 || snap.Body != "" {
		t.Errorf("zero-value snapshot = %+v, want empty", snap)
	}

	lf.RecordForTest(http.StatusInternalServerError, []byte(`{"error":{"message":"boom"}}`), false)

	if !lf.HasFailed() {
		t.Error("HasFailed must flip after a recorded failure")
	}
	snap := lf.Snapshot()
	if snap.StatusCode != http.StatusInternalServerError {
		t.Errorf("StatusCode = %d, want %d", snap.StatusCode, http.StatusInternalServerError)
	}
	if !strings.Contains(snap.Body, "boom") {
		t.Errorf("Body = %q, want the recorded error body", snap.Body)
	}
	if snap.RequestPath != "/test/path" {
		t.Errorf("RequestPath = %q, want the seam path", snap.RequestPath)
	}
	if snap.Stream {
		t.Error("Stream must be false for the non-streaming seam call")
	}
	if snap.TransportErr != "" {
		t.Errorf("TransportErr = %q, want empty (the seam records no transport error)", snap.TransportErr)
	}
	if snap.Timestamp == "" {
		t.Error("Timestamp must be stamped")
	}

	// The seam serves the streaming lane too — Stream must round-trip.
	lf.RecordForTest(http.StatusBadRequest, []byte(`{"error":{"message":"bad shape"}}`), true)
	if !lf.Snapshot().Stream {
		t.Error("a stream-flagged record must round-trip Stream=true")
	}
}
