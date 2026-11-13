package deepseek

// Error classification matrix — provider-neutral HTTP semantics (Epic
// t-1788121946 redesign). The superseded v1 design special-cased one
// vendor's error marker ("1214"); that was vendor tunnel-vision. This file
// classifies failures by HTTP semantics only — no vendor codes, no vendor
// branches:
//
//	4xx (except 429)  → deterministic: the request as shaped will NEVER be
//	                    accepted. One degradation re-send (minimal core);
//	                    still 4xx → fail fast with the request dumped.
//	429 / 5xx         → transient: retry with exponential backoff (existing
//	                    retryDo behaviour).
//	network error     → transient: same retry loop (existing isNetworkError).
//
// Per-provider quirks (GLM always-thinking, DeepSeek thinking:disabled, …)
// live in CONFIG DATA at the request-construction layer (claudeClientOptions
// / settings), never in `if vendor` logic here.

import (
	"net"
	"net/http"
	"strings"
)

// errorClass is the provider-neutral failure classification.
type errorClass int

const (
	// classUnknown covers nil errors and anything unclassifiable.
	classUnknown errorClass = iota
	// classNetwork: transport-level failure (timeout, refused, reset, DNS).
	// May be transient — retried with backoff.
	classNetwork
	// classTransient: HTTP 429 / 5xx — the server asked us to back off or is
	// unhealthy. Retried with exponential backoff (Retry-After honored).
	classTransient
	// classDeterministic: HTTP 4xx (except 429). The request itself is
	// rejected; retrying verbatim cannot succeed. One minimal-core
	// degradation re-send, then fail fast.
	classDeterministic
)

// classifyNetworkError reports whether err is a network-level error
// (timeout, connection refused, DNS failure, etc.) that may be transient.
// Exported alias kept for the existing call sites/tests.
func isNetworkError(err error) bool {
	if err == nil {
		return false
	}
	if _, ok := err.(*net.OpError); ok {
		return true
	}
	if _, ok := err.(*net.DNSError); ok {
		return true
	}
	s := err.Error()
	if strings.Contains(s, "timeout") || strings.Contains(s, "deadline exceeded") {
		return true
	}
	if strings.Contains(s, "connection refused") || strings.Contains(s, "connection reset") {
		return true
	}
	return false
}

// classifyHTTPError classifies a failure by HTTP semantics:
//   - transport err            → classNetwork
//   - HTTP 429 / 5xx           → classTransient
//   - HTTP 4xx (except 429)    → classDeterministic
//   - anything else            → classUnknown
//
// body participates only for logging/observability — the classification is
// status-code driven, never vendor-error-code driven.
func classifyHTTPError(status int, body []byte, err error) errorClass {
	_ = body // reserved for diagnostics; classification is HTTP-semantic only
	switch {
	case err != nil:
		if isNetworkError(err) {
			return classNetwork
		}
		return classUnknown
	case status == http.StatusTooManyRequests, status >= 500:
		return classTransient
	case status >= 400:
		return classDeterministic
	default:
		return classUnknown
	}
}

// retryableStatus reports whether an HTTP status should trigger a retry
// (transient class: 429 + all 5xx). Deterministic 4xx never retries
// verbatim — it degrades once, then fails.
func retryableStatus(code int) bool {
	return code == http.StatusTooManyRequests || code >= 500
}

// isDeterministicHTTPError reports whether status+err pair is a 4xx-class
// (deterministic) failure needing the minimal-core degradation path.
func isDeterministicHTTPError(status int, err error) bool {
	return err == nil && status >= 400 && status < 500 &&
		status != http.StatusTooManyRequests
}

// minCoreBodyCap caps the error-response tail kept in the fail-fast error.
const minCoreBodyCap = 500
