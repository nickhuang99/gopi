// Package sqlitecode — the ONE definition of the driver result codes gopi
// RECOGNISES by number, and the ONE reading of what each one means.
//
// WHY A PACKAGE AND NOT A CONSTANT IN EACH SEAM. Two different seams in this
// tree hit the same driver sentence and must not disagree about it: the warm
// read-only ledger opener (cmd/agent/chat_prefetch_ledger_open.go) and the
// chat-index opener (internal/tools). A second copy of the number is exactly
// the drift Rule 14 exists to stop — one of them gets corrected and the other
// keeps telling an operator to go hunting for memory pressure.
//
// WHY THE NUMBER AND NOT THE LIBRARY'S CONSTANT (the measured reason, stated
// where the number lives): modernc.org/sqlite's `lib` package is generated
// per-architecture, so importing it to read a value the driver reports as a
// plain `int` would make recognition depend on that library's internals. The
// number is stated once, HERE, with its source named: modernc.org/sqlite/lib's
// `const SQLITE_CANTOPEN = 14`.
//
// This package is a LEAF on purpose (only constants and one classifier, no
// project imports) so every consumer can reach it without a cycle — the shape
// Rule 14 prescribes and internal/toolnames already has.
package sqlitecode

import "errors"

const (
	// CantOpen is SQLite's SQLITE_CANTOPEN result code, the code the driver
	// renders as "(14)".
	CantOpen = 14

	// CantOpenCause is gopi's statement of WHAT a CANTOPEN actually means. The
	// driver's own wording for this code is "unable to open database file: out
	// of memory (14)", which reads as an allocation failure and sends the
	// reader to `free -m`, a cgroup limit or the OOM killer — while the real
	// defect is a PATH or a PERMISSION. Measured (2026-10-11, and again on an
	// empty profile 2026-11-06): a mode=ro open of a path whose parent
	// directory does not exist, and a read-write open of a file with mode 000,
	// both produce exactly that string, and the returned error's dynamic type
	// is *sqlite.Error with Code() == 14 in both cases.
	//
	// Named (Rule 1b) because it is a sentence an operator reads and a test
	// compares, so the two must not drift.
	CantOpenCause = "either the file, a directory in its path, or the permission to reach it is missing — this is SQLite's SQLITE_CANTOPEN, NOT an allocation failure"
)

// Is reports whether err carries SQLite's SQLITE_CANTOPEN code.
//
// It reads the code through an anonymous interface so this package takes no
// dependency of its own, and it walks the whole wrap chain (errors.As) for the
// same reason: the seam that produced the driver error is often two wraps below
// the error the caller holds, and a classifier that only looked at the top would
// miss every translated error.
func Is(err error) bool {
	if err == nil {
		return false
	}
	var coded interface{ Code() int }
	if !errors.As(err, &coded) {
		return false
	}
	return coded.Code() == CantOpen
}
