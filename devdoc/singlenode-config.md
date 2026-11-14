<!-- doc-class: normative -->

> **Retired mechanism (2026-11-05).** `cmd/agent/cli_tasks_sync_git.go` and `cli_tasks_sync_pass.go`
> were DELETED with the cross-node sync family. `serveGitSyncSweep` — the sweep those files hosted —
> is ALSO GONE: two of its legs were the row sync tick and the delta-sync switch, both deleted with
> the family, so it was a clock with nothing to run (`cmd/agent/cli_tasks.go:6378`,
> `cmd/agent/ref_lag_tick_clock.go:20`). `serveAdoptionSweep` — a LOCAL git operation — stays.
> Read the anchors below as history, and the claims as history too.

# Single-Node Configuration Shape (2026-09-02)

This doc defines the **single-node settings shape** and how gopi's runtime
reads it. It is the operator-facing companion to
`devdoc/singlenode-refactor-review-2026-09-02.md` (the code-review
classification) and `devdoc/single-node-convergence-20260902.md` (nick's
architecture decision).

## The config file

Settings live at `~/.gopi/settings.json` (symlinked to `.ds_settings.json` in
nick's profile; the `GOPI_SETTINGS` env var overrides the path). The keys that
define the runtime's *topology* — as opposed to model/API config — are:

```jsonc
{
  "api_key": "...",
  // ... model / vision / budget keys are orthogonal to topology ...

  "compute_nodes": [],   // dispatch whitelist — EMPTY = no remote dispatch
  "sync_peers":  [],     // memsync/git-channel peer whitelist — EMPTY = no peers
  // "role": "server"    // optional — see "role declaration" below
}
```

## What "single-node mode" means (the settings driver)

**Single-node = `compute_nodes` empty AND `sync_peers` empty.** A deployment
declares nothing to reach across: no ssh compute nodes to dispatch to, no sync
peers to exchange task rows or git branches with. The runtime is pure local:

| Machinery | Single-node behavior |
|-----------|---------------------|
| claim lock | **local CAS only** — no remote leg (no ssh spawn, no ALOHA yield). `config.HasSyncPeers()` is false → `runTaskRunOnce` prints `single-node: no sync peers declared — local claim only` and runs. The atomic pending→claimed CAS (防本地双跑) stays. |
| serve-tick git pass | **no-op** — `serveGitSyncSweep` short-circuits before any role/git/ssh work when `HasSyncPeers()` is false. |
| serve-tick dispatch | **no-op** — `ResolveComputeNodes` empty → `runDispatchOnce` returns `(false, nil)`, serve runs locally. |
| task sync / harvest / push | **manual only** — commands stay (one-off `task sync --peer X` for a rare operator); nothing auto-syncs. Peer verbs refuse undeclared names. |
| adoption / merge-backlog / merged-cleanup | **local branch integration** — peerbox merges its OWN Bond branches; adoption scans only local refs + `refs/remotes/origin`. |
| memory-assets sync (`gopi memsync`) | **untouched** — file-based, peer-free; a distinct feature that stays. |

## Two shapes on this machine

**Prior (the 14:39 stale-branch-flood source)** — `sync_peers: ["hp"]` made the
server declare HP as a peer, so the git channel's fetch/push-back and the
claim broadcast remote leg were ACTIVE even though the architecture decided
single-node:

```jsonc
{ "compute_nodes": [], "sync_peers": ["hp"] }   // ← cross-node machinery ON (wrong)
```

**Now (single-node, the deliverable)**:

```jsonc
{ "compute_nodes": [], "sync_peers": [], "memory_peers": ["hp"] }  // ← single-node task mode, asset sync kept
```

The refactor **gates the cross-node machinery on these keys** — empty peers =
no remote work, regardless of which hostname runs gopi. The code review found
the resolve-level no-op paths were already correct
(`StartClaimLockBroadcast` / `PushTerminalTaskSync` return Proceed/vacuous on
empty peers); the gap was that `sync_peers: ["hp"]` kept that machinery
*enabled*. Cleaning the key + adding the top-of-pass `HasSyncPeers()` gates
makes single-node the enforced default.

**Exception — `memory_peers` (bugfix 2026-09-02)**: the distilled_qa
memory-asset channel validates against its OWN decoupled key `memory_peers`, NOT
`sync_peers`. Two doors read that key: the MANUAL session `gopi memory sync
--peer <alias>` (deleted by `5677cc6e` on 2026-11-05, restored the next day) and
the one-way copy `gopi memory backup --peer` — the cross-node feature that made
the decoupling necessary is the session, and the copy alone would have justified
the same key. Single-node task mode therefore ships
`sync_peers: []` + `memory_peers: ["hp"]` — task sync stripped, knowledge
sync available. See `devdoc/bugfix-memory-peer-whitelist-2026-09-02.md`.

## "role": declare it, don't guess

`role` is optional (`server` | `client`). When **absent**, gopi's integration
machinery (adoption, merge-backlog, merged-cleanup) is role-INDEPENDENT and
runs on any node — which single-node peerbox relies on to merge its own Bond
branches. When a role IS declared, it wins:
`config.ResolveNodeRole()` (env `GOPI_NODE_ROLE` > settings `role`) is consulted
by `memory_sync_delta.IsServerNode()` BEFORE the legacy hostname-against-peers
guess (bug044 — a hostname-guessed "server" let an undeclared host claim
integration rights; the N1 fix defaults unknown hosts to CLIENT).

**Recommendation**: set `"role": "server"` on the single-node integration
machine so `IsServerNode()` has a declared answer and never falls back to the
hostname guess. (Deferred in this refactor to keep the settings diff minimal —
the code already honors a declared role if present.)

## Where the gates live (file:line)

- `internal/config/config.go` — `HasSyncPeers()` (single go/no-go), driven by
  `ResolveSyncPeers` (env `GOPI_SYNC_PEERS` > settings `sync_peers`).
- `cmd/agent/cli_tasks.go` — single-node claim gate (skips the remote lock
  protocol when no peers).
- `cmd/agent/cli_tasks_sync_git.go` — `serveGitSyncSweep` short-circuits when
  `!HasSyncPeers()`.
- `cmd/agent/dispatch.go` — `runDispatchOnce` no-ops when `ResolveComputeNodes`
  is empty (existing).
- `internal/tools/memory_sync_claim.go` — the resolve-level no-op fallbacks
  (Proceed / vacuous success on empty peers) stay as the second line of defense.

## Restoring cooperation mode (future)

To re-enter cooperation mode, DECLARE it: add a peer to `sync_peers` and/or a
compute node to `compute_nodes` (and/or a peer to `memory_peers` for the
knowledge-asset channel alone). The machinery is code-gated on these keys —
it is not deleted, just dormant while they are empty. No rebuild needed for a
topology-key change (settings are read at resolve time).
