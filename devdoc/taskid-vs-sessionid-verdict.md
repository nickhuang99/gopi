<!-- doc-class: normative -->
# Task Report: task_id vs session_id — what each id IS, and whether they can be consolidated

Task: `01a0bb5b-af7c-75ad-9783-c882ae775001` ("taskid vs sessionid"), branch
`docs/taskid-vs-sessionid` (worktree `/home/user/workspace/gopi-taskid-vs-sessionid`),
off `master` @ `e3195ca6`. Investigation only — the epic asks to understand the
logic and evaluate the risk, not to change code.

**Short answer: keep both. They are two different entities — `task_id` = the
ledger ROW (a work order + its container), `session_id` = the SESSION that
submitted or executed it (a run) — and the "one id" the operator remembers is
the accidental legacy shape, not the design. The confusion in the briefing is
real but its cause is narrower than "two UUIDs": the FAILED-hint comment and one
directory convention say "session id" where the code actually takes the task id,
and `task show` prints `session_id` with no statement of what it is.**

## Completed

### 1. What each id IS (schema + the one minting site each)

| column | entity | minted by | constraint |
|---|---|---|---|
| `task_id` | the ledger **row** — the work order and its container (`sessions/<task_id>/`) | `internal/tools/tasks.go:1294 taskIDGenerate` → `uuid.NewV7()` with a collision-retry loop | `TEXT NOT NULL UNIQUE` (the ONLY unique key on the table) |
| `session_id` | the **session** that submitted (`llm` source) or executed (`worker`/`resume`) the row | `internal/agent/session.go:150 NewSessionLogger` → `newUUIDv4()` (`session.go:1148`) | no constraint at all — 239 sessions span 689 rows |

The session **directory** is named by `task_id` and the **log file inside** by
the run's own uuid:
`internal/agent/session.go:97` `sessionDirForID(sessionID, taskID)` returns
`sessions/<task_id>` whenever a task is known (else `sessions/<sessionID>` for a
bare chat/briefing session), and `internal/agent/session.go:122
SessionLogFileName` names the file `<sessionID>.jsonl`.

Live proof of the layout, from **this run** (env + filesystem + binary):

```
GOPI_TASK_RUN_ID=01a0bb5b-af7c-75ad-9783-c882ae775001
GOPI_EXECUTOR_SESSION_ID=b3e8414b-4b1f-4093-be3b-1975a85e8cb2
ls ~/.gopi/sessions/01a0bb5b-af7c-…/ →
  b3e8414b-….jsonl  b3e8414b-….raw.log  output.log  metadata.json  attachments/  task-report.md
```

`session_start` of that log carries BOTH:
`{"sessionId":"b3e8414b-…","taskId":"01a0bb5b-af7c-…","type":"session_start"}`.

### 2. Producer map — 10 sites, each measured

| # | producer | writes | evidence |
|---|---|---|---|
| 1 | `gopi task submit` / `TaskSubmit` (`tasks.go:4415`) | a fresh uuid v7 row, `session_id=''` (no session exists yet) | 3 pending rows, all `session_id` empty |
| 2 | submit container | `sessions/<task_id>/{metadata.json,attachments/}` created at submit, not at run | verified on those same 3 pending rows (and confirmed by the `metadata.json` of this run, written at submit time 04:29 while the log file appeared later) |
| 3 | claim (`TaskNext`/`TaskClaimExplicit`) | **no session write** | `tasks.go:4900` is the only session read on the claim path, and it is the anti-self-reference filter `session_id != excludeSession` |
| 4 | `task run` wrapper child | stamps the child's session uuid | `cmd/agent/cli_tasks.go:1943` → `tools.TaskBackfillSession` (`tasks.go:3560`), polled in from the child's `session_start` (`waitChildSessionJSONL`, `cli_tasks.go:2328`) |
| 5 | executor child session logger | dir = **wrapper row's task_id**, file = its own run uuid | `TaskWrapperEnv = "GOPI_TASK_WRAPPER_ID"` (`tasks.go:7219`); `session.go:97/122` |
| 6 | bare prompt / chat auto-registration (`taskregister.go:233`) | `session_id` = the session's own uuid; `task_id` = a **separate** minted uuid v7 | the comment at `taskregister.go:238-241` states it explicitly ("task_id and session_id are different entities"); landed with `bd04f250` (2026-09-05, "task-create-unified") |
| 7 | `llm` `task_submit` | `session_id` = the **submitter's** session | `llm` rows: 89% carry a session id (highest of all sources) |
| 8 | `gopi resume` | the row keeps its id; a **NEW** run uuid is minted and recorded as a `resumed:<uuid>` tag | `cmd/agent/cli_resume.go:401` → `TaskTagResumed` (`tasks.go:5218`) |
| 9 | sync/import (`memory_sync_task.go:825,2055,2205`) | copies both columns; row identity is `WHERE task_id = ?` (`:1314,1339,1922,2109`) | — |
| 10 | legacy rows | `t-<unix>` **or** the session uuid as task_id | see §5 |

### 3. Consumer map — and why the other id cannot serve

| consumer | id | why |
|---|---|---|
| claim the next pending row | `task_id` | a session does not exist before a run: **3/3 pending rows have `session_id=''`** |
| `sessions/<task_id>/` container, attachments, `output.log`, `task-report.md` | `task_id` | it must exist at SUBMIT time; the code says so in a comment (`worker_output.go:159-183`: "THE KEY IS THE TASK ID, never the session id … STABILITY" — the session id is backfilled mid-run, so a session-keyed directory would MOVE under the running worker) |
| `session_log:` / `raw_log:` lines of `task show` | `session_id` (resolved through the row) | these name a specific RUN's log — `tasks.go:2284 taskSessionLogPath`, `:2368 taskRawLogPathFromSession` |
| resume | `task_id` as the target, `session_id` as the checkpoint | `taskproc.go:855 TaskResumeTarget` reads the row's session id to find the raw log; `cli_resume.go:269` sets `taskID` from `args[0]` — **`gopi resume <task-id>` is the documented task-mode target**, and the `<session-id>` form is the bare-chat case |
| heartbeat / staleness / branch attribution / reclaim / sync | `task_id` | a row-level fact; `branchAttributionRowWorktree(rows, taskID)` and `task_for_branch.go:148` key on `task_id` (session id is carried along as a description, `:58-61`) |
| write_task_area carry-forward across a resume | `session_id` | the conclusions were stored under the ORIGINAL run's uuid (`cli_resume.go:387`), so the row's session id is the only pointer to them |
| chat rows + `session_conclusions` | `session_id` — and these rows are **not tasks** | `chat.go` (39 refs), `writetaskarea.go` (12) — sessions exist with no task at all |

### 4. Measured reality of the current split (live ledger, this host, 2,096 rows)

```
rows=2096   t- rows=542   uuid-v7 rows=1236   task_id==session_id: 184   no session_id: 668
UNIQUE(task_id) violations: 0        distinct session_id: 978
239 sessions serve >1 row — together 689 rows; max fanout 16 (49d965a8: 15 llm + 1 worker rows)
sessions/ dirs = 74,943       dirs holding >1 run log = 128
```

Row→run resolution over the newest 200 rows (each row's own dir, matched by the
`sessionId` **inside** the run's `session_start`):

```
rows with a dir log checked = 113
  ledger session_id == the run's own sessionId .......... 104
  ledger session_id != the run's own sessionId ..........   9   (the legacy t- rows of §5)
  the dir holds a log belonging to ANOTHER task ........   0
rows with no sessions/<task_id>/ dir at all .............  87   (legacy / merger / cancel-probe rows)
```

So the current design is materially better than a dir-walk guess: for every
non-legacy row, `task show <id>` resolves the exact log of the run that
executed it. The 9 mismatches are the `t-` rows whose dir kept the multi-second
`t-1788905960…-3` ids polled by a different naming rule.

### 5. The "they used to be the same" memory — confirmed, and it is 184 rows of accident

Between 2026-08-28 and 2026-09-02 (the same window `bd04f250` closed) the
auto-registration path passed the session uuid as BOTH ids
(`TaskNew(summary, sessionID, sessionID, model)`), so 184 rows have
`task_id==session_id` — the only "one UUID" the ledger has ever held:

```
task_id=session_id rows: 184 (0 of them 't-', 0 uuid rows)   span: 2026-08-28 … 2026-09-02
today: task_id LIKE '01a0%' AND task_id=session_id → 0
```

It was never a design: the collision-retry loop only runs on the MINTED path
(`taskIDGenerate`), so the pass-through form could not detect the duplicate and
would have collided the moment one session registered two rows. Today's uuid-v7
rows share an id with their session **zero** times.

The `t-<unix>` id is also gone from the mint path — the only post-2026-09-05
`t-` rows are test probes (`t-live-decoy`, `t-live-accept`, `t-report-legacy`)
plus two auto rows (`t-1789013949/5454`) and the merger's `t-<unix>` conflict
rows, i.e. the merger submit path still mints t- ids while the queue path mints
uuid v7 (a shape inconsistency, not a semantic one).

### 6. The counterexamples — the "one UUID suffices" hypothesis does not survive

**(a) One row, several runs, and the ledger can name only one of them.**
`sessions/<task_id>/` is 1:N and mostly untracked: 128 dirs hold ≥2 run logs.
`01a0ba14-7d18` is `run_method=resume`, `session_id=847d61f3`, and its dir holds
`eab92eca…jsonl` whose `session_start` says
`{"sessionId":"eab92eca…","taskId":"01a0ba14-7d18-…"}`; `847d61f3`'s log lives in
**another** row's dir (`sessions/01a0b1cd-…/`). So a resume leaves runs that the
row's `session_id` cannot name — only the empty-`task_id` column plus the
`resumed:` tag record the new run.

**(b) An unrecorded run uuid.** `sessions/01a0ba37-ac7d-…/` holds
`5ee7ef8a-…jsonl` (with its own `.raw.log` — that run really happened and
produced the row's `task-report.md`), yet the row's `session_id` is
`0db61d4e-…` and `5ee7ef8a` appears **nowhere** in the ledger
(`WHERE session_id LIKE '5ee7ef8a%' OR tags LIKE …` → 0). A directory walk is
the only way to that run today — the gap that motivates follow-up 2 below.

**(c) A pending row has no session at all** — 3/3, while its
`sessions/<task_id>/{metadata.json,attachments}` container already exists.
Consolidating on the session id would have to invent the key for exactly the
state the queue spends most of its life in.

**(d) Sessions that are not tasks exist and are load-bearing** — `chats`,
`session_conclusions` (write_task_area), bare prompt/briefing sessions. Their
key is the session uuid and there is no row to name.

**(e) The claim path already treats the session as foreign** — the only
`session_id` read on the claim path is `session_id != ?` (anti-self-reference,
`tasks.go:4900`). A claim is not a session; that is the whole point of the
filter.

### 7. Risk of the direction the briefing asks about — eliminate `task_id`

Blast radius (non-test Go occurrences, and the files that carry them):

```
task_id|TaskID: 939 occurrences in 78 files
  internal/tools/tasks.go 176   memory_sync_task.go 103   task_finish_cleanup.go 52
  internal/agent/loop.go 40     memory_sync_delta.go 37   cmd/agent/cli_tasks.go 37
  verify_gate.go 35             memory_sync_claim.go 26   task_reclaim.go 23
  tasklist.go 18                task_staleness_guard.go 17   signalrec.go 17
session_id|SessionID: 305 occurrences  (tasks.go 60, chat.go 39, loop.go 23 …)
```

What breaks, in order of severity:

1. **The claim funnel loses its key.** Pending rows have no session id (measured
   3/3), and the row is `UNIQUE(task_id)` — the session id has no uniqueness
   constraint at all and is *deliberately* shared (239 sessions over 689 rows).
2. **The container path moves under a running worker.** `worker_output.go`
   documents the measured failure this avoids: the session id is backfilled
   after the worker opened `output.log`, so a session-keyed dir would split one
   row across two directories mid-run.
3. **Sync identity changes shape.** `memory_sync_task.go` uses `task_id` as the
   row identity key across nodes; the 2026-09-01/09-09 analyses both refuse a
   rename for exactly this reason (replicas + `parent_task_id` references).
   `devdoc/task-id-collision.md` records the last time two nodes minted the same
   id — the t-<unix> collision whose fix was "unique id", not "borrow another
   namespace's id".
4. **`task_id` is not decoration — it is the only durable handle** for a row
   that is submitted, claimed, delegated to a wrapper, resumed, merged,
   attributed to a branch, and reclaimed; several of those steps happen when no
   session is live.
5. Anything keyed on the session id but NOT a task (chats, session_conclusions)
   would silently merge into the task namespace.

Verdict: **high risk, and it does not remove the confusion** — the confusion
comes from labels and stale comments, not from the existence of two ids. A
consolidation would also be irreversible on 184 legacy rows plus 12 sync
replicas for no functional gain.

### 8. The genuine simplification (what to fix instead)

All three are cheap, additive, and keep the ids:

1. **`task show` explains the ids** — today it prints `session_log:`, `raw_log:`,
   `session_id:` with no statement of which entity each is (`tasks.go:2610,
   2625, 2640`). One label block: `task_id` = the ledger row + its container;
   `session_id` = the session that submitted/executed it; `session_log` = the
   log of the run that executed it.
2. **Record the executor run uuid** — a `run_uuid` column (or a `task_events`
   row) written at the seam that already knows both (`cli_tasks.go:1943`
   → `TaskBackfillSession`), so the 128 multi-run dirs (§6a/6b) resolve to the
   *running* log without a directory walk. Legacy rows keep a NULL and the
   current dir-walk fallback.
3. **Fix the two stale "session id" comments** — `taskregister.go:99` and
   `:314` both say `` `gopi resume <session-id>` `` while the code formats the
   ROW id (`digest += fmt.Sprintf(hint, taskID)`, `:393`). Also `taskregister.go`
   itself is the one place a session uuid was ever used as a task id; its
   comment already states the corrected rule.

## Files

- `devdoc/taskid-vs-sessionid-verdict.md` — NEW (this report; the whole
  deliverable). Read with `git show docs/taskid-vs-sessionid:devdoc/taskid-vs-sessionid-verdict.md`.
- No source file was modified: the epic is an investigation, and every finding
  is a measurement of the existing code/ledger (file:line cited throughout).

## Not Done

- No code change — the epic asked for investigation + risk evaluation; §8's
  three items are written as dispatchable work, not silently applied.
- No legacy row was renamed (and none should be: sync replicas +
  `parent_task_id`).
- No synthetic run that fails a task and resumes it; §6a/b use live rows that
  already did exactly that (stronger than a mock).
- The merger's `t-<unix>` submit path (§5, still minting the old shape) was
  noted, not changed.
- `run_method` coverage (9% of rows) is thin; §6's 1:N evidence therefore rests
  on directories (`128×`) and the `resumed:` tags, not on the column.

## Verification

Diff is one markdown file — no Go source touched, so `go build`/`go test` has
nothing to cover (stated, not skipped silently).

- `sqlite3 ~/.gopi/memory.db '.schema tasks'` — `task_id TEXT NOT NULL UNIQUE`,
  `session_id TEXT DEFAULT ''`, no index or constraint on `session_id`.
- 14 ledger queries (§4, §5, §6): counts, fanout, uniqueness, status×session,
  post-migration `t-` rows, `resumed:`/`run-retry:` tags.
- Bounded python join over the newest 200 rows × `~/.gopi/sessions/*`:
  parsing each dir log's `session_start` for `sessionId`/`taskId`.
- `gopi task show` on this run's own row (binary `bin/gopi`, master `e3195ca6`):
  `task_id`, `session_log`, `raw_log`, `session_id` all printed and cross-checked
  against the live directory listing and against `$GOPI_TASK_RUN_ID` /
  `$GOPI_EXECUTOR_SESSION_ID`.
- Code reads: `internal/agent/session.go`, `internal/agent/taskregister.go`,
  `internal/tools/tasks.go`, `taskproc.go`, `worker_output.go`, `task_session.go`,
  `task_for_branch.go`, `branch_attribution.go`, `cmd/agent/cli_resume.go`,
  `cmd/agent/cli_tasks.go`, `cmd/agent/cli_tasks_worker.go`.
- `git worktree list` + `git status --porcelain` — this run's worktree holds only
  this report (committed on `docs/taskid-vs-sessionid`).

## Evidence (self-filed)

- `git status --porcelain` in `/home/user/workspace/gopi-taskid-vs-sessionid`
  printed nothing (before the commit of this file, with the previous report
  committed; re-run after commit — empty).
- `sqlite3 … "SELECT count(*), sum(task_id LIKE 't-%'), sum(task_id=session_id), sum(session_id IS NULL OR session_id='') FROM tasks"` → `2096|542|184|668`.
- `sqlite3 … "SELECT count(*) FROM tasks WHERE task_id=session_id"` → `184`; `… WHERE task_id LIKE '01a0%' AND task_id=session_id` → `0`; date span of those rows → `2026-08-28 … 2026-09-02`.
- `sqlite3 … "SELECT count(*) FROM (SELECT task_id FROM tasks GROUP BY task_id HAVING count(*)>1)"` → `0` (row ids unique).
- `sqlite3 … "SELECT count(DISTINCT session_id) FROM tasks WHERE session_id<>''"` → `978`; `sum(c) over HAVING count(*)>1` → `689` rows / `239` sessions; top fanout `49d965a8=16`, `714b69af=13`, `6e19120f=12`, `69fd4bba=12`.
- `sqlite3 … "SELECT status, count(*), sum(session_id IS NULL OR session_id='') FROM tasks GROUP BY status"` → `pending 3|3`, `in_progress 2|0`, `completed 1555|430`, `cancelled 301|184`, `failed 167|45`, `verified 68|6`.
- python dir-log join (newest 200 rows): `{'named':104,'other_uuid':9,'no_sid':0,'no_dir':87}`; per-`session_start` check: `checked=113, ledger_sid==run_sid=104, mismatch=9, dir holds another task's log=0`.
- `sessions/` dirs `= 74,943`; `dirs_with_2plus_runlogs = 128` (whole tree), and only `1` among the newest 200 rows.
- `~/workspace/…/5ee7ef8a-…jsonl` present with its `.raw.log`, while `sqlite3 … "SELECT count(*) FROM tasks WHERE session_id LIKE '5ee7ef8a%' OR tags LIKE '%5ee7ef8a%'"` → `0`.
- `head -c 400 ~/.gopi/sessions/01a0ba37-…/5ee7ef8a-….jsonl` → `session_start{"sessionId":"5ee7ef8a…","taskId":"01a0ba37-ac7d-…"}`; ledger row `01a0ba37|completed|0db61d4e-…|deepseek-flash|worker|peerbox:786197`.
- `ls ~/.gopi/sessions/01a0bb5b-af7c-…/` → `b3e8414b-….jsonl`, `b3e8414b-….raw.log`, `output.log`, `metadata.json`, `attachments/`, `task-report.md`; env `GOPI_TASK_RUN_ID=01a0bb5b-af7c-…`, `GOPI_EXECUTOR_SESSION_ID=b3e8414b-…`.
- `bin/gopi task show 01a0bb5b-af7c-…` → prints `task_id`, `session_log: …/01a0bb5b-…/b3e8414b-….jsonl`, `raw_log: …`, `session_id: b3e8414b-…`.
- occurrence counts: `grep -rn 'task_id\|TaskID' --include=*.go internal cmd | grep -v _test | wc -l` → `939` (78 files, top file `tasks.go` 176); `'session_id\|SessionID'` → `305`.
- `sqlite3 … '.schema tasks'` → `task_id TEXT NOT NULL UNIQUE`, `session_id TEXT DEFAULT ''`; `SELECT count(*) … sql LIKE '%session_id%' AND sql LIKE '%UNIQUE%'` → the `tasks` table itself (its `task_id` line), i.e. no unique constraint on `session_id`.
- `sqlite3 … "SELECT run_method, count(*) FROM tasks GROUP BY run_method"` → `'' 922, worker 833, auto 163, manual 103, child 39, merger 28, resume 8`.
- `sqlite3 … "SELECT tags FROM tasks WHERE task_id LIKE '01a0ba14%'"` → `bug,worker:peerbox,run-retry:1,resumed:e314457b-…,resolved`; `ls ~/.gopi/sessions/01a0ba14-…/` → `eab92eca-….jsonl` (not `847d61f3`), and `847d61f3`'s log found under `sessions/01a0b1cd-…/`.

## Follow-ups (dispatchable, each self-contained)

1. **`task show` explains the ids** — one label block in
   `taskShowWithDB` (`internal/tools/tasks.go:2688`) saying what `task_id`,
   `session_id` and `session_log` each are. CLI/doc-only, no schema change.
2. **Record the executor run uuid** — add `run_uuid` (or a `task_events` row) at
   `cmd/agent/cli_tasks.go:1943` (`TaskBackfillSession` call site) and make
   `task show` resolve the running log from it, falling back to today's
   dir-walk for legacy rows; acceptance = the 128 multi-run dirs resolve to the
   *executing* run, the 9 legacy `t-` mismatches still render.
3. **Stale comment fix** — `internal/agent/taskregister.go:99` and `:314` print
   "gopi resume <session-id>" while the code formats the row id; align the text
   with `cli_resume.go`'s accepted shapes.
