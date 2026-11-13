#!/usr/bin/env python3
"""gif-verify.py — 校验一个 gif 里的终端文字，是否就是原始终端输出本身。

用法: python3 tools/gif-verify.py <x.gif> <baseline.txt> <cols> <cell_w> <cell_h>

它把 gif 拆成帧（PIL seek，逐帧落 PNG），对最后一帧（内容全部落地的那一帧）做两项检查：

(A) 结构检查（字体无关、逐字符）
    帧切成 cols x lines 的字符格，每格取二值墨迹补丁。格的形状聚类后要求与
    baseline.txt 的字符**互为双射**：不同字符不许长成一个样（OCR 最容易被骗的
    失败模式），同一字符不许长成两个样，文本有字的格必须有墨，空格必须干净。
    帧是窗口截图、文本原点未知，所以在 ±1 格内搜索偏移并取违反最少的那个作为
    标定，同时报出 naive 原点与四个相邻格的违反数——最优是尖峰还是平台一眼可见
    （平台=标定无意义，结论不能建立在它上面）。

(B) 像素检查
    用同一字体把 baseline.txt 逐行渲染进等高的条带（整幅多行 label 会累积行距漂移：
    实测 magick 自然行距 16.5px vs xterm 格高 19px，30 行漂 15px——那是渲染器的
    假象，不许污染结论），在标定偏移下求墨迹 IoU，并产出可视 diff。

退出码：0 = 两项都通过；1 = 结构检查有违反（或基线放不下这个网格）。
"""
import os, subprocess, sys
from collections import defaultdict

# The scratch root comes from the shared contract (Rule 22): tmp_ban creates
# under the scratch home and asserts BY DEVICE ID that it is off the `/`
# partition. A bare tempfile.mkdtemp() takes the ambient root, i.e. /tmp here.
sys.path.insert(0, os.path.join(os.path.dirname(os.path.abspath(__file__)),
                                "..", "scripts", "lib"))
from tmp_ban import make_scratch_dir  # noqa: E402  (path set above)

import numpy as np
from PIL import Image

INK_THRESHOLD = 60      # 黑底白字：亮度 > 60 记为有墨（抗锯齿灰阶按无墨处理）
BLANK_EPS = 2           # 空格格里允许的杂散墨点
PAD = 32                # 允许的负偏移


def ink_array(path):
    return (np.asarray(Image.open(path).convert("L")) > INK_THRESHOLD)


def analyse(a, grid, cols, cw, ch, pad, off):
    """在给定偏移下切格并统计违反。a 已 pad 过；off=(dx,dy)。"""
    dx, dy = off
    nrows, ncols = len(grid), cols
    nonblank = {}
    blank_text_cells = []
    for r in range(nrows):
        for c in range(ncols):
            y, x = pad + dy + r * ch, pad + dx + c * cw
            p = a[y:y + ch, x:x + cw]
            has = p.sum() > BLANK_EPS
            if has:
                nonblank[(r, c)] = p.tobytes()
            elif grid[r][c] != " ":
                blank_text_cells.append((r, c, grid[r][c]))
    # term-out 里真有字符、屏幕上也有墨的格子之间要求双射
    char2bm, bm2char = defaultdict(set), defaultdict(set)
    for (r, c), bm in nonblank.items():
        if grid[r][c] != " ":
            char2bm[grid[r][c]].add(bm)
            bm2char[bm].add(grid[r][c])
    split = sum(1 for bm, cs in bm2char.items() if len(cs) > 1)      # 一个形状装了多个字符
    merged = sum(1 for chx, bs in char2bm.items() if len(bs) > 1)    # 一个字符画成了多个形状
    # 屏幕上空白格的数量（含 term-out 里是空格的）也算一条约束：不许有凭空多出来的墨
    extra = 0
    for (r, c) in nonblank:
        if grid[r][c] == " ":
            extra += 1
    viol = split + merged + len(blank_text_cells) + extra
    return dict(viol=viol, split=split, merged=merged,
                blank_text=len(blank_text_cells), extra_ink=extra,
                nonblank=len(nonblank))


def explode(gif, tmp):
    """拆帧：gif -> 每帧一张 PNG。返回 (帧文件列表, 每帧延迟 ms)。"""
    im = Image.open(gif)
    paths, delays = [], []
    try:
        n = 0
        while True:
            p = os.path.join(tmp, f"frame-{n:02d}.png")
            im.convert("RGB").save(p)
            paths.append(p); delays.append(im.info.get("duration", 0))
            n += 1
            im.seek(im.tell() + 1)
    except EOFError:
        pass
    return paths, delays


def main():
    gif, termout, cols, cw, ch = (sys.argv[1], sys.argv[2],
                                  int(sys.argv[3]), int(sys.argv[4]), int(sys.argv[5]))
    tmp0 = make_scratch_dir("gifverify")
    frames, delays = explode(gif, tmp0)
    total = sum(delays)
    print(f"gif        : {gif} ({os.path.getsize(gif)} bytes)")
    print(f"frames     : {len(frames)} frames, total {total} ms = {total/1000:.2f} s "
          f"(delays {delays} ms, loop forever)")
    if total < 3000:
        sys.exit(f"REFUSED: {total} ms is shorter than the 3000 ms floor")
    frame = frames[-1]                       # the fully materialised frame
    print(f"verifying  : {os.path.basename(frame)} (the last frame — everything has landed)")
    lines = open(termout, encoding="utf-8").read().split("\n")
    if lines and lines[-1] == "":
        lines.pop()
    longest = max(len(l) for l in lines)
    if longest > cols:
        sys.exit(f"REFUSED: the terminal text has a {longest}-char line but the grid is only "
                 f"{cols} cols — the frame cannot hold it verbatim")
    grid = [list(l.ljust(cols)) for l in lines]

    raw = ink_array(frame)
    H, W = raw.shape
    a = np.zeros((H + 2 * PAD, W + 2 * PAD), bool)
    a[PAD:PAD + H, PAD:PAD + W] = raw
    print(f"frame      : {frame}  {W}x{H} px")
    print(f"grid       : {cols} cols x {len(lines)} lines, cell {cw}x{ch} px "
          f"(frame holds {H//ch} rows x {W//cw} cols)")
    if H // ch < len(lines):
        sys.exit(f"REFUSED: the frame holds {H//ch} rows but the session printed {len(lines)} "
                 f"lines — the top {len(lines)-H//ch} line(s) scrolled off the terminal and are "
                 f"NOT in the gif, so no frame can be compared against the whole baseline. "
                 f"Re-run with a taller window (>= {len(lines)} rows).")

    # ---------- calibration: the offset that minimises bijection violations ----
    best, table = None, {}
    for dy in range(-ch, ch + 1, 1):
        for dx in range(-cw, cw + 1, 1):
            st = analyse(a, grid, cols, cw, ch, PAD, (dx, dy))
            table[(dx, dy)] = st
            if best is None or (st["viol"], abs(dx) + abs(dy)) < (best[1]["viol"], abs(best[0][0]) + abs(best[0][1])):
                best = ((dx, dy), st)
    (bx, by), bst = best
    print(f"\n(A) STRUCTURAL — screen glyphs vs terminal text, per character cell")
    print(f"  calibrated grid origin: dx={bx} dy={by} px "
          f"({bx/cw:+.2f} col, {by/ch:+.2f} row off the naive origin)")
    print(f"  at the calibrated origin : {bst['viol']} violation(s)  "
          f"[one shape/several chars={bst['split']}, one char/several shapes={bst['merged']}, "
          f"text cell blank on screen={bst['blank_text']}, ink in a blank cell={bst['extra_ink']}] "
          f"over {bst['nonblank']} inked cells")
    st00 = table[(0, 0)]
    print(f"  at the naive origin       : {st00['viol']} violation(s)  "
          f"[split={st00['split']} merged={st00['merged']} "
          f"blank-text={st00['blank_text']} extra-ink={st00['extra_ink']}]")
    for d in [(cw, 0), (-cw, 0), (0, ch), (0, -ch)]:
        s = table.get(d)
        if s:
            print(f"  at origin{d if False else ''} offset {d}: {s['viol']} violation(s) "
                  f"(a sharp optimum is required for the calibration to mean anything)")
    ok = bst["viol"] == 0
    print(f"  VERDICT: the screen's glyphs and the terminal text are in BIJECTION at the "
          f"calibrated origin: {'YES' if ok else 'NO (' + str(bst['viol']) + ' violations)'}")

    # ---------- (B) render the same text and compare ink ----------------------
    tmp = tmp0
    exp_txt = os.path.join(tmp, "expected.txt")
    open(exp_txt, "w", encoding="utf-8").write("\n".join(lines) + "\n")

    def advance(points):
        w = []
        for s in ("M", "MM"):
            path = os.path.join(tmp, f"m{len(w)}.png")
            subprocess.run(["magick", "-background", "black", "-fill", "white",
                            "-font", "DejaVu-Sans-Mono", "-pointsize", str(points),
                            f"label:{s}", "-trim", "+repage", path], check=True)
            w.append(Image.open(path).size[0])
        return w[1] - w[0]

    cand = [(abs(advance(p) - cw), p, advance(p)) for p in range(8, 24)]
    cand.sort()
    _, ps, adv = cand[0]

    def pitch(points, spacing):
        path = os.path.join(tmp, "p.png")
        subprocess.run(["magick", "-font", "DejaVu-Sans-Mono", "-pointsize", str(points),
                        "-interline-spacing", str(spacing), "-background", "black",
                        "-fill", "white", "label:M\\nM\\nM", "-trim", "+repage", path],
                       check=True)
        return (Image.open(path).size[1] - points) / 2.0

    # Render EACH LINE into its own cell-sized strip and paste it on the grid.
    # A single multi-line label drifts (magick's line pitch is 18.5 px against
    # xterm's 19 px cell), and a drift of half a pixel per row is 15 px over 30
    # rows — that is a renderer artefact, not a capture defect, so it must not be
    # allowed to pollute the number.
    p0 = pitch(ps, 0)
    exp_png = os.path.join(tmp, "expected.png")
    canvas = Image.new("L", (W, H), 0)
    strip = os.path.join(tmp, "strip.png")
    for r, line in enumerate(lines):
        if line.strip() == "":
            canvas.paste(Image.new("L", (W, ch), 0), (0, r * ch))
            continue
        subprocess.run(["magick", "-background", "black", "-fill", "white",
                        "-font", "DejaVu-Sans-Mono", "-pointsize", str(ps),
                        "-size", f"{W}x{ch}", f"label:{line}", strip], check=True)
        canvas.paste(Image.open(strip).convert("L"), (0, r * ch))
    canvas.save(exp_png)
    e = np.zeros((H + 2 * PAD, W + 2 * PAD), bool)
    e[PAD:PAD + H, PAD:PAD + W] = ink_array(exp_png)

    best_iou, best_off = 0.0, (0, 0)
    for dy in range(-ch - 8, ch + 9):
        for dx in range(-12, 13):
            es = np.roll(np.roll(e, dy, 0), dx, 1)
            inter = np.logical_and(es, a).sum(); union = np.logical_or(es, a).sum()
            if union and inter / union > best_iou:
                best_iou, best_off = inter / union, (dx, dy)
    dx, dy = best_off
    es = np.roll(np.roll(e, dy, 0), dx, 1)
    print(f"\n(B) PIXEL — the same text re-rendered with the same font, ink compared")
    print(f"  expected rendered at pointsize {ps}: advance {adv} px vs xterm cell {cw} px; "
          f"natural pitch {p0:.1f} px vs xterm cell height {ch} px (each line pasted into its own cell-sized strip)")
    print(f"  best alignment dx={dx} dy={dy}: whole-frame ink IoU = {best_iou:.4f}  "
          f"(expected ink px {e.sum()}, captured ink px {a.sum()})")
    vis = np.zeros((H, W, 3), np.uint8)
    sub = lambda M: M[PAD:PAD + H, PAD:PAD + W]
    vis[..., 0] = np.where(np.logical_and(sub(es), np.logical_not(sub(a))), 255, 0)
    vis[..., 1] = np.where(np.logical_and(sub(a), np.logical_not(sub(es))), 255, 0)
    vis[..., 2] = np.where(np.logical_and(sub(es), sub(a)), 255, 0)
    out = os.path.join(os.path.dirname(os.path.abspath(frame)), "diff-vs-expected.png")
    Image.fromarray(vis).save(out)
    print(f"  visual diff: {out}  (red=expected only, green=captured only, blue=both)")
    return 0 if ok else 1


sys.exit(main())
