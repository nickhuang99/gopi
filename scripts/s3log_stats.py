#!/usr/bin/env python3
"""s3log_stats.py — 分析 AWS S3 server access log 目录，找出计费大头。

计费口径（S3 请求计费的关键）：
  1. **请求数**（每 1000 请求计费）——这是 $100/月账单最可能的来源
  2. **传输字节**（字段 12 = bytes sent）——出流量计费
  3. **403/4xx 也是计费请求**：S3 对任何到达它的请求都计费，包括被拒的
     （实测本数据里有大量 403 AccessDenied 却仍传了 15003 字节）

日志格式（S3 server access log，空格分隔，引号内含空格）：
  owner bucket [time] remoteip requester reqid operation key "reqline" status errcode
  sentbytes objsize totaltime turnaround "referer" "useragent" versionid ...
  字段编号（0-based）：0 owner, 1 bucket, 2+3 time, 4 ip, 6 operation, 7 key,
  8 reqline, 9 status, 10 errcode, 11 sentbytes, 12 objsize

设计要点：
  - 单遍扫描（160K 文件），用缓冲区聚合，避免为每行建对象
  - 输出多个维度 top-N：按 key 前缀 / 按完整 key / 按操作 / 按状态 / 按 UA(爬虫)
  - 支持 -j 并行（文件独立，天然可并行）

用法:
  python3 s3log_stats.py <logdir> [--top N] [--jobs N] [--json out.json]
"""
import argparse
import json
import os
import re
import sys
from collections import defaultdict


def _pair():
    """defaultdict factory — module-level so it survives pickling."""
    return [0, 0]

from concurrent.futures import ProcessPoolExecutor

# 引号感知的字段切分（key 里可能有转义引号；时间戳含空格故此规则只切一次）
FIELD_RE = re.compile(r'\[([^\]]+)\]')


def parse_line(line: str):
    """返回 (bucket, key, operation, status, errcode, sentbytes) 或 None。

    S3 access log 不是规整 CSV，三个坑（都有实测证据）：
      1. [时间戳] 内含空格 → 先替换成占位符再切分，否则后面全部错位
      2. status/errcode/sentbytes 在【请求行结束引号之后】，且前面有空格 →
         必须用 split() 而不是 split(" ")，否则得到前导空串（实测 status=''
         errcode='403' 的错位）
      3. key 含空格时字段 7 被切断，且**后续所有字段右移一位**（实测
         p[7]='some/dir/with' p[8]='space.pdf'）→ 不能靠固定下标

    因此：以【请求行】为锚（它是唯一带引号、结构稳定的部分），
    从里面同时取路径、状态、字节数。
    """
    m = FIELD_RE.search(line)
    if not m:
        return None
    flat = line[:m.start()] + "TS" + line[m.end():]

    # 请求行的开引号定位：' "METHOD /path HTTP/1.1" '
    qo = flat.find('"')
    if qo < 0:
        return None
    rest = flat[qo + 1:]                 # 'METHOD /path HTTP/1.1" STATUS ERR ...'
    qc = rest.find('"')                  # 请求行结束引号
    if qc < 0:
        return None
    reqline = rest[:qc].split(" ")       # ['METHOD', '/path', 'HTTP/1.1']
    if len(reqline) < 2:
        return None
    key = reqline[1].lstrip("/")
    after = rest[qc + 1:].split()        # [STATUS, ERRCODE, SENT, ...]
    if len(after) < 3:
        return None
    status, errcode = after[0], after[1]
    try:
        sent = int(after[2])
    except ValueError:
        sent = 0

    # bucket / operation 在请求行之前，位置稳定（前导字段里没有空格）
    head = flat[:qo].split(" ")
    if len(head) < 8:
        return None
    bucket, operation = head[1], head[6]
    return bucket, key, operation, status, errcode, sent


def analyze_file(path: str):
    """单文件聚合 —— 返回局部的计数器字典（可并行合并）。"""
    w_prefix = defaultdict(_pair)   # 前2段 → [count, bytes]
    w_key = defaultdict(_pair)      # 完整 key → [count, bytes]
    w_op = defaultdict(int)
    w_status = defaultdict(int)
    w_ip = defaultdict(int)
    w_ua = defaultdict(int)
    w_month = defaultdict(_pair)
    bucket_seen = set()
    lines = errors = 0
    try:
        with open(path, "rb") as f:
            for raw in f:
                lines += 1
                try:
                    line = raw.decode("utf-8", "replace")
                except Exception:
                    continue
                r = parse_line(line)
                if not r:
                    errors += 1
                    continue
                bucket, key, operation, status, errcode, sent = r
                bucket_seen.add(bucket)
                pre = "/".join(key.split("/")[:2])
                w_prefix[pre][0] += 1
                w_prefix[pre][1] += sent
                w_key[key][0] += 1
                w_key[key][1] += sent
                w_op[operation] += 1
                w_status[f"{status} {errcode}".strip()] += 1
                # UA 粗略归类（爬虫识别）
                low = line.lower()
                for bot in ("googlebot", "bingbot", "tiktokspider", "ahrefs",
                            "semrush", "yandex", "petalbot", "bytespider",
                            "gptbot", "ccbot", "claudebot", "applebot"):
                    if bot in low:
                        w_ua[bot] += 1
                        break
                else:
                    w_ua["(human/other)"] += 1
                # 时间戳 → 月份/日期
                m = re.search(r"\[(\d{2}/\w{3}/\d{4}):", line)
                if m:
                    w_month[m.group(1)][0] += 1
                    w_month[m.group(1)][1] += sent
    except Exception as e:                      # noqa: BLE001 — 报告而非吞掉
        print(f"WARN {path}: {e}", file=sys.stderr)
    return dict(prefix=w_prefix, key=w_key, op=w_op, status=w_status,
                ua=w_ua, month=w_month, buckets=bucket_seen,
                lines=lines, errors=errors)


def merge(acc: dict, part: dict):
    for dim in ("prefix", "key", "month"):
        for k, (c, b) in part[dim].items():
            t = acc[dim].setdefault(k, [0, 0])
            t[0] += c
            t[1] += b
    for dim in ("op", "status", "ua"):
        for k, v in part[dim].items():
            acc[dim][k] = acc[dim].get(k, 0) + v
    acc["buckets"] |= part["buckets"]
    acc["lines"] += part["lines"]
    acc["errors"] += part["errors"]


def new_acc():
    return dict(prefix={}, key={}, op={}, status={}, ua={}, month={},
                buckets=set(), lines=0, errors=0)


def main(argv=None) -> int:
    ap = argparse.ArgumentParser(description="S3 access log cost analysis")
    ap.add_argument("logdir")
    ap.add_argument("--top", type=int, default=25)
    ap.add_argument("--jobs", type=int, default=max(1, (os.cpu_count() or 2) - 1))
    ap.add_argument("--json", default="", help="write full aggregates to this path")
    args = ap.parse_args(argv)

    files = []
    for root, _, names in os.walk(args.logdir):
        for n in names:
            files.append(os.path.join(root, n))
    if not files:
        print(f"no files under {args.logdir}", file=sys.stderr)
        return 1
    print(f"scanning {len(files)} files with {args.jobs} jobs ...", file=sys.stderr)

    acc = new_acc()
    with ProcessPoolExecutor(max_workers=args.jobs) as ex:
        for i, part in enumerate(ex.map(analyze_file, files, chunksize=64), 1):
            merge(acc, part)
            if i % 20000 == 0:
                print(f"  {i}/{len(files)}", file=sys.stderr)

    total_req = sum(c for c, _ in acc["month"].values())
    total_bytes = sum(b for _, b in acc["month"].values())

    def show(title, dim, n=None):
        n = n or args.top
        top = sorted(dim.items(), key=lambda kv: -kv[1][0])[:n]
        print(f"\n=== {title} (top {len(top)}) ===")
        print(f"{'count':>12}  {'bytes':>14}  {'MB':>9}  name")
        for k, (c, b) in top:
            print(f"{c:>12,}  {b:>14,}  {b/1e6:>9.1f}  {k}")
        return top

    print("=" * 70)
    print(f"S3 ACCESS LOG ANALYSIS — {args.logdir}")
    print("=" * 70)
    print(f"files scanned : {len(files):,}")
    print(f"log lines     : {acc['lines']:,}   (unparsed: {acc['errors']:,})")
    print(f"requests      : {total_req:,}")
    print(f"bytes sent    : {total_bytes:,}  ({total_bytes/1e6:.1f} MB)")
    print(f"buckets       : {', '.join(sorted(acc['buckets']))}")

    show("TOP KEY PREFIXES (2 levels) — where the requests go", acc["prefix"])
    show("TOP FULL KEYS", acc["key"], n=min(15, args.top))

    print(f"\n=== OPERATIONS (top {args.top}) ===")
    for k, v in sorted(acc["op"].items(), key=lambda kv: -kv[1])[:args.top]:
        print(f"{v:>12,}  {k}")
    print(f"\n=== STATUS / ERRORS (top {args.top}) ===")
    for k, v in sorted(acc["status"].items(), key=lambda kv: -kv[1])[:args.top]:
        print(f"{v:>12,}  {k}")
    print(f"\n=== USER AGENT CLASS (top {args.top}) — scrapers drive request counts ===")
    for k, v in sorted(acc["ua"].items(), key=lambda kv: -kv[1])[:args.top]:
        print(f"{v:>12,}  {k}")
    print("\n=== BY MONTH ===")
    for k in sorted(acc["month"]):
        c, b = acc["month"][k]
        print(f"{c:>12,}  {b:>14,}  {b/1e6:>9.1f} MB  {k}")

    print("\n" + "=" * 70)
    print("BILLING HINT (S3 standard, us-east-1 list prices):")
    print(f"  PUT/COPY/POST/LIST : ${total_req/1000*0.005:.2f}   (all requests)")
    print(f"  GET & other        : ${total_req/1000*0.0004:.2f}   (if all GET)")
    print(f"  data transfer out  : ${total_bytes/1e9*0.09:.2f}   (internet, tier1)")
    print("  NOTE: a high request COUNT with small bodies = request-charge driven;")
    print("        large bytes with few requests = transfer driven.")

    if args.json:
        ser = {d: {k: list(v) if isinstance(v, list) else v
                   for k, v in acc[d].items()}
               for d in ("prefix", "key", "op", "status", "ua", "month")}
        ser["buckets"] = sorted(acc["buckets"])
        ser.update(lines=acc["lines"], errors=acc["errors"],
                   files=len(files), total_req=total_req, total_bytes=total_bytes)
        with open(args.json, "w") as f:
            json.dump(ser, f, indent=1)
        print(f"\nfull aggregates -> {args.json}")
    return 0


if __name__ == "__main__":
    sys.exit(main())
