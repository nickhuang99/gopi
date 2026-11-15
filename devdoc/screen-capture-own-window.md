<!-- doc-class: normative -->
# Screen capture that photographs the window HOSTING the process (not "the current one")

**Status:** delivered · **Task:** `01a0f211` · **Branch:** `task/01a0f211-8ffa-7acb-838c-a06e7bcde381`
**New surface:** `gopi capture --self [--decorations]` · `src: internal/shot/pidwindow.go`

## The defect this closes

`gopi capture --window` answered "which window is ACTIVE?" — the EWMH
`_NET_ACTIVE_WINDOW` property, i.e. whatever the user last clicked. A capture
made from a terminal was therefore a screenshot of *another application*: the
measured 2026-09-30 run returned the operator's hermes window (the screenshot
showed the wrong program, while every field of the result looked healthy).

The question a caller actually means is **"where am I running?"**, and that has
a deterministic, non-heuristic answer:

```
my controlling tty (/proc/self/fd/0 → /dev/pts/N)
        ↓  the process owning that pty's MASTER side (/proc/<pid>/fdinfo inode)
the terminal emulator's pid
        ↓  EWMH _NET_WM_PID on the display's windows
the window that HOSTS this process
```

Leg 1 is kernel ground truth: it needs no display, no window manager and no
cooperation. Leg 2 is an **identity** (a client sets `_NET_WM_PID` to its own
pid), so it does not depend on focus, on the pointer, or on stacking order — the
three things that made `_NET_ACTIVE_WINDOW` wrong.

## What was built

| Piece | Where | What it does |
|---|---|---|
| `ttyChain` | `pidwindow.go` | Walks `/proc/<pid>/stat` parent links (nearest first, bounded at 32) — works with no display |
| `terminalEmulatorPID` | same | Matches the tty's **inode** against every process's `/dev/ptmx` fdinfo — the pty's two faces share one inode, so this is a device-level identity, not a name match |
| `windowRectFor` | same | Resolves the window and returns `(rect, window id, route label)` |
| `matchWindowByPID` | same | The selection rule, pure: emulator pid first, then the ancestry from this process upward (nearest owning a window wins) |
| `clientsWithPID` / `topLevelWindows` | same | `_NET_CLIENT_LIST` when a WM publishes one, otherwise **QueryTree of the root** — a bare Xvfb has no client list, and that case is a test fixture |
| `frameRect` | same | `--decorations`: expands the client rectangle by `_NET_FRAME_EXTENTS` (left, right, top, bottom) |
| CLI `--self` / `--decorations` | `cmd/agent/cli_capture.go` | New flags; `--decorations` is refused on any other mode rather than silently ignored |
| `ScreenCaptureModeSelf` | `internal/tools/screencapture.go` | The mode constant, defined once in `internal/shot` (Rule 14) |

**Nothing new was invented at the wire level**: `QueryTree` (opcode 15),
`GetProperty` and `TranslateCoordinates` were already implemented for the
actuation path; the pid path reuses them, and `queryTreeReply` was factored out
so the parent-oriented and children-oriented readers can never become two
decodings of one reply (the reply's `length` field is a **child count**, not a
byte length — that is where such a pair would drift apart).

## Why not `_NET_ACTIVE_WINDOW` (kept, and why both exist)

`--window` keeps its old meaning — it is an honest question with an honest
answer, and there are callers who mean it (a screenshot of "what the user is
looking at"). `--self` is a *different question*, which is why it is a different
mode and not a change of behaviour under the same flag. The result's
`window_route` names which route answered, so a caller never has to guess which
question was asked.

## Honest limits — stated, not discovered later

1. **A pid-namespace boundary makes the display leg unanswerable.** An
   X-forwarded display (`ssh -X`) reports the pids of the host that RUNS the
   emulator, so no window on it can carry this process's pid or any of its
   ancestors'. The resolution then **refuses** and prints BOTH sides of the
   comparison — the ancestry walked and the pids the display reported — because
   two disjoint lists are what makes the cause readable.
   *This is the path this very development session took*: the agent's `DISPLAY`
   was `localhost:16.0` (a forwarded display whose terminal emulator lives on
   another host), `stdin` was `sshd`, and `gopi capture --self` refused with the
   full chain. Falling back to the active window there would have reinstated
   exactly the mis-capture this work removes, so there is no fallback: the run
   reports what it walked and stops.
2. **Client-side decorations are already inside the client rectangle.** Measured
   on this host's GNOME Terminal: no `_NET_FRAME_EXTENTS` at all,
   `_GTK_FRAME_EXTENTS = 26,26,23,29`, and the title bar's pixels (`(251,251,251)`
   / `(34,34,34)` in the very first rows of the client capture) are inside it.
   So `--decorations` on such a window captures the same rectangle and the route
   says `client-area (no _NET_FRAME_EXTENTS: this window is client-side
   decorated…)`. **A tool cannot add what the display does not have** — see the
   xterm comparison below.
3. **A bare X server with no window manager has no decorations to capture at
   all.** In an Xvfb + xterm environment the title bar is the *emulator's own*
   drawing when a WM is absent — there is nothing above the client area to take.
   This is a property of the environment, not a defect of the capture.

## Verification (Rule 31: the changed surface was executed)

### Ran — the verb, for real

```
$ gopi capture --self --out …/self-window.png
{"path":"…/self-window.png","display":"localhost:16.0","mode":"self",
 "width":604,"height":368,"x":70,"y":77,"depth":24,"bands":3,"sha256_16":"01633560f05a83f7",
 "window":"0x4a00020",
 "window_route":"_NET_WM_PID pid=2113071 (xterm) window=0x4a00020 tty=/dev/pts/8 client-area"}
```

The route names the **whole chain**: the property that matched, the pid, the
emulator's command name, the tty, the window id, and which rectangle was taken.

### The window proof: pixels follow the process's own output

Two captures of the SAME window, produced by one shell inside it, differing
**only** in the glyphs of one marker line (`evidence/markerA.png`,
`evidence/markerB.png`):

| | marker written by the process | vision reads back from the capture |
|---|---|---|
| run A | `GOPSELFPROOF-a4f19c73` | `GOPSELFPROOF-a4f19c73` |
| run B | `ZZZQSWXPLOOF-z4f19c73` | `ZZZQSWXPLOOF-z4f19c73` |

`ImageChops.difference(markerA, markerB)` is **non-empty over 587 of 597 rows**,
and each capture reads back its own run's marker — so the pixels are bound to
what *this* process wrote, not to a coincidence of similar-looking text.

### Colour gamut: the real display vs the synthetic one

`unique_colors` (PIL, exact RGB tuples):

| image | what it is | size | colors |
|---|---|---|---|
| `markerA.png` | xterm on the **real** display (`localhost:16.0`, 24-bit) | 936×597 | **1,966** |
| `gt-self.png` | xfce4-terminal on the real display | 1116×597 | **4,761** |
| `gt-self-deco.png` | same, `--decorations` | 1116×634 | **5,346** |
| `control-realdisplay.png` | xterm, real display, `-u8` (`#0c0c0c`/`#dcdcdc`) | 1850×1016 | **1,651** |
| `xvfb-self.png` | the same xterm/`-u8` spelling on the **Xvfb demo** display `:71` | 2314×1058 | **208** |
| `operator-real-gnome-terminal-16736.png` | the operator's real GNOME Terminal, a dense Markdown/TUI session | 2180×1210 | **24,643** |

The synthetic demo environment is **8× poorer in colour than the same emulator
on the real display** (208 vs 1,651) and **~118× poorer than the operator's real
interface** — the "16,736-class vs 768-class" gap the epic named, measured
again here. It is not a rendering setting: it is the Xvfb demo's depth and its
lack of a compositing/decoration layer.

## The seven demo images vs the real interface

The demo set is `docs/archive/demo/` (and the specimens under
`~/.gopi/sessions/capdemo-5/`). Measured, the same way as above:

| image | colors | what it structurally lacks |
|---|---|---|
| `docs/archive/demo/human/clean/{chat,hero}.png` | 895 | decoration, tabs, any colour depth |
| `docs/archive/demo/human/clean/ui.png` | 535 | as above |
| `docs/archive/demo/human/frames/f0*.png` (41 frames) | 655 each | as above |
| `docs/archive/demo/external/stills/02-live-desktop-window.png` | 20,728 | — (a real window: this is the honest specimen) |
| `docs/archive/demo/external/stills/03-text2img-cjk.png` | 256 | a renderer output, deliberately palette-limited |

**Differences the capture can never fix** (they are properties of the
environment, and making the tool "fix" them would mean inventing pixels):

- **Decorations**: with no window manager there is no frame to capture; with
  client-side decorations the title bar is inside the client rectangle (which
  the capture *does* include — it is not missing, it is not where
  `_NET_FRAME_EXTENTS` would put it).
- **Tabs**: a tab bar is one emulator's UI, not X11's; a capture shows what the
  window paints, and an emulator with no tabs has none to show.
- **Theme/colour depth**: an 8-bit demo server has 208 colours to give. A capture
  reproduces the framebuffer; it cannot raise the display's depth.
- **Markdown/truetype rendering**: this is the *emulator's* font stack and
  escapes, not the capture's. `--self` photographed the same text at 1,966
  colours on the real display and 208 on Xvfb, with identical glyphs — the
  capture was never the limiting factor, the server was.

What the capture *can* fix, and now does: **which window** is in the picture.

## Rule 11 — the model was actually persuaded to use it

A tool is delivered when an LLM CALLS it, not when it compiles. Evidence from a
real run (not a fixture): session `987acc95-cd98-43b6-add3-a9f9f6635c40`,
model `deepseek-flash`, 2026-09-30 12:03Z — the prompt asked where the process was
running, and the model's own tool call was

```json
{"out": "<session>/self-window-proof.png", "self": true}
```

**One argument, no hint in the prompt about a flag name** — it read `self` out of
the tool description. In the same run it then fell back to
`{"area": "1353x740+366+119", ...}` for the *operator's* window, which is exactly
the intended division: `self` for "where am I running", `area`/`window` when the
question is about a specific other window.

The call returned the refusal this run's environment produces (limit 1: the
display is forwarded, so no window carries a local pid) — a refusal is a result,
and it is the *correct* result here; the adoption being measured is the model
reaching for the right argument.

## Files

- `internal/shot/pidwindow.go` (new) — the resolution path and the capture entry
- `internal/shot/pidwindow_test.go` (new) — the /proc leg over a fixture process
  tree, the display leg over the package's fake X server
- `internal/shot/x11rect.go` — `CaptureModeSelf`, `CaptureOptions.Decorations`,
  `queryTreeReply` factored out
- `internal/shot/cov_shot_x11_test.go` — the fake server gained an injectable
  `pidFixture`
- `cmd/agent/cli_capture.go` — `--self`, `--decorations`, mutual exclusion, usage
- `cmd/agent/cli_capture_test.go` — verb-level tests (argv → engine → file)
- `internal/tools/screencapture.go` — `ScreenCaptureModeSelf`, `Decorations`

Evidence images: `~/.gopi/sessions/01a0f211-8ffa-7acb-838c-a06e7bcde381/evidence/`
(markerA/markerB + their `.json` route labels, gt-self*, control-realdisplay,
xvfb-self, operator-real-gnome-terminal-16736).

## Not Done (honest)

- **`gopi capture --self` could not be run inside the window hosting THIS
  session**, because this session's display is forwarded from another host (limit
  1 above): the tool correctly refused and named the chain. The proof above
  therefore uses windows this run **spawned on the same real display**
  (`xfce4-terminal` with a second tab, and xterm) — the window is the *shell's*
  own host, resolved by the same code path, but it is not the operator's
  interactive terminal.
- `--decorations` returns the client area on any client-side-decorated window
  (GNOME Terminal/xfce4-terminal here). Reading `_GTK_FRAME_EXTENTS` as a
  *reported* fact rather than an expansion is deliberate; if a future caller
  needs the GTK shadow rectangle, it is a new flag with its own name.
