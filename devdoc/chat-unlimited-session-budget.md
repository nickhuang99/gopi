<!-- doc-class: historical -->
# Chat Is Not a Worker: the session budget the chat surface must not inherit

<!-- WORKER-OWNERSHIP: OPERATOR-ONLY — devdoc/worker-ownership.md -->

**Task:** `01a115c4-1ef0-7551-9960-9a5c34f3ed84`
**Date:** 2026-10-07
**Files:** `cmd/agent/chat_session_budget.go` (new),
`cmd/agent/chat_session_budget_test.go` (new), `cmd/agent/cli_chat.go` (one call
site), `CONFIG.md` (two rows).

## The defect, as measured

`gopi chat --tools` sends the **TASK** system prompt — the same assembly a
dispatched worker gets — and that prompt's `[TASK MANAGEMENT RULES]` rule 5
renders the session budget:

```
BUDGET: session budget 60min — a task that cannot finish inside it is TOO BIG by
definition; running out is a planning failure, not a graceful ending; past 75%
split it (task(action=reframe, verdict=TOO_BIG)).
```

A second line, in the tick footer, repeats it:

```
A ticking clock runs from session start; each tool result carries [t+NNNs].
Session budget 60min — a task that cannot finish inside it is TOO BIG by
definition. t+300s = wrong tool: job(action=start) or cut scope.
```

On a **worker** that number is a real planning ceiling: there is an epic to
split, a dispatcher waiting, a queue that needs the row back. On a **chat** it
is a fiction with no referent — and the same prompt says so two paragraphs
earlier, in its own `[CHAT SURFACE]` block:

> the long-lived interactive `gopi chat` console … a turn is conversation, not a
> task run (no task list, no epic framing).

The two blocks contradict each other, and **the budget wins**, because a number
beats a noun in a model's planning. Measured 2026-10-07: a PA chat reached
`t+5527s`, read itself as *"92× over the budget"*, and used that reading to
impose a limit on **itself** and propose ending early — a fabricated deadline
made a chat quit. The operator's ruling, verbatim:

> 你是 chat，根本不是普通的 worker 不存在超时的问题

The knob that actually bounds a chat run is a **different** knob with a different
job: `GOPI_SESSION_MAX_WALLCLOCK` (default 8h), enforced by the engine. The
prompt budget is a number the prompt *teaches*; the wall-clock cap is a boundary
the engine *enforces* — `devdoc/session-budget-prompt.md` records that
distinction and `TestSessionBudgetIsNotTheWallclockCap` pins both inequalities.

## The repair: publish a default, never an enforcement

`cmd/agent/chat_session_budget.go` — one function,
`publishChatSessionBudgetDefault()`, called from `runChatWithInput` at
`cmd/agent/cli_chat.go:2061`:

```go
const chatSessionBudgetEnv       = "GOPI_SESSION_BUDGET"
const chatSessionBudgetUnlimited = "0"
const chatSessionBudgetDefault   = 60 // the reader's own default, named in the warning only
```

* **Unset** (nobody decided) ⇒ write `"0"` and say so on stderr: the variable,
  the value, the reason, and how to set one instead.
* **Set** (`"20"`, `"0"`, anything) ⇒ *nothing happens*. An exported value is
  the operator's own decision about their own surface and it is honoured
  verbatim. That is the difference between a default and an enforcement.
* **The write fails** ⇒ the failure is reported with the variable, the refused
  value and the error, and the sentence says the run keeps the 60-minute ceiling
  (Rule 15). The alternative — silence — leaves the operator with the exact
  symptom the defect was and no way to see it.

`"0"` is already a first-class value end to end, which is why this is a default
and not a mechanism: `internal/agent`'s `sessionBudget()` returns unlimited for
it, `sessionBudgetRule` renders `BUDGET: unlimited — no planning ceiling; still
split anything that cannot finish in one sitting.`, and the tick line drops its
budget clause entirely rather than inventing a `0min` denominator. **No prompt
text, no prompt length, no constant and no reader changed** — the density gate
(`TestSystemPromptDensity`, cap 4500) sees the same 0-budget prompt it always
did.

### Why the call sits where it does

The call is placed after the argv walk and after the `--film` dispatch, so
`gopi chat -h`, a usage refusal and a film run (a *driver*, which serves no turn
of its own) all return above it and change nothing in the environment —
`TestChatHelpChangesNoBudget` reads the source to pin that ordering, which is a
property no single command line can demonstrate at runtime. Placing it at that
one seam also means every chat carrier is covered by construction: the
interactive REPL, `gopi chat --once`, the scripted sequence face and the
pipe-back driver all reach their turns through `runChatWithInput`.

## The tradeoff: why the ENVIRONMENT, not a render-time check in `internal/agent`

The alternative was to teach the prompt assembly "am I a chat?" and render `0`
there. Rejected, for three reasons:

1. **There is no such flag to read at that moment.** The chat prompt is built
   from the *same* `assembleSystemPrompt()` on the tool-enabled path
   (`Agent.ChatOps`), and at the point rule 5 renders, the group is not
   distinguishable: the chat blocks are appended by a different layer than the
   one that renders the budget. "Is this a chat?" would have to become a new
   process-wide signal — i.e. the fix would *add a knob* where it needs a fact.
2. **It would be a second source of truth for a resolved value.** Rule 2 puts
   the budget's single reader in `internal/agent` (`sessionBudget()`); a
   render-time special case would have to disagree with it, which is exactly the
   drift the one-reader rule exists to prevent.
3. **The environment is the operator's VISIBLE channel.** With the default
   published there, `env`, a child process and any `gopi chat` the agent spawns
   all read the same value the prompt was rendered from, and an operator can
   answer "what budget is this chat running with?" without reasoning about which
   code path rendered which sentence.

## The knob's surfaces — what is real, and what was NOT built

`devdoc/session-budget-prompt.md` records how the knob reaches the model and
what it left unfinished: its `## Not Done` list says *"`GOPI_SESSION_BUDGET` is
not yet in `CONFIG.md`"* (item 2) and that the two numbers are not surfaced in
`gopi --help` (item 3). Honest status after this change:

| channel | status | note |
|---|---|---|
| `GOPI_SESSION_BUDGET` (env) | **real** | the single reader is `sessionBudget()`; this task makes the CHAT path fill it in with a default |
| `CONFIG.md` | **added here** | the knob's own report listed it as "not yet in `CONFIG.md`" (`## Not Done` #2 of that devdoc) — this task closes that gap with two rows, `GOPI_SESSION_BUDGET` and its kill switch `GOPI_SESSION_BUDGET_REFUSAL` |
| a `--session-budget` flag | **deliberately NOT built** | see below |

**Why the flag was left unbuilt.** A flag would be a *second* spelling of a
decision the environment already carries, and the only caller who needs it —
an operator setting a budget on their own chat — already has the export. Rule 35
is explicit that an artifact must earn its existence by naming the failure it
closes; there is no observed failure a flag would close that the export does
not. Adding one would also put a chat-shaped knob into the top-level CLI, where
it would then have to be threaded through the pre-verb hoisting machinery
(`config.BootstrapEnvFromArgs`) for no measurable gain. **A knob nobody asked
for is a dependency; the export is not.** The `settings.json` channel is absent
for the same reason.

## Verification (the report's own `## Ran:` lines)

Full build/test/report evidence is committed beside this document at
`tasks/01a115c41ef0755199609a5c34f3ed84-report.md`. The load-bearing measurement
is the **surface**, not a copy of it:

```
# BEFORE this change (binary built from the branch base):
$ ./bin/gopi-pre chat --once --tools "reply with the single word OK and nothing else"
  request body carried: BUDGET: session budget 60min — a task that cannot finish inside it …
  request body carried: Session budget 60min — a task that cannot finish inside it is TOO BIG by definition.

# AFTER (branch tip):
$ ./bin/gopi chat --once --tools "reply with the single word OK and nothing else"
[gopi] chat: GOPI_SESSION_BUDGET unset — publishing GOPI_SESSION_BUDGET=0 (unlimited) for this run;
             the chat surface has no planning ceiling (export GOPI_SESSION_BUDGET=N to set one; …)
  request body carries: BUDGET: unlimited — no planning ceiling; still split anything that cannot finish in one sitting.
  request body carries: A ticking clock runs from session start; each tool result carries [t+NNNs]. t+300s = wrong tool: …
  "60min" occurs 0 times.

# An explicit value is NOT overwritten:
$ GOPI_SESSION_BUDGET=20 ./bin/gopi chat --once --tools "reply with the single word OK"
  request body carries: BUDGET: session budget 20min — …
  (no "unset" notice on stderr — a decided value is not a default)
```

## Tests

`cmd/agent/chat_session_budget_test.go`:

* `TestChatBudgetPublishesUnlimitedWhenUnset` — the variable is genuinely
  *absent* (not empty), the default lands, and the notice names the variable,
  the value and the reason.
* `TestChatBudgetHonoursAnExplicitValue` — `20`, `0`, `1`: each survives
  untouched and **no default is announced** (including `0`, which catches an
  implementation keyed on the *value* instead of on presence).
* `TestChatBudgetPublishedValueIsTheAgentsOwnSpelling` — the join that makes the
  default real: the name, the `=0 means UNLIMITED` contract, the
  `return 0 // 0 = unlimited` spelling and the 60-minute contrast are read out
  of `internal/agent/loop.go`. `cmd/agent` is `package main` and the reader's
  constant is unexported, so the two spellings cannot be tied by the compiler —
  the same shape `internal/config.ChatSurfaceIdentityEnv` uses. A rename in
  `internal/agent` now fails here instead of silently unsetting the default.
* `TestChatOncePromptTeachesNoFabricatedCeiling` — **the real binary**,
  `gopi chat --once --tools`, against the mock provider, reading the sentence
  back off the captured request body: no `BUDGET: session budget 60min`, no
  "60min" anywhere in the request, and the unlimited rule present.
* `TestChatOnceHonoursAnExplicitBudget` — the same surface with
  `GOPI_SESSION_BUDGET=20`: the value reaches the prompt unchanged and the
  unlimited line does *not* appear.
* `TestChatHelpChangesNoBudget` — the placement gate described above.

Rule 17 note: the per-call env flips use `os.Setenv` + `testutil.SnapshotEnv`
(not `t.Setenv`), because the assertions are about *absence* — the hand-rolled
`prev := os.Getenv(k)` restore would come back `""` instead of absent and the
test would pass for the wrong reason.

## What this change deliberately does NOT touch

* **A worker's 60-minute ceiling.** `gopi worker`, a bare prompt run and every
  non-chat entry keep `sessionBudgetMinutes = 60` — that number is what rule 5
  was written for. Rule 28 also puts worker semantics on the system layer, which
  is not this change's business.
* **`GOPI_SESSION_MAX_WALLCLOCK`.** Untouched; a chat's real bound stays the
  engine's 8h enforcement.
* **Any prompt text.** The fix works through the value the prompt already reads.
