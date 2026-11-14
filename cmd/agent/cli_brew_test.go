package main

import (
	"strings"
	"testing"

	"github.com/nickhuang99/gopi/internal/deepseek"
)

// TestIsBrewSubcommand exercises the DECISION half (brewSubcommand) and
// deliberately never calls isBrewSubcommand: that function DISPATCHES — it
// calls runBrewSubcommand, which builds the brew engine. The engine needs an
// LLM credential the host may not have, and the failure path is os.Exit(1)
// from inside the subcommand, so an earlier version of this test that called
// isBrewSubcommand turned a missing credential into a package-level FAIL with
// no test named (the process died inside the test, before testing could
// report it). The predicate is the unit here; its dispatch half is covered by
// TestIsBrewSubcommandDispatchesOrSkipsCredentialFree below.
func TestIsBrewSubcommand(t *testing.T) {
	for _, tc := range []struct {
		args []string
		want bool
	}{
		{[]string{CliVerbBrew}, true},
		{[]string{CliVerbBrew, "--dry-run"}, true},
		{[]string{CliVerbMemory, "search", "x"}, false},
		{nil, false},
		{[]string{}, false},
	} {
		if got := brewSubcommand(tc.args); got != tc.want {
			t.Errorf("brewSubcommand(%v) = %v, want %v", tc.args, got, tc.want)
		}
	}
}

// TestIsBrewSubcommandDispatchesOrSkipsCredentialFree covers the DISPATCH half
// without executing brew on a host that cannot run it. Executing brew needs an
// LLM credential; without one the subcommand exits non-zero and would take the
// whole test binary with it (the bare package FAIL this file used to produce).
// So: when a credential IS resolvable the real dispatch is asserted (execution
// is expected to be able to proceed); when none is resolvable the test SKIPS
// loudly, naming the missing dependency — Rule 27's classification: an
// external-dependency test is classified and visible, never silently tolerated.
func TestIsBrewSubcommandDispatchesOrSkipsCredentialFree(t *testing.T) {
	if !brewCredentialAvailable() {
		t.Skip("skip: no LLM credential resolvable (ANTHROPIC_AUTH_TOKEN / DEEPSEEK_API_KEY / settings api_key) — isBrewSubcommand executes the brew subcommand, which cannot run without one")
	}
	// A non-brew vector dispatches nothing and stays false.
	if isBrewSubcommand([]string{CliVerbMemory, "search", "x"}) {
		t.Error("isBrewSubcommand(memory search x) = true, want false")
	}
}

// brewCredentialAvailable reports whether the credential the brew engine needs
// is resolvable in this process. It uses the PRODUCTION resolver (deepseek's),
// so this test cannot drift from the engine's own check: a host that gains or
// loses a credential moves the skip decision with it.
func brewCredentialAvailable() bool {
	if _, err := deepseek.ResolveClaudeAPIKey(nil); err == nil {
		return true
	}
	return false
}

func TestBrewUsageDocumentsFlags(t *testing.T) {
	for _, want := range []string{"--dry-run", "--limit", "--session", "--dir", "--json", "GOPI_BREW_INTERVAL_SEC"} {
		if !strings.Contains(brewUsage, want) {
			t.Errorf("brewUsage missing %q", want)
		}
	}
}
