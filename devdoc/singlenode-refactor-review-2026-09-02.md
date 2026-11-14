<!-- doc-class: historical -->
# Single-Node Refactor — Code Review Report (2026-09-02)

Branch: `feat/singlenode-refactor`
Epic: SPI machine-config single-node refactor — gate all cross-node machinery
on the DECLARED topology (settings.json). Single-node mode =
`compute_nodes: []` AND `sync_peers: []` → the runtime is pure local.

This report classifies every piece of cross-node machinery per the epic
question — **disable / localize / keep** — with file:line evidence, then the
refactor commits that implement the classification.

---

## Review baseline

The review is done against `master` HEAD `0e49478` (2026-09-02), with the two
in-scope bugfix branches merged in to form the refactor base:
`feat/fix-pushback-current-branch` (2252 — merged-branch flood / push-back
gate) and `feat/fix-merged-worktree-cleanup` (9004 — merged-branch worktree
cleanup). Those branches close epic requirements 3/4's machinery; this
refactor adds the **single-node gating layer** that makes all of it a no-op
when no peers exist.

Machine this review runs on: **peerbox** (hostname `peerbox`). This is the
single-node execution position (per `devdoc/single-node-convergence-20260902.md`).

---

## The single-node definition (settings as the source of truth)

- `compute_nodes: []` — EMPTY (cleared 2026-09-02).
- `sync_peers: ["hp"]` — STILL SET. This is the 14:39 stale-branch-flood
  source AND drives the git channel's fetch/push-back.
- `role`: absent.

Single-node = `compute_nodes` AND `sync_peers` both EMPTY (no declared peer,
nothing to sync to/from). The refactor **gates cross-node machinery on these
two** and adds tests proving empty topology ⇒ zero remote ops; the settings
itself is cleaned to `sync_peers: []` as the deliverable ④.

---

## Per-machinery classification

### 1. Claim-lock protocol — LOCALIZE (disable the remote leg), keep local CAS

**Files**: `internal/tools/memory_sync_claim.go`, `memory_sync_claim_ssh.go`,
`cmd/agent/cli_tasks.go:876-892` (the `RunClaimLockProtocol` call site),
`cmd/agent/cli_tasks_worker.go:16` (PushTerminalTaskSync).

The two-phase protocol (M4.21) runs Phase-1 **LOCAL** CAS claim
(TaskNextFiltered / TaskClaimExplicit → claimed) then Phase-2/3 **REMOTE**
CAS acquire+advance over ssh (`--claim-remote`) and a must-succeed terminal
push (`PushTerminalTaskSync`, `taskregister.go:207`).

**Single-node verdict**: the LOCAL CAS (防本地双跑) is the whole point of
single-node claim — KEEP. The remote leg (Phase-2 ssh CAS, the ALOHA
yield-loop, the must-succeed terminal push) must be a **no-op with no peers**.

The code is ALREADY no-op-safe at the resolve level:
- `StartClaimLockBroadcast` (memory_sync_claim.go:164): `ResolveSyncPeers`
  empty → immediately `out <- {Action: ClaimLockProceed, Reason:
  "no sync_peers declared — single-node claim"}` (line 186). No ssh is spawned.
- `RunClaimLockProtocol` (line 543) drives that broadcast — with no peers it
  returns Proceed on round 1, zero remote ops.
- `PushTerminalTaskSync` (line 697): `ResolveSyncPeers` empty → `return nil`
  vacuous success (line 708).

**Existing tests prove this** (all in `internal/tools/memory_sync_claim_test.go`
+ `singleside_claim_test.go`):
- `TestRunClaimLockProtocolNoPeers` (line 321), `TestStartClaimLockBroadcast
  EmptyPeers` (line 375), `TestPushTerminalTaskSyncNoPeersIsSuccess` (line
  466) — all sandbox `GOPI_SETTINGS` to empty and assert Proceed/vacuous-success.

**Change needed**: the CODE is already correct — the git channel issue is
that with `sync_peers:["hp"]` the remote leg did NOT short-circuit. The
refactor's single-node gate must (a) make the serving tick skip the git pass
entirely when no peers (see §3), and (b) the settings cleanup removes `hp` so
these existing no-op paths actually engage. Verify (acceptance 1): claim with
no peers spawns no ssh — should be asserted at the serve boundary too (the
tests above are unit-level; I add a serve-tick-level assertion).

### 2. Task sync / harvest / diff / push — KEEP as manual opt-in, NO auto-sync

**Files**: `internal/tools/memory_sync_task.go`, `cmd/agent/cli_tasks_sync.go`,
`cmd/agent/cli_tasks_sync_git.go`.

The db task-row sync (file/memsync + `gopi task sync` family).

**Single-node verdict**: nick ("two sides can have completely different task
sets, no need to sync tasks"). The commands REMAIN as manual opt-in for a rare
one-off `task sync --peer` (they already refuse undeclared peers), but:
- **`serveGitSyncSweep`** (cli_tasks_sync_git.go:55, called at
  cli_tasks.go:1317) — after a git pass it invokes `runTaskSyncTick()` when
  delta-sync is otherwise off (the "六合一" ride-along). This must be a no-op
  without peers.
- **`runTaskSyncTick`** (cli_tasks.go:1366, cli_tasks_sync.go:264) — already
  gated: `ResolveSyncPeers` empty → return (line 278). Good.
- **Nothing** in the serve tick / task-run path may auto-sync without peers.

### 3. Git channel — DISABLE the server pass / localize the client pass

**Files**: `internal/tools/task_sync_git.go` (client+server passes, push-back
core), `cmd/agent/cli_tasks_sync_git.go` (`serveGitSyncSweep`), the in-flight
`task_sync_git_pushback.go`.

- **`serveGitSyncSweep`** (cli_tasks_sync_git.go:55): throttle-clock gated,
  then role-resolves (`IsServerNode()` → ServerPass, else ClientPass). It is
  the server/tick remote-facing sweep. **In single-node**: with `sync_peers:
  []`, the ClientPass already early-skips (`GitSyncClientPass` line 269:
  "no sync peer (no server to push to)") and ServerPass early-skips
  (`GitSyncServerPass` line 293: "no sync peers declared (nothing to fetch)").
  But the sweep still RUNS the throttle bookkeeping and role resolution. For
  a clean no-op, the sweep should short-circuit at the top when no peers.

- **`GitSyncPushLocalBranches`** (now delegating to `gitPushbackPlan` from 2252):
  pushes to the server target. The 2252 fix already makes it skip
  already-integrated branches (the resurrection flood). The current-task-only
  branch push is `TaskFinishCleanupSessionID` (client finish-cleanup). Both
  already require peers to have a target.

**server-side close loop** (§3 of epic): peerbox (this machine) is the server.
It fetches client-pushed branches and merges them via `AdoptionSweep` +
`GitSyncServerPass.fetch`. In pure single-node there are no client-pushed
branches, but the epic REQUIRES the server-side merge→cleanup→rebuild loop
over **LOCAL Bond branches** to still work. That machinery is:
  - `AdoptionSweep` (task_adoption.go) — sweeps `refs/heads` + `refs/remotes/origin`;
    **role-independent**, serves LOCAL branch merges regardless of peers.
  - `serveMergeBacklogAudit` / `serveMergeExecSweep` (local merge executor — 
    `task_merge_backlog.go`, `task_merge_executor.go`).
  - `serveMergedCleanupSweep` (task_merged_cleanup.go, from 9004) — removes
    merged-branch worktrees + branches.
  These are the **LOCAL integration machinery** = KEEP.

### 4. Serve-tick passes — per-pass disposition

**File**: `cmd/agent/cli_tasks.go:1241-1410` (`runTaskServe`) — the pass at
top (1266-1340) then the daemon loop (1357-1389). Passes and single-node
disposition:

| Pass | Line | Single-node | Why |
|------|------|-------------|-----|
| stale sweep (`serveStaleSweep`) | 1270 | **KEEP (local)** | zombie in_progress rows are a single-node problem |
| adoption (`serveAdoptionSweep`) | 1278 | **KEEP (local)** | merges local Bond branches; the "adoption sweep only sees LOCAL branches now" |
| finish-cleanup (`serveFinishCleanupSweep`) | 1285 | **KEEP (local)** | clears the worktree+branch after a local Bond's done |
| merged-cleanup (`serveMergedCleanupSweep`) | 1298 | **KEEP (local)** | 9004's local worktree removal |
| adoption (dup call) | 1306 | **KEEP (dedup?)** | a SECOND `serveAdoptionSweep(opts.repo)` — this is a pre-existing redundancy (both fire before dispatch) — see note below |
| branch-scan (`serveBranchScan`) | 1308 | **KEEP (local)** | merge-backlog discovery over local branches |
| git-sync (`serveGitSyncSweep`) | 1317 | **DISABLE w/o peers** | fetch/push-back = the remote client/server channel |
| merge-backlog audit (`serveMergeBacklogAudit`) | 1324 | **KEEP (local)** | local branch merge enqueue |
| merge exec (`serveMergeExecSweep`) | 1334 | **KEEP (local)** | local merge lander |
| dispatch (`runDispatchOnce`) | 1340 | no-op w/o compute_nodes | `ResolveComputeNodes(nil)` empty → `runDispatchOnce` returns `(false, nil)` at line 381 |
| daemon run (`runTaskRunOnce`) | 1363 | **KEEP (local)** | the local claim+run |
| sync tick (`runTaskSyncTick`) | 1366 | no-op w/o peers | already gated |

**Note (duplicate adoption call)**: `serveAdoptionSweep(opts.repo)` appears
TWICE (1277-1278 and 1306). Both are pre-dispatch, same repo/options. The
internal throttle clock (`serveAdoptionLastTick`) means the second is a
no-op on the same tick UNLESS the first was throttled (then the second also
throttled) — they NEVER both run in one tick because the internal clock only
advances on the first successful call, and the second sees
`now - lastTick < interval`. So the double call is **harmless but redundant**;
I leave it (removing could risk touching a review-domain shared-edit zone
with the anchor/9004 branches; the redundancy costs one function-call no-op).

### 5. Branch-sync remnants — closed by the merged fixes

- **HP-side stale-branch accumulation / push-back flood** (`t-1788332252`):
  closed by `feat/fix-pushback-current-branch` (2252) — `gitPushbackPlan`
  skips already-integrated branches. MERGED into this branch.
- **Merged-branch worktree cleanup** (`t-1788329004`): closed by
  `serveMergedCleanupSweep`. MERGED into this branch.
- Both integrate cleanly; this refactor ADDS the no-peer gate that makes the
  underlying remote passes no-ops.

### 6. Memory db sync — SEPARATE, STAYS (untouched)

**Files**: `cmd/agent/cli_memsync.go`, `internal/tools/memory_sync.go`,
`memory_sync_session.go` (distilled_qa brew rows / procedural assets).

The memory-assets sync (pitfalls/lessons/experiences) is a DELIBERATE,
wanted feature (`t-1788307190`) — **NOT** part of this task's "remove". The
refactor touches NONE of these files: the memsync CLI and the 
`gopi memsync` subcommand are manual (no serve-tick auto path), and the
claim/task channels being gated are in `memory_sync_claim/task` — the 
`memory_sync.go` brew export/import is file-based and peer-free (uses
`--out patch.json` / `--db PATH`, never ssh). Nothing in this refactor
touches `memory_sync.go`, `cli_memsync.go`, `memory_sync_session.go`; the
procedural/brew asset sync path is buildable and unchanged (acceptance 5).

---

## The core refactor change

The machinery is already mostly correct at the resolve level (no peers ⇒
no-op). The GAP is that on the server (peerbox) with `sync_peers: ["hp"]`
still set, the serve tick's git-sync pass `serveGitSyncSweep` did run (it
fetched hp, adopted its branches, dispatched) — that was the pre-refactor
cross-node state. The refactor makes **single-node the default posture**:

1. **`serveGitSyncSweep` short-circuits without peers/role server**: when no
   sync peer is declared (the single-node shape), it logs one line and
   returns without role-resolution or the downstream tick. This is enforced
   by a new helper `gitSyncHasPeerTarget()` that resolves the target and the
   peers list once.
2. **The claim's `RunClaimLockProtocol` gate is tightened at the serve/CLI
   boundary** — not in the tools layer (already correct) but by ensuring no
   code path even ATTEMPTS the remote protocol when the topology is empty.
   This is defense-in-depth; the tools-level no-op paths + a serve-level
   no-ssh assertion back it.
3. **settings cleanup**: `sync_peers` → `[]` so the single-node shape is
   actually in effect on this machine.
4. **Single-node regression tests** (acceptance 1-4) at the serve boundary.

I scope the refactor to these so the anchor/9004-doctor/file-domain rules are
respected (I do NOT touch `task_adoption.go`, `task_merge_backlog/executor.go`,
`task_merged_cleanup.go`, or `task_finish_cleanup.go` bodies — the 9004 fix's
territory — nor `memory_sync.go`).

---

## In-flight branch coordination

Reviewed ALL file domains touched by in-flight branches vs master:

- `feat/fix-pushback-current-branch` (2252): `cmd/agent/cli_tasks.go` (harvest
  CLI print lines), `internal/tools/memory_sync_task.go`,
  `task_sync_git.go`, NEW `task_sync_git_pushback.go`+. MERGED into refactor.
- `feat/fix-merged-worktree-cleanup` (9004): `cmd/agent/cli_tasks.go` (the
  merged-cleanup serve pass), NEW `task_merged_cleanup.go`+,
  `task_adoption.go`, `task_merge_executor.go` comments. MERGED into refactor.
- `feat/fix-deliverable-branch-backfill`, `feat/fix-anchor-guard-claim-gate`,
  `review/cli-vision`: NOT in the refactor's scope. No overlap with the files
  I change (`cmd/agent/cli_tasks_sync_git.go`, `cmd/agent/cli_tasks.go` git
  pass call-site region, settings).

My refactor edits `cli_tasks_sync_git.go` (adds the no-peer short-circuit)
and the git-sync call site in `cli_tasks.go` if needed — both are in territory
MY base branch already owns (2252 merged in), so no clobbering.

---

## Files this refactor changes

- `cmd/agent/cli_tasks_sync_git.go` — `serveGitSyncSweep` no-peer short-circuit.
- `internal/tools/task_sync_git.go` — a `GitSyncHasPeers`/target helper already
  largely present; expose a single no-peer resolver if needed.
- `cmd/agent/cli_tasks_sync_git_test.go` + a new serve-boundary single-node
  test — assert no peer ⇒ `serveGitSyncSweep` does zero remote work.
- `~/.gopi/settings.json` — `sync_peers` → `[]`.
- `devdoc/singlenode-config.md` — the config-shape documentation.
- This report.

---

## Acceptance mapping

1. `sync_peers=[]` + `compute_nodes=[]` ⇒ `task run next` performs NO ssh
   (claim local-only) — new serve-boundary test asserts `RunClaimLockProtocol`
   never reaches the ssh path (Proceed on round 1) + no ssh binary invocation.
2. serve tick with empty topology runs stale/adoption(local)/merge-backlog
   but no fetch/dispatch/remote sync — new test asserts `serveGitSyncSweep`
   + `runDispatchOnce` are no-ops with empty peers/nodes.
3. Client push-back pushes only its own branch — the existing 2252 regression
   tests (`TestPushback*`) cover it; kept.
4. Merged-cleanup + worktree-remove + rebuild — the 9004 tests cover cleanup;
   the merge loop is `TaskMergeBacklogExecute`/`AdoptionSweep` (existing tests).
5. memory-assets sync untouched — `go test ./internal/tools/` still builds;
   no edit to `memory_sync.go`/`cli_memsync.go`.
6. `go test ./internal/... ./cmd/agent/` green.
7. devdoc committed.
