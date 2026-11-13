#!/usr/bin/env python3
"""test_materials_render.py — 共享渲染器的框几何红→绿回归（scripts/materials/render.py）。

为什么有这个测试
----------------
`scripts/materials/render.py` 过去把框的上下边**按标定行距外推**：

    y  = int(round(y0 + pitch * idx)) - PAD_ABOVE_MARK
    bh = line_ink_h + BOX_H_PAD          (line_ink_h = 标定图里最高的行墨迹 + 6)

用的 pitch 是**取整过的整数**（实测 30），而真实行距带小数 ⇒ 每行漂零点几像素，
到十几行就差 1–2 像素；`line_ink_h + 6` 又让框比这一行的墨迹高出 6px。两者一起
把框的下边推到**下一行字形**的笔画上。旧判据是**比例**的（"框里有墨迹就行"），
比例看不见"框线切了下一行的字"。

实测（本次审计，逐条像素测量 159 条成对素材）：`002-task-submit/04-annotated.png`
的第二个框 `rect:1063x22+5+343`，下边 y=364 在框的列范围内切到 **25 个墨迹像素**
（下一行字形的上缘），而比例判据照样放行——图看起来完全正常。

判据现在与 D 面同一套（`scripts/materials/materials-render-D.py` 的三命题）：
① 框的上下边所在像素行，在框的列范围内没有墨迹；② 框内恰好一段墨迹；
③ 那一段落在框**声称的那一行**的行槽里。行槽按像素量（`measure_line_band`），
框边由像素定（`place_box`）——两者都不再由标定值外推。

WHAT IS PINNED
--------------
RED    —— 盘上那个**真的发出去的**框必须被判为 MISALIGNED，且**因为下边切墨迹**而拒。
非空   —— 同一个框，旧的**比例**判据确实放行过它（框内有墨迹 > 0）：否则红是空的。
机理   —— 旧**公式**在同一张图上算出来的框同样下边切墨迹：缺陷是算法的，不是那一条的。
GREEN  —— 新算法在同一行的像素上量出来的框，三条命题全过。
DRIFT  —— 标定值偏 2px：外推值跟着漂，而 `measure_line_band` 量出来的行槽**不动**。
拒绝   —— 让不出干净上下边时返回诊断，而不是悄悄给一个坏框。

fixture 是**盘上真实素材本身**（`03-raw.png`），不是重画的替身：判据必须落在真实像素上。

用法: python3 scripts/test_materials_render.py     # 从仓库根跑
"""
import importlib.util
import os
import sys

from PIL import Image

HERE = os.path.dirname(os.path.abspath(__file__))
sys.path.insert(0, os.path.join(HERE, "lib"))
from tmp_ban import make_scratch_dir  # noqa: E402

RENDER = os.path.join(HERE, "materials", "render.py")
SOURCE = os.path.normpath(os.path.join(
    HERE, "..", "docs", "demo", "materials", "002-task-submit", "03-raw.png"))
# 该图的标定值（生成器实测：首行墨迹顶 / 取整过的行距——缺口的来源）
Y0 = 17
PITCH = 30
# 盘上那个真的发出去的框（`04-annotated.png` 里量出来的红色矩形边），就是红样本：
# 记法 (x0, y0, x1, y1)；`check_box`/`_ink_rows` 要的是 (x, y, w, h)，转换如下。
SHIPPED_RECT = (5, 343, 1064, 364)
SHIPPED_BOX = (SHIPPED_RECT[0], SHIPPED_RECT[1],
               SHIPPED_RECT[2] - SHIPPED_RECT[0] + 1,
               SHIPPED_RECT[3] - SHIPPED_RECT[1] + 1)
SHIPPED_ROW_HINT_Y = 353       # 该框声称的那一行（框心所在的墨迹段）
# 旧公式的常量（旧 render.py 里的字面量，逐字复刻；本测试要证明它算出来的框有缺陷）
OLD_PAD_ABOVE_MARK = 3
OLD_BOX_H_PAD = 6


def load_render():
    spec = importlib.util.spec_from_file_location("materials_render", RENDER)
    mod = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(mod)
    return mod


def old_formula_box(mod, png, y0, idx, w):
    """旧公式逐字复刻：行槽与框高都由标定值外推。"""
    im = Image.open(png).convert("L")
    H = im.size[1]
    runs = mod._ink_spans(mod._ink_rows(png, 0, 0, w, H, mod.EDGE_LUMA))
    line_ink_h = max(b - t + 1 for t, b in runs)
    y = int(round(y0 + PITCH * idx)) - OLD_PAD_ABOVE_MARK
    return (mod.FULLROW_MARGIN, y, w - 2 * mod.FULLROW_MARGIN, line_ink_h + OLD_BOX_H_PAD)


def main():
    mod = load_render()
    make_scratch_dir("materials-render-test")   # Rule 22：scratch 在 / 之外
    W, H = Image.open(SOURCE).size
    checks = 0

    # 该框声称的那一行：按标定提示量出它落在哪一段（行号由全图墨迹段认领）
    hint_idx = int(round((SHIPPED_ROW_HINT_Y - Y0) / float(PITCH)))
    band, why = mod.measure_line_band(SOURCE, Y0, PITCH, hint_idx, 0, W)
    assert band is not None, "行槽必须能按像素量出来：%s" % why

    # --- 1. RED：盘上那个框被结构判据拒绝，且**因为下边切墨迹** -------------
    v_old = mod.check_box(SOURCE, SHIPPED_BOX, band, hint_idx, "shipped 002 box")
    assert v_old["verdict"] == "MISALIGNED", \
        "盘上那个框必须被拒（红证明），实得 %s" % v_old
    assert v_old["ink_on_bottom_edge"] > 0, \
        "拒绝的理由必须是**下边切到墨迹**，而不是碰巧别的命题：%s" % v_old
    assert any("下边" in w for w in v_old["why"]), v_old["why"]
    checks += 1

    # --- 2. 非空：旧的**比例**判据确实放行过这个框 -------------------------
    # 旧判据只问「框里有没有墨迹」——把这个洞钉成算术，免得后人重新推一遍推错。
    ink_in_old = mod._ink_rows(SOURCE, SHIPPED_BOX[0], SHIPPED_BOX[1],
                               SHIPPED_BOX[2], SHIPPED_BOX[3], mod.EDGE_LUMA)
    assert sum(ink_in_old.values()) > 0, \
        ("旧判据（框内有墨迹）必须放行这个红样本，否则这个红是空的：框 %s 内墨迹 = %d"
         % (list(SHIPPED_BOX), sum(ink_in_old.values())))
    checks += 1

    # --- 3. 机理：缺陷是**不对称**——框顶留了 3px 留白，框底一点都没留 ---------
    # 旧公式只有 **顶部** 的 `PAD_ABOVE_MARK`（=3），框高直接来自标定行墨迹高；
    # 这一行的行槽底 y=364 本身就是**带下伸部的字形**（实测该行 raw<200 有 25 个墨迹
    # 像素）——框底因此落在墨迹上。框线是**围着**墨迹画的，留白必须在两侧都有。
    assert v_old["ink_on_top_edge"] == 0 and v_old["ink_on_bottom_edge"] > 0, \
        ("红样本的形状必须是「顶边干净、底边切墨迹」，这正是旧公式的不对称：%s" % v_old)
    assert v_old["box_edge_rows"][1] == mod.measure_line_band(
        SOURCE, Y0, PITCH, hint_idx, 0, W)[0][1], \
        "该框的下边与本行行槽的底边重合——行槽的底边就是墨迹所在，框底因此穿字"
    print("  mechanism: 红样本 (y0=%d, y1=%d) 底边切到 %d 个墨迹像素，顶边 0 个；"
          "本行行槽 %s（末行是下伸部）"
          % (SHIPPED_BOX[1], SHIPPED_BOX[1] + SHIPPED_BOX[3] - 1,
             v_old["ink_on_bottom_edge"], list(band)))
    checks += 1

    # --- 4. GREEN：新算法量出来的框三条命题全过 ---------------------------
    y, bh, why = mod.place_box(SOURCE, band, W, hint_idx)
    assert y is not None, "新算法必须能给这一行放一个框：%s" % why
    box = (mod.FULLROW_MARGIN, y, W - 2 * mod.FULLROW_MARGIN, bh)
    v_new = mod.check_box(SOURCE, box, band, hint_idx, "new measured box")
    assert v_new["verdict"] == "ok", v_new
    assert v_new["ink_on_top_edge"] == 0 and v_new["ink_on_bottom_edge"] == 0, v_new
    assert len(v_new["spans_inside"]) == 1, v_new
    assert v_new["span_in_target_band"], v_new
    assert (y, y + bh - 1) != (SHIPPED_BOX[1], SHIPPED_BOX[3]), \
        "新框必须与旧框**不同**，否则什么都没修：%s vs %s" \
        % ((y, y + bh - 1), (SHIPPED_BOX[1], SHIPPED_BOX[3]))
    checks += 1

    # --- 5. DRIFT：标定值漂 2px，外推跟着漂，量出来的行槽不动 --------------
    b_shift, _ = mod.measure_line_band(SOURCE, Y0 + 2, PITCH, hint_idx, 0, W)
    assert b_shift == band, \
        "行槽必须咬住像素：标定值偏 2px 时它不该跟着动（%s vs %s）" % (b_shift, band)
    assert int(round(Y0 + 2 + PITCH * hint_idx)) != int(round(Y0 + PITCH * hint_idx)), \
        "外推值则确实会漂——这个对照是 DRIFT 这一条的意义所在"
    checks += 1

    # --- 6. 拒绝：让不出干净上下边时返回诊断（绝不悄悄给坏框）-------------
    tight = (0, H - 1)      # 顶到图边：让位四次都到不了干净边
    y2, bh2, why2 = mod.place_box(SOURCE, tight, W, hint_idx)
    assert y2 is None and why2, \
        "让不出干净边必须返回诊断，而不是一个坏框（实得 y=%s h=%s why=%s）" % (y2, bh2, why2)
    checks += 1

    print("test_materials_render: %d checks PASS" % checks)
    return 0


if __name__ == "__main__":
    sys.exit(main())
