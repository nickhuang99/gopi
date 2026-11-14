<!-- doc-class: historical -->
# Task Report: `gopi task show` waits out the ledger write lock on an existing row

<!-- WORKER-OWNERSHIP: OPERATOR-ONLY — devdoc/worker-ownership.md -->

Task `01a133ee-2d09-72f8-9657-e0956d7a0d5b` · branch `task/01a133ee2d0972f89657e0956d7a0d5b`

## Completed

**The reported failure mode is reproduced, root-caused and fixed — with the
reported *mechanism* corrected.**

- **Root cause.** `gopi task show` reads one row and writes nothing, but it
  opened the ledger through `openTasksDB`, which runs the whole tasks DDL batch
  on EVERY open (`ensureTasksSchema`: `CREATE TABLE IF NOT EXISTS`, the
  P2/proc-identity `ALTER`s, then `migrateTasksFTS`'s index-shape probe) over a
  `_txlock=immediate` DSN — i.e. `BEGIN IMMEDIATE`, the ledger's **write lock**,
  on every invocation. On a live host whose ledger carries the resident daemons
  (`gopi worker` ×2 + `gopi merge --every 2m` were running here), that lock is
  regularly held, so the read verb waited out `dbBusyTimeoutMSec` before its
  first `SELECT`. Under an operator's `timeout 15`/`timeout 40` wrapper that is
  the reported "hangs until timeout". It is a **~17s stall, not an unbounded
  hang**, and it is not confined to one row.
- **Fix** (minimal, one seam): `openTasksDBReadOnly` — the ledger opened through
  production's own read-only DSN contract (`LedgerReadOnlyDSN`: `mode=ro` +
  `query_only` + the same busy timeout), used by `TaskShow` and by the
  deliverable read `taskRowDeliverable` (which rides inside `TaskShow`'s
  advisory ground line). A verb that may not write no longer takes the write
  lock, and no longer runs schema DDL.
- **Regression guard**: `internal/tools/task_show_readonly_lock_test.go` —
  `TaskShow` must return the digest quickly while another connection holds the
  write lock (red proof below), plus a contract test that the handle it reads
  through refuses a write.

### The evidence, and the correction to the report's second half

| repro from the report | verdict | deciding evidence |
|---|---|---|
| `task show 01a133a585dd76d5a667927577aa56a3` → exit 1 | **NOT a defect** — corrected by row `01a133ee-bdf4` before this run: the operand is a non-canonical uuid spelling and the tool names the canonical one | raw output in the reporter's `artifacts/` + row `01a133ee-bdf4`'s own digest |
| `task show 01a133e7-…` → exit 124 (timeout, 0 bytes) | **reproduced as a lock stall, then fixed** | `lock-ab.txt`: unlocked 57/219/52ms vs locked 17226/17126/17130ms |

`task list` listing the row while `show` "could not read" it is the SAME stall
seen through a 15s kill: `task list` reads no run directory, so its own read is
short enough to finish inside that window (measured: `list` 1.5s, `show` on the
same live ledger 1.7s — the row was never unreadable, it was slow).

Mechanism, pinned with strace on the live shape (not inferred): the child blocks
on `fcntl(F_SETLK, F_WRLCK, …) = -1 EAGAIN` on the ledger while the holder's
`BEGIN IMMEDIATE` stands, and the wait is the SQLite busy handler's `nanosleep`
loop — no infinite retry, no goroutine deadlock (`lock-mechanism.txt`).

## Files

- `internal/tools/tasks.go` — `openTasksDBReadOnly` (new) + `TaskShow` wired to it.
- `internal/tools/ledger_dsn.go` — `ledgerReadQueryOnlyPragma` (one named
  spelling both the DSN renderer and the handle-level re-assertion share).
- `internal/tools/task_deliverable_worktree.go` — `taskRowDeliverable` reads
  through the read-only opener; its lenient contract (`("", "", "")` on any
  opener error) is unchanged.
- `internal/tools/task_show_readonly_lock_test.go` — the regression guard.

## Verification

- **Build**: `CGO_ENABLED=0 go build ./...` → exit 0;
  `CGO_ENABLED=0 go vet ./internal/tools/` → exit 0.
- **Tests**: `CGO_ENABLED=0 go build ./...` → exit 0;
  `CGO_ENABLED=0 go vet ./internal/tools/` → exit 0;
  `CGO_ENABLED=0 go test ./internal/tools/ -run 'TaskShow|TestTaskRowDeliverable|GroundAdvisory' -count=1` → `ok github.com/nick/gopi/internal/tools 4.754s`;
  `CGO_ENABLED=0 go test ./cmd/agent/ -run 'TestLedgerOpenersUseProductionDSNContract|TestTaskShow'` → reading below (the package-wide `make test-budget PKGS='./internal/tools'` job was killed at 490s on budget — see Not Done).
- **Ran** (Rule 31 — the changed surface, real binary, real ledger, real args):

  ```
  $ cd /home/user/workspace/gopi-01a133ee2d0972f89657e0956d7a0d5b
  $ CGO_ENABLED=0 go build -o bin/gopi ./cmd/agent/          # the FIXED binary
  $ ./bin/gopi task show 01a133e7-0a5e-7709-8fc2-ce73b4782198   # live ~/.gopi/memory.db
  task_id:        01a133e7-0a5e-7709-8fc2-ce73b4782198
  status:         completed
  run_dir:        /home/user/.gopi/sessions/01a133e7-0a5e-7709-8fc2-ce73b4782198
  ...
  EXIT=0
  ```
  (141 lines, full digest: `artifacts/acceptance-live.txt`)

- **BEFORE/AFTER on the real surface**, same command, same holder, one ledger
  copy, one second apart (`artifacts/lock-after.txt`):

  ```
  rep1 BEFORE (/usr/local/bin/gopi, pre-fix): 13310ms rc=0
  rep1 AFTER  (./bin/gopi, fixed):               47ms rc=0
  rep2 BEFORE (/usr/local/bin/gopi, pre-fix): 13257ms rc=0
  rep2 AFTER  (./bin/gopi, fixed):              217ms rc=0
  ```

- **Red proof** — `TaskShow` reverted to the write opener, same test:

  ```
  --- FAIL: TestTaskShowDoesNotWaitOnAWriterHold (61.43s)
  ```
  Restore verified by blob, not an index-relative diff:
  `test "$(git hash-object internal/tools/tasks.go)" = "$(git hash-object <the file saved before the mutation>)"` → equal.

- **Result**: the read verb no longer queues on a writer; the digest is
  identical to the pre-fix rendering for the rows sampled (a row's digest is
  rendered by the same readers either way — the opener change touches which lock
  is taken, not which bytes are read).

## Evidence (self-filed)

- `~/.gopi/sessions/01a133ee-2d09-72f8-9657-e0956d7a0d5b/artifacts/lock-ab.txt`
  — interleaved A/B, live ledger copy, three reps (unlocked 57/219/52ms; locked
  17226/17126/17130ms).
- `.../artifacts/lock-after.txt` — pre-fix vs fixed binary against the same
  holder (13.3s → 47ms).
- `.../artifacts/acceptance-live.txt` — the fixed binary on the LIVE ledger,
  full digest + `EXIT=0`.
- `.../artifacts/lock-mechanism.txt` — strace of the blocked run
  (`F_WRLCK … EAGAIN` + busy-handler `nanosleep`), and the ledger ops.
- `git status --porcelain` in the worktree: empty.

## Not Done (honest)

- **The `-wal`/`-shm` deletion on `--db` ledgers is untouched, not swept under.**
  `openTasksDB` runs DDL on every open and `enableWAL` was measured here removing
  `-wal`/`-shm` from a `--db` ledger copy. That is a *separate* write path with
  its own callers; this task narrowed its scope to the read verb the report
  names. Worth its own row.
- **`task list` and the other ledger readers were left on the write opener.**
  They are not the reported surface, `list` measured 1.5s (it does less work per
  row), and converting every reader is a behavioural change to surfaces nobody
  measured a complaint about. The ones the same **`show` verb** reaches were
  converted, because they ride the same command.
- **A ledger that does not exist yet keeps the old behaviour.** The read-only
  opener falls back to the creating opener for that one arm (SQLite refuses
  `mode=ro` on an absent path, so a fresh profile's first `gopi task show` would
  otherwise error out of the open instead of answering "no task with task_id" —
  measured: `TestTaskShowNotFound` and `TestTaskShowDoesNotOfferASpellingRepair…`
  reddened exactly so). The fallback cannot reintroduce the defect: the write
  lock it may take is a lock on a file that does not exist, so no other process
  can hold it.
- **The full `./internal/tools` package was NOT run to completion.** Its wall
  time exceeds what remained of this session's budget (a `make test-budget
  PKGS='./internal/tools'` job was still running at 490s and was killed). What
  WAS run and observed: `go build ./...` exit 0, `go vet ./internal/tools/`
  exit 0, and `-run 'TaskShow|TaskRowDeliverable|GroundAdvisory'` → ok
  (4.754s), plus a wider `-run 'TaskShow|GroundAdvisory|DeliverableWorktree|
  RunGround|Reclaim'` → 59s, whose only two failures were the fresh-profile
  cases above (now green). The package-wide line stays unreported rather than
  claimed, which is why the earlier `make test-budget` invocation is named as
  killed and not as a result.
- **No host-level workaround was used** (no `PRAGMA` in the operator's shell, no
  retry wrapper, no lowered `dbBusyTimeoutMSec`): Rule 35 — the fix is in the
  reader, and the knob that produced the 17s wait is still 15s for the writers
  that legitimately queue.
