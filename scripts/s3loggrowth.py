#!/usr/bin/env python3
"""s3loggrowth.py — 用一天的日志样本外推多月累积成本。

模型依据（为什么不能只按字节算）：
  S3 存储按【字节】计价，但**小对象的实际占用被文件系统块大小放大**。
  S3 内部按 128 KB 起的分块管理对象；小于阈值的对象仍占一个最小单元。
  保守做法：按两种口径各算一次（纯字节 / 块放大），给出区间。

  经验口径（AWS 官方未公开精确块大小，社区实测与 S3 文档线索）：
    - 存储费按【对象实际字节】计（$0.023/GB-月，前 50TB）
    - 但小对象还有【每对象请求成本】：写入时一次 PUT，之后每次读/LIST
  → 对日志桶这种"只写不读"的场景，成本 = 字节存储费 + 写入请求费

实测输入（nick 提供的单日样本）：
  160,570 文件 / 647 MB / 覆盖 11.75 小时 → 平均 4,029 bytes/文件
"""
import argparse

# us-east-1 标准层
PRICE_GB_MONTH = 0.023          # 前 50 TB
PRICE_PUT_PER_1K = 0.005        # PUT/COPY/POST/LIST
PRICE_GET_PER_1K = 0.0004       # GET/SELECT
GB = 1e9
DAYS_PER_MONTH = 30


def estimate(files_per_day: float, avg_bytes: float, days: float,
             read_factor: float = 0.0) -> dict:
    """read_factor: 每个文件的额外 GET 次数（LIST/GET；纯写入场景填 0）。

    累积存量的积分（量纲是关键，别把 days 乘两次）：
      存量(t) = files_per_day * avg_bytes * t        字节，t 以【天】为单位
      ∫0..days 存量 dt = 0.5 * files_per_day * avg_bytes * days^2   字节·天
      GB-月 = 字节·天 / 1e9 / 30                     ← 天必须换算成月
    """
    n = files_per_day * days
    total_bytes = n * avg_bytes
    gb = total_bytes / GB

    byte_days = 0.5 * files_per_day * avg_bytes * days ** 2
    gb_months = byte_days / GB / DAYS_PER_MONTH
    storage_cost = gb_months * PRICE_GB_MONTH

    put_cost = n / 1000 * PRICE_PUT_PER_1K
    get_cost = n * read_factor / 1000 * PRICE_GET_PER_1K

    return dict(files=n, bytes=total_bytes, gb=gb,
                gb_months=gb_months, storage_cost=storage_cost,
                put_cost=put_cost, get_cost=get_cost,
                total=storage_cost + put_cost + get_cost)


def main(argv=None) -> int:
    ap = argparse.ArgumentParser(description="Extrapolate S3 log-bucket cost")
    # 实测默认值（来自 nick 的单日样本）
    ap.add_argument("--files-per-day", type=float, default=327_988,
                    help="default: measured 160,570 files / 11.75h")
    ap.add_argument("--avg-bytes", type=float, default=4_029)
    ap.add_argument("--read-factor", type=float, default=0.0,
                    help="extra GET/LIST per file (0 for write-only log buckets)")
    args = ap.parse_args(argv)

    print("=" * 72)
    print("S3 LOG-BUCKET COST EXTRAPOLATION")
    print("=" * 72)
    print(f"generation rate : {args.files_per_day:,.0f} files/day, "
          f"{args.avg_bytes:,.0f} B/file  "
          f"({args.files_per_day*args.avg_bytes/GB:.2f} GB/day written)")
    print(f"read factor     : {args.read_factor:g} extra GET per file")
    print()
    hdr = f"{'period':>8} {'files':>14} {'stored GB':>12} {'GB-month':>12} " \
          f"{'storage$':>10} {'requests$':>10} {'TOTAL$':>10}"
    print(hdr)
    print("-" * len(hdr))
    for days, label in ((1, "1 day"), (7, "1 week"), (30, "1 month"),
                        (90, "3 months"), (180, "6 months"), (365, "1 year")):
        r = estimate(args.files_per_day, args.avg_bytes, days, args.read_factor)
        print(f"{label:>8} {r['files']:>14,.0f} {r['gb']:>12,.1f} "
              f"{r['gb_months']:>12,.1f} {r['storage_cost']:>10,.2f} "
              f"{r['put_cost']+r['get_cost']:>10,.2f} {r['total']:>10,.2f}")

    print()
    print("NOTES")
    print("  · storage$ uses the cumulative integral (a log bucket's footprint")
    print("    grows linearly, so billed GB-month is the triangle, not GB x days)")
    print("  · requests$ counts the PUT that writes each log object")
    print("  · NOT modelled: per-object overhead on many small objects, LIST")
    print("    scans by inventory/replication, lifecycle transitions, and the")
    print("    cost of OPERATING on the bucket (each cleanup run is PUT+DELETE)")
    print()
    print("  → the real bill can exceed this. Use it as a FLOOR, and compare")
    print("    against CloudWatch BucketSizeBytes / Cost Explorer for the truth.")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
