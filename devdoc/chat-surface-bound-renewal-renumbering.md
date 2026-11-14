<!-- doc-class: normative -->
# "budget RENEWED" but the next self-driven turn was numbered 21/20 — the FIXTURE was reading its own fold recap

Task `01a1bd8f-3911-7cfa-9907-2c211abeb35d`. The end-to-end case
`TestChatSurfaceBound_TerminatesAndIsRenewedOnlyByTheOperator` was red at
`cmd/agent/chat_surface_bound_e2e_test.go:1449`:

```
chat_surface_bound_e2e_test.go:1449: the renewed period announced no self-driven turn 1/20
after the renewal, want it announced — the operator's line must open a fresh period whose
first self-driven turn is accepted and numbered 1 of the bound again
```

…while the console had printed `[gopi] budget RENEWED by your own submitted line` and had
announced the renewed period's first turn as `self-driven turn 21/20`.

## The verdict: the convention holds, and the code under test was RIGHT

**A renewal DOES renumber.** `internal/agent/chat_surface.go`'s `ChatSurfaceUserTurn`
zeroes `chatSurface.typed`, `ChatSurfaceTakePending` numbers a turn `l.typed++`, and
`cli_chat.go` prints `chatSelfDrivenTurnNotice` with `turn.Used`. So a period opened by the
operator's own line numbers its first accepted turn **1 of the bound** again. The
production path was measured correct on this tree — the fixed case's own log:

```
PERIODS: period 1 accepted 20 turn(s) numbered 1..20 (wire 1..20) with 1 refusal(s);
         period 2 accepted 20 numbered 1..20 (wire 22..41) with 1 refusal(s)
```

**The `21/20` was a stale notice, not a fresh turn.** `turn.Used` is computed at the TURN
BOUNDARY, and the boundary that hands out turn 21 runs **before** the renewal — the renewal
is the operator's line, which the REPL reads at the boundary AFTER. The console therefore
painted `self-driven turn 21/20` for a turn the seam then REFUSED, and the renewal landed
one boundary later. A two-line sequence in the durable stream is the whole story:
`…turn 20/20…`, `…this is the last self-driven turn of this period (bound 20)…`,
`…budget RENEWED…`. (A refused turn is never `Closing` and never numbered again: the
refusal happens in `chatSurfaceType` before anything is enqueued, and
`ChatSurfaceTakePending` never sees it.)

So there was nothing to fix on the renewal path, and nothing to fix in the assertion: the
convention is "the renewed period's first turn is 1 of the bound", and that is what the code
does.

## The actual defect: the FIXTURE classified rounds by a SUBSTRING, and a fold recap quotes

The mock model decides, per round, whether it is answering a tool result and whether that
result was a refusal:

```go
// before
tail := string(body)
if i := strings.LastIndex(tail, `"role":"user"`); i >= 0 { tail = tail[i:] }
answeredAlready := strings.Contains(tail, "tool_result")
refused := strings.Contains(tail, "at its bound")
```

A chat turn's history is **FOLDED** (`internal/agent` turnfold, commits `fc98e205`,
`26f10fc1`, `80d2f2f4`): the round after a completed turn opens with a `[system] turn
folded` text block carrying that turn's `conclusion:` and its `anchors:` — and the anchors
**quote the tool result verbatim**, including the sentence
`chat_surface action=type refused: this period's self-driven loop is at its bound…`.

⇒ From period 1's refusal onwards, **every** round's tail contains `at its bound` inside an
anchor, so `refused` was `true` forever after. The measured wire trace (the round index is
the mock's own counter):

```
round 42  answeredAlready=true  refused=true    ← the real refusal (a tool_result block)
round 43  answeredAlready=false refused=true    ← the OPERATOR's renewed line "carry on"
(run ends here — the mock answers the period marker to every later round, so period 2 never asks)
```

The operator's line was swallowed as if it were a refusal. The console renewed **correctly**
(the renewal is gated on the ARM in `cli_chat.go`, never on the text — the ruling from task
`01a0e44a`), and then nothing ever drove the renewed period, so the assertion at `:1449`
waited 20 seconds for a `1/20` that no mock request would ever produce.

This is the same trap the production code already documents one layer up: **the renewal must
be keyed on WHICH ARM produced the line, never on the text**. The fixture violated it.

## The fix: read the CONTENT BLOCK KIND, never a substring

```go
answeredAlready, refused := boundRoundDecision(string(body))
```

`boundRoundDecision` looks at the **last content block of the last user message**:

- last block is `"type":"text"` (or there is no tool_result at all) → **an ASK**: the model
  is being handed a line to drive, and an anchor quoting an OLD refusal inside that text is
  a statement about the past, not the present answer;
- last block is `"type":"tool_result"` → this round carries the tool's decision, and it is a
  **refusal only when THAT BLOCK** contains the marker (`strings.Contains(tail[tr:], …)`).

The marker string, the two block-type tags and its own wording are named constants
(`boundRefusalMarker`, `boundToolResultTag`, `boundTextTag` — Rules 1/13).

Minimal repro: `cmd/agent/chat_surface_bound_round_decision_test.go`
(`TestBoundRoundDecisionReadsTheBlockNotTheProse`), four sub-cases over the smallest
faithful wire samples: the renewed line after a recap that quotes the refusal (the
regression), the real refusal block, an acceptance block, and an acceptance block that
follows a recap quoting a refusal. **Red proof**: short-circuiting the function to the
old "text wins" branch turns **3 of the 4** red:

```
--- FAIL: …/the_tool's_own_refusal,_carried_in_the_tool_result_block
    boundRoundDecision = (answeredAlready=false, refused=false), want (true, true)
--- FAIL: …/the_tool's_acceptance,_carried_in_the_tool_result_block
    boundRoundDecision = (answeredAlready=false, refused=false), want (true, false)
--- FAIL: …/an_acceptance_whose_result_text_mentions_nothing,_after_a_recap_quoting_a_refusal
    boundRoundDecision = (answeredAlready=false, refused=false), want (true, false)
```

and restoring the function returns all four to green.

## Why this was worth a document

A red end-to-end case that names "the renewed period announced no 1/20" reads as a defect in
the renewal. It was a defect in the OBSERVER, and the two have opposite fixes: the code path
was correct and the fixture lied. The general shape — **a test that classifies a folded chat
history by substring will eventually read its own recap**, because the recap is designed to
quote the very wording the test is grepping for. Anyone extending this fixture (or writing a
sibling against a folded transcript) should key on block kind or on a structural field, never
on prose that the fold is allowed to carry forward.
