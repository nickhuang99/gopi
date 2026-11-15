#!/usr/bin/env python3
"""SIMULATION — is the binding constraint RESOLUTION or BLUR?
Same photo geometry, only the lens-blur sigma changes. OCR recall on 11 px UI text.
Run: <venv python> sim_blur_vs_ocr.py"""
import subprocess, cv2, numpy as np
from PIL import Image, ImageDraw, ImageFont
OUT = "."
FONT = "/usr/share/fonts/truetype/dejavu/DejaVuSans.ttf"
LINE = "Mode 0: automatic 1920x1080 60 Hz"
# 11 px UI text rendered on a 1920-wide panel at 50cm/2304 -> 6.9 photo px glyph
img = Image.new("RGB", (1920, 200), (240, 240, 240))
ImageDraw.Draw(img).text((20, 40), LINE, font=ImageFont.truetype(FONT, 11), fill=(20, 20, 20))
screen = cv2.cvtColor(np.array(img), cv2.COLOR_RGB2BGR)
for sig in (0.4, 0.7, 1.0, 1.1, 1.4, 2.0, 3.0):
    s = 0.626
    small = cv2.resize(screen, None, fx=s, fy=s, interpolation=cv2.INTER_AREA)
    if sig: small = cv2.GaussianBlur(small, (0, 0), sig)
    small = cv2.imdecode(cv2.imencode(".jpg", small, [cv2.IMWRITE_JPEG_QUALITY, 75])[1], 1)
    p = "%s/blursweep-sig%.1f.png" % (OUT, sig)
    cv2.imwrite(p, small)
    t = " ".join(subprocess.run(["tesseract", p, "stdout", "--psm", "6"],
                                capture_output=True, text=True).stdout.split())
    cap = 11 * 0.729 * s
    print("  blur sigma %.1f px (glyph cap %.1f px, image %dx%d): OCR %r"
          % (sig, cap, small.shape[1], small.shape[0], t[:52]))
