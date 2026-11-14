<!-- doc-class: historical -->
# `gopi -h` — a user-perspective audit, line by line, against the real binary

<!-- WORKER-OWNERSHIP: OPERATOR-ONLY — devdoc/worker-ownership.md -->

Truth class: **historical** — the record of that run, not a standing claim about
today's tree: the rows below are what the built binary printed at the commit the
run measured (`ac35c924f`, task `01a113a8-…`, 2026-10-06). The class was
re-derived from the corpus's own classifier (`tools/docclass/classify.py`) when
this file's banner and its `devdoc/DOC-INDEX.tsv` row disagreed; only this
classification line changed, and the run's own account below is untouched.

This document answers one question with a table instead of an impression: **every
line `gopi -h` prints — is it TRUE?** It exists because the operator read the page
and asked a question the page could not answer: 「merger 既然都不存在来为什么还要
提一遍？这不是混淆吗？」 — a row for a command that does not work. That turned out
to be the page's ONE false line, and finding that out required running all of them.

## The method (and why the number in it is not an opinion)

Every command row of the rendered index was executed **as printed**, with `-h`,
against the **BUILT BINARY** in an **isolated `GOPI_HOME`** (the standard sandbox
shape, Rule 34), and the exit status recorded. Two things this buys:

- **The binary, not the source.** A help constant and the page a user sees are two
  artefacts; the census gate already learned this (a stale `bin/gopi` reports the
  pre-change page). Every row below is evidence from `./bin/gopi`.
- **An isolated profile.** Probing the operator's own `GOPI_HOME` would have
  written sessions into the live ledger, and a refusal caused by the operator's
  state would have been misread as a defect of the help.

```bash
CGO_ENABLED=0 go build -o bin/gopi ./cmd/agent/
GOPI_HOME=/home/user/workspace/gopi-sandbox/sandbox-clihelp-audit-c48c0da5 ./bin/gopi -h
```

## The finding, in one line

**Exactly one row of the 42-line pre-fix index was false**: `gopi merger`
(RETIRED 2026-09-19) answered `-h` with a refusal and **exit 1** — and the page's
own preamble, two lines above it, promises that every command it lists *"accepts
`-h` for its full help"*. Every other row held.

| | pre-fix | post-fix |
|---|---|---|
| command-index rows (the `Commands …` section) | 40 | **39** |
| rows whose `<verb> -h` exits 0 | 39 | **39** |
| rows whose `<verb> -h` exits ≠ 0 | **1** (`gopi merger`) | **0** |
| `gopi <TAB>` first-level candidates (3 shells) | 16 | 16 |

The diff of the two runs is exactly one line, and it is the line that was false.
Counts re-derived at each state; the full runs are
`artifacts/verify-all-rows-fresh.txt` and `artifacts/acceptance-rows.txt`.

## The table — every row, its real result, its verdict

Run as `gopi <row prefix> -h`; `rc` is the process exit status.

| index row | rc | verdict |
|---|---|---|
| `gopi task <subcommand>` | 0 | TRUE — prints the family page |
| `gopi worker [<task-id>]` | 0 | TRUE |
| ~~`gopi merger`~~ | **1** | **FALSE — RETIRED; answers help with a refusal. REMOVED** |
| `gopi merge` | 0 | TRUE |
| `gopi task archive <branch>` | 0 | TRUE |
| `gopi session <subcommand>` | 0 | TRUE |
| `gopi tool code-search <symbol>` | 0 | TRUE |
| `gopi tool keyword_search <query>` | 0 | TRUE |
| `gopi chat [--tools]` | 0 | TRUE |
| `gopi web [--tools]` | 0 | TRUE |
| `gopi ui_state [--pid N] [--json]` | 0 | TRUE page; see finding B (pointer) |
| `gopi web search "<query>"` | 0 | TRUE |
| `gopi web fetch <url>` | 0 | TRUE |
| `gopi tool proxy` | 0 | TRUE |
| `gopi tool <command>` | 0 | TRUE |
| `gopi tool capture` | 0 | TRUE |
| `gopi tool screen_capture` | 0 | TRUE |
| `gopi tool act <action>` | 0 | TRUE |
| `gopi tool annotate` | 0 | TRUE |
| `gopi tool gif` | 0 | TRUE |
| `gopi tool shot [--url URL\|--self]` | 0 | TRUE |
| `gopi tool vision_analyze <image>` | 0 | TRUE |
| `gopi tool voice_input <audio>` | 0 | TRUE |
| `gopi tool text2img <input.txt>` | 0 | TRUE |
| `gopi tool text2img-verify` | 0 | TRUE |
| `gopi tool code-search <symbol>` | 0 | TRUE (the tool's own spelling — see finding A) |
| `gopi tool keyword_search <query>` | 0 | TRUE (same) |
| `gopi tool doccheck` | 0 | TRUE |
| `gopi tool proxy` | 0 | TRUE (same) |
| `gopi tool sandbox <subcommand>` | 0 | TRUE |
| `gopi tool scratch-gc` | 0 | TRUE |
| `gopi tool ledger-window` | 0 | TRUE |
| `gopi memory <subcommand>` | 0 | TRUE |
| `gopi memory sync` | 0 | TRUE |
| `gopi memory memsync` | 0 | TRUE |
| `gopi memory brew` | 0 | TRUE |
| `gopi config show` | 0 | TRUE |
| `gopi config setup` | 0 | TRUE |
| `gopi config completion <shell>` | 0 | TRUE |
| `gopi notify <subcommand>` | 0 | TRUE |

**Rows that name a flag in the index row itself** (`gopi tool shot [--url URL|
--self]`, `gopi tool act <action>`, `gopi memory memsync`, …, the ones whose
argument is part of the printed row): run with the row's literal prefix and no
argument, all exit 0. Two of them exit **1 when handed the placeholder as a
literal argument** (`gopi ui_state 'N]' -h`, `gopi tool shot 'URL|--self' -h`) —
that is correct behaviour, not a defect: the placeholders are documentation
syntax, not argv. Recorded here so a later reader does not "fix" a refusal that is
the parser working.

## Findings

### A. The index carries the same command twice, under two spellings (P3, stylistic)

`gopi tool code-search <symbol>`, `gopi tool keyword_search <query>` and
`gopi tool proxy` appear in **two sections** — once in their hand-written
narrative section (`Coding & session inspection`, `Chat & web`) and once in the
generated `Tools (the agent's own tools, from the shell)` enumeration. Verified:
2 exact-duplicate rows each (`artifacts/verify-dups-fresh.txt`).

This is **not** the `merger` defect: both spellings work, both exit 0, and each
one is where a user would look for it. It is a readability cost (the page looks
longer than it is; a reader scanning "Chat & web" sees `tool proxy` and wonders
whether Tools will repeat it — it does). **Left as-is**: a fix means deciding
which section owns a tool mirror, which is a surface decision (Rule 26 puts it in
its own task), and the page is inside its line budget.

### B. `gopi ui_state`'s own page points at `gopi ui`, which is not in the index (P3, by design)

`gopi ui_state -h` opens with *"read a live `gopi ui` screen state…"*, and
`gopi ui` has **no index row** (withheld by ruling, task `01a10dba…`: 暂不暴露 —
0 matched rows, measured). A reader may follow the pointer and find nothing.

**This is intended and already guarded**: `cmd/agent/cli_help_hidden_verbs_test.go`'s
`TestTopLevelHelpPointsAtNoCommandItDropped` asserts the *top-level* index points
at nothing it does not carry, and `gopi ui` still RUNS (`rc=0`) — the word is
withheld from publicity, not removed. The residual cost is real but small: a
verb's own page names its sibling by capability, and the sibling is runnable.

### C. Two commands whose `-h` is a bare `usage:` line (P3, stylistic)

`gopi tool text2img-verify` and `gopi tool ledger-window` answer `-h` with a
one-line `usage:` (both exit 0 — TRUE claims, complete for their argument set).
They are the two-token sample tools; a full page would be longer than the command
is. Recorded, not changed.

### D. What was checked and found NOT to be a defect

Stated explicitly, because "I did not look" and "I looked and it is fine" read the
same in a report otherwise:

- **Every row's `-h` is its OWN page**, not a fall-through to the top-level index
  (the failure mode that makes a row useless while looking fine).
- **The preamble's promise** (*"each accepts `-h` for its full help"*) is TRUE
  for all 39 remaining rows — this is precisely what the `merger` row broke.
- **The Examples block** (`gopi "write a README for this repo"`, …) is not a
  command list; those lines are argument examples and are excluded from the table
  by construction. The `Usage:` block above it is the same shape.
- **`gopi` with no arguments** prints usage and exits 0 (measured, stdin at EOF).

## The fix

One row, two surfaces, and ONE definition:

- `internal/config/help.go` — the `commandIndex` row is removed. The comment in
  its place states the decision, the date, and the operator's words, so the next
  reader meets a decision rather than a gap.
- `cmd/agent/cli_completion.go` — `completionSuppressedVerbs` gains `CliVerbMerger`
  beside `ui` / `cli`. This is the **one list** that withholds a dispatching verb
  from BOTH published surfaces (the help index and `gopi <TAB>`), so the two
  cannot drift apart — which is exactly the invariant the file's own
  `TestCompletionSuppressesTheSameVerbsAsHelp` enforces, and it was already the
  mechanism built for `ui` / `cli` in `cc8289b0`. Reused, not reinvented
  (Rule 8: 复用不重做).
- The word **keeps dispatching** (`isMergerSubcommand` is untouched, the refusal
  is untouched): an old script or service unit still reads the retirement sentence
  and the pointer to `gopi merge` instead of "unknown command" (Rule 15).

## Not Done (honest)

- **The `notify` row survives a "should `notify` be top-level?" question.** The
  census's own classification table marks it `demote-to:task` with status `next` —
  i.e. a standing, agreed, not-yet-done demotion. That is a SURFACE decision with
  its own agenda row, deliberately not taken here.
- **Finding A (duplicate rows) is reported, not fixed** — reason above.
- **The page's line count rose by 14 lines** (the pre-fix index section was 40
  rows over the sections I measured; the page as a whole is compared in the
  Verification section). No budget is threatened: `topLevelUsageMaxLines` is
  enforced by `TestTopLevelUsageIsAlignedAndTerse` and is green.

## Verification

See `task-report.md` in this run's session directory for the full `Build / Tests /
Ran / Result / Commit` block. The two facts worth repeating here:

- `gopi merger -h` → **rc=1**, stderr names the verb, the cause and `gopi merge`;
  `gopi ui -h` / `gopi cli -h` → **rc=0** with their own pages.
- `gopi -h` → **0 rows** matching `^  gopi merger( |$)` (was 1),
  and the three generated completion scripts offer **0** of the three suppressed
  words.
