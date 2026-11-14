<!-- doc-class: normative -->
# Task Report: task-notify terminal gap — funneling every status move through one notify path

Epic: `task-notify-terminal-gap` (2026-09-12) · Branch `fix/terminal-notify-gap`
Worktree `~/workspace/gopi-notifygap` · Base `3b51fc00`

## Completed

### 1. The premise was tested before it was believed

The briefing's literal claim — "the notify hook is wired into exactly two funnel
points, so the worker writeback/finish path moves the ledger row silently" — was
**falsified by measurement** before any code was written. A real `task run` with
a failing child on the PRE-FIX binary (`/tmp/gopi-prefix-notifygap`, built from
`3b51fc00`) delivered `in_progress` **and** `failed`: the writeback path is
`writebackOutcome → TaskRunWriteback → TaskUpdateWithResult → taskUpdate → notifyTaskTransition`,
i.e. it already funneled. The production ledger agrees: 10901 tasks have a
`completed` notify event, 8189 a `failed`, 294 a `stale`.

Every `UPDATE tasks ... status = ?` / `INSERT INTO tasks` site was then read
directly. The genuinely silent writers were:

| silent writer | status it moved |
|---|---|
| `RollbackLocalClaim` | claimed/in_progress → **pending** |
| `TaskClaimExplicit` (Phase-1 CAS) | **pending → claimed** |
| `syncClaimRemoteAcquire` (Phase-2/3 CAS ladder) | **pending → claimed → in_progress** |
| `syncClaimRemoteRevert` | claimed/in_progress → **pending** |
| `syncDeltaApplyNewer` (rank ladder) | any forward move incl. **terminal** |
| `syncDeltaServerResolve` (conflict ruling) | → **completed** (fired only the alarm, never the completion) |
| `harvestUpdate` (peer adoption) | local live → **remote terminal** |
| `syncTaskMigrate` / `syncTaskMigrateFrom` | **pending → terminal** |

`internal/tools/tasks.go:3775/3811` (`TaskResumeClaimed`, `TaskStampSelfClaim`)
rewrite `claimed_by`/`claimed_at` only — same-status, correctly not a transition.

### 2. The fix: one shared notify path (`internal/tools/tasknotifybus.go`)

`notifyTaskTransition` stayed the single delivery implementation; what changed is
that **any** writer can reach it:

- `taskNotifyBus.observe(prev, cur)` — a row-imaged transition.
- `taskNotifyBus.observeIDs(taskID, prevStatus, status)` — a row-addressed move
  whose payload is re-read from the ledger at flush (the batch writers).
- `taskNotifyBus.observeEvent(prev, cur, decorate)` — a payload decorator, which
  is how the zombie **`stale`** alarm (zombie evidence chain, `previous_status`
  = the row's own status) now rides the *same* gates as the transitions. The
  pre-fix `notifyStaleTask` was a second, hand-rolled copy of the config-resolve
  + events allow-list + pattern-match + delivery sequence; it is now an
  `observeEvent` call, and its `{"zombie":…}` payload shape is byte-compatible.
- `taskNotifyFlush` / `taskNotifyFlushDeferred` (past-commit) /
  `taskNotifyFlushStatus` / `taskNotifyFlushStatusMove` — the four call shapes.

Transactional writers (`TaskHarvest`, `SyncTaskImportDelta`, the claim-remote
terminal leg) buffer their observation and flush **after** the commit, so a
rolled-back write never announces a status the row does not have.

The notify `events` allow-list filter is **not weakened**: it is still the one
gate. What is new is that a configured subscriber now sees a **log line** when a
transition is dropped by the list or by `pattern` (Rule 15) — the missing line
becomes diagnosable without changing what is delivered.

A notify delivery failure still never touches the ledger: every failure path ends
in `taskNotifyLog` (stderr).

### 3. Complete path table over `taskAllowedTransitions`

"Funnels through taskUpdate?" = did the pre-fix hook at
`tasks.go:2395` fire for it.

| status | writer fn (file) | via taskUpdate (pre) | notifies before | notifies after |
|---|---|---|---|---|
| **pending** ← (root) | `insertTaskRow` (tasks.go:1090) — `TaskNew/OID`, `TaskSubmit`, `TaskReopen` | no (INSERT) | **no** (birth, not a transition) | **no** — deliberate: the notify `events` default excludes `pending`, and `TaskSubmit` births `pending` rows (see limits) |
| pending → **claimed** | `TaskNextFiltered` CAS (tasks.go:3139) | no (own CAS) | **yes** (the one hardcoded hook, prev literal `taskStatusPending`) | yes (bus, prev snapshot) |
| pending → **claimed** | `TaskClaimExplicit` (tasks.go:3640) | no | **no** | **yes** |
| pending → **claimed** | `syncClaimRemoteAcquire` (memory_sync_claim.go:531) | no | **no** | **yes** |
| pending → **in_progress** | — none (Phase-1 claim is mandatory) | — | — | — |
| claimed → **in_progress** | `TaskStart` → `TaskUpdate` (tasks.go:3691) | **yes** | yes | yes (unchanged) |
| claimed → **in_progress** | `syncClaimRemoteAcquire` Phase-3 (:550) | no | **no** | **yes** |
| claimed → **pending** | `RollbackLocalClaim` (:665) | no | **no** | **yes** |
| claimed/in_progress → **pending** | `syncClaimRemoteRevert` (:615) | no | **no** | **yes** |
| claimed/in_progress → **pending** | `syncTaskMigrateFrom` (memory_sync_task.go:1123) | no | **no** | **yes** via `SyncTaskImport`/`syncTaskMigrate`/the cancelled-intent ruling |
| in_progress → **completed** | `TaskUpdate` / `TaskUpdateWithResult` / `TaskRunWriteback(+Tagged/Tags)` / the **worker completion path** `writebackOutcome` (tasks.go:2085/2359) | **yes** | yes | yes (unchanged) |
| in_progress → **completed** | `syncDeltaApplyNewer` rank ladder (memory_sync_delta.go:823) | no | **no** | **yes** (bus, flushed past the batch) |
| in_progress → **completed** | `syncDeltaServerResolve` conflict ruling (:703) | no | **only the `legacy-conflict` alarm** | **yes** (completion + alarm) |
| in_progress → **completed** | `harvestUpdate` (memory_sync_task.go:1906) | no | **no** | **yes** (flushed past the `TaskHarvest` commit) |
| in_progress → **failed** | `TaskUpdate*` / worker failure path | **yes** | yes | yes (unchanged) |
| in_progress → **failed** | **zombie disposal** `TaskUpdateWithResult` from `sweep-zombies --kill` (taskzombie_sweep.go:218) | **yes** | yes | yes (unchanged) + the `stale` alarm also rides the shared path now |
| any → **failed/completed** | `syncDeltaApplyNewer` / `harvestUpdate` | no | **no** | **yes** |
| completed ↔ failed (rank-3 peers) | `syncDeltaApplyNewer` 后到覆盖 (:823) | no | **no** | **yes** |
| completed/failed → **verified** | `TaskUpdate*` (verify verdict) | **yes** | yes | yes (unchanged) |
| completed/failed → **verified** | `syncDeltaApplyNewer` | no | **no** | **yes** |
| any → **cancelled** | `TaskUpdate*` / `TaskCancel*` | **yes** | only when `cancelled` is in the allow-list | unchanged (still opt-in) |
| **stale** (synthetic) | `DetectStaleTasks` (taskheartbeat.go:157) + `SweepZombies` | n/a (synthetic) | yes — own forked delivery | **yes — same path, shared gates** |

### 4. Regression test, provably red on pre-fix code

`internal/tools/tasknotifyband_test.go` — four tests, each driving a **real
writer** and asserting the delivered event:

| test | writer driven |
|---|---|
| `TestNotifyGapRollbackLocalClaimFiresPending` | `RollbackLocalClaim` (real `TaskClaimExplicit` claim first) |
| `TestNotifyGapClaimRemoteLadderFires` | `syncClaimRemoteAcquire` (both CAS legs) |
| `TestNotifyGapHarvestUpdateFiresTerminal` | real `TaskHarvest` transaction (`harvestUpdate`) |
| `TestNotifyGapServerRulingFiresCompleted` | real `SyncTaskImportDelta` server ruling |

**RED proof** (same test file copied into a detached worktree at pre-fix `HEAD`):

To reproduce from the fix branch (the pre-fix tree is not kept on disk — its base
commit is in history):

```bash
git worktree add --detach /tmp/notifygap-prefix 3b51fc00
cp internal/tools/tasknotifyband_test.go /tmp/notifygap-prefix/internal/tools/
cd /tmp/notifygap-prefix && GOPI_SETTINGS=/dev/null go test ./internal/tools/ -run TestNotifyGap -count=1
# → FAIL (4/4);  git worktree remove /tmp/notifygap-prefix when done
```

```
--- FAIL: TestNotifyGapRollbackLocalClaimFiresPending  delivered events: []
--- FAIL: TestNotifyGapClaimRemoteLadderFires          delivered events: []
--- FAIL: TestNotifyGapHarvestUpdateFiresTerminal      delivered events: []
--- FAIL: TestNotifyGapServerRulingFiresCompleted      delivered events: [legacy-conflict]
FAIL   (4/4)
```

Post-fix: `go test ./internal/tools/ -run TestNotifyGap -count=1` → **ok**.

Two pre-existing delta tests were adapted honestly rather than worked around:
they captured the notify target with `cat >` (truncate → last delivery wins), a
shape that only survived because `syncDeltaServerResolve` fired exactly one
event. With the completion now delivered too, the capture appends and the tests
assert the alarm is **present** (`notifyLogHasEvent`) instead of being the only
line — the human-task alarm assertion was additionally preserved.

## Real-task before/after evidence

Three end-to-end scenarios, each run against the **pre-fix** binary
(`/tmp/gopi-prefix-notifygap` @ `3b51fc00`) and the **post-fix** binary
(`/tmp/gopi-fixed` @ `fix/terminal-notify-gap`), in fresh sandboxes
(`GOPI_HOME`/`GOPI_SETTINGS`/`GOPI_MEMORY_DB` pinned), with
`GOPI_TASK_NOTIFY_CMD='cat >> task-events.jsonl'` — the same file an operator's
`~/.gopi/notify/task-events.jsonl` is:

```
########## PRE-FIX @HEAD (no funnel fix)
(A) completed run : status=completed delivered=[in_progress,completed]
(B) claim rollback : status=pending  delivered=[in_progress]
(C) zombie dispose : status=failed   delivered=[stale,failed]
########## POST-FIX @fix/terminal-notify-gap
(A) completed run : status=completed delivered=[claimed,in_progress,completed]
(B) claim rollback : status=pending  delivered=[in_progress,pending]
(C) zombie dispose : status=failed   delivered=[stale,failed]
```

Reading of the diff:

- **(B) is the acceptance-relevant A/B.** Both binaries ended with the row at
  `pending`; only the post-fix build delivered the `pending` line. This is the
  claim-lock rollback leg (`RollbackLocalClaim`) — the transition that was
  provably **silent** and is now **delivered**, verified on both sides.
- **(C)** shows the zombie-disposal `failed` line arriving on **both** builds —
  an honest correction to the briefing's premise (that writer already funneled
  through `taskUpdate`); the `stale` alarm additionally now rides the shared
  path. Per the allow-list it requires the subscriber to list `stale`
  (`stale,failed,in_progress` in this run); with the default event set the alarm
  is dropped — pre-fix it was silently dropped, post-fix the drop is logged.
- **(A)** `completed` is delivered on both builds; the post-fix run shows the
  `claimed` line too because in that sandbox the run's claim went through
  `TaskClaimExplicit` (pre-fix the missing `claimed` was masked by an ambient
  smoke-test row in the shared notify file). The worker-completion path itself is
  unchanged and was never silent.

Observed delivery-failure-is-not-fatal and allow-list logging samples:

```
[task-notify] event "stale" for 01a0925d-… not in the events allow-list — not delivered (row is still in_progress; …)
[task-notify] event "in_progress" for 01a09256-… filtered by pattern "feat*" — not delivered
[task-notify] zombie sweep 01a0925d-…: KILLED (stalled, heartbeat stop + claimed pid 999999 dead (peerbox) — confirmed zombie)
```

## Not Done / honest limits

1. **Row birth is not notified, by design.** `TaskSubmit`/`TaskNewOID`/
   `TaskOpen` mint rows that are born `pending` or `in_progress`; a birth is not
   a transition and `pending` is outside the notify `events` default. If the
   operator wants an event for "work enqueued", that is a deliberate
   `events=pending` subscription decision, not a gap.
2. **`cancelled` and `verified` remain opt-in** — absent from the default
   `events` string. They now *do* fire when subscribed; with the default set they
   are dropped, and (post-fix) that drop is logged.
3. **The sync writers announce the row's own move, not the remote's story.** A
   peer-side adoption that lands identically (`RowsAlready`/`Duped`) notifies
   nothing, because the local ledger did not move. The `from-<peer>` provenance
   tag is not part of the payload's `previous_status`.
4. **`syncClaimRemoteRevert` reports the observed previous status.** Its CAS is
   `status IN (claimed, in_progress)`; the payload carries whichever the leg read,
   which is the row's real prior state but not proof of which one the UPDATE
   matched. Closing that would need a per-row post-write re-read (not worth a
   round-trip here).
5. **`harvestInsert`/`syncDeltaInsertRow` (rows arriving from a peer) notify
   nothing** — same class as (1): an INSERT is a birth, not a local transition.
6. **`TaskNextFiltered`'s claim CAS was already wired**; its hardcoded
   `prevStatus` literal was replaced by a row snapshot so the predecessor value
   has one source (the row) rather than a call-site constant.
7. **The bulk-ingest path (`archive/taskingest.go:185`) is NOT funnelled — it is
   out of scope by construction.** That file carries `//go:build ignore` and
   imports `internal/tools` as its own package (`package tools`), so it is
   **excluded from every build** (`go build ./archive/` → "build constraints
   exclude all Go files") and from `go build ./...`. It is a one-shot bulk
   importer for historical ledgers, not a live writer of `tasks.status`; wiring
   the bus into a file no build compiles would be untestable and is deliberately
   left alone. **If that importer is ever un-ignored, it must be revisited** —
   it is the one remaining non-funnel status writer in the tree.
8. **The full `internal/tools` suite is slow (>20 min)**; verification used
   targeted `-run` filters with `-count=1`, plus one full-suite background run
   whose result is recorded below. Any package not named here was not re-run
   beyond `go build ./...` + `gofmt -l`.

## Verification

- Build: `CGO_ENABLED=0 go build ./...` → OK; `gofmt -l internal/ cmd/` → clean.
- Targeted suites: `go test ./internal/tools/ -run 'TestSync|TestTaskNotify|TestStale|TestZombie|TestDetectStale|TestHarvest|TestClaim|TestRollback|TestImport|TestBidir' -count=1` → **ok (39.3s)**.
- New regression: pre-fix **4/4 FAIL**, post-fix **ok** (`-run TestNotifyGap -count=1`).
- Standards gates: `go test ./internal/standards/ -count=1` → **ok**. My edits widened `internal/tools/memory_sync_claim.go` (+35) and
  `internal/tools/memory_sync_delta_test.go` (+43), which shifted 5 baselined
  `file:line` permits; the baseline was updated to the SAME 5 sites (not new
  permits — the entries' targets are unchanged, the ratchet stays where it was,
  and the gate fails on a stale entry, which is what caught the shift).
- `internal/stylegate` **fails identically at the pre-fix base** (`cmd/agent/cli_voice.go`,
  `cmd/agent/cli_voice_server.go`: 2 ignored-error + 2 unchecked-close hits).
  Pre-existing and untouched by this change — not adopted here.
- Real-session A/B: see the table above (three scenarios × two binaries).
- Full-package run: `GOPI_SETTINGS=/dev/null go test ./internal/tools/ -count=1 -timeout 110m` →
  **ok, `315.226s` (exit 0)** — the run was still in flight when the first draft of this
  report was written and quoted it as green; it has since been collected, and it is green.
  (The claim was re-checked because a report may not assert a result it has not read.)
- Commit: `e75e2ee5 fix(tools): funnel every operator-relevant status transition through one notify path`

## Closing re-verification (independent pass over the claims above)

- **RED proof re-run first-hand**: `tasknotifyband_test.go` in the detached pre-fix
  worktree `/tmp/notifygap-prefix` @ `3b51fc00` → **4/4 FAIL**, every one with
  `delivered events: []` (the ruling test with `[legacy-conflict]`). Exit 1.
  Post-fix `-run TestNotifyGap -count=1` → ok.
- **Full-suite claim collected**: `ok github.com/nick/gopi/internal/tools 315.226s`, exit 0.
- **Production sink cross-check** (acceptance 1/2, on the operator's real
  `~/.gopi/notify/task-events.jsonl`): of the last 400 terminal ledger rows, 329
  carry their terminal event; **every one of the 71 missing is >24h old**
  (pre-notify-config history) and **zero** are from the last 24h. Live rows in the
  last 2h read e.g. `ledger=completed → [claimed,in_progress,completed]` and
  `ledger=failed → [claimed,in_progress,stale,failed]`.
- **Const/rule audit**: no bare status literal was added by the diff (every status
  in the new/edited production files is a named const; only comments quote
  `"stale"` as prose). The `internal/standards` baseline change is 5 → 5 permits —
  the SAME five targets, line numbers shifted by the edits; no new permit, and
  the gate (which fails on a stale entry) is green.

## Follow-ups (not done here)

- The test-hygiene landmine found during the probe: `internal/tools`' `TestMain`
  unsets every `GOPI_TASK_NOTIFY_*` var, but `config.ResolveNotify` falls back to
  the **ambient** `~/.gopi/settings.json` when `GOPI_SETTINGS` is unset — so a
  default-env test run can write into the operator's REAL notify sink. The runs
  above pinned `GOPI_SETTINGS=/dev/null`; a permanent guard belongs in `TestMain`.
