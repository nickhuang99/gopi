<!-- doc-class: normative -->
# Screen Capture: Window and Area Modes (`gopi capture`)

**Status**: delivered (branch `task/01a0c5110f937222a8f944b8d60a398d`)
**Touches**: `internal/shot/`, `internal/tools/screencapture.go`,
`internal/agent/loop.go`, `cmd/agent/cli_capture.go`,
`cmd/agent/cli_verbs.go`, `cmd/agent/cli_completion.go`,
`internal/config/help.go`

## 1. What changed, and why the verb changed with it

`screen_capture` grabbed the whole X11 root window. That is the wrong shape for
the question the tool actually gets asked — *"what is this window doing right
now?"* — because a full-desktop PNG spends most of its pixels on windows the
caller did not ask about, and every downstream reader (`vision_analyze`, a
human) pays for them. A 1920x1080 desktop with one 800x600 window in it is
~78% waste.

So the capability grew the common OS screenshot triple, and the CLI verb grew
with it:

```
gopi capture --screen                   # whole display (the default)
gopi capture --window                   # the ACTIVE window
gopi capture --area 800x600+100+50       # a rectangle
```

**`gopi screen_capture` was NOT removed.** It is the CLI mirror of the
`screen_capture` LLM tool and its word IS `toolnames.ToolScreenCapture`, so a
session log's `tool_use` stays greppable as a command. It keeps its own
whole-screen behaviour, and a mode flag given to it (`gopi screen_capture
--window`) is forwarded to `gopi capture` rather than refused — refusing would
be pedantry about a spelling, while silently ignoring the flag would be worse
than either.

The LLM tool is still `screen_capture` and gained two optional arguments rather
than a second tool (Rule 3): `window: true` and `area: "WxH+X+Y"`. Zero new
tools, zero new dependencies.

## 2. The two bugs that made every rectangle wrong (read this before touching `x11rect.go`)

Both were **wire-level, both silent, and both invisible to the whole-screen
path** — which is exactly why the whole-screen suite stayed green while every
window capture returned nonsense. They are recorded here because neither is a
compile error and neither shows up as a failing pre-existing test.

### 2.1 The opcode: `GetGeometry` is 14, not 3

`X_GetWindowAttributes` is opcode **3**; `X_GetGeometry` is **14**
(`/usr/include/X11/Xproto.h`). A first revision declared it as 3, and the
server answered — with a *well-formed 44-byte attributes reply*, because a
wrong opcode is not malformed, it is a different request. Decoding that reply
with the `xGetGeometryReply` layout produced type=1 (plausible), length=3 (a
nonstandard reply size), and window dimensions read out of bytes that are not
width/height: every rectangle came back `65535x65535`.

### 2.2 The coordinates: `GetGeometry`'s x/y are PARENT-relative

`xGetGeometryReply` x@12 and y@14 are relative to the window's **parent**, not
the screen. Passing them into `TranslateCoordinates(window → root, x, y)`
computes `parent_origin + parent_relative_offset` — the parent's offset added
**twice**. Measured on xfwm4: a window whose true rectangle is
`184x134+45+59` was reported at `184x134+50+88`.

The correct call is the point `(0,0)` **in the window's own coordinate space**:

```go
ax, ay, err := c.translateToRoot(window, 0, 0)   // NOT (x, y) from GetGeometry
```

`windowSize` therefore returns **only** width and height, deliberately: a
parent-relative coordinate that looks usable is the trap, so it is not handed
out.

## 3. The oracles, and why each one exists

A rectangle claim ("these pixels are that window") cannot be settled by the
code that made the claim. Three independent oracles, in increasing strictness:

| oracle | what it is | which bug it catches |
|---|---|---|
| `TestX11OpcodeConstantsMatchTheProtocolHeader` | greps `#define X_<Name> <n>` out of `Xproto.h` and compares against the Go constants | a wrong/mistyped opcode (**needs no X server at all**) |
| `TestWindowSizeOfTheRootMatchesTheSetupReply` | the root's own `GetGeometry` must equal the width/height the connection setup independently parsed | wrong opcode **and** wrong field offset, in one assertion |
| `xwininfo` measurement | an independent X11 client measures the same window id | the whole rect (position *and* size) |
| **pixel identity** | `--window` pixels vs the crop of `--screen` at the same rect, absolute error **0** | everything: a rect that "looks right" but is off by a pixel fails |

The pixel-identity oracle is the one that matters most (Rule 10.2): two
different code paths over one server state, compared at the pixel level. A
plausible-looking geometry passes the first three and fails this one.

**The border subtlety, which cost a debugging round.** `xwininfo`'s "Absolute
upper-left X/Y" is the window's **outer** corner; `GetGeometry`'s width/height
— and therefore the rectangle this engine captures — describe the area
**inside** the border. On a WM-less server xterm's border is 1px (outer 40,30
vs content 41,31); under a reparenting WM the client's border is 0 and the two
coincide. The oracle adds `Border width` before comparing.

## 4. Active-window resolution: three routes, each LABELLED

`CaptureX11Window(display, 0)` resolves "the current window" in a fixed order,
and **reports which route answered** in the result's `window_route` field:

1. **`_NET_ACTIVE_WINDOW`** on the root window — the EWMH property a window
   manager maintains. The authoritative answer when a WM is running.
2. **`GetInputFocus`** + a `QueryTree` walk to the top-level ancestor — for a
   server where something took the focus but no EWMH property exists.
3. **`QueryPointer`**'s child of root — for a bare Xvfb / kiosk client, where
   there is neither a WM nor an explicit focus (measured: `GetInputFocus`
   answers the `PointerRoot` sentinel, `0x1`, which names no window).

A **fallback that is not announced cannot be told from the authoritative
answer** (Rule 15), so the route travels in the payload and the tests assert
it.

Note the asymmetry between routes 2 and 3 for a **reparented** window: the
focus route walks up to the top-level ancestor (the WM frame, because that is
what the user sees and what carries the titlebar), while the pointer route
returns the child of root the pointer is actually in (the client). Both are
correct answers to slightly different questions, and the label is what makes
the difference legible instead of confusing.

## 5. Fixture: a WM changes the answer, so the fixture must run one

The live tests bring up a private `Xvfb` **plus xfwm4** plus one xterm. The WM
is the interesting case on purpose: it **reparents** the client into a frame,
so the window's own `GetGeometry` coordinates are frame-relative and only
`TranslateCoordinates` yields the screen position — the exact bug in §2.2 that
a WM-less fixture would hide. The WM-less route (§4.3) is covered by a
separate test.

**A client mapped before the WM has initialised is never focused.** Measured:
the EWMH active window stayed an xfwm4-internal 1-pixel-wide window
(`1x257`, at `619,64`) for the whole 20s poll budget, because the client's map
request arrived before the WM was ready to manage it. The fixture therefore
waits for the WM's own announcement (interning `_NET_ACTIVE_WINDOW` succeeds ⇒
it has taken over) **before** starting the client.

The settle predicate is **three consecutive identical measurements**, not a
fixed sleep: a fixed sleep is a guess on a loaded host, and comparing pixels of
a rectangle that is still moving fails for a reason that has nothing to do with
the code under test.

Every fixture test **skips visibly** when Xvfb / xfwm4 / xterm / xwininfo /
xdotool is unavailable, so the suite still passes on a machine with no X at all.

## 6. Red-proof

The engine guards were proven red **before** being accepted green (Rule 10.4),
by restoring the buggy opcode:

```
$ sed -i 's/x11OpGetGeometry     = 14/x11OpGetGeometry     = 3/' internal/shot/x11rect.go
$ go test -run 'TestX11OpcodeConstants|TestWindowSizeOfTheRoot|TestCaptureWindowRectMatches' ./internal/shot/
--- FAIL: TestCaptureWindowRectMatchesIndependentGeometryAndScreenCrop
    x11rect_test.go:747: x11 opcode GetGeometry = 3, but /usr/include/X11/Xproto.h says 14 — ...
--- FAIL: TestWindowSizeOfTheRootMatchesTheSetupReply
    x11rect_test.go:788: GetGeometry(root) reports 65535x65535, the connection setup reports 640x480
```

Both oracles fail on the one-line regression; restoring `14` turns them green.
The header oracle needs no X server, so it also guards CI.

## 7. Refusal contract (no silent substitutions)

| input | behaviour |
|---|---|
| `--area 0x0`, `abc`, `800x`, negative offset | refused at parse, naming the offending text, non-zero exit, **no file written** |
| area that runs off the display | refused **quoting the rectangle** — never clipped, never padded |
| a window id that does not exist | refused, **naming the id** |
| an unknown capture mode | refused, listing the legal modes |
| `--screen --window` together | refused as mutually exclusive |

## 8. Not done / honest limits

- **Wayland**: this is X11-only and fails loudly there by design (no portal
  implementation). Unchanged by this work.
- **Multi-monitor / multi-screen**: a window on a screen other than the root's
  default is refused by `TranslateCoordinates` (`sameScreen = false`) rather
  than silently captured from the wrong screen.
- **`--window` with an explicit window id** is implemented in the engine
  (`CaptureX11Window(display, id)`) and exercised by the engine tests, but the
  CLI does not yet expose a `--window-id` flag; the tool has no argument for it
  either. The default (id 0 = "the active window") is what the surface offers.
- **Obscured windows**: the capture reads the window's own pixels as the server
  composited them into the root, which is what a screenshot means; an
  *occluded* window is not captured as if it were raised.
