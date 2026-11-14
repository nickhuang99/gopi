<!-- doc-class: historical -->
# Task Report: Backup gopi database (peerbox) to HP — exploration + operation

## Summary

Goal: "backup gopi database to HP — find out gopi existing tools/functions
(maybe gopi CLI already has such feature) to sync database content to HP
counterpart, push unique records in peerbox to HP's memory.db — explore and
operate, not rebuild the wheel."

## Completed

### 1. Exploration verdict — what gopi's existing tools DO carry (and don't)

No whole-DB file-transfer verb exists in the gopi CLI (`gopi --help`, all
subcommand helps, code search — no backup/restore/VACUUM-INTO verb). What
IS designed for cross-node convergence, and its exact scope:

| Verb | Channel scope | Result this session |
|---|---|---|
| `gopi task sync --peer hp [--full]` | **whole task ledger** — superset replay, push+pull | **fully converged live**: pushed=1530+2, imported=55, both sides now **1585 tasks** |
| `gopi memory sync --peer hp` | **distilled_qa rows tagged `brew` only** (by design, see §Known limitations 6 of devdoc/memory-sync-assets-2026-09-02.md) | converged: 0 pushed, HP's 3 brew rows already present on peerbox |
| `gopi memsync --export/--import [--source TAG]` | same brew-gated knowledge channel via file patch (`--source` LIKE-filter `%,TAG,%`; `--source ""` re-mapped to brew) | brew=2 rows only; **cannot select the empty-source / one-off-tag rows** |

Data measured (2026-09-09, both live DBs):
- peerbox distilled_qa = **1482** rows: brew=2 (both node-imports), node-import=9,
  non-brew=1471 (459 empty-source + legacy bulk imports
  `distilled.db导入…`/`hermes-skills-import`).
- HP distilled_qa = 1213. Question-set overlap: **402 peerbox-unique questions
  missing on HP**, carrying ~200 distinct one-off source tags (mostly empty
  source — unselectable by the `LIKE '%,x,%'` export filter).
- **The non-brew rows are local-only BY DOCUMENTED DESIGN** (both channels
  agree: devdoc/memory-sync-assets-2026-09-02.md §Known limitations 6 —
  "A row stored without a source (or with a non-brew custom source) is
  local-only and never exported by either channel"). So the memory-sync
  channel cannot and is not meant to push the 402-row remainder.

### 2. Operation — the actual backup

Live convergence (designed verbs, one-shot peer env as settings declare
single-node topology):
- `GOPI_SYNC_PEERS=hp ./bin/gopi task sync --peer hp` → task ledger superset
  replayed to HP (pushed=1530, imported=55 HP-unique rows back, anchor moved).
- `./bin/gopi memory sync --peer hp` → brew asset channel converged
  (HP's 3 brew rows pulled to peerbox earlier; 0 new).
- Re-run at close: `pushed=2` (rows created during this session) with 0
  imports = both channels converged.

Full-DB snapshot backup (covers the by-design-local-only remainder — this is
**the database backup the epic asks for**):
- SQLite **online backup API** (`src.backup(dst)` under Python sqlite3 —
  consistent copy, safe under concurrent gopi use, no gopi downtime):
  peerbox `~/.gopi/memory.db` → snapshot, then `scp` to HP.
- Snapshot: `hp:~/.gopi/backups/memory.db.peerbox-backup-20260909-035923`
  (42,115,072 bytes), **verified table counts on HP**:
  distilled_qa=1482, tasks=1585, chats=82, procedural=2,
  session_conclusions=308 — identical to peerbox live. Zero data loss path:
  the snapshot carries every peerbox row the live channels exclude.

### Final state

| Store | peerbox | HP live | HP snapshot backup |
|---|---|---|---|
| tasks | 1585 | 1585 (converged live) | 1585 |
| distilled_qa | 1482 | 1213 (brew-shared converged; non-brew local by design) | **1482 (full)** |
| procedural / chats / conclusions | … | partial live | full copy |

## Not Done

- The 402 peerbox-unique non-brew knowledge rows were NOT pushed into HP's
  **live** distilled_qa — impossible by documented design (local-only,
  deliberately, in both sync channels; no `--full`/filter-lift flag on
  `memory sync`, and the memsync LIKE-filter cannot select empty-source
  rows). If a live-merge of those rows is ever wanted, it needs a design
  decision (a new channel or a filter-lift flag) — out of scope here. The
  snapshot backup covers them fully for disaster-recovery purposes.

## Verification

- Live task ledger both sides: 1585 = 1585 (`gopi task sync` re-run: pushed=2,
  imported=0 → converged).
- Memory asset channel: converged (pushed=0, existing=3 → no divergence).
- Snapshot integrity on HP: sqlite3 open OK + per-table counts match peerbox
  live (distilled_qa 1482, tasks 1585, chats 82, procedural 2,
  session_conclusions 308).
- Commit: N/A — exploration/operation task, no code changed.
- Report: `devdoc/backup-peerbox-db-to-hp-20260909.md`

## Recovery commands

```bash
# Restore the full peerbox DB snapshot (currently on HP) to a peerbox DB:
scp hp:~/.gopi/backups/memory.db.peerbox-backup-20260909-035923 ~/.gopi/memory.db.restored
# (point gopi at it with --db, or swap in after stopping gopi)
```
