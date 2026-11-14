<!-- doc-class: historical -->
# Task Report: TestTaskFindSessionJSONLStillHonoursEveryLayout flake — fixture artifact, not a production defect

## Intended precedence of `taskFindSessionJSONLForRow` (the ruling)

`taskFindSessionJSONLForRow` (internal/tools/tasks.go:1852) resolves in this
order, and this order is the CONTRACT:

1. **The row's own two deterministic addresses**, via
   `taskFindSessionJSONLInTaskDir`: two stats + one readdir, in the SCAN's
   precedence.
2. **Ledger-named directories** of the session's row group
   (`taskSessionDirHints`) — bounded probes, skipped for an already-probed id.
3. **The flat layout** `findSessionJSONLFlat` (`<session-id>.jsonl` at the root)
   — its turn is late because it costs a readdir of the whole root.
4. **The deep scan** `taskFindSessionJSONLDeepOnly` — last, O(sessions tree).

Inside step 1, when BOTH deterministic addresses exist for the same session in
one root, the winner is **readdir order — byte-sorted directory names**:

- `<session-id>` (bare dir) sorts before `<task-id>` (task-run dir) →
  the **bare** record `sessions/<session-id>/session.jsonl` wins.
- otherwise (including a tie, `session-id == task-id`) → the **task-run**
  record `sessions/<task-id>/<session-id>.jsonl` wins.

The flat record is **never** preferred over either addressed form.

**Ruling: the fixture was the artifact, not the production resolver.** The deep
scan visits the sessions root in byte-sorted readdir order and returns the first
record whose id matches, so with the three layouts coexisting in ONE root the
answer is fully determined by which dir name sorts first — nothing nondeterministic
lives in `taskFindSessionJSONLForRow` beyond that ordering, and the ordering is
reproduced deliberately (`if id < tid` in `taskFindSessionJSONLInTaskDir`,
matching `taskFindSessionJSONLDeepOnly`). The old test wrote the three layouts
into one shared root through a **Go map range** (random order) and asserted each
layout in turn — an unfalsifiable property: whichever address the resolver
legitimately prefers, two of the three assertions must fail. So the ~40% red was
the fixture asserting something untrue, and the defect it masked was the missing
pin on the precedence itself.

## Completed

- `TestTaskFindSessionJSONLStillHonoursEveryLayout` made deterministic: the
  layout matrix is now a **slice** (`taskSessionLayoutProbes`) — never a map —
  and **each layout is probed against its OWN `t.TempDir()` root**. The three
  layouts are alternative addresses for ONE session, not three coexisting
  records; the property under test is "each layout is reachable at all", which is
  only observable when that layout is the only record present.
- New deterministic regression guard
  `TestSessionLayoutPrecedenceIsTheScanOrder` pins **BOTH directions** of the
  layout-order probe, each against **BOTH** entry points
  (`taskFindSessionJSONLForRow` and the deep scan
  `taskFindSessionJSONLDeepOnly` that it must agree with), with a flat record
  present as a loser in both directions:
  - `bare-dir-sorts-first` (session id `<` task id) → bare record wins;
  - `task-run-dir-sorts-first` (session id `>` task id) → task-run record wins.
  One direction alone cannot tell a fixed order from a lucky one.
- Red-proof of the guard: inverting the comparison (`if id < tid` →
  `if id > tid`) in `internal/tools/tasks.go` fails both subtests; restored.

## Not Done

- Nothing deferred from the epic. The precedence is documented in place (the
  existing doc comments on `taskFindSessionJSONLInTaskDir` /
  `taskFindSessionJSONLDeepOnly` already stated the readdir rule; the new guard
  now enforces it) and restated above.
- `taskFindSessionJSONLForRow` itself was **not** changed: the ruling is that it
  was already correct. No production behaviour change was needed.

## Verification

- Build: `go build ./...` → OK
- Flake gate (the epic's acceptance command):
  `go test -count=20 -run TestTaskFindSessionJSONLStillHonoursEveryLayout ./internal/tools/`
  → `ok github.com/nick/gopi/internal/tools 3.35s`, 0 failures
- Both real runs re-executed together, final state of the branch:
  `go test -count=20 -run 'TestTaskFindSessionJSONLStillHonoursEveryLayout|TestSessionLayoutPrecedenceIsTheScanOrder' ./internal/tools/`
  → `ok github.com/nick/gopi/internal/tools 10.021s`, 0 failures (100 test runs)
- Guard alone: `go test -count=20 -run TestSessionLayoutPrecedenceIsTheScanOrder ./internal/tools/`
  → 0 failures
- Guard red-proof: comparison inverted in `tasks.go` → both subtests FAIL;
  reverted → PASS (the guard fails if the fixed behaviour regresses)
- Commit: `3f212cb2` (code) + this report's commit, on branch `fix/taskfindjsonl-layout-determinism`
  ("test(tools): deterministic session-layout probes + precedence guard")
- Package tests (LANDED, green): `go test -count=1 -timeout 30m ./internal/tools/`
  → `ok github.com/nick/gopi/internal/tools 432.382s`, exit 0. The first attempt
  used Go's DEFAULT `-timeout 10m` and died at 604s with a goroutine dump — that
  is the package budget expiring under a host at load ~20, **not** a test
  failure: with the budget raised the same tree passes in 432s. (Diagnosis, not
  tolerance: no test in the package hung — the run finished on its own once it
  was allowed to.) A targeted re-run of every session/layout-touching test is
  green too: `go test -count=5 -run 'Session|Layout|TaskFind|SessionDirHints' ./internal/tools/` → `ok 11.978s`.
- No heavy/device-load test was written or run (Rule 27). `GOPI_TMPDIR` was
  pointed at `/dev/shm` scratch for the fixture roots — never `/` (Rule 22).

## Deliverable location (Rule 6d)

- Branch: `fix/taskfindjsonl-layout-determinism` (worktree
  `/home/user/workspace/gopi-flaky-layout`, shares the main repo's refs — no push
  needed)
- Recovery: `git -C /home/user/work/GoPi show fix/taskfindjsonl-layout-determinism:devdoc/taskfindjsonl-layout-determinism.md`
