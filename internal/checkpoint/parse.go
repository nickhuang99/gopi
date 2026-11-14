// Package checkpoint extracts resume checkpoints from gopi session raw logs.
//
// Every raw log (~/.gopi/raw/gopi-raw-*.log) is a byte-exact transcript of
// the HTTP layer: each round writes
//
//	→ REQUEST\n  <request body>\n
//	← CHUNK\n    <raw SSE event>\n          (multi-line; repeated)
//	← RESPONSE\n <response body>\n          (non-stream fallback)
//
// The → REQUEST payload is the exact bytes handed to the Claude Messages
// API: model, system blocks (kernel prompt + GOPI.md), full conversation
// history (including thinking blocks with their signatures), tool defs,
// stream/thinking/reasoning_effort params. That makes every → REQUEST a
// complete, crash-safe context checkpoint — resume is extraction + bootstrap,
// not new capture (see devdoc/resume-feasibility.md).
//
// Parsing is marker-driven per entry (feasibility §4.1): a marker line is
// exactly one of the directions above at a line boundary, and an entry's
// payload is everything after it up to the next marker (or EOF), minus the
// single trailing newline the raw logger appends after each write. ← CHUNK
// payloads are multi-line (SSE framing preserved), so "one line per marker"
// parsing is wrong — this package never assumes payload shape.
package checkpoint

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/nickhuang99/gopi/internal/deepseek"
	"github.com/nickhuang99/gopi/internal/rawlogfmt"
	"github.com/nickhuang99/gopi/internal/scratch"
)

// Marker lines in the raw log (exactly these bytes at a line boundary).
const (
	dirRequest  = "→ REQUEST\n"
	dirChunk    = "← CHUNK\n"
	dirResponse = "← RESPONSE\n"
	// dirMarker is the `#` HEADER the trim arm writes in place of the dropped
	// middle (scratch.SessionsRawLogTrimMarkerPrefix — the writer's own
	// constant, never a re-spelling: Rules 1/13/14). It is MARKER + "", i.e. the
	// match is a line START rather than a whole line, because every marker line
	// continues with a timestamp.
	//
	// WHY IT MUST BE A MARKER HERE AT ALL (the measured defect this repairs):
	// before it existed, the parser knew only the three round directions, so a
	// trimmed log's head `→ REQUEST` ran to the NEXT round marker — which sat on
	// the far side of the marker line — and swallowed `# gopi … TRIMMED …` into
	// the request payload. `json.Unmarshal` then failed with
	// `invalid character '#' after top-level value`: the tool made a readable log
	// unreadable, and the failure arrived as a JSON syntax error that names
	// neither the tool nor the file's history.
	dirMarker = scratch.SessionsRawLogTrimMarkerPrefix
)

// Direction names for Entry.Dir, matching the raw log markers without the
// trailing newline.
const (
	DirectionRequest  = "→ REQUEST"
	DirectionChunk    = "← CHUNK"
	DirectionResponse = "← RESPONSE"
	// DirectionMarker is the trim arm's `#` header record (see dirMarker).
	DirectionMarker = scratch.SessionsRawLogTrimMarkerPrefix
)

// ErrTrimmedRawLog is RETURNED (wrapped, so errors.Is matches it) by every
// checkpoint reader asked to answer from a log `gopi session gc --trim-raw-log`
// has rewritten.
//
// WHY A REFUSAL AND NOT BEST EFFORT: a trimmed log still holds its head and its
// tail, so a reader that merely tolerates the marker line finds the HEAD's first
// `→ REQUEST` and could return it. That answer is not a degraded answer, it is a
// WRONG one — the head's request is the run's first round, offered as "the last
// state", and a resume seeded from it silently discards everything the run did
// after round one. The middle is gone; the honest answer is that nobody can say
// where the run stopped, and the sentence says so by name (Rule 15).
var ErrTrimmedRawLog = errors.New(scratch.TrimmedLogResumeRefusal)

// Entry is one raw-log record: a direction marker plus its exact payload.
// Payload is the original bytes handed to RawLogger.LogFunc — for a request
// it re-marshals byte-exactly into the original ClaudeRequest body.
type Entry struct {
	Dir     string // DirectionRequest | DirectionChunk | DirectionResponse
	Payload []byte // exact bytes, trailing LogFunc newline removed
	Offset  int64  // byte offset of the marker in the file

	// payloadTruncated is set by LastRequest's tail-window search when the
	// request's payload ran past the window end — the caller re-reads the
	// span to EOF before using Payload. Never set by Scan.
	payloadTruncated bool
}

// findMarker returns the marker bytes (with trailing newline) and its byte
// offset at or after start, or ("", 0) when no marker is found. Uses
// bytes.Index (memmem) per marker and picks the earliest hit. A marker must
// sit at a line boundary (start of file or right after '\n') so payload
// lines that merely begin with marker text (impossible for JSON/SSE payloads
// today, cheap to guard) are never misread as entries.
func findMarker(data []byte, start int) (string, int) {
	best := ""
	bestOff := len(data) + 1
	for _, m := range []string{dirRequest, dirChunk, dirResponse, dirMarker} {
		from := start
		for {
			j := bytes.Index(data[from:], []byte(m))
			if j < 0 {
				break
			}
			abs := from + j
			if abs == 0 || data[abs-1] == '\n' {
				if abs < bestOff {
					best, bestOff = m, abs
				}
				break
			}
			from = abs + 1 // not a line boundary — keep looking
		}
	}
	if best == "" {
		return "", 0
	}
	return best, bestOff
}

// LastRequest extracts the LAST complete → REQUEST entry of a raw log — the
// recommended resume point (crash-safe: raw log is fsync'd at every round
// boundary; risk 6 in the feasibility study). Returns the entry plus the
// request parsed into a ClaudeRequest. A log with no → REQUEST is an error.
//
// Performance: raw logs grow to hundreds of MB (3.7GB across ~/.gopi/raw),
// so this reads only the file TAIL in growing windows and stops at the last
// → REQUEST — it never scans the whole file. A log's final entries are
// chunks from the last round; the last request is always shortly before
// them, so the first 8MB window hits in practice.
func LastRequest(path string) (Entry, *deepseek.ClaudeRequest, error) {
	const (
		initialTailWindow = 8 * 1024 * 1024 // first attempt: last 8MB of the file
		maxTailWindow     = 512 * 1024 * 1024
	)

	f, err := os.Open(path)
	if err != nil {
		return Entry{}, nil, fmt.Errorf("checkpoint: open %s: %w", path, err)
	}
	defer f.Close()
	fi, err := f.Stat()
	if err != nil {
		return Entry{}, nil, fmt.Errorf("checkpoint: stat %s: %w", path, err)
	}
	if fi.Size() == 0 {
		return Entry{}, nil, fmt.Errorf("checkpoint: %s: no → REQUEST entry (empty raw log)", path)
	}
	// A TRIMMED LOG IS REFUSED BEFORE ANY PARSING, not merely tolerated while
	// parsing: the middle of the file is gone, so no reader can say where the
	// run stopped, and the head's first request must never be offered as "the
	// last state" (see ErrTrimmedRawLog). The check reads a bounded head, not
	// the file.
	trimmed, terr := scratch.RawLogCarriesTrimMarker(path)
	if terr != nil {
		return Entry{}, nil, fmt.Errorf("checkpoint: %s: %w", path, terr)
	}
	if trimmed {
		return Entry{}, nil, fmt.Errorf("checkpoint: %s: %w", path, ErrTrimmedRawLog)
	}
	// TWO FRAMINGS, TWO STRATEGIES. The legacy file has no lengths, so its last
	// request can only be found by scanning backwards for a marker; the framed one
	// states each payload's length, so a candidate start can be decoded forward and
	// judged. Both answer the same question with the same Entry, and the framing is
	// DETECTED, never assumed — assuming legacy would report "no request in the last
	// N bytes" for every framed log, which is the failure this dispatch removes.
	format, err := rawlogfmt.Detect(path)
	if err != nil {
		return Entry{}, nil, fmt.Errorf("checkpoint: %s: %w", path, err)
	}
	if format == rawlogfmt.FormatFramed {
		return lastRequestFramed(path, f, fi)
	}

	window := int64(initialTailWindow)
	if fi.Size() < window {
		window = fi.Size()
	}
	var lastReq *Entry
	for {
		start := fi.Size() - window
		data := make([]byte, window)
		if _, err := f.ReadAt(data, start); err != nil {
			return Entry{}, nil, fmt.Errorf("checkpoint: read tail of %s: %w", path, err)
		}
		if e, ok := lastRequestInWindow(data, start); ok {
			lastReq = &e
			// If the request's payload is truncated (no next marker inside
			// the window), re-read its span to EOF directly.
			if e.payloadTruncated {
				payload := make([]byte, fi.Size()-e.Offset-int64(len(dirRequest)))
				if _, err := f.ReadAt(payload, e.Offset+int64(len(dirRequest))); err != nil {
					return Entry{}, nil, fmt.Errorf("checkpoint: read payload of %s: %w", path, err)
				}
				lastReq.Payload = trimLogFuncNewline(payload)
			}
			break
		}
		// Grow the window toward the file head. Termination: the window
		// covers the whole file, or it has hit the maxTailWindow cap and
		// cannot grow further — rescanning the same bytes can never find a
		// new marker, so give up instead of looping forever on logs whose
		// last → REQUEST sits deeper than maxTailWindow from EOF.
		if window >= fi.Size() || window >= int64(maxTailWindow) {
			return Entry{}, nil, fmt.Errorf("checkpoint: %s: no → REQUEST entry in last %d bytes (no completed API round)", path, window)
		}
		window *= 2
		if window > int64(maxTailWindow) {
			window = int64(maxTailWindow)
		}
		if window > fi.Size() {
			window = fi.Size()
		}
	}

	var req deepseek.ClaudeRequest
	if err := json.Unmarshal(lastReq.Payload, &req); err != nil {
		return Entry{}, nil, fmt.Errorf("checkpoint: %s: parse last → REQUEST: %w", path, err)
	}
	return *lastReq, &req, nil
}

// lastRequestFramed recovers the LAST request record of a FRAMED raw log.
//
// The legacy strategy (scan backwards for a marker) does not generalize: a framed
// payload is a byte span, so a window's first record is usually a record that
// STARTED before the window and whose header bytes are therefore not in hand. What
// generalizes is the framing's own promise — a header states a length — so this
// walks FORWARD from a candidate start and keeps the answer only if the rest of the
// file decodes.
//
// Three things make that cheap and safe:
//
//   - the window grows from the tail (8 MiB, doubling, capped), exactly like the
//     legacy branch, because the last request always sits shortly before the tail;
//   - within a window, only LINE BOUNDARIES whose line is a decodable header
//     (rawlogfmt.CanDecodeHeader) are candidates;
//   - a candidate whose decode fails (a payload line that merely looks like a
//     header) is DISCARDED and the next candidate is tried — the decoder is the
//     judge, never the shape test.
//
// A file that does not decode from ANY candidate in a full-file window is an ERROR
// naming what was tried: silence here would report "no request" for a log whose
// framing is intact, which is the wrong diagnosis (Rule 15).
func lastRequestFramed(path string, f *os.File, fi os.FileInfo) (Entry, *deepseek.ClaudeRequest, error) {
	const (
		initialTailWindow = 8 * 1024 * 1024
		maxTailWindow     = 512 * 1024 * 1024
	)
	window := int64(initialTailWindow)
	if fi.Size() < window {
		window = fi.Size()
	}
	var tried int
	for {
		start := fi.Size() - window
		data := make([]byte, window)
		if _, err := f.ReadAt(data, start); err != nil {
			return Entry{}, nil, fmt.Errorf("checkpoint: read tail of %s: %w", path, err)
		}
		// The window's first line may be a truncated record; skipping to the first
		// line boundary is what makes the byte offset a legal place to begin.
		firstBoundary := 0
		if start > 0 {
			nl := bytes.IndexByte(data, '\n')
			if nl < 0 {
				nl = len(data) - 1
			}
			firstBoundary = nl + 1
		}
		for off := firstBoundary; off < len(data); {
			lineEnd := len(data)
			if nl := bytes.IndexByte(data[off:], '\n'); nl >= 0 {
				lineEnd = off + nl
			}
			line := strings.TrimRight(string(data[off:lineEnd]), "\r")
			if rawlogfmt.CanDecodeHeader(line, rawlogfmt.FormatFramed) {
				tried++
				if e, ok := lastRequestFromFramedStream(data[off:], start+int64(off)); ok {
					var req deepseek.ClaudeRequest
					if err := json.Unmarshal(e.Payload, &req); err != nil {
						// The record decoded but its payload is not a request body. Keep
						// looking: a candidate further into the window may be a real one.
						off = lineEnd + 1
						continue
					}
					return e, &req, nil
				}
			}
			off = lineEnd + 1
		}
		if window >= fi.Size() || window >= maxTailWindow {
			return Entry{}, nil, fmt.Errorf("checkpoint: %s: no request record decodes in the last %d bytes (tried %d candidate record starts; the log is %d bytes)", path, window, tried, fi.Size())
		}
		window *= 2
		if window > maxTailWindow {
			window = maxTailWindow
		}
		if window > fi.Size() {
			window = fi.Size()
		}
	}
}

// lastRequestFromFramedStream decodes the framed records in data (whose first byte
// is at absolute offset base) and returns the LAST request record, or ok=false when
// the stream does not decode cleanly to the end.
//
// "Cleanly to the end" is the criterion, and it is what rejects a candidate that
// starts inside a payload: such a start consumes the following bytes as a bogus
// header and the decode fails (or the framing's lengths do not line up), so the
// record it produced is not trusted. A candidate that decodes the whole remaining
// file IS a record boundary, because the framing is self-checking.
func lastRequestFromFramedStream(data []byte, base int64) (Entry, bool) {
	r := rawlogfmt.NewReaderFrom(bytes.NewReader(data), rawlogfmt.FormatFramed)
	var last *rawlogfmt.Record
	for {
		rec, err := r.Next()
		if err != nil {
			if errors.Is(err, io.EOF) {
				if last == nil {
					return Entry{}, false
				}
				return Entry{
					Dir:     DirectionRequest,
					Payload: trimLogFuncNewline(last.Payload),
					Offset:  base + last.Offset,
				}, true
			}
			return Entry{}, false
		}
		tag := rec.Tag
		if tag == rawlogfmt.TagRequest || tag == rawlogfmt.TagProbeRequest {
			c := rec
			last = &c
		}
	}
}

// lastRequestInWindow finds the last → REQUEST marker inside data (whose
// first byte is at absolute offset base) and extracts its payload span.
// payloadTruncated reports that the span runs to the window's end without a
// closing marker — the caller must re-read to EOF. Window-relative offsets
// are converted to absolute file offsets via base.
func lastRequestInWindow(data []byte, base int64) (Entry, bool) {
	// bytes.LastIndex is memmem-fast; payloads can never contain the marker
	// bytes (JSON payloads have no literal newlines; SSE lines start with
	// "event:"/"data:"), so the first hit is the real marker. The boundary
	// check stays as paranoia for hand-crafted logs.
	j := bytes.LastIndex(data, []byte(dirRequest))
	for j >= 0 && j > 0 && data[j-1] != '\n' {
		j = bytes.LastIndex(data[:j], []byte(dirRequest))
	}
	if j < 0 {
		return Entry{}, false
	}
	payloadStart := j + len(dirRequest)
	// The next marker (any direction) closes the payload.
	next, nextOff := findMarker(data, payloadStart)
	e := Entry{
		Dir:    DirectionRequest,
		Offset: base + int64(j),
	}
	if next == "" {
		e.Payload = data[payloadStart:]
		e.payloadTruncated = true
	} else {
		e.Payload = trimLogFuncNewline(data[payloadStart:nextOff])
	}
	return e, true
}

// trimLogFuncNewline removes the single trailing newline RawLogger.LogFunc
// appends after each write, restoring the payload's exact bytes.
func trimLogFuncNewline(payload []byte) []byte {
	if n := len(payload); n > 0 && payload[n-1] == '\n' {
		return payload[:n-1]
	}
	return payload
}
