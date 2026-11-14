<!-- doc-class: historical -->
# The `ui` surface's row grammar, and the two absences `--compare` used to spell the same way

**Task 01a0e62d · 2026-09-28 · `gopi ui_state --compare` on a live `gopi ui` surface**

## What was reported

`gopi ui_state --compare` was described as inert on the `gopi ui` surface: it
located the line on the grid (`in_grid=true`, `display_hits=1`) but answered
`transcript_lines=0 outcome=could-not-run`, as if the authoritative transcript it
consulted did not correspond to the ui surface.

## What a live run actually shows (the measured root cause)

A real pty run settles the question, and the answer is not the briefing's
wording — it is worse in one direction and better in another.

The run: `gopi ui --dump-screen` under a 110x40 pty, one typed message served by
a local Anthropic-SSE stub, the dump **copied while the surface was still live**
(`View()` returns `""` after quitting, so the artifact on disk after Ctrl+C is
the erased screen). The artifact pair:

```
003|gopi> UI-AUTH-OK-8842 qwerty          ← the grid row (text side of the dump)
{"text":"UI-AUTH-OK-8842 qwerty", …}      ← the session record beside it
```

**The ui surface DOES have an authoritative record**, in exactly the place the
derivation looks: `uiclient.SessionDirOfDump` → the `session.jsonl` one level
above the dump, the same file `gopi log <id> --response --text` prints. The claim
that the ui surface keeps no transcript the reader can consult is false.

What was actually broken is the **row grammar**. Each surface labels its own rows
(`internal/uiclient/model.go`'s `wrapEntry` draws `you> ` / `gopi> ` / …), and the
transcript holds the model's TEXT, which never carries a label. The compare
stripped only the *chat* surface's band markers (`internal/lineedit`'s `▌` and
`·`), so on a ui grid every row was compared with its label still attached:

```
row after the chat-marker strip : "gopi> UI-AUTH-OK-8842 qwerty"
transcript line                 : "UI-AUTH-OK-8842 qwerty"
→ in_grid=false, damage=paint_loss
```

Measured, live, before the fix:

```
outcome=disagreed match=false
  mismatch grid_row=-1 in_grid=false in_transcript=true damage=paint_loss prefix=0
  reason="…the paint lost it, or it has scrolled out of the visible grid"
```

The line was on the grid, the line was in the log, and the verb told the reader
the paint had lost it — **the diagnosis inverted**, on a screen painted
faithfully. That is the real defect: not an inert flag, but a flag that answers
confidently and wrongly for the surface it was asked about.

The `transcript_lines=0 could-not-run` shape in the briefing is the *stale
artifact* variant (see "torn pair" below), which is a different defect and is
also repaired here: `no-authoritative-side` is now its own outcome word.

## The fix

1. **The row grammar is per surface** — `uiclient.CompareGutters(surface)`. The
   chat surface's markers still come from `internal/lineedit` (never re-typed);
   the ui surface's markers come from `internal/uiclient/model.go`'s own prefix
   constants. Both are threaded through every place a grid row is read against
   text: `compare.go` (`stripCompareGutters`), `audit.go` (`auditWrapRow`,
   `auditGutterCols`, the wrap join), `damage.go`.
   *The union, not a replacement*: a dump read at the wrong surface marker must
   never be answered with a false paint loss, and a strip that removes a prefix
   the row does not carry is a no-op. The chat grammar deliberately does **not**
   gain the ui labels — a chat row whose text genuinely begins with `gopi> ` must
   keep it (`TestTheUILabelIsNotStrippedFromAChatRow`).
2. **`no-authoritative-side` is its own outcome word** (`uiclient.CompareNoAuthority`,
   `uiclient.AuditNoAuthority`), distinct from `no-transcript`. The two absences
   are different facts — *"an authority was read and held nothing"* vs *"there is
   no authority here at all"* — and both used to print as `transcript_lines=0`,
   which a reader cannot tell apart. The new word is produced only when the
   derivation finds no record at all, carries `--session` as the way to supply a
   side, and **never carries a damage class**: the evidence does not support
   blaming the paint. Exit code stays 1 (nothing was compared); the *word* is what
   distinguishes it, because an unchecked screen is not a verified one.
3. **`-h` corrected**: the verdict vocabulary gained the new word, the exit-code
   paragraph names it, and `--compare` now documents the per-surface row grammar
   and the ui surface's spelling (`gopi> …` is the ROW; `UI-AUTH-OK-8842 qwerty`
   is the transcript line).

## Verification

- **Live acceptance** (Rule 31), verbatim, on a real pty run of the shipped
  binary, against the LIVE artifact:

```
gopi ui_state --compare "UI-AUTH-OK-8842 qwerty"
#gopi-ui-state-compare surface=ui state=live … transcript_lines=1 outcome=agreed match=true
  match grid_row=3 in_grid=true in_transcript=true damage= display_hits=1 geometry=layout cols=110 rows=40 frames=27 prefix=0
EXIT=0
```

  The same command on the same artifact **before** the fix: `outcome=disagreed
  damage=paint_loss prefix=0`, exit 3. The red→green transition is the evidence.
- **Fixture-driven tests** (the captured pair of that live run, archived under
  `cmd/agent/testdata/`): `cli_uistate_compare_ui_surface_test.go` — the real ui
  dump agrees; the ROW's spelling is not the transcript; a missing authority is
  `no-authoritative-side` with no loss class; a read-and-empty authority is
  `could-not-run`/`no-transcript` (the negative control that gives the new word
  its meaning).
- **Grammar tests** (`internal/uiclient/ui_surface_grammar_test.go`): every ui
  label strips; the chat case is unchanged; `TestUIRowPrefixesMatchTheRenderer`
  pins the reader's list against the renderer's own output, because the drift
  between those two is what produced the defect.

## Pitfalls a later agent will otherwise pay for

- **The dump must be copied while the surface is LIVE** (`View()` → `""` after
  quit), and **as a PAIR**: the reader takes the grid from the `.json` twin and
  the identity from the text file. Copying one without the other produces a torn
  pair which reports `ALL-BLANK` / `transcript_lines=0` — a plausible-looking but
  *fake* reproduction. The first hour of this task was spent on exactly that.
- **A compare fired the instant the pty bytes carry the answer measures the drip
  rate, not the paint** (measured: `prefix=21` of 22). The turn has to settle:
  the surface's own status row says `idle · turns N`, and the sink is throttled
  (~100 ms) so the dump file must stop changing too.
- **A local SSE stub must bind port 0** and publish the bound port; a pre-picked
  port races and the turn dies with "connection refused". Set BOTH
  `ANTHROPIC_*` and `DEEPSEEK_*` — one group alone silently reaches the real
  endpoint.
- `GOPI_MAX_TOKENS`/`GOPI_THINKING_BUDGET` must be sized so no thinking-budget
  NOTICE line is painted between the answer and the end of the turn — otherwise a
  compare against the plain line reports `in_grid=false` on a faithful paint.

## Evidence artifacts

`~/.gopi/sessions/01a0e62d-7d38-71f3-b1b1-ec9f15bd4861/evidence/`:
`stubllm_ui.py` (SSE stub), `drive_ui_live.py` (pty driver: type → settle → compare
→ copy the live pair), `pty/run8.txt` (the accepted run), `pty/ui-agreed2.dump{,.json}`
(the captured pair).
