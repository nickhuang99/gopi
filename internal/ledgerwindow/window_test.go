package ledgerwindow

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	_ "modernc.org/sqlite" // the repo's zero-CGO SQLite driver (Rule 7)
)

// THE FIXTURE IS SYNTHETIC AND IN-MEMORY. The trap is a property of SQLite's
// date parsing and of the ledger's storage convention — it does not need the
// live ledger to demonstrate, and the live ledger must never be written by a
// test. Every epoch below is computed by this test, so no expected count is a
// copied number.

const (
	// fixtureWindowSince / fixtureWindowUntil spell the window the way an
	// operator types it on a report command line (the briefing's own window).
	fixtureWindowSince = "2026-09-16 22:00"
	fixtureWindowUntil = "2026-09-17 05:00"
	// fixtureSpreadHours is how far the fixture's hourly grid extends beyond
	// each end of the window: rows outside the window are what make a SHIFTED
	// window produce a different count (without them the trap is invisible).
	fixtureSpreadHours = 6
	// fixtureHourSec / fixtureMinSec are the epoch widths the grid steps by.
	fixtureHourSec = 3600
	fixtureMinSec  = 60
	// fixtureNullEvery makes every Nth grid row carry a NULL claimed_at, so the
	// predicate's NULL behaviour is asserted rather than assumed (BETWEEN
	// excludes NULL, and a NULL row is not "in the window").
	fixtureNullEvery = 5
)

// fixtureWindow is the window under test.
func fixtureWindow() Window {
	return Window{Since: fixtureWindowSince, Until: fixtureWindowUntil}
}

// openFixtureDB opens a private in-memory ledger with the three epoch columns
// and returns it with the grid of epochs it was seeded with.
//
// `:memory:` (not a file) is deliberate: nothing this test does can reach the
// live ~/.gopi/memory.db.
func openFixtureDB(t *testing.T) (*sql.DB, []int64) {
	t.Helper()
	db, err := sql.Open(fixtureDriverName, fixtureMemoryDSN)
	if err != nil {
		t.Fatalf("open the in-memory fixture ledger: %v", err)
	}
	// The fixture handle is closed when the test ends — after every assertion,
	// so a close error cannot outlive the verdict. The count assertions are
	// what matter here; a close failure has no verdict to change.
	t.Cleanup(func() { _ = db.Close() }) //stylegate:ignore test fixture handle; see the note above

	ddl := "CREATE TABLE " + TableTasks + " (" +
		ColumnCreatedAt + " INTEGER, " + ColumnClaimedAt + " INTEGER, " + ColumnEndedAt + " INTEGER)"
	if _, err := db.Exec(ddl); err != nil {
		t.Fatalf("create the fixture ledger (%s): %v", ddl, err)
	}

	since, err := ParseBound(fixtureWindowSince)
	if err != nil {
		t.Fatalf("parse the fixture window's lower bound: %v", err)
	}
	until, err := ParseBound(fixtureWindowUntil)
	if err != nil {
		t.Fatalf("parse the fixture window's upper bound: %v", err)
	}

	var epochs []int64
	insert := "INSERT INTO " + TableTasks + " (" + ColumnCreatedAt + ", " + ColumnClaimedAt + ", " +
		ColumnEndedAt + ") VALUES (?, ?, NULL)"
	for at := since - fixtureSpreadHours*fixtureHourSec; at <= until+fixtureSpreadHours*fixtureHourSec; at += fixtureHourSec {
		epochs = append(epochs, at)
	}
	for i, at := range epochs {
		claimed := any(at)
		if i%fixtureNullEvery == 0 {
			claimed = nil // a row stamped only at creation is never "claimed in window"
		}
		if _, err := db.Exec(insert, at, claimed); err != nil {
			t.Fatalf("seed the fixture row at %d: %v", at, err)
		}
	}
	return db, epochs
}

const (
	// fixtureDriverName is the driver the repo registers (memory.go imports the
	// same package blank).
	fixtureDriverName = "sqlite"
	// fixtureMemoryDSN is the in-memory database DSN.
	fixtureMemoryDSN = ":memory:"
)

// countClaimedInRange is the Go-side truth: how many of `epochs` lie inside the
// inclusive epoch interval. It is deliberately plain integer arithmetic — it
// shares nothing with the SQL under test, so agreement between the two is
// evidence rather than a tautology.
func countClaimedInRange(epochs []int64, lo, hi int64) int {
	n := 0
	for _, e := range epochs {
		if e >= lo && e <= hi {
			n++
		}
	}
	return n
}

// TestWindowResultMatchesGoEpochs is the WINDOW-RESULT assertion: the helper's
// query, run against a synthetic ledger of known epochs, returns exactly the
// number of rows the same window selects when the bounds are computed in Go.
//
// It asserts a RESULT, never SQL text — which is what makes it a regression
// guard for the 'utc' modifier: dropping the modifier changes the epoch the
// bounds resolve to, changes the count, and reddens THIS test.
func TestWindowResultMatchesGoEpochs(t *testing.T) {
	db, epochs := openFixtureDB(t)
	w := fixtureWindow()
	since, err := ParseBound(w.Since)
	if err != nil {
		t.Fatalf("parse %q: %v", w.Since, err)
	}
	until, err := ParseBound(w.Until)
	if err != nil {
		t.Fatalf("parse %q: %v", w.Until, err)
	}

	// The Go-side expectation, restricted to the rows that actually carry a
	// claimed_at stamp (every fixtureNullEvery-th row is NULL).
	nonNull := make([]int64, 0, len(epochs))
	for i, e := range epochs {
		if i%fixtureNullEvery != 0 {
			nonNull = append(nonNull, e)
		}
	}
	want := countClaimedInRange(nonNull, since, until)
	if want == 0 {
		t.Fatalf("the fixture is degenerate: a window of %d rows selected none", len(epochs))
	}

	got, err := CountInWindow(context.Background(), db, ColumnClaimedAt, w)
	if err != nil {
		t.Fatalf("count %s in %s: %v", ColumnClaimedAt, w.Name(), err)
	}
	if got != int64(want) {
		t.Errorf("window %s over %s = %d rows, want %d (the Go-computed epoch bounds [%d,%d] select %d) — "+
			"the predicate resolved the wall-clock bounds to different epoch seconds than the local clock stamps",
			w.Name(), ColumnClaimedAt, got, want, since, until, want)
	}

	// The same interval expressed WITHOUT the helper (plain epoch bounds, no
	// date function at all) must agree — an independent SQL path to the same
	// number.
	plainQuery := "SELECT COUNT(*) FROM " + TableTasks + " WHERE " + ColumnClaimedAt + " BETWEEN ? AND ?"
	var plain int64
	if err := db.QueryRow(plainQuery, since, until).Scan(&plain); err != nil {
		t.Fatalf("count with plain epoch bounds: %v", err)
	}
	if plain != got {
		t.Errorf("plain epoch bounds select %d rows, the predicate selects %d — the two must describe one window",
			plain, got)
	}

	// Every epoch column may be windowed the same way: the helper takes the
	// column, so a report over created_at uses one definition too.
	for _, col := range []string{ColumnCreatedAt, ColumnClaimedAt, ColumnEndedAt} {
		if _, err := CountInWindow(context.Background(), db, col, w); err != nil {
			t.Errorf("count %s in %s: %v", col, w.Name(), err)
		}
	}
}

// TestResolveBoundsReadsTheLocalClock is the second half of the recipe: the
// reported bounds must be the numbers the query used, and their difference from
// the bare (UTC-reading) form must be exactly the host's UTC offset.
func TestResolveBoundsReadsTheLocalClock(t *testing.T) {
	db, _ := openFixtureDB(t)
	w := fixtureWindow()
	since, err := ParseBound(w.Since)
	if err != nil {
		t.Fatalf("parse %q: %v", w.Since, err)
	}
	until, err := ParseBound(w.Until)
	if err != nil {
		t.Fatalf("parse %q: %v", w.Until, err)
	}

	bounds, err := ResolveBounds(context.Background(), db, w)
	if err != nil {
		t.Fatalf("resolve bounds of %s: %v", w.Name(), err)
	}
	if bounds.Since != since || bounds.Until != until {
		t.Errorf("resolved bounds [%d,%d], want [%d,%d] — the SQL and Go readings of the same wall clock must agree",
			bounds.Since, bounds.Until, since, until)
	}
	_, offset := time.Unix(since, 0).In(LocalZone).Zone()
	if bounds.OffsetSec != offset {
		t.Errorf("reported UTC offset %d, want %d (the host's offset at %s)", bounds.OffsetSec, offset, w.Since)
	}
	if bounds.Until-bounds.Since != fixtureHoursOfWindow*fixtureHourSec {
		t.Errorf("window spans %d s, want %d — a 7-hour wall-clock window is 7 hours of epoch",
			bounds.Until-bounds.Since, fixtureHoursOfWindow*fixtureHourSec)
	}
}

// fixtureHoursOfWindow is the window's width in hours (22:00 → 05:00).
const fixtureHoursOfWindow = 7

// TestBareFormShiftsTheWindowByTheHostOffset is the RED-PROOF: the same window,
// evaluated with the 'utc' modifier absent, resolves to bounds shifted by
// exactly the host's UTC offset and selects a DIFFERENT set of rows — and the
// shifted selection is the one the Go-side arithmetic predicts.
//
// The bare expression is assembled here from the package's own constants
// (without UTCModifierSQL): the test carries no bare-form SQL literal, and the
// "removal" it simulates is the one a future editor would actually make.
func TestBareFormShiftsTheWindowByTheHostOffset(t *testing.T) {
	db, epochs := openFixtureDB(t)
	w := fixtureWindow()

	correct, err := ResolveBounds(context.Background(), db, w)
	if err != nil {
		t.Fatalf("resolve bounds of %s: %v", w.Name(), err)
	}
	if correct.OffsetSec == 0 {
		// The skip is honest but not silent: the reason explains why a UTC host
		// cannot show the divergence, and the window-result assertion still holds.
		t.Skip("this host's UTC offset is zero, so the bare form cannot diverge from the local reading here")
	}

	// The bare expression: the same call, WITHOUT the modifier.
	bare := bareBoundExpr()
	bareWhere := ColumnClaimedAt + SpaceSQL + BetweenKeyword + SpaceSQL + bare + SpaceSQL +
		AndOperator + SpaceSQL + bare
	bareQuery := SelectCountPrefix + bareWhere

	var bareCount int64
	if err := db.QueryRow(bareQuery, w.Since, w.Until).Scan(&bareCount); err != nil {
		t.Fatalf("count with the bare (modifier-less) form %q: %v", bareQuery, err)
	}

	// What the bare form's bounds ARE: the modifier-less reading of each bound,
	// read back from SQL so the shift is measured, not assumed.
	var bareSince int64
	sinceExpr := selectKeyword + SpaceSQL + bare
	if err := db.QueryRow(sinceExpr, w.Since).Scan(&bareSince); err != nil {
		t.Fatalf("read the bare lower bound (%s): %v", sinceExpr, err)
	}
	if shift := bareSince - correct.Since; shift != int64(correct.OffsetSec) {
		t.Errorf("the modifier-less form shifts the lower bound by %d s, want exactly the host offset %d s — "+
			"the shift IS the timezone, nothing else", shift, correct.OffsetSec)
	}

	// The shifted window is exactly [since+offset, until+offset]; the Go-side
	// count over it must equal what SQL returned.
	nonNull := make([]int64, 0, len(epochs))
	for i, e := range epochs {
		if i%fixtureNullEvery != 0 {
			nonNull = append(nonNull, e)
		}
	}
	off := int64(correct.OffsetSec)
	wantShifted := countClaimedInRange(nonNull, correct.Since+off, correct.Until+off)
	if bareCount != int64(wantShifted) {
		t.Errorf("the modifier-less form selected %d rows, want %d (the Go arithmetic over the shifted window "+
			"[%d,%d])", bareCount, wantShifted, correct.Since+off, correct.Until+off)
	}

	// THE TRAP, stated as an assertion: the two spellings of the same window
	// disagree, and the correct one is the local-clock reading.
	got, err := CountInWindow(context.Background(), db, ColumnClaimedAt, w)
	if err != nil {
		t.Fatalf("count %s in %s: %v", ColumnClaimedAt, w.Name(), err)
	}
	want := countClaimedInRange(nonNull, correct.Since, correct.Until)
	if bareCount == got {
		t.Errorf("the bare and the 'utc' forms both selected %d rows over a %d-hour window on a host offset by "+
			"%d s — the divergence this test exists to prove is absent: the modifier has no effect here, so the "+
			"window is not being read through the host clock (is the modifier still in the predicate?)",
			got, fixtureHoursOfWindow, correct.OffsetSec)
	}
	if got != int64(want) {
		t.Errorf("the helper selected %d rows, want %d (the local-clock epoch bounds [%d,%d])",
			got, want, correct.Since, correct.Until)
	}
	t.Logf("red-proof on this host (offset %d s): 'utc' form = %d rows, bare form = %d rows — "+
		"the bare form's window is %d h later than the operator meant",
		correct.OffsetSec, got, bareCount, correct.OffsetSec/fixtureHourSec)
}

// bareBoundExpr renders one bound WITHOUT the 'utc' modifier — the pre-fix
// shape, built from the package's constants so this file carries no bare-form
// SQL literal of its own.
func bareBoundExpr() string {
	return FuncStrftime + OpenParenSQL + strings.Join([]string{FormatEpochSecondsSQL, PlaceholderSQL}, CommaSQL) +
		CloseParenSQL
}

// TestPredicateCarriesTheModifier is the fragment-shape check (supplementary to
// the window-result assertion above): both renderings of the predicate carry
// the modifier exactly twice — once per bound — and the epoch format once per
// bound, so a future edit that drops it from one shape is caught even in a
// package whose tests never query a database.
func TestPredicateCarriesTheModifier(t *testing.T) {
	for name, expr := range map[string]string{
		"Where":        Where(ColumnClaimedAt),
		"WhereLiteral": WhereLiteral(ColumnClaimedAt, fixtureWindow()),
		"CountQuery":   CountQuery(ColumnClaimedAt),
	} {
		if got := strings.Count(expr, UTCModifierSQL); got != predicateBoundCount {
			t.Errorf("%s carries the %s modifier %d time(s), want %d (one per bound) — %q",
				name, UTCModifierSQL, got, predicateBoundCount, expr)
		}
		if got := strings.Count(expr, FormatEpochSecondsSQL); got != predicateBoundCount {
			t.Errorf("%s carries the epoch format %q %d time(s), want %d — %q",
				name, FormatEpochSecondsSQL, got, predicateBoundCount, expr)
		}
		if !strings.Contains(expr, ColumnClaimedAt+SpaceSQL+BetweenKeyword) {
			t.Errorf("%s = %q, want it to carry the column and %s", name, expr, BetweenKeyword)
		}
	}
	if strings.Contains(bareBoundExpr(), UTCModifierSQL) {
		t.Fatalf("the test's own bare-form builder carries the modifier — the red-proof would not be a red-proof")
	}
}

// predicateBoundCount is how many bounds an interval predicate has.
const predicateBoundCount = 2

// TestWhereLiteralQuotesItsBounds pins the literal shape: the bounds are
// quoted SQL literals, and an embedded quote is doubled rather than escaping
// into the query.
func TestWhereLiteralQuotesItsBounds(t *testing.T) {
	expr := WhereLiteral(ColumnClaimedAt, Window{Since: fixtureWindowSince, Until: fixtureWindowUntil})
	for _, want := range []string{QuoteLiteral(fixtureWindowSince), QuoteLiteral(fixtureWindowUntil)} {
		if !strings.Contains(expr, want) {
			t.Errorf("%s does not carry the bound %s — %q", "WhereLiteral", want, expr)
		}
	}
	hostile := Window{Since: fixtureWindowSince, Until: "2026-09-17 05:00'; DROP TABLE " + TableTasks + "; --"}
	quoted := WhereLiteral(ColumnClaimedAt, hostile)
	if !strings.Contains(quoted, QuoteEscapeSQL) {
		t.Errorf("a bound carrying a quote is not escaped: %q", quoted)
	}
	if strings.Count(quoted, QuoteSQL)%2 != 0 {
		t.Errorf("the rendered predicate has an odd number of %s delimiters — a bound escaped out of its literal: %q",
			QuoteSQL, quoted)
	}
}

// TestInvalidWindowsAreErrorsNotEmptyResults: a window that cannot match any
// row must FAIL, not return 0. An empty result is what the trap looks like, so
// a window that is malformed for any other reason must never be reported as
// one.
func TestInvalidWindowsAreErrorsNotEmptyResults(t *testing.T) {
	db, _ := openFixtureDB(t)
	ctx := context.Background()

	if _, err := CountInWindow(ctx, db, ColumnClaimedAt, Window{
		Since: fixtureWindowUntil, Until: fixtureWindowSince,
	}); !errors.Is(err, ErrInvertedWindow) {
		t.Errorf("an inverted window reported %v, want %v", err, ErrInvertedWindow)
	}
	if _, err := ResolveBounds(ctx, db, Window{Since: "", Until: fixtureWindowUntil}); !errors.Is(err, ErrEmptyBound) {
		t.Errorf("an empty bound reported %v, want %v", err, ErrEmptyBound)
	}
	if _, err := ParseBound("last night"); err == nil {
		t.Errorf("ParseBound accepted a non-timestamp — a bound that cannot be read must not become a silent window")
	}

	// A window whose two bounds are equal is legal and selects the rows stamped
	// exactly at that minute (a minute-wide window is a legitimate question).
	one := Window{Since: fixtureWindowSince, Until: fixtureWindowSince}
	if err := one.Validate(); err != nil {
		t.Errorf("a single-minute window was rejected: %v", err)
	}
	bounds, err := ResolveBounds(ctx, db, one)
	if err != nil {
		t.Fatalf("resolve a single-minute window: %v", err)
	}
	if bounds.Until-bounds.Since != 0 {
		t.Errorf("a single-minute window spans %d s, want 0", bounds.Until-bounds.Since)
	}
}

// TestWindowNameIsTheOperatorsSpelling: the window's name is what a report
// quotes back, so it must be the wall clock the operator typed (not the epoch).
func TestWindowNameIsTheOperatorsSpelling(t *testing.T) {
	w := fixtureWindow()
	if got, want := w.Name(), fixtureWindowSince+" .. "+fixtureWindowUntil; got != want {
		t.Errorf("Window.Name() = %q, want %q", got, want)
	}
	if _, err := time.Parse(DateFormat, fixtureWindowSince); err != nil {
		t.Errorf("DateFormat %q does not parse the window spelling %q: %v", DateFormat, fixtureWindowSince, err)
	}
}

// TestCountQueryHasNoTimestampArithmetic pins the query's SHAPE: the ledger's
// columns are compared as the epoch numbers they are, so the query must contain
// no strftime other than through the named constant and must not decorate the
// column (a datetime(column,'localtime') comparison would be a second,
// divergent conversion).
func TestCountQueryHasNoTimestampArithmetic(t *testing.T) {
	q := CountQuery(ColumnClaimedAt)
	if !strings.HasPrefix(q, SelectCountPrefix) {
		t.Errorf("CountQuery = %q, want it to open with %q", q, SelectCountPrefix)
	}
	if strings.Count(q, FuncStrftime) != predicateBoundCount {
		t.Errorf("CountQuery carries %q %d time(s), want %d — %q", FuncStrftime, strings.Count(q, FuncStrftime), predicateBoundCount, q)
	}
	for _, banned := range []string{"datetime(", "unixepoch(", "localtime", "julianday"} {
		if strings.Contains(q, banned) {
			t.Errorf("CountQuery carries %q — the columns already hold epoch seconds; converting them again is the "+
				"second, divergent conversion this package exists to prevent: %q", banned, q)
		}
	}
}

// TestFixtureEpochsAreTheLocalReading is the sanity check on the fixture
// itself: ParseBound (Go, local zone) and the modifier-less SQL reading must
// differ by the offset, which is the premise every assertion above builds on.
func TestFixtureEpochsAreTheLocalReading(t *testing.T) {
	db, _ := openFixtureDB(t)
	var bare int64
	if err := db.QueryRow(selectKeyword+SpaceSQL+bareBoundExpr(), fixtureWindowSince).Scan(&bare); err != nil {
		t.Fatalf("read the modifier-less bound: %v", err)
	}
	local, err := ParseBound(fixtureWindowSince)
	if err != nil {
		t.Fatalf("parse %q: %v", fixtureWindowSince, err)
	}
	_, offset := time.Unix(local, 0).In(LocalZone).Zone()
	if diff := bare - local; diff != int64(offset) {
		t.Errorf("the modifier-less reading %d minus the local reading %d = %d, want the host offset %d — "+
			"the premise of this package (the modifier converts a wall clock to the local epoch) does not hold here",
			bare, local, diff, offset)
	}
	t.Logf("premise on this host: bare=%d local=%d offset=%d s", bare, local, offset)
}

// TestSelectCountPrefixIsATableQuery keeps the shared query's table name tied
// to the constant: the ledger's DDL creates this table, so a rename must be a
// compile-time edit here, not a query against a table that no longer exists.
func TestSelectCountPrefixIsATableQuery(t *testing.T) {
	if !strings.Contains(SelectCountPrefix, TableTasks) {
		t.Errorf("%q does not name the ledger table constant %q", SelectCountPrefix, TableTasks)
	}
	want := fmt.Sprintf("%s%s", SelectCountPrefix, Window{}.Since)
	if strings.Contains(want, strconvPlaceholderProbe()) {
		t.Errorf("the shared prefix leaks a probe placeholder: %q", want)
	}
}

// strconvPlaceholderProbe is a value that must never appear in the shared query
// prefix (a formatting verb would invite string-splicing a bound into SQL).
func strconvPlaceholderProbe() string { return "%s" }
