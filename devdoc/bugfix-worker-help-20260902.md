<!-- doc-class: historical -->
# Bugfix Report: `gopi worker -h` runs the worker loop (P0-1)

> **WORKER-OWNERSHIP: OPERATOR-ONLY — devdoc/worker-ownership.md** — the operator owns this; an agent must never run it (submit the work instead: `gopi task submit`).

Date: 2026-09-02 · Branch: `feat/fix-worker-help` · Source review: `devdoc/review-cli-task-sync.md` P0-1

## The bug

`cmd/agent/main.go` `isWorkerSubcommand` checked only `args[0] == "worker"` and
called `runTaskWorker()` — no `-h`/`--help` scan, no flag parsing whatsoever.
`gopi worker -h` therefore STARTED A REAL WORKER: it claimed and executed a
live pending task. `-h` must NEVER have side-effects.

## Incident chain (both real, both this bug)

1. **2026-09-02 07:37** — the task-sync review (Bond) ran `gopi worker -h` to
   capture its usage head; it claimed **t-1788305178** (sibling
   config/options review task), spawned a nested gopi; the bash timeout killed
   the loop → row left `failed` with result `signal: broken pipe — elapsed=18s`.
2. **2026-09-02 07:51** — PA ran `gopi worker -h` the same way; it claimed
   **t-1788305179** (memory family review) → failed 6s "broken pipe".

Both failures *looked* like "task-run wrapper broken pipe" but were actually
`worker -h` side-effects. This is the class of bug the epic's isXSubcommand
registry review hunts: every other subcommand handles `-h` correctly; worker
was the sole offender.

## The fix

`isWorkerSubcommand` now scans `args[1:]` for `-h`/`--help` BEFORE running the
loop, prints `workerUsageHead` and exits 0 — claiming nothing. Added the
`workerUsageHead` named constant (Rule 1) describing:

- the loop contract (claim → run → must-succeed push → db-sync → next;
  exit on drained queue, exit 0);
- `GOPI_WORKER_MAX_ROUNDS` (cap, default 0 = unlimited, malformed → default);
- the pa-queue exclusion (the fixed anti-misclaim guard shared with serve's
  `--skip-tag`).

## Regression test

`TestWorkerHelpDoesNotRun` (cmd/agent/cli_tasks_worker_test.go): queues a
PENDING task in a sandboxed ledger, runs the REAL built binary with
`gopi worker -h` and `gopi worker --help`, asserts:

1. exit 0;
2. the usage head is printed (loop contract + GOPI_WORKER_MAX_ROUNDS +
   pa-queue);
3. **the queued task is still pending — nothing claimed** (the P0-1
   side-effect assertion);
4. no `worker: loop start` banner (the loop never started).

Plus `TestWorkerUsageHeadCoversLoopContract` pinning the help's contract text.

## Verification

- Build: `go build ./...` — OK
- Tests:
  - `go test ./cmd/agent/ -run 'TestWorkerHelpDoesNotRun|TestWorkerUsageHeadCoversLoopContract'` — PASS
  - `go test ./cmd/agent/` — PASS (99.7s, full package)
  - `go test ./internal/tools/` — PASS
- Live check: `gopi worker -h` (sandboxed GOPI_MEMORY_DB) prints
  `workerUsageHead` and exits 0 with zero claims.
- Commit: see `git log` on `feat/fix-worker-help` (committed to main repo via
  worktree = shared object store).
