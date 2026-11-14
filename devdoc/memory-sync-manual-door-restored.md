<!-- doc-class: normative -->
# `gopi memory sync` — the MANUAL cross-node knowledge door, restored

**What exists now.** `gopi memory sync --peer <alias>` runs ONE unique-only
session between this node and a declared `memory_peers` peer, and exits.

```
gopi memory sync --peer peerbox                 bidirectional (the default)
gopi memory sync --peer hp --push             one-way OUTWARD
gopi memory sync --peer hp --pull             one-way INWARD
gopi memory sync --peer hp --dry-run          size the outbound patch, no ssh
gopi memory sync --serve-assets               the PEER side (ssh'd, not typed)
```

**What did NOT come back, and must stay retired.** The verb was deleted on
2026-11-05 by `5677cc6e` together with the whole cross-node sync CLI family
(task sync, transfer, submit-forward, claim broadcast). Three capabilities were
retired for their own measured reasons and are NOT part of this restoration:

| retired | why (its own decision, unchanged) |
|---|---|
| `--every <dur>` periodic loop | re-ran the session on a cadence from an operator's terminal; `MemorySyncEveryRetiredEnvName` still names the knob and its reader is gone |
| `memory_sync_on_store` | a `memory store` must never spawn a peer session (epic memory-store-sync-decouple, 2026-10-27) |
| the resident worker/merger tick | held a worker round for 315-534s measured in `~/.gopi/logs/worker.log` (`devdoc/memory-sync-tick-retired.md`) |

So the door is MANUAL: nothing schedules it, nothing triggers it, one invocation
is one session.

**Why restoring it is a wiring job and not a rewrite.** The tools layer was never
removed. `MemoryAssetsPeerSessionMode`, `MemoryAssetsServeRemote`,
`MemoryAssetsDryRun`, `SyncExport` / `SyncImport`, the `memory_peers` whitelist
(`config.ResolveMemoryPeers`) and the direction modes all survived the CLI
deletion. What was missing was argv → those functions, plus the two words:

- `tools.MemorySyncVerb` = `sync` and `tools.MemorySyncServeAssetsFlag` =
  `serve-assets` (`internal/tools/memory_sync_verb.go`). They live in the TOOLS
  layer, not in `internal/cliverbs`, because the same string is BOTH the command
  an operator types and the REMOTE COMMAND this binary sends over ssh — the
  shape `gopi memory backup --serve` already has. The old `cliverbs` constants
  were deleted in `5677cc6e` when their last reader left.

**The defect this restoration had to repair in passing.** The asset transport's
production path had been replaced by a refusal (`remoteLegRetired("memory asset
sync", peer)`) whose reason was true at the time: it dialled a verb that no
longer dispatched, so every session would have ended in a removal notice read at
the far end of a connection as a protocol error. The refusal is gone and the leg
composes its argv again —
`<gopi> memory sync --serve-assets`, ONE remote-argv element (the quoting
contract `syncRemoteGopi` documents), with `BatchMode`,
`ConnectTimeout` and the ServerAlive keepalives. A stale peer (one whose binary
predates `--serve-assets`) still gets the actionable upgrade hint instead of a
raw ssh failure.

**Two guards had to be re-aimed, and that is the interesting part.** Both were
written on 2026-11-06 to catch prose that taught a deleted command; both keyed on
the bare address `gopi memory sync`, and once the verb runs again a gate that
still judged it would go red on CORRECT prose — the way a gate gets suppressed.
The subject was narrowed to the part that is still gone: the `--every` cadence
flag. That is

- `cmd/agent/memory_sync_address_guard_test.go`'s `deletedMemorySyncAddress`
  (now `"gopi memory sync --every"`, with its judged-paragraph floor lowered
  5 → 4 in the same commit and the four paragraphs named in the comment), and
- `internal/standards/retired_pipeline_reference_gate.go`'s
  `retiredRefMemorySyncPattern`, which now requires verb AND flag on the line.

A gate whose subject moved is not a gate that got weaker: the population it
judges is measured, the floor sits on it, and a floor left above a shrunk
population fails loudly rather than passing quietly.

**How it was verified** — real sessions, not `-h` (Rule 31). Two `gopi tool
sandbox` profiles stood in for two nodes; the ssh leg's argv was composed for
real and a shim ran the remote command string locally with the NEW binary and
the PEER's profile. Measured, in one session: `pushed=1 pull{imported=0 …}`
(a row crosses one way), then `pushed=1 pull{imported=1 …}` (the peer's own row
comes back the other way), then a third session reports `imported=0 existing=1`
— idempotent, converged, insert-only. Refusals were measured too: an undeclared
peer exits 1 naming this node's whole whitelist, `--push --pull` together exits
1 as mutually exclusive, and a `--pus` typo exits 1 instead of silently running
a bidirectional session.

**Known limitation, stated rather than hidden.** A real `--peer peerbox` against
the live fleet could not be run from this branch: `peerbox` and `hp` still run a
binary built before the restoration, so they answer
`gopi memory: unknown subcommand "sync"` and the leg correctly reports that as a
failed session. The peer-side capability exists only after this branch is built
on both hosts. The sandboxed session proves the protocol, the argv, the
transport and both directions; it does not prove a live pair.

## What else had to move with it (prose is part of the surface)

Restoring a verb leaves FALSE PROSE behind in both directions, and both were
measured on this branch rather than guessed:

| surface | was | now |
|---|---|---|
| `CONFIG.md` `GOPI_MEMORY_SYNC_TIMEOUT_SEC` row | "RETIRED 2026-11-05, its flag face and the whole verb were DELETED" | LIVE again, BOTH faces, `--timeout` wins over the env, one reader `assetSyncSSHBound` |
| `CONFIG.md` divergence paragraph | "the manual spelling was itself deleted … what an operator has instead is a ONE-WAY COPY" | two manual doors — `memory sync` converges, `memory backup` copies |
| `internal/config/settings.go` help-env row | same "RETIRED" text | the bound is live and the flag wins |
| `config.LedgerCopyForwardReplacement` | one-way copy only | names BOTH doors, and says which one imports |
| `config.MemorySyncEveryRetiredReplacement` | "the since-DELETED `gopi memory sync` took the loop with it" | the VERB is back; the CADENCE is what is gone — the constant must not deny a command that runs |
| `internal/tools/memory_sync_assets.go` header | "⚠ THE CLI FACE OF THIS FILE IS DELETED", "nothing in production reads the machinery" | the CLI face is live and manually reached; the readers are named |
| `internal/cliverbs/cliverbs.go` margin | "the since-DELETED `gopi memory sync`" | why the word lives in the TOOLS layer instead |
| `internal/config/help.go` `gopi help` index | no row, with a comment explaining the absence | a row, plus the note on `memsync` which is still gone |
| 11 devdoc pages carrying the 2026-11-05 retirement banner | "the command this page names no longer exists" | "PARTLY REVERSED — the command is back, minus `--every`" |
| `devdoc/memory-sync-tick-retired.md`, `-periodic-leg.md`, `-timeout-and-oneway.md` | the verb quoted as deleted / the `--every` loop quoted as live | a reading note distinguishing the restored verb from the still-retired cadence |

**The first line of that table is the one that matters most.** A configuration
row that says a knob is retired while the code reads it is worse than no row:
`CONFIG.md` is what an operator consults BEFORE changing a knob, and this
project's own rule is that a wrong answer about configuration costs as much as a
wrong change.

## Verification — the surface, run

```
$ ./bin/gopi memory sync -h                    # the page, rc=0
$ ./bin/gopi memory sync --peer peerbox --dry-run
dry-run hosta→peerbox: push would carry 17465 row(s) (local non-node rows; anti-reflow excludes node:* imports)   rc=0
$ ./bin/gopi memory sync --peer nosuchhost
gopi memory sync: peer "nosuchhost" is not declared — "peerbox|hp" is this node's whole whitelist
  ("memory_peers", or GOPI_MEMORY_PEERS for one run); topology is declared, never discovered   rc=1
$ ./bin/gopi memory sync --peer peerbox --push --pull
gopi memory sync: --push and --pull are mutually exclusive (omit both for one bidirectional session)   rc=1
$ ./bin/gopi help | grep "memory sync"
  gopi memory sync            One MANUAL session against a declared `memory_peers` peer: …
```

Raw captures: `/home/user/.gopi/sessions/01a1b2a2-4cf0-70b0-906f-c77f861fde69/evidence/07-restored-verb-measured.txt`
— where the row count is 17465 at one minute and 17467 a few minutes later: it is
THIS node's ledger growing while the report is written, which is what a live
count does. Read the number as "the whole corpus is sized", never as a constant.

(and `05-sandboxed-session.txt` for the two-way sandbox the section above
describes).

## A defect this branch FOUND IN ITS OWN VERIFICATION (fixed, with a guard)

The first version ran the direction and `--timeout` validators ONLY after the
`--dry-run` early return, so a dry run never reached them. Measured, 2026-11-06:

```
$ ./bin/gopi memory sync --peer peerbox --dry-run --timeout 0
dry-run hosta→peerbox: push would carry 17465 row(s) …        rc=0     ← WRONG
$ ./bin/gopi memory sync --peer peerbox --timeout 0
gopi memory sync: --timeout "0" is not a positive number of seconds   rc=1
```

One flag set, two verdicts — and the LENIENT one is the rehearsal, which is
exactly where an operator checks a command before running it for real. Nothing
in the suite would have caught this: the dry-run path was covered, the refusal
path was covered, and the two were never run TOGETHER. It was found by executing
the surface (Rule 31), which is the rule's whole point.

The fix moves every validator ABOVE the branch:
`memorySyncDirection` → `applyMemorySyncTimeout` → then `--dry-run`. And because
`runMemorySync` exits the process, the regression guard
(`TestMemorySyncValidatesEveryFlagBeforeTheDryRunBranch`) pins the property
rather than the order: both validators are pure and total, so a value is refused
whatever else is in the argv.

Corrected measurement, same binary shape:

```
$ ./bin/gopi memory sync --peer peerbox --dry-run --timeout 0        rc=1
$ ./bin/gopi memory sync --peer peerbox --dry-run --push --pull      rc=1
$ ./bin/gopi memory sync --peer peerbox --dry-run --timeout 60       rc=0
```

And the two-profile session was re-run AFTER the fix, because a validation-order
change is exactly the kind of edit that can leave the happy path broken while
every refusal stays green:

```
$ GOPI_HOME=$SBA GOPI_MEMORY_PEERS=fakepeer PATH="$EV/bin:$PATH" \
    HARNESS_BIN=$EV/bin HARNESS_PEER_HOME=$SBB \
    ./bin/gopi memory sync --peer fakepeer --timeout 60
sync fakepeer: mode=both pushed=2 pull{imported=0 existing=1 deduped=0
  body_differing=0 refused=0 rows=1/1 batches=1 failed=0} (0.6s, bound=60s)     rc=0
```

`existing=1` is the converged pair doing what the design says: the row the peer
already holds is not touched, the one it lacks crosses, nothing is duplicated.
The `bound=60s` in that line is the proof that `--timeout 60` reached the ONE
reader — a flag that silently failed to publish would have shown `900s`.
