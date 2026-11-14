<!-- doc-class: normative -->
# Task Report: Root Cause of the Useless Auto-Submitted Task `t-1788546774` (`[auto] worker`)

## TL;DR

`t-1788546774` was **NOT submitted by any LLM task_submit**. It is the
**harness auto-registration row of a bare `gopi` engine run** whose entire
stdin/prompt was the single word `worker`. The row `(source=auto,
reason="task finished", session_id = 1bb51189-…, no parent, no wrapper, no
run_cmd)` is the fingerprint of `maybeRegisterSessionTask` in
`internal/agent/taskregister.go` — every non-resume gopi run registers itself
as a ledger row on start and flips it to completed on end. The "useless task"
is therefore not a task-queue pollution from the LLM submit path; it is an
**audit side-effect of a degenerate interactive/probe run** that was given no
real instruction.

## Evidence Trail

### 1. The task row is a session self-registration, not a submission

`gopi task show t-1788546774`:

```
summary:        [auto] worker          ← prefix = autoTaskSummaryPrefix ("[auto] ")
session_log:    ~/.gopi/sessions/1bb51189-…jsonl   ← session_id == its own run
source:         auto
submitted_by:   harness auto-registration (row IS the run: session …)
reason:         task finished          ← finishSessionTask's terminal flip
parent_task_id: —                      ← NOT a task-run child (no GOPI_TASK_WRAPPER_ID)
run_method:     — / run_cmd: —         ← never passed through runTaskRunOnce stamping
created 02:32:54 → ended 02:33:17      elapsed=22.8s iter=3 tokens=38398
```

Compare with a genuine worker child (`t-1788546733`, same minute): it has
`source=auto`, `run_method=child`, `run_cmd=gopi worker (claim→run round,
target t-1788545050)`, and `parent` tag — because the child is spawned with
`GOPI_TASK_WRAPPER_ID` and runTaskRunOnce stamps provenance. The mystery row
has NONE of those, so it was never a dispatched task.

### 2. The session JSONL shows a degenerate no-instruction run

`~/.gopi/sessions/1bb51189-8f8d-4413-889f-0378a4037d04.jsonl` (14 lines):

- `session_start` 02:32:54, workdir `/home/user/work/GoPi`
- first user message: `[EPIC GOAL]\nworker\n\nBased on this Epic, Establish the [TASK_LIST_BEGIN] plan…`
- 3 iterations: the LLM called only `todo` once, then twice asked
  *"Hi! I'm GoPi, ready to help. What would you like to work on?"* /
  *"there's no specific task or question provided yet"*
- `session_end` 02:33:17 exit 0 — the end_turn-without-tools guard
  (`maxConsecutiveEndTurnRounds=2`) let it exit cleanly.

The model correctly identified there was NO task. Nothing to do, no tools, exit 0.

### 3. Who launched `gopi` with stdin `worker`?

Exhaustive checks ruled out every automated launcher:

- **No wrapper env / no parent** → not `task run` / worker / serve dispatch
  (all spawn via `exec.Command(gopiExecutable())` with
  `GOPI_TASK_WRAPPER_ID` set and a composed `[Task summary]/[Briefing payload]`
  briefing — see `runTaskRunOnce` cli_tasks.go:1162).
- **No concurrent gopi session ran a spawn** — scanned every session JSONL
  with bash/job_start/task_submit tool calls in the 02:32:45–02:33:10 window
  (R3 machinery review 5ae7787b, R4 prompt review 82aa95ca, zombie-orphan-fix
  a3268146, stale-task root-cause 951afaac): none executes `gopi` or echoes
  `worker`.
- **No cron, no PA/scheduler submit.sh** (that tool writes to a separate
  scheduler pool, and its rows carry `pa-queue` tags / external session_id='').
- Only **one** row in the whole ledger has summary `[auto] worker` → unique.
- The run read piped stdin (readInput: non-TTY stdin → whole stdin becomes
  the prompt), producing the bare `[EPIC GOAL]\nworker` framing.

Conclusion: the launcher was **manual / an external terminal tool feeding the
word `worker` into a bare `gopi`** — most plausibly a human (the operator) or the PA
probing the worker mode (`echo worker | gopi`-style), at 02:32:54 while R4 was
running. The run itself was harmless and self-terminated in 22.8 s; its only
"damage" is that the harness minted a **useless ledger row** from a
non-task input.

## Why gopi "submits" such tasks — the mechanism

The design intent (devdoc/task-autoregister-impl.md) is: *every gopi run is a
task* — the ledger records each session for auditability. The start hook
`maybeRegisterSessionTask(briefing)` (taskregister.go:99) inserts an
`in_progress` row with:

```go
summary := autoTaskSummaryPrefix + normalizeTaskSummary(briefing)  // "[auto] " + first 200 chars
```

The ONLY guard is emptiness (taskregister.go:107):

```go
if strings.TrimSpace(summary) == strings.TrimSpace(autoTaskSummaryPrefix) {
    return // empty briefing — nothing meaningful to register
}
```

`"worker"` is non-empty → a row is minted. There is **no semantic-content
gate**: any single word, junk, or degenerate prompt fed to a bare `gopi` run
becomes a "task" row that a worker/`task run next` could later claim and burn
a full context budget on. (In THIS case the row flipped to completed on its
own within seconds — but a degenerate in_progress row that crashes instead of
exiting would sit as a zombie for the stale sweep.)

## Required-fields comparison: LLM task_submit vs auto-register bypass

| Field | `task_submit` (LLM submit path) | auto-register (harness, row IS the run) |
|---|---|---|
| summary | **required** — `task_submit: summary must not be empty` | **[auto] + briefing**, any non-empty string accepted |
| payload | optional but validated (trim) | n/a (never set; briefing = stdin/prompt) |
| repo | verified before INSERT (or cwd default) | n/a |
| source | validated ∈ {llm,human,auto} | always `auto` |
| parent_task_id | must exist if set | optional wrapper tag (env-driven) |
| status | pending (dispatched later) | in_progress immediately, completed/failed at end hook |
| semantic gate | summary is the *headline a human wrote* | **none** — briefing text taken verbatim |

The auto-register hook deliberately bypasses every "is this a real task?"
check because it is an *audit* layer (bug056: summary immutable, stats ride
the audit trail). The design never intended auto rows to be *dispatched* —
and in practice a normal auto row is NOT claimable: it starts
`in_progress` (TaskNewReturningID inserts `taskStatusInProgress`), so
`TaskNextFiltered`/`task run next` (which walk only `status=pending`) skip
it, and its end hook flips it to completed/failed within the run. The real
residual risks are (a) a degenerate auto run that **crashes** mid-way leaves
an `in_progress` zombie for the stale sweep (row noise), and (b) an auto row
that is **explicitly** `task run <id>`'d or re-enqueued can be dispatched on
a meaningless one-word briefing — the summary column is the only text the
child would get.

## Potential fixes (analysis, not yet implemented)

1. **Content gate in the register hook** — reject degenerate briefings
   (single token < N chars, or no whitespace and length < 15, or matches a
   known non-task wordlist like {worker, hi, hello, test}). But a hard
   wordlist is fragile; prefer a **minimum-meaningfulness heuristic**:
   register only if the briefing has ≥ 2 tokens or ≥ ~20 chars of real
   instruction. `"worker"` (1 token) would be skipped.
2. **Prevent degenerate auto rows from becoming dispatchable** — the
   register-hook content gate (fix 1) is the primary defense. As a second
   layer, `task run <id>` on an auto-audit row could warn when the briefing
   is a single token (advisory, never a hard refusal — the operator may
   legitimately want to resume a crashed auto run). The stale sweep already
   covers crashed in_progress zombies.
3. **Task-run briefing guard already exists for dispatch** (taskRunBriefing
   refuses both-empty summary+payload — the e3ca35ca fix). But it does not
   catch a **one-word** briefing. Consider an upper bound on "meaningless
   one-liner" dispatches, or require the child dispatch to come from a
   row with source llm/human (real task_submit) — never from an auto-audit
   row.
4. **Summary+payload composition at dispatch is already correct**
   (taskRunBriefing emits `[Task summary]` AND `[Briefing payload]`
   headers — the child gets the FULL text from both fields, so an
   explanation in only one of them is never lost). This part of the
   briefing's concern is already solved by b4f6609/feat-task-show-briefing.
   The remaining hole is purely the **degenerate-input → row** path.

## Verification

- Build: not needed (investigation-only; no code change in this pass).
- Evidence: session JSONL 1bb51189 (14 lines), DB row t-1788546774,
  taskregister.go:99-150 (start hook + single emptiness guard),
  main.go readInput (stdin → prompt), cli_tasks.go:966-993 (taskRunBriefing),
  internal/tools/tasks.go:2449-2534 (TaskSubmit validation).
- Commit: none (root-cause analysis report only).

## Not Done

- No code fix landed; the proposed fixes above are analysis awaiting the operator's
  pick. Recommended minimal fix: (1) register-hook content gate (skip
  degenerate one-token briefings like "worker") — it directly prevents the
  useless-row class at the source.
- The launcher of the bare `worker` run was not identified with certainty
  (manual/terminal probe most likely; all automated gopi launchers ruled
  out). If the probe was `gopi worker` mis-typed as a piped stdin run, the
  content gate makes such slips invisible to the ledger regardless.
