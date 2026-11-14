<!-- doc-class: normative -->
# The chat console's output channels, and which of them speak in the system's voice

**Scope.** Every way `gopi chat` puts characters in front of the operator, whether
it should carry SGR at all, and — where it should — which emphasis. The ruling
(operator, 2026-10-22) is one sentence: *the surface's remarks about itself are
LOW-CONTRAST and never loud* — `Dim`, never a highlight, never red — and every
channel that draws must keep its **Render** (styled cells) apart from its
**CopyText** (the bytes a paste produces).

This document is the INVENTORY the epic asks for (deliverable 1). It says what was
changed, what was deliberately left alone, and why each answer is the answer.

## The one shape everything inherits

`cmd/agent/term_tool_row.go` — `termCell` (one styled run), `termRow` (CopyText +
Render + Colour), `termToolStyles` (the ONE table, resolved once from the run's
colour capability via `termToolStylesFor` / `termToolStylesFromPalette`).

`cmd/agent/term_note_row.go` adds **no vocabulary**: it produces `termRow` values
and reads that same table. The only decision it makes is *which role a system
explanation wears* — `termToolStyles.Meta`, resolved `Dim` — and that decision is
one named function (`termSystemStyle`) so a later change is one line.

## The inventory

| channel | reaches | verdict | why |
|---|---|---|---|
| `/info` block (`chat_info.go`) | screen + piped stdout | **CHANGED** → dim labels, default-foreground values | the surface describing the session; the values are the operator's material and must stay copyable and un-emphasised |
| `/voice` notices (`chat_voice.go` → `chat_voice_note.go`) | screen + stderr | **CHANGED** → dim whole row | 28 rows of engine/device/refusal/transcript evidence: remarks about the mode, not answers. A notice is a PASTE SOURCE too (`VoiceNoticeStyled` passes `copied=true`) so it keeps the panel's hard column cut and gets no `↳ ` marker — measured: without that, four `chat_voice_pty_test.go` cases went red on a marker master never drew |
| tool-activity rows (`term_tool_row.go`) | screen | **UNCHANGED** | already the reference shape; this epic inherits from it |
| `/help`, `/queue`, slash-command tables (`WriteLines`) | screen + piped stdout | **NOT CHANGED** | these are REFERENCE content the operator reads and copies line by line; a table row is not a remark about the run, and dimming a table makes it unreadable at a glance for no gain |
| the answer stream / assistant output | screen | **NOT CHANGED** | the model's own words; the surface must never decide their emphasis |
| status row, prompt, editing line | screen | **NOT CHANGED** | owned by `lineedit.Palette`; a second emphasis decision here would be the second table Rule 14 forbids |
| `chat_notice_sink.go` (driver notices) | **file** (`console.log`) when filming, else stderr | **NOT CHANGED — and it must not be** | its whole purpose is to be a RECORD. A log carries no SGR: an escape sequence in a log file is noise that no reader parses and every `grep` trips over |
| `chat_film*.go`, `chat_dump*.go`, `chat_diagnostics.go` | **files** | **NOT CHANGED — must not be** | artifacts. `chat-screen.dump` is read back by `uiclient`, and its attribute grid records style as DATA (`AttrRuns`); a sequence painted into the dump's text would both corrupt the artifact and be measured as cells |
| `chat_session_budget.go`, `chat_session_slots.go`, `chat_policy_flag.go` | stderr, **before** the REPL exists | **CHANGED → dim** (the surface's own line only; refusals stay plain) | these fire during start-up, before any `chatInput` exists, so they could not read a table off the input. The fix is neither a second resolution nor a copy of the table: `runChat`'s ONE `lineedit.CapabilityForEnv` call was **hoisted above them** and its `termToolStylesFor` table handed DOWN as a parameter (`publishChatSessionBudgetDefault(st)`, `chatSessionSlotRegister(path, st)`, `chatAnnounceChatPolicy(…, st)`), and each writer draws through `termWriteRows` → `termRow.Row()`. Only the line that is the surface reporting a fact about itself wears dim; the three REFUSALS on the slot path (replay, unresolvable profile, registration error) and the policy NOTES/parse errors stay plain — a failure dressed as a normal state is the mirror of the emphasis the operator's ruling forbids |
| `chat_prefetch*.go` | status row / notes | **NOT CHANGED** | its rows already go through the REPL's own notice roads; nothing in it composes a row independently |

## The two-part contract every changed channel satisfies

1. **Render ≠ CopyText.** A `termCell` carries text and a `sgr.Style`; the
   sequence is applied by the DRAWER (`termRow.Row`, or `lineedit.StyleFromSGR`
   at the band seam). No cell's text ever contains an escape byte, so a copy of
   the row is the row's characters.
2. **No colour, no bytes.** `termToolStylesFor(lineedit.ColourOff)` yields the
   `sgr.Default()` table and `On=false`; the band then receives runs with an empty
   style and the flow path writes the concatenated text. Measured: 0 escape bytes
   on `NO_COLOR=1 TERM=dumb`, and `cmp` against the pre-change binary's stream is
   byte-identical.

## What no longer decorates a copyable line

`/info`'s block wraps on a narrow band (a session path is longer than 76 cells),
and the band's note path opens a wrapped row with `↳ ` — a marker INSIDE the cells
a selection carries. `noteRow.copied` / `AnswerRenderer.NoteRunsCopied` /
`Editor.NoteRunsCopied` / `chatInput.WriteNoticeRuns(…, copied=true)` carry "this
row is a paste source" through the QUEUE (`answerOp.copied`), because the read
loop is the applier and a property that did not travel would be silently dropped
on the path production takes. A `/voice` notice keeps the marker (it is not a
paste source; the marker tells the operator a wrapped remark is one remark).

## Remaining work (honest)

- **The reserved layout opens the block one row lower than master did** (measured
  2026-10-22 on a real PTY grid: `/info` at row 11 through `WriteLines`, row 12
  through the note path; `/voice` 25 → 26). The characters are identical — the
  piped/no-colour path is byte-identical — but an emphasis change must not move
  the layout, so this is a defect of this work, not a nicety. The request row,
  the in-flight prefix carry and the wrap cut were each eliminated by
  measurement and are now guarded (`notesAllCopied`, `cutNoteRowCopied`); what
  remains is a first-scroll difference between `region.writeBandLine` and
  `AnswerRenderer.writeNote`, filed with its reproducible command.
- **The three start-up writers are DONE** (`chat_session_budget.go`,
  `chat_session_slots.go`, `chat_policy_flag.go`), on the terms this document
  named: the capability is resolved ONCE in `runChat`, above them, and the table
  is handed to each writer as a parameter — no call site resolves a capability of
  its own. Measured, on the real binaries, both ways round:
  - **no colour, no bytes**: `NO_COLOR=1 TERM=dumb`, the pre-change and
    post-change binaries driven with `chat --verbose --policy on-request`, stderr
    3842 B each, `cmp` **byte-identical** after normalising the run's own facts
    (UUID, profile path, slot index), 0 ESC bytes
    (`artifacts/startup-colorless.sh`);
  - **colour changes the emphasis and nothing else**: a real PTY grid
    (`devdoc/evidence/sgr-grid/termgrid.py`) shows all three lines painted
    `default` on the pre-change binary and `dim` on this one, with the grid TEXT
    identical between the two (`artifacts/startup-pty.sh`).

  The one thing this does NOT re-measure is where the lines sit: hoisting the
  resolution moved no print, and the earlier row-shift finding above is a
  property of the note path, not of these three writers (they go straight to a
  plain writer, `termWriteRows`, and never enter the band's queue).
