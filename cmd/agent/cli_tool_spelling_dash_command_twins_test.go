package main

// cli_tool_spelling_dash_command_twins_test.go — the MIRROR direction of the
// spelling table, and the defect it was missing (epic 01a1acc8…, 2026-11-06).
//
// THE DEFECT, measured on the built binary before the fix. A command whose CLI
// face is DASH-cased (`ledger-window`, `scratch-gc`, `text2img-verify`) has an
// UNDERSCORE spelling (`ledger_window`, …), and that spelling was in NO
// vocabulary at all: not the spelling table, not the synonym table, not
// internal/config's positional list. argv verbatim, in an isolated GOPI_HOME:
//
//	gopi ledger_window -h      rc=0  "GoPi — a tool-using AI coding agent…"
//	gopi text2img_verify -h    rc=0  "GoPi — a tool-using AI coding agent…"
//	gopi ledger_window         rc=1  "Error creating agent: failed to create
//	                                  DeepSeek Claude client: … not set"
//
// i.e. the word was spent as a PROMPT and the process walked into the RUN path,
// where with a key present it buys a real model call for something the operator
// typed as a command. The DASH spelling of the same word was refused, and that
// is exactly why the hole read as covered: the vocabulary held the spelling the
// ROW is written with, and rows are written with the dash value.
//
// WHAT THIS FILE PINS, as a PROPERTY over the vocabulary rather than over the
// three rows the fix added — so the next dash-cased command added without its
// twin fails here instead of costing a model call:
//
//	for every word `gopi tool <TAB>` offers that carries a dash separator,
//	its underscore twin is
//	  (a) present in the refused vocabulary (an address exists), and
//	  (b) refused by the real binary at argv[0] (rc != 0, a refusal line, no
//	      run path, nothing minted), and
//	  (c) refused after a run flag too (the same bug in the other argv order).
//
// Every arm runs the BINARY with argv exactly as written (runCLIContractProbeRaw
// — Rule 31: a surface in no execution has not been tested), and every call's
// input is DERIVED from the command's own constant, so no hand-typed twin can
// appear in this file either (Rules 1/13/14).

import (
	"strings"
	"testing"
)

// twinCandidateWords returns every word this binary DISPATCHES that carries a
// separator — the words whose other spelling must therefore be refused. The
// source is the dispatch surface itself (topLevelCommandNames ∪
// toolCompletionNames), never a list kept in step by hand, which is what makes
// this a PROPERTY rather than an enumeration of the three words this epic
// fixed: a command added tomorrow joins it the moment it dispatches, in either
// spelling direction.
//
// THE COVERAGE CLAIM, measured rather than assumed: on this rev every dash-cased
// top-level word is also a `tool` group word (the group is where the dash-cased
// commands live now — `code-search`, `ledger-window`, `scratch-gc`,
// `text2img-verify`), so walking the union walks everything. The arm below
// asserts the union is non-empty; a rev where it shrinks to nothing fails
// instead of passing vacuously.
func twinCandidateWords() []string {
	seen := map[string]bool{}
	var out []string
	add := func(words []string) {
		for _, w := range words {
			if seen[w] || !strings.ContainsAny(w, "-_") {
				continue
			}
			seen[w] = true
			out = append(out, w)
		}
	}
	add(topLevelCommandNames)
	add(toolCompletionNames)
	return out
}

// spellingRowDoor is the live command line this table's own row carries for a
// dash-cased word — the ONE source of "where does this spelling actually run",
// which the refusal is then held against.
func spellingRowDoor(word string) string {
	for _, r := range toolSpellingRows {
		if r.canonical == word {
			return r.door
		}
	}
	return toolGroupProg(word)
}

// TestEveryDashCasedCommandWordRefusesItsUnderscoreTwin is the gate. It walks
// the group's dash-cased words and asserts all three arms above; the floor arm
// refuses an empty walk, because a scan that reached nothing must not read as a
// clean vocabulary (Rule 15 — the same reason the corpus gates count their
// files).
func TestEverySeparatedCommandWordRefusesItsOtherSpelling(t *testing.T) {
	bin := gopiTestBinPath(t)
	home := t.TempDir()

	words := twinCandidateWords()
	if len(words) == 0 {
		t.Fatalf("the walk reached no separator-carrying command word — the dispatch vocabulary is empty or truncated, and a vacuous pass would hide the hole this test exists for")
	}

	refused := map[string]bool{}
	for _, w := range undispatchedRefusedWordSpellings() {
		refused[w] = true
	}

	for _, word := range words {
		twin := underscoreTwin(word)
		if twin == word {
			continue // no separator to flip — not this property's business
		}
		t.Run(twin, func(t *testing.T) {
			// (a) VOCABULARY: an address exists for the twin. Without this arm
			// the binary arms below would fail with a bare "exited 0" and the
			// reader would not learn that the row is what is missing.
			if !refused[twin] {
				t.Fatalf("%q is the underscore twin of the dispatched `gopi tool %s`, but it is in NO refusal table — the word falls through to the run path as prompt text. Add its row to cmd/agent/cli_tool_spelling_table.go (derived: underscoreTwin(CliVerb…)) and to internal/config's PositionalVerbs + aliasAddresses.", twin, word)
			}

			// (b) argv[0], on the real binary. The pre-fix measurement for
			// `ledger_window` is rc=0 with the generic banner on stdout; the
			// post-fix shape is rc=1 with a `gopi:` refusal line naming the
			// dash spelling.
			out, err := runCLIContractProbeRaw(t, bin, home, twin, HelpFlag)
			if err == nil {
				t.Fatalf("`gopi %s %s` exited 0 — the underscore twin of a dash-cased command must be refused by name\n--- output ---\n%s", twin, HelpFlag, out)
			}
			if !strings.Contains(out, gopiProgramWord+":") {
				t.Fatalf("`gopi %s %s` exited non-zero WITHOUT a refusal line — the word fell into some other path\n--- output ---\n%s", twin, HelpFlag, out)
			}
			if strings.Contains(out, runPathSignature) {
				t.Fatalf("`gopi %s %s` reached the RUN path — the whole point is that this word never costs an agent run\n--- output ---\n%s", twin, HelpFlag, out)
			}
			// The refusal must send the reader to the spelling that RUNS — and
			// that spelling is the DASH one under the group, not the twin it is
			// refusing (a correction that lands back on the refused word is the
			// measured 2026-10-07 defect).
			// The door is READ FROM THE TABLE, not assumed to be the tool
			// group: a top-level dash-cased verb lives at `gopi <word>`, and a
			// gate that hardcoded `gopi tool <word>` would call that correct
			// refusal drifted (measured 2026-11-08 on `screen_replay`, whose
			// live home is `gopi screen-replay`).
			if want := spellingRowDoor(word); !strings.Contains(out, want) {
				t.Errorf("`gopi %s %s`: the refusal does not name the live door %q — the recommendation and the command have drifted apart\n--- output ---\n%s", twin, HelpFlag, want, out)
			}
			assertNoCLIContractSession(t, twin+" "+HelpFlag, home)

			// (c) the same word AFTER a run flag — the other argv order the
			// operator types, answered by a different guard with a different
			// sentence. Both shells of one refusal must hold, or the hole stays
			// open in whichever order came to hand.
			//
			// NO HelpFlag ON THIS ARM, and that is a MEASURED requirement rather
			// than a style choice: `-h` anywhere in argv is answered by the help
			// page BEFORE the run path reaches the positional guard, so a
			// `--timeout 3 <twin> -h` probe exits 0 whether or not the word is
			// guarded — it measures the help surface, not this guard. Measured
			// on the built binary: `gopi --timeout 3 ledger_window -h` rc=0
			// (banner) while `gopi --timeout 3 ledger_window` rc=1 (the
			// positional refusal). An arm carrying `-h` here would be a probe
			// that can never fail.
			post, errPost := runCLIContractProbeRaw(t, bin, home, runFlagForTwinProbe, runFlagValueForTwinProbe, twin)
			if errPost == nil {
				t.Fatalf("`gopi %s %s %s` exited 0 — the positional guard does not carry this word\n--- output ---\n%s", runFlagForTwinProbe, runFlagValueForTwinProbe, twin, post)
			}
			if strings.Contains(post, runPathSignature) {
				t.Fatalf("`gopi %s %s %s` reached the RUN path — the positional guard's vocabulary is missing the word\n--- output ---\n%s", runFlagForTwinProbe, runFlagValueForTwinProbe, twin, post)
			}
		})
	}
}

// runFlagForTwinProbe is the run flag the post-flag arm types before the word.
// Named rather than inlined because it carries meaning (it is the ONE flag the
// positional guard is exercised with across this suite's probes) and because a
// second literal in a test body would be a Rule 1 defect.
const runFlagForTwinProbe = "--timeout"

// runFlagValueForTwinProbe is a VALID value for that flag: the arm must reach
// the positional guard, so the flag itself has to parse (a malformed value is a
// different refusal and would answer a different question).
const runFlagValueForTwinProbe = "3"
