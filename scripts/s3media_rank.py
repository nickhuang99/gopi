#!/usr/bin/env python3
"""从 s3log_stats 的聚合 JSON 里抽出媒体类流量（视频/音频/大文件）。

动机：按 egress 排名时我只看了 top-25 前缀，而 video/ 这类可能被
二级前缀切散（video/wechat/xxx.mp4 会归到 "video/wechat"），也可能
单个大文件藏在长尾里。所以必须【按扩展名】再统计一遍——这才是
"媒体文件到底吃了多少出网流量"的正确口径。
"""
import argparse
import json
import sys
from collections import defaultdict

PRICE_GB_OUT = 0.09
GB = 1e9

MEDIA_EXT = {
    ".mp4", ".mkv", ".avi", ".mov", ".webm", ".flv", ".wmv", ".m4v", ".mpg",
    ".mpeg", ".rmvb", ".ts", ".vob", ".3gp",
    ".mp3", ".wav", ".flac", ".aac", ".ogg", ".m4a", ".wma", ".ape", ".opus",
    ".pdf", ".epub", ".mobi", ".djvu",          # 文档大件也拉出来对比
}


def ext_of(key: str) -> str:
    k = key.lower().split("?")[0].split("#")[0]
    i = k.rfind(".")
    return k[i:] if i > 0 and "/" not in k[i:] else ""


def main(argv=None):
    # 用 argparse 而非裸 sys.argv：这样测试能直接驱动 main(argv)，
    # 也让 --help 可用（裸 sys.argv[1] 无法从测试喂参数）。
    ap = argparse.ArgumentParser(
        description="Break an S3 log aggregation down by file extension")
    ap.add_argument("agg_json", help="output of s3log_stats.py --json")
    args = ap.parse_args(argv)
    path = args.agg_json
    with open(path) as f:
        d = json.load(f)

    total = d["total_bytes"]
    keys = d.get("key", {})          # 全量 key → [count, bytes]

    by_ext = defaultdict(lambda: [0, 0])
    media_keys = []
    for k, v in keys.items():
        if not (isinstance(v, list) and len(v) >= 2):
            continue
        e = ext_of(k)
        by_ext[e][0] += v[0]
        by_ext[e][1] += v[1]
        if e in MEDIA_EXT:
            media_keys.append((k, v[0], v[1]))

    print("=" * 84)
    print("BY FILE EXTENSION — all keys, ranked by EGRESS BYTES")
    print("=" * 84)
    print(f"{'ext':<12}{'reqs':>10}{'GB':>10}{'share':>9}{'egress $':>12}")
    print("-" * 84)
    for e, (c, b) in sorted(by_ext.items(), key=lambda kv: -kv[1][1])[:25]:
        label = e if e else "(none)"
        print(f"{label:<12}{c:>10,}{b/GB:>10.4f}{b/total*100:>8.2f}%"
              f"{b/GB*PRICE_GB_OUT:>12.4f}")
    print("-" * 84)
    print(f"{'TOTAL':<12}{sum(v[0] for v in by_ext.values()):>10,}"
          f"{total/GB:>10.4f}{100:>8.2f}%{total/GB*PRICE_GB_OUT:>12.4f}")

    msum = sum(r[2] for r in media_keys)
    print()
    print("=" * 84)
    print("MEDIA/DOCUMENT FILES ONLY (video, audio, pdf, epub...)")
    print("=" * 84)
    print(f"  {len(media_keys)} distinct files, {msum/GB:.4f} GB, "
          f"{msum/total*100:.2f}% of egress, ${msum/GB*PRICE_GB_OUT:.4f}/day")
    print()
    print(f"{'bytes':>13}{'GB':>9}{'reqs':>7}  key")
    print("-" * 84)
    for k, c, b in sorted(media_keys, key=lambda r: -r[2])[:30]:
        print(f"{b:>13,}{b/GB:>9.4f}{c:>7}  {k[:60]}")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
