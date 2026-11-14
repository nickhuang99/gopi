// The complete-response record for a STREAMED Claude call.
//
// WHY THIS FILE EXISTS (bug 01a19bf1-ff52): a streamed round used to write only
// its request (R) and its raw SSE events (C). Nothing in the raw log was a
// complete response, so a reader could not pair "one request, one response" the
// way the non-streaming path allows — usage was recoverable only by scanning the
// chunk sequence for the message_start event, and `cmd/agent/tokens_count.go`'s
// usageInLine had to accept a second shape for exactly that reason.
//
// The fix is on the WRITE side, and it is the shape the non-streaming path
// already writes: when the stream ends, one TagResponse (S) record carrying the
// SAME body shape (`{"id","content","stop_reason","usage"}` — claudeWireResponse,
// the struct the non-streaming parse produces). Nothing about the chunk records
// changes: they remain the byte-exact wire evidence, and this record is derived
// from them, so a reader that wants "the bytes the server sent" still reads the
// C records and a reader that wants "what the call returned" reads the S record.
//
// MEASURED SIZE COST (Rule: measure before choosing the shape). On a real
// session log (592,016 bytes, 3 requests, 99 chunks, /tmp/amb-probe's
// 914315f7 session) the three synthesized bodies cost 427 + 373 + 428 = 1,228
// bytes plus framing — 1,252 bytes, 0.21% of the log. The alternative ("record
// the raw body too") was never available: a streamed call has no body to record,
// which is the whole defect. So the cost is bounded by the ANSWER size, not by
// the chunk size: the chunks are ~10x the synthesized body because each one
// repeats the SSE envelope (`event:` + `data:` + JSON type/index scaffolding).
//
// ORDERING. The record is written when the stream ENDS, so it always follows the
// C records of its own request and precedes the next request's R record. A
// reader walking the log therefore sees R, C…C, S, R, C…C, S … — the same
// alternation a non-streaming log has, with the chunk records between.
package deepseek

import (
	"encoding/json"

	"github.com/nickhuang99/gopi/internal/rawlogfmt"
)

// claudeStreamResponseAnnotation marks a TagResponse record as DERIVED from a
// stream rather than captured from a response body. It is the one thing the
// record cannot say about itself: a nested-stream body and a non-stream body
// have the same shape (`claudeWireResponse`), and only the writer knows which
// one it synthesized. The spelling lives in rawlogfmt beside the other
// annotation vocabulary (Rules 13/14) so a reader can compare against a
// constant instead of a literal.
const claudeStreamResponseAnnotation = rawlogfmt.StreamResponseAnnotation

// claudeStreamPartialSuffix is appended to the annotation when the stream did
// NOT end normally (a transport error, a cancelled context). The record is
// still written — the usage from message_start is real and is exactly what a
// cost audit needs — but a reader must be able to tell a complete response from
// what a broken stream managed to produce.
const claudeStreamPartialSuffix = rawlogfmt.StreamResponsePartialSuffix

// claudeStreamResponse accumulates a streaming call into the body shape the
// non-streaming path writes.
//
// FIELDS ARE FOLDED, NOT REPLAYED: content blocks are keyed by their wire index
// (the index a delta announces), because a `content_block_delta` carries only an
// index and the block it belongs to is whatever `content_block_start` created.
// The order slice preserves wire order for the final array, so the body's
// `content` reads top-to-bottom like the server's.
type claudeStreamResponse struct {
	id         string
	stopReason string
	usage      ClaudeUsage
	// usageSeen records that the stream carried at least one usage block. A body
	// whose usage is all zeros and was NEVER announced is not "zero usage" — it is
	// a stream that said nothing about it, and the two must not be conflated
	// (Rule 15a: the zero value is not a measurement).
	usageSeen bool
	// sawMessage records that the stream produced at least one message_start —
	// the only event that makes the result a response at all. A stream that ended
	// before it (an immediate EOF, a refused connection) writes no S record:
	// inventing one would claim a response that never arrived.
	sawMessage bool
	// sawStop records that the server SIGNED OFF (a message_stop event). It is
	// the only honest witness that the body is whole: a connection that dies
	// after message_delta ends in the SAME clean io.EOF as a finished stream, so
	// "the reader returned no error" cannot tell them apart (Rule 15a — the zero
	// value of an absent event is not a measurement).
	sawStop bool
	order   []int
	blocks  map[int]*ClaudeContentBlock
}

// newClaudeStreamResponse starts an empty accumulator.
func newClaudeStreamResponse() *claudeStreamResponse {
	return &claudeStreamResponse{blocks: make(map[int]*ClaudeContentBlock)}
}

// observe folds ONE parsed wire event into the accumulated response. It is
// called for every event the stream carries, including `ping` (which contributes
// nothing and is ignored by the switch below).
//
// The usage rule mirrors what the caller already sees on the event channel:
// message_start carries the PROMPT side (with output_tokens still 0) and
// message_delta carries the FINAL counters, so the later value wins — otherwise
// every streamed record would report 0 output tokens.
func (s *claudeStreamResponse) observe(wire *claudeWireEvent) {
	switch wire.Type {
	case "message_start":
		s.sawMessage = true
		if wire.Message != nil {
			s.id = wire.Message.ID
			s.usage = wire.Message.Usage
			s.usageSeen = true
		}
	case "content_block_start":
		block := wire.ContentBlock
		s.blockAt(wire.Index)
		s.blocks[wire.Index] = &block
	case "content_block_delta":
		block := s.blockAt(wire.Index)
		d := wire.Delta
		switch d.Type {
		case "text_delta":
			block.Text += d.Text
		case "thinking_delta":
			block.Thinking += d.Thinking
		case "signature_delta":
			block.Signature = d.Signature
		case "input_json_delta":
			block.InputJSON += d.PartialJSON
		}
	case "message_stop":
		s.sawStop = true
	case "message_delta":
		if d := wire.Delta.StopReason; d != "" {
			s.stopReason = d
		}
		if wire.Usage != nil {
			s.usage = *wire.Usage
			s.usageSeen = true
		}
	}
}

// blockAt returns the block under construction at index i, creating it when the
// server sent a delta without a preceding content_block_start (tolerated for the
// same reason the parser tolerates it: the record should describe what arrived,
// not refuse to describe it).
func (s *claudeStreamResponse) blockAt(i int) *ClaudeContentBlock {
	if b, ok := s.blocks[i]; ok {
		return b
	}
	b := &ClaudeContentBlock{}
	s.blocks[i] = b
	s.order = append(s.order, i)
	return b
}

// body renders the accumulated response as the JSON a non-streaming call would
// have recorded — `claudeWireResponse`, field for field, so one reader handles
// both paths.
//
// ok=false means the stream never announced a message, so there is no response
// to record (see sawMessage).
//
// TOOL-USE INPUTS ARE PARSED, not left as fragments: the wire delivers a tool
// call as `input_json_delta` fragments and a non-streaming body carries the
// assembled `input` OBJECT. ClaudeContentBlock marshal-drops InputJSON
// (`json:"-"`), so folding without parsing would record every tool call with
// `"input":null` — a body that proves a tool was called and cannot say with what.
// A fragment run that does not parse (a truncated stream) keeps Input empty
// rather than failing the whole record: the rest of the body is still true.
func (s *claudeStreamResponse) body() ([]byte, bool) {
	if !s.sawMessage {
		return nil, false
	}
	content := make([]ClaudeContentBlock, 0, len(s.order))
	for _, i := range s.order {
		b := s.blocks[i]
		if b.InputJSON != "" {
			var input map[string]interface{}
			if err := json.Unmarshal([]byte(b.InputJSON), &input); err == nil {
				b.Input = input
			}
			b.InputJSON = ""
		}
		content = append(content, *b)
	}
	wire := claudeWireResponse{
		ID:         s.id,
		Content:    content,
		StopReason: s.stopReason,
		Usage:      s.usage,
	}
	out, err := json.Marshal(wire)
	if err != nil {
		// A marshal failure here is a shape bug, not a data condition: the value
		// is composed of strings, ints and a map decoded from JSON. Reported as
		// "no body" so the caller writes no record — a record whose bytes do not
		// say what the call returned is worse than no record (Rule 15).
		return nil, false
	}
	return out, true
}

// annotation names what this record is, and how complete it is.
func (s *claudeStreamResponse) annotation(complete bool) string {
	if complete {
		return claudeStreamResponseAnnotation
	}
	return claudeStreamResponseAnnotation + claudeStreamPartialSuffix
}

// logStreamResponse writes the complete-response record for a finished stream —
// the ONE writer, called from parseClaudeSSE when the stream ends (however it
// ends).
//
// IT WRITES NOTHING when the stream never announced a message, and nothing when
// raw logging is off. Both are silent by design: this is a derived record, and
// the chunk records that ARE the wire evidence have already been written by the
// per-event path. A best-effort audit channel does not fail its caller over an
// extra record (the same policy LogFunc's unknown-tag drop states).
func (c *Client) logStreamResponse(s *claudeStreamResponse, complete bool) {
	if c.RawLog == nil {
		return
	}
	body, ok := s.body()
	if !ok {
		return
	}
	c.RawLog(rawlogfmt.TagResponse, s.annotation(complete), body)
}
