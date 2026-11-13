package main

// cli_verb_hint_guard_test.go — the live-binary gate for the SECOND refusal
// surface: an unknown word that is a KNOWN NEAR-MISS of this binary's own
// command set (task 01a0f72b, 2026-10-01).
//
// THE DEFECT THIS PINS. The refusal vocabulary that already existed answers a
// word it RECOGNISES (`version`, `code_search`, `buildinfo`). Anything else is
// prompt text — right for `gopi frobnicate`, wrong for `gopi revision`: the
// operator meant a COMMAND (a query about the repository), the run path spent
// it as a prompt, started the interactive console and REGISTERED an `[auto]
// revision` ledger row. Measured on master (`GOPI_HOME=<sandbox> ./bin/gopi
// revision </dev/null`): no refusal, a session directory, a ledger row — the
// operator pays a whole agent run for a typo. 123 such `[auto]` rows were
// counted in the 2026-10-01 failed-row audit (`revision`, `status`,
// `branch list` ×2, `settings show` ×5, `scratch dir`).
//
// WHAT THE FIVE ASSERTIONS ARE FOR, in the order that matters:
//  1. the vocabulary is well formed — its addresses really dispatch, and no
//     word is ALSO a dispatched verb (that would make the command unreachable);
//  2. the shapes are refused by the REAL binary — exit non-zero, with a message
//     naming the word, the escape hatch and the help route;
//  3. the refusal mints NOTHING — no session directory, no ledger row (the
//     cost the defect charged, measured as zero);
//  4. `-h` stays LEGAL — `gopi revision -h` exits 0 with the top-level page,
//     which is the query the operator was making;
//  5. ordinary prompt text still runs — the negative control, without which
//     the vocabulary has become a guess about what the operator meant.

import (
	"github.com/nick/gopi/internal/heavytest"
	"strings"
	"testing"

	"github.com/nick/gopi/internal/config"
)

// TestCommandHintVocabularyIsWellFormed — the table's own precondition.
func TestCommandHintVocabularyIsWellFormed(t *testing.T) {
	dispatched := dispatchedVerbWords(t)
	guarded := map[string]bool{}
	for _, v := range config.PositionalVerbs {
		guarded[v] = true
	}
	seen := map[string]bool{}
	for _, row := range config.CommandHintRows() {
		if row.Word == "" {
			t.Fatalf("near-miss row %+v has an empty word — the gate would be vacuous", row)
		}
		if seen[row.Word] {
			t.Errorf("near-miss vocabulary lists %q twice", row.Word)
		}
		seen[row.Word] = true
		if dispatched[row.Word] {
			t.Errorf("%q is a DISPATCHED verb — refusing it would make the command unreachable", row.Word)
		}
		if guarded[row.Word] {
			t.Errorf("%q is already refused by config.PositionalVerbs (the spelling/synonym class) — a second row here is unreachable, and the message would differ between argv positions", row.Word)
		}
		if row.Address != "" && !dispatched[row.Address] {
			t.Errorf("%q points at %q, which this binary does NOT dispatch — the refusal's hint leads nowhere", row.Word, row.Address)
		}
	}
}

// TestUnknownNearMissIsRefusedByNameNoRun is the live-binary acceptance probe:
// the five measured shapes exit non-zero, the message names the word, the
// `--prompt` escape and the top-level help route, and the isolated GOPI_HOME
// is left with NO session directory and NO ledger row.
func TestUnknownNearMissIsRefusedByNameNoRun(t *testing.T) {
	bin := gopiTestBinPath(t)
	for _, tc := range []struct {
		argv []string
		word string
	}{
		{argv: []string{"revision"}, word: "revision"},
		{argv: []string{"status"}, word: "status"},
		{argv: []string{"branch", "list"}, word: "branch"},
		{argv: []string{"settings", "show"}, word: "settings"},
		{argv: []string{"scratch", "dir"}, word: "scratch"},
		{argv: []string{"--timeout", "3", "revision"}, word: "revision"},
		// The row measured as a LEDGER ROW rather than a sentence (task
		// 01a194e1, 2026-11-01): `gopi chats` — this binary's own `run_cmd` field
		// recorded it verbatim after the word was spent as a prompt.
		{argv: []string{"chats"}, word: "chats"},
	} {
		argv := strings.Join(tc.argv, " ")
		t.Run(argv, func(t *testing.T) {
			home := t.TempDir()
			dbPath := config.ProfilePathIn(home, config.MemoryDBFileName)
			before := ledgerRowCountAt(t, dbPath)
			// RAW: the refused near-miss word is the subject, so argv[0] must
			// not be rewritten by the family door.
			out, err := runCLIContractProbeRaw(t, bin, home, tc.argv...)
			if err == nil {
				t.Fatalf("gopi %s: exit 0 — the word was spent as a prompt instead of refused\n--- output ---\n%s", argv, out)
			}
			for _, want := range []string{tc.word, "--prompt " + tc.word, "gopi -h"} {
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

// TestNearMissHelpQueryStaysLegal is the constraint the fix had to preserve:
// `gopi revision -h` is a legal top-level help query and must keep printing the
// top-level page with exit 0. Pinned on the real binary, because the failure
// mode is a guard placed one line too early — the shape then dies with a
// refusal where an operator used to get help.
func TestNearMissHelpQueryStaysLegal(t *testing.T) {
	bin := gopiTestBinPath(t)
	for _, word := range []string{"revision", "status", "branch", "chats"} {
		t.Run(word, func(t *testing.T) {
			home := t.TempDir()
			out, err := runCLIContractProbeEnv(t, bin, home, word, HelpFlag)
			if err != nil {
				t.Fatalf("gopi %s -h: %v — the help query must stay legal\n--- output ---\n%s", word, err, out)
			}
			if !strings.Contains(out, "Usage:") {
				t.Errorf("gopi %s -h: exit 0 but no usage page\n--- output ---\n%s", word, out)
			}
			assertNoCLIContractSession(t, word+" -h", home)
		})
	}
}

// TestNearMissWithFurtherWordsIsStillPromptText pins the ARITY boundary: a
// near-miss word that OPENS a longer sentence is ordinary prompt text and must
// keep running. Without this the vocabulary would refuse `gopi branch off the
// failed attempt` — a prompt, not a command query — which is the "guess about
// what the operator meant" the closed table exists to avoid.
func TestNearMissWithFurtherWordsIsStillPromptText(t *testing.T) {
	heavytest.Require(t, heavytest.CriterionDeviceExec)
	bin := gopiTestBinPath(t)
	//
	// The sentences deliberately contain NO word that is a DISPATCHED verb:
	// the spelling/synonym guard (cli_verb_guard_test.go) refuses a sentence
	// carrying one wherever it appears (`gopi status of the merge queue` is
	// refused for the word `merge`, correctly) — that is a different guard's
	// business, and mixing the two would make this test read as a failure of
	// the hint vocabulary.
	for _, argv := range [][]string{
		{"branch", "off", "the", "failed", "attempt"},
		{"status", "report", "for", "today"},
	} {
		joined := strings.Join(argv, " ")
		t.Run(joined, func(t *testing.T) {
			home := t.TempDir()
			args := append([]string{"--base-url", "http://127.0.0.1:9", "--api-key", "dummy", "--timeout", "3"}, argv...)
			out, _ := runCLIContractProbeEnvTimeout(t, bin, home, cliVerbGuardRunProbeTimeout, args...) //stylegate:ignore runs the hint guard's probe; a nonzero probe error is reported by the guard itself and the output is the assertion
			if strings.Contains(out, "is not a gopi command") {
				t.Fatalf("gopi %s was refused — a longer sentence opening with a near-miss word is prompt text\n--- output ---\n%s", joined, out)
			}
			if !sessionMinted(home) {
				t.Fatalf("gopi %s did not reach the run path — the dispatch chain swallowed a prompt\n--- output ---\n%s", joined, out)
			}
		})
	}
}

// TestUnknownWordIsStillPromptTextAfterTheHintGuard is the negative control for
// THIS guard (its sibling in cli_undispatched_tool_words_test.go covers the
// spelling/synonym guard): a word in no vocabulary keeps reaching the run path.
func TestUnknownWordIsStillPromptTextAfterTheHintGuard(t *testing.T) {
	bin := gopiTestBinPath(t)
	home := t.TempDir()
	out, _ := runCLIContractProbeEnvTimeout(t, bin, home, cliVerbGuardRunProbeTimeout,
		"--base-url", "http://127.0.0.1:9", "--api-key", "dummy", "--timeout", "3", "frobnicate") //stylegate:ignore runs the fixture probe; the captured output is the assertion
	if strings.Contains(out, "is not a gopi command") {
		t.Fatalf("an unknown word was refused — ordinary prompt text must keep running\n--- output ---\n%s", out)
	}
	if !sessionMinted(home) {
		t.Fatalf("an unknown word did not reach the run path — the dispatch chain swallowed a prompt\n--- output ---\n%s", out)
	}
}

// TestChatsNearMissMintsNothingAndStillAnswersHelp is THIS epic's acceptance
// probe (task 01a194e1, 2026-11-01), and it is a live-binary test rather than a
// table walk because the defect was never about the table: `gopi chats` started
// a REAL agent run, minted a session directory and registered an `[auto] chats`
// ledger row, and the proof that this no longer happens is that the three
// artifacts do not appear.
//
// THE ROW THAT PRODUCED IT, verbatim from `gopi task show 01a194e1-…`:
//
//	summary:        [auto] chats
//	run_method:     auto
//	run_cmd:        gopi chats
//
// THREE INSTRUMENTS for "no run happened", because one alone can lie (a refused
// run and a fast run both print little): the exit status, the absence of a
// session directory under the isolated GOPI_HOME, and an unchanged row count in
// that profile's own ledger.
func TestChatsNearMissMintsNothingAndStillAnswersHelp(t *testing.T) {
	bin := gopiTestBinPath(t)

	t.Run("refused by name, no run", func(t *testing.T) {
		home := t.TempDir()
		dbPath := config.ProfilePathIn(home, config.MemoryDBFileName)
		before := ledgerRowCountAt(t, dbPath)
		// RAW: the refused spelling IS the subject, so argv[0] must not be
		// rewritten by a family door.
		out, err := runCLIContractProbeRaw(t, bin, home, "chats")
		if err == nil {
			t.Fatalf("gopi chats: exit 0 — the word was spent as a prompt instead of refused\n--- output ---\n%s", out)
		}
		for _, want := range []string{`"chats"`, "--prompt chats", "gopi " + CliVerbChat + " -h"} {
			if !strings.Contains(out, want) {
				t.Errorf("the refusal does not mention %q\n--- output ---\n%s", want, out)
			}
		}
		assertNoCLIContractSession(t, "chats", home)
		if after := ledgerRowCountAt(t, dbPath); after != before {
			t.Errorf("the refusal wrote %d ledger row(s) (before=%d after=%d) — `[auto] chats` is back\n--- output ---\n%s",
				after-before, before, after, out)
		}
	})

	t.Run("help query stays legal", func(t *testing.T) {
		home := t.TempDir()
		out, err := runCLIContractProbeRaw(t, bin, home, "chats", HelpFlag)
		if err != nil {
			t.Fatalf("gopi chats -h: %v — the help query must stay legal (the near-miss table is reached AFTER the help scan)\n--- output ---\n%s", err, out)
		}
		if !strings.Contains(out, "Usage:") {
			t.Errorf("gopi chats -h: exit 0 but no usage page\n--- output ---\n%s", out)
		}
		assertNoCLIContractSession(t, "chats -h", home)
	})
}
