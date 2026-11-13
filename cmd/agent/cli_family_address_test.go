package main

// cli_family_address_test.go — the ONE place a test turns a RETIRED flat spelling
// into the family address that now dispatches it.
//
// WHY IT EXISTS (verb-retirement epic, 2026-10-05/06). The epic moved every
// tool-derived and session-derived command under its group (`gopi capture` →
// `gopi tool capture`, `gopi log` → `gopi session log`, `gopi memsync` →
// `gopi memory memsync`) and made the old flat word a NAMED REFUSAL at argv[0].
// A test that still drove the binary with the flat word therefore no longer
// tested the command: it observed the refusal, and every assertion about what
// the command DOES failed — measured at 6565aa25: 84 of the package's tests.
//
// WHAT IT FIXES AND WHAT IT DOES NOT. Only the DOOR — the argv[0] the test
// types. The assertions are untouched, which is the point: a test that reached
// `gopi capture -h` and asserted the capture usage page still asserts that page,
// now reached through `gopi tool capture -h`. Deleting an assertion would hide a
// behaviour; changing the door restores the behaviour to the assertion.
//
// IT IS DRIVEN BY PRODUCTION'S OWN TABLE (Rules 13/14). The mapping is not a
// second list typed here: verbAliasCanonical (cli_verb_aliases.go) is the TABLE
// the refusal itself renders from, so a word the binary refuses by name is a
// word this mapper forwards to the very address the refusal prints. A word the
// table does not know (an ordinary argv word, `-h`, a path) passes through
// UNCHANGED — the mapper never guesses, because a guess about what the operator
// (or the test author) meant is the class of bug cli_undispatched_tool_words.go
// exists to refuse.
//
// A HOMELESS ROW (`canonical == ""`) has no address to forward to and is left
// alone: the capability is gone, so a test driving it is testing the refusal
// itself, and that is a deliberate assertion to keep, not a door to move.

import (
	"strings"
	"testing"

	"github.com/nick/gopi/internal/testutil"
)

// runGopiMapped is testutil.RunGopi with the DOOR mapped: the argv[0] a test
// typed is translated to the family address that dispatches it
// (familyAddressArgs), every other word is untouched. It is what lets a test
// that was written before the verb-retirement epic keep asserting the behaviour
// it always asserted, through the spelling the binary now serves.
//
// It is a thin wrapper rather than a change inside testutil on purpose: RunGopi
// lives in an internal package every test in the repo uses, including tests that
// deliberately drive a RETIRED spelling to pin its refusal, so the mapping
// belongs on this package's side of the boundary — and only this package is
// having its test face migrated.
func runGopiMapped(t *testing.T, args []string, stdin string, env []string, dir string) (string, string, int) {
	t.Helper()
	return testutil.RunGopi(t, familyAddressArgs(args), stdin, env, dir)
}

// familyAddressArgs rewrites argv so its leading retired word is the family
// address that dispatches it. Subsequent words are untouched: only argv[0] is a
// door (`gopi log`, not `gopi session log`), and a second word is either a real
// subcommand (`list`, `--dir`) or the accidental spelling a refusal test is
// deliberately probing.
func familyAddressArgs(args []string) []string {
	if len(args) == 0 {
		return args
	}
	canonical := verbAliasCanonical(args[0])
	if canonical == "" {
		return args
	}
	out := append(strings.Split(canonical, " "), args[1:]...)
	return out
}
