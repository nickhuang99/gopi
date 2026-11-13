package deepseek

// Minimal-core degradation — the provider-neutral answer to any deterministic
// HTTP 4xx. The minimal core (model + messages + max_tokens + stream) is the
// greatest common denominator of every LLM chat API: all optional/extension
// parameters are stripped. One shape that DeepSeek (empty-thinking 400),
// Anthropic (400 invalid_request) and GLM (1214 messages-shape) all accept.
//
// Design rules (Epic t-1788121946):
//   - ONE degradation re-send per deterministic 4xx — then fail fast with
//     the full request dumped for replay/resume.
//   - NO vendor branches: the same core is sent to every provider. What each
//     provider needs by default lives in config data at request-construction
//     time (claudeClientOptions), never in `if glm` / `if deepseek` here.
//   - The caller's request is never mutated.

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/nick/gopi/internal/rawlogfmt"

	"github.com/nick/gopi/internal/scratch"
)

// minimalCoreDumpDir returns the directory for deterministic-death dumps.
// Overridable via GOPI_ERROR_DUMP_DIR; default <scratch home>/dumps/gopi-dumps
// (internal/scratch) — never /tmp, the 87%-full `/` partition behind the
// 2026-09-11 exhaustion (devdoc/tmp-ban-scratch-root.md).
func minimalCoreDumpDir() string {
	if d := os.Getenv("GOPI_ERROR_DUMP_DIR"); d != "" {
		return d
	}
	return scratch.DumpDir(minimalCoreDumpClass)
}

// minimalCoreDumpClass names this dump class inside the scratch dumps tree.
const minimalCoreDumpClass = "gopi-dumps"

// minimalCoreDumpMaxTail caps the request-body tail kept in the dump file
// (the full body can be megabytes after compact).
const minimalCoreDumpMaxTail = 512 * 1024

// minimalCoreLogTag prefixes every minimal-core log line.
const minimalCoreLogTag = "[GOPI][minimal-core]"

// MinimalCore returns a copy of req stripped to the minimal core:
// model + messages + max_tokens + stream. Everything optional/extension is
// removed — thinking, reasoning_effort, cache_control markers, tools, system
// blocks, temperature/top_p-style extras (none exist on ClaudeRequest today;
// the struct literal below is the whitelist, so any FUTURE optional field is
// stripped by construction — adding a field cannot leak into the core).
func MinimalCore(req *ClaudeRequest) *ClaudeRequest {
	return &ClaudeRequest{
		Model:     req.Model,
		MaxTokens: req.MaxTokens,
		Messages:  req.Messages,
		Stream:    req.Stream,
	}
}

// claudeSendRoundtrip is one full HTTP attempt against the Claude Messages
// endpoint. Returns status, body, error. An HTTP 4xx/5xx is DATA to the
// caller, not a transport error.
// runMinimalCoreDegrade sends req once; on a deterministic 4xx it re-sends
// the minimal core exactly once. Outcomes:
//   - first send OK              → req returned unchanged, nil error.
//   - 4xx then core accepted     → core variant returned, nil error.
//   - 4xx then core still 4xx    → fail fast; FULL request dumped to
//     <dump-dir>/<ts>.dump.json so the failed request is replayable and the
//     session resumable.
//   - transient (429/5xx/network) on either leg → returned verbatim for the
//     normal retry loop to handle (NOT the degradation path's business).
func (c *Client) runMinimalCoreDegrade(ctx context.Context, req *ClaudeRequest, send claudeSendRoundtrip) (*ClaudeRequest, error) {
	status, body, err := send(ctx, req)
	if err == nil && !isDeterministicHTTPError(status, err) {
		// First send fine (200 / transient) — nothing to degrade.
		return req, nil
	}
	if err != nil {
		return req, err
	}
	// ACCOUNT-LEVEL (402 billing failure, or a vendor account code inside a
	// 4xx): the credential is unusable, so stripping the request down to its
	// minimal core and re-sending is pointless — a minimal request cannot fix
	// an empty balance. Fail fast with the operator message (retryDo never
	// retries these either).
	if isAccountLevelStatus(status, body) {
		return req, accountLevelError(&sentinelAPIError{status: status, body: body, providerBase: c.BaseURL})
	}

	log.Printf("%s HTTP %d (deterministic 4xx) — one minimal-core degradation re-send", minimalCoreLogTag, status)

	core := MinimalCore(req)
	status, body, err = send(ctx, core)
	if err != nil {
		return req, err
	}
	if isDeterministicHTTPError(status, err) {
		// Still 4xx — deterministic failure end-to-end. Fail fast with a
		// structured dump of the original request (replayable, resumeable).
		dumpPath := dumpFailedRequest(req, status, body)
		detail := trimmedBody(body)
		if dumpPath != "" {
			return req, fmt.Errorf(
				"deepseek: request rejected after minimal-core degradation (HTTP %d); request dumps: %s; api error: %s",
				status, dumpPath, detail)
		}
		return req, fmt.Errorf(
			"deepseek: request rejected after minimal-core degradation (HTTP %d); dump write failed; api error: %s",
			status, detail)
	}
	// Core accepted (200 or a transient the retry loop may handle).
	log.Printf("%s minimal core accepted after 4xx", minimalCoreLogTag)
	return core, nil
}

// dumpFailedRequest writes the deterministic-death dump:
// {dump_dir}/<yyyymmdd-hhmmss>.dump.json containing the original request,
// the HTTP status, the API error body and a timestamp. The name comes from
// writeDumpFile (shared with dump1214Request), so two dumps inside the same
// second get distinct "-1"/"-2" names instead of the second one truncating
// the first. Returns the dump path, or "" when the write failed (never
// fatal — the fail-fast error is what matters).
func dumpFailedRequest(orig *ClaudeRequest, status int, apiErrBody []byte) string {
	type dumpDoc struct {
		Timestamp   string          `json:"timestamp"`
		HTTPStatus  int             `json:"http_status"`
		APIError    string          `json:"api_error_body"`
		OriginalReq json.RawMessage `json:"original_request"`
	}
	doc := dumpDoc{
		Timestamp:  time.Now().UTC().Format(time.RFC3339),
		HTTPStatus: status,
		APIError:   tailBytes(apiErrBody, minimalCoreDumpMaxTail),
	}
	if b, err := json.Marshal(orig); err == nil {
		doc.OriginalReq = json.RawMessage(b)
	}
	out, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		log.Printf("%s dump marshal failed: %v", minimalCoreLogTag, err)
		return ""
	}
	path := writeDumpFile(minimalCoreDumpDir(), out, minimalCoreLogTag)
	if path == "" {
		return ""
	}
	log.Printf("%s full request dump written: %s", minimalCoreLogTag, path)
	return path
}

// non200RawLog writes a provider-agnostic diagnostic record for a failed
// HTTP attempt to the raw log: HTTP status + full error response body.
// Best-effort — raw logging must never alter control flow.
func (c *Client) non200RawLog(stage string, status int, body []byte) {
	if c.RawLog == nil {
		return
	}
	var buf bytes.Buffer
	fmt.Fprintf(&buf, "← %s — HTTP %d (non-OK)\n", stage, status)
	buf.Write(body)
	buf.WriteByte('\n')
	c.RawLog(rawlogfmt.TagError, "", buf.Bytes())
}

// tailBytes returns at most max trailing bytes of b (head-trimmed).
// trimmedBody returns at most minCoreBodyCap bytes of body (head-kept),
// appending an ellipsis when cut — for inline error messages.
func trimmedBody(body []byte) string {
	if len(body) <= minCoreBodyCap {
		return string(body)
	}
	return string(body[:minCoreBodyCap]) + "..."
}
