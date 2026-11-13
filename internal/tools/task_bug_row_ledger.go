package tools

// The bug-row LEDGER LOCUS — where a "filed bug row" line actually landed.
//
// THE DEFECT THIS FILE EXISTS FOR (measured 2026-10-16, task 01a142da): a merger
// lane printed `filed bug row: CONFIG-ANCHOR gate: …` and an operator read that
// as "there is a queue item", ran `gopi task list`, got 0 hits, and had no way
// to tell THREE different worlds apart:
//
//	1. the write was lost (a real defect),
//	2. the write landed in a ledger this process resolved but the operator's
//	   CLI does not (the ordinary case inside a TEST: internal/testutil pins
//	   GOPI_MEMORY_DB to a per-test sandbox — Rule 32's whole reason),
//	3. the write landed in the operator's ledger and the reader looked with the
//	   wrong filter.
//
// The line was HONEST — TaskSubmit really did succeed — but it was not
// INFORMATIVE, and Rule 15 names exactly this shape: "found nothing" and "never
// scanned" must never look alike. A bare "filed" is the same ambiguity one level
// up: "filed" and "filed somewhere invisible" must not look alike either.
//
// RULE 2 — ONE READER OF THE RANKING. The locus derives the path from
// config.ResolveLedger, the SAME function the write itself resolves through
// (memoryDBPath → config.LedgerPath → ResolveLedger). It is not a second
// derivation: if a channel is added to the ranking, this line follows with no
// edit here, and it can never disagree with the file TaskSubmit opened. The
// CHANNEL it prints is the resolver's OWN source value — config.MemoryDBEnv,
// config.LedgerSettingsKey, config.LedgerSourceDefault … — so no channel name is
// spelled a second time in this package (Rule 1/13/14).
//
// It deliberately does NOT change the test-ledger pin (internal/testutil): the
// pin is correct and the fix is to SAY where the row went, not to move it.

import (
	"fmt"
	"strings"

	"github.com/nick/gopi/internal/config"
)

const (
	// bugRowLedgerLocusFmt renders the locus as a TRAILING CLAUSE — the filing
	// sites append it to their own line, so the summary stays greppable as one
	// family and the locus is unmistakably a property of the file, not of the
	// row.
	bugRowLedgerLocusFmt = "(ledger %s via %s)"

	// bugRowLedgerFallbackFmt is the last-resort shape: config.ResolveLedger
	// reports the built-in default with an EMPTY path when no profile home
	// resolves, and memoryDBPath then falls back to the CWD-relative file. A
	// reader has to see that, because that file is one directory away from
	// wherever the merger was standing — the worst possible place for a row to
	// be "filed" silently.
	bugRowLedgerFallbackFmt = "(ledger %s via %s — no profile home resolved, memoryDBPath's CWD-relative last resort)"
)

// bugRowLedgerLocus renders where THIS process's bug-row writes land: the
// resolved ledger path and the channel that decided it.
//
// It is cheap (a few getenv reads and at most one settings lookup) and it is
// called only on a filing that just happened, so it is not cached: a cached
// locus could name a ledger a later t.Setenv had already moved — the exact
// failure mode of printing a path nobody re-derived.
func bugRowLedgerLocus() string {
	path, source := config.ResolveLedger(config.LedgerNoFlag)
	if strings.TrimSpace(path) == "" {
		// The write did not land at a resolved path: mimic memoryDBPath's own
		// last resort so the line names the file TaskSubmit actually opened.
		return fmt.Sprintf(bugRowLedgerFallbackFmt, memoryDefaultDBFile, source)
	}
	return fmt.Sprintf(bugRowLedgerLocusFmt, path, source)
}
