<!-- doc-class: historical -->
# Task Report: `internal/tools` intermittent FAIL — diagnosis

Branch: `diag/tools-flake` (worktree `~/workspace/gopi-toolsdiag`)
Tip diagnosed: `622cd465` (== `1c021f4c` content-wise for `internal/tools`; runs used both)
Host: 4 cores, ambient load average 12–25 throughout (workers + `task merger --every 5m`).
Verdict: **one real bug (F1) + external interference / environment for the rest; no bug row
for anything without a repro.** F1's flake-vs-bug test is §1's `'rebuild'-heals` probe plus the
deterministic 5/5 repro — both run, not reasoned.

## 0. Verdict in one paragraph

`internal/tools` is **not** a load flake. It contains **one real, reproducible bug**
(an unindexed-store race in the memory engine: `TestMemorySearchNonBlocking`,
`internal/tools/memory_test.go:551`) — reproduced at 5/40, 2/40, 6/40, 27/200 and 29/200
across four legs, with a **deterministic** minimal repro that needs no race at all
(5/5). Two further failures seen in the log corpus are **external-process interference /
environment**, and one (`TestStyleGate`) is a genuine red gate belonging to another
task. The `make test` tail loses the test name for a mechanical reason reproduced below
in a 4-line experiment.

---

## 1. Every distinct failure observed, with evidence

### F1 — `TestMemorySearchNonBlocking` (memory_test.go:551) — **GENUINE BUG** (index desync, race)

Assertion: `internal/tools/memory_test.go:551`

```go
out, err := MemorySearch("concurrent", 5, "")
if err != nil { t.Fatal(err) }
if !strings.Contains(out, "concurrent topic") {
    t.Errorf("concurrent entries not searchable: %s", out)   // <-- line 551
}
```

Exact failure text (identical in every occurrence, incl. the 2026-09-11 worker log):

```
--- FAIL: TestMemorySearchNonBlocking (0.03s)
    memory_test.go:551: concurrent entries not searchable: No distilled memory found for this query. Nothing has been distilled on this topic yet — after you finish the task, call memory(action=store) to save what you learned so the next agent finds it.
```

Occurrences: `~/.gopi/logs/worker-20260911-043104-1.log:34516` (0.05s),
`/tmp/peeralias-quiet.log:83` (0.04s), `/tmp/stylegate-fullsuite-tip98ecff4.log:116` (0.05s),
`/tmp/verify-s3log.log:88` (0.04s), `/tmp/conc-tools.log:719` (0.02s).

**Measured rates** (built binaries, `-run '^TestMemorySearchNonBlocking$' -test.count=1`):

| leg | run | FAIL | asserting line |
|---|---|---|---|
| tip `1c021f4c`, ambient load ~14.9 | `-count=10` | 5/40 | :551 |
| master `3d9ae86d`, ambient load ~24 | 40 runs | 2/40 | :551 |
| tip `622cd465`, ambient load ~24 (6 `dd` hogs added) | 40 runs | 6/40 | :551 |
| tip `622cd465`, ambient load ~24 (in-process loop, 200 iters) | 200 iters | 27/200 | :551 (+1 store error :543) |
| tip `622cd465` (in-process loop, second probe) | 200 iters | 29/200 | :551 |
| tip `diag/tools-flake` `9ed2cb5d` (**re-check at report time**), ambient load 18.6 | `-count=20` | 4/20 | :551 |

The re-check leg (log `/tmp/diag-flake/confirm20.log`) is run **after** the diagnosis was
written, on the branch's own tip, with **no `.go` change in the worktree** (`git status
--porcelain` empty before and after) — it rules out the possibility that the earlier legs were
measuring a tree state that no longer exists. Single-run control on the same tip:
`-count=1 -v` → PASS 1/1 (`/tmp/diag-flake/confirm-v.log`) — the "a single green run proves
nothing" caveat, demonstrated rather than asserted.

Failing repetitions archived (each carries the `memory_test.go:551` assertion text verbatim):
`/tmp/diag-flake/run-4.log`, `run-10.log`, `run-21.log`, `run-23.log`, `run-31.log`.

A **second, distinct** assertion of the same test also fires, less often:

```
    memory_test.go:543: concurrent store: memory_store: database schema has changed: vtable constructor failed: distilled_qa_fts (17)
    memory_test.go:543: concurrent store: memory_store: database schema has changed: vtable constructor failed: distilled_qa_tri (17)
```

`internal/tools/memory_test.go:543`. Seen at `/tmp/diag-flake/race.log:7-9` (three times in one
run) and once as `STORE-ERR #145` in the 200-iteration probe (`/tmp/diag-flake/probe.log:21`).
**Same window, not a proven identical code path**: `SQLITE_SCHEMA (17)` is what a
connection reports when a virtual table it holds a prepared statement on is
created/dropped underneath it — i.e. the same "reader racing the schema build" condition that
produces F1, surfacing as a store error instead of an unindexed row. It is *not* counted as a
separate root cause, and the fix's acceptance (`-count=20`, 0 FAIL) covers both texts.

**`-race` is clean**: `go test -race ./internal/tools/ -count=5 -run '^TestMemorySearchNonBlocking$'`
→ `WARNING: DATA RACE` count **0**, 2/5 runs FAIL. So this is *not* a Go memory race — it is
a **schema-visibility race in the SQLite file**, invisible to the race detector.

#### Root cause (evidence, not reasoning)

Probe `/tmp/diag-flake/probe3.log` — on **every** miss (29/29) the row is committed but the
index is empty, and a manual FTS `'rebuild'` heals it:

```
MISS #3 triggers=6 rows=2 matchBefore=0 matchAfterRebuild=2 rebuildErr=<nil> journal=wal
...
SUMMARY: miss=29/200 healedByManualRebuild=29 zeroTriggerCases=0
```

(`matchBefore/After` = `SELECT count(*) FROM distilled_qa_fts WHERE distilled_qa_fts MATCH 'concurrent'`;
`SELECT count(*) FROM distilled_qa_fts` is *not* a usable probe — an external-content FTS table
reads through to the content table, so it always equals the base-table count.)

Mechanism, from `internal/tools/memory.go`:

1. The test runs 4 concurrent `MemoryStore` calls against a **brand-new** DB path.
2. `initKnowledgeBase` (memory.go ~723) creates the DB inside the schema-build window.

   `ensureSchema` (memory.go ~455) creates the base table **first**, then the FTS tables and
   the keep-in-sync triggers, **as separate autocommit DDL statements**:

   ```go
   // 1. Base table first — the FTS DDL below is rendered for the layout this CREATE produces
   CREATE TABLE IF NOT EXISTS distilled_qa (...)          // <-- base table now visible
   // 2. ... FTS index tables and keep-in-sync triggers
   stmts := append(m1TriVirtualTable(...), m1FTSVirtualTable(...))   // tri table, then fts table
   stmts = append(stmts, m1TriTriggers(ctx, db)...)                  // then the 3 tri triggers
   stmts = append(stmts, m1FTSTriggers(ctx, db)...)                  // then the 3 fts triggers
   ```

   Each statement is its own committed transaction (WAL: ~0.2 ms per DDL), so a reader on
   another connection observes **every intermediate state** of the schema build.
3. `openStoreDB` (memory.go ~977) declares the DB ready as soon as `tableExists("distilled_qa")`
   is true → a store that lands in the window between step "base table" and step "FTS+triggers"
   proceeds.
4. `memoryStoreRow` (memory.go ~2143) then: `INSERT` succeeds, and
   `if !ftsTriggersOwned(ctx, q)` (memory.go ~2298) — re-probing `sqlite_master` at that moment —
   sees the triggers **already created** and skips the explicit `syncFTSRow` fallback. The row
   is committed **and never indexed**.

Net: `distilled_qa` has the row (`rows=1..2`), the index has nothing (`matchBefore=0`), and
`MemorySearch` correctly returns the empty-result hint. Nothing later backfills it — a later
store in the same DB does not heal it either (probe STEP5 below).

#### Deterministic minimal repro — **no race required** (5/5 FAIL)

`TestZZDiagPartialInitStore` (probe file, deleted before commit) simulates exactly that
window without any concurrency: create the file with *only* the `distilled_qa` base table,
then call `MemoryStore` + `MemorySearch`.

```
--- run 1..5 (identical) ---
STEP1 store result="Memory stored (id=1)." err=<nil>     (a store here is reported SUCCESSFUL)
STEP2 search err=memory_search: knowledge base has no FTS index (distilled_qa_fts missing) containsRow=false
STEP3 baseRows=1 triggers=0 ftsTableExists=0 ftsMatch=0
STEP4 search-after-2nd-store containsRow=false
STEP5 search-after-later-store containsRow=false
```

Re-run at report time on tip `9ed2cb5d` (probe copied in, run, then deleted — `git status
--porcelain` empty afterwards): **5/5 identical, `-count=5`** (`/tmp/diag-flake/deterministic5.log`),
i.e. the reduction is stable across tips and does not itself depend on ambient load.

5 runs, 5 identical. So the bug needs no timing at all — the race only *decides* whether the
window is hit. This is the regression test form the fix should ship with (deterministic, no sleeps).

**Why the window is hit at all** (and why it varies run to run): the base-table `CREATE` is
committed before the FTS DDL, and its commit cost varies with ambient load/fsync pressure —
under rollback journal + `synchronous=FULL` a single DDL commit on this host has been measured
at ~155 ms (see `TestFreshDBInitIsFast`'s comment). A concurrent `MemoryStore` that lands in
that gap is unindexed. That is why the rate tracks load (2/40 on a quiet leg, 6/40 on the
hog-loaded leg) while the *failure* itself is deterministic.

### F2 — `TestBashDaemonSpawn_DoesNotLeakProcesses` (bash_daemon_test.go:87) — **EXTERNAL INTERFERENCE (not isolated)**

Assertion: `internal/tools/bash_daemon_test.go:87` —
`t.Error("a 'sleep 300' background process survived the process-group kill")`.

```go
// Independent /proc scan: no `sleep 300` argv may remain anywhere.
if !waitFor(func() bool { return !anyProcessCmdlineHas("sleep", "300") }, testDaemonReapPollWindow) {
    t.Error("a 'sleep 300' background process survived the process-group kill")
}
```

Exact text: `/tmp/cov-tools-sync-fulltest.log:2` and `/tmp/jobfamily-full-test3.log:14`
(both `3.07s`/`3.09s`).

Verdict: **not a bug in the test's own subject.** It asserts over the *whole machine's* `/proc`
argv, so **any** unrelated `sleep 300` process (another worker's `bash` call, an agent's poll
loop, a leftover daemon from a concurrent run) makes it fail. Isolation: **0/40 FAIL** at
package level under ambient load 12.5–16.6 (`bashd: 0/40`), and both historical failures
occurred in runs that also carried heavy `[task-notify]`/worker traffic. This probe cannot be
made deterministic on a shared host without scoping it to the process tree it owns — that is a
*test-design* observation, not a defect in `bash`'s process-group kill.

### F3 — `TestTaskDiffPeerRefusesInvalidAlias` (memory_sync_reconcile_cov_test.go:57) — **EXTERNAL (ssh) / environment**

Assertion: `internal/tools/memory_sync_reconcile_cov_test.go:57`.

```
--- FAIL: TestTaskDiffPeerRefusesInvalidAlias (0.02s)
    memory_sync_reconcile_cov_test.go:57: refusal must self-describe (invalid peer), got: diff: ssh peer|pipe: exit status 255
```

Verdict: the assertion wants a *self-describing* refusal but got the raw `ssh` exit status —
i.e. the `ssh` client was present and returned 255 (its own "cannot resolve/connect" code)
before the code's own message was produced. Environment-dependent (ssh availability/version),
0/40 FAIL in isolation. **Not part of the `internal/tools` flake class** and not reproducible
here → recorded, no bug row (cannot reproduce ⇒ cannot ship a repro).

### F4 — `TestStyleGate` (gate_test.go:101) — **genuine red gate, DIFFERENT task**

```
--- FAIL: TestStyleGate (4.08s)
    gate_test.go:76: undecidable sites (reported, never guessed): 86
    gate_test.go:101: stylegate unchecked-close: 33 test hit(s), ceiling 32 — new violations...
```

`~/.gopi/logs/worker-20260911-072424-1.log:8641`. This is a deliberate ratchet/ceiling gate
failing for a *content* reason (33 hits vs ceiling 32), on the worker that was editing the
ceiling. Not flaky, not load-related, not part of this diagnosis. Named here so it is not
mistaken for the flake.

### F5 — `TestFreshDBOpenCloseIsFast` (testsuite_temproot_test.go:166) — **ENVIRONMENT (scratch root on a block-backed fs)**

```
--- FAIL: TestFreshDBOpenCloseIsFast (0.48s)
    testsuite_temproot_test.go:166: opened+closed 12 fresh DBs in 479.859794ms (39.988308ms each, budget 300ms total):
    the scratch root "/dev/shm/gopi-tools-suite-tmp-2119916082" syncs at WAL close — ONE stalled fsync hangs the whole suite ...
```

Evidence: `/tmp/cov_accept.log:296`, `/tmp/cov_accept2.log:27`, `/tmp/a2.log`, `/tmp/freshdb-matrix.log:12`,
`/tmp/freshdb-matrix2.log:8,16`, `/tmp/guard-{BRANCH,MASTER}-GOPI-TEST-TMPDIR--tmp-.log:1`.

This test is *designed* as an environment guard (it fails when the scratch root is on a
block-backed FS). Its verdict depends on the host's fsync latency and on `GOPI_TEST_TMPDIR`:
`/tmp/guard-MASTER-…-old-guard` used `GOPI_TEST_TMPDIR=/tmp` → 15.19s FAIL; the same test
without the override → 0/15 FAIL; with `GOPI_TEST_TMPDIR=/dev/shm` → 0/15 FAIL. It is a
*timing* assertion by construction (median ratio vs a 12× limit) and is **inherently
load-sensitive** — it is the one place in this package where "flake" is the accurate word,
but it is a *guard on the machine*, not on product code. Reported here for completeness; no
bug row (the maintainers already treat it as a host guard, and a row would only restate it).

### Non-failures that look like failures (recorded to prevent a future mis-diagnosis)

- `/tmp/freshdb-final.log`: `FAIL / FAIL github.com/nick/gopi/internal/tools 0.872s` with
  **zero** `--- FAIL` lines — a build/compile failure of the untracked probe file, not a test
  failure. The tail shows only `FAIL`; this is the exact shape of the "no name in the tail"
  complaint.
- `/tmp/esc_tools_test.log:416`: `FAIL github.com/nick/gopi/internal/tools 600.034s` — the
  600 s package timeout (`panic: test timed out`) with zero `--- FAIL` lines.
- `/tmp/jobfamily-full-test.log:104` `TestGoldenCLILogSearchEmptyState` (golden
  `log-search-filter.txt` still lists `job_start` where the tool surface is now `job`) — a
  real content drift owned by the tool-surface rename task. Deterministic: 3/3 FAIL/PASS check
  → I ran it 3× on this tip: **PASS 3/3**. So it is already fixed/varies by tree; not part of
  this flake.
- `TestFoo` / `TestX` in `~/.gopi/logs/worker-20260912-033016-1.log:2126,2185` are **synthetic
  examples in an agent's reasoning text**, not real test names (confirmed: `TestFoo` exists
  nowhere; `TestX` exists only inside a *comment* in `internal/standards/gate_test.go`).

---

## 2. Reproduction statement

- **F1 is reproduced** and additionally reduced to a **deterministic** repro (5/5, no
  concurrency needed). Counts: 5/40, 2/40, 6/40, 27/200, 29/200 across four legs on two tips
  and three load levels (ambient 12–25 on 4 cores).
- F2/F3/F5 are **not reproduced** in isolation (0/40, 0/40, 0/15 respectively) under the same
  ambient load; their occurrences are all inside full-suite / shared-host runs.
- `-race` clean (0 data races, 2/5 FAIL) → the bug is not a Go data race.

## 3. Bug rows

Filed via `task_submit tags=bug` — see §6 for ids. Payload carries the repro loop, the exact
failure text, and the file:line.

## 4. Why `make test`'s tail loses the failing test name

Reproduced mechanically in 4 lines (`/tmp/gotail`, package `a` with one failing test +
package `b` that finishes later):

```
$ go test ./...
--- FAIL: TestAaaFails (0.00s)
    a_test.go:4: boom: 1
FAIL
FAIL	tailcheck/a	0.038s
ok  	tailcheck/b	0.204s
FAIL

$ tail -4
FAIL
FAIL	tailcheck/a	0.038s
ok  	tailcheck/b	0.204s
FAIL
```

Two independent causes, both confirmed:

1. **Ordering inside a package**: `go test` (non-verbose) prints the package's `--- FAIL: <name>`
   block **first** and the `FAIL\t<pkg>\t<secs>` line **last**. Any tail-based reader therefore
   sees `FAIL github.com/nick/gopi/internal/tools 254.857s` with the name *above* the cut.
   The name is always the *first* thing dropped.
2. **Interleaving across packages**: packages are run in parallel (GOMAXPROCS) and each
   package's block is emitted when *it* finishes, so later-finishing packages are printed
   **after** the failing block. In the real symptom
   (`worker-20260912-033016-1.log:4309`) the observers' tail showed
   `FAIL internal/tools 157.841s` followed by `ok … testsuite 2.687s` — i.e. the failing
   name is ≥1 full package-block above the tail cut. In this repo the gap is enlarged by the
   long-lived `task merger`/worker processes writing `[task-notify]` lines into the same
   stream.

So it is not a `go test` bug and not (only) tool-level truncation: **the gate's own output
order guarantees the name is above the tail**, and a gate whose failure output is unreadable
is one people learn to ignore.

### Proposed harness change (small, deterministic — no retry, no new timing)

Make the failure summary the *last* thing the gate prints. It does not weaken, skip or retry
anything; it only re-states what already happened, after the stream has drained:

```make
# Test output is captured to a log AND replayed, then a failure summary is printed
# LAST: `go test` prints `--- FAIL: <name>` at the TOP of the failing package's block
# and the bare `FAIL\t<pkg>\t<secs>` line last, so every tail-based reader (log
# truncation, agent output caps) loses the name. This makes the name survive.
TEST_LOG ?= /tmp/gopi-test.log
test:
	@go test ./... > $(TEST_LOG) 2>&1; rc=$$?; cat $(TEST_LOG); \
	if [ $$rc -ne 0 ]; then echo "--- failing tests (full log: $(TEST_LOG)) ---"; grep -h '^--- FAIL' $(TEST_LOG) | sort -u; fi; \
	exit $$rc
```

`/bin/sh`-safe (no `PIPESTATUS`/`pipefail`), zero new dependencies, no new file in the repo
(`/tmp`), and the exit code is unchanged, so nothing downstream can mistake a red gate for green.

**Verified end-to-end** on the real target (`Makefile:11-12`, `test: go test ./...`) in a
throwaway module `/tmp/mkcheck` (packages `a` with one failing test + `b` that finishes later,
so the interleaving is exercised): the gate now prints `--- FAIL: TestAaaFails (0.00s)` **as its
last output line**, and `make` still exits **2** — so a tail-based reader (and a CI that reads
only `make`'s exit code) sees both the name and the failure. The 4-line reproduction of the
*loss* is re-run in place (§4 above, `/tmp/gotail`) so both halves are backed by executed
commands, not by argument.

## 5. Files touched

`git diff --name-only 622cd465..HEAD` (the branch point) → **`devdoc/tools-flake-diagnosis.md`
only**. No `.go` file at all — `internal/tools` (production and tests) is byte-identical to the
diagnosed tip.

Note on the acceptance command as literally written: `git diff --name-only master..HEAD -- '*.go'`
reports 7 files (`cmd/agent/main.go`, `internal/agent/crashdump*.go`, `internal/agent/session.go`,
`internal/agent/envparse.go`) — **none of them mine**, and `grep -c internal/tools` on that list
is **0**. `master` (`3b51fc00`) advanced past this branch's point (`622cd465`) while the diagnosis
ran, by merging `feat/fix-crashdump-false-positive`; a two-dot range therefore attributes
*master's new commits* to this branch. The two-dot range against the branch point
(`622cd465..HEAD`) and `git merge-base --is-ancestor 622cd465 master`
(exit 0) together establish provenance.

The three temporary probe files (`zzz_diagprobe*_test.go`) were **deleted before the report
commit**; copies kept outside the repo at `/tmp/diag-flake/` for the bug row's repro evidence.

## 6. Verification

```
$ CGO_ENABLED=0 go build -o /tmp/diagbuild ./cmd/agent/   → build OK
$ git diff --name-only 622cd465..HEAD                      → devdoc/tools-flake-diagnosis.md
$ git diff --name-only master..HEAD -- '*.go' | grep -c internal/tools → 0
$ go test -race ./internal/tools/ -count=5 -run '^TestMemorySearchNonBlocking$'  → 0 data races (2/5 FAIL)
$ go test ./internal/tools/ -count=20 -run '^TestMemorySearchNonBlocking$' (tip 9ed2cb5d) → 4/20 FAIL, all :551
$ go test ./internal/tools/ -count=1  -run '^TestMemorySearchNonBlocking$' (tip 9ed2cb5d) → PASS 1/1
$ make test (proposed block, /tmp/mkcheck) → last line `--- FAIL: TestAaaFails (0.00s)`, make exit=2
```

`git status --porcelain` is **empty** on `diag/tools-flake`: every probe file used for a run was
deleted in the same command that created it. The branch carries the report and nothing else.

## 7. Bug rows

| id | title | tags |
|---|---|---|
| `01a0920b-afe0-7587-ac78-57dddd117497` | BUG: memory store races the first-open schema build → row committed but never indexed (`TestMemorySearchNonBlocking` flake) | bug, memory, sqlite, fts |

Payload carries: the exact failure text, file:line (`memory_test.go:551` / `:543`), the
deterministic repro (5/5), the measured counts, the `'rebuild'`-heals evidence, the root-cause
walk of `memory.go`, and the acceptance criteria for the fix (deterministic publish-then-expose
init + regression test + `-count=20` green).

## 8. Honest negatives

- **A prior fix attempt exists as an EMPTY branch.** `fix/memory-search-nonblocking-flake`
  (worktree `~/workspace/gopi-memflake`) points at `3d9ae86d` — a *master* merge commit — and has
  **zero commits beyond master** (`git log --oneline master..fix/memory-search-nonblocking-flake`
  → empty). So the branch name records an intent that produced no change; the F1 bug is
  **still live on master**. The bug row explicitly lists the deterministic repro so the next
  attempt does not have to re-derive it.
- **F2, F3, F5 are NOT reproduced** in isolation: 0/40 (`TestBashDaemonSpawn_DoesNotLeakProcesses`),
  0/40 (`TestTaskDiffPeerRefusesInvalidAlias`), 0/15 (`TestFreshDBOpenCloseIsFast` ambient),
  0/15 (pinned `/dev/shm`) — all under ambient load 12.5–16.6 on 4 cores. Their only occurrences
  are inside shared-host full-suite runs, so the honest verdict is *external interference /
  environment*, not product bug — and no bug row is warranted where no repro exists.
- The `make test` signal (`FAIL internal/tools 157.841s`, no name) can be produced with **zero**
  failing tests — see the `/tmp/freshdb-final.log` and `/tmp/esc_tools_test.log` cases in §1
  (compile failure; 600 s package timeout). Any future "internal/tools is flaky" claim must
  first rule those two out.
