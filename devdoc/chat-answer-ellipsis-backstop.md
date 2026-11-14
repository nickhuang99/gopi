<!-- doc-class: historical -->
# The chat reply ellipsis: why a prompt-only ban did not hold, and the render-time backstop

**SUPERSEDED IN PART (2026-10-22, task `01a1603f-bad3-74c2-921a-8ff90abcdbca`):**
this document's §1 attributes a rendered `…` to the MODEL. The guard that carried
that wording has been corrected — the band's right-edge cut DRAWS the same glyph
(`writeCutLabel`), so a character in the rendered copy is either the model's text
or the renderer's label, and the report now names WHICH (`model output` /
`renderer cut label` / `unattributable`). See
`devdoc/chat-answer-ellipsis-attribution.md` for the measured evidence and the
new wording. The MECHANISM described below (watch the deltas at the one
`Write` seam, report, never substitute) is unchanged and still correct; only the
attribution and the report's closing clause were wrong.

**SUPERSEDED IN PART (2026-10-22, task `01a16033-dd15-7ba0-9411-9401b400aebd`):** §3's
report shape ("a hit is `{Turn, Offset, Excerpt}`", one LINE per hit, capped at
eight) is what the epic measured as the console noise — nine lines for one round.
The report is now at most one detail line plus one count line per turn, and the
criterion is whether the ellipsis cut a PASTED UNIT rather than whether the
character appeared. The MECHANISM below (watch the deltas at the one `Write` seam,
report, never substitute, hold an incomplete trailing rune) is unchanged. See
`devdoc/chat-answer-ellipsis-aggregation.md`.

**Status:** implemented 2026-10-20 (task `01a15629-3967-7015-93c7-91dae6c6cfd4`).
**Code:** `cmd/agent/chat_answer_ellipsis.go` (detector + report),
`cmd/agent/chat_stream.go` (`chatTurnRenderer.noteAnswer` / `.Ellipses` + the
`BeginTurn` turn index), `cmd/agent/cli_chat.go` (the one emission seam, after
`Finish`).

## 1. What was already there, and what it did

`internal/agent/chat_reply_glyphs.go` carries
`[CHAT REPLY GLYPHS — screen cells are the deliverable] … never elide anything
with an ellipsis`, mounted on both chat paths by `chatContextRulesFor`
(`internal/agent/chat_context.go`). It landed as commit `82605ae57`
(2026-10-14 03:23:33 +0800, `merge: task/01a13657…`, 3 files, +177) —
verified with `git show --stat 82605ae57` in `/home/user/work/gopi`, the main
checkout (the ref is not in this task's worktree, which was cut at `b904d75`).

That commit's reasoning was measured and correct about the ORIGIN of the
character: the ellipsis is the model's own text, not a mark the renderer draws
(`internal/lineedit` paints no such marker). Where it was wrong was in the
INFERENCE — "so the fix belongs in the prompt".

## 2. Why it did not hold — measured, not asserted

**Generator** (Rule 38): `python3` line scan over

```
/home/user/.gopi/sessions/8616b46c-db23-41cb-a657-c3a519abd998/8616b46c-db23-41cb-a657-c3a519abd998.raw.log
```
taking every `→ REQUEST` line and `json.loads`-ing the payload on the next line.

Measured:

| figure | value |
|---|---|
| requests in the session | 166 |
| requests whose payload carries the `CHAT REPLY GLYPHS` paragraph | 165 |
| requests whose assistant HISTORY carries U+2026 | 157 |
| assistant messages carrying U+2026 (cumulative over the history) | 2832 |
| requests whose LATEST assistant message carries U+2026 | 23 |
| first request with U+2026 in its assistant history | #9 |

Independent cross-check on the session's own record
(`session.jsonl`, same directory): 164 assistant records, **24** of them carry
U+2026.

The one request WITHOUT the paragraph is #119 — a round that carried a
`thinking` key (a different request shape), not a round where the mount was
skipped. So the paragraph was not "missing from the prompt"; it was present and
the character was still produced. That is the whole finding: **the mount is not
the mechanism that fails — the instruction is.**

### The mechanism, stated as a mechanism

1. The paragraph constrains a **per-character** property of the output.
2. Adherence to an instruction is a **statistical** property of a probabilistic
   writer, over the whole body; there is no step in the model's own loop that
   checks its text against the rule, and no step downstream that can refuse the
   bytes.
3. So the requirement is satisfied at the rate the writer happens to satisfy it
   — and, crucially, the failure leaves **no trace anyone can act on**: the
   glyph is a character of an answer that looks complete.
4. A rule whose violation is (a) probabilistic and (b) unobservable is a
   preference, not a constraint. The commit made the rule *known*; it did not
   make it *checked*.

The second-order effect is in the table too: 157 of 166 requests had a U+2026
in their assistant history. Once the pattern is in the context window, the
model's own earlier output is a competing example — the prompt says one thing,
the transcript demonstrates another, 2832 times.

## 3. What the backstop is — and what it deliberately is not

The check moves to the render path, where the bytes are already the artifact:

* `chatTurnRenderer.Write` — the ONE seam both renderers share — feeds every
  delta to `chatAnswerEllipsisWatch` **before** either renderer draws it.
  One hook, so the two paths cannot observe different text.
* The detector accumulates the turn's body (a locatable record needs the bytes
  around the hit and an offset from the START of the turn), scans incrementally,
  and **holds an incomplete trailing rune** between deltas: U+2026 is three
  bytes and the chat path is measured at ~1 byte per delta, so a detector that
  decoded each delta alone would miss exactly the case that matters.
* A hit is `{Turn, Offset (rune offset in the body), Excerpt (≤16 runes per
  side, flattened to one line)}`.
* `chatReportAnswerEllipses(input, renderer)` draws the report through
  `chatInput.WriteNotice` — the band in front of an operator, stderr for a
  scripted run — the same channel the rest of the REPL's notices use, because a
  report the operator cannot see is not a report (`chat_diagnostics.go`).
* **It never substitutes.** The drawn bytes are the model's bytes; the test
  asserts this directly (`drawn != wantDrawn` fails).

Deliberate non-goal: the ASCII spelling `...` is **not** reported. It is an
ordinary token (`go test ./...`, `args...`), and a detector that fired on it
would produce the noise that gets a real notice skimmed past. U+2026 has no
legitimate use in an answer body; `...` does.

Cost: one bounded accumulator per turn (the turn's body, which the renderer
already holds fragmentarily), a linear incremental scan, and zero new
dependencies — Rule 3/35: no tool, no config key, no state file.

## 4. Red → green

`cmd/agent/chat_answer_ellipsis_test.go`:

* `TestChatAnswerEllipsisIsReported_RED` — the measured body shape
  (`the file is \`COMMANDS.…`), streamed with the ellipsis SPLIT ACROSS two
  fragments. Asserts: exactly one hit, turn 7, the rune offset, an excerpt that
  quotes both the character and the text before it, one line, and that the
  drawn bytes are unchanged.
* `TestChatAnswerEllipsisIsQuietOnACleanBody_GREEN` — a body containing
  `go test ./...` and no U+2026: zero hits, zero report lines, and the REPL seam
  writes nothing on the notice channel.
* `TestChatAnswerEllipsisReportIsCappedNotDropped` — the cap is on the REPORT,
  never on the detection; the remainder is stated (`3 more in this turn`), so
  the record is never silently incomplete.

Red proof: deleting the one `r.noteAnswer(delta)` call from
`chatTurnRenderer.Write` makes the RED test fail with
`a body carrying one U+2026 produced 0 hits, want 1: []`; restoring it brings
the pair green. The restore is proven by `git diff HEAD -- cmd/agent/chat_stream.go`
holding only the feature's own hunks (no mutation left behind).

## 5. A note for the next reader

The prompt paragraph stays. It is the only channel that can prevent the
character from being WRITTEN; the backstop is the only channel that can observe
it being written. Neither replaces the other, and the backstop is what makes
the rule survive a writer that slips.

<banner>
kind: normative
why: This document is the record of a measured conclusion (a prompt-only fix for
a per-character property was falsified on 2832 assistant messages) and of the
mechanism that replaced it. The design decision — report, never substitute — is
the part a later reader is most likely to "improve"; it is a constraint, not a
style choice.
</banner>
