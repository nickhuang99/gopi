#!/usr/bin/env python3
"""渲染真值：判"标注真的画到字上了吗"——像素级，不是 OCR，也不是数标签个数。

用法: render-truth.py <work_dir> <materials_root> [--pattern '0[0-9][0-9]-notify-*']

对 materials_root 下每个匹配 pattern 的条目：
  * 必须存在 <work_dir>/<slug>.json（render.py 的像素测量结果）；
  * ink_in_box 非空 —— 至少有一处标注落进了图里；
  * changed_pixels > 0 —— 标注图与原始图确实不同（等于"画上了"）；
  * 每处标注的 ink_under_mark >= MIN_INK —— 标注底下真有字（框到空白上就是假标注）。

退出码 0 = 真值成立；1 = 有假标注（把"我以为画上了"变成硬错误）。
"""
import argparse
import glob
import json
import os
import sys

MIN_INK = 20


def main() -> int:
    ap = argparse.ArgumentParser()
    ap.add_argument("work_dir")
    ap.add_argument("materials_root")
    ap.add_argument("--pattern", default="0[0-9][0-9]-*",
                    help="glob pattern under materials_root")
    a = ap.parse_args()

    bad, checked = [], 0
    for d in sorted(glob.glob(os.path.join(a.materials_root, a.pattern))):
        if not os.path.isdir(d):
            continue
        name = os.path.basename(d)
        slug = name.split("-", 1)[1]
        j = os.path.join(a.work_dir, slug + ".json")
        if not os.path.exists(j):
            bad.append((name, "渲染器没有产出 json —— 标注图不可信"))
            continue
        m = json.load(open(j))
        marks = m.get("ink_in_box") or []
        checked += 1
        if not marks:
            bad.append((name, "没有任何标注落进图里"))
            continue
        if not m.get("changed_pixels"):
            bad.append((name, "标注图与原始图像素完全相同 —— 等于没画"))
        for k in marks:
            if (k.get("ink_under_mark") or 0) < MIN_INK:
                bad.append((name, "label %r 底下只有 %s 个墨点 —— 框到空白上了"
                            % (k.get("label"), k.get("ink_under_mark"))))
        print("  %-38s marks=%d changed_pixels=%-6s ink=%s"
              % (name, len(marks), m.get("changed_pixels"),
                 [k.get("ink_under_mark") for k in marks]))

    if checked == 0:
        print("RENDER TRUTH FAIL: 一条都没量到 —— 前缀/目录口径不对", file=sys.stderr)
        return 1
    if bad:
        print("RENDER TRUTH FAIL:", file=sys.stderr)
        for s, why in bad:
            print("  %s: %s" % (s, why), file=sys.stderr)
        return 1
    print("  render truth: %d 条，每个声明过的标注都落在了有墨的行上" % checked)
    return 0


if __name__ == "__main__":
    sys.exit(main())
