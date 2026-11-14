<!-- doc-class: historical -->
# Study: `task notify` — what the line means, what the config does, does "subscribe to a task" work?

Task `01a0bb97` (study, 2026-09-20) · worktree `~/workspace/gopi-notify-study` (branch `docs/task-notify-study`)
Code read: `internal/tools/tasknotify.go` (535 L), `internal/tools/tasknotifybus.go`,
`internal/config/config.go` (`ResolveNotify`, `NotifySettings`, catalog rows).
**No code was changed by this task** — this is an investigation + its evidence.

---

## 1. `[task-notify] cmd ok (stdout: )` — what it means

It is the **delivery-success line of the task-notify side-channel**, printed by
`deliverTaskNotify` (`internal/tools/tasknotify.go:496`) through
`taskNotifyLog` (`:519`, `fmt.Fprintf(os.Stderr, "[task-notify] …")`):

```go
stdout, err := taskNotifyCmd(cfg.cmd, body)
if err != nil {
    taskNotifyLog("cmd %q: %v", cfg.cmd, err)      // failure shape
} else {
    taskNotifyLog("cmd ok (stdout: %s)", stdout)   // ← the line in question
}
```

Read it literally, left to right:

| fragment | meaning |
|---|---|
| `[task-notify]` | the side-channel's stderr prefix (not an error severity marker) |
| `cmd ok` | the configured `notify.cmd` ran and **exited 0** |
| `(stdout: )` | that command's OWN stdout was **empty** — expected here |

The live cmd is `cat >> /home/user/.gopi/notify/task-events.jsonl`: `cat` writes
the payload (stdin) into a file and prints nothing, so an empty stdout is the
**success shape**, not a warning. Failure shapes are different and explicit:
`cmd "…": exit code N (stderr: …)` or `timeout after 30s` (`taskNotifyCmd`,
`taskNotifyCmdTimeout = 30 * time.Second`). Same family: `webhook <url>: ok (HTTP 200)`.

**"whenever a task starts" = the `claimed` transition.** `TaskNext`'s CAS claim
(`pending → claimed`) is the transition the operator sees at run start, and it
is one of the hooked moves — the same line fires for `in_progress`, `completed`,
`failed`, and every synthetic alarm in the event set.

**Do not misread the neighbouring line.** Before 2026-09-12 the same stderr also
carried a bare `[task-notify] event %q for %s not in the events allow-list …`,
so a *successful* write next to a *filter* notice read as a failure. It now
explains itself (`… — not delivered (this is a delivery FILTER, not a failure;
the write succeeded — GOPI_TASK_NOTIFY_EVENTS (env) / the "notify" section in
settings.json="…"; add "…" to it, or set it to "all" for every event)`), and the
pattern-drop line says `filtered by pattern "feat*" — not delivered`.

### Where the hook lives (capture design)
CLI/Go-layer hooks, **not** SQLite triggers — deliberately (devdoc/task-notify.md):
the hook needs the **pre-transition status**, and a delivery failure must never
be able to hold a ledger transaction. Two write entrances: `TaskUpdate` (state-
machine gate records `prevStatus`; hook fires **after** the UPDATE commits) and
`TaskNext` (CAS winner notifies from the in-memory candidate). `prevStatus == status`
returns early — an idempotent writeback is not a transition.

## 2. The `settings.json` `notify` block — purpose and function

It is the **DURABLE layer** of the same feature. Attachment 1:

```json
"notify": {
  "cmd": "cat >> /home/user/.gopi/notify/task-events.jsonl",
  "events": "claimed,in_progress,completed,failed,stale,build-failed",
  "async": false
}
```
(live source: `~/.gopi/.ds_settings.json`, `notify.events` exactly as above)

**Why it exists** — the 2026-09-02 incident: a serve daemon was spawned without
inheriting `GOPI_TASK_NOTIFY_CMD`, so for 3+ days events were delivered to the
void while the process kept printing `[stale] reported N`. Env-only config is
lost on every restart; the settings key is what survives one.

**Fields** (`config.NotifySettings`, key const `NotifySettingsKey = "notify"`, `internal/config/config.go:494`)

| field | function |
|---|---|
| `webhook` | URL to `POST` the JSON payload to (10 s deadline) |
| `cmd` | shell command via `sh -c`, payload on **stdin** (30 s deadline, process-group kill on timeout) |
| `events` | comma allow-list of the statuses/alarms to deliver; `all`/`any` = every status of `taskStatusSet` **plus** every entry of `taskNotifyAlarmEvents` |
| `pattern` | `filepath.Match` glob over the task's tags (whole string + each tag) and the `task_id`; empty = broadcast; invalid glob matches nothing (never errors) |
| `async` | truthy = deliver in a detached goroutine so a slow webhook never delays the ledger writeback |

Both `webhook` and `cmd` may be set; zero targets = notify **off** (zero cost,
zero error output).

**Resolution** — `config.ResolveNotify()` (`internal/config/config.go:543`) is the ONE resolver
every notify path goes through, per knob:

```
GOPI_TASK_NOTIFY_{WEBHOOK,CMD,EVENTS,PATTERN,ASYNC}   (SET wins — even set to "")
  > settings.json "notify"
  > code default/disabled
```

Re-read on **every firing** (the `~/work/tasks` subscribe hot-reload philosophy:
editing the config takes effect without restarting the process). A settings read
error fails **soft** — notify disabled, never a wedge on the write path.
`loadTaskNotifyConfig` (`tasknotify.go:246`) delegates here; the five
`GOPI_TASK_NOTIFY_*` names have catalog rows in `internal/config/settings.go`
(→ `gopi --help-env`).

**Delivery is a side-channel**: failures go to stderr and are never fatal — the
ledger transition is already committed.

## 3. The original design want — "an interested party subscribes to one task"

**Honest answer: the *interest filter* works; the *subscription* does not exist.**

*What exists (= a per-subscriber interest filter, not a subscription):*
`events` + `pattern` are a **single global subscriber's interest expression**,
stored on the subscriber's own side (env/settings). A subscriber expressing
"tell me about branch/feature work" writes `pattern="feat*"`; one expressing
"only terminal outcomes" writes `events="completed,failed"`. Measured (see §5):
a `chore`-tagged task is dropped while a `feat`-tagged one is delivered, with the
drop announced.

*What does NOT exist:* any per-task subscription. There is **no subscriptions
store, no subscribe/unsubscribe surface, and no CLI verb** — verified:

```
$ gopi --help | grep -i notify        → (nothing)
$ grep -rn '"notify"|subscribe' cmd/agent/*.go   → only lineedit's signal unsubscribe
$ memory #1317 / devdoc/task-notify.md
  "订阅存储未引入 … GoPi 的通知走 env 单一来源 (Rule 2)"
  "订阅持久化（subscriptions.json + CLI 子命令）：~/work/tasks 有全套；GoPi 侧 … 待真实需求再引入"
```

The pattern semantics were deliberately aligned with the origin system
(`~/work/tasks/internal/scheduler/subscribe/notify.go`'s `filepath.Match(sub.TaskTypePattern, tsk.Type)`)
but **the store was not ported** — the whole notify config is one source of
truth (Rule 2), so "N parties subscribe to M tasks" cannot be expressed today.

*Worked example of the gap:* "notify me when **this one task** finishes, without
hearing about the other 200" — `pattern` can carry the task id (it is matched
against `task_id`), so a single-task listener is achievable as a **one-off
global-subscriber configuration**, but it means re-pointing the process's notify
config; it is not a subscription that can coexist with other subscribers.

*If the store is ever wanted*, the smallest honest design is a
`notify.subscriptions` array (each entry `{pattern | task_id, events, target}`),
delivered by a loop in `deliverTaskNotify` that calls the existing single-target
primitives and a `gopi task subscribe` verb — i.e. **one new table, reusing
`taskNotifyPatternMatch` + `taskNotifyCmd`/`Webhook` unchanged**. Not proposed
here as work; recorded as the upgrade path because the question was asked.

## 4. Is it documented?

| surface | state |
|---|---|
| `gopi --help-env` | **yes** — all five knobs, each naming its `notify.*` settings key |
| `internal/tools/tasknotify.go` header | **yes** — the settings.json example + all five env vars, in the file every notify reader opens |
| `devdoc/task-notify.md` | historical task report: mechanism, payload, capture design, and an explicit **"Not Done"** naming the missing subscription store |
| `devdoc/task-notify-settings-20260903.md` | the settings layer + the 2026-09-02 incident |
| `devdoc/task-notify-terminal-gap-20260912.md` | the single-funnel fix that produced the two self-explaining stderr lines |
| `devdoc/sweep-zombies-dryrun-notify.md` | the `--dry-run` side-effect class |
| **no user-facing manual** | **gap** — nothing in `GOPI.md` / `README.md`; there is no `docs/` dir. The whole feature is discoverable only via `--help-env` + devdoc + source headers |

**Judged gap:** the *feature* is documented, the *user* is not. A one-page
`devdoc/task-notify-user-guide.md` (or a `GOPI.md` section) stating "configure
`notify.cmd` in settings.json once; here is the payload schema; here is what
`[task-notify]` lines mean" would close it — the payload schema, for instance,
is currently only spelled out inside `devdoc/task-notify.md`.

## 5. Verification — the measured ladder (not inferred)

Sandbox: fresh `GOPI_HOME`/`GOPI_MEMORY_DB`/`GOPI_SETTINGS`, **all five
`GOPI_TASK_NOTIFY_*` unset**, `notify.cmd = cat >> <sb>/sink.jsonl`,
`events="claimed,in_progress,completed"`, `pattern="feat*"`.
Binary `/home/user/work/GoPi/bin/gopi` (b993dc7a2547); task repo verified by
`git rev-parse` before each row was written.

```
submit  (A feat,web / B chore)          bytes 0      ← 'pending' is not in the allow-list
A pending→claimed   [task-notify] cmd ok (stdout: )            bytes 359   ← record 1
B pending→claimed   [task-notify] event "claimed" for <B> filtered by pattern "feat*" — not delivered
                                                               bytes 359   ← unchanged: pattern drops it
A claimed→in_progress [task-notify] cmd ok (stdout: )          bytes 709   ← record 2
events switched to "completed"; A in_progress→completed
                    [task-notify] cmd ok (stdout: )            bytes 1060  ← record 3 (re-read, no restart)
```

Sink content (one physical line; split on `}{`):

```
rec0 claimed     pending   tags='feat,web'
rec1 in_progress claimed   tags='feat,web'
rec2 completed   in_progress tags='feat,web'
```

Payload record (keys delivered; `session_id`/`model`/`claimed_by` omitted when
empty — the human-typed rows carried none):

```json
{"event":"claimed","previous_status":"pending","task_id":"01a0bb99-51e1-…",
 "status":"claimed","summary":"notify probe A","result":"notify probe A",
 "reason":"claim picked up for execution","tags":"feat,web","source":"human",
 "elapsed_seconds":4,"tokens":0,"created_at":1789853782,"updated_at":1789853786,
 "timestamp":"2026-09-20T05:36:27+08:00"}
```

Gate: `go test ./internal/tools/ -run 'TestCovToolsNotifyAlarmRegistryIsTheSourceOfTruth|TestTaskNotify' -count=1`
→ `ok  github.com/nick/gopi/internal/tools  7.545s` (registry test asserts every
fired alarm appears in `taskNotifyAlarmEvents` — the `events=all` completeness guard).

**Operator's sink is live and healthy**: `~/.gopi/notify/task-events.jsonl`
holds real transitions from 2026-08-30 → 2026-09-20 under the very
`notify.cmd` in attachment 1 — the feature is in production use.

### A trap this study hit itself (recorded, not hidden)
Three mid-study probe calls omitted `GOPI_SETTINGS`, so the CLI fell back to the
**operator's real** settings and delivered the `chore` probe into the live sink
(`~/.gopi/notify/task-events.jsonl`) before the pin was re-added. Root cause:
`ResolveSettingsPath()` (`internal/config/config.go:945`) returns the operator default whenever
`GOPI_SETTINGS` is unset — **unsetting the notify env does not hermeticize a
fixture** (memory #2074 trap 3, re-confirmed). The final ladder above pins
`GOPI_SETTINGS` explicitly. Operator impact: six harmless ledger-probe records
appended to a write-only audit sink; no library or ledger mutation beyond the
probe rows in this session's sandbox.
