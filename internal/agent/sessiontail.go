package agent

// sessiontail.go — the BOUNDED tail reader behind HasSessionEnd.
//
// The orphan sweep (BackfillOrphanCrashDumps → DetectOrphanSessions) runs on
// every gopi startup over the whole sessions tree. HasSessionEnd used to call
// os.ReadFile — the WHOLE JSONL (up to 31 MB observed, 405 MB across the
// reference tree's 28.5 k entries) — to inspect at most
// sessionEndScanBackMax bytes of tail: 430.9 MB of allocation churn and a
// measured 668 ms per start, with zero dumps to write
// (devdoc/crash-dump-startup-backfill.md, branch diag/crash-dump-startup).
//
// This file is the COST half of the fix: the same verdict from O(tail) bytes.
// The CLASSIFICATION predicate ("what counts as a crash") is untouched and
// still owned by the sibling task — HasSessionEnd lives in session.go and
// hands the walk the same bytes it used to receive.

import (
	"io"
	"os"
)

// sessionTailWindowBytes is the byte budget ONE sessionTailRead call may take
// off disk: sessionEndScanBackMax of event tail plus the single byte before
// that bound — which, on a file larger than the bound, is the newline that
// terminates the partial line the window cut off. The walk needs that byte as
// the boundary of its first candidate line; without it the partial head line
// would be spliced onto the first complete event and every candidate would be
// unparseable (the same newline-bounded-walk invariant the whole-file version
// documented).
//
// WHY THE WINDOW IS THE WHOLE READ (not just the walk's bound): the walk's
// scan-back bound allows it to travel sessionEndScanBackMax bytes back from
// the end. Any byte the walk can legitimately inspect must therefore be in the
// window, so window size = bound + the one boundary byte.
const sessionTailWindowBytes = sessionEndScanBackMax + 1

// sessionTailReader is the minimal file surface a tail read needs (ReaderAt +
// Stat + Close). A named interface so the bounded-read proof can substitute a
// counting reader — the seam exists for the test, not for production, where it
// is always an *os.File.
type sessionTailReader interface {
	io.ReaderAt
	Stat() (os.FileInfo, error)
	Close() error
}

// openSessionTailReader opens a session JSONL for the tail read. The test seam
// behind the bounded-read guarantee: a substituted opener counts the bytes
// ReadAt actually pulls off disk.
var openSessionTailReader = func(path string) (sessionTailReader, error) {
	return os.Open(path)
}

// sessionTailRead returns the BOUNDED window at the end of the session JSONL:
// at most sessionTailWindowBytes bytes (fewer for a smaller file), read with
// ReadAt from size-sessionTailWindowBytes onward. ok=false means "no window to
// walk" — an unreadable, empty or concurrently truncated file; the caller's
// documented best-effort verdict (false = orphan candidate, re-inspected on
// the next sweep) is the same one the whole-file version reached for an empty
// or unreadable file.
//
// A partial read (file truncated between Stat and ReadAt) is NOT an error
// here: ReadAt returns the bytes it has plus io.EOF, and the truncated tail is
// a valid window over the file as it exists now.
func sessionTailRead(path string) ([]byte, bool) {
	f, err := openSessionTailReader(path)
	if err != nil {
		return nil, false
	}
	defer f.Close() // read-only probe on a best-effort path: the close carries no signal
	fi, err := f.Stat()
	if err != nil || fi.Size() <= 0 {
		return nil, false
	}
	size := fi.Size()
	windowStart := int64(0)
	if size > sessionTailWindowBytes {
		windowStart = size - sessionTailWindowBytes
	}
	windowLen := int(size - windowStart)

	// A FRESH buffer per call, deliberately not a pooled one: the window is
	// returned to the caller as a slice, so a buffer recycled at return would
	// alias whatever the next caller reads — a correctness hazard in exchange
	// for ~6 MB of the ~430 MB this file's whole-file read used to churn across
	// the reference tree's 1469 JSONL. Measured: the churn stays ~27 MB with a
	// fresh window and no cross-call state to reason about.
	buf := make([]byte, windowLen)
	n, err := f.ReadAt(buf, windowStart)
	if n <= 0 || (err != nil && err != io.EOF) {
		return nil, false
	}
	return buf[:n], true
}
