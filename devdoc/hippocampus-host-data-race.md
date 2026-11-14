<!-- doc-class: historical -->
# Task Report: hippocampus host data race (unlocked in-flight hand-off)

**Branch:** `fix/hippocampus-race2` (worktree `~/workspace/gopi-race2`)
**Commit:** tip of `fix/hippocampus-race2` (`git log -1 fix/hippocampus-race2` is
authoritative — docs-only amends may advance the hash without touching code),
rebased onto current `master` `d51d437` (the work happened on base `e0932ee`;
master advanced) — so the rebase was clean and the diff vs master is
exactly this task's four files). Blob identity across both rebases proves no
production byte moved: `internal/agent/hippocampus.go` = `008d4a8602e7f2d13f6ec3e5088569800f5a697f`
and `internal/agent/hippocampus_test.go` = `4aaf3c22b6acbe1b978f0980dc477d2ad411784a`
on the pre-rebase tip, the re-tested tip and the final tip alike — so the
verification table below transfers verbatim to the delivered commit.
**Status:** fixed, verified, committed.
**Bug type:** pre-existing race on master (NOT introduced by `fix/test-env-isolation`).

## Symptom

```
go test ./internal/agent/ -race -run 'TestHippocampus_DefaultOffLeavesBehaviourUnchanged' -count=1
=> WARNING: DATA RACE
   --- FAIL: TestHippocampus_DefaultOffLeavesBehaviourUnchanged
   testing.go:1490: race detected during execution of test
```

Verbatim stacks (reproduced on the untouched baseline before the fix):

```
Read at 0x… by goroutine 8:
  (*hippoScheduler).AtRoundBoundary()  hippocampus.go:518   <- run goroutine, UNLOCKED
Previous write at 0x… by goroutine 146:
  (*hippoScheduler).enqueue()          hippocampus.go:636   <- recall goroutine, under mu
```

## Root cause (5-why)

1. **Where** does the race live? Between the run goroutine (`AtRoundBoundary` /
   `EndOfRun`) and the recall goroutine (`launchRecall.func1` → `enqueue`).
2. **What** is the shared memory? The scheduler fields `inflight` / `cancel`
   (and, on the same hand-off path, `delivered`).
3. **How** can both touch it? The recall goroutine, on finishing, calls
   `enqueue` which clears `h.cancel` / `h.inflight` **under `mu`**; the run
   goroutine read/wrote the same fields **without** `mu` at
   `AtRoundBoundary` (518-528), `EndOfRun` (579-592) and `launchRecall`
   (610-611).
4. **Why was the lock not taken?** The scheduler *has* locked helpers
   (`takeInflight`/`clearInflight`) — they were **never called**. The fields
   were reached directly at the call sites, so the mutex covered only one side
   of the hand-off. The slot's `done` channel only orders the *close*, not the
   sibling fields — **the channel is not the lock**.
5. **Why did the test suite not notice?** The interleaving needs a recall that
   is still in flight when the drain runs; only `-race` sees unsynchronized
   access, and only the E2E loop test (`DefaultOffLeavesBehaviourUnchanged`,
   which runs the real `runEngine` with a stub organ) produces it.

A **second, latent** race sat on the same path: `slot.brief = brief` (622) —
the recall goroutine wrote the slot while `AtRoundBoundary` read `slot != nil`
from the run goroutine. It is the same field pair and was fixed together.

The `-race` acceptance run then exposed **two more** instances of the same
class, both fixed in this change:

1. **Production:** the recall goroutine called `h.recall(...)` (line 640) —
   reading the *binding* field from its own goroutine while
   `SetHippocampusRecall` (the merge-time seam) can rebind it. The goroutine now
   closes over an `organ` captured once through `recallFn()` under `mu`;
   `SetHippocampusRecall` writes through `setRecall()`.
2. **Test:** `if calls == 0` in `TestHippocampus_Lv0_ArchiveOnly` read the
   counter with a plain load while `stubRecall` wrote it with
   `atomic.AddInt32` — a race *in the test* (a test is part of the race
   surface, not exempt from it). Now `atomic.LoadInt32`.

## Fix

Enforce one door to every shared field — no call site touches `inflight`,
`cancel`, `pending`, `delivered`, `started`, `ended` directly:

| Accessor | Purpose |
|---|---|
| `setInflight(slot, cancel)` | install the in-flight recall (locked) |
| `takeInflight()` | detach slot+cancel under mu, wait **outside** it |
| `stopInflight()` | abort + drop whatever is left (locked) |
| `enqueue(cancel, brief)` | recall goroutine hand-off (already locked) |
| `drainPending()` | drain queue (already locked) |
| `deliver(round)` | record the drained round (was a bare write) |
| `beginEnd()` | EndOfRun latch (was a bare read+write pair) |

Also removed `brief *hippocampusBrief` from `hippoRecallSlot`: the brief now
travels **only** through the locked queue (one hand-off path), so no second
unsynchronized copy exists. Semantic delta is nil — the detached slot is not
used as a result slot; the cancellation covered by `stopInflight` replaces the
duplicate cancel in `EndOfRun`.

## Verification

Repro test added: `TestHippocampus_Race_RecallHandoffUnderConcurrentDrain`
(`internal/agent/hippocampus_test.go`). It parks the stub organ on a channel
while the drain holds the slot, so both goroutines provably touch the fields;
before the fix the detector reports the race, after it the test is green.

```
# PRE-FIX (baseline hippocampus.go, new test) — RED
WARNING: DATA RACE
  Write at … enqueue() hippocampus.go:636   <- recall goroutine
  Previous read at … AtRoundBoundary() hippocampus.go:518  <- run goroutine
--- FAIL: TestHippocampus_Race_RecallHandoffUnderConcurrentDrain

# POST-FIX — GREEN
go test ./internal/agent/ -race -run 'TestHippocampus_Race_RecallHandoffUnderConcurrentDrain' -count=1
ok  github.com/nick/gopi/internal/agent  1.421s

go test ./internal/agent/ -race -run 'TestHippocampus_DefaultOffLeavesBehaviourUnchanged' -count=1
ok  github.com/nick/gopi/internal/agent  6.271s      <- the original failing repro
```

| Check | Command | Result |
|---|---|---|
| Build | `CGO_ENABLED=0 go build ./...` | `build=OK` |
| Vet | `go vet ./internal/agent/` | `vet=OK` |
| Regression test | `go test ./internal/agent/ -race -run TestHippocampus_Race_RecallHandoffUnderConcurrentDrain -count=1` | `ok 1.376s` |
| **Acceptance repro** | `go test ./internal/agent/ -race -run TestHippocampus_DefaultOffLeavesBehaviourUnchanged -count=1` | `ok 3.079s` (was `WARNING: DATA RACE` + FAIL 13.70s) |
| **Acceptance, whole pkg** | `go test ./internal/agent/ -race -count=1` | `ok 110.400s` — **no DATA RACE** |
| Full repo (race) | `go test ./... -race -count=1` | **0 occurrences of `DATA RACE`**; every package `ok` except the pre-existing `internal/tools` hang below |

All of the above ran on the rebased tree (master `f0c8974` + this fix) — the
final commit differs from the tested revision only in these two devdoc files
(proved: `git rev-parse HEAD:internal/agent/hippocampus.go
<tested-rev>:internal/agent/hippocampus.go` → identical blob `008d4a8…`).

`-race` runs take ~2 min and print nothing until the package finishes, so they
were run through a background job — a plain bash call is killed by the 120s
no-output watchdog and loses the verdict.

## Not Done

- The other pre-existing finding from `devdoc/test-env-isolation.md` (NOT the
  env leak itself — that shipped on `fix/test-env-isolation`) is untouched.
- `delivered`/`recallRound()` are now lock-protected for future callers; no
  reader of `recallRound` exists yet, so nothing changed behaviourally there.
- No load/stress run beyond the deterministic repro (the randomized
  interleaving is the detector's job; `-count=50` not run).
- **Separate pre-existing bug found by the acceptance sweep, filed not fixed
  (Rule 15b): the `internal/tools` whole-package test run hangs.**
  `go test ./internal/tools/ -count=1 -timeout 300s` → `panic: test timed out`,
  with `TestFailedViewHidesArchived` blocked in `TaskUpdate` →
  `taskByID` (`tasks.go:934`). It reproduces WITHOUT `-race`, and
  `internal/tools` is byte-identical to master at this HEAD (tree hash
  `f0584cf…` on both sides, and `go list -deps ./internal/tools` has **zero**
  dependency edge to `internal/agent`) — so it cannot be caused by this fix.
  The named test alone passes (`-race` 4.475s), so the hang needs the whole
  package: a cross-test interaction (likely an ambient `GOPI_MEMORY_DB`/DB
  handle left behind by a predecessor — the same environment class as
  `devdoc/test-env-isolation.md`). Filed as bug task
  `01a089cf-7f67-79ea-b3da-c9194c3a740e` with the repro, the goroutine shape
  and the isolation data point.

## Notes for future agents

- The contract is written on the `mu` field: **every field the recall goroutine
  can touch is lock-only**. If you add a field the organ goroutine writes,
  guard it the same way — a channel close orders its own wake-up, never the
  sibling fields.
- The unlock-wait-recheck shape matters: wait on `slot.done` **outside** `mu`
  (waiting under the lock serializes the hand-off), and never use the detached
  slot as a result carrier.
- Worktree note: an earlier attempt at `~/workspace/gopi-race-fix` was removed
  by an external cleanup mid-task; this fix lives in `~/workspace/gopi-race2`.
