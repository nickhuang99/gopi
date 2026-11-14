<!-- doc-class: normative -->
<!-- census-evidence-ok: this public export was published with NO git history (a clean-tree export, not a clone of the development repository), so a commit pin can never resolve here by construction — the figures are a snapshot of the private tree, kept as a record and not as a re-derivable claim about this repository -->
# G7 census — every top-level `gopi` verb: dispatched, advertised, and what actually exercises it

Truth class: **normative** — a standing claim about master, re-derivable at any
commit. The class is declared on line 1 of this file (`<!-- doc-class: normative -->`),
which is where a document's genre lives (Rule 36); the old
`devdoc/DOC-INDEX.tsv` registry is no longer tracked, so nothing should be read
from it — this line used to name it and that name was a dead citation.

This is the G7 deliverable of the help-surface hardening epic. It answers one
question with a number instead of an impression: **for every top-level verb the
`gopi` binary dispatches, has anything ever handed it a real argv?**

**HISTORICAL BASELINE — the surface at `cc5ac1fd`, NOT this document's base and
NOT the current table.** The figure below records the tree as it stood before the
verb-retirement epic and before the `merge` removal (both are described further
down); it is kept because it is the measurement the epic opened with, and it must
never be read as a reading of master today — the current table's own tally is the
"## The answer, in one line per class" section, and this document's base is named
there. Measured on master at `cc5ac1fd8a82c9cdd78ec1e3ef9e9bf4aa1b8906` (re-measured here — the
original pin `216d3d41`, 2026-09-25, is NOT reachable in this snapshot; see the truncation
note below): **35 verbs dispatched, 30 of them
driven by a test with a real (non-help) argv, 5 that no test drives.** Those five
are the finding, and each is named with the evidence that keeps it out of class
`a:test` — none is silently assumed covered, and none is claimed to be a defect
without saying why it looks the way it does.

**Update (35 → 32 `a:test`).** The first of the five, `capture`, is closed: it is
now driven end to end with a real argv, for both of its top-level spellings, with
the X11 leg replaced at the seam `shot.CaptureX11Provider` — so the drive needs no
display and no Xvfb. Its row below is now `a:test`, and the reasoning that kept it
out of that class is kept as the record of what the gap was. The remaining four
(`proxy`, `sandbox`, `setup`, `ui`) are unchanged and still named. The newest row is
`keyword_search` below: the word became a dispatched verb at task
`01a0dde8-8436-74b6-83c1-d78f909df14b` (it used to be spent as a prompt) and its
test drives it with a real argv on the built binary.

**Update — the `doccheck` row (task `01a0def2-45cb-71ed-9d47-4f9eca55a350`; task
`01a0df40-55c3-7659-801b-bc82f5e3b92b`).**
`doccheck` was dispatched by `cmd/agent/main.go`'s chain (predicate
`isDocCheckSubcommand`, constant `CliVerbDocCheck`, commit `c804d68e`) but had NO
row here, so this census was stale and its gate red. The row below is now present
and reads `c:neither` — the census's ONE product class, and the honest reading:
**nothing has ever handed `doccheck` a real argv at this commit.** Its own contract
suite (`cmd/agent/cli_doccheck_test.go`) drives the verb through
`runDocCheckInProcess`, which calls `parseDocCheckArgs` and the report/fix
functions **in process** — close to a run, but neither an argv literal nor a bare
argument to a variadic runner, so by this census's own definition it earns no
`test_drive` column.

**Update — the `gif` row (task `01a0eec1-c70e-74d2-b16b-0c1bcd3686c4`).** A new
top-level verb, `gopi gif` (constant `CliVerbGif`, predicate `isGifSubcommand`,
commit `4c79f470`), turns a PNG frame sequence into one animated GIF — the TIME
dimension of the picture surface, beside `capture`/`shot`/`text2img`/`annotate`,
all of which produce stills. Its row reads `a:test`: `cmd/agent/cli_contracts_test.go`
handles it in the contract table with a real argv (`{argv: CliVerbGif, args:
[]string{CliVerbGif, "-h"}, fingerprint: cliUsageTitle(gifUsage)}`) and
`cmd/agent/cli_completion_test.go` carries it in the completion vocabulary, so the
verb is exercised, advertised and pinned against a rename. The end-to-end run lives
one level up in `internal/gifgen/gifgen_test.go` (15 tests, 5 of them red→green
regressions for real defects), and the verb itself was driven on real frames for the
example artifact — both in `docs/archive/demo/showcase/gif/`.

**Update 2026-10-31 — `merge` is GONE from this census (epic merge-removal,
item 2).** The top-level `gopi merge` verb was removed from the command surface:
its help row, its dispatch clause, its `CliVerbMerge` / `cliverbs.Merge`
constants and its dispatch file are all deleted in that slice. So this table
loses exactly one row and gains none, and the row count is **17 → 16** — with
the class tally re-derived by the generator itself:

```
bash scripts/g7-verb-census.sh ./bin/gopi
g7-verb-census.sh: 16 verbs, 14 a:test, 0 b:runner-line, 0 b:help-only, 0 c:neither
```

(The generator prints `b:usage-log` / `b:doc-recipe` rows too and does not count
them in that summary line; the two here are `merger` and `ui`, exactly as
before — removing `merge` did not move any other row's class.)

**This is a SURFACE removal, not a capability removal, and the census must not be
read as the second.** The merge engine still runs: the git atoms under
`gopi task merge` dispatch it (same engine, same single-instance lock), which is
why `task`'s row is untouched. What this table measures is the TOP-LEVEL
dispatch chain in `cmd/agent/main.go`, and that chain no longer reaches a `merge`
predicate — so a row for it would be the stale claim the gate's second direction
exists to catch.

**Update 2026-10-03 — `replay` is GONE from this census (task
`01a0ffbb-a1bc-7c31-9d2f-631e475ee449`, item 1/5 of the replay-into-gopi epic).**
The verb was abolished, not renamed: `gopi replay` and its self-drawn renderer were
deleted, and replay is reached as `gopi chat --replay <session>` — a FLAG on the
chat verb (memory #7653: the comparison with `log`/`raw`/`stats` was about STATUS,
never about SHAPE). So this table loses exactly one row and gains none, and the same
argument applies to `internal/config.PositionalVerbs`, which lost the word in the
same change. The row count is **44 → 43**, the class tally above is the re-derived
one at commit `971400a7` (`bash scripts/g7-verb-census.sh ./bin/gopi`), and
`devdoc/replay-abolished-verb-single-route.md` is the record.

**Update 2026-10-02 — `ledger-window` and `text2img-verify` (task
`01a0fa4d-0195-7db7-a027-e0dfabe3a72e`).** The three programs that were separate
binaries under `cmd/` are subcommands of the ONE binary now (PROJECT.md Rule 35;
`devdoc/one-binary-census.md` is the census that decided it; the third, `replay`,
was abolished on 2026-10-03 — see the update above). They appear here for
the first time — as `b:doc-recipe`, which is the honest class: **no test drives them
yet**, and the devdoc rows the table counts include this document and the report,
because a recipe line in either IS a recipe. The gap is a known, named one: each verb
has a `<cmd> -h` entry in the contract suite's inventory, and a real argv drive is the
`a:test` upgrade this branch did not attempt — a `gopi ledger-window` invocation against
a test ledger and a `gopi text2img-verify` render are both cheap, and belong to the
next task rather than to a merge whose whole point was to change no behaviour.

**Update — the `advertised` column is now 1 (task
`01a0df40-55c3-7659-801b-bc82f5e3b92b`).** The first update above left the word
undiscoverable: the top-level command index in `internal/config/help.go` carried no
`gopi doccheck` line, which is why this gate's OWN direction
(`TestTopLevelUsageAdvertisesEveryDispatchedVerb`) was red alongside the census —
a dispatched verb that `gopi -h` never names is exactly the P0 that test exists to
catch (memory #1252). The index now carries the row (leaving the 160-line top-level
budget untouched by moving detail, never by widening the cap), so `advertised`
reads 1 and the row's class is unchanged only because a HELP line is not a run —
the operator-level `Ran:` line in that task's report is still what would move
`doccheck` out of `c:neither`, and `--help` is never counted (PROJECT.md Rule 31).

**Update — the missing `notify` row, and the class table brought to the generator's
own numbers (task `01a0f450-da2b-7cf5-b712-12566380ed56`, 2026-10-01, master
`31edde69`).** `gopi notify` (commit `1d47296a`, merged into master at `f0560275`)
was dispatched with **no row at all**, so `TestVerbCensusMatchesTheDispatch` was red
on master for everyone — the exact failure this census exists to catch, reported by
the gate rather than by a reader. The row is added above and the class table now
reads the generator's own figures, measured by
`bash scripts/g7-verb-census.sh ./bin/gopi` at master `31edde69`: **40 verbs, 34
`a:test`, 2 `b:usage-log`, 4 `b:doc-recipe`, 0 `b:runner-line`, 0 `b:help-only`, 0
`c:neither`**. Two classes moved for reasons that are not this row: `sandbox`
(`b:doc-recipe` → `b:usage-log`: 2 recorded runs exist in the corpus now) and
`doccheck` (`c:neither` → `b:doc-recipe`: 11 devdoc recipes run it, its `Ran:` line
landed). The table's other COUNT columns still read their own base and drift with
every landed branch — the gate re-derives the CLASS, and the class is what the
table claims.

**Update — the `notify` row's class, now EARNED (task
`01a0f4b1-f6f1-7dcd-b39e-27df6ad9ea69`, 2026-10-01, base `942a44ac`).** The
paragraph above landed the row and read it `b:doc-recipe`, and it said why the
count column was the thing that would move: the verb's own suite had always driven
the REAL binary (11 tests), but through `runNotifyBinary(t, …)`, whose argv was
spelled `append([]string{"notify"}, …)` — a string literal, which the census's
`censusArgvLiteralDrives` does not recognise as an argv, so only the 11 devdoc
recipes in `devdoc/notify-subscription.md` were credited. That count was movable,
so it was moved rather than explained: `runNotifyBinary` now builds the argv
literal the census re-derives — the verb FIRST, `{CliVerbNotify, …}` — and hands it
to the new `runNotifyArgv`, which passes it to `exec.Command`. The row reads
`a:test`, which it now earns. Re-derived with the generator at `942a44ac`: **40
rows, 35 `a:test`, 1 `b:usage-log`, 4 `b:doc-recipe`, 0 `b:runner-line`, 0
`b:help-only`, 0 `c:neither`** — the class table above reads these; the five verbs
with `test_drive 0` are `doccheck`, `proxy`, `sandbox`, `setup` and `ui`, and an
A/B of the class column against a pre-change run showed **only `notify` moved**.

**Update — the census's constant-site READER (task `01a0dfae9ca0`, 2026-09-26).**
The table was not red because a row was wrong: one arm could not be READ at all.
This census projects each dispatched predicate's constant onto its verb word, and
that projection's window was cmd/agent's own files plus `internal/toolnames` +
`internal/tools` — while `CliVerbMerge`'s word is spelled **once**, in the leaf
constant package `internal/cliverbs` (`Merge = "merge"`, Rule 14), because the
top-level help in `internal/config` must name the same verb and cannot import
`package main`. The alias landed at `ee8aff27` (`fix(runpath): a plain prompt
reaches the model verbatim (RP-1)`), and the reader's window was not widened with
it. So on a tree where 37 verbs are dispatched, both the gate and the generator
refused the WHOLE census with `constant CliVerbMerge (from isMergeSubcommand)
resolves to no verb word` — a Rule-14-correct tree reported as a defect. The fix
is in the reader, and it is generic (Rules 1/13/14: never a second literal —
resolve the alias): the qualifier (`cliverbs`) is resolved through the importing
file's own import block and the directory through `go.mod`'s module line, so a new
leaf constant package needs no list edited here or in the generator. **No row
changed and no class changed**: the counts in the paragraphs above (35 dispatched /
30 test-driven at `216d3d41`) are numbers AT THAT BASE; what the fix restored is
the ability to read the 37th verb at all. `doccheck` is still the one `c:neither`.

**Update — the top-level INDEX tightening (task `01a10bb7-d11b-7007-874c-be0dd34d31ba`,
2026-10-05, base `819a7324`).** The `gopi -h` command index (`internal/config/help.go`,
`commandIndex`) was tightened to **one line per verb, one sentence per line**: the
long enumerations and asides moved out of the index and into each verb's own
`-h` (where detail belongs), which took the page from **175 lines to 167**
(`topLevelUsageMaxLines` = 176) and the Commands block from **76 lines to 68**.
**The census re-derivation is why this paragraph exists**: the table below was
**regenerated**, not edited — `bash scripts/g7-verb-census.sh ./bin/gopi` at base
`819a7324` with the tightened binary reads **44 verbs, 37 `a:test`, 3
`b:usage-log`, 4 `b:doc-recipe`, 0 `b:runner-line`, 0 `b:help-only`, 0
`c:neither`** — the SAME 44 rows and the SAME classes as before the tightening,
which is the property the change had to preserve: the census matches a verb at the
START of a help line (`(?m)^ {2}gopi ([a-z][a-z0-9_-]*)(?: |$)`, re-derived in Go
by `censusAdvertised`), so a row that merely CONTAINS a word does not advertise it.
The only cells that moved are the drift columns (`test_drive`, `doc_recipe`,
`usage_log`) that move with every landed branch and with the corpus `GOPI_HOME`
names; the two `advertised 0` rows (`web_fetch`, `web_search`) and every class are
unchanged. `advertised` is re-derived from the BUILT binary's own text, so a stale
`bin/gopi` would report the pre-change page: build before generating
(`CGO_ENABLED=0 go build -o bin/gopi ./cmd/agent/`).

## The answer, in one line per class

| class | count | meaning |
|---|---|---|
| `a:test` | 16 | a test hands the verb a real (non-help) argv — an argv literal, a call to the verb's own run function, or a bare argument to a variadic runner |
| `b:usage-log` | 1 | no test drives it, but a **recorded run** of it exists in the session corpus |
| `b:doc-recipe` | 1 | no test and no logged run, but a devdoc recipe runs it (`ui`) |
| `b:runner-line` | 0 | (weaker evidence: the verb appears on a line that calls a runner, with no argv literal) |
| `b:help-only` | 0 | (only `<verb> -h` has ever reached it) |
| `c:neither` | 0 | nothing at all — a verb shipped, dispatched, and never run (empty: every verb the table lists is reached by one of the five surer classes) |

These six counts are the exact tally of the table below: **18 rows — 16 `a:test` +
1 `b:usage-log` (`merger`) + 1 `b:doc-recipe` (`ui`)**. The generator's own
summary line for this table reads:

```
g7-verb-census.sh: 18 verbs, 16 a:test, 0 b:runner-line, 0 b:help-only, 0 c:neither
```

**The 18th row is `screen-replay`, and it is this branch's addition** — the
TOP-LEVEL address of the recorded screen's byte reader, whose SUB-VERB `screen`
already occupies its own row above. The two rows are two addresses of ONE
implementation (`cmd/agent/cli_screen_replay_verb.go` delegates to the same
resolver, writer, ending sentences and frame comparison as
`cmd/agent/cli_screen_replay.go`), and they are separate rows because the census
counts VERBS, not implementations: an operator who types `gopi screen-replay` and
one who types `gopi screen replay` are running two command lines, and only a row
per command line can say whether either has ever been handed a real argv. Both
read `a:test`, and the `screen` row keeps its own `test_drive` count — the new
file's tests drive only the new word, deliberately: the census counts argv, so a
test that merely COMPARES the two words against each other drives neither.

The row's cells come from `bash scripts/g7-verb-census.sh ./bin/gopi` run against
a binary built from **this working tree on top of the published revision
`8370e3bf411fce07b927404f676ee6782901258d`** (`git merge-base --is-ancestor
8370e3bf411f origin/master` exits 0, so that revision is not one a rebase can
rewrite — Rule 38). The other 17 rows moved with it because the generator counts
the tree, not a commit: every `test_drive` / `usage_log` / `doc_recipe` cell is a
reading of the corpus as it stands, which is why this table is REGENERATED rather
than edited.

(The summary line counts `a:test` / `b:runner-line` / `b:help-only` / `c:neither`
and does NOT count the `b:usage-log` / `b:doc-recipe` rows — read the TSV for
those two. That is why the row arithmetic here is `16 + 1 + 1 = 18`, while the
line above names only the 16.)

The `b:*` cells are the CORPUS-dependent ones — which verb sits in `b:usage-log`
versus `b:doc-recipe` moves with the profile `GOPI_HOME` names (see the last
bullet of "What this census does NOT claim"), so the summary follows the table
rather than any one host's generator run. Only `a:test` and `c:neither` are
re-derived strictly by the gate.

**The class table and the verb table are ONE arithmetic claim, and it is now
checked.** The drift that `6ccb7a49` introduced — the `merge` ROW was deleted when
the verb was removed, and this prose plus the class table kept reading 17 / 16
`a:test` over a table that had 14 — survived review because nothing compared the
two: `TestVerbCensusMatchesTheDispatch` re-derives the dispatched set against the
built binary and is heavy, and the census-evidence gate (Rule 38) asks only
whether the document names a generator and pins a commit. So
`internal/standards/verb_census_tally_gate_test.go` now reads this ONE file in pure
text and fails when the class table stops counting the rows the verb table prints:

```bash
go test ./internal/standards/ -run TestVerbCensusTallyMatchesItsOwnTable
```

It is lightweight on purpose (no binary, no corpus, no dispatch walk), so it runs
on every change; the class-vs-corpus direction stays the heavy gate's job.

**Update — the retirement's own table (verb-retirement epic, 2026-10-06;
regenerated with `bash scripts/g7-verb-census.sh ./bin/gopi`).** This table counts
**TOP-LEVEL dispatched verbs**, and the retirement is precisely a change to that
set: the four session verbs (`log`, `stats`, `raw`, `resume`) and the local tools
(`capture`, `shot`, `text2img`, `screen_capture`, `code-search`, …) left the
top level for `gopi session …` / `gopi tool …`, so their rows are gone — a census
row for a word the binary refuses would claim a surface that does not exist.
Their coverage did not go anywhere: it moved to the family row (`session`'s
`test_drive` 7 → 15, `tool`'s 5), which is the same drives counted under the door
that now reaches them. The exact tally is the generator's own last line, quoted
above; at THIS document's base the whole table is 14 `a:test` rows, one
`b:usage-log` (`merger`) and one `b:doc-recipe` (`ui`, whose launch needs a TTY),
and `c:neither` is empty. (This paragraph was written when the table read 16
`a:test` + 1 `b:doc-recipe`; the `merge` row's removal is what moved the first
number, and the class table above is the current reading.)

`-h` is never counted as a run anywhere in this document (PROJECT.md Rule 31): a
help query proves the dispatch chain reached the predicate, never that the verb
does its work.

**Update — the `tool` row (command-hierarchy epic, slice 1/5; measured at
`156e2e69`, generator `scripts/g7-verb-census.sh ./bin/gopi`).** `gopi tool` is a
new GROUP verb (constant `CliVerbTool`, predicate `isToolSubcommand`,
`cmd/agent/cli_tool.go`): gopi's local tools under one noun, a PURE name→
implementation map that forwards `gopi tool <word> …` to the word's OWN
top-level predicate. It is dispatched and advertised, so it is a row like every
other verb — and its row reads `a:test`, because `cmd/agent/cli_tool_test.go`
hands it a real argv (a bare word, an unknown word, and every forwarded word in
both spellings) on the built binary. The table below was regenerated at that
commit, which is why the corpus-dependent `usage_log`/`doc_recipe` columns moved
with it: **44 verbs, 37 `a:test`, 3 `b:usage-log`, 4 `b:doc-recipe`** (the
`tool` row is the 44th, an `a:test`). Only the classes the gate re-derives
strictly (`a:test`, `c:neither`) are claims about this branch's code; the corpus
counts are a reading of the host's own profile.

**Update — the double-spelling slice (verb-restructure epic, 3/5; regenerated at
this branch's base with generator `scripts/g7-verb-census.sh ./bin/gopi`).** The
table was regenerated rather than edited: the 18 cells that moved are the
corpus-dependent ones (`usage_log`, `doc_recipe`, and the two `test_drive`
counts that grew with new test sites), and the class column is UNCHANGED on
every row — no verb appeared, none disappeared, and the six derived spelling
pairs the slice audits are visible here as exactly six dispatched words whose
twin is not a row: `code-search`, `screen_capture`, `ui_state`, `vision_analyze`,
`voice_input`, `keyword_search`. The audit of those pairs, and of the four
vocabularies that carry them, is `devdoc/verb-double-spelling-slice.md`.

## The verbs with no test-driven argv

**At this document's base there are exactly TWO,** and they are the table's whole
non-`a:test` population (they are what makes the tally `14 + 1 + 1 = 16`):

| verb | class | what exercises it instead | why that is enough — or not |
|---|---|---|---|
| `merger` | `b:usage-log` | one recorded run in this profile's session corpus, plus 189 devdoc recipes (`gopi merger …`) | it is the resident merge loop: driving it from a test means running a resident process against a real repo. Its engine is covered through `gopi task merge`, but no test hands the `merger` WORD a real argv. |
| `ui` | `b:doc-recipe` | 33 devdoc recipes (`gopi ui --tools …`, the `ui → http://127.0.0.1:8090` startup line) | it opens a terminal browser on a TTY; `ui_state` (its state verb) HAS a test drive, and `gopi ui`'s argv parsing is covered — the launch itself is not. |

### HISTORICAL RECORD — the gap list as it read at `cc5ac1fd` (pre-merge-removal and pre-retirement)

**Nothing in this table is a claim about the current surface**, and most of its verbs
are not rows any more: `proxy`, `sandbox` and `setup` left the TOP level for
`gopi tool …` in the verb-retirement epic (their rows are gone; their coverage moved
to the `tool` family row), `capture` and `notify` closed their gaps and now read
`a:test` in the current table, and `ui` is the one row that is still non-`a:test`
today. It is kept because it is the evidence behind the epic's five-finding claim,
and because the reasoning for each gap is the part worth re-reading.

| verb | class | what exercises it instead | why that is enough — or not |
|---|---|---|---|
| `capture` | **`a:test`** (was `b:usage-log`) | `cmd/agent/cli_capture_test.go` — a real argv through the verb's dispatch, for `--screen`, `--window` and `--area`, plus the `screen_capture` alias arm, with `shot.CaptureX11Provider` stubbed so no display is required. | **the gap, closed.** The old distinction still explains why this row mattered: two recorded runs existed (`gopi capture --screen --out …`, `gopi capture --area 1364x740+277+78 --out …`) and the pixels landed, but the repo's own suite only called the argument parsers — a regression would have been caught by nobody until someone tried it. The drives that close it are at the X11 boundary: `TestCaptureVerbRunsARealArgvEndToEnd`, `TestScreenCaptureAliasRunsARealArgvEndToEnd`, `TestScreenCaptureAliasForwardsAModeFlagToTheCaptureVerb`, `TestCaptureAndScreenCaptureShareOneEngine` (replacing the tautological version) and `TestCaptureVerbFailureArmsExitNonZero`. |
| `proxy` | `b:usage-log` | 6 recorded runs, e.g. `gopi proxy --upstream openai --openai-base-url https://open.bigmodel.cn/api/paas/v4 …` | the argv is the operator's (`--upstream`, a base URL, a model), so a faithful test would need a live upstream. A start-up/refusal test would be cheap; a routing test would not. Not filed as a bug — named as a gap. |
| `sandbox` | `b:usage-log` | 2 recorded runs and 51 devdoc recipes (`gopi sandbox show`, `gopi sandbox create --label build --task-id …`; the sandbox now has its own recipe corpus) | its initializer has its own tests (`internal/sandbox`), and the recipes are copied and run — but no test in `cmd/agent` hands `sandbox` an argv. |
| `notify` | **`a:test`** (was `b:doc-recipe`) | `cmd/agent/cli_notify_test.go` — 11 tests driving the REAL built binary through `runNotifyBinary(t, …)`, which hands `exec.Command` an argv literal with the verb FIRST (`{CliVerbNotify, …}`). | **the understated row, corrected.** The row was added reading `b:doc-recipe`, crediting only the 11 devdoc recipes in `devdoc/notify-subscription.md`: the suite's argv was spelled `append([]string{"notify"}, …)`, a string literal the census's `censusArgvLiteralDrives` does not recognise, so real coverage was not counted. The drive is now spelled in the shape the census re-derives (the `gif`/`annotate` precedent) and the row reads `a:test`. |
| `setup` | `b:doc-recipe` | 3 devdoc recipes (`gopi setup` — "4 paths provisioned, exit 0") | it is a provisioning verb whose effect is on the operator's profile, which is exactly what a test may not touch. Registering the verb with no test is the defensible choice; saying so is this row. |
| `ui` | `b:doc-recipe` | 13 devdoc recipes (`gopi ui --tools …`, the `ui → http://127.0.0.1:8090` startup line) | it opens a terminal browser on a TTY; `ui_state` (its state verb) HAS a test drive, and `gopi ui`'s argv parsing is covered — the launch itself is not. |

## How to re-derive this table

```bash
CGO_ENABLED=0 go build -o bin/gopi ./cmd/agent/
bash scripts/g7-verb-census.sh ./bin/gopi
```

The script derives the verb list from the dispatch chain itself and the built
binary's own `-h` text — it contains no verb list, so it cannot go stale the way
a hand-written table does. Point it at another profile's corpus with `GOPI_HOME`
(`GOPI_HOME=/path bash scripts/g7-verb-census.sh ./bin/gopi`); with no sessions
directory present the `usage_log` column is all zeros, which is a fact about the
host, not a defect of the census.

## The gate

`cmd/agent/cli_verb_census_test.go` re-derives the dispatched set, the advertised
set and the exercised columns **in Go**, and compares them with the table above
**in both directions**:

```bash
go test ./cmd/agent/ -run TestVerbCensusMatchesTheDispatch
```

* a verb the dispatch chain gains and the table lacks → **red** (the doc is stale);
* a row naming a verb no longer dispatched or advertised → **red** (the doc invented it);
* a row classed `a:test` with no argv drive in the corpus → **red** (the class was not earned);
* a row classed anything else while the corpus DOES hand it an argv → **red** (understating coverage);
* fewer than `censusMinRows` rows → **red** (non-vacuity floor: a table that lost
  most of its rows is a broken table, not a small surface; the constant is
  `cmd/agent/cli_verb_census_test.go`'s and reads 8, measured against the
  retirement's 17-row surface — Rule 1, and the same reasoning
  `censusMinChainPredicates` carries).

Where the built binary is absent, the advertised direction is skipped **with a
log line** rather than passed silently — an unverified direction says so.

**And a SECOND, lightweight gate reads this file's own arithmetic**
(`internal/standards/verb_census_tally_gate_test.go`): the class table above must
count the rows the verb table prints, in both directions plus the sum, and it must
keep naming the empty classes:

```bash
go test ./internal/standards/ -run TestVerbCensusTallyMatchesItsOwnTable
```

* a class the ROWS carry but the class table counts differently (or omits) → **red**
  (the tally is stale against the table it summarises);
* a class the class table counts above zero with no row carrying it → **red**
  (the table lost a row and the tally invented the population back);
* the class counts summing to something other than the row count → **red**;
* fewer than `verbCensusMinRows` rows, or fewer than `verbCensusMinClasses` named
  classes → **red** (a sweep over a fragment must not pass as green).

It needs no binary, no corpus and no dispatch walk — that is why it can run on
every change, where the gate above (`heavytest.CriterionDeviceExec`) cannot. The
two failure shapes are not the same check: the heavy gate asks whether the table
matches the CODE, this one asks whether the document matches ITSELF. The drift that
`6ccb7a49` introduced (the `merge` row deleted, the tally left at 17 / 16
`a:test`) was invisible to the heavy gate for exactly that reason and stayed live
until a reader noticed — this gate is the arm that would have caught it on the
commit that introduced it.

## The table

| verb | dispatched | advertised | test_drive | runner_line | usage_log | doc_recipe | help_only | class |
|---|---|---|---|---|---|---|---|---|
| `chat` | 1 | 1 | 109 | 0 | 61 | 286 | 1 | a:test |
| `cli` | 1 | 0 | 3 | 0 | 0 | 36 | 0 | a:test |
| `config` | 1 | 1 | 25 | 43 | 24 | 61 | 0 | a:test |
| `memory` | 1 | 1 | 27 | 0 | 41 | 212 | 1 | a:test |
| `merger` | 1 | 0 | 0 | 0 | 1 | 189 | 0 | b:usage-log |
| `notify` | 1 | 1 | 3 | 0 | 132 | 42 | 0 | a:test |
| `screen` | 1 | 1 | 1 | 0 | 0 | 0 | 0 | a:test |
| `screen-replay` | 1 | 1 | 2 | 0 | 0 | 0 | 0 | a:test |
| `session` | 1 | 1 | 41 | 6 | 208 | 306 | 0 | a:test |
| `task` | 1 | 1 | 106 | 10 | 2201 | 1796 | 1 | a:test |
| `tool` | 1 | 1 | 14 | 0 | 132 | 681 | 0 | a:test |
| `ui` | 1 | 0 | 0 | 0 | 0 | 33 | 0 | b:doc-recipe |
| `ui_state` | 1 | 1 | 5 | 0 | 35 | 84 | 0 | a:test |
| `version` | 1 | 1 | 6 | 1 | 14 | 17 | 1 | a:test |
| `web` | 1 | 1 | 23 | 0 | 18 | 116 | 1 | a:test |
| `web_fetch` | 1 | 0 | 2 | 0 | 0 | 6 | 1 | a:test |
| `web_search` | 1 | 0 | 9 | 0 | 0 | 15 | 1 | a:test |
| `worker` | 1 | 1 | 97 | 1 | 161 | 353 | 0 | a:test |

Column meanings — `dispatched`: the chain in `cmd/agent/main.go` reaches the
verb's predicate; `advertised`: the built binary's `gopi -h` lists it;
`test_drive`: test sites handing it a real argv (argv literal, its own run
function, or a runner line); `runner_line`: the weaker runner-line subset;
`usage_log`: `gopi <verb> …` in a command a recorded session actually ran
(help queries excluded); `doc_recipe`: the verb opening a command line inside a
fenced devdoc block; `help_only`: `<verb> -h` is in the contract suite's
inventory (never a run, never sufficient for `a:test`).

Three verbs read `advertised 0` with `dispatched 1` — `web_search`, `web_fetch`
and (since task `01a113a8…`, 2026-10-06) `merger`.
That is not a defect and not an omission: both are **retired spellings** whose
predicates REFUSE by name and point at the live home (`gopi web_search: retired —
use ` + "`gopi tool web search`" + ``, `cmd/agent/cli_websearch.go`,
`cmd/agent/cli_webfetch.go`). They are dispatched on purpose (so the word reaches
its own handler instead of the agent loop), kept out of the command index on
purpose, and both are still driven by tests.

**Third row reading `advertised 0` (task `01a113a8…`, 2026-10-06): `merger`.**
Same shape, one degree older: the word is RETIRED (2026-09-19,
`devdoc/merger-retired.md`) and its predicate refuses at EVERY spelling —
`gopi merger`, its deprecated aliases, and `-h` included — so it now joins the
other two in the `advertised 0` family instead of carrying its own index row. The
row that used to read `advertised 1` was the ONE top-level row that could not
honour the index's own preamble ("each accepts -h for its full help"), which is
the defect the operator reported: 「merger 既然都不存在来为什么还要提一遍？这不是
混淆吗？」. The removal is `internal/config/help.go`'s index row plus its entry in
`cmd/agent/cli_completion.go`'s `completionSuppressedVerbs` — the ONE list that
withholds a dispatching verb from both published surfaces, so help and `gopi <TAB>`
cannot drift apart — and both halves are pinned by
`cmd/agent/cli_help_hidden_verbs_test.go`, against the BUILT binary. The word
still routes (that is the whole point of a retirement), and the explanation now
lives where a reader can run it: `gopi task -h`.

*(Correction 2026-10-31, epic merge-removal item 2: this sentence used to name TWO
runnable pages, `gopi task -h` and `gopi merge -h`. The second is not runnable any
more — the standalone verb was removed, so it exits 1 with the retirement
sentence. The engine's document is the FILE `man/gopi-merge.md`, which is still in
the tree and is what the surviving refusal points a reader at.)*

**Correction (task `01a10b4b…`, 2026-10-05).** This paragraph used to describe
those two rows as deprecated aliases that "print a note and delegate to the live
verb". They do not delegate any more, and the difference is the whole point of
the refusal: a retired word that forwards is still a second spelling of one
command. The behaviour that replaced it — and the arms added for the dash twins
`web-search` / `web-fetch`, which were in NO vocabulary and reached the run path
— is recorded in `devdoc/web-tool-dash-twin-refusal.md`.

## The concept-dimension classification — what each top-level verb IS

The table above answers "is this verb exercised?". This section answers the
question that decides the SURFACE: **should this word be top-level at all?**

**The criterion, stated once, and it is the only one used below.** A top-level
verb must name an **independent concept dimension** — a thing the reader reasons
ABOUT, which introduces its own SUBJECT (an operand the verb is *about*). An
**operation ON a concept** — a verb whose subject is another verb's noun, adding
no subject of its own — belongs at the second level, under the verb that owns
that subject. So for each word ask one question: **what is this verb's subject?**

* a NEW noun it introduces (a task row, a run, the knowledge base, the live
  desktop, a picture, the build) → `concept`, stays top-level;
* ANOTHER verb's noun, adding nothing (a session's log, a session's numbers, a
  session's snapshot, a picture produced elsewhere, a document of another verb's
  corpus) → `demote-to:<parent>`, belongs under `<parent>`.

**The population is machine-derived, the class is argued — and that split is the
point.** The verb set is re-derived from the dispatch chain at the commit named
below (`scripts/g7-verb-census.sh`, and in Go by
`cmd/agent/cli_verb_census_test.go`); the CLASS is a written judgment, and every
row carries its reason so the judgment can be argued with. That is what makes the
next round of demotions a decision instead of a debate: the rows a reader
disagrees with are the rows whose reason they can name.

**Generator + base (Rule 38):** the verb population and every count above are
produced by `bash scripts/g7-verb-census.sh ./bin/gopi`; the classification's
population was taken at master **`156e2e69`** (2026-10-05), and the gate
re-derives it in both directions, so adding a top-level verb without a row here
fails the build.

**Update — the demotion round (epic verb-restructure, slice 2/5; measured at
master `e02538fa`, generator `scripts/g7-verb-census.sh ./bin/gopi`).** Six rows
moved from `next` to `landed` (`archive`, `brew`, `memsync`, `checkpoint`, and the
re-classification of the two tool mirrors from `demote-to:web` to `demote-to:tool`,
whose flat spelling is now a refusal rather than a deprecation note), `merger`
stopped claiming status `next` (it was retired 2026-09-19 and is now read as it
is), and the three parent rows the round resolved carry
`RESOLVED this round` in place of a pending question. The verb POPULATION is
unchanged (**44 dispatched**), because every one of these demotions moved a door
and kept the command: the generator's own summary still reads 44 verbs, 37
`a:test`, 3 `b:usage-log`, 4 `b:doc-recipe`. What moved is the class column of
the classification table, and only in the direction the round's work went.

**`status` says what the next round must do:** `landed` = the demotion is in tree
and the old spelling still works; `next` = the demotion is agreed but NOT done
(this round changes names only — Rule 5's `## Not Done` names these); `n/a` = a
concept, nothing to do.

| verb | class | status | reason |
|---|---|---|---|
| `act` | demote-to:tool | landed | Its subject is the LIVE DESKTOP — the same operand `capture` reads. "What is on this screen?" and "change what is on this screen" are one question about one noun; two top-level words make the reader discover the pair by accident. **Retired from the top level (verb-retirement epic, 2026-10-06):** the word's ONLY door is `gopi tool act`, the flat spelling is refused by name with that address, so the demotion LANDED. |
| `annotate` | demote-to:tool | landed | Its subject is an EXISTING PNG — a picture another verb produced. It adds no subject of its own, so it is an operation on the picture dimension, not a dimension. **Retired from the top level (verb-retirement epic, 2026-10-06):** the word's ONLY door is `gopi tool annotate`, the flat spelling is refused by name with that address, so the demotion LANDED. |
| `archive` | demote-to:task | landed | Its subject is a BRANCH, retired from the active set — the same act the ledger's `task archive` performs on a ROW. Demoted this round (`gopi task archive <branch>`, alias keeps `gopi archive`). **Retired from the top level (verb-retirement epic, 2026-10-06):** the word's ONLY door is `gopi task archive`, the flat spelling is refused by name with that address, so the demotion LANDED. |
| `brew` | demote-to:memory | landed | Its subject is DISTILLED LESSONS — the knowledge base's own content. It writes the memory dimension, it does not introduce one. Demoted this round (`gopi memory brew`, alias keeps `gopi brew`). **Retired from the top level (verb-retirement epic, 2026-10-06):** the word's ONLY door is `gopi memory brew`, the flat spelling is refused by name with that address, so the demotion LANDED. |
| `capture` | demote-to:tool | landed | The live-desktop observation dimension: it introduces the screen as an operand and owns the three modes of reading it (screen / window / area). **Retired from the top level (verb-retirement epic, 2026-10-06):** the word's ONLY door is `gopi tool capture`, the flat spelling is refused by name with that address, so the demotion LANDED. |
| `chat` | concept | n/a | The conversation-carrier dimension: it introduces a conversation as an operand and is the door to starting one. |
| `checkpoint` | demote-to:session | landed | Its subject is a SNAPSHOT OF ONE SESSION — the session's own operand read at an earlier point, adding no subject of its own. Demoted this round (`gopi session checkpoint list` / `export`, alias keeps `gopi checkpoint`; its `resume` sub-verb is REMOVED, refused by name toward `gopi session resume`). **Retired from the top level (verb-retirement epic, 2026-10-06):** the word's ONLY door is `gopi session checkpoint`, the flat spelling is refused by name with that address, so the demotion LANDED. |
| `cli` | demote-to:web | next | Documented in its own definition as "a second view over the `gopi web` engine" (`CliVerbCLI`): same subject as `web`, a different display technology. |
| `code-search` | demote-to:tool | landed | The code-symbol dimension: its own question ("where is X DEFINED?") and its own index; it introduces the symbol as an operand. **Retired from the top level (verb-retirement epic, 2026-10-06):** the word's ONLY door is `gopi tool code-search`, the flat spelling is refused by name with that address, so the demotion LANDED. |
| `completion` | demote-to:config | landed | The shell-integration dimension: its subject is the shell's own completion surface, which no other verb owns. NOT a demotion — but reachable through the configuration group too (`gopi config completion`, added this round), since the shell's readiness is what `config` owns. **Retired from the top level (verb-retirement epic, 2026-10-06):** the word's ONLY door is `gopi config completion`, the flat spelling is refused by name with that address, so the demotion LANDED. |
| `config` | concept | n/a | The configuration dimension: it introduces the effective settings (and their provenance) as an operand. |
| `gif` | demote-to:tool | landed | Its subject is a PNG FRAME SEQUENCE — pictures, the same noun as `text2img`'s output; the time dimension of one picture pipeline. **Retired from the top level (verb-retirement epic, 2026-10-06):** the word's ONLY door is `gopi tool gif`, the flat spelling is refused by name with that address, so the demotion LANDED. |
| `keyword_search` | demote-to:tool | landed | Its subject is the SAME repo text `code-search` indexes. One search dimension with two modes (definition lookup, free text), not two concepts. **Retired from the top level (verb-retirement epic, 2026-10-06):** the word's ONLY door is `gopi tool keyword_search`, the flat spelling is refused by name with that address, so the demotion LANDED. |
| `ledger-window` | demote-to:tool | landed | Its subject is a window over the LEDGER — the rows `task` owns, read as a time range. A pure utility on another verb's noun. **Retired from the top level (verb-retirement epic, 2026-10-06):** the word's ONLY door is `gopi tool ledger-window`, the flat spelling is refused by name with that address, so the demotion LANDED. |
| `log` | demote-to:session | landed | Read ONE SESSION's JSONL — the session is the subject, the log is one of its faces. Demoted this round (`gopi session log`, alias keeps `gopi log`). **Retired from the top level (verb-retirement epic, 2026-10-06):** the word's ONLY door is `gopi session log`, the flat spelling is refused by name with that address, so the demotion LANDED. |
| `memory` | concept | n/a | The knowledge dimension: it introduces the knowledge base as an operand. |
| `memsync` | demote-to:memory | landed | Its subject is the MEMORY LEDGER, moved node to node — the memory dimension's own noun, transported. Demoted this round (`gopi memory memsync`, alias keeps `gopi memsync`) — under its OWN word, not `memory sync`: that spelling is the LIVE ssh channel, a different command. **Retired from the top level (verb-retirement epic, 2026-10-06):** the word's ONLY door is `gopi memory memsync`, the flat spelling is refused by name with that address, so the demotion LANDED. |
| `merge` | demote-to:task | landed | The integration dimension: it introduces branch integration (what lands, what is handed over) as its subject — but the STANDALONE verb was DELETED (epic merge-removal, item 3/6, 2026-10-31) and the capability now has exactly one door, `gopi task merge`. The flat spelling is refused by name with that address, so the demotion LANDED and the word is no longer a dispatched top-level verb. |
| `merger` | demote-to:task | landed | RETIRED (2026-09-19), before this round: it refuses and names the merge door. A retired word is not a dimension; it is kept only so every old spelling is recognised. **Re-parented 2026-10-31:** its parent used to be `merge`, which stopped dispatching when the standalone verb was deleted (epic merge-removal item 3/6), and a demotion's parent must be a real verb — so it hangs under the family that now owns the subject, `task`. The demotion's END STATE — the old spelling answers with the pointer, never with a second implementation — was already in tree, which is why this row reads `landed` and not `next`. |
| `notify` | demote-to:task | next | Its subject is TASK EVENTS — the ledger's own stream. It reads a face of the planning dimension. |
| `proxy` | demote-to:tool | landed | The transport dimension: its subject is the upstream protocol/endpoint, a noun no other verb owns. **Retired from the top level (verb-retirement epic, 2026-10-06):** the word's ONLY door is `gopi tool proxy`, the flat spelling is refused by name with that address, so the demotion LANDED. |
| `raw` | demote-to:session | landed | The verbatim wire log OF ONE SESSION — a second face of the session, not a subject. Demoted this round (`gopi session raw`, alias keeps `gopi raw`). **Retired from the top level (verb-retirement epic, 2026-10-06):** the word's ONLY door is `gopi session raw`, the flat spelling is refused by name with that address, so the demotion LANDED. |
| `resume` | demote-to:session | landed | It continues ONE SESSION from its checkpoint: the session is the operand, this is an action on it. Demoted this round (`gopi session resume`, alias keeps `gopi resume`). **Retired from the top level (verb-retirement epic, 2026-10-06):** the word's ONLY door is `gopi session resume`, the flat spelling is refused by name with that address, so the demotion LANDED. |
| `sandbox` | demote-to:tool | landed | The execution-environment dimension: it introduces an isolated profile as an operand and owns its create/show lifecycle. **Retired from the top level (verb-retirement epic, 2026-10-06):** the word's ONLY door is `gopi tool sandbox`, the flat spelling is refused by name with that address, so the demotion LANDED. |
| `scratch-gc` | demote-to:tool | landed | The scratch-home dimension: its subject is the scratch root and the reclaim policy, which no other verb owns. **Retired from the top level (verb-retirement epic, 2026-10-06):** the word's ONLY door is `gopi tool scratch-gc`, the flat spelling is refused by name with that address, so the demotion LANDED. |
| `screen` | concept | n/a | The RECORDED-screen dimension: it introduces a session's recorded render stream as an operand in its own right — the bytes the renderer wrote, not a picture of them — and it owns the readers of those bytes (`gopi screen replay`, whose stdout is the payload unchanged, plus `--verify` for a frame-by-frame comparison). It is deliberately NOT a demotion under `session`: `gopi session replay` answers "what was on the screen" (a renderer that picks a canvas, paces frames and falls back to the dump), while this family answers "what BYTES painted it" and REFUSES a stream-less session by name rather than substituting the dump — Rule 15's two questions, which the 2026-10-03 replay abolition (`devdoc/replay-abolished-verb-single-route.md`) left with one spelling. Same reasoning as `capture` versus `shot`: the live desktop and the recording are different artifacts, and each names its own. |
| `screen-replay` | demote-to:screen | landed | The TOP-LEVEL address of the same reader `gopi screen replay` owns: one command, an operand that is a session id OR a `.screen.stream` path, and `--verify A B` for a frame-by-frame comparison of two NAMED recordings (which the sub-verb cannot express — it compares one implicit session with one other). Demoted to `screen` rather than promoted to a dimension of its own because it introduces no new operand KIND, no new artifact and no new reader: the resolver, the writer, the counts, the three ending sentences and the frame comparison are the sub-verb's (one test asserts the two doors emit byte-identical stdout). What it adds is an ADDRESS and a scriptable exit status — a divergence exits non-zero — so it is an operation on the `screen` concept, not a concept. |
| `screen_capture` | demote-to:tool | landed | An ALIAS of `capture` (whole-screen spelling), forwarding a mode flag to it — the same subject twice, exactly what Rule 13 forbids at the top level. **Retired from the top level (verb-retirement epic, 2026-10-06):** the word's ONLY door is `gopi tool screen_capture`, the flat spelling is refused by name with that address, so the demotion LANDED. |
| `session` | concept | n/a | The execution dimension: it introduces ONE RUN as an operand, and it is the parent the four demotions above land on. |
| `setup` | demote-to:config | landed | The provisioning dimension: its subject is a HOST's readiness (profile, ledger, sessions), introduced by no other verb. Reachable through the configuration group too (`gopi config setup`, added this round). **Retired from the top level (verb-retirement epic, 2026-10-06):** the word's ONLY door is `gopi config setup`, the flat spelling is refused by name with that address, so the demotion LANDED. |
| `shot` | demote-to:tool | landed | The headless-render dimension: its subject is a URL or the local chat UI rendered WITHOUT a display — a different operand from the live desktop `capture` reads. **Retired from the top level (verb-retirement epic, 2026-10-06):** the word's ONLY door is `gopi tool shot`, the flat spelling is refused by name with that address, so the demotion LANDED. |
| `stats` | demote-to:session | landed | The NUMBERS of one session — a third face of the same operand. Demoted this round (`gopi session stats`, alias keeps `gopi stats`). **Retired from the top level (verb-retirement epic, 2026-10-06):** the word's ONLY door is `gopi session stats`, the flat spelling is refused by name with that address, so the demotion LANDED. |
| `task` | concept | n/a | The planning dimension: it introduces the task row (and the ledger's order) as an operand. |
| `text2img` | demote-to:tool | landed | The text-to-raster render dimension: it introduces a rendered image and the font/size decisions behind it. **Retired from the top level (verb-retirement epic, 2026-10-06):** the word's ONLY door is `gopi tool text2img`, the flat spelling is refused by name with that address, so the demotion LANDED. |
| `text2img-verify` | demote-to:tool | landed | It VERIFIES `text2img`'s own render (the `知` fixture at N px) — a check on another verb's output, adding no subject. **Retired from the top level (verb-retirement epic, 2026-10-06):** the word's ONLY door is `gopi tool text2img-verify`, the flat spelling is refused by name with that address, so the demotion LANDED. |
| `ui` | demote-to:web | next | "A second VIEW over the same engine as `gopi web`" (`CliVerbUI`): the conversation subject belongs to the carrier, the display technology does not make a dimension. |
| `tool` | concept | n/a | The local-tools dimension: a PURE name→implementation map (`CliVerbTool`) that forwards `gopi tool <word> …` to the word's OWN predicate. It introduces the tool-set as an operand and, since the retirement, it is the ONLY door to every word under it. |
| `ui_state` | demote-to:ui | next | It READS a live `gopi ui` screen — the ui surface's own operand, consumed. It follows `ui`: whichever parent the view lands under, the reader follows it. |
| `version` | concept | n/a | The build-identity dimension: its subject is which binary is running, a noun no other verb owns. |
| `vision_analyze` | demote-to:tool | landed | The image-UNDERSTANDING dimension: its subject is the picture being read (the mirror of the picture producers), and it is a tool's own CLI face. **Retired from the top level (verb-retirement epic, 2026-10-06):** the word's ONLY door is `gopi tool vision_analyze`, the flat spelling is refused by name with that address, so the demotion LANDED. |
| `voice_input` | demote-to:tool | landed | The audio dimension: it introduces speech/microphone input as an operand. **Retired from the top level (verb-retirement epic, 2026-10-06):** the word's ONLY door is `gopi tool voice_input`, the flat spelling is refused by name with that address, so the demotion LANDED. |
| `web` | concept | n/a | The browser carrier dimension, and the group its two tool mirrors already live under (`gopi web search` / `gopi web fetch`). |
| `web_fetch` | demote-to:tool | landed | A tool-wire mirror of the `web_fetch` TOOL. It dispatches as `gopi web fetch` and, since this round, as `gopi tool web fetch`; the flat top-level spelling is now REFUSED by name (it used to print a deprecation note and run). |
| `web_search` | demote-to:tool | landed | A tool-wire mirror of the `web_search` TOOL. It dispatches as `gopi web search` and, since this round, as `gopi tool web search`; the flat top-level spelling is now REFUSED by name (it used to print a deprecation note and run). |
| `worker` | concept | n/a | The dispatch dimension: its subject is WHICH row runs (the claim), a noun `task` does not own. |

**Reading the classification.** Re-derived from the table above, never
hand-carried: **44 rows — 10 are `concept`** and stay, **34 are
`demote-to:<parent>`** across 7 parents (`tool` 18, `session` 5, `task` 4,
`config` 2, `memory` 2, `web` 2, `ui` 1); of those **30 are `landed`** and
**4 are `next`** — `cli`, `notify`, `ui`, `ui_state`. That last figure is the
standing agenda, and it is the set of rows whose STATUS column reads `next`,
nothing else:

*(This paragraph read `21 rows are concept / 23 demote-to / 12 landed / 11 next`
until 2026-11-08 — arithmetically stale against a table that had already moved
through the verb-retirement and merge-removal rounds: the classification table
today carries 44 rows and no `doccheck` or `replay` row at all, while `merge`
survives as a `landed` demotion whose verb no longer dispatches. The
figures are re-derived from the table, never hand-carried; the `concept` and
`demote-to` tallies are the ones `go test ./cmd/agent/ -run
TestVerbClassificationCoversEveryDispatchedVerb` walks, and the
`landed`/`next` split is read off the status column.)*

| parent | verbs waiting | what the parent round must decide |
|---|---|---|
| `task` | `notify` | whether the ledger's own event stream is a face of planning |
| `web` | `cli`, `ui` | whether a display technology is a second-level VIEW (`gopi web ui`) — the node's reading follows from the two verbs' own definitions |
| `ui` | `ui_state` | follows `ui`: the reader re-homes with the surface it reads |

*(Every earlier agenda row — `capture`'s `act` + `screen_capture`, `text2img`'s
`annotate` + `gif` + `text2img-verify`, `code-search`'s `keyword_search`,
`task`'s `ledger-window`, and the RESOLVED rows for `merge`, `session`'s
`checkpoint` and `memory`'s `brew`/`memsync` — is gone from this table because
those demotions LANDED: the decisions and their evidence live in the
classification rows themselves, and an agenda that keeps listing a settled verb
is the same defect as a tally that keeps counting a deleted row.)*

**The recorded screen, decided 2026-11-08 — `screen` stays `concept`.** The
proposal was to demote it to `session` on the ground that
`gopi screen replay` is the twin reader of the same artifact as `gopi session
replay`. Run the reading and that premise fails: `gopi session replay` is a
RENDERER (it picks a canvas, paces frames, and falls back to the reduced dump
when the session recorded no stream), while `gopi screen replay` writes the
paint bytes unchanged and REFUSES a stream-less session by name rather than
substituting the dump. Two verbs, two answers to two different questions about
two different artifacts — the same distinction the `capture` / `shot` pair
already carries. The criterion asks what a word's SUBJECT is, and this one's
subject is a recorded byte stream no other verb introduces; that it is *stored
in* a session directory is where the artifact lives, not whose noun it is. Its
row above states this in full and needs no change.


## What this census does NOT claim

* It does not claim the `a:test` verbs are *well* tested — only that a test
  hands each one a real argv. Depth is the per-feature suites' business.
* It does not claim the `test_drive` count is a quality score: it counts
  *sites*, and `task` dominates the corpus simply because the ledger has the most
  surfaces.
* **Every column of the table above was re-derived in one run**, at this
  document's base `ae284c9d3527cbca16a72a6f90e35ad1c1321f04`
  (`bash scripts/g7-verb-census.sh ./bin/gopi`) — not carried over from an older
  base. That replaced an older state where the counts were stamped at several
  different revisions (`216d3d41` for most rows, `942a44ac` for `notify`'s, and
  a drift note for eleven others). The columns still MOVE with every landed
  branch — `test_drive`/`doc_recipe` are readings of this tree's test and devdoc
  literature, `usage_log` of this host's corpus — so a later reader must expect
  the counts to be stale and the classes not to be: **a drifted COUNT is not a
  drifted CLASS**, the gate re-derives the class, and the class is what this
  table claims. Running `bash scripts/g7-verb-census.sh ./bin/gopi` re-derives
  the whole table.
* The `usage_log` column reads the corpus of the profile `GOPI_HOME` names. A
  verb that ran on another host, or in a profile since rotated away, reads 0
  there — the column is evidence of a run, never proof that none happened.
