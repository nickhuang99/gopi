<!-- doc-class: historical -->
# Task Report: Tab-order as an Actuate primitive — POC + measurement

Host: `peerbox` (Linux 5.15.0-163, Xvfb `:99` 1280x800x24 + xfwm4, no sudo).
All raw output below was produced **on this host**; every number is measured, not recalled.
POC code: `devdoc/tab-order-poc/` — screenshots: `devdoc/tab-order-poc/shots/` (66 PNGs:
57 from the original runA/runB/scroll/trap set + 9 from the re-verification runC).

**Headline:** Tab-order is deterministic and cheap *as a signed delta*, but it is a
weak *actuation* primitive on its own (real apps swallow Tab; AT-SPI's `focused`
flag lies across applications). The decisive finding is that **the a11y tree is a
strictly better Actuate primitive than Tab**: `grab_focus` moves focus in 0.15 ms
and `do_action(0)` clicks a widget in 0.08 ms, both **by name, with no
coordinates, no Tab counting, and no keyboard injection at all**. Tab-order's
real value is as the *fallback* on paths where the a11y tree is unavailable — and
its value is exactly the width of that gap, so the gap is measured in §B.

---

## Environment / method (so the evidence is reproducible)

```bash
# session bus (needed for AT-SPI) + :99 already had Xvfb + xfwm4
$ XDG_RUNTIME_DIR=/tmp/tabpoc/rt dbus-daemon --session --fork --print-address
unix:abstract=/tmp/dbus-jGPGNlaYB6,guid=01ae1a2a45da7c87115f39926aa2c6a8

$ /usr/bin/python3 -c "import gi; gi.require_version('Atspi','2.0'); from gi.repository import Atspi; print('Atspi import OK', hasattr(Atspi,'get_desktop'))"
Atspi import OK True
$ /usr/bin/python3 -c "import pyatspi"
ModuleNotFoundError: No module named 'pyatspi'      # expected: reachable only via GObject introspection
```

Interpreter split confirmed: `/usr/bin/python3` = gi + Xlib + PIL + numpy (no cv2);
hermes venv `python3` = cv2 + PIL + numpy (no gi). The driver script shells out so
each experiment runs on the right interpreter.

Reference GUI: `gtk_probe_app.py` — a purpose-built GTK3 window with a **known**
focus chain, so the a11y reading can be checked against ground truth:

```
entry-1, check-1, button-1, entry-2, radio-1, button-2        (plain)
scroll-entry-00 … scroll-entry-29 inside a 150px viewport      (scroll)
+ variants: --trap (widget swallows Tab), --layout invisible   (focusable-but-unmapped)
```

---

## A. Determinism: Tab produces a byte-identical focus chain across runs

`tab_order_poc.py` presses Tab N times, and after **every** step reads three
channels at once — AT-SPI, X11 (`xdotool`), pixels (`import -window root`).
Run A and run B were **separate app processes** (fresh PID, fresh window id):

```
$ /usr/bin/python3 tab_order_poc.py --run-id runA --tabs 8
[runA] step 1 -> atspi=check box/check-1 extents=[55, 119, 500, 22]
[runA] step 2 -> atspi=push button/button-1 extents=[55, 147, 500, 34]
[runA] step 3 -> atspi=text/entry-2 extents=[55, 187, 500, 34]
[runA] step 4 -> atspi=radio button/radio-1 extents=[55, 227, 500, 22]
[runA] step 5 -> atspi=push button/button-2 extents=[55, 255, 500, 34]
[runA] step 6 -> atspi=text/entry-1 extents=[55, 79, 500, 34]
[runA] step 7 -> atspi=check box/check-1 extents=[55, 119, 500, 22]
[runA] step 8 -> atspi=push button/button-1 extents=[55, 147, 500, 34]
LOG=/home/user/work/GoPi/devdoc/tab-order-poc/shots/runA_log.json
FOCUS-CHAIN=["entry-1","check-1","button-1","entry-2","radio-1","button-2","entry-1","check-1","button-1"]

$ /usr/bin/python3 tab_order_poc.py --run-id runB --tabs 8      # fresh process
FOCUS-CHAIN=["entry-1","check-1","button-1","entry-2","radio-1","button-2","entry-1","check-1","button-1"]
```

The chain is exactly the declared widget order, then wraps (6-widget cycle).

**Pixel-level determinism** — md5 of the full-screen PNG after each step:

```
step00  runA=994b1cc52992 runB=994b1cc52992  IDENTICAL  changed_px=0 (0.0000%)
step01  runA=103a43306464 runB=103a43306464  IDENTICAL  changed_px=0 (0.0000%)
step02  runA=f75d0e5b386d runB=f75d0e5b386d  IDENTICAL  changed_px=0 (0.0000%)
step03  runA=fd0a7ccc9e54 runB=fd0a7ccc9e54  IDENTICAL  changed_px=0 (0.0000%)
step04  runA=04a45f21317a runB=04a45f21317a  IDENTICAL  changed_px=0 (0.0000%)
step05  runA=bdf4817d7c3f runB=bdf4817d7c3f  IDENTICAL  changed_px=0 (0.0000%)
step06  runA=994b1cc52992 runB=994b1cc52992  IDENTICAL  changed_px=0 (0.0000%)
step07  runA=103a43306464 runB=103a43306464  IDENTICAL  changed_px=0 (0.0000%)
step08  runA=f75d0e5b386d runB=f75d0e5b386d  IDENTICAL  changed_px=0 (0.0000%)
```

Every step of two independent runs is **byte-identical**, and the cycle closes
pixel-exactly (step06 == step00). Tab-order is fully deterministic *inside a
known focus chain*.

**The focus ring is visible and machine-detectable.** Consecutive-step pixel diff
(threshold 24/255), from `visual_diff_probe.py`:

```
step00->01 changed_px=  3585 (0.3501%) bbox=[54, 79, 555, 377]
step01->02 changed_px=  2426 (0.2369%) bbox=[54, 118, 555, 374]
step02->03 changed_px=  5107 (0.4987%) bbox=[55, 87, 554, 377]
step03->04 changed_px=  3299 (0.3222%) bbox=[54, 187, 555, 377]
step04->05 changed_px=  2380 (0.2324%) bbox=[54, 226, 555, 374]
step05->06 changed_px=  5108 (0.4988%) bbox=[55, 79, 554, 377]
```

The `bbox` tracks the widget rect ±1px — the change is the focus ring moving.

**vision_analyze on the cropped screenshots (verbatim model output):**

- `shots/focus_cmp_step00.png` → *"The widget with the keyboard focus is the first
  text entry field, labeled **entry-1** … a blue highlighted border around the entry.
  (2) The status label at the bottom reads verbatim: **focus: entry-1**"*
- `shots/focus_cmp_step01.png` → *"**(1) check-1** (It has a dashed focus ring
  around it, and the status label confirms it). (2) focus: check-1"*
- `shots/focus_diff_overlay.png` (changed pixels painted red) → *"A solid red border
  surrounding the text entry field … reads entry-1. A red dotted/dashed border
  outlining the checkbox … reads check-1. Red text at the bottom … reads focus
  check-1"* — i.e. the model localizes **both** the vacated and the acquired ring.

vision independently agrees with AT-SPI on every sampled frame.

---

## B. The feedback problem: three detection channels, measured

### B1. Latency (10 samples each, `channel_bench.py`)

```
it_atspi  : median  3.23 ms   min 2.64   max 13.44   result: "push button/button-1"
it_x11    : median 10.63 ms   min 7.16   max 12.74   result: {window_id: 6291463, name: "gopi-tab-probe"}
it_pixels : median 195.99 ms  min 180.79 max 353.66  result: {changed_px: 0, bbox: null}
```

Per-hop costs for the combined loop (`tab_rate_bench.py`, 12 hops):

```
inject_only_ms                     : median  15.59   (xdotool key Tab = XTEST)
inject_plus_verify_ms              : median 103.91   (AT-SPI read via SUBPROCESS)
inject_plus_verify_plus_shot_ms    : median 253.32   (AT-SPI + import -window root)
```

In-process AT-SPI is 3 ms; the 104 ms figure is dominated by process spawn — a Go
implementation reads AT-SPI over D-Bus in-process, so the realistic per-hop cost
is **~3 ms verify + key injection**, not 104 ms.

### B2. What each channel actually *knows*

```
channel   | sees the KEYBOARD-owning widget? | per-app view              | portable?
----------|----------------------------------|---------------------------|----------------------------
AT-SPI    | yes, but ONLY via the oracle      | per-application           | needs an a11y toolkit + bus
          | (frame ACTIVE ∧ descendant FOCUSED)| (focused flag is local)  | (GTK/Qt/Gecko yes, Xaw no)
xdotool   | NO — toplevel only                | global but coarse         | X11 only
xprop     | NO                                | global but coarse         | X11 only
pixels    | yes (visually) but inferential    | blind to identity         | any display, any OS
```

X11 proof — three Tabs in a row, `_NET_ACTIVE_WINDOW` never changes:

```
  tab1: _NET_ACTIVE_WINDOW=window id # 0x600003 | toplevel=6291459 'gopi-tab-probe'
  tab2: _NET_ACTIVE_WINDOW=window id # 0x600003 | toplevel=6291459 'gopi-tab-probe'
  tab3: _NET_ACTIVE_WINDOW=window id # 0x600003 | toplevel=6291459 'gopi-tab-probe'
distinct x11 window ids across 9 steps: 1
distinct atspi widget names across 9 steps: 6
```

`xdotool`/`xprop` answer "which **window** is active" — never "which **widget** has
the keyboard". The task wants the latter, so on X11 this channel is **only** usable
as a toplevel-level guard, not as the focus oracle.

### B3. The AT-SPI trap (the most important finding in §B)

AT-SPI's `focused` state is **per-application** ("this widget holds the toolkit
focus inside its own toplevel"), not global. Measured with gedit owning the
keyboard, two apps alive:

```
=== ALL nodes carrying the AT-SPI FOCUSED state ===
app=gtk_probe_app.py       focused_nodes=1
    0.0.0.0.0.0.23 text           scroll-entry-23
which frame is ACTIVE per app:
  xfwm4                  active=False
  gtk_probe_app.py       frame=gopi-tab-probe   active=False
  gedit                  frame=*test1.txt (/tmp/tabpoc) - gedit   active=True
=== X11 ground truth === *test1.txt (/tmp/tabpoc) - gedit
```

The only `FOCUSED` node on the whole desktop belonged to an **inactive**
application. A naive "find the focused node" implementation **silently returns
another app's widget** — and my first version of the probe did exactly that
(a `--app gedit` query returned `scroll-entry-23` from the probe app). The
correct oracle is:

```
focus := the FOCUSED descendant of the application whose FRAME carries ACTIVE
```

implemented as `focus_probe.py --oracle`, verified on both apps:

```
$ /usr/bin/python3 focus_probe.py --oracle --json       # gedit active
{"app":"gedit","frame":"*test1.txt (/tmp/tabpoc) - gedit","frame_active":true,
 "focused_count":1,"focused":{"role":"text","name":"", ... extents:[0,47,1280,726]}}

$ /usr/bin/python3 focus_probe.py --oracle --json       # probe app activated
{"app":"gtk_probe_app.py","frame":"gopi-tab-probe","frame_active":true,
 "focused_count":1,"focused":{"role":"text","name":"scroll-entry-23", ...
 extents:[55,322,500,34]}}
```

A second AT-SPI trap, found by the same experiment: **depth**. gedit's focus sits
at path depth **16**; my initial walk bound of 12 produced a *false* "no focused
descendant". Bound raised to 40 (`MAX_TREE_DEPTH`) — a walk that stops early
manufactures a wrong answer, it does not degrade gracefully.

### B4. Portability of the two a11y-adjacent channels

| Toolkit | on the a11y bus? | evidence |
|---|---|---|
| GTK3 (`gtk_probe_app.py`, `gedit`, `zenity`) | **yes** | 3+ apps listed; zenity appears as `{"role":"application","name":"zenity"}` |
| Xaw (`xmessage`) | **NO** | window exists (`xdotool` search → 10485811) but it never appears in `focus_probe.py --apps` |
| Gecko (`firefox`, snap) | **not runnable here** | `firefox` exited: `...session-18661.scope is not a snap cgroup for tag snap.firefox.firefox` |

```
$ /usr/bin/python3 focus_probe.py --apps --json
{"applications":[{"role":"application","name":"xfwm4"},{"role":"application","name":"gtk_probe_app.py"},
 {"role":"application","name":"gedit"},{"role":"application","name":"zenity"}]}
# xmessage (Xaw) present as an X window but ABSENT here
```

**Portability verdict:** AT-SPI covers GTK/Qt/Gecko-class apps but is blind to
Xaw/Tk/raw-X/openGL/canvas UIs — precisely the "apps outside the focus chain"
class. The pixel channel is the only universally portable one, and it is 60×
slower and identity-blind (it can see *that* something moved, not *what* is
focused). This is the gap Tab-order is competing to fill.

### B5. Does the a11y tree also remove the Capture problem? — **Yes, decisively**

`a11y_actuate_probe.py` on the probe app, verified by the oracle afterwards:

```
$ /usr/bin/python3 a11y_actuate_probe.py --app gtk_probe_app --name scroll-entry-05
{"app":"gtk_probe_app.py","target":{"role":"text","name":"scroll-entry-05"},
 "grab_focus_returned":true,
 "focused_after_grab":{"role":"text","name":"scroll-entry-05"},
 "extents":[55,79,500,34],
 "action_iface":{"n_actions":1,"names":["activate"]},"do_action_0_returned":true}

$ /usr/bin/python3 focus_probe.py --oracle --json     # AFTER
{"app":"gtk_probe_app.py","frame_active":true,"focused_count":1,
 "focused":{"role":"text","name":"scroll-entry-05", ... extents:[55,79,500,34]}}
```

`grab_focus` on `scroll-entry-05` jumped **18 widgets in one call** — no Tab
counting, no coordinates, no keyboard. Extents returned **the very next line**
(`[55,79,500,34]`), i.e. the a11y tree answers the Capture question in the *same
round trip*: identity, role, name, screen rect and state set. Measured latency:

```
find target by name (walk)   median=3.75ms min=2.77 max=11.11
grab_focus                   median=0.15ms min=0.14 max=2.00
do_action(0) click           median=0.08ms min=0.07 max=0.43
```

**The click proof** (`do_action`, no keyboard, no mouse, no coordinates):

```
$ /usr/bin/python3 a11y_actuate_probe.py --app gtk_probe_app --name button-2
   status-label = 'clicked: button-2'      # read back via AT-SPI Text interface
$ vision_analyze(shots/a11y_after_button2_crop.png) ->
   "entry-1 / check-1 / button-1 / entry-2 / radio-1 / button-2 / clicked: button-2"
```

The model reads the confirmation from pixels; AT-SPI reads it structurally. Both
agree. Note this is a *real* GUI event (it fires the button's `clicked` signal),
not a synthetic coordinate click.

---

## C. Adversarial catalogue — where Tab-order breaks

Seven modes, all **reproduced on this host**, each with a concrete mitigation.

### C1. Focus traps — Tabs are silently eaten (REPRODUCED)

`gtk_probe_app.py --trap`: the trap widget returns `True` from `key-press-event`,
consuming Tab. Trap stdout (`grep -c` = 8) and the oracle chain:

```
TRAP swallowed Tab     (×8)
step00  text         entry-1     extents=[55, 79, 500, 34]
step01  check box    check-1     extents=[55, 119, 500, 22]
step02  push button  button-1    extents=[55, 147, 500, 34]
step03  text         entry-2     extents=[55, 187, 500, 34]
step04  radio button radio-1     extents=[55, 227, 500, 22]
step05  push button  button-2    extents=[55, 255, 500, 34]
step06  text                     extents=[55, 295, 168, 34]   <-- trapped
step07…step14  identical to step06  (8 Tabs consumed, zero movement)
```

**Mitigation:** never blind-fire. The oracle read after every hop makes an eaten
Tab *observable* — `step_{n+1} == step_n` for k consecutive steps ⇒ trapped.
Then: (a) press Escape (unwinds GTK grab/modal), (b) fall back to a11y
`grab_focus`/`do_action` for that window, (c) if neither works, escalate to a
coordinate click solely to *break* the trap. Budget: 2 no-move hops ⇒ abort the
Tab strategy for this window.

### C2. Tab does nothing at all — the app consumes it (REPRODUCED, real app)

The strongest real-app finding. gedit, activated, 7 Tabs, two full passes:

```
=== PASS 1 ===                                        === PASS 2 ===
  tab00  gedit  text  ''  extents=[0,47,1280,726]        tab00  gedit  text  ''  [0,47,1280,726]
  tab01  gedit  text  ''  extents=[0,47,1280,726]        tab01  gedit  text  ''  [0,47,1280,726]
  ...                                                    ...
  tab07  gedit  text  ''  extents=[0,47,1280,726]        tab07  gedit  text  ''  [0,47,1280,726]
=== diff === IDENTICAL ACROSS PASSES
```

Focus **never leaves the document widget** — GTK text views bind Tab as
insert-literal-tab. Deterministic, repeatable, and **useless for navigation**.
The same app is fully drivable via the a11y tree.

**Mitigation:** the focus-chain must be *learned/validated*, not assumed. Probe
first: 3 Tabs; if the oracle shows no movement, do not spend the remaining N tabs
— switch to the a11y channel (`grab_focus`/`do_action`), and only if that is
unavailable use a coordinate click on the goal (which still needs one capture).

### C3. Canvas / no-a11y UI, and apps with NO focus chain (REPRODUCED)

`gtk_probe_app.py --layout scroll` — 30 entries, viewport 150px tall:

```
step | focused widget    | a11y extents.x    | visible?  | pixels changed vs prev
   9 | scroll-entry-23    | 55                | YES       |   390 px (0.0381%)
  10 | scroll-entry-24    | 55                | YES       |   409 px (0.0399%)
  11 | scroll-entry-25    | 55                | YES       |   409 px (0.0399%)
  12 | scroll-entry-26    | 55                | YES       |   417 px (0.0407%)
  13 | scroll-entry-27    | 55                | YES       |   411 px (0.0401%)
  14 | scroll-entry-28    | 55                | YES       |   403 px (0.0394%)
```

The **7** entries inside the viewport report a real rect (`extents.x = 55`, and
note `y` stays pinned at 322 in the per-step log because GTK **auto-scrolls the
viewport to follow the focus** — the focused entry is always the scrolled-to one);
the remaining **23** report the **INT32_MIN sentinel**:

```
  scroll-entry-16  extents=(-2147483648,-2147483648,500, 34)
  scroll-entry-17  extents=(  55,  82,500, 34)
  ...
  scroll-entry-23  extents=(  55, 322,500, 34)
  scroll-entry-24  extents=(-2147483648,-2147483648,500, 34)
```

So `extents.x == INT32_MIN` is a **machine-readable "off-screen" flag** — and it,
not the pixels, is what tells you Tab has walked out of the visible viewport. A
pixel-only detector sees a 0.04% scroll shimmer and cannot name the target.

`xmessage` (Xaw) is *entirely* absent from the a11y bus, so on such a UI the
oracle has nothing to say and only the pixel channel remains.

**Mitigation:** treat `INT32_MIN` extents as `OFFSCREEN` and treat "app missing
from the a11y bus" as `NO_A11Y`; in both cases either scroll it into view (proven
by a re-read) or abandon Tab-order for that surface and use capture-based
coordinates. Never infer "arrived" from "something changed".

### C4. Focus-stealing popups (REPRODUCED)

`popup_probe.sh` — a zenity popup appears while the Tab chain is in flight:

```
### launch focus-stealing popup (zenity) as a separate process
  popup window=8388616  x11-focus=FOCUS THIEF
  focus after popup appears : {"name":"scroll-entry-07","app":"gtk_probe_app.py",...}
  target app focus          : {"name":"scroll-entry-07", ... "matched_filter": false}
### FORCE the steal (xfwm4 has focus-stealing prevention, so ask explicitly)
  x11 channel after steal   : win=8388616 name=FOCUS THIEF
  atspi channel after steal : {"name":"scroll-entry-07","app":"gtk_probe_app.py",...}
### press Tab while the popup owns the focus (blind driver behaviour)
  blind-tab1 -> {"name":"scroll-entry-07"}      # unchanged
  blind-tab2 -> {"name":"scroll-entry-07"}      # unchanged
### recover: re-activate the target, then Tab
  focus after re-activate   : {"name":"scroll-entry-07"}
  tab after recover         : {"name":"scroll-entry-08"}     # chain resumes correctly
```

Two lessons: (1) xfwm4's focus-stealing prevention meant a newly-mapped popup did
**not** take the focus by itself — the popup only owned the keyboard after an
explicit `windowactivate`; (2) during the steal the **atspi channel still reported
the app's widget as focused** while X11 correctly said `FOCUS THIEF`. The oracle
alone is therefore not sufficient at toplevel granularity — pair it with the
window-level channel.

**Mitigation (the "double-read" guard):** before each Tab, read both
(a) the X11 active window and (b) the oracle; if (a) is not the target toplevel,
re-activate it (proven to work above) or fail loudly. Tab is only safe to fire
when both channels agree on the target window.

### C5. No WM ⇒ Actuate and Capture both break (REPRODUCED)

On a bare Xvfb `:94` (no window manager), with the app mapped:

```
$ DISPLAY=:94 xdotool windowactivate --sync 2097159
Your windowmanager claims not to support _NET_ACTIVE_WINDOW, so the attempt to activate the window was aborted.
xdo_activate_window on window:2097159 reported an error
activate_rc=1
XGetInputFocus returned the focused window of 1. This is likely a bug in the X server.
X Error of failed request:  BadWindow (invalid Window parameter)
```

**Mitigation:** a WM (xfwm4 here) is a hard dependency, not a convenience — assert
`_NET_SUPPORTING_WM_CHECK` / a successful `windowactivate` at startup and abort
with a clear error rather than pressing Tabs into the void.

### C6. N-tabs cost at CH9329 key rates

CH9329 is a UART→USB-HID bridge: each keystroke is an **8-byte report** on the
serial link at a configurable baud (commonly 9600 8N1; the datasheet text was not
fetchable from this host — CSDN HTTP 521, Zhihu HTTP 403 — so this is computed
from UART framing as an explicit **assumption**, marked as such). At 9600 8N1 each
byte is 10 bits ⇒ 960 B/s ⇒ 8 bytes ≈ **8.3 ms serial time per keystroke**, plus
the HID host poll (1–10 ms at 1000/100 Hz), plus a key-down/key-up pair for a
reliable Tab ⇒ roughly **17–40 ms per Tab** end-to-end on the HID path.

Cost of N Tabs vs one direct actuation, at N=20:

| path | per-step | 20 steps | note |
|---|---|---|---|
| Tab × N, software (XTEST) | ~16 ms measured | ~0.3 s | + N verify reads |
| Tab × N, CH9329 HID | ~17–40 ms (assumed) | **0.3–0.8 s** | serial-bound, degrades with N |
| `grab_focus`/`do_action` | **0.08–0.15 ms** | **~0.003 s** | independent of N |

N Tabs is O(N) and serial-bound; a direct a11y actuation is O(1). At CH9329 rates
a 20-Tab walk costs 100–270× a `do_action` — and that is before counting the N
verification reads the Tab path additionally needs.

### C7. Duplicate instances — the oracle and the keys address DIFFERENT windows (REPRODUCED)

Found while re-running §A: a leftover `gtk_probe_app.py` plus a fresh one meant
**two** windows with WM_CLASS `gtk_probe_app.py` and **two** AT-SPI applications
named `gtk_probe_app.py`. `xdotool search --class gtk_probe` matched both; the
oracle resolved focused state to one instance while the Tab keys were delivered to
the other:

```
$ tab_order_poc.py --run-id rerunC --match "--class gtk_probe" --app-filter gtk_probe_app ...
[rerunC] step 1 -> atspi=push button/button-1 extents=[55, 147, 500, 34]
[rerunC] step 2 -> atspi=push button/button-1 extents=[55, 147, 500, 34]
...  step 3..8 identical ...
FOCUS-CHAIN=["button-1","button-1","button-1","button-1","button-1","button-1","button-1","button-1","button-1"]
$ pgrep -af gtk_probe_app
3441840 /usr/bin/python3 gtk_probe_app.py
3441940 /usr/bin/python3 gtk_probe_app.py       <-- two instances
```

This is the worst kind of failure: it **mimics C1 (a focus trap)** — the chain is
frozen and perfectly repeatable — so a naive "no movement ⇒ trapped" reading
misdiagnoses it, and the retry ladder would press Escape and give up when the real
problem is addressing. It also silently voids any determinism measurement made in
its presence.

**Mitigation:** (a) the oracle must resolve to a **window id**, not just an app
**name** — carry the X window id into the a11y query and require exactly one
match, failing loudly on 2+ (`len(matches) != 1 ⇒ abort`, never "take the first");
(b) at startup, refuse to run when the target class/app name resolves to more than
one window; (c) distinguish the two cases in the retry ladder — **frozen focus +
one reachable window id ⇒ trap (Escape); frozen focus + N>1 windows ⇒ addressing
error (abort, re-resolve)**, never the same recovery path.

---

## D. Combined per-step algorithm + LLM-call comparison

### D1. Algorithm (a11y-first, Tab as fallback, coordinates last)

```
per task:
  0. PREFLIGHT
     - assert DISPLAY + a WM (_NET_SUPPORTING_WM_CHECK); abort if absent      [C5]
     - assert a session bus + atspi registry; if absent ⇒ pixel-only mode     [B4]
  1. RESOLVE TARGET  (goal -> addressable node, NO coordinates)
     - if a11y: build the tree once  (~3.75 ms; 1 LLM call to name the target)
       match by role+name/description  ->  target node
     - else: pixel/capture path (1 screenshot + 1 LLM call for the bbox)
  2. ACTUATE
     - if node has Action IFACE  -> do_action(0)      (0.08 ms, no keyboard,
                                    no mouse, no coordinates)                [B5]
     - else if node focusable    -> grab_focus()      (0.15 ms) then type/Enter
     - else if a known focus chain-> Tab×N, N from the learned chain
     - else                      -> coordinate click (LAST resort)
  3. VERIFY  (always, never skipped)
     - oracle read: (frame ACTIVE) ∧ (descendant FOCUSED)                    [B3]
     - AND X11 active window == target toplevel                              [C4]
     - AND extents.x != INT32_MIN if the step required visibility             [C3]
     - failure ⇒ bounded retry: Escape (trap) → a11y actuate → coords → abort [C1]
  4. LEARN: append (window class, chain) to the app profile; a chain that did
     not move the focus is recorded as NONE so the next run skips Tab          [C2]
```

### D2. LLM calls per task vs coordinate aiming

Assume a task of S steps on a 6-widget window, and N ~ 12 Tab presses to reach
the far widget.

| approach | LLM calls | screenshots | notes |
|---|---|---|---|
| **Coordinate aiming** | **2 per step** (1 to locate the target in the screenshot, 1 to confirm the click landed) = **2S** | 2S | plus re-locate after every scroll/layout shift |
| **Tab-order, blind** | 1 per step (choose the next control) = **S** | 0–S (only if you want evidence) | unsafe: silently wrong on traps/swallow [C1,C2] |
| **Tab-order + oracle verify** | **S** (the oracle is deterministic code, not an LLM) | 0 | the verify step costs 0 LLM calls |
| **a11y-first (recommended)** | **1 per task** (resolve the target name once) + 1 per *state-changing* step | 0 | actuation and verification are code, not inference |

The sharp result: **verification is free**. The oracle, the X11 check and the
`do_action` call are deterministic reads — they add **zero** LLM calls. So the
Tab-order path and the a11y path both cost ~S calls against coordinate aiming's
2S, and the a11y path drops to ~1 because the target is addressed by *name*
instead of being re-found in pixels each step. Concretely, a 6-step task:
coordinate aiming ≈ 12 LLM calls + 12 screenshots; a11y-first ≈ 1–2 LLM calls and
0 screenshots. Tab-order alone ≈ 6 calls and 0 screenshots, but with C1/C2 failure
modes that a coordinate click does not have.

### D3. What Tab-order buys on the channels it *can* reach

Where the a11y tree is unavailable (Xaw/Tk/canvas/HDMI-captured boxes), Tab-order
still gives: 0 LLM calls per step, no coordinates, no pixel parsing — at the cost
of O(N) time, a C1/C2 risk, and the requirement to verify. It is a *fallback*,
not the primary primitive.

---

## E. Actuate × Capture pairing across the three delivery paths

| Delivery path | Actuate channel | Best Capture channel | Why this pairing | Measured here? |
|---|---|---|---|---|
| **1. Local software (Xvfb/desktop, gopi on the same host)** | XTEST keyboard/mouse (`xdotool`) **plus AT-SPI `do_action`/`grab_focus`** | **AT-SPI a11y tree** (fallback: screenshot) | both live in-process; oracle 3.2 ms vs pixels 196 ms | **yes** — all of §A/§B |
| **2. Hardware HID (CH9329 keys / HID mouse on a remote box)** | HID keyboard (Tab/Enter) or HID mouse (coords) | **HDMI capture / webcam** (no a11y bus on the target) | the target's OS is opaque; only pixels come back | partially — Tab rate computed from UART framing (**assumed**), not run |
| **3. Hybrid (software injection + a11y, HID for boot/firmware)** | software when the agent runs on the target; HID otherwise | a11y when the agent is on the target, capture otherwise | pairing must be chosen per target, not per tool | no |

Key structural point: **an Actuate channel and a Capture channel are only
independent on the HID path.** When the agent runs on the target, the same a11y
tree is both the best actuator (`do_action`) and the best capture (identity +
extents + states in one round trip) — which is why "does the a11y tree also remove
the Capture problem?" (§B5) is answered yes, *for path 1*. On path 2 the a11y tree
is unavailable by construction, so Capture degrades to HDMI/webcam and Actuate to
HD keys — exactly the regime where Tab-order's O(N) cost and C1–C4 risks bite, and
where CH9329's serial rate (§C6) sets a hard floor on how many Tabs are affordable.

---

## What Tab-order cannot do (honest list)

1. **It cannot reach a widget that is not in the focus chain.** gedit's document
   consumes Tab entirely (§C2) — 7 Tabs, zero movement, repeatable. Tab-order only
   works where a focus chain exists and is reachable by Tab.
2. **It cannot address a target directly.** It is O(N) and depends on the *entire*
   preceding chain being stable; one extra widget upstream shifts every subsequent
   step. `grab_focus`/`do_action` address by name in O(1).
3. **It cannot name what it reached.** Tab-order's own output is "focus moved",
   never "focus is `button-2`". Without the a11y oracle you must infer identity
   from pixels (≈196 ms and identity-blind).
4. **It cannot see off-screen arrivals.** Off-viewport widgets report
   `extents.x = INT32_MIN` and change ~0.04% of pixels (§C3).
5. **It cannot work without a WM** (§C5) — no WM, no keyboard focus to move.
6. **It cannot survive focus theft** without a toplevel-level guard, and AT-SPI
   alone will not tell you: it kept reporting the app's widget as focused while
   the popup owned the keyboard (§C4).
7. **It cannot cross an application boundary** reliably — Tab never leaves the
   active toplevel.
8. **It cannot do anything on UI with no focus traversal at all**: canvas,
   OpenGL, games, Xaw/Tk windows (Xaw is absent from the a11y bus entirely, §B4).
9. **It is not cheaper than a direct a11y actuation**: 0.08 ms vs 16 ms per hop,
   and O(1) vs O(N) (§C6, §B5).
10. **It cannot be a primary primitive** — only a fallback for the cases where
    the a11y tree does not exist.

---

## What a Go implementation would need

1. **AT-SPI over D-Bus, no cgo.** Use `github.com/godbus/dbus/v5` (pure Go) against
   `org.a11y.atspi.Registry` on the session bus. The three interfaces actually
   needed: `Accessible` (`org.a11y.atspi.Accessible` — role/name/child count/
   GetChildren), `Component` (GetExtents/`grab_focus`), `Action` (`GetActions`/
   `DoAction`). **No GTK/cgo dependency** — this is the whole point.
2. **Session-bus discovery.** Read `DBUS_SESSION_BUS_ADDRESS`; if absent, try
   `$XDG_RUNTIME_DIR/bus`. On Wayland, the accessibility bridge is a separate
   consideration (out of scope here).
3. **The oracle, correctly.** Mirror §B3: iterate top-level applications, select
   the one whose first child frame has the `active` state, then walk to the
   `focused` descendant. Explicitly **do not** implement "first node with
   `focused`". Bound the walk at depth ≥ 40 and treat a depth-exhausted walk as an
   error, not as "no focus" (§B3).
4. **State decoding.** `GetState` returns an `(uint32, uint32)` bitfield pair; the
   `ACTIVE`/`FOCUSED`/`SHOWING` bit indices map to the `Atspi.StateType` enum
   (FOCUSED = 12, ACTIVE = 11 in at-spi2-core 2.44 — verify against the enum at
   build time rather than hardcoding; the same discipline that made
   `Atspi.state_type_get_name` unnecessary here).
5. **Focus-chain profiles.** Persist `{window_class, [widget names/roles]}` plus a
   `NONE` marker for windows that consume Tab (§C2). Feed `N` to the Tab fallback
   instead of guessing.
6. **Guards as code, not policy.** `INT32_MIN` extents ⇒ OFFSCREEN (§C3); a false
   `frame_active` ⇒ check the X11 active window (§C4); `_NET_SUPPORTING_WM_CHECK`
   ⇒ WM present (§C5). Each guard is a named constant, per Rule 1.
7. **HID path.** CH9329 framing (8-byte report) needs a pluggable
   `ActuateChannel` whose latency budget is a declared constant, so the per-task
   planner can decide Tab × N vs a single action against a real rate — the number
   §C6 currently has to assume.
8. **The verification loop as first-class code**, not an LLM turn: inject → oracle
   → compare → retry, with the retry ladder Enum{Escape, A11yActuate, CoordClick,
   Abort}. This is what makes the LLM-call reduction in §D2 real (0 calls to
   verify).

---

## Re-verification log (independent re-check on 2026-09-10, this host)

Re-ran the load-bearing claims rather than trusting the earlier session's text.
Three of them held; **one defect and one doc error were found and fixed.**

**1. Determinism — CONFIRMED (md5 recomputed from the PNGs on disk):**

```
step00 runA=994b1cc52992 runB=994b1cc52992 IDENTICAL
step01 runA=103a43306464 runB=103a43306464 IDENTICAL
step02 runA=f75d0e5b386d runB=f75d0e5b386d IDENTICAL
step03 runA=fd0a7ccc9e54 runB=fd0a7ccc9e54 IDENTICAL
step04 runA=04a45f21317a runB=04a45f21317a IDENTICAL
step05 runA=bdf4817d7c3f runB=bdf4817d7c3f IDENTICAL
step06 runA=994b1cc52992 runB=994b1cc52992 IDENTICAL
step07 runA=103a43306464 runB=103a43306464 IDENTICAL
step08 runA=f75d0e5b386d runB=f75d0e5b386d IDENTICAL
```

and the focus chain re-read from both JSON logs is identical (9 steps each):
`["entry-1","check-1","button-1","entry-2","radio-1","button-2","entry-1","check-1","button-1"]`.

**2. AT-SPI — CONFIRMED WORKING** (`pyatspi` still absent; introspection path only):

```
$ /usr/bin/python3 -c "import gi; gi.require_version('Atspi','2.0'); from gi.repository import Atspi; print('Atspi OK', hasattr(Atspi,'get_desktop'))"
Atspi OK True
$ /usr/bin/python3 -c "import pyatspi"
ModuleNotFoundError: No module named 'pyatspi'
$ /usr/bin/python3 focus_probe.py --apps --json
{"applications": [{"role": "application", "name": "xfwm4"}, {"role": "application", "name": "gedit"},
                  {"role": "application", "name": "gtk_probe_app.py"}]}
```

**3. Coordinate-free actuation — CONFIRMED live** (fresh app, `do_action` on
`button-2`, no mouse, no keyboard, no coordinates):

```
$ /usr/bin/python3 a11y_actuate_probe.py --app gtk_probe_app --name button-2 --json
{"app": "gtk_probe_app.py", "target": {"role": "push button", "name": "button-2"},
 "grab_focus_returned": true, "focused_after_grab": {"role": "push button", "name": "button-2"},
 "extents": [55, 255, 500, 34], "action_iface": {"n_actions": 1, "names": ["click"]},
 "do_action_0_returned": true}

$ /usr/bin/python3 focus_probe.py --oracle --json      # AFTER
{"app": "gtk_probe_app.py", "frame": "gopi-tab-probe", "frame_active": true, "focused_count": 1,
 "focused": {"role": "push button", "name": "button-2", "states": ["enabled","focusable",
             "focused","sensitive","showing","visible"], "extents": [55, 255, 500, 34]}}
```

`vision_analyze` on the cropped screenshot taken 200 ms later reads the status
label **"clicked: button-2"** — the button's real `clicked` signal fired from a
name-addressed a11y action, with zero coordinates involved.

**4. Off-screen sentinel — CONFIRMED, 30/30 enumerated** (§C3):

```
$ /usr/bin/python3 focus_probe.py --tree --app gtk_probe_app --depth 40 --json
total nodes: 39      scroll entries found: 30
INT32_MIN offscreen entries: 23      on-screen entries: 7
   scroll-entry-00 [55, 79, 500, 34]   …   scroll-entry-06 [55, 319, 500, 34]
   scroll-entry-07 [-2147483648, -2147483648, 500, 34]
   scroll-entry-08 [-2147483648, -2147483648, 500, 34]
```

**5. DEFECT FOUND AND FIXED — `--tree` silently truncated at depth 3.** A casual
`--tree` reported **5 nodes and 0 scroll entries** where the truth is **39 nodes
and 30 entries** — because `DEFAULT_TREE_DEPTH = 3` stopped the walk exactly at
the `filler` node, one level above the `scroll pane`. This is §B3's depth trap
reappearing in a *second* code path (there it was the focus search at depth 12 vs
gedit's depth 16; here the tree dump). Same lesson, sharper: **a truncated walk
does not degrade gracefully, it returns a confident wrong answer.** Fixed by
defaulting to the full bound:

```
-    DEFAULT_TREE_DEPTH = 3
+    DEFAULT_TREE_DEPTH = MAX_TREE_DEPTH      # narrow explicitly with --depth
```

Verified after the fix — the same command now reaches the entries:
`nodes: 10, entries: 2` (plain layout, depth ≥ 40 no longer required). The first
attempt at this fix also **broke the script** (`NameError: MAX_TREE_DEPTH is not
defined` — the alias was placed above the definition); caught by `ast.parse` +
the immediate re-run, then reordered. Both are recorded here rather than quietly
repaired.

**6. Doc error found and fixed** — this file claimed **66 PNGs**; the real count at
that moment on disk was **57** (`find … -name '*.png' | wc -l`). (It is 66 again
only because this re-verification added the 9 `rerunC_*` PNGs — the claim was
wrong when written, and is honest only with that provenance stated, as at the top
of this file.) §C3 also carried a
self-contradictory sentence ("All 30 entries report `extents.x = 55` but only 7
are inside the viewport"); the measurement behind it is sound (7 visible / 23
sentinel) but the wording was wrong and is corrected.

**Still not verified:** the Gecko/firefox row and the CH9329 timings — see *Not
Done* above; both remain assumptions, not measurements.

**7. Third independent run (rerunC) — CONFIRMED, and it EXPOSTS a flattering artifact
in the original §A evidence.**

Requested an extra end-to-end run of `tab_order_poc.py` in a fresh process to
verify my `focus_probe.py` edit had not broken the driver. It passed, but only
after **two invocation bugs and one environmental trap** were cleared — all
recorded here because each is a real operational hazard:

```
$ tab_order_poc.py --run-id rerunC            # FAILS: xdotool search --class gopi-tab-probe times out
$ tab_order_poc.py --run-id rerunC --match "--class gtk_probe" --app-cmd "..."
   -> FOCUS-CHAIN=[null,null,...]             # FAILS: --app-filter default is the WINDOW TITLE,
                                              # but the AT-SPI application name is 'gtk_probe_app.py'
$ tab_order_poc.py --run-id rerunC --match "--class gtk_probe" --app-filter gtk_probe_app --app-cmd "..."
   -> FOCUS-CHAIN=["button-1" x9]             # WRONG: Tab never moved the focus
```

The third failure was the interesting one: **a second `gtk_probe_app.py` instance
was still alive.** Both instances share (a) the X WM_CLASS `gtk_probe_app.py` and
(b) the AT-SPI application name `gtk_probe_app.py`, so `xdotool search` matched
two windows, the oracle read focused state from *one* instance while Tab keys
went to *another*. Result: a frozen chain that *looks* like a focus trap (§C1) but
is actually an addressing collision. **This is a new failure mode — C7 below.**

With exactly one instance, the run reproduces the chain exactly:

```
FOCUS-CHAIN=["entry-1", "check-1", "button-1", "entry-2", "radio-1", "button-2",
             "entry-1", "check-1", "button-1"]
```

**The artifact:** rerunC's PNGs are NOT md5-identical to runA/runB — but the
difference is *entirely outside* the probe window:

```
step00 A=994b1cc52992 B=994b1cc52992 C=80feb59fb555   (full-screen md5)
  changed OUTSIDE the probe window : 836380 px
  changed INSIDE  the probe window :      18 px   (all on the bgcolor titlebar corner ring)
  INTERIOR (excluding 6px border)  :       0 px   max_delta=0   at ALL 9 steps
$ unique colors in runA['s outside area: 1   (pure black — empty desktop)
$ unique colors in rerunC's outside area: 226 (gedit + WM decorations visible)
```

So runA and runB happened to run on a **blank desktop**, which made the whole
screen byte-identical *for free*; rerunC ran against a live desktop and the
full-screen hash moved with the background, not with the focus. **The §A claim
survives in its strong form — the probe window interior is pixel-identical at
every step (max_delta = 0) — but full-screen md5 identity was never the right
metric, and §A's presentation of it was generous to the result.** The honest
statement: *the focus chain and the window's own pixels are deterministic; a
whole-screen hash additionally measures whatever else is on the desktop and
must be replaced by a window-interior comparison.* This is a correction to §A,
not a refutation of it.

---

## Completed

- Working POC under `devdoc/tab-order-poc/`: `tab_order_poc.py` (drive Tab +
  screenshot + 3-channel log), `focus_probe.py` (AT-SPI reader, `--apps/--tree/
  --focus/--oracle/--chain`), `visual_diff_probe.py` (pixel diff), plus
  `channel_bench.py`, `tab_rate_bench.py`, `a11y_actuate_probe.py`,
  `gtk_probe_app.py` (reference GUI w/ trap + scroll + invisible variants),
  `popup_probe.sh`.
- Two independent full runs, byte-identical (`shots/runA_*`, `shots/runB_*`),
  plus a third independent run `shots/rerunC_*` launched from a fresh process —
  which re-verified the chain and **corrected the §A evidence** (see the
  *Re-verification log*, item 7: whole-screen md5 identity was flattered by a
  blank desktop; the window *interior* is the sound metric and is 0 px changed).
- AT-SPI reached on this host via GObject introspection (pyatspi absent) and
  **shown working**; the `focused`-flag trap found and fixed with the
  frame-`active` oracle.
- Seven adversarial modes reproduced: focus trap, Tab-swallowing real app,
  off-screen focus, focus-stealing popup, no-WM, non-a11y toolkit (Xaw),
  duplicate-instance addressing collision (§C7 — newly found during re-verification).
- The strong result: `grab_focus` (0.15 ms) and `do_action` (0.08 ms) actuate a
  widget **by name with no coordinates and no keyboard**, verified structurally
  and visually.
- Sections A–E written, plus the cannot-do list and the Go notes.

## Not Done

- **Firefox/Gecko a11y was not verified** — snap firefox exits on this host
  (`not a snap cgroup`), so the Gecko row of §B4 rests on the general AT-SPI
  toolkit mechanism, not on a measurement here. Chromium was not tested either.
- **CH9329 timings are computed from UART framing, not measured** — no CH9329
  hardware on this host, and the vendor page text was unfetchable (CSDN 521,
  Zhihu 403, wch.cn returns a near-empty page). Flagged as an assumption in §C6.
- **HID and hybrid delivery paths (2, 3) were not exercised** — no hardware.
- **Wayland** was not tested (`:99` is Xvfb/X11).
- Tab-order was not tested against a web page's DOM focus (firefox unavailable).
- No Go code was written — production implementation is explicitly out of scope.

## Verification

- **Scripts**: all ran on this host; raw output pasted verbatim in §A–§C.
- **AT-SPI**: exact command shown — `/usr/bin/python3 -c "import gi; gi.require_version('Atspi','2.0'); from gi.repository import Atspi"` → `Atspi import OK True`; `--apps` listed `xfwm4, gtk_probe_app.py, gedit, zenity`. **Working**, not unavailable.
- **Liveness of the focus claim**: AT-SPI (identity) ∧ pixel diff (bbox) ∧
  `vision_analyze` (reads `focus: entry-1` / `check-1` / `clicked: button-2`) all agree.
- **Screenshots on disk**: `devdoc/tab-order-poc/shots/` — 66 PNGs
  (`runA_step00..08`, `runB_step00..08`, `rerunC_step00..08`, `scrollA_*`, `trapA_*`,
  `focus_cmp_*`, `focus_diff_overlay.png`, `a11y_before/after_button2*.png`).
- **Determinism**: md5 of step N identical between runA/runB for all 9 steps; the
  6-widget cycle closes byte-exactly (`step00 == step06`). For the third run the
  sound metric is the **window interior** (0 px changed, max_delta = 0 at all 9
  steps) — see the Re-verification log item 7 on why whole-screen md5 was flattered.
- **Go**: not applicable (devdoc + Python POC only; no Go changes ⇒ no worktree,
  no build). No `go build`/`go test` was run because no Go file was touched.
