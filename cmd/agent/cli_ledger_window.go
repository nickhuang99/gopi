package main

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	_ "modernc.org/sqlite" // the repo's zero-CGO driver (Rule 7)

	"github.com/nickhuang99/gopi/internal/config"
	"github.com/nickhuang99/gopi/internal/ledgerwindow"
	"github.com/nickhuang99/gopi/internal/tools"
)

// cli_ledger_window.go — the `gopi tool ledger-window` verb, answering the ONE
// question a script surface needs about a wall-clock window: the epoch seconds
// of its two bounds, read through SQLite's date functions with the 'utc'
// modifier — the same predicate internal/ledgerwindow ships to Go callers.
//
// WHY A COMMAND AND NOT A SNIPPET. A shell surface (scripts/night-throughput.sh
// and anything like it) has to turn 'YYYY-MM-DD HH:MM' into epoch seconds before
// it can compare against the ledger's columns. Written inline, that conversion is
// the trap this package exists to close: the bare form silently shifts the window
// by the host's UTC offset. This verb is the shell-side entry point to the SAME
// definition — it prints the two numbers, so a script never types a date function
// at all.
//
// IT WAS `cmd/ledger-window`, A SECOND BINARY, UNTIL 2026-10-02. The reason it
// was a separate PROGRAM rather than a verb was never the program: ledger-window's
// own header said "this is a CLI verb on the existing binary surface, like `gopi
// shot` or `gopi tool scratch-gc`". It said verb and shipped binary. This file makes
// the shape match the sentence.
//
// Zero new top-level LLM tools (Rule 3): this is a CLI verb on the existing
// binary surface, not a tool the model calls.
//
// Output: one line, "<since>\t<until>\t<offset_seconds>" — tab-separated so
// `read s u off < <(gopi tool ledger-window ...)` works, and the third field is the
// host's UTC offset (the amount a missing modifier would shift the window by),
// reported so a surface can print the window's honesty rather than assume it.

const (
	// ledgerWindowUsage is the whole interface: two wall-clock bounds.
	ledgerWindowUsage = "usage: gopi tool ledger-window 'YYYY-MM-DD HH:MM' 'YYYY-MM-DD HH:MM'"
	// ledgerWindowOutSep separates the three reported fields.
	ledgerWindowOutSep = "\t"
	// ledgerWindowMemoryDBName is the ledger file's name under the gopi profile.
	ledgerWindowMemoryDBName = "memory.db"
	// ledgerWindowBusyTimeoutMS is how long a read waits for the live ledger's
	// writer. It is passed to internal/tools's ONE read-only DSN renderer rather
	// than to a format string of this file's own: the DSN's spelling (mode=ro +
	// query_only(true)) has a single definition, so `gopi tool ledger-window` cannot
	// drift from every other read-only opener in the tree (Rule 13/14).
	//
	// It is deliberately NOT tools.LedgerReadOnlyDSN's production default (15s):
	// this is a foreground shell surface whose answer is two integers, so it
	// GIVES UP and reports rather than parking a script for a quarter minute
	// behind a resident writer.
	ledgerWindowBusyTimeoutMS = 5000
	// ledgerWindowArgCount is the exact number of positional arguments expected.
	ledgerWindowArgCount = 2
	// ledgerWindowExitUsage / ledgerWindowExitError are the verb's two failure
	// statuses: a misuse (2) and a run that could not answer (1).
	ledgerWindowExitUsage = 2
	ledgerWindowExitError = 1
)

// envLedgerWindowMemoryDB names a database explicitly (the same knob the
// ledger's own CLI surfaces read).
const envLedgerWindowMemoryDB = "GOPI_MEMORY_DB"

// isLedgerWindowSubcommand is the argv predicate; like every other subcommand it
// consumes the rest of argv and handles its own exit.
func isLedgerWindowSubcommand(args []string) bool {
	if len(args) == 0 || args[0] != CliVerbLedgerWindow {
		return false
	}
	os.Exit(runLedgerWindowSubcommand(args[1:]))
	return true
}

func runLedgerWindowSubcommand(args []string) int {
	// -h is a HELP request, not a misuse: the usage line prints and the exit is
	// 0, so `gopi tool ledger-window -h` can be run inside a `set -e` script.
	if len(args) == 1 && (args[0] == "-h" || args[0] == "--help") {
		fmt.Println(ledgerWindowUsage)
		return 0
	}
	if len(args) != ledgerWindowArgCount {
		fmt.Fprintln(os.Stderr, ledgerWindowUsage)
		return ledgerWindowExitUsage
	}
	window := ledgerwindow.Window{Since: args[0], Until: args[1]}
	if err := window.Validate(); err != nil {
		fmt.Fprintf(os.Stderr, "gopi tool ledger-window: %v\n", err)
		return ledgerWindowExitUsage
	}

	path, err := ledgerWindowPath()
	if err != nil {
		fmt.Fprintf(os.Stderr, "gopi tool ledger-window: %v\n", err)
		return ledgerWindowExitError
	}
	db, err := sql.Open("sqlite", tools.LedgerReadOnlyDSNIn(path, ledgerWindowBusyTimeoutMS))
	if err != nil {
		fmt.Fprintf(os.Stderr, "gopi tool ledger-window: open %s: %v\n", path, err)
		return ledgerWindowExitError
	}
	// The handle is read-only and the process exits immediately after the single
	// query; a close error at that point has no verdict to change.
	defer func() { _ = db.Close() }() //stylegate:ignore read-only handle; the process exits after the query

	bounds, err := ledgerwindow.ResolveBounds(context.Background(), db, window)
	if err != nil {
		fmt.Fprintf(os.Stderr, "gopi tool ledger-window: %v\n", err)
		return ledgerWindowExitError
	}
	fmt.Printf("%d%s%d%s%d\n", bounds.Since, ledgerWindowOutSep, bounds.Until, ledgerWindowOutSep, bounds.OffsetSec)
	return 0
}

// ledgerWindowPath resolves the ledger under the gopi profile: $GOPI_MEMORY_DB
// names a database directly, otherwise <gopi-dir>/memory.db (config.GopiDir is
// the profile's single resolution point).
func ledgerWindowPath() (string, error) {
	if p := strings.TrimSpace(os.Getenv(envLedgerWindowMemoryDB)); p != "" {
		return p, nil
	}
	dir, err := config.GopiDir()
	if err != nil {
		return "", fmt.Errorf("resolve the gopi profile directory: %w", err)
	}
	return filepath.Join(dir, ledgerWindowMemoryDBName), nil
}
