<!-- doc-class: normative -->
# Two numbers, one word: the turn axes in `gopi session log --turns`

> Written 2026-11-04 while repairing the turn-count collision in
> `internal/gopilog`. It records a JUDGMENT (which words name which quantity,
> and why each pair is or is not on the same axis) plus the measurements it
> rests on. The fix itself lives in `internal/gopilog/sessionround.go`
> (`sessionTurnBoundaryIs`, `sessionTurnCountGapLine`) and
> `internal/sessionevent/counters.go` (`CounterUnitTurnBoundary`).

## 1. The defect, as measured

`gopi session log e1e22e9f-eee0-4c33-a238-0fca04534ad9 --turns` printed, before
this change:

```
SESSION HUMAN TURNS — e1e22e9f-eee0-4c33-a238-0fca04534ad9 (214)
  HUMAN TURNS = …
  turns = 619  — one TURN = one COMPLETE INPUT to the next …
    the LEDGER's `chats.turn_count` — the count `gopi session show <id>` prints
  ROUNDS = …
  PASSES = 610 …
   TURN     ROUNDS  PROMPT
    621        27  那么你看现在要不要我们把使用这个统计工具啊？…
```

The header said **619**, the column under it ran to **621**, both under the one
word *turn*, and the only sentence separating them was a parenthetical about
`chats.turn_count`. A reader who looked up “turn 621” landed on 619's rows.

The same session through `gopi session replay … --turns` said `turns=621` on
its first line — so two commands disagreed about one session's turn count while
neither was wrong.

## 2. Why both numbers are true

They count different EVENTS, and each event is written by different code:

| figure | where it is written | when it advances |
|---|---|---|
| `chats.turn_count` | `internal/tools/chat.go` (`turn_count = turn_count + 1`, in `ChatRecordTurnOutcome`) | a turn **COMPLETES** |
| turn boundaries | the session log's own turn seams (`SessionTurnBounds`, `TurnBoundary` on an `Iteration`) | a turn **OPENS** |

So for any session that was interrupted, or is still running, the log holds
boundaries the ledger never charged. On the session above the gap is exactly
**2**, and the row's state is `interrupt` — its last two turns opened and never
closed. Neither number is wrong.

**The repair is therefore not to force them equal** (that would make one of
them a lie) — it is to print each under its own word and state the difference in
numbers, which is what the change does:

```
  turns = 619  — one TURN = one COMPLETE INPUT … NOT a round and NOT a loop pass
    the LEDGER's `chats.turn_count` — …
  turn boundaries = 621 — the axis the TURN column numbers along: a boundary is
    recorded each time a turn OPENS, while the ledger's count advances when a
    turn COMPLETES — so the two figures answer different questions and need not
    be equal
    NOT the ledger's turn count (…) and NOT a round
    THE TWO ARE DIFFERENT QUANTITIES: the ledger counted 619 COMPLETED turn(s),
    the log recorded 621 turn boundary(ies) — 2 turn(s) opened and never closed
    (an interrupt or a run still in flight), so `turn N` above and the ledger's
    `turns` agree only on N <= 619
```

The gap paragraph is printed **only when the two numbers differ**, and only when
both were measured: a session that closed every turn it opened carries no
explanation, and an unmeasured ledger count produces no fabricated difference.

**The standing sentence claims NO direction, because the direction is not
constant.** Measured 2026-11-04 on session
`e364e357-a9ea-4210-a0d3-c606c6997f03`: the ledger holds **209** while that log
recorded **34** boundaries — the opposite way round. A first draft of the
boundary sentence said the boundary count "reaches past the ledger's count", and
that sentence was FALSE on this session; it now states only which event each
figure counts, and the gap line — which reads its own two numbers — carries the
comparison:

```
  turn boundaries = 34 — the axis the TURN column numbers along: …
    THE TWO ARE DIFFERENT QUANTITIES: the ledger counted 209 COMPLETED turn(s)
    and this log recorded 34 turn boundary(ies)
```

(No cause is asserted for that direction: the ledger is a per-session counter
that survives resumes and compaction, the log is what this session directory
holds, and the difference between them is not, on this evidence, attributable to
one mechanism. Stating a guess would be the same defect this document repairs.)

## 3. Judgment: does `paste N` share an axis with `turn N`?

**Two different `paste N` spellings exist, and they are on two different axes.**
Measured on session `e1e22e9f-…`:

| spelling | where | what `N` is | evidence |
|---|---|---|---|
| `──── paste 605 · 4727 bytes · 4677 runes · 1 lines · /path ────` | the chat band header (`internal/lineedit/answer.go`, `TurnInfo.Index`) | the **TURN** index | `gopi session replay … --turn 605` prints that turn's prompt, opening “这个我在之前就测试了一次超长的对话…” |
| `[paste 4677: /path source=human size=12B]` | the inline reference in an input row (`internal/lineedit/pasteref.go`, `PasteRef{Runes}`) | the **RUNE COUNT** of the payload | `gopi session replay … --turn 4677` → `turn 4677 is out of range: this session has 621 turn(s)` |

**Verdict:**

1. **`turn N` and the band's `paste N` ARE on the same axis** (both are turn
   indexes). That is consistent, not a collision — but it is not *stated*
   anywhere: the band's own header labels its byte/line figures (`4727 bytes`,
   `4677 runes`) and leaves its leading number unlabelled, so the only unit word
   a reader gets for `605` comes from knowing the form.
2. **`turn N` and the inline `[paste N: …]` are NOT on the same axis** (turn
   index vs rune count). Nothing on that surface says what `N` counts.
3. **The risk is real but small, and it is measured**: over every
   `chat-screen.dump.json` on this host (5 inline references total), **zero** had
   a rune count small enough to also be a plausible turn number, and on the
   session above the two figures for one payload were `605` vs `4677` — an order
   of magnitude apart. So a reader rarely confuses them in practice.
4. **The lookup surfaces ARE distinguishable at the only place that resolves
   them**: `--turn N` is the single resolver, it is 1-based over turn
   boundaries, and an out-of-range value is refused by name with the session's
   own count (`turn 4677 is out of range: this session has 621 turn(s)`). A
   rune count therefore cannot be silently accepted as a turn.

**What was deliberately NOT changed**: the inline reference's spelling. Renaming
`[paste N: …]` to carry a unit word would touch the reference's writers, its
reader (`pasteRefParse`), the tag vocabulary keyed on it, and every test that
asserts the rendering — a change of its own size with no measured failure behind
it (Rule 35: a change needs an observed failure, and none was observed). What
this document does instead is record the axes so the next reader does not have to
re-derive them, and so a future change that *does* tighten the reference knows
which number it is holding.

## 4. The rule this leaves behind

* A figure printed under a word must be the quantity that word names in the
  place it is read (Rule 14). Two quantities may share a word only if they are
  never printed as if they were one number.
* When two printed figures count different events, **say which event each one
  counts, in the output, where the numbers are** (Rule 15) — a reader reconciling
  them from memory is a reader who will get it wrong.
* A gap sentence is printed only for a gap that exists: an explanation attached
  to equal numbers teaches the reader to skip the explanation that matters.

## Verification carried by the change

```
go build ./...                                                    → exit 0
go test ./internal/gopilog/ -run 'TestTurnCountAndTurnBoundaries
  AreNamedSeparately|TestTurnGapSentence|TestTurnColumnNumbers
  AlongTheBoundaryAxis' -count=1                                  → ok
RED PROOF (boundary + gap lines removed from the render)           → FAIL, as intended
restore                                                            → green again
gopi session log e1e22e9f-… --turns                                → both axes printed (§2)
gopi session replay e1e22e9f-… --turns                             → ledger named + gap (§1)
gopi session replay e1e22e9f-… --turn 4677                         → refused by name
```
