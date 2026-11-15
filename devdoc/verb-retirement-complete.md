<!-- doc-class: normative -->
# The verb retirement, complete: one door per command, and the old spellings refuse

Truth class: **normative** — the standing shape of the CLI surface, as of the
commit this lands in.

## What changed, in one paragraph

Every command that was reachable at the TOP LEVEL and is now reached through a
family — `log`/`stats`/`raw`/`resume`/`checkpoint` → `session`, `archive` →
`task`, `brew`/`memsync` → `memory`, `setup`/`completion` → `config`, and the
19 local tools (`capture`, `shot`, `text2img`, …) → `tool` — was REMOVED from
the dispatch chain and given a refusal row. The capability is untouched: every
predicate, run function and help page is still in the tree, and `gopi session
log` / `gopi tool capture` run the same code they always did. What is gone is
the second spelling. Two commands have no successor at all (`gopi ui`,
`gopi cli` — two of the three views of one engine, withdrawn rather than
moved), and their refusal says so instead of pointing at nothing.

**Why the ordinary alias arrangement was rejected** (operator ruling,
2026-10-05): 不要再残留别名，否则后患无穷 — a leftover alias is not
compatibility, it is harm that outlives the convenience — and 一次性的做完.
So this is not a deprecation window: the words refuse TODAY, with the address
printed.

## The mechanism, and why the two halves cannot ship apart

A verb removed from `cmd/agent/main.go`'s dispatch chain and NOT added to a
refusal table falls through to the **bare-prompt path** — one real model call,
a minted session, an `[auto] <word>` ledger row. Measured 2026-09-30: three
`gopi buildinfo` invocations became three failed tasks. That is why every word
below was moved in ONE edit: 摘除 the dispatch entry, 接入 the refusal table.

The refusal has TWO entry points, because a command word can arrive in two
argv positions:

| position | reader | vocabulary |
|---|---|---|
| argv[0] — `gopi log` | `isUndispatchedToolWordSubcommand` → `undispatchedToolWordRefusalReason` | `cmd/agent/cli_undispatched_tool_words.go` + `cmd/agent/cli_verb_aliases.go` |
| after a run flag — `gopi --timeout 3 log` | `internal/config`'s positional-verb guard | `PositionalVerbs` + `aliasAddresses` |

Both halves are pinned against each other by
`cmd/agent/cli_verb_guard_test.go`, in BOTH directions, on the real binary.

## The address is a COMMAND LINE, not a word

`verbAlias.canonical` now holds a whole address — `session log`,
`tool capture`, `tool web search` — and `programAddress` assembles
`gopi <address>` in exactly one place. Before this change a two-word home could
only be expressed by a second `homeProg` field, and the refusal's message
rendered it into a slot that read as *one word plus a stray argument*.

The retirement added the row's third shape:

- **homed** (`canonical` set, `retired: true`) — the door MOVED; the message
  leads with the retirement and prints the address verbatim;
- **homeless** (`ui`, `cli`) — no successor exists, so the address is empty and
  the message renders no bare `gopi ` (Rule 15: a hint that leads nowhere is
  worse than no hint). `checkVerbAliasRows` refuses an empty address on any row
  that is not marked retired;
- **synonym** (`buildinfo` → `version`) — the word never dispatched; the
  shipped message shape is unchanged.

Every address is executed by a test (`checkVerbAliasRows(dispatches)`), so a
row whose hint does not run fails the suite — Rule 31 applied to a help string.

## The surfaces that had to move with it

A word's reach is not just its dispatcher. Each of these carries the SAME
vocabulary and had to be edited in the same commit, or the tree would advertise
what the chain no longer dispatches:

- `internal/config/help.go`'s `commandIndex` — the `gopi -h` index. Its rows
  were rewritten to the surviving address (and the tool rows now start with the
  group word, which is what the census reads).
- `cmd/agent/cli_completion.go`'s `topLevelCommandNames` + `subcommandSets` —
  tab completion. An entry here is a word the operator is TOLD to type at the
  moment they are deciding, so a retired word in this list would actively teach
  an error.
- `cmd/agent/cli_contracts_test.go`'s `cliContractDispatchPredicates` (shrunk
  with the chain) and `cliContractInventory` (its entries were RE-ADDRESSED —
  `{argv: "log", args: {"log","-h"}}` became `{argv: "session log", args:
  {"session","log","-h"}}` — with the fingerprints kept, so the same live-binary
  contract still pins each page).
- `cmd/agent/cli_tool.go`'s page: the sentence "Every command below is ALSO its
  own top-level verb" was the leftover theoretical basis for the aliases, and
  is now the opposite claim (the group is the only door).
- `COMMANDS.md` (generated) and `devdoc/gopi-verb-census.md` (generated).

## Two generators had to be repaired, not just regenerated

The floors and scans inside the generators encoded the OLD size of the surface,
and each failed LOUDLY (which is the read-floor working):

- `scripts/g7-verb-census.sh` refused twice — `parsed only 0 dispatch
  predicates` (its `awk` still keyed on `isCompletionSubcommand`, which is no
  longer the chain's first predicate, so it found no chain at all) and then
  `derived only 15 verbs` against a floor of 30. Its ADVERTISED floor (30) and
  union floor (30) were also re-sized, and its HELP-ONLY scan had to learn the
  group argv shape: it demanded `"-h"` immediately after the FIRST element, so
  every `{CliVerbTool, CliVerbCapture, "-h"}` entry derived `help_only=0`
  silently.
- `cmd/agent/cli_verb_census_test.go`'s `censusMinRows` fell from 25 to 8 for
  the same reason (12 verbs remain, measured at this commit).

## Not done

- `gopi ui` and `gopi cli` keep their Go implementations and their usage
  constants (`termUsage`, `uiUsage`) even though nothing dispatches them. That
  is deliberate — the epic removes ENTRY POINTS, never capabilities — but it
  leaves two now-unreachable help pages whose `cliUsageTitle`/`uiUsageTitle`
  helpers are referenced only by the refusal tests. Removing the implementations
  would be a separate decision with its own review.
- The `web_search` / `web_fetch` predicates remain in the dispatch chain as the
  PREVIOUS epic's refused spellings (they exit with a pointer and are
  advertised nowhere). They are the same class as the words this change
  retires, but they predate it and were left alone rather than folded in.

## The positional guard had a dead end of its own

Found by RUNNING the pair, not by reading the code (Rule 31): `gopi --timeout 3
ui` printed

    Put the command first: `gopi ui`

— an address that is itself refused. The guard's `aliasAddresses` table carried
`"ui": "ui"`, and the corrected-line slot rendered it. Fixed by letting the
address be EMPTY for the withdrawn pair (the same representation the argv[0]
table uses), with two dedicated message shapes (`positionalVerbHomeless…`) that
say plainly that no position fixes the word. `config.AliasAddresses()` now
exposes the table so `cmd/agent`'s vocabulary contract compares BOTH halves on
the real binary: the word set and the address set.

## The spelling table's addresses were retired words too

Same class, second table, found the same way (by running every refusal and
reading the corrected line): `gopi code_search` said "this one is spelled `gopi
code-search`" — and `gopi code-search` refuses. The SPELLING table
(`cli_undispatched_tool_words.go`) corrected a word to a flat command word that
the retirement had just removed, so all six tool-derived rows (`code_search`,
`keyword-search`, `vision-analyze`, `screen-capture`, `voice-input`, and the two
web dash twins) were sending the operator from one refusal into the next.

Three fixes, in the order the code needs them:

1. `internal/cliverbs` gained the words that were still literals in the guard's
   address table (`Tool`, `Web`, `WebSearch`, `WebFetch`, `CodeSearch`) — Rule 14:
   a value two packages must spell lives once, in the leaf package.
2. `config.aliasAddresses` got a G group for the spelling twins, each pointing at
   the whole surviving line (`tool code-search`, `tool web search`), and the
   withdrawn pair's entry became the EMPTY string so the corrected-line slot is
   not rendered at all (two new message shapes say plainly that no position
   fixes the word).
3. `undispatchedToolWord.refusalAddress` now derives `gopi tool <word>` from the
   constants for a tool-derived row, while `ui_state` — the one tool-SHAPED word
   that is still dispatched — keeps its own word as the address.

The contract that stops this recurring walks BOTH refusal tables and both argv
positions: `cmd/agent/cli_verb_guard_test.go` compares each refused word's
argv[0] address with `config.AliasAddresses()[word]`, in both directions, so a
word refused in one shell and un-addressed in the other fails the build rather
than waiting to be discovered by hand.

## The prompt, the pages and the hints were routing into the refusals

A retirement that stops at the dispatch chain leaves a worse machine than the
one it started with: the words stop working, and everything that TEACHES the
words keeps teaching the retired spelling. Every channel that names a command
was found by grepping for the retired word and repaired, and each one is a
channel the model or the operator reads FIRST:

- **The system prompt** (`internal/agent/loop.go`, `selfKnowledgeRules`) — the
  only channel that teaches a tool (Rule 11). It said `log`/`raw`/`stats`/
  `capture`/`screen_capture`/`act`/`sandbox`; it says the live addresses now,
  built through `cliverbs.SessionAddress` / `ToolAddress` so the prompt and the
  refusals cannot drift. The test asserts the ADDRESSES are present and the
  retired forms are absent.
- **The tool descriptions** (`capture --window/--self/--area`, `act`, `shot`) —
  "the CLI spelling is `gopi capture --window`" now says `gopi tool capture
  --window`. These are the strings the model reads before deciding how to drive
  the desktop.
- **Every usage page** in `cmd/agent` — 31 files, the `gopi X — …` first line
  and the `Usage:` blocks.
- **The refusal texts themselves** — see the two sections above.
- **`CONFIG.md`** and **`devdoc/session-verb-family.md`** — the configuration
  doc's `gopi log --last` examples, and a supersede note on the historical doc
  whose standing claims the retirement contradicts.

Nothing in the retirement is allowed to be a dead end in EITHER direction: a
word the operator can type must reach a page, and a page that names a word must
name one that works.

## Not Done — the honest remainder (the suite is NOT green)

The branch first shipped the PRODUCTION half of the retirement, proved by running
it, and left **5 of 6 packages FAILED / 263 tests failing** — every one of them a
test that drove the REAL BINARY with a retired spelling. That half is now
DELIVERED in `fc730b22`; this section is the measured remainder after it.

### What `fc730b22` did (the test face, through ONE door)

`cmd/agent/cli_family_address_test.go` is the mapper: `familyAddressArgs` takes
the argv a test types and, when `argv[0]` is a retired flat word, replaces it
with the family address the refusal itself prints — **driven by production's own
`verbAliasCanonical` table**, never by a second list typed in a test. `runGopiMapped`
is `testutil.RunGopi` with the door mapped. 147 call sites across 43 files now
go through it, and the runners that bypass `RunGopi` were gated the same way
(`runGopiInDir`'s `exec.Command`, the `cli_sandbox_ledger_test` direct exec, the
G5 probe's `run`/`runLive`).

**No assertion was deleted.** That was the whole point: a test that reached
`gopi capture -h` and asserted the capture usage page still asserts that page,
now reached through `gopi tool capture -h`. Deleting an assertion would hide a
behaviour; changing the door restores the behaviour to the assertion.

Constant/collection drift fixed in the same commit: `cliContractInventory`
re-addressed (`{argv: "log"}` → the family address, fingerprints kept),
`cliContractInventoryToken` gained the family arm (four tool-derived exception
words are reached through the `tool` group now, so an argv[0]-only comparison
reported them unreachable — false about coverage, true about the comparison),
`cli_completion_exceptions_test`'s five predicates followed, `cli_capture_test`
asserts `programAddress(toolHome(...))`, and the ONE production change:
`cli_checkpoint.go`'s `checkpointResumeRemovedPointer` no longer offers the
retired top-level `gopi resume <log>` as a live spelling (it is refused by name,
so the old pointer was a dead end pointing into a second refusal).

### The measured remainder — 13 tests, each read and classified

Re-measured after the commit, `go test ./cmd/agent/ -run '<the 15>'`:

| test | class | what it needs |
|---|---|---|
| `TestG5InventoryMatchesTheHelp` | stale inventory DOC | `scripts/g5-chat-web-help-surface.sh` must re-emit the row as `gopi tool proxy` |
| `TestG5InventoryListsEveryGapTheTestsCannotCover` | stale inventory DOC | same table's gap row |
| `TestG4HelpSurfaceMatchesInventory` | stale inventory DOC | the session group's rows still say `gopi log <session-id>`; must be the family address |
| `TestG4HelpSurfaceInventoryTableMatchesHelp` | stale inventory DOC | `devdoc/gopi-session-help-surface-inventory.md` — 86 rows vs 87 advertised, flat `gopi checkpoint …` rows |
| `TestCheckpointUsageLinesAreDispatchable` | derivation keyed on the old spelling | "only 0 checkpoint Usage line(s) were inspected" — it derives the lines to probe from the page, and the page now spells `gopi session checkpoint`, so the derivation finds none (a BLIND derivation, loudly — the read-floor working) |
| `TestCLIContractInventoryMatchesDispatch` | count off by 2 | `cmd/agent/main.go` has 20 dispatch predicates, `cliContractDispatchPredicates` tracks 18 — two missing cases |
| `TestBashScriptStructure` | generated completion script | the bash `compgen -W` word list no longer matches the assertion's list |
| `TestKeywordSearchRunPrintsHitsAndNoMatches` | test drives the binary DIRECTLY | this one spawns `gopi keyword_search …` without going through a mapped runner |
| `TestProxyBusyPortNeverClaimsListening` | direct-drive, refusal text asserted | the refused start prints the retirement line, not the bind failure |
| `TestSandboxDirFlagDecidesTheRoot` | direct-drive at `:249` | the call now reaches `gopi tool sandbox create` and exits 1 — the mapping of an ALREADY-family argv is the thing to check |
| `TestAnnotateVerbRunsARealArgvEndToEnd` | assertion on the page HEAD | the usage text IS reached and DOES print; the sub-case `-h is the early return` fails on the head spelling |
| `TestSyncBarePeersTestCoversEveryChangedFile` | coverage list | names 73 changed `cmd/agent` files not in `syncPeersBareSourceFiles`/`syncPeersBareTestFiles` |
| `TestChatStatus_LiveBlockKeepsTheStateWord` | **NOT this epic** | the row renders `resp\u00a0` where the assertion wants `responding\u00a0` — a width/truncation question in the chat status row, reached by the retirement only in that the suite around it moved. Suspected pre-existing; it must be measured against `master` before it is charged to this epic |

**None of these is evidence the retirement is wrong** — each was read, and each
fails on a spelling, a table, or a derivation that still encodes the OLD surface.
But a branch that leaves them red is not mergeable, and this report will not
pretend otherwise (Rule 26's corollary: a fix that overstates its coverage hides
the defect).

Of the six packages the earlier run measured, this commit closes the
retired-spelling class across **`cmd/agent`'s shared runners**; the doc/table
class (4 tests above, all four in the same two generator outputs) and the
remaining direct-drive sites are what is left.

Dispatched as its own row: **`01a10e02-bf65-77c7-9a96-892dd06d067c`** ("测试面跟随
退役重写"), with the failure classes, the file list, the acceptance criteria and
the anti-regression test it must add.

## Evidence (self-filed)

- `gopi log` → exit **1**, `gopi: "log" is RETIRED — use `gopi session log` …`
- `gopi capture` → exit **1**, same shape, address `gopi tool capture`
- `gopi ui` → exit **1**, "WITHDRAWN — … no position fixes it" (homeless shape)
- `gopi --timeout 3 ui` → the withdrawn form; the pre-fix output was
  "Put the command first: `gopi ui`" — a corrected line pointing at the
  refusal it came from (found by running the pair, not by reading the code)
- `gopi session log --last` → exit **0**
- all **27** surviving family addresses → `-h` exit **0** each
- `gopi doccheck` → "the devdoc corpus is consistent"
- `go test ./cmd/agent/ -run TestPositionalVerbGuardVocabularyMatchesDispatch` → ok
- `go test ./cmd/agent/ -run TestCLIContract` → ok (7.7s)
- Census regenerated: `COMMANDS.md`, `devdoc/gopi-verb-census.md`,
  `scripts/g7-verb-census.sh`
- Commit: `a4314fdf` on `feat/verb-retirement-complete` (production half)
- **Test face: `fc730b22`** — the door mapper + 147 call sites + the drift fixes.
  `go build ./...` OK (`CGO_ENABLED=0`). Re-measured after that commit:
  `go test ./cmd/agent/ -count=1 -run '<the 15 named above>'` → 13 FAIL, 2 PASS
  (`TestCLINamingExceptionsAreStillDispatchable` and
  `TestHelpPagesAdvertiseTheirOwnSubcommands` went green with this commit).
- `gofmt -l cmd/agent internal` — the only dirty file this branch had touched was
  `cli_tools_maintenance_help_surface_basic_test.go` (a map-literal alignment the
  edit disturbed); `gofmt -w` applied, now clean.
- The 13 remaining failures were each read at the assertion site, not inferred
  from the test name; the table above carries the class and the missing work.
