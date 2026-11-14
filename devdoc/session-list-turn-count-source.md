<!-- doc-class: historical -->
# `session list` and `session log` count the same turns

**Date:** 2026-10-26 · **Base:** `60eeb2c` · **Worktree:** `~/workspace/01a17537-09fe-71dc-ae7b-7ed8575978be`

## The defect (measured, not inferred)

One session, two surfaces, two numbers:

```
$ gopi session list --mode chat | grep 80c07a7d
80c07a7d-bb6b-48b4-be66-ea1867f403f2  [chat]  human  … (interrupt, model deepseek-flash, repl turns 18, …)

$ gopi session log 80c07a7d-bb6b-48b4-be66-ea1867f403f2
  Turns:      11  (turns (operator rows) — …)
  Passes:     18  (loop passes — session_end total_iterations)
```

Neither number was wrong. `chats.turn_count` is the REPL loop pass count — the
same 18 the log prints as `Passes:` — while `Turns` is the operator-row count
(`gopilog.SessionTurns`, predicate `sessionevent.IsHumanRoundBoundary`), which is
what `session log --turns`, `session replay` and `chat --replay` all count after
the earlier three-surface fix.

So the list face was the last surface still printing the LOOP PASS number beside
a row, wearing a turn-shaped word. Two things were missing there: the measured
turn count, and any way to know it was missing.

## Why the fix is a hook and not an import

`internal/tools` cannot import `internal/gopilog`. Measured, with the import
temporarily in place:

```
$ go build ./internal/tools
package github.com/nick/gopi/internal/tools
	imports github.com/nick/gopi/internal/gopilog from zz_probe_import.go
	imports github.com/nick/gopi/internal/agent from formatter.go
	imports github.com/nick/gopi/internal/hippocampus from hippocampus_agent.go
	imports github.com/nick/gopi/internal/tools: import cycle not allowed
```

The alternatives were worse, and both are named here so a later reader does not
"simplify" the hook away:

- **a second scanner inside `internal/tools`** — the MIRROR Rule 14 forbids. Two
  predicates over one log drift, and each surface stays internally consistent
  while doing it, so the drift is invisible.
- **moving the counter out of `internal/gopilog`** — it would take the definition
  away from the four surfaces that already read it (`session log --turns`,
  `session replay`, `chat --replay`, `session stats`), which is the same defect
  pointing the other way.

So `internal/tools` **declares** the dependency and the binary **injects** the
one implementation:

| file | role |
|---|---|
| `internal/tools/chat_turns.go` | `ChatSessionTurnsReader`, `InstallChatSessionTurnsReader`, the measured/unmeasured rendering |
| `cmd/agent/session_turns_wiring.go` | `sessionTurnsFromLog` = `gopilog.ParseFile` → `gopilog.SessionTurns` |
| `cmd/agent/main.go` | installs it **before** the subcommand chain (the chain is where a group verb returns from `main()`) |
| `cmd/agent/testmain_test.go` | installs the same function, so the suite exercises the measured branch |

## What the two numbers look like now

```
$ gopi session list --mode chat | grep 80c07a7d
… (interrupt, model deepseek-flash, turns 11 (repl turns 18), tok in=… )

$ gopi session log 80c07a7d-bb6b-48b4-be66-ea1867f403f2 | grep '^  Turns:'
  Turns:      11  (turns (operator rows) — one TURN = one OPERATOR row …)
```

The recorded REPL-pass count is **not deleted** — it moves to its own qualified
word, `sessionevent.CounterUnitReplTurn` ("repl turns"), because it is a real,
different quantity (the loop also charges notify-driven and FAILED passes).
Rule 14: one meaning per word; both facts, each under its own.

## The unmeasured branch is stated, never fabricated

`ChatSessionDetail` gained `Turns` / `TurnsKnown` / `TurnsNote`, filled in the
SAME `chatListPartition` that stamps `FilePresent` (one loop, one address — the
presence probe's `resolve` now returns the path it found, so the turn reader
reads the very file presence was decided on, not a second guess at the layout).

Three ways a row's turn count can be unmeasured, each with its own sentence:

| constant | when |
|---|---|
| `ChatTurnsNoCounterNote` | no reader installed in this process |
| `ChatTurnsLogAbsentNote` | the log is not on this machine |
| `ChatTurnsDirUnresolvedNote` | the sessions tree itself could not be resolved |
| `ChatTurnsUnreadablePrefix` + err | the log exists and failed to parse |

An unmeasured row prints the recorded count under `repl turns` **only** — it
never borrows the bare `turns` word, so no reader can mistake a loop pass for the
operator's own rows. A measured ZERO (`TurnsKnown` true, `Turns` 0) is different
from unmeasured, because "the person typed nothing" and "nobody looked" must not
render the same (Rule 15).

## JSON faces

The wire key of the REPL-turn count is `repl_turns` — the SAME name the table
prints, and the constant `sessionevent.JSONKeyReplTurn` (2026-10-26, closing the
other half of the collision: `--json` published `"turns": 18` while every
human-facing surface spends that bare word on the operator's rows). The measured
neighbour rides beside it:

```json
{ "repl_turns": 18, "operator_turns": 11, "operator_turns_known": true,
  "deprecated_keys": { "turns": 18, "replaced_by": "repl_turns",
                       "remove_after": "2026-12-31", "note": "…" } }
```

`operator_turns` is `omitempty` and pairs with `operator_turns_known` (Rule 15a:
an optional outgoing field, and a zero that means "measured none" is
distinguishable from an absent one).

The retired bare key is NOT published beside the live one — that would keep the
collision (two keys, one number, one of them wearing the operator's word). It
survives for the migration window only as a NUMBER nested inside the announced
`deprecated_keys` block, whose `replaced_by` and `remove_after` travel on every
row: a script still reading `.turns` at the top level gets nothing rather than a
number whose meaning it might misread. Removal date:
`sessionevent.DeprecatedJSONKeyTurnsRemoveAfter`.

## Test / verification

| gate | command | result |
|---|---|---|
| build | `go build ./...` | clean |
| target packages | `make test-budget PKGS='./internal/tools ./cmd/agent'` | see the task report |
| the real surfaces | `gopi session list --mode chat` / `gopi session log <full-id>` | 11 and 11 |

The regression test is `cmd/agent/session_list_turns_test.go`:

- `TestSessionListAndLogAgreeOnTurns` builds the SMALLEST fixture that can see
  the defect — a chats row recorded at 18 REPL passes and a session log holding
  11 operator rows — drives the real run functions (`runSessionList`,
  `runLogSubcommand`), and asserts the two surfaces report the same 11 while the
  18 survives under `repl turns`. It fails FIRST if the counter is not installed,
  so it cannot pass by testing the unmeasured branch.
- `TestSessionListStatesAnUnmeasuredTurnCount` pins the other arm: with the
  counter cleared, the row must not borrow the bare turn word and must still
  report the recorded count.

## What is NOT done

- `gopi chat list` (the sibling verb, same renderer) prints the same corrected
  clause because it shares `ChatList` — but no test of its own was added for the
  new word; `chat_test.go`'s existing assertions still pass unchanged.
- `turn_count` itself is still written by `ChatRecordTurn` and still means "REPL
  passes the model finished". Nothing in this change touches the writer; the fix
  is purely at the rendering/reading seam.
