package deepseek

// Account-level error classification tests (epic
// fix/llm-account-error-classification).
//
// Evidence under test: the 23:00 HP worker death log
// (~/.gopi/worker-night-1.log, quoted in devdoc/night-report-20260910.md):
//
//	deepseek: API error (status 429): {"type":"error","error":{
//	  "type":"rate_limit_error","code":"1113",
//	  "message":"[1113][余额不足或无可用资源包,请充值。]"}}
//	  after 3 retries, fallback: deepseek: API error (status 429): ... after 3 retries
//
// Contract pinned here:
//   - an account-level failure is sent ONCE (no retry, no sleep);
//   - its error text is the actionable operator message (condition + remedy);
//   - a GENUINE 429 rate limit still retries and still honours Retry-After
//     (regression guard — the classification must not eat rate limiting);
//   - 5xx/402-adjacent statuses keep their previous treatment.
//
// No real sleeps: every retryable case is driven with an InitialDelay of
// 1ns (the retry DELAY is proven separately, deterministically, by
// TestSentinel429_RetryAfterHeaderHonored and the retryDelay unit tests).

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// glmBalance429Body is the byte-exact GLM body from the HP worker death log
// (single line — the provider wraps it across lines only for display).
const glmBalance429Body = `{"type":"error","error":{"type":"rate_limit_error","code":"1113","message":"[1113][余额不足或无可用资源包,请充值。]"}}`

// rateLimit429Body is a GENUINE rate limit: same status, no account code.
const rateLimit429Body = `{"type":"error","error":{"type":"rate_limit_error","message":"requests per minute exceeded"}}`

// TestIsAccountLevel_StatusAndCodeMatrix pins the predicate: HTTP semantics
// first (402 billing), then the vendor account codes — and only inside a
// status the request could not have earned.
func TestIsAccountLevel_StatusAndCodeMatrix(t *testing.T) {
	cases := []struct {
		name   string
		status int
		body   string
		want   bool
	}{
		{"GLM 1113 balance inside a 429 is account-level", http.StatusTooManyRequests, glmBalance429Body, true},
		{"1113 flat OpenAI-wire envelope is account-level", http.StatusTooManyRequests, `{"code":"1113","message":"余额不足"}`, true},
		{"1113 numeric code is account-level", http.StatusTooManyRequests, `{"error":{"code":1113}}`, true},
		{"1113 error_code gateway key is account-level", http.StatusTooManyRequests, `{"error_code":"1113"}`, true},
		{"1113 inside a 400 is account-level", http.StatusBadRequest, `{"error":{"code":"1113"}}`, true},
		{"402 payment required is account-level (provider-neutral)", http.StatusPaymentRequired, `{"error":"billing"}`, true},
		{"402 with no body is account-level", http.StatusPaymentRequired, ``, true},
		{"genuine 429 rate limit is NOT account-level", http.StatusTooManyRequests, rateLimit429Body, false},
		{"429 with no body is NOT account-level", http.StatusTooManyRequests, ``, false},
		{"429 with unparsable body is NOT account-level", http.StatusTooManyRequests, `<html>429</html>`, false},
		{"1113 in a 5xx stays transient (server failure)", http.StatusInternalServerError, `{"error":{"code":"1113"}}`, false},
		{"503 is NOT account-level", http.StatusServiceUnavailable, `{"error":"overloaded"}`, false},
		{"401 is NOT account-level (deterministic 4xx path)", http.StatusUnauthorized, `{"error":"bad key"}`, false},
		{"500 rate_limit body is NOT account-level", http.StatusInternalServerError, rateLimit429Body, false},
		{"200 with a 1113 body is NOT account-level", http.StatusOK, glmBalance429Body, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := isAccountLevelStatus(tc.status, []byte(tc.body)); got != tc.want {
				t.Fatalf("isAccountLevelStatus(%d, %s) = %t, want %t", tc.status, tc.body, got, tc.want)
			}
		})
	}
}

// TestAccountLevel_NotRetriedAndSentOnce — the core acceptance test: a 429
// carrying {"code":"1113"} must be sent EXACTLY ONCE (the pre-fix client
// burned 3 retries + a same-credential fallback) and the returned error must
// carry the actionable operator message naming condition AND remedy.
func TestAccountLevel_NotRetriedAndSentOnce(t *testing.T) {
	var sends int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sends++
		// A Retry-After is deliberately present: an account-level failure
		// must NOT wait it out either.
		w.Header().Set("Retry-After", "60")
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = w.Write([]byte(glmBalance429Body)) //stylegate:ignore writes to the test's own sink; the buffered content is the assertion
	}))
	defer srv.Close()

	c := newMatrixTestClient(t, srv)
	c.RetryConfig.MaxRetries = 3
	c.RetryConfig.InitialDelay = time.Nanosecond
	c.RetryConfig.MaxDelay = time.Nanosecond
	c.RetryConfig.JitterFraction = 0

	_, err := c.ClaudeChat(context.Background(), baseReq())
	if err == nil {
		t.Fatal("account-level failure must be returned as an error")
	}
	if sends != 1 {
		t.Fatalf("account-level error must NOT be retried: %d sends, want 1", sends)
	}

	msg := err.Error()
	for _, want := range []string{
		AccountLevelMessagePrefix,     // greppable anchor
		"account out of balance",      // the CONDITION
		"code 1113",                   // the vendor code
		"top up the provider account", // the REMEDY
		"switch to another provider",  // the alternative remedy
		"Not retrying",                // explicit no-retry statement
		"429",                         // the enclosing HTTP status
	} {
		if !strings.Contains(msg, want) {
			t.Errorf("account-level message missing %q:\n%s", want, msg)
		}
	}
	// The message must NOT look like a retry-exhausted error (the pre-fix
	// text was "... after 3 retries").
	if strings.Contains(msg, "after 3 retries") {
		t.Errorf("account-level error must not claim retry exhaustion:\n%s", msg)
	}

	// The typed predicate must agree, so the agent layer can skip fallback.
	if !IsAccountLevelError(err) {
		t.Error("IsAccountLevelError(err) = false, want true")
	}
	if got := AccountLevelMessageOf(err); got != msg {
		t.Errorf("AccountLevelMessageOf != err.Error():\n%q\n%q", got, msg)
	}
}

// TestAccountLevel_StreamingPathNotRetried — the streaming path (the one the
// HP worker actually used) behaves identically: one send, account-level error.
func TestAccountLevel_StreamingPathNotRetried(t *testing.T) {
	var sends int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sends++
		w.Header().Set("Retry-After", "60")
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = w.Write([]byte(glmBalance429Body)) //stylegate:ignore writes to the test's own sink; the buffered content is the assertion
	}))
	defer srv.Close()

	c := newMatrixTestClient(t, srv)
	c.RetryConfig.MaxRetries = 3
	c.RetryConfig.InitialDelay = time.Nanosecond
	c.RetryConfig.MaxDelay = time.Nanosecond
	c.RetryConfig.JitterFraction = 0

	_, err := c.ClaudeChatStream(context.Background(), baseReq())
	if err == nil {
		t.Fatal("streaming path must surface the account-level failure")
	}
	if sends != 1 {
		t.Fatalf("streaming account-level error must NOT be retried: %d sends, want 1", sends)
	}
	if !IsAccountLevelError(err) {
		t.Fatalf("streaming error not classified account-level: %v", err)
	}
}

// TestGenuineRateLimit_StillRetried — REGRESSION GUARD: the classification
// must not eat real rate limiting. A 429 WITHOUT an account code is retried
// MaxRetries times (and the attempt count, not a wall-clock sleep, is what
// this test asserts — delays are 1ns).
func TestGenuineRateLimit_StillRetried(t *testing.T) {
	var sends int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sends++
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = w.Write([]byte(rateLimit429Body)) //stylegate:ignore writes to the test's own sink; the buffered content is the assertion
	}))
	defer srv.Close()

	const maxRetries = 2
	c := newMatrixTestClient(t, srv)
	c.RetryConfig.MaxRetries = maxRetries
	c.RetryConfig.InitialDelay = time.Nanosecond
	c.RetryConfig.MaxDelay = time.Nanosecond
	c.RetryConfig.JitterFraction = 0

	_, err := c.ClaudeChat(context.Background(), baseReq())
	if err == nil {
		t.Fatal("persistent genuine 429 must eventually fail")
	}
	if sends != maxRetries+1 {
		t.Fatalf("genuine 429 must be retried: %d sends, want %d", sends, maxRetries+1)
	}
	if IsAccountLevelError(err) {
		t.Errorf("genuine rate limit must NOT be account-level: %v", err)
	}
	if !strings.Contains(err.Error(), "after 2 retries") {
		t.Errorf("genuine 429 error should report retry exhaustion: %v", err)
	}
}

// TestServerError_StillRetried — 5xx keeps its transient treatment (the
// account-code probe must not leak into server failures).
func TestServerError_StillRetried(t *testing.T) {
	var sends int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sends++
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"error":{"code":"1113","message":"server side"}}`)) //stylegate:ignore writes to the test's own sink; the buffered content is the assertion
	}))
	defer srv.Close()

	c := newMatrixTestClient(t, srv)
	c.RetryConfig.MaxRetries = 1
	c.RetryConfig.InitialDelay = time.Nanosecond
	c.RetryConfig.MaxDelay = time.Nanosecond
	c.RetryConfig.JitterFraction = 0

	_, err := c.ClaudeChat(context.Background(), baseReq())
	if err == nil {
		t.Fatal("persistent 5xx must fail")
	}
	if sends != 2 {
		t.Fatalf("5xx must be retried even with an account code in the body: %d sends, want 2", sends)
	}
	if IsAccountLevelError(err) {
		t.Errorf("5xx must not be account-level: %v", err)
	}
}

// TestAccountLevelMessage_402ProviderNeutral — the provider-neutral path: a
// bare 402 from ANY provider is account-level and yields an actionable message
// (no vendor code needed).
func TestAccountLevelMessage_402ProviderNeutral(t *testing.T) {
	var sends int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sends++
		w.WriteHeader(http.StatusPaymentRequired)
		_, _ = w.Write([]byte(`{"error":{"message":"insufficient credit"}}`)) //stylegate:ignore writes to the test's own sink; the buffered content is the assertion
	}))
	defer srv.Close()

	c := newMatrixTestClient(t, srv)
	c.RetryConfig.MaxRetries = 3
	c.RetryConfig.InitialDelay = time.Nanosecond
	c.RetryConfig.MaxDelay = time.Nanosecond
	c.RetryConfig.JitterFraction = 0

	_, err := c.ClaudeChat(context.Background(), baseReq())
	if err == nil {
		t.Fatal("402 must be an error")
	}
	if sends != 1 {
		t.Fatalf("402 must not be retried: %d sends, want 1", sends)
	}
	msg := err.Error()
	if !strings.Contains(msg, "payment required") || !strings.Contains(msg, "402") {
		t.Errorf("402 message must name the condition and status: %s", msg)
	}
	if !strings.Contains(msg, "top up") && !strings.Contains(msg, "settle the billing") {
		t.Errorf("402 message must name a remedy: %s", msg)
	}
}

// TestProviderLabel — the operator message names the failing provider
// (host of the base URL) and degrades to a generic label, never to an empty
// string (Rule 15a).
func TestProviderLabel(t *testing.T) {
	cases := map[string]string{
		"https://open.bigmodel.cn/api/anthropic": "open.bigmodel.cn",
		"https://api.deepseek.com":               "api.deepseek.com",
		"HTTPS://Open.BigModel.CN/":              "open.bigmodel.cn",
		"127.0.0.1:8080":                         "127.0.0.1:8080",
		"":                                       providerLabelGeneric,
		"   ":                                    providerLabelGeneric,
	}
	for in, want := range cases {
		if got := ProviderLabelForBase(in); got != want {
			t.Errorf("ProviderLabelForBase(%q) = %q, want %q", in, got, want)
		}
	}
}

// TestParseVendorErrorCode_Shapes — every envelope shape the probe must read,
// and the bodies that must yield "" (never a false account code).
func TestParseVendorErrorCode_Shapes(t *testing.T) {
	cases := map[string]string{
		glmBalance429Body:                 vendorAccountCodeGLMBalance,
		`{"error":{"code":"1113"}}`:       vendorAccountCodeGLMBalance,
		`{"code":"1113"}`:                 vendorAccountCodeGLMBalance,
		`{"code":1113}`:                   vendorAccountCodeGLMBalance,
		`{"error_code":"1113"}`:           vendorAccountCodeGLMBalance,
		`{"error":{"code":1210,"x":1}}`:   "1210",
		`{"error":"plain string"}`:        "",
		`{"error":{"message":"no code"}}`: "",
		`<html>429 rate limited</html>`:   "",
		`not json at all`:                 "",
		"":                                "",
	}
	for body, want := range cases {
		if got := parseVendorErrorCode([]byte(body)); got != want {
			t.Errorf("parseVendorErrorCode(%q) = %q, want %q", body, got, want)
		}
	}
}

// TestIsAccountLevelError_ErrorShapes — the predicate must be robust to
// non-HTTP errors and nil (it is called on every failure path).
func TestIsAccountLevelError_ErrorShapes(t *testing.T) {
	if IsAccountLevelError(nil) {
		t.Error("nil error must not be account-level")
	}
	if IsAccountLevelError(errors.New("dial tcp: connection refused")) {
		t.Error("transport error must not be account-level")
	}
	if IsAccountLevelError(context.DeadlineExceeded) {
		t.Error("context deadline must not be account-level")
	}
	wrapped := &HTTPStatusError{Status: http.StatusPaymentRequired, Body: []byte(`{}`), ProviderBase: "https://x.example"}
	if !IsAccountLevelError(wrapped) {
		t.Error("exported HTTPStatusError(402) must classify account-level")
	}
	// Wrapping must not hide it (the client wraps with %w in some paths).
	if !IsAccountLevelError(wrappedAs(wrapped)) {
		t.Error("account-level classification must survive error wrapping")
	}
}

// wrappedAs wraps err with %w, mirroring the client's wrapping style.
func wrappedAs(err error) error { return fmt.Errorf("client: %w", err) }
