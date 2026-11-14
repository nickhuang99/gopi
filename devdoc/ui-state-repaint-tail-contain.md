<!-- doc-class: historical -->
# ui_state containment: a re-painted row is arrival, not `paint_loss`

Task `01a0e305-f0b4-7a47-a07a-2d0cf05a2126`. Files: `internal/uiclient/compare.go`,
`internal/uiclient/audit.go`, `cmd/agent/cli_uistate_compare.go`.

## The defect, measured

`gopi ui_state --target chat --all` — the whole-answer audit of gopi's own chat
paint — reported a **faithful CJK answer line** as `damage=paint_loss` with
`exit=3`, i.e. "the paint lost it, re-wrapped it, or it has scrolled out of the
visible grid". The line was on the screen in full.

Reproduced on a real 117x26 armed chat (pre-fix binary, evidence
`evidence/cjk/audit-live.txt`):

```
$ gopi ui_state --target chat --all
exit=3
  status=disagreed geometry=layout cols=117 rows=26 frames=79 transcript_lines=1 audited=1 skipped=0 truncated=false matched=0 disagreed=1
  line 0 damage=paint_loss grid_row=-1 in_grid=false in_transcript=true expected_cols=61 expected_runes=33 grid_cols=117 prefix=33 text="自观测闭环：这一行必须原样落在画布上，verb 自己会把两面对照。"
```

`prefix=33` **equals** `expected_runes=33`: the longest verbatim prefix of the line
found on the grid was the WHOLE line. The audit's own number said so, and the class
said `paint_loss` anyway.

## Root cause (5-why)

1. Why `paint_loss`? — `CompareTranscriptLine` matched the transcript line against
   `ScreenDump.Lines` rows **byte-exactly**; no row was byte-equal to the line.
2. Why was no row equal? — The row *carries* the line plus a tail. Measured row, read
   off the dump (`evidence/cjk/rows-cjk.txt`, row 015):
   `'▌ 自观测闭环：…两面对照。自观测闭环：…自己会把两'`
   — the line (33 runes), then the line's **own head re-drawn** (29 runes).
3. Why is the row shaped like that? — The band re-drew the answer line while the tail
   of the previous paint of that same line was still on the row: a re-paint that
   rewrote cells without clearing what the row had held past the new text. The
   renderer's 117-cell row was 115 cells of content (64 runes), not padding — so the
   tail is a visible paint residue, not a trailing-blank artifact.
4. Why did the row-exact rule not cover it? — Because byte-exact equality offers no
   way to say "the row holds this line AND something else", and the only other
   instrument in the ledger (`compareSequence`) runs **only for over-wide lines**
   (`DamageWrapArtifactOrLoss`/`DamageCJKWrap`). This line is 61 cells in a 117-cell
   grid, so it classified as `paint_loss` and the wrap instrument never ran.
5. Why is that a defect of the rule and not of the renderer? — The containment rule
   exists to answer "did this text arrive?". A row that OPENS with the whole line has
   answered yes. Reporting that as `paint_loss` inverts the diagnosis, and
   `paint_loss` is the one class a reader acts on.

## The fix

`rowCarriesLine(row, line)` (`compare.go`) — one rule with one arithmetic:

* the row's text must **open with the line** (gutters stripped), and
* the remainder must be **a prefix of the line itself** (a re-paint of the line,
  drawn from its own start), or empty (the ordinary row-exact hit).

A row that continues with anything else is **not** this line's row and the verdict
falls through to the existing mismatch path, unchanged.

`CompareVerdict.RowTailRunes` reports the tail: `0` for an exact row, `N` for a
carried row, `-1` when the line is not on a row. The evidence is a number a reader can
re-do, not a sentence to trust.

### The class: `repaint_tail`

The direction the ledger takes is decided by *whose row this is*, not by a byte
equality that a re-paint defeats:

* `matched` — the line arrived on one row that is exactly it, OR on a row that
  carries it plus its own re-drawn head. The status stays `agreed` and the exit code
  stays `0`: the text is on the screen.
* `repainted` — a **separate count** of the second shape. The line is recorded in
  `Ledger.Lines` with `damage=repaint_tail` (not folded silently into `matched`),
  because a re-paint that leaves row residue is a real paint defect the operator may
  want to chase — it is simply not a *loss*, and it must not report as one.
* everything else — byte-identical to before: a genuine prefix-only row (the band
  truncated the line) is still `paint_loss`, and a line missing from the grid
  entirely is still `paint_loss`/`cjk_wrap`/`wrap_artifact_or_loss` by width.

The CLI prints `rejoined=` and `repainted=` on the status line and
`row_tail_runes=` per line, so the new class is visible in the verb's own output.

## Why the rule is not a blanket excuse

The dangerous shape is a rule that accepts any row starting with the line: then a row
carrying two transcription lines would make the first one "arrived" *and* hide the
second. `rowCarriesLine` requires the tail to be **the line's own head**, so:

| row | verdict |
|---|---|
| `line` | match, `row_tail_runes=0` |
| `line + line[:29]` (measured) | match, `row_tail_runes=29`, `repaint_tail` |
| `line + line` (a full re-draw) | match, `row_tail_runes=33`, `repaint_tail` |
| `line + "另一行完全不同的内容"` | mismatch — not this line's row |
| `line + line + "某"` | mismatch — a third thing the line does not have |
| `line[:20]` (truncated) | mismatch — the real-loss control |

Every one of these is pinned by a test (see the report).

## Not fixed here (deliberately)

The **renderer** residue is not repaired: the band still leaves a re-paint's tail on
the row. That is a paint defect of `internal/lineedit`'s answer band and reproducing
it needs the live-event path, not a containment rule; this task's scope was the
audit's classification (the briefing's own framing: "fixed **or** a new damage
class"). What changed is that the defect is now **named** (`repaint_tail`) instead of
reported as the loss it is not.

Also observed while measuring, and left alone as out of scope: the chat transcript
screen row keeps the `[gopi] ℹ thinking budget …` notice while the session log holds
only the assistant answer (the harness's stub model is re-issued thinking-disabled), so
the row-level coincidence between dump and log is weaker than the audit's counts
suggest. It does not affect this defect's verdict, which is computed per answer line.
