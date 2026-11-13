#!/usr/bin/env python3
"""box_audit.py — 逐面像素测量：盘上现存 03-raw.png / 04-annotated.png 成对素材。

判据（D 面结构命题的测量侧，全部是像素：不读标定值、不读 manifest、不猜坐标）：

  1. 框**怎么量** —— 用标记色身份，不用差分形状：`gopi annotate` 的标记墨色是
     RGB(220,30,30)（internal/text2img/annotate.go markInkR/G/B），底图是灰阶
     （0/64/127/191/255），两者不重叠。框边 = 该色的长水平游程（上下边）与长竖直
     游程（左右边）。标签芯片是白底黑字，天然不参与。
     **一张图里的每个框都单独量、单独测**（素材图常有多框）：对每个「上边候选」
     找其下方最近、列区间一致的「下边候选」，且左右两边在 y 区间内连续覆盖 >=60%。

  2. 三条结构命题（逐框各测一次）：
     ① 框**上边所在像素行**，在框列范围内，原始图（03-raw）有没有墨迹；
     ② 框**下边所在像素行**，同上；
     ③ 框**内部**恰好几段墨迹（段 = 连续有墨行；段间空白 >= SPAN_GAP 行才算断开）。
     ⇒ ①②非零 = 框线穿过字形；③ != 1 = 框里不是一个字段（0=框了空行，>=2 压两个字段）。

  口径：边缘墨迹只数**框列范围内**（x0+1 .. x1-1）的原始图 L < INK_LUMA 像素；
  边厚取该边连续的长游程行数（annotate 线宽 2）。判据不看标注图，只看 03-raw。
"""
import json
import os
import sys

from PIL import Image

INK_LUMA = 200      # 判「这行有墨」（含抗锯齿）
MIN_EDGE_RUN = 24   # 一条边至少要有的连续长度（短于此不算框线）
MARK_RGB = (220, 30, 30)
SPAN_GAP = 4        # 墨迹段之间的最小空白行数才算断开
MIN_BOX_H = 8       # 框高下限
MAX_BOX_H = 400     # 上下边配对的最大跨度（超过就不是同一个框）


def runs_of(vals):
    out, s = [], None
    for i, v in enumerate(vals):
        if v and s is None:
            s = i
        elif not v and s is not None:
            out.append((s, i - 1))
            s = None
    if s is not None:
        out.append((s, len(vals) - 1))
    return out


def edge_groups(mark, W, H):
    """所有「水平长游程」→ 折叠成 (y_start, y_end, x0, x1)。"""
    per_row = {}
    for y in range(H):
        row = mark[y * W:(y + 1) * W]
        long_runs = [(s, e) for s, e in runs_of(row) if e - s + 1 >= MIN_EDGE_RUN]
        if long_runs:
            per_row[y] = long_runs
    groups = []
    for y in sorted(per_row):
        for (s, e) in per_row[y]:
            merged = False
            for g in groups:
                if g[1] == y - 1 and abs(g[2] - s) <= 3 and abs(g[3] - e) <= 3:
                    g[1] = y
                    merged = True
                    break
            if not merged:
                groups.append([y, y, s, e])
    return [tuple(g) for g in groups]


def boxes_in(mark, W, H):
    """枚举图里所有框矩形 (x0,y0,x1,y1)。"""
    groups = edge_groups(mark, W, H)
    out = []
    for i, g in enumerate(groups):
        y0, ye, x0, x1 = g
        for h in groups[i + 1:]:
            if h[2] < x0 - 3 or h[3] > x1 + 3:
                continue            # 下边必须在同一列区间（可宽于上边）
            if h[0] - ye < MIN_BOX_H or h[0] - y0 > MAX_BOX_H:
                continue
            y1 = h[1]
            height = y1 - y0 + 1
            ok = 0
            for x in (x0, x0 + 1, x1 - 1, x1):
                if not (0 <= x < W):
                    continue
                col = [mark[y * W + x] for y in range(y0, y1 + 1)]
                if max([e - s + 1 for s, e in runs_of(col)] + [0]) >= height * 0.6:
                    ok += 1
            if ok >= 1:
                out.append((x0, y0, x1, y1))
                break
    return out


def measure_entry(d):
    name = os.path.basename(d.rstrip("/"))
    rp, ap = os.path.join(d, "03-raw.png"), os.path.join(d, "04-annotated.png")
    if not (os.path.exists(rp) and os.path.exists(ap)):
        return {"entry": name, "status": "no-pair"}
    raw = Image.open(rp).convert("L")
    ann = Image.open(ap).convert("RGB")
    if raw.size != ann.size:
        return {"entry": name, "status": "size-mismatch"}
    W, H = raw.size
    mark = [p == MARK_RGB for p in ann.getdata()]
    rpix = list(raw.getdata())
    dpix = [p[0] for p in ann.getdata()]

    def row_ink(y, x0, x1):
        base = y * W
        return sum(1 for x in range(max(0, x0 + 1), min(W, x1)) if rpix[base + x] < INK_LUMA)

    def spans(y0, y1, x0, x1, th):
        has = [row_ink(y, x0, x1) > 0 for y in range(y0 + th, y1 - th + 1)]
        n, blank, cur = 0, 0, False
        for v in has:
            if v:
                cur, blank = True, 0
            elif cur:
                blank += 1
                if blank >= SPAN_GAP:
                    n += 1
                    cur = False
        if cur:
            n += 1
        return n

    # 芯片（label chip）与真正的标注框：芯片把底图**涂白**了（interior 被改），
    # 真正的框内部与 03-raw 逐像素相同（只动边）。用这个区分，不猜几何。
    def interior_changed_frac(x0, y0, x1, y1):
        n = ch = 0
        for y in range(y0 + 2, y1 - 1):
            base = y * W
            for x in range(x0 + 2, x1 - 1):
                n += 1
                if abs(rpix[base + x] - dpix[base + x]) > 30:
                    ch += 1
        return (ch / float(n)) if n else 0.0

    res = []
    for (x0, y0, x1, y1) in boxes_in(mark, W, H):
        th = 2
        top = sum(row_ink(y, x0, x1) for y in range(y0, y0 + th))
        bot = sum(row_ink(y, x0, x1) for y in range(y1 - th + 1, y1 + 1))
        sp = spans(y0, y1, x0, x1, th)
        inside = sum(row_ink(y, x0, x1) for y in range(y0 + th, y1 - th + 1))
        chg = interior_changed_frac(x0, y0, x1, y1)
        # 真框 vs 标签芯片：**底图内部有没有墨迹**说了算。
        # 真框是围着一行字的套子 ⇒ raw 内部有墨；芯片是标签的底衬，画在空白处
        # ⇒ raw 内部墨迹为 0（芯片的白色填充 + 黑色标签文字都只出现在 ann 图上）。
        raw_inside = sum(row_ink(y, x0, x1) for y in range(y0 + th, y1 - th + 1))
        if raw_inside == 0:
            res.append({"box": [x0, y0, x1, y1], "kind": "chip",
                        "interior_changed": round(chg, 3)})
            continue
        res.append({"box": [x0, y0, x1, y1], "kind": "box",
                    "interior_changed": round(chg, 3), "edge_top_ink": top,
                    "edge_bot_ink": bot, "spans": sp, "ink_inside": inside,
                    "verdict": "DIRTY" if (top or bot or sp != 1) else "CLEAN",
                    "reason": ("top-edge-ink=%d " % top if top else "") +
                              ("bot-edge-ink=%d " % bot if bot else "") +
                              ("spans=%d" % sp if sp != 1 else "")})
    real = [b for b in res if b["kind"] == "box"]
    if not real:
        return {"entry": name, "status": "no-box", "size": [W, H],
                "mark_px": sum(mark), "chips": len(res)}
    return {"entry": name, "status": "ok", "size": [W, H],
            "n_boxes": len(real), "n_chips": len(res) - len(real), "boxes": real,
            "verdict": "DIRTY" if any(b["verdict"] == "DIRTY" for b in real) else "CLEAN"}


def main():
    root = sys.argv[1]
    out = []
    for name in sorted(os.listdir(root)):
        d = os.path.join(root, name)
        if os.path.isdir(d):
            out.append(measure_entry(d))
    if len(sys.argv) > 2:
        with open(sys.argv[2], "w") as f:
            json.dump(out, f, ensure_ascii=False, indent=1)
    for r in out:
        if r["status"] == "ok":
            bad = [b for b in r["boxes"] if b["verdict"] == "DIRTY"]
            print("%-42s %-6s boxes=%d dirty=%d %s"
                  % (r["entry"], r["verdict"], r["n_boxes"], len(bad),
                     "; ".join("%s %s" % (b["box"], b["reason"]) for b in bad[:3])))
        else:
            print("%-42s %s" % (r["entry"], r["status"]))
    cnt = lambda v: len([r for r in out if r.get("verdict") == v])
    print("-- total=%d clean=%d dirty=%d unmeasured=%d"
          % (len(out), cnt("CLEAN"), cnt("DIRTY"),
             len([r for r in out if r["status"] != "ok"])))


if __name__ == "__main__":
    main()
