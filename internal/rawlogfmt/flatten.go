// flatten.go — the ONE place that decides what TEXT a record occupies, so every
// line-oriented reader (the round parser, the verbatim line-span view, the
// response digest) sees the same rendering of the same file.
//
// # Why this layer exists at all
//
// The framed framing is length-prefixed: a record's payload is a BYTE SPAN, not a
// set of lines. Consumers that were written against the legacy framing are
// line-oriented — they ask "which line does this round start on", they re-read
// "lines 17..40 verbatim", they digest "the SSE event lines under this chunk".
// Rather than teach each of them a second decoding strategy (and let them drift
// apart on the questions that matter: where a round starts, how big it is, what
// its response side says), they all consume ONE flattened view produced here.
//
// # The rule, and why it reproduces the legacy file EXACTLY
//
// A record's text is the concatenation of what the file physically holds:
//
//	header text   (HeaderText, plus its terminator when HeaderTerminated)
//	payload       (Record.Payload, verbatim)
//	framing "\n"  (the encoder's/logger's own trailing newline, when Terminated)
//
// and the LINES are that text split on newlines, with the one trailing framing
// newline treated as a separator rather than as the start of a final empty line —
// which is exactly what a `bufio.Scanner` over the legacy file did (it emits no
// empty line for a file that ends with a newline).
//
// That is not a coincidence to be maintained by hand: it is the property the
// acceptance criterion "the same payload renders the same lines in both framings"
// rests on, and flatten_test.go pins it by rendering the same record both ways.
package rawlogfmt

import (
	"bytes"
	"strings"
)

// HeaderText renders a record's header/marker line WITHOUT its terminator:
//
//	framed  →  `R 412`, `R 412 1214-guard, stream`
//	legacy  →  `→ REQUEST`, `→ REQUEST (1214-guard, stream)`, `HTTP-ERROR`
//
// It is built from the RECORD's own fields, never re-derived from the payload
// length alone: a header whose stated length disagreed with its payload is shown
// as written, because hiding that disagreement would hide the defect a reader
// needs to see.
//
// For a legacy HTTP-ERROR record it returns just the marker word: the writer glued
// the diagnostics' first bytes onto that same line with no separator, and those
// bytes are PAYLOAD (they are in Record.Payload), so re-emitting them in the header
// would duplicate them. FlattenRecord's concatenation puts them back where the file
// has them.
func HeaderText(rec Record, format Format) string {
	if rec.HeaderLen <= 0 {
		return ""
	}
	if format == FormatFramed {
		// HeaderLineAnnotated renders the line WITH its terminator; this function's
		// contract is the text without it (the caller re-adds it from
		// HeaderTerminated, which is the only source that knows whether the file
		// actually has one).
		return strings.TrimSuffix(HeaderLineAnnotated(rec.Tag, len(rec.Payload), rec.Annotation), string(RecordTerminator))
	}
	marker, ok := LegacyMarker(rec.Tag, rec.Annotation)
	if !ok {
		return ""
	}
	// LegacyMarker carries the terminator; the caller re-adds it only when the
	// file's own line had one (see HeaderTerminated).
	return strings.TrimSuffix(marker, string(RecordTerminator))
}

// SplitFramingNewline returns the lines a payload occupies once the framing's own
// trailing newline is accounted for, plus whether a standalone trailer line
// follows them.
//
// A payload ending in a newline therefore renders as its lines plus one empty
// trailer line (the framing newline, seen as a separator). A payload NOT ending in
// a newline renders as its lines with no trailer.
func SplitFramingNewline(payload []byte) (lines []string, trailer bool) {
	if len(payload) == 0 {
		return nil, true // the framing newline alone is the record's only line
	}
	if !bytes.HasSuffix(payload, []byte{RecordTerminator}) {
		return splitLines(payload), false
	}
	body := payload[:len(payload)-1]
	if len(body) == 0 {
		return []string{""}, true
	}
	return splitLines(body), true
}

// FramingTrailerLine is the line that carries a record's framing newline when the
// payload itself ended in one ("" — the empty line). It is named rather than
// spelled so the trailer's identity is one fact.
func FramingTrailerLine() string { return "" }

// splitLines splits b on the record terminator. b is already newline-trimmed by
// the caller, so no trailing empty element is invented.
func splitLines(b []byte) []string {
	parts := bytes.Split(b, []byte{RecordTerminator})
	out := make([]string, len(parts))
	for i, p := range parts {
		out[i] = string(p)
	}
	return out
}

// FlattenRecord renders a decoded record as the lines a line-oriented reader sees
// — the record's header/marker line (in the file's own framing), its payload's
// lines, and the framing's closing line.
//
// The rendering is the record's PHYSICAL bytes split into lines, so a reader that
// counts lines, or that re-reads a line span verbatim, gets the same numbering it
// always had on a legacy log — and, on a framed log, the numbering its own lines
// imply. That property is what lets one walk serve both framings.
func FlattenRecord(rec Record, format Format) []string {
	if rec.HeaderLen <= 0 {
		// A record whose header the decoder could not locate (only reachable for a
		// hand-built Record): render its payload alone rather than invent a header.
		return splitText(string(rec.Payload))
	}
	// ONE RULE, BOTH FRAMINGS: rebuild the record's PHYSICAL text and split it the
	// way a line reader splits a file — exactly one trailing terminator is a
	// SEPARATOR, never the start of a final empty line.
	//
	//   header text
	//   its terminator, when the file's own header line has one
	//   the payload, verbatim
	//   the terminator that CLOSED this record in the file, when it had one
	//
	// This replaced a shape that asked `rec.Terminated` to mean "framed" — it does
	// not: nextLegacy never set it, so every legacy record took the truncated-tail
	// branch, which (a) never restored the terminator the decoder trims off a
	// payload and (b) split a text still ending in a terminator, inventing a
	// trailing empty line. MEASURED on a real 1.79 GB legacy session log: the last
	// round's END was reported as 7 where the file's own last line is 6, and a
	// round whose response body is 3356 bytes was counted across a phantom line.
	// The line-equivalence unit test was green throughout, because its fixture's
	// records all ended the same way — a fixture that never varies the shape it is
	// supposed to pin (see the empty-payload arm added with this fix).
	var sb strings.Builder
	sb.WriteString(HeaderText(rec, format))
	if rec.HeaderTerminated {
		sb.WriteByte(RecordTerminator)
	}
	sb.Write(rec.Payload)
	if rec.Terminated {
		sb.WriteByte(RecordTerminator)
	}
	return splitText(sb.String())
}

// splitText renders text as the lines a line reader sees: ONE trailing terminator
// is consumed as the last line's separator, then the rest is split on terminators.
// That is bufio.Scanner's rule over the same bytes, and it is unconditional — a
// terminator in a file always ENDS a line, so it never opens a final empty one.
// A text that does not end in a terminator loses nothing to it.
func splitText(text string) []string {
	if n := len(text); n > 0 && text[n-1] == RecordTerminator {
		text = text[:n-1]
	}
	return splitLines([]byte(text))
}
