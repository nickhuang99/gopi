// chat_prefetch_ledger_open.go — the gopi READING of a driver-level failure to
// open the operator's memory ledger.
//
// WHY IT EXISTS. The prefetch opens the ledger with a READ-ONLY DSN
// (tools.LedgerReadOnlyDSNIn: mode=ro + query_only(true)), and a read-only open
// cannot CREATE the file. When the ledger — or a directory in its path — is
// absent, the driver answers with its own sentence for SQLITE_CANTOPEN:
//
//	unable to open database file: out of memory (14)
//
// A wording that names neither the ledger nor the trigger, and whose "(14)"
// reads as an allocation failure: the reader is sent hunting for memory
// pressure while the actual defect is a PATH. Measured (this task's own probe,
// cmd/agent, 2026-10-11): a mode=ro open of a path whose parent directory does
// not exist produced exactly that string; the returned error's dynamic type was
// *sqlite.Error and its Code() was 14. `(14)` is SQLITE_CANTOPEN
// (modernc.org/sqlite/lib `const SQLITE_CANTOPEN = 14`), not SQLITE_NOMEM.
//
// So the ONE warm-opener seam translates it into gopi's OWN sentence — the
// ledger path, plus the path-shaped cause, plus the code — and keeps the
// driver's text in the chain (wrapped with %w, never discarded): the
// translation ADDS the diagnosis, it does not hide what the driver said
// (Rule 15). No new error type is introduced: the translation is a plain
// wrapped error.
//
// THE RECOGNITION AND THE SENTENCE ARE NOT THIS FILE'S ANY MORE. A second seam
// hit the same driver text (internal/tools/ledger_open_cause.go, the chat-index
// opener), so the code, the cause and the classifier live in
// internal/sqlitecode — one definition, two consumers (Rule 14). The names
// below are aliases of it, kept so this seam and its tests read unchanged.
package main

import (
	"fmt"

	"github.com/nickhuang99/gopi/internal/sqlitecode"
)

const (
	// prefetchLedgerCantOpenCode is SQLite's SQLITE_CANTOPEN result code, the
	// code the driver renders as "(14)" in the sentence above.
	//
	// IT IS AN ALIAS, NOT A SECOND DEFINITION (Rule 14). The number and the
	// cause sentence now live in internal/sqlitecode, because a SECOND seam hit
	// the same driver text and would otherwise have carried its own copy: the
	// chat-index opener (internal/tools/ledger_open_cause.go). One of two
	// copies gets corrected and the other keeps telling an operator to hunt
	// memory pressure, which is the drift this alias prevents. This name is
	// kept so the prefetch seam and its tests read unchanged.
	prefetchLedgerCantOpenCode = sqlitecode.CantOpen

	// prefetchLedgerCantOpenCause is gopi's statement of WHAT a CANTOPEN on a
	// ledger open actually means — the shared sentence (see sqlitecode). Named
	// (Rule 1b) because it is the sentence an operator reads on stderr and a
	// test compares, so the two must not drift.
	prefetchLedgerCantOpenCause = sqlitecode.CantOpenCause
)

// prefetchLedgerOpenCause returns err with gopi's own sentence PREPENDED when
// the driver reported SQLITE_CANTOPEN, and err UNCHANGED for every other
// failure. A nil error stays nil (Rule 15: the caller's `err != nil` test must
// keep meaning exactly what it meant).
//
// WHY A TRANSLATION RATHER THAN A NEW ERROR TYPE: the two callers of the warm
// opener act on the error as a whole — (*chatPrefetch).handle discloses it and
// falls back, prefetchLookup records its own segment attribution — so nobody
// needs to branch on the kind. A type hierarchy here would be machinery with no
// consumer; a sentence is what the operator was missing.
func prefetchLedgerOpenCause(err error, path string) error {
	if err == nil {
		return nil
	}
	if !sqlitecode.Is(err) {
		return err
	}
	return fmt.Errorf("cannot open the memory ledger %s (SQLite code %d): %s: %w",
		path, prefetchLedgerCantOpenCode, prefetchLedgerCantOpenCause, err)
}
