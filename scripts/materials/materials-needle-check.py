#!/usr/bin/env python3
"""materials-needle-check.py — 标注 needle 的渲染前预验（Rule 15：不静默）。

render.py 的 `--mark-match <needle>:<label>` 在**找不到行**时只往 stderr 打一行 note，
退出码仍是 0——于是整条素材静默少一张 04-annotated.png，而 census 只会看到「少一件」。
本脚本用与 render.py **逐字同一份** wrap()（import 同一文件，不复制实现）把
「解说 + 01-input + 02-output」折行一遍，然后要求 needle 真的出现在**证据段**
（02-output 分隔行之后）里。

用法: materials-needle-check.py <entry_dir> <needle>
退出码: 0 = needle 命中；1 = 没命中（调用方应 FATAL，不要静默继续）。
"""
import importlib.util
import os
import sys

HERE = os.path.dirname(os.path.abspath(__file__))
SEPARATOR = "02-output.txt"
RULE = "─"


def load_render():
    spec = importlib.util.spec_from_file_location("materials_render", os.path.join(HERE, "render.py"))
    mod = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(mod)
    return mod


def main():
    if len(sys.argv) != 3:
        sys.stderr.write("usage: materials-needle-check.py <entry_dir> <needle>\n")
        return 2
    entry_dir, needle = sys.argv[1], sys.argv[2]
    render = load_render()
    head = (
        "── 04-annotated.png ──\nx\n\n── 01-input.txt ──\n"
        + open(os.path.join(entry_dir, "01-input.txt"), encoding="utf-8").read()
        + "\n── " + SEPARATOR + " ──\n"
        + open(os.path.join(entry_dir, "02-output.txt"), encoding="utf-8").read()
    )
    lines = render.wrap(head, render.COLS)
    floor = 0
    for i, line in enumerate(lines):
        if SEPARATOR in line and line.lstrip().startswith(RULE):
            floor = i + 1
            break
    if any(needle in line for line in lines[floor:]):
        return 0
    sys.stderr.write("needle %r not in the rendered evidence section (%d lines)\n" % (needle, len(lines)))
    return 1


if __name__ == "__main__":
    sys.exit(main())
