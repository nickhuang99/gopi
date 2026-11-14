<!-- doc-class: historical -->
# The diagnostic channel writes over the layout — folding it to the width, and repainting the rows it covered

Task `01a1c334-dfb8-77e8-9689-4698c679f783` (2026-11-10). Historical record: what
the defect was, what the fix took, and the two things a later reader would
otherwise re-derive.

## The defect, measured

The editor's degradations go to `errOut` — stderr, the SAME terminal the reserved
layout draws on, but not the layout's writer. `degradation()` wrote the caller's
sentence verbatim, so a sentence that did not fit the width was not merely ugly,
it was a write OVER THE LAYOUT: it started at the cursor's row (the editing line)
and every LF it carried scrolled the frame.

`warnPasteSuppressed` composed ~300 cells (five facts and a remedy). At 101
columns that is three rows on a 24-row screen, and the rows below the editing
line are the menu's and the STATUS ROW's — so the operator read a status line with
a sentence printed through it until the next keystroke repainted the frame.

The red proof, run against the unmodified production files with the new test in
place:

```
--- FAIL: TestDegradationFoldsToTheLayoutWidth (0.00s)
    degradation_width_test.go:181: row 0 is 285 display columns wide, want <= 101:
--- FAIL: TestDegradationRestoresTheStatusRow (0.00s)
    degradation_width_test.go:233: the status row changed across the diagnostic — it was not repainted:
```

## The fix, in three parts

1. **`Editor.emitWarnRows` folds the screen's bytes.** While the layout is
   installed it wraps with `WrapToCols` at `region.Cols()` — the ONE wrap policy
   this package already exports for a caller streaming to a terminal without a
   band — caps the rows at `statusRows` (the ONE row outside the band and the
   editing line), and breaks with CRLF (raw mode cleared OPOST, so a bare LF
   starts the next row where the previous one ended). With no layout it keeps the
   historic bytes.
2. **The RECORD is not folded.** `diagSink` receives the sentence verbatim, with
   the trailing newline `warn()` composes. A record that inherited the screen's
   fold would be N lines with a carriage return in front of each, and the grep
   that exists to find it would stop working (Rule 15).
3. **`Editor.repaintAfterDegradation` puts the chrome back**, on the read loop
   only (`onLoopGoroutine`: a repaint reads the editing model and one goroutine
   does that). It drops the answer renderer's `drawnStatus` record — a sentence
   printed through the row makes it stale — draws the status row, then repaints
   the editing line and the menu. What it CANNOT restore is the band's committed
   transcript: those rows scroll as a unit and re-drawing them is not a thing the
   band can do from here. This is deliberately not claimed as a full restore.

`warnPasteSuppressed` is also shortened to what a reader cannot derive — which
evidence classified the read, the window that evidence carries, the running
suppression count — and the remedy half is gone. Measured in the live run below:
87 cells including the `[gopi] editor: ` prefix, one row at 101 columns.

## The live run (Rule 31)

A real `gopi chat` on a 101x24 pty, driven through the production key path (one
read of three keys, then a CR inside `PasteBurstCharInterval`):

```
[gopi] editor: paste: Enter kept as newline (3 keys at once, window 8ms, 1 suppressed)\r
\n
ESC[24;1H ESC[2K ... status row redrawn ...   ← the very next bytes
```

The diagnostic row is 87 cells (one row), and the status row is written again
immediately after it — the two facts the unit tests assert, observed on a
terminal rather than asserted against a model.

## What a later reader should know

- The status row is the only row outside both the band's scrolling region and the
  editing line, which is why `statusRows` is the natural row budget for a
  diagnostic and why it is the row such a write destroys.
- `degrade_record_test.go` asserts the record's bytes; `degradation_width_test.go`
  asserts the screen's rows and the restored grid. They are different
  destinations and a change to one does not cover the other.
- Two existing arms named the old wording (`paste_burst_origin_test.go`,
  `paste_burst_pty_test.go`); they now assert the facts that survive the
  shortening. Neither lost an assertion about the window or the count.
