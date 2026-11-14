<!-- doc-class: historical -->
# The resident memory-sync tick is RETIRED (2026-10-25)

> **READING NOTE (added 2026-11-06, the record below is untouched).** This page
> records the day the TICK was retired; what it says about the tick is still
> exactly true. What has moved since is the DOOR. `5677cc6e` (2026-11-05) deleted
> the whole cross-node sync CLI family, so for one day the commands quoted below
> did not exist at all; `gopi memory sync` was then restored (2026-11-06) as a
> MANUAL door — one invocation = one session, exit. **The `--every <dur>` loop
> quoted below did NOT come back**: it was retired for the same reason the tick
> was (a cadence nobody watches), and its reader `ResolveMemorySyncEvery` is
> gone, so that spelling is refused rather than honoured. Read every
> `--every 5m` below as a record of 2026-10-25, never as a command to paste.
> The restored shape is `devdoc/memory-sync-manual-door-restored.md`.

**Task**: `01a17319-7d77-7987-b98b-725f65c9473d` · **Branch**: `master` (clone ground)

The companion page for the mechanism as it was is
[`memory-sync-tick.md`](memory-sync-tick.md) — it is `historical` and describes a
tick this repository no longer has. Read it only for the reasoning that produced
it; nothing in it runs.

## The measurement that ended it

A worker round that fired the tick spawned one `gopi memory sync --peer P --push`
child per declared peer and **blocked until that child exited**. Measured in
`~/.gopi/logs/worker.log`: **315–534 seconds** — five to nine minutes — per
firing, every 30 minutes the worker stayed up.

The process whose whole job is to keep claiming tasks stopped claiming them, to
push another component's knowledge base. That is the same defect the resident
merge tick was retired for on 2026-09-27 (`worker-merge-tick-retired.md`): the
tick "was not misbehaving — its EXISTENCE was the bug". The 30-minute cadence was
also chosen when the asset was assumed to need unattended convergence; the cost it
paid was a claim loop that went blind for a fifth of every half hour.

## What still exists, unchanged

> **Correction, added 2026-10-27 (this page's body is unchanged: it is a record of
> 2026-10-25).** Two rows of the table below are no longer true. The store-triggered
> round was DELETED — not disabled — in `5e3ce2e`: the trigger file
> (`internal/tools/memory_store_trigger.go`) and
> `internal/tools/memory_sync_child_cwd.go` are gone, so a successful `memory_store`
> **no longer carries anything across** and there is no second seam left to move a
> child out of a ground. Read the two `unchanged` rows as the state on 2026-10-25.
> Current state: `devdoc/memory-store-sync-decouple.md`. The three rows above them
> (the manual verb, its `--every` foreground loop, the peer's `--serve-assets`
> answer) are still exactly as written.

| surface | state |
|---|---|
| `gopi memory sync --peer P --push` | **a first-class command**, unchanged. Run it by hand. |
| `gopi memory sync --peer P --push --every 5m` | unchanged — the foreground loop an operator watches. |
| the peer's `--serve-assets` answer | unchanged; nothing resident is required on the peer. |
| the store-triggered round (`internal/tools/memory_store_trigger.go`) | unchanged — a successful `memory_store` still carries *that* lesson across asynchronously. |
| `internal/tools/memory_sync_child_cwd.go` | unchanged, and still needed: the store-triggered round is the seam that uses it. |

## What is gone

Deleted in the same commit, so nothing a future reader finds advertises a cadence
nothing honours:

- `cmd/agent/memory_sync_tick.go` (439 lines) and its two test files
  (`memory_sync_tick_test.go`, `memory_sync_tick_cwd_test.go`);
- the worker round's ③ call site in `cmd/agent/cli_tasks_worker.go`;
- `config.PublishMemorySyncTickMin`, the `Config.MemorySyncTickMin` settings
  field, and the `EnvVars` catalogue row (`internal/config/settings.go`) — so
  `gopi --help-env` no longer offers the knob;
- the `CONFIG.md` row (and its `#### Resident memory-sync tick` section, rewritten
  to describe the store trigger that still runs) — the flag-table inventory in
  `internal/standards/config_doc_flag_gate_test.go` drops 74 → 73 with it;
- the `internal/tools` allow-map entry that classified a deleted test's claim.

`config.MemorySyncTickIntervalEnvName` / `MemorySyncTickSettingsKey` were **not**
deleted: they were renamed to `MemorySyncTickRetiredEnvName` /
`MemorySyncTickRetiredSettingsKey` and kept as *nameable* spellings, because the
settings decoder tolerates an undeclared key silently and the retirement report is
the only thing that can tell an operator the key is dead (see the Rule 15 section).

Two symbols that had a consumer **outside** the mechanism were dealt with by
Rule 43 rather than by deletion:

- **`config.MemorySyncTickIntervalEnvName` was referenced in production outside
  the tick** — `internal/tools/memory_store_trigger.go` used it as the *name* in
  its round-cap report ("stepping aside for the resident tick"). That report was
  rewritten to name the command the operator should run instead; it was naming a
  tick that no longer exists.
- **`internal/tools/memory_sync_child_cwd.go` stays where it is.** It was written
  for two seams; seam ① (the tick) is gone, seam ② (the store-triggered round)
  is live and lives in the same package. Deleting it with the tick would have
  taken the live seam's decision with it — which is exactly the failure Rule 43
  is written against.

> **Correction, added 2026-10-27 (the bullet above is unchanged: it is the state on
> 2026-10-25).** The second bullet is now false. `internal/tools/memory_sync_child_cwd.go`
> was NOT kept: it was deleted, 550 lines, in `5e3ce2e` (2026-10-27), and
> `DecideMemorySyncChildCwd` — its only export — has zero definitions at HEAD. The
> seam that justified keeping it (②, the store-triggered round) was deleted in the
> same commit, so there was no live consumer left to keep the decision for. Rule 43's
> judgement was correct for the commit it was made in; it stopped being correct one
> commit later, when the other seam went. Current state:
> `devdoc/memory-store-sync-decouple.md`.

## The retired knob still SPEAKS (Rule 15)

The settings decoder uses a plain `json.Unmarshal` (no `DisallowUnknownFields`), so
a settings file still carrying `"memory_sync_tick": 30` loads with every other knob
intact and **silently ignores** that key. Silence is the defect, so:

```
$ GOPI_MEMORY_SYNC_TICK_MIN=30 gopi task list >/dev/null
[worker] GOPI_MEMORY_SYNC_TICK_MIN is RETIRED: the worker no longer runs a
memory-sync pass: ... was deleted 2026-10-25 — each fired run held a worker round
for 315-534s measured in ~/.gopi/logs/worker.log, and unattended convergence is
now the operator's own decision (see devdoc/memory-sync-tick-retired.md)
  run `gopi memory sync --peer P --push` by hand — no tick in this process syncs for you
```

Both faces are covered (env and the settings key), the report fires at the same
pre-chain moment the retired merge-tick report uses
(`cmd/agent/memory_sync_tick_retired.go`, called from `tmpDirBootstrapFromArgs`),
and it **reports rather than refuses** — a stale cadence in an old file is not a
reason to take an operator's worker down.

The two spellings survive as `config.MemorySyncTickRetiredEnvName` /
`config.MemorySyncTickRetiredSettingsKey` and carry an `env-reader-exempt`
justification: a retirement must be able to *name* the knob it refuses.

## Unsynced knowledge bases diverge — by decision, not by accident

**This is the consequence, stated plainly so nobody later reads it as a bug.**
After this change, nothing in gopi periodically reconciles two hosts' knowledge
bases. A host whose `memory_store`s are quiet **diverges** from its peers until a
human syncs it:

```bash
gopi memory sync --peer peer1 --push     # one shot, seconds on a converged pair
gopi memory sync --peer peer1 --push --every 5m   # the same, on a loop an operator watches
```

The remaining automatic leg is per-store: a `memory_store` carries *that one
lesson* across asynchronously (bounded to 8 rounds per process). It is a freshness
trigger, not a driver.

> **Correction, added 2026-10-27.** This paragraph is false as of `5e3ce2e`
> (2026-10-27): there is **no** remaining automatic leg. The per-store trigger was
> deleted with `internal/tools/memory_store_trigger.go`, so a successful
> `memory_store` carries nothing across and the sentence above describes the state
> between 2026-10-25 and 2026-10-27 only. The paragraph's conclusion — "no
> replacement automation was invented" — is the part that still holds, and it holds
> more strongly now. Current state: `devdoc/memory-store-sync-decouple.md`.

**No replacement automation was invented** (Rule 35): no cron line, no systemd
unit, no daemon, no script, and the tick was **not** moved into `serve`, the
merger or any other resident. An operator who wants convergence asks for it by
name; that is the whole mechanism, and it is the operator's explicit decision
(2026-10-25).

## How this is guarded

Two files, in the shapes their merge-tick counterparts already established:

`cmd/agent/memory_sync_tick_retired_test.go`:

1. `TestWorkerLoopDoesNotSync` — the loop's own source contains none of
   `memorySyncTickSweep` / `memorySyncTickSend` / `memorySyncTickLastRun` /
   `GOPI_MEMORY_SYNC_TICK_MIN`. **Red on the pre-change tree**, where the call site
   was present.
2. `TestMemorySyncTickPlumbingIsGone` — the tick file and both test files do not
   exist, and neither `cli_tasks_worker.go` nor `tmpdir_inject.go` calls the
   publisher. A half-removed knob fails here.
3. `TestRetiredMemorySyncTickKnobsSpeak` /
   `TestSettingsCarriesKeySeesTheRetiredMemorySyncTickKey` — the report names both
   faces and their replacement, stays quiet when neither is set, and the settings
   probe answers from the FILE (the decoded struct no longer has a field for it).

`internal/config/memory_sync_tick_removed_test.go` — the config package's half,
which `merge_tick_removed_test.go` already had for the merge tick:

4. `TestRetiredMemorySyncTickSettingsKeyIsTolerated` — a settings.json still
   carrying the key loads, and the knob beside it survives.
5. `TestRetiredMemorySyncTickSettingsKeyHasNoField` — no `Config` field may carry
   the `memory_sync_tick` json tag again (scanned by TAG, since the field name is
   gone).
6. `TestRetiredMemorySyncTickEnvNameHasNoReader` — inside the config package, the
   retired env spelling is NAMED and never read (Rule 21's property stated where
   the constant lives).

A future run that puts a sync back into the round fails arm 1 with the ruling
quoted, rather than merely differing from a deleted test; a run that revives the
knob fails arm 5 even if it re-adds it under the same wire name.

