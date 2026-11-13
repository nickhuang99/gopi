package main

// cli_completion_exceptions_test.go — the naming-rule exception discipline.
//
// The CLI's naming rule (devdoc/cli-naming-convention.md) is: a TOOL-named
// command keeps the tool's own underscore spelling (vision_analyze,
// screen_capture, voice_input), while gopi's own verbs are dash-cased
// (code-search, scratch-gc, audit-merges). The three underscore commands are
// therefore DELIBERATE exceptions, not leftovers — and an exception that
// nobody re-checks silently becomes either a stale exemption (the reason
// evaporated) or an unreachable command (dispatch moved on).
//
// This test is that re-check: it is the "safe-exceptions discipline" gate.
// Each exempt token must still (a) dispatch — spelled exactly as the user
// types it, (b) still be an UNDASHED spelling, i.e. the dash twin really is
// not the surface, and (c) still carry the toolnames constant that makes the
// underscore meaningful. Failing any of the three means the exemption must be
// deleted or the rule re-stated — never quietly ignored.

import (
	"strings"
	"testing"

	"github.com/nick/gopi/internal/toolnames"
)

// cliNamingExceptions are the tool-derived command words that keep the
// TOOL's underscore spelling instead of the CLI's dash convention. Each
// entry carries the reason the exception is legitimate (Rule: a documented
// exception, never an accident); the reason also lives in the top-level help.
//
// IT IS A PROJECTION, NOT A LIST (spelling-table epic, 2026-10-07). The words,
// the reasons and the dash twins used to be typed here a second time, next to
// the refusal table that carries the same facts — and a hand-typed dash twin is
// a copy of something DERIVED, which is what let the `tool` group's refusal
// correct a tool WIRE name into another refusal (measured: `gopi tool
// code_search -h` → "or `gopi code_search` for the command's own page", rc=1).
// The ONE spelling table (cli_tool_spelling_table.go) now owns the words; this
// is its rule-1 half, read through its own predicate, so an exception cannot
// drift from the refusal it belongs to (Rules 1/13/14).
var cliNamingExceptions = projectedNamingExceptions()

// projectedNamingExceptions renders the spelling table's EXCEPTIONS — the rows
// whose command keeps the tool's own underscore spelling — in table order.
func projectedNamingExceptions() []struct {
	token    string // the command word as the user types it
	toolname string // the internal/toolnames constant whose value it must BE
	reason   string // why the underscore survives here
	dashTwin string // the dash-cased spelling, which must NOT be a command
} {
	out := make([]struct {
		token    string
		toolname string
		reason   string
		dashTwin string
	}, 0, len(toolSpellingRows))
	for _, r := range toolSpellingRows {
		if !r.underscoreException() {
			continue
		}
		out = append(out, struct {
			token    string
			toolname string
			reason   string
			dashTwin string
		}{token: r.underscored, toolname: r.canonical, reason: r.reason, dashTwin: dashTwin(r.underscored)})
	}
	return out
}

// TestCLINamingExceptionsAreStillDispatchable — every exception must still be
// a DISPATCHED word (compared through the very constant the predicate uses)
// and must still differ from its dash twin: if the dash spelling were
// dispatched too, the underscore would no longer be an exception but one of
// two spellings — exactly the duplicate surface this epic removes.
func TestCLINamingExceptionsAreStillDispatchable(t *testing.T) {
	predicates := map[string]bool{
		CliVerbUIState:              cliContractInventoryToken(t, CliVerbUIState) != "",
		toolnames.ToolVisionAnalyze: cliContractInventoryToken(t, toolnames.ToolVisionAnalyze) != "",
		toolnames.ToolScreenCapture: cliContractInventoryToken(t, toolnames.ToolScreenCapture) != "",
		toolnames.ToolVoiceInput:    cliContractInventoryToken(t, toolnames.ToolVoiceInput) != "",
		toolnames.ToolKeywordSearch: cliContractInventoryToken(t, toolnames.ToolKeywordSearch) != "",
		// The two rows whose dash-cased flat spelling ALSO names no command —
		// the same class as the five above, and reachable through the same
		// inventory: `gopi tool web search` / `gopi tool web fetch` is the live
		// home (cli_tool_spelling_table.go). Without them the gate reports these
		// rows as "unreachable from the contract suite", i.e. as a hole in the
		// exception surface, which is a fact about THIS test's table and not
		// about the exceptions.
		toolnames.ToolWebSearch: cliContractInventoryToken(t, toolnames.ToolWebSearch) != "",
		toolnames.ToolWebFetch:  cliContractInventoryToken(t, toolnames.ToolWebFetch) != "",
	}
	for _, ex := range cliNamingExceptions {
		if ex.token == "" || ex.toolname == "" {
			t.Fatalf("exception entry %+v is empty — a blank token would make this gate vacuous", ex)
		}
		if ex.token != ex.toolname {
			t.Errorf("%q is not the toolnames constant value %q — the underscore spelling must BE the tool name", ex.token, ex.toolname)
		}
		if !predicates[ex.token] {
			t.Errorf("%q has no contract-inventory entry — the exception would be unreachable from the contract suite", ex.token)
		}
		if ex.dashTwin == ex.token {
			t.Errorf("%q has no dash twin by construction — drop it from the exception list", ex.token)
		}
		if !strings.EqualFold(strings.ReplaceAll(ex.token, "_", "-"), ex.dashTwin) {
			t.Errorf("%q: dash twin %q is not its dash-cased form", ex.token, ex.dashTwin)
		}
	}
}

// TestCLINamingExceptionsDocumentedInTopLevelHelp — the rule must be readable
// where users meet it: the top-level index states it, and every exception is
// named in that same index region. A rule nobody can read is not a rule.
//
// RE-ALIGNED by f8a35246 ("fix(master-reds): family A help-surface drift"):
// the sentinel below is graded as a SUBSTRING of the whole rendered page, so it
// must survive on ONE line — and the Commands paragraph in
// internal/config/help.go is a backtick LITERAL (not wrapText-wrapped), so the
// author's own hard line break decides which substrings exist. f8a35246 moved
// that break so `keeps the tool's own underscore spelling` reads as one
// substring again (it had been split across two rendered lines, which no
// rewording of the graded phrase could repair). The assertion is UNCHANGED,
// still graded as a substring, still graded against the real `usage` string.
func TestCLINamingExceptionsDocumentedInTopLevelHelp(t *testing.T) {
	for _, want := range []string{"keeps the tool's own underscore spelling", "devdoc/cli-naming-convention.md"} {
		if !strings.Contains(usage, want) {
			t.Errorf("top-level gopi -h does not state the naming rule (missing %q)", want)
		}
	}
	for _, ex := range cliNamingExceptions {
		if !strings.Contains(usage, ex.token) {
			t.Errorf("the naming rule is stated but %q (an exception) is not in the index", ex.token)
		}
	}
}

// cliContractInventoryToken returns the registered contract-inventory token
// that dispatches `word` — the word itself when it is an address of its own, or
// the FAMILY ADDRESS that now serves it when the retirement moved it under a
// group (`vision_analyze` → `tool vision_analyze`). "" when neither is
// registered — the reachability half of the exception gate.
//
// The family arm is what the retirement made necessary. Before it, an exception
// word was necessarily its own row; now three of the five are reached through
// the `tool` group, so a comparison against argv[0] alone reported them
// "unreachable from the contract suite" — FALSE about coverage, TRUE about the
// comparison. Reading the family row measures the row that really drives the
// word.
func cliContractInventoryToken(t *testing.T, word string) string {
	t.Helper()
	candidates := append([]string{word}, familyAddressArgs([]string{word})[0])
	for _, tc := range cliContractInventory {
		if len(tc.args) == 0 {
			continue
		}
		for _, want := range candidates {
			if tc.args[0] == want {
				return want
			}
		}
	}
	return ""
}
