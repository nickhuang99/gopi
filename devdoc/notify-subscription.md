<!-- doc-class: normative -->
# The task-event notify stream — subscription contract

**Audience:** a THIRD-PARTY consumer — a PA loop, an upper-layer agent, an ops
script, a peer node. You are not gopi; you want to know what a task did and
what failed. This document is the contract you code against.

**Status:** the producer has existed for a long time
(`internal/tools/tasknotify.go`). The consumer face — `gopi notify` — is what
this document describes, delivered 2026-10-01 (task
`01a0f42455cc73b9b3e853a6b1f60ca1`).

---

## 1. What you are subscribing to

gopi writes one JSON object per task-lifecycle event. The object is delivered to
the configured targets:

| channel | setting | what it is |
|---|---|---|
| command | `notify.cmd` (`GOPI_TASK_NOTIFY_CMD`) | a shell command run with the JSON payload on **stdin** |
| webhook | `notify.webhook` (`GOPI_TASK_NOTIFY_WEBHOOK`) | an HTTP POST of the same payload |
| filter | `notify.events` (`GOPI_TASK_NOTIFY_EVENTS`) | comma list of event names; `all` = every status plus every synthetic alarm |
| filter | `notify.pattern` (`GOPI_TASK_NOTIFY_PATTERN`) | glob over tags + task id; empty = broadcast |

The operator's own sink is the documented `cat >>` shape, in practice
`awk '{print; print ""}' | tee -a ~/.gopi/notify/task-events.jsonl > /dev/null`
— a fan-out that writes a local copy and discards the pipeline's stdout.

**Nothing here is a daemon, a broker, or a queue.** The stream is a file (or an
HTTP endpoint) that a producer appends to. There is no subscription handshake
and no delivery acknowledgement. Design accordingly: you are a POLLER, or you
are the thing at the end of `notify.cmd` itself.

## 2. Where the events live

`gopi notify sink` prints the resolved path **and which channel decided it**:

```
$ gopi notify sink
notify sink: /home/user/.gopi/notify/task-events.jsonl
decided by:  notify.cmd
notify.cmd:  awk '{print; print ""}' | tee -a /home/user/.gopi/notify/task-events.jsonl > /dev/null
note: the path above was RECOGNISED inside notify.cmd, not configured as a path — pass --sink PATH to be authoritative.
```

The three values of `decided by` are the provenance you need:

- `flag` — you passed `--sink PATH`. Authoritative.
- `notify.cmd` — the path was **recognised** inside the configured command. It
  recognises the redirect shapes (`cmd >> FILE`, `tee -a FILE`, `1>> FILE`,
  quoted paths) and stops at an `ssh`/`scp`/`rsync` token, because what follows
  is the *peer's* command line and a path on another host is not this host's
  stream. A `tee -a LOCAL > /dev/null` fan-out resolves to the LOCAL file.
- `(none)` with a refusal message — no local file is named (a webhook, an ssh
  leg, an empty command). **Pass `--sink PATH`.** The verb refuses rather than
  guessing: a consumer reading the wrong file reports "no events" forever.

## 3. The three subscription routes

Pick by what your consumer IS.

### Route A — fan-out in the sink command (you need events as they land)

Add your own leg to `notify.cmd`. The command is arbitrary shell, so this costs
no gopi code:

```json
"notify": {
  "cmd": "tee -a ~/.gopi/notify/task-events.jsonl | ssh peer \"cat >> /srv/notify/events.jsonl\""
}
```

**Use it when** the consumer must see an event the moment it happens and can
tolerate a slow sink delaying the ledger writeback (set `notify.async=true` if it
cannot). **Do not use it** for a consumer that must not lose events across its
own restart: a pipe has no memory.

### Route B — `gopi notify` with a cursor (you are a loop) — RECOMMENDED

```bash
gopi notify events --cursor ~/.gopi/notify/feed.cursor --follow
```

Reads the sink through the reader that tolerates **both framings** and skips a
damaged record instead of stopping at it, and resumes from the cursor so a
restart neither duplicates nor loses. **Use it when** your consumer is a loop
that must survive its own restart. `--follow` is a FOREGROUND poll (the
`tail -f` shape) — the process is yours and `^C` ends it. Nothing starts it for
you; nothing restarts it. That is deliberate (PROJECT.md Rule 35): a resident
watcher is a dependency, and a polling loop you own is not.

### Route C — read the file directly

Fine, if you obey the two rules in §4. **Do not** read it line-by-line.

## 4. Two properties of the sink that will break a naive reader

Both are measured facts about the operator's real sink, not hypotheticals.

### 4.1 It is not line-oriented

Before the producer-side framing fix (commit `e2b7e379`, 2026-09-20) each event
was written with **no trailing newline**, so consecutive objects were
concatenated. The operator's 51 MB sink reports `wc -l` = **1**. A reader that
splits on `\n` sees ONE record for the whole file and reports zero events for
everything before the fix — a silent, total loss that looks like success.

A post-fix sink is newline-framed. The file on disk is the **union of both
eras**, because the fix changed the producer and not the file. A reader must
handle both, and should report which framing each record had (the `Line` field
in `internal/notifyfeed`; `gopi notify --json` emits the raw bytes, so the
framing is visible in whether the line ends with a brace or a newline).

**This is why `jq -c . file | wc -l` appeared to work**: `jq`'s stream parser
splits a concatenated stream back into values, so the wrong reader produced the
right count and the defect survived. The count was right; the reader was still
wrong for any tool that did not happen to have a stream parser.

### 4.2 One record is structurally damaged — and a strict reader stops there

The sink carries a record whose nested, escaped probe payload (a `zombie`
evidence chain) swallowed the tail of the following object — measured
2026-09-12, on the row `t-z-dedup`. A strict JSON stream decoder reaches byte
**33,935,609** and aborts, having read **92,134** records out of ~135,000.

The sink is append-only history that nobody may rewrite, so there is no repair
available — only tolerance. `gopi notify` skips the damaged span, **prints a
diagnostic on stderr naming the offset and the byte count**, and continues with
every record after it. Measured: 135,385 records from the same file.

```bash
$ gopi notify events --json 2>/dev/null | wc -l
135385
$ gopi notify failed >/dev/null   # diagnostics go to stderr
notify: skipped 5545 byte(s) at offset 33935609 (undecodable): invalid character ...
```

**A consumer that cannot see its own gap is the failure mode this exists to
prevent.** If your reader stops at the damage, at least say so.

## 5. The record schema

One JSON object per event, `json.Marshal`ed, delivered on stdin.

```json
{"event":"failed","previous_status":"in_progress","task_id":"01a0f439-...","status":"failed",
 "summary":"...","result":"...","reason":"task failed","session_id":"...","model":"...",
 "tags":"a,b","source":"agent","claimed_by":"peer1:2411198","elapsed_seconds":6072,
 "tokens":0,"created_at":1790797832,"updated_at":1790803904,"timestamp":"2026-10-01T05:31:44+08:00"}
```

| field | meaning |
|---|---|
| `event` | the event name (see §6). **Your filter key.** |
| `previous_status` / `status` | the transition. Equal for a re-notification of the same state. |
| `task_id` | the ledger row — **for the transition events only**. See §6.1. |
| `summary` | the row's summary at the time. |
| `result` | the row's result digest (the failure text, when failed). |
| `reason` | why the transition fired ("work started", "task failed", "zombie disposal …"). |
| `session_id`, `model`, `tokens`, `elapsed_seconds` | the run that produced the transition. |
| `tags`, `source`, `claimed_by` | the row's tags, its origin (`agent`/`human`/`auto`), and the claimant (`host:pid`). |
| `created_at`, `updated_at` | unix seconds, ledger-side. |
| `timestamp` | RFC3339 — **the delivery time**. Use this for age windows. |

**Unknown fields must be ignored, not rejected.** The schema grows: a synthetic
alarm carries its own evidence fields, and a field added by a future producer
reaches you without notice. Decode into a map or a struct with the fields you
need.

### 6. The six status events

`pending` → `claimed` → `in_progress` → one of `completed` / `failed` /
`cancelled`. Each fires `event` equal to the NEW status, with `task_id` a real
ledger row.

### 6.1 The synthetic alarms (where `task_id` is NOT a ledger row)

These fire outside the status machine and their `task_id` field is **overloaded**.
Read this table before keying on `task_id`:

| event | what it reports | what `task_id` actually holds |
|---|---|---|
| `stale` | a row whose heartbeat stalled | the ledger row |
| `build-failed` | the declared build failed after a merge (Rule 12) | **the REPO PATH**, not a row |
| `merge-reverted` | a merge was rolled back | the repo or row id |
| `loop-stuck`, `loop-stuck-toolful`, `loop-stuck-observation` | an agent run repeating itself | **the SESSION id** |
| `session-wallclock-exceeded` | a run past its wall clock | **the SESSION id** |

A consumer that assumes `task_id` is always a row will look up a repo path in a
task ledger and report "row not found" as a failure. Check `event` first.

## 7. Failure semantics — what you must handle

| situation | what the stream does | what a consumer must do |
|---|---|---|
| **A sink that does not exist yet** | `gopi notify` reads it as EMPTY and exits 0 | treat "no events" and "no sink" identically — both mean "nothing has happened yet" |
| **An unreadable sink** (permissions) | exits 1 with the path and the OS error | **never** treat an error as empty. A consumer that does must be able to tell "nothing happened" from "I could not look" |
| **A record that never finishes** (a live producer mid-append) | reported as `Truncated` on stderr; the offset is NOT advanced past it | re-read from the same cursor next tick — the record is still arriving, and nothing is lost |
| **A damaged record** | skipped, reported on stderr with offset + byte count | decide your own policy: count the gap, alert, or ignore — but **do not let it silently end your read** |
| **A rotated or truncated sink** | `--cursor` refuses with `cursor does not describe this sink` | re-read from the start (delete the cursor) and reconcile the duplicates yourself; a silently mis-resumed read reports another stream's bytes as events |
| **A cursor carried to a different sink** | refused, naming both paths | a configuration mistake — fix the path, do not delete the cursor |

## 8. The failure report, and what it deliberately cannot see

```bash
$ gopi notify failed --sink /path/events.jsonl
notify: 1 id(s) whose latest terminal event in this stream is a failure
  01a0f439-3655-7418-bece-ae30707e78e8	failed	2026-10-01T05:29:48+08:00	...	probe: deliberate failure ...
notify: this is the STREAM's count, deliberately not the ledger's. ...
```

The fold: scan in sink order, remember each id's last terminal event, **clear
the entry when a later `completed`/`verified` for that id arrives**. That is the
whole predicate, and it is weaker than the ledger's on purpose.

**Two things the stream cannot see, both measured:**

1. **`resolved` tags and successor rows fire NO event.** An operator retires a
   failure by tagging the row `resolved` or by filing a replacement row — both
   are ledger-side facts, and neither produces a notify payload. The ledger's
   own predicate (`internal/tools.FailedNeedsReissue` /
   `taskBugPopulationWhere`) counts those as handled; a stream-only consumer
   cannot.
2. **Most of the stream's failure ids are not ledger rows at all.** Measured on
   the operator's sink 2026-10-01: of **11,685** ids whose last terminal event is
   a failure, **11,508** never appear in the `tasks` table — years of smoke and
   fixture runs. The ledger's own count of failed rows is **290**, of which 175
   also end-failed in the stream.

So the report names itself an id count, not a row count, and **says all of this
in its own output** rather than in a doc nobody reads. If you need the
authoritative failed set, ask the ledger (`gopi task list`). If you are a peer
or a forwarded sink and have no ledger, this is what you can prove.

## 9. Recipes

```bash
# Everything that has happened since the last tick, as machine-readable lines.
gopi notify events --json --cursor ~/.gopi/notify/feed.cursor

# A live loop (foreground; ^C to stop).
gopi notify events --cursor ~/.gopi/notify/feed.cursor --follow

# Everything that failed in the last two hours.
gopi notify failed --since 2h

# One row's whole history, oldest first.
gopi notify events --task 01a0f439-3655-7418-bece-ae30707e78e8

# Only the events you care about.
gopi notify events --event failed,stale --event build-failed

# Which file, and how that was decided.
gopi notify sink
```

`--limit 0` means "all"; a bare `gopi notify failed` shows the newest 20 and
**prints how many it hid**, so a truncated report is never mistaken for a short
one.

## 10. Why this shape and not another (the design record)

**Why a CLI verb and not a tool?** Rule 3 fixes the tool set at 17; a verb is
the established extension point (`doccheck`, `scratch-gc`, `sandbox`).

**Why a library plus a thin verb?** `internal/notifyfeed` owns the framing, the
resynchronisation and the cursor; `cmd/agent/cli_notify.go` parses arguments and
prints. One implementation of the hard part, so the verb and any future consumer
cannot disagree — the `gopi doccheck` → `internal/docindex` shape.

**Why a byte offset plus a record identity, and not a timestamp?** The sink is
append-ordered, so an offset is exact. Timestamps are not monotonic across a
clock adjustment and are not unique, so a timestamp-based resume either
duplicates or skips at the boundary. The identity is the second half: an offset
alone cannot tell you the file it points into is no longer the file you were
reading.

**Why refuse rather than guess the sink?** Recognising a path inside an arbitrary
shell command is a derivation, and a wrong derivation produces a consumer that
reads the wrong file and reports "no events" forever. Explicit is better than
wrong — hence `--sink` wins, `decided by` is printed, and an unresolvable command
is refused by name.

**Why no watcher, no timer, no service?** Rule 35: an artifact that outlives your
reason for adding it is a liability, and a resident reader also makes "who read
that?" unanswerable. `--follow` is a foreground loop the caller owns.

## 11. Verification of this contract

| claim | how it is checked |
|---|---|
| both framings read | `go test ./internal/notifyfeed/ -run 'TestConcatStreamIsReadWithoutNewlines\|TestFramedStreamIsReadAndMarkedAsFramed\|TestMixedEraSinkReadsBothShapes'` |
| damage costs only itself | `-run TestDamagedRecordIsSkippedAndTheRestRecovered` |
| a truncated tail is not damage | `-run TestTruncatedTailIsNotDamage` |
| resume neither duplicates nor loses | `-run 'TestReadIsResumableAtAnyBoundary\|TestCursorRoundTripsAndResumes'` |
| a rotated sink is refused | `-run 'TestCursorDetectsTruncation\|TestCursorDetectsARewrittenRecord\|TestCursorRefusesAForeignSink'` |
| the recogniser refuses a remote path | `-run TestSinkFromNotifyCmdRefusesWhatItCannotKnow` |
| the verb's end-to-end contract | `GOPI_BIN=$PWD/bin/gopi go test ./cmd/agent/ -run TestNotify -count=1` |
| the damage is real | `gopi notify events --sink ~/.gopi/notify/task-events.jsonl 2>&1 >/dev/null \| head -1` on any sink carrying the 2026-09-12 record |

The red proofs — each property failing when its mechanism is removed — are
recorded in the run's report under `## Evidence (self-filed)`, at
`/home/user/.gopi/sessions/01a0f424-55cc-73b9-b3e8-53a6b1f60ca1/task-report.md` (a
session-dir artifact, not a repo file: it is the evidence of one run, and the
contract above is the durable half).
