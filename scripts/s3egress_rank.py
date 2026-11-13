#!/usr/bin/env python3
"""按【出网字节】排序热点 —— egress 是账单里最大的一项（$0.09/GB）。

与 s3log_stats.py 的区别：那个按请求数+字节双列展示，这个专门回答
"谁在吃掉出网流量"——所以只按字节排序，并直接把字节换算成美元。

输入：s3log_stats.py --json 的聚合结果（路径由参数给出）。
"""
import argparse
import json
import sys

PRICE_GB_OUT = 0.09            # us-east-1 internet egress
GB = 1e9


def show(title, rows, total_bytes, top=25):
    print()
    print("=" * 80)
    print(title)
    print("=" * 80)
    print(f"{'name':<40}{'reqs':>9}{'GB':>9}{'share':>8}{'egress $':>11}")
    print("-" * 80)
    for name, cnt, byt in rows[:top]:
        print(f"{name[:39]:<40}{cnt:>9,}{byt/GB:>9.3f}"
              f"{byt/total_bytes*100:>7.1f}%{byt/GB*PRICE_GB_OUT:>11.4f}")
    top_sum = sum(r[2] for r in rows[:top])
    print("-" * 80)
    print(f"{'TOP ' + str(top):<40}{'':>9}{top_sum/GB:>9.3f}"
          f"{top_sum/total_bytes*100:>7.1f}%{top_sum/GB*PRICE_GB_OUT:>11.4f}")
    print(f"{'ALL':<40}{'':>9}{total_bytes/GB:>9.3f}{100:>7.1f}%"
          f"{total_bytes/GB*PRICE_GB_OUT:>11.4f}")


def main(argv=None):
    # The aggregation JSON is produced by s3log_stats.py --json. Take the path
    # as an argument (required) — do NOT default to a /tmp path: /tmp lives on
    # the boot partition, and this repo hard-bans /tmp literals in scripts
    # (see lib/tmp_ban.py; the gate in internal/testutil enforces it).
    ap = argparse.ArgumentParser(
        description="Rank an S3 access-log aggregation by EGRESS bytes")
    ap.add_argument("agg_json", help="output of s3log_stats.py --json")
    ap.add_argument("--top", type=int, default=25)
    args = ap.parse_args(argv)
    path = args.agg_json
    top = args.top
    with open(path) as f:
        d = json.load(f)

    total = d.get("total_bytes", 0)
    if not total:
        raise SystemExit("no total_bytes in json")

    for dim, title in (
            ("prefix", "TOP PREFIXES BY EGRESS (2 levels) — where the bytes leave"),
            ("key", "TOP FULL KEYS BY EGRESS — single files worth blocking"),
    ):
        raw = d.get(dim, {})
        rows = [(k or "(none)", v[0], v[1]) for k, v in raw.items()
                if isinstance(v, list) and len(v) >= 2]
        rows.sort(key=lambda r: -r[2])
        show(title, rows, total)

    print()
    print("=" * 80)
    print("READ THIS")
    print("=" * 80)
    print(f"  1 day of this log = {total/GB:.3f} GB outbound = "
          f"${total/GB*PRICE_GB_OUT:.4f}")
    print(f"  scaled to a month (x30) = {total/GB*30:.0f} GB = "
          f"${total/GB*PRICE_GB_OUT*30:.2f}/month")
    print()
    print("  A directory with many requests but small bytes costs REQUEST money;")
    print("  a directory with few requests but large bytes costs EGRESS money.")
    print("  This report ranks by the second kind — egress is 60% of the bill.")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
