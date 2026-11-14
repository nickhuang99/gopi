<!-- doc-class: historical -->
# Task Report: legacy-audit batch-08 — the STILL-NEEDED rerun

## Completed

- Read the batch-08 id list out of
  `~/.gopi/legacy-audit-batches/batch-08.tsv` on HP (reached as `ssh hp` =
  10.0.0.5; the briefing's `nick@10.0.0.6` is peerbox itself): 162 raw
  lines = 1 header + 161 non-blank lines = **160 data rows / 160 unique ids**
  (multiline payloads — the raw line count is not the id count; the id set was
  taken as the first TSV field matching `^t-[0-9]`).
- Re-established the missing audit DB at its documented path
  `~/.gopi/memory-glm-audit.db` from the named pre-batch-08 backup
  `~/.gopi/backups/memory-glm-audit.db.bak-batch08-20260910-051750` (851 rows — exactly the
  post-batch-07 figure log-07's hand-off records, so the baseline is proven).
- Took a pre-write snapshot `~/.gopi/backups/memory-glm-audit.db.bak-b08-run-20260919-094611`
  (chmod 444) — the backup every DELETE in this batch names.
- Audited all 160 ids against `~/.gopi/brief-glm-night-legacy-audit.md`
  (DELETE stale / UPDATE-tag valuable-and-unfinished / MIGRATE valid-unfinished /
  KEEP valuable-done), judging "already done by a later task?" against the
  **peerbox mirror** (`~/.gopi/peerbox-mirror/memory.db`, 160/160 ids hit, status
  identical to the audit DB row-for-row) plus **git evidence** from
  `~/work/GoPi` — never from the HP ledger alone.
- Applied the decisions in one transaction: 50 DELETE / 9 UPDATE (reason tagged
  `[glm-audit:b08:*]`) / 101 KEEP. Row count 851 -> 801 (delta = DELETE count).
- Wrote `~/.gopi/legacy-audit-log-08.md` on HP — one line per batch-08 id with
  its verdict and reason — in the batch-09 log format (总览 / 逐条记录 /
  证据清单 / 交接). Verified 160/160 ids appear exactly once.
- Decision table left on HP at `~/.gopi/legacy-audit-decisions/decide-08.tsv`
  + `decide-08-delete.txt` (the DELETE list with its reasons).
- Copies of the log and the decision table ride this branch as
  `devdoc/legacy-audit-batch08-log.md` and
  `devdoc/legacy-audit-batch08-decisions.tsv`.

## Not Done

- **No MIGRATE.** The brief's MIGRATE class is for cancelled/failed work whose
  intent is still valid *and* self-contained enough to re-submit under a new
  uuid. Six such rows exist and are the honest MIGRATE candidates
  (t-1788305179, t-1788307568's sibling t-1788307558, t-1788329788,
  t-1788511058-3, t-1788548312, t-1788551878). This run tagged them
  UPDATE/`U-open` rather than MIGRATE because creating successor task rows is
  outside the blast radius this briefing authorised, and because each is
  *already* a live task row that its owner can re-dispatch (Rule 26). If the
  operator wants real successor uuids, that is a separate, explicitly-scoped
  pass — these six are named and ready for it.
- **batch-08's own partial writes, if any, are unrecoverable.** There is no
  post-batch-08 backup and no batch-08 marker inside the DB, so nothing can be
  said about what the interrupted run did before it died. This rerun does not
  claim to continue it.
- No peerbox production ledger (`~/.gopi/memory.db`) write, no HP ledger write, no
  task row mutated, no host-level config touched, no other agent's
  branch/worktree touched.

## Verification

- Build: not run — this change is docs/data only (2 markdown/TSV files under
  `devdoc/`); no `.go` file is touched.
- Tests: see `## Evidence (self-filed)` for the exact commands run.
- Branch: `feat/legacy-audit-batch08` (worktree
  `/home/user/workspace/gopi-legacy-b08`, base `fa12d97f`).

## Evidence (self-filed)

Host: peerbox drives; all DB/log work executes on HP over `ssh hp`.

**Deterministic verification (sqlite3, re-runnable)**
- `PRAGMA integrity_check` on the rebuilt DB -> `ok`
- before `select count(*) from tasks` -> `851`; after -> `801`; delta `50` = DELETE count
- `t-` rows: `530` -> `480`
- KEEP+UPDATE ids still present: `keep_update_present=110 / 110`
- DELETE ids gone: `delete_absent=0 (expect 0)` of 50
- deleted-id diff `comm -23 (before, after)` = 50 rows, `not_in_batch08=0` (no cross-batch damage)
- added rows `comm -13` = `0`
- UPDATE-tagged rows queryable: `select count(*) ... reason like '[glm-audit:b08:%'` -> `9`
- untouched-row spot check: `t-1788531677` -> `completed` unchanged

**Outside evidence (not the HP ledger)**
- peerbox mirror: 160/160 batch-08 ids present, status/source identical to the audit DB
- mirror originals for every D-echo-mirror row -> `completed`
- mirror check for the two retained mirror rows: `t-1788468200` -> `cancelled` (deleted by batch-05 D2),
  `t-de-resume` -> already deleted by batch-06 D1 — hence KEEP, not DELETE
- git (`git log --all`): bug056 `717a1164`; `-h` P0-1 fix present at `cmd/agent/main.go:121`;
  stuck-polling `devdoc/bugfix-stuck-polling-20260902.md`; SIGPIPE `58f61bcd`/`3495262e`;
  M4.19 report `4d0003ff`; M3 brew `c3d77d60`+`2c1acde0`; M4.6/7 `a4ba5cc2`+`05f55d73`;
  serve-tick merge `6e6a7d81`/`680b0ab4`; CLI review `devdoc/review-cli-config-options.md`

**Honesty note carried into the log** — the live audit DB was absent at run time;
the verdict rests on `~/.gopi/backups/memory-glm-audit.db.bak-batch08-20260910-051750`, which
makes this a full 160-id RERUN from a proven pre-batch-08 baseline, not a
continuation of the interrupted run.
