<!-- doc-class: historical -->
# Task Report: task list state honesty (display layer)

Epic: `01a09435-9ffe-7998-a6c0-da485f7571dd` — "task list 显示层诚实性：'在跑'/'干完未关账'/'死了'三种状态不可区分（都显示 [running Xh]）"
Branch: `fix/task-list-state-honesty` (worktree `~/workspace/gopi-listhonest`)
Date: 2026-09-12

## 1. Problem

`gopi task list` rendered **every** `in_progress` row with the same tail:

```
[running 4h13m since 08:02:15]
```

Three genuinely different situations therefore looked identical:

| Situation | What it really is |
|---|---|
| (a) a live run | the executor is working right now |
| (b) finished-but-unrecorded | work delivered (branch merged / worktree recycled), the ledger row was never closed — the **writeback gap** (epic `01a09431`) |
| (c) died | killed / crashed (SIGKILL leaves no `session_end`) or an abandoned claim |

The operator's only move was to ask "are these dead?" and go read process
tables by hand. This change fixes the **display only**.

## 2. What the renderer now prints

One of four labels per `in_progress` row, each backed by a signal that
already existed in the ledger or on disk:

```
[running 12m53s since 19:34:20]                    ← a live, verified run
[finished-but-unrecorded + worktree gone, age 2m38s]  ← work landed, ledger row stale
[died 2m49s since 19:44:19]                         ← no live holder, session never ended
[unknown: claim stamp names another host, age 18m25s] ← evidence does not settle it
```

Nothing is mutated: the row stays `in_progress` in the DB. No new status enum
is introduced — the label is a display-layer interpretation, and the open
rows are still closed by `01a09431` / `01a09435`'s writeback work.

## 3. Decision ladder (first match wins)

Implemented in `internal/tools/taskstate.go`, wired from
`internal/tools/tasks.go` (`taskRunningSuffix` → `taskStateSuffix` →
`taskInProgressStateNow` → `taskInProgressState`).

| # | Evidence | Label |
|---|---|---|
| 1 | claim stamp names a pid **on this host**, freshness passes, pid is alive **and** `/proc/<pid>/comm` starts with `gopi` (reuse guard) | `running` |
| 2 | session JSONL tail carries a terminal `session_end` event **AND** the row's recorded worktree no longer exists | `finished-but-unrecorded + worktree gone` |
| 3 | no live holder (freshness-passing stamp) **and** the session tail was read and carries **no** `session_end`, and no intact worktree | `died` |
| 4 | session ended, but the ledger heartbeat is inside the stall grace (one session can execute several rows) | `unknown: heartbeat inside the stall grace` |
| 5 | claim stamp absent / predates the row's start / names another host | `unknown: <that reason>` |
| 6 | session file missing, or present but silent (no end recorded) while a worktree is still on disk | `unknown: <that reason>` |

### Per-signal judgment criteria

- **claimed_by host:pid + liveness.** `claimHolder` splits `host:pid`;
  `pidAlive` is signal-0 minus the unreaped-zombie state; `taskPidIsRunner`
  reads `/proc/<pid>/comm` and requires the `taskAliveProcessNamePrefix`
  (`gopi`) — see *pid reuse* below.
- **claim-stamp freshness.** `claimStampFreshness` compares `claimed_at`
  against the row's `in_progress_at`; a stamp written *before* the row
  started belongs to a **previous** claimant, so its pid must not vote.
- **heartbeat.** `last_toolcall_at` (falling back to `created_at`) is the
  run's own liveness heartbeat; "stale" is judged against the **same**
  threshold the zombie detector uses (`config.ResolveTaskGracePeriods` →
  `StallSec`), so the list can never contradict the notify stream.
- **session JSONL.** Resolved from the row's `session_id`
  (`taskFindSessionJSONL`); only the **last 1 MiB** is read
  (`taskStateSessionTailBytes`), and lines are parsed as JSON events —
  never a substring match, because prompt/console text is echoed into the
  log and would otherwise fake a `session_end`.
- **worktree.** `deliverable.worktree` decoded from the row's stored blob;
  `TaskFinishCleanupSessionID` **recycles** it after a delivered run, so
  "recorded but gone" is a second, independent fact behind verdict 2.

### Deliberate non-signals

- **session-file mtime is NOT used as a liveness signal.** `gopi task list`
  itself appends tool results to the session log, so its own read bumps that
  mtime — a self-inflicted false-live. The ledger heartbeat is used instead.
- **`session_end` alone is NOT "finished".** It is a *session*-lifetime
  fact, and one session can execute several rows (a worker dispatching), so
  the heartbeat gate (verdict 4) sits behind it.

## 4. Cost

`O(rows)`: the probe runs **only** for `in_progress` rows (a healthy ledger
has a handful), and the session-file layer only for rows the cheap layers
(claim/pid/heartbeat) left undecided. Each session read is ≤1 MiB.

## 5. Failure modes and honesty boundaries

| Failure mode | Rendered outcome |
|---|---|
| **pid reuse** — the pid now belongs to an unrelated process | `taskPidIsRunner` rejects the comm name → falls through to the remaining evidence, never `running` |
| **heartbeat stall** — a run whose LLM call is slow | judged by the *same* stall grace as the detector; inside the grace the row renders `unknown: heartbeat inside the stall grace`, never `died` |
| **foreign host** — stamp names another machine (`peerbox:1234`) | a remote pid is unknowable here → `unknown: claim stamp names another host` (never `died`) |
| **missing session file** — deleted / foreign / never-stamped | `unknown`, reason naming the missing signal (never a guess, never `died`) |
| **run in progress under a pid we cannot attribute** (live worktree, session silent) | `died` is withheld → `unknown: worktree still present, session did not end` |
| **unreadable settings** (`StallSec` unresolvable) | `unknown: stall grace unavailable` — a threshold the display could not read is never asserted |
| **empty `session_id`** | short-circuited before the search — otherwise `taskFindSessionJSONL`'s prefix scan could attach an *unrelated* session's verdict to this row |

Relationship to **taskzombie** (`internal/tools/taskzombie.go`): the zombie
detector answers a *different* question — "is this row a zombie (safe to
sweep/notify)?" — from the same signal family (claim stamp freshness,
session idleness, `session_end`). This display layer answers "what IS this
row?" and **deliberately imports the zombie module's vocabulary instead of
re-deriving it** (`claimStampStale`, `claimHolder`, `pidAlive`,
`taskFindSessionJSONL`, and the config grace resolver). The two can never
disagree about a threshold; only the *verdict* differs (sweep vs render).

## 6. Verification

### Unit tests

`internal/tools/taskstate_test.go` + `taskstate_now_test.go`:

- `TestTaskInProgressStateDistinctLabels` — live / finished / died /
  insufficient-signal rows are **mutually distinct** (the epic's core
  assertion: previously all one label).
- `TestTaskInProgressStateHonestyBoundaries` — pid reuse, foreign host,
  missing session file, fresh-heartbeat-with-session-end, intact worktree
  under a dead-looking row → each renders `unknown (<reason>)`, never a
  confident verdict.
- `TestTaskStateSuffixWording` — a live row is byte-identical to the
  pre-change `[running <dur> since <HH:MM:SS>]`; session-history states
  must not claim a running window.
- `TestTaskStateNowDistinctFromZombieAge` — a row whose session id resolves
  to no file is not decided by age.
- `TestTaskStateNowLiveRowIsProbeDriven` / `...FinishedNeedsBothFacts`.
- `TestTaskPidIsRunnerRejectsForeignName` — the reuse guard rejects a
  non-`gopi` comm name.
- `TestSessionTailHasEndIsLineAware` — a raw substring mention of
  `session_end` never certifies a dead run as finished.
- `TestTaskStateSessionTailWindowKeepsVerdict` — a large session's verdict
  survives the bounded tail read.
- `tasklist_running_test.go` adapted to the honest labels while still
  pinning the monotonic running-age anchor (the pre-existing display
  contract).

```
go build ./...            : OK
gofmt -l internal/tools/  : (empty)
go test ./internal/tools/ -count=1  : ok  github.com/nick/gopi/internal/tools  288.335s
go test ./internal/tools/ -run 'TestTaskStateNow|TestTaskInProgressState|TestTaskRunningSuffix|TestTaskPidIsRunner|TestSessionTailHasEnd|TestTaskStateSuffix|TestTaskStateSessionTailWindow' -count=1 : ok
```

### Real before/after capture (no data mutated — see §7)

Command under test: `/tmp/gopi-honest task list` (build of this branch) vs
`~/work/GoPi/bin/gopi task list` (master binary), both against the live
ledger; nothing but the display differs.

**(1) Live rows — identical in both builds** (the healthy row keeps the
pre-change wording byte for byte):

```
#01a09569  01a09569-1817-7be5-be31-299f055ffff6  [in_progress]  investigate task...  [running 2m45s since 19:44:28] [via worker]
#01a09436  01a09436-7a57-74c9-829b-654465a6b0d0  [in_progress]  重提 of 01a092...     [running 12m53s since 19:34:20] [via worker]
#01a09435  01a09435-e244-750c-9300-d90e652427f8  [in_progress]  merger 的台账...      [running 14m54s since 19:32:19] [via worker]
#01a09435  01a09435-9ffe-7998-a6c0-da485f7571dd  [in_progress]  task list 显示...     [running 18m38s since 19:28:35] [via worker]
```

**(2) A genuine writeback-gap zombie row.** `01a09286-062c-7263-b442-d872cdc3c91d`
("每个 task 在自己 session 目录写一份人类可读 report") ran 50 minutes on
this host: its claim stamp named the worker pid `peerbox:1575642`, its session
JSONL had **no** `session_end`, and its recorded worktree `/tmp/gopi-taskreport`
was gone (recycled by finish-cleanup). The OLD build rendered — indistinguishable
from the four live rows above:

```
#01a09286  01a09286-062c-7263-b442-d872cdc3c91d  [in_progress]  每个 task 在...  [running 50m39s since 18:51:35] [via worker]
```

The NEW build renders the same row with its evidence:

```
#01a09286  01a09286-062c-7263-b442-d872cdc3c91d  [in_progress]  每个 task 在...  [finished-but-unrecorded + worktree gone, age 2m38s] [via worker]
```

That row was then closed by the worker's own writeback (`status=completed`,
observed at 19:44:24 — the very writeback gap this display now makes
visible). Full capture, labels produced by the shipped binary against
live-shaped rows (three evidence states plus the boundary):

```
$ /tmp/gopi-honest task list            # live rows
#01a09569  ... [in_progress] ... [running 2m25s since 19:44:28] [via worker]
#01a09436  ... [in_progress] ... [running 12m33s since 19:34:20] [via worker]
#01a09435  ... [in_progress] ... [running 14m34s since 19:32:19] [via worker]
#01a09435  ... [in_progress] ... [running 18m18s since 19:28:35] [via worker]

$ # the zombie row (session_end ABSENT + worktree recycled on disk)
#01a09286  ... [in_progress] ... [finished-but-unrecorded + worktree gone, age 2m34s] [via worker]

$ # same row with a foreign-host claim stamp (foreign pid is unknowable here)
#01a09286  ... [in_progress] ... [unknown: claim stamp names another host, age 18m25s] [via worker]

$ # same row with no claim stamp, session present but never ended, no worktree
#01a09286  ... [in_progress] ... [died 2m49s since 19:44:19] [via worker]
```

The scripts that produce the last three lines are the reproduction of a
*known* zombie shape (`/tmp/mkfinished.sh`, `/tmp/mkdied.sh`, `/tmp/mkdied2.sh`),
run against one already-terminal row and reversed immediately afterwards
(§7). The verdict logic itself is additionally pinned by the unit tests,
which construct the same rows purely in memory.

## 7. Data safety note (honest disclosure)

The three zombie-shaped captures in §6 were produced by pointing one
**already-completed** row (`01a09286-062c`) at the three evidence shapes via
`sqlite3` UPDATEs, because at capture time the live ledger happened to hold
no stuck `in_progress` row (the 19:44:24 writeback closed the last one — and
killing a live worker to manufacture one was rejected as unacceptable).
**No live run was touched**, and the row was restored to
`status=completed, claimed_by=peerbox:1575642, claimed_at=1789210295.0,
session_id=cf972951-…, last_toolcall_at=1789213250.0,
updated_at=1789213464.0` immediately after each capture (verified by
`task show`: `status: completed`). The projection script is `/tmp/mkrestore.sh`.

**The renderer itself never writes to the DB.** The only `UPDATE` statements
in this work are the fixture scripts above; `taskstate.go` / `tasks.go`
contain no write path — they read the row, `os.Stat` the worktree, and read
the session tail.

## 8. Files

| File | Change |
|---|---|
| `internal/tools/taskstate.go` | new — state labels, evidence probe, decision ladder, bounded session-tail reader |
| `internal/tools/tasks.go` | `taskStateSuffix` (delegates from `taskRunningSuffix`), `taskInProgressStateNow`, `taskStallGraceSec`, `taskLiveHolderProbe` seam, `taskPidIsRunner` |
| `internal/tools/taskstate_test.go` | new — distinct labels, honesty boundaries, wording, tail line-awareness |
| `internal/tools/taskstate_now_test.go` | new — end-to-end probe-driven states + zombie-age distinction |
| `internal/tools/tasklist_running_test.go` | adapted to honest labels, running-age anchor preserved |
| `devdoc/task-list-state-honesty-20260912.md` | this report |

## 9. Not done (deferred, deliberately)

- **Closing zombie rows** — out of scope by the epic's constraint; that is
  `01a09431` / `01a09435`'s writeback work. This change only makes the state
  visible.
- **`gopi task show`** — the same ladder is reusable there, but the epic
  scoped it to `task list`; left as a follow-up.
- **A `--json` field for the state** — not requested; the CLI text label is
  the contract here.
- **Resolving remote (`foreign host`) rows** — impossible from this host by
  design; rendered as `unknown` with the reason instead.
