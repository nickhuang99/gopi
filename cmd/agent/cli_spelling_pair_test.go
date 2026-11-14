// cli_spelling_pair_test.go — the PAIR invariant behind the double-spelling
// slice of the verb-restructure epic (2026-10-05).
//
// THE RULE THIS PINS: a command concept has exactly ONE dispatched spelling, and
// the other spelling of the same concept is refused BY NAME. That is the
// operator's own wording (artifacts/verb-final-table-v2.md §三, absorbing Hermes
// C4): remove the DISPATCH side of a redundant spelling, never the GUARD side —
// a deleted guard line does not delete the word, it hands it to the bare-prompt
// path, where `gopi code_search` becomes a paid model call plus a session (the
// 2026-09-30 `buildinfo` accident, task 01a0f451…).
//
// WHY IT IS A TEST AND NOT A PROSE RULE: the vocabularies are four separate
// lists in four files, and a hand-typed pair table drifts from all of them the
// first time a verb moves. Every word below is READ FROM the table it lives in
// (Rule 13: the constant is the single source), so a new verb, a renamed verb or
// a newly-refused spelling is measured by the same code that measures the ones
// already shipped.
//
// The invariant has three arms:
//
//	(1) NO DOUBLE DISPATCH — no dispatched word has a dispatched twin, or the
//	    CLI teaches two spellings of one command.
//	(2) NO DANGEROUS REFUSAL — a word in either refusal table (a respelling in
//	    cli_undispatched_tool_words.go, a synonym in cli_verb_aliases.go) must
//	    (a) not dispatch, (b) be guarded in config.PositionalVerbs, so the
//	    refusal holds in BOTH argv positions — at argv[0] and after a run flag,
//	    and (c) if it IS a respelling, its twin must be the command it points at.
//
// A guarded word that neither dispatches nor is refused (the SYNONYM class,
// `buildinfo` → `version`) is arm (2)'s own business only in the sense that it
// must be listed in a refusal table — the bidirectional coverage of
// config.PositionalVerbs against the dispatch surface is
// TestPositionalVerbGuardVocabularyMatchesDispatch's, and this file does not
// restate it.
//
// KNOWN HOLE THIS FILE DOES NOT COVER, measured 2026-10-05: the GROUP verbs
// (`web search`, `web fetch`) refuse their flat spellings in
// cmd/agent/cli_websearch.go / cli_webfetch.go through refuseWebToolFlatVerb,
// and neither that word list nor its dash twins are visible to the three
// vocabularies above — so `gopi web-search` and `gopi web-fetch` still reach the
// run path (measured: the agent-path message, i.e. a paid model call once
// credentials are present). Filed as a bug task rather than fixed here; when the
// refusal arms land, they belong in a table this file can read, and these two
// arms become live coverage instead of a comment.
package main

import (
	"strings"
	"testing"

	"github.com/nickhuang99/gopi/internal/config"
)

// The two separators a spelling pair differs in. Named because they are the
// hinge the pair relation is defined on (Rule 1: a literal carrying meaning is a
// constant).
const (
	spellingPairDash       = "-"
	spellingPairUnderscore = "_"
)

// spellingTwin returns the other spelling of a word: the dash form for an
// underscored word, the underscored form for a dash word. A word carrying
// neither separator is its own twin, and every caller skips it.
func spellingTwin(word string) string {
	if strings.Contains(word, spellingPairUnderscore) {
		return strings.ReplaceAll(word, spellingPairUnderscore, spellingPairDash)
	}
	return strings.ReplaceAll(word, spellingPairDash, spellingPairUnderscore)
}

// TestVerbSpellingPairsHaveExactlyOneDispatchedSide pins arm (1): no concept is
// taught under two spellings at once.
func TestVerbSpellingPairsHaveExactlyOneDispatchedSide(t *testing.T) {
	dispatched := dispatchedVerbWords(t)
	for word := range dispatched {
		twin := spellingTwin(word)
		if twin == word || !dispatched[twin] {
			continue
		}
		t.Errorf("both %q and its respelling %q are dispatched — one command must have one spelling; keep the canonical word dispatched and refuse the other by name (cli_undispatched_tool_words.go)", word, twin)
	}
}

// TestRefusedSpellingsAreGuardedInBothArgvPositions pins arm (2): the two
// refusal tables and the run-path guard must agree about the same words, or the
// hole stays open in whichever argv position the operator happened to type.
func TestRefusedSpellingsAreGuardedInBothArgvPositions(t *testing.T) {
	dispatched := dispatchedVerbWords(t)
	guarded := map[string]bool{}
	for _, v := range config.PositionalVerbs {
		guarded[v] = true
	}

	refused := map[string]string{} // word → the table it came from
	for _, e := range undispatchedToolWords {
		refused[e.word] = "cli_undispatched_tool_words.go"
	}
	for _, a := range verbAliases {
		refused[a.alias] = "cli_verb_aliases.go"
	}

	for word, table := range refused {
		if dispatched[word] {
			t.Errorf("%s refuses %q, but this binary DISPATCHES it — the command would be unreachable", table, word)
		}
		if !guarded[word] {
			t.Errorf("%s refuses %q at argv[0], but %q is missing from config.PositionalVerbs — `gopi --timeout 3 %s` would still run it as a prompt", table, word, word, word)
			continue
		}
		// ARM (3), THE ADDRESS: the refusal must send the reader somewhere that
		// runs. For a respelling that is its twin; for the retired web flat
		// twins the twin IS the canonical word and is itself refused, so the
		// address is the LIVE home the row carries (`gopi tool web search`) —
		// which this helper requires to run. A row whose hint leads to another
		// refusal would be a dead end dressed as a fix (Rule 15).
		if !refusalAddressDispatches(t, refusalAddressOf(word)) {
			t.Errorf("%s refuses %q, but the address it prints (%q) is not a command line this binary runs — the refusal's hint leads nowhere", table, word, refusalAddressOf(word))
		}
	}
}
