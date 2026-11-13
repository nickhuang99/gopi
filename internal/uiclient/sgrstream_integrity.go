package uiclient

// sgrstream_integrity.go — WHAT THE FILE ITSELF CONTRADICTS, told apart from what
// the bytes MEAN.
//
// THE MISDIAGNOSIS THIS FILE EXISTS TO END (measured 2026-11-07, task
// 01a1b336-4160). A 38,151,147-byte recording was read by a locator that walked
// its records in order; the recording's own bytes are
//
//	offset 0        header  `#gopi-sgr-stream version=1 … grid=0x0 …`
//	offset 116      trailer `#gopi-sgr-stream-end frames=0 bytes=0 grid=0x0`
//	offset 117..    a sparse NUL hole (38,150,984 bytes, of which 3,874,076 are
//	                real frame bytes near the end)
//	last 61 bytes   a SECOND, genuine trailer `frames=885963 bytes=35856031 grid=117x24`
//
// The walk stopped at the FIRST trailer (that is what a trailer means: the run
// closed), reported `frames=0`, and the only sentence the caller could produce was
// "the anchor does not match the real renderer" — a statement about the LOCATOR,
// produced by a file whose own records contradict each other. The reader sent its
// operator to inspect the renderer instead of the file.
//
// TWO FINDINGS, TWO SENTENCES, BOTH NUMERIC (Rule 15). A contradiction here is a
// fact about the FILE, and it is named with the offset and the size that show it:
//
//  1. RECORDS-AFTER-TRAILER — a trailer is not the end of the file. A sparse NUL
//     hole after it is the signature of two writers on one path: one opened the
//     path with O_TRUNC (uiclient.OpenSgrStream does exactly that), wrote a header
//     and, a moment later, a trailer with its own zero counts, while the FIRST
//     writer kept appending at fd offsets that are now past everything that was
//     just written. The kernel fills the gap with NULs.
//  2. UNPARSEABLE-RECORD — a record line in the frame area that is neither the
//     trailer prefix nor a decimal frame length. When it is NUL bytes it is the
//     same hole seen from inside the frame walk; when it is anything else it is
//     damage, and the reader must not report it as a frame-length complaint.
//
// WHAT THIS FILE IS NOT. It does not repair, truncate or skip anything, and it does
// not decide which writer was right. It answers one question for the reader above:
// "is this stream a file whose records agree with its own length?" — and when the
// answer is no, the caller's refusal names the file instead of the code that reads
// it. A stream that is merely STILL BEING WRITTEN (no trailer yet, or a frame cut
// mid-payload) is NOT a contradiction: that is a state of the world, and it stays
// tolerated exactly as before.

import (
	"bytes"
	"fmt"
	"io"
	"strings"
)

const (
	// sgrIntegrityScanWindow bounds how much of the after-trailer region is
	// EXAMINED to say what it is made of. The finding does not depend on the
	// whole region being read (the region can be tens of megabytes): the offset,
	// the total length and a counted window are enough to name it, and reading
	// all of it would make a refusal cost as much as a full copy.
	sgrIntegrityScanWindow = 4096
	// sgrIntegrityPreview is how many bytes of a damaged region are quoted back
	// to the caller. Quoting a byte count with no sample leaves a reader unable
	// to tell a hole from a flipped bit.
	sgrIntegrityPreview = 24
	// sgrIntegrityPreviewTextMax bounds how many PRINTABLE bytes of a preview are
	// rendered, so one corrupt record cannot put kilobytes of garbage into an
	// error message a human has to read.
	sgrIntegrityPreviewTextMax = 64
	// SgrStreamContradictionKind* are the three findings this file distinguishes.
	// They are spelled once and compared by these constants (Rule 1b).
	//
	// EXPORTED because a CALLER has to name the finding it holds: the replay verb
	// quotes the kind it met so an operator can tell "a second writer left bytes
	// after the close-out record" from "a record in the frame area cannot be read"
	// (cmd/agent/chat_sgr_replay.go). An unexported spelling would force that caller
	// to re-type the literal, which is how two spellings of one finding begin.
	SgrStreamContradictionKindAfterTrailer = "records-after-trailer"
	SgrStreamContradictionKindBadRecord    = "unparseable-record"
	// SgrStreamContradictionKindCountMismatch is the third finding, and the cheapest one to
	// catch: a trailer AT the end of the file whose own counts disagree with the
	// frames the walk actually read. The writer emits those counts from the same
	// struct it appends from, so a disagreement is a defect in the artifact, not
	// a tolerance question.
	SgrStreamContradictionKindCountMismatch = "trailer-count-mismatch"
)

// SgrStreamContradiction is one place where a stream's own bytes and its own
// records disagree. Nothing here is an interpretation: Offset and Size are
// measured, and What says which of the two findings it is.
type SgrStreamContradiction struct {
	// Kind is SgrStreamContradictionKindAfterTrailer or SgrStreamContradictionKindBadRecord.
	Kind string
	// Offset is where the contradicting region begins (bytes from file start).
	Offset int64
	// Size is how many bytes the finding covers, as far as it is known.
	Size int64
	// Detail is the measured sentence: counts, the trailer's own declaration, and
	// a bounded sample of the bytes.
	Detail string
}

// String renders one finding for an error message that a human reads.
func (c SgrStreamContradiction) String() string {
	return fmt.Sprintf("%s at offset %d (%d byte(s)): %s", c.Kind, c.Offset, c.Size, c.Detail)
}

// sgrAfterTrailerFinding reports the contradiction of a trailer that is not at the
// end of the file, or nil when the trailer IS the end (or there is no trailer).
//
// pos is the offset just past the trailer line (readSgrRecordAt's second return),
// so the size of the surplus is plain arithmetic — the reader does not have to
// trust any count the writer declared.
func sgrAfterTrailerFinding(ra io.ReaderAt, pos, size int64, meta SgrStreamMeta) (*SgrStreamContradiction, error) {
	if pos >= size {
		return nil, nil
	}
	rest := size - pos
	scan := rest
	if scan > sgrIntegrityScanWindow {
		scan = sgrIntegrityScanWindow
	}
	buf := make([]byte, scan)
	n, rerr := ra.ReadAt(buf, pos)
	if rerr != nil && rerr != io.EOF {
		return nil, fmt.Errorf("uiclient: cannot read the %d byte(s) that follow the SGR stream's trailer at offset %d: %w", scan, pos, rerr)
	}
	buf = buf[:n]
	zeros := bytes.Count(buf, []byte{0})
	var detail string
	if zeros == len(buf) && len(buf) > 0 {
		detail = fmt.Sprintf("the trailer declares frames=%d bytes=%d yet %d byte(s) follow it to end of file, and the first %d of them are ALL NUL — a sparse hole, which is what two writers on one path leave: a second writer truncated the path and wrote a header+trailer pair while the first kept appending at its old fd offsets",
			meta.Frames, meta.Bytes, rest, len(buf))
	} else {
		detail = fmt.Sprintf("the trailer declares frames=%d bytes=%d yet %d byte(s) follow it to end of file (%d of the first %d are NUL; first bytes %s) — a closed stream has nothing after its trailer, so this file has more than one writer or more than one stream in it",
			meta.Frames, meta.Bytes, rest, zeros, len(buf), sgrIntegritySample(buf))
	}
	return &SgrStreamContradiction{Kind: SgrStreamContradictionKindAfterTrailer, Offset: pos, Size: rest, Detail: detail}, nil
}

// sgrBadRecordFinding names a record line the frame walk cannot read, telling the
// two shapes apart: a NUL run (the same hole, met from inside the frame area) and
// anything else (damage). It is called only when the line is neither the trailer
// prefix nor a decimal length — the caller has already ruled those out.
//
// A record cut off by the end of file is NOT this finding: the caller tolerates it
// as truncation, which is what a live recording looks like. This one is raised for
// a COMPLETE line, i.e. bytes the writer finished writing and meant as a record.
func sgrBadRecordFinding(pos int64, line string) SgrStreamContradiction {
	raw := []byte(line)
	zeros := bytes.Count(raw, []byte{0})
	var detail string
	if zeros == len(raw) && len(raw) > 0 {
		detail = fmt.Sprintf("the record is %d NUL byte(s) and no newline-terminated frame length — the frame area starts inside a sparse hole, so the offsets after this point do not belong to the stream that wrote the header (a second writer truncating the same path produces exactly this)", len(raw))
	} else {
		detail = fmt.Sprintf("the record %q is neither the %s trailer nor a decimal frame length (%d of its %d byte(s) are NUL; frame area damaged or written by a second stream)",
			sgrIntegrityTruncateForMessage(line), ScreenStreamTrailerPrefix, zeros, len(raw))
	}
	return SgrStreamContradiction{Kind: SgrStreamContradictionKindBadRecord, Offset: pos, Size: int64(len(raw)), Detail: detail}
}

// sgrIntegritySample renders at most sgrIntegrityPreview bytes of a region as a
// Go-style quoted string, truncating by the byte and never by the rune, so a
// message about a binary region stays readable.
func sgrIntegritySample(b []byte) string {
	n := len(b)
	if n > sgrIntegrityPreview {
		n = sgrIntegrityPreview
	}
	if n == 0 {
		return `""`
	}
	return fmt.Sprintf("%q", b[:n])
}

// sgrIntegrityTruncateForMessage quotes a record line for a human, bounded by
// sgrIntegrityPreviewTextMax printable bytes so a damaged line cannot flood the
// message it appears in.
func sgrIntegrityTruncateForMessage(line string) string {
	printable := make([]byte, 0, len(line))
	for i := 0; i < len(line) && len(printable) < sgrIntegrityPreviewTextMax; i++ {
		c := line[i]
		if c < 0x20 || c > 0x7e {
			printable = append(printable, '.')
			continue
		}
		printable = append(printable, c)
	}
	if len(printable) == 0 {
		return strings.Repeat(".", min(len(line), sgrIntegrityPreviewTextMax))
	}
	return string(printable)
}

// SgrStreamContradictionError is the ONE refusal a reader produces when a
// stream's records contradict its own length. It is a TYPE rather than a formatted
// string so a caller can tell this finding apart from every other read failure and
// say which it holds (Rule 15): the locator's own report ("the anchor matches
// nothing") and the file's report ("this stream is in two pieces") are different
// diagnoses with different owners.
//
// THE WORDING IS THE DELIVERABLE. It says the file is contradictory, quotes the
// offsets and sizes that show it, and states that no reading of this file is
// evidence about the code that wrote or read it — because that is the sentence
// whose absence sent an operator to inspect the renderer.
type SgrStreamContradictionError struct {
	// Path is the stream as the caller named it; empty when the reader holds no
	// path (a range read over an io.ReaderAt), in which case the message names
	// the size only.
	Path string
	// Size is the file's size on disk, as the OS reported it.
	Size int64
	// Findings is every contradiction the walk met, in file order.
	Findings []SgrStreamContradiction
}

// Error implements error.
func (e *SgrStreamContradictionError) Error() string {
	parts := make([]string, 0, len(e.Findings))
	for _, f := range e.Findings {
		parts = append(parts, f.String())
	}
	where := "the SGR stream"
	if e.Path != "" {
		where = "the SGR stream " + e.Path
	}
	return fmt.Sprintf("uiclient: %s contradicts itself (%d byte(s) on disk): %s. This is a fact about the FILE, not about the code that reads or writes it — a reader cannot be blamed for a stream whose own records disagree with its own length",
		where, e.Size, strings.Join(parts, "; "))
}

// sgrCountMismatchFinding compares the trailer's declaration with what the walk
// read, and reports the disagreement as a finding of its own.
//
// IT IS SEPARATE FROM THE SURPLUS CHECK because the two are different defects with
// different repairs: a surplus says the FILE has more than one writer in it, while a
// count mismatch says ONE writer's record and its own bytes disagree. Merging them
// into one sentence would leave an operator unable to tell which happened.
func sgrCountMismatchFinding(pos int64, meta SgrStreamMeta, frames int, payload int64) *SgrStreamContradiction {
	if meta.Frames == frames && meta.Bytes == payload {
		return nil
	}
	return &SgrStreamContradiction{
		Kind:   SgrStreamContradictionKindCountMismatch,
		Offset: pos,
		Size:   payload,
		Detail: fmt.Sprintf("the trailer declares frames=%d bytes=%d but the frames before it total %d frame(s) and %d byte(s)",
			meta.Frames, meta.Bytes, frames, payload),
	}
}
