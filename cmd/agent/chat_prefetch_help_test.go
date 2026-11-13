package main

// chat_prefetch_help_test.go — the chat-page half of the memory-prefetch
// discoverability guard. Its sibling is internal/config/memory_prefetch_help_test.go
// (the front door); this file asserts the verb's own page, from the LIVE binary,
// which is the surface the defect was measured on.
//
// THE DEFECT (measured before the fix, on this task's base commit 6c17d819):
//
//	$ gopi chat -h 2>&1 | grep -i prefetch ; echo $?
//	1
//
// The verb this feature exists FOR was the one page that could not tell a reader
// the feature existed. The knob had three channels, an executor, records, its own
// tests and a CONFIG.md row — everything except a sentence on the page a user
// opens before typing.
//
// WHY THE ASSERTION RUNS THE BINARY (Rule 31). A test over the usage CONSTANT
// would pass even if the constant stopped being joined into the page — which is
// precisely the shape this feature arrived in: chatGeometryUsage was joined in,
// chatPrefetchUsage was not in existence at all. helpPage spawns the real
// `gopi chat -h`, the same probe TestChatUsageStaysWithinItsLineBudget uses.

import (
	"strings"
	"testing"

	"github.com/nick/gopi/internal/config"
)

// TestChatHelpShowsMemoryPrefetch pins the instance on the page the user is
// looking at when the feature matters. The spellings come from the knob's own
// constants and from the INVOCATION constant the page composes (Rules 1b/13/14),
// so a rename of the flag reaches the page and this assertion together.
func TestChatHelpShowsMemoryPrefetch(t *testing.T) {
	out := helpPage(t, []string{CliVerbChat})
	if strings.TrimSpace(out) == "" {
		t.Fatalf("`gopi chat -h` printed nothing — a gate that measured nothing must not pass")
	}
	flat := strings.Join(strings.Fields(out), " ")

	for _, want := range []string{
		config.MemoryPrefetchInvocation,               // `--memory-prefetch chat` — the position, as an example
		config.MemoryPrefetchVerbPositionClaim,        // and the rule stated in words
		config.MemoryPrefetchEnv,                      // GOPI_MEMORY_PREFETCH
		config.MemoryPrefetchSettingsKey,              // memory_prefetch
		config.MemoryPrefetchRecordName,               // what the run leaves behind
		"--" + config.MemoryPrefetchFlagName + "=off", // the way to turn it off again
	} {
		if !strings.Contains(flat, want) {
			t.Errorf("`gopi chat -h` does not show the memory-prefetch knob: missing %q — the verb the knob "+
				"exists for must be able to tell a reader it exists (measured: 0 grep hits on this page "+
				"before the fix)", want)
		}
	}
	if !strings.Contains(flat, "BEFORE") {
		t.Errorf("`gopi chat -h` names the knob without saying the flag must come BEFORE the verb — a reader "+
			"would type `gopi chat --%s`, which this surface refuses by name", config.MemoryPrefetchFlagName)
	}
}

// TestChatHelpPrefetchTextIsNotAFlagDefinitionOfThisVerb pins the DERIVATION
// contract the section was written against. The help-surface sweep
// (scripts/g5-chat-web-help-surface.sh, mirrored in-process by the G5 inventory
// gate) reads three line shapes as "this verb advertises this flag": a synopsis
// HEAD, a bracket CONTINUATION, and a flag DEFINITION at two-space indent. This
// knob is a TOP-LEVEL flag that `gopi chat` REFUSES after the verb, so a
// definition row for it would make the page advertise a flag the verb rejects —
// a real defect, and the reason the section is prose.
//
// The assertion is written against the LIVE page rather than the constant,
// because the derivation reads the page. The knob's own name is checked by the
// sweep's own rules, one shape at a time.
func TestChatHelpPrefetchTextIsNotAFlagDefinitionOfThisVerb(t *testing.T) {
	out := helpPage(t, []string{CliVerbChat})
	lines := strings.Split(out, "\n")

	// The section's lines are found by the knob's own spelling, so this test
	// follows a rename (and fails loudly if the section vanishes, rather than
	// scanning nothing: the floor below).
	var section []string
	for _, ln := range lines {
		if strings.Contains(ln, config.MemoryPrefetchFlagName) {
			section = append(section, ln)
		}
	}
	if len(section) == 0 {
		t.Fatalf("no line of `gopi chat -h` names %s — the sweep below would scan nothing (the knob's own "+
			"section is what carries it)", config.MemoryPrefetchFlagName)
	}
	for _, ln := range section {
		// Shape 1: a flag DEFINITION at this page's two-space indent, which the
		// derivation collects as an advertised flag of THIS verb.
		if strings.HasPrefix(ln, "  --") {
			t.Errorf("the prefetch section carries a flag-definition line (%q) — the sweep would count it as "+
				"a flag `gopi chat` advertises, and this verb REFUSES the flag after the verb", ln)
		}
		// Shape 2: a synopsis HEAD at the same indent, which would advertise the
		// form as runnable after the verb.
		if strings.HasPrefix(ln, "  gopi ") {
			t.Errorf("the prefetch section carries a synopsis head (%q) — it would be read as a form this verb "+
				"accepts, which is exactly what the position constraint says it is not", ln)
		}
	}
}
