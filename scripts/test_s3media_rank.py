#!/usr/bin/env python3
"""Functional test for scripts/s3media_rank.py — no network.

Why this tool needs its own test: it exists because a PREFIX ranking gave a
misleading answer. `video/wechat/a.mp4` collapses into the prefix
`video/wechat`, so a prefix report can neither tell you what fraction of
egress is video, nor see that one .html accounts for half a gigabyte. The
tests therefore pin two things the prefix view cannot express:

  1. per-EXTENSION aggregation across unrelated prefixes
  2. the extension is taken from the KEY, and query/fragment suffixes
     (`x.html?debug=false&modules=...` appears in the real corpus) must not
     defeat the match
"""
import importlib.util
import io
import json
import os
import sys
from contextlib import redirect_stdout

sys.path.insert(0, os.path.join(os.path.dirname(os.path.abspath(__file__)), "lib"))
from tmp_ban import make_scratch_dir  # noqa: E402

HERE = os.path.dirname(os.path.abspath(__file__))
SCRIPT = os.path.join(HERE, "s3media_rank.py")


def load():
    spec = importlib.util.spec_from_file_location("s3media_rank", SCRIPT)
    mod = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(mod)
    return mod


def run(mod, fixture):
    d = make_scratch_dir("s3media-test")
    p = os.path.join(d, "agg.json")
    with open(p, "w") as f:
        json.dump(fixture, f)
    buf = io.StringIO()
    with redirect_stdout(buf):
        code = mod.main([p])
    return code, buf.getvalue()


def main():
    mod = load()
    checks = 0

    # --- 1. ext_of: strips query/fragment, ignores dots inside path segments
    assert mod.ext_of("a/b/c.pdf") == ".pdf"
    assert mod.ext_of("weain/hero3/load.php?debug=false&lang=en") == ".php", \
        mod.ext_of("weain/hero3/load.php?debug=false&lang=en")
    assert mod.ext_of("UPPER/NAME.MP4") == ".mp4", "must lowercase"
    # a dot in a directory name is not an extension
    assert mod.ext_of("v1.2/readme") == "", mod.ext_of("v1.2/readme")
    assert mod.ext_of("noext") == ""
    checks += 1

    # --- 2. aggregation spans prefixes: two .html in different dirs add up --
    fix = {
        "total_bytes": 1_000_000_000,
        "key": {
            "weain/a.html": [10, 300_000_000],
            "other/b.html": [5, 200_000_000],
            "video/wechat/x.mp4": [2, 50_000_000],
            "e-book/deep.pdf": [3, 400_000_000],
            "noext/thing": [1, 50_000_000],
        },
    }
    code, out = run(mod, fix)
    assert code == 0, code

    # html should appear ONCE, with the summed bytes (0.5 GB) and 50% share
    html_lines = [ln for ln in out.splitlines() if ln.startswith(".html")]
    assert len(html_lines) == 1, f"html must be one row:\n{out}"
    assert "0.5000" in html_lines[0], html_lines[0]
    assert "50.00%" in html_lines[0], html_lines[0]
    checks += 1

    # --- 3. pdf second, media far behind — the ordering the bill implies ---
    ext_rows = [ln.split()[0] for ln in out.splitlines()
                if ln.startswith(".") and "%" in ln]
    assert ext_rows[:3] == [".html", ".pdf", ".mp4"], ext_rows[:4]
    checks += 1

    # --- 4. no-extension keys get their own bucket, not dropped ------------
    assert "(none)" in out, out
    checks += 1

    # --- 5. the media section reports its SHARE (so "is video the problem?"
    #        is answerable from the output alone) --------------------------
    assert "MEDIA/DOCUMENT FILES ONLY" in out
    mline = [ln for ln in out.splitlines() if "distinct files" in ln]
    assert mline, out
    # .mp4 + .pdf are media/doc here: 0.05 + 0.40 = 0.45 GB of 1.0 = 45%
    assert "45.00% of egress" in mline[0], mline[0]
    assert "2 distinct files" in mline[0] or "files," in mline[0], mline[0]
    checks += 1

    # --- 6. shares sum to ~100% (nothing silently lost in bucketing) -------
    tot = [ln for ln in out.splitlines() if ln.startswith("TOTAL")]
    assert tot, out
    assert "100.00%" in tot[0], tot[0]
    checks += 1

    print(f"test_s3media_rank: {checks} checks PASS")
    return 0


if __name__ == "__main__":
    sys.exit(main())
