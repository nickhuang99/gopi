<!-- doc-class: normative -->
# M4.24 Sync Reconciliation — task diff --peer + sync --full + anchor-advance guard

> **Retired mechanism (2026-11-05).** Item ② below — the anchor-bypassing FULL
> session (`SyncFullReconcile` → `syncPeerSessionFull`), its per-peer counter and
> the `GOPI_SYNC_FULL_EVERY` pacing knob — is **DELETED**: the cross-node sync
> surface went on 2026-11-05 and no production caller remained. Item ① (`task
> diff --peer`) and item ③ (the anchor-advance guard) are LIVE and are what
> `internal/tools/memory_sync_reconcile.go` still implements; read that file's
> package comment for the current three-part shape. The removal's own records:
> `CONFIG.md`'s REMOVED rows and the anti-re-advertisement gate
> `internal/config/sync_knob_removal_gate_test.go`. Sections below that describe
> a *periodic full pass* describe a mechanism this repository no longer has.

Epic: the timestamp-delta protocol's anchor has one **self-evident premise**
(nick: common sense — the anchor is a *synchronized starting timeline*;
everything BEFORE the anchor must be identical on both sides). Nothing
mechanized that premise: a row created on one side whose `updated_at` is
older than the peer's receive-anchor is **permanently outside the delta
window** — silently lost until an unrelated update bumps it. The live
trigger: `aad7e286` (an auto-created task on HP whose peer anchor had
already advanced) nearly vanished. This epic mechanizes the premise in
three tools.

## Anchor semantics (nick's common sense, written into the design)

| Property | Meaning | Enforced by |
|---|---|---|
| Anchor = common confirmed start point | The receive-anchor for peer P says "I have everything from P up to this `updated_at`" | `sync_state(peer, anchor)` written ONLY after a session's merge succeeded (epic ⑥, unchanged) |
| **Advance precondition** | An anchor may advance ONLY when "everything before it is identical on both sides" holds | **NEW `syncAnchorGuard`**: before a delta session commits the anchor, pending counts must agree (local vs peer, ssh count probe). Mismatch → anchor does NOT move + warning naming the repair. Reconciliation takes precedence over increments. |
| Premise breakage = protocol bug, not data loss | A single-side row stranded under the anchor is a *detectable* divergence | **NEW `task diff --peer`**: both-direction (task_id,status) set compare — the premise made executable, read-only |
| Repair must be idempotent | Re-delivering the whole ledger is safe | **NEW `sync --full`**: both exports bypass the anchor (AfterSource=0, the claim-broadcast no-window pattern); the M4.21 rank merge absorbs replays (already / migrated / mirror) |

## What shipped

1. **`gopi task diff --peer <alias>`** — pulls the peer's full
   (task_id, status) catalog over ssh `sqlite3 -json` (harvest's transport,
   quote-free SQL), compares sets both directions: missing-on-peer,
   missing-on-local, same-id status mismatch. Headline count + per-row
   lines + `repair:` hint. Exit 1 on any difference (script-friendly).
   Never writes either ledger.
2. **`gopi task sync --full [--peer|--peers]`** — a full-reconciliation
   session: initiator exports `AfterSource=0` and sets `patch.Full`; the
   peer's `--serve-remote` sees `Full` and exports with its own anchor
   bypassed. Wire shape untouched (v2 envelope, one new optional field).
   The anchors advance to the freshly-proven end (the full pass IS the
   reconciliation, so the commit skips the guard).
3. **Anchor-advance guard** — `guardSyncStateCommit` wraps the delta
   session's anchor commit: pending-count equality (local vs peer via one
   ssh count probe) is the cheap writable proof of "identical before the
   anchor". Refuses + names the repair on mismatch. The full path commits
   directly (it just reconciled everything).
4. **Serve-tick periodic full** — `GOPI_SYNC_FULL_EVERY` (default 10,
   0=off): after N successful incremental sessions with a peer, the next
   tick session is a full one. Counter persists in `sync_state` under the
   key `<peer>#full-counter:` (no schema change; forward-only lifecycle
   rides the anchors). Malformed env = error (Rule 2, syncIntervalFromEnv
   precedent). Counters advance only on successful sessions.
5. **`--push-only --task-id T` fallback** (found in live testing): a row
   the anchor already passed is invisible to the window export, so a
   targeted push previously failed with "not found in the current ledger
   window". Now falls back to a full export for that one row (the same
   AfterSource=0 pattern).

## Live 实测 (peerbox ↔ hp, real nodes)

| 验证 | 结果 |
|---|---|
| ① diff 列出差异 | `diff hp: 2031 difference(s)` — 1029 missing-on-peer (peerbox-origin rows stranded under HP's receive-anchor), 0 status-mismatch after convergence; exit 1, repair hint printed |
| ② sync --full 收敛 | pushed=984 whole-ledger replay; `updated_at>1788244000` window 21/21 identical both sides; pending 19→4 (peerbox) / 12→2 (hp); replay rerun `already=3` = idempotent (no duplicates, no re-import) |
| ③ 守卫拒绝推进 | planted a local-only pending row → `task sync --peer hp` → `anchor guard: refusing advance to 1788245956 — pending counts differ (local=4 peer=3) … run diff --peer / sync --full`; anchor stayed at the old value |
| ④ 正常路径不退化 | delta session after `--full`: pushed=0, already=3, anchor advances cleanly (guard passes on equal counts); full-suite `go test ./...` green |

## Known residual (documented, not a bug)

The anti-reflow export gate (56fd84a: `from-<self>` rows never re-export)
also applies inside a `--full` pass — by design. Consequence: rows a node
*imported* from a third node cannot be repaired toward the peer by THIS
node's full pass; the diff reports them `missing-on-peer` and the peer's
own full session (which owns those rows as origin) carries them. In the
live run this left 27 peerbox-origin terminal rows absent on HP — all
terminal (completed/failed), zero pending → no work is lost, only
history. The guard's pending-count check still converges because the
residual contains no pending rows. If terminal-history completeness ever
matters, the repair is peer-side: `ssh hp gopi task sync --full peerbox`.

## Files

- `internal/tools/memory_sync_reconcile.go` — diff engine, full session,
  guard, tick counter (Rule-1 constant block at top)
- `internal/tools/memory_sync_reconcile_test.go` — reconCompare set logic,
  guard fail-closed, Rule-2 env parsing, counter cycles, replay idempotency
- `internal/tools/memory_sync_session.go` — delta commit routed through
  the guard; serve-remote honors `patch.Full`
- `internal/tools/memory_sync_task.go` — `SyncTaskPatch.Full` field
- `internal/tools/memory_sync_claim.go` — push-only single-row fallback
- `cmd/agent/cli_tasks.go` / `cli_tasks_sync.go` — diff/sync verbs, flags,
  usage, exclusivity guards, tick wiring

## Verification

- Build: `go build ./...` OK; `CGO_ENABLED=0 go build -o bin/gopi ./cmd/agent/` OK
- Tests: targeted suite 6/6 PASS; `go test ./cmd/agent/` full package PASS (79s);
  live two-node 实测 table above
- Worktree: feat/sync-reconcile @ this commit (shared object store with
  ~/work/GoPi master)
