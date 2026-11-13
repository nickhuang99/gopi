package ledgerwindow

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strconv"
	"time"
)

// Bounds is a window resolved to the epoch seconds the ledger stores — the
// quantity the predicate compares against, so a caller can log the bounds it
// actually asked about instead of the wall clock it typed.
type Bounds struct {
	// Since / Until are inclusive epoch-second bounds.
	Since int64
	Until int64
	// OffsetSec is the host's UTC offset at Since (the amount the bare form
	// shifts a window by). It is reported, never applied: the conversion lives
	// in the SQL, and a caller that recomputed it in Go would be the second
	// definition this package exists to prevent.
	OffsetSec int
}

// ErrInvertedWindow is returned for a window whose Since is after its Until.
//
// It is an ERROR rather than an empty result on purpose: an inverted window
// matches no row and reads exactly like a quiet period — the same silent
// failure the missing 'utc' modifier produces. Both make a report say "nothing
// happened" for a question that was never asked.
var ErrInvertedWindow = errors.New(
	"ledgerwindow: window is inverted (Since is after Until) — an inverted window matches no row and reads as a quiet period")

// ErrEmptyBound is returned when either bound is blank: an empty wall-clock
// string would be quoted into the predicate and silently match nothing.
var ErrEmptyBound = errors.New("ledgerwindow: window bound is empty")

// DateFormat is the wall-clock spelling a Window carries — the format
// strftime accepts as a bare date string ('YYYY-MM-DD HH:MM'), which is also
// what the operator types on a report's command line.
const DateFormat = "2006-01-02 15:04"

// LocalZone is the timezone the WALL-CLOCK BOUNDS are read in — the host's
// local zone, which is the zone the ledger's epoch stamps were written in
// (they are `date +%s` values). It is named here once so the 'utc' modifier's
// meaning is documented at its definition site rather than assumed.
var LocalZone = time.Local

// ParseBound converts one wall-clock bound to epoch seconds using the host's
// local zone — the SAME reading the 'utc' modifier gives it inside SQLite.
//
// This is the Go-side twin of the predicate: the two must agree, and the test
// suite asserts the agreement on a synthetic ledger rather than trusting it.
// It exists for callers that hold bounds in Go (a filename, a JSON field) and
// need the number without a round trip — never as a substitute for the
// predicate in a query.
func ParseBound(s string) (int64, error) {
	if s == "" {
		return 0, ErrEmptyBound
	}
	t, err := time.ParseInLocation(DateFormat, s, LocalZone)
	if err != nil {
		return 0, fmt.Errorf("parse wall-clock bound %q as %q: %w", s, DateFormat, err)
	}
	return t.Unix(), nil
}

// Validate reports whether the window is usable: both bounds non-empty and
// Since not after Until.
func (w Window) Validate() error {
	since, err := ParseBound(w.Since)
	if err != nil {
		return err
	}
	until, err := ParseBound(w.Until)
	if err != nil {
		return err
	}
	if until < since {
		return fmt.Errorf("%w: %s > %s", ErrInvertedWindow, w.Since, w.Until)
	}
	return nil
}

// BoundsQuery renders the ONE statement that resolves a window's bounds, with
// both values converted by the same predicate the counting query uses. Doing it
// in one statement (rather than two Go parses) is what makes the reported
// bounds provably the arithmetic the query ran.
func BoundsQuery() string {
	return selectKeyword + SpaceSQL + boundExpr(PlaceholderSQL) + CommaSQL + boundExpr(PlaceholderSQL)
}

// selectKeyword opens a projection.
const selectKeyword = "SELECT"

// ResolveBounds asks db for the epoch seconds of a window's two bounds, the
// same way the counting predicate computes them. Read-only: it runs one SELECT
// and opens no transaction.
func ResolveBounds(ctx context.Context, db *sql.DB, w Window) (Bounds, error) {
	if err := w.Validate(); err != nil {
		return Bounds{}, err
	}
	var sinceTxt, untilTxt string
	if err := db.QueryRowContext(ctx, BoundsQuery(), w.Since, w.Until).Scan(&sinceTxt, &untilTxt); err != nil {
		return Bounds{}, fmt.Errorf("resolve window bounds via %s: %w", BoundsQuery(), err)
	}
	since, err := strconv.ParseInt(sinceTxt, 10, 64)
	if err != nil {
		return Bounds{}, fmt.Errorf("bound %q is not an epoch number: %w", sinceTxt, err)
	}
	until, err := strconv.ParseInt(untilTxt, 10, 64)
	if err != nil {
		return Bounds{}, fmt.Errorf("bound %q is not an epoch number: %w", untilTxt, err)
	}
	_, offset := time.Unix(since, 0).In(LocalZone).Zone()
	return Bounds{Since: since, Until: until, OffsetSec: offset}, nil
}

// CountInWindow is the whole recipe in one call: how many rows of `column`
// carry an epoch stamp inside the wall-clock window, resolved in SQL.
//
// `column` is one of ColumnCreatedAt / ColumnClaimedAt / ColumnEndedAt (any
// ledger epoch column), and the query is CountQuery(column) with the two
// wall-clock bounds bound as parameters.
func CountInWindow(ctx context.Context, db *sql.DB, column string, w Window) (int64, error) {
	if err := w.Validate(); err != nil {
		return 0, err
	}
	var n int64
	if err := db.QueryRowContext(ctx, CountQuery(column), w.Since, w.Until).Scan(&n); err != nil {
		return 0, fmt.Errorf("count %s in window %s via %s: %w", column, w.Name(), CountQuery(column), err)
	}
	return n, nil
}
