package main

// cli_replay_prompt_refusal_test.go — the live-binary gate for the ONE row of
// the near-miss vocabulary that carries an OPERAND shape: the abolished top-level
// word `replay`, followed by a session id.
//
// THE DEFECT THIS PINS (measured 2026-10-06, this epic's briefing). A task
// briefing spelled `gopi replay 56507166-ef62-4d2d-bafe-af504fca9311` — the name
// the playback had before the 2026-10-03 consolidation
// (devdoc/replay-abolished-verb-single-route.md) moved the capability under the
// session family. No vocabulary on the run path knew the word, so the briefing
// line was spent as a PROMPT: a real agent session was started and answered by
// wondering what "replay" meant. The cost is the same one the near-miss guard was
// built for (123 `[auto]` ledger rows in the 2026-10-01 audit) — except this one
// was paid by a MACHINE, verbatim from a document.
//
// WHY THE ROW IS NOT A `verbAliases` ROW, which is where every other retired word
// lives: an alias row is refused at argv[0] in EVERY argv shape, `-h` included —
// TestVerbAliasIsRefusedByNameNoRun drives `{alias, "-h"}` and requires exit 1
// (measured: `gopi buildinfo -h` exits 1 with the refusal). This word must keep
// `-h` LEGAL: `gopi replay -h` prints the top-level page and exits 0, which is
// the acceptance contract of this epic and the same one the near-miss rows keep
// (TestNearMissHelpQueryStaysLegal). The near-miss vocabulary is reached AFTER
// the help scan, so it is the only run-path surface that can refuse a word while
// leaving its `-h` alone.
//
// THE FOUR CLAIMS, and why each is a separate test:
//  1. the row is WELL FORMED and its address really runs — the refusal's hint
//     may not lead nowhere (Rule 15);
//  2. the OPERAND shape is refused with exit 1 and mints NOTHING: no session
//     directory, no ledger row (three instruments, because one alone lies);
//  3. `-h` stays legal, and the word is NOT advertised by the top-level help
//     (the operator's contract: a refusal is not a revival);
//  4. the NEGATIVE CONTROL — one more word and the sentence is prompt text, so
//     `gopi "replay my day in three lines"` still runs. Without this the row
//     would be a guess about what the operator meant, which is the one thing
//     this vocabulary is forbidden to become.

import (
	"github.com/nick/gopi/internal/heavytest"
	"regexp"
	"strings"
	"testing"

	"github.com/nick/gopi/internal/config"
)

// replayPromptWord is the word under test: the session family's PLAYBACK
// sub-verb, which is also the name the top-level verb had before the 2026-10-03
// consolidation abolished it. It is referenced — never re-typed — so a rename of
// the sub-verb moves this gate with it (Rule 13).
const replayPromptWord = sessionSubcmdReplay

// replayTopLevelAdvert matches a top-level help line that advertises the word as
// a command (`  gopi replay ...`). It is the shape the abolition removed from
// internal/config/help.go's index.
var replayTopLevelAdvert = regexp.MustCompile(`(?m)^\s*gopi\s+` + replayPromptWord + `\b`)

// replayProbeSessionID is the id the briefing used, verbatim. It is the operand
// of every probe below, so the shape under test is the shape that hurt.
const replayProbeSessionID = "56507166-ef62-4d2d-bafe-af504fca9311"

// TestReplayPromptRowIsWellFormedAndItsAddressRuns — the row's own precondition:
// the word is in the vocabulary, it is NOT a dispatched verb (refusing a live
// command would make it unreachable), and the address it prints is a command
// line this binary really runs.
//
// The third arm is the one that matters for THIS row: the address is TWO words
// (`session replay`), which no other row here has. A hint that led nowhere would
// be the same defect one layer down, so the binary is asked — `gopi session
// replay -h` must exit 0 and print the player's own page.
func TestReplayPromptRowIsWellFormedAndItsAddressRuns(t *testing.T) {
	dispatched := dispatchedVerbWords(t)
	guarded := map[string]bool{}
	for _, v := range config.PositionalVerbs {
		guarded[v] = true
	}

	var row *config.CommandHintRow
	for _, r := range config.CommandHintRows() {
		if r.Word == replayPromptWord {
			cp := r
			row = &cp
		}
	}
	if row == nil {
		t.Fatalf("%q is not in the near-miss vocabulary — `gopi %s <session-id>` reaches the run path as a prompt again",
			replayPromptWord, replayPromptWord)
	}
	if dispatched[row.Word] {
		t.Errorf("%q is a DISPATCHED verb — refusing it would make the command unreachable", row.Word)
	}
	if guarded[row.Word] {
		t.Errorf("%q is in config.PositionalVerbs — that list refuses a word by casting it as a COMMAND "+
			"(“put the command first”), and this word has no top-level command to put first; its door is the session family",
			row.Word)
	}
	if row.Address == "" {
		t.Fatalf("%q carries no address — the refusal would be a dead end", row.Word)
	}

	// The address really runs — and it NAMES the player, which is the stronger
	// assertion this row's contract allows: the address is the FAMILY word
	// (`session`, the one this binary dispatches, as the vocabulary's own gate
	// requires), so `gopi session -h` must exit 0 AND list the play sub-verb.
	// The precise command line is a ROUTE, asserted separately below.
	bin := gopiTestBinPath(t)
	home := t.TempDir()
	out, err := runCLIContractProbeEnv(t, bin, home, append(strings.Fields(row.Address), HelpFlag)...)
	if err != nil {
		t.Fatalf("gopi %s -h: %v — the refusal's address leads nowhere\n--- output ---\n%s", row.Address, err, out)
	}
	if want := CliVerbSession + " " + replayPromptWord; !strings.Contains(out, want) {
		t.Errorf("gopi %s -h does not list %q — the address leads to a page that does not name the player\n--- output ---\n%s",
			row.Address, want, out)
	}

	// EVERY ROUTE the refusal prints is driven too, for the same reason: a route
	// asserted only in the message text could point at a flag that no longer
	// exists, and the sentence written to prevent a dead end would BE one.
	if len(row.Routes) == 0 {
		t.Fatalf("%q carries no routes — the refusal cannot name the second live door, and `gopi chat --replay` is the one a reader who typed the word alone is most likely to want", row.Word)
	}
	for _, r := range row.Routes {
		t.Run("route "+r.Command, func(t *testing.T) {
			out, err := runCLIContractProbeEnv(t, bin, t.TempDir(), append(strings.Fields(r.Command), HelpFlag)...)
			if err != nil {
				t.Fatalf("gopi %s -h: %v — a route printed by the refusal leads nowhere\n--- output ---\n%s", r.Command, err, out)
			}
		})
	}
}

// TestReplayWithASessionIDIsRefusedNoRun is the acceptance probe on the real
// binary: the briefing's own line, refused before the run path.
//
// THREE INSTRUMENTS for "no run happened", because one alone lies (a refused run
// and a fast run can both print little): the exit status, the absence of a
// session directory under an isolated GOPI_HOME, and an unchanged ledger row
// count. The session directory is the run path's own artifact, so its absence is
// the strongest of the three.
func TestReplayWithASessionIDIsRefusedNoRun(t *testing.T) {
	bin := gopiTestBinPath(t)
	for _, args := range [][]string{
		{replayPromptWord, replayProbeSessionID},                   // the briefing's line
		{replayPromptWord, replayProbeSessionID, "--timeout", "3"}, // operands + one trailing GLOBAL flag pair
		{replayPromptWord, replayProbeSessionID, "tty"},            // the three-positional bound
	} {
		argv := strings.Join(args, " ")
		t.Run(argv, func(t *testing.T) {
			home := t.TempDir()
			dbPath := config.ProfilePathIn(home, config.MemoryDBFileName)
			before := ledgerRowCountAt(t, dbPath)
			// RAW: the refused spelling IS the subject, so argv[0] must reach
			// the binary unmapped — the family door would rewrite the word into
			// the `session replay` that DISPATCHES, and the assertion below
			// would read a successful command as a failed refusal.
			out, err := runCLIContractProbeRaw(t, bin, home, args...)
			if err == nil {
				t.Fatalf("gopi %s: exit 0 — the word was spent as a prompt instead of refused\n--- output ---\n%s", argv, out)
			}
			for _, want := range []string{replayPromptWord, "--prompt " + replayPromptWord, "gopi -h", CliVerbSession + " " + replayPromptWord} {
				if !strings.Contains(out, want) {
					t.Errorf("gopi %s: refusal does not mention %q\n--- output ---\n%s", argv, want, out)
				}
			}
			assertNoCLIContractSession(t, argv, home)
			if after := ledgerRowCountAt(t, dbPath); after != before {
				t.Errorf("gopi %s: the refusal wrote %d ledger row(s) (before=%d after=%d) — the word reached the run path\n--- output ---\n%s",
					argv, after-before, before, after, out)
			}
		})
	}
}

// TestReplayHelpQueryStaysLegalAndUnadvertised is the constraint the fix had to
// preserve, from both sides at once: `gopi replay -h` keeps printing the
// TOP-LEVEL page with exit 0 (the word is not revived — the guard runs after the
// help scan), and the top-level help does NOT advertise `gopi replay` as a
// command (a refusal that made the word discoverable would be the revival this
// epic exists to prevent).
func TestReplayHelpQueryStaysLegalAndUnadvertised(t *testing.T) {
	bin := gopiTestBinPath(t)
	home := t.TempDir()
	out, err := runCLIContractProbeEnv(t, bin, home, replayPromptWord, HelpFlag)
	if err != nil {
		t.Fatalf("gopi %s -h: %v — the help query must stay legal (this row is a NEAR-MISS row, not an alias row: "+
			"an alias row refuses -h, and that is the one behaviour this word may not have)\n--- output ---\n%s",
			replayPromptWord, err, out)
	}
	if strings.Contains(out, sessionReplayUsageHead) {
		t.Fatalf("gopi %s -h printed the PLAYER's page — the abolished top-level verb is dispatched again:\n%s",
			replayPromptWord, out)
	}
	if !strings.Contains(out, "GoPi — a tool-using AI coding agent") {
		t.Fatalf("gopi %s -h printed neither the player's page nor the top-level help:\n%s", replayPromptWord, out)
	}
	if re := replayTopLevelAdvert; re.MatchString(out) {
		t.Errorf("the top-level help advertises `gopi %s` again:\n%s", replayPromptWord, out)
	}
	assertNoCLIContractSession(t, replayPromptWord+" -h", home)
}

// TestReplaySentenceIsStillPromptText is the NEGATIVE CONTROL, and the reason
// the row is bounded by the OPERAND shape rather than by "does this mention the
// word": `gopi "replay my day in three lines"` is a prompt, not a mistyped
// command, and must keep buying a run. Asserted by the run path's own signature
// (a session is minted) with the provider refused, so the probe is bounded and
// cheap.
//
// The operand bound is what makes this pass: the shape is the word, a session-id
// positional, and at most one more word — a sentence longer than that is prompt
// text however it opens.
func TestReplaySentenceIsStillPromptText(t *testing.T) {
	heavytest.Require(t, heavytest.CriterionDeviceExec)
	bin := gopiTestBinPath(t)
	for _, prompt := range []string{
		"replay my day in three lines",
		"replay the failed attempt and tell me why it died",
	} {
		t.Run(prompt, func(t *testing.T) {
			home := t.TempDir()
			out, _ := runCLIContractProbeEnvTimeout(t, bin, home, cliVerbGuardRunProbeTimeout,
				"--base-url", "http://127.0.0.1:9", "--api-key", "dummy", "--timeout", "3", prompt) //stylegate:ignore runs the fixture probe; the minted session is the assertion
			if strings.Contains(out, "is not a gopi command") {
				t.Fatalf("gopi %q was refused — a sentence opening with the word is prompt text\n--- output ---\n%s", prompt, out)
			}
			if !sessionMinted(home) {
				t.Fatalf("gopi %q did not reach the run path — the dispatch chain swallowed a prompt\n--- output ---\n%s", prompt, out)
			}
		})
	}
}
