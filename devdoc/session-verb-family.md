<!-- doc-class: historical -->
# The `gopi session` verb family — one implementation, six spellings

Truth class: **historical** — the record of the change that introduced the family
and of the shape it landed (`devdoc/DOC-INDEX.tsv` carries the row the classifier
wrote). The standing claims in it are backed by the guards listed below, each of
which fails the build if the shape drifts.

> **SUPERSEDED IN PART (2026-10-05, epic verb-retirement).** The six spellings
> are FOUR now: the top-level aliases this document describes (`gopi log`,
> `gopi stats`, `gopi raw`, `gopi resume`) were REMOVED from the dispatch chain
> and refuse by name, pointing here — `gopi session log` is the ONE spelling,
> not an alias of a top-level verb. The family, the run functions and the pages
> are unchanged; what the retirement took away is the second door. Read the
> statements below as the record of what was true when this landed, and
> `devdoc/verb-retirement-complete.md` for the current shape.

## What exists now

`gopi session` is a GROUP verb (like `gopi task`, `gopi memory`, `gopi web`): the
bare word prints the family page, the second level names the verb.

```
gopi session list [--mode chat|web] [--limit N] [--json]   NEW — reads the chat index
gopi session show <id> [--json]                            NEW — one session's summary
gopi session log <id> [view flags]                         ALIAS → top-level `gopi log`
gopi session stats <id> [flags]                            ALIAS → top-level `gopi stats`
gopi session raw <id> [round] [part]                       ALIAS → top-level `gopi raw`
gopi session resume <id> [--task T]                        ALIAS → top-level `gopi resume`
```

**Nothing was removed.** `gopi log`, `gopi stats`, `gopi raw` and `gopi resume`
are still dispatched top-level verbs with their own argv predicates and their own
run functions; `gopi session <verb>` reaches those same functions.

## Why a family, and why the aliases are the interesting half

The six spellings answer one question about one operand. `gopi session -h` is the
page that teaches them together, and — the part that matters — `gopi session list`
**prints the very operand every sibling takes**: a full session uuid. The
discoverability is structural, not documentary: the output of one sub-verb is the
input of the next.

## Implementation: where each piece lives

| piece | file | one line |
|---|---|---|
| family page, dispatch, `list`/`show` | `cmd/agent/cli_session.go` | the group |
| group-verb constant | `cmd/agent/cli_verbs.go` (`CliVerbSession`) | Rule 1b |
| dispatch chain entry | `cmd/agent/main.go` (`isSessionSubcommand`) | one predicate |
| tab completion set | `cmd/agent/cli_completion.go` (`sessionGroupCompletionNames`) | references the one slice |
| command index line | `internal/config/help.go` (`commandIndex`) | one entry for the family |
| positional-verb guard | `internal/config/cli_verb_guard.go` | `session` admitted |
| chat-index query + reader | `internal/tools/chat.go` | `ChatListFilter`, `ChatSessionRows`, `ChatShow` |

### The aliasing is a substitution, not a reimplementation

Each alias arm is ONE statement — the top-level run function, called with the
same argv minus the group word:

```go
case CliVerbLog:    runLogSubcommand(rest)
case CliVerbStats:  runStatsSubcommand(rest)
case CliVerbRaw:    runRawSubcommand(rest)
case CliVerbResume: os.Exit(runResume(rest))
```

There is no second flag parser, no copied usage text and no second renderer, so
the two spellings cannot drift. What proves it is a **behavioural byte
comparison** (not a source reading): `gopi log <id>` and `gopi session log <id>`
were both run against a real session and their outputs `diff`ed — empty — with
matching sha256, and the same for `stats` and `raw`. The measurement is in the
task report; the source-level assertion
(`TestSessionAliasRunsTheSameFunction`) exists only as a regression guard for the
one thing a rendered page cannot distinguish: one implementation from two
identical ones.

### `--mode` is the chats table's own vocabulary (Rule 13)

`ChatListFilter` gained a `Mode` field whose two accepted values are the
constants the `chats` schema's CHECK constraint already names —
`tools.ChatModeCLI` (`chat`) and `tools.ChatModeWeb` (`web`). The values are
never re-typed in the CLI; `session list --mode` passes the string through to the
query. A value that is neither is a **named error**
(`chat: list: invalid mode "chet" (want chat|web)`), never an empty list: a
typo'd filter and an empty table render identically, and that ambiguity is the
defect Rule 15 names.

### One query, two renderings

`ChatList` (the human table, also behind `gopi chat list`) and `ChatSessionRows`
(the data behind `--json`) build the same WHERE clause, in the same function
shape, over the same table, with the same `ORDER BY started_at DESC`. They cannot
disagree about which sessions matched. The row's lifecycle word comes from the
tools layer's own constants (`ChatStateRunning` / `ChatStateEnded` / the recorded
end reason), so the table and the JSON describe a row in one vocabulary.

### Why `ChatList` takes a struct now

It took `(query, model string, limit int)`. Adding a third selector as a
positional string would have produced `ChatList("", "", "chat", 0)` — a call site
where no reader can tell which empty string is which. The filter struct makes the
call site self-describing and means the next selector costs no new parameter.
`ChatListFilter{}` is the "everything, default cap" spelling the tests use.

## The honest boundary: what `session list` does NOT show

**The chat index covers conversations.** A row is written by `gopi chat`, `gopi
web`, `gopi ui` and `gopi cli` — the interactive surfaces. A **task run** or a
**one-shot prompt** has a session log (`~/.gopi/sessions/<id>/session.jsonl`) and
is fully readable by `gopi session log` / `stats` / `raw`, but it has **no
`chats` row**, so it does not appear in `session list`.

That is not a filter this task forgot to write: it is where the index's writer is
today. The doors to those sessions are `gopi log --last` and
`gopi task show <id>`, and the family page says so in its Scope paragraph so a
reader learns it from the help rather than from an empty-looking list.

## Sibling task `01a0fd14` and `session replay`

`session replay` is re-homed by a sibling task; this family does not implement
the replay renderer. What this task guarantees is the **compatibility that
re-homing needs**: `session list` prints full session uuids — byte-for-byte the
operand the replay verb takes — and those ids were verified to feed `session log`
end to end. When replay lands under the group, it becomes one more arm of the
same switch plus one line on the family page; the gate
`TestSessionFamilyUsageAdvertisesEverySubVerb` will demand that line.

## Guards this shape ships with

| guard | file | what it fails on |
|---|---|---|
| `TestSessionFamilyUsageAdvertisesEverySubVerb` | `cmd/agent/cli_session_test.go` | a sub-verb in the vocabulary with no line on the family page, or a `--mode` value the page does not teach |
| `TestSessionSubVerbVocabularyIsSingleSourced` | same | completion and dispatch reading two different vocabularies |
| `TestSessionAliasRunsTheSameFunction` | same | an alias arm that re-parses argv instead of calling the top-level run function |
| `TestSessionModeFilterMatchesChatIndex` | same | the table face and the JSON face disagreeing about a filtered row set |
| `TestSessionModeFilterRefusesATypo` | same | a typo'd `--mode` answered with an empty list instead of a named error |
| `TestSessionStateWordIsOneVocabulary` | same | table and JSON rendering a row's lifecycle differently |
| `TestTopLevelUsageAdvertisesEveryDispatchedVerb` | `cmd/agent/usage_test.go` (existing) | the family missing from `gopi -h` (memory #1252's P0 shape) |
| `TestVerbCensusMatchesTheDispatch` | `cmd/agent/cli_verb_census_test.go` (existing) | a dispatched verb with no row in `devdoc/gopi-verb-census.md`, whose `session` row this change adds |

## A defect this change found in the census GENERATOR

`scripts/g7-verb-census.sh` extracted main.go's dispatch chain with `grep -m1`,
which reads only the chain's first line. main.go's chain is now wrapped across
three lines, so every predicate below the first line (`sandbox`, `setup`,
`scratch-gc`, `ui`, `ui_state`, `voice_input`, `keyword_search` — and the new
`session`) was regenerated with `dispatched 0`, while the Go gate that consumes
the table (`censusChainNames`) reads the WHOLE file. The generator and its gate
disagreed about the same tree.

The fix is in this change: the chain is read to its closing brace. Measured on
the same tree, the captured chain's unique predicate count went **38 → 47**, and
`isSessionSubcommand` is in it.

## Measurements taken (this task's own run)

```
$ gopi session list --limit 1000 | head -1
233 chat(s):
$ sqlite3 ~/.gopi/memory.db "SELECT COUNT(*) FROM chats;"
233
$ gopi session list --mode chat --limit 1000 | head -1
178 chat(s) (filtered):
$ sqlite3 ~/.gopi/memory.db "SELECT COUNT(*) FROM chats WHERE mode='chat';"
178
$ gopi session list --mode web --limit 1000 | head -1
55 chat(s) (filtered):
$ sqlite3 ~/.gopi/memory.db "SELECT COUNT(*) FROM chats WHERE mode='web';"
55
```

Alias equivalence, against session `4ca8cd29-019c-4dda-a000-6dafd782bf91` (a real
running chat row from this profile):

```
log:   IDENTICAL  sha256 94a26501b84959aec390e663a7e6146db6ed80ab2398b9559d5a87b6c603fb27
stats: IDENTICAL  sha256 6c26a105348b03beda1295a52096da97f35f4c164270f26f94e61edd0b2154ff
raw:   IDENTICAL  sha256 cb0983ca44ba3abf2102e504c0bfbbfa3b961b80906296f1712b48128209f2ac
```

The count comparisons are against the profile's real ledger. A census figure in
this document is re-derivable with the command beside it (Rule 38).
