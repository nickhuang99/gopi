<!-- doc-class: normative -->

> **Retired mechanism (2026-11-05/06).** Every `cmd/agent/cli_tasks_sync*.go` path this page cites as a
> live location was DELETED with the cross-node sync family; `gopi task pull` / `receive` / `sync-pass`
> now dispatch to a removal sentence and exit 1. What survived was the SWITCH (`task_pull`) and its
> one reader — **and both are gone now:** the switch's reader (`config.TaskPullEnabled`), its resolver,
> its banner, its settings key, its env name, its catalogue row, its CONFIG.md row and its two test
> files were deleted in batch 5 (2026-11-06, commit `d7d836e5`). The spellings survive only in
> `internal/config` as `TaskPullRetiredEnvName` / `TaskPullRetiredSettingsKey` /
> `TaskPullRetiredReplacement`, which `cmd/agent/task_pull_retired.go` NAMES back at an operator whose
> settings file still carries the key (Rule 15 — the decoder ignores an undeclared key silently).
> Read the body below as the RECORD of the split, not as a description of live behaviour.

# task_pull — the worker-pull switch, split out of task_sync

<!-- WORKER-OWNERSHIP: OPERATOR-ONLY — devdoc/worker-ownership.md -->

> **CORRECTED 2026-10-29 (epic `01a186d6`).** The WORKER-side trigger this page
> describes (`workerTakeOverAttempt`, the loop's step ②′, and the
> `workerSyncPeers` row-exchange session beside it) has been **DELETED** from
> `cmd/agent/cli_tasks_worker.go`: a worker's round is CLAIM → RUN → REPORT, and
> taking a peer's row (a WRITE to the peer's ledger) never was one of its
> duties. The `task_pull` switch and its resolver (`config.TaskPullEnabled`)
> still exist, but its only consumer is now the **manual** face
> `gopi task receive --takeover` (`cmd/agent/cli_tasks_sync.go`). Read the
> section below as the RECORD of why the split was made; read
> `devdoc/worker-round-audit.md` for what the round does today. Two claims in it
> are now false and are named here rather than rewritten: "the loop's step ②′"
> and "`workerSyncPeers` … still reads `TaskSyncEnabled` — pinned by
> `TestSyncMLConsumersStillReadTaskSync`" (that test is gone with the function).

**Task**: `01a1247c-12db-798a-9b20-e0aab5184150` · branch
`task/01a1247c-12db798a9b20e0aab5184150`

## The defect

A worker whose LOCAL queue is empty already had the code to ask a declared peer
for one transferable row and run it (`workerTakeOverAttempt`, the loop's step
②′, `cmd/agent/cli_tasks_worker.go`; the peer-side handover is `task receive
--takeover`, epic *task-pull* deliverable ④). It was unreachable by
configuration on any node with `task_sync: false`, because its first line read:

```go
if !config.TaskSyncEnabled() || !config.HasSyncPeers() {
```

`TaskSyncEnabled` answers a **different** question — "does this node
automatically EXCHANGE ledger rows with declared peers (claim broadcast,
terminal-state push, serve-tick delta pass, submit forward)?" — and its own doc
comment lists exactly those four crossings. Taking WORK from a peer after the
local queue drains is not one of them; it was riding a switch whose semantics
never covered it. Measured (hosta, 2026-10-10, operator's report): `task_sync:
false` there made every drained round return at that gate, so the transfer path
was live code no configuration could reach.

## The change (deliberately minimal)

| # | Where | What |
|---|---|---|
| 1 | `internal/config/task_sync_switch.go` | `TaskPullSettingsKey` (`task_pull`) · `TaskPullEnv` (`GOPI_TASK_PULL`) · `taskPullDefault` · `resolveTaskPull()` · `TaskPullEnabled()` · `TaskPullLine()` — shape copied from `resolveTaskSync`, **same file**, for the reason its `ChatOpAuto` sibling already states: one place resolves a switch of this shape, so precedence, the truthy/falsey vocabulary and the "which channel decided" reporting cannot drift between them (Rules 2/13/14) |
| 2 | `cmd/agent/cli_tasks_worker.go` | the one-line gate: `config.TaskSyncEnabled()` → `config.TaskPullEnabled()`. The other **8** `TaskSyncEnabled()` consumers are untouched |
| 3 | `internal/config/config.go` | `TaskPull bool \`json:"task_pull,omitempty"\`` on `Config`, with the "not the same axis as TaskSync" note |
| 4 | `internal/config/settings.go` | the `EnvVars` catalog row (also what `gopi --help-env` renders) |
| 5 | `internal/config/help.go` | the env goes in the grouped table's *Distributed task ledger* section; the section lists literal names, so both switches are named from their constants |
| 6 | `cmd/agent/main.go` | the `workerUsageHead` Environment block, and the startup banner line via `config.TaskPullLine()` beside `TaskSyncLine()` |
| 7 | `CONFIG.md` | the §2c *Task ledger: identity and topology* row (key / env / no flag / semantics / `read at` line ref) + the banner bullet in §4 |
| 8 | `internal/standards/config_doc_flag_gate_test.go` | `configDocFlagRowCount` 70 → 71, with the audit-trail comment the constant exists for |

Nothing was deleted: the SyncML lane is intact and still gated by `task_sync`.
`sync_peers` is untouched — it remains both the manual verbs' whitelist and the
topology half of the new gate (`<switch> && sync_peers non-empty`), so a
single-node host pays one boolean and never dials.

## Design decisions worth keeping

- **Default OFF is the whole safety story.** `task_pull` joins the family whose
  default is the upgrade posture (`TaskSyncEnabled`'s own comment): a node that
  upgrades with the key absent must not start reaching out to a peer's ledger.
  OFF is the literal zero value, so a stale value carried by an upgraded client
  cannot enable a crossing on its own.
- **A third banner line, not a suffix on the second.** The task-sync line
  invites the wrong inference ("rows are off ⇒ my worker reaches nobody");
  naming the deciding channel per switch on its own line keeps the two
  independent answers readable, and the pair is why an operator can now tell
  *which* knob to turn.
- **`omitempty` + a presence probe.** The typed `Config` cannot distinguish
  "off because you asked" from "off because nobody asked" (both are the zero
  value), so `resolveTaskPull` reads the presence off the decode
  `FromSettingsFile` already performed (`Config.rawTopLevel`, same package) —
  no second file read, whereas the older `taskSyncDeclaredInSettingsFile()`
  re-reads it. The VALUE still comes from the one resolution.
- **An unreadable spelling is reported, never read as off** (Rule 15): the
  `sync.Once`-free `os.Getenv` funnel is the one place `GOPI_TASK_PULL` is read
  (Rule 2), a typo goes to stderr and the settings channel decides.

## Verification (Rule 5 / Rule 31)

- Build: `CGO_ENABLED=0 go build ./...` → rc 0;
  `CGO_ENABLED=0 go build -o bin/gopi ./cmd/agent/` → rc 0
- Tests: `make test-budget PKGS='./internal/config ./cmd/agent ./internal/standards'`
  (see the report for the tally; the independence cases are unstubbed by
  `-run 'TaskPull|TakeOver|SyncMLConsumers'`)
- Ran (verbatim, in `bin/gopi`):

```
# harness: gopi tool sandbox create --label taskpull --task-id 01a1247c  (a real
# private profile; ledger and sessions are its own)
# settings fixture: {"role":"client","sync_to":"hosta-fixture.invalid",
#                    "sync_peers":["hosta-fixture.invalid"],"task_sync":false}

env GOPI_HOME=$SB GOPI_SETTINGS=$ART/settings-taskpull.json GOPI_TASK_PULL=0 \
    ./bin/gopi worker --once          → drained, NO takeover line
env GOPI_HOME=$SB GOPI_SETTINGS=$ART/settings-taskpull.json GOPI_TASK_PULL=1 \
    ./bin/gopi worker --once          → drained, NO takeover line (--once is the deploy seam)
env GOPI_HOME=$SB GOPI_SETTINGS=$ART/settings-taskpull.json GOPI_TASK_PULL=1 \
    ./bin/gopi worker                 → "worker: takeover hosta-fixture.invalid: … (next peer)"
env GOPI_HOME=$SB GOPI_SETTINGS=$ART/settings-taskpull.json GOPI_TASK_PULL=0 \
    ./bin/gopi worker                 → drained, NO takeover line
```

  The peer alias is RFC-2606 `.invalid` ON PURPOSE: a leg that never dialled
  cannot name a host, and the leg that did dial is *provably* the new one (its
  own line names the peer, and DNS refuses so nothing real was contacted).
  Full log: `~/.gopi/sessions/01a1247c-…/artifacts/task-pull-evidence.log`,
  harness `…/artifacts/task-pull-evidence.sh`.
- Ran (banner, `gopi term` under the same profile — the three channels):

```
GOPI_TASK_PULL unset → task-pull: off (default — a drained local queue does not reach a declared peer; …)
GOPI_TASK_PULL=0     → task-pull: off (GOPI_TASK_PULL: false — …)
GOPI_TASK_PULL=1     → task-pull: on (GOPI_TASK_PULL)
GOPI_TASK_PULL=ture  → config: GOPI_TASK_PULL="ture" is not a recognised boolean (1/off) — ignored; task_pull decides
                      then: task-pull: off (default — …)      ← Rule 15: reported, never silently read as off
settings task_pull:true, env unset → task-pull: on (task_pull)
settings task_pull:true, env 0     → task-pull: off (GOPI_TASK_PULL: false — …)   ← env > settings
```

## Not done / honest limits

- **The run happened on peerbox, not on hosta.** hosta (192.168.7.13) answers
  ping but refuses ssh on 22, has no `Host` entry in peerbox's `~/.ssh/config`,
  and its 11434 is closed too — so no configuration could be driven on it from
  here. The verification uses a real sandbox profile on peerbox with hosta's exact
  blocked state (`role: client`, one declared peer, `task_sync: false`), and the
  peer alias is `.invalid` by design, so **no peer exists to take a row from**:
  the takeover leg is proven REACHED (its own line prints), not proven to move
  a row. A peer-backed end-to-end handover (a row reaching `cancelled` on the
  source ledger) is NOT covered by this branch and needs a reachable peer.
- `--once` intentionally never transfers (the deploy/E2E seam keeps its old
  contract); the pull is reached on the drain face, which is why the evidence
  runs both.
- `workerSyncPeers` (the same loop's pull-then-push session) still reads
  `TaskSyncEnabled` — pinned by `TestSyncMLConsumersStillReadTaskSync`, because
  the reverse edit would make enabling the pull silently enable row exchange.

## The defect the new tests caught (and the gate that catches it)

The first full-package run went red on
`TestTaskPullLineDistinguishesTheTwoOffStates`, and the failure was REAL:

```
the declared OFF line must name the key that was read, got
"task-pull: off (declared in task_sync: false — …)"
```

`resolveTaskPull` had reused the sibling switch's source label
(`taskSyncDeclaredHere`), so a `task_pull` declaration rendered as
`declared in task_sync` — value right, key name wrong, and wrong in exactly the
way an operator reads. Each switch now carries its OWN label
(`taskPullDeclaredHere = "declared in " + TaskPullSettingsKey`), and the test
asserts the KEY NAME rather than merely that the two OFF lines differ — a test
that only compared the strings would have passed with the label shared.
