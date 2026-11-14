<!-- doc-class: historical -->
# memory-prefetch: the segment identity, and who gets blamed for a held ledger

**Task** `01a11604af56794a95ec7432045eb6c6` (HP, feature layer — Rule 28) ·
**Class** verdict + fix report for two flaky assertions in
`cmd/agent/chat_prefetch_segments_test.go`.

Both flakes were investigated to a mechanism before anything was changed
(Rule 10 #3: root cause before parameters — no constant was enlarged, no
`time.Sleep` was added, no assertion was emptied). **Both are REAL defects in
`cmd/agent/chat_prefetch.go`, not test defects.** The failing assertions were
right; the code was reporting a measurement it had not made.

## Completed

### 1. Line 481 — `TestPrefetchBusyIsRetriedWithinTheSameBudget`: the give-up was misattributed. VERDICT: real defect.

**Mechanism (measured, not inferred).** Under a ledger a writer holds
`BEGIN EXCLUSIVE`, one attempt of the shared retrieval costs TWO busy timeouts:

```
A shared(query) under lock: 485-512ms  → the walk (ladder) 252-256ms, then
                                          the OR fallback's own refusal ~238-251ms
D attempt 1 (+505ms) · attempt 2 (+978ms) · attempt 3 (+1522ms)
  → 3 attempts × ~500ms > 1500ms, so the THIRD attempt is cut by the deadline
E lookup: outcome=gave-up ms=1520 attempts=3 err="prefetch gave up after 1500ms
  (query): context deadline exceeded"  → isBusyErr(...) == FALSE
```

Both facts are true at the end of a locked lookup — the ledger WAS locked
(`SQLITE_BUSY`, every attempt) and the 1500ms budget WAS expired. The report
chose the deadline because the `switch` tested `runCtx.Err() != nil` before
`isBusyErr(err)`. That is the misdiagnosis the task was filed on: it tells the
reader "the search was slow" about a ledger that was simply held. Whether the
budget ran out first is decided by scheduling, which is why the same test was
green on an idle host and red at high load — **the assertion was not flaky, the
report was.**

**Fix (in the origin, both halves).**

- `prefetchQueryShared(…, skipLadder bool)` + the retry loop's memory: once an
  attempt has met the lock, the ladder walk — which costs its own
  `prefetchLadderBudgetMs` (252ms measured) and can only learn the same refusal
  again while the writer still holds the ledger — is skipped, so the fallback
  meets the lock once. Measured: attempts now finish at ~850-1090ms, inside the
  budget, so what the record reports is the refusal.
- The classification at the query seam tests `isBusyErr(err)` **first**: the
  refusal is the more specific, actionable fact and it wins when both hold. The
  budget is still respected — the give-up happens when it did either way.
- The same reordering was applied to the fresh-handle dial arm (a dial refused by
  a lock used to fall through to the `runCtx.Err()` arm too), which also aligns
  the two arms on one precedence instead of two.

### 2. Line 137 — `TestPrefetchRecordCarriesSegmentTiming`: the residual. VERDICT: real defect.

**Mechanism.** The record's `ms` was taken from the **caller's** clock
(`chatPrefetch.fetch` wrapped the lookup in its own `time.Since`), `seg_stat_ms`
from a clock started *after* path resolution, and `seg_open_ms` from a third
started at the dial. Three separately floored spans, each losing part of a
millisecond in the SAME direction, against a windows-length outer clock — so the
sum of the parts sat *below* the whole by up to +2ms, and the test's ±1ms
tolerance (which the code's own docstring called a rounding unit) absorbed the
error on an idle host and failed under load.

**Fix: one clock, and the third segment is its remainder.**

```go
lookupStart := time.Now()                 // before path resolution, before stat
res := prefetchLookupPhased(ctx, query, cfg, h, lookupStart)
res.ElapsedMs = time.Since(lookupStart).Milliseconds()
res.Seg.QueryMs = res.ElapsedMs - res.Seg.StatMs - res.Seg.OpenMs
```

`prefetchResult.ElapsedMs` is the one source of truth for the lookup's duration
and `prefetchRecord.Ms` now takes it from there, never from a caller's outer
clock; `chatPrefetch.fetch` no longer opens a clock of its own. The identity
`ms == seg_stat_ms + seg_open_ms + seg_query_ms` is now exact **by
construction**, so the test asserts equality instead of a tolerance — the
tolerance is what let the defect hide.

### 3. Assertion aligned (not weakened) and a stale constant retired

- The residual check is now `sum != lookup.Ms` (exact). The comment states why a
  tolerance is the wrong instrument here.
- `prefetchSegUnitMs` (the "rounding unit" the tolerance was written against) is
  retired; the unit is expressed by the code, and the identity is the property a
  reader checks. `devdoc/memory-prefetch-giveup-diagnosis.md` quoted the old
  tolerance as a rationale and its sentence was corrected in the same change
  (Rule 13: a constant that no longer exists must not be cited).

## Not Done

- **The fresh (unwarmed-handle) shape is not retried by the executor.** Under a
  lock, `prefetchOpenHandle` on the session path is attempted once; the lookup
  then falls back to a fresh handle and reports `segment=open`. This is
  pre-existing behaviour, unrelated to either defect, and unchanged by this
  task.
- **`attempts` still does not include the dial's attempts** on the session path.
  Considered (a `dialLockBusy` field on the handle was implemented and then
  removed): neither defect needs it, no assertion reads it, and it would add a
  handle-lifetime fact for a report-only gain (Rule 35: nothing new unless it is
  earned). Recorded here as a known gap, not as a fix.
- **Latency of a locked lookup is not bounded below the budget.** The lock now
  costs ~850-1090ms rather than ~1500ms (the ladder skip), but the retry loop
  still spends the full allowance against a held ledger. Making it give up sooner
  would change the retry contract for a momentary lock, which is the case the
  retries exist for; that is a design decision for a separate task.

## Verification

- **Build:** `go build ./...` → OK (exit 0).
- **Tests:**
  `go test ./cmd/agent/ -run 'TestPrefetch' -count=1` → ok, every
  `TestPrefetch*` PASS (incl. `TestPrefetchRecordCarriesSegmentTiming`,
  `TestPrefetchBusyIsRetriedWithinTheSameBudget`,
  `TestPrefetchSegmentNamesThePhaseThatFailed`).
- **Ran (the acceptance command, 5 qualifying rounds at load > 12; a further 8
  rounds at 9.4-11.7 during the watcher's wait):**

  ```
  round 1 load_before=13.40 -> ok  github.com/nick/gopi/cmd/agent  2.874s
  round 2 load_before=13.30 -> ok  ...  2.396s
  round 3 load_before=12.09 -> ok  ...  2.673s
  round 4 load_before=11.85 -> ok  ...  2.635s
  round 5 load_before=11.14 -> ok  ...  2.617s
  round 6 load_before=10.49 -> ok  ...  2.559s
  (then 8 more rounds, load_before 8.96 … 11.68, all ok, 0 FAIL)
  ```

  Invocation, verbatim:

  ```
  go test ./cmd/agent/ -run 'TestPrefetchBusyIsRetriedWithinTheSameBudget|TestPrefetchRecordCarriesSegmentTiming' -count=1
  ```
- **Result (the A/B, on the same probe binary, one host, one writer-held lock;
  the probe was a throwaway file deleted before the commit — Rule 30):**

  | measurement | pre-fix | post-fix |
  |---|---|---|
  | 481: `isBusyErr(give-up err) == false` | **19 / 30** | **0 / 30** |
  | 481: err string when red | `prefetch gave up after 1500ms (query): context deadline exceeded` | — (no longer produced for a held ledger) |
  | 481: ms (session, locked) | 1274-1572 | 848-1090 |
  | 481: attempts | 3 | 3 |
  | 137 session residual (n=60) | `{0:58, +1:2}`, ms 0-1 | `{0:60}`, ms 0-13 |
  | 137 fresh residual (n=20) | `{0:17, +1:3}`, ms 2-3 | `{0:20}`, ms 2-12 |

  Loads, recorded (Rule 27 — no load was created; the host's own average is
  printed around every run): 8.24 / 11.63 / 12.07 / 13.30 / 14.17 / 15.14.
- **Commit:** see `## Evidence` below for the exact tip; the branch is
  `task/01a11604af56794a95ec7432045eb6c6`. **This commit never reached a ref** —
  see the re-verification section at the end for the reachability proof and the
  recovery (`153d7b054` on branch `task/01a1163a44337cbdabb7c4c6e8c66ce0`).

## Evidence (self-filed)

- `cat /proc/loadavg` immediately before the A/B (pre-fix): `15.14 14.51 14.16`;
  immediately before the post-fix run: `8.24 12.07 13.30`.
- Pre-fix, 30 rounds: `481 session summary: isBusyErr=false 19/30`, with round 0
  printing `ms=1572 … err="prefetch gave up after 1500ms (query): context
  deadline exceeded"`.
- Post-fix, 30 rounds: `481 session summary: isBusyErr=false 0/30`.
- Pre-fix residual: `137 session n=60 … max=1 hist=map[0:58 1:2]` and
  `137 fresh n=20 … max=1 hist=map[0:17 1:3]`; post-fix `hist=map[0:60]` and
  `hist=map[0:20]`.
- Acceptance rounds: 6 consecutive rounds `ok` at load 13.40 → 10.49, plus 8 more
  `ok` rounds; `grep -c FAIL` over the whole log = 0.
- `git status --porcelain` in the run's worktree after the probe files were
  deleted: only the two intended files (`M cmd/agent/chat_prefetch.go`,
  `M cmd/agent/chat_prefetch_segments_test.go`) before the commit, and empty
  after it.

## Re-verification on peerbox, 2026-10-07 (task `01a1163a44337cbdabb7c4c6e8c66ce0`)

**Provenance correction — this fix was DANGLING.** `a9d1f02dc`, the commit that
produced this devdoc, never reached a ref: `git branch -a --contains a9d1f02dc`
is empty, it is not an ancestor of `master` nor of the branch that filed the
follow-up (its parent `bb6ee97f9` is), and the tree still carried the unfixed
`chat_prefetch.go`. Recovered with
`git show a9d1f02dc -- <path> | git apply -3` (one conflict, in newer
`Hits`/`Ms` label code) and re-landed as `153d7b054` on branch
`task/01a1163a44337cbdabb7c4c6e8c66ce0`. Memory #8584/#8606/#8612 describe this
fix as *landed* — on this tree it was not. Reachability, never object existence,
is the test: `git merge-base --is-ancestor <sha> HEAD`.

**The give-up record observed directly (Rule 31).** The upstream run proved the
flip statistically (19/30 → 0/30). Here the ambient load never reached 14 (peak
~13.2) and Rule 27 forbids generating it, so verification took the direct route:
a temporary in-package probe (`//go:build probeignore`, deleted before the
commit) called `prefetchLookupDetailed` against a ledger held `BEGIN EXCLUSIVE`,
the read-only handle warmed BEFORE the lock, and printed the returned record.
5 of 5 runs, load ~11:

```
PROBE-RESULT outcome="gave-up" segment="query" attempts=3 stat=0 open=0 query=1111 ms=1111 sum=1111 elapsed=1.111019249s overBudget=false isBusy=true queryOnly=1 busyTimeout=250 handle="session" err=prefetch query: database is locked: vtable constructor failed: distilled_qa_fts (5) (SQLITE_BUSY)
PROBE-RESULT ... query=1066 ms=1066 sum=1066 elapsed=1.066465732s overBudget=false isBusy=true   (SQLITE_BUSY)
PROBE-RESULT ... query=854  ms=854  sum=854  elapsed=854.423894ms  overBudget=false isBusy=true   (SQLITE_BUSY)
PROBE-RESULT ... query=1087 ms=1087 sum=1087 elapsed=1.087301413s overBudget=false isBusy=true   (SQLITE_BUSY)
PROBE-RESULT ... query=1107 ms=1107 sum=1107 elapsed=1.10770281s  overBudget=false isBusy=true   (SQLITE_BUSY)
```

`segment="query"`, `attempts=3` (== `prefetchBusyRetryMax+1`), the error is the
**SQLITE_BUSY refusal** and NOT `context deadline exceeded`, `ms == sum`
exactly, `overBudget=false`: the retry loop's memory of the first lock is visible
in the record — 3 attempts now finish inside the 1500ms carrying the refusal
(854–1111ms, against the pre-fix 1274–1572ms expiry).

**Re-run here:** `go test ./cmd/agent/ -run TestPrefetchBusyIsRetriedWithinTheSameBudget`
3 separate runs `ok` (load 10.65/10.97/10.57 → 2.663s/2.077s/2.237s), `-count=3`
`ok` at load 10.45 (4.539s); `go test ./internal/standards/ -run 'DocManifest|DocClassBanner|DocIndex'`
`ok` (the Rule 36 gates covering this doc's row).

**Package budget (`make test-budget PKGS='./cmd/agent'`), read off its own JSON
stream** — the run had not finished when the session closed, so this is the
partial reading, not a verdict: at that point 3451 tests had PASSED and the
failures were `TestResumeKeepsADirtyWorktree`,
`TestSyncTaskIDRefusedOnEverySessionFace` (+8 subtests),
`TestWorkerResumeKeepsTheGuardsIntact`, `TestLedgerOpenersUseProductionDSNContract`
— pre-existing at the branch point — plus `TestMergePromptCoversEveryRequiredSection`,
which is neither in this diff's tree nor in the task's protected list.
`TestPrefetchBusyIsRetriedWithinTheSameBudget` is **absent from that failure set**,
which is the one thing this epic needed from the package run.

## Post-port corrections (verification round, same day)

Three things the first pass got wrong or left unfinished. Recorded because the
next reader would otherwise trust the sections above as final.

### 1. The port left a DEAD ARM — the fix as recovered was not the fix as landed

The recovered patch was applied 3-way against newer code and produced one
conflict; the resolution was reported as complete. It was not: the query arm kept
**both** `case isBusyErr(err)` **and** a second `case runCtx.Err() != nil`, and
the following arm still read `case isBusyErr(err)` **after** it. The second copy
of the busy arm was unreachable — Go needs no `default` to break out, and a
`switch` evaluates its cases in source order, so the later `isBusyErr` matched
nothing that had not already been caught. The behaviour was therefore right and
the code lied: it read as if the arm order were unsettled, which is exactly the
thing this doc is about. The pre-commit `git diff` should have been the last
step before the first commit and was not.

**The lesson, and it is the same one as the conflict**: a 3-way apply of somebody
else's commit produces a tree that *compiles* long before it produces the
*intent*. Reading the resulting arm order end to end — not just resolving the
conflict and building — is part of applying a foreign patch.

### 2. The defect is NOT load-dependent — it reproduces at load 6.9

The epic frames the red as "under load ~14 on a 4-core host", and the pre-fix
round in the section above could not reproduce it (ambient load topped out at
~13.2). A base worktree at `5276068c1` — the unmodified pre-fix source, in a
detached worktree at `/home/user/workspace/gopi-test-scratch/base-5276068c1` —
reproduced it on three separate runs at **load 9.05 / 7.44 / 6.85**, with the
task's verbatim symptom:

```
--- FAIL: TestPrefetchBusyIsRetriedWithinTheSameBudget (1.60s)
    chat_prefetch_segments_test.go:481: session shape: the give-up is not a
    busy refusal: prefetch gave up after 1500ms (query): context deadline exceeded
```

Load is a **proxy**, not the cause. What decides the attribution is whether three
attempts of (ladder walk 252ms + DSN busy timeout 250ms) finish inside
`prefetchGoroutineTimeoutMs` (1500ms) on the machine's current slices — and on
that base tree they reliably do not, at any load. The pre-fix section's "the
defect is knife-edge there" conclusion was wrong in its mechanism and is
corrected here; the earlier round's *evidence* (2 valid passes of 3 at load
8.86/13.24) stands as a record of what it saw, but not as a characterisation of
the defect.

**Method note — this is the red→green the epic's acceptance asked for**, and it
was obtainable all along without generating load (Rule 27): the pre-fix red is
reproducible in a *second worktree*, so a fix does not have to be reverted in
place to measure the before. The "before" is a commit that already exists on the
branch's ancestry.

### 3. The five extra reds in the package run are pre-existing, measured

`make test-budget PKGS='./cmd/agent'` on the fixed tree reports five failures
beyond the task's protected six: `TestMergePromptCoversEveryRequiredSection`,
`TestMergeUnionRowCheckOnTheRealManifest`, `TestSpawnersPinTheRepoContext`,
`TestSubmitSpawnersGiveTheChildAResolvableRepo`,
`TestSubmitSpawnersGiveTheChildAResolvableRepo_ExactMatchRule`. The first pass
could only say they were "outside this diff's tree", which is not the same claim.

Run on the **base worktree, unmodified** (load 5.95) all five fail identically,
by their own assertions, with the same messages:

```
--- FAIL: TestMergePromptCoversEveryRequiredSection (0.00s)
    merge_prompt_gate_test.go:366: the prompt no longer states "gopi session resume"
--- FAIL: TestSpawnersPinTheRepoContext (0.56s)
    repo_context_sandbox_guard_test.go:492: the scan found only 64 lifecycle spawn site(s), expected at least 80
```

They are **pre-existing at the branch point**, not introduced here, and they are
outside this task's scope per the brief. The gate text is worth reading rather
than skimming: several of them are *vacuous-verdict guards* firing because the
tree's shape drifted from what the scan expects, which is a different defect class
from a plain assertion failure and belongs to whoever owns those gates.
