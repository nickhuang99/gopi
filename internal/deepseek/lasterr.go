package deepseek

import (
	"net/http"
	"strings"
	"sync"
	"time"
)

// LastHTTPFailure is the mutable capture slot (mutex-guarded). Readers get
// plain FailureSnapshot VALUES (no lock inside — free to copy).
type LastHTTPFailure struct {
	mu      sync.Mutex
	current FailureSnapshot
	failed  bool
}

// FailureSnapshot is the immutable view of one failed HTTP round-trip (the
// thing session_end/dump consumers read; safe to copy by value).
type FailureSnapshot struct {
	// StatusCode is the HTTP status of the failed response (0 = transport
	// error before a response arrived — network refusal, timeout, ctx cancel).
	StatusCode int `json:"http_status,omitempty"`
	// Body is the (bounded) response body text. Empty for transport errors.
	Body string `json:"error_body,omitempty"`
	// TransportErr is the transport error text when StatusCode == 0.
	TransportErr string `json:"transport_error,omitempty"`
	// RequestPath is the URL path of the failed request.
	RequestPath string `json:"request_path,omitempty"`
	// Stream reports whether the failed request was the streaming variant.
	Stream bool `json:"stream,omitempty"`
	// Provider is the provider identity (base-URL host) of the failed
	// request. Recorded so the account-level remedy text can name WHICH
	// account is unusable (the crash dump travels without the client).
	Provider string `json:"provider,omitempty"`
	// Timestamp of the failure (UTC).
	Timestamp string `json:"timestamp,omitempty"`
}

// record stores one failure snapshot. Bounded work only: the body is
// tail-capped here (the dump layer caps the FULL request separately).
func (lf *LastHTTPFailure) record(status int, body []byte, path string, stream bool, terr error) {
	lf.mu.Lock()
	defer lf.mu.Unlock()
	lf.failed = true
	lf.current = FailureSnapshot{
		StatusCode:   status,
		Body:         tailStr(string(body), lastErrBodyMax),
		TransportErr: "",
		RequestPath:  path,
		Stream:       stream,
		Provider:     ProviderLabelForBase(path),
		Timestamp:    time.Now().UTC().Format(time.RFC3339),
	}
	if terr != nil {
		lf.current.TransportErr = terr.Error()
	}
}

// snapshot returns a copy of the current failure (zero value when nothing
// failed yet). Thread-safe: send paths record from retry attempts.
func (lf *LastHTTPFailure) snapshot() FailureSnapshot {
	lf.mu.Lock()
	defer lf.mu.Unlock()
	return lf.current
}

// HasFailed reports whether a failure was ever recorded.
func (lf *LastHTTPFailure) HasFailed() bool {
	lf.mu.Lock()
	defer lf.mu.Unlock()
	return lf.failed
}

// Snapshot returns a copy of the current failure (exported read side for the
// agent-loop collector; zero value when nothing failed yet).
func (lf *LastHTTPFailure) Snapshot() FailureSnapshot {
	return lf.snapshot()
}

// RecordForTest is a test seam: records a failure without going through the
// HTTP client (unit tests of the collector/dump pipeline).
func (lf *LastHTTPFailure) RecordForTest(status int, body []byte, stream bool) {
	lf.record(status, body, "/test/path", stream, nil)
}

// recordClientFailure is the package-level hook the send paths call.
func (c *Client) recordClientFailure(status int, body []byte, stream bool, terr error) {
	c.LastErrDetail.record(status, body, c.claudeMessagesURL(), stream, terr)
}

// Named constants — Rule 1.
const (
	// lastErrBodyMax caps the stored error body (the dump file keeps more;
	// this snapshot rides in every session_end event line).
	lastErrBodyMax = 8 * 1024
)

// tailStr returns at most max trailing bytes of s (head-trimmed with a
// marker), mirroring the raw-log tail convention.
func tailStr(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return "…(head trimmed)…" + s[len(s)-max:]
}

// ErrorCategory classifies a failure for the session_end error block and
// the task-ledger failed summary (bug036 matrix: account/vendor/network/4xx/5xx).
// The classification is derived purely from the recorded snapshot so every
// consumer (session_end, dump, task summary) agrees on one taxonomy.
type ErrorCategory string

// Category values (bug036 error taxonomy).
const (
	// CategoryNetwork = transport-level failures: connection refused/reset,
	// DNS, timeouts, context cancellation before a response arrived.
	CategoryNetwork ErrorCategory = "network"
	// CategoryHTTP5xx = server-side failures (retryable by retryDo policy).
	CategoryHTTP5xx ErrorCategory = "5xx"
	// CategoryHTTP4xx = client-side request failures (400/401/403/404/413/422/429...).
	CategoryHTTP4xx ErrorCategory = "4xx"
	// CategoryVendor = vendor-protocol errors that arrived in a 200 envelope
	// (SSE error event mid-stream — e.g. overloaded, quota), where no HTTP
	// status marks the failure.
	CategoryVendor ErrorCategory = "vendor"
	// CategoryAccount = ACCOUNT-LEVEL failures: the provider account itself
	// is unusable (out of balance, dead credential), so no retry, no backoff
	// and no same-provider fallback can help. It is derived from the SAME
	// predicate the retry path uses (isAccountLevelStatus) — never from a
	// second copy of the vendor-code knowledge (Rule 13/14). It takes
	// precedence over the HTTP category: a 429/4xx carrying an account code
	// is an ACCOUNT failure that merely wore a 429's clothes.
	CategoryAccount ErrorCategory = "account"
	// CategoryPanic = runtime panic recovered by the runEngine defer.
	CategoryPanic ErrorCategory = "panic"
	// CategoryUnknown = failure with neither status, body, nor transport text.
	CategoryUnknown ErrorCategory = "unknown"
	// CategorySignal = the run was ended by a CATCHABLE SIGNAL (SIGHUP on a
	// terminal teardown, SIGTERM from a supervisor) that this process handled
	// and unwound through its own close-out. It is NOT a failure of the request:
	// nothing was in flight and nothing about the provider is wrong.
	//
	// WHY IT IS ITS OWN CATEGORY (measured, session ab9e0023, 2026-10-21). A
	// handled hangup closed the chat through the ordinary close-out with a
	// non-zero exit code, and the collector — which has an arm only for RUN
	// ERRORS — filled in the unknown default, because there was no error to
	// classify. The session's own artifacts then said "category=unknown" beside
	// a clean, deliberate close-out: an operator reading them after the fact
	// cannot tell a terminal teardown from a provider failure, which is the
	// "disappeared without a trace" report this category exists to end. The
	// signal's NAME rides the record's error body, so the cause is READ, not
	// inferred (Rule 15a: "nothing recognised the failure" must not be the
	// word used for a cause that was known exactly).
	CategorySignal ErrorCategory = "signal"
)

// Category derives the bug036 category for this failure snapshot.
func (f FailureSnapshot) Category() ErrorCategory {
	// Account-level FIRST: the provider signaled that the account itself is
	// unusable. Its HTTP status can be ambiguous (GLM hides "余额不足" behind
	// the same 429 a rate limit uses) or itself meaningful (402 Payment
	// Required), so status alone must never decide this one.
	if IsAccountLevelStatus(f.StatusCode, []byte(f.Body)) {
		return CategoryAccount
	}
	switch {
	case f.StatusCode == 0:
		if f.TransportErr != "" {
			return CategoryNetwork
		}
		if strings.Contains(f.Body, "stream error") || f.Body != "" {
			return CategoryVendor
		}
		return CategoryUnknown
	case f.StatusCode >= 500:
		return CategoryHTTP5xx
	case f.StatusCode >= 400:
		return CategoryHTTP4xx
	default:
		return CategoryVendor
	}
}

// RecoverableHint reports whether the session can plausibly be resumed
// (the heuristic the session_end error block carries for the PA/resumer):
//   - account: NO — the account/credential is unusable; resuming re-hits the
//     same dead account. The operator must act (remedy: see RemedyText).
//   - network / 5xx: yes — the provider was unhealthy, the request was fine.
//   - 429: yes — quota/backoff, retry later.
//   - 4xx: no by default EXCEPT 400-1214 (request shape — the known compact
//     degradation ladder case) which is yes.
//   - vendor: yes — mid-stream vendor errors are often transient.
//   - unknown: no (cannot reason about what failed).
func (f FailureSnapshot) RecoverableHint() bool {
	switch f.Category() {
	case CategoryAccount:
		// Checked BEFORE the 429 clause below: a 429 that carries an account
		// code is an account failure, not a rate limit (this ordering is the
		// whole point of the account category).
		return false
	case CategoryNetwork, CategoryHTTP5xx, CategoryVendor:
		return true
	case CategoryHTTP4xx:
		if f.StatusCode == http.StatusTooManyRequests {
			return true
		}
		return strings.Contains(f.Body, "1214") // the compact-shape class
	default:
		return false
	}
}

// IsAccountLevelStatus reports whether an HTTP status + error body pair is an
// ACCOUNT-LEVEL failure (the account/credential is unusable: out of balance,
// dead credential). It is the taxonomy's window onto the single predicate the
// retry path already consults — no second copy of the vendor-code knowledge
// (Rule 13/14).
func IsAccountLevelStatus(status int, body []byte) bool {
	return isAccountLevelStatus(status, body)
}

// RemedyText renders the operator-facing condition + remedy for an
// ACCOUNT-LEVEL snapshot, and "" for every other failure. It is the taxonomy
// contract the crash dump / session_end / notify layers carry, so a dump
// distinguishes "rate limited, retry later" from "account out of balance,
// top up or switch provider".
func (f FailureSnapshot) RemedyText() string {
	// The carrier is the account-level condition itself: build the message
	// from the raw status+body the same way the retry path does, so both
	// surfaces word the failure identically.
	return AccountLevelMessage(&HTTPStatusError{
		Status:       f.StatusCode,
		Body:         []byte(f.Body),
		ProviderBase: f.Provider,
	})
}
