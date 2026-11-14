<!-- doc-class: normative -->
# The ellipsis report: one line per turn, and the criterion is a cut pasted unit — not the character

**Status:** implemented 2026-10-22 (task `01a16033-dd15-7ba0-9411-9401b400aebd`, branch
`task/01a16033-dd15-7ba0-9411-9401b400aebd`, base `b880d8a`).
**Code:** `cmd/agent/chat_answer_ellipsis.go` (report layer + criterion),
`cmd/agent/chat_answer_ellipsis_test.go` (aggregation and criterion arms),
`cmd/agent/chat_answer_ellipsis_attribution_test.go` (budget of the mixed-copy arm).
**Does not redo:** the ATTRIBUTION fix (model output / renderer cut label /
unattributable) landed in `6a412b9`; this document is about the REPORT SHAPE and the
CRITERION, and it leaves the attribution vocabulary untouched.

## 1. The defect this revision removes — measured, not asserted

The guard's job is to say "an answer body carried U+2026". It said it ONE LINE PER
CHARACTER, capped at eight, plus a remainder line:

```
[gopi] chat answer body: rendered U+2026 at turn 5, rune offset 1016 — NOT altered (report only); source: …: «n/task/01a15b2c-…
… (eight of these) …
[gopi] chat answer body: rendered U+2026: 1 more in this turn, not listed (turn 5)
```

**Nine lines for one round** — the operator's own console, pasted into the ledger
when this work was filed. The answer itself was pushed off the screen. A guard that
buries the artifact to report a defect in the artifact commits the same failure it
was built to catch.

The second defect is the criterion. The report asked "did the character appear?",
which is not the thing that costs the operator anything. `before … after` and
`根本不是 worker 运行……你以为他还在跑` are prose: nothing was lost, nothing had to be
pasted. `task/01a16033-dd15-…` and `/home/user/workspace/01a15bb9-…-r1` are identifiers
whose exact bytes ARE the deliverable, and an ellipsis inside one produces a token no
shell accepts and no reader can complete.

The third defect is the message. It printed a rune offset and a 16-rune flattened
excerpt, so a reader had to reconstruct, from a window that might start mid-token,
what had been cut.

## 2. The report shape now — at most two lines per turn, and every one of them earned

```
<prefix> — HIGH truncated a pasted unit "`origin/task/01a15b2c-…`" (turn 5, rune offset 1016); NOT altered (report only); source: model output (the wire copy carries it)
<prefix> — turn 5: 9 hit(s): 8 HIGH truncated a pasted unit (named 1), 1 LOW prose (no pasted unit cut) (first at rune offset 2066); NOT altered (report only); source: model output (…)
```

* the **detail line** names the KIND of pasted unit and the token's own text, and is
  written at most `chatAnswerEllipsisTruncatedLinesPerTurn` (= 1) time per turn;
* the **summary line** carries every count the turn produced — hits, the truncated
  class with how many were named, the prose class with one locator, undecidable
  tokens, and the display-layer cut-label count when it is non-zero;
* a turn whose ONLY finding is one truncated hit gets no summary: the detail line is
  the whole report (one line, which is the floor, not a failure to reach two).

**Aggregation is a budget on the REPORT, never on the DETECTION.** `chatTurnRenderer.
Ellipses()` still returns every hit; `len(hits)` is in the summary line. A turn that
elides eleven times says `11 hit(s)`. Nothing is dropped (Rule 15), because the count
IS the record and the lines are only what the console can carry.

## 3. The criterion — what makes a hit escalate

A hit is **HIGH** when the ellipsis falls INSIDE a token an operator is expected to
paste; **LOW** (prose) otherwise. Classification reads the token the character sits
in — the maximal run of *pasted-unit runes* around it — and asks for its shape:

| kind | shape | example from the measured body |
|---|---|---|
| `path` | contains `/`, looks like a filesystem path | `/home/user/workspace/01a15b11-…` |
| `branch` | `/`-joined with a ref-vocabulary first segment (`task/`, `feat/`, `fix/`, `chat/`) | `task/01a15b2c-…` |
| `identifier` | a hyphen-separated segment spelled entirely in hex runes, ≥ 7 | `01a16033-dd15-…`, `01a15bb9-…-r1` |
| `command/flag` | opens with `--` | `--parent-task-id=01a1603…` |
| `URL` | contains `://` | `https://example.com/a/very/lo…` |

Three design decisions in that table, each measured on the operator's turn-5 body:

1. **The token walker is bounded by an ASCII character class, not by whitespace.**
   Written Chinese has no spaces, so a whitespace-only walker returned
   `origin/task/01a15b2c-…`。这些分支是在` — the identifier fused with the sentence after
   it. The finding would have quoted a token nobody can complete, which is exactly
   the defect the report exists to name. `chatAnswerEllipsisUnitRune` ends a token at
   any rune outside printable ASCII (U+2026 itself excepted).
2. **The id test is SEGMENT-wise, not whole-token.** `01a15bb9-…-r1` is a task id
   carrying the reap suffix; a whole-token hex test rejected it and the report
   DOWNGRADED a genuinely uncompletable token to "prose" — a silent false negative in
   the one direction that matters.
3. **The shape is read at the TURN'S END, not at detection time.** `task/01a16033-…`
   reaches the seam one fragment before `-r1` does, so a shape read mid-stream sees
   half a token. `flush()` scans the tail and then classifies every hit against the
   complete body.

**A stated limit, left in on purpose:** an English word spelled entirely from `a`–`f`
(`deadcafe`) classifies as an id. The cost is one extra detail line; the cost of the
opposite error was a truncated task id reported as prose, which is why the false
positive is the direction this trades toward.

**When the body ends inside the token**, its shape is unknowable. The hit is counted
in its own bucket (`a token this seam cannot recover (the body ends inside it)`),
never folded into the prose count — an unknown named, not filled in (Rule 15).

## 4. Red → green, with the real bodies

Generator (Rule 38): both arms below stream a body recorded in session
`b9936006-5e63-4668-8be1-e08501bcbe21` (`session.jsonl`, the assistant record carrying
9 U+2026) through the SAME `chatTurnRenderer` in 7-byte fragments, and print
`reportChatAnswerEllipses`'s line count.

The harmful body, same data, before vs after:

| | report lines | shape |
|---|---|---|
| before (`b880d8a`) | **9** | 8 per-hit lines + `1 more in this turn, not listed` |
| after | **2** | 1 detail line (`HIGH truncated a pasted unit`) + 1 count line |

The harmless body (a recorded answer whose two ellipses are the `……` of a Chinese
rhetorical pause):

| | report lines | shape |
|---|---|---|
| before | **2** | 2 per-hit lines — both reported as findings |
| after 2026-10-22 | **1** | `0 HIGH … , 2 LOW prose (no pasted unit cut)` |
| after 2026-11-09 (this narrowing) | **0** | nothing at all — the LOW budget is zero LINES, not zero detail lines |

So: the low-severity class does not disappear (it is still recorded on every hit), it
stops pretending to be a finding — and then it stopped being a CONSOLE LINE either.
The second narrowing is the subject of §7: the summary line the 2026-10-22 revision
kept was still one line per turn on the operator's band, for a character that cost him
nothing to read past, with no switch and no silence condition.

And the 9-line turn becomes 2 lines while still stating `9 hit(s)`.

## 5. What stays untouched

* **Attribution** (`model output` / `renderer cut label` / `unattributable`,
  `devdoc/chat-answer-ellipsis-attribution.md`) — unchanged, still decided once per
  turn by `attributeChatAnswerElision`, and still the word every line ends with.
* **report-never-substitute** — the detector still never rewrites a byte. The test
  `TestChatAnswerEllipsisIsReported_RED` asserts `drawn == wantDrawn` on the same
  assembly, and the classification only READS the body after the turn is closed.
* **ASCII `...` is still not reported** — `go test ./...` is an ordinary token.

## 6. Budget, not a knob

`chatAnswerEllipsisTruncatedLinesPerTurn` is a constant (Rule 1/13), not a setting: a
report whose verbosity a config key can raise is a report that will be raised back to
nine lines by whoever finds it too quiet. The low-severity budget is **zero lines**,
and since 2026-11-09 that is zero LINES rather than zero detail lines — see §7. The
escalation is the criterion's job; the volume is the budget's.

## 7. 2026-11-09 — LOW is silent, not merely unexpanded (task `01a1bd51`)

**The measured defect.** The operator's console carried this, once per turn, for
turns whose ellipses were all prose:

```
[gopi] chat answer body: rendered U+2026 — turn 17: 2 hit(s): 0 HIGH truncated a pasted unit, 2 LOW prose (no pasted unit cut) (first at rune offset 489 of 1343); NOT altered (report only); source: model output (the wire copy carries it)
```

No switch, no silence condition, on the band the answer itself is drawn on. §6
already said "the low-severity budget is zero lines"; the CODE gave LOW the summary
line anyway, because the only early return was "exactly one HIGH and nothing else".
A comment promising a budget the code does not implement is the same defect class as
a devdoc example no test can re-emit.

**The narrowing, as ONE predicate** (`reportChatAnswerEllipses`). A turn speaks to the
operator only when it carries at least one of:

| condition | why it earns a line |
|---|---|
| a **HIGH** hit | the ellipsis is INSIDE a pasted unit — what he was handed cannot be used |
| an **undecidable** hit | the body ended inside the token, so the seam cannot say whether a pasted unit was cut (an unknown is named, never filled in) |
| a **cut label** the renderer drew | the display copy carries a cell the model's text does not; alone it is a display-layer fact, beside a hit it makes the turn unattributable |

Everything else — every prose ellipsis — prints NOTHING. What it does not do is stop
being RECORDED: `chatTurnRenderer.Ellipses()` still returns every hit with its turn,
rune offset, body length and classification, so the record is intact for any caller,
any test, and any future report that finds a question worth the line.

**What changed in the tests, and why it is not a weakening.**

* `TestChatAnswerEllipsisReportAggregatesOneTurn` now asserts BOTH halves: a prose-only
  turn writes zero bytes on the report writer AND on the REPL's notice channel, while
  a turn carrying one cut pasted unit still states `N hit(s)` and its prose count.
  Compression stays, loss still forbidden (Rule 15).
* `TestChatAnswerEllipsisOffsetCountsAcrossTheTurnsMessages` (the "+19" basis guard)
  gained a third fragment with a cut pasted unit: its subject is a line a reader sees,
  and a two-prose-ellipsis turn now has no line to read the anchor off. The basis
  assertion itself is untouched.
* `TestChatRenderedEllipsis_HitBesideCutLabelsIsUnattributable_PER_CHARACTER`'s body
  now cuts a pasted unit, for the same reason: the attribution vocabulary is what that
  arm asserts, and it needs a line.
* Two new minimal arms state this epic outright:
  `TestChatAnswerEllipsisProseOnlyTurnIsSilent` (0 lines) and
  `TestChatAnswerEllipsisCutUnitSpeaksWithItsAnchor` (≥1 line carrying `turn` and
  `rune offset N of M`).

**Live evidence, not a fixture** (`gopi chat --once`, the declared build's own binary):
a prose-only answer (`他站在门口愣了几秒……最后还是把伞递给了我。`) printed the answer with
no report line at all; an answer echoing `task/01a1bd51-32a2-…` printed exactly one
HIGH line naming that token at `rune offset 30 of 182`.

<banner>
kind: normative
why: The two constants this document describes are a BUDGET, not a preference: the
measured defect was nine console lines for one turn, and the fix is one detail line
plus one count line. A later reader who finds the report "too quiet" will be tempted
to raise chatAnswerEllipsisTruncatedLinesPerTurn — that reverses the epic. The
criterion is what makes a hit worth a line; the budget is what keeps the console
readable. Both are constraints, not style.
</banner>
