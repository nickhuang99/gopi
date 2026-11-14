<!-- doc-class: normative -->
# A notify event names the ledger it came from — the riddle of a row that is in no ledger

Task `01a16516-3630-7d1a-a65e-4089e3c6812b`. Baseline `c9c0534`.
Evidence with raw command/output pairs:
`~/.gopi/sessions/01a16516-3630-7d1a-a65e-4089e3c6812b/evidence.md`.

## 1. The symptom the operator reported

The chat console received lifecycle events for two task ids:

| id | events | in the main ledger? |
|---|---|---|
| `01a16515-dae5-76a2-88c9-c8f5a15a0c37` | `in_progress`, `completed` | no |
| `probe-dead-holder` | `stale` ×4 | no |

`gopi task show <id>` answered `no task with task_id=…` on host-b, and the
operator reported the same on peer1 and hp. So the console held events it could
not act on: no row to confirm, to reopen, or to reissue.

## 2. What actually happened — two INDEPENDENT channels

gopi resolves the **ledger** and the **notify target** through different chains,
and neither knows about the other:

| fact | resolution | where |
|---|---|---|
| which ledger holds the row | `--db` > `GOPI_MEMORY_DB` > `GOPI_HIPPO_DB` > settings `memory_db` > `<gopi dir>/memory.db` | `config.ResolveLedger` (`internal/config/memory_env.go`) |
| where a lifecycle event is delivered | `GOPI_TASK_NOTIFY_*` env > settings `notify` **in the profile root** | `config.ResolveNotify` (`internal/config/config.go`) |

The profile root is `$GOPI_HOME/.gopi` when `GOPI_HOME` is set, else
`$HOME/.gopi` (`config.GopiDir`). So:

```
GOPI_MEMORY_DB=<anything>        → the ROW moves.   The EVENT does not.
GOPI_HOME=<sandbox>              → BOTH move.       Full isolation.
```

**Both producing runs isolated only their ledger.** Measured:

* `01a16515-…` — session `01a16409-78af-…/rule31/` wrote it into
  its own `rule31/ledger.db` and delivered through the operator's
  `notify.cmd` (`close.err`: `[task-notify] cmd ok (stdout: )`).
* `probe-dead-holder` — session `01a16409-9a5b-…/probe/main.go`, a real-run
  probe with `PROBE_LEDGER` in its own directory, whose
  `DetectStaleTasks(30min)` reported one row and delivered four `stale` events
  (`run-postfix.txt`: `[task-notify] cmd ok (stdout: )`).

Neither run lost data and neither corrupted anything. The sink wrote to its own
resolved profile both times — proven by feeding the same payload with
`GOPI_HOME=<sandbox>`: the event lands in the sandbox's own
`notify/task-events.jsonl` and the operator's sink is byte-unchanged.

## 3. So what was the defect — traceability, not leakage

`notify.sh` is profile-correct. It re-derives `PROFILE` from `$GOPI_HOME`
exactly the way `config.GopiDir` does. A caller that sets `GOPI_HOME` is fully
isolated; a caller that moves only `GOPI_MEMORY_DB` is not, and nothing told it.

The event that reached the console therefore named **a task id and nothing
else**. An event from a ledger the console cannot open was byte-indistinguishable
from one it can:

```
task event: event=completed task=01a16515-… status=completed summary=rule31: undelivered ground probe
```

That is the defect. Filtering those events would be wrong — an event from a
legitimately separate profile (a probe, a second node, a scratch run) is still a
fact the operator may want. **What must never happen is an UNTRACEABLE one.**

## 4. The fix — the payload carries its origin ledger

`internal/tools/tasknotify.go`:

* `taskNotifyPayload.Ledger` (`json:"ledger,omitempty"`), populated by
  `taskNotifyOriginLedger()`, which returns `memoryDBPath()` **only when it is
  not** `defaultMemoryDBPath()` (`<gopi dir>/memory.db`), and `""` otherwise.
* `taskNotifyLedgerJSONKey = "ledger"` — the wire spelling as a constant, because
  the struct tag cannot hold one and the shell sink renders that spelling
  (Rule 1b/13).

`scripts/notify/notify.sh` (`build_message`): when `ledger` is present, the
one-line console message gains ` ledger=<path>`; when it is absent the message is
**byte-identical to before**. The ordinary steady state (every worker, every CLI
call, the whole profile's own ledger) adds zero bytes — that is what `omitempty`
buys, and it is pinned by a test.

Why not silently filter or rename: the origin ledger is the fact the operator
actually needs ("open THIS file and the row is there"), and a filtered event is
one the operator cannot even know he lost.

## 5. The locating recipe (unchanged by the fix, and needed for older events)

Older sink records have no `ledger` field, so the historical hunt still looks
like this:

1. **Parse the sink, never `grep` it.** `~/.gopi/notify/task-events.jsonl` is
   concatenated JSON — `wc -l` sees one line. Use `json.JSONDecoder().raw_decode`
   in a loop; a line-based reader under-reports badly.
2. Read the payload's own facts: `task_id`, `timestamp`, `claimed_by`
   (`<host>:<pid>` names the emitting host), `summary`.
3. **Find the producer by its summary string**, not by the id (the id was minted
   in a private ledger): `grep -rl "<summary>" ~/.gopi/sessions/*/`.
4. **Find the private ledger**: `grep -rl "<task id>" ~/.gopi/sessions/*/ --include=*.db`.
5. Open the row: `gopi task show --db <that db> <task id>`.

With the fix, step 1 gives you the ledger directly and steps 3–4 are
confirmation.

## 6. What this doc does NOT claim

* `gopi task show <missing-id>` does **not** exit 0. Measured `EXIT=1` for a full
  uuid, a non-uuid id, a missing ledger and an unreadable ledger. The two shapes
  that make it LOOK like "exit 0, no output" are `| head` (the pipeline's status
  is `head`'s) and `2>/dev/null` (the diagnostic is on stderr). No change was
  made, because changing it would be inventing a defect.
* `GOPI_MEMORY_DB` still does not isolate the notify target. That is the
  documented contract (`CONFIG.md`): a ledger knob moves the ledger. The
  isolation knob is `GOPI_HOME`, and a run that wants both must set it. Stamping
  the event is what makes the difference visible instead of surprising.

## 7. Red proofs

```bash
# the payload stamp
go test ./internal/tools/ -run 'TestTaskNotifyPayloadNamesAForeignLedger|TestTaskNotifyPayloadOmitsTheOwnLedger|TestTaskNotifyLedgerJSONKeyMatchesTheStructTag' -count=1
# remove `Ledger: taskNotifyOriginLedger(),` from the payload build → NamesAForeignLedger FAILS
# with: payload ledger = "", want the emitting ledger "<sandbox>/memory.db"

# the sink's rendering
bash scripts/notify/notify_slot_contract_test.sh     # 7 cases
# remove the ledger fragment from build_message → "FAIL case 7: the message names the ledger
# that emitted the event"; the control arm (no fragment for an own-ledger payload) stays green
```

The second arm of each pair is a CONTROL, not decoration: `OmitsTheOwnLedger`
and the "NO ledger fragment" case both fail if the stamp/render is made
unconditional, which is the shape this fix would take if it were written as
decoration instead of as a finding.

## 8. Backwards compatibility — measured, not assumed

The field is additive and unknown fields are ignored, so an OLD deployed sink
facing a NEW payload still delivers. Measured against this host's deployed copy
(`/home/user/.gopi/notify/notify.sh`, unchanged since 2026-10-13):

```
$ printf '%s' "$NEW_PAYLOAD" | GOPI_HOME="$SB" PATH="$RIG:$PATH" bash /home/user/.gopi/notify/notify.sh
exit=0
task event: event=in_progress task=01a1693d-9760-7e6e-9bde-c1fc397bd257 status=in_progress summary=origin ledger e2e row
```

The message simply lacks the `ledger=` fragment — no error, no dropped delivery.
So merging this change cannot break a host whose sink has not been redeployed;
that host just does not gain the traceability yet.

**That gap is filed as its own bug** — the deployed copy is ten days behind the
repo's (`55143` vs `23449` bytes, missing `refuse_checkout_home`,
`peer_health_write`, `deliver_batch`, `deadline_expired` and five more), and
nothing detects the drift. Task `01a16942-3b77-7a88-840a-dc6de42bbe20`. Do not
"fix" it with a scheduled redeploy (Rule 35); the report there argues the
one-shot verification direction instead.
