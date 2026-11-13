package config

// memory_prefetch_help_test.go — the guard for the memory-prefetch knob's
// DISCOVERABILITY, the sibling of help_knob_discoverability_test.go's
// output-language instance and the front-door half of the pair whose other half
// is cmd/agent/chat_prefetch_help_test.go.
//
// THE DEFECT THIS FILE CLOSES (measured on `gopi -h` at this task's base commit
// 6c17d819). The knob shipped COMPLETE — three channels, a resolver, an executor, a record, its
// own tests, a catalog row in this very package — and `gopi -h`, the page a user
// reads first, mentioned it nowhere:
//
//	$ gopi -h 2>&1 | grep -i prefetch ; echo $?
//	1
//
// The same class as the reply-language defect that opened
// help_knob_discoverability_test.go: a knob can be real, documented in
// `gopi --help-env`, and still invisible on the front door. That file's own
// remedy — pin the INSTANCE byte-for-byte next to a whole-catalog discovery
// rule — is repeated here rather than relied on, because its discovery rule is
// keyed on the single word "language" and memory prefetch's catalog prose does
// not carry it.
//
// RED PROOF (Rule 10.4 / minimal sample): removing the optionRows entry this
// file's subject adds (`{"--" + MemoryPrefetchFlagName, MemoryPrefetchRowSummary}`
// in help.go) turns TestTopLevelHelpShowsMemoryPrefetch red, naming every
// missing spelling. A guard that passes on the un-fixed tree proves nothing —
// and this one was measured red that way before it was committed.

import (
	"strings"
	"testing"
)

// TestTopLevelHelpShowsMemoryPrefetch pins the INSTANCE: the typing-time memory
// prefetch must be visible on `gopi -h`, naming every channel a reader can use
// and stating the two facts that decide whether the command line they type will
// work at all — that the flag is a TOP-LEVEL one written BEFORE the verb, and
// that writing it bare means ON.
//
// The expected spellings are read from the knob's OWN constants (Rules 1b/13/14)
// — never re-typed — so a rename of the flag, the env var or the settings key
// moves the page and this assertion together, and the test cannot pin a spelling
// the hoist does not read.
func TestTopLevelHelpShowsMemoryPrefetch(t *testing.T) {
	usage := FormatUsage()
	flat := strings.Join(strings.Fields(usage), " ")

	for _, want := range []string{
		flagPrefix + MemoryPrefetchFlagName, // --memory-prefetch
		MemoryPrefetchEnv,                   // GOPI_MEMORY_PREFETCH
		MemoryPrefetchSettingsKey,           // memory_prefetch (the settings key)
		MemoryPrefetchVerbPositionClaim,     // the pre-verb constraint, ONE wording
		MemoryPrefetchInvocation,            // the copy-pasteable pre-verb example
	} {
		if !strings.Contains(flat, want) {
			t.Errorf("`gopi -h` does not show the memory-prefetch knob: missing %q — the knob exists in "+
				"three channels and a user cannot discover it from the front door (the defect this file "+
				"was written for; measured `gopi -h | grep -i prefetch` = 0 hits before the fix)", want)
		}
	}

	// The page must also say what the switch COSTS and which way it defaults:
	// "typing-time prefetch" alone reads as surveillance, and a reader who
	// cannot tell ON from OFF cannot decide whether he has to turn it off.
	for _, want := range []string{"READ-ONLY", MemoryPrefetchDefaultClaim} {
		if !strings.Contains(flat, want) {
			t.Errorf("`gopi -h` names the knob but not %q — the row must say what it does and which way it defaults", want)
		}
	}

	// The page's own claim must be TRUE of the parser: the flag it advertises is
	// accepted in the position it names. A page advertising a flag the parser
	// refuses is the worse half of the same defect class.
	if _, err := FlagOverrides([]string{flagPrefix + MemoryPrefetchFlagName, "version"}); err != nil {
		t.Errorf("`gopi -h` advertises the BARE pre-verb form %s but the parser refuses it: %v",
			MemoryPrefetchInvocation, err)
	}
}

// TestMemoryPrefetchHelpWordingHasOneDefinition is the anti-drift half, and it
// guards the SHARED wording rather than either page: the position constraint is
// printed by BOTH surfaces (`gopi -h` here and `gopi chat -h` in cmd/agent), so
// it exists as ONE constant and both compose it. A paraphrase on either page
// would be a second claim about the same command line, and the two would drift
// the first time one was edited.
//
// This test can only see the config side; the chat side is asserted by
// cmd/agent/chat_prefetch_help_test.go against this SAME constant, so the pair
// fails from whichever side stops using it.
func TestMemoryPrefetchHelpWordingHasOneDefinition(t *testing.T) {
	if MemoryPrefetchVerbPositionClaim != strings.TrimSpace(MemoryPrefetchVerbPositionClaim) {
		t.Errorf("the position claim carries leading/trailing space (%q) — a page composing it would print a ragged line",
			MemoryPrefetchVerbPositionClaim)
	}
	// The claim names the flag and the verb but must NOT hard-code either: it is
	// built from MemoryPrefetchFlagName and cliverbs.Chat, so a rename reaches
	// both pages without an edit here.
	if !strings.Contains(MemoryPrefetchVerbPositionClaim, "BEFORE") {
		t.Errorf("the position claim no longer states the constraint it exists for: %q", MemoryPrefetchVerbPositionClaim)
	}
	// The row summary is the page's own line and must carry the claim verbatim,
	// not a rewritten version of it.
	if !strings.Contains(MemoryPrefetchRowSummary, MemoryPrefetchVerbPositionClaim) {
		t.Errorf("MemoryPrefetchRowSummary does not carry MemoryPrefetchVerbPositionClaim verbatim — the front "+
			"door and the chat page would then state the position constraint in two different wordings:\n"+
			"  row:   %s\n  claim: %s", MemoryPrefetchRowSummary, MemoryPrefetchVerbPositionClaim)
	}
	// And it must stay within the front door's own budget: the page is an index
	// (cmd/agent's topLevelUsageMaxLines) and this row is rendered into a
	// wrapped table, so an unbounded summary is a budget defect, not a style one.
	if w := DisplayWidth(MemoryPrefetchRowSummary); w > helpRowSummaryMaxCols {
		t.Errorf("MemoryPrefetchRowSummary is %d display cols (budget %d) — the front door is an index; "+
			"move the detail to the chat page or CONFIG.md", w, helpRowSummaryMaxCols)
	}
}

// helpRowSummaryMaxCols bounds one Options row's summary. The renderer wraps it,
// so this is not a terminal-width limit: it is the point past which a single
// curated row has grown into the manual the front door's own budget exists to
// prevent (cmd/agent's topLevelUsageMaxLines caps the PAGE; this caps the row,
// so the failure names the row instead of the line count).
//
// MEASURED when the cap was set (Rule 38): the longest pre-existing summary is
// 287 cols (the chat ideal-geometry row, whose text is a constant for the same
// reason this one is), and the memory-prefetch row this file guards measures
// 246. The cap admits both with room for one more clause and refuses the next
// paragraph — a raise is a decision about what the row is FOR, not a drive-by.
const helpRowSummaryMaxCols = 300
