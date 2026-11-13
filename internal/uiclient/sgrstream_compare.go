package uiclient

// sgrstream_compare.go — TWO SCREEN STREAMS, COMPARED FRAME BY FRAME.
//
// WHY THIS FILE EXISTS. `gopi session log` and `gopi session replay` answer
// questions about a session; neither can answer the ONE question a render
// regression turns on: "are the bytes the renderer wrote identical?". Two
// recordings of the same paint sequence are the only pair that can, and comparing
// them by eye is not a measurement. This walk produces the measurement: the
// FIRST frame that differs, the byte offset in each file, and both sides.
//
// THE OFFSET IS THE PAYLOAD'S, not the frame's prefix line: a reader who wants to
// look at it with `dd bs=1 skip=N` reads payload bytes, and an offset that
// pointed at the decimal length would be off by the digits.
//
// Frames are compared by PAYLOAD BYTES, never by an interpretation of them — that
// is what makes the answer a fact about the files rather than about this build's
// terminal model. A length difference IS a byte difference: it is reported as the
// first frame that differs.

import (
	"fmt"
	"io"
	"os"
	"strconv"
)

// SgrStreamFrameAt is one recorded frame together with where it lives in its
// file. Payload is the RAW bytes the render stream wrote; Offset is the first
// byte of Payload.
type SgrStreamFrameAt struct {
	Index   int
	Offset  int64
	Payload []byte
}

// SgrFrameDivergence names the first difference between two streams.
//
// A and B are the two PAYLOADS at that frame. One of them may be nil: a stream
// that ends before the other has no payload at that frame, and the sentence must
// say which side ran out rather than print an empty byte range as a difference.
type SgrFrameDivergence struct {
	// Frame is the 1-based frame number both files agree up to (the first one
	// that differs).
	Frame int
	// OffsetA / OffsetB are the payload offsets in each file, -1 when that file
	// has no frame there.
	OffsetA, OffsetB int64
	// A / B are the payloads, nil when the file has no frame there.
	A, B []byte
	// Reason is the one-clause diagnosis ("payload bytes differ", "stream A ends
	// before B at this frame", …).
	Reason string
}

// SgrStreamComparison is the whole reading of two streams.
type SgrStreamComparison struct {
	// FramesA / FramesB are how many frames each file holds (counted, never the
	// trailer's declaration — a declaration is compared, not believed).
	FramesA, FramesB int
	// BytesA / BytesB are the counted payload totals.
	BytesA, BytesB int64
	// First is nil when the two streams are identical: same frame count, same
	// payload bytes, frame for frame.
	First *SgrFrameDivergence
}

// Identical is the ONE predicate a caller reports `first divergence: none` from,
// so the sentence and the condition cannot drift apart.
func (c SgrStreamComparison) Identical() bool { return c.First == nil }

// ReadSgrFramesAt parses a stream into frames THAT CARRY THEIR OFFSETS. It is the
// read side `--verify` needs, and it reuses this package's record reader and
// header decoder (readSgrRecordAt / readSgrHeader) rather than a second grammar:
// a comparison that accepted a file the replay refuses would judge a stream
// nobody can read.
func ReadSgrFramesAt(ra io.ReaderAt, size int64) (SgrStreamMeta, []SgrStreamFrameAt, error) {
	var meta SgrStreamMeta
	var frames []SgrStreamFrameAt
	if size <= 0 {
		return meta, nil, fmt.Errorf("uiclient: the screen stream is empty (no header to read)")
	}
	head, pos, err := readSgrRecordAt(ra, 0, size)
	if err != nil {
		return meta, nil, fmt.Errorf("uiclient: screen stream has no readable header: %w", err)
	}
	if meta, err = readSgrHeader(head); err != nil {
		return meta, nil, err
	}
	for pos < size {
		line, after, lerr := readSgrRecordAt(ra, pos, size)
		if lerr != nil {
			// The file ends inside a record line. Every frame before it is
			// intact, and the walk reports what it counted (the same tolerance
			// ReadSgrFrames keeps, for the same reason: a live run is read
			// mid-write every time).
			break
		}
		if IsSgrStreamTrailerLine(line) {
			if terr := readSgrTrailer(line, &meta); terr != nil {
				return meta, frames, terr
			}
			break
		}
		n, cerr := sgrStreamFrameLength(line)
		if cerr != nil {
			return meta, frames, &SgrStreamContradictionError{
				Size:     size,
				Findings: []SgrStreamContradiction{sgrBadRecordFinding(pos, line)},
			}
		}
		if after+int64(n) > size {
			// An announced frame the writer never finished: the recording ends
			// there, and it is not a frame to compare.
			break
		}
		payload := make([]byte, n)
		if _, rerr := ra.ReadAt(payload, after); rerr != nil {
			return meta, frames, fmt.Errorf("uiclient: cannot read frame %d's payload at offset %d: %w", len(frames)+1, after, rerr)
		}
		frames = append(frames, SgrStreamFrameAt{Index: len(frames) + 1, Offset: after, Payload: payload})
		pos = after + int64(n)
	}
	return meta, frames, nil
}

// CompareSgrStreams reads two files and reports the first divergence.
//
// An unreadable file is an ERROR naming the path (Rule 15): "no divergence" must
// never be the answer for a file that could not be read, or the one measurement
// this mechanism exists for would fail open.
func CompareSgrStreams(pathA, pathB string) (SgrStreamComparison, error) {
	var cmp SgrStreamComparison
	_, a, err := readSgrFramesAtPath(pathA)
	if err != nil {
		return cmp, err
	}
	_, b, err := readSgrFramesAtPath(pathB)
	if err != nil {
		return cmp, err
	}
	return CompareSgrFrameSets(a, b), nil
}

// CompareSgrFrameSets is the comparison itself, over already-read frames. It is
// separate from the file reading so a test can drive the byte-level verdict with
// two synthetic recordings, and so a caller holding frames for another reason
// (`--verify <sid>` resolving both sides) does not have to re-read them.
func CompareSgrFrameSets(a, b []SgrStreamFrameAt) SgrStreamComparison {
	cmp := SgrStreamComparison{FramesA: len(a), FramesB: len(b)}
	for _, f := range a {
		cmp.BytesA += int64(len(f.Payload))
	}
	for _, f := range b {
		cmp.BytesB += int64(len(f.Payload))
	}
	n := len(a)
	if len(b) < n {
		n = len(b)
	}
	for i := 0; i < n; i++ {
		if string(a[i].Payload) == string(b[i].Payload) {
			continue
		}
		cmp.First = &SgrFrameDivergence{
			Frame:   i + 1,
			OffsetA: a[i].Offset,
			OffsetB: b[i].Offset,
			A:       a[i].Payload,
			B:       b[i].Payload,
			Reason:  sgrPayloadDivergenceReason(a[i], b[i]),
		}
		return cmp
	}
	// One stream stops first. That IS the divergence: a frame that exists on one
	// side only is the largest byte difference a pair of recordings can have, and
	// reporting "identical" for it would be the failure this walk exists to
	// prevent.
	switch {
	case len(a) > n:
		cmp.First = &SgrFrameDivergence{
			Frame: n + 1, OffsetA: a[n].Offset, OffsetB: sgrFrameNoOffset,
			A: a[n].Payload, Reason: fmt.Sprintf("stream A continues for %d more frame(s); B ends at frame %d", len(a)-n, n),
		}
	case len(b) > n:
		cmp.First = &SgrFrameDivergence{
			Frame: n + 1, OffsetA: sgrFrameNoOffset, OffsetB: b[n].Offset,
			B: b[n].Payload, Reason: fmt.Sprintf("stream B continues for %d more frame(s); A ends at frame %d", len(b)-n, n),
		}
	}
	return cmp
}

// sgrFrameNoOffset is the sentinel for "this stream has no frame at that
// position". It is a named zero shape, never a bare -1 a reader could mistake for
// a byte position in the file.
const sgrFrameNoOffset = int64(-1)

// sgrPayloadDivergenceReason says HOW two payloads differ, in one clause. The
// length is stated because "frame 913 differs" alone sends a reader looking for a
// character change when the two writes were not even the same size.
func sgrPayloadDivergenceReason(a, b SgrStreamFrameAt) string {
	if len(a.Payload) != len(b.Payload) {
		return fmt.Sprintf("payload lengths differ (%s=%s byte(s), %s=%s byte(s))",
			sgrSideA, strconv.Itoa(len(a.Payload)), sgrSideB, strconv.Itoa(len(b.Payload)))
	}
	return fmt.Sprintf("payload bytes differ (%d byte(s) each; first differing byte at %d)",
		len(a.Payload), sgrFirstDifferingByte(a.Payload, b.Payload))
}

// The two sides' names in every sentence this file emits, so the report and the
// tests agree on the vocabulary of "which stream".
const (
	sgrSideA = "A"
	sgrSideB = "B"
)

// sgrFirstDifferingByte is the offset INSIDE a payload where the two first
// disagree. Payloads of different lengths are compared up to the shorter one (the
// length difference is already stated separately).
func sgrFirstDifferingByte(a, b []byte) int {
	n := len(a)
	if len(b) < n {
		n = len(b)
	}
	for i := 0; i < n; i++ {
		if a[i] != b[i] {
			return i
		}
	}
	return n
}

// readSgrFramesAtPath opens a stream and reads it with offsets.
func readSgrFramesAtPath(path string) (SgrStreamMeta, []SgrStreamFrameAt, error) {
	f, err := os.Open(path)
	if err != nil {
		return SgrStreamMeta{}, nil, fmt.Errorf("uiclient: cannot open the screen stream %s: %w", path, err)
	}
	defer f.Close() //stylegate:ignore a read-only handle; the close cannot change the reading
	st, err := f.Stat()
	if err != nil {
		return SgrStreamMeta{}, nil, fmt.Errorf("uiclient: cannot measure the screen stream %s: %w", path, err)
	}
	meta, frames, rerr := ReadSgrFramesAt(f, st.Size())
	if rerr != nil {
		return meta, frames, fmt.Errorf("uiclient: %s: %w", path, rerr)
	}
	return meta, frames, nil
}

// ReadSgrFramesAtPath is the exported open+walk, so `--verify` and a test drive
// the same sequence.
func ReadSgrFramesAtPath(path string) (SgrStreamMeta, []SgrStreamFrameAt, error) {
	return readSgrFramesAtPath(path)
}
