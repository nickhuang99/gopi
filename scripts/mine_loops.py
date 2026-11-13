#!/usr/bin/env python3
"""Offline miner pass 2 — repeated MANUAL LOOPS across sessions.

Mines every session JSONL for normalized bash commands + tool-call
prefix sequences and reports how many DISTINCT sessions each recurs in.
That recurrence count is the raw distribution the proposal threshold is
derived from. Read-only.
"""
import json, os, re, glob, collections, sys

SESS = os.path.expanduser("~/.gopi/sessions")

NUM = re.compile(r"\b\d+\b")
HEX = re.compile(r"\b[0-9a-f]{7,}\b")
QID = re.compile(r"[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}")

def norm_cmd(c):
    c = c.strip()
    c = QID.sub("<id>", c)
    c = c.replace(os.path.expanduser("~"), "~")
    c = HEX.sub("<h>", c)
    c = NUM.sub("<n>", c)
    c = re.sub(r"\s+", " ", c)
    return c

def signature(c):
    """Coarse loop signature: the command's verb chain (first 4 tokens),
    flags and paths stripped of their volatile parts."""
    c = norm_cmd(c)
    toks = c.split()
    return " ".join(toks[:4])

cmd_sessions = collections.defaultdict(set)
sig_sessions = collections.defaultdict(set)
py_heredocs = collections.Counter()
n_files = 0
for f in glob.glob(os.path.join(SESS, "*", "*.jsonl")):
    if os.path.getsize(f) < 200:
        continue
    sid = os.path.basename(os.path.dirname(f))
    n_files += 1
    seen = set()
    for line in open(f, "r", errors="replace"):
        line = line.strip()
        if not line.startswith("{"):
            continue
        try:
            d = json.loads(line)
        except Exception:
            continue
        if d.get("type") != "assistant":
            continue
        for blk in ((d.get("message") or {}).get("content") or []):
            if not (isinstance(blk, dict) and blk.get("type") == "tool_use"):
                continue
            if (blk.get("name") or "") != "bash":
                continue
            cmd = (blk.get("input") or {}).get("command") or ""
            if not cmd:
                continue
            seen.add(norm_cmd(cmd))
            s = signature(cmd)
            if len(s.split()) >= 3:
                sig_sessions[s].add(sid)
    for c in seen:
        cmd_sessions[c].add(sid)

def report(counter_map, name, mins=(5, 10, 20, 30, 50)):
    sizes = collections.Counter(len(v) for v in counter_map.values())
    tot = len(counter_map)
    print("== %s: %d distinct, sessions-with-jsonl=%d" % (name, tot, n_files))
    print("   session-count distribution:", sorted(sizes.items(), key=lambda kv: -kv[0])[:12])
    for m in mins:
        print("   >=%d sessions: %d" % (m, sum(1 for v in counter_map.values() if len(v) >= m)))
    return sizes

report(cmd_sessions, "exact normalized bash commands")
report(sig_sessions, "bash loop signatures (first 4 tokens)")

print()
print("== top loop signatures (by distinct sessions) ==")
for s, v in sorted(sig_sessions.items(), key=lambda kv: -len(kv[1]))[:25]:
    print("   %4d  %s" % (len(v), s[:95]))
print()
print("== top exact commands ==")
for s, v in sorted(cmd_sessions.items(), key=lambda kv: -len(kv[1]))[:20]:
    print("   %4d  %s" % (len(v), s[:110]))
