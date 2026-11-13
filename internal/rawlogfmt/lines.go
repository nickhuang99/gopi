// lines.go — a LINE view over either framing, so a consumer written against
// "a raw log is a file of lines" keeps working unchanged while the file it reads
// may be length-prefixed.
//
// It is a thin walk over Reader + FlattenRecord: the DECODING stays in one place
// (rawlogfmt), and this type only answers the question a line reader asks —
// "next line, please" — for a file whose records are byte spans.
package rawlogfmt

import (
	"errors"
	"io"
)

// LineScanner iterates a raw log's lines in file order, in EITHER framing. It is
// the drop-in the line-oriented readers use (the round parser, the verbatim
// line-span view): the bytes handed out are what the file says, no line is
// invented and no line is dropped, and no consumer learns which framing it read.
//
// A line is what the file's own text says a line is: a framed record's payload is
// split on newlines with the SAME one-trailing-newline rule the legacy writer
// applied (SplitFramingNewline), and the comment/header (`#`) lines the decoder
// steps over are emitted in order (Reader.TakeSkippedComments) — they carry no
// direction, but they are in the file, and a reader that renumbers around them
// breaks `gopi session raw --lines A-B`, which promises the file's own numbering.
type LineScanner struct {
	r   *Reader
	buf []string
	// recs is buf's parallel record slice: recs[i] is the record line i belongs
	// to, and the ZERO Record for a line that belongs to none (a comment/header
	// line). One slice rather than one field, because a buffer can hold a
	// comment line AND its record's lines at the same time, and a single field
	// made the comment line report the record's tag (measured: the first line of
	// a framed log claimed tag "R").
	recs []Record
	// idxInRec[i] is line i's 1-based position inside its record (0 for a
	// comment/header line, which belongs to none). A reader needs it to answer "is
	// this line the record's own header?" without comparing text — the framed
	// header (`R 13`) and the legacy marker (`→ REQUEST`) are different strings for
	// the same event, and a text comparison would make a consumer branch on the
	// format, which is exactly what this package exists to prevent.
	idxInRec []int
	idx      int
	done     bool
	err      error
}

// OpenLineScanner opens path for line iteration in a KNOWN framing (the caller
// asks Detect once and passes the answer, so the head is read once).
func OpenLineScanner(path string, format Format) (*LineScanner, error) {
	r, err := OpenReader(path, format)
	if err != nil {
		return nil, err
	}
	return &LineScanner{r: r}, nil
}

// NewLineScannerFrom wraps an already-open reader with a known framing. The
// caller owns the underlying reader's lifetime.
func NewLineScannerFrom(r io.Reader, format Format) *LineScanner {
	return &LineScanner{r: NewReaderFrom(r, format)}
}

// Format is the framing this scanner decodes.
func (s *LineScanner) Format() Format { return s.r.Format() }

// Scan advances to the next line and reports whether there is one.
func (s *LineScanner) Scan() bool {
	for s.idx >= len(s.buf) {
		if s.done || s.err != nil {
			return false
		}
		rec, err := s.r.Next()
		// The comment/header lines the decoder stepped over PRECEDE this record,
		// and they belong to NO record (zero Record).
		comments := s.r.TakeSkippedComments()
		s.buf = append([]string(nil), comments...)
		s.recs = make([]Record, len(comments))
		s.idxInRec = make([]int, len(comments))
		s.idx = 0
		if err != nil {
			s.done = true
			if !errors.Is(err, io.EOF) {
				s.err = err
			}
			// A file that ends with comment lines still has them to hand out.
			if len(s.buf) == 0 {
				return false
			}
			continue
		}
		for i, line := range FlattenRecord(rec, s.r.Format()) {
			s.buf = append(s.buf, line)
			s.recs = append(s.recs, rec)
			s.idxInRec = append(s.idxInRec, i+1)
		}
		if len(s.buf) == 0 {
			continue
		}
	}
	s.idx++
	return true
}

// Text is the current line, WITHOUT its terminator — the shape
// bufio.Scanner.Text() has, which is what the readers this type replaces were
// written against.
func (s *LineScanner) Text() string {
	if s.idx == 0 || s.idx > len(s.buf) {
		return ""
	}
	line := s.buf[s.idx-1]
	if n := len(line); n > 0 && line[n-1] == RecordTerminator {
		return line[:n-1]
	}
	return line
}

// LineInRecord is the current line's 1-based position INSIDE its record, or 0 when
// the line belongs to no record (a comment/header line). Position 1 is the
// record's header/marker line.
func (s *LineScanner) LineInRecord() int {
	if s.idx == 0 || s.idx > len(s.idxInRec) {
		return 0
	}
	return s.idxInRec[s.idx-1]
}

// IsRecordHeader reports whether the current line is its record's own header —
// the length-prefixed header in a framed log, the direction marker in a legacy
// one. It is the format-INDEPENDENT spelling of "a marker line arrives here", so a
// parser dispatches on it instead of on the line's text.
func (s *LineScanner) IsRecordHeader() bool { return s.LineInRecord() == 1 }

// Record is the record the current line came from. Its Tag is the
// Format-INDEPENDENT view of direction, and Offset/Line locate it, so a reader
// that must not branch on the format reads this instead of the line text. It is
// the zero Record for a comment/header line (those belong to no record).
func (s *LineScanner) Record() Record {
	if s.idx == 0 || s.idx > len(s.recs) {
		return Record{}
	}
	return s.recs[s.idx-1]
}

// Err reports the first decode error (a malformed framed record, a truncated
// payload), or nil. It is never io.EOF: end-of-file is Scan returning false.
func (s *LineScanner) Err() error { return s.err }
