<!-- doc-class: historical -->
# Bugfix: memory-asset sync blocked by single-node settings — memory_peers decoupling

Date: 2026-09-02 · Branch: `feat/mem-peer-whitelist` · Epic: BUG/DESIGN FIX —
`gopi memory sync --peer hp` is blocked by the single-node settings.

## The bug

The single-node convergence refactor (t-1788332711) cleared `settings.json`:
`sync_peers=[]` + `compute_nodes=[]` — task-state sync, claim broadcast and
the git channel are all correctly stripped. BUT the memory-asset sync
(`gopi memory sync --peer hp`, t-1788307190, merged 1cf3690) — the one
cross-node feature nick explicitly WANTS to keep ("memory_store/search is
for pitfall/lesson/experience, this db sync is important") — validated its
peer against the SAME `sync_peers` whitelist. With `sync_peers=[]` it
refused:

    gopi memory sync: memsync assets: peer "hp" is not in the sync_peers whitelist (settings.json "sync_peers")

So the single-node config that disables task sync ALSO disabled asset sync.

## The fix (option 1: separate whitelist — decoupled, same philosophy)

A new settings key **`memory_peers`** declares the ASSET-only peer topology:

- `gopi memory sync --peer <alias>` validates against `memory_peers`
  (`config.ResolveMemoryPeers`, env `GOPI_MEMORY_PEERS` > settings
  `memory_peers`).
- Task machinery (`task sync` / claim broadcast / git channel) keeps
  validating against `sync_peers` — untouched.
- Single-node shape: `sync_peers=[]` (no task sync) + `memory_peers=["hp"]`
  (asset sync available). ALL cross-node contact stays whitelisted; only
  the key differs.

Rationale for option 1 over dropping the whitelist (option 2): consistency —
a knowledge pull to an undeclared peer is low-risk, but the declared-topology
philosophy (topology is DECLARED, never discovered) is the same across every
cross-node channel; a bare `--peer ssh alias` would silently break that
invariant and future channels would copy the loophole.

### File domains touched

| File | Change |
|---|---|
| `internal/config/config.go` | `Config.MemoryPeers []string` json `memory_peers`; `MemoryPeersSettingsKey`/`MemoryPeersEnv`; `ResolveMemoryPeers()`; `HasMemoryPeers()`; `normalizePeerList(what, peers)` shared by both whitelists |
| `internal/config/settings.go` | EnvVars catalog entry for `GOPI_MEMORY_PEERS` |
| `internal/config/help.go` | New "Memory-asset sync" help section |
| `internal/tools/memory_sync_assets.go` | `syncAssetsPeerWhitelisted` → `config.ResolveMemoryPeers`; error names `memory_peers`; `cliPeers` param now ignored (documented) |
| `cmd/agent/cli_memory_sync.go` | usage text: memory_peers |
| `~/.gopi/settings.json` (symlink → `.ds_settings.json`) | `sync_peers: []` + `memory_peers: ["hp"]`; backup `settings.json.bak-mempeer-20260902` |

NOT touched (task machinery stays on sync_peers — regression-protected):
`memory_sync_session.go` (syncPeerSession/syncPeerDeclared),
`memory_sync_claim.go`, `cli_tasks_sync.go`, `singlenode_*` gates.

## Also fixed: the memory sync session's stale-peer leg

The remote leg of memory sync needs the PEER to run a current binary (the
ssh call is `gopi memory sync --serve-assets`; an outdated peer gopi errors
`unknown flag --serve-assets`). The sync session now detects this
(`syncAssetsStalePeerHint` matches the go-flag "unknown flag --serve-assets"
/ "flag provided but not defined: -serve-assets" stderr shapes) and prints an
actionable upgrade hint — pull master on the peer + rebuild `bin/gopi` —
instead of just the raw ssh error.

## Verification (acceptance, machine-checkable)

1. `sync_peers=[]` + `memory_peers=["hp"]`: `gopi memory sync --peer hp`
   runs (bidirectional asset sync); `gopi task sync --peer hp` still
   refuses (no task peers). **Verified live.**
2. `memory_peers=[]`: memory sync refuses with the whitelist message.
   **Test: `TestMemoryAssetsPeerWhitelistDecoupled` + `TestMemorySyncWhitelistRefusal`.**
3. Stale-peer binary produces the upgrade hint.
   **Test: `TestMemoryAssetsStalePeerHint` + `TestMemorySyncStalePeerUpgradeHint`.**
4. `go test ./internal/tools/ ./cmd/agent/` green.
5. devdoc committed.

## Tests added

- `internal/tools/memory_sync_assets_test.go`:
  `TestMemoryAssetsPeerWhitelistDecoupled` (① empty memory_peers refuses
  even with sync_peers declared; ② sync_peers=[] + memory_peers=[hp] runs —
  the bugfix scenario; ③ undeclared asset peer refused),
  `TestMemoryAssetsStalePeerHint` (detector + hint unit test).
- `internal/config/singlenode_gate_test.go`: `TestResolveMemoryPeers*`
  (empty / from settings / env override) + decoupling invariant
  (HasSyncPeers false + HasMemoryPeers true) + `TestNormalizePeerListRejectsDuplicates`.
- `cmd/agent/cli_memory_sync_test.go`: usage test now asserts `memory_peers`;
  E2E runs with `GOPI_MEMORY_PEERS=hp` + `GOPI_SYNC_PEERS=` (single-node
  shape) and must still converge; `TestMemorySyncWhitelistRefusal`;
  `TestMemorySyncStalePeerUpgradeHint` (fake ssh answers like an outdated
  peer → initiator error carries pull-master+rebuild hint).

## Regression evidence (task sync unchanged)

- `TestServeGitSyncSweepNoPeerIsPureNoOp` PASS (single-node serve gate).
- Task-sync suite (`TestTaskSync*`) PASS — all still validate against
  sync_peers.
- Live: `gopi task sync --peer hp` with the new settings refuses:
  `peer "hp" is not in the sync_peers whitelist`.

## Settings state (peerbox, after cleanup)

    compute_nodes: []
    sync_peers:    []          ← single-node task mode
    memory_peers:  ["hp"]      ← asset pull available (the kept feature)

HP side: `memory_peers=["peerbox"]` if it should pull back.
