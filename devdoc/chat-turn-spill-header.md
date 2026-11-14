<!-- doc-class: historical -->
# The turn header's SPILL FORM — a spilled turn describes the paste, not the path

Task `01a0e922-69fe-740c-8c0b-901ee777fc06` (epic: rework of the archived
`01a0e823` branch). This document is the record of that rework: the intent came
from a branch whose ref was deleted after a substantive conflict with master, and
the feature was re-implemented from current master on a fresh branch.

## The defect this closes

When a paste is too large for the input line, the editor spills it to a file and
leaves a REFERENCE on the line:

```
you> [paste 65536: /home/user/.gopi/sessions/<id>/paste/paste-20260929-013000-1.txt]
```

That reference was then what the turn's header displayed, because the header's
`Prompt` field is whatever the input line held:

```
──── turn 3 · [paste 65536: /…/paste-20260929-013000-1.txt] ────────────────────
```

Two things are wrong with it, and they are different kinds of wrong:

1. **A reader cannot tell the turn's input was a paste.** The words `turn 3`
   followed by a path read exactly like a typed prompt that happens to be a path.
   The one fact a reader most needs — *your input was a file, not a message* — is
   nowhere on the line.
2. **The size it shows is the WRONG number.** The `N` in a reference is the count
   the editor had when it last rewrote the reference; on the already-spilled route
   (the route every editor spill takes) the boundary built the turn's description
   from the reference LINE itself, so a 64 KiB paste was described by the
   reference's own ~70 characters. The header was not misreading the payload; it
   was describing the wrong object entirely.

The fix gives a spilled turn its own header form, whose fields are read from the
FILE.

## The shape

```
──── paste 1 · 20480 bytes · 7168 runes · 512 lines (+512 more lines) · /dev/shm/suite-….txt ────
──── paste 7 · 65536 bytes · 65536 runes · 11 lines · head 3 · $ git status · On branch master · [paste …]────
```

Fields, in the order the band can afford them:

| field | what it answers | source |
|---|---|---|
| `paste 7` | was this turn a paste? | the turn index |
| `65536 bytes · 65536 runes` | how big was it | the FILE (`ReadSpillFacts`) |
| `11 lines` | how long was it | the FILE |
| `head N` | how many of its opening lines this row drew | the header's own fit |
| `(+M more lines)` | what is NOT shown | the FILE's line count − the lines drawn |
| `/…/paste-….txt` | where do I open it | the payload path |

**Both sizes are shown** (bytes and runes) because the two readers are different
people: an operator pasting a log reads bytes, one pasting CJK text reads runes,
and the pair is the cheap check that the file holds what was sent.

### The budget is derived, not guessed

`spillContent` is the ONE place the split is decided, and neither half is a
constant that could disagree with the other:

- the DESCRIPTION's floor is computed from the payload: exactly what it takes to
  state the size **and** the marker for the lines not drawn;
- the HANDLE (the path) takes what is left, capped at its whole length, and never
  less than `turnSpillHandleMinCells`.

When the band cannot afford both, the HANDLE alone is the body. That is a
deliberate priority: a description with no file to open is worse than a path — the
payload is one `read` away and this header is the only place the operator sees
where it went.

### "Omission must be marked explicitly"

`turnSpillMoreMarker` composes the count INTO the summary string that is then
measured, rather than appending it after the fact. A head line is kept only while
the whole composition (size + correctly-counted marker + that line) still fits, so
a narrower band shows FEWER lines and a BIGGER count — never a silently shorter
string. `turnSpillMoreMarker(0)` is empty on purpose: a marker reading
`(+0 more lines)` would claim an omission that did not happen.

The count is the number of lines the header DID NOT DRAW, and `spillSummary` and
the header's rendered form derive it from the same `len(head)` at the moment each
candidate is composed — so the number a reader sees always adds up
(`shown + dropped == Lines`), which is asserted.

### Do not reprint the reference

`TurnSpill.Handle` is the **path**, not the reference line verbatim, and
`chatSealMessage` fills it with `paths[0]`. Reprinting the reference would put its
own stale `N` back on the screen beside the verified count, which is the defect
this change exists to remove. Both the unit case and the pty case assert the
header carries no `[paste` reference at all.

## Where the facts come from

`lineedit.ReadSpillFacts(path)` streams the payload and returns
`{Bytes, Runes, Lines, Head, HeadTruncated}`:

- **Streamed, never loaded** — a fixed 64 KiB buffer, so "show a few lines of the
  payload" can never come to mean "load the payload" (a 64 MiB paste is described
  with 64 KiB of memory).
- **The rune count is exact across read boundaries** — the trailing bytes of a
  split rune are held back (`splitWholeRunes`) and counted with the bytes that
  complete it. A naive per-chunk `utf8.RuneCount` inflates by one per boundary,
  which on a large CJK payload is a wrong number that looks plausible.
  **Measured pitfall (this rework's own bug):** `chunk := append(carry[:0], buf[:n]...)`
  writes the read OVER the carried bytes instead of prefixing it, and costs exactly
  one rune per straddling boundary — 44999 runes for a 45000-rune payload, found by
  the boundary leg of `TestReadSpillFacts_CountsWhatTheFileHolds`. The carry is
  copied into a fresh slice.
- **A payload with no trailing newline is ONE line**, not zero — the count a naive
  newline count gets wrong.
- **The head is bounded** (`turnSpillHeadMaxLines` lines × `spillHeadLineCapture`
  bytes each), and a line cut at that bound sets `HeadTruncated` rather than
  leaving a reader to infer it.

`ReadSpillFacts` FAILS LOUDLY on an unreadable path (Rule 15). The caller
(`spillDescriptionFor`, in `cmd/agent/chat_boundary.go`) then returns a handle-only
description and rides the failure on the operator's own notice
(`chatBoundarySpillFactsFailedFormat`) — never silently a header with no size.

**Why not take the counts from `PasteStore`**, which has them for the route it just
wrote? Because the already-spilled route did not write the file and holds only the
reference. Reading the file is the ONE source BOTH routes can share, and it is
authoritative by PasteStore's own contract.

## No-regression

The ordinary form is untouched at the byte level: `TurnHeader` returns early to
`spillHeader` only when `info.Spill.Spilled`, and every existing header path is
otherwise byte-identical. `chatBeginTurn` (new, in `cmd/agent/chat_stream.go`) is
the dispatch, and it is a FUNCTION rather than an if-statement at the call site
precisely so a test can execute the same dispatch production runs.

`internal/lineedit/turn_spill_header_test.go` asserts the two forms in one band
differ, that the spill vocabulary appears only on the spill row, and that the
ordinary header still opens with `turn N`.

## Verification

- `internal/lineedit/turn_spill_header_test.go` — total-in-cols at every width
  0..240, the marked-omission arithmetic (`shown + dropped == Lines`), spill-vs-
  ordinary distinguishability, and the `ReadSpillFacts` counts (ASCII,
  no-trailing-newline, CJK, a payload past the reader's buffer whose rune count must
  not move, a head line past the capture bound, and an unreadable path).
- `cmd/agent/chat_turn_spill_header_pty_test.go` — a REAL 117×26 pty, the real
  layout, the real boundary, a real spill through the real store, the header opened
  through the production dispatch (`chatBeginTurn`), read off the real dump grid.
  Observed:

  ```
  ──── paste 1 · 20480 bytes · 7168 runes · 512 lines (+512 more lines) · /dev/shm/suite-cmd-agent-run-809071… ────────
  ```

  against a fixture of `512 lines × 40 bytes = 20480` bytes and `512 × 14 = 7168`
  runes — the FILE's facts, not the reference line's own length (which is the number
  the defect printed, and which the case asserts is different). The fixture payload
  is deliberately ABOVE `lineedit.PasteThreshold`, so it is a paste production would
  really have spilled.
- `cmd/agent/chat_boundary_test.go:TestChatBoundaryDescribesTheFileNotTheReferenceLine`
  — the boundary-level regression gate for the wrong-size defect, on BOTH routes,
  plus the unreadable-payload report.
- RED PROOF: with the spill branch short-circuited in `TurnHeader` and the boundary
  description rebuilt from the reference line (the pre-fix behaviour), the pty case
  fails on `the spilled turn's header does not state the payload's true size
  ("20480 bytes")` and the unit case on `the header does not carry " bytes"`; with
  the branch restored both are green.

## Not done

- The header is SINGLE-LINE by design, so a very wide paste is still summarised on
  one row rather than the head occupying rows of its own.
- The terminal-context echo (the operator's pasted text appearing in the band after
  the echo) is out of scope and untouched.
- `head N` states how many of the payload's opening lines the header drew, but the
  head lines are the ones the CALLER assembled (`turnSpillHeadMaxLines`). A caller
  that assembles fewer gets a truthful smaller `N` and a larger dropped count; no
  caller does today.
- The archived branch's own `TurnSpill.Ref`/`Path` field pair was reduced to ONE
  `Handle` field: the two spelled the same absolute path, and this tree's
  `chatSealMessage` has a single path to hand over on both routes.
