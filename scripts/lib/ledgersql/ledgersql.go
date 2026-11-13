// Command ledgersql is the ledger access channel the scripts in this repository
// use INSTEAD OF the `sqlite3` command line — which is NOT installed on this
// host (recorded), and which this repo may not start depending on (Rule 35,
// zero dependency).
//
// It is deliberately tiny and shaped like the call it replaces:
//
//	ledgersql --db PATH "INSERT INTO tasks (…) VALUES (…)"
//	ledgersql --read-only --db PATH "SELECT group_concat(event,' ;; ') FROM task_events …"
//
// Rows are printed one per line, columns separated by a TAB, SQL NULL as the
// empty string; a statement that returns no rows prints nothing.
//
// EVERY FAILURE EXITS NON-ZERO with the driver's own message on stderr. A
// caller reading this must decide what to do about the failure rather than
// reading its silence as "no rows" — that distinction is the whole reason the
// helper exists (Rule 15): the script that came before it ran
// `sqlite3 "$DB" "SELECT …" 2>/dev/null`, so on a host without sqlite3 the
// trail came back EMPTY for every branch and the verdict said
// "non-ff-able-and-UNEXPLAINED", with the real cause visible only on the
// script's stdio and unrelated to its exit code.
//
// It is compiled on demand by the script that needs it, into that run's scratch
// root (Rule 22 — never the `/` partition), and it builds OFFLINE: sqlite is
// modernc.org/sqlite, already a module dependency, pure Go, no CGO.
package main

import (
	"database/sql"
	"flag"
	"fmt"
	"os"
	"strings"

	_ "modernc.org/sqlite" // database/sql driver registration (pure Go, no CGO)
)

const (
	// driverName is the database/sql driver modernc.org/sqlite registers.
	driverName = "sqlite"
	// busyTimeoutMs keeps a concurrent appender to the same ledger (a live gopi
	// process on the real profile) from surfacing as an immediate SQLITE_BUSY.
	busyTimeoutMs = 5000
	// columnSeparator is what a row's cells are joined with — a TAB, the same
	// shape `sqlite3 -separator $'\t'` gives the scripts that used it.
	columnSeparator = "\t"
	// exitFailure is what every error path returns; 2 = "could not run", which
	// is what a caller missing its SQL channel has to announce, not tolerate.
	exitFailure = 2
)

// queryPrefixes are the statement opening words that RETURN ROWS. Anything else
// is executed. Uppercased comparison, so the caller's case does not matter.
var queryPrefixes = []string{"SELECT", "WITH", "PRAGMA", "EXPLAIN", "VALUES"}

func main() {
	dbPath := flag.String("db", "", "the ledger file to open")
	readOnly := flag.Bool("read-only", false,
		"open mode=ro + query_only(true), so an observer cannot write the ledger it measures")
	flag.Parse()
	if err := run(*dbPath, *readOnly, flag.Args()); err != nil {
		fmt.Fprintf(os.Stderr, "ledgersql: %v\n", err)
		os.Exit(exitFailure)
	}
}

// run opens the ledger, runs ONE statement (the words after the flags, joined
// with spaces) and prints its rows. The deferred Close reports its own error
// only when nothing more important already failed.
func run(dbPath string, readOnly bool, words []string) (err error) {
	if dbPath == "" {
		return fmt.Errorf("--db PATH is required")
	}
	if len(words) == 0 {
		return fmt.Errorf("no SQL statement given")
	}
	statement := strings.Join(words, " ")

	db, err := sql.Open(driverName, dataSourceName(dbPath, readOnly))
	if err != nil {
		return fmt.Errorf("open %s: %w", dbPath, err)
	}
	defer func() {
		if closeErr := db.Close(); closeErr != nil && err == nil {
			err = fmt.Errorf("close %s: %w", dbPath, closeErr)
		}
	}()

	// Force the connection now: sql.Open is lazy, so without this a missing or
	// unreadable file would only surface at the first statement.
	if err = db.Ping(); err != nil {
		return fmt.Errorf("connect to %s: %w", dbPath, err)
	}
	if !returnsRows(statement) {
		if _, execErr := db.Exec(statement); execErr != nil {
			return fmt.Errorf("exec against %s: %w", dbPath, execErr)
		}
		return nil
	}
	return printRows(db, statement)
}

// printRows runs a row-returning statement and writes one line per row. The
// deferred Close reports its own error only when nothing more important already
// failed — a dropped error is a silent failure (Rule 15).
func printRows(db *sql.DB, statement string) (err error) {
	rows, err := db.Query(statement)
	if err != nil {
		return fmt.Errorf("query: %w", err)
	}
	defer func() {
		if closeErr := rows.Close(); closeErr != nil && err == nil {
			err = fmt.Errorf("close result set: %w", closeErr)
		}
	}()

	columns, err := rows.Columns()
	if err != nil {
		return fmt.Errorf("columns: %w", err)
	}
	cells := make([]any, len(columns))
	scanTargets := make([]any, len(columns))
	for i := range cells {
		scanTargets[i] = &cells[i]
	}
	for rows.Next() {
		if err := rows.Scan(scanTargets...); err != nil {
			return fmt.Errorf("scan: %w", err)
		}
		printed := make([]string, len(cells))
		for i, cell := range cells {
			printed[i] = cellText(cell)
		}
		fmt.Println(strings.Join(printed, columnSeparator))
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("read rows: %w", err)
	}
	return nil
}

// returnsRows reports whether the statement is one that produces a result set.
// A leading whitespace/comment run is skipped so "  SELECT …" is still a query.
func returnsRows(statement string) bool {
	upper := strings.ToUpper(strings.TrimSpace(statement))
	for _, prefix := range queryPrefixes {
		if strings.HasPrefix(upper, prefix) {
			return true
		}
	}
	return false
}

// cellText renders one scanned value the way the sqlite3 CLI printed it: NULL
// as the empty string, a blob as its bytes, everything else as its own text.
func cellText(cell any) string {
	switch typed := cell.(type) {
	case nil:
		return ""
	case []byte:
		return string(typed)
	case string:
		return typed
	default:
		return fmt.Sprint(typed)
	}
}

// dataSourceName builds the DSN. The read-only shape is the one that matters:
// mode=ro plus query_only(true) means the observer cannot write the ledger it
// is measuring, so a measurement bug cannot become a data bug.
func dataSourceName(dbPath string, readOnly bool) string {
	if readOnly {
		return fmt.Sprintf("file:%s?mode=ro&_pragma=query_only(true)&_pragma=busy_timeout(%d)",
			dbPath, busyTimeoutMs)
	}
	return fmt.Sprintf("file:%s?_pragma=busy_timeout(%d)", dbPath, busyTimeoutMs)
}
