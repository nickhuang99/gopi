package main

// cli_webtool_dash_twin_test.go — the refusal arms for the DASH TWINS of the
// retired web tool words, and the usage pages that used to promise them
// (task 01a10b4b…, 2026-10-05, the follow-up the double-spelling slice filed).
//
// THE DEFECT THIS PINS. `web_search` / `web_fetch` are retired flat verbs with
// their own refusal arm (refuseWebToolFlatVerb, cli_websearch.go /
// cli_webfetch.go). Their dash-cased twins carry the SAME concept under the
// other separator, and they were in NO vocabulary at all — not the dispatch
// chain, not the spelling table, not config.PositionalVerbs — so
// `gopi web-search "<query>"` fell through to the bare-prompt path and reached
// the agent loop. Measured on the pre-fix binary in an isolated GOPI_HOME
// (artifacts/webtool-spellings-before.txt): both words print the run path's own
// `Error creating agent: failed to create DeepSeek Claude client: …` — with
// credentials in the environment that is a PAID MODEL CALL plus a minted
// session, which is exactly what the guard exists to prevent. `gopi web-search
// -h` was worse in a quieter way: it printed the top-level help as if the word
// were ordinary prompt text, while `gopi web_search -h` answered with the
// retirement line.
//
// WHAT THE TESTS BELOW ASSERT, in the order that matters:
//
//	(1) THE ARM IS IN THE ONE VOCABULARY. The refusal words are read from the
//	    tables their refusal lives in (cli_undispatched_tool_words.go), never
//	    re-typed here, so the assertion follows a rename.
//	(2) THE REFUSAL SENDs THE READER SOMEWHERE THAT RUNS. The address a row
//	    prints must be a command line this binary really runs — checked by
//	    RUNNING it (Rule 31), not by comparing strings.
//	(3) THE REAL BINARY REFUSES BOTH SHAPES. argv[0] and the position after a
//	    run flag, each exit non-zero with the pointed refusal, and each minting
//	    NOTHING: no session directory, no ledger row (the cost, as zero).
//	(4) `-h` STAYS A QUERY. The retired flat words answer with the live page;
//	    their dash twins are refused in every shape, `-h` included, so neither
//	    can be mistaken for a command.
//	(5) NO PAGE PRINTS THE STALE PROMISE. The closing paragraph of the three
//	    web pages used to say the legacy flat verb "still works (deprecation
//	    note on stderr)" while the binary refused it and exited 1 — a page that
//	    contradicts its own binary. Each page is driven OUT OF THE BINARY and
//	    checked for the promise and for the live home.
//
// RED PROOF (recorded in the report): deleting the two dash-twin rows from
// cli_undispatched_tool_words.go turns (3) red for both words; restoring them
// turns it green.

import (
	"strings"
	"testing"

	"github.com/nick/gopi/internal/toolnames"
)

// legacyFlatVerbPromise is the sentence the web usage pages used to carry. It
// is named here because the test's job is to fail if ANY page this binary
// prints brings it back: the promise and the behaviour must agree, and the
// behaviour is a refusal (exit 1).
const legacyFlatVerbPromise = "still work"

// runPathSignature is the run path's own first line in the isolated probe
// environment (no credentials are reachable there). Its presence means the word
// was spent as a PROMPT — with credentials that same path is a paid model call,
// which is why the probe asserts on the signature rather than on a timeout.
const runPathSignature = "Error creating agent"

// TestRefusedSpellingWordsCannotReachTheRunPath drives EVERY word of the
// spelling table through the real binary in both argv positions, in an isolated
// GOPI_HOME, and asserts the two things a refusal owes the operator: a pointed
// message and no run. The vocabulary is read from the table, so a new row is
// covered the moment it is added — this test does not name a word.
func TestRefusedSpellingWordsCannotReachTheRunPath(t *testing.T) {
	bin := gopiTestBinPath(t)
	for _, e := range undispatchedToolWords {
		address := e.refusalAddress()
		for _, tc := range []struct {
			name  string
			args  []string
			names bool // does this shape's refusal print the row's live home?
		}{
			// The argv[0] shape: the spelling table's own refusal, which names
			// the LIVE HOME the row carries (Rule 15 — a refusal that says how
			// to proceed).
			{name: "argv0", args: []string{e.word}, names: true},
			{name: "argv0 with an argument", args: []string{e.word, e.word + " arg"}, names: true},
			// The shape after a run flag: internal/config's positional-verb
			// guard owns it (that package cannot import this one), and what it
			// prints is the corrected command line — the word itself, moved to
			// the front. It names no home, so only the refusal and the no-run
			// assertions apply here.
			{name: "after a run flag", args: []string{"--timeout", "3", e.word}},
		} {
			argv := strings.Join(tc.args, " ")
			t.Run(argv, func(t *testing.T) {
				home := t.TempDir()
				out, err := runCLIContractProbeEnv(t, bin, home, tc.args...)
				if err == nil {
					t.Fatalf("gopi %s: exit 0 — the word was accepted instead of refused\n--- output ---\n%s", argv, out)
				}
				if strings.Contains(out, runPathSignature) {
					t.Fatalf("gopi %s: reached the RUN PATH (the agent was constructed) — the word was spent as a prompt\n--- output ---\n%s", argv, out)
				}
				if !strings.Contains(out, e.word) {
					t.Errorf("gopi %s: the refusal does not name the word\n--- output ---\n%s", argv, out)
				}
				if tc.names {
					for _, want := range strings.Fields(address) {
						if !strings.Contains(out, want) {
							t.Errorf("gopi %s: the refusal does not name its live home %q (missing %q)\n--- output ---\n%s", argv, address, want, out)
						}
					}
				}
				assertNoRunArtifacts(t, argv, home, out)
			})
		}
	}
}

// TestWebToolFlatTwinsAreRefusedByTheirOwnArm is the acceptance probe for THIS
// task's two rows, with the expectations written out: the dash twins print the
// same sentence SHAPE as the flat words (a named refusal, the live `tool` group
// home, the `--prompt` escape), and no shape of them reaches the run path.
//
// The run-flag shape is asserted for the REFUSAL and the NO-RUN property only.
// Its refusal belongs to a different layer — internal/config's positional-verb
// guard, which cannot know the word is retired (that package cannot import this
// one) — so it corrects the POSITION and hands the reader to `gopi web-search`,
// which is the shape whose arm names the live home. Both hops were driven for
// real and are recorded in devdoc/web-tool-dash-twin-refusal.md; requiring the
// home from hop #1 would be requiring a layer to know something it cannot.
//
// The words themselves come from toolnames through dashTwin — the constant that
// owns them (Rule 1/13) — so a tool rename cannot leave this test asserting on
// a spelling the binary no longer knows.
func TestWebToolFlatTwinsAreRefusedByTheirOwnArm(t *testing.T) {
	bin := gopiTestBinPath(t)
	for _, tc := range []struct {
		word string
		home string
	}{
		{word: dashTwin(toolnames.ToolWebSearch), home: webSearchGroupProg},
		{word: dashTwin(toolnames.ToolWebFetch), home: webFetchGroupProg},
	} {
		t.Run(tc.word, func(t *testing.T) {
			for _, shape := range []struct {
				name  string
				args  []string
				names bool // does this shape's refusal print the live home?
			}{
				{name: "argv0", args: []string{tc.word, "hello"}, names: true},
				{name: "help", args: []string{tc.word, "-h"}, names: true},
				{name: "after a run flag", args: []string{"--timeout", "3", tc.word}},
			} {
				argv := strings.Join(shape.args, " ")
				home := t.TempDir()
				out, err := runCLIContractProbeEnv(t, bin, home, shape.args...)
				if err == nil {
					t.Fatalf("gopi %s: exit 0 — a refused spelling must exit non-zero\n--- output ---\n%s", argv, out)
				}
				if strings.Contains(out, runPathSignature) {
					t.Fatalf("gopi %s: reached the RUN PATH\n--- output ---\n%s", argv, out)
				}
				wants := []string{argvRefusalNamesTheWord(tc.word), "--prompt " + tc.word}
				if shape.names {
					wants = append(wants, tc.home) // the LIVE home, not the retired one
				}
				for _, want := range wants {
					if !strings.Contains(out, want) {
						t.Errorf("gopi %s: refusal does not mention %q\n--- output ---\n%s", argv, want, out)
					}
				}
				assertNoRunArtifacts(t, argv, home, out)
			}
		})
	}
}

// argvRefusalNamesTheWord is the spelling the refusal quotes the refused word
// in (the shared refusal table double-quotes it). Named so the assertion above
// reads as the sentence shape it is checking rather than as a quote character.
func argvRefusalNamesTheWord(word string) string { return `"` + word + `"` }

// TestWebUsagePagesDoNotPromiseARetiredFlatVerb pins the second half of the
// task: no page THIS binary prints may claim the legacy flat verb works. Each
// page is produced by running the surface that prints it, and each must both
// (a) be free of the stale promise and (b) name the live home the reader needs
// — because deleting the promise without naming where the command went would
// leave the reader with no door (Rule 15).
//
// The home each page must name is PER PAGE, not the pair: the flat word's own
// page documents ITS tool (the fetch page naming only `gopi tool web fetch` is
// the correct page), while the web GROUP's page covers the group and must name
// both.
func TestWebUsagePagesDoNotPromiseARetiredFlatVerb(t *testing.T) {
	bin := gopiTestBinPath(t)
	for _, tc := range []struct {
		name  string
		args  []string
		homes []string
	}{
		{
			name:  "the search flat word's own page (-h is answered with the live page)",
			args:  []string{toolnames.ToolWebSearch, HelpFlag},
			homes: []string{webSearchGroupProg},
		},
		{
			name:  "the fetch flat word's own page",
			args:  []string{toolnames.ToolWebFetch, HelpFlag},
			homes: []string{webFetchGroupProg},
		},
		{
			name:  "the web group's page (printed on an unknown sub-verb)",
			args:  []string{CliVerbWeb, "no-such-subverb"},
			homes: []string{webSearchGroupProg, webFetchGroupProg},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			home := t.TempDir()
			out, _ := runCLIContractProbeEnv(t, bin, home, tc.args...) //stylegate:ignore the exit status is not the assertion here — the PAGE the surface prints is
			argv := strings.Join(tc.args, " ")
			if strings.Contains(out, legacyFlatVerbPromise) {
				t.Fatalf("gopi %s prints a page promising that the retired flat verb %q — the binary refuses it and exits 1, so the page contradicts its own binary:\n%s",
					argv, legacyFlatVerbPromise, out)
			}
			for _, want := range tc.homes {
				if !strings.Contains(out, want) {
					t.Fatalf("gopi %s: the page does not name the live home %q\n--- output ---\n%s", argv, want, out)
				}
			}
			assertNoRunArtifacts(t, argv, home, out)
		})
	}
}

// refusalAddressOf is the address a refused word's message prints — the row's
// own home when it carries one (a spelling whose canonical word is itself
// refused), `gopi <canonical>` otherwise. It reads the SAME tables the refusal
// itself walks, so the tests and the message cannot disagree.
func refusalAddressOf(word string) string {
	for _, e := range undispatchedToolWords {
		if e.word == word {
			return e.refusalAddress()
		}
	}
	for _, a := range verbAliases {
		if a.alias == word {
			// The row's OWN accessor, so the empty address a homeless row
			// carries (`ui`, `cli`) is read the same way here as it is by the
			// refusal message — one representation of "no home".
			return a.refusalAddress()
		}
	}
	return ""
}

// refusalAddressDispatches reports whether an address a refusal prints is
// something this binary really RUNS. Two legs, because the addresses have two
// shapes:
//
//   - the FIRST word must be a dispatched top-level verb (the derived
//     vocabulary `gopi <TAB>` and the guard share) and must NOT itself be a
//     refused spelling — an address pointing at another refusal is a dead end;
//   - when the address carries a second level (`gopi tool web search`), that
//     second level is checked by RUNNING it: `gopi tool web search -h` must
//     exit 0. A word inside a group is not in any top-level vocabulary, and
//     this is the only honest way to know the door opens (Rule 31 — the
//     keyword hit is not the evidence).
//
// Each distinct address is run at most once per test binary: the probe is a
// process, and the vocabulary holds a handful of addresses.
func refusalAddressDispatches(t *testing.T, address string) bool {
	t.Helper()
	fields := strings.Fields(address)
	if len(fields) == 0 {
		return false
	}
	if fields[0] != gopiProgramWord {
		return false
	}
	word := fields[1]
	if !dispatchedVerbWords(t)[word] {
		return false
	}
	if refusalAddressOf(word) != "" {
		return false // the address points at a refused spelling
	}
	if len(fields) == 2 {
		return true
	}
	return groupedAddressRuns(t, fields[1:])
}

// groupedAddressRuns runs `gopi <tail…> -h` and reports whether it exits 0 —
// the group level has no vocabulary to read, so the binary is asked.
func groupedAddressRuns(t *testing.T, tail []string) bool {
	t.Helper()
	if ok, cached := groupedAddressCache[strings.Join(tail, " ")]; cached {
		return ok
	}
	bin := gopiTestBinPath(t)
	home := t.TempDir()
	args := append(append([]string{}, tail...), HelpFlag)
	out, err := runCLIContractProbeEnv(t, bin, home, args...)
	ok := err == nil && !strings.Contains(out, runPathSignature)
	groupedAddressCache[strings.Join(tail, " ")] = ok
	if !ok {
		t.Logf("address `%s %s` did not run (exit %v):\n%s", gopiProgramWord, strings.Join(args, " "), err, out)
	}
	return ok
}

// groupedAddressCache memoises the probe above for the life of the test binary.
var groupedAddressCache = map[string]bool{}
