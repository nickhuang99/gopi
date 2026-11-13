package deepseek

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"math"
	"math/rand"
	"net/http"
	"strconv"
	"time"

	"github.com/nick/gopi/internal/config"
)

// non200BodyProbeMax caps how many bytes of a non-200 response body are read
// for classification/logging. It is deliberately ONE constant for every probe
// site — retryDo's raw *http.Response branch AND the ClaudeChat /
// ClaudeChatStream sentinel branches that feed retryDo.
//
// Why one constant (PROJECT.md Rule 13): isAccountLevelStatus only sees the bytes
// it is handed, so two drifted caps — a 8192-byte raw read versus a 65536-byte
// sentinel read — made a vendor account code sitting past byte 8192 INVISIBLE
// on the raw branch. The 429 was then classified transient and a dead account
// (GLM 1113) was retried MaxRetries times (the 23:00 HP worker death,
// devdoc/night-report-20260910.md). The cap must stay larger than any
// plausible provider error envelope; raise it HERE, never inline.
const non200BodyProbeMax = 64 * 1024

// RetryConfig controls the retry behaviour of the DeepSeek HTTP client.
type RetryConfig struct {
	// MaxRetries is the maximum number of retry attempts (default 3).
	MaxRetries int
	// InitialDelay is the base backoff duration for the first retry (default 1s).
	InitialDelay time.Duration
	// MaxDelay is the ceiling for exponential backoff (default 30s).
	MaxDelay time.Duration
	// JitterFraction is the fraction of the current backoff to randomise (±).
	// A value of 0.25 means the delay is varied uniformly in [0.75×delay, 1.25×delay].
	JitterFraction float64
}

// DefaultRetryConfig returns the standard retry configuration.
func DefaultRetryConfig() RetryConfig {
	return RetryConfig{
		MaxRetries:     3,
		InitialDelay:   1 * time.Second,
		MaxDelay:       30 * time.Second,
		JitterFraction: 0.25,
	}
}

// retryableStatus and isNetworkError now live in error_matrix.go (the
// provider-neutral classification matrix).

// backoff returns the delay for the nth retry attempt (0-indexed).
// delay = min(MaxDelay, InitialDelay * 2^n) with ±JitterFraction applied.
func (cfg RetryConfig) backoff(attempt int) time.Duration {
	d := time.Duration(float64(cfg.InitialDelay) * math.Pow(2, float64(attempt)))
	if d > cfg.MaxDelay {
		d = cfg.MaxDelay
	}
	if cfg.JitterFraction > 0 {
		low := float64(d) * (1 - cfg.JitterFraction)
		high := float64(d) * (1 + cfg.JitterFraction)
		d = time.Duration(low + rand.Float64()*(high-low))
	}
	return d
}

// retryAfterDelay computes the retry delay from a raw response, preferring
// the server's Retry-After header (429 only) over the exponential schedule.
// Kept for direct-retryDo callers that return (resp, nil) on non-200.
func (cfg RetryConfig) retryAfterDelay(resp *http.Response, attempt int) time.Duration {
	if resp.StatusCode == http.StatusTooManyRequests {
		return cfg.retryDelay(parseRetryAfter(resp), attempt)
	}
	return cfg.retryDelay(0, attempt)
}

// retryDelay returns the delay before the given retry attempt: the server's
// Retry-After directive (delta-seconds, 0 = none) takes precedence over the
// local exponential backoff schedule — delay = max(backoff, retryAfter) — so
// the client never re-hammers before the server's requested wait elapses and
// never waits less than its own schedule. Retry-After is capped at MaxDelay:
// a backstop against a multi-hour rate-limit window stalling the retry loop
// (the cap mirrors the pre-5c6f5ca retryAfterDelay behaviour).
func (cfg RetryConfig) retryDelay(retryAfterSecs, attempt int) time.Duration {
	delay := cfg.backoff(attempt)
	if retryAfterSecs <= 0 {
		return delay
	}
	ra := time.Duration(retryAfterSecs) * time.Second
	if ra > cfg.MaxDelay {
		ra = cfg.MaxDelay
	}
	if ra > delay {
		return ra
	}
	return delay
}

// parseRetryAfter reads the Retry-After header value.
// Handles both delta-seconds (integer) and HTTP-date formats.
func parseRetryAfter(resp *http.Response) int {
	v := resp.Header.Get("Retry-After")
	if v == "" {
		return 0
	}
	// Try delta-seconds first.
	if secs, err := strconv.Atoi(v); err == nil {
		return secs
	}
	// Try HTTP-date.
	t, err := http.ParseTime(v)
	if err != nil {
		return 0
	}
	d := time.Until(t)
	if d <= 0 {
		return 1 // already past — wait at least 1s
	}
	return int(math.Ceil(d.Seconds()))
}

// debugLogEnabled lazily parses GOPI_DEBUG with config.EnvBool (the single
// truthy parser — "1", "on", "yes", "true", "enabled"; anything else off).
// Two constraints make a package-init snapshot WRONG here (t-1788053264-3):
// (a) `os.Getenv(...) != ""` treated the canonical "off" that ApplyEnv
// force-writes for disabled switches as ENABLED (same regression agent
// fixed in boolEnvTrue 2026-08-29); (b) package vars evaluate BEFORE main
// runs config.ApplyEnv, so settings.json "debug": true (which main writes
// into GOPI_DEBUG=1 at runtime) never reached the snapshot — retry logging
// stayed dark exactly when a settings user needed it. Reading on every call
// fixes both; logRetry already short-circuits before formatting.
func debugLogEnabled() bool {
	return config.EnvBool("GOPI_DEBUG")
}

// logRetry emits a log line describing a retry attempt.
func logRetry(attempt, maxRetries int, reason string, delay time.Duration, detail string) {
	if !debugLogEnabled() {
		return
	}
	if len(detail) > 200 {
		detail = detail[:200] + "..."
	}
	log.Printf("[GOPI] retry %d/%d — %s — waiting %s — %s",
		attempt+1, maxRetries, reason, delay.Round(time.Millisecond), detail)
}

// sentinelAPIError carries a non-OK HTTP status out of the caller's operation
// closure so retryDo can classify it by HTTP semantics (transient vs
// deterministic) instead of string-matching the error message. Historical
// bug: the closure returned a formatted "API error (status 500)" string and
// isNetworkError() saw neither timeout nor refused → retryDo treated every
// 5xx as non-retryable. The classification matrix (error_matrix.go) needs the
// STATUS, not prose.
type sentinelAPIError struct {
	status int
	body   []byte
	// providerBase is the failing request's base URL — carried so the
	// account-level operator message can name the provider/credential that
	// is unusable (the sentinel is the only carrier out of the operation
	// closure, same reason as retryAfter).
	providerBase string
	// retryAfter holds the server's Retry-After directive parsed to
	// delta-seconds (0 = header absent/unparsable). Captured in the
	// operation closure because the sentinel is the ONLY carrier out of
	// it — the header itself would otherwise be dropped before retryDo
	// sees the error (5c6f5ca regression: every caller returns the
	// sentinel for any non-200, so the raw-*http.Response Retry-After
	// branch is unreachable).
	retryAfter int
}

func (e *sentinelAPIError) Error() string {
	return fmt.Sprintf("deepseek: API error (status %d): %s", e.status, trimmedBody(e.body))
}

// retryDo wraps an HTTP operation with retry logic.
//
// The operation returns an *http.Response and any error. retryDo inspects
// the status code and error to decide whether to retry, by the
// provider-neutral classification matrix (error_matrix.go):
//
//	network errors            → retry with exponential backoff
//	HTTP 429 / 5xx            → transient: retry with backoff (Retry-After honored)
//	HTTP 4xx (except 429)     → deterministic: returned immediately (the
//	                            caller owns the one-shot minimal-core degradation)
//
// On success (HTTP 200) the *http.Response is returned with its Body open —
// the caller is responsible for closing it. On failure the body (if any) is
// already closed.
func (c *Client) retryDo(
	ctx context.Context,
	operation func() (*http.Response, error),
) (*http.Response, error) {
	cfg := c.RetryConfig
	var lastErr error

	for attempt := 0; attempt <= cfg.MaxRetries; attempt++ {
		// Check for context cancellation before each attempt (except the first,
		// which should be attempted even if the deadline is near).
		if attempt > 0 {
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			default:
			}
		}

		resp, err := operation()

		// — transport / classified error —
		if err != nil {
			lastErr = err
			// Unwrap the sentinel: a non-OK HTTP status is DATA, classified
			// by the matrix — not a transport failure.
			var apiErr *sentinelAPIError
			if errors.As(err, &apiErr) {
				// ACCOUNT-LEVEL (402 billing / vendor account code in a
				// 429-4xx envelope): the credential itself is unusable.
				// Fail fast — no retry, no same-provider retry, and an
				// actionable operator message. Evidence: the 23:00 HP worker
				// death burned 3 retries + a same-credential "fallback" on
				// GLM 1113 (devdoc/night-report-20260910.md).
				if isAccountLevelStatus(apiErr.status, apiErr.body) {
					return nil, accountLevelError(apiErr)
				}
				lastErr = fmt.Errorf("%s after classification", err)
				switch classifyHTTPError(apiErr.status, apiErr.body, nil) {
				case classDeterministic:
					// Never retried verbatim — surface for the caller's
					// one-shot minimal-core degradation.
					return nil, err
				case classTransient:
					if attempt >= cfg.MaxRetries {
						return nil, fmt.Errorf("%s after %d retries", err, cfg.MaxRetries)
					}
					delay := cfg.retryDelay(apiErr.retryAfter, attempt)
					logRetry(attempt, cfg.MaxRetries, fmt.Sprintf("HTTP %d", apiErr.status), delay, "")
					sleep(ctx, delay)
					continue
				default:
					return nil, err
				}
			}
			if !isNetworkError(err) {
				return nil, err
			}
			if attempt >= cfg.MaxRetries {
				return nil, fmt.Errorf("deepseek: request failed after %d retries: %w", cfg.MaxRetries, err)
			}
			delay := cfg.backoff(attempt)
			logRetry(attempt, cfg.MaxRetries, "network error", delay, err.Error())
			sleep(ctx, delay)
			continue
		}

		// — success —
		if resp.StatusCode == http.StatusOK {
			return resp, nil
		}

		// — HTTP error —
		// Read the body for the error message, then close it — the caller won't.
		bodyBytes, _ := io.ReadAll(io.LimitReader(resp.Body, non200BodyProbeMax)) //stylegate:ignore bounded read of an error/diagnostic body; a short read is acceptable
		resp.Body.Close()
		bodyStr := string(bodyBytes)
		if len(bodyStr) > maxErrorDisplayBytes {
			bodyStr = bodyStr[:maxErrorDisplayBytes] + "..."
		}
		lastErr = fmt.Errorf("deepseek: API error (status %d): %s", resp.StatusCode, bodyStr)

		// ACCOUNT-LEVEL: the credential is unusable (402 billing, or a
		// vendor account code hidden inside a 429/4xx). Never retried, and
		// surfaced as a raw-status error so the caller can refuse a
		// same-provider fallback and print the operator remedy.
		if isAccountLevelStatus(resp.StatusCode, bodyBytes) {
			return nil, accountLevelError(&sentinelAPIError{
				status:       resp.StatusCode,
				body:         bodyBytes,
				providerBase: c.BaseURL,
			})
		}

		if !retryableStatus(resp.StatusCode) {
			return nil, lastErr
		}

		if attempt >= cfg.MaxRetries {
			return nil, fmt.Errorf("%s after %d retries", lastErr, cfg.MaxRetries)
		}

		delay := cfg.retryAfterDelay(resp, attempt)
		logRetry(attempt, cfg.MaxRetries, fmt.Sprintf("HTTP %d", resp.StatusCode), delay, "")
		sleep(ctx, delay)
	}

	return nil, lastErr
}

// accountLevelError converts a raw HTTP failure into the exported,
// account-level error value: the wrapped *HTTPStatusError carries the status
// + body (so any consumer can re-classify without string matching) and the
// error TEXT is the actionable operator message naming the condition and the
// remedy. Returned unchanged by every layer above — that is what makes
// "fail fast, do not fall back" a property of the error itself rather than a
// convention each caller must remember.
func accountLevelError(apiErr *sentinelAPIError) error {
	return &accountLevelErrorValue{HTTPStatusError: &HTTPStatusError{
		Status:       apiErr.status,
		Body:         apiErr.body,
		ProviderBase: apiErr.providerBase,
	}}
}

// accountLevelErrorValue is the concrete account-level error: an
// *HTTPStatusError whose Error() IS the operator message. Keeping the value
// type unexported means callers can only obtain one from the client (never
// construct a fake account-level failure), while errors.As on
// *HTTPStatusError still works.
type accountLevelErrorValue struct {
	*HTTPStatusError
}

func (e *accountLevelErrorValue) Error() string { return AccountLevelMessage(e.HTTPStatusError) }

// Unwrap exposes the raw status error so errors.As(err, **HTTPStatusError)
// and errors.Is chains keep working through the account-level wrapper.
func (e *accountLevelErrorValue) Unwrap() error { return e.HTTPStatusError }

// IsAccountLevelError reports whether err is an account-level provider
// failure — the predicate the agent layer uses to SKIP a same-provider
// fallback (retrying the same dead credential cannot succeed).
func IsAccountLevelError(err error) bool {
	return isAccountLevel(err)
}

// AccountLevelMessageOf returns the operator-facing message for an
// account-level error, or "" when err is not account-level.
func AccountLevelMessageOf(err error) string { return AccountLevelMessage(err) }

// sleep pauses for the given duration or until ctx is cancelled.
func sleep(ctx context.Context, d time.Duration) {
	timer := time.NewTimer(d)
	select {
	case <-timer.C:
	case <-ctx.Done():
		timer.Stop()
	}
}
