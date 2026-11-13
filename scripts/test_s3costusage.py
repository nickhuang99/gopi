#!/usr/bin/env python3
"""Functional test for scripts/s3costusage.py — no network.

This script reads AWS's authoritative bill (Cost Explorer export). It exists
because I got the answer WRONG from a self-consistent model: I assumed the log
bucket's PUT traffic dominated, built arithmetically sound reasoning on top of
it, and was off by 400x on that line. The fix is to read the bill, not infer it.

So the tests pin exactly the things that would have caught my error:

  1. usage type is a COLUMN, month is a ROW (transposed vs the access logs)
  2. blank cells mean "no usage", not zero-by-error
  3. the breakdown must attribute the biggest line correctly — for the real
     corpus that is DataTransfer-Out-Bytes, NOT Requests-Tier1
  4. a PUT-heavy fixture must report PUT as dominant (so the aggregate is
     really reading the data and not hardcoding an expectation)
"""
import importlib.util
import csv
import io
import os
import sys
from contextlib import redirect_stdout

sys.path.insert(0, os.path.join(os.path.dirname(os.path.abspath(__file__)), "lib"))
from tmp_ban import make_scratch_dir  # noqa: E402

HERE = os.path.dirname(os.path.abspath(__file__))
SCRIPT = os.path.join(HERE, "s3costusage.py")


def load():
    spec = importlib.util.spec_from_file_location("s3costusage", SCRIPT)
    mod = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(mod)
    return mod


def write_csv(path, header, rows):
    with open(path, "w", newline="", encoding="utf-8-sig") as f:
        w = csv.writer(f)
        w.writerow(header)
        w.writerow(["Usage type total"] + [""] * (len(header) - 1))
        for r in rows:
            w.writerow(r)


def run(mod, path, extra=()):
    buf = io.StringIO()
    with redirect_stdout(buf):
        code = mod.main([path, *extra])
    return code, buf.getvalue()


def main():
    mod = load()
    checks = 0
    d = make_scratch_dir("s3costusage-test")

    # --- 1. transposed shape: usage types are columns, months are rows -----
    # The real export has cost-type COLUMNS and month ROWS — the opposite of
    # the access logs. Getting this backwards silently zeroes everything.
    p = os.path.join(d, "a.csv")
    write_csv(p, ["Usage type", "DataTransfer-Out-Bytes($)", "Requests-Tier1($)"],
              [["2026-08-01", "64.86", "0.15"],
               ["2026-07-01", "45.32", "0.02"]])
    months, data = mod.load(p)
    assert months == ["2026-08-01", "2026-07-01"], months
    assert abs(data["DataTransfer-Out-Bytes"]["2026-08-01"] - 64.86) < 1e-9, data
    assert abs(data["Requests-Tier1"]["2026-07-01"] - 0.02) < 1e-9, data
    checks += 1

    # --- 2. blank cell means "no usage", and must not abort the parse ------
    p2 = os.path.join(d, "b.csv")
    write_csv(p2, ["Usage type", "HostedZone($)", "USE1-LAX1-AWS-Out-Bytes($)"],
              [["2026-08-01", "", "0.5"]])
    months2, data2 = mod.load(p2)
    assert months2 == ["2026-08-01"], months2
    assert data2["HostedZone"] == {}, f"blank should be absent: {data2['HostedZone']}"
    assert abs(data2["USE1-LAX1-AWS-Out-Bytes"]["2026-08-01"] - 0.5) < 1e-9
    checks += 1

    # --- 3. egress-dominant fixture must name egress as the top S3 line ----
    p3 = os.path.join(d, "c.csv")
    write_csv(p3, ["Usage type", "DataTransfer-Out-Bytes($)", "Requests-Tier1($)",
                   "TimedStorage-ByteHrs($)", "Requests-Tier2($)"],
              [["2026-08-01", "64.86", "0.15", "3.43", "6.45"]])
    code, out = run(mod, p3, ["--month", "2026-08-01"])
    assert code == 0, code
    assert "64.86" in out, out
    # 64.86 of 74.89 = 86.6% — assert the FIXTURE's share, not the real
    # corpus's 79.6% (a test that hardcodes the production number would pass
    # here for the wrong reason and hide an aggregate bug).
    assert "86.6%" in out, out
    # the tool must SAY where the money goes, not merely print a table
    assert "egress" in out.lower(), out
    checks += 1

    # --- 4. an inverted fixture must flip the answer (proves no hardcoding) -
    p4 = os.path.join(d, "d.csv")
    write_csv(p4, ["Usage type", "DataTransfer-Out-Bytes($)", "Requests-Tier1($)"],
              [["2026-08-01", "0.15", "64.86"]])
    code, out4 = run(mod, p4, ["--month", "2026-08-01"])
    assert code == 0, code
    # PUT now dominates; the S3 breakdown must reflect the data
    assert "64.86" in out4 and "Requests-Tier1" in out4, out4
    tier1_line = [ln for ln in out4.splitlines() if "Requests-Tier1" in ln and "PUT" in ln]
    assert tier1_line, f"PUT line missing:\n{out4}"
    assert "99.8%" in tier1_line[0], tier1_line[0]
    checks += 1

    # --- 5. totals add up to the sum of their parts ------------------------
    code, out5 = run(mod, p3, ["--month", "2026-08-01"])
    assert "74.89" in out5, f"S3 subtotal should be 64.86+6.45+3.43+0.15:\n{out5}"
    checks += 1

    # --- 6. a short/garbage CSV is refused rather than crashing ------------
    p6 = os.path.join(d, "e.csv")
    with open(p6, "w") as f:
        f.write("Usage type\n")
    try:
        mod.load(p6)
        raise AssertionError("short csv should raise")
    except SystemExit:
        pass
    checks += 1

    print(f"test_s3costusage: {checks} checks PASS")
    return 0


if __name__ == "__main__":
    sys.exit(main())
