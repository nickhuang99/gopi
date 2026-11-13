// cli_memory_backup_test.go — the `gopi memory backup` interface guards
// (epic: ledger snapshot/ship, 2026-09-19).
//
// Two defects this file exists to prevent, both found while building the verb:
// a mistyped flag that the parser silently ignored (so `--no-shipp` took and
// SHIPPED a snapshot the operator never asked for), and a usage page that
// drifts from the flags the parser actually accepts.
package main

import (
	"regexp"
	"strings"
	"testing"

	"github.com/nick/gopi/internal/tools"
)

// TestMemoryFlagSetStrictModeRefusesAnUndeclaredFlag pins expect()'s contract:
// once a sub-verb declares its flags, an undeclared one is a hard error — and
// a caller that declares nothing keeps the permissive behaviour it always had
// (this is why strict mode is opt-in: ~30 existing callers rely on it).
func TestMemoryFlagSetStrictModeRefusesAnUndeclaredFlag(t *testing.T) {
	strict := newMemoryFlagSet()
	strict.boolean(memoryBackupNoShipFlag)
	strict.expect(memoryBackupNoShipFlag)
	strict.expect(memoryBackupDirFlag)

	if _, err := strict.parse([]string{"--no-shipp"}); err == nil {
		t.Fatal("strict mode accepted an undeclared flag — a typo would silently take the default path")
	}
	if _, err := strict.parse([]string{"--bogus"}); err == nil {
		t.Fatal("strict mode accepted an unknown flag")
	}
	positional, err := strict.parse([]string{"--dir", "/some/dir", "--no-ship"})
	if err != nil {
		t.Fatalf("declared flags were refused: %v", err)
	}
	if len(positional) != 0 {
		t.Fatalf("positional args = %v, want none", positional)
	}
	if got := strict.get(memoryBackupDirFlag); got != "/some/dir" {
		t.Errorf("--dir parsed as %q, want /some/dir", got)
	}
	if !strict.has(memoryBackupNoShipFlag) {
		t.Error("--no-ship was not recorded")
	}

	// Permissive (undeclared) mode is UNCHANGED for every existing caller.
	permissive := newMemoryFlagSet()
	if _, err := permissive.parse([]string{"--anything", "value"}); err != nil {
		t.Fatalf("permissive mode now refuses flags it used to accept: %v", err)
	}
}

// TestMemoryBackupUsageAndParserAgree is the interface gate: every flag the
// usage page documents must be accepted by the parser, and every flag the
// parser accepts must be documented. Without it, a flag can be real but
// undiscoverable, or documented but rejected at the moment it is typed.
func TestMemoryBackupUsageAndParserAgree(t *testing.T) {
	accepted := map[string]bool{
		tools.LedgerBackupServeFlag: true,
		memoryBackupNoShipFlag:      true,
		memoryBackupDirFlag:         true,
		memoryBackupDBFlag:          true,
		memoryBackupPeerFlag:        true,
		memoryBackupNameFlag:        true,
		"help":                      true, // -h/--help: the page every verb accepts
	}
	// Every --flag the usage page's FLAGS section offers must be accepted.
	// Scoped to that section on purpose: the OUTPUT and RESTORE sections name
	// other verbs' flags (`gopi memory list --limit 1`) as part of a command
	// example, and those are not this sub-verb's interface.
	flags := memoryBackupUsage
	if i := strings.Index(flags, "\nFLAGS\n"); i >= 0 {
		flags = flags[i:]
	} else {
		t.Fatal("the usage page lost its FLAGS section — the extraction cannot be scoped")
	}
	if j := strings.Index(flags, "\nRESTORE"); j >= 0 {
		flags = flags[:j]
	}
	mentioned := regexp.MustCompile(`--([a-z][a-z-]*)`).FindAllStringSubmatch(flags, -1)
	if len(mentioned) == 0 {
		t.Fatal("no flags found in the usage page's FLAGS section — the extraction or the page is broken")
	}
	for _, m := range mentioned {
		if !accepted[m[1]] {
			t.Errorf("the usage page documents --%s but the parser does not accept it", m[1])
		}
	}
	// Every accepted flag must be documented.
	for f := range accepted {
		if !strings.Contains(memoryBackupUsage, "--"+f) {
			t.Errorf("the parser accepts --%s but the usage page does not document it", f)
		}
	}
}

// TestIsMemoryBackupSubcommand pins the dispatch predicate, including the
// boundary that matters: `gopi memory backup -h` must reach the sub-verb (a
// predicate that also matched a bare `gopi memory` would steal the group page),
// and a longer word must not be mistaken for it.
func TestIsMemoryBackupSubcommand(t *testing.T) {
	yes := [][]string{
		{CliVerbMemory, tools.LedgerBackupVerb},
		{CliVerbMemory, tools.LedgerBackupVerb, "-h"},
		{CliVerbMemory, tools.LedgerBackupVerb, "--serve"},
		{CliVerbMemory, tools.LedgerBackupVerb, "--dir", "/x"},
	}
	for _, argv := range yes {
		if !isMemoryBackupSubcommand(argv) {
			t.Errorf("isMemoryBackupSubcommand(%v) = false, want true", argv)
		}
	}
	no := [][]string{
		{CliVerbMemory},
		{CliVerbMemory, "sync"},
		{CliVerbMemory, "list"},
		{tools.LedgerBackupVerb, CliVerbMemory},
		{CliVerbMemory, tools.LedgerBackupVerb + "s"},
	}
	for _, argv := range no {
		if isMemoryBackupSubcommand(argv) {
			t.Errorf("isMemoryBackupSubcommand(%v) = true, want false", argv)
		}
	}
}
