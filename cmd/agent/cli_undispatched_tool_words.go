package main

// cli_undispatched_tool_words.go — the OTHER half of the naming rule
// (devdoc/cli-naming-convention.md): a tool-NAMED word that is deliberately
// NOT a command gets refused BY NAME, instead of being spent as a prompt.
//
// WHY (task 01a0dde8…, EUAUDIT slice 4 TOOLS-1 F3): the dispatch chain reads
// the verb from the FIRST argument; a word it does not recognize is not
// refused — it falls through to the bare-prompt path and buys a real agent
// run (a session directory, a live `[auto] <word> …` ledger row, tokens).
// Measured 2026-09-26: `gopi code_search EUAuditWidget` and
// `gopi tool keyword_search -h` were both consumed as prompts. `code_search` is
// the code_search TOOL's WIRE name; the CLI's own spelling for that command
// is the dash-cased `code-search` (rule 2 — the two must not be conflated, so
// `code_search` must NEVER become a second dispatched spelling of the same
// command). The correct shape is a refusal that points at the canonical verb.
//
// WHAT IS IN THE VOCABULARY (and, more importantly, what is NOT): exactly one
// row per SPELLING OF A REAL COMMAND THAT THIS CLI DELIBERATELY DOES NOT
// DISPATCH — the tool wire name where the command is dash-cased, and the
// dash-cased twin where the documented exception keeps the tool's underscore.
// Every other unknown word stays legitimate prompt text: `gopi frobnicate …`
// must keep running, exactly as today, because the refusal is a statement
// about THIS BINARY's own surface (a command exists, spelled differently),
// never a guess about what the operator meant. That is also why the row
// carries its address: a refusal whose hint points at nothing would be
// worse than the run it replaces (asserted by the contract tests).
//
// THE WORDS THEMSELVES ARE NOT HERE (spelling-table epic, 2026-10-07). This
// file is the REFUSAL MACHINERY; the vocabulary it walks is PROJECTED from
// cli_tool_spelling_table.go, because the same words were previously written
// down three times by hand (here, in the naming-exception set, and in the
// group's leaf list) and the copies disagreed — a hand-typed dash twin is a
// second copy of something derivable, and the `tool` group's refusal corrected
// a tool WIRE name into `gopi <wire name>`, which is itself refused. One table,
// every consumer projecting from it (Rules 1/13/14).
//
// THE OTHER HALF — SYNONYMS. The table above covers SPELLING; a word that
// MEANS a command without being a respelling of it (the `gopi buildinfo` →
// `gopi version` family) is the other half, and it lives in
// cli_verb_aliases.go's own authoritative table (task 01a0f451…, 2026-09-30:
// three `gopi buildinfo` invocations became three failed agent runs). The ONE
// refusal lookup below walks BOTH — so a word is refused in every argv
// position by construction rather than by keeping two lists in step, and
// internal/config's positional-verb guard reads the same union through
// undispatchedRefusedWordSpellings.
//
// WHERE IT RUNS: the dispatch chain in main() (the argv[0] shape), and —
// through internal/config's positional-verb guard, whose vocabulary carries
// the same words — the run path, which catches the word left over AFTER a run
// flag (`gopi --timeout 3 code_search`), a position the dispatch chain never
// reads.

import (
	"fmt"
	"os"
	"strings"

	"github.com/nick/gopi/internal/config"
)

// undispatchedToolWord is one spelling that names a REAL command without
// being its dispatched token, together with the token that does dispatch it.
type undispatchedToolWord struct {
	word      string // the spelling a user is likely to type
	canonical string // the command word that actually dispatches it (an address fragment; see door)
	reason    string // why this spelling is deliberately not a command
	// address is the FULL program spelling the refusal SENDS the reader to,
	// as projected from the spelling table's door: `gopi tool code-search`,
	// `gopi ui_state`, `gopi tool web search`. It is never `gopi <canonical>`
	// for a tool-derived row — the canonical word of those rows is a form of
	// the same command, and for the retired web pair the canonical word is
	// ITSELF refused, so the address must be the live home (Rule 15: a refusal
	// pointing at another refusal is a dead end).
	//
	// EMPTY means "this row has no home at all" (the withdrawn commands on the
	// SYNONYM table, e.g. `gopi ui` / `gopi cli`), and the caller then prints no
	// address rather than a bare `gopi `. One representation on both tables.
	address string
}

// undispatchedToolWords is the vocabulary, PROJECTED from the one spelling
// table (cli_tool_spelling_table.go). Rule 1/13/14: nothing here is typed —
// the word, its dash twin, the canonical fragment and the door all resolve
// through constants in internal/toolnames / the cliVerbs block, through the
// table's own derivations.
var undispatchedToolWords = projectedUndispatchedToolWords()

// projectedUndispatchedToolWords renders the spelling table as refusal rows.
// It is a PROJECTION in both directions at once: the word is the row's DERIVED
// refused spelling (so a hand-typed twin cannot exist), and the address is the
// row's door (so a refusal can never point at a spelling that is itself
// refused). Adding a surface to the table adds its refusal here.
func projectedUndispatchedToolWords() []undispatchedToolWord {
	out := make([]undispatchedToolWord, 0, len(toolSpellingRows))
	for _, r := range toolSpellingRows {
		out = append(out, undispatchedToolWord{
			word:      r.refusedSpelling(),
			canonical: r.canonical,
			reason:    r.reason,
			address:   r.pageAddress(),
		})
	}
	return out
}

// flatToolAddress is the SPLIT spelling a tool-derived command WOULD have if it
// were still a top-level verb (`code-search` for the code_search tool): the
// form the naming rule's rule 2 corrects a reader to — never a command any more
// since the verb-retirement epic, which is exactly why a refusal must print the
// whole surviving door instead. Derived from the tool name, never typed.
func flatToolAddress(tool string) string { return dashTwin(tool) }

// toolGroupProg is the SURVIVING address of a tool-derived command — the whole
// `gopi tool <word>` line. It is the only home those words have since the
// verb-retirement epic, and it is assembled here (one place) so a rename of the
// group word moves every row's hint at once (Rule 1b/13/14).
func toolGroupProg(word string) string { return gopiProgramWord + " " + CliVerbTool + " " + word }

// dashTwin returns the dash-cased normalisation of an underscore command word
// (`code_search` → `code-search`) — derived, never typed, so no surface can
// drift from the constants it names (Rule 13).
func dashTwin(word string) string {
	return strings.ReplaceAll(word, "_", "-")
}

// underscoreTwin is dashTwin's exact inverse, and it exists for the MIRROR
// direction of the same defect: a command whose CLI face is dash-cased
// (`ledger-window`) has an underscore spelling (`ledger_window`) that names no
// command, and that spelling must be refused BY NAME rather than spent as a
// prompt. The twin is DERIVED from the constant so no second literal can drift
// (Rules 1/13/14) — task 01a1acc8…, where the hole was measured.
func underscoreTwin(word string) string {
	return strings.ReplaceAll(word, "-", "_")
}

// undispatchedToolWordsSubcommand names the predicate the dispatch chain calls
// — and it is ALSO the census's exemption key, not a verb. A predicate that
// dispatches no verb (it refuses a spelling by name and exits) has no word for
// cmd/agent/cli_verb_census_test.go's table to carry, so the census exempts it
// by THIS name and checks that `func is<name>Subcommand` still exists in this
// file (an exemption that outlives its predicate fails the build).
const undispatchedToolWordsSubcommand = "UndispatchedToolWordSubcommand"

// undispatchedToolWordRefusalReason returns the pointed refusal for the word a
// command line begins with, or "" when that word is not in the vocabulary — an
// unknown word takes the ordinary path (this refuses a KNOWN spelling of a real
// command, never a guess about what the operator meant).
//
// IT WALKS BOTH HALVES OF THE VOCABULARY, in one place, on purpose: the
// SPELLINGS table above (a word that names a command under another spelling)
// and cli_verb_aliases.go's SYNONYM table (a word that MEANS a command without
// being a respelling of it). Keeping the union in the single lookup — rather
// than giving each table its own predicate — is what makes "every refused word
// is refused in every argv position" a structural property instead of two lists
// somebody must remember to keep in step; it is why the alias table needs no
// line in the dispatch chain.
func undispatchedToolWordRefusalReason(args []string) string {
	if len(args) == 0 {
		return ""
	}
	word := strings.TrimSpace(args[0])
	for _, e := range undispatchedToolWords {
		if e.word == word {
			return fmt.Sprintf(undispatchedToolWordRefusal,
				e.word, e.refusalAddress(), e.refusalAddress(), e.word, config.PromptEnvVar)
		}
	}
	return verbAliasRefusalReason(word)
}

// refusalAddress is the program spelling the refusal sends the reader to: the
// row's projected address, or `gopi <canonical>` when the row carries none —
// where <canonical> may be a MULTI-WORD command line (`gopi tool web search`),
// which is why the address is assembled from the field rather than assumed to
// be one word.
//
// It is DERIVED from the row rather than stored twice: a row cannot name an
// address in its message that differs from the one its field holds. The program
// word comes from gopiProgramWord (Rule 1b/13), never a literal typed here.
//
// An EMPTY address is the homeless row (`gopi ui`, `gopi cli`): those were
// top-level commands and are now refused with NO successor, so there is nothing
// to send the reader to and the caller must not print a bare `gopi `. The empty
// string is the ONE representation of that, on BOTH tables (the synonym table
// in cli_verb_aliases.go reads it the same way), so "no home" cannot be
// spelled two ways in two files.
func (e undispatchedToolWord) refusalAddress() string {
	if e.address != "" {
		return e.address
	}
	return programAddress(e.canonical)
}

// programAddress renders a command line for a refusal: the program word plus
// the row's canonical spelling, or "" when the row has no home at all. It is
// the ONE place a "gopi <address>" string is assembled for the refusal
// machinery — both tables read it, so a multi-level address can never be
// spelled two ways (and the two-word-message accident it prevents is the one
// the file header records: a home that IS two words must not be pushed through
// a prefix that doubles it or split into two independent words).
func programAddress(canonical string) string {
	if canonical == "" {
		return ""
	}
	return gopiProgramWord + " " + canonical
}

// undispatchedRefusedWordSpellings lists EVERY refused spelling — both halves of
// the vocabulary, in table order (the spellings table, then the synonyms). It is
// the ONE projection the guard mirror reads: internal/config's positional-verb
// guard takes its class-(b) words from here, so the run path's vocabulary and
// this package's refusals cannot drift (cmd/agent/cli_verb_guard_test.go asserts
// the two directions on the real binary).
func undispatchedRefusedWordSpellings() []string {
	out := undispatchedToolWordSpellings()
	return append(out, verbAliasWords()...)
}

// undispatchedToolWordSpellings lists the refused spellings, in table order.
// Callers that need the vocabulary as a SET walk this against a map they build
// once (there is no second copy of it to drift — Rules 13/14).
func undispatchedToolWordSpellings() []string {
	out := make([]string, 0, len(undispatchedToolWords))
	for _, e := range undispatchedToolWords {
		out = append(out, e.word)
	}
	return out
}

// undispatchedToolWordRefusal is the message for one refused word: it names
// the word, the command line the reader should use instead, and the ONE way to
// mean the word literally. A refusal that does not say how to proceed is a dead
// end (Rule 15), and a refusal that spends no run is the whole point of this
// file.
//
// BOTH ADDRESS SLOTS TAKE THE WHOLE COMMAND LINE (refusalAddress on the row), so a
// row whose live home is a group spelling prints that spelling VERBATIM rather
// than being pushed through a `gopi ` prefix that would double it
// (`gopi tool web search` was the measured shape of exactly that mistake).
// The program word is therefore part of the ADDRESS, derived once from
// gopiProgramWord in refusalAddress (Rule 1b/13).
// Args: (word, address, address, word, prompt env var).
const undispatchedToolWordRefusal = "gopi: %q is not a gopi command — this one is spelled `%s` on the command line " +
	"(`%s -h` for its help). To send the word as a prompt instead, say so explicitly: " +
	"`--prompt %s` (or the %s env var)."

// isUndispatchedToolWordSubcommand reports whether argv begins with a known
// not-dispatched spelling of a real command. When it does, it prints the
// pointed refusal on stderr and exits 1 — BEFORE any config resolution and
// BEFORE the run path, so the word can never be spent as a prompt.
//
// It classifies only the FIRST argument, and only when that argument IS the
// whole word: a prompt that merely contains one of these words is untouched.
func isUndispatchedToolWordSubcommand(args []string) bool {
	msg := undispatchedToolWordRefusalReason(args)
	if msg == "" {
		return false
	}
	fmt.Fprintln(os.Stderr, msg)
	os.Exit(1)
	return true
}
