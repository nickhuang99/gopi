package main

// cli_memory_ledger_guard.go — the REACTION of `gopi memory …` to an explicitly
// named `--db` that is not there.
//
// THE DEFECT THIS CLOSES (measured 2026-11-06, before this file existed). Every
// read-only memory subcommand resolved an explicitly-named `--db` through the
// ordinary opener, which CREATES and initialises a missing knowledge base. So a
// typo'd path was indistinguishable from an empty knowledge base, and the
// read-only verb left a 128 KiB persistent artifact behind that nobody reclaims
// (Rule 35):
//
//	$ ./bin/gopi memory list --limit 2 --db /nonexistent/x.db
//	knowledge base is empty                       (exit 0)
//	$ ls -l /nonexistent/x.db
//	-rw-r--r-- … 131072 …                         ← created by a READ
//
// The same wrong answer arrived in a second wording from `get`
// (`memory_get: sql: no rows in result set`) — two phrasings, both pointing at
// the wrong cause. And the mirror-image mistake is just as costly: a WRITER verb
// that silently falls back to the DEFAULT ledger writes the row somewhere the
// caller never named (measured: a probe row landed in the operator's live
// 737 MiB ledger that way). One root cause: the resolution never tells the
// caller WHICH ledger it used and WHETHER that ledger was just created. So the
// guard has BOTH arms, and `empty` and `newly created` stop being the same
// shape (Rule 15).
//
// WHAT IS DELIBERATELY NOT TOUCHED (the epic's constraint). The resolution
// itself — tools.MemoryDBPath, config.ResolveLedger, OpenMemoryLedger — keeps
// its ranking (elsewhere it is CONFIG.md's registered behaviour, and other
// callers legitimately create a ledger). This file only decides what the CLI
// does when the caller EXPLICITLY named a path and the file is absent.

import (
	"errors"
	"fmt"
	"io/fs"
	"os"

	"github.com/nickhuang99/gopi/internal/tools"
)

// The static sentences are constants, not inline literals (Rule 1b/13): the
// refusal is asserted by a test and quoted by two help pages, so it has exactly
// one spelling.
const (
	// memoryLedgerActiveExistsSuffix / memoryLedgerActiveMissingSuffix report the
	// ACTIVE ledger's own state, so one message answers both halves of "which
	// file did you actually use, and is it real".
	memoryLedgerActiveExistsSuffix  = " (exists)"
	memoryLedgerActiveMissingSuffix = " (it does not exist either)"

	// memoryLedgerDefaultTag marks a ledger the caller did NOT name — the
	// mirror-image hazard: a writer that silently used the default.
	memoryLedgerDefaultTag = " (the DEFAULT ledger — no --db was given)"

	// memoryLedgerReadOnlyRefusalFmt is the read-only refusal. It names the
	// verb, the path the caller wrote, what that path's absence means, and the
	// active ledger; then it points at the two legal routes, because a refusal
	// without a route is how people learn to reach for a bypass (Rule 35).
	memoryLedgerReadOnlyRefusalFmt = "gopi %s: --db %s does not exist — this verb only READS the knowledge base, " +
		"so it will not create that file. A path that does not exist is NOT an empty knowledge base, " +
		"and refusing is the honest answer. The active ledger is %s. " +
		"Run the verb without --db to read the active ledger, or name a file that exists. " +
		"A WRITER verb (store/update/delete/feedback <id>) creates a ledger deliberately.\n"

	// memoryLedgerUninspectableRefusalFmt is the "I could not even look" arm:
	// stat failed for a reason that is NOT absence (a permission error, a path
	// component that is a file). It is refused rather than treated as present —
	// "unknown" must never read as "fine" (Rule 15).
	memoryLedgerUninspectableRefusalFmt = "gopi %s: --db %s could not be inspected: %v. " +
		"Refusing rather than guessing whether it exists. The active ledger is %s.\n"

	// memoryLedgerCreatedNoticeFmt is the WRITER arm: a writer is allowed to
	// create, and it says so instead of leaving "empty" and "brand new" to look
	// alike.
	memoryLedgerCreatedNoticeFmt = "gopi %s: created a new knowledge base at %s (it did not exist before this run)%s"
)

// memoryLedgerAbsent reports whether path names a file that is NOT there.
// A stat failure that is not absence is returned as an error and handled by the
// caller — never folded into "absent" or into "present".
func memoryLedgerAbsent(path string) (bool, error) {
	if _, err := os.Stat(path); err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return true, nil
		}
		return false, err
	}
	return false, nil
}

// memoryLedgerActiveDescription names the ledger this process WOULD use with no
// --db, and whether that file is there. It reports the resolved path rather
// than re-deriving the ranking (Rule 2).
func memoryLedgerActiveDescription() string {
	active := tools.MemoryDBPath()
	if active == "" {
		return "(unresolved)"
	}
	absent, err := memoryLedgerAbsent(active)
	switch {
	case err != nil:
		return active
	case absent:
		return active + memoryLedgerActiveMissingSuffix
	default:
		return active + memoryLedgerActiveExistsSuffix
	}
}

// memoryLedgerReadOnlyRefusal returns the refusal sentence for a READ-ONLY
// subcommand given an explicit --db, and "" when the verb may run: no flag at
// all (the caller asked for the active ledger, whatever it is), or a file that
// is really there.
func memoryLedgerReadOnlyRefusal(displayName, explicitDB string) string {
	if explicitDB == "" {
		return ""
	}
	absent, err := memoryLedgerAbsent(explicitDB)
	if err != nil {
		return fmt.Sprintf(memoryLedgerUninspectableRefusalFmt,
			displayName, explicitDB, err, memoryLedgerActiveDescription())
	}
	if !absent {
		return ""
	}
	return fmt.Sprintf(memoryLedgerReadOnlyRefusalFmt,
		displayName, explicitDB, memoryLedgerActiveDescription())
}

// memoryLedgerWriterNotice returns the sentence a WRITER verb prints after it
// created a ledger, and "" when the ledger was already there (or its state could
// not be read — the writer's own open reports that).
//
// The DEFAULT-LEDGER tag is decided by whether the caller passed --db at all,
// NOT by comparing paths: a caller who happens to type the ambient default path
// explicitly did name it, and a path comparison would tag his explicit choice
// "no --db was given" (the same defect this guard exists to stop, one layer
// down).
func memoryLedgerWriterNotice(displayName, explicitDB, effectiveDB string) string {
	absent, err := memoryLedgerAbsent(effectiveDB)
	if err != nil || !absent {
		return ""
	}
	tag := ""
	if explicitDB == "" {
		tag = memoryLedgerDefaultTag
	}
	return fmt.Sprintf(memoryLedgerCreatedNoticeFmt, displayName, effectiveDB, tag)
}

// memoryLedgerCreatingOp reports whether one dispatch of the grouped `memory`
// verb may CREATE the ledger. The distinction is the whole contract: a
// read-only op must refuse an absent explicit path, a creating op must not be
// blocked by it (only told, afterwards, that it created one).
//
// The modes are read off the parsed options, because `provenance` and
// `feedback` carry both halves under one word: `provenance --backfill` writes,
// `provenance` (shape/counts/latent…) only reports; `feedback <id> --useful`
// writes a vote, `feedback --list` only reports.
func memoryLedgerCreatingOp(name, mode string) bool {
	switch name {
	case "memory_store", "memory_update", "memory_delete":
		return true
	case memoryProvenanceOp:
		return mode == memoryProvenanceBackfillMode || mode == memoryProvenanceReclassifyMode
	case "memory_feedback":
		return mode == memoryFeedbackVoteMode
	}
	return false
}

// memoryLedgerEffectivePath is the ledger path a subcommand will actually use:
// the explicit flag when it was given, else the resolved active ledger.
func memoryLedgerEffectivePath(explicitDB string) string {
	if explicitDB != "" {
		return explicitDB
	}
	return tools.MemoryDBPath()
}
