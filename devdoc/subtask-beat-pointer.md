<!-- doc-class: historical -->
# The subtask-beat pointer — a pointer, never a payload

**Status:** implemented on `feat/subtask-beat-memory-pointer`. Brief:
`briefings/brief-memory-prefetch-at-subtask-beats.md`.

## The problem

The owner's framing: `memory(action=search)` is a database read, free of token
cost *for the retrieval itself*, so the moment the model is deciding what to do
next is the right moment to remind it that "have we been here before?" is one
call away. The beats named were (a) when the subtask list is issued and (b)
when a subtask is committed/reviewed.

## The two shapes that were rejected

1. **Prefetch → inject.** Running the search for the model and putting hits in
   the context. Rejected: the memory schema's own rule is *"value-gated
   retrieval, not dumb RAG"*, and this pays result tokens on every commit for
   hits the model never asked for. It also silently decides the query — the
   exact judgment the model is supposed to make.
2. **A background prefetch (goroutine/cache).** Rejected by the brief, and
   pointless besides: the model is synchronous, so a result produced in the
   background can only surface as a tool_result in the round the model already
   planned to continue in. `parallelSafeCall` already makes a search emitted
   alongside other read-only calls run concurrently — that is the existing,
   sufficient parallelism.

## What was built

**One appended clause** to the `memory(action=search)` schema description, in
the same imperative voice as the three existing triggers, naming the commit
beat. The original trigger sentence survives byte-identical (asserted).

**One prose pointer in the commit return.**
`nextIncompletePointer(committedTaskID)` scans `TaskState.view()` — the list
the harness already tracks, parsed from the model's own `[TASK_LIST_BEGIN]`
block — for the first item that is not the committed one and not completed, and
renders:

```
Next item 2 "wire the schema clause" is starting — if it looks like something you have done before, memory(action=search) is free and one call away.
```

`commitTaskItem` appends it after the existing budget line (a second,
independent append — the budget line's own shaping is unchanged). The line is
bounded (80-rune title + `…`), whitespace-collapsed to one line, and omitted
entirely when there is no next item, so the commit return is byte-identical to
its old shape in that case.

Why the commit beat specifically: the commit return is the ONE harness-authored
value at a subtask boundary that the model already reads every time — the
budget line established that precedent (`sessionBudgetLine`), and this reuses
it rather than opening anything new.

## Part 3 — the list-issued beat: reported, not built

**CORRECTED at close-out — the paragraph below is too strong; see the
correction at the end of this section.** The list-issued beat carries **no**
memory reminder, and wiring one would need
a **new injection channel** — so it was left alone, per the brief's stop rule.

What is there today: the `task(action=view)` schema description (mechanics
only), the static `[TASK MANAGEMENT RULES]` block (rule 6 mentions
`memory(action=store)` at DONE, nothing about retrieval), and three nudge
constants (`noListYetNudge`, `staleTaskListNudge`, `taskAreaCompletionNudge`)
that reach the model as **user messages** via `Ctx.AddUserMessage` — that is the
new-channel shape. The asymmetry is the reason: the commit beat returns a value
the model reads anyway; the list-issued beat has no such value, only
harness-injected messages.

## Tests, and what each one is for

`internal/agent/subtaskbeat_test.go`:

- pointer present with a next pending item (RED on master);
- pointer absent when the committed item was last, and when no list is tracked
  (the case that rots — an unconditional append fails it);
- return shape unchanged when the budget line is empty (`GOPI_SESSION_BUDGET=0`),
  including the exact suffix and single-space joining;
- **no retrieval**: a `&Agent{}` literal (no session logger, no clock, no
  context, no DB) still renders the pointer — the runtime half — paired with a
  source-shape guard that parses the helper's own declaration and fails on any
  `tools.`/`sql.`/`MemorySearch`/`memoryDBPath` reference in it. The guard fails
  if it cannot FIND the helper, so a rename cannot leave it blind;
- truncation + titleless fallback + alt-field-name resolution;
- schema-clause diff guard (original sentence byte-identical, clause appended
  after it, commit beat named exactly once).

`testsuite/subtaskbeat_e2e_test.go`: the real binary, a scripted mock LLM, and
the pointer read back out of the session JSONL (Rule 31 — the unit tests prove
the line is BUILD correctly, this proves it REACHES the model). It also asserts
the clause is in the first request's wire body, and that the run issued no
`memory(action=search)` call at all.

## Known gap (honest)

The schema clause's **adoption** is not measured here. Rule 11's standard would
mean two real tasks whose only sane route is memory-at-commit, plus a session
audit showing the model searched there. This change ships the persuasion
surface and proves it is on the wire; whether it persuades is a separate,
measurable question.

## Correction — Part 3's "needs a new injection channel" was too strong

Recorded at close-out by the continuing session, so no reader inherits the wrong
conclusion. Two channels already exist for the list-issued beat:

1. **The nudge channel already carries list-beat reminders.** They are
   `noListYetNudge` (loop.go:131), `staleTaskListNudge` (:140) and
   `taskAreaCompletionNudge` (:436), all injected through
   `a.Ctx.AddUserMessage(...)`. That channel is established, capped
   (`maxNudgeCount`, `defaultNudgeRounds`, `maxTaskAreaNudges`) and prefixed
   (`harnessMessagePrefix`). A fourth constant riding it is **a new nudge site,
   not a new channel** — which is what the original finding conflated.
2. **`task(action=view)` returns a harness-authored, model-visible tool
   result** — the same kind of surface `commitTaskItem` decorates in Part 2.

Neither route needs a new injection channel, so the brief's STOP condition did
not actually apply. The list-beat half is dispatched as follow-up
**`01a0ce7e-db42-7671-ab27-e5b9852b7ffc`** (`parent_task_id` = this row) with
these constraints carried verbatim; nothing was built here.
