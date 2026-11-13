#!/usr/bin/env python3
"""Snapshot the DeepSeek pricing page model/price table.

Run repeatedly; each run saves a timestamped snapshot and prints a diff
against the previous snapshot so table changes (e.g. V4 Pro retirement on
2026-09-14 12:00 Beijing) are surfaced loudly instead of silently.

Usage:
    python3 scripts/ds_pricing_snapshot.py            # snapshot + diff vs latest
    python3 scripts/ds_pricing_snapshot.py --show     # also print the parsed table
"""
import argparse
import datetime as dt
import difflib
import html
import json
import os
import re
import sys
import urllib.request

URL = "https://api-docs.deepseek.com/zh-cn/quick_start/pricing/"
SNAP_DIR = os.path.expanduser("~/.gopi/pricing-snapshots")


def fetch_text() -> str:
    req = urllib.request.Request(URL, headers={"User-Agent": "Mozilla/5.0"})
    raw = urllib.request.urlopen(req, timeout=60).read().decode("utf-8", "ignore")
    s = re.sub(r"<(script|style)[^>]*>.*?</\1>", "", raw, flags=re.S | re.I)
    s = re.sub(r"<[^>]+>", " ", s)
    s = html.unescape(s)
    return re.sub(r"\s+", " ", s).strip()


def extract_table(text: str) -> dict:
    """Pull the '模型细节' block: model names, versions, and prices."""
    out = {"raw_models_section": "", "model_names": [], "versions": [], "prices": []}
    i = text.find("模型细节")
    j = text.find("扣费规则")
    section = text[i : j if j > i else i + 4000]
    out["raw_models_section"] = section

    # model column headers look like: deepseek-flash (1) deepseek-v4-pro (2)
    out["model_names"] = re.findall(r"\b(deepseek[-a-z0-9.]*)\b", section)
    # versions: DeepSeek-V4.1-Flash / DeepSeek-V4-Pro-0813
    out["versions"] = re.findall(r"DeepSeek-[A-Za-z0-9.\-]+", section)
    # prices: <number>元
    out["prices"] = re.findall(r"(\d+(?:\.\d+)?)元", section)
    return out


def check_sane(parsed: dict) -> str:
    """Return an error string if the fetch plainly failed to yield a table.

    A rate-limited / redirected / truncated response parses to empty lists.
    Writing that as a snapshot would (a) raise a bogus CHANGED and (b) poison
    the baseline, silently swallowing the next real change. Refuse instead.
    Note this only guards the shape we always expect — it never suppresses a
    genuine table change, which keeps at least one model and one price.
    """
    if not parsed["model_names"]:
        return "no model names parsed — page layout changed or fetch failed"
    if not parsed["versions"]:
        return "no version strings parsed — page layout changed or fetch failed"
    if not parsed["prices"]:
        return "no prices parsed — page layout changed or fetch failed"
    return ""


def main(argv=None) -> None:
    ap = argparse.ArgumentParser()
    ap.add_argument("--show", action="store_true", help="print parsed table fields")
    args = ap.parse_args(argv)

    os.makedirs(SNAP_DIR, exist_ok=True)
    now = dt.datetime.now()
    text = fetch_text()
    parsed = extract_table(text)

    err = check_sane(parsed)
    if err:
        print(f"[ABORT]   fetch did not yield a usable table: {err}", file=sys.stderr)
        print(f"[ABORT]   nothing written — baseline preserved", file=sys.stderr)
        raise SystemExit(2)

    # Snapshot naming: zero-padded monotonic sequence + timestamp. Ordering by
    # NAME is then exact (no mtime precision races, no collision suffixes),
    # so "newest snapshot" is well-defined even for runs inside one second.
    def existing():
        nums = []
        for f in os.listdir(SNAP_DIR):
            m = re.fullmatch(r"(\d+)-(.*)\.json", f)
            if m:
                nums.append((int(m.group(1)), os.path.join(SNAP_DIR, f)))
        return max(nums, default=None)

    prev_entry = existing()
    prev = None
    if prev_entry:
        with open(prev_entry[1]) as fh:
            prev = json.load(fh)

    seq = prev_entry[0] + 1 if prev_entry else 1
    snap = {"ts": now.isoformat(timespec="seconds"), "url": URL,
            "text": text, "parsed": parsed}
    path = os.path.join(SNAP_DIR, f"{seq:06d}-{now.strftime('%Y%m%d-%H%M%S')}.json")
    with open(path, "w") as fh:
        json.dump(snap, fh, ensure_ascii=False, indent=1)

    print(f"[snapshot] {path}")
    print(f"[models]   {parsed['model_names']}")
    print(f"[versions] {parsed['versions']}")
    print(f"[prices]   {parsed['prices']}")
    if args.show:
        print("[section]", parsed["raw_models_section"][:1200])

    if prev is None:
        print("[diff]     (first snapshot — nothing to compare)")
        return

    if prev.get("parsed") == parsed:
        print(f"[diff]     UNCHANGED vs {prev['ts']}")
        return

    print(f"[diff]     *** CHANGED vs {prev['ts']} ***")
    for field in ("model_names", "versions", "prices"):
        a, b = prev["parsed"].get(field, []), parsed.get(field, [])
        if a != b:
            print(f"  {field}: {a}")
            print(f"  {' ' * len(field)}  -> {b}")
    sm = difflib.SequenceMatcher(None, prev["text"], text)
    for tag, i1, i2, j1, j2 in sm.get_opcodes():
        if tag != "equal":
            print(f"  text[{tag}] -{prev['text'][i1:i2][:200]!r}")
            print(f"  text[{tag}] +{text[j1:j2][:200]!r}")


if __name__ == "__main__":
    main()
