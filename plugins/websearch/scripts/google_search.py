#!/usr/bin/env python3
"""Google web search — 0-token websearch plugin script (W5, U1 python 迁移).

走 search-bot VPS 跳板（GFW 本地直连 google.com 不通），三层来源：
  1) 浏览器-SERP（CDP 主路径）：ssh searchbox → 有头 Chrome CDP 网关 (:9232)
     Runtime.evaluate 导航真实 SERP + 提取 title/url/snippet。
  2) curl SERP    ：ssh searchbox 'curl https://www.google.com/search ...'（udm=14 纯网页）。
  3) News RSS 兜底：ssh searchbox curl news.google.com/rss/search（免 JS 真实结果）。

访问路径（2026-08 实证，重要！）：
  - search-bot（DigitalOcean 机房）IPv4 对 google.com/search 返回 reCAPTCHA /sorry
    硬墙；IPv6（2001:db8:4:1d0::/64）只把"墙的类型"从 /sorry 换成 enablejs 软墙
    ——curl 无论 v4/v6/udm=14/gbv=1/带 cookie 都只能拿到 enablejs 页（无结果），
    有头 Chrome 无论 v4/v6 都被 /sorry 拦（页面明示该 VPS 的 IPv6 地址），
    预热（先开首页再搜索）与 reCAPTCHA 复选框点击均无效。
  => Epic 所述"IPv6 绕过 google 反爬"在本 VPS 上不成立：IPv6 只是避开 IPv4 的
     302/硬墙，但 SERP 结果永远拿不到。CDP 路径保持为正确架构（网关已在
     searchbox 上以 systemd google-cdp.service 常驻 + IPv6 强制连接），一旦 Google
     风控变化或接入住家退出 IP，CDP 主路径自动生效；当前实际由 News RSS 兜底。
  4) News-RSS 的 news.google.com 包装 URL 已实证无法解出真实文章 URL：
     重定向链止于 news.google.com 查看页（JS 渲染），CBMi 载荷为加密/签名数据，
     静态解码不可行。故兜底结果 URL 保持包装链接（可点击跳转真实文章）。

Usage:
  python3 google_search.py "<query>" [--count N]

Output (stdout, 单行 JSON):
  {"status":"ok|empty|timeout|unavailable","method":"google",
   "elapsed_ms":N,"results":[{"title","url","snippet"}],"count":N}
"""
import argparse
import base64
import html as html_mod
import json
import os
import re
import signal
import subprocess
import sys
import time
import urllib.parse
import urllib.request
import xml.etree.ElementTree as ET

# ---------------------------------------------------------------------------
# Constants — single source of truth, no magic numbers inline (Rule 1)
# ---------------------------------------------------------------------------
SSH_HOST = os.environ.get("SSH_HOST", "searchbox")
SSH_OPTS = ["-o", "ConnectTimeout=8", "-o", "BatchMode=yes", "-T"]  # 快速失败 + 非交互
SSH_TIMEOUT_SEC = 30     # 整个 ssh 往返总超时（curl SERP/RSS，最坏 ~10s + 握手 2-6s）
CDP_SSH_TIMEOUT_SEC = 40 # CDP 网关往返超时（页面加载 ~3.5s + 轮询 ~15s + 握手 2-6s；IPv6 主路径需留足余量）
TIMEOUT_SEC = 5          # 远端 curl --max-time（SERP 与 RSS 各一次）
CDP_GATEWAY_PORT = int(os.environ.get("CDP_GATEWAY_PORT", "9232"))
DEFAULT_COUNT = 5        # 默认返回条数
MAX_COUNT = 20           # 硬上限，保证 JSON 精简（0-token）
SNIPPET_CHARS = 200      # snippet 截断长度
UA = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/126.0.0.0 Safari/537.36"

# 远端脚本 1（curl SERP / News RSS 兜底，base64 打包传参避免注入；随 ssh 单次往返执行）
# 输出约定：stdout 首行 @@SRC=serp|news-rss|ERR:<code>，随后为原始 HTML/XML
REMOTE_SCRIPT = r"""set -u
UA='Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/126.0.0.0 Safari/537.36'
q=$(printf '%s' "$1" | base64 -d 2>/dev/null)          # $1 = 本地 base64 的 query
t=$(mktemp -d) || exit 1
trap 'rm -rf "$t"' EXIT
# 1) google.com SERP（udm=14 纯网页，无 AI overview）
c=$(curl -sS -L --max-time 5 -A "$UA" -G 'https://www.google.com/search' \
      --data-urlencode "q=$q" --data-urlencode 'udm=14' \
      --data-urlencode 'hl=en' --data-urlencode 'gl=us' --data-urlencode 'num=20' \
      -o "$t/serp" -w '%{http_code}')
if [ "$c" = 200 ] && ! grep -qE 'enablejs|httpservice/retry|SG_REL|sorry/index' "$t/serp"; then
    echo '@@SRC=serp'; cat "$t/serp"                    # 真 SERP
else
    # 2) enablejs/sorry 墙/非 200 => Google News RSS 兜底（免 JS 的真实 Google 结果）
    c2=$(curl -sS -L --max-time 5 -A "$UA" -G 'https://news.google.com/rss/search' \
           --data-urlencode "q=$q" --data-urlencode 'hl=en-US' \
           --data-urlencode 'gl=US' --data-urlencode 'ceid=US:en' \
           -o "$t/rss" -w '%{http_code}')
    if [ "$c2" = 200 ]; then echo '@@SRC=news-rss'; cat "$t/rss"
    else echo "@@SRC=ERR:$c2"; fi
fi
"""

# 远端脚本 2（CDP 主路径）：远端 python3 GET 本机 CDP 网关，取 JSON 结果。
# 输出约定：首行 @@SRC=cdp，随后为网关 JSON（{"status",..,"results",..,"message"}）；
#           失败首行 @@SRC=CDPERR:<msg>
REMOTE_CDP = r"""python3 -c 'import urllib.request,json,sys
q=sys.argv[1] if len(sys.argv)>1 else ""
try:
 d=json.load(urllib.request.urlopen("http://127.0.0.1:%s/search?q="+urllib.parse.quote(q)+"&n=%d",timeout=45))
 print("@@SRC=cdp"); print(json.dumps(d,ensure_ascii=False))
except Exception as e:
 print("@@SRC=CDPERR:"+repr(e))
'""" % (CDP_GATEWAY_PORT, MAX_COUNT)


def strip_tags(s):
    """剥离 HTML 标签 + 反转义实体 + 折叠空白（snippet/title 蒸馏核心）。"""
    s = re.sub(r"<[^>]+>", "", s)
    return " ".join(html_mod.unescape(s).split())


def unwrap_google_url(u):
    """Google SERP 外链形如 /url?q=<real>&sa=...；解出真实 URL。"""
    if u.startswith("/url?q=") or u.startswith("https://www.google.com/url?q="):
        m = re.search(r"[?&]q=([^&]+)", u)
        if m:
            return urllib.parse.unquote(m.group(1))
    return u


def parse_serp(body, count):
    """现代 SERP（MjjYud 块：<a href><h3>标题</h3></a> + VwiC3b 摘要）+ 旧版 gbv 块。"""
    results = []
    seen = set()
    for m in re.finditer(r'<div class="MjjYud".*?</div>', body, re.S):
        a = re.search(r'<a[^>]*href="([^"]+)"[^>]*>\s*<h3[^>]*>(.*?)</h3>', m.group(0), re.S)
        if not a:
            continue
        url = unwrap_google_url(a.group(1))
        if url.startswith("https://www.google.com/") and not url.startswith("https://www.google.com/url?q="):
            continue
        if url in seen:
            continue
        title = strip_tags(a.group(2))
        if not title:
            continue
        snip_m = re.search(r'<div class="VwiC3b[^"]*"[^>]*>(.*?)</div>', m.group(0), re.S)
        snip = strip_tags(snip_m.group(1))[:SNIPPET_CHARS] if snip_m else ""
        seen.add(url)
        results.append({"title": title, "url": url, "snippet": snip})
        if len(results) >= count:
            break
    return results


def parse_news_rss(body, count):
    """Google News RSS：title 直接用；url 取 description 锚点（news.google.com 包装 ——
    已实证无法解出真实 URL：重定向链止于 news.google.com 查看页，CBMi 载荷已加密）；
    snippet 取锚点之后的 "- Source - 摘要" 文本。"""
    root = ET.fromstring(body)
    results = []
    seen = set()
    for it in root.findall(".//item"):
        title = strip_tags(it.findtext("title") or "")
        d = it.findtext("description") or ""
        am = re.search(r'<a href="([^"]+)"[^>]*>(.*?)</a>', d, re.S)
        if am:
            url = html_mod.unescape(am.group(1))
            desc_body = d[am.end():]           # 锚点之后 = "- Source - 摘要"
        else:
            url = ""
            desc_body = d
        url = url or (it.findtext("link") or "").strip()
        snip = strip_tags(desc_body)
        snip = re.sub(r"^-\s*", "", snip).strip()
        if not snip:
            snip = strip_tags(it.findtext("source") or "")
        snip = snip[:SNIPPET_CHARS]
        if not title or not url or url in seen:
            continue
        seen.add(url)
        results.append({"title": title, "url": url, "snippet": snip})
        if len(results) >= count:
            break
    return results


def _ssh_run(remote_script, query_b64, timeout):
    """Runner 执行前置脚本（输入为 query 的 base64）。返回 (stdout_text, forced, forced_msg)"""
    cmd = ["ssh"] + SSH_OPTS + [SSH_HOST,
           "echo %s | base64 -d | bash -s %s" % (remote_script, query_b64)]
    try:
        proc = subprocess.run(cmd, capture_output=True, text=True, timeout=timeout)
    except subprocess.TimeoutExpired:
        return "", "timeout", f"ssh to search-bot timed out after {timeout}s"
    if proc.returncode != 0:
        return "", "unavailable", f"ssh to search-bot failed (rc={proc.returncode})"
    return proc.stdout, "", ""


def cdp_search(query, count):
    """CDP 主路径：远端 python3 GET CDP 网关。成功返回 (results, message)；失败返回 ([], message)。"""
    remote = base64.b64encode(REMOTE_CDP.encode("utf-8")).decode()
    qb = base64.b64encode(query.encode("utf-8")).decode()
    stdout, forced, msg = _ssh_run(remote, qb, CDP_SSH_TIMEOUT_SEC)
    if forced:
        return [], f"cdp {msg}"
    lines = stdout.split("\n", 1)
    marker = lines[0].strip()
    body = lines[1] if len(lines) > 1 else ""
    if marker == "@@SRC=cdp":
        try:
            data = json.loads(body)
        except (ValueError, AttributeError):
            return [], "cdp parse failed"
        results = data.get("results") or []
        if results:
            return results[:count], data.get("message", "") or "source: google cdp"
        detail = data.get("message", "") or "cdp empty"
        return [], f"cdp empty ({detail})"
    if marker.startswith("@@SRC=CDPERR:"):
        return [], f"cdp {marker.split(':', 1)[1]}"
    return [], f"cdp unexpected payload: {marker}"


def fallback_search(args_query, count):
    """curl SERP → News RSS 兜底（原有逻辑，bas64 打包远端 curl 脚本）。"""
    remote_b64 = base64.b64encode(REMOTE_SCRIPT.encode("utf-8")).decode()
    q_b64 = base64.b64encode(args_query.encode("utf-8")).decode()
    stdout, forced, forced_msg = _ssh_run(remote_b64, q_b64, SSH_TIMEOUT_SEC)
    if forced:
        return forced, forced_msg, []
    lines = stdout.split("\n", 1)
    marker = lines[0].strip()
    body = lines[1] if len(lines) > 1 else ""
    msg = ""
    results = []
    if marker == "@@SRC=serp":
        results = parse_serp(body, count)
    elif marker == "@@SRC=news-rss":
        try:
            results = parse_news_rss(body, count)
        except ET.ParseError:
            return "unavailable", "news rss parse failed", []
        msg = "source: google news rss (SERP js-walled for this IP)"
    elif marker.startswith("@@SRC=ERR:"):
        code = marker.split(":", 1)[1]
        if code == "000":
            return "timeout", f"google timeout after {code}s fetch", []
        return "unavailable", f"google HTTP {code}", []
    else:
        return "unavailable", "unexpected payload from search-bot", []
    return "ok" if results else "empty", msg, results


def main():
    # 下游提前关闭管道（如 | head）时按默认行为退出，不打 BrokenPipeError
    signal.signal(signal.SIGPIPE, signal.SIG_DFL)
    ap = argparse.ArgumentParser(
        description="Google web search via search-bot VPS: CDP/browser-SERP first, RSS fallback."
    )
    ap.add_argument("query", help='search query, e.g. "deepseek"')
    ap.add_argument("--count", type=int, default=DEFAULT_COUNT,
                    help=f"max results (default {DEFAULT_COUNT}, capped at {MAX_COUNT})")
    args = ap.parse_args()

    count = max(1, min(args.count, MAX_COUNT))
    start = time.monotonic()
    payload = {"status": "unavailable", "method": "google",
               "elapsed_ms": 0, "results": [], "count": 0}

    if not args.query.strip():
        payload["message"] = "empty query"
    else:
        # ---- 1. CDP 主路径（有头浏览器 SERP）----
        cdp_results, cdp_msg = cdp_search(args.query, count)
        if cdp_results:
            payload["status"] = "ok"
            payload["results"] = cdp_results
            payload["count"] = len(cdp_results)
            payload["message"] = cdp_msg
        elif "challenge" in cdp_msg or "/sorry" in cdp_msg:
            # ---- 2a. CDP 网关报告 Google 反爬挑战（reCAPTCHA /sorry，缓存判定）----
            # 同一出口 IP 的 curl SERP / News RSS 兜底必撞同一堵墙（2026-08 实证），
            # 再花 ~10s 重试只是浪费时间——快速失败并说明，等住家出口/换 IP 后 CDP
            # 主路径自动生效。merged 路径下 bing/arxiv 正常返回，google 快速降级。
            payload["status"] = "unavailable"
            payload["message"] = (
                f"google blocked: reCAPTCHA /sorry challenge for this egress IP "
                f"({cdp_msg}); needs residential IP / VPN route — see devdoc "
                f"websearch-multisource. bing/arxiv unaffected.")
        else:
            # ---- 2b. CDP 其他失败 → curl SERP / News RSS 兜底 ----
            fb_status, fb_msg, fb_results = fallback_search(args.query, count)
            payload["status"] = fb_status
            payload["message"] = f"cdp unavailable ({cdp_msg}); fallback {fb_msg}".strip("; ")
            payload["results"] = fb_results
            payload["count"] = len(fb_results)

    payload["elapsed_ms"] = int((time.monotonic() - start) * 1000)
    json.dump(payload, sys.stdout, ensure_ascii=False)
    sys.stdout.write("\n")


if __name__ == "__main__":
    main()
