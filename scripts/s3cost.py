#!/usr/bin/env python3
"""s3cost.py — 把 s3log_stats.py 的聚合结果换算成【真实账单估算】。

为什么要单独一个脚本（而不是塞进 stats）：
  账单 = 请求数 × 单价 + 传输字节 × 传输单价，**且单价取决于请求类型**
  （GET 便宜、PUT/LIST 贵、DELETE 免费、生命周期/归档请求另算）。
  实测账单对不上时，需要能一眼看出"哪一项在驱动"，所以按项拆开算。

口径（us-east-1 标准层，2026 官方 list price，元/美元按 7.2 换算只作参考）：
  请求：
    PUT/COPY/POST/LIST/HEAD-of-bucket  $0.005 / 1000
    GET/SELECT/其他                     $0.0004 / 1000
    DELETE / CANCEL                     免费
  传输（出网）：
    第 1 个 100GB/月（免费额度已取消，现在是 100GB 免费）
    $0.09 / GB  （0-10TB，互联网出口）
  另有：CloudFront/存储/其他不属于本 log。

诚实边界：
  - 本 log 只覆盖【这个 bucket 的访问】。若账单还包含其他 bucket / CloudFront /
    存储费用 / 请求费之外的项，本脚本看不到，必须说明。
  - 403/404 等【错误响应也计费】（S3 对任何到达它的请求收费），
    这是最容易被忽略的一项——本脚本单独列出。
"""
import argparse
import json
import sys

# S3 标准层 list price（us-east-1）
PRICE_REQ_CHEAP = 0.0004 / 1000      # GET/SELECT 类，每次
PRICE_REQ_EXPENSIVE = 0.005 / 1000   # PUT/COPY/POST/LIST 类，每次
PRICE_GB_OUT = 0.09                  # 互联网出口
FREE_GB_OUT = 100                    # 每月免费出口额度

CHEAP_OPS = ("GET", "SELECT")
FREE_OPS = ("DELETE", "CANCEL")


def classify(op: str) -> str:
    o = op.upper()
    if any(k in o for k in ("DELETE", "CANCEL")):
        return "free"
    if any(o.endswith(k) or k in o for k in CHEAP_OPS) and "LIST" not in o:
        return "cheap"
    return "expensive"


def main(argv=None) -> int:
    ap = argparse.ArgumentParser(description="Turn s3log_stats JSON into a bill estimate")
    ap.add_argument("stats_json", help="output of s3log_stats.py --json")
    args = ap.parse_args(argv)

    with open(args.stats_json) as f:
        d = json.load(f)

    ops = d.get("op", {})
    total_req = d.get("total_req", 0)
    total_bytes = d.get("total_bytes", 0)

    buckets = {"cheap": 0, "expensive": 0, "free": 0}
    for op, n in ops.items():
        buckets[classify(op)] += n

    req_cost = buckets["cheap"] * PRICE_REQ_CHEAP + buckets["expensive"] * PRICE_REQ_EXPENSIVE
    gb = total_bytes / 1e9
    billable_gb = max(gb - FREE_GB_OUT, 0)
    out_cost = billable_gb * PRICE_GB_OUT

    print("=" * 66)
    print("S3 BILL ESTIMATE (us-east-1 standard, list prices)")
    print("=" * 66)
    print(f"requests total      : {total_req:>14,}")
    print(f"  cheap (GET/…)     : {buckets['cheap']:>14,}  x ${PRICE_REQ_CHEAP*1000:.4f}/1k = ${buckets['cheap']*PRICE_REQ_CHEAP:>8.2f}")
    print(f"  expensive (PUT/…) : {buckets['expensive']:>14,}  x ${PRICE_REQ_EXPENSIVE*1000:.4f}/1k = ${buckets['expensive']*PRICE_REQ_EXPENSIVE:>8.2f}")
    print(f"  free (DELETE/…)   : {buckets['free']:>14,}")
    print()
    print(f"bytes out           : {total_bytes:>14,}  ({gb:,.2f} GB)")
    print(f"  free tier (100GB) : {min(gb, FREE_GB_OUT):>14,.2f} GB")
    print(f"  billable          : {billable_gb:>14,.2f} GB  x ${PRICE_GB_OUT}/GB = ${out_cost:>8.2f}")
    print()
    print(f"── ESTIMATED TOTAL  : ${req_cost + out_cost:,.2f}")
    print()
    print("DRIVER ANALYSIS — which term dominates:")
    if req_cost + out_cost > 0:
        rp = req_cost / (req_cost + out_cost) * 100
        print(f"  request charges    : {rp:5.1f}%  (${req_cost:,.2f})")
        print(f"  transfer charges   : {100-rp:5.1f}%  (${out_cost:,.2f})")
    else:
        print("  (no billable activity in this log)")
    print()
    print("NOTE: this covers ONLY this bucket's access log. If your bill differs")
    print("      materially, look at: other buckets, CloudFront, storage (GB-month),")
    print("      lifecycle/Glacier transitions, or request charges from other sources.")
    return 0


if __name__ == "__main__":
    sys.exit(main())
