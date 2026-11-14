<!-- doc-class: normative -->
# `internal/tools` re-arm flake: the assertion measured the host, not the renderer

Task `01a13023-6070-730b-904b-38e3b01ed520` (branch `task/01a130236070730b904b38e3b01ed520`).
Base: `ff9a8b0e9` at the start of the run.

`TestTaskRunningSuffixMonotonicAcrossRearm` (`internal/tools/tasklist_running_test.go`)
failed in a whole-package run and passed standalone. It was **not** a concurrency
interleaving and **not** host load in the sense of "the machine is busy": the
assertion itself sampled a live wall clock and then compared that sample, as a
string, against a rendering taken at a *different* instant.

## 1. The failure, as the failing run recorded it

Stream `/home/user/workspace/gopi-test-scratch/queue/test-budget/run-1791808002-2321410.json`
(JSONL, one record per event; the runner's own per-test stream):

```
output | 2026-10-12T20:38:11.97495769+08:00  | '=== RUN   TestTaskRunningSuffixMonotonicAcrossRearm\n'
output | 2026-10-12T20:38:12.000931903+08:00 | '    tasklist_running_test.go:217: after 1st re-arm age = "4h0m1s", want unchanged "4h0m0s"\n'
output | 2026-10-12T20:38:12.001032442+08:00 | '--- FAIL: TestTaskRunningSuffixMonotonicAcrossRearm (0.03s)\n'
fail   | 2026-10-12T20:38:12.001046589+08:00 | Test: TestTaskRunningSuffixMonotonicAcrossRearm, Elapsed: 0.03
output | 2026-10-12T20:39:27.421206318+08:00 | 'FAIL\tgithub.com/the operator/gopi/internal/tools\t724.127s\n'
```

`Elapsed: 0.03` — the failing test is the only `fail` action in the whole
package record; every other `FAIL` line in the stream is fixture output (branch
gates deliberately failing inside their own temp repos).

## 2. The mechanism (deterministic, and the two samples are the whole story)

The pre-fix test did this:

```go
start := time.Now().Add(-4 * time.Hour).Truncate(time.Second)
...
wantAge := taskFormatDuration(time.Since(start))   // sample #1 — live wall clock
if since0 != wantAge { ... }                       // vs a render taken at sample #0
rearm(10)
if since1 := sinceClock(); since1 != wantAge { ... } // vs a render taken at sample #2
```

and `taskFormatDuration` truncates a >=1m duration to whole seconds:

```go
case d >= time.Minute:
        return d.Truncate(time.Second).String() // 2h13m5s
```

So the expected string is a *host observation*: any scheduling pause that pushes
the second read past an integer-second boundary moves the expected value by one
tick. `4h0m0s` -> `4h0m1s` is exactly the 1.0s boundary, which is why the failure
message reads the way it does. Nothing about the re-arm bump, the anchor ladder
or the renderer is involved — the renderer printed `4h0m1s` honestly, 1.1s later.

## 3. The RED proof (the mechanism reproduced on demand)

A temporary file (`internal/tools/zz_rearm_wallclock_red_proof_test.go`, deleted
before the commit) carried the old assertion verbatim plus ONE line: a
`time.Sleep(1100 * time.Millisecond)` between the two samples — the smallest
pause that must cross a whole-second boundary. No load is generated (Rule 27);
a sleep measures nothing and creates nothing.

```
$ go test ./internal/tools/ -run 'TestZZReproRearmWallClockRed' -count=3 -v
    zz_rearm_wallclock_red_proof_test.go:62: rendered row: #t-zombie  [in_progress]  zombie row  ... [unknown: no claim stamp, age 4h0m0s]
    zz_rearm_wallclock_red_proof_test.go:62: rendered row: #t-zombie  [in_progress]  zombie row  ... [unknown: no claim stamp, age 4h0m1s]
    zz_rearm_wallclock_red_proof_test.go:78: after 1st re-arm age = "4h0m1s", want unchanged "4h0m0s"
--- FAIL: TestZZReproRearmWallClockRed (1.13s)
... (rep 3 identical to rep 2) ...
FAIL	github.com/the operator/gopi/internal/tools	14.046s
```

3/3 deterministic, and the failure line is byte-identical to the production
failure — `after 1st re-arm age = "4h0m1s", want unchanged "4h0m0s"`. That is
the proof that the cause is a wall-clock sample crossing a second boundary, not
a concurrency interleaving. Full log:
`~/.gopi/sessions/01a13023-6070-730b-904b-38e3b01ed520/artifacts/red-proof-log.txt`.

## 4. The fix: a controlled `now`, and two surfaces instead of one

`now` is now a **fixed instant** (`zombieControlledNowUnix`), never a reading,
and it is fed to the renderer directly — so every expected string is a constant
and the host's clock is not an input to any assertion. The property is asserted
on **two** surfaces, because it has two halves:

1. **The render path** — `taskStateSuffix(row, time.Unix(zombieControlledNowUnix, 0))`
   must print exactly `zombieRearmAgeLabel` (`"4h0m0s"`) before the re-arm and
   after each of the two bumps. This is the shape the old test was reaching for,
   minus the host.
2. **The DB path** — the live `TaskList("in_progress")` row must carry the same
   real start (`created <stamp>`, compared as a string) across both bumps, and
   the fixture asserts that `updated_at` **provably moved** to +10min and +20min
   in the same steps. So the test now establishes that the column the re-arm
   writes really did change while the displayed start did not — the "don't
   anchor on updated_at" property, made positive instead of merely implied.

Nothing was weakened and nothing was deleted: the elapsed-age equality, the real
anchor, the `~`-marker tripwire and the two re-arm rounds are all still there.

**The assertion still bites.** Mutation check (Rule: a green that no mutation can
turn red is not evidence): `taskRunningAnchor` was replaced with
`return t.UpdatedAt` in a copy of `tasks.go` and the test run —

```
    tasklist_running_test.go:289: after 1st re-arm age = "3h50m0s", want unchanged "4h0m0s"
--- FAIL: TestTaskRunningSuffixMonotonicAcrossRearm (0.03s)
```

— red, with the anchor-on-`updated_at` defect named by the message (`3h50m0s` is
`4h - 10min`, i.e. the age measured from the bumped column). `tasks.go` was
restored immediately; `git diff internal/tools/tasks.go` is empty in the commit.

## 5. Verification

```
Build:  go build ./...                                            -> OK
        go vet ./internal/tools/                                  -> exit 0
Tests:  go test ./internal/tools/ -run 'TestTaskRunning|TestTaskListRunningSuffix|TestTaskFormatDuration' -count=20
        -> ok  github.com/the operator/gopi/internal/tools  10.734s
        go test ./internal/tools/ -run TestTaskRunningSuffixMonotonicAcrossRearm -count=20 -v
        -> 20/20 '--- PASS'
Ran:    see §3 (RED proof) and §5's count runs — the surfaces the change
        touches are the test's own two paths, both exercised above.
```

Live-clock audit of the file: `time.Now`/`time.Since` occur at lines 32, 80, 118
(sibling tests) and inside the doc comment; **zero** inside
`TestTaskRunningSuffixMonotonicAcrossRearm` and zero inside every helper it
calls, so no expectation in this test is derived from a clock.

## 6. Not done (honest)

- The other `time.Now()` uses in this file (lines 32/80/118) are fixtures that
  only *seed* rows — they do not participate in an exact string comparison — so
  they were left alone (in-scope change only).
- The whole-package `make test-budget PKGS='./internal/tools'` run is reported in
  the task report; `internal/tools` takes ~724s under load, so it is a
  background job and its result is not a claim of this document.
- No `testdata/flakes.tsv` row was added: the test is deterministic again, and a
  registry row for it would be a suppression entry wearing a registry row's
  clothes.
