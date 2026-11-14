<!-- doc-class: historical -->
# Task Report: live desktop captured to /tmp/llm-shot.png

## Completed
- Captured the LIVE X11 desktop (display `:99`) to `/tmp/llm-shot.png`.
- Reported the tool's own ground truth: width 1280, height 800, sha256_16 `1bc7e456c826ae7f`.

## Not Done
- Nothing deferred. (Note: the `screen_capture` *LLM tool* is not registered in
  this runtime — see Root cause — so the shell mirror `gopi screen_capture` was
  used; same engine `tools.ScreenCapture`, same wire word
  `toolnames.ToolScreenCapture`, identical JSON payload.)

## Root cause (why `screen_capture` tool path failed)
- `screen_capture({out:…})` and the no-arg form both returned `Unknown tool`.
- CWD `/home/user/workspace/gopi-screen-act` is the worktree of the unmerged
  branch `feat/screen-capture-actuation-tools` (HEAD `bbbeaa9`). The tool source
  exists there (`internal/tools/screencapture.go`,
  `cmd/agent/cli_screencapture.go`) but **not on master**
  (`git show master:internal/tools/screencapture.go` → does not exist).
- The running agent harness is served by the master-built binary, which predates
  the feature commit → the tool is unregistered in this session's tool table.
- Fix used: built the worktree binary (`CGO_ENABLED=0 go build -o /tmp/gopi-sc ./cmd/agent/`)
  and invoked `screen_capture` through its CLI mirror.

## Verification
- Build: `CGO_ENABLED=0 go build -o /tmp/gopi-sc ./cmd/agent/` → OK (exit 0)
- Capture: `/tmp/gopi-sc screen_capture --out /tmp/llm-shot.png` → exit 0, JSON:
  `{"path":"/tmp/llm-shot.png","display":":99","width":1280,"height":800,"depth":24,"bands":7,"bytes":30518,"sha256_16":"1bc7e456c826ae7f"}`
- Independent pixel/file truth: `identify` / `file` → PNG 1280x800, 30518 bytes;
  `sha256sum /tmp/llm-shot.png` → `1bc7e456c826ae7fb3805ca857bcf2f4e1ccaa1dcf4f11522d48fbf13441c987`
  (first 16 hex chars = `1bc7e456c826ae7f` — matches `sha256_16`, truncation is
  `src.SHA256 = hex(sum)[:screenCaptureHashLen]`, `screenCaptureHashLen = 16`).
- Commit: no commit made — the worktree carried another agent's untracked files
  (`devdoc/screen-capture-and-actuation-tools.md`, `internal/shot/x11_test.go`);
  the deliverable is the artifact `/tmp/llm-shot.png` + the values above.

## Delivered values
| d | value |
|---|---|
| width | 1280 |
| height | 800 |
| sha256_16 | 1bc7e456c826ae7f |

Branch carrying this report: `feat/screen-capture-actuation-tools`
(recover: `git show feat/screen-capture-actuation-tools:devdoc/screen-capture-live-desktop-report.md`).
