<!-- doc-class: historical -->
# Task Report: `TestTaskFindSessionJSONLStillHonoursEveryLayout` — the flake was the FIXTURE, not the resolver

## Ruling (read the resolver before touching the fixture)

`taskFindSessionJSONLForRow` (`internal/tools/tasks.go:1880`) is **not** order
dependent. Its precedence is deliberate and documented in place:

1. the row's own two deterministic addresses — `taskFindSessionJSONLInTaskDir`,
   which reproduces the deep scan's precedence (`if id < tid` probes the bare
   `<session-id>/session.jsonl` first, else the task-run
   `<task-id>/<session-id>.jsonl`);
2. the ledger-named directories of the session's own row group;
3. `findSessionJSONLFlat` (`<session-id>.jsonl` at the root);
4. the deep scan (`taskFindSessionJSONLDeepOnly`) as the fallback.

**The three layouts under test are ALTERNATIVE ADDRESSES FOR ONE SESSION, not
three coexisting records.** The bare dir and the task-run dir both sit one level
under ONE sessions root, so one lookup returns exactly ONE of them, decided by
byte-sorted readdir order. The old fixture wrote all three into ONE shared
`t.TempDir()` root, ranging a **Go map** (randomized order), and asserted each in
turn — an assertion that is *untrue by construction*: whichever address the
resolver legitimately prefers, the other two MUST fail. The outcome was a coin
flip over the map's iteration order.

Measured on master (`bf9fbef8`, fresh worktree, `-count=40`): red in ~1-2 runs
in 8, always the same shape —

```
--- FAIL: TestTaskFindSessionJSONLStillHonoursEveryLayout
    taskledger_cost_test.go:246: flat layout resolved ".../<sid>/session.jsonl", want ".../<sid>.jsonl"
```

So: **no production defect.** The assertion was answering the precedence
question by accident while claiming to answer a reachability question.

## Completed

- `TestTaskFindSessionJSONLStillHonoursEveryLayout` is now deterministic AND
  non-vacuous:
  - the layout matrix is a **slice** (`taskSessionLayoutProbes`), never a map;
  - **each layout gets its OWN `t.TempDir()` root**, where it is the only record
    present, so a hit can only mean *that layout is honoured*;
  - the assertion is unchanged and strict (exact path equality) — no weakening,
    no "either answer accepted";
  - `assertOnlyThisLayoutIsPresent` is a fixture guard: it fails if another
    layout's record leaked into a case's root (which would re-ask the precedence
    question silently);
  - **zero-tree-walk requirement per case** (`countSessionTreeScans`): the two
    deep-scannable layouts (bare, task-run) resolve to the very file the deep
    scan would return, so a blind value assertion is unfalsifiable there — the
    walk counter is what makes breaking an addressed probe fail the case it
    belongs to.
- New deterministic guard `TestSessionLayoutPrecedenceIsTheScanOrder` pins WHICH
  address wins when several coexist — the question the per-layout cases
  deliberately do not ask. Both directions (session id sorting before / after
  the task id), both entry points (row-addressed lookup + the deep scan it
  stands in for), flat record present as a loser in both.
- Layout names (`layoutNameTaskRun` / `layoutNameBare` / `layoutNameFlat`) are
  named constants used as the probe identity — no bare keyword strings (Rules
  1b/13).
- Test-only diff: `internal/tools/taskledger_cost_test.go` (+146/-10).
  `internal/tools/tasks.go` is **untouched** on the branch.

## Not Done (honest)

- The sibling evidence in the table below is a session/layout-scoped selection,
  **not** the whole `internal/tools` package suite: the full package exceeds
  Go's default 10m budget on this host (recorded in
  `devdoc/taskfindjsonl-layout-determinism.md`), and this task's scope is a
  test-only fixture fix. The acceptance commands from the epic are all green.
- `taskFindSessionJSONLForRow` was not changed — the ruling is that it is
  already correct. The prior attempt (`fix/taskfindjsonl-layout-determinism`,
  `3f212cb2`) reached the same ruling and was read as information only
  (Rule 23); this branch starts fresh from master and re-derives it.

## Red-proof evidence (each layout broken in the resolver → that case reddens → restored)

| probe broken | command (in the worktree, `internal/tools/tasks.go` temporarily edited) | observed |
|---|---|---|
| flat answered (`findSessionJSONLFlat` → `return ""`) | `go test ./internal/tools/ -run TestTaskFindSessionJSONLStillHonoursEveryLayout -count=1` | `--- FAIL: .../flat`: `flat layout resolved "", want ".../<sid>.jsonl"` — exactly ONE case reddens |
| both addressed probes dead (`sessionBareRecordPath` and `taskRunRecordPath` → `return ""`) | same | `--- FAIL: .../bare` and `--- FAIL: .../task-run`: `layout cost 1 sessions-tree walk(s); the row-addressed lookup must resolve it, not the scan fallback` — this is the non-vacuity proof: the value assertion alone would have PASSED (the deep scan returns the same file) |
| precedence inverted (`if id < tid` → `if id > tid`) | `go test ./internal/tools/ -run TestSessionLayoutPrecedenceIsTheScanOrder -count=1` | BOTH subtests red: `row lookup resolved ".../t-bare-loses-row/<sid>.jsonl", want ".../<sid>/session.jsonl"` and the mirror case |

`git checkout -- internal/tools/tasks.go` after each probe; `git status
--porcelain` empty afterwards (only the test file is in the commit).

## Verification

- Build: `go build ./...` → **OK** (in the worktree, master + this commit)
- Acceptance: `go test ./internal/tools/ -run TestTaskFindSessionJSONLStillHonoursEveryLayout -count=20` → **ok 1.347s**, 0 failures (60 test runs, 3 subtests × 20)
- Precedence guard: `go test ./internal/tools/ -run TestSessionLayoutPrecedenceIsTheScanOrder -count=20` → **ok 0.480s**
- Epic's sibling selection: `go test ./internal/tools/ -run 'SessionDirHints|TaskFindSessionJSONL|LedgerNamedDir|RowAddressResolution' -count=1` → **ok 1.852s**
- Broader session/layout scoped selection: `go test ./internal/tools/ -run 'Layout|Session|Ledger|RowAddress|Hint' -count=1 -timeout 1800s` → **ok 8.123s**, exit 0.
- `gofmt -l internal/tools/taskledger_cost_test.go` → no output (clean)
- No heavy/device-load test was written or run (Rule 27). Fixture roots were
  pointed at `/dev/shm` scratch via `GOPI_TMPDIR` — off the `/` partition
  (Rule 22).
- Commit: `14b4ee41` `test(tools): give each session-record layout its own fixture root`
  on branch `fix/taskfindjsonl-layout-determinism-v3` (worktree
  `/home/user/workspace/gopi-layoutdet-v3`).

## Deliverable location (Rule 6d)

- Branch: **`fix/taskfindjsonl-layout-determinism-v3`**, worktree
  `/home/user/workspace/gopi-layoutdet-v3` (shares the main repo's object store
  and refs — the commit is already in `/home/user/work/GoPi`, no push needed).
- Recovery: `git -C /home/user/work/GoPi show fix/taskfindjsonl-layout-determinism-v3:internal/tools/taskledger_cost_test.go`
  and `...:devdoc/taskfindjsonl-layout-fixture-isolation.md`
- The branch must not be merged by its own author (Rule 6e).
