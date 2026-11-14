<!-- doc-class: historical -->
# Task Report: report what is on display :99 right now

## Completed
- Captured the **live X11 desktop of display `:99`** to `/tmp/llm-shot.png`
  using the `screen_capture` **engine** (pure-Go X11 GetImage client — no
  external screenshot binary, no shell screengrab).
- Read the PNG with `vision_analyze` and reduced it to a one-line summary.
- Reported the three required facts: pixel dimensions, `sha256_16`, visible content.

## Delivered facts (the answer)

| field | value |
|---|---|
| path | `/tmp/llm-shot.png` |
| width × height | **1280 × 800** (depth 24, 7 bands, 25697 bytes) |
| sha256_16 | **`9adfd46934aa465c`** |
| one-line summary | A GNOME Text Editor window is visible, editing `/tmp/tabpoc/test1.txt` with sequential lines `line 01` … `line 14` and the caret at the end of line 14. |

Full sha256 of the PNG:
`9adfd46934aa465c280711a7c34a07b9ca77320556cd8f9c659c68936ff500c5`
(`sha256_16` = first 16 hex chars, per `screenCaptureHashLen = 16`).

## How the capture was made (why not the LLM tool call)
- `screen_capture({display:":99", out:"/tmp/llm-shot.png"})` returned
  **`Unknown tool`**: this session is served by the **master-built harness**,
  and `internal/tools/screencapture.go` does not exist on master
  (`git show master:internal/tools/screencapture.go` → missing). The tool is
  registered only on the unmerged branch below.
- Workaround (same engine, same wire word, same JSON contract — **not** a shell
  screenshot program): built the worktree binary
  `CGO_ENABLED=0 go build -o /tmp/gopi-sc ./cmd/agent/` and ran its CLI mirror
  `/tmp/gopi-sc screen_capture --display :99 --out /tmp/llm-shot.png`.
- No `import` / `xwd` / `scrot` was invoked (`import` and `xwd` merely appear in
  a `command -v` presence probe; they are never executed).

## Verification
- Build: `CGO_ENABLED=0 go build -o /tmp/gopi-sc ./cmd/agent/` → **OK** (exit 0).
- Capture (tool's own ground truth):
  `{"path":"/tmp/llm-shot.png","display":":99","width":1280,"height":800,"depth":24,"bands":7,"bytes":25697,"sha256_16":"9adfd46934aa465c"}`
- **Independent** file truth — the PNG header decoded with stdlib Python
  (`struct.unpack('>II', data[16:24])`) → **1280 × 800**, 25697 bytes on disk
  (so the reported dimensions are the real image, not a hardcoded claim).
- **Independent** hash: `sha256sum /tmp/llm-shot.png` →
  `9adfd46934aa465c280711a7c34a07b9ca77320556cd8f9c659c68936ff500c5`,
  whose first 16 hex chars equal the reported `sha256_16`.
- **Stability / liveness**: two consecutive captures (`/tmp/capA.png`,
  `/tmp/capB.png`) both hashed `9adfd46934aa465c` → the screen is unchanged
  between captures, i.e. `:99` is live and static (not a stale artifact).
- Vision: `vision_analyze(/tmp/llm-shot.png)` (deepseek-flash-vision) → GNOME
  Text Editor showing `test1.txt` (`/tmp/tabpoc`), 14 numbered lines
  `line 01`…`line 14`; top bar `Open | Save | *test1.txt | /tmp/tabpoc`; status
  bar `Plain Text | Tab Width: 8 | Ln 14, Col 9 | INS`. Confirms the PNG is real
  screen content, not blank.
- Commit: `docs: report display :99 capture (sha256_16 9adfd46934aa465c)` on
  `feat/screen-capture-actuation-tools`.

## Not Done / honesty notes
- The **`screen_capture` LLM tool itself** was not registered in this session's
  tool table (master binary predates the feature), so the tool call could not be
  exercised directly; the identical engine was reached through its CLI mirror.
  Rule 11 (LLM-adoption proof) therefore still needs a session running a binary
  that *does* register the tool — a master merge of the branch, or running
  `./bin/gopi` from the built worktree.
- `/tmp/llm-shot.png` was already present from an earlier run with the **same**
  hash `9adfd46934aa465c`; this run regenerated it from scratch (identical
  bytes/`mtime` semantics — X11 screen content was unchanged).

Recovery: `git show feat/screen-capture-actuation-tools:devdoc/screen-capture-llm-shot-report.md`
