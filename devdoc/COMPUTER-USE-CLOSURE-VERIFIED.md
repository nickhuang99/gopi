<!-- doc-class: historical -->
# Computer-Use closure VERIFIED — gopi has an eye and a hand (2026-09-11 04:10)

## What was verified

The `screen_capture` tool and the `gopi act` actuation CLI were merged
(`902bab4`) and rebuilt, then **end-to-end verified on a live X11 display
(:99)** by the PA — not by the agent that wrote them.

### The eye: `gopi screen_capture`

```
$ DISPLAY=:99 ./bin/gopi screen_capture --out /tmp/night-test.png
{"path":"/tmp/night-test.png","display":":99","width":1280,"height":800,
 "depth":24,"bands":7,"bytes":25697,"sha256_16":"9adfd46934aa465c",
 "hint":"Pass this path to vision_analyze to read the screen; compare
         sha256_16 across captures to prove the screen changed."}
```

**Independently cross-checked by three sources** (not trusting the tool's
self-report):

| Source | Size | Bytes | sha256_16 |
|---|---|---|---|
| tool self-report | 1280x800 | 25697 | 9adfd46934aa465c |
| `file` | 1280 x 800, 8-bit/color RGB | — | — |
| ImageMagick `identify` | 1280x800 8-bit sRGB | 25697B | — |
| **`sha256sum`** | — | — | **9adfd46934aa465c** |

All four agree. Implementation is a **hand-rolled X11 protocol client**
(`internal/shot/x11.go`) — zero CGO, zero new Go dependencies — grabbing the
root window in 128px bands and PNG-encoding it.

### The hand: `gopi act`

```
$ DISPLAY=:99 ./bin/gopi act info
{"action":"info","display":":99","dry_run":false,"pointer":"640,400",
 "focus":"0x800085","root":"1280x800","note":"read-only probe: nothing was injected"}

$ DISPLAY=:99 ./bin/gopi act move 400 300
{"action":"move","display":":99","dry_run":false,"pointer":"400,300", ...}
```

Input is injected through **XTEST** (the X11 standard for synthetic input) with
the same hand-rolled client — again no CGO, no `xdotool` binary, nothing linked.

### The closure: capture → act → capture → compare

```
1) act info        → pointer 640,400 ; root 1280x800 ; nothing injected
2) screen_capture  → sha256_16 = 9adfd46934aa465c   (/tmp/before.png)
3) act move 400 300→ pointer moved
4) screen_capture  → sha256_16 = 446fd15bb4aeebcc   (/tmp/after.png)
```

**The hash changed**, proving the whole loop is real: the capture reads true
screen pixels, the actuation reaches the X server, and a re-capture detects the
change. This is the Verify primitive working with no LLM in the loop.

## Why this matters (nick's framing)

> "if LLM can view screen, then it is another level of Turing-Complete for
> computer-use. Let's make it a closure for LLM to operate-control computer."

The pieces now exist as **toolcalls** (not just CLI): `screen_capture` is a
registered tool (`toolnames.ToolScreenCapture`), and the model was already
observed *choosing* it over `bash`+`import` in a live session (see the branch's
Rule-11 evidence: three `screen_capture` calls with correct arguments, no shell
fallback). The failure mode that motivated the tool — an LLM reaching for a
shell screenshot — does not occur.

**Deliberately NOT done yet** (per the implementing task's own report):
- window-targeted capture (only the root window today)
- Wayland (`org.freedesktop.portal.Screenshot`) — X11 only
- full keymap coverage for `act type` (printable ASCII today)

## Reproduce

```bash
cd ~/work/GoPi
DISPLAY=:99 ./bin/gopi screen_capture --out /tmp/b.png
DISPLAY=:99 ./bin/gopi act move 400 300
DISPLAY=:99 ./bin/gopi screen_capture --out /tmp/a.png
# the two sha256_16 values differ when the pointer moved
```

(Xvfb `:99` is already running on peerbox; several other displays exist too.)
