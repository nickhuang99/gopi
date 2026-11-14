<!-- doc-class: historical -->
# The goroutine-leak red-proof measures a quantity the test OWNS

**Task:** `01a0feda` · **branch:** `task/01a0feda175e72b79f5ac817730ac679` ·
**file changed:** `cmd/agent/chat_prefetch_test.go` (test-only) ·
**epic:** `TestPrefetchGoroutineSettleGapDetectsALiveLeak` flaked 10/12 on master.

**The two-line version.** A goroutine-leak assertion built on
`runtime.NumGoroutine() - baseline` measures a quantity the test does NOT own:
two whole-process counts, from which any *unrelated* goroutine can depart (or
arrive) in the window, cancelling the +1 the test just created. Replace it with a
count of the goroutines carrying a **frame marker for the code under test** —
a signature only that code produces. Measured here: the pre-fix form failed
3/12; the signature form is green `-count=12` (repeated), and stays red when the
poll is stubbed or when either marker is rotted.

*(The executors' own task report is the same document — this devdoc is its
committed copy, so a later agent reads the reasoning without the session dir.)*


# Task Report: the goroutine-leak red-proof measures a quantity the test OWNS
## (cmd/agent `TestPrefetchGoroutineSettleGapDetectsALiveLeak`, task 01a0feda)


# Task Report: the goroutine-leak red-proof measures a quantity the test OWNS
## (cmd/agent `TestPrefetchGoroutineSettleGapDetectsALiveLeak`, task 01a0feda)


# Task Report: the goroutine-leak red-proof measures a quantity the test OWNS
## (cmd/agent `TestPrefetchGoroutineSettleGapDetectsALiveLeak`, task 01a0feda)

## Completed

`cmd/agent/chat_prefetch_test.go` only (no production file touched).

1. **Root cause, measured (Rule 10).** The red-proof compared
   `runtime.NumGoroutine()` against a baseline read *before* the goroutine under
   test was spawned. That is a DIFFERENCE OF TWO WHOLE-PROCESS COUNTS: every
   goroutine in the binary — not only the one being watched — enters it with
   either sign. Instrumented on the reproduced flake (12 rounds of the shipped
   file, `-v`), the losing direction was caught with both dumps in hand:
   `baseline=5 now=4`, the two departing goroutines named
   `database/sql.(*DB).connectionOpener` (a previous test's pool), while the
   parked spy was named in the SAME dump. `gap = 4-5 ≤ 0` → "settled" printed
   while the spy sat on the stack. Reproduced **3/12** on the pristine file, and
   **5/8** with `-run '<the blocked-ledger case>|<the red-proof>'` (a
   `*sql.DB` opener alive at the read), so the cross-test direction is
   demonstrable, not hypothetical. **The briefing's figure was 10/12; this host
   measured 3/12** on the same pristine file (5/8 with the sibling case in the
   same binary). The direction and the mechanism are the same; the RATE is
   host- and load-dependent, which is itself consistent with a race whose second
   participant is unrelated goroutines. The 3/12 number is the one this run
   observed.

2. **The fix: a goroutine-count SIGNATURE, not a process delta.**
   `prefetchGoroutinesWithFrame(frame)` counts the live goroutines whose stack
   carries a frame marker. `prefetchGoroutineSettleGap(frame, want, within)`
   polls that count (never `time.Sleep`; `runtime.Gosched` between reads).
   Nothing else in the binary can move a subset keyed on a named function:
   - `prefetchLoopFrame = "cmd/agent.(*chatPrefetch).loop"` — the executor's own
     goroutine, the corpus the OFF-path "started none" claim is made against.
   - `prefetchRunSpyFrame = "cmd/agent.prefetchRunSpyGoroutine"` — the spy the
     red-proof parks, now a NAMED function so the measured signature is a
     quantity *this test run owns*.
   All three durations/sizes are named constants (Rule 1:
   `prefetchOffGoroutineSettleWait`, `prefetchOffGoroutineLeakProbe`,
   `prefetchGoroutineAppearWait`, `prefetchGoroutineDumpBytes`).

3. **A second, deeper flake found the same way, and fixed in the same shape.**
   The first cut of the red-proof read the spy's visibility ONCE, immediately
   after `go`. Measured with a throwaway diagnostic case: `-count=12` → the spy
   was ABSENT from **5 of 12** dumps (`rawContains=false`, dump headers=7),
   because an unscheduled goroutine has no frames to report yet. So the
   red-proof's precondition is itself a bounded poll
   (`prefetchAwaitFrameCount(..., prefetchGoroutineAppearWait)`), and the failure
   it reports names that fact. Same defect class, one level down — a single
   instantaneous read asserting a scheduling fact.

4. **A claim of ABSENCE is paired with a proof that the marker RESOLVES.**
   A frame marker matching nothing satisfies "the OFF path left no loop
   goroutine" whether or not the feature exists. New sibling
   `TestPrefetchOnStartsExactlyOneLoopGoroutineAndStopReclaimsIt` pins the other
   direction on the real executor: 0 loop goroutines before `Start`, exactly 1
   after (bounded poll), 0 after `Stop` returns — and it FAILS when
   `prefetchLoopFrame` is rotted (measured below).

5. **`testdata/flakes.tsv`: no row added, deliberately.** This test is now
   deterministic by construction and no longer qualifies for the registry: a
   registry row would be a claim that the flake is still live (the registry's own
   gate reads it as such), and the honest state after this change is that the
   measurement was repaired at the root. Evidence of non-determinism is in this
   report (`3/12`, `5/8`), not in the registry.

## Package-level gate (`make test-budget PKGS='./cmd/agent'`): NOT green — honest

Two whole-package runs were made after the change; both exited 1, and I state
plainly that **the package-level gate is red**, with the failure named and
attributed:

| run | stamp | load (start→end) | failing cases |
|---|---|---|---|
| 1 | 07:27 (change UNCOMMITTED) | 4.9→11.9 | `TestPrefetchGoroutineSettleGapDetectsALiveLeak`, `TestSyncBarePeersTestCoversEveryChangedFile`, `TestRunCloseoutReapOnTheRealRunPath` |
| 2 | 07:48 (rev `7c9e8d7f`) | 6.8→15.9 | `TestWorkerDaemon_StopEndsTheDaemon` (21.57s) |

Every red in run 1 that was named against this file is explained by the tree's
state at that moment, and none of run 2's is this task's surface:

- **`TestPrefetchGoroutineSettleGapDetectsALiveLeak`** (run 1, this task's test)
  is GREEN on the committed rev: standalone `-count=1` → `ok … 5.330s`, and
  `-count=12` three consecutive times → `ok 8.394s / 10.336s / 9.941s`
  (`post-commit-gates.log`). Run 1 was taken with this change **uncommitted**
  (its 07:27 stamp precedes the commit).
- **`TestSyncBarePeersTestCoversEveryChangedFile`** is a DIFF-BASED RATCHET over
  the WORKING TREE (`git diff --name-only HEAD -- cmd/agent/`). While my change
  was uncommitted it saw exactly one changed file — mine — and reported
  `this branch changed cmd/agent/chat_prefetch_test.go, which is not in this
  test's coverage lists`. Committed, the diff is empty and the case PASSES:
  `go test ./cmd/agent/ -run 'TestSyncBarePeersTestCoversEveryChangedFile'
  -count=1` → `ok … 0.801s`. It fires on a file being TOUCHED, not on what the
  file does (this file asserts nothing about the bare-`--peers` grammar), so the
  right response is the commit — **not** an entry in
  `syncPeersBareUnrelatedFiles`, a list that belongs to the peers task.
- **`TestRunCloseoutReapOnTheRealRunPath`** failed only inside run 1's
  whole-package execution and PASSES standalone on the same tree
  (`-count=1` → `ok … 4.052s`).
- **`TestWorkerDaemon_StopEndsTheDaemon`** (run 2) is a daemon start/stop case
  that took 21.57s — near its budget — on a 4-core host at load 15.9 with other
  sessions' `test-budget` runs in flight. Measured standalone afterwards, at
  load 12.94: `go test ./cmd/agent/ -run 'TestWorkerDaemon_StopEndsTheDaemon'
  -count=1` → `ok … 17.610s`. This change touches no daemon code, adds no
  process to the package, and its only new goroutine (the spy) is joined before
  the test returns.

So the honest package-level statement is: run 1's three reds are a dirty-tree
artefact (two) plus this task's own case before its commit (one, green now), and
run 2's single red is a load-sensitive daemon case that passes standalone. The
acceptance item "`make test-budget PKGS='./cmd/agent'` green" is therefore **NOT
met on this host under the load present during this run**; the scope-specific
evidence (three green `-count=12` runs, the three red-proofs failing by name,
stylegate, `vet`, `gofmt`) is in `## Evidence`.

## Files

Deliverable (committed on branch `task/01a0feda175e72b79f5ac817730ac679`):

- `cmd/agent/chat_prefetch_test.go` — the measurement helper + the two goroutine
  cases (`7c9e8d7f`).
- `devdoc/goroutine-signature-red-proof.md` — the reasoning, the numbers and this
  report's committed copy; row added to `devdoc/DOC-INDEX.tsv` (`gopi doccheck`
  clean).

Evidence (session dir `/home/user/.gopi/sessions/01a0feda-175e-72b7-9f5a-c817730ac679/`,
Rule 30 — nothing of this kind inside the tree): `prefetch-flake-repro.log` (the
pre-fix flake), `count12-fail-detail.log`, `proofs.sh` + `proofs-final.log` (the
mutation harness and its results), `chat_prefetch_test.go.orig/.fixed/.head` (the
pristine, work-in-progress and committed copies), `post-commit-gates.log`,
`test-budget-cmd-agent.log`, `test-budget-final.log`, `stylegate-list.txt`,
`task-report.md` (this file, at `$GOPI_TASK_REPORT_PATH`).

## Not Done

- No production file changed, so no `Ran:` invocation of a *product* surface: the
  surface this task changed is a test-file measurement helper, and it is executed
  for real (not a unit test of it in isolation) in every run below.
- The other `time.Sleep` hits stylegate lists (5 pre-existing, all outside
  `chat_prefetch_test.go`) are untouched — they are not this task's, and moving
  their ceiling would be exactly the "weaken the gate" move the task forbids.
- The ON-path sibling does not assert anything about *what the loop does* (it
  never claims a query: the ledger path it pins does not exist) — it is a
  goroutine-lifecycle case, and the query path is already covered by
  `TestPrefetchAdoptAppendsToMessageNeverToDisplay`.

## Verification

- Build: `go vet ./cmd/agent/` → clean (no output); `gofmt -l cmd/agent/chat_prefetch_test.go` → no output.
- Tests:
  - `go test ./cmd/agent/ -run TestPrefetch -count=12` on the COMMITTED rev
    `7c9e8d7f`, three consecutive runs → `ok … 8.394s` / `10.336s` / `9.941s`;
    `-count=1` → `ok … 5.330s` (`post-commit-gates.log`). The pre-fix file
    reproduced `--- FAIL: TestPrefetchGoroutineSettleGapDetectsALiveLeak` 3/12.
  - `make test-budget PKGS='./cmd/agent'` → (see `## Files`:
    `test-budget-final.log`; the earlier run's extra reds are explained above).
  - stylegate: `go run ./internal/stylegate/cmd/stylegate -root . -list` → 79
    lines, **no hit mentioning `chat_prefetch`** (the 5 remaining `test-sleep`
    hits are pre-existing and outside this file; no ceiling moved, no marker
    added).
- Ran (the exact invocations; logs in
  `/home/user/.gopi/sessions/01a0feda-175e-72b7-9f5a-c817730ac679/`):
  - `go test ./cmd/agent/ -run 'TestPrefetch' -count=12` — pre-fix: 3/12 runs red,
    each naming `TestPrefetchGoroutineSettleGapDetectsALiveLeak`
    (`prefetch-flake-repro.log`).
  - `bash proofs.sh` — the re-runnable mutation harness, run LAST against the
    frozen copy of the committed file (`chat_prefetch_test.go.head`, byte-identical
    to `HEAD`, so the mutations are scripted edits, never a hand-edit), restoring
    it after each (`proofs-final.log`):
    - control: `-count=1` → `ok 2.353s`; `-count=12` → `ok 7.690s`; restored →
      `ok 1.029s`.
    - **Red-proof A (rubber stamp)** — `prefetchGoroutineSettleGap` stubbed to
      `return 0` unconditionally → `-count=3` → **3/3 FAIL**, every one naming
      `TestPrefetchGoroutineSettleGapDetectsALiveLeak`, with
      `chat_prefetch_test.go:441: the poll reported a settled count while a
      goroutine was alive — it cannot detect the leak it exists for`.
    - **Red-proof B (rotted loop marker)** — `prefetchLoopFrame` →
      `cmd/agent.(*chatPrefetch).noSuchLoopMethod` →
      `--- FAIL: TestPrefetchOnStartsExactlyOneLoopGoroutineAndStopReclaimsIt`,
      `chat_prefetch_test.go:95: Start must run exactly ONE prefetch loop
      goroutine, but frame "…noSuchLoopMethod" matched 0 — a marker that resolves
      to nothing would make the OFF path's claim of absence vacuous`.
    - **Red-proof C (rotted spy marker)** — `prefetchRunSpyFrame` →
      `cmd/agent.noSuchSpyFunction` → `--- FAIL:
      TestPrefetchGoroutineSettleGapDetectsALiveLeak`,
      `chat_prefetch_test.go:431: the parked spy goroutine is not visible in the
      frame signature "cmd/agent.noSuchSpyFunction" — the measurement below would
      assert nothing`.
  - `bash /home/user/.gopi/sessions/01a0feda-175e-72b7-9f5a-c817730ac679/proofs.sh`
    (re-runnable, restores the file after each mutation).
- Result: see the four `## Evidence` lines.

## Evidence (self-filed)

- Pre-fix flake, observed: `grep -c '^--- FAIL' prefetch-flake-repro.log` → `3`, all
  `TestPrefetchGoroutineSettleGapDetectsALiveLeak`; and with the sibling case in the
  same binary (`-run 'TestPrefetchGivesUpOnABlockedLedger|TestPrefetchGoroutineSettleGapDetectsALiveLeak' -count=8`) → `5` FAIL.
- Signature attribution, observed in the failing dump: `baseline=5 now=4`, base names
  include `database/sql.(*DB).connectionOpener(...)` twice, `nowNames` include
  `TestPrefetchGoroutineSettleGapDetectsALiveLeak.func1()` — the watched goroutine alive
  while `gap ≤ 0`.
- Presence race, observed: throwaway diagnostic over 12 iterations →
  `misses=5/12`, each with `rawContains=false bytes=2177 headers=7`.
- Post-fix: `go test ./cmd/agent/ -run 'TestPrefetch' -count=12` → `ok … 6.657s`; 3 more
  `-count=12` runs → `ok` (all three), `suite goroutine sweep: parked repository
  goroutines: 0`.
- stylegate: `go run ./internal/stylegate/cmd/stylegate -root . -list | grep -c chat_prefetch` → `0`.
- Worktree cleanliness: `git status --porcelain` before the commit prints only
  `M cmd/agent/chat_prefetch_test.go` (the deliverable itself) — after `git add` of that
  one path and the commit, it prints nothing.

## Post-commit gate runs (the numbers on the committed rev)

The numbers above were taken with the file uncommitted. Re-measured on the
committed rev, in the same worktree:

- `go test ./cmd/agent/ -run 'TestPrefetch' -count=12` — on `7c9e8d7f` and again
  on the final `98070610`: `ok … 8.394s`, `ok … 10.336s`, `ok … 9.941s` (three
  consecutive) and `ok … 6.856s`; `-count=1` → `ok … 5.330s`
  (`post-commit-gates.log`).
- `go test ./cmd/agent/ -run 'TestWorkerDaemon_StopEndsTheDaemon' -count=1` →
  `ok … 17.610s` (run 2's only red, at load 12.94).
- `go test ./cmd/agent/ -run 'TestSyncBarePeersTestCoversEveryChangedFile' -count=1`
  → `ok … 0.801s`. That case is a diff-based ratchet over the WORKING TREE: while
  this change was uncommitted it saw my file as "changed on this branch" and
  reported it outside its coverage lists; committed, the diff is empty and it
  passes. It fires on a file being TOUCHED, not on what the file does, so the
  fix is the commit — not an entry in `syncPeersBareUnrelatedFiles`, which
  belongs to the peers task.
- `go test ./cmd/agent/ -run 'TestRunCloseoutReapOnTheRealRunPath' -count=1` →
  `ok … 4.052s` (it had failed inside the whole-package run at load 9.9–11.7 on
  4 cores, with two other `test-budget` runs in flight).
