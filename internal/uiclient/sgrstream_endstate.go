package uiclient

// sgrstream_endstate.go — HOW A STREAM ENDED, as ONE reading with THREE answers.
//
// THE MISDIAGNOSIS THIS FILE EXISTS TO END (measured 2026-11-08, bug
// 01a1b823-5070). The replay verb decided everything from one boolean —
// `meta.Trailer == false` — and printed `the stream ends mid-frame`. That sentence
// names a TRUNCATION, and it was wrong about every file a census of this host
// found: 14 streams had no trailer, and a record-boundary walk of all 14 measured
// `remaining=0` — the last frame ended exactly on EOF. The recordings were WHOLE;
// what was missing was the closing record. So the verb told an operator their bytes
// were cut when the bytes were fine and the writer had not closed (Rule 15: two
// different failures, one sentence).
//
// THE THREE ANSWERS, and they are decided by what the FILE contains, never by what
// a caller hoped:
//
//  1. TRAILER — the file ends at a `#gopi-sgr-stream-end` record, and that record's
//     own counts agree with the frames before it. Nothing to report.
//  2. UNCLOSED — every record ends on a boundary, and no trailer was ever written.
//     The run did not reach close-out. The bytes are whole.
//  3. CONTRADICTION — the file's own records disagree with its own length
//     (sgrstream_integrity.go's findings: records-after-trailer,
//     unparseable-record, trailer-count-mismatch). This is a fact about the FILE.
//
// A FOURTH SHAPE IS NOT A FOURTH STATE: a last frame whose declared length runs
// past EOF is a cut frame, and it is still the UNCLOSED state — the recording run
// did not close — but its MEASURED CLAUSE says so instead of claiming a whole
// boundary. The state answers "did this file reach close-out"; the clause answers
// "are its bytes whole", and they are separate questions with separate evidence.

import (
	"fmt"
	"io"
	"os"
	"strconv"
)

// The end-state vocabulary. Spelled once (Rule 1b/13): the walk below, the replay
// verb's sentences and their tests all compare by these constants, so a reader
// cannot recognize a state the writer stopped producing.
const (
	// SgrStreamEndsWithTrailer is the closed, self-consistent stream.
	SgrStreamEndsWithTrailer = "trailer"
	// SgrStreamEndsUnclosed is a stream whose record boundaries are intact up to
	// its last record, and which carries no close-out record.
	SgrStreamEndsUnclosed = "unclosed"
	// SgrStreamEndsContradiction is a stream whose own records contradict its own
	// length. It is never a reading.
	SgrStreamEndsContradiction = "contradiction"
	// sgrEndNoOffset is the sentinel for "this offset does not exist in this
	// stream" — used for a trailer that was never written and a cut frame that
	// never happened. It is a named zero rather than a bare -1 so a reader of a
	// field cannot mistake it for a byte position in the file.
	sgrEndNoOffset = int64(-1)
)

// SgrStreamEnd is one stream's ending, measured by walking its records. Every
// field is a reading: the counts are what the walk counted, and the offsets are
// where the walk stood, never what a record declared.
type SgrStreamEnd struct {
	// State is one of the three SgrStreamEnds* constants.
	State string
	// Frames / Bytes are what the walk COUNTED before it stopped — not the
	// trailer's declaration (a declaration is compared against these, never
	// substituted for them).
	Frames int
	Bytes  int64
	// TrailerOffset is where the close-out record begins, or sgrEndNoOffset when
	// the stream has none.
	TrailerOffset int64
	// CutFrameAt is where an announced-but-unfinished frame begins, or
	// sgrEndNoOffset when every frame ran to its full length.
	CutFrameAt int64
	// Findings is every contradiction the walk met, in file order. It is empty
	// for the other two states by construction.
	Findings []SgrStreamContradiction
}

// BoundaryWhole reports whether the stream's last frame ran to its full declared
// length. It is the ONE predicate the "is this a truncation?" question reads, so
// no caller can answer it a second way (Rule 14).
func (e SgrStreamEnd) BoundaryWhole() bool { return e.CutFrameAt == sgrEndNoOffset }

// Closed reports whether the recording run reached its close-out record.
func (e SgrStreamEnd) Closed() bool { return e.State == SgrStreamEndsWithTrailer }

// Contradicts reports whether the file's own records disagree with its own length.
func (e SgrStreamEnd) Contradicts() bool { return e.State == SgrStreamEndsContradiction }

// ContradictionError returns the refusal shape the locator/copier already produce
// (*SgrStreamContradictionError), so a caller that has an SgrStreamEnd can hand the
// SAME type to a caller expecting a read failure. Nil for a clean or merely
// unclosed stream: being unclosed is a state of the world, not an error.
func (e SgrStreamEnd) ContradictionError(path string, size int64) error {
	if !e.Contradicts() {
		return nil
	}
	return &SgrStreamContradictionError{Path: path, Size: size, Findings: e.Findings}
}

// ScanSgrStreamEnd walks a stream's records and reports how it ended. It reads
// record boundaries only — it never copies a payload — so its cost is the number of
// frames, not the size of the file.
//
// TOLERANCE, and it is deliberate: a stream cut mid-frame and a stream with no
// trailer both return a value and NO error, because both are ordinary states of a
// recording being appended to while it is read. Only a genuine read failure (an
// unreadable header, an unknown version) is an error, and a self-contradictory
// stream is not an error either — it is a STATE whose findings the caller must
// report (a caller that cannot tell "unclosed" from "contradictory" is the defect
// this file exists to end, so returning both as `error` would erase the
// distinction the moment a caller handled one).
func ScanSgrStreamEnd(ra io.ReaderAt, size int64) (SgrStreamEnd, error) {
	end := SgrStreamEnd{TrailerOffset: sgrEndNoOffset, CutFrameAt: sgrEndNoOffset}
	if size <= 0 {
		return end, fmt.Errorf("uiclient: the SGR stream is empty (no header to read)")
	}
	head, pos, err := readSgrRecordAt(ra, 0, size)
	if err != nil {
		return end, fmt.Errorf("uiclient: SGR stream has no readable header: %w", err)
	}
	var meta SgrStreamMeta
	if meta, err = readSgrHeader(head); err != nil {
		return end, fmt.Errorf("uiclient: %w", err)
	}
	var payload int64
	for pos < size {
		line, after, lerr := readSgrRecordAt(ra, pos, size)
		if lerr != nil {
			// A line that runs to EOF with no newline is not a record boundary, and
			// calling it one would be the very flattening this file ends. The only
			// way readSgrRecordAt fails on an intact tail is a cut record; the
			// stream is unclosed and its boundary is NOT whole.
			end.CutFrameAt = pos
			end.State = SgrStreamEndsUnclosed
			return end, nil
		}
		if sgrStreamIsTrailerRecord(line) {
			if terr := readSgrTrailer(line, &meta); terr != nil {
				return end, fmt.Errorf("uiclient: %w", terr)
			}
			end.TrailerOffset = pos
			end.State = SgrStreamEndsWithTrailer
			// THE COUNTED TOTALS ARE REPORTED IN EVERY STATE, not only when the walk
			// falls off the end: a caller comparing a trailer's declaration against
			// what the file holds reads this field, and leaving it at zero for the
			// closed case would make "0 bytes" mean two different things.
			end.Bytes = payload
			var findings []SgrStreamContradiction
			surplus, serr := sgrAfterTrailerFinding(ra, after, size, meta)
			if serr != nil {
				return end, serr
			}
			if surplus != nil {
				findings = append(findings, *surplus)
			}
			if counted := sgrCountMismatchFinding(pos, meta, end.Frames, payload); counted != nil {
				findings = append(findings, *counted)
			}
			if len(findings) > 0 {
				end.State = SgrStreamEndsContradiction
				end.Findings = findings
			}
			return end, nil
		}
		n, cerr := sgrStreamFrameLength(line)
		if cerr != nil {
			// A COMPLETE record that is neither the trailer nor a decimal length is
			// damage — the sparse hole a second writer leaves, or bytes from a
			// different file. It is named as such (sgrstream_integrity.go), never
			// reported as a complaint about the length grammar.
			bad := sgrBadRecordFinding(pos, line)
			end.State = SgrStreamEndsContradiction
			end.Findings = []SgrStreamContradiction{bad}
			return end, nil
		}
		if after+int64(n) > size {
			// The writer announced a frame it has not finished: everything before it
			// is intact, and the recording is a snapshot of what had been painted.
			// UNCLOSED with a cut clause — never a contradiction (a live run reaches
			// this state every time it is read mid-write).
			end.CutFrameAt = pos
			end.Bytes = payload
			end.State = SgrStreamEndsUnclosed
			return end, nil
		}
		end.Frames++
		payload += int64(n)
		pos = after + int64(n)
	}
	end.Bytes = payload
	end.State = SgrStreamEndsUnclosed
	return end, nil
}

// sgrStreamIsTrailerRecord reports whether a record line is the close-out record.
// It requires the separating space for the reason the reader above does: a frame
// payload can begin with any byte, and `#gopi-sgr-stream-end` is a PREFIX of
// nothing else in this grammar — but `#gopi-sgr-stream` alone is a prefix of it, so
// a header-looking line cannot be mistaken for a trailer.
func sgrStreamIsTrailerRecord(line string) bool {
	return IsSgrStreamTrailerLine(line)
}

// sgrStreamFrameLength decodes a record as a frame length, in the same bounds the
// full reader enforces. It is ONE predicate for the whole package (Rule 14): a
// second spelling of "how long is this frame" is how a walk and a parse come to
// disagree about one file.
func sgrStreamFrameLength(line string) (int, error) {
	n, err := strconv.Atoi(line)
	if err != nil {
		return 0, fmt.Errorf("not a decimal frame length: %w", err)
	}
	if n < 0 || n > sgrStreamLenPrefixMax {
		return 0, fmt.Errorf("frame length %d is outside 0..%d", n, sgrStreamLenPrefixMax)
	}
	return n, nil
}

// ScanSgrStreamEndPath is the same reading over a path, so a caller that has only a
// file name does not have to open-code the open/stat/close sequence a third time.
func ScanSgrStreamEndPath(path string) (SgrStreamEnd, int64, error) {
	f, err := os.Open(path)
	if err != nil {
		return SgrStreamEnd{}, 0, err
	}
	defer f.Close() //stylegate:ignore a read-only handle; the close cannot change the reading
	st, err := f.Stat()
	if err != nil {
		return SgrStreamEnd{}, 0, fmt.Errorf("uiclient: cannot measure the SGR stream %s: %w", path, err)
	}
	end, serr := ScanSgrStreamEnd(f, st.Size())
	return end, st.Size(), serr
}
