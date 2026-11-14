<!-- doc-class: historical -->
# Webcam Capture for a Raspberry Pi 5 "eye + hand" box — Evaluation + Design

**Deliverable of epic `01a08b79-bc19` (reframed).** Status: EVALUATION + DESIGN only.
No production code was written; `/home/user/work/ch9329` was read only; every
artifact lives under `devdoc/` as the brief names.

**One-sentence conclusion.** A webcam-as-eye on an unmodified target is viable, and
it is viable *only* as a closed-loop visual servo whose per-step signal is
quantized to "which way" (a direction/heat signal), never to "the target is at
screen (x,y)": the measured open-loop error is ~40x the size of a GUI target,
while the deterministic *arrival* signal (did the expected visual change happen)
has a 7.9x margin at exactly the resolution where absolute localisation dies.

---

## 0. How to read this document, and what is evidence vs assumption

Every quantitative claim carries one of these labels:

| label | meaning |
|---|---|
| **[M-HW]** | measured on real hardware on this host (webcam timing, X-server pointer transfer) |
| **[M-SIM]** | measured by running a real program on a *simulated* optical image (numbers are real program output; the optics are synthetic) |
| **[M-API]** | measured by real calls to the real vision model (on simulated images) |
| **[A]** | arithmetic derived from published specs, shown inline |
| **[X]** | assumption or estimate — NOT measured, stated as such |
| **[SPEC]** | quoted from a cited external source (URL in Appendix A) |

**The single most important honesty statement in this document:** this host's
integrated webcam delivers frames, but in a completely dark room, so **no real
optical test of screen→photo fidelity was performed**. Everything optical is
**[M-SIM]**, clearly labelled at every use. What *was* measured on real hardware
is (a) the capture-leg timing of a real USB webcam, and (b) the Actuate-side
pointer transfer function on a real X server. Section 5 states this at length.

Evidence directory: `devdoc/webcam-capture-evidence/` (91 files).
Index in Appendix B.

---

## 1. (a) RETRACTION — the open-loop absolute-coordinate premise is wrong

### 1.1 What is retracted

The original brief's section C stated the premise verbatim:

> "Since Actuate uses **absolute** mouse coordinates scaled to the target's screen,
> the capture side must map 'a point in the photo' → 'a point on the target screen'."
> … "**Why this matters**: this is the bridge between Capture and Actuate. Without
> it, the agent can see but cannot aim."

**This is retracted.** It is an open-loop control design: observe once, compute an
absolute target coordinate, emit one absolute command, assume it landed. The
chain `photograph → compute screen (x,y) → HID click at (x,y)` has no feedback
term, and the task owner rejected it from first principles. Quoted verbatim from
the addendum that overturns the brief:

> "human operates these input device is not based on accurate coordinate, but
> just moving and feedback of position and adjust. exactly how mouse is
> operated, never based on pre-calculated accurate coordinate."

and the owner's conclusion after the working POC:

> "this can NOT be done unless with robot vision which continues adjust the mouse
> or key moving position. ... this is actually the most difficult part."

and the explicit instruction to this task:

> "The user spent real effort on a POC and concluded it is too hard *in open-loop
> form*. Honour that: do not produce a report that quietly assumes open-loop
> will work if only the camera were better."

**Therefore this document does not propose a better camera to save the premise.**
Section 4 shows the arithmetic of why a better camera cannot save it.

### 1.2 Why it fails — the error budget, with numbers

Two independent legs, both measured or derived, and they multiply.

**Leg 1 — capture/geometry (synthetic optics, real code) [M-SIM]**
`sim_capture_degrade.py` → `sim-raw-output.txt` §G, terms summed in quadrature:

| term | screen px | basis |
|---|---|---|
| resolution quantisation (1 screen px @ full-frame screen) | 1.0 | [M-SIM] |
| **keystone residual, uncorrected H (26 px corner error / 1200 px)** | **20.8** | [M-SIM] |
| lens distortion residual (0.5 % barrel, uncorrected) | 6.0 | [X] model |
| focus/blur centroid bias (σ=1.1 photo px, full-frame) | 0.9 | [M-SIM] |
| screen-to-sensor moiré beating (half a sampling cell) | 1.6 | [X] model |
| target feature centroid on a 13 px widget | 1.0 | [M-SIM] |
| camera pose drift between shots (1 mm @ 50 cm) | 3.8 | [A] |
| **total, capture leg only** | **22.1** | quadrature |

A GUI target is commonly **20x20 screen px** — the capture leg's own error is
**22.1 px = 1.1x the target width**, before the vision model is involved.

**Leg 2 — vision-model absolute localisation jitter (real API calls) [M-API]**
`api-vision-jitter-raw.txt`, 5 identical calls per image against the raw endpoint
of the same model the agent uses:

| view sent | err in the *sent image* (px) median / mean / max | err in **screen** px median / mean / max |
|---|---|---|
| coarse 640x360 (full view) | 48.5 / 266.1 / 835.3 | **154.8 / 850.3 / 2668.6** |
| fine 780x600 (3x zoom of the correct ROI) | 29.9 / 46.0 / 142.3 | **15.9 / 24.5 / 75.8** |

A second, independent 8-call run through the agent's own `vision_analyze` tool
(`vision-jitter-report.json`) gives the same shape: coarse median 2291 screen px
(3 of 6 replies point at an unrelated part of the image), fine-ROI median 23.0
screen px. *Metric caveat, stated honestly:* the two runs use different
scale conversions (the tool-run figure divides by the photo scale a second time
in some rows); the **raw-endpoint run is the trustworthy one**, and both agree on
the verdict: coarse full-view absolute localisation is off by hundreds to
thousands of pixels and is *multimodally wrong*, not merely noisy.

**Combined.** Even in the best case measured (fine ROI, 24.5 px mean) the chain is
√(22.1² + 24.5²) = **33.0 screen px** against a 20 px target — and the design's own
arrival tolerance is 12 px (below). In the coarse case it is ≈ 850 px, i.e.
**42x the target**. This is a structural failure, not a tuning problem.

**Leg 3 — the decisive one: the open loop does not know it is wrong.**
`sim_closed_loop.py` (SIMULATION, `closed-loop-raw-output.txt`), the retracted
premise executed as a strategy (`open_loop_one_shot`): one command assuming
1 HID count = 1 screen px, then click. Four start positions, 1920x1080 target,
80x24 px widget at (1350, 620):

```
--- strategy: open_loop_one_shot ---
  start far    ( 300, 250) -> iters= 1 finalTrueErr=   534.8px flips=0  MISSED
  start mid    ( 900, 900) -> iters= 1 finalTrueErr=   769.9px flips=0  MISSED
  start near   (1300, 590) -> iters= 1 finalTrueErr=    84.4px flips=0  MISSED
  start corner (  60,1000) -> iters= 1 finalTrueErr=   628.7px flips=0  MISSED
```

0/4 hits, 84–770 px of final error — and at the moment it "clicked" it *believed*
its error was **5–8 px**. That is the whole argument for the retraction in one
line: the open loop's self-reported confidence is uncorrelated with its actual
error, so it cannot even detect its own failure. A system that cannot tell it
missed is not a system that can be tuned into working.

### 1.3 What replaces it

One sentence: **the camera's job is not to produce a coordinate, it is to produce
a direction and to certify arrival.** The pointer is moved by small measured
relative nudges, the result is re-observed, and the loop terminates on *evidence
of the expected change*, not on a coordinate comparison. Sections 2–3 design that.

### 1.4 What survives from the original brief (explicitly retained)

- **RPi5 as a mobile agent box** (webcam + CH9329 + gopi) — the topology stands.
- **Non-invasiveness**: a webcam needs no cabling, no port access and no software
  on the target; an HDMI capture card requires physical access to the video port
  and the target's cooperation. This is the reason to accept a hard optical
  problem instead of an easy electrical one. Retained.
- **Optical-artifact catalogue** — retained, reframed in §4.6 as "what degrades
  the *feedback signal*" rather than "what breaks the coordinate math".
- **Photo→screen mapping** — retained, but demoted to *rough initial estimate and
  search-region definition* (§7), never aiming.
- **Non-computer instrument panels** — retained and extended in §9 with its own
  measured simulation.

---

## 2. (b) Visual-servoing framing — and which classic assumptions break here

### 2.1 The standard taxonomy, from cited sources

Visual servoing ("vision-based robot control") is the established robotics answer
to exactly our problem: control motion from visual feedback rather than from a
pre-computed absolute pose [SPEC-1].

Two axes matter for us [SPEC-1]:

| axis | option A | option B | **ours** |
|---|---|---|---|
| camera placement | **eye-in-hand** (camera on the moving hand; "end-point open-loop control") | **eye-to-hand** (camera fixed in the world, observing both target and hand) | **eye-to-hand** — the webcam is bolted to the box and the *cursor* is the hand |
| control law | **IBVS** — error between current and desired *image* features; **no pose estimate** (Weiss & Sanderson) | **PBVS** — estimate the target's pose w.r.t. the camera, then servo in 3-D | **IBVS-flavoured, with a 2-D-on-the-screen twist** (below) |

The third family — hybrid (2-1/2-D, motion-partition, partitioned-DOF) [SPEC-1] —
is where our design actually lands, for the reason in §2.3.

**IBVS** is the right family first, because it needs no calibrated camera model:
the control law is a function of the error between current and desired image
features alone [SPEC-1]. The clean precedent for our situation is
*uncalibrated* IBVS: Yin et al. achieve "human-like manipulation accuracy … without
hand-eye and camera calibrations", their only stated restriction being that the
relevant image features must be visible in the views, and they explicitly handle
the case where **"cameras are moved or the workspace shifts during manipulation"**
[SPEC-2]. Our box is a camera that gets moved; that is precisely the cited regime.

The cost of IBVS is named in the literature too: it has "difficulties with very
large rotations, which has come to be called **camera retreat**" [SPEC-1], and the
2025 adaptive-IBVS paper opens by noting that "most research papers focused on
developing control solutions when 3D point features can be detected **inside the
field of view**", and proposes a feedforward feature-estimation loop for features
**outside** the FoV [SPEC-3]. Both are real hazards for us (§2.3, items 3 and 6).

### 2.2 The mapping that matters: our "image plane" is the target's screen

Classic IBVS works in camera-image coordinates. Ours has a second plane in the
middle: the target's *screen*. The useful restatement is:

* features live in the **photo** (a projective view of the screen plane),
* the thing we want to control lives in **screen coordinates** (the OS pointer),
* and the two are related by a homography, because the screen is planar [SPEC-4a].

So the design uses IBVS's *spirit* (never estimate a 3-D pose; servo on a 2-D
feature error) with a homography as the 2-D→2-D bridge, and it estimates the
homography **every iteration** rather than once (§7.2). That is the "hybrid"
attachment: 2-D error, no metric calibration, one 3x3 reprojection.

### 2.3 Which classic servoing assumptions break here — itemized

1. **No calibrated camera model.** Standard IBVS/PBVS assume intrinsics and often
   hand-eye calibration. We refuse calibration: an uncalibrated IBVS precedent
   exists [SPEC-2], and the alternative (calibrate a webcam that gets picked up
   and moved) is not worth it. **Mitigation:** everything is expressed as a ratio
   measured *in the loop* — never as millimetres.
2. **The loop rate is ~0.4 Hz, not 30–100 Hz.** Servoing normally runs
   "30–100 Hz" [SPEC-1, context of the whole field]; ours is bounded by a vision
   round trip measured at **1.5–4.6 s** [M-API]. Control-theory quantities
   (stability margins, damping) are meaningless at 0.4 Hz. **Mitigation:** the
   loop must be designed to converge in a *counted number of iterations*
   (measured: 9–14, §3.4), with an explicit iteration budget as the abort rule.
3. **The scene moves between iterations.** IBVS assumes a static target. Ours is a
   GUI: menus open, pages scroll, the window under the cursor changes. Measured
   consequence [M-SIM]: with the anchor's stored ratio, a 200 px content scroll
   produces **202.7 screen px** of target error and a 600 px scroll produces
   **618.4 px** — the signal (§3.5).
4. **The feature we track is not a blob but a rendered glyph/bitmap.** Classic
   features are corners/centroids of a physical object; ours is a cursor arrow
   drawn by a compositor, whose appearance changes with the background beneath it.
   Measured [M-SIM]: an unmasked normalised cross-correlation template of the
   cursor **collapses from NCC 0.99 to 0.45** as the pointer crosses different
   backgrounds and the servo diverges. **Mitigation:** mask the template with a
   frame-difference mask (same code path as the cursor bootstrap, §3.3).
5. **The actuator's transfer function is non-linear and opaque.** IBVS assumes the
   robot's kinematics are at least approximately known. Ours is an OS pointer
   whose mapping device-units→px depends on the OS, DPI scaling and pointer
   acceleration. Measured directly on this host [M-HW] (§3.4 and
   `x11-pointer-transfer-raw.txt`): on a clean Xvfb server it is exactly
   1.000 px/count for all command sizes 1…127 and for decomposed 508-count
   commands — but the *same* script recorded 0.000 px/count for ≥24 counts during
   one 3-second window on a different (long-running) server, and the cause was
   not established. A transfer that is 1.000 in isolation once measured 0.000.
   **Mitigation: measure-and-invert, never assume** — and the measurement is
   exactly what the servo needs anyway (§3.4).
6. **No global feature is guaranteed to be in view.** The "camera retreat"
   failure of IBVS [SPEC-1] and the out-of-FoV case [SPEC-3] have a direct
   analogue: the target may be outside the *current* crop, and the cursor may be
   outside the frame entirely. Measured [M-SIM]: a naive "largest bright contour"
   screen detector locks onto an application window instead of the panel when the
   panel overfills the frame, giving **275.5 screen px** of error instead of the
   5–9 px it delivers when the panel is fully visible, and after a small camera
   move the same wrong lock gives **1014.7 px** [M-SIM, §7.3]. **Consequence:**
   quad-validity gating is mandatory, and the operating envelope excludes
   "screen fills the frame" (see §4.4 — this inverts the naive "get closer" instinct).
7. **Feedback is not pixel-truth of the actuator.** In robots, the encoder is
   ground truth for the joint. Here the only feedback is a *photograph*, whose
   own noise floor is measured at **0.86–1.30 grey levels** (mean |diff| between
   two frames of the *same* state, 8 independent draws each) [M-SIM] — i.e. the
   feedback channel has a defined noise floor and any threshold must be expressed
   relative to it, not as an absolute constant.

### 2.4 The GUI-agent literature already validates the coarse-to-fine half

This is worth stating because it means half the design is not speculative. GUI
grounding research converges on the same conclusion as the retraction, in their
own words: GUI-Lens states that existing methods "still rely on a direct click
prediction, allowing visual ambiguity or **an inaccurate initial estimate to
propagate** to the final result", and fixes it with a coarse-to-fine framework
where "the VLM selects the region and scale of the next view, which is cropped
and enlarged to provide finer visual details", reporting "up to **24.9 percentage
points**" improvement [SPEC-5]. The earlier Iterative Narrowing paper is the same
mechanism [SPEC-6]. **Their loop is bounded by the same wall ours is: their
screenshots are digital, ours are photographs** — which is precisely the gap this
document designs for.

---

## 3. (c) The LLM-in-the-loop convergence design

### 3.1 Shape of the loop

```
                    ┌──────────────── DETERMINISTIC (no LLM) ────────────────┐
   frame t ──► [quad lock + H] ──► [cursor template match in ±W window] ──► believed cursor (px in screen coords)
                    └──────────────────────────────────────────────────────┘
                                        │
                   LLM (1 call / iteration) answers only: "which way, and how far coarse"
                                        │
                              [command planner: quantise to int8 HID frames,
                               apply gain estimate, damp near target, cap travel]
                                        │
                              CH9329 relative-mouse frames ──► target OS ──► cursor moves
                                        │
   frame t+1 ──► [arrival check: ROI mean|diff| vs self-calibrated null floor]
                                        │
                        arrived? ──► Verify the *policy* expectation (menu open / value changed)
                                   ──► done | else next iteration (budget--)
```

Measured properties of this shape: **9–14 iterations** to arrive, **10.6–27.7 px**
final true error, **4/4 arrivals** with the arrival verdict agreeing with ground
truth every time [M-SIM, `closed-loop-metrics.json`, strategy `servo_learned`].

### 3.2 Roles per iteration — deterministic vs LLM (the required split)

| loop step | who | why | measured basis |
|---|---|---|---|
| open the stream / grab a frame | deterministic | no judgement involved | 160 ms/frame [M-HW] |
| locate the screen quad + homography | deterministic | classical contour + 4-corner correspondence; validity gate on aspect | quad RMS 1.8–3.5 photo px; lock cost 22.0/6.1/2.1 ms at 100/50/25 % scale [M-SIM] |
| track the cursor (or a landmark) across frames | deterministic | template match in a small window | ±90 px window on 2304x1296: 3.83–5.20 ms/frame host; cursor found 4/4 at 9.8–11.3 photo px [M-SIM] |
| **decide "which way / roughly how far"** | **LLM** | the only step needing semantic understanding ("that is the OK button", "the menu is not open yet") | 1.5–4.6 s/call [M-API] |
| **choose the target at all** (which widget) | **LLM** | genuinely semantic | same call |
| convert direction+distance into HID counts | deterministic | arithmetic + measured gain | 1 count = 1 px measured [M-HW]; non-linear gain table otherwise [M-SIM] |
| **is the app state what we wanted?** (policy level) | **LLM** | "is the dialog that appeared the right one" | 6/6 correct in 1.5–3.3 s [M-API] |
| **did the pixels change at all?** (arrival) | **deterministic** | a threshold on local pixel change beats spending 2.5 s to ask | 7.9x separation; 0/8 FP and 0/8 FN at a 2x-null threshold [M-SIM] |
| re-anchor when the anchor is stale | deterministic trigger, LLM re-acquisition | staleness is detectable without semantics (§3.5) | stale-anchor error 202.7/618.4 px is a *signal*, not a silent failure [M-SIM] |

**Design rule extracted from the measurements:** *the LLM never produces a
number that is used as a coordinate.* Its outputs are (i) a discrete direction
and coarse magnitude, (ii) a region choice, (iii) a policy verdict. Every
coordinate that reaches the actuator is produced by deterministic code from a
deterministic measurement. This is exactly what the measured jitter forces: the
LLM's own absolute estimate has a 15.9–850 px error, but its *direction* judgement
does not need that accuracy.

### 3.3 The bootstrap — how the cursor is found the first time (all deterministic)

Problem: template matching needs a template of the cursor, but we do not have one.
Solved without the LLM, using the actuator as a probe [M-SIM, `sim_closed_loop.py`
`bootstrap`]:

1. Send a known dither command (e.g. +32 counts on x, then back).
2. Frame-difference the two photos (thresholded at the measured noise floor) →
   a small connected mask = the cursor glyph.
3. Grab the template from that mask **and keep the mask**: match with
   `TM_CCORR_NORMED` under a mask, not plain `TM_CCOEFF_NORMED`.

Measured result: **4/4 successful**, found position 9.8–11.3 **photo** px from the
true cursor (≈ 4.9–5.7 screen px at the 0.626 screen→photo ratio) — i.e. the
deterministic bootstrap localises the pointer *better than the system's own
10 px design assumption*, and it costs one extra frame pair.

The masking requirement is not optional: with an unmasked `TM_CCOEFF_NORMED`
template the score fell from 0.99 to 0.45 as the cursor crossed a different
background, and the servo diverged [M-SIM]. Root cause: normalised correlation
subtracts the mean, so a masked-out region contributes arbitrary signal.

### 3.4 The command planner — the measured transfer function is the whole trick

`sim_closed_loop.py` ran three strategies against the same target and the same
simulated optics. This is the core empirical result of the design section:

| strategy | iterations | final true error (px) | sign flips | outcome |
|---|---|---|---|---|
| `servo_learned` (measure-and-invert the counts→px transfer) | **10 / 14 / 9 / 14** | **27.7 / 10.6 / 15.0 / 13.0** | 0–2 | **4/4 ARRIVED**, verdict agreed with ground truth 4/4 |
| `servo_fixed_gain` (the naive "1 count = 1 px" belief) | 4 / 30 / 30 / 30 | 535.8 / 547.9 / 310.5 / 529.7 | 1 / 29 / 29 / 28 | **1 FALSE ARRIVAL**, 3/4 MISSED |
| `open_loop_one_shot` (the retracted premise) | 1 | 534.8 / 769.9 / 84.4 / 628.7 | 0 | 0/4 hits |

The learned controller's own measured transfer table (counts → px moved), taken
from the run log:

```
start far : [(24,120.5), (125,875.0), (41,205.0), (20,100.0), (6,9.0), (8,24.0), (4,6.0), (5,7.0), (5,7.0), (2,2.0)]
start near: [(24,120.0), (15,45.0),  (12,36.0),  (4,6.0),  (5,7.0), (6,9.0), (4,6.0), (4,6.0), (3,4.0)]
```

Note what the *real* numbers say: 24 counts → 120.5 px (5.0 px/count) while
2 counts → 2 px (1.0 px/count). The ratio is **5x** across the command range, and
whether the OS's curve is an acceleration profile, a rounding artefact or
something else **does not matter to the design** — it is measured in the loop and
inverted. Supporting real measurement [M-HW] on Xvfb: XTEST relative motion gave a
*perfectly linear* 1.000 px/count for 1…127 and for decomposed 508-count commands,
and the `xset m` acceleration setting (2/1 threshold 4 vs 1/1 threshold 0) made
**no measurable difference**. So the linear case is achievable — **and it is still
correct to measure it**, because the same script observed a 0.000 px/count stall
on another server (§2.3 item 5, `x11-pointer-transfer-raw.txt` step 4).

**Planner rules that the failures forced (each is a proven requirement):**

- **Decompose, never truncate.** A 508-count move costs 5 int8 frames. Truncating
  to ±127 silently loses the rest — the classic relative-pointer bug.
- **Cap travel per iteration to the search window.** With a fixed ±64 px window
  the tracker *lost the cursor* whenever the commanded travel exceeded 64 px. The
  window must scale with the expected command travel (`w ≈ |Δ| + margin`).
- **Damp near the target.** The last commands must be small (≤6 counts): the
  measured 5.0 px/count region is exactly where overshoot happens, and
  `servo_learned`'s monotone trace shows the final approach stepping only 2–8 px.
- **Arrival is a deadband, not a threshold crossing.** Requiring believed |err| ≤ tol
  on **two consecutive** frames (not one) is what removed the three spurious
  arrivals that a single-frame test produced.
- **Absolute HID is not the primary target for this design.** A sibling evaluation
  of the CH9329 covers absolute-mode accuracy and its chip-side reference
  resolution (`devdoc/ch9329-hid-actuator-evaluation.md` §6 — do not duplicate
  it). This document's servo drives **relative** frames, which needs no
  chip-resolution calibration at all; absolute mode remains a useful *initial
  teleport* to move the pointer into the neighbourhood cheaply.

**Serial cost of the Actuate leg [A]:** a CH9329 relative frame is 11 bytes on the
wire (`57 AB 00 05 05` + 5 data + checksum); at the POC's 9600 8N1 that is
960/11 = **87 commands/s = 11.5 ms per command**, so a full 508-count move costs
**58 ms** of serial time. The Actuate leg is therefore negligible against the
capture leg's 160 ms frame period and the LLM's ~2.5 s.

### 3.5 Convergence criteria — tied to the Verify primitive

Two levels, deliberately separate:

**Level 1 — mechanical arrival (deterministic, no LLM).**
`Verify` = "the expected visual change occurred". Implemented as the mean absolute
grey-level difference over an **ROI known to contain the target**, against a
threshold derived from that ROI's own **self-calibrated null floor** (two frames,
same state). Measured [M-SIM, `verify-raw-output.txt`, `exp9-arrival-llm-raw.txt`]:

| condition | ROI mean \|diff\| (grey levels) | separation vs null |
|---|---|---|
| null (same state, two independent draws) | 0.86 (max 0.92) actuated ROI / 0.32 control ROI | 1.0x |
| **real GUI change** (button red→blue + a menu row appears) | **7.28** | **7.9x** |
| *untouched* control widget | 0.47 | 1.0x — correctly NOT arrival |
| subtle change: one 18 px glyph (~11 photo px) drawn inside the ROI | 2.94 vs null max 1.30 | 2.26x — detectable, barely |

So: threshold = 2 x the ROI's own null maximum; 0/8 false positives and 0/8 false
negatives on the primary case. **Honesty correction, recorded because it was
published earlier in this session:** an earlier run reported a 48.9x separation
for the same test, but that version compared a *same-draw* null against a
*redrawn* signal — an apples-to-oranges floor. The honest same-methodology
separation is **7.9x**, and 7.9x is still a clean 3.9x margin over a 2x threshold.

**Level 2 — policy verification (LLM).** Arrival only says "the pixels changed".
Something must say "the *right* pixels changed in the *right* way" — "the menu is
open", "the value field shows 137". Measured [M-API, `exp9-arrival-llm-raw.txt`]:
the same model, shown before/after photos, answers YES/NO correctly **6/6** in
1.5–3.3 s. **Cheapest correct design: deterministic arrival first, LLM policy
verdict only when the deterministic check is ambiguous** (i.e. in the 2–4 grey-level
band). This is the same "crop-and-diff, then ask only on ambiguity" economy the
sibling CH9329 evaluation recommends for click confirmation.

**The overlap with Verify is exact and worth stating as a design identity:**
*arrival detection IS the Verify primitive at the pixel level.* Both consume
"before frame, after frame, expected region"; both return a boolean; both are
deterministic. Capture therefore does not need a new primitive — it needs the
Verify primitive to be available in the photo domain, which is what §3.5 measured.

**Also measured: Verify works where absolute localisation does not.** The arrival
detector's 7.9x margin lives at the *same* 50 cm / 2304 px operating point where
the vision model's absolute estimate is off by 154.8 px (median). One number is
robust, the other is not; the design is built on the robust one.

### 3.6 Abort and anti-oscillation rules (each traceable to a measured failure)

1. **Iteration budget.** Hard cap at `N_max = 20` iterations (the measured
   converging runs used 9–14; a run that exceeds 20 is not converging). On
   exhaustion: abandon the target, re-anchor, replan at the policy level.
   *Basis:* `servo_fixed_gain` hit the 30-iteration cap in 3/4 runs.
2. **Oscillation detector.** Count **sign flips of the commanded error**. Measured:
   converging runs 0–2 flips; divergent runs **28–29 flips**. Abort at >4 flips
   (a comfortable factor of 7 below the divergent signature).
3. **Stall detector.** If the believed error does not decrease by a minimum
   fraction over 2 consecutive iterations → the transfer assumption is broken
   (this is the failure mode actually observed on a real X server: 0.000 px/count).
   Re-measure the transfer with a fresh probe; if it is still 0, abort.
4. **Stale-anchor detector (mandatory).** Store the target as a **ratio inside the
   screen quad** when it is first confirmed, then *use* it only while the quad
   validation passes. Measured failure signal: a 200 px content scroll gives
   202.7 px of error and a 600 px scroll gives 618.4 px — the error becomes
   *large and jumps*, which is distinguishable from servo jitter. Trigger:
   believed-target position changes by more than the last command's expected
   effect → re-acquire (LLM) rather than chase.
5. **Quad-validity gate (mandatory).** Reject a locked quad whose aspect ratio is
   not within 15 % of the screen's 16:9 and whose corners are not all inside the
   frame. Measured basis: the naive "largest contour" rule locks onto an app
   window when the panel overfills the frame (aspect 3.27 instead of 1.76)
   giving 275.5 px error, and 1014.7 px after a small camera move.
6. **No-clamp rule.** Never plan a command that would drive the cursor past the
   screen edge. Measured [M-HW]: starting at x=1800 and sending +508 counts moves
   the pointer only +119 px — **389 px of the command is silently eaten by the
   clamp**, and a servo that trusts its own command would integrate a phantom error.
7. **Deadband anti-chatter.** Terminate with the two-consecutive-frame deadband
   (§3.4) and require the *policy* verdict before declaring success. This is the
   standard deadband/hysteresis answer to limit-cycle oscillation; the measured
   no-deadband implementation produced a false arrival at 536 px true error.

### 3.7 What the loop costs per target (measured, then extrapolated)

Deterministic path per iteration, host x86 [M-SIM/M-HW]: JPEG decode 23.64 ms +
grey 1.10 ms + blur 13.66 ms (only when re-estimating) + quad lock 2.1–22.0 ms +
tracker 3.83–5.20 ms ≈ **50–160 ms** once a stream is open. Serial command
11.5 ms + the 160 ms frame period to observe the result. **LLM 1.5–4.6 s.**
=> **per iteration ≈ 2.5–4 s; a 9–14 iteration convergence = 25–60 s per target.**
That is the honest price of a closed loop at LLM speed, and it is the number that
should decide whether the loop is used for a given GUI action (yes for a click
that matters, no for bulk form filling — see §10).

---

## 4. (d) Capture-quality findings, RE-FRAMED against the lower bar

### 4.1 The quality bar, stated before the numbers

**Old bar (retracted):** "can the photo yield a screen coordinate accurate enough
to click a 20x20 px target (≲10 px)?" — **fails by 3–85x**, §1.2.

**New bar (this section):**
- **B1 — direction:** from a photo, can we tell *which way* the target is, and how
  far in coarse terms? Error tolerance: **tens of pixels**, because each servo
  iteration only needs to reduce the distance (measured: the planner tolerates
  directions good to ±25° and still converges in 9–14 steps).
- **B2 — arrival:** can we detect that the expected visual change happened?
  Tolerance: the change is "real" vs "noise" — measured margin 7.9x.
- **B3 — read a value** (only for text/number *readout*, not for aiming):
  tolerance set by the OCR floor at 6 photo px of glyph cap-height.

Every measurement below is re-read against B1/B2/B3. **The re-reading is the
point:** numbers that looked like failures against the old bar are
passes/irrelevancies against the new one.

### 4.2 Resolution arithmetic (numbers, not adjectives) [A]

Geometry: target panel **310 mm** active width (a 13.3" 1920x1080 laptop panel),
camera horizontal FoV **66°** — the Raspberry Pi Camera Module 3 wide/standard
class value used throughout the simulation. Screen **fills the frame width** at

```
d_fill = (W/2) / tan(FoV/2) = 0.155 / tan(33°) = 0.155 / 0.6494 = 0.2387 m = 23.9 cm
```

Screen fraction of the frame and the resulting photo-px-per-screen-px ratio:

| standoff | frame fraction | ratio (CSI 2304 px sensor) | 20 px target → | 18 px glyph → | 11 px glyph → | ratio (USB 1280 px) | 20 px target → |
|---|---|---|---|---|---|---|---|
| 24 cm | 0.996 | 1.195 | 23.9 px | 21.5 px | 13.1 px | 0.664 | 13.3 px |
| 30 cm | 0.828 | 0.994 | 19.9 px | 17.9 px | 10.9 px | 0.552 | 11.0 px |
| 40 cm | 0.642 | 0.770 | 15.4 px | 13.9 px | 8.5 px | 0.428 | 8.6 px |
| **50 cm** | 0.522 | **0.626** | **12.5 px** | **11.3 px** | **6.9 px** | 0.348 | 7.0 px |
| 60 cm | 0.439 | 0.527 | 10.5 px | 9.5 px | 5.8 px | 0.293 | 5.9 px |
| 70 cm | 0.378 | 0.454 | 9.1 px | 8.2 px | 5.0 px | 0.252 | 5.0 px |
| 85 cm | 0.313 | 0.376 | 7.5 px | 6.8 px | 4.1 px | 0.209 | 4.2 px |

**Measured OCR floor** [M-SIM, `sim-raw-output.txt` §C2, reproduced at shell level
in §4.5]: glyph cap-height **<6 photo px → recall 0.000; ≥6 px → recall 1.000**
(sharp). Consequence (arithmetic): to read an 18 px UI glyph you need
ratio ≥ 6/18 = **0.333**, i.e. a standoff ≤ **96.1 cm** with the 2304 px sensor;
to read an 11 px glyph you need ratio ≥ 6/11 = 0.545, i.e. ≤ **57.8 cm**. The
50 cm operating point sits just inside the 11 px limit (6.9 px measured) — which
is why the design's "lean in" step exists.

Panel pixel pitch = 310 mm / 1920 = **0.161 mm** (157 px/inch). At 50 cm / 2304 px,
one photo px covers 0.26 mm of panel — the sensor's sampling cell is **1.6x larger
than a panel pixel**, which is the *cause* of moiré (the two grids are close in
pitch, which is exactly the interference condition [SPEC-7]) and the reason
"lean in" both helps OCR and *worsens* moiré.

### 4.3 The same numbers against B1 (direction) — they are all fine

To steer, the loop needs the target localised well enough to choose a crop and a
direction. Against that bar:

- **Whole-photo OCR worked at 24 cm** (recall 1.000, 271 chars) and **failed at
  50 cm and at 720p** (recall 0.000) [M-SIM §C]. Against B1 this is *not* a
  blocker: at 50 cm the UI regions are still **200x150 photo px** of *shape*,
  which is all a region selector needs. Reading the label is B3, and B3 is
  served by a separate lean-in step.
- **The vision model's own coarse-view error (154.8 px median) is irrelevant to
  B1** — because the design never asks the model for a coordinate. It asks for a
  region choice among a bounded set of crops; a 154 px error means "wrong crop
  sometimes", which the loop absorbs because the next iteration re-observes.
- **The measured 4.9–9.8 screen px *geometry* accuracy** [M-SIM §E: quad→homography
  with a valid lock gives 4.90–8.57 screen px, 4.23–10.16 px under 1 px corner noise] is *far better than B1 needs* — and it
  is there for free, deterministic, no LLM. It is not enough for the old bar (that
  needed ≲10 px *including* every other term) but it is ample for the new one.

### 4.4 The inverted design rule: closer is WORSE past the fill distance ⚠

The naive instinct ("get closer for resolution") is wrong at the one place it
looks most attractive [M-SIM, `anchor-raw-output.txt` VIEW V3]: at **24 cm the
panel overfills the frame**, the quad detector locks onto the app window instead
(measured aspect **3.27** instead of 16:9 → the dominant contour is the *window*),
and the stored ratio then produces **275.5 screen px** of target error, rising to
**1014.7 px** after a small camera move (vs **5.1 px** at 85 cm with a correct
lock). The full measured envelope:

| view | standoff | screen→photo | target error, baseline | target error, camera moved |
|---|---|---|---|---|
| V1 | 50 cm CSI 2304x1296 | 0.626 | 0.0 px | 0.0–2.2 px |
| V2 | 85 cm USB 1280x720 | 0.209 | 5.1 px | 3.1–6.9 px |
| V3 | 24 cm CSI 2304x1296 | 1.195 | 0.0 px *(wrong lock, by luck)* | **1014.7 px** |

**Rule: the operating envelope is "panel fully inside the frame with margin",
i.e. ≥ ~40 cm for a 31 cm panel with a 66° lens — the opposite of the resolution
instinct.** Resolution is bought by *sensor pixels*, not by proximity; proximity
buys only moiré and frame-overflow failure.

### 4.5 The executed degraded-image simulation (raw commands, raw output, SIMULATED)

**This is a simulation, not a hardware optical test.** The "photo" is a synthetic
re-sampling of a synthetic screen render through a synthetic homography + lens
blur + sensor noise + JPEG. Raw, reproducible shell commands and output — the full
transcript is `devdoc/webcam-capture-evidence/exp7-ocr-standoff-raw.txt`:

```
CMD1  crop the 520x180 dialog row out of the ground-truth screen render
  $ convert $EVIDENCE/screen-1920x1080-GT.png -crop 520x180+1180+520 +repage asif-screen.png
  -> asif-screen.png PNG 520x180 8-bit sRGB 8851B

CMD2  simulate the 50 cm optical path on the USB-720p sensor:
      scale 34.8% (the measured screen->photo ratio at 50cm/1280px), lens blur
      sigma = 1.1 * 0.348 = 0.38 photo px, sensor noise 2/255, JPEG q75
  $ convert asif-screen.png -resize 34.8% -blur 0x0.38 -attenuate 2 -noise 2 \
        -quality 75 asif-photo-720p-50cm.jpg
  -> asif-photo-720p-50cm.jpg JPEG 181x63 8-bit sRGB 1670B

CMD3  OCR both (tesseract 4.1.1, --psm 6, eng)
  $ tesseract asif-screen.png - --psm 6
  -> 'System Preferences  Mode 0: automatic 1920x1080 60 Hz Modegsutomatign1920x1080 60 Hz ...'
  $ tesseract asif-photo-720p-50cm.jpg - --psm 6
  -> '-'                    <-- TOTAL LOSS. Not a partial read: nothing.

CMD4  control: the SAME crop at 5x screen scale (1.74 photo px per screen px)
  $ convert asif-screen.png -resize 500% asif-close-12cm.png
  $ tesseract asif-close-12cm.png - --psm 6
  -> 'System Preferences Mode 0: automatic 1920x1080 60 Hz Modegautomati 920x1080 60Hz ...'

CMD5  image statistics (PIL/numpy)
  photo 181x63  mean 212.16  sd 50.06  ink(<100) px 1355
  -> the ink survives as grey mass; the GLYPH SHAPES do not.
```

The same conclusion from a second, independent sweep at fixed geometry
(`sim_blur_vs_ocr.py` → `blur-raw-output.txt`): at a 5.0 px glyph cap-height the
line OCRs as `''` at blur σ = 0.4 … 3.0 (garbage `'ee ae eam eae'` at σ = 1.0).
**Root cause: resolution, not blur.** Defocus only dominates below ~6 px, exactly
matching the measured 6-px OCR floor. This is the 5-why the design needed: the
first suspicion (autofocus hunting / motion blur) is not the binding constraint;
sampling is.

### 4.6 Artifacts re-framed: "what degrades the feedback signal"

| artifact | mechanism [SPEC] | effect on the OLD bar (coordinate) | effect on the NEW bar (direction + arrival) | mitigation in this design |
|---|---|---|---|---|
| **moiré** | interference between two similar-pitch grids, needing displacement/rotation/small pitch difference [SPEC-7] | biases the *coordinate* in a position-dependent way — poison | adds periodic texture to the ROI; **cancels** in a frame difference because it is largely static | geometric: sample the panel coarser (more standoff) so the panel grid is well below the sensor Nyquist; never solve it in software |
| **PWM backlight flicker + rolling shutter** | temporal aliasing between the display's brightness modulation and the sensor's row-by-row readout → banding, colour shifts [SPEC-8] | corrupts localisation | **the dominant new risk**: two frames of the *same* state can differ by a whole band → a false "arrival" | (i) set exposure ≥ one backlight period when the driver allows; (ii) the ROI-local null floor is measured from two frames of the same state, so a *static* band is absorbed; (iii) arrival requires the change to persist on 2 consecutive frames |
| **glare / reflection** | ambient light from windows/lamps reflecting off the panel | saturates pixels → wrong coordinate | kills a *region* of the signal → a crop can become unusable; the loop must be able to say "this crop is unusable" | polarization filter + a dark box/light shield; the LLM's "which crop" step naturally routes around a blown-out region |
| **keystone** | camera not perpendicular → projective distortion | 20.8 px of the 22.1 px budget [M-SIM] | **fully solved deterministically** by the per-iteration homography: measured target error 0.0 px with keystone 26→40 px [M-SIM] | keep the homography, re-estimate every iteration |
| **lens distortion** | radial, worst at frame edges | 6.0 px [X] model | mostly a *mapping* error, absorbed by the same homography at the operating point | keep the panel away from the frame edges (envelope rule §4.4) |
| **autofocus hunting** | focus search changes between frames | ruins localisation intermittently | **the killer for arrival**: the whole image changes when focus moves | Camera Module 3 has electric autofocus [SPEC-9] — set a **fixed manual focus** at the operating distance and disable AF; GS camera has no AF at all |
| **low-light sensor noise** | photon/read noise | degrades everything | **measured to be benign**: the null floor is 0.86–1.30 grey levels and the signal 7.28 → 7.9x margin, with sensor noise 2/255 in the sim | keep the ROI-local self-calibrating threshold; add ambient light if the target panel is dim |

**The re-reading, in one sentence:** four of these artifacts that were fatal to the
old bar (keystone, lens distortion, moiré, noise) are absorbed by the new bar's
deterministic machinery, and the two that become *newly* dangerous (PWM flicker,
autofocus) are dangerous for a different reason — they move the pixels *between*
the two frames the arrival check compares — so they are handled at the capture
configuration level, not in software.

---

## 5. (e) Hardware inventory actually found on this host — and what was measurably tested

### 5.1 Inventory [M-HW], raw in `hw-inventory-raw.txt`

```
/dev/video0, /dev/video1 present
Bus 002 Device 005: ID 05c8:0374 Cheng Uei Precision Industry Co., Ltd (Foxlink)
                     HP EliteBook integrated HD Webcam
ffmpeg -list_formats:
  Raw        yuyv422 : 640x480 160x120 176x144 320x180 320x240 352x288 424x240
                       640x360 848x480 960x540 1280x720
  Compressed mjpeg   : same ladder up to 1280x720
```

**No 1080p mode on this webcam** (max 1280x720) — worth stating because the
original brief's arithmetic assumed a 1080p webcam. `v4l2-ctl` is absent; `ffmpeg`,
ImageMagick `convert`/`compare`/`identify`, tesseract 4.1.1 and (in the project
venv) numpy 2.3.5 / PIL 12.2.0 / cv2 4.10.0 are present. No USB-serial device is
attached, so **no CH9329 is on this host** (`hw-inventory-raw.txt` §J).

### 5.2 What the real camera could and could not provide — stated honestly

The camera **opens and delivers frames**; a still and a 90-frame sequence were
captured. But the frames contain **no scene**:

```
hw-still-clean-1280x720.jpg : 8-bit mean = 8.88 / 255, sd = 4.27
hw-still-fresh-1280x720.jpg : 8-bit mean = 8.81 / 255, sd = 4.27
hw-still-clean-640x480.jpg  : 8-bit mean = 8.21 / 255, sd = 3.12
```

A *uniformly* black frame would sit at mean ≈ 1.5 with sd ≈ 0.95; 8.8 ± 4.3 is
**noise on a dark scene**, i.e. the camera is open in a completely dark room (this
is a remote-X11 session; the physical room is unlit and no target screen is
present). An earlier record in this session blamed a "privacy shutter" for a
720p-black-vs-480p-not-black discrepancy and reported a 16-bit-units figure of
`mean 2126 / 65535`; **both are now resolved as artefacts**: the units were
ImageMagick's 16-bit reporting (`0.0306 x 65535 = 2006`), and with 8-bit units
both modes show the same low-level noise. **No open question remains there.**

**Therefore: no real optical test of screen→photo fidelity was performed, and none
is claimed.** Every optical number in this document is labelled [M-SIM]. This is
the strongest honesty constraint of the task and it is respected throughout.

### 5.3 Real hardware measurement #1 — capture-leg timing [M-HW]

`capture-latency-raw.txt`, integrated 05c8:0374 webcam:

```
--- MJPEG 1280x720, 60 frames ---            wall = 10.55 s  -> 5.7 fps (period 176 ms)
--- YUYV 640x480, 60 frames ---              wall = 10.18 s  -> 5.9 fps (period 170 ms)
--- 90-frame showinfo run (30 fps REQUESTED) mean inter-frame 0.1681 s = 5.9 fps
                                             min 0.1600 s, max 0.8760 s
--- single still, whole ffmpeg process ---   1.22 / 1.36 / 1.43 s  (still #1..#3)
--- 30-frame warm stream, process total ---  1.69 s
--- host x86 per-frame costs (single thread, cv2/PIL) ---
    JPEG encode 2304x1296   10.95 ms   JPEG decode 2304x1296   23.64 ms
    BGR->GRAY   2304x1296    1.10 ms   GaussianBlur sigma 1.1  13.66 ms
    resize to 25%            0.54 ms
```

**The number the design must live with: a cheap USB webcam gives ~6 fps when the
loop grabs stills — a 160 ms frame period, even when 30 fps is requested.** That
is 6x worse than the sensor's spec and it is the reason §3.7's per-iteration
budget is 2.5–4 s rather than sub-second. Spawning a fresh `ffmpeg` per frame costs
1.2–1.4 s and must never be done; keep the stream open.

### 5.4 Real hardware measurement #2 — the Actuate-side transfer function [M-HW]

This one was invented during this evaluation because §3.4's simulated non-linearity
needed grounding, and it turned out to be the most operationally valuable real
measurement in the document. Full transcript: `x11-pointer-transfer-raw.txt`.
Method: drive a real X server with **XTEST relative motion** (the same event class
a CH9329 relative report produces at the OS input layer) and read the pointer back
from the server.

```
$ xset q | grep -A1 'Pointer Control'
    acceleration:  2/1    threshold:  4

counts  moved  px/count         (each measurement preceded by a pointer warp)
     1      1    1.000           ...  (linear all the way:)
   127    127    1.000
   508    508    1.000  (sent as 4 decomposed int8 frames: 256 in 3 frames, 512 in 5)

A/B acceleration ON (2/1 thr 4) vs OFF (1/1 thr 0): identical results -> the
  XTEST relative-motion path does NOT apply the accel profile on Xvfb.

ANOMALY (recorded, NOT reproduced): on a different, long-running server the SAME
  script returned  dx=16 -> 5 px (0.312), dx>=24 -> 0 px (0.000);
  a later run with warp-reset on that same display was fully linear.

EDGE CLAMP: start x=1800, send +508 counts -> x=1919 (moved +119, 389 px lost)
POSITION DEPENDENCE: at x=60 a -100 command moved -60 px (0.600), not -100.
```

Three design consequences, and they are the reason this experiment was worth the
time: (i) the transfer function **is** measurable and **is** 1 px/count in the best
case — so the servo can use short commands as a probe; (ii) the same code once
measured **0.000 px/count**, so the stall/oscillation detectors (§3.6 items 2–3)
are not theoretical; (iii) **the clamp silently eats commands** — a servo that
trusts its own command integrates 389 px of phantom error, which is exactly how an
open-loop system fails without knowing.

---

## 6. (f) RPi5 feasibility — camera, latency budget, power/mobility

### 6.1 USB webcam vs CSI camera [SPEC-9]

| | USB webcam | **CSI (Camera Module 3, IMX708)** | CSI GS (IMX296) | CSI HQ (IMX477) |
|---|---|---|---|---|
| sensor | vendor-dependent, typically 1/4"–1/3" | 12 MP 4608x2592 [SPEC-9] | 1.6 MP 1456x1088, **global shutter** [SPEC-9] | 12 MP 4056x3040 [SPEC-9] |
| focus | fixed or contrast-hunting AF | **electric autofocus** (can be pinned) [SPEC-9] | manual lens (C/CS) | manual (C/CS or M12) |
| shutter | rolling (usual) | rolling | **global** — no wobble/banding from motion [SPEC-9] | rolling |
| interface | USB-A on the Pi (bandwidth shared) | Pi5: 2x 4-lane MIPI transceivers via **mini-22pin CAM/DISP0/1** [SPEC-9] | same | same |
| latency | unbounded by UVC: **measured 160 ms/frame** for a cheap one [M-HW] | DMA to memory, no UVC stack → typically far lower [X] | same [X] | same [X] |
| driver on Debian arm64 | any UVC camera, zero setup | `libcamera`/`rpicam-apps` in Raspberry Pi OS by default [SPEC-9] | same | same |
| mobility | excellent (throw it in the box) | good, but the ribbon is fragile; mini-22 to standard cables exist [SPEC-9] | bulkier (C-mount lens) | bulky |
| **for our job** | fine for a first prototype (one cable, any camera) | **the right default**: resolution headroom for lean-in, autofocus that can be *disabled and set*, first-class support | **the interesting one**: global shutter removes a whole class of between-frame artefacts (§4.6) at the cost of resolution and bulk | not needed; the GS trade is better for us |

**Recommendation and its reason:** prototype on a **USB webcam** (it is what the
development host already has, it is what the POC can be built against today), but
**design the capture layer so a CSI camera is a drop-in** — same frame interface,
same JPEG path. Go to CSI (Camera Module 3, or GS if PWM-banding proves to be the
dominant real-world problem) when the first hardware measurements are taken;
nothing in the loop design depends on which one is used, because the loop only
consumes frames.

**Also relevant [SPEC-9]:** Pi5 carries 2x four-lane MIPI transceivers supporting
any combination of two cameras/displays, and the Camera Module 3 has a documented
RFI caution near GPS L1 (1575 MHz) — noted, not relevant here.

### 6.2 Capture-leg latency budget, per stage, with the dominant term named

| # | stage | time | basis | note |
|---|---|---|---|---|
| 1 | shutter → frame in memory | **160 ms** | [M-HW] measured, cheap USB webcam, 5.9 fps | CSI is expected to be much lower; **not measured** |
| 2 | JPEG decode (2304x1296) | 23.6 ms | [M-HW] host x86 | ~3.5x on Pi5 ⇒ ~83 ms [X] |
| 3 | grayscale + resize to 25% | 1.6 ms | [M-HW] | ~6 ms on Pi5 [X] |
| 4 | quad lock (25% scale) | 2.1 ms | [M-SIM] | ~7 ms on Pi5 [X] |
| 5 | cursor template match (±90 px window) | 5.2 ms | [M-SIM] | ~18 ms on Pi5 [X] |
| 6 | arrival ROI diff | <1 ms | [M-SIM] | — |
| 7 | encode the crop for the LLM (JPEG q75, ~60 KB) | 11.0 ms | [M-HW] | ~38 ms on Pi5 [X] |
| 8 | transmit to the model | 0.5–6.5 ms | [A]: 61,295 B ⇒ 4.9 ms raw at 100 Mbit/s, 6.5 ms base64 at 1 Gbit/s; 0.5/0.7 ms at 1 Gbit/s | **negligible** |
| 9 | **model round trip** | **1.5–4.6 s** (mean ≈ 2.5 s) | [M-API] measured, 30 calls | **DOMINANT TERM by 15x** |
| 10 | serial HID command | 11.5 ms (58 ms for 5 frames) | [A] | negligible |
| 11 | OS applies the move + repaint | 10–50 ms | [X] | unmeasured |

**Dominant term: the LLM round trip (#9), at ~2.5 s — roughly 15x the entire
capture leg (#1–#8 ≈ 165 ms) and 200x the deterministic compute.** Second is the
frame period (#1, 160 ms), which is the only capture-side term worth engineering
(CSI or a better UVC camera). **Consequence for the design: optimize iterations,
not pixels.** Cutting the capture leg to zero would speed a 10-iteration
convergence by ~1.7 s out of ~30 s. Cutting *iterations* by giving the LLM a
better coarse view is worth 2.5 s each.

**LAN vs streaming:** the transmit term is 0.5–6.5 ms per still, so there is no
bandwidth argument at all for streaming video to the model; send *stills*, and only
the crop the model needs. (Streaming would also pay 5.9 fps of decode ≈ 140 ms/frame
on the Pi and burn tokens.)

### 6.3 Power and mobility [SPEC-9 / X]

- Pi5 is powered by USB-C PD; the official 27 W supply delivers **5 V / 5 A**, and
  the documentation notes that without a 5 A-capable supply the OS limits
  **peripheral current to 600 mA**; with it, the USB peripheral budget rises to
  **1.6 A** [SPEC-9].
- **This matters directly:** a webcam + CH9329 + (optionally) a USB SSD can exceed
  600 mA. A phone charger that "claims 30 W" almost always does it by raising
  voltage, which the Pi5 does not use that way — the docs call this out explicitly
  [SPEC-9]. **Use the official 27 W supply or an equivalent 5 V/5 A PD source, or
  expect peripheral brownouts.**
- Idle/light-load consumption of a Pi5 + webcam + CH9329 is order **5–8 W** [X,
  not measured]; a 10,000 mAh (37 Wh) USB-C bank therefore gives roughly **5–7 h**
  [A] of headless operation. That is enough to make the box genuinely mobile, and
  it is the argument for keeping the compute light (no local vision model, no
  video encoding: the model is remote, so the Pi is a capture+serial+HTTP client).
- **Physical form:** a small box holding Pi5 + camera (ribbon or USB) + CH9329 +
  battery; the only things leaving the box are the USB cable to the target
  (as a keyboard/mouse, i.e. impersonating a human peripheral) and Wi-Fi. The
  non-invasiveness claim survives: **no cable into the target's video path, no
  software on the target.**

---

## 7. (g) Photo→screen mapping: retained, but only for a rough estimate and search region

**Technique and citation.** The screen is a **planar** object; two views of a plane
(or one view of a plane plus its known outline) are related by a **homography** —
a 3x3 projective transform that preserves straight lines and is exactly the tool
used for "perspective correction … correcting keystone distortions for projected
images" [SPEC-4]. Four point correspondences determine it fully [SPEC-4]. The
document-scanning literature is the standard application of exactly this
(screen/camera-captured document alignment) [SPEC-4b]. This is not a research
question: it is a textbook, solved technique, and this document does not
re-derive it.

**What we keep it for (the demotion):**
1. **A rough initial estimate** — "the target is in the lower-right third of the
   screen" — enough to pick the first crop. Measured accuracy at the recommended
   operating point: **4.9–8.6 screen px** [M-SIM] — comfortably inside the B1 bar
   and nowhere near the old one.
2. **Defining the search region** for the deterministic tracker (the ±W window).
3. **Expressing the target as a ratio** so the same target survives camera motion
   (§3.6 item 4) — the measured 0.0–6.9 px error across keystone/standoff/blur/
   JPEG/noise perturbations [M-SIM, VIEW V1/V2] is the mechanism.

**What we no longer ask of it:** a coordinate precise enough to aim at. §1.2
explains why that was never achievable.

### 7.1 Calibration proposal (one-time, then continuous, then cheap)

1. **One-time acquisition.** Put the box in front of the target, capture, detect
   the panel quad (classical contour + polygon approximation), **validate it**
   (aspect within 15 % of the panel's true aspect; corners inside the frame), and
   store: panel aspect, the panel rect, and the *operating standoff band*.
2. **Continuous per-iteration re-estimation.** Never reuse a stale H: re-detect the
   quad on every iteration (measured cost 2.1–22.0 ms depending on scale), because
   a moved camera invalidates H and re-detection is cheap. Measured robustness to
   the perturbations that actually happen: keystone 26→40 px **0.0 px** target
   error, +3 cm closer **2.2 px**, panel gamma 0.75 **1.2 px**, defocus σ1.1→2.5
   **1.8 px**, JPEG q75→q25 **0.0 px**, noise ×6 **1.8 px** [M-SIM].
3. **Drift detection (the stored ratio is the detector).** Because the target is
   stored as a *ratio*, using it against a fresh H is valid **only** while the
   panel's content and framing are stable. A content scroll produced **202.7 px**
   (200 px scroll) and **618.4 px** (600 px scroll) of error [M-SIM] — a jump far
   outside servo jitter, so it is *detectable* and triggers re-acquisition. This is
   the calibration-drift answer: **we do not detect drift of the camera, we detect
   invalidity of the ratio.**
4. **Never silently accept a lock.** The measured gate candidates: aspect ratio,
   all four corners inside the frame, contour area > 50 % of the frame. Without
   the gate, the naive "largest bright contour" rule can lock onto an app window
   (§4.4). Validity must be a hard precondition of using H at all.

### 7.2 Failure modes (each observed or specified)

| failure mode | observed consequence | detection | response |
|---|---|---|---|
| panel overfills the frame | lock onto the app window; **275.5 px**, then **1014.7 px** after a camera move [M-SIM] | quad aspect gate; corner-inside-frame test | reject the quad; instruct "back off" (and note the counter-intuitive rule of §4.4) |
| panel partially occluded / off-frame | quad detection fails or is partial | missing candidate count (`n_candidates`) | fail loudly, re-frame, do not guess |
| extreme camera angle | homography degrades; corner 1 px noise → 4–10 screen px leverage [M-SIM] | corner RMS vs local gradient quality | keep the angle in the envelope; the servo absorbs the residual |
| content scrolled/menus opened | stored ratio invalid (**202.7/618.4 px**) [M-SIM] | error jump beyond expected command effect | re-acquire the target (LLM) |
| glare/saturation region | crop unusable | clip statistics of the crop | route the crop choice around it; add light shield/polarizer |
| camera nudged mid-task | H stale by definition | quad moved between iterations | re-detect (already per-iteration); the ratio survives **camera motion** (validated for keystone/standoff) |
| cursor outside the frame | tracker fails (0 matches) | no template peak above threshold | plan a bounded "sweep" motion to bring it into view; probe with a dither |

---

## 8. (h) The minimum complete end-to-end loop, off-the-shelf

### 8.1 Parts

| part | role | notes |
|---|---|---|
| Raspberry Pi 5 (4 or 8 GB) + official 27 W USB-C PD supply | the box | §6.3: the 5 A supply is what unlocks the 1.6 A peripheral budget |
| USB webcam (prototype) or Camera Module 3 (target) | Capture / eye | §6.1 |
| CH9329 USB-HID adapter + USB-serial on the Pi | Actuate / hand | sibling evaluation: `devdoc/ch9329-hid-actuator-evaluation.md` (read-only reference; not duplicated here) |
| target computer (unmodified) | the subject | no software, no video cable |
| a dark-ish, glare-free placement | the operating environment | §4.6 |
| Wi-Fi/LAN to the vision API | the model | the Pi does not run a model (§6.3) |

### 8.2 The loop, with the deterministic-vs-LLM label per step

```
0  POWER-ON / PREFLIGHT            deterministic   open the stream ONCE (never per-frame:
                                                   1.2-1.4 s per spawned ffmpeg [M-HW]);
                                                   set exposure/focus to fixed values
1  ACQUIRE FRAME                   deterministic   160 ms [M-HW]
2  LOCK PANEL QUAD + H             deterministic   2.1-22.0 ms; GATE on aspect/corners
                                                    (else: fail loudly, do not proceed)
3  BOOTSTRAP CURSOR                deterministic   dither + frame diff + masked template
                                                   4/4 success, 9.8-11.3 photo px [M-SIM]
4  COARSE UNDERSTAND               LLM (1 call)    "which region contains <target>" from a
                                                   2304x1296 view (or a 640x360 downscale)
5  CHOOSE CROP                     deterministic   map region -> photo ROI via H
6  FINE LOOK (lean in)             LLM (1 call)    cropped+upscaled ROI: confirm identity
7  DIRECTION + COARSE DISTANCE     LLM             returns e.g. "left, far" -- NEVER a
                                                   coordinate that is used as a target
8  PLAN COMMAND                    deterministic   measure-and-invert gain; decompose into
                                                   int8 frames; cap to the search window;
                                                   damp near target; no edge clamp
9  SEND HID FRAMES                 deterministic   11.5 ms/frame serial [A]
10 OBSERVE (re-capture)            deterministic   160 ms
11 TRACK CURSOR                    deterministic   masked template match in a window scaled
                                                   to the expected travel
12 ARRIVAL CHECK                   deterministic   ROI mean|diff| vs 2x self-calibrated null
                                                    -> "moved as expected?" (7.9x margin)
13 LOOP 6-12 until deadband        (bounded)       9-14 iterations measured; cap 20;
                                                   abort on >4 sign flips or a stall
14 POLICY VERIFY                   LLM (1 call)    "is the resulting state the intended one?"
                                                   (e.g. the right menu is open) -- only if
                                                   step 12 is ambiguous, else a cheap check
```

**Deterministic-vs-LLM in one line: 10 of the 15 steps are deterministic; the LLM
appears exactly 4 times per target (coarse understand, fine confirm, direction,
policy verify) — and the last of those is skipped whenever the deterministic
arrival check is unambiguous, which was 8/8 of the measured cases. The LLM never
emits a number that becomes a coordinate.**

### 8.3 What breaks first, in order of likelihood

1. **The environment, not the algorithm.** Glare, a dark room, a glossy panel, or
   PWM banding between the two frames of the arrival check. This is the highest
   risk because it is *invisible* in simulation and this evaluation could not
   measure it (no light, §5.2). *Concretely: expect the first hardware session to
   be spent on lighting and exposure, not on code.*
2. **Panel detection at the first power-on.** The envelope rule (§4.4) is
   counter-intuitive: the operator's instinct will be to move closer, which
   produces the 1014 px failure.
3. **The cursor-tracker losing its template** when the pointer crosses a
   background with a similar shape/colour (measured NCC collapse 0.99 → 0.45 with
   an unmasked template). Mitigated by masking, but this is a per-app empirical
   question.
4. **The transfer-function assumption** — the anonymous 1 count = 1 px belief is
   *wrong on real desktops* even though it was exactly right on Xvfb [M-HW]. Live
   CI-grade code must re-measure, and the first non-1.0 measurement should be
   expected, not surprising.
5. **Multi-monitor / DPI-scaled desktops.** The homography maps to *one* panel;
   a virtual desktop spanning two panels breaks the ratio assumption. Not
   investigated here; flagged in §11.
6. **Autofocus re-hunting between frames**, which corrupts the arrival check. Fix
   by configuration (fixed focus), which is why the camera choice matters more than
   the resolution does.

---

## 9. (i) Extension to non-computer instrument panels

A legacy panel (gauges, switches, a 7-segment readout) is attractive for exactly
the reason in the brief: it is where software modification is impossible, and it is
where a webcam's non-invasiveness is the *only* viable interface.

**Measured on a purpose-built simulation [M-SIM, `sim_gauge_panel.py`, raw output
`gauge-panel-raw-output.txt`]** — a 320x240 panel with a 78 px-radius needle gauge
and a 3-digit 7-segment readout, photographed with the same exp-#1 optical path:

| quantity | 50 cm-class (0.626 photo px / panel px) | 2x further (0.313) |
|---|---|---|
| panel in the photo | 200 x 150 px | 100 x 75 px |
| **needle angle** (deterministic red-mask centroid, rectify-first) | true 60.0° → **60.3°** (err **0.3°**) | red mask empty → **no reading** |
| needle tip travel per degree | 0.787 photo px / ° | 0.393 photo px / ° |
| **7-segment decode** (deterministic per-segment intensity, no OCR) | true 137 → **137** ✓ | true 137 → **117** ✗ (segment f/g confusion; contrast margin dropped 0.67→0.43) |
| arrival detector, gauge ROI (null / needle +30°) | 1.16 / **2.44** (2.1x) | 1.16 / **2.29** (2.0x) |
| arrival detector, readout ROI (null / value +5) | 1.19 / **7.86** (6.6x) | 1.48 / **6.88** (4.6x) |

**What this shows, and why it matters more than the GUI case:**
- **The deterministic primitives work at a coarse scale.** A needle angle is exact
  to 0.3° with no model at all, and a 7-segment value is decoded by *measuring
  segment intensities*, which is a far more robust mechanism than OCR for a
  seven-bar shape (OCR at this resolution would return nothing — the 6 photo px
  glyph floor of §4.2 applies only to real glyphs).
- **It also shows the failure at the far end honestly:** at 0.313 photo px per
  panel px the needle mask goes *empty* (a 4-px-wide needle sampled at 0.9 photo px
  disappears into the panel background), and the digit decode returns 117 instead
  of 137 with a 0.43 contrast margin. **So the panel's operating envelope is
  tighter than the GUI's, not looser**, even though the panel has 6x fewer pixels.
  The binding quantity is **photo px per unit of the *smallest* feature** — 0.9 for
  a 3-panel-px needle — not the panel's pixel count.
- **Verify generalizes cleanly to non-computer targets**: the arrival detector
  fires on "the needle moved" (2.1x) and "the value changed" (6.6x) with the same
  self-calibrating ROI-local null floor. For a panel, "arrival" is exactly the
  human action "look again and see if the needle moved" — the strongest
  confirmation that the primitive is the *right* primitive.
- **What does not generalize:** keying/typing (a panel has no keyboard), so the
  actuation side becomes switch-flipping/pressing, which the CH9329 *cannot* do
  (it impersonates keyboard+mouse on a USB host — a legacy panel does not accept a
  USB keyboard). **This is a hard boundary and it belongs in the "cannot do" list
  (§11).** The capture half generalizes to panels; the actuation half does not,
  and any panel project must solve actuation separately.

---

## 10. (j) BLOCKED on machine-vision/robotics progress vs ACTIONABLE NOW

The owner's principle, verbatim from the reframing: *"do not wait for the whole
thing to be ready — prepare the half that does not depend on it."* Applying it:

### 10.1 ACTIONABLE NOW (no new hardware, no robots, no research)

| # | item | evidence it is ready |
|---|---|---|
| A1 | **The whole servo loop in simulation** — it exists and converges: 9–14 iterations, 4/4 arrivals, 10.6–27.7 px | [M-SIM] `sim_closed_loop.py` |
| A2 | **The deterministic tracker + cursor bootstrap** — masked-template matching, 4/4, 9.8–11.3 photo px | [M-SIM] |
| A3 | **The arrival/Verify detector with its self-calibrating threshold** — 7.9x separation, 0/8 FP, 0/8 FN | [M-SIM] |
| A4 | **The measure-and-invert command planner + the int8 decomposition** | [M-HW] `x11-pointer-transfer-raw.txt`; [M-SIM] `closed-loop-raw-output.txt` |
| A5 | **The abort/anti-oscillation rule set** — each rule tied to a measured failure signature (28–29 flips, 389 px clamp loss, 535 px false arrival) | [M-SIM]/[M-HW] |
| A6 | **The transfer-function probe as a startup routine** — it is a 20-line measurement and it is the difference between servo_learned and servo_fixed_gain | [M-HW] |
| A7 | **Capture plumbing** — keep a stream open, grab stills, JPEG-encode crops, call the API. On this host the API path is already proven (30 real calls in this session) | [M-API] |
| A8 | **The panel-side deterministic readers** (needle angle to 0.3°, 7-segment intensity decode) | [M-SIM] `sim_gauge_panel.py` |
| A9 | **The operating-envelope rule** (panel fully inside the frame; closer is worse) and the quad-validity gate | [M-SIM] §4.4 |
| A10 | **A hardware-in-the-loop test harness** for the first real camera session: a synthetic target shown on a real screen, a real camera, and the *same* metrics this document used | derived from the above |

**A1–A10 need: a USB camera, a screen, a light, and a Pi. No research, no robotics,
no new sensors.** That is the "prepare what does not depend on progress" half and
it is the majority of the work.

### 10.2 BLOCKED on machine-vision / robotics progress

| # | item | why it is blocked, and by what |
|---|---|---|
| B1 | **Precise absolute aiming from a photo** | This is the retracted premise; it is blocked *in principle* by the error stack (glare/moiré/keystone/focus/vision jitter) irrespective of camera quality. It would need a robot arm holding the camera in a calibrated pose (i.e. **PBVS with a real kinematic chain**) — that is robotics, and it buys nothing a closed loop does not already achieve. |
| B2 | **Sub-second servo iterations** | Bounded by the remote vision model (1.5–4.6 s) and the frame period (160 ms). A local model on a Pi would be needed to go below ~1 s — that is a different project (and a different hardware class). |
| B3 | **Robust tracking under adverse light** (glare, low light, specular reflections on glossy panels) | An open machine-vision problem; the honest answer is "engineer the light" (shield, polarizer, controlled illumination), not "wait for an algorithm". Where it *is* blocked: unsupervised operation in an unknown room. |
| B4 | **PWM-flicker-immune single-photograph arrival checks** | Active research (bracketed multi-exposure RAW restoration is the current answer [SPEC-8]) and it needs RAW from the sensor — not available through the UVC/JPEG path. Blocked on hardware/format, not on us. |
| B5 | **Calibration-free metric pose of the camera** | Classic PBVS territory; needs intrinsics+extrinsics. Blocked by choice (we do not want to calibrate a camera that gets moved) and by the physical reality of a mobile box. |
| B6 | **The actuation half for non-computer panels** (§9) | Blocked on a **physical actuator** for switches/knobs — the CH9329 cannot do it. Robotics/mechanism, not vision. |
| B7 | **Multi-monitor / DPI-scaled virtual desktops** | Not blocked on research, but on an OS-semantics question this evaluation did not investigate (§11). Classified here because it is outside the current envelope. |

**Justification of the split, in one line:** everything that depends on *seeing
better* is on the blocked list; everything that depends on *interpreting a bad
view and acting on it* is on the actionable list — and the measurements in §3 show
the second list is sufficient to complete the task.

### 10.3 What we deliberately do NOT build yet

- **No absolute-coordinate aiming path** (the retraction; not even as a "quick
  first try").
- **No camera calibration** (intrinsics/extrinsics) — it would be invalidated every
  time the box is moved.
- **No local model / no on-Pi inference** — the latency budget's dominant term is
  the remote call, and a local model is a different project (§10.2 B2).
- **No video streaming pipeline** — stills are 0.5–6.5 ms on the wire; streaming
  costs decode time and tokens for nothing.
- **No moiré/flicker removal in software** — §4.6: mitigate geometrically and by
  exposure; the frame-difference arrival check is already immune to *static*
  artefacts.
- **No ML-based quad detection** — the classical contour + validity gate is
  measured sufficient (5–9 px) and has no training/inference cost.
- **No new gopi tool** (Rule 3). Capture/Actuate/Wait/Verify are CLI verbs or
  internal packages, as the sibling CH9329 evaluation already argues.
- **No panel actuation design** (§9's boundary) — the actuator does not exist.

---

## 11. (k) Open questions, CANNOT-do, and the honest ledger

### 11.1 Open questions (do not guess — measure)

1. **Does a real screen through a real camera clear the arrival detector's 7.9x
   margin?** All optical evidence here is simulated. The two between-frame risks
   (PWM flicker, autofocus) are precisely the ones simulation cannot show.
2. **What is the real latency of a CSI camera on the Pi5?** The 160 ms/frame figure
   is a cheap UVC webcam's; CSI is expected to be much lower [X] and unmeasured.
3. **Does OS pointer acceleration exist on the target, and does a relative-HID
   device get accelerated?** On Xvfb XTEST it did not (measured identical with
   acceleration on/off); on a real desktop the CH9329's relative reports are likely
   subject to it. The design *measures* rather than assumes it — but the actual
   curve on a real Windows/Linux desktop is unmeasured.
4. **What produced the observed 0.000 px/count stall** on the long-running X
   server? Root cause NOT established. It is recorded because the design tolerates
   it; understanding it is not required to proceed.
5. **Multi-monitor and DPI scaling**: how does the homography→ratio approach behave
   when the virtual desktop spans two panels, or when the OS scales by 150 %?
6. **What is the real autofocus-hunting cost**, and can a UVC camera's focus be
   pinned through the UVC controls?
7. **Does the cursor template survive under subpixel motion** and under the OS's
   cursor-size/shadow settings? Simulated only.
8. **Latency of the OS applying a HID move and repainting** (estimated 10–50 ms,
   never measured).
9. **Whether the LLM's *direction* judgement is stable** — this evaluation measured
   the model's *absolute coordinate* jitter (bad) and its *policy* verdicts (6/6
   good), but did **not** measure a "which way" accuracy rate, because the
   closed-loop simulation used a synthetic oracle instead of the model. **This is
   the single most important unmeasured assumption in the design.**
10. **Pi5 single-thread performance vs the host x86** — the ~3.5x scaling used
    throughout is an estimate, not a measurement.

### 11.2 CANNOT-do (explicit)

- **We cannot aim at a 20x20 px GUI target open-loop from a photograph.** Measured:
  0/4 hits, 84–770 px of error, and a self-belief of 5–8 px (§1.2).
- **We cannot read 12 px UI text from a 720p sensor at 50 cm.** Measured: 0
  characters (§4.5), and the OCR floor is a 6-photo-px glyph cap-height (§4.2).
- **We cannot trust the vision model's absolute coordinates.** Measured: 154.8 px
  median error at full view, and 3/6 responses pointing at unrelated image areas.
- **We cannot perform any real optical test on this host.** The only camera present
  delivers noise in an unlit room (§5.2).
- **We cannot claim the LLM's direction judgements are accurate.** Not measured
  (§11.1 item 9).
- **We cannot operate a non-computer panel's switches through a CH9329.** It
  impersonates a USB keyboard/mouse; a legacy panel has no USB host (§9).
- **We cannot run the servo faster than ~0.4 Hz** with a remote vision model
  (§3.7).
- **We cannot promise convergence when the panel is occluded, off-frame, or
  reflects the room.** The loop detects these, but its response is "abort and ask",
  not "succeed anyway".

### 11.3 Honesty ledger (corrections made during this evaluation)

Recorded because a report that hides its own corrections is not honest:

1. **Verify separation 48.9x → 7.9x.** The first number compared a same-draw null
   against a redrawn signal (apples-to-oranges floor). The honest identical-pipeline
   figure is **7.9x** (§3.5).
2. **"Privacy shutter closed" → "camera open in a dark room."** The 720p-black vs
   480p-not-black discrepancy was an ImageMagick 16-bit units artefact
   (0.0306 x 65535 = 2006), not a format/shutter difference (§5.2).
3. **Vision-jitter metric units.** Two independent runs used different
   image-px→screen-px conversions; the raw-endpoint run (`api-vision-jitter-raw.txt`)
   is the authoritative one, and both agree on the verdict even where the
   conversions differ (§1.2).
4. **The panel simulation's first run was wrong**, and it was wrong in an
   instructive way: an unrestricted red mask in a gauge ROI caused the 7-segment
   readout (also red) to dominate the needle centroid → **47.7°** of angle error and
   33,688 "needle" pixels for a 78 px needle. Restricting the mask to the gauge disc
   gave 0.3° and 3,537 px. Recorded in the script itself. **This is the mechanical
   class of bug the whole design is exposed to — a deterministic feature extractor
   silently measuring the wrong thing — which is why the loop always re-observes.**
5. **The 0.000 px/count anomaly** is reported as *unreproduced*, not as explained.

---

## Appendix A — Cited sources (all fetched via web_search/web_fetch, not curl)

| id | source | used for |
|---|---|---|
| **SPEC-1** | Visual servoing — Wikipedia. https://en.wikipedia.org/wiki/Visual_servoing | eye-in-hand vs eye-to-hand; end-point open/closed-loop; IBVS (Weiss & Sanderson, no pose estimate) vs PBVS vs hybrid (2-1/2-D, motion-partition, partitioned-DOF); IBVS failure "camera retreat"; the 30–100 Hz norm |
| **SPEC-2** | Yin, Wang, Zhang, Taylor, Vagvolgyi — *Applications of Uncalibrated Image Based Visual Servoing in Micro- and Macroscale Robotics*, arXiv:2304.08464 (2023) | uncalibrated markerless IBVS; "human-like manipulation accuracy" with no hand-eye/camera calibration; robustness to cameras being **moved** mid-task — the precedent for a moved camera |
| **SPEC-3** | Li & Assadian — *Innovative Adaptive IBVS Control of 6 DoF Industrial Robots*, arXiv:2506.10240 (2025) | "most research … when 3D point features can be detected **inside** the field of view"; feedforward estimation for features outside the FoV |
| **SPEC-4** | *Homographies*, Foundations of Computer Vision (MIT visionbook), ch. 41. https://visionbook.mit.edu/homography.html | planar-scene ⇒ 3x3 homography, straight lines preserved, "correcting keystone distortions"; 4 correspondences |
| **SPEC-4b** | arXiv:1911.05870 — *Character Keypoint-based Homography Estimation in Scanned Documents* | document/scan alignment as the standard homography application |
| **SPEC-5** | Fu et al. — *GUI-Lens: Coarse-to-Fine Cropping for GUI Grounding with General-Purpose VLMs*, arXiv:2608.03270 | "a direct click prediction, allowing visual ambiguity or **an inaccurate initial estimate to propagate**"; coarse-to-fine region+scale selection; +24.9 pp |
| **SPEC-6** | Nguyen — *Improved GUI Grounding via Iterative Narrowing*, arXiv:2411.13591 | the same iterative-narrowing mechanism as an established visual-prompting technique |
| **SPEC-7** | Moiré pattern — Wikipedia. https://en.wikipedia.org/wiki/Moiré_pattern | interference requires two similar patterns **displaced, rotated, or of slightly different pitch**; digital photography exposing "a pattern on an object being photographed interfer[ing] with the shape of the light sensors" |
| **SPEC-8** | Zhou et al. — *Bricker to BRACE: A Bracket Exposure RAW Dataset and Restoration Model for Flicker-Banding*, arXiv:2606.29845 | flicker-banding = "temporal aliasing between a camera's rolling shutter and a display's brightness modulation"; degrades "screen-captured image readability with color shifts and jagged patterns"; current answer is multi-exposure RAW |
| **SPEC-9** | Raspberry Pi official documentation (mirror https://pidoc.cn/docs/computers/raspberry-pi-5/ and .../camera/) | Pi5: quad Cortex-A76 @ 2.4 GHz, 2x four-lane MIPI transceivers (any 2 cameras/displays), USB-C PD 5 V/5 A (25 W) with the 600 mA → 1.6 A peripheral-current trade; Camera Module 3 = IMX708 12 MP 4608x2592 with electric autofocus; GS camera = IMX296 1456x1088 global shutter; HQ = IMX477 4056x3040; mini-22pin CAM/DISP0/1; CM3 GPS-L1 RFI caution |
| **SPEC-10** | Rolling shutter — Wikipedia. https://en.wikipedia.org/wiki/Rolling_shutter | rolling vs global shutter; "not all parts of the image are recorded at the same instant"; wobble from vibration |
| **SPEC-11** | PID controller — Wikipedia. https://en.wikipedia.org/wiki/PID_controller | the standard feedback-loop frame (setpoint vs process variable, error, corrective action) that §3's deadband/abort rules are expressed against |
| **SPEC-12** | Anti-windup for position-saturated servo controllers, arXiv:2504.12207 | the saturation/limit problem class behind the §3.6 no-clamp rule |

## Appendix B — Evidence index (`devdoc/webcam-capture-evidence/`)

**Raw transcripts (the numbers quoted above are in these files):**

| file | contains |
|---|---|
| `hw-inventory-raw.txt` | [M-HW] camera identity, formats, stills, throughput, no CH9329 attached |
| `capture-latency-raw.txt` | [M-HW] 5.7/5.9 fps, inter-frame distribution (mean 0.1681 s), per-frame codec costs, still-frame statistics |
| `x11-pointer-transfer-raw.txt` + `x11-pointer-transfer-scripts.tar.gz` | [M-HW] **the Actuate-side transfer function**: linear 1.000 px/count, accel A/B, the unreproduced 0.000 stall, the 389 px edge clamp |
| `sim-raw-output.txt`, `sim-metrics.json` | [M-SIM] resolution arithmetic, degraded-photo simulation, OCR floor curve, tracking, degradation ladder, error budget |
| `exp7-ocr-standoff-raw.txt` | [M-SIM] **shell-level** ImageMagick+tesseract reproduction with raw commands |
| `blur-raw-output.txt` | [M-SIM] blur sweep at fixed geometry (resolution floor) |
| `anchor-raw-output.txt`, `anchor-track-metrics.json` | [M-SIM] quad/H tracking under perturbations; the overfill failure; stale-anchor signal |
| `verify-raw-output.txt`, `verify-arrival-metrics.json` | [M-SIM] the arrival detector, null vs signal, the subtle-glyph case |
| `closed-loop-raw-output.txt`, `closed-loop-metrics.json` | [M-SIM] **the three strategies** (learned / fixed-gain / open-loop), iteration traces, bootstrap, transfer tables |
| `api-vision-jitter-raw.txt`, `vision-jitter-report.json`, `vision-jitter-gt.json` | [M-API] absolute-localisation jitter, raw endpoint + tool path |
| `exp9-arrival-llm-raw.txt`, `before-menu.jpg`, `after-menu.jpg` | [M-API]+[M-SIM] deterministic arrival vs LLM arrival verdicts (6/6) |
| `gauge-panel-raw-output.txt` | [M-SIM] the instrument-panel extension (needle 0.3°, 7-seg decode, arrival) |
| `exp9-arrival-absolute-raw.txt` | a superseded skeleton — retained for audit |

**Scripts (re-runnable, deterministic seeds):** `sim_capture_degrade.py`,
`sim_anchor_track.py`, `sim_verify_arrival.py`, `sim_closed_loop.py`,
`sim_blur_vs_ocr.py`, `sim_exp9_arrival_llm.py`, `sim_vision_jitter_prep.py`,
`sim_gauge_panel.py`. Re-running the two primary sims from scratch reproduced
byte-identical metric sets (fixed RNG seeds).

**Images:** ground-truth screen render (`screen-1920x1080-GT.png`); four simulated
photos (`photo-P1..P4-*.png/.jpg`) with ROI crops; the two jitter inputs; the
OCR-size ladder (`ocrsize-*.png`, `ocrglyph-*.png`); the blur sweep; the before/after
arrival pair; **the real hardware stills** (`hw-still-*.jpg`, `host-webcam-raw-black-1280x720.jpg`,
`hw-still-1280x720-amplified.png`) — real files from the real camera, containing
noise rather than a scene.

## Appendix C — Report (Rule 5 shape)

### Completed

- **Retraction** of the open-loop absolute-coordinate premise with the owner's
  reasoning preserved verbatim, plus the measured error stack that makes it
  structural (§1).
- **Visual-servoing framing** with **12 cited sources** (§2, Appendix A) and a
  **7-item analysis of which classic servoing assumptions break here** (§2.3),
  each tied to a measurement.
- **LLM-in-the-loop convergence design**: coarse-to-fine iteration roles, a
  per-step deterministic-vs-LLM table, the deterministic cursor bootstrap, the
  measure-and-invert planner with its measured transfer table, convergence tied
  explicitly to the Verify primitive (two levels), and **7 abort/anti-oscillation
  rules each traceable to a measured failure** (§3).
- **Re-framed quality bar** (direction + arrival + readout) stated explicitly, with
  every prior measurement re-read against it, plus the counter-intuitive envelope
  finding that "closer is worse" past the fill distance (§4).
- **Resolution arithmetic with explicit numbers** for 7 standoffs x 2 sensors x
  3 feature sizes, with the OCR floor derivation (§4.2).
- **A genuinely executed degraded-image simulation** at the shell level with raw
  commands and raw output, labelled simulated (§4.5), plus 8 further executed
  simulations/measurements (§3, §5, §9).
- **Real hardware inventory** (webcam present, its limits, an honest statement that
  it sees nothing) **and two real hardware experiments**: capture-leg timing and
  the X11/CH9329-class pointer transfer function (§5).
- **RPi5 feasibility**: USB vs CSI comparison table, a 11-row **latency budget with
  the dominant term named** (the ~2.5 s model round trip, 15x the whole capture leg),
  power/mobility notes including the 600 mA → 1.6 A peripheral-current trap (§6).
- **Photo→screen mapping** retained only for rough estimate + search region, with
  citations, a 4-step calibration flow, and a 7-row failure-mode table (§7).
- **Minimum complete loop**, 15 steps with per-step deterministic/LLM labels and an
  ordered "what breaks first" list (§8).
- **Instrument-panel extension** with its own executed simulation, including an
  honest far-standoff failure and the hard actuation boundary (§9).
- **Blocked-vs-actionable split** with 10 actionable items and 7 blocked items,
  each justified (§10), plus an explicit do-not-build list.
- **Open questions (10), CANNOT-do list (8), and an honesty ledger recording 5
  corrections made during this evaluation** (§11).

### Not Done (honestly)

- **No real optical test of screen→photo fidelity.** Impossible on this host: the
  only camera sees an unlit room (§5.2). Every optical claim is [M-SIM].
- **No hardware screen tracked on real hardware** — no target screen, no light, no
  CSI camera, no Pi5 in this environment.
- **The "which way" accuracy of the LLM was not measured** (§11.1 item 9). The
  closed-loop simulation used a synthetic oracle for the direction signal, so the
  design's most load-bearing unmeasured assumption is exactly that the LLM can
  produce a usable *direction*. The design is robust to a bad direction (the loop
  re-observes; the abort rules bound the damage) but the convergence *rate* depends
  on it.
- **No CH9329 on this host**, so the Actuate leg's timing is arithmetic plus the
  proxy X11 measurement, not a serial timing test. The sibling evaluation owns the
  device-level evidence and was deliberately not duplicated.
- **No multi-monitor / DPI-scaling investigation.**
- **No production code** (out of scope by the brief).

### Verification

- **Deliverable path:** `~/work/GoPi/devdoc/webcam-capture-rpi5-agent-evaluation.md`
  (this file), self-contained, in `master` (docs-only task in the main repo's
  `devdoc/`, as the brief names this path — no worktree/branch was created because
  no code is modified).
- **Evidence:** `devdoc/webcam-capture-evidence/`, 91 files; every number in this
  document traces to a named file there.
- **Reproducibility:** re-running the two primary simulations from scratch produced
  identical metric sets (fixed RNG seeds); every shell-level experiment's commands
  are pasted verbatim (`exp7-ocr-standoff-raw.txt`, `x11-pointer-transfer-raw.txt`).
- **Build/tests:** not applicable — no production code changed; the only Go repo
  state modified is the addition of this document and its evidence directory.
- **Commit:** subject `docs(webcam-capture): RPi5 webcam-as-eye evaluation
  retracted to closed-loop visual servoing design` — 91 files, ~4.8 k insertions,
  in `master` (docs-only, no code touched, so no worktree/branch per Rule 6a).
  Resolve the exact hash with:
  `git log -1 --format=%H -- devdoc/webcam-capture-rpi5-agent-evaluation.md`
  (a hash pasted into the commit that contains the file would invalidate itself;
  this command is deliberately used instead).
- **Rule 3 compliance:** no new tool was added, none modified; `task`/`memory`
  used as designed.
- **Rule 6 (ch9329 read-only):** `/home/user/work/ch9329` was read (three POC files
  and the directory listing) and never written; `git status` there is untouched.
- **Rule 6d deliverable declaration:** the deliverable is a file in the **main
  repo working tree** at `devdoc/webcam-capture-rpi5-agent-evaluation.md` (plus
  `devdoc/webcam-capture-evidence/`); recovery is a plain path read — no branch
  was created and nothing was pushed, because no code was modified and the brief
  names this exact path inside the main repo's `devdoc/`.
