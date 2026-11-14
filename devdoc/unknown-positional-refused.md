<!-- doc-class: historical -->
# An unknown top-level word is refused — never spent as a run's prompt

Task `01a0f72b-d341-7f06-a7a3-94d72a39c9ce` (2026-10-01), branch
`fix/unknown-verb-refused`.

## The defect

`gopi revision` (also `status`, `branch list`, `scratch dir`, `settings show`)
is not a dispatched verb. The dispatch chain in `cmd/agent/main.go` reads the
verb from the **first argument only** and, finding none of its own, falls
through to the run path, where the leftover positional becomes the task
**prompt**: `gopi revision` started the interactive console, minted a session
directory and registered an `[auto] revision` row in the ledger.

Measured against the master binary in a private sandbox
(`GOPI_HOME=<sandbox> /usr/local/bin/gopi revision </dev/null`):

```
sessions: 5e5fa0c4-…, c75dc450-…     (two sessions minted)
ledger:   1|in_progress|server|[auto] revision
          2|failed|server|[auto] revision
```

The cost is a whole agent run — tokens, a live ledger row, a worktree-shaped
session — for a word the operator meant as a **query about the repository**.

`buildinfo` already had the right shape (the "PositionalCommandVerb" guard,
class (c) of `config.PositionalVerbs`). The gap: that guard answers a word it
**recognises**, so it cannot answer a word the operator *believed* was a
command.

## The fix — a second, CLOSED vocabulary of near-misses

`internal/config/cli_verb_guard.go` gains `commandHintWords`: a closed table of
the words an operator reaches for when the spelling they want does not exist,
each with the real command that answers the request (`""` when the binary has
none — an honest "there is no such command" beats an invented hint):

| word | address |
|---|---|
| `revision` | `version` (the command whose report prints `revision:`) |
| `status` | *(none — the honest clause)* |
| `branch` | *(none — the family lives elsewhere, `gopi task for-branch`)* |
| `settings` | *(none — the real family is `gopi config`)* |
| `scratch` | *(none — the real family is `gopi scratch-gc`)* |

`config.PositionalCommandHintRefusal(cfg)` returns the refusal **text** (never
printing, never exiting — the exit stays `main`'s), or `""` when the invocation
must run as ordinary prompt text. `cmd/agent/main.go` calls it immediately after
the existing `PositionalCommandVerb` guard and exits 1 with the message.

The shapes it answers are spelled by **arity**, not by guessing:

```
gopi revision                  ONE positional → refused
gopi branch list               first of TWO is a near-miss → refused
gopi --timeout 3 revision      near-miss under a run flag → refused
gopi revision -h               the help scan below matches any -h → exit 0, top-level page
gopi branch off the failed…    THREE words, a sentence → runs as prompt text
gopi frobnicate                unknown word, no vocabulary → runs as prompt text
--prompt revision / GOPI_PROMPT  the deliberate escape → runs
```

### Why a closed table and not a heuristic

A shape heuristic ("unknown word → refuse") would refuse `gopi summarize the
docs`. The table grows only on measured evidence, and the vocabulary is pinned
against the binary's real dispatch surface by
`cmd/agent/cli_verb_hint_guard_test.go`.

### Why `-h` had to stay legal

`gopi revision -h` is a legal top-level help query (the help scan in `main.go`
matches any `-h` and exits 0 with the usage page). A guard placed one line too
early turns that query into a refusal. The call sits **before** the help scan,
so the ordering is pinned by a test rather than by reading the diff:
`TestNearMissHelpQueryStaysLegal` runs the real binary.

## Rule-31 evidence (real invocations, private sandbox)

`sandbox-unkverb-final-5bbdba54`, five refusals, ledger untouched:

```
LEDGER_BYTES_BEFORE=0   ROWS_BEFORE=(no table tasks)   SESSIONS_BEFORE=0
gopi revision      => exit=1 | gopi: "revision" is not a gopi command — … `--prompt revision`. `gopi version -h` …
gopi status        => exit=1 | … there is no command spelled "status".
gopi branch list   => exit=1 | … there is no command spelled "branch".
gopi scratch dir   => exit=1 | … there is no command spelled "scratch".
gopi settings show => exit=1 | … there is no command spelled "settings".
LEDGER_BYTES_AFTER=0    ROWS_AFTER=(no table tasks)    SESSIONS_AFTER=0
```

The sandbox's ledger is a real private file (`0600`), created empty by the
initializer; no table was ever created because **no run path was reached** —
which is the point (a probe that reached the run path would have created the
schema, as the master-binary control above did).

## Red proof

The guard's condition in `cmd/agent/main.go` was inverted
(`false && hint != ""`), the binary rebuilt, and the new test re-run:

```
--- FAIL: TestUnknownNearMissIsRefusedByNameNoRun (4.91s)
    --- FAIL: …/revision …/status …/branch_list …/settings_show …/scratch_dir …/--timeout_3_revision
FAIL
```

Restored, the same test passes. Recorded in the report's `## Evidence`.

## Not done / honest gaps

- `gopi status` and `gopi branch`/`settings`/`scratch` point at the top-level
  page only: this binary has **no single command** that answers those requests,
  and naming one would be a guess. `merger --status`, `config show`,
  `task for-branch` and `scratch-gc` are the real surfaces; a later task with a
  measured invocation may add them as addresses.
- The guard is argv-position- and arity-based. A near-miss word that opens a
  three-word sentence still runs as prompt text — deliberate (that is what
  keeps `gopi branch off the failed attempt` a prompt), but it means
  `gopi status report for today` is refused while `gopi status of the merge
  queue` is refused by the *other* guard (the word `merge` is a dispatched
  verb). Two guards, one surface; both are pinned by tests.
