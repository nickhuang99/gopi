package config

import (
	"fmt"

	"github.com/nickhuang99/gopi/internal/cliverbs"
)

// cli_leaf_verb_guard.go — the run path's LEAF-VERB refusal: one bare positional
// that is a FAMILY LEAF (`submit`, `list`, `show`, `update`, `next`, `clone`,
// `archive`) is refused instead of being spent as a prompt.
//
// THE DEFECT (task 01a1e5a2, operator's ruling 2026-11-17). A family's LEAF word
// — the second level of `gopi task submit` — is a word an operator types on its
// own all the time, and this binary's two other positional guards could not
// answer it:
//
//   - §1 (`PositionalVerbs`) holds FAMILY and TOP-LEVEL words only, and no leaf
//     word is one, so `gopi submit` matched nothing there;
//   - §1b (`commandHintWords`) is a CLOSED seven-word near-miss list that does
//     not contain any of them.
//
// So the word fell through to the bare-prompt path and bought a real agent run:
// a session directory, a paid model call and an `[auto] submit` ledger row.
// Measured in this profile's own ledger: 6 such rows (`[auto] submit`).
//
// THE THIRD SCREEN IS THE LENGTH FLOOR (§1d), AND IT IS NOT ENOUGH BY ITSELF.
// That guard refuses a prompt too SHORT to be a prompt — 1 token — which does
// catch `gopi submit` (1 token) but says nothing useful about WHICH command was
// meant, and catches nothing at all for the multi-token leaves: `gopi
// dedup-scan` (4 tokens), `gopi improvements` (2), `gopi sweep-worktrees` (6)
// were all measured reaching the run path on the built binary. Length is an axis
// the command line can decide; WORD OWNERSHIP is the other one, and for a word
// this binary really dispatches one level down it is both decisive and
// actionable — the refusal can name the command.
//
// THE SHAPE IS ONE POSITIONAL, DELIBERATELY. `cfg.Positionals` is the
// flag-value-aware list (Rule 2: the same resolution the run path uses), so a
// flag's VALUE can never be mistaken for the word. Exactly ONE positional means
// the operator typed a single word — `gopi submit` — never a sentence; a leaf
// word followed by anything at all (`gopi list the docs`) is prompt text and
// keeps running, which is the property that makes this guard safe to widen to
// the operator's own terminal (unlike §1c, which needs an agent signature).
//
// WHAT IT DOES NOT DO.
//   - It does not touch an EXPLICIT prompt channel (`--prompt X`, the settings
//     "prompt" key): those state that the text IS input, which is exactly the
//     answer the refusal hands the operator.
//   - It refuses no word outside the family-leaf table (cliverbs.FamilyLeaves),
//     so `gopi hello` keeps its prior verdicts and `gopi frobnicate` keeps
//     running as prompt text (Rule 15's mirror: an unknown word is not a guess).
//   - It refuses no word the dispatch chain already resolves: a word that
//     reaches this guard has already failed every isXxxSubcommand predicate, so
//     a family leaf that also dispatches at top level (`show` → the RETIRED
//     spelling's own pointer, `log` → `gopi session log`) never arrives here.

// leafVerbArity is the ONE arity this refusal answers: a single bare positional
// word. Named because it is the decision (Rule 1) — widening it to two words
// would start refusing sentences that open with a leaf word.
const leafVerbArity = 1

// LeafVerbRefusalFace is the surface name THIS refusal carries, and the one
// string that tells it apart from every other refusal the run path can answer
// the same argv with — §1d's length floor in particular, which stands nine
// screens later and answers a 1-token positional of its own (`gopi hello`).
//
// It is EXPORTED for the same reason config.ShortBarePromptFace is (Rule 14:
// one definition every consumer can import): the e2e acceptance tests live in
// cmd/agent, which is `package main` and can import internal/config but can
// never reach an unexported constant. A test that typed "LEAF VERB" inline
// would be asserting on a copy of the message, so a reworded refusal would
// leave the test green against text the binary no longer prints.
const LeafVerbRefusalFace = "LEAF VERB"

// leafVerbRefusalFmt is the refusal text. Every fact in it is measured elsewhere
// in this file: the word, the command it belongs to, that command's own help
// page, and the two ways to say the text was really input. The last line names
// the cost, because the whole reason the refusal exists is the cost.
//
// Args: (word, family+leaf address, family note, exit code).
const leafVerbRefusalFmt = "%q is a " + LeafVerbRefusalFace + " of a gopi family, not prompt text — a bare word on this\n" +
	"  face would start a REAL agent run (a session directory, an `[auto] %s` ledger row,\n" +
	"  a clone ground and paid model tokens), and that is not undone by killing it.\n" +
	"  The command you almost certainly meant is:\n" +
	"    %s -h        (%s)\n" +
	"  If the word really is your prompt, say so explicitly:\n" +
	"    %s --prompt %q\n" +
	"  Or write the request out as a sentence:\n" +
	"    %s %q\n" +
	"  %s%d — this refusal started nothing: no session, no ledger row, no ground, no tokens."

// leafVerbSentencePlaceholder is the shape the refusal shows for a request
// written out as one argument. It is a PLACEHOLDER, not the operator's word
// repeated: repeating a leaf word would hand back a line this same guard
// refuses if the operator typed only that (a self-pointing dead end, Rule 15).
const leafVerbSentencePlaceholder = "<the request, as a sentence>"

// PositionalCommandLeafRefusal is the run path's ONE call: it returns the
// refusal for a bare family-leaf invocation, or "" when the invocation is not
// that shape and must keep running.
func PositionalCommandLeafRefusal(cfg *Config) string { return leafVerbRefusal(cfg) }

// leafVerbRefusal builds the refusal (see PositionalCommandLeafRefusal).
func leafVerbRefusal(cfg *Config) string {
	if cfg == nil || promptInForce(cfg) {
		return ""
	}
	if len(cfg.Positionals) != leafVerbArity {
		return ""
	}
	word := cfg.Positionals[0]
	row := cliverbs.FamilyLeafWord(word)
	if row == nil {
		return ""
	}
	address := cliverbs.Program + " " + row.Family + " " + row.Leaf
	return fmt.Sprintf(leafVerbRefusalFmt,
		word, word,
		address, cliverbs.FamilyLeafNotes[row.Family],
		cliverbs.Program, word,
		cliverbs.Program, leafVerbSentencePlaceholder,
		AgentSpawnGuardExitCodeLabel, CLIUsageExitCode)
}
