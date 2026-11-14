package main

// cli_tool_spelling_table.go — the ONE table of tool-named command spellings,
// and the derivations every surface reads (epic spelling-table-source-of-truth,
// 2026-10-07).
//
// WHY THIS FILE EXISTS (the defect it removes). The rule
// (devdoc/cli-naming-convention.md) is short — a tool-DERIVED word keeps the
// tool's own underscore spelling, a gopi CLI verb is dash-cased — but the words
// it decides were written down THREE times, by hand, in three places:
//
//	the refusal vocabulary   cmd/agent/cli_undispatched_tool_words.go (word,
//	                         canonical, homeProg and a dash twin typed per row),
//	the exception set        cmd/agent/cli_completion_exceptions_test.go
//	                         (token + a hand-typed dashTwin string per row), and
//	the group's leaf list    cmd/agent/cli_tool.go (toolnames constants).
//
// A hand-typed twin is a second copy of a fact that is DERIVED
// (`keyword_search` → `keyword-search` is a substring), so the three copies can
// disagree, and they did: the group's refusal corrected an unknown word to
// `gopi <word>`, which for a tool WIRE name is itself refused — measured
// 2026-10-07 on the built binary: `gopi tool code_search -h` exits 1 and its
// stderr says "(or `gopi code_search` for the command's own page)", and
// `gopi code_search -h` exits 1 with `this one is spelled gopi tool code-search`.
// A hint that lands on another refusal is a dead end (Rule 15), and the epic's
// acceptance is exactly that no refusal may point at one.
//
// So the words live HERE, once, and every consumer PROJECTS from this table:
// there is no second list to keep in step (Rules 1/13/14). Two of the three
// consumers above are now derived; the third (the group's leaf list) reads the
// toolnames constants directly and is pinned against this table by
// the rc gate in cli_tool_spelling_rc_test.go.

import (
	"strings"

	"github.com/nickhuang99/gopi/internal/toolnames"
)

// toolSpellingRow is one TOOL-named surface of this CLI: the underscore spelling
// it answers to, the word that dispatches it, and the live command line that
// reaches it. Every field is a constant's value — never a typed literal — and
// the word the CLI REFUSES is DERIVED from the two spellings rather than
// stored, so a rename moves every surface at once.
type toolSpellingRow struct {
	// underscored is the tool-style underscore spelling of this surface: the
	// tool's WIRE name (internal/toolnames) for every genuinely tool-derived
	// word, or the cliVerbs constant for `ui_state`, the one tool-SHAPED reader
	// surface this CLI dispatches at top level. It is the spelling the naming
	// rule keeps (rule 1) and the key this whole file is organised by.
	underscored string
	// canonical is the word that DISPATCHES the surface (`code-search`,
	// `keyword_search`, `ui_state`), or — for the retired flat web words — the
	// word the refusal's corrected-line slot names (rule 4: a deprecated
	// spelling is frozen, never re-spelled). It is ALSO the address fragment
	// the refusal machinery renders (refusalAddress).
	canonical string
	// door is the WHOLE live command line that runs this surface, program word
	// included (`gopi tool keyword_search`, `gopi ui_state`). Assembled from
	// constants at the definition site (toolGroupProg / programAddress), never
	// typed: it is the string every refusal prints and the rc gate RUNS.
	door string
	// reason is why this spelling is the one and only one — carried at the code
	// site so the decision survives without the devdoc, and rendered by the
	// help-index exception test.
	reason string
}

// toolSpellingRows is the table. Order is the order refusals are checked in,
// and it is the SAME order the vocabulary has always had — one row per
// tool-shaped surface this CLI refuses a spelling of.
//
// The naming rule decides each row's shape, and the table makes the three cases
// readable at a glance:
//
//	canonical == underscored  → an EXCEPTION: the command keeps the tool's own
//	                            underscore spelling (rule 1), and its dash twin
//	                            is the refused word.
//	canonical != underscored  → rule 2: the CLI face is dash-cased, and the
//	                            tool's WIRE name is the refused word.
//
// Both cases are DERIVED by refusedSpelling, so neither is a hand-typed twin.
var toolSpellingRows = []toolSpellingRow{
	{
		// rule 2 — the code_search TOOL is exposed as the dash-cased CLI verb
		// `code-search`, so the WIRE name is the spelling refused.
		underscored: toolnames.ToolCodeSearch,
		canonical:   CliVerbCodeSearch,
		door:        toolGroupProg(CliVerbCodeSearch),
		reason:      "the code_search TOOL is exposed as `code-search`, a gopi CLI verb — gopi's own verbs are dash-cased",
	},
	{
		// rule 1 exception — a tool-derived command keeps the tool's own name.
		underscored: toolnames.ToolKeywordSearch,
		canonical:   toolnames.ToolKeywordSearch,
		door:        toolGroupProg(toolnames.ToolKeywordSearch),
		reason:      "a tool-derived command keeps the TOOL's underscore spelling (the documented exception)",
	},
	{
		underscored: toolnames.ToolVisionAnalyze,
		canonical:   toolnames.ToolVisionAnalyze,
		door:        toolGroupProg(toolnames.ToolVisionAnalyze),
		reason:      "a tool-derived command keeps the TOOL's underscore spelling (the documented exception)",
	},
	{
		underscored: toolnames.ToolScreenCapture,
		canonical:   toolnames.ToolScreenCapture,
		door:        toolGroupProg(toolnames.ToolScreenCapture),
		reason:      "a tool-derived command keeps the TOOL's underscore spelling (the documented exception)",
	},
	{
		underscored: toolnames.ToolVoiceInput,
		canonical:   toolnames.ToolVoiceInput,
		door:        toolGroupProg(toolnames.ToolVoiceInput),
		reason:      "a tool-derived command keeps the TOOL's underscore spelling (the documented exception)",
	},
	{
		// ui_state is the ONE tool-shaped reader that is still DISPATCHED at top
		// level (it reads a live chat screen, it is not a tool the agent calls),
		// so its door is the word itself and its OWNER is the cliVerbs block —
		// there is no toolnames constant for it, and inventing one would name a
		// tool that does not exist.
		underscored: CliVerbUIState,
		canonical:   CliVerbUIState,
		door:        programAddress(CliVerbUIState),
		reason:      "the tool-shaped reader surface is spelled with the underscore `ui_state`, never dash-cased",
	},
	{
		// The two rows below are the DASH TWINS of the retired web tool words,
		// and their `canonical` is the RETIRED flat word (rule 4: frozen) while
		// their door is the live group line — the address the refusal prints,
		// because a refusal pointing at another refusal is a dead end (Rule 15).
		// Measured reason this row exists at all: the dash-cased spelling was in
		// NO vocabulary, so `gopi web-search "<q>"` reached the agent loop and
		// bought a paid run (devdoc/web-tool-dash-twin-refusal.md).
		underscored: toolnames.ToolWebSearch,
		canonical:   toolnames.ToolWebSearch,
		door:        webSearchGroupProg,
		reason:      "the dash-cased flat spelling of the web_search tool names no command — the tool's live home is `gopi tool web search`",
	},
	{
		underscored: toolnames.ToolWebFetch,
		canonical:   toolnames.ToolWebFetch,
		door:        webFetchGroupProg,
		reason:      "same as its web search sibling: the dash twin of the retired flat word names no command — `gopi tool web fetch` is the live spelling",
	},
	// ---- rule 2, the MIRROR direction (task 01a1acc8…, 2026-11-06) -----------
	//
	// The three rows below are the rows this table was MISSING, and the hole
	// they left was a live one: a command whose CLI face is DASH-cased
	// (`ledger-window`, `scratch-gc`, `text2img-verify`) has an UNDERSCORE twin
	// (`ledger_window`, …), and until these rows existed that twin was in NO
	// vocabulary at all — not in this table, not in the synonym table, not in
	// internal/config's positional list. Measured on the built binary, argv
	// verbatim in an isolated GOPI_HOME:
	//
	//	gopi ledger_window -h      rc=0  "GoPi — a tool-using AI coding agent…"
	//	gopi text2img_verify -h    rc=0  "GoPi — a tool-using AI coding agent…"
	//	gopi ledger_window         rc=1  "Error creating agent: failed to create
	//	                                  DeepSeek Claude client: … ANTHROPIC_AUTH_TOKEN not set"
	//
	// — i.e. the word was spent as a PROMPT and the process walked into the RUN
	// path, where (with a key present) it buys a real model call for what the
	// operator typed as a command. The dash spelling of the same word IS
	// refused (`gopi ledger-window -h` rc=1, "is RETIRED — use `gopi tool
	// ledger-window`"), which is why the gap read as covered: the vocabulary
	// held the word the ROW is written with, and the row is written with the
	// DASH value.
	//
	// WHY THE ROWS ARE HERE AND NOT TYPED AS TWINS OF A LITERAL. Every field is
	// DERIVED: `underscored` is underscoreTwin of the cliVerbs constant (the
	// dash word the CLI really dispatches), so a rename of the command moves
	// this row with it and no hand-typed twin can appear; `door` is
	// toolGroupProg of the same constant, so the refusal can only ever point at
	// the spelling that RUNS (Rules 1/13/14). Nothing below repeats a word.
	//
	// THE GENERAL PROPERTY, pinned in cli_tool_spelling_table_test.go rather
	// than here: EVERY dash-cased word this table's doors dispatch carries its
	// underscore twin as a refused row. That is what makes this the last time
	// this defect can be found by hand — the next dash-cased command added
	// without its twin fails the suite instead of costing a model call.
	{
		underscored: underscoreTwin(CliVerbLedgerWindow),
		canonical:   CliVerbLedgerWindow,
		door:        toolGroupProg(CliVerbLedgerWindow),
		reason:      "the CLI face of the ledger-window tool is DASH-cased — the underscore twin names no command, and its live home is `gopi tool ledger-window`",
	},
	{
		underscored: underscoreTwin(CliVerbScratchGC),
		canonical:   CliVerbScratchGC,
		door:        toolGroupProg(CliVerbScratchGC),
		reason:      "the CLI face of the scratch-gc tool is DASH-cased — the underscore twin names no command, and its live home is `gopi tool scratch-gc`",
	},
	{
		// `screen-replay` is NOT a tool-group child — it is a TOP-LEVEL verb
		// (cli_screen_replay_verb.go) whose live home is `gopi screen-replay`,
		// the same shape the `ui_state` row above takes. Its underscore twin
		// still needs a refusal arm: this test derives the twin of every
		// dash-cased word in the top-level vocabulary, and a twin with no row
		// falls through to the run path as prompt text.
		underscored: underscoreTwin(CliVerbScreenReplay),
		canonical:   CliVerbScreenReplay,
		door:        programAddress(CliVerbScreenReplay),
		reason:      "the CLI face of this verb is DASH-cased — the underscore twin names no command, and its live home is `gopi screen-replay`",
	},
	{
		underscored: underscoreTwin(CliVerbText2imgVerify),
		canonical:   CliVerbText2imgVerify,
		door:        toolGroupProg(CliVerbText2imgVerify),
		reason:      "the CLI face of the text2img-verify tool is DASH-cased — the underscore twin names no command, and its live home is `gopi tool text2img-verify`",
	},
}

// refusedSpelling is the argv[0] word this row's surface REFUSES — the OTHER
// spelling of the same surface: whichever of the two a live command line is NOT.
//
// It is DERIVED, and that is the point (Rules 1/13/14): the dash twin is a
// substring transformation, so a hand-typed copy of it can only ever be a
// second, wrongable source. `code_search` resolves to the WIRE name (its dash
// twin IS the dispatched verb), every underscore exception resolves to its dash
// twin, and the two retired web words resolve to their dash twins as well.
func (r toolSpellingRow) refusedSpelling() string {
	if dashTwin(r.underscored) == r.canonical {
		return r.underscored
	}
	return dashTwin(r.underscored)
}

// underscoreException reports whether this row is a rule-1 EXCEPTION — a
// command that keeps the tool's own underscore spelling instead of the CLI's
// dash convention (`canonical == underscored`). The exception set is a
// PROJECTION of this table, never a list of its own: that is what
// cliNamingExceptions reads, and it is why the set cannot drift from the
// refusal table it belongs to.
func (r toolSpellingRow) underscoreException() bool {
	// A RETIRED row is never an exception, even when its two spellings happen
	// to be equal. The two web rows carry `canonical == underscored` because
	// rule 4 FREEZES a deprecated flat spelling — not because a command keeps
	// the tool's underscore convention. Their live home is the TOOL group
	// (`gopi tool web search`), which the top-level index advertises as ONE
	// composite row; that row spells the GROUP, so it cannot be the place the
	// exception gate looks for the token `web_search`. Reading the equality
	// alone made the gate demand a front-door spelling the operator's ruling
	// (2026-10-28) deliberately removed.
	//
	// The retirement is DERIVED from the address, never stored as a second
	// fact that could drift from it: a row is still dispatchable under its own
	// word exactly when its DOOR ENDS with that word (`gopi tool
	// keyword_search`, `gopi ui_state`). A door that ends somewhere else
	// (`gopi tool web search` for the word `web_search`) names a surface
	// reached at another address — the deprecated-spelling shape.
	return r.canonical == r.underscored &&
		strings.HasSuffix(strings.TrimSpace(r.door), " "+r.underscored)
}

// pageAddress is the command line a refusal sends the reader to for THIS
// spelling, and it is the whole reason the table exists: the address is the
// row's DOOR (a line this binary really runs — refusedSpelling's twin), never
// `gopi <the word the user typed>`. Before this table, the `tool` group's
// refusal printed the raw word, so a tool WIRE name was corrected into a second
// refusal (measured: `gopi tool code_search -h` → "or `gopi code_search` for the
// command's own page", which exits 1).
func (r toolSpellingRow) pageAddress() string { return r.door }

// toolSpellingRowForSpelling finds the row that REFUSES a given argv word, or
// false. It is the lookup that lets a refusal name the right door: a refusal
// must be able to answer "this word is a spelling of a command that lives
// elsewhere — where?" without carrying a second copy of the vocabulary.
func toolSpellingRowForSpelling(word string) (toolSpellingRow, bool) {
	for _, r := range toolSpellingRows {
		if r.refusedSpelling() == word {
			return r, true
		}
	}
	return toolSpellingRow{}, false
}

// toolRefusalPageWord is the WORD the `tool` group's refusal should name for
// the page of an unknown second-level word. It returns the row's door minus the
// program word (`tool code-search` for the wire name `code_search`, so the
// refusal's own `gopi %s` prefix renders a line that OPENS), and this group's
// OWN page when the word is no known spelling.
//
// The door is what makes the two arms agree: the argv[0] refusal prints the
// whole door ("this one is spelled `gopi tool code-search`"), and this one
// prints the same line after its own `gopi ` prefix.
//
// THE NO-ROW ANSWER IS A PAGE, NEVER THE ECHOED WORD (measured 2026-10-07): an
// unknown word has no page of its own — `gopi <word>` is not a refusal, it is
// the bare-prompt path, and `gopi tool nosuchword -h` printed exactly that
// recommendation, which then exited 0 and minted a session, one real agent run
// bought by a typo. Every address a refusal prints must be a PAGE (Rule 15 dead
// end) and must OPEN (Rule 31).
//
// THE SECOND TABLE IS CONSULTED TOO, for the same reason: the refusal vocabulary
// has two halves — the SPELLINGS table above (a respelling of a real command)
// and cli_verb_aliases.go's SYNONYMS (a word that MEANS a command without being
// a respelling of it). A synonym is not a spelling, so it arrives here with no
// row above, while the binary knows perfectly well where the reader was going:
// `gopi tool buildinfo` prints the page of `gopi version`. The read side is the
// alias table's OWN (verbAliasCanonical — the function the census gate asks),
// never a second list; an alias row with NO live home (a withdrawn capability)
// answers "" and therefore falls back to this group's page rather than to a
// bare `gopi `.
func toolRefusalPageWord(word string) string {
	if r, ok := toolSpellingRowForSpelling(word); ok {
		return strings.TrimPrefix(r.pageAddress(), gopiProgramWord+" ")
	}
	if home := verbAliasCanonical(word); home != "" {
		return home
	}
	return toolGroupOwnPageWord
}
