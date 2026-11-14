#!/usr/bin/env python3
"""Bing web search — 0-token websearch plugin script (W2, U1 python 迁移).

curl 抓取 + 内嵌解析（原 bash 版迁移为纯 python3，全插件统一 python3 调用）。
Design: gopi-websearch-plugin-design.md — 脚本层 0 token：
抓取 + 剥离 HTML 标签全部在脚本内完成，只把精简 JSON 输出进 agent context。

Usage:
  python3 bing_search.py "<query>" [--count N]

Output (stdout, 单行 JSON):
  {"status":"ok|empty|timeout|unavailable","method":"bing",
   "elapsed_ms":N,"results":[{"title","url","snippet"}],"count":N}

status semantics (design doc gopi-websearch-plugin-design.md):
  ok          — >=1 条结果
  empty       — 查询成功但无结果
  timeout     — bing 在 TIMEOUT_SEC 内未应答
  unavailable — HTTP 错误 / 网络失败 / 页面不可解析
"""
import argparse
import base64
import html as html_mod
import json
import os
import re
import signal
import socket
import subprocess
import sys
import time
import urllib.error
import urllib.parse
import urllib.request

# ---------------------------------------------------------------------------
# Constants — single source of truth, no magic numbers inline (Rule 1)
# ---------------------------------------------------------------------------
# 中国直连优先（cn.bing.com 直接 200）；www.bing.com 兜底（302 -> cn.bing.com）。
# U2：本地 cn.bing.com 对英文查询按中文语境解析（真实测试 0/5 全偏），
# 故纯英文查询改走 search-bot 境外跳板访问 www.bing.com（英文语义稳定）。
BING_HOSTS = ("cn.bing.com", "www.bing.com")
TIMEOUT_SEC = 8          # urlopen 超时；超过 -> status=timeout
DEFAULT_COUNT = 5        # 默认返回条数
MAX_COUNT = 20           # 硬上限，保证 JSON 精简（0-token）
SNIPPET_CHARS = 200      # snippet 截断长度（与 bash 版一致）
SSH_HOST = os.environ.get("SSH_HOST", "searchbox")  # 英文查询跳板（同 google_search.py）
SSH_OPTS = ["-o", "ConnectTimeout=8", "-o", "BatchMode=yes", "-T"]
SSH_TIMEOUT_SEC = 30     # 英文查询整次 ssh 往返总超时
UA = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/126.0.0.0 Safari/537.36"

# NL 脚手架词：相关性门不计入（见 query_terms）。保留核心内容词参与判 junk，
# 避免 "find all paper links by X" 里的 find/all 巧合命中诱饵标题。
QUERY_STOPWORDS = frozenset(
    "find all paper papers links link for download by authors author including "
    "name of from to the a an and with on list show me my their his her whose "
    "who are is what how do does did get about related page pages search site "
    "please need want would like some any each every more most other".split())

# 远端抓取脚本（英文查询用）：境外 IP + setlang=en&mkt=en-US 强制英文市场语义。
# 输出约定：成功 stdout 为原始 HTML；失败输出首行 @@SRC=ERR:<http_code>
REMOTE_SCRIPT = r"""set -u
UA='Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/126.0.0.0 Safari/537.36'
q=$(printf '%s' "$1" | base64 -d 2>/dev/null)          # $1 = 本地 base64 的 query
c=$(curl -sS -L --max-time 8 -A "$UA" -G 'https://www.bing.com/search' \
      --data-urlencode "q=$q" --data-urlencode 'setlang=en' \
      --data-urlencode 'mkt=en-US' --data-urlencode "count=$2")
if [ "$?" = 28 ]; then echo '@@SRC=ERR:000'; else printf '%s' "$c"; fi
"""


def strip_tags(s):
    """剥离 HTML 标签 + 反转义实体 + 折叠空白（snippet/title 蒸馏核心）。"""
    s = re.sub(r"<[^>]+>", "", s)
    return " ".join(html_mod.unescape(s).split())


def is_cjk(query):
    """是否含 CJK 字符——决定走本地中文语境还是境外英文语境。"""
    return bool(re.search(r"[\u4e00-\u9fff]", query))


def build_url(host, query, count):
    """构造本地 cn.bing /search URL（中文语境，含 setlang=zh-hans）。"""
    params = {"q": query, "count": count, "setlang": "zh-hans", "mkt": "zh-CN"}
    return "https://%s/search?%s" % (host, urllib.parse.urlencode(params))


def fetch_local(host, query, count):
    """GET 本地 bing /search（中文语境）；成功返回 HTML 文本，失败抛异常。"""
    req = urllib.request.Request(build_url(host, query, count),
                                 headers={"User-Agent": UA})
    with urllib.request.urlopen(req, timeout=TIMEOUT_SEC) as resp:
        return resp.read().decode("utf-8", errors="replace")


def _local_en_host(host, query, count):
    """GET 本地 <host> /search（英文语境 setlang=en&mkt=en-US&cc=US）。"""
    params = {"q": query, "count": count, "setlang": "en", "mkt": "en-US", "cc": "US"}
    req = urllib.request.Request("https://%s/search?" % host + urllib.parse.urlencode(params),
                                 headers={"User-Agent": UA})
    with urllib.request.urlopen(req, timeout=TIMEOUT_SEC) as resp:
        return resp.read().decode("utf-8", errors="replace")


def fetch_local_en(query, count):
    """GET 本地 www.bing.com /search（英文语境 setlang=en&mkt=en-US&cc=US）。

    2026-09 实证：本网络（住宅出口）直连 www.bing.com 对"稀有名+主题词"
    查询返回真实结果（~0.6s，ReAct 论文在首条），而 search-bot VPS 对同一
    查询被 bing 软屏蔽喂诱饵 SERP（回显查询、内容随机无关页）。故英文查询
    改为本地直连优先，search-bot 仅作兜底。失败抛异常（同 fetch_local）。"""
    params = {"q": query, "count": count, "setlang": "en", "mkt": "en-US", "cc": "US"}
    req = urllib.request.Request("https://www.bing.com/search?" + urllib.parse.urlencode(params),
                                 headers={"User-Agent": UA})
    with urllib.request.urlopen(req, timeout=TIMEOUT_SEC) as resp:
        return resp.read().decode("utf-8", errors="replace")


def fetch_en(query, count):
    """英文查询兜底：走 search-bot 境外跳板抓 www.bing.com（setlang=en&mkt=en-US）。
    返回 (html_text, forced_status, forced_msg)；forced_status 非空表示失败。"""
    remote_b64 = base64.b64encode(REMOTE_SCRIPT.encode("utf-8")).decode()
    q_b64 = base64.b64encode(query.encode("utf-8")).decode()
    cmd = ["ssh"] + SSH_OPTS + [SSH_HOST,
           "echo %s | base64 -d | bash -s %s %s" % (remote_b64, q_b64, count)]
    try:
        proc = subprocess.run(cmd, capture_output=True, text=True,
                              timeout=SSH_TIMEOUT_SEC)
    except subprocess.TimeoutExpired:
        return "", "timeout", f"bing(en) ssh to search-bot timed out after {SSH_TIMEOUT_SEC}s"
    if proc.returncode != 0:
        return "", "unavailable", f"bing(en) ssh to search-bot failed (rc={proc.returncode})"
    out = proc.stdout
    if out.startswith("@@SRC=ERR:"):
        code = out.split(":", 1)[1].strip()
        if code == "000":
            return "", "timeout", "bing(en) remote fetch timed out"
        return "", "unavailable", f"bing(en) remote HTTP {code}"
    return out, "", ""


def unwrap_bing_url(u):
    """境外 www.bing.com 外链是 ck/a 追踪包装（?…&u=<base64url> 携带真实 URL），
    解出真实 URL；非 ck/a 链接原样返回；解不出的 ck/a 丢弃（返回空串）。"""
    if "bing.com/ck/a" not in u:
        return html_mod.unescape(u)   # 普通外链也反转义（scholar ...&amp;hl=）
    u = html_mod.unescape(u)          # 原始 HTML 里 & 是 &amp;
    m = re.search(r"[?&]u=([^&]+)", u)
    if not m:
        return ""
    try:
        enc = m.group(1)
        if enc.startswith("a1"):          # bing ck/a 的 u 参数格式：a1 + base64url(URL)
            enc = enc[2:]
        enc = enc.replace("-", "+").replace("_", "/")   # base64url -> base64
        enc += "=" * (-len(enc) % 4)
        dec = base64.b64decode(enc).decode("utf-8", errors="replace")
        if dec.startswith(("http://", "https://")):
            return dec
    except Exception:  # noqa: BLE001 — 解不出就当追踪链接丢弃
        pass
    return ""


def parse_results(raw, count):
    """bing 结果块：<li class="b_algo">…<h2 class=""><a href="URL">标题</a></h2>
    …<div class="b_caption"><p class="b_lineclamp2">摘要</p></div>…</li>"""
    results = []
    seen = set()
    for m in re.finditer(r'<li class="b_algo".*?</li>', raw, re.S):
        block = m.group(0)
        a = re.search(r'<h2[^>]*>\s*<a[^>]*href="([^"]+)"[^>]*>(.*?)</a>', block, re.S)
        if not a:
            continue
        url = unwrap_bing_url(a.group(1))
        if not url or url.startswith(("https://www.bing.com/", "http://www.bing.com/", "javascript:")):
            continue                      # 丢弃 bing 内部链接 / 解不出的追踪链接
        if url in seen:
            continue                      # 按 URL 去重
        title = strip_tags(a.group(2))
        if not title:
            continue
        p = re.search(r"<p[^>]*>(.*?)</p>", block, re.S)
        snip = strip_tags(p.group(1))[:SNIPPET_CHARS] if p else ""
        seen.add(url)
        results.append({"title": title, "url": url, "snippet": snip})
        if len(results) >= count:
            break
    return results


# ---------------------------------------------------------------------------
# 相关性门（junk-SERP 检测）
#
# 2026-09 实证：bing 对"稀有名字 + 主题词"的多词查询（如 "shunyu yao arxiv"）
# 会返回与查询完全无关的缓存 SERP（Cambridge "appreciate" 词典页、知乎杂帖、
# Reddit 灌水、Google Maps 帮助页……每次请求内容还随机变化）——这是 bing 的
# bot 软屏蔽：页面正确回显查询、结构上是 b_algo 有机块，但内容是对其它缓存
# 查询的诱饵结果。脚本解析器无法从结构区分，只能做相关性门：
#   结果 title/snippet 必须至少命中一个查询关键词；全部 miss => 判为诱饵 SERP，
#   报 status=empty + message 说明（agent 转而换方法），绝不把垃圾当 ok 结果。
# CJK 查询按字符子串匹配（"姚顺雨" vs 标题含 "姚顺雨"），拉丁按词匹配。
# ---------------------------------------------------------------------------
# 查询词切分：拉丁按空白/标点；CJK 段做 2 字重叠 bigram（4+ 字查询对部分命中
# 的容忍，如 舜宇光学 vs 结果标题 舜宇集团）。
def query_terms(query):
    """切分查询为相关性匹配用 term 列表（拉丁词 >=2 字符 / CJK 2字重叠 bigram），
    丢弃 NL 脚手架词（find/all/for/download/by/of...）——这些词会巧合命中
    诱饵 SERP 标题（如 "Find your phone"），让垃圾逃过相关性门。"""
    terms = []
    for seg in re.split(r"[\s,;:!?。，；：！？]+", (query or "").strip()):
        if not seg:
            continue
        low = seg.lower()
        if low in QUERY_STOPWORDS:
            continue
        if re.search(r"[\u4e00-\u9fff]", seg):
            for c in re.findall(r"[\u4e00-\u9fff]+", seg):
                if len(c) == 2:
                    terms.append(c)
                elif len(c) > 2:
                    terms.extend(c[i:i + 2] for i in range(len(c) - 1))
        elif len(seg) >= 2:
            terms.append(low)
    return terms


def term_hit(term, text):
    """term 是否命中 text：CJK term 子串匹配，拉丁 term 子串匹配。"""
    low = text.lower()
    if re.search(r"[\u4e00-\u9fff]", term):
        return term in text
    return term in low


def junk_serp(query, results):
    """诱饵 SERP 检测：有结果、含至少一个"特征词"（>=4 拉丁字符或 CJK——
    脚手架词过滤后），但无一条结果命中任何查询词 => True。
    无特征词（如缩写查询 "llm"）无法可靠判定 => False（同义词风险），宁可不
    判也不误杀真实结果。

    命中判定看 title+snippet+URL：结果 URL 携带查询 token（如
    golang.google.cn）本身就是相关性证据 —— 2026-10 实测 SERP 首条
    (golang.google.cn) 标题/摘要都无字面 "golang"，只有 URL 有。特征词
    （>=4 拉丁字符或 CJK）的要求不变，URL 只做补充信号，不放松门。"""
    terms = query_terms(query)
    if not terms or not results:
        return False
    distinctive = [t for t in terms
                   if re.search(r"[\u4e00-\u9fff]", t) or len(t) >= 4]
    if not distinctive:
        return False
    for r in results:
        hay = " ".join((r.get("title") or "", r.get("snippet") or "",
                        r.get("url") or ""))
        if any(term_hit(t, hay) for t in terms):
            return False            # >=1 条命中 => 真实结果
    return True                     # 全部 miss => 诱饵 SERP


def finalize(payload, results, query, count, ok_message=""):
    """统一收尾：相关性门 + status/结果填充。results 是 parse_results 输出
    （可能为 []）。诱饵 SERP 报 empty 并说明，ok 才真正返回结果。

    门判定的样本 == 报告出去的样本（parse_results 已按 count 截断）：
    判"这页是不是诱饵"必须只依据"将要展示给 agent 的这几条"，否则出现
    「前几条件是垃圾、后几条碰巧相关」时会把垃圾当 ok 报出去
    （716 审计的核心指责）。"""
    if junk_serp(query, results):
        payload["status"] = "empty"
        payload["results"] = []
        payload["count"] = 0
        payload["message"] = (
            "bing returned results unrelated to the query "
            "(possible bot soft-block); treating as empty — try google/arxiv")
    else:
        payload["status"] = "ok" if results else "empty"
        payload["results"] = results
        payload["count"] = len(results)
        if results and ok_message:
            payload["message"] = ok_message
    return payload


def main():
    # 下游提前关闭管道（如 | head）时按默认行为退出，不打 BrokenPipeError
    signal.signal(signal.SIGPIPE, signal.SIG_DFL)
    ap = argparse.ArgumentParser(
        description="Bing web search, return slim JSON (0-token plugin)."
    )
    ap.add_argument("query", help='search query, e.g. "hermes agent"')
    ap.add_argument("--count", type=int, default=DEFAULT_COUNT,
                    help=f"max results (default {DEFAULT_COUNT}, capped at {MAX_COUNT})")
    args = ap.parse_args()

    count = max(1, min(args.count, MAX_COUNT))
    start = time.monotonic()
    payload = {"status": "unavailable", "method": "bing",
               "elapsed_ms": 0, "results": [], "count": 0}

    if not args.query.strip():
        payload["message"] = "empty query"
    elif is_cjk(args.query):
        # 中文查询：本地 cn.bing.com 直连（中文语境，最快）
        forced = ""          # "" | "timeout" | "unavailable"
        forced_msg = ""
        raw = ""
        for host in BING_HOSTS:
            try:
                raw = fetch_local(host, args.query, count)
                break                      # 成功
            except (socket.timeout, TimeoutError) as e:
                # 超时：网络级问题，换 host 大概率一样，快速失败（与 bash 版 rc=28 一致）
                forced = "timeout"
                forced_msg = f"bing timeout after {TIMEOUT_SEC}s: {e}"
                break
            except urllib.error.HTTPError as e:
                forced = "unavailable"     # 非 200：记下后试下一个 host
                forced_msg = f"bing HTTP {e.code}"
            except urllib.error.URLError as e:
                forced = "unavailable"
                forced_msg = f"bing unreachable: {e.reason}"
            except Exception as e:  # noqa: BLE001 — plugin must always emit JSON
                forced = "unavailable"
                forced_msg = f"bing fetch failed: {e!r}"
        if not forced:
            results = parse_results(raw, count)
            finalize(payload, results, args.query, count)
        else:
            payload["status"] = forced
            payload["message"] = forced_msg
    else:
        # 英文查询：本地 www.bing.com 直连（英文语境）优先 —— 2026-09 实证本
        # 网络直连对"稀有名+主题词"查询返回真实结果，search-bot VPS 反被 bing
        # 软屏蔽喂诱饵。本地失败（墙/超时/诱饵门）才走 search-bot 兜底。
        # bing 直连有偶发 200-空体/超时（每请求抖动，非稳定墙），host 失败重试
        # 一次再换下一个源（www.bing.com -> cn.bing.com -> search-bot 兜底）。
        hosts = ("www.bing.com", "cn.bing.com")
        sources = [("direct " + h, lambda q, h=h: _local_en_host(h, q, count))
                   for h in hosts]
        sources.append(("search-bot", lambda q: fetch_en(q, count)[0]))
        for label, fetch_fn in sources:
            forced = ""
            forced_msg = ""
            raw = ""
            for attempt in (1, 2):
                if raw:
                    break
                try:
                    raw = fetch_fn(args.query)
                except (socket.timeout, TimeoutError) as e:
                    forced, forced_msg = "timeout", f"bing({label}) timeout after {TIMEOUT_SEC}s: {e}"
                except urllib.error.HTTPError as e:
                    forced, forced_msg = "unavailable", f"bing({label}) HTTP {e.code}"
                except urllib.error.URLError as e:
                    forced, forced_msg = "unavailable", f"bing({label}) unreachable: {e.reason}"
                except Exception as e:  # noqa: BLE001 — plugin must always emit JSON
                    forced, forced_msg = "unavailable", f"bing({label}) fetch failed: {e!r}"
            if forced or not raw:
                continue                # 该源失败 -> 下一源
            results = parse_results(raw, count)
            if not results or junk_serp(args.query, results):
                continue                # 空解析/诱饵门 -> 下一源
            finalize(payload, results, args.query, count,
                     ok_message="source: " + label)
            break
        else:
            payload["status"] = "empty"
            payload["message"] = ("all bing sources failed or returned decoy "
                                  "(possible bot soft-block); treating as empty")

    payload["elapsed_ms"] = int((time.monotonic() - start) * 1000)
    json.dump(payload, sys.stdout, ensure_ascii=False)
    sys.stdout.write("\n")


if __name__ == "__main__":
    main()
