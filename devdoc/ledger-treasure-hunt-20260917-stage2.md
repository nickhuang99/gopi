<!-- doc-class: historical -->
# Ledger Treasure Hunt — Stage 2 (curated failed rows)

**Epic**: audit all 319 terminal rows in `~/.gopi/memory.db` and decide, per row, whether
the underlying work is actually done. This is the STAGE 2 batch.
**Run**: task `01a0ab3e-d17f-72b4-bc5a-4ed945d14cd9` · branch `feat/ledger-treasure-hunt-stage2`
**Scope**: the curated (human / llm submitted) **failed** rows — the highest-signal batch.
**Ledger contract**: read-only (`sqlite3 -readonly`). No row was updated, deleted or archived.

---

## 0. The briefing's "51" is a residual — the class is 56

The frozen worklist is `~/.gopi/sessions/01a0ab1c-e5c1-75ac-8992-ea71c58c1287/rows-master.csv`
(md5 `ff312577ccb7eb0406b520cce31b15cb`, 319 data rows), never re-derived — the ledger is
live and counts drift.

The briefing says *"the 51 rows in status=failed that are NOT auto / sync-yielded / wrapper /
HP-origin / merge-lane"*. That is a **precedence-ordered residual**, not a SQL predicate.
Measured from the frozen CSV (one statement, class ≠ predicate):

```
$ awk -F, 'NR>1{print $4"|"$2}' rows-master.csv | sort | uniq -c
     93 curated-cancelled|cancelled
     56 curated-failed|failed          <- THIS BATCH
     40 auto-prefixed|cancelled
     32 HP-origin|cancelled
     31 auto-prefixed|failed
     18 sync-yielded|cancelled
     18 HP-origin|failed
     13 merge-lane|failed
      8 wrapper:t-*|failed
      6 wrapper:t-*|cancelled
      4 merge-lane|cancelled
```

`curated-failed` = **56**. The briefing's 51 is the same residual arithmetic that produced its
"auto 89" / "HP 29" figures (each other's complement): the difference is the rows the briefing's
own precedence order swept into other classes (e.g. `01a0a4c7` / `01a0aa8b`, which carry a
worker tag). **All 56 were classified** — the acceptance target is 56/56, and the 51 is
reconciled here rather than silently used.

Of the 56: 25 carry the `resolved` tag, 31 do not. Both were audited (a `resolved` tag is a
tombstone, not evidence that the work is done).

## 1. Method, and the evidence hierarchy that survived contact

Per row, in this order:

1. **Payload / deliverable** — extracted from the live ledger once, per row, into
   `.stage2/payloads/<task_id>.txt` (all 56 present; the ledger query was retried on
   `database is locked`).
2. **Git facts first** (never the row's own summary text):
   - `git rev-parse --verify --quiet refs/heads/<branch>`, falling back to
     `refs/remotes/*/<branch>` — then
   - `git rev-list --count master..<sha>` **== 0** plus `git merge-base --is-ancestor <sha> master`
     to name the actual landing commit, and
   - `git log master --format='%ct|%h|%s'` scanned for the branch name, ordered by **timestamp**
     (not by grep order) so that a *Revert* followed by a *re-merge* is read correctly.
3. **Content check** — does the symbol / behaviour / doc the row asked for exist on master
   (`grep`, `git cat-file -e master:<path>`)?
4. **Session JSONL** (`~/.gopi/sessions/<task_id>/`) when git was ambiguous.

**A branch ref being GONE is the signature of a successful merge, not of failure** — the merger
deletes the branch after landing it. Measured: **all 38** rows that name a branch have NO local
or remote ref, yet **21 of them have a merge commit as their LAST event on master**. Reading
ref-absence as failure would have produced 38 false "still needed" verdicts.

**`git merge-base --is-ancestor` alone is not enough here**: 7 rows show a *pattern* of
merge → revert → re-merge on master, so ancestry is true and *useless* — the ordering carries the
information:

```
fix/task-list-state-honesty        aad25579 Merge(1789213811) → d4512e18 Revert(1789214331) → 249d7860 Merge(1789243935) LAST
fix/launcher-install-refresh       bdde6e3c Merge(1789216312) → ecd2d272 Revert(1789216592) → ... → LAST Merge
fix/rawlog-test-home-leak          ... bd958087 Revert"Revert"(1789290553) → 833cc2b6 resolve merge(1789363229) LAST
feat/peak-hour-guard               3e6e6a9f … Revert(1789268119) → 4c1dee4f/849371ff Merge LAST
feat/guard-test-daemon-env-isolation d45d90e2 Merge → d07e0d31 Revert → … → 3c579375 Merge(1789329993) LAST
fix/verify-no-verdict-keeps-merge  earlier reverts → 2bc93022 Merge(1789417889) LAST
fix/peak-hour-build-gaps           a09287dc Merge(1789318161) LAST
```

## 2. Per-row verdicts

Full machine-readable table with the evidence line for every row:
**`devdoc/ledger-treasure-hunt-20260917-stage2-verdicts.csv`** (56 data rows + header).

| verdict | count |
|---|---|
| ALREADY-DONE | 50 |
| SUPERSEDED | 3 |
| STILL-NEEDED | 2 |
| NO-VALUE | 1 |
| PARTIALLY-DONE | 0 |
| UNCLEAR | 0 |
| **total** | **56** |

No row needed UNCLEAR: every one of the 56 reached a verdict with independent evidence, so the
"missing information" allowance was not used. That includes the two rows whose session
directories have **no jsonl at all** (`01a0a92d-560e`, `01a0a92d-77a0`) — for those the git
content check alone decided (see §4).

### 2.1 The 50 ALREADY-DONE (landed evidence, one line each)

Every entry below resolves to a ref, an ancestor sha, a test that compiles/runs green, or a doc
present on master — the command is in the CSV's `evidence` column.

`t-1788031086` `t-1788041900` `t-1788047065` `t-1788218896` `t-1788278743` `t-1788305178`
`t-1788305179` `t-1788306421` `t-1788307556` `t-1788307558` `t-1788309830` `01a08e7b` `01a08e80`
`01a091ea` `01a091ed-5466` `01a091ed-62a1` `01a091f1` `01a091f4` `01a091f9` `01a091fb-1cff`
`01a091fb-1e7d` `01a092db` `01a092fc` `01a09435` `01a09587` `01a095af` `01a0965e-b3e4`
`01a0965e-c117` `01a097f8` `01a098af` `01a09950` `01a09fd1` `01a0a416` `01a0a429` `01a0a42d`
`01a0a45c` `01a0a4ae` `01a0a4b8` `01a0a4c7` `01a0a540` `01a0a63e` `01a0a724` `01a0a72c`
`01a0a92d-21a5` `01a0a92d-2361` `01a0a92d-560e` `01a0a92d-575d` `01a0a9b1-6dc6` `01a0a9b1-7581`
`01a0aa8b`

Three of these deserve their own note, because the git ref alone would have misled:

- **`t-1788278743`** — this row *is* the fix for a defect I found still being described wrongly
  elsewhere. Its demand ("the writeback must stop overwriting `tasks.summary`") is implemented:
  `internal/tools/tasks.go:2970` now documents *"bug056: the result NO LONGER overwrites the
  immutable summary — it is appended as a task_events row"*. **Landed.**
- **`01a0a724`** — the row's *subject* (a `done` channel with two readers, `merger_verify_child.go`)
  no longer exists in that shape: the file is gone and the wait cap is
  `internal/tools/verify_gate.go:72` `MergeVerifyReasonChildTimeout` with a single
  recorded-verdict reader. `devdoc/stale-merge-audit-20260916.md:406` explicitly names this row
  as the shape its §5 rule 3 prevents. **Landed (restructured), and the row's session was
  SIGTERM'd at iteration 7** — the failure is the daemon kill, not the work.
- **`01a0a429`** — the row's own session died mid-run (34 assistant messages, last:
  *"Now let me write the 'dirty/too-young/foreign' table-driven test … then commit"*). But the
  mechanism it asked for — verification worktrees under the memory base being **inside a declared
  root** — is present on master as `internal/tools/path_policy.go:315 declaredRoots()`. Verdict
  is ALREADY-DONE on the content, with the authorship caveat recorded in the CSV.

### 2.2 The 3 SUPERSEDED

| row | why the work item is moot |
|---|---|
| `01a0973a` | The **whole subject was retired**: `cmd/agent/peak_guard_removal_test.go` asserts the peak-hour knob's ABSENCE from master, and master `cdb1fd75` *"retire the last live instructions for the removed peak-hour knob"* swept its leftover instructions (`24ccd78b`, `33bb3b3c`, `0f3e7628`). The branch merged (`849371ff`, ancestor) and the feature was later deliberately deleted — there is no work left to do. |
| `01a09805` | The row asked "who kills all the gopi processes?" and its **own session answered with exit_code=0**: root cause is an *external login session-scope teardown* (`session-18200.scope`), **not** a gopi watchdog — "no fleet-scoped signal exists anywhere in the tree". The two genuine in-tree defects were fixed at `feat/signal-provenance` @ `7d7a12f3` (**ON-MASTER**) via `stopChildGracefully` + `internal/signalrec`, with regression tests. The row's failure is the SIGTERM teardown it diagnosed — the diagnosis succeeded. |
| `01a0a650` | The row reproduced a hang on **pristine master** and attributed it to the environment — its own session ends *"pristine master 807dda98 … my branch's failures are a strict subset → pre-existing/environmental"*, and the worktree was deleted mid-task by the residue sweeper. The successor epic **closed the matter by withdrawal** (Rule 27: no test may manufacture device load): master `4dd753a4` / `bf7a87cb` — *"cmd/agent is RED on pristine master (attributed)"*, *"report cmd/agent as UNRESOLVED, not PASS"*. |

All 3 were merged as branches AND superseded as subjects — the same row can be both, and the
distinction matters: ancestry proves the *branch* landed, not that its *goal* is still a goal.

### 2.3 The 1 NO-VALUE

- **`t-1788551878`** ("independent review of `devdoc/verify-write-task-area-20260904.md`"). Its
  session (`~/.gopi/sessions/t-1788551878/32231236-….jsonl`, 48 events) ends
  `category=4xx http_status=429 "rate limit exceeded" (digitalocean)` at `failed_iteration 11`:
  the run never produced anything. The deliverable was a 300–500 word reply in the final message
  ("不需要 commit 任何文件") — unpreservable by design — and its subject doc is superseded by the
  write-task-area implementation reports now on master. Negative value: re-running it reviews a
  2026-09-04 doc that is no longer the live design.

## 3. Recovered work — NEW tasks submitted

Two rows are genuinely unfinished. Both were turned into self-contained tasks:

| row | new task | what it asks |
|---|---|---|
| `01a0a4c4-c26a-726f-a32f-72b171194741` | **`01a0b4a7-b0b2-7c35-aeef-41db957ae921`** | `internal/scratch/gc.go:56-60` `StaleAge = 6 * time.Hour` is **still** a bare constant with an explicitly falsified assumption; only the *cadence* knob exists (`GCIntervalEnvName`, `gc.go:78`). Parameterise the stale age (or replace the time window with a measured budget), one definition site, one resolution site, red-proof test. |
| `01a0a92d-77a0-7eb0-90a6-8c5f63051177` | **`01a0b4a7-b427-7e42-897f-3ed84da2c8a4`** | The merger still does **not** re-run the repo's gates on the MERGED result: `internal/smokegate/smokegate.go` carries the smoke command but no post-merge `go vet` / standards / stylegate / testutil run — Rule 12's close loop only rebuilds, and `go build` cannot see a test-only break. Implements `devdoc/stale-merge-audit-20260916.md` §5 rule 2. |

Both carry `tags=bug,ledger-recovery,stage2` (the second also `merger-policy`), so the run is
auditable back to this report.

## 4. Pattern analysis — why these rows read `failed` while their work is done

Four mechanical causes cover all 56 rows. None of them is "the agent gave up".

**C1 — the daemon/session was killed mid-run; the work was already landed or the branch landed
without the row ever being written back.** Measured `session_end` bodies:
`01a0a724` `run interrupted by signal` @iteration 7 · `01a0a4b8` same @13 · `01a0a650` same @26
· `01a09805`/`01a095af` **`exit_code: 0`** — those two rows *succeeded* and are still `failed`.
`01a095af`'s session ran 16299 s / 818 iterations to a clean exit and wrote
`devdoc/flat-raw-log-rehome.md`; `01a09805`'s reported success verbatim. **A session that exits 0
does not imply the row was written back** — 2/2 of the exit-0 rows here are `failed`.

**C2 — the merge machinery failed *after* the merge landed.** `fix/rawlog-test-home-leak` needed
a revert-then-reland (`bd958087` `Revert "Revert …"` → `833cc2b6 resolve merge … into master`),
and the row stayed `failed` from the first attempt. `fix/task-list-state-honesty` merged,
reverted, and re-merged 29 604 s later. The row's terminal state is a claim about **the row**,
never about the merge — the same defect Stage 0 recorded.

**C3 — the subject was retired by a later decision, leaving the row failed forever.**
`feat/peak-hour-guard` merged and was then *deliberately removed* from master. A row cannot
distinguish "abandoned" from "landed then intentionally deleted" — only a content check can.

**C4 — the row is a shape with no work identity, or its deliverable was never preservable.**
`t-1788551878` (reply-only review, killed by a 429) is the only NO-VALUE here, but the family is
larger in the other batches.

**Cost of the lie, measured**: the 56 rows consumed real runs — `01a095af` alone burned
70.5 M tokens over 818 iterations — and every one of the 50 landed ones is a row an operator
would re-dispatch on sight.

## 5. Ledger-mechanism defects found by this batch (all read from the CODE that writes the reason, per the Stage 0 lesson)

- **D9 — `session_end exit_code: 0` does not reach the row.** `01a095af` and `01a09805` both have
  `{"exit_code": 0, "type": "session_end"}` in their JSONL while the ledger says `failed`. The
  writeback path (`cmd/agent/cli_tasks.go` → `writebackOutcome` → `tools.TaskRunWriteback`) is
  driven from the RUN wrapper, so a wrapper killed *after* the child exited cannot write back.
  **Evidence**: two independent rows, clean exits, permanent `failed`.
- **D10 — a merge → revert → re-merge leaves the row terminal-failed with no re-open path.**
  7 of 38 branch-bearing rows show the pattern; the final state on master is a *merge* in every
  one, and the ledger still says `failed` in all 7. The re-land is invisible to the row.
- **D11 — `curated-failed` is a residual, so every downstream count inherits the drift.** The
  briefing quoted 51 for a class of 56; Stage 0 already recorded the same shape for
  "merge-lane 30" (no predicate). **A class must never be published as a subtraction** — publish
  the SQL predicate, or publish the frozen CSV row-set.
- **D12 — a reply-only deliverable is unpreservable.** `t-1788551878`'s payload says
  "不需要 commit 任何文件 / 直接在你的最终回复中给出报告". When the run dies before its final
  message, the deliverable is *structurally* unrecoverable — no branch, no file, nothing to
  audit. Compare `01a0a92d-560e` / `01a0a92d-77a0`: their session dirs contain **no jsonl at
  all** (20 KB, no run), so even the run's own trail is gone. **A task whose only output is a
  final message cannot be audited after the fact** — such rows should be required to write a file.

## 6. Worktree / delivery

- Branch `feat/ledger-treasure-hunt-stage2` in worktree `~/workspace/gopi-ledger-hunt2`,
  forked from master `5cdae25e`.
- Deliverables: this report + `devdoc/ledger-treasure-hunt-20260917-stage2-verdicts.csv`
  (56 data rows + header).
- Scratch evidence (payload dumps, the branch/merge/ordering tables) lives **outside** the
  worktree under `~/workspace/gopi-ledger-hunt2/.stage2/` — the worktree itself ends clean.

## Verification

- **Build**: `CGO_ENABLED=0 go build -o /dev/null ./cmd/agent/` → exit 0 (run in `~/work/GoPi`
  while proving `01a098af`'s two build gaps were closed).
- **Tests** (package-scoped, branch-relevant):
  - `go test ./internal/tools/ -run TestMemorySearchNonBlocking -count=5` → `ok … 13.318s`
    (proves `01a08e7b` / `01a08e80` ALREADY-DONE).
  - `go test ./internal/testutil/ -run 'TestWorkerOwnershipPointerPresent|TestTestBudget|TestBudget' -count=1`
    → `ok … 2.236s` (proves `01a0a92d-21a5`'s three failures no longer reproduce).
  - `go test ./cmd/agent/ -run TestCLIContractInventoryMatchesDispatch -count=1` → `ok … 1.342s`
    (proves `01a091fb-1cff`).
- **Git evidence**: `branch_evidence.tsv` (38 ref lookups), `merge_proof.tsv`
  (`git merge-base --is-ancestor` per landing commit), `lastevent2.tsv` (timestamp-ordered
  master log per branch) — all under `.stage2/`.
- **Coverage proof (mechanical, not eyeballed)**: the frozen worklist row-set and the verdict
  CSV were set-differenced — `missing: []  extra: []  dupes: []`, 56 rows on both sides:
  ```
  worklist rows: 56 verdict rows: 56
  missing: []
  extra  : []
  dupes  : []
  ```
- **Ledger untouched**: every query used `sqlite3 -readonly`; no INSERT/UPDATE/DELETE, no
  `gopi task archive`. The two new rows were created through `task_submit` (the normal append path).

## Not Done (honest)

- **56/56 classified, but 4 verdicts rest on a single evidence class.** `01a0a92d-560e`,
  `01a0a92d-77a0` (no session jsonl, no ref, no commit anywhere matching their branch name) and
  `01a0a429` (session died mid-run) are decided by the **content** check alone. For
  `01a0a92d-560e` that is strong (`cwd_release_rig_test.go` + `cwd_release_composition_test.go`
  drive the producer paths); for `01a0a429` the CSV states the authorship caveat explicitly.
- **The two STILL-NEEDED verdicts are code-read, not test-run.** I did not build the new gate
  myself (that is the submitted task's job) — the claim "the merger does not run `go vet` on the
  merged result" is a read of `internal/smokegate/smokegate.go` + the Rule-12 closeloop, not an
  executed red/green.
- **Stage 2 covers only the 56 `curated-failed` rows.** The remaining classes
  (`curated-cancelled` 93, `auto-prefixed` 71, `HP-origin` 50, `sync-yielded` 18, `merge-lane` 17,
  `wrapper:t-*` 14) are other stages of the epic.
- **No pattern claim about `curated-cancelled`** — this batch measured only failed rows; the C1–C4
  causes are stated for the 56 audited here and must not be quoted as fleet-wide counts.
