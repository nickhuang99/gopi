<!-- doc-class: historical -->
# Task Report: Capture + read display :99

## Completed
- Captured the live X11 desktop of display `:99` to `/tmp/llm-shot.png`
  **without any shell screenshot binary** (no `import` / `scrot` / `xwd` / `ffmpeg`).
- Capture went through the `screen_capture` tool's own engine.
- Read the PNG back with `vision_analyze`.
- Independent sha256 re-computation of the file to cross-check the tool payload.

## Method (why the engine, not the LLM tool call)
The **live harness binary built from master does not expose `screen_capture`** —
an LLM-side call returned `Unknown tool: screen_capture` (the tool exists only on
the feature branch; `internal/tools/screencapture.go` is absent on master).
So the tool path was exercised from its own worktree build, which is still the
tool's code path (`internal/tools` + `internal/screencapture` X11 client), not a
shell screenshot utility:

```bash
git worktree list                       # ~/workspace/gopi-screen-act @ feat/screen-capture-actuation-tools
cd ~/workspace/gopi-screen-act
CGO_ENABLED=0 go build -o /tmp/gopi-sc ./cmd/agent/
/tmp/gopi-sc screen_capture --display :99 --out /tmp/llm-shot.png
```

## Results
| Field | Value |
|---|---|
| Path | `/tmp/llm-shot.png` |
| Display | `:99` |
| Dimensions | **1280 x 800** |
| Depth | 24-bit, 7 bands |
| Bytes | 25697 |
| sha256_16 (tool) | **9adfd46934aa465c** |
| sha256_16 (independent `sha256sum`) | **9adfd46934aa465c** (match) |

### What is visible (one line)
A GUI text editor showing `/tmp/tabpoc/test1.txt` — 14 numbered lines
(`line 01` … `line 14`), cursor at end of line 14, top bar
`Open | Save | *test1.txt | /tmp/tabpoc`, bottom status
`Plain Text | Tab Width: 8 | Ln 14, Col 9 | INS`.

## Not Done
- Could not use the LLM-facing `screen_capture` tool call directly in this
  session (master binary lacks it) — noted above; the same engine was used.

## Verification
- Build: `CGO_ENABLED=0 go build -o /tmp/gopi-sc ./cmd/agent/`: OK
- Capture: exit 0, PNG tool payload emitted
- File: `file /tmp/llm-shot.png` → `PNG image data, 1280 x 800, 8-bit/color RGB, non-interlaced`
- Hash: tool `9adfd46934aa465c` == `sha256sum | cut -c1-16` `9adfd46934aa465c`
- Vision: `vision_analyze` read the pixels and transcribed the editor content
