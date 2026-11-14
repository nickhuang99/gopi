<!-- doc-class: normative -->
# Memory sync, phase 2: the periodic `--every` leg

> **Retired mechanism (2026-11-05/06).** The verb this page specifies —
> `gopi memory sync --every <dur>` — no longer exists: batch 1 (`5677cc6e`,
> 2026-11-05) deleted `cmd/agent/cli_memory_sync_every.go` together with the
> whole `memory sync` verb (`./bin/gopi memory sync` now prints
> `gopi memory: unknown subcommand "sync"`), and batch 5 (2026-11-06, commit
> `d7d836e5`) deleted the cadence knob this page's §4 specifies —
> `GOPI_MEMORY_SYNC_EVERY_SEC`, its default `MemorySyncEveryDefaultSeconds`,
> its single reader `ResolveMemorySyncEvery`, its `--help-env` row and its
> CONFIG.md row. The spelling survives only in `internal/config` as
> `MemorySyncEveryRetiredEnvName` / `MemorySyncEveryRetiredReplacement`, which
> `cmd/agent/memory_sync_every_retired.go` names back at an operator whose
> environment still exports it (Rule 15). **Read §4 below as the RECORD of how
> the cadence was resolved, not as a description of live behaviour** — the
> default constant and the reader it names are both gone.
>
> **What survives today are the two MANUAL doors, and neither repeats — there is
> no periodic cross-host leg today, by design.** (1) The converging one came
> back on 2026-11-06: `gopi memory sync --peer <alias>` runs ONE unique-only
> session and exits — the very session §2 below specifies, with the `--every`
> cadence removed and everything else (`--push`/`--pull`/`--dry-run`/
> `--serve-assets`/`--timeout`) intact; see
> `devdoc/memory-sync-manual-door-restored.md`. (2) The copying one is
> `gopi memory backup`: it snapshots this ledger (SQLite's own `VACUUM INTO`),
> verifies the artifact read-only and ships it over one ssh leg to a declared
> `memory_peers` peer. It imports nothing back, so it REPLACES the peer's
> knowledge base rather than merging it.

Phase 1 (devdoc/memory-sync-timeout-and-oneway.md) made cross-node knowledge
sync a **manual** verb: `gopi memory sync --peer hp --push` runs ONE unique-only
session and reports what crossed. This is phase 2 — the same session, repeated
on a cadence from a single frontend invocation:

```
gopi memory sync --peer hp --push --every 5m
```

## 1. The shape, and why it is the merge loop's

> **Retired mechanism (merge verb removal, 2026-11-01).** The precedent named in
> this section — the `--every` loop of the top-level `gopi merge` — was removed
> with that verb: `./bin/gopi merge --every 5m` exits **1** today. The PRECEDENT
> is still the design (a sleep parameter, not a control face) and `gopi memory
> sync --every` is live and is what a reader should run.

The flag is a **sleep parameter**, not a control face. `gopi merge --every 5m`
set the precedent: run one pass, sleep the interval, run it again, in this
process, until stopped. `--every` here means exactly the same thing and takes
exactly the same value syntax (Go duration: `30s`, `5m`, `1h`), so an operator
who learned one has learned both. There is no `--daemon`, no `--start/--stop`,
no service — this is a foreground loop the operator starts, in the lineage of
`gopi merge --every`, and deliberately **not** of the retired resident merger
daemon (`cmd/agent/merger_retired.go`).

Targets are `--peer <alias>` when given, else **every declared memory peer**
(`memory_peers` / `GOPI_MEMORY_PEERS`) — the "no operator retyping the verb"
half of the epic.

**Non-interference (epic constraint).** The loop is reachable from exactly ONE
frontend verb. It touches no ledger row, it is wired into no worker/merger tick,
and it adds no daemon — the state it keeps across rounds is a `map[string]bool`
in the function that owns the loop. A round that takes minutes blocks only
itself.

## 2. The three quiet rules (why a periodic leg does not get muted)

An automated tick that prints the same line every cadence trains its operator to
ignore it. So the loop prints:

| round outcome | printed |
|---|---|
| imported > 0 (knowledge moved) | one round line with the tally |
| imported == 0, **known** (converged) | nothing at all |
| the peer reported **no tally** | one "cannot tell whether knowledge moved" line, once |
| peer unreachable | one line, then silent while it stays down |
| peer recovered | one line, once |

The failure filter is the epic's "logged once per STATE CHANGE": `down` keeps a
per-peer state, set on the first failure and cleared by the first success. The
same discipline covers the unknown tally (a standing condition, said once) — it
rides the same map rather than growing a second mechanism.

## 3. "Did this push move any knowledge?" — a question only the peer can answer

A `--push` run deliberately does not apply the peer's reply, so the initiator
has no import tally of its own. What it *can* know is the shape of the reply the
peer already sent (phase 1's one-ssh-leg protocol: the peer imports our patch,
then answers with its own export).

So the answer now rides back on that reply: the serve handler fills
`SyncPatch.ImportStats` (imported / existing / deduped / refused), and the
initiator reads it into `MemoryAssetSyncStats.PeerImport`.

Two properties make this cheap and safe:

* **Additive, no version bump.** The field is `omitempty` and is a *report*,
  never a data field — an older initiator drops it silently and loses a number,
  never a row (contrast the data-bearing fields `syncCheckPatchVersion` guards).
* **Absence is `nil`, never zero.** `PeerImport == nil` means "the peer did not
  report", and the loop prints the *unknown* line rather than folding it into
  the silence a converged round earns. Rendering an unmeasured outcome as
  "imported 0" would be a fabricated convergence (Rule 15).

Verified live (this task's own run, §5): the tally arrives, round 1 reports
`imported 1 row(s)`, and a later round that carries a new row reports
`imported 1 row(s)` again — non-zero exactly when knowledge actually crossed,
0-and-silent on the rounds in between.

## 4. The cadence knob (Rule 2)

* The **default is a constant**: `config.MemorySyncEveryDefaultSeconds = 300`
  (5 minutes — the cadence `gopi merge --every 5m` already uses).
* **Exactly ONE reader** of the env override: `config.ResolveMemorySyncEvery()`
  (`GOPI_MEMORY_SYNC_EVERY_SEC`, seconds). The `--every` flag is parsed by its
  own frontend parser and does **not** consult the env var — the flag's own
  parser is the caller that decides, which is the Rule 2 shape the one-shot
  verb's `--timeout` uses.
* A malformed/zero/negative override is an **error**, never a silent fallback:
  a typo must not quietly retime a loop.
* Documented as a CONFIG.md row (same commit), with the source line the
  `internal/standards` config-doc gate checks — that gate fails the build when
  the doc invents a knob, drops a catalogued one, or carries a stale line
  reference.

## 5. What was run for real (Rule 31)

The real verb, against a real ssh leg, with the fake-ssh shim standing in for
the second host (the epic's documented two-DB local analogue):

```
$ GOPI_MEMORY_DB=$SB/a.db GOPI_SYNC_NODE=peerbox GOPI_MEMORY_PEERS=hp
  GOPI_SYNC_E2E_PEER_DB_hp=$SB/b.db ./bin/gopi memory sync --peer hp --push --every 2s
gopi memory sync: --every 2s — running one --push round toward hp now, then every 2s; Ctrl+C (SIGINT) or SIGTERM stops it
gopi memory sync round 1: peer hp imported 1 row(s) from our push (1 offered, 0.0s)
gopi memory sync: stop signal — the loop stops here
```

Three things that output proves, each of which was a wrong guess first:

* **Round 1 carried the row** — `imported 1 row(s)` is the PEER's tally.
* **Rounds 2, 3, 4 were SILENT** (converged). Not "printed 0" — printed
  nothing. That is the property the epic asks for, observed rather than
  asserted.
* **A round that LATER gained a row reports again.** A second row was stored on
  this node mid-loop; the next round printed `round 3: peer hp imported 1
  row(s) from our push (2 offered, 0.0s)`. The loop's silence is therefore
  information, not a blind spot.

The failure path, same binary, same loop, an alias with no simulated DB:

```
gopi memory sync round 1: peer hpz is unreachable — memsync assets: ssh hpz: exit status 1
  (stderr: fake ssh: no simulated DB for peer hpz) (further failures are suppressed until it recovers)
```

**One line for four consecutive failing rounds** (the run lasted ~9 s at a 2 s
cadence) — the per-state-change suppression, observed.

### The signal trap this run walked into

The first live run **died at exit 143 with no stop line**. The cause was not the
loop: `internal/signalrec` installs a SIGTERM recorder that files provenance and
then, for an argv it cannot classify, *re-raises* the signal with the default
disposition one grace later. `memory sync` is not one of its handler-owning mode
words, so it read as "unclassified ⇒ forward", and `signal.Reset` cleared the
loop's own registration mid-wait.

The fix is the documented one-line contract: `signalrec.ClaimSignalHandlers()`
immediately after `signal.Notify`. That sentence now sits at the call site
(`cmd/agent/cli_memory_sync_every.go`), because the next loop-shaped verb
written here will hit exactly the same wall.

## 6. Not done (honest)

* **The one-shot verb's report is unchanged.** `--push` still prints
  `pull{skipped=N}`; the peer's *import* tally is decoded into
  `MemoryAssetSyncStats.PeerImport` but the one-shot line does not print it yet.
  The periodic leg is the consumer this phase needed; widening the one-shot
  line is a small follow-up, and leaving the historical wording intact keeps
  phase 1's E2E assertions honest.
* **The fake-ssh shim script was written by hand for this session's live run**
  (it lives in the scratch dir, not the repo). The repo's own E2E writes its own
  shim (`memorySyncE2EFakeSSH`), so nothing depends on the hand copy.
* **A peer older than this change will not report a tally**, and the loop says
  so once — but the peer must still be running a binary that supports
  `--serve-assets` (phase 1). Cross-host binary skew is otherwise out of scope
  here (the `syncCheckPatchVersion` gate covers the data-bearing direction).
