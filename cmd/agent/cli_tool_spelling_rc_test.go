package main

// cli_tool_spelling_rc_test.go — the RUN-FOR-REAL contract between the spelling
// table and the binary (epic spelling-table-source-of-truth, 2026-10-07).
//
// WHY A SEPARATE FILE, AND WHY IT RUNS THE BINARY. The table's whole claim is
// about what the BINARY does with a word: the canonical spelling opens a page,
// the refused spelling exits non-zero, and the page a refusal NAMES opens too.
// A source-level comparison cannot make that claim (Rule 31 — a surface in no
// execution has not been tested), and the defect this epic repairs was exactly
// of that shape: every list agreed with itself while the hint the operator was
// handed pointed at a refused word. Measured 2026-10-07 on the built binary:
//
//	gopi tool code_search -h   rc=1 … "(or `gopi code_search` for the command's own page)"
//	gopi code_search -h        rc=1 … "this one is spelled gopi tool code-search"
//
// — a correction that lands on a second refusal. Each test below is one leg of
// the property that makes that impossible.
//
// THE PROBES ARE THE SUITE'S OWN (cli_contracts_test.go): runCLIContractProbeRaw
// drives argv EXACTLY as given (a refused spelling is the subject, so mapping
// it would answer a different question), the probe's environment is an isolated
// GOPI_HOME, and its timeout IS the no-agent-loop detector.

import (
	"strings"
	"testing"
)

// TestUndispatchedToolWordsRcContract is the table-driven acceptance: for every
// row of the one spelling table,
//
//	(a) the live door RUNS — exit 0, non-empty page;
//	(b) the refused spelling does NOT — exit non-zero;
//	(c) the refusal NAMES that door (not some other line, and never a word that
//	    is itself refused), and
//	(d) naming it is not a claim: the address printed in the message is the
//	    door, and the door is then RUN to prove it opens.
//
// (c)+(d) are the self-consistency the epic asks for: the recommendation a
// refusal prints must itself be rc=0.
func TestUndispatchedToolWordsRcContract(t *testing.T) {
	bin := gopiTestBinPath(t)
	home := t.TempDir()

	for _, row := range toolSpellingRows {
		t.Run(row.refusedSpelling(), func(t *testing.T) {
			doorArgs := append(strings.Fields(row.door), HelpFlag)
			page, err := runCLIContractProbeRaw(t, bin, home, doorArgs...)
			if err != nil {
				t.Fatalf("the canonical door `%s` does not run: %v — the spelling rule's own page must be reachable\n--- output ---\n%s",
					row.door+" "+HelpFlag, err, page)
			}
			if strings.TrimSpace(page) == "" {
				t.Fatalf("`%s` exited 0 with an empty page — a door that prints nothing is not a door", row.door)
			}
			assertNoCLIContractSession(t, row.door, home)

			refused, errUsed := runCLIContractProbeRaw(t, bin, home, row.refusedSpelling(), HelpFlag)
			if errUsed == nil {
				t.Fatalf("`gopi %s -h` exited 0 — the refused spelling must exit non-zero (it would otherwise be a second surface for one command)\n--- output ---\n%s",
					row.refusedSpelling(), refused)
			}
			if !strings.Contains(refused, row.door) {
				t.Errorf("`gopi %s -h`: the refusal does not name the live door %q — the recommendation and the command have drifted apart\n--- output ---\n%s",
					row.refusedSpelling(), row.door, refused)
			}
			// SELF-CONSISTENCY, measured rather than assumed: take the address
			// out of the refusal's OWN message and run it. Comparing the printed
			// string to row.door would only re-read the table; running it asks
			// the question the operator asks.
			address := refusalAddressFromMessage(t, refused)
			if address != row.door {
				t.Fatalf("the refusal for %q prints the address %q, but the row's door is %q — two answers to one question",
					row.refusedSpelling(), address, row.door)
			}
			assertRefusalAddressRuns(t, bin, home, row.refusedSpelling(), address)
			assertNoCLIContractSession(t, row.refusedSpelling()+" "+HelpFlag, home)
		})
	}
}

// TestUndispatchedToolWordsVocabularyRcContract drives EVERY refused word of the
// whole vocabulary — both halves (the spellings table and the synonym table) —
// through the real binary. It is the floor under the epic's acceptance, stated
// over the vocabulary rather than over one table, so a word added to either half
// without a runnable address is caught here.
//
// The address is not asserted for the synonym half (its own gate,
// cli_verb_aliases_test.go, drives each row's canonical word); what is asserted
// here is what both halves share: rc = 1, and nothing minted.
// The shared property across BOTH halves of the vocabulary is what is asserted
// here: the word exits non-zero on the real binary, the failure is a REFUSAL
// (a `gopi:` refusal line, not some other path's error), and nothing is minted.
// The sentence differs by row class — the spelling table's rows say "is not a
// gopi command", the retired verbs with a pointer say "is RETIRED — use …" —
// and both are refusals, so the assertion is written over the class-independent
// shape rather than over one sentence.
func TestUndispatchedToolWordsVocabularyRcContract(t *testing.T) {
	bin := gopiTestBinPath(t)
	home := t.TempDir()
	for _, word := range undispatchedRefusedWordSpellings() {
		t.Run(word, func(t *testing.T) {
			out, err := runCLIContractProbeRaw(t, bin, home, word, HelpFlag)
			if err == nil {
				t.Fatalf("`gopi %s -h` exited 0 — a refused spelling must exit 1\n--- output ---\n%s", word, out)
			}
			if !strings.Contains(out, gopiProgramWord+":") {
				t.Fatalf("`gopi %s -h` exited non-zero without a refusal line — the word fell into some other path\n--- output ---\n%s", word, out)
			}
			if strings.Contains(out, runPathSignature) {
				t.Fatalf("`gopi %s -h` reached the RUN path — the whole point is that this word never costs an agent run\n--- output ---\n%s", word, out)
			}
			assertNoCLIContractSession(t, word, home)
		})
	}
}

// TestToolGroupWordsRefuseToALivePage is the `tool` GROUP's own leg of the same
// property, and it is the defect's exact regression: the group refuses a word
// its vocabulary does not hold and offers the reader "the command's own page" —
// THAT page must RUN. Before the spelling table the group echoed the word it was
// given (`gopi code_search`), which the argv[0] arm then refuses in turn.
//
// The word under test is the group's own vocabulary read the wrong way round:
// for every row whose door lives under this group, the word fed to the group is
// the spelling the table declares REFUSED, and the page the refusal offers is
// then executed.
func TestToolGroupWordsRefuseToALivePage(t *testing.T) {
	bin := gopiTestBinPath(t)
	home := t.TempDir()
	for _, row := range toolSpellingRows {
		if !strings.HasPrefix(row.door, gopiProgramWord+" "+CliVerbTool+" ") {
			continue // doors outside the group are covered by the arm above
		}
		t.Run(row.refusedSpelling(), func(t *testing.T) {
			out, err := runCLIContractProbeRaw(t, bin, home, CliVerbTool, row.refusedSpelling(), HelpFlag)
			if err == nil {
				t.Fatalf("`gopi %s %s %s` exited 0 — the group dispatches a spelling the table declares refused\n--- output ---\n%s",
					CliVerbTool, row.refusedSpelling(), HelpFlag, out)
			}
			pageWord := toolGroupRefusalPageWord(t, out)
			assertRefusalAddressRuns(t, bin, home, CliVerbTool+" "+row.refusedSpelling(), gopiProgramWord+" "+pageWord)
			assertNoCLIContractSession(t, CliVerbTool+" "+row.refusedSpelling(), home)
		})
	}
}

// assertRefusalAddressRuns RUNS the command line a refusal recommended and fails
// unless it exits 0 with a non-empty page. It takes the address whole (program
// word included) so the caller cannot accidentally prepend one.
func assertRefusalAddressRuns(t *testing.T, bin, home, subject, address string) {
	t.Helper()
	fields := strings.Fields(address)
	if len(fields) < 2 || fields[0] != gopiProgramWord {
		t.Fatalf("%s: malformed address %q — a refusal must name a full command line", subject, address)
	}
	out, err := runCLIContractProbeRaw(t, bin, home, append(fields[1:], HelpFlag)...)
	if err != nil {
		t.Fatalf("%s recommends `%s`, which exits non-zero (%v) — the hint leads into another refusal (Rule 15 dead end)\n--- output ---\n%s",
			subject, address, err, out)
	}
	if strings.TrimSpace(out) == "" {
		t.Fatalf("%s recommends `%s`, which exits 0 with an empty page — the hint reaches nothing", subject, address)
	}
	assertNoCLIContractSession(t, address, home)
}

// refusalAddressFromMessage reads the address out of the argv[0] refusal's own
// text — the sentence names it in backticks, so this asks what the operator
// actually reads rather than what the table holds. Fatal when the message does
// not carry one: an address-less refusal is a dead end by definition.
func refusalAddressFromMessage(t *testing.T, message string) string {
	t.Helper()
	const marker = "this one is spelled `"
	i := strings.Index(message, marker)
	if i < 0 {
		t.Fatalf("the refusal names no address — nothing to run\n--- output ---\n%s", message)
	}
	rest := message[i+len(marker):]
	j := strings.Index(rest, "`")
	if j < 0 {
		t.Fatalf("the refusal's address is unclosed\n--- output ---\n%s", message)
	}
	return rest[:j]
}

// toolGroupRefusalPageWord reads the word the `tool` group's refusal offers as
// the command's own page — the last argument of its
// `(or `gopi <word>` for the command's own page)` sentence. Fatal when the
// sentence is absent, because that sentence IS the hint the defect was about.
//
// The sentence's own name for itself is the PRODUCTION constant
// (toolRefusalPageClause — Rule 1/13/14): a copy typed here would keep passing
// while the message it claims to parse had been reworded, and the assertions
// below would then be reading a sentence that no longer exists.
func toolGroupRefusalPageWord(t *testing.T, message string) string {
	t.Helper()
	i := strings.Index(message, toolRefusalPageClause)
	if i < 0 {
		t.Fatalf("the group's refusal offers no page — the reader is left with a vocabulary list only\n--- output ---\n%s", message)
	}
	head := message[:i]
	j := strings.LastIndex(head, gopiProgramWord+" ")
	if j < 0 {
		t.Fatalf("the group's refusal sentence carries no `gopi <word>` address\n--- output ---\n%s", message)
	}
	tail := strings.TrimSpace(head[j+len(gopiProgramWord)+1:])
	return strings.Trim(tail, "`()")
}
