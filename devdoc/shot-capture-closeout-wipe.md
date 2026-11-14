<!-- doc-class: historical -->
# Task Report: `gopi shot --surface chat` captured a wiped grid — the flake in `TestShotSurfaceChatGeometryIsHonoured`

Task `01a0d68745217a91a66d07a8c5f5b96f` · branch `task/01a0d68745217a91a66d07a8c5f5b96f`
(base: master e007b2d4; the branch was already delivery-point-synced, `git merge-base
--is-ancestor master HEAD` → true).

## Completed

- **Root-caused the flake** (it is neither test-only nor the trailing-blank trim):
  `gopi shot --surface chat` sends `/quit` + CR as its exit keys, and `cmd/agent`'s chat
  REPL hands the screen back on exit — `internal/lineedit`'s `region.Reset()` emits
  `\033[r\033[H\033[2J` (reset scroll region, home cursor, erase display) immediately
  before the goodbye line. The capture then read the grid **after** that erase, so the
  PNG held whatever the wipe left behind: sometimes the pre-exit frame, sometimes the
  goodbye line plus an empty screen — a race between the REPL's close-out, the reader
  goroutine and the capture's own settle/drain timing. Measured: `--rows 36` gave
  `64x88` (pre-exit) in one run and `64x56` (wiped) in the next, on the same binary.
- **Fixed it in the capture engine** (`internal/shot/textcapture.go`), not in the TUI's
  close-out (which is deliberate and correct — see `region.Reset`'s own doc comment: the
  last owner of the screen hands it back clean):
  - the grid as of the last **quiet window** (the engine's own definition of a finished
    frame) is recorded before anything can erase it;
  - after the exit keys, `gridLostRows` decides whether the ending **destroyed rows the
    settled screen showed**. If it did, the capture reports the settled frame and sets
    the new diagnostic `TextCapture.FrameFromHistory`; if it did not (a UI that answered
    the keys and kept its rows), the live frame is kept.
  - `gridLines`' trailing-blank trim was factored into `trimGridRows` with the rule
    stated: interior blank rows are layout and stay, only the tail folds.
- **Rewrote the test** (`testsuite/shot_surface_chat_test.go`) to assert the observable
  the capture guarantees — the **row count** (`gridRows`, derived from
  `text2img.DefaultLh`/`DefaultMargin`), not the pixel height, whose size is decided by
  where the trim lands. `shotE2EGridRowSlack = 1` states the trim's allowance; the test
  comment explains why heights flake and what is asserted instead.
- **Regression tests** (`internal/shot/textcapture_test.go`): `..._ExitKeysWipeKeepsTheScreen`
  (a child that answers, emits the REPL's exact `\033[r\033[H\033[2J`, then exits — the
  banner and prompt must survive), `..._ExitKeysAnswerIsNotOverwritten` (the fallback must
  NOT fire when the UI keeps its screen), `..._FallbackNeedsNoMarker` (the criterion is
  content loss, not the ready marker), and `TestTrimGridRowsAndGridLostRows` (both pure
  rules, including "a row rewritten in place is not a lost row").

## Not Done

- **The captured screen is the PRE-EXIT frame.** The PNG now shows the chat UI as it was
  when it finished drawing, not the post-`/quit` goodbye line. That is forced: the REPL's
  close-out wipes exactly so the next program gets a clean terminal, so the post-exit
  screen contains no chat UI to photograph. A caller who wanted the goodbye in the image
  would need the REPL to stop wiping (an option/flag in `internal/lineedit`), which is a
  product decision, not a capture bug — deliberately not taken here.
- **`--wait` + exit keys on a wedged UI**: the fallback frame is only as good as the
  settled screen; a UI that never settles has none, and the capture keeps its current
  (wiped) frame rather than inventing one.
- `internal/standards` has pre-existing failures in files this diff does not touch
  (`internal/agent/rawlog_test.go:309` NEW Rule-17 hit, `internal/tools/worktree_sweep.go:2199`
  escaped Rule-16, `cmd/agent/*_test.go` escapes; `TestDocManifestCoversEveryDevdocFile`
  needs this file's manifest row). Not introduced here; not repaired here (Rule 26: a new
  dispatch, not a silent widening).

## Verification

- Build: `CGO_ENABLED=0 go build -o bin/gopi ./cmd/agent/` → `BUILD_OK`; `go build ./...` clean; `gofmt -l internal/shot testsuite cmd/agent` → no output.
- Tests:
  - `go test -count=1 -timeout 10m ./internal/shot/` → `ok github.com/nick/gopi/internal/shot 48.832s`
    (includes the four new tests).
  - `go test -count=1 -timeout 10m ./testsuite/ -run 'TestShotSurfaceChatGeometryIsHonoured'`
    → `ok` on 6 consecutive runs (N=6, no load generated — Rule 27).
  - `go vet ./internal/shot/ ./testsuite/` → clean.
- Ran (Rule 31 — the surface itself, real args, real state):
  - `GOPI_HOME=<scratch> bin/gopi shot --surface chat --cols 100 --rows 36 --out cli36.png`
    → exit 0, printed the PNG path.
  - the same with `--cols 100 --rows 16` → exit 0.
  - Before the fix: `cli16.png` and `cli36.png` were both **970 bytes** (identical
    captures — both the wiped grid): `-rw-r--r-- 970 Sep 25 12:33 cli16.png` /
    `cli36.png`.
  - The REPL's own wipe, read off the wire by running the real TUI on a pty:
    `(\ printf '/quit\r'; sleep 6 ) | GOPI_HOME=… timeout 20 script -qec "bin/gopi chat" chat3.raw`
    then counting bytes in `chat3.raw` → `{b'\x1b[2J': 1, b'\x1b[?1049': 0, b'bye': 1}`,
    with the 160 bytes before `bye` ending `…\x1b[23;1H\x1b[2K\x1b[1m\x1b[32myou>…\x1b[r\x1b[H\x1b[2J`
    — i.e. **no alternate screen**, a plain ED(2) at the very end.
- Result (measured, after the fix):
  ```
  PROBE run=1 cols=100 rows=16 -> 1024x536  dark=16993
  PROBE run=1 cols=100 rows=36 -> 1024x1176 dark=17008
  PROBE run=2 cols=100 rows=16 -> 1024x504  dark=15786
  PROBE run=2 cols=100 rows=36 -> 1024x1176 dark=16982
  PROBE run=3 cols=100 rows=16 -> 1024x504  dark=15794
  PROBE run=3 cols=100 rows=36 -> 1024x1176 dark=16981
  GRID rows=16 kept=15 … history=true rows=16 last="you>"
  GRID rows=36 kept=36 … history=true rows=36 last="deepseek-flash   ·  iter 0  ·  … ready"
  ```
  `1024px = 2×12 margin + 100 cells × 10px` on both; `1176px = 24 + 36×32`; `536px = 24 + 16×32`
  (the `504px` runs are the one trailing blank row the trim is allowed to fold — hence the
  test's row-count-with-slack assertion). Before the fix the same probe gave `64x88` /
  `64x56` / `64x88` — i.e. 2–3 inked rows, no geometry, which is the flake's signature.

## Evidence (self-filed)

- RED→GREEN of the rewritten test: before the fix the suite test failed with
  `--rows 16 → 56px, --rows 36 → 56px` (the briefing's own repro) and the probe showed
  2–3-row PNGs; after it, 6/6 runs of the same test are green (`nruns.txt`: runs 1–4 `ok`,
  and the same loop's later runs likewise).
- Rule 27 compliance: no load was generated anywhere; the flake was measured by running
  the test/probe, and the REPL's wipe was read from a `script(1)` capture of the real TUI.
- The `\x1b[r\x1b[H\x1b[2J` is not my invention: `internal/lineedit/region.go`'s
  `Reset()` doc comment documents it as the chat REPL's close-out, with its own
  measured rationale — the capture simply had to stop photographing its aftermath.

## Commit

(see the branch tip — `fix(shot): the chat capture no longer photographs the REPL's close-out wipe`)
