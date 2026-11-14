<!-- doc-class: historical -->
# Task Report: deterministic TestWorkerBusyRoundResetsIdleStreak (no wall-clock seeding)

Branch: `fix/worker-idle-streak-det` · worktree `~/workspace/gopi-idle-det`
(created off master `aafa7ce`). Commit `b89e9d4`.

## Completed

- **Root cause, reproduced first.** Baseline `go test ./cmd/agent/ -run
  TestWorkerBusyRoundResetsIdleStreak -count=10` on clean master =
  **3/10 FAIL** (not a single unlucky run). Failing transcript shows the
  mechanism exactly as briefed: `worker: claimed t-busy-reset-1` in round 1,
  zero pre-task `(1/3 idle rounds)` lines, `got 1`.
- **Synchronization seam** — new file `cmd/agent/cli_tasks_worker_idle_hook.go`:
  `workerIdleDrainHook` / `workerIdleDrainHooks` / `notifyWorkerIdleDrain(round, streak)`.
  Production cost is one nil-slice range (only tests arm it); no constants or
  literals are introduced, so Rules 1b/13 are untouched.
- **The loop announces its own verdict** — `runTaskWorker` calls
  `notifyWorkerIdleDrain(round, idleStreak)` immediately AFTER printing the
  `"(streak/idleExits idle rounds) — waiting"` line and BEFORE
  `time.Sleep(workerIdleInterval())`. Because the loop invokes observers
  synchronously it is *blocked inside* the observer, so whatever the observer
  commits is committed before the next round's claim scan can run: the race
  window is closed, not shifted.
- **The test seeds from that signal** — `armWorkerIdleDrainHook(t, hook)`
  (helper in the test file, `t.Cleanup` teardown so a stale hook cannot leak
  into a sibling test); the hook submits `t-busy-reset-1` exactly once, at
  `round == 1`. The `time.Sleep(1500ms)` goroutine, its channel and the
  ordering assumption are gone; `seeds != 1` is a `t.Fatalf` so a hook that
  never fires fails loudly instead of silently.
- **Assertion untouched** — `strings.Count(out, "(1/3 idle rounds)") >= 2`,
  the terminal-`failed` row check and the self-exit line check are byte-for-byte
  the same. The busy round is still *proven* to reset the streak.
- **Doc comment corrected** — the `submit t-busy-reset-1 at 1.5s` bullet is now
  "the idle-drain hook fires HERE, synchronously … no wall-clock guess
  involved"; the header note about the hermetic escape is unchanged.

## Not Done

- No product-behavior change was attempted — the epic established the failure
  is pre-existing test seeding, not a worker-loop bug. (The only production
  file touched is the loop's one hook call.)
- The other pre-existing cmd/agent flake (`TestTaskRunSurvivesClosedStdoutPipe`,
  SIGPIPE, tracked as bug `t-1788387314`) was left alone — out of scope.
- Not verified on the two historical trees (b684656 / 3ce88c2): the baseline
  was re-measured on current master, which already carries memory #1586's A/B
  evidence for those two commits.

## Verification

- Baseline (pre-fix, master): `go test ./cmd/agent/ -run
  TestWorkerBusyRoundResetsIdleStreak -count=10` → **3/10 FAIL** (flake
  reproduced; `/tmp/baseline_flake.log`).
- Post-fix targeted, `-count=3 -v` → **3/3 PASS**.
- Post-fix acceptance, `go test ./cmd/agent/ -run
  TestWorkerBusyRoundResetsIdleStreak -count=20 -v` → **20/20 PASS** (verified
  by `grep -c "^--- PASS"` = **20**, job-3; the run also printed one `ok`).
- Full package, `go test ./cmd/agent/ -count=1` → **ok github.com/nick/gopi/cmd/agent
  345.702s** (job-2, exit 0).
- Re-verified on the live branch after this report's first draft: build
  (`CGO_ENABLED=0 go build -o /tmp/gopi-idle-det ./cmd/agent/`) → OK;
  `gofmt -l` on the three touched files → clean; branch `fix/worker-idle-streak-det`
  still at `8a87205` with master untouched at `aafa7ce` (no merger absorption).
- `go vet ./cmd/agent/` → clean.

## Deliverable location (Rule 6d)

- Branch `fix/worker-idle-streak-det` — fix commit **`b89e9d4`** (the only
  code change), then report commits (`8a87205` report, `07b159e` verification
  note, `fd8bf3e` this declaration; the branch tip is always the last
  report-touching commit — read it with `git log --oneline -1
  fix/worker-idle-streak-det`) — in the SHARED repo (`~/work/GoPi` — the
  worktree shares master's object store, so the commits are already in the
  main repo; no push needed by design).
- Recover with: `git show fix/worker-idle-streak-det:devdoc/worker-idle-streak-deterministic-test.md`
  (or the same path from a later master once merged). The commit is always
  found by `git log --all --oneline --grep "seed the busy round"`.
- Branch-survival verified at hand-off:
  `git worktree list | grep gopi-idle-det` →
  `/home/user/workspace/gopi-idle-det 07b159e [fix/worker-idle-streak-det]`,
  and master still at `aafa7ce` — the branch was NOT absorbed by the merger
  during this session.
