<!-- doc-class: historical -->
# The `⚙ call …` row is a WRAP, not a status field — where the operator's chat dump was cut, and which layer owned it

Task `01a0e166`. Two layers rendered a `⚙ call …` activity row; one owned the
defect. Both are named below, and so is the measurement that decided between
them.

## The defect, as the operator's dump shows it

`/tmp/error.txt` (a real chat transcript, 117 columns):

```
⚙ call bash cd ~/work/GoPi; echo "=== branch ==="; git branch -vv; echo "=== remotes ==="; git remote -v; echo "=== s
tatus ==="; git status -sb | head -20
⚙ call bash cd ~/work/GoPi; echo "=== worktrees (the + prefix) ==="; git worktree list; echo "=== peer/sync config ==
="; python3 -c "
```

and, in the turn header, `… $ git branch+ fix/p` / `…`. The shell word `status`
is split across two rows; the continuation carries the SAME blank two-cell indent
a fresh `⚙` row carries, so nothing says the two rows are one command. A copied
first line does not run. `internal/lineedit/answer.go` already states the policy
this violates — a field the row had to shorten ends with `statusEllipsis`, "the
difference between 'the terminal is narrow' and 'this is corrupted data'".

## Which layer wrote it — the question the brief sent me to answer

**Cleared (not the writer): `cmd/agent/chat_stream.go`'s live status surface.**
That row is composed by `fitField` and a field it must shorten ends with
`statusEllipsis` — the surfaced `/status`, the glyph row and the retry/elapsed
fields all elide. A long `bash` argument never reaches it.

**The owner: `internal/lineedit/answer.go`.** The chain, read off master
`b514d1a2`:

```
cmd/agent/lineedit_wire.go:268  chatInput.WriteToolRow(...)   // "⚙ call bash …"
  → lineedit.Editor.Note(text, st)                             // queued as a NOTE row
    → AnswerRenderer.drainNotes()                              // answer.go
      → writeRow(answerRow{text: n.text, st: n.st}, n.text)    // → cutCols(text, avail)
        → cutCols(text, avail)                                 // HARD CELL CUT, no boundary, no marker
```

and `lineGutter()` returned `""` for every row drawn while `r.noting` was set —
so the continuation row had no gutter at all beyond the layout's fixed two-cell
indent. `writeRow`'s continuation policy is correct for an ANSWER line, whose
wrap is the reader's own prose line break; a note is the record of what the agent
RAN, and its reader is as often a copy-paste as an eye.

### The cut is counted in CELLS, not bytes — measured, not assumed

Two independent readings, both on the operator's own artifact:

* every `⚙` row in `/tmp/error.txt` is exactly **117 cells** — the same 117 as
  the turn-header rule drawn on the same screen (`────────────────────…`), and
  CJK rows in the same dump are also 117 cells while their byte length differs.
  A byte-counted cut would have produced three different row lengths.
* the harness measurement below: a note handed to a 48-column band is cut at
  `avail = cols - liveCol` in display cells.

So the fix belongs where cells are counted — and it must not change what a
*byte*-measured reader sees.

### The second defect, found on the way (fixed in the same write)

`drainNotes` wrote each note as an ACTIVE LIVE row (`liveRow = out.LiveRow()`,
`liveCol = 0`) and only finalized it after every note drained. The band's
live-row wrap is computed from the LIVE ROW's width, which on the xterm legend
layout is **one cell wider** than the terminal's — so a note of 118…125 cells
rendered one cell into the screen's right margin: text that is never displayed.
Reserving the note's rows explicitly (below) removes the shape.

## The fix

`internal/lineedit/answer.go`: `drainNotes` now calls `writeNote(n)`, which
commits a note as many band rows as it needs.

* **`writeNote` / `cutNoteRow`** — reserve the row explicitly at the band's own
  width `r.cols` (not "the live row's remaining cells"), then search the row's
  cells for a break.
* **`cutAtBoundary`** — break at a boundary the text itself has: the LAST space
  the row used (consumed by the break — `noteTail`), otherwise one of
  `cutNoteBoundaries` (`/-._=:,` — kept, because a path's own separator belongs
  to the text that continues: `fix/pushback-rule31-evidence` breaks after
  `…-evidence`, never inside `evid|ence`). A space break is preferred to a
  word-internal one only when it fills at least `noteWrapMinFill`
  (one half) of the row — otherwise the transcript wastes a row on a break 35
  cells short of the edge.
* **`NoteContinuation = "↳ "`** — a continuation row opens with it (via
  `lineGutter()`'s new `r.noteContinuing` arm). Two cells, the same allowance
  every other gutter uses, so the activity block keeps ONE left edge and **no
  existing row moves by a cell**: the gutter only ever appears on rows this
  defect itself created. `↳` rather than `…` because the row is not short: the
  command continues in full on the next row.
* **the must-elide case** — a break inside one NARROW word (`noteBreakInsideWord`:
  ASCII letters/digits/underscore on both sides) has no honest boundary to take,
  so the head ends that word's cut prefix with `statusEllipsis`, reserved OUT of
  the budget so a marked head is never a cell over the band. The characters the
  marker stands in for are NOT dropped: they continue on the next row (this is
  what separates the note path from the status row, whose one-screen-row
  invariant forces `fitField` to drop the tail). A break beside a WIDE rune
  (CJK), whitespace or punctuation is NOT "inside a word" — those scripts do not
  separate words with spaces, every rune is a legal break there, and a marker
  would flag text that is not damaged.

### The three cases, explicitly

| case | what the row shows | why |
|---|---|---|
| fits | the text unchanged, ONE row, no gutter | the common case — byte-identical to what `cmd/agent` has always printed |
| wraps at a boundary | `⚙ call bash …echo "===` then `↳ status ==="; git status -sb \| head -20` | every character is on the screen, the whole word moved to the next row, and the marker says the row continues |
| a word with no boundary in reach | head ends `…` and the next row carries the rest | no honest break exists; the marker says "this continues" rather than leaving a silent mid-token cut that reads as corruption |

## Evidence

* **RED on master** `/dev/shm/ledit-red` @ `b514d1a2`, with ONLY the new test
  file added → 4 FAIL, pasted in the session log and in
  `~/workspace/01a0e166-evid/red-master.txt`: identifier split
  (`… git status -s` / `b | head -20`, no ellipsis), continuation unmarked, CJK
  continuation unmarked, no marker on an unbreakable token.
* **GREEN after the fix**: `go test ./internal/lineedit/ -count=1` → `ok`
  (8.5s), `TestNoteRow*` 6/6 PASS.
* **The changed surface executed on a real pty** (Rule 31): a real `bin/gopi
  chat --tools` turn at 117×26 with the operator's own long `bash` argument,
  paint stream captured, read back through the shipped reader
  (`gopi ui_state`-format dump). Master grid rows 014/015 vs the fixed grid:

```
master 014|⚙ call bash cd ~/work/GoPi; echo "=== branch ==="; git branch -vv; …; echo "=== remotes ==="; git remote -v; echo "=== s
       015|tatus ==="; git status -sb | head -20
fixed  014|⚙ call bash cd ~/work/GoPi; echo "=== branch ==="; git branch -vv; …; echo "=== remotes ==="; git remote -v; echo "===
       015|↳ status ==="; git status -sb | head -20
```

* **CJK regression guard**: `TestNoteRowCJKNeverSplitsAWideRune` — a wrapped CJK
  note keeps every character, never carries U+FFFD, and its continuation is
  marked.
* **No invariant was weakened**: `statusEllipsis`, `statusLayoutBudgetCols` and
  `statusColumnsAtCols` are untouched (`git diff` names `answer.go` only), and
  the whole `internal/lineedit` package — including the band/one-screen-row
  suites — is green.

## Not done (honest)

* `TestChatDump_GridFollowsTheLayoutGeometryOnARealPty` failed once inside a
  whole-package `cmd/agent` run (`open the pty slave as a terminal: … not a
  terminal`) and passes on its own and on master: a pre-existing load-sensitive
  flake in the pty fixture, not a regression from this diff.
* `TestSpawnersPinTheRepoContext` and `TestTmpDirSettingsValueReachesTheSpawnedChild`
  fail on master's tree identically (measured at `b514d1a2`): host/settings
  state, untouched by this change.
* The turn-header row (`… $ git branch+ fix/p` / `…`) is the same cell cut on the
  same path and is fixed with it, but the operator's citation was the `⚙` row;
  the header's own wrap behaviour is not separately asserted by a test here.
