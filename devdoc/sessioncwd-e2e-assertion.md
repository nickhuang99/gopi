<!-- doc-class: historical -->
# Task Report: `TestTaskRunStandsInTheSessionDirAndHandsTheWorkdirToTheChild` — root cause + fix

> **WORKER-OWNERSHIP: OPERATOR-ONLY — devdoc/worker-ownership.md** — the operator owns this; an agent must never run it (submit the work instead: `gopi task submit`).

Branch `fix/sessioncwd-e2e-assertion` (worktree `~/workspace/gopi-sessioncwd-e2e`), based on
master `249d7860` (the epic's `8380ce9c` is an ancestor).

## Root cause: (b) — the round works; its move line was diverted by the output-log attach

Not a claim→run regression. The failure-path records prove the round reached
`workerRound` → `taskWorkdirEnter` → the child, and that the move line exists:

`<gopiHome>/.gopi/sessions/t-session-cwd-e2e/output.log` (probe, master build):

```
# gopi worker output — task t-session-cwd-e2e session  pid 1188641 (...)
worker: output attributed to task t-session-cwd-e2e → .../sessions/t-session-cwd-e2e/output.log
gopi task: workdir → .../sessions/t-session-cwd-e2e (started in .../003/repo)
worker: resumed claimed task t-session-cwd-e2e
claimed: t-session-cwd-e2e
gopi task: workdir → .../sessions/t-session-cwd-e2e (started in .../003/repo)
...
task t-session-cwd-e2e completed (elapsed 0s)
```

Mechanism (5-why): the e2e sets `GOPI_WORKER_LOG_ROOT` → at CLAIM time
`workerAttachOutputLog` re-points `os.Stdout`/`os.Stderr` into
`<session-dir>/output.log` (epic worker-log-session-attribution, by design) → the
round's `fmt.Println(wdLine)` after the chdir therefore lands ONLY in that log →
the launcher's pipe (what `testutil.RunGopi` captures) never sees it → the
assertion watched a stream the line cannot reach. The epic's REAL requirement
(worker stands in the session dir; its log is written there; the child keeps the
operator's workdir) is fully satisfied — see acceptance 3/4 of the e2e, both of
which ALSO fail after the corrected move line only if the requirement breaks.

### Causal A/B probe (`ZZ_PROBE_LOG_ROOT=off` on the same master binary)

Attribution off (no root configured — the documented "pipe-only" mode):

```
worker: claimed t-session-cwd-e2e
gopi task: workdir → .../sessions/t-session-cwd-e2e (started in .../003/repo)   ← launcher stream
```

The line reappears on the launcher stream exactly when the attach is skipped:
the diversion is the cause, not a missing code path.

## Fix (both halves, minimal)

1. **Production — the round's record renders through ONE writer** (`cmd/agent/cli_tasks_worker_output.go`):
   `workerRoundOut()` (the task's output log while attached, `os.Stdout` otherwise)
   and `workerRoundAnnounce()`. `workerRoundAnnounce` additionally mirrors the
   round's line to the LAUNCHER's stdout **on the `--once` face** — that face is
   the drain-once deploy seam (`scripts/gopi-worker-up.sh`), whose whole output
   the caller reads; the resident face keeps the documented split (its launcher
   has the log path, and every resident round would otherwise double). The
   round's claim line, workdir line and own-release line now go through it.
2. **Test — the assertion names the round's actual record** (`cmd/agent/task_session_cwd_e2e_test.go`):
   the OLD assertion was `strings.Contains(stdout+stderr, sessionDir)` — a bare
   directory substring that is green-blind (the same path appears in the child's
   briefing/raw log, so it stayed green while the move line went nowhere). It now
   asserts the round's move line `taskWorkdirMovePrefix + sessionDir` on the
   launcher's stream AND inside the round's `output.log` (the round's own record
   must carry it — the epic's requirement is not traded for the mirror).
3. `taskWorkdirLineFmt` / `taskWorkdirMovePrefix` in production (Rule 1: the
   message shape is not typed inline at the call sites or duplicated in the test).
4. New focused pin `TestWorkerRoundAnnounceMirrorsToTheLauncherOnTheOnceFace`
   (`cmd/agent/cli_tasks_worker_output_test.go`): once → announced; resident →
   not duplicated; the task log carries the record on BOTH faces.

## Red-proof (mutation, real binary rebuilt each time)

| mutation | result |
|---|---|
| pre-fix shape (`fmt.Fprint(workerRoundOut(), wdLine+"\n")` — log only, no launcher announce) | `--- FAIL: TestTaskRunStandsInTheSessionDirAndHandsTheWorkdirToTheChild` — "must report \"gopi task: workdir → …\" on the launcher's stream", stdout = loop start / claimed / own-release / drained |
| move line dropped on BOTH streams | probe `output.log` no longer contains the move line (ditto `ZZ_PROBE_LOG_ROOT=off` transcript) → the test's log assertion is the second, independent red arm |
| `workerRoundAnnounce` mirrors unconditionally (unset the `--once` guard) | `FAIL … resident_face_does_not_duplicate` |

So the OLD assertion fails on master and the NEW assertions fail if either half
of the contract is removed — the correction is not a weakening.

## Verification

- `go test -count=1 -run TestTaskRunStandsInTheSessionDirAndHandsTheWorkdirToTheChild ./cmd/agent/` → PASS (1.99s), after `CGO_ENABLED=0 go build -o bin/gopi ./cmd/agent/` (the e2e's own freshness guard enforces the rebuild).
- Neighbour suite (one run): `TestFrontRunConsoleOutputLandsInItsSessionDir`, `TestWorkerAttachOutputLogLandsInSessionDir`, `TestWorkerAttachOutputLogRefusesWithoutASessionDir`, `TestWorkerOutputLogSecondRunKeepsTheFirst`, `TestWorkerUnknownArgument*`, `TestWorkerBusyRoundResetsIdleStreak`, `TestCLIPositionalSubmit*` → all PASS.
- `go test ./internal/standards/ -count=1` → ok (no baseline re-key needed: no `cmd/agent/main.go` line shifted).
- Whole package `go test ./cmd/agent/ -count=1` → see "Package transcript" below.

## Package transcript

```
cd ~/workspace/gopi-sessioncwd-e2e
CGO_ENABLED=0 go build -o bin/gopi ./cmd/agent/   → BUILD_OK
GOPI_BIN=$PWD/bin/gopi go test ./cmd/agent/ -count=1
                                                  → ok  github.com/nick/gopi/cmd/agent  324.062s
GOPI_BIN=$PWD/bin/gopi go test ./cmd/agent/ -count=1 -v \
  -run TestTaskRunStandsInTheSessionDirAndHandsTheWorkdirToTheChild
  --- PASS: TestTaskRunStandsInTheSessionDirAndHandsTheWorkdirToTheChild (2.41s)
  → ok  github.com/nick/gopi/cmd/agent  2.586s
go test ./internal/standards/ -count=1            → ok  github.com/nick/gopi/internal/standards  19.245s
```

An intermediate package run was RED for a reason worth recording: `bin/gopi`
mtime (04:30:19) older than `cli_tasks_worker_output.go` mtime (04:30:26) — the
e2e's own freshness guard refused the run (a doc-comment edit landed after the
build). Rebuilt, re-ran: green, as above. That guard is exactly what keeps this
verdict falsifiable.

## Not Done / found on the way (not in scope)

- The child's `raw.log` is written beside the session JSONL while the child
  prints to the session-dir log — consistent with the epic, not touched.
- `[task-notify] task run closeout … no live claim evidence (claimed_by="peerbox:<pid>")`
  on every worker round: the worker claims the row and runs it in-process, so no
  external "claim evidence" exists; left for the stale sweep. Pre-existing and
  unrelated to this red (the row IS marked completed in the same log), so it is
  reported here rather than fixed.
