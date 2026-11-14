<!-- doc-class: historical -->
# Task-row sync gets its own switch (default OFF)

<!-- WORKER-OWNERSHIP: OPERATOR-ONLY — devdoc/worker-ownership.md -->

**Epic**: five conflated axes — identity, collaboration, delivery, knowledge,
dispatch — were decided by each other's knobs. This change separates the
**task-row collaboration** axis, gives it ONE switch, and turns it OFF by
default.

Status: implemented on `feat/task-sync-switch-off` (branch, not merged).

## The conflation, concretely

| question | before | after |
|---|---|---|
| Who am I? (conflict arbitration, whose finish-cleanup push-back runs) | `role` | `role` — unchanged |
| Does my ledger exchange rows with peers AUTOMATICALLY? | `role=client` + `sync_to`, and `sync_peers` for the serve legs | **`task_sync`** (env `GOPI_TASK_SYNC`), default **OFF** |
| Where does my BRANCH go when a run finishes? | `role`/`sync_peers` via `finishCleanupServerTarget()` | unchanged — **never** gated by the new switch |
| Which knowledge assets travel? | `memory_peers` | unchanged — not gated |
| Where may a task be dispatched? | `compute_nodes` | unchanged — not gated |

The measured symptom the epic started from: on a `role=client` node **every**
local `gopi task submit` was either FORWARDED to the peer or hard-refused
(`internal/tools/task_submit_forward.go`), so a client node could not enqueue
its own work — even when it shared nothing with that peer.

## The switch

`internal/config/task_sync_switch.go` — one definition, one reader:

- settings key `task_sync` (`config.TaskSyncSettingsKey`)
- env `GOPI_TASK_SYNC` (`config.TaskSyncEnv`), precedence env > settings > OFF
- `config.TaskSyncEnabled()` is the ONLY reader (Rule 2); falsey env spellings
  (`0`/`off`/`false`/`no`) resolve OFF; a spelling that is neither truthy nor
  falsey (a typo) is **recorded on stderr and falls through to settings** — never
  silently read as "off" (Rule 15)
- `config.TaskSyncLine()` is the banner's never-empty third line
  (`[gopi] task-sync: on (task_sync)` / `off (default …; task_sync/GOPI_TASK_SYNC enables it)`)

Default OFF is also the **upgrade posture**: a node that arrives with a stale
`role: client` + `sync_to` keeps behaving locally until an operator
deliberately turns exchange on. That is why the key is new rather than a
repurpose of `sync_to`/`sync_peers`.

## What OFF disables — the automatic crossings (each gated at its own entry point)

The switch answers ONE question — "does this node exchange **task-ledger rows**
with declared peers?" — and only the ROW crossings below consult it. The two
CODE-DELIVERY passes were removed from this table on 2026-09-22 (epic: "decouple
code delivery from the task-row switch"): delivering a branch REF is the delivery
axis, and gating it on the row switch meant a declared peer with `task_sync` off
handed nothing back and adopted nothing, while the third delivery face (a single
task's close-out push-back) had never read the switch at all.

| row crossing | gate site | why it is automatic |
|---|---|---|
| submit forward | `internal/tools/task_submit_forward.go: taskSubmitNeedsForward` | the receipt the SUBMITTER never asked to route |
| claim-lock broadcast (remote leg) | `internal/tools/memory_sync_claim.go: StartClaimLockBroadcast` | `gopi worker` runs it per claim |
| terminal-state push | `internal/tools/memory_sync_claim.go: PushTerminalTaskSync` | the close-out fires it |
| serve db-sync tick (the delta pass) | `cmd/agent/cli_tasks_sync.go: runTaskSyncTick` | the one-shot pass face / resident tick |
| worker round-end db sync | `cmd/agent/cli_tasks_worker.go: workerSyncPeers` | end of every worker round |

### The CODE-DELIVERY axis — decided by the declared topology, never by this switch

| delivery face | site | precondition |
|---|---|---|
| a finished task's branch push-back | `internal/tools/task_finish_cleanup.go: finishCleanupServerTarget` | a declared peer (`config.ResolveSyncPeers`) |
| serve git/ref pass (fetch/push) | `cmd/agent/cli_tasks_sync_git.go: serveGitSyncSweep` | `config.HasSyncPeers()` + role (`tools.IsServerNode`) |
| peer-branch adoption sweep | `cmd/agent/cli_tasks_adopt.go: serveAdoptionSweep` | `config.HasSyncPeers()` |

ONE declared peer therefore means ONE coherent "code delivery is on" state
across all three faces, while row exchange stays independently OFF. With
`sync_peers` empty every face is a pure no-op (nothing to deliver to, nothing to
adopt) — the single-node posture, unchanged.

Gate placement is deliberate in two spots:

- `serveGitSyncSweep` and `serveAdoptionSweep` check **`HasSyncPeers()` FIRST**,
  and never the row switch: their axis is delivery, and the peer list is its
  whole precondition (nothing declared = nothing to deliver to or adopt from).
  They check it before the throttle clock advances, so an empty topology is a
  provable no-op rather than a skipped stage.
- `StartClaimLockBroadcast` checks the switch before peer resolution, so an off
  switch pays no settings read per peer, spawns no ssh leg, and does not advance
  the ALOHA round.

## What OFF does NOT disable

- **Manual verbs.** `gopi task sync|diff|harvest --peer`, `gopi task push`,
  `gopi memsync` are operator-typed: the switch governs exchange a node does
  **on its own**. A switch that silently broke a typed verb would be a new
  defect, and `sync_peers` remains their whitelist.
- **The CODE-DELIVERY axis — all three of its faces.** The finish-cleanup
  branch push-back chain, the serve git/ref pass and the adoption sweep are the
  DELIVERY axis, and the reason the epic is safe to default OFF. Pinned three
  ways: `internal/tools/task_sync_switch_finish_test.go` proves with a real
  two-node git world that the branch still reaches the PEER's ref with the
  switch off (plus a structural assertion that `finishCleanupServerTarget()`
  never starts consulting `TaskSyncEnabled`), and
  `cmd/agent/task_sync_switch_test.go` +
  `cmd/agent/task_sync_pass_test.go` do the same for the two periodic passes
  (they RUN with the switch off and a peer declared, and are a pure no-op with
  no peer declared whatever the switch says).
  Measured end to end with the real one-shot face
  (`scripts/evidence-code-delivery-axis.sh`, 3 shapes): peer + switch OFF →
  the git pass dials the peer and the adoption sweep scans, rows exchanged = 0;
  no peer + switch ON → nothing runs at all; peer + switch ON → all three.
- `role`, `compute_nodes`, `memory_peers`.

The claim-lock print now distinguishes the two reasons it has for "no remote
leg" — the switch (`task_sync`) vs no peers (`sync_peers`) — so an operator
reading `[claim-lock]` never mistakes one for the other.

## Local submit with the switch off

`taskSubmitNeedsForward()` returned true for `role=client` unconditionally; it
now requires the switch on. Consequences:

- a client node with the switch off submits into its OWN ledger — which is the
  queue that really claims, runs and notifies the row;
- the refusal that named the would-be local path still fires when the switch is
  ON and no authoritative peer is declared (`taskSubmitForwardRefusal`), so the
  remote-submit black hole guard is intact;
- an upgrading node with a stale `sync_to` does **not** start forwarding.

## Tests changed (pre-existing assumptions)

| test / fixture | was | now |
|---|---|---|
| `internal/tools/task_submit_forward_test.go` fixture | role=client + sync_to was enough to forward | the fixture sets `GOPI_TASK_SYNC=1` (that scene IS about forwarding); named in-file |
| `internal/tools/taskid_whitespace_normalize_residual_test.go` sandbox | role=client + peers | + `GOPI_TASK_SYNC=1` (its claim rounds must reach the fake ssh capture) |
| `internal/tools/remote_submit_repro_test.go` | one phase: mirror leg must refuse, not write locally | two phases: ① ON + no peer → the refusal still fires and writes nothing; ② OFF → submits locally AND the row is really in the ledger |
| `cmd/agent/singlenode_serve_test.go` | expected `[claim-lock] single-node: …` on empty topology | expects `[claim-lock] task sync OFF …` (that is now the winning arm), and the git-sweep clock test declares the switch ON on top of its peer |

New tests: `internal/config/task_sync_switch_test.go` (7),
`cmd/agent/task_sync_switch_test.go` (7), and the finish-push-back file above.

## Verification recipes worth keeping

- Two-sided controls: every "off ⇒ no-op" test is paired with "on ⇒ the pass
  reaches its own next boundary", read on the pass's throttle clock. A gate
  written as an early `return` after the work would pass a one-sided test.
- The controls run with the process moved OUT of the checkout (`t.Chdir`), so a
  pass that does run finds no repo and dials nothing — a serve test must never
  fetch/push/adopt in a real worktree.
- The push-back proof reads the PEER's ref (`git rev-parse refs/heads/<branch>`
  in the bare server repo), not the step list, so a report cannot pass on
  optimism.

## Rule 31 — the surfaces actually run (and two findings that came out of it)

Run against the rebuilt `bin/gopi` on `feat/task-sync-switch-off`, with
`GOPI_HOME` / `GOPI_SETTINGS` pointed at throwaway sandboxes so no operator
settings took part.

**The banner, all three channels** (`gopi term`, stderr):
`off (default — …)` on a clean profile; `on (task_sync)` with
`"task_sync": true`; `on (GOPI_TASK_SYNC)` with the env override on a
settings-off profile.

**Manual verbs are unaffected by the switch** — `gopi task push` and
`gopi task sync --peer localhost --push-only` dial `localhost` with the switch
OFF (they fail on the sandbox's ssh auth, which is the point: they RAN), and
`gopi task sync -h` now documents which half is automatic.

**Submit, both arms, real ledger** — client-shaped settings:
OFF ⇒ `Task queued: 01a0c380-…` and `task list` really carries the row
(`1 task(s)`); ON + unreachable peer ⇒ the loud refusal naming
`…/.gopi/memory.db` and `task list` shows `No tasks found.` (the black hole
guard still holds).

### Finding 1 — the whole cross-node delivery path never runs (pre-existing)

`gopi merge` does not reach `mergerPassOnce`. Measured three ways: a
`code_search` for `mergerSerialFaceLegs` resolves only its declaration
(`cmd/agent/cli_tasks.go:4143`) and never its call site (4217, inside
`mergerPassOnce`); `grep -rn 'mergerPassOnce('` finds the declaration and three
test files that assert on its text, and **no caller**; and a compiler probe —
replacing the call with `_ = mergerSerialFaceLegs` — still builds clean, which
is only possible when no live path reaches it.

The mechanism is recorded in `devdoc/merger-retired.md`: the daemon was retired
by **closing its entry points** while keeping `mergerPassOnce` in the tree
deliberately ("the dispatch that used to run is left verbatim and unreachable in
the tree"), and that same section claims the cross-node channel passes run
unconditionally during the tick. So `serveGitSyncSweep` and
`serveAdoptionSweep` are unreachable in master today: `[serve] [git-client]` /
`[git-server]` never prints, and this branch's switch changes nothing about
that (ON and OFF produce identical logs from `gopi merge`).

This is **not** this epic's defect and the switch is correctly placed for when
the face is rewired. Filed as a follow-up rather than fixed here: rewiring a
retired face is the re-development's own task, and doing it inside a
configuration-switch branch would hide a large change behind a small one.

### Finding 1, ADDRESSED — `gopi task sync-pass --once` gives the crossings a face

Finding 1 above established that the serve-tick crossings were unreachable
(`gopi task serve` is a retired-daemon refusal; `gopi merge` never calls
`mergerSerialFaceLegs`). A gate inside an unreachable verb cannot be observed
working, so the epic's second half was not deliverable by gating alone. The
resolution is NOT to un-retire anything — it is to give the passes the
one-shot face the retired loop's per-tick body deserves:

- `gopi task sync-pass --once` (`cmd/agent/cli_tasks_sync_pass.go`) runs ONE
  pass: `serveGitSyncSweep` → `serveAdoptionSweep` → the delta tick (the same
  two functions, the same order the retired tick used, and the delta tick is
  still owned by the git sweep's `deltaSyncOn()` decision so it never
  double-runs).
- The verb is `taskverbs.SyncPass`, declared in the leaf constant package and
  in `SyncVerbs`, so the family dispatcher, the completion set and the
  top-level index stay derived rather than re-typed (Rules 1b/13/14).
- Each leg keeps reading its OWN precondition ITSELF — the face never
  pre-checks one. A caller-side check would leave the legs' own gates
  unexercised, which is the durability the epic asks for.
- `--once` is mandatory: a verb named after a loop that silently ran one tick
  is a surprise, not a service (Rule 15).

### Second half (2026-09-22): the delivery legs were the wrong axis — fixed

The first version of this face had the git/ref pass and the adoption sweep under
the row switch, i.e. it preserved the very conflation the epic exists to end:
with `sync_peers: ["hp"]` and `task_sync` off, a client pushed NOTHING and a
server adopted NOTHING. Both legs now read `config.HasSyncPeers()` (the git pass
also reads the role, `tools.IsServerNode`), never `config.TaskSyncEnabled()`;
the delta tick keeps reading the switch. Measured, three shapes, real binary —
`scripts/evidence-code-delivery-axis.sh`:

| shape | topology | git/ref pass | adoption sweep | rows exchanged |
|---|---|---|---|---|
| A | peer declared, `task_sync` OFF | RUNS (dials the peer) | RUNS (scans) | 0 |
| B | no peer, `task_sync` ON | no-op | no-op | 0 |
| C | peer declared, `task_sync` ON | RUNS | RUNS | 1 (the delta leg) |

A defect found by that same run and fixed with it: the row leg was wired to the
git sweep's TAIL, whose `deltaSyncOn()` early return is only correct for a
LIVE tick (where the loop body runs the row tick right after). On the one-shot
face it inverted the row axis — shape C exchanged zero rows. The face now calls
`runTaskSyncTick()` itself, once, after the two delivery legs; the ride-along
tick in the sweep is left for the resident caller. A delivery leg must never
own the row axis.

**Measured, both arms, real binary, hermetic settings** (`GOPI_SETTINGS`
pinned — the lesson from memory #3197: `GOPI_HOME` alone does not sandbox the
settings table):

```
$ GOPI_SETTINGS=…-off.json GOPI_SYNC_GIT_INTERVAL=1 GOPI_SYNC_INTERVAL=1 \
    ./bin/gopi task sync-pass --once
gopi task sync-pass: pass starting (task-row exchange OFF (default …))
gopi task sync-pass: pass done
# grep -c hp-fake-peer  →  0 lines: no leg dialled, no ssh, no git, no tick

$ GOPI_SETTINGS=…-on.json  (same, task_sync:true)  ./bin/gopi task sync-pass --once
gopi task sync-pass: pass starting (task-row exchange ON)
[serve] [git-client] push_failed=[server-master fetch: git fetch --force --no-tags hp-fake-peer:…]
[serve] [git-client] push: 1 branch(es) failed: …
[serve] [adopt] skipped: worktree dirty (adoption refuses a dirty tree)
gopi task sync-pass: pass done
# grep -c hp-fake-peer  →  2 lines: the legs really ran
```

The ON arm's failures are the *expected* ones — `hp-fake-peer` does not
resolve — and they are the evidence: a leg that never dialled cannot report
`ssh: Could not resolve hostname`. The ENV override was measured on the same
file: `GOPI_TASK_SYNC=1` over `task_sync`-absent settings produces the dialling
arm (2 lines), which is the documented `env > settings` precedence.

`gopi merge` is deliberately left alone: rewiring a retired face is the
re-development's own task, and this face is the non-invasive bridge (it shares
the same two functions, so a future `mergerSerialFaceLegs` caller and this verb
cannot diverge).
