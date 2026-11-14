<!-- doc-class: historical -->
# gopi chat debugged with its own eyes: the answer band owned the screen and two writers fought over it

Task: *"gopi should debug itself by running `gopi chat --tools` and using tools to
view this chat ui program … current gopi chat ui is ugly and unusable."*

Branch `feat/chat-ui-debug` (base `dc75ea64`). Everything below was measured on a
real `gopi chat --tools` run rendered into a real terminal, not read off the code.

## How gopi looked at itself (the instrument)

Two independent instruments, deliberately cross-checked because each one lies in
its own way:

1. **Pixels** — `Xvfb :99` + `xterm -u8` + `xdotool` typing + `import` → PNG →
   `vision_analyze`. Proves what a human actually sees.
   *Pitfall:* `LANG=C` mojibakes `·`/`⚙` (a capture artifact, not a UI defect) —
   `-u8` is mandatory.
2. **Bytes** — `evidence/termgrid.py`, a VT100 grid emulator over a real PTY:
   every escape sequence is applied and the resulting 100×30 character grid is
   dumped. Byte-exact, and the only instrument that can *name* the collision.
   *Pitfall:* `vision` misreads the status row (`·`→`Å:`) and cannot see a
   1-cell overlap; grid truth decides, PNG truth confirms.

Harness committed outside the worktree at
`~/workspace/gopi-chat-ui-debug-evidence/evidence/`
(`termgrid.py`, `fakellm.py` — a scripted Anthropic-SSE model so no real API is
touched, `20-capture.sh`, `20-screen.sh`, `21-capture-think.sh`).
**A capture must be scrubbed**: `env -u` the API keys and point `GOPI_HOME` at a
throwaway home per run. Two harness bugs cost real time and are recorded here so
nobody re-finds them:

- `$scrub` (a bash **array**) expands to element 0 only → the `20-capture.sh` run
  was NOT scrubbed and talked to the real API. Must be `"${scrub[@]}"`.
- A whole-line write followed by `\r` reaches `internal/lineedit` as a **paste
  burst**: the submitting Enter is eaten as content and the line never runs. Type
  character by character with small gaps.

## Defects (all four measured in `before-*.grid.txt` / `before-screen-*.png`)

**D1 — the answer band ate the screen and scrolled it.** `computePlan` bounded
the band by `regionRowsMax = 12`, so at 30 rows the band reserved 12 and left
**17 dead rows** between the band and the status row — the prompt floated at
row 12 of a 30-row screen. Worse, the agent's *stderr* writes were not part of
the layout: a 1-line `[gopi] ℹ️ thinking budget …` INFO wrapped over rows 11-13,
landed **inside** the band, and a wrapped write inside a scroll region scrolls
the whole screen — the banner disappeared and the prompt moved from row 12 to 14.
`before-screen-3` shows the status row carrying raw logger text
(`…chat table: chat row ensured: f21a827e-c…`).

**D2 — thinking and answers fought over the same row.** The reasoning block's
only separator was a trailing `\n`, and the answer band strips `\r`
(`Delta: strings.ReplaceAll(s, "\r", "")`), so a raw LF was just another region
scroll with **no column reset**: the next line started at the previous column and
the next turn's `[thinking]` label landed mid-line (grid rows 15-18:
`…[thinking] The file doesn't exist at that path…`). That interleaving *is* the
"flickering, unreadable" the user reported.

**D3 — the status row was a dumping ground** — `⚙ bash pwd; echo "---"; ls -la;…`
truncated at 32 runes with no width budget, and no token count at all while a
turn was running.

**D4 — the renderer duplicated text (a real bug, found by the instrument).**
`AnswerRenderer.drain` **painted** the in-flight tail on the live row *and*
re-queued it in `pending`, so the next drain committed already-displayed text a
second time. Trigger = one delta carrying a line break **and** the next line's
head — exactly what the endpoint sent. On the wire:

```
The path is devdoc/chat-mode-report.md, relative to the repo root, and the read tooThe path is devdo
c/chat-mode-report.md, …
```

## The fix, by defect

- **D1/D3 — one band, no cap.** `regionRowsMax` is deleted from
  `internal/lineedit/region.go`; the band takes every residual row, so the
  editing line sits directly above the status row (at 30 rows: `you>` at row 28,
  status at 29, nothing dead between). `region_yield_test.go` updated.
- **D2 — one writer per row.** Live reasoning for `gopi chat` is routed through
  the *same* renderer as the answer (`internal/agent/loop.go`: new `OnThinking`
  callback + `emitThinking`), so it is committed line by line inside the band and
  scrolls with the answer instead of fighting it. `cmd/agent/cli_chat.go`
  installs the callback **only when the band is active**; with `cb == nil` the
  stderr path is byte-for-byte unchanged (the `gopi p | tee` and task-run
  captures depend on it). ANSI dimming is suppressed on this path on purpose —
  the band counts display cells and would shear the row on an SGR sequence.
- **D1 — the agent's own diagnostics leave the screen.**
  `cmd/agent/chat_diagnostics.go` points `ag.Logger` at
  `<session-dir>/console.log` while the band is active (a front run over a TTY
  has no console record otherwise), and `closeChatDiagnostics` runs in
  `finishChat` *after* `ChatClose` so the closeout is still logged. Failure to
  open the file is reported, not swallowed (Rule 15).
- **D4 — the renderer carries what it already painted.** `pending` is emptied
  when the tail is painted; `liveText`/`liveRow` record what the live row
  displays, and a commit *carries that head forward* through the scroll instead
  of re-writing it. Guarded by
  `TestA5B_BreakDeltaWithATailDoesNotDuplicateTheTail`, which replays the exact
  captured delta shapes and asserts every character appears exactly once.

## Verification

- `CGO_ENABLED=0 go build ./cmd/agent/` → OK; `go vet` on `internal/lineedit`,
  `cmd/agent`, `internal/agent` → clean.
- `go test ./internal/lineedit/` → ok; `./cmd/agent/ -run 'Chat|chat|Diagnostic'`
  → ok; `./internal/agent/ -run 'Thinking|OnThinking'` → ok; all 18 `TestA5B_*`
  pass.
- **RED-proof for D4**: `git stash push internal/lineedit/answer.go` → the test
  fails printing the duplicated row; `git stash pop` → passes. A fix without a
  red first is an assertion, not evidence.
- Same scripted session, before/after, 100×30: BEFORE boot = INFO wrapped over
  rows 0-2, prompt row 12, 17 dead rows; AFTER boot = banner row 0, prompt row
  28, status row 29. AFTER answer: answer rows 12-26, status row reads
  `…· ⚙ read /home/user/work/GoPi/dev…  thinking…` with **no logger text**.
  `vision_analyze` on `after-screen-3-answer.png`: 17 lines, prompt second to
  last, "no overlapping or duplicated text".
- Diagnostics redirect proof: `evidence/home-after/.gopi/sessions/<uuid>/console.log`
  holds exactly the three lines that used to paint the screen (thinking-budget
  INFO, "chat row ensured", "ended (normal)"); the BEFORE tree has no
  `console.log` at all.

## Not done (honest)

- **Thinking is not visually distinguished** in the band (no dim/prefix) — the
  marker would cost display cells and shear the row. It is a follow-up, not a
  silent omission.
- **D3 is only half fixed**: the band now reaches the status row, but the status
  row still truncates a long tool command at a fixed rune count with no width
  budget, and still shows no token count during the first turn.
- **Small terminals** (< ~12 rows) were not captured; the plan's minimum paths are
  unit-tested, not screenshot-verified.
- Sibling-run interference (two `gopi chat` runs writing one session dir) was
  observed once in the raw capture and is **not** reproduced in the grid run —
  recorded as a hypothesis, not a claim.
- Two failures on unmodified master (`TestRule13_NoBareCharsPerTokenLiteral`,
  `TestDocAnchorsResolveInNormativeDocs`, verified in a separate worktree at
  `dc75ea64`) are pre-existing and not touched by this diff.
