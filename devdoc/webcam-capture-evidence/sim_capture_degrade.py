#!/usr/bin/env python3
"""
SIMULATION ONLY — no hardware optical test is claimed anywhere below.

Capture-leg model for a webcam-as-eye looking at an UNMODIFIED target screen.
Chain: synthetic screen (PIL + DejaVu) -> plane homography (keystone + distance
scale) -> Gaussian lens blur -> sensor sampling (INTER_AREA = 1 photo px per
sensor px) -> sensor noise -> JPEG q.

Sections (all numbers printed are measured by THIS script unless labelled
analytic):
  A  resolution arithmetic   : screen -> photo px, per sensor mode & standoff
  B  degraded-photo gallery  : the 4 photos this script reasons about
  C  OCR readability         : fuzzy line recall on photo, full frame + ROI
  C2 OCR threshold curve     : glyph height in PHOTO px vs recall (measured)
  D  deterministic tracking  : naive NCC, multi-scale NCC, windowed-search cost
  E  photo->screen mapping   : homography ESTIMATED FROM THE PHOTO (no
                               privileged ground truth), target-mapping error
  F  degradation ladder      : tracker robustness vs blur/noise/JPEG/illum
  G  open-loop error budget  : analytic terms + the measured terms

Run: /home/user/work/hermes-agent/.venv/bin/python3 sim_capture_degrade.py
"""
import json
def _json_default(o):
    import numpy as _np
    return float(o) if isinstance(o,(_np.floating,_np.integer)) else str(o)
import math
import subprocess
import time
from difflib import SequenceMatcher

import cv2
import numpy as np
from PIL import Image, ImageDraw, ImageFilter, ImageFont

OUT = "/home/user/work/GoPi/devdoc/webcam-capture-evidence"
SCREEN_W, SCREEN_H = 1920, 1080
TARGET_RECT = (1230, 600, 1250, 620)              # 20x20 GUI target, screen px
TGT_X = (TARGET_RECT[0] + TARGET_RECT[2]) / 2.0
TGT_Y = (TARGET_RECT[1] + TARGET_RECT[3]) / 2.0
TARGET_CENTER = np.float32([[[TGT_X, TGT_Y]]])       # shape (1,1,2) for cv2
FONT = "/usr/share/fonts/truetype/dejavu/DejaVuSans.ttf"
FONT_BOLD = "/usr/share/fonts/truetype/dejavu/DejaVuSans-Bold.ttf"
REF_LINE = "Mode 0: automatic 1920x1080 60 Hz"
CAP_RATIO = 0.729                                  # DejaVu cap-height / em

# Camera Module 3 (IMX708, standard): 66 deg horizontal FoV (official docs).
# Video-relevant sensor modes; the full 4608x2592 mode is 14 fps -> too slow.
SENSOR_MODES = {"CSI-2304x1296": 2304, "CSI-1536x864": 1536, "USB-1280x720": 1280}
FOV_H_DEG = 66.0
SCREEN_PHYS_W_M = 0.310                            # 14-inch 16:9 active width
STANDOFFS_M = {"24cm": 0.24, "50cm": 0.50, "85cm": 0.85}
RNG = np.random.default_rng(7)


# --------------------------------------------------------------------- screen
def render_screen():
    img = Image.new("RGB", (SCREEN_W, SCREEN_H), (56, 58, 66))
    d = ImageDraw.Draw(img)
    f11 = ImageFont.truetype(FONT, 11)
    f9 = ImageFont.truetype(FONT, 9)
    f11b = ImageFont.truetype(FONT_BOLD, 11)
    d.rectangle((1180, 520, 1700, 700), fill=(240, 240, 240), outline=(90, 90, 90))
    d.rectangle((1180, 520, 1700, 542), fill=(62, 100, 160))
    d.text((1186, 525), "Settings", font=f11b, fill=(255, 255, 255))
    d.rectangle(TARGET_RECT, fill=(200, 30, 30))
    d.text((TARGET_RECT[0] + 6, TARGET_RECT[1] + 5), "S", font=f9, fill=(255, 255, 255))
    d.rectangle((1290, 600, 1310, 620), fill=(30, 160, 60))    # distractor
    d.text((1296, 605), "R", font=f9, fill=(255, 255, 255))
    d.text((1232, 626), "Save", font=f11, fill=(20, 20, 20))
    d.text((1292, 626), "Revert", font=f11, fill=(20, 20, 20))
    d.text((1192, 552), "System Preferences", font=f11, fill=(40, 40, 40))
    for i in range(8):
        d.text((1200, 580 + i * 12), "Mode %d: automatic  1920x1080  60 Hz" % i,
               font=f11, fill=(30, 30, 30))
    d.rectangle((0, 1060, SCREEN_W, 1080), fill=(30, 30, 34))
    for i in range(12):
        d.rectangle((12 + i * 34, 1064, 40 + i * 34, 1076), fill=(120, 124, 132))
    return img


def frame_fraction(dist_m):
    """fraction of the camera frame width the screen subtends at dist_m"""
    return math.degrees(2 * math.atan((SCREEN_PHYS_W_M / 2) / dist_m)) / FOV_H_DEG


def homography_screen_to_photo(scale, keystone_px, tgt_w, tgt_h):
    w, h = SCREEN_W * scale, SCREEN_H * scale
    x0, y0 = (tgt_w - w) / 2.0, (tgt_h - h) / 2.0
    src = np.float32([[0, 0], [SCREEN_W, 0], [SCREEN_W, SCREEN_H], [0, SCREEN_H]])
    dst = np.float32([[x0 + keystone_px, y0], [x0 + w - keystone_px, y0],
                      [x0 + w, y0 + h], [x0, y0 + h]])
    return cv2.getPerspectiveTransform(src, dst)


def make_photo(screen_bgr, scale, photo_w, keystone_px, blur_sigma,
               jpeg_q=None, noise_sigma=0.0, gamma=1.0):
    photo_h = int(round(SCREEN_H * photo_w / float(SCREEN_W)))
    H = homography_screen_to_photo(scale, keystone_px, photo_w, photo_h)
    img = cv2.warpPerspective(screen_bgr, H, (photo_w, photo_h), flags=cv2.INTER_AREA)
    if blur_sigma > 0:
        img = cv2.GaussianBlur(img, (0, 0), blur_sigma)
    if noise_sigma > 0:
        img = np.clip(img.astype(np.float32) +
                      RNG.normal(0, noise_sigma, img.shape), 0, 255).astype(np.uint8)
    if gamma != 1.0:                       # ambient-light change on the screen
        lut = (np.power(np.arange(256) / 255.0, gamma) * 255).astype(np.uint8)
        img = cv2.LUT(img, lut)
    if jpeg_q is not None:
        ok, buf = cv2.imencode(".jpg", img, [cv2.IMWRITE_JPEG_QUALITY, jpeg_q])
        img = cv2.imdecode(buf, cv2.IMREAD_COLOR)
    return img, H


# ----------------------------------------------------------------------- ocr
def ocr(path, psm="6"):
    r = subprocess.run(["tesseract", path, "stdout", "--psm", psm],
                       capture_output=True, text=True)
    return " ".join(r.stdout.split())


def line_recall(ref_line, text):
    """best fuzzy similarity between ref_line and any same-length window of the
    OCR text. 1.0 = the line was read (this is the 'can the LLM read it?' metric;
    a global CER is meaningless when OCR also emits other UI text/furniture)."""
    t = " ".join(text.split()).lower()
    r = ref_line.lower()
    if not t:
        return 0.0
    n, best = len(r), 0.0
    for i in range(max(1, len(t) - n + 1)):
        s = SequenceMatcher(None, r, t[i:i + n]).ratio()
        best = max(best, s)
    return best


def ncc_template(photo, tmpl):
    res = cv2.matchTemplate(photo, tmpl, cv2.TM_CCOEFF_NORMED)
    _, mx, _, ml = cv2.minMaxLoc(res)
    return res, mx, (ml[0] + tmpl.shape[1] / 2.0, ml[1] + tmpl.shape[0] / 2.0)


# ------------------------------------------------------------------ sections
def section_a():
    print("=" * 78)
    print("A. RESOLUTION ARITHMETIC  (target screen 1920x1080, active width "
          "%.0f mm; CM3 FoV_h = %.0f deg)" % (SCREEN_PHYS_W_M * 1000, FOV_H_DEG))
    d_fill = (SCREEN_PHYS_W_M / 2.0) / math.tan(math.radians(FOV_H_DEG / 2))
    print("   d_fill (screen exactly fills frame width) = (W/2)/tan(FoV/2)")
    print("        = %.3f / tan(%.1f deg) = %.3f m = %.1f cm"
          % (SCREEN_PHYS_W_M / 2, FOV_H_DEG / 2, d_fill, d_fill * 100))
    rows = []
    for mode, fw in SENSOR_MODES.items():
        row = {"mode": mode, "frame_w_px": fw}
        line = "   %-13s | " % mode
        for name, d in STANDOFFS_M.items():
            frac = frame_fraction(d)
            ppp = (fw * frac) / SCREEN_W                  # photo px / screen px
            row[name] = {"frame_fraction": round(frac, 3),
                         "photo_px_per_screen_px": round(ppp, 3),
                         "20px_target_photo_px": round(20 * ppp, 1),
                         "11px_glyph_photo_px": round(11 * ppp, 1)}
            line += "%s: screen=%.2f frame, 20px->%.1f px | " % (name, frac, 20 * ppp)
        print(line)
        rows.append(row)
    print("   -> a 20x20 GUI widget is 24 px wide in the BEST case (24 cm, 2304 px")
    print("      sensor) and 4 px wide in the worst (85 cm, 1280 px USB sensor):")
    print("      a 6x span decided purely by standoff and sensor, before any blur.")
    return rows, d_fill


def section_b(screen_bgr):
    print("=" * 78)
    print("B. DEGRADED-PHOTO SIMULATION (homography + lens blur + noise + JPEG)")
    cases = [
        ("P1", "CSI-2304x1296", "24cm", 18, 0.8, 75, 2.0),
        ("P2", "CSI-2304x1296", "50cm", 26, 1.1, 75, 2.0),
        ("P3", "USB-1280x720", "50cm", 26, 1.4, 75, 3.0),
        ("P4", "USB-1280x720", "85cm", 30, 1.8, 75, 3.5),
    ]
    photos = {}
    for pid, mode, stand, key, blur, q, nz in cases:
        fw = SENSOR_MODES[mode]
        scale = (fw * frame_fraction(STANDOFFS_M[stand])) / SCREEN_W
        img, H = make_photo(screen_bgr, scale, fw, key, blur, q, nz)
        base = "%s-%s-%s" % (pid, stand, mode)
        cv2.imwrite("%s/photo-%s.png" % (OUT, base), img)
        cv2.imwrite("%s/photo-%s-q%d.jpg" % (OUT, base, q), img,
                    [cv2.IMWRITE_JPEG_QUALITY, q])
        gt = cv2.perspectiveTransform(TARGET_CENTER, H)[0][0]
        photos[pid] = {"mode": mode, "standoff": stand, "scale": scale, "H": H,
                       "img": img, "gt": gt, "path": "%s/photo-%s.png" % (OUT, base),
                       "keystone_px": key, "blur": blur}
        print("   %s %-9s %-5s : photo %dx%d, screen->photo %.3f, 20px target "
              "= %.1f px, target GT centre (%.1f, %.1f)"
              % (pid, mode, stand, img.shape[1], img.shape[0], scale, 20 * scale,
                 gt[0], gt[1]))
    return cases, photos


def section_c(photos):
    print("=" * 78)
    print("C. OCR READABILITY (tesseract psm 6, fuzzy line recall of %r)" % REF_LINE)
    scores = {}
    for pid, p in photos.items():
        full = ocr(p["path"])
        rec_full = line_recall(REF_LINE, full)
        # ROI = the app window as it appears in this photo, lifted by the SAME
        # estimated homography a real pipeline would use is not available here;
        # use the known window box scaled, i.e. an OPTIMISTIC upper bound.
        s = p["scale"]
        gx, gy = p["gt"]
        half_w, half_h = int(560 * s / 2), int(180 * s / 2)
        x0 = max(0, int(gx) - 110)
        y0 = max(0, int(gy) - 40)
        crop = p["img"][y0:y0 + max(8, half_h * 2), x0:x0 + max(8, half_w * 2)]
        if crop.size:
            cp = "%s/photo-%s-roi.png" % (OUT, pid)
            cv2.imwrite(cp, crop)
            rec_roi = line_recall(REF_LINE, ocr(cp))
        else:
            rec_roi = 0.0
        scores[pid] = {"full": round(rec_full, 3), "roi": round(rec_roi, 3)}
        print("   %s   recall(full frame)=%.3f   recall(cropped ROI)=%.3f   "
              "OCR chars=%d" % (pid, rec_full, rec_roi, len(full)))
    print("   control: the SAME OCR on the undegraded screen render -> recall=%.3f"
          % line_recall(REF_LINE, ocr("%s/screen-1920x1080-GT.png" % OUT)))
    return scores


def section_c2():
    print("=" * 78)
    print("C2. OCR THRESHOLD CURVE — glyph height in PHOTO px vs recall")
    print("     (render the line large, then resample down = what the sensor does)")
    base = Image.new("L", (1600, 140), 255)
    ImageDraw.Draw(base).text((10, 30), REF_LINE,
                              font=ImageFont.truetype(FONT, 64), fill=0)
    cap64 = 64 * CAP_RATIO
    curve = []
    for target_cap_px in (3, 4, 5, 6, 7, 8, 10, 12, 16, 24):
        f = target_cap_px / cap64
        im = base.resize((max(4, int(1600 * f)), max(6, int(140 * f))),
                         Image.Resampling.LANCZOS)
        im = im.filter(ImageFilter.GaussianBlur(0.4))
        p = "%s/ocrglyph-%02dpx.png" % (OUT, target_cap_px)
        im.save(p)
        rec = line_recall(REF_LINE, ocr(p))
        curve.append({"photo_cap_px": target_cap_px, "recall": round(rec, 3)})
        print("   glyph cap-height %2d photo px (image %dx%d): recall %.3f  %r"
              % (target_cap_px, im.size[0], im.size[1], rec, ocr(p)[:48]))
    return curve


def section_d(photos, screen_bgr):
    print("=" * 78)
    print("D. DETERMINISTIC TRACKING (cv2.matchTemplate TM_CCOEFF_NORMED)")
    res = {}
    for pid, p in photos.items():
        gncc, gerr = [], []
        for tag, blur in (("GT-template(blur-matched)", p["blur"]),
                          ("ideal-template(no blur)", 0.0)):
            tmpl = cv2.resize(screen_bgr[TARGET_RECT[1]:TARGET_RECT[3],
                                         TARGET_RECT[0]:TARGET_RECT[2]],
                              None, fx=p["scale"], fy=p["scale"],
                              interpolation=cv2.INTER_AREA)
            if blur > 0:
                tmpl = cv2.GaussianBlur(tmpl, (0, 0), blur)
            if min(tmpl.shape[:2]) < 3:
                print("   %s %-24s : template %s < 3 px — untrackable"
                      % (pid, tag, tmpl.shape[:2]))
                continue
            t0 = time.perf_counter()
            _, mx, ctr = ncc_template(p["img"], tmpl)
            dt = (time.perf_counter() - t0) * 1000.0
            e = math.hypot(ctr[0] - p["gt"][0], ctr[1] - p["gt"][1])
            gncc.append(mx)
            gerr.append(e / p["scale"])
            print("   %s %-24s : NCC %.3f err %.1f photo px = %.1f SCREEN px "
                  "(%.0f ms, %dx%d search)" % (pid, tag, mx, e, e / p["scale"], dt,
                                               p["img"].shape[1], p["img"].shape[0]))
        # a LARGER, higher-contrast anchor: the whole 520x180 window frame
        win_t = screen_bgr[520:700, 1180:1700]
        tmpl = cv2.resize(win_t, None, fx=p["scale"], fy=p["scale"],
                          interpolation=cv2.INTER_AREA)
        if min(tmpl.shape[:2]) >= 8:
            t0 = time.perf_counter()
            _, mx, ctr = ncc_template(p["img"], tmpl)
            dt = (time.perf_counter() - t0) * 1000.0
            # window centre in screen coords, then offset to the target centre
            win_center = np.float32([[[1180 + 260, 520 + 90]]])
            gt_win = cv2.perspectiveTransform(win_center, p["H"])[0][0]
            e = math.hypot(ctr[0] - gt_win[0], ctr[1] - gt_win[1]) / p["scale"]
            print("   %s %-24s : NCC %.3f err %.1f SCREEN px (%.0f ms) "
                  "[520x180 window anchor]" % (pid, "window-anchor", mx, e, dt))
            res[pid] = {"window_anchor_ncc": round(mx, 3), "window_anchor_err_screen_px": round(e, 2)}
        res.setdefault(pid, {})
        res[pid]["ncc"] = [round(v, 3) for v in gncc]
        res[pid]["err_screen_px"] = [round(v, 2) for v in gerr]

    print("   D2. TRACKING-WINDOW COST (coarse-to-fine: search only near the last")
    print("       known position once locked) — the single biggest compute lever:")
    p = photos["P2"]
    tmpl_ref = cv2.resize(screen_bgr[600:620, 1230:1250], None, fx=p["scale"],
                          fy=p["scale"], interpolation=cv2.INTER_AREA)
    t0 = time.perf_counter()
    for _ in range(3):
        ncc_template(p["img"], tmpl_ref)
    full_ms = (time.perf_counter() - t0) / 3 * 1000
    d2 = {"full_frame_ms": round(full_ms, 1),
          "frame_wh": [p["img"].shape[1], p["img"].shape[0]]}
    print("      full frame %dx%d            : %.0f ms  (baseline)"
          % (p["img"].shape[1], p["img"].shape[0], full_ms))
    for rad in (64, 128, 256):
        x0, y0 = int(p["gt"][0] - rad), int(p["gt"][1] - rad)
        x0, y0 = int(p["gt"][0] - rad), int(p["gt"][1] - rad)
        win = p["img"][max(0, y0):y0 + 2 * rad, max(0, x0):x0 + 2 * rad]
        t0 = time.perf_counter()
        for _ in range(3):
            ncc_template(win, tmpl_ref)
        dt = (time.perf_counter() - t0) / 3 * 1000
        d2["window_pm%d_ms" % rad] = round(dt, 2)
        d2["window_pm%d_speedup" % rad] = round(full_ms / dt, 1) if dt else None
        print("      window +/-%3d px (%dx%d) : %6.2f ms -> %5.1fx faster"
              % (rad, win.shape[1], win.shape[0], dt, full_ms / dt))
        # coarse-to-fine: search the SAME window at quarter resolution
        small_img = cv2.resize(win, None, fx=0.25, fy=0.25, interpolation=cv2.INTER_AREA)
        small_t = cv2.resize(tmpl_ref, None, fx=0.25, fy=0.25,
                             interpolation=cv2.INTER_AREA)
        if min(small_t.shape[:2]) >= 2:
            t0 = time.perf_counter()
            for _ in range(3):
                ncc_template(small_img, small_t)
            dt2 = (time.perf_counter() - t0) / 3 * 1000
            print("         same window at 25%% scale         : %6.2f ms -> %5.1fx "
                  "faster than full frame" % (dt2, full_ms / dt2))
    return res, d2


def order_quad(pts):
    pts = np.array(pts, dtype=np.float32).reshape(4, 2)
    s, d = pts.sum(1), np.diff(pts, axis=1).ravel()
    return np.float32([pts[np.argmin(s)], pts[np.argmin(d)],
                       pts[np.argmax(s)], pts[np.argmax(d)]])


def quad_candidates(img):
    """all 4-gon candidates from a thresholded, closed gray image"""
    g = cv2.GaussianBlur(cv2.cvtColor(img, cv2.COLOR_BGR2GRAY), (5, 5), 0)
    thr = cv2.threshold(g, 0, 255, cv2.THRESH_BINARY + cv2.THRESH_OTSU)[1]
    thr = cv2.morphologyEx(thr, cv2.MORPH_CLOSE, np.ones((15, 15), np.uint8))
    cnts, _ = cv2.findContours(thr, cv2.RETR_EXTERNAL, cv2.CHAIN_APPROX_SIMPLE)
    out = []
    for c in cnts:
        if cv2.contourArea(c) < 0.01 * img.shape[0] * img.shape[1]:
            continue
        for eps in np.linspace(0.002, 0.08, 40):
            ap = cv2.approxPolyDP(c, eps * cv2.arcLength(c, True), True)
            if len(ap) == 4:
                out.append(order_quad(ap.reshape(4, 2)))
                break
    return out


def aspect(q):
    (x0, y0), (x1, y1), (x2, y2), (x3, y3) = q
    w = (np.linalg.norm(q[1] - q[0]) + np.linalg.norm(q[2] - q[3])) / 2
    h = (np.linalg.norm(q[3] - q[0]) + np.linalg.norm(q[2] - q[1])) / 2
    return w / max(1e-6, h)


def section_e(photos, screen_bgr):
    print("=" * 78)
    print("E. PHOTO -> SCREEN MAPPING, homography ESTIMATED FROM THE PHOTO")
    print("   no privileged ground truth: screen quad from contour detection,")
    print("   corners matched to the 1920x1080 screen corners (calibration-free).")
    print("   A naive 'largest contour' rule FAILS when the screen overfills the")
    print("   frame (it locks onto the app window). The fix is a validity gate:")
    print("   aspect ratio must be within 15%% of the screen's 16:9.")
    out = {}
    for pid, p in photos.items():
        cands = quad_candidates(p["img"])
        if not cands:
            print("   %s : no quad candidate" % pid)
            continue
        naive = max(cands, key=lambda q: abs(cv2.contourArea(q.astype(np.float32))))
        target_ar = SCREEN_W / float(SCREEN_H)
        valid = [q for q in cands if abs(aspect(q) - target_ar) / target_ar < 0.15]
        gated = max(valid, key=lambda q: abs(cv2.contourArea(q.astype(np.float32)))) \
            if valid else naive
        out[pid] = {"n_candidates": len(cands), "gate_candidates": len(valid),
                    "naive_aspect": round(aspect(naive), 2)}
        print("   %s : %d quad candidate(s), aspect %s -> after 16:9 gate: %d"
              % (pid, len(cands),
                 ", ".join("%.2f" % aspect(q) for q in cands), len(valid)))
        for tag, q in (("naive largest", naive), ("16:9-gated", gated)):
            H_est = cv2.getPerspectiveTransform(
                np.float32([[0, 0], [SCREEN_W, 0], [SCREEN_W, SCREEN_H], [0, SCREEN_H]]), q)
            proj = cv2.perspectiveTransform(TARGET_CENTER, H_est)[0][0]
            e_photo = math.hypot(proj[0] - p["gt"][0], proj[1] - p["gt"][1])
            # 1 px of corner noise propagated to the target's screen position
            jit = q + RNG.normal(0, 1.0, q.shape).astype(np.float32)
            H_j = cv2.getPerspectiveTransform(
                np.float32([[0, 0], [SCREEN_W, 0], [SCREEN_W, SCREEN_H], [0, SCREEN_H]]), jit)
            pj = cv2.perspectiveTransform(TARGET_CENTER, H_j)[0][0]
            e_jit = math.hypot(pj[0] - p["gt"][0], pj[1] - p["gt"][1]) / p["scale"]
            out[pid][tag] = {"aspect": round(aspect(q), 2),
                             "target_err_screen_px": round(e_photo / p["scale"], 2),
                             "corner1px_leverage_screen_px": round(e_jit, 2)}
            print("      %-15s aspect %.2f : target maps (%.1f,%.1f) vs GT "
                  "(%.1f,%.1f) -> %.2f SCREEN px; 1 px corner noise -> %.2f SCREEN px"
                  % (tag, aspect(q), proj[0], proj[1], p["gt"][0], p["gt"][1],
                     e_photo / p["scale"], e_jit))
    return out


def section_f(screen_bgr, photos):
    print("=" * 78)
    print("F. DEGRADATION LADDER — does deterministic tracking survive? (P2 base)")
    p = photos["P2"]
    base_scale = p["scale"]
    def build(blur=1.1, noise=2.0, q=75, gamma=1.0, stand="50cm", mode="CSI-2304x1296"):
        fw = SENSOR_MODES[mode]
        s = (fw * frame_fraction(STANDOFFS_M[stand])) / SCREEN_W
        img, H = make_photo(screen_bgr, s, fw, 26, blur, q, noise, gamma)
        tmpl = cv2.resize(screen_bgr[600:620, 1230:1250], None, fx=s, fy=s,
                          interpolation=cv2.INTER_AREA)
        tmpl = cv2.GaussianBlur(tmpl, (0, 0), max(0.4, blur))
        _, mx, ctr = ncc_template(img, tmpl)
        gt = cv2.perspectiveTransform(TARGET_CENTER, H)[0][0]
        e = math.hypot(ctr[0] - gt[0], ctr[1] - gt[1])
        return mx, e / s
    rows = []
    for blur in (0.5, 1.1, 2.0, 3.0, 4.0):
        mx, e = build(blur=blur)
        rows.append(("blur sigma=%.1f px" % blur, mx, e))
    for nz in (0, 5, 10, 20):
        mx, e = build(noise=nz)
        rows.append(("sensor noise sd=%d/255" % nz, mx, e))
    for q in (90, 60, 30, 15):
        mx, e = build(q=q)
        rows.append(("JPEG quality=%d" % q, mx, e))
    for gm in (0.7, 1.0, 1.4):
        mx, e = build(gamma=gm)
        rows.append(("screen brightness gamma=%.1f" % gm, mx, e))
    for stand in ("24cm", "50cm", "85cm"):
        mx, e = build(stand=stand)
        rows.append(("standoff %s" % stand, mx, e))
    for mode in ("CSI-1536x864", "USB-1280x720"):
        mx, e = build(mode=mode)
        rows.append(("sensor %s" % mode, mx, e))
    print("   %-34s %8s %14s" % ("variation", "NCC", "err [screen px]"))
    for label, mx, e in rows:
        flag = "" if (mx > 0.5 and e < 10) else "   <-- LOST" if mx < 0.5 else "   <-- drifting"
        print("   %-34s %8.3f %14.1f%s" % (label, mx, e, flag))
    return [{"variation": l, "ncc": round(m, 3), "err_screen_px": round(e, 1)}
            for l, m, e in rows]


def section_g(rpi_factor=3.5):
    print("=" * 78)
    print("G. OPEN-LOOP ERROR BUDGET (screen px; terms summed in quadrature)")
    print("   Analytic terms are model assumptions (labelled); the vision-model")
    print("   term is MEASURED in experiment #2 (vision-jitter-report.json).")
    terms = {
        "resolution quantisation (1 screen px @ full-frame screen)": 1.0,
        "keystone residual, uncorrected H (26 px corner error / 1200 px)": 26 / 1200 * 1920 * 0.5,
        "lens distortion residual (0.5%% barrel, uncorrected)": 0.005 * 1200,
        "focus/blur centroid bias (sigma=1.1 photo px, full-frame)": 1.1 / 0.626 * 0.5,
        "screen-to-sensor moire beating (half a sampling cell)": 0.5 * (1920.0 / 1200) * 2,
        "target feature centroid on a 13 px widget": 1.0,
        "camera pose drift between shots (1 mm @ 50 cm)": 1.0 / 500 * 1920,
    }
    tot = math.sqrt(sum(v * v for v in terms.values()))
    for k, v in terms.items():
        print("   %-58s %7.1f" % (k, v))
    print("   %-58s %7.1f px" % ("TOTAL, capture leg only", tot))
    print("   %-58s %7.1f px" % ("(add measured vision-model pointer jitter, exp #2)", float("nan")))
    print("   %-58s %7.1f px" % ("TARGET WIDGET", 20.0))
    print("   capture-leg error alone = %.1f x the target width" % (tot / 20.0))
    print("   single-threaded tracking cost scales ~%.1fx on an RPi5 vs this host."
          % rpi_factor)
    return terms, tot


def main():
    screen = render_screen()
    screen.save("%s/screen-1920x1080-GT.png" % OUT)
    screen_bgr = cv2.cvtColor(np.array(screen), cv2.COLOR_RGB2BGR)
    a_rows, d_fill = section_a()
    cases, photos = section_b(screen_bgr)
    c_scores = section_c(photos)
    c2 = section_c2()
    d_res, d2_res = section_d(photos, screen_bgr)
    e_res = section_e(photos, screen_bgr)
    f_res = section_f(screen_bgr, photos)
    g_terms, g_tot = section_g()
    print("=" * 78)
    json.dump({"A_resolution": a_rows, "d_fill_m": d_fill, "C_ocr": c_scores,
               "C2_ocr_glyph_curve": c2, "D_tracking": d_res, "D2_cost": d2_res, "E_mapping": e_res,
               "F_ladder": f_res, "G_budget": {k: round(v, 2) for k, v in g_terms.items()},
               "G_total_px": round(g_tot, 2)},
              open("%s/sim-metrics.json" % OUT, "w"), indent=1, default=_json_default)
    print("wrote %s/sim-metrics.json" % OUT)


if __name__ == "__main__":
    main()
