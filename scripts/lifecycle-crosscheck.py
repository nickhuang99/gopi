#!/usr/bin/env python3
# test-drivable: no one-shot lifecycle analysis script reading fixed argv positions at module scope
# NOTE: reads sys.argv at module scope (one-shot analysis script).
# To make it test-drivable, thread argv through a main(argv=None).
"""Final cross-validation table: session JSONL actions vs memory.db sediment.

    python3 scripts/lifecycle-crosscheck.py /tmp/lca/extract2.jsonl ~/.gopi/memory.db

Every number printed here is quoted verbatim in
devdoc/lifecycle-chain-evidence-20260911.md §5.
"""
import json
import os
import re
import sqlite3
import sys
from collections import Counter, defaultdict

extract = sys.argv[1]
dbpath = sys.argv[2]
recs = [json.loads(l) for l in open(extract) if l.strip()]
db = sqlite3.connect("file:%s?mode=ro" % os.path.abspath(dbpath), uri=True)
q = db.execute
one = lambda sql, *a: q(sql, a).fetchone()[0]


def era(ts):
    d = (ts or "")[:10]
    return "E1" if d <= "2026-08-27" else ("E2" if d <= "2026-09-09" else "E3")


print("### 0. frame")
print("  sessions with any extracted action :", len({r["session_id"] for r in recs}))
print("  memory.db distilled_qa / session_conclusions / tasks:",
      one("SELECT COUNT(*) FROM distilled_qa"), one("SELECT COUNT(*) FROM session_conclusions"),
      one("SELECT COUNT(*) FROM tasks"))

print("\n### 1. H1 research")
web = [r for r in recs if r["tool"] in ("web_search", "web_fetch")]
print("  web_search+web_fetch calls        :", len(web), "in", len({r['session_id'] for r in web}), "sessions")
print("  distinct web_search queries       :", len({str(r["input"].get("query")) for r in recs if r["tool"] == "web_search"}))
print("  distilled_qa rows citing a URL    :", one("SELECT COUNT(*) FROM distilled_qa WHERE sources LIKE '%http%' OR answer LIKE '%http%'"))
print("  ... of those never retrieved (rc=0):", one("SELECT COUNT(*) FROM distilled_qa WHERE (sources LIKE '%http%' OR answer LIKE '%http%') AND retrieval_count=0"))
print("  tasks whose summary says RESEARCH :", one("SELECT COUNT(*) FROM tasks WHERE summary LIKE '%research%' OR summary LIKE '%RESEARCH%'"))

print("\n### 2. H2 code understanding")
u = Counter(r["tool"] for r in recs if r["tool"] in ("read", "code_search", "keyword_search", "git_blame"))
print("  understand-tool calls             :", dict(u), "total", sum(u.values()))
modblock = q("SELECT COUNT(*) FROM distilled_qa WHERE id BETWEEN 924 AND 1009").fetchone()[0]
print("  distilled_qa rows from the ONE 08-27 code-reading task (id 924-1009):", modblock)
print("  rows naming a *.go file           :", one("SELECT COUNT(*) FROM distilled_qa WHERE topic LIKE '%.go%' OR topic LIKE '%.py%'"))
print("  rows naming a *.go file w/ empty sources:", one("SELECT COUNT(*) FROM distilled_qa WHERE (topic LIKE '%.go%' OR topic LIKE '%.py%') AND (sources IS NULL OR sources='')"))
code_sess = {r["session_id"] for r in recs if r["tool"] in ("read", "code_search")}
CODEQ = re.compile(r"(\.go\b|internal/|cmd/|代码|实现|架构|module|模块|package|函数|symbol)", re.I)
per_sess_q = defaultdict(list)
for r in recs:
    if r["tool"] == "memory:search":
        per_sess_q[r["session_id"]].append(str(r["input"].get("query", "")))
agg = defaultdict(lambda: [0, 0])
for s, qs in per_sess_q.items():
    e = era(next((r["ts"] for r in recs if r["session_id"] == s), ""))
    agg[e][0] += 1
    if any(CODEQ.search(x) for x in qs):
        agg[e][1] += 1
for e in ("E1", "E2", "E3"):
    print("  %s sessions issuing a code-targeted memory query: %d / %d memory-using sessions" % (e, agg[e][1], agg[e][0]))

print("\n### 3. H3 debugging")
PLAG = re.compile(r"(FAIL|panic:|undefined:|cannot use|no such file|syntax error|error:)", re.I)
fails = [r for r in recs if r["tool"] == "bash" and r.get("result_flag")]
print("  bash cmds flagged as failing      :", len(fails), "in", len({r['session_id'] for r in fails}), "sessions")
TESTSIG = re.compile(r"FAIL: (Test[A-Za-z0-9_]+)")
occ = defaultdict(set)
for r in fails:
    m = TESTSIG.search(r.get("result_head", ""))
    if m:
        occ[m.group(1)].add(r["session_id"])
assert occ
rec = {k: len(v) for k, v in occ.items() if len(v) >= 3}
print("  distinct FAILING test names       :", len(occ))
print("  ... recurring in >=3 sessions     :", len(rec))
covered = sum(1 for k in rec if one("SELECT COUNT(*) FROM distilled_qa WHERE topic LIKE ? OR answer LIKE ? OR question LIKE ?", "%" + k + "%", "%" + k + "%", "%" + k + "%"))
print("  ... of those named anywhere in memory.db rows:", covered, "/", len(rec))
ERRQ = re.compile(r"(FAIL|error|panic|timeout|超时|flake|SQLITE_BUSY|stale|失败|cannot|undefined)", re.I)
sq = [r for r in recs if r["tool"] == "memory:search"]
print("  memory:search calls               :", len(sq))
print("  ... whose QUERY carries an error token:", sum(1 for r in sq if ERRQ.search(str(r["input"].get("query", "")))))
print("  distilled_qa rows keyed on an error token:",
      {k: one("SELECT COUNT(*) FROM distilled_qa WHERE topic LIKE ? OR search_kw LIKE ?", "%" + k + "%", "%" + k + "%") for k in ("FAIL", "timeout", "SQLITE_BUSY", "panic", "flake")})

print("\n### 4. H4 environment state")
ENV = re.compile(r"(^\s*(which|command -v|type)\s)|(--version\b)|(brew (list|info|install))|(\bgo env\b)|(\bsystemctl\b)", re.I)
ev = [r for r in recs if r["tool"] == "bash" and ENV.search(r.get("cmd", ""))]
print("  env-probe bash cmds               :", len(ev), "in", len({r['session_id'] for r in ev}), "sessions")
print("  distilled_qa questions that ARE an env state fact:",
      {k: one("SELECT COUNT(*) FROM distilled_qa WHERE question LIKE ?", "%" + k + "%") for k in ("装了什么", "哪个版本", "是否安装", "installed", "version")})

print("\n### 5. H5 requirement motive")
print("  tasks total                       :", one("SELECT COUNT(*) FROM tasks"))
for s in ("human", "llm", "auto"):
    t = one("SELECT COUNT(*) FROM tasks WHERE source=?", s)
    e = one("SELECT COUNT(*) FROM tasks WHERE source=? AND (payload='' OR payload IS NULL)", s)
    print("   source=%-5s n=%4d payload_empty=%4d (%d%%)" % (s, t, e, round(100 * e / max(t, 1))))
print("  tasks.reason non-empty            :", one("SELECT COUNT(*) FROM tasks WHERE reason IS NOT NULL AND reason<>''"))
print("  session_conclusions.analysis non-empty:", one("SELECT COUNT(*) FROM session_conclusions WHERE analysis IS NOT NULL AND analysis<>''"),
      "/", one("SELECT COUNT(*) FROM session_conclusions"))
print("  payload of the human tasks containing a literal '$(' (unexpanded shell):",
      one("SELECT COUNT(*) FROM tasks WHERE payload LIKE '%$(%'"))

print("\n### 6. sink (store) side — is the WRITER the bottleneck?")
store = {r["session_id"] for r in recs if r["tool"] in ("memory:store", "memory:update")}
all_sessions = {r["session_id"] for r in recs}
print("  sessions that called memory:store|update:", len(store), "/", len(all_sessions))
first = {}
for r in recs:
    s = r["session_id"]
    if s not in first or (r["ts"] or "") < (first[s][0] or ""):
        first[s] = (r["ts"], r["tool"])
fa = defaultdict(lambda: [0, 0])
for s, (t, tool) in first.items():
    e = era(t)
    fa[e][0] += 1
    if tool == "memory:search":
        fa[e][1] += 1
for e in ("E1", "E2", "E3"):
    print("  %s sessions whose FIRST extracted call is memory:search: %d / %d" % (e, fa[e][1], fa[e][0]))
