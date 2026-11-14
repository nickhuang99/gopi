<!-- doc-class: normative -->
# A Context-Ended Run Names Its Real Cause — and Never an Iteration Budget

> **Provenance.** `TestDocAnchorsResolveInNormativeDocs` was retired on 2026-10-22 in `5a35f61` (deleted with `internal/docclass`), so a `file.go:NNN` reference in this page is a landing-time record, not an anchor a live gate enforces — nothing resolves one today. The line numbers stay useful to a reader; they are not checked by the suite. Citations below are quoted as they were written — where one of them reads as present tense ("is RED", "resolves", "fails"), read it as the state of THAT run, never as a gate this tree still has.

**Status:** landed. **Surface:** `internal/agent/loop.go` (the round-boundary
`ctx.Done()` fallback), `internal/runend` (the shared end-reason vocabulary).

## The defect (measured 2026-10-16)

`internal/agent/loop.go:4289` (pre-fix) ended every context cancellation with:

```go
return "", fmt.Errorf("agent timed out after %d iterations", i)
```

Two things were wrong, and together they made the message useless:

1. **It named a budget that does not exist.** `NewAgent` sets
   `MaxIterations: 0` — "0 = unlimited, like Pi-mono" — and **no non-test code in
   the repository assigns it anything else**. The for-loop's own bail
   (`max iterations (%d) reached`) is therefore unreachable in production, while
   this fallback is reachable with `MaxIterations == 0`, i.e. exactly when there
   is no limit to time out against.
2. **It printed the loop counter as the number.** `%d` was `i`, the round index,
   so the same defect reported a different figure every occurrence — **43, 48,
   49, 52 …** in the merge hand-over failures, and `1`, `3` in this task's own
   live runs. A number that varies with how long the run happened to live reads
   like a limit that was hit, and is not.

**The blast radius:** 81 merge hand-over failures shared that one sentence, and
the classifier downstream (`crashdump.go`) matched neither `"timed out"` (it
matches `"timeout"` — one space apart), so a real, actionable cause landed in
`category=unknown`, `recoverable_hint=false`. Filing that half is a separate row
(`loop.go` is not the classifier); this change is the **producer** side.

## The change

`ctx.Err()` distinguishes the two ends exactly, and both are
`errors.Is`-comparable, so the fallback now says which one happened:

| `ctx.Err()` | message suffix | `runend.Reason` | truncated? |
|---|---|---|---|
| `context.DeadlineExceeded` | `context deadline exceeded (deadline-exceeded)` | `runend.DeadlineExceeded` | yes |
| `context.Canceled` | `context canceled (run-context-cancelled)` | `runend.RunContextCancelled` | yes |

```go
if errors.Is(ctx.Err(), context.DeadlineExceeded) {
    endReason = runend.DeadlineExceeded
} else {
    endReason = runend.RunContextCancelled
}
return "", fmt.Errorf("agent run ended before the task was finished: %w (%s)", ctx.Err(), endReason.Token())
```

Design decisions worth keeping:

- **Two values, not one.** An expired limit is a budget/configuration fact (raise
  it or accept the truncation); a cancellation is somebody's decision (a parent
  run, a supervising surface, a harness stop). Merging them would have put the
  reader back where the single misleading sentence was.
- **Both are `Truncated()` (engine-forced).** Neither end is the model reporting
  its work finished, so the row must not close `completed` — the same rule the
  wallclock cap and the three stuck nets already obey
  (`internal/agent/taskregister.go`: `status = exitCode != 0 || endReason.Truncated()`).
- **No new ledger status**; the reason rides the existing `reason:` column and
  the `run-end` `task_events` note (`runend.ReasonText` / `Detail`).
- **The signal latch keeps its own precedence.** `interruptedBySignal()`
  (`Interrupt()`, SIGINT/SIGTERM — the process is leaving) and
  `TurnInterrupted(ctx)` (the operator abandoned one turn) are checked BEFORE
  this fallback, unchanged. Ordering is load-bearing: a `SIGTERM` run records
  `interruptErr`, not `run-context-cancelled`, and this change does not touch
  that.
- **`%w`, not `%v`** — the error stays in the context chain, so a carrier can
  classify it (`errors.Is(err, context.Canceled)`), which the old message could
  not support at all.
- **The old sentence survives only inside a comment** (the post-mortem). A grep
  gate over `internal/` + `cmd/` would otherwise be impossible to write without
  a redundant literal.

## The minimal reproduction

`internal/agent/loop_ctxend_reason_test.go` drives the **real loop** (mock
Anthropic endpoint → real `Run` → real end hook → real ledger row) with
`MaxIterations = 0` and cancels the run from inside the first tool round:

- `TestAgent_MaxIterationsZero_CancelledRunClaimsNoIterationBudget` — the error
  must not contain `iterations`, must contain `context canceled`, must satisfy
  `errors.Is(err, context.Canceled)`, and the row must carry
  `runend.RunContextCancelled` (asserted through `task show` and the session
  JSONL's `task_row_closed` witness, not the row alone).
- `TestAgent_RunCtxEnded_DistinguishesDeadlineFromCancel` — the same exit site
  with the run's own deadline armed at 1ns: the message names the deadline, zero
  LLM rounds are issued, and the row records `runend.DeadlineExceeded` — the
  distinguishability the two values exist for.

### Red proof (master fails the same claim)

`git stash` is banned in this repo (the stash stack is shared by every linked
worktree), so master was reached by `git checkout HEAD -- internal/agent/loop.go
internal/runend/runend.go` with a probe test that references **no symbol this
change introduces** — on master the failure is a wrong message, never a build
error:

```
--- FAIL: TestRedProofCtxCancelIsNotAnIterationTimeout
    PROBE observed error text: "agent timed out after 1 iterations" (tool rounds=1)
```

(Raw capture: `artifacts/redproof-master.txt` in this task's session directory.)
The `1` vs production's `43/48/49/52` is the point: the number is the round
counter, not a limit.

## The live run (Rule 31)

A real binary (`CGO_ENABLED=0 go build -o /tmp/gopi-ctxend ./cmd/agent/`),
`--timeout 8` (the flag that publishes `GOPI_TIMEOUT_SEC`), against a scripted
SSE mock that emits a `bash sleep 5` tool round so the deadline expires **while a
tool round is in flight** — i.e. is noticed at the round boundary, which is the
changed exit:

```
{"type": "session_end", "exit_code": 1,
 "error": {"error_body": "agent run ended before the task was finished: context deadline exceeded (deadline-exceeded)",
           "failed_iteration": 1, "category": "network"},
 "total_iterations": 3}
{"type": "task_row_closed", "status": "failed", "reason": "deadline-exceeded", ...}
```

`total_iterations: 3` beside a message that names the deadline (and never a
budget) is the whole change in one line. The run's own remedy values:
`runend.DeadlineExceeded.Token()` = `deadline-exceeded`.

**The cancel arm on a live surface was NOT reproduced end to end, and this is
stated rather than glossed.** Both external signals a terminal user can send
(`SIGINT`, `SIGTERM`) take the latch path first — measured: live `SIGTERM` mid-run
produced the loop counter `333 iterations` in the stats line, `error_body: "run
interrupted by signal"`, and `session_end exit_code=1`, i.e. the pre-existing,
deliberate `interruptErr` precedence. The remaining live producers of a bare
`context.Canceled` through this exit are per-turn cancels
(`InterruptRun` — the web `/api/chat/interrupt` route, the UI Ctrl+X hook,
`gopi cli --once`), and driving one of those end to end (embed the child in a
live turn, then cancel it) is the follow-up this report leaves open. The cancel
arm is covered by the in-process test above, which exercises the same exit site
in the same function with a real `Run`.

## Not this change's business

- **The downstream classifier** (`crashdump.go` matches `"timeout"` but not
  `"timed out"`) and its `category=unknown, recoverable_hint=false` consequence —
  a separate row.
- **`TestDocAnchorsResolveInNormativeDocs`**: red before and after. This diff
  shifts `loop.go` line numbers, so the count was measured on both sides —
  **master 515 drift lines, this branch 507** — the pre-existing red is not
  increased, and the drift it reports is in other docs' anchors.
