package main

// chat_usage_width_test.go — the wrap-budget gate for `gopi chat -h`'s own page.
//
// WHY IT IS HERE AND NOT IN THE PACKAGE SWEEP. help_width_test.go walks help
// CONSTANTS; chatUsage is a VAR (its index is padded and its sections are
// joined), so the sweep cannot see it — and for a long time that was the
// documented reason its long lines stayed: TestChatScopedPagesFitTheWrapBudget
// carried a comment saying chatUsage "is deliberately NOT here: … ten prose rows
// run past the column. Fixing them is its own task". This is that task.
//
// Measured at 8c967277 (the commit before the fix): ten lines ran past
// config.HelpWrapCol, the widest prose row 114 columns. After it: none, except
// the deliberate SYNOPSIS line, which is a command-line shape and is exempted BY
// NAME below rather than by a marker that could hide the next one.

import (
	"strings"
	"testing"

	"github.com/nickhuang99/gopi/internal/config"
)

// chatUsageSynopsisPrefix begins the ONE line of the page allowed past the wrap
// budget: the `Usage:` synopsis is a command-line SHAPE, and breaking it across
// lines would misrepresent the syntax it exists to show.
const chatUsageSynopsisPrefix = "Usage: gopi chat"

// TestChatUsagePageFitsTheWrapBudget is the measurement the sweep cannot make.
// It walks chatUsage line by line with the SAME measure the help guard uses
// (config.DisplayWidth — a CJK glyph counts two cells), names every offender with
// its column count, and refuses a page that lost its synopsis (the exempted line
// must exist, or the exemption is hiding a restructuring nobody measured).
func TestChatUsagePageFitsTheWrapBudget(t *testing.T) {
	lines := strings.Split(chatUsage, "\n")
	synopsis := 0
	for i, line := range lines {
		if strings.HasPrefix(line, chatUsageSynopsisPrefix) {
			synopsis++
			continue
		}
		if w := config.DisplayWidth(line); w > config.HelpWrapCol {
			t.Errorf("gopi chat -h line %d is %d display cols (> %d): %q",
				i+1, w, config.HelpWrapCol, line)
		}
	}
	if synopsis != 1 {
		t.Fatalf("the page carries %d %q line(s), want exactly 1 — the exemption above is hiding a page that was restructured without measuring it",
			synopsis, chatUsageSynopsisPrefix)
	}
}

// TestChatUsageRelistsEveryFlagItAdvertises is the second half of the same
// defect, one level down: the SYNOPSIS and the REFUSAL list must both admit
// every flag this verb parses.
//
// MEASURED DEFECT THIS CLOSES (2026-10-11): `--sgr-stream`,
// `--ambiguous-width` and `--chat-surface-turns` were advertised in the usage
// line, parsed by this verb, and ABSENT from chatREPLFlags() — so
// `gopi chat --sgr-stream=x -h` was refused by the verb that defines the flag,
// and the refusal's own vocabulary described a command line that could not arm
// the recording. The page and the parser are two surfaces of ONE vocabulary;
// this test is the check.
func TestChatUsageRelistsEveryFlagItAdvertises(t *testing.T) {
	listed := chatREPLFlags()
	for _, flag := range []string{chatSgrStreamFlag, chatAmbiguousWidthFlag, chatSurfaceTurnsFlag} {
		found := false
		for _, l := range listed {
			if l == flag {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("%s is parsed by this verb and printed in its usage line, but is NOT in chatREPLFlags() — every unknown-flag refusal would omit it", flag)
			continue
		}
		if !strings.Contains(chatUsageLine, flag) {
			t.Errorf("%s is missing from chatUsageLine — the one-line usage does not admit the flag exists", flag)
		}
	}
}
