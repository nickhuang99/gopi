<!-- doc-class: historical -->
# HP → peerbox Branch Delivery: Confirmation Audit + Fix (2026-09-16)

**Task**: `01a0a9fb-65fc-75d5-9ea5-b494a6d129f9` — "confirm HP push branch"
**Host**: `hostb-Laptop` (HP) — the declared **client**; peer = `peerbox`
(Rule 28: peerbox = system layer/server, HP = feature development).

## Verdict

**The delivery mechanism was correct and was NEVER RUN.** HP shipped
`"sync_peers": []`, and every HP→peerbox path is gated on that key — so each push
was a *silent no-op* and five completed tasks' deliverable branches never left
HP. **Fixed and verified live on HP** (§4); no code change was needed or made.

---

## 1. What "HP pushes its branch to peerbox" is implemented by

One atom, two faces (`internal/tools/task_sync_git.go`):

| face | entry point | role decision |
|---|---|---|
| resident worker tick | `serveGitSyncSweep(workerRoleLabel)` (`cli_tasks_worker.go:638`) | `tools.IsServerNode()` → server pass, else client pass |
| merger tick | `serveGitSyncSweep(mergerRoleLabel)` (`cmd/agent/cli_tasks.go:5874`) | same |
| serve pass | `serveGitSyncSweep(serveGitSyncRolePass)` (`cmd/agent/cli_tasks_sync_pass.go:141`) | same |
| manual | `gopi task push` (`cmd/agent/cli_tasks_sync_git.go:180`) | same |
| run end-hook | `cli_tasks.go:2202` `git push <repo> HEAD:<branch>` | per-run `--repo` target, independent of peers |
| finish-cleanup | `taskFinishCleanupChain` → `git push <peer>:<repo> <branch>:<branch>` | `finishCleanupServerTarget()` |

*(The worker-tick row is 2026-10-13: on a CLIENT host that keeps a worker and no
serve and no merger, nothing advanced `refs/heads/master` at all — see
`devdoc/integration-ref-cadence-on-worker-tick.md`. The pass is now reachable from
three ticks, and its `role` argument names which one fired it, so the staleness
alarm says who saw it.)*

Client pass = `GitSyncClientPass(repo, GitSyncServerTarget())`:
① `GitSyncPushLocalBranches` — push every local branch whose tip is not yet an
ancestor of server master (`gitPushbackPlan`: unmerged-only, no deleted-ref
resurrection), then ② `git pull --ff-only`.

**All of it is gated on `config.ResolveSyncPeers()`** (env `GOPI_SYNC_PEERS` >
settings.json `"sync_peers"`):

- `serveGitSyncSweep`: `if !config.HasSyncPeers() { return }` — line 59. Zero
  git, zero ssh, no throttle-clock advance.
- `GitSyncServerTarget()` → `""` when no peer ⇒ client pass returns
  `Skipped: "no sync peer (no server to push to)"`.
- `taskFinishCleanupChain` → `Skipped: "no sync_peers resolved — no server to
  push to"` (`internal/tools/task_finish_cleanup.go:1015`).
- `RunClaimLockProtocol` → `"single-node: no sync peers declared — local claim
  only"` (`cmd/agent/cli_tasks.go:1482`).
- `PushTerminalTaskSync` → `len(peers)==0` ⇒ `return nil` (row history never
  crosses; `internal/tools/memory_sync_claim.go:899`).

So on the client, `sync_peers` is not merely "the peer list" — it is the only
switch that turns HP→peerbox delivery on.

## 2. The defect: the topology was never re-declared

```
HP   ~/.gopi/settings.json (symlink → .ds_9_15_failure_settings.json)
     "sync_peers": [],  "memory_peers": ["hp"],  no "role", no "sync_to"
     env GOPI_SYNC_NODE=hp (identity tag, NOT a peer list)

peerbox ~/.gopi/settings.json
     "sync_peers": [],  "memory_peers": ["hp"],  no "role", no "sync_to"
```

`sync_peers: []` is not an accident — it is the deliberate **single-node shape**
of 2026-09-02 (`devdoc/singlenode-config.md`: "single-node = compute_nodes empty
AND sync_peers empty", with `serveGitSyncSweep` explicitly short-circuited).
That decision was right for one machine. Rule 28 (2026-09-16) re-split the work
across two hosts; the topology keys were never re-declared for the new division,
so HP kept the single-node shape while being used as a client.

`GOPI_SYNC_NODE=hp` does not help: it is read by exactly one place — the
self-exclusion inside `GitSyncServerTarget()` / the legacy hostname fallback of
`IsServerNode()`. It declares **who HP is**, never **where HP pushes**.

The designed client knob **`sync_to` / `GOPI_SYNC_TO`** is the intuitive
spelling of "this client reports to peerbox" — and **its value is read by
nothing**. `ResolveNodeRole()`'s `syncTo` half has two consumers in the whole
tree: the startup banner (`NodeIdentityLine`) and the task briefing
(`NodeIdentityBriefing`). It reaches no git/ssh/push path at all. Verified:
`GOPI_NODE_ROLE=client GOPI_SYNC_TO=peerbox gopi task push` still prints
`git-client: skipped: no sync peer`. Recorded as a follow-up in §6.

## 3. Evidence

**3a. Pre-fix live no-op** (HP, master `3db2c0e0`) — the exact user-visible
symptom:

```
$ gopi task push
git-client: skipped: no sync peer (no server to push to)

$ gopi task finish-cleanup --dry-run
  candidates=10 would-run=0 settled=6 refused=4
  # the 4 refusals: completed rows whose worktrees were hand-removed while the
  # branch push had been skipped — each one a branch that should have crossed
  # to peerbox at finish-cleanup time
```

**3b. Six HP branches absent from peerbox** (pre-fix, checked `refs/heads` AND
`refs/remotes/hp/*` on peerbox's repo): `feat/chat-lineedit-m1`,
`feat/web-m1-b1b2b3`, `feat/web-m1-b4b7`, `feat/web-m2-b5b8b9b12`,
`fix/standards-stylegate-master-red`, `feat/chat-m2-streaming`. Five are the
deliverables of **completed** rows (`01a0a97e-e39c…`, `-e3c0…`, `-e3e0…`,
`-e41f…`, `01a0a9aa-13ea…`) — reported done, never delivered. peerbox's ledger
does not hold those rows either (`gopi task show` → no such task), so the
deliverable existed only on HP.

**3c. Positive control** (throwaway clone + probe commit, against the shipping
settings):

```
$ gopi task push                        → git-client: skipped: no sync peer
$ GOPI_SYNC_PEERS=peerbox gopi task push  → pushed=[test/push-verify-3486370]
peerbox: refs/heads/test/push-verify-3486370 = 434bce2ced67…   ✅ (probe ref deleted after)
```

**3d. Real delivery** — with the peer declared, HP's six epic branches crossed
and peerbox still holds every one (`git rev-parse` on peerbox, refs/heads):

```
feat/chat-lineedit-m1 a2dc2c5e · feat/chat-m2-streaming 0ecda146 ·
feat/web-m1-b1b2b3 d145c847 · feat/web-m1-b4b7 9ebc9554 ·
feat/web-m2-b5b8b9b12 98fb2942 · fix/standards-stylegate-master-red a3dfb1a3
```

**3e. End-to-end with the CONFIG fix in place (not an env override).** A fresh
HP worktree branch `feat/hp-push-verify-194449` @ `9bcfa029`, created after the
settings change, pushed by plain `gopi task push`:

```
$ gopi task push
git-client: pushed=[… feat/hp-push-verify-194449 …]  pull:skipped (no tracking info)
peerbox: refs/heads/feat/hp-push-verify-194449 = 9bcfa0294179634badcbd3b829f6e25ded10bf7f ✅
```

(staging branch + worktree + the peerbox test ref removed after the proof.)

**3f. The other two channels the same key unblocks, both measured live:**

| channel | pre-fix | post-fix (live probe) |
|---|---|---|
| claim-lock (HP→peerbox lock leg) | never ran (`single-node: no sync peers`) | `action=proceed round=1 in 1.44s — reason "双锁齐"; claim-lock peerbox: row-not-found (uncontested)` |
| terminal row seal (`PushTerminalTaskSync`) | `len(peers)==0 ⇒ return nil` | `terminal push 01a0a97e-e39c…: OK` — peerbox now holds that row (`status: completed`) |

The claim-lock result matters because the 2026-09-01 "nick incident"
(`memory_sync_claim.go:190`) was exactly a peer-list self-reference: with
`sync_peers: ["peerbox"]` on HP the peer is the *other* node, and the probe shows
round 1 proceeds uncontested in 1.44s — no ALOHA yield, no unrunnable tasks.

**3g. What HP cannot verify about peerbox's *merge* pass — and the honest
reading.** peerbox's own `sync_peers` is still `[]`, so peerbox's serve tick does
not auto-`fetch hp`; the delivered refs reach `refs/heads` (measured, 3d/3e)
and peerbox's sweep sees them. Measured on peerbox after delivery:
`gopi task merger --status` → `/home/user/work/GoPi … pending: 7 branch(es) the
next sweep would merge` (the delivered branches, including `feat/chat-lineedit-m1`
and `fix/standards-stylegate-master-red`). `merge-base --is-ancestor` on peerbox
confirmed none of them is in peerbox master yet — the merge itself has not run.

That gap is **not** in this audit's scope: peerbox's fetch/merge policy is the
system layer (Rule 28 → peerbox). It is recorded as a follow-up in §6 rather than
diagnosed here.

## 4. The fix (APPLIED on HP, verified live)

Client side (HP) — `~/.gopi/settings.json`:

```jsonc
{
  "role": "client",
  "sync_to": "peerbox",
  "sync_peers": ["peerbox"],     // ← the switch that enables HP→peerbox delivery
  "memory_peers": ["hp"],      // unchanged (asset sync, decoupled 2026-09-02)
  "compute_nodes": []
}
```

- Backup of the previous file:
  `~/.gopi/settings.json.bak-pre-sync-peers-20260916-194334`
- `role: "client"` is the declared answer for `IsServerNode()`, so HP no longer
  falls to the hostname guess and can never take server-side integration rights
  (the fail-safe direction).
- `sync_to: "peerbox"` is set for **documentation value only** — nothing reads it
  (§2); it is written so the file states the topology the rest of the keys
  enforce.
- Peer-verb side effect (also a fix, not a regression): `gopi task sync --peer
  peerbox` / `--peer hp` were refused with "not in the sync_peers whitelist"; the
  whitelist now names the peer.

**peerbox side: no change required to receive.** Receiving is `git fetch` +
`AdoptionSweep`, and the delivered `refs/heads` entry is visible to peerbox's own
sweep (3g). The optional server-side counterpart — `"sync_peers": ["hp"]` on
peerbox — is what was removed for single-node; without it HP must push (which is
exactly what the fixed client does every pass) rather than peerbox pulling.

## 5. Not done (honest)

- **No code change.** The audit found no defect in the git channel itself; the
  defect was an undeclared topology. No test was added for that reason.
- **peerbox's fetch/merge of the delivered branches is not verified complete** —
  they sit unmerged on peerbox (3g). That is peerbox's sweep's job (Rule 28).
- The HP branches were NOT merged or deleted here; nothing was pushed to master;
  no worktree of another task was touched. The two proof artifacts (staging
  branch `feat/hp-push-verify-194449` and its peerbox ref) were removed.
- `devdoc/singlenode-config.md` still documents `sync_peers: []` as the shape —
  now stale for HP. Not edited here (docs-only follow-up, §6).

## 6. Follow-ups worth a task (found, not fixed)

1. **`sync_to` / `GOPI_SYNC_TO` has no delivery reader** — a declared,
   documented, banner-rendered knob whose value reaches no push path. Either it
   becomes the client's push target ("my `sync_to` IS my server") or the
   collision is documented. Same defect class Rule 21's env-reader gate exists
   to catch; it is invisible to that gate because the value *is* read (for the
   banner), just not by anything that delivers.
2. **The finish-cleanup refusal hides a delivery failure.** All four refusals in
   3a are `git status: chdir <gone worktree>` — the worktree was hand-removed
   while the push had been skipped, so the row looks "settled/refused for
   hygiene" rather than "branch never delivered". When `sync_peers` is empty on
   a host whose tasks claim deliverables, "single-node" silently converts every
   finish-cleanup into "nothing to hand off". A one-line warning there would
   have surfaced this on day one.
3. **peerbox's `sync_peers` decision** (fetch hp vs. rely on hp pushing) is a
   two-host topology choice for the system layer.

## Verification

- **Build**: `CGO_ENABLED=0 go build -o bin/gopi ./cmd/agent/` — not re-run; no
  code change. The binary audited is master-head `bin/gopi` (version
  `1817dd201087-dirty`, built 2026-09-16 19:41 on peerbox / 17:03 on HP).
- **Live acceptance evidence** (the checks that matter, all re-runnable):
  - `gopi task push` on HP now emits
    `git-client: pushed=[…] pull:…` instead of `skipped: no sync peer`.
  - `ssh peerbox git -C ~/work/GoPi rev-parse refs/heads/feat/hp-push-verify-194449`
    → `9bcfa029…` before cleanup (3e).
  - Six pre-existing HP epic branches present on peerbox `refs/heads` (3d).
  - Claim-lock probe: `action=proceed round=1, 1.44s` (3f).
  - Terminal-row probe: `OK`; `ssh peerbox gopi task show 01a0a97e-e39c…` →
    `status: completed` (3f).
- **Tests**: none added (no code change). The live probes were temporary files
  in this worktree and were deleted before commit; they are described in §3f so
  they can be recreated in minutes.
- **Commit**: see the branch tip of `docs/hp-push-topology-audit` (this file).
