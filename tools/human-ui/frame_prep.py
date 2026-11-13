#!/usr/bin/env python3
"""frame_prep.py — turn a raw screen grab into a GIF frame: crop to the CELL
BAND that matters, then scale to the target width.

WHY THE CELL GRID DRIVES THE CROP. A chat capture is 2094x814 because the
terminal is 110x30 CELLS; the height that belongs to row R is not a magic
number, it is (R / rows) of the picture. Deriving the band from the grid the
screen itself reported (`gopi ui_state` reads the same cols/rows) means the
crop follows a geometry change instead of silently cutting the wrong rows when
someone resizes the terminal.

  cell_w = width  / cols      cell_h = height / rows
  band   = rows [first..last]  ->  y in [first*cell_h, (last+1)*cell_h)

USAGE
  frame_prep.py --in raw.png --out frame.png --cols 110 --rows 30 \
                --first-row 8 --last-row 29 --width 1200
"""
import argparse
import sys

try:
    from PIL import Image
except ImportError:  # pragma: no cover
    sys.exit("frame_prep: python3-PIL missing")


def main():
    p = argparse.ArgumentParser(add_help=True)
    p.add_argument("--in", dest="inp", required=True)
    p.add_argument("--out", dest="outp", required=True)
    p.add_argument("--cols", type=int, required=True, help="terminal columns (from the dump header)")
    p.add_argument("--rows", type=int, required=True, help="terminal rows (from the dump header)")
    p.add_argument("--first-row", type=int, required=True)
    p.add_argument("--last-row", type=int, required=True)
    p.add_argument("--width", type=int, default=0, help="target width; 0 keeps the crop's own width")
    args = p.parse_args()

    im = Image.open(args.inp).convert("RGB")
    w, h = im.size
    if args.cols <= 0 or args.rows <= 0:
        sys.exit("frame_prep: cols/rows must be positive")
    if not (0 <= args.first_row <= args.last_row < args.rows):
        sys.exit(f"frame_prep: row band {args.first_row}..{args.last_row} outside 0..{args.rows - 1}")
    cell_h = h / args.rows
    top = int(round(args.first_row * cell_h))
    bottom = int(round((args.last_row + 1) * cell_h))
    crop = im.crop((0, top, w, bottom))
    if args.width > 0 and args.width != crop.size[0]:
        ratio = args.width / crop.size[0]
        crop = crop.resize((args.width, max(1, int(round(crop.size[1] * ratio)))), Image.LANCZOS)
    crop.save(args.outp)
    print("%s -> %s  %dx%d (cell %.2fx%.2f, rows %d..%d)" %
          (args.inp.rsplit("/", 1)[-1], args.outp.rsplit("/", 1)[-1],
           crop.size[0], crop.size[1], w / args.cols, cell_h, args.first_row, args.last_row))


if __name__ == "__main__":
    main()
