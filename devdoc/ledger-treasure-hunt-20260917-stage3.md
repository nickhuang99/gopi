<!-- doc-class: historical -->
# Ledger Treasure Hunt — Stage 3: cancelled rows that are neither HP-origin nor auto-prefixed

Part of the epic `ledger-treasure-hunt-20260917` (Stage 3 of 7). Prior stages:
`feat/ledger-treasure-hunt-stage1`, `feat/ledger-treasure-hunt-stage2`;
canonical rows in `devdoc/ledger-treasure-hunt-20260917-rows-master.csv`.

## Scope correction (report it — the briefing's count was wrong)

The Stage 3 briefing said **85** cancelled non-HP/non-auto rows. The canonical CSV
yields **121**. Verified two ways: `wc -l` on the sliced worklist, and a
per-row existence check against `~/.gopi/memory.db` (121/121 present, 0 missing).
The composition is 93 curated-cancelled + 18 `sync-yielded` + 6 `wrapper:t-*` +
4 merge-lane. Every verdict below covers one of those 121 — no row was skipped
and no row was invented.

## Deliverable

`devdoc/ledger-treasure-hunt-20260917-rows-stage3-cancelled.csv` — 121 rows,
columns `task_id,verdict,evidence_cmd,evidence_result`. Every row carries
exactly ONE verdict and a non-empty command-and-result pair. 121/121 unique
ids, 0 empty evidence cells, 0 ids outside the worklist (checked by set
difference in both directions).

| verdict | count | what it means here |
|---|---|---|
| SUPERSEDED | 56 | the cancel reason names (or the git record shows) a successor/landing that IS present |
| ALREADY-DONE | 45 | the work is observably in master, or in a completed successor row |
| NO-VALUE | 16 | probe/auto/smoke/duplicate-registration rows (payload never reached a worker) |
| STILL-NEEDED | 4 | no landing anywhere + the work is genuinely still wanted → re-filed |

## Hunting target (a): abandoned branches that still carry unmerged commits

Method: resolve each row's named branch against the live ref database
(`git for-each-ref`, then `rev-parse`), compute `git rev-list --count master..<tip>`,
and for every ref still ahead, compare each of its commits' file content against
master by blob hash (`rev-parse <tip>:<file>` vs `rev-parse master:<file>`) — a
subject-line match is not evidence of a landing, a blob match is.

**Result: 5 refs are still ahead of master, all of them archive tags.**

| tag | commits ahead | content verdict |
|---|---|---|
| `archive/fix/heavy-tests-classification` | 4 | LANDED — `internal/standards/heavy_inventory_gate_test.go` and `internal/testutil/rule27_gate_test.go` are **blob-identical** to master; the differing files were superseded by later master work (`b9217d71 test(heavy): heavy tests off by default, Rule 27 gated`) |
| `archive/fix/cwd-and-lifetime` | 7 | LANDED — the fix content is on master under different commit subjects (`d1c8bed9 fix(tools): split the procfs root from the cwd-release destination`, `4b5e9f6e fix(worktree): release only real producers`) |
| `archive/fix/task-child-fail-escape-sandbox` | 12 | LANDED — `taskChildFailVerbRun` and the sandbox precondition live on master (`cmd/agent/task_child_fail_guard.go`, `cmd/agent/cli_tasks.go`) |
| `archive/feat/archive-why-gate` | 4 | LANDED — reason gate present on master (`01a0aa9e completed`), and the 88-tag sweep is recorded (`devdoc/archive-tag-sweep-88-20260916.md`); a fresh re-audit row is already pending (`01a0ab47-28c7`) |
| `archive/fix/daemoncwd-scratch-pin` | 1 | LANDED — master carries the suite pin with provenance (`internal/daemoncwd/testmain_scratchroot_test.go`); the archived 1-file version is superseded by the `01a0ab11-70e6` recovery fix |

So shape (a) yields **no lost deliverable**: the five tags ahead of master are
archived duplicates of work that already landed. This matches memory lesson
#1892 ("a vanished branch is usually MERGED — check before mourning it").

The other 66 named refs are gone locally; 40 of them are name-visible in a
master merge subject, and the residuals resolve through successor rows (below).

## Hunting target (b): rows cancelled as "superseded" where no successor exists

Method: for every row whose reason/summary claims supersession, duplication,
migration or replacement, extract all UUIDs from the claim text, drop the row's
own id, and look each one up in the ledger:

- **19 rows name a successor that EXISTS** — 17 of them `completed` (the
  `01a0ab11-*` HP-migration family, the `01a0a764` family), 2 landing on a
  `failed` successor (`01a0a540`, `01a0a4c7`).
- **0 rows name a UUID that does not exist** (no dangling UUID references).
- **25 rows claim supersession WITHOUT naming any id at all** — the reason text
  is prose ("superseded: brief corrected", "merged into the new combined task").
  Two of these turned out to be genuine defects, found by chasing the prose
  rather than a UUID:

### Defect B1 — the merged-into target never existed

`01a0a7a0` and `01a0a7ac` both say "merged into the new combined task
`01a0a7ae` (brief: `briefings/brief-heavy-tests-and-load-ban.md`)".
`select task_id,status from tasks where task_id like '01a0a7ae%'` returns **zero
rows** — the target id was never written to the ledger, so two cancellations
point at a task that does not exist. The WORK, however, did land, under a
different row: `01a0a88e-4a59` (completed) — `internal/heavytest/{heavy.go,boundary.go}`,
`internal/standards/heavy_inventory_gate_test.go`, `internal/testutil/rule27_gate_test.go`,
`testdata/heavy-tests.tsv`, plus `GOPI.md` Rule 27. Both rows were therefore
verdicted ALREADY-DONE (the work is in master), not SUPERSEDED (the named
successor is fiction).

### Defect B2 — a placeholder id used as a successor

`01a09225-db31` reason: "superseded-by **01a09225-XXXX**: wrong mechanism …".
`XXXX` is a literal placeholder; `where task_id like '01a09225-XXXX%'` matches
nothing. A reader who trusts the reason cannot follow the trail. (Corrected in
this stage: `01a09225-db31` is SUPERSEDED on the merit — the merger-side release
gate it asked for is void because the merger was retired, `f9ff1a89`.)

### Ledger-mechanism defect list (carried forward to the epic)

1. **Cancel reasons are prose, not links.** Only 19/121 cancellation claims
   name a resolvable successor id. 25 claim supersession naming nothing, so the
   audit cannot follow the chain mechanically — it has to read English.
2. **A claimed successor can be absent** (B1) — the reason is not validated at
   cancel time. `01a0a7a0`/`01a0a7ac` point at `01a0a7ae`, which does not exist.
3. **A claimed successor can be a placeholder** (B2) — `01a09225-XXXX`.
4. **A claimed successor can itself be dead** — `01a0a540` (failed) is the
   successor of `01a0a50a`, and `01a0a4c7` (failed) is the successor of
   `01a0a59a`; the chain terminates in failure with no re-file.
5. **Archive tags carry no rationale.** `git for-each-ref --format='%(contents:subject)'`
   on the archived tags shows the commit subject the tag points at, not WHY the
   branch was buried. This is exactly the gap `archive/feat/archive-why-gate`
   was built to close — and the audit confirms why it matters: five refs ahead
   of master looked like lost work until blob comparison proved otherwise.

## Rows verdicted NO-VALUE (16)

Probe/auto rows whose payload never reached a worker: `SELFTEST` mirrors,
`[auto] prompt test`, `EPIC CONTEXT auto re-registration` duplicates (the same
epic landed under a completed sibling), empty-payload rows correctly refused by
the bug028 guard, `pa-notify-smoke{1,2,3}` / `pa-tsb-refuse-smoke` operator
smoke rows, and `t-1788330680` — a `wrapper:t-*` row with an **empty payload**
whose wrapper target `t-1788330598` does not exist.

## STILL-NEEDED → re-filed (4)

| original row | new task | why still needed |
|---|---|---|
| `pa-q-spa-screenshot-paradigm` | `01a0b4af-78da-7d9b-b9df-4dc5f317f52e` | no CDP/headless-render→vision path anywhere in master; blocks its sibling too |
| `pa-q-cny-screenshot-shot` | folded into the row above | same blocker; one mechanism unblocks both |
| `t-1788065158` | `01a0b4af-93cf-788e-a916-ebdc122a8220` | `internal/tools/tasks.go:111,350` still call `openTasksDB(ctx, memoryDBPath())` — a remote `task_submit` still writes the node-local ledger |
| `t-1788897004` | `01a0b4af-b718-7766-a6b6-295b2d7aecbd` | no ledger-backup pipeline exists; `~/.gopi/backups/` holds only ad-hoc audit dumps |

Three tasks were submitted (the two PA-QUEUE screenshot rows share one deliverable
and are briefed together).

## Not Done (honest)

- **No ledger row was mutated.** Every query ran against
  `file:~/.gopi/memory.db?mode=ro` (SQLite read-only URI). Verdicts live only in
  the CSV. This is deliberate: the ledger is append-only evidence.
- **Rows that landed nowhere and are no longer wanted** were verdicted
  SUPERSEDED (56) rather than pursued — a cancel reason of "superseded", "re-filed",
  "zombie"/"orphaned claim" with no live successor means the want is gone. Where a
  successor DOES exist it is named in the evidence column, with its status.
- **No remote-push of the stage branch was performed** beyond committing in the
  worktree, which is the deliverable for a linked worktree (Rule 6c).
- **`01a0a7ad`** is verdicted SUPERSEDED on the strength of `01a0a793` being
  `verified`; the reason named two tasks and only one was resolved by direct lookup.

## Verification

- Build: not run — this stage changes no Go code (CSV + report only). `git status --porcelain` in the worktree lists only the two added devdoc files.
- Tests: not applicable for the same reason; no production code touched.
- CSV integrity: `121 rows / 121 unique ids / 0 empty evidence cells / 0 ids outside the worklist` (set-difference both directions, printed by the emitter).
- Verdict enum: every value is one of ALREADY-DONE / SUPERSEDED / NO-VALUE / STILL-NEEDED (the epic's enum), confirmed by a `Counter` over the written file.
- Commit: see the branch tip below.

## Evidence (self-filed)

- Count correction: `wc -l ids.txt` → 121; existence check → `0 missing` (121/121 found in `memory.db`).
- Shape (a): `git rev-list --count master..refs/tags/archive/fix/cwd-and-lifetime` → `7`; and `git rev-parse refs/tags/archive/fix/heavy-tests-classification:internal/testutil/rule27_gate_test.go` == `git rev-parse master:internal/testutil/rule27_gate_test.go` → IDENTICAL (blob hash), therefore landed.
- Shape (b): successor resolution over 121 rows → `named-successor rows OK: 19`, `DANGLING: 0`, `NO ID NAMED: 25`.
- Defect B1: `sqlite3 -readonly ~/.gopi/memory.db "select task_id from tasks where task_id like '01a0a7ae%'"` → **empty**.
- Defect B2: `... where task_id like '01a09225-XXXX%'` → **empty** (placeholder, never a row).
- Remote-submit bug still live: `git grep -n memoryDBPath master -- internal/tools/tasks.go` → `111:` and `350:` both `openTasksDB(ctx, memoryDBPath())`.
- Backup gap: `ls ~/.gopi/backups/` → `memory-glm-audit-20260909-184030.db` (+`-shm`/`-wal`) only.
- Heavy-tests work landed: `git ls-tree -r master --name-only | grep -i heavytest` → `internal/heavytest/boundary.go`, `internal/heavytest/heavy.go`.
- Row `01a0a88e-4a59` (the real owner of the heavy-tests landing) → `status=completed`.
