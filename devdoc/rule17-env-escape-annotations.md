<!-- doc-class: historical -->
# Task Report: Rule 17 env-escape annotations (standards gate suite green on master)

## Completed

- Diagnosed the red Rule 17 (test env isolation) gate on master `9cc034bc`.
  The epic named ONE unannotated site; the gate in fact reported **TWO**
  (`NEW=2`). Both are now annotated:
  - `cmd/agent/cli_voice_review_test.go` — `TestUserHomeDirOrDefaultFallsBack`
    (`os.Unsetenv("HOME")`)
  - `cmd/agent/worktree_release_accept_test.go` — `releaseOwnRow`
    (`os.Setenv(tools.TaskWrapperEnv, row)`)
- Both sites were annotated with the gate's **reviewed inline escape**
  (`// env-leak-ok: <reason>`), not a line-keyed baseline permit.
- `internal/standards/baseline.go` is **untouched**: 94 entries before and after.
  Two new permits would have been widening; escapes are the ratchet-compatible
  form and keep both sites VISIBLE in `TestStandardGateDataReport`.

### Why an escape and not a conversion (both sites are correct by construction)

Neither site can take `t.Setenv`:

1. `cli_voice_review_test.go` — the test's **subject is ABSENCE**. `t.Setenv`
   only ever SETS a variable (its restore makes `LookupEnv` report the variable
   present); it cannot express "HOME is unset". `os.Unsetenv` + the reviewed
   `testutil.SnapshotEnv(t, "HOME")` (which captures with `os.LookupEnv`, so an
   absent variable comes back absent) is the only correct construction.
2. `worktree_release_accept_test.go` — a **per-call flip**: the wrapper env must
   be installed only for the duration of one child-process-spawning call, then
   undone when THAT CALL returns. `t.Setenv`'s undo is TEST-scoped and would
   leave the last value installed for the rest of the test — the wrong scope.

Escape-marker placement matters: `linesAbove` in `internal/standards/gate.go`
scans the offending line plus the **contiguous** comment block directly above it
and stops at the first non-comment line. The marker is therefore placed at the
END of a comment block adjacent to the statement (the existing convention in
`internal/testutil/envsnapshot_test.go` and `cmd/agent/dispatch_test.go`), not
floating above the enclosing function.

## Not Done

- No baseline entry was added or re-pointed (deliberate — the constraint was
  "never widen the baseline"; escapes are the preferred channel).
- The other 11 pre-existing escapes were left as they are; no unrelated cleanup.
- No change to the gate predicate itself — the fix is a site annotation, exactly
  as scoped.

## Verification

Before (master `9cc034bc`, `go test -count=1 -run TestStandardGates ./internal/standards/`):

```
Rule 17 (test env isolation): 107 total hits, 94 baselined, 11 escaped, 2 new (unannotated), 89 baseline entries
Rule 17: NEW violation (checklist 5.2) at cmd/agent/cli_voice_review_test.go:1301
Rule 17: NEW violation (checklist 5.2) at cmd/agent/worktree_release_accept_test.go:225
```

After (branch `fix/rule17-env-escape-annotations`, `go test -count=1 -v -run TestStandardGateDataReport ./internal/standards/`):

```
Rule 16 (%w error wrapping) (checklist 2.2): hits=8  baselined=8 escaped=0  NEW=0
Rule 17 (test env isolation) (checklist 5.2): hits=107 baselined=94 escaped=13 NEW=0
  escape: cmd/agent/cli_voice_review_test.go:1305
  escape: cmd/agent/worktree_release_accept_test.go:231
Rule 18 (no else after terminating if) (checklist 4.3): hits=5 baselined=5 escaped=0 NEW=0
Rule 19 (no panic outside init/main/Must*) (checklist 2.7): hits=1 baselined=0 escaped=1 NEW=0
Rule 20 (no get/is/handle prefix on an accessor) (checklist 1.4): hits=1 baselined=0 escaped=1 NEW=0
Rule 21 (env-name constant must have a real reader) (checklist 1b/env): hits=0 baselined=0 escaped=0 NEW=0
--- PASS: TestStandardGateDataReport
```

Rule 17 arithmetic: `94 baselined + 13 escaped + 0 new = 107 hits` — preserved,
with the 2 hits moved from the `new` column into `escaped`. `NEW=0`, `STALE=0`.

- Gate suite: `go test -count=1 ./internal/standards/` → **ok** (TestStandardGates
  + TestStandardGateDataReport green; the negative classes are empty).
- Annotated test still passes: `go test -count=1 -run TestUserHomeDirOrDefaultFallsBack ./cmd/agent/` → **ok**.
- gofmt: `gofmt -l cmd/agent/cli_voice_review_test.go cmd/agent/worktree_release_accept_test.go` → empty (clean).
- Build: see commit footer / job output (`CGO_ENABLED=0 go build -o bin/gopi ./cmd/agent/`).
- Main repo untouched: `git status --short cmd/agent/` in `~/work/GoPi` is EMPTY;
  both modified files are listed only inside the worktree.

## Notes for future agents

- `gate_test.go` counts and classifies in ONE pass (`classifyAll`); the count
  `new` is the class `enforce()` fails on, and the row's classes are asserted to
  sum to the scan's hit total. **Read the counts, not the log phrasing** — an
  older revision printed `hits - len(baseline)` (a hit count minus an entry
  count) and could log a positive number on a green run. That derivation is gone;
  the numbers quoted above are counted, not derived.
- A `hits` figure does not change when a site is annotated — only the split
  between `baselined` / `escaped` / `new` does. Expect `hits=107` before and after.
- Rule 16/18/19/20/21 baselines were already green; this change touches only
  Rule 17's escape set.
