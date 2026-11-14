<!-- doc-class: historical -->
# The ui_state damage taxonomy — what each class means, and the contradiction it used to print

**Task:** 01a0e49e (re-measure of 01a0e313's report *"the oracle names the wrong failure"*)
**Branch:** `fix/uistate-damage-taxonomy`
**Surface:** `gopi ui_state` (whole-answer `--all`, single-line `--compare`, the
`chat_surface` tool action) + `internal/uiclient`'s verdict/ledger core.

## The defect, as the report measured it

On a real 117×26 pty, `gopi ui_state --target chat --all` printed, for ONE line:

```
line 0 damage=paint_loss grid_row=-1 in_grid=false in_transcript=true
expected_cols=95 expected_runes=95 grid_cols=117 prefix=95
```

`prefix=95` **equals** `expected_runes=95`: the longest verbatim prefix of the line on
the grid was the whole line — the paint carried all of it — while the damage word said
the paint lost it. The verdict contradicted the measurement printed beside it, and sent
the reader to the wrong layer (the paint) when the fact was about the row.

## What was re-measured on master first

`e167bbb9` ("a re-painted row is arrival, not paint_loss") landed after the report and
closed the shape the report was measured on — a row holding the line DOUBLED, whose tail
is the line's own head. Measured on this branch's base (`git archive HEAD`, probe test):

| row shape | master's verdict | reported |
|---|---|---|
| `line+line` (the report's row) | `repaint_tail`, exit 0 | ✅ closed by e167bbb9 |
| `line+line+line` | `in_grid=false prefix=95 expected_runes=95 damage=paint_loss` | ❌ **still contradictory** |
| `line+other line` | `in_grid=false prefix=95 expected_runes=95 damage=paint_loss` | ❌ **still contradictory** |
| 117-cell line the grid carried WRAPPED | `damage=paint_loss` (prefix=58) | ❌ taxonomy defect |
| 179-cell ASCII line the grid carried wrapped | `wrap_artifact_or_loss`, rejoined=0 | ❌ the join was never asked |

Two independent root causes, both fixed here.

**Root cause 1 — the containment test and the prefix diagnostic answer the same question
and were allowed to disagree.** `rowCarriesLine` accepts a row as the line only when the
row's *excess* is the line's own head re-drawn (a re-paint). A row that opens with the
whole line and then continues with anything else therefore failed containment *while*
`sharedRunePrefix` reported the whole line — two measurements of one row, no rule tying
them together. The classifier then asked only the line's width.

**Root cause 2 — the width was a proxy for a fact the grid can be asked directly.** The
wrap classes were reached by `displayWidth(line) > grid.Cols` (and `cjk_wrap`
additionally by "wider in cells than in runes"). A line exactly as wide as the grid, which
the grid provably reassembled, was never given to the wrap instrument, so it fell to
`paint_loss` — the one class that asserts the paint lost the text. A width test answered a
question the grid answers as evidence.

## The fix

1. **One classifier for both faces** (`internal/uiclient/damage.go`,
   `classifyLostLine`), called by the one-line core (`CompareTranscriptLine`) and used by
   the ledger through `CompareVerdict.Damage`. The recorded defect was two faces naming
   the same row two different ways; the class is now decided once.
2. **`damage_to_row`** — the whole line is verbatim at the START of a grid row that then
   continues with text the line does not have. The text ARRIVED and is not exactly alone
   (a stale re-paint, or a second line drawn over the tail). It is recorded and counted
   (`repainted`), the ledger stays **agreed**, exit **0**, and the damage word names the
   ROW.
3. **`text_wrapped`** — the whole line is on the grid continued across consecutive rows.
   The join instrument is ASKED (`compareSequence`) rather than assumed from the width.
   A claim that cannot reproduce its own evidence is not counted as arrival.
4. **`lost_text`** — the ledger's new count is the number of lines whose damage is a LOSS,
   and it is what the whole-answer STATUS is derived from (`auditStatus`). `Disagreed`
   also counts lines whose class could not be confirmed, and using it would let an arrival
   class decide the status as if it were a loss (measured: `outcome=disagreed` beside
   `matched=1 disagreed=0`).
5. **The reason sentence is generated FROM the class** (`lostReason`), so a class and its
   prose cannot drift apart — which is exactly how the reported verdict came to say "the
   paint lost it" next to a covering prefix.

## Every damage class that can now be emitted

| class | meaning | ledger | arrival? |
|---|---|---|---|
| *(empty)* | the line matched, or was reassembled by the wrap instrument (`rejoined`) | matched | arrival |
| `repaint_tail` | the line arrived on ONE row, which continues past it with the line's own head re-drawn (a re-paint that did not clear the tail) | repainted | **arrival** |
| `text_wrapped` | the whole line is on the grid continued across consecutive rows | rejoined + matched | **arrival** |
| `damage_to_row` | the whole line is at a row's start and the row continues with text the line does not have | repainted | **arrival** |
| `paint_loss` | the line FITS the grid and is on no row: no wrap can explain the absence | disagreed + lost_text | loss |
| `wrap_artifact_or_loss` | the line is at least as wide as the grid and the grid did not reassemble it: a wrap explains it as well as a loss | disagreed + lost_text | loss (honest) |
| `cjk_wrap` | as above, and wider in CELLS than in RUNES (double-width arithmetic is the candidate explanation) | disagreed + lost_text | loss (honest) |
| `emphasis_unpainted` | the line arrived but its recorded emphasis never reached the grid | disagreed (EmphasisUnpainted) | arrival of text, loss of formatting |
| `grid_only` | the grid has the line and the transcript does not: a stale frame or another session's screen | disagreed | n/a |
| `neither` | the line is on neither side | disagreed | loss |

`DamageArrived` (`damage.go`) is the ONE predicate that says which side a class is on, so
the counting and the wording ask the same question. `wrapJoinCandidate` names the classes
whose absence the join instrument may answer.

## The exit contract — unchanged

`0` = on the grid AND in the transcript (including every arrival class above);
`3` = disagreement (a lost line, or a row carrying a line the transcript does not have);
`2` = the audit could not run. No exit code moved: `damage_to_row` is recorded and exits 0,
and a lost line still exits 3.

## Honest limits (what this change does NOT claim)

- **A row that carries the line but is not the line is reported as a MISMATCH by
  `--compare` and exit 3 there**, because no single row carries the line exactly. The
  class and the reason sentence carry the reconciliation (`damage_to_row`, "the text is on
  the grid — what is damaged is the ROW"); the STATUS deliberately does not pretend the row
  is the line, because the row really is defective.
- **A grid-wide ASCII wrap whose last piece ends in a blank is still
  `wrap_artifact_or_loss`**, not `text_wrapped`: the join instrument reads a trailing space
  as ambiguous. The class stays honest ("a wrap explains it as well as a loss"), so the
  failure is silence about arrival, never a claim of loss. Measured: 179-cell line ending
  in a letter → rejoined; the same line ending in a blank → not rejoined.
- The `--target ui` face could not be exercised with a live `ui-screen.dump` here (no ui
  dump existed in the profile); it shares the same core and the same `--target` plumbing, so
  the exposure is the code path, not the surface.

## Verification recipes (reusable)

```bash
# 1. The report's exact pair, RED on master, GREEN on the fix — base-compatible API only,
#    so the file COMPILES on the base tree (a compile error is not a RED proof):
git archive HEAD | tar -x -C /dev/shm/redproof && \
  cp internal/uiclient/compare_red_proof_test.go /dev/shm/redproof/internal/uiclient/ && \
  (cd /dev/shm/redproof && go test ./internal/uiclient/ -run TestRedProof -count=1)  # FAIL
go test ./internal/uiclient/ -run TestRedProof -count=1                              # ok

# 2. A REAL profile + REAL pty-painted dump, then the built binary against it (Rule 31):
GOPI_HOME=<profile> <bin>/gopi ui_state --target chat --all --session <id>
GOPI_HOME=<profile> <bin>/gopi ui_state --compare "<line>" --target chat
```

**A row longer than the band is not a grid row.** The production band SPLITS a row wider
than itself across band rows, so a fixture that paints `line+line` for a 95-rune line
(190 cells > 117) does not put the shape on the grid at all — the grid came back holding
only the split's last piece (prefix=0). Shapes asserted through the pty rig must fit the
band; over-wide shapes belong in `internal/uiclient`'s cases, on grids built directly.
