<!-- doc-class: historical -->
# Requirements 11 — Notifications & Observability

> **Module B11/15** of the GoPi reverse-requirements series. This is not a design
> document and not a spec written from imagination: it reconstructs the **user
> need** behind an existing surface by reading the built system, then measures the
> gap between that need and the code.
>
> Every claim is cited (ledger task id / `file:line` / commit SHA / rule number) or
> prefixed `[opinion]`. Inferences are marked `[inferred]` with their weakest link
> named. **This branch changes exactly one file.**
>
> **Revision read:** `feat/m11-notifications-observability` at `0ac1f70e`
> (master `5cdae25e`), 2026-09-18, host `peerbox`. Every `file:line` and every
> measurement below is that revision on that host.

---

## 1. What this is for

I run gopi on a machine I am not sitting at. Work starts, runs for tens of
minutes, and ends — or does not. I need two different things from that, and they
pull in opposite directions.

**First: I must be told, without asking, the small number of things only I can
decide.** Work that landed on the trunk, work that failed and will not retry, the
machine no longer running the code I think it is running, a task that has gone
quiet while holding a claim, a branch that no row owns. These are not progress
reports. Each one is a decision waiting for me: accept it, re-dispatch it, look at
it, or run something by hand. If nobody tells me, the decision simply does not
happen — and the system's own record shows that this is what occurs: work sat
unfixed across several reports until the rule was changed to "dispatch again,
never wait" (GOPI.md Rule 26).

**Second — the half that is easy to forget — I must be able to look back and find
out what happened.** A notification tells me *that* something needs me. It is a
one-line summary at best. The question that follows it is always "why", and only a
record answers it: what the run was asked to do, which tool calls it made, what
the process was doing when it died, what the daemon printed while I slept. Without
the record, every notification is a dead end — told something broke, given no way
to know anything about it, which is worse than not being told.

**The tension is the point of this module.** A system that reports everything
reports nothing: it trains me to stop reading. A system that reports nothing
leaves me blind. The job is to pick the handful of moments that are genuinely
mine — and then make the archive good enough that those few moments are enough.

**What "good enough" means, in my terms:** after a run I did not watch, I can sit
down and reconstruct the failure from the artefacts alone, without re-running
anything and without asking the machine to remember something it discarded.

## 2. Who is served

| actor | the need it has from this module | evidence |
|---|---|---|
| **The operator (nick)** — the primary user, and the only actor for whom this module exists | To be interrupted *only* when a decision is his, and to reconstruct any finished-or-died run afterwards from what was left on disk | `01a09225-a216…` (filed because terminal transitions never notified); `01a0aad7-8a18…` ("merger tick past due"); `01a091e7-6efc…` ("why does gopi report previous 14 crashes?") |
| **The agent (Bond)** — a run reading its own past | To answer "has this been tried, and what did it say?" without a human in the loop; the notify sink is one of its cheapest evidence sources, and a session's own log is the only place its reasoning survives | `tasknotify.go:180-203` (the payload is the transition, machine-parseable); `internal/gopilog/` (the reader exists to be read back) |
| **The merger / worker daemons** | To record their decisions where a human will find them, since they run unattended and their stdout is nobody's terminal | `cmd/agent/cli_worker_daemon.go:62`; `~/.gopi/logs/merger.log`, `worker.log` |
| **The next operator, or the next agent, after the machine changed** | To read what happened *without* the process that produced it: a crash dump and a session log outlive the run | `internal/agent/crashdump.go`; `internal/taskreport/gateclaim.go` |
| **A future maintainer** | A named, single funnel for "who is told what", changeable in one place rather than N call sites — *this is why the bus exists* | `internal/tools/tasknotifybus.go:3-12` |

**The distinguishing need.** Only the operator needs *both* halves at once. The
agent needs the record (it does not read notifications); the daemons need to write
one (they do not read them); the maintainer needs the funnel. The operator is the
actor whose attention is the scarce resource, and every design statement in this
module is ultimately a statement about spending it.

## 3. The workflow it sits in

**Entry condition.** Some process moves a task row's status, or a daemon decides
something about the world (a merge landed, a build did not match the trunk, a
branch is owned by no row, a loop stopped progressing). A same-status write is
explicitly not a transition (`tasknotify.go:291-292`).

**End-to-end.**

1. **The row moves and is committed**, then the transition is recorded into the
   notify **bus** and flushed (`tasknotifybus.go:112`). Every writer of
   `tasks.status` now goes through that one funnel; before it, the hook was wired
   at exactly two call sites and every other writer moved the row silently
   (`tasknotifybus.go:5-9`, commit `e75e2ee5`).
2. **The configuration is re-read on every firing**, never cached, so a change
   takes effect without restarting a daemon (`tasknotify.go:116-135`). Resolution
   is one resolver per knob, merged per knob — explicit env (even "") > the
   settings file > the code default (`config.ResolveNotify`).
3. **Two filters, in this order**: the *event allow-list*
   (default `claimed,in_progress,completed,failed`, `tasknotify.go:59-63`) and an
   optional *pattern* glob against the task id and tags (`tasknotify.go:205-215`).
   A drop on either is logged with the reason (`tasknotify.go:302-303`, `:307`).
4. **The payload is assembled from the committed row** — event, previous status,
   task id, status, summary, the latest audit-trail digest, the human-readable
   reason, session id, model, tags, source, who claimed it, elapsed seconds, token
   count, plus the zombie evidence chain when the bus decorates it
   (`taskNotifyPayload`, `tasknotify.go:180-203`; assembly `:310-337`).
5. **Delivery** to one or both configured targets: an HTTP POST to a webhook or a
   shell command with the payload on stdin. Both carry a deadline and the command
   runs in its own process group, so a hung child is killed with its children
   (`taskNotifyWebhookTimeout = 10s`, `taskNotifyCmdTimeout = 30s`,
   `SysProcAttr{Setpgid:true}` — `tasknotify.go:56-57`, `:373-383`, `:394-433`).
   Delivery may be synchronous (the default) or detached so a slow target never
   delays the ledger write (`tasknotify.go:338-341`).
6. **Failure is never fatal.** A delivery error is logged and swallowed: the
   ledger transition is already committed and is the authority
   (`tasknotify.go:32-33`, `tasknotifybus.go:22-25`).
7. **The record half is written independently of all of the above.** It does not
   go through the notify pipeline: the session's own log directory, the per-host
   daemon log, and (on some deaths) a crash dump.

**Exit conditions, and they are three different ones.**

- *Delivered*: the configured target received the payload. On this host the target
  is an append-only file (measured in §6.0).
- *Deliberately dropped*: the event is outside the allow-list, or the pattern did
  not match. A line is logged; nothing is delivered. **A supported end state, not
  an error** — and half of the signal/noise design.
- *Recorded but not signalled*: the row moved and only the audit trail knows.
  §6.0 measures how large that is.

## 4. Why it exists

### 4.1 "A transition that only I can act on must reach me" — *recorded*

The operator filed the **absence** as a bug, with the measurement attached:

> "terminal transitions never notify: completed/failed never reach the operator
> (12/12 completed tasks show only 'claimed')" — ledger
> `01a09225-a216-7453-ba0f-2513cf2f8a60` (`source=human`, 2026-09-12).

Closed by commit `e75e2ee5` ("funnel every operator-relevant status transition
through one notify path"), which created `tasknotifybus.go`. **The requirement is
recorded, not inferred**, and the fix is why `completed` is now 32 % of volume.

### 4.2 "A signal must be *about* something I can act on" — *recorded, and self-corrected*

The sharpest recorded evidence is an alarm that fired and **worked**, so it is
worth telling properly. In September the post-merge version probe compared the
whole `--version` **display** against the trunk tip instead of the revision the
display carries. A tagged binary answers with a multi-line report, the probe read
it as one token, recovered no revision, and called a correct build a mismatch —
which the post-merge rebuild turned into an **auto-revert of every tagged merge**.

The alarm told the operator. He filed it, twice:

```
01a09abb-2ca1-7c81-b298-44de73d04cc2  llm  verified  2026-09-13 20:25
  bug(merger): post-merge rebuild auto-reverts EVERY merge — version probe compares a multi-…
01a09fd2-3298-7de0-ac40-5a1836110801  llm  completed 2026-09-14 20:09
  bug(merger): version probe mis-parses the multi-line `gopi --version` report → landings au…
```

Fixed by `810f19ac` (2026-09-13 21:07), an ancestor of this HEAD, whose own
comment records the mechanism:

> "a probe that re-parsed that report through the single-line parser recovered NO
> revision out of a perfectly correct answer — then reported it as a version
> mismatch, which the post-merge rebuild turned into an auto-revert of every
> tagged merge round (epic 2026-09-13)"

**This is §4.1's requirement demonstrated end-to-end**: something only the
operator could decide happened at night, an alarm reached him, he filed it, it was
fixed. It is also the reason §6.4 measures the alarm *ratio* rather than calling
the alarms noise.

### 4.3 "The set of things that reach me must be chosen, not everything" — *inferred*

The strongest inference in this document, supported by code that implements a
position no user typed:

- The default list excludes `pending` **deliberately**, reason written down:
  "creation, not a transition" (`tasknotify.go:59-63`).
- A subscriber who does not list a status does not get it, and the drop is logged
  rather than silently taken (`tasknotify.go:302-303`, `:307`).
- The async knob exists so that "a slow webhook never delays the ledger writeback"
  — the signal explicitly subordinate to the record (`tasknotify.go:338`).

**Falsifiable:** if the intent were "tell him everything", the default would be
`all`, the drop log would not exist, and the pattern filter would not exist.
Instead `all` is a non-default escape hatch. See §6.2c for the counterweight: the
code chose *curation*, then the live configuration re-opened it.

### 4.4 "Each alarm must name the situation that requires a human"

The synthetic alarms are the clearest statement of this, because each exists to
name a *human* situation and carries its reasoning in a comment at the site:

| event | the human situation | citation |
|---|---|---|
| `build-failed` | "the operator knows the binary didn't follow master" | GOPI.md:355-361 (Rule 12); `rebuild.go:21-23`, `:311-324` |
| `merge-reverted` | a merge that shipped a regression was unwound — the trunk moved backwards | `merge_revert.go:78-88`; `rebuild_vet_revert.go:19` ("master stayed broken until a human read the alarm") |
| `stale` (task heartbeat) | a claimed task went quiet while holding its claim | `taskheartbeat.go:20-22`; operator row `t-1788040730` |
| `worktree-orphan` | a dirty worktree **no ledger row owns** — "the one shape no automated arm can act on" | `worktree_sweep.go:1211`; `tasknotify.go:160-165` |
| `loop-stuck` / `loop-stuck-toolful` | an agent loop that stopped making progress | `internal/agent/loop.go:265-280` |
| `session-wallclock-exceeded` | a session past its wall-clock bound | `internal/agent/loop.go:303-305` |
| `terminal-push-failed` | the end-of-life push to a peer exhausted its retries | `tasknotify.go:67-71` |
| `legacy-conflict` | a delta import hit a both-sides-wrote conflict | `tasknotify.go:72-77` |
| `ledger-conflict-human-task` | a **real human/LLM ticket** reached a conflict ruling — once blind-flattened to `completed`, which "FAKE-CLOSED real work" | `tasknotify.go:78-92` |

### 4.5 "I must be able to look back afterwards" — *recorded*

- `t-1788258886` (`source=human`) — *"被杀 session 的显示诚实性：SIGTERM 死亡无 session_end
  时 log 渲染 EXIT: SUCCESS——假象"*: a killed session rendered as SUCCESS, which
  *"误导排查一整回合"*. The fix renders `TERMINATED (no session_end …)` and attaches
  the raw-log tail as the death-site evidence chain.
- `t-1788330933` and `t-1788357623` — the operator asked for the log path *from
  the surface he was already looking at*, twice.
- `t-1788555329`, `t-1788581076` — the log surface was **re-designed in a
  competition** at his request; implemented in `t-1788583867`.
- `01a091e7-6efc-7e19-80f2-54773bfb3f05` (`source=human`, 2026-09-12) — *"why does
  gopi report previous 14 crashes? … I suspect it confuses with both worker mode
  and task run mode?"* — filed against a report that was **wrong about which
  sessions had died**, i.e. a truthfulness requirement on the record.
- `01a0aad7-8a18-76ef-977f-cdcfc4c22e3b` — *"merger --status shows … elapsed 10m38s
  … how does status report merger last tick due without knowing the next tick
  time? … this is a buggy flavor."* — the operator treating the **display** as
  something he is owed a correct version of.

### 4.6 "The record must outlive the process, and be findable from the row"

- Crash dumps go to a fixed root with a naming family, and a sweep gate decides
  when to re-walk (`internal/agent/crashdump.go`, `crashdump_sweep_gate.go`;
  tests `TestWriteCrashDump_NamingFamily`,
  `TestOrphanSweepDue_VerdictTableFailsOpenOnEveryUncertainty`).
- A session's directory is the unit: `<session-id>.jsonl`, `<session-id>.raw.log`,
  `output.log`, `console.log`, `session.dump.json`, `metadata.json`,
  `attachments/`, `task-report.md`.
- The daemon logs are declared, not discovered: `cmd/agent/cli_worker_daemon.go:62`.

## 5. Acceptance in user terms

**A1 — a terminal transition reaches me.** When a task reaches a terminal state,
the target I configured receives one payload whose event is that state. *Bug if:*
the transition happened, the configuration listed that event, and no payload
arrived. *It failed exactly this way:* `01a09225-a216-7453-ba0f-2513cf2f8a60`.

**A2 — the payload identifies the run without a lookup.** Task id, previous and
new status, summary, reason, session id, elapsed time. *Bug if:* a payload leaves
me unable to find the session or the row it is about.

**A3 — a drop is explainable.** When the configuration filters an event out there
is a record naming the event and the task. *Bug if:* I cannot tell "nothing
happened" from "it happened and was filtered". *Evidence:* `tasknotify.go:302-307`;
measured in §6.2b.

**A4 — delivery failure is not a lost transition.** A delivery error does not
change the task's outcome. *Evidence:* `tasknotify.go:32-33`,
`tasknotifybus.go:22-25`.

**A5 — I can reconstruct a run I did not watch.** From artefacts alone I can state
what it was asked to do, what it did, and how it ended. *Bug if:* the honest answer
is "SUCCESS" for a run that was killed (`t-1788258886`).

**A6 — an alarm says which situation it is.** A synthetic alarm reaches me with
its subject — repo, branch, task or session — determinable from the payload.
*Evidence:* §6.4b — the 40 alarms name a repo path in the `task_id` field, so this
passes by convention, not by decision.

**A7 — being told "everything" actually tells me everything.** If I configure the
universal sentinel, every alarm the system can raise reaches me. **The code
currently fails this**; see §6.4e.

**A8 — the target is durable across a restart.** A daemon starting with no notify
environment still delivers to the settings-configured target. *Evidence:*
`tasknotify.go:116-135`, and the 2026-09-02 incident recorded there ("a restart
dropped `GOPI_TASK_NOTIFY_CMD` and stale events silently stopped landing" — the
reason the settings layer exists).

## 6. Gap analysis

### 6.0 The measured state of the signal

**The configured target.** `notify.events` = six events; `notify.cmd` appends the
payload with `cat >>`, **with no newline**, to one file:

```
$ jq -r '.notify' ~/.gopi/settings.json
{"events":"claimed,in_progress,completed,failed,stale,build-failed",
 "cmd":"cat >> /home/user/.gopi/notify/task-events.jsonl"}
```

Measured 2026-09-18 on `peerbox` with `python3 final.py ~/.gopi/notify/task-events.jsonl`
(script kept at `~/workspace/m11-tools/final.py`, outside the worktree):

| property | value |
|---|---|
| bytes / newline bytes / `}{` delimiters | 49,446,070 / **1** / 132,090 |
| deliveries | **132,090** |
| distinct task ids named | 36,911 |

**Histogram, with the writer's `source` — because `source` is the whole argument:**

| event | count | share | `auto` | `human` | `llm` |
|---|---:|---:|---:|---:|---:|
| `completed` | 42,331 | 32.05 % | 31,228 | 8,831 | 2,272 |
| `in_progress` | 36,773 | 27.84 % | 17,355 | 17,180 | 2,238 |
| `claimed` | 30,060 | 22.76 % | 12,585 | 15,254 | 2,221 |
| `failed` | 21,432 | 16.23 % | 18,151 | 2,981 | 300 |
| `stale` | 1,454 | 1.10 % | 990 | 411 | 53 |
| `build-failed` | 40 | 0.03 % | — | — | — |

**The alarm ratio, stated precisely.** Three of the six configured classes —
`claimed`, `in_progress`, `completed`, together **109,164 of 132,090, 82.64 %** —
are machine bookkeeping: emitted by an automated writer for work the operator did
not submit and cannot act on (`auto` alone is 61,168). The classes that can require
a decision — `failed`, `stale`, `build-failed` — are **22,926, 17.36 %**. So:

- bookkeeping : triageable = **1 : 4.76**
- the rarest and most consequential alarm, `build-failed`, is **1 in 3,302
  deliveries** (40 of 132,090)

**Per-day volume, which is where the shape shows:**

| day | deliveries | breakdown |
|---|---:|---|
| 2026-09-05 | 4,162 | in_progress=1263 completed=1251 claimed=1069 failed=512 stale=67 |
| 2026-09-08 | 4,225 | completed=1247 in_progress=1240 claimed=999 failed=703 stale=36 |
| 2026-09-10 | 15,095 | completed=4429 in_progress=4230 claimed=3517 failed=2797 stale=122 |
| **2026-09-11** | **50,063** | completed=16249 in_progress=13739 claimed=10758 failed=8739 stale=578 |
| **2026-09-12** | **42,322** | completed=14493 in_progress=11458 claimed=9375 failed=6626 stale=369 build-failed=1 |
| 2026-09-13 | 1,073 | … build-failed=19 |
| 2026-09-14 | 177 | … build-failed=5 |
| 2026-09-16 | 413 | in_progress=123 claimed=115 completed=114 failed=41 stale=7 **build-failed=13** |
| 2026-09-18 | 369 | completed=140 claimed=72 in_progress=72 stale=53 failed=32 |

**One day delivered 50,063 events, of which 578 were `stale` and none was
`build-failed`.** The signal the operator most needs is a rounding error inside
the stream he must scan for it.

**[FINDING 6.0 — new, and structural]** **The sink is not a JSONL file, it is one
unterminated JSON line, and it has been unreadable by line-oriented tools for its
entire 49 MB life.**

```
$ tr -cd '\n' < ~/.gopi/notify/task-events.jsonl | wc -c
1
$ wc -l ~/.gopi/notify/task-events.jsonl
1
$ jq -c . ~/.gopi/notify/task-events.jsonl >/dev/null
parse error: Invalid numeric literal at line 1, column 33994549
```

Every delivery is the object immediately followed by the next — `}{`, 132,090
occurrences, one line break in the file. The consequence is not cosmetic: the
surface the operator subscribes to in order to **look back** is the one surface a
`jq`, `grep -c`, `wc -l`, `sort`, `tail -1` or any log tool cannot read. It is the
default behaviour of the documented configuration — `tasknotify.go:310-312`
marshals the object alone and the configured command is `cat >>` **without** a
trailing `echo`.

**And it is worse than "unparseable": it is silently mis-parseable.** The obvious
repair is to split on the `}{` delimiter. That is unsound, because JSON strings may
contain `}{`. The `probe-1` delivery in this very file demonstrates the mechanism:

```
\n{"event":"claim…   ← a newline INSIDE a delivery, from a probe payload
```

A streaming reader that trusts `}{` as a separator gets a **wrong count, silently**.
Measured: splitting on `}{` yields 132,091 chunks (132,090 parse, 1 does not);
`awk`/`grep` on `"event":` yields 132,089; a streaming `raw_decode` yields 132,092
plus 43 resync bytes. **Three readers, three answers, no error.** A surface that
must be quotable for an operator audit cannot be in that state.

### 6.1 Serves — implemented, with the site

| need | served by | note |
|---|---|---|
| 4.1 terminal transitions reach me | the bus (commit `e75e2ee5`) | gap filed and closed; `completed` is now 32 % of volume — *over*-served |
| 4.3 the set is chosen | allow-list + pattern + drop log | implemented; the default is curatorial, the live config is not (§6.2c) |
| 4.4 each alarm names a situation | **19** synthetic alarm constants | implemented in full |
| 4.5 I can look back | `internal/gopilog` (10,083 L, 27 files), `cli_log.go` (803 L), the `stats` verb | the largest investment in this module by an order of magnitude |
| 4.6 the record outlives the process | 2,314 crash dumps (57 MB) + session dirs + declared daemon logs | `TestOrphanSweepDue_…` pins the fail-open verdict table |

**The reconstruction half works** — I attempted the required log-only
reconstruction (§6.6) and it **succeeded**.

### 6.2 Falls short

**[GAP 6.2a — the sink's readability (6.0).]** The look-back surface fails A5 for
anyone who reads it with tools. Nothing in the code documents a newline contract,
so this is not a broken promise but a **never-decided** decision that landed on the
unreadable side (§6.4c).

**[GAP 6.2b — `cancelled` is invisible, and it is one of the decisions the rules
make mandatory.]** The ledger today:

```
$ sqlite3 -readonly ~/.gopi/memory.db "select status,count(*) from tasks group by status;"
completed 1397 | cancelled 273 | failed 155 | verified 68 | pending 35 | in_progress 6
$ grep -o '"event":"cancelled"' ~/.gopi/notify/task-events.jsonl | wc -l
0
$ grep -h 'not in the events allow-list' ~/.gopi/logs/*.log | wc -l
1
```

**273 `cancelled` rows, zero `cancelled` deliveries, one logged drop ever.** The
other 272 are simply absent — the drop log only fires when the pipeline is reached
with a configured target. `cancelled` is in `taskStatusSet` (`tasks.go:77-82`) and
in `all`, but not in the live allow-list and not in the default. It matters more
here than anywhere: GOPI.md Rule 28 makes the cancel the **mandatory** half of
migrating a task between hosts ("the cancel is not optional"), and Rule 26 makes
re-dispatching the standard repair path. Cancellation is a deliberate operator
decision, and it is the one decision whose notification is off by default.

**[GAP 6.2c — the noise budget was curated by the code and re-opened by the
operator; the honest statement is weaker than §4.3.]** The code's position is
curation (default four events, `pending` excluded as "creation, not a
transition"). The live configuration is a **superset** of that default. So 132,090
deliveries with a 1 : 4.76 ratio is the **chosen** volume, not the default volume.
**§4.3's inference is not wrong, but it is not what is running.** [inferred —
weakest link: one host's configuration. A second operator running the four-event
default would make 4.3 describe the live system exactly.]

### 6.3 Over-builds

**[OVER-BUILD 6.3a — 19 synthetic alarm names, 17 of which have never been
delivered.]** All 19 constants in the tree, against the sink's entire history:

| ever delivered | never delivered |
|---|---|
| `build-failed` (40), `stale` (1,454) | `merge-reverted`, `merge-verify`, `merge-verify-stale-rework`, `merge-verify-stale-landed`, `worktree-reaped`, `worktree-orphan`, `worktree-off-policy`, `premerge-refused`, `premerge-unverified`, `merge-backlog-terminal-refused`, `merge-backlog-terminal-unverified`, `terminal-push-failed`, `legacy-conflict`, `ledger-conflict-human-task`, `loop-stuck`, `loop-stuck-toolful`, `session-wallclock-exceeded` |

Not automatically a defect — several fire only on paths that have not occurred, and
a *dropped* delivery is indistinguishable from a never-fire in the sink (below).
But it is a real cost against **C5**: 17 named alarms, each with a constant, a
comment block stating its human situation, and in several cases tests, for signals
that have never reached the person they were built for. **Note that 13 of the 17
are `worktree`/`premerge`/`merge-backlog` events — one subsystem, not a scattered
over-build.** [inferred — weakest link: cannot separate "never fired" from "fired
and dropped"; see §7.5 for the observable that would settle it.]

### 6.4 Never decided

**[NEVER DECIDED 6.4a — who is told about a completion, and whether it is signal.]**
`completed` is 32 % of volume and mostly `auto` (31,228 of 42,331). But the
operator *filed `01a09225-a216` precisely because completions were not reaching
him*, and the bus was built to fix that. So the project recorded "completions must
notify" as a **bug fix** and has never had the conversation "…but 16,249 of them in
one day is not a signal". The curation principle **exists** — it was applied to
`pending` (`tasknotify.go:59-63`) — and was simply not applied to the event class
that dominates the file. **Nothing in GOPI.md, the ledger or the code answers "how
many completions a day is too many".** [inferred]

**[NEVER DECIDED 6.4b — whether an alarm's payload must name its subject.]** All
40 `build-failed` payloads carry `task_id` = a **filesystem path**
(`/home/user/work/GoPi` ×39, `/home/user/workspace/gopi-arch-sweep-demo` ×1) with an
empty `previous_status` and `session_id` set to the same path. The call explains it
— `NotifySyntheticEvent(BuildFailedEvent, r.Repo, "", summary)` (`rebuild.go:323`)
passes the repo where the signature expects a session id (`taskheartbeat.go:40-41`,
"The payload shape mirrors taskNotifyPayload (empty ledger fields)")
— but whether a consumer is documented to tolerate a path in `task_id` is recorded
nowhere I found. **Could not determine whether this is a decision or an accident.**

**[NEVER DECIDED 6.4c — whether the sink is a log file or a data file.]** The
config spells it `cmd`, the natural idiom is `cat >>`, no newline is written, and
the result is one 49 MB line. Nobody decided that; it is the intersection of two
individually reasonable choices. Recorded as a decision never made, because "we
chose an unreadable format" is not something anyone would decide on purpose.

**[FINDING 6.4d — the alarm worked, and then it did not stop. This is the ratio
failure, and it is a real one.]** All 40 `build-failed` deliveries, classified by
what they actually report:

| family | count | days | exit | true or false? |
|---|---:|---|---|---|
| version mismatch | 25 | 09-12 (1), 09-13 (19), 09-14 (5) | all **exit=0** | **TRUE — a real bug** (§4.2), fixed `810f19ac` |
| real build failure | 13 | 09-16 (11), 09-17 (2) | exit=1 | true — e.g. `internal/scratch` compile errors, `go: downloading modernc.org/sqlite` |
| vet gate | 2 | 09-16 (2) | — | true (merge kept, reported) |

**Every one of the 25 version-mismatch alarms was correct**: in each, the binary
genuinely was not the tip by the comparison the code then performed. That is the
alarm doing its job, and it is why §4.2 cites it as the requirement demonstrated.
**But look at the density:**

```
2026-09-12 05:13   1
2026-09-13 15:31 → 21:11  19   ← one every ~18 minutes for six hours
2026-09-14 09:01 → 14:49   5
2026-09-16 06:31 → 23:11  13   ← the build was broken for 17 hours
2026-09-17           2
```

**46 alarms in five days**, 19 of them inside a six-hour window on 09-13. The
operator's own rows confirm he had to act: `01a09fd1-3f80` ("RE-LAND the reverted
perf fix"). So the measured failure is not "the alarm lied" — it is that **the
alarm has no quiet interval**: a persistent condition re-fires on every sweep, so
the 1-in-3,302 signal is delivered 19 times in an evening and 13 times across a
17-hour broken build. **Nothing in the sink, the code or the rules implements or
even names a repeat interval.** That is §6.4a's missing decision in its sharpest
form, and it is exactly what makes a 1-in-4.76 bookkeeping ratio fatal: an operator
who receives 19 copies of one problem in a night learns to skip the stream.

**Two smaller defects inside the same family, both measured:**

1. **The word `FAILED` rides `exit=0` — in all 25 cases.** A reader cannot triage
   on the exit field, because the status shown is not what is being judged. The
   `expected=… actual=…` tail only appears when `r.ExpectedVersion`/`r.ActualVersion`
   are non-empty (`rebuild.go:317-322`), so the same alarm has two shapes and the
   machine-readable half is sometimes absent.
2. **The *dirty* variant is a probable false positive and remains unfixed.** One
   delivery reads `expected 1cb6b05b, binary reports "1cb6b05bf82d-dirty"` — the
   binary **is** the expected revision, merely built from a dirty tree. `versionMatches`
   returns `false` on `IsDirty` by design (`rebuild.go:256-258`, with its reasoning
   comment at `:381-385`), which is defensible — but the *alarm's* word for it is
   `post-merge rebuild FAILED`. [inferred: I read the one dirty delivery and the
   documented rule; I did not reproduce a dirty build.]

### 6.5 The required failure-mode-with-no-trace

**[FINDING 6.5 — new: a failed auto-registered task leaves no trace of itself, and
45 rows are already in that state.]**

**The situation:** work is submitted and does not arrive. The most expensive
failure in the system, because *nothing happens afterwards* — no failing run to
reconstruct, no notification, no row that explains itself. Silence is the symptom.

`source=auto` rows are created by the operator's own CLI verbs through an
auto-register path. When that path fails, the ledger holds a `failed` row with **no
session** — there was never a run to log:

```
$ sqlite3 -readonly ~/.gopi/memory.db \
  "select count(*) from tasks where status='failed' and (session_id is null or session_id='');"
45
```

Pick one — `01a0b2c4-fd05-799f-b003-3ad4299531d0`, summary `[auto] config show`:

```
$ ls -la ~/.gopi/sessions/01a0b2c4-fd05-799f-b003-3ad4299531d0
ls: cannot access '…': No such file or directory
```

**No session directory exists for that task id.** The row exists, says `failed`,
says `task failed`, and there is no log, no raw log, no `output.log`, no crash
dump, and no notify payload that explains why. The `failed` payload says only:

```
{"event":"failed","previous_status":"in_progress","task_id":"01a0b2c4-fd05-…",
 "summary":"[auto] config show","result":"zombie disposal (…)","reason":"task failed",
 "session_id":"95483818-…","elapsed_seconds":5643,"timestamp":"2026-09-18T14:01:32+08:00"}
```

Everything in it is *about the row*. The reason the run produced no work is in none
of it.

**[FINDING 6.5a — new: the row's `session_id` and the session's directory live in
different namespaces, and the retrieval path does not bridge them.]**

```
row 01a0b2c4-fd05-799f-b003-3ad4299531d0 → session_id 95483818-d939-4449-b3f7-b751c710837b
$ ls -la ~/.gopi/sessions/01a0b2c4-fd05-799f-b003-3ad4299531d0  → No such file or directory
$ ls -la ~/.gopi/sessions/95483818-d939-4449-b3f7-b751c710837b  → session.jsonl  session.dump.json
                                                                   console.log   metadata.json  *.raw.log
```

"Find the run behind row X" therefore requires a **second lookup**
(`select session_id from tasks where task_id=…`) before any path exists — and a
reader who assumes the directory is named for the task, which is the natural
assumption and the one the module briefing's own line encourages
(`01-per-module-briefings.md:413`, `~/.gopi/sessions/<task-id>/<session-id>.jsonl`),
finds nothing and concludes the run left no trace. **A discoverability defect on
the look-back half**, and exactly the shape §4.5 measures: `t-1788330933` and
`t-1788357623` are the operator asking, twice, for the log path to be shown *from
the row he is looking at*. The lookup exists; the bridge does not.

**What I do NOT claim:** why the auto-registered row was created without a session
— that is the submit/auto-register path in `internal/tools`, M1's boundary. This
module owns that **once the path fails, the observability surfaces can say nothing
about it**: no session, therefore no record; the notify payload is a
webhook-shaped summary of the row.

### 6.6 The required log-only reconstruction (attempted, not assumed)

```
$ sqlite3 -readonly ~/.gopi/memory.db \
  "select task_id,session_id,datetime(created_at,'unixepoch','localtime') \
   from tasks where status='failed' and summary like '%config show%' order by updated_at desc limit 3;"
01a0b2c4-fd05-…|95483818-…|2026-09-18 12:27:29
```

**Result — the reconstruction SUCCEEDED, and it needed all three layers:**

| layer | what it contributed |
|---|---|
| the ledger row | that it failed, and that the work was `[auto] config show` |
| the notify payload's `result` | **the mechanism**: `zombie disposal (zombie: heartbeat stalled past grace (last_toolcall_at idle)); heartbeat stop + claimed pid 786679 dead (peerbox) — confirmed zombie` |
| `console.log` in the session dir | **what it was doing**: the agent's own reasoning about `gopi config show` hanging, and the memory hits it had found |
| `session.dump.json` in the session dir | **the death verdict, independently**: `{"category":"orphan-no-session-end","note":"process died without session_end (kill/panic/power loss); no request context recoverable — see session_log","orphan":true,…}` |

**So §4.5's requirement is met on this path** — and it required the `result` field,
which exists only because of bug056 (`tasknotify.go:316`). **The counter-finding is
that the row's own `reason` column still reads `task failed` for all of it** — the
human-readable, operator-facing field on the ledger row is the one that says
nothing, while the useful text lives in a payload stream (§6.0) that is one
unreadable line. **The information exists; it is stored in the layer hardest to
read and absent from the layer easiest to read.**

### 6.7 The required set of existing notify/log tests — see §7

### 6.8 The sixth gap class the epic asks for: **what a universal subscription misses**

**[FINDING 6.8 — new, and it is a silent gap with no test.]** The operator-facing
promise of the universal sentinel is stated in the code:

```
taskNotifyEventsAll = "all"   // "enables every status incl. pending/cancelled"
```

and the code extends it to synthetic alarms, with a reason:

> "M4.19 synthetic alarm events ride `all` too — a subscriber who asked for
> everything must not silently miss the alarms"
> — `tasknotify.go:154-166`

**That promise is 15 alarms short.** `all` enables the 8 statuses from
`taskStatusSet` (`tasks.go:77-82`) plus exactly four alarms — `terminal-push-failed`,
`legacy-conflict`, `ledger-conflict-human-task` and `worktree-orphan`. Measured
against the 19 alarm constants in the tree, these are **not** enabled by `all`:

```
build-failed        merge-reverted          worktree-reaped
worktree-off-policy premerge-refused        premerge-unverified
merge-backlog-terminal-refused              merge-backlog-terminal-unverified
merge-verify        merge-verify-stale-rework  merge-verify-stale-landed
loop-stuck          loop-stuck-toolful      session-wallclock-exceeded
stale
```

So an operator who does the one thing that means "tell me everything" gets **no
build alarm** (Rule 12's mandatory signal), no merge-reverted alarm, no stuck-loop
alarm, no wall-clock alarm, no stale alarm. Each of those requires being named
**exactly** — and a typo in that name (`merge-verify-stale-rework` vs
`merge-verify-stale-landed`) fails **silently**, because
`cfg.events[ev] = true` (`tasknotify.go:169`) accepts any string as a valid event
name. There is no validation of the configured names against the known set, no
warning for an unrecognised one, and — measured — **no test anywhere on the `"all"`
sentinel**:

```
$ grep -rn '"all"' internal/tools/tasknotify*_test.go
(no matches)
```

The live configuration on this host lists six explicit names, which is why
`build-failed` and `stale` reach him at all — the operator worked around the gap
without knowing it was there. **A7 fails, and nothing in the test suite notices.**

## 7. Implications for tests — existing tests whose verdict the requirement changes

**No tests are written by this document.** These are named because the requirement
changes what their green means.

**7.1 `internal/tools/tasknotify_test.go` and the `tasknotify*` family** —
`TestTaskNotifyTransitionFires`, `TestTaskNotifyNonSubscribedStatusSkipped`,
`TestTaskNotifyPatternFilter`, `TestTaskNotifyDisabledByDefault`,
`TestTaskNotifySettingsCmdDelivers`, `TestTaskNotifyEnvWinsOverSettings`,
`TestStaleEventLandsWithSettingsOnlyConfig`, the `TestNotifyGap*` group in
`tasknotifyband_test.go`.
**Direction of change:** they assert *that a payload is built and handed to a
target*, and they are why the pipeline is trustworthy. §6.0 adds a property none of
them tests: **the delivery's framing**. A sink written exactly as the documented
configuration writes it is unreadable by every line-oriented tool, and **no test in
this family goes red**. A single assertion — *one delivery occupies one line in the
target stream* — would have caught 49 MB of unreadable output at the first
delivery. The verdict changes from "the notify path works" to "the notify path
works **and what it produces is readable by the tools it is meant to be read
with**".

**7.2 `internal/tools/rebuild_versionprobe_test.go` and `rebuild_test.go`.**
`rebuild_versionprobe_test.go:73` sets `GOPI_TASK_NOTIFY_EVENTS=build-failed`;
`rebuild_test.go:127,158` do the same.
**Direction of change:** **this is the series' worked example, and the code shows
it.** `810f19ac` fixed the comparison and left the alarm firing correctly —
`gopiversion.RevisionOfDisplay` is now the single owner of the reading
(`rebuild.go:350-360`), and `versionMatches` refuses rather than prefix-matching a
display (`rebuild.go:259-264`). What the fix did **not** bring is any assertion
about the *alarm's own honesty*: 25 deliveries said `FAILED` while carrying
`exit=0`, and the payload has two shapes depending on whether the version fields
are populated. The verdict should change from "a mismatch was reported" to "a
mismatch was reported **and the payload's own status field agrees with what was
judged**".

**7.3 `internal/agent/crashdump_test.go` — the false-report family** —
`TestBackfill_ReconcileSparesOwnLiveSession`,
`TestBackfill_ReconcileClearsLegacyOrphanDumps`,
`TestBackfill_OrphanLiveWindowAndReconcile`, `TestBackfill_TrueKill9StaysVisible`,
`TestDetectOrphanSessions`.
**Direction of change:** these were written *in response* to `01a091e7-6efc` ("why
does gopi report previous 14 crashes?") and its sibling `01a091ed-5466` ("crash-dump
backfill misreports live/cleanly-ended sessions as crashes — nick-reported, 14
phantom crashes"). Their verdict — a dump is written only for a genuine orphan — is
correct, and this requirement **raises** it: truthfulness of the record is a
requirement, not a feature, because a false report costs a debugging round (his
words: *"误导排查一整回合"*, `t-1788258886`). This family is the model §6.4d should
follow.

**7.4 `internal/gopilog/*_test.go`** — `TestFormatSessionFailedExit` (the
`TERMINATED` rendering from `t-1788258886`), `TestOneLineRuneSafe`,
`TestExtractAllTaskLists_BeginWithoutEnd_RecoversRealFixture68`, and the golden
tests (`log_golden_test.go`, `stats_golden_test.go`).
**Direction of change:** these encode the record half's honesty — a killed session
must not render SUCCESS — and §6.6 shows the half works. The requirement adds the
case they do not cover: **a failure whose session directory does not exist**
(§6.5, 45 rows), where the renderer has nothing to render and silence is the
output. An assertion that an unretrievable session is **reported as
unretrievable** is the guard §6.5 asks for.

**7.5 the observable that would settle §6.3a and §6.8.** No test today can
distinguish "an alarm never fired" from "it fired and was filtered", because the
drop log is only reached when the pipeline runs. A counter of alarms *raised*,
per event name, written wherever the sink is written, is the single addition that
would make the 17 never-delivered alarms measurable — and it would have made §6.8's
missing 15 visible the day `all` was written.

**7.6 `internal/taskreport/gateclaim_test.go` — read here, owned by M1** —
`TestGateClaimRejectsTheMeasuredCase`,
`TestGateClaimFindsTheMotivatingShapeWithoutAnEvidenceSection`,
`TestGateClaimIsSilentOnACleanReport`.
**Direction of change: none, and that is the finding.** This family reads like an
observability promise — a gate checking whether a run's report asserts results it
did not observe (GOPI.md Rule 5, "Evidence (self-filed)"). It is *not* one: it
adjudicates the text of a report, emits no notification, and a run that overstates
its coverage is recorded, never interrupted. **The one surface in this module whose
whole job is "catch the dishonest report" reaches no human at the moment it
matters** — §4.1's shape, except nothing ever promised it would notify. Recorded as
a §6.4-style unstated boundary; M1 to rule on.

## 8. Confidence and limits

| claim | confidence | basis |
|---|---|---|
| the 25 version-mismatch alarms were **true positives** of a real bug | **high** | all 25 payloads read; ledger `01a09abb`/`01a09fd2`; fix `810f19ac`, ancestor of HEAD |
| the sink's volume, histogram, sources and shape (§6.0) | **high** | directly measured; three reading styles disagree (a finding in itself) |
| the missing newline and the resulting unreadability | **high** | `tr`, `jq`, `wc` agree; the writer and the config both explain it |
| 46 alarms in five days, 19 in one six-hour window, no repeat interval | **high** | per-delivery timestamps read; the code has no interval concept |
| `cancelled` absent while 273 rows exist | **high** | two measurements, both given |
| the log-only reconstruction succeeds (§6.6) | **high** | performed; artefacts named |
| the no-trace failure and the session-id/task-id split (§6.5) | **high** | measured; 45 rows |
| `all` misses 15 alarms (§6.8) | **high** | the `all` block read line by line; the constant list enumerated; cross-checked |
| 17 of 19 alarms never delivered (§6.3a) | **medium** | *delivered* count measured; "never fired" is [inferred] — a drop is indistinguishable from a never-fire |
| §4.3 "the set is chosen, not everything" | **medium** | code evidence strong; §6.2c shows the live config contradicts the code's own default |
| §4.1/4.2/4.5 are *recorded* requirements | **high** | operator's own rows and the commits that closed them |

### Inferences, each with its weakest link

- **[inferred] §4.3 — "the operator wants curation, not a firehose."** Weakest
  link: his live configuration is a superset of the default. I infer intent from
  the *code's* curation principle while his *own choice* keeps the bookkeeping
  events on.
- **[inferred] §6.3a — 17 never-delivered alarms are an over-build against C5.**
  Weakest link: drop and never-fire are indistinguishable from the sink, so the
  count of *fired but filtered* alarms is unknown.
- **[inferred] §6.4a — "how many completions a day is too many" was never
  decided.** Weakest link: an absence of a decision is hard to prove from a tree;
  a decision could exist in a conversation I cannot read.
- **[inferred] §6.4d item 2 — the dirty variant is a false positive.** Weakest
  link: I read one dirty delivery and the documented `IsDirty` rule; I did not
  reproduce a dirty build.
- **[inferred] §7.6 — the report-honesty gate reaches no human.** Weakest link: I
  read the test family and its documentation; I did not trace every caller.

### What this document does NOT claim

- It does **not** claim the notify pipeline is unreliable, nor that its alarms are
  false. §4.2 and §6.4d show the opposite for the most important alarm family: it
  fired correctly and produced a filed, fixed bug. **The failure measured here is
  the ratio and the repeat, not the truth.**
- It does **not** claim the volume is wrong. It measures it; §6.2c records that the
  operator configured it.
- It does **not** restate framework-measured facts (worktree state, merge status)
  as evidence.
- It owns no part of `internal/taskreport` (§7.6 is labelled as M1's).

### A correction recorded in this document, deliberately

My first pass at §6.4d concluded that the `build-failed` alarms were **false** — I
had regex-matched the expected short hash against the *wrong token* of the
multi-line version block and got 0 prefix matches. Re-measuring properly (matching
against the 12-char revision the block carries) reversed the conclusion: all 25 are
true positives of a real defect that was filed and fixed. The wrong version would
have written "the alarm lies" into the requirements of a module whose defining
success story is that same alarm telling the truth. **It is recorded here because
it is exactly the failure mode §4.5 is about** — and because the mistake was caused
by the same multi-line-display detail that caused the bug the alarm caught.

### The weakest links in this document as a whole

1. **One host.** Every measurement is `peerbox` at `0ac1f70e`. The signal path is
   per-host configurable, so these figures are *this host's*, not a property of
   gopi. The largest caveat here.
2. **The sink's history is not the operator's history.** **129,488 of the 132,090
   deliveries (98.0 %) were emitted before 2026-09-13**, when the system ran at
   10–50× today's rate (50,063 deliveries in one day). So the aggregate ratio in
   §6.0 is dominated by a period the operator has since quietened. Measuring the
   **last five days only** (2026-09-14 … 09-18, 1,530 deliveries — the same
   script, windowed) gives the current regime:

   | event | count | share |
   |---|---:|---:|
   | `completed` | 479 | 31.31 % |
   | `in_progress` | 431 | 28.17 % |
   | `claimed` | 397 | 25.95 % |
   | `failed` | 116 | 7.58 % |
   | `stale` | 87 | 5.69 % |
   | `build-failed` | 20 | 1.31 % |

   **Bookkeeping is 1,307 of 1,530 — 85.4 %, a ratio of 1 : 5.86.** The shape is
   *unchanged* under a 10× quieter workload: the three machine bookkeeping events
   still dominate, and the alarm that matters is still 1.3 % of the stream. What
   the quiet period does change is §6.3a — `build-failed` (14 of its 40) and `stale`
   (85 of 1,454) are concentrated in these five days, so "never delivered" for the
   other 17 alarms is a statement about the *recent* regime, not about a system
   that has never tried.
3. **"Triageable" is my classification, not the code's.** I grouped `failed` +
   `stale` + `build-failed`. The code has no such notion — no field, constant or
   rule says which events require a person. [inferred; and if that classification
   is the requirement, its absence from the code is itself a §6.4 finding.]
4. **A drop cannot be observed.** Because the drop log fires only when the pipeline
   is reached with a configured target, every "never delivered" count in §6.3a and
   §6.8 is an upper bound on how much the operator is missing, not a measurement of
   it. §7.5 names the observable that would close this.

## The operator-vision constraints (C1–C6), checked against this module

| constraint | verdict for this module |
|---|---|
| **C1 — one static binary, zero deps** | **Holds, and is why this module is affordable.** The notify path is `net/http` + `os/exec` + `encoding/json` (`tasknotify.go:39-55`); the log reader is stdlib-only; crash dumps are `encoding/json`. The webhook knob is the only external surface and is optional (empty = disabled). No plugin, no daemon added. |
| **C2 — the operator controls the whole thing** | **Holds, and is unusually explicit**: one resolver, four layers (flag > env > settings > default), re-read on **every** firing so a running daemon obeys a config change (`tasknotify.go:116-135`). The settings layer exists *because* a restart once silently dropped the env var. He can point the signal anywhere — including at a file, which is what he did. |
| **C3 — a daemon must be simple** | **At risk, and the risk is the alarm catalogue.** The notify *path* is one hook and scales fine. But the catalogue is 19 named alarms, each a branch of policy about what deserves a human. The operator's own words (quoted at `00-method-and-standards.md:407-409`): *"A daemon must be simple. Every branch of logic it carries multiplies its failure surface."* 17 of 19 have never been delivered (§6.3a); `build-failed` — the one that has — fired 46 times in five days with no quiet interval (§6.4d). A simpler daemon would have fewer alarms, and the ones it had would be ratcheted. |
| **C4 — the agent cleans up after itself** | **Implicated, and *violated* in one measured place.** `worktree-orphan` exists precisely for "a dirty worktree no ledger row owns", and its escalation is documented as never silent (`tasknotify.go:160-165`). But the sink is 49 MB of residue in the operator's home that no sweep owns, in a format no tool can read (§6.0) and that no test would notice (`§7.1`). The record half of this module is the one place where gopi's own output accumulates without a bound. |
| **C5 — a small team, limited resources** | **The measured cost is the alarm catalogue and the log stack.** 10,083 lines of `internal/gopilog` (27 files) plus 803 lines of CLI, against 19 alarm events — of which 17 have never reached anyone, and 13 of the 17 belong to one subsystem (`worktree`/`premerge`/`merge-backlog`). The question this constraint asks is whether six event classes and 49 MB of unreadable stream is the smallest thing that makes him feel told. **I cannot answer it; §6.2c says he configured the volume.** |
| **C6 — `why` over `what`** | **This module is where C6 is most visible and most often unwritten.** The alarm constants carry their `why` in comment blocks at the site — an unusually good example (`merge_revert.go:87`, `rebuild.go:21-23`, the bug053 block at `tasknotify.go:78-92`, and `810f19ac`'s own mechanism note). What has **no** written `why`: the *ratio* (§6.4a), the *repeat interval* (§6.4d), and the *newline contract* (§6.4c). Three sentences nobody wrote, and each one cost a measurable defect. |

## What I could not determine

1. **Why a task id sometimes names a filesystem path** (§6.4b). The call explains
   the mechanism; whether a consumer is documented to tolerate it is recorded
   nowhere. **Decision or accident — I could not determine which.**
2. **Whether any of the 17 never-delivered alarms has ever fired and been
   filtered.** The sink cannot distinguish a drop from a never-fire, and the drop
   log fires only when the pipeline is reached with a configured target. The one
   logged drop in `~/.gopi/logs/*.log` is a `cancelled`. **Not measurable from the
   artefacts available to me** — §7.5 names what would settle it.
3. **Why `01a0b2c4-fd05` and its 44 siblings were created without a session.**
   The submit/auto-register path, M1's boundary. I measured only that the
   observability surfaces say nothing about them (§6.5).
4. **Whether the operator has ever asked for a notification-quality bar.** I
   searched `source=human` rows for notification vocabulary (query in my session)
   and found filed bugs about *missing* signals and *false* reports — never about
   *volume* or *repetition*. So §6.4a's "never decided" is an absence of evidence,
   labelled as such rather than as evidence of absence.
5. **What the operator's target does with 50,063 events in a day.** It appends them
   to a file. Whether he ever opens it, or has a reader outside this repository, I
   cannot see — and §6.0's unreadability finding makes "he reads the raw file"
   implausible. **I could not determine the consumption side at all**, a real limit
   on the noise claim: I can measure what is emitted, not what is attended to.
6. **The `night-watch-merger.log` companion file** (101 KB, same directory, last
   written 2026-09-18 07:03) is a second writer in `~/.gopi/notify/` whose owner I
   did not trace. It suggests at least one consumer or producer of this directory
   lives outside the Go tree, which bounds my `internal/`-reading conclusion.

---

## Verification

- **Read at:** `feat/m11-notifications-observability` = `0ac1f70e`, 2026-09-18, host
  `peerbox`. Every `file:line` above was read at that revision. The fix commit cited
  in §4.2 (`810f19ac`) was confirmed an ancestor of that revision with
  `git merge-base --is-ancestor 810f19ac HEAD` → exit 0.
- **Measurements run on this host**, with commands printed in §6.0, §6.2b, §6.3a,
  §6.4d, §6.5, §6.6 and §6.8. The counter used is
  `~/workspace/m11-tools/final.py` (a two-pass reader: `}{`-split and streaming
  `raw_decode`, so the reading disagreement in §6.0 is reproduced rather than
  assumed). Raw output is kept at
  `~/.gopi/sessions/01a0ab12-7cf9-7a4e-87e0-9a1529a718eb/m11/final.txt`.
- **This document changes exactly one file.** No production code, no test, no
  configuration was touched. The counting script and the raw captures live
  **outside** the worktree by design (Rule 30: a run leaves its worktree clean).
