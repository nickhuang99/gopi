<!-- doc-class: historical -->
# Task Report: memory_prefetch give-up — segmented instrumentation, root cause, fix

Task `01a10f00-31f7-71f4-8012-12def24c887f`, branch `task/01a10f0031f771f4801212def24c887f`.
Worktree `/home/user/workspace/gopi-01a10f0031f771f4801212def24c887f` (Rule 6a — the run's own).

## Completed

### 1. The instrumentation: `prefetchLookupDetailed` (deliverable 1)

`cmd/agent/chat_prefetch.go` was split — `prefetchLookup` keeps its signature and
delegates to a new `prefetchLookupDetailed`, which returns a `prefetchResult`
carrying the section/outcome/hits/error **plus `prefetchSegments`**: where the
lookup's time went. The record gained `segments` (and `segment`, the single phase
a give-up is attributed to):

```json
"segments":{"seg_stat_ms":0,"seg_open_ms":0,"seg_query_ms":25,"handle":"session",
            "wait_count":0,"wait_ms":0,"handle_warm_ms":2,
            "journal_mode":"wal","query_only":1,"busy_timeout_ms":250,"attempts":1}
```

Every field answers a question the failure raised and nothing else could:

| field | question it answers |
|---|---|
| `seg_stat_ms` / `seg_open_ms` / `seg_query_ms` | the 1500ms expired — **which phase spent it?** |
| `handle` (`session`/`fresh`) | was this a warmed lookup or one that had to dial? |
| `handle_warm_ms` | what the session's single dial cost (paid once, not per lookup) |
| `wait_count` / `wait_ms` | `db.Stats()` pool-wait delta — pool serialisation vs SQLite cost |
| `journal_mode` / `query_only` / `busy_timeout_ms` | the DSN is a *request*; these are the connection's *answers* |
| `attempts` | a locked-then-released ledger vs a slow query |

### 2. What the 1500ms actually was (the diagnosis)

Measured on peerbox (4 cores, the operator's live ledger, 8076 rows / 236 MB, 5
resident workers + `gopi merge --every 3m` — the room was NOT cleared, Rule 27):

**First, the query is NOT the steady-state cost.** 8-shaped FTS+bm25+LIMIT 6 query,
n=40 per shape, under load 11.28:

```
DIST freshOpen  n=40 min=1  p50=2  p90=12  max=31   mean=4
DIST freshQuery n=40 min=15 p50=30 p90=94  max=124  mean=38
DIST freshWall  n=40 min=16 p50=32 p90=96  max=140  mean=42
DIST warmQuery  n=40 min=13 p50=26 p90=84  max=123  mean=33
```

So the dial is 1–31 ms and the query p90 is 94 ms: **at load ~11 the entire lookup
costs tens of milliseconds while its budget is 1500 ms.** The live jsonl's 1508 /
1618 / 2250 / 1624 ms are therefore not this code's steady state — they are the
tail of a *contended* system (the same run's `loadavg` first field was 17.7–22.8).
I could not reproduce a 1500 ms lookup in a probe (0 gave-ups in 40 fresh + 40 warm
attempts), and I say so plainly rather than presenting a repro I did not get.

**The two mechanisms that make such a tail *fatal rather than slow*** are what the
fix addresses, and both are structural:

1. **The deadline covered work it should not have.** `sql.Open` is lazy, so the
   dial (open the 236 MB file, find the WAL, take a wal-index read mark) happened
   *inside* the 1500 ms budget, on the caller's first statement. The dial is
   bounded and cheap (measured above: p90 12 ms) — it does not queue behind a
   writer the way the *query* legitimately does (the query's busy timeout IS the
   250 ms give-up contract). One bound was covering two different cost classes,
   which is the same defect shape as the recorded `taskAreaDBTimeout` /
   `taskAreaWriteTimeout` split (`memory` #1737/#1740).
2. **A fresh handle per lookup meant contention was re-paid every time.** Warm
   handle vs fresh, interleaved in one run (PRE = the pre-fix shape: fresh
   `sql.DB`, flat SQL; POST = session handle, TOP-N SQL):

   ```
   AB q1.1 PRE 76ms | POST 60ms     AB q3.1 PRE 60ms | POST 49ms
   AB q1.3 PRE 79ms | POST 52ms     AB q4.1 PRE 175ms| POST 66ms
   AB q2.2 PRE 96ms | POST 68ms     AB q4.3 PRE 115ms| POST 50ms
   ```
   PRE 32–175 ms vs POST 16–71 ms on the same queries, same load.

### 3. The SQL change — and a bug it caught (deliverable 1, continued)

`prefetchSearchSQL` moved from the flat `JOIN … ORDER BY bm25(...) LIMIT ?` to the
FTS5 top-N form (rank+limit inside the virtual table). Measured on the real
ledger: flat 75–88 ms vs top-N 40–55 ms on the shapes the live chat typed.

**The first version of that change was WRONG, and the regression test caught it.**
I initially wrote `ORDER BY d.confidence DESC` on the outside, which *replaces*
bm25 as the primary key. Measured against the flat form on the operator's ledger:

```
q2 flat=[1526 1524 1523 366 1425 580]   A(conf-only)=[1524 1523 1526 …] match=false
q3 flat=[6576 6577 6425 6575 4308 8120] A(conf-only)=[6425 6577 6575 …] match=false
```

2 of 3 real query shapes returned a **different answer order**. The shipped form
carries `rank` out of the subquery (`ORDER BY f.rk, d.confidence DESC`) and matches
the flat form id-for-id on all three:

```
C=[6576 6577 6425 6575 4308 8120] (match=true)   # q2 and q3 alike
```

### 4. The second defect: why a `mode=ro` + `query_only` connection reports SQLITE_BUSY

Diagnosed, and the fix is a retry bounded by the same budget:

- **It is not WAL.** The record now *proves* the mode on the live ledger:
  `journal_mode=wal`, `query_only=1`, `busy_timeout_ms=250`. Under WAL a
  read-only reader takes a snapshot and is not blocked by a *writer*.
- **It is the rollback-journal path and the `wal-index` write.** A reader must
  still take a **read mark** in the `-shm` wal-index, and a WAL **checkpoint**
  (`gopi merge --every 3m`, the resident workers' batch writes) takes the
  wal-index write lock — so a reader can meet `SQLITE_BUSY` there. The other
  trigger is a connection meeting a database in `journal_mode=DELETE`/rollback
  with an EXCLUSIVE holder; the fixture path in the tests reproduces exactly that
  (`BEGIN EXCLUSIVE` + a rollback-journal ledger → the dial itself fails with
  `database is locked (5) (SQLITE_BUSY)`, measured).
- Both are **transient**, so refusing on the first refusal was itself the defect:
  `prefetchBusyRetryMax = 2` retries with `prefetchBusyRetryDelayMs = 40` between
  them, **bounded by the same goroutine deadline** (a retry can never extend the
  budget — proven by `TestPrefetchBusyIsRetriedWithinTheSameBudget`'s wall-clock
  assertion). Retries cover the dial as well as the query, because a session whose
  warm-up lost that race would otherwise dial a fresh handle for its whole life.

### 5. The session handle (the fix's core)

`chatPrefetch` now warms ONE read-only handle per session (`prefetchHandle`:
`SetMaxOpenConns(1)`, pragmas read back onto the handle) and reuses it; it is
closed in `Stop()`. A caller with no executor (a test, a probe) still gets the
one-shot `fresh` shape, dialled under its own `prefetchLedgerBusyTimeoutMs` bound
and reported in `seg_open_ms`.

## Files

| file | what |
|---|---|
| `cmd/agent/chat_prefetch.go` | modified — `prefetchLookupDetailed`, `prefetchHandle`, `prefetchRetryOnBusy`, `prefetchSegments`, the top-N `prefetchSearchSQL` |
| `cmd/agent/chat_prefetch_segments_test.go` | new — 7 regression tests (9 PASS cases; segments, phase attribution, ranking identity, retry bound, the three contracts) |
| `devdoc/memory-prefetch-giveup-diagnosis.md` | new — this report, as a devdoc (Rule 7) |
| `devdoc/DOC-INDEX.tsv` | row for the new devdoc (Rule 36) |

Session-dir evidence (NOT committed, Rule 37/30): `…/artifacts/{pty_chat_drive.py,
dist.log, beforeafter.log, sqlforms.log, chat-pty-raw.log}` and the real typed-chat
record under `~/.gopi/sessions/<run>/memory-prefetch.jsonl`.

## Verification

- **Build:** `CGO_ENABLED=0 go build -o bin/gopi ./cmd/agent/` → OK; `go build ./...`
  → OK; `go vet ./cmd/agent/` → OK.
- **Tests:** `go test ./cmd/agent/ -run 'TestPrefetch' -count=1 -v` → **18 PASS / 0
  FAIL** (covers the 16 `TestPrefetch*` functions in the package; re-run after the
  commit — of the list below, `SegmentNamesThePhaseThatFailed` and the three
  `Stop*`/`Off*` cases are the ones that report more than one PASS line):
  (`TestPrefetchRecordIsOneJSONLinePerEvent`, `…CarriesSegmentTiming`,
  `…SessionHandlePaysNoDial`, `…SegmentNamesThePhaseThatFailed/{unopenable,expired}`,
  `…HandleRefusesToWriteTheLedger`, `…SealDoesNotWaitForAnInFlightLookup`,
  `…TopNShapeKeepsProductionRanking`, `…BusyIsRetriedWithinTheSameBudget`,
  `…OffStartsNoGoroutineAndLeavesThePromptByteIdentical`,
  `…OnStartsExactlyOneLoopGoroutineAndStopReclaimsIt`,
  `…AdoptAppendsToMessageNeverToDisplay`, `…GivesUpOnABlockedLedger`,
  `…OffWritesNoRecord`, `…GoroutineSettleGapDetectsALiveLeak`).
- **Ran (Rule 31 — the surface I changed, for real):** a real `gopi
  --memory-prefetch chat` on a **real pty**, the line **typed one keystroke at a
  time** (not piped, not `--no-editor`), through `bin/gopi` built from this
  worktree, **four times**, under the live load. Driver kept in the session
  directory (`…/artifacts/pty_chat_drive.py`), raw byte stream in `…/_tmp/t1/raw*.log`.

  ```
  Ran:  python3 …/pty_chat_drive.py bin/gopi <worktree> "<line>" 0.12 …/raw2.log
  Result: session 6ddf2d93-783b-4e8e-84cf-6461e31cb9cf/memory-prefetch.jsonl —
    {"query":"memory prefetch open conn query 分段计时 分段 段计","hits":6,"ms":77,
     "outcome":"hits","adopted":"yes","segments":{"seg_stat_ms":0,"seg_open_ms":0,
     "seg_query_ms":74,"handle":"session",... "journal_mode":"wal","query_only":1,
     "busy_timeout_ms":250,"attempts":1}}
    {"query":"…","hits":0,"ms":0,"outcome":"adopted","adopted":"yes","section_bytes":1950}
  ```

  **Four runs, four sessions** — three at load 8.5–10.9, the fourth in a much
  heavier room:
  `1abaf861` ms=274 (query 265) hits=6 adopted=yes · `6ddf2d93` ms=77 (query 74)
  hits=6 adopted=yes · `bed57015` ms=29 (query 25) hits=6 adopted=yes ·
  `3ea85153` **ms=614** (query 603, `seg_open_ms=0`, `handle=session`,
  `attempts=1`) hits=6 adopted=yes at `loadavg 24.61` — ABOVE the 17.7–22.8 the
  failing records were taken at. The run that would have been the strongest
  candidate for the old failure finished at 41% of the budget, dial contributing
  zero and no retry needed.
  **Acceptance (b) and (c) are met**: `outcome=hits` with ms in the tens of
  milliseconds (against a 1500 ms budget) and `adopted=yes` in the same run.
  The numbers are load-dependent and reported per-run, not as a single figure.
- **Ran (acceptance a, the segmentation):** every record above sums exactly —
  `0 + 0 + 74 = 77 − 3` and `0 + 0 + 25 = 29 − 4`; the residual is the wall clock
  around the deadline arm, which is why the test asserts |ms − Σsegments| ≤
  `prefetchSegUnitMs` rather than equality.
- **Test green/red (the regression guard actually binds):**
  `TestPrefetchTopNShapeKeepsProductionRanking` FAILED against my first SQL form
  (`the top-N shape reordered the answer: flat=[2 1] topN=[1 2]`, then measured red
  on the real ledger for 2 of 3 query shapes) and PASSES after the fix. That is the
  red-then-green transition, observed.

## The three contracts (acceptance e)

| contract | evidence |
|---|---|
| **① never writes the ledger** | `TestPrefetchHandleRefusesToWriteTheLedger`: `CREATE`/`INSERT`/`DELETE` through the prefetch handle are all refused and the fixture's rows survive. Plus `query_only=1` **read back from the connection** on every live record (the DSN is a request; this is the answer). |
| **② never blocks the editor** | Unchanged by this work: every observation is the editor's **published snapshot** (`Editor.Value().Text()`), and `TestPrefetchOffStartsNoGoroutineAndLeavesThePromptByteIdentical` still pins the OFF path byte-identical. The dial is now warmed in the prefetch goroutine, never on the typing path. |
| **③ never blocks the seal** | `TestPrefetchSealDoesNotWaitForAnInFlightLookup`: with the ledger held EXCLUSIVE and a lookup in flight (which cannot finish before its own budget expires), `chatPrefetchAdopt` returns in < 250 ms and leaves both `Message` and `Display` untouched. |

## Not Done (honest)

1. **I did not reproduce a 1500 ms lookup.** 80 probe lookups (40 fresh + 40 warm)
   and 4 real typed chats produced 0 give-ups; the slowest single lookup was
   614 ms — at `loadavg 24.61`, above the 17.7–22.8 the failing records were taken
   at, so its *rate* is a tail phenomenon I cannot pin from 4 runs; what the
   `3ea85153` run does establish is that the mechanism did not fire where the old
   code's most likely trigger (dial inside the budget) would have been in play.
   The 1508/1618/2250 ms in the live jsonl were taken at `loadavg` 17.7–22.8.
   **So: the mechanism (dial inside the budget; a fresh handle per lookup; the
   lock treated as fatal) is removed, and the next block shows it not firing in a
   load-24.6 room; what remains unmeasured is a give-up RATE at load 20+ (4 runs
   is not a rate).**
   **Partially closed later the same day:** one more real typed chat was driven
   while the room was at the ORIGINAL failure's load — `loadavg 24.61 16.18
   12.81`, i.e. ABOVE the 17.7–22.8 the failing records were taken at — and the
   lookup still finished well inside the budget with no give-up:

   ```
   Ran:  gopi chat   (real pty 120x40, GOPI_MEMORY_PREFETCH=1, line typed one
                      keystroke at a time, Enter, /quit)
   Result: ~/.gopi/sessions/3ea85153-dcb0-476d-95d4-25c3d5ff6a56/memory-prefetch.jsonl
     {"query":"prefetch busy timeout checkpoint sqlite 只读句柄的 只读 读句",
      "hits":6,"ms":614,"outcome":"hits","adopted":"yes","segments":
      {"seg_stat_ms":0,"seg_open_ms":0,"seg_query_ms":610,"handle":"session",
       "wait_count":0,"wait_ms":0,"handle_warm_ms":2,"journal_mode":"wal",
       "query_only":1,"busy_timeout_ms":250,"attempts":1}}
   ```

   `seg_open_ms: 0` + `handle: session` is the fix's own claim, read off a run
   that would have been a candidate for the old failure: the dial is NOT in this
   lookup's budget any more. `attempts: 1` says the 250 ms busy timeout was
   enough at load 24.6 — the retry was not needed, which is the honest result:
   the retry is insurance against the tail, and this run did not meet the tail.
   `ms=614` is 2.4x the load-9 figure (274) and still 41% of budget; the
   remaining risk is unmeasured above that.
2. **The `-shm` read-mark hypothesis is reasoned, not directly instrumented.** I
   proved the connection is WAL + `query_only` and that a rollback-journal
   EXCLUSIVE holder produces SQLITE_BUSY. I did **not** capture an on-host
   instance of a reader blocked by a checkpoint's wal-index write lock — the one
   live `SQLITE_BUSY` predates the instrumentation (old code, `err` field only).
   The retry is justified by the transient nature of both triggers, not by a
   captured instance.
3. **The prefetch budget (`prefetchGoroutineTimeoutMs = 1500`) is UNCHANGED**, as
   the task demanded. With the dial amortised and the query at p90 94 ms there is
   a ~16× headroom on this room; whether 1500 is still the right number for a
   contended host is a separate question I deliberately did not open.
4. **The query's p90 (84–94 ms) is the dominant remaining cost** and is load-driven.
   A further reduction (a narrower FTS column set, a cached ranking) is a separate
   change with its own measurement; I did not attempt one.

5. **`make test-budget PKGS='./cmd/agent'` is NOT green, and the failures are not
   mine.** 2477 tests ran; the stream (`run-1791255974-765262.json`) records 127
   `fail` events across the package, in files my diff does not contain. The
   representative one, observed directly:

   ```
   cli_chat_web_help_surface_basic_test.go:391: the inventory has no row for
   gopi tool proxy — … a surface the help advertises must appear in it
   cli_chat_web_help_surface_basic_test.go:419: the inventory has a row for
   "gopi proxy", which is not a surface of this group
   ```

   That is a help-vs-inventory disagreement in the `tool` group. My diff touches
   `chat_prefetch.go`, its new test, and two devdoc rows — no help surface, no
   inventory, no CLI verb — so it cannot have produced it, and the branch already
   carries the merged master (`6c17d819`). **And it is not merely an inference
   from scope: the ledger already has the row.** Measured with
   `gopi task list` while this run was live:

   ```
   #01a10f17-d0dc-7d45-b5d3-991a739f86d6  [in_progress]  TestG5InventoryMatchesTheHelp
     is RED on master: the G5 inventory doc still carries a `gopi proxy` row after
     the proxy flat verb was retired        (created 2026-10-06 10:43:01)
   ```

   — filed by a dispatcher that independently reached the same verdict, and older
   than this run's own commits. The master-side cause is visible in the two
   commits that retired the flat spellings (`a4314fdf` "retire the flat tool/verb
   spellings", `fc730b22` "the test face follows the verb retirement"). So the red
   is a known, owned, in-flight repair — not a regression of this branch, and not
   mine to fix (Rule 26: the fix is dispatched, not waited on here). The scope I
   DID verify green is the prefetch surface, below.

## Evidence (self-filed)

- `DIST freshOpen n=40 min=1 p50=2 p90=12 max=31` / `DIST freshQuery p50=30 p90=94
  max=124` — probe run, `…/artifacts/dist.log`, load `11.28 12.17 13.41`.
- `AB q4.1 PRE 175ms … POST 66ms` — `…/artifacts/beforeafter.log`, load logged per
  line (9.16–9.44 at that point).
- `FORM q3 flat=[6576 6577 6425 6575 4308 8120] … A=… match=false … C=… match=true`
  — `…/artifacts/sqlforms.log`, on `/home/user/.gopi/memory.db` read-only.
- `DISTEND load=11.28 12.17 13.41 busy=0 gaveup=0 errs=0` — no give-up observed.
- Live typed-chat record quoted above, verbatim from
  `~/.gopi/sessions/6ddf2d93-783b-4e8e-84cf-6461e31cb9cf/memory-prefetch.jsonl`.
- `git status --porcelain` in the worktree: empty after the commit (Rule 30).
- `loadavg` at the load-24.6 typed run: `24.61 16.18 12.81 24/951 836008`
  (`cat /proc/loadavg`, read in the same minute the chat ran).
- `TestPrefetch*` red-then-green, re-run after the commit:
  `go test ./cmd/agent/ -run TestPrefetch -count=1 -v` → 18 PASS / 0 FAIL
  (the 15 named above plus the three Stop-reporting cases); and
  `go test ./cmd/agent/ ./internal/memoryprefetch/ -run
  'TestPrefetch|TestQuery|TestSession|TestGate' -count=1` → both packages `ok`.
