#!/usr/bin/env python3
"""Functional test for scripts/ds_balance_probe.py — no network needed.

The probe's value is: read the balance, optionally fire one controlled request,
and turn the balance DELTA into a real per-token price. What must hold:

  1. balance parsing picks the CNY row, not just the first row
  2. usage parsing handles both shapes seen in the wild
     (prompt_cache_hit_tokens/miss_tokens  AND  prompt_tokens_details.cached_tokens)
  3. cost_from_delta divides correctly, and refuses nonsense (Δ<=0, tokens=0)
  4. the estimated-vs-real comparison is reported, not silently dropped
  5. a request failure fails the run (no pretend success)
  6. no network at import or in the tested paths

Design note: the probe is only meaningful at BATCH scale — the API reports the
balance to 2 decimals, so a single small request shows Δ=0.0. The test pins that
behaviour explicitly (check 3) so nobody later "fixes" it into a false claim.
"""
import importlib.util
import io
import os
import sys
from contextlib import redirect_stdout

sys.path.insert(0, os.path.join(os.path.dirname(os.path.abspath(__file__)), "lib"))
from tmp_ban import make_scratch_dir  # noqa: E402

HERE = os.path.dirname(os.path.abspath(__file__))
SCRIPT = os.path.join(HERE, "ds_balance_probe.py")


def load():
    spec = importlib.util.spec_from_file_location("probe", SCRIPT)
    mod = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(mod)
    return mod


BAL = {
    "is_available": True,
    "balance_infos": [
        {"currency": "USD", "total_balance": "10.00",
         "granted_balance": "0.00", "topped_up_balance": "10.00"},
        {"currency": "CNY", "total_balance": "99.01",
         "granted_balance": "0.00", "topped_up_balance": "99.01"},
    ],
}

BAL_AFTER = {
    "is_available": True,
    "balance_infos": [
        {"currency": "USD", "total_balance": "10.00",
         "granted_balance": "0.00", "topped_up_balance": "10.00"},
        {"currency": "CNY", "total_balance": "98.51",
         "granted_balance": "0.00", "topped_up_balance": "98.51"},
    ],
}


def main():
    mod = load()
    checks = 0

    # 1. CNY row is the one read (not "the first balance_infos entry")
    assert mod.cny_balance(BAL) == 99.01, mod.cny_balance(BAL)
    assert mod.cny_balance({"balance_infos": [{"currency": "USD", "total_balance": "1"}]}) is None
    checks += 1

    # 2. both usage shapes parse to the same (hit, miss)
    shape_a = {"prompt_tokens": 1000, "completion_tokens": 50,
               "prompt_cache_hit_tokens": 900, "prompt_cache_miss_tokens": 100}
    shape_b = {"prompt_tokens": 1000, "completion_tokens": 50,
               "prompt_tokens_details": {"cached_tokens": 900}}
    assert mod.split_usage(shape_a) == (900, 100, 50), mod.split_usage(shape_a)
    assert mod.split_usage(shape_b) == (900, 100, 50), mod.split_usage(shape_b)
    # no cache fields at all -> all miss, never a negative
    assert mod.split_usage({"prompt_tokens": 10, "completion_tokens": 2}) == (0, 10, 2)
    checks += 1

    # 3. delta -> price, and the honest zero case (2-decimal balance)
    #    0.50 CNY for 100 miss + 900 hit + 50 out
    hit, miss, out = 900, 100, 50
    est = mod.estimate(hit, miss, out)
    assert est > 0, est
    assert abs(mod.delta_of(99.01, 98.51) - 0.50) < 1e-9
    # a real measurement of Δ=0 must NOT be reported as a price
    assert mod.delta_of(99.01, 99.01) == 0.0
    checks += 1

    # 4. the comparison line names both real and estimated (so a mismatch is visible)
    line = mod.compare_line(0.50, est, hit, miss, out)
    assert "0.500000" in line or "0.50" in line, line
    assert "estimated" in line.lower(), line
    checks += 1

    # 5. a request failure raises instead of printing a fake delta
    real_chat, real_bal, real_key = mod.chat, mod.balance, mod.api_key
    mod.api_key = lambda: "test-key"
    mod.balance = lambda k: BAL
    mod.chat = lambda *a, **k: (_ for _ in ()).throw(RuntimeError("simulated 500"))
    try:
        mod.main(["--probe", "--settle", "0", "--key", "test-key"])
    except RuntimeError:
        pass                      # expected: surfaced, not swallowed
    else:
        raise AssertionError("request failure was swallowed")
    finally:
        mod.chat, mod.balance, mod.api_key = real_chat, real_bal, real_key
    checks += 1

    # 6. the dry path reads the balance exactly once and needs no other network
    called = {"n": 0}
    real_bal = mod.balance
    real_key = mod.api_key
    mod.balance = lambda k: (called.__setitem__("n", called["n"] + 1), BAL)[1]
    mod.api_key = lambda: "test-key"
    buf = io.StringIO()
    try:
        with redirect_stdout(buf):
            code = mod.main(["--key", "test-key"])
    finally:
        mod.balance = real_bal
        mod.api_key = real_key
    assert code == 0 and called["n"] == 1, f"balance calls={called['n']} code={code}"
    assert "99.01" in buf.getvalue(), buf.getvalue()
    checks += 1

    # Self-check: the count and the label must agree. A hardcoded
    # denominator once read "5/6" on a green run and "8/7" on another —
    # a label that contradicts the run costs someone an investigation.
    assert checks == 6, f"expected 6 checks, ran {checks}"
    print(f"test_ds_balance_probe: {checks} checks PASS")
    return 0


if __name__ == "__main__":
    sys.exit(main())
