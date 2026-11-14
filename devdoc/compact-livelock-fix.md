<!-- doc-class: historical -->
# Report: memory compaction livelock — the cycle at the context limit

Branch: `fix/compact-anchor-livelock` (worktree `~/workspace/gopi-compact-loop`)
Task: bug report — *"clearly this is a bug when reaching memory limit which is
running in a cycle. fix this."* (attachment: the field session's `raw.log`).

## Completed

### 1. Root cause — measured on the field log, not inferred

The attachment is a 3-round excerpt of one session's own log, and the full
`raw.log` (`c2eff914`) is still being appended by the live process while this
task ran. Counting it gives the cycle its exact shape (numbers are **one
snapshot** at 2026-09-16 17:17 — the file grows by ~400 KB per minute, so the
counts only rise; the ratios and the fixed-point pairs are what matter):

| observation | count (snapshot) |
|---|---|
| `🛡️ compaction anchored at last user` | 305 |
| `🧠 memory compaction: summarized 1 oldest message(s)` | 305 |
| `🧠 memory compaction: summarized 2 oldest message(s)` | 183 |
| `assertion: after ≤ target+summary = false` | 315 |
| `assertion: after ≤ target+summary = true` | 181 |
| `before=157995 after=118285` (same pair, repeated) | 2508 |

The two headline counts are **equal** (305 = 305): every "anchored at last
user" round is followed by "summarized 1 oldest message" — the fixed point,
one summarizer round-trip per round. Three unique before/after pairs alternate
(`157995→118285`, `158347→118616`, `158844→119145`) and the `before` values
**rise** while each round claims to have compacted — the context never settles.
Root cause chain (5-why):

1. Why does compaction re-fire every round? The estimate stays above the
   trigger (150000 = 75% of 200000) after each compaction.
2. Why does it stay above? Each compaction removes only 1–2 messages.
3. Why only 1–2? `TakeOldest`'s deletion loop stops at the anchor floors
   (last user / last visible assistant) — in this session those sat
   immediately behind the head, so the compressible window collapsed to a
   sliver.
4. Why does the loop then not back off? `len(oldest) > 0` was the only
   condition for "a real window exists" — a 1-message window passes it, so the
   summarizer ran, prepended **another** memory block, and **cleared the
   failure cooldown**.
5. Why does that reproduce forever? The new block lands at index 1 (right
   behind the epic input), which re-creates the identical pinned window — a
   fixed point. The cycle is one LLM round-trip per round, for no headroom.

Confirmed live: session `c2eff914` (pid 601510) was still cycling while this
task ran — the log grew from 66 MB to 131 MB during the session.

### 2. Second defect found while fixing — the savings verdict compared mixed bases

`compactContext` resets `PromptTokens = 0` (correct — the measurement covered
the old context) and then computed the anti-thrash verdict as
`(before_measured − after_heuristic) / before_measured`. Two different bases:
the chars/4 heuristic reads *lower* than an API count, so a compression that
changed nothing reported **phantom savings**. The `< 10%` ineffective breaker
— the second safety net against exactly this cycle — therefore could never
latch.

### 3. Fix (three parts, each with its own guard)

- `internal/agent/compact.go`
  - named constants `compactEffectiveSavingsPct`, `compactIneffectiveStrikes`,
    `maxCompactSliverMessages` (were inline `10`, `2`, bare `1`).
  - **LIVELOCK GUARD**: a window that yields ≤ 2 messages *and* was stopped by
    an anchor floor *and* leaves the context still above the **trigger** is a
    STRUCTURAL NO-OP — no summarizer call, the sliver is put back, the
    existing structural-no-op backoff is armed, and the deterministic trim
    (which has no anchor floors) bounds the context instead.
    The three-way condition is what keeps a *legitimate* compaction intact: a
    real body of conversation in the window still takes the normal path.
  - the effectiveness verdict now compares **heuristic vs heuristic**
    (`beforeHeuristic` captured before the mutation, `heuristicTokens()`
    after), so the breaker latches on real stagnation.
- `internal/agent/context.go`
  - `heuristicTokens()` — the chars/4 estimate with no dependence on the API
    measurement, i.e. a basis available on both sides of a reset.
  - `windowIsMemoryBlocks(offset, lastUser, lastVisibleAssistant, keepRecent)`
    + the same guard inside `TakeOldest`: when every message the deletion
    pass can still reach before its FIRST stop floor is a compaction memory
    block, there is nothing left to distill (summarizing a summary converges
    on nothing); the loop stops and records
    `AnchorStop = anchorStopMemoryBlock` instead of eating its own output.
- `internal/agent/loop.go`
  - `compactionPaused` compares against `compactIneffectiveStrikes` (was a
    bare `2`); comments updated to name the constant.

### 4. Red proofs (new tests against **master's** source)

Four tests in `internal/agent/compact_cycle_test.go`. With the fix stashed so
only the new test file is added, the reported defects reproduce:

```
--- FAIL: TestCompact_PinnedWindowBacksOffInsteadOfLooping
    round 2: summarizer re-fired for a window that cannot converge — 2 calls total (livelock)
    🛡️ compaction anchored at last user …
    🧠 memory compaction: summarized 1 oldest message(s) into a memory block
    ⚠️ est 20226 > 15000 (101.1%) — compacted to est 20226 — assertion: after ≤ target+summary = false

--- FAIL: TestCompact_SavingsVerdictUsesSameBasis
    recorded 81.55% vs the real single-basis change 44.64% (phantom savings — the breaker can never latch)

--- FAIL: TestCompact_LargeSliverWindowStillConverges
    round 2: a memory block is being re-summarized (the window bound defect)

--- FAIL: TestCompact_FieldNumbersStopLooping
    (the field's own numbers: measured 157995, window 200000, trigger 150000)
```

`TestCompact_FieldNumbersStopLooping` replays the attachment verbatim
(`before=157995`, `est 118285`, window 200000, ratios 75%/35%, "summarized 1
oldest message"). `TestCompact_LargeSliverWindowStillConverges` pins the one
shape the CALLER-side guard cannot catch (a large sliver frees ≥10%, so the
guard's savings test reads FALSE and the summarizer runs legitimately) — the
defect there is the NEXT round deleting the block the previous round wrote,
which is why the guard also lives inside `TakeOldest`.

All four PASS with the fix. **No existing test was modified** —
`TestCompact_AnchorsPreserveLastUserAndAssistant`, `TestCompact_IneffectiveDebounce`,
`TestCompact_EmptyWindowStructuralNoOp`, `TestCompact_AssertionLog`,
`TestTakeOldest_KeepRecentPreservesTail`, the `TestTrimContext_*` family and
the `TestCompact_*Round*` family pass unchanged.

### 5. A defect found in this fix itself, by probing

The first version of the `TakeOldest` guard bounded its region by
`len(Messages) - keepRecentFloor - 1` (a fixed 2-message tail) instead of the
FIRST stop floor the deletion loop would actually hit. On the large-sliver
shape that bound sat *below* the anchor floor, so the guard never fired, the
deletion proceeded past the memory block, and round 2 summarized the block
round 1 had just written (round 3 did it again). Found with a throwaway probe
test (`TestZZProbeLargeSliver`) that printed the per-round log; the bound is now
computed from `min(keepRecent floor, lastUser, lastVisibleAssistant)` and the
shape is pinned by `TestCompact_LargeSliverWindowStillConverges`. The probe was
deleted; the durable test replaced it.

## Not Done (honest)

- The **trigger** of the field session — *why* the anchors ended up jammed
  against the head — is not addressed. This fix makes the loop **converge**
  (bounded, no LLM call per round) rather than making an anchor-jammed window
  compressible. A follow-up could ask whether harness-injected follow-up
  messages should be exempt from being the "last user" anchor (they are the
  reason `lastUser` sits so close to the head). **This is a real remaining
  gap and is dispatchable as its own task.**
- Live replay against a self-hosted session was not run; the evidence is the
  package tests plus the master-source red proofs.

## Verification

- Build: `go build ./...` → OK (BUILD_OK)
- Vet + fmt: `go vet ./internal/agent/` clean; `gofmt -l internal/agent/` clean
- Targeted: `go test ./internal/agent/ -run 'TestCompact|TestTrimContext|TestTakeOldest' -count=1` → ok
- Full package: `go test ./internal/agent/ -count=1` → ok
- Gates: `go test ./internal/standards/ -run TestStandardGates` — the red hits
  (Rule 16 at `internal/tools/task_merge.go:366`, Rule 17 at
  `internal/agent/journalreport_test.go:101,104`, Rule 18 at
  `internal/tools/task_merge.go:336`) are **pre-existing on this branch's base
  (0dac2ae7)** — verified by re-running the gate with this branch's three
  source files stashed: identical hits. `git diff --name-only 0dac2ae7` lists
  only `compact.go`/`context.go`/`loop.go`, none of which a gate hit names.
- Red proofs: `git stash push -- internal/agent/{compact,context,loop}.go` then
  `go test ./internal/agent/ -run TestCompact -count=1` → the four new tests
  FAIL (`NoLivelockAcrossRounds` passes on both — it is an invariant guard, not
  a red proof); `git stash pop` → all PASS.
- Commit: `903c0d5c` on `fix/compact-anchor-livelock` (worktree shares the
  main repo's refs — the commit is already on the branch).
