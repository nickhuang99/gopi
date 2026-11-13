#!/usr/bin/env python3
"""render-entry.py — 为一条素材条目渲染 03-raw.png / 04-annotated.png（中文原生）。

## 为什么有它（本脚本存在的唯一理由）

旧生成器用 `xterm(:77)` + `gopi capture` 出图，脚本里**没有 LANG/LC_ALL**，
于是 xterm 按 Latin-1 逐字节画 —— 同一张图上的截图中文全是乱码，
而 `gopi annotate` 的标注中文却是好的（它走 text2img 的字体链）。**一图两制**。

本脚本把截图路径也改走 `gopi text2img`（`internal/text2img` 的 CLI 脸）：
   自研 SFNT/TrueType 解析 + 栅格化，**全程没有 locale 这个概念**，
   CJK 走系统字体链（本脚本显式 `--font` 指定），因此误读字节的机会不存在。

## 标注：框字段/框词，不框整行

标注按**针（needle）+ 字符偏移**定位：用 PIL 读出与 text2img 同源的 TrueType
advance，算出 needle 在渲染行内的 x 起止，框住那个词/字段本身。
框整行 = 没有焦点（一行本来就一行，框住整行等于没框）。

## 自证（Rule 2 渲染真值：像素级，不是"看起来对"）

每次标注后量三件事，任一不满足就 exit 1、拒绝产出这张图：
  1. 框内原本有墨迹（ink_under_box > 0）—— 否则这个框是装饰；
  2. `annotate` 真的改了像素（changed_pixels > 0）；
  3. **标签带没有压住证据**：标签芯片画在框上方，量「标签带内原本的墨迹 ∩
     被 annotate 改动的像素」必须为 0 —— 否则标注盖住了它声称展示的东西。
     （对策：折行之后在标注行上方插两个空行，60px > 标签带高度 ~39px。）

用法（由 gen-task17-CJK.sh 调用，也可单条真跑）:
  render-entry.py --dir <条目目录> --needle <词> --label <标签> --caption <说明> \
      --font <ttf> --bin <gopi> --scratch <中间件目录> [--json-out <json>]
"""
import argparse
import json
import os
import subprocess
import sys

from PIL import Image, ImageChops, ImageFont

COLS = 96            # 自己折行的列宽（按 rune 计）
SIZE = 20            # 字号 px（text2img 默认，无 hinting 的最优值）
LH = 30              # 行高 px
MARGIN = 12          # text2img 默认页边距
LINESCOLS = 400      # 交给 text2img 的折行列宽：远大于 COLS ⇒ 它不会再折行
MAXLINES = 400       # 一页上限（远大于本批最长输出 26 行；分页会改名，见下）
PAD = 4              # 框在词外的余量 px（覆盖左右 side bearing）
BLANK_LINES = 3      # 标注行上方插入的空行数（90px > 标签带 ~39px）
CHIP_BAND = 80       # 量「标签带」的高度 px


def wrap(text, cols):
    """按 cols 折行，空行保留（不做任何"美化"：图里显示的就是文本本身）。"""
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


def run(cmd):
    p = subprocess.run(cmd, capture_output=True, text=True)
    return p.returncode, (p.stdout + p.stderr).strip()


def ink_column_extent(png):
    im = Image.open(png).convert("L")
    w, h = im.size
    px = im.load()
    xs = [x for x in range(w) if any(px[x, y] < 128 for y in range(h))]
    return (min(xs), max(xs)) if xs else None


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("--dir", required=True, help="条目目录（六件套之一）")
    ap.add_argument("--needle", required=True, help="要框住的词/字段（必须逐字出现在输出区）")
    ap.add_argument("--label", required=True, help="标签（不得含半角冒号）")
    ap.add_argument("--caption", required=True, help="图顶那句「这张图在说明什么」")
    ap.add_argument("--font", required=True)
    ap.add_argument("--bin", default="gopi")
    ap.add_argument("--scratch", required=True)
    ap.add_argument("--json-out")
    args = ap.parse_args()

    if ":" in args.label or ";;" in args.label:
        sys.stderr.write("FATAL: label 含半角冒号或 ;;，annotate 会切错字段: %r\n" % args.label)
        return 1

    d = args.dir
    entry = os.path.basename(d)
    inp = open(os.path.join(d, "01-input.txt"), encoding="utf-8").read().rstrip("\n")
    out = open(os.path.join(d, "02-output.txt"), encoding="utf-8").read().rstrip("\n")

    sep_in = "── 01-input.txt（verbatim 命令）─────────────────────────────────"
    sep_out = "── 02-output.txt（verbatim 输出，末行是 exit code）───────────────"
    head = "── %s 素材渲染（internal/text2img，无 locale 依赖）──────────────" % entry

    text = [head, args.caption, ""]
    text += [sep_in]
    text += inp.split("\n")
    text += ["", sep_out]
    text_start = len(text)
    text += out.split("\n")

    raw = wrap("\n".join(text), COLS)
    # 只框证据区：标注必须落在 02-output.txt 分隔行之后（框到图顶解说 = 图没在承担解释）
    floor = 0
    for i, l in enumerate(raw):
        if l.startswith(sep_out[:20]):
            floor = i + 1
            break
    hits = [i for i, l in enumerate(raw) if args.needle in l and i >= floor]
    if not hits:
        sys.stderr.write("REFUSED: needle %r 不在证据区（折行后找不到；floor=%d）\n" % (args.needle, floor))
        return 1
    line_no = hits[0]
    # 折行之后（不是原文上）插空行：标签带需要 60px 净空
    lines = raw[:line_no] + [""] * BLANK_LINES + raw[line_no:]
    mark_line = line_no + BLANK_LINES

    # --- x 偏移：PIL 的 TrueType advance 与 text2img 同源（都是 hmtx）--------
    font = ImageFont.truetype(args.font, SIZE)
    src = raw[line_no]
    start = src.index(args.needle)
    x0 = MARGIN + font.getlength(src[:start])
    wtext = font.getlength(args.needle)
    box_x = max(0, int(round(x0)) - PAD)
    box_w = int(round(wtext)) + 2 * PAD
    box_y = MARGIN + mark_line * LH
    box_h = LH - 4

    os.makedirs(args.scratch, exist_ok=True)
    txt = os.path.join(args.scratch, entry + ".txt")
    with open(txt, "w", encoding="utf-8") as f:
        f.write("\n".join(lines) + "\n")

    raw_png = os.path.join(d, "03-raw.png")
    ann_png = os.path.join(d, "04-annotated.png")
    rc, t2i_out = run([args.bin, "tool", "text2img", txt, "--out", raw_png, "--font", args.font,
                       "--size", str(SIZE), "--cols", str(LINESCOLS), "--lh", str(LH),
                       "--lines", str(MAXLINES)])
    if rc != 0 or not os.path.exists(raw_png):
        sys.stderr.write("REFUSED: text2img rc=%d 未产出 %s：%s\n" % (rc, raw_png, t2i_out))
        return 1

    im = Image.open(raw_png)
    W, H = im.size
    if box_y + box_h > H:
        sys.stderr.write("REFUSED: 框 Y 越界（%d+%d > %d）\n" % (box_y, box_h, H))
        return 1

    spec = "rect:%dx%d+%d+%d:%s:fixed" % (box_w, box_h, box_x, box_y, args.label)
    rc, ann_out = run([args.bin, "tool", "annotate", raw_png, "--out", ann_png,
                       "--font", args.font, "--size", "15", "--mark", spec, "--json"])
    if rc != 0 or not os.path.exists(ann_png):
        sys.stderr.write("REFUSED: annotate rc=%d：%s\n" % (rc, ann_out))
        return 1

    # --- 自证 1/2/3（像素真值）--------------------------------------------
    raw_im = Image.open(raw_png).convert("RGB")
    ann_im = Image.open(ann_png).convert("RGB")
    if raw_im.size != ann_im.size:
        sys.stderr.write("REFUSED: 尺寸变了 raw=%s ann=%s\n" % (raw_im.size, ann_im.size))
        return 1
    rl = raw_im.convert("L").load()
    ink = 0
    for yy in range(box_y, min(H, box_y + box_h)):
        for xx in range(box_x, min(W, box_x + box_w)):
            if rl[xx, yy] < 128:
                ink += 1
    diff = ImageChops.difference(raw_im, ann_im)
    bbox = diff.getbbox()
    changed = 0
    if bbox:
        changed = diff.crop(bbox).convert("L").point(lambda v: 255 if v > 8 else 0).histogram()[255]
    # 标签带：框上方 CHIP_BAND px。原本有墨迹且被改动的像素 = 标注盖住了证据。
    band = raw_im.crop((0, max(0, box_y - CHIP_BAND), W, box_y)).convert("L")
    band_diff = ImageChops.difference(
        raw_im.crop((0, max(0, box_y - CHIP_BAND), W, box_y)),
        ann_im.crop((0, max(0, box_y - CHIP_BAND), W, box_y))).convert("L")
    covered = sum(1 for a, b in zip(band.getdata(), band_diff.getdata()) if a < 128 and b > 30)

    res = {"entry": entry, "needle": args.needle, "label": args.label, "caption": args.caption,
           "size": [W, H], "lines_rendered": len(lines), "output_lines": out.count("\n") + 1,
           "mark": spec, "box": [box_x, box_y, box_w, box_h],
           "ink_under_box": ink, "changed_pixels": changed,
           "label_covered_ink": covered, "width_share": round(box_w / float(W), 3),
           "text2img": t2i_out, "annotate": ann_out, "raw": raw_png, "ann": ann_png,
           "raw_sha16": __import__("hashlib").sha256(open(raw_png, "rb").read()).hexdigest()[:16],
           "ann_sha16": __import__("hashlib").sha256(open(ann_png, "rb").read()).hexdigest()[:16]}
    if args.json_out:
        with open(args.json_out, "w", encoding="utf-8") as f:
            json.dump(res, f, ensure_ascii=False, indent=2)
    print(json.dumps(res, ensure_ascii=False))

    if ink == 0:
        sys.stderr.write("REFUSED: 框里没有墨迹（框错了行）—— %s\n" % entry)
        return 1
    if changed == 0:
        sys.stderr.write("REFUSED: annotate 改了 0 像素\n")
        return 1
    if covered != 0:
        sys.stderr.write("REFUSED: 标签带压住证据 %d 像素（需要上方留空）\n" % covered)
        return 1
    if box_w >= W * 0.6:
        sys.stderr.write("REFUSED: 框宽 %d = 图宽 %d 的 %.0f%% —— 这是在框整行，不是框字段\n"
                         % (box_w, W, 100.0 * box_w / W))
        return 1
    return 0


if __name__ == "__main__":
    sys.exit(main())
