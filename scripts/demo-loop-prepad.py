#!/usr/bin/env python3
"""demo-loop-prepad.py — 在标注行上方补空行，并把补好的文本写到 stdout。

为什么要单独一步：`gopi annotate` 把标签芯片画在框**上方**（实测带高 ~39px），
而 render.py 自己也会给标注行腾空——但它是**先算好所有 needle 的行号、再逐条插空行**，
插进去的空行会让后面 needle 的行号整体下移 ⇒ 后一条的"行号"指向了前一条的标签带，
自证（`label_covered_ink == 0`）就会拒绝产出（实测 190 像素压住证据）。

所以：**在把文本交给 render.py 之前**就把空行插好，让 render.py 的腾空逻辑变成空操作。
插空行要在 render.py 的 wrap() 之后、按**渲染行号**插，并且按 needle 的行号**升序**处理、
同步累计偏移量（原样照抄 docs/archive/demo/materials 生成器里那段经过验证的做法，只是把顺序补正）。

用法: demo-loop-prepad.py <text-file> <needle> ...
stdin 也可，但本脚本只从文件读（调用方先落盘，图与文本都可复核）。
"""
import importlib.util
import os
import sys

HERE = os.path.dirname(os.path.abspath(__file__))
RENDER = os.path.join(HERE, "materials", "render.py")
SEPARATOR = "02-output.txt"
RULE = "─"
BLANKS = 2          # 补两个空行（60px > 标签带 ~39px）


def load_render():
    spec = importlib.util.spec_from_file_location("materials_render", RENDER)
    mod = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(mod)
    return mod


def main():
    if len(sys.argv) < 3:
        sys.stderr.write(__doc__)
        return 2
    path, needles = sys.argv[1], sys.argv[2:]
    render = load_render()
    lines = render.wrap(open(path, encoding="utf-8").read().rstrip("\n"), render.COLS)
    floor = 0
    for i, l in enumerate(lines):
        if SEPARATOR in l and l.lstrip().startswith(RULE):
            floor = i + 1
            break
    picks = []
    for n in needles:
        if ":" in n:
            sys.stderr.write("REFUSED: needle 含冒号会被 render.py 的规格切错: %r\n" % n)
            return 1
        hits = [i for i, l in enumerate(lines) if n in l and i >= floor]
        if not hits:
            sys.stderr.write("REFUSED: needle 在证据段里找不到: %r\n" % n)
            return 1
        picks.append((hits[0], n))
    # 升序处理 + 累计偏移：插进去的空行会让后面 needle 的行号下移，必须补上。
    picks.sort()
    offset = 0
    for idx, n in picks:
        at = idx + offset
        lines[at:at] = [""] * BLANKS
        offset += BLANKS
        sys.stderr.write("prepad: %r 行 %d → 标注行 %d（上方补 %d 空行）\n" % (n, idx, at + BLANKS, BLANKS))
    sys.stdout.write("\n".join(lines) + "\n")
    return 0


if __name__ == "__main__":
    sys.exit(main())
