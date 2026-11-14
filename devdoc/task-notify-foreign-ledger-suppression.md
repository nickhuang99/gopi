<!-- doc-class: normative -->
# Task-notify: a foreign ledger's events are not delivered to this profile's sink

**Mechanism**: `internal/tools/tasknotify.go` — `taskNotifyDelivers` (the gate),
`deliverTaskNotify` (its single call site), `taskNotifyOriginLedger` (the criterion).

## 1. The defect, as measured

gopi resolves the **ledger** and the **notify target** through two INDEPENDENT
channels:

| channel | resolved by | rank chain |
|---|---|---|
| ledger | `config.LedgerPath` | `--db` > `GOPI_MEMORY_DB` > `GOPI_HIPPO_DB` > settings `memory_db` > `<gopi dir>/memory.db` |
| notify target | `config.ResolveNotify` | env `GOPI_TASK_NOTIFY_*` > settings `notify` in the **profile root** (`$GOPI_HOME/.gopi`, else `$HOME/.gopi`) |

So a run that isolates **only** its ledger — a probe, a `--db` CLI call, a red-proof
fixture, a worker started with `--db` — still resolves the OPERATOR's
`notify.cmd`. Every lifecycle event it emits is then broadcast into the operator's
console: chat slots plus `~/.gopi/notify/task-events.jsonl`.

Red, on the pre-fix binary (this ground, 2026-11-04):

```
$ GOPI_MEMORY_DB=<evidence>/red-1793793096/private.db gopi task update <id> --status in_progress
$ GOPI_MEMORY_DB=<evidence>/red-1793793096/private.db gopi task update <id> --status completed

sink BEFORE bytes=6154449 sha=b8f65093…6d6e0
sink AFTER  bytes=6155358  delta=909
```

and the appended bytes:

```json
{"event":"in_progress","previous_status":"pending","task_id":"01a1a666-7d19-7bc2-8c7f-3804ebc5fe42","ledger":"…/red-1793793096/private.db","status":"in_progress","summary":"RED probe parent A",…}
{"event":"completed","previous_status":"in_progress","task_id":"01a1a666-7d19-7bc2-8c7f-3804ebc5fe42","ledger":"…/red-1793793096/private.db","status":"completed","summary":"RED probe parent A",…}
```

The `ledger` field was **already present** — a prior change
(`devdoc/notify-event-origin-ledger.md`) stamped foreign events so they were
*traceable*. Traceability is not truth: the reader still saw a row arrive as
"work in progress" that exists in no ledger they can open.

## 2. Why it is structural, not an accident

Every red-proof run of every row writes its own ledger. Measured on a sibling
row's session, `redproof/` held `g2`, `g3`, `g5`, `g6`, `r1`, `r2` — six ledgers,
each broadcasting its own group of invented rows ("parent A" / "parent B"). The
cost is therefore **one false group per lane per red-proof round**, paid out of
the operator's trust in a channel whose whole value is that a line in it means
real work moved.

## 3. The criterion is a PROPERTY, never a path

```
taskNotifyOriginLedger()  == ""   ⇒  this ledger IS the profile's own  ⇒ deliver
taskNotifyOriginLedger()  != ""   ⇒  foreign ledger                    ⇒ suppress
```

It reuses the ONE resolver (`memoryDBPath()` vs `defaultMemoryDBPath()`, Rule
2/14) — no second ranking, no filename match, no path prefix. A directory named
`redproof` is irrelevant; a file that simply is not the profile's `memory.db` is
not. `TestTaskNotifyGateIsTheOriginProperty` pins this with a row whose path
*looks* like a probe but IS the profile's own: a name-matching gate would
silence it, and that arm fails.

## 4. The gate sits at the ONE convergence point

`deliverTaskNotify` is reached by every producer:

- `notifyTaskTransitionDecorate` (the ledger transition path);
- `NotifySyntheticEvent` (`taskheartbeat.go` — the session-level alarms);
- the memory-sync legs (`memory_sync_delta.go` ×2, `memory_sync_claim.go`).

A single check there covers all of them, and a future call site cannot forget it.

## 5. Default = suppression, and why no knob

A private-ledger event reaching another profile's sink is a **false alarm about
work nobody dispatched**. The safe direction must be the default, not a flag
somebody remembers to pass (Rule 35): a knob would leave every future probe, test
and `--db` invocation one forgotten flag away from the same leak.

A run that *wants* its own events broadcast isolates its **profile**
(`GOPI_HOME`) — and then its ledger IS its profile's own, the gate passes it, and
the events land in the sandbox's own sink. That is the sandbox-correct shape and
it keeps working byte for byte.

## 6. Rule 15: the suppression is reported

```
[task-notify] event "completed" for task <id> NOT delivered: emitted from ledger
<path>, which is not this profile's own (<profile>/memory.db) — its events belong
to that ledger's own sink; to broadcast them, isolate the profile (GOPI_HOME) so
its ledger becomes the profile's own
```

Without it, "no notification" and "the notify pipeline is broken" are
indistinguishable to whoever is hunting a missing event.

## 7. What is NOT changed

- `notify.events` is untouched — it remains its own documented delivery filter.
- Nothing is deleted from `notify/task-events.jsonl`; suppression prevents new
  writes, it never rewrites history.
- The event is still **recorded**: the ledger write and its `task_events` audit
  row happen exactly as before. Only delivery to another profile's sink is
  withheld — measured: the private ledger's row reads
  `status: completed, reason: task finished` after suppression went in.
- No new knob, no new persistent dependency, no per-test flag.

## 8. Fixture consequences (the part a later reader will hit)

A test that wants an isolated ledger used to write
`GOPI_MEMORY_DB=<temp>` and leave the profile ambient. That is precisely the
shape the gate suppresses, so such a fixture now tests the gate instead of the
behaviour it names. The correct shape is a profile that OWNS the ledger, and it
is a helper so nobody re-spells it (`internal/tools/tasknotify_test.go`):

- `taskNotifyProfileSandbox(t)` — fresh `GOPI_HOME`, ledger overrides masked;
- `taskNotifyProfileLedger(t)` — fresh `GOPI_HOME` **and** rank-2
  `GOPI_MEMORY_DB` pointing at that profile's own `memory.db`.

**The rank-2 detail is the trap.** The suite entry point pins `GOPI_MEMORY_DB`
(`testutil.PinSuiteLedgerEnv`), and an env override outranks the profile default,
so setting `GOPI_HOME` alone leaves the ledger on the suite's file while the
profile moves — still foreign, still suppressed, and the failure looks unrelated
to the test's subject. Both halves must name the same file.

## 9. Verification — the A/B on real binaries

Both arms ran the SAME CLI calls on the SAME ground; only the binary differed.

**(a) Foreign ledger, pre-fix binary `gopi-prefix` — RED, the sink grew.**

```
$ GOPI_MEMORY_DB=<evidence>/red-1793793096/private.db gopi-prefix task update <id> --status in_progress
$ GOPI_MEMORY_DB=<evidence>/red-1793793096/private.db gopi-prefix task update <id> --status completed
sink BEFORE bytes=6154449  sha=b8f65093…6d6e0
sink AFTER  bytes=6155358  sha=…            delta=909   (2 events appended)
```

**(b) Foreign ledger, fixed binary — GREEN, the sink is byte-identical.**

```
$ GOPI_MEMORY_DB=<evidence>/green-1793793858/private.db gopi-fixed task update <id> --status in_progress
$ GOPI_MEMORY_DB=<evidence>/green-1793793858/private.db gopi-fixed task update <id> --status completed
sink BEFORE bytes=6165233  sha=66cd6d33…2ec329
sink AFTER  bytes=6165233  sha=66cd6d33…2ec329
DELTA=0   SHA_SAME=YES
[task-notify] event "in_progress" for task 01a1a672-1cb9-… NOT delivered: emitted from
ledger <evidence>/green-1793793858/private.db, which is not this profile's own
(/home/user/.gopi/memory.db) — …
[task-notify] event "completed"  for task 01a1a672-1cb9-… NOT delivered: … (same reason)
```

The two lines are the Rule-15 report, and the count of them (2) equals the count
of events the pre-fix run appended (2) — the gate acted on exactly what leaked.

**(c) The sandbox-correct shape still delivers — no regression.**

```
SB=$(gopi tool sandbox create --label notifygate --task-id <row> | tail -1)   # /home/user/workspace/gopi-sandbox/sandbox-notifygate-713afd10
GOPI_HOME="$SB" GOPI_SETTINGS="$SB/cfg.json" gopi-fixed task update <id> --status in_progress
GOPI_HOME="$SB" GOPI_SETTINGS="$SB/cfg.json" gopi-fixed task update <id> --status completed
$SB/sink.jsonl → 2 lines: event=in_progress ×1, event=completed ×1 ; "NOT delivered" count = 0
```

**(d) The control that pins "property, not path": the SAME sandbox, with
`GOPI_MEMORY_DB` explicitly naming that profile's own ledger — still delivered.**

```
GOPI_HOME="$SB" GOPI_MEMORY_DB="$SB/.gopi/memory.db" GOPI_SETTINGS="$SB/cfg.json" gopi-fixed task update <id> --status completed
$SB/sink.jsonl → 4 lines total (2 from (c) + 2 here) ; "NOT delivered" count = 0
```

An explicit `GOPI_MEMORY_DB` that happens to NAME the profile's own ledger is not
foreign, and the gate passes it. That is what makes this a property gate rather
than a "did the caller set an env var" gate.

**(e) Suite** — `go test ./internal/tools/ -run 'TaskNotify|Notify|TaskUpdate|TaskStale|MemorySync|TaskIDWhitespace' -count=1` → `ok … 2.355s`;
`CGO_ENABLED=0 go build ./...` → exit 0.

The private ledger's own record survives all of it (measured on the RED row):
`status: completed`, `reason: task finished` — the events are PRODUCED and
RECORDED as before; only the broadcast is withheld.
