<!-- doc-class: normative -->
# The session-inspection + resume group's help-surface inventory (G4 of the help-surface hardening)

<!-- WORKER-OWNERSHIP: OPERATOR-ONLY — devdoc/worker-ownership.md -->

**Task**: 01a0c2d1-9e10-7ad9-912d-ae0f47618775 (epic brief:
`briefings/brief-help-surface-hardening.md`, group G4).
**Branch**: `task/01a0c2d19e107ad9912dae0f47618775`.
**Question this document answers**: for every verb/flag this group's help
advertises — `gopi session log`, `gopi session stats`, `gopi session raw`,
`gopi session checkpoint list|export`, `gopi session resume` (incl. `--task`) —
does a BASIC test exist that INVOKES it the way the help says to invoke it,
never `-h`? (**The five words gained their family address in verb-retirement
phase 1, 2026-10-05**: the flat top-level spellings are refused by name now, so
the invoking addresses below — and the whole table — are the ones the live help
prints. Every row was re-derived, not retyped.)

**Why a test and not a reading.** The family's motivating defect (G1's
`gopi task sync --push-only --task-id T`) was implemented, compiled, unit-tested,
reported complete and never typed (PROJECT.md Rule 31). A suite entering through a
seam its author chose cannot see that class. "Has a basic test" below therefore
means **the help's own argv was driven through the real binary**, against a
sandboxed `GOPI_HOME` and a real session fixture.

## Re-derivation (the quoted command)

The surface list is DERIVED, never hand-typed. Two independent derivations,
which the build requires to agree:

```bash
# 1. the operator-facing derivation (this document's table)
$ CGO_ENABLED=0 go build -o bin/gopi ./cmd/agent/
$ scripts/g4-session-help-surface.sh ./bin/gopi
log	Usage	gopi session log <session-id>
log	Usage	gopi session log <session-id> <round>
log	Usage	gopi session log <session-id> <round> --request
log	Usage	gopi session log <session-id> <round> --response
log	Usage	gopi session log <session-id> <round> --thinking
log	Usage	gopi session log <session-id> <round> --filter TOOL
log	Usage	gopi session log <session-id> --turns
log	Usage	gopi session log <session-id> --toolcall N
log	Usage	gopi session log <session-id> --response
log	Usage	gopi session log <session-id> --thinking
log	Usage	gopi session log <session-id> --request
log	Usage	gopi session log <session-id> --request --json
log	Usage	gopi session log <session-id> --chat
log	Usage	gopi session log <session-id> --todo
log	Usage	gopi session log <session-id> --memory
log	Usage	gopi session log <session-id> --context-trim
log	Usage	gopi session log <session-id> --search
log	Usage	gopi session log <session-id> --web
log	Usage	gopi session log <session-id> --job
log	Usage	gopi session log <session-id> --context
log	Usage	gopi session log <session-id> --vision
log	Usage	gopi session log <session-id> --text2img
log	Usage	gopi session log <session-id> --git
log	Usage	gopi session log <session-id> --filter TOOL
log	Usage	gopi session log <session-id> --grep TEXT
log	Usage	gopi session log <session-id> --json
log	Usage	gopi session log <session-id> --text
log	Usage	gopi session log --last
log	Usage	gopi session log --last N
log	Usage	gopi session log --last N --memory
log	Usage	gopi session log --branch BRANCH
log	Usage	gopi session log --all --summary
log	combines with them exactly as it does with the id	gopi session log --branch BRANCH --response
log	combines with them exactly as it does with the id	gopi session log --worktree WT --todo
log	Flags can be combined	gopi session log <id> --response --text
log	Flags can be combined	gopi session log <id> --thinking --text
log	Flags can be combined	gopi session log <id> --request --text
log	Flags can be combined	gopi session log <id> --request --json
log	Flags can be combined	gopi session log <id> --memory --text
log	Flags can be combined	gopi session log <id> --context-trim
log	Flags can be combined	gopi session log <id> --filter bash
log	Flags can be combined	gopi session log <id> --filter read --filter write
log	Flags can be combined	gopi session log <id> --search
log	Flags can be combined	gopi session log <id> --web
log	Flags can be combined	gopi session log <id> --vision
log	Flags can be combined	gopi session log <id> --text2img
log	Flags can be combined	gopi session log <id> --git
log	Flags can be combined	gopi session log <id> --grep timeout
log	Flags can be combined	gopi session log <id> --grep timeout --job
log	Flags can be combined	gopi session log --last --chat
stats	Usage	gopi session stats <session-id>
stats	Usage	gopi session stats <session-id> --rounds
stats	Usage	gopi session stats <session-id> --tools
stats	Usage	gopi session stats <session-id> --bytes
stats	Usage	gopi session stats <session-id> --cost
stats	Usage	gopi session stats <session-id> --bill
stats	Usage	gopi session stats <session-id> --json
stats	Usage	gopi session stats --branch BRANCH
stats	Usage	gopi session stats --last [N]
stats	Usage	gopi session stats --all
stats	Flags can be combined	gopi session stats <id> --rounds --json
stats	Flags can be combined	gopi session stats <id> --rounds --bytes
stats	Flags can be combined	gopi session stats <id> --cost --rounds
stats	Flags can be combined	gopi session stats --last 5 --tools
stats	Flags can be combined	gopi session stats --all --tools
stats	Flags can be combined	gopi session stats <id> --json
raw	Usage	gopi session raw <session-id>
raw	Usage	gopi session raw <session-id> <round>
raw	Usage	gopi session raw <session-id> <round> request
raw	Usage	gopi session raw <session-id> <round> response
raw	Usage	gopi session raw <session-id> --rounds
raw	Usage	gopi session raw <session-id> --lines A-B
raw	Usage	gopi session raw <session-id> --sysprompt
raw	Usage	gopi session raw <session-id> --userprompt
raw	Usage	gopi session raw --file PATH
raw	Usage	gopi session raw --branch BRANCH
raw	Usage	gopi session raw --last [N]
checkpoint	Usage	gopi session checkpoint list [--dir DIR]
checkpoint	Usage	gopi session checkpoint export <log> [--dir DIR]
resume	Examples	gopi session resume 01a096c9-6f7d-7e43-8e46-9ecca58e9cd0
resume	Examples	gopi session resume ~/.gopi/raw/gopi-raw-20260826-120000.log
resume	Examples	gopi session resume ~/.gopi/sessions/01a096c9-6f7d-7e43-8e46-9ecca58e9cd0/....raw.log
resume	Examples	gopi session resume 4c9a1f3e-8b2d-4a10-9e6f-2c5d7a0b1e33
resume	Examples	gopi session resume ~/.gopi/raw/gopi-raw-20260826-120000.log next-step.md
resume	Examples	gopi session resume t-1788048877
resume	Examples	gopi session resume t-1788048877 --task t-1788048877
resume	Examples	gopi session resume ~/.gopi/raw/gopi-raw-20260826-120000.log --task t-1788048877
$ wc -l < <(scripts/g4-session-help-surface.sh ./bin/gopi)
87

# 2. the same list, re-derived in the test package from the SAME usage text
#    (cmd/agent/cli_session_help_surface_basic_test.go, from logUsage /
#    statsUsage / rawUsage / checkpointUsage / resumeUsage):
$ GOPI_TEST_BIN=$PWD/bin/gopi go test -count=1 -run TestG4HelpSurfaceMatchesInventory -v ./cmd/agent/
=== RUN   TestG4HelpSurfaceMatchesInventory
--- PASS: TestG4HelpSurfaceMatchesInventory (0.00s)

# 3. and the table below, read back FROM DISK and compared with derivation 2 —
#    a table describing last month's binary is a red build:
$ GOPI_TEST_BIN=$PWD/bin/gopi go test -count=1 -run TestG4HelpSurfaceInventoryTableMatchesHelp -v ./cmd/agent/
=== RUN   TestG4HelpSurfaceInventoryTableMatchesHelp
--- PASS: TestG4HelpSurfaceInventoryTableMatchesHelp (0.00s)
```

**Totals: 5 verbs, 87 advertised invocations** (32 `log` usage + 18 `log`
combinations, 10 `stats` usage + 6 `stats` combinations, 11 `raw`, 2
`checkpoint` (`list` and `export`) + 8 `resume` examples). Six of the `log` usage
rows are the round view (`gopi session log <session-id> <round>` + its four view
forms; the `--rounds` TABLE row is not among them — see below), and one is the
CALL view added by the canonical-round
unification (`gopi session log <session-id> --toolcall N`,
`cmd/agent/cli_log_toolcall.go`); the `resume` examples are eight because the page
prints eight. The `resume` page's own synopsis is its `Usage:`-prefixed title line, so
the verb's derived rows are its eight worked examples, and the canonical
`session resume <log> [continuation...]` spelling appears on the checkpoint page
as the withdrawn row's replacement prose (see "the shared page is an oracle"
below). The five rows added by the deliverable-selector change
(`gopi session log|raw|stats --branch BRANCH` plus the two `log` combinations)
are the group's first invocations whose *value* is a ledger lookup, and they are
driven against a real sandbox ledger row — see "the deliverable selectors" below.
The verb count is the group the briefing names, and it equals the five dispatch
predicates in `cmd/agent/main.go` (`isLogSubcommand`, `isStatsSubcommand`,
`isRawSubcommand`, `isCheckpointSubcommand`, `isResumeSubcommand`) — the
derivation reads the invocation's own verb word, so a help page that prints a
SIBLING's synopsis lands under the verb it actually names.

**The count moved 87 → 86 in the re-derivation, and the reason is a WITHDRAWAL,
not a rename.** While this table carried 86 rows the Go derivation read 87: the
checkpoint page printed a third Usage line — the shorthand
`session checkpoint list|export [--dir DIR]`, program word and family but no
accepted subcommand word — which the dispatcher answers with its own refusal. It
is prose now (see "the shared page is an oracle" below), so both derivations read
the same 86.

**The same shorthand was withdrawn from the FAMILY page's Usage block, and this
table's count did not move** (2026-10-07). `gopi session -h` printed, among its
seven `gopi session …` synopses, the line

    gopi session checkpoint list|export    → the raw-log snapshot registry

— the identical defect one page up, in a cross-reference rather than in a
synopsis of the page's own subcommands (the session page does not dispatch
`checkpoint list|export`; it points at the third level that does). The line is
gone; the shorthand survives as the paragraph that names both real commands,
which is also where the hierarchy guard's assertion now reads it
(`TestFamilyPageStillTeachesTheCheckpointShorthand`, with the synopsis-shape
refusal in `cmd/agent/cli_session_usage_dispatch_test.go`). **This table is
unaffected and was verified rather than assumed**: its five pages are the VERB
pages (`log`/`stats`/`raw`/`checkpoint`/`resume`), the family page is not one of
them, and re-running the generator byte-for-byte over the rebuilt binary
reproduced all 86 rows — the quoted listing above and the table below are the
run made after the withdrawal.

**The count then moved 86 → 87 with an ADDITION, not a re-derivation** (2026-10-11,
the canonical-round unification). `gopi session log <session-id> --toolcall N`
(`cmd/agent/cli_log_toolcall.go`) is a new advertised surface on the `log` page:
the Nth TOOL CALL of the session, in full, naming the round it ran in. It exists
because "round" had four spellings for one session, and the largest of them
(`stats --rounds`, 348) was exactly the tool-call count — so the CALL address was
kept and the WORD moved to the canonical exchange. Rows 9-87 above are the
previous rows renumbered; the new row is 8, in help order.

**The count then moved 87 → 88 with an ADDITION, not a re-derivation** (2026-10-12,
the session-counter flag rename), **and the naming has since been REVOKED in the
operator's favour** (same day): `round` is the JSONL `request` row — one HTTP
request/response = one iteration — on every surface, and the OPERATOR-row
quantity is the neighbouring `turns`. So the log surface's `--rounds` tabulated
THE ROUND (the same number the playback prints as `rounds=`), and its sibling
table answers to `gopi session log <session-id> --turns`
(`gopilog.SessionTurnFlagName`, `cmd/agent/cli_log.go`).

**The `--rounds` row is NO LONGER ADVERTISED, and this table does not carry it**
(58ec82b96, the ruling that made `round` an internal concept — the log group's
own table is `--turns`). The paragraph above is kept because it records how the
two quantities were separated; what it says about a `--rounds` ROW describes a
state this table has moved past. `stats --rounds` and `raw --rounds` still
advertise and still have their rows. See `devdoc/round-definition-restored.md`.

**The FLAG still parses; only the ADVERTISEMENT is gone — and that distinction
was measured, not read off the ruling.** 58ec82b96 removed the Usage line and
left `logFlagRounds` in `logFlagMember`, so `gopi session log <id> --rounds`
still prints the ROUND table (`SESSION ROUNDS — …`) while the page no longer
lists it; the page's own prose says so in one sentence ("its flag is a
compatibility alias, no longer listed above"), and this inventory follows the
page. An earlier revision of this paragraph claimed "the flag's production parse
was removed in the same commit" — that sentence was false and is corrected here
by measurement, with the invocation and its output rather than a reading of the
code:

```
$ gopi session log d5da7999-21c7-4131-b831-39f4ba42483d --rounds      # exit 0
═══════════════════════════════════════════════════════════════
SESSION ROUNDS — d5da7999-21c7-4131-b831-39f4ba42483d (1 turn(s), 52 rounds)
  ROUND = rounds — one JSONL `request` row = one HTTP request/response = one iteration
```

(The round count in that header is the LIVE session's own and moves as it runs —
the measurement this sentence rests on is that the TABLE printed at all, not the
number in it.)

The test that keeps the parse alive is the cross-surface equality pair
(`cmd/agent/cli_session_replay_test.go`,
`cmd/agent/cli_session_replay_units_test.go`), which drives
`session log <id> --rounds` and `session replay <id> --rounds` and asserts they
report the SAME number. `TestG4LogMovedFlagsStayAdmittedAndRefused` is NOT
evidence for this: it covers the MOVED flags (`--http`, `--raw`), which are a
different contract (admitted so a pointer can be printed, refused at run time).

**The line came BACK once, and the reason is a merge husk, not a re-decision**
(measured 2026-10-25, this task). 13ddb5c (on the integration line) deleted the
Usage line from `cmd/agent/cli_log.go` — so did 58ec82b96 — while the parallel
`turn-is-the-unit` branch 74ec430 was cut from a base where the line still
existed and did not delete it. Merge 966cd4d therefore resolved the file by
taking the SIDE that never made the deletion: the Usage line returned, the flag
parse never left, and the three G4 witness tests went red (88 advertised vs 87
expected — `TestG4HelpSurfaceMatchesInventory`,
`TestG4HelpSurfaceInventoryTableMatchesHelp`,
`TestG4DerivationScriptRunsTheRealBinary`). The fix re-applies 58ec82b96's
deletion to the Usage block only; the parse stays, because `--rounds` is still a
working compatibility alias and removing it would break
`TestG4LogMovedFlagsStayAdmittedAndRefused` and every script that reads the
ROUND table. **A deletion in a help constant is what a merge silently undoes** —
which is why the row count is pinned in three places.

### The deliverable selectors (`--branch` / `--worktree`) in this group

The three session verbs address a session by what an operator standing in a task
worktree holds — `git branch --show-current` (`--branch BRANCH`) or `pwd`
(`--worktree WT`) — instead of by a `<session-id>` that workflow does not carry,
exactly as `gopi task show` and `gopi worker --resume` already do. The value is a
PLACEHOLDER on the pages (`BRANCH` / `WT`, substituted by the case's own fixture),
and the resolution is a LEDGER read: row → the session that row is running. The
cases therefore drive them against a real sandbox ledger whose live row records
that branch/worktree and whose `session_id` IS the fixture session
(`g4SeedSelectorRow`), which is what makes the synopsis real rather than a string
the parser happens to accept.

### What the derivation reads, and what it deliberately does not

The scanner takes **two-space-indented `gopi session <verb> …` lines** (the live
address; the generator probes the same family spelling), cuts each at its
description column (two or more spaces, at column 18+), and cuts a worked
example at its shell `|` — the `session stats <id> --json | jq .prompt_tokens`
example's second half is prose, not argv. A line that breaks the page's own
column rule produces argv and prose run together, and the scanner has NO special
case for that: `cli_log.go`'s `--request --json` synopsis had a single space where
every sibling line has a column, so it was **fixed on the page** (`  session log
<session-id> --request --json  Full first request body as indented JSON`) rather
than papered over in the scanner.

The section a row was printed under travels with it (`Usage:` / `Flags can be
combined:` / `Examples:`), because they are unlike claims: a `Usage:` line is the
canonical surface, a `Flags can be combined:` line is a worked combination, and
the `Examples:` lines are where `resume --task` is spelled.

What is **not** in the table, and why:

- The `session log` page's "Moved to the raw-log commands" section lists `--http,
  --http N, --raw, --http N --request/--response` with the replacement commands.
  These are NOT synopsis lines (`  gopi session log …`), so the derivation does not
  produce rows for them: they are a documented REFUSAL, asserted instead by
  `TestG4LogMovedFlagsStayAdmittedAndRefused` (exit 2 + the replacement command
  on stderr). The flags stay in `logFlagMember` for exactly that reason — removing
  them would turn a named pointer into a generic unknown-flag error.
- Prose that mentions a flag (`--db PATH` in another group's "Common flags:")
  is not a surface and is never read, because it is neither a section header nor
  an indented `gopi <verb> …` line.
- The `resume` page's own usage-head line (`Usage: session resume <task-id|raw-log-
  path|session-id> [continuation-file]`) is `Usage:`-prefixed rather than
  two-space-indented: it is the page's title line, and the canonical synopsis
  appears inside `checkpointUsage` — as TWO rows since the shared page became an
  oracle (below). Both spellings are executed (the target forms are the `resume`
  rows, and `--task` is the last row).

### The shared page was an oracle twice (rows 77–78 today)

`checkpointUsage` is printed by both faces, so every synopsis on it is a promise
the `checkpoint` dispatcher has to honour. Two rounds of that rule, in opposite
directions:

**A synopsis the dispatcher refused.** The page once advertised a THIRD Usage
line — `session checkpoint list|export [--dir DIR]` — a note about the two
subcommands spelled as if it were an invocation. The dispatcher answers the word
after the verb, and `list|export` is not one of the two subcommands it accepts,
so that line was an invocation nobody could type (measured: `session checkpoint
list|export` exits 1 on the dispatcher's own refusal, while the two real lines
run). **It is withdrawn from the Usage block in this change**, kept as prose
instead — a synopsis is a promise, and this one was not kept. The Go
derivation reads the family word off every synopsis, so the same line had also
been filed as the nonsense address `session gopi …`; fixing it on the page is what
makes both derivations agree.

**A synopsis for a word that was removed (epic verb-restructure, 2026-10-05).**
The row before it was `session checkpoint resume <log> [continuation...]` — an
earlier round's fix, when the switch accepted `list|export` only and the page
advertised a surface that answered the dispatcher's `unknown subcommand`
refusal. Once `session resume` existed, the checkpoint spelling was a SECOND door
onto one continuation, so it stopped being a surface this page may advertise: the
synopsis left the usage block, the dispatch case now refuses BY NAME (exit 1)
instead of forwarding, and `checkpointCompletionNames` dropped the word so `<TAB>`
cannot offer it. The page is an oracle in both directions — it may not name a word
the dispatcher refuses, and the dispatcher may not silently accept a word the page
no longer teaches. The dispatch half is pinned
by `cmd/agent/cli_checkpoint_usage_dispatch_test.go`: every line under the page's
`Usage:` heading is handed to the real dispatcher, its own refusal is the red
proof, and the test also compares the promised set with the accepted set in BOTH
directions — so a synopsis added to the page with no dispatch case fails the build
here instead of shipping as a refusal.

### The fixture installs the addresses the resume page advertises

Three of the eight `resume` examples name addresses that do not exist on the
reader's machine, and two of them are FILESYSTEM addresses the verb resolves
without the ledger:

- **the session-DIRECTORY raw log** (`~/.gopi/sessions/<session-id>/<run>.raw.log`,
  the run id elided as `....`). The fixture re-homes its own raw log inside a
  directory named by the fixture session id, so the advertised SHAPE — a
  `.raw.log` reached through a session DIRECTORY — is executed for real instead
  of being left to refuse on a literal `~`, which no host expands.
- **the task-id forms.** Beside the ledger row the fixture seeds the row's OWN
  run directory (`sessions/<task-id>/<task-id>.raw.log` — the address
  `rundir.DirFor` + `rundir.RunDirRawLog` resolve), which is what makes `gopi
  resume <task-id>` answer from the filesystem alone, independent of the row view.
  The row itself is pinned through BOTH channels the two sides use: `GOPI_HOME` +
  `GOPI_MEMORY_DB` (the sandbox profile) for the Go-side writer, and the child's
  `GOPI_HOME` for the reader — `cliContractEnv` drops every `GOPI_*` variable and
  re-adds only `GOPI_HOME`, so a row written anywhere but the sandbox profile is
  invisible to the very invocation under test.

One fixture, two addresses: the flat raw log and the session-directory one hold
the SAME bytes, because that is what a re-homed checkpoint is. In the
substitution, the session-directory example is matched WHOLE and BEFORE the bare
example session id (its match starts at the `~`, earlier in the line), so the
advertised path survives intact; cutting it at the raw-log basename alone would
leave the example session id and the `....` elision in argv.

Two guard clauses follow from this: the "cannot exist offline" refusal class is
keyed on the EXACT advertised path (`~/.gopi/raw/` + the example basename), never
on a `~/.gopi/raw/` prefix that also swallows forms which now resolve; and the
task-id / session-directory forms are asserted as RESOLVING (the checkpoint
loads, then the agent builder stops on a missing provider credential), which is
the same assertion the two resolvable raw-log examples already carried.

## The inventory

Every row is **covered**: `TestG4EveryAdvertisedSurfaceRuns` builds that row's
argv from the row's OWN invocation (placeholders substituted for the fixture's
real session id / raw log / directory / round), runs the real binary, and
asserts the observable effect plus the exit code. There are no "not covered"
rows, and no row is proven by `-h`.

Legend — **Coverage**:
- `basic (G4)` — the invocation above ran end to end through the real binary.
- **the `covered` column is `yes` for every row**; the reason column records
  WHAT the assertion is, so a reader can tell a banner assertion from a content
  assertion.

| # | verb | section | advertised invocation | covered | test / reason |
|---|------|---------|----------------------|---------|---------------|
| 1 | `log` | Usage | `gopi session log <session-id>` | yes | `TestG4EveryAdvertisedSurfaceRuns` → `gopi_log_session_id`: the session view names the fixture session it resolved |
| 2 | `log` | Usage | `gopi session log <session-id> <round>` | yes | `TestG4EveryAdvertisedSurfaceRuns` → `log_Usage_gopi_log_session-id_round`: one ROUND of the session, named by the session's own 1-based round number (the round header names the round it read) |
| 3 | `log` | Usage | `gopi session log <session-id> <round> --request` | yes | `TestG4EveryAdvertisedSurfaceRuns` → `log_Usage_gopi_log_session-id_round_--request`: that round's request body VERBATIM (the pairing with the raw-log ordinal is named on stderr) |
| 4 | `log` | Usage | `gopi session log <session-id> <round> --response` | yes | `TestG4EveryAdvertisedSurfaceRuns` → `log_Usage_gopi_log_session-id_round_--response`: that round's response side as logged |
| 5 | `log` | Usage | `gopi session log <session-id> <round> --thinking` | yes | `TestG4EveryAdvertisedSurfaceRuns` → `log_Usage_gopi_log_session-id_round_--thinking`: the round header (this fixture round logged no thinking) |
| 6 | `log` | Usage | `gopi session log <session-id> <round> --filter TOOL` | yes | `TestG4EveryAdvertisedSurfaceRuns` → `log_Usage_gopi_log_session-id_round_--filter_TOOL`: the round header, with the filter applied to that round's calls |
| 7 | `log` | Usage | `gopi session log <session-id> --turns` | yes | `TestG4EveryAdvertisedSurfaceRuns` → `log_Usage_gopi_session_log_session-id_--turns`: the TURN table — one OPERATOR row to the next, with the number of ROUNDS that turn took (the neighbouring quantity, named for itself) |
| 8 | `log` | Usage | `gopi session log <session-id> --toolcall N` | yes | `TestG4EveryAdvertisedSurfaceRuns` → `log_Usage_gopi_session_log_session-id_--toolcall_N`: the Nth TOOL CALL in full, naming the ROUND it ran in (`--toolcall` counts calls, never rounds — this fixture session holds 8 calls against 9 rounds) |
| 9 | `log` | Usage | `gopi session log <session-id> --response` | yes | `TestG4EveryAdvertisedSurfaceRuns` → `gopi_log_session_id_response`: the assistant responses, per iteration |
| 10 | `log` | Usage | `gopi session log <session-id> --thinking` | yes | `TestG4EveryAdvertisedSurfaceRuns` → `gopi_log_session_id_thinking`: the thinking blocks, per iteration |
| 11 | `log` | Usage | `gopi session log <session-id> --request` | yes | `TestG4EveryAdvertisedSurfaceRuns` → `gopi_log_session_id_request`: the system prompt the session actually sent |
| 12 | `log` | Usage | `gopi session log <session-id> --request --json` | yes | `TestG4EveryAdvertisedSurfaceRuns` → `gopi_log_session_id_request_json`: the full first request body as JSON |
| 13 | `log` | Usage | `gopi session log <session-id> --chat` | yes | `TestG4EveryAdvertisedSurfaceRuns` → `gopi_log_session_id_chat`: the conversation view |
| 14 | `log` | Usage | `gopi session log <session-id> --todo` | yes | `TestG4EveryAdvertisedSurfaceRuns` → `gopi_log_session_id_todo`: the task-area view |
| 15 | `log` | Usage | `gopi session log <session-id> --memory` | yes | `TestG4EveryAdvertisedSurfaceRuns` → `gopi_log_session_id_memory`: the fixture called no tool of this family — the empty result is the correct one (still a real invocation) |
| 16 | `log` | Usage | `gopi session log <session-id> --context-trim` | yes | `TestG4EveryAdvertisedSurfaceRuns` → `gopi_log_session_id_context_trim`: the context-trim view renders the session header |
| 17 | `log` | Usage | `gopi session log <session-id> --search` | yes | `TestG4EveryAdvertisedSurfaceRuns` → `gopi_log_session_id_search`: the fixture called no tool of this family — the empty result is the correct one (still a real invocation) |
| 18 | `log` | Usage | `gopi session log <session-id> --web` | yes | `TestG4EveryAdvertisedSurfaceRuns` → `gopi_log_session_id_web`: the fixture called no tool of this family — the empty result is the correct one (still a real invocation) |
| 19 | `log` | Usage | `gopi session log <session-id> --job` | yes | `TestG4EveryAdvertisedSurfaceRuns` → `gopi_log_session_id_job`: the fixture called no tool of this family — the empty result is the correct one (still a real invocation) |
| 20 | `log` | Usage | `gopi session log <session-id> --context` | yes | `TestG4EveryAdvertisedSurfaceRuns` → `gopi_log_session_id_context`: the fixture called no tool of this family — the empty result is the correct one (still a real invocation) |
| 21 | `log` | Usage | `gopi session log <session-id> --vision` | yes | `TestG4EveryAdvertisedSurfaceRuns` → `gopi_log_session_id_vision`: the fixture called no tool of this family — the empty result is the correct one (still a real invocation) |
| 22 | `log` | Usage | `gopi session log <session-id> --text2img` | yes | `TestG4EveryAdvertisedSurfaceRuns` → `gopi_log_session_id_text2img`: the fixture called no tool of this family — the empty result is the correct one (still a real invocation) |
| 23 | `log` | Usage | `gopi session log <session-id> --git` | yes | `TestG4EveryAdvertisedSurfaceRuns` → `gopi_log_session_id_git`: the fixture called no tool of this family — the empty result is the correct one (still a real invocation) |
| 24 | `log` | Usage | `gopi session log <session-id> --filter TOOL` | yes | `TestG4EveryAdvertisedSurfaceRuns` → `gopi_log_session_id_filter_TOOL`: the empty result the help documents for a tool the fixture never called (the view still prints its filter header) |
| 25 | `log` | Usage | `gopi session log <session-id> --grep TEXT` | yes | `TestG4EveryAdvertisedSurfaceRuns` → `gopi_log_session_id_grep_TEXT`: content match against the fixture's own text (the view echoes `Grep: …`) |
| 26 | `log` | Usage | `gopi session log <session-id> --json` | yes | `TestG4EveryAdvertisedSurfaceRuns` → `gopi_log_session_id_json`: the JSON view names the session |
| 27 | `log` | Usage | `gopi session log <session-id> --text` | yes | `TestG4EveryAdvertisedSurfaceRuns` → `gopi_log_session_id_text`: the session view names the fixture session it resolved |
| 28 | `log` | Usage | `gopi session log --last` | yes | `TestG4EveryAdvertisedSurfaceRuns` → `gopi_log_last`: cross-session selector (newest N / every session) |
| 29 | `log` | Usage | `gopi session log --last N` | yes | `TestG4EveryAdvertisedSurfaceRuns` → `gopi_log_last_N`: cross-session selector (newest N / every session) |
| 30 | `log` | Usage | `gopi session log --last N --memory` | yes | `TestG4EveryAdvertisedSurfaceRuns` → `gopi_log_last_N_memory`: cross-session selector (newest N / every session) |
| 31 | `log` | Usage | `gopi session log --branch BRANCH` | yes | `TestG4EveryAdvertisedSurfaceRuns`: the advertised invocation runs against the fixture and prints its observed effect |
| 32 | `log` | Usage | `gopi session log --all --summary` | yes | `TestG4EveryAdvertisedSurfaceRuns` → `gopi_log_all_summary`: cross-session selector (newest N / every session) |
| 33 | `log` | combines with them exactly as it does with the id | `gopi session log --branch BRANCH --response` | yes | `TestG4EveryAdvertisedSurfaceRuns`: the advertised invocation runs against the fixture and prints its observed effect |
| 34 | `log` | combines with them exactly as it does with the id | `gopi session log --worktree WT --todo` | yes | `TestG4EveryAdvertisedSurfaceRuns`: the advertised invocation runs against the fixture and prints its observed effect |
| 35 | `log` | Flags can be combined | `gopi session log <id> --response --text` | yes | `TestG4EveryAdvertisedSurfaceRuns` → `gopi_log_id_response_text`: plain-text responses only (no banner) |
| 36 | `log` | Flags can be combined | `gopi session log <id> --thinking --text` | yes | `TestG4EveryAdvertisedSurfaceRuns` → `gopi_log_id_thinking_text`: plain-text thinking only (no banner) |
| 37 | `log` | Flags can be combined | `gopi session log <id> --request --text` | yes | `TestG4EveryAdvertisedSurfaceRuns` → `gopi_log_id_request_text`: plain-text request only (no banner) |
| 38 | `log` | Flags can be combined | `gopi session log <id> --request --json` | yes | `TestG4EveryAdvertisedSurfaceRuns` → `gopi_log_id_request_json`: the full first request body as JSON |
| 39 | `log` | Flags can be combined | `gopi session log <id> --memory --text` | yes | `TestG4EveryAdvertisedSurfaceRuns` → `gopi_log_id_memory_text`: plain-text memory view only (no banner) |
| 40 | `log` | Flags can be combined | `gopi session log <id> --context-trim` | yes | `TestG4EveryAdvertisedSurfaceRuns` → `gopi_log_id_context_trim`: the context-trim view renders the session header |
| 41 | `log` | Flags can be combined | `gopi session log <id> --filter bash` | yes | `TestG4EveryAdvertisedSurfaceRuns` → `gopi_log_id_filter_bash`: the empty result the help documents for a tool the fixture never called (the view still prints its filter header) |
| 42 | `log` | Flags can be combined | `gopi session log <id> --filter read --filter write` | yes | `TestG4EveryAdvertisedSurfaceRuns` → `gopi_log_id_filter_read_filter_write`: repeatable `--filter` — every occurrence is honoured |
| 43 | `log` | Flags can be combined | `gopi session log <id> --search` | yes | `TestG4EveryAdvertisedSurfaceRuns` → `gopi_log_id_search`: the fixture called no tool of this family — the empty result is the correct one (still a real invocation) |
| 44 | `log` | Flags can be combined | `gopi session log <id> --web` | yes | `TestG4EveryAdvertisedSurfaceRuns` → `gopi_log_id_web`: the fixture called no tool of this family — the empty result is the correct one (still a real invocation) |
| 45 | `log` | Flags can be combined | `gopi session log <id> --vision` | yes | `TestG4EveryAdvertisedSurfaceRuns` → `gopi_log_id_vision`: the fixture called no tool of this family — the empty result is the correct one (still a real invocation) |
| 46 | `log` | Flags can be combined | `gopi session log <id> --text2img` | yes | `TestG4EveryAdvertisedSurfaceRuns` → `gopi_log_id_text2img`: the fixture called no tool of this family — the empty result is the correct one (still a real invocation) |
| 47 | `log` | Flags can be combined | `gopi session log <id> --git` | yes | `TestG4EveryAdvertisedSurfaceRuns` → `gopi_log_id_git`: the fixture called no tool of this family — the empty result is the correct one (still a real invocation) |
| 48 | `log` | Flags can be combined | `gopi session log <id> --grep timeout` | yes | `TestG4EveryAdvertisedSurfaceRuns` → `gopi_log_id_grep_timeout`: content match against the fixture's own text (the view echoes `Grep: …`) |
| 49 | `log` | Flags can be combined | `gopi session log <id> --grep timeout --job` | yes | `TestG4EveryAdvertisedSurfaceRuns` → `gopi_log_id_grep_timeout_job`: content match against the fixture's own text (the view echoes `Grep: …`) |
| 50 | `log` | Flags can be combined | `gopi session log --last --chat` | yes | `TestG4EveryAdvertisedSurfaceRuns` → `gopi_log_last_chat`: the conversation view |
| 51 | `stats` | Usage | `gopi session stats <session-id>` | yes | `TestG4EveryAdvertisedSurfaceRuns` → `gopi_stats_session_id`: the per-session numbers block |
| 52 | `stats` | Usage | `gopi session stats <session-id> --rounds` | yes | `TestG4EveryAdvertisedSurfaceRuns` → `gopi_stats_session_id_rounds`: the per-session numbers block |
| 53 | `stats` | Usage | `gopi session stats <session-id> --tools` | yes | `TestG4EveryAdvertisedSurfaceRuns` → `gopi_stats_session_id_tools`: the per-session numbers block |
| 54 | `stats` | Usage | `gopi session stats <session-id> --bytes` | yes | `TestG4EveryAdvertisedSurfaceRuns` → `gopi_stats_session_id_bytes`: the per-session numbers block |
| 55 | `stats` | Usage | `gopi session stats <session-id> --cost` | yes | `TestG4EveryAdvertisedSurfaceRuns` → `gopi_stats_session_id_cost`: the per-session numbers block |
| 56 | `stats` | Usage | `gopi session stats <session-id> --bill` | yes | `TestG4EveryAdvertisedSurfaceRuns` → `gopi_stats_session_id_bill`: the per-session numbers block |
| 57 | `stats` | Usage | `gopi session stats <session-id> --json` | yes | `TestG4EveryAdvertisedSurfaceRuns` → `gopi_stats_session_id_json`: the JSON view names the session |
| 58 | `stats` | Usage | `gopi session stats --branch BRANCH` | yes | `TestG4EveryAdvertisedSurfaceRuns`: the advertised invocation runs against the fixture and prints its observed effect |
| 59 | `stats` | Usage | `gopi session stats --last [N]` | yes | `TestG4EveryAdvertisedSurfaceRuns` → `gopi_stats_last_N`: cross-session selector (newest N / every session) |
| 60 | `stats` | Usage | `gopi session stats --all` | yes | `TestG4EveryAdvertisedSurfaceRuns` → `gopi_stats_all`: cross-session selector (newest N / every session) |
| 61 | `stats` | Flags can be combined | `gopi session stats <id> --rounds --json` | yes | `TestG4EveryAdvertisedSurfaceRuns` → `gopi_stats_id_rounds_json`: the JSON view names the session |
| 62 | `stats` | Flags can be combined | `gopi session stats <id> --rounds --bytes` | yes | `TestG4EveryAdvertisedSurfaceRuns` → `gopi_stats_id_rounds_bytes`: the per-session numbers block |
| 63 | `stats` | Flags can be combined | `gopi session stats <id> --cost --rounds` | yes | `TestG4EveryAdvertisedSurfaceRuns` → `gopi_stats_id_cost_rounds`: the per-session numbers block |
| 64 | `stats` | Flags can be combined | `gopi session stats --last 5 --tools` | yes | `TestG4EveryAdvertisedSurfaceRuns` → `gopi_stats_last_5_tools`: cross-session selector (newest N / every session) |
| 65 | `stats` | Flags can be combined | `gopi session stats --all --tools` | yes | `TestG4EveryAdvertisedSurfaceRuns` → `gopi_stats_all_tools`: cross-session selector (newest N / every session) |
| 66 | `stats` | Flags can be combined | `gopi session stats <id> --json` | yes | `TestG4EveryAdvertisedSurfaceRuns` → `gopi_stats_id_json`: the JSON view names the session |
| 67 | `raw` | Usage | `gopi session raw <session-id>` | yes | `TestG4EveryAdvertisedSurfaceRuns` → `gopi_raw_session_id`: the verbatim bytes, starting at the request marker |
| 68 | `raw` | Usage | `gopi session raw <session-id> <round>` | yes | `TestG4EveryAdvertisedSurfaceRuns` → `gopi_raw_session_id_round`: the verbatim bytes, starting at the request marker |
| 69 | `raw` | Usage | `gopi session raw <session-id> <round> request` | yes | `TestG4EveryAdvertisedSurfaceRuns` → `gopi_raw_session_id_round_request`: the verbatim request body |
| 70 | `raw` | Usage | `gopi session raw <session-id> <round> response` | yes | `TestG4EveryAdvertisedSurfaceRuns` → `gopi_raw_session_id_round_response`: the paired response side of round 1 |
| 71 | `raw` | Usage | `gopi session raw <session-id> --rounds` | yes | `TestG4EveryAdvertisedSurfaceRuns` → `gopi_raw_session_id_rounds`: the round index |
| 72 | `raw` | Usage | `gopi session raw <session-id> --lines A-B` | yes | `TestG4EveryAdvertisedSurfaceRuns` → `gopi_raw_session_id_lines_A_B`: the verbatim bytes, starting at the request marker |
| 73 | `raw` | Usage | `gopi session raw <session-id> --sysprompt` | yes | `TestG4EveryAdvertisedSurfaceRuns` → `gopi_raw_session_id_sysprompt`: the system prompt the session actually sent |
| 74 | `raw` | Usage | `gopi session raw <session-id> --userprompt` | yes | `TestG4EveryAdvertisedSurfaceRuns` → `gopi_raw_session_id_userprompt`: the first user prompt the session actually sent |
| 75 | `raw` | Usage | `gopi session raw --file PATH` | yes | `TestG4EveryAdvertisedSurfaceRuns` → `gopi_raw_file_PATH`: the verbatim bytes, starting at the request marker |
| 76 | `raw` | Usage | `gopi session raw --branch BRANCH` | yes | `TestG4EveryAdvertisedSurfaceRuns`: the advertised invocation runs against the fixture and prints its observed effect |
| 77 | `raw` | Usage | `gopi session raw --last [N]` | yes | `TestG4EveryAdvertisedSurfaceRuns` → `gopi_raw_last_N`: cross-session selector (newest N / every session) |
| 78 | `checkpoint` | Usage | `gopi session checkpoint list [--dir DIR]` | yes | `TestG4EveryAdvertisedSurfaceRuns` → `gopi_checkpoint_list_dir_DIR`: the raw-log listing header |
| 79 | `checkpoint` | Usage | `gopi session checkpoint export <log> [--dir DIR]` | yes | `TestG4EveryAdvertisedSurfaceRuns` → `gopi_checkpoint_export_log_dir_DIR`: the exported checkpoint JSON body |
| 80 | `resume` | Examples | `gopi session resume 01a096c9-6f7d-7e43-8e46-9ecca58e9cd0` | yes | `TestG4EveryAdvertisedSurfaceRuns` → `gopi_resume_01a096c9_6f7d_7e43_8e46_9ecca58e9cd0`: the advertised target resolves and the CHECKPOINT LOADS; the continuation round needs a provider credential, deliberately not mocked (Rule 31's honest limit) |
| 81 | `resume` | Examples | `gopi session resume ~/.gopi/raw/gopi-raw-20260826-120000.log` | yes | `TestG4EveryAdvertisedSurfaceRuns` → `gopi_resume_~_-gopi_raw_gopi-raw-20260826-120000-log`: the advertised target resolves and the CHECKPOINT LOADS; the continuation round needs a provider credential, deliberately not mocked (Rule 31's honest limit) |
| 82 | `resume` | Examples | `gopi session resume ~/.gopi/sessions/01a096c9-6f7d-7e43-8e46-9ecca58e9cd0/....raw.log` | yes | `TestG4EveryAdvertisedSurfaceRuns` → `gopi_resume_~_-gopi_sessions_01a096c9-6f7d-7e43-8e46-9ecca58e9cd0_----raw-log`: the re-homed session-DIRECTORY checkpoint the fixture installs resolves and the CHECKPOINT LOADS; the continuation round needs a provider credential, deliberately not mocked (Rule 31's honest limit) |
| 83 | `resume` | Examples | `gopi session resume 4c9a1f3e-8b2d-4a10-9e6f-2c5d7a0b1e33` | yes | `TestG4EveryAdvertisedSurfaceRuns` → `gopi_resume_4c9a1f3e_8b2d_4a10_9e6f_2c5d7a0b1e33`: the advertised target resolves and the CHECKPOINT LOADS; the continuation round needs a provider credential, deliberately not mocked (Rule 31's honest limit) |
| 84 | `resume` | Examples | `gopi session resume ~/.gopi/raw/gopi-raw-20260826-120000.log next-step.md` | yes | `TestG4EveryAdvertisedSurfaceRuns` → `gopi_resume_gopi_raw_gopi_raw_20260826_120000_log_next_step_md`: the advertised target resolves and the CHECKPOINT LOADS; the continuation round needs a provider credential, deliberately not mocked (Rule 31's honest limit) |
| 85 | `resume` | Examples | `gopi session resume t-1788048877` | yes | `TestG4EveryAdvertisedSurfaceRuns` → `gopi_resume_t-1788048877`: the task-id form resolves through the run directory the fixture seeds beside the ledger row (rundir.DirFor + rundir.RunDirRawLog) and the CHECKPOINT LOADS; the continuation round needs a provider credential, deliberately not mocked (Rule 31's honest limit) |
| 86 | `resume` | Examples | `gopi session resume t-1788048877 --task t-1788048877` | yes | `TestG4EveryAdvertisedSurfaceRuns` → `gopi_resume_t-1788048877_--task_t-1788048877`: a task-id target with the explicit --task id says the same thing, and the CHECKPOINT LOADS; the continuation round needs a provider credential, deliberately not mocked (Rule 31's honest limit) |
| 87 | `resume` | Examples | `gopi session resume ~/.gopi/raw/gopi-raw-20260826-120000.log --task t-1788048877` | yes | `TestG4EveryAdvertisedSurfaceRuns`: the raw-log form with --task (the fixture's own log stands in for the other machine's path) |
