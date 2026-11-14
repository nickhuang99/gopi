<!-- doc-class: normative -->
# The ellipsis report states its BASIS, not just its offset

Task `01a16053` asked why the render-time ellipsis guard's `rune offset` sat a
constant **+19** ahead of the same U+2026 in the turn's wire text (measured in
session `b9936006`). The answer is that the offset was never wrong: the report
named a coordinate and never named the body it was measured on.

## The measurement

`b9936006`'s `raw.log` holds 168 requests. Concatenating every `text_delta`
after a `→ REQUEST` reconstructs each round's wire text exactly, and concatenating
a TURN's rounds reconstructs what the seam was handed:

```
turn 8 = round 65 (  19 runes: "收到，**不合并，另开一条新任务**。")
       + round 66 (1355 runes, U+2026 at 524 / 1306)
→ seam body = 1374 runes, U+2026 at 543 / 1325   ← exactly what the guard printed
```

Both differences are 19, and 19 is the first round's length. Every other turn in
that session reconciles the same way: where a turn is ONE round (turns 1–4, 7,
9–14, 18, 20–22, 24, 28) the guard's offsets match the round's offsets to the
rune; where a turn is several rounds the difference is exactly the runes the
earlier rounds contributed.

**A turn is not a message.** With `--tools`, one turn carries several ReAct
rounds, and `cli_chat.go`'s `OnText` hands every round's text to ONE renderer as
one stream (`chatTurnRenderer.Write` → `noteAnswer`). The report's body therefore
spans the whole turn, while a reader comparing against "the turn's wire text" has
only the last `→ REQUEST` in front of them — because the raw log's `→ REQUEST`
marker delimits ROUNDS.

## The change

`chatAnswerEllipsisHit` carries two more facts, both stamped at `flush()` when the
body is final (stamping them earlier would name a body shorter than the offsets
inside it):

- `BodyRunes` — the length of the body the offset indexes;
- `Messages` — how many assistant messages streamed into it, counted at
  `AnswerBoundary` (the agent's own `OnMessageEnd` seam) and moved only when the
  next message's text actually arrives, so a message that streamed nothing cannot
  inflate the count.

The report gains the anchor unconditionally (`rune offset 543 of 1374`), and gains
a basis clause only when the turn spans more than one message, so the ordinary
single-round report stays as short as it was. Both go inside the DETAIL line — the
one that names a cut pasted unit — because that is the only line that carries a
rune offset, and it is the coordinate a reader compares against the wire.

The MERGE NOTE is part of this doc because the two halves came from different
lines of work. When this branch was merged, master had already replaced the
per-hit report (one line per hit, capped at eight, plus a remainder) with a
TWO-LINE-per-turn budget: one HIGH detail line plus one summary carrying every
count (the measured reason is in `reportChatAnswerEllipses`'s header — nine lines
pushed the answer off the console). This branch's `reportChatAnswerEllipses`
revision had been written against the older per-hit shape, so the conflict was not
"ours or theirs": the rows are master's and the anchor is this branch's. The
resolution is master's line budget + the anchor and basis clause grafted into the
HIGH detail line, and the LOW locator inside the summary carries the same anchor
for the same reason. Lines measured on the merged tree, verbatim, and
re-emittable from the tree (each block names the test that prints it):

`go test ./cmd/agent/ -run TestChatAnswerEllipsisTruncatedUnitIsNamed_RED -v`
— the HIGH detail row, one case of the table:

```
[gopi] chat answer body: rendered U+2026 — HIGH truncated a pasted unit "/home/user/workspace/01a15bb9-…-r1" (turn 2, rune offset 46 of 78); NOT altered (report only); source: model output (the wire copy carries it)
```

`go test ./cmd/agent/ -run TestChatAnswerEllipsisOffsetCountsAcrossTheTurnsMessages -v`
— a two-message turn, whose two rows are the detail line for its cut pasted unit and
the summary carrying every count (the turn's other two U+2026 are prose, so they get
no line of their own):

```
[gopi] chat answer body: rendered U+2026 — HIGH truncated a pasted unit "task/01a16033-dd15-…" (turn 8, rune offset 105 of 124 (across 2 assistant messages: the turn's ReAct rounds stream into ONE answer body, so this offset is NOT an index into the last round's wire text alone)); NOT altered (report only); source: model output (the wire copy carries it)
[gopi] chat answer body: rendered U+2026 — turn 8: 3 hit(s): 1 HIGH truncated a pasted unit (named 1), 2 LOW prose (no pasted unit cut) (first at rune offset 52 of 124 (across 2 assistant messages: the turn's ReAct rounds stream into ONE answer body, so this offset is NOT an index into the last round's wire text alone)); NOT altered (report only); source: model output (the wire copy carries it)
```

**A prose-only turn prints NOTHING** (since 2026-11-09 — the LOW budget is zero LINES,
`reportChatAnswerEllipses`'s silence criterion). That is why this fixture carries a
third fragment with a cut pasted unit: the anchor this document is about has to be
read off a line, and before the narrowing a two-prose-ellipsis turn still emitted the
summary row. The detector's own record is unchanged either way — every hit is still
in `Ellipses()` with its turn, offset and body length.

The first row is a one-message turn (no basis clause — the ordinary case, kept
plain); the second is a two-message turn, where the clause appears. The clause is
built by one function, `chatAnswerEllipsisBasisSuffix`, which both rows call, so
the HIGH row carries it on a multi-message turn too — that call site is asserted
(anchor present) by the table above, and the clause text itself is asserted on the
summary row by the basis test.

An earlier draft of this section quoted a two-message HIGH row captured from a
throwaway test that was then deleted. It is replaced here because an example no
test in the tree can re-emit is a claim with no reader's route to check it — the
same defect class this whole document is about.

Nothing about the drawn bytes changed: the backstop still only reports.

## Why not "fix" the offset instead

Making the offset relative to the last round would make the report's coordinate
system depend on which round a reader happened to look at, and would silently drop
every earlier round's characters from the body — a detector that stops counting is
a detector that lies. Naming the basis makes the existing coordinate refutable
instead: `543 of 1374` can be checked, `543` invited the wrong comparison.

## Check

```bash
go test ./cmd/agent/ -run 'TestChatAnswerEllipsis'
```

`chat_answer_ellipsis_basis_test.go` is the regression guard: a two-message turn
whose offsets only reconcile once the earlier message's text is included — the
smallest sample that can tell the two behaviours apart. A single-message turn
cannot show the difference at all, which is why the pre-fix tests were green. The
red proof is a no-op'd `messageBoundary` (the pre-fix behaviour), which turns
`TestChatAnswerEllipsisOffsetCountsAcrossTheTurnsMessages` red and nothing else.
