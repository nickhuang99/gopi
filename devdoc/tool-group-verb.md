<!-- doc-class: normative -->
# The `tool` group verb — gopi's local tools under one noun

**Status:** landed (command-hierarchy epic, slice 1/5).
**Surface:** `cmd/agent/cli_tool.go` (+ `cli_tool_test.go`), and one line each in
`cmd/agent/main.go`, `cli_verbs.go`, `cli_completion.go`,
`internal/config/cli_verb_guard.go`, `internal/config/help.go`.

## The problem this slice answers

The top level had grown to 43 verbs, and they were not the same KIND of thing.
Most are grouped by a **concept dimension** — `task` (planning), `session`
(execution record), `memory` (knowledge), `chat` (human interaction), `worker`
(execution control) — or by a **high-frequency closed loop** the operator
actually runs (`worker`, `merge`). A third cluster was neither: `shot`, `act`,
`capture`, `gif`, `annotate`, `proxy`, `scratch-gc`, `doccheck`,
`ledger-window`, `code-search`, `keyword_search`, `screen_capture`,
`vision_analyze`, `web_search`, `web_fetch`. Each is a **one-shot local tool**:
you reach for it once, it does one thing to this machine, and nothing about it
loops. They were crowding the index of a list whose whole value is that a
reader can find the verb they want.

The operator's ruling (recorded in
`artifacts/verb-final-table-v2.md`, the design artifact for this epic) fixed
both the grouping and its ceiling: **the change lives in the command line and
the help menu — 无非就是把一个命令映射到一个实现** ("it is nothing but mapping a
command to an implementation"). No functionality is added or removed. That
sentence is the design constraint this file obeys, and it is why the
implementation below is a *map* and not a *component*.

## What was built

One new top-level group verb, `tool` (`CliVerbTool = "tool"`), whose second
level is the local-tool vocabulary:

| block | words |
|---|---|
| **direct commands** (`gopi tool <word> [args…]`) | `code-search`, `keyword_search`, `screen_capture`, `capture`, `act`, `annotate`, `gif`, `shot`, `proxy`, `vision_analyze`, `text2img-verify`, `doccheck`†, `ledger-window`†, `scratch-gc`† |
| **grouped commands** (`gopi tool <word> <action> …`) | `sandbox <create\|run\|show>`, `text2img <input.txt> [output.png] [flags]`, `web <search\|fetch>` |

† marked **internal** on the page: their output is a contract for this repo's
own gates and scripts, not a stable surface for a person. The operator kept
them visible on purpose — **一个功能你不暴露，那么你就只能内部写 unittest** (a
capability with no door can only be tested by unit tests) — so the page lists
them *and* carries a warning block saying who they are for.

### The forwarding shape, which is the whole design

```go
pred, ok := toolSubcommandPredicates[word]   // word → the leaf's OWN predicate
...
pred(append([]string{word}, args[1:]...))    // the leaf's own argv, verb included
```

`toolSubcommandPredicates` maps each accepted word to the predicate that
**already owns** that command at top level (`isSandboxSubcommand`,
`isCodeSearchSubcommand`, …). The tail is handed over with the leaf's own word
put back in front, so the leaf sees *exactly* the argv it sees when invoked
directly: `gopi tool sandbox create -h` reaches `isSandboxSubcommand({"sandbox",
"create", "-h"})`, which is byte-identical to `gopi sandbox create -h`.

Three consequences, and each is a decision rather than an accident:

- **Nothing is re-parsed**, so nothing can diverge from the top-level path.
  There is one implementation, one help page and one exit status per command;
  the group is a second *spelling*, not a second command.
- **A new leaf costs one map entry** — no dispatch body, no usage page to
  copy, no second place to keep in sync.
- **The group cannot invent a command.** A word with no top-level verb cannot
  be added to the map usefully, and `TestToolGroupEveryWordIsADispatchedVerb`
  fails if one is (Rule 3: the tool count does not move).

### The refusal is the point

The dispatch chain reads the verb from the **first** argument, and an
unrecognized first argument is not refused there — it falls through to the
bare-prompt path and buys a **real agent run**: a session directory, an
`[auto] <word>` ledger row, tokens. So `gopi tool nosuchword` is refused **by
name**, with exit 1, listing the whole vocabulary, *before* anything else can
happen:

```
$ gopi tool nosuchword ; echo $?
gopi tool: unknown command "nosuchword" — this group knows: act|annotate|capture|code-search|doccheck|gif|keyword_search|ledger-window|proxy|sandbox|scratch-gc|screen_capture|shot|text2img|vision_analyze|web
Run `gopi tool -h` for what each one does (or `gopi nosuchword` for the command's own page).
1
```

A **bare** `gopi tool` is the same class of error: it prints the page on stderr
and exits non-zero, so a script cannot read "I forgot the word" as a
successful empty run. Both shapes are probed on the built binary, and both
assert that no session directory was minted
(`assertNoCLIContractSession`) — the expensive half of the accident.

The `"Code-Search"` spelling is refused too: the group matches exact words, and
case-folding would make the vocabulary larger than the dispatch chain's.

## Decisions worth recording

**Why a `tool` verb instead of aliasing the words under an existing group.**
`web` was the tempting host for `web_search`/`web_fetch`, and the operator
considered it — then split it: the **bare** `web` word stays the chat-UI
carrier the top-level help describes, while its **tool** face (`search`,
`fetch`) lives in the `tool` table. One word, two jobs, each with its own
entry point, and the group is where the tool face belongs.

**`text2img` has no `render` action word.** The epic's target table lists
`tool text2img render|verify`, and `render` is the one word the shipped binary
does **not** offer: `gopi text2img <input.txt> [output.png]` takes the input FILE
as its first argument, and `gopi text2img render …` would treat `render` as a
filename. Because this slice is a name→implementation map and nothing else,
inventing the word was out of scope (it would be a *new surface*, and Rule 31
would then require it to be run for real). The page therefore renders the real
shape — `text2img <input.txt> [output.png] [flags]` in the grouped block.

What the first cut got *wrong* about `verify`, and why the page changed: it
rendered `text2img <input.txt> … | verify`, i.e. it advertised `verify` as
text2img's second level. It is not — `verify`'s real word is the top-level verb
`text2img-verify`, and `gopi tool text2img verify` reads `verify` as an INPUT
FILENAME. The page was promising a spelling the binary would not run, so
`text2img-verify` was moved to its own row in the **direct** block (it IS a
direct command: `gopi tool text2img-verify 40` runs it) and the grouped block now
shows text2img's one real form. The gap is still named in the task report's
`## Not Done`.

**Why the help page is generated from the tables.** `toolLeafCommands`,
`toolGroupCommands` and `toolSubcommandPredicates` are the single definition of
the vocabulary; the page, the refusal's word list, and the completion candidate
set are all *derived* from them (Rule 13/14 — a constant that is defined and
then bypassed by a second hand-typed list is the defect). A word added to one
side and forgotten on the other fails
`TestToolGroupVocabularyMatchesHelp`, in both directions.

**Every word is a constant, never a literal** (Rules 1/13/14). The words come
from the `cliVerbs` block in `cli_verbs.go`, `internal/toolnames` for the
tool-named spellings (`keyword_search`, `screen_capture`, `vision_analyze`,
`text2img`), the sandbox leaf's own action constants (`SandboxCreateVerb`,
`SandboxRunVerb`, `SandboxShowVerb`), and `webCompletionNames` for the `web`
group's two actions. A rename lands in one place and the compiler points at
every site that must follow.

**`tool` was added to `internal/config.PositionalVerbs`.** That vocabulary is
what refuses a verb-shaped word appearing *after* a run flag (the
`gopi --timeout 3 tool code-search x` shape, which without the entry would
spend `tool` as the prompt — see `devdoc/verb-after-run-flag-guard.md`). The
group word gets the same protection as every other verb.

## Verification (Rule 31 — the surfaces were RUN, not just built)

```
$ ./bin/gopi tool -h                         # the two blocks + the internal warning
$ ./bin/gopi tool code-search -h             # forwards to the leaf's own page
$ ./bin/gopi tool sandbox create -h          # ... through the group's second level
$ ./bin/gopi tool text2img-verify 40          # ... and the pixel-level render check
font=/usr/share/fonts/truetype/wqy/wqy-zenhei.ttc size=40 pages=1 chars=4 out=[…/dumps/text2img/z_40px.png]
$ ./bin/gopi tool vision_analyze -h
$ ./bin/gopi tool nosuchword ; echo $?       # refused by name, exit 1, no session
$ ./bin/gopi tool ledger-window "2026-10-01 00:00" "2026-10-02 00:00"   # a REAL run
1790784000	1790870400	28800
$ ./bin/gopi tool sandbox show               # a REAL run
sandbox root=/home/user/workspace/gopi-sandbox source=from the built-in default prefix=sandbox- stale=72h0m0s
$ ./bin/gopi tool keyword_search toolUsageTitle   # a REAL run
cmd/agent/cli_tool.go:62: const toolUsageTitle = "gopi tool — gopi's local tools, collected under one noun"
$ ./bin/gopi keyword-search Foo ; echo $?    # the dash twin: refused, no session
gopi: "keyword-search" is not a gopi command — this one is spelled `gopi keyword_search` …
1
```

`cmd/agent/cli_tool_test.go` turns the group's central promise into an
execution-level assertion rather than a source-level one:
`TestToolGroupForwardsToTheSameCommand` runs **both** spellings on the built
binary for **every** forwarded word and compares exit status and output byte for
byte — a forwarder that dropped or reordered argv fails there while every
static check stays green.

## Not done

The `render` action word for `text2img` (above), and — reported with it — one
**pre-existing defect this slice's evidence surfaced, fixed here**: the
dash-cased `keyword-search` was not in `cli_undispatched_tool_words.go`'s
vocabulary, so `gopi keyword-search …` fell through the dispatch chain and bought
a real agent run (a session directory in the isolated profile — measured, twice),
while the module's own head states that exactly this shape must be refused by
name. The missing row was added from the constant that owns it
(`dashTwin(toolnames.ToolKeywordSearch)`, Rules 1/13/14); the same word shapes
`code_search` and `vision-analyze` were already covered. Its **second position**
came from the group's own gate rather than from my reading:
`TestPositionalVerbGuardVocabularyMatchesDispatch` refused the one-sided edit,
because a word refused at argv[0] but absent from `config.PositionalVerbs` still
runs as a prompt after a run flag (`gopi --timeout 3 keyword-search …`).
`"keyword-search"` was added to that vocabulary too, so both positions refuse it
— measured both ways on the built binary. Nothing else in this slice was
deferred: the words already present at top level are untouched and still dispatch
there, because the group is an addition, not a move.
