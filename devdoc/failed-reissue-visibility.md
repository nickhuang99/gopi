<!-- doc-class: historical -->
# failed-needs-reissue — a `failed` row with no successor can no longer be silently forgotten

<!-- WORKER-OWNERSHIP: OPERATOR-ONLY — devdoc/worker-ownership.md -->

**Status:** implemented on `task/01a0f3cf7d3c7b9fb17744228c1365c8`, base commit
`fc86efdb33f500d04cc28a65b17e8983d008b7d2`.
**Mechanism:** `internal/tools/taskreissue.go` (the ONE predicate).
**Census generator:** `scripts/gopi-stats.sh` (the `failed-needs-reissue:` row
under `== ledger & knowledge base (~/.gopi) ==`).

## The defect

A row that reaches `failed` is **terminal by design**: `taskAllowedTransitions`
gives it no outgoing edge, and a reissue is a **manual act** (`gopi task submit`
/ `TaskVerifyToReopen`). Nothing ever asks for that act. So a failure leaves the
system and is never mentioned again — the operator's own framing, in the brief
this work answers: *"a failed row is SILENT forever"*.

Measured on the live ledger at base commit `fc86efdb` (`scripts/gopi-stats.sh`,
row `failed-needs-reissue`): **223 of the 289 `failed` rows** had no successor
row and no `resolved` tag — 100 `auto`, 41 `llm`, 31 `human`, 22 `server`, 20
`agent`, 9 `merger`. The brief's own count (`107 human/agent/llm`) is the
subset that should never have been silent at all; the mechanism counts the
whole population, because an `auto` row's failure is a debt too.

## What was added (and what was deliberately not)

**The ONE mechanism** — `internal/tools/taskreissue.go`:

| piece | what it is |
|---|---|
| `FailedNeedsReissue(ctx, db)` | the query: `status='failed'` ∧ no successor row (no `parent_task_id` names it) ∧ no `resolved` tag, newest first |
| `FailedReissueNote(rows)` | the rendered count line — and `""` at zero |
| `SweepFailedNeedsReissue()` | the resident half: open the env-resolved ledger, count, fire the event **once per process**, return the line |
| `TaskFailedReissueNotifyEvent` | `"failed-needs-reissue"`, registered in `taskNotifyAlarmEvents` |

Two conditions decide "handled", and neither is a status change — which is why
this needed no schema change and no transition:

- **a successor exists** — a row carries it as `parent_task_id` (a reissue or
  rework child, `TaskVerifyToReopen`'s `-rN` row, a dispatch child);
- **`resolved`** — the operator's own "handled, stop counting it" mark, the same
  tag `gopi task archive` already appends. A handled failure is retired by an
  **existing** verb, never by a new one.

Deliberately **not** done: no status value, no edit to `taskAllowedTransitions`,
no auto-retry, no auto-reissue, no new process/script/cron/systemd unit
(Rule 35 — the leg rides the tick the worker loop already fires), and no write
of any kind to a row. The 289 existing rows are read, never modified.

## Where it surfaces (three, all pre-existing)

**① `gopi task list`** — `TaskList` appends `FailedReissueNote`. It rides the
**raw** render rather than the `TaskListVisible` funnel on purpose: the debt is
a property of the *ledger*, not of a row, so a filtered view (`--status
completed`) shows it exactly where it is most useful — the operator looking at
finished work is the person who can reissue the thing that did not finish.

**② the resident task-lifecycle sweep** — `cmd/agent/task_lifecycle_tick.go`
gained a fifth leg that prints the line on the pass's own log, carried by the
worker loop that already fires `runTaskLifecycleTick` (`gopi worker`). The
`gopi merger` daemon this file's own header names as the second carrier is
**retired** (`merger: retired — … use the one-shot `gopi merge` face`), so on a
host running no worker the leg goes quiet with it — see *Honest coverage*.

**③ the notify sink** — `NotifySyntheticEvent(TaskFailedReissueNotifyEvent, …)`
reaches whatever `GOPI_TASK_NOTIFY_CMD` / webhook the operator configured,
delivering the count **and the ids** as the payload's `summary`.

### Dedup, and why it is not a re-arm bump

The notify fires **once per process** (`failedReissueNotifyOnce`). The stale
sweep's precedent is an `updated_at` re-arm bump, and it is deliberately **not**
reused here: a `failed` row's `updated_at` is the timestamp of the **alert that
delivered its failure**, and rewriting it to dedup a *reader* would destroy the
record of when the failure happened — the same class of defect the zombie
sweep documents for a bump that "would rewrite the delivered alert's
timestamp". A process-local latch costs nothing and mutates nothing.

## Evidence (self-filed — every figure below was observed, none typed)

### RED, both arms, on the pre-change tree

The test file is dropped onto a pristine `git archive fc86efdb` tree and run
against it:

```
$ GOPI_MEMORY_DB= go test ./internal/tools/ -run 'TestFailedReissue|TestFailedNeedsReissue' -count=1
# github.com/nick/gopi/internal/tools [github.com/nick/gopi/internal/tools.test]
internal/tools/taskreissue_test.go:110:15: undefined: FailedNeedsReissue
internal/tools/taskreissue_test.go:129:10: undefined: FailedReissueNote
internal/tools/taskreissue_test.go:130:29: undefined: TaskFailedReissuePrefix
… 
FAIL	github.com/nick/gopi/internal/tools [build failed]
```

The rendered surface, on a binary built from `fc86efdb` over the real ledger:

```
$ ./gopi-pre task list --status failed --db fixture.db | head -3
223 task(s) with status=failed:
ordered newest first (created_at DESC); …
#01a0f235-0041-7d3a-9589-1e3bd17f384c  [failed]  [auto] 拍一张截图，…  (model deepseek-flash, source server, created 2026-09-30 20:05:54) [via auto]

$ ./gopi-pre task list --status failed --db fixture.db | grep -c "failed-needs-reissue"
0
```

```
$ grep -c "failed-needs-reissue" cmd/agent/task_lifecycle_tick.go internal/tools/tasknotify.go
cmd/agent/task_lifecycle_tick.go:0
internal/tools/tasknotify.go:0
```

### GREEN, the same arms after the change

```
$ GOPI_MEMORY_DB= go test ./internal/tools/ -run 'TestFailedReissue|TestFailedNeedsReissue' -count=1
ok  	github.com/nick/gopi/internal/tools	0.352s
```

```
$ gopi task list --status failed --db fixture2.db | grep failed-needs-reissue
failed-needs-reissue: 223 failed row(s), no successor row and no "resolved" tag — reissue or tag them; newest first: 01a0f235-0041-7d3a-9589-1e3bd17f384c 01a0ef8a-ec07-7ef0-8eb9-86fed6994677 01a0ef60-804b-7eef-92d6-a014bc15c944 01a0ef60-681b-78fa-a30c-596c40316c40 01a0eeed-1805-7080-aa49-4819c43ccbf6 (+218 more)
```

The **unfiltered** view carries it too (observed at the tail of a 3,000-row
render, above the pre-existing `[hidden] 72 archived row(s)…` line).

### The resident leg, executed for real

```
$ GOPI_ALLOW_AGENT_SPAWN=1 GOPI_MEMORY_DB=…/fixture2.db \
  GOPI_TASK_NOTIFY_CMD="cat >> …/reissue-events.jsonl" GOPI_TASK_NOTIFY_EVENTS=all \
  gopi-post worker --once
[gopi] [lifecycle] resident task-lifecycle sweep (…) active on this process; every 10m0s
[worker] [lifecycle] stale reported=0 disposed=0 sink=cat >> …/reissue-events.jsonl
[worker] [failed-needs-reissue] failed-needs-reissue: 224 failed row(s), no successor row and no "resolved" tag — … (+219 more)
```

(the count reads 224 rather than 223 because the run's own worker row was
in_progress at the time and the fixture carries this repo's live ledger; the
sink's payload below is the settled 223.)

The operator's sink received the alarm:

```
$ python3 -c "…"
EVENT  : failed-needs-reissue
STATUS : failed-needs-reissue
SUMMARY: failed-needs-reissue: 223 failed row(s), no successor row and no "resolved" tag — reissue or tag them; newest first: 01a0f235-… 01a0ef8a-… 01a0ef60-… 01a0ef60-… 01a0eeed-… (+218 more)
```

`GOPI_TASK_NOTIFY_EVENTS=all` reached it — which is the registry contract
(`internal/tools/tasknotify.go`), pinned by
`TestCovToolsNotifyAlarmRegistryIsTheSourceOfTruth` and
`TestCovToolsNotifyAllFilterDeliversEveryAlarm`, both re-run green.

## Census (Rule 38)

| figure | value | generator | measured at |
|---|---|---|---|
| `failed-needs-reissue` | **223** | `scripts/gopi-stats.sh` (row `failed-needs-reissue:`), predicate stated in SQL beside `tasks with verify_evidence` | `fc86efdb` (`base commit:` line of the same run) |

Split by `source`, from the same predicate (`auto` 100, `llm` 41, `human` 31,
`server` 22, `agent` 20, `merger` 9 — total 223), against 289 `failed` rows and
exactly **1** with a successor. The mechanism touches no row, so these are the
operator's own pre-existing rows read back.

## Regression guards the tests pin

- `TestFailedNeedsReissueNamesOnlyOrphanFailures` — the orphan IS reported; a
  row **with a named successor is NOT**; a **`resolved`-tagged row is NOT**;
  and once the orphan itself is reissued it **drops out** — the count is a live
  debt, not a historical tally.
- `TestFailedReissueNoteIsOnTheListRender` — the count line is on
  `tools.TaskList`, and an empty ledger renders **no** line.
- The existing `TestTaskListAllAndStatusFilter` was updated for the note (its
  leak check now reads **row** lines, `#`-prefixed, rather than substring-
  matching the whole block). This is a real interaction, recorded rather than
  hidden: the note names ids, so a whole-block substring test sees them.

## Honest coverage (what this does NOT do)

- **No worker alive ⇒ no resident report.** The leg rides the worker loop
  (`runTaskLifecycleTick`). `gopi merge` is one-shot, `gopi merger` is retired,
  and no other resident tick carries the lifecycle pass today. On such a host
  the `gopi task list` arm still shows the debt on every render — the resident
  arm is silent, and that is a gap in *where* it reports, not in whether the
  debt is visible.
- **A `cancelled` row is not counted** (the epic's scope is `failed`).
- **`resolved` is an operator act.** Nothing tags rows automatically; the count
  only falls when a human reissues or adjudicates.
- **`--status pending` plus a pending-view sweep re-renders** the note per
  invocation; it is a read, never a stored flag.
