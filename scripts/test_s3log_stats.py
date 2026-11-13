#!/usr/bin/env python3
"""Functional test for scripts/s3log_stats.py — no network, no big data.

The parser is where this script lives or dies: the S3 access-log line is NOT a
well-behaved CSV. Three traps cost real debugging time while it was written,
and each one is pinned here:

  1. the [timestamp] contains a SPACE, so a naive split shifts every later field
  2. status/errcode/sentbytes sit AFTER the request line's closing quote, and
     are preceded by a space — so `split(" ")` yields a leading "" and shifts
     them (status="" , errcode="403")
  3. the key appears twice: once whitespace-delimited (truncated if it has a
     space) and once inside the request line (authoritative)

Plus the aggregation contract: per-prefix counts/bytes, status buckets, and
the scraper classification the bill analysis leans on.
"""
import importlib.util
import os
import sys
import tempfile

sys.path.insert(0, os.path.join(os.path.dirname(os.path.abspath(__file__)), "lib"))
from tmp_ban import make_scratch_dir  # noqa: E402

HERE = os.path.dirname(os.path.abspath(__file__))
SCRIPT = os.path.join(HERE, "s3log_stats.py")


def load():
    spec = importlib.util.spec_from_file_location("s3stats", SCRIPT)
    mod = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(mod)
    return mod


# A real line (from the actual corpus), plus a key-with-spaces variant.
LINE_403 = (
    "OWNER staroceans.org [09/Sep/2026:06:35:56 +0000] 10.12.89.137 - REQID123 "
    "WEBSITE.GET.OBJECT myprojects/linux/arch/x/irq.h "
    '"GET /myprojects/linux/arch/x/irq.h HTTP/1.1" 403 AccessDenied 15003 15003 '
    '32 31 "-" "Mozilla/5.0 (compatible; Googlebot/2.1; +http://www.google.com/bot.html)" '
    "- Tk== - - - staroceans.org - - - us-east-1"
)
LINE_200_SPACEY = (
    "OWNER staroceans.org [09/Sep/2026:07:00:00 +0000] 1.2.3.4 - REQID9 "
    "WEBSITE.GET.OBJECT some/dir/with space.pdf "
    '"GET /some/dir/with%20space.pdf HTTP/1.1" 200 - 123456 123456 10 9 '
    '"http://ref/" "Mozilla/5.0 (compatible; Bytespider; x@x.com)" '
    "- sig - - - staroceans.org - - - ap-southeast-1"
)
LINE_404 = (
    "OWNER staroceans.org [09/Sep/2026:07:01:00 +0000] 5.6.7.8 - R3 "
    "WEBSITE.GET.OBJECT missing/thing "
    '"GET /missing/thing HTTP/1.1" 404 NoSuchKey 271 271 5 4 '
    '"-" "curl/8.0" - s - - - staroceans.org - - - us-east-1'
)
LINE_DELETE = (
    "OWNER staroceans.org [09/Sep/2026:07:02:00 +0000] 9.9.9.9 - R4 "
    "REST.DELETE.OBJECT gone/thing "
    '"DELETE /gone/thing HTTP/1.1" 204 - 0 0 3 2 "-" "aws-cli/2" - s - - - '
    "staroceans.org - - - us-east-1"
)
LINE_GARBAGE = "this is not an access log line at all"


def main():
    mod = load()
    checks = 0

    # 1. the three field-position traps
    r = mod.parse_line(LINE_403)
    assert r is not None, "403 line failed to parse"
    bucket, key, op, status, errcode, sent = r
    assert bucket == "staroceans.org", bucket
    assert op == "WEBSITE.GET.OBJECT", op
    assert key == "myprojects/linux/arch/x/irq.h", f"key wrong: {key!r}"
    assert status == "403", f"status shifted (got {status!r})"
    assert errcode == "AccessDenied", f"errcode shifted (got {errcode!r})"
    assert sent == 15003, f"sentbytes wrong: {sent}"
    checks += 1

    # 2. key with a space: the request line is authoritative, not field 7
    r = mod.parse_line(LINE_200_SPACEY)
    assert r and r[1] == "some/dir/with%20space.pdf", f"spacey key: {r and r[1]!r}"
    assert r[3] == "200" and r[5] == 123456, f"spacey line fields: {r}"
    checks += 1

    # 3. no-such-key and delete lines parse, and delete carries 0 bytes
    r404 = mod.parse_line(LINE_404)
    assert r404 and (r404[3], r404[4]) == ("404", "NoSuchKey"), r404
    rdel = mod.parse_line(LINE_DELETE)
    assert rdel and rdel[2] == "REST.DELETE.OBJECT" and rdel[5] == 0, rdel
    checks += 1

    # 4. garbage returns None instead of fabricating a row
    assert mod.parse_line(LINE_GARBAGE) is None
    assert mod.parse_line("") is None
    checks += 1

    # 5. aggregation: counts, bytes, status buckets, prefix grouping
    d = make_scratch_dir("s3log-test")
    path = os.path.join(d, "one-log-file")
    with open(path, "w") as f:
        for ln in (LINE_403, LINE_200_SPACEY, LINE_404, LINE_DELETE, LINE_GARBAGE):
            f.write(ln + "\n")
    part = mod.analyze_file(path)
    assert part["lines"] == 5, part["lines"]
    assert part["errors"] == 1, f"garbage should be counted, not parsed: {part['errors']}"
    assert part["prefix"]["myprojects/linux"] == [1, 15003], part["prefix"]
    assert part["prefix"]["some/dir"] == [1, 123456], part["prefix"]
    assert part["status"]["403 AccessDenied"] == 1, part["status"]
    assert part["status"]["404 NoSuchKey"] == 1, part["status"]
    assert part["op"]["REST.DELETE.OBJECT"] == 1, part["op"]
    checks += 1

    # 6. scraper classification is what the bill analysis depends on
    #    (garbage line never reaches the UA classifier — it fails to parse first)
    assert part["ua"].get("googlebot") == 1, part["ua"]
    assert part["ua"].get("bytespider") == 1, part["ua"]
    assert part["ua"].get("(human/other)") == 2, part["ua"]
    assert sum(part["ua"].values()) == part["lines"] - part["errors"], part["ua"]
    checks += 1

    # 7. merge() adds parts instead of replacing them
    acc = mod.new_acc()
    mod.merge(acc, part)
    mod.merge(acc, part)
    assert acc["lines"] == 10, acc["lines"]
    assert acc["prefix"]["myprojects/linux"] == [2, 30006], acc["prefix"]
    assert acc["status"]["403 AccessDenied"] == 2, acc["status"]
    assert acc["buckets"] == {"staroceans.org"}, acc["buckets"]
    checks += 1

    # Self-check: the count and the label must agree. A hardcoded
    # denominator once read "5/6" on a green run and "8/7" on another —
    # a label that contradicts the run costs someone an investigation.
    assert checks == 7, f"expected 7 checks, ran {checks}"
    print(f"test_s3log_stats: {checks} checks PASS")
    return 0


if __name__ == "__main__":
    sys.exit(main())
