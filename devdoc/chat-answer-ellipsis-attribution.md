<!-- doc-class: historical -->
# Which copy carries the `…`: the answer's text, or the row the renderer drew

**Status:** implemented 2026-10-22 (task `01a1603f-bad3-74c2-921a-8ff90abcdbca`).
**Code:** `cmd/agent/chat_answer_ellipsis.go` (attribution + wording),
`cmd/agent/chat_stream.go` (`chatTurnRenderer.CutLabels` / `.Turn`),
`internal/lineedit/answer.go` (`writeCutLabel` records into `cutLabels`),
`internal/lineedit/answer_cut_labels.go` (the ledger + `CutLabels()` accessor).
**Supersedes the reading of:** `devdoc/chat-answer-ellipsis-backstop.md` §1 — see
"what that document got right and wrong" below. The mechanism it describes is
still in place; only its ATTRIBUTION was wrong.

## 1. The reported symptom, and why it was worth a second look

The operator's screen showed an answer body carrying `…` where the model's own
wire text was complete. The guard reported, for each such character:

```
[gopi] chat answer body: ellipsis U+2026 at turn 8, rune offset 1325 — the body
was NOT altered; the prompt forbids it: «与 01a16033-dd15-…
```

"the prompt forbids it" is a verdict on the MODEL. The operator's own
counter-argument was exact: *a language model does not know how many columns the
operator's row holds, so a `…` aligned to a row boundary cannot be its own
choice.* A guard that answers a display-layer question with a model verdict cost
a real investigation its first day.

## 2. Measured: which layer produces the character (task 01a1603f)

### 2.1 The render path DOES draw a U+2026 the answer does not contain

`internal/lineedit/answer.go`, the reserved answer band's right edge: a row whose
head ends between two narrow word runes (`cutLabelFor`) spends its last cell on
the product's cut label and pushes the displaced character onto the row below
(`writeCutLabel`). The label is deliberately **absent from `liveText`** — that
field is the ANSWER's source — so from that moment on the screen and the source
are two copies that differ by exactly that cell.

Measured on the real renderer (band width 40, body = 30×`identifier`, streamed in
7-byte fragments — no U+2026 anywhere in the body):

```
U+2026 cells drawn on the band: 8; model text: contains=false
```

Command and full output: `~/.gopi/sessions/01a1603f-bad3-74c2-921a-8ff90abcdbca/redproof/`
(`red-oldcode-silent.log`, `green-newcode.log`); the probe is the generator.

### 2.2 …and the WATCH could not see it, so the guard was silent OR accusing

`chatTurnRenderer.Write` feeds the watch the delta BEFORE either renderer draws
it. That is right for "the model wrote it", and it is blind to every cell the
renderer adds afterwards. Two consequences, both measured:

* a turn whose model text is clean and whose rows were cut → the watch records
  **zero** hits → the guard was **completely silent** while the screen carried 8
  `…` cells;
* a turn that had both (the measured session) → the guard's per-hit word accused
  the model of every character it saw, and the operator could not tell which of
  the `…` on screen was which.

### 2.3 What the wire copy says about a real occurrence — BOTH layers are present

Measured on the session the operator reported (`b9936006-…`): the turn's answer
text as the model's own `assistant` record holds it (`session.jsonl`, line 335)
is 1355 runes and carries **U+2026 at rune offsets 524 and 1306**. The guard's
two hits for that turn were offsets **543** and **1325** — the same two
characters, shifted by a constant **+19** (the seam's own prefix before the
answer's first rune). Cross-checked by substring: the wire text at 1306 reads
``dd15-…（告警刷屏/判据）…``, i.e. **the model itself truncated that task id**;
the operator's screen shows the identical truncation.

So in this session the two layers BOTH apply, and the honest statement is:

| observation | layer | measured |
|---|---|---|
| a `…` at wire offsets 524/1306 | the MODEL's own text | `session.jsonl:335`, `t.count('…') == 2` |
| 8 `…` cells on a band whose body has none | the RENDERER's cut label | the probe in §5 (`contains=false`, `drawn: 8`) |

**Which is why the old verdict was wrong even when it was accidentally right.**
At this sighting the character really was the model's — but the guard had no way
to know that: it never reads the wire copy, and the same code path produces the
same glyph from the cut label. A verdict that is right by luck is a verdict that
will be wrong the next time, and it WAS wrong: on a turn whose text is clean it
printed nothing at all while the screen carried eight cells (§2.2). The report
now names the copy instead of the culprit, and says "unattributable" where the
two local copies cannot settle it.

## 3. The correction: report the fact, then answer "is it in the model's text?"

`chatAnswerEllipsisSource` has three values, and the report prints one of them on
every line (`String()` is the one spelling, Rule 14):

| source | when | what it means |
|---|---|---|
| `model output` | the character is in the text the seam was HANDED, and the renderer drew no cut label this turn | the wire copy carries it; this is the model's own character |
| `renderer cut label` | the renderer drew cut label(s) and the handed text carries none at that place | **DISPLAY-LAYER ONLY — the wire copy does NOT carry it** |
| `unattributable` | the handed text carries a hit AND the renderer drew labels | this seam cannot say which drawn `…` is which — it does not read `raw.log` |

The decision is one function (`attributeChatAnswerElision`), applied once per
turn, and it REPLACES the per-hit word whenever the turn is not a clean
model-only turn: the report never prints a source for one character that it
cannot stand behind. A turn with no hits of its own but with drawn labels still
gets ONE summary line (naming the turn from the caller's counter), because
otherwise a wholly display-layer difference would be invisible — the exact
silence §2.2 measured.

The old close — "the body was NOT altered; **the prompt forbids it**" — is gone.
It survives only as a constant the tests assert its ABSENCE against
(`chatAnswerEllipsisBannedVerdict`), so the wording cannot creep back.

## 4. Where the record lives, and why there

`internal/lineedit`'s `writeCutLabel` — the ONE place the glyph is drawn — calls
`r.cutLabels.add(r.turnIndex, r.liveRow)` (`internal/lineedit/answer.go:3085`). The accessor a caller reads is
`internal/lineedit/answer_cut_labels.go:112` (`AnswerRenderer.CutLabels`). A derived
alternative (re-running `cutLabelFor` over the body at report time) is the second
definition Rule 14 forbids: it can disagree with the paint, and a disagreement here
misattributes the character in the one report a reader trusts.

The ledger is bounded (`cutLabelLedgerCap`) and always COUNTS, so the bound is
never a silent truncation (Rule 15). A record is `{Turn, Row}` — the renderer's
own scrollback row identity, not a screen row: the seam has no screen dump, and
claiming a screen row it did not measure would be a second kind of guess.

## 5. Red evidence

The probe below is a temporary test (kept verbatim at the session path) that
streams a clean body through the REAL turn renderer on the REAL band path and
then calls the REPL's report seam.

* **On the pre-change code** (`git show HEAD:cmd/agent/chat_answer_ellipsis.go`):

```
U+2026 cells drawn on the band: 8; model text: contains=false
lines=0 notice=""
--- FAIL: the rendered copy carries 8 U+2026 cell(s) the model never wrote,
          and the guard reported NOTHING
```

* **On the changed code** (same probe, same body):

```
U+2026 cells drawn on the band: 8; model text: contains=false
lines=1 notice="[gopi] chat answer body: rendered U+2026 — display layer: the
renderer drew 8 cut label(s) on this turn's rows; those cells are U+2026 the
model's output does NOT contain; source: renderer cut label (DISPLAY-LAYER
ONLY — the wire copy does NOT carry it) (turn 4)"
--- PASS
```

The permanent tests are the pair, not the probe:
`cmd/agent/chat_answer_ellipsis_attribution_test.go` (a clean model text with
drawn labels must say "display layer"; a genuine hit beside drawn labels must say
"unattributable" and must NOT print the model word) and
`internal/lineedit/answer_cut_labels_test.go` (the labels the grid SHOWS and the
labels the ledger RECORDS are the same number, on the same rows; an uncut body
records nothing; the cap bounds the list and not the count).

## 6. What this does NOT claim

* It does not claim every `…` the operator has ever seen came from the renderer.
  §2.3 shows a real model-written one in the same session. The point is that the
  guard now distinguishes them instead of asserting one.
* It does not read `raw.log`. The wire copy is the transport's record; a seam that
  opened the session's raw log to answer "is this character in the model's text?"
  would be a second reader of an artifact it does not own. "Unattributable" is the
  honest answer where the two local copies cannot decide it.
* It does not change the drawn bytes, ever. Report only — that contract is
  unchanged from the previous revision and is asserted in the tests.

<banner>
kind: historical
why: This document corrects an attribution that a previous document and a shipped
guard both asserted ("the prompt forbids it"), and records the measured mechanism
that makes the two copies differ. The three-value source vocabulary and the
"report the fact before the verdict" order are constraints a later reader is most
likely to simplify back into a single accusation.
</banner>
