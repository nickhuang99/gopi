#!/usr/bin/env python3
"""M1 backfill: 存量 memory 全量补 exam_questions + 5W1H（roadmap M1 ②）。

hermes batch_exam_gen.py 模式（~/work/glow-moon/bin/）+ GoPi 适配：
  - 批 5 条拼一个 prompt → 单次 DeepSeek 调用 → 严格 JSON 按 id 回填
  - thinking:disabled 防 reasoning 吃 max_tokens
  - NO_JSON / PARTIAL / STALL(10 批零进度熔断) / 429 退避 四类防御
  - WHERE exam_questions='' 断点续跑，天然幂等
  - 只 ALTER ADD COLUMN（只加不删），索引重建交给 gopi 二进制的 heal 路径

用法:
  python3 scripts/backfill_exam_5w1h.py [--db PATH] [--limit N] [--batch 5]
环境:
  DEEPSEEK_API_KEY 或 ~/.hermes/.env 的 DEEPSEEK_API_KEY（真实 DeepSeek key）
"""
import json
import os
import re
import sqlite3
import sys
import time
import urllib.request
import urllib.error
from concurrent.futures import ThreadPoolExecutor

# 并发批数：GLM flash 无官方 RPM 文档，8 并发实测稳定；429 由退避兜底。
PARALLEL_BATCHES = int(os.environ.get("GOPI_BACKFILL_PARALLEL", "2"))
SQLITE_CHECKPOINT = "PRAGMA wal_checkpoint(TRUNCATE)"

DB_DEFAULT = os.path.expanduser("~/.gopi/memory.db")
# 端点备忘（2026-08-30 实测）: hermes 的 deepseek key 已 401；GLM 代理
# (open.bigmodel.cn/api/paas/v4, OpenAI 兼容) 可用且支持 thinking:disabled
# —— anthropic 协议端点反而强制思考（1210 错误）。
API = os.environ.get("GOPI_BACKFILL_API",
                     "https://open.bigmodel.cn/api/paas/v4/chat/completions")
MODEL = os.environ.get("GOPI_BACKFILL_MODEL", "glm-4.5-flash")
MAX_TOKENS = 4096
TEMPERATURE = 0.3
STALL_ABORT = 10          # 连续零进度批数熔断（hermes 同款）
Q_PREVIEW, A_PREVIEW = 200, 300

HEAD = (
    "为下面每条记忆生成检索辅助标注。要求：\n"
    "1. eq: 2-3 个考试问法（用户可能问的角度，各≤30字，中英混合场景保留原术语，不得照抄原问题）\n"
    "2. 5W1H 标签，每维 0-2 个、每个 2-6 字、无该维度则空数组：\n"
    "   what=主题/事物, why=原因/目的, when=时间/时机, where=环境/位置, who=角色/工具\n"
    '只返回 JSON：{"0":{"eq":["问法1","问法2"],"what":["…"],"why":["…"],"when":[],"where":[],"who":[]},…}\n\n'
)


def api_key() -> str:
    k = os.environ.get("DEEPSEEK_API_KEY")
    if k:
        return k.strip()
    env = os.path.expanduser("~/.hermes/.env")
    if os.path.exists(env):
        with open(env) as f:
            for line in f:
                if line.startswith("DEEPSEEK_API_KEY"):
                    return line.split("=", 1)[1].strip().strip('"')
    sys.exit("no API key: set DEEPSEEK_API_KEY (GLM proxy key works)")


def ensure_cols(conn: sqlite3.Connection) -> None:
    """只加不删：缺的 M1 列 ALTER 进去（存量库预热，等价 gopi 二进制的 heal）。"""
    cols = {r[1] for r in conn.execute("PRAGMA table_info(distilled_qa)")}
    for c in ("exam_questions", "kw_what", "kw_why", "kw_when", "kw_where", "kw_who"):
        if c not in cols:
            conn.execute(f"ALTER TABLE distilled_qa ADD COLUMN {c} TEXT DEFAULT ''")
            print(f"  ALTER ADD {c}")
    conn.commit()


def call_llm(key: str, prompt: str) -> str:
    req = urllib.request.Request(
        API,
        data=json.dumps({
            "model": MODEL,
            "messages": [{"role": "user", "content": prompt}],
            "temperature": TEMPERATURE,
            "max_tokens": MAX_TOKENS,
            "thinking": {"type": "disabled"},  # 防 reasoning 吃 max_tokens
        }).encode(),
        headers={"Content-Type": "application/json", "Authorization": f"Bearer {key}"},
    )
    last = None
    for attempt in range(4):
        try:
            resp = urllib.request.urlopen(req, timeout=90)
            return json.loads(resp.read().decode())["choices"][0]["message"]["content"]
        except urllib.error.HTTPError as e:
            if e.code == 429:
                wait = 5 * (attempt + 1)
                sys.stderr.write(f"  429, backoff {wait}s\n")
                time.sleep(wait)
                last = e
                continue
            raise
    raise last


def clamp_labels(v, maxlen=2, charmax=12) -> list:
    """5W1H 标签防御：截数组、去空白。CJK 标签限 charmax 字（hermes 规范
    2-6 字，容忍到 12）；含 ASCII 词的标签限到 24 但不切断词——截断的
    ASCII 词是死面包屑（memory_search→memory_searc 永远检索不到）。"""
    if not isinstance(v, list):
        return []
    out = []
    for x in v[:maxlen]:
        if not (isinstance(x, str) and x.strip()):
            continue
        x = x.strip()
        if re.search(r"[A-Za-z]", x):
            if len(x) > 24:
                x = x[:24]
                # 词界回退：切在词中间时丢掉残词
                if not x.endswith((" ", "_", "-")) and " " in x:
                    x = x.rsplit(" ", 1)[0]
        else:
            x = x[:charmax]
        out.append(x)
    return out


def clamp_exam(v) -> list:
    """考试问法防御：2-3 条、每条 ≤30 字（问法是整句，不能按标签截）。"""
    return clamp_labels(v, maxlen=3, charmax=30)


FTS_COLS = ("topic, question, answer, sources, search_kw, jieba_token, "
            "exam_questions, kw_what, kw_why, kw_when, kw_where, kw_who")
TRI_COLS = "question, answer, topic, exam_questions"


def _trigger_sqls(tri: bool) -> list:
    """生成 keep-in-sync 触发器（与 gopi 二进制 m1FTSTriggers/m1TriTriggers 同构）。"""
    cols = TRI_COLS if tri else FTS_COLS
    allc = cols.split(", ")
    oldc = ", ".join("old." + c for c in allc)
    newc = ", ".join("new." + c for c in allc)
    tbl = "distilled_qa_tri" if tri else "distilled_qa_fts"
    pfx = "distilled_qa_tri" if tri else "distilled_qa"
    return [
        f"CREATE TRIGGER IF NOT EXISTS {pfx}_ai AFTER INSERT ON distilled_qa BEGIN\n"
        f"\t\tINSERT INTO {tbl}(rowid, {cols}) VALUES (new.id, {newc});\n\t\tEND",
        f"CREATE TRIGGER IF NOT EXISTS {pfx}_au AFTER UPDATE ON distilled_qa BEGIN\n"
        f"\t\tINSERT INTO {tbl}({tbl}, rowid, {cols}) VALUES ('delete', old.id, {oldc});\n"
        f"\t\tINSERT INTO {tbl}(rowid, {cols}) VALUES (new.id, {newc});\n\t\tEND",
        f"CREATE TRIGGER IF NOT EXISTS {pfx}_ad AFTER DELETE ON distilled_qa BEGIN\n"
        f"\t\tINSERT INTO {tbl}({tbl}, rowid, {cols}) VALUES ('delete', old.id, {oldc});\n\t\tEND",
    ]


def finalize_schema(conn: sqlite3.Connection) -> None:
    """收尾：FTS 两表重建到 M1 宽布局（12/4 列）+ 触发器重建 + 全量重灌。

    --db 副本不经过 gopi 二进制的默认路径 heal，布局必须在这里对齐；
    生产库则由二进制首搜时的 m1MigrateFTSCols 兜底（幂等）。
    """
    cols = {r[1] for r in conn.execute("PRAGMA table_info(distilled_qa)")}
    if "exam_questions" not in cols:
        return
    for t in ("distilled_qa_ai", "distilled_qa_au", "distilled_qa_ad",
              "distilled_qa_tri_ai", "distilled_qa_tri_au", "distilled_qa_tri_ad"):
        conn.execute(f"DROP TRIGGER IF EXISTS {t}")
    conn.execute("DROP TABLE IF EXISTS distilled_qa_fts")
    conn.execute("DROP TABLE IF EXISTS distilled_qa_tri")
    conn.execute(
        f"CREATE VIRTUAL TABLE distilled_qa_fts USING fts5({FTS_COLS}, "
        "content='distilled_qa', content_rowid='id')")
    conn.execute(
        f"CREATE VIRTUAL TABLE distilled_qa_tri USING fts5({TRI_COLS}, "
        "content='distilled_qa', content_rowid='id', tokenize='trigram')")
    for sql in _trigger_sqls(tri=False) + _trigger_sqls(tri=True):
        conn.execute(sql)
    for idx in ("distilled_qa_fts", "distilled_qa_tri"):
        conn.execute(f"INSERT INTO {idx}({idx}) VALUES('rebuild')")
        print(f"  rebuilt {idx} at M1 wide layout")


def main(argv=None) -> None:
    args = sys.argv[1:] if argv is None else list(argv)
    db_path = DB_DEFAULT
    batch_size, limit = 10, 0
    if "--db" in args:
        db_path = args[args.index("--db") + 1]
    if "--batch" in args:
        batch_size = int(args[args.index("--batch") + 1])
    if "--limit" in args:
        limit = int(args[args.index("--limit") + 1])
    key = api_key()

    conn = sqlite3.connect(db_path)
    conn.row_factory = sqlite3.Row
    ensure_cols(conn)
    where = "WHERE (exam_questions IS NULL OR exam_questions='') AND question != ''"
    total = conn.execute(f"SELECT COUNT(*) FROM distilled_qa {where}").fetchone()[0]
    if limit:
        total = min(total, limit)
    print(f"Backfill target: {total} rows in {db_path}")

    processed, t0 = 0, time.time()
    zero_rounds = 0

    def do_batch(batch):
        """单批 LLM 调用 + 解析。返回 [(rid, eq_json, labels...)]，失败返回 []。"""
        prompt = HEAD
        id_map = {}
        for i, row in enumerate(batch):
            prompt += f"[{i}] Q: {row['question'][:Q_PREVIEW]} A: {row['answer'][:A_PREVIEW]}\n\n"
            id_map[str(i)] = row["id"]
        text = call_llm(key, prompt)
        m = re.search(r"\{.*\}", text, re.DOTALL)
        if not m:
            sys.stderr.write(f"  batch@{batch[0]['id']} NO_JSON: {text[:120]!r}\n")
            return []
        result = json.loads(m.group(0))
        if len(result) < len(batch):
            sys.stderr.write(f"  batch@{batch[0]['id']} PARTIAL: {len(result)}/{len(batch)}\n")
        out = []
        for num, obj in result.items():
            rid = id_map.get(num)
            if not rid or not isinstance(obj, dict):
                continue
            eq = clamp_exam(obj.get("eq"))
            if not eq:
                continue
            labels = {k: clamp_labels(obj.get(k)) for k in ("what", "why", "when", "where", "who")}
            out.append((rid, json.dumps(eq, ensure_ascii=False), labels))
        return out

    # 一次性锁定剩余 id 列表：并发批按索引切片，避免 WHERE 头部重复取数
    # （UPDATE 落库前所有并发 SELECT 都会看到同一批未处理行）。
    ids = [r[0] for r in conn.execute(
        f"SELECT id FROM distilled_qa {where} ORDER BY id").fetchall()]
    if limit:
        ids = ids[:limit]
    byid = {}
    for i in range(0, len(ids), 500):
        chunk = ids[i:i + 500]
        ph = ",".join("?" * len(chunk))
        for r in conn.execute(
                f"SELECT id, question, answer FROM distilled_qa WHERE id IN ({ph})",
                chunk):
            byid[r[0]] = r

    with ThreadPoolExecutor(max_workers=PARALLEL_BATCHES) as pool:
        pos = 0
        while pos < len(ids):
            # 连续切片：每个并发槽取下一段 id，重叠=重复请求同批行。
            chunks = [ids[pos + i * batch_size:pos + (i + 1) * batch_size]
                      for i in range(PARALLEL_BATCHES)]
            batch_rows = [[byid[i] for i in c] for c in chunks if c]
            if not batch_rows:
                break
            pos += batch_size * PARALLEL_BATCHES
            before = processed
            # 先并发取数再排空游标：sqlite3 游标在 fetchall 后即可安全并发。
            for fut in [pool.submit(do_batch, b) for b in batch_rows]:
                try:
                    for rid, eqj, labels in fut.result():
                        conn.execute(
                            "UPDATE distilled_qa SET exam_questions=?, kw_what=?, kw_why=?, kw_when=?, kw_where=?, kw_who=? WHERE id=?",
                            (eqj,
                             ", ".join(labels["what"]), ", ".join(labels["why"]),
                             ", ".join(labels["when"]), ", ".join(labels["where"]),
                             ", ".join(labels["who"]), rid))
                        processed += 1
                except Exception as e:  # noqa: BLE001 — 单批失败不中断整体
                    sys.stderr.write(f"  batch failed: {type(e).__name__}: {str(e)[:120]}\n")
            conn.commit()
            if processed % 100 < batch_size * PARALLEL_BATCHES and processed:
                el = time.time() - t0
                sys.stdout.write(f"  {processed}/{total} {processed/max(el,0.01):.1f}/s\n")
                sys.stdout.flush()
            zero_rounds = zero_rounds + 1 if processed == before else 0
            if zero_rounds >= STALL_ABORT:
                sys.stderr.write("  STALL: 10 rounds with zero progress, aborting\n")
                break
            if limit and processed >= limit:
                break

    conn.commit()
    finalize_schema(conn)
    conn.commit()
    conn.close()
    print(f"Done: {processed} rows in {time.time()-t0:.0f}s")


if __name__ == "__main__":
    main()
