package deepseek

// Regression test for the DRIFTED non-200 body-probe cap (PROJECT.md Rule 1a/13).
//
// Defect: the cap was duplicated as bare literals — retryDo's raw
// *http.Response branch read 8192 bytes (retry.go) while the sentinel
// branches read 65536 (claude_client.go). isAccountLevelStatus only sees the
// bytes it is handed, so a vendor account code (GLM 1113) sitting PAST byte
// 8192 was invisible on the raw branch: the 429 looked like a plain rate limit,
// was classified transient, and a DEAD account was retried MaxRetries times
// (the 23:00 HP worker death, devdoc/night-report-20260910.md).
//
// Contract pinned here: the SAME over-8192 account-code body is classified
// account-level on BOTH branches — one send, no retry — and every probe site
// references the single constant non200BodyProbeMax.
//
// Deterministic: no network, no real sleeps (delays = 1ns).

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"
)

// probePadBytes is the padding pushed in front of the account code: it must
// exceed the pre-fix 8192-byte raw-branch cap (8 KiB) while staying well
// inside non200BodyProbeMax (64 KiB).
const probePadBytes = 12 * 1024

// paddedAccount429Body builds a GLM-shaped 429 body whose JSON envelope
// carries probePadBytes of pad BEFORE "code":"1113" — i.e. the pre-fix
// 8192-byte probe truncated the body before the account code and could not
// see it.
func paddedAccount429Body(t *testing.T) []byte {
	t.Helper()
	pad := strings.Repeat("x", probePadBytes)
	body := fmt.Sprintf(
		`{"type":"error","pad":%q,"error":{"type":"rate_limit_error","code":"1113","message":"[1113][余额不足或无可用资源包,请充值。]"}}`,
		pad)
	idx := bytes.Index([]byte(body), []byte(`"code":"1113"`))
	if idx <= 8192 {
		t.Fatalf("test fixture broken: account code at offset %d, must sit beyond 8192", idx)
	}
	if len(body) > non200BodyProbeMax {
		t.Fatalf("test fixture broken: body %d bytes exceeds the probe cap %d", len(body), non200BodyProbeMax)
	}
	return []byte(body)
}

// TestNon200ProbeCap_RawBranch_AccountCodeBeyond8192 drives retryDo's RAW
// *http.Response branch (the operation returns a response, not a sentinel):
// the 429 body carries "code":"1113" past offset 8192, so the raw branch's
// probe MUST read far enough to see it — exactly one call, no retry.
func TestNon200ProbeCap_RawBranch_AccountCodeBeyond8192(t *testing.T) {
	body := paddedAccount429Body(t)

	calls := 0
	c := &Client{
		BaseURL: "https://raw-branch.test",
		APIKey:  "test-key",
		RetryConfig: RetryConfig{
			MaxRetries:     3,
			InitialDelay:   time.Nanosecond,
			MaxDelay:       time.Nanosecond,
			JitterFraction: 0,
		},
	}
	operation := func() (*http.Response, error) {
		calls++
		return &http.Response{
			StatusCode: http.StatusTooManyRequests,
			Status:     "429 Too Many Requests",
			Header:     http.Header{},
			Body:       io.NopCloser(bytes.NewReader(body)),
		}, nil
	}

	_, err := c.retryDo(context.Background(), operation)

	// Pre-fix: calls == MaxRetries+1 == 4 (the invisible account code was
	// classified transient and the dead account was retried).
	if calls != 1 {
		t.Fatalf("raw branch retried an account-level 429: %d calls, want 1 "+
			"(the account code past byte 8192 must be visible)", calls)
	}
	if err == nil {
		t.Fatal("account-level 429 must fail (no response returned)")
	}
	if !isAccountLevel(err) {
		t.Fatalf("raw branch must classify as account-level, got: %v", err)
	}
}

// TestNon200ProbeCap_SentinelBranch_AccountCodeBeyond8192 drives the sentinel
// branch end-to-end through the public ClaudeChat (the operation closure
// builds the sentinel that retryDo classifies): same over-8192 body, one send.
func TestNon200ProbeCap_SentinelBranch_AccountCodeBeyond8192(t *testing.T) {
	body := paddedAccount429Body(t)

	sends := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sends++
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = w.Write(body) //stylegate:ignore writes to the test's own sink; the buffered content is the assertion
	}))
	defer srv.Close()

	c := newMatrixTestClient(t, srv)
	c.RetryConfig.MaxRetries = 3
	c.RetryConfig.InitialDelay = time.Nanosecond
	c.RetryConfig.MaxDelay = time.Nanosecond

	_, err := c.ClaudeChat(context.Background(), baseReq())
	if sends != 1 {
		t.Fatalf("sentinel branch retried an account-level 429: %d sends, want 1", sends)
	}
	if err == nil || !isAccountLevel(err) {
		t.Fatalf("sentinel branch must classify as account-level, got: %v", err)
	}
}

// TestNon200ProbeCap_StreamSentinelBranch_AccountCodeBeyond8192 is the
// streaming twin: ClaudeChatStream's sentinel branch must read the same body
// depth — one send, account-level.
func TestNon200ProbeCap_StreamSentinelBranch_AccountCodeBeyond8192(t *testing.T) {
	body := paddedAccount429Body(t)

	sends := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sends++
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = w.Write(body) //stylegate:ignore writes to the test's own sink; the buffered content is the assertion
	}))
	defer srv.Close()

	c := newMatrixTestClient(t, srv)
	c.RetryConfig.MaxRetries = 3
	c.RetryConfig.InitialDelay = time.Nanosecond
	c.RetryConfig.MaxDelay = time.Nanosecond

	req := baseReq()
	req.Stream = true
	_, err := c.ClaudeChatStream(context.Background(), req)
	if sends != 1 {
		t.Fatalf("stream sentinel branch retried an account-level 429: %d sends, want 1", sends)
	}
	if err == nil || !isAccountLevel(err) {
		t.Fatalf("stream sentinel branch must classify as account-level, got: %v", err)
	}
}

// TestNon200ProbeCap_Genuine429StillRetried is the both-ways guard: a 429 with
// NO account code and the same over-8192 padding is a genuine rate limit and
// must still be retried MaxRetries+1 times — the deeper probe must not turn
// every large body into an account-level error.
func TestNon200ProbeCap_Genuine429StillRetried(t *testing.T) {
	const maxRetries = 2
	pad := strings.Repeat("x", probePadBytes)
	body := []byte(fmt.Sprintf(
		`{"type":"error","pad":%q,"error":{"type":"rate_limit_error","message":"requests per minute exceeded"}}`, pad))

	calls := 0
	c := &Client{
		BaseURL: "https://raw-branch.test",
		APIKey:  "test-key",
		RetryConfig: RetryConfig{
			MaxRetries:     maxRetries,
			InitialDelay:   time.Nanosecond,
			MaxDelay:       time.Nanosecond,
			JitterFraction: 0,
		},
	}
	operation := func() (*http.Response, error) {
		calls++
		return &http.Response{
			StatusCode: http.StatusTooManyRequests,
			Header:     http.Header{},
			Body:       io.NopCloser(bytes.NewReader(body)),
		}, nil
	}

	if _, err := c.retryDo(context.Background(), operation); err == nil {
		t.Fatal("persistent genuine 429 must fail after retries")
	}
	if calls != maxRetries+1 {
		t.Fatalf("genuine 429 must stay retryable: %d calls, want %d", calls, maxRetries+1)
	}
}

// TestNon200ProbeCap_NoBareLimitReaderLiteral (PROJECT.md Rule 13 gate) — every
// body-probe site in this package MUST reference non200BodyProbeMax. A future
// re-inlined literal (`io.LimitReader(resp.Body, 8192)`) fails here instead of
// silently re-opening the drift.
func TestNon200ProbeCap_NoBareLimitReaderLiteral(t *testing.T) {
	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatalf("glob: %v", err)
	}
	// The constant's own definition site is the ONE allowed literal holder;
	// _test.go files are not production probe sites.
	probeCall := regexp.MustCompile(`io\.LimitReader\(`)
	for _, f := range files {
		if strings.HasSuffix(f, "_test.go") {
			continue
		}
		src, err := os.ReadFile(f)
		if err != nil {
			t.Fatalf("read %s: %v", f, err)
		}
		for i, line := range strings.Split(string(src), "\n") {
			if probeCall.MatchString(line) && !strings.Contains(line, "non200BodyProbeMax") {
				t.Errorf("%s:%d carries a body-probe cap that bypasses non200BodyProbeMax: %s",
					f, i+1, strings.TrimSpace(line))
			}
		}
	}
}
