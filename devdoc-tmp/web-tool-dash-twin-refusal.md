<!-- doc-class: normative -->
# The web tool spellings' dash twins — refused by name, and the page that promised them

This document is one run's RECORD: the defect it measured, the fix it landed, the
probes it drove on the real binary, and what it did not finish. It is the
follow-up the double-spelling slice filed (`devdoc/verb-double-spelling-slice.md`,
"Known hole" section; task `01a109bc…`).

Branch: `task/01a10b4b1815746bac7a49c47a00d75b`, base `d60dc74d`.

## The defect, measured before the fix

Four spellings of the two web tool commands, each run as `argv[0]` in an
ISOLATED `GOPI_HOME` with the ambient `GOPI_*` / `ANTHROPIC_*` / `DEEPSEEK_*` /
`DISPLAY` masked (a task-run child exports credentials and a settings path;
inheriting them is the difference between "a refusal" and a paid model call):

| word | measured | verdict |
|---|---|---|
| `web_search` | `exit=1` · `gopi web_search: retired — use \`gopi tool web search\`` | refused ✔ |
| `web_fetch` | `exit=1` · `gopi web_fetch: retired — use \`gopi tool web fetch\`` | refused ✔ |
| `web-search` | `exit=1` · `Error creating agent: failed to create DeepSeek Claude client: deepseek: ANTHROPIC_AUTH_TOKEN not set` | **the run path** ✘ |
| `web-fetch` | same as above | **the run path** ✘ |

The message on the last two rows is the RUN PATH's own: with credentials in the
environment the same argv is a **paid model call plus a minted session**, not a
refusal — the failure mode `refuseWebToolFlatVerb` exists to prevent, reached
through the other separator. The `-h` shapes were worse in a quieter way:
`gopi web-search -h` printed the **top-level help page** as if the word were
ordinary prompt text, i.e. no refusal at all.

**ROOT CAUSE (5-why).** The flat words are refused by their OWN arm
(`refuseWebToolFlatVerb`) rather than by a vocabulary: `web_search`/`web_fetch`
are dispatched (the `isXxxSubcommand` chain), and the refusal lives in
`cmd/agent/cli_websearch.go` / `cli_webfetch.go`. Their dash twins are therefore
in **no** list — not the dispatch chain, not the spelling table
(`cli_undispatched_tool_words.go`), not `config.PositionalVerbs` — so
`main.go`'s chain matched nothing and the run path took the word as the prompt.
The gap is structural, not accidental: a refusal implemented as a *bespoke arm*
is invisible to the four derived vocabularies the slice audits, so the slice's
own P4/P5 pair invariant could not see these two words.

**The second arm of the defect (the page).** The closing paragraph of the three
web pages read "The legacy flat verb gopi web_search still works (deprecation
note on stderr) — gopi web search is the canonical form", while the binary
refused that very word and exited 1. `gopi web_search -h` printed the
retirement line AND then that sentence — the page contradicting its own binary.
Reached from three surfaces: the flat word's own `-h`, its fetch sibling, and
`gopi web <unknown-subverb>` (which prints `webGroupUsage`).

## The fix — an arm in the EXISTING refusal table, not a new mechanism

Rule 3/35: no new tool, no new mechanism, no new dependency. The two words are
rows in the ONE spelling vocabulary that already refuses a spelling by name
(`undispatchedToolWords`), which is what puts them in both argv positions by
construction:

1. **`cmd/agent/cli_undispatched_tool_words.go`** — two rows, both words
   DERIVED (`dashTwin(toolnames.ToolWebSearch)` / `dashTwin(toolnames.ToolWebFetch)`),
   so a tool rename cannot leave the refusal naming a spelling nothing knows.
   They are refused at `argv[0]` by the chain's own `isUndispatchedToolWordSubcommand`
   arm (already in `main.go`'s dispatch line) — no line was added to the chain.
2. **A row now carries its ADDRESS.** `undispatchedToolWord` gained `homeProg`:
   the FULL program spelling the refusal prints. Empty for every pre-existing
   row (their message is byte-identical: `gopi <canonical>` derived through the
   package's own `gopiProgramWord`), and set for the two new rows to
   `webSearchGroupProg` / `webFetchGroupProg` — the SAME constants the flat-word
   refusal and the contract test read. This is needed because their canonical
   word (`web_search`) is itself refused: a refusal pointing at another refusal
   is a dead end (Rule 15).
3. **`internal/config/cli_verb_guard.go`** — the two dash twins joined
   `PositionalVerbs` (derived through a new leaf-local
   `positionalVerbDashTwin`, since `cmd/agent` is package main and unimportable),
   so the shape after a run flag (`gopi --timeout 3 web-search`) is refused too.
4. **The three pages** corrected: the promise is replaced by what the binary
   does (the flat word is retired and refused; its dash twin is refused the same
   way; the live spelling is named, from the group-word constants).

## The refusal now prints (the same sentence shape as the flat words)

```
$ gopi web-search "x"
gopi: "web-search" is not a gopi command — this one is spelled `gopi tool web search` on the command line (`gopi tool web search -h` for its help). To send the word as a prompt instead, say so explicitly: `--prompt web-search` (or the GOPI_PROMPT env var).

$ gopi --timeout 3 web-search
gopi: "web-search" is a gopi command, not prompt text — it is a POSITIONAL here, following the run flag --timeout … Put the command first: `gopi web-search` …
```

**The run-flag shape is a TWO-LEVEL refusal, and both hops were walked.**
The first message is `internal/config`'s positional-verb guard, which sees only
a word that LOOKS like a command and corrects the POSITION — it cannot know the
word is retired (that package cannot import `cmd/agent`). It tells the reader to
put the command first, and the command it names is the one hop #2 answers:

```
$ gopi web-search          # hop 2 — the address hop 1 handed over
gopi: "web-search" is not a gopi command — this one is spelled `gopi tool web search` on the command line …
```

A reader who follows hop #1's advice verbatim lands in a refusal that names the
LIVE home, not a dead end. `cmd/agent/cli_verb_guard_test.go`'s probe-pair test
asserts hop #1's behaviour only for the message shape
(`is a gopi command, not prompt text`), so hop #2 is asserted here by the arms
test rather than assumed.

## The acceptance probes (Rule 31 — the surface was RUN)

Probe script: `artifacts/probe-webtool-spellings.sh` (session
`01a10b4b-1815-…`), reporting exit status, sessions minted under BOTH store
shapes, and the first output line. Raw before/after in
`artifacts/webtool-spellings-{before,after}.txt`; the stale page text in
`artifacts/stale-usage-page-before.txt`.

**after** (four words × {run, `-h`}): `web_search`/`web_fetch` refuse (exit 1,
run) and answer their page (`-h`, exit 0); `web-search`/`web-fetch` refuse in
BOTH shapes with the named message naming `gopi tool web search` / `gopi tool web
fetch`; **sessions = 0 in all eight**.

### RED PROOF (the arm is load-bearing) — and the honest nuance it exposed

Renaming the new row's `word` (i.e. removing the arm, nothing else changed) and
re-running the acceptance test, `artifacts/redproof-row-removed.txt`:

```
--- FAIL: TestWebToolFlatTwinsAreRefusedByTheirOwnArm/web-search
    gopi web-search hello: refusal does not mention "gopi tool web search"
    gopi web-search -h: exit 0 — a refused spelling must exit non-zero
```

Two properties fail together, and they are exactly the two the task named:
the **live-home address** is absent, and the **`-h` shape stops refusing** —
without the row, `gopi web-search -h` exits **0** and prints the top-level page,
the pre-fix behaviour.

**The nuance, stated because a reader would otherwise over-read that red:** on
the PRE-FIX binary the bare `gopi web-search` also reached the run path (the
measured `Error creating agent: …`). By the time the red proof was taken,
`PositionalVerbs` had already received both dash twins, so
`gopi web-search <arg>` was refused by `internal/config`'s positional-verb guard
— a refusal that does NOT name the live home and that does not fire at all for
`-h`. So the row is load-bearing for (a) the live-home address and (b) every
shape the config layer does not guard, `-h` included; the config row is
load-bearing for the run-flag shape, and neither arm alone closes the hole.
Both are asserted, by different tests, for that reason.

## What this does NOT fix (honest)

- **A THIRD page carried the same stale claim, and it was found only by running
  the full package set — not by grepping for the sentence.** `COMMANDS.md` §3.5
  said the flat spellings "still route for existing scripts" and opened with a
  deprecation note. It is hand-written prose OUTSIDE the generated verb-index
  block, so no generator rewrites it; the sentence does not share the wording the
  three `usage` strings use, so the grep that found those missed it. It is
  corrected here (and its two `file.go:NNN` anchors re-pointed, since the gate
  resolves them), and the two `gopi web-search` / `gopi web-fetch` spans it now
  needs are declared as reviewed non-verb exemptions in
  `internal/standards/commands_doc_gate_test.go`. **The lesson: a grep for one
  sentence is not a search for one CLAIM.**
- **`internal/config/help.go:602` still shows `gopi web search "<query>"`** as a
  worked example. It is a LEGAL spelling (the `web` group dispatches `search`),
  so it is not a contradiction with the binary — but it is not the live
  `tool web search` either. Unifying the two help-surface producers is a
  help-surface decision, not a refusal fix; recorded in the devdoc rather than
  edited here.
- **`cmd/agent/cli_web.go`'s group usage text and `internal/config/help.go` are
  two producers of one surface.** Only the first prints the paragraph this task
  corrected; the second never carried the stale promise, so it needed no change —
  but a future edit to one will not reach the other.
- **The dash twins are refused, not ALIASED.** `web-search` now says "no such
  command" rather than quietly running the web search. That is the deliberate
  choice (the slice's P4 invariant: one command, one spelling), and it is the
  spelling the task asked for; a reader who wanted an alias is reading the wrong
  task.
- **`positionalVerbDashTwin` is a second implementation of `dashTwin`.** It has
  to be (package main is unimportable), and the drift guard is
  `TestPositionalVerbGuardVocabularyMatchesDispatch`, which fails if the two
  vocabularies disagree on the real binary. It is a guarded duplication, not a
  free one.


## Verification

- Build: `CGO_ENABLED=0 go build -o bin/gopi ./cmd/agent/` → `BUILD_OK` (twice:
  once at the two-row fix, once after the address field was moved into the
  refusal's message).
- Tests: `make test-budget PKGS='./cmd/agent/ ./internal/config/ ./internal/standards/'`
  → `3 finished, 0 with a failure`, `elapsed=545s exit=0 outcome=pass`
  (stream `run-1791194813-2045254.json`). Targeted: `go test -count=1 -run
  'TestWebToolFlatTwinsAreRefusedByTheirOwnArm|TestWebUsagePagesDoNotPromiseARetiredFlatVerb|TestRefusedSpellingWordsCannotReachTheRunPath|TestSpellingPair|TestVerbGuard|TestUndispatchedToolWord|TestPositionalVerbGuard'
  ./cmd/agent/` → `ok github.com/nick/gopi/cmd/agent 1.932s`.
- Ran (verbatim, on the fixed binary, in an isolated `GOPI_HOME` with
  `GOPI_*`/`ANTHROPIC_*`/`DEEPSEEK_*`/`DISPLAY` masked):

```
$ GOPI_HOME=$SB ./bin/gopi web-search
gopi: "web-search" is not a gopi command — this one is spelled `gopi tool web search` on the command line (`gopi tool web search -h` for its help). To send the word as a prompt instead, say so explicitly: `--prompt web-search` (or the GOPI_PROMPT env var).
$ GOPI_HOME=$SB ./bin/gopi --timeout 3 web-search
gopi: "web-search" is a gopi command, not prompt text — it is a POSITIONAL here, following the run flag --timeout … Put the command first: `gopi web-search` …
$ GOPI_HOME=$SB ./bin/gopi web-fetch
gopi: "web-fetch" is not a gopi command — this one is spelled `gopi tool web fetch` on the command line …
$ GOPI_HOME=$SB ./bin/gopi web_search
gopi web_search: retired — use `gopi tool web search` (the same tool, one spelling)
```

- Result: exit **1** for every refused shape; **sessions = 0** in all eight
  probe rows (`artifacts/webtool-spellings-after.txt`), where the pre-fix binary
  reached `Error creating agent: …` for both dash twins
  (`artifacts/webtool-spellings-before.txt`); `gopi web-search -h` now exits 1
  with the refusal instead of exit 0 with the top-level page; the three web pages
  print the corrected closing paragraph (asserted by driving each surface).
- Commit: the branch's single commit — `git log -1 --format=%H` on
  `task/01a10b4b1815746bac7a49c47a00d75b` (a document cannot cite its own
  commit's hash without invalidating it, so it names the query instead).

## Evidence (self-filed)

| claim | command | observed |
|---|---|---|
| pre-fix: dash twins reach the run path | `bash artifacts/probe-webtool-spellings.sh ./bin/gopi before both` | `web-search run exit=1 sessions=0 | Error creating agent: failed to create DeepSeek Claude client: deepseek: ANTHROPIC_AUTH_TOKEN not set` (same for `web-fetch`) |
| pre-fix: the page promised the retired word | `./bin/gopi web_search -h` | `The legacy flat verb gopi web_search still works (deprecation note on stderr) — gopi web search is the canonical form.` |
| post-fix: refused, no run | `bash artifacts/probe-webtool-spellings.sh ./bin/gopi after both` | `web-search run exit=1 sessions=0 | gopi: "web-search" is not a gopi command — this one is spelled \`gopi tool web search\` …`; `-h` also exit=1; sessions=0 in all rows |
| the arm is load-bearing | `artifacts/redproof-row-removed.txt` | `--- FAIL: …/web-search: refusal does not mention "gopi tool web search"` **and** `gopi web-search -h: exit 0` |
| gofmt clean on touched files | `gofmt -l <the 7 touched .go files>` | no output (the 10 pre-existing unformatted files in the package are untouched by this diff and unformatted at base too) |
| the full package set is green | `make test-budget PKGS='./cmd/agent/ ./internal/config/ ./internal/standards/'` | `packages: 3 finished, 0 with a failure`, `elapsed=545s exit=0 outcome=pass`, stream `run-1791194813-2045254.json` |
| a RED was observed — and it is not this diff | the SAME budget command started a second time while the first was still running (two streams raced: `run-1791194761-2040374.json` vs `run-1791194813-2045254.json`) | the racing stream reported `TestTermOnceHonestExitAndNoResidue` red — `cli_term_once_test.go:457: the run left a listening socket behind: /proc/net/tcp 0100007F:A019`. Isolating it: `go test -count=1 -run TestTermOnceHonestExitAndNoResidue ./cmd/agent/` → `ok 0.220s`; the serial budget run is green. That test reads the KERNEL's shared `/proc/net/tcp` table, so a second concurrently running test process is itself a listener it can see. Recorded because a red was observed and must not be hidden; it is an artefact of running the gate twice at once, not a property of this branch. |

## Files touched

| file | why |
|---|---|
| `cmd/agent/cli_undispatched_tool_words.go` | the two rows; the `homeProg` address field; the refusal message re-cut so the ADDRESS carries the program word (no double `gopi`); `refusalAddress()` |
| `cmd/agent/cli_verb_aliases.go` | the alias arm now formats the same message through the same address helper |
| `internal/config/cli_verb_guard.go` | the two dash twins in `PositionalVerbs` + the leaf-local `positionalVerbDashTwin` |
| `cmd/agent/cli_websearch.go`, `cli_webfetch.go`, `cli_web.go` | the three pages' closing paragraph (the promise → the behaviour + the live home) |
| `cmd/agent/cli_webtool_dash_twin_test.go` | NEW — the arms and the pages, driven on the real binary |
| `cmd/agent/cli_undispatched_tool_words_test.go`, `cli_verb_guard_test.go`, `cli_spelling_pair_test.go` | the two vocabularies' gates now assert the ADDRESS (run for real) instead of comparing the canonical word |
| `devdoc/gopi-verb-census.md` | the `web_search`/`web_fetch` paragraph described a delegation that no longer happens |
| `COMMANDS.md` | §3.5 prose claimed the flat spellings "still route for existing scripts" and cited line numbers my edits shifted; the GENERATED verb index re-derived (`go run internal/standards/testdata/commands-census.go . COMMANDS.md`) |
| `internal/standards/commands_doc_gate_test.go` | two reviewed non-verb exemptions for the spelled-out `web-search`/`web-fetch` claims the corrected COMMANDS.md prose makes |
| `cmd/agent/cli_tasks_sync_peers_bare_test.go` | the bare-`--peers` change-detection ratchet lists this branch's `cmd/agent` files (with the reason) |
| `devdoc/DOC-INDEX.tsv` | this document's row (written by the docclass tool, never by hand) |

## For the next reader

The reusable lesson, stored in memory as `#7995`: **a refusal implemented as a
bespoke arm is invisible to the derived vocabularies.** When a spelling is
refused by a handler rather than by a table, its separator-twin (and its
case-twin) is in no list at all, and the run path takes it as prompt text. The
fix is a row in the ONE spelling table — not a second arm — and the row must
carry the ADDRESS it prints, because a spelling whose canonical word is itself
retired would otherwise send the reader into another refusal.
