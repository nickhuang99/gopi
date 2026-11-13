#!/usr/bin/env python3
"""M1 验收 harness：12 项抽测 before/after 命中率对照（roadmap M1 ④）。

抽测集 = hermes 蒸馏抽测同款：中英混合 2 + 同义变体 2 + 5W1H 维度 8
（what/why/when/where/who 各 2 项，从 after 库真实 kw_* 标签选取——查询措辞
来自 5W1H 词汇面，这正是 M1 加列要解决的检索场景）。

所有产物（before/after 二进制、DB 副本、结果 JSON）默认落在共享
scratch queue（~/workspace/gopi-test-scratch/queue）——绝不落 /tmp
（147G `/` 分区 87% 满，2026-09-11 耗尽事故的现场）。
见 scripts/lib/tmp_ban.py。

用法: python3 scripts/m1_acceptance.py --before-bin ~/workspace/m1/gopi-before \
        --after-bin ~/workspace/m1/gopi-after --before-db ~/workspace/m1/m1-before.db \
        --after-db ~/workspace/m1/m1-after.db
"""
import argparse
import json
import os
import re
import sqlite3
import subprocess
import sys

sys.path.insert(0, os.path.join(os.path.dirname(os.path.abspath(__file__)), "lib"))
from tmp_ban import (  # noqa: E402  (path set above)
    assert_off_root_fs,
    bounded_loop,
    queue_path,
    scratch_queue,
)

# Scratch queue location (off `/` — asserted by tmp_ban on first use):
# the acceptance harness's own artifacts live here, never in /tmp.
M1_DIR = queue_path("m1-acceptance")
BEFORE_DB = os.path.join(M1_DIR, "m1-before.db")
AFTER_DB = os.path.join(M1_DIR, "m1-after.db")
DETAIL_JSON = os.path.join(M1_DIR, "m1-acceptance.json")
BEFORE_BIN = os.path.join(M1_DIR, "gopi-before")
AFTER_BIN = os.path.join(M1_DIR, "gopi-after")
# MAX_QUERY_ATTEMPTS / SEARCH_ATTEMPTS_SECONDS bound the search retry loop:
# an iteration cap stops a fast spin, the wall-clock budget stops a slow stall.
MAX_QUERY_ATTEMPTS = 3
SEARCH_ATTEMPTS_SECONDS = 120


def pick_5w1h(after_db: str) -> list:
    """从 after 库按 5W1H 维度各选 2 条。查询标签 = 该行标签里区分度最高
    （文档频率最低）的一个——泛化词（验证/2026/VPN）谁都有，不是有效检索面；
    DF 最低的标签才是这条记忆的专属钩子。"""
    conn = sqlite3.connect(after_db)
    conn.row_factory = sqlite3.Row
    # 全库标签 DF 表（低频=高区分度）
    df = {}
    for (dim,) in [("kw_what",), ("kw_why",), ("kw_when",), ("kw_where",), ("kw_who",)]:
        for (kw,) in conn.execute(f"SELECT {dim} FROM distilled_qa WHERE {dim} != ''"):
            for lab in kw.split(","):
                lab = lab.strip().lower()
                if lab:
                    df[lab] = df.get(lab, 0) + 1
    out = []
    for dim in ("kw_what", "kw_why", "kw_when", "kw_where", "kw_who"):
        rows = conn.execute(
            f"SELECT id, {dim} AS kw FROM distilled_qa "
            f"WHERE {dim} != '' AND exam_questions != '' "
            "ORDER BY retrieval_count DESC, id LIMIT 8").fetchall()
        picked = []
        seen_topics = set()
        for r in rows:
            if r["id"] in seen_topics:
                continue
            labels = [l.strip() for l in r["kw"].split(",") if l.strip()]
            if not labels:
                continue
            best = min(labels, key=lambda l: df.get(l.lower(), 1))
            if df.get(best.lower(), 1) > 3:  # 泛化词（>3 行共有）不是钩子
                continue
            picked.append({"q": best, "expect": r["id"], "dim": dim,
                           "df": df.get(best.lower(), 1)})
            seen_topics.add(r["id"])
            if len(picked) == 2:
                break
        out.extend(picked)
    conn.close()
    return out


FIXED = [
    # 中英混合 2（查询混排英文术语+中文，与正文措辞错开）
    {"q": "VPN 掉线排查", "expect": 1, "dim": "mixed"},
    {"q": "memory 检索可靠性测试", "expect": 2, "dim": "mixed"},
    # 同义变体 2（换一种问法）
    {"q": "VCMI 新增 AI 玩家的接入点", "expect": 200, "dim": "variant"},
    {"q": "hermes 命令行模块有哪些", "expect": 50, "dim": "variant"},
]


def build_cases(after_db: str) -> list:
    conn = sqlite3.connect(after_db)
    live = {r[0] for r in conn.execute("SELECT id FROM distilled_qa")}
    conn.close()
    cases = [c for c in FIXED if c["expect"] in live]
    cases += pick_5w1h(after_db)
    return cases


def run_search(binary: str, db: str, query: str) -> list:
    """跑 gopi memory search，返回按序命中的 id 列表。

    重试由 bounded_loop 双重限界（迭代上限 + 墙钟预算）：外部进程卡住时
    不会变成无界等待（2026-09-11 耗尽事故的直接教训）。
    """
    env = dict(os.environ)

    def once() -> list:
        r = subprocess.run([binary, "memory", "search", query, "--db", db],
                           capture_output=True, text=True, timeout=60, env=env)
        out = r.stdout + r.stderr
        return [int(m) for m in re.findall(r"^#(\d+) ", out, re.M)]

    attempts = bounded_loop(SEARCH_ATTEMPTS_SECONDS, MAX_QUERY_ATTEMPTS, once)
    for ids in attempts:
        if ids:
            return ids
    return []


def main(argv=None) -> None:
    ap = argparse.ArgumentParser()
    ap.add_argument("--before-bin", default=BEFORE_BIN)
    ap.add_argument("--after-bin", default=AFTER_BIN)
    ap.add_argument("--before-db", default=BEFORE_DB)
    ap.add_argument("--after-db", default=AFTER_DB)
    ap.add_argument("--dry-run", action="store_true",
                    help="print the plan (paths, cases, scratch root) and exit")
    args = ap.parse_args(argv)

    if args.dry_run:
        # Plan only — nothing is opened, queried or written. The scratch queue
        # is asserted off `/` right here, so the report proves the paths this
        # harness would use are usable (and a bad TMPDIR/GOPI_TEST_HOME is a
        # hard failure with the fix, not a warning).
        assert_off_root_fs(scratch_queue())
        print("[dry-run] m1-acceptance")
        print(f"  scratch queue: {scratch_queue()} (off the / filesystem: verified)")
        print(f"  after db     : {args.after_db} (exists: {os.path.exists(args.after_db)})")
        print(f"  before bin/db: {args.before_bin} / {args.before_db}")
        print(f"  after bin    : {args.after_bin}")
        print(f"  would do     : FTS sanity on the after DB, then the 12-case "
              f"top1/top3 before-vs-after comparison")
        print("[dry-run] nothing was executed")
        return

    # FTS 同步 sanity：after 库 FTS 行数 == base 行数
    conn = sqlite3.connect(args.after_db)
    base_n = conn.execute("SELECT count(*) FROM distilled_qa").fetchone()[0]
    fts_n = conn.execute("SELECT count(*) FROM distilled_qa_fts").fetchone()[0]
    tri_n = conn.execute("SELECT count(*) FROM distilled_qa_tri").fetchone()[0]
    filled = conn.execute(
        "SELECT count(*) FROM distilled_qa WHERE exam_questions != ''").fetchone()[0]
    conn.close()
    print(f"after-db: base={base_n} fts={fts_n} tri={tri_n} "
          f"exam_filled={filled} ({100*filled/max(base_n,1):.1f}%)")
    if fts_n != base_n or tri_n != base_n:
        sys.exit("FTS/base row mismatch — index not in sync, aborting")

    cases = build_cases(args.after_db)
    print(f"cases: {len(cases)}")
    if len(cases) != 12:
        sys.stderr.write(f"warn: expected 12 cases, got {len(cases)}\n")

    hits = {"before": {"top1": 0, "top3": 0}, "after": {"top1": 0, "top3": 0}}
    rows = []
    for c in cases:
        row = {"q": c["q"], "expect": c["expect"], "dim": c["dim"]}
        for side, binary, db in (
                ("before", args.before_bin, args.before_db),
                ("after", args.after_bin, args.after_db)):
            try:
                ids = run_search(binary, db, c["q"])
            except Exception as e:  # noqa: BLE001
                ids = []
                row[side + "_err"] = str(e)[:60]
            row[side + "_ids"] = ids[:5]
            if ids and ids[0] == c["expect"]:
                hits[side]["top1"] += 1
            if c["expect"] in ids[:3]:
                hits[side]["top3"] += 1
        rows.append(row)
        print(f"[{c['dim']:7s}] {c['q']!r} expect=#{c['expect']} "
              f"before={row['before_ids'][:3]} after={row['after_ids'][:3]}")

    n = len(rows)
    print("\n===== 结果 =====")
    print(f"{'':10s}{'top1':>8s}{'top3':>8s}")
    for side in ("before", "after"):
        h = hits[side]
        print(f"{side:10s}{h['top1']:>5d}/{n} {h['top3']:>5d}/{n}")
    print(f"top1 提升: {hits['after']['top1'] - hits['before']['top1']:+d}  "
          f"top3 提升: {hits['after']['top3'] - hits['before']['top3']:+d}")
    with open(DETAIL_JSON, "w") as f:
        json.dump({"cases": rows, "hits": hits, "n": n}, f, ensure_ascii=False, indent=1)
    print(f"detail: {DETAIL_JSON}")


if __name__ == "__main__":
    main()
