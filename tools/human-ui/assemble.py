#!/usr/bin/env python3
"""assemble.py — turn build.sh's raw grabs into the delivered artifacts.

Phase 4 of the human-ui showcase, deliberately a SEPARATE file from build.sh:
capturing needs a live display and a real model turn (expensive, timing
dependent), while assembling is arithmetic over PNGs (cheap, re-runnable). A
reader can therefore re-derive every size, crop and mask without paying for
another model round trip.

WHAT IT PRODUCES (into --out)
  hero.png            the conversation, delivered at the capture's own resolution
  chat-loop.gif       typed prompt + the streaming answer, >= 12 DISTINCT frames,
                      >= --gif-min-px-per-char PIXELS PER CHARACTER
  chat.png ui.png     the two terminal surfaces
  web.png             the browser render, from `gopi shot`
  raw/ clean/         every delivered image twice: as captured, and masked
  frames/             the clean film's frames as PNGs, so it is auditable
  redaction.json      every mask box actually applied (the containment record)
  manifest.json       inputs, commands, sizes and measured evidence

ORDER MATTERS, AND IT IS THE POINT OF THIS FILE. The mask is applied to the RAW
grab, BEFORE any crop or scale, because that is the coordinate system each
surface's own cell grid describes (row/col in the dump == the picture's cells).
Masking after a resize would need every box scaled and rounded by hand — a
second geometry nobody checks. Here raw and clean share one crop/scale chain, so
they are the same picture by construction: one fact withheld, no glyph moved.

WHICH DUMP MASKS WHICH IMAGE. A mask box is only valid for the grab it was
measured on. The chat still and the film therefore mask from the CHAT dump, and
the ui still from the UI dump; feeding one surface's grid to another's pixels
would place boxes at plausible-looking wrong coordinates — the worst kind of
redaction bug, because it leaves no visible trace.
"""
import argparse
import glob
import json
import math
import os
from collections import Counter
from PIL import Image
import re
import shutil
import subprocess
import sys

try:
    from PIL import Image, ImageChops
except ImportError:  # pragma: no cover
    sys.exit("assemble: python3-PIL missing")

MASK = "█"

# Pixels of margin around every mask, so an antialiased glyph edge cannot survive
# as a readable sliver of the secret at the box boundary. The margin lives INSIDE
# the rectangle that gets painted AND recorded — one geometry, so the containment
# check (every changed pixel must fall in a recorded box) can actually pass and
# therefore actually mean something.
MASK_PAD = 2

# The right-edge strip that is a terminal's scrollbar: it is ink on every row, so
# a row-ink test that reads it declares every row of the screen inked.
SCROLLBAR_GUARD = 40
# A pixel counts as ink this far (manhattan) from the picture's own background.
GRID_INK_DELTA = 60
# The tallest terminal chrome this pipeline tolerates above the cell grid (px):
# a menu bar and a border, not a toolbar rack. It bounds the geometry search.
MAX_CHROME = 80


# What counts as "identifying" for the mask. Every pattern is either derived
# from this host at run time or is a SHAPE (a UUID, a long hex id) that carries
# no meaning by itself — never a literal name typed into this source, which on
# another machine would match nothing and still report success.
SECRET_PATTERNS = [
    r"[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}",  # session/run uuid
    r"[0-9a-f]{32}",                                                    # task id
    r"sandbox-[A-Za-z0-9-]+-[0-9a-f]{8}",                                # sandbox dir name
    r"/home/[A-Za-z0-9_.-]+",                                            # home path
]


def parse_args():
    p = argparse.ArgumentParser(add_help=True)
    p.add_argument("--scratch", required=True)
    p.add_argument("--out", required=True)
    p.add_argument("--tools", required=True)
    p.add_argument("--bin", required=True, help="gopi binary (for `gopi gif`)")
    p.add_argument("--term-cols", type=int, default=120)
    p.add_argument("--term-rows", type=int, default=40)
    p.add_argument("--hero-width", type=int, default=0,
                   help="delivered hero width; 0 = the capture's own width (the default, and the "
                        "only value that keeps a terminal glyph at its captured size)")
    p.add_argument("--gif-min-px-per-char", type=float, default=16.0,
                   help="LEGIBILITY FLOOR: the delivered film must give every terminal cell at "
                        "least this many pixels. The film is delivered at the capture's own "
                        "resolution whenever that already clears the floor (it does: 2284px / "
                        "120 cols = 19.03), because downsampling a text screenshot is what made "
                        "an earlier cut of this film unreadable at 8.00 px/char")
    p.add_argument("--gif-delay", type=int, default=45)
    # WHICH TERMINAL the frames are pictures OF, and what decided it. The choice is
    # probed at run time (scripts/human-ui/build.sh), never written into this file,
    # so it is passed in and recorded — a manifest that named a terminal the run did
    # not use would be worse than no manifest.
    p.add_argument("--term-kind", default="", help="xterm | xfce4 | gnome (what the run actually used)")
    p.add_argument("--term-why", default="", help="how it was chosen (the probe's reason)")
    p.add_argument("--term-profile", default="", help="which profile the terminal rendered with")
    p.add_argument("--term-size", default="", help="the calibrated size knob (e.g. GDK_DPI_SCALE=1.8)")
    p.add_argument("--term-size-table", default="", help="path to the calibration table (tsv)")
    p.add_argument("--web-banner-band", default="",
                   help="Y0,Y1 of a composed render's own banner line, masked because a "
                        "browser render has no cell grid to locate a box in")
    return p.parse_args()


def run(cmd):
    r = subprocess.run(cmd, capture_output=True, text=True)
    if r.returncode != 0:
        sys.exit("assemble: %s\n  stdout: %s\n  stderr: %s" % (" ".join(cmd), r.stdout.strip(), r.stderr.strip()))
    return r.stdout.strip()


def size_of(path):
    with Image.open(path) as im:
        return im.size


def png_frames(dirpath):
    if not os.path.isdir(dirpath):
        return []
    return [os.path.join(dirpath, n) for n in sorted(os.listdir(dirpath)) if n.endswith(".png")]


def pixels_differ(a_path, b_path, threshold=8):
    """Count pixels differing by more than `threshold` in any channel.

    `threshold` is not zero because the capture path re-encodes the same picture
    with slightly different antialiasing at the edges of glyphs; a handful of
    one-level differences is a repaint, not a new state.
    """
    a = Image.open(a_path).convert("RGB")
    b = Image.open(b_path).convert("RGB")
    if a.size != b.size:
        return None
    diff = ImageChops.difference(a, b).convert("L")
    return sum(1 for v in diff.getdata() if v > threshold)


def dedupe(paths, threshold=8, label=""):
    """Drop frames pixel-identical to their predecessor.

    A frame showing exactly what the previous one showed carries no information,
    and keeping it is not neutral: the GIF gives it its own delay, so the film
    plays a stutter — AND the per-frame diff table would report a zero-change
    pair, turning the very evidence that the artifact moves into a claim that it
    stood still. So the film keeps distinct STATES, and the dropped count is
    recorded rather than hidden.
    """
    if not paths:
        return [], 0
    kept = [paths[0]]
    dropped = 0
    for p in paths[1:]:
        n = pixels_differ(kept[-1], p, threshold)
        if n is None:
            sys.exit("assemble: frame size changed mid-sequence at %s" % p)
        if n == 0:
            dropped += 1
            continue
        kept.append(p)
    if label:
        print("assemble: %s kept %d/%d distinct frames (dropped %d identical)" %
              (label, len(kept), len(paths), dropped))
    return kept, dropped


def grid_rows(dump_path):
    """The dump's rendered screen, as {row_index: cell_text}.

    Only the `grid_*` section is read: it is what the terminal shows NOW, while
    `lines_*` is the frame DIFF log. Reading the wrong one is a documented way to
    conclude "the screen is empty" from a dump that records the whole screen, so
    this parser never looks at the other section.
    """
    rows, inside = {}, False
    if not dump_path or not os.path.exists(dump_path):
        return rows
    with open(dump_path, encoding="utf-8", errors="replace") as fh:
        for line in fh:
            if line.startswith("grid_begin"):
                inside = True
                continue
            if line.startswith("grid_end"):
                break
            if not inside:
                continue
            m = re.match(r"^(\d{3})\|(.*)$", line.rstrip("\n"))
            if m:
                rows[int(m.group(1))] = m.group(2)
    return rows


def locate_grid(im, grid, cols, rows, bg=None):
    """(y0, cell_h) — where the terminal's CELL GRID actually sits in a grab.

    THIS IS NOT img_h/rows, and assuming it was is a leaked secret. A real
    terminal draws its own chrome ABOVE the grid: xfce4-terminal on this host puts
    a menu bar of ~37px there, so the window is `chrome + rows*cell_h` tall and
    `row * img_h/rows` names a band ~one row too high. Measured on the first run
    of the real-terminal change: the session UUID's mask boxes landed on an EMPTY
    row and the UUID itself stayed in the delivered clean still — published, and
    invisible to every check, because the checks all asked the dump (whose grid
    was right) and never the pixels.

    So the geometry is MEASURED here, from the picture and the dump together. The
    window height gives a one-parameter family — y0 = H - rows*cell_h — and the
    dump says which rows carry text, so each candidate cell_h is scored by the one
    property that must hold: a dump row with text has ink in its band, a dump row
    without text has none (the scrollbar column is excluded; it is ink on every
    row). The candidate that explains every row wins; if none does, the picture
    and the dump describe different screens and the caller REFUSES rather than
    guessing — a mask computed from a geometry nobody verified is worse than no
    mask at all.
    """
    w, h = im.size
    if bg is None:
        bg = Counter(im.getdata()).most_common(1)[0][0]
    px = im.load()
    x_max = w - SCROLLBAR_GUARD      # the scrollbar is ink on EVERY row
    ink = []
    for y in range(h):
        hit = False
        for x in range(0, x_max, 3):
            p = px[x, y]
            if abs(p[0] - bg[0]) + abs(p[1] - bg[1]) + abs(p[2] - bg[2]) > GRID_INK_DELTA:
                hit = True
                break
        ink.append(hit)
    row_has_ink = {r: (str(t).strip() != "") for r, t in grid.items()}
    # The pass/fail test alone has a PLATEAU, and the honest description of it is
    # that no origin can be told apart from one a few pixels away: a glyph row
    # keeps its band inked until the drift is large enough to push its ink out
    # (measured on a synthetic 36px-chrome screen: origins 30..36 all explain every
    # row). That is tolerable because what a mask must do is CONTAIN its target, not
    # start at a particular pixel — so among the plateau the winner is the candidate
    # that puts the most LEADING around the ink it claims: for every text row, the
    # smaller of (ink_top - band_top) and (band_bottom - ink_bottom), maximised over
    # rows. A mask centred on its glyph survives a rounding difference; one flush
    # against the ink does not.
    ink_rows = [y for y in range(h) if ink[y]]
    best = None
    steps = int(round(MAX_CHROME)) + 1
    for i in range(steps):
        y0 = i                                   # the chrome band, 0 .. MAX_CHROME
        cell_h = (h - y0) / float(rows)
        if cell_h <= 0:
            continue
        good = 0
        margin = None
        for r in sorted(grid):
            a = int(round(y0 + r * cell_h))
            b = int(round(y0 + (r + 1) * cell_h))
            a, b = max(0, a), min(h, max(a + 1, b))
            band = [y for y in ink_rows if a <= y < b]
            if bool(band) != row_has_ink[r]:
                continue
            good += 1
            if row_has_ink[r] and band:
                m = min(band[0] - a, b - 1 - band[-1])
                margin = m if margin is None else min(margin, m)
        key = (good, margin if margin is not None else -1)
        if best is None or key > best[0]:
            best = (key, y0, cell_h)
    if best is None or best[0][0] < len(grid):
        return None
    _, y0, cell_h = best
    return y0, cell_h


def dump_boxes(dump_path, cols, rows, img, patterns):
    """Mask boxes, in the grab's own pixels, for every secret its grid shows.

    Refuses (returns []) when the grid cannot be located in the picture: an empty
    mask list makes the caller ship the RAW picture unmasked, which the cleanliness
    gate then fails loudly — the correct outcome, and far better than boxes placed
    by an unverified geometry.
    """
    grid = grid_rows(dump_path)
    if not grid:
        return [], None
    geom = locate_grid(img, grid, cols, rows)
    if geom is None:
        return [], None
    return boxes_for_geom(grid, geom, img.size[0] / float(cols), img.size[0]), geom


def boxes_for_geom(grid, geom, cell_w, img_w):
    """Mask boxes for a grid at an ALREADY-LOCATED geometry.

    The film has ~50 frames of the SAME window, so its geometry is located ONCE
    (from the still) and reused: re-locating it per frame would be ~50x the work
    to re-derive the same answer, and — worse — a frame caught mid-repaint could
    locate a different one, putting the mask in a different place in different
    frames of one film.
    """
    y0, cell_h = geom
    out = []
    for row, text in sorted(grid.items()):
        for pat in SECRET_PATTERNS:
            for m in re.finditer(pat, text):
                x = int(m.start() * cell_w)
                w = int((m.end() - m.start()) * cell_w)
                if x + w > img_w:
                    w = max(1, img_w - x)
                out.append(("%d,%d,%d,%d" % (x, int(round(y0 + row * cell_h)), w, int(round(cell_h))),
                            MASK * (m.end() - m.start())))
    return out



def expand_pairs(pairs, pad=MASK_PAD):
    """Grow every (box_spec, text) rectangle by `pad` on each side.

    The returned pairs are BOTH what gets painted and what gets recorded — one
    geometry, which is what makes the containment check meaningful.
    """
    out = []
    for spec, text in pairs:
        x, y, w, h = (int(v) for v in spec.split(","))
        out.append(("%d,%d,%d,%d" % (x - pad, y - pad, w + 2 * pad, h + 2 * pad), text))
    return out


def delivered_pairs(pairs, src_w, dst_w, crop_top=0):
    """Re-express mask rectangles in the DELIVERED image's coordinates.

    The mask is applied to the raw grab (the only place the surface's cell grid
    describes), while the delivered twin is that grab CROPPED and SCALED. A record
    kept in source coordinates would be off by exactly the scale factor — and
    wrong in the one way a reader cannot see: it would look like a redaction that
    leaked, because changed pixels would land outside every recorded box. The
    edge is rounded OUTWARD (floor on the left/top, ceil on the right/bottom), so
    the recorded rectangle always covers the painted one rather than nearly
    covering it.
    """
    if not pairs:
        return []
    k = dst_w / float(src_w)
    out = []
    for spec, text in pairs:
        x, y, w, h = (int(v) for v in spec.split(","))
        nx = int(math.floor(x * k))
        nw = int(math.ceil((x + w) * k)) - nx
        sy = y - crop_top
        ny = int(math.floor(sy * k))
        nh = int(math.ceil((sy + h) * k)) - ny
        out.append(("%d,%d,%d,%d" % (nx, ny, nw, nh), text))
    return out


def mask_image(tools, src, dst, boxes, band=""):
    """Run redact.py, or copy when there is nothing to mask.

    `band` handles the one image that has no cell grid: a browser render composes
    its own layout, so a secret there is located by the ROW it sits on rather
    than by cells. The band is recorded like any other box, so the containment
    check covers it too.
    """
    if not boxes and not band:
        shutil.copyfile(src, dst)
        return False
    cmd = [sys.executable, os.path.join(tools, "redact.py"), "--in", src, "--out", dst]
    for box, text in boxes:
        cmd += ["--box", box, "--text", text]
    if band:
        cmd += ["--band", band]
    run(cmd)
    return True


def paint_delivered(tools, path, specs):
    """Paint mask rectangles ON the delivered image, in its own pixels.

    This is the order that makes containment exact. The first attempt masked the
    RAW grab and then cropped and scaled it: the downscale averages ~1.8 source
    pixels per destination pixel, so the mask's footprint spread about a pixel
    past every recorded rectangle (measured: the hero's record said y=225, the
    first changed pixel sat at y=224) and the containment check failed — rightly,
    because a record that does not contain the change is not a record. Painting
    AFTER the resample removes the question: the clean twin is the raw twin with
    exactly these rectangles repainted, so the set of changed pixels IS the set of
    recorded rectangles, and a leak has nowhere to hide.
    """
    if not specs:
        return False
    # PIL picks the encoder from the file EXTENSION, so the temp name must keep
    # the .png suffix (a bare ".masked" makes it refuse to save at all).
    tmp = path[: -len(".png")] + ".masked.png"
    cmd = [sys.executable, os.path.join(tools, "redact.py"), "--in", path, "--out", tmp]
    for spec, text in specs:
        cmd += ["--box", spec, "--text", text]
    run(cmd)
    os.replace(tmp, path)
    return True


def crop_scale(tools, src, dst, cols, rows, top, bottom, width):
    run([sys.executable, os.path.join(tools, "frame_prep.py"), "--in", src, "--out", dst,
         "--cols", str(cols), "--rows", str(rows),
         "--first-row", str(top), "--last-row", str(bottom), "--width", str(width)])


def pick_dump(root, name):
    """The newest `<name>-screen.dump` under a profile's session tree."""
    hits = glob.glob(os.path.join(root, ".gopi", "sessions", "*", name))
    return max(hits, key=os.path.getmtime) if hits else ""


def film_scale(capture_w, cols, min_px_per_char):
    """The ONE scale factor the film is delivered at, chosen to clear a
    LEGIBILITY floor rather than a total-width target.

    A width target is the wrong criterion for a terminal screenshot, and it was
    measured to be wrong: an earlier cut shipped 960px, i.e. 960/120 = 8.00 px
    per character, and the words in it could not be read. Worse, on THIS capture
    the target is not merely unnecessary, it is counterproductive — the source is
    a palette-free dithered grab, so resampling mixes grey into the glyph edges,
    and those mid-tones cost the palette encoder MORE bytes than the pixels
    removed (measured over the same 46 frames: 2284px -> 3,546,990 B,
    2056px -> 6,139,157 B, 1964px -> 5,777,246 B). So: deliver at the capture's
    own resolution whenever it already clears the floor (scale 1.0), and shrink
    only when it does not, by the smallest factor that still clears it.
    """
    native = capture_w / float(cols)
    if native + 1e-9 >= min_px_per_char:
        return 1.0, native
    return min_px_per_char / native, min_px_per_char


def main():
    a = parse_args()
    raw = os.path.join(a.out, "raw")
    clean = os.path.join(a.out, "clean")
    frames_out = os.path.join(a.out, "frames")
    for d in (raw, clean, frames_out):
        os.makedirs(d, exist_ok=True)
    tmp = os.path.join(a.scratch, "asm")
    os.makedirs(tmp, exist_ok=True)

    home = os.environ.get("GOPI_HOME", "")
    chat_dump = pick_dump(home, "chat-screen.dump")
    ui_dump = pick_dump(home, "ui-screen.dump")

    manifest = {
        "generator": "scripts/human-ui/build.sh + tools/human-ui/assemble.py",
        "terminal": {"cols": a.term_cols, "rows": a.term_rows, "kind": a.term_kind,
                     "chosen_by": a.term_why, "profile": a.term_profile,
                     "size_knob": a.term_size},
        "artifacts": {}, "masking": {}, "commands": {},
    }
    # The calibration table travels WITH the manifest: the chosen size knob is a
    # measurement, and a reader must be able to see the measurements it beat.
    if a.term_size_table and os.path.exists(a.term_size_table):
        rows = []
        with open(a.term_size_table, encoding="utf-8") as fh:
            for line in fh:
                line = line.rstrip("\n")
                if line and not line.startswith("#"):
                    parts = line.split("\t")
                    rows.append({"knob": parts[0], "window": parts[1] if len(parts) > 1 else "",
                                 "px_per_char": parts[2] if len(parts) > 2 else ""})
        manifest["terminal"]["size_candidates"] = rows
        manifest["terminal"]["size_floor_px_per_char"] = a.gif_min_px_per_char
    redaction = {}
    masked_total = 0

    manifest["masking"] = {
        "patterns": SECRET_PATTERNS,
        "dump_chat": os.path.basename(chat_dump) or "(none found)",
        "dump_ui": os.path.basename(ui_dump) or "(none found)",
        "rule": "the secret's rectangle is covered with a fill sampled from its own "
                "surroundings, and the stand-in is drawn as len(secret) filled CELLS across "
                "exactly that width — so the mask occupies the cells the secret did, cannot "
                "overshoot them (a typeface glyph could: '█' advances ~1.65x a cell), and does "
                "not draw the eye to itself by being brighter than the interface",
        "note": "each surface is masked from ITS OWN dump: a box measured on one grab's grid "
                "is meaningless on another grab's pixels",
    }

    def record(name, boxes, band, did, note=""):
        """Record the geometry that was PAINTED, in the DELIVERED image's pixels.

        The boxes arrive as the same (spec, text) pairs handed to the painter, so
        the record and the paint cannot drift apart: there is one list, used
        twice.
        """
        if boxes or band:
            redaction[name] = {"boxes": [[int(v) for v in b[0].split(",")] if isinstance(b, tuple)
                                         else [int(v) for v in b] for b in boxes],
                               "band": [int(v) for v in band.split(",")] if band else [],
                               "count": len(boxes) + (1 if band else 0), "note": note}
        else:
            redaction[name] = {"boxes": [], "band": [], "count": 0,
                               "note": note or "no identifying string fell inside this crop"}

    # ── the terminal surfaces ─────────────────────────────────────────────
    for name, src, dump in (("chat", os.path.join(a.scratch, "chat-still.png"), chat_dump),
                            ("ui", os.path.join(a.scratch, "ui-still.png"), ui_dump)):
        if not os.path.exists(src):
            sys.exit("assemble: missing capture %s" % src)
        w, h = size_of(src)
        boxes, geom = dump_boxes(dump, a.term_cols, a.term_rows, Image.open(src).convert("RGB"),
                                 SECRET_PATTERNS)
        if geom is None and grid_rows(dump):
            sys.exit("assemble: %s: the dump's cell grid could NOT be located in %s — refusing to "
                     "place mask boxes by an unverified geometry (that is exactly how a secret "
                     "leaks into a delivered picture)" % (name, src))
        raw_path = os.path.join(raw, "%s.png" % name)
        clean_path = os.path.join(clean, "%s.png" % name)
        crop_scale(a.tools, src, raw_path, a.term_cols, a.term_rows, 0, a.term_rows - 1, 0)
        sz = list(size_of(raw_path))
        shutil.copyfile(raw_path, clean_path)
        specs = delivered_pairs(expand_pairs(boxes), w, sz[0])
        did = paint_delivered(a.tools, clean_path, specs)
        manifest["artifacts"]["%s.png" % name] = {
            "size": sz, "source": os.path.basename(src), "masked": did, "mask_boxes": len(boxes)}
        record("%s.png" % name, specs, "", did)
        masked_total += len(boxes)

    # ── the browser render ────────────────────────────────────────────────
    web_src = os.path.join(a.scratch, "web-still.png")
    if not os.path.exists(web_src):
        sys.exit("assemble: missing %s (the `gopi shot` leg did not run)" % web_src)
    web_masked = os.path.join(tmp, "web-masked.png")
    band = a.web_banner_band
    did = mask_image(a.tools, web_src, web_masked, [], band=band)
    shutil.copyfile(web_src, os.path.join(raw, "web.png"))
    shutil.copyfile(web_masked if did else web_src, os.path.join(clean, "web.png"))
    wsz = list(size_of(web_src))
    manifest["artifacts"]["web.png"] = {
        "size": wsz, "source": "gopi shot --self (headless Chromium, its own CDP client)",
        "masked": did, "mask_band": band, "wide_enough_1200": wsz[0] >= 1200}
    record("web.png", [], band, did,
           note="a browser render composes its own layout, so the run's own banner line (which "
                "carries session= and addr=) is masked as a full-width band rather than by cell")
    masked_total += 1 if band else 0

    # ── hero ──────────────────────────────────────────────────────────────
    hero_src = os.path.join(a.scratch, "hero-a.png")
    if not os.path.exists(hero_src):
        hero_src = os.path.join(a.scratch, "hero-b.png")
    if not os.path.exists(hero_src):
        sys.exit("assemble: neither hero-a.png nor hero-b.png exists — no conversation was captured")
    hw, hh = size_of(hero_src)
    hero_boxes, hero_geom = dump_boxes(chat_dump, a.term_cols, a.term_rows,
                                       Image.open(hero_src).convert("RGB"), SECRET_PATTERNS)
    if hero_geom is None and grid_rows(chat_dump):
        sys.exit("assemble: hero: the dump's cell grid could NOT be located in %s — refusing to "
                 "place mask boxes by an unverified geometry" % hero_src)
    hero_raw = os.path.join(raw, "hero.png")
    hero_clean = os.path.join(clean, "hero.png")
    crop_scale(a.tools, hero_src, hero_raw, a.term_cols, a.term_rows, 0, a.term_rows - 1, a.hero_width)
    hsz = list(size_of(hero_raw))
    shutil.copyfile(hero_raw, hero_clean)
    hero_specs = delivered_pairs(expand_pairs(hero_boxes), hw, hsz[0])
    hero_did = paint_delivered(a.tools, hero_clean, hero_specs)
    manifest["artifacts"]["hero.png"] = {"size": hsz, "source": os.path.basename(hero_src),
                                         "masked": hero_did, "mask_boxes": len(hero_boxes),
                                         "wide_enough_1200": hsz[0] >= 1200}
    record("hero.png", hero_specs, "", hero_did)
    masked_total += len(hero_boxes)

    # ── the film ──────────────────────────────────────────────────────────
    typed = png_frames(os.path.join(a.scratch, "shots", "type"))
    stream = png_frames(os.path.join(a.scratch, "shots", "stream"))
    # Pool every sampler's stream in time order: the stop-on-stability sampler
    # (no prefix) and the cadence sampler (`h`, the second turn) overlap in
    # content, so both are collected and the reduction to distinct states happens
    # afterwards — pooling first is what keeps a short stream above the floor.
    stream = sorted(stream, key=lambda p: (os.path.basename(p)[0] == "h", os.path.basename(p)))
    typed_kept, typed_dropped = dedupe(typed, label="typing")
    stream_kept, stream_dropped = dedupe(stream, label="streaming")
    sequence = typed_kept + stream_kept
    if len(sequence) < 12:
        sys.exit("assemble: only %d DISTINCT frames after dedupe, the film needs >= 12 "
                 "(typing=%d/%d, streaming=%d/%d) — capture more, do not pad the film"
                 % (len(sequence), len(typed_kept), len(typed), len(stream_kept), len(stream)))
    manifest["film"] = {"typing_frames": len(typed_kept), "typing_captured": len(typed),
                        "streaming_frames": len(stream_kept), "streaming_captured": len(stream),
                        "dropped_identical": typed_dropped + stream_dropped}

    # The film is delivered CLEAN, like the stills: a watermark/status line on a
    # live frame can carry the run's own id, and a showcase that leaks one fact
    # while its neighboring PNG withholds the same fact is worse than either
    # choice made consistently.
    frame_info = []
    masked_frames = 0
    # The film's ONE delivered width (and its height, which follows the same
    # factor): decided once, from the first frame's own size, and asserted equal
    # for every frame below — a film whose frames disagree on size is refused by
    # the encoder anyway, but naming it here says WHY.
    cap_w, cap_h = size_of(sequence[0])
    film_k, _native_ppc = film_scale(cap_w, a.term_cols, a.gif_min_px_per_char)
    gif_width = int(round(cap_w * film_k))
    # The film's mask geometry is located ONCE, from the delivered still of the
    # same window (see boxes_for_geom): one location for every frame, so a frame
    # caught mid-repaint cannot shift the mask.
    film_grid = grid_rows(chat_dump)
    film_geom = locate_grid(Image.open(os.path.join(a.scratch, "chat-still.png")).convert("RGB"),
                            film_grid, a.term_cols, a.term_rows) if film_grid else None
    if film_grid and film_geom is None:
        sys.exit("assemble: the film's dump grid could not be located in chat-still.png — "
                 "refusing to place mask boxes by an unverified geometry")
    for i, src in enumerate(sequence):
        w, h = size_of(src)
        boxes = boxes_for_geom(film_grid, film_geom, w / float(a.term_cols), w) if film_geom else []
        dst = os.path.join(frames_out, "f%03d.png" % (i + 1))
        crop_scale(a.tools, src, dst, a.term_cols, a.term_rows, 0, a.term_rows - 1, gif_width)
        fw = size_of(dst)[0]
        if paint_delivered(a.tools, dst, delivered_pairs(expand_pairs(boxes), w, fw)):
            masked_frames += 1
        frame_info.append({"frame": os.path.basename(dst), "origin": os.path.basename(src),
                           "phase": "typing" if i < len(typed_kept) else "answer",
                           "mask_boxes": len(boxes)})

    sizes = {size_of(os.path.join(frames_out, f["frame"])) for f in frame_info}
    if len(sizes) != 1:
        sys.exit("assemble: frames disagree on size %s — the capture geometry moved mid-film" % sorted(sizes))

    gif_path = os.path.join(a.out, "chat-loop.gif")
    gif_cmd = [a.bin, "gif", "--frames", ",".join(os.path.join(frames_out, f["frame"]) for f in frame_info),
               "--out", gif_path, "--delay", str(a.gif_delay)]
    run(gif_cmd)
    # The recorded command is SANITIZED: it is delivered prose, so it must carry
    # no absolute path from this host. Both the frames directory and the output
    # directory are replaced by placeholders — recording only the first left the
    # output's path in the manifest, which is exactly the leak the clean twin is
    # built to avoid.
    recorded = " ".join(gif_cmd)
    recorded = recorded.replace(frames_out, "<frames>").replace(a.out, "<out>").replace(a.bin, "gopi")
    manifest["commands"]["gif"] = recorded
    frame_w, frame_h = list(sizes)[0]
    # The LEGIBILITY gate, measured on the DELIVERED bytes: the film's own width
    # divided by the terminal's column count is the pixels a single character
    # occupies. The floor is not decoration — a delivered file that cannot be read
    # is not a demonstration, and this check is what makes the criterion the
    # pixels-per-character figure rather than "the picture is >= N px wide", which
    # tests the file's size and says nothing about the glyphs in it.
    px_per_char = frame_w / float(a.term_cols)
    deliver_w = int(round(cap_w * film_k))
    manifest["artifacts"]["chat-loop.gif"] = {
        "frames": len(frame_info), "frame_size": [frame_w, frame_h],
        "width": frame_w, "height": frame_h,
        "px_per_char": round(px_per_char, 3),
        "min_px_per_char": a.gif_min_px_per_char,
        "legible": px_per_char + 1e-9 >= a.gif_min_px_per_char,
        "frame_scale": round(film_k, 4),
        "px_per_char_if_not_scaled": round(cap_w / float(a.term_cols), 3),
        "delay_centiseconds": a.gif_delay,
        "typing_frames": len(typed_kept), "streaming_frames": len(stream_kept),
        "frames_masked": masked_frames, "frames_detail": frame_info,
        "bytes": os.path.getsize(gif_path)}
    manifest["masking"]["masks_applied"] = masked_total

    # hero: reported with the same figure, so the two delivery paths of the SAME
    # terminal geometry cannot drift apart unnoticed again.
    hpx = hsz[0] / float(a.term_cols)
    manifest["artifacts"]["hero.png"]["px_per_char"] = round(hpx, 3)
    manifest["artifacts"]["hero.png"]["min_px_per_char"] = a.gif_min_px_per_char
    manifest["artifacts"]["hero.png"]["legible"] = hpx + 1e-9 >= a.gif_min_px_per_char
    manifest["artifacts"]["chat.png"]["px_per_char"] = round(sz[0] / float(a.term_cols), 3)
    manifest["artifacts"]["chat.png"]["legible"] = sz[0] / float(a.term_cols) + 1e-9 >= a.gif_min_px_per_char

    # The TOP LEVEL carries the published versions: the clean stills, so a reader
    # who opens docs/archive/demo/human/hero.png gets the artifact as it is meant to be
    # shown, while raw/ keeps the untouched capture for comparison. Delivering only
    # the raw/clean pair would make every consumer choose, and the directory a
    # README links to would not exist.
    published = []
    for name in ("hero", "chat", "ui", "web"):
        src = os.path.join(clean, "%s.png" % name)
        if os.path.exists(src):
            shutil.copyfile(src, os.path.join(a.out, "%s.png" % name))
            published.append("%s.png" % name)
    manifest["published_top_level"] = published
    manifest["top_level_note"] = ("the top-level stills are the CLEAN versions; raw/ holds the "
                                  "same captures unmasked, and clean/ is the same bytes by copy")

    with open(os.path.join(a.out, "redaction.json"), "w", encoding="utf-8") as fh:
        json.dump(redaction, fh, ensure_ascii=False, indent=2, sort_keys=True)
    with open(os.path.join(a.out, "manifest.json"), "w", encoding="utf-8") as fh:
        json.dump(manifest, fh, ensure_ascii=False, indent=2, sort_keys=True)
    print(json.dumps({k: v for k, v in manifest["artifacts"].items() if k != "chat-loop.gif"},
                     ensure_ascii=False, indent=2, sort_keys=True))
    print(json.dumps({k: v for k, v in manifest["artifacts"]["chat-loop.gif"].items() if k != "frames_detail"},
                     ensure_ascii=False, indent=2, sort_keys=True))
    if not manifest["artifacts"]["chat-loop.gif"]["legible"]:
        sys.exit("assemble: the delivered film gives %.2f px per character, below the %.2f floor "
                 "— raise the capture geometry or the scale, do not deliver an unreadable film"
                 % (px_per_char, a.gif_min_px_per_char))

if __name__ == "__main__":
    main()
