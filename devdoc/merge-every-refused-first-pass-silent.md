<!-- doc-class: historical -->
# `gopi merge --every D` RETRIES on a held lock — the FIRST refused tick just never said so

**Task:** 01a114e6-b7a1-711e-8882-42e9a129d170
**Measured:** 2026-10-07 (peerbox)
**Branch:** `task/01a114e6b7a1711e888242e9a129d170`

## The report, and what was actually true

The operator ran `gopi merge --every 3m`, saw the single-instance lock refusal,
and reported that the command "exits immediately instead" — asking whether
`--every 3m` was even supposed to retry every 3m.

Both halves of the premise are wrong, and the output gave the operator no way to
know it:

| The premise | The measurement |
|---|---|
| the loop exits when the lock is held | it does not — it refuses, sleeps the interval, refuses again, forever |
| `--every 3m` might not mean "retry every 3m" | it does exactly that: one pass per `D`, `D` being the pause between them |

Measured, the fixed binary, `--every 5s`, with a real separate process holding
the private profile's `serve.lock`:

```
gopi merge: --every 5s — running the pass now, then every 5s; ...
gopi merge: refused — another merge face holds the single-instance lock; ...
gopi merge: pass 1 returned 1 — the loop continues; the next one is in 5s
gopi merge: pass 2 returned 1 — the loop continues; the next one is in 5s
gopi merge: pass 3 returned 1 — the loop continues; the next one is in 5s
gopi merge: interrupt — the loop stops here; ...
runner-exit=124          # timeout(1) ended it; the loop never exited on its own
```

## The real defect: silence on pass 1

`mergeLoopReportPassFailure` (`cmd/agent/merge_run.go`) suppressed its line for
`pass <= 1`:

```go
if code == 0 || pass <= 1 {
    return
}
```

with the justification *"its own lines are the run's opening, and the loop has no
interval to name 'the next one in' yet."* **The second clause is simply false** —
`every` is in hand on the first pass exactly as on any other, and the opening
line has already printed it. So a refused first tick produced:

```
gopi merge: --every 3m — running the pass now, then every 3m; ...
gopi merge: refused — another merge face holds the single-instance lock; this run decided NOTHING: ...
<nothing for 3 minutes>
```

which is **byte-for-byte the output of a one-shot `gopi merge` that exited 1**.
The dominant first-tick failure — a refusal, which is the ordinary case (a
hand-run merge, the post-receive hook, a sibling loop) rather than an
exceptional one — was exactly the one the loop described least. The interval is
minutes, so "wait and see" is not a diagnostic either.

**The stake:** an operator who cannot distinguish *retrying* from *dead* either
kills a healthy loop or leaves a dead one running, and with `D = 3m` the
ambiguity lasts long enough to be acted on. That is the report.

## The fix

Two lines, both in `merge_run.go`:

1. **`mergeLoopReportPassFailure` now reports every non-zero pass**, the first
   included. The line carries the only fact that matters here — *the loop is
   alive and will try again, in `D`* — and pass 1 is where it is least
   inferable.
2. **The `--every` usage page** states that a failed pass (a held lock named as
   the common cause) never ends the loop, and contrasts it deliberately with the
   one-shot face, where a held lock **is** fatal so a hook gets a non-zero exit.

### What was NOT changed, and why

The lock refusal itself stays fatal for the **one-shot** face. It is not a
degraded mode: two live merge writers in the same refs *is* the double-merge the
guard exists to prevent, and the post-receive hook calls `gopi merge` and must
be able to tell success from refusal by exit status. Only the LOOP's *reporting*
was in scope. Waiting for the lock (`flock` blocking) was considered and
rejected — it would hold the loop's own tick open for another face's whole run,
and it contradicts the per-pass lock contract the `--every` epic chose
deliberately (the lock is taken per pass and released while sleeping, so a
hand-run merge during a tick is served in the ordinary way).

## Verification

### Red → green on the new test

`TestMergeEveryRefusedFirstPassAnnouncesTheLoopContinues` encodes the operator's
report: a real separate holder process on the profile's own lock path (the
suite's existing `mergeStartLockHolder`, so the test cannot prove a lock the
production run does not take), the real binary, real argv. It asserts the
refusal is present, then that `pass 1 returned` appears, then that the loop did
NOT record a completed pass, then that the stop still exits 0.

The red half was first attempted against a tree being edited under a compiling
`go test` and was killed as contaminated (it measured a race, not the old code).
It was then taken cleanly: `merge_run.go` was reverted to `8eb98d6e8^` with
`git show`, with nothing writing it, and the test was run against that tree. The
old guard `if code == 0 || pass <= 1 { return }` is what this test pins, and the
run printed it directly:

```
make test-budget PKGS='-run TestMergeEveryRefusedFirstPassAnnouncesTheLoopContinues ./cmd/agent/'
  --- FAIL: TestMergeEveryRefusedFirstPassAnnouncesTheLoopContinues (0.45s)
    merge_every_test.go:628: a refused FIRST pass was reported silently:
      "pass 1 returned" does not appear in the log, so a reader cannot tell a
      retrying loop from a command that exited 1 (the defect this test pins).
      The loop must report EVERY failed pass, the first one included.
  ... 1 finished, 1 with a failure ... exit=1 outcome=fail   (stream=run-1791353266-1881635.json)
```

Note the shape of the red log: the refusal line, then `pass 2 returned 1 — the
loop continues` — pass 2 reports fine, only pass 1 is silent, which is exactly
the defect (and exactly why the second marker `pass 1 returned` had to exist:
the pass-2 line satisfies `the loop continues`, so a marker-only test would have
been green against the broken code).

The fixed file was then restored unmodified from the commit and the **green**
half observed on the identical invocation (7s wall):

```
make test-budget PKGS='-run TestMergeEveryRefusedFirstPassAnnouncesTheLoopContinues ./cmd/agent/'
  ... 1 finished, 0 with a failure ... exit=0 outcome=pass   (stream=run-1791353276-1883796.json)
```

The full-suite green:

```
make test-budget PKGS='-run TestMergeEvery ./cmd/agent/'
  ... TestMergeEveryRefusedFirstPassAnnouncesTheLoopContinues (0.09s) PASS
  slowest 15 tests ... exit=0 outcome=pass   (15/15 PASS, elapsed=137s, stream=run-1791352734-1828513.json)
```

The twelve pre-existing `TestMergeEvery*` tests and the three child scenarios all
pass, including `TestMergeEveryLoopRunsSuccessivePasses` (which independently
asserts the per-pass lock acquisition) and `TestMergeEveryAbsentIsExactlyOnePass`
(which asserts the one-shot way is untouched).

### Help page

```
make test-budget PKGS='-run "TestHelp|TestEveryHelpConstant|TestMergeLock|TestMergeEveryUsagePage" ./cmd/agent/'
  ... exit=0 outcome=pass   (stream=run-1791352974-1866878.json)
```

`TestEveryHelpConstantFitsTheWrapBudget` is the exhaustive width gate over every
help constant; the new paragraph is ≤88 display columns like the rest.
`gopi merge -h` was rendered from the built binary and the new section reads
correctly (15-line block, `helppage.txt`).

## Files

- `cmd/agent/merge_run.go` — `mergeLoopReportPassFailure` reports pass 1; the
  `--every` usage note gains the retry-vs-fatal contrast.
- `cmd/agent/merge_every_test.go` — the regression test plus two new markers
  (`mergeLoopPassFailedMarker`, `mergeLoopFirstPassFailedMarker`; the second
  exists because `pass %d` is interpolated and pass 2's line would otherwise
  satisfy the marker).
