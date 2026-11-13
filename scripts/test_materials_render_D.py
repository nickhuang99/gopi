#!/usr/bin/env python3
"""test_materials_render_D.py — the red→green regression for the D-face annotation
criterion (scripts/materials/materials-render-D.py).

WHY THIS TEST EXISTS
--------------------
The old criterion asked a PROPORTION question: "does the box overlap its target
row by at least half the row height, and does it overlap any other row by no
more than half?"  A box that straddles two text rows and presses ~50% of each
satisfies it right at the threshold — and the shipped
`001-sandbox-create/04-annotated.png` is exactly that: the box
`rect:1219x31+0+55` has its bottom edge at y=85, which cuts through 29 ink
pixels of the NEXT line's glyphs, and the box holds TWO ink spans
([58,74] and [83,85], i.e. two fields) while the proportion test saw a single
overlap number.  The picture looks perfectly normal; only a measurement finds it.

The criterion is now STRUCTURAL (three pixel propositions, see
`structural_check`): ① the pixel row each box edge sits on carries no ink inside
the box's column range; ② the box holds exactly ONE ink span (one field's strokes
are connected; two fields have a gap between them); ③ that span lies inside the
row band the box CLAIMS.  The box geometry is no longer derived from the
calibration pitch either — a 25px row band whose bottom edge lands ~8px below the
baseline is a coincidence that some glyphs' descenders occupy; the frame is now
measured from this line's actual ink, with the frame line placed in the white
around it.

FIXTURES ARE CROPS, NOT COPIES
------------------------------
Each fixture is a small local crop (PIL `crop`, never a redraw) of a SHIPPED
`03-raw.png`, plus the row bands shifted into crop coordinates.  A committed copy
of the whole 78KB PNG beside the tracked original would be a 4x space cost and a
second source of truth that drifts; the original is in git
(`git show <source_commit>:<source_path>`), and the crop names that path.

WHAT IS PINNED
--------------
RED   — the OLD 001 box must be REFUSED, by name, naming the edge that cuts ink.
GREEN — the CURRENT 003 box must be ACCEPTED, and its own edge rows must actually
        carry the frame line in the shipped 04 image (the manifest is not
        describing a picture that was never drawn).
DRIFT — a box slid a whole row down must be refused by ③ even though ①② pass:
        the other shape that "looks normal".
FIX   — the new geometry, run on the same pixels, must come out clean: the fix is
        a fix, not just a tighter rejection.

The generator's manifest and this test call the SAME functions
(`structural_check`, `frame_around`): the criterion and the box geometry cannot
disagree, because there is one implementation of each.

Usage:
    python3 scripts/test_materials_render_D.py            # from the repo root
"""
import importlib.util
import json
import os
import sys

sys.path.insert(0, os.path.join(os.path.dirname(os.path.abspath(__file__)), "lib"))
from tmp_ban import make_scratch_dir  # noqa: E402

HERE = os.path.dirname(os.path.abspath(__file__))
RENDER = os.path.join(HERE, "materials", "materials-render-D.py")
TESTDATA = os.path.join(HERE, "testdata", "materials-render-D")


def load_render():
    spec = importlib.util.spec_from_file_location("materials_render_D", RENDER)
    mod = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(mod)
    return mod


def fixture(name):
    with open(os.path.join(TESTDATA, name)) as f:
        f = json.load(f)
    f["png"] = os.path.join(TESTDATA, f["crop_png"])
    f["bands"] = {int(k): tuple(v) for k, v in f["row_bands"].items()}
    f["box_tuple"] = (f["box"]["x"], f["box"]["y"], f["box"]["w"], f["box"]["h"])
    # column count: the field's own width in cells (the generator passes it as an int;
    # a fixture may carry it as a string — take it either way).
    f["cols"] = int(f["cols"]) if f.get("cols") else max(12, int(f["box"]["w"] / f["cell_w"]))
    return f


def main():
    mod = load_render()
    make_scratch_dir("materials-render-D-test")   # Rule 22: scratch is off `/`
    checks = 0

    # --- 1. RED: the criterion REFUSES the old 001 box ---------------------
    red = fixture("001-old-row2.json")
    v_old = mod.structural_check(red["png"], red["box_tuple"], red["bands"], red["row"],
                                 "old 001 box")
    assert v_old["verdict"] == "MISALIGNED", \
        "the OLD 001 box MUST be refused (red proof); got %s" % json.dumps(v_old, ensure_ascii=False)
    # ...and for the RIGHT reasons, not incidentally:
    assert v_old["ink_on_bottom_edge"] > 0, v_old
    assert len(v_old["spans_inside"]) == 2, v_old
    assert any("下边" in w for w in v_old["why"]), v_old["why"]
    # the old PROPORTION criterion would have accepted this very box — that is the
    # hole, pinned as arithmetic so a future reader cannot re-derive it wrongly.
    t, b = red["bands"][red["row"]]
    hh = b - t + 1
    ov = max(0, min(red["box"]["y"] + red["box"]["h"] - 1, b) - max(red["box"]["y"], t) + 1)
    other = 0
    for j, (t2, b2) in red["bands"].items():
        if j == red["row"]:
            continue
        other = max(other, max(0, min(red["box"]["y"] + red["box"]["h"] - 1, b2)
                               - max(red["box"]["y"], t2) + 1))
    assert ov >= hh * 0.5 and other <= hh * 0.5, \
        ("the old box must be one the OLD criterion accepted "
         "(ov=%s hh=%s other=%s) — otherwise the red proof is vacuous" % (ov, hh, other))
    checks += 1

    # --- 2. FIX: the new geometry on the same pixels comes out clean -------
    b_new, why = mod.frame_around(red["png"], red["row"], red["bands"][red["row"]], 0,
                                  red["cols"], red["cell_w"], red["bands"],
                                  "new 001 box", "select", red["cols"])
    assert b_new is not None, "the new algorithm must produce a box for row %d: %s" % (red["row"], why)
    assert b_new["h"] != red["box"]["h"] or b_new["y"] != red["box"]["y"], \
        "the new box must DIFFER from the refused one (otherwise nothing was fixed): %s vs %s" \
        % (b_new, red["box"])
    v_new = mod.structural_check(red["png"], (b_new["x"], b_new["y"], b_new["w"], b_new["h"]),
                                 red["bands"], red["row"], "new 001 box")
    assert v_new["verdict"] == "ok", json.dumps(v_new, ensure_ascii=False)
    assert v_new["ink_on_top_edge"] == 0 and v_new["ink_on_bottom_edge"] == 0, v_new
    assert len(v_new["spans_inside"]) == 1, v_new
    assert v_new["span_in_target_band"], v_new
    checks += 1

    # --- 3. GREEN: the criterion ACCEPTS the shipped 003 box ---------------
    green = fixture("003-current-row7.json")
    v_ok = mod.structural_check(green["png"], green["box_tuple"], green["bands"],
                                green["row"], "003 current box")
    assert v_ok["verdict"] == "ok", json.dumps(v_ok, ensure_ascii=False)
    assert v_ok["ink_on_top_edge"] == 0 and v_ok["ink_on_bottom_edge"] == 0, v_ok
    # ...and the shipped 04 image really carries that frame (the manifest does not
    # describe a picture that was never drawn): its own edge rows are non-blank.
    ann = os.path.join(os.path.dirname(TESTDATA), "..", "..", "docs", "demo", "materials",
                       green["slug"], "04-annotated.png")
    ann = os.path.normpath(ann)
    if os.path.exists(ann):
        x0, y0 = green["crop"][0], green["crop"][1]
        x, y, w, h = green["box_tuple"]
        rows = mod.ink_rows_in_box(ann, x + x0, y + y0, w, 1, mod.EDGE_LUMA)
        assert rows.get(y + y0, 0) > 0, \
            "the shipped 04 image has no frame line at the manifest's top edge row"
    checks += 1

    # --- 4. DRIFT: a box slid a whole row down is refused by ③ -------------
    # ①② pass (one span inside, clean edges), ③ fails — the span belongs to the
    # row BELOW. This is the shape that looks normal too.
    t_below, b_below = green["bands"][green["row"] + 1]
    drift = (green["box"]["x"], t_below - 3, green["box"]["w"], (b_below - t_below + 1) + 6)
    v_drift = mod.structural_check(green["png"], drift, green["bands"], green["row"],
                                   "drifted box")
    assert v_drift["verdict"] == "MISALIGNED", json.dumps(v_drift, ensure_ascii=False)
    assert not v_drift["span_in_target_band"], v_drift
    checks += 1

    # --- 5. frame_around's contract on every row of both fixtures ----------
    # A returned box is always clean, and a row it cannot frame is a REFUSAL
    # (None + a diagnosis), never a silent bad box.
    for fx in (red, green):
        for row in sorted(fx["bands"]):
            if row < 0:
                continue
            b, _why = mod.frame_around(fx["png"], row, fx["bands"][row], 0, fx["cols"],
                                       fx["cell_w"], fx["bands"], "probe", "select", fx["cols"])
            if b is None:
                continue
            v = mod.structural_check(fx["png"], (b["x"], b["y"], b["w"], b["h"]),
                                     fx["bands"], row, "probe")
            assert v["verdict"] == "ok", (fx["slug"], row, json.dumps(v, ensure_ascii=False))
            checks += 1

    print("test_materials_render_D: %d checks PASS" % checks)
    return 0


if __name__ == "__main__":
    sys.exit(main())
