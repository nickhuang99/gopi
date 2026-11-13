#!/usr/bin/env python3
"""s3costusage.py — 解析 AWS Cost Explorer 的「按用量类型」导出 CSV。

与 s3cost.py 的区别（两者互补，不是替代）：
  s3cost.py      从 S3 ACCESS LOG 推"如果按 list price 计费应该是多少"
  s3costusage.py 读 AWS 的【真实账单】按用量类型拆分——这是权威数

为什么需要它：access log 只能看见"访问"，看不见"存储"和"跨区传输"。
账单里 DataTransfer-Out-Bytes 是最大项，而它来自 **S3 公网出站**，
不是我从日志里算的内容下载 —— 两者必须对上账才知道钱去哪了。

CSV 形态（Cost Explorer 导出）：第一列 Usage type，
其余每列是一个用量类型 × 一个月（金额 $）。表头两行 + 每月一行。
"""
import argparse
import csv
import sys


def load(path: str):
    """→ (months, rows)  months=['2026-03-01',...], rows={usage_type: {month: float}}"""
    with open(path, encoding="utf-8-sig", newline="") as f:
        rows = list(csv.reader(f))
    if len(rows) < 2:
        raise SystemExit("csv too short")

    # 表头行里，第 0 列是 "Usage type"，第 1 列起是 "XXXX($)"
    # CSV 里列名可能被截断（Cost Explorer 导出会有长名），所以月名必须
    # 从【数据行】取——第一列是月份（2026-03-01 形式），这才是权威的。
    # 结构：rows[0]=表头（列 = usage type），rows[1]="Usage type total"，
    # rows[2:] = 每月一行。所以 usage type 是【列】，月份是【行】。
    header = rows[0]
    labels = [h[:-3] if h.endswith("($)") else h for h in header[1:]]
    months = [r[0] for r in rows[2:] if r and r[0]]

    data = {}
    for ci, label in enumerate(labels, start=1):
        vals = {}
        for r in rows[2:]:
            if len(r) <= ci:
                continue
            raw = (r[ci] or "").strip()
            if not raw:
                continue
            try:
                vals[r[0]] = float(raw)
            except ValueError:
                pass          # Cost Explorer 用空串表示"无用量"
        data[label] = vals
    return months, data


def main(argv=None) -> int:
    ap = argparse.ArgumentParser(description="Break down an AWS cost CSV by usage type")
    ap.add_argument("csv")
    ap.add_argument("--top", type=int, default=12)
    ap.add_argument("--month", help="focus one month (default: every month)")
    ap.add_argument("--exclude-zero", action="store_true", default=True)
    args = ap.parse_args(argv)

    months, data = load(args.csv)
    if not months:
        raise SystemExit("no month columns found")

    # 每月总计（用 AWS 自己给的 Total costs 行，缺失则自己求和）
    print("=" * 78)
    print("AWS COST BY USAGE TYPE".center(78))
    print("=" * 78)
    print(f"months: {', '.join(m[:7] for m in months)}")
    print()

    # ---- 逐月：最大项及其占比 -------------------------------------------
    for m in ([args.month] if args.month else months):
        total = sum(v.get(m, 0.0) for k, v in data.items() if k != "Total costs")
        print(f"--- {m[:7]}   total ${total:,.2f} ---")
        items = sorted(((k, v.get(m, 0.0)) for k, v in data.items()
                        if k != "Total costs"), key=lambda kv: -kv[1])
        for k, v in items:
            if v == 0 and args.exclude_zero:
                continue
            if v < 0.005:
                continue
            print(f"  {v:>10,.2f}  {v/total*100:>5.1f}%  {k}")
        print()

    # ---- 只统计 S3 相关的项 ---------------------------------------------
    print("=" * 78)
    print("S3-RELEVANT LINES ONLY".center(78))
    print("=" * 78)
    s3_keys = [k for k in data if any(t in k for t in (
        "DataTransfer-Out-Bytes", "Requests-Tier", "TimedStorage",
        "AWS-Out-Bytes", "DataTransferOut", "Storage-"))]
    hdr = f"{'usage type':<34}" + "".join(f"{m[5:7]:>9}" for m in months)
    print(hdr)
    print("-" * len(hdr))
    for k in sorted(s3_keys):
        vals = data[k]
        line = f"{k[:33]:<34}" + "".join(
            f"{vals.get(m, 0):>9.2f}" if vals.get(m, 0) else f"{'.':>9}"
            for m in months)
        print(line)

    # ---- 关键问题的答案 ---------------------------------------------------
    print()
    print("=" * 78)
    print("THE QUESTION: where does the S3 money actually go?")
    print("=" * 78)
    last = months[-1]
    buckets = {
        "S3 internet egress (DataTransfer-Out-Bytes)":
            data.get("DataTransfer-Out-Bytes", {}).get(last, 0.0),
        "S3 GET requests (Requests-Tier2)":
            data.get("Requests-Tier2", {}).get(last, 0.0),
        "S3 PUT/LIST requests (Requests-Tier1)":
            data.get("Requests-Tier1", {}).get(last, 0.0),
        "S3 storage, Standard (TimedStorage-ByteHrs)":
            data.get("TimedStorage-ByteHrs", {}).get(last, 0.0),
        "S3 storage, Reduced Redundancy (RRS)":
            data.get("TimedStorage-RRS-ByteHrs", {}).get(last, 0.0),
    }
    grand = sum(buckets.values())
    for k, v in sorted(buckets.items(), key=lambda kv: -kv[1]):
        pct = f"{v/grand*100:>5.1f}%" if grand else "  n/a"
        print(f"  {v:>10,.2f}  {pct}  {k}")
    print(f"  {'-'*10}")
    print(f"  {grand:>10,.2f}         S3 subtotal ({last[:7]})")
    print()
    print("Reading this: if DataTransfer-Out-Bytes dominates, the bill is")
    print("EGRESS - bytes leaving S3 over the internet - not request counts")
    print("and not storage. Deleting log files helps storage, but egress is")
    print("driven by whatever is being DOWNLOADED.")
    return 0


if __name__ == "__main__":
    sys.exit(main())
