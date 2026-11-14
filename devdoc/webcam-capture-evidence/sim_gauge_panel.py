#!/usr/bin/env python3
"""SIMULATION (no hardware): the NON-COMPUTER instrument-panel extension.

A legacy panel is a 320x240 LCD/CRT panel with an analog gauge (needle) and a
3-digit 7-segment readout -- an order of magnitude coarser than the 1920x1080
GUI of the main experiments, but physically SMALLER, so the hard question is not
resolution per screen pixel but whether the DETERMINISTIC primitives still work:

  (1) needle-angle estimation in the photo  (the "direction" signal)
  (2) 7-segment digit decode by segment intensity, not OCR  (the "readout")
  (3) the ROI mean|diff| arrival detector on a needle increment

Optical path identical to experiment #1 (homography + lens blur + sensor noise +
JPEG q75), at the recommended-class standoff, and at a 2x-further standoff.
"""
import io, json, math, os
import cv2, numpy as np
from PIL import Image, ImageDraw, ImageFont

OUT = os.path.dirname(os.path.abspath(__file__))
FONT = "/usr/share/fonts/truetype/dejavu/DejaVuSansMono-Bold.ttf"
RNG = np.random.default_rng(11)

PANEL_W, PANEL_H = 320, 240          # the legacy panel, in its own pixels
GAUGE_C, GAUGE_R = (95, 120), 78     # gauge centre + needle radius, panel px
SEG_ORIGIN = (200, 60)               # 3-digit 7-seg block, panel px

SEG_MAP = {  # which segments are lit for each digit: a b c d e f g
    0: "abcdef", 1: "bc", 2: "abdeg", 3: "abcdg", 4: "bcfg",
    5: "acdfg", 6: "acdefg", 7: "abc", 8: "abcdefg", 9: "abcdfg",
}


def draw_panel(needle_deg, digits):
    img = Image.new("RGB", (PANEL_W, PANEL_H), (24, 26, 22))
    d = ImageDraw.Draw(img)
    cx, cy = GAUGE_C
    d.ellipse([cx - GAUGE_R, cy - GAUGE_R, cx + GAUGE_R, cy + GAUGE_R],
              outline=(210, 205, 190), width=2)
    # 9 tick marks over 240 deg, label-free (a legacy panel has no font)
    for i in range(9):
        a = math.radians(150 + i * 30)
        d.line([cx + (GAUGE_R - 14) * math.cos(a), cy - (GAUGE_R - 14) * math.sin(a),
                cx + (GAUGE_R - 2) * math.cos(a), cy - (GAUGE_R - 2) * math.sin(a)],
               fill=(200, 196, 180), width=2)
    a = math.radians(needle_deg)
    d.line([cx, cy, cx + (GAUGE_R - 6) * math.cos(a), cy - (GAUGE_R - 6) * math.sin(a)],
           fill=(230, 70, 50), width=4)
    d.ellipse([cx - 5, cy - 5, cx + 5, cy + 5], fill=(180, 180, 170))
    # 3-digit 7-segment red LED-style readout
    x0, y0 = SEG_ORIGIN
    for k, ch in enumerate("%03d" % digits):
        on = SEG_MAP[int(ch)]
        bx = x0 + k * 34
        segs = {"a": [(3, 0), (25, 0), (25, 5), (3, 5)], "b": [(25, 2), (31, 2), (31, 28), (25, 28)],
                "c": [(25, 30), (31, 30), (31, 56), (25, 56)], "d": [(3, 53), (25, 53), (25, 58), (3, 58)],
                "e": [(0, 30), (6, 30), (6, 56), (0, 56)], "f": [(0, 2), (6, 2), (6, 28), (0, 28)],
                "g": [(3, 27), (25, 27), (25, 32), (3, 32)]}
        for s, poly in segs.items():
            fill = (255, 40, 30) if s in on else (52, 26, 24)
            d.polygon([(bx + px, y0 + py) for px, py in poly], fill=fill)
    im = np.array(img)
    im = cv2.GaussianBlur(im, (0, 0), 0.6)                 # panel's own softness
    return cv2.cvtColor(im, cv2.COLOR_RGB2BGR)


def photograph(panel_bgr, ratio):
    """ratio = photo px per panel px. Screen quad placed like exp #1."""
    ph, pw = int(round(PANEL_H * ratio)), int(round(PANEL_W * ratio))
    im = cv2.resize(panel_bgr, (pw, ph), interpolation=cv2.INTER_AREA)
    W, H = 2304, 1296
    x0, y0 = 520, 300
    quad = np.float32([[x0, y0], [x0 + pw - 1, y0 + 6], [x0 + pw + 14, y0 + ph - 1], [x0 - 8, y0 + ph - 9]])
    src = np.float32([[0, 0], [pw - 1, 0], [pw - 1, ph - 1], [0, ph - 1]])
    canvas = cv2.warpPerspective(im, cv2.getPerspectiveTransform(src, quad), (W, H),
                                 flags=cv2.INTER_LINEAR, borderMode=cv2.BORDER_CONSTANT,
                                 borderValue=(30, 32, 34))
    canvas = cv2.GaussianBlur(canvas, (0, 0), 1.1)
    canvas = np.clip(canvas.astype(np.float32) + RNG.normal(0, 2.0, canvas.shape), 0, 255).astype(np.uint8)
    return cv2.imdecode(cv2.imencode(".jpg", canvas, [cv2.IMWRITE_JPEG_QUALITY, 75])[1], cv2.IMREAD_COLOR), quad


def needle_angle(photo, quad, panel_ratio_px):
    """DETERMINISTIC: rectify the panel (homography from the detected quad),
    then find the red needle pixels and fit their principal axis centre->tip."""
    W, H = 4 * PANEL_W, 4 * PANEL_H
    M = cv2.getPerspectiveTransform(quad, np.float32([[0, 0], [W - 1, 0], [W - 1, H - 1], [0, H - 1]]))
    flat = cv2.warpPerspective(photo, M, (W, H), flags=cv2.INTER_LINEAR)
    hsv = cv2.cvtColor(flat, cv2.COLOR_BGR2HSV)
    m = cv2.inRange(hsv, (0, 120, 120), (12, 255, 255)) | cv2.inRange(hsv, (168, 120, 120), (180, 255, 255))
    cx, cy = GAUGE_C[0] * 4, GAUGE_C[1] * 4
    # restrict to the gauge disc: the 7-seg readout is ALSO red, and an
    # unrestricted red mask makes the needle centroid meaningless (first run:
    # 33688 red px for a 78 px needle -> median angle error 47.7 deg).
    yy, xx = np.mgrid[0:m.shape[0], 0:m.shape[1]]
    disc = (xx - cx) ** 2 + (yy - cy) ** 2 < (GAUGE_R * 4) ** 2
    m = np.where(disc, m, 0)
    ys, xs = np.nonzero(m)
    if len(xs) < 30:
        return None, 0
    dx, dy = xs.mean() - cx, cy - ys.mean()          # image y is down; angle up-positive
    return math.degrees(math.atan2(dy, dx)), len(xs)


def seg_readout(photo, quad, panel_ratio_px):
    """DETERMINISTIC: rectify, then classify each of the 3x7 segments by the mean
    red-channel intensity inside that segment's polygon (no OCR, no model)."""
    W, H = 4 * PANEL_W, 4 * PANEL_H
    M = cv2.getPerspectiveTransform(quad, np.float32([[0, 0], [W - 1, 0], [W - 1, H - 1], [0, H - 1]]))
    flat = cv2.warpPerspective(photo, M, (W, H), flags=cv2.INTER_LINEAR)
    r = flat[:, :, 2].astype(float)
    x0, y0 = SEG_ORIGIN
    segs = {"a": [(3, 0), (25, 0), (25, 5), (3, 5)], "b": [(25, 2), (31, 2), (31, 28), (25, 28)],
            "c": [(25, 30), (31, 30), (31, 56), (25, 56)], "d": [(3, 53), (25, 53), (25, 58), (3, 58)],
            "e": [(0, 30), (6, 30), (6, 56), (0, 56)], "f": [(0, 2), (6, 2), (6, 28), (0, 28)],
            "g": [(3, 27), (25, 27), (25, 32), (3, 32)]}
    digits, margins = [], []
    for k in range(3):
        bx = x0 + k * 34
        vals = {}
        for s, poly in segs.items():
            mask = np.zeros(r.shape, np.uint8)
            cv2.fillPoly(mask, [np.int32([(4 * (bx + px), 4 * (y0 + py)) for px, py in poly])], 1)
            vals[s] = r[mask == 1].mean()
        lo = min(vals.values()); hi = max(vals.values())
        on = {s for s, v in vals.items() if v > lo + 0.5 * (hi - lo)}
        best = max(SEG_MAP, key=lambda d: len(on & set(SEG_MAP[d])) - len(set(SEG_MAP[d]) - on))
        digits.append(best)
        margins.append(round((hi - lo) / max(1.0, hi), 3))
    return "".join(str(d) for d in digits), margins


def roi_diff(a, b, box):
    x0, y0, x1, y1 = box
    ga = cv2.cvtColor(a[y0:y1, x0:x1], cv2.COLOR_BGR2GRAY).astype(float)
    gb = cv2.cvtColor(b[y0:y1, x0:x1], cv2.COLOR_BGR2GRAY).astype(float)
    return float(np.abs(ga - gb).mean())


def main():
    lines = []
    P = lines.append
    P("=" * 78)
    P("SIMULATION (no hardware) — INSTRUMENT-PANEL EXTENSION OF THE CAPTURE DESIGN")
    P("  panel = 320x240 px (legacy LCD/CRT), gauge (78 px radius) + 3-digit 7-seg")
    P("  photo = 2304x1296 with the exp-#1 optical path (keystone + blur 1.1 +")
    P("          noise 2/255 + JPEG q75).  NOT a hardware test.")
    P("=" * 78)
    for label, ratio in (("50 cm-class  (panel->photo 0.626)", 0.626),
                         ("2x further   (panel->photo 0.313)", 0.313)):
        P("")
        P("--- %s : 1 panel px = %.3f photo px ---" % (label, ratio))
        p = photograph(draw_panel(60.0, 137), ratio)
        photo, quad = p
        ang, npx = needle_angle(photo, quad, ratio)
        dig, marg = seg_readout(photo, quad, ratio)
        P("   panel occupies %.0f x %.0f photo px; needle red px found = %d"
          % (ratio * PANEL_W, ratio * PANEL_H, npx))
        P("   needle angle: true 60.0 deg -> measured %s deg  (err %s deg)"
          % ("%.1f" % ang if ang is not None else "n/a",
             "%.1f" % abs(ang - 60.0) if ang is not None else "n/a"))
        P("   gauge full scale = 240 deg -> 1 deg = %.3f photo px of needle tip travel"
          % (math.radians(1.0) * (GAUGE_R - 6) * ratio))
        P("   7-seg decode: true 137 -> read %s   (segment contrast margin %s)"
          % (dig, marg))
        # arrival detector: does a real needle increment clear the null floor?
        a, q = photograph(draw_panel(60.0, 137), ratio)
        b, _ = photograph(draw_panel(60.0, 137), ratio)          # same state, new draw
        c, _ = photograph(draw_panel(90.0, 137), ratio)          # needle moved 30 deg
        d, _ = photograph(draw_panel(60.0, 142), ratio)          # readout changed 5
        # ROI boxes come from the panel coords through the SAME quad (the first
        # run hardcoded them outside the panel and measured noise: 0.29-0.37)
        def roi_box(px0, py0, px1, py1):
            pts = np.float32([[px0, py0], [px1, py0], [px1, py1], [px0, py1]]
                             ).reshape(-1, 1, 2) * ratio
            src = np.float32([[0, 0], [PANEL_W * ratio - 1, 0],
                              [PANEL_W * ratio - 1, PANEL_H * ratio - 1], [0, PANEL_H * ratio - 1]])
            # the photo was built from the panel rect at 1:1 panel px; map panel->photo
            M2 = cv2.getPerspectiveTransform(
                np.float32([[0, 0], [PANEL_W - 1, 0], [PANEL_W - 1, PANEL_H - 1], [0, PANEL_H - 1]]),
                np.float32([[520, 300], [520 + PANEL_W * ratio - 1, 306],
                            [520 + PANEL_W * ratio + 14, 300 + PANEL_H * ratio - 1],
                            [512, 291 + PANEL_H * ratio]]))
            pp = cv2.perspectiveTransform(np.float32([[px0, py0], [px1, py0], [px1, py1], [px0, py1]]
                                                    ).reshape(-1, 1, 2), M2).reshape(4, 2)
            X0, Y0 = pp.min(0); X1, Y1 = pp.max(0)
            return (int(X0), int(Y0), int(X1), int(Y1))
        gb_ = roi_box(10, 35, 180, 210)      # the gauge half of the panel
        rb = roi_box(190, 50, 310, 125)      # the 3-digit readout
        P("   arrival detector (mean|grey diff| over the rectified ROI):")
        P("      null (same state, 2 independent draws)  gauge ROI %.2f   readout ROI %.2f"
          % (roi_diff(a, b, gb_), roi_diff(a, b, rb)))
        P("      needle moved 30 deg                     gauge ROI %.2f   readout ROI %.2f"
          % (roi_diff(a, c, gb_), roi_diff(a, c, rb)))
        P("      readout 137 -> 142                      gauge ROI %.2f   readout ROI %.2f"
          % (roi_diff(a, d, gb_), roi_diff(a, d, rb)))
    out = "\n".join(lines) + "\n"
    open(os.path.join(OUT, "gauge-panel-raw-output.txt"), "w").write(out)
    print(out)


if __name__ == "__main__":
    main()
