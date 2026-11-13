#!/usr/bin/env python3
"""M2 acceptance: feedback injection before/after top-hit comparison +
tier/confidence distribution sanity — data-driven (reads the real prod DB
copy, no magic thresholds: every gate is a named constant or measured).

The DB copy lives in the shared scratch queue
(~/workspace/gopi-test-scratch/queue) — never /tmp (the / partition is the
subject of the 2026-09-11 exhaustion; see scripts/lib/tmp_ban.py).

Usage: python3 scripts/m2_acceptance.py [--db /path/to/memory.db] [--dry-run]
"""
import argparse
import json
import math
import os
import shutil
import sqlite3
import subprocess
import sys
import time

sys.path.insert(0, os.path.join(os.path.dirname(os.path.abspath(__file__)), "lib"))
from tmp_ban import assert_off_root_fs, make_scratch_dir  # noqa: E402

# ---- named gates (Rule 1: no magic numbers in logic) ----
DEFAULT_REPO = os.path.expanduser("~/workspace/gopi")
REPO = os.environ.get("GOPI_REPO_ROOT", DEFAULT_REPO)
DEFAULT_DB = os.path.expanduser("~/.gopi/memory.db")
BIN = os.path.join(REPO, "bin/gopi")
# SUBPROCESS_TIMEOUT_SEC bounds every gopi invocation this harness makes: no
# child may outlive the harness (an unbounded child is how a harness leaks).
SUBPROCESS_TIMEOUT_SEC = 30
QUERY_SET = [  # 12-item probe set: mixed CN/EN, same as M1's acceptance style
    ("network test env", "文本检索网络测试环境什么工具"),
    ("batch keyword fix", "怎么批量修改 keywords 关键词"),
    ("text2img render", "文本渲染成图片 PNG 怎么验证"),
    ("worktree standard", "为什么必须用 git worktree 而不是 clone"),
    ("fts sync trigger", "FTS 索引和表数据怎么保持同步"),
    ("feedback closure", "memory_feedback 投票后置信度会变吗"),
    ("conf decay tau", "confidence 衰减时间常数是多少天"),
    ("hot tier semantics", "hot tier 常驻注入是什么意思"),
    ("cold demotion", "cold 冷却建议什么时候触发"),
    ("garbage gate", "垃圾记忆入库前怎么被拦截"),
    ("value signal", "搜索排序 token coverage 权重"),
    ("reverify reward", "重新验证奖励上限是多少"),
]
TOP_K = 3           # top3 hit evaluation window
STALE_DAYS = 30     # possiblyStale window (matches staleAfterDays)
HOT_STALE_DAYS = 7  # matches hotStaleDays


def query_memory(db, query, limit=5):
    env = dict(os.environ, GOPI_MEMORY_DB=db)
    r = subprocess.run([BIN, "memory", "search", query, "--limit", str(limit)],
                       capture_output=True, text=True, env=env,
                       timeout=SUBPROCESS_TIMEOUT_SEC)
    out = (r.stdout or "").strip()
    hits = []
    for line in out.splitlines():  # per-line: answers may contain \n---\n
        if line.startswith("#"):
            try:
                hits.append(int(line[1:].split(" ", 1)[0]))
            except ValueError:
                pass
    return hits


def main(argv=None):
    ap = argparse.ArgumentParser()
    ap.add_argument("--db", default=DEFAULT_DB)
    ap.add_argument("--dry-run", action="store_true",
                    help="print the plan (scratch root + probes) and exit")
    args = ap.parse_args(argv)
    src = args.db
    tmp = make_scratch_dir("m2-acc")   # under the queue, off `/`, removed at exit
    assert_off_root_fs(tmp)
    db = os.path.join(tmp, "memory.db")
    if args.dry_run:
        print(f"[dry-run] m2-acceptance")
        print(f"  scratch root : {tmp} (off the / filesystem: verified)")
        print(f"  source db    : {src} (read-only copy, {len(QUERY_SET)} probes)")
        print(f"  binary       : {BIN} (exists: {os.path.exists(BIN)})")
        print("[dry-run] nothing was executed")
        return 0
    shutil.copy2(src, db)  # never touch prod
    print(f"db copy: {db} (src={src})")

    # ---- 1. feedback before/after: vote on the top hit of each query, re-run ----
    before = {q: query_memory(db, q) for q, _ in QUERY_SET}
    voted = set()
    conn = sqlite3.connect(db)
    for q, _ in QUERY_SET:
        for h in before[q][:1]:
            if h in voted:
                continue
            voted.add(h)
            env = dict(os.environ, GOPI_MEMORY_DB=db)
            r = subprocess.run([BIN, "memory", "feedback", str(h), "--useful",
                            "--reason", "m2-acceptance", "--task", f"m2-acc-{h}"],
                           capture_output=True, text=True, env=env,
                       timeout=SUBPROCESS_TIMEOUT_SEC)
            if r.returncode != 0:
                print(f"  VOTE FAILED id={h}: {r.stderr.strip()[:120]}")
    after = {q: query_memory(db, q) for q, _ in QUERY_SET}

    print("\n== feedback before/after (target = the voted top hit, window=top%d) ==" % TOP_K)
    top1_b = top1_a = top3_b = top3_a = 0
    for q, label in QUERY_SET:
        b, a = before[q], after[q]
        if not b:
            print(f"  SKIP {label[:24]:24} (no hits before)")
            continue
        target = b[0]
        top1_b += target in b[:1]
        top1_a += target in a[:1]
        top3_b += target in b[:TOP_K]
        top3_a += target in a[:TOP_K]
        arrow = "up" if (target in a[:TOP_K] and not target in b[:TOP_K]) else \
                "down" if (target in b[:TOP_K] and not target in a[:TOP_K]) else "same"
        print(f"  {label[:24]:24} before_top3={b[:TOP_K]} after_top3={a[:TOP_K]} {arrow}")
    n = sum(1 for q, _ in QUERY_SET if before[q])
    print(f"\ntop1: {top1_b}/{n} -> {top1_a}/{n}   top3: {top3_b}/{n} -> {top3_a}/{n}")
    # gate: useful votes must never LOSE top hits (closure monotonicity)
    regressions = sum(1 for q, _ in QUERY_SET if before[q]
                      and before[q][0] in before[q][:TOP_K] and before[q][0] not in after[q][:TOP_K])
    print(f"useful-vote top3 regressions: {regressions} (expect 0)")

    # ---- 2. tier/confidence distribution (read-only sanity) ----
    tiers = dict(conn.execute("SELECT tier, COUNT(*) FROM distilled_qa GROUP BY tier").fetchall())
    print(f"\ntier distribution: {tiers} (total {sum(tiers.values())})")
    confs = [r[0] for r in conn.execute(
        "SELECT confidence FROM distilled_qa WHERE confidence IS NOT NULL").fetchall()]
    if confs:
        mean = sum(confs) / len(confs)
        sd = math.sqrt(sum((c - mean) ** 2 for c in confs) / len(confs))
        oob = sum(1 for c in confs if not (0.0 <= c <= 1.0))
        print(f"confidence: mean={mean:.3f} sd={sd:.3f} min={min(confs):.2f} max={max(confs):.2f} out-of-[0,1]={oob}")
    # hot_stale share among hot rows (no permanent hot — is it observable?)
    hot = conn.execute("SELECT COUNT(*) FROM distilled_qa WHERE tier='hot'").fetchone()[0]
    if hot:
        hot_old = conn.execute(
            """SELECT COUNT(*) FROM distilled_qa WHERE tier='hot' AND
               (last_retrieved IS NULL OR last_retrieved=0
                 OR last_retrieved < ?)""",
            (time.time() - HOT_STALE_DAYS * 86400,)).fetchone()[0]
        print(f"hot rows: {hot}, hot_stale(>{HOT_STALE_DAYS}d unverified): {hot_old} ({100*hot_old/hot:.0f}%)")
    # fb columns sane?
    fb = conn.execute("SELECT COUNT(*) FROM distilled_qa WHERE useful_count+useless_count>0").fetchone()[0]
    print(f"rows with feedback: {fb}")
    conn.close()
    return 0 if regressions == 0 else 1


if __name__ == "__main__":
    sys.exit(main())
