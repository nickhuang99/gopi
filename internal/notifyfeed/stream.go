// Package notifyfeed is the SUBSCRIBER side of gopi's task-event notify
// stream: it reads a notify sink (the file a configured `notify.cmd` appends
// the delivered payloads to) as ORDERED ENTRIES, tolerates the two shapes a
// real sink has in practice, and serves them with a CURSOR so a consumer that
// restarts can resume where it stopped.
//
// WHY THIS PACKAGE EXISTS. gopi already PRODUCES lifecycle events — every task
// status transition and every synthetic alarm is delivered to the configured
// `notify.cmd` / `notify.webhook` (internal/tools/tasknotify.go). What was
// missing is the CONSUMER side, and the gap has a measured cost:
//
//  1. THE SINK IS NOT LINE-ORIENTED IN PRACTICE, and reading it line-by-line is
//     SILENTLY WRONG. Every event delivered before the producer-side framing fix
//     (commit e2b7e379, 2026-09-20) was appended WITHOUT a line terminator, so
//     consecutive JSON objects were concatenated — the operator's own sink,
//     51 MB, reports `wc -l` = 1. A consumer that reads it line-by-line sees one
//     enormous "line", and `jq -c . | wc -l` appears to work (it splits a
//     concatenated stream back into values) which is exactly why the defect
//     survived: the WRONG reader looked right. This package reads BOTH shapes —
//     newline-framed and concatenated — because the file on disk is the union
//     of both eras and truncating either loses real events.
//
//  2. A CORRUPT RECORD MUST NOT COST THE ONES AROUND IT. The same sink carries
//     one historical record with a nested, escaped probe payload that swallowed
//     a following object's tail (measured: 2026-09-12, the `zombie` field of a
//     `t-z-dedup` stale event). A strict decoder (or `jq`) stops at the damage;
//     the operator's sink is append-only history and cannot be edited, so the
//     reader must RESYNCHRONISE — skip to the next `{"` and keep going — and
//     say so, so a consumer can see the gap instead of inferring a clean stream.
//
//  3. RESTART LOSES EVENTS WITHOUT A CURSOR. A consumer (a PA loop, a fleet
//     monitor) that re-reads the whole file every time duplicates everything; one
//     that keeps an in-memory position loses everything appended while it was
//     down. A cursor is a byte offset PLUS the resumed line's (length, hash) so a
//     TRUNCATED OR ROTATED sink is detected rather than silently mis-resumed.
//
// WHAT IT IS NOT. It is a library with no processes, no timers and no state of
// its own (Rule 35: zero new persistent dependencies) — the sink is the
// existing `notify.cmd` target, the cursor is a file the CONSUMER owns, and the
// only thing this package adds to a host is code that runs when somebody asks.
package notifyfeed

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
)

// The reader's knobs. Every literal is named here (Rule 1): the chunk size the
// stream is slurped in, the sizes its largest single record may reach, and the
// bound on how far a resynchronisation looks before it gives up.
const (
	// readChunkBytes is the slurp granularity. The whole sink is scanned in
	// chunks so a 50 MB sink is never held twice in memory and so a read on a
	// FIFO/tail-style sink still makes progress.
	readChunkBytes = 1 << 20
	// maxRecordScanBytes caps how much input a single candidate record may span
	// before the reader calls it damaged. It is deliberately far above the
	// largest real payload (a zombie evidence chain is a few KB) and far below
	// the sink's total size, so a stray `{` in prose cannot make the scanner
	// consume the rest of the file looking for a closing brace.
	maxRecordScanBytes = 1 << 20
	// resyncAnchor is the byte sequence a damaged stream is resynchronised on —
	// the start of a JSON object as gopi writes it (`json.Marshal` emits no
	// space after the brace). Falling back to it is what recovers every healthy
	// record after the one damaged one, instead of abandoning the file.
	resyncAnchor = `{"`
	// maxResyncBytes bounds the search for that anchor. Past it the reader stops
	// with a diagnostic rather than scanning a whole sink byte-by-byte for a
	// record that is not there.
	maxResyncBytes = 1 << 20
)

// ErrTruncatedTail is returned by a read whose LAST record is incomplete — the
// normal shape of reading a sink a live producer is appending to. It is
// distinct from damage on purpose: a truncated tail is a record that has not
// finished arriving (retry later, nothing is lost), while damage is a record
// that can never be parsed (skip it, the bytes are already on disk).
var ErrTruncatedTail = errors.New("notifyfeed: the last record is incomplete (a producer is still appending, or the sink was cut mid-record)")

// Record is ONE delivered notify payload, plus the framing provenance a
// subscriber needs to reason about what it just read.
type Record struct {
	// Offset is the byte offset of the record's first byte in the sink.
	Offset int64
	// Raw is the record's own bytes, exactly as they sit in the sink.
	Raw []byte
	// Line is true when the record was terminated by a newline (the framing
	// every event delivered after e2b7e379 has) and false when it was
	// concatenated onto the previous one. It is reported rather than hidden:
	// a subscriber reading an OLD sink should be able to see that the file
	// predates the framing fix.
	Line bool
	// Payload is the decoded object. It is a map rather than a struct so an
	// event field ADDED later (a new alarm's evidence) reaches a subscriber
	// without this package having to learn about it — the schema is owned by
	// internal/tools/tasknotify.go, and a consumer's own filters decide what
	// they need.
	Payload map[string]any
}

// Event returns the payload's `event` field ("" when absent or not a string).
// Every notify payload carries it; it is the wire name a subscriber filters on.
func (r Record) Event() string { return recordString(r.Payload, fieldEvent) }

// TaskID returns the payload's `task_id` field. NOTE: for the SESSION-level
// alarms (loop-stuck, session-wallclock-exceeded, …) and for build-failed this
// is the session id or the repo path, not a ledger row — see the subscription
// contract in devdoc/notify-subscription.md.
func (r Record) TaskID() string { return recordString(r.Payload, fieldTaskID) }

// Timestamp returns the payload's RFC3339 `timestamp` (delivery time).
func (r Record) Timestamp() string { return recordString(r.Payload, fieldTimestamp) }

// recordString reads one string field out of a decoded payload.
func recordString(m map[string]any, key string) string {
	v, ok := m[key].(string)
	if !ok {
		return ""
	}
	return v
}

// ScanResult is what one Read call reports: the records it decoded, the byte
// offset a consumer should resume from, and the honest account of what it could
// not decode.
type ScanResult struct {
	// Records are the decoded entries, in sink order.
	Records []Record
	// NextOffset is where the caller resumes. It points at the first byte
	// AFTER the last COMPLETE record, so a truncated tail is re-read rather
	// than lost.
	NextOffset int64
	// Damaged counts the spans the reader had to skip. Each entry is also
	// reported in Problems so the count is never a bare number.
	Damaged []Problem
	// Truncated is true when the input ended inside a record.
	Truncated bool
}

// Problem is one damaged span, named precisely enough to be looked up by hand.
type Problem struct {
	// Offset is the first byte of the damaged span.
	Offset int64
	// Bytes is how long the span was.
	Bytes int64
	// Reason is the decoder's own diagnosis (or the resynchronisation story).
	Reason string
}

// Read decodes the sink's byte range [from, until) and returns the records it
// holds. until < 0 means "to the end of the file as it is now" — the ordinary
// case for a consumer polling a live sink.
//
// Damaged spans are SKIPPED, not fatal: the bytes are already on disk in an
// append-only file nobody may rewrite, so the only useful behaviour is to say
// where the damage is and hand back every healthy record around it.
func Read(path string, from, until int64) (ScanResult, error) {
	data, _, err := readRange(path, from, until)
	if err != nil {
		return ScanResult{}, err
	}
	res := ScanResult{NextOffset: from}
	i := 0
	base := from
	for i < len(data) {
		// One leading newline (the frame terminator of the previous record) is
		// part of the FRAMING, not of this record: consume it here so the
		// record's own `Line` flag is computed from its terminator, not from
		// its predecessor's.
		if data[i] == newlineByte {
			i++
			res.NextOffset = base + int64(i)
			continue
		}
		if data[i] == ' ' || data[i] == '\t' || data[i] == '\r' {
			i++
			res.NextOffset = base + int64(i)
			continue
		}
		start := i
		end, ok, decodeErr := findRecordEnd(data, i)
		if !ok {
			// The remainder does not form a complete record. Two shapes look
			// identical from here and have OPPOSITE remedies:
			//   - a record still arriving (a live producer mid-append) → a
			//     truncated tail: resume from HERE, nothing is lost;
			//   - damage that never closes (the measured escaped-payload
			//     record) → skip it, so the healthy records after it are
			//     reachable instead of being abandoned with it.
			// The discriminator is whether a LATER record start actually
			// yields a complete record: a still-arriving record sits at the
			// end of everything, while damage has healthy records behind it.
			next, ok := resyncToComplete(data, i)
			if !ok {
				res.Truncated = true
				break
			}
			res.Damaged = append(res.Damaged, Problem{
				Offset: base + int64(i),
				Bytes:  int64(next - i),
				Reason: decodeErr.Error(),
			})
			i = next
			continue
		}
		raw := data[start:end]
		var payload map[string]any
		if err := json.Unmarshal(bytes.TrimSpace(raw), &payload); err != nil {
			// The span LOOKS closed but does not decode (a truncated string
			// that happened to contain a brace, an escaped probe payload).
			// Same choice as above: a later COMPLETE record means this span is
			// damage; no later record means the tail is still arriving.
			next, ok := resyncToComplete(data, start)
			if !ok {
				res.Truncated = true
				break
			}
			res.Damaged = append(res.Damaged, Problem{
				Offset: base + int64(start),
				Bytes:  int64(next - start),
				Reason: err.Error(),
			})
			i = next
			continue
		}
		res.Records = append(res.Records, Record{
			Offset:  base + int64(start),
			Raw:     raw,
			Line:    end < len(data) && data[end] == newlineByte,
			Payload: payload,
		})
		i = end
		res.NextOffset = base + int64(i)
	}
	return res, nil
}

// newlineByte is the byte that terminates a framed record.
const newlineByte = byte('\n')

// The payload field names this package reads by name. They are the wire
// contract's own spellings (internal/tools taskNotifyPayload's json tags) —
// declared once so a filter, a formatter and a test cannot drift from them.
const (
	fieldEvent     = "event"
	fieldTaskID    = "task_id"
	fieldTimestamp = "timestamp"
	fieldSummary   = "summary"
	fieldResult    = "result"
)

// readRange slurps the sink's [from, until) window. A missing sink reads as an
// empty stream (a consumer may legitimately poll a sink that has not been
// written yet — that is not an error, it is "no events yet"); any OTHER error
// is returned (Rule 15: a permission failure must not read as "nothing
// happened").
func readRange(path string, from, until int64) ([]byte, int64, error) {
	f, err := os.Open(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, 0, nil
		}
		return nil, 0, fmt.Errorf("notifyfeed: open %s: %w", path, err)
	}
	defer f.Close()
	fi, err := f.Stat()
	if err != nil {
		return nil, 0, fmt.Errorf("notifyfeed: stat %s: %w", path, err)
	}
	size := fi.Size()
	if from < 0 {
		from = 0
	}
	if from > size {
		// The sink shrank under us (rotation/truncation). Read nothing and say
		// so by reporting an empty window; the CURSOR carries the verdict.
		return nil, size, nil
	}
	length := size - from
	if until >= 0 && until < size {
		length = until - from
	}
	if length <= 0 {
		return nil, size, nil
	}
	var buf bytes.Buffer
	remaining := length
	for remaining > 0 {
		n := int64(readChunkBytes)
		if remaining < n {
			n = remaining
		}
		chunk := make([]byte, n)
		read, err := f.ReadAt(chunk, from+int64(buf.Len()))
		if read > 0 {
			buf.Write(chunk[:read])
		}
		if err != nil {
			if errors.Is(err, io.EOF) {
				break
			}
			return nil, size, fmt.Errorf("notifyfeed: read %s: %w", path, err)
		}
		if read == 0 {
			break
		}
		remaining -= int64(read)
	}
	return buf.Bytes(), size, nil
}

// findRecordEnd returns the offset just past the JSON object starting at i, and
// whether a complete one was found. It is a brace/string-aware scanner rather
// than `json.Decoder`, because the sink is a CONCATENATED stream: a decoder
// would consume neighbouring damaged records as one, while this reports the
// damaged span's real bounds so the resynchronisation can be precise.
func findRecordEnd(data []byte, i int) (int, bool, error) {
	// Leading whitespace was already consumed by the caller's loop, but a
	// concatenated stream may have a newline between objects.
	for i < len(data) && (data[i] == newlineByte || data[i] == ' ' || data[i] == '\t' || data[i] == '\r') {
		i++
	}
	if i >= len(data) {
		return i, false, io.ErrUnexpectedEOF
	}
	if data[i] != '{' {
		return i, false, fmt.Errorf("notifyfeed: byte %d is %q, not the start of a JSON object", i, data[i])
	}
	depth := 0
	inString := false
	escaped := false
	for j := i; j < len(data); j++ {
		c := data[j]
		if j-i > maxRecordScanBytes {
			return i, false, fmt.Errorf("notifyfeed: no complete record within %d bytes", maxRecordScanBytes)
		}
		if inString {
			switch {
			case escaped:
				escaped = false
			case c == '\\':
				escaped = true
			case c == '"':
				inString = false
			}
			continue
		}
		switch c {
		case '"':
			inString = true
		case '{', '[':
			depth++
		case '}', ']':
			depth--
			if depth == 0 {
				return j + 1, true, nil
			}
			if depth < 0 {
				return i, false, fmt.Errorf("notifyfeed: unbalanced closing brace at byte %d", j)
			}
		}
	}
	return i, false, io.ErrUnexpectedEOF
}

// resyncToComplete scans forward for the next offset at which a COMPLETE JSON
// record starts, returning it and whether one exists before the end of the
// window. It is the discriminator between the two shapes a non-decodable span
// can have, and the discriminator is the LOOKAHEAD rather than the span's own
// appearance: damage has healthy records behind it, while a record still
// arriving is at the end of every byte there is.
//
// Scanning from `i+1` (not `i`) matters: searching from `i` would find the
// damaged span's OWN opening brace and resynchronise straight back onto it —
// a resynchronisation that skips nothing is a consumer that spins.
func resyncToComplete(data []byte, i int) (int, bool) {
	limit := i + maxResyncBytes
	if limit > len(data) {
		limit = len(data)
	}
	for search := i + 1; search < limit; {
		idx := bytes.Index(data[search:limit], []byte(resyncAnchor))
		if idx < 0 {
			return 0, false
		}
		candidate := search + idx
		// A candidate that does not complete (ok false, err non-nil) is the
		// ORDINARY case while scanning past a damaged span — it is why this
		// function exists — so the next iteration advances one byte and looks
		// again rather than treating the malformed candidate as fatal.
		if _, ok, err := findRecordEnd(data, candidate); err == nil && ok {
			return candidate, true
		}
		search = candidate + 1
	}
	return 0, false
}

// Cursor is a consumer's resume point: a byte offset into one sink, plus the
// identity of the line the consumer had already read there. The identity is
// what makes a RESUMED read safe — an offset alone cannot tell a consumer that
// the file it points into is no longer the file it was reading.
type Cursor struct {
	// Version is the cursor format's version, so a future change is a refusal
	// rather than a misread.
	Version int `json:"version"`
	// Sink is the path (or configured command) the offset refers to. A cursor
	// carried to a DIFFERENT sink is refused, not silently applied.
	Sink string `json:"sink"`
	// Offset is the first byte NOT yet consumed.
	Offset int64 `json:"offset"`
	// Size is the sink's size when the cursor was written, and
	// NextScore is the (length, sha256) of the FIRST unread line. Together they
	// detect truncation and rotation: a sink that is now shorter than Size, or
	// whose byte at Offset no longer hashes to NextScore, is not the stream
	// this cursor describes.
	Size      int64  `json:"size"`
	NextLen   int    `json:"next_len"`
	NextScore string `json:"next_sha256"`
}

// CursorVersion is the format version every cursor this package writes carries.
const CursorVersion = 1

// ErrCursorStale is returned when a cursor cannot be applied to the sink it
// names: the sink was truncated, rotated, or rewritten. It is an ERROR rather
// than a warning because silently resuming from the wrong byte is how a
// consumer misses events while believing it read them all.
var ErrCursorStale = errors.New("notifyfeed: cursor does not describe this sink (truncated, rotated, or a different file)")

// ErrCursorForeign is returned when the cursor names a different sink. The two
// cases are separate errors because their remedies differ: a foreign cursor is
// a consumer configuration mistake, a stale one is a sink-lifecycle event.
var ErrCursorForeign = errors.New("notifyfeed: cursor belongs to a different sink")

// LoadCursor reads a cursor file. A missing file is NOT an error: it is the
// first run of a consumer, and the honest answer is a zero cursor the caller
// turns into "start from the beginning" — never a silent failure (Rule 15).
func LoadCursor(path string) (Cursor, bool, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return Cursor{}, false, nil
		}
		return Cursor{}, false, fmt.Errorf("notifyfeed: read cursor %s: %w", path, err)
	}
	var c Cursor
	if err := json.Unmarshal(raw, &c); err != nil {
		return Cursor{}, false, fmt.Errorf("notifyfeed: cursor %s is not a cursor document: %w", path, err)
	}
	if c.Version != CursorVersion {
		return Cursor{}, false, fmt.Errorf("notifyfeed: cursor %s is version %d, this reader speaks %d", path, c.Version, CursorVersion)
	}
	return c, true, nil
}

// Validate checks a loaded cursor against the sink it names and returns the
// byte offset a read may resume from. A zero-value cursor (no previous run)
// resumes from 0.
func (c Cursor) Validate(sinkPath string, size int64, at func(offset int64, n int) ([]byte, error)) (int64, error) {
	if c.Version == 0 && c.Sink == "" {
		return 0, nil // the first run
	}
	if c.Sink != sinkPath {
		return 0, fmt.Errorf("%w: cursor names %q, sink is %q", ErrCursorForeign, c.Sink, sinkPath)
	}
	if size < c.Offset {
		return 0, fmt.Errorf("%w: sink is %d bytes, cursor points at %d", ErrCursorStale, size, c.Offset)
	}
	if c.NextLen == 0 && c.NextScore == "" {
		return c.Offset, nil
	}
	got, err := at(c.Offset, c.NextLen)
	if err != nil {
		return 0, fmt.Errorf("notifyfeed: cursor identity read: %w", err)
	}
	if hashBytes(got) != c.NextScore {
		return 0, fmt.Errorf("%w: the byte at offset %d is no longer the record the cursor had already skipped", ErrCursorStale, c.Offset)
	}
	return c.Offset, nil
}

// NewCursor builds the cursor to store after a read: the resume offset plus the
// identity of the first unread record.
func NewCursor(sinkPath string, size, next int64, firstUnread []byte) Cursor {
	return Cursor{
		Version:   CursorVersion,
		Sink:      sinkPath,
		Offset:    next,
		Size:      size,
		NextLen:   len(firstUnread),
		NextScore: hashBytes(firstUnread),
	}
}

// SaveCursor writes the cursor atomically (tmp + rename in the same directory):
// a consumer killed mid-write must never leave a half-written cursor that the
// next run would read as "resume from offset 12345" — Rule 30's spirit applied
// to a consumer's own state.
func SaveCursor(path string, c Cursor) error {
	body, err := json.Marshal(c)
	if err != nil {
		return fmt.Errorf("notifyfeed: marshal cursor: %w", err)
	}
	body = append(body, newlineByte)
	tmp := path + cursorTempSuffix
	if err := os.WriteFile(tmp, body, cursorFileMode); err != nil {
		return fmt.Errorf("notifyfeed: write cursor %s: %w", tmp, err)
	}
	if err := os.Rename(tmp, path); err != nil {
		return fmt.Errorf("notifyfeed: install cursor %s: %w", path, err)
	}
	return nil
}

const (
	// cursorTempSuffix is the staging name a cursor is written through, and
	// cursorFileMode is the mode a cursor file gets: it is consumer state, not
	// a secret, but it belongs to one user's run.
	cursorTempSuffix = ".tmp"
	cursorFileMode   = 0o600
)

// hashBytes is the identity function the cursor stores: a truncated SHA-256 of
// the first unread record. Truncation is a size choice, not a security claim —
// the cursor detects rotation, it is not an integrity seal.
func hashBytes(b []byte) string {
	if len(b) == 0 {
		return ""
	}
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:cursorHashBytes])
}

// cursorHashBytes is how many bytes of the digest are kept.
const cursorHashBytes = 8

// CursorIdentityBytes is how many bytes of the first unread record the cursor
// stores the identity of. It is exported because the WRITER of a cursor (a CLI
// verb, a consumer) must read exactly this many bytes to build one — a caller
// that guessed a smaller window would store an identity that could never match
// on resume.
const CursorIdentityBytes = 64

// RecordSummary renders a record's one-line human digest: the event, the row's
// summary text, and the failure digest when the payload carries one. It lives
// in this package rather than in each caller so every consumer's report shows
// the same fields in the same order (Rule 14: one definition for a shared
// shape) — and so a field added to the payload reaches every report together.
func RecordSummary(r Record) string {
	out := recordString(r.Payload, fieldSummary)
	if result := recordString(r.Payload, fieldResult); result != "" {
		if out != "" {
			out += " — "
		}
		out += result
	}
	return firstLine(out)
}

// firstLine collapses an embedded multi-line digest onto one line so every
// record stays one greppable line.
func firstLine(s string) string {
	for i, r := range s {
		if r == '\n' || r == '\r' {
			return s[:i]
		}
	}
	return s
}
