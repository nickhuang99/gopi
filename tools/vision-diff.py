#!/usr/bin/env python3
"""vision-diff.py — 独立读图模型（vision）读出的帧文字 vs 原始终端文本，逐字对齐并像素裁决。

用法:
  python3 tools/vision-diff.py <baseline.txt> <vision-read.txt>
  python3 tools/vision-diff.py <baseline.txt> <vision-read.txt> <frame.png> <cols> <cell_w> <cell_h> <dx> <dy>

vision 模型不做像素比对，它「认字」，所以它的输出是**独立第三方**的读法；代价是它会
顺手「纠正」字符（实测：— 读成 -、; 读成 ,、9 读成 0、. 读成 _，也会在空字段上补一个 -）。

两层，各说各的：
  1. 对齐层：逐行（**保持空行**）比对，报出每一处差异并归类；
  2. 裁决层：**不信任何一方的说法**，回到 gif 自己的像素上读取争议格：
       · 行号 = baseline 文件的行号 = **帧里的行号**（所以空行必须保留，否则行号会错位）；
       · 列号 = 该行内的字符列号；
       · 取格原点用 tools/gif-verify.py 标定出的 (dx,dy)；
       · 该格的字形补丁必须与「baseline 那个字符」在本帧别处的补丁逐字节相同，
         且与「vision 声称的字符」的补丁不同。
     两者同时成立 ⇒ 这处差异是**读图模型的**，不是 gif 的。

最终裁决权在 tools/gif-verify.py（它在像素上证明每一格画的就是文本里那个字符）。
"""
import sys
from difflib import SequenceMatcher

DASHES = "—-–"
BLANK_EPS = 2


def file_lines(p):
    """按帧的语义分行：保留空行（空行也是帧里的一行），去掉结尾换行产生的那一行。"""
    lines = [l.rstrip() for l in open(p, encoding="utf-8").read().split("\n")]
    if lines and lines[-1] == "":
        lines.pop()
    return lines


def classify(a, b):
    if a == b:
        return "same"
    if a in DASHES and b == "-":
        return "em-dash read as hyphen"
    if a.replace(" ", "") == b.replace(" ", ""):
        return "whitespace/alignment drift"
    return "character substitution"


def adjudicate(frame, baseline, cols, cw, ch, dx, dy, vision_path):
    import numpy as np
    from PIL import Image

    a = np.asarray(Image.open(frame).convert("L")) > 60
    L = file_lines(baseline)
    print(f"\n裁决层 — 回到 gif 自己的像素（{frame}，网格原点 dx={dx} dy={dy}）")

    def cell(r, c):
        y, x = r * ch + dy, c * cw + dx
        return a[y:y + ch, x:x + cw]

    # 规范字形：每个字符在本帧里出现过的所有补丁（跳过空格格）
    canon = {}
    for r, line in enumerate(L):
        for c, chx in enumerate(line):
            if chx == " ":
                continue
            p = cell(r, c)
            if p.sum() > BLANK_EPS:
                canon.setdefault(chx, set()).add(p.tobytes())

    # 行对齐：帧行号 = L 的行号；读图侧按顺序跳过空行取内容行
    V = [l for l in file_lines(vision_path)]
    drift = bad = 0
    vj = 0
    for r, base in enumerate(L):
        while vj < len(V) and V[vj].strip() == "" and base.strip() != "":
            vj += 1                     # 读图侧漏掉的空行
        vis = V[vj] if vj < len(V) else ""
        vj += 1
        if base.strip() == "" and vis.strip() == "":
            continue
        if base == vis:
            continue
        for tag, i1, i2, j1, j2 in SequenceMatcher(None, base, vis).get_opcodes():
            if tag == "equal":
                continue
            exp, got = base[i1:i2], vis[j1:j2]
            if exp.strip() == "" or got.strip() == "":
                drift += 1              # 排版漂移（对齐空白/列宽），不是字符主张
                continue
            if not exp:                 # 读者凭空多出来的字符：它落的那一格必须是空的
                for c in {max(0, i1 - 1), min(len(base) - 1, i1)}:
                    p = cell(r, c)
                    blank = p.sum() <= BLANK_EPS
                    if not blank:
                        bad += 1
                    print(f"  line {r} col {c}: baseline {base[c]!r}, reader ADDED {got!r} "
                          f"-> cell is {'blank' if blank else 'INKED'}  "
                          f"[{'screen is BLANK — the reader invented it' if blank else 'AMBIGUOUS'}]")
                continue
            for c in range(i1, i2):
                chx = base[c]
                p = cell(r, c)
                if chx == " ":
                    # baseline 在这里没有墨；读者却读出了字符 → 只问那一格是不是空的
                    blank = p.sum() <= BLANK_EPS
                    if not blank:
                        bad += 1
                    print(f"  line {r} col {c}: baseline <space>, reader read {got!r} "
                          f"-> cell is {'blank' if blank else 'INKED'}  "
                          f"[{'screen is BLANK — the reader invented it' if blank else 'AMBIGUOUS'}]")
                    continue
                real = sorted(k for k, v in canon.items() if p.tobytes() in v)
                good = chx in real and not any(g in real and g != chx for g in got)
                if not good:
                    bad += 1
                print(f"  line {r} col {c}: baseline {chx!r}, reader read {got!r} "
                      f"-> the cell is {real!r}  "
                      f"[{'screen draws the BASELINE character' if good else 'AMBIGUOUS'}]")
    print(f"  排版漂移（对齐空白/列宽，非字符主张）：{drift} 处，不裁决")
    print(f"  每一处字符争议都由 gif 自己的像素判给了 baseline："
          f"{'YES' if bad == 0 else f'NO ({bad})'}")
    return bad == 0


def main():
    baseline, read = sys.argv[1], sys.argv[2]
    L, V = file_lines(baseline), file_lines(read)
    content_L = [l for l in L if l.strip()]
    content_V = [l for l in V if l.strip()]
    print(f"baseline : {len(L)} lines ({len(content_L)} with content)")
    print(f"vision   : {len(V)} lines ({len(content_V)} with content)")
    same = sum(1 for x, y in zip(content_L, content_V) if x == y)
    print(f"byte-identical content lines: {same} / {len(content_L)}")

    kinds, n_char = {}, 0
    for i, (x, y) in enumerate(zip(content_L, content_V)):
        if x == y:
            continue
        for tag, i1, i2, j1, j2 in SequenceMatcher(None, x, y).get_opcodes():
            if tag == "equal":
                continue
            xa, yb = x[i1:i2], y[j1:j2]
            if xa.strip() == "" or yb.strip() == "":
                k = "whitespace/alignment drift"
            else:
                k = classify(xa, yb)
                n_char += 1
                print(f"  line {i}: {xa!r} -> {yb!r}   [{k}]")
            kinds[k] = kinds.get(k, 0) + 1
    print("\n分类:")
    for k, v in sorted(kinds.items(), key=lambda kv: -kv[1]):
        print(f"  {k:34s} {v}")

    if len(sys.argv) >= 9:
        ok = adjudicate(sys.argv[3], baseline, int(sys.argv[4]), int(sys.argv[5]),
                        int(sys.argv[6]), int(sys.argv[7]), int(sys.argv[8]), read)
        sys.exit(0 if ok else 1)
    sys.exit(0 if n_char == 0 else 1)


main()
