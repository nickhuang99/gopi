package deepseek

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestLastErrDetail_NonStreaming400: a non-200 response is recorded on the
// client (the crash-dump epic's collection site) with status + body.
func TestLastErrDetail_NonStreaming400(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte(`{"type":"error","error":{"type":"invalid_request_error","message":"input is invalid (1214)"}}`))
	}))
	defer srv.Close()

	c := &Client{
		BaseURL:     srv.URL,
		APIKey:      "k",
		HTTPClient:  srv.Client(),
		RetryConfig: RetryConfig{MaxRetries: 0},
	}
	_, err := c.ClaudeChat(context.Background(), &ClaudeRequest{Model: "m", MaxTokens: 10, Messages: []ClaudeMessage{{Role: "user", Content: []ClaudeContentBlock{{Type: "text", Text: "hi"}}}}})
	if err == nil {
		t.Fatal("expected error")
	}
	if !c.LastErrDetail.HasFailed() {
		t.Fatal("LastErrDetail not recorded on 400")
	}
	snap := c.LastErrDetail.Snapshot()
	if snap.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", snap.StatusCode)
	}
	if snap.Body == "" || !contains(snap.Body, "1214") {
		t.Fatalf("body = %q", snap.Body)
	}
	if got := string(snap.Category()); got != "4xx" {
		t.Fatalf("category = %s", got)
	}
}

// TestLastErrDetail_RetryExhaustedKeepsLast: after retries, the LAST failure
// is what's recorded.
func TestLastErrDetail_RetryExhaustedKeepsLast(t *testing.T) {
	n := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n++
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte(`{"error":"attempt ` + itoa(n) + `"}`))
	}))
	defer srv.Close()

	c := &Client{
		BaseURL:     srv.URL,
		APIKey:      "k",
		HTTPClient:  srv.Client(),
		RetryConfig: RetryConfig{MaxRetries: 2, InitialDelay: 1, MaxDelay: 1},
	}
	_, err := c.ClaudeChat(context.Background(), &ClaudeRequest{Model: "m", MaxTokens: 10, Messages: []ClaudeMessage{{Role: "user", Content: []ClaudeContentBlock{{Type: "text", Text: "hi"}}}}})
	if err == nil {
		t.Fatal("expected error")
	}
	snap := c.LastErrDetail.Snapshot()
	if snap.StatusCode != 500 {
		t.Fatalf("status = %d", snap.StatusCode)
	}
	if !contains(snap.Body, "attempt") {
		t.Fatalf("body = %q", snap.Body)
	}
}

// TestLastErrDetail_StreamNon200: the streaming path records too.
func TestLastErrDetail_StreamNon200(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
		w.Write([]byte(`{"error":"overloaded"}`))
	}))
	defer srv.Close()

	c := &Client{
		BaseURL:     srv.URL,
		APIKey:      "k",
		HTTPClient:  srv.Client(),
		RetryConfig: RetryConfig{MaxRetries: 0},
	}
	_, err := c.ClaudeChatStream(context.Background(), &ClaudeRequest{Model: "m", MaxTokens: 10, Messages: []ClaudeMessage{{Role: "user", Content: []ClaudeContentBlock{{Type: "text", Text: "hi"}}}}})
	if err == nil {
		t.Fatal("expected error")
	}
	snap := c.LastErrDetail.Snapshot()
	if snap.StatusCode != 503 || !snap.Stream {
		t.Fatalf("snapshot = %+v", snap)
	}
	if got := string(snap.Category()); got != "5xx" {
		t.Fatalf("category = %s", got)
	}
}

// TestLastErrDetail_RecordPersistsAcrossSuccess: the snapshot is only read
// when a RUN fails — a successful send after a failed one does not clear the
// record (the 1214-ladder flow relies on this: a failed baseline attempt is
// followed by an accepted variant). Note: retry semantics for 5xx over the
// ClaudeChat wrapper are governed by the synthesized-error path (retry.go);
// here MaxRetries=0 isolates the record-persistence contract.
func TestLastErrDetail_RecordPersistsAcrossSuccess(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
		w.Write([]byte(`{"error":"overloaded"}`))
	}))
	defer srv.Close()

	c := &Client{
		BaseURL:     srv.URL,
		APIKey:      "k",
		HTTPClient:  srv.Client(),
		RetryConfig: RetryConfig{MaxRetries: 0},
	}
	_, err := c.ClaudeChat(context.Background(), &ClaudeRequest{Model: "m", MaxTokens: 10, Messages: []ClaudeMessage{{Role: "user", Content: []ClaudeContentBlock{{Type: "text", Text: "hi"}}}}})
	if err == nil {
		t.Fatal("expected error")
	}
	if !c.LastErrDetail.HasFailed() {
		t.Fatal("failure should be recorded")
	}
	snap := c.LastErrDetail.Snapshot()
	if snap.StatusCode != 503 {
		t.Fatalf("status = %d, want 503", snap.StatusCode)
	}
}

// glmBalanceExhaustedBody is the verbatim wire shape GLM returns for
// "账户余额不足或无可用资源包" (account out of balance) — HTTP 429 with the
// same status a genuine rate limit uses (evidence: the 23:00 HP worker
// death, devdoc/night-report-20260910.md).
const glmBalanceExhaustedBody = `{"type":"error","error":{"type":"rate_limit_error","code":"1113","message":"[1113][余额不足或无可用资源包,请充值。]"}}`

// TestFailureSnapshot_AccountCategory — the acceptance contract at the
// taxonomy level: an account-level 429 (GLM 1113) is category "account" and
// NOT recoverable, so a resumer is never told to retry a dead account.
func TestFailureSnapshot_AccountCategory(t *testing.T) {
	snap := FailureSnapshot{StatusCode: http.StatusTooManyRequests, Body: glmBalanceExhaustedBody}
	if got := string(snap.Category()); got != string(CategoryAccount) {
		t.Fatalf("category = %q, want %q", got, CategoryAccount)
	}
	if snap.RecoverableHint() {
		t.Fatal("account-level failure reported recoverable — a resumer would retry a dead account")
	}
	// The remedy text names the condition AND the operator action.
	remedy := snap.RemedyText()
	for _, want := range []string{AccountLevelMessagePrefix, "1113", "balance", "top up"} {
		if !strings.Contains(remedy, want) {
			t.Errorf("remedy %q missing %q", remedy, want)
		}
	}
}

// TestFailureSnapshot_AccountCategory_OpenAIWireShape pins the acceptance
// shape verbatim: the flat OpenAI-wire body {"code":"1113"} inside a 429 is
// the account category and NOT recoverable. The status alone says "rate
// limit"; the code says "account unusable" — and the code wins.
func TestFailureSnapshot_AccountCategory_OpenAIWireShape(t *testing.T) {
	snap := FailureSnapshot{StatusCode: http.StatusTooManyRequests, Body: `{"code":"1113"}`}
	if got := string(snap.Category()); got != string(CategoryAccount) {
		t.Fatalf("category = %q, want %q", got, CategoryAccount)
	}
	if snap.RecoverableHint() {
		t.Fatal("RecoverableHint = true — a resumer would retry a dead account")
	}
	if !strings.Contains(snap.RemedyText(), "top up") {
		t.Errorf("remedy missing the operator action: %q", snap.RemedyText())
	}
}

// TestFailureSnapshot_Genuine429StaysRetryable — the regression guard: a 429
// WITHOUT an account code is still a rate limit (category 4xx, recoverable,
// no remedy text). Without this, the account category could swallow every
// transient 429 and stop the retry path from working at all.
func TestFailureSnapshot_Genuine429StaysRetryable(t *testing.T) {
	snap := FailureSnapshot{StatusCode: http.StatusTooManyRequests, Body: `{"error":"rate limited, retry later"}`}
	if got := string(snap.Category()); got != string(CategoryHTTP4xx) {
		t.Fatalf("category = %q, want %q", got, CategoryHTTP4xx)
	}
	if !snap.RecoverableHint() {
		t.Fatal("genuine 429 must stay recoverable (retry later)")
	}
	if got := snap.RemedyText(); got != "" {
		t.Fatalf("non-account failure must carry no remedy text, got %q", got)
	}
}

// TestFailureSnapshot_402PaymentRequiredIsAccount: 402 is the provider-neutral
// billing status — account-level with no vendor code needed.
func TestFailureSnapshot_402PaymentRequiredIsAccount(t *testing.T) {
	snap := FailureSnapshot{StatusCode: http.StatusPaymentRequired, Body: `{"error":"payment required"}`}
	if got := string(snap.Category()); got != string(CategoryAccount) {
		t.Fatalf("category = %q, want %q", got, CategoryAccount)
	}
	if snap.RecoverableHint() {
		t.Fatal("402 must not be reported recoverable")
	}
	if snap.RemedyText() == "" {
		t.Fatal("402 must carry the generic billing remedy")
	}
}

// TestLastErrDetail_AccountLevelThroughClient: the account category must
// survive the REAL send path (a unit test on a constructed snapshot alone
// would not prove the snapshot is populated with the account code).
func TestLastErrDetail_AccountLevelThroughClient(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
		w.Write([]byte(glmBalanceExhaustedBody))
	}))
	defer srv.Close()

	c := &Client{
		BaseURL:     srv.URL,
		APIKey:      "k",
		HTTPClient:  srv.Client(),
		RetryConfig: RetryConfig{MaxRetries: 0},
	}
	_, err := c.ClaudeChat(context.Background(), &ClaudeRequest{Model: "m", MaxTokens: 10, Messages: []ClaudeMessage{{Role: "user", Content: []ClaudeContentBlock{{Type: "text", Text: "hi"}}}}})
	if err == nil {
		t.Fatal("expected error")
	}
	if !IsAccountLevelError(err) {
		t.Fatal("client error should be account-level")
	}
	snap := c.LastErrDetail.Snapshot()
	if got := string(snap.Category()); got != string(CategoryAccount) {
		t.Fatalf("snapshot category = %q, want %q (%+v)", got, CategoryAccount, snap)
	}
	if snap.RecoverableHint() {
		t.Fatal("snapshot reported recoverable for an account-level failure")
	}
	if !strings.Contains(snap.RemedyText(), "1113") {
		t.Fatalf("remedy must name the vendor code: %q", snap.RemedyText())
	}
}

// helpers.
func contains(s, sub string) bool {
	return len(s) >= len(sub) && (func() bool {
		for i := 0; i+len(sub) <= len(s); i++ {
			if s[i:i+len(sub)] == sub {
				return true
			}
		}
		return false
	})()
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b []byte
	for n > 0 {
		b = append([]byte{byte('0' + n%10)}, b...)
		n /= 10
	}
	return string(b)
}
