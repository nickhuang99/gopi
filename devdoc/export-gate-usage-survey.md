<!-- doc-class: normative -->

> **Retired mechanism (2026-11-05).** The peer-sync channel surveyed here
> (`gopi memory sync --peer`, `gopi memsync`) was DELETED with the cross-node sync family. The
> survey's other channels — the ones that remain — are unaffected; read the peer-sync rows as
> history.

# Survey: export-gate usage + widened-gate blast radius (commit 5ac19ff)

Branch `feat/export-gate-wide`, commit 5ac19ff (verified on 2026-09-09/10).

## 1. The gate change (what 5ac19ff did)

`internal/tools/memory_sync.go`:

- `syncExportSourceFilter` constant: `BrewSourceTag` → `""`.
- `SyncExport` SQL: `WHERE sources LIKE %,brew,%` → `WHERE sources NOT LIKE %,node:%`
  (i.e. export every LOCAL distilled_qa row that is not a sync-imported row),
  plus an OPTIONAL `--source TAG` that narrows back to rows carrying that tag.
- Import side (`SyncImport`/`syncImportRows`) **unchanged** — already
  source-agnostic: identity = question; human-owned rows win
  (`syncQuestionOwnedQ`); garbage gate; imported rows stamped `node:<peer>`.

## 2. Every call site of the export gate (blast radius)

| Caller | File:line | Source override | Effect of widening |
|---|---|---|---|
| `SyncExport` (file channel) | cli_memsync.go:196 | `--source` flag → default "" | `memsync --export` now ships all local non-node rows |
| `ProceduralSyncExport` (proc channel) | cli_memsync.go:136 | independent gate, `proceduralSourceTag` default, separate table | **unaffected** — its own `WHERE LIKE %,procedural,%` |
| `MemoryAssetsPeerSession` (ssh peer) | memory_sync_assets.go:127 | none → wide default | `memory sync --peer` push leg now ships all local rows |
| `MemoryAssetsDryRun` | memory_sync_assets.go:170 | none → wide default | dry-run sizing now matches the real push |
| `MemoryAssetsServeRemote` (reply leg) | memory_sync_assets.go:281 | none → wide default | peer's reply carries all its local rows; still anti-reflow'd |

Anti-reflow is **still intact** at every site: the `NOT LIKE %,node:%` clause
means a row imported from any peer (tagged `node:<peer>`) never re-exports.
The widening adds local rows of *other* source tags (hermes-import, empty
source, custom one-off tags); it does not open a ping-pong path.

## 3. Behavioral blast radius (what actually changes)

- **QA knowledge rows only.** The task ledger channel (`--export-tasks`,
  SyncTaskExportDelta, memory_sync_delta.go) and procedural channel each have
  their own exporters with their own gates — untouched by 5ac19ff.
- **Peer-sync (`gopi memory sync --peer`)** and **file patch (`memsync
  --export/--import`)** now agree: both export the full local non-node set.
  Previously memsync was brew-only while the peer channel was already... brew
  only too; the new default makes both "everything local".
- **Import behavior is the safety net.** Since import is unique-only by
  question and refuses to overwrite human-owned questions, the wider export
  cannot clobber a peer's divergent rows. New rows only ever INSERT.
- **Reverse direction**: rows HP imports from peer1 carry `node:peer1`, so a
  later peer1→HP re-sync will see them as existing (question-identity probe)
  and skip. No loop, no duplicate fan-out across N peers.

## 4. Merge state + conflicts (2026-09-10 check)

- `feat/export-gate-wide` = 27da68a (base) + 5ac19ff + dfa69a5. Master has
  moved past 27da68a (now 8af415f) with unrelated uuid/session-dir cleanup
  work (no memory_sync.go edits).
- `git merge-tree` of master↔branch: 0 conflicts, no file overlap.
- Master still carries `syncExportSourceFilter = BrewSourceTag` — the wide
  default is NOT yet merged; it will land when the branch merges.

## 5. Tests (regression coverage shipped with 5ac19ff)

- `TestSyncExportLocalNonNode` (internal/tools): empty-source + brew +
  hermes-import rows all export; node:* stays home; `--source brew` narrows.
- `TestMemorySyncE2EBidirectional` (cmd/agent): adds a source-less row to
  prove the wide class travels at CLI level.
- Full suite on the branch: **all packages ok, 0 FAIL** (cmd/agent 115s).

## 6. Risk notes

- Import ownership probe: `syncQuestionOwnedQ` treats brew OR node:* as
  sync-managed (`syncRowIsSyncSourced`); only a purely-local (no brew/no
  node) owner makes the human-win refuse fire. Export excludes ANY row whose
  sources contain node:* regardless of brew — conservative anti-reflow.
- Volume: HP previously exported ~3 brew rows; after merge it will export
  ~1500 local rows to a peer that lacks them. Expected one-time cost, then
  converged.
