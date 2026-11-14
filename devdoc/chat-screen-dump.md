<!-- doc-class: historical -->
# Task Report: `gopi chat --dump-screen` — the chat surface's own self-observation

## Completed

- **`gopi chat --dump-screen[=PATH]` (+ `GOPI_CHAT_DUMP`)** — the chat REPL arms the
  existing `uiclient.ScreenDumper` and tees its real paint stream into it, writing
  `chat-screen.dump` (+ `.json` twin) so `gopi ui_state` can read the chat surface
  as text instead of pixels.
  - Only the **joined** form takes a path: `--dump-screen=/a/path`. The separated
    form is refused **by name**, because chat's positional argument is a session
    seed and `--dump-screen /a/path` would silently swallow it.
  - The tee sits at `newChatInputWithDumper` (`chatDumpWriter(out, dumper)`), i.e.
    at the **editor's own writer** — the layout's only writer. A **nil sink
    returns the caller's writer unchanged**, so a run without the flag is
    byte-identical by construction, not by review.
- **A `Surface` discriminator** (`internal/uiclient`): `ui` | `chat`, carried in the
  dump's provenance prefix (`#gopi-chat-dump …` vs `#gopi-ui-dump …`) and
  cross-checked against the file NAME by `ReadDump`, so `ui_state` can never report
  a chat dump as a ui dump (nor a false ambiguity between them).
- **`gopi ui_state --target ui|chat`** + the surface column in `--list`, text and
  JSON.
- **Eight per-cell bold assertions** driven through the REAL render path
  (`chatTurnRenderer.Write` → `region.Delta` → the editor's paint stream →
  `lineedit.Screen` → the dump), covering all three mechanisms the epic names:
  half-row (`rowForPartial`), fence/diff (`rowFor`'s composer gate), and a
  multi-bold complete answer. The expected mask is derived from the SOURCE line's
  marker positions (using `lineedit.BoldMarker`, never a typed `**`); the measured
  mask comes from the dump's own attribute runs.
- **A real defect found and fixed by the live run**: the sink was frozen by the
  REPL's deferred finalize, which runs *after* `leaveChat` → `input.CloseOut()`
  has taken the layout down (region reset, scroll-region reset, cursor show). The
  frozen artifact therefore described the **teardown**, not the screen — a real run
  wrote 24 empty rows with `frames=16 dumps=5`, so every "the dump exists" check
  passed while the artifact said nothing. The freeze now happens in
  `chatInput.CloseOut`, *before* the terminal is handed back (Close is idempotent,
  so the deferred finalize re-freezes the same frame).

## Not Done (honest)

- **The chat dump is not yet scanned by `ui_state`'s default roots** when the dump
  lives in an explicit path outside a session dir — verified only for the
  session-dir placement (which is what the resolver chooses by default).
- **The fenced-line assertion checks "no cell is bold" and the literal text, not the
  code colour**: the cyan `LineCode` style is visible in the dump I quote below,
  but no test in this branch pins it per cell.
- **`GOPI_CHAT_DUMP` has no test of its own shape** (truthy vs path); it is covered
  indirectly by the shared resolver, and by the live run using the flag form.
- **No test asserts the frames=0 announcement text** on the cooked path (the live
  run shows it; the string is not pinned).

## Verification

- **Build**: `CGO_ENABLED=0 go build ./...` → OK. `CGO_ENABLED=0 go build -o bin/gopi ./cmd/agent/` → OK.
- **Tests**:
  - `go test ./cmd/agent/ -run 'TestChatDump_' -count=1` → `ok … 26.583s`
  - `go test ./internal/lineedit/ ./internal/uiclient/ -count=1` → `ok` / `ok`
  - `go test ./internal/standards/ -run 'TestConfigDoc' -count=1` → `ok` (CONFIG.md row added with a resolving line ref)
- **Ran** (Rule 31 — the surface invoked for real):

  ```
  SINK=/home/user/workspace/gopi-sandbox/.../chat-screen.dump
  python3 drive.py     # real /dev/ptmx, 40x100 winsize, stub SSE server on 127.0.0.1:8791
  → bin/gopi chat --dump-screen=$SINK   (ANTHROPIC_BASE_URL=http://127.0.0.1:8791)
  ```
  **Result** — the run's own report line and the artifact it wrote:

  ```
  #gopi-chat-dump-report path=…/chat-screen.dump json=…/chat-screen.dump.json
    bytes=8664 pid=1280805 grid=100x40 dumps=6 frames=146 blind_spots=8
  ```

  ```
  018|──── turn 1 · say the thing ───────…
  023|▌ here is the answer:
  025|▌ alpha and beta plain
  027|▌ ```
  028|▌ code **literal** line
  029|▌ ```
  031|▌ done
  038|you>
  039|deepseek-flash   ·  iter 1  ·  …  17 tokens  ready
  ```

  ```
  attrs_begin
  025|0:2=default 2:5=bold 7:5=default 12:4=bold 16:84=default
  027|0:2=default 2:3=fg=cyan 5:95=default
  028|0:2=default 2:21=fg=cyan 23:77=default
  038|0:4=fg=green bold 4:96=default
  ```
  i.e. `alpha` and `beta` are the bold cells (columns 2..17, the gutter at 0..1 is
  not), the fenced `code **literal** line` is cyan code with **no** bold cell, and
  the prompt is green+bold — the per-cell mask the tests assert, measured on a live
  terminal.

  `gopi ui_state --target chat` against that dump →
  `#gopi-ui-state surface=chat pid=1280805 … state=exited` + the rendered grid
  (exit 0); `gopi ui_state --list` lists it with `surf=chat`.

- **Red proofs** (each assertion shown to fail when the thing it measures is broken):
  - freeze order disabled (`CloseOut` freeze off) → `TestChatDump_FreezeHappensBeforeTheLayoutIsTakenDown` FAILS with the empty grid (the defect above).
  - `rowFor`'s marker/spans path disabled → the fence, multi-bold, half-row-decision and red-proof tests FAIL.
  - `rowForPartial` drawing resolved text instead of the raw segment → the half-row test FAILS.
  - `styleForSpan` returning the plain answer style → the multi-bold test FAILS.
  - the **monochrome palette** run (`TestChatDump_BoldMaskRedProofs`) is the in-test red proof: same characters, mask collapses to 0, and the marker-derived expectation still names the bold columns.

## Evidence (self-filed)

- `go test ./cmd/agent/ -run 'TestChatDump_' -count=1` → `ok github.com/nick/gopi/cmd/agent 26.583s` (all 7 tests pass).
- Red proof, freeze order off: `--- FAIL: TestChatDump_FreezeHappensBeforeTheLayoutIsTakenDown … the frozen dump does not carry the answer — the artifact describes the TEARDOWN, not the screen`.
- Red proof, `rowFor` gate off: `--- FAIL: TestChatDump_FenceAndDiffMarkersStayLiteral`, `TestChatDump_MultiBoldAnswerMarksExactlyItsSpans`, `TestChatDump_BoldMaskRedProofs`, `TestChatDump_HalfRowDecidesWhenTheBreakArrives`.
- Live run: the `attrs_begin` block quoted above (`2:5=bold` on row 025) is the measured mask from a real PTY run, `frames=146`.
- `gopi chat --dump-screen /a/path` (separated form) → `gopi chat: --dump-screen takes its path joined — write --dump-screen=/tmp/x …` (refused by name, exit 0 with usage).

## Files

- `cmd/agent/chat_dump.go` (new)
- `cmd/agent/chat_dump_test.go` (new)
- `cmd/agent/chat_stream_test.go`, `cmd/agent/cli_chat.go`, `cmd/agent/lineedit_wire.go`, `cmd/agent/cli_uistate.go`
- `internal/uiclient/dump.go`, `internal/uiclient/dumpread.go`, `internal/uiclient/dumpread_test.go`
- `CONFIG.md`
