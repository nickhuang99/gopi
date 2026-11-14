<!-- doc-class: normative -->
# CLI verb aliases — a known synonym is refused by name, not spent as a prompt

**Task** 01a0f451-da13-794a-8991-75d09a109d34 (epic: `gopi <未识别词>` 掉进裸 prompt 路径),
2026-10-01, branch `task/01a0f451da13794a899175d09a109d34`.

## The defect, measured

`gopi buildinfo` — the natural way to ask for what `gopi version` prints — was
neither a command nor a refusal. The dispatch chain (`cmd/agent/main.go`, the
`isXxxSubcommand` chain) reads the verb from `argv[0]`; a word it does not know
falls through to the bare-prompt path and buys a **whole agent run**: a session
directory, a live `[auto] <word>` ledger row, model tokens.

From the operator's own ledger (read-only, `$HOME/.gopi/memory.db`):

```
1790800640.0|failed|[auto] buildinfo
1790800720.0|failed|[auto] buildinfo
1790800842.0|failed|[auto] buildinfo
```

Three tombstones, zero output — the operator asked for a version string three
times and paid for three model runs. The existing refusal table
(`cmd/agent/cli_undispatched_tool_words.go`) could not catch this: it holds
**spellings** of a command (the tool wire name `code_search` for the dash-cased
`code-search`, the dash twin of an underscore command), each derivable from its
canonical word. `buildinfo` is not a spelling of `version` — it is a **synonym**,
and no derivation produces it.

## The mechanism

**One authoritative table, one lookup, two argv positions.**

| piece | where | what it is |
|---|---|---|
| the synonym table | `cmd/agent/cli_verb_aliases.go` (`verbAliases`) | ONE row per (alias, canonical) pair, each with its reason. Adding a synonym is one row. |
| the refusal lookup | `cli_undispatched_tool_words.go` — `undispatchedToolWordRefusalReason` | walks the spelling table **and** the synonym table. One place, so "refused at argv[0]" and "refused after a run flag" are the same fact, not two lists kept in step. |
| the projection | `undispatchedRefusedWordSpellings` | every refused word, both halves — the shape the guard mirror reads. |
| the run path's guard | `internal/config/cli_verb_guard.go` — `PositionalVerbs` + `positionalVerbAddress` | refuses the word left over AFTER a run flag (`gopi --timeout 3 buildinfo`), a position the dispatch chain never reads. |

**The corrected line must name the ADDRESS, not the token.** A synonym is not a
verb in any position, so "put the verb first" is not a fix for it:

```
$ gopi --timeout 3 buildinfo
gopi: "buildinfo" is a gopi command, not prompt text — it is a POSITIONAL here,
following the run flag --timeout, and gopi reads the verb from the FIRST
argument. Put the command first: `gopi version` (the run flag is not the problem:
`gopi --timeout <VALUE>` is correct). To send the word as a prompt instead, say
so explicitly: `--prompt buildinfo`. (For this command's own help: `gopi version -h`.)
```

The first draft printed the refused token back (`gopi buildinfo`) — which sends
the operator straight into the *other* refusal, a dead end pointing at itself.
Found by running the pair on the real binary, not by reading the code.
`positionalVerbAddress` (`internal/config`) and `verbAliasRefusalReason`
(`cmd/agent`) are the two halves of that mapping; they cannot share a symbol
(`internal/config` must not import `package main`), so they are compared in
both directions by `cmd/agent/cli_verb_guard_test.go` and driven on the binary
by `cli_verb_aliases_test.go`.

**Constants, not literals** (Rules 1/13/14). The alias word is a named constant
(`verbAliasBuildInfo`); the canonical word moved to the leaf package
`internal/cliverbs` (`cliverbs.Version`) because **two packages** must now spell
`version` — `cmd/agent`'s `CliVerbVersion` is an alias of it, exactly like
`CliVerbMemory` and `CliVerbMerge`, and `internal/config` addresses the refusal
with the same constant.

## What is deliberately NOT in it

Nothing that merely *sounds like* a command, and no prefix/similarity rule. A
refusal is a statement about **this binary's own surface** (a command exists,
under a synonym), never a guess about what the operator meant — so `gopi
frobnicate9` and `gopi hi` still reach the run path, and that direction is a
test (`TestAliasRefusalLeavesOrdinaryUnknownWordsAlone`), not a comment.
`build` was considered and rejected: no `gopi build` verb ever existed here, so
the word is prompt text as far as this binary is concerned.

## Verification (red-then-green, real binary, private sandbox)

Private sandbox profile `gopi sandbox create --label alias-refusal`; the
refusals were run with `GOPI_HOME=<sandbox>` and `gopi log --last` first printed
"no sessions found" (the profile is what it says it is).

**Red** — with the alias wiring disabled (the lookup returned `""` and the
`PositionalVerbs` row removed), on the same tree:

```
--- FAIL: TestVerbAliasIsRefusedByNameNoRun (7.97s)
--- FAIL: TestVerbAliasIsRefusedAfterARunFlag (0.01s)
--- FAIL: TestPositionalVerbGuardVocabularyMatchesDispatch (0.00s)
```

**Green** — wiring restored: `go test ./cmd/agent/ -run 'VerbAlias|PositionalVerbGuard|UndispatchedToolWord|UnknownWordIsStillPrompt|CLIUserVerbTokens|CLIToolMirror'` → `ok … 7.881s`;
`go test ./internal/config/` → `ok`, and the same five gates (see the report’s
`Ran:`/`Result:` lines for the verbatim invocations).

**Zero cost, measured before/after** — the probe's own instrument is validated
first: one real PROMPT run moved `sessions 1→2` and `ledger_rows 0→1`, so both
counters are live. The four refusal shapes then moved neither:

```
before : sessions=2 ledger_rows=1
gopi buildinfo              exit=1 elapsed=6ms
gopi buildinfo -h           exit=1 elapsed=6ms
gopi buildinfo extra        exit=1 elapsed=6ms
gopi --timeout 3 buildinfo  exit=1 elapsed=7ms
after  : sessions=2 ledger_rows=1
```

**Negative control** — `gopi frobnicate9` and `gopi hi` still mint a session
(the run path's own signature) with the provider refused; the tests assert it
on the binary, not on the classifier.

## Not done

- **Existing ledger rows are untouched.** The four `[auto] buildinfo`
  tombstones stay where they are: cleaning the ledger is a separate decision
  with its own audit meaning, and no code in this change writes to it.
- **The refusal vocabulary is not exhaustive by construction.** It refuses the
  synonyms somebody has DECLARED (one table row each). It does not attempt to
  guess further synonyms of further verbs; that would be the guessing the
  negative control exists to prevent. What is now mechanical is the part that
  can be: every declared alias is refused, in both argv positions, at zero cost,
  and the declaration has exactly one home.
