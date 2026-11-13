#!/usr/bin/env python3
"""Results merge — 0-token websearch plugin script (W7).

把多个搜索方法（bing/google/arxiv/deepseek）的输出 JSON 融合为一份：
跨方法去重（URL 相同 或 domain+title 相似）+ 按相关性排序 + 来源标注。
纯本地处理（无网络、无 API key），全部逻辑在脚本内完成，agent 只为
最终蒸馏结果付 token（设计文档 gopi-websearch-plugin-design.md W7）。

Usage:
  python3 merge_results.py bing.json google.json arxiv.json [--query "rag"]
  python3 merge_results.py - < deepseek.json                # stdin 单个方法
  python3 bing_search.py "q" > /tmp/b.json & python3 google_search.py "q" > /tmp/g.json & wait
  python3 merge_results.py /tmp/b.json /tmp/g.json --query "q"

输入：每个参数是一个搜索脚本的输出文件（统一 JSON 格式，单行对象）：
  {"status":"ok|empty|timeout|unavailable","method":"bing|google|arxiv|deepseek",
   "elapsed_ms":N,"results":[...],"count":N}
  results 形状按方法：bing/google [{"title","url","snippet"}],
  arxiv [{"title","url","abstract_snippet","published"}],
  deepseek [{"text":"..."}]。status 非 ok 或 results 空的输入被跳过。

输出（stdout 单行 JSON）：
  {"status":"ok|empty","method":"merged","query":"...","elapsed_ms":N,"count":N,
   "inputs":[{"method","status","count"},...],
   "results":[{"title","url","snippet","source","sources","score", ...方法透传字段}]}
  source = 首个命中该结果的来源；sources = 所有命中的来源列表；
  score = 排序分（见 score_result）。

去重规则（is_duplicate）：
  1) normalize_url 后相等（忽略 scheme 大小写、www.、尾部斜杠、fragment、
     默认端口、utm_*/fbclid/gclid/ref 追踪参数、arxiv pdf<->abs 互换）
  2) domain 相同 且 title 归一化后相等或 difflib 相似度 >= threshold（默认 0.85）

排序（score_result，保留各方法原始顺序权重）：
  score = method_weight*10 - rank*0.5 + min(matched_tokens,3)*2.0 + (n_sources-1)*1.5
  - method_weight：bing/google=1.0, arxiv=0.8, deepseek=0.6（--weights 可覆盖）
  - rank：该方法内原始位置（越靠前分越高）——保留方法内原始顺序
  - matched_tokens：query 分词中出现在 title（deepseek 无 title 用 text）里的个数
  - n_sources：跨方法命中次数（多方法一致 = 更可信，加分）
  并列时：method_weight 高者优先，再按方法内原始 rank。
"""
import argparse
import difflib
import json
import re
import signal
import sys
import time

# ---------------------------------------------------------------------------
# 常量 — 单一事实来源，无内联魔法数
# ---------------------------------------------------------------------------
DEFAULT_WEIGHTS = {"bing": 1.0, "google": 1.0, "arxiv": 0.8, "deepseek": 0.6}
DEFAULT_THRESHOLD = 0.85      # domain+title 相似判重阈值
DEFAULT_MAX_RESULTS = 20      # 输出条数上限（0-token：结果精简才进 context）
RANK_PENALTY = 0.5            # 方法内 rank 每往后一名扣分
KEYWORD_BONUS = 2.0           # 每个命中的 query 词加分（上限 3 词）
AGREEMENT_BONUS = 1.5         # 每个额外命中来源加分
METHOD_SCALE = 10.0           # method_weight 的放大系数（主导排序）
SNIPPET_CHARS = 200           # deepseek text 生成的 snippet 长度
MAX_QUERY_TOKENS = 3          # 关键词命中计分上限
_QUERY_SPLIT_RE = re.compile(r"[\s,;:。，；：]+")


def _strip_tracking(u):
    """从 URL（或 path 段）中删除追踪参数 utm_*/fbclid/gclid/ref（含分隔符）。"""
    if "?" not in u:
        return u
    base, _, query = u.partition("?")
    keep = [p for p in query.split("&")
            if p and not re.match(r"(?:utm_[a-z]+|fbclid|gclid|ref)=", p)]
    return base + ("?" + "&".join(keep) if keep else "")


# ---------------------------------------------------------------------------
# URL / title 归一化
# ---------------------------------------------------------------------------
def normalize_url(url):
    """URL 归一化：低格 scheme+host、去 www.、默认端口、fragment、尾部斜杠、
    追踪参数；arxiv pdf<->abs 互换。返回可比较的规范串。"""
    u = (url or "").strip()
    u = u.split("#", 1)[0]
    m = re.match(r"^([a-zA-Z][a-zA-Z0-9+.\-]*://)?([^/]+)(/.*)?$", u)
    if not m:
        return u.rstrip("/").lower()
    scheme, host, path = m.group(1), m.group(2).lower(), m.group(3) or "/"
    scheme = (scheme or "http://").lower()
    host = re.sub(r":(?:80|443)$", "", host)
    host = host[4:] if host.startswith("www.") else host
    path = _strip_tracking(path)
    path = re.sub(r"[?&]+$", "", path)
    path = re.sub(r"/pdf/(\d+\.\d+.*)$", r"/abs/\1", path)  # arxiv pdf -> abs
    path = path.rstrip("/") or "/"
    # 全小写（含 path）：大小写不敏感站点在两个引擎的同一页可能大小写不同
    return f"{scheme}{host}{path}".lower()


def domain_of(url):
    """提取归一化后的 host（作为 domain 判重基准）。"""
    n = normalize_url(url)
    m = re.match(r"^[a-zA-Z][a-zA-Z0-9+.\-]*://([^/]+)", n)
    return m.group(1) if m else n


def normalize_title(title):
    """title 归一化：小写 + 非字母数字变空格 + 折叠空白（判重/相似度基准）。"""
    if not title:
        return ""
    t = re.sub(r"[^a-z0-9\u4e00-\u9fff]+", " ", title.lower())
    return " ".join(t.split())


def title_similar(t1, t2, threshold):
    """title 相似：归一化相等 或 difflib ratio >= threshold。"""
    n1, n2 = normalize_title(t1), normalize_title(t2)
    if not n1 or not n2:
        return False
    if n1 == n2:
        return True
    return difflib.SequenceMatcher(None, n1, n2).ratio() >= threshold


def is_duplicate(a, b, threshold):
    """跨方法判重：URL 归一化相等，或 domain 相同且 title 相似。"""
    if not a.get("url") or not b.get("url"):
        return False
    if normalize_url(a["url"]) == normalize_url(b["url"]):
        return True
    return (domain_of(a["url"]) == domain_of(b["url"])
            and title_similar(a.get("title", ""), b.get("title", ""), threshold))


# ---------------------------------------------------------------------------
# query 关键词命中
# ---------------------------------------------------------------------------
def query_tokens(query):
    """query 分词：空白/标点切分；整串无分隔（如纯中文）时整体算一个词。"""
    return [t for t in _QUERY_SPLIT_RE.split((query or "").strip().lower()) if t]


def matched_token_count(tokens, haystack):
    """query 词中出现在 haystack（title，deepseek 用 text）里的个数。"""
    h = (haystack or "").lower()
    return sum(1 for t in tokens if t in h)


# ---------------------------------------------------------------------------
# 融合主逻辑
# ---------------------------------------------------------------------------
def score_result(method_weight, rank, matched, n_sources):
    """排序分：方法权重（原始顺序权重）主导 + rank 位置 + 关键词加分 + 多源加分。"""
    return (method_weight * METHOD_SCALE - rank * RANK_PENALTY
            + min(matched, MAX_QUERY_TOKENS) * KEYWORD_BONUS
            + (n_sources - 1) * AGREEMENT_BONUS)


def load_input(path):
    """读一个方法输出文件 -> payload dict；- 表示 stdin；解析失败返回 None。"""
    try:
        if path == "-":
            data = sys.stdin.read()
        else:
            with open(path, encoding="utf-8") as f:
                data = f.read()
        return json.loads(data)
    except (OSError, json.JSONDecodeError) as e:
        return {"status": "unavailable", "method": "?", "results": [],
                "message": f"cannot read {path}: {e!r}"}


def merge(payloads, query, weights, threshold, max_results):
    """融合 N 个方法 payload。返回输出 JSON（dict）。"""
    start = time.monotonic()
    tokens = query_tokens(query)
    survivors = []          # 已通过判重的结果（含内部排序元数据）
    inputs_info = []

    for payload in payloads:
        method = str(payload.get("method", "?"))
        results = payload.get("results") or []
        status = payload.get("status", "unavailable")
        inputs_info.append({"method": method, "status": status, "count": len(results)})
        if status != "ok" or not results:
            continue                        # 失败/空输入不参与融合
        w = weights.get(method, 0.5)
        for rank, item in enumerate(results):
            title = str(item.get("title") or "")
            url = str(item.get("url") or "")
            text = str(item.get("text") or "")
            haystack = title or text        # deepseek 无 title，用 text 匹配关键词
            dup = None
            for s in survivors:
                if is_duplicate(s["_item"], item, threshold):
                    dup = s
                    break
            if dup is not None:
                # 合并：记录多源，title/url 保留首个命中者
                if method not in dup["_sources"]:
                    dup["_sources"].append(method)
                # 富字段透传：arxiv 的 abstract_snippet/published 补进幸存者
                for k in ("abstract_snippet", "published"):
                    if k in item and k not in dup["_item"]:
                        dup["_item"][k] = item[k]
                continue
            survivors.append({
                "_item": dict(item),
                "_method": method,
                "_rank": rank,
                "_matched": matched_token_count(tokens, haystack),
                "_sources": [method],
            })

    results_out = []
    for s in survivors:
        n_sources = len(s["_sources"])
        score = score_result(weights.get(s["_method"], 0.5), s["_rank"],
                             s["_matched"], n_sources)
        out = dict(s["_item"])
        out.setdefault("title", "")      # 统一形状：每条都有 title/url/snippet
        out.setdefault("url", "")
        out.setdefault("snippet", out["text"][:SNIPPET_CHARS] if "text" in out else "")
        out["source"] = s["_sources"][0]
        out["sources"] = s["_sources"]
        out["score"] = round(score, 2)
        results_out.append((score, s["_method"], s["_rank"], out))

    # 排序：score 降序；并列 -> 方法权重降序 -> 方法内原始 rank 升序（保留原始顺序权重）
    results_out.sort(key=lambda t: (-t[0], -weights.get(t[1], 0.5), t[2]))
    final = [r[3] for r in results_out][:max_results]

    payload = {
        "status": "ok" if final else "empty",
        "method": "merged",
        "query": query or "",
        "elapsed_ms": int((time.monotonic() - start) * 1000),
        "count": len(final),
        "inputs": inputs_info,
        "results": final,
    }
    if not final:
        payload["message"] = "no usable results across inputs"
    return payload


def main():
    signal.signal(signal.SIGPIPE, signal.SIG_DFL)   # | head 时按默认退出
    ap = argparse.ArgumentParser(
        description="Merge multi-method search JSONs: dedup + relevance sort (W7).")
    ap.add_argument("files", nargs="+", metavar="FILE",
                    help="method output JSON(s); '-' = stdin")
    ap.add_argument("--query", default="",
                    help="original query for title keyword bonus")
    ap.add_argument("--weights", default="",
                    help="method weights, e.g. 'bing:1.0,google:1.0,arxiv:0.8,deepseek:0.6'")
    ap.add_argument("--threshold", type=float, default=DEFAULT_THRESHOLD,
                    help=f"domain+title similarity dedup threshold (default {DEFAULT_THRESHOLD})")
    ap.add_argument("--max-results", type=int, default=DEFAULT_MAX_RESULTS,
                    help=f"max output results (default {DEFAULT_MAX_RESULTS})")
    args = ap.parse_args()

    weights = dict(DEFAULT_WEIGHTS)
    if args.weights:
        for pair in args.weights.split(","):
            if ":" in pair:
                k, _, v = pair.partition(":")
                weights[k.strip()] = float(v)

    payloads = [load_input(f) for f in args.files]
    out = merge(payloads, args.query, weights,
                max(0.0, min(args.threshold, 1.0)),
                max(1, args.max_results))
    json.dump(out, sys.stdout, ensure_ascii=False)
    sys.stdout.write("\n")


if __name__ == "__main__":
    main()
