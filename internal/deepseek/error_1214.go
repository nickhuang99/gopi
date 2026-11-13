package deepseek

// AD-HOC GUARD (1214) — temporary degradation ladder for HTTP 400 responses
// whose error body contains "1214" (DeepSeek invalid-request-shape error).
//
// Root cause is NOT yet identified — prime suspect: messages shape after
// compact/context folding (authorized incidents: nick 2026-12-14, two long
// 1.5h+ sessions, first turn after a mid-session legal streaming round).
// Static config cannot police runtime message-shape changes, hence this
// runtime ladder. REMOVE THIS FILE once the root cause is fixed properly.
// (mark ad-hoc: root cause = compact 后 messages 形态，查清后移除)

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/nick/gopi/internal/scratch"
)

// Named constants — Rule 1 (no magic numbers).
const (
	// error1214Marker is the substring identifying this error class in the
	// API error body. DeepSeek returns HTTP 400 with body mentioning 1214
	// when the request shape violates a model-side constraint.
	error1214Marker = "1214"

	// errorDumpMaxTail caps the request-body tail written into the dump
	// file (the full body can be megabytes after compact).
	errorDumpMaxTail = 512 * 1024 // bytes

	// guardDegradedMaxTextLen caps per-block text/thinking length in the
	// final (third) ladder rung — far below any context-window limit but
	// above any legitimate single block.
	guardDegradedMaxTextLen = 100_000 // runes

	// guardLogTag prefixes every guard log line for grep-ability.
	guardLogTag = "[GOPI][1214-guard]"
)

// guardDumpDir returns the directory for 1214 death dumps. Overridable via
// GOPI_1214_DUMP_DIR; default <scratch home>/dumps/gopi-1214-dumps
// (internal/scratch) — never /tmp, the 87%-full `/` partition behind the
// 2026-09-11 exhaustion (devdoc/tmp-ban-scratch-root.md).
func guardDumpDir() string {
	if d := os.Getenv("GOPI_1214_DUMP_DIR"); d != "" {
		return d
	}
	return scratch.DumpDir(guardDumpClass)
}

// guardDumpClass names this dump class inside the scratch dumps tree.
const guardDumpClass = "gopi-1214-dumps"

// is1214Error reports whether an HTTP status + response body pair is the
// 1214 error class the guard handles.
func is1214Error(status int, body []byte) bool {
	return status == http.StatusBadRequest && bytes.Contains(body, []byte(error1214Marker))
}

// claudeSendRoundtrip is one full HTTP attempt against the Claude Messages
// endpoint. Returns status, body, error. A non-1214 failure is returned
// verbatim (status/body may carry it); the ladder only reacts to 1214s.
type claudeSendRoundtrip func(ctx context.Context, req *ClaudeRequest) (int, []byte, error)

// run1214Ladder sends req once; on a 1214-shaped 400 it walks the
// degradation ladder and returns the request variant the API accepted:
//
//	rung 1: strip optional request parameters (thinking / reasoning_effort /
//	        cache_control / tools) — keep model+messages+max_tokens+stream.
//	rung 2: messages-body degradation — system blocks merged into one text
//	        block, tool_result null content normalized, overlong texts
//	        truncated.
//	still failing: fail-fast with the FULL final request body dumped to
//	        <dump-dir>/<ts>.dump.json (bug037 simplified mechanism) so the
//	        failed request is replayable and the session resumable.
//
// The caller's request is never mutated. send must marshal the request
// fresh each call (idempotent re-send).
//
// NOTE on effort_values (config-ization): rung 1 stripping reasoning_effort
// is deliberately INDEPENDENT of the settings.json "effort_values" list.
// The list declares the provider's legal effort range as data (no vendor
// mapping in code); if a listed value is still rejected at runtime with a
// 1214-shaped 400, THIS ladder is the runtime degradation — the error path
// IS the mapping's implementation. See internal/config ResolveReasoningEffort.
func (c *Client) run1214Ladder(ctx context.Context, req *ClaudeRequest, send claudeSendRoundtrip) (*ClaudeRequest, error) {
	status, body, err := send(ctx, req)
	if err != nil || !is1214Error(status, body) {
		return req, err
	}

	log.Printf("%s HTTP 400 + 1214 detected — starting degradation ladder (ad-hoc guard, root cause pending)", guardLogTag)

	// Rung 1 — strip optional parameters, keep the minimal core.
	r1 := stripOptionalFor1214(req)
	status, body, err = send(ctx, r1)
	if err != nil {
		return req, err
	}
	if !is1214Error(status, body) {
		log.Printf("%s rung 1 (strip optional params) — request accepted", guardLogTag)
		return r1, nil
	}
	log.Printf("%s rung 1 (strip optional params) — still 1214", guardLogTag)

	// Rung 2 — degrade the messages body itself.
	r2 := degradeMessagesFor1214(r1)
	status, body, err = send(ctx, r2)
	if err != nil {
		return req, err
	}
	if !is1214Error(status, body) {
		log.Printf("%s rung 2 (messages degradation) — request accepted", guardLogTag)
		return r2, nil
	}
	log.Printf("%s rung 2 (messages degradation) — still 1214 — failing fast", guardLogTag)

	// Fail-fast with a structured dump of the LAST request body (bug037
	// simplified: raw-log tail exists but is not structured/replayable).
	dumpPath := dump1214Request(req, r1, r2, body)
	detail := string(body)
	if len(detail) > maxErrorDisplayBytes {
		detail = detail[:maxErrorDisplayBytes] + "..."
	}
	if dumpPath != "" {
		return req, fmt.Errorf(
			"deepseek: 1214 persisted after degradation ladder (strip params + messages degrade); full request dumps: %s; api error: %s",
			dumpPath, detail)
	}
	return req, fmt.Errorf(
		"deepseek: 1214 persisted after degradation ladder (strip params + messages degrade); dump write failed; api error: %s", detail)
}

// stripOptionalFor1214 returns a shallow copy of req with all optional
// parameters removed: thinking, reasoning_effort, cache_control markers and
// tools. The minimal core (model + messages + max_tokens + stream) is kept.
// The original request is NOT mutated.
func stripOptionalFor1214(req *ClaudeRequest) *ClaudeRequest {
	r := *req // shallow copy
	r.Thinking = nil
	r.ReasoningEffort = ""
	r.Tools = nil
	// Strip cache_control markers from system blocks and tools (they are
	// only valid together with tools on Anthropic, and are optional).
	if len(r.System) > 0 {
		sys := make([]ClaudeSystemBlock, len(r.System))
		copy(sys, r.System)
		for i := range sys {
			sys[i].CacheControl = nil
		}
		r.System = sys
	}
	return &r
}

// degradeMessagesFor1214 applies messages-body normalization on top of a
// parameter-stripped request:
//   - system blocks merged into a single text block (cache markers gone);
//   - tool_result blocks with nil/empty content normalized to a text block
//     with "input" placeholder (null-content shape is the prime suspect);
//   - overlong text/thinking blocks truncated to guardDegradedMaxTextLen.
//
// The input request is not mutated.
func degradeMessagesFor1214(req *ClaudeRequest) *ClaudeRequest {
	r := *req
	r.Messages = make([]ClaudeMessage, len(req.Messages))

	if len(r.System) > 0 {
		var sb strings.Builder
		for _, b := range r.System {
			sb.WriteString(b.Text)
			sb.WriteString("\n\n")
		}
		r.System = []ClaudeSystemBlock{{Type: "text", Text: strings.TrimRight(sb.String(), "\n")}}
	}

	for i, m := range req.Messages {
		blocks := make([]ClaudeContentBlock, len(m.Content))
		copy(blocks, m.Content)
		for j, b := range blocks {
			switch b.Type {
			case "tool_result":
				// Normalize null/empty tool_result content into a text
				// block — null content is a shape suspect after compact.
				if b.Content == nil || b.Content == "" {
					b.Content = []ClaudeContentBlock{{Type: "text", Text: "(empty tool result)"}}
				}
			case "text":
				b.Text = truncateRunes(b.Text, guardDegradedMaxTextLen)
			case "thinking":
				b.Thinking = truncateRunes(b.Thinking, guardDegradedMaxTextLen)
			}
			blocks[j] = b
		}
		r.Messages[i] = ClaudeMessage{Role: m.Role, Content: blocks}
	}
	return &r
}

// truncateRunes caps s to max runes, appending an ellipsis marker when cut.
func truncateRunes(s string, max int) string {
	rs := []rune(s)
	if len(rs) <= max {
		return s
	}
	return string(rs[:max]) + "…[truncated by 1214-guard]"
}

// dump1214Request writes the structured 1214 death dump:
// {dump_dir}/<yyyymmdd-hhmmss>.dump.json containing the original request,
// the two degraded variants, the final API error body and a timestamp.
// The name comes from writeDumpFile (shared with dumpFailedRequest), so two
// dumps inside the same second get distinct "-1"/"-2" names instead of the
// second one truncating the first.
// Returns the dump path, or "" when the write failed (never fatal — the
// fail-fast error is what matters).
func dump1214Request(orig, r1, r2 *ClaudeRequest, apiErrBody []byte) string {
	type dumpDoc struct {
		Timestamp   string          `json:"timestamp"`
		Marker      string          `json:"marker"`
		APIError    string          `json:"api_error_body"`
		OriginalReq json.RawMessage `json:"original_request"`
		StrippedReq json.RawMessage `json:"stripped_request,omitempty"`
		DegradedReq json.RawMessage `json:"degraded_request,omitempty"`
	}
	doc := dumpDoc{
		Timestamp: time.Now().UTC().Format(time.RFC3339),
		Marker:    error1214Marker,
		APIError:  tailBytes(apiErrBody, errorDumpMaxTail),
	}
	if b, err := json.Marshal(orig); err == nil {
		doc.OriginalReq = json.RawMessage(b)
	}
	if b, err := json.Marshal(r1); err == nil {
		doc.StrippedReq = json.RawMessage(b)
	}
	if b, err := json.Marshal(r2); err == nil {
		doc.DegradedReq = json.RawMessage(b)
	}
	out, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		log.Printf("%s dump marshal failed: %v", guardLogTag, err)
		return ""
	}
	path := writeDumpFile(guardDumpDir(), out, guardLogTag)
	if path == "" {
		return ""
	}
	log.Printf("%s full request dump written: %s", guardLogTag, path)
	return path
}

// tailBytes returns at most max trailing bytes of b (head-trimmed).
func tailBytes(b []byte, max int) string {
	if len(b) <= max {
		return string(b)
	}
	return "…(head trimmed)…" + string(b[len(b)-max:])
}
