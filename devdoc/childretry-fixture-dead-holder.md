<!-- doc-class: historical -->
# Task Report: the dead-holder retry fixture named a pid the kernel was about to hand out

Branch: `fix/childretry-fixture-dead-holder2` (fresh, off `master` 9ce8e159 —
Rule 23: opened new, never continued on an earlier attempt). Worktree:
`/home/user/workspace/gopi-childretry-fix`.

## Completed

Four `cmd/agent` child-retry tests (`ChildRetry|RetryCounter|RetryRefuses|
JudgedChildFailure`) failed on plain master at ONE shared fixture precondition,
not at the behaviour they assert. Both halves of the defect and both halves of
the fix are in the fixture; **no production classification was touched**
(`taskIdentityVerdict`, `TaskProcessStateX`, `TaskRunChildResumable`,
`TaskStampHolder`, `PidAlive` are all untouched — `git diff --stat` for
`internal/` is empty).

### Root cause (with evidence)

`retryDeadPid()` (old `cmd/agent/cli_tasks_child_retry_test.go:105`) returned
**the highest `/proc` entry + 1**. That is not a RESERVED pid — pids are
allocated sequentially, so the returned number is the very NEXT pid the kernel
will hand out. The fixture then wrote the claim trail with an **EMPTY identity
triple** (`proc_pid = 0, proc_start_ticks = 0, proc_cmd = ''`) via raw SQL.

Two independent defects, and together they produce the reported verdict:

1. **The pid was not reserved.** A live process — most likely one spawned
   between the `os.ReadDir("/proc")` scan and the classifier's read — wore the
   number.
2. **The empty triple pushed the classifier onto the name guard.** With no
   identity on the row, `taskIdentityVerdict` (`internal/tools/taskproc.go`)
   falls past the start-tick axis (`recorded.HasStr` false) and the cmdline
   axis (`recorded.Cmd == ""`) to the legacy arm at the bottom:
   `taskPidIsRunner(pid)` — `/proc/<pid>/comm` starts with `gopi`. A live
   gopi-named process at that pid therefore answered **`running`**.

What the classifier READ vs what the fixture PRODUCED:

| axis read by `taskIdentityVerdict` | fixture produced | consequence |
|---|---|---|
| `pidAlive(pid)` (signal-0 minus zombie) | pid recycled onto a LIVE process | not dead |
| `recorded.Start` (start ticks) | `0` → `HasStr` false | start-tick ladder skipped |
| `recorded.Cmd` | `''` | cmdline ladder skipped |
| `/proc/<pid>/comm` (legacy name guard) | live process named `gopi*` | → **`running`** |

Evidence (anti-proof, run in this worktree and then removed — a live
`gopi`-named process was placed at the claim position on an old-shape row):

```
OLD SHAPE (live gopi-named pid 2259489, empty triple): verdict="running" resumable=false
  note="pid 2259489 is alive and named gopi* (no recorded identity on this row — legacy record)"
  identity_recorded=false
```

and the four tests under that same shape — the reported failure, reproduced
deterministically at the shared precondition:

```
--- FAIL: TestAbnormalChildRetryIsBoundedAndHandsOverOnce  fixture precondition: the dead-holder row must be resumable, got verdict "running"
--- FAIL: TestJudgedChildFailureIsNotRetried                fixture precondition: ... got verdict "running"
--- FAIL: TestRetryCounterSurvivesARestart                  fixture precondition: ... got verdict "running"
--- FAIL: TestRetryRefusesWhenNoRunStarted                  fixture precondition: ... got verdict "running"
```

Note the third `TestRetryRefusesWhenTheRowCannotBeRead` — it takes the
unreadable-row arm and never calls the fixture, which is why the briefing's
`ChildRetry|RetryCounter|RetryRefuses|JudgedChildFailure` selector names four
tests while the helper's precondition bites four (the other three `RetryRefuses`
names plus `RetryCounter`/`JudgedChildFailure` overlap differently; the four
that died are those listed above).

### The fix (minimal, fixture-only)

1. **`retryDeadHolderPID(t)`** — the hermetic dead holder is now an **UNREAPED
   (zombie) child of the test process**. Reaping is what frees a pid, so a
   `SIGKILL`ed child that is never `Wait()`ed keeps its number RESERVED for the
   whole test: no other process can be given it. `tools.PidAlive` — the
   production predicate the verdict itself is built on (`pidAlive`, state `Z`
   = dead) — already reads that shape DEAD, so the fixture borrows the real
   instrument instead of mirroring a `/proc` parse (Rules 13/14). The child is
   `Wait()`ed in `t.Cleanup` (so the pid is eventually released and no zombie
   leaks per test), and the helper waits for the state rather than a duration
   (bounds `retryDeadHolderWait` / `retryDeadHolderPoll`, Rule 1).
2. **`retrySeedDeadHolder`** now stamps through **`tools.TaskStampHolder`** —
   the production writer — instead of raw `UPDATE`. That captures a REAL
   recorded identity (`proc_start_ticks` readable after death), so the row is
   no longer the empty-triple/legacy shape that production itself refuses to
   write, and a pid whose identity is unreadable is refused rather than
   silently degraded into the bug's shape.
3. **The precondition asserts more than "resumable"** (the part that makes the
   fix non-vacuous): it requires `st.IdentityRecorded`. Red-proof — this is not
   cosmetic: with the empty triple restored, the verdict is **still `dead`**
   (a zombie holds the pid), so a resumability-only check passes while the row
   is the legacy shape whose live pid lands on the name guard:

```
GOPI_ANTIPROOF_EMPTY=1: --- FAIL: TestAbnormalChildRetryIsBoundedAndHandsOverOnce
  fixture precondition: the dead-holder row carries NO recorded identity (verdict "dead")
  — the verdict came from the legacy name guard, not the identity the fixture must pin
```

4. **`cmd/agent/cli_tasks_child_retry_fixture_test.go`** (new) —
   `TestRetryDeadHolderFixtureHoldsAReservedPid` pins the two properties so a
   future "simplification" back to pid arithmetic + an empty triple fails here
   rather than in four unrelated tests: (a) the pid still HAS a `/proc` entry
   while `tools.PidAlive` calls it dead (possession + deadness — a REAPED pid is
   also dead, so deadness alone would not prove reservation); (b) the row
   carries a real identity (`RecordedPid == pid`, `RecordedStart > 0`); (c)
   `TaskRunChildResumable` returns `TaskProcessStateDead`. The cmdline axis is
   deliberately NOT asserted: a corpse has no address space left, so
   `/proc/<pid>/cmdline` legitimately reads empty — requiring it would fail on
   the very shape that makes the pid hermetic.

### Which ladder branch each test covers (all five exercised, distinct)

The tests are not walking one path; each reaches a different arm of
`taskRunAbnormalChildRetry` (`cmd/agent/cli_tasks.go:2088`), confirmed from the
arms' own stderr lines:

| test | hatch / arm reached | observed line (abridged) |
|---|---|---|
| `TestAbnormalChildRetryIsBoundedAndHandsOverOnce` | bounded ladder, attempts 1+2+3, then the **cap handover** | `abnormal child exit (attempt 1..3 of 3) — resuming the same session` … `retry ladder exhausted — abnormal-child retry cap reached (3 attempts spent)` (twice = idempotent second pass) |
| `TestRetryCounterSurvivesARestart` | **restart survival**: a second fixture image reads the trail back (2 spent) and reaches the same cap | `(attempt 1..3 of 3)` from the restarted process … `retry ladder exhausted` on `t-retry-restart` |
| `TestJudgedChildFailureIsNotRetried` | the **discriminator**: child closed its own row → judged failure is a result | `no automatic retry started — the child closed its own row (status failed)` |
| `TestRetryRefusesWhenTheRowCannotBeRead` | Rule 15 **unreadable row** refusal (no fixture; never preconditions) | `no automatic retry started — the row's status could not be read: ... no task with task_id="t-retry-missing"` |
| `TestRetryRefusesWhenNoRunStarted` | **spawn produced nothing** (pid 0) — must not consume an attempt | `no automatic retry started — the resume run could not be started: file does not exist` |

So the shared precondition was hiding five distinct branches: three ladder arms
(bounded attempts / cap handover / restart continuity) and two refusal arms
(judged failure, unreadable row) plus the spawn-failure arm — one precondition
covering five tests is only safe once each is shown to take a different exit.

## Not Done (honest)

- **The pid-arithmetic shape survives elsewhere in spirit, not in code.**
  `retryDeadPid()` was the only such helper in this repo (`grep -rn retryDeadPid`
  → no hits after the fix), but the pattern "invent a pid that must be free" is
  not machine-banned: a future test could reintroduce it. The new guard test
  fails only if the *fixture* weakens; it cannot police other files.
- **`modernc.org/sqlite` import dropped** from the retry test (the fixture no
  longer writes raw SQL) — that removal is incidental to the fix, noted for the
  reviewer's benefit rather than claimed as a deliverable.
- The four tests' wall time rose (~22s → ~20-30s range, unchanged in substance:
  the zombie wait is milliseconds; the variance is the ledger fixture cost this
  package already documents).

## Verification

- Build: `CGO_ENABLED=0 go build ./...` → OK (exit 0).
- Vet: `CGO_ENABLED=0 go vet ./cmd/agent/` → OK (exit 0).
- Tests: `go test ./cmd/agent/ -count=1 -run 'ChildRetry|RetryCounter|RetryRefuses|JudgedChildFailure|RetryDeadHolderFixture' -v`

```
--- PASS: TestRetryDeadHolderFixtureHoldsAReservedPid (2.64s)
--- PASS: TestAbnormalChildRetryIsBoundedAndHandsOverOnce (5.14s)
--- PASS: TestJudgedChildFailureIsNotRetried (3.21s)
--- PASS: TestRetryCounterSurvivesARestart (3.13s)
--- PASS: TestRetryRefusesWhenTheRowCannotBeRead (0.87s)
--- PASS: TestRetryRefusesWhenNoRunStarted (2.28s)
PASS
ok  github.com/nick/gopi/cmd/agent  19.674s
```

  Also green with the wiring tests included:
  `go test ./cmd/agent/ -count=1 -run 'ChildRetry|RetryCounter|RetryRefuses|JudgedChildFailure|DeadHolder|hangup|Abnormal'` → `ok ... 29.016s`.
  The bare acceptance selector was run three times green (19.2s, 19.7s, 23.1s).
- Commit: see `git log -1` on `fix/childretry-fixture-dead-holder2`.

## Evidence (self-filed)

- `CGO_ENABLED=0 go build ./...` → `exit=0`.
- `CGO_ENABLED=0 go vet ./cmd/agent/` → `VET-OK`.
- Anti-proof, old shape (live gopi-named pid + empty triple), in this worktree:
  `verdict="running" resumable=false identity_recorded=false note="pid 2259489 is
  alive and named gopi* (no recorded identity on this row — legacy record)"`.
- Anti-proof, four tests under a live-holder fixture: four `--- FAIL ...
  fixture precondition: the dead-holder row must be resumable, got verdict
  "running"` (full block above).
- Red-proof of the identity assertion: `GOPI_ANTIPROOF_EMPTY=1` →
  `fixture precondition: the dead-holder row carries NO recorded identity
  (verdict "dead")` while resumability still held — the check is not vacuous.
- Ladder-arm observation: the five stderr lines quoted in the table (each test
  run alone, `-v`, filtered to `task t-retry*`).
- `git status --porcelain` in the worktree after the commit: see the close-out
  measurement (Rule 30) — the two test files are the entire diff; the anti-proof
  test file `zz_antiproof_tmp_test.go` was deleted, never committed.
