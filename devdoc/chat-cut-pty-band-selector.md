<!-- doc-class: historical -->
# The chat cut pty test's band selector: a start-up notice wearing the band's geometry

**Found** 2026-10-22 by task `01a16885-8a83-7e1c-8708-937d12073b93`, on master `23c19e0`.
**Surface** `testsuite/chat_cut_pty_test.go` (the wire-level guardian of the answer
band's right-edge cut label). **Product unchanged** — the defect was in the test's
SELECTION of the band rows, not in the renderer.

## The defect, measured

`TestChatPTYCut_MidWordCutIsMarkedOnARealTerminal` reads the child's bytes back
through `lineedit.NewScreen(117, 26)` and collects the answer band by asking one
question per row: *do the row's first two cells read `▌ ` or `  `?* That is the
answer band's own geometry — the turn marker on a line's first row, the same two
columns blank on a row the line wraps onto.

`[gopi] chat: slot 0 registered in …` (the chat session slot self-check line,
`cmd/agent/chat_session_slots.go`) is a START-UP notice written to stderr through
`termNoteRows` + `termWriteRows`. It is drawn at the terminal's own width and the
terminal wraps it like anything else, so **its second row also opens with blanks**.
Measured on the real pty:

```
band row 0|                                                                      [gopi] chat: slot 0 registered in /dev/shm/su
band row 1|fix/pushback-rule31-evid:alpha-beta_gamma.deltafix/pushback-rule31-evid:…
...
the band does not hold the answer whole: got 515 characters, want the token's 400
```

`515 - 400 = 115` — exactly the length of the notice's visible text that landed in
the collected band. Assertion (3) ("folding the label away, the band's characters
ARE the answer") is the one that caught it, and it caught it CORRECTLY: the band it
was handed really did contain a row that is not the answer.

The byte stream shows the notice is drawn in the SYSTEM voice
(`ESC[0;2;39;49m` — dim) on stderr, immediately after the banner and BEFORE the
first prompt, i.e. structurally outside the answer band; the notice is not in the
answer's column at all. Only the shape-based selector conflated them.

## The fix: anchor the run, keep the geometry check

The lead check stays (it is the necessary condition for a row being in the answer's
column) and gains a second condition the notice cannot satisfy: **the run opens on
the row whose body begins with the answer's own first characters**. Rows after that
first one are appended with no comparison against the token.

```go
if !inAnswer {
    if !strings.HasPrefix(body, chatCutSuiteTokenPieces) {
        return false // band-column row that is not the answer's first row
    }
    inAnswer = true
}
```

This is NOT a weakened assertion, and the red proof is the evidence: the completeness
assertion (3) is untouched. Mutating production
(`internal/lineedit/answer.go`, `tail = text[len(displayed):]` →
`tail = text[len(head):]`, i.e. "the character the label displaced is never
re-drawn") still fails the test — on assertion (2), the one that reads the row
below the mark:

```
the row below the mark does not open with the character the label displaced:
marked row ends "-rule31-", row below begins "vid:alph"
```

so a genuinely lost character is still caught. The mutant was restored byte-identically
(`git hash-object` == `git rev-parse HEAD:internal/lineedit/answer.go`).

## Why this shape and not the alternatives

- **Exclude notice rows by their text** (`strings.Contains(row, "[gopi]")`) — it
  hard-codes one notice's wording into the case; the next start-up notice would
  re-open the same hole.
- **Select by a marker column only (`▌ `, no blank lead)** — that drops every
  CONTINUATION row of a wrapped answer, i.e. it would break the multi-row reading
  the case exists to make.
- **Read only ONE turn's rows by the turn separator** — the answer band's turn
  separator (`──── turn N · …`) is chrome whose wording is not this case's
  subject; the answer's own first characters are.

## Rule 31 evidence

`go test ./testsuite/ -run '^TestChatPTYCut_MidWordCutIsMarkedOnARealTerminal$' -count=1 -v`
— three consecutive runs on the fixed tree, all `ok` (16.7s / 16.6s / 14.4s), with
the collected band printed each time as five rows: the four answer rows plus the
`-cut-end-` tail row, and **no** `[gopi] chat: slot 0 registered` row.
