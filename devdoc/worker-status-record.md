<!-- doc-class: historical -->
<!-- WORKER-OWNERSHIP: OPERATOR-ONLY — devdoc/worker-ownership.md -->

# Task Report: durable worker liveness/state record + `gopi worker --status`

Epic `worker-status-record` — branch `feat/worker-status-record`, worktree
`/home/user/workspace/gopi-worker-status`, based on master `e6170cee`.
Commit: **`7ceb6ed3`**.

## Completed

### 1. The record — schema + writer
`cmd/agent/cli_worker_status.go` defines `workerStateRecord`, one JSON file per
pid at `<control-dir>/worker/worker-state-<pid>`:

| field | meaning |
|---|---|
| `pid`, `started`, `version` | who, since when, which binary |
| `rounds`, `state` (idle\|busy), `updated` | progress, the ADVISORY word, the write stamp |
| `last_round_start`, `last_round_end` | **the authority** for the idle/busy verdict |
| `task_id`, `session_id` | what the worker is running right now |

`workerStateWriter.flush` is the single publish point and stamps `updated`
itself, so no future boundary can forget it. The claim boundary in
`cli_tasks_worker.go` publishes the task + session via
`workerCtx.setRoundTask(target, sessionID)`.

### 2. The reader — `workerStatusEnumerate`
**State files first**: the NAME must parse as `worker-state-<pid>` (stop/force
requests and `.tmp` installs are skipped — not this reader's business); then
`tools.PidAlive` (signal 0 + `/proc/<pid>/stat` `Z` check) decides — alive →
worker; dead/zombie → **stale** (reported, never a ghost entry); unreadable body
→ **unreadable** (counted, never a worker).

**Then the argv fallback** (`fallbackWorkerPIDs`) for workers started without a
record: scan the procfs root for `comm == gopi` AND `argv[1] == worker` AND
**detached** (`pid == session id`). The detachment test fixes a real defect
found on the live binary: without it the operator's own `gopi worker --status`
matched the shape and listed itself.

An unreadable state DIR is exit 1 with the reason on stderr (the fallback half
still ran); a MISSING dir is an empty enumeration, not an error.

### 3. The verdict — timestamps, never the advisory word
`Busy()` = an unclosed round still inside `workerStatusActivityWindow` (6h).
This closes two honesty holes: a stale `state:"busy"` field can never override
closed timestamps, and a worker SIGKILLed mid-round (no end stamp possible)
reads **idle** past the window instead of ghosting as busy forever.

### 4. `--status` (`main.go:122` → `workerStatusReport()`)
One line per live worker, plus explicit zero cases:

```
gopi worker pid 3166834: busy — 01a099ae-… [state …/worker-state-3166834, updated 2026-09-13T07:32:51+00:00]
gopi worker pid 2140436: idle — gopi-worker worker (no state file — identified by argv; …)
gopi worker: 1 stale state file(s): …/worker-state-3166834 (pid 3166834)
gopi worker: no workers running
```

## Verification

- **Build**: `CGO_ENABLED=0 go build ./...` → OK (`BUILD_OK`).
- **Unit tests**: `go test ./cmd/agent/ -run TestWorkerStatus -v` → **12/12 PASS**

  | test | case |
  |---|---|
  | AliveWorkerIsListedAsIdle | alive pid from a state file → `idle` |
  | ZombiePidIsNotAlive | zombie/dead pid rejected by the shared predicate |
  | DeadPidStateFileIsStaleNeverAWorker | dead pid → stale, never a worker |
  | UnreadableAndUnparsableAreCounted | garbage body counted; non-state filename ignored |
  | EmptyStateDirReportsNoWorkers | zero case |
  | MissingStateDirIsNotAnError | missing dir = empty, not error |
  | BusyNeedsAnOpenRecentRound | busy / closed / killed-past-window |
  | AdvisoryStateWordNeverOverridesTimestamps | stale `busy` word loses |
  | BusyActivityNamesTaskAndSession | names task+session; unknown marker (Rule 15) |
  | FallbackFindsADetachedWorkerWithoutAStateFile | detached accepted; foreground / other verb / non-gopi comm rejected |
  | FallbackLiveWorkerIsFound | real spawned child: live ⇒ alive, wrong shape ⇒ not a worker |
  | RecordRoundTripsEveryField | schema round trip |

- **Real binary acceptance** (`GOPI_HOME=…/.wstatus-demo`):
  1. `gopi worker --daemon` → `worker daemon started: pid 3173268`; `--status` →
     `pid 3173268: idle — drained queue [state …]`. (Needs
     `GOPI_ALLOW_AGENT_SPAWN=1` — the operator guard forbids an agent spawning.)
  2. `gopi task submit "wstatus acceptance probe" --payload "echo probe; sleep 25"`
     → `--status` MID-ROUND → `pid 3166834: busy — 01a099ae-de34-7380-804a-06d58c2289d0`
     — the live pid and the task it was running.
  3. `kill -9 3166834` mid-round → `--status` → **no line for that pid**, plus
     `gopi worker: 1 stale state file(s): …worker-state-3166834 (pid 3166834)`,
     `EXIT=0`. No ghost entry; the SIGKILL-left `busy` record never rendered busy.
- **Standards gates**: `go test ./internal/standards/` → **OK**. Two NEW
  violations surfaced in files this change touches and were FIXED, not baselined:
  Rule 18 (my own new `else` after a terminating `if` in `cli_tasks_worker.go` —
  restructured into guard clauses) and Rule 16 (the `main.go:932` baseline entry
  re-keyed to `:940`; the site is pre-existing legacy, verified by
  `git show master:cmd/agent/main.go | sed -n 932p`).

## Not Done / honest notes

- **`PidAlive` has no test seam** (`syscall.Kill` + hardcoded `/proc`), so the
  fallback test drives the real predicate against a real spawned child; the
  shape predicate is tested separately over a fixture proc root (`GOPI_PROC_ROOT`).
- The record is a status artifact, **not a lock** — liveness authority remains
  `PidAlive`, unchanged.
- No new tool added (Rule 3): `--status` is a CLI verb only.
- A *live* worker that hangs past the 6h window would also read `idle`; the
  stamps are shown so the operator can still tell.
- `gofmt -l` reports `internal/tools/task_merge.go` — pre-existing on master,
  untouched by this branch (confirmed by `git status` being clean for that file).

## Deliverable declaration (Rule 6d)

- **Branch**: `feat/worker-status-record` (commit `7ceb6ed3`), in the shared
  repo — the worktree shares the main repo's refs, so the commit is already
  there; nothing was pushed or deleted.
- **Recovery**: `git show feat/worker-status-record:devdoc/worker-status-record.md`
- Worktree `/home/user/workspace/gopi-worker-status` left in place for the PA.
