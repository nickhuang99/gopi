#!/usr/bin/env python3
"""Functional test for scripts/s3egress_rank.py — no network.

Why this file exists separately from test_s3log_stats: the whole point of the
egress ranking is that it sorts by a DIFFERENT key than the request ranking.
A directory like `wiki/A` has 48,826 requests but only 0.73 GB, while a single
PDF has 29 requests and 0.81 GB. If this tool ever sorted by count, the output
would look plausible and point at the wrong thing entirely — so the test
asserts the ORDER, not just that numbers appear.
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
SCRIPT = os.path.join(HERE, "s3egress_rank.py")


def load():
    spec = importlib.util.spec_from_file_location("s3egress_rank", SCRIPT)
    mod = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(mod)
    return mod


def run(mod, fixture, args=()):
    d = make_scratch_dir("s3egress-test")
    p = os.path.join(d, "agg.json")
    with open(p, "w") as f:
        json.dump(fixture, f)
    buf = io.StringIO()
    with redirect_stdout(buf):
        code = mod.main([p, *args])
    return code, buf.getvalue()


def ranked_names(out, names):
    """Names in the order the PREFIX TABLE ranks them (top to bottom).

    Must read the table rows in output order — a name can also appear in the
    prose/footer, and matching text presence would not prove the ranking.
    """
    seen = []
    for line in out.splitlines():
        # table rows look like:  "<name padded to 40><reqs><GB><share><cost>"
        for n in names:
            if line.startswith(n) and n not in seen:
                seen.append(n)
    return seen


def main():
    mod = load()
    checks = 0

    # A fixture with the real asymmetry: many-tiny vs few-huge.
    fix = {
        "total_bytes": 1_000_000_000,
        "prefix": {
            "many/requests": [50_000, 10_000_000],     # 50k reqs, 0.01 GB
            "few/huge": [29, 800_000_000],             # 29 reqs, 0.80 GB
            "middle": [500, 100_000_000],
        },
        "key": {
            "big.pdf": [29, 800_000_000],
            "tiny.png": [50_000, 10_000_000],
        },
    }
    code, out = run(mod, fix)
    assert code == 0, code

    # --- 1. ranked by BYTES, so few/huge leads and many/requests trails ----
    order = ranked_names(out, ["few/huge", "many/requests", "middle"])
    assert order[:3] == ["few/huge", "middle", "many/requests"], \
        f"must rank by bytes (huge, middle, tiny) but got {order}"
    checks += 1

    # --- 2. the cost column is bytes x $0.09/GB ---------------------------
    # 0.80 GB -> $0.0720
    assert "0.0720" in out, f"egress $ for 0.8GB should be 0.0720:\n{out}"
    checks += 1

    # --- 3. totals reported for the whole set, not just the top N ---------
    assert "1.000" in out, f"ALL line should show exactly 1.000 GB:\n{out}"
    assert "0.0900" in out, f"1 GB total = $0.09:\n{out}"
    checks += 1

    # --- 4. both dimensions get their own table ---------------------------
    assert "TOP PREFIXES BY EGRESS" in out
    assert "TOP FULL KEYS BY EGRESS" in out
    checks += 1

    # --- 5. the read-this footer warns about the request-vs-byte trap -----
    assert "REQUEST money" in out and "EGRESS money" in out, out
    checks += 1

    # --- 6. a json without total_bytes is refused, not divided by zero ----
    try:
        run(mod, {"prefix": {}})
        raise AssertionError("missing total_bytes should raise")
    except SystemExit:
        pass
    checks += 1

    print(f"test_s3egress_rank: {checks} checks PASS")
    return 0


if __name__ == "__main__":
    sys.exit(main())
