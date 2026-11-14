package deepseek

// Regression tests for the 5c6f5ca Retry-After dead-branch bug (audit A/B #3,
// devdoc/audit-ab3-zpeer-2026-09-04.md):
//
//   - 429/5xx arrived as sentinelAPIError → classTransient arm ran
//     cfg.backoff(attempt) ONLY — the Retry-After header, parsed in the
//     operation closure, was never consulted → rate-limit dogpile risk and
//     a false "Retry-After honored" doc contract.
//
// The fix: classTransient consults the sentinel's retryAfter via the shared
// retryDelay helper — delay = max(backoff, retryAfter), Retry-After capped
// at MaxDelay. These tests pin the helper semantics and prove end-to-end
// that a real 429 + Retry-After header changes the measured wait.

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// TestRetryDelay_MaxSemantics — delay = max(backoff, retryAfter): a server
// Retry-After longer than the local backoff must win (dogpile protection),
// a shorter one must not shorten the client's own schedule, and retryAfter
// 0/negative must fall back to plain backoff.
func TestRetryDelay_MaxSemantics(t *testing.T) {
	cfg := RetryConfig{InitialDelay: 10 * time.Millisecond, MaxDelay: time.Second}

	// attempt 0 → backoff 10ms; retryAfter 500ms must win.
	if d := cfg.retryDelay(500, 0); d < 500*time.Millisecond {
		t.Errorf("retryAfter 500ms must dominate backoff 10ms, got %v", d)
	}
	// attempt 6 → backoff already capped at MaxDelay 1s; retryAfter must NOT win
	// once it exceeds the cap.
	if d := cfg.retryDelay(3600, 6); d > time.Second {
		t.Errorf("Retry-After must be capped at MaxDelay, got %v", d)
	}
	// retryAfter 0 → plain backoff.
	d0 := cfg.retryDelay(0, 0)
	plain := cfg.backoff(0)
	if d0 != plain {
		t.Errorf("retryAfter 0 must equal plain backoff: got %v want %v", d0, plain)
	}
	// negative retryAfter (unparsable header → parseRetryAfter returns 0, but
	// guard anyway) → plain backoff.
	if dn := cfg.retryDelay(-1, 0); dn != plain {
		t.Errorf("negative retryAfter must equal plain backoff: got %v want %v", dn, plain)
	}
}

// TestParseRetryAfter_Formats — delta-seconds, HTTP-date, past HTTP-date
// (already elapsed → floor wait of 1s), and garbage → 0.
func TestParseRetryAfter_Formats(t *testing.T) {
	// delta-seconds.
	if got := parseRetryAfter(&http.Response{Header: http.Header{"Retry-After": {"7"}}}); got != 7 {
		t.Errorf("delta-seconds: got %d want 7", got)
	}
	// HTTP-date ~5s in the future.
	future := time.Now().Add(5 * time.Second).UTC().Format(http.TimeFormat)
	if got := parseRetryAfter(&http.Response{Header: http.Header{"Retry-After": {future}}}); got < 4 || got > 6 {
		t.Errorf("HTTP-date: got %d, want ~5 (1s tolerance)", got)
	}
	// HTTP-date already past → wait at least 1s.
	past := time.Now().Add(-5 * time.Second).UTC().Format(http.TimeFormat)
	if got := parseRetryAfter(&http.Response{Header: http.Header{"Retry-After": {past}}}); got != 1 {
		t.Errorf("past HTTP-date: got %d want 1", got)
	}
	// No header / garbage.
	if got := parseRetryAfter(&http.Response{}); got != 0 {
		t.Errorf("absent header: got %d want 0", got)
	}
	if got := parseRetryAfter(&http.Response{Header: http.Header{"Retry-After": {"soon"}}}); got != 0 {
		t.Errorf("garbage header: got %d want 0", got)
	}
}

// TestSentinel429_RetryAfterHeaderHonored — end-to-end proof the header is
// NOT dead: a 429 carrying Retry-After must make the classTransient arm wait
// ≥ retryAfter (the 5c6f5ca regression waited only the plain backoff).
func TestSentinel429_RetryAfterHeaderHonored(t *testing.T) {
	const retryAfterSecs = 2
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Retry-After", fmt.Sprintf("%d", retryAfterSecs))
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = w.Write([]byte(`{"error":"rate limited"}`)) //stylegate:ignore writes to the test's own sink; the buffered content is the assertion
	}))
	defer server.Close()

	c := newMatrixTestClient(t, server)
	c.RetryConfig.MaxRetries = 1
	c.RetryConfig.InitialDelay = time.Millisecond // plain backoff would be ~1ms
	c.RetryConfig.MaxDelay = 30 * time.Second
	c.RetryConfig.JitterFraction = 0 // deterministic delay measurement

	start := time.Now()
	_, err := c.ClaudeChat(context.Background(), baseReq())
	waited := time.Since(start)

	if err == nil {
		t.Fatal("persistent 429 must fail after retries")
	}
	// The classTransient arm must have waited for Retry-After, not 1ms.
	// Allow generous slack for scheduling; the pre-fix wait was ~1ms so a
	// ≥1s threshold cleanly separates honored from dead branch.
	if waited < 1*time.Second {
		t.Fatalf("Retry-After %ds not honored: measured wait %v (dead-branch regression)", retryAfterSecs, waited)
	}
	if !strings.Contains(err.Error(), "429") {
		t.Errorf("error should surface status 429: %v", err)
	}
}
