<!-- doc-class: historical -->
# Task Report: `settings.json` `memory_db` — a documented channel that nothing read

**Task**: `01a0de69-ee6b-774b-99d5-50347fbaab5f` (slice 7 end-user-audit follow-up bug row)
**Date**: 2026-09-27
**Branch**: `task/01a0de69ee6b774b99d550347fbaab5f`

## Completed

### 1. The defect, measured before the fix (RED)

Two end-user-facing statements were false on the tree this task started from:

| Class | The claim | The measurement |
|---|---|---|
| **catalogue** | `settings.json` key `memory_db` is a ranked channel of the ledger path (catalogue row for `GOPI_MEMORY_DB`; CONFIG.md §2c) | **nothing read the key.** A settings file declaring `{"memory_db": "<X>"}` left `<X>` not even created, and every surface silently used the built-in default |
| **divergence** | — | one `gopi config show` printed `memory db / ledger …/memory.db (built-in default)` in its identity section and `memory_db "<X>"` in its settings.json section: one report, two ledgers, and the WRONG one named as in force |

RED evidence (binary built from the pre-fix tree, before any edit):

```
$ GOPI_HOME=$SB bin/gopi --settings $EV/red/D.json config show
  memory db / ledger     …/sandbox-slice7-env-4ec00812/.gopi/memory.db  (built-in default)
settings.json:
  memory_db  "…/red/declared/ledger.db"
$ ls …/red/declared/          → empty (the declared ledger was never created)
```

Two further measurements from the same RED probe, each closing a claim that was
NOT true on the tree (recorded so they are not re-asserted):

- **`--help-env`'s flag column already said `--db`** for `GOPI_MEMORY_DB`, and
  CONFIG.md §2c carried it too. The row did NOT hide the flag; the real gap was
  the catalog MEANING one line below it — silent about the settings channel and
  about rank, so a reader could not tell which channel wins.
- **a LEADING `--db` (`gopi --db X memory search q`) is refused by name**, not
  silently ignored: the positional-verb guard reports "`memory` is a POSITIONAL
  here, following the run flag `--db` … Put the verb first". That is correct
  behaviour — so the report surface deliberately does NOT read a leading `--db`
  as a ledger channel (doing so would name a channel this launch never reached).
  Both halves now say so: the doc row and the `--help-env` description.

### 2. ONE ranked resolution, in `internal/config` (the fix)

`config.ResolveLedger(flagPath) (path, source)` is the single ranked chain, and
`config.LedgerPath()` is the no-flag convenience wrapper over it:

```
rank 1  --db                       (the flag of the verb that owns one — handed IN by the caller)
rank 2  GOPI_MEMORY_DB
rank 3  GOPI_HIPPO_DB              (legacy alias)
rank 4  settings.json "memory_db"   ← the channel nothing read
rank 5  <gopi dir>/memory.db       (built-in default)
```

- The `source` return value names the channel that decided, so a reporter prints
  the fact instead of re-deriving the ranking — which is exactly how the two
  halves of `config show` came to disagree (Rule 2/14).
- The rank-4 reader is `ledgerPathFromSettings()`: it reads through
  `ResolveSettingsPath()` + `FromSettingsFile()` — the same loader as everything
  else — so "where are the settings" has one answer, and an absent/broken file
  falls through to the default rather than reporting a fault (the agent path
  already exits on a broken file; a report-only surface says so itself — Rule 15
  without a double print).
- New named constants (Rule 1b/13/14): `LedgerSettingsKey` (`memory_db`),
  `LedgerFlagName` (`db`), `LedgerSourceFlag` (`--db`), `LedgerSourceDefault`
  (`built-in default`), `LedgerNoFlag`. The catalogue row, the doc table, the
  report and the tests all reference them — one spelling each.

### 3. The consumers now agree (no second chain)

| File | Before | After |
|---|---|---|
| `internal/tools/memory.go` `memoryDBPath()` | walked the env half privately | delegates to `config.LedgerPath()`, keeping only the CWD-relative last resort |
| `cmd/agent/cli_config.go` `configMemoryDBPath()` | re-derived the env chain, then printed "(built-in default)" unconditionally | delegates to `config.ResolveLedger(config.LedgerNoFlag)` and prints the channel it returns |
| `cmd/agent/cli_config.go` `toolsMemoryDBEnvNames()` | existed only to feed that re-derivation | **deleted** (its one caller is gone) |
| `internal/tools/branch_attribution.go` `branchAttributionLedgerCause()` | its own `switch` over env vars | asks the resolution which channel decided, rendered via the new `ledgerChannelPhrase()` |

### 4. Tests (regression guards for every claim above)

- `internal/config/memory_ledger_resolution_test.go` — the ranked chain on the
  REAL resolver: settings key honoured + named; the full order in one table
  (default → settings → env → legacy → flag); the zero-value case (the key
  present but EMPTY falls through, Rule 15a); the no-settings-file case; the
  catalogue row naming the SAME constants the resolution reads; and the rendered
  `--help-env` row's flag column equal to `--db` (documented, not em-dashed).
- `cmd/agent/ledger_settings_value_test.go` — **the epic's acceptance, on the
  real binary**: a settings-only profile makes the run CREATE and open exactly
  the declared file and NOT the profile default; the control case (no key →
  default created, declared path untouched), which is what makes the first case
  capable of failing; and one `config show` naming ONE ledger in both its
  identity and settings.json sections.

RED-then-GREEN of the new guards, both runs observed:

```
$ GOPI_BIN=<pre-fix build> go test ./cmd/agent/ -run 'TestMemoryDBSettingsKey|TestConfigShowNamesOneLedger'
--- FAIL: TestMemoryDBSettingsKeyOpensTheDeclaredLedger
    the declared ledger …/declared/declared-ledger.db was NOT created: no such file or directory
--- FAIL: TestConfigShowNamesOneLedgerInBothSections
    the identity section names "…/.gopi/memory.db  (built-in default)", which does not carry the declared ledger
FAIL

$ go test ./cmd/agent/ -run 'TestMemoryDBSettingsKey|TestConfigShowNamesOneLedger'   (fixed tree)
ok  	github.com/nick/gopi/cmd/agent	30.294s
```

### 5. Documentation, in the same commit

- CONFIG.md §2c's `memory_db` row now states the ranked chain, the new source
  references (the resolver and the settings reader), and the leading-`--db`
  refusal. The `--db` flag row names what a leading occurrence IS (a run flag)
  rather than leaving it unexplained.
- `internal/config/settings.go`'s catalogue description for `GOPI_MEMORY_DB`
  carries the ordered text the `--help-env` row renders, so the table tells the
  operator the ORDER instead of one path.
- `internal/standards/config_doc_gate_test.go` passes (every doc row resolves to
  a real line).

## Not Done (honest)

- **The operator's own profile is untouched**: `~/.gopi/settings.json` carries no
  `memory_db` key, so rank 4 has never fired on the operator's host outside this
  task's probes. Correct-and-unexercised there is still correct, but it is not a
  claim of field use.
- **`gopi setup` was not re-run** against a settings-declared ledger: it reads
  `config.LedgerPath()`, which now consults the settings key, so it inherits the
  fix — but I did not execute that surface, so I am not claiming it (Rule 31).
- **Two `internal/agent` tests fail on this tree** —
  `TestDocAnchorsResolveInNormativeDocs` (1 new drift, 1 stale baseline permit)
  and `TestTaskPromptCarriesDirectiveAtHead`. Both fail identically on a clean
  `git archive master` checkout (`/dev/shm/slice7-master-proof`: the same two
  FAILs and the same "224 pre-existing drift entries permitted, 1 new, 1 stale
  permits"), so they are pre-existing and outside this diff — but they are red,
  and a red suite is not "green with an asterisk".
- The `internal/tools` and `cmd/agent` FULL package runs were still in flight at
  report time (jobs started at `[t+1089s]`, both past 7 minutes); the scoped runs
  recorded under Verification are the ones I observed to completion.

## Verification

- **Build**: `CGO_ENABLED=0 go build -o bin/gopi ./cmd/agent/` → exit 0; `go build ./...` → exit 0.
- **Tests**: `go test ./internal/config/ -count=1` → `ok` (3.652s) ·
  `go test ./internal/standards/ -count=1` → `ok` (67.200s) ·
  `go test ./cmd/agent/ -run 'TestMemoryDBSettingsKey|TestConfigShowNamesOneLedger' -count=1` → `ok` (30.294s) ·
  RED proof of the same two under `GOPI_BIN=<pre-fix build>` → both FAIL.
- **Ran** (verbatim, in a `gopi sandbox create` profile — Rule 34, never the operator profile):

```
$ printf '{"memory_db":"<EV>/final/declared/ledger.db"}\n' > <EV>/final/D.json
$ GOPI_HOME=<SB> bin/gopi --settings <EV>/final/D.json memory search slice7final
No distilled memory found for this query. Nothing has been distilled on this topic yet …
$ ls -la <EV>/final/declared/
-rw-r--r-- 1 nick nick 131072 Sep 27 01:43 ledger.db          ← the DECLARED file, created by the run
$ GOPI_HOME=<SB> bin/gopi --settings <EV>/final/D.json config show
  memory db / ledger     <EV>/final/declared/ledger.db  (from memory_db)
settings.json:
  memory_db  "<EV>/final/declared/ledger.db"                    ← ONE ledger in both sections, channel named
$ GOPI_HOME=<SB> bin/gopi --help-env | grep -A1 GOPI_MEMORY_DB
  GOPI_MEMORY_DB                   memory_db                   --db
    Knowledge base path (default ~/.gopi/memory.db). Ranked chain: the ledger flag of …
```

- **Result**: the settings-only run created and opened exactly the declared
  ledger, and `config show` named that one ledger as in force in BOTH sections,
  with the channel (`from memory_db`) stated. The `--help-env` row prints `--db`
  and the catalogue description carries the ranked order.
- **Commit**: see the branch tip (this report is committed with the change).

## Evidence (self-filed)

- RED, pre-fix binary, settings-only profile: the build-from-before-edits `gopi`
  under `--settings D.json memory search x` → the declared directory stayed
  EMPTY; `config show` → identity `(built-in default)` vs the settings section
  naming the declared path.
- RED, the guard itself: `GOPI_BIN=/tmp/slice7/gopi go test ./cmd/agent/ -run 'TestMemoryDBSettingsKey|TestConfigShowNamesOneLedger'`
  → `--- FAIL: TestMemoryDBSettingsKeyOpensTheDeclaredLedger … was NOT created`
  and `--- FAIL: TestConfigShowNamesOneLedgerInBothSections`; the same two tests
  → `ok` on the fixed tree (verbatim outputs in the run's `[t+837s]` and
  `[t+823s]` rounds).
- GREEN, real surfaces (the `Ran:` block above), with `<EV>` =
  `/home/user/.gopi/sessions/01a0de69-ee6b-774b-99d5-50347fbaab5f` and `<SB>` =
  the sandbox named by `gopi sandbox create --label slice7`.
- Pre-existing redness: `git archive master` → `/dev/shm/slice7-master-proof`;
  `go test ./internal/agent/ -run 'TestDocAnchorsResolveInNormativeDocs|TestTaskPromptCarriesDirectiveAtHead'`
  → the SAME two FAILs.
- Rule 30: `git status --porcelain` in this worktree lists this run's six
  modified files and two new test files (all committed at the branch tip).
