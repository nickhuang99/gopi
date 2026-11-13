#!/usr/bin/env python3
"""Functional test for scripts/ds_pricing_snapshot.py — no network needed.

The script's whole value is: snapshot the pricing table, and LOUDLY report a
change vs the previous snapshot. A test that only runs it once proves nothing.
We therefore redirect its snapshot dir to a temp location, feed it two
different fake pricing-page texts, and assert:
  1. first run  -> baseline, "nothing to compare"
  2. identical  -> "UNCHANGED"
  3. changed    -> "*** CHANGED ***" + field-level diff
  4. extract_table parses model names / versions / prices correctly
"""
import importlib.util
import io
import json
import os
import shutil
import sys
import tempfile
from contextlib import redirect_stdout

sys.path.insert(0, os.path.join(os.path.dirname(os.path.abspath(__file__)), "lib"))
from tmp_ban import make_scratch_dir  # noqa: E402

HERE = os.path.dirname(os.path.abspath(__file__))
SCRIPT = os.path.join(HERE, "ds_pricing_snapshot.py")

PAGE_A = """模型细节 模型 deepseek-flash (1) deepseek-v4-pro (2) 模型版本 DeepSeek-V4.1-Flash DeepSeek-V4-Pro-0813
价格 百万tokens输入 （缓存命中） 空闲时段 0.02元 0.15元 高峰时段 0.04元 0.30元
百万tokens输出 空闲时段 4元 13.5元 扣费规则"""

# Same shape, but V4 Pro retired: its column is gone, prices collapsed to one.
PAGE_B = """模型细节 模型 deepseek-flash (1) 模型版本 DeepSeek-V4.1-Flash
价格 百万tokens输入 （缓存命中） 空闲时段 0.02元 高峰时段 0.04元
百万tokens输出 空闲时段 4元 扣费规则"""


def load_module():
    spec = importlib.util.spec_from_file_location("dssnap", SCRIPT)
    if spec is None or spec.loader is None:
        raise RuntimeError(f"cannot load {SCRIPT}")
    m = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(m)
    return m


def stub(m, text, snap_dir):
    """Point the module at a fake page + temp snapshot dir."""
    setattr(m, "SNAP_DIR", snap_dir)
    setattr(m, "fetch_text", lambda: text)
    return m


def run(m, text, snap_dir):
    stub(m, text, snap_dir)
    buf = io.StringIO()
    with redirect_stdout(buf):
        sys.argv = ["ds_pricing_snapshot.py"]
        m.main()
    return buf.getvalue()


def main():
    m = load_module()
    tmp = make_scratch_dir("dssnap-test")  # under the scratch queue, off `/`
    failures = []

    def check(name, cond, extra=""):
        print(f"  {'PASS' if cond else 'FAIL'}  {name}{'  ' + extra if extra and not cond else ''}")
        if not cond:
            failures.append(name)

    try:
        print("== extract_table parsing ==")
        p = m.extract_table(PAGE_A)
        check("parses both flash model names",
              "deepseek-flash" in p["model_names"] and "deepseek-v4-flash" not in p["model_names"],
              f"got {p['model_names']}")
        check("parses both versions",
              p["versions"][:2] == ["DeepSeek-V4.1-Flash", "DeepSeek-V4-Pro-0813"],
              f"got {p['versions']}")
        check("parses prices across hit/miss/output rows",
              p["prices"][:6] == ["0.02", "0.15", "0.04", "0.30", "4", "13.5"],
              f"got {p['prices'][:6]}")

        print("== run 1: baseline ==")
        out = run(m, PAGE_A, tmp)
        check("baseline reports no comparison", "first snapshot" in out, out)
        check("baseline prints model names", "deepseek-v4-pro" in out, out)

        print("== run 2: unchanged ==")
        out = run(m, PAGE_A, tmp)
        check("identical page -> UNCHANGED", "UNCHANGED" in out, out)
        check("identical page -> no CHANGED marker", "*** CHANGED" not in out, out)

        print("== run 3: changed page (V4 Pro retired) ==")
        out = run(m, PAGE_B, tmp)
        check("changed page -> CHANGED marker", "*** CHANGED" in out, out)
        check("diff names the dropped version", "DeepSeek-V4-Pro-0813" in out, out)
        check("diff shows field deltas", "model_names" in out or "versions" in out, out)

        print("== run 4: idempotent after change ==")
        out = run(m, PAGE_B, tmp)
        check("re-running the new page -> UNCHANGED", "UNCHANGED" in out, out)

        print("== run 5: fetch failure must NOT poison the baseline ==")
        before = sorted(os.listdir(tmp))
        stub(m, "Service Unavailable — please retry later", tmp)
        buf = io.StringIO()
        code = 0
        try:
            with redirect_stdout(buf):
                sys.argv = ["ds_pricing_snapshot.py"]
                m.main()
        except SystemExit as e:
            code = e.code or 0
        check("empty/garbage fetch aborts", code == 2, f"exit={code}")
        check("abort wrote no snapshot", sorted(os.listdir(tmp)) == before,
              f"{before} -> {sorted(os.listdir(tmp))}")
        check("abort is not reported as a table change",
              "*** CHANGED" not in buf.getvalue(), buf.getvalue())

        print("== run 6: baseline still comparable after an aborted run ==")
        stub(m, PAGE_B, tmp)
        out = run(m, PAGE_B, tmp)
        check("real page after abort -> UNCHANGED (baseline intact)",
              "UNCHANGED" in out, out)

        print("== snapshot files written ==")
        n = len([f for f in os.listdir(tmp) if f.endswith(".json")])
        check("5 snapshots on disk (6 runs, 1 aborted)", n == 5, f"got {n}")
        last = sorted(f for f in os.listdir(tmp) if f.endswith(".json"))[-1]
        snap = json.load(open(os.path.join(tmp, last)))
        check("snapshot has parsed block", "parsed" in snap and "text" in snap)
    finally:
        shutil.rmtree(tmp, ignore_errors=True)

    print()
    if failures:
        print(f"RESULT: FAIL ({len(failures)}): {failures}")
        return 1
    print("RESULT: ALL PASS")
    return 0


if __name__ == "__main__":
    sys.exit(main())
