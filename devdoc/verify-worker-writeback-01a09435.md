<!-- doc-class: historical -->
# Task Report: independent verification of ledger 01a09435 (worker writeback gap)

Verifier role: **read/build/test only — nothing was edited, no fix touched.**
Fix under review: `001b09f8` "fix(task): close a run's ledger row when the wrapper
dies before writeback" (parent `d259403e`), merged to master at `9c604ee8`.
Tree verified: `/home/user/work/GoPi` @ `9c604ee8` (= master HEAD).

## Completed

- Located the fix and proved it is in master (`git merge-base --is-ancestor
  001b09f8 master` → exit 0; master HEAD `9c604ee8` = "Merge branch
  'fix/worker-writeback-normal-exit'").
- Reconstructed the diff scope from the commit itself (8 files, +1215/−12).
- Build / format / vet gates on the merged tree.
- Ran the touched-package suites with `-count=1`, including the **full
  `internal/tools` suite** that devdoc §E.5 left owed.
- Proved the negative-case RED at the pre-fix parent `d259403e`.
- Reproduced devdoc §C's `taskzombie` numbers from the live notify sink.
- Recorded ONE verdict on the ledger row via the CLI (terminal state).

## Not Done

- The **E2E**-level wrapper-death negative case could not be verified because it
  is not in the merged tree (see Deviations 1). The criterion is met by the
  deterministic tools-layer test instead.
- `internal/agent` and `cmd/agent` suites were run with the verifier session's
  `GOPI_AUTO_TASK_REGISTER` removed (see Deviations 2) — that is a session-env
  scrub, not a source change.
- No fix was applied to anything; no merge was performed (Rule 6: not the
  verifier's job).

## Evidence

### Diff scope — `git show --stat 001b09f8`

```
 cmd/agent/cli_tasks.go                       |  38 ++-
 cmd/agent/cli_tasks_worker_writeback_test.go | 141 +++++++++
 devdoc/worker-writeback-gap-20260912.md      | 432 +++++++++++++++++++++++++++
 internal/agent/taskregister.go               |  88 ++++++
 internal/tools/task_run_closeout_test.go     | 243 +++++++++++++++
 internal/tools/tasks.go                      | 193 ++++++++++++
 internal/tools/taskzombie.go                 |  37 +++
 internal/tools/taskzombie_test.go            |  55 ++++
 8 files changed, 1215 insertions(+), 12 deletions(-)
```

### Build / format / vet (merged tree)

```
CGO_ENABLED=0 go build ./...   → exit 0, no output          (go1.24.5 linux/amd64)
gofmt -l .                     → 0 lines
gofmt -l internal cmd          → 0 lines                    (the gate tree)
go vet ./internal/tools/ ./internal/agent/ ./cmd/agent/ → exit 0
```

### Targeted closeout tests — `go test -count=1 -p 1 -v ./internal/tools/`

```
--- PASS: TestRunCloseoutClosesOpenRow (0.80s)
--- PASS: TestRunCloseoutIdempotentAfterChildSelfReport (0.79s)
--- PASS: TestRunCloseoutRefusesUnclaimedRow (0.02s)
--- PASS: TestRunCloseoutRejectsNonTerminalOutcome (0.02s)
--- PASS: TestRunCloseoutRefusesLiveWrapper (0.02s)
--- PASS: TestPidAlive (0.01s)
--- PASS: TestPidAliveZombieIsNotAlive (0.01s)
--- PASS: TestZombieVerdict_NoClaimStamp (0.00s)
--- PASS: TestZombieVerdict_LivePidVetoes (0.00s)
--- PASS: TestZombieVerdict_DeadPidConfirmed (0.01s)
--- PASS: TestZombieVerdict_ForeignHost (0.00s)
--- PASS: TestZombieVerdict_MalformedStamps (0.00s)
ok  github.com/nick/gopi/internal/tools  1.701s
```

### Full touched packages, `-count=1` (background jobs, serial `-p 1`)

```
ok  github.com/nick/gopi/internal/tools   226.233s   TOOLS_FULL_EXIT=0
ok  github.com/nick/gopi/internal/agent    62.762s   agent_exit=0
ok  github.com/nick/gopi/cmd/agent        120.707s   CMDAGENT_EXIT=0
```

The first line discharges devdoc §E.5's owed full-suite run for `internal/tools`
(the 36 `task_merged_cleanup_test.go` fork/exec EAGAIN failures were resource
exhaustion, not logic — confirmed green now on a recovered host).

### RED proof — pre-fix parent `d259403e` (throwaway detached worktree `/tmp/verifyprefix01a09435`)

```
$ grep -rl 'TaskRunCloseoutStaleRun' --include=*.go .     # pre-fix tree
0 hits

$ cp <fix-tree>/internal/tools/task_run_closeout_test.go internal/tools/ && go test -run TestRunCloseout ./internal/tools/
internal/tools/task_run_closeout_test.go:55:11: undefined: TaskRunIDEnv
internal/tools/task_run_closeout_test.go:58:23: undefined: TaskRunCloseoutStaleRun
internal/tools/task_run_closeout_test.go:58:47: undefined: RunCloseoutRequest
… (too many errors)
```

The negative case therefore **fails red pre-fix by construction** — the entry
point it exercises did not exist. On the fixed tree the identical file compiles
and 5/5 close-out tests pass.

### Root cause — confirmed by grep on the fix tree

```
cmd/agent/cli_tasks.go:1225  func runTaskRunOnce(...)
cmd/agent/cli_tasks.go:1412  tools.RunCloseoutPair(target, TaskStatusCompleted)   ← the new seam env
cmd/agent/cli_tasks.go:1441  runErr := cmd.Wait()                                 ← terminal writes follow it
cmd/agent/cli_tasks.go:1481  tools.TaskFinishCleanupSessionID(target)             ← also post-Wait
internal/agent/taskregister.go:191  a.autoTaskRegistered = false                  ← pre-existing gate
internal/agent/taskregister.go:201  logAutoTaskf("… wrapper child … no auto row …")
internal/agent/taskregister.go:295  a.closeOutRunRow(...)                         ← new, BEFORE the gate
internal/agent/taskregister.go:296  if !a.autoTaskRegistered || … { return }      ← the old early return
```

The devdoc's line numbers (`:1400 cmd.Wait`, `:1417 completed writeback`,
`:1440 cleanup`; `taskregister.go:187-190`, `:275-277`) are **drifted by ~40
lines / 8 lines** against the merged tree — the mechanism and ordering it states
are exact, the pinned numbers are not.

### devdoc §C (`taskzombie` did not report the 7 rows) — reproduced from the live sink

Streaming `json.JSONDecoder().raw_decode` over `~/.gopi/notify/task-events.jsonl`
(47.1 MB, **127 400 JSON objects, 2 physical lines** — a line-based parser
under-reports badly, as the doc warns):

```
stale_events = 1316
zombie(confidence,pid_dead) = {('suspected', False): 836, ('confirmed', True): 480}
distinct rows with confidence=confirmed = 360
01a0924c-5201 n=6  08:22:15 → 14:50:24
01a09260-f5c8 n=6  08:56:32 → 14:50:24
01a092a6-43a2 n=8  08:16:01 → 14:50:25
01a092a6-6011 n=9  08:03:26 → 14:50:25
01a092ab-db9b n=7  08:22:18 → 14:50:26
01a092b2-4f21 n=8  08:06:56 → 14:50:26
01a092b3-592e n=8  08:10:57 → 14:50:27
```

Exactly the devdoc table (9/8/8/8/7/6/6). `SweepZombies(` has **zero non-test
callers** on this tree (detector without a disposer, confirmed); `pidAlive` now
consults `/proc/<pid>/stat` state `Z` before the signal-0 probe.

### Ledger row 01a09435 (before the verdict)

```
status: completed   run_method: worker   deliverable.branch: fix/worker-writeback-normal-exit
events: in_progress 09-12 14:54 → reframe (DOABLE) 09-12 14:55 → completed 09-12 16:28
        ("gopi run completed — run: elapsed=1h33m52s")
```

No `[stale]` event exists for this row's own id in the sink — it never sat in
the zombie class.

## Deviations found (recorded, non-blocking)

1. **Dangling citation for the E2E negative case.** The test file's header cites
   `TestWorkerRunRowClosesWithoutWrapperWritebackE2E`
   (`internal/tools/task_run_closeout_test.go:52`) and devdoc §E.1 quotes a RED
   output from `TestWorkerRunRowClosesWhenTheWorkerNeverWritesBackE2E`. **Neither
   test exists in the merged tree** (grep: 0 hits). The only merged E2E is the
   positive regression guard `TestWorkerRunHealthyPathKeepsTheWrapperResultE2E`.
   The devdoc itself discloses the limit (§D.9, §E.3: the wrapper-death E2E is not
   staged) — but §E.1's stale test name + RED transcript is unreproducible on the
   merged tree. The acceptance criterion ("negative case covered by a test whose
   pre-fix failure is demonstrable") is still met via
   `TestRunCloseoutClosesOpenRow` + the compile-level RED above.
2. **Session-env trap in `internal/agent`.** With the caller's ambient
   `GOPI_AUTO_TASK_REGISTER=0` inherited, 9 `taskregister_test.go` tests fail
   ("start hook did not register"). Reproduced **identically on pre-fix
   `d259403e`** → environmental, not a regression from this fix. With that
   variable unset: `ok internal/agent 62.762s`.

## Verification

- Build: `CGO_ENABLED=0 go build ./...` → exit 0 (no output)
- Format: `gofmt -l .` → 0 lines; `gofmt -l internal cmd` → 0 lines
- Vet: `go vet` on the three touched packages → exit 0
- Tests: targeted 12/12 PASS; `internal/tools` ok 226.2s; `internal/agent` ok
  62.8s; `cmd/agent` ok 120.7s (all `-count=1`, serial)
- RED proof: pre-fix `d259403e` — seam absent (0 hits), new test file
  fails to compile (`undefined: TaskRunCloseoutStaleRun`)
- §C reproduction: 1316 stale / 480 confirmed / 360 rows; 7 incident rows
  9-8-8-8-7-6-6 matching the devdoc; `SweepZombies` non-test callers = 0
- Commit: `001b09f8` (in master at `9c604ee8`); this report is untracked evidence
  in the main checkout — the authoritative deliverable is the ledger verdict.

## Verdict

**VERIFIED** — every acceptance criterion met; two documentation-level deviations
recorded above, neither blocking.
