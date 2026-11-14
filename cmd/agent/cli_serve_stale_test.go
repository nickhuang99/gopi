package main

// Shared ledger fixtures that OUTLIVED the stale sweep's tests (Rule 43).
//
// WHAT THIS FILE WAS. It carried the stale-sweep carrier tests — `gopi task
// list --sweep` → runTaskList → maybeTaskListSweep → staleSweep →
// agent.DetectStaleTasks — and the two E2E arms that pinned "the worker does
// not report stale rows, the operator's one verb does".
//
// WHAT CHANGED (2026-11-05). The stale sweep is DELETED — the detector, the
// `--sweep` flag, the persisted throttle stamp and the re-arm UPDATE. The
// operator's ruling: no process watches another process for it. A stale row is
// now simply READ as stale wherever it is displayed, and the ways to repair one
// are the ledger's own verbs. So the arms above have no subject, and what
// remains here are the two fixtures other test files in this package still
// need (`openRawTasksDB` and the pid spelling) — MOVED, not deleted with the
// mechanism that happened to host them.

import (
	"database/sql"

	"github.com/nickhuang99/gopi/internal/tools"
)

// openRawTasksDB opens the seeded ledger directly for claim-stamp surgery
// (claimed_by has no API setter — claims are CAS-managed by design). The
// sqlite driver is registered by the tools package import.
//
// It opens through tools.LedgerDSN, NEVER a bare DSN: this handle is the
// WRITE half of every zombie/claim fixture (disposeFixture's `UPDATE
// tasks …`), so a DSN without a busy timeout loses the race against the
// resident daemon the same test starts and the fixture dies with
// `database is locked (5) (SQLITE_BUSY)` — measured 7 of 8 isolated runs of
// TestMergerDaemon_DisposesZombieWithZeroWorkerProcesses.
func openRawTasksDB(path string) (*sql.DB, error) {
	return sql.Open("sqlite", tools.LedgerDSN(path))
}

// itoaPID renders a pid for the claim stamp fixture.
func itoaPID(n int) string {
	return strconvItoa(n)
}

// strconvItoa avoids pulling strconv into two test files for one call.
func strconvItoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var b [20]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		b[i] = '-'
	}
	return string(b[i:])
}
