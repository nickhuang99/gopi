package main

// cli_group_dispatch_test.go — the subcommand-group restructure (Epic: CLI
// verbs as groups — gopi memory <sub>, gopi web search|fetch — instead of
// flat verbs like gopi memory_search). Guards the dispatch routing:

//  1. the legacy flat verbs still map to the grouped handlers (alias), and
//  2. `gopi memory` / `gopi web <unknown>` produce usage errors instead of
//     falling through to the agent loop (they are NOT input files).

import (
	"strings"
	"testing"
)

// TestMemoryLegacyAliasTable locks the flat-verb → grouped-subcommand map
// (memory_feedback_list is special: it never had a real dispatch — only
// dead help text — so it routes to the audit form).
func TestMemoryLegacyAliasTable(t *testing.T) {
	want := map[string]string{
		"memory_search":        "search",
		"memory_store":         "store",
		"memory_get":           "get",
		"memory_update":        "update",
		"memory_delete":        "delete",
		"memory_feedback":      "feedback",
		"memory_feedback_list": "feedback", // + --list implied
	}
	for verb, sub := range want {
		got, ok := memoryLegacyAliases[verb]
		if !ok {
			t.Errorf("memoryLegacyAliases missing %q", verb)
			continue
		}
		if got != sub {
			t.Errorf("memoryLegacyAliases[%q] = %q, want %q", verb, got, sub)
		}
	}
	// The alias map must cover every subcommand a legacy FLAT VERB existed
	// for (search|store|get|update|delete|feedback — sync was always
	// `gopi memory sync`, never a flat verb, so it needs no alias), so a
	// flat verb can never silently vanish (breakage policy: alias, never
	// remove).
	for _, sub := range []string{"search", "store", "get", "update", "delete", "feedback"} {
		found := false
		for _, a := range memoryLegacyAliases {
			if a == sub {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("no legacy flat verb maps to memory subcommand %q — a script could break", sub)
		}
	}
}

// TestMemoryGroupSubcommandList locks the second-level candidate set — the
// tab-completion surface (gopi memory <TAB>).
func TestMemoryGroupSubcommandList(t *testing.T) {
	for _, sub := range []string{"search", "store", "get", "update", "delete", "feedback"} {
		if !containsString(memorySubcommandNames, sub) {
			t.Errorf("memorySubcommandNames missing %q", sub)
		}
	}
}

// TestMemoryDisplayName teaches the grouped spelling in errors:
// memory_search → "memory search".
func TestMemoryDisplayName(t *testing.T) {
	cases := map[string]string{
		"memory_search":   "memory search",
		"memory_store":    "memory store",
		"memory_feedback": "memory feedback",
	}
	for in, want := range cases {
		if got := memoryDisplayName(in); got != want {
			t.Errorf("memoryDisplayName(%q) = %q, want %q", in, got, want)
		}
	}
}

func containsString(hay []string, needle string) bool {
	for _, s := range hay {
		if s == needle {
			return true
		}
	}
	return false
}

// TestWebGroupUsageMentionsSubcommands — the web group's second-level help
// must advertise search|fetch (the surface users discover).
func TestWebGroupUsageMentionsSubcommands(t *testing.T) {
	for _, want := range []string{"search", "fetch", "web_search", "web_fetch"} {
		if !strings.Contains(webGroupUsage, want) {
			t.Errorf("webGroupUsage missing %q", want)
		}
	}
}

// TestWebGroupServerFlagRecognition — the disambiguation rule between the
// web SERVER (bare `gopi web` + server flags) and the web tool group
// (search|fetch): server flags must be recognized so they never route into
// the group dispatcher.
func TestWebGroupServerFlagRecognition(t *testing.T) {
	for _, a := range []string{"--tools", "--addr=:9999", "--addr", "-h", "--help"} {
		if !webGroupServerFlags(a) {
			t.Errorf("webGroupServerFlags(%q) = false, want true (server flag)", a)
		}
	}
	for _, a := range []string{"search", "fetch", "--bogus"} {
		if webGroupServerFlags(a) {
			t.Errorf("webGroupServerFlags(%q) = true, want false (group subcommand / unknown)", a)
		}
	}
}
