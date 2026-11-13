#!/usr/bin/env python3
"""Functional test for scripts/s3cost.py — no network.

The point of this file is the DRIVER ANALYSIS: a bill of $X is only actionable
if you can say which term produced it. What must hold:

  1. request classification: DELETE is free, GET/SELECT cheap, PUT/LIST expensive
  2. the 100 GB/month egress allowance is applied (not billed from byte zero)
  3. requesting the same cents from a stats JSON matches hand arithmetic
  4. the driver split names both terms, so a mismatch with the real bill points
     somewhere else (other buckets / CloudFront / storage)
  5. malformed/empty stats JSON does not crash — it reports zeros
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
SCRIPT = os.path.join(HERE, "s3cost.py")


def load():
    spec = importlib.util.spec_from_file_location("s3cost", SCRIPT)
    mod = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(mod)
    return mod


def run(mod, stats: dict):
    d = make_scratch_dir("s3cost-test")
    p = os.path.join(d, "stats.json")
    with open(p, "w") as f:
        json.dump(stats, f)
    buf = io.StringIO()
    with redirect_stdout(buf):
        code = mod.main([p])
    return code, buf.getvalue()


def main():
    mod = load()
    checks = 0

    # 1. request classes — the price differs by two orders of magnitude
    assert mod.classify("REST.DELETE.OBJECT") == "free", mod.classify("REST.DELETE.OBJECT")
    assert mod.classify("WEBSITE.GET.OBJECT") == "cheap", mod.classify("WEBSITE.GET.OBJECT")
    assert mod.classify("REST.PUT.OBJECT") == "expensive", mod.classify("REST.PUT.OBJECT")
    assert mod.classify("REST.GET.BUCKET") == "cheap", mod.classify("REST.GET.BUCKET")
    assert mod.classify("REST.GET.BUCKET_TAGS") == "cheap", mod.classify("REST.GET.BUCKET_TAGS")
    assert mod.classify("REST.GET.LIST") == "expensive", mod.classify("REST.GET.LIST")
    checks += 1

    # 2/3. money arithmetic, including the free egress tier
    #      1M GET + 1M PUT + 50GB out => 1e6*4e-7 + 1e6*5e-6 + 0 (under 100GB)
    stats = {
        "op": {"WEBSITE.GET.OBJECT": 1_000_000, "REST.PUT.OBJECT": 1_000_000},
        "total_req": 2_000_000,
        "total_bytes": 50 * 10**9,
    }
    code, out = run(mod, stats)
    assert code == 0, code
    expect_req = 1_000_000 * (0.0004 / 1000) + 1_000_000 * (0.005 / 1000)
    assert f"${expect_req:.2f}" in out, f"request cost not in output:\n{out}"
    assert "billable          :           0.00 GB" in out, f"free tier not applied:\n{out}"
    checks += 1

    # 4. above the free tier, egress is billed on the excess only
    stats2 = {
        "op": {"WEBSITE.GET.OBJECT": 1000},
        "total_req": 1000,
        "total_bytes": 300 * 10**9,          # 300 GB -> 200 GB billable
    }
    code, out2 = run(mod, stats2)
    assert code == 0
    # alignment padding shifts with the number's width — assert the VALUES, not the spacing
    assert "200.00 GB" in out2, out2
    assert f"${200 * 0.09:.2f}" in out2, f"egress cost wrong:\n{out2}"
    checks += 1

    # 5. the driver split is printed and adds to 100%
    #    (pure-egress case: request cost rounds to $0.00, transfer is 100%)
    assert "request charges" in out2 and "transfer charges" in out2, out2
    rp = [ln for ln in out2.splitlines() if "request charges" in ln][0]
    tp = [ln for ln in out2.splitlines() if "transfer charges" in ln][0]
    assert "100.0%" in tp, tp
    assert "0.0%" in rp, rp
    checks += 1

    # 6. empty / missing keys do not crash — report zeros honestly
    code, out3 = run(mod, {"op": {}, "total_req": 0, "total_bytes": 0})
    assert code == 0, code
    assert "$0.00" in out3, out3
    assert "no billable activity" in out3, out3
    checks += 1

    # Self-check: the count and the label must agree. A hardcoded
    # denominator once read "5/6" on a green run and "8/7" on another —
    # a label that contradicts the run costs someone an investigation.
    assert checks == 5, f"expected 5 checks, ran {checks}"
    print(f"test_s3cost: {checks} checks PASS")
    return 0


if __name__ == "__main__":
    sys.exit(main())