<!-- doc-class: historical -->
# devdoc: `--dump-screen` describes an observation gopi takes

**Task**: `01a0e2e6-553f-76cc-bab4-164393671105` · **Branch**: `task/01a0e2e6553f76ccbab4164393671105`
· **Date**: 2026-09-27

## The defect, in the operator's words

`gopi chat --help`, under `--dump-screen`, opened with:

```
THE RECIPE (in this order; each step is needed):
  (1) in ONE real terminal start:  gopi chat --dump-screen
  (2) read it back:               gopi ui_state --target chat
  (3) check a line you know:      gopi ui_state --compare "<line>" --session <session-id> --target chat
```

Read from the outside that is gopi asking a **human** to debug gopi's own screen.
Step 3 is worse than the others: it asks the reader for a *line* — and the lines
on this screen are gopi's own output, so the operator has nothing to supply and no
reason to be told to "check a line you know". A help page that hands the operator
a task list is a page that has not noticed who owns the artifacts.

They are gopi's. The painted cell grid is gopi's own dump; the authoritative
transcript is gopi's own session record; and gopi already ships both halves of the
comparison — the in-turn `chat_surface` tool (`action=see` / `action=compare`) and
the CLI verb that audits a whole answer in one call.

## What changed

**`cmd/agent/cli_chat.go` — the `--dump-screen` block.** The numbered recipe is
gone. In its place, `WHO READS IT: gopi itself`, naming:

- the answering turn, through its own `chat_surface` tool — `action=see` (the
  painted grid as a cell grid) and `action=compare` (a line it wrote, against BOTH
  layers);
- the ONE automatic command, `gopi ui_state --target chat --all`, and *why* it
  needs nothing handed over: the audit **derives its authority from the dump**
  (the session record one level above it), so there is no line to name and no
  session to pass. The ledger reports how many lines of the answer arrived on the
  painted grid and how many disagreed, with the direction (which layer lost it)
  per disagreement;
- the exit contract, in the ledger's own words —
  `outcome=agreed / disagreed / could-not-run` → `0 / 3 / 1`, where `1` is
  deliberately **not** "they agree", because an unchecked screen is not a verified
  one.

The arming prose is unchanged in substance: the flag still documents what it
writes, the joined-form rule, `GOPI_CHAT_DUMP`, the opt-in cost and the
throttled-sample caveat. One sentence of the caveat was re-aimed at gopi ("the
GRID is the answer") instead of at the operator ("ASK THE GRID").

**`cmd/agent/cli_uistate.go` — the `--all` block.** It led with
`gopi ui_state --all --session <session-id> --target chat`, which is the two-operand
form. It now leads with the one-call form (`gopi ui_state --target chat --all`) and
says the session flag is *optional* — the authority is derived from the dump; name
a session only to audit against a DIFFERENT transcript on purpose.

## The automatic path was already shipped — and is now proven, not assumed

`ui_state --all` deriving its own authority is not new (`uiStateResolveCompareSession`
→ `uiStateCompareSiblingSession` → `sessionlayout.RecordInDir`); what was missing
was a demonstration that an operator can reach a verdict with nothing handed over,
on a **live** surface. Measured on a real 117×26 pty, real armed chat, one command:

```
$ gopi ui_state --target chat --all
#gopi-ui-state-audit surface=chat state=live … outcome=agreed
  status=agreed geometry=layout cols=117 rows=26 frames=102 transcript_lines=1
  audited=1 skipped=0 truncated=false matched=1 disagreed=0
exit=0
```

and, on the CJK line, the disagreeing direction with its own number:

```
  line 0 damage=paint_loss grid_row=-1 in_grid=false in_transcript=true
    expected_cols=61 expected_runes=33 grid_cols=117 prefix=33
    reason="the transcript has this line but the grid does not (longest verbatim
            prefix on the grid: 33 runes) …"
exit=3
```

## The pipeline finding worth keeping (it is NOT this task's bug)

The CJK `prefix=33` above is **not** a paint loss, and the `--all` ledger's own
wording is careful to list the other explanations ("the paint lost it, re-wrapped
it, or it has scrolled out of the visible grid"). What the artifact shows:

- the dump's **grid** row is *cell-spaced* (`▌ 自 观 测 闭 环 ： …`, one blank cell
  between CJK glyphs) while its **lines** section holds the same row *unspaced*
  (`▌ 自观测闭环：…`) — `internal/lineedit/screendump.go` documents the grid as
  one rune per CELL;
- the verdict matches on `cand.Dump.Lines` (the unspaced section) — so a CJK
  answer that fits the line exactly (the ASCII control: `matched=1`) reaches
  `match`, while the CJK one does not.



## Files

- `cmd/agent/cli_chat.go` — the `--dump-screen` help block.
- `cmd/agent/cli_uistate.go` — the `--all` help block.
- `cmd/agent/cli_chat_dump_screen_self_serving_help_test.go` — the gate (new).
