// Package ledgerwindow is the ONE definition of the wall-clock WINDOW
// predicate over the ledger's epoch columns (Rule 14: a value two surfaces
// must agree on lives in one small exported leaf package; Rule 1b/13: every
// SQL token is a named constant, referenced — never re-typed inline).
//
// THE TRAP THIS PACKAGE EXISTS TO CLOSE.
//
// The ledger's time columns (tasks.created_at / claimed_at / ended_at) hold
// TRUE epoch seconds: the value `date +%s` reports for the local wall clock.
// SQLite's date functions do not read them that way. `strftime('%s','X')`
// parses the bare date string X — 'YYYY-MM-DD HH:MM' — as UTC, i.e.
//
//	SELECT strftime('%s','2026-09-16 22:00');         -- 1789576800   (UTC reading)
//	SELECT strftime('%s','2026-09-16 22:00','utc');   -- 1789567200   (local reading)
//
// The difference is the host's UTC offset (28800 s on a CST/+0800 host). A
// window written with the bare form therefore asks about a window that is
// eight hours LATER than the operator meant, on a host whose ledger rows are
// stamped by the local clock:
//
//	claimed_at BETWEEN strftime('%s','2026-09-16 22:00')           -- the trap
//	               AND strftime('%s','2026-09-17 05:00')
//
// fails with NO error, over- or under-reports (measured: 6 rows instead of 38
// on this host's live ledger), and reads exactly like "a quiet night". The
// 'utc' modifier is SQLite's own spelling for "convert this wall-clock string
// from UTC into the local timezone" — applied to a wall-clock literal it
// yields the epoch the local clock would have stamped, which is the convention
// the ledger stores.
//
// WHY A WINDOW HELPER AND NOT A SQL SNIPPET IN EACH SURFACE: the bare form is
// invisible in review (it looks like correct SQL) and its symptom is a NUMBER,
// not an error. One definition, one regression test that asserts the WINDOW
// RESULT, and one exhaustive gate over checked-in sources are what keep a new
// report surface from re-introducing it.
//
// The package is a leaf: stdlib plus the pure-Go SQLite driver (Rule 7 — the
// repo's zero-CGO driver is the only dependency), no imports of project
// packages, so no consumer can create an import cycle.
package ledgerwindow

// Ledger column names. Each is the single definition site for a name that is
// also persisted data: the ledger's own writers spell the same names, so a
// consumer that types the literal instead of this constant is one rename away
// from querying a column that no longer exists (Rule 13).
const (
	// ColumnCreatedAt is the row's submission time (epoch seconds).
	ColumnCreatedAt = "created_at"
	// ColumnClaimedAt is the time a runner took the row (epoch seconds);
	// NULL until claimed.
	ColumnClaimedAt = "claimed_at"
	// ColumnEndedAt is the time the row reached a terminal state (epoch
	// seconds); NULL while it is open.
	ColumnEndedAt = "ended_at"
)

// SQL tokens of the date predicate. They are constants for the ordinary Rule 1b
// reason (they are meaning-carrying literals) and for one more: the gate in
// gate_test.go derives what a *correct* predicate looks like from these values,
// so the detector and the shipped query cannot describe different predicates.
const (
	// FuncStrftime is SQLite's date-formatting function. It is used here for
	// one job: turning a wall-clock string into epoch seconds.
	FuncStrftime = "strftime"
	// FormatEpochSeconds is the conversion specifier that renders a
	// timestamp as epoch seconds — the format the ledger's columns store.
	FormatEpochSeconds = "%s"
	// UTCModifier is the SQLite modifier NAME for "interpret/convert against
	// the local timezone".
	UTCModifier = "utc"
	// UTCModifierSQL is that modifier in its SQL literal spelling, quotes
	// included. It is the value a wall-clock window predicate MUST carry —
	// and the value a gate can search for (Rule 1b: the quoted form is spelled
	// once, here, never in a query string).
	UTCModifierSQL = "'" + UTCModifier + "'"
	// NowKeyword is SQLite's spelling of the ABSOLUTE present instant
	// ('now'). It is named here because the gate leans on it: `'now'` carries
	// no calendar date, so it is not a wall-clock literal being misread, and
	// the gate spares it for that reason rather than by an allowlist entry.
	NowKeyword = "now"
	// AndOperator joins the two halves of an interval predicate.
	AndOperator = "AND"
	// BetweenKeyword is the interval predicate's operator.
	BetweenKeyword = "BETWEEN"
)

// Window is a wall-clock interval, spelled exactly as the operator reads it
// off a report: 'YYYY-MM-DD HH:MM'. It is NOT an epoch pair — that is the
// point: the caller states the wall clock it means, and this package renders
// the predicate that converts it the way the ledger was stamped.
type Window struct {
	// Since is the inclusive lower bound, 'YYYY-MM-DD HH:MM'.
	Since string
	// Until is the inclusive upper bound, 'YYYY-MM-DD HH:MM'.
	Until string
}

// Name is the window as the operator spelled it ("2026-09-16 22:00 ..
// 2026-09-17 05:00") — for report headlines, where quoting the window is what
// makes a number auditable.
func (w Window) Name() string { return w.Since + " .. " + w.Until }
