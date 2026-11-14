<!-- doc-class: historical -->
# Devdoc: redundancy cleanup (testsuite-gopi)

## What & why

Code-review-redundancy-report.md (11 findings: 5 high / 4 med / 2 low) found
structural copy-paste across internal/. This task (1) locked the behavior of
each at-risk area with unit tests, then (2) removed the redundancy under those
locks.

## The recipe that worked (reuse later)

1. **Test-first**: for each redundant/dead-code area, write the Phase-1 test
   that pins current behavior BEFORE touching source. Test the *cleanest
   testable surface* (e.g. `compactConfigFromEnv` rather than the block deep
   inside `NewAgent`).
2. **Surrounding-behavior tests** for dead code: never test the dead symbol
   itself (a test on it would block its deletion). Instead test what should
   still hold *after* deletion (e.g. `Bash(cmd) ≡ BashInDir(cmd,"")`, job
   registry count via `registeredJobIDs()` not `JobCount`).
3. **Verify zero production callers** by grep excluding `_test.go` and the
   definition line. Only then delete.
4. **One convergent helper per cluster**: withTimeout[T] (5 wrappers),
   expandHome (3 tools), envInt/envFloat (compact.go's 5 blocks).
5. **Cross-package merge without a new package**: export the canonical type
   (agent.FlexibleString + .Resolve()), alias in the sibling
   (gopilog `type todoItem = agent.TaskItem`). Check import direction first
   (agent must not import gopilog — it doesn't).
6. **Capture-os.Stderr deterministically**: redirect os.Stderr to a temp file
   and read it back (a pipe+goroutine drain races the read); the write-side
   helper must remember to Close in cleanup so the OS doesn't keep a stale fd.

## Merged items

| item | merge | key trick |
|------|-------|-----------|
| flexibleString/TaskItem ↔ gopilog | agent is canonical; gopilog aliases | exported FlexibleString(has String())/Resolve; agent does not import gopilog → no cycle |
| 5×withTimeout ×5 | withTimeout[T] + withTimeoutErr | op takes `func(ctx)` so context-aware ops (search's exec.CommandContext) still cancel at deadline |
| ~ / ~/ home expan ×3 | expandHome helper | bare-~ vs ~/-only drift resolved by aligning all three to read's semantics (epic-endorsed) |
| env-int parse ×5 (compact) | envInt/envFloat | pass a validity predicate (per-var range) + display value (preserves per-site warning text) |
| JobCount/EditDetails/taskID/createAt/Scan | delete | zero production callers confirmed; test-only helpers removed with their tests |

## Deferred (存疑, with reason)

- `Bash` deletion — thin alias, used as a test convenience across many tests;
  equivalence locked, keep it.
- loop.go / NewAgent env parsing — ~8 blocks, but embedded in a heavy
  constructor not unit-testable; converged helper exists but not applied
  (would be an unprotected change → violates the no-destructive-change rule).
- tools/timeout.go init() env parsing — Rule-2 single point, silent (no
  warn), different semantics; cross-package helper reuse not worth a new pkg.
- SessionLogger/RawLogger buffered-logger merge — perf-sensitive fsync path
  + different event formats; deferred (not in the 5-item Phase-2 list).

## Guard: run the full suite after every refactor commit

`go test ./...` takes ~3min (tools pkg alone ~122s) — use job_start and keep
working; don't poll. Exit 0 throughout.
