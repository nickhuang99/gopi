#!/usr/bin/env python3
"""Functional test for scripts/s3storage.py — no network, no big data.

This script answers "is the S3 bill storage?" so its parse must not silently
drop lines: a dropped line under-counts bytes, and under-counting is the one
error direction that makes the answer WRONG in the reassuring way ("only 3 TB,
not the problem"). Hence the test pins the unparsed count too.

Traps it inherits from the other S3 tools (same line format):
  - [timestamp] contains a space
  - STATUS/ERRCODE/SENT/OBJSIZE live after the request line's closing quote,
    preceded by a space -> split(" ") yields a leading '' and shifts them
  - a key with a space truncates field 7; the request line is authoritative

Plus the tiered pricing, which must not be a flat rate.
"""
import importlib.util
import io
import os
import sys

sys.path.insert(0, os.path.join(os.path.dirname(os.path.abspath(__file__)), "lib"))
from tmp_ban import make_scratch_dir  # noqa: E402

HERE = os.path.dirname(os.path.abspath(__file__))
SCRIPT = os.path.join(HERE, "s3storage.py")


def load():
    spec = importlib.util.spec_from_file_location("s3storage", SCRIPT)
    mod = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(mod)
    return mod


# real-corpus shape: STATUS ERRCODE SENTBYTES OBJSIZE
LINE = (
    "OWNER staroceans.org [09/Sep/2026:06:35:56 +0000] 10.12.89.137 - R1 "
    "WEBSITE.GET.OBJECT dir/file.bin "
    '"GET /dir/file.bin HTTP/1.1" 200 - 1000 8388608 32 31 "-" "Mozilla/5.0" '
    "- sig - - - staroceans.org - - - us-east-1"
)
LINE_SPACEY = (
    "OWNER staroceans.org [09/Sep/2026:07:00:00 +0000] 1.2.3.4 - R2 "
    "WEBSITE.GET.OBJECT a/b c.bin "
    '"GET /a/b%20c.bin HTTP/1.1" 200 - 10 2048 5 4 "-" "curl" '
    "- sig - - - staroceans.org - - - us-east-1"
)
LINE_403 = (
    "OWNER staroceans.org [09/Sep/2026:06:40:00 +0000] 9.9.9.9 - R3 "
    "WEBSITE.GET.OBJECT gone/x "
    '"GET /gone/x HTTP/1.1" 403 AccessDenied 15003 0 3 2 "-" "curl" '
    "- sig - - - staroceans.org - - - us-east-1"
)
# HEAD reports no size at all: SENT and OBJSIZE are literally "-".
LINE_HEAD_DASH = (
    "OWNER staroceans.org [09/Sep/2026:09:27:38 +0000] 186.224.30.26 - R4 "
    "WEBSITE.HEAD.OBJECT wiki/A/Mali_(processor) "
    '"HEAD /wiki/A/Mali_(processor) HTTP/1.1" 403 AccessDenied - - 25 - "-" "curl" '
    "- sig - - - staroceans.org - - - us-east-1"
)
GARBAGE = "not an access log line"


def main():
    mod = load()
    checks = 0

    # 1. objsize is field 4 after the closing quote — not sentbytes (field 3)
    r = mod.parse_sizes(LINE)
    assert r == ("dir/file.bin", 8388608), f"objsize wrong: {r}"
    checks += 1

    # 2. spacey key: request line wins
    r = mod.parse_sizes(LINE_SPACEY)
    assert r == ("a/b%20c.bin", 2048), r
    checks += 1

    # 3. a 403 carries objsize 0 -> key still parses, size is 0 (not the error body)
    r = mod.parse_sizes(LINE_403)
    assert r == ("gone/x", 0), f"403 must not use the error-body size: {r}"
    checks += 1

    # 3b. a "-" size yields (key, None) — the line IS parsed (the key is known),
    #     it just carries no size. Returning None here would inflate "unparsed"
    #     and make the report look like the parser was broken.
    r = mod.parse_sizes(LINE_HEAD_DASH)
    assert r == ("wiki/A/Mali_(processor)", None), f"dash size: {r}"
    checks += 1

    # 4. garbage is refused, not guessed
    assert mod.parse_sizes(GARBAGE) is None
    assert mod.parse_sizes("") is None
    checks += 1

    # 5. scan() dedups by key, keeps last-seen size, and REPORTS unparsed lines
    d = make_scratch_dir("s3storage-test")
    p = os.path.join(d, "log1")
    with open(p, "w") as f:
        f.write(LINE + "\n")            # 8 MB
        f.write(LINE_SPACEY + "\n")     # 2 KB
        f.write(LINE_403 + "\n")        # size 0 -> excluded from the sum
        f.write(GARBAGE + "\n")         # unparsed
    sizes, lines, errs = mod.scan(p)
    assert lines == 4 and errs == 1, f"lines={lines} errs={errs}"
    assert sizes == {"dir/file.bin": 8388608, "a/b%20c.bin": 2048}, sizes
    checks += 1

    # zero-size entries must not be stored (they would inflate the key count
    # without adding bytes, making the "distinct keys" figure misleading)
    assert "gone/x" not in sizes, sizes
    checks += 1

    # 6. tiered pricing: 50 GB is cheap, 1 TB crosses into the next band,
    #    and the function is monotonic + not a flat rate
    assert abs(mod.storage_cost(0) - 0.0) < 1e-9
    fifty = mod.storage_cost(50 * 1024)
    assert abs(fifty - 50 * 1024 * 0.023) < 1e-6, fifty
    one_tb = mod.storage_cost(1024 * 1024)
    assert one_tb > fifty, "must be monotonic"
    # crossing the 50 TB boundary must not be a flat application of $0.023
    over = mod.storage_cost(60 * 1024)
    assert over < 60 * 1024 * 0.023, f"tier not applied above 50 TB: {over}"
    assert over > 50 * 1024 * 0.023, over
    checks += 1

    # 7. the bill our report reasons about: how much storage does $82 buy?
    #    (sanity: at $0.023/GB-month that is ~3.5 TB — the headline number)
    gb_for_82 = 82.03 / 0.023
    assert 3400 < gb_for_82 < 3700, gb_for_82
    checks += 1

    print(f"test_s3storage: {checks} checks PASS")
    return 0


if __name__ == "__main__":
    sys.exit(main())
