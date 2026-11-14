<!-- doc-class: normative -->
# The chat status row's Δ reading: one instant, one quantity

Task `01a1c312-3f51-7b3f-9828-6bd38d485de1`. The row printed `Σ95.9%Δ100%` and the
operator asked whether a 100% cache hit rate is possible. It is — a turn served
entirely from the cache really is 100% — but that was not what happened: the turn
that printed `Δ100%` measured **98.43%**.

## The defect

`statusCacheTurnHitRate` took its two increments off two **different counters**,
each against its own baseline:

```
read Δ  = cacheRead  − cacheReadAtTurn   (CacheReadInputTokens)
write Δ = cacheWrite − cacheWriteAtTurn  (CacheCreationInputTokens + InputTokens)
ratio   = read Δ / (read Δ + write Δ)
```

A sum of counters is only as fresh as its **last arriving term**. The read side
arrives with the response's usage block; the miss side is a sum whose terms land
with that same block in the normal case — but any instant at which the denominator
had not yet been credited produced `read/(read+0)`, i.e. exactly `100%`. The
reading therefore degraded into a binary indicator of *"has the miss counter moved
since the snapshot"*, which is the symptom the operator saw. Measured on the same
snapshot, term by term (probe output, `artifacts/turn-timeline-probe.txt`):

```
SNAPSHOT beginTurn: prompt=0 read=0 miss=0 -> Δ=""
  prompt+read credited: prompt=70210 read=69376 miss=0 -> Δ="99%"
    pre-fix arithmetic on the SAME snapshot: readΔ=69376 writeΔ=0 -> "100%"
  miss credited: prompt=70210 read=69376 miss=834 -> Δ="99%"
  check: promptΔ-readΔ=834 == missΔ=834 : true
```

## The fix

One instant, one quantity. The transport defines the prompt side as
`input + cache_creation + cache_read` (`ClaudeUsage.PromptSideTokens`) and credits
all three in ONE call (`CostTracker.AddClaudeUsage`), so the prompt-side total and
the served count always arrive together. The write side is **derived** from that
same snapshot instead of being read off a counter of its own:

```
prompt Δ = cachePrompt − cachePromptAtTurn
read Δ   = cacheRead   − cacheReadAtTurn
write Δ  = prompt Δ − read Δ          (== input Δ + cache_creation Δ)
```

`statusCacheTurnRatio` is that arithmetic and `statusCacheTurnHitRate` is its
formatter; `statusOps.cachePrompt` / `cachePromptAtTurn` are the two new readings,
both taken in the SAME `snapshotTurnCountersLocked` call that takes the other two
(Rule 15: a reading and the baseline it subtracts cannot come from two moments).
`statusOpsFor` fills `cachePrompt` from `CostTracker.TotalPromptTokens`, which is
the same quantity by construction: `TotalPromptTokens == TotalCacheHitTokens +
TotalCacheMissTokens` on this path.

The emphasis reader needed no separate change **and that is the point**: the span
and its tier both go through `statusCacheTurnHitRate`, so the drawn text and the
coloured span cannot disagree about the number (Rule 14). Geometry is untouched —
`statusPinSlackCells` stays 0 and the Δ form is still `Δ` + at most three digits.

Two honest states are preserved:

- **no baseline** (`cacheTurnKnown == false`) → `""`, the cumulative reading alone;
- **zero sample** (both increments zero) → `""`, never a clamped `100%` (Rule 15b).

And one NAMED degradation: a `statusOps` built by a caller that never sets
`cachePrompt` (not the row's own source) falls back to the miss counter's own
delta — the pre-fix number, stated rather than silent.

## Why the pre-fix numbers were right anyway

On a settled instant the two definitions agree exactly, and the session data says
so: across consecutive totals `prompt Δ − read Δ == miss Δ` holds for every pair
(asserted in `TestStatusCacheTurnRateAgreesWithThePromptSide` over real rows
69,442 / 139,652 / 283,220 / 502,386). The fix is not a new definition of the miss;
it is the same number read from the counter that cannot lag its own response.

## Verification

```
go build ./...                                              → OK
go test ./cmd/agent/ -run 'TestStatusCacheTurn|Cache|Status' → ok, 19.7s
```

Red proof: with the pre-fix arithmetic restored in `statusCacheTurnRatio`, the new
tests fail with `Δ reading = "100%", want "93%"` (stale-miss construction) and
`Δ = "100%", want "99%"` (the measured turn), then pass again with the fix.
