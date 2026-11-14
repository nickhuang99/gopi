package main

// cli_undispatched_tool_words_test.go — the refusal contract for tool-named
// words this CLI deliberately does NOT dispatch (EUAUDIT slice 4 TOOLS-1 F3).
//
// THE DEFECT THIS PINS: an unknown FIRST argument is not refused, it is a
// PROMPT — so `gopi code_search EUAuditWidget` started a real agent run (a
// session directory, a live `[auto]` row, tokens) for the wire name of a
// command that exists under a different spelling (`gopi tool code-search`).
//
// The assertions, in the order that matters:
//  1. the refusal NAMES the word, the canonical spelling and the escape hatch
//     (`--prompt <word>`) — a refusal that leaves no way forward is a second
//     defect (Rule 15);
//  2. it exits non-zero and is FAST (no provider is reachable in the probe
//     env, so "slow" means it reached the run path);
//  3. it mints NOTHING — no session directory, no ledger row: the
//     prompt-fallback cost, measured as zero;
//  4. a word NOT in the vocabulary is still ordinary prompt text (the
//     refusal is a statement about this binary's own surface, never a guess
//     about what the operator meant);
//  5. every canonical spelling in the table really dispatches, and every word
//     really does not — a hint pointing at nothing would be worse than the
//     run it replaces.

import (
	"database/sql"
	"os"
	"strings"
	"testing"

	"github.com/nickhuang99/gopi/internal/config"
	"github.com/nickhuang99/gopi/internal/tools"

	_ "modernc.org/sqlite"
)

// TestUndispatchedToolWordsNameADispatchingCanonical — the vocabulary's own
// precondition, checked before any probe: each entry's canonical word must be
// a dispatched token (so the hint is actionable), and the refused word must
// NOT be one (so the refusal is about a spelling, not a command).
func TestUndispatchedToolWordsNameADispatchingCanonical(t *testing.T) {
	dispatched := map[string]bool{}
	for _, v := range topLevelCommandNames {
		dispatched[v] = true
	}
	for _, v := range []string{CliVerbCompletion, CliVerbRaw, CliVerbStats, CliVerbAct, CliVerbCLI} {
		dispatched[v] = true
	}
	for _, e := range undispatchedToolWords {
		if e.word == "" || e.canonical == "" || e.reason == "" {
			t.Fatalf("entry %+v is incomplete — an empty field would make this gate vacuous", e)
		}
		// The row is a statement about the ADDRESS its refusal prints — and
		// since the epic verb-retirement (commit 844d1f3d2) `canonical` is an
		// ADDRESS FRAGMENT, not the dispatched command word: `code_search`'s
		// field holds the dash-cased `code-search`, the spelling the refusal
		// CORRECTS the reader to, whose live home is the `tool` group line in
		// `homeProg`. The pre-retirement assertion compared the two fields'
		// DASH TWINS (`dashTwin(canonical) == dashTwin(word)`), which is TRUE BY
		// CONSTRUCTION once canonical is an address fragment — every row reads
		// as "refuses itself", which is why all eight lines went red on master
		// (measured) while the refusals themselves stayed correct.
		//
		// What must actually hold is form-independent: the address a refusal
		// prints must NOT be the refused spelling itself — a row printing
		// `gopi <the word>` would correct the reader into the very refusal they
		// just answered, a dead end (Rule 15). Whether a refused word SHADOWS a
		// real command is a question about the dispatch surface, and it is
		// answered by its own guard below (`dispatched[e.word]`), not here.
		if e.refusalAddress() == programAddress(e.word) {
			t.Errorf("%q refuses itself — the address its refusal prints (%q) IS the refused spelling; the hint would send the reader back into this same refusal", e.word, e.refusalAddress())
		}
		// THE ADDRESS, not the canonical word, is what must resolve: a row whose
		// canonical word is itself retired (the web dash twins point at
		// `web_search`, which is refused by its own arm) carries its own
		// homeProg, and THIS is the property that has to hold either way — the
		// command line the refusal prints is one this binary RUNS
		// (refusalAddressDispatches drives it, Rule 31).
		if !refusalAddressDispatches(t, e.refusalAddress()) {
			t.Errorf("%q prints the address %q, which this binary does not run — the hint leads nowhere", e.word, e.refusalAddress())
		}
		if dispatched[e.word] {
			t.Errorf("%q IS dispatched — it must not be in the refusal vocabulary (the command would be unreachable)", e.word)
		}
	}
}

// TestUndispatchedToolWordIsRefusedByNameNoRun is the live-binary acceptance
// probe: the refusal happens before the run path, exits 1, names the
// canonical spelling, and leaves the isolated GOPI_HOME with nothing in it.
func TestUndispatchedToolWordIsRefusedByNameNoRun(t *testing.T) {
	bin := gopiTestBinPath(t)
	for _, e := range undispatchedToolWords {
		for _, args := range [][]string{
			{e.word},
			{e.word, "-h"},
			{e.word, "EUAuditWidget"},
		} {
			argv := strings.Join(args, " ")
			t.Run(argv, func(t *testing.T) {
				home := t.TempDir()
				// RAW: the refused spelling is the subject — see
				// cli_verb_aliases_test.go's argv[0] probe for why the family
				// door would make this assertion vacuous.
				out, err := runCLIContractProbeRaw(t, bin, home, args...)
				if err == nil {
					t.Fatalf("gopi %s: exit 0 — the word was accepted instead of refused\n--- output ---\n%s", argv, out)
				}
				for _, want := range []string{e.word, e.refusalAddress(), "--prompt " + e.word} {
					if !strings.Contains(out, want) {
						t.Errorf("gopi %s: refusal does not mention %q\n--- output ---\n%s", argv, want, out)
					}
				}
				assertNoCLIContractSession(t, argv, home)
			})
		}
	}
}

// TestUndispatchedToolWordRefusalHasNoLedgerRow is the second half of the
// no-run proof. The session-directory assertion above covers the strongest
// signal (the ledger row is registered only once the session log exists), but
// the acceptance counts LEDGER ROWS, so this asserts the same thing on the
// row's own surface: the isolated home's ledger database gains no row for the
// refused word.
func TestUndispatchedToolWordRefusalHasNoLedgerRow(t *testing.T) {
	bin := gopiTestBinPath(t)
	home := t.TempDir()
	dbPath := config.ProfilePathIn(home, config.MemoryDBFileName)
	before := ledgerRowCountAt(t, dbPath)

	word := undispatchedToolWords[0].word
	out, err := runCLIContractProbeEnv(t, bin, home, word, "EUAuditWidget")
	if err == nil {
		t.Fatalf("gopi %s: exit 0 — no refusal\n--- output ---\n%s", word, out)
	}
	if after := ledgerRowCountAt(t, dbPath); after != before {
		t.Fatalf("the refusal wrote %d new ledger row(s) (before=%d after=%d) — the word reached the run path\n--- output ---\n%s",
			after-before, before, after, out)
	}
}

// ledgerRowCountAt counts the rows of the ledger at an EXPLICIT path (the
// probe's isolated profile) — a zero-absence read, so it must never be able to
// create the file it counts: a missing ledger IS zero rows, and the file is
// opened read-only through the tools layer's own DSN (Rule 13: no re-typed
// DSN, no mode=ro typed by hand here).
func ledgerRowCountAt(t *testing.T, path string) int {
	t.Helper()
	if _, err := os.Stat(path); os.IsNotExist(err) {
		return 0
	}
	db, err := sql.Open("sqlite", tools.LedgerReadOnlyDSN(path))
	if err != nil {
		t.Fatalf("open ledger %s: %v", path, err)
	}
	defer db.Close() //stylegate:ignore the probe's own handle, closed immediately
	var n int
	if err := db.QueryRow(`SELECT COUNT(*) FROM tasks`).Scan(&n); err != nil {
		t.Fatalf("count rows in %s: %v", path, err)
	}
	return n
}

// TestUnknownWordIsStillPromptText is the counter-case that keeps the refusal
// honest: a word nobody claims must still reach the RUN path (asserted by its
// absence-of-refusal signature, not by a successful answer — the probe's
// provider is unreachable on purpose). This is why the vocabulary is a table
// of KNOWN spellings rather than a pattern: `gopi frobnicate …` is a prompt.
func TestUnknownWordIsStillPromptText(t *testing.T) {
	bin := gopiTestBinPath(t)
	home := t.TempDir()
	out, _ := runCLIContractProbeEnvTimeout(t, bin, home, cliVerbGuardRunProbeTimeout,
		"--base-url", "http://127.0.0.1:9", "--api-key", "dummy", "--timeout", "3", "frobnicate") //stylegate:ignore runs the fixture probe; the captured output is the assertion //stylegate:ignore runs the fixture probe; the captured output is the assertion
	if strings.Contains(out, "is not a gopi command") {
		t.Fatalf("an unknown word was refused — ordinary prompt text must keep running\n--- output ---\n%s", out)
	}
	if !sessionMinted(home) {
		t.Fatalf("an unknown word did not reach the run path — the dispatch chain swallowed a prompt\n--- output ---\n%s", out)
	}
}
