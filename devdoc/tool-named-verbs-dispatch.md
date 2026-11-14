<!-- doc-class: historical -->
# Tool-named CLI words: dispatch the ones that are commands, refuse the ones that are not

**Task:** 01a0dde8-8436-74b6-83c1-d78f909df14b (EUAUDIT slice 4 TOOLS-1, F2+F3)
**Date:** 2026-09-26

## The defect, in one sentence

The top-level help promised "a tool-named command keeps the tool's own
underscore spelling", but a tool-named word that was **not** in the dispatch
chain is not refused — it is a **PROMPT**: the dispatch chain reads the verb
from `argv[0]`, and anything it does not recognize falls through to the
bare-prompt agent path (a session directory, a live `[auto] …` ledger row,
tokens).

Measured before the fix (sandbox, provider refused):

```
$ gopi keyword_search -h | md5sum   →  6546d3ee…   (BYTE-IDENTICAL to gopi --help)
$ gopi --help | md5sum              →  6546d3ee…
$ grep -c keyword_search <(gopi --help)  →  0
$ gopi code_search EUAuditWidget    →  real agent run
```

Two distinct bugs behind one measurement, and they need opposite fixes:

| word | what it is | correct shape |
|---|---|---|
| `keyword_search` | a TOOL-DERIVED command (naming rule 1) that simply was never wired | **make it a real dispatched verb** |
| `code_search` | the code_search TOOL's **wire name**, while the CLI's command is the dash-cased `code-search` (rule 2) | **refuse it by name**, with a pointed hint — never a second spelling of `code-search` |

## What shipped

### F2 — `gopi keyword_search <query> [--root DIR]` (`cmd/agent/cli_keyword_search.go`)

A real top-level verb:

* spelled `toolnames.ToolKeywordSearch` everywhere (predicate, completion
  candidate, contract token, error prefix) — Rule 1/13/14, no literal;
* runs the **same** `tools.KeywordSearch` path the tool runs, with the same
  scope shape as the agent loop's `keywordSearchScope()` (`.` or `--root`,
  `.git/**`+`vendor/**` excluded, the loop's 50-result cap);
* exit 0 with `file:line: snippet` hits, exit 1 with the engine's own
  `No matches.` — scripts can branch on it;
* `-h` is its OWN page (acceptance's smallest repro: md5 differs from
  `gopi --help`, page contains `keyword_search` 4×).

Wiring, all two-sided pairs (a verb dispatchable in one and missing from the
other fails the build): `main.go` dispatch chain, `cli_completion.go`'s
`topLevelCommandNames`, `internal/config/help.go`'s `commandIndex`,
`cli_contracts_test.go`'s `dispatcherOfToken` + `cliContractDispatchPredicates`
+ inventory, `config.PositionalVerbs`, and the naming-exception table.

### F3 — refusal by name (`cmd/agent/cli_undispatched_tool_words.go`)

A table of the spellings that name a **real command this CLI deliberately does
not dispatch**:

| refused word | points at |
|---|---|
| `code_search` | `gopi code-search` |
| `ui-state` | `gopi ui_state` |
| `vision-analyze`, `screen-capture`, `voice-input` | the underscore spelling (rule 1 exception) |

Each row carries its canonical word, and the tests assert the hint is
**actionable** (the canonical word really dispatches) — a refusal pointing at
nothing would be worse than the run it replaces. Message shape:

```
gopi: "code_search" is not a gopi command — this one is spelled `gopi code-search` on the command line
(`gopi code-search -h` for its help). To send the word as a prompt instead, say so explicitly:
`--prompt code_search` (or the GOPI_PROMPT env var).
```

rc=1, **no session dir and no ledger row**. Crucially the vocabulary is a
**table of known spellings**, never a pattern: `gopi frobnicate …` still runs
as a prompt (measured: the refusal text does not appear, a session IS minted).
The refusal is a statement about this binary's own surface, not a guess about
operator intent.

### The run path too (`internal/config/cli_verb_guard.go`)

The dispatch chain only sees `argv[0]`, so `gopi --timeout 3 code_search` never
reaches the new predicate. The positional-verb guard (which already refused
`gopi --timeout 3 version`) now carries the same words plus
`toolnames.ToolKeywordSearch` (which the fix turned into a command). The
vocabulary is pinned against cmd/agent's table in **both directions**, so a
word added on one side only fails the build.

## The decisions worth remembering

1. **`code_search` must NOT become a dispatched alias.** devdoc/cli-naming-convention.md
   rule 2 spells the CLI verb `code-search` deliberately (a tool rename must not
   rename a user's command). Two dispatched spellings for one command is the
   duplicate surface the naming rule exists to prevent — refusal + hint is the
   right shape, and it is cheaper than the run it replaces.
2. **A refusal vocabulary is a table, not a heuristic.** "Refuse every
   underscore token" would have broken ordinary prompt text; "refuse these five
   known spellings of a real command" does not.
3. **The top-level help has a hard 160-line budget** (`topLevelUsageMaxLines`)
   and the naming-rule sentence must survive on ONE rendered line (the renderer
   wraps by width and never re-joins). Adding the new command row + the refusal
   sentence blew the budget to 168; it is back at 160 by trimming four index
   rows to their essentials (the detail always lived one hop away, in each
   command's own `-h`). Two of those trims are load-bearing for tests:
   `gopi voice_input`'s row must keep `--server`/`--client`, and the
   `keeps the tool's own underscore spelling` sentence must stay unsplit.
4. **Contract-inventory "no case" class.** `UndispatchedToolWordSubcommand`
   has no `-h` page to fingerprint, so its coverage lives in its own live-binary
   test file (`cli_undispatched_tool_words_test.go`) and
   `cliContractDispatchPredicates` skips it by name — the same shape as
   `MergerSubcommand` (a retired verb whose page deliberately does not exist).

## Evidence layout

All captures/transcripts live in the RUN SESSION dir
(`$GOPI_HOME/sessions/01a0dde8…/evidence/`, Rule 30), never in the worktree:
`art/transcript-a.txt` … `transcript-d.txt`, `art/transcript-c2.txt`
(negative control), plus the raw help oracles (`art/kw-help.txt`,
`art/top-help.txt`) and the `art/rig/` fixture tree.

## Not done (honest)

* The vocabulary covers the tool wire names whose command exists under another
  spelling. A word with **no** command behind it (`web_search` is a *dispatched*
  deprecated alias, so it is not in it) stays prompt text by design — widening
  it to "everything that looks like a tool name" would refuse prompts.
* `code_search` is refused rather than aliased to `code-search`; if a future
  decision reverses that, the naming rule doc must change first (it is the
  authority the refusal cites).
