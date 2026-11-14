// ledger_open_cause.go — the gopi READING of a failure to open the shared
// ledger file, for every index face that reaches it through
// openLedgerIndexDB.
//
// WHY IT EXISTS. The driver's own sentence for SQLITE_CANTOPEN is
//
//	unable to open database file: out of memory (14)
//
// which names neither the FILE nor the TRIGGER, and whose "(14)" reads as an
// allocation failure. Measured on this host 2026-11-06 against an empty
// profile: `GOPI_HOME=<fresh dir> gopi session list` printed exactly that
// string under a `[degraded]` line, and a reader following it would go looking
// for memory pressure (free -m, a cgroup limit, the OOM killer) while the real
// defect was a PATH — the profile directory had never been created, so the
// ledger file could not be created either.
//
// So every error this opener returns names the PATH, and a CANTOPEN also
// carries gopi's statement of the cause and the result code. The driver's text
// stays in the chain, wrapped with %w and never discarded: the translation ADDS
// the diagnosis, it does not hide what the driver said (Rule 15). The
// recognition and the cause sentence live in internal/sqlitecode (Rule 14: ONE
// definition, shared with cmd/agent's warm read-only opener), not here.
package tools

import (
	"fmt"

	"github.com/nickhuang99/gopi/internal/sqlitecode"
)

// ledgerIndexOpenCause names the ledger file in every opener failure, and adds
// SQLite's own diagnosis when the failure was SQLITE_CANTOPEN. A nil error
// stays nil, so a caller's `err != nil` test keeps meaning exactly what it
// meant (Rule 15).
//
// It does NOT decide what a caller should DO about the failure — an absent file
// is a normal state for a read that has nothing to read, and that judgement
// belongs to the face (see chatSessionRowsForSources).
func ledgerIndexOpenCause(err error, path string) error {
	if err == nil {
		return nil
	}
	if sqlitecode.Is(err) {
		return fmt.Errorf("cannot open the ledger index %s (SQLite code %d): %s: %w",
			path, sqlitecode.CantOpen, sqlitecode.CantOpenCause, err)
	}
	return fmt.Errorf("cannot open the ledger index %s: %w", path, err)
}
