#!/usr/bin/env python3
"""SIMULATION prep — make the images the vision-model jitter experiment reads.
Coarse = the whole photo downscaled (what the LLM sees first).
Fine   = the ROI around the candidate widget, upscaled (the zoom-in step).
Ground truth red-button centre in each produced image is printed AND written to
vision-jitter-gt.json so the jitter can be converted into SCREEN pixels."""
import json
import cv2
import numpy as np

OUT = "/home/user/work/GoPi/devdoc/webcam-capture-evidence"
src = cv2.imread(OUT + "/photo-P3-50cm-USB-1280x720.png")
gt_photo = (734.0, 376.8)          # printed by sim_capture_degrade.py section B
gts = {}
# coarse: 640x360 downscale of the full photo
coarse = cv2.resize(src, (640, 360), interpolation=cv2.INTER_AREA)
cv2.imwrite(OUT + "/jitter-coarse-640x360.png", coarse)
s = 640.0 / src.shape[1]
gts["jitter-coarse-640x360.png"] = {"image_wh": [640, 360],
                                    "gt_red_center": [gt_photo[0] * s, gt_photo[1] * s],
                                    "scale_vs_photo": s}
# fine: 260x200 ROI around the app window, upscaled 3x
x0, y0 = 560, 300
roi = src[y0:y0 + 200, x0:x0 + 260]
fine = cv2.resize(roi, None, fx=3, fy=3, interpolation=cv2.INTER_CUBIC)
cv2.imwrite(OUT + "/jitter-fine-roi-780x600.png", fine)
gts["jitter-fine-roi-780x600.png"] = {"image_wh": [780, 600],
                                      "gt_red_center": [(gt_photo[0] - x0) * 3,
                                                        (gt_photo[1] - y0) * 3],
                                      "scale_vs_photo": 3.0}
json.dump(gts, open(OUT + "/vision-jitter-gt.json", "w"), indent=1)
print(json.dumps(gts, indent=1))
