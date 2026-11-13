package uiclient

// sgrstream_range.go — the READ side's RANGE half: pick a bounded TAIL of a
// stream's frames, and compose a file that is a valid stream of exactly those
// frames.
//
// WHY THIS IS NOT ReadSgrFrames WITH A LIMIT. ReadSgrFrames loads the whole
// stream: correct for a replay, wrong for `gopi chat`'s `/screen`, where the
// artifact has to be bounded by a NUMBER (the model is told the size, and a size
// nobody bounded is a file the reader may not be able to read). Taking a bounded
// tail also needs each frame's POSITION before the payload is copied, so the walk
// below is a two-pass reader over an io.ReaderAt: pass one records every frame's
// (length, offset) and reads no payload at all; pass two copies ONLY the selected
// frames. Memory is therefore bounded by the caller's budget, not by the stream.
//
// THE OUTPUT FILE IS A VALID STREAM, NOT A RAW SLICE. A copied tail of a
// length-prefixed file is not itself a length-prefixed file: it starts inside a
// payload and has no header. So the compositor writes the SAME three record kinds
// the writer writes (header line, `len`+payload frames, trailer line) with the
// INCLUDED counts — which means every existing reader (`ReadSgrFrames`, a replay,
// a stranger's tool) reads a range artifact without knowing what a range is. The
// truncation FACT is not encoded here; it is stated to the operator and to the
// model by the caller (Rule 15 lives at the surface, and a hidden field inside a
// file is not a statement).

import (
	"errors"
	"fmt"
	"io"
	"os"
	"strconv"
)

const (
	// sgrRangeLineMax bounds one RECORD line's length while walking a file
	// (a frame's decimal length prefix, a header or a trailer). A record longer
	// than this is not one this format produces, so the walk refuses it instead
	// of growing a buffer for a file that is not the shape it claims — the same
	// refusal rule sgrStreamLenPrefixMax applies to a prefix NUMBER.
	sgrRangeLineMax = 512
	// sgrRangeReadChunk is the buffer size a record line is assembled from.
	sgrRangeReadChunk = 128
)

// SgrFrameMark is one frame's position inside a stream file.
type SgrFrameMark struct {
	// Bytes is the frame's payload length, as its own prefix states it.
	Bytes int
	// Offset is where that payload starts in the file.
	Offset int64
}

// SgrRange is a decoded span of a stream: what a caller needs in order to say
// what it took, and to be honest about what it left out.
type SgrRange struct {
	// Meta is the source stream's header as the shared decoder read it.
	Meta SgrStreamMeta
	// StreamBytes is the WHOLE source stream's payload total, so a caller can
	// state the truncation as a quantity ("kept 64 KiB of 3 MiB") rather than as
	// an adjective.
	StreamBytes int64
	// StreamFrames is the whole source stream's frame count.
	StreamFrames int
	// StartFrame is the 0-based index of the first INCLUDED frame.
	StartFrame int
	// Frames is how many frames are included.
	Frames int
	// Bytes is the payload total of the included frames.
	Bytes int64
	// Truncated reports whether frames were left out (StartFrame > 0).
	Truncated bool
	// SourceTrailer reports whether the source stream reached its trailer. A
	// source that did not is a run still writing (or one that died), and a caller
	// must be able to say which it is holding.
	SourceTrailer bool
}

// ReadSgrRangeFile is ReadSgrRange over a path — the shape every caller has,
// because the artifact this package writes is a file.
func ReadSgrRangeFile(path string, maxBytes int64, maxFrames int) (SgrRange, [][]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return SgrRange{}, nil, fmt.Errorf("uiclient: cannot open the SGR stream %s: %w", path, err)
	}
	defer f.Close() //stylegate:ignore a read-only open; the close cannot change the parse result
	st, err := f.Stat()
	if err != nil {
		return SgrRange{}, nil, fmt.Errorf("uiclient: cannot measure the SGR stream %s: %w", path, err)
	}
	rng, frames, err := ReadSgrRange(f, st.Size(), maxBytes, maxFrames)
	// THE REFUSAL NAMES THE FILE IT READ. ReadSgrRange holds an io.ReaderAt and
	// therefore cannot know a path; a caller that DOES know it (every production
	// caller, because the artifact is a file) owes its reader the name, or the
	// contradiction it reports is not actionable (Rule 15).
	var contradiction *SgrStreamContradictionError
	if errors.As(err, &contradiction) {
		contradiction.Path = path
	}
	return rng, frames, err
}

// ReadSgrRange walks a stream and returns its bounded TAIL: the most recent
// frames whose payload total stays within maxBytes and whose count stays within
// maxFrames.
//
// BOTH BOUNDS ARE REQUIRED AND MUST BE POSITIVE. A caller that wants "everything"
// passes limits derived from the stream it just measured; a zero or negative
// bound is refused rather than read as "no limit", because a silent unbounded
// read is exactly the defect the two numbers exist to prevent (Rule 15).
//
// TOLERANCE IS THE SAME AS ReadSgrFrames': a stream cut mid-frame yields the
// frames it completed and no error (SourceTrailer=false), while a missing header
// or an unknown version is an error — truncation is a state of the world, a
// foreign format is a different file.
func ReadSgrRange(ra io.ReaderAt, size, maxBytes int64, maxFrames int) (SgrRange, [][]byte, error) {
	var rng SgrRange
	if maxBytes <= 0 || maxFrames <= 0 {
		return rng, nil, fmt.Errorf("uiclient: the SGR range needs positive bounds (got maxBytes=%d maxFrames=%d)", maxBytes, maxFrames)
	}
	if size <= 0 {
		return rng, nil, fmt.Errorf("uiclient: the SGR stream is empty (no header to read)")
	}
	pos := int64(0)
	head, next, err := readSgrRecordAt(ra, pos, size)
	if err != nil {
		return rng, nil, fmt.Errorf("uiclient: SGR stream has no readable header: %w", err)
	}
	meta, err := readSgrHeader(head)
	if err != nil {
		return rng, nil, fmt.Errorf("uiclient: %w", err)
	}
	rng.Meta = meta
	pos = next
	var marks []SgrFrameMark
	var payload int64
	for pos < size {
		line, after, lerr := readSgrRecordAt(ra, pos, size)
		if lerr != nil {
			// A cut in the middle of a RECORD line: the file ends there, and the
			// frames before it are intact.
			break
		}
		if IsSgrStreamTrailerLine(line) {
			if terr := readSgrTrailer(line, &rng.Meta); terr != nil {
				return rng, nil, fmt.Errorf("uiclient: %w", terr)
			}
			rng.SourceTrailer = true
			pos = after
			// THE SAME TWO CONTRADICTIONS THE TURN WALK REFUSES (Rule 14: ONE
			// reader's rule, two entry points). A trailer with data behind it and
			// a complete record that is not a length are both facts about the
			// FILE; a range taken out of such a stream would be a slice of a file
			// nobody can describe, and reporting it as a plain parse result is how
			// the defect reached an operator as "the renderer changed".
			finding, ierr := sgrAfterTrailerFinding(ra, after, size, rng.Meta)
			if ierr != nil {
				return rng, nil, ierr
			}
			var findings []SgrStreamContradiction
			if finding != nil {
				findings = append(findings, *finding)
			}
			if counted := sgrCountMismatchFinding(pos, rng.Meta, len(marks), payload); counted != nil {
				findings = append(findings, *counted)
			}
			if len(findings) > 0 {
				return rng, nil, &SgrStreamContradictionError{Size: size, Findings: findings}
			}
			break
		}
		n, cerr := strconv.Atoi(line)
		if cerr != nil || n < 0 || n > sgrStreamLenPrefixMax {
			bad := sgrBadRecordFinding(pos, line)
			return rng, nil, &SgrStreamContradictionError{Size: size, Findings: []SgrStreamContradiction{bad}}
		}
		if after+int64(n) > size {
			// A frame the writer announced and did not finish: the stream is
			// truncated there. Everything before it is intact and usable.
			pos = size
			break
		}
		marks = append(marks, SgrFrameMark{Bytes: n, Offset: after})
		payload += int64(n)
		pos = after + int64(n)
	}
	rng.StreamFrames = len(marks)
	rng.StreamBytes = payload
	// SELECT THE TAIL: walk backwards until either bound would be crossed. The
	// newest frames are the ones that carry the current screen, which is what a
	// "capture the screen" command is for.
	start := len(marks)
	var keptBytes int64
	for i := len(marks) - 1; i >= 0; i-- {
		add := int64(marks[i].Bytes)
		if keptBytes+add > maxBytes || len(marks)-i > maxFrames {
			break
		}
		keptBytes += add
		start = i
	}
	frames := make([][]byte, 0, len(marks)-start)
	for _, m := range marks[start:] {
		buf := make([]byte, m.Bytes)
		if m.Bytes > 0 {
			if _, rerr := ra.ReadAt(buf, m.Offset); rerr != nil && rerr != io.EOF {
				return rng, nil, fmt.Errorf("uiclient: cannot read a %d-byte frame at offset %d of the SGR stream: %w", m.Bytes, m.Offset, rerr)
			}
		}
		frames = append(frames, buf)
	}
	rng.StartFrame = start
	rng.Frames = len(frames)
	rng.Bytes = keptBytes
	rng.Truncated = start > 0
	return rng, frames, nil
}

// readSgrRecordAt returns one LF-terminated record starting at pos, WITHOUT the
// LF, plus the offset just past it. It reads in small chunks through the
// ReaderAt, so a walk costs one syscall-shaped read per record rather than one
// per byte (lineReader's byte-at-a-time path is for a stream already in memory).
func readSgrRecordAt(ra io.ReaderAt, pos, size int64) (string, int64, error) {
	var out []byte
	buf := make([]byte, sgrRangeReadChunk)
	for {
		if pos >= size {
			if len(out) == 0 {
				return "", pos, io.EOF
			}
			return string(out), pos, io.EOF
		}
		want := int64(len(buf))
		if rest := size - pos; rest < want {
			want = rest
		}
		n, err := ra.ReadAt(buf[:want], pos)
		for i := 0; i < n; i++ {
			if buf[i] == '\n' {
				// out ALREADY holds bytes 0..i-1 of this chunk: each iteration
				// appends its byte before advancing. Appending buf[:i] again here
				// double-counts the prefix — measured, that turned a 111-byte
				// header line into 222 and every later frame prefix into a
				// duplicate ("1616"), which the walk then read as an absurd length
				// and REJECTED, so a whole recording parsed as zero frames.
				return string(out), pos + int64(i) + 1, nil
			}
			out = append(out, buf[i])
			if len(out) > sgrRangeLineMax {
				return "", pos, fmt.Errorf("a record line exceeds %d bytes at offset %d", sgrRangeLineMax, pos)
			}
		}
		pos += int64(n)
		if err != nil && n == 0 {
			if len(out) == 0 {
				return "", pos, io.EOF
			}
			return string(out), pos, io.EOF
		}
	}
}

// WriteSgrPrefix composes a VALID stream file holding exactly the given frames,
// with the source's header identity and the INCLUDED counts in the trailer.
//
// It is the ONE writer of a range artifact: the header and trailer lines come
// from the same two helpers OpenSgrStream/Close use (sgrHeaderLine,
// sgrTrailerLine), so a range artifact and a recording cannot drift in shape. The
// file carries no field of its own that a reader would have to know about: it is
// an ordinary stream, and "this is a tail" is a fact the surface states.
func WriteSgrPrefix(w io.Writer, meta SgrStreamMeta, frames [][]byte) (int64, int, error) {
	var total int64
	for _, f := range frames {
		total += int64(len(f))
	}
	hdr := sgrHeaderLine(meta.Grid, meta.Revision, meta.Started)
	if _, err := io.WriteString(w, hdr); err != nil {
		return 0, 0, fmt.Errorf("uiclient: cannot write a range artifact's header: %w", err)
	}
	for _, f := range frames {
		if _, err := io.WriteString(w, strconv.Itoa(len(f))+"\n"); err != nil {
			return 0, 0, fmt.Errorf("uiclient: cannot write a range artifact's frame prefix: %w", err)
		}
		if _, err := w.Write(f); err != nil {
			return 0, 0, fmt.Errorf("uiclient: cannot write a range artifact's frame: %w", err)
		}
	}
	tr := sgrTrailerLine(len(frames), total, meta.Grid)
	if _, err := io.WriteString(w, tr); err != nil {
		return 0, 0, fmt.Errorf("uiclient: cannot write a range artifact's trailer: %w", err)
	}
	return total, len(frames), nil
}
