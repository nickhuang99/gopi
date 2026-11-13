#!/usr/bin/env python3
"""Functional test for scripts/archive_deepseek_docs.py — no network needed.

The archiver's whole value is: walk a sitemap, write one file per page, and
report failures LOUDLY. A test that only runs it once proves nothing. So we
drive its pure functions directly against synthetic sitemap/HTML fixtures and
assert the properties that matter:

  1. parse_sitemap extracts every <loc>, dedups, keeps only the docs host
  2. strip_html keeps article prose AND code blocks, drops nav/script noise
  3. path_for maps a URL to a safe on-disk path (no traversal, index for root)
  4. a fetch failure lands in the report's FAILED section, exit != 0
  5. rerunning against identical input is idempotent (same files, same bytes)
  6. --dry-run writes nothing
"""
import importlib.util
import io
import os
import sys
import tempfile
from contextlib import redirect_stdout

sys.path.insert(0, os.path.join(os.path.dirname(os.path.abspath(__file__)), "lib"))
from tmp_ban import make_scratch_dir  # noqa: E402

HERE = os.path.dirname(os.path.abspath(__file__))
SCRIPT = os.path.join(HERE, "archive_deepseek_docs.py")


def load():
    spec = importlib.util.spec_from_file_location("archiver", SCRIPT)
    mod = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(mod)
    return mod


SITEMAP = """<?xml version="1.0" encoding="UTF-8"?>
<urlset xmlns="http://www.sitemaps.org/schemas/sitemap/0.9">
<url><loc>https://api-docs.deepseek.com/</loc></url>
<url><loc>https://api-docs.deepseek.com/guides/vision</loc></url>
<url><loc>https://api-docs.deepseek.com/api/get-user-balance</loc></url>
<url><loc>https://api-docs.deepseek.com/guides/vision</loc></url>
<url><loc>https://evil.example.com/../etc/passwd</loc></url>
</urlset>"""

PAGE = """<html><head>
<script>var noise = 1;</script>
<style>.x{color:red}</style>
</head><body>
<nav>SKIP-ME-NAV</nav>
<article>
<h1>Vision</h1>
<p>Send an image as a data: URL.</p>
<pre>client.chat.completions.create(model="deepseek-flash")</pre>
<p>The body limit is 48 MiB.</p>
</article>
<footer>SKIP-ME-FOOTER</footer>
</body></html>"""


def run(mod, argv, sitemap=SITEMAP, pages=None, fail_urls=()):
    """Drive main() with stubbed network. Returns (exit_code, stdout)."""
    pages = pages or {}
    mod.fetch_sitemap = lambda: sitemap

    def fake_fetch(url):
        if url in fail_urls:
            raise RuntimeError("simulated 500")
        return pages.get(url, PAGE)

    mod.fetch_page = fake_fetch
    buf = io.StringIO()
    with redirect_stdout(buf):
        code = mod.main(argv)
    return code, buf.getvalue()


def main():
    mod = load()
    out_dir = make_scratch_dir("deepseek-archive-test")
    checks = 0

    # 1. sitemap parsing: dedup, host filter, traversal rejected
    urls = mod.parse_sitemap(SITEMAP)
    assert urls.count("https://api-docs.deepseek.com/guides/vision") == 1, \
        f"dedup failed: {urls}"
    assert all(u.startswith("https://api-docs.deepseek.com/") for u in urls), \
        f"host filter failed: {urls}"
    assert not any("passwd" in u for u in urls), f"traversal leaked: {urls}"
    checks += 1

    # 2. strip_html: prose + code kept, nav/footer/script/style dropped
    txt = mod.strip_html(PAGE)
    for want in ("Vision", "data: URL", "deepseek-flash", "48 MiB"):
        assert want in txt, f"strip_html lost {want!r}: {txt[:200]}"
    for drop in ("SKIP-ME-NAV", "SKIP-ME-FOOTER", "var noise", "color:red"):
        assert drop not in txt, f"strip_html kept {drop!r}"
    checks += 1

    # 3. path mapping: root -> index, nested preserved, no escaping
    assert mod.path_for("https://api-docs.deepseek.com/").endswith("index.md")
    assert mod.path_for("https://api-docs.deepseek.com/guides/vision") \
        .endswith(os.path.join("guides", "vision.md"))
    evil = mod.path_for("https://api-docs.deepseek.com/../../etc/passwd")
    assert ".." not in evil, f"path traversal not sanitized: {evil}"
    checks += 1

    # 4. failure is loud: exit != 0 and the URL appears in the report
    code, out = run(mod, ["--out", out_dir],
                    fail_urls=("https://api-docs.deepseek.com/guides/vision",))
    assert code != 0, f"failure did not fail the run (exit={code})"
    report = open(os.path.join(out_dir, "download-report.md")).read()
    assert "guides/vision" in report, "failed url missing from report"
    assert "FAIL" in out, "failure not printed"
    checks += 1

    # 5. idempotent rerun: same bytes, zero failures
    code1, _ = run(mod, ["--out", out_dir])
    first = {}
    for root, _, files in os.walk(out_dir):
        for f in files:
            p = os.path.join(root, f)
            first[p] = open(p, "rb").read()
    code2, _ = run(mod, ["--out", out_dir])
    assert code1 == 0 and code2 == 0, f"clean runs should exit 0 ({code1},{code2})"
    for p, b in first.items():
        assert open(p, "rb").read() == b, f"rerun changed {p}"
    checks += 1

    # 6. --dry-run writes nothing
    dry = make_scratch_dir("deepseek-archive-dry")
    code, _ = run(mod, ["--out", dry, "--dry-run"])
    assert code == 0
    assert not any(f.endswith(".md") for _, _, fs in os.walk(dry) for f in fs), \
        "--dry-run wrote files"
    checks += 1

    # Self-check: the count and the label must agree. A hardcoded
    # denominator once read "5/6" on a green run and "8/7" on another —
    # a label that contradicts the run costs someone an investigation.
    assert checks == 6, f"expected 6 checks, ran {checks}"
    print(f"test_archive_deepseek_docs: {checks} checks PASS")
    return 0


if __name__ == "__main__":
    sys.exit(main())
