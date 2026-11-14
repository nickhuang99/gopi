<!-- doc-class: historical -->
# Task Report: U2 — the chat stream's fallback renderer bounds its rows to the terminal width

Branch: `fix/chat-stream-app-wraps` (worktree `/home/user/workspace/gopi-u2-wrap`)
Code commit: `87b8bf3d`; this report lands in the follow-up docs commit.

## Completed

**The wire** — `cmd/agent/chat_stream.go`:

- `chatRenderer.rows` (int) is the width this renderer's rows are bounded to.
  It is decided **once per turn**, in `newChatTurnRenderer`:
  `r.buffered.rows = wrapColsFor(out, r.region != nil)`. A plain field, not a live
  ioctl: a row already written cannot be re-wrapped without repainting it, so the
  width can only change *between* turns — and the next turn re-reads it.
  `rows == 0` (the default) is the frozen path.
- `emit(text)` is now the renderer's **single write point**, replacing the three
  `fmt.Fprint(r.out, s)` calls. `rows <= 0` → `fmt.Fprint(r.out, text)` byte for
  byte; otherwise each row is written followed by its break. The three flush
  points (last newline / word window / buffer size) and their cadence are
  untouched — only *where a row ends* moved.
- `wrapStreamText(text, cols) []streamRow` splits per answer line
  (`strings.Split(text, "\n")`); each line is cut by **`lineedit.WrapToCols`** —
  the reserved answer band's own policy (Rule 14: no second wrapping
  implementation). `streamRow{text, breakText}` carries the break that ends the
  row, named as constants (Rule 1):
  - `chatRowBreakLF` — the answer's own line break, emitted unchanged;
  - `chatRowBreakCRLF` — a row only the **width** ended. The CR matters: a row of
    exactly `cols` cells leaves the terminal in its pending-wrap state, where a
    bare LF advances **two** rows and prints a blank row under every full row.
  - `""` — the reading unit simply ran out mid-row (a word-window or buffer
    flush); the next unit's bytes land on the same row.

**The width predicate** — `cmd/agent/chat_width.go` (new), one place, one rule:

- `wrapColsFor(out io.Writer, reserved bool) int` returns **0 = frozen path**
  unless every one of these holds: `out` is an `*os.File`; that file is an
  interactive terminal (`isTerminal`, the termios-backed predicate the REPL
  already uses); the reserved answer band is absent; and
  `lineedit.TerminalCols(fd)` answers `>= chatWrapColsMin`. A buffer, a pipe, a
  regular file, `/dev/null`, an unknown width and a reserved band all get **0** —
  "leave this writer alone", never a guess.
- `chatWrapColsMin` (named, Rule 1) rejects a bad ioctl reply (0 = "the ioctl
  failed") rather than wrapping to a sliver.

**`internal/lineedit`** — two symbols exported instead of re-derived (Rule 14),
plus tests:

- `winsize.go`: `TerminalCols(fd int) int` — the same `TIOCGWINSZ` request
  `Terminal.Size` makes; **0 means unknown**, never a width.
- `answer.go`: `WrapToCols(s, width) []string` — a documented, tested export of
  the band's `wrapCols` (`width <= 0` = no wrapping; a wide rune is never split).
- `winsize_export_test.go` (new): the row bound + no-loss invariant, `0` = no
  wrapping, a wide rune at an odd width (7 cells → 3+3+1 runes), and
  `TerminalCols` = 0 on a pipe and on a regular file.

**Tests**

- `cmd/agent/chat_stream_wrap_test.go` (new) — the wrapped half asserted on the
  **rendered screen** (Rule 10.2: the emitted bytes are replayed through
  `lineedit.Screen`, the vt100 cell grid, and the check is on the cells a reader
  sees; "the bytes got shorter" cannot see where a row broke). The frozen half is
  asserted as **bytes**, because on a non-TTY the byte stream *is* the contract.
  The reference for the frozen half is the historic emit written out in the test,
  not a paraphrase of it.
- `testsuite/chat_wrap_pty_test.go` (new) — the wire level end to end: a real
  `gopi chat --no-editor` child on a `/dev/ptmx` pair with `TIOCSWINSZ` set to 40
  columns, asked for a 200-cell answer with no break opportunity. The rows the
  terminal receives must each be ≤ 40 cells, the widest exactly 40, five rows in
  total, and their concatenation is the answer byte for byte.

## Not Done (honest)

- **A mid-turn resize does not re-wrap a turn already in flight.** The width is
  read once per turn. Deliberate: committed rows cannot be re-wrapped without
  repainting them, and "a committed row is never repainted" is this path's
  contract. A resize takes effect on the next turn; the bounded edge is a resize
  during one very long answer, whose remainder keeps the old width. (The
  prior-art branch `rework-backup/chat-stream-tty-wrap` reached the same
  conclusion after prototyping a SIGWINCH registration and removing it — read,
  not merged.)
- **The reader half of the predicate is a heuristic.** `wrapColsFor` decides by
  the *writer*: a terminal on stdout is wrapped whether its stdin is a terminal
  or a script's pipe. That was a deliberate choice over the prior art's
  `isTerminal(os.Stdin)` conjunct. The prior art's stated reason for the conjunct
  was "the reserved layout is reader-driven, so a terminal-stdout + piped-stdin
  run must keep the historic stream" — but that case is exactly a long answer
  arriving at a terminal that will fold it, which is the defect U2 names, and the
  frozen half's real contract is the *non-TTY* stream (a pipe, a file, a buffer),
  which this predicate still returns 0 for unconditionally. So the conjunct was
  dropped on purpose. The residual risk is a harness that gives a child a pty on
  stdout with piped stdin *and* compares stdout bytes to a pre-change recording;
  `testsuite/` does not (it reads the pty and asserts on rows), and the byte
  equivalence claimed below is measured on pipes, which are unaffected.
- **`displayWidth` was NOT exported.** The prior art renamed `displayWidth` →
  `DisplayWidth` (10 in-package call sites) so the new cmd/agent test could
  measure cells. This branch keeps the lineedit diff to the two symbols the wire
  actually needs: the wrap fixtures are ASCII (1 byte = 1 cell) and the CJK case
  measures its own wide runes explicitly, so the test needs no exported
  measurer. Exporting a symbol no production code calls would be surface for
  one test's convenience.
- Status line, prompt, `--help`, the reserved band's own rendering and the
  web/term carriers are untouched.
- No `SIGWINCH` handler, no terminal library, no new dependency (Rule 10.7 —
  `internal/lineedit` was already the owner of the ioctl).

## Verification

**One-line run command** (from the worktree root):

```
go build ./... && go vet ./... && gofmt -l . && go test -count=1 -run 'TestChatStreamWrap|TestWrapToCols|TestTerminalCols' ./cmd/agent/ ./internal/lineedit/ && GOPI_BIN=$PWD/bin/gopi go test -count=1 -run TestChatPTYWrap ./testsuite/
```

## Evidence (self-filed)

- **Build**: `go build ./...` → no output (exit 0). `CGO_ENABLED=0 go build -o
  bin/gopi ./cmd/agent/` → `BUILD_OK`.
- **Vet**: `go vet ./...` → `VET_DONE` with no diagnostics above it (exit 0);
  `go vet ./testsuite/` → no output.
- **gofmt**: `gofmt -l .` lists six files — the *same* six on master
  (`cmd/agent/serve_lock_replacement_test.go`,
  `cmd/agent/worker_stop_forward_regression_test.go`,
  `devdoc/voice-zerodep-poc/model_embed.go`, `model_embedq5.go`,
  `internal/tools/task_staleness_guard_test.go`,
  `internal/tools/taskledger_cost_test.go`). None is touched by this branch.
- **Red, wire reverted** — `emit`'s wrap branch replaced by the pre-change body
  `fmt.Fprint(r.out, text)`, then `go test -count=1 -run TestChatStreamWrap
  ./cmd/agent/`:
  - `--- FAIL: TestChatStreamWrap_RowsNeverExceedTheTerminalWidth` —
    `an emitted row is 200 cells wide, wider than the 40-column terminal`
  - `--- FAIL: TestChatStreamWrap_RegressionEmitsRowBreaks` — `the wrapped
    stream carries 0 row breaks, want 4 — the answer was emitted unwrapped`
  - `--- FAIL: TestChatStreamWrap_SoftWrapDoesNotDoubleAdvance` — `the width
    break is not "\r\n"…`
  - `--- FAIL: TestChatStreamWrap_NeverSplitsAWideRune` — `first row holds 30
    runes, want 20 wide runes`
  - `--- FAIL: TestChatStreamWrap_MultilineAnswerKeepsTheAnswersOwnBreaks` —
    `the stream carries 2 line feeds, want 3`
  - `--- FAIL: TestChatStreamWrap_NonTTYOutputIsByteIdentical` — `a width was
    set but the rows were not broken: the wrap wire is not active`
  (Written to the worktree, observed, then restored from a copy — the restored
  file is the committed one.)
- **Red, wire reverted at the BINARY level** — the same branch's `testsuite`
  case run against a **master** binary (built by `git archive master | tar -x`
  into `/home/user/workspace/gopi-test-scratch/u2-verify/master-src`, built there)
  with `GOPI_BIN=<master bin> ./testsuite.test -test.run TestChatPTYWrap -test.v`:
  `--- FAIL: TestChatPTYWrap_BoundsRowsToTheTerminalWidth (4.21s)` —
  `an emitted row is 200 cells wide, wider than the 40-column terminal — the row
  was left to the terminal's auto-wrap`. With `GOPI_BIN=<this branch's bin>`:
  `--- PASS: TestChatPTYWrap_BoundsRowsToTheTerminalWidth (2.49s)`.
- **Green, wire in place**:
  `go test -count=1 -run TestChatStreamWrap ./cmd/agent/` → `ok
  github.com/nick/gopi/cmd/agent 3.295s`;
  `go test -count=1 -run 'TestWrapToCols|TestTerminalCols|TestChatStreamWrap'
  ./internal/lineedit/ ./cmd/agent/` → `ok …/internal/lineedit 1.321s`,
  `ok …/cmd/agent 0.704s`;
  `GOPI_BIN=$PWD/bin/gopi go test -count=1 -run TestChatPTYWrap ./testsuite/` →
  `ok github.com/nick/gopi/testsuite 3.559s`.
- **Pipe equivalence, measured** — a real deterministic answer (220 words, no
  ANSI) served by a stdlib-only local SSE server (`fake_llm.py`, `python3`),
  fed to both binaries with `printf 'write a long answer\n/quit\n' | … chat
  --no-editor` (stdout redirected to a file, i.e. the non-TTY path):
  `out-master.txt` = 1234 bytes, `out-wrap.txt` = 1234 bytes,
  `cmp out-master.txt out-wrap.txt` → identical (`PIPE_IDENTICAL`, exit 0).
  (The server was `pkill`ed after the measurement.)
- **A pre-existing failure in the full `./testsuite/` run, unchanged by this
  branch**: `TestChatPTY_EditorRendersAndSubmits` fails with `the editor did not
  announce itself on a TTY; output so far:` (0.04s). The same case run against a
  **master** binary in the same harness fails the same way at 0.09s, so the
  branch did not introduce it; the rest of the pty family
  (`TestChatPTY_ResizeDoesNotFreezeTheEditor`, `TestChatPTY_PasteBurstIsOneTurn`,
  `TestChatPTYWrap_BoundsRowsToTheTerminalWidth`) and 19 of the run's 20 cases
  pass. Reported, not fixed: it is not part of U2's wire.
- **The stylegate is red on master too, with the same findings**: 260 findings on
  each side, `diff` of the two sorted finding lists empty, and zero of them naming
  a file this branch touches.

## Files

- `cmd/agent/chat_stream.go` — the wire (`rows`, `emit`, `wrapStreamText`, `streamRow`, the two break constants).
- `cmd/agent/chat_width.go` — new: `wrapColsFor`, `chatWrapColsMin`.
- `internal/lineedit/answer.go` — `WrapToCols` export.
- `internal/lineedit/winsize.go` — `TerminalCols` export.
- `cmd/agent/chat_stream_wrap_test.go` — new (7 cases).
- `internal/lineedit/winsize_export_test.go` — new (4 cases).
- `testsuite/chat_wrap_pty_test.go` — new (1 pty case).
- `devdoc/chat-stream-app-wraps.md` + its `devdoc/DOC-INDEX.tsv` row — this report.

Branch left **unmerged**, per the briefing. Prior art (`rework-backup/chat-stream-tty-wrap`)
was read for its reasoning and deviates deliberately (see Not Done).
