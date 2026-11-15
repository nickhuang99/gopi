#!/usr/bin/env python3
"""SIMULATION (no hardware): EXP #9 -- is the LLM needed for the Verify/arrival
judgement?  Compare the deterministic ROI |diff| verdict against the vision
model's verdict on the SAME before/after pair.

Pipeline identical to exp #1/#4: screen 1920x1080 -> keystone -> 2304x1296 photo
-> blur 1.1 -> noise 2/255 -> JPEG q75 (50 cm, the recommended operating point).
"""
import io, json, os, subprocess, time
import cv2, numpy as np
from PIL import Image, ImageDraw, ImageFont

OUT = "/home/user/work/GoPi/devdoc/webcam-capture-evidence"
FONT = "/usr/share/fonts/truetype/dejavu/DejaVuSans.ttf"
RNG = np.random.default_rng(7)
W, H = 2304, 1296
S2P = 0.626                      # screen -> photo ratio at 50 cm / 2304 px
BTN = (760, 600, 880, 640)       # actuated widget, screen px


def render(changed):
    img = Image.new("RGB", (1920, 1080), (58, 60, 68))
    d = ImageDraw.Draw(img)
    f11 = ImageFont.truetype(FONT, 11)
    d.rectangle((1180, 520, 1700, 700), fill=(240, 240, 240), outline=(90, 90, 90))
    for i in range(8):
        d.text((1200, 580 + i * 12), "Mode %d: automatic 1920x1080 60 Hz" % i, font=f11, fill=(30, 30, 30))
    d.rectangle(BTN, fill=(210, 60, 60) if not changed else (60, 90, 210))
    if changed:
        d.rectangle([560, 700, 1360, 740], fill=(255, 250, 190))
    return img


def photo(pil):
    src = np.float32([[0, 0], [1919, 0], [1919, 1079], [0, 1079]])
    dst = np.float32([[577, 312], [1726, 311], [1752, 984], [552, 985]])
    Hm = cv2.getPerspectiveTransform(src, dst)
    im = cv2.warpPerspective(cv2.cvtColor(np.array(pil), cv2.COLOR_RGB2BGR), Hm, (W, H), flags=cv2.INTER_AREA)
    im = cv2.GaussianBlur(im, (0, 0), 1.1)
    im = np.clip(im.astype(np.float32) + RNG.normal(0, 2.0, im.shape), 0, 255).astype(np.uint8)
    return cv2.imdecode(cv2.imencode(".jpg", im, [cv2.IMWRITE_JPEG_QUALITY, 75])[1], cv2.IMREAD_COLOR)


g = cv2.cvtColor(np.array(render(False)), cv2.COLOR_RGB2GRAY)
q = cv2.getPerspectiveTransform(np.float32([[0, 0], [1919, 0], [1919, 1079], [0, 1079]]),
                                np.float32([[577, 312], [1726, 311], [1752, 984], [552, 985]]))
quads = {"before-menu.jpg": photo(render(False)), "after-menu.jpg": photo(render(True))}
for n, im in quads.items():
    cv2.imwrite(os.path.join(OUT, n), im, [cv2.IMWRITE_JPEG_QUALITY, 75])


def roi_photo(im, box):
    x0, y0, x1, y1 = box
    pts = np.float32([[x0, y0], [x1, y0], [x1, y1], [x0, y1]]).reshape(-1, 1, 2)
    p = cv2.perspectiveTransform(pts, q).reshape(4, 2)
    X0, Y0 = p.min(0).astype(int) - 4
    X1, Y1 = p.max(0).astype(int) + 4
    return cv2.cvtColor(im[max(0, Y0):Y1, max(0, X0):X1], cv2.COLOR_BGR2GRAY)


a, b = quads["before-menu.jpg"], quads["after-menu.jpg"]
print("=== EXP #9: arrival judgement -- deterministic ROI diff vs the vision model ===")
for name, box in [("actuated widget (760,600,880,640)", BTN), ("control widget (960,600,1080,640)", (960, 600, 1080, 640))]:
    ra, rb = roi_photo(a, box), roi_photo(b, box)
    d = float(np.mean(np.abs(ra.astype(np.int16) - rb.astype(np.int16))))
    print("  %-38s ROI %s  mean|diff| = %6.2f grey levels  -> %s"
          % (name, ra.shape, d, "ARRIVED" if d > 2.6 else "no change"))

s = json.load(open(os.path.expanduser("/home/user/.gopi/settings.json")))
key, url = s["api_key"], s["vision_base_url"].rstrip("/") + "/v1/messages"
Q = ("These are two photos of the same computer screen taken seconds apart: "
     "photo A then photo B. Did the screen content visibly CHANGE between them? "
     "Answer in one short line starting with YES or NO.")


def ask(pa, pb, q):
    c = []
    for f in (pa, pb):
        c.append({"type": "image", "source": {"type": "base64", "media_type": "image/jpeg",
                                              "data": __import__("base64").b64encode(open(f, "rb").read()).decode()}})
    c.append({"type": "text", "text": q})
    body = json.dumps({"model": "deepseek-flash", "max_tokens": 1500, "messages": [{"role": "user", "content": c}]})
    t = time.time()
    p = subprocess.run(["curl", "-s", "-m", "180", url, "-H", "content-type: application/json",
                        "-H", "x-api-key: " + key, "-H", "anthropic-version: 2023-06-01",
                        "-X", "POST", "--data-binary", "@-"], input=body, capture_output=True, text=True)
    try:
        r = json.loads(p.stdout)
        tx = [x.get("text", "") for x in r.get("content", []) if x.get("type") == "text"]
        return (tx[-1] if tx else "").strip().replace("\n", " ")[:110], round(time.time() - t, 2), r.get("usage", {})
    except Exception as ex:
        return "PARSE-FAIL " + str(ex)[:40], round(time.time() - t, 2), {}


pairs = [("REAL change    ", os.path.join(OUT, "before-menu.jpg"), os.path.join(OUT, "after-menu.jpg"), "YES"),
         ("SAME state     ", os.path.join(OUT, "before-menu.jpg"), os.path.join(OUT, "before-menu.jpg"), "NO")]
for label, f1, f2, want in pairs:
    for i in range(3):
        txt, dt, u = ask(f1, f2, Q)
        ok = "OK " if txt.upper().startswith(want) else "WRONG"
        print("  %s trial %d wall=%.2fs in_tok=%s -> %s %s" % (label, i + 1, dt, u.get("input_tokens"), ok, txt))
