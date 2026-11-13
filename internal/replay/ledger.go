// ledger.go — the beat protocol's SHARED vocabulary: the file names, the row
// format, and the writer.
//
// WHY THESE ARE EXPORTED. The protocol has ONE producer — `gopi chat --replay`,
// the REAL chat surface in cmd/agent, which is also its own driver. Four facts
// share this file because one file format is at stake: the ledger's six columns,
// its header, the acknowledgement's name, the completion marker and the
// per-beat dump's name. Spelling any of them a second time in cmd/agent would be
// a second answer to "what does the driver parse", and the first thing to drift
// would be silently unreadable rows (Rule 13 — a constant that exists but is
// bypassed is a defect, and this is the mirror: the value's ONE definition lives
// where the format is owned).
//
// The ROW is built by ONE function (LedgerRow) for both producers, so a column
// added here appears in both files at once.
package replay

import (
	"bufio"
	"encoding/base64"
	"fmt"
	"os"
	"strconv"
	"strings"
)

// The beat protocol's file and directory names, under --scratch.
const (
	// LedgerName is the beat ledger: one row per drawn beat.
	LedgerName = "beats.tsv"
	// AckName is the driver's one-integer reply to a beat.
	AckName = "ack"
	// DoneName is written once, after the last beat, holding the run's beat
	// count. The driver's frame loop ends on this file: a terminal outlives the
	// process inside it, so "the window closed" says nothing about whether the
	// run finished.
	DoneName = "done.txt"
	// DumpDirName is the sub-directory of --scratch holding the per-beat grid
	// snapshots.
	DumpDirName = "dumps"
	// DumpBeatFormat names one beat's grid snapshot under DumpDirName.
	DumpBeatFormat = "beat-%04d.dump"
	// ledgerPreviewRunes caps the ledger's human-readable preview column.
	LedgerPreviewRunes = 60
	// LedgerHeader is the ledger's one header row.
	LedgerHeader = "#beat\tkind\tsrc_line\tat_ms\tpreview\ttext_b64\n"
)

// LedgerRow formats ONE ledger row, newline included.
//
// The row carries the beat's text TWICE, on purpose: a truncated preview for a
// human reading the file, and the full text base64-encoded so the checker
// compares the drawn text byte for byte — a preview cut at LedgerPreviewRunes
// would make the traceability check weaker than it looks.
//
// The preview is FLATTENED to one line: the ledger is a tab-separated table, and
// an answer fragment legitimately contains newlines. Left in, one beat's row would
// span several lines, the row would lose its columns, and a driver's "highest beat
// number" read would stop advancing at the first multi-line fragment — which is
// exactly how a run stalls with the producer still waiting for an acknowledgement
// (measured: beat 36 of a 201-beat run, held open until the timeout).
func LedgerRow(beat int, kind string, srcLine, atMS int, text string) string {
	return fmt.Sprintf("%d\t%s\t%d\t%d\t%s\t%s\n",
		beat, kind, srcLine, atMS,
		Flatten(CutRunes(text, LedgerPreviewRunes)),
		base64.StdEncoding.EncodeToString([]byte(text)))
}

// Flatten collapses a fragment's own line breaks and tabs to spaces, so one
// fragment is one ledger row.
func Flatten(s string) string {
	s = strings.ReplaceAll(s, "\t", " ")
	s = strings.ReplaceAll(s, "\r", " ")
	s = strings.ReplaceAll(s, "\n", " ")
	return s
}

// CutRunes returns at most n runes of s, marked with an ellipsis when it was cut.
func CutRunes(s string, n int) string {
	rs := []rune(s)
	if len(rs) <= n {
		return s
	}
	return string(rs[:n]) + Ellipsis
}

// Ellipsis marks a cut. It is one rune and one cell wide in the palette's own
// fonts, which is why it is a constant rather than a literal.
const Ellipsis = "…"

// DrainLedger is how a driver reads the highest beat number in a ledger: rows with
// fewer than six columns are skipped, so a partially written row never counts as a
// beat. It is exported here so the shell driver's awk and any Go reader describe
// the same acceptance.
func LedgerRowComplete(cols int) bool { return cols >= 6 }

// LedgerWriter appends rows to a ledger file from OUTSIDE this package (the chat
// surface's own producer). It exists so cmd/agent does not open the file, write a
// header and format rows of its own — the format is this package's, and a second
// writer would be a second format.
type LedgerWriter struct {
	f *bufio.Writer
	n int
}

// NewLedgerWriter creates (truncating) the ledger at path and writes its header.
func NewLedgerWriter(path string) (*LedgerWriter, error) {
	f, err := os.Create(path)
	if err != nil {
		return nil, fmt.Errorf("create the beat ledger %s: %w", path, err)
	}
	w := bufio.NewWriter(f)
	if _, err := w.WriteString(LedgerHeader); err != nil {
		return nil, fmt.Errorf("write the beat ledger header: %w", err)
	}
	// The header is FLUSHED here, not left in the buffer: the driver reads this
	// file from another process, and a header that appears only once the first
	// beat is written would make "the ledger exists and is well formed" a claim a
	// reader cannot check before the run starts (measured: a test reading the file
	// straight after the constructor found it empty).
	if err := w.Flush(); err != nil {
		return nil, fmt.Errorf("flush the beat ledger header: %w", err)
	}
	return &LedgerWriter{f: w}, nil
}

// Write appends one beat's row and FLUSHES it. The flush is not optional: a driver
// reads "there is a beat" and then acknowledges it, so the row must be on disk
// before the producer blocks — an unflushed row is a beat the driver never sees,
// i.e. a hang.
//
// atMS is the beat's offset from the run's origin. The chat surface leaves it at
// zero: the beats are the surface's own frame boundaries, and a producer that
// invented offsets would be recording a clock nobody else keeps.
func (l *LedgerWriter) Write(beat int, kind string, srcLine, atMS int, text string) error {
	if l == nil {
		return nil
	}
	if _, err := l.f.WriteString(LedgerRow(beat, kind, srcLine, atMS, text)); err != nil {
		return fmt.Errorf("write beat %d: %w", beat, err)
	}
	if err := l.f.Flush(); err != nil {
		return fmt.Errorf("flush beat %d: %w", beat, err)
	}
	l.n = beat
	return nil
}

// Beats reports how many rows this writer has written.
func (l *LedgerWriter) Beats() int {
	if l == nil {
		return 0
	}
	return l.n
}

// Close flushes and closes the ledger.
func (l *LedgerWriter) Close() error {
	if l == nil {
		return nil
	}
	if err := l.f.Flush(); err != nil {
		return fmt.Errorf("flush the beat ledger: %w", err)
	}
	return nil
}

// WriteDone writes the completion marker holding the run's beat count.
func WriteDone(path string, beats int) error {
	if err := os.WriteFile(path, []byte(strconv.Itoa(beats)+"\n"), 0o644); err != nil {
		return fmt.Errorf("write the completion marker %s: %w", path, err)
	}
	return nil
}
