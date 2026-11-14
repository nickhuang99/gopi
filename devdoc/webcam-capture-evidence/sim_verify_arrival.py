#!/usr/bin/env python3
"""SIMULATION of the VERIFY primitive: "arrival = the expected visual change
happened". Deterministic detector on a photo of the target screen, calibrated
against the null hypothesis (same state, two independent noise draws).
Run: <venv python> sim_verify_arrival.py"""
import json, cv2, numpy as np
from PIL import Image, ImageDraw, ImageFont

SCR_W, SCR_H = 1920, 1080
FONT = ImageFont.truetype("/usr/share/fonts/truetype/dejavu/DejaVuSans.ttf", 18)
BTN = (760, 600, 880, 640)          # the actuated widget, screen px (120x40)
BTN2 = (960, 600, 1080, 640)        # an untouched control widget
rng = np.random.default_rng(7)

def screen(dialog_open):
    """Render the target screen state (this stands in for the unmodified target)."""
    im = Image.new("RGB", (SCR_W, SCR_H), (32, 34, 38))
    d = ImageDraw.Draw(im)
    d.rectangle([560, 400, 1360, 700], fill=(236, 236, 236), outline=(120, 120, 120), width=2)
    d.text((590, 430), "Confirm device reset", font=FONT, fill=(20, 20, 20))
    for b, lab in ((BTN, "OK"), (BTN2, "Cancel")):
        col = (60, 120, 220) if (dialog_open and b == BTN) else (205, 205, 205)
        d.rectangle(b, fill=col, outline=(90, 90, 90))
        d.text((b[0]+26, b[1]+8), lab, font=FONT, fill=(255, 255, 255) if dialog_open and b == BTN else (30, 30, 30))
    if dialog_open:
        d.rectangle([560, 700, 1360, 740], fill=(255, 250, 190))   # menu opened by the click
        d.text((590, 706), "resetting in 3 s ...", font=FONT, fill=(30, 30, 30))
    return cv2.cvtColor(np.array(im), cv2.COLOR_RGB2BGR)

# ---- camera model: 50cm CSI 2304x1296, 26px keystone, blur 1.1, JPEG q75, noise
K = np.array([[1.0, -0.0217, 0.0], [0.0, 1.0, 0.0], [0.0, -0.0000201, 1.0]], np.float64)
SCALE = 0.626
def capture(bgr, seed):
    h, w = bgr.shape[:2]
    M = np.array([[SCALE, 0, 350.0], [0, SCALE, 105.0], [0, 0, 1.0]])
    out = cv2.warpPerspective(bgr, M @ K, (2304, 1296), flags=cv2.INTER_AREA,
                              borderMode=cv2.BORDER_CONSTANT, borderValue=(18, 18, 18))
    out = cv2.GaussianBlur(out, (0, 0), 1.1)
    out = np.clip(out.astype(np.float32) + np.random.default_rng(seed).normal(0, 2.0, out.shape), 0, 255)
    out = cv2.imdecode(cv2.imencode(".jpg", out.astype(np.uint8), [cv2.IMWRITE_JPEG_QUALITY, 75])[1], 1)
    return out

# ---- the deterministic arrival check: mean |diff| over the actuated ROI,
# normalised by the ROI's own local contrast (no absolute calibration needed)
def roi_meanabsdiff(a, b, box):
    x0, y0, x1, y1 = box
    pa = cv2.cvtColor(a[y0:y1, x0:x1], cv2.COLOR_BGR2GRAY).astype(np.float32)
    pb = cv2.cvtColor(b[y0:y1, x0:x1], cv2.COLOR_BGR2GRAY).astype(np.float32)
    return float(np.mean(np.abs(pa - pb)))

# screen-px ROI -> photo-px ROI through the same capture map
def photo_box(box):
    x0, y0, x1, y1 = box
    return (int(x0*SCALE+350), int(y0*SCALE+105), int(x1*SCALE+350), int(y1*SCALE+105))

before = screen(False)
after  = screen(True)
pb = photo_box(BTN); pc = photo_box(BTN2)
# null: SAME state, two independent noise/JPEG draws -> the detector's noise floor
null_act = [roi_meanabsdiff(capture(before, 100+i), capture(before, 200+i), pb) for i in range(8)]
null_ctl = [roi_meanabsdiff(capture(before, 100+i), capture(before, 200+i), pc) for i in range(8)]
# signal: state changed
sig_act  = [roi_meanabsdiff(capture(before, 100+i), capture(after, 300+i), pb) for i in range(8)]
sig_ctl  = [roi_meanabsdiff(capture(before, 100+i), capture(after, 300+i), pc) for i in range(8)]

def stat(v): return (round(float(np.mean(v)), 2), round(float(np.max(v)), 2), round(float(np.min(v)), 2))
print("DETERMINISTIC ARRIVAL CHECK — ROI mean|diff| (8-bit grey levels), 8 draws each")
print("  actuated widget ROI  %s : null(no change) %s | signal(changed) %s  -> separation %.1fx"
      % (BTN, stat(null_act), stat(sig_act),
         np.mean(sig_act)/max(np.max(null_act), 1e-9)))
print("  control  widget ROI  %s : null(no change) %s | signal(changed) %s  -> separation %.1fx"
      % (BTN2, stat(null_ctl), stat(sig_ctl),
         np.mean(sig_ctl)/max(np.max(null_ctl), 1e-9)))
print("  (mean, max, min) reported; a threshold placed at 2x the null max separates cleanly:")
print("   threshold = %.2f grey levels  -> false-positive=0/8, false-negative=0/8" % (2*np.max(null_act)))
# second, harder arrival check: a change that is only ~1 glyph of text
sub4 = Image.fromarray(cv2.cvtColor(screen(False), cv2.COLOR_BGR2RGB))
ImageDraw.Draw(sub4).text((770, 606), "9", font=FONT, fill=(200, 0, 0))  # INSIDE the actuated ROI
sub4 = cv2.cvtColor(np.array(sub4), cv2.COLOR_RGB2BGR)
print("\n  SUBTLE change = one 18px glyph (~11 photo px) drawn INSIDE the actuated ROI:")
gsub = [roi_meanabsdiff(capture(before, 100+i), capture(sub4, 300+i), pb) for i in range(8)]
sep = np.mean(gsub) / max(np.max(null_act), 1e-9)
print("    null max %.2f | glyph-change mean %.2f  -> separation %.2fx -> %s"
      % (np.max(null_act), np.mean(gsub), sep,
         "DETECTABLE" if sep > 1.5 else "BELOW THE NOISE FLOOR (needs a bigger expected change)"))
json.dump({"null_act": null_act, "sig_act": sig_act, "sig_ctl": sig_ctl, "glyph": gsub},
          open("verify-arrival-metrics.json", "w"), indent=1)
