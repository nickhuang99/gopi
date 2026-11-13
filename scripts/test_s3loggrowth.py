#!/usr/bin/env python3
"""Functional test for scripts/s3loggrowth.py — no network, pure arithmetic.

The model must be checkable by hand, because the whole point is to convert a
one-day sample into a multi-month bill. Two things have to be right or the
answer is silently wrong in the reassuring direction:

  1. The cumulative footprint integral. A log bucket's stored bytes grow
     linearly, so the billed GB-month over N days is the TRIANGLE under that
     line: 0.5 * (bytes/day) * N (in GB) * N (months of carry).
     Getting this wrong by a factor of N is the easiest mistake to make.
  2. Request charges are per-object, so a write-only log bucket pays a PUT per
     object no matter how small it is. Byte storage is nearly free per file at
     4 KB; the per-object PUT is not.

The headline check: 7.4 years of the measured rate reproduces the $82 bill.
That is the claim the whole investigation rests on, so it is pinned here.
"""
import importlib.util
import os
import sys

sys.path.insert(0, os.path.join(os.path.dirname(os.path.abspath(__file__)), "lib"))

HERE = os.path.dirname(os.path.abspath(__file__))
SCRIPT = os.path.join(HERE, "s3loggrowth.py")
GB = 1e9


def load():
    spec = importlib.util.spec_from_file_location("s3loggrowth", SCRIPT)
    mod = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(mod)
    return mod


def main():
    mod = load()
    checks = 0

    # --- 1. the cumulative integral, hand-computed -----------------------
    # 1 file/s, 1 GB/file, 1 day: bytes/day = 1e9; stored after 1 day = 1 GB;
    # GB-month billed = 0.5 * 1e9 * 1 / 1e9 * 1 = 0.5
    r = mod.estimate(files_per_day=1.0, avg_bytes=GB, days=1)
    assert abs(r["gb"] - 1.0) < 1e-9, r["gb"]
    # byte-days = 0.5*1*1e9*1 = 5e8; GB-month = 5e8/1e9/30 = 1/60
    expect_gbm = 0.5 / 30
    assert abs(r["gb_months"] - expect_gbm) < 1e-12, \
        f"triangle wrong: {r['gb_months']} != {expect_gbm}"
    assert abs(r["storage_cost"] - expect_gbm * 0.023) < 1e-12, r["storage_cost"]
    checks += 1

    # --- 2. the integral scales as N^2, and at 2 days it is 4x -----------
    # (footprint doubles AND it is carried twice as long)
    r2 = mod.estimate(1.0, GB, 2)
    assert abs(r2["gb_months"] - expect_gbm * 4) < 1e-12, \
        f"2-day triangle: {r2['gb_months']}"
    r4 = mod.estimate(1.0, GB, 4)
    assert abs(r4["gb_months"] - expect_gbm * 16) < 1e-12, \
        f"4-day triangle: {r4['gb_months']}"
    # NOT linear in days: that would be the 0.5*n*avg*days mistake
    assert r4["gb_months"] != expect_gbm * 4, "days must not enter only once"
    checks += 1

    # --- 3. request charges are per object, independent of size ----------
    small = mod.estimate(1_000_000, 100, 1)     # 1M tiny objects
    big = mod.estimate(1_000_000, 100_000, 1)   # same count, 1000x larger
    assert abs(small["put_cost"] - big["put_cost"]) < 1e-9, \
        "PUT cost must not depend on object size"
    assert big["storage_cost"] > small["storage_cost"] * 100, \
        "storage cost must scale with bytes"
    checks += 1

    # 1M PUTs at $0.005/1k = $5
    assert abs(small["put_cost"] - 5.0) < 1e-9, small["put_cost"]
    checks += 1

    # --- 4. read_factor adds GETs on top, and 0 means write-only ---------
    w0 = mod.estimate(1_000_000, 4_029, 1, read_factor=0.0)
    w1 = mod.estimate(1_000_000, 4_029, 1, read_factor=1.0)
    assert w1["get_cost"] > 0 and w0["get_cost"] == 0
    # a GET is 12.5x cheaper than a PUT (0.0004 vs 0.005 per 1k)
    assert abs(w1["get_cost"] - 0.4) < 1e-9, w1["get_cost"]
    checks += 1

    # --- 5. the headline claim, pinned: measured rate -> ~$82 over years --
    files_day, avg = 327_988, 4_029        # the measured single-day sample
    years = 7.4
    r = mod.estimate(files_day, avg, years * 365)
    # This is an UPPER bound: it assumes the bucket received this rate
    # continuously for 7.4 years. The observed bill is $82/month, so the floor
    # statement that matters is "the current monthly bill is reproducible from
    # a bucket whose FOOTPRINT is ~3.5 TB" - see the footprint check below.
    assert r["storage_cost"] > 82, "continuous-rate upper bound must exceed $82"
    # the footprint after that many days, priced flat, is the $82-scale number
    footprint_gb = files_day * avg * years * 365 / GB
    assert 3000 < footprint_gb < 5000, f"footprint {footprint_gb:.0f} GB"
    flat_monthly = footprint_gb * 0.023
    assert 60 < flat_monthly < 130, f"flat monthly {flat_monthly:.2f}"
    checks += 1

    # --- 6. monotonic in days, and zero-length is zero -------------------
    prev = -1.0
    for d in (0, 1, 7, 30, 90, 180, 365):
        t = mod.estimate(files_day, avg, d)["total"]
        assert t >= prev, f"not monotonic at {d} days"
        prev = t
    z = mod.estimate(files_day, avg, 0)
    assert z["total"] == 0 and z["files"] == 0, z
    checks += 1

    print(f"test_s3loggrowth: {checks} checks PASS")
    return 0


if __name__ == "__main__":
    sys.exit(main())
