<!-- doc-class: historical -->
# Master red after the 2026-09-18 batch land: the reported tip was already stale

Task row: `01a0b5f5-bb69-7e18-999e-173d830f2bc1` (session ID = row ID).
Branch: `fix/master-batch-land-red-rework`, off master `9e6d7844`.
Original branch: `fix/master-batch-land-red` (tip `89351a7b`, dangling — see
"why the rework" below).

## What was reported, and what was actually true

The first run of this work (row `01a0b4cb`, master tip `2341bfcc`) reported two
reds:

- (a) `cmd/agent`'s test package did not compile — `merge_lock_e2e_test.go`
  called `mtlGit`/`mtlProbe`, defined in `merger_tick_legs_test.go`, which the
  batch-land merge of `feat/merger-retire` (`b201d7e0`) deleted;
- (b) `internal/standards` failed because `gate_test.go` still read
  `cmd/agent/merger_tick_legs_test.go` by name.

**Neither was true of the tree when that run started.** Measured read-only
before any edit:

| check | result |
|---|---|
| `go build ./...` | exit 0 |
| `go vet ./...` | exit 0 |
| `go test ./internal/standards/ -count=1` | **FAIL** — but for Rule 17, not a missing file |
| `git ls-tree master -- cmd/agent/merger_tick_legs_test.go` | absent (the retire was real) |
| `git grep -ln mtlGit master -- cmd/agent/` | `cmd/agent/merge_lock_e2e_test.go` — the ONLY hit, and it compiles |

Two things had moved underneath the report:

1. **`8f7c0e14` had already repaired red (a)** — ("fix(merge-lock): inline
   mtlGit/mtlProbe into the merge-lock e2e file") inlined both helpers into the
   sole surviving caller, ~1 minute after the reported tip.
2. **Red (b) does not exist and never did at that tip.** A repo-wide search
   (`grep -rn 'merger_tick_legs'`, excluding `.git`) finds the basename in
   **exactly one place**: the historical doc `devdoc/merger-tick-legs.md`
   line 33. No production file, no test file, no `testdata` inventory names it.

## The red that was actually live, and its attribution

```
--- FAIL: TestStandardGates
    --- FAIL: TestStandardGates/TestGate2TestEnvRedProof
        gate_test.go:111: Rule 17 (test env isolation): NEW violation
          (checklist 5.2) at cmd/agent/merge_lock_test.go:200
          [os.Setenv/os.Unsetenv in a test leaks process state — use t.Setenv]
--- FAIL: TestStandardGateDataReport
    gate_test.go:130: Rule 17: 1 hit(s) are neither baselined nor escaped
```

Attribution, measured rather than inferred:

- **Which commit** — `git_blame` on line 200: `5c8a8fac` ("feat(merge): `gopi
  merge` takes the single-instance lock for the run", 2026-09-18 13:20:43Z).
- **Which merge put it on master** — `5c8a8fac` first appears on master in
  `ff9678eb` (`merge: feat/merge-flock into master (batch land)`).
- **Did the merge introduce the hit?** No. `git diff af7d8630 ff9678eb --
  cmd/agent/merge_lock_test.go` is EMPTY and `git show
  af7d8630:internal/standards/baseline.go` contains **zero** entries for that
  file. The branch was already red on Rule 17 when it landed; the batch land
  promoted a branch-local red to master.
- **Why the batch land could not see it** — a post-merge gate that re-runs
  `go vet ./...` **and** the standards/stylegate legs over the merged result
  exists: `internal/tools/rebuild_vet_mergeset.go` (`RebuildAfterMergeMergeset`,
  armed in `RebuildAfterMerge`'s deferred step, default on). It landed in
  `2341bfcc` **itself**, i.e. the detector arrived in the same round as the
  reds, and the binary that landed them did not contain it. `gopi task list
  --all` has **zero** rows matching "post-merge gate": it has never yet fired on
  a landing. `bin/gopi`'s mtime (22:01:04) is after both the break (21:52) and
  the fix (21:55), so it only became live later.

## Why the rework, and what was STILL live at `9e6d7844`

The original branch (`bc6c2bdb` + two devdoc commits, tip `89351a7b`) no longer
merges: master had in the meantime adopted its own fix for the same line —
`5c8a8fac`'s escape landed as `env-leak-ok: the child merge-lock holder inherits
this profile at exec time, so t.Setenv's automatic restore would come too late;
the SnapshotEnv pair above is the restore` (row `01a0b5b6`, master `9e6d7844`).
So the archived branch's version of the line, and the `CONFLICT (content)` the
merge run reported, are both about a repair master had already half-made.

**Half, not whole.** The `env-leak-ok` escape satisfies
`internal/standards` Rule 17 only. A second swept gate reads the same line and
accepts neither a bare `os.Setenv` nor that spelling:
`internal/stylegate`'s `test-setenv` ceiling is 0, and
`reasonOverSpan` accepts exactly two forms — `//stylegate:ignore <reason>` (≥ 10
chars) or the prose phrasings "deliberately ignored" / "intentionally ignored".
`env-leak-ok` is none of them, so the gate stayed red on that one site:

```
gate_test.go:174: stylegate test-setenv: 2 test hit(s), ceiling 0
      sites:
        cmd/agent/merge_lock_test.go:200 (os.Setenv leaks into the process env — use t.Setenv)
        internal/shot/launch_test.go:100 (os.Setenv leaks into the process env — use t.Setenv)
```

## The repair

`cmd/agent/merge_lock_test.go` — 1 file, +8/−1, no behavioural change: the
`//stylegate:ignore <reason>` marker on the one line, plus a doc-comment
paragraph recording why the explicit `os.Setenv` + `testutil.SnapshotEnv` +
`t.Cleanup` pair is the reviewed form here and `t.Setenv` is the wrong tool (its
undo is registered by the `t.Setenv` call and runs at test cleanup, while the
caller reads the pinned profile for the whole scenario, including the child
holder's exec).

```go
if err := os.Setenv("GOPI_HOME", home); err != nil { //stylegate:ignore the profile stays pinned for the whole SCENARIO, so the undo belongs at test cleanup (t.Cleanup(restoreHome) above), not at this helper's return // env-leak-ok: the child merge-lock holder inherits this profile at exec time, so t.Setenv's automatic restore would come too late; the SnapshotEnv pair above is the restore
```

Inside `internal/standards` itself this stays an **escape**, not a baseline
entry — a permit is for a violation that ALREADY existed, an escape is the
opposite (*this line is correct and here is why*). Precedent for the double
marker: `internal/testutil/tmproot_assert_test.go:152`.

## Verification (branch `fix/master-batch-land-red-rework`, tip `4468154b`)

Fresh `git worktree add --detach /home/user/workspace/gopi-blr-baseline
9e6d7844` of PURE master supplies the differential, so every number below is
tip-vs-master, not tip-alone:

| gate | master `9e6d7844` | branch tip | verdict |
|---|---|---|---|
| `internal/stylegate` `test-setenv` | 2 sites (incl. `merge_lock_test.go:200`) | **1 site** (`internal/shot/launch_test.go:100` only) | the claimed hit is fixed, no new one |
| every other stylegate rule (`ignored-error` 225/163 + 9/0, `test-sleep` 9/0, `test-skip` 2/0, `unchecked-close` 2/0) | identical | identical | pre-existing master reds, other rows |
| `internal/standards` Rule 16 `%w` | NEW=2 (`internal/docclass/docclass.go:348`, `internal/tools/memory_sync_claim.go:917`) | NEW=2, identical | pre-existing |
| `internal/standards` Rule 17 | hits=123 baselined=72 escaped=28 NEW=23 | identical | pre-existing |
| `internal/standards` Rule 17 at `cmd/agent/merge_lock_test.go:200` | already `escape:` | still `escape:` | unchanged by this branch |

```
go build ./...                                   exit 0
go vet ./cmd/agent/ ./internal/stylegate/ ./internal/standards/   exit 0
go test -run '^$' ./cmd/agent/ -count=1          ok — the whole test tree compiles
```

The stylegate differential is the load-bearing one: it is the only gate whose
report changes between master and this tip, and it changes by exactly the one
site the branch claims. `internal/shot/launch_test.go:100` is master's own
(commit `027a3c18`, "test(shot): reason the per-case env flip with the gate-2
escape it needs" — it reasoned the STANDARDS gate and left the swept one, the
same half-repair shape as this branch's subject; it is a different row's work).

`TMPDIR` was pinned off the `/` partition for every run
(`/dev/shm/gopi-test-scratch/tmp`). No load was generated (Rule 27); the
`cmd/agent` measurement is the project's own `make test-budget` path.

## Not done (honest)

- **The other stylegate ceilings were not touched.** `ignored-error` (225 test
  hits against 163, 9 production against 0), `test-sleep` 9/0, `test-skip` 2/0,
  `unchecked-close` 2/0 are red on clean master, are not caused by this line,
  and already have ledger rows (`01a0b3d6` ceilings, `01a0b47e` rework,
  `01a0b4dc`). This branch does not green `internal/stylegate` — it removes one
  of its six over-ceiling rules.
- **`internal/standards` Rule 16 (2 NEW) and Rule 17 (23 NEW) are still red on
  master** and are not in this branch's scope.
- **`internal/shot/launch_test.go:100` — the remaining `test-setenv` site — was
  not fixed.** It is master's own half-repair (`027a3c18` reasoned Rule 17 and
  left the swept rule), the identical defect this branch repairs on
  `merge_lock_test.go`. Fixing it is one marker; it is left to its own row
  rather than smuggled into this one (Rule 26: an incomplete fix is repaired by
  dispatching again).
- **The merge-set gate's arrival lag is not repaired here.** A merge cannot be
  caught by a detector landed in the same round; the durable fix would be to
  rebuild `bin/gopi` before the running merger evaluates a round. That is merger
  machinery and Rule 28 puts the system layer on the server host, not in this
  branch. Reported, not changed.
- **No new regression guard was added.** The mechanically expressible guard
  already exists and is what this change satisfies: stylegate's `RuleTestSetenv`
  ceiling fires on any new unescaped `os.Setenv` in a test, and `internal/standards`
  Rule 17 on any unexplained one. A guard for the cross-cutting cause — a merged
  result that does not compile — already landed as the merge-set gate in
  `2341bfcc`. Note `go build ./...` cannot serve as that guard: it never
  compiles `_test.go`, which is why stale test trees read green (memory #3141).

## Sibling rows (Rule 26: the duplicate dispatch is deliberate)

Four rows were dispatched at 03:19 on 2026-09-19 for the same
`CONFLICT (content) cmd/agent/merge_lock_test.go`, from the same
pre-merge run: `01a0b5f5-bf70` (`fix/stylegate-ratchet-master-base`),
`01a0b5f5-be84` (`fix/rule17-merge-lock-escape`), `01a0b5f5-bd84`
(`fix/rule17-envescape`) and this one. The first three target the Rule 17
escape, which master has ALREADY taken (`01a0b5b6`, `9e6d7844`); this row's
finding is that the remaining gap is the OTHER gate on the same line. A merger
reading this list should land the smallest diff that greens a gate master
currently fails, not four copies of the same line.
