<!-- doc-class: normative -->
# A resumed chat's turn number — display and write-back are two counters

Task: `01a16fb3-7639-767e-8c84-1cd1eda1aed5`.

## The defect

`gopi chat --resume <session>` numbered its turns from **1** while the session's
own log already held **N**. Measured on a real session
(`50ceb6cf-ad95-4659-8120-60035e8a9480`, 19 operator turns by
`gopi session log --turns`): every heading and status row of the resumed run said
"turn 1, turn 2, …" — the session's 20th turn was announced as its 1st, and the
screen and the log disagreed for the rest of the run.

## Root cause — one variable, two meanings

`cmd/agent/cli_chat.go`'s REPL had a single `iterCount` feeding two call sites that
mean different things:

| call site | what it means | unit |
|---|---|---|
| `chatBeginTurn(renderer, iterCount+1, …)` and `turnStatus.begin/publish(…)` | "the turn a reader is looking at" | the operator's own row |
| `session_end`'s `total_iterations` | "how many loop passes THIS process ran" | the agent loop's pass |

A **turn is not a pass**: one turn routinely takes many passes (every tool round
is one), and the loop also charges passes for notify-driven and failed turns. The
two quantities coincide only in the trivial case (one pass per turn), which is why
the defect was invisible in single-round sessions.

## The fix — a baseline field, never a seeded counter

`chatSession` gained `turnBaseline int`: how many turns the continued session
already held, read ONCE at resume time
(`chatResumeTurnBaseline` → `gopilog.SessionTurns`, the same counter
`gopi session log --turns` prints, so the screen and the log cannot define "turn"
differently).

**`iterCount` still starts at 0 and is still what the close-out writes.** The
one-line alternative — seeding `iterCount` with the baseline — was rejected
because it would make every resumed session report N fabricated passes in
`session_end`; the regression test below pins exactly that leg, so it cannot be
reintroduced by accident.

All six display seams go through ONE derivation, `chatSession.chatTurnDisplay(local)`:

```go
sess.chatTurnDisplay(iterCount+1)   // heading of the turn about to run
sess.chatTurnDisplay(iterCount)     // status row after a turn completed (×4)
```

The write side never calls it. The resume also prints the baseline so an operator
can see which numbering they are in:

```
[gopi] chat: this session already holds 3 turn(s) — the next turn is turn 4
```

## Regression guard

`cmd/agent/chat_resume_turn_baseline_test.go`, five cases, red-then-green verified:

- the baseline IS the log's own turn count (asserted through the production call
  shape: `resolveResumeLogPath` then `chatResumeTurnBaseline`);
- no resolvable record → 0 (a fresh numbering, not a refusal);
- the derivation: baseline 19 ⇒ first new turn is 20; baseline 0 ⇒ 1;
- **on the rendered grid**, through `chatBeginTurn` + `publishChatStatus`: the
  heading and the status row both say `turn 20`, and no un-baselined heading
  survives elsewhere on screen;
- **a real resumed run** (`gopi chat --resume <fixture>`, one operator line) whose
  `session_end` `total_iterations` is not inflated by the 3 turns already on disk,
  and whose stderr states the baseline sentence.

The red proof: `chatTurnDisplay` mutated to `return local` turns cases 3 and 4 red
(`= 1, want 20`); restored and re-measured green (content identity against the
pre-mutation snapshot).

## What a future reader should not "simplify"

- **Do not seed `iterCount`.** It is the close-out's pass counter; the baseline is
  a display fact. The two are separate on purpose, and `total_iterations` is what
  catches a violation.
- **Do not inline `+ sess.turnBaseline` at a call site.** There are six; each one
  forgotten numbers half the screen. The method exists for that reason.
- **Do not count turns by user rows yourself.** `gopilog.SessionTurns` is the one
  definition (it excludes harness-injected rows — `[system]` nudges, tool results,
  the thinking-budget notice — which is why a naive user-row count reports 104
  where the log reports 19).
