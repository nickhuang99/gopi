#!/usr/bin/env python3
"""到达侧逐字输入复核（anti-cheat 第 8 条的第二半：到达侧 sha256）。

方法：拿发送侧 6 个探测文件，按 `$(cat f)` 的字节语义（剥掉**行尾一个**换行）
算出 delivered sha256_16；然后在四家**自己的**会话记录里遍历每一条字符串，比对
这个 sha256_16。命中即证明该家收到的那串字符与发送侧逐字相同。

只读：不写四家任何目录，不杀任何进程，不改任何配置。
用法：strongest-arrival-check.py <materials-dir>
"""
import glob
import hashlib
import json
import os
import subprocess
import sys

M = sys.argv[1]

want = {}
for f in sorted(glob.glob(os.path.join(M, "probe-m*.txt"))):
    payload = open(f, "rb").read().rstrip(b"\n")  # $() strips ONE trailing newline
    want[hashlib.sha256(payload).hexdigest()[:16]] = os.path.basename(f)[6:-4]


def texts(lines):
    """Every string anywhere in a JSONL line (the payload lives in some nested field).
    A line that is not JSON at all is treated as a plain string — that is how the
    hermes reader hands over rows straight from the DB."""
    out = []
    for ln in lines:
        try:
            obj = json.loads(ln)
        except Exception:
            if isinstance(ln, str) and ln:
                out.append(ln)
            continue
        stack = [obj]
        while stack:
            x = stack.pop()
            if isinstance(x, str):
                out.append(x)
            elif isinstance(x, dict):
                stack.extend(x.values())
            elif isinstance(x, list):
                stack.extend(x)
    return out


def scan(label, paths, lines_of):
    found = {}
    for p in paths:
        try:
            hay = texts(lines_of(p))
        except Exception:
            continue
        for t in hay:
            h = hashlib.sha256(t.encode()).hexdigest()[:16]
            if h in want:
                found.setdefault(want[h], set()).add(p)
    print(f"### {label}")
    for probe in sorted(want.values()):
        hits = found.get(probe)
        if hits:
            print(f"  {probe:16s} HIT  {sorted(hits)[0]}")
        else:
            print(f"  {probe:16s} MISS (no record — the cell produced no session record)")
    return found


def jsonl(path):
    return open(path, errors="replace")


def zstd(path):
    return subprocess.run(["zstd", "-dc", path], capture_output=True, text=True).stdout.splitlines()


def hermes_rows():
    """hermes stores the arrival in ~/.hermes/state.db (messages.role='user').
    read-only; only the newest rows are scanned (the 5.7 GB DB has an FTS index
    we must not trigger)."""
    import sqlite3

    global _HERMES_ROWS
    if "_HERMES_ROWS" not in globals():
        c = sqlite3.connect("file:%s?mode=ro" % os.path.expanduser("~/.hermes/state.db"), uri=True)
        c.execute("pragma query_only=1")
        _HERMES_ROWS = [r for r in c.execute(
            "select id, content from messages where role='user' "
            "and id > (select max(id)-4000 from messages)")]
    return [t for _, t in _HERMES_ROWS if isinstance(t, str)]


print("delivered sha256_16 (=$() of the probe file, trailing newline stripped):")
for k, v in sorted(want.items(), key=lambda kv: kv[1]):
    print(f"  {k}  {v}")
print()

scan("codex  ~/.codex/sessions/**/rollout-*.jsonl",
     sorted(glob.glob(os.path.expanduser("~/.codex/sessions/2026/09/30/rollout-*.jsonl"))), jsonl)
scan("claude ~/.claude/projects/<cwd-hash>/*.jsonl",
     sorted(glob.glob(os.path.expanduser(
         "~/.claude/projects/-home-nick--gopi-sessions-01a0eec1-*/*.jsonl"))), jsonl)
scan("dsh    ~/.dsh/sessions/<cwd-hash>/session-*/session.jsonl.zstd",
     sorted(glob.glob(os.path.expanduser("~/.dsh/sessions/*01a0eec1*/*/session.jsonl.zstd"))), zstd)
scan("hermes ~/.hermes/state.db  messages(role=user, newest 4000 rows)",
     [os.path.expanduser("~/.hermes/state.db")], lambda _p: hermes_rows())
