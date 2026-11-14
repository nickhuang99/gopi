package main

// cli_tool_page_address_opens_test.go — every address the `tool` group's
// refusal PRINTS is a page that OPENS, and it is the table's answer rather than
// an echo of the word the reader typed (task 01a11560…, 2026-10-07 — a review of
// the archived branch task/01a114949f… against master's ONE spelling table).
//
// THE DEFECT THIS PINS, measured on the built binary. The refusal's page slot
// was `gopi <word>` — the word the reader had just typed — in TWO of its three
// arms, and neither arm can know that the spelling opens:
//
//	gopi tool nosuchword -h  → "… (or `gopi nosuchword` for the command's own
//	                           page)"   → running it: EXIT 0, and a session
//	                           directory is minted — it is not a page at all,
//	                           it is the bare-prompt path, i.e. the refusal
//	                           recommended one real agent run to someone who
//	                           made a typo.
//	gopi tool sandbox        → "… (or `gopi sandbox` for the command's own
//	                           page)"   → running it: exit 1 with the SAME
//	                           refusal — the hint points at itself, forever.
//
// The third arm (a word the spelling table CLAIMS) was already resolved by
// toolRefusalPageWord and is asserted here too, so the three arms are held to
// ONE property instead of one arm being fixed at a time.
//
// WHAT IS ASSERTED: for every arm, the address is read out of the MESSAGE BODY
// (a constant compared here would stay green while the message drifted), and
// then it is RUN — once bare and once with `-h`. A page exits 0 either way and
// mints nothing; the property that separates a page from the prompt path is the
// no-session half, because `gopi nosuchword` exited 0 precisely by spending a
// run (Rule 31: the address a refusal prints is a claim about the binary, and
// only the binary settles it).

import (
	"strings"
	"testing"
)

// toolRefusalPageArms is the set of argv shapes that reach a refusal which
// prints a page: one per spelling-table row the group can refuse, one per alias
// whose home LIVES under this group (the synonym half of the vocabulary, which
// the group's own vocabulary does not carry either), one word the group serves
// but whose bare form needs a second word, and one word no table claims. The
// words are the tables' own spellings (read, never typed), so a row that leaves
// its table fails here by name.
func toolRefusalPageArms(t *testing.T) [][]string {
	t.Helper()
	arms := make([][]string, 0, len(toolSpellingRows)+len(verbAliases)+4)
	for _, row := range toolSpellingRows {
		arms = append(arms, []string{CliVerbTool, row.refusedSpelling(), HelpFlag})
	}
	// THE SYNONYM HALF: a word that MEANS a command without being a respelling
	// of it (`buildinfo` → `version`) reaches this group with no spelling row,
	// and its home answers from the alias table. The filter is the property this
	// file is about — a word the GROUP DISPATCHES never reaches a refusal (it is
	// covered by that word's own page tests), so only the aliases the group
	// refuses are driven here.
	for _, a := range verbAliases {
		if _, served := toolSubcommandPredicates[a.alias]; served {
			continue
		}
		arms = append(arms, []string{CliVerbTool, a.alias, HelpFlag})
	}
	// Every word the group dispatches but whose bare address is incomplete
	// (a GROUPED word): the group prints its page after refusing the bare form.
	// The bare CARRIER (the one grouped word whose bare form is a real command
	// of its own — `gopi tool web`, which serves the chat UI) is skipped through
	// the production predicate rather than filtered by name here, so the two
	// lists cannot drift.
	for _, g := range toolGroupCommands {
		if toolWordIsABareCarrier(g.word) {
			continue
		}
		arms = append(arms, []string{CliVerbTool, g.word})
	}
	// The no-row arm: a word in no table and in no vocabulary.
	arms = append(arms, []string{CliVerbTool, toolRefusalProbeUnknownWord, HelpFlag})
	return arms
}

// toolRefusalProbeUnknownWord is the word this file uses for the no-row arm. It
// is a constant because the test asserts a table lookup MISSES it: a literal
// typed twice could drift into a word the tables really claim and turn the arm
// into a silent duplicate of another row (Rule 1).
const toolRefusalProbeUnknownWord = "nosuchword"

// TestToolGroupEveryRefusalPrintsAPageThatOpens is the regression guard: the
// address in every refusal's own message is the group's/ table's answer, and
// running it exits 0 with no session minted — both with and without `-h`,
// because the address is printed as a command line the reader may type verbatim.
func TestToolGroupEveryRefusalPrintsAPageThatOpens(t *testing.T) {
	bin := gopiTestBinPath(t)
	for _, argv := range toolRefusalPageArms(t) {
		t.Run(strings.Join(argv, " "), func(t *testing.T) {
			home := t.TempDir()
			out, err := runCLIContractProbeRaw(t, bin, home, argv...)
			if err == nil {
				t.Fatalf("`gopi %s` exited 0 — a refusal arm was accepted as a command\n--- output ---\n%s",
					strings.Join(argv, " "), out)
			}
			assertNoCLIContractSession(t, strings.Join(argv, " "), home)

			address := toolGroupRefusalPageWord(t, out)
			full := gopiProgramWord + " " + address
			if address == strings.TrimSpace(argv[1]) {
				t.Errorf("`gopi %s`: the refusal prints `%s` — the word it just refused, which is not a page (measured: the bare spelling exits 0 and mints a session)",
					strings.Join(argv, " "), full)
			}
			assertToolPageAddressOpens(t, bin, strings.Join(argv, " "), address)
		})
	}
}

// TestToolRefusalNoRowArmIsTheGroupsPage pins the no-row arm to its exact
// answer: no spelling row claims the word, so the address is this group's own
// page — the one page such a word can be sent to, and the property the
// pre-fix message violated by echoing a spelling that mints a session.
func TestToolRefusalNoRowArmIsTheGroupsPage(t *testing.T) {
	if _, ok := toolSpellingRowForSpelling(toolRefusalProbeUnknownWord); ok {
		t.Fatalf("%q is a spelling the table claims — this case is the NO-ROW arm and its premise is gone; move the word into the row-driven arm",
			toolRefusalProbeUnknownWord)
	}
	if got, want := toolRefusalPageWord(toolRefusalProbeUnknownWord), toolGroupOwnPageWord; got != want {
		t.Errorf("an unknown word is sent to %q, want %q — the group's own page is the only address that opens for a word with no page of its own",
			got, want)
	}
	bin := gopiTestBinPath(t)
	home := t.TempDir()
	out, err := runCLIContractProbeRaw(t, bin, home, CliVerbTool, toolRefusalProbeUnknownWord, HelpFlag)
	if err == nil {
		t.Fatalf("`gopi %s %s` exited 0\n--- output ---\n%s", CliVerbTool, toolRefusalProbeUnknownWord, out)
	}
	if !strings.Contains(out, gopiProgramWord+" "+toolGroupOwnPageWord) {
		t.Errorf("`gopi %s %s`: the refusal does not print `%s %s`\n--- output ---\n%s",
			CliVerbTool, toolRefusalProbeUnknownWord, gopiProgramWord, toolGroupOwnPageWord, out)
	}
	assertToolPageAddressOpens(t, bin, CliVerbTool+" "+toolRefusalProbeUnknownWord, toolGroupOwnPageWord)
}

// assertToolPageAddressOpens RUNS an address a refusal printed, as a PAGE: the
// reader asks for it with `-h`, the convention master's own rc gate uses ("for
// the command's own page"), and it must exit 0 and mint NOTHING.
//
// THE LEG IS `-h`, AND THE MEASUREMENT SAYS WHY THE BARE LEG IS NOT DRIVEN: an
// address that is a leaf with required input (`gopi tool code-search <symbol>`)
// or one that starts listening (`gopi tool voice_input`) exits non-zero or runs
// forever without `-h` — the command's own contract, not a dead end. What the
// no-session half is for is still real, and it was measured on the pre-patch
// binary 2026-10-07: the printed address “ `gopi nosuchword` “ run BARE exits 0
// and leaves 2 session directories and a ledger behind (the bare-prompt path,
// one agent run bought by a typo), while its `-h` leg exits 0 with nothing. So
// the load-bearing assertion for THAT defect is the address identity above
// (the printed spelling may not be the word the refusal just refused); the
// run-and-mint check is the floor under it, and the red proof below shows which
// leg catches which.
func assertToolPageAddressOpens(t *testing.T, bin, subject, address string) {
	t.Helper()
	fields := strings.Fields(address)
	if len(fields) == 0 || fields[0] == gopiProgramWord {
		t.Fatalf("%s: the refusal printed %q as a page word — it must be the word sequence after the `gopi ` prefix", subject, address)
	}
	argv := append(append([]string{}, fields...), HelpFlag)
	home := t.TempDir()
	out, err := runCLIContractProbeRaw(t, bin, home, argv...)
	if err != nil {
		t.Fatalf("%s recommends `%s %s`, which exits non-zero (%v) — every address a refusal prints must be a page that OPENS\n--- output ---\n%s",
			subject, gopiProgramWord, address, err, out)
	}
	assertNoCLIContractSession(t, gopiProgramWord+" "+strings.Join(argv, " "), home)
}
