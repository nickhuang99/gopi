<!-- doc-class: historical -->
# Task Report: `gopi help` format — concise top-level help, env table behind `--help-env`

Epic: *gopi help must be in good format* (2026-09-13). The briefing: `gopi -h` was
extremely long (a full env-var table inlined into the everyday help), the text was
careless (the attachment showed the `gopi task` block as a sloppy word-wrapped dump
ending in `(run 'gopi task' or 'gopi task -h' for the full family help)`), and several
dispatchable subcommands were not advertised at all.

## Completed

### 1. The env-var table left the top-level help

`internal/config/help.go`:

- `FormatUsage()` no longer inlines `formatEnvTable()`. The everyday help keeps a
  compact **Options** list (each flag next to its env var) and points at
  `gopi --help-env` for the complete table.
- `FormatEnvTable()` — already wired to `--help-env` / `help-env` in `cmd/agent/main.go`
  — is now the ONLY renderer of the table, and it gained a preamble stating the three
  equivalent spellings and the precedence order.

Result: `gopi -h` **468 lines / 29 556 bytes → 129 lines / 7 713 bytes** (−72% lines,
−74% bytes). `gopi --help-env` keeps the full table (199 lines, all 74 catalog rows ×
env + settings key + flag + prose help).

### 2. Format is rendered, not hand-spaced

The old renderer used `fmt.Sprintf("%-28s …")` — a fixed width that the long catalog
names (`GOPI_COMPACT_MAX_SUMMARY_TOKENS`) silently outgrew, so rows lost their
alignment. The new renderer derives every column from the data:

- `helpColumn(rows, floor)` sizes a block's summary column from its widest name —
  now used by the top-level blocks too (Usage / Inputs / Options / Examples), which
  were still hand-spaced literals in the first pass: `--vision-base-url URL` and
  `--reasoning-effort LEVEL` had outgrown the fixed padding and left the Options table
  ragged (the exact defect the epic complains about, in the front-door block). They are
  now `[][2]string` data rendered through the same renderer, with
  `usageMinCol`/`inputMinCol`/`optionMinCol`/`exampleMinCol` as column floors.
- `renderRows` emits `name + pad + summary`, wrapping the summary under ITSELF at the
  same column (a continuation line never starts under the name).
- `wrapText(s, indent)` wraps to `helpWrapCol` and never splits a word — a truncated
  env var name or flag would be worse than a long line.
- `formatEnvTable` measures the env/key/flag columns from the catalog.

The layout budgets are named constants (`helpWrapCol`, `helpHeadMaxCol`,
`envTableHelpCol`, `commandIndexMinCol`, `helpRowIndent`) — no inline width literals
(Rule 1a).

### 3. Every dispatched command is now advertised (the P0)

The reference build's `gopi -h` omitted **8** dispatchable commands. Verified by
comparing `cmd/agent/main.go`'s dispatch chain against the rendered text:

| command | before | after |
|---|---|---|
| `gopi proxy` | missing | listed |
| `gopi chat` | missing | listed |
| `gopi brew` | missing | listed |
| `gopi act` | missing | listed |
| `gopi voice_input` | missing | listed |
| `gopi scratch-gc` | missing | listed |
| `gopi screen_capture` | missing | listed |
| `gopi memsync` | missing | listed |

(memory #1252 had already flagged the `proxy` case as P0.) The index is grouped by
intent — Task ledger & execution / Coding & session inspection / Chat & web / Tools /
Memory & knowledge / Configuration & maintenance — with ONE line per command and a
pointer to `gopi <command> -h` for the full contract.

### 4. The careless text is gone

- The `gopi task` family is now a single index entry that *enumerates* the verbs
  (`adopt, archive, …, verify — append-only, never deleted`) instead of a broken
  word-wrapped dump pointing at itself. The attachment's fragment
  (`run 'gopi task' or 'gopi task -h' for the full family help`) is asserted absent by
  test.
- The nine multi-paragraph subcommand essays (worker, merger, log, stats, raw,
  memory ×8, task ×20, web, checkpoint/resume, completion, examples, image-input,
  `@`-file, attachment archiving) moved out of the top-level text; each already has a
  complete `-h` of its own (`cli_*.go` usage constants) which the index now points at.

### 5. Drift guards (tests)

- `internal/config/help_format_test.go` (new): `renderRows` alignment + wrap oracle,
  `helpColumn` growth, `wrapText` budget/word-safety, env-table alignment + prose
  completeness, `FormatUsage` must NOT inline a catalog entry's prose (content-based,
  so a re-inline fails even if reformatted), and
  **`TestTopLevelBlocksAreColumnAligned`** — every top-level block is rendered from
  data, so no row can outgrow its column again (the guard for the ragged-Options
  defect the second pass fixed).
- `cmd/agent/usage_test.go`:
  - **`TestTopLevelUsageAdvertisesEveryDispatchedVerb`** (new, the anti-omission guard):
    the expected verb set is DERIVED from `cliContractDispatchPredicates` ×
    `dispatcherOfToken` (themselves guarded against `cmd/agent/main.go` by
    `TestCLIContractInventoryMatchesDispatch`), so adding a dispatcher without an index
    entry fails the build. This is what closes the 8-missing-commands hole for good.
  - `TestTopLevelUsageDelegatesEnvVarsToHelpEnv` (replaces
    `TestTopLevelUsageListsEveryEnvVar`): `--help-env` carries every `EnvVars` entry;
    the top-level text must not carry a table row; a `topLevelUsageMaxLines = 160` cap
    catches re-bloat.
  - `TestTopLevelUsageIsAlignedAndTerse`: no line past the wrap budget + the attachment's
    careless fragments absent.
  - Feature tests (vision/text2img/code-search/native-image/task-ledger) now assert
    the top-level INDEX half, with the parameter detail asserted against the command's
    own `-h` — the correct owner of that detail.
- `cmd/agent/merger_top_level_e2e_test.go`: `TestTopLevelUsagePromotesMergerOutOfTaskGroup`
  updated to the index shape (merger is a listed verb saying what it merges; the
  deprecated `gopi task merger` block is no longer re-advertised at the top level).
- `cmd/agent/cli_contracts_test.go`: the `-h` fingerprint follows the new headline.

### 6. One constant backfill (Rule 13)

`internal/config/settings.go` gained `settingsPathKey = "settings_path"`, referenced by
both the catalog entry and the help renderer instead of the bare literal.

### 7. Master integration (40 commits behind at delivery)

The branch was 40 commits behind `master` (which had meanwhile added the
`gopi merger --status | --stop | --restart | --start [--force] [--wait D]` contract to
the OLD inlined help). `git merge master` conflicted in `internal/config/help.go` — the
whole inlined env table + subcommand essays on master's side vs the rewritten index on
ours. Resolution: keep the index (`--ours`), then carry master's NEW content into it —
the merger index row now also advertises the daemon-control flags and points at
`gopi merger -h` (which owns the full `--stop`/`--force`/`--wait` contract). Nothing
master added was dropped: the merger contract text lives in `mergerUsageHead`
(`cmd/agent/main.go`), which the index points at; the `gopi task submit`/`@`-file/
attachment-archiving essays likewise live in `tasksUsage` behind `gopi task -h`.

## Not Done

- **Subcommand `-h` bodies** were not reformatted; they remain verbose (e.g.
  `gopi task -h`). The top-level help is now an index, so the verbosity is one `-h`
  hop away instead of in the user's face. Measured inventory of the remaining
  over-budget (>88 col) synopsis lines, each already present on `master`:
  `gopi log -h` 3, `gopi task -h` 3 (one is the unavoidable
  `gopi task sync [--peer … | --full]` flag grammar), `gopi brew|memory|stats|resume -h`
  1 each. None of these is in the top-level text — `gopi -h` has zero. A follow-up
  could route those constants through the same `wrapText`/`renderRows` helpers; doing
  it here would have re-pinned a dozen golden assertions for a body the user opts into.
- **Verb-NAME consistency** for the tool-wire mirrors: the index still shows
  `gopi vision_analyze`, `gopi screen_capture`, `gopi voice_input` (tool wire names)
  alongside CLI verbs like `code-search` / `text2img`. Renaming those verbs was out of
  scope: they are dispatch tokens pinned by `dispatcherOfToken` / the contract inventory
  / completion scripts, and a rename is a separate, riskier change (deprecation aliases
  needed). The index's grouping and prose now make them unambiguous.
- `gopi merger -h`'s "logs/merger.log" detail is no longer restated at the top level
  (the index says what it merges and points at `-h`); its assertion moved accordingly.

## Verification

- Build: `CGO_ENABLED=0 go build -o bin/gopi ./cmd/agent/` → OK
- Rendered output (real binary, after the master merge):
  - `./bin/gopi -h` → exit 0, 130 lines / ~7.9 KB, widest line 88 cols (was 468 / 29 556,
    ragged)
  - `./bin/gopi help`, `--help` → identical text, exit 0
  - `./bin/gopi --help-env` / `help-env` → exit 0, 199 lines, 74 catalog rows
- Coverage audit: every verb in `cmd/agent/main.go`'s dispatch chain appears in the rendered help
  (8 previously missing → all present); `gopi --help` no longer contains
  `run 'gopi task' or` / `full family help`.
- Tests: `go test ./internal/config/ ./cmd/agent/` → PASS on the pre-merge tree
  (config 0.06s, cmd/agent 115.5s) and PASS again on the merged tree
  (`ok github.com/nick/gopi/cmd/agent 158.5s`, `ok internal/config (cached)`).
- Prose assertions in `usage_test.go` now match on the whitespace-FOLDED text
  (`flat()`, a small helper): the first pass rendered new prose through the wrap
  renderer, so a sentence that used to be one line ("no OCR round-trip") could break
  anywhere — where a sentence wraps is a layout property, not a contract.
  `internal/config/config_test.go`'s env-name-as-flag sentence constant was updated to
  the re-flowed wording (the parser path it pins is unchanged).
- `gofmt -l internal/config cmd/agent` → clean.
- Full suite: `GOPI_BIN=$PWD/bin/gopi go test -count=1 ./...` → two failures, BOTH
  pre-existing and reproduced on master HEAD:
  - `internal/stylegate` `TestStyleGate` — ignored-error 189 > ceiling 182, test-sleep
    9, test-skip 1, unchecked-close 7; identical counts on master `d79e1982`.
  - `internal/testutil` `TestWorkerOwnershipPointerPresent` (2019-era worker-ownership
    pointer gate) and an occasional `/dev/shm/gopi-test-scratch/suite-locked` permission
    artifact from a leftover locked scratch dir — both reproduce on master.
  No test in this change added a `_ =`, a sleep or a skip.
- Live binary smoke: `./bin/gopi --gopi-timeout-sec 0 --max-tokens 16 --prompt "say OK"`
  → `OK` (env-name-as-flag path intact through the rewritten options block).
- Per-command `-h` smoke (every listed command really is dispatchable):
  `gopi task -h` exit 0; `gopi proxy|chat|brew|act|voice_input|scratch-gc|screen_capture|memsync -h`
  each print their own usage headline.
- Commits: `523b18ff` feat(help): index-style top-level help, env table behind
  `--help-env`; `64602268` fix(help): render Usage/Inputs/Options/Examples from data;
  `3b749f71` docs(help): verification record; `537bc16f` fix(help): re-wrap the
  task-submit synopsis prose that outgrew its column; + the `master` integration merge
  (index row for `gopi merger --status/--stop/--restart/--start`) — branch
  `feat/help-format`, worktree `~/workspace/gopi-help`.

---

## Follow-up pass (same epic): the SUBCOMMAND half was still unformatted

The pass above fixed the front door and left a hole behind it. A re-audit of the
rendered binary found **14 lines over the budget across the subcommand `-h`
bodies**, none of which route through the renderer, and **no guard whatsoever**
over those constants (the only width test covered the top-level text).

Measured before (display columns, `max` per surface):

| surface | before | after |
|---|---|---|
| `gopi task -h` | 354 lines, max **152** | 357 lines, max 88 |
| `gopi log -h` | 67 lines, max **103** | 67 lines, max 87 |
| `gopi memory -h` | 109 lines, max **102** | 111 lines, max 80 |
| `gopi resume -h` | 28, max 92 | 28, max 78 |
| `gopi stats -h` | 73, max 89 | 73, max 85 |
| `gopi vision_analyze -h` | 24, max 91 | 24, max 81 |
| `gopi web search -h` | 25, max 89 | 25, max 82 |
| `gopi web -h` (group) | 25, max 95 | 25, max 87 |
| every other dispatched verb | already ≤ 88 | ≤ 88 |

### 1. Display width, not bytes (`internal/config/width.go`)

`len(line)` counts BYTES: a CJK line reads as three times its real width, so the
old guards would have been loud about `知` and blind about a 100-column ASCII row.
Rune count is the opposite error (half). The budget is a terminal's columns, so the
measure is now `config.DisplayWidth` (East Asian wide/fullwidth + emoji = 2 columns)
and the budget is the exported `config.HelpWrapCol` — one definition for both the
renderer and the `cmd/agent` guard (Rule 14: a second copy in `cmd/agent` would
drift). `helpColumn`/`renderRows`/`wrapText` measure with it too, so a wide name
landing in a rendered table can no longer shift the summary column.

Two `--help-env` head lines are **legitimately** over the prose budget (91 and 90
display columns): an entry head is the catalog triple (env name / settings key /
flag), i.e. DATA, budgeted by `helpHeadMaxCol`; it cannot wrap without destroying
the columns. That is a documented exception, not a defect.

### 2. Content: jargon and machine paths out of user-facing help

The same pass removed what made the help read like a changelog rather than a manual —
the epic's "careless text" complaint, beyond the one attachment fragment:

- roadmap jargon in prose: `M4.6 … M4.24`, `P2.5`, `bug038`, `epic ③`, `HP→peerbox`,
  `--full (M4.24)` — 20 sites across `cli_tasks.go`, `cli_tasks_sync.go`,
  `cli_chat.go`, `cli_memsync.go`, `cli_resume.go`, `cmd/agent/main.go`, plus the env-section
  title `Remote dispatch (M4.7)`;
- developer machine paths: `/home/user/workspace/out/...` in the `resume`,
  `vision_analyze`, `act` and `shot` examples;
- a hand-broken clause (`Refused when >=3` / `pending rows already sit in the queue`
  — a sentence an editor's wrap split mid-clause) rejoined into one line;
- the `gopi memory provenance` synopsis printed TWICE (once for the shape flags, once
  for the latent flags) collapsed into one synopsis with a wrapped continuation;
- pipe-separated enums with no spaces (`--latent-crosscheck|--latent-calibrate`,
  `bing|google|arxiv|…`) spaced out;
- the log `--memory` row's own tool list reduced to `(search/store/…)`: the detail is
  already in the `--memory  memory_search memory_store …` shortcut table three lines
  below.

### 3. The guard that was missing (`cmd/agent/help_width_test.go`)

- `TestEveryHelpConstantFitsTheWrapBudget` — **discovery-based**: `go/parser` walks
  every non-test file of the package, collects every raw-string constant whose name
  says it is help text, and fails any line over `config.HelpWrapCol`. A new usage
  block is covered the moment it is written; nothing is hand-listed. It asserts it
  found ≥ 30 constants, so a silently-blind sweep cannot pass (the failure mode
  `internal/standards`' env-reader gate documents). `shotUsage` is a `fmt` template
  and is rendered with real defaults before measuring.
- `TestHelpConstantsCarryNoCarelessText` — the content gate: the attachment fragment,
  the hand-broken clause, the unspaced enum, `/home/user/`, and the jargon regexes
  (`M4.\d+`, `bug\d{3}`, `P\d.\d`, `epic ①-⑳`) — plus "every multi-line help constant
  is a raw string", so escapes cannot hide from the sweep.
- `TestHelpWidthPredicateIsNotBlind` and
  `internal/config`'s `TestHelpSurfacesCarryNoInternalJargon` — the red proofs,
  including the CJK case both `len()` and rune-counting miss.
- The pre-existing guards were switched to display width
  (`usage_test.go`'s "is aligned and terse" dropped its local `maxLine = 92` literal
  for `config.HelpWrapCol`).

**Red proof:** with the guard and `width.go` in place and the fix stashed, the sweep
reported exactly the 14 offending lines (log 96/101/103, memory 102, resume 92,
stats 89, task 94/90/152 across the same files) — then went green after the fix.

## 4. Final integration — `feat/voice-help` and the index/detail split

The second master merge (after `feat/voice-help`) exposed the design tension
plainly: that branch had independently re-added verbose top-level blocks for
`voice_input`, `chat`, `brew`, `memsync`, `proxy`, `screen_capture`, `act` and
`scratch-gc` — the exact shape this epic removes (it is what made `-h` 468 lines).
The resolution keeps the epic's rule and the other branch's intent:

- **The top-level page is an INDEX.** One line per verb: name, what it does, and
  where the detail lives (`gopi <verb> -h`). `voice_input`'s row says
  "`gopi voice_input -h` has the usage shapes and the engine contract".
- **The verb's own `-h` owns the detail.** `voiceInputUsage` (already written by
  `feat/voice-help` in `cmd/agent/cli_voice.go`) carries the three usage shapes,
  the `POST /transcribe` contract, the CER record and the "CLI verb only — no
  agent tool" rationale. That file gained an `Examples:` block for symmetry with
  its siblings.
- **The guard was split, not weakened.** `TestTopLevelUsageShowsVoiceInput` now
  asserts the index half against `usage` (`gopi -h`) and the detail half against
  `voiceInputUsage` (`gopi voice_input -h`) — discoverability AND detail both stay
  pinned. `TestTopLevelHelpCoversEveryDispatchedSubcommand` (the class-level
  guard) is unchanged and still passes.

Rendered truth on the merged tree: `gopi -h` = 131 lines, max 88 cols, 0 lines
over `HelpWrapCol`; `gopi voice_input -h` = 44 lines, 0 over budget; `gopi
--help-env` = 199 lines / 74 catalog rows.

### The width budget is one number, in one place

`internal/config/width.go` exports `HelpWrapCol = 88`; `helpWrapCol` aliases it
(Rule 14 — the subcommand constants live in `cmd/agent`, whose guard must measure
against the *same* budget, and an unexported constant cannot cross packages).
`DisplayWidth`/`IsWideRune` make every measurement display-based: a CJK glyph or
emoji is two columns, so padding by `len()` or by rune count shifts the summary
column under a wide row (the CJK "相片" probe in `TestHelpWidthPredicateIsNotBlind`
is the red proof for both).
