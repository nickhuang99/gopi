<!-- doc-class: historical -->
# The sticky-head trim could empty the history (`messages:null` → HTTP 422)

**Date**: 2026-10-01 · **Task**: `01a0f716-81c5-7be5-b985-27f560e23830` ·
**Branch**: `task/01a0f71681c57be5b98527f560e23830` · **Base**: master `b0e93cef`

## Symptom

`internal/agent`'s `TestCompact_PinnedWindowBacksOffInsteadOfLooping` is red at master tip:

```
compact_cycle_test.go:156: trim emptied the history — the request would serialize as messages:null (HTTP 422)
```

The test was written *for* this failure (`9b9264b9`, before it landed: a chat's second round
died with HTTP 422 because the request body carried `"messages":null`), so the red is the
guard doing its job — not a stale fixture.

## Introducing commit

`git bisect run` on a detached control worktree (`git worktree add --detach … 9b9264b9`,
base well-endpoint `b0e93cef`) pinned **`6cc43183`**, *"delivery-point sync: merge master
into the delivered branch before close-out"* — a merge, and its resolution is the defect:

| file | vs parent `3830eedc` | vs parent `a539ebb0` |
|---|---|---|
| `internal/agent/context.go` | differs | differs |
| `internal/agent/loop.go` | differs | **identical** |
| `internal/agent/compact_cycle_test.go` | **identical** | differs |

It took the **call site** from the branch that had `trimToFitStickyHead` (`loop.go`), the
**corrected assertion** from the branch that had the 422 fix (`compact_cycle_test.go`), and
`context.go` from neither — leaving the sticky trimmer's uncapped floor beside a test that
asserts the never-empty property. `b0e93cef` and the newer master tip `b97affcd` carry the
same shape (`sticky floor: … len(c.Messages) > 0`, `TrimToFit guard: present`,
`call site: a.Ctx.trimToFitStickyHead(limit, target)`).

## Mechanism

`trimContext` bounds the context by three routes, and the deterministic one is
`Context.trimToFitStickyHead` — added by `6dbe5a43` (cache amortization: evict in ONE chunk
down to the target instead of nibbling per round). Its two deletion loops had **no floor at
all**:

```go
for c.EstimatedTokens() > targetTokens && len(c.Messages) > 0 { … }
    for len(c.Messages) > 0 && c.Messages[0].Role == "tool" { … }
```

The pinned-window shape makes both run to the end: `TakeOldest`'s anchor floors leave only
the round's own newest tool result reachable, the livelock guard refuses the summarizer call,
and that one message alone exceeds the target. The array is deleted empty → `WireMessages()`
→ `BuildClaudeRequest` (no `omitempty` on `Messages`) → `"messages":null` → HTTP 422.

`9b9264b9` had already established both halves of the contract for the OTHER trimmer —
`TrimToFit` refuses the run that would consume the whole history, and `internal/deepseek`'s
outbound choke point refuses an empty array with `ErrEmptyMessages`. Neither covers
`trimToFitStickyHead`, and nothing exercised its floor.

## Fix

`internal/agent/context.go`, `trimToFitStickyHead` — the same two guards `TrimToFit` has:

```go
for len(c.Messages) > 1 && c.EstimatedTokens() > targetTokens {
    drop := 1 + pairedToolResultRun(c.Messages, 0)
    if drop >= len(c.Messages) {
        break // never empty the history — see the doc comment
    }
    c.Messages = c.Messages[drop:]
    removed += drop
}
```

The whole head RUN is dropped atomically, so no iteration can end between a `tool_use` and
its `tool_result`; that is also what makes the orphan-eviction safety net unnecessary (it was
the second uncapped loop, and the same reason removed it from `TrimToFit` in `9b9264b9`). The
estimate may stay above `maxTokens` when the newest message alone is over the target —
deliberate: that message is the round being served, and a valid over-budget request beats a
malformed one.

## Regression guard

`internal/agent/trim_sticky_head_empty_test.go`:

- `TestTrimToFitStickyHeadNeverEmptiesMessages` — the repro through the production entry
  point (`Agent.trimContext`), asserting the wire body, not an internal invariant;
- `TestTrimToFitStickyHeadDirectNeverEmptiesMessages` — the smallest sample: 3 messages, one
  direct call, target below what the newest message alone costs.

Red at base (2 × `--- FAIL`, exit 1), green after (6/6 `--- PASS` across the compaction family,
exit 0). Full evidence: `~/.gopi/sessions/01a0f716-81c5-7be5-b985-27f560e23830/task-report.md`.

## Lesson for merges

A delivery-point merge can pair one file's **call site** with another file's **implementation**
and produce a state neither branch ever tested. The individual halves were each reviewed and
green on their own branch; the mixed resolution was not. The concrete tell here was that the
guard, its regression test, and its call site lived in three different files — a resolved merge
must be judged as a whole, never file by file.
