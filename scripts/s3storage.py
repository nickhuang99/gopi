#!/usr/bin/env python3
"""s3storage.py — 从 S3 access log 估算【存储费】（日志里唯一能推出账单大头的字段）。

为什么需要它：
  我们的日志分析证明「请求 + 传输」一天才 $0.085，而实际账单 S3 = $82/月。
  剩下唯一能从日志推的就是 **对象大小**（字段 16 = objsize）——
  它出现在每条 GET 记录里，累加去重后 ≈ bucket 的存量大小。

口径（us-east-1 Standard，2026 官方价）：
  前 50 TB      $0.023 / GB-月
  50-450 TB     $0.022 / GB-月
  > 500 TB      $0.021 / GB-月

重要限制（必须说明，别当精确值）：
  - 只覆盖【被访问过的】对象 → 是存量【下界】
  - 同一 key 在不同时间大小可能变（只取最后见到的）
  - 403/404 的 sentbytes 是错误响应体，不是对象大小 → 必须用 objsize 字段
  - 不包含版本化对象的旧版本、未访问对象、multipart 上传中的分片
"""
import argparse
import os
import re
import sys
from collections import defaultdict
from concurrent.futures import ProcessPoolExecutor

FIELD_RE = re.compile(r"\[([^\]]+)\]")


def parse_sizes(line: str):
    """返回 (key, objsize) 或 None。

    字段布局（实测）：... [6]op [7]key [8]'"METHOD' [9]'/path HTTP/1.1" STATUS ERRCODE SENT OBJSIZE ...'
    即：请求行结束引号之后 = STATUS, ERRCODE, SENTBYTES, OBJSIZE
    """
    m = FIELD_RE.search(line)
    if not m:
        return None
    flat = line[:m.start()] + "TS" + line[m.end():]
    qo = flat.find('"')
    if qo < 0:
        return None
    rest = flat[qo + 1:]
    qc = rest.find('"')
    if qc < 0:
        return None
    reqline = rest[:qc].split(" ")
    if len(reqline) < 2:
        return None
    key = reqline[1].lstrip("/")
    after = rest[qc + 1:].split()
    if len(after) < 4:                  # 需要 STATUS ERRCODE SENT OBJSIZE
        return None
    # SENT/OBJSIZE are "-" on some operations (observed on HEAD, where no body
    # is transferred and no object size is reported). A missing number is NOT
    # a zero size: for storage accounting it must be skipped, not counted as 0
    # (a 0 would inflate the "distinct keys" figure without adding bytes).
    try:
        objsize = int(after[3])
    except ValueError:
        return key, None
    return key, objsize


def scan(path: str):
    """单文件扫描 → (sizes, lines, unparsed)。

    sizes 只收【正数】的对象大小：0 和 None（"-"）都不进，
    否则 distinct-keys 会被虚高而字节数不变，报告就自相矛盾。
    """
    sizes = {}
    lines = unparsed = 0
    try:
        with open(path, "rb") as f:
            for raw in f:
                lines += 1
                r = parse_sizes(raw.decode("utf-8", "replace"))
                if not r:
                    unparsed += 1
                    continue
                k, s = r
                if s:                      # None ("-") 与 0 都跳过
                    sizes[k] = s           # last-seen wins
    except Exception as e:                 # noqa: BLE001
        print(f"WARN {path}: {e}", file=sys.stderr)
    return sizes, lines, unparsed


def storage_cost(gb: float) -> float:
    """us-east-1 Standard 阶梯价。"""
    if gb <= 50 * 1024:
        return gb * 0.023
    if gb <= 450 * 1024:
        return 50 * 1024 * 0.023 + (gb - 50 * 1024) * 0.022
    return (50 * 1024 * 0.023 + 400 * 1024 * 0.022
            + (gb - 450 * 1024) * 0.021)


def main(argv=None) -> int:
    ap = argparse.ArgumentParser(description="Estimate S3 storage from access logs")
    ap.add_argument("logdir")
    ap.add_argument("--jobs", type=int, default=max(1, (os.cpu_count() or 2) - 1))
    ap.add_argument("--top", type=int, default=20)
    args = ap.parse_args(argv)

    files = [os.path.join(r, n) for r, _, ns in os.walk(args.logdir) for n in ns]
    if not files:
        print("no files", file=sys.stderr)
        return 1
    print(f"scanning {len(files)} files ...", file=sys.stderr)

    all_sizes = {}
    tot_lines = tot_errs = 0
    with ProcessPoolExecutor(max_workers=args.jobs) as ex:
        for i, (sizes, lines, errs) in enumerate(
                ex.map(scan, files, chunksize=64), 1):
            all_sizes.update(sizes)
            tot_lines += lines
            tot_errs += errs
            if i % 40000 == 0:
                print(f"  {i}/{len(files)}", file=sys.stderr)

    total = sum(all_sizes.values())
    gb = total / 1e9
    print("=" * 66)
    print("S3 STORAGE ESTIMATE (lower bound, from access-log objsize)")
    print("=" * 66)
    print(f"log lines      : {tot_lines:,}  (unparsed {tot_errs:,})")
    print(f"distinct keys  : {len(all_sizes):,}")
    print(f"total size     : {total:,} bytes = {gb:,.2f} GB = {gb/1024:,.3f} TB")
    print()
    print(f"storage cost   : ${storage_cost(gb):,.2f} / month  (us-east-1 Standard)")
    print()
    print("LOWER BOUND — this counts ONLY objects that were requested (and had")
    print("a positive size) in this log window. The real bucket can be larger:")
    print("unrequested objects, old versions, and multipart parts are invisible.")
    print()
    print(f"=== TOP {args.top} LARGEST KEYS SEEN ===")
    for k, s in sorted(all_sizes.items(), key=lambda kv: -kv[1])[:args.top]:
        print(f"{s:>14,}  {s/1e6:>9.1f} MB  {k[:90]}")

    print("\n=== SIZE DISTRIBUTION ===")
    # (lower bound, label) descending; each band counts [lo, next_lo)
    bands = [(1 << 30, ">= 1 GB"), (100 << 20, "100 MB - 1 GB"),
             (10 << 20, "10-100 MB"), (1 << 20, "1-10 MB"),
             (100 << 10, "100 KB - 1 MB"), (0, "< 100 KB")]
    bounds = [b[0] for b in bands]
    counts = {label: 0 for _, label in bands}
    for s in all_sizes.values():
        for i, (lo, label) in enumerate(bands):
            hi = bounds[i - 1] if i else float("inf")
            if lo <= s < hi:
                counts[label] += 1
                break
    for _, label in bands:
        print(f"{counts[label]:>10,}  {label}")
    return 0


if __name__ == "__main__":
    sys.exit(main())
