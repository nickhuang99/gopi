package main

// cli_verb_aliases_test.go — the red-then-green gate for the SYNONYM half of
// the refusal vocabulary (cli_verb_aliases.go).
//
// THE DEFECT THIS PINS: `gopi buildinfo` — the natural way to ask for what
// `gopi version` prints — was not a command and not refused, so it fell through
// to the bare-prompt path and bought a whole agent run. Measured 2026-09-30:
// three consecutive invocations, three `reason=task failed` ledger tombstones,
// zero output.
//
// The assertions, in the order that matters:
//  1. the alias is REFUSED BY NAME on the real binary — exit 1, and the
//     message names the canonical command the operator actually wanted;
//  2. it mints NOTHING — no session directory and no ledger row: the
//     prompt-fallback cost, measured as zero (the same contract
//     cli_undispatched_tool_words_test.go pins for spellings);
//  3. it is refused in BOTH argv positions — as argv[0] (the dispatch chain's
//     shape) and left over AFTER a run flag (`gopi --timeout 3 buildinfo`),
//     which is internal/config's positional-verb guard;
//  4. the canonical word still DISPATCHES (a hint pointing at nothing would be
//     worse than the run it replaces);
//  5. an ordinary unknown word still reaches the prompt path (the negative
//     control — without it the refusal table has become a guess about intent).

import (
	"github.com/nick/gopi/internal/heavytest"
	"strings"
	"testing"

	"github.com/nick/gopi/internal/config"
)

// TestVerbAliasTableIsWellFormed — the table's own precondition, checked
// before any probe: a row must name three non-empty fields, must not point at
// itself, must name a canonical word this binary DISPATCHES, and must not be a
// word that is itself a command (an alias that is dispatched would make the
// command unreachable, and the refusal would fire on the real verb).
func TestVerbAliasTableIsWellFormed(t *testing.T) {
	dispatched := dispatchedVerbWords(t)
	// The table's own invariant, in the production file so the reader of the
	// table can call it too: every row is complete, an address that EXISTS is a
	// command line this binary really runs, and an EMPTY address is legal only
	// on a retired row whose capability is gone (the two withdrawn chat
	// views). The oracle is the same live-binary probe the guards below use.
	if err := checkVerbAliasRows(func(address string) bool {
		return refusalAddressDispatches(t, address)
	}); err != nil {
		t.Fatalf("the alias table is not well formed: %v", err)
	}
	for _, a := range verbAliases {
		// A row must not name a word this binary dispatches: the refusal would
		// fire on the real command and make it unreachable.
		if dispatched[a.alias] {
			t.Errorf("alias %q IS dispatched — it must not be in the refusal vocabulary (the command would be unreachable behind the refusal)", a.alias)
		}
		// `canonical` is a whole COMMAND LINE (`session log`, `tool capture`),
		// never the alias itself: a row pointing at its own word would send the
		// reader back into the same refusal.
		if a.alias == a.canonical {
			t.Errorf("%q is its own home — a word that IS the command must dispatch, not refuse", a.alias)
		}
		// A word already refused as a SPELLING needs no synonym row: two rows
		// for one word would mean two messages, and only one would ever print.
		for _, e := range undispatchedToolWords {
			if e.word == a.alias {
				t.Errorf("alias %q is also a refused SPELLING (cli_undispatched_tool_words.go) — the spelling table owns it; a second row is unreachable", a.alias)
			}
		}
	}
}

// TestVerbAliasIsRefusedByNameNoRun is the live-binary acceptance probe: the
// refusal happens before the run path, exits 1, names the canonical command,
// and leaves the isolated GOPI_HOME with nothing in it — no session directory
// and no ledger row. The three argv shapes are the operator's three natural
// spellings: the bare word, its `-h`, and a word followed by an argument.
func TestVerbAliasIsRefusedByNameNoRun(t *testing.T) {
	bin := gopiTestBinPath(t)
	for _, a := range verbAliases {
		for _, args := range [][]string{
			{a.alias},
			{a.alias, "-h"},
			{a.alias, "extra"},
		} {
			argv := strings.Join(args, " ")
			t.Run(argv, func(t *testing.T) {
				home := t.TempDir()
				dbPath := config.ProfilePathIn(home, config.MemoryDBFileName)
				before := ledgerRowCountAt(t, dbPath)
				// RAW: this probe's SUBJECT is the refused spelling, so argv[0]
				// must reach the binary unmapped — the family door would
				// rewrite `log` into the `session log` that DISPATCHES, and the
				// assertion below would read a successful command as a failed
				// refusal.
				out, err := runCLIContractProbeRaw(t, bin, home, args...)
				if err == nil {
					t.Fatalf("gopi %s: exit 0 — the alias was accepted instead of refused\n--- output ---\n%s", argv, out)
				}
				// The address, not the canonical word: the message prints a whole
				// COMMAND LINE (`gopi session log`), and a homeless row prints no
				// address at all — so the homed and homeless shapes are asserted
				// separately rather than by one string that would be `gopi ` for
				// the withdrawn pair.
				want := []string{a.alias, "--prompt " + a.alias}
				if addr := a.refusalAddress(); addr != "" {
					want = append(want, addr)
				} else if strings.Contains(out, "`gopi `") {
					t.Errorf("gopi %s: the homeless row rendered a bare `gopi ` — a hint pointing at nothing\n--- output ---\n%s", argv, out)
				}
				for _, want := range want {
					if !strings.Contains(out, want) {
						t.Errorf("gopi %s: refusal does not mention %q\n--- output ---\n%s", argv, want, out)
					}
				}
				assertNoCLIContractSession(t, argv, home)
				if after := ledgerRowCountAt(t, dbPath); after != before {
					t.Errorf("gopi %s: the refusal wrote %d ledger row(s) (before=%d after=%d) — the alias reached the run path\n--- output ---\n%s",
						argv, after-before, before, after, out)
				}
			})
		}
	}
}

// TestVerbAliasIsRefusedAfterARunFlag is the OTHER argv position. The dispatch
// chain reads argv[0] only, so `gopi --timeout 3 buildinfo` never reaches it —
// the run path's positional-verb guard (internal/config) is what refuses that
// shape, and it does so only because the alias word is in PositionalVerbs. This
// test drives the real binary, so a word added to the alias table without the
// guard list fails here (the vocabulary contract in cli_verb_guard_test.go
// fails it too, one layer earlier).
func TestVerbAliasIsRefusedAfterARunFlag(t *testing.T) {
	bin := gopiTestBinPath(t)
	for _, a := range verbAliases {
		t.Run(a.alias, func(t *testing.T) {
			home := t.TempDir()
			out, err := runCLIContractProbeEnv(t, bin, home, "--timeout", "3", a.alias)
			if err == nil {
				t.Fatalf("gopi --timeout 3 %s: exit 0 — the leftover positional was sent to the model\n--- output ---\n%s", a.alias, out)
			}
			if !strings.Contains(out, a.alias) || !strings.Contains(out, "--prompt "+a.alias) {
				t.Errorf("gopi --timeout 3 %s: refusal does not name the word and the escape hatch\n--- output ---\n%s", a.alias, out)
			}
			assertNoCLIContractSession(t, "--timeout 3 "+a.alias, home)
		})
	}
}

// TestVerbAliasCanonicalStillDispatches — the hint must be actionable. Each
// canonical word is run with `-h` and must exit 0 printing its own usage: a
// refusal that sends the operator to a command that does not answer is a second
// defect (Rule 15 — silence is the failure, not the inconvenience).
func TestVerbAliasCanonicalStillDispatches(t *testing.T) {
	bin := gopiTestBinPath(t)
	for _, a := range verbAliases {
		fields := strings.Fields(a.canonical)
		if len(fields) == 0 {
			// A HOMELESS row has no address to run — the pair withdrawn in this
			// epic (`ui`, `cli`) was two views of an engine that is still
			// reachable, so its refusal deliberately sends the reader nowhere.
			// checkVerbAliasRows is what permits the empty address, and only on
			// a retired row.
			if !a.retired {
				t.Errorf("alias %q carries no address and is not retired — a word with a command behind it must send the reader there", a.alias)
			}
			continue
		}
		t.Run(a.canonical, func(t *testing.T) {
			home := t.TempDir()
			args := append(append([]string{}, fields...), "-h")
			out, err := runCLIContractProbeEnv(t, bin, home, args...)
			if err != nil {
				t.Fatalf("gopi %s -h: %v — the refusal's hint leads nowhere\n--- output ---\n%s", a.canonical, err, out)
			}
			if !strings.Contains(out, "gopi "+fields[0]) {
				t.Errorf("gopi %s -h: output does not identify the command\n--- output ---\n%s", a.canonical, out)
			}
		})
	}
}

// TestAliasRefusalLeavesOrdinaryUnknownWordsAlone is the NEGATIVE CONTROL, and
// it is the reason the vocabulary is a table of known synonyms rather than a
// prefix or similarity rule: `gopi frobnicate9` and `gopi hi` are prompts, not
// mistyped commands, and must keep buying a run. Asserted by the run path's own
// signature (a session is minted) with the provider refused, so the probe is
// bounded and cheap.
func TestAliasRefusalLeavesOrdinaryUnknownWordsAlone(t *testing.T) {
	heavytest.Require(t, heavytest.CriterionDeviceExec)
	bin := gopiTestBinPath(t)
	for _, word := range []string{"frobnicate9", "hi"} {
		t.Run(word, func(t *testing.T) {
			home := t.TempDir()
			out, _ := runCLIContractProbeEnvTimeout(t, bin, home, cliVerbGuardRunProbeTimeout,
				"--base-url", "http://127.0.0.1:9", "--api-key", "dummy", "--timeout", "3", word) //stylegate:ignore runs the fixture probe; the minted session is the assertion
			if strings.Contains(out, "is not a gopi command") {
				t.Fatalf("gopi %s was refused — ordinary prompt text must keep running\n--- output ---\n%s", word, out)
			}
			if !sessionMinted(home) {
				t.Fatalf("gopi %s did not reach the run path — the dispatch chain swallowed a prompt\n--- output ---\n%s", word, out)
			}
		})
	}
}
