<!-- doc-class: historical -->
# The whole-answer audit: `gopi ui_state --all` — an agent checks its OWN paint

Task: `01a0dd2943d6776aa99f59f5f7245be3`, branch
`task/01a0dd2943d6776aa99f59f5f7245be3`. Builds directly on the one-line compare
(`devdoc/chat-screen-three-channel-diff.md`, task `01a0dd26-…`).

## The problem

The one-line compare answers **"is this one line mine?"** — the caller brings the
line. That is the wrong question when the operator's complaint is *"my answer did
not arrive"*: the answer is hundreds of lines, the caller does not have it in hand,
and the lines that matter are exactly the ones it cannot quote (they are missing).

## What was built

`gopi ui_state --all --session <id|path> [--target ui|chat] [--last N] [--json]`
adds a WHOLE-ANSWER mode riding the existing surface: every line of the session's
own assistant text is checked against the ARMED dump's grid as ONE unit, and the
answer is a **ledger**, not a per-line yes/no.

```
#gopi-ui-state-audit surface=chat state=exited dump=… session=… outcome=disagreed
  status=disagreed geometry=layout cols=117 rows=26 frames=97 transcript_lines=3 audited=3 skipped=0 truncated=false matched=0 disagreed=3
  line 0 damage=paint_loss grid_row=-1 in_grid=false in_transcript=true expected_cols=57 expected_runes=57 grid_cols=117 prefix=0 text="…"
    reason="the transcript has this line but the grid does not (longest verbatim prefix on the grid: 0 runes): the paint lost it, re-wrapped it, or it has scrolled out of the visible grid"
```

**The implementation re-uses the one-line core unchanged.** Every line goes through
`CompareTranscriptLine` — the screen model, dump reader, geometry rule and
garbage/refusal logic are not re-implemented. `AuditAnswer` adds the UNIT (walk the
answer), the LEDGER (counts that close), the CLASSIFICATION (from each verdict's own
evidence), the DIRECTION (which layer lost it) and the EMPHASIS half.

### The three outcomes, and the exit code derived from them

| `outcome=` | meaning | exit |
|---|---|---|
| `agreed` | the check RAN and every audited line arrived | 0 |
| `disagreed` | the check RAN and ≥1 line did not | 3 (`uiStateExitCompareMismatch`) |
| `could-not-run` | no dump armed / unpainted / fallback geometry / nothing to check | 1 |

`could-not-run` exists because the dangerous confusion is not "we disagree" — it is
N refusals read as N verified lines. A grid that cannot be read refuses the WHOLE
audit, never per line.

### Damage classes, named from evidence

- `paint_loss` — in the log, fits the grid's width, not on it. A wrap cannot
  explain it; the band never drew it.
- `wrap_artifact_or_loss` — in the log, **wider** than the grid. One frame cannot
  tell a correct wrap from a loss, so the audit says the ambiguity instead of
  asserting a defect.
- `cjk_wrap` — as above, and wider in **cells** than in **runes**: the layout's
  double-width arithmetic is the candidate explanation.
- `emphasis_unpainted` — the text arrived and the format marked part of it
  emphasised while the grid's cells carry the row's plain style: the emphasis was
  lost (or never applied). Measured per CELL against the row's own baseline; a row
  with ONE style has no baseline, so emphasis there is reported as **unknown** —
  never counted as painted, never accused.
- `rejoined` — the line was NOT on one row because the grid wrapped it. It ARRIVED:
  the counts say `matched`, the ledger keeps the entry and the rows it came from,
  because "the wrap is not a loss" is a fact a reader must be able to see.

### Two measurements that had to be made rather than assumed

1. **A band row the driver wrote is scrollback, not grid.** The reserved region
   holds its LIVE row; earlier rows leave the top margin and the terminal discards
   them (measured: `WriteBandRow("ROW-ONE")` then `("ROW-TWO")` leaves only
   `ROW-TWO` on the grid). The committed test fixture therefore paints ONE row and
   asserts it read back — a case can no longer go green against a grid that never
   carried it.
2. **The band CUTS a row wider than itself; it does not wrap it.** So an over-wide
   line reaches the audit as "in the log, not on the grid, wider than the grid" —
   which is exactly the `cjk_wrap` / `wrap_artifact_or_loss` shape.
   `TestUIStateAudit_OverWideLineIsNotAccusedOfBeingLost` pins it, including the
   requirement that the flat accusation `paint_loss` never appears for it.

## What `/tmp/dump_text.txt` is, and what the audit says about it

`/tmp/dump_text.txt` is **channel C of the three-channel investigation**: the
OPERATOR's MOUSE COPY (89–90 lines) — text dragged off the terminal by hand, with
no provenance header, no geometry and no frames. It is not produced by gopi and it
is not the dump.

Because the audit's own comparison is *grid vs the session log*, C is NOT one of
the two sides: C is a different kind of object (a human copy), and comparing B
against C is precisely what the earlier investigation already did, concluding
`A = B ≠ C` — **C is the lossy side**, and the loss is in the copy (the visible
damage in C is a shifted first character: `证明oubao 说对了一半…` where the grid
carries `▌ ## Doubao 说对了一半…`). The audit does not need to account for C's
damage: it measures the paint, and C's damage is downstream of the mouse.

Reproduced for the record against the REAL artifacts of that run (the true session
log of the 117x26 `runFull` chat, and C wrapped as a transcript):

```
$ GOPI_HOME=… ./gopi ui_state --all --session …/runFull/…/session.jsonl --target chat
  status=disagreed geometry=layout cols=117 rows=26 frames=687 transcript_lines=76 audited=51 skipped=25 truncated=false …
  line 0 damage=paint_loss … text="I have the verified architecture. Let me answer directly."
```

That grid (frame 687, `dumps=45`) is the FINAL frame of a chat that had already
scrolled — the visible top-of-screen rows are the band's latest ones, and the
earlier answer rows are genuinely not on it. The audit reports exactly that, and
the `reason` sentence names the third explanation ("it has scrolled out of the
visible grid") rather than claiming a renderer defect. **This is the honest limit
of one frame**: the dump is a single instant, the grid is finite, and an audit can
say "not on the visible grid" but not "never drawn" for text that scrolled.

## Verification

- Build: `go build ./...` OK; `CGO_ENABLED=0 go build -o bin/gopi ./cmd/agent/` OK.
- Tests: `go test ./internal/uiclient/ ./cmd/agent/ -run 'Audit|Compare|UIState' -count=1` → both packages `ok`.
  `internal/standards` and `internal/stylegate` clean (the errors-verb ceiling was
  at 7/7 before this change and stays there; the one new `time.Sleep` carries an
  `//stylegate:allow-sleep` reason).
- Ran: `e2e/audit_e2e_final.py` — a real pty running the real `gopi chat
  --dump-screen`, the real verb against the real dump (the four-way contract
  below), plus `./bin/gopi ui_state -h`.
- Result: exit contract OBSERVED — clean=**0** (`outcome=agreed`), planted=3
  (`damage=paint_loss`, direction named), window=3 (reason names the window),
  unarmed=1 (could-not-run, never "agreed").

### The real-pty run

`gopi chat --dump-screen` on a 117x26 pty seeded by a local SSE stub
(`$GOPI_TASK_SESSION_DIR/e2e/audit_e2e_final.py` + `textmock.py`, adapted from task
`01a0dd26`'s harness; the harness lives in the run's session dir, not the repo).
The harness walks the answer in a handful of short lines, so the sink writes small
frames the reader can accept.

**The capture rule, learned the hard way.** The band holds only its LIVE row, and
the sink is throttled (100 ms) writing `.dump` and `.dump.json` as two separate
atomic steps. So a frame must be seized **while painted AND whole**: a pair copied
mid-step is TORN, and the reader refuses it by name (`unreadable`: ".dump names
pid A while its .json names pid B") — a torn pair is never audited as if it were a
screen. The harness therefore captures several frames in capture order and runs the
audit on the **first pair the reader ACCEPTS**; that pair is the one that really
held the answer. Raw untrimmed console transcripts and the `.exit` files are in the
run's session dir (`…/e2e/run16/`, `…/e2e/evidence/`).

```
chat exit 0 pty bytes 4321 frames captured 3
clean leg runs on …/live/frame-00
$ gopi ui_state --all --session …/audit-home/…/session-one.jsonl --target chat
exit 0
  status=agreed geometry=layout cols=117 rows=26 frames=65 transcript_lines=1 audited=1 skipped=0 truncated=false matched=1 disagreed=0

$ gopi ui_state --all --session …/audit-home/…/session.jsonl --target chat   # the full answer, same grid
exit 3
  status=disagreed … transcript_lines=2 audited=2 matched=1 disagreed=1
  line 1 damage=paint_loss grid_row=-1 in_grid=false in_transcript=true expected_cols=35 expected_runes=35 grid_cols=117 prefix=2 text="zqy second short line, also painted"

$ gopi ui_state --all --session …/session-planted.jsonl --target chat   # one line planted into the log
exit 3
  status=disagreed … transcript_lines=3 audited=3 matched=1 disagreed=2
  line 1 damage=paint_loss … expected_cols=47 … text="zqq planted line the painted grid never carried"

$ gopi ui_state --all --session …/session.jsonl --last 1
exit 3   reason="audited the LAST 1 of the 2 transcript lines (--last): lines before the window were not checked"
$ GOPI_HOME=<empty> gopi ui_state --all --session …
exit 1   (could-not-run: "no ui-screen.dump dump found in any scanned location")
```

Three things this run settles that a unit test cannot:

- **The CLEAN leg is green and it is not vacuous.** `outcome=agreed`/exit 0 came
  from the same verb, the same real dump and a real session log — the reader
  ACCEPTED that pair, and the one audited line is the one the accepted frame
  holds. A green that a torn or empty frame could produce would be worthless; the
  capture rule above is what makes it worth something.
- **The DIRECTION is named, and the wrap explanation is not abused.** The planted
  line is **47 columns on a 117-column grid** — it FITS by construction, so
  `paint_loss` is the only reading that survives, and the audit does not retreat
  to "it wrapped" to excuse itself (`wrap_artifact_or_loss` is reserved for the
  case where the grid really shows the re-wrap).
- **The full answer on the same grid disagrees for a different reason.** The
  two-line log sends line 0 (on the grid) and line 1 (a 26-row grid whose band
  holds one live row), and the audit reports the ledger honestly:
  `audited=2 matched=1 disagreed=1`, with only line 1 accused.

## Not Done (honest)

- **The emphasis half is pinned by unit tests, not by the real pty.** Every
  real-pty leg above reports `emphasis checked=0` — the stub's answer carries no
  bold span whose glyph the grid drops, so the real-pty evidence covers the
  paint-loss/agreed/refusal halves and leaves `EmphasisUnpainted` to
  `internal/uiclient/audit_test.go` (the identical-grids-one-variable-flipped
  pair). An agent looking for a measurement of the emphasis path should read that
  test, not this section.
- **One frame cannot separate "scrolled off" from "never drawn."** A line absent
  from a 26-row visible grid is reported as `paint_loss` with the third
  explanation spelled out in `reason` ("… or it has scrolled out of the visible
  grid"). That is the honest limit of the surface: only a taller grid (or a
  capture at the exact frame the answer occupies, which the clean leg above does)
  makes "was on screen" the same claim as "was drawn".
- The `skipped=25` count in the real-artifact probe is the format layer's own
  skipping (blank lines and fence/table grammar) — counted, never dropped, and the
  ledger says so; it is not a measurement of loss.
- A C-vs-B comparison mode is NOT added: C is a human copy, not a grid, and the
  audit deliberately compares grid against the session log.
