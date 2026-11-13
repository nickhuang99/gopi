package gopilog

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/nick/gopi/internal/rawlogfmt"
)

// The round parser dispatches on a RECORD's identity, never on a line's text:
// rawlogfmt.LineScanner says whether the current line is its record's own header
// (IsRecordHeader) and what that record's direction tag is (Record().Tag). That is
// what makes ONE parser serve both framings — a framed header says `R 412` and a
// legacy marker says `→ REQUEST`, but both arrive as "the header of a TagRequest
// record", so nothing below branches on the format (Rules 13/14).
//
// The legacy vocabulary itself lives in internal/rawlogfmt (MarkerRequest /
// MarkerResponse / MarkerChunk / MarkerHTTPError) — the ONE definition. It is still
// needed here for two questions the tags cannot answer:
//
//   - a ROUND's marker text and annotation (roundMarkerText/roundAnnotation), which
//     are user-visible in `session raw --rounds` and must reproduce the file;
//   - is a given line a marker line, i.e. does it open a record or merely sit inside
//     a payload (isPayloadMarkerLine)?
var (
	markerRequestPrefix  = rawlogfmt.MarkerRequest
	markerResponsePrefix = rawlogfmt.MarkerResponse
	markerChunkPrefix    = rawlogfmt.MarkerChunk
	markerHTTPError      = rawlogfmt.MarkerHTTPError
)

// legacyMarkerForTag is the ONE tag→legacy-marker mapping the round parser needs.
//
// TagProbeRequest maps onto the REQUEST marker because a probe is a request: the
// reader promotes an annotated request to ProbeTag in BOTH framings (so the guard
// line survives the format change), and a mapping that omitted it would silently
// lose the annotation — and, worse, the round itself (measured: every annotated
// marker stopped opening a round, 7 tests red).
var legacyMarkerForTag = map[string]string{
	rawlogfmt.TagRequest:      rawlogfmt.MarkerRequest,
	rawlogfmt.TagProbeRequest: rawlogfmt.MarkerRequest,
	rawlogfmt.TagResponse:     rawlogfmt.MarkerResponse,
	rawlogfmt.TagChunk:        rawlogfmt.MarkerChunk,
}

// isRequestTag reports whether a record tag opens a round: a plain request, or the
// PROBE variant of one (an annotated request the guard/fail-fast ladder sent). They
// are one direction with two spellings, so every dispatch site asks this rather
// than comparing against TagRequest alone.
func isRequestTag(tag string) bool {
	return tag == rawlogfmt.TagRequest || tag == rawlogfmt.TagProbeRequest
}

// HTTPRound is one request/response pair as recorded in a raw HTTP log: the
// request marker line (with any annotation like "(1214-guard, stream)"), the
// request body, and everything that follows it up to the next request marker.
type HTTPRound struct {
	// Number is the ordinal of the request marker in the raw log (1-based).
	Number int
	// Marker is the request marker line verbatim ("→ REQUEST" or an
	// annotated variant).
	Marker string
	// Annotation is the parenthesised suffix of the marker, stripped
	// ("" for the plain "→ REQUEST" of a normal API send). Annotated rounds
	// are guard/fail-fast ladder probes, not the agent's accepted call.
	Annotation string
	// Request is the parsed request body (nil when the payload is missing or
	// not parseable JSON).
	Request *RequestPayload
	// RequestRaw is the exact request body bytes as logged.
	RequestRaw []byte
	// Stream is true when the request body asked for streaming (or the
	// marker annotation says so).
	Stream bool
	// Response is the digest of everything after the request up to the next
	// request marker (chunks / response body / HTTP-ERROR diagnostics).
	Response RoundResponse
	// LineStart / LineEnd are the 1-based raw-log line numbers of the round
	// (marker line through the last line before the next request). Useful for
	// pointing a user at the raw file when a view truncates.
	LineStart int
	LineEnd   int
	// RespLineStart is the 1-based raw-log line number of the round's first
	// response-side marker (the first ← CHUNK / ← RESPONSE / HTTP-ERROR after
	// the request body); 0 when the round logged no response markers. A
	// verbatim response view re-reads lines RespLineStart..LineEnd from the
	// raw log — the exact bytes as written (the digest keeps only summary
	// fields, not the raw SSE payloads, so a re-read is required).
	RespLineStart int
	// Err records a structural problem with the round (unparseable request
	// body, unterminated round) without failing the whole view.
	Err error
}

// RoundResponse digests one round's response side. Bytes carries the exact
// payload bytes as logged.
type RoundResponse struct {
	// Kind is "stream" (chunks seen), "response" (a ← RESPONSE body was
	// logged), "http-error" (HTTP-ERROR diagnostics), or "none".
	Kind string
	// Bytes is the total response payload size in bytes.
	Bytes int
	// Chunks is the number of ← CHUNK events.
	Chunks int
	// TextChars / ThinkingChars count the text/thinking characters
	// reassembled from chunk deltas (a cheap, log-side proxy for output
	// volume — NOT token counts).
	TextChars int
	// ThinkingChars counts reasoning characters (Anthropic thinking_delta /
	// Deepseek reasoning_content).
	ThinkingChars int
	// StopReason is the first stop_reason seen in the round's stream
	// (Anthropic message_delta / Deepseek finish_reason), "" when none.
	StopReason string
	// InputTokens / CacheRead / CacheWrite / OutputTokens are the USAGE
	// numbers reported by the stream (Anthropic message_start/message_delta,
	// Deepseek usage when present). 0 when the provider did not report them.
	InputTokens  int
	CacheRead    int
	CacheWrite   int
	OutputTokens int
	// ToolUses lists the tool names whose content_block_start appeared in
	// this round (deduplicated, in first-seen order).
	ToolUses []string
	// ErrorBody is the raw error response body of an HTTP-ERROR round.
	ErrorBody []byte
}

// roundMarkerText is the marker text a round records: the file's own header text —
// `R 412` for a framed record, `→ REQUEST` (or its annotated variant) for a legacy
// one. Both are what the user sees in `session raw --rounds`, and each must
// reproduce the FILE it came from, so the answer is the line itself; the
// format-independent facts a consumer must not lose (direction, annotation) are
// carried separately as the record's Tag and Annotation.
func roundMarkerText(line string) string { return strings.TrimSpace(line) }

// roundAnnotation is the record's annotation, independent of the framing: a framed
// header carries it as metadata after the length (`P 26 1214-guard, stream`), a
// legacy marker as a parenthesised suffix (`→ REQUEST (1214-guard, stream)`). The
// reader already decoded it (Record.Annotation) and its value is IDENTICAL in both
// framings — that identity is why the probe guard's visibility survives the format
// change (rawview renders it as "probe · 1214-guard, stream").
func roundAnnotation(rec rawlogfmt.Record, format rawlogfmt.Format, line string) string {
	if format == rawlogfmt.FormatFramed {
		return rec.Annotation
	}
	marker, ok := legacyMarkerForTag[rec.Tag]
	if !ok {
		return ""
	}
	return rawlogfmt.LegacyAnnotation(strings.TrimSpace(line), marker)
}

// THREE HELPERS WERE DELETED HERE, and the evidence for each is a reference count
// with no hit outside this file (Rule 43's判据, measured on this tree):
//
//   - isMarkerLine(line, marker)        — the legacy text test the round dispatch
//     used before it dispatched on a record's TAG. Zero callers after the switch:
//     the answer it gave (is this line a marker) is now the decoder's own
//     IsLegacyMarkerLine / CanDecodeHeader, asked in rawlogfmt where the vocabulary
//     lives.
//   - stripAnnotation(line, marker)     — the legacy-only annotation extraction, now
//     rawlogfmt.LegacyAnnotation (one implementation, next to the marker it parses).
//   - isPayloadMarkerLine(line, format) — an unused seam from the first draft of the
//     dual-format dispatch: the parser never needs "is this line a marker" once every
//     decision comes from sc.IsRecordHeader()/sc.Record().Tag.
//
// `grep -rn 'isMarkerLine\|stripAnnotation\|isPayloadMarkerLine' --include=*.go .`
// printed zero hits outside this file before they were removed, so nothing had to be
// moved out first (Rule 43's first step).

// openRawLogForParse opens a raw log together with the framing it is written in.
// The format is detected ONCE here and handed to the reader, so the head of the
// file is read once and no consumer below has to ask which framing it is.
//
// Detection is an ERROR when it cannot decide (rawlogfmt.DetectExact): a raw log
// this system cannot classify must be reported, never silently parsed as legacy —
// a wrong guess renders a framed log as 3381 "rounds" of garbage, and the reader
// would have no way to tell that from a real one (Rule 15).
func openRawLogForParse(path string) (io.ReadCloser, rawlogfmt.Format, error) {
	f, err := openRawLog(path)
	if err != nil {
		return nil, 0, err
	}
	format, err := rawlogfmt.Detect(path)
	if err != nil {
		// The detection failure is the one the caller must act on, so it stays
		// the head of the message — but a failed Close is a real resource
		// failure of its own, and dropping it is the silent-failure shape
		// (Rule 15). errors.Join keeps BOTH in the chain, so errors.Is can
		// still match either one.
		return nil, 0, fmt.Errorf("raw log %s: %w", path, errors.Join(err, f.Close()))
	}
	return f, format, nil
}

// ParseHTTPRounds scans a raw HTTP log and returns every round (request
// marker → next request marker). The whole log is scanned once; request
// bodies are kept raw for callers that need exact bytes. Round numbering
// counts ALL request markers in file order, so guard/fail-fast ladder probes
// (annotated markers) are visible as their own rounds — exactly the extra
// sends the JSONL hides.
//
// Payload framing per marker:
//   - → REQUEST / ← RESPONSE: the JSON body is the single following non-blank
//     line (json.Marshal output — never pretty-printed). A trailing blank
//     line (LogFunc appends "\n" after data) is tolerated.
//   - ← CHUNK: the SSE event is MULTI-LINE — "event: …" / "data: …" lines
//     terminated by a blank line (the raw logger preserves the SSE framing,
//     so data: payloads may themselves span several lines). Lines accumulate
//     until the blank separator, then the whole event is digested.
//   - HTTP-ERROR: diagnostic header + body lines until the next marker.
func ParseHTTPRounds(path string) ([]HTTPRound, error) {
	f, format, err := openRawLogForParse(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	sc := rawlogfmt.NewLineScannerFrom(f, format)
	var (
		rounds   []HTTPRound
		cur      *HTTPRound
		lineNo   int
		scErr    error
		pending  string // marker seen; payload accumulation state
		chunkBuf strings.Builder
		errBuf   []string
	)
	// flushHTTPError writes an accumulated HTTP-ERROR body into the current
	// round. The error body ends at the next marker line, so every marker
	// dispatch must flush before acting.
	flushHTTPError := func() {
		if cur == nil || len(errBuf) == 0 {
			return
		}
		cur.Response.Kind = "http-error"
		cur.Response.ErrorBody = []byte(strings.Join(errBuf, "\n"))
		cur.Response.Bytes += len(cur.Response.ErrorBody)
		errBuf = nil
	}
	// flushChunk digests an accumulated chunk buffer. Two wire shapes exist:
	// Anthropic-style multi-line SSE events (event:/data: lines terminated by
	// a blank line) and older single-line JSON chunks with NO blank separator
	// (the next raw-log line is the next marker). digestChunk is invoked on
	// the blank separator AND when a marker line arrives mid-chunk, so both
	// shapes are handled.
	flushChunk := func() {
		if chunkBuf.Len() > 0 {
			digestChunk(&cur.Response, chunkBuf.String())
			chunkBuf.Reset()
		}
	}
	// recordRespStart stamps the first response-side marker line of the open
	// round (a ← CHUNK / ← RESPONSE / HTTP-ERROR). It fires once per round at
	// the first response marker, pinning where the response verbatim begins
	// so a later view can re-read lines RespLineStart..LineEnd from the raw
	// file. A round with no response marker keeps RespLineStart=0.
	recordRespStart := func() {
		if cur != nil && cur.RespLineStart == 0 {
			cur.RespLineStart = lineNo
		}
	}
	for sc.Scan() {
		lineNo++
		line := strings.TrimRight(sc.Text(), "\r")
		trimmed := strings.TrimSpace(line)
		// A record's OWN header — the length-prefixed one in a framed log, the
		// direction marker in a legacy one. The direction comes from the record's
		// TAG, so the same switch below serves both framings without asking which
		// one it is reading.
		headerTag := ""
		if sc.IsRecordHeader() {
			headerTag = sc.Record().Tag
		}

		switch {
		case isRequestTag(headerTag):
			// A new round begins. Finalize the previous one, then open the new.
			flushHTTPError()
			flushChunk()
			if cur != nil {
				cur.LineEnd = lineNo - 1
				rounds = append(rounds, *cur)
			}
			cur = &HTTPRound{
				Number:     len(rounds) + 1,
				Marker:     roundMarkerText(line),
				Annotation: roundAnnotation(sc.Record(), format, line),
				LineStart:  lineNo,
				Response:   RoundResponse{},
			}
			// THE REQUEST BODY IS THE RECORD'S PAYLOAD, not the next line.
			//
			// The legacy logger wrote a request body as one line, so a LINE-oriented
			// reader got away with "the body is the following line". The framed
			// framing makes that assumption false in general: a payload is a BYTE
			// SPAN and may carry newlines (MEASURED: a 146-byte request payload whose
			// own bytes quote the marker vocabulary was cut down to its first 7 bytes
			// — `prefix` — so `session raw 1 request` returned a truncated body while
			// the record announced 146). The record's own payload is what the writer
			// logged, in either framing, so reading it here is both correct and
			// format-independent (Rules 13/14).
			if payload := sc.Record().Payload; len(payload) > 0 {
				cur.RequestRaw = append([]byte(nil), payload...)
				cur.Request = &RequestPayload{}
				if err := json.Unmarshal(cur.RequestRaw, cur.Request); err != nil {
					cur.Err = fmt.Errorf("round %d: unparseable request body: %w", cur.Number, err)
				} else {
					cur.Stream = detectStream(cur.RequestRaw, cur.Annotation)
				}
			}
			pending = ""
			chunkBuf.Reset()

		case headerTag == rawlogfmt.TagChunk:
			flushHTTPError()
			flushChunk()
			recordRespStart()
			pending = "chunk"
			chunkBuf.Reset()

		case headerTag == rawlogfmt.TagResponse && isDerivedStreamResponse(sc.Record().Annotation):
			// A SYNTHESIZED complete-response record (rawlogfmt.StreamResponseAnnotation)
			// closes a STREAMED round. It is derived from the chunks that precede it,
			// so it must not become the round's response: doing that would overwrite
			// Kind "stream" with "response" (losing the chunk count a reader uses to
			// see the round was streamed) and charge its bytes a SECOND time on top of
			// the chunks that already carried them. The round's response side stays
			// exactly what the wire produced; this record is a convenience for the
			// pairing/usage reader, and the round model's job is the wire.
			//
			// MEASURED: without this arm a one-request streamed round reported
			// kind="response" chunks=1 bytes=195 where the streamed log alone reports
			// kind="stream" chunks=1.
			flushHTTPError()
			flushChunk()
			recordRespStart()
			pending = "response"
			chunkBuf.Reset()

		case headerTag == rawlogfmt.TagResponse:
			flushHTTPError()
			flushChunk()
			recordRespStart()
			// THE RESPONSE BODY IS THE RECORD'S PAYLOAD, and it is counted HERE, ONCE.
			//
			// A payload is a BYTE SPAN: in a framed record it may carry several lines,
			// and in a legacy one it is the single body line with the framing's own
			// terminator trimmed off. `len(payload)+1` therefore states the same thing
			// for both — the body's bytes plus the one line terminator this counter has
			// always charged — and it is the ONLY arm allowed to charge them.
			//
			// The line accumulator below must not charge them a second time. MEASURED
			// (real 1.79 GB legacy session log, 2026-11-02): charging both made round
			// 102 report 9.7 KB where the baseline binary reports 6.4 KB, the excess
			// being exactly (len(payload)+1). And charging ONLY the line arm is wrong
			// the other way — it stops at the first line, so a three-line body reported
			// 9 bytes of its 29 (TestResponseBytesCountAMultiLineBodyOnce).
			if payload := sc.Record().Payload; len(payload) > 0 {
				cur.Response.Kind = "response"
				cur.Response.Bytes += len(payload) + 1
			}
			// The body's lines still arrive below; they are IGNORED there by design —
			// their bytes are already counted, and a blank line inside the body must not
			// end the record.
			pending = "response"
			chunkBuf.Reset()

		case headerTag == rawlogfmt.TagError:
			// The diagnostics record, in EITHER framing. The record's tag says so, so
			// no marker text is compared; the only format difference is where the
			// diagnostics' first bytes sit. In the LEGACY file the writer glued them
			// onto the marker line itself (internal/deepseek/minimal_core.go
			// non200RawLog passes TagError with a payload whose own first bytes follow
			// the marker word on the same line), so that remainder is recovered here;
			// in the FRAMED file those bytes are simply the payload's first line and
			// arrive through the accumulation arm below.
			flushHTTPError()
			flushChunk()
			recordRespStart()
			pending = "http-error"
			if format == rawlogfmt.FormatLegacy {
				if rest := rawlogfmt.HTTPErrorRemainder(line); rest != "" {
					errBuf = append(errBuf, rest)
				}
			}

		case pending == "chunk" && cur != nil:
			// Accumulate the SSE event lines until the blank separator (or the
			// next marker, dispatched above).
			if trimmed == "" {
				flushChunk()
				pending = ""
			} else {
				chunkBuf.WriteString(line)
				chunkBuf.WriteByte('\n')
			}

		case pending == "response" && cur != nil:
			// The body's bytes were counted ONCE by the record arm above (the payload
			// IS the body); this arm exists only to consume the lines it rendered as,
			// so a blank line inside a multi-line body is data rather than an
			// end-of-record. Charging len(line)+1 here as well reported every response
			// body at about double its size — see the record arm's measurement.
			if trimmed == "" {
				continue
			}
			pending = ""

		case pending == "http-error" && cur != nil:
			// Accumulate body lines until the next marker line. Blank lines
			// inside an error body are data, not terminators.
			errBuf = append(errBuf, line)
		}
	}
	if err := sc.Err(); err != nil {
		// A raw log is a different file format from a session JSONL, but the
		// same rule applies: never surface the reader's anonymous reason. The
		// shared JSONL reader's over-ceiling error already names the file, the
		// line, its byte offset and the ceiling (jsonlline.go).
		scErr = fmt.Errorf("raw log %s: %w", path, err)
	}
	if pending == "chunk" && cur != nil && chunkBuf.Len() > 0 {
		// EOF without the blank separator: digest the trailing partial event
		// best-effort (same policy as the SSE reader itself).
		digestChunk(&cur.Response, chunkBuf.String())
	}
	if pending == "http-error" && cur != nil && len(errBuf) > 0 {
		cur.Response.Kind = "http-error"
		cur.Response.ErrorBody = []byte(strings.Join(errBuf, "\n"))
	}
	if cur != nil {
		cur.LineEnd = lineNo
		rounds = append(rounds, *cur)
	}
	if scErr != nil {
		return rounds, scErr
	}
	return rounds, nil
}

// openRawLog opens a raw log for scanning.
func openRawLog(path string) (io.ReadCloser, error) {
	return os.Open(path)
}

// detectStream reports whether a request asks for streaming. The marker
// annotation ("stream") and the body's "stream" field both count. The raw
// body bytes are probed directly (RequestPayload does not model the "stream"
// field — parseRequestBody preserves p.Raw for the --request --json view, but
// ParseHTTPRounds unmarshals into the struct only).
func detectStream(rawBody []byte, annotation string) bool {
	if strings.Contains(annotation, "stream") {
		return true
	}
	if len(rawBody) == 0 {
		return false
	}
	var probe struct {
		Stream bool `json:"stream"`
	}
	if json.Unmarshal(rawBody, &probe) == nil {
		return probe.Stream
	}
	return false
}

// digestChunk folds one ← CHUNK payload (an SSE event) into the round's
// response digest. Two wire shapes are handled:
//   - Anthropic-style multi-line SSE ("event: …" / "data: {…}" lines) — the
//     JSON is the concatenation of the data: lines.
//   - Deepseek single-line chat.completion.chunk JSON ("{…}").
//
// The event's data payload is parsed far enough to extract usage numbers,
// stop reason, text/thinking character deltas and tool_use block starts.
func digestChunk(r *RoundResponse, line string) {
	r.Chunks++
	r.Bytes += len(line) + 1

	payload := sseDataPayload(line)
	if len(payload) == 0 {
		// Bare JSON chunk (Deepseek-style single-line SSE).
		payload = []byte(line)
	}

	// Anthropic-style event wrapper.
	var ev struct {
		Type string `json:"type"`
	}
	_ = json.Unmarshal(payload, &ev) //stylegate:ignore best-effort decode of an optional field; the zero value is the documented fallback

	// usage fields share names across both wire shapes (nested under
	// message_start.message.usage / message_delta.usage for Anthropic;
	// top-level "usage" for Deepseek).
	type usageBlock struct {
		InputTokens  int `json:"input_tokens"`
		OutputTokens int `json:"output_tokens"`
		CacheRead    int `json:"cache_read_input_tokens"`
		CacheWrite   int `json:"cache_creation_input_tokens"`
	}
	// Deepseek usage is top-level in the final chunk.
	var ds struct {
		Usage usageBlock `json:"usage"`
	}
	_ = json.Unmarshal(payload, &ds) //stylegate:ignore best-effort decode of an optional field; the zero value is the documented fallback
	if ds.Usage.InputTokens > 0 && r.InputTokens == 0 {
		r.InputTokens = ds.Usage.InputTokens
	}
	if ds.Usage.CacheRead > 0 && r.CacheRead == 0 {
		r.CacheRead = ds.Usage.CacheRead
	}
	if ds.Usage.CacheWrite > 0 && r.CacheWrite == 0 {
		r.CacheWrite = ds.Usage.CacheWrite
	}
	if ds.Usage.OutputTokens > r.OutputTokens {
		r.OutputTokens = ds.Usage.OutputTokens
	}

	switch ev.Type {
	case "message_start":
		// usage lives in message.message.usage.
		var m struct {
			Message struct {
				Usage struct {
					InputTokens int `json:"input_tokens"`
					CacheRead   int `json:"cache_read_input_tokens"`
					CacheWrite  int `json:"cache_creation_input_tokens"`
				} `json:"usage"`
			} `json:"message"`
		}
		if json.Unmarshal(payload, &m) == nil {
			if m.Message.Usage.InputTokens > 0 {
				r.InputTokens = m.Message.Usage.InputTokens
			}
			if m.Message.Usage.CacheRead > 0 {
				r.CacheRead = m.Message.Usage.CacheRead
			}
			if m.Message.Usage.CacheWrite > 0 {
				r.CacheWrite = m.Message.Usage.CacheWrite
			}
		}

	case "content_block_start":
		// Tool-use block announces a tool call.
		var cb struct {
			ContentBlock struct {
				Type string `json:"type"`
				Name string `json:"name"`
			} `json:"content_block"`
		}
		if json.Unmarshal(payload, &cb) == nil && cb.ContentBlock.Type == "tool_use" && cb.ContentBlock.Name != "" {
			r.ToolUses = append(r.ToolUses, cb.ContentBlock.Name)
		}

	case "content_block_delta":
		var d struct {
			Delta struct {
				Type        string `json:"type"`
				Text        string `json:"text"`
				Thinking    string `json:"thinking"`
				PartialJSON string `json:"partial_json"`
			} `json:"delta"`
		}
		if json.Unmarshal(payload, &d) == nil {
			switch d.Delta.Type {
			case "text_delta":
				r.TextChars += len(d.Delta.Text)
			case "thinking_delta":
				r.ThinkingChars += len(d.Delta.Thinking)
			case "input_json_delta":
				r.TextChars += len(d.Delta.PartialJSON)
			}
		}

	case "message_delta":
		var d struct {
			Delta struct {
				StopReason string `json:"stop_reason"`
			} `json:"delta"`
			Usage struct {
				OutputTokens int `json:"output_tokens"`
				InputTokens  int `json:"input_tokens"`
				CacheRead    int `json:"cache_read_input_tokens"`
			} `json:"usage"`
		}
		if json.Unmarshal(payload, &d) == nil {
			if d.Delta.StopReason != "" && r.StopReason == "" {
				r.StopReason = d.Delta.StopReason
			}
			if d.Usage.OutputTokens > r.OutputTokens {
				r.OutputTokens = d.Usage.OutputTokens
			}
			if d.Usage.InputTokens > 0 && r.InputTokens == 0 {
				r.InputTokens = d.Usage.InputTokens
			}
			if d.Usage.CacheRead > 0 && r.CacheRead == 0 {
				r.CacheRead = d.Usage.CacheRead
			}
		}
	}

	// Anthropic style may set Stream kind via chunk presence.
	if r.Kind == "" || r.Kind == "none" {
		r.Kind = "stream"
	}
	if r.StopReason == "" {
		// Deepseek finish_reason lives in choices[0].finish_reason.
		var c struct {
			Choices []struct {
				FinishReason string `json:"finish_reason"`
				Delta        struct {
					Content          string `json:"content"`
					ReasoningContent string `json:"reasoning_content"`
				} `json:"delta"`
			} `json:"choices"`
		}
		if json.Unmarshal(payload, &c) == nil {
			for _, ch := range c.Choices {
				if ch.FinishReason != "" && r.StopReason == "" {
					r.StopReason = ch.FinishReason
				}
				r.TextChars += len(ch.Delta.Content)
				r.ThinkingChars += len(ch.Delta.ReasoningContent)
			}
		}
	}
}

// sseDataPayload extracts the concatenated "data: " lines from a multi-line
// SSE event; "" when the payload has no data: line (e.g. a comment-only
// event or a bare JSON chunk).
func sseDataPayload(event string) []byte {
	var sb strings.Builder
	for _, l := range strings.Split(event, "\n") {
		if rest, ok := strings.CutPrefix(l, "data:"); ok {
			sb.WriteString(strings.TrimSpace(rest))
			sb.WriteByte('\n')
		}
	}
	if sb.Len() == 0 {
		return nil
	}
	return []byte(strings.TrimRight(sb.String(), "\n"))
}

// UniqueToolUses dedupes a round's ToolUses in first-seen order.
func (r *RoundResponse) UniqueToolUses() []string {
	seen := map[string]bool{}
	var out []string
	for _, n := range r.ToolUses {
		if !seen[n] {
			seen[n] = true
			out = append(out, n)
		}
	}
	return out
}

// isDerivedStreamResponse reports whether a TagResponse record was SYNTHESIZED
// from a streamed call's events rather than captured from a response body
// (rawlogfmt.StreamResponseAnnotation, plus the ", partial" suffix a truncated
// stream carries). Such a record is still a response record — one reader pairs
// it with its request — but it is not the round's WIRE response, and the two
// must not be conflated here: see the dispatch arm that calls this.
func isDerivedStreamResponse(annotation string) bool {
	return strings.HasPrefix(annotation, rawlogfmt.StreamResponseAnnotation)
}
