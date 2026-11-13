#!/usr/bin/env python3
"""materials-render-D.py — 把一条素材条目的 01/02 变成 03-raw.png + 04-annotated.png。

与 scripts/materials/render.py（A 面）的区别，也是本脚本存在的理由：
**图必须是一张真实运行截图**，不是 text2img 的字体渲染。D 面（隔离与恢复）的证据里有
沙盒路径、退出码、信号死亡这些"运行过"才有的东西，所以像素要来自一个真的跑着命令的终端：

  xterm（离屏 Xvfb）→ gopi capture --screen → 裁到窗口 → 03-raw.png
                    → gopi annotate 在同一批像素上画框 → 04-annotated.png

四条来自实测的设计（别"优化"掉）：

1. **离屏显示**（Rule 22 / Rule 10.2）：操作者的 DISPLAY 上跑着他自己的桌面，在上面开窗
   取证 = 闯进别人的工作区。一律 Xvfb :77，pid 落文件——`pkill -f 'Xvfb :77'` 会匹配到
   跑 pkill 的那个 shell 自己的 cmdline，整个 bash 调用被自杀（实测 exit=-1）。

2. **几何全靠量**：xterm 的窗口大小/位置不归我控制（无窗口管理器时 -geometry 只是建议，
   实测请求 144x8 得到约 174 列），所以坐标一律从**标定图**里量出来：同一窗口跑一张
   6 行 'H'*8 的先导图，量 y0（首行墨迹顶）、pitch（行距）、cell_w（字宽）、x_left。
   行 → 像素的映射由这三个数算出，不猜。

3. **窗口映射是有延迟的**：capture 早于窗口 map 就得到一张全黑图（实测踩过）。所以
   截图要**等稳定**：连续两次截图的 sha256_16 相同、且图里有墨迹，才算落定。

4. **自证**：每个框内原本必须有墨迹（PIL 数），annotate 必须真的改了像素（它自己的
   ink_pixels）。任一为 0 ⇒ exit 1，不产出那张图。框住空白的标注图比没有图更坏——
   它看起来像证据。

用法（由 scripts/materials/gen-taskD.sh 调用；单条可手动复跑）:
  materials-render-D.py --entry-dir DIR --sb SB --cwd CWD --cmd '…' \
      --select-text 'T:<substring>:<cols>:<label>' \
      --caption     'T:<substring>:0:<解说文字>' \
      --raw OUT.png --ann OUT.png --manifest OUT.json
  01-input.txt / 02-output.txt 从 --entry-dir 读；--scroll N 从正文头部剥掉 N 行
  （02 里被 head 截断的命令行；01 已经独立于 02，两者都对得上）。
"""
import argparse
import hashlib
import json
import os
import re
import signal
import subprocess
import sys
import time

from PIL import Image, ImageChops

COLS = 150          # 折行列数（显示宽度；CJK 算 2 格）
GEOM_COLS = COLS + 6   # 给 xterm 的建议列数（它可能给得更多，多不是问题）
FONT_SIZE = 12
FONT = "Noto Sans Mono CJK SC"
GLYPH = "H"
DISPLAY = os.environ.get("GOPI_MATERIALS_DISPLAY", ":79")
SCREEN = "1400x1400x24"
MAX_LINES = 38      # 图上最多画的正文行数；超出部分在图上明写被截断
# ---- needle 定位的三种结局（Rule 15：语义不同的失败绝不许共用一句话）---------
# 缺陷（2026-10-06，bug 01a11341）：needle 曾经在「正文按 --max-lines 截断**之后**」
# 的窗口里找，于是「全文里根本没有这个词」与「有，但落在窗口之外画不到」共用一句
# "matched no rendered line" —— 读者按文案去查拼写与环境，永远查不到真因。
NEEDLE_FOUND = "found"           # 在（折行后的）正文里：payload = 段号
NEEDLE_SPLIT_BY_WRAP = "split"   # 只在原文里，横跨折行断点：payload = 原文行号
NEEDLE_ABSENT = "absent"         # 全文里就没有：payload = None
CAPTION_FLAG = "--caption"
SELECT_FLAG = "--select-text"
BODY_MIN_ROWS = 8                # 图上一页至少要画这么多段正文（再少就不成图了）
TAG_TEXT = "T"                   # 标注规格的 tag：T:<needle>:…
TOOL_VERB = ("tool", "capture")  # 截图动词（Rule 1：CLI 动词不许裸写；退休的扁平拼法
                                 # `gopi capture` 会让整条渲染静默失败在「终端没画出来」上）
TAG_SPLIT_MAX = 3                # spec.split(":", 3)：解说文字里可以带冒号
FMT_NEEDLE_ABSENT = (
    "FATAL: %s %r 在**全文**里没有出现（正文 %d 行，折行后 %d 段）—— 这是"
    "\"这一页的输入里根本没有这个 needle\"，不是画不下。先核对 needle 与"
    " 02-output.txt 的拼写/取值，别改环境。")
FMT_NEEDLE_SPLIT = (
    "FATAL: %s %r 只出现在原文第 %d 行，而那一行被终端宽度折成了 %d 段、"
    "needle 横跨断点 —— 图上的任何一段都不完整含它。换一个不跨段的 needle。")
FMT_SPAN_BEYOND_PAGE = (
    "FATAL: 被标注的那些行彼此离得太远，一页放不下：%r 在第 %d 行、%r 在第 %d 行，"
    "跨度 %d 段，而这一页只画得下 %d 段正文（页容量 %d 行 − 解说占 %d 行 − 截断说明 %d 行），"
    "画得到的最远一段是第 %d 段。这不是 needle 拼错（两个都在全文里），也不是 --max-lines "
    "太小 —— 是这张图一页装不下这么长的跨度：拆成两条素材，或把终端窗口调高 / 字号调小。")
FMT_NEEDLE_BEYOND_PAGE = (
    "FATAL: %s %r 存在（原文第 %d 行，折行后第 %d 段），但它落在这一页画得到的地方"
    "之外：窗口从第 %d 段起，一页可画 %d 行（页容量 %d 行 − 解说占 %d 行），"
    "只画到第 %d 段。这不是 needle 拼错（它在全文里），也不是 --max-lines 太小"
    "（窗口已经跟着它走）—— 是这一页放不下：把终端窗口调高 / 字号调小，"
    "或换一个落在前 %d 段里的 needle。")
TRUNC_BLOCK = ("",                 # 图上的截断说明前留一个空行
               "…… 图到此为止（终端一页上限）：正文折行后共 %d 段，这一页从第 %d 段起"
               "画了 %d 段（窗口跟着被标注的那一行走）。",
               "完整的 verbatim 输出见同目录 02-output.txt —— 图是它的一个窗口，不是替代品。")
SETTLE_TRIES = 40   # 等待窗口映射+画完的上限（× 0.4s）
PAINT_LUMA = 200    # "窗口画好了没"的判据：CJK 字形是深色像素，取自 INK_LUMA 同档
PAINT_MIN_PX = 200  # 一页正文至少要有这么多深色像素才算画好了（窗口边框/白底不算）
MAX_TERM_ROWS = 300      # 终端窗口最多这么高（离屏虚拟屏 5000px 高，一页放得下）
CELL_W_PX_DEFAULT = 8    # xterm 12px 等宽字形的 cell（resize increments 不可读时的兜底）
CELL_H_PX_DEFAULT = 16
WIDEN_TRIES = 3     # 行→像素映射不成立时，把窗口加宽重下的次数上限
WIDEN_STEP_COLS = 40
SCRATCH = "/home/nick/.gopi/sessions/01a0ed23-04b0-7af2-8ac5-eafe05cc25bd/materials-scratch"

# ---- 标注框的结构判据（Rule 10.2「渲染真值」）与框的贴合常数 ----------------
# 判据问的不是"框够不够贴目标行"（一个骑在两行之间、各压一半的框完全满足这种
# 比例判据——实测 001-sandbox-create 就这样漏过去了），而是三条**结构**命题：
#   ① 框的上下边所在的像素行，在框的列范围内没有墨迹；
#   ② 框内只有一个墨迹段（一个字段的笔画自己是连着的，两个字段之间有空白缝）；
#   ③ 框的垂直区间不与它**没指**的那些行的行槽相交。
INK_LUMA = 200      # 行→像素映射的墨迹阈值（含抗锯齿：细字形不能读成空白行）
EDGE_LUMA = 128     # 判据阈值：<128 的像素算墨迹（框线自己落在这一档上）
BOX_PAD = 3         # 框线离墨迹上下各留这么多像素（留白是算出来的，不是碰巧的）
SPAN_GAP = 4        # 墨迹行间隔 ≤ 这个值算同一段（一个字段的笔画是连着的）
SPAN_MAX_ROWS = 2   # 框内最多允许几段墨迹：>1 段 = 骑上另一行 → 拒绝出图
FRAME_TRIES = 4     # 框线在墨迹段外让位的次数上限（每次多让 1 像素）
SLOT_JITTER = 1     # 行槽的上下边各缩进这么多行再找墨迹段：行槽高 = 标定行距（取整过的
                    # 整数），而字形的栅格起点在小数上——实测 003 第 7 行的行槽 [183,207]
                    # 里，207（槽底那一行）是**下一行**字形的上缘，不是这一行的字段。
                    # BOX_PAD(3) > SLOT_JITTER(1)，所以框线离那一位仍有 ≥2 像素。


def sh(cmd, **kw):
    p = subprocess.run(cmd, capture_output=True, text=True, **kw)
    return p.returncode, p.stdout + p.stderr


def wrap(text, cols):
    """按显示宽度折行（CJK 占 2 格）——图里显示的就是文本本身，不重排。"""
    out = []
    for raw in text.split("\n"):
        if not raw:
            out.append("")
            continue
        line, w = "", 0
        for ch in raw:
            cw = 2 if ord(ch) > 0x1100 else 1
            if w + cw > cols:
                out.append(line)
                line, w = "", 0
            line += ch
            w += cw
        out.append(line)
    return out


def disp_width(s):
    return sum(2 if ord(ch) > 0x1100 else 1 for ch in s)


def parse_caption_specs(specs):
    """T:<needle>:0:<解说文字> → [(needle, 解说文字)]（解说文字里可以带冒号）。"""
    out = []
    for spec in specs:
        tag, needle, _z, text_c = spec.split(":", TAG_SPLIT_MAX)
        if tag != TAG_TEXT:
            raise SystemExit("FATAL: %s wants T:<needle>:0:<text>, got %r"
                             % (CAPTION_FLAG, spec))
        out.append((needle, text_c))
    return out


def parse_select_specs(specs):
    """T:<needle>:<cols>:<label> → [(needle, cols, label)]。"""
    out = []
    for spec in specs:
        tag, needle, cols, label = spec.split(":", TAG_SPLIT_MAX)
        if tag != TAG_TEXT:
            raise SystemExit("FATAL: %s wants T:<needle>:<cols>:<label>, got %r"
                             % (SELECT_FLAG, spec))
        out.append((needle, cols, label))
    return out


def locate_needle(needle, wrapped, logical):
    """needle 在**全文**里定位（不是在被 --max-lines 截断后的窗口里定位）。

    返回 (kind, payload)：NEEDLE_FOUND → 折行后的段号（第一处命中）；
    NEEDLE_SPLIT_BY_WRAP → 它在原文里的行号（needle 横跨折行断点，图上没有任何一段
    完整含它）；NEEDLE_ABSENT → None。三种结局在调用方给出三句语义不同的话。
    """
    hits = [i for i, l in enumerate(wrapped) if needle in l]
    if not hits:
        hits = [i for i, l in enumerate(wrapped) if loose_eq(needle, l)]
    if hits:
        return NEEDLE_FOUND, hits[0]
    for li, l in enumerate(logical):
        if needle in l or loose_eq(needle, l):
            return NEEDLE_SPLIT_BY_WRAP, li
    return NEEDLE_ABSENT, None


def beyond_page_msg(flag, needle, full_seg, dl_start, drawn_segs, page_rows, cap_rows,
                    logical_of_seg):
    """「needle 存在，但这一页画不到它」—— 明写它的段号与窗口画到的段号。"""
    last_seg = dl_start + drawn_segs
    return FMT_NEEDLE_BEYOND_PAGE % (flag, needle, logical_of_seg[full_seg] + 1, full_seg + 1,
                                     dl_start + 1, drawn_segs, page_rows, cap_rows,
                                     last_seg, last_seg)


def cut_to_width(s, w):
    out, used = "", 0
    for ch in s:
        cw = 2 if ord(ch) > 0x1100 else 1
        if used + cw > w:
            break
        out += ch
        used += cw
    return out


def wrap_display(s, w):
    """按显示宽度折行，优先在空格处断（避免把路径从中间劈开），断不了就硬断。
    续行缩进 2 格，读图的人一眼看得出这是同一行的延续。"""
    if w <= 4:
        return [s]
    lines, cur, curw = [], "", 0
    for ch in s:
        cw = 2 if ord(ch) > 0x1100 else 1
        if curw + cw > w:
            sp = cur.rfind(" ")
            if sp > w // 3:
                lines.append(cur[:sp])
                cur = "  " + cur[sp + 1:]
                curw = 2 + sum(2 if ord(c) > 0x1100 else 1 for c in cur[2:])
            else:
                lines.append(cur)
                cur, curw = "  ", 2
        cur += ch
        curw += cw
    if cur.strip():
        lines.append(cur)
    return lines or [""]


def loose_eq(a, b):
    """宽松匹配：比同一个东西的两种写法——CJK 用 U+FF1A/U+FF1B，行尾有空格，
    以及 02 里被折行的长命令行（两边都去掉空白后互为前缀即可）。"""
    a2 = re.sub(r"\s+", "", a)
    b2 = re.sub(r"\s+", "", b)
    if not a2 or not b2:
        return False
    n = min(len(a2), len(b2))
    return a2[:n] == b2[:n]


def sha16(path):
    return hashlib.sha256(open(path, "rb").read()).hexdigest()[:16]


def window_bbox(png, floor=8):
    """窗口的矩形 = 图上第一个非纯黑的像素范围。

    不要用墨迹 bbox 当窗口宽度：标定图只画了几个字形，墨迹宽度远小于窗口宽度
    （实测：600px vs 2660px），拿它算列数会把每行都当成"超宽"反复折行。"""
    im = Image.open(png).convert("L")
    w, h = im.size
    px = im.load()
    xs, ys = [], []
    for y in range(0, h, 2):
        for x in range(0, w, 2):
            if px[x, y] > floor:
                xs.append(x)
                ys.append(y)
    if not xs:
        return None
    return (min(xs), min(ys), max(xs), max(ys))


def ink_bbox(png):
    """非黑像素的外接矩形（离屏根窗口是纯黑，窗口是浅色）——用于裁定"图上有没有东西"。"""
    im = Image.open(png).convert("L")
    w, h = im.size
    px = im.load()
    xs, ys = [], []
    for y in range(0, h, 2):
        for x in range(0, w, 2):
            if px[x, y] > 60:
                xs.append(x)
                ys.append(y)
    if not xs:
        return None
    return (min(xs), min(ys), max(xs), max(ys))


def probe_geometry(title):
    """问 X11 拿终端真实的 cell 尺寸与列数。

    像素计数在这里是不靠谱的：相邻字形会连成一块，数出来的是"墨迹块"不是"列"。
    而 xterm 把 cell 尺寸发布在窗口的 WM_NORMAL_HINTS 里（xwininfo 打印为
    `resize increments`），Width/Height 是客户区像素——两者相除就是列数。
    标定用这条通道，行的宽度限制才是**终端自己的**宽度，而不是我猜的。"""
    rc, out = sh(["xwininfo", "-display", DISPLAY, "-root", "-tree"])
    if rc != 0:
        return None
    win_id = win_w = win_h = None
    for line in out.splitlines():
        if title in line and "xterm" in line:
            m = re.search(r"(0x[0-9a-f]+)\s+\"[^\"]*\".*?(\d+)x(\d+)\+", line)
            if m:
                win_id, win_w, win_h = m.group(1), int(m.group(2)), int(m.group(3))
    if not win_id:
        return None
    rc, out = sh(["xwininfo", "-display", DISPLAY, "-id", win_id, "-all"])
    cell_w, cell_h = CELL_W_PX_DEFAULT, CELL_H_PX_DEFAULT
    m = re.search(r"resize increments:\s*width:\s*(\d+)\s*height:\s*(\d+)", out)
    if m:
        cell_w, cell_h = int(m.group(1)), int(m.group(2))
    return {"win_id": win_id, "cell_w": cell_w, "cell_h": cell_h,
            "win_w": win_w, "win_h": win_h,
            "cols": max(20, win_w // cell_w), "rows": max(4, win_h // cell_h)}


def ink_runs(png, bbox, pad_x=10, min_px=3, min_h=8):
    """窗口区域里每一段连续含墨迹的行 = 一行文字。

    两个必须的过滤（都是实测踩出来的）：x 上留出 pad_x 的边（xterm 的边框/滚动条
    会画一条贯穿所有行的暗线，不过滤就会把整张图连成"一行"）；一行至少要有 min_px
    个暗像素才算文字（防单像素噪声）。"""
    im = Image.open(png).convert("L").crop((bbox[0], bbox[1], bbox[2] + 1, bbox[3] + 1))
    w, h = im.size
    x0, x1 = min(pad_x, w // 4), max(min(pad_x, w // 4), w - 2 * pad_x)
    px = im.load()
    counts = []
    for y in range(h):
        n = 0
        for x in range(x0, x1):
            if px[x, y] < 128:
                n += 1
        counts.append(n)
    runs, start = [], None
    for y, n in enumerate(counts):
        d = n >= min_px
        if d and start is None:
            start = y
        if not d and start is not None:
            runs.append((start, y - 1))
            start = None
    if start is not None:
        runs.append((start, h - 1))
    # 合并被空行切断的行（行距内的细缝不该被当成两行）
    merged = []
    for r in runs:
        if merged and r[0] - merged[-1][1] <= 2:
            merged[-1] = (merged[-1][0], r[1])
        else:
            merged.append(r)
    # 一行文字至少 min_h 高：窗口边框、滚动条、单像素噪声都过不了这一关
    # （实测：不滤掉它们，11 行正文会量出 13~15 段"墨迹"，映射当场失效）
    return [(a + bbox[1], b + bbox[1]) for a, b in merged if b - a + 1 >= min_h]


def ink_in_box(png, x, y, w, h):
    im = Image.open(png).convert("L")
    px = im.load()
    W, H = im.size
    n = 0
    for yy in range(max(0, y), min(H, y + h)):
        for xx in range(max(0, x), min(W, x + w)):
            if px[xx, yy] < 128:
                n += 1
    return n


# ---- 标注框的结构判据 ------------------------------------------------------
# 判据与测量共用这一套 reader：判据量的是**同一张未标注图**上的像素，不是标记
# 字符串里的数字——这样"框对不对"与"框怎么算出来的"不可能各说各话。
def measure_row_bands(png, y0_abs, oy, pitch, x0, x1, n_lines):
    """行槽**量出来**，不是从标定行距外推。返回 ({k: (t,b)}, 诊断列表)。

    为什么不能外推：`round(y0 + pitch*k)` 用的是**取整过的**行距（实测 25.0），
    而真实行距带小数——每行差零点几像素，到第 14 行就漂了 2 像素（实测
    001-sandbox-create：解说那一槽外推成 [358,382]，而这一行的墨迹实测是
    [356,374]——**行槽与它自己那一行对不上**）。同样 003 第 7 行：外推 [183,207]，
    实测 [183,199]，多出来的 207 是**下一行**字形的上缘。外推的行槽因此同时犯了
    两个错：把本行说矮了、把邻行说进来了——判据于是既可能误伤也可能漏放。

    所以行槽由像素定：量出全图的墨迹段（一行一段，行间空白 ≥ 行距的 1/5），
    每段按 `round((top - y0)/pitch)` 认领它属于哪一行。认领必须**恰好一一对应**
    （非空行各得一段、空行一段都没有），否则返回诊断，由调用方 FATAL ——
    绝不悄悄把段丢给相邻的行。
    """
    im = Image.open(png).convert("L")
    px = im.load()
    W, H = im.size
    x0, x1 = max(0, int(x0)), min(W, int(x1))
    segs = []
    for y in range(H):
        if any(px[x, y] < INK_LUMA for x in range(x0, x1)):
            if segs and y - segs[-1][1] <= SPAN_GAP:
                segs[-1][1] = y
            else:
                segs.append([y, y])
    bands, why = {}, []
    for t, b in segs:
        k = int(round((t + oy - y0_abs) / pitch))
        if k < 0 or k >= n_lines:
            why.append("墨迹段 [%d,%d] 认领到第 %d 行，而这一页只有 %d 行"
                       % (t, b, k, n_lines))
            continue
        if k in bands:
            why.append("两段墨迹（%s 与 [%d,%d]）认领同一行 k=%d —— 行→像素映射不成立"
                       % (list(bands[k]), t, b, k))
            continue
        bands[k] = (t, b)
    return bands, why


def ink_rows_in_box(png, x, y, w, h, thresh):
    """框内每一行的墨迹像素数 {行号: 个数}。"""
    im = Image.open(png).convert("L")
    px = im.load()
    W, H = im.size
    out = {}
    for yy in range(max(0, y), min(H, y + h)):
        n = 0
        for xx in range(max(0, x), min(W, x + w)):
            if px[xx, yy] < thresh:
                n += 1
        out[yy] = n
    return out


def ink_spans(rows, gap=SPAN_GAP):
    """把"有墨迹的行号"归并成**墨迹段** [top, bottom]。
    一个字段的笔画自己是连着的（字形的纵向笔画贯穿好几行），两个字段之间有
    行间空白（实测 8 像素），所以"段数"直接对应"框里压着几个字段"。"""
    spans = []
    for y in sorted(k for k, n in rows.items() if n > 0):
        if spans and y - spans[-1][1] <= gap:
            spans[-1][1] = y
        else:
            spans.append([y, y])
    return [(a, b) for a, b in spans]


def structural_check(png, box, row_bands, row, label=None):
    """框的**结构**判据：三条像素命题，全部成立才算 ok。

    ① 框的**上下边**所在像素行，在框的列范围内没有墨迹（EDGE_LUMA 以下算墨迹）
       —— 这正是"框不该穿过任何笔画"，也是"骑在两行之间"这个形状的**直接**反面。
    ② 框内恰好**一个**墨迹段 —— 一个字段的笔画是连着的；框里出现两段，说明它
       上下各压住了一个字段。
    ③ 归属唯一：框内那一个墨迹段必须落在**它指的那一行**的行槽里。②只保证了"框里
       只有一个字段"，③保证"这个字段就是它指的那一个"——框整行漂一格时是②过了、
       ③失败，而那种图看起来也完全正常。

    为什么不能只用比例（"与目标行重叠 ≥ 一半、与别的行重叠 ≤ 一半"）：一个**骑在
    两行之间、每行各压一半**的框恰好落在比例判据的阈值上，判定与浮点取整的边界
    重合时它就滑过去了——实测 001-sandbox-create 的旧图正是这样漏过去的（框
    [55,85]：与目标行 [58,82] 重叠 25/25，与下一行 [83,107] 重叠 3/25，两个条件
    都"刚好"满足）。比例问的是"够不够贴"，结构问的是"有没有压到别的行"。
    """
    x, y, w, h = box
    top_row, bot_row = y, y + h - 1
    top_ink = ink_rows_in_box(png, x, top_row, w, 1, EDGE_LUMA).get(top_row, 0)
    bot_ink = ink_rows_in_box(png, x, bot_row, w, 1, EDGE_LUMA).get(bot_row, 0)
    inside = ink_rows_in_box(png, x, y, w, h, EDGE_LUMA)
    spans = ink_spans(inside)
    band = row_bands.get(row)
    in_band = bool(spans) and band is not None and \
        band[0] <= spans[0][0] and spans[0][1] <= band[1]
    why = []
    if top_ink:
        why.append("上边(y=%d)在框的列范围内切到 %d 个墨迹像素" % (top_row, top_ink))
    if bot_ink:
        why.append("下边(y=%d)在框的列范围内切到 %d 个墨迹像素" % (bot_row, bot_ink))
    if len(spans) != 1:
        why.append("框内有 %d 段墨迹（要求恰好 1 段）：%s" % (len(spans), spans[:4]))
    if spans and not in_band:
        why.append("框内那段墨迹 %s 不在第 %d 行的行槽 %s 里（框漂到了别的行）"
                   % (spans[0], row, list(band) if band else None))
    return {"label": label, "row": int(row), "box": [int(v) for v in box],
            "box_edge_rows": [top_row, bot_row],
            "ink_on_top_edge": top_ink, "ink_on_bottom_edge": bot_ink,
            "spans_inside": [list(s) for s in spans],
            "row_band": list(band) if band else None,
            "span_in_target_band": in_band,
            "verdict": "ok" if not why else "MISALIGNED",
            "why": why}


def fit_vert_to_ink(png, x0, x1, band, row):
    """用**这一行实际的墨迹**定框的上下边：返回 (top, bot, None) 或 (None, None, 诊断)。

    x0/x1 是框的**列范围**（框只框到这一行的字段为止），band 是该行**按标定行距
    推出的行槽** [t, b]。行槽用来**限定搜索范围**（哪个字段属于这一行），
    但**不用来定框的边**——框的边由像素定。

    为什么行槽不能当框的边：行槽高 = 标定行距（实测 25px），而字形的墨迹只有
    17px 高，且基线在槽内的位置取决于字体——行槽的下边落在大约"基线下方 8px"，
    那是个**碰巧**的位置：某些字形（带下伸部、下标）就在那个高度上有笔画。一个被
    比例判据接受、却让框线穿过笔画的框，根源就在这里（实测 001-sandbox-create：
    框下边 y=85 穿进下一行的字形，[83,85] 被算成"邻行重叠 3px"，只占 25px 行高
    的 12%，于是 `other <= hh*0.5` 放行）。所以框必须由像素定：先量出这一行的
    墨迹段，再把框线放到它的上下留白里。
    """
    rows = ink_rows_in_box(png, x0, band[0] + SLOT_JITTER, x1 - x0,
                           band[1] - band[0] + 1 - 2 * SLOT_JITTER, INK_LUMA)
    spans = ink_spans(rows)
    if len(spans) != 1:
        return None, None, ("第 %d 行的行槽 [%d,%d] 里有 %d 段墨迹（要求恰好 1 段，"
                            "多段说明行槽跨了两行）：%s"
                            % (row, band[0], band[1], len(spans), spans[:4]))
    top, bot = spans[0]
    if bot - top + 1 > SPAN_MAX_ROWS * (band[1] - band[0] + 1):
        return None, None, ("第 %d 行的墨迹段有 %d 行高（行距只有 %d 行）——这不是"
                            "一个字段" % (row, bot - top + 1, band[1] - band[0] + 1))
    return top, bot, None


def frame_around(png, row, band, x, cols, cell_w, row_run, label, kind, text_cols):
    """算出一个**结构上站得住**的框：(x, y, w, h)，或 (None, 诊断)。

    框的**列范围**由调用方给出（`x` = 框左边界，宽度按声明的列数算：框只框到这一
    行的字段为止），**上下边**由像素定：量出这一行在列范围内的墨迹段，再把框线放进
    它外面的空白里；这一对边不行就让得更远一点（BOX_PAD 起，最多 FRAME_TRIES 次）。
    让不出去（图边、邻行贴太近）就返回诊断，由调用方 FATAL —— 绝不悄悄把框挪到
    别的行上，也绝不"手调坐标"绕过判据。

    这是**唯一的**框算法：生成器与回归测试都走这里（判据与框不可能各说各话）。
    """
    im = Image.open(png).convert("L")
    px = im.load()
    W, H = im.size
    if row not in row_run:
        return None, ("标注要指第 %d 行，而图上没有这一行（映射表只有 %d 行）"
                      % (row, len(row_run)))
    bw = int(min(W - x - 4, cols * cell_w + 14))
    top, bot, why = fit_vert_to_ink(png, x, x + bw, band, row)
    if why:
        return None, why
    cols_ink = [xx for xx in range(max(0, x), min(W, x + bw))
                if any(px[xx, yy] < INK_LUMA for yy in range(top, bot + 1))]
    if not cols_ink:
        return None, ("第 %d 行在列范围 [%d,%d] 里没有墨迹——框会框住空白"
                      % (row, x, x + bw))
    x_end = min(W - 1, max(cols_ink) + BOX_PAD)
    for pad in range(BOX_PAD, BOX_PAD + FRAME_TRIES):
        y, y2 = top - pad, bot + pad
        if y < 0 or y2 > H - 1:
            continue
        # 判据 ①：框的上下边所在像素行，在框的列范围内没有墨迹。
        if ink_rows_in_box(png, x, y, x_end - x + 1, 1, EDGE_LUMA).get(y, 0):
            continue
        if ink_rows_in_box(png, x, y2, x_end - x + 1, 1, EDGE_LUMA).get(y2, 0):
            continue
        return {"x": x, "y": y, "w": x_end - x + 1, "h": y2 - y + 1, "row": int(row),
                "text_cols": text_cols, "label": label, "kind": kind}, None
    return None, ("第 %d 行的框在墨迹段 [%d,%d] 外面让位 %d 次都找不到一对干净的"
                  "上下边（邻行贴得太近）" % (row, top, bot, FRAME_TRIES))


def ensure_xvfb(scratch):
    # 显示已经在（比如由 job(action=start) 起的常驻 Xvfb）就直接用——
    # 反复起 Xvfb 是这一路上最贵的坑：它会随调用方的进程组一起死，
    # 而在一个不可靠的显示上取证，量出来的一切都是错的。
    if sh(["xdpyinfo", "-display", DISPLAY])[0] == 0:
        return 0, False
    pidfile = os.path.join(scratch, "xvfb-%s.pid" % DISPLAY.strip(":"))
    if os.path.exists(pidfile):
        try:
            pid = int(open(pidfile).read().strip())
            os.kill(pid, 0)
            return pid, False
        except (ValueError, ProcessLookupError, PermissionError):
            pass
    p = subprocess.Popen(["Xvfb", DISPLAY, "-screen", "0", SCREEN],
                         stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL,
                         start_new_session=True)
    with open(pidfile, "w") as f:
        f.write(str(p.pid))
    for _ in range(40):
        time.sleep(0.25)
        if sh(["xdpyinfo", "-display", DISPLAY])[0] == 0:
            return p.pid, True
    raise SystemExit("FATAL: Xvfb %s did not come up" % DISPLAY)


def kill_strays(marker):
    """清掉上一轮遗留的、属于**这一张离屏显示**的 xterm。

    不这么做的话：一次崩溃留下的旧窗口还在，ink_bbox 量到的是所有窗口的并集，
    标定量出来的 y0/pitch 就是错的（实测：pitch 23.6、y0 10、字宽 17.2，
    全部来自另一个窗口的残留内容）。

    绝不用 `pkill -f '<pattern>'`：它会匹配到跑 pkill 的那个 shell 自己的 cmdline，
    整个 bash 调用被自杀（实测 exit=-1）。改成逐个 /proc/<pid>/cmdline 精确匹配，
    并且跳过自己。
    """
    me = os.getpid()
    for pid in os.listdir("/proc"):
        if not pid.isdigit() or int(pid) == me:
            continue
        try:
            with open("/proc/%s/cmdline" % pid, "rb") as f:
                cmd = f.read().decode("utf-8", "replace")
        except OSError:
            continue
        if "xterm" in cmd and marker in cmd:
            try:
                os.kill(int(pid), signal.SIGTERM)
            except OSError:
                pass
    time.sleep(0.4)


def sh_quote(s):
    return "'" + s.replace("'", "'\\''") + "'"


def build_term_file(path, lines):
    """正文按行写到终端的绝对位置（CSI 定位）：空行、长行、回卷都不会让
    第 N 行跑到别处去，行→像素的映射必须是确定的。"""
    # \033[?7l = 关闭自动换行（autowrap off）：过长的行会被终端在右缘截掉，
    # 而不是折成两行。这样"一个逻辑行 = 一个屏幕行"由终端保证，
    # 行→像素的映射不再依赖我对终端宽度的猜测（实测：猜错就会框错地方）。
    parts = ["\033[?7l\033[2J"]
    for i, line in enumerate(lines):
        parts.append("\033[%d;1H" % (i + 1))
        parts.append(line)
    with open(path, "w") as f:
        f.write("".join(parts))


def dark_pixels(png, x0=0, y0=0, x1=None, y1=None, thresh=PAINT_LUMA):
    """图上有多少**深色**像素 —— "窗口画好了没"的量。"""
    im = Image.open(png).convert("L")
    W, H = im.size
    px = im.load()
    x1 = W if x1 is None else min(W, x1)
    y1 = H if y1 is None else min(H, y1)
    n = 0
    for y in range(max(0, y0), y1):
        for x in range(max(0, x0), x1):
            if px[x, y] < thresh:
                n += 1
    return n


def capture_screen(bin_path, out):
    """等窗口映射并画完：连续两次截图的 sha16 **与窗口几何**都相同、且窗口里已经有
    真正的字形，才算落定。

    只看 sha 不够——窗口映射过程中窗口会移动/改尺寸，两个不同瞬间也可能撞出同一张图；
    而几何一旦不稳，后面所有像素坐标都是错的。

    "有非黑像素"同样不够（实测 2026-09-30）：xterm 的窗口刚映射出来是**白底空窗**，
    ink_bbox 看到白底就报"有墨迹"，于是截图在 xterm 还没画字的时候就被接受了 ——
    002 那一张的屏幕只有两个颜色（纯黑 + 纯白），一个字形都没有，随后所有坐标全错。
    判据因此改成"窗口矩形内深色像素 ≥ PAINT_MIN_PX"：白底不含深色像素，只有字形算。"""
    prev = None
    last = ""
    for _ in range(SETTLE_TRIES):
        time.sleep(0.4)
        rc, out_s = sh([bin_path] + list(TOOL_VERB) + ["--display", DISPLAY, "--screen", "--out", out])
        if rc != 0:
            # 截图失败要留话：只报"终端没画出来"会把真因（动词退休 / 显示不可达 /
            # 屏幕被别的 run 搅乱）整个藏起来 —— 实测这一句让人找错了方向。
            last = "capture rc=%d: %s" % (rc, (out_s.strip().splitlines() or [""])[-1][:300])
            continue
        bb = ink_bbox(out)
        if not bb:
            prev = None
            last = "capture ok, but the screen has no window with ink"
            continue
        painted = dark_pixels(out, bb[0], bb[1], bb[2] + 1, bb[3] + 1)
        if painted < PAINT_MIN_PX:
            prev = None
            last = ("capture ok, window present but only %d dark pixel(s) "
                    "(< %d = still a blank white window)" % (painted, PAINT_MIN_PX))
            continue
        cur = (sha16(out), bb, painted)
        if cur == prev:
            return json.loads(out_s.strip().splitlines()[-1])
        prev = cur
        last = "capture ok, painted=%d, but the sha/geometry did not repeat (not settled)" % painted
    raise SystemExit("FATAL: the terminal never settled on %s (or never painted) — last state: %s"
                     % (DISPLAY, last))


def shoot(term_file, rows, args, out):
    """起 xterm 渲染 term_file，等它画出稳定的画面，截全屏。"""
    kill_strays(args.marker)
    env = dict(os.environ)
    env.update(LANG="C.UTF-8", LC_ALL="C.UTF-8")   # 不设这个，CJK 会渲染成乱码
    title = "%s %s" % (args.marker, args.win)
    xlog = os.path.join(args.scratch, "%s.%s.xterm.log" % (args.entry, args.win))
    logf = open(xlog, "w")
    proc = subprocess.Popen(
        ["xterm", "-display", DISPLAY, "-fa", FONT, "-fs", str(FONT_SIZE),
         "-geometry", "%dx%d" % (GEOM_COLS + int(getattr(args, "cols_extra", 0)),
                                 min(rows + 2, MAX_TERM_ROWS)), "-T", title,
         "-e", "bash", "-c", "cat %s; sleep 60" % sh_quote(term_file)],
        stdout=logf, stderr=logf, start_new_session=True, env=env)
    try:
        geo = probe_geometry(title)
        if geo:
            args.geo = geo
        return capture_screen(args.bin, out)
    finally:
        try:
            os.killpg(os.getpgid(proc.pid), signal.SIGTERM)
        except ProcessLookupError:
            pass
        time.sleep(0.3)


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("--entry-dir", required=True)
    ap.add_argument("--sb", required=True)
    ap.add_argument("--cwd", required=True)
    ap.add_argument("--cmd", required=True)
    ap.add_argument("--body", default=None,
                    help="图上正文的来源；默认 02-output.txt 去掉 --scroll 的头")
    ap.add_argument("--scroll", type=int, default=0)
    ap.add_argument("--raw", required=True)
    ap.add_argument("--ann", required=True)
    ap.add_argument("--manifest", required=True)
    ap.add_argument("--select-text", action="append", default=[],
                    help="T:<substring>:<cols>:<label> —— 按内容找行来框（找不到即失败，"
                         "绝不静默不框；不写死行号，因为行号一改框就框到别处）")
    ap.add_argument("--caption", action="append", default=[],
                    help="T:<substring>:0:<解说文字> —— 在那一行下面放一段解说，"
                         "解说自己用 6x6 方块自证像素")
    ap.add_argument("--bin", default="gopi")
    ap.add_argument("--max-lines", type=int, default=MAX_LINES)
    ap.add_argument("--scratch", default=SCRATCH)
    ap.add_argument("--marker", default="gopi-materials-D",
                    help="这个窗口的属主标记（kill_strays 只杀带标记的 xterm）")
    ap.add_argument("--win", default="win",
                    help="本条目这一轮的唯一窗口标记，拼进 -T 标题")
    args = ap.parse_args()

    os.makedirs(args.scratch, exist_ok=True)
    slug = os.path.basename(args.entry_dir.rstrip("/"))
    args.entry = slug
    stem = os.path.join(args.scratch, slug)
    body = args.body or os.path.join(args.entry_dir, "02-output.txt")
    raw02 = open(body, "rb").read().decode("utf-8", "replace")
    text = "\n".join(raw02.split("\n")[args.scroll:]) if args.scroll else raw02

    ensure_xvfb(args.scratch)
    kill_strays(args.marker)

    # ---- 标定：同一窗口先跑一张已知内容的图，量出 y0 / pitch / cell_w / x_left
    cal_png = stem + ".cal.screen.png"
    cal_txt = stem + ".cal.term"
    build_term_file(cal_txt, [GLYPH * 8] * 6)
    ca = dict(args.__dict__)
    ca["bin"] = args.bin
    ca["geo"] = None
    shoot(cal_txt, 6, type("A", (), ca), cal_png)
    bb = ink_bbox(cal_png)
    if not bb:
        raise SystemExit("FATAL: calibration screen has no window on it")
    geo = getattr(args, "geo", None) or {"cols": 0, "cell_w": 0, "cell_h": 0,
                                         "win_w": 0, "win_h": 0, "win_id": None}
    runs = ink_runs(cal_png, bb)
    if len(runs) < 3:
        raise SystemExit("FATAL: calibration found %d ink runs (want >=3)" % len(runs))
    y0 = runs[0][0]
    pitch = (runs[-1][0] - runs[0][0]) / float(len(runs) - 1)
    line_ink_h = max(b - t + 1 for t, b in runs)
    # 字宽：一行 72 个 H 的墨迹宽度 / 72（同一窗口，同一字体）
    wide_png = stem + ".wide.screen.png"
    wide_txt = stem + ".wide.term"
    build_term_file(wide_txt, [GLYPH * 72])
    shoot(wide_txt, 1, type("A", (), ca), wide_png)
    wb = ink_bbox(wide_png)
    im = Image.open(wide_png).convert("L")
    px = im.load()
    cols72 = range(wb[0], wb[2] + 1)
    x_left = min(x for x in cols72 if any(px[x, y] < 128 for y in range(wb[1], wb[3] + 1)))
    x_right = max(x for x in cols72 if any(px[x, y] < 128 for y in range(wb[1], wb[3] + 1)))
    cell_w = float(geo["cell_w"]) if geo.get("cell_w") else (x_right - x_left + 1) / 72.0
    if cell_w <= 0:
        raise SystemExit("FATAL: measured a zero cell width")
    # 终端一行能放几个字符：窗口可视宽度 / 一个 ASCII 字符的像素宽（就是上面
    # 'H'*72 量出来的那个）。超过它的行会被终端**折行**，于是"一个逻辑行 = 一个
    # 屏幕行"不再成立，下面的所有行都会整体错位一格——实测：294 字符的行在
    # ~154 列的窗口上折行，把后面的标注框全部推歪。所以先截断（带省略号，
    # 并在报告里记下截了几行），再渲染。
    wbb = window_bbox(cal_png) or bb
    win_px = (wbb[2] - wbb[0] + 1) if wbb else 0
    char_px = (x_right - x_left + 1) / 72.0
    term_cols = int((win_px - x_left - 12) / char_px) if char_px > 0 else 0
    if term_cols <= 0:
        raise SystemExit("FATAL: could not derive the terminal's column count")

    # ---- 正文 ----------------------------------------------------------
    # 不自己折行：终端就是权威。脚本自己 wrap 会与终端再折一次叠加，行→像素
    # 的映射当场失效（实测：34 行量出 32 段墨迹）。交给终端，宽度由它决定。
    dl = text.rstrip("\n").split("\n")
    total = len(dl)
    # **未折行**的原文行：needle 恰好跨在折行断点上时，图上没有任何一段完整含它 ——
    # 那一句错报（"换一个不跨段的 needle"）只能靠这份原文认出来。必须在折行**之前**
    # 留档（实测：折行后再 list(dl)，split 这条路径永远不会触发，于是"跨断点"
    # 被误报成"全文里没有"—— 假阴性，比原来的歧义更坏）。
    logical = list(dl)
    # 终端到底有多宽：标定图的 'H'*8 一行里数得出 H 的个数（cell 单位=2 个 ASCII）。
    # 超过它的行必须**显式**截断并留一个省略号——否则终端替我折行，行→像素的
    # 映射就不可信了，而那正是标注框会"看起来正常但框错地方"的原因。

    truncated_lines = 0
    fixed = []
    for l in dl:
        if term_cols and disp_width(l) > term_cols:
            # 自己折行（不是截断）：证据不能被截掉。折出来的续行由**我**记账，
            # 所以"第 k 行 ↔ 第 k 个格子"这条映射仍然精确——真正危险的是让终端
            # 替我折（它的折法我看不见，映射会整体漂一格）。
            parts_w = wrap_display(l, term_cols)
            fixed.extend(parts_w)
            truncated_lines += len(parts_w) - 1
        else:
            fixed.append(l)
    dl = fixed
    # 按原文行折行成的段（图上的"正文段"），一行原文 ∈ [segs_of_line[i], segs_of_line[i+1])。
    # 页窗口只截一段前缀，所以"原文行号"与"段号"是两条不同的坐标，各要各的账。
    segs_of_line = [0]
    for l in dl:
        segs_of_line.append(segs_of_line[-1] + (len(wrap_display(l, term_cols)) if term_cols else 1))
    logical_of_seg = []
    for i, l in enumerate(dl):
        logical_of_seg.extend([i] * (segs_of_line[i + 1] - segs_of_line[i]))

    # 解说行：插在正文**之后**的独立行槽里（绝对定位，不与正文抢行）。
    # needle 在**全文**（折行后的 dl）里定位 —— 缺陷的根本原因就在这一步曾经发生在
    # 截断之后：那时"没有这个词"和"这一页画不到它"已经分不出来了。
    caps_req = parse_caption_specs(args.caption)
    sels_req = parse_select_specs(args.select_text)
    full_seg_of = {}
    for flag, needle in ([(CAPTION_FLAG, n) for n, _t in caps_req] +
                         [(SELECT_FLAG, n) for n, _c, _l in sels_req]):
        if needle in full_seg_of:
            continue
        kind, payload = locate_needle(needle, dl, logical)
        if kind == NEEDLE_FOUND:
            full_seg_of[needle] = payload
        elif kind == NEEDLE_SPLIT_BY_WRAP:
            raise SystemExit(FMT_NEEDLE_SPLIT % (flag, needle, payload + 1, len(wrap_display(dl[payload], term_cols))))
        else:
            raise SystemExit(FMT_NEEDLE_ABSENT % (flag, needle, total, len(dl)))

    # ---- 一页能画多少行（页容量）----------------------------------------
    # 这一步必须在窗口之前：页容量才是窗口的**唯一硬边界**。
    screen_h = 0
    rc, out = sh(["xdpyinfo", "-display", DISPLAY])
    m = re.search(r"dimensions:\s*(\d+)x(\d+)", out)
    if rc == 0 and m:
        screen_h = int(m.group(2))
    # 一屏放得下几行：用**量出来的行距**（pitch），不要用 xterm 声称的 cell 高度
    # ——实测 cell_h 报 0/16，而真实行距是 20~25，按小的估会算出 247 行的窗口，
    # 屏幕根本画不下（第 120 行往后全在图外，解说框会算出负高度）。
    row_px = int(round(pitch)) + 2
    room = ((screen_h - 60) // row_px) if screen_h else MAX_TERM_ROWS
    page_rows = max(8, min(MAX_TERM_ROWS, room))

    # ---- 正文窗口：跟着 needle 走，硬边界是页容量 ------------------------
    # 缺陷的第二个原因：窗口曾经只画 dl 的前 --max-lines 段。正文长度是变量（新增
    # 一行启动日志就把它整体下推），于是被标注的那一行会自己跌出窗口 —— 而 needle
    # 的拼写没有问题。--max-lines 现在只是**上下文预算**（被标注的行之上留多少行），
    # 页容量才是硬边界：needle 落在预算之外不再是错误。
    #
    # 窗口的规则只有两条：
    #   ① 每一个被标注的段都必须在窗口里（禁止把被标注的行丢在图画得到的范围之外）；
    #   ② 在①之下，尽量多留上下文：窗口末尾贴着**最后一个**被标注的段，再往上补
    #      到上下文预算；补不进（上面没有那么多行）就顶到正文开头。
    # 两条放在一起才是一个**确定**的窗口：被标注的段跨度超过一页画得下的行数时，
    # 没有任何窗口能装下它们 —— 那才是真正的"画不到"，明确拒绝并说出段号。
    cap_rows_max = 2 * len(caps_req) + 1
    # 容量分两档：窗口能从正文开头起时不需要那 3 行截断说明（`dl_start == 0`），
    # 需要时才扣。曾经无条件扣掉 → 004 被 3 行之差误拒（实测：跨度 48 段、
    # 不扣说明的容量 50 段，扣了就只剩 47）。
    avail_full = page_rows - cap_rows_max
    avail_note = avail_full - len(TRUNC_BLOCK)
    marked = sorted(set(full_seg_of.values()))
    dl_start = 0
    if marked:
        cap_for_fit = avail_full if marked[-1] + 1 <= avail_full else avail_note
        span = marked[-1] - marked[0] + 1
        if span > cap_for_fit:
            lo_n = next(n for n, s in full_seg_of.items() if s == marked[0])
            hi_n = next(n for n, s in full_seg_of.items() if s == marked[-1])
            raise SystemExit(FMT_SPAN_BEYOND_PAGE % (
                lo_n, logical_of_seg[marked[0]] + 1, hi_n, logical_of_seg[marked[-1]] + 1,
                span, cap_for_fit, page_rows, cap_rows_max, len(TRUNC_BLOCK),
                marked[0] + cap_for_fit))
        room_segs = max(BODY_MIN_ROWS, min(args.max_lines or cap_for_fit, cap_for_fit))
        room_segs = max(room_segs, span)
        dl_start = min(marked[0], max(0, marked[-1] + 1 - room_segs))

    # 解说：段号 → 窗口内的段号。窗口与 needle 的关系只由这一步决定，
    # 于是"画不到"再次成为**可判定的**事实（而不是被截断掩盖的歧义）。
    caps = []
    for needle, text_c in caps_req:
        caps.append((full_seg_of.get(needle), text_c, needle))

    # ---- 画到页容量为止，且**在整行边界上**停 ---------------------------
    # 停在整行边界，行→像素的映射才是精确的：一行的下半截被屏幕切掉会让
    # "第 k 行占 [y0+k*pitch, …)" 这条等式失效。剩下的行不画，并且明确记账
    # （02-output.txt 是全文，图是它的一页窗口——素材契约就是这么写的）。
    # 页容量 page_rows 是这里唯一的硬边界；窗口起点 dl_start 由被标注的行决定，
    # 于是"needle 落在 --max-lines 之后"不再是一种失败。
    cap_rows = 2 * len(caps) + 1
    note_rows = len(TRUNC_BLOCK) if dl_start else 0
    body_segs = 0
    used_rows = 0
    body_lines = []
    for k, l in enumerate(dl[dl_start:], start=dl_start):
        w_line = len(wrap_display(l, term_cols)) if term_cols else 1
        if used_rows + w_line > page_rows - cap_rows - note_rows:
            break
        body_lines.extend(wrap_display(l, term_cols) if term_cols else [l])
        used_rows += w_line
        body_segs += 1
    page_cut = len(dl) - (dl_start + body_segs)
    if note_rows:
        body_lines = body_lines + [TRUNC_BLOCK[0],
                                   TRUNC_BLOCK[1] % (len(dl), dl_start + 1, body_segs),
                                   TRUNC_BLOCK[2]]
    all_lines = list(body_lines) + [""] * (2 * len(caps) + 2)
    # 解说必须落在**画出来的**那些段上：窗口起点是跟着被标注的行算出来的，所以正常
    # 情况下它就在那里；确实放不下时（容量判断与实际分页不一致）明说段号与画到的
    # 段号，绝不把框悄悄挪到别的段上。
    cap_slots = []
    for i, (seg, text_c, needle) in enumerate(caps):
        slot = len(body_lines) + 1 + 2 * i
        all_lines[slot] = text_c
        idx = None if seg is None else seg - dl_start
        if idx is None or not (0 <= idx < body_segs):
            # 容量判断已经放行、分页却没收下：这是**我的**账算错了，不是素材的问题，
            # 所以把它当内部不一致报出来（带两档容量），而不是伪装成 needle 画不到。
            raise SystemExit(
                "FATAL: 内部不一致：容量判断放行了段 %d（窗口从第 %d 段起，画了 %d 段），"
                "分页却停在 %d 段（页容量 %d，解说 %d 行，说明 %d 行）。"
                % (seg, dl_start + 1, body_segs, dl_start + body_segs, page_rows,
                   cap_rows, note_rows))
        cap_slots.append((slot, text_c, idx))

    # ---- 渲染 + 行→像素映射 --------------------------------------------
    # 映射不用"数墨迹段"（细字形如 `{` 整行不够高、光标块会多出一段——实测两者
    # 都会让段数对不上），而是用**标定出来的格子**：第 k 行占 [y0+k*pitch, …)。
    # 再用"非空行必须有墨迹、空行必须没有"自证这张映射——它对细字形与光标免疫，
    # 且不成立时会明说，绝不悄悄框错地方。
    nonempty = [i for i, l in enumerate(all_lines) if l.strip()]
    body_txt = stem + ".term"
    build_term_file(body_txt, all_lines)
    raw_screen = stem + ".body.screen.png"
    ca = dict(args.__dict__)
    ca["bin"] = args.bin
    ca["geo"] = None
    want_rows = len(all_lines) + 1
    shoot(body_txt, want_rows, type("A", (), ca), raw_screen)
    snap = Image.open(raw_screen).convert("RGB")
    wb_body = window_bbox(raw_screen)
    if not wb_body:
        raise SystemExit("FATAL: the body screen has no window on it")
    scr_w, scr_h = snap.size
    # 裁到**窗口**（不是墨迹）：窗口底部若还有空白行，墨迹 bbox 会在最后一行墨迹
    # 处收尾，把窗口里剩下的行切掉——那些行随后会被报告成"非空行落在图外"。
    crop = (max(0, wb_body[0]), max(0, wb_body[1]),
            min(scr_w, wb_body[2] + 1), min(scr_h, wb_body[3] + 1))
    snap.crop(crop).save(args.raw)
    ox, oy = crop[0], crop[1]
    W, H = Image.open(args.raw).size

    n_lines = len(all_lines)
    row_bands, band_why = measure_row_bands(args.raw, y0, oy, pitch,
                                            x_left - ox, W - 8, n_lines)
    if band_why:
        raise SystemExit("FATAL: 行→像素映射不自洽（行槽按墨迹段量出，不是从行距外推）："
                         "%s [img=%dx%d pitch=%.3f y0=%s]" % (band_why[:6], W, H, pitch, y0))
    # 只校验到**最后一个非空行**为止：它下面的行槽是刻意的留白，而 shell 的光标
    # 块正好停在那里（一个 6x16 的实心块），把留白变成"有墨迹"——那不是错位。
    # 图上到底有几行：窗口**内部**高度 ÷ 行距。窗口高度从屏幕坐标量（含边框），
    # 边框/内边距 = 标定的 y0 与窗口顶之间的距离，两侧对称。
    # 不要用 H 直接反推（H 之外还有边框），也不要用 want_rows（窗口放不下的行
    # 终端根本不画——实测：给了 149 行、屏幕只放得下 ~120 行，第 120 行往后全在图外）。
    border = max(0, int(y0) - int(wb_body[1]))
    win_h_px = int(wb_body[3]) - int(wb_body[1]) + 1
    rows_on_page = max(1, int((win_h_px - 2 * border) // max(1, int(round(pitch)))))
    off_page = [k for k in nonempty if k >= rows_on_page]
    last_content = min(max([k for k in nonempty if k < rows_on_page] or [0]), rows_on_page - 1)
    # 每个**非空行**都必须量到它自己的那一段墨迹（量不到 = 它被画到了图外，或被
    # 判成了别的行——两者都是"映射失效"，必须说出来，不许拿外推值补上）。
    missing = [k for k in nonempty if k <= last_content and k not in row_bands]
    if missing:
        raise SystemExit("FATAL: 这些非空行没量到自己的墨迹段（行→像素映射不成立）：%s"
                         " [img=%dx%d pitch=%.3f y0=%s oy=%s x0=%s x1=%s term_cols=%s"
                         " claimed=%s]"
                         % (missing[:6], W, H, pitch, y0, oy, int(x_left - ox), W - 8,
                            term_cols, sorted(row_bands.items())[:8]))
    row_run = row_bands

    # ---- 裁到窗口（含边距）：像素仍来自真实帧缓冲，只是取证据所在这块矩形 ----
    bb = ink_bbox(raw_screen)
    if not bb:
        raise SystemExit("FATAL: the body screen has no window on it")
    scr_w, scr_h = Image.open(raw_screen).size
    # 边距必须为 0：屏幕在窗口之外是纯黑（<128），带边距裁进来会被当成"墨迹"，
    # 于是行→像素的映射里混进几段假行（实测：11 行正文量出 14 段）。
    mx, my = 0, 0
    crop = (max(0, bb[0] - mx), max(0, bb[1] - my),
            min(scr_w, bb[2] + mx), min(scr_h, bb[3] + my))
    Image.open(raw_screen).convert("RGB").crop(crop).save(args.raw)
    ox, oy = crop[0], crop[1]
    W, H = Image.open(args.raw).size

    # 正文段号 → 画出来的行号：窗口就是从 dl 的第 dl_start 段起的，body_lines 里
    # 按段顺序排，所以画出来的第 k 段就是 dl 的第 dl_start+k 段。窗口外的段
    # （被页容量截掉的尾部）不在这张表里 —— 那正是"画不到"的判据。
    drawn_lo = dl_start
    drawn_hi = dl_start + body_segs

    boxes = []
    for needle, cols, label in sels_req:
        seg = full_seg_of.get(needle)
        if seg is None or not (drawn_lo <= seg < drawn_hi):
            # 窗口起点是跟着被标注的段算出来的，所以走到这里 = 内部不一致；仍然把
            # 段号与画到的范围说出来（但不谎称是 needle 的问题）。
            raise SystemExit(
                "FATAL: 内部不一致：%s %r 落在第 %d 段，而画出来的是第 %d–%d 段"
                "（页容量 %d，解说 %d 行）。" % (SELECT_FLAG, needle, (seg + 1) if seg is not None else 0,
                                                 drawn_lo + 1, drawn_hi, rows_on_page, cap_rows))
        row = seg - dl_start
        n = min(int(cols), len(re.sub(r"\s+$", "", body_lines[row])))
        b, why = frame_around(args.raw, row, row_run[row], max(0, int(x_left - ox) - 4),
                              max(n, 12), cell_w, row_run, label, "select", cols)
        if why:
            raise SystemExit("FATAL: %s %r（第 %d 行）的框算不出来：%s"
                             % (SELECT_FLAG, needle, row, why))
        boxes.append(b)

    for slot, text_c, row in cap_slots:
        dispw = sum(2 if ord(ch) > 0x1100 else 1 for ch in text_c)
        b, why = frame_around(args.raw, slot, row_run[slot], max(0, int(x_left - ox) - 4),
                              max(12, int(dispw + 2)), cell_w, row_run, "", "caption", 0)
        if why:
            raise SystemExit("FATAL: 解说行（槽 %d）的框算不出来：%s" % (slot, why))
        b["text"] = text_c
        b["explains_row"] = row
        boxes.append(b)

    if not boxes:
        raise SystemExit("FATAL: no marks — the entry would ship an unannotated image")

    # 判据**先**跑：structural_check 量的是未标注图上的墨迹，所以它能在画框之前
    # 就裁决。不成立 = 拒绝出图（图看起来正常但是错的，只有量能发现）。
    closed = []
    for b in boxes:
        c = structural_check(args.raw, (b["x"], b["y"], b["w"], b["h"]),
                             row_run, b["row"], b.get("label"))
        c["text_cols"] = b["text_cols"]
        c["kind"] = b["kind"]
        closed.append(c)
    bad = [c for c in closed if c.get("verdict") != "ok"]
    if bad:
        raise SystemExit("FATAL: 结构判据不成立（框线会穿过墨迹 / 框里压着不止一个"
                         "字段 / 框漂到了别的行）：%s" % bad)

    # 判据成立 → 由 boxes 渲染 marks（框是**算出来的**，marks 只是它的字符串形状）。
    checks_out = [{"label": b.get("label") or "caption slot %d" % b.get("slot", 0),
                   "row": b["row"], "kind": b["kind"],
                   "box": [b["x"], b["y"], b["w"], b["h"]],
                   "ink_under_mark": ink_in_box(args.raw, b["x"], b["y"], b["w"], b["h"])}
                  for b in boxes]
    empty = [c for c in checks_out if c["ink_under_mark"] == 0]
    if empty:
        raise SystemExit("REFUSED: mark(s) boxed no ink — %s" % empty)
    marks = []
    for b in boxes:
        label = b.get("label") or ""
        marks.append("rect:%dx%d+%d+%d:%s:fixed" % (b["w"], b["h"], b["x"], b["y"], label))
        if b["kind"] == "caption":
            marks.append("point:6x6+%d+%d::fixed" % (min(W - 10, b["x"] + b["w"] + 8), b["y"] + 2))
    log = []
    for b in boxes:
        ent = {"kind": b["kind"], "row": b["row"], "y": b["y"], "h": b["h"],
               "box_x": b["x"], "box_w": b["w"], "cols": b["text_cols"]}
        if b["kind"] == "caption":
            ent["slot"] = b["row"]
            ent["text"] = b.get("text", "")
            ent["explains_row"] = b.get("explains_row")
        else:
            ent["label"] = b.get("label", "")
        log.append(ent)

    cmd = [args.bin, "tool", "annotate", args.raw, "--out", args.ann,
           "--font", "/usr/share/fonts/truetype/wqy/wqy-zenhei.ttc",
           "--size", "15", "--json"]
    for m in marks:
        cmd += ["--mark", m]
    rc, out = sh(cmd)
    if rc != 0 or not os.path.exists(args.ann):
        raise SystemExit("FATAL: annotate rc=%d: %s" % (rc, out))
    try:
        ares = json.loads(out.strip().splitlines()[-1])
    except Exception:
        ares = {"raw": out.strip()}
    # ---- 自证（画完之后）------------------------------------------------
    # 判据在**未标注图**上已经裁决过（未标注图的墨迹才是证据）；这里量标注图，只为
    # 证明"声明的框真的画在了它声称的位置上"：每一条的上下边所在像素行，在框的列范围
    # 内真的有一道线（未标注图那里本来就是空白 —— 判据①要求的正是这个）。
    # 没有这一步，manifest 就可能描述一张**没画出来的**图，而下游（回归测试）看不出来。
    drawn = []
    for b in boxes:
        y2 = b["y"] + b["h"] - 1
        t = ink_rows_in_box(args.ann, b["x"], b["y"], b["w"], 1, EDGE_LUMA).get(b["y"], 0)
        bo = ink_rows_in_box(args.ann, b["x"], y2, b["w"], 1, EDGE_LUMA).get(y2, 0)
        if t == 0 or bo == 0:
            raise SystemExit("REFUSED: 声明的框没有被画出来（%s 第 %d 行的上下边在标注图"
                             "上是空的）—— 标注与 manifest 不一致"
                             % (b.get("label"), b["row"]))
        drawn.append({"row": b["row"], "edge_rows": [b["y"], y2], "edge_px": [t, bo]})

    raw_im = Image.open(args.raw).convert("RGB")
    ann_im = Image.open(args.ann).convert("RGB")
    if raw_im.size != ann_im.size:
        raise SystemExit("FATAL: annotate changed the size: %s -> %s" % (raw_im.size, ann_im.size))
    diff = ImageChops.difference(raw_im, ann_im)
    bbox = diff.getbbox()
    changed = 0
    if bbox:
        d = diff.crop(bbox).convert("L").point(lambda v: 255 if v > 8 else 0).histogram()
        changed = d[255]

    man = {
        "entry": slug, "body": body, "cmd": args.cmd, "sb": args.sb, "cwd": args.cwd,
        "metrics": {"pitch": pitch, "y0": y0, "cell_w": cell_w, "x_left": x_left,
                    "line_ink_h": line_ink_h, "wrap_cols": COLS, "font_px": FONT_SIZE,
                    "display": DISPLAY, "screen": SCREEN},
        "lines_total": total, "lines_rendered": len(all_lines),
        "lines_truncated": dl_start,
        "term_cols": term_cols, "lines_clipped_to_terminal": truncated_lines,
        "window_start_seg": dl_start, "body_segs": body_segs,
        "marked_segs": {n: s for n, s in full_seg_of.items()},
        "lines_beyond_page": page_cut,
        "rows_on_page": rows_on_page, "page_rows": want_rows,
        "terminal_geometry": geo,
        "scroll_skipped": args.scroll,
        "shot": {"path": args.raw, "size": [W, H], "sha256_16": sha16(args.raw),
                 "crop_from_screen": crop if "crop" in dir() else None,
                 "screen_capture_path": raw_screen, "screen_capture_sha16": sha16(raw_screen)},
        "annotated": {"path": args.ann, "size": list(ann_im.size),
                      "sha256_16": sha16(args.ann), "annotate": ares},
        "marks": marks, "selectors": log, "boxes": boxes, "ink_in_box": checks_out,
        "frame_drawn": drawn,
        "map_check": closed, "row_bands": {str(k): v for k, v in row_run.items()},
        "changed_pixels": changed, "changed_bbox": bbox,
    }
    with open(args.manifest, "w") as f:
        json.dump(man, f, ensure_ascii=False, indent=2)

    if man["shot"]["sha256_16"] == man["annotated"]["sha256_16"]:
        raise SystemExit("FATAL: raw and annotated have the same sha — nothing was annotated")
    if changed == 0:
        raise SystemExit("REFUSED: annotate changed 0 pixels")
    print(json.dumps({"entry": slug, "pitch": round(pitch, 2), "y0": y0,
                      "cell_w": round(cell_w, 2), "marks": len(marks), "changed_pixels": changed,
                      "raw_sha16": man["shot"]["sha256_16"],
                      "ann_sha16": man["annotated"]["sha256_16"], "size": [W, H]},
                     ensure_ascii=False))
    return 0


if __name__ == "__main__":
    sys.exit(main())
