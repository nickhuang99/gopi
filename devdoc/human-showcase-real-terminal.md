<!-- doc-class: historical -->
# The showcase was recorded in a terminal nobody uses — and the mask moved with it

Task `01a0f677-c52e-7ed3-a0e5-6236829fbb4d`, 2026-10-01. Two findings worth keeping:
one about what "record the real thing" means, one about a masking defect that every
existing check was structurally unable to see.

## 1. A capture of gopi in an xterm is a capture of xterm

`docs/archive/demo/human/` photographs gopi's human interface. It was recorded in **xterm**,
chosen as "the internal mechanism" — no profile, no server, always present. That is
a good property for a *test fixture* and the wrong one for a *showcase*: xterm
renders the 16 ANSI colours from its own built-in table and never reads the user's
palette.

Measured, on identical bytes fed to both (`tools/human-ui/make_sample.py`, 782 B,
23 declared swatches read back by `tools/human-ui/compare_terms.py`):

| leg | distinct colours | bg | swatches differing from the reference |
|---|---|---|---|
| `xfce4-terminal`, the operator's own profile | 692 | `#131926` | — (reference) |
| `xterm -fs 13` | 323 | `#ffffff` | **14 of 23, all ANSI** |

The 256-cube entries matched exactly in both legs — index 178 *is* `#d7af00` and no
terminal gets to disagree. That asymmetry is the whole reason the distortion was not
obvious: gopi's interface is drawn mostly from the 256-cube, and the part that was
wrong is the part that carries the brand.

**The recorder now probes for the host's terminal** (`--term` → `$GOPI_HUMAN_UI_TERM`
→ the login desktop → a profile's presence → `xterm`), and **calibrates the glyph
scale by measurement** rather than declaring it: the operator's own size gives
10.13 px/char at 120 columns, below this directory's 16 px floor, so the script tries
candidates until one clears it (`GDK_DPI_SCALE=1.8` → 17.133 px/char). The reason
and every candidate measurement land in the delivered `manifest.json` — a manifest
that named a terminal the run did not use would be worse than none.

**Fidelity is proved by re-recording, not by asserting.** `compare-terminals.sh`
copies the operator's profile, changes `ColorPalette[1]` and the background, points
the same terminal at the copy through `XDG_CONFIG_HOME`, and re-captures. The check
is a gate: if the variant leg's `ansi1` does not move, the script exits non-zero —
"this is the xterm failure mode, in the leg that is supposed to prove the opposite."

## 2. The leak: mask boxes placed by an assumed grid origin

Re-recording in a terminal **with chrome** silently broke the masking, and the way
it broke is the transferable part.

`assemble.py` computed each mask box as `row × (img_h / rows)` — i.e. it assumed the
cell grid started at the window's first pixel. `xfce4-terminal` draws a **~37 px menu
bar** above the grid, so every box sat about **one row too high**: it landed on an
*empty* row, painted the substitute ink there, and the **session UUID stayed in the
published clean still**.

Why no check saw it:

- **Containment (check B)** asks only whether *changed* pixels lie **inside** a
  recorded box. A box one row high satisfies it perfectly — the paint is inside the
  box, it is simply inside the *wrong* box.
- Every other check asked the **dump** (`ui_state`'s grid), whose row indices were
  right all along. Nobody asked the **pixels**.

Two fixes:

- **`assemble.py: locate_grid()`** — the geometry is now *measured*, from the picture
  and the dump together. The window height gives a one-parameter family
  (`y0 = H − rows·cell_h`), and each candidate is scored by the one property that
  must hold: a dump row carrying text has ink in its band, a dump row without text
  has none (the scrollbar column excluded — it is ink on every row). Unreconcilable
  picture and dump → **refuse**, never guess. Measured on the real capture:
  `y0 = 36, cell_h = 34.075`, i.e. 36 px of chrome, against the old assumption of 0.
- **`check-clean.sh: check D`** — every recorded mask box must contain **ink** in the
  raw picture. Red on the pre-fix artifacts (`empty-boxes=1` for `chat.png` and
  `hero.png`), green after. This is the check class that was missing: B proves the
  mask *covered* something; D proves it covered *the thing*.

**The generalisable lesson**: a redaction gate that only verifies containment
verifies the *shape* of the edit, not its *target*. Whenever a mask is placed by a
geometry, assert that the target was actually there — otherwise a shifted geometry
fails green and publishes.

## 3. The web leg

`gopi shot` (headless chromium, its own CDP client) vs a real chromium in Xvfb:
**headless is kept**, and it is a measured choice rather than inertia. The browser
leg paints chrome's own *"You are using an unsupported command-line flag:
--no-sandbox"* banner into the frame (`top_band_is_banner: true`, top-band luminance
248 vs 20), and it displaces the line `web.png`'s mask was measured for. Two further
costs on this host: snap chromium dies as an ordinary child (`--version` exits 133
under AppArmor) and only runs as a transient systemd user unit; and a composed
render has no cell grid, so its masks are bands while every other artifact's are
cells. `scripts/human-ui/compare-web.sh` reproduces both legs.

## Not done

- `docs/archive/demo/final/` was not re-assembled against the new stills (a separate
  delivery step; the stills themselves are verified by `check-clean.sh`).
- The second full re-record refused to assemble — correctly: its dump's grid could
  not be located in its picture. The shipped set is the record whose geometry
  locates and whose gate is green, so pixels and `redaction.json` come from one
  capture.
