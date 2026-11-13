// Package rawlogfmt is the ONE definition of the raw HTTP log's on-disk record
// framing — the encoder, the decoder, and the deterministic discriminator
// between the two framings this system has ever written.
//
// # The two framings
//
// LEGACY (every log written before this package existed, and every log a
// continued run keeps writing in — see "a file's framing is fixed at creation"):
//
//	→ REQUEST\n<body>\n
//	← CHUNK\n<event>\n
//	← RESPONSE\n<body>\n
//	HTTP-ERROR\n<diagnostics>\n
//
// The direction is a human-readable marker LINE and a record's payload is
// everything up to the next marker line, minus the single framing newline the
// logger appended after each write. The framing is therefore a scan for TEXT,
// which is why the legacy readers must compare whole lines — and why a payload
// that itself contains a marker line is a hazard they can only partly defend
// against.
//
// FRAMED (what this package writes for every NEW log):
//
//	R 412\n<exactly 412 bytes>\n
//	C 91\n<exactly 91 bytes>\n
//
// The first line is the record HEADER: one tag byte, one space, the payload's
// length in decimal, and a newline. The payload follows VERBATIM, then ONE
// newline that belongs to the framing and not to the payload. Nothing in a record
// is decoration, no payload byte is ever scanned for text, and no byte sequence
// inside a payload can be mistaken for a header — the length decides where the
// record ends. (The framing adds METADATA and removes the marker word: the
// encoder never edits a payload byte.)
//
// # The tags
//
// A tag is one ASCII letter and names the record's DIRECTION — nothing else:
//
//	R  a request body handed to the provider            (→ REQUEST)
//	P  a request the guard/fail-fast ladder sent        (→ REQUEST (1214-guard…))
//	S  a complete (non-streamed) response body          (← RESPONSE)
//	C  one streamed SSE event                           (← CHUNK)
//	E  a failed HTTP attempt's diagnostics              (HTTP-ERROR)
//
// THEY ARE ASCII ON PURPOSE: a legacy marker line begins with a non-ASCII arrow
// (U+2192 / U+2190) or with the word HTTP-ERROR (which carries letters and a
// space after its first byte) — neither of which a framed header ("one tag byte,
// one space, digits") can be. That single property is what makes Detect total: a
// framed header and a legacy marker can never match the same line.
//
// # A file's framing is FIXED AT CREATION
//
// A raw log is created by one run and APPENDED to by later runs of the same row
// (a resume). Mixing two framings inside one file would force every reader into a
// mid-stream mode switch, so the writer decides ONCE at creation: the run that
// CREATES a file writes framed records, and a run that CONTINUES an existing file
// writes that file's own framing (detected, never guessed, never a flag). No file
// is ever mixed, so Detect — a bounded read of the head — is exact for every byte
// of every file.
package rawlogfmt

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
)

// The record tags. Each names the DIRECTION a record carries and nothing else
// (Rules 1/13/14: the values live here, once, and every reader and writer refers
// to them by name).
const (
	// TagRequest is a request body handed to the provider.
	TagRequest = "R"
	// TagProbeRequest is a request the guard/fail-fast ladder sent — a real send
	// that is not the agent's accepted call. It has its own tag because that
	// distinction cannot be recovered from a payload, and both the round index
	// and `gopi session stats` report it.
	TagProbeRequest = "P"
	// TagResponse is a complete (non-streamed) response body.
	TagResponse = "S"
	// TagChunk is one streamed SSE event.
	TagChunk = "C"
	// TagError is a failed HTTP attempt's diagnostics (status line + body).
	TagError = "E"
)

// ProbeAnnotation is what a probe request's tag means, spelled the way the legacy
// marker carried it, so a reader of either framing reports ONE string for one
// event.
const ProbeAnnotation = "1214-guard"

// ProbeStreamAnnotation is the probe spelling for a streamed send.
const ProbeStreamAnnotation = ProbeAnnotation + ", stream"

// StreamResponseAnnotation marks a TagResponse record that was SYNTHESIZED from
// a streamed call's events rather than captured from a response body.
//
// It exists because the two cannot be told apart from the payload: a streamed
// call's complete response and a non-streamed one have the same JSON shape, so
// without this the log would claim a streamed body arrived whole. A reader that
// must not conflate them (a replay that would re-POST a body nobody sent) and a
// reader that does not care (a token audit that only wants `usage`) both get
// their answer from the header, in either framing.
const StreamResponseAnnotation = "stream"

// StreamResponsePartialSuffix extends StreamResponseAnnotation when the stream
// did NOT end normally — a transport error, a cancelled context, a killed
// reader. Everything before it is the same record; the suffix is the only thing
// that says "this is what the broken stream managed to produce". It is a
// SUFFIX, so a reader comparing against StreamResponseAnnotation with a prefix
// test gets both cases and one comparing exactly gets the complete one.
const StreamResponsePartialSuffix = ", partial"

// Header grammar pieces. `<tag><TagLengthSep><digits><RecordTerminator>` is one
// header line — `R 412\n`. The separator, the terminator and the decimal base are
// fixed here so the encoder and the header parser cannot drift.
const (
	// TagLengthSep separates a record's tag from its payload length.
	TagLengthSep = " "
	// RecordTerminator ends a header line and, once more, a record.
	RecordTerminator = byte('\n')
	headerFieldCount = 2
	// maxLengthDigits bounds the digit run a header may carry. 20 digits is past
	// the largest int64, so it is a SHAPE bound, not a value bound.
	maxLengthDigits = 20
	decimalBase     = 10
	bitSizeOfInt    = 64
	// maxAnnotationLen bounds a header's annotation field. It is a SHAPE bound on a
	// field this package writes (the two spellings it has ever written are
	// ProbeAnnotation and ProbeStreamAnnotation, 10 and 18 bytes), generous enough
	// that a future annotation does not need this constant touched, tight enough
	// that a corrupt line cannot be read as a header.
	maxAnnotationLen = 64
	// maxHeaderLineLen bounds a header line (tag + separator + digits +
	// terminator, or the annotated three-field shape), so the reader's buffer
	// arithmetic is provable.
	maxHeaderLineLen = 1 + len(TagLengthSep) + maxLengthDigits + len(TagLengthSep) + maxAnnotationLen + 1
)

// The legacy direction markers, verbatim: the whole line, no trailing newline. A
// legacy reader compares the WHOLE line (or the line plus one " (annotation)"
// suffix), never a substring — marker text can appear inside a payload, and that
// hazard is exactly what the framed format removes for new logs.
//
// They live here because three packages read them (internal/gopilog,
// internal/agent, internal/checkpoint) and the discriminator must know them; a
// second spelling anywhere else is the mirror-literal defect Rules 1/13/14 name.
const (
	MarkerRequest   = "→ REQUEST"
	MarkerResponse  = "← RESPONSE"
	MarkerChunk     = "← CHUNK"
	MarkerHTTPError = "HTTP-ERROR"
)

// Format is the on-disk framing of a raw log file.
type Format int

const (
	// FormatLegacy is the marker-line framing (see the package doc).
	FormatLegacy Format = iota
	// FormatFramed is the length-prefixed framing this package writes.
	FormatFramed
)

// String names the format for a reader (the zero value is NAMED, never left to
// print as "0").
func (f Format) String() string {
	switch f {
	case FormatFramed:
		return "framed"
	case FormatLegacy:
		return "legacy"
	}
	return "unknown(" + strconv.Itoa(int(f)) + ")"
}

// Record is ONE decoded record: its tag, its payload bytes exactly as written,
// and where it sits in the file.
type Record struct {
	// Tag is one of TagRequest/TagProbeRequest/TagResponse/TagChunk/TagError.
	Tag string
	// Payload is the record's bytes, exactly as the writer logged them. The
	// framing's trailing newline is NOT part of it.
	Payload []byte
	// Annotation is the legacy marker's parenthesised suffix, reproduced verbatim
	// for a legacy record and EMPTY for a framed one (the tag carries the
	// distinction there — see ProbeAnnotation).
	Annotation string
	// Offset is the byte offset of the record's header (framed) or marker line
	// (legacy).
	Offset int64
	// HeaderLen is the byte length of the header/marker line, newline included.
	HeaderLen int
	// HeaderTerminated reports whether the header/marker line ENDS with the
	// record terminator. It is false in exactly two cases: a legacy HTTP-ERROR
	// marker, whose diagnostics' first bytes the writer glued onto the same line
	// (see HTTPErrorRemainder), and a header truncated by a kill. A renderer needs
	// it because re-adding a newline that the file does not have invents a line.
	HeaderTerminated bool
	// Line is the 1-based line number the record's header sits on.
	Line int
	// EndLine is the 1-based line number the record's payload ends on.
	EndLine int
	// Terminated reports whether the framing's closing newline was present. A log
	// truncated by a kill can end mid-record, and a reader must be able to say so
	// instead of returning a short payload as if it were whole.
	Terminated bool
}

// Encode renders one framed record: header, payload verbatim, framing newline.
func Encode(tag string, payload []byte) []byte {
	return EncodeAnnotated(tag, "", payload)
}

// EncodeAnnotated renders one framed record carrying an ANNOTATION — the legacy
// marker's parenthesised suffix ("1214-guard", "1214-guard, stream"), which the
// round view renders as `probe · 1214-guard, stream` and `gopi session stats`
// counts as a probe round.
//
// WHY THE ANNOTATION SURVIVES THE FORMAT CHANGE. Dropping it would be a
// user-visible regression dressed as a byte-level refactor: the annotation is the
// ONLY thing that distinguishes a guard/fail-fast ladder send from the agent's
// accepted call, both views print it, and it cannot be recovered from a payload.
// So it moves from the marker's prose into the header's METADATA — where, like the
// length, it is never scanned for and a payload cannot forge it.
//
// Grammar: `<tag> <len> <annotation>\n`. The annotation is the rest of the header
// line, so it may not carry a newline; a tag with no annotation keeps the two-field
// shape, so an unannotated framed record is byte-identical to Encode's.
func EncodeAnnotated(tag, annotation string, payload []byte) []byte {
	header := HeaderLineAnnotated(tag, len(payload), annotation)
	out := make([]byte, 0, len(header)+len(payload)+1)
	out = append(out, header...)
	out = append(out, payload...)
	out = append(out, RecordTerminator)
	return out
}

// EncodeLegacy renders one record in the LEGACY framing — the marker line plus the
// payload plus one newline — so a run CONTINUING a legacy file appends bytes
// indistinguishable from the file's own history.
//
// A tag with no legacy spelling (none of the five has that problem today) is
// reported as an error rather than silently framed as something else: a record
// whose direction a reader cannot recover must not be written at all.
func EncodeLegacy(tag, annotation string, payload []byte) ([]byte, error) {
	marker, ok := LegacyMarker(tag, annotation)
	if !ok {
		return nil, fmt.Errorf("raw log: tag %q has no legacy marker spelling (annotation %q)", tag, annotation)
	}
	out := make([]byte, 0, len(marker)+len(payload)+1)
	out = append(out, marker...)
	out = append(out, payload...)
	out = append(out, RecordTerminator)
	return out, nil
}

// HeaderLine renders the header line for one record, terminator included. It is
// exported so a test — and any record walk — can build or recognise a header
// without re-implementing the grammar.
func HeaderLine(tag string, length int) string {
	return HeaderLineAnnotated(tag, length, "")
}

// HeaderLineAnnotated renders a header line carrying an annotation — see
// EncodeAnnotated for why the annotation survives. `""` produces the two-field
// shape.
func HeaderLineAnnotated(tag string, length int, annotation string) string {
	line := tag + TagLengthSep + strconv.Itoa(length)
	if annotation != "" {
		line += TagLengthSep + annotation
	}
	return line + string(RecordTerminator)
}

// IsTag reports whether s is one of the declared tags.
func IsTag(s string) bool {
	switch s {
	case TagRequest, TagProbeRequest, TagResponse, TagChunk, TagError:
		return true
	}
	return false
}

// ParseHeaderLine recognises a framed header line and returns its tag and payload
// length. ok=false means the line is not a framed header — the caller decides
// what else it might be.
//
// The grammar is strict on purpose: exactly one declared tag byte, exactly one
// space, then ASCII digits that are the whole rest of the line. Tolerance is what
// turns a payload line into a header, so there is none (a trailing "\r" is the
// single exception, so a log that passed through a Windows editor stays
// decodable).
func ParseHeaderLine(line []byte) (tag string, length int, ok bool) {
	tag, length, _, ok = ParseHeaderLineAnnotated(line)
	return tag, length, ok
}

// ParseHeaderLineAnnotated is ParseHeaderLine plus the optional annotation field
// (`<tag> <len> <annotation>` — see EncodeAnnotated). The annotation is "" for the
// two-field shape, so one parser serves both and no reader has to ask which it has.
//
// STRICTNESS IS UNCHANGED where it matters: the length field must be the WHOLE
// field between the tag and the next separator, and a separator followed by
// nothing is not an annotation (it is a malformed header — `R 5 \n` stays refused).
func ParseHeaderLineAnnotated(line []byte) (tag string, length int, annotation string, ok bool) {
	if bytes.HasSuffix(line, []byte{RecordTerminator}) {
		line = line[:len(line)-1]
	}
	line = bytes.TrimSuffix(line, []byte{'\r'})
	if len(line) < 1+len(TagLengthSep)+1 || len(line) > maxHeaderLineLen {
		return "", 0, "", false
	}
	if len(line) < 2 || line[1] != TagLengthSep[0] {
		return "", 0, "", false
	}
	t := string(line[:1])
	if !IsTag(t) {
		return "", 0, "", false
	}
	rest := line[1+len(TagLengthSep):]
	digits := rest
	if i := bytes.IndexByte(rest, TagLengthSep[0]); i >= 0 {
		digits = rest[:i]
		annotation = string(rest[i+len(TagLengthSep):])
		if annotation == "" {
			return "", 0, "", false
		}
		if strings.ContainsAny(annotation, "\r\n") {
			return "", 0, "", false
		}
	}
	if len(digits) == 0 || len(digits) > maxLengthDigits {
		return "", 0, "", false
	}
	for _, c := range digits {
		if c < '0' || c > '9' {
			return "", 0, "", false
		}
	}
	n, err := strconv.ParseInt(string(digits), decimalBase, bitSizeOfInt)
	if err != nil || n < 0 {
		return "", 0, "", false
	}
	return t, int(n), annotation, true
}

// markerMatches is the ONE "is this line that marker" test: whole-line equality
// for the base marker, or base + " (annotation)" for the guarded variants the
// fail-fast ladder writes. A payload line that merely CONTAINS the marker text
// never matches, because the comparison starts at byte 0 and consumes the whole
// line.
func markerMatches(line, marker string) bool {
	if line == marker {
		return true
	}
	if len(line) <= len(marker)+2 || line[:len(marker)+2] != marker+" (" {
		return false
	}
	return line[len(line)-1] == ')'
}

// IsLegacyMarkerLine reports whether a line opens a legacy record.
//
// HTTP-ERROR is special on disk, MEASURED from the writer that produced it
// (internal/deepseek/minimal_core.go's non200RawLog): it passes "HTTP-ERROR" as
// the direction with NO trailing newline, so the diagnostics' first bytes sit on
// the SAME line — `HTTP-ERROR← streaming attempt — HTTP 500 (non-OK)`. The test is
// therefore "equals HTTP-ERROR, or is HTTP-ERROR followed by a non-alphanumeric
// byte", which covers both the glued real shape and a bare marker, and cannot
// match a word like "HTTP-ERRORS".
func IsLegacyMarkerLine(line string) bool {
	if markerMatches(line, MarkerRequest) ||
		markerMatches(line, MarkerResponse) ||
		markerMatches(line, MarkerChunk) {
		return true
	}
	return isHTTPErrorMarker(line)
}

// isHTTPErrorMarker is the ONE test for the HTTP-ERROR marker line (bare, or with
// the diagnostics' bytes glued onto the same line by the writer above).
func isHTTPErrorMarker(line string) bool {
	if line == MarkerHTTPError {
		return true
	}
	if len(line) <= len(MarkerHTTPError) || !strings.HasPrefix(line, MarkerHTTPError) {
		return false
	}
	c := line[len(MarkerHTTPError)]
	return !(c >= 'A' && c <= 'Z') && !(c >= 'a' && c <= 'z') && !(c >= '0' && c <= '9')
}

// HTTPErrorRemainder returns the diagnostics bytes the writer glued onto the
// marker line itself ("" for a bare marker). A reader that renders a diagnostics
// record verbatim must prepend this, or it loses the status line.
func HTTPErrorRemainder(line string) string {
	if !isHTTPErrorMarker(line) || line == MarkerHTTPError {
		return ""
	}
	return line[len(MarkerHTTPError):]
}

// LegacyAnnotation extracts the parenthesised suffix of an annotated legacy marker
// ("" for a plain marker or a non-marker line).
func LegacyAnnotation(line, marker string) string {
	if line == marker || !markerMatches(line, marker) {
		return ""
	}
	return line[len(marker)+2 : len(line)-1]
}

// LegacyTag maps a legacy marker line onto the framed tag carrying the same
// direction, so ONE round model describes records from either framing.
//
// It answers TagRequest for an ANNOTATED request marker too, because the caller
// has the annotation in hand and decides whether the send was a probe
// (ProbeTag) — the mapping must not silently promote or demote a record.
func LegacyTag(line string) (string, bool) {
	switch {
	case markerMatches(line, MarkerRequest):
		return TagRequest, true
	case markerMatches(line, MarkerResponse):
		return TagResponse, true
	case markerMatches(line, MarkerChunk):
		return TagChunk, true
	case isHTTPErrorMarker(line):
		return TagError, true
	}
	return "", false
}

// ProbeTag upgrades a request tag to TagProbeRequest when the record carries a
// probe annotation in EITHER framing, so the two framings agree on what a probe
// is.
func ProbeTag(tag, annotation string) string {
	if (tag == TagRequest || tag == TagProbeRequest) && annotation != "" {
		return TagProbeRequest
	}
	return tag
}

// LegacyMarker renders the legacy marker LINE (terminator included) for a tag,
// reproducing a legacy file's own vocabulary when a continued run appends to it.
// ok=false means the tag has no legacy spelling.
func LegacyMarker(tag, annotation string) (string, bool) {
	base := ""
	switch tag {
	case TagRequest, TagProbeRequest:
		base = MarkerRequest
	case TagResponse:
		base = MarkerResponse
	case TagChunk:
		base = MarkerChunk
	case TagError:
		// The legacy writer passed "HTTP-ERROR" as the direction and the payload
		// put its first byte on the SAME line, so this marker carries no
		// terminator of its own: EncodeLegacy appends the payload directly and
		// then one framing newline, exactly as the legacy logger did.
		return MarkerHTTPError, annotation == ""
	}
	if base == "" {
		return "", false
	}
	if annotation == "" {
		return base + string(RecordTerminator), true
	}
	return base + " (" + annotation + ")" + string(RecordTerminator), true
}

// CanDecodeHeader reports whether line looks like the START of a decodable record
// in its framing — the predicate a TAIL reader uses to pick a place where decoding
// can safely begin, because a byte offset in the middle of a payload has no framing
// of its own.
//
// It is deliberately a SHAPE test, not a verdict, and the two framings answer from
// different evidence:
//
//   - FRAMED: the line must parse as a header (parseHeaderLineAnnotated), whose
//     digit run is bounded and whose tag is one of the four directions.
//   - LEGACY: a byte in the middle of a payload is arbitrary — often invalid UTF-8,
//     often a newline — so the ONLY thing that can be said is that a decodable
//     record begins at a line boundary where the line is a legacy marker. The
//     check is therefore the same marker test every legacy reader uses.
//
// A caller must still DECODE from the chosen offset and be prepared for the
// attempt to fail: this function narrows the search, and the decoder is the judge
// (a payload line can look like a header). That is why Reader has no
// "seek and trust" — the caller walks from a candidate and keeps the answer only
// if the whole remaining file decodes.
func CanDecodeHeader(line string, format Format) bool {
	switch format {
	case FormatFramed:
		_, _, _, ok := ParseHeaderLineAnnotated([]byte(line))
		return ok
	case FormatLegacy:
		return IsLegacyMarkerLine(line)
	}
	return false
}

// Detect answers which framing a raw log uses — the ONE discriminator every reader
// calls, so no two of them can disagree. It is EXACT and AUTOMATIC: no flag, no
// caller hint, no heuristic on file size.
//
// A record boundary decides it: a framed header line means FormatFramed, a legacy
// marker line means FormatLegacy. Comment/header (`#`) lines are skipped, because
// they are boundaries in both framings and carry no direction. A file with no
// record at all (empty, or only header lines) is FormatLegacy — there is nothing to
// decode, and every legacy reader already answers "no rounds" for it.
//
// The read is BOUNDED (detectScanBytes) and stops at the first record boundary: a
// log's framing is fixed at creation, so its first record decides the file.
func Detect(path string) (Format, error) {
	format, _, err := DetectExact(path)
	return format, err
}

// DetectExact is Detect plus the one fact a WRITER needs and a reader does not:
// whether a RECORD actually decided it.
//
// decided=false means the file carries no record in its head at all (empty, or
// only `#` comment/header lines). For a reader that is the same answer as legacy
// — there are no rounds to decode either way — but for a writer it is not an
// answer at all: the file has nothing to be compatible WITH, so the writer
// decides, and a writer that decides writes FormatFramed.
//
// THAT IS WHY THE THIRD VALUE EXISTS. Without it, "no records yet" and "a legacy
// file" would be indistinguishable to the one caller that must not confuse them,
// and a fresh log whose creation header is written before its first record would
// be classified legacy for the rest of its life (the header is written at
// creation, always before any record).
func DetectExact(path string) (Format, bool, error) {
	f, err := os.Open(path)
	if err != nil {
		return FormatLegacy, false, fmt.Errorf("raw log format: %s: %w", path, err)
	}
	defer f.Close()
	return DetectReaderExact(f)
}

// detectScanBytes bounds the head read Detect performs. Every raw log opens with
// at most a handful of `#` header lines before its first record header, so 8 KiB is
// generous by two orders of magnitude; the bound keeps the probe from reading a
// whole 1.8 GB log to answer one question.
const detectScanBytes = 8 << 10

// DetectReader is Detect for a caller that already holds the file open (the writer
// uses it to decide which framing to continue a file in). It CONSUMES the reader;
// a caller that needs the head afterwards must re-open or Seek.
func DetectReader(r io.Reader) (Format, error) {
	format, _, err := DetectReaderExact(r)
	return format, err
}

// DetectReaderExact is DetectReader with the "a record decided this" flag — see
// DetectExact for what the flag is for. It CONSUMES the reader.
func DetectReaderExact(r io.Reader) (Format, bool, error) {
	buf := make([]byte, detectScanBytes)
	n, err := io.ReadFull(r, buf)
	if err != nil && !errors.Is(err, io.ErrUnexpectedEOF) && !errors.Is(err, io.EOF) {
		return FormatLegacy, false, fmt.Errorf("raw log format: read head: %w", err)
	}
	head := buf[:n]
	for len(head) > 0 {
		line, rest := head, []byte(nil)
		if i := bytes.IndexByte(head, RecordTerminator); i >= 0 {
			line, rest = head[:i+1], head[i+1:]
		}
		head = rest
		trimmed := strings.TrimRight(string(line), "\r\n")
		if trimmed == "" || trimmed[0] == '#' {
			// A comment/header line: a boundary in BOTH framings, carrying no
			// direction, so it must not be allowed to decide.
			continue
		}
		if _, _, ok := ParseHeaderLine(line); ok {
			return FormatFramed, true, nil
		}
		if IsLegacyMarkerLine(trimmed) {
			return FormatLegacy, true, nil
		}
		// The first line is payload, not a record opening: no framing was declared
		// here. Legacy is the honest answer, and its readers answer "no rounds"
		// for such a file, which is the truth.
		return FormatLegacy, false, nil
	}
	return FormatLegacy, false, nil
}
