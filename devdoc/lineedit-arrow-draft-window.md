<!-- doc-class: historical -->
# The arrow keys must never replace a multi-line draft

## The defect

`internal/lineedit` gives Up/Down two meanings, chosen at key time in
`Editor.handleKey`:

1. an open completion menu owns them (selection move);
2. otherwise, if the editing line's WINDOW has rows the screen does not show,
   they scroll the window;
3. otherwise they recall history (`History.Prev` / `History.Next`).

The arm that decided between (2) and (3) asked the wrong question. It called
`scrollWindowIfClipped`, which reported **whether the window MOVED** — and
`scrollEditingLine` returns `false` both when nothing is hidden AND when the
window is already at that end. At the edge (the ordinary state: the caret sits
at the end of the text, so the window at the caret's end is at its last row) the
scroll reported "no movement", the arrow fell through to (3), and
`History.Prev` handed its answer to `Editor.recall` → `Value.SetText`, which
**replaces the whole buffer**.

A multi-line draft — pasted text, several prompt lines, a long message being
written — was therefore replaced by a history entry on a single arrow press.
`History.Prev` keeps the live line in `h.pending`, but that copy was taken from
the buffer AFTER the caller had already read it, so nothing restored the draft:
the operator's own text existed nowhere. Data loss, not a display defect.

Measured on the real surface (pty, `gopi chat`, private sandbox home; the pty
transcript is the instrument and the history entry is a unique marker string):

| binary | mode | recalled marker on screen | outcome |
|---|---|---|---|
| pre-fix (f6b35c7e3) | 30-line draft, 24× Up | **yes** (draft gone) | defect reproduced |
| pre-fix (f6b35c7e3) | one row, 1× Up | yes | correct (recall) |
| fixed | 30-line draft, 24× Up | no (draft head still drawn) | draft survives |
| fixed | one row, 1× Up | yes | recall unchanged |

## The fix (two questions, never one)

The two facts are now asked separately:

- `Editor.windowHidesRows()` — **which key this is**: the window reaches fewer
  rows than the buffer has. This is a pure predicate over the published window
  (`win.total > win.textRowsOf()`), with no side effect and no "did it move" in
  it.
- `scrollWindowIfClipped(up)` — **what to do about it**: scroll, and report only
  whether a repaint is owed. Its `false` no longer means "not my key".

`handleKey` asks the predicate FIRST:

```go
if e.windowHidesRows() {
        e.scrollWindowIfClipped(true)   // scrolls, or is INERT at the end
        break                            // never the history
}
e.recall(...)                            // only a line that FITS recalls
```

So a window with hidden rows makes the arrow a window key and nothing else —
inert at the edge, never the history — while a line that fits (every ordinary
single-line prompt, and the whole recalled-history workflow) keeps exactly the
behaviour it had.

The third meaning is unchanged: an open menu still takes precedence over both
(the menu branch is still first in the switch), so Up/Down keep moving the
completion selection. Each of the three is pinned by a test.

## Files

- `internal/lineedit/window.go` — `windowHidesRows`, and the doc comment on
  `scrollWindowIfClipped` that now states which question it answers.
- `internal/lineedit/editor.go` — the KeyUp/KeyDown arms: predicate first, scroll
  second, history only when nothing is hidden.
- `internal/lineedit/window_arrow_draft_test.go` — the regression: the draft
  survives every arrow press at both ends and past them; a line that fits still
  recalls and walks back to the live line; an open menu still owns the arrows.

## Verification

- Red first: `go test ./internal/lineedit/ -run TestWindow_Arrow -count=1` failed
  at Up press 24 with the buffer reading the recalled entry.
- Green after: same command passes; `make test-budget PKGS='./internal/lineedit'`
  reports `outcome=pass`.
- Real surface (Rule 31): the binaries were run under a pty with a private
  `GOPI_HOME`; the table above is that run.
