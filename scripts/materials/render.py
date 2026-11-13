#!/usr/bin/env python3
"""material-render.py — 为一条素材条目渲染 03-raw.png / 04-annotated.png。

为什么需要它：素材条目的标注必须**指向图像里真实的墨迹**，而不是猜坐标
（Rule 10.2 —— 渲染真值是像素真值，不是"看起来对"）。本脚本：

 1. 自己按固定列宽折行，再让 `gopi text2img --cols 200` 渲染 → 文件第 i 行
    就是图里第 i 行（text2img 不会再折行），行号→像素的映射是可测的。
 2. 用 PIL 量出**标定行距**（pitch）与首行墨迹顶（y0）：渲染一张标定图，
    量每行墨迹的行区间。
 3. 用 `gopi annotate` 画框；框的几何来自上面的测量。
 4. **自证**：PIL 量出「框内原本有多少墨迹像素」+「annotate 改了哪些像素」，
    框里没有墨迹 = 这次标注是装饰，脚本 exit 1（不写 04）。

用法:
  material-render.py --raw <out.png> --ann <out.png> --font PATH \
      --mark-match 'needle:LABEL' [--mark-match ...] [--mark-line N:LABEL ...] < <text>
文本从 stdin 读；空行保留（行号按原文计）。

2026-09-30 修正（三处，都是**量出来**的，见 devdoc/materials-cjk-render-fix.md）：
 a. `--mark-line N` 的 N 现在是 **02-output.txt 内的第几行（1 起）**，
    不再是图内绝对下标（输出行数一变就框错行）。
 b. 标注行上方**补足空行**（`LABEL_BAND`=60px > 标签芯片 ~39px）：长输出每行紧挨着，
    不补空行标签必然压住上一行正文 —— 一张"证明"图盖住它要证明的东西。
    文末另补 `TAIL_BLANK_LINES` 行，否则末行的框没有框高可用。
 c. 框越界**不再静默 clamp**（clamp 会把框挪到别的行上 → 标注在指别处），改为 REFUSED。
    并新增第三项自证 `label_covered_ink == 0`。
"""
import argparse
import json
import math
import os
import subprocess
import sys

from PIL import Image, ImageChops

COLS = 96          # 自己折行的列宽
LINESCOLS = 200    # 交给 text2img 的折行列宽（远大于 COLS ⇒ 它不会再折）
SIZE = 20
LH = 30
LINES_PER_PAGE = 200
# 整行框：框住的是**行**，所以宽度就是版心宽度，不是某个词的宽度。
MARGIN = 12             # text2img 的页边距 px（图的页高 = 行数×LH + 2×MARGIN）
FULLROW_MARGIN = 6      # 框在版心内左右各留的边距 px
INK_LUMA = 200          # 判「这一行有墨」（含抗锯齿）——行槽按像素量时用
EDGE_LUMA = 128         # 判据阈值：<128 算墨迹（框线自己落在这一档上）
BOX_PAD = 3             # 框线离墨迹上下各留的像素数（留白是算出来的，不是碰巧的）
FRAME_TRIES = 4         # 框线在墨迹段外让位的次数上限（每次多让 1 像素）
SPAN_GAP = 4            # 墨迹行间隔 ≤ 这个值算同一段（一个字段的笔画是连着的）
LABEL_BAND = 60         # annotate 的标签芯片画在框上方这段高度内（实测 ~39px，取 60 更宽）
BLANK_LINES_BEFORE_MARK = 3   # 折行后在标注行上方插的空行数（90px > LABEL_BAND）
TAIL_BLANK_LINES = 2    # 文末补的空行：给最后几行的框留出行高，页面因此不会把框挤出去


def sh(cmd):
    p = subprocess.run(cmd, capture_output=True, text=True)
    return p.returncode, p.stdout + p.stderr


def wrap(text, cols):
    """按 cols 折行（不做任何"美化"：图里显示的就是文本本身）。"""
    out = []
    for raw in text.split("\n"):
        if not raw:
            out.append("")
            continue
        line = raw
        while len(line) > cols:
            out.append(line[:cols])
            line = line[cols:]
        out.append(line)
    return out


def ensure_label_room(lines, idx, need):
    """保证标记行**上方**至少 need 行是空的：不够就在该行前插空行。

    为什么必须有：`gopi annotate` 把标签芯片画在框**上方** ~39px 内。长输出里每行
    都紧挨着上一行，标签就会**压住上一行正文** —— 一张"证明"图盖住它要证明的东西，
    是标注最恶的失败形态（实测：vision 看到「红框压住文字尾巴」）。
    返回 (新行表, 标注行新下标)，并保证把**空格**插进去而不是让标注盖住证据。
    """
    band = max(1, int(math.ceil(need / float(LH))))
    above = 0
    j = idx - 1
    while j >= 0 and lines[j] == "" and above < band:
        above += 1
        j -= 1
    if above >= band:
        return lines, idx
    pad = [""] * (band - above)
    return lines[:idx] + pad + lines[idx:], idx + len(pad)


def ink_runs(png):
    """每一段连续含墨迹的行 = 一行文字；返回 [(top, bottom), ...]"""
    a = Image.open(png).convert("L")
    w, h = a.size
    px = a.load()
    rows = []
    for y in range(h):
        dark = False
        for x in range(0, w, 2):
            if px[x, y] < 128:
                dark = True
                break
        rows.append(dark)
    runs, start = [], None
    for y, d in enumerate(rows):
        if d and start is None:
            start = y
        if not d and start is not None:
            runs.append((start, y - 1))
            start = None
    if start is not None:
        runs.append((start, len(rows) - 1))
    return a.size, runs


def _ink_spans(rows, gap=SPAN_GAP):
    """把「有墨迹的行号」归并成段 [top, bottom]（与 D 面 ink_spans 同一口径）。"""
    spans = []
    for y in sorted(k for k, n in rows.items() if n > 0):
        if spans and y - spans[-1][1] <= gap:
            spans[-1][1] = y
        else:
            spans.append([y, y])
    return [(a, b) for a, b in spans]


def _ink_rows(png, x, y, w, h, thresh):
    im = Image.open(png).convert("L")
    px = im.load()
    W, H = im.size
    out = {}
    for yy in range(max(0, y), min(H, y + h)):
        n = 0
        for xx in range(max(0, x), min(W, x + w)):
            if px[xx, yy] < thresh:
                n += 1
        if n:
            out[yy] = n
    return out


def measure_line_band(png, y0, pitch, idx, x0, x1):
    """行槽**量出来**，不由标定 pitch 外推（D 面同一根因，见 devdoc 同名文件）。

    为什么不能外推：`round(y0 + pitch*idx)` 用的是**取整过的**行距（实测 30），
    真实行距带小数 ⇒ 每行漂零点几像素，到十几行就差 1–2 像素，行槽的边于是落在
    **下一行字形**的上缘或本行的笔画上。旧代码正是把那个外推槽的边当成框的边。

    做法（与 D 面 `measure_row_bands` 同一口径）：量出**全图**的墨迹段，每个段按
    `round((top - y0)/pitch)` 认领行号；标定值只用来决定**要找哪一段**。认领必须
    一一对应（一个行号只能有一段，且相邻段之间确实隔了行距），否则返回诊断，由调用
    方 FATAL —— 绝不悄悄用外推值顶上。
    """
    im = Image.open(png).convert("L")
    W, H = im.size
    x0, x1 = max(0, int(x0)), min(W, int(x1))
    segs = _ink_spans(_ink_rows(png, x0, 0, x1 - x0, H, INK_LUMA))
    claim = {}
    why = []
    for t, b in segs:
        k = int(round((t - y0) / pitch))
        if k in claim:
            why.append("两段墨迹（%s 与 [%d,%d]）认领同一行 k=%d —— 行→像素映射不成立"
                       % (list(claim[k]), t, b, k))
            continue
        claim[k] = (t, b)
    if why:
        return None, "; ".join(why[:3])
    if idx not in claim:
        return None, ("第 %d 行按标定提示落在 y≈%d，而全图量出来的墨迹段只认领到 %s"
                      % (idx, int(round(y0 + pitch * idx)), sorted(claim)[:12]))
    band = claim[idx]
    if band[1] - band[0] + 1 >= pitch:
        return None, ("第 %d 行的墨迹段 %s 高 %.0f 像素（≥ 行距 %.1f）——它跨了两行"
                      % (idx, list(band), band[1] - band[0] + 1, pitch))
    return band, None


def place_box(png, band, w, idx):
    """框的上下边放进**这一行墨迹外**的留白里（BOX_PAD 起，最多 FRAME_TRIES 次）。

    让不出去就把诊断交给调用方 FATAL —— 绝不手调坐标、绝不 clamp 到别的行。
    """
    im = Image.open(png).convert("L")
    H = im.size[1]
    top, bot = band
    for pad in range(BOX_PAD, BOX_PAD + FRAME_TRIES):
        y, y2 = top - pad, bot + pad
        if y < 0 or y2 > H - 1:
            continue
        if _ink_rows(png, 0, y, w, 1, EDGE_LUMA).get(y, 0):
            continue
        if _ink_rows(png, 0, y2, w, 1, EDGE_LUMA).get(y2, 0):
            continue
        return y, y2 - y + 1, None
    return None, None, ("第 %d 行的框在墨迹段 [%d,%d] 外面让位 %d 次都找不到一对"
                        "干净的上下边（邻行贴得太近）" % (idx, top, bot, FRAME_TRIES))


def check_box(png, box, band, idx, label=None):
    """三命题**结构判据**（与 D 面 structural_check 同一口径、同一阈值）。

    ① 框的上下边所在像素行，在框的列范围内没有墨迹（EDGE_LUMA 以下算墨迹）；
    ② 框内恰好一段墨迹；
    ③ 那一段落在框**声称的那一行**的行槽里（归因唯一，拦「整行漂一格」）。

    为什么不用比例（"与目标行重叠 ≥ 一半、与别的行 ≤ 一半"）：骑在两行之间各压
    一半的框正好落在阈值上，滑过去而图看起来完全正常 —— 这正是本次审计在盘上
    实测到的形状。
    """
    x, y, w, h = box
    top_row, bot_row = y, y + h - 1
    top_ink = _ink_rows(png, x, top_row, w, 1, EDGE_LUMA).get(top_row, 0)
    bot_ink = _ink_rows(png, x, bot_row, w, 1, EDGE_LUMA).get(bot_row, 0)
    spans = _ink_spans(_ink_rows(png, x, y, w, h, EDGE_LUMA))
    in_band = bool(spans) and band is not None and band[0] <= spans[0][0] and spans[0][1] <= band[1]
    why = []
    if top_ink:
        why.append("上边(y=%d)在框的列范围内切到 %d 个墨迹像素" % (top_row, top_ink))
    if bot_ink:
        why.append("下边(y=%d)在框的列范围内切到 %d 个墨迹像素" % (bot_row, bot_ink))
    if len(spans) != 1:
        why.append("框内有 %d 段墨迹（要求恰好 1 段）：%s" % (len(spans), spans[:4]))
    if spans and not in_band:
        why.append("框内那段墨迹 %s 不在第 %d 行的行槽 %s 里" % (list(spans[0]), idx, list(band)))
    return {"label": label, "row": int(idx), "box": [int(v) for v in box],
            "box_edge_rows": [top_row, bot_row], "ink_on_top_edge": top_ink,
            "ink_on_bottom_edge": bot_ink, "spans_inside": [list(s) for s in spans],
            "row_band": list(band), "span_in_target_band": in_band,
            "verdict": "ok" if not why else "MISALIGNED", "why": why}


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("--raw", required=True)
    ap.add_argument("--ann", required=True)
    ap.add_argument("--font", required=True)
    ap.add_argument("--bin", default="gopi")
    ap.add_argument("--mark-line", action="append", default=[])
    ap.add_argument("--mark-match", action="append", default=[])
    ap.add_argument("--mark-after", default="02-output.txt",
                    help="只在含此字样的分隔行**之后**的正文里找标注行。"
                         "默认锚在 02-output.txt 分隔行上——这样标注永远落在证据上，"
                         "永远不会框到图顶部那段解说文字（框到解说=这张图没在承担解释）。")
    ap.add_argument("--max-lines", type=int, default=80)
    ap.add_argument("--legacy-layout", action="store_true",
                    help="按**盘上已发出去的图**的版式重画：不插标签净空、不补文末空行、"
                         "不截断、并按原图高度定行数。用它可以重画老素材的 04 而不动 03"
                         "一个字节（老图的版式与 render.py 现在的版式不同——这正是"
                         "'输入重建'过去失败的原因）。")
    ap.add_argument("--json-out")
    ap.add_argument("--scratch", default=None,
                    help="渲染中间件（折行后的 .txt、标定图）落在这个目录。"
                         "默认与 --raw 同目录——**别用默认**：素材目录的契约是"
                         "恰好六件，中间件落进去就多出 3 个文件（实测踩过）。")
    args = ap.parse_args()

    lines = wrap(sys.stdin.read().rstrip("\n"), COLS)
    total_lines = len(lines)
    # 一条目 = 一页图。太长的输出（比如整篇 help dump）不塞进图里：只画前
    # --max-lines 行，并在图上**明确写出**被截断了、完整 verbatim 在哪。
    # 图里显示的永远是文本的真实前缀，不是重新排过的摘要。
    truncated = 0
    if args.max_lines and total_lines > args.max_lines:
        truncated = total_lines - args.max_lines
        lines = lines[:args.max_lines] + [
            "",
            "…… 图到此为止（text2img 一页上限）：本条目输出共 %d 行，此处只画前 %d 行。" % (
                total_lines, args.max_lines),
            "完整的 verbatim 输出见同目录 02-output.txt —— 图是它的一个窗口，不是替代品。",
        ]
    # --- 组 --mark：框住指定行（行号 + 内容匹配两种选择器） --------------
    # 两种行选择器：--mark-line <N>:<label> 是硬编码行号（输出行数一变就框错行）；
    # --mark-match <substring>:<label> 按**内容**找行，找不到就跳过，绝不乱框。
    picks = []
    # 正文地板：只看 02-output.txt 分隔行之后的输出——解说文字在上半张图，
    # 标注必须落在证据上，否则这张图就没在承担解释（README §1 的判据）。
    floor = 0
    if args.mark_after:
        # 锚必须落在**分隔行**上（以 ─ 开头），并且取**第一次**出现：
        # 图尾那句"完整输出见 02-output.txt"的截断说明里也含同样的字样，
        # 取最后一次会把地板推到图外（实测：三个标注全落空、整条图没产出）。
        for i, l in enumerate(lines):
            if args.mark_after in l and l.lstrip().startswith("─"):
                floor = i + 1
                break
    picks = [(floor + int(spec.partition(":")[0]) - 1, spec.partition(":")[2],
              "output-line %s" % spec.partition(":")[0]) for spec in args.mark_line]
    for spec in args.mark_match:
        needle, _, label = spec.partition(":")
        hits = [i for i, l in enumerate(lines) if needle in l and i >= floor]
        if not hits:
            sys.stderr.write("note: --mark-match %r matched no rendered line at/after the output section\n" % needle)
            continue
        picks.append((hits[0], label, needle))

    if args.legacy_layout:
        # **按盘上那张图**的版式重画：老素材的高度与当前版式不同（老图没有标签净空、
        # 没有文末补行），原样重建才能让 03 逐字节相同——这是重画老素材 04 的前提。
        # **不要在这里再写 `from PIL import Image`**：Python 里 import 是赋值，函数内
        # 出现一次就把 `Image` 变成**整个 main() 的局部名**，模块级那行在函数里从此
        # 不可达；凡是没走本分支的路径读到它（下面 Image.open(args.ann) 那处像素自证）
        # 就抛 UnboundLocalError——这正是本模式长期跑不起来的唯一原因。
        # 静态守卫：internal/testutil/python_import_shadow_gate_test.go
        _w, _h = Image.open(args.raw).size if os.path.exists(args.raw) else (0, 0)
        if _h:
            want = max(1, int(round((_h - 2 * MARGIN) / float(LH))))
            if want < len(lines):
                lines = lines[:want]
            else:
                lines = lines + [""] * (want - len(lines))
    else:
        # 文末补空行：图的页高由行数决定；不补的话最后几行没有框高可用，
        # 下面的 y 就会被挤到页外（旧的静默 clamp 会把框挪到**别的行**上）。
        lines = lines + [""] * TAIL_BLANK_LINES

        # 标注行上方要给标签腾净空（插空行会挪动下方所有下标 → 必须在算 y 之前做）
        for k, (idx, label, needle) in enumerate(picks):
            lines, newidx = ensure_label_room(lines, idx, LABEL_BAND)
            picks[k] = (newidx, label, needle)
    total_lines = len(lines)

    scratch = args.scratch or os.path.dirname(args.raw)
    os.makedirs(scratch, exist_ok=True)
    stem = os.path.join(scratch, os.path.basename(args.raw))
    txt = stem + ".txt"
    with open(txt, "w") as f:
        f.write("\n".join(lines) + "\n")

    rc, out = sh([args.bin, "tool", "text2img", txt, "--out", args.raw, "--font", args.font,
                  "--size", str(SIZE), "--cols", str(LINESCOLS), "--lh", str(LH),
                  "--lines", str(LINES_PER_PAGE)])
    if not os.path.exists(args.raw):
        sys.stderr.write("text2img produced no %s (rc=%d): %s\n" % (args.raw, rc, out))
        return 1

    # --- 标定行距：渲染一张已知行数的图，量出行→像素的映射 -------------
    cal_txt = stem + ".cal.txt"
    cal_png = stem + ".cal.png"
    with open(cal_txt, "w") as f:
        f.write("\n".join("H" * 8 for _ in range(6)) + "\n")
    rc, out = sh([args.bin, "tool", "text2img", cal_txt, "--out", cal_png, "--font", args.font,
                  "--size", str(SIZE), "--cols", str(LINESCOLS), "--lh", str(LH),
                  "--lines", str(LINES_PER_PAGE)])
    if rc != 0:
        sys.stderr.write("calibration render failed: %s\n" % out)
        return 1
    _, runs = ink_runs(cal_png)
    if len(runs) < 3:
        sys.stderr.write("calibration found %d ink runs, want >=3\n" % len(runs))
        return 1
    y0 = runs[0][0]
    pitch = (runs[-1][0] - runs[0][0]) / float(len(runs) - 1)
    x_left_x0 = 0

    (w, h), runs = ink_runs(args.raw)

    marks, checks, box_verdicts = [], [], []
    for idx, label, needle in picks:
        if idx >= len(lines):
            continue
        # 框的上下边**由像素定**，不由标定行距外推（2026-09-30，本次审计的修法）：
        # 标定 pitch 只当**搜索提示**，真实的行槽从这张图的墨迹量出来（measure_line_band），
        # 边线放进该行墨迹外的留白里，并逐框过三命题结构判据（structural_check）。
        band, why = measure_line_band(args.raw, y0, pitch, idx, x_left_x0, w)
        if why:
            sys.stderr.write("REFUSED: 标记行 %d 的行槽量不出来：%s\n" % (idx, why))
            return 1
        y, bh, why = place_box(args.raw, band, w, idx)
        if why:
            sys.stderr.write("REFUSED: 标记行 %d 的框放不下：%s\n" % (idx, why))
            return 1
        x = FULLROW_MARGIN
        box_w = max(60, w - 2 * FULLROW_MARGIN)
        v = check_box(args.raw, (x, y, box_w, bh), band, idx, label)
        if v["verdict"] != "ok":
            sys.stderr.write("REFUSED: 标记行 %d 的框结构判据不成立：%s\n"
                             % (idx, "; ".join(v["why"])))
            return 1
        marks.append("rect:%dx%d+%d+%d:%s:fixed" % (box_w, bh, x, y, label))
        checks.append((x, y, box_w, bh, idx, label, needle))
        box_verdicts.append(v)

    if not marks:
        sys.stderr.write("no mark line landed inside the image (%d lines rendered)\n" % len(lines))
        return 1

    cmd = [args.bin, "tool", "annotate", args.raw, "--out", args.ann, "--font", args.font,
           "--size", "15"]
    for m in marks:
        cmd += ["--mark", m]
    rc, out = sh(cmd)
    if rc != 0 or not os.path.exists(args.ann):
        sys.stderr.write("annotate failed rc=%d: %s\n" % (rc, out))
        return 1

    # --- 自证（像素真值）：框里有没有原本的墨迹；改了哪些像素 -----------
    raw_im = Image.open(args.raw).convert("RGB")
    ann_im = Image.open(args.ann).convert("RGB")
    if raw_im.size != ann_im.size:
        sys.stderr.write("size changed (raw=%s ann=%s)\n" % (raw_im.size, ann_im.size))
        return 1
    raw_l = raw_im.convert("L").load()
    ink_in_box = []
    for (x, y, bw, bh, idx, label, needle) in checks:
        n = 0
        for yy in range(y, min(h, y + bh)):
            for xx in range(x, min(w, x + bw)):
                if raw_l[xx, yy] < 128:
                    n += 1
        ink_in_box.append({"line": idx, "label": label, "needle": needle, "ink_under_mark": n})
    diff = ImageChops.difference(raw_im, ann_im)
    bbox = diff.getbbox()
    changed = 0
    if bbox:
        d = diff.crop(bbox).convert("L").point(lambda v: 255 if v > 8 else 0).histogram()
        changed = d[255]
    covered = 0
    for (x, y, bw, bh, idx, label, needle) in checks:
        top = max(0, y - LABEL_BAND)
        if top >= y:
            continue
        band_raw = raw_im.crop((0, top, w, y)).convert("L")
        band_diff = ImageChops.difference(raw_im.crop((0, top, w, y)),
                                          ann_im.crop((0, top, w, y))).convert("L")
        covered += sum(1 for a, b in zip(band_raw.getdata(), band_diff.getdata())
                       if a < 128 and b > 30)

    result = {
        "raw": args.raw, "ann": args.ann, "size": [w, h],
        "label_covered_ink": covered,
        "y0": y0, "pitch": pitch, "lines_rendered": len(lines),
        "marks": marks, "ink_in_box": ink_in_box,
        "box_verdicts": box_verdicts,
        "changed_bbox": bbox, "changed_pixels": changed,
        "annotate_stdout": out.strip(),
    }
    if args.json_out:

        with open(args.json_out, "w") as f:
            json.dump(result, f, ensure_ascii=False, indent=2)
    print(json.dumps({k: result[k] for k in
                      ("size", "y0", "pitch", "changed_pixels", "ink_in_box")},
                     ensure_ascii=False))
    if covered and not args.legacy_layout:
        # legacy 模式下不拒：老素材的标签带**本来就**压着上一行（那时还没有这条净空
        # 要求）。本次只重建那张图、把框画对，不改版式；covered 仍如实写进 JSON。
        sys.stderr.write("REFUSED: 标签带压住了证据 %d 像素（标注行上方要留空）\n" % covered)
        return 1
    empty = [c for c in ink_in_box if c["ink_under_mark"] == 0]
    if empty:
        sys.stderr.write("REFUSED: mark(s) boxed no ink — %s\n" % empty)
        return 1
    if changed == 0:
        sys.stderr.write("REFUSED: 0 pixels changed by annotate\n")
        return 1
    return 0


if __name__ == "__main__":
    sys.exit(main())
