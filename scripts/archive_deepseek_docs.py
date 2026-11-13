#!/usr/bin/env python3
"""DeepSeek API 文档全站归档器。

来源: https://api-docs.deepseek.com/sitemap.xml （Docusaurus 站点，sitemap 完整）
输出: 默认 ~/docs/deepseek-api/ ，保留 URL 路径结构为目录。

为什么用 sitemap 而不是爬链接：Docusaurus 的侧边栏是 JS 渲染的，页面里拿不到
完整目录；sitemap.xml 是站点自曝的全量清单（GLM 那边对应的东西是 llms.txt）。
有全站索引就别做 SPA 截图。

设计成可测的：网络取回集中在 fetch_sitemap/fetch_page 两个函数（测试会替换
它们），解析/清洗/落盘都是纯函数。
"""
import argparse
import os
import re
import sys
import time
import urllib.request

SITEMAP = "https://api-docs.deepseek.com/sitemap.xml"
HOST = "https://api-docs.deepseek.com/"
DEFAULT_OUT = os.path.expanduser("~/docs/deepseek-api")
UA = "Mozilla/5.0 (compatible; docs-archiver/1.0)"
DELAY = 0.7      # 礼貌间隔（秒）
TIMEOUT = 25


# ---------- 网络（测试会替换这两个） ----------

def fetch_sitemap() -> str:
    return _get(SITEMAP)


def fetch_page(url: str) -> str:
    return _get(url)


def _get(url: str) -> str:
    req = urllib.request.Request(url, headers={"User-Agent": UA})
    with urllib.request.urlopen(req, timeout=TIMEOUT) as r:
        return r.read().decode("utf-8", errors="replace")


# ---------- 纯函数 ----------

def parse_sitemap(xml: str) -> list:
    """提取本站在 sitemap 里的全部 URL：去重、去外站、去路径穿越。"""
    urls = []
    seen = set()
    for u in re.findall(r"<loc>\s*([^<\s]+)\s*</loc>", xml):
        if not u.startswith(HOST) or ".." in u:
            continue
        if u not in seen:
            seen.add(u)
            urls.append(u)
    return urls


def path_for(url: str) -> str:
    """URL → 相对落盘路径（根页 = index.md）。"""
    rel = url[len(HOST):].strip("/") or "index"
    rel = rel.replace("..", "_")
    return rel + ".md"


def strip_html(html: str) -> str:
    """从 Docusaurus 页面提取正文：留 prose 与代码块，去 script/style/nav。"""
    html = re.sub(r"(?is)<(script|style)\b.*?</\1>", " ", html)
    m = re.search(r"(?is)<article[^>]*>(.*?)</article>", html)
    body = m.group(1) if m else html
    # 代码块：保留内容，用 ``` 包裹
    body = re.sub(
        r"(?is)<pre[^>]*>(.*?)</pre>",
        lambda mm: "\n```\n" + re.sub(r"(?is)<[^>]+>", "", mm.group(1)) + "\n```\n",
        body,
    )
    # 块级标签 → 换行；其余标签 → 删除
    body = re.sub(r"(?i)</?(p|div|li|tr|h[1-6]|br|table|ul|ol|nav|footer)[^>]*>", "\n", body)
    body = re.sub(r"(?is)<[^>]+>", "", body)
    for ent, ch in (("&quot;", '"'), ("&amp;", "&"), ("&lt;", "<"), ("&gt;", ">"),
                    ("&#x27;", "'"), ("&nbsp;", " ")):
        body = body.replace(ent, ch)
    body = re.sub(r"\n{3,}", "\n\n", body)
    return "\n".join(ln.strip() for ln in body.splitlines() if ln.strip())


# ---------- 主流程 ----------

def main(argv=None) -> int:
    ap = argparse.ArgumentParser(description="Archive the DeepSeek API docs site.")
    ap.add_argument("--out", default=DEFAULT_OUT, help=f"output dir (default {DEFAULT_OUT})")
    ap.add_argument("--delay", type=float, default=DELAY, help="seconds between requests")
    ap.add_argument("--dry-run", action="store_true", help="list pages, write nothing")
    args = ap.parse_args(argv)

    try:
        urls = parse_sitemap(fetch_sitemap())
    except Exception as e:                       # noqa: BLE001 — surfaced, not swallowed
        print(f"FATAL: sitemap fetch failed: {e}")
        return 1
    if not urls:
        print("FATAL: sitemap parsed to zero urls (format changed?)")
        return 1
    print(f"sitemap: {len(urls)} urls")

    if args.dry_run:
        for u in urls:
            print(f"  would write {path_for(u)}  <- {u}")
        return 0

    os.makedirs(args.out, exist_ok=True)
    ok, fail = [], []
    for i, url in enumerate(urls, 1):
        dest = os.path.join(args.out, path_for(url))
        os.makedirs(os.path.dirname(dest) or args.out, exist_ok=True)
        try:
            text = strip_html(fetch_page(url))
            with open(dest, "w", encoding="utf-8") as f:
                f.write(f"<!-- source: {url} -->\n\n{text}\n")
            ok.append((url, len(text)))
            print(f"  [{i}/{len(urls)}] OK   {len(text):6d}B  {path_for(url)}")
        except Exception as e:                   # noqa: BLE001 — collected for the report
            fail.append((url, str(e)))
            print(f"  [{i}/{len(urls)}] FAIL {path_for(url)}: {e}")
        if i < len(urls):
            time.sleep(args.delay)

    with open(os.path.join(args.out, "download-report.md"), "w", encoding="utf-8") as f:
        f.write("# DeepSeek API docs archive\n\n")
        # NO timestamp here: the report must be byte-identical on an unchanged
        # rerun, or every refresh shows a spurious git diff and idempotence
        # (the property this file is checked for) becomes unverifiable.
        f.write(f"- source: {SITEMAP}\n")
        f.write(f"- total: {len(urls)}  ok: {len(ok)}  failed: {len(fail)}\n\n## FAILED\n\n")
        for u, e in fail:
            f.write(f"- {u} — {e}\n")
        f.write("\n## OK (bytes)\n\n")
        for u, n in sorted(ok, key=lambda x: -x[1]):
            f.write(f"- {n:6d}  {u}\n")

    print(f"\nDONE ok={len(ok)} fail={len(fail)} -> {args.out}")
    return 0 if not fail else 2


if __name__ == "__main__":
    sys.exit(main())
