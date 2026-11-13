package deepseek

// Account-level (non-retryable, credential/provider-bound) error
// classification.
//
// WHY (evidence): the 23:00 HP worker death log
// (~/.gopi/worker-night-1.log, quoted in devdoc/night-report-20260910.md):
//
//	deepseek: API error (status 429): {"type":"error","error":{
//	  "type":"rate_limit_error","code":"1113",
//	  "message":"[1113][余额不足或无可用资源包,请充值。]"}}
//	  after 3 retries, fallback: deepseek: API error (status 429): ... after 3 retries
//
// The provider signals "account out of balance" with HTTP 429 — the SAME
// status it uses for a genuine rate limit. Status-only classification
// therefore (a) burned 3 backoff retries and (b) "fell back" to the SAME
// provider/credential, which cannot possibly succeed. Every one of those
// attempts is pure wall-clock loss on a dead account.
//
// The fix is two-layered:
//
//  1. HTTP semantics first (provider-neutral, error_matrix.go): 402 Payment
//     Required is the standard status for a billing failure — classified
//     account-level for EVERY provider, no vendor knowledge needed.
//  2. Vendor account codes second (below): a provider that reports account
//     exhaustion inside an otherwise-transient status (GLM 1113 behind a 429)
//     is caught by one named set of codes behind ONE predicate.
//
// A code found in the vendor set is treated as account-level ONLY when the
// enclosing HTTP status is one a request could not have earned: a 429 (rate
// limit — where "insufficient balance" hides) or a 4xx. A code that shows up
// in a 5xx envelope is a SERVER failure (5xx is transient by definition) and
// stays retryable — a false "account dead, don't retry" is worse than a
// wasted retry.

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"
)

// thin wrappers keep the classification file testable in isolation and give
// every probe one place to change (Rule 14: one definition, many consumers).

// errorsAs is errors.As behind a local name (classification seam).
func errorsAs(err error, target interface{}) bool { return errors.As(err, target) }

// jsonUnmarshal is json.Unmarshal behind a local name (error-body probe seam).
func jsonUnmarshal(data []byte, v interface{}) error { return json.Unmarshal(data, v) }

// HTTPStatusError is a raw HTTP failure (status + body) as a first-class
// error value: the exported carrier the client returns for account-level
// failures and the entry point every consumer classifies. Keeping the raw
// status+body ON the error is what lets a caller decide "retry vs fail fast"
// without string-matching the message.
type HTTPStatusError struct {
	// Status is the HTTP status code of the failed response.
	Status int
	// Body is the (bounded) provider error body.
	Body []byte
	// ProviderBase is the request's base URL — the provider identity used in
	// the operator-facing message (host label).
	ProviderBase string
}

func (e *HTTPStatusError) Error() string {
	return "deepseek: API error (status " + strconv.Itoa(e.Status) + "): " + trimmedBody(e.Body)
}

// providerLabel names the failing credential's provider for the operator
// message. GoPi routes every Anthropic-wire request through this client, so
// the request's base URL is the only provider identity available at the
// failure site; an empty/unparsable base degrades to a generic label rather
// than an empty string (Rule 15a — never emit a nameless condition).
func providerLabel(e *HTTPStatusError) string {
	if e == nil {
		return providerLabelGeneric
	}
	return ProviderLabelForBase(e.ProviderBase)
}

// providerLabelGeneric is the fallback provider name when the failing base
// URL is unknown — the message must still name a provider.
const providerLabelGeneric = "provider"

// ProviderLabelForBase extracts a human provider label (host) from a base
// URL. Exported so callers holding a URL can label their own messages.
func ProviderLabelForBase(base string) string {
	b := strings.ToLower(strings.TrimSpace(base))
	if b == "" {
		return providerLabelGeneric
	}
	host := b
	if i := strings.Index(b, "//"); i >= 0 {
		host = b[i+2:]
	}
	if i := strings.IndexAny(host, "/?"); i >= 0 {
		host = host[:i]
	}
	if host == "" {
		return providerLabelGeneric
	}
	return host
}

// Account-level error classification constants — Rule 1 (no magic numbers /
// bare string keywords; every literal used as a VALUE is named here).
const (
	// vendorAccountCodeGLMBalance is Zhipu GLM's "账户余额不足或无可用资源包"
	// (account out of balance / no usable resource pack) error code. GLM
	// reports it with HTTP 429 and an Anthropic-wire body of the shape
	// {"type":"error","error":{"type":"rate_limit_error","code":"1113",...}}.
	// Remedy: top up the account or switch provider — retrying cannot help.
	vendorAccountCodeGLMBalance = "1113"

	// errorBodyProbeMax caps how much of an error body is parsed/inspected.
	// Error envelopes are small (a code + a message); the cap is a backstop
	// against a provider echoing a huge body into the 429.
	errorBodyProbeMax = 64 * 1024

	// jsonFieldError and jsonFieldCode are the JSON keys probed in a provider
	// error envelope — the Anthropic-wire shape nests "code" under "error";
	// the OpenAI-wire shape puts it at the top level.
	jsonFieldError = "error"
	jsonFieldCode  = "code"
)

// vendorCodeFieldErrorCode is the OpenAI-wire provider code key (gateways
// in the OpenAI-compatible family use "error_code").
const vendorCodeFieldErrorCode = "error_code"

// vendorAccountErrorCodes is the single source of truth for "this vendor code
// means the ACCOUNT (not the request) is unusable". Add a code by adding one
// entry here — no predicate edits, no per-provider branches.
var vendorAccountErrorCodes = map[string]VendorErrorInfo{
	vendorAccountCodeGLMBalance: {
		Remedy: "top up the provider account or switch to another provider",
		Label:  "account out of balance (no usable resource pack)",
	},
}

// VendorErrorInfo describes one vendor account-level code: the operator-facing
// remedy and a human label for the condition.
type VendorErrorInfo struct {
	// Remedy is the actionable operator instruction.
	Remedy string
	// Label names the condition in prose.
	Label string
}

// isAccountLevel reports whether err denotes an ACCOUNT-LEVEL failure —
// the provider/credential itself is unusable, so retrying (and falling back
// to the same provider) is pure waste. It is the general, provider-neutral
// predicate the retry path consults.
//
// Account-level means:
//
//	HTTP 402 Payment Required            → the standard billing-failure status
//	vendor account code (see map) in a    → e.g. GLM 1113 "余额不足" arriving
//	  429 or 4xx envelope                    as a 429 rate_limit_error
//
// Everything else — genuine 429 rate limits, 5xx, transport errors, plain
// 4xx shape rejections — is NOT account-level and keeps its existing
// treatment (retry / minimal-core degradation).
func isAccountLevel(err error) bool {
	apiErr, ok := asAPIError(err)
	if !ok {
		return false
	}
	return isAccountLevelStatus(apiErr.Status, apiErr.Body)
}

// asAPIError unwraps an error chain to the raw HTTP failure it carries.
// sentinelAPIError is the client's own carrier (produced inside retryDo's
// operation closures); *HTTPStatusError is the exported equivalent that
// leaves the package. Both expose status + body, so classification works on
// either side of the boundary.
func asAPIError(err error) (*HTTPStatusError, bool) {
	if err == nil {
		return nil, false
	}
	var httpErr *HTTPStatusError
	if errorsAs(err, &httpErr) {
		return httpErr, true
	}
	var sentinel *sentinelAPIError
	if errorsAs(err, &sentinel) {
		// Re-home into the exported carrier so callers outside this package
		// can classify the raw status+body too.
		return &HTTPStatusError{Status: sentinel.status, Body: sentinel.body, ProviderBase: sentinel.providerBase}, true
	}
	return nil, false
}

// isAccountLevelStatus reports whether an HTTP status + error body pair is an
// account-level failure. Exported entry point for the agent layer, which
// classifies the error surfaced by the client (and keeps the CLI/provider
// selection logic out of this package).
func isAccountLevelStatus(status int, body []byte) bool {
	if status == http.StatusPaymentRequired {
		// Provider-neutral: 402 means "pay up", whichever vendor sends it.
		return true
	}
	// A vendor account code only counts inside a status the REQUEST could
	// not have earned: 429 (where GLM hides 1113) or another 4xx. In a 5xx
	// envelope the code would be server-side noise — leave it transient.
	if status != http.StatusTooManyRequests && (status < 400 || status >= 500) {
		return false
	}
	_, ok := lookupVendorAccountCode(body)
	return ok
}

// LookupVendorAccountCode parses the vendor error code out of an error body
// and returns its operator-facing info when the code is account-level.
// Exported so the message builder can name the code, label and remedy.
func LookupVendorAccountCode(body []byte) (VendorErrorInfo, bool) {
	return lookupVendorAccountCode(body)
}

// lookupVendorAccountCode is the single lookup: parse the code, then resolve
// it against vendorAccountErrorCodes.
func lookupVendorAccountCode(body []byte) (VendorErrorInfo, bool) {
	code := parseVendorErrorCode(body)
	if code == "" {
		return VendorErrorInfo{}, false
	}
	info, ok := vendorAccountErrorCodes[code]
	return info, ok
}

// parseVendorErrorCode extracts the provider error code from a JSON error
// body, tolerating every shape seen on the wire:
//
//	{"type":"error","error":{"type":"rate_limit_error","code":"1113","message":"..."}}
//	{"error":{"code":"1113"}}                      (Anthropic-wire)
//	{"code":"1113","message":"..."}                (OpenAI-wire flat)
//	{"code":1113}                                  (numeric code)
//	{"error_code":"1113"}                          (gateway variant)
//
// Returns "" for an unparsable body (SSE, HTML, plain text) — a non-JSON
// body can never be mistaken for an account code.
func parseVendorErrorCode(body []byte) string {
	if len(body) == 0 {
		return ""
	}
	if len(body) > errorBodyProbeMax {
		body = body[:errorBodyProbeMax]
	}
	var env map[string]interface{}
	if err := jsonUnmarshal(body, &env); err != nil {
		return ""
	}
	// Only the NESTED envelope, the flat top-level "code" and the gateway
	// "error_code" carry codes. A top-level "error" that is NOT an object is
	// prose ("error":"plain string"), never a code — treating it as one would
	// let arbitrary text impersonate a vendor account code.
	if code := codeFromValue(env[vendorCodeFieldErrorCode]); code != "" {
		return code
	}
	if code := codeFromValue(env[jsonFieldCode]); code != "" {
		return code
	}
	return parseNestedErrorCode(env)
}

// parseNestedErrorCode reads the code out of the nested "error" object
// (Anthropic-wire: {"error":{"code":"1113"}}). A non-object "error" value is
// ignored.
func parseNestedErrorCode(env map[string]interface{}) string {
	nested, ok := env[jsonFieldError].(map[string]interface{})
	if !ok {
		return ""
	}
	return codeFromValue(nested[jsonFieldCode])
}

// codeFromValue renders a JSON code value as a canonical string: string
// values verbatim, numbers via their integer form (JSON decodes numbers to
// float64, so 1113 arrives as 1113.0).
func codeFromValue(v interface{}) string {
	switch t := v.(type) {
	case string:
		return strings.TrimSpace(t)
	case float64:
		return strconv.FormatFloat(t, 'f', -1, 64)
	case map[string]interface{}:
		return codeFromValue(t[jsonFieldCode])
	default:
		return ""
	}
}

// AccountLevelMessage builds the operator-facing failure text for an
// account-level provider error: it names the CONDITION (what the provider
// said, with the vendor code) and the REMEDY (what the operator must do) and
// states plainly that no retry happened and none would help.
//
// Shape (all fields on one line, greppable by the leading marker):
//
//	ACCOUNT-LEVEL: provider account unusable — <label> (code 1113). Not
//	retrying and no provider fallback can help: <remedy>.
//
// Empty when the error is not account-level, so callers can write
// `if msg := ...; msg != ""`.
func AccountLevelMessage(err error) string {
	apiErr, ok := asAPIError(err)
	if !ok || !isAccountLevelStatus(apiErr.Status, apiErr.Body) {
		return ""
	}
	label, remedy := accountLevelCondition(apiErr.Status, apiErr.Body)
	code := parseVendorErrorCode(apiErr.Body)
	var sb strings.Builder
	sb.WriteString(accountLevelMessagePrefix)
	sb.WriteString(providerLabel(apiErr))
	sb.WriteString(" account unusable — ")
	sb.WriteString(label)
	if code != "" {
		sb.WriteString(" (code ")
		sb.WriteString(code)
		sb.WriteString(")")
	}
	sb.WriteString(" [HTTP ")
	sb.WriteString(strconv.Itoa(apiErr.Status))
	sb.WriteString("]. Not retrying and no provider fallback can help: ")
	sb.WriteString(remedy)
	sb.WriteString(".")
	return sb.String()
}

// accountLevelMessagePrefix leads every account-level failure message — the
// single greppable anchor operators and the notify/ledger layers key on.
const accountLevelMessagePrefix = "[GOPI][account-level] ACCOUNT-LEVEL ERROR: "

// AccountLevelMessagePrefix is the exported anchor (agent layer, tests).
const AccountLevelMessagePrefix = accountLevelMessagePrefix

// accountLevelCondition returns the human label + remedy for an
// account-level status/body pair, falling back to a generic billing wording
// for a bare 402 (no vendor code to elaborate on).
func accountLevelCondition(status int, body []byte) (label, remedy string) {
	if info, ok := lookupVendorAccountCode(body); ok {
		return info.Label, info.Remedy
	}
	if status == http.StatusPaymentRequired {
		return "payment required", "settle the billing issue (top up or fix the payment method) or switch to another provider"
	}
	// Unreachable by construction (isAccountLevelStatus gates the call), but
	// never silently return an empty remedy — Rule 15.
	return "account-level provider rejection", "check the provider account status (balance / credentials) or switch to another provider"
}
