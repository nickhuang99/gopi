<!-- doc-class: historical -->
# The human interface to gopi — how the showcase was recorded

**Task**: `01a0efbd-8a83-7d83-b9be-ccd0debced09` · **Branch**: `feat/human-ui-showcase`
**Deliverable**: `docs/archive/demo/human/` (hero.png, chat-loop.gif, three stills, raw/ + clean/, README.md, manifest.json, redaction.json, pxdiff.tsv)
**Tooling**: `scripts/human-ui/build.sh`, `tools/human-ui/{assemble,redact,frame_prep,pxdiff,seed_web}.py`, `tools/human-ui/check-clean.sh`

This devdoc is the *why* behind the numbers that `docs/archive/demo/human/README.md`
publishes for a reader. It is the record of a session that spent most of its time
on six measurements, each of which changed the design.

---

## 1. The recorder had to be gopi, so half the work was finding what could be run

The constraint was "no new dependency" — no ffmpeg, ImageMagick, asciinema or OBS.
The repo turned out to have the whole chain already: `gopi capture` (a hand-rolled
X11 `GetImage` client, pure Go), `gopi shot` (gopi's own CDP client),
`gopi gif` (`internal/gifgen`), `gopi annotate` and `gopi text2img`
(`internal/text2img`), plus `gopi sandbox` for isolation. Outside the binary only
Xvfb, xterm and xdotool are used. The four Python helpers depend on **PIL alone**,
which `scripts/materials/render-entry.py` already established as an ingredient of
this repo's material scripts.

That left the question of *what can be run at all*, and the answers were not
obvious.

## 2. `gopi shot` fails on this host by default, and its error message misdirects

Measured, verbatim:

```
shot: chrome did not report a DevTools port within 15s (stderr: ) — falling back to
an attached launch, which cannot escape the inherited address-space limit (chrome
reserves ~51 GiB of virtual address space); chrome may fail to start under this clamp
gopi shot: shot: chrome exited without printing a DevTools port (stderr consumed)
```

`internal/shot/launch.go` starts chromium through a `systemd-run --user` **unit**
so it escapes the session's process limits — the design is right. What kills it is
the unit's **default 512 MiB memory ceiling**: chromium dies (exit 153) before it
can print a port. `GOPI_SHOT_MEM_MAX_MB=4096` makes `gopi shot --self` succeed in
5.5s.

Two details are worth keeping, because they cost time:

- The fallback message names the **address space**, and that is a red herring. Asked
  directly, the sandbox is fine: `systemd-run --user --unit=probe --collect --wait
  --pipe /snap/bin/chromium --version` runs. The limit is the ceiling, not the
  isolation.
- `serverType=notify` **requires `NotifyAccess=all`**. The unit's `Type` is
  configurable (`GOPI_SHOT_UNIT_TYPE`) precisely because a launch path that works
  for notify does not work for simple, and the default is picked fresh for each
  chromium. Launch this by hand as a service unit and the process is reaped before
  it prints anything, which looks like the direct `SIGTRAP` failure and is not.

## 3. A window can be wider than the screen — and the capture does not tell you

An xterm asked for `110x30` at `13pt` came out **2094px wide** (a 19px cell,
measured) on a 2400px screen; the same request against a 1400px Xvfb produced a
**3124px** window. `gopi capture --window` then returns the window's request,
CLIPPED to the screen, with dead grey margins — a picture that is wrong in a way
no hash reveals, because it is consistently wrong in every frame.

`build.sh` therefore asserts `WIDTH <= SCREEN_W && HEIGHT <= SCREEN_H` from
`xdotool getwindowgeometry` and refuses to capture otherwise.

## 4. A keyboard event needs the pointer, and `xdotool type` cannot type Chinese

A bare Xvfb has no window manager. `xdotool windowfocus` is a **no-op** there, and
XTEST delivers key events to the window **under the pointer** — so typing silently
goes nowhere (the first probe produced a session with zero model turns and no error
anywhere). The build parks the pointer inside its own xterm first:

```bash
eval "$(xdotool getwindowgeometry --shell "$wid")"     # sets X Y WIDTH HEIGHT
xdotool mousemove $((X + WIDTH/2)) $((Y + HEIGHT/2))
```

`xdotool type` also **cannot deliver a multi-byte character** — it fails with
"Invalid multi-byte sequence encountered". So the *typed* prompt is ASCII and the
*answer* is Chinese: the model answers in the operator's language, and the answer is
the half worth showing. The alternative (feeding keys as Unicode codepoints) would
have been a second, weaker input path for a demo that already shows a real one.

## 5. The film had to be sampled fast, and reduced to distinct states

An answer here renders in about **two to four seconds**. The first sampler slept
between grabs and returned the gap before the answer and the stillness after it: a
film of one state. Worse, it made a *wrong* claim easy to reach — the still frames
hashed identically, so a hash-based check would have reported "not moving" about a
film that was fine, and the converse error (a caret blink changing the hash while
the picture stood still) was just as available.

The fix has two halves, and both are load-bearing:

1. **Sample as fast as the capture path allows** (no sleep), stopping only when the
   picture has settled for N grabs or a bound is hit.
2. **Reduce to DISTINCT states.** A frame identical to its predecessor is not
   merely redundant: the GIF gives it its own delay (a visible stutter) *and* the
   per-frame diff table would report a **zero-change pair**, converting the film's
   own motion evidence into a claim that it stood still.

Measured on the first full run: 12 typing grabs → 11 distinct; 64 streaming grabs
→ 28 distinct; 37 identical frames dropped; 39 delivered, with 38 adjacent pairs
none of which had zero changed pixels. The **shipped** artifacts come from the
final run (taken while this devdoc was being written): 11 of 12 typing grabs
distinct, 35 of 75 streaming grabs distinct, 41 dropped, 46 delivered, and **45
adjacent pairs, none with zero changed pixels**, min 0.056%, max 23.5%. The counts
are per-run and `docs/archive/demo/human/manifest.json` is the authoritative record of the
run whose images are in the tree; the RECIPE is what generalizes — the evidence
that the film moves is `pxdiff.tsv`, measured on pixels, never a hash.

## 6. The mask is painted on the DELIVERED image — this is the whole redaction story

The first implementation masked the RAW grab and then cropped and scaled it. That
is wrong, and `check-clean.sh` refused it: the downscale averages ~1.8 source
pixels per destination pixel, so the mask's footprint spread about a pixel past
every recorded rectangle (the hero's record said `y=225`; the first changed pixel
sat at `y=224`). The verdict is correct and matters: **a record that does not
contain the change is not a record.**

The fix removes the question rather than tuning a margin. The clean twin is built
**from the delivered raw twin** and the mask is painted on it in recorded
coordinates, so the set of changed pixels *is* the set of recorded rectangles.
Containment then holds by construction: `chat.png`, `hero.png` and `web.png` all
report `changed-outside-box=0`.

Two further measurements are baked into `redact.py`:

- **The substitute is filled cells, never a typeface glyph.** A full-width `█` in
  NotoSansCJK advances about **1.65x** a 19px terminal cell, so a 36-character run
  overshot its 685px span to over 1100px and painted across the rest of the line —
  a mask WIDER than the fact it hid, caught by containment as change outside the
  box. Filled cells are exact by construction (`w // len(secret)`), independent of
  the fonts a machine happens to have. (This was the second defect the gate caught.)
- **The fill is sampled from the box's own surroundings**, not assumed white. A
  white rectangle on a dark interface is the brightest thing on screen and draws the
  eye to the line that was redacted. Verified by eye: the masked run is visually
  indistinguishable from the terminal background, and the surrounding text on the
  same line is intact. (An early `TypeError` here was harmless — `shutil.copyfile`
  takes two paths, not three — but it is why `assemble.py` has no third path in that
  call today.)

The mask patterns are **derived, never typed**: UUID and 32-hex shapes are generic,
and the operator's user name and hostname are read from the host record at run time.
A literal name compiled in would match nothing on another machine and still report
"0 replacements" — a redaction that lies.

## 7. The gif command is recorded sanitized

`manifest.json`'s recorded command is delivered prose, so it may not carry an
absolute path from this host. Both the frames directory and the output directory are
replaced by placeholders — and the binary path too, because recording only the first
left `/home/user/...` in the manifest. `check-clean.sh`'s text scan caught each
version; that is the check earning its place, since the leak was in a file the mask
tool never touches (it is a PNG tool).

---

## Verification recipes worth keeping

- **A GIF's motion is a PIXEL measurement.** `pxdiff.py` per adjacent pair, exit
  non-zero on any zero-change pair. Never `sha256_16` on live-screen frames: a caret
  blink changes the hash while the picture stands still, and a stale grab can hash
  identically to the previous one.
- **Redaction is proved by two checks, and the first is the one people skip.** A
  positive control (the secrets must be findable in the RAW material — otherwise
  "the clean images contain no uuid" is trivially true of a run that never had one)
  plus containment (every changed pixel inside a recorded box). A vacuously passing
  redaction check is worse than a failing one, because it reads as evidence.
- **Mask the DELIVERED image, at coordinates you record from the same list you
  paint from.** One list, used twice: the record and the paint cannot drift.
- **Assert the window fits the screen before capturing.** A clipped window is a
  wrong artifact with no error anywhere.
- **A bare Xvfb needs the pointer, not focus.** `xdotool windowfocus` is a no-op
  without a WM.

## Deliverable location

Branch `feat/human-ui-showcase` in the main repo (the run's worktree shares the
repo's refs, so the commit is already there — nothing to push). Recover a file with:

```bash
git show feat/human-ui-showcase:docs/archive/demo/human/README.md
git show feat/human-ui-showcase:scripts/human-ui/build.sh
```
