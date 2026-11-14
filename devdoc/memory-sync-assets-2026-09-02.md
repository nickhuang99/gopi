<!-- doc-class: historical -->
# Memory-Asset Sync — cross-node distilled_qa channel

Date: 2026-09-02 · Branch: `feat/memory-assets-sync` · Epic: cross-node
memory-asset sync (`gopi memory sync --peer <alias>`, unique-only,
low-hanging-fruit). Export gate widened 2026-09-09 (feat/export-gate-wide,
commit 5ac19ff): see Known limitation 6.

## What this is

The db-sync story previously covered ONLY the task ledger (`gopi task sync
--peer`, M4.13 timestamp-delta). The knowledge rows `memory_store` writes —
the distilled_qa brew/lessons that `memory_search` reads — were a blind
spot: peerbox and HP each accumulated their own brew rows and neither could
retrieve the other's. This feature closes that gap with a peer verb:

    gopi memory sync --peer hp        # one invocation converges both stores

SYNC SEMANTICS = **unique-only superset merge** (conservative by design):

- identity = the **question** (exact match — the MemoryStore exact-dedup
  probe, question[:200] + topic for long questions).
- a row the PEER lacks is copied over (stored through the local MemoryStore
  write path);
- a row present on BOTH sides (same question) is **never touched** — content
  divergence is the out-of-scope conflict zone. No newer-wins, no merge, no
  conflict tag. Counted `existing`.
- **bidirectional one-shot**: ONE invocation pushes local-unique rows AND
  pulls peer-unique rows (the peer's reply export), so a single
  `gopi memory sync --peer hp` from either side converges both.
- **insert-only**: existing rows are never updated/overwritten.
- **provenance**: every imported row carries `node:<peer>` in sources.
- **anti-reflow**: imported rows (node:* tagged) never re-export — no
  ping-pong loops.
- **idempotent**: re-running after convergence imports ~0 (each side's
  re-export finds everything already present → `existing`).

## Semantics table

| Row class | Identity on BOTH sides? | Action | Counter |
|---|---|---|---|
| peer lacks it | no | stored through MemoryStore (dedup + garbage gate + FTS + node:<peer>) | `imported` |
| same question, any content | yes | NEVER touched (divergence = out-of-scope conflict) | `existing` |
| near-duplicate question | similar (trigram) | merged into existing by MemoryStore | `deduped` |
| garbage / empty fields | — | refused per-row (garbage gate / validation) | `refused` |

Out of scope by design: reconciling divergent same-question copies,
newer-wins, conflict tags, anchors/timestamps. Task-ledger semantics
(newer-updated_at wins, terminal-never-regresses, anchor windows) do NOT
apply here — this channel is insert-only assets, not mutable queue state.

## Command surface

    gopi memory sync --peer <alias> [--db PATH] [--dry-run]
        One bidirectional unique-only session with a whitelisted peer.
    gopi memory sync --serve-assets [--db PATH]
        PEER side (ssh'd by an initiator): stdin = initiator export,
        stdout = this node's reply export. Never run by a human.
    gopi memory sync -h
        Self-documented help (flags + semantics).

Flags: `--peer` (required for a session; must be in the sync_peers
whitelist — settings.json `sync_peers` / `GOPI_SYNC_PEERS`, declared never
discovered), `--db` (knowledge base override), `--dry-run` (report the
local push without the ssh leg). Exit 0 on success (including full
convergence: 0 imported), 1 on any failure.

Output (one line + per-row refusals to stderr):

    sync hp: pushed=3 pull{imported=1 existing=2 deduped=0 refused=0} (1.2s)

## Transport

`ssh <peer>` direct with the patch on stdin/stdout — the task-sync
serve-remote shape, NO intermediate patch files in the happy path:

1. initiator exports its brew rows (SyncExport — the SAME gate memsync
   --export uses: sources carry `brew`, no `node:*`), stamps `node:<self>`;
2. pipes the JSON patch to `ssh <peer> gopi memory sync --serve-assets`;
3. the peer merges it (below) then replies on stdout with ITS export;
4. the initiator merges the reply the same way.

One ssh round trip, both directions, no helper files, `BatchMode=yes`,
15s whole-exchange bound (`memoryAssetSyncSSHTimeout`), `ConnectTimeout=10`
(the existing task-sync constants).

## Reuse map (nothing reinvented)

| Piece | Where | Used for |
|---|---|---|
| SyncExport / SyncPatch / SyncRow | internal/tools/memory_sync.go | wire format + the brew-source export gate (anti-reflow) |
| memoryStoreRow (store core) | internal/tools/memory.go | the atomic write path on import: dedup + garbage gate + FTS + provenance |
| syncCheckPatchVersion | memory_sync.go | version gate on both the inbound patch and the peer reply |
| syncSSHExchangeHook seam | memory_sync_session.go | in-process test routing of the ssh leg |
| ssh argv shape + constants | memory_sync_task.go / memory_sync_claim_ssh.go | BatchMode/ConnectTimeout/timeout, one remote-command element |
| config.ResolveSyncPeers | internal/config/config.go | the peer whitelist (settings/env) |
| bidir-node E2E pattern | memory_sync_bidir2_e2e_test.go | two real DBs + real export/merge primitives |
| memsync import txn contract | memory_sync.go (syncImportRows) | whole-patch BEGIN IMMEDIATE all-or-nothing |

New files: `internal/tools/memory_sync_assets.go` (engine),
`internal/tools/memory_sync_assets_test.go` (unit sim),
`cmd/agent/cli_memory_sync.go` (CLI), `cmd/agent/cli_memory_sync_test.go`
(CLI + binary E2E), plus a one-line pointer in `cli_memory.go`'s family
usage and a dispatch hook in `cmd/agent/main.go`.

## Design notes (resolutions in favor of existing mechanisms)

- **Anti-reflow** reuses the memsync export gate verbatim: rows whose
  sources carry a `node:*` tag never export. Rows imported from peer X
  carry `node:X` — so they stay home on every later export. This is
  stronger than the task channel's `from-<peer>` gate (which only excludes
  the specific peer) and matches the assets' file-channel precedent
  (memory_sync.go: "sync never re-exports rows imported from another
  node"). No new tag vocabulary.
- **Import uses the EXACT memsync atomic path**: one transaction, BEGIN
  IMMEDIATE (openDB's _txlock), every accepted row lands or none does.
  The difference from SyncImport is only the identity probe order: the
  file channel probes "owned by a non-sync row?" (human-wins refusal),
  the assets channel probes "question exists AT ALL?" and skips — the
  unique-only superset rule has no human-wins carve-out because a
  same-question row is by definition already shared.
- **No anchors / no sync_state rows.** Task sync needs anchors because its
  rows MUTATE (status flips, newer-wins) and deltas must be bounded.
  Assets are append-mostly and the channel is insert-only — the "delta"
  is just "what the peer lacks", which is unknowable without asking, and
  re-sending the full owned set is cheap (hundreds of rows) and idempotent
  (dedup). Anchor bookkeeping would add state for zero convergence gain.
- **The exact-question probe mirrors the MemoryStore dedup key width**
  (question[:200]) so the probe and the store agree on identity — a long
  question (200+) that the store would treat as new can't be wrongly
  skipped. The stored column keeps the full text; both compare truncated.

## Verification

- Unit sim (`TestMemoryAssetsUniqueOnlySuperset`, `...ProvenanceAndAntiReflow`,
  `...IdenticalRowsExistingSkipped`, `...ImportRejectsGarbage`,
  `...ServeRemoteRoundTrip`): two real temp MemoryStore DBs through the real
  engine with the ssh leg routed in-process (the established bidir seam).
  A{1,2} B{2-divergent,3} → both {1,2,3}, row 2 untouched per side, second
  sync imports 0; provenance node:<peer>; anti-reflow (imported rows never
  re-export).
- Binary E2E (`TestMemorySyncE2EBidirectional`): the real gopi CLI against
  two real temp DBs with a fake `ssh` on PATH exec'ing the peer-side
  handler — the full flag→engine→ssh-argv→serve-assets→merge path.
- `go build ./...`, `go vet`, `go test ./internal/tools/ ./cmd/agent/` all
  green.

## Known limitations

1. Divergent same-question rows are intentionally NOT reconciled — the
   conflict zone is out of scope (each side keeps its own copy; later
   identical re-brews on one side never overwrite the other).
2. Similarity-dedup (trigram) can still merge a near-duplicate question
   into an existing row on the receiving side (`deduped`) — that is the
   MemoryStore's established behavior for every write path, not a sync
   decision. The unique-only guarantee is exact-question identity.
3. Only distilled_qa rows travel (brew source). The procedural rows
   (M4.10 memory_sync_procedural.go) are a documented FOLLOW-UP — the
   export-proc/import-proc file path already exists, but folding it into
   this ssh session was deliberately out of this task's scope (stretch
   guard: "IF near-free — otherwise mark procedural as follow-up").
4. The initiator always re-sends its whole owned set (no per-peer
   "what do you lack" filtering) — correct and idempotent, but on large
   stores the wire carries duplicates the receiver skips. Optimization
   (peer echo of held questions) is future work if stores grow large.
5. No serve-tick automation: `memory sync` is a manual verb, like the
   task sync session before M4.19 wired ticks. GOPI_SYNC_INTERVAL covers
   task-ledger sync only.
6. [SUPERSEDED 2026-09-09, feat/export-gate-wide] The export scope was
   originally brew-only (SyncExport's `syncExportSourceFilter`). The wide
   gate (commit 5ac19ff) now exports EVERY local row whose sources carry no
   `node:*` tag — brew, hermes-import, and source-less early-dev rows all
   travel, so `memory sync` and `memsync --export` agree on a wider shared
   asset set. Anti-reflow is unchanged: any `node:*` row never re-exports.
   An explicit `--source TAG` narrows back to one tag (e.g. the historical
   brew-only scope). The garbage gate and similarity dedup still apply to
   what does travel.
