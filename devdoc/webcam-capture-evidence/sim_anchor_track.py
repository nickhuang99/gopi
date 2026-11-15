#!/usr/bin/env python3
"""SIMULATION — second, stronger tracker: STRUCTURAL ANCHOR + HOMOGRAPHY +
RATIO (scale-invariant, no metric calibration of template appearance).

Why this exists: a bare 12 px NCC template (script 1, section D/F) is the wrong
detector and its NCC falls with blur (inverted vs expectation) — it is not a
valid proxy for "can we find the target". The robust primitive is to lock the
whole SCREEN QUAD (a large, high-gradient structure: 1920x1080 of bright panel
against a dark surround), then take the geometric ratio `t = (target corner -
screen corner)/(screen extent)` from the last good frame, and re-apply it.
Error chain: quad-corner localisation -> H -> ratio -> target position.
Run: <venv python> sim_anchor_track.py
"""
import json
import math
import time

import cv2
import numpy as np
from PIL import Image, ImageDraw, ImageFont

OUT = "/home/user/work/GoPi/devdoc/webcam-capture-evidence"
SCREEN_W, SCREEN_H = 1920, 1080
TGT_X, TGT_Y = 1240.0, 610.0
RNG = np.random.default_rng(11)
FONT = "/usr/share/fonts/truetype/dejavu/DejaVuSans.ttf"


def frame_fraction(dist_m, screen_w_m, fov_h_deg=66.0):
    """fraction of the camera frame width the screen occupies (exact, not
    small-angle): 2*atan((W/2)/d) / FoV_h"""
    return math.degrees(2 * math.atan((screen_w_m / 2.0) / dist_m)) / fov_h_deg


def screen_render():
    img = Image.new("RGB", (SCREEN_W, SCREEN_H), (58, 60, 68))
    d = ImageDraw.Draw(img)
    f11 = ImageFont.truetype(FONT, 11)
    d.rectangle((1180, 520, 1700, 700), fill=(240, 240, 240), outline=(90, 90, 90))
    d.rectangle((1230, 600, 1250, 620), fill=(200, 30, 30))
    d.rectangle((1290, 600, 1310, 620), fill=(30, 160, 60))
    for i in range(8):
        d.text((1200, 580 + i * 12), "Mode %d: automatic 1920x1080 60 Hz" % i,
               font=f11, fill=(30, 30, 30))
    return img


def photo(screen_bgr, photo_w, quad_scale, src_pts, dst_pts, blur, q, nz, gamma):
    h = int(round(SCREEN_H * photo_w / float(SCREEN_W)))
    H = cv2.getPerspectiveTransform(np.float32(src_pts), np.float32(dst_pts))
    img = cv2.warpPerspective(screen_bgr, H, (photo_w, h), flags=cv2.INTER_AREA)
    if blur:
        img = cv2.GaussianBlur(img, (0, 0), blur)
    if gamma != 1.0:
        lut = (np.power(np.arange(256) / 255.0, gamma) * 255).astype(np.uint8)
        img = cv2.LUT(img, lut)
    if nz:
        img = np.clip(img.astype(np.float32) + RNG.normal(0, nz, img.shape),
                      0, 255).astype(np.uint8)
    if q:
        img = cv2.imdecode(cv2.imencode(".jpg", img,
                          [cv2.IMWRITE_JPEG_QUALITY, q])[1], cv2.IMREAD_COLOR)
    return img, H


def find_quad(img):
    """structural screen-quad detection with a 'panel covers the frame'
    fallback: when the camera is so close that the screen overfills the sensor,
    no surround is visible and Otsu returns only interior bright blobs (e.g. the
    app window) — the honest answer then is 'the whole frame IS the screen'."""
    q = _find_quad_contour(img)
    g = cv2.cvtColor(img, cv2.COLOR_BGR2GRAY)
    h, w = g.shape
    border = np.concatenate([g[:int(h * 0.04)].ravel(), g[-int(h * 0.04):].ravel(),
                             g[:, :int(w * 0.04)].ravel(), g[:, -int(w * 0.04):].ravel()])
    if q is not None and cv2.contourArea(q.astype(np.float32)) > 0.5 * w * h:
        return q
    if float(np.median(border)) > float(np.median(g)) + 8:      # bright border
        return np.float32([[0, 0], [w - 1, 0], [w - 1, h - 1], [0, h - 1]])
    return q


def _find_quad_contour(img):
    """structural screen-quad detection: Otsu on a blurred gray, largest
    external contour, polygon-approximated to 4 corners. This is the ONE
    deterministic anchor of the loop."""
    g = cv2.GaussianBlur(cv2.cvtColor(img, cv2.COLOR_BGR2GRAY), (5, 5), 0)
    t = cv2.threshold(g, 0, 255, cv2.THRESH_BINARY + cv2.THRESH_OTSU)[1]
    t = cv2.morphologyEx(t, cv2.MORPH_CLOSE, np.ones((15, 15), np.uint8))
    cnts, _ = cv2.findContours(t, cv2.RETR_EXTERNAL, cv2.CHAIN_APPROX_SIMPLE)
    if not cnts:
        return None
    c = max(cnts, key=cv2.contourArea)
    for eps in np.linspace(0.002, 0.08, 40):
        ap = cv2.approxPolyDP(c, eps * cv2.arcLength(c, True), True)
        if len(ap) == 4:
            return order_quad(ap.reshape(4, 2))
    return None


def order_quad(pts):
    pts = np.array(pts, dtype=np.float32).reshape(4, 2)
    s, d = pts.sum(1), np.diff(pts, axis=1).ravel()
    return np.float32([pts[np.argmin(s)], pts[np.argmin(d)],
                       pts[np.argmax(s)], pts[np.argmax(d)]])


def main():
    screen = screen_render()
    screen.save("%s/screen-1920x1080-GT.png" % OUT)
    sbgr = cv2.cvtColor(np.array(screen), cv2.COLOR_RGB2BGR)
    ap = [{"view": "V1 50cm CSI 2304x1296 (RECOMMENDED operating point)",
           "w": 2304, "k": 26, "blur": 1.1, "q": 75, "nz": 2.0, "g": 1.0,
           "src_w": 310.0, "d": 50.0},
          {"view": "V2 85cm USB 1280x720 (far end of envelope)",
           "w": 1280, "k": 18, "blur": 1.8, "q": 75, "nz": 3.5, "g": 1.0,
           "src_w": 310.0, "d": 85.0},
          {"view": "V3 24cm 2304 (screen OVERFILLS frame: corners leave the FoV)",
           "w": 2304, "k": 14, "blur": 0.8, "q": 75, "nz": 2.0, "g": 1.0,
           "src_w": 310.0, "d": 24.0}]
    results = []
    for case in ap:
        w = case["w"]
        h = int(round(SCREEN_H * w / float(SCREEN_W)))
        frac = frame_fraction(case["d"] / 100.0, case["src_w"] / 1000.0)
        sw = w * frac                       # screen width inside the photo
        s = sw / SCREEN_W                   # photo px per screen px
        sh = SCREEN_H * s
        x0, y0 = (w - sw) / 2.0, (h - sh) / 2.0
        k = case["k"]
        dst = [[x0 + k, y0], [x0 + sw - k, y0], [x0 + sw, y0 + sh], [x0, y0 + sh]]
        src = [[0, 0], [SCREEN_W, 0], [SCREEN_W, SCREEN_H], [0, SCREEN_H]]
        img, H = photo(sbgr, w, s, src, dst, case["blur"], case["q"], case["nz"], case["g"])
        p = "%s/anchor-%dx%d.png" % (OUT, w, h)
        cv2.imwrite(p, img)
        gt_t = cv2.perspectiveTransform(np.float32([[[TGT_X, TGT_Y]]]), H)[0][0]
        print("=" * 78)
        print("VIEW %s  photo %dx%d  screen->photo %.3f  target GT (%.1f,%.1f)"
              % (case["view"], w, h, s, gt_t[0], gt_t[1]))
        # --- step 1: lock the quad on a "reference" frame ---------------------
        q_ref = find_quad(img)
        if q_ref is None:
            print("   quad NOT found -> lock step fails")
            continue
        H_ref = cv2.getPerspectiveTransform(
            np.float32([[0, 0], [SCREEN_W, 0], [SCREEN_W, SCREEN_H], [0, SCREEN_H]]), q_ref)
        # ratio of the target inside the screen: measured ONCE on this frame
        tgt_photo = gt_t.astype(np.float32).reshape(1, 1, 2)
        t_ratio = cv2.perspectiveTransform(tgt_photo, np.linalg.inv(H_ref))[0][0]
        print("   quad locked: %s" % [[round(v, 1) for v in r] for r in q_ref.tolist()])
        print("   target's ratio in screen coords: (%.1f, %.1f)  [GT (1240,610)]"
              % (t_ratio[0], t_ratio[1]))
        # --- step 2: perturbation ladder, re-lock the quad each frame ---------
        print("   %-40s %9s %14s %14s" % ("perturbation", "quad ok",
                                            "quad RMS err", "target err"))
        print("   %-40s %9s %14s %14s" % ("", "", "[photo px]", "[screen px]"))
        for label, kw in [("baseline (no change)", dict()),
                          ("camera moved: keystone 26->40px", dict(k=40)),
                          ("camera moved: +3cm closer (ratio 0.5->0.6)", dict(d=43.0)),
                          ("app window unchanged, panel gamma 0.75", dict(g=0.75)),
                          ("blur sigma 1.1->2.5 (defocus)", dict(blur=2.5)),
                          ("JPEG q75->q25", dict(q=25)),
                          ("sensor noise 2->12/255", dict(nz=12.0)),
                          ("USB 720p instead of 2304", dict(w=1280)),
                          ("STALE ANCHOR: app scrolled 200 screen px", dict(scroll=200.0)),
                          ("STALE ANCHOR: app scrolled 600 screen px", dict(scroll=600.0))]:
            c = dict(case)
            c.update(kw)
            w2 = c["w"]
            h2 = int(round(SCREEN_H * w2 / float(SCREEN_W)))
            frac2 = frame_fraction(c["d"] / 100.0, c["src_w"] / 1000.0)
            s2 = (w2 * frac2) / SCREEN_W
            sh2 = SCREEN_H * s2
            x02, y02 = (w2 - w2 * frac2) / 2.0, (h2 - sh2) / 2.0
            k2 = c["k"]
            dst2 = [[x02 + k2, y02], [x02 + w2 * frac2 - k2, y02],
                    [x02 + w2 * frac2, y02 + sh2], [x02, y02 + sh2]]
            img2, H2 = photo(sbgr, w2, s2, src, dst2, c["blur"], c["q"], c["nz"], c["g"])
            tgt_now = (TGT_X, TGT_Y + c.get("scroll", 0.0))
            q2 = find_quad(img2)
            if q2 is None:
                print("   %-40s %9s" % (label, "NO"))
                continue
            H2e = cv2.getPerspectiveTransform(
                np.float32([[0, 0], [SCREEN_W, 0], [SCREEN_W, SCREEN_H], [0, SCREEN_H]]), q2)
            # apply the STORED ratio -> predicted target position on THIS frame
            pred = cv2.perspectiveTransform(
                np.float32([[[t_ratio[0], t_ratio[1]]]]), H2e)[0][0]
            gt2 = cv2.perspectiveTransform(np.float32([[tgt_now]]), H2)[0][0]
            err = math.hypot(pred[0] - gt2[0], pred[1] - gt2[1]) / s2
            corner_sig = float(np.sqrt(np.mean(
                np.sum((q2 - np.float32(dst2)) ** 2, axis=1))))   # per-corner RMS vs GT
            print("   %-40s %9s %10.2f %12.1f" % (label, "yes", corner_sig, err))
            results.append({"view": case["view"], "perturbation": label,
                            "target_err_screen_px": round(err, 1),
                            "quad_corner_sd_px": round(corner_sig, 2)})
        # --- step 3: cost of the quad-lock at full resolution -----------------
        for rw in (1.0, 0.5, 0.25):
            small = cv2.resize(img, None, fx=rw, fy=rw, interpolation=cv2.INTER_AREA)
            t0 = time.perf_counter()
            for _ in range(5):
                find_quad(small)
            dt = (time.perf_counter() - t0) / 5 * 1000
            print("   quad-lock cost: %3d%% res (%dx%d) %.1f ms on host x86"
                  % (rw * 100, small.shape[1], small.shape[0], dt))
    json.dump(results, open("%s/anchor-track-metrics.json" % OUT, "w"), indent=1)


if __name__ == "__main__":
    main()
